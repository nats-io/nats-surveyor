package surveyor

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

var testMetricInfoLabels = prometheus.Labels{"foo": "bar"}

func assertMetricInfos(t *testing.T, infos []MetricInfo) {
	t.Helper()

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
