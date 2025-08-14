package surveyor

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/prometheus/client_golang/prometheus"
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
	statsRaw, err := os.ReadFile("testdata/networks/stats.json")
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
	if !strings.Contains(output, "nats_core_route_recv_msg_count") {
		t.Fatalf("invalid output, missing 'nats_core_route_recv_msg_count':\n%v\n", output)
	}
	if !strings.Contains(output, "server_name") {
		t.Fatalf("invalid output, missing 'server_name':\n%v\n", output)
	}
	if !strings.Contains(output, "server_cluster") {
		t.Fatalf("invalid output, missing 'server_cluster':\n%v\n", output)
	}
	if !strings.Contains(output, "server_id") {
		t.Fatalf("invalid output, missing 'server_id':\n%v\n", output)
	}
	if !strings.Contains(output, "server_gateway_name") {
		t.Fatalf("invalid output, missing 'server_gateway_name':\n%v\n", output)
	}
	if !strings.Contains(output, "server_gateway_name_id") {
		t.Fatalf("invalid output, missing 'server_gateway_name_id':\n%v\n", output)
	}
	if !strings.Contains(output, "server_route_name") {
		t.Fatalf("invalid output, missing 'server_route_name':\n%v\n", output)
	}
	if !strings.Contains(output, "server_route_name_id") {
		t.Fatalf("invalid output, missing 'server_route_name_id':\n%v\n", output)
	}
	if !strings.Contains(output, "nats_survey_surveyed_count 1") {
		t.Fatalf("invalid output, missing 'nats_survey_surveyed_count 1':\n%v\n", output)
	}

}

func TestStatzCollector_WithStats_Account(t *testing.T) {
	statsRaw, err := os.ReadFile("testdata/networks/accstatzs.json")
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
			t.Logf("output: %s", output)
			t.Fatalf("missing: %s", m)
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
