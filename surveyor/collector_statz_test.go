package surveyor

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	st "github.com/nats-io/nats-surveyor/test"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/expfmt"
)

func TestStatzCollector_WithoutNATSConnection(t *testing.T) {
	sc := NewStatzCollector(nil, nil, 0, 0, 0, false, false, false, false, CollectJszNone, 0, false, nil, "", nil)

	registry := prometheus.NewRegistry()
	registry.MustRegister(sc)
	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("error gathering metrics: %v", err)
	}
	if families == nil {
		t.Fatal("error gathering metrics: families is nil")
	}
}

func TestStatzCollector_WithStats_Stats(t *testing.T) {
	statsRaw, err := os.ReadFile("testdata/stats/stats.json")
	if err != nil {
		t.Fatalf("error reading testdata: %v", err)
	}
	stats := &server.ServerStatsMsg{}
	err = json.Unmarshal(statsRaw, stats)
	if err != nil {
		t.Fatalf("error unmarshalling stats: %v", err)
	}

	sc, err := NewStatzCollectorOpts(
		WithStats(WithStatsBatch{
			Stats: []*server.ServerStatsMsg{stats},
		}),
	)
	if err != nil {
		t.Fatalf("error creating statz collector: %v", err)
	}

	output := gatherStatzCollectorMetrics(t, sc)

	// Based on TestSurveyor_Basic
	want := []string{
		"nats_core_route_recv_msg_count",
		"nats_core_go_memlimit_bytes",
		"server_name",
		"server_cluster",
		"server_id",
		"server_gateway_name",
		"server_gateway_name_id",
		"server_route_name",
		"server_route_name_id",
		"nats_survey_surveyed_count 1",
	}
	for _, m := range want {
		if !strings.Contains(output, m) {
			t.Fatalf("invalid output, missing '%s':\n%v\n", m, output)
		}
	}
}

func TestStatzCollector_WithStats_Account(t *testing.T) {
	statsRaw, err := os.ReadFile("testdata/stats/accstatzs.json")
	if err != nil {
		t.Fatalf("error reading testdata: %v", err)
	}
	stats := &ServerAPIAccstatzResponse{}
	err = json.Unmarshal(statsRaw, stats)
	if err != nil {
		t.Fatalf("error unmarshalling stats: %v", err)
	}

	sc, err := NewStatzCollectorOpts(
		WithStats(WithStatsBatch{AccStatzs: []*ServerAPIAccstatzResponse{stats}}),
		WithCollectAccounts(true, true),
	)
	if err != nil {
		t.Fatalf("error creating statz collector: %v", err)
	}

	output := gatherStatzCollectorMetrics(t, sc)

	// Based on TestSurveyor_Account
	want := []string{
		"nats_core_account_count",
		"nats_core_account_conn_count",
		"nats_core_account_total_conn_count",
		"nats_core_account_leaf_count",
		"nats_core_account_sub_count",
		"nats_core_account_slow_consumer_count",
		"nats_core_account_bytes_sent",
		"nats_core_account_bytes_recv",
		"nats_core_account_msgs_sent",
		"nats_core_account_msgs_recv",
		"nats_core_account_jetstream_enabled",
		"nats_core_account_jetstream_stream_count",
	}
	for _, m := range want {
		if !strings.Contains(output, m) {
			t.Fatalf("invalid output, missing '%s':\n%v\n", m, output)
		}
	}
}

