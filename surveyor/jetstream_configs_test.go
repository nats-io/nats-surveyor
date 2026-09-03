// Copyright 2020 The NATS Authors
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
	"context"
	"testing"
	"time"

	st "github.com/nats-io/nats-surveyor/test"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus"
	ptu "github.com/prometheus/client_golang/prometheus/testutil"
)

func TestJetStreamConfig_IntervalValidation(t *testing.T) {
	registry := prometheus.NewRegistry()
	metrics := NewJetStreamConfigMetrics(registry, nil)
	opt := GetDefaultOptions()
	cp := newSurveyorConnPool(opt, registry)

	for _, interval := range []time.Duration{-time.Second, 0, 999 * time.Millisecond} {
		if _, err := newJetStreamConfigListener(cp, opt.Logger, metrics, interval); err == nil {
			t.Fatalf("expected interval %s to be rejected", interval)
		}
	}

	if _, err := newJetStreamConfigListener(cp, opt.Logger, metrics, MinJSConfigPollInterval); err != nil {
		t.Fatalf("expected interval %s to be accepted: %s", MinJSConfigPollInterval, err)
	}
}

func TestJetStreamConfig_Poll(t *testing.T) {
	srv := st.NewJetStreamServer(t)
	defer srv.Shutdown()

	nc, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("could not connect to nats: %s", err)
	}
	defer nc.Close()

	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("could not create jetstream context: %s", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stream, err := js.CreateStream(ctx, jetstream.StreamConfig{
		Name:     "SURVEYED",
		Subjects: []string{"surveyed.>"},
		MaxMsgs:  100,
	})
	if err != nil {
		t.Fatalf("could not create stream: %s", err)
	}
	if _, err := stream.CreateConsumer(ctx, jetstream.ConsumerConfig{Durable: "PULLER"}); err != nil {
		t.Fatalf("could not create consumer: %s", err)
	}

	opt := GetDefaultOptions()
	opt.URLs = srv.ClientURL()
	registry := prometheus.NewRegistry()
	metrics := NewJetStreamConfigMetrics(registry, nil)
	cp := newSurveyorConnPool(opt, registry)

	listener, err := newJetStreamConfigListener(cp, opt.Logger, metrics, MinJSConfigPollInterval)
	if err != nil {
		t.Fatalf("could not create listener: %s", err)
	}
	if err := listener.Start(&NatsContext{}); err != nil {
		t.Fatalf("could not start listener: %s", err)
	}
	defer listener.Stop()

	waitFor(t, func() bool { return ptu.CollectAndCount(metrics.jsStreamConfig) == 1 }, "stream configuration metric")

	if got := ptu.ToFloat64(metrics.jsStreamLimitMaxMsgs.WithLabelValues("SURVEYED")); got != 100 {
		t.Fatalf("unexpected stream max msgs limit: got %v, expected 100", got)
	}

	waitFor(t, func() bool { return ptu.CollectAndCount(metrics.jsConsumerConfig) == 1 }, "consumer configuration metric")

	if got := ptu.ToFloat64(metrics.jsConsumerConfig.WithLabelValues("SURVEYED", "PULLER", "AckExplicit", "true")); got != 1 {
		t.Fatalf("unexpected consumer configuration metric: got %v, expected 1", got)
	}

	// The server is not clustered, so there is no Raft group to report on.
	if got := ptu.CollectAndCount(metrics.jsStreamPeerLeader); got != 0 {
		t.Fatalf("unexpected stream peer series on a non-clustered server: got %d", got)
	}

	// Deleting the stream has to take its series with it, including those of
	// its consumers.
	if err := js.DeleteStream(ctx, "SURVEYED"); err != nil {
		t.Fatalf("could not delete stream: %s", err)
	}
	waitFor(t, func() bool {
		return ptu.CollectAndCount(metrics.jsStreamConfig) == 0 &&
			ptu.CollectAndCount(metrics.jsStreamLimitMaxMsgs) == 0 &&
			ptu.CollectAndCount(metrics.jsConsumerConfig) == 0
	}, "series of the deleted stream to be removed")
}

func TestJetStreamConfig_RaftPeers(t *testing.T) {
	if peers := raftPeers(nil); peers != nil {
		t.Fatalf("expected no peers without cluster info, got %v", peers)
	}

	peers := raftPeers(&jetstream.ClusterInfo{
		Leader: "s1",
		Replicas: []*jetstream.PeerInfo{
			{Name: "s2", Current: true},
			{Name: "s3", Offline: true, Lag: 42},
		},
	})

	expected := []raftPeer{
		{name: "s1", leader: true, current: true},
		{name: "s2", current: true},
		{name: "s3", offline: true, lag: 42},
	}
	if len(peers) != len(expected) {
		t.Fatalf("unexpected peers: got %v, expected %v", peers, expected)
	}
	for i, peer := range peers {
		if peer != expected[i] {
			t.Fatalf("unexpected peer at %d: got %v, expected %v", i, peer, expected[i])
		}
	}
}

func TestJetStreamConfig_IsPullBased(t *testing.T) {
	if !isPullBased(jetstream.ConsumerConfig{}) {
		t.Fatal("expected a consumer without a delivery subject to be pull based")
	}
	if isPullBased(jetstream.ConsumerConfig{DeliverSubject: "deliver.here"}) {
		t.Fatal("expected a consumer with a delivery subject to be push based")
	}
}

func waitFor(t *testing.T, condition func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
