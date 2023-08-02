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
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/nats-io/nats.go"

	"github.com/fsnotify/fsnotify"
	"github.com/nats-io/jsm.go"
	"github.com/nats-io/jsm.go/api/server/metric"
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
		}, []string{"service", "app", "source_account"}),

		serviceRequestStatus: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name:        prometheus.BuildFQName("nats", "latency", "observation_status_count"),
			Help:        "The status result codes for requests to a service",
			ConstLabels: constLabels,
		}, []string{"service", "status", "source_account"}),

		invalidObservationsReceived: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name:        prometheus.BuildFQName("nats", "latency", "observation_error_count"),
			Help:        "Number of observations received by this surveyor across all services that could not be handled",
			ConstLabels: constLabels,
		}, []string{"service", "source_account"}),

		serviceLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:        prometheus.BuildFQName("nats", "latency", "service_duration"),
			Help:        "Time spent serving the request in the service",
			ConstLabels: constLabels,
		}, []string{"service", "app", "source_account"}),

		totalLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:        prometheus.BuildFQName("nats", "latency", "total_duration"),
			Help:        "Total time spent serving a service including network overheads",
			ConstLabels: constLabels,
		}, []string{"service", "app", "source_account"}),

		requestorRTT: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:        prometheus.BuildFQName("nats", "latency", "requestor_rtt"),
			Help:        "The RTT to the client making a request",
			ConstLabels: constLabels,
		}, []string{"service", "app", "source_account"}),

		responderRTT: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:        prometheus.BuildFQName("nats", "latency", "responder_rtt"),
			Help:        "The RTT to the service serving the request",
			ConstLabels: constLabels,
		}, []string{"service", "app", "source_account"}),

		systemRTT: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:        prometheus.BuildFQName("nats", "latency", "system_rtt"),
			Help:        "The RTT within the NATS system - time traveling clusters, gateways and leaf nodes",
			ConstLabels: constLabels,
		}, []string{"service", "app", "source_account"}),
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

