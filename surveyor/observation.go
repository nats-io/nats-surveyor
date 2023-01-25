// Copyright 2019 The NATS Authors
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

// ServiceObsListener listens for observations from nats service latency checks
type ServiceObsListener struct {
	nc       *nats.Conn
	logger   *logrus.Logger
	opts     *ObservationConfig
	metrics  *ServiceObsMetrics
	sopts    *Options
	fromFile string
}

// ObservationConfig is used to set up new service observations.
type ObservationConfig struct {
	ServiceName string `json:"name"`
	Topic       string `json:"topic"`
	Credentials string `json:"credential"`
	Nkey        string `json:"nkey"`
}

func (o *ObservationConfig) Validate() error {
	errs := []string{}

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

// NewServiceObservationFromFile creates a new performance observation listener
func NewServiceObservationFromFile(f string, sopts Options, metrics *ServiceObsMetrics, reconnectCtr *prometheus.CounterVec) (*ServiceObsListener, error) {
	js, err := os.ReadFile(f)
	if err != nil {
		return nil, err
	}

	opts := &ObservationConfig{}
	err = json.Unmarshal(js, opts)
	if err != nil {
		return nil, fmt.Errorf("invalid service observation configuration: %s: %s", f, err)
	}
	err = opts.Validate()
	if err != nil {
		return nil, fmt.Errorf("invalid service observation configuration: %s: %s", f, err)
	}

	obs, err := newServiceObservation(*opts, sopts, metrics, reconnectCtr)
	if err != nil {
		return nil, err
	}

	obs.fromFile = f

	return obs, nil
}

func newServiceObservation(observationConfig ObservationConfig, sopts Options, metrics *ServiceObsMetrics, reconnectCtr *prometheus.CounterVec) (*ServiceObsListener, error) {
	err := observationConfig.Validate()
	if err != nil {
		return nil, fmt.Errorf("invalid service observation configuration: %s: %s", observationConfig.ServiceName, err)
	}

	sopts.Name = fmt.Sprintf("%s (observing %s)", sopts.Name, observationConfig.ServiceName)
	sopts.Credentials = observationConfig.Credentials
	sopts.Nkey = observationConfig.Nkey
	nc, err := connect(&sopts, reconnectCtr)
	if err != nil {
		return nil, fmt.Errorf("nats connection failed: %s", err)
	}

	return &ServiceObsListener{
		nc:      nc,
		logger:  sopts.Logger,
		opts:    &observationConfig,
		metrics: metrics,
		sopts:   &sopts,
	}, nil
}

func (s *Surveyor) startObservationsInDir() fs.WalkDirFunc {
	return func(path string, info fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if filepath.Ext(info.Name()) != ".json" {
			return nil
		}

		obs, err := NewServiceObservationFromFile(path, s.opts, s.observationMetrics, s.reconnectCtr)
		if err != nil {
			return fmt.Errorf("could not create observation from %s: %s", path, err)
		}

		// Prevent an equal observation to be loaded twice
		// This is a problem that occurs with k8s mounts
		for _, existingObservation := range s.observations {
			if obs.opts.ServiceName == existingObservation.opts.ServiceName {
				return nil
			}
		}

		err = obs.Start()
		if err != nil {
			return fmt.Errorf("could not start observation from %s: %s", path, err)
		}

		s.observations = append(s.observations, obs)

		return nil
	}
}

// Start starts listening for observations
func (o *ServiceObsListener) Start() error {
	_, err := o.nc.Subscribe(o.opts.Topic, o.observationHandler)
	if err != nil {
		return fmt.Errorf("could not subscribe to observation topic for %s (%s): %s", o.opts.ServiceName, o.opts.Topic, err)
	}
	err = o.nc.Flush()
	if err != nil {
		return err
	}

	o.metrics.observationsGauge.Inc()
	o.logger.Infof("Started observing stats on %s for %s", o.opts.Topic, o.opts.ServiceName)

	return nil
}

func (o *ServiceObsListener) observationHandler(m *nats.Msg) {
	kind, obs, err := jsm.ParseEvent(m.Data)
	if err != nil {
		o.metrics.invalidObservationsReceived.WithLabelValues(o.opts.ServiceName).Inc()
		o.logger.Warnf("data: %s", m.Data)
		o.logger.Warnf("Unparsable observation received on %s: %s", o.opts.Topic, err)
		return
	}

	switch obs := obs.(type) {
	case *metric.ServiceLatencyV1:
		o.metrics.observationsReceived.WithLabelValues(o.opts.ServiceName, obs.Responder.Name).Inc()
		o.metrics.serviceLatency.WithLabelValues(o.opts.ServiceName, obs.Responder.Name).Observe(obs.ServiceLatency.Seconds())
		o.metrics.totalLatency.WithLabelValues(o.opts.ServiceName, obs.Responder.Name).Observe(obs.TotalLatency.Seconds())
		o.metrics.requestorRTT.WithLabelValues(o.opts.ServiceName, obs.Responder.Name).Observe(obs.Requestor.RTT.Seconds())
		o.metrics.responderRTT.WithLabelValues(o.opts.ServiceName, obs.Responder.Name).Observe(obs.Responder.RTT.Seconds())
		o.metrics.systemRTT.WithLabelValues(o.opts.ServiceName, obs.Responder.Name).Observe(obs.SystemLatency.Seconds())

		if obs.Status == 0 {
			o.metrics.serviceRequestStatus.WithLabelValues(o.opts.ServiceName, "500").Inc()
		} else {
			o.metrics.serviceRequestStatus.WithLabelValues(o.opts.ServiceName, strconv.Itoa(obs.Status)).Inc()
		}

	default:
		o.metrics.invalidObservationsReceived.WithLabelValues(o.opts.ServiceName).Inc()
		o.logger.Warnf("Unsupported observation received on %s: %s", o.opts.Topic, kind)
		return
	}
}

// Stop closes the connection to the network
func (o *ServiceObsListener) Stop() {
	o.metrics.observationsGauge.Dec()
	o.nc.Close()
}

func (s *Surveyor) watchObservations(dir string, depth int) error {
	if depth == 0 {
		return fmt.Errorf("exceeded observation dir max depth")
	}
	if dir == "" {
		return nil
	}

	go func() {
		s.Mutex.Lock()
		if _, ok := s.observationWatchers[dir]; ok {
			return
		}
		watcher, err := fsnotify.NewWatcher()
		if err != nil {
			s.logger.Errorf("error creating watcher: %s", err)
			s.Mutex.Unlock()
			return
		}

		if err := watcher.Add(dir); err != nil {
			s.logger.Errorf("error adding dir to watcher: %s", err)
			s.Mutex.Unlock()
			return
		}
		defer watcher.Close()
		s.observationWatchers[dir] = struct{}{}
		s.Mutex.Unlock()
		s.logger.Debugf("starting listener goroutine for %s", dir)
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if err := s.handleWatcherEvent(event, depth); err != nil {
					s.logger.Warn(err)
				}
			case <-s.stop:
				return
			}
		}
	}()
	return nil
}

