// Copyright 2019-2023 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package surveyor

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/nats-io/jsm.go"
	"github.com/nats-io/jsm.go/api/server/metric"
	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
)

type ServiceObsMetrics struct {
	observationsGauge           prometheus.Gauge
	observationsReceived        *prometheus.CounterVec
	serviceRequestStatus        *prometheus.CounterVec
	invalidObservationsReceived *prometheus.CounterVec
	serviceLatency              *prometheus.HistogramVec
	totalLatency                *prometheus.HistogramVec
	requestorRTT                *prometheus.HistogramVec
	responderRTT                *prometheus.HistogramVec
	systemRTT                   *prometheus.HistogramVec
}

func NewServiceObservationMetrics(registry *prometheus.Registry, constLabels prometheus.Labels) *ServiceObsMetrics {
	metrics := &ServiceObsMetrics{
		observationsGauge: prometheus.NewGauge(prometheus.GaugeOpts{
			Name:        prometheus.BuildFQName("nats", "latency", "observations_count"),
			Help:        "Number of Service Latency listeners that are running",
			ConstLabels: constLabels,
		}),

		observationsReceived: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name:        prometheus.BuildFQName("nats", "latency", "observations_received_count"),
			Help:        "Number of observations received by this surveyor across all services",
			ConstLabels: constLabels,
		}, []string{"service", "app"}),

		serviceRequestStatus: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name:        prometheus.BuildFQName("nats", "latency", "observation_status_count"),
			Help:        "The status result codes for requests to a service",
			ConstLabels: constLabels,
		}, []string{"service", "status"}),

		invalidObservationsReceived: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name:        prometheus.BuildFQName("nats", "latency", "observation_error_count"),
			Help:        "Number of observations received by this surveyor across all services that could not be handled",
			ConstLabels: constLabels,
		}, []string{"service"}),

		serviceLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:        prometheus.BuildFQName("nats", "latency", "service_duration"),
			Help:        "Time spent serving the request in the service",
			ConstLabels: constLabels,
		}, []string{"service", "app"}),

		totalLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:        prometheus.BuildFQName("nats", "latency", "total_duration"),
			Help:        "Total time spent serving a service including network overheads",
			ConstLabels: constLabels,
		}, []string{"service", "app"}),

		requestorRTT: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:        prometheus.BuildFQName("nats", "latency", "requestor_rtt"),
			Help:        "The RTT to the client making a request",
			ConstLabels: constLabels,
		}, []string{"service", "app"}),

		responderRTT: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:        prometheus.BuildFQName("nats", "latency", "responder_rtt"),
			Help:        "The RTT to the service serving the request",
			ConstLabels: constLabels,
		}, []string{"service", "app"}),

		systemRTT: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:        prometheus.BuildFQName("nats", "latency", "system_rtt"),
			Help:        "The RTT within the NATS system - time traveling clusters, gateways and leaf nodes",
			ConstLabels: constLabels,
		}, []string{"service", "app"}),
	}

	registry.MustRegister(metrics.invalidObservationsReceived)
	registry.MustRegister(metrics.observationsReceived)
	registry.MustRegister(metrics.serviceRequestStatus)
	registry.MustRegister(metrics.serviceLatency)
	registry.MustRegister(metrics.totalLatency)
	registry.MustRegister(metrics.requestorRTT)
	registry.MustRegister(metrics.responderRTT)
	registry.MustRegister(metrics.systemRTT)
	registry.MustRegister(metrics.observationsGauge)

	return metrics
}

// ServiceObsListener listens for observations from nats service latency checks.
//
// Deprecated: ServiceObsListener will be unexported in a future release
// Use ServiceObsConfig and Surveyor.ServiceObservationManager instead
type ServiceObsListener struct {
	nc      *nats.Conn
	logger  *logrus.Logger
	config  *ServiceObsConfig
	metrics *ServiceObsMetrics
	sopts   *Options
}

// ServiceObsConfig is used to configure service observations
type ServiceObsConfig struct {
	ID          string `json:"id"`
	ServiceName string `json:"name"`
	Topic       string `json:"topic"`
	Credentials string `json:"credential"`
	Nkey        string `json:"nkey"`
}

// Validate is used to validate a ServiceObsConfig
func (o *ServiceObsConfig) Validate() error {
	var errs []string
	if o == nil {
		return fmt.Errorf("service observation config cannot be nil")
	}

	if o.ID == "" {
		errs = append(errs, "id is required")
	}

	if o.ServiceName == "" {
		errs = append(errs, "name is required")
	}

	if o.Topic == "" {
		errs = append(errs, "topic is required")
	}

	switch {
	case o.Credentials == "" && o.Nkey == "":
		errs = append(errs, "jwt or nkey credentials is required")
	case o.Credentials != "" && o.Nkey != "":
		errs = append(errs, "both jwt and nkey credentials found, only one can be used")
	case o.Credentials != "":
		_, err := os.Stat(o.Credentials)
		if err != nil {
			errs = append(errs, fmt.Sprintf("invalid credential file: %s", err))
		}
	case o.Nkey != "":
		_, err := os.Stat(o.Nkey)
		if err != nil {
			errs = append(errs, fmt.Sprintf("invalid nkey file: %s", err))
		}
	}

	if len(errs) == 0 {
		return nil
	}

	return errors.New(strings.Join(errs, ", "))
}

