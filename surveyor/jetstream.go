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
	Credentials string `json:"credential"`
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
		Name: prometheus.BuildFQName("nats", "jetstream", "api_audit"),
		Help: "JetStream API access audit events",
	}, []string{"server", "subject", "account"})

	jsAPIErrorsCtr = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName("nats", "jetstream", "api_errors"),
		Help: "JetStream API Errors Count",
	}, []string{"server", "subject", "account"})

	// Delivery Exceeded
	jsDeliveryExceededCtr = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName("nats", "jetstream", "delivery_exceeded_count"),
		Help: "Advisories about JetStream Consumer Delivery Exceeded events",
	}, []string{"account", "stream", "consumer"})

	jsDeliveryTerminatedCtr = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName("nats", "jetstream", "delivery_terminated_count"),
		Help: "Advisories about JetStream Consumer Delivery Terminated events",
	}, []string{"account", "stream", "consumer"})

	// Ack Samples
	jsAckMetricDelay = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: prometheus.BuildFQName("nats", "jetstream", "acknowledgement_duration"),
		Help: "How long an Acknowledged message took to be Acknowledged",
	}, []string{"account", "stream", "consumer"})

	jsAckMetricDeliveries = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName("nats", "jetstream", "acknowledgement_deliveries"),
		Help: "How many times messages took to be delivered and Acknowledged",
	}, []string{"account", "stream", "consumer"})

	// Misc
	jsAdvisoriesGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: prometheus.BuildFQName("nats", "jetstream", "advisory_count"),
		Help: "Number of JetStream Advisory listeners that are running",
	})

	jsUnknownAdvisoryCtr = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName("nats", "jetstream", "unknown_advisories"),
		Help: "Unsupported JetStream Advisory types received",
	}, []string{"schema", "account"})

	jsTotalAdvisoryCtr = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName("nats", "jetstream", "total_advisories"),
		Help: "Total JetStream Advisories handled",
	}, []string{"account"})

	jsAdvisoryParseErrorCtr = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName("nats", "jetstream", "advisory_parse_errors"),
		Help: "Number of advisories that could not be parsed",
	}, []string{"account"})

	// Stream and Consumer actions
	jsConsumerActionCtr = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName("nats", "jetstream", "consumer_actions"),
		Help: "Actions performed on consumers",
	}, []string{"account", "stream", "action"})

	jsStreamActionCtr = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName("nats", "jetstream", "stream_actions"),
		Help: "Actions performed on streams",
	}, []string{"account", "stream", "action"})

	// Snapshot create
	jsSnapshotSizeCtr = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName("nats", "jetstream", "snapshot_size_bytes"),
		Help: "The size of snapshots being created",
	}, []string{"account", "stream"})

	jsSnapthotDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: prometheus.BuildFQName("nats", "jetstream", "snapshot_duration"),
		Help: "How long a snapshot takes to be processed",
	}, []string{"account", "stream"})

	// Restore
	jsRestoreCreatedCtr = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName("nats", "jetstream", "restore_created_count"),
		Help: "How many restore operations were started",
	}, []string{"account", "stream"})

	jsRestoreSizeCtr = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName("nats", "jetstream", "restore_size_bytes"),
		Help: "The size of restores that was completed",
	}, []string{"account", "stream"})

	jsRestoreDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: prometheus.BuildFQName("nats", "jetstream", "restore_duration"),
		Help: "How long a restore took to be processed",
	}, []string{"account", "stream"})
)

func init() {
	prometheus.MustRegister(jsAPIAuditCtr)
	prometheus.MustRegister(jsAPIErrorsCtr)
	prometheus.MustRegister(jsAdvisoriesGauge)
	prometheus.MustRegister(jsUnknownAdvisoryCtr)
	prometheus.MustRegister(jsDeliveryExceededCtr)
	prometheus.MustRegister(jsDeliveryTerminatedCtr)
	prometheus.MustRegister(jsAckMetricDelay)
	prometheus.MustRegister(jsAckMetricDeliveries)
	prometheus.MustRegister(jsConsumerActionCtr)
	prometheus.MustRegister(jsStreamActionCtr)
	prometheus.MustRegister(jsSnapshotSizeCtr)
	prometheus.MustRegister(jsSnapthotDuration)
	prometheus.MustRegister(jsRestoreSizeCtr)
	prometheus.MustRegister(jsRestoreDuration)
	prometheus.MustRegister(jsRestoreCreatedCtr)
	prometheus.MustRegister(jsTotalAdvisoryCtr)
	prometheus.MustRegister(jsAdvisoryParseErrorCtr)
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
		jsAdvisoryParseErrorCtr.WithLabelValues(o.opts.AccountName).Inc()
		log.Printf("Could not parse JetStream API Audit Advisory: %s", err)
		return
	}

	jsTotalAdvisoryCtr.WithLabelValues(o.opts.AccountName).Inc()

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

	case *advisory.JSConsumerActionAdvisoryV1:
		jsConsumerActionCtr.WithLabelValues(o.opts.AccountName, event.Stream, event.Action.String()).Inc()

	case *advisory.JSStreamActionAdvisoryV1:
		jsStreamActionCtr.WithLabelValues(o.opts.AccountName, event.Stream, event.Action.String()).Inc()

	case *advisory.JSConsumerDeliveryTerminatedAdvisoryV1:
		jsDeliveryTerminatedCtr.WithLabelValues(o.opts.AccountName, event.Stream, event.Consumer).Inc()

	case *advisory.JSRestoreCreateAdvisoryV1:
		jsRestoreCreatedCtr.WithLabelValues(o.opts.AccountName, event.Stream).Inc()

	case *advisory.JSRestoreCompleteAdvisoryV1:
		jsRestoreSizeCtr.WithLabelValues(o.opts.AccountName, event.Stream).Add(float64(event.Bytes))
		jsRestoreDuration.WithLabelValues(o.opts.AccountName, event.Stream).Observe(event.End.Sub(event.Start).Seconds())

	case *advisory.JSSnapshotCreateAdvisoryV1:
		jsSnapshotSizeCtr.WithLabelValues(o.opts.AccountName, event.Stream).Add(float64(event.BlkSize * event.NumBlks))

	case *advisory.JSSnapshotCompleteAdvisoryV1:
		jsSnapthotDuration.WithLabelValues(o.opts.AccountName, event.Stream).Observe(event.End.Sub(event.Start).Seconds())

	default:
		jsUnknownAdvisoryCtr.WithLabelValues(schema, o.opts.AccountName).Inc()
		log.Printf("Could not handle event as an JetStream Advisory with schema %s", schema)
	}
}

// Stop closes the connection to the network
func (o *JSAdvisoryListener) Stop() {
	o.nc.Close()
}