func (s *Surveyor) handleWatcherEvent(event fsnotify.Event, depth int) error {
	path := event.Name
	s.Lock()
	defer s.Unlock()

	switch {
	case event.Has(fsnotify.Create):
		fs, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("could not read observation file %s: %s", path, err)
		}
		// if a new directory was created, first start all observation already in it
		// and then start watchong for changes in this directory (fsnotify.Watcher is not recursive)
		if fs.IsDir() && fs.Name() != "." {
			depth--
			err = filepath.WalkDir(path, s.startObservationsInDir())
			if err != nil {
				return fmt.Errorf("could not start observation from %s: %s", path, err)
			}
			if err := s.watchObservations(path, depth); err != nil {
				return fmt.Errorf("could not start watcher in directory %s: %s", path, err)
			}
		}
		// if not a directory and not a JSON, ignore
		if filepath.Ext(fs.Name()) != ".json" {
			return nil
		}

		// create new observation from json
		obs, err := NewServiceObservationFromFile(path, s.opts, s.observationMetrics, s.reconnectCtr)
		if err != nil {
			return fmt.Errorf("could not create observation from %s: %s", path, err)
		}
		for _, existingObservation := range s.observations {
			if obs.opts.ServiceName == existingObservation.opts.ServiceName {
				return nil
			}
		}

		err = obs.Start()
		if err != nil {
			return fmt.Errorf("could not start observation from %s: %s", path, err)
		}

		s.observations = append(s.observations, obs)
	case event.Has(fsnotify.Write) && !event.Has(fsnotify.Remove):
		fs, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("could not read observation file %s: %s", path, err)
		}
		// if not a JSON, ignore
		if filepath.Ext(fs.Name()) != ".json" {
			return nil
		}
		obs, err := NewServiceObservationFromFile(path, s.opts, s.observationMetrics, s.reconnectCtr)
		for _, existingObservation := range s.observations {
			// ignore service if it already exists
			if obs.opts.ServiceName == existingObservation.opts.ServiceName {
				return fmt.Errorf("service observation with provided service name already exists: %s", obs.opts.ServiceName)
			}
		}
		if err != nil {
			return fmt.Errorf("could not create observation from %s: %s", path, err)
		}

		err = obs.Start()
		if err != nil {
			return fmt.Errorf("could not start observation from %s: %s", path, err)
		}

		s.observations = append(s.observations, obs)

	case event.Has(fsnotify.Remove):
		// directory removed, delete all observations inside and cancel watching this dir
		if _, ok := s.observationWatchers[path]; ok {
			for i := 0; ; i++ {
				if i > len(s.observations)-1 {
					break
				}
				if strings.HasPrefix(s.observations[i].fromFile, path) {
					s.observations = removeObservation(s.observations, i)
					i--
				}
			}
			delete(s.observationWatchers, path)
			return nil
		}
		// if not a directory and not a JSON, ignore
		if filepath.Ext(path) != ".json" {
			return nil
		}
		for i := 0; ; i++ {
			if i > len(s.observations)-1 {
				break
			}
			if s.observations[i].fromFile == path {
				s.observations = removeObservation(s.observations, i)
				i--
			}
		}
	}
	return nil
}

