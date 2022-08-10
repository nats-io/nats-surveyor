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
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/nats-io/jsm.go"
	"github.com/nats-io/jsm.go/api/server/metric"
	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus"
)

// ServiceObsListener listens for observations from nats service latency checks
type ServiceObsListener struct {
	nc    *nats.Conn
	opts  *serviceObsOptions
	sopts *Options
}

type serviceObsOptions struct {
	ServiceName string `json:"name"`
	Topic       string `json:"topic"`
	Credentials string `json:"credential"`
	Nkey        string `json:"nkey"`
}

func (o *serviceObsOptions) Validate() error {
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

var (
	observationsGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: prometheus.BuildFQName("nats", "latency", "observations_count"),
		Help: "Number of Service Latency listeners that are running",
	})

	observationsReceived = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName("nats", "latency", "observations_received_count"),
		Help: "Number of observations received by this surveyor across all services",
	}, []string{"service", "app"})

	serviceRequestStatus = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName("nats", "latency", "observation_status_count"),
		Help: "The status result codes for requests to a service",
	}, []string{"service", "status"})

	invalidObservationsReceived = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName("nats", "latency", "observation_error_count"),
		Help: "Number of observations received by this surveyor across all services that could not be handled",
	}, []string{"service"})

	serviceLatency = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: prometheus.BuildFQName("nats", "latency", "service_duration"),
		Help: "Time spent serving the request in the service",
	}, []string{"service", "app"})

	totalLatency = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: prometheus.BuildFQName("nats", "latency", "total_duration"),
		Help: "Total time spent serving a service including network overheads",
	}, []string{"service", "app"})

	requestorRTT = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: prometheus.BuildFQName("nats", "latency", "requestor_rtt"),
		Help: "The RTT to the client making a request",
	}, []string{"service", "app"})

	responderRTT = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: prometheus.BuildFQName("nats", "latency", "responder_rtt"),
		Help: "The RTT to the service serving the request",
	}, []string{"service", "app"})

	systemRTT = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: prometheus.BuildFQName("nats", "latency", "system_rtt"),
		Help: "The RTT within the NATS system - time traveling clusters, gateways and leaf nodes",
	}, []string{"service", "app"})
)

func init() {
	prometheus.MustRegister(invalidObservationsReceived)
	prometheus.MustRegister(observationsReceived)
	prometheus.MustRegister(serviceRequestStatus)
	prometheus.MustRegister(serviceLatency)
	prometheus.MustRegister(totalLatency)
	prometheus.MustRegister(requestorRTT)
	prometheus.MustRegister(responderRTT)
	prometheus.MustRegister(systemRTT)
	prometheus.MustRegister(observationsGauge)
}

// NewServiceObservation creates a new performance observation listener
func NewServiceObservation(f string, sopts Options) (*ServiceObsListener, error) {
	js, err := os.ReadFile(f)
	if err != nil {
		return nil, err
	}

	opts := &serviceObsOptions{}
	err = json.Unmarshal(js, opts)
	if err != nil {
		return nil, fmt.Errorf("invalid service observation configuration: %s: %s", f, err)
	}

	err = opts.Validate()
	if err != nil {
		return nil, fmt.Errorf("invalid service observation configuration: %s: %s", f, err)
	}

	sopts.Name = fmt.Sprintf("%s (observing %s)", sopts.Name, opts.ServiceName)
	sopts.Credentials = opts.Credentials
	sopts.Nkey = opts.Nkey
	nc, err := connect(&sopts)
	if err != nil {
		return nil, fmt.Errorf("nats connection failed: %s", err)
	}

	return &ServiceObsListener{
		nc:    nc,
		opts:  opts,
		sopts: &sopts,
	}, nil
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

	observationsGauge.Inc()
	log.Printf("Started observing stats on %s for %s", o.opts.Topic, o.opts.ServiceName)

	return nil
}

func (o *ServiceObsListener) observationHandler(m *nats.Msg) {
	kind, obs, err := jsm.ParseEvent(m.Data)
	if err != nil {
		invalidObservationsReceived.WithLabelValues(o.opts.ServiceName).Inc()
		log.Printf("Unparsable observation received on %s: %s", o.opts.Topic, err)
		return
	}

	switch obs := obs.(type) {
	case *metric.ServiceLatencyV1:
		observationsReceived.WithLabelValues(o.opts.ServiceName, obs.Responder.Name).Inc()
		serviceLatency.WithLabelValues(o.opts.ServiceName, obs.Responder.Name).Observe(obs.ServiceLatency.Seconds())
		totalLatency.WithLabelValues(o.opts.ServiceName, obs.Responder.Name).Observe(obs.TotalLatency.Seconds())
		requestorRTT.WithLabelValues(o.opts.ServiceName, obs.Responder.Name).Observe(obs.Requestor.RTT.Seconds())
		responderRTT.WithLabelValues(o.opts.ServiceName, obs.Responder.Name).Observe(obs.Responder.RTT.Seconds())
		systemRTT.WithLabelValues(o.opts.ServiceName, obs.Responder.Name).Observe(obs.SystemLatency.Seconds())

		if obs.Status == 0 {
			serviceRequestStatus.WithLabelValues(o.opts.ServiceName, "500").Inc()
		} else {
			serviceRequestStatus.WithLabelValues(o.opts.ServiceName, strconv.Itoa(obs.Status)).Inc()
		}

	default:
		invalidObservationsReceived.WithLabelValues(o.opts.ServiceName).Inc()
		log.Printf("Unsupported observation received on %s: %s", o.opts.Topic, kind)
		return
	}
}

// Stop closes the connection to the network
func (o *ServiceObsListener) Stop() {
	o.nc.Close()
}
