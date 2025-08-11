package surveyor

import (
	"testing"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/prometheus/client_golang/prometheus"
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

func TestStatzCollector_WithStats(t *testing.T) {
	sc := NewStatzCollector(nil, nil, 0, 0, 0, false, false, false, "", false, nil, "", nil, WithStats(
		WithStatsBatch{
			Stats: []*server.ServerStatsMsg{
				{
					Server: server.ServerInfo{
						ID:      "server1",
						Name:    "server1",
						Cluster: "cluster1",
					},
				},
			},
		}))

	registry := prometheus.NewRegistry()
	registry.MustRegister(sc)
	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("error gathering metrics: %v", err)
	}
	if len(families) == 0 {
		t.Fatal("error gathering metrics: families is empty")
	}
}
