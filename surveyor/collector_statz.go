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
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus"
)

// statzDescs holds the metric descriptions
type statzDescs struct {
	Info             *prometheus.Desc
	Start            *prometheus.Desc
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
	accCount                    *prometheus.Desc
	accConnCount                *prometheus.Desc
	accLeafCount                *prometheus.Desc
	accSubCount                 *prometheus.Desc
	accBytesSent                *prometheus.Desc
	accBytesRecv                *prometheus.Desc
	accMsgsSent                 *prometheus.Desc
	accMsgsRecv                 *prometheus.Desc
	accJetstreamEnabled         *prometheus.Desc
	accJetstreamMemoryUsed      *prometheus.Desc
	accJetstreamStorageUsed     *prometheus.Desc
	accJetstreamMemoryReserved  *prometheus.Desc
	accJetstreamStorageReserved *prometheus.Desc
	accJetstreamStreamCount     *prometheus.Desc
	accJetstreamConsumerCount   *prometheus.Desc
	accJetstreamReplicaCount    *prometheus.Desc
}

// StatzCollector collects statz from a server deployment
type StatzCollector struct {
	sync.Mutex
	nc              *nats.Conn
	start           time.Time
	stats           []*server.ServerStatsMsg
	accStats        []accountStats
	rtts            map[string]time.Duration
	pollTimeout     time.Duration
	reply           string
	polling         bool
	pollkey         string
	numServers      int
	more            int
	servers         map[string]bool
	doneCh          chan struct{}
	moreCh          chan struct{}
	descs           statzDescs
	collectAccounts bool
	natsUp          *prometheus.Desc

	serverLabels     []string
	serverInfoLabels []string
	routeLabels      []string
	gatewayLabels    []string

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

	jetstreamEnabled         float64
	jetstreamMemoryUsed      float64
	jetstreamStorageUsed     float64
	jetstreamMemoryReserved  float64
	jetstreamStorageReserved float64
	jetstreamStreamCount     float64
	jetstreamStreams         []streamAccountStats
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
		return "*EMPTY*"
	}
	return sm.Server.Domain
}