func removeObservation(observations []*ServiceObsListener, i int) []*ServiceObsListener {
	if i >= len(observations) {
		return observations
	}
	observations[i].Stop()
	if i < len(observations)-1 {
		observations = append(observations[:i], observations[i+1:]...)
	} else {
		observations = observations[:i]
	}
	return observations
}

type ManageObservationsOpt func(*ObservationsManager) error

type ObservationErrHandler func(string, error)

type ObservationsManager struct {
	surveyor            *Surveyor
	addObservations     chan ObservationConfig
	deleteObseravations chan string
	updateObservations  chan UpdateObservation
	errHandler          ObservationErrHandler
}

type UpdateObservation struct {
	Name   string
	Config ObservationConfig
}

// ObservationsError is a configuration option for [ManageObservations].
// It sets error handler invoked when adding/deleting observations fails.
func ObservationsError(handler ObservationErrHandler) ManageObservationsOpt {
	return func(om *ObservationsManager) error {
		om.errHandler = handler
		return nil
	}
}

// ManageObservations creates an ObservationManager, allowing for adding/deleting service observations to the surveyor.
//
// ManageObservationOpts can be supplied to configure ObservationsManager.
func (s *Surveyor) ManageObservations(opts ...ManageObservationsOpt) (*ObservationsManager, error) {
	obsManager := &ObservationsManager{
		surveyor:            s,
		addObservations:     make(chan ObservationConfig, 100),
		updateObservations:  make(chan UpdateObservation, 100),
		deleteObseravations: make(chan string, 100),
	}
	for _, opt := range opts {
		if err := opt(obsManager); err != nil {
			return nil, err
		}
	}
	go func() {
		for {
			select {
			case obsConfig := <-obsManager.addObservations:
				if err := obsManager.addObservation(obsConfig); err != nil {
					s.logger.Warnf("adding service observation: %s", err)
				}
			case updateObservation := <-obsManager.updateObservations:
				if err := obsManager.updateObservation(updateObservation); err != nil {
					s.logger.Warnf("updating service observation: %s", err)
				}
			case serviceName := <-obsManager.deleteObseravations:
				if err := obsManager.deleteObservation(serviceName); err != nil {
					s.logger.Warnf("deleting service observation: %s", err)
				}
			case <-s.stop:
				return
			}
		}
	}()
	return obsManager, nil
}

