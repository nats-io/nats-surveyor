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
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
)

var (
	//JSStreamList          = `$JS.API.STREAM.LIST`
	streamConfigLabels         = []string{"discard_policy", "storage_type", "replica_number", "stream_name"}
	consumerConfigLabels       = []string{"stream_name", "max_pending_ack", "ack_policy", "is_pull", "consumer_name"}
	streamRaftInfoLabels       = []string{"stream_name", "leader", "cluster_size"}
	streamRaftPeerInfoLabels   = []string{"stream_name", "peer_name", "offline", "current", "leader"}
	consumerRaftInfoLabels     = []string{"consumer_name", "leader", "cluster_size", "stream_name"}
	consumerStateLabels        = []string{"consumer_name", "stream_name", "last_delivered_message_consumer", "last_delivered_message_stream", "ack_floor_consumer", "ack_floor_stream"}
	consumerRaftPeerInfoLabels = []string{"stream_name", "consumer_name", "peer_name", "offline", "current", "leader"}

	DefaultScrapeInterval = 10 * time.Second
	//DefaultListenerID     = "default_listener"
)

type JSStreamConfigMetrics struct {
	jsStreamConfig       *prometheus.GaugeVec
	jsStreamRaftInfo     *prometheus.GaugeVec
	jsStreamRaftPeerInfo *prometheus.GaugeVec

	jsConsumerConfig       *prometheus.GaugeVec
	jsConsumerState        *prometheus.GaugeVec
	jsConsumerRaftInfo     *prometheus.GaugeVec
	jsConsumerRaftPeerInfo *prometheus.GaugeVec
}

func NewJetStreamConfigListMetrics(registry *prometheus.Registry, constLabels prometheus.Labels) *JSStreamConfigMetrics {
	metrics := &JSStreamConfigMetrics{
		// API Audit
		jsStreamConfig: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name:        prometheus.BuildFQName("nats", "jetstream", "stream_configuration"),
			Help:        "Configurations for streams",
			ConstLabels: constLabels,
		}, streamConfigLabels),
		jsConsumerConfig: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name:        prometheus.BuildFQName("nats", "jetstream", "consumer_configuration"),
			Help:        "Configurations for consumer",
			ConstLabels: constLabels,
		}, consumerConfigLabels),
		jsConsumerState: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name:        prometheus.BuildFQName("nats", "jetstream", "consumer_state"),
			Help:        "state of consumer consumer",
			ConstLabels: constLabels,
		}, consumerStateLabels),
		jsStreamRaftInfo: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name:        prometheus.BuildFQName("nats", "jetstream", "stream_raft_info"),
			Help:        "raft info for streams",
			ConstLabels: constLabels,
		}, streamRaftInfoLabels),
		jsStreamRaftPeerInfo: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name:        prometheus.BuildFQName("nats", "jetstream", "stream_raft_peer_info"),
			Help:        "raft peer info for streams",
			ConstLabels: constLabels,
		}, streamRaftPeerInfoLabels),
		jsConsumerRaftInfo: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name:        prometheus.BuildFQName("nats", "jetstream", "consumer_raft_info"),
			Help:        "raft info for consumer",
			ConstLabels: constLabels,
		}, consumerRaftInfoLabels),
		jsConsumerRaftPeerInfo: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name:        prometheus.BuildFQName("nats", "jetstream", "consumer_raft_peer_info"),
			Help:        "raft peer info for consumer",
			ConstLabels: constLabels,
		}, consumerRaftPeerInfoLabels),
	}

	registry.MustRegister(metrics.jsStreamConfig)
	registry.MustRegister(metrics.jsConsumerConfig)
	registry.MustRegister(metrics.jsConsumerState)
	registry.MustRegister(metrics.jsStreamRaftInfo)
	registry.MustRegister(metrics.jsStreamRaftPeerInfo)
	registry.MustRegister(metrics.jsConsumerRaftInfo)
	registry.MustRegister(metrics.jsConsumerRaftPeerInfo)
	return metrics
}

// jsAdvisoryListener listens for JetStream advisories and expose them as prometheus data
type jsConfigListListener struct {
	sync.Mutex
	cancelLoop context.CancelFunc
	cp         *natsConnPool
	logger     *logrus.Logger
	metrics    *JSStreamConfigMetrics
	pc         *pooledNatsConn
	js         nats.JetStreamContext
}

func NewJetStreamConfigListener(cp *natsConnPool, logger *logrus.Logger, metrics *JSStreamConfigMetrics) *jsConfigListListener {

	return &jsConfigListListener{
		cp:      cp,
		logger:  logger,
		metrics: metrics,
	}
}

