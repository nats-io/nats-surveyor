// Copyright 2019-2023 The NATS Authors
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
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	st "github.com/nats-io/nats-surveyor/test"
	ptu "github.com/prometheus/client_golang/prometheus/testutil"
)

// Testing constants
const (
	clientCert         = "../test/certs/client-cert.pem"
	clientKey          = "../test/certs/client-key.pem"
	serverCert         = "../test/certs/server-cert.pem"
	serverKey          = "../test/certs/server-key.pem"
	caCertFile         = "../test/certs/ca.pem"
	defaultSurveyorURL = "http://127.0.0.1:7777/metrics"
)

func httpGetSecure(url string) (*http.Response, error) {
	tlsConfig := &tls.Config{}
	caCert, err := os.ReadFile(caCertFile)
	if err != nil {
		return nil, fmt.Errorf("Got error reading RootCA file: %s", err)
	}
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)
	tlsConfig.RootCAs = caCertPool

	cert, err := tls.LoadX509KeyPair(
		clientCert,
		clientKey)
	if err != nil {
		return nil, fmt.Errorf("Got error reading client certificates: %s", err)
	}
	tlsConfig.Certificates = []tls.Certificate{cert}
	transport := &http.Transport{TLSClientConfig: tlsConfig}
	httpClient := &http.Client{Transport: transport, Timeout: 30 * time.Second}
	return httpClient.Get(url)
}

func httpGet(url string) (*http.Response, error) {
	httpClient := &http.Client{Timeout: 30 * time.Second}
	return httpClient.Get(url)
}

func getTestOptions() *Options {
	o := GetDefaultOptions()
	o.Credentials = st.SystemCreds
	return o
}

// PollSurveyorEndpoint polls a surveyor endpoint for data
func PollSurveyorEndpoint(t *testing.T, url string, secure bool, expectedRc int) (string, error) {
	var resp *http.Response
	var err error

	if secure {
		resp, err = httpGetSecure(url)
	} else {
		resp, err = httpGet(url)
	}
	if err != nil {
		return "", fmt.Errorf("error from get: %v", err)
	}
	defer resp.Body.Close()

	rc := resp.StatusCode
	if rc != expectedRc {
		return "", fmt.Errorf("expected a %d response, got %d", expectedRc, rc)
	}
	if rc != 200 {
		return "", nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("got an error reading the body: %v", err)
	}
	return string(body), nil
}

func pollAndCheckDefault(t *testing.T, result string) (string, error) {
	results, err := PollSurveyorEndpoint(t, defaultSurveyorURL, false, http.StatusOK)
	if err != nil {
		return "", err
	}
	if !strings.Contains(results, result) {
		return results, fmt.Errorf("response did not have NATS data")
	}
	return results, nil
}

func TestSurveyor_Basic(t *testing.T) {
	sc := st.NewSuperCluster(t)
	defer sc.Shutdown()

	s, err := NewSurveyor(getTestOptions())
	if err != nil {
		t.Fatalf("couldn't create surveyor: %v", err)
	}
	if err = s.Start(); err != nil {
		t.Fatalf("start error: %v", err)
	}
	defer s.Stop()

	// poll and check for basic core NATS output
	output, err := pollAndCheckDefault(t, "nats_core_mem_bytes")
	if err != nil {
		t.Fatalf("poll error:  %v\n", err)
	}

	// check for route output
	if strings.Contains(output, "nats_core_route_recv_msg_count") == false {
		t.Fatalf("invalid output:  %v\n", err)
	}

	// check for gateway output
	if strings.Contains(output, "nats_core_gateway_sent_bytes") == false {
		t.Fatalf("invalid output:  %v\n", err)
	}

	// check for labels
	if strings.Contains(output, "server_name") == false {
		t.Fatalf("invalid output:  %v\n", output)
	}
	if strings.Contains(output, "server_cluster") == false {
		t.Fatalf("invalid output:  %v\n", output)
	}
	if strings.Contains(output, "server_id") == false {
		t.Fatalf("invalid output:  %v\n", output)
	}
	if strings.Contains(output, "server_gateway_name") == false {
		t.Fatalf("invalid output:  %v\n", output)
	}
	if strings.Contains(output, "server_gateway_id") == false {
		t.Fatalf("invalid output:  %v\n", output)
	}
	if strings.Contains(output, "server_route_id") == false {
		t.Fatalf("invalid output:  %v\n", output)
	}
}