func (om *ObservationsManager) addObservation(obsConfig ObservationConfig) error {
	om.surveyor.Lock()
	defer om.surveyor.Unlock()
	for _, existingObservation := range om.surveyor.observations {
		if obsConfig.ServiceName == existingObservation.opts.ServiceName {
			return fmt.Errorf("observation with given service name already exists: %s", obsConfig.ServiceName)
		}
	}
	obs, err := newServiceObservation(obsConfig, om.surveyor.opts, om.surveyor.observationMetrics, om.surveyor.reconnectCtr)
	if err != nil {
		if om.errHandler != nil {
			om.errHandler(obsConfig.ServiceName, err)
		}
		return fmt.Errorf("could not create observation from config: %s: %s", obsConfig.ServiceName, err)
	}

	if err := obs.Start(); err != nil {
		if om.errHandler != nil {
			om.errHandler(obsConfig.ServiceName, err)
		}
		return fmt.Errorf("could not start observation for service: %s: %s", obsConfig.ServiceName, err)
	}

	om.surveyor.observations = append(om.surveyor.observations, obs)
	return nil
}

func (om *ObservationsManager) updateObservation(updateObservation UpdateObservation) error {
	om.surveyor.Lock()
	defer om.surveyor.Unlock()
	var found bool
	var obsIndex int
	for i, existingObservation := range om.surveyor.observations {
		if updateObservation.Name == existingObservation.opts.ServiceName {
			found = true
			obsIndex = i
			break
		}
	}
	if !found {
		return fmt.Errorf("observation with provided name does not exist: %s", updateObservation.Name)
	}
	obs, err := newServiceObservation(updateObservation.Config, om.surveyor.opts, om.surveyor.observationMetrics, om.surveyor.reconnectCtr)
	if err != nil {
		if om.errHandler != nil {
			om.errHandler(updateObservation.Config.ServiceName, err)
		}
		return fmt.Errorf("could not create observation from config: %s: %s", updateObservation.Config.ServiceName, err)
	}
	if err := obs.Start(); err != nil {
		if om.errHandler != nil {
			om.errHandler(updateObservation.Config.ServiceName, err)
		}
		return fmt.Errorf("could not start observation for service: %s: %s", updateObservation.Config.ServiceName, err)
	}

	om.surveyor.observations[obsIndex].Stop()
	om.surveyor.observations[obsIndex] = obs
	return nil
}

func (om *ObservationsManager) deleteObservation(serviceName string) error {
	om.surveyor.Lock()
	defer om.surveyor.Unlock()
	var found bool
	for i, existingObservation := range om.surveyor.observations {
		if serviceName == existingObservation.opts.ServiceName {
			found = true
			existingObservation.Stop()
			if i < len(om.surveyor.observations)-1 {
				om.surveyor.observations = append(om.surveyor.observations[:i], om.surveyor.observations[i+1:]...)
			} else {
				om.surveyor.observations = om.surveyor.observations[:i]
			}
		}
	}
	if !found {
		return fmt.Errorf("observation with given service name does not exist: %s", serviceName)
	}
	return nil
}

// AddObservations creates and starts new service observations.
// If service observation with given name already exists, it will not be updated.
func (om *ObservationsManager) AddObservations(observations ...ObservationConfig) {
	for _, obsConfig := range observations {
		om.addObservations <- obsConfig
	}
}

// DeleteObservations deletes exisiting observations with provided service names.
func (om *ObservationsManager) DeleteObservations(serviceNames ...string) {
	for _, serviceName := range serviceNames {
		om.deleteObseravations <- serviceName
	}
}

// UpdateObservations updates exisiting observations.
// Service observation with provided name has to exist for the update to succeed.
func (om *ObservationsManager) UpdateObservations(observations ...UpdateObservation) {
	for _, obsUpdate := range observations {
		om.updateObservations <- obsUpdate
	}
}

// GetObservations returns configs of all running service observations.
func (om *ObservationsManager) GetObservations() []ObservationConfig {
	om.surveyor.Lock()
	defer om.surveyor.Unlock()
	observations := make([]ObservationConfig, 0, len(om.surveyor.observations))
	for _, obs := range om.surveyor.observations {
		observations = append(observations, *obs.opts)
	}
	return observations
}
