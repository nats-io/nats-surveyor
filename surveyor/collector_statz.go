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

// Package surveyor is used to garner data from a NATS deployment for Prometheus
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

	server "github.com/nats-io/nats-server/v2/server"
	nats "github.com/nats-io/nats.go"
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
}

// StatzCollector collects statz from a server deployment
type StatzCollector struct {
	sync.Mutex
	nc          *nats.Conn
	start       time.Time
	stats       []*server.ServerStatsMsg
	rtts        map[string]time.Duration
	pollTimeout time.Duration
	reply       string
	polling     bool
	pollkey     string
	numServers  int
	more        int
	servers     map[string]bool
	doneCh      chan struct{}
	moreCh      chan struct{}
	descs       statzDescs
	natsUp      *prometheus.Desc

	surveyedCnt *prometheus.GaugeVec
	expectedCnt *prometheus.GaugeVec
	pollErrCnt  *prometheus.CounterVec
	pollTime    *prometheus.SummaryVec
	lateReplies *prometheus.CounterVec
}

////////////////////////////////////////////
// LABELS
////////////////////////////////////////////

var (
	serverLabels     = []string{"server_cluster", "server_name", "server_id"}
	serverInfoLabels = []string{"server_cluster", "server_name", "server_id", "server_version"}
	routeLabels      = []string{"server_cluster", "server_name", "server_id", "server_route_id"}
	gatewayLabels    = []string{"server_cluster", "server_name", "server_id", "server_gateway_name", "server_gateway_id"}
)

func serverName(sm *server.ServerStatsMsg) string {
	if sm.Server.Name == "" {
		return sm.Server.ID
	}

	return sm.Server.Name
}

func serverLabelValues(sm *server.ServerStatsMsg) []string {
	return []string{sm.Server.Cluster, serverName(sm), sm.Server.ID}
}

func serverInfoLabelValues(sm *server.ServerStatsMsg) []string {
	return []string{sm.Server.Cluster, serverName(sm), sm.Server.ID, sm.Server.Version}
}

func routeLabelValues(sm *server.ServerStatsMsg, rStat *server.RouteStat) []string {
	return []string{sm.Server.Cluster, serverName(sm), sm.Server.ID, strconv.FormatUint(rStat.ID, 10)}
}

func gatewayLabelValues(sm *server.ServerStatsMsg, gStat *server.GatewayStat) []string {
	return []string{sm.Server.Cluster, serverName(sm), sm.Server.ID, gStat.Name, strconv.FormatUint(gStat.ID, 10)}
}

