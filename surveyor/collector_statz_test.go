package surveyor

import (
	"bytes"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/expfmt"
)

func TestStatzCollector_WithoutNATSConnection(t *testing.T) {
	sc := NewStatzCollector(nil, nil, 0, 0, 0, false, false, false, "", 0, false, nil, "", nil)

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
				for _, m := range allMetrics {
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

func TestStatzCollector_WithStats_JetstreamMetaSnapshot(t *testing.T) {
	statsRaw, err := os.ReadFile("testdata/stats/stats-meta-snapshot.json")
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

	// Verify all JetStream meta cluster snapshot metrics are present
	want := []string{
		"nats_core_jetstream_meta_snapshot_pending_entries",
		"nats_core_jetstream_meta_snapshot_pending_bytes",
		"nats_core_jetstream_meta_snapshot_last_duration",
	}

	for _, m := range want {
		if !strings.Contains(output, m) {
			t.Fatalf("invalid output, missing '%s':\n%v\n", m, output)
		}
	}

	// Verify specific metric values
	expectedValues := []string{
		"nats_core_jetstream_meta_snapshot_pending_entries{cluster_name=\"meta-cluster\",server_id=\"meta-server1\",server_name=\"meta-server1\"} 1500",
		"nats_core_jetstream_meta_snapshot_pending_bytes{cluster_name=\"meta-cluster\",server_id=\"meta-server1\",server_name=\"meta-server1\"} 524288",
		"nats_core_jetstream_meta_snapshot_last_duration{cluster_name=\"meta-cluster\",server_id=\"meta-server1\",server_name=\"meta-server1\"} 1.23456789e+09",
	}

	for _, expectedValue := range expectedValues {
		if !strings.Contains(output, expectedValue) {
			t.Fatalf("expected metric value not found. Expected: %s\nActual output:\n%v", expectedValue, output)
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