func TestStatzCollector_WithStats_Gatewayz(t *testing.T) {
	statsRaw, err := os.ReadFile("testdata/stats/gatewayzs.json")
	if err != nil {
		t.Fatalf("error reading testdata: %v", err)
	}
	stats := &server.ServerAPIGatewayzResponse{}
	err = json.Unmarshal(statsRaw, stats)
	if err != nil {
		t.Fatalf("error unmarshalling stats: %v", err)
	}

	sc, err := NewStatzCollectorOpts(
		WithStats(WithStatsBatch{GatewayStatzs: []*server.ServerAPIGatewayzResponse{stats}}),
		WithCollectGatewayz(true),
	)
	if err != nil {
		t.Fatalf("error creating statz collector: %v", err)
	}

	output := gatherStatzCollectorMetrics(t, sc)

	// Based on TestSurveyor_Gatewayz
	want := []string{
		"nats_core_gatewayz_inbound_gateway_configured",
		"nats_core_gatewayz_inbound_gateway_conn_idle_seconds",
		"nats_core_gatewayz_inbound_gateway_conn_in_bytes",
		"nats_core_gatewayz_inbound_gateway_conn_in_msgs",
		"nats_core_gatewayz_inbound_gateway_conn_last_activity_seconds",
		"nats_core_gatewayz_inbound_gateway_conn_out_bytes",
		"nats_core_gatewayz_inbound_gateway_conn_out_msgs",
		"nats_core_gatewayz_inbound_gateway_conn_pending_bytes",
		"nats_core_gatewayz_inbound_gateway_conn_rtt",
		"nats_core_gatewayz_inbound_gateway_conn_subscriptions",
		"nats_core_gatewayz_inbound_gateway_conn_uptime_seconds",
		"nats_core_gatewayz_outbound_gateway_configured",
		"nats_core_gatewayz_outbound_gateway_conn_idle_seconds",
		"nats_core_gatewayz_outbound_gateway_conn_in_bytes",
		"nats_core_gatewayz_outbound_gateway_conn_in_msgs",
		"nats_core_gatewayz_outbound_gateway_conn_last_activity_seconds",
		"nats_core_gatewayz_outbound_gateway_conn_out_bytes",
		"nats_core_gatewayz_outbound_gateway_conn_out_msgs",
		"nats_core_gatewayz_outbound_gateway_conn_pending_bytes",
		"nats_core_gatewayz_outbound_gateway_conn_rtt",
		"nats_core_gatewayz_outbound_gateway_conn_subscriptions",
		"nats_core_gatewayz_outbound_gateway_conn_uptime_seconds",
	}
	for _, m := range want {
		if !strings.Contains(output, m) {
			t.Fatalf("invalid output, missing '%s':\n%v\n", m, output)
		}
	}
}

