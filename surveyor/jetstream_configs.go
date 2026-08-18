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

	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
)

const (
	// MinJSConfigPollInterval is the smallest accepted JetStream configuration
	// polling interval. Every poll lists all streams and the consumers of each
	// of them, and in a clustered deployment those requests are all served by
	// the meta leader, so polling too often adds load to the very cluster that
	// is being observed.
	MinJSConfigPollInterval = time.Second

	// jsConfigProbeTimeout bounds the JetStream API probe performed when the
	// listener starts.
	jsConfigProbeTimeout = 5 * time.Second
)

var (
	jsStreamLabels         = []string{"stream"}
	jsStreamConfigLabels   = []string{"stream", "discard_policy", "storage_type", "replica_number"}
	jsStreamPeerLabels     = []string{"stream", "peer_name"}
	jsConsumerLabels       = []string{"stream", "consumer_name"}
	jsConsumerConfigLabels = []string{"stream", "consumer_name", "ack_policy", "is_pull"}
	jsConsumerPeerLabels   = []string{"stream", "consumer_name", "peer_name"}
)

// JSConfigMetrics are the metrics exported by the JetStream configuration
// listener. They deliberately only cover what the jsz collector does not
// already export: stream and consumer configuration, per-asset limits and
// per-peer Raft health.
type JSConfigMetrics struct {
	jsConfigPollErrors *CounterVec

	jsStreamConfig            *GaugeVec
	jsStreamDeletedCount      *GaugeVec
	jsStreamLimitMaxMsgs      *GaugeVec
	jsStreamLimitMaxMsgsPer   *GaugeVec
	jsStreamLimitMaxBytes     *GaugeVec
	jsStreamLimitMaxAge       *GaugeVec
	jsStreamLimitMaxMsgSize   *GaugeVec
	jsStreamLimitMaxConsumers *GaugeVec
	jsStreamPeerLeader        *GaugeVec
	jsStreamPeerCurrent       *GaugeVec
	jsStreamPeerOffline       *GaugeVec
	jsStreamReplicationLag    *GaugeVec

	jsConsumerConfig             *GaugeVec
	jsConsumerLimitMaxAckPending *GaugeVec
	jsConsumerPeerLeader         *GaugeVec
	jsConsumerPeerCurrent        *GaugeVec
	jsConsumerPeerOffline        *GaugeVec
	jsConsumerReplicationLag     *GaugeVec
}

