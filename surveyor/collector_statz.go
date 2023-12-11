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
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/singleflight"
)

// statzDescs holds the metric descriptions
type statzDescs struct {
	Info             *prometheus.Desc
	Start            *prometheus.Desc
	Uptime           *prometheus.Desc
	Mem              *prometheus.Desc
	Cores            *prometheus.Desc
	CPU              *prometheus.Desc
	Connections      *prometheus.Desc
	TotalConnections *prometheus.Desc
	ActiveAccounts   *prometheus.Desc
	NumSubs          *prometheus.Desc
	SentMsgs         *prometheus.Desc
	SentBytes        *prometheus.Desc
	RecvMsgs         *prometheus.Desc
	RecvBytes        *prometheus.Desc
	SlowConsumers    *prometheus.Desc
	RTT              *prometheus.Desc
	Routes           *prometheus.Desc
	Gateways         *prometheus.Desc

	// Routes
	RouteSentMsgs  *prometheus.Desc
	RouteSentBytes *prometheus.Desc
	RouteRecvMsgs  *prometheus.Desc
	RouteRecvBytes *prometheus.Desc
	RoutePending   *prometheus.Desc

	// Gateways
	GatewaySentMsgs   *prometheus.Desc
	GatewaySentBytes  *prometheus.Desc
	GatewayRecvMsgs   *prometheus.Desc
	GatewayRecvBytes  *prometheus.Desc
	GatewayNumInbound *prometheus.Desc

	// Jetstream Info
	JetstreamInfo *prometheus.Desc
	// Jetstream Server
	JetstreamEnabled                    *prometheus.Desc
	JetstreamFilestoreSizeBytes         *prometheus.Desc
	JetstreamMemstoreSizeBytes          *prometheus.Desc
	JetstreamFilestoreUsedBytes         *prometheus.Desc
	JetstreamFilestoreReservedBytes     *prometheus.Desc
	JetstreamFilestoreReservedUsedBytes *prometheus.Desc
	JetstreamMemstoreUsedBytes          *prometheus.Desc
	JetstreamMemstoreReservedBytes      *prometheus.Desc
	JetstreamMemstoreReservedUsedBytes  *prometheus.Desc
	JetstreamAccounts                   *prometheus.Desc
	JetstreamHAAssets                   *prometheus.Desc
	JetstreamAPIRequests                *prometheus.Desc
	JetstreamAPIErrors                  *prometheus.Desc
	// Jetstream Cluster
	JetstreamClusterRaftGroupInfo     *prometheus.Desc
	JetstreamClusterRaftGroupSize     *prometheus.Desc
	JetstreamClusterRaftGroupLeader   *prometheus.Desc
	JetstreamClusterRaftGroupReplicas *prometheus.Desc
	// Jetstream Cluster Replicas
	JetstreamClusterRaftGroupReplicaActive  *prometheus.Desc
	JetstreamClusterRaftGroupReplicaCurrent *prometheus.Desc
	JetstreamClusterRaftGroupReplicaOffline *prometheus.Desc

	// Account scope metrics
	accCount                          *prometheus.Desc
	accConnCount                      *prometheus.Desc
	accLeafCount                      *prometheus.Desc
	accSubCount                       *prometheus.Desc
	accBytesSent                      *prometheus.Desc
	accBytesRecv                      *prometheus.Desc
	accMsgsSent                       *prometheus.Desc
	accMsgsRecv                       *prometheus.Desc
	accJetstreamEnabled               *prometheus.Desc
	accJetstreamMemoryUsed            *prometheus.Desc
	accJetstreamStorageUsed           *prometheus.Desc
	accJetstreamMemoryReserved        *prometheus.Desc
	accJetstreamStorageReserved       *prometheus.Desc
	accJetstreamTieredMemoryUsed      *prometheus.Desc
	accJetstreamTieredStorageUsed     *prometheus.Desc
	accJetstreamTieredMemoryReserved  *prometheus.Desc
	accJetstreamTieredStorageReserved *prometheus.Desc
	accJetstreamStreamCount           *prometheus.Desc
	accJetstreamConsumerCount         *prometheus.Desc
	accJetstreamReplicaCount          *prometheus.Desc
}

// StatzCollector collects statz from a server deployment
type StatzCollector struct {
	sync.Mutex
	flightGroup         singleflight.Group
	nc                  *nats.Conn
	logger              *logrus.Logger
	start               time.Time
	uptime              time.Duration
	stats               []*server.ServerStatsMsg
	statsChan           chan *server.ServerStatsMsg
	accStats            []accountStats
	rtts                map[string]time.Duration
	pollTimeout         time.Duration
	reply               string
	polling             bool
	pollkey             string
	numServers          int
	serverDiscoveryWait time.Duration
	servers             map[string]bool
	doneCh              chan struct{}
	descs               statzDescs
	collectAccounts     bool
	natsUp              *prometheus.Desc

	serverLabels       []string
	serverInfoLabels   []string
	routeLabels        []string
	gatewayLabels      []string
	jsServerLabels     []string
	jsServerInfoLabels []string
	constLabels        prometheus.Labels

	surveyedCnt *prometheus.GaugeVec
	expectedCnt *prometheus.GaugeVec
	pollErrCnt  *prometheus.CounterVec
	pollTime    *prometheus.SummaryVec
	lateReplies *prometheus.CounterVec
	noReplies   *prometheus.CounterVec
}

type accountStats struct {
	accountID string

	connCount float64
	subCount  float64
	leafCount float64

	bytesSent float64
	bytesRecv float64
	msgsSent  float64
	msgsRecv  float64

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
}

type streamAccountStats struct {
	streamName    string
	consumerCount float64
	replicaCount  float64
}