func TestStatzCollector_WithStats_Jsz(t *testing.T) {
	statsRaw, err := os.ReadFile("testdata/stats/jsz.json")
	if err != nil {
		t.Fatalf("error reading testdata: %v", err)
	}
	stats := &server.ServerAPIJszResponse{}
	err = json.Unmarshal(statsRaw, stats)
	if err != nil {
		t.Fatalf("error unmarshalling stats: %v", err)
	}

	streamMetrics := []string{
		"nats_stream_consumer_count",
		"nats_stream_first_seq",
		"nats_stream_last_seq",
		"nats_stream_subject_count",
		"nats_stream_total_bytes",
		"nats_stream_total_messages",
	}
	consumerMetrics := []string{
		"nats_consumer_ack_floor_consumer_seq",
		"nats_consumer_ack_floor_stream_seq",
		"nats_consumer_delivered_consumer_seq",
		"nats_consumer_delivered_stream_seq",
		"nats_consumer_num_ack_pending",
		"nats_consumer_num_pending",
		"nats_consumer_num_redelivered",
		"nats_consumer_num_waiting",
	}
	metaSnapshotMetrics := []string{
		"nats_core_jetstream_meta_snapshot_last_duration_seconds",
		"nats_core_jetstream_meta_snapshot_last_timestamp_seconds",
		"nats_core_jetstream_meta_snapshot_pending_bytes",
		"nats_core_jetstream_meta_snapshot_pending_entries",
	}

	allMetrics := []string{}
	allMetrics = append(allMetrics, streamMetrics...)
	allMetrics = append(allMetrics, consumerMetrics...)

	type test struct {
		name           string
		jsz            CollectJsz
		jszLeadersOnly bool
		jszFilters     []JszFilter
		assert         func(t *testing.T, test *test, output string)
	}

	tests := []test{
		// jsz type tests
		{
			name:           "collect none",
			jsz:            CollectJszNone,
			jszLeadersOnly: false,
			jszFilters:     nil,
			assert: func(t *testing.T, test *test, output string) {
				for _, m := range allMetrics {
					if strings.Contains(output, m) {
						t.Fatalf("invalid output, should not contain '%s':\n%v\n", m, output)
					}
				}
			},
		},
		{
			name:           "collect all",
			jsz:            CollectJszAll,
			jszLeadersOnly: false,
			jszFilters:     nil,
			assert: func(t *testing.T, test *test, output string) {
				for _, m := range append(allMetrics, metaSnapshotMetrics...) {
					if !strings.Contains(output, m) {
						t.Fatalf("invalid output, missing '%s':\n%v\n", m, output)
					}
				}
			},
		},
		{
			name:           "collect streams",
			jsz:            CollectJszStreams,
			jszLeadersOnly: false,
			jszFilters:     nil,
			assert: func(t *testing.T, test *test, output string) {
				for _, m := range streamMetrics {
					if !strings.Contains(output, m) {
						t.Fatalf("invalid output, missing '%s':\n%v\n", m, output)
					}
				}
				for _, m := range consumerMetrics {
					if strings.Contains(output, m) {
						t.Fatalf("invalid output, should not contain '%s':\n%v\n", m, output)
					}
				}
			},
		},
		{
			name:           "collect consumers",
			jsz:            CollectJszConsumers,
			jszLeadersOnly: false,
			jszFilters:     nil,
			assert: func(t *testing.T, test *test, output string) {
				for _, m := range consumerMetrics {
					if !strings.Contains(output, m) {
						t.Fatalf("invalid output, missing '%s':\n%v\n", m, output)
					}
				}
				for _, m := range streamMetrics {
					if strings.Contains(output, m) {
						t.Fatalf("invalid output, should not contain '%s':\n%v\n", m, output)
					}
				}
			},
		},
		// leaders only tests
		{
			name:           "collect leaders only",
			jsz:            CollectJszAll,
			jszLeadersOnly: true,
			jszFilters:     nil,
			assert: func(t *testing.T, test *test, output string) {
				lines := strings.Split(output, "\n")

				for _, m := range consumerMetrics {
					count := 0
					for _, line := range lines {
						if strings.Contains(line, m) && !strings.HasPrefix(line, "#") {
							count++
						}
					}
					if count != 1 {
						t.Fatalf("invalid output, expected %v, got %v: %v\n", 1, count, output)
					}
				}
			},
		},
		{
			name:           "not collect leaders only",
			jsz:            CollectJszAll,
			jszLeadersOnly: false,
			jszFilters:     nil,
			assert: func(t *testing.T, test *test, output string) {
				lines := strings.Split(output, "\n")

				for _, m := range consumerMetrics {
					count := 0
					for _, line := range lines {
						if strings.Contains(line, m) && !strings.HasPrefix(line, "#") {
							count++
						}
					}
					if count != 3 {
						t.Fatalf("invalid output, expected %v, got %v: %v\n", 3, count, output)
					}
				}
			},
		},
		// jsz filter tests
		{
			name:           "collect streams with filter",
			jsz:            CollectJszStreams,
			jszLeadersOnly: false,
			jszFilters:     []JszFilter{StreamTotalMessages},
			assert: func(t *testing.T, test *test, output string) {
				metrics := []string{}
				for _, filter := range test.jszFilters {
					id := JszFilterIds[filter][0]
					m := "nats_" + id
					metrics = append(metrics, m)
				}

				for _, m := range metrics {
					if !strings.Contains(output, m) {
						t.Fatalf("invalid output, missing '%s':\n%v\n", m, output)
					}
				}

				for _, m := range allMetrics {
					if !slices.Contains(metrics, m) && strings.Contains(output, m) {
						t.Fatalf("invalid output, should not contain '%s':\n%v\n", m, output)
					}
				}

			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sc, err := NewStatzCollectorOpts(
				WithStats(WithStatsBatch{JsStatzs: []*server.ServerAPIJszResponse{stats}}),
				WithCollectJsz(test.jsz, test.jszLeadersOnly, test.jszFilters),
			)
			if err != nil {
				t.Fatalf("error creating statz collector: %v", err)
			}

			output := gatherStatzCollectorMetrics(t, sc)
			test.assert(t, &test, output)
		})
	}
}