// NewJetStreamConfigMetrics creates and registers the metrics used by the
// JetStream configuration listener.
func NewJetStreamConfigMetrics(registry *prometheus.Registry, constLabels prometheus.Labels) *JSConfigMetrics {
	metrics := &JSConfigMetrics{
		jsConfigPollErrors: newCounterVec(
			prometheus.BuildFQName("nats", "jetstream", "config_poll_errors"),
			"Number of JetStream API requests that failed while polling configuration",
			constLabels,
			[]string{"operation"},
		),

		// Streams
		jsStreamConfig: newGaugeVec(
			prometheus.BuildFQName("nats", "jetstream", "stream_configuration"),
			"Configuration of a stream, always 1, the configuration is in the labels",
			constLabels,
			jsStreamConfigLabels,
		),

		jsStreamDeletedCount: newGaugeVec(
			prometheus.BuildFQName("nats", "jetstream", "stream_deleted_count"),
			"Number of messages deleted from a stream out of sequence order",
			constLabels,
			jsStreamLabels,
		),

		jsStreamLimitMaxMsgs: newGaugeVec(
			prometheus.BuildFQName("nats", "jetstream", "stream_limit_max_msgs"),
			"Maximum number of messages a stream will store, -1 for unlimited",
			constLabels,
			jsStreamLabels,
		),

		jsStreamLimitMaxMsgsPer: newGaugeVec(
			prometheus.BuildFQName("nats", "jetstream", "stream_limit_max_msgs_per_subject"),
			"Maximum number of messages per subject a stream will store, -1 for unlimited",
			constLabels,
			jsStreamLabels,
		),

		jsStreamLimitMaxBytes: newGaugeVec(
			prometheus.BuildFQName("nats", "jetstream", "stream_limit_max_bytes"),
			"Maximum number of bytes a stream will store, -1 for unlimited",
			constLabels,
			jsStreamLabels,
		),

		jsStreamLimitMaxAge: newGaugeVec(
			prometheus.BuildFQName("nats", "jetstream", "stream_limit_max_age_seconds"),
			"Maximum age of messages a stream will retain in seconds, 0 for unlimited",
			constLabels,
			jsStreamLabels,
		),

		jsStreamLimitMaxMsgSize: newGaugeVec(
			prometheus.BuildFQName("nats", "jetstream", "stream_limit_max_msg_size"),
			"Maximum size of a single message in bytes, -1 for unlimited",
			constLabels,
			jsStreamLabels,
		),

		jsStreamLimitMaxConsumers: newGaugeVec(
			prometheus.BuildFQName("nats", "jetstream", "stream_limit_max_consumers"),
			"Maximum number of consumers allowed on a stream, -1 for unlimited",
			constLabels,
			jsStreamLabels,
		),

		jsStreamPeerLeader: newGaugeVec(
			prometheus.BuildFQName("nats", "jetstream", "stream_peer_leader"),
			"Whether a peer is the Raft leader of a stream",
			constLabels,
			jsStreamPeerLabels,
		),

		jsStreamPeerCurrent: newGaugeVec(
			prometheus.BuildFQName("nats", "jetstream", "stream_peer_current"),
			"Whether a peer is up to date with the Raft leader of a stream",
			constLabels,
			jsStreamPeerLabels,
		),

		jsStreamPeerOffline: newGaugeVec(
			prometheus.BuildFQName("nats", "jetstream", "stream_peer_offline"),
			"Whether a peer of a stream is considered offline by its Raft group",
			constLabels,
			jsStreamPeerLabels,
		),

		jsStreamReplicationLag: newGaugeVec(
			prometheus.BuildFQName("nats", "jetstream", "stream_replication_lag"),
			"Number of operations a peer of a stream is behind its Raft leader",
			constLabels,
			jsStreamPeerLabels,
		),

		// Consumers
		jsConsumerConfig: newGaugeVec(
			prometheus.BuildFQName("nats", "jetstream", "consumer_configuration"),
			"Configuration of a consumer, always 1, the configuration is in the labels",
			constLabels,
			jsConsumerConfigLabels,
		),

		jsConsumerLimitMaxAckPending: newGaugeVec(
			prometheus.BuildFQName("nats", "jetstream", "consumer_limit_max_ack_pending"),
			"Maximum number of outstanding unacknowledged messages of a consumer",
			constLabels,
			jsConsumerLabels,
		),

		jsConsumerPeerLeader: newGaugeVec(
			prometheus.BuildFQName("nats", "jetstream", "consumer_peer_leader"),
			"Whether a peer is the Raft leader of a consumer",
			constLabels,
			jsConsumerPeerLabels,
		),

		jsConsumerPeerCurrent: newGaugeVec(
			prometheus.BuildFQName("nats", "jetstream", "consumer_peer_current"),
			"Whether a peer is up to date with the Raft leader of a consumer",
			constLabels,
			jsConsumerPeerLabels,
		),

		jsConsumerPeerOffline: newGaugeVec(
			prometheus.BuildFQName("nats", "jetstream", "consumer_peer_offline"),
			"Whether a peer of a consumer is considered offline by its Raft group",
			constLabels,
			jsConsumerPeerLabels,
		),

		jsConsumerReplicationLag: newGaugeVec(
			prometheus.BuildFQName("nats", "jetstream", "consumer_replication_lag"),
			"Number of operations a peer of a consumer is behind its Raft leader",
			constLabels,
			jsConsumerPeerLabels,
		),
	}

	registry.MustRegister(metrics.jsConfigPollErrors)

	registry.MustRegister(metrics.jsStreamConfig)
	registry.MustRegister(metrics.jsStreamDeletedCount)
	registry.MustRegister(metrics.jsStreamLimitMaxMsgs)
	registry.MustRegister(metrics.jsStreamLimitMaxMsgsPer)
	registry.MustRegister(metrics.jsStreamLimitMaxBytes)
	registry.MustRegister(metrics.jsStreamLimitMaxAge)
	registry.MustRegister(metrics.jsStreamLimitMaxMsgSize)
	registry.MustRegister(metrics.jsStreamLimitMaxConsumers)
	registry.MustRegister(metrics.jsStreamPeerLeader)
	registry.MustRegister(metrics.jsStreamPeerCurrent)
	registry.MustRegister(metrics.jsStreamPeerOffline)
	registry.MustRegister(metrics.jsStreamReplicationLag)

	registry.MustRegister(metrics.jsConsumerConfig)
	registry.MustRegister(metrics.jsConsumerLimitMaxAckPending)
	registry.MustRegister(metrics.jsConsumerPeerLeader)
	registry.MustRegister(metrics.jsConsumerPeerCurrent)
	registry.MustRegister(metrics.jsConsumerPeerOffline)
	registry.MustRegister(metrics.jsConsumerReplicationLag)

	return metrics
}