// NewServiceObservationConfigFromFile creates a new ServiceObsConfig from a file
// the ID of the ServiceObsConfig is set to the filename f
func NewServiceObservationConfigFromFile(f string) (*ServiceObsConfig, error) {
	js, err := os.ReadFile(f)
	if err != nil {
		return nil, err
	}

	obs := &ServiceObsConfig{}
	err = json.Unmarshal(js, obs)
	if err != nil {
		return nil, fmt.Errorf("invalid service observation config: %s: %s", f, err)
	}
	obs.ID = f
	err = obs.Validate()
	if err != nil {
		return nil, fmt.Errorf("invalid service observation config: %s: %s", f, err)
	}

	return obs, nil
}

// NewServiceObservation creates a new service observation listener from a JSON file.
//
// Deprecated: ServiceObsListener will be unexported in a future release
// Use NewServiceObservationConfigFromFile and Surveyor.ServiceObservationManager instead
func NewServiceObservation(f string, sopts Options, metrics *ServiceObsMetrics, reconnectCtr *prometheus.CounterVec) (*ServiceObsListener, error) {
	serviceObs, err := NewServiceObservationConfigFromFile(f)
	if err != nil {
		return nil, err
	}

	obs, err := newServiceObservationListener(serviceObs, sopts, metrics, reconnectCtr)
	if err != nil {
		return nil, err
	}

	return obs, nil
}

func newServiceObservationListener(serviceObs *ServiceObsConfig, sopts Options, metrics *ServiceObsMetrics, reconnectCtr *prometheus.CounterVec) (*ServiceObsListener, error) {
	err := serviceObs.Validate()
	if err != nil {
		return nil, fmt.Errorf("invalid service observation config: %s: %s", serviceObs.ServiceName, err)
	}

	sopts.Name = fmt.Sprintf("%s (observing %s)", sopts.Name, serviceObs.ServiceName)
	sopts.Credentials = serviceObs.Credentials
	sopts.Nkey = serviceObs.Nkey
	nc, err := connect(&sopts, reconnectCtr)
	if err != nil {
		return nil, fmt.Errorf("nats connection failed: %s", err)
	}

	return &ServiceObsListener{
		nc:      nc,
		logger:  sopts.Logger,
		config:  serviceObs,
		metrics: metrics,
		sopts:   &sopts,
	}, nil
}

// Start starts listening for observations.
func (o *ServiceObsListener) Start() error {
	_, err := o.nc.Subscribe(o.config.Topic, o.observationHandler)
	if err != nil {
		return fmt.Errorf("could not subscribe to observation topic for %s (%s): %s", o.config.ServiceName, o.config.Topic, err)
	}
	err = o.nc.Flush()
	if err != nil {
		return err
	}

	o.metrics.observationsGauge.Inc()
	o.logger.Infof("Started observing stats on %s for %s", o.config.Topic, o.config.ServiceName)

	return nil
}

func (o *ServiceObsListener) observationHandler(m *nats.Msg) {
	kind, obs, err := jsm.ParseEvent(m.Data)
	if err != nil {
		o.metrics.invalidObservationsReceived.WithLabelValues(o.config.ServiceName).Inc()
		o.logger.Warnf("data: %s", m.Data)
		o.logger.Warnf("Unparsable observation received on %s: %s", o.config.Topic, err)
		return
	}

	switch obs := obs.(type) {
	case *metric.ServiceLatencyV1:
		o.metrics.observationsReceived.WithLabelValues(o.config.ServiceName, obs.Responder.Name).Inc()
		o.metrics.serviceLatency.WithLabelValues(o.config.ServiceName, obs.Responder.Name).Observe(obs.ServiceLatency.Seconds())
		o.metrics.totalLatency.WithLabelValues(o.config.ServiceName, obs.Responder.Name).Observe(obs.TotalLatency.Seconds())
		o.metrics.requestorRTT.WithLabelValues(o.config.ServiceName, obs.Responder.Name).Observe(obs.Requestor.RTT.Seconds())
		o.metrics.responderRTT.WithLabelValues(o.config.ServiceName, obs.Responder.Name).Observe(obs.Responder.RTT.Seconds())
		o.metrics.systemRTT.WithLabelValues(o.config.ServiceName, obs.Responder.Name).Observe(obs.SystemLatency.Seconds())

		if obs.Status == 0 {
			o.metrics.serviceRequestStatus.WithLabelValues(o.config.ServiceName, "500").Inc()
		} else {
			o.metrics.serviceRequestStatus.WithLabelValues(o.config.ServiceName, strconv.Itoa(obs.Status)).Inc()
		}

	default:
		o.metrics.invalidObservationsReceived.WithLabelValues(o.config.ServiceName).Inc()
		o.logger.Warnf("Unsupported observation received on %s: %s", o.config.Topic, kind)
		return
	}
}