func TestStatzCollector_GoMemLimit(t *testing.T) {
	tests := []struct {
		name           string
		gomemlimit     int64
		expectedMetric string
	}{
		{
			name:           "GOMEMLIMIT set to 1GB",
			gomemlimit:     1073741824,
			expectedMetric: "nats_core_go_memlimit_bytes{server_cluster=\"\",server_id=\"test-server\",server_name=\"test-server\"} 1.073741824e+09",
		},
		{
			name:           "GOMEMLIMIT not set",
			gomemlimit:     0,
			expectedMetric: "nats_core_go_memlimit_bytes{server_cluster=\"\",server_id=\"test-server\",server_name=\"test-server\"} 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stats := &server.ServerStatsMsg{
				Server: server.ServerInfo{
					ID:   "test-server",
					Name: "test-server",
				},
				Stats: server.ServerStats{
					MemLimit: tt.gomemlimit,
				},
			}

			sc, err := NewStatzCollectorOpts(
				WithStats(WithStatsBatch{
					Stats: []*server.ServerStatsMsg{stats},
				}),
			)
			if err != nil {
				t.Fatalf("error creating statz collector: %v", err)
			}

			output := gatherStatzCollectorMetrics(t, sc)

			if !strings.Contains(output, "nats_core_go_memlimit_bytes") {
				t.Fatalf("missing GOMEMLIMIT metric in output:\n%v", output)
			}

			if !strings.Contains(output, tt.expectedMetric) {
				t.Fatalf("expected metric value not found. Expected: %s\nActual output:\n%v", tt.expectedMetric, output)
			}
		})
	}
}

func TestStatzCollector_GoMaxProcs(t *testing.T) {
	stats := &server.ServerStatsMsg{
		Server: server.ServerInfo{
			ID:   "test-server",
			Name: "test-server",
		},
		Stats: server.ServerStats{
			MaxProcs: 4,
		},
	}

	sc, err := NewStatzCollectorOpts(
		WithStats(WithStatsBatch{
			Stats: []*server.ServerStatsMsg{stats},
		}),
	)
	if err != nil {
		t.Fatalf("error creating statz collector: %v", err)
	}

	output := gatherStatzCollectorMetrics(t, sc)

	expectedMetric := "nats_core_gomaxprocs{server_cluster=\"\",server_id=\"test-server\",server_name=\"test-server\"} 4"
	if !strings.Contains(output, expectedMetric) {
		t.Fatalf("expected metric value not found. Expected: %s\nActual output:\n%v", expectedMetric, output)
	}
}

func TestStatzCollector_ClientTrafficMetrics(t *testing.T) {
	const (
		sentMsgs  = 11
		sentBytes = 23
		recvMsgs  = 37
		recvBytes = 53
	)

	stats := &server.ServerStatsMsg{
		Server: server.ServerInfo{ID: "test-server", Name: "test-server"},
		Stats: server.ServerStats{
			SentToClients:       server.DataStats{MsgBytes: server.MsgBytes{Msgs: sentMsgs, Bytes: sentBytes}},
			ReceivedFromClients: server.DataStats{MsgBytes: server.MsgBytes{Msgs: recvMsgs, Bytes: recvBytes}},
		},
	}

	sc, err := NewStatzCollectorOpts(
		WithStats(WithStatsBatch{Stats: []*server.ServerStatsMsg{stats}}),
	)
	if err != nil {
		t.Fatalf("error creating statz collector: %v", err)
	}

	output := gatherStatzCollectorMetrics(t, sc)

	want := []string{
		`nats_core_sent_to_client_msgs_total{server_cluster="",server_id="test-server",server_name="test-server"} 11`,
		`nats_core_sent_to_client_bytes_total{server_cluster="",server_id="test-server",server_name="test-server"} 23`,
		`nats_core_recv_from_client_msgs_total{server_cluster="",server_id="test-server",server_name="test-server"} 37`,
		`nats_core_recv_from_client_bytes_total{server_cluster="",server_id="test-server",server_name="test-server"} 53`,
	}
	for _, w := range want {
		if !strings.Contains(output, w) {
			t.Errorf("missing or wrong metric line:\n  want: %s\n  output:\n%v", w, output)
		}
	}
}