func jetstreamInfoLabelValues(sm *server.ServerStatsMsg) []string {
	// Maybe also "meta_leader", "store_dir"?
	return []string{sm.Server.Name, sm.Server.Host, sm.Server.ID, sm.Server.Cluster, jsDomainLabelValue(sm), sm.Server.Version,
		strconv.FormatBool(sm.Server.JetStream)}
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
			prometheus.BuildFQName("nats", "core", name), help, labels, nil)
	}

	// A unlabelled description for the up/down
	sc.natsUp = prometheus.NewDesc(prometheus.BuildFQName("nats", "core", "nats_up"),
		"1 if connected to NATS, 0 otherwise.  A gauge.", nil, nil)

	sc.descs.Info = newPromDesc("info", "General Server information Summary gauge", sc.serverInfoLabels)
	sc.descs.Start = newPromDesc("start_time", "Server start time gauge", sc.serverLabels)
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
	sc.descs.JetstreamInfo = newPromDesc("jetstream_info", " Always 1. Contains metadata for cross-reference from other time-series",
		[]string{"server_name", "server_host", "server_id", "server_cluster", "server_domain", "server_version", "server_jetstream"})

	// Jetstream Server
	// Maybe this should be server_id since we can lookup server_name from info?
	jsServerLabelKeys := []string{"server_id"}
	sc.descs.JetstreamEnabled = newPromDesc("jetstream_enabled", "1 if Jetstream is enabled, 0 otherwise.  A gauge.", jsServerLabelKeys)
	sc.descs.JetstreamFilestoreSizeBytes = newPromDesc("jetstream_filestore_size_bytes", "Capacity of jetstream filesystem storage in bytes", jsServerLabelKeys)
	sc.descs.JetstreamMemstoreSizeBytes = newPromDesc("jetstream_memstore_size_bytes", "Capacity of jetstream in-memory store in bytes", jsServerLabelKeys)
	sc.descs.JetstreamFilestoreUsedBytes = newPromDesc("jetstream_filestore_used_bytes", "Consumption of jetstream filesystem storage in bytes", jsServerLabelKeys)
	sc.descs.JetstreamFilestoreReservedBytes = newPromDesc("jetstream_filestore_reserved_bytes", "Account Reservations of jetstream filesystem storage in bytes", jsServerLabelKeys)
	sc.descs.JetstreamFilestoreReservedUsedBytes = newPromDesc("jetstream_filestore_reserved_used_bytes", "Consumption of Account Reservation of jetstream filesystem storage in bytes", jsServerLabelKeys)
	sc.descs.JetstreamMemstoreUsedBytes = newPromDesc("jetstream_memstore_used_bytes", "Consumption of jetstream in-memory store in bytes", jsServerLabelKeys)
	sc.descs.JetstreamMemstoreReservedBytes = newPromDesc("jetstream_memstore_reserved_bytes", "Account Reservations of  jetstream in-memory store in bytes", jsServerLabelKeys)
	sc.descs.JetstreamMemstoreReservedUsedBytes = newPromDesc("jetstream_memstore_reserved_used_bytes", "Consumption of Account Reservation of jetstream in-memory store in bytes. ", jsServerLabelKeys)
	sc.descs.JetstreamAccounts = newPromDesc("jetstream_accounts", "Number of NATS Accounts present on a Jetstream server", jsServerLabelKeys)
	sc.descs.JetstreamHAAssets = newPromDesc("jetstream_ha_assets", "Number of Raft nodes used by NATS", jsServerLabelKeys)
	sc.descs.JetstreamAPIRequests = newPromDesc("jetstream_api_requests", "Number of Jetstream API Requests processed. Value is 0 when server starts", jsServerLabelKeys)
	sc.descs.JetstreamAPIErrors = newPromDesc("jetstream_api_errors", "Number of Jetstream API Errors. Value is 0 when server starts", jsServerLabelKeys)

	// Jetstream Raft Groups
	jsRaftGroupInfoLabelKeys := []string{"jetstream_domain", "raft_group", "server_id", "cluster_name", "leader"}
	sc.descs.JetstreamClusterRaftGroupInfo = newPromDesc("jetstream_cluster_raft_group_info", "Provides metadata about a RAFT Group", jsRaftGroupInfoLabelKeys)
	jsRaftGroupLabelKeys := []string{"server_id"}
	sc.descs.JetstreamClusterRaftGroupSize = newPromDesc("jetstream_cluster_raft_group_size", "Number of peers in a RAFT group", jsRaftGroupLabelKeys)
	sc.descs.JetstreamClusterRaftGroupLeader = newPromDesc("jetstream_cluster_raft_group_leader", "1 if this server is leader of raft group, 0 otherwise", jsRaftGroupLabelKeys)
	sc.descs.JetstreamClusterRaftGroupReplicas = newPromDesc("jetstream_cluster_raft_group_replicas", "Info about replicas from leaders perspective", jsRaftGroupLabelKeys)

	// Jetstream Cluster Replicas
	jsClusterReplicaLabelKeys := []string{"server_id", "peer"}
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
		sc.descs.accJetstreamStreamCount = newPromDesc("account_jetstream_stream_count", "The number of streams in this account", accLabel)
		sc.descs.accJetstreamConsumerCount = newPromDesc("account_jetstream_consumer_count", "The number of consumers per stream for this account", append(accLabel, "stream"))
		sc.descs.accJetstreamReplicaCount = newPromDesc("account_jetstream_replica_count", "The number of replicas per stream for this account", append(accLabel, "stream"))
	}

	// Surveyor
	sc.surveyedCnt = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: prometheus.BuildFQName("nats", "survey", "surveyed_count"),
		Help: "Number of remote hosts successfully surveyed gauge",
	}, []string{})

	sc.expectedCnt = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: prometheus.BuildFQName("nats", "survey", "expected_count"),
		Help: "Number of remote hosts expected to responded gauge",
	}, []string{})

	sc.pollTime = prometheus.NewSummaryVec(prometheus.SummaryOpts{
		Name: prometheus.BuildFQName("nats", "survey", "duration_seconds"),
		Help: "Time it took to gather the surveyed data histogram",
	}, []string{})

	sc.pollErrCnt = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName("nats", "survey", "poll_error_count"),
		Help: "The number of times the poller encountered errors counter",
	}, []string{})

	sc.lateReplies = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName("nats", "survey", "late_replies_count"),
		Help: "Number of times a reply was received too late counter",
	}, []string{"timeout"})

	sc.noReplies = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName("nats", "survey", "no_replies_count"),
		Help: "Number of nodes that did not reply in poll cycle",
	}, []string{"expected"})
}

