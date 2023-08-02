// Copyright 2020-2023 The NATS Authors
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
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	st "github.com/nats-io/nats-surveyor/test"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nuid"
	"github.com/prometheus/client_golang/prometheus"
	ptu "github.com/prometheus/client_golang/prometheus/testutil"
)

func TestServiceObservation_Load(t *testing.T) {
	sc := st.NewSuperCluster(t)
	defer sc.Shutdown()

	opt := getTestOptions()
	metrics := NewServiceObservationMetrics(prometheus.NewRegistry(), nil)
	reconnectCtr := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName("nats", "survey", "nats_reconnects"),
		Help: "Number of times the surveyor reconnected to the NATS cluster",
	}, []string{"name"})
	cp := newSurveyorConnPool(opt, reconnectCtr)

	config, err := NewServiceObservationConfigFromFile("testdata/goodobs/good.json")
	if err != nil {
		t.Fatalf("observation config error: %s", err)
	}
	obs, err := newServiceObservationListener(config, cp, opt.Logger, metrics)
	if err != nil {
		t.Fatalf("observation listener error: %s", err)
	}
	err = obs.Start()
	if err != nil {
		t.Fatalf("observation start error: %s", err)
	}
	obs.Stop()

	_, err = NewServiceObservationConfigFromFile("testdata/badobs/missing.json")
	if err.Error() != "open testdata/badobs/missing.json: no such file or directory" {
		t.Fatalf("observation load error: %s", err)
	}

	_, err = NewServiceObservationConfigFromFile("testdata/badobs/bad.json")
	if err.Error() != "invalid service observation config: testdata/badobs/bad.json: name is required, topic is required" {
		t.Fatalf("observation load error: %s", err)
	}

	config, err = NewServiceObservationConfigFromFile("testdata/badobs/badauth.json")
	if err != nil {
		t.Fatalf("observation config error: %s", err)
	}
	obs, err = newServiceObservationListener(config, cp, opt.Logger, metrics)
	if err != nil {
		t.Fatalf("observation listener error: %s", err)
	}
	err = obs.Start()
	if err.Error() != "nats connection failed for id: testdata/badobs/badauth.json, service name: testing, error: nats: Authorization Violation" {
		t.Fatalf("observation load error does not match expected error: %s", err)
	}
}

func TestServiceObservation_Handle(t *testing.T) {
	sc := st.NewSuperCluster(t)
	defer sc.Shutdown()

	opt := getTestOptions()
	metrics := NewServiceObservationMetrics(prometheus.NewRegistry(), nil)
	reconnectCtr := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName("nats", "survey", "nats_reconnects"),
		Help: "Number of times the surveyor reconnected to the NATS cluster",
	}, []string{"name"})
	cp := newSurveyorConnPool(opt, reconnectCtr)

	config, err := NewServiceObservationConfigFromFile("testdata/goodobs/good.json")
	if err != nil {
		t.Fatalf("observation config error: %s", err)
	}
	obs, err := newServiceObservationListener(config, cp, opt.Logger, metrics)
	if err != nil {
		t.Fatalf("observation listener error: %s", err)
	}
	err = obs.Start()
	if err != nil {
		t.Fatalf("observation start error: %s", err)
	}
	defer obs.Stop()

	expected := `
# HELP nats_latency_observations_count Number of Service Latency listeners that are running
# TYPE nats_latency_observations_count gauge
nats_latency_observations_count 1
`
	err = ptu.CollectAndCompare(metrics.observationsGauge, bytes.NewReader([]byte(expected)))
	if err != nil {
		t.Fatalf("Invalid observations counter: %s", err)
	}

	tnc := sc.Clients[0]

	sub, err := tnc.SubscribeSync("testing.topic")
	if err != nil {
		t.Fatalf("subscribe failed: %s", err)
	}

	statuses := []int{0, 200, 400, 500}

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
			Status:         statuses[i%4],
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
nats_latency_observations_received_count{app="testing_service",service="testing",source_account=""} 10
`
	err = ptu.CollectAndCompare(metrics.observationsReceived, bytes.NewReader([]byte(expected)))
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
nats_latency_observation_error_count{service="testing",source_account=""} 10
`
	err = ptu.CollectAndCompare(metrics.invalidObservationsReceived, bytes.NewReader([]byte(expected)))
	if err != nil {
		t.Fatalf("Invalid invalidObservationsReceived counter: %s", err)
	}

	expected = `
# HELP nats_latency_observations_received_count Number of observations received by this surveyor across all services
# TYPE nats_latency_observations_received_count counter
nats_latency_observations_received_count{app="testing_service",service="testing",source_account=""} 10
`
	err = ptu.CollectAndCompare(metrics.observationsReceived, bytes.NewReader([]byte(expected)))
	if err != nil {
		t.Fatalf("Invalid observationsReceived counter: %s", err)
	}

	expected = `
# HELP nats_latency_observation_status_count The status result codes for requests to a service
# TYPE nats_latency_observation_status_count counter
nats_latency_observation_status_count{service="testing",source_account="",status="200"} 3
nats_latency_observation_status_count{service="testing",source_account="",status="400"} 2
nats_latency_observation_status_count{service="testing",source_account="",status="500"} 5
`
	err = ptu.CollectAndCompare(metrics.serviceRequestStatus, bytes.NewReader([]byte(expected)))
	if err != nil {
		t.Fatalf("Status counter: %s", err)
	}
}

