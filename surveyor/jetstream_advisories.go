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
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/nats-io/nats.go"

	"github.com/nats-io/jsm.go"
	"github.com/nats-io/jsm.go/api"
	"github.com/nats-io/jsm.go/api/jetstream/advisory"
	"github.com/nats-io/jsm.go/api/jetstream/metric"
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

type JSAdvisoryConfig struct {
	// unique identifier
	ID string `json:"id"`

	// account name
	AccountName string `json:"name"`

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

	// additional opts available via JSAdvisoryConfig directly (not from config file)

	// optional configuration for importing JS metrics and advisories from other accounts
	ExternalAccountConfig *JSAdvisoriesExternalAccountConfig `json:"-"`

	// nats options appended to base surveyor options
	NatsOpts []nats.Option `json:"-"`
}

// JSAdvisoriesExternalAccountConfig is used to configure external accounts from which
// JS metrics and advisories will be imported
type JSAdvisoriesExternalAccountConfig struct {
	// subject on which JS metrics from external accounts will be received.
	// if not set, metrics will be gathered from account set in AccountName.
	MetricsSubject string `json:"metrics_subject"`

	// position of account token in ExternalMetricsSubject
	MetricsAccountTokenPosition int `json:"metrics_account_token_position"`

	// subject on which JS advisories from external accounts will be received.
	// if not set, advisories will be gathered from account set in AccountName.
	AdvisorySubject string `json:"advisory_subject"`

	// position of account token in ExternalAdvisorySubject
	AdvisoryAccountTokenPosition int `json:"advisory_account_token_position"`
}

// Validate is used to validate a JSAdvisoryConfig
func (o *JSAdvisoryConfig) Validate() error {
	if o == nil {
		return fmt.Errorf("js advisory config cannot be nil")
	}

	var errs []string
	if o.ID == "" {
		errs = append(errs, "id is required")
	}

	if o.AccountName == "" {
		errs = append(errs, "name is required")
	}

	if o.ExternalAccountConfig != nil {
		if o.ExternalAccountConfig.MetricsSubject == "" {
			errs = append(errs, "external_account_config.metrics_subject is required when importing metrics from external accounts")
		}
		metricsTokens := strings.Split(o.ExternalAccountConfig.MetricsSubject, ".")
		switch {
		case o.ExternalAccountConfig.MetricsAccountTokenPosition <= 0:
			errs = append(errs, "external_account_config.metrics_account_token_position is required when importing metrics from external accounts")
		case o.ExternalAccountConfig.MetricsAccountTokenPosition > len(metricsTokens):
			errs = append(errs, "external_account_config.metrics_account_token_position is greater than the number of tokens in external_account_config.metrics_subject")
		case metricsTokens[o.ExternalAccountConfig.AdvisoryAccountTokenPosition-1] != "*":
			errs = append(errs, "external_account_config.metrics_subject must have a wildcard token at the position specified by external_account_config.metrics_account_token_position")
		}

		if o.ExternalAccountConfig.AdvisorySubject == "" {
			errs = append(errs, "external_account_config.advisory_subject is required when importing advisories from external accounts")
		}
		advisoryTokens := strings.Split(o.ExternalAccountConfig.AdvisorySubject, ".")
		switch {
		case o.ExternalAccountConfig.AdvisoryAccountTokenPosition <= 0:
			errs = append(errs, "external_account_config.advisory_account_token_position is required when importing advisories from external accounts")
		case o.ExternalAccountConfig.AdvisoryAccountTokenPosition > len(advisoryTokens):
			errs = append(errs, "external_account_config.advisory_account_token_position is greater than the number of tokens in external_account_config.advisory_subject")
		case advisoryTokens[o.ExternalAccountConfig.AdvisoryAccountTokenPosition-1] != "*":
			errs = append(errs, "external_account_config.advisory_subject must have a wildcard token at the position specified by external_account_config.advisory_account_token_position")
		}
	}
	if len(errs) == 0 {
		return nil
	}

	return fmt.Errorf(strings.Join(errs, ", "))
}