func serverName(sm *server.ServerStatsMsg) string {
	if sm.Server.Name == "" {
		return sm.Server.ID
	}

	return sm.Server.Name
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

func (sc *StatzCollector) serverLabelValues(sm *server.ServerStatsMsg) []string {
	return []string{sm.Server.Cluster, serverName(sm), sm.Server.ID}
}

func (sc *StatzCollector) serverInfoLabelValues(sm *server.ServerStatsMsg) []string {
	return []string{sm.Server.Cluster, serverName(sm), sm.Server.ID, sm.Server.Version}
}

func (sc *StatzCollector) routeLabelValues(sm *server.ServerStatsMsg, rStat *server.RouteStat) []string {
	return []string{sm.Server.Cluster, serverName(sm), sm.Server.ID, strconv.FormatUint(rStat.ID, 10)}
}

func (sc *StatzCollector) gatewayLabelValues(sm *server.ServerStatsMsg, gStat *server.GatewayStat) []string {
	return []string{sm.Server.Cluster, serverName(sm), sm.Server.ID, gStat.Name, strconv.FormatUint(gStat.ID, 10)}
}

// Up/Down on servers - look at discovery mechanisms in Prometheus - aging out, how does it work?
func (sc *StatzCollector) buildDescs() {
	newPromDesc := func(name, help string, labels []string) *prometheus.Desc {
		return prometheus.NewDesc(
			prometheus.BuildFQName("nats", "core", name), help, labels, sc.constLabels)
	}

	// A unlabelled description for the up/down
	sc.natsUp = prometheus.NewDesc(prometheus.BuildFQName("nats", "core", "nats_up"),
		"1 if connected to NATS, 0 otherwise.  A gauge.", nil, sc.constLabels)

	sc.descs.Info = newPromDesc("info", "General Server information Summary gauge", sc.serverInfoLabels)
	sc.descs.Start = newPromDesc("start_time", "Server start time gauge", sc.serverLabels)
	sc.descs.Uptime = newPromDesc("uptime", "Server uptime gauge", sc.serverLabels)
	sc.descs.Mem = newPromDesc("mem_bytes", "Server memory gauge", sc.serverLabels)
	sc.descs.Cores = newPromDesc("core_count", "Machine cores gauge", sc.serverLabels)
	sc.descs.CPU = newPromDesc("cpu_percentage", "Server cpu utilization gauge", sc.serverLabels)
	sc.descs.Connections = newPromDesc("connection_count", "Current number of client connections gauge", sc.serverLabels)
	sc.descs.TotalConnections = newPromDesc("total_connection_count", "Total number of client connections serviced gauge", sc.serverLabels)
	sc.descs.ActiveAccounts = newPromDesc("active_account_count", "Number of active accounts gauge", sc.serverLabels)
	sc.descs.NumSubs = newPromDesc("subs_count", "Current number of subscriptions gauge", sc.serverLabels)
	sc.descs.SentMsgs = newPromDesc("sent_msgs_count", "Number of messages sent gauge", sc.serverLabels)
	sc.descs.SentBytes = newPromDesc("sent_bytes", "Number of messages sent gauge", sc.serverLabels)
	sc.descs.RecvMsgs = newPromDesc("recv_msgs_count", "Number of messages received gauge", sc.serverLabels)
	sc.descs.RecvBytes = newPromDesc("recv_bytes", "Number of messages received gauge", sc.serverLabels)
	sc.descs.SlowConsumers = newPromDesc("slow_consumer_count", "Number of slow consumers gauge", sc.serverLabels)
	sc.descs.RTT = newPromDesc("rtt_nanoseconds", "RTT in nanoseconds gauge", sc.serverLabels)
	sc.descs.Routes = newPromDesc("route_count", "Number of active routes gauge", sc.serverLabels)
	sc.descs.Gateways = newPromDesc("gateway_count", "Number of active gateways gauge", sc.serverLabels)

	// Routes
	sc.descs.RouteSentMsgs = newPromDesc("route_sent_msg_count", "Number of messages sent over the route gauge", sc.routeLabels)
	sc.descs.RouteSentBytes = newPromDesc("route_sent_bytes", "Number of bytes sent over the route gauge", sc.routeLabels)
	sc.descs.RouteRecvMsgs = newPromDesc("route_recv_msg_count", "Number of messages received over the route gauge", sc.routeLabels)
	sc.descs.RouteRecvBytes = newPromDesc("route_recv_bytes", "Number of bytes received over the route gauge", sc.routeLabels)
	sc.descs.RoutePending = newPromDesc("route_pending_bytes", "Number of bytes pending in the route gauge", sc.routeLabels)

	// Gateways
	sc.descs.GatewaySentMsgs = newPromDesc("gateway_sent_msgs_count", "Number of messages sent over the gateway gauge", sc.gatewayLabels)
	sc.descs.GatewaySentBytes = newPromDesc("gateway_sent_bytes", "Number of messages sent over the gateway gauge", sc.gatewayLabels)
	sc.descs.GatewayRecvMsgs = newPromDesc("gateway_recv_msg_count", "Number of messages sent over the gateway gauge", sc.gatewayLabels)
	sc.descs.GatewayRecvBytes = newPromDesc("gateway_recv_bytes", "Number of messages sent over the gateway gauge", sc.gatewayLabels)
	sc.descs.GatewayNumInbound = newPromDesc("gateway_inbound_msg_count", "Number inbound messages through the gateway gauge", sc.gatewayLabels)

	// Jetstream Info
	sc.descs.JetstreamInfo = newPromDesc("jetstream_info", " Always 1. Contains metadata for cross-reference from other time-series", sc.jsServerInfoLabels)

	// Jetstream Server
	sc.descs.JetstreamEnabled = newPromDesc("jetstream_enabled", "1 if Jetstream is enabled, 0 otherwise.  A gauge.", sc.jsServerLabels)
	sc.descs.JetstreamFilestoreSizeBytes = newPromDesc("jetstream_filestore_size_bytes", "Capacity of jetstream filesystem storage in bytes", sc.jsServerLabels)
	sc.descs.JetstreamMemstoreSizeBytes = newPromDesc("jetstream_memstore_size_bytes", "Capacity of jetstream in-memory store in bytes", sc.jsServerLabels)
	sc.descs.JetstreamFilestoreUsedBytes = newPromDesc("jetstream_filestore_used_bytes", "Consumption of jetstream filesystem storage in bytes", sc.jsServerLabels)
	sc.descs.JetstreamFilestoreReservedBytes = newPromDesc("jetstream_filestore_reserved_bytes", "Account Reservations of jetstream filesystem storage in bytes", sc.jsServerLabels)
	sc.descs.JetstreamFilestoreReservedUsedBytes = newPromDesc("jetstream_filestore_reserved_used_bytes", "Consumption of Account Reservation of jetstream filesystem storage in bytes", sc.jsServerLabels)
	sc.descs.JetstreamMemstoreUsedBytes = newPromDesc("jetstream_memstore_used_bytes", "Consumption of jetstream in-memory store in bytes", sc.jsServerLabels)
	sc.descs.JetstreamMemstoreReservedBytes = newPromDesc("jetstream_memstore_reserved_bytes", "Account Reservations of  jetstream in-memory store in bytes", sc.jsServerLabels)
	sc.descs.JetstreamMemstoreReservedUsedBytes = newPromDesc("jetstream_memstore_reserved_used_bytes", "Consumption of Account Reservation of jetstream in-memory store in bytes. ", sc.jsServerLabels)
	sc.descs.JetstreamAccounts = newPromDesc("jetstream_accounts", "Number of NATS Accounts present on a Jetstream server", sc.jsServerLabels)
	sc.descs.JetstreamHAAssets = newPromDesc("jetstream_ha_assets", "Number of HA (R>1) assets used by NATS", sc.jsServerLabels)
	sc.descs.JetstreamAPIRequests = newPromDesc("jetstream_api_requests", "Number of Jetstream API Requests processed. Value is 0 when server starts", sc.jsServerLabels)
	sc.descs.JetstreamAPIErrors = newPromDesc("jetstream_api_errors", "Number of Jetstream API Errors. Value is 0 when server starts", sc.jsServerLabels)

	// Jetstream Raft Groups
	jsRaftGroupInfoLabelKeys := []string{"jetstream_domain", "raft_group", "server_id", "server_name", "cluster_name", "leader"}
	sc.descs.JetstreamClusterRaftGroupInfo = newPromDesc("jetstream_cluster_raft_group_info", "Provides metadata about a RAFT Group", jsRaftGroupInfoLabelKeys)
	jsRaftGroupLabelKeys := []string{"server_id", "server_name", "cluster_name"}
	sc.descs.JetstreamClusterRaftGroupSize = newPromDesc("jetstream_cluster_raft_group_size", "Number of peers in a RAFT group", jsRaftGroupLabelKeys)
	sc.descs.JetstreamClusterRaftGroupLeader = newPromDesc("jetstream_cluster_raft_group_leader", "1 if this server is leader of raft group, 0 otherwise", jsRaftGroupLabelKeys)
	sc.descs.JetstreamClusterRaftGroupReplicas = newPromDesc("jetstream_cluster_raft_group_replicas", "Info about replicas from leaders perspective", jsRaftGroupLabelKeys)

	// Jetstream Cluster Replicas
	jsClusterReplicaLabelKeys := []string{"server_id", "server_name", "peer", "cluster_name"}
	// FIXME: help could use some work...
	sc.descs.JetstreamClusterRaftGroupReplicaActive = newPromDesc("jetstream_cluster_raft_group_replica_peer_active", "Jetstream RAFT Group Peer last Active time. Very large values may imply raft is stalled", jsClusterReplicaLabelKeys)
	sc.descs.JetstreamClusterRaftGroupReplicaCurrent = newPromDesc("jetstream_cluster_raft_group_replica_peer_current", "Jetstream RAFT Group Peer is current: 1 or not: 0", jsClusterReplicaLabelKeys)
	sc.descs.JetstreamClusterRaftGroupReplicaOffline = newPromDesc("jetstream_cluster_raft_group_replica_peer_offline", "Jetstream RAFT Group Peer is offline: 1 or online: 0", jsClusterReplicaLabelKeys)

	// Account scope metrics
	if sc.collectAccounts {
		accLabel := []string{"account"}
		sc.descs.accCount = newPromDesc("account_count", "The number of accounts detected", nil)

		sc.descs.accConnCount = newPromDesc("account_conn_count", "The number of client connections to this account", accLabel)
		sc.descs.accLeafCount = newPromDesc("account_leaf_count", "The number of leafnode connections to this account", accLabel)
		sc.descs.accSubCount = newPromDesc("account_sub_count", "The number of subscriptions on this account", accLabel)

		sc.descs.accBytesSent = newPromDesc("account_bytes_sent", "The number of bytes sent on this account", accLabel)
		sc.descs.accBytesRecv = newPromDesc("account_bytes_recv", "The number of bytes received on this account", accLabel)
		sc.descs.accMsgsSent = newPromDesc("account_msgs_sent", "The number of messages sent on this account", accLabel)
		sc.descs.accMsgsRecv = newPromDesc("account_msgs_recv", "The number of messages received on this account", accLabel)

		sc.descs.accJetstreamEnabled = newPromDesc("account_jetstream_enabled", "Whether JetStream is enabled or not for this account", accLabel)
		sc.descs.accJetstreamMemoryUsed = newPromDesc("account_jetstream_memory_used", "The number of bytes used by JetStream memory", accLabel)
		sc.descs.accJetstreamStorageUsed = newPromDesc("account_jetstream_storage_used", "The number of bytes used by JetStream storage", accLabel)
		sc.descs.accJetstreamMemoryReserved = newPromDesc("account_jetstream_memory_reserved", "The number of bytes reserved by JetStream memory", accLabel)
		sc.descs.accJetstreamStorageReserved = newPromDesc("account_jetstream_storage_reserved", "The number of bytes reserved by JetStream storage", accLabel)
		sc.descs.accJetstreamTieredMemoryUsed = newPromDesc("account_jetstream_tiered_memory_used", "The number of bytes used by JetStream memory tier", append(accLabel, "tier"))
		sc.descs.accJetstreamTieredStorageUsed = newPromDesc("account_jetstream_tiered_storage_used", "The number of bytes used by JetStream storage tier", append(accLabel, "tier"))
		sc.descs.accJetstreamTieredMemoryReserved = newPromDesc("account_jetstream_tiered_memory_reserved", "The number of bytes reserved by JetStream memory tier", append(accLabel, "tier"))
		sc.descs.accJetstreamTieredStorageReserved = newPromDesc("account_jetstream_tiered_storage_reserved", "The number of bytes reserved by JetStream storage tier", append(accLabel, "tier"))
		sc.descs.accJetstreamStreamCount = newPromDesc("account_jetstream_stream_count", "The number of streams in this account", accLabel)
		sc.descs.accJetstreamConsumerCount = newPromDesc("account_jetstream_consumer_count", "The number of consumers per stream for this account", append(accLabel, "stream"))
		sc.descs.accJetstreamReplicaCount = newPromDesc("account_jetstream_replica_count", "The number of replicas per stream for this account", append(accLabel, "stream"))
	}

	// Surveyor
	sc.surveyedCnt = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        prometheus.BuildFQName("nats", "survey", "surveyed_count"),
		Help:        "Number of remote hosts successfully surveyed gauge",
		ConstLabels: sc.constLabels,
	}, []string{})

	sc.expectedCnt = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        prometheus.BuildFQName("nats", "survey", "expected_count"),
		Help:        "Number of remote hosts expected to responded gauge",
		ConstLabels: sc.constLabels,
	}, []string{})

	sc.pollTime = prometheus.NewSummaryVec(prometheus.SummaryOpts{
		Name:        prometheus.BuildFQName("nats", "survey", "duration_seconds"),
		Help:        "Time it took to gather the surveyed data histogram",
		ConstLabels: sc.constLabels,
	}, []string{})

	sc.pollErrCnt = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:        prometheus.BuildFQName("nats", "survey", "poll_error_count"),
		Help:        "The number of times the poller encountered errors counter",
		ConstLabels: sc.constLabels,
	}, []string{})

	sc.lateReplies = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:        prometheus.BuildFQName("nats", "survey", "late_replies_count"),
		Help:        "Number of times a reply was received too late counter",
		ConstLabels: sc.constLabels,
	}, []string{"timeout"})

	sc.noReplies = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:        prometheus.BuildFQName("nats", "survey", "no_replies_count"),
		Help:        "Number of nodes that did not reply in poll cycle",
		ConstLabels: sc.constLabels,
	}, []string{"expected"})
}