// Up/Down on servers - look at discovery mechanisms in Prometheus - aging out, how does it work?
func buildDescs(sc *StatzCollector) {

	newPromDesc := func(name, help string, labels []string) *prometheus.Desc {
		return prometheus.NewDesc(
			prometheus.BuildFQName("nats", "core", name), help, labels, nil)
	}

	// A unlabelled description for the up/down
	sc.natsUp = prometheus.NewDesc(prometheus.BuildFQName("nats", "core", "nats_up"),
		"1 if connected to NATS, 0 otherwise.  A gauge.", nil, nil)

	sc.descs.Info = newPromDesc("info", "General Server information Summary gauge", serverInfoLabels)
	sc.descs.Start = newPromDesc("start_time", "Server start time gauge", serverLabels)
	sc.descs.Mem = newPromDesc("mem_bytes", "Server memory gauge", serverLabels)
	sc.descs.Cores = newPromDesc("core_count", "Machine cores gauge", serverLabels)
	sc.descs.CPU = newPromDesc("cpu_percentage", "Server cpu utilization gauge", serverLabels)
	sc.descs.Connections = newPromDesc("connection_count", "Current number of client connections gauge", serverLabels)
	sc.descs.TotalConnections = newPromDesc("total_connection_count", "Total number of client connections serviced gauge", serverLabels)
	sc.descs.ActiveAccounts = newPromDesc("active_account_count", "Number of active accounts gauge", serverLabels)
	sc.descs.NumSubs = newPromDesc("subs_count", "Current number of subscriptions gauge", serverLabels)
	sc.descs.SentMsgs = newPromDesc("sent_msgs_count", "Number of messages sent gauge", serverLabels)
	sc.descs.SentBytes = newPromDesc("sent_bytes", "Number of messages sent gauge", serverLabels)
	sc.descs.RecvMsgs = newPromDesc("recv_msgs_count", "Number of messages received gauge", serverLabels)
	sc.descs.RecvBytes = newPromDesc("recv_bytes", "Number of messages received gauge", serverLabels)
	sc.descs.SlowConsumers = newPromDesc("slow_consumer_count", "Number of slow consumers gauge", serverLabels)
	sc.descs.RTT = newPromDesc("rtt_nanoseconds", "RTT in nanoseconds gauge", serverLabels)
	sc.descs.Routes = newPromDesc("route_count", "Number of active routes gauge", serverLabels)
	sc.descs.Gateways = newPromDesc("gateway_count", "Number of active gateways gauge", serverLabels)

	// Routes
	sc.descs.RouteSentMsgs = newPromDesc("route_sent_msg_count", "Number of messages sent over the route gauge", routeLabels)
	sc.descs.RouteSentBytes = newPromDesc("route_sent_bytes", "Number of bytes sent over the route gauge", routeLabels)
	sc.descs.RouteRecvMsgs = newPromDesc("route_recv_msg_count", "Number of messages received over the route gauge", routeLabels)
	sc.descs.RouteRecvBytes = newPromDesc("route_recv_bytes", "Number of bytes received over the route gauge", routeLabels)
	sc.descs.RoutePending = newPromDesc("route_pending_bytes", "Number of bytes pending in the route gauge", routeLabels)

	// Gateways
	sc.descs.GatewaySentMsgs = newPromDesc("gateway_sent_msgs_count", "Number of messages sent over the gateway gauge", gatewayLabels)
	sc.descs.GatewaySentBytes = newPromDesc("gateway_sent_bytes", "Number of messages sent over the gateway gauge", gatewayLabels)
	sc.descs.GatewayRecvMsgs = newPromDesc("gateway_recv_msg_count", "Number of messages sent over the gateway gauge", gatewayLabels)
	sc.descs.GatewayRecvBytes = newPromDesc("gateway_recv_bytes", "Number of messages sent over the gateway gauge", gatewayLabels)
	sc.descs.GatewayNumInbound = newPromDesc("gateway_inbound_msg_count", "Number inbound messages through the gateway gauge", gatewayLabels)

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
}

