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
	"testing"
	"time"

	server "github.com/nats-io/nats-server/v2/server"
	st "github.com/nats-io/nats-surveyor/test"
	ptu "github.com/prometheus/client_golang/prometheus/testutil"
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
	if err.Error() != "invalid service observation configuration: testdata/badobs/bad.json: name is required, topic is required, credential is required" {
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
	defer obs.Stop()
	err = obs.Start()
	if err != nil {
		t.Fatalf("obs could not start: %s", err)
	}

	// create a test subscriber as to approximate when the observer is ready.
	sub, _ := sc.Clients[0].SubscribeSync("testing.topic")

	// send a bunch of observations
	for i := 0; i < 10; i++ {
		observation := &server.ServiceLatency{
			AppName:      "testing",
			RequestStart: time.Now(),
			TotalLatency: time.Second,
			NATSLatency: server.NATSLatency{
				Requestor: 333 * time.Microsecond,
				Responder: 333 * time.Microsecond,
				System:    333 * time.Microsecond,
			},
		}
		oj, err := json.Marshal(observation)
		if err != nil {
			t.Fatalf("encode error: %s", err)
		}

		err = sc.Clients[0].Publish("testing.topic", oj)
		if err != nil {
			t.Fatalf("publish error: %s", err)
		}
	}

	sc.Clients[0].Flush()

	// wait for all observations to be received in the test subscription
	for i := 0; i < 10; i++ {
		_, err = sub.NextMsg(time.Second)
		if err != nil {
			t.Fatalf("test subscriber didn't receive all published messages")
		}
	}

	// sleep a bit just in case of slower delivery to the observer
	time.Sleep(250 * time.Microsecond)
	if ptu.ToFloat64(observationsReceived) == 0.0 {
		t.Fatalf("did not receive observations")
	}

	// publish some invalid observations
	for i := 0; i < 10; i++ {
		err = sc.Clients[0].Publish("testing.topic", []byte{})
		if err != nil {
			t.Fatalf("publish error: %s", err)
		}
	}

	sc.Clients[0].Flush()

	for i := 0; i < 10; i++ {
		// wait for the invalid observations to arrive
		_, err = sub.NextMsg(time.Second)
		if err != nil {
			t.Fatalf("test subscriber didn't receive invalid message")
		}
	}

	time.Sleep(250 * time.Microsecond)
	if ptu.ToFloat64(invalidObservationsReceived) == 0.0 {
		t.Fatalf("did not receive invalid observation")
	}
}