// NewStatzCollector creates a NATS Statz Collector
func NewStatzCollector(nc *nats.Conn, logger *logrus.Logger, numServers int, serverDiscoveryWait, pollTimeout time.Duration, accounts bool, constLabels prometheus.Labels) *StatzCollector {
	sc := &StatzCollector{
		nc:                  nc,
		logger:              logger,
		numServers:          numServers,
		serverDiscoveryWait: serverDiscoveryWait,
		reply:               nc.NewRespInbox(),
		pollTimeout:         pollTimeout,
		servers:             make(map[string]bool),
		doneCh:              make(chan struct{}, 1),
		collectAccounts:     accounts,

		// TODO - normalize these if possible.  Jetstream varies from the other server labels
		serverLabels:       []string{"server_cluster", "server_name", "server_id"},
		serverInfoLabels:   []string{"server_cluster", "server_name", "server_id", "server_version"},
		routeLabels:        []string{"server_cluster", "server_name", "server_id", "server_route_id"},
		gatewayLabels:      []string{"server_cluster", "server_name", "server_id", "server_gateway_name", "server_gateway_id"},
		jsServerLabels:     []string{"server_id", "server_name", "cluster_name"},
		jsServerInfoLabels: []string{"server_name", "server_host", "server_id", "server_cluster", "server_domain", "server_version", "server_jetstream"},
		constLabels:        constLabels,
	}

	sc.buildDescs()

	sc.expectedCnt.WithLabelValues().Set(float64(numServers))

	nc.Subscribe(sc.reply+".*", sc.handleResponse)
	return sc
}