// Stop closes the connection to the network.
func (o *ServiceObsListener) Stop() {
	o.metrics.observationsGauge.Dec()
	o.nc.Close()
}

// ServiceObsManager exposes methods to operate on service observations.
type ServiceObsManager struct {
	sync.Mutex
	listenerMap      map[string]*ServiceObsListener
	logger           *logrus.Logger
	metrics          *ServiceObsMetrics
	reconnectCtr     *prometheus.CounterVec
	sopts            Options
	running          bool
	watcherStopChMap map[string]chan struct{}
}

// newServiceObservationManager creates an ObservationManager, allowing for adding/deleting service observations to the surveyor.
func newServiceObservationManager(logger *logrus.Logger, sopts Options, metrics *ServiceObsMetrics, reconnectCtr *prometheus.CounterVec) *ServiceObsManager {
	return &ServiceObsManager{
		logger:           logger,
		sopts:            sopts,
		metrics:          metrics,
		reconnectCtr:     reconnectCtr,
		listenerMap:      map[string]*ServiceObsListener{},
		watcherStopChMap: map[string]chan struct{}{},
		running:          false,
	}
}

func (om *ServiceObsManager) startObservationsInDir() fs.WalkDirFunc {
	return func(path string, info fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// skip directories starting with '..'
		// this prevents double observation loading when using kubernetes mounts
		if info.IsDir() && strings.HasPrefix(info.Name(), "..") {
			return filepath.SkipDir
		}

		if filepath.Ext(info.Name()) != ".json" {
			return nil
		}

		obs, err := NewServiceObservationConfigFromFile(path)
		if err != nil {
			return fmt.Errorf("could not create observation from %s: %s", path, err)
		}

		return om.Set(obs)
	}
}

func (om *ServiceObsManager) watchObservations(dir string, depth int) error {
	if depth == 0 {
		return fmt.Errorf("exceeded observation dir max depth")
	}
	if dir == "" {
		return nil
	}

	go func() {
		om.Lock()
		if !om.running {
			return
		}

		if _, ok := om.watcherStopChMap[dir]; ok {
			om.Unlock()
			return
		}

		watcher, err := fsnotify.NewWatcher()
		if err != nil {
			om.Unlock()
			om.logger.Errorf("error creating watcher: %s", err)
			return
		}

		if err := watcher.Add(dir); err != nil {
			om.Unlock()
			om.logger.Errorf("error adding dir to watcher: %s", err)
			return
		}

		stopCh := make(chan struct{}, 1)
		om.watcherStopChMap[dir] = stopCh
		om.Unlock()

		defer func() {
			om.Lock()
			delete(om.watcherStopChMap, dir)
			om.Unlock()
			watcher.Close()
		}()

		om.logger.Debugf("starting listener goroutine for %s", dir)
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if err := om.handleWatcherEvent(event, depth); err != nil {
					om.logger.Warn(err)
				}
			case <-stopCh:
				return
			}
		}
	}()
	return nil
}

func (om *ServiceObsManager) handleWatcherEvent(event fsnotify.Event, depth int) error {
	path := event.Name

	switch {
	case event.Has(fsnotify.Create):
		return om.handleCreateEvent(path, depth)
	case event.Has(fsnotify.Write) && !event.Has(fsnotify.Remove):
		return om.handleWriteEvent(path)
	case event.Has(fsnotify.Remove):
		return om.handleRemoveEvent(path)
	}
	return nil
}

func (om *ServiceObsManager) handleCreateEvent(path string, depth int) error {
	stat, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("could not read observation file %s: %s", path, err)
	}
	// if a new directory was created, first start all observations already in it
	// and then start watching for changes in this directory (fsnotify.Watcher is not recursive)
	if stat.IsDir() && !strings.HasPrefix(stat.Name(), "..") {
		depth--
		err = filepath.WalkDir(path, om.startObservationsInDir())
		if err != nil {
			return fmt.Errorf("could not start observation from %s: %s", path, err)
		}
		if err := om.watchObservations(path, depth); err != nil {
			return fmt.Errorf("could not start watcher in directory %s: %s", path, err)
		}
	}
	// if not a directory and not a JSON, ignore
	if filepath.Ext(stat.Name()) != ".json" {
		return nil
	}

	// handle as a write event
	return om.handleWriteEvent(path)
}

