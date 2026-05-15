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
	streamConfigLabels           = []string{"discard_policy", "storage_type", "replica_number", "stream_name"}
	consumerConfigLabels         = []string{"stream_name", "max_pending_ack", "ack_policy", "is_pull", "consumer_name"}
	streamRaftInfoLabels         = []string{"stream_name", "leader", "replica_count"}
	streamRaftPeerInfoLabels     = []string{"stream_name", "peer_name", "offline", "current", "leader"}
	consumerRaftInfoLabels       = []string{"consumer_name", "leader", "replica_count", "stream_name"}
	consumerStateLabels          = []string{"consumer_name", "stream_name"}
	consumerRaftPeerInfoLabels   = []string{"stream_name", "consumer_name", "peer_name", "offline", "current", "leader"}
	streamReplicationLagLabels   = []string{"stream_name", "peer_name"}
	consumerReplicationLagLabels = []string{"stream_name", "consumer_name", "peer_name"}
	streamLabel                  = []string{"stream_name"}
	consumerStreamLabel          = []string{"consumer_name", "stream_name"}
)

// DefaultJSConfigPollInterval is the polling interval used when the listener is
// enabled without an explicit interval.
const DefaultJSConfigPollInterval = 10 * time.Second

type JSStreamConfigMetrics struct {
	jsStreamConfig           *prometheus.GaugeVec
	jsStreamRaftInfo         *prometheus.GaugeVec
	jsStreamRaftPeerInfo     *prometheus.GaugeVec
	jsStreamReplicationLag   *prometheus.GaugeVec
	jsStreamStateConsumerNum *prometheus.GaugeVec
	jsStreamStateDeletedNum  *prometheus.GaugeVec
	jsStreamStateSubjectNum  *prometheus.GaugeVec

	jsConsumerConfig                 *prometheus.GaugeVec
	jsConsumerState                  *prometheus.GaugeVec
	jsConsumerRaftInfo               *prometheus.GaugeVec
	jsConsumerRaftPeerInfo           *prometheus.GaugeVec
	jsConsumerReplicationLag         *prometheus.GaugeVec
	jsConsumerAckPendingNum          *prometheus.GaugeVec
	jsConsumerPendingNum             *prometheus.GaugeVec
	jsConsumerRedeliveredNum         *prometheus.GaugeVec
	jsConsumerWaitingNum             *prometheus.GaugeVec
	jsConsumerLastAckFloorConsumer   *prometheus.GaugeVec
	jsConsumerLastAckFloorStream     *prometheus.GaugeVec
	jsConsumerLastDeliveredConsumer  *prometheus.GaugeVec
	jsConsumerLastDeliveredStream    *prometheus.GaugeVec

	jsStreamLimitMaxMsgs      *prometheus.GaugeVec
	jsStreamLimitMaxMsgsPer   *prometheus.GaugeVec
	jsStreamLimitMaxBytes     *prometheus.GaugeVec
	jsStreamLimitMaxAge       *prometheus.GaugeVec
	jsStreamLimitMaxMsgSize   *prometheus.GaugeVec
	jsStreamLimitMaxConsumers *prometheus.GaugeVec
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
		jsStreamStateConsumerNum: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name:        prometheus.BuildFQName("nats", "jetstream", "stream_consumer_number"),
			Help:        "consumer number of stream",
			ConstLabels: constLabels,
		}, streamLabel),
		jsStreamStateDeletedNum: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name:        prometheus.BuildFQName("nats", "jetstream", "stream_deleted_number"),
			Help:        "deleted number of stream",
			ConstLabels: constLabels,
		}, streamLabel),
		jsStreamStateSubjectNum: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name:        prometheus.BuildFQName("nats", "jetstream", "stream_subject_number"),
			Help:        "subject number for streams",
			ConstLabels: constLabels,
		}, streamLabel),
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
		jsStreamReplicationLag: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name:        prometheus.BuildFQName("nats", "jetstream", "stream_replication_lag"),
			Help:        "replication lag of stream peers",
			ConstLabels: constLabels,
		}, streamReplicationLagLabels),
		jsConsumerReplicationLag: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name:        prometheus.BuildFQName("nats", "jetstream", "consumer_replication_lag"),
			Help:        "replication lag of consumer peers",
			ConstLabels: constLabels,
		}, consumerReplicationLagLabels),
		jsConsumerAckPendingNum: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name:        prometheus.BuildFQName("nats", "jetstream", "consumer_ack_pending_number"),
			Help:        "pending ack number of consumer",
			ConstLabels: constLabels,
		}, consumerStreamLabel),
		jsConsumerPendingNum: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name:        prometheus.BuildFQName("nats", "jetstream", "consumer_pending_number"),
			Help:        "pending number of consumer",
			ConstLabels: constLabels,
		}, consumerStreamLabel),
		jsConsumerRedeliveredNum: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name:        prometheus.BuildFQName("nats", "jetstream", "consumer_redelivered_number"),
			Help:        "redelivered number of consumer",
			ConstLabels: constLabels,
		}, consumerStreamLabel),
		jsConsumerWaitingNum: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name:        prometheus.BuildFQName("nats", "jetstream", "consumer_waiting_number"),
			Help:        "waiting number of consumer",
			ConstLabels: constLabels,
		}, consumerStreamLabel),
		jsConsumerLastAckFloorConsumer: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name:        prometheus.BuildFQName("nats", "jetstream", "consumer_last_ack_floor_consumer_seq"),
			Help:        "Consumer-scoped sequence number of the last acknowledged message",
			ConstLabels: constLabels,
		}, consumerStreamLabel),
		jsConsumerLastAckFloorStream: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name:        prometheus.BuildFQName("nats", "jetstream", "consumer_last_ack_floor_stream_seq"),
			Help:        "Stream-scoped sequence number of the last acknowledged message",
			ConstLabels: constLabels,
		}, consumerStreamLabel),
		jsConsumerLastDeliveredConsumer: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name:        prometheus.BuildFQName("nats", "jetstream", "consumer_last_delivered_consumer_seq"),
			Help:        "Consumer-scoped sequence number of the last delivered message",
			ConstLabels: constLabels,
		}, consumerStreamLabel),
		jsConsumerLastDeliveredStream: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name:        prometheus.BuildFQName("nats", "jetstream", "consumer_last_delivered_stream_seq"),
			Help:        "Stream-scoped sequence number of the last delivered message",
			ConstLabels: constLabels,
		}, consumerStreamLabel),
		jsStreamLimitMaxMsgs: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name:        prometheus.BuildFQName("nats", "jetstream", "stream_limit_max_msgs"),
			Help:        "Maximum number of messages allowed in the stream (-1 for unlimited)",
			ConstLabels: constLabels,
		}, streamLabel),
		jsStreamLimitMaxMsgsPer: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name:        prometheus.BuildFQName("nats", "jetstream", "stream_limit_max_msgs_per_subject"),
			Help:        "Maximum number of messages per subject allowed in the stream (-1 for unlimited)",
			ConstLabels: constLabels,
		}, streamLabel),
		jsStreamLimitMaxBytes: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name:        prometheus.BuildFQName("nats", "jetstream", "stream_limit_max_bytes"),
			Help:        "Maximum total bytes allowed in the stream (-1 for unlimited)",
			ConstLabels: constLabels,
		}, streamLabel),
		jsStreamLimitMaxAge: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name:        prometheus.BuildFQName("nats", "jetstream", "stream_limit_max_age_seconds"),
			Help:        "Maximum age of messages in seconds (0 for unlimited)",
			ConstLabels: constLabels,
		}, streamLabel),
		jsStreamLimitMaxMsgSize: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name:        prometheus.BuildFQName("nats", "jetstream", "stream_limit_max_msg_size"),
			Help:        "Maximum message size in bytes (-1 for unlimited)",
			ConstLabels: constLabels,
		}, streamLabel),
		jsStreamLimitMaxConsumers: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name:        prometheus.BuildFQName("nats", "jetstream", "stream_limit_max_consumers"),
			Help:        "Maximum number of consumers allowed (-1 for unlimited)",
			ConstLabels: constLabels,
		}, streamLabel),
	}

	registry.MustRegister(metrics.jsStreamConfig)
	registry.MustRegister(metrics.jsConsumerConfig)
	registry.MustRegister(metrics.jsConsumerState)
	registry.MustRegister(metrics.jsStreamRaftInfo)
	registry.MustRegister(metrics.jsStreamRaftPeerInfo)
	registry.MustRegister(metrics.jsStreamStateSubjectNum)
	registry.MustRegister(metrics.jsStreamStateDeletedNum)
	registry.MustRegister(metrics.jsStreamStateConsumerNum)

	registry.MustRegister(metrics.jsConsumerRaftInfo)
	registry.MustRegister(metrics.jsConsumerRaftPeerInfo)
	registry.MustRegister(metrics.jsStreamReplicationLag)
	registry.MustRegister(metrics.jsConsumerReplicationLag)
	registry.MustRegister(metrics.jsConsumerWaitingNum)
	registry.MustRegister(metrics.jsConsumerAckPendingNum)
	registry.MustRegister(metrics.jsConsumerPendingNum)
	registry.MustRegister(metrics.jsConsumerRedeliveredNum)
	registry.MustRegister(metrics.jsConsumerLastDeliveredConsumer)
	registry.MustRegister(metrics.jsConsumerLastDeliveredStream)
	registry.MustRegister(metrics.jsConsumerLastAckFloorConsumer)
	registry.MustRegister(metrics.jsConsumerLastAckFloorStream)

	registry.MustRegister(metrics.jsStreamLimitMaxMsgs)
	registry.MustRegister(metrics.jsStreamLimitMaxMsgsPer)
	registry.MustRegister(metrics.jsStreamLimitMaxBytes)
	registry.MustRegister(metrics.jsStreamLimitMaxAge)
	registry.MustRegister(metrics.jsStreamLimitMaxMsgSize)
	registry.MustRegister(metrics.jsStreamLimitMaxConsumers)

	return metrics
}