func (o *JSAdvisoryConfig) copy() *JSAdvisoryConfig {
	if o == nil {
		return nil
	}
	cp := *o
	return &cp
}

// NewJetStreamAdvisoryConfigFromFile creates a new JSAdvisoryConfig from a file
// the ID of the JSAdvisoryConfig is set to the filename f
func NewJetStreamAdvisoryConfigFromFile(f string) (*JSAdvisoryConfig, error) {
	js, err := os.ReadFile(f)
	if err != nil {
		return nil, err
	}

	jsa := &JSAdvisoryConfig{}
	err = json.Unmarshal(js, jsa)
	if err != nil {
		return nil, fmt.Errorf("invalid JetStream advisory config: %s: %s", f, err)
	}
	jsa.ID = f
	err = jsa.Validate()
	if err != nil {
		return nil, fmt.Errorf("invalid JetStream advisory config: %s: %s", f, err)
	}

	return jsa, nil
}

// jsAdvisoryListener listens for JetStream advisories and expose them as prometheus data
type jsAdvisoryListener struct {
	sync.Mutex
	config      *JSAdvisoryConfig
	cp          *natsConnPool
	logger      *logrus.Logger
	metrics     *JSAdvisoryMetrics
	pc          *pooledNatsConn
	subAdvisory *nats.Subscription
	subMetric   *nats.Subscription
}

func newJetStreamAdvisoryListener(config *JSAdvisoryConfig, cp *natsConnPool, logger *logrus.Logger, metrics *JSAdvisoryMetrics) (*jsAdvisoryListener, error) {
	err := config.Validate()
	if err != nil {
		return nil, fmt.Errorf("invalid JetStream advisory config for id: %s, account name: %s, error: %v", config.ID, config.AccountName, err)
	}

	return &jsAdvisoryListener{
		config:  config,
		cp:      cp,
		logger:  logger,
		metrics: metrics,
	}, nil
}

func (o *jsAdvisoryListener) natsContext() *natsContext {
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
		NatsOpts:    o.config.NatsOpts,
	}

	return natsCtx
}

