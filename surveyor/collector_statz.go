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
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/klauspost/compress/s2"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/singleflight"
)

// skip reporting account on a server after this many subsequent polls with 0 conns
const accStatZeroConnSkip = 3

type CollectJsz string

const (
	CollectJszNone      CollectJsz = ""
	CollectJszAll       CollectJsz = "all"
	CollectJszStreams   CollectJsz = "streams"
	CollectJszConsumers CollectJsz = "consumers"
)

// statzDescs holds the metric descriptions
type statzDescs struct {
	Info             *GaugeVec
	Start            *GaugeVec
	Uptime           *GaugeVec
	Mem              *GaugeVec
	GoMemLimit       *GaugeVec
	Cores            *GaugeVec
	CPU              *GaugeVec
	Connections      *GaugeVec
	TotalConnections *CounterVec
	ActiveAccounts   *GaugeVec
	NumSubs          *GaugeVec
	SentMsgs         *CounterVec
	SentBytes        *CounterVec
	RecvMsgs         *CounterVec
	RecvBytes        *CounterVec
	SlowConsumers    *GaugeVec
	RTT              *GaugeVec
	Routes           *GaugeVec
	Gateways         *GaugeVec

	// Routes
	RouteSentMsgs  *CounterVec
	RouteSentBytes *CounterVec
	RouteRecvMsgs  *CounterVec
	RouteRecvBytes *CounterVec
	RoutePending   *GaugeVec

	// Gateways
	GatewaySentMsgs   *CounterVec
	GatewaySentBytes  *CounterVec
	GatewayRecvMsgs   *CounterVec
	GatewayRecvBytes  *CounterVec
	GatewayNumInbound *GaugeVec
	OutboundGateways  *gatewayzDescs
	InboundGateways   *gatewayzDescs

	// Jetstream Info
	JetstreamInfo *GaugeVec
	// Jetstream Server
	JetstreamEnabled                    *GaugeVec
	JetstreamFilestoreSizeBytes         *GaugeVec
	JetstreamMemstoreSizeBytes          *GaugeVec
	JetstreamFilestoreUsedBytes         *GaugeVec
	JetstreamFilestoreReservedBytes     *GaugeVec
	JetstreamFilestoreReservedUsedBytes *GaugeVec
	JetstreamMemstoreUsedBytes          *GaugeVec
	JetstreamMemstoreReservedBytes      *GaugeVec
	JetstreamMemstoreReservedUsedBytes  *GaugeVec
	JetstreamAccounts                   *GaugeVec
	JetstreamHAAssets                   *GaugeVec
	JetstreamAPIRequests                *CounterVec
	JetstreamAPIPending                 *GaugeVec
	JetstreamAPIErrors                  *CounterVec
	// Jetstream Cluster
	JetstreamClusterRaftGroupInfo     *GaugeVec
	JetstreamClusterRaftGroupSize     *GaugeVec
	JetstreamClusterRaftGroupLeader   *GaugeVec
	JetstreamClusterRaftGroupReplicas *GaugeVec
	// Jetstream Cluster Replicas
	JetstreamClusterRaftGroupReplicaActive  *GaugeVec
	JetstreamClusterRaftGroupReplicaCurrent *GaugeVec
	JetstreamClusterRaftGroupReplicaOffline *GaugeVec
	// JetStream server stats
	JetstreamServerDisabled   *GaugeVec
	JetstreamServerStreams    *GaugeVec
	JetstreamServerConsumers  *GaugeVec
	JetstreamServerMessages   *GaugeVec
	JetstreamServerBytes      *GaugeVec
	JetstreamServerMaxMemory  *GaugeVec
	JetstreamServerMaxStorage *GaugeVec

	// Account scope metrics
	accCount             *GaugeVec
	accConnCount         *GaugeVec
	accTotalConnCount    *GaugeVec
	accLeafCount         *GaugeVec
	accSubCount          *GaugeVec
	accSlowConsumerCount *GaugeVec

	// Bytes and messages sent and received
	accBytesSent        *CounterVec
	accBytesRecv        *CounterVec
	accMsgsSent         *CounterVec
	accMsgsRecv         *CounterVec
	accClientBytesSent  *CounterVec
	accClientBytesRecv  *CounterVec
	accClientMsgsSent   *CounterVec
	accClientMsgsRecv   *CounterVec
	accLeafBytesSent    *CounterVec
	accLeafBytesRecv    *CounterVec
	accLeafMsgsSent     *CounterVec
	accLeafMsgsRecv     *CounterVec
	accRouteBytesSent   *CounterVec
	accRouteBytesRecv   *CounterVec
	accRouteMsgsSent    *CounterVec
	accRouteMsgsRecv    *CounterVec
	accGatewayBytesSent *CounterVec
	accGatewayBytesRecv *CounterVec
	accGatewayMsgsSent  *CounterVec
	accGatewayMsgsRecv  *CounterVec

	accJetstreamEnabled               *GaugeVec
	accJetstreamMemoryUsed            *GaugeVec
	accJetstreamStorageUsed           *GaugeVec
	accJetstreamMemoryReserved        *GaugeVec
	accJetstreamStorageReserved       *GaugeVec
	accJetstreamTieredMemoryUsed      *GaugeVec
	accJetstreamTieredStorageUsed     *GaugeVec
	accJetstreamTieredMemoryReserved  *GaugeVec
	accJetstreamTieredStorageReserved *GaugeVec
	accJetstreamStreamCount           *GaugeVec
	accJetstreamConsumerCount         *GaugeVec
	accJetstreamReplicaCount          *GaugeVec

	// JSZ Stream metrics.
	accJszStreamMsgs          *GaugeVec
	accJszStreamBytes         *GaugeVec
	accJszStreamFirstSeq      *GaugeVec
	accJszStreamLastSeq       *GaugeVec
	accJszStreamConsumerCount *GaugeVec
	accJszStreamSubjectCount  *GaugeVec

	// JSZ Consumer metrics.
	accJszConsumerDeliveredStreamSeq   *GaugeVec
	accJszConsumerDeliveredConsumerSeq *GaugeVec
	accJszConsumerNumAckPending        *GaugeVec
	accJszConsumerNumRedelivered       *GaugeVec
	accJszConsumerNumWaiting           *GaugeVec
	accJszConsumerNumPending           *GaugeVec
	accJszConsumerAckFloorStreamSeq    *GaugeVec
	accJszConsumerAckFloorConsumerSeq  *GaugeVec
}

// gatewayzDescs holds the gateway metric descriptions
type gatewayzDescs struct {
	configured        *GaugeVec
	connStart         *GaugeVec
	connLastActivity  *GaugeVec
	connUptime        *GaugeVec
	connIdle          *GaugeVec
	connRtt           *GaugeVec
	connPendingBytes  *GaugeVec
	connInMsgs        *GaugeVec
	connOutMsgs       *GaugeVec
	connInBytes       *GaugeVec
	connOutBytes      *GaugeVec
	connSubscriptions *GaugeVec
}

// StatzCollector collects statz from a server deployment
type StatzCollector struct {
	sync.Mutex
	flightGroup             singleflight.Group
	nc                      *nats.Conn
	logger                  *logrus.Logger
	start                   time.Time
	uptime                  time.Duration
	stats                   []*server.ServerStatsMsg
	statsChan               chan *server.ServerStatsMsg
	jsStats                 []*jsStat
	accStats                []*accountStats
	gatewayStatz            []*gatewayStatz
	rtts                    map[string]time.Duration
	pollTimeout             time.Duration
	reply                   string
	polling                 bool
	pollkey                 string
	numServers              int
	serverDiscoveryWait     time.Duration
	servers                 map[string]bool
	doneCh                  chan struct{}
	descs                   statzDescs
	collectAccounts         bool
	collectAccountsDetailed bool
	collectGatewayz         bool
	collectJsz              CollectJsz
	jszLimit                int
	jszLeadersOnly          bool
	jszFilterSet            map[JszFilter]bool
	sysReqPrefix            string
	accStatZeroConn         map[string]int
	natsUp                  *Gauge

	serverLabels       []string
	serverInfoLabels   []string
	routeLabels        []string
	gatewayLabels      []string
	gatewayzLabels     []string
	jsServerLabels     []string
	jsServerInfoLabels []string
	constLabels        prometheus.Labels

	surveyedCnt *GaugeVec
	expectedCnt *GaugeVec
	pollErrCnt  *CounterVec
	pollTime    *SummaryVec
	lateReplies *CounterVec
	noReplies   *CounterVec
}

type accountStats struct {
	accountID   string
	accountName string

	stats []*accStat

	jetstreamEnabled               float64
	jetstreamMemoryUsed            float64
	jetstreamStorageUsed           float64
	jetstreamMemoryReserved        float64
	jetstreamStorageReserved       float64
	jetstreamTieredMemoryUsed      map[int]float64
	jetstreamTieredStorageUsed     map[int]float64
	jetstreamTieredMemoryReserved  map[int]float64
	jetstreamTieredStorageReserved map[int]float64
	jetstreamStreamCount           float64
	jetstreamStreams               []streamAccountStats
	jszData                        []streamAccountStats
}

type consumerStats struct {
	consumerName                 string
	consumerLeader               string
	consumerDeliveredConsumerSeq float64
	consumerDeliveredStreamSeq   float64
	consumerNumAckPending        float64
	consumerNumRedelivered       float64
	consumerNumWaiting           float64
	consumerNumPending           float64
	consumerAckFloorStreamSeq    float64
	consumerAckFloorConsumerSeq  float64
	consumerRaftGroup            string
}

type streamAccountStats struct {
	accountID           string
	accountName         string
	serverName          string
	clusterName         string
	serverID            string
	streamName          string
	raftGroup           string
	consumerCount       float64
	replicaCount        float64
	streamMessages      float64
	streamBytes         float64
	streamFirstSeq      float64
	streamLastSeq       float64
	streamConsumerCount float64
	streamSubjectCount  float64
	streamLeader        string
	consumerStats       []*consumerStats
}

func serverName(sm *server.ServerInfo) string {
	if sm.Name == "" {
		return sm.ID
	}

	return sm.Name
}

func jsDomainLabelValue(sm *server.ServerStatsMsg) string {
	if sm.Server.Domain == "" {
		// Labels with empty values are ignored by Prometheus, but a JS Domain of "" is a valid configuration.
		// Use a name with '*' as a placeholder. This is an invalid JS Domain.
		return "Default"
	}
	return sm.Server.Domain
}

func jetstreamInfoLabelValues(sm *server.ServerStatsMsg) []string {
	// Maybe also "meta_leader", "store_dir"?
	return []string{
		sm.Server.Name, sm.Server.Host, sm.Server.ID, sm.Server.Cluster, jsDomainLabelValue(sm), sm.Server.Version,
		strconv.FormatBool(sm.Server.JetStream),
	}
}

func (sc *StatzCollector) serverLabelValues(sm *server.ServerInfo) []string {
	return []string{sm.Cluster, serverName(sm), sm.ID}
}

func (sc *StatzCollector) serverInfoLabelValues(sm *server.ServerInfo) []string {
	return []string{sm.Cluster, serverName(sm), sm.ID, sm.Version}
}

func (sc *StatzCollector) routeLabelValues(sm *server.ServerInfo, rStat *server.RouteStat) []string {
	return []string{sm.Cluster, serverName(sm), sm.ID, rStat.Name, strconv.FormatUint(rStat.ID, 10)}
}

func (sc *StatzCollector) gatewayLabelValues(sm *server.ServerInfo, gStat *server.GatewayStat) []string {
	return []string{sm.Cluster, serverName(sm), sm.ID, gStat.Name, strconv.FormatUint(gStat.ID, 10)}
}