// MetricInfos returns metadata about the metrics used by JSConfigMetrics
func (m *JSConfigMetrics) MetricInfos() []MetricInfo {
	return []MetricInfo{
		m.jsConfigPollErrors,
		m.jsStreamConfig,
		m.jsStreamDeletedCount,
		m.jsStreamLimitMaxMsgs,
		m.jsStreamLimitMaxMsgsPer,
		m.jsStreamLimitMaxBytes,
		m.jsStreamLimitMaxAge,
		m.jsStreamLimitMaxMsgSize,
		m.jsStreamLimitMaxConsumers,
		m.jsStreamPeerLeader,
		m.jsStreamPeerCurrent,
		m.jsStreamPeerOffline,
		m.jsStreamReplicationLag,
		m.jsConsumerConfig,
		m.jsConsumerLimitMaxAckPending,
		m.jsConsumerPeerLeader,
		m.jsConsumerPeerCurrent,
		m.jsConsumerPeerOffline,
		m.jsConsumerReplicationLag,
	}
}

// consumerKey identifies a consumer across polls.
type consumerKey struct {
	stream   string
	consumer string
}

// peerKey identifies a member of a stream or consumer Raft group across polls.
// consumer is empty for stream peers.
type peerKey struct {
	stream   string
	consumer string
	peer     string
}

// pollState records everything a single poll observed, so that assets which
// disappeared since the previous poll can have their series removed instead of
// reporting their last known value forever.
type pollState struct {
	streams       map[string]struct{}
	streamPeers   map[peerKey]struct{}
	consumers     map[consumerKey]struct{}
	consumerPeers map[peerKey]struct{}
}

func newPollState() *pollState {
	return &pollState{
		streams:       make(map[string]struct{}),
		streamPeers:   make(map[peerKey]struct{}),
		consumers:     make(map[consumerKey]struct{}),
		consumerPeers: make(map[peerKey]struct{}),
	}
}

// jsConfigListener polls JetStream stream and consumer configuration and Raft
// state on an interval and exposes the result as prometheus metrics.
type jsConfigListener struct {
	sync.Mutex
	cancelLoop context.CancelFunc
	wg         sync.WaitGroup
	provider   ConnProvider
	logger     *logrus.Logger
	metrics    *JSConfigMetrics
	interval   time.Duration
	conn       Conn
	js         jetstream.JetStream

	// streams caches stream handles so that listing the consumers of a known
	// stream does not cost an extra STREAM.INFO request on every poll.
	streams map[string]jetstream.Stream

	// prev is what the previous complete poll observed.
	prev *pollState
}

func newJetStreamConfigListener(provider ConnProvider, logger *logrus.Logger, metrics *JSConfigMetrics, interval time.Duration) (*jsConfigListener, error) {
	if interval < MinJSConfigPollInterval {
		return nil, fmt.Errorf("jetstream config poll interval %s is below the %s minimum", interval, MinJSConfigPollInterval)
	}
	return &jsConfigListener{
		provider: provider,
		logger:   logger,
		metrics:  metrics,
		interval: interval,
		streams:  make(map[string]jetstream.Stream),
		prev:     newPollState(),
	}, nil
}

