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
	"io/ioutil"
	"log"
	"os"
	"strings"

	server "github.com/nats-io/nats-server/v2/server"
	nats "github.com/nats-io/nats.go"
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
}

func (o *serviceObsOptions) Validate() error {
	errs := []string{}

	if o.ServiceName == "" {
		errs = append(errs, "name is required")
	}

	if o.Topic == "" {
		errs = append(errs, "topic is required")
	}

	if o.Credentials == "" {
		errs = append(errs, "credential is required")
	} else {
		_, err := os.Stat(o.Credentials)
		if err != nil {
			errs = append(errs, fmt.Sprintf("invalid credential file: %s", err))
		}
	}

	if len(errs) == 0 {
		return nil
	}

	return errors.New(strings.Join(errs, ", "))
}

var (
	observationsGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: prometheus.BuildFQName("nats", "survey", "observerations_count"),
		Help: "Number of observations that are running",
	})

	observationsReceived = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "nats_latency_observation_count",
		Help: "Number of observations received by this surveyor across all services",
	}, []string{"service", "app"})

	invalidObservationsReceived = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "nats_latency_observation_error_count",
		Help: "Number of observations received by this surveyor across all services that could not be handled",
	}, []string{"service"})

	serviceLatency = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "nats_latency_service_duration",
		Help: "Time spent serving the request in the service",
	}, []string{"service", "app"})

	totalLatency = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "nats_latency_total_duration",
		Help: "Total time spent serving a service including network overheads",
	}, []string{"service", "app"})

	requestorRTT = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "nats_latency_requestor_rtt",
		Help: "The RTT to the client making a request",
	}, []string{"service", "app"})

	responderRTT = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "nats_latency_responder_rtt",
		Help: "The RTT to the service serving the request",
	}, []string{"service", "app"})

	systemRTT = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "nats_latency_system_rtt",
		Help: "The RTT within the NATS system - time traveling clusters, gateways and leaf nodes",
	}, []string{"service", "app"})
)

func init() {
	prometheus.MustRegister(invalidObservationsReceived)
	prometheus.MustRegister(observationsReceived)
	prometheus.MustRegister(serviceLatency)
	prometheus.MustRegister(totalLatency)
	prometheus.MustRegister(requestorRTT)
	prometheus.MustRegister(responderRTT)
	prometheus.MustRegister(systemRTT)
	prometheus.MustRegister(observationsGauge)
}

// NewServiceObservation creates a new performance observation listener
func NewServiceObservation(f string, sopts Options) (*ServiceObsListener, error) {
	js, err := ioutil.ReadFile(f)
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
	_ = o.nc.Flush()

	observationsGauge.Inc()
	log.Printf("Started observing stats on %s for %s", o.opts.Topic, o.opts.ServiceName)

	return nil
}

func (o *ServiceObsListener) observationHandler(m *nats.Msg) {
	obs := &server.ServiceLatency{}
	err := json.Unmarshal(m.Data, obs)
	if err != nil {
		invalidObservationsReceived.WithLabelValues(o.opts.ServiceName).Inc()
		log.Printf("Unparsable observation received on %s: %s", o.opts.Topic, err)
		return
	}

	observationsReceived.WithLabelValues(o.opts.ServiceName, obs.AppName).Inc()
	serviceLatency.WithLabelValues(o.opts.ServiceName, obs.AppName).Observe(obs.ServiceLatency.Seconds())
	totalLatency.WithLabelValues(o.opts.ServiceName, obs.AppName).Observe(obs.TotalLatency.Seconds())
	requestorRTT.WithLabelValues(o.opts.ServiceName, obs.AppName).Observe(obs.NATSLatency.Requestor.Seconds())
	responderRTT.WithLabelValues(o.opts.ServiceName, obs.AppName).Observe(obs.NATSLatency.Responder.Seconds())
	systemRTT.WithLabelValues(o.opts.ServiceName, obs.AppName).Observe(obs.NATSLatency.System.Seconds())
}

// Stop closes the connection to the network
func (o *ServiceObsListener) Stop() {
	o.nc.Close()
}