// Up/Down on servers - look at discovery mechanisms in Prometheus - aging out, how does it work?
func (sc *StatzCollector) buildDescs() {
	newName := func(name string) string {
		return prometheus.BuildFQName("nats", "core", name)
	}

	// A unlabelled description for the up/down
	sc.natsUp = newGauge("nats_up", "1 if connected to NATS, 0 otherwise.  A gauge.", sc.constLabels)

	sc.descs.Info = newGaugeVec(newName("info"), "General Server information Summary gauge", sc.constLabels, sc.serverInfoLabels)
	sc.descs.Start = newGaugeVec(newName("start_time"), "Server start time gauge", sc.constLabels, sc.serverLabels)
	sc.descs.Uptime = newGaugeVec(newName("uptime"), "Server uptime gauge", sc.constLabels, sc.serverLabels)
	sc.descs.Mem = newGaugeVec(newName("mem_bytes"), "Server memory gauge", sc.constLabels, sc.serverLabels)
	sc.descs.GoMemLimit = newGaugeVec(newName("go_memlimit_bytes"), "Server GOMEMLIMIT gauge (0 if not set)", sc.constLabels, sc.serverLabels)
	sc.descs.Cores = newGaugeVec(newName("core_count"), "Machine cores gauge", sc.constLabels, sc.serverLabels)
	sc.descs.CPU = newGaugeVec(newName("cpu_percentage"), "Server cpu utilization gauge", sc.constLabels, sc.serverLabels)
	sc.descs.Connections = newGaugeVec(newName("connection_count"), "Current number of client connections gauge", sc.constLabels, sc.serverLabels)
	sc.descs.TotalConnections = newCounterVec(newName("total_connection_count"), "Total number of client connections serviced counter", sc.constLabels, sc.serverLabels)
	sc.descs.ActiveAccounts = newGaugeVec(newName("active_account_count"), "Number of active accounts gauge", sc.constLabels, sc.serverLabels)
	sc.descs.NumSubs = newGaugeVec(newName("subs_count"), "Current number of subscriptions gauge", sc.constLabels, sc.serverLabels)
	sc.descs.SentMsgs = newCounterVec(newName("sent_msgs_count"), "Number of messages sent counter", sc.constLabels, sc.serverLabels)
	sc.descs.SentBytes = newCounterVec(newName("sent_bytes"), "Number of bytes sent counter", sc.constLabels, sc.serverLabels)
	sc.descs.RecvMsgs = newCounterVec(newName("recv_msgs_count"), "Number of messages received counter", sc.constLabels, sc.serverLabels)
	sc.descs.RecvBytes = newCounterVec(newName("recv_bytes"), "Number of bytes received counter", sc.constLabels, sc.serverLabels)
	sc.descs.SlowConsumers = newGaugeVec(newName("slow_consumer_count"), "Number of slow consumers gauge", sc.constLabels, sc.serverLabels)
	sc.descs.RTT = newGaugeVec(newName("rtt_nanoseconds"), "RTT in nanoseconds gauge", sc.constLabels, sc.serverLabels)
	sc.descs.Routes = newGaugeVec(newName("route_count"), "Number of active routes gauge", sc.constLabels, sc.serverLabels)
	sc.descs.Gateways = newGaugeVec(newName("gateway_count"), "Number of active gateways gauge", sc.constLabels, sc.serverLabels)

	// Routes
	sc.descs.RouteSentMsgs = newCounterVec(newName("route_sent_msg_count"), "Number of messages sent over the route counter", sc.constLabels, sc.routeLabels)
	sc.descs.RouteSentBytes = newCounterVec(newName("route_sent_bytes"), "Number of bytes sent over the route counter", sc.constLabels, sc.routeLabels)
	sc.descs.RouteRecvMsgs = newCounterVec(newName("route_recv_msg_count"), "Number of messages received over the route counter", sc.constLabels, sc.routeLabels)
	sc.descs.RouteRecvBytes = newCounterVec(newName("route_recv_bytes"), "Number of bytes received over the route counter", sc.constLabels, sc.routeLabels)
	sc.descs.RoutePending = newGaugeVec(newName("route_pending_bytes"), "Number of bytes pending in the route gauge", sc.constLabels, sc.routeLabels)

	// Gateways
	sc.descs.GatewaySentMsgs = newCounterVec(newName("gateway_sent_msgs_count"), "Number of messages sent over the gateway counter", sc.constLabels, sc.gatewayLabels)
	sc.descs.GatewaySentBytes = newCounterVec(newName("gateway_sent_bytes"), "Number of messages sent over the gateway counter", sc.constLabels, sc.gatewayLabels)
	sc.descs.GatewayRecvMsgs = newCounterVec(newName("gateway_recv_msg_count"), "Number of messages sent over the gateway counter", sc.constLabels, sc.gatewayLabels)
	sc.descs.GatewayRecvBytes = newCounterVec(newName("gateway_recv_bytes"), "Number of messages sent over the gateway counter", sc.constLabels, sc.gatewayLabels)
	sc.descs.GatewayNumInbound = newGaugeVec(newName("gateway_inbound_msg_count"), "Number inbound messages through the gateway gauge", sc.constLabels, sc.gatewayLabels)

	// Gatewayz
	if sc.collectGatewayz {
		sc.descs.OutboundGateways = sc.newGatewayzDescs("gatewayz_outbound_gateway", newName)
		sc.descs.InboundGateways = sc.newGatewayzDescs("gatewayz_inbound_gateway", newName)
	}

	// Jetstream Info
	sc.descs.JetstreamInfo = newGaugeVec(newName("jetstream_info"), " Always 1. Contains metadata for cross-reference from other time-series", sc.constLabels, sc.jsServerInfoLabels)

	// Jetstream Server
	sc.descs.JetstreamEnabled = newGaugeVec(newName("jetstream_enabled"), "1 if Jetstream is enabled, 0 otherwise.  A gauge.", sc.constLabels, sc.jsServerLabels)
	sc.descs.JetstreamFilestoreSizeBytes = newGaugeVec(newName("jetstream_filestore_size_bytes"), "Capacity of jetstream filesystem storage in bytes", sc.constLabels, sc.jsServerLabels)
	sc.descs.JetstreamMemstoreSizeBytes = newGaugeVec(newName("jetstream_memstore_size_bytes"), "Capacity of jetstream in-memory store in bytes", sc.constLabels, sc.jsServerLabels)
	sc.descs.JetstreamFilestoreUsedBytes = newGaugeVec(newName("jetstream_filestore_used_bytes"), "Consumption of jetstream filesystem storage in bytes", sc.constLabels, sc.jsServerLabels)
	sc.descs.JetstreamFilestoreReservedBytes = newGaugeVec(newName("jetstream_filestore_reserved_bytes"), "Account Reservations of jetstream filesystem storage in bytes", sc.constLabels, sc.jsServerLabels)
	sc.descs.JetstreamFilestoreReservedUsedBytes = newGaugeVec(newName("jetstream_filestore_reserved_used_bytes"), "Consumption of Account Reservation of jetstream filesystem storage in bytes", sc.constLabels, sc.jsServerLabels)
	sc.descs.JetstreamMemstoreUsedBytes = newGaugeVec(newName("jetstream_memstore_used_bytes"), "Consumption of jetstream in-memory store in bytes", sc.constLabels, sc.jsServerLabels)
	sc.descs.JetstreamMemstoreReservedBytes = newGaugeVec(newName("jetstream_memstore_reserved_bytes"), "Account Reservations of  jetstream in-memory store in bytes", sc.constLabels, sc.jsServerLabels)
	sc.descs.JetstreamMemstoreReservedUsedBytes = newGaugeVec(newName("jetstream_memstore_reserved_used_bytes"), "Consumption of Account Reservation of jetstream in-memory store in bytes. ", sc.constLabels, sc.jsServerLabels)
	sc.descs.JetstreamAccounts = newGaugeVec(newName("jetstream_accounts"), "Number of NATS Accounts present on a Jetstream server", sc.constLabels, sc.jsServerLabels)
	sc.descs.JetstreamHAAssets = newGaugeVec(newName("jetstream_ha_assets"), "Number of HA (R>1) assets used by NATS", sc.constLabels, sc.jsServerLabels)
	sc.descs.JetstreamAPIRequests = newCounterVec(newName("jetstream_api_requests"), "Number of Jetstream API Requests processed. Value is 0 when server starts", sc.constLabels, sc.jsServerLabels)
	sc.descs.JetstreamAPIPending = newGaugeVec(newName("jetstream_api_pending"), "Number of Jetstream API in the queue waiting to be processed", sc.constLabels, sc.jsServerLabels)
	sc.descs.JetstreamAPIErrors = newCounterVec(newName("jetstream_api_errors"), "Number of Jetstream API Errors. Value is 0 when server starts", sc.constLabels, sc.jsServerLabels)

	// JetStream Raft Groups
	jsRaftGroupInfoLabelKeys := []string{"jetstream_domain", "raft_group", "server_id", "server_name", "cluster_name", "leader"}
	sc.descs.JetstreamClusterRaftGroupInfo = newGaugeVec(newName("jetstream_cluster_raft_group_info"), "Provides metadata about a RAFT Group", sc.constLabels, jsRaftGroupInfoLabelKeys)
	jsRaftGroupLabelKeys := []string{"server_id", "server_name", "cluster_name"}
	sc.descs.JetstreamClusterRaftGroupSize = newGaugeVec(newName("jetstream_cluster_raft_group_size"), "Number of peers in a RAFT group", sc.constLabels, jsRaftGroupLabelKeys)
	sc.descs.JetstreamClusterRaftGroupLeader = newGaugeVec(newName("jetstream_cluster_raft_group_leader"), "1 if this server is leader of raft group, 0 otherwise", sc.constLabels, jsRaftGroupLabelKeys)
	sc.descs.JetstreamClusterRaftGroupReplicas = newGaugeVec(newName("jetstream_cluster_raft_group_replicas"), "Info about replicas from leaders perspective", sc.constLabels, jsRaftGroupLabelKeys)

	// Jetstream Cluster Replicas
	jsClusterReplicaLabelKeys := []string{"server_id", "server_name", "peer", "cluster_name"}
	// FIXME: help could use some work...
	sc.descs.JetstreamClusterRaftGroupReplicaActive = newGaugeVec(newName("jetstream_cluster_raft_group_replica_peer_active"), "Jetstream RAFT Group Peer last Active time. Very large values may imply raft is stalled", sc.constLabels, jsClusterReplicaLabelKeys)
	sc.descs.JetstreamClusterRaftGroupReplicaCurrent = newGaugeVec(newName("jetstream_cluster_raft_group_replica_peer_current"), "Jetstream RAFT Group Peer is current: 1 or not: 0", sc.constLabels, jsClusterReplicaLabelKeys)
	sc.descs.JetstreamClusterRaftGroupReplicaOffline = newGaugeVec(newName("jetstream_cluster_raft_group_replica_peer_offline"), "Jetstream RAFT Group Peer is offline: 1 or online: 0", sc.constLabels, jsClusterReplicaLabelKeys)

	jsServerLabelKeys := []string{"server_id", "server_name", "cluster_name"}
	sc.descs.JetstreamServerDisabled = newGaugeVec(newName("jetstream_server_jetstream_disabled"), "JetStream disabled or not", sc.constLabels, jsServerLabelKeys)
	sc.descs.JetstreamServerStreams = newGaugeVec(newName("jetstream_server_total_streams"), "Total number of streams in JetStream", sc.constLabels, jsServerLabelKeys)
	sc.descs.JetstreamServerConsumers = newGaugeVec(newName("jetstream_server_total_consumers"), "Total number of consumers in JetStream", sc.constLabels, jsServerLabelKeys)
	sc.descs.JetstreamServerMessages = newGaugeVec(newName("jetstream_server_total_messages"), "Total number of stored messages in JetStream", sc.constLabels, jsServerLabelKeys)
	sc.descs.JetstreamServerBytes = newGaugeVec(newName("jetstream_server_total_message_bytes"), "Total number of bytes stored in JetStream", sc.constLabels, jsServerLabelKeys)
	sc.descs.JetstreamServerMaxMemory = newGaugeVec(newName("jetstream_server_max_memory"), "JetStream Max Memory", sc.constLabels, jsServerLabelKeys)
	sc.descs.JetstreamServerMaxStorage = newGaugeVec(newName("jetstream_server_max_storage"), "JetStream Max Storage", sc.constLabels, jsServerLabelKeys)

	_, collectJsz := shouldCollectJsz(sc.collectJsz)

	// Account scope metrics
	if sc.collectAccounts || collectJsz {
		accLabel := []string{"account", "account_name"}
		serverAndAccLabel := append(sc.serverLabels, accLabel...)
		sc.descs.accCount = newGaugeVec(newName("account_count"), "The number of accounts detected", sc.constLabels, nil)

		// Metrics reported per-server
		sc.descs.accConnCount = newGaugeVec(newName("account_conn_count"), "The number of client connections to this account", sc.constLabels, serverAndAccLabel)
		sc.descs.accTotalConnCount = newGaugeVec(newName("account_total_conn_count"), "The combined current number of client and leafnode connections to this account", sc.constLabels, serverAndAccLabel)
		sc.descs.accLeafCount = newGaugeVec(newName("account_leaf_count"), "The number of leafnode connections to this account", sc.constLabels, serverAndAccLabel)
		sc.descs.accSubCount = newGaugeVec(newName("account_sub_count"), "The number of subscriptions on this account", sc.constLabels, serverAndAccLabel)
		sc.descs.accSlowConsumerCount = newGaugeVec(newName("account_slow_consumer_count"), "The number of slow consumers detected in this account", sc.constLabels, serverAndAccLabel)

		sc.descs.accBytesSent = newCounterVec(newName("account_bytes_sent"), "The number of bytes sent on this account across all connections", sc.constLabels, serverAndAccLabel)
		sc.descs.accBytesRecv = newCounterVec(newName("account_bytes_recv"), "The number of bytes received on this account across all connections", sc.constLabels, serverAndAccLabel)
		sc.descs.accMsgsSent = newCounterVec(newName("account_msgs_sent"), "The number of messages sent on this account across all connections", sc.constLabels, serverAndAccLabel)
		sc.descs.accMsgsRecv = newCounterVec(newName("account_msgs_recv"), "The number of messages received on this account across all connections", sc.constLabels, serverAndAccLabel)

		if sc.collectAccountsDetailed {
			sc.descs.accClientBytesSent = newCounterVec(newName("account_client_bytes_sent"), "The number of bytes sent on this account via a client connection", sc.constLabels, serverAndAccLabel)
			sc.descs.accClientBytesRecv = newCounterVec(newName("account_client_bytes_recv"), "The number of bytes received on this account via a client connection", sc.constLabels, serverAndAccLabel)
			sc.descs.accClientMsgsSent = newCounterVec(newName("account_client_msgs_sent"), "The number of messages sent on this account via a client connection", sc.constLabels, serverAndAccLabel)
			sc.descs.accClientMsgsRecv = newCounterVec(newName("account_client_msgs_recv"), "The number of messages received on this account via a client connection", sc.constLabels, serverAndAccLabel)
			sc.descs.accLeafBytesSent = newCounterVec(newName("account_leaf_bytes_sent"), "The number of bytes sent on this account via a leafnode connection", sc.constLabels, serverAndAccLabel)
			sc.descs.accLeafBytesRecv = newCounterVec(newName("account_leaf_bytes_recv"), "The number of bytes received on this account via a leafnode connection", sc.constLabels, serverAndAccLabel)
			sc.descs.accLeafMsgsSent = newCounterVec(newName("account_leaf_msgs_sent"), "The number of messages sent on this account via a leafnode connection", sc.constLabels, serverAndAccLabel)
			sc.descs.accLeafMsgsRecv = newCounterVec(newName("account_leaf_msgs_recv"), "The number of messages received on this account via a leafnode connection", sc.constLabels, serverAndAccLabel)
			sc.descs.accRouteBytesSent = newCounterVec(newName("account_route_bytes_sent"), "The number of bytes sent on this account via a route connection", sc.constLabels, serverAndAccLabel)
			sc.descs.accRouteBytesRecv = newCounterVec(newName("account_route_bytes_recv"), "The number of bytes received on this account via a route connection", sc.constLabels, serverAndAccLabel)
			sc.descs.accRouteMsgsSent = newCounterVec(newName("account_route_msgs_sent"), "The number of messages sent on this account via a route connection", sc.constLabels, serverAndAccLabel)
			sc.descs.accRouteMsgsRecv = newCounterVec(newName("account_route_msgs_recv"), "The number of messages received on this account via a route connection", sc.constLabels, serverAndAccLabel)
			sc.descs.accGatewayBytesSent = newCounterVec(newName("account_gateway_bytes_sent"), "The number of bytes sent on this account via a gateway connection", sc.constLabels, serverAndAccLabel)
			sc.descs.accGatewayBytesRecv = newCounterVec(newName("account_gateway_bytes_recv"), "The number of bytes received on this account via a gateway connection", sc.constLabels, serverAndAccLabel)
			sc.descs.accGatewayMsgsSent = newCounterVec(newName("account_gateway_msgs_sent"), "The number of messages sent on this account via a gateway connection", sc.constLabels, serverAndAccLabel)
			sc.descs.accGatewayMsgsRecv = newCounterVec(newName("account_gateway_msgs_recv"), "The number of messages received on this account via a gateway connection", sc.constLabels, serverAndAccLabel)
		}

		// Aggregated metrics
		sc.descs.accJetstreamEnabled = newGaugeVec(newName("account_jetstream_enabled"), "Whether JetStream is enabled or not for this account", sc.constLabels, accLabel)
		sc.descs.accJetstreamMemoryUsed = newGaugeVec(newName("account_jetstream_memory_used"), "The number of bytes used by JetStream memory", sc.constLabels, accLabel)
		sc.descs.accJetstreamStorageUsed = newGaugeVec(newName("account_jetstream_storage_used"), "The number of bytes used by JetStream storage", sc.constLabels, accLabel)
		sc.descs.accJetstreamMemoryReserved = newGaugeVec(newName("account_jetstream_memory_reserved"), "The number of bytes reserved by JetStream memory", sc.constLabels, accLabel)
		sc.descs.accJetstreamStorageReserved = newGaugeVec(newName("account_jetstream_storage_reserved"), "The number of bytes reserved by JetStream storage", sc.constLabels, accLabel)
		sc.descs.accJetstreamTieredMemoryUsed = newGaugeVec(newName("account_jetstream_tiered_memory_used"), "The number of bytes used by JetStream memory tier", sc.constLabels, append(accLabel, "tier"))
		sc.descs.accJetstreamTieredStorageUsed = newGaugeVec(newName("account_jetstream_tiered_storage_used"), "The number of bytes used by JetStream storage tier", sc.constLabels, append(accLabel, "tier"))
		sc.descs.accJetstreamTieredMemoryReserved = newGaugeVec(newName("account_jetstream_tiered_memory_reserved"), "The number of bytes reserved by JetStream memory tier", sc.constLabels, append(accLabel, "tier"))
		sc.descs.accJetstreamTieredStorageReserved = newGaugeVec(newName("account_jetstream_tiered_storage_reserved"), "The number of bytes reserved by JetStream storage tier", sc.constLabels, append(accLabel, "tier"))
		sc.descs.accJetstreamStreamCount = newGaugeVec(newName("account_jetstream_stream_count"), "The number of streams in this account", sc.constLabels, accLabel)
		sc.descs.accJetstreamConsumerCount = newGaugeVec(newName("account_jetstream_consumer_count"), "The number of consumers per stream for this account", sc.constLabels, append(accLabel, "stream", "raft_group"))
		sc.descs.accJetstreamReplicaCount = newGaugeVec(newName("account_jetstream_replica_count"), "The number of replicas per stream for this account", sc.constLabels, append(accLabel, "stream", "raft_group"))

		jszLabels := append(accLabel, []string{"cluster_name", "raft_group", "server_id", "server_name", "stream", "stream_leader"}...)
		var consumerLabels []string
		consumerLabels = append(consumerLabels, jszLabels...)
		consumerLabels = append(consumerLabels, "consumer_name")
		consumerLabels = append(consumerLabels, "consumer_leader")

		sc.descs.accJszStreamMsgs = newGaugeVec(
			prometheus.BuildFQName("nats", "stream", "total_messages"),
			"Total number of messages from a stream",
			sc.constLabels,
			jszLabels,
		)
		sc.descs.accJszStreamBytes = newGaugeVec(
			prometheus.BuildFQName("nats", "stream", "total_bytes"),
			"Total stored bytes from a stream",
			sc.constLabels,
			jszLabels,
		)
		sc.descs.accJszStreamFirstSeq = newGaugeVec(
			prometheus.BuildFQName("nats", "stream", "first_seq"),
			"First sequence from a stream",
			sc.constLabels,
			jszLabels,
		)
		sc.descs.accJszStreamLastSeq = newGaugeVec(
			prometheus.BuildFQName("nats", "stream", "last_seq"),
			"Last sequence from a stream",
			sc.constLabels,
			jszLabels,
		)
		sc.descs.accJszStreamConsumerCount = newGaugeVec(
			prometheus.BuildFQName("nats", "stream", "consumer_count"),
			"Total number of consumers from a stream",
			sc.constLabels,
			jszLabels,
		)
		sc.descs.accJszStreamSubjectCount = newGaugeVec(
			prometheus.BuildFQName("nats", "stream", "subject_count"),
			"Total number of subjects in a stream",
			sc.constLabels,
			jszLabels,
		)
		sc.descs.accJszConsumerDeliveredConsumerSeq = newGaugeVec(
			prometheus.BuildFQName("nats", "consumer", "delivered_consumer_seq"),
			"Latest consumer sequence number of a stream consumer",
			sc.constLabels,
			consumerLabels,
		)
		sc.descs.accJszConsumerDeliveredStreamSeq = newGaugeVec(
			prometheus.BuildFQName("nats", "consumer", "delivered_stream_seq"),
			"Latest stream sequence number of a stream",
			sc.constLabels,
			consumerLabels,
		)
		sc.descs.accJszConsumerNumAckPending = newGaugeVec(
			prometheus.BuildFQName("nats", "consumer", "num_ack_pending"),
			"Number of pending acks from a consumer",
			sc.constLabels,
			consumerLabels,
		)
		sc.descs.accJszConsumerNumRedelivered = newGaugeVec(
			prometheus.BuildFQName("nats", "consumer", "num_redelivered"),
			"Number of redelivered messages from a consumer",
			sc.constLabels,
			consumerLabels,
		)
		sc.descs.accJszConsumerNumWaiting = newGaugeVec(
			prometheus.BuildFQName("nats", "consumer", "num_waiting"),
			"Number of inflight fetch requests from a pull consumer",
			sc.constLabels,
			consumerLabels,
		)
		sc.descs.accJszConsumerNumPending = newGaugeVec(
			prometheus.BuildFQName("nats", "consumer", "num_pending"),
			"Number of pending messages from a consumer",
			sc.constLabels,
			consumerLabels,
		)
		sc.descs.accJszConsumerAckFloorStreamSeq = newGaugeVec(
			prometheus.BuildFQName("nats", "consumer", "ack_floor_stream_seq"),
			"Number of ack floor stream seq from a consumer",
			sc.constLabels,
			consumerLabels,
		)
		sc.descs.accJszConsumerAckFloorConsumerSeq = newGaugeVec(
			prometheus.BuildFQName("nats", "consumer", "ack_floor_consumer_seq"),
			"Number of ack floor consumer seq from a consumer",
			sc.constLabels,
			consumerLabels,
		)
	}

	// Surveyor
	sc.surveyedCnt = newGaugeVec(
		prometheus.BuildFQName("nats", "survey", "surveyed_count"),
		"Number of remote hosts successfully surveyed gauge",
		sc.constLabels,
		[]string{},
	)

	sc.expectedCnt = newGaugeVec(
		prometheus.BuildFQName("nats", "survey", "expected_count"),
		"Number of remote hosts expected to responded gauge",
		sc.constLabels,
		[]string{},
	)

	sc.pollTime = newSummaryVec(
		prometheus.BuildFQName("nats", "survey", "duration_seconds"),
		"Time it took to gather the surveyed data histogram",
		sc.constLabels,
		[]string{},
	)

	sc.pollErrCnt = newCounterVec(
		prometheus.BuildFQName("nats", "survey", "poll_error_count"),
		"The number of times the poller encountered errors counter",
		sc.constLabels,
		[]string{},
	)

	sc.lateReplies = newCounterVec(
		prometheus.BuildFQName("nats", "survey", "late_replies_count"),
		"Number of times a reply was received too late counter",
		sc.constLabels,
		[]string{"timeout"},
	)

	sc.noReplies = newCounterVec(
		prometheus.BuildFQName("nats", "survey", "no_replies_count"),
		"Number of nodes that did not reply in poll cycle",
		sc.constLabels,
		[]string{"expected"},
	)
}