func (o *jsConfigListListener) gatherData(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	o.logger.Infoln("starting config list listener ticker")
	for {
		select {
		case <-ticker.C:
			o.Lock()
			for str := range o.js.Streams() {
				o.StreamHandler(str)
				for con := range o.js.Consumers(str.Config.Name) {
					o.ConsumerHandler(con)
				}
			}
			o.Unlock()
		// do operation
		case <-ctx.Done():
			fmt.Println("shutting down data gathering")
			return
		}
	}
}

func (o *jsConfigListListener) Start() error {
	o.Lock()
	defer o.Unlock()
	if o.pc != nil {
		// already started
		return nil
	}
	pc, err := o.cp.Get(&natsContext{})
	if err != nil {
		return fmt.Errorf("nats connection failed. error: %v", err)
	}
	o.pc = pc
	js, err := pc.nc.JetStream()
	if err != nil {
		return fmt.Errorf("failed to create jetstream connection")
	}
	o.js = js
	ctx, cancelFunc := context.WithCancel(context.Background())

	o.cancelLoop = cancelFunc
	go o.gatherData(ctx, DefaultScrapeInterval)
	o.logger.Infof("started JetStream Stream List for metric topic")
	return nil
}

func (o *jsConfigListListener) StreamHandler(streamInfo *nats.StreamInfo) {
	if streamInfo == nil {
		o.logger.Infof("received empty stream")
		return
	}
	o.metrics.jsStreamConfig.DeletePartialMatch(prometheus.Labels{
		"stream_name": streamInfo.Config.Name,
	})
	o.metrics.jsStreamConfig.With(
		prometheus.Labels{
			"discard_policy": streamInfo.Config.Discard.String(),
			"storage_type":   streamInfo.Config.Storage.String(),
			"replica_number": strconv.Itoa(streamInfo.Config.Replicas),
			"stream_name":    streamInfo.Config.Name,
		},
	).Set(1)
	o.metrics.jsStreamRaftInfo.DeletePartialMatch(prometheus.Labels{
		"stream_name": streamInfo.Config.Name,
	})
	o.metrics.jsStreamRaftInfo.With(
		prometheus.Labels{
			"stream_name":  streamInfo.Config.Name,
			"leader":       streamInfo.Cluster.Leader,
			"cluster_size": strconv.Itoa(len(streamInfo.Cluster.Replicas)),
		},
	).Set(1)
	o.metrics.jsStreamRaftPeerInfo.DeletePartialMatch(prometheus.Labels{
		"stream_name": streamInfo.Config.Name,
	})
	for _, peer := range streamInfo.Cluster.Replicas {
		o.metrics.jsStreamRaftPeerInfo.With(
			prometheus.Labels{
				"leader":      streamInfo.Cluster.Leader,
				"stream_name": streamInfo.Config.Name,
				"peer_name":   peer.Name,
				"offline":     convertBoolToString(peer.Offline),
				"current":     convertBoolToString(peer.Current),
			},
		).Set(1)
	}
}
func convertBoolToString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
func (o *jsConfigListListener) ConsumerHandler(consumerInfo *nats.ConsumerInfo) {
	if consumerInfo == nil {
		o.logger.Infof("received empty consumer")
		return
	}

	o.metrics.jsConsumerConfig.DeletePartialMatch(prometheus.Labels{
		"stream_name":   consumerInfo.Stream,
		"consumer_name": consumerInfo.Name,
	})
	o.metrics.jsConsumerConfig.With(
		prometheus.Labels{
			"stream_name":     consumerInfo.Stream,
			"consumer_name":   consumerInfo.Name,
			"max_pending_ack": strconv.Itoa(consumerInfo.Config.MaxAckPending),
			"ack_policy":      consumerInfo.Config.AckPolicy.String(),
			"is_pull":         IsPullBased(consumerInfo),
		},
	).Set(1)

	o.metrics.jsConsumerState.DeletePartialMatch(prometheus.Labels{
		"stream_name":   consumerInfo.Stream,
		"consumer_name": consumerInfo.Name,
	})
	o.metrics.jsConsumerState.With(
		prometheus.Labels{
			"consumer_name":                   consumerInfo.Name,
			"stream_name":                     consumerInfo.Stream,
			"last_delivered_message_consumer": strconv.FormatUint(consumerInfo.Delivered.Consumer, 10),
			"last_delivered_message_stream":   strconv.FormatUint(consumerInfo.Delivered.Stream, 10),
			"ack_floor_consumer":              strconv.FormatUint(consumerInfo.AckFloor.Consumer, 10),
			"ack_floor_stream":                strconv.FormatUint(consumerInfo.AckFloor.Stream, 10),
		}).Set(1)

	o.metrics.jsConsumerRaftInfo.DeletePartialMatch(prometheus.Labels{
		"stream_name":   consumerInfo.Stream,
		"consumer_name": consumerInfo.Name,
	})
	o.metrics.jsConsumerRaftInfo.With(
		prometheus.Labels{
			"consumer_name": consumerInfo.Name,
			"stream_name":   consumerInfo.Stream,
			"leader":        consumerInfo.Cluster.Leader,
			"cluster_size":  strconv.Itoa(len(consumerInfo.Cluster.Replicas)),
		},
	).Set(1)

	o.metrics.jsConsumerRaftPeerInfo.DeletePartialMatch(prometheus.Labels{
		"stream_name":   consumerInfo.Stream,
		"consumer_name": consumerInfo.Name,
	})
	for _, peer := range consumerInfo.Cluster.Replicas {
		o.metrics.jsConsumerRaftPeerInfo.With(
			prometheus.Labels{
				"leader":        consumerInfo.Cluster.Leader,
				"stream_name":   consumerInfo.Stream,
				"consumer_name": consumerInfo.Name,
				"peer_name":     peer.Name,
				"offline":       convertBoolToString(peer.Offline),
				"current":       convertBoolToString(peer.Current),
			},
		).Set(1)
	}
}
func IsPullBased(info *nats.ConsumerInfo) string {
	isPull := info.Config.DeliverGroup == "" && info.Config.DeliverSubject == ""
	if isPull {
		return "true"
	}
	return "false"
}