// Start starts listening for JetStream advisories
func (o *jsAdvisoryListener) Start() error {
	o.Lock()
	defer o.Unlock()
	if o.pc != nil {
		// already started
		return nil
	}

	pc, err := o.cp.Get(o.natsContext())
	if err != nil {
		return fmt.Errorf("nats connection failed for id: %s, account name: %s, error: %v", o.config.ID, o.config.AccountName, err)
	}
	metricsSubject := api.JSMetricPrefix + ".>"
	advisorySubject := api.JSAdvisoryPrefix + ".>"
	if o.config.ExternalAccountConfig != nil {
		metricsSubject = o.config.ExternalAccountConfig.MetricsSubject
		advisorySubject = o.config.ExternalAccountConfig.AdvisorySubject
	}

	subAdvisory, err := pc.nc.Subscribe(metricsSubject, o.advisoryHandler)
	if err != nil {
		pc.ReturnToPool()
		return fmt.Errorf("could not subscribe to JetStream advisory for id: %s, account name: %s, topic: %s, error: %v", o.config.ID, o.config.AccountName, api.JSAdvisoryPrefix, err)
	}

	subMetric, err := pc.nc.Subscribe(advisorySubject, o.advisoryHandler)
	if err != nil {
		_ = subAdvisory.Unsubscribe()
		pc.ReturnToPool()
		return fmt.Errorf("could not subscribe to JetStream advisory for id: %s, account name: %s, topic: %s, error: %v", o.config.ID, o.config.AccountName, api.JSMetricPrefix, err)
	}

	o.pc = pc
	o.subAdvisory = subAdvisory
	o.subMetric = subMetric
	o.logger.Infof("started JetStream advisory for id: %s, account name: %s, advisory topic: %s, metric topic: %s", o.config.ID, o.config.AccountName, api.JSAdvisoryPrefix, api.JSMetricPrefix)
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

func (o *jsAdvisoryListener) advisoryHandler(m *nats.Msg) {
	accountName := o.config.AccountName
	var err error
	if o.config.ExternalAccountConfig != nil {
		var tokenPosition int
		if m.Sub.Subject == o.config.ExternalAccountConfig.MetricsSubject {
			tokenPosition = o.config.ExternalAccountConfig.MetricsAccountTokenPosition
		} else if m.Sub.Subject == o.config.ExternalAccountConfig.AdvisorySubject {
			tokenPosition = o.config.ExternalAccountConfig.AdvisoryAccountTokenPosition
		}
		if tokenPosition == 0 {
			o.logger.Warnf("Could not parse JetStream API Advisory: no configured subject matches subscription subject")
			return
		}
		accountName, err = getTokenFromSubject(m.Subject, tokenPosition)
		if err != nil {
			o.logger.Warnf("Could not parse JetStream API Advisory: %s", err)
			return
		}
	}
	schema, event, err := jsm.ParseEvent(m.Data)
	if err != nil {
		o.metrics.jsAdvisoryParseErrorCtr.WithLabelValues(accountName).Inc()
		o.logger.Warnf("Could not parse JetStream API Audit Advisory: %s", err)
		return
	}

	o.metrics.jsTotalAdvisoryCtr.WithLabelValues(accountName).Inc()

	switch event := event.(type) {
	case *advisory.JetStreamAPIAuditV1:
		o.metrics.jsAPIAuditCtr.WithLabelValues(limitJSSubject(event.Subject), accountName).Inc()

	case *advisory.ConsumerDeliveryExceededAdvisoryV1:
		o.metrics.jsDeliveryExceededCtr.WithLabelValues(accountName, event.Stream, event.Consumer).Add(float64(event.Deliveries))

	case *metric.ConsumerAckMetricV1:
		o.metrics.jsAckMetricDelay.WithLabelValues(accountName, event.Stream, event.Consumer).Observe(time.Duration(event.Delay).Seconds())
		o.metrics.jsAckMetricDeliveries.WithLabelValues(accountName, event.Stream, event.Consumer).Add(float64(event.Deliveries))

	case *advisory.JSConsumerActionAdvisoryV1:
		o.metrics.jsConsumerActionCtr.WithLabelValues(accountName, event.Stream, event.Action.String()).Inc()

	case *advisory.JSStreamActionAdvisoryV1:
		o.metrics.jsStreamActionCtr.WithLabelValues(accountName, event.Stream, event.Action.String()).Inc()

	case *advisory.JSConsumerDeliveryTerminatedAdvisoryV1:
		o.metrics.jsDeliveryTerminatedCtr.WithLabelValues(accountName, event.Stream, event.Consumer).Inc()

	case *advisory.JSRestoreCreateAdvisoryV1:
		o.metrics.jsRestoreCreatedCtr.WithLabelValues(accountName, event.Stream).Inc()

	case *advisory.JSRestoreCompleteAdvisoryV1:
		o.metrics.jsRestoreSizeCtr.WithLabelValues(accountName, event.Stream).Add(float64(event.Bytes))
		o.metrics.jsRestoreDuration.WithLabelValues(accountName, event.Stream).Observe(event.End.Sub(event.Start).Seconds())

	case *advisory.JSSnapshotCreateAdvisoryV1:
		o.metrics.jsSnapshotSizeCtr.WithLabelValues(accountName, event.Stream).Add(float64(event.BlkSize * event.NumBlks))

	case *advisory.JSSnapshotCompleteAdvisoryV1:
		o.metrics.jsSnapthotDuration.WithLabelValues(accountName, event.Stream).Observe(event.End.Sub(event.Start).Seconds())

	case *advisory.JSConsumerLeaderElectedV1:
		o.metrics.jsConsumerLeaderElected.WithLabelValues(accountName, event.Stream).Inc()

	case *advisory.JSConsumerQuorumLostV1:
		o.metrics.jsConsumerQuorumLost.WithLabelValues(accountName, event.Stream).Inc()

	case *advisory.JSStreamLeaderElectedV1:
		o.metrics.jsStreamLeaderElected.WithLabelValues(accountName, event.Stream).Inc()

	case *advisory.JSStreamQuorumLostV1:
		o.metrics.jsStreamQuorumLost.WithLabelValues(accountName, event.Stream).Inc()

	case *advisory.JSConsumerDeliveryNakAdvisoryV1:
		o.metrics.jsConsumerDeliveryNAK.WithLabelValues(accountName, event.Stream, event.Consumer).Inc()

	default:
		o.metrics.jsUnknownAdvisoryCtr.WithLabelValues(schema, accountName).Inc()
		o.logger.Warnf("Could not handle event as an JetStream Advisory with schema %s", schema)
	}
}

// Stop stops listening for JetStream advisories
func (o *jsAdvisoryListener) Stop() {
	o.Lock()
	defer o.Unlock()
	if o.pc == nil {
		// already stopped
		return
	}

	if o.subAdvisory != nil {
		_ = o.subAdvisory.Unsubscribe()
		o.subAdvisory = nil
	}

	if o.subMetric != nil {
		_ = o.subMetric.Unsubscribe()
		o.subMetric = nil
	}

	o.metrics.jsAdvisoriesGauge.Dec()
	o.pc.ReturnToPool()
	o.pc = nil
}

// JSAdvisoryManager exposes methods to operate on JetStream advisories
type JSAdvisoryManager struct {
	sync.Mutex
	cp          *natsConnPool
	listenerMap map[string]*jsAdvisoryListener
	logger      *logrus.Logger
	metrics     *JSAdvisoryMetrics
}

// newJetStreamAdvisoryManager creates a JSAdvisoryManager for managing JetStream advisories
func newJetStreamAdvisoryManager(cp *natsConnPool, logger *logrus.Logger, metrics *JSAdvisoryMetrics) *JSAdvisoryManager {
	return &JSAdvisoryManager{
		cp:      cp,
		logger:  logger,
		metrics: metrics,
	}
}

func (am *JSAdvisoryManager) start() {
	am.Lock()
	defer am.Unlock()
	if am.listenerMap != nil {
		// already started
		return
	}

	am.listenerMap = map[string]*jsAdvisoryListener{}
}

// IsRunning returns true if the advisory manager is running or false if it is stopped
func (am *JSAdvisoryManager) IsRunning() bool {
	am.Lock()
	defer am.Unlock()
	return am.listenerMap != nil
}

func (am *JSAdvisoryManager) stop() {
	am.Lock()
	defer am.Unlock()
	if am.listenerMap == nil {
		// already stopped
		return
	}

	for _, adv := range am.listenerMap {
		adv.Stop()
	}
	am.metrics.jsAdvisoriesGauge.Set(0)
	am.listenerMap = nil
}

// ConfigMap returns a map of id:*JSAdvisoryConfig for all running advisories
func (am *JSAdvisoryManager) ConfigMap() map[string]*JSAdvisoryConfig {
	am.Lock()
	defer am.Unlock()

	advMap := make(map[string]*JSAdvisoryConfig, len(am.listenerMap))
	if am.listenerMap == nil {
		return advMap
	}

	for id, adv := range am.listenerMap {
		// copy so that internal references cannot be changed
		advMap[id] = adv.config.copy()
	}

	return advMap
}

// Set creates or updates an advisory
// if an advisory exists with the same ID, it is updated
// otherwise, a new advisory is created
func (am *JSAdvisoryManager) Set(config *JSAdvisoryConfig) error {
	err := config.Validate()
	if err != nil {
		return err
	}

	// copy so that internal references cannot be changed
	config = config.copy()

	am.Lock()
	if am.listenerMap == nil {
		am.Unlock()
		return fmt.Errorf("advisory manager is stopped; could not set advisory for id: %s, account name: %s", config.ID, config.AccountName)
	}

	existingAdv, found := am.listenerMap[config.ID]
	am.Unlock()

	if found && reflect.DeepEqual(config, existingAdv.config) {
		return nil
	}

	adv, err := newJetStreamAdvisoryListener(config, am.cp, am.logger, am.metrics)
	if err != nil {
		return fmt.Errorf("could not set advisory for id: %s, account name: %s, error: %v", config.ID, config.AccountName, err)
	}

	if err := adv.Start(); err != nil {
		return fmt.Errorf("could not start advisory for id: %s, account name: %s, error: %v", config.ID, config.AccountName, err)
	}

	am.Lock()
	if am.listenerMap == nil {
		am.Unlock()
		adv.Stop()
		return fmt.Errorf("advisory manager is stopped; could not set advisory for id: %s, account name: %s", config.ID, config.AccountName)
	}

	am.listenerMap[config.ID] = adv
	am.Unlock()

	if found {
		existingAdv.Stop()
	}
	return nil
}

// Delete deletes existing advisory with provided ID
func (am *JSAdvisoryManager) Delete(id string) error {
	am.Lock()
	if am.listenerMap == nil {
		am.Unlock()
		return fmt.Errorf("advisory manager is stopped; could not delete advisory id: %s", id)
	}

	existingAdv, found := am.listenerMap[id]
	if !found {
		am.Unlock()
		return fmt.Errorf("advisory with given ID does not exist: %s", id)
	}

	delete(am.listenerMap, id)
	am.Unlock()

	existingAdv.Stop()
	return nil
}

type jsAdvisoryFSWatcher struct {
	sync.Mutex
	am      *JSAdvisoryManager
	logger  *logrus.Logger
	stopCh  chan struct{}
	watcher *fsnotify.Watcher
}

func newJetStreamAdvisoryFSWatcher(logger *logrus.Logger, am *JSAdvisoryManager) *jsAdvisoryFSWatcher {
	return &jsAdvisoryFSWatcher{
		logger: logger,
		am:     am,
	}
}

func (w *jsAdvisoryFSWatcher) start() error {
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

func (w *jsAdvisoryFSWatcher) stop() {
	w.Lock()
	defer w.Unlock()

	if w.watcher == nil {
		return
	}

	w.stopCh <- struct{}{}
	_ = w.watcher.Close()
	w.watcher = nil
}

func (w *jsAdvisoryFSWatcher) startAdvisoriesInDir() fs.WalkDirFunc {
	return func(path string, info fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// skip directories starting with '..'
		// this prevents double advisory loading when using kubernetes mounts
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

		adv, err := NewJetStreamAdvisoryConfigFromFile(path)
		if err != nil {
			return fmt.Errorf("could not create advisory from path: %s, error: %v", path, err)
		}

		return w.am.Set(adv)
	}
}

func (w *jsAdvisoryFSWatcher) handleWatcherEvent(event fsnotify.Event) error {
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

func (w *jsAdvisoryFSWatcher) handleCreateEvent(path string) error {
	stat, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("could not stat advisory path %s: %s", path, err)
	}

	// if a new directory was created, start advisory in dir
	if stat.IsDir() {
		return filepath.WalkDir(path, w.startAdvisoriesInDir())
	}

	// if not a directory and not a JSON, ignore
	if filepath.Ext(stat.Name()) != ".json" {
		return nil
	}

	// handle as a write event
	return w.handleWriteEvent(path)
}

func (w *jsAdvisoryFSWatcher) handleWriteEvent(path string) error {
	stat, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("could not stat advisory path: %s, error: %v", path, err)
	}

	// if not a JSON file, ignore
	if stat.IsDir() || filepath.Ext(stat.Name()) != ".json" {
		return nil
	}

	config, err := NewJetStreamAdvisoryConfigFromFile(path)
	if err != nil {
		return fmt.Errorf("could not create advisory from path: %s, error: %v", path, err)
	}

	return w.am.Set(config)
}

func (w *jsAdvisoryFSWatcher) handleRemoveEvent(path string) error {
	var removeIDs []string
	configMap := w.am.ConfigMap()
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
			if removeErr := w.am.Delete(removeID); removeErr != nil {
				err = removeErr
			}
		}
	}

	return err
}