type StatzCollectorOpt func(sc *StatzCollector) error

// ServerAPIAccstatzResponse is the response type for accstatz, defined here since nats-server doesn't export it
type ServerAPIAccstatzResponse struct {
	server.ServerAPIResponse
	Data server.AccountStatz `json:"data,omitempty"`
}

func WithNATSConnection(nc *nats.Conn) StatzCollectorOpt {
	return func(sc *StatzCollector) error {
		sc.nc = nc
		if nc != nil {
			sc.reply = nc.NewRespInbox()
		}
		return nil
	}
}

func WithLogger(logger *logrus.Logger) StatzCollectorOpt {
	return func(sc *StatzCollector) error {
		sc.logger = logger
		return nil
	}
}

func WithNumServers(numServers int) StatzCollectorOpt {
	return func(sc *StatzCollector) error {
		sc.numServers = numServers
		return nil
	}
}

func WithServerDiscoveryWait(serverDiscoveryWait time.Duration) StatzCollectorOpt {
	return func(sc *StatzCollector) error {
		sc.serverDiscoveryWait = serverDiscoveryWait
		return nil
	}
}

func WithPollTimeout(pollTimeout time.Duration) StatzCollectorOpt {
	return func(sc *StatzCollector) error {
		sc.pollTimeout = pollTimeout
		return nil
	}
}

func WithCollectAccounts(collectAccounts bool, collectAccountsDetailed bool) StatzCollectorOpt {
	return func(sc *StatzCollector) error {
		sc.collectAccounts = collectAccounts
		sc.collectAccountsDetailed = collectAccountsDetailed
		return nil
	}
}

func WithCollectGatewayz(collectGatewayz bool) StatzCollectorOpt {
	return func(sc *StatzCollector) error {
		sc.collectGatewayz = collectGatewayz
		return nil
	}
}

func WithCollectJsz(jsz CollectJsz, jszLeadersOnly bool, jszFilters []JszFilter) StatzCollectorOpt {
	return func(sc *StatzCollector) error {
		sc.collectJsz = jsz
		sc.jszLeadersOnly = jszLeadersOnly
		for i := range jszFilters {
			sc.jszFilterSet[jszFilters[i]] = true
		}
		return nil
	}
}

func WithJszLimit(jszLimit int) StatzCollectorOpt {
	return func(sc *StatzCollector) error {
		sc.jszLimit = jszLimit
		return nil
	}
}

func WithConstantLabels(constLabels prometheus.Labels) StatzCollectorOpt {
	return func(sc *StatzCollector) error {
		sc.constLabels = constLabels
		return nil
	}
}

func WithSysRequestPrefix(prefix string) StatzCollectorOpt {
	return func(sc *StatzCollector) error {
		// Remove the potential wildcard users might place
		// since we are simply prepending the string
		prefix = strings.TrimSuffix(prefix, ".>")
		if prefix == "" {
			prefix = DefaultSysReqPrefix
		}
		sc.sysReqPrefix = prefix
		return nil
	}
}

type WithStatsBatch struct {
	Stats         []*server.ServerStatsMsg
	AccStatzs     []*ServerAPIAccstatzResponse
	JsStatzs      []*server.ServerAPIJszResponse
	GatewayStatzs []*server.ServerAPIGatewayzResponse
}