func TestStatzCollector_MetricInfos(t *testing.T) {
	sc, err := NewStatzCollectorOpts(
		WithStats(WithStatsBatch{
			Stats:         []*server.ServerStatsMsg{},
			GatewayStatzs: []*server.ServerAPIGatewayzResponse{},
			JsStatzs:      []*server.ServerAPIJszResponse{},
			AccStatzs:     []*ServerAPIAccstatzResponse{},
		}),
		WithConstantLabels(testMetricInfoLabels),
	)
	if err != nil {
		t.Fatalf("error creating statz collector: %v", err)
	}

	infos := sc.MetricInfos()
	assertMetricInfos(t, infos)
}

func gatherStatzCollectorMetrics(t *testing.T, sc *StatzCollector) string {
	registry := prometheus.NewRegistry()
	registry.MustRegister(sc)
	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("error gathering metrics: %v", err)
	}
	if len(families) == 0 {
		t.Fatal("error gathering metrics: families is empty")
	}

	// convert metrics families to text format
	buf := bytes.NewBuffer(nil)
	format := expfmt.NewFormat(expfmt.TypeTextPlain)
	enc := expfmt.NewEncoder(buf, format)
	for _, family := range families {
		enc.Encode(family)
	}
	output := buf.String()
	return output
}

func TestPollTime(t *testing.T) {
	sc := st.NewJetStreamCluster(t)
	defer sc.Shutdown()

	opt := getTestOptions()
	opt.URLs = sc.ClientURL()
	opt.Credentials = ""
	opt.NATSUser = "admin"
	opt.NATSPassword = "s3cr3t!"
	opt.ServerResponseWait = 500 * time.Millisecond
	// Set expected to 4 to simulate situation when one of the servers is down
	opt.ExpectedServers = 4
	s, err := NewSurveyor(opt)
	if err != nil {
		t.Fatalf("couldn't create surveyor: %v", err)
	}
	if err = s.Start(); err != nil {
		t.Fatalf("start error: %v", err)
	}
	defer s.Stop()

	start := time.Now()
	output, err := PollSurveyorEndpoint(t, "http://127.0.0.1:7777/metrics", false, http.StatusOK)
	if err != nil {
		t.Fatalf("%v", err)
	}
	took := time.Since(start)
	t.Logf("Response took: %v", took)
	if took >= opt.PollTimeout+opt.ServerResponseWait {
		t.Fatalf("PollTimeout should limit total wait time. "+
			"PollTimeout:%v, ServerResponseWait:%v, Response took: %v",
			opt.PollTimeout, opt.ServerResponseWait, took)
	}
	if !strings.Contains(output, `nats_survey_surveyed_count 3`) {
		t.Fatalf("Expected response from 3 servers:  %v\n", output)
	}

}

// Label sets of the fixture in testdata/raftz/raftz.json, in the order the
// Prometheus text format writes them, which is alphabetical by label name.
const (
	raftzGroupMetaLabels   = `{account="$SYS",group="_meta_",server_cluster="C1",server_id="NAAA",server_name="n1"}`
	raftzGroupIdleLabels   = `{account="ACC",group="S-IDLE",server_cluster="C1",server_id="NAAA",server_name="n1"}`
	raftzGroupBehindLabels = `{account="ACC",group="C-BEHIND",server_cluster="C1",server_id="NAAA",server_name="n1"}`

	// The legacy meta series pass (cluster, name, id) into labels declared as
	// (server_id, server_name, cluster_name). That mapping predates the per
	// group family and is asserted here so that it cannot change unnoticed.
	raftzMetaLabels = `{cluster_name="NAAA",server_id="C1",server_name="n1"}`
)