// Start connects to NATS and starts the polling loop.
func (o *jsConfigListener) Start(natsCtx *NatsContext) error {
	o.Lock()
	defer o.Unlock()
	if o.conn != nil {
		// already started
		return nil
	}
	conn, err := o.provider.Get(natsCtx)
	if err != nil {
		return fmt.Errorf("nats connection failed: %w", err)
	}
	js, err := jetstream.New(conn.Conn())
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to create jetstream context: %w", err)
	}

	// Creating the context does not talk to the server, so probe the API
	// before reporting success. The surveyor normally runs as the system
	// account, which cannot use the JetStream API at all, and without this
	// check that misconfiguration would only show up as permanently missing
	// metrics.
	probeCtx, cancelProbe := context.WithTimeout(context.Background(), jsConfigProbeTimeout)
	defer cancelProbe()
	if _, err := js.AccountInfo(probeCtx); err != nil {
		conn.Close()
		return fmt.Errorf("jetstream api is unavailable to the configured credentials: %w", err)
	}

	o.conn = conn
	o.js = js

	ctx, cancelFunc := context.WithCancel(context.Background())
	o.cancelLoop = cancelFunc
	o.wg.Add(1)
	go o.loop(ctx)
	return nil
}

// Stop stops the JetStream config polling loop.
func (o *jsConfigListener) Stop() {
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

func (o *jsConfigListener) loop(ctx context.Context) {
	defer o.wg.Done()
	o.logger.Infof("Starting JetStream config poll loop, interval %s", o.interval)

	// A timer rather than a ticker: it is armed again only once a poll has
	// completed, so a poll that outruns the interval delays the next one
	// instead of being followed immediately by a queued tick. The zero delay
	// polls once right away rather than leaving metrics empty for a full
	// interval after startup.
	timer := time.NewTimer(0)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			o.logger.Debugln("Stopping JetStream config poll loop")
			return
		case <-timer.C:
			o.poll(ctx)
			timer.Reset(o.interval)
		}
	}
}

// poll walks every stream and its consumers once and updates the metrics.
func (o *jsConfigListener) poll(ctx context.Context) {
	o.Lock()
	js := o.js
	o.Unlock()
	if js == nil {
		return
	}

	state := newPollState()
	streams := js.ListStreams(ctx)
	for info := range streams.Info() {
		if info == nil {
			o.logger.Warn("Received nil stream info")
			continue
		}
		o.recordStream(info, state)

		if info.State.Consumers == 0 {
			// Nothing to list, and skipping the request spares the meta
			// leader a scatter-gather over a stream with no consumers.
			continue
		}
		if err := o.pollConsumers(ctx, js, info.Config.Name, state); err != nil {
			if ctx.Err() != nil {
				return
			}
			o.metrics.jsConfigPollErrors.WithLabelValues("list_consumers").Inc()
			o.logger.Warnf("Could not list consumers of stream %s: %s", info.Config.Name, err)
			// The consumers of this stream are unknown this cycle, so keep
			// whatever the previous poll saw rather than deleting it below.
			o.carryOverConsumers(info.Config.Name, state)
		}
	}
	if err := streams.Err(); err != nil {
		if ctx.Err() == nil {
			o.metrics.jsConfigPollErrors.WithLabelValues("list_streams").Inc()
			o.logger.Warnf("Could not list JetStream streams: %s", err)
		}
		// The listing is incomplete, so it is impossible to tell an absent
		// stream from an unreported one. Leave every series in place.
		return
	}

	o.pruneStale(state)
}

func (o *jsConfigListener) pollConsumers(ctx context.Context, js jetstream.JetStream, stream string, state *pollState) error {
	str, ok := o.streams[stream]
	if !ok {
		var err error
		if str, err = js.Stream(ctx, stream); err != nil {
			return err
		}
		o.streams[stream] = str
	}

	consumers := str.ListConsumers(ctx)
	for info := range consumers.Info() {
		if info == nil {
			o.logger.Warn("Received nil consumer info")
			continue
		}
		o.recordConsumer(info, state)
	}
	return consumers.Err()
}