func TestServiceObservation_Aggregate(t *testing.T) {
	tests := []struct {
		name          string
		obsConfigFile string
		obsConfig     *ServiceObsConfig
	}{
		{
			name:          "aggregate stream export from file",
			obsConfigFile: "testdata/aggregate_observations/aggregate_stream.json",
		},
		{
			name:          "aggregate service export from file",
			obsConfigFile: "testdata/aggregate_observations/aggregate_service.json",
		},
		{
			name: "aggregate service export from config",
			obsConfig: &ServiceObsConfig{
				ID:                           "test",
				ServiceName:                  "aggregate",
				Topic:                        "test.service.latency.ACC.*.*",
				ExternalAccountTokenPosition: 5,
				ExternalServiceNamePosition:  6,
				Username:                     "agg_service",
				Password:                     "agg_service",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			srv := st.NewServerFromConfig(t, "../test/services.conf")
			defer srv.Shutdown()

			opt := GetDefaultOptions()
			opt.URLs = srv.ClientURL()
			metrics := NewServiceObservationMetrics(prometheus.NewRegistry(), nil)
			s, err := NewSurveyor(opt)
			if err != nil {
				t.Fatalf("couldn't create surveyor: %v", err)
			}
			if err = s.Start(); err != nil {
				t.Fatalf("start error: %v", err)
			}
			defer s.Stop()
			config := test.obsConfig
			if test.obsConfigFile != "" {
				config, err = NewServiceObservationConfigFromFile(test.obsConfigFile)
				if err != nil {
					t.Fatalf("advisory config error: %s", err)
				}
			}
			obsManager := s.ServiceObservationManager()
			obsManager.metrics = metrics
			err = obsManager.Set(config)
			if err != nil {
				t.Fatalf("Error setting advisory config: %s", err)
			}
			waitForObsUpdate(t, obsManager, map[string]*ServiceObsConfig{config.ID: config})

			urlAgg := fmt.Sprintf("nats://%s:%s@", config.Username, config.Password) + strings.TrimPrefix(srv.ClientURL(), "nats://")
			urlA := "nats://a:a@" + strings.TrimPrefix(srv.ClientURL(), "nats://")
			urlB := "nats://b:b@" + strings.TrimPrefix(srv.ClientURL(), "nats://")
			ncAgg, err := nats.Connect(urlAgg)
			if err != nil {
				t.Fatalf("could not connect nats client: %s", err)
			}
			defer ncAgg.Close()
			ncA, err := nats.Connect(urlA, nats.UseOldRequestStyle(), nats.Name("testing_service"))
			if err != nil {
				t.Fatalf("could not connect nats client: %s", err)
			}
			defer ncA.Close()
			ncB, err := nats.Connect(urlB)
			if err != nil {
				t.Fatalf("could not connect nats client: %s", err)
			}
			defer ncB.Close()

			expected := `
# HELP nats_latency_observations_count Number of Service Latency listeners that are running
# TYPE nats_latency_observations_count gauge
nats_latency_observations_count 1
`
			err = ptu.CollectAndCompare(metrics.observationsGauge, bytes.NewReader([]byte(expected)))
			if err != nil {
				t.Fatalf("Invalid observations counter: %s", err)
			}

			sub, err := ncAgg.SubscribeSync("test.service.latency.>")
			if err != nil {
				t.Fatalf("subscribe failed: %s", err)
			}

			replySub, err := ncA.Subscribe("test.service", func(m *nats.Msg) {
				m.Respond([]byte("hello"))
			})
			if err != nil {
				t.Fatalf("subscribe failed: %s", err)
			}
			defer replySub.Unsubscribe()
			// send a bunch of observations
			for i := 0; i < 10; i++ {
				_, err := ncB.Request("test.service", []byte("hello"), time.Second)
				if err != nil {
					t.Fatalf("request failed: %s", err)
				}
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
	nats_latency_observations_received_count{app="testing_service",service="myservice",source_account="a"} 10
	`
			err = ptu.CollectAndCompare(metrics.observationsReceived, bytes.NewReader([]byte(expected)))
			if err != nil {
				t.Fatalf("Invalid observations counter: %s", err)
			}
		})
	}
}

func TestSurveyor_ObservationsFromFile(t *testing.T) {
	sc := st.NewSuperCluster(t)
	defer sc.Shutdown()

	opts := getTestOptions()
	opts.ObservationConfigDir = "testdata/goodobs"

	s, err := NewSurveyor(opts)
	if err != nil {
		t.Fatalf("couldn't create surveyor: %v", err)
	}
	if err = s.Start(); err != nil {
		t.Fatalf("start error: %v", err)
	}
	defer s.Stop()

	if ptu.ToFloat64(s.ServiceObservationManager().metrics.observationsGauge) != 1 {
		t.Fatalf("process error: observations not started")
	}
}

func TestSurveyor_Observations(t *testing.T) {
	sc := st.NewSuperCluster(t)
	defer sc.Shutdown()

	opts := getTestOptions()

	s, err := NewSurveyor(opts)
	if err != nil {
		t.Fatalf("couldn't create surveyor: %v", err)
	}
	if err = s.Start(); err != nil {
		t.Fatalf("start error: %v", err)
	}
	defer s.Stop()
	om := s.ServiceObservationManager()

	expectedObservations := make(map[string]*ServiceObsConfig)
	observations := []*ServiceObsConfig{
		{
			ID:          "srv1",
			ServiceName: "srv1",
			Topic:       "testing.topic",
			Credentials: "../test/myuser.creds",
			Nkey:        "",
		},
		{
			ID:          "srv2",
			ServiceName: "srv2",
			Topic:       "testing.topic",
			Credentials: "../test/myuser.creds",
		},
		{
			ID:          "srv3",
			ServiceName: "srv3",
			Topic:       "testing.topic",
			JWT:         "eyJ0eXAiOiJqd3QiLCJhbGciOiJlZDI1NTE5In0.eyJqdGkiOiJFSkZHSjZPSDVFM1FXVk5GRUpRVEVLRkZXNDZFN1RISDVTQkhXSDMyQ1pIUUg1S1g3NVRRIiwiaWF0IjoxNTcwNDg3OTEzLCJpc3MiOiJBRDZEUEFUS1laTkFCNk01V0hJUTZPWFRORFRQQldMRk02TEU3TVBNTVNUVTdGSE9OTUNTUlJWVCIsIm5hbWUiOiJteXVzZXIiLCJzdWIiOiJVRDNPR1BPSVJUMjJVQUo3Nk9EUjJDRVFST0RHT1g2UVpZUldIMkw2Tk40VzRaNEhPNUpZTkNYMiIsInR5cGUiOiJ1c2VyIiwibmF0cyI6eyJwdWIiOnsiYWxsb3ciOlsiXHUwMDNlIl19LCJzdWIiOnsiYWxsb3ciOlsiXHUwMDNlIl19fX0.zmWzJIC8113VpwHjyJeg8gmOj1rceUIqvfFBlRFq62UBB08PjCe8yYjfWl-J_Enf8xGnv-ipvtEPxOxblkA8DQ",
			Seed:        "SUAEVSBKQ25JOR3JVVFZKQKZ7WGCYNEGYDJK7US76D2KUXNQSMK57SW2JU",
		},
	}

	obsIDs := make([]string, 0)
	for _, obs := range observations {
		err := om.Set(obs)
		if err != nil {
			t.Errorf("Unexpected error on observation set: %s", err)
		}
		obsIDs = append(obsIDs, obs.ID)
		expectedObservations[obs.ID] = obs
	}
	waitForObsUpdate(t, om, expectedObservations)

	setObservation := &ServiceObsConfig{
		ID:          obsIDs[0],
		ServiceName: "srv4",
		Topic:       "testing_updated.topic",
		Credentials: "../test/myuser.creds",
	}
	expectedObservations[obsIDs[0]] = setObservation
	err = om.Set(setObservation)
	if err != nil {
		t.Errorf("Unexpected error on observation set: %s", err)
	}
	waitForObsUpdate(t, om, expectedObservations)
	var found bool
	obsMap := om.ConfigMap()
	for _, obs := range obsMap {
		if obs.ServiceName == "srv4" {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("Expected updated service name in observations: %s", "srv4")
	}
	deleteID := obsIDs[0]
	err = om.Delete(deleteID)
	delete(expectedObservations, deleteID)
	if err != nil {
		t.Errorf("Unexpected error on observation delete request: %s", err)
	}
	waitForObsUpdate(t, om, expectedObservations)

	// observation no longer exists
	err = om.Delete(deleteID)
	if err == nil {
		t.Error("Expected error; got nil")
	}
	waitForObsUpdate(t, om, expectedObservations)
}

func TestSurveyor_ObservationsError(t *testing.T) {
	sc := st.NewSuperCluster(t)
	defer sc.Shutdown()

	opts := getTestOptions()

	s, err := NewSurveyor(opts)
	if err != nil {
		t.Fatalf("couldn't create surveyor: %v", err)
	}
	if err = s.Start(); err != nil {
		t.Fatalf("start error: %v", err)
	}
	defer s.Stop()
	om := s.ServiceObservationManager()
	if err != nil {
		t.Fatalf("Error creating observations manager: %s", err)
	}

	// add invalid observation (missing service name)
	err = om.Set(
		&ServiceObsConfig{
			ID:          "id",
			ServiceName: "",
			Topic:       "testing.topic",
			Credentials: "../test/myuser.creds",
		},
	)

	if err == nil {
		t.Errorf("Expected error; got nil")
	}

	// valid observation, no error
	err = om.Set(
		&ServiceObsConfig{
			ID:          "id",
			ServiceName: "srv",
			Topic:       "testing.topic",
			Credentials: "../test/myuser.creds",
		},
	)

	if err != nil {
		t.Errorf("Expected no error; got: %s", err)
	}

	// update error, invalid config
	err = om.Set(
		&ServiceObsConfig{
			ID:          "srv",
			ServiceName: "srv",
			Topic:       "",
			Credentials: "../test/myuser.creds",
		},
	)

	if err == nil {
		t.Errorf("Expected error; got nil")
	}
}

func waitForObsUpdate(t *testing.T, om *ServiceObsManager, expectedObservations map[string]*ServiceObsConfig) {
	t.Helper()
	ticker := time.NewTicker(50 * time.Millisecond)
	timeout := time.After(5 * time.Second)
	defer ticker.Stop()
Outer:
	for {
		select {
		case <-ticker.C:
			observationsNum := ptu.ToFloat64(om.metrics.observationsGauge)
			if observationsNum == float64(len(expectedObservations)) {
				break Outer
			}
		case <-timeout:
			observationsNum := ptu.ToFloat64(om.metrics.observationsGauge)
			t.Fatalf("process error: invalid number of observations; want: %d; got: %f\n", len(expectedObservations), observationsNum)
			return
		}
	}

	existingObservations := om.ConfigMap()
	if len(existingObservations) != len(expectedObservations) {
		t.Fatalf("Unexpected number of observations; want: %d; got: %d", len(expectedObservations), len(existingObservations))
	}
	for _, existingObservation := range existingObservations {
		obs, ok := expectedObservations[existingObservation.ID]
		if !ok {
			t.Fatalf("Missing observation with ID: %s", existingObservation.ID)
		}
		if !reflect.DeepEqual(obs, existingObservation) {
			t.Fatalf("Invalid observation config; want: %+v; got: %+v", obs, existingObservation)
		}
	}
}

func TestSurveyor_ObservationsWatcher(t *testing.T) {
	sc := st.NewSuperCluster(t)
	defer sc.Shutdown()

	opts := getTestOptions()

	dirName := fmt.Sprintf("testdata/obs%d", time.Now().UnixNano())
	if err := os.Mkdir(dirName, 0o700); err != nil {
		t.Fatalf("Error creating observations dir: %s", err)
	}
	defer os.RemoveAll(dirName)
	opts.ObservationConfigDir = dirName

	s, err := NewSurveyor(opts)
	if err != nil {
		t.Fatalf("couldn't create surveyor: %v", err)
	}
	if err = s.Start(); err != nil {
		t.Fatalf("start error: %v", err)
	}
	defer s.Stop()
	time.Sleep(200 * time.Millisecond)

	om := s.ServiceObservationManager()
	expectedObservations := make(map[string]*ServiceObsConfig)

	t.Run("write observation file - create operation", func(t *testing.T) {
		obsConfig := &ServiceObsConfig{
			ServiceName: "testing1",
			Topic:       "testing1.topic",
			Credentials: "../test/myuser.creds",
		}
		obsConfigJSON, err := json.Marshal(obsConfig)
		if err != nil {
			t.Fatalf("marshalling error: %s", err)
		}
		obsPath := fmt.Sprintf("%s/create.json", dirName)
		if err := os.WriteFile(obsPath, obsConfigJSON, 0o600); err != nil {
			t.Fatalf("Error writing observation config file: %s", err)
		}

		obsConfig.ID = obsPath
		expectedObservations[obsPath] = obsConfig
		waitForObsUpdate(t, om, expectedObservations)
	})

	t.Run("first create then write to file - write operation", func(t *testing.T) {
		obsConfig := &ServiceObsConfig{
			ServiceName: "testing2",
			Topic:       "testing2.topic",
			Credentials: "../test/myuser.creds",
		}
		obsConfigJSON, err := json.Marshal(obsConfig)
		if err != nil {
			t.Fatalf("marshalling error: %s", err)
		}
		obsPath := fmt.Sprintf("%s/write.json", dirName)
		f, err := os.Create(obsPath)
		if err != nil {
			t.Fatalf("Error writing observation config file: %s", err)
		}
		if err := f.Close(); err != nil {
			t.Fatalf("Error closing file: %s", err)
		}
		time.Sleep(200 * time.Millisecond)
		if err := os.WriteFile(obsPath, obsConfigJSON, 0o600); err != nil {
			t.Fatalf("Error writing to file: %s", err)
		}

		obsConfig.ID = obsPath
		expectedObservations[obsPath] = obsConfig
		waitForObsUpdate(t, om, expectedObservations)
	})

	t.Run("create observations in subfolder", func(t *testing.T) {
		obsConfig := &ServiceObsConfig{
			ServiceName: "testing3",
			Topic:       "testing3.topic",
			Credentials: "../test/myuser.creds",
		}
		obsConfigJSON, err := json.Marshal(obsConfig)
		if err != nil {
			t.Fatalf("marshalling error: %s", err)
		}

		if err := os.Mkdir(fmt.Sprintf("%s/subdir", dirName), 0o700); err != nil {
			t.Fatalf("Error creating subdirectory: %s", err)
		}
		time.Sleep(100 * time.Millisecond)

		obsPath := fmt.Sprintf("%s/subdir/subobs.json", dirName)

		err = os.WriteFile(obsPath, obsConfigJSON, 0o600)
		if err != nil {
			t.Fatalf("Error writing observation config file: %s", err)
		}

		obsConfig.ID = obsPath
		expectedObservations[obsPath] = obsConfig
		waitForObsUpdate(t, om, expectedObservations)

		obsConfig = &ServiceObsConfig{
			ServiceName: "testing4",
			Topic:       "testing4.topic",
			Credentials: "../test/myuser.creds",
		}
		obsConfigJSON, err = json.Marshal(obsConfig)
		if err != nil {
			t.Fatalf("marshalling error: %s", err)
		}
		obsPath = fmt.Sprintf("%s/subdir/abc.json", dirName)

		if err := os.WriteFile(obsPath, obsConfigJSON, 0o600); err != nil {
			t.Fatalf("Error writing observation config file: %s", err)
		}

		obsConfig.ID = obsPath
		expectedObservations[obsPath] = obsConfig
		waitForObsUpdate(t, om, expectedObservations)

		obsConfig = &ServiceObsConfig{
			ServiceName: "testing5",
			Topic:       "testing5.topic",
			Credentials: "../test/myuser.creds",
		}
		obsConfigJSON, err = json.Marshal(obsConfig)
		if err != nil {
			t.Fatalf("marshalling error: %s", err)
		}
		if err := os.Mkdir(fmt.Sprintf("%s/subdir/nested", dirName), 0o700); err != nil {
			t.Fatalf("Error creating subdirectory: %s", err)
		}
		time.Sleep(100 * time.Millisecond)

		obsPath = fmt.Sprintf("%s/subdir/nested/nested.json", dirName)
		err = os.WriteFile(obsPath, obsConfigJSON, 0o600)
		if err != nil {
			t.Fatalf("Error writing observation config file: %s", err)
		}

		obsConfig.ID = obsPath
		expectedObservations[obsPath] = obsConfig
		waitForObsUpdate(t, om, expectedObservations)
	})

	t.Run("update observations", func(t *testing.T) {
		obsConfig := &ServiceObsConfig{
			ServiceName: "testing_updated",
			Topic:       "testing_updated.topic",
			Credentials: "../test/myuser.creds",
		}
		obsConfigJSON, err := json.Marshal(obsConfig)
		if err != nil {
			t.Fatalf("marshalling error: %s", err)
		}

		obsPath := fmt.Sprintf("%s/write.json", dirName)
		if err := os.WriteFile(obsPath, obsConfigJSON, 0o600); err != nil {
			t.Fatalf("Error writing to file: %s", err)
		}

		obsConfig.ID = obsPath
		expectedObservations[obsPath] = obsConfig
		waitForObsUpdate(t, om, expectedObservations)

		// update file with invalid JSON - existing observation should not be impacted
		if err := os.WriteFile(obsPath, []byte("abc"), 0o600); err != nil {
			t.Fatalf("Error writing to file: %s", err)
		}
		time.Sleep(100 * time.Millisecond)
		waitForObsUpdate(t, om, expectedObservations)
	})

	t.Run("remove observations", func(t *testing.T) {
		// remove single observation
		obsPath := fmt.Sprintf("%s/create.json", dirName)
		if err := os.Remove(obsPath); err != nil {
			t.Fatalf("Error removing observation config: %s", err)
		}
		delete(expectedObservations, obsPath)
		waitForObsUpdate(t, om, expectedObservations)

		// remove whole subfolder
		if err := os.RemoveAll(fmt.Sprintf("%s/subdir", dirName)); err != nil {
			t.Fatalf("Error removing subdirectory: %s", err)
		}

		delete(expectedObservations, fmt.Sprintf("%s/subdir/subobs.json", dirName))
		delete(expectedObservations, fmt.Sprintf("%s/subdir/abc.json", dirName))
		delete(expectedObservations, fmt.Sprintf("%s/subdir/nested/nested.json", dirName))
		waitForObsUpdate(t, om, expectedObservations)

		obsConfig := &ServiceObsConfig{
			ServiceName: "testing10",
			Topic:       "testing1.topic",
			Credentials: "../test/myuser.creds",
		}
		obsConfigJSON, err := json.Marshal(obsConfig)
		if err != nil {
			t.Fatalf("marshalling error: %s", err)
		}

		obsPath = fmt.Sprintf("%s/another.json", dirName)
		if err := os.WriteFile(obsPath, obsConfigJSON, 0o600); err != nil {
			t.Fatalf("Error writing observation config file: %s", err)
		}

		obsConfig.ID = obsPath
		expectedObservations[obsPath] = obsConfig
		waitForObsUpdate(t, om, expectedObservations)
	})
}