func raftzTestdata(t *testing.T) *ServerAPIRaftzResponse {
	t.Helper()

	raw, err := os.ReadFile("testdata/raftz/raftz.json")
	if err != nil {
		t.Fatalf("error reading testdata: %v", err)
	}
	resp := &ServerAPIRaftzResponse{}
	if err := json.Unmarshal(raw, resp); err != nil {
		t.Fatalf("error unmarshalling raftz testdata: %v", err)
	}
	if resp.Data == nil {
		t.Fatal("raftz testdata has no data")
	}
	return resp
}

func assertContainsAll(t *testing.T, output string, want []string) {
	t.Helper()

	for _, w := range want {
		if !strings.Contains(output, w) {
			t.Errorf("missing: %s", w)
		}
	}
	if t.Failed() {
		t.Logf("output: %s", output)
		t.FailNow()
	}
}

func assertContainsNone(t *testing.T, output string, unwanted []string) {
	t.Helper()

	for _, u := range unwanted {
		if strings.Contains(output, u) {
			t.Errorf("unexpected: %s", u)
		}
	}
	if t.Failed() {
		t.Logf("output: %s", output)
		t.FailNow()
	}
}

func TestStatzCollector_WithStats_RaftzGroups(t *testing.T) {
	sc, err := NewStatzCollectorOpts(
		WithStats(WithStatsBatch{
			RaftzStatzs: []*ServerAPIRaftzResponse{raftzTestdata(t)},
		}),
		WithCollectRaftz(true),
		WithCollectRaftzGroups(true),
	)
	if err != nil {
		t.Fatalf("error creating statz collector: %v", err)
	}

	output := gatherStatzCollectorMetrics(t, sc)

	assertContainsAll(t, output, []string{
		// overrun_count is the only counter in the family
		"# TYPE nats_core_raftz_group_overrun_count counter",
		"# TYPE nats_core_raftz_group_ipq_apply_len gauge",
		"# TYPE nats_core_raftz_group_state gauge",

		// A group that is behind: every field of the fixture, including the
		// ones the server only sends when they are set.
		"nats_core_raftz_group_state" + raftzGroupBehindLabels + " 2",
		"nats_core_raftz_group_size" + raftzGroupBehindLabels + " 3",
		"nats_core_raftz_group_quorum_needed" + raftzGroupBehindLabels + " 2",
		"nats_core_raftz_group_committed" + raftzGroupBehindLabels + " 9100",
		"nats_core_raftz_group_applied" + raftzGroupBehindLabels + " 9000",
		"nats_core_raftz_group_pindex" + raftzGroupBehindLabels + " 8900",
		"nats_core_raftz_group_term" + raftzGroupBehindLabels + " 7",
		"nats_core_raftz_group_observer" + raftzGroupBehindLabels + " 1",
		"nats_core_raftz_group_paused" + raftzGroupBehindLabels + " 1",
		"nats_core_raftz_group_catching_up" + raftzGroupBehindLabels + " 1",
		"nats_core_raftz_group_overrun" + raftzGroupBehindLabels + " 1",
		"nats_core_raftz_group_overrun_count" + raftzGroupBehindLabels + " 3",
		"nats_core_raftz_group_ipq_proposal_len" + raftzGroupBehindLabels + " 1",
		"nats_core_raftz_group_ipq_entry_len" + raftzGroupBehindLabels + " 2",
		"nats_core_raftz_group_ipq_resp_len" + raftzGroupBehindLabels + " 4",
		"nats_core_raftz_group_ipq_apply_len" + raftzGroupBehindLabels + " 42",
		"nats_core_raftz_group_wal_msgs" + raftzGroupBehindLabels + " 100",
		"nats_core_raftz_group_wal_bytes" + raftzGroupBehindLabels + " 65536",

		// A healthy group: the fields the server omits when empty must still
		// be exported, as 0, or an alert rule cannot use them.
		"nats_core_raftz_group_state" + raftzGroupIdleLabels + " 0",
		"nats_core_raftz_group_observer" + raftzGroupIdleLabels + " 0",
		"nats_core_raftz_group_paused" + raftzGroupIdleLabels + " 0",
		"nats_core_raftz_group_catching_up" + raftzGroupIdleLabels + " 0",
		"nats_core_raftz_group_overrun" + raftzGroupIdleLabels + " 0",
		"nats_core_raftz_group_overrun_count" + raftzGroupIdleLabels + " 0",
		"nats_core_raftz_group_ipq_apply_len" + raftzGroupIdleLabels + " 0",
		"nats_core_raftz_group_committed" + raftzGroupIdleLabels + " 5000",
		"nats_core_raftz_group_applied" + raftzGroupIdleLabels + " 5000",
		"nats_core_raftz_group_wal_bytes" + raftzGroupIdleLabels + " 8192",

		// The metalayer group is part of the per group family as well.
		"nats_core_raftz_group_state" + raftzGroupMetaLabels + " 1",
		"nats_core_raftz_group_committed" + raftzGroupMetaLabels + " 120",

		// and the legacy series are unchanged.
		"nats_core_raftz_meta_committed" + raftzMetaLabels + " 120",
		"nats_core_raftz_meta_applied" + raftzMetaLabels + " 120",
		"nats_core_raftz_meta_pindex" + raftzMetaLabels + " 118",
	})
}