func TestSurveyor_StartTwice(t *testing.T) {
	sc := st.NewSuperCluster(t)
	defer sc.Shutdown()

	s, err := NewSurveyor(getTestOptions())
	if err != nil {
		t.Fatalf("couldn't create surveyor: %v", err)
	}
	if err = s.Start(); err != nil {
		t.Fatalf("start error: %v", err)
	}
	s.Stop()
	if err = s.Start(); err != nil {
		t.Fatalf("second start error: %v", err)
	}
	s.Stop()
}

func TestSurveyor_Account(t *testing.T) {
	sc := st.NewSuperCluster(t)
	defer sc.Shutdown()

	opt := getTestOptions()
	opt.Accounts = true
	s, err := NewSurveyor(opt)
	if err != nil {
		t.Fatalf("couldn't create surveyor: %v", err)
	}
	if err = s.Start(); err != nil {
		t.Fatalf("start error: %v", err)
	}
	defer s.Stop()

	output, err := PollSurveyorEndpoint(t, "http://127.0.0.1:7777/metrics", false, http.StatusOK)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"nats_core_account_bytes_recv",
		"nats_core_account_bytes_sent",
		"nats_core_account_conn_count",
		"nats_core_account_count",
		"nats_core_account_jetstream_enabled",
		"nats_core_account_jetstream_stream_count",
		"nats_core_account_leaf_count",
		"nats_core_account_msgs_recv",
		"nats_core_account_msgs_sent",
		"nats_core_account_sub_count",
	}
	for _, m := range want {
		if !strings.Contains(output, m) {
			t.Logf("output: %s", output)
			t.Fatalf("missing: %s", m)
		}
	}
}

func TestSurveyor_Reconnect(t *testing.T) {
	ns := st.NewSingleServer(t)
	defer ns.Shutdown()

	opts := getTestOptions()
	opts.ExpectedServers = 1
	opts.PollTimeout = time.Second
	s, err := NewSurveyor(opts)
	if err != nil {
		t.Fatalf("couldn't create surveyor: %v", err)
	}
	if err = s.Start(); err != nil {
		t.Fatalf("start error: %v", err)
	}
	defer s.Stop()

	// poll and check for basic core NATS output
	_, err = pollAndCheckDefault(t, "nats")
	if err != nil {
		t.Fatalf("poll error:  %v\n", err)
	}

	// shutdown the server
	ns.Shutdown()

	time.Sleep(time.Second * 2)

	// this poll should fail...
	output, err := pollAndCheckDefault(t, "nats_core_mem_bytes")
	if strings.Contains(output, "nats_up 0") == false {
		t.Fatalf("output did not contain nats_up 0.\n====Output====\n%s", output)
	}

	// restart the server
	ns = st.NewSingleServer(t)
	defer ns.Shutdown()

	// poll and check for basic core NATS output, the next server should
	for i := 0; i < 5; i++ {
		_, err = pollAndCheckDefault(t, "nats_up 1")
		if err == nil {
			break
		}
		time.Sleep(1 * time.Second)
	}
	if err != nil {
		t.Fatalf("Reconnect failed: %v.", err)
	}
}