// raftPeer is the per-peer view of a Raft group. The leader is included as a
// peer of its own group: it is the server that answered the request, so it is
// by definition online, current and not lagging, and without it there would be
// no series saying which server leads the group.
type raftPeer struct {
	name    string
	leader  bool
	current bool
	offline bool
	lag     uint64
}

func raftPeers(cluster *jetstream.ClusterInfo) []raftPeer {
	// Cluster is nil on a non-clustered deployment, where there is no Raft
	// group to report on.
	if cluster == nil {
		return nil
	}
	peers := make([]raftPeer, 0, len(cluster.Replicas)+1)
	if cluster.Leader != "" {
		peers = append(peers, raftPeer{name: cluster.Leader, leader: true, current: true})
	}
	for _, replica := range cluster.Replicas {
		peers = append(peers, raftPeer{
			name:    replica.Name,
			current: replica.Current,
			offline: replica.Offline,
			lag:     replica.Lag,
		})
	}
	return peers
}

func boolToFloat(value bool) float64 {
	if value {
		return 1
	}
	return 0
}

func isPullBased(config jetstream.ConsumerConfig) bool {
	return config.DeliverSubject == ""
}

func (o *jsConfigListener) recordStream(info *jetstream.StreamInfo, state *pollState) {
	stream := info.Config.Name
	state.streams[stream] = struct{}{}
	labels := prometheus.Labels{"stream": stream}

	// A stream update changes the label values of the configuration series, so
	// the previous one has to be dropped. The gauges below are keyed by stream
	// alone and are simply overwritten.
	o.metrics.jsStreamConfig.DeletePartialMatch(labels)
	o.metrics.jsStreamConfig.With(prometheus.Labels{
		"stream":         stream,
		"discard_policy": info.Config.Discard.String(),
		"storage_type":   info.Config.Storage.String(),
		"replica_number": strconv.Itoa(info.Config.Replicas),
	}).Set(1)

	o.metrics.jsStreamDeletedCount.With(labels).Set(float64(info.State.NumDeleted))
	o.metrics.jsStreamLimitMaxMsgs.With(labels).Set(float64(info.Config.MaxMsgs))
	o.metrics.jsStreamLimitMaxMsgsPer.With(labels).Set(float64(info.Config.MaxMsgsPerSubject))
	o.metrics.jsStreamLimitMaxBytes.With(labels).Set(float64(info.Config.MaxBytes))
	o.metrics.jsStreamLimitMaxAge.With(labels).Set(info.Config.MaxAge.Seconds())
	o.metrics.jsStreamLimitMaxMsgSize.With(labels).Set(float64(info.Config.MaxMsgSize))
	o.metrics.jsStreamLimitMaxConsumers.With(labels).Set(float64(info.Config.MaxConsumers))

	for _, peer := range raftPeers(info.Cluster) {
		state.streamPeers[peerKey{stream: stream, peer: peer.name}] = struct{}{}
		peerLabels := prometheus.Labels{"stream": stream, "peer_name": peer.name}

		o.metrics.jsStreamPeerLeader.With(peerLabels).Set(boolToFloat(peer.leader))
		o.metrics.jsStreamPeerCurrent.With(peerLabels).Set(boolToFloat(peer.current))
		o.metrics.jsStreamPeerOffline.With(peerLabels).Set(boolToFloat(peer.offline))
		o.metrics.jsStreamReplicationLag.With(peerLabels).Set(float64(peer.lag))
	}
}