// WithStats lets you provide your own NATS Server responses for generating metrics.
// If a NATS connection is provided, the collector will still poll for the stats and override the provided stats.
func WithStats(batch WithStatsBatch) StatzCollectorOpt {
	return func(sc *StatzCollector) error {
		// statz
		sc.stats = batch.Stats

		// accstatz
		accs := make(map[string][]*accStat)
		for _, statz := range batch.AccStatzs {
			for _, acc := range statz.Data.Accounts {
				accInfo := accs[acc.Account]
				accs[acc.Account] = append(accInfo, &accStat{
					Server: statz.Server,
					Data:   acc,
				})
			}
		}

		accStats := make(map[string]*accountStats)
		for accID, stats := range accs {
			sts := mapAccountStat(accID, stats)
			accStats[accID] = sts
		}

		// js statz
		sc.jsStats = make([]*jsStat, 0)
		for _, statz := range batch.JsStatzs {
			if statz.Data == nil {
				continue
			}
			s := &jsStat{
				Server: statz.Server,
				Data:   statz.Data,
			}
			sc.jsStats = append(sc.jsStats, s)
		}

		// Combine jsz account, stream, and consumer details from all stats
		jsAccInfos := make(map[string]*server.AccountDetail)
		for _, jsStat := range sc.jsStats {
			for _, acc := range jsStat.Data.AccountDetails {
				accInfo, ok := jsAccInfos[acc.Id]
				if !ok {
					jsAccInfos[acc.Id] = acc
					continue
				}
				mergeStreamDetails(accInfo, acc)
				jsAccInfos[acc.Id] = acc
			}
		}

		// Add jsz account details to accstatz
		for accID, accDetail := range jsAccInfos {
			sts := accStats[accID]
			sts = mapJSAccountDetailToStats(accID, accDetail, sts)
			accStats[accID] = sts
		}

		// Add jsz stream details to accstatz
		for _, jsStat := range sc.jsStats {
			for _, accDetail := range jsStat.Data.AccountDetails {
				sts, ok := accStats[accDetail.Id]
				if !ok {
					continue
				}
				sts = mapJSStreamDetailToStats(jsStat.Server, accDetail, sts)
				accStats[accDetail.Id] = sts
			}
		}

		sc.accStats = make([]*accountStats, 0)
		for _, acc := range accStats {
			sc.accStats = append(sc.accStats, acc)
		}

		// gateway statz
		sc.gatewayStatz = make([]*gatewayStatz, 0)
		for _, statz := range batch.GatewayStatzs {
			g := &gatewayStatz{
				ServerAPIResponse: server.ServerAPIResponse{Server: statz.Server},
				Data:              statz.Data,
			}
			sc.gatewayStatz = append(sc.gatewayStatz, g)
		}

		return nil
	}
}

// NewStatzCollector creates a NATS Statz Collector.
// Deprecated: NewStatzCollector is deprecated. Use NewStatzCollectorOpts instead.
func NewStatzCollector(nc *nats.Conn, logger *logrus.Logger, numServers int,
	serverDiscoveryWait, pollTimeout time.Duration, accounts, accountsDetailed bool, gatewayz bool,
	jsz string, jszLimit int, jszLeadersOnly bool, jszFilters []JszFilter, sysReqPrefix string,
	constLabels prometheus.Labels,
) *StatzCollector {
	sc, _ := NewStatzCollectorOpts(
		WithNATSConnection(nc),
		WithLogger(logger),
		WithNumServers(numServers),
		WithServerDiscoveryWait(serverDiscoveryWait),
		WithPollTimeout(pollTimeout),
		WithCollectAccounts(accounts, accountsDetailed),
		WithCollectGatewayz(gatewayz),
		WithCollectJsz(CollectJsz(jsz), jszLeadersOnly, jszFilters),
		WithJszLimit(jszLimit),
		WithConstantLabels(constLabels),
		WithSysRequestPrefix(sysReqPrefix),
	)
	return sc
}

// NewStatzCollectorOpts creates a NATS Statz Collector with the given options.
// It bubbles up the first error from the options.
func NewStatzCollectorOpts(opts ...StatzCollectorOpt) (*StatzCollector, error) {
	sc := &StatzCollector{
		// initialize
		servers:         make(map[string]bool),
		doneCh:          make(chan struct{}, 1),
		accStatZeroConn: make(map[string]int),
		jszFilterSet:    make(map[JszFilter]bool),

		// defaults
		numServers:          DefaultExpectedServers,
		serverDiscoveryWait: DefaultServerResponseWait,
		pollTimeout:         DefaultPollTimeout,
		jszLimit:            DefaultJszLimit,

		// TODO - normalize these if possible.  Jetstream varies from the other server labels
		serverLabels:       []string{"server_cluster", "server_name", "server_id"},
		serverInfoLabels:   []string{"server_cluster", "server_name", "server_id", "server_version"},
		routeLabels:        []string{"server_cluster", "server_name", "server_id", "server_route_name", "server_route_name_id"},
		gatewayLabels:      []string{"server_cluster", "server_name", "server_id", "server_gateway_name", "server_gateway_name_id"},
		gatewayzLabels:     []string{"server_id", "server_name", "gateway_name", "remote_gateway_name", "gateway_id", "cid"},
		jsServerLabels:     []string{"server_id", "server_name", "cluster_name"},
		jsServerInfoLabels: []string{"server_name", "server_host", "server_id", "server_cluster", "server_domain", "server_version", "server_jetstream"},
	}

	for _, opt := range opts {
		if err := opt(sc); err != nil {
			return nil, err
		}
	}

	if sc.nc != nil && len(sc.stats) > 0 {
		return nil, fmt.Errorf("WithStats and WithNATSConnection are mutually exclusive")
	}

	sc.buildDescs()
	sc.expectedCnt.WithLabelValues().Set(float64(sc.numServers))

	if sc.nc != nil {
		_, err := sc.nc.Subscribe(sc.reply+".*", sc.handleStatzResponse)
		if err != nil {
			return nil, fmt.Errorf("error subscribing to statz response: %w", err)
		}
	}

	if sc.logger == nil {
		sc.logger = logrus.New()
	}

	return sc, nil
}

func (sc *StatzCollector) newGatewayzDescs(gwType string, newName func(name string) string) *gatewayzDescs {
	return &gatewayzDescs{
		configured:        newGaugeVec(newName(gwType+"_configured"), "configured", sc.constLabels, sc.gatewayzLabels),
		connStart:         newGaugeVec(newName(gwType+"_conn_start_time_seconds"), "conn_start_time_seconds", sc.constLabels, sc.gatewayzLabels),
		connLastActivity:  newGaugeVec(newName(gwType+"_conn_last_activity_seconds"), "conn_last_activity_seconds", sc.constLabels, sc.gatewayzLabels),
		connUptime:        newGaugeVec(newName(gwType+"_conn_uptime_seconds"), "conn_uptime_seconds", sc.constLabels, sc.gatewayzLabels),
		connIdle:          newGaugeVec(newName(gwType+"_conn_idle_seconds"), "conn_idle_seconds", sc.constLabels, sc.gatewayzLabels),
		connRtt:           newGaugeVec(newName(gwType+"_conn_rtt"), "rtt", sc.constLabels, sc.gatewayzLabels),
		connPendingBytes:  newGaugeVec(newName(gwType+"_conn_pending_bytes"), "pending_bytes", sc.constLabels, sc.gatewayzLabels),
		connInMsgs:        newGaugeVec(newName(gwType+"_conn_in_msgs"), "in_msgs", sc.constLabels, sc.gatewayzLabels),
		connOutMsgs:       newGaugeVec(newName(gwType+"_conn_out_msgs"), "out_msgs", sc.constLabels, sc.gatewayzLabels),
		connInBytes:       newGaugeVec(newName(gwType+"_conn_in_bytes"), "in_bytes", sc.constLabels, sc.gatewayzLabels),
		connOutBytes:      newGaugeVec(newName(gwType+"_conn_out_bytes"), "out_bytes", sc.constLabels, sc.gatewayzLabels),
		connSubscriptions: newGaugeVec(newName(gwType+"_conn_subscriptions"), "subscriptions", sc.constLabels, sc.gatewayzLabels),
	}
}

func (sc *StatzCollector) handleStatzResponse(msg *nats.Msg) {
	m := &server.ServerStatsMsg{}

	if err := unmarshalMsg(msg, m); err != nil {
		sc.logger.Warnf("Error unmarshalling statz json: %v", err)
		return
	}

	sc.Lock()
	defer sc.Unlock()
	isCurrent := strings.HasSuffix(msg.Subject, sc.pollkey)
	rtt := time.Since(sc.start)
	if sc.polling && isCurrent { //nolint
		// Avoid edge-case deadlock if message comes in after poll() receive loop completes
		// but before lock is re-acquired
		select {
		case <-time.After(500 * time.Millisecond):
		case sc.statsChan <- m:
		}
		sc.rtts[m.Server.ID] = rtt
	} else if !isCurrent {
		sc.logger.Infof("Late reply for server [%15s : %15s : %s]: %v", m.Server.Cluster, serverName(&m.Server), m.Server.ID, rtt)
		sc.lateReplies.WithLabelValues(fmt.Sprintf("%.1f", sc.pollTimeout.Seconds())).Inc()
	} else {
		sc.logger.Infof("Extra reply from server [%15s : %15s : %s]: %v", m.Server.Cluster, serverName(&m.Server), m.Server.ID, rtt)
	}
}

// poll will only fail if there is a NATS publishing error
func (sc *StatzCollector) poll() error {
	sc.Lock()
	sc.start = time.Now()
	sc.uptime = time.Duration(time.Since(sc.start).Seconds())
	sc.polling = true
	sc.pollkey = strconv.Itoa(int(sc.start.UnixNano()))
	sc.stats = nil
	sc.rtts = make(map[string]time.Duration, sc.numServers)
	sc.statsChan = make(chan *server.ServerStatsMsg)
	expectedServers := sc.numServers
	sc.Unlock()

	// not all error paths clean this up, so this way might be easier
	defer func() {
		sc.Lock()
		sc.polling = false
		sc.Unlock()
	}()

	// fail fast if we aren't connected to return a nats down (nats_up=0) to
	// Prometheus
	if !sc.nc.IsConnected() {
		return fmt.Errorf("no connection to NATS")
	}

	msg := nats.Msg{
		Subject: sc.sysReqPrefix + ".SERVER.PING",
		Reply:   sc.reply + "." + sc.pollkey,
		Header: nats.Header{
			"Accept-Encoding": []string{"snappy"},
		},
	}

	// Send our ping for statusz updates
	if err := sc.nc.PublishMsg(&msg); err != nil {
		return err
	}

	// Wait to collect all the servers responses.
	// For set number of expected servers (> 0), terminate as soon as either expected
	// number of responses is reached or we reach polling timeout.
	// For unlimited number of expected servers (-1), terminate once the interval between
	// server responses reaches server-discovery-timeout value (defaults to 500ms) or we reach
	// polling timeout.
	timer := time.NewTimer(sc.serverDiscoveryWait)
	var done bool
	for !done {
		select {
		case stat := <-sc.statsChan:
			timer.Reset(sc.serverDiscoveryWait)
			sc.stats = append(sc.stats, stat)
			if expectedServers != -1 && len(sc.stats) == expectedServers {
				done = true
			}
		case <-sc.doneCh:
			done = true
		case <-timer.C:
			if expectedServers == -1 {
				done = true
			}
		case <-time.After(sc.pollTimeout):
			done = true
			if expectedServers != -1 {
				sc.logger.Warnf("Poll timeout after %v while waiting for responses", sc.pollTimeout)
			}
		}
	}

	sc.Lock()
	sc.polling = false
	ns := len(sc.stats)
	stats := append([]*server.ServerStatsMsg(nil), sc.stats...)
	rtts := sc.rtts
	sc.Unlock()

	// If we do not see expected number of servers complain.
	if sc.numServers != -1 && ns != sc.numServers {
		sort.Slice(stats, func(i, j int) bool {
			a := fmt.Sprintf("%s-%s", stats[i].Server.Cluster, serverName(&stats[i].Server))
			b := fmt.Sprintf("%s-%s", stats[j].Server.Cluster, serverName(&stats[j].Server))
			return a < b
		})

		// Reset the state of what server has been seen
		for key := range sc.servers {
			sc.servers[key] = false
		}

		sc.logger.Debugln("RTTs for responding servers:")
		for _, stat := range stats {
			// We use for key the cluster name followed by ID which is unique per server
			key := fmt.Sprintf("%s:%s", stat.Server.Cluster, stat.Server.ID)
			// Mark this server has been seen
			sc.servers[key] = true
			sc.logger.Debugf("Server [%15s : %15s : %15s : %s]: %v", stat.Server.Cluster, serverName(&stat.Server), stat.Server.Host, stat.Server.ID, rtts[stat.Server.ID])
		}

		sc.logger.Debugln("Missing servers:")
		var missingServers []string
		for key, seen := range sc.servers {
			if !seen {
				sc.logger.Debugln(key)
				missingServers = append(missingServers, "["+key+"]")
			}
		}

		sc.noReplies.WithLabelValues(strconv.Itoa(sc.numServers)).Add(float64(len(missingServers)))

		sc.logger.Infof("Expected %d servers, only saw responses from %d. Missing %v", sc.numServers, ns, missingServers)
	}

	if ns == sc.numServers || sc.numServers == -1 {
		// Build map of what is our expected set...
		sc.servers = make(map[string]bool)
		for _, stat := range stats {
			key := fmt.Sprintf("%s:%s", stat.Server.Cluster, stat.Server.ID)
			sc.servers[key] = false
		}
	}

	if sc.collectGatewayz {
		err := sc.pollGatewayInfo()
		if err != nil {
			return err
		}
	}
	_, collectJsz := shouldCollectJsz(sc.collectJsz)
	if sc.collectAccounts || collectJsz {
		return sc.pollAccountInfo()
	}
	return nil
}