// NewStatzCollector creates a NATS Statz Collector
func NewStatzCollector(nc *nats.Conn, numServers int, pollTimeout time.Duration, accounts bool) *StatzCollector {
	sc := &StatzCollector{
		nc:              nc,
		numServers:      numServers,
		reply:           nc.NewRespInbox(),
		pollTimeout:     pollTimeout,
		servers:         make(map[string]bool, numServers),
		doneCh:          make(chan struct{}, 1),
		moreCh:          make(chan struct{}, 1),
		collectAccounts: accounts,

		serverLabels:     []string{"server_cluster", "server_name", "server_id"},
		serverInfoLabels: []string{"server_cluster", "server_name", "server_id", "server_version"},
		routeLabels:      []string{"server_cluster", "server_name", "server_id", "server_route_id"},
		gatewayLabels:    []string{"server_cluster", "server_name", "server_id", "server_gateway_name", "server_gateway_id"},
	}

	sc.buildDescs()

	sc.expectedCnt.WithLabelValues().Set(float64(numServers))

	nc.Subscribe(sc.reply+".*", sc.handleResponse)
	return sc
}

// Polling determines if the collector is in a polling cycle
func (sc *StatzCollector) Polling() bool {
	sc.Lock()
	defer sc.Unlock()

	return sc.polling
}

func (sc *StatzCollector) handleResponse(msg *nats.Msg) {
	m := &server.ServerStatsMsg{}
	if err := json.Unmarshal(msg.Data, m); err != nil {
		log.Printf("Error unmarshalling statz json: %v", err)
	}

	sc.Lock()
	isCurrent := strings.HasSuffix(msg.Subject, sc.pollkey)
	rtt := time.Since(sc.start)
	if sc.polling && isCurrent { //nolint
		sc.stats = append(sc.stats, m)
		sc.rtts[m.Server.ID] = rtt
		if len(sc.stats) == sc.numServers {
			sc.polling = false
			sc.doneCh <- struct{}{}
		}
	} else if !isCurrent || len(sc.stats) < sc.numServers {
		log.Printf("Late reply for server [%15s : %15s : %s]: %v", m.Server.Cluster, serverName(m), m.Server.ID, rtt)
		sc.lateReplies.WithLabelValues(fmt.Sprintf("%.1f", sc.pollTimeout.Seconds())).Inc()
	} else {
		log.Printf("Extra reply from server [%15s : %15s : %s]: %v", m.Server.Cluster, serverName(m), m.Server.ID, rtt)
		sc.more++
		if sc.more == 1 {
			sc.moreCh <- struct{}{}
		}
	}
	sc.Unlock()
}