func (o *jsConfigListener) recordConsumer(info *jetstream.ConsumerInfo, state *pollState) {
	// Config.Name is empty for ephemeral consumers, Name always holds the name
	// the server knows the consumer by.
	stream, consumer := info.Stream, info.Name
	state.consumers[consumerKey{stream: stream, consumer: consumer}] = struct{}{}
	labels := prometheus.Labels{"stream": stream, "consumer_name": consumer}

	o.metrics.jsConsumerConfig.DeletePartialMatch(labels)
	o.metrics.jsConsumerConfig.With(prometheus.Labels{
		"stream":        stream,
		"consumer_name": consumer,
		"ack_policy":    info.Config.AckPolicy.String(),
		"is_pull":       strconv.FormatBool(isPullBased(info.Config)),
	}).Set(1)

	o.metrics.jsConsumerLimitMaxAckPending.With(labels).Set(float64(info.Config.MaxAckPending))

	for _, peer := range raftPeers(info.Cluster) {
		state.consumerPeers[peerKey{stream: stream, consumer: consumer, peer: peer.name}] = struct{}{}
		peerLabels := prometheus.Labels{"stream": stream, "consumer_name": consumer, "peer_name": peer.name}

		o.metrics.jsConsumerPeerLeader.With(peerLabels).Set(boolToFloat(peer.leader))
		o.metrics.jsConsumerPeerCurrent.With(peerLabels).Set(boolToFloat(peer.current))
		o.metrics.jsConsumerPeerOffline.With(peerLabels).Set(boolToFloat(peer.offline))
		o.metrics.jsConsumerReplicationLag.With(peerLabels).Set(float64(peer.lag))
	}
}

// carryOverConsumers copies the previous poll's view of a stream's consumers
// into state, so that a failed consumer listing does not look like the
// consumers were deleted.
func (o *jsConfigListener) carryOverConsumers(stream string, state *pollState) {
	for key := range o.prev.consumers {
		if key.stream == stream {
			state.consumers[key] = struct{}{}
		}
	}
	for key := range o.prev.consumerPeers {
		if key.stream == stream {
			state.consumerPeers[key] = struct{}{}
		}
	}
}

// pruneStale removes the series of every asset that the previous poll saw and
// this one did not, then remembers the new state.
func (o *jsConfigListener) pruneStale(state *pollState) {
	for stream := range o.prev.streams {
		if _, ok := state.streams[stream]; ok {
			continue
		}
		delete(o.streams, stream)
		labels := prometheus.Labels{"stream": stream}
		o.metrics.jsStreamConfig.DeletePartialMatch(labels)
		o.metrics.jsStreamDeletedCount.Delete(labels)
		o.metrics.jsStreamLimitMaxMsgs.Delete(labels)
		o.metrics.jsStreamLimitMaxMsgsPer.Delete(labels)
		o.metrics.jsStreamLimitMaxBytes.Delete(labels)
		o.metrics.jsStreamLimitMaxAge.Delete(labels)
		o.metrics.jsStreamLimitMaxMsgSize.Delete(labels)
		o.metrics.jsStreamLimitMaxConsumers.Delete(labels)
	}

	for key := range o.prev.streamPeers {
		if _, ok := state.streamPeers[key]; ok {
			continue
		}
		labels := prometheus.Labels{"stream": key.stream, "peer_name": key.peer}
		o.metrics.jsStreamPeerLeader.Delete(labels)
		o.metrics.jsStreamPeerCurrent.Delete(labels)
		o.metrics.jsStreamPeerOffline.Delete(labels)
		o.metrics.jsStreamReplicationLag.Delete(labels)
	}

	for key := range o.prev.consumers {
		if _, ok := state.consumers[key]; ok {
			continue
		}
		labels := prometheus.Labels{"stream": key.stream, "consumer_name": key.consumer}
		o.metrics.jsConsumerConfig.DeletePartialMatch(labels)
		o.metrics.jsConsumerLimitMaxAckPending.Delete(labels)
	}

	for key := range o.prev.consumerPeers {
		if _, ok := state.consumerPeers[key]; ok {
			continue
		}
		labels := prometheus.Labels{"stream": key.stream, "consumer_name": key.consumer, "peer_name": key.peer}
		o.metrics.jsConsumerPeerLeader.Delete(labels)
		o.metrics.jsConsumerPeerCurrent.Delete(labels)
		o.metrics.jsConsumerPeerOffline.Delete(labels)
		o.metrics.jsConsumerReplicationLag.Delete(labels)
	}

	o.prev = state
}