func shouldCollectJsz(setting CollectJsz) (CollectJsz, bool) {
	jsz := strings.TrimSpace(strings.ToLower(string(setting)))
	enabled := jsz == "all" || jsz == "streams" || jsz == "consumers"
	return CollectJsz(jsz), enabled
}

func (sc *StatzCollector) pollAccountInfo() error {
	nc := sc.nc
	accs, err := sc.getAccStatz(nc)
	if err != nil {
		return err
	}

	accStats := make(map[string]*accountStats, len(accs))
	for accID, stats := range accs {
		sts := mapAccountStat(accID, stats)
		accStats[accID] = sts
	}

	accDetails, jsStats := sc.getJSInfos(nc)
	for accID, accDetail := range accDetails {
		sts := accStats[accID]
		sts = mapJSAccountDetailToStats(accID, accDetail, sts)
		accStats[accID] = sts
	}
	for _, jsStat := range jsStats {
		for _, accDetail := range jsStat.Data.AccountDetails {
			sts, ok := accStats[accDetail.Id]
			if !ok {
				continue
			}
			sts = mapJSStreamDetailToStats(jsStat.Server, accDetail, sts)
			accStats[accDetail.Id] = sts
		}
	}

	sc.Lock()
	sc.jsStats = jsStats
	sc.accStats = make([]*accountStats, 0)
	for _, acc := range accStats {
		sc.accStats = append(sc.accStats, acc)
	}
	sc.Unlock()

	return nil
}

func (sc *StatzCollector) pollGatewayInfo() error {
	gatewayz, err := sc.getGatewayz(sc.nc)
	if err != nil {
		return err
	}

	sc.Lock()
	sc.gatewayStatz = gatewayz
	sc.Unlock()

	return nil
}

func (sc *StatzCollector) getJSInfos(nc *nats.Conn) (map[string]*server.AccountDetail, []*jsStat) {
	var opts server.JSzOptions
	jsz, collectJsz := shouldCollectJsz(sc.collectJsz)
	getConsumers := jsz == CollectJszConsumers || jsz == CollectJszAll
	if sc.collectAccounts {
		opts = server.JSzOptions{
			Accounts:   true,
			Streams:    true,
			Consumer:   true,
			Config:     true,
			Limit:      sc.jszLimit,
			RaftGroups: true,
		}
	} else if collectJsz {
		opts = server.JSzOptions{
			Accounts:   true,
			Streams:    true,
			Consumer:   getConsumers,
			Config:     true,
			Limit:      sc.jszLimit,
			RaftGroups: true,
		}
	}

	jss := make([]*jsStat, 0)
	req, err := json.Marshal(opts)
	if err != nil {
		sc.logger.Warnf("Error marshaling request: %s", err)
	}

	subj := sc.sysReqPrefix + ".SERVER.PING.JSZ"
	msgs, err := requestMany(nc, sc, subj, req, true)
	if err != nil {
		sc.logger.Warnf("Unable to request JetStream info: %s", err)
	}

	for _, msg := range msgs {
		var r server.ServerAPIResponse
		var d server.JSInfo
		r.Data = &d
		if err := unmarshalMsg(msg, &r); err != nil {
			sc.logger.Warnf("Error deserializing JetStream info: %s", err)
			continue
		}
		if r.Error != nil {
			if strings.Contains(r.Error.Description, "jetstream not enabled") {
				// jetstream is not enabled on server
				return nil, nil
			}
			continue
		}
		jss = append(jss, &jsStat{
			Server: r.Server,
			Data:   &d,
		})
		if sc.numServers != -1 && len(jss) == sc.numServers {
			break
		}
	}

	jsAccInfos := make(map[string]*server.AccountDetail)
	for _, jsStat := range jss {
		for _, acc := range jsStat.Data.AccountDetails {
			accInfo, ok := jsAccInfos[acc.Id]
			if !ok {
				jsAccInfos[acc.Id] = acc
				continue
			}
			mergeStreamDetails(accInfo, acc)
			jsAccInfos[acc.Id] = acc
		}
	}

	return jsAccInfos, jss
}

type accStatz struct {
	server.ServerAPIResponse
	Data server.AccountStatz `json:"data,omitempty"`
}

type gatewayStatz struct {
	server.ServerAPIResponse
	Data *server.Gatewayz `json:"data,omitempty"`
}

type jsStat struct {
	Server *server.ServerInfo
	Data   *server.JSInfo
}

type accStat struct {
	Server *server.ServerInfo
	Data   *server.AccountStat
}

func (sc *StatzCollector) getAccStatz(nc *nats.Conn) (map[string][]*accStat, error) {
	req := &server.AccountStatzOptions{
		IncludeUnused: true,
	}
	reqJSON, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	res := make([]*accStatz, 0)
	subj := sc.sysReqPrefix + ".ACCOUNT.PING.STATZ"

	msgs, err := requestMany(nc, sc, subj, reqJSON, true)
	if err != nil {
		sc.logger.Warnf("Error requesting account stats: %s", err.Error())
	}

	for _, msg := range msgs {
		var a accStatz

		if err = unmarshalMsg(msg, &a); err != nil {
			sc.logger.Warnf("Error deserializing account stats: %s", err.Error())
			continue
		}

		if a.Error != nil {
			sc.logger.Warnf("Error in Account stats response: %s", a.Error.Error())
			continue
		}

		res = append(res, &a)
		if sc.numServers != -1 && len(res) == sc.numServers {
			break
		}
	}

	accStats := make(map[string][]*accStat)
	for _, statz := range res {
		for _, acc := range statz.Data.Accounts {
			// optimization to stop reporting a server/account pair
			// when a server is continuously reporting 0 conns for that account
			zeroConnKey := statz.Server.ID + ":" + acc.Account
			if acc.TotalConns == 0 {
				count := sc.accStatZeroConn[zeroConnKey]
				if count >= accStatZeroConnSkip {
					// at limit for continuous polls with 0 conns
					continue
				}

				sc.accStatZeroConn[zeroConnKey] = count + 1
			} else {
				sc.accStatZeroConn[zeroConnKey] = 0
			}
			accInfo := accStats[acc.Account]
			accStats[acc.Account] = append(accInfo, &accStat{
				Server: statz.Server,
				Data:   acc,
			})
		}
	}

	return accStats, nil
}

func (sc *StatzCollector) getGatewayz(nc *nats.Conn) ([]*gatewayStatz, error) {
	req := &server.GatewayzEventOptions{
		GatewayzOptions: server.GatewayzOptions{
			Accounts: false,
		},
	}
	reqJSON, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	res := make([]*gatewayStatz, 0)
	subj := sc.sysReqPrefix + ".SERVER.PING.GATEWAYZ"

	msgs, err := requestMany(nc, sc, subj, reqJSON, true)
	if err != nil {
		sc.logger.Warnf("Error requesting gatewayz stats: %s", err.Error())
	}

	for _, msg := range msgs {
		var g gatewayStatz

		if err = unmarshalMsg(msg, &g); err != nil {
			sc.logger.Warnf("Error deserializing gatewayz stats: %s", err.Error())
			continue
		}

		if g.Error != nil {
			sc.logger.Warnf("Error in Gatewayz stats response: %s", g.Error.Error())
			continue
		}

		res = append(res, &g)
		if sc.numServers != -1 && len(res) == sc.numServers {
			break
		}
	}

	return res, nil
}

func mergeStreamDetails(from, to *server.AccountDetail) {
Outer:
	for _, stream := range from.Streams {
		for i, toStream := range to.Streams {
			if stream.Name == toStream.Name {
				mergeStreamConsumers(&stream, &toStream)
				to.Streams[i] = toStream
				continue Outer
			}
		}
		to.Streams = append(to.Streams, stream)
	}
}

func mergeStreamConsumers(from, to *server.StreamDetail) {
Outer:
	for _, cons := range from.Consumer {
		for _, toCons := range to.Consumer {
			if cons.Name == toCons.Name {
				continue Outer
			}
		}
		to.Consumer = append(to.Consumer, cons)
	}
}

func (sc *StatzCollector) MetricInfos() []MetricInfo {
	metrics := []MetricInfo{
		// surveyor
		sc.natsUp,
		sc.surveyedCnt,
		sc.expectedCnt,
		sc.pollErrCnt,
		sc.pollTime,
		sc.lateReplies,
		sc.noReplies,

		// Server Descriptions
		sc.descs.Info,
		sc.descs.Start,
		sc.descs.Uptime,
		sc.descs.Mem,
		sc.descs.Cores,
		sc.descs.CPU,
		sc.descs.Connections,
		sc.descs.TotalConnections,
		sc.descs.ActiveAccounts,
		sc.descs.NumSubs,
		sc.descs.SentMsgs,
		sc.descs.SentBytes,
		sc.descs.RecvMsgs,
		sc.descs.RecvBytes,
		sc.descs.SlowConsumers,
		sc.descs.RTT,
		sc.descs.Routes,
		sc.descs.Gateways,

		// Route Descriptions
		sc.descs.RouteSentMsgs,
		sc.descs.RouteSentBytes,
		sc.descs.RouteRecvMsgs,
		sc.descs.RouteRecvBytes,
		sc.descs.RoutePending,

		// Gateway Descriptions
		sc.descs.GatewaySentMsgs,
		sc.descs.GatewaySentBytes,
		sc.descs.GatewayRecvMsgs,
		sc.descs.GatewayRecvBytes,
		sc.descs.GatewayNumInbound,

		// Jetstream Descriptions
		// Jetstream Info
		sc.descs.JetstreamInfo,
		// Jetstream Server
		sc.descs.JetstreamEnabled,
		sc.descs.JetstreamFilestoreSizeBytes,
		sc.descs.JetstreamMemstoreSizeBytes,
		sc.descs.JetstreamFilestoreUsedBytes,
		sc.descs.JetstreamFilestoreReservedBytes,
		sc.descs.JetstreamFilestoreReservedUsedBytes,
		sc.descs.JetstreamMemstoreUsedBytes,
		sc.descs.JetstreamMemstoreReservedBytes,
		sc.descs.JetstreamMemstoreReservedUsedBytes,
		sc.descs.JetstreamAccounts,
		sc.descs.JetstreamHAAssets,
		sc.descs.JetstreamAPIRequests,
		sc.descs.JetstreamAPIPending,
		sc.descs.JetstreamAPIErrors,
		// Jetstream Cluster
		sc.descs.JetstreamClusterRaftGroupInfo,
		sc.descs.JetstreamClusterRaftGroupSize,
		sc.descs.JetstreamClusterRaftGroupLeader,
		sc.descs.JetstreamClusterRaftGroupReplicas,
		// Jetstream Cluster Replicas
		sc.descs.JetstreamClusterRaftGroupReplicaActive,
		sc.descs.JetstreamClusterRaftGroupReplicaCurrent,
		sc.descs.JetstreamClusterRaftGroupReplicaOffline,
	}

	// Account scope metrics
	collectJsz, shouldCollectJsz := shouldCollectJsz(sc.collectJsz)
	if sc.collectAccounts || shouldCollectJsz {
		metrics = append(metrics,
			sc.descs.accCount,
			sc.descs.accConnCount,
			sc.descs.accTotalConnCount,
			sc.descs.accLeafCount,
			sc.descs.accSubCount,
			sc.descs.accSlowConsumerCount,

			sc.descs.accBytesSent,
			sc.descs.accBytesRecv,
			sc.descs.accMsgsSent,
			sc.descs.accMsgsRecv,
		)

		if sc.collectAccountsDetailed {
			metrics = append(metrics,
				sc.descs.accClientBytesSent,
				sc.descs.accClientBytesRecv,
				sc.descs.accClientMsgsSent,
				sc.descs.accClientMsgsRecv,
				sc.descs.accLeafBytesSent,
				sc.descs.accLeafBytesRecv,
				sc.descs.accLeafMsgsSent,
				sc.descs.accLeafMsgsRecv,
				sc.descs.accRouteBytesSent,
				sc.descs.accRouteBytesRecv,
				sc.descs.accRouteMsgsSent,
				sc.descs.accRouteMsgsRecv,
				sc.descs.accGatewayBytesSent,
				sc.descs.accGatewayBytesRecv,
				sc.descs.accGatewayMsgsSent,
				sc.descs.accGatewayMsgsRecv,
			)
		}

		metrics = append(metrics,
			// Jetstream Server
			sc.descs.JetstreamServerDisabled,
			sc.descs.JetstreamServerStreams,
			sc.descs.JetstreamServerConsumers,
			sc.descs.JetstreamServerMessages,
			sc.descs.JetstreamServerBytes,
			sc.descs.JetstreamServerMaxMemory,
			sc.descs.JetstreamServerMaxStorage,

			// Account Jetstream
			sc.descs.accJetstreamEnabled,
			sc.descs.accJetstreamMemoryUsed,
			sc.descs.accJetstreamStorageUsed,
			sc.descs.accJetstreamMemoryReserved,
			sc.descs.accJetstreamStorageReserved,
			sc.descs.accJetstreamTieredMemoryUsed,
			sc.descs.accJetstreamTieredStorageUsed,
			sc.descs.accJetstreamTieredMemoryReserved,
			sc.descs.accJetstreamTieredStorageReserved,
			sc.descs.accJetstreamStreamCount,
			sc.descs.accJetstreamConsumerCount,
			sc.descs.accJetstreamReplicaCount,
		)

		if shouldCollectJsz {
			// JSZ Stream metrics.
			if collectJsz == CollectJszAll || collectJsz == CollectJszStreams {
				metrics = append(metrics,
					sc.descs.accJszStreamMsgs,
					sc.descs.accJszStreamBytes,
					sc.descs.accJszStreamFirstSeq,
					sc.descs.accJszStreamLastSeq,
					sc.descs.accJszStreamConsumerCount,
					sc.descs.accJszStreamSubjectCount,
				)
			}

			// JSZ Consumer metrics.
			if collectJsz == CollectJszAll || collectJsz == CollectJszConsumers {
				metrics = append(metrics,
					sc.descs.accJszConsumerDeliveredConsumerSeq,
					sc.descs.accJszConsumerDeliveredStreamSeq,
					sc.descs.accJszConsumerNumAckPending,
					sc.descs.accJszConsumerNumRedelivered,
					sc.descs.accJszConsumerNumWaiting,
					sc.descs.accJszConsumerNumPending,
					sc.descs.accJszConsumerAckFloorStreamSeq,
					sc.descs.accJszConsumerAckFloorConsumerSeq,
				)
			}
		}
	}

	return metrics
}