// ServiceObsConfig is used to configure service observations
type ServiceObsConfig struct {
	// unique identifier
	ID string `json:"id"`

	// service settings
	ServiceName string `json:"name"`
	Topic       string `json:"topic"`

	// account name position in subject, used when aggregating services across multiple accounts
	ExternalAccountTokenPosition int `json:"external_account_token_position"`
	// optional service name position in subject, useful when aggregating services across multiple accounts
	ExternalServiceNamePosition int `json:"external_service_name_position"`

	// connection options
	JWT         string `json:"jwt"`
	Seed        string `json:"seed"`
	Credentials string `json:"credential"`
	Nkey        string `json:"nkey"`
	Token       string `json:"token"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	TLSCA       string `json:"tls_ca"`
	TLSCert     string `json:"tls_cert"`
	TLSKey      string `json:"tls_key"`

	// tls.Config cannot be provided in observation config file,
	// only programmatically
	TLSConfig *tls.Config `json:"-"`
}

// Validate is used to validate a ServiceObsConfig
func (o *ServiceObsConfig) Validate() error {
	if o == nil {
		return fmt.Errorf("service observation config cannot be nil")
	}

	var errs []string
	if o.ID == "" {
		errs = append(errs, "id is required")
	}

	if o.ServiceName == "" {
		errs = append(errs, "name is required")
	}

	if o.Topic == "" {
		errs = append(errs, "topic is required")
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
	cp.TLSConfig = o.TLSConfig.Clone()
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

// serviceObsListener listens for observations from nats service latency checks
type serviceObsListener struct {
	sync.Mutex
	config  *ServiceObsConfig
	cp      *natsConnPool
	logger  *logrus.Logger
	metrics *ServiceObsMetrics
	pc      *pooledNatsConn
	sub     *nats.Subscription
}

func newServiceObservationListener(config *ServiceObsConfig, cp *natsConnPool, logger *logrus.Logger, metrics *ServiceObsMetrics) (*serviceObsListener, error) {
	err := config.Validate()
	if err != nil {
		return nil, fmt.Errorf("invalid service observation config for id: %s, service name: %s, error: %v", config.ID, config.ServiceName, err)
	}

	return &serviceObsListener{
		config:  config,
		cp:      cp,
		logger:  logger,
		metrics: metrics,
	}, nil
}

func (o *serviceObsListener) natsContext() *natsContext {
	natsCtx := &natsContext{
		JWT:         o.config.JWT,
		Seed:        o.config.Seed,
		Credentials: o.config.Credentials,
		Nkey:        o.config.Nkey,
		Token:       o.config.Token,
		Username:    o.config.Username,
		Password:    o.config.Password,
		TLSCA:       o.config.TLSCA,
		TLSCert:     o.config.TLSCert,
		TLSKey:      o.config.TLSKey,
		TLSConfig:   o.config.TLSConfig,
	}

	// legacy Credentials field
	if natsCtx.Credentials == "" && o.config.Credentials != "" {
		natsCtx.Credentials = o.config.Credentials
		o.logger.Warnf("deprecated service observation config field 'credential', use 'creds' instead for id: %s, service name: %s", o.config.ID, o.config.ServiceName)
	}
	return natsCtx
}

// Start starts listening for observations
func (o *serviceObsListener) Start() error {
	o.Lock()
	defer o.Unlock()
	if o.pc != nil {
		// already started
		return nil
	}

	pc, err := o.cp.Get(o.natsContext())
	if err != nil {
		return fmt.Errorf("nats connection failed for id: %s, service name: %s, error: %v", o.config.ID, o.config.ServiceName, err)
	}

	sub, err := pc.nc.Subscribe(o.config.Topic, o.observationHandler)
	if err != nil {
		pc.ReturnToPool()
		return fmt.Errorf("could not subscribe to service observation topic for id: %s, service name: %s, topic: %s, error: %v", o.config.ID, o.config.ServiceName, o.config.Topic, err)
	}

	o.pc = pc
	o.sub = sub
	o.metrics.observationsGauge.Inc()
	o.logger.Infof("started service observation for id: %s, service name: %s, topic: %s", o.config.ID, o.config.ServiceName, o.config.Topic)
	return nil
}

func (o *serviceObsListener) observationHandler(m *nats.Msg) {
	serviceName := o.config.ServiceName
	var accountName string
	var err error
	if o.config.ExternalServiceNamePosition != 0 {
		serviceName, err = getTokenFromSubject(m.Subject, o.config.ExternalServiceNamePosition)
		if err != nil {
			o.metrics.invalidObservationsReceived.WithLabelValues(o.config.ServiceName, accountName).Inc()
			o.logger.Warnf("invalid service observation subject received for id: %s, service name: %s, error: %v, subject: %s", o.config.ID, o.config.ServiceName, err, m.Subject)
			return
		}
	}
	if o.config.ExternalAccountTokenPosition != 0 {
		accountName, err = getTokenFromSubject(m.Subject, o.config.ExternalAccountTokenPosition)
		if err != nil {
			o.metrics.invalidObservationsReceived.WithLabelValues(o.config.ServiceName, accountName).Inc()
			o.logger.Warnf("invalid service observation subject received for id: %s, service name: %s, error: %v, subject: %s", o.config.ID, o.config.ServiceName, err, m.Subject)
			return
		}
	}
	kind, obs, err := jsm.ParseEvent(m.Data)
	if err != nil {
		o.metrics.invalidObservationsReceived.WithLabelValues(serviceName, accountName).Inc()
		o.logger.Warnf("unparsable service observation received for id: %s, service name: %s, error: %v, data: %q", o.config.ID, serviceName, err, m.Data)
		return
	}

	switch obs := obs.(type) {
	case *metric.ServiceLatencyV1:
		o.metrics.observationsReceived.WithLabelValues(serviceName, obs.Responder.Name, accountName).Inc()
		o.metrics.serviceLatency.WithLabelValues(serviceName, obs.Responder.Name, accountName).Observe(obs.ServiceLatency.Seconds())
		o.metrics.totalLatency.WithLabelValues(serviceName, obs.Responder.Name, accountName).Observe(obs.TotalLatency.Seconds())
		o.metrics.requestorRTT.WithLabelValues(serviceName, obs.Responder.Name, accountName).Observe(obs.Requestor.RTT.Seconds())
		o.metrics.responderRTT.WithLabelValues(serviceName, obs.Responder.Name, accountName).Observe(obs.Responder.RTT.Seconds())
		o.metrics.systemRTT.WithLabelValues(serviceName, obs.Responder.Name, accountName).Observe(obs.SystemLatency.Seconds())

		if obs.Status == 0 {
			o.metrics.serviceRequestStatus.WithLabelValues(serviceName, "500", accountName).Inc()
		} else {
			o.metrics.serviceRequestStatus.WithLabelValues(serviceName, strconv.Itoa(obs.Status), accountName).Inc()
		}

	default:
		o.metrics.invalidObservationsReceived.WithLabelValues(serviceName, accountName).Inc()
		o.logger.Warnf("unsupported service observation received for id: %s, service name: %s, kind: %s", o.config.ID, serviceName, kind)
		return
	}
}

func getTokenFromSubject(subject string, token int) (string, error) {
	parts := strings.Split(subject, ".")
	if token-1 > len(parts) {
		return "", fmt.Errorf("invalid subject: %q: expected service name on token position %d", subject, token)
	}
	return parts[token-1], nil
}

// Stop stops listening for observations
func (o *serviceObsListener) Stop() {
	o.Lock()
	defer o.Unlock()
	if o.pc == nil {
		// already stopped
		return
	}

	if o.sub != nil {
		_ = o.sub.Unsubscribe()
		o.sub = nil
	}

	o.metrics.observationsGauge.Dec()
	o.pc.ReturnToPool()
	o.pc = nil
}

// ServiceObsManager exposes methods to operate on service observations
type ServiceObsManager struct {
	sync.Mutex
	cp          *natsConnPool
	listenerMap map[string]*serviceObsListener
	logger      *logrus.Logger
	metrics     *ServiceObsMetrics
}

// newServiceObservationManager creates a ServiceObsManager for managing Service Observations
func newServiceObservationManager(cp *natsConnPool, logger *logrus.Logger, metrics *ServiceObsMetrics) *ServiceObsManager {
	return &ServiceObsManager{
		cp:      cp,
		logger:  logger,
		metrics: metrics,
	}
}

func (om *ServiceObsManager) start() {
	om.Lock()
	defer om.Unlock()
	if om.listenerMap != nil {
		// already started
		return
	}

	om.listenerMap = map[string]*serviceObsListener{}
}

// IsRunning returns true if the observation manager is running or false if it is stopped
func (om *ServiceObsManager) IsRunning() bool {
	om.Lock()
	defer om.Unlock()
	return om.listenerMap != nil
}

func (om *ServiceObsManager) stop() {
	om.Lock()
	defer om.Unlock()
	if om.listenerMap == nil {
		// already stopped
		return
	}

	for _, obs := range om.listenerMap {
		obs.Stop()
	}
	om.metrics.observationsGauge.Set(0)
	om.listenerMap = nil
}

// ConfigMap returns a map of id:*ServiceObsConfig for all running observations
func (om *ServiceObsManager) ConfigMap() map[string]*ServiceObsConfig {
	om.Lock()
	defer om.Unlock()

	obsMap := make(map[string]*ServiceObsConfig, len(om.listenerMap))
	if om.listenerMap == nil {
		return obsMap
	}

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
	if om.listenerMap == nil {
		om.Unlock()
		return fmt.Errorf("observation manager is stopped; could not set observation for id: %s, service name: %s", config.ID, config.ServiceName)
	}

	existingObs, found := om.listenerMap[config.ID]
	om.Unlock()

	if found && *config == *existingObs.config {
		return nil
	}

	obs, err := newServiceObservationListener(config, om.cp, om.logger, om.metrics)
	if err != nil {
		return fmt.Errorf("could not set observation for id: %s, service name: %s, error: %v", config.ID, config.ServiceName, err)
	}

	if err := obs.Start(); err != nil {
		return fmt.Errorf("could not start observation for id: %s, service name: %s, error: %v", config.ID, config.ServiceName, err)
	}

	om.Lock()
	if om.listenerMap == nil {
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
	if om.listenerMap == nil {
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

type serviceObsFSWatcher struct {
	sync.Mutex
	logger  *logrus.Logger
	om      *ServiceObsManager
	stopCh  chan struct{}
	watcher *fsnotify.Watcher
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
