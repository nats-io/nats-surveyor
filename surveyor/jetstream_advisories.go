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
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/nats-io/jsm.go"
	"github.com/nats-io/jsm.go/api"
	"github.com/nats-io/jsm.go/api/jetstream/advisory"
	"github.com/nats-io/jsm.go/api/jetstream/metric"
	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
)

var (
	streamCrudRe   = regexp.MustCompile(`\$JS.API.STREAM.(CREATE|UPDATE|DELETE|INFO|SNAPSHOT|RESTORE)`)
	streamMsgRe    = regexp.MustCompile(`\$JS.API.STREAM.MSG.(GET|DELETE)`)
	consumerCrudRe = regexp.MustCompile(`\$JS.API.CONSUMER.(CREATE|UPDATE|DELETE|INFO|SNAPSHOT|RESTORE|NAMES)`)
	templateCrudRe = regexp.MustCompile(`\$JS.API.STREAM.TEMPLATE.(CREATE|DELETE|INFO)`)
)

type JSAdvisoryMetrics struct {
	jsAPIAuditCtr           *prometheus.CounterVec
	jsDeliveryExceededCtr   *prometheus.CounterVec
	jsDeliveryTerminatedCtr *prometheus.CounterVec
	jsAckMetricDelay        *prometheus.HistogramVec
	jsAckMetricDeliveries   *prometheus.CounterVec
	jsAdvisoriesGauge       prometheus.Gauge
	jsUnknownAdvisoryCtr    *prometheus.CounterVec
	jsTotalAdvisoryCtr      *prometheus.CounterVec
	jsAdvisoryParseErrorCtr *prometheus.CounterVec
	jsConsumerActionCtr     *prometheus.CounterVec
	jsStreamActionCtr       *prometheus.CounterVec
	jsSnapshotSizeCtr       *prometheus.CounterVec
	jsSnapthotDuration      *prometheus.HistogramVec
	jsRestoreCreatedCtr     *prometheus.CounterVec
	jsRestoreSizeCtr        *prometheus.CounterVec
	jsRestoreDuration       *prometheus.HistogramVec
	jsConsumerLeaderElected *prometheus.CounterVec
	jsConsumerQuorumLost    *prometheus.CounterVec
	jsStreamLeaderElected   *prometheus.CounterVec
	jsStreamQuorumLost      *prometheus.CounterVec
	jsConsumerDeliveryNAK   *prometheus.CounterVec
}

