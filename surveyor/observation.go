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

func (o *ServiceObsConfig) copy() *ServiceObsConfig {
	if o == nil {
		return nil
	}
	cp := *o
	return &cp
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

func newServiceObservationListener(config *ServiceObsConfig, sopts Options, metrics *ServiceObsMetrics, reconnectCtr *prometheus.CounterVec) (*ServiceObsListener, error) {
	err := config.Validate()
	if err != nil {
		return nil, fmt.Errorf("invalid service observation config for id: %s, service name: %s, error: %v", config.ID, config.ServiceName, err)
	}

	sopts.Name = fmt.Sprintf("%s (observing %s)", sopts.Name, config.ServiceName)
	sopts.Credentials = config.Credentials
	sopts.Nkey = config.Nkey
	nc, err := connect(&sopts, reconnectCtr)
	if err != nil {
		return nil, fmt.Errorf("nats connection failed: %s", err)
	}

	return &ServiceObsListener{
		nc:      nc,
		logger:  sopts.Logger,
		config:  config,
		metrics: metrics,
		sopts:   &sopts,
	}, nil
}

// Start starts listening for observations.
func (o *ServiceObsListener) Start() error {
	_, err := o.nc.Subscribe(o.config.Topic, o.observationHandler)
	if err != nil {
		return fmt.Errorf("could not subscribe to service observation topic for id: %s, service name: %s, topic: %s, error: %v", o.config.ID, o.config.ServiceName, o.config.Topic, err)
	}
	err = o.nc.Flush()
	if err != nil {
		return err
	}

	o.metrics.observationsGauge.Inc()
	o.logger.Infof("started service observation for id: %s, service name: %s, topic: %s", o.config.ID, o.config.ServiceName, o.config.Topic)

	return nil
}

func (o *ServiceObsListener) observationHandler(m *nats.Msg) {
	kind, obs, err := jsm.ParseEvent(m.Data)
	if err != nil {
		o.metrics.invalidObservationsReceived.WithLabelValues(o.config.ServiceName).Inc()
		o.logger.Warnf("unparsable service observation received for id: %s, service name: %s, subject: %s, error: %v, data: %q", o.config.ID, o.config.ServiceName, m.Subject, err, m.Data)
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
		o.logger.Warnf("unsupported service observation received for id: %s, service name: %s, subject: %s, kind: %s", o.config.ID, o.config.ServiceName, m.Subject, kind)
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

// newServiceObservationManager creates a ServiceObsManager, allowing for adding/deleting service observations to the surveyor.
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
		// copy so that internal references cannot be changed
		obsMap[id] = obs.config.copy()
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

	// copy so that internal references cannot be changed
	config = config.copy()

	om.Lock()
	if !om.running {
		om.Unlock()
		return fmt.Errorf("observation manager is stopped; could not set observation for id: %s, service name: %s", config.ID, config.ServiceName)
	}

	existingObs, found := om.listenerMap[config.ID]
	om.Unlock()

	if found && *config == *existingObs.config {
		return nil
	}

	obs, err := newServiceObservationListener(config, om.sopts, om.metrics, om.reconnectCtr)
	if err != nil {
		return fmt.Errorf("could not set observation for id: %s, service name: %s, error: %v", config.ID, config.ServiceName, err)
	}

	if err := obs.Start(); err != nil {
		return fmt.Errorf("could not start observation for id: %s, service name: %s, error: %v", config.ID, config.ServiceName, err)
	}

	om.Lock()
	if !om.running {
		om.Unlock()
		obs.Stop()
		return fmt.Errorf("observation manager is stopped; could not set observation for id: %s, service name: %s", config.ID, config.ServiceName)
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
		return fmt.Errorf("observation manager is stopped; could not delete observation id: %s", id)
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

type serviceObsFSWatcher struct {
	sync.Mutex
	logger  *logrus.Logger
	om      *ServiceObsManager
	watcher *fsnotify.Watcher
	stopCh  chan struct{}
}

func newServiceObservationFSWatcher(logger *logrus.Logger, om *ServiceObsManager) *serviceObsFSWatcher {
	return &serviceObsFSWatcher{
		logger: logger,
		om:     om,
	}
}

func (w *serviceObsFSWatcher) start() error {
	w.Lock()
	defer w.Unlock()
	if w.watcher != nil {
		return nil
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	stopCh := make(chan struct{}, 1)

	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if err := w.handleWatcherEvent(event); err != nil {
					w.logger.Warn(err)
				}
			case <-stopCh:
				return
			}
		}
	}()

	w.watcher = watcher
	w.stopCh = stopCh
	return nil
}

func (w *serviceObsFSWatcher) stop() {
	w.Lock()
	defer w.Unlock()

	if w.watcher == nil {
		return
	}

	w.stopCh <- struct{}{}
	_ = w.watcher.Close()
	w.watcher = nil
}

func (w *serviceObsFSWatcher) startObservationsInDir() fs.WalkDirFunc {
	return func(path string, info fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// skip directories starting with '..'
		// this prevents double observation loading when using kubernetes mounts
		if info.IsDir() && strings.HasPrefix(info.Name(), "..") {
			return filepath.SkipDir
		}

		if info.IsDir() {
			w.Lock()
			if w.watcher != nil {
				_ = w.watcher.Add(path)
			}
			w.Unlock()
		}

		if filepath.Ext(info.Name()) != ".json" {
			return nil
		}

		obs, err := NewServiceObservationConfigFromFile(path)
		if err != nil {
			return fmt.Errorf("could not create observation from path: %s, error: %v", path, err)
		}

		return w.om.Set(obs)
	}
}

func (w *serviceObsFSWatcher) handleWatcherEvent(event fsnotify.Event) error {
	path := event.Name

	switch {
	case event.Has(fsnotify.Create):
		return w.handleCreateEvent(path)
	case event.Has(fsnotify.Write) && !event.Has(fsnotify.Remove):
		return w.handleWriteEvent(path)
	case event.Has(fsnotify.Remove):
		return w.handleRemoveEvent(path)
	}
	return nil
}

func (w *serviceObsFSWatcher) handleCreateEvent(path string) error {
	stat, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("could not stat observation path: %s, error: %v", path, err)
	}

	// if a new directory was created, start observations in dir
	if stat.IsDir() {
		return filepath.WalkDir(path, w.startObservationsInDir())
	}

	// if not a directory and not a JSON, ignore
	if filepath.Ext(stat.Name()) != ".json" {
		return nil
	}

	// handle as a write event
	return w.handleWriteEvent(path)
}

func (w *serviceObsFSWatcher) handleWriteEvent(path string) error {
	stat, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("could not stat observation path: %s, error: %v", path, err)
	}

	// if not a JSON file, ignore
	if stat.IsDir() || filepath.Ext(stat.Name()) != ".json" {
		return nil
	}

	config, err := NewServiceObservationConfigFromFile(path)
	if err != nil {
		return fmt.Errorf("could not create observation from path: %s, error: %v", path, err)
	}

	return w.om.Set(config)
}

func (w *serviceObsFSWatcher) handleRemoveEvent(path string) error {
	var removeIDs []string
	configMap := w.om.ConfigMap()
	dir := strings.TrimSuffix(path, string(filepath.Separator)) + string(filepath.Separator)
	for id := range configMap {
		if id == path {
			// file
			removeIDs = append(removeIDs, id)
		} else if strings.HasPrefix(id, dir) {
			// directory
			removeIDs = append(removeIDs, id)
		}
	}

	var err error
	if len(removeIDs) > 0 {
		for _, removeID := range removeIDs {
			if removeErr := w.om.Delete(removeID); removeErr != nil {
				err = removeErr
			}
		}
	}

	return err
}