// jsConfigListListener polls JetStream stream/consumer configuration and state on an
// interval and exposes the result as prometheus metrics.
type jsConfigListListener struct {
	sync.Mutex
	cancelLoop context.CancelFunc
	wg         sync.WaitGroup
	provider   ConnProvider
	logger     *logrus.Logger
	metrics    *JSStreamConfigMetrics
	interval   time.Duration
	conn       Conn
	js         nats.JetStreamContext
}

func NewJetStreamConfigListener(provider ConnProvider, logger *logrus.Logger, metrics *JSStreamConfigMetrics, interval time.Duration) *jsConfigListListener {
	if interval <= 0 {
		interval = DefaultJSConfigPollInterval
	}
	return &jsConfigListListener{
		provider: provider,
		logger:   logger,
		metrics:  metrics,
		interval: interval,
	}
}

func (o *jsConfigListListener) gatherData(ctx context.Context, interval time.Duration) {
	defer o.wg.Done()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	o.logger.Infof("starting JetStream config poll loop (interval=%s)", interval)
	for {
		select {
		case <-ticker.C:
			// Snapshot js under the lock to avoid holding it across blocking NATS I/O.
			o.Lock()
			js := o.js
			o.Unlock()
			if js == nil || ctx.Err() != nil {
				return
			}
			for str := range js.Streams() {
				if ctx.Err() != nil {
					return
				}
				o.StreamHandler(str)
				for con := range js.Consumers(str.Config.Name) {
					if ctx.Err() != nil {
						return
					}
					o.ConsumerHandler(con)
				}
			}
		case <-ctx.Done():
			o.logger.Debugln("stopping JetStream config poll loop")
			return
		}
	}
}