func NewJetStreamAdvisoryMetrics(registry *prometheus.Registry, constLabels prometheus.Labels) *JSAdvisoryMetrics {
	metrics := &JSAdvisoryMetrics{
		// API Audit
		jsAPIAuditCtr: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name:        prometheus.BuildFQName("nats", "jetstream", "api_audit"),
			Help:        "JetStream API access audit events",
			ConstLabels: constLabels,
		}, []string{"subject", "account"}),

		// Delivery Exceeded
		jsDeliveryExceededCtr: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name:        prometheus.BuildFQName("nats", "jetstream", "delivery_exceeded_count"),
			Help:        "Advisories about JetStream Consumer Delivery Exceeded events",
			ConstLabels: constLabels,
		}, []string{"account", "stream", "consumer"}),

		jsDeliveryTerminatedCtr: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name:        prometheus.BuildFQName("nats", "jetstream", "delivery_terminated_count"),
			Help:        "Advisories about JetStream Consumer Delivery Terminated events",
			ConstLabels: constLabels,
		}, []string{"account", "stream", "consumer"}),

		// Ack Samples
		jsAckMetricDelay: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:        prometheus.BuildFQName("nats", "jetstream", "acknowledgement_duration"),
			Help:        "How long an Acknowledged message took to be Acknowledged",
			ConstLabels: constLabels,
		}, []string{"account", "stream", "consumer"}),

		jsAckMetricDeliveries: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name:        prometheus.BuildFQName("nats", "jetstream", "acknowledgement_deliveries"),
			Help:        "How many times messages took to be delivered and Acknowledged",
			ConstLabels: constLabels,
		}, []string{"account", "stream", "consumer"}),

		// Misc
		jsAdvisoriesGauge: prometheus.NewGauge(prometheus.GaugeOpts{
			Name:        prometheus.BuildFQName("nats", "jetstream", "advisory_count"),
			Help:        "Number of JetStream Advisory listeners that are running",
			ConstLabels: constLabels,
		}),

		jsUnknownAdvisoryCtr: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name:        prometheus.BuildFQName("nats", "jetstream", "unknown_advisories"),
			Help:        "Unsupported JetStream Advisory types received",
			ConstLabels: constLabels,
		}, []string{"schema", "account"}),

		jsTotalAdvisoryCtr: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name:        prometheus.BuildFQName("nats", "jetstream", "total_advisories"),
			Help:        "Total JetStream Advisories handled",
			ConstLabels: constLabels,
		}, []string{"account"}),

		jsAdvisoryParseErrorCtr: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name:        prometheus.BuildFQName("nats", "jetstream", "advisory_parse_errors"),
			Help:        "Number of advisories that could not be parsed",
			ConstLabels: constLabels,
		}, []string{"account"}),

		// Stream and Consumer actions
		jsConsumerActionCtr: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name:        prometheus.BuildFQName("nats", "jetstream", "consumer_actions"),
			Help:        "Actions performed on consumers",
			ConstLabels: constLabels,
		}, []string{"account", "stream", "action"}),

		jsStreamActionCtr: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name:        prometheus.BuildFQName("nats", "jetstream", "stream_actions"),
			Help:        "Actions performed on streams",
			ConstLabels: constLabels,
		}, []string{"account", "stream", "action"}),

		// Snapshot create
		jsSnapshotSizeCtr: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name:        prometheus.BuildFQName("nats", "jetstream", "snapshot_size_bytes"),
			Help:        "The size of snapshots being created",
			ConstLabels: constLabels,
		}, []string{"account", "stream"}),

		jsSnapthotDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:        prometheus.BuildFQName("nats", "jetstream", "snapshot_duration"),
			Help:        "How long a snapshot takes to be processed",
			ConstLabels: constLabels,
		}, []string{"account", "stream"}),

		// Restore
		jsRestoreCreatedCtr: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name:        prometheus.BuildFQName("nats", "jetstream", "restore_created_count"),
			Help:        "How many restore operations were started",
			ConstLabels: constLabels,
		}, []string{"account", "stream"}),

		jsRestoreSizeCtr: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name:        prometheus.BuildFQName("nats", "jetstream", "restore_size_bytes"),
			Help:        "The size of restores that was completed",
			ConstLabels: constLabels,
		}, []string{"account", "stream"}),

		jsRestoreDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:        prometheus.BuildFQName("nats", "jetstream", "restore_duration"),
			Help:        "How long a restore took to be processed",
			ConstLabels: constLabels,
		}, []string{"account", "stream"}),

		jsConsumerLeaderElected: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name:        prometheus.BuildFQName("nats", "jetstream", "consumer_leader_elected"),
			Help:        "How many times leader elections were done for consumers",
			ConstLabels: constLabels,
		}, []string{"account", "stream"}),

		jsConsumerQuorumLost: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name:        prometheus.BuildFQName("nats", "jetstream", "consumer_quorum_lost"),
			Help:        "How many times a consumer lost quorum leading to new leader elections",
			ConstLabels: constLabels,
		}, []string{"account", "stream"}),

		jsStreamLeaderElected: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name:        prometheus.BuildFQName("nats", "jetstream", "stream_leader_elected"),
			Help:        "How many times leader elections were done for streams",
			ConstLabels: constLabels,
		}, []string{"account", "stream"}),

		jsStreamQuorumLost: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name:        prometheus.BuildFQName("nats", "jetstream", "stream_quorum_lost"),
			Help:        "How many times a stream lost quorum leading to new leader elections",
			ConstLabels: constLabels,
		}, []string{"account", "stream"}),

		jsConsumerDeliveryNAK: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name:        prometheus.BuildFQName("nats", "jetstream", "consumer_nak"),
			Help:        "How many times a consumer sent a NAK",
			ConstLabels: constLabels,
		}, []string{"account", "stream", "consumer"}),
	}

	registry.MustRegister(metrics.jsAPIAuditCtr)
	registry.MustRegister(metrics.jsDeliveryExceededCtr)
	registry.MustRegister(metrics.jsDeliveryTerminatedCtr)
	registry.MustRegister(metrics.jsAckMetricDelay)
	registry.MustRegister(metrics.jsAckMetricDeliveries)
	registry.MustRegister(metrics.jsAdvisoriesGauge)
	registry.MustRegister(metrics.jsUnknownAdvisoryCtr)
	registry.MustRegister(metrics.jsTotalAdvisoryCtr)
	registry.MustRegister(metrics.jsAdvisoryParseErrorCtr)
	registry.MustRegister(metrics.jsConsumerActionCtr)
	registry.MustRegister(metrics.jsStreamActionCtr)
	registry.MustRegister(metrics.jsSnapshotSizeCtr)
	registry.MustRegister(metrics.jsSnapthotDuration)
	registry.MustRegister(metrics.jsRestoreCreatedCtr)
	registry.MustRegister(metrics.jsRestoreSizeCtr)
	registry.MustRegister(metrics.jsRestoreDuration)
	registry.MustRegister(metrics.jsConsumerLeaderElected)
	registry.MustRegister(metrics.jsConsumerQuorumLost)
	registry.MustRegister(metrics.jsStreamLeaderElected)
	registry.MustRegister(metrics.jsStreamQuorumLost)
	registry.MustRegister(metrics.jsConsumerDeliveryNAK)

	return metrics
}