// Describe is the Prometheus interface to describe metrics for
// the prometheus system
func (sc *StatzCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- sc.natsUp.Desc()

	infos := sc.MetricInfos()
	for _, info := range infos {
		ch <- info.Desc()
	}

	// Surveyor
	sc.surveyedCnt.Describe(ch)
	sc.expectedCnt.Describe(ch)
	sc.pollErrCnt.Describe(ch)
	sc.pollTime.Describe(ch)
	sc.lateReplies.Describe(ch)
	sc.noReplies.Describe(ch)
}

type metricSlice struct {
	sync.Mutex
	metrics []Metric
}

func (ms *metricSlice) appendMetric(m Metric) {
	ms.Lock()
	defer ms.Unlock()
	ms.metrics = append(ms.metrics, m)
}

func (ms *metricSlice) newMetric(info MetricInfo, value float64, valueType prometheus.ValueType, labels []string) {
	m := prometheus.MustNewConstMetric(info.Desc(), valueType, value, labels...)
	metric := newMetric(m, info.Name(), info.Help(), info.Type(), info.ConstLabels())
	ms.appendMetric(metric)
}

func (ms *metricSlice) newGaugeMetric(gauge *GaugeVec, value float64, labels []string) {
	ms.newMetric(gauge, value, prometheus.GaugeValue, labels)
}

func (ms *metricSlice) newCounterMetric(counter *CounterVec, value float64, labels []string) {
	ms.newMetric(counter, value, prometheus.CounterValue, labels)
}

