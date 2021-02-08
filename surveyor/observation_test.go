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
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nuid"
	ptu "github.com/prometheus/client_golang/prometheus/testutil"

	st "github.com/nats-io/nats-surveyor/test"
)

func TestServiceObservation_Load(t *testing.T) {
	sc := st.NewSuperCluster(t)
	defer sc.Shutdown()

	opt := getTestOptions()
	obs, err := NewServiceObservation("testdata/goodobs/good.json", *opt)
	if err != nil {
		t.Fatalf("observation load error: %s", err)
	}
	obs.Stop()

	_, err = NewServiceObservation("testdata/badobs/missing.json", *opt)
	if err.Error() != "open testdata/badobs/missing.json: no such file or directory" {
		t.Fatalf("observation load error: %s", err)
	}

	_, err = NewServiceObservation("testdata/badobs/bad.json", *opt)
	if err.Error() != "invalid service observation configuration: testdata/badobs/bad.json: name is required, topic is required, jwt or nkey credentials is required" {
		t.Fatalf("observation load error: %s", err)
	}

	_, err = NewServiceObservation("testdata/badobs/badauth.json", *opt)
	if err.Error() != "nats connection failed: nats: Authorization Violation" {
		t.Fatalf("observation load error: %s", err)
	}
}

func TestServiceObservation_Handle(t *testing.T) {
	sc := st.NewSuperCluster(t)
	defer sc.Shutdown()

	opt := getTestOptions()
	obs, err := NewServiceObservation("testdata/goodobs/good.json", *opt)
	if err != nil {
		t.Fatalf("observation load error: %s", err)
	}

	err = obs.Start()
	if err != nil {
		t.Fatalf("obs could not start: %s", err)
	}
	defer obs.Stop()

	expected := `
# HELP nats_latency_observations_count Number of Service Latency listeners that are running
# TYPE nats_latency_observations_count gauge
nats_latency_observations_count 1
`
	err = ptu.CollectAndCompare(observationsGauge, bytes.NewReader([]byte(expected)))
	if err != nil {
		t.Fatalf("Invalid observations counter: %s", err)
	}

	tnc := sc.Clients[0]

	sub, err := tnc.SubscribeSync("testing.topic")
	if err != nil {
		t.Fatalf("subscribe failed: %s", err)
	}

	statusses := []int{0, 200, 400, 500}

	// send a bunch of observations
	for i := 0; i < 10; i++ {
		now := time.Now()
		observation := &server.ServiceLatency{
			TypedEvent: server.TypedEvent{
				Type: "io.nats.server.metric.v1.service_latency",
				ID:   nuid.New().Next(),
				Time: time.Now().UTC(),
			},
			Requestor: &server.ClientInfo{
				Start: &now,
				RTT:   333 * time.Second,
			},
			Responder: &server.ClientInfo{
				Name:  "testing_service",
				RTT:   time.Second,
				Start: &now,
			},
			RequestStart:   time.Now(),
			ServiceLatency: 333 * time.Microsecond,
			SystemLatency:  333 * time.Microsecond,
			Status:         statusses[i%4],
		}
		oj, err := json.Marshal(observation)
		if err != nil {
			t.Fatalf("encode error: %s", err)
		}

		err = tnc.Publish("testing.topic", oj)
		if err != nil {
			t.Fatalf("publish error: %s", err)
		}
	}

	err = tnc.Flush()
	if err != nil {
		t.Fatalf("Flush failed: %s", err)
	}

	// wait for all observations to be received in the test subscription
	for i := 0; i < 10; i++ {
		_, err = sub.NextMsg(time.Second)
		if err != nil {
			t.Fatalf("test subscriber didn't receive all published messages")
		}
	}

	// sleep a bit just in case of slower delivery to the observer
	time.Sleep(time.Second)
	expected = `
# HELP nats_latency_observations_received_count Number of observations received by this surveyor across all services
# TYPE nats_latency_observations_received_count counter
nats_latency_observations_received_count{app="testing_service",service="testing"} 10
`
	err = ptu.CollectAndCompare(observationsReceived, bytes.NewReader([]byte(expected)))
	if err != nil {
		t.Fatalf("Invalid observations counter: %s", err)
	}

	// publish some invalid observations
	for i := 0; i < 10; i++ {
		err = tnc.Publish("testing.topic", []byte{})
		if err != nil {
			t.Fatalf("publish error: %s", err)
		}
	}

	err = tnc.Flush()
	if err != nil {
		t.Fatalf("flush failed: %s", err)
	}

	for i := 0; i < 10; i++ {
		_, err = sub.NextMsg(time.Second)
		if err != nil {
			t.Fatalf("test subscriber didn't receive invalid message")
		}
	}

	time.Sleep(time.Second)

	expected = `
# HELP nats_latency_observation_error_count Number of observations received by this surveyor across all services that could not be handled
# TYPE nats_latency_observation_error_count counter
nats_latency_observation_error_count{service="testing"} 10
`
	err = ptu.CollectAndCompare(invalidObservationsReceived, bytes.NewReader([]byte(expected)))
	if err != nil {
		t.Fatalf("Invalid invalidObservationsReceived counter: %s", err)
	}

	expected = `
# HELP nats_latency_observations_received_count Number of observations received by this surveyor across all services
# TYPE nats_latency_observations_received_count counter
nats_latency_observations_received_count{app="testing_service",service="testing"} 10
`
	err = ptu.CollectAndCompare(observationsReceived, bytes.NewReader([]byte(expected)))
	if err != nil {
		t.Fatalf("Invalid observationsReceived counter: %s", err)
	}

	expected = `
# HELP nats_latency_observation_status_count The status result codes for requests to a service
# TYPE nats_latency_observation_status_count counter
nats_latency_observation_status_count{service="testing",status="200"} 3
nats_latency_observation_status_count{service="testing",status="400"} 2
nats_latency_observation_status_count{service="testing",status="500"} 5
`
	err = ptu.CollectAndCompare(serviceRequestStatus, bytes.NewReader([]byte(expected)))
	if err != nil {
		t.Fatalf("Status counter: %s", err)
	}
}
