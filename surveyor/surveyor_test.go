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
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	st "github.com/nats-io/nats-surveyor/test"
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
	tlsConfig, err := parseTLSConfig(clientCert, clientKey, caCertFile)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{TLSClientConfig: tlsConfig}
	httpClient := &http.Client{Transport: transport, Timeout: 30 * time.Second}
	return httpClient.Get(url)
}

func parseTLSConfig(certFile, keyFile, caFile string) (*tls.Config, error) {
	tlsConfig := &tls.Config{}
	caCert, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("Got error reading RootCA file: %s", err)
	}
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)
	tlsConfig.RootCAs = caCertPool

	cert, err := tls.LoadX509KeyPair(
		certFile,
		keyFile)
	if err != nil {
		return nil, fmt.Errorf("Got error reading client certificates: %s", err)
	}
	tlsConfig.Certificates = []tls.Certificate{cert}
	return tlsConfig, nil
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
	t.Helper()
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

func pollAndCheckDefault(t *testing.T, result string) string {
	t.Helper()
	results, err := PollSurveyorEndpoint(t, defaultSurveyorURL, false, http.StatusOK)
	if err != nil {
		t.Fatalf("Error polling surveyor: %s", err)
	}
	if !strings.Contains(results, result) {
		t.Fatalf("Response does not contain expected data: %s", result)
	}
	return results
}