func TestSurveyor_ClientTLSFail(t *testing.T) {
	ns := st.StartServer(t, "../test/r1s1.conf")
	st.ConnectAndVerify(t, ns.ClientURL())
	defer ns.Shutdown()

	opts := getTestOptions()
	opts.CaFile = caCertFile
	opts.CertFile = clientCert
	opts.KeyFile = clientKey

	s, err := NewSurveyor(opts)
	if err != nil {
		t.Fatalf("couldn't create surveyor: %v", err)
	}
	err = s.Start()
	defer s.Stop()

	if err == nil {
		t.Fatalf("Connected to a server that required TLS")
	}
}

func TestSurveyor_ClientTLS(t *testing.T) {
	ns := st.StartServer(t, "../test/tls.conf")
	defer ns.Shutdown()

	opts := getTestOptions()
	opts.URLs = "127.0.0.1:4223"
	opts.CaFile = caCertFile
	opts.CertFile = clientCert
	opts.KeyFile = clientKey

	s, err := NewSurveyor(opts)
	if err != nil {
		t.Fatalf("couldn't create surveyor: %v", err)
	}
	if err = s.Start(); err != nil {
		t.Fatalf("start error: %v", err)
	}
	defer s.Stop()

	pollAndCheckDefault(t, "nats_core_mem_bytes")
}

func TestSurveyor_HTTPS(t *testing.T) {
	sc := st.NewSuperCluster(t)
	defer sc.Shutdown()

	opts := getTestOptions()
	opts.HTTPCaFile = caCertFile
	opts.HTTPCertFile = serverCert
	opts.HTTPKeyFile = serverKey

	s, err := NewSurveyor(opts)
	if err != nil {
		t.Fatalf("couldn't create surveyor: %v", err)
	}
	if err = s.Start(); err != nil {
		t.Fatalf("start error: %v", err)
	}
	defer s.Stop()

	// Check that we CANNOT connect with http
	if _, err = PollSurveyorEndpoint(t, "http://127.0.0.1:7777/metrics", false, http.StatusOK); err == nil {
		t.Fatalf("didn't receive an error")
	}
	// Check that we CAN connect with https
	if _, err = PollSurveyorEndpoint(t, "https://127.0.0.1:7777/metrics", true, http.StatusOK); err != nil {
		t.Fatalf("received unexpected error: %v", err)
	}
}

func TestSurveyor_UserPass(t *testing.T) {
	ns := st.StartBasicServer()
	defer ns.Shutdown()

	opts := getTestOptions()
	opts.HTTPUser = "colin"
	opts.HTTPPassword = "secret"
	s, err := NewSurveyor(opts)
	if err != nil {
		t.Fatalf("couldn't create surveyor: %v", err)
	}
	if err = s.Start(); err != nil {
		t.Fatalf("start error: %v", err)
	}
	defer s.Stop()

	if _, err = PollSurveyorEndpoint(t, "http://colin:secret@127.0.0.1:7777/metrics", false, http.StatusOK); err != nil {
		t.Fatalf("received unexpected error: %v", err)
	}

	if _, err = PollSurveyorEndpoint(t, defaultSurveyorURL, false, http.StatusUnauthorized); err != nil {
		t.Fatalf("received unexpected error: %v", err)
	}

	if _, err = PollSurveyorEndpoint(t, "http://garbage:badpass@127.0.0.1:7777/metrics", false, http.StatusUnauthorized); err != nil {
		t.Fatalf("received unexpected error: %v", err)
	}

	if _, err = PollSurveyorEndpoint(t, "http://colin:badpass@127.0.0.1:7777/metrics", false, http.StatusUnauthorized); err != nil {
		t.Fatalf("received unexpected error: %v", err)
	}

	if _, err = PollSurveyorEndpoint(t, "http://foo:secret@127.0.0.1:7777/metrics", false, http.StatusUnauthorized); err != nil {
		t.Fatalf("received unexpected error: %v", err)
	}
}

func TestSurveyor_NoServer(t *testing.T) {
	s, err := NewSurveyor(getTestOptions())
	if err != nil {
		t.Fatalf("couldn't create surveyor: %v", err)
	}
	err = s.Start()
	defer s.Stop()

	if err == nil {
		t.Fatalf("didn't get expected error")
	}
}