// Collect gathers the streaming server serverz metrics.
func (sc *StatzCollector) Collect(ch chan<- prometheus.Metric) {
	// Run in flightgroup to allow simultaneous query
	result, err, _ := sc.flightGroup.Do("collect", func() (interface{}, error) {
		metrics := &metricSlice{
			metrics: make([]Metric, 0),
			Mutex:   sync.Mutex{},
		}

		timer := prometheus.NewTimer(sc.pollTime.WithLabelValues())

		// poll the servers
		if sc.nc != nil {
			if err := sc.poll(); err != nil {
				sc.logger.Warnf("Error polling NATS server: %v", err)
				sc.pollErrCnt.WithLabelValues().Inc()
				metrics.newMetric(sc.natsUp, 0, prometheus.GaugeValue, nil)
				return metrics.metrics, nil
			}
		}

		// lock the stats
		sc.Lock()
		defer sc.Unlock()

		metrics.newMetric(sc.natsUp, 1, prometheus.GaugeValue, nil)
		sc.surveyedCnt.WithLabelValues().Set(0)

		for _, sm := range sc.stats {
			sc.surveyedCnt.WithLabelValues().Inc()

			metrics.newGaugeMetric(sc.descs.Info, 1, sc.serverInfoLabelValues(&sm.Server))

			labels := sc.serverLabelValues(&sm.Server)
			metrics.newGaugeMetric(sc.descs.Start, float64(sm.Stats.Start.UnixNano()), labels)
			metrics.newGaugeMetric(sc.descs.Uptime, time.Since(sm.Stats.Start).Seconds(), labels)
			metrics.newGaugeMetric(sc.descs.Mem, float64(sm.Stats.Mem), labels)
			metrics.newGaugeMetric(sc.descs.GoMemLimit, float64(sm.Stats.MemLimit), labels)
			metrics.newGaugeMetric(sc.descs.Cores, float64(sm.Stats.Cores), labels)
			metrics.newGaugeMetric(sc.descs.CPU, sm.Stats.CPU, labels)
			metrics.newGaugeMetric(sc.descs.Connections, float64(sm.Stats.Connections), labels)
			metrics.newCounterMetric(sc.descs.TotalConnections, float64(sm.Stats.TotalConnections), labels)
			metrics.newGaugeMetric(sc.descs.ActiveAccounts, float64(sm.Stats.ActiveAccounts), labels)
			metrics.newGaugeMetric(sc.descs.NumSubs, float64(sm.Stats.NumSubs), labels)
			metrics.newCounterMetric(sc.descs.SentMsgs, float64(sm.Stats.Sent.Msgs), labels)
			metrics.newCounterMetric(sc.descs.SentBytes, float64(sm.Stats.Sent.Bytes), labels)
			metrics.newCounterMetric(sc.descs.RecvMsgs, float64(sm.Stats.Received.Msgs), labels)
			metrics.newCounterMetric(sc.descs.RecvBytes, float64(sm.Stats.Received.Bytes), labels)
			metrics.newGaugeMetric(sc.descs.SlowConsumers, float64(sm.Stats.SlowConsumers), labels)
			metrics.newGaugeMetric(sc.descs.RTT, float64(sc.rtts[sm.Server.ID]), labels)
			metrics.newGaugeMetric(sc.descs.Routes, float64(len(sm.Stats.Routes)), labels)
			metrics.newGaugeMetric(sc.descs.Gateways, float64(len(sm.Stats.Gateways)), labels)

			metrics.newGaugeMetric(sc.descs.JetstreamInfo, float64(1), jetstreamInfoLabelValues(sm))
			// Any / All Meta-data in sc.descs.JetstreamInfo can be xrefed by the server_id.
			// labels define the "uniqueness" of a time series, any associations beyond that should be left to prometheus
			lblServerID := []string{sm.Server.ID, sm.Server.Name, sm.Server.Cluster}
			if sm.Stats.JetStream == nil {
				metrics.newGaugeMetric(sc.descs.JetstreamEnabled, float64(0), lblServerID)
			} else {
				metrics.newGaugeMetric(sc.descs.JetstreamEnabled, float64(1), lblServerID)
				if sm.Stats.JetStream.Config != nil {
					metrics.newGaugeMetric(sc.descs.JetstreamFilestoreSizeBytes, float64(sm.Stats.JetStream.Config.MaxStore), lblServerID)
					metrics.newGaugeMetric(sc.descs.JetstreamMemstoreSizeBytes, float64(sm.Stats.JetStream.Config.MaxMemory), lblServerID)
					// StoreDir  At present, '$SYS.REQ.SERVER.PING', server.sendStatsz() squashes StoreDir to "".
					// Domain is also at 'sm.Server.Domain'. Unknown if there's a semantic difference at present. See jsDomainLabelValue().
				}
				if sm.Stats.JetStream.Stats != nil {
					metrics.newGaugeMetric(sc.descs.JetstreamFilestoreUsedBytes, float64(sm.Stats.JetStream.Stats.Store), lblServerID)
					metrics.newGaugeMetric(sc.descs.JetstreamFilestoreReservedBytes, float64(sm.Stats.JetStream.Stats.ReservedStore), lblServerID)
					metrics.newGaugeMetric(sc.descs.JetstreamMemstoreUsedBytes, float64(sm.Stats.JetStream.Stats.Memory), lblServerID)
					metrics.newGaugeMetric(sc.descs.JetstreamMemstoreReservedBytes, float64(sm.Stats.JetStream.Stats.ReservedMemory), lblServerID)
					metrics.newGaugeMetric(sc.descs.JetstreamAccounts, float64(sm.Stats.JetStream.Stats.Accounts), lblServerID)
					metrics.newGaugeMetric(sc.descs.JetstreamHAAssets, float64(sm.Stats.JetStream.Stats.HAAssets), lblServerID)
					// At present, Total does not include Errors. Keeping them separate
					metrics.newCounterMetric(sc.descs.JetstreamAPIRequests, float64(sm.Stats.JetStream.Stats.API.Total), lblServerID)
					metrics.newCounterMetric(sc.descs.JetstreamAPIErrors, float64(sm.Stats.JetStream.Stats.API.Errors), lblServerID)
				}

				if sm.Stats.JetStream.Meta == nil {
					metrics.newGaugeMetric(sc.descs.JetstreamClusterRaftGroupInfo, float64(0), []string{"", "", sm.Server.ID, serverName(&sm.Server), "", ""})
				} else {
					jsRaftGroupInfoLabelValues := []string{jsDomainLabelValue(sm), "_meta_", sm.Server.ID, serverName(&sm.Server), sm.Stats.JetStream.Meta.Name, sm.Stats.JetStream.Meta.Leader}
					metrics.newGaugeMetric(sc.descs.JetstreamClusterRaftGroupInfo, float64(1), jsRaftGroupInfoLabelValues)

					jsRaftGroupLabelValues := []string{sm.Server.ID, serverName(&sm.Server), sm.Server.Cluster}
					// FIXME: add labels needed or remove...

					metrics.newGaugeMetric(sc.descs.JetstreamAPIPending, float64(sm.Stats.JetStream.Meta.Pending), jsRaftGroupLabelValues)
					metrics.newGaugeMetric(sc.descs.JetstreamClusterRaftGroupSize, float64(sm.Stats.JetStream.Meta.Size), jsRaftGroupLabelValues)

					// Could provide false positive if two server have the same server_name in the same or different clusters in the super-cluster...
					// At present, in this statsz only a peer that thinks it's a Leader will have `sm.Stats.JetStream.Meta.Replicas != nil`.
					if sm.Stats.JetStream.Meta.Leader != "" && sm.Server.Name != "" && sm.Server.Name == sm.Stats.JetStream.Meta.Leader {
						metrics.newGaugeMetric(sc.descs.JetstreamClusterRaftGroupLeader, float64(1), jsRaftGroupLabelValues)
					} else {
						metrics.newGaugeMetric(sc.descs.JetstreamClusterRaftGroupLeader, float64(0), jsRaftGroupLabelValues)
					}
					metrics.newGaugeMetric(sc.descs.JetstreamClusterRaftGroupReplicas, float64(len(sm.Stats.JetStream.Meta.Replicas)), jsRaftGroupLabelValues)
					for _, jsr := range sm.Stats.JetStream.Meta.Replicas {
						if jsr == nil {
							continue
						}
						jsClusterReplicaLabelValues := []string{sm.Server.ID, serverName(&sm.Server), jsr.Name, sm.Server.Cluster}
						metrics.newGaugeMetric(sc.descs.JetstreamClusterRaftGroupReplicaActive, float64(jsr.Active), jsClusterReplicaLabelValues)
						if jsr.Current {
							metrics.newGaugeMetric(sc.descs.JetstreamClusterRaftGroupReplicaCurrent, float64(1), jsClusterReplicaLabelValues)
						} else {
							metrics.newGaugeMetric(sc.descs.JetstreamClusterRaftGroupReplicaCurrent, float64(0), jsClusterReplicaLabelValues)
						}
						if jsr.Offline {
							metrics.newGaugeMetric(sc.descs.JetstreamClusterRaftGroupReplicaOffline, float64(1), jsClusterReplicaLabelValues)
						} else {
							metrics.newGaugeMetric(sc.descs.JetstreamClusterRaftGroupReplicaOffline, float64(0), jsClusterReplicaLabelValues)
						}
					}
				}
			}

			for _, rs := range sm.Stats.Routes {
				labels = sc.routeLabelValues(&sm.Server, rs)
				metrics.newCounterMetric(sc.descs.RouteSentMsgs, float64(rs.Sent.Msgs), labels)
				metrics.newCounterMetric(sc.descs.RouteSentBytes, float64(rs.Sent.Bytes), labels)
				metrics.newCounterMetric(sc.descs.RouteRecvMsgs, float64(rs.Received.Msgs), labels)
				metrics.newCounterMetric(sc.descs.RouteRecvBytes, float64(rs.Received.Bytes), labels)
				metrics.newGaugeMetric(sc.descs.RoutePending, float64(rs.Pending), labels)
			}

			for _, gw := range sm.Stats.Gateways {
				labels = sc.gatewayLabelValues(&sm.Server, gw)
				metrics.newCounterMetric(sc.descs.GatewaySentMsgs, float64(gw.Sent.Msgs), labels)
				metrics.newCounterMetric(sc.descs.GatewaySentBytes, float64(gw.Sent.Bytes), labels)
				metrics.newCounterMetric(sc.descs.GatewayRecvMsgs, float64(gw.Received.Msgs), labels)
				metrics.newCounterMetric(sc.descs.GatewayRecvBytes, float64(gw.Received.Bytes), labels)
				metrics.newGaugeMetric(sc.descs.GatewayNumInbound, float64(gw.NumInbound), labels)
			}
		}

		// Account scope metrics
		_, collectJsz := shouldCollectJsz(sc.collectJsz)
		if sc.collectAccounts || collectJsz {
			metrics.newGaugeMetric(sc.descs.accCount, float64(len(sc.accStats)), nil)
			for _, stat := range sc.accStats {
				accLabels := []string{stat.accountID, stat.accountName}
				for _, as := range stat.stats {
					serverAndAccLabels := append(sc.serverLabelValues(as.Server), accLabels...)
					metrics.newGaugeMetric(sc.descs.accConnCount, float64(as.Data.Conns), serverAndAccLabels)
					metrics.newGaugeMetric(sc.descs.accTotalConnCount, float64(as.Data.TotalConns), serverAndAccLabels)
					metrics.newGaugeMetric(sc.descs.accLeafCount, float64(as.Data.LeafNodes), serverAndAccLabels)
					metrics.newGaugeMetric(sc.descs.accSubCount, float64(as.Data.NumSubs), serverAndAccLabels)
					metrics.newGaugeMetric(sc.descs.accSlowConsumerCount, float64(as.Data.SlowConsumers), serverAndAccLabels)
					metrics.newCounterMetric(sc.descs.accBytesSent, float64(as.Data.Sent.Bytes), serverAndAccLabels)
					metrics.newCounterMetric(sc.descs.accBytesRecv, float64(as.Data.Received.Bytes), serverAndAccLabels)
					metrics.newCounterMetric(sc.descs.accMsgsSent, float64(as.Data.Sent.Msgs), serverAndAccLabels)
					metrics.newCounterMetric(sc.descs.accMsgsRecv, float64(as.Data.Received.Msgs), serverAndAccLabels)

					if sc.collectAccountsDetailed {
						metrics.newCounterMetric(sc.descs.accClientBytesSent, float64(as.Data.Sent.Bytes-as.Data.Sent.Routes.Bytes-as.Data.Sent.Gateways.Bytes-as.Data.Sent.Leafs.Bytes), serverAndAccLabels)
						metrics.newCounterMetric(sc.descs.accClientBytesRecv, float64(as.Data.Received.Bytes-as.Data.Received.Routes.Bytes-as.Data.Received.Gateways.Bytes-as.Data.Received.Leafs.Bytes), serverAndAccLabels)
						metrics.newCounterMetric(sc.descs.accClientMsgsSent, float64(as.Data.Sent.Msgs-as.Data.Sent.Routes.Msgs-as.Data.Sent.Gateways.Msgs-as.Data.Sent.Leafs.Msgs), serverAndAccLabels)
						metrics.newCounterMetric(sc.descs.accClientMsgsRecv, float64(as.Data.Received.Msgs-as.Data.Received.Routes.Msgs-as.Data.Received.Gateways.Msgs-as.Data.Received.Leafs.Msgs), serverAndAccLabels)
						metrics.newCounterMetric(sc.descs.accLeafBytesSent, float64(as.Data.Sent.Leafs.Bytes), serverAndAccLabels)
						metrics.newCounterMetric(sc.descs.accLeafBytesRecv, float64(as.Data.Received.Leafs.Bytes), serverAndAccLabels)
						metrics.newCounterMetric(sc.descs.accLeafMsgsSent, float64(as.Data.Sent.Leafs.Msgs), serverAndAccLabels)
						metrics.newCounterMetric(sc.descs.accLeafMsgsRecv, float64(as.Data.Received.Leafs.Msgs), serverAndAccLabels)
						metrics.newCounterMetric(sc.descs.accRouteBytesSent, float64(as.Data.Sent.Routes.Bytes), serverAndAccLabels)
						metrics.newCounterMetric(sc.descs.accRouteBytesRecv, float64(as.Data.Received.Routes.Bytes), serverAndAccLabels)
						metrics.newCounterMetric(sc.descs.accRouteMsgsSent, float64(as.Data.Sent.Routes.Msgs), serverAndAccLabels)
						metrics.newCounterMetric(sc.descs.accRouteMsgsRecv, float64(as.Data.Received.Routes.Msgs), serverAndAccLabels)
						metrics.newCounterMetric(sc.descs.accGatewayBytesSent, float64(as.Data.Sent.Gateways.Bytes), serverAndAccLabels)
						metrics.newCounterMetric(sc.descs.accGatewayBytesRecv, float64(as.Data.Received.Gateways.Bytes), serverAndAccLabels)
						metrics.newCounterMetric(sc.descs.accGatewayMsgsSent, float64(as.Data.Sent.Gateways.Msgs), serverAndAccLabels)
						metrics.newCounterMetric(sc.descs.accGatewayMsgsRecv, float64(as.Data.Received.Gateways.Msgs), serverAndAccLabels)
					}
				}

				metrics.newGaugeMetric(sc.descs.accJetstreamEnabled, stat.jetstreamEnabled, accLabels)
				metrics.newGaugeMetric(sc.descs.accJetstreamMemoryUsed, stat.jetstreamMemoryUsed, accLabels)
				metrics.newGaugeMetric(sc.descs.accJetstreamStorageUsed, stat.jetstreamStorageUsed, accLabels)
				metrics.newGaugeMetric(sc.descs.accJetstreamMemoryReserved, stat.jetstreamMemoryReserved, accLabels)
				metrics.newGaugeMetric(sc.descs.accJetstreamStorageReserved, stat.jetstreamStorageReserved, accLabels)
				for tier, size := range stat.jetstreamTieredMemoryUsed {
					metrics.newGaugeMetric(sc.descs.accJetstreamTieredMemoryUsed, size, append(accLabels, fmt.Sprintf("R%d", tier)))
				}
				for tier, size := range stat.jetstreamTieredStorageUsed {
					metrics.newGaugeMetric(sc.descs.accJetstreamTieredStorageUsed, size, append(accLabels, fmt.Sprintf("R%d", tier)))
				}
				for tier, size := range stat.jetstreamTieredMemoryReserved {
					metrics.newGaugeMetric(sc.descs.accJetstreamTieredMemoryReserved, size, append(accLabels, fmt.Sprintf("R%d", tier)))
				}
				for tier, size := range stat.jetstreamTieredStorageReserved {
					metrics.newGaugeMetric(sc.descs.accJetstreamTieredStorageReserved, size, append(accLabels, fmt.Sprintf("R%d", tier)))
				}

				metrics.newGaugeMetric(sc.descs.accJetstreamStreamCount, stat.jetstreamStreamCount, accLabels)
				for _, streamStat := range stat.jetstreamStreams {
					metrics.newGaugeMetric(sc.descs.accJetstreamConsumerCount, streamStat.consumerCount, append(accLabels, streamStat.streamName, streamStat.raftGroup))
					metrics.newGaugeMetric(sc.descs.accJetstreamReplicaCount, streamStat.replicaCount, append(accLabels, streamStat.streamName, streamStat.raftGroup))
				}

				collectJsz, shouldCollectJsz := shouldCollectJsz(sc.collectJsz)
				if !shouldCollectJsz {
					continue
				}
				for _, streamStat := range stat.jszData {
					hasFilters := len(sc.jszFilterSet) > 0

					isLeaderOrAll := !sc.jszLeadersOnly || sc.jszLeadersOnly && streamStat.serverName == streamStat.streamLeader
					showJszStreams := collectJsz == CollectJszAll || collectJsz == CollectJszStreams
					if isLeaderOrAll && showJszStreams {
						if sc.jszFilterSet[StreamTotalMessages] || !hasFilters {
							metrics.newGaugeMetric(sc.descs.accJszStreamMsgs,
								streamStat.streamMessages,
								append(accLabels,
									streamStat.clusterName, streamStat.raftGroup, streamStat.serverID, streamStat.serverName,
									streamStat.streamName, streamStat.streamLeader,
								),
							)
						}

						if sc.jszFilterSet[StreamTotalBytes] || !hasFilters {
							metrics.newGaugeMetric(sc.descs.accJszStreamBytes,
								streamStat.streamBytes,
								append(accLabels,
									streamStat.clusterName, streamStat.raftGroup, streamStat.serverID, streamStat.serverName,
									streamStat.streamName, streamStat.streamLeader,
								),
							)
						}

						if sc.jszFilterSet[StreamFirstSeq] || !hasFilters {
							metrics.newGaugeMetric(sc.descs.accJszStreamFirstSeq,
								streamStat.streamFirstSeq,
								append(accLabels,
									streamStat.clusterName, streamStat.raftGroup, streamStat.serverID, streamStat.serverName,
									streamStat.streamName, streamStat.streamLeader,
								),
							)
						}

						if sc.jszFilterSet[StreamLastSeq] || !hasFilters {
							metrics.newGaugeMetric(sc.descs.accJszStreamLastSeq,
								streamStat.streamLastSeq,
								append(accLabels,
									streamStat.clusterName, streamStat.raftGroup, streamStat.serverID, streamStat.serverName,
									streamStat.streamName, streamStat.streamLeader,
								),
							)
						}

						if sc.jszFilterSet[StreamConsumerCount] || !hasFilters {
							metrics.newGaugeMetric(sc.descs.accJszStreamConsumerCount,
								streamStat.streamConsumerCount,
								append(accLabels,
									streamStat.clusterName, streamStat.raftGroup, streamStat.serverID, streamStat.serverName,
									streamStat.streamName, streamStat.streamLeader,
								),
							)
						}

						if sc.jszFilterSet[StreamSubjectCount] || !hasFilters {
							metrics.newGaugeMetric(sc.descs.accJszStreamSubjectCount,
								streamStat.streamSubjectCount,
								append(accLabels,
									streamStat.clusterName, streamStat.raftGroup, streamStat.serverID, streamStat.serverName,
									streamStat.streamName, streamStat.streamLeader,
								),
							)
						}
					}

					for _, consumerStat := range streamStat.consumerStats {
						isLeaderOrAll := !sc.jszLeadersOnly || sc.jszLeadersOnly && streamStat.serverName == consumerStat.consumerLeader
						showJszConsumers := collectJsz == CollectJszAll || collectJsz == CollectJszConsumers
						if isLeaderOrAll && showJszConsumers {
							raftGroup := consumerStat.consumerRaftGroup

							if sc.jszFilterSet[ConsumerDeliveredConsumerSeq] || !hasFilters {
								metrics.newGaugeMetric(sc.descs.accJszConsumerDeliveredConsumerSeq,
									consumerStat.consumerDeliveredConsumerSeq,
									append(accLabels,
										streamStat.clusterName, raftGroup, streamStat.serverID, streamStat.serverName,
										streamStat.streamName, streamStat.streamLeader, consumerStat.consumerName, consumerStat.consumerLeader,
									),
								)
							}

							if sc.jszFilterSet[ConsumerDeliveredStreamSeq] || !hasFilters {
								metrics.newGaugeMetric(sc.descs.accJszConsumerDeliveredStreamSeq,
									consumerStat.consumerDeliveredStreamSeq,
									append(accLabels,
										streamStat.clusterName, raftGroup, streamStat.serverID, streamStat.serverName,
										streamStat.streamName, streamStat.streamLeader, consumerStat.consumerName, consumerStat.consumerLeader,
									),
								)
							}
							if sc.jszFilterSet[ConsumerNumAckPending] || !hasFilters {
								metrics.newGaugeMetric(sc.descs.accJszConsumerNumAckPending,
									consumerStat.consumerNumAckPending,
									append(accLabels,
										streamStat.clusterName, raftGroup, streamStat.serverID, streamStat.serverName,
										streamStat.streamName, streamStat.streamLeader, consumerStat.consumerName, consumerStat.consumerLeader,
									),
								)
							}
							if sc.jszFilterSet[ConsumerNumRedelivered] || !hasFilters {
								metrics.newGaugeMetric(sc.descs.accJszConsumerNumRedelivered,
									consumerStat.consumerNumRedelivered,
									append(accLabels,
										streamStat.clusterName, raftGroup, streamStat.serverID, streamStat.serverName,
										streamStat.streamName, streamStat.streamLeader, consumerStat.consumerName, consumerStat.consumerLeader,
									),
								)
							}
							if sc.jszFilterSet[ConsumerNumWaiting] || !hasFilters {
								metrics.newGaugeMetric(sc.descs.accJszConsumerNumWaiting,
									consumerStat.consumerNumWaiting,
									append(accLabels,
										streamStat.clusterName, raftGroup, streamStat.serverID, streamStat.serverName,
										streamStat.streamName, streamStat.streamLeader, consumerStat.consumerName, consumerStat.consumerLeader,
									),
								)
							}
							if sc.jszFilterSet[ConsumerNumPending] || !hasFilters {
								metrics.newGaugeMetric(sc.descs.accJszConsumerNumPending,
									consumerStat.consumerNumPending,
									append(accLabels,
										streamStat.clusterName, raftGroup, streamStat.serverID, streamStat.serverName,
										streamStat.streamName, streamStat.streamLeader, consumerStat.consumerName, consumerStat.consumerLeader,
									),
								)
							}
							if sc.jszFilterSet[ConsumerAckFloorStreamSeq] || !hasFilters {
								metrics.newGaugeMetric(sc.descs.accJszConsumerAckFloorStreamSeq,
									consumerStat.consumerAckFloorStreamSeq,
									append(accLabels,
										streamStat.clusterName, raftGroup, streamStat.serverID, streamStat.serverName,
										streamStat.streamName, streamStat.streamLeader, consumerStat.consumerName, consumerStat.consumerLeader,
									),
								)
							}
							if sc.jszFilterSet[ConsumerAckFloorConsumerSeq] || !hasFilters {
								metrics.newGaugeMetric(sc.descs.accJszConsumerAckFloorConsumerSeq,
									consumerStat.consumerAckFloorConsumerSeq,
									append(accLabels,
										streamStat.clusterName, raftGroup, streamStat.serverID, streamStat.serverName,
										streamStat.streamName, streamStat.streamLeader, consumerStat.consumerName, consumerStat.consumerLeader,
									),
								)
							}
						}
					}
				}
			}

			for _, jss := range sc.jsStats {
				jsServerLabelValues := []string{jss.Server.ID, jss.Server.Name, jss.Server.Cluster}
				var isJetStreamDisabled float64
				if jss.Data.Disabled {
					isJetStreamDisabled = 1
				}
				metrics.newGaugeMetric(sc.descs.JetstreamServerDisabled, isJetStreamDisabled, jsServerLabelValues)
				metrics.newGaugeMetric(sc.descs.JetstreamServerStreams, float64(jss.Data.Streams), jsServerLabelValues)
				metrics.newGaugeMetric(sc.descs.JetstreamServerConsumers, float64(jss.Data.Consumers), jsServerLabelValues)
				metrics.newGaugeMetric(sc.descs.JetstreamServerMessages, float64(jss.Data.Messages), jsServerLabelValues)
				metrics.newGaugeMetric(sc.descs.JetstreamServerBytes, float64(jss.Data.Bytes), jsServerLabelValues)
				metrics.newGaugeMetric(sc.descs.JetstreamServerMaxMemory, float64(jss.Data.Config.MaxMemory), jsServerLabelValues)
				metrics.newGaugeMetric(sc.descs.JetstreamServerMaxStorage, float64(jss.Data.Config.MaxStore), jsServerLabelValues)
			}
		}

		// Gatewayz metrics
		if sc.collectGatewayz {
			for _, gwStat := range sc.gatewayStatz {
				if gwStat == nil || gwStat.Data == nil {
					continue
				}

				for rgwName, gw := range gwStat.Data.OutboundGateways {
					collectGatewayzMetrics(metrics, sc.descs.OutboundGateways, rgwName, gwStat, gw)
				}

				for rgwName, gws := range gwStat.Data.InboundGateways {
					for _, gw := range gws {
						collectGatewayzMetrics(metrics, sc.descs.InboundGateways, rgwName, gwStat, gw)
					}
				}
			}
		}

		collectCh := make(chan prometheus.Metric)

		surveyorMetrics := []MetricVec{
			sc.pollTime,
			sc.pollErrCnt,
			sc.surveyedCnt,
			sc.expectedCnt,
			sc.lateReplies,
			sc.noReplies,
		}

		// We want to collect these before we exit the flight group
		// but they should still be sent to every caller
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			var i int
			for m := range collectCh {
				sm := surveyorMetrics[i]
				metric := newMetric(m, sm.Name(), sm.Help(), sm.Type(), sm.ConstLabels())
				metrics.appendMetric(metric)
				i++
			}

			wg.Done()
		}()

		timer.ObserveDuration()
		for _, m := range surveyorMetrics {
			m.Vec().Collect(collectCh)
		}

		close(collectCh)

		wg.Wait()

		return metrics.metrics, nil
	})
	if err != nil {
		sc.logger.Error(err)
		return
	}

	var metrics []Metric
	var ok bool
	if metrics, ok = result.([]Metric); !ok || metrics == nil {
		if metrics == nil {
			sc.logger.Error("no metrics collected")
		} else {
			sc.logger.Error("unexpected collect response type")
		}
		return
	}

	for _, m := range metrics {
		ch <- m.Metric()
	}
}