// poll will only fail if there is a NATS publishing error
func (sc *StatzCollector) poll() error {
	sc.Lock()
	sc.start = time.Now()
	sc.polling = true
	sc.pollkey = strconv.Itoa(int(sc.start.UnixNano()))
	sc.stats = nil
	sc.rtts = make(map[string]time.Duration, sc.numServers)
	sc.more = 0

	// drain possible notification from previous poll
	select {
	case <-sc.moreCh:
	default:
	}
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
	select {
	case <-sc.doneCh:
	case <-time.After(sc.pollTimeout):
		log.Printf("Poll timeout after %v while waiting for %d responses\n", sc.pollTimeout, sc.numServers)
	}

	sc.Lock()
	sc.polling = false
	ns := len(sc.stats)
	stats := append([]*server.ServerStatsMsg(nil), sc.stats...)
	rtts := sc.rtts
	sc.Unlock()

	// If we do not see expected number of servers complain.
	if ns != sc.numServers {
		sort.Slice(stats, func(i, j int) bool {
			a := fmt.Sprintf("%s-%s", stats[i].Server.Cluster, serverName(stats[i]))
			b := fmt.Sprintf("%s-%s", stats[j].Server.Cluster, serverName(stats[j]))
			return a < b
		})

		// Reset the state of what server has been seen
		for key := range sc.servers {
			sc.servers[key] = false
		}

		log.Println("RTTs for responding servers:")
		for _, stat := range stats {
			// We use for key the cluster name followed by ID which is unique per server
			key := fmt.Sprintf("%s:%s", stat.Server.Cluster, stat.Server.ID)
			// Mark this server has been seen
			sc.servers[key] = true
			log.Printf("Server [%15s : %15s : %15s : %s]: %v\n", stat.Server.Cluster, serverName(stat), stat.Server.Host, stat.Server.ID, rtts[stat.Server.ID])
		}

		log.Println("Missing servers:")
		var missingServers []string
		for key, seen := range sc.servers {
			if !seen {
				log.Println(key)
				missingServers = append(missingServers, "["+key+"]")
			}
		}

		sc.noReplies.WithLabelValues(strconv.Itoa(sc.numServers)).Add(float64(len(missingServers)))

		log.Printf("Expected %d servers, only saw responses from %d. Missing %v", sc.numServers, ns, missingServers)
	}

	if ns == sc.numServers {
		// Build map of what is our expected set...
		sc.servers = make(map[string]bool, sc.numServers)
		for _, stat := range stats {
			key := fmt.Sprintf("%s:%s", stat.Server.Cluster, stat.Server.Host)
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
	accs, err := getAccounts(nc)
	if err != nil {
		return err
	}

	accStats := make([]accountStats, 0, len(accs))
	for _, acc := range accs {
		sts := accountStats{accountID: acc}

		accInfo, err := getAccountInfo(nc, acc)
		if err != nil {
			return err
		}

		if accInfo.JetStream {
			sts.jetstreamEnabled = 1.0

			jsInfo, err := getJSInfo(nc, acc)
			if err != nil {
				return err
			}

			sts.jetstreamMemoryUsed = float64(jsInfo.Memory)
			sts.jetstreamStorageUsed = float64(jsInfo.Store)
			sts.jetstreamMemoryReserved = float64(jsInfo.ReservedStore)
			sts.jetstreamStorageReserved = float64(jsInfo.ReservedMemory)

			sts.jetstreamStreamCount = float64(len(jsInfo.Streams))
			for _, stream := range jsInfo.Streams {
				sts.jetstreamStreams = append(sts.jetstreamStreams, streamAccountStats{
					streamName:    stream.Name,
					consumerCount: float64(len(stream.Consumer)),
					replicaCount:  float64(stream.Config.Replicas),
				})
			}
		}

		sts.connCount = float64(accInfo.ClientCnt)
		sts.leafCount = float64(accInfo.LeafCnt)
		sts.subCount = float64(accInfo.SubCnt)

		agg, err := getConnzAggregate(nc, acc)
		if err != nil {
			return err
		}
		sts.bytesSent = agg.bytesSent
		sts.bytesRecv = agg.bytesRecv
		sts.msgsSent = agg.msgsSent
		sts.msgsRecv = agg.msgsRecv

		accStats = append(accStats, sts)
	}

	sc.Lock()
	sc.accStats = accStats
	sc.Unlock()

	return nil
}

func getAccounts(nc *nats.Conn) ([]string, error) {
	const subj = "$SYS.REQ.SERVER.PING.ACCOUNTZ"
	msg, err := nc.Request(subj, nil, 3*time.Second)
	if err != nil {
		return nil, err
	}

	var r server.ServerAPIResponse
	var d server.Accountz
	r.Data = &d
	if err := json.Unmarshal(msg.Data, &r); err != nil {
		return nil, err
	}

	sort.Strings(d.Accounts)
	return d.Accounts, nil
}
func getAccountInfo(nc *nats.Conn, account string) (server.AccountInfo, error) {
	subj := fmt.Sprintf("$SYS.REQ.ACCOUNT.%s.INFO", account)
	msg, err := nc.Request(subj, nil, 3*time.Second)
	if err != nil {
		return server.AccountInfo{}, err
	}

	var r server.ServerAPIResponse
	var d server.AccountInfo
	r.Data = &d
	if err := json.Unmarshal(msg.Data, &r); err != nil {
		return server.AccountInfo{}, err
	}

	return d, nil
}

func getJSInfo(nc *nats.Conn, account string) (server.AccountDetail, error) {
	subj := fmt.Sprintf("$SYS.REQ.ACCOUNT.%s.JSZ", account)
	opts := []byte(`{"streams": true, "consumer": true, "config": true}`)
	msg, err := nc.Request(subj, opts, 3*time.Second)
	if err != nil {
		return server.AccountDetail{}, err
	}

	var r server.ServerAPIResponse
	var d server.AccountDetail
	r.Data = &d
	if err := json.Unmarshal(msg.Data, &r); err != nil {
		return server.AccountDetail{}, err
	}

	return d, nil
}

type connzAggregate struct {
	bytesSent float64
	bytesRecv float64
	msgsSent  float64
	msgsRecv  float64
}

func getConnzAggregate(nc *nats.Conn, account string) (connzAggregate, error) {
	// TODO: Replace with "$SYS.REQ.ACCOUNT.%s.CONNS" after NATS 2.8.4.
	// CONNS returns bytes sent/recv at the account level without needing the
	// following code.
	subj := fmt.Sprintf("$SYS.REQ.ACCOUNT.%s.CONNZ", account)
	msg, err := nc.Request(subj, nil, 3*time.Second)
	if err != nil {
		return connzAggregate{}, err
	}

	var r server.ServerAPIResponse
	var d server.Connz
	r.Data = &d
	if err := json.Unmarshal(msg.Data, &r); err != nil {
		return connzAggregate{}, err
	}

	var agg connzAggregate
	for _, c := range d.Conns {
		agg.bytesSent += float64(c.InBytes)
		agg.bytesRecv += float64(c.OutBytes)

		agg.msgsSent += float64(c.InMsgs)
		agg.msgsRecv += float64(c.OutMsgs)
	}

	return agg, nil
}

// Describe is the Prometheus interface to describe metrics for
// the prometheus system
func (sc *StatzCollector) Describe(ch chan<- *prometheus.Desc) {
	// Server Descriptions
	ch <- sc.natsUp
	ch <- sc.descs.Info
	ch <- sc.descs.Start
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

func newGaugeMetric(desc *prometheus.Desc, value float64, labels []string) prometheus.Metric {
	return prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, value, labels...)
}

func newCounterMetric(desc *prometheus.Desc, value float64, labels []string) prometheus.Metric {
	return prometheus.MustNewConstMetric(desc, prometheus.CounterValue, value, labels...)
}

func (sc *StatzCollector) newNatsUpGaugeMetric(value bool) prometheus.Metric {
	var fval float64
	if value {
		fval = 1
	}
	return prometheus.MustNewConstMetric(sc.natsUp, prometheus.GaugeValue, fval)
}

// Collect gathers the streaming server serverz metrics.
func (sc *StatzCollector) Collect(ch chan<- prometheus.Metric) {
	timer := prometheus.NewTimer(sc.pollTime.WithLabelValues())
	defer func() {
		timer.ObserveDuration()
		sc.pollTime.Collect(ch)
		sc.pollErrCnt.Collect(ch)
		sc.surveyedCnt.Collect(ch)
		sc.expectedCnt.Collect(ch)
		sc.lateReplies.Collect(ch)
		sc.noReplies.Collect(ch)
	}()

	// poll the servers
	if err := sc.poll(); err != nil {
		log.Printf("Error polling NATS server: %v", err)
		sc.pollErrCnt.WithLabelValues().Inc()
		ch <- sc.newNatsUpGaugeMetric(false)
		return
	}

	// lock the stats
	sc.Lock()
	defer sc.Unlock()

	ch <- sc.newNatsUpGaugeMetric(true)
	sc.surveyedCnt.WithLabelValues().Set(0)

	for _, sm := range sc.stats {
		sc.surveyedCnt.WithLabelValues().Inc()

		ch <- newGaugeMetric(sc.descs.Info, 1, sc.serverInfoLabelValues(sm))

		labels := sc.serverLabelValues(sm)
		ch <- newGaugeMetric(sc.descs.Start, float64(sm.Stats.Start.UnixNano()), labels)
		ch <- newGaugeMetric(sc.descs.Mem, float64(sm.Stats.Mem), labels)
		ch <- newGaugeMetric(sc.descs.Cores, float64(sm.Stats.Cores), labels)
		ch <- newGaugeMetric(sc.descs.CPU, sm.Stats.CPU, labels)
		ch <- newGaugeMetric(sc.descs.Connections, float64(sm.Stats.Connections), labels)
		ch <- newGaugeMetric(sc.descs.TotalConnections, float64(sm.Stats.TotalConnections), labels)
		ch <- newGaugeMetric(sc.descs.ActiveAccounts, float64(sm.Stats.ActiveAccounts), labels)
		ch <- newGaugeMetric(sc.descs.NumSubs, float64(sm.Stats.NumSubs), labels)
		ch <- newGaugeMetric(sc.descs.SentMsgs, float64(sm.Stats.Sent.Msgs), labels)
		ch <- newGaugeMetric(sc.descs.SentBytes, float64(sm.Stats.Sent.Bytes), labels)
		ch <- newGaugeMetric(sc.descs.RecvMsgs, float64(sm.Stats.Received.Msgs), labels)
		ch <- newGaugeMetric(sc.descs.RecvBytes, float64(sm.Stats.Received.Bytes), labels)
		ch <- newGaugeMetric(sc.descs.SlowConsumers, float64(sm.Stats.SlowConsumers), labels)
		ch <- newGaugeMetric(sc.descs.RTT, float64(sc.rtts[sm.Server.ID]), labels)
		ch <- newGaugeMetric(sc.descs.Routes, float64(len(sm.Stats.Routes)), labels)
		ch <- newGaugeMetric(sc.descs.Gateways, float64(len(sm.Stats.Gateways)), labels)

		ch <- newGaugeMetric(sc.descs.JetstreamInfo, float64(1), jetstreamInfoLabelValues(sm))
		// Any / All Meta-data in sc.descs.JetstreamInfo can be xrefed by the server_id.
		// labels define the "uniqueness" of a time series, any associations beyond that should be left to prometheus
		lblServerID := []string{sm.Server.ID}
		if sm.Stats.JetStream == nil {
			ch <- newGaugeMetric(sc.descs.JetstreamEnabled, float64(0), lblServerID)
		} else {
			ch <- newGaugeMetric(sc.descs.JetstreamEnabled, float64(1), lblServerID)
			if sm.Stats.JetStream.Config != nil {
				ch <- newGaugeMetric(sc.descs.JetstreamFilestoreSizeBytes, float64(sm.Stats.JetStream.Config.MaxStore), lblServerID)
				ch <- newGaugeMetric(sc.descs.JetstreamMemstoreSizeBytes, float64(sm.Stats.JetStream.Config.MaxMemory), lblServerID)
				// StoreDir  At present, '$SYS.REQ.SERVER.PING', server.sendStatsz() squashes StoreDir to "".
				// Domain is also at 'sm.Server.Domain'. Unknown if there's a semantic difference at present. See jsDomainLabelValue().
			}
			if sm.Stats.JetStream.Stats != nil {
				ch <- newGaugeMetric(sc.descs.JetstreamFilestoreUsedBytes, float64(sm.Stats.JetStream.Stats.Store), lblServerID)
				ch <- newGaugeMetric(sc.descs.JetstreamFilestoreReservedBytes, float64(sm.Stats.JetStream.Stats.ReservedStore), lblServerID)
				ch <- newGaugeMetric(sc.descs.JetstreamMemstoreUsedBytes, float64(sm.Stats.JetStream.Stats.Memory), lblServerID)
				ch <- newGaugeMetric(sc.descs.JetstreamMemstoreReservedBytes, float64(sm.Stats.JetStream.Stats.ReservedMemory), lblServerID)
				ch <- newGaugeMetric(sc.descs.JetstreamAccounts, float64(sm.Stats.JetStream.Stats.Accounts), lblServerID)
				ch <- newGaugeMetric(sc.descs.JetstreamHAAssets, float64(sm.Stats.JetStream.Stats.HAAssets), lblServerID)
				// NIT: Technically these should be Counters, not Gauges.
				// At present, Total does not include Errors. Keeping them separate
				ch <- newGaugeMetric(sc.descs.JetstreamAPIRequests, float64(sm.Stats.JetStream.Stats.API.Total), lblServerID)
				ch <- newGaugeMetric(sc.descs.JetstreamAPIErrors, float64(sm.Stats.JetStream.Stats.API.Errors), lblServerID)
			}

			if sm.Stats.JetStream.Meta == nil {
				ch <- newGaugeMetric(sc.descs.JetstreamClusterRaftGroupInfo, float64(0), []string{"", "", sm.Server.ID, "", ""})
			} else {
				jsRaftGroupInfoLabelValues := []string{jsDomainLabelValue(sm), "_meta_", sm.Server.ID, sm.Stats.JetStream.Meta.Name, sm.Stats.JetStream.Meta.Leader}
				ch <- newGaugeMetric(sc.descs.JetstreamClusterRaftGroupInfo, float64(1), jsRaftGroupInfoLabelValues)

				jsRaftGroupLabelValues := []string{sm.Server.ID}
				// FIXME: add labels needed or remove...

				ch <- newGaugeMetric(sc.descs.JetstreamClusterRaftGroupSize, float64(sm.Stats.JetStream.Meta.Size), jsRaftGroupLabelValues)

				// Could provide false positive if two server have the same server_name in the same or different clusters in the super-cluster...
				// At present, in this statsz only a peer that thinks it's a Leader will have `sm.Stats.JetStream.Meta.Replicas != nil`.
				if sm.Stats.JetStream.Meta.Leader != "" && sm.Server.Name != "" && sm.Server.Name == sm.Stats.JetStream.Meta.Leader {
					ch <- newGaugeMetric(sc.descs.JetstreamClusterRaftGroupLeader, float64(1), jsRaftGroupLabelValues)
				} else {
					ch <- newGaugeMetric(sc.descs.JetstreamClusterRaftGroupLeader, float64(0), jsRaftGroupLabelValues)
				}
				ch <- newGaugeMetric(sc.descs.JetstreamClusterRaftGroupReplicas, float64(len(sm.Stats.JetStream.Meta.Replicas)), jsRaftGroupLabelValues)
				for _, jsr := range sm.Stats.JetStream.Meta.Replicas {
					if jsr == nil {
						continue
					}
					jsClusterReplicaLabelValues := []string{sm.Server.ID, jsr.Name}
					ch <- newGaugeMetric(sc.descs.JetstreamClusterRaftGroupReplicaActive, float64(jsr.Active), jsClusterReplicaLabelValues)
					if jsr.Current {
						ch <- newGaugeMetric(sc.descs.JetstreamClusterRaftGroupReplicaCurrent, float64(1), jsClusterReplicaLabelValues)
					} else {
						ch <- newGaugeMetric(sc.descs.JetstreamClusterRaftGroupReplicaCurrent, float64(0), jsClusterReplicaLabelValues)
					}
					if jsr.Offline {
						ch <- newGaugeMetric(sc.descs.JetstreamClusterRaftGroupReplicaOffline, float64(1), jsClusterReplicaLabelValues)
					} else {
						ch <- newGaugeMetric(sc.descs.JetstreamClusterRaftGroupReplicaOffline, float64(0), jsClusterReplicaLabelValues)
					}
				}
			}
		}
		for _, rs := range sm.Stats.Routes {
			labels = sc.routeLabelValues(sm, rs)
			ch <- newGaugeMetric(sc.descs.RouteSentMsgs, float64(rs.Sent.Msgs), labels)
			ch <- newGaugeMetric(sc.descs.RouteSentBytes, float64(rs.Sent.Bytes), labels)
			ch <- newGaugeMetric(sc.descs.RouteRecvMsgs, float64(rs.Received.Msgs), labels)
			ch <- newGaugeMetric(sc.descs.RouteRecvBytes, float64(rs.Received.Bytes), labels)
			ch <- newGaugeMetric(sc.descs.RoutePending, float64(rs.Pending), labels)
		}

		for _, gw := range sm.Stats.Gateways {
			labels = sc.gatewayLabelValues(sm, gw)
			ch <- newGaugeMetric(sc.descs.GatewaySentMsgs, float64(gw.Sent.Msgs), labels)
			ch <- newGaugeMetric(sc.descs.GatewaySentBytes, float64(gw.Sent.Bytes), labels)
			ch <- newGaugeMetric(sc.descs.GatewayRecvMsgs, float64(gw.Received.Msgs), labels)
			ch <- newGaugeMetric(sc.descs.GatewayRecvBytes, float64(gw.Received.Bytes), labels)
			ch <- newGaugeMetric(sc.descs.GatewayNumInbound, float64(gw.NumInbound), labels)
		}
	}

	// Account scope metrics
	if sc.collectAccounts {
		ch <- newGaugeMetric(sc.descs.accCount, float64(len(sc.accStats)), nil)
		for _, stat := range sc.accStats {
			id := []string{stat.accountID}

			ch <- newGaugeMetric(sc.descs.accConnCount, stat.connCount, id)
			ch <- newGaugeMetric(sc.descs.accLeafCount, stat.leafCount, id)
			ch <- newGaugeMetric(sc.descs.accSubCount, stat.subCount, id)

			ch <- newCounterMetric(sc.descs.accBytesSent, stat.bytesSent, id)
			ch <- newCounterMetric(sc.descs.accBytesRecv, stat.bytesRecv, id)
			ch <- newCounterMetric(sc.descs.accMsgsSent, stat.msgsSent, id)
			ch <- newCounterMetric(sc.descs.accMsgsRecv, stat.msgsRecv, id)

			ch <- newGaugeMetric(sc.descs.accJetstreamEnabled, stat.jetstreamEnabled, id)
			ch <- newGaugeMetric(sc.descs.accJetstreamMemoryUsed, stat.jetstreamMemoryUsed, id)
			ch <- newGaugeMetric(sc.descs.accJetstreamStorageUsed, stat.jetstreamStorageUsed, id)
			ch <- newGaugeMetric(sc.descs.accJetstreamMemoryReserved, stat.jetstreamMemoryReserved, id)
			ch <- newGaugeMetric(sc.descs.accJetstreamStorageReserved, stat.jetstreamStorageReserved, id)

			ch <- newGaugeMetric(sc.descs.accJetstreamStreamCount, stat.jetstreamStreamCount, id)
			for _, streamStat := range stat.jetstreamStreams {
				ch <- newGaugeMetric(sc.descs.accJetstreamConsumerCount, streamStat.consumerCount, append(id, streamStat.streamName))
				ch <- newGaugeMetric(sc.descs.accJetstreamReplicaCount, streamStat.replicaCount, append(id, streamStat.streamName))
			}
		}
	}
}