func TestStatzCollector_WithStats_RaftzGroupsDisabled(t *testing.T) {
	sc, err := NewStatzCollectorOpts(
		WithStats(WithStatsBatch{
			RaftzStatzs: []*ServerAPIRaftzResponse{raftzTestdata(t)},
		}),
		WithCollectRaftz(true),
	)
	if err != nil {
		t.Fatalf("error creating statz collector: %v", err)
	}

	output := gatherStatzCollectorMetrics(t, sc)

	// --raftz on its own exports what it always did, and nothing more.
	assertContainsAll(t, output, []string{
		"nats_core_raftz_meta_committed" + raftzMetaLabels + " 120",
		"nats_core_raftz_meta_applied" + raftzMetaLabels + " 120",
		"nats_core_raftz_meta_pindex" + raftzMetaLabels + " 118",
	})
	assertContainsNone(t, output, []string{"nats_core_raftz_group_"})
}

func TestStatzCollector_WithStats_RaftzGroupsActiveOnly(t *testing.T) {
	sc, err := NewStatzCollectorOpts(
		WithStats(WithStatsBatch{
			RaftzStatzs: []*ServerAPIRaftzResponse{raftzTestdata(t)},
		}),
		WithCollectRaftz(true),
		WithCollectRaftzGroups(true),
		WithRaftzGroupsActiveOnly(true),
	)
	if err != nil {
		t.Fatalf("error creating statz collector: %v", err)
	}

	output := gatherStatzCollectorMetrics(t, sc)

	// Only the group that is behind is exported. The idle groups, metalayer
	// included, are skipped entirely, while the legacy meta series stay.
	assertContainsAll(t, output, []string{
		"nats_core_raftz_group_ipq_apply_len" + raftzGroupBehindLabels + " 42",
		"nats_core_raftz_group_overrun_count" + raftzGroupBehindLabels + " 3",
		"nats_core_raftz_meta_committed" + raftzMetaLabels + " 120",
	})
	assertContainsNone(t, output, []string{
		"nats_core_raftz_group_state" + raftzGroupIdleLabels,
		"nats_core_raftz_group_state" + raftzGroupMetaLabels,
	})
}