// JSAdvisoryListener listens for JetStream advisories and expose them as prometheus data
type JSAdvisoryListener struct {
	nc      *nats.Conn
	logger  *logrus.Logger
	opts    *jsAdvisoryOptions
	metrics *JSAdvisoryMetrics
	sopts   *Options
}

type jsAdvisoryOptions struct {
	AccountName string `json:"name"`
	Credentials string `json:"credential"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	NKey        string `json:"nkey"`
	TLSCert     string `json:"tls_cert"`
	TLSKey      string `json:"tls_key"`
	TLSCA       string `json:"tls_ca"`
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

// NewJetStreamAdvisoryListener creates a new JetStream advisory reporter
func NewJetStreamAdvisoryListener(f string, sopts Options, metrics *JSAdvisoryMetrics, reconnectCtr *prometheus.CounterVec) (*JSAdvisoryListener, error) {
	js, err := os.ReadFile(f)
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
	sopts.NATSUser = opts.Username
	sopts.NATSPassword = opts.Password
	sopts.Nkey = opts.NKey

	if opts.TLSKey != "" && opts.TLSCert != "" && opts.TLSCA != "" {
		sopts.CaFile = opts.TLSCA
		sopts.CertFile = opts.TLSCert
		sopts.KeyFile = opts.TLSKey
	}

	nc, err := connect(&sopts, reconnectCtr)
	if err != nil {
		return nil, fmt.Errorf("nats connection failed: %s", err)
	}

	return &JSAdvisoryListener{
		nc:      nc,
		logger:  sopts.Logger,
		opts:    opts,
		metrics: metrics,
		sopts:   &sopts,
	}, nil
}

// Start starts listening for observations
func (o *JSAdvisoryListener) Start() error {
	_, err := o.nc.Subscribe(api.JSAdvisoryPrefix+".>", o.advisoryHandler)
	if err != nil {
		return fmt.Errorf("could not subscribe to JetStream Advisory topic for %s (%s): %s", o.opts.AccountName, api.JSAdvisoryPrefix, err)
	}
	o.logger.Infof("Started JetStream Advisory listener stats on %s.> for %s", api.JSAdvisoryPrefix, o.opts.AccountName)

	_, err = o.nc.Subscribe(api.JSMetricPrefix+".>", o.advisoryHandler)
	if err != nil {
		return fmt.Errorf("could not subscribe to JetStream Advisory topic for %s (%s): %s", o.opts.AccountName, api.JSMetricPrefix, err)
	}
	o.logger.Infof("Started JetStream Metric listener stats on %s.> for %s", api.JSMetricPrefix, o.opts.AccountName)

	_ = o.nc.Flush()

	o.metrics.jsAdvisoriesGauge.Inc()

	return nil
}

// this removes much of the dynamic details off the api subjects to limit the number of labels,
// without doing this the dimensions in the data will explode and consume vast resources
//
// bit janky in having to maintain this, but seems to be the best we can do atm without modifying
// the advisories to include a API type in the subject
func limitJSSubject(subj string) string {
	var parts [][]string

	switch {
	case streamMsgRe.MatchString(subj):
		parts = streamMsgRe.FindAllStringSubmatch(subj, -1)

	case streamCrudRe.MatchString(subj):
		parts = streamCrudRe.FindAllStringSubmatch(subj, -1)

	case consumerCrudRe.MatchString(subj):
		parts = consumerCrudRe.FindAllStringSubmatch(subj, -1)

	case templateCrudRe.MatchString(subj):
		parts = templateCrudRe.FindAllStringSubmatch(subj, -1)

	case strings.HasPrefix(subj, "$JS.API.CONSUMER.DURABLE.CREATE"):
		return "$JS.API.CONSUMER.DURABLE.CREATE"

	case strings.HasPrefix(subj, "$JS.API.CONSUMER.MSG.NEXT"):
		return "$JS.API.CONSUMER.MSG.NEXT"

	}

	if len(parts) > 0 {
		return parts[0][0]
	}

	return subj
}

func (o *JSAdvisoryListener) advisoryHandler(m *nats.Msg) {
	schema, event, err := jsm.ParseEvent(m.Data)
	if err != nil {
		o.metrics.jsAdvisoryParseErrorCtr.WithLabelValues(o.opts.AccountName).Inc()
		o.logger.Warnf("Could not parse JetStream API Audit Advisory: %s", err)
		return
	}

	o.metrics.jsTotalAdvisoryCtr.WithLabelValues(o.opts.AccountName).Inc()

	switch event := event.(type) {
	case *advisory.JetStreamAPIAuditV1:
		o.metrics.jsAPIAuditCtr.WithLabelValues(limitJSSubject(event.Subject), o.opts.AccountName).Inc()

	case *advisory.ConsumerDeliveryExceededAdvisoryV1:
		o.metrics.jsDeliveryExceededCtr.WithLabelValues(o.opts.AccountName, event.Stream, event.Consumer).Add(float64(event.Deliveries))

	case *metric.ConsumerAckMetricV1:
		o.metrics.jsAckMetricDelay.WithLabelValues(o.opts.AccountName, event.Stream, event.Consumer).Observe(time.Duration(event.Delay).Seconds())
		o.metrics.jsAckMetricDeliveries.WithLabelValues(o.opts.AccountName, event.Stream, event.Consumer).Add(float64(event.Deliveries))

	case *advisory.JSConsumerActionAdvisoryV1:
		o.metrics.jsConsumerActionCtr.WithLabelValues(o.opts.AccountName, event.Stream, event.Action.String()).Inc()

	case *advisory.JSStreamActionAdvisoryV1:
		o.metrics.jsStreamActionCtr.WithLabelValues(o.opts.AccountName, event.Stream, event.Action.String()).Inc()

	case *advisory.JSConsumerDeliveryTerminatedAdvisoryV1:
		o.metrics.jsDeliveryTerminatedCtr.WithLabelValues(o.opts.AccountName, event.Stream, event.Consumer).Inc()

	case *advisory.JSRestoreCreateAdvisoryV1:
		o.metrics.jsRestoreCreatedCtr.WithLabelValues(o.opts.AccountName, event.Stream).Inc()

	case *advisory.JSRestoreCompleteAdvisoryV1:
		o.metrics.jsRestoreSizeCtr.WithLabelValues(o.opts.AccountName, event.Stream).Add(float64(event.Bytes))
		o.metrics.jsRestoreDuration.WithLabelValues(o.opts.AccountName, event.Stream).Observe(event.End.Sub(event.Start).Seconds())

	case *advisory.JSSnapshotCreateAdvisoryV1:
		o.metrics.jsSnapshotSizeCtr.WithLabelValues(o.opts.AccountName, event.Stream).Add(float64(event.BlkSize * event.NumBlks))

	case *advisory.JSSnapshotCompleteAdvisoryV1:
		o.metrics.jsSnapthotDuration.WithLabelValues(o.opts.AccountName, event.Stream).Observe(event.End.Sub(event.Start).Seconds())

	case *advisory.JSConsumerLeaderElectedV1:
		o.metrics.jsConsumerLeaderElected.WithLabelValues(o.opts.AccountName, event.Stream).Inc()

	case *advisory.JSConsumerQuorumLostV1:
		o.metrics.jsConsumerQuorumLost.WithLabelValues(o.opts.AccountName, event.Stream).Inc()

	case *advisory.JSStreamLeaderElectedV1:
		o.metrics.jsStreamLeaderElected.WithLabelValues(o.opts.AccountName, event.Stream).Inc()

	case *advisory.JSStreamQuorumLostV1:
		o.metrics.jsStreamQuorumLost.WithLabelValues(o.opts.AccountName, event.Stream).Inc()

	case *advisory.JSConsumerDeliveryNakAdvisoryV1:
		o.metrics.jsConsumerDeliveryNAK.WithLabelValues(o.opts.AccountName, event.Stream, event.Consumer).Inc()

	default:
		o.metrics.jsUnknownAdvisoryCtr.WithLabelValues(schema, o.opts.AccountName).Inc()
		o.logger.Warnf("Could not handle event as an JetStream Advisory with schema %s", schema)
	}
}

// Stop closes the connection to the network
func (o *JSAdvisoryListener) Stop() {
	o.nc.Close()
}