func collectGatewayzMetrics(metrics *metricSlice, gwDescs *gatewayzDescs, rgwName string, gwStat *gatewayStatz, gw *server.RemoteGatewayz) {
	if gw == nil || gw.Connection == nil || gwStat == nil || gwStat.Server == nil || gwStat.Data == nil {
		return
	}

	var isConfigured float64
	if gw.IsConfigured {
		isConfigured = 1
	}
	serverID := gwStat.Server.ID
	serverName := gwStat.Server.Name
	gwName := gwStat.Data.Name
	gwID := gw.Connection.Name
	cid := strconv.FormatUint(gw.Connection.Cid, 10)

	// server_id, server_name, gateway_name, remote_gateway_name, gateway_id, cid
	labels := []string{serverID, serverName, gwName, rgwName, gwID, cid}
	uptime, _ := time.ParseDuration(gw.Connection.Uptime)
	idle, _ := time.ParseDuration(gw.Connection.Idle)
	rtt, _ := time.ParseDuration(gw.Connection.RTT)

	metrics.newGaugeMetric(gwDescs.configured, isConfigured, labels)
	metrics.newGaugeMetric(gwDescs.connLastActivity, float64(gw.Connection.LastActivity.Unix()), labels)
	metrics.newGaugeMetric(gwDescs.connUptime, uptime.Seconds(), labels)
	metrics.newGaugeMetric(gwDescs.connIdle, idle.Seconds(), labels)
	metrics.newGaugeMetric(gwDescs.connRtt, rtt.Seconds(), labels)
	metrics.newGaugeMetric(gwDescs.connPendingBytes, float64(gw.Connection.Pending), labels)
	metrics.newGaugeMetric(gwDescs.connInMsgs, float64(gw.Connection.InMsgs), labels)
	metrics.newGaugeMetric(gwDescs.connOutMsgs, float64(gw.Connection.OutMsgs), labels)
	metrics.newGaugeMetric(gwDescs.connInBytes, float64(gw.Connection.InBytes), labels)
	metrics.newGaugeMetric(gwDescs.connOutBytes, float64(gw.Connection.OutBytes), labels)
	metrics.newGaugeMetric(gwDescs.connSubscriptions, float64(gw.Connection.NumSubs), labels)
}

func requestMany(nc *nats.Conn, sc *StatzCollector, subject string, data []byte, compression bool) ([]*nats.Msg, error) {
	if subject == "" {
		return nil, fmt.Errorf("subject cannot be empty")
	}

	inbox := nats.NewInbox()
	res := make([]*nats.Msg, 0)
	msgsChan := make(chan *nats.Msg, 100)

	intervalTimer := time.NewTimer(sc.pollTimeout)
	sub, err := nc.Subscribe(inbox, func(msg *nats.Msg) {
		intervalTimer.Reset(sc.serverDiscoveryWait)
		msgsChan <- msg
	})
	defer sub.Unsubscribe()

	msg := &nats.Msg{
		Subject: subject,
		Reply:   inbox,
		Data:    data,
		Header:  nats.Header{},
	}

	if compression {
		msg.Header.Set("Accept-Encoding", "snappy")
	}

	if err := nc.PublishMsg(msg); err != nil {
		return nil, err
	}

	for {
		select {
		case msg := <-msgsChan:
			if msg.Header.Get("Status") == "503" {
				return nil, fmt.Errorf("server request on subject %q failed: %w", subject, err)
			}
			res = append(res, msg)
			if sc.numServers != -1 && len(res) == sc.numServers {
				return res, nil
			}
		case <-intervalTimer.C:
			return res, nil
		case <-time.After(sc.pollTimeout):
			return res, nil
		}
	}
}

// Performs JSON Unmarshal, decompressing snappy encoding if necessary
func unmarshalMsg(msg *nats.Msg, v any) error {
	if msg.Header.Get("Status") == "503" && len(msg.Data) == 0 {
		return fmt.Errorf("got a message with status 503: No responders available: %v", msg)
	}
	data := msg.Data

	if msg.Header.Get("Content-Encoding") == "snappy" {
		var err error
		data, err = io.ReadAll(s2.NewReader(bytes.NewBuffer(data)))
		if err != nil {
			return err
		}
	}

	return json.Unmarshal(data, v)
}

// mapAccountStat creates account stats from the server account statz response.
func mapAccountStat(accID string, stats []*accStat) *accountStats {
	accountName := ""
	if len(stats) > 0 && stats[0].Data != nil {
		accountName = stats[0].Data.Name
	}
	sts := &accountStats{
		accountID:   accID,
		accountName: accountName,
		stats:       stats,
	}
	return sts
}

// mapJSAccountDetail creates or updates account stats from the server JS Info response.
func mapJSAccountDetailToStats(accID string, accDetail *server.AccountDetail, sts *accountStats) *accountStats {
	// If no account stats returned, still report JS metrics
	if sts == nil {
		sts = &accountStats{
			accountID:   accID,
			accountName: accDetail.Name,
		}
	}

	sts.jetstreamEnabled = 1.0
	sts.jetstreamMemoryUsed = float64(accDetail.Memory)
	sts.jetstreamStorageUsed = float64(accDetail.Store)
	sts.jetstreamMemoryReserved = float64(accDetail.ReservedMemory)
	sts.jetstreamStorageReserved = float64(accDetail.ReservedStore)
	sts.jetstreamTieredMemoryUsed = make(map[int]float64)
	sts.jetstreamTieredStorageUsed = make(map[int]float64)
	sts.jetstreamTieredMemoryReserved = make(map[int]float64)
	sts.jetstreamTieredStorageReserved = make(map[int]float64)

	sts.jetstreamStreamCount = float64(len(accDetail.Streams))
	for _, stream := range accDetail.Streams {
		sts.jetstreamStreams = append(sts.jetstreamStreams, streamAccountStats{
			streamName:    stream.Name,
			raftGroup:     stream.RaftGroup,
			consumerCount: float64(len(stream.Consumer)),
			replicaCount:  float64(stream.Config.Replicas),
		})

		// computed tiered storage usage
		used := float64(stream.State.Bytes)
		var reserved float64
		if stream.Config.MaxBytes > 0 {
			reserved = float64(stream.Config.MaxBytes)
		}
		switch stream.Config.Storage {
		case server.MemoryStorage:
			if _, ok := sts.jetstreamTieredMemoryUsed[stream.Config.Replicas]; ok {
				sts.jetstreamTieredMemoryUsed[stream.Config.Replicas] += used
				sts.jetstreamTieredMemoryReserved[stream.Config.Replicas] += reserved
			} else {
				sts.jetstreamTieredMemoryUsed[stream.Config.Replicas] = used
				sts.jetstreamTieredMemoryReserved[stream.Config.Replicas] = reserved
			}
		case server.FileStorage:
			if _, ok := sts.jetstreamTieredStorageUsed[stream.Config.Replicas]; ok {
				sts.jetstreamTieredStorageUsed[stream.Config.Replicas] += used
				sts.jetstreamTieredStorageReserved[stream.Config.Replicas] += reserved
			} else {
				sts.jetstreamTieredStorageUsed[stream.Config.Replicas] = used
				sts.jetstreamTieredStorageReserved[stream.Config.Replicas] = reserved
			}
		}
	}
	return sts
}

// mapJSStreamDetailToStats updates account stats with JS stream detail from the server response.
func mapJSStreamDetailToStats(server *server.ServerInfo, accDetail *server.AccountDetail, sts *accountStats) *accountStats {
	if sts == nil {
		return nil
	}
	for _, stream := range accDetail.Streams {
		var streamLeader string
		if stream.Cluster != nil {
			streamLeader = stream.Cluster.Leader
		}
		sas := streamAccountStats{
			accountID:          accDetail.Id,
			accountName:        accDetail.Name,
			serverID:           server.ID,
			serverName:         server.Name,
			clusterName:        server.Cluster,
			streamName:         stream.Name,
			raftGroup:          stream.RaftGroup,
			consumerCount:      float64(stream.State.Consumers),
			replicaCount:       float64(stream.Config.Replicas),
			streamMessages:     float64(stream.State.Msgs),
			streamBytes:        float64(stream.State.Bytes),
			streamFirstSeq:     float64(stream.State.FirstSeq),
			streamLastSeq:      float64(stream.State.LastSeq),
			streamSubjectCount: float64(stream.State.NumSubjects),
			streamLeader:       streamLeader,
			consumerStats:      make([]*consumerStats, 0),
		}

		for _, consumer := range stream.Consumer {
			var consumerLeader, raftGroup string
			if consumer.Cluster != nil {
				consumerLeader = consumer.Cluster.Leader
				raftGroup = consumer.Cluster.RaftGroup
			}
			if raftGroup == "" {
				// For R1 may need to find manually within the complete list
				// of raft groups.
				for _, cr := range stream.ConsumerRaftGroups {
					if cr.Name == consumer.Name {
						raftGroup = cr.RaftGroup
						break
					}
				}
			}
			cs := consumerStats{
				consumerName:                 consumer.Name,
				consumerLeader:               consumerLeader,
				consumerRaftGroup:            raftGroup,
				consumerDeliveredConsumerSeq: float64(consumer.Delivered.Consumer),
				consumerDeliveredStreamSeq:   float64(consumer.Delivered.Stream),
				consumerNumAckPending:        float64(consumer.NumAckPending),
				consumerNumRedelivered:       float64(consumer.NumRedelivered),
				consumerNumWaiting:           float64(consumer.NumWaiting),
				consumerNumPending:           float64(consumer.NumPending),
				consumerAckFloorStreamSeq:    float64(consumer.AckFloor.Stream),
				consumerAckFloorConsumerSeq:  float64(consumer.AckFloor.Consumer),
			}
			sas.consumerStats = append(sas.consumerStats, &cs)
		}
		sts.jszData = append(sts.jszData, sas)
	}
	return sts
}