func TestSurveyor_Basic(t *testing.T) {
	sc := st.NewSuperCluster(t)
	defer sc.Shutdown()
	checkOutput := func(t *testing.T, output string) {
		t.Helper()
		// check for route output
		if !strings.Contains(output, "nats_core_route_recv_msg_count") {
			t.Fatalf("invalid output, missing 'nats_core_route_recv_msg_count':  %v\n", output)
		}
		// check for gateway output
		if !strings.Contains(output, "nats_core_gateway_sent_bytes") {
			t.Fatalf("invalid output, missing 'nats_core_gateway_sent_bytes':  %v\n", output)
		}
		if !strings.Contains(output, "server_name") {
			t.Fatalf("invalid output, missing 'server_name':  %v\n", output)
		}
		if !strings.Contains(output, "server_cluster") {
			t.Fatalf("invalid output, missing 'server_cluster':  %v\n", output)
		}
		if !strings.Contains(output, "server_id") {
			t.Fatalf("invalid output, missing 'server_id':  %v\n", output)
		}
		if !strings.Contains(output, "server_gateway_name") {
			t.Fatalf("invalid output, missing 'server_gateway_name':  %v\n", output)
		}
		if !strings.Contains(output, "server_gateway_id") {
			t.Fatalf("invalid output, missing 'server_gateway_id':  %v\n", output)
		}
		if !strings.Contains(output, "server_route_id") {
			t.Fatalf("invalid output, missing 'server_route_id':  %v\n", output)
		}
		if !strings.Contains(output, "nats_survey_surveyed_count 3") {
			t.Fatalf("invalid output, missing 'nats_survey_surveyed_count 3':  %v\n", output)
		}
	}

	testOpts := getTestOptions()
	t.Run("with 3 expected servers", func(t *testing.T) {
		testOpts.ExpectedServers = 3
		s, err := NewSurveyor(testOpts)
		if err != nil {
			t.Fatalf("couldn't create surveyor: %v", err)
		}
		if err = s.Start(); err != nil {
			t.Fatalf("start error: %v", err)
		}
		defer s.Stop()

		// poll and check for basic core NATS output
		output := pollAndCheckDefault(t, "nats_core_mem_bytes")

		checkOutput(t, output)
	})

	t.Run("with unlimited expected servers", func(t *testing.T) {
		testOpts.ExpectedServers = -1
		testOpts.ServerResponseWait = 100 * time.Millisecond
		s, err := NewSurveyor(testOpts)
		if err != nil {
			t.Fatalf("couldn't create surveyor: %v", err)
		}
		if err = s.Start(); err != nil {
			t.Fatalf("start error: %v", err)
		}
		defer s.Stop()

		// poll and check for basic core NATS output
		output := pollAndCheckDefault(t, "nats_core_mem_bytes")

		if !strings.Contains(output, "nats_survey_expected_count -1") {
			t.Fatalf("invalid output:  %v\n", output)
		}
		checkOutput(t, output)
	})
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
	opt.ExpectedServers = 3
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

func TestSurveyor_AccountJetStreamAssets(t *testing.T) {
	sc := st.NewJetStreamCluster(t)
	defer sc.Shutdown()

	opt := getTestOptions()
	opt.Credentials = ""
	opt.NATSUser = "admin"
	opt.NATSPassword = "s3cr3t!"
	opt.Accounts = true
	opt.ExpectedServers = 3
	s, err := NewSurveyor(opt)
	if err != nil {
		t.Fatalf("couldn't create surveyor: %v", err)
	}
	if err = s.Start(); err != nil {
		t.Fatalf("start error: %v", err)
	}
	defer s.Stop()

	nc := sc.Clients[0]
	js, err := nc.JetStream()
	if err != nil {
		t.Fatalf("Error creating JetStream context: %s", err)
	}
	// create 10 streams, half of them with replicas
	for i := 0; i < 5; i++ {
		_, err = js.AddStream(&nats.StreamConfig{Name: fmt.Sprintf("single%d", i), Subjects: []string{fmt.Sprintf("SINGLE.%d", i)}})
		if err != nil {
			t.Fatalf("Error adding stream: %s", err)
		}
		_, err = js.AddStream(&nats.StreamConfig{Name: fmt.Sprintf("repl%d", i), Subjects: []string{fmt.Sprintf("REPL.%d", i)}, Replicas: 3})
		if err != nil {
			t.Fatalf("Error adding stream: %s", err)
		}
	}

	// create 15 consumers, 3 variants
	for i := 0; i < 5; i++ {
		// non-replicated consumer on non-replicated stream
		_, err = js.AddConsumer("single1", &nats.ConsumerConfig{Durable: fmt.Sprintf("singlecons_%d", i)})
		if err != nil {
			t.Fatalf("Error adding consumer: %s", err)
		}
		// consumer with replicas on stream with replicas
		_, err = js.AddConsumer("repl1", &nats.ConsumerConfig{Durable: fmt.Sprintf("replcons_%d", i), Replicas: 3})
		if err != nil {
			t.Fatalf("Error adding consumer: %s", err)
		}
		// non-replicated consumer on stream with replicas
		_, err = js.AddConsumer("repl2", &nats.ConsumerConfig{Durable: fmt.Sprintf("singleonrepl_%d", i), Replicas: 1})
		if err != nil {
			t.Fatalf("Error adding consumer: %s", err)
		}
	}

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
		`nats_core_account_jetstream_stream_count{account="JS"} 10`,
		`nats_core_account_jetstream_consumer_count{account="JS",stream="repl1"} 5`,
		`nats_core_account_jetstream_consumer_count{account="JS",stream="repl2"} 5`,
		`nats_core_account_jetstream_consumer_count{account="JS",stream="single1"} 5`,
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
	pollAndCheckDefault(t, "nats")
	if err != nil {
		t.Fatalf("poll error:  %v\n", err)
	}

	// shutdown the server
	ns.Shutdown()

	time.Sleep(time.Second * 2)

	pollAndCheckDefault(t, "nats_up 0")

	// restart the server
	ns = st.NewSingleServer(t)
	defer ns.Shutdown()

	// poll and check for basic core NATS output, the next server should
	for i := 0; i < 5; i++ {
		results, err := PollSurveyorEndpoint(t, defaultSurveyorURL, false, http.StatusOK)
		if err == nil || strings.Contains(results, "nats_up 1") {
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
	st.ConnectAndVerify(t, ns.ClientURL(), nats.UserCredentials("../test/myuser.creds"))
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
	t.Run("pass cert and key files", func(t *testing.T) {
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
	})
	t.Run("pass tls config", func(t *testing.T) {
		tlsConfig, err := parseTLSConfig(clientCert, clientKey, caCertFile)
		if err != nil {
			t.Fatalf("Error parsing TLS config: %s", err)
		}

		opts := getTestOptions()
		opts.URLs = "127.0.0.1:4223"
		opts.NATSOpts = []nats.Option{nats.Secure(tlsConfig)}

		s, err := NewSurveyor(opts)
		if err != nil {
			t.Fatalf("couldn't create surveyor: %v", err)
		}
		if err = s.Start(); err != nil {
			t.Fatalf("start error: %v", err)
		}
		defer s.Stop()

		pollAndCheckDefault(t, "nats_core_mem_bytes")
	})
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
	if _, err = PollSurveyorEndpoint(t, defaultSurveyorURL, false, http.StatusOK); err == nil {
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

	testOpts := getTestOptions()

	t.Run("with 3 expected servers", func(t *testing.T) {
		testOpts.ExpectedServers = 3
		testOpts.PollTimeout = 300 * time.Millisecond
		s, err := NewSurveyor(testOpts)
		if err != nil {
			t.Fatalf("couldn't create surveyor: %v", err)
		}
		if err = s.Start(); err != nil {
			t.Fatalf("start error: %v", err)
		}
		defer s.Stop()
		output := pollAndCheckDefault(t, "nats_core_mem_bytes")
		if !strings.Contains(output, `nats_survey_surveyed_count 3`) {
			t.Fatalf("invalid output:  %v\n", output)
		}
		if strings.Contains(output, `nats_survey_no_replies_count`) {
			t.Fatalf("invalid output:  %v\n", output)
		}

		sc.Servers[2].Shutdown()

		// poll and check for basic core NATS output
		output = pollAndCheckDefault(t, "nats_core_mem_bytes")
		if !strings.Contains(output, `nats_survey_surveyed_count 2`) {
			t.Fatalf("invalid output:  %v\n", output)
		}
		// expect missing servers reported
		if !strings.Contains(output, `nats_survey_no_replies_count{expected="3"} 1`) {
			t.Fatalf("invalid output:  %v\n", output)
		}
	})

	t.Run("with unlimited expected servers", func(t *testing.T) {
		testOpts.ExpectedServers = -1
		testOpts.ServerResponseWait = 100 * time.Millisecond

		s, err := NewSurveyor(testOpts)
		if err != nil {
			t.Fatalf("couldn't create surveyor: %v", err)
		}
		if err = s.Start(); err != nil {
			t.Fatalf("start error: %v", err)
		}
		defer s.Stop()
		output := pollAndCheckDefault(t, "nats_core_mem_bytes")
		if !strings.Contains(output, `nats_survey_surveyed_count 2`) {
			t.Fatalf("invalid output:  %v\n", output)
		}
		if strings.Contains(output, `nats_survey_no_replies_count`) {
			t.Fatalf("invalid output:  %v\n", output)
		}

		sc.Servers[1].Shutdown()

		// poll and check for basic core NATS output
		output = pollAndCheckDefault(t, "nats_core_mem_bytes")
		if !strings.Contains(output, `nats_survey_surveyed_count 1`) {
			t.Fatalf("invalid output:  %v\n", output)
		}
		// expect missing servers reported
		if strings.Contains(output, `nats_survey_no_replies_count`) {
			t.Fatalf("invalid output:  %v\n", output)
		}
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
	_, err = PollSurveyorEndpoint(t, defaultSurveyorURL, false, http.StatusOK)
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
