// Copyright 2020 The NATS Authors
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
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"
	"time"

	"github.com/nats-io/jsm.go"
	"github.com/nats-io/jsm.go/api"
	"github.com/nats-io/jsm.go/api/jetstream/advisory"
	"github.com/nats-io/jsm.go/api/jetstream/metric"
	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus"
)

// JSAdvisoryListener listens for JetStream advisories and expose them as prometheus data
type JSAdvisoryListener struct {
	nc    *nats.Conn
	opts  *jsAdvisoryOptions
	sopts *Options
}

type jsAdvisoryOptions struct {
	AccountName string `json:"name"`
	Credentials string `json:"credentials"`
}

// Validate checks the options meet our expectations
func (o *jsAdvisoryOptions) Validate() error {
	errs := []string{}

	if o.AccountName == "" {
		errs = append(errs, "name is required")
	}

	if o.Credentials != "" {
		_, err := os.Stat(o.Credentials)
		if err != nil {
			errs = append(errs, fmt.Sprintf("invalid credential file: %s", err))
		}
	}

	if len(errs) == 0 {
		return nil
	}

	return fmt.Errorf(strings.Join(errs, ", "))
}

var (
	// API Audit
	jsAPIAuditCtr = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName("nats", "survey", "jetstream_api_audit"),
		Help: "JetStream API access audit events",
	}, []string{"server", "subject", "account"})

	jsAPIErrorsCtr = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName("nats", "survey", "jetstream_api_errors"),
		Help: "JetStream API Errors Count",
	}, []string{"server", "subject", "account"})

	// Delivery Exceeded
	jsDeliveryExceededCtr = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName("nats", "survey", "jetstream_delivery_exceeded_count"),
		Help: "Advisories about JetStream Consumer Delivery Exceeded events",
	}, []string{"account", "stream", "consumer"})

	// Ack Samples
	jsAckMetricDelay = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: prometheus.BuildFQName("nats", "survey", "jetstream_acknowledgement_duration"),
		Help: "How long an Acknowledged message took to be Acknowledged",
	}, []string{"account", "stream", "consumer"})

	jsAckMetricDeliveries = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName("nats", "survey", "jetstream_acknowledgement_deliveries"),
		Help: "How many times messages took to be delivered and Acknowledged",
	}, []string{"account", "stream", "consumer"})

	// Misc
	jsAdvisoriesGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: prometheus.BuildFQName("nats", "survey", "jetstream_advisory_count"),
		Help: "Number of JetStream Advisory listeners that are running",
	})

	jsUnknownAdvisoryCtr = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName("nats", "survey", "jetstream_unknown_advisories"),
		Help: "Unsupported JetStream Advisory types received",
	}, []string{"schema", "account"})
)

func init() {
	prometheus.MustRegister(jsAPIAuditCtr)
	prometheus.MustRegister(jsAPIErrorsCtr)
	prometheus.MustRegister(jsAdvisoriesGauge)
	prometheus.MustRegister(jsUnknownAdvisoryCtr)
	prometheus.MustRegister(jsDeliveryExceededCtr)
	prometheus.MustRegister(jsAckMetricDelay)
	prometheus.MustRegister(jsAckMetricDeliveries)
}

// NewJetStreamAdvisoryListener creates a new JetStream advisory reporter
func NewJetStreamAdvisoryListener(f string, sopts Options) (*JSAdvisoryListener, error) {
	js, err := ioutil.ReadFile(f)
	if err != nil {
		return nil, err
	}

	opts := &jsAdvisoryOptions{}
	err = json.Unmarshal(js, opts)
	if err != nil {
		return nil, fmt.Errorf("invalid JetStream advisory configuration: %s: %s", f, err)
	}

	err = opts.Validate()
	if err != nil {
		return nil, fmt.Errorf("invalid JetStream advisory configuration: %s: %s", f, err)
	}

	sopts.Name = fmt.Sprintf("%s (jetstream %s)", sopts.Name, opts.AccountName)
	sopts.Credentials = opts.Credentials
	nc, err := connect(&sopts)
	if err != nil {
		return nil, fmt.Errorf("nats connection failed: %s", err)
	}

	return &JSAdvisoryListener{
		nc:    nc,
		opts:  opts,
		sopts: &sopts,
	}, nil
}

// Start starts listening for observations
func (o *JSAdvisoryListener) Start() error {
	_, err := o.nc.Subscribe(api.JSAdvisoryPrefix+".>", o.advisoryHandler)
	if err != nil {
		return fmt.Errorf("could not subscribe to JetStream Advisory topic for %s (%s): %s", o.opts.AccountName, api.JSAdvisoryPrefix, err)
	}
	log.Printf("Started JetStream Advisory listener stats on %s.> for %s", api.JSAdvisoryPrefix, o.opts.AccountName)

	_, err = o.nc.Subscribe(api.JSMetricPrefix+".>", o.advisoryHandler)
	if err != nil {
		return fmt.Errorf("could not subscribe to JetStream Advisory topic for %s (%s): %s", o.opts.AccountName, api.JSMetricPrefix, err)
	}
	log.Printf("Started JetStream Advisory listener stats on %s.> for %s", api.JSMetricPrefix, o.opts.AccountName)

	_ = o.nc.Flush()

	jsAdvisoriesGauge.Inc()

	return nil
}

func (o *JSAdvisoryListener) advisoryHandler(m *nats.Msg) {
	schema, event, err := jsm.ParseEvent(m.Data)
	if err != nil {
		log.Printf("Could not parse JetStream API Audit Advisory: %s", err)
		return
	}

	switch event := event.(type) {
	case *advisory.JetStreamAPIAuditV1:
		if strings.HasPrefix(event.Response, api.ErrPrefix) {
			jsAPIErrorsCtr.WithLabelValues(event.Server, event.Subject, o.opts.AccountName).Inc()
		}

		jsAPIAuditCtr.WithLabelValues(event.Server, event.Subject, o.opts.AccountName).Inc()

	case *advisory.ConsumerDeliveryExceededAdvisoryV1:
		jsDeliveryExceededCtr.WithLabelValues(o.opts.AccountName, event.Stream, event.Consumer).Add(float64(event.Deliveries))

	case *metric.ConsumerAckMetricV1:
		jsAckMetricDelay.WithLabelValues(o.opts.AccountName, event.Stream, event.Consumer).Observe(time.Duration(event.Delay).Seconds())
		jsAckMetricDeliveries.WithLabelValues(o.opts.AccountName, event.Stream, event.Consumer).Add(float64(event.Deliveries))

	default:
		jsUnknownAdvisoryCtr.WithLabelValues(schema, o.opts.AccountName).Inc()
		log.Printf("Could not handle event as an JetStream Advisory with schema %s", schema)
	}
}

// Stop closes the connection to the network
func (o *JSAdvisoryListener) Stop() {
	o.nc.Close()
}