func TestSurveyor_MissingResponses(t *testing.T) {
	sc := st.NewSuperCluster(t)
	defer sc.Shutdown()

	s, err := NewSurveyor(getTestOptions())
	if err != nil {
		t.Fatalf("couldn't create surveyor: %v", err)
	}
	if err = s.Start(); err != nil {
		t.Fatalf("start error: %v", err)
	}
	defer s.Stop()

	sc.Servers[1].Shutdown()

	// poll and check for basic core NATS output
	_, err = pollAndCheckDefault(t, "nats_core_mem_bytes")
	if err != nil {
		t.Fatalf("poll error:  %v\n", err)
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
			Credentials: "../test/myuser.creds",
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
	waitForMetricUpdate(t, om, expectedObservations)

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
	waitForMetricUpdate(t, om, expectedObservations)
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
	waitForMetricUpdate(t, om, expectedObservations)

	// observation no longer exists
	err = om.Delete(deleteID)
	if err == nil {
		t.Error("Expected error; got nil")
	}
	waitForMetricUpdate(t, om, expectedObservations)
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

func waitForMetricUpdate(t *testing.T, om *ServiceObsManager, expectedObservations map[string]*ServiceObsConfig) {
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
	if err := os.Mkdir(dirName, 0700); err != nil {
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
		if err := os.WriteFile(obsPath, obsConfigJSON, 0600); err != nil {
			t.Fatalf("Error writing observation config file: %s", err)
		}

		obsConfig.ID = obsPath
		expectedObservations[obsPath] = obsConfig
		waitForMetricUpdate(t, om, expectedObservations)
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
		if err := os.WriteFile(obsPath, obsConfigJSON, 0600); err != nil {
			t.Fatalf("Error writing to file: %s", err)
		}

		obsConfig.ID = obsPath
		expectedObservations[obsPath] = obsConfig
		waitForMetricUpdate(t, om, expectedObservations)
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

		if err := os.Mkdir(fmt.Sprintf("%s/subdir", dirName), 0700); err != nil {
			t.Fatalf("Error creating subdirectory: %s", err)
		}
		time.Sleep(100 * time.Millisecond)

		obsPath := fmt.Sprintf("%s/subdir/subobs.json", dirName)

		err = os.WriteFile(obsPath, obsConfigJSON, 0600)
		if err != nil {
			t.Fatalf("Error writing observation config file: %s", err)
		}

		obsConfig.ID = obsPath
		expectedObservations[obsPath] = obsConfig
		waitForMetricUpdate(t, om, expectedObservations)

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

		if err := os.WriteFile(obsPath, obsConfigJSON, 0600); err != nil {
			t.Fatalf("Error writing observation config file: %s", err)
		}

		obsConfig.ID = obsPath
		expectedObservations[obsPath] = obsConfig
		waitForMetricUpdate(t, om, expectedObservations)

		obsConfig = &ServiceObsConfig{
			ServiceName: "testing5",
			Topic:       "testing5.topic",
			Credentials: "../test/myuser.creds",
		}
		obsConfigJSON, err = json.Marshal(obsConfig)
		if err != nil {
			t.Fatalf("marshalling error: %s", err)
		}
		if err := os.Mkdir(fmt.Sprintf("%s/subdir/nested", dirName), 0700); err != nil {
			t.Fatalf("Error creating subdirectory: %s", err)
		}
		time.Sleep(100 * time.Millisecond)

		obsPath = fmt.Sprintf("%s/subdir/nested/nested.json", dirName)
		err = os.WriteFile(obsPath, obsConfigJSON, 0600)
		if err != nil {
			t.Fatalf("Error writing observation config file: %s", err)
		}

		obsConfig.ID = obsPath
		expectedObservations[obsPath] = obsConfig
		waitForMetricUpdate(t, om, expectedObservations)
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
		if err := os.WriteFile(obsPath, obsConfigJSON, 0600); err != nil {
			t.Fatalf("Error writing to file: %s", err)
		}

		obsConfig.ID = obsPath
		expectedObservations[obsPath] = obsConfig
		waitForMetricUpdate(t, om, expectedObservations)

		// update file with invalid JSON - existing observation should not be impacted
		if err := os.WriteFile(obsPath, []byte("abc"), 0600); err != nil {
			t.Fatalf("Error writing to file: %s", err)
		}
		time.Sleep(100 * time.Millisecond)
		waitForMetricUpdate(t, om, expectedObservations)
	})

	t.Run("remove observations", func(t *testing.T) {
		// remove single observation
		obsPath := fmt.Sprintf("%s/create.json", dirName)
		if err := os.Remove(obsPath); err != nil {
			t.Fatalf("Error removing observation config: %s", err)
		}
		delete(expectedObservations, obsPath)
		waitForMetricUpdate(t, om, expectedObservations)

		// remove whole subfolder
		if err := os.RemoveAll(fmt.Sprintf("%s/subdir", dirName)); err != nil {
			t.Fatalf("Error removing subdirectory: %s", err)
		}

		delete(expectedObservations, fmt.Sprintf("%s/subdir/subobs.json", dirName))
		delete(expectedObservations, fmt.Sprintf("%s/subdir/abc.json", dirName))
		delete(expectedObservations, fmt.Sprintf("%s/subdir/nested/nested.json", dirName))
		waitForMetricUpdate(t, om, expectedObservations)

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
		if err := os.WriteFile(obsPath, obsConfigJSON, 0600); err != nil {
			t.Fatalf("Error writing observation config file: %s", err)
		}

		obsConfig.ID = obsPath
		expectedObservations[obsPath] = obsConfig
		waitForMetricUpdate(t, om, expectedObservations)
	})
}

func TestSurveyor_ConcurrentBlock(t *testing.T) {
	sc := st.NewSuperCluster(t)
	defer sc.Shutdown()

	s, err := NewSurveyor(getTestOptions())
	if err != nil {
		t.Fatalf("couldn't create surveyor: %v", err)
	}
	if err = s.Start(); err != nil {
		t.Fatalf("start error: %v", err)
	}
	defer s.Stop()

	s.statzC.polling = true
	_, err = pollAndCheckDefault(t, "nats_core_mem_bytes")
	if err == nil {
		t.Fatalf("Expected an error but none were encountered")
	}

	if err.Error() != "expected a 200 response, got 503" {
		t.Fatalf("Expected 503 error but got: %v", err)
	}
}

func TestSurveyor_NATSUserPass(t *testing.T) {
	ns := st.StartServer(t, "../test/trad.conf")
	defer ns.Shutdown()

	opts := getTestOptions()
	opts.Credentials = ""

	opts.NATSUser = "invalid_user"
	opts.NATSPassword = "password"
	s, err := NewSurveyor(opts)
	if err != nil {
		t.Fatalf("couldn't create surveyor: %v", err)
	}
	err = s.Start()
	if err == nil {
		t.Fatalf("didn't receive expected error")
	}
	if !strings.Contains(err.Error(), "Auth") {
		t.Fatalf("didn't receive expected error: %v", err)
	}
	s.Stop()

	opts.NATSUser = "sys"
	opts.NATSPassword = "password"
	s, err = NewSurveyor(opts)
	if err != nil {
		t.Fatalf("couldn't create surveyor: %v", err)
	}
	if err = s.Start(); err != nil {
		t.Fatalf("start error: %v", err)
	}
	defer s.Stop()

	if _, err = PollSurveyorEndpoint(t, "http://127.0.0.1:7777/metrics", false, http.StatusOK); err != nil {
		t.Fatalf("received unexpected error: %v", err)
	}
}