func (o *jsConfigListListener) Start(natsCtx *NatsContext) error {
	o.Lock()
	defer o.Unlock()
	if o.conn != nil {
		// already started
		return nil
	}
	conn, err := o.provider.Get(natsCtx)
	if err != nil {
		return fmt.Errorf("nats connection failed: %v", err)
	}
	o.conn = conn
	js, err := conn.Conn().JetStream()
	if err != nil {
		o.conn.Close()
		o.conn = nil
		return fmt.Errorf("failed to create jetstream context: %v", err)
	}
	o.js = js
	ctx, cancelFunc := context.WithCancel(context.Background())

	o.cancelLoop = cancelFunc
	o.wg.Add(1)
	go o.gatherData(ctx, o.interval)
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
	o.metrics.jsStreamRaftPeerInfo.DeletePartialMatch(prometheus.Labels{
		"stream_name": streamInfo.Config.Name,
	})
	o.metrics.jsStreamReplicationLag.DeletePartialMatch(
		prometheus.Labels{
			"stream_name": streamInfo.Config.Name,
		},
	)

	if streamInfo.Cluster != nil {
		o.metrics.jsStreamRaftInfo.With(
			prometheus.Labels{
				"stream_name":   streamInfo.Config.Name,
				"leader":        streamInfo.Cluster.Leader,
				"replica_count": strconv.Itoa(len(streamInfo.Cluster.Replicas)),
			},
		).Set(1)

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

			o.metrics.jsStreamReplicationLag.With(
				prometheus.Labels{
					"stream_name": streamInfo.Config.Name,
					"peer_name":   peer.Name,
				},
			).Set(float64(peer.Lag))
		}
	}

	o.metrics.jsStreamStateConsumerNum.DeletePartialMatch(prometheus.Labels{
		"stream_name": streamInfo.Config.Name,
	})
	o.metrics.jsStreamStateSubjectNum.DeletePartialMatch(prometheus.Labels{
		"stream_name": streamInfo.Config.Name,
	})
	o.metrics.jsStreamStateDeletedNum.DeletePartialMatch(prometheus.Labels{
		"stream_name": streamInfo.Config.Name,
	})

	o.metrics.jsStreamStateSubjectNum.With(
		prometheus.Labels{
			"stream_name": streamInfo.Config.Name,
		},
	).Set(float64(streamInfo.State.NumSubjects))
	o.metrics.jsStreamStateDeletedNum.With(
		prometheus.Labels{
			"stream_name": streamInfo.Config.Name,
		},
	).Set(float64(streamInfo.State.NumDeleted))
	o.metrics.jsStreamStateConsumerNum.With(
		prometheus.Labels{
			"stream_name": streamInfo.Config.Name,
		},
	).Set(float64(streamInfo.State.Consumers))

	// Stream Limits
	o.metrics.jsStreamLimitMaxMsgs.DeletePartialMatch(prometheus.Labels{
		"stream_name": streamInfo.Config.Name,
	})
	o.metrics.jsStreamLimitMaxMsgsPer.DeletePartialMatch(prometheus.Labels{
		"stream_name": streamInfo.Config.Name,
	})
	o.metrics.jsStreamLimitMaxBytes.DeletePartialMatch(prometheus.Labels{
		"stream_name": streamInfo.Config.Name,
	})
	o.metrics.jsStreamLimitMaxAge.DeletePartialMatch(prometheus.Labels{
		"stream_name": streamInfo.Config.Name,
	})
	o.metrics.jsStreamLimitMaxMsgSize.DeletePartialMatch(prometheus.Labels{
		"stream_name": streamInfo.Config.Name,
	})
	o.metrics.jsStreamLimitMaxConsumers.DeletePartialMatch(prometheus.Labels{
		"stream_name": streamInfo.Config.Name,
	})

	o.metrics.jsStreamLimitMaxMsgs.With(
		prometheus.Labels{
			"stream_name": streamInfo.Config.Name,
		},
	).Set(float64(streamInfo.Config.MaxMsgs))

	o.metrics.jsStreamLimitMaxMsgsPer.With(
		prometheus.Labels{
			"stream_name": streamInfo.Config.Name,
		},
	).Set(float64(streamInfo.Config.MaxMsgsPerSubject))

	o.metrics.jsStreamLimitMaxBytes.With(
		prometheus.Labels{
			"stream_name": streamInfo.Config.Name,
		},
	).Set(float64(streamInfo.Config.MaxBytes))

	o.metrics.jsStreamLimitMaxAge.With(
		prometheus.Labels{
			"stream_name": streamInfo.Config.Name,
		},
	).Set(streamInfo.Config.MaxAge.Seconds())

	o.metrics.jsStreamLimitMaxMsgSize.With(
		prometheus.Labels{
			"stream_name": streamInfo.Config.Name,
		},
	).Set(float64(streamInfo.Config.MaxMsgSize))

	o.metrics.jsStreamLimitMaxConsumers.With(
		prometheus.Labels{
			"stream_name": streamInfo.Config.Name,
		},
	).Set(float64(streamInfo.Config.MaxConsumers))
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
	consumerLabels := prometheus.Labels{
		"stream_name":   consumerInfo.Stream,
		"consumer_name": consumerInfo.Name,
	}

	o.metrics.jsConsumerConfig.DeletePartialMatch(consumerLabels)
	o.metrics.jsConsumerConfig.With(
		prometheus.Labels{
			"stream_name":     consumerInfo.Stream,
			"consumer_name":   consumerInfo.Name,
			"max_pending_ack": strconv.Itoa(consumerInfo.Config.MaxAckPending),
			"ack_policy":      consumerInfo.Config.AckPolicy.String(),
			"is_pull":         IsPullBased(consumerInfo),
		},
	).Set(1)

	o.metrics.jsConsumerState.DeletePartialMatch(consumerLabels)
	o.metrics.jsConsumerState.With(consumerLabels).Set(1)

	o.metrics.jsConsumerWaitingNum.DeletePartialMatch(consumerLabels)
	o.metrics.jsConsumerPendingNum.DeletePartialMatch(consumerLabels)
	o.metrics.jsConsumerAckPendingNum.DeletePartialMatch(consumerLabels)
	o.metrics.jsConsumerRedeliveredNum.DeletePartialMatch(consumerLabels)
	o.metrics.jsConsumerLastDeliveredConsumer.DeletePartialMatch(consumerLabels)
	o.metrics.jsConsumerLastDeliveredStream.DeletePartialMatch(consumerLabels)
	o.metrics.jsConsumerLastAckFloorConsumer.DeletePartialMatch(consumerLabels)
	o.metrics.jsConsumerLastAckFloorStream.DeletePartialMatch(consumerLabels)

	o.metrics.jsConsumerWaitingNum.With(consumerLabels).Set(float64(consumerInfo.NumWaiting))
	o.metrics.jsConsumerPendingNum.With(consumerLabels).Set(float64(consumerInfo.NumPending))
	o.metrics.jsConsumerAckPendingNum.With(consumerLabels).Set(float64(consumerInfo.NumAckPending))
	o.metrics.jsConsumerRedeliveredNum.With(consumerLabels).Set(float64(consumerInfo.NumRedelivered))
	o.metrics.jsConsumerLastDeliveredConsumer.With(consumerLabels).Set(float64(consumerInfo.Delivered.Consumer))
	o.metrics.jsConsumerLastDeliveredStream.With(consumerLabels).Set(float64(consumerInfo.Delivered.Stream))
	o.metrics.jsConsumerLastAckFloorConsumer.With(consumerLabels).Set(float64(consumerInfo.AckFloor.Consumer))
	o.metrics.jsConsumerLastAckFloorStream.With(consumerLabels).Set(float64(consumerInfo.AckFloor.Stream))

	o.metrics.jsConsumerRaftInfo.DeletePartialMatch(consumerLabels)
	o.metrics.jsConsumerRaftPeerInfo.DeletePartialMatch(consumerLabels)
	o.metrics.jsConsumerReplicationLag.DeletePartialMatch(consumerLabels)

	if consumerInfo.Cluster == nil {
		// non-clustered (single-server or R1) deployment — no raft/peer data
		return
	}
	o.metrics.jsConsumerRaftInfo.With(
		prometheus.Labels{
			"consumer_name": consumerInfo.Name,
			"stream_name":   consumerInfo.Stream,
			"leader":        consumerInfo.Cluster.Leader,
			"replica_count": strconv.Itoa(len(consumerInfo.Cluster.Replicas)),
		},
	).Set(1)
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
		o.metrics.jsConsumerReplicationLag.With(
			prometheus.Labels{
				"stream_name":   consumerInfo.Stream,
				"consumer_name": consumerInfo.Name,
				"peer_name":     peer.Name,
			},
		).Set(float64(peer.Lag))
	}
}
func IsPullBased(info *nats.ConsumerInfo) string {
	isPull := info.Config.DeliverGroup == "" && info.Config.DeliverSubject == ""
	if isPull {
		return "true"
	}
	return "false"
}

// Stop stops the JetStream config polling loop.
func (o *jsConfigListListener) Stop() {
	o.Lock()
	if o.conn == nil {
		// already stopped
		o.Unlock()
		return
	}
	cancel := o.cancelLoop
	o.Unlock()

	cancel()
	o.wg.Wait()

	o.Lock()
	if o.conn != nil {
		o.conn.Close()
		o.conn = nil
		o.js = nil
	}
	o.Unlock()
}