// Stop stops listening for JetStream advisories
func (o *jsConfigListListener) Stop() {
	o.Lock()
	defer o.Unlock()
	if o.pc == nil {
		// already stopped
		return
	}
	o.cancelLoop()
	o.pc.ReturnToPool()
	o.pc = nil
}

//
//// JSConfigListManager exposes methods to operate on JetStream advisories
//type JSConfigListManager struct {
//	sync.Mutex
//	cp          *natsConnPool
//	natsCtx     *natsContext
//	listenerMap map[string]*jsConfigListListener
//	logger      *logrus.Logger
//	metrics     *JSStreamConfigMetrics
//}
//
//// newJetStreamAdvisoryManager creates a JSAdvisoryManager for managing JetStream advisories
//func newJetStreamConfigListManager(cp *natsConnPool, logger *logrus.Logger, metrics *JSStreamConfigMetrics) *JSConfigListManager {
//	return &JSConfigListManager{
//		cp:      cp,
//		logger:  logger,
//		metrics: metrics,
//	}
//}
//func (am *JSConfigListManager) addNatsContext(natsCtx *natsContext) {
//	am.natsCtx = natsCtx
//}
//func (am *JSConfigListManager) Init() {
//	am.Lock()
//	defer am.Unlock()
//	if am.listenerMap != nil {
//		// already started
//		return
//	}
//
//	am.listenerMap = map[string]*jsConfigListListener{}
//}
//
//// IsRunning returns true if the advisory manager is running or false if it is stopped
//func (am *JSConfigListManager) IsRunning() bool {
//	am.Lock()
//	defer am.Unlock()
//	return am.listenerMap != nil
//}
//
//func (am *JSConfigListManager) stop() {
//	am.Lock()
//	defer am.Unlock()
//	if am.listenerMap == nil {
//		// already stopped
//		return
//	}
//
//	for _, listener := range am.listenerMap {
//		listener.Stop()
//	}
//	am.listenerMap = nil
//}
//
//// Start creates or updates an JSConfigListManager
//// if a listener exists with the same ID, it is updated
//// otherwise, a new advisory is created
//func (am *JSConfigListManager) Start() error {
//	am.Lock()
//	if am.listenerMap == nil {
//		am.Unlock()
//		return fmt.Errorf("config list manager is stopped; could not set config list")
//	}
//
//	existingConfMan, found := am.listenerMap[DefaultListenerID]
//	am.Unlock()
//
//	strListMan, err := newJetStreamConfigListener(am.natsCtx, am.cp, am.logger, am.metrics)
//	if err != nil {
//		return fmt.Errorf("could not set config list. error: %v", err)
//	}
//
//	if err := strListMan.Start(); err != nil {
//		return fmt.Errorf("could not start config list manager. error: %v", err)
//	}
//
//	am.Lock()
//	if am.listenerMap == nil {
//		am.Unlock()
//		strListMan.Stop()
//		return fmt.Errorf("config list manager is stopped; could not set config list manager")
//	}
//
//	am.listenerMap[DefaultListenerID] = strListMan
//	am.Unlock()
//
//	if found {
//		existingConfMan.Stop()
//	}
//	return nil
//}
//
//// Delete deletes existing advisory with provided ID
//func (am *JSConfigListManager) Delete(id string) error {
//	am.Lock()
//	if am.listenerMap == nil {
//		am.Unlock()
//		return fmt.Errorf("stream list manager is stopped; could not delete advisory id: %s", id)
//	}
//
//	strListMan, found := am.listenerMap[id]
//	if !found {
//		am.Unlock()
//		return fmt.Errorf("stream list manager with given ID does not exist: %s", id)
//	}
//
//	delete(am.listenerMap, id)
//	am.Unlock()
//
//	strListMan.Stop()
//	return nil
//}