// NewStatzCollector creates a NATS Statz Collector
func NewStatzCollector(nc *nats.Conn, numServers int, pollTimeout time.Duration) *StatzCollector {
	sc := &StatzCollector{
		nc:          nc,
		numServers:  numServers,
		reply:       nc.NewRespInbox(),
		pollTimeout: pollTimeout,
		servers:     make(map[string]bool, numServers),
		doneCh:      make(chan struct{}, 1),
		moreCh:      make(chan struct{}, 1),
	}
	buildDescs(sc)

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
	rtt := time.Now().Sub(sc.start)
	if sc.polling && isCurrent {
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
	if sc.nc.IsConnected() == false {
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
		missingServers := []string{}
		for key, seen := range sc.servers {
			if !seen {
				log.Println(key)
				missingServers = append(missingServers, "["+key+"]")
			}
		}
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

	return nil
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

	// Surveyor
	sc.surveyedCnt.Describe(ch)
	sc.expectedCnt.Describe(ch)
	sc.pollErrCnt.Describe(ch)
	sc.pollTime.Describe(ch)
	sc.lateReplies.Describe(ch)
}

func newGaugeMetric(sm *server.ServerStatsMsg, desc *prometheus.Desc, value float64, labels []string) prometheus.Metric {
	return prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, value, labels...)
}

func (sc *StatzCollector) newNatsUpGaugeMetric(value bool) prometheus.Metric {
	var fval float64
	if value == true {
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

	for _, sm := range sc.stats {
		sc.surveyedCnt.WithLabelValues().Inc()

		ch <- newGaugeMetric(sm, sc.descs.Info, 1, serverInfoLabelValues(sm))

		labels := serverLabelValues(sm)
		ch <- newGaugeMetric(sm, sc.descs.Start, float64(sm.Stats.Start.UnixNano()), labels)
		ch <- newGaugeMetric(sm, sc.descs.Mem, float64(sm.Stats.Mem), labels)
		ch <- newGaugeMetric(sm, sc.descs.Cores, float64(sm.Stats.Cores), labels)
		ch <- newGaugeMetric(sm, sc.descs.CPU, float64(sm.Stats.CPU), labels)
		ch <- newGaugeMetric(sm, sc.descs.Connections, float64(sm.Stats.Connections), labels)
		ch <- newGaugeMetric(sm, sc.descs.TotalConnections, float64(sm.Stats.TotalConnections), labels)
		ch <- newGaugeMetric(sm, sc.descs.ActiveAccounts, float64(sm.Stats.ActiveAccounts), labels)
		ch <- newGaugeMetric(sm, sc.descs.NumSubs, float64(sm.Stats.NumSubs), labels)
		ch <- newGaugeMetric(sm, sc.descs.SentMsgs, float64(sm.Stats.Sent.Msgs), labels)
		ch <- newGaugeMetric(sm, sc.descs.SentBytes, float64(sm.Stats.Sent.Bytes), labels)
		ch <- newGaugeMetric(sm, sc.descs.RecvMsgs, float64(sm.Stats.Received.Msgs), labels)
		ch <- newGaugeMetric(sm, sc.descs.RecvBytes, float64(sm.Stats.Received.Bytes), labels)
		ch <- newGaugeMetric(sm, sc.descs.SlowConsumers, float64(sm.Stats.SlowConsumers), labels)
		ch <- newGaugeMetric(sm, sc.descs.RTT, float64(sc.rtts[sm.Server.ID]), labels)
		ch <- newGaugeMetric(sm, sc.descs.Routes, float64(len(sm.Stats.Routes)), labels)
		ch <- newGaugeMetric(sm, sc.descs.Gateways, float64(len(sm.Stats.Gateways)), labels)

		for _, rs := range sm.Stats.Routes {
			labels = routeLabelValues(sm, rs)
			ch <- newGaugeMetric(sm, sc.descs.RouteSentMsgs, float64(rs.Sent.Msgs), labels)
			ch <- newGaugeMetric(sm, sc.descs.RouteSentBytes, float64(rs.Sent.Bytes), labels)
			ch <- newGaugeMetric(sm, sc.descs.RouteRecvMsgs, float64(rs.Received.Msgs), labels)
			ch <- newGaugeMetric(sm, sc.descs.RouteRecvBytes, float64(rs.Received.Bytes), labels)
			ch <- newGaugeMetric(sm, sc.descs.RoutePending, float64(rs.Pending), labels)
		}

		for _, gw := range sm.Stats.Gateways {
			labels = gatewayLabelValues(sm, gw)
			ch <- newGaugeMetric(sm, sc.descs.GatewaySentMsgs, float64(gw.Sent.Msgs), labels)
			ch <- newGaugeMetric(sm, sc.descs.GatewaySentBytes, float64(gw.Sent.Bytes), labels)
			ch <- newGaugeMetric(sm, sc.descs.GatewayRecvMsgs, float64(gw.Received.Msgs), labels)
			ch <- newGaugeMetric(sm, sc.descs.GatewayRecvBytes, float64(gw.Received.Bytes), labels)
			ch <- newGaugeMetric(sm, sc.descs.GatewayNumInbound, float64(gw.NumInbound), labels)
		}
	}
}