func (sc *StatzCollector) handleResponse(msg *nats.Msg) {
	m := &server.ServerStatsMsg{}
	if err := json.Unmarshal(msg.Data, m); err != nil {
		sc.logger.Warnf("Error unmarshalling statz json: %v", err)
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
		sc.logger.Infof("Late reply for server [%15s : %15s : %s]: %v", m.Server.Cluster, serverName(m), m.Server.ID, rtt)
		sc.lateReplies.WithLabelValues(fmt.Sprintf("%.1f", sc.pollTimeout.Seconds())).Inc()
	} else {
		sc.logger.Infof("Extra reply from server [%15s : %15s : %s]: %v", m.Server.Cluster, serverName(m), m.Server.ID, rtt)
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

	// Send our ping for statusz updates
	if err := sc.nc.PublishRequest("$SYS.REQ.SERVER.PING", sc.reply+"."+sc.pollkey, nil); err != nil {
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
			sc.logger.Warnf("Poll timeout after %v while waiting for responses", sc.pollTimeout)
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
			a := fmt.Sprintf("%s-%s", stats[i].Server.Cluster, serverName(stats[i]))
			b := fmt.Sprintf("%s-%s", stats[j].Server.Cluster, serverName(stats[j]))
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
			sc.logger.Debugf("Server [%15s : %15s : %15s : %s]: %v", stat.Server.Cluster, serverName(stat), stat.Server.Host, stat.Server.ID, rtts[stat.Server.ID])
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

	if sc.collectAccounts {
		return sc.pollAccountInfo()
	}
	return nil
}

func (sc *StatzCollector) pollAccountInfo() error {
	nc := sc.nc
	accs, err := sc.getAccStatz(nc)
	if err != nil {
		return err
	}

	accStats := make(map[string]accountStats, len(accs))
	for accID, acc := range accs {
		sts := accountStats{accountID: accID}

		sts.leafCount = float64(acc.LeafNodes)
		sts.subCount = float64(acc.NumSubs)
		sts.connCount = float64(acc.Conns)
		sts.bytesSent = float64(acc.Sent.Bytes)
		sts.bytesRecv = float64(acc.Received.Bytes)
		sts.msgsSent = float64(acc.Sent.Msgs)
		sts.msgsRecv = float64(acc.Received.Msgs)

		accStats[acc.Account] = sts
	}
	jsInfos := sc.getJSInfos(nc)
	for accID, jsInfo := range jsInfos {
		sts, ok := accStats[accID]
		if !ok {
			continue
		}
		sts.jetstreamEnabled = 1.0
		sts.jetstreamMemoryUsed = float64(jsInfo.Memory)
		sts.jetstreamStorageUsed = float64(jsInfo.Store)
		sts.jetstreamMemoryReserved = float64(jsInfo.ReservedMemory)
		sts.jetstreamStorageReserved = float64(jsInfo.ReservedStore)
		sts.jetstreamTieredMemoryUsed = make(map[int]float64)
		sts.jetstreamTieredStorageUsed = make(map[int]float64)
		sts.jetstreamTieredMemoryReserved = make(map[int]float64)
		sts.jetstreamTieredStorageReserved = make(map[int]float64)

		sts.jetstreamStreamCount = float64(len(jsInfo.Streams))
		for _, stream := range jsInfo.Streams {
			sts.jetstreamStreams = append(sts.jetstreamStreams, streamAccountStats{
				streamName:    stream.Name,
				consumerCount: float64(len(stream.Consumer)),
				replicaCount:  float64(stream.Config.Replicas),
			})

			// computed tiered storage usage
			used := float64(stream.State.Bytes)
			var reserved float64
			if stream.Config.MaxBytes > 0 {
				reserved = float64(stream.Config.MaxBytes)
			}
			if stream.Config.Storage == server.MemoryStorage {
				if _, ok = sts.jetstreamTieredMemoryUsed[stream.Config.Replicas]; ok {
					sts.jetstreamTieredMemoryUsed[stream.Config.Replicas] += used
					sts.jetstreamTieredMemoryReserved[stream.Config.Replicas] += reserved
				} else {
					sts.jetstreamTieredMemoryUsed[stream.Config.Replicas] = used
					sts.jetstreamTieredMemoryReserved[stream.Config.Replicas] = reserved
				}
			} else if stream.Config.Storage == server.FileStorage {
				if _, ok = sts.jetstreamTieredStorageUsed[stream.Config.Replicas]; ok {
					sts.jetstreamTieredStorageUsed[stream.Config.Replicas] += used
					sts.jetstreamTieredStorageReserved[stream.Config.Replicas] += reserved
				} else {
					sts.jetstreamTieredStorageUsed[stream.Config.Replicas] = used
					sts.jetstreamTieredStorageReserved[stream.Config.Replicas] = reserved
				}
			}
		}
		accStats[jsInfo.Id] = sts
	}

	sc.Lock()
	sc.accStats = make([]accountStats, 0, len(accStats))
	for _, acc := range accStats {
		sc.accStats = append(sc.accStats, acc)
	}
	sc.Unlock()

	return nil
}

func (sc *StatzCollector) getJSInfos(nc *nats.Conn) map[string]*server.AccountDetail {
	opts := server.JSzOptions{
		Accounts: true,
		Streams:  true,
		Consumer: true,
		Config:   true,
	}
	res := make([]*server.JSInfo, 0)
	req, err := json.Marshal(opts)
	if err != nil {
		sc.logger.Warnf("Error marshaling request: %s", err)
	}

	subj := "$SYS.REQ.SERVER.PING.JSZ"
	msgs, err := requestMany(nc, sc, subj, req)
	if err != nil {
		sc.logger.Warnf("Unable to request JetStream info: %s", err)
	}

	for _, msg := range msgs {
		var r server.ServerAPIResponse
		var d server.JSInfo
		r.Data = &d
		if err := json.Unmarshal(msg.Data, &r); err != nil {
			sc.logger.Warnf("Error deserializing JetStream info: %s", err)
			continue
		}
		if r.Error != nil {
			if strings.Contains(r.Error.Description, "jetstream not enabled") {
				// jetstream is not enabled on server
				return nil
			}
			continue
		}
		res = append(res, &d)
		if sc.numServers != -1 && len(res) == sc.numServers {
			break
		}
	}

	jsAccInfos := make(map[string]*server.AccountDetail)
	for _, jsInfo := range res {
		for _, acc := range jsInfo.AccountDetails {
			accInfo, ok := jsAccInfos[acc.Id]
			if !ok {
				jsAccInfos[acc.Id] = acc
				continue
			}
			mergeStreamDetails(accInfo, acc)
			jsAccInfos[acc.Id] = acc
		}
	}

	return jsAccInfos
}

func (sc *StatzCollector) getAccStatz(nc *nats.Conn) (map[string]*server.AccountStat, error) {
	req := &server.AccountStatzOptions{
		IncludeUnused: true,
	}
	reqJSON, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	res := make([]*server.AccountStatz, 0)
	const subj = "$SYS.REQ.ACCOUNT.PING.STATZ"

	msgs, err := requestMany(nc, sc, subj, reqJSON)
	if err != nil {
		sc.logger.Warnf("Unable to request JetStream info: %s", err)
	}

	for _, msg := range msgs {
		var r server.ServerAPIResponse
		var d server.AccountStatz
		r.Data = &d
		if err := json.Unmarshal(msg.Data, &r); err != nil {
			return nil, err
		}
		r.Data = &d
		if err := json.Unmarshal(msg.Data, &r); err != nil {
			sc.logger.Warnf("Error deserializing JetStream info: %s", err)
			continue
		}
		if r.Error != nil {
			if strings.Contains(r.Error.Description, "jetstream not enabled") {
				// jetstream is not enabled on server
				return nil, r.Error
			}
			continue
		}
		res = append(res, &d)
		if sc.numServers != -1 && len(res) == sc.numServers {
			break
		}
	}

	accStatz := make(map[string]*server.AccountStat)
	for _, statz := range res {
		for _, acc := range statz.Accounts {
			accInfo, ok := accStatz[acc.Account]
			if !ok {
				accStatz[acc.Account] = acc
				continue
			}
			mergeAccountStats(accInfo, acc)
			accStatz[acc.Account] = acc
		}
	}

	return accStatz, nil
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

func mergeAccountStats(from, to *server.AccountStat) {
	to.Conns += from.Conns
	to.LeafNodes += from.LeafNodes
	to.TotalConns += from.TotalConns
	to.NumSubs += from.NumSubs
	to.Sent.Msgs += from.Sent.Msgs
	to.Sent.Bytes += from.Sent.Bytes
	to.Received.Msgs += from.Received.Msgs
	to.Received.Bytes += from.Received.Bytes
	to.SlowConsumers += from.SlowConsumers
}

// Describe is the Prometheus interface to describe metrics for
// the prometheus system
func (sc *StatzCollector) Describe(ch chan<- *prometheus.Desc) {
	// Server Descriptions
	ch <- sc.natsUp
	ch <- sc.descs.Info
	ch <- sc.descs.Start
	ch <- sc.descs.Uptime
	ch <- sc.descs.Mem
	ch <- sc.descs.Cores
	ch <- sc.descs.CPU
	ch <- sc.descs.Connections
	ch <- sc.descs.TotalConnections
	ch <- sc.descs.ActiveAccounts
	ch <- sc.descs.NumSubs
	ch <- sc.descs.SentMsgs
	ch <- sc.descs.SentBytes
	ch <- sc.descs.RecvMsgs
	ch <- sc.descs.RecvBytes
	ch <- sc.descs.SlowConsumers
	ch <- sc.descs.Routes
	ch <- sc.descs.Gateways

	// Route Descriptions
	ch <- sc.descs.RouteSentMsgs
	ch <- sc.descs.RouteSentBytes
	ch <- sc.descs.RouteRecvMsgs
	ch <- sc.descs.RouteRecvBytes
	ch <- sc.descs.RoutePending

	// Gateway Descriptions
	ch <- sc.descs.GatewaySentMsgs
	ch <- sc.descs.GatewaySentBytes
	ch <- sc.descs.GatewayRecvMsgs
	ch <- sc.descs.GatewayRecvBytes
	ch <- sc.descs.GatewayNumInbound

	// Jetstream Descriptions
	// Jetstream Info
	ch <- sc.descs.JetstreamInfo
	// Jetstream Server
	ch <- sc.descs.JetstreamEnabled
	ch <- sc.descs.JetstreamFilestoreSizeBytes
	ch <- sc.descs.JetstreamMemstoreSizeBytes
	ch <- sc.descs.JetstreamFilestoreUsedBytes
	ch <- sc.descs.JetstreamFilestoreReservedBytes
	ch <- sc.descs.JetstreamFilestoreReservedUsedBytes
	ch <- sc.descs.JetstreamMemstoreUsedBytes
	ch <- sc.descs.JetstreamMemstoreReservedBytes
	ch <- sc.descs.JetstreamMemstoreReservedUsedBytes
	ch <- sc.descs.JetstreamAccounts
	ch <- sc.descs.JetstreamAPIRequests
	ch <- sc.descs.JetstreamAPIErrors
	// Jetstream Cluster
	ch <- sc.descs.JetstreamClusterRaftGroupInfo
	ch <- sc.descs.JetstreamClusterRaftGroupSize
	ch <- sc.descs.JetstreamClusterRaftGroupLeader
	ch <- sc.descs.JetstreamClusterRaftGroupReplicas
	// Jetstream Cluster Replicas
	ch <- sc.descs.JetstreamClusterRaftGroupReplicaActive
	ch <- sc.descs.JetstreamClusterRaftGroupReplicaCurrent
	ch <- sc.descs.JetstreamClusterRaftGroupReplicaOffline

	// Account scope metrics
	if sc.collectAccounts {
		ch <- sc.descs.accCount
		ch <- sc.descs.accConnCount
		ch <- sc.descs.accLeafCount
		ch <- sc.descs.accSubCount
		ch <- sc.descs.accBytesSent
		ch <- sc.descs.accBytesRecv
		ch <- sc.descs.accMsgsSent
		ch <- sc.descs.accMsgsRecv
		ch <- sc.descs.accJetstreamEnabled
		ch <- sc.descs.accJetstreamMemoryUsed
		ch <- sc.descs.accJetstreamStorageUsed
		ch <- sc.descs.accJetstreamMemoryReserved
		ch <- sc.descs.accJetstreamStorageReserved
		ch <- sc.descs.accJetstreamStreamCount
		ch <- sc.descs.accJetstreamConsumerCount
		ch <- sc.descs.accJetstreamReplicaCount
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
	metrics []prometheus.Metric
}

func (ms *metricSlice) appendMetric(m prometheus.Metric) {
	ms.Lock()
	defer ms.Unlock()
	ms.metrics = append(ms.metrics, m)
}

func (ms *metricSlice) newGaugeMetric(desc *prometheus.Desc, value float64, labels []string) {
	m := prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, value, labels...)

	ms.appendMetric(m)
}

func (ms *metricSlice) newCounterMetric(desc *prometheus.Desc, value float64, labels []string) {
	m := prometheus.MustNewConstMetric(desc, prometheus.CounterValue, value, labels...)

	ms.appendMetric(m)
}

// Collect gathers the streaming server serverz metrics.
func (sc *StatzCollector) Collect(ch chan<- prometheus.Metric) {
	// Run in flightgroup to allow simultaneous query
	result, err, _ := sc.flightGroup.Do("collect", func() (interface{}, error) {
		metrics := &metricSlice{
			metrics: make([]prometheus.Metric, 0),
			Mutex:   sync.Mutex{},
		}

		timer := prometheus.NewTimer(sc.pollTime.WithLabelValues())

		// poll the servers
		if err := sc.poll(); err != nil {
			sc.logger.Warnf("Error polling NATS server: %v", err)
			sc.pollErrCnt.WithLabelValues().Inc()
			metrics.newCounterMetric(sc.natsUp, 0, nil)
			return metrics.metrics, nil
		}

		// lock the stats
		sc.Lock()
		defer sc.Unlock()

		metrics.newCounterMetric(sc.natsUp, 1, nil)
		sc.surveyedCnt.WithLabelValues().Set(0)

		for _, sm := range sc.stats {
			sc.surveyedCnt.WithLabelValues().Inc()

			metrics.newGaugeMetric(sc.descs.Info, 1, sc.serverInfoLabelValues(sm))

			labels := sc.serverLabelValues(sm)
			metrics.newGaugeMetric(sc.descs.Start, float64(sm.Stats.Start.UnixNano()), labels)
			metrics.newGaugeMetric(sc.descs.Uptime, time.Since(sm.Stats.Start).Seconds(), labels)
			metrics.newGaugeMetric(sc.descs.Mem, float64(sm.Stats.Mem), labels)
			metrics.newGaugeMetric(sc.descs.Cores, float64(sm.Stats.Cores), labels)
			metrics.newGaugeMetric(sc.descs.CPU, sm.Stats.CPU, labels)
			metrics.newGaugeMetric(sc.descs.Connections, float64(sm.Stats.Connections), labels)
			metrics.newGaugeMetric(sc.descs.TotalConnections, float64(sm.Stats.TotalConnections), labels)
			metrics.newGaugeMetric(sc.descs.ActiveAccounts, float64(sm.Stats.ActiveAccounts), labels)
			metrics.newGaugeMetric(sc.descs.NumSubs, float64(sm.Stats.NumSubs), labels)
			metrics.newGaugeMetric(sc.descs.SentMsgs, float64(sm.Stats.Sent.Msgs), labels)
			metrics.newGaugeMetric(sc.descs.SentBytes, float64(sm.Stats.Sent.Bytes), labels)
			metrics.newGaugeMetric(sc.descs.RecvMsgs, float64(sm.Stats.Received.Msgs), labels)
			metrics.newGaugeMetric(sc.descs.RecvBytes, float64(sm.Stats.Received.Bytes), labels)
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
					// NIT: Technically these should be Counters, not Gauges.
					// At present, Total does not include Errors. Keeping them separate
					metrics.newGaugeMetric(sc.descs.JetstreamAPIRequests, float64(sm.Stats.JetStream.Stats.API.Total), lblServerID)
					metrics.newGaugeMetric(sc.descs.JetstreamAPIErrors, float64(sm.Stats.JetStream.Stats.API.Errors), lblServerID)
				}

				if sm.Stats.JetStream.Meta == nil {
					metrics.newGaugeMetric(sc.descs.JetstreamClusterRaftGroupInfo, float64(0), []string{"", "", sm.Server.ID, serverName(sm), "", ""})
				} else {
					jsRaftGroupInfoLabelValues := []string{jsDomainLabelValue(sm), "_meta_", sm.Server.ID, serverName(sm), sm.Stats.JetStream.Meta.Name, sm.Stats.JetStream.Meta.Leader}
					metrics.newGaugeMetric(sc.descs.JetstreamClusterRaftGroupInfo, float64(1), jsRaftGroupInfoLabelValues)

					jsRaftGroupLabelValues := []string{sm.Server.ID, serverName(sm), sm.Server.Cluster}
					// FIXME: add labels needed or remove...

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
						jsClusterReplicaLabelValues := []string{sm.Server.ID, serverName(sm), jsr.Name, sm.Server.Cluster}
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
				labels = sc.routeLabelValues(sm, rs)
				metrics.newGaugeMetric(sc.descs.RouteSentMsgs, float64(rs.Sent.Msgs), labels)
				metrics.newGaugeMetric(sc.descs.RouteSentBytes, float64(rs.Sent.Bytes), labels)
				metrics.newGaugeMetric(sc.descs.RouteRecvMsgs, float64(rs.Received.Msgs), labels)
				metrics.newGaugeMetric(sc.descs.RouteRecvBytes, float64(rs.Received.Bytes), labels)
				metrics.newGaugeMetric(sc.descs.RoutePending, float64(rs.Pending), labels)
			}

			for _, gw := range sm.Stats.Gateways {
				labels = sc.gatewayLabelValues(sm, gw)
				metrics.newGaugeMetric(sc.descs.GatewaySentMsgs, float64(gw.Sent.Msgs), labels)
				metrics.newGaugeMetric(sc.descs.GatewaySentBytes, float64(gw.Sent.Bytes), labels)
				metrics.newGaugeMetric(sc.descs.GatewayRecvMsgs, float64(gw.Received.Msgs), labels)
				metrics.newGaugeMetric(sc.descs.GatewayRecvBytes, float64(gw.Received.Bytes), labels)
				metrics.newGaugeMetric(sc.descs.GatewayNumInbound, float64(gw.NumInbound), labels)
			}
		}

		// Account scope metrics
		if sc.collectAccounts {
			metrics.newGaugeMetric(sc.descs.accCount, float64(len(sc.accStats)), nil)
			for _, stat := range sc.accStats {
				id := []string{stat.accountID}

				metrics.newGaugeMetric(sc.descs.accConnCount, stat.connCount, id)
				metrics.newGaugeMetric(sc.descs.accLeafCount, stat.leafCount, id)
				metrics.newGaugeMetric(sc.descs.accSubCount, stat.subCount, id)

				metrics.newCounterMetric(sc.descs.accBytesSent, stat.bytesSent, id)
				metrics.newCounterMetric(sc.descs.accBytesRecv, stat.bytesRecv, id)
				metrics.newCounterMetric(sc.descs.accMsgsSent, stat.msgsSent, id)
				metrics.newCounterMetric(sc.descs.accMsgsRecv, stat.msgsRecv, id)

				metrics.newGaugeMetric(sc.descs.accJetstreamEnabled, stat.jetstreamEnabled, id)
				metrics.newGaugeMetric(sc.descs.accJetstreamMemoryUsed, stat.jetstreamMemoryUsed, id)
				metrics.newGaugeMetric(sc.descs.accJetstreamStorageUsed, stat.jetstreamStorageUsed, id)
				metrics.newGaugeMetric(sc.descs.accJetstreamMemoryReserved, stat.jetstreamMemoryReserved, id)
				metrics.newGaugeMetric(sc.descs.accJetstreamStorageReserved, stat.jetstreamStorageReserved, id)
				for tier, size := range stat.jetstreamTieredMemoryUsed {
					metrics.newGaugeMetric(sc.descs.accJetstreamTieredMemoryUsed, size, append(id, fmt.Sprintf("R%d", tier)))
				}
				for tier, size := range stat.jetstreamTieredStorageUsed {
					metrics.newGaugeMetric(sc.descs.accJetstreamTieredStorageUsed, size, append(id, fmt.Sprintf("R%d", tier)))
				}
				for tier, size := range stat.jetstreamTieredMemoryReserved {
					metrics.newGaugeMetric(sc.descs.accJetstreamTieredMemoryReserved, size, append(id, fmt.Sprintf("R%d", tier)))
				}
				for tier, size := range stat.jetstreamTieredStorageReserved {
					metrics.newGaugeMetric(sc.descs.accJetstreamTieredStorageReserved, size, append(id, fmt.Sprintf("R%d", tier)))
				}

				metrics.newGaugeMetric(sc.descs.accJetstreamStreamCount, stat.jetstreamStreamCount, id)
				for _, streamStat := range stat.jetstreamStreams {
					metrics.newGaugeMetric(sc.descs.accJetstreamConsumerCount, streamStat.consumerCount, append(id, streamStat.streamName))
					metrics.newGaugeMetric(sc.descs.accJetstreamReplicaCount, streamStat.replicaCount, append(id, streamStat.streamName))
				}
			}
		}

		collectCh := make(chan prometheus.Metric)

		// We want to collect these before we exit the flight group
		// but they should still be sent to every caller
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			for m := range collectCh {
				metrics.appendMetric(m)
			}

			wg.Done()
		}()

		timer.ObserveDuration()
		sc.pollTime.Collect(collectCh)
		sc.pollErrCnt.Collect(collectCh)
		sc.surveyedCnt.Collect(collectCh)
		sc.expectedCnt.Collect(collectCh)
		sc.lateReplies.Collect(collectCh)
		sc.noReplies.Collect(collectCh)

		close(collectCh)

		wg.Wait()

		return metrics.metrics, nil
	})
	if err != nil {
		sc.logger.Error(err)
		return
	}

	if m, ok := result.([]prometheus.Metric); !ok || m == nil {
		if m == nil {
			sc.logger.Error("no metrics collected")
		} else {
			sc.logger.Error("unexpected collect response type")
		}
		return
	}

	for _, m := range result.([]prometheus.Metric) {
		ch <- m
	}

}

func requestMany(nc *nats.Conn, sc *StatzCollector, subject string, data []byte) ([]*nats.Msg, error) {
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

	if err := nc.PublishRequest(subject, inbox, data); err != nil {
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
