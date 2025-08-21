package surveyor

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
)

func TestStatzCollector_WithoutNATSConnection(t *testing.T) {
	sc := NewStatzCollector(nil, nil, 0, 0, 0, false, false, false, "", false, nil, "", nil)

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
		WithStats(WithStatsBatch{
			AccStatzs: []*ServerAPIAccstatzResponse{stats},
		}),
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
		WithStats(WithStatsBatch{
			GatewayStatzs: []*server.ServerAPIGatewayzResponse{stats},
		}),
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

	sc, err := NewStatzCollectorOpts(
		WithStats(WithStatsBatch{
			JsStatzs: []*server.ServerAPIJszResponse{stats},
		}),
	)
	if err != nil {
		t.Fatalf("error creating statz collector: %v", err)
	}

	output := gatherStatzCollectorMetrics(t, sc)

	want := []string{
		"nats_core_jetstream_server_jetstream_disabled",
		"nats_core_jetstream_server_total_streams",
		"nats_core_jetstream_server_total_consumers",
		"nats_core_jetstream_server_total_messages",
		"nats_core_jetstream_server_total_message_bytes",
		"nats_core_jetstream_server_max_memory",
		"nats_core_jetstream_server_max_storage",
	}
	for _, m := range want {
		if !strings.Contains(output, m) {
			t.Fatalf("invalid output, missing '%s':\n%v\n", m, output)
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
		WithConstantLabels(prometheus.Labels{"foo": "bar"}),
	)
	if err != nil {
		t.Fatalf("error creating statz collector: %v", err)
	}

	infos := sc.MetricInfos()
	if len(infos) == 0 {
		t.Fatal("error getting metric infos: infos is empty")
	}

	for _, info := range infos {
		if info.Name() == "" {
			t.Fatalf("error getting metric infos: name is empty: %s", info.Name())
		}
		if info.Help() == "" {
			t.Fatalf("error getting metric infos: help is empty: %s", info.Name())
		}
		if info.Type() == dto.MetricType_UNTYPED {
			t.Fatalf("error getting metric infos: type is untyped: %s", info.Name())
		}
		if info.Desc() == nil {
			t.Fatalf("error getting metric infos: desc is nil: %s", info.Name())
		}

		labels := info.ConstLabels()
		if len(labels) != 1 {
			t.Fatalf("error getting metric infos: expected 1 label, got %d: %s", len(labels), info.Name())
		}
		if labels["foo"] != "bar" {
			t.Fatalf("error getting metric infos: expected label foo=bar, got %+v: %s", labels, info.Name())
		}
	}
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