func (om *ServiceObsManager) handleWriteEvent(path string) error {
	stat, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("could not read observation file %s: %s", path, err)
	}

	// if not a JSON, ignore
	if filepath.Ext(stat.Name()) != ".json" {
		return nil
	}

	obs, err := NewServiceObservationConfigFromFile(path)
	if err != nil {
		return fmt.Errorf("could not create observation from %s: %s", path, err)
	}

	return om.Set(obs)
}

func (om *ServiceObsManager) handleRemoveEvent(path string) error {
	var removeIDs []string

	om.Lock()
	if stopCh, ok := om.watcherStopChMap[path]; ok {
		// directory removed, delete all observations inside and cancel watching this dir
		prefix := strings.TrimSuffix(path, string(filepath.Separator)) + string(filepath.Separator)
		for id := range om.listenerMap {
			if strings.HasPrefix(id, prefix) {
				removeIDs = append(removeIDs, id)
			}
		}
		stopCh <- struct{}{}
	} else if _, ok := om.listenerMap[path]; ok {
		// JSON file exists
		removeIDs = append(removeIDs, path)
	}
	om.Unlock()

	var err error
	if len(removeIDs) > 0 {
		for _, removeID := range removeIDs {
			if removeErr := om.Delete(removeID); removeErr != nil {
				err = removeErr
			}
		}
	}

	return err
}

func (om *ServiceObsManager) start() {
	om.Lock()
	defer om.Unlock()
	if om.running {
		return
	}

	om.running = true
}

// IsRunning returns true if the observation manager is running or false if it is stopped
func (om *ServiceObsManager) IsRunning() bool {
	om.Lock()
	defer om.Unlock()
	return om.running
}

func (om *ServiceObsManager) stop() {
	om.Lock()
	defer om.Unlock()
	if !om.running {
		return
	}

	for _, obs := range om.listenerMap {
		obs.Stop()
	}
	om.listenerMap = map[string]*ServiceObsListener{}

	for _, watcherStopCh := range om.watcherStopChMap {
		watcherStopCh <- struct{}{}
	}
	om.watcherStopChMap = map[string]chan struct{}{}

	om.metrics.observationsGauge.Set(0)
	om.running = false
}

// ConfigMap returns a map of id:*ServiceObsConfig for all running observations.
func (om *ServiceObsManager) ConfigMap() map[string]*ServiceObsConfig {
	om.Lock()
	defer om.Unlock()

	obsMap := make(map[string]*ServiceObsConfig, len(om.listenerMap))
	for id, obs := range om.listenerMap {
		// copy value so that it can't be changed
		obsMap[id] = &ServiceObsConfig{}
		*obsMap[id] = *obs.config
	}

	return obsMap
}

// Set creates or updates an observation
// if an observation exists with the same ID, it is updated
// otherwise, a new observation is created
func (om *ServiceObsManager) Set(config *ServiceObsConfig) error {
	err := config.Validate()
	if err != nil {
		return err
	}

	{
		// copy value so that it can't be changed
		configCp := &ServiceObsConfig{}
		*configCp = *config
		config = configCp
	}

	om.Lock()
	if !om.running {
		om.Unlock()
		return fmt.Errorf("could not set observation for service: %s: observation manager is stopped", config.ServiceName)
	}

	existingObs, found := om.listenerMap[config.ID]
	om.Unlock()

	if found && reflect.DeepEqual(config, existingObs.config) {
		return nil
	}

	obs, err := newServiceObservationListener(config, om.sopts, om.metrics, om.reconnectCtr)
	if err != nil {
		return fmt.Errorf("could not update observation for service: %s: %s", config.ServiceName, err)
	}

	if err := obs.Start(); err != nil {
		return fmt.Errorf("could not start updated observation for service: %s: %s", config.ServiceName, err)
	}

	om.Lock()
	if !om.running {
		om.Unlock()
		obs.Stop()
		return fmt.Errorf("could not set observation for service: %s: observation manager is stopped", config.ServiceName)
	}

	om.listenerMap[config.ID] = obs
	om.Unlock()

	if found {
		existingObs.Stop()
	}
	return nil
}

// Delete deletes existing observations with provided ID
func (om *ServiceObsManager) Delete(id string) error {
	om.Lock()
	if !om.running {
		om.Unlock()
		return fmt.Errorf("could not delete observation id: %s: observation manager is stopped", id)
	}

	existingObs, found := om.listenerMap[id]
	if !found {
		om.Unlock()
		return fmt.Errorf("observation with given ID does not exist: %s", id)
	}

	delete(om.listenerMap, id)
	om.Unlock()

	existingObs.Stop()
	return nil
}
