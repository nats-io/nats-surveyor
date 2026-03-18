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

// Package test has test functions for the surveyor
package test

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/logger"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

// Test variables
var (
	SystemCreds = "../test/SYS.creds"
)

func resetPreviousHTTPConnections() {
	http.DefaultTransport = &http.Transport{}
}

// StartBasicServer runs the NATS server with a monitor port in a go routine
func StartBasicServer() *server.Server {
	resetPreviousHTTPConnections()

	// To enable debug/trace output in the NATS server,
	// flip the enableLogging flag.
	// enableLogging = true

	opts := &server.Options{
		Host:     "127.0.0.1",
		Port:     -1,
		HTTPHost: "127.0.0.1",
		HTTPPort: -1,
		NoLog:    true,
		NoSigs:   true,
	}

	s, err := server.NewServer(opts)
	if err != nil {
		panic(err)
	}

	if s == nil {
		panic("No NATS Server object returned.")
	}

	if !opts.NoLog {
		l := logger.NewStdLogger(true, true, true, false, true)
		s.SetLogger(l, true, true)
	}

	// Run server in Go routine.
	go s.Start()

	end := time.Now().Add(15 * time.Second)
	for time.Now().Before(end) {
		netAddr := s.Addr()
		if netAddr == nil {
			continue
		}
		addr := s.Addr().String()
		if addr == "" {
			time.Sleep(10 * time.Millisecond)
			// Retry. We might take a little while to open a connection.
			continue
		}
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			// Retry after 50ms
			time.Sleep(50 * time.Millisecond)
			continue
		}
		_ = conn.Close() // nolint

		// Wait a bit to give a chance to the server to remove this
		// "client" from its state, which may otherwise interfere with
		// some tests.
		time.Sleep(25 * time.Millisecond)

		return s
	}
	panic("Unable to start NATS Server in Go Routine")
}

// startServerFromOpts starts a NATS server from pre-configured options.
func startServerFromOpts(t *testing.T, opts *server.Options) *server.Server {
	t.Helper()
	resetPreviousHTTPConnections()

	opts.NoLog = true

	s, err := server.NewServer(opts)
	if err != nil || s == nil {
		t.Fatalf("No NATS Server object returned: %v", err)
	}

	if !opts.NoLog {
		s.ConfigureLogger()
	}

	go s.Start()

	if !s.ReadyForConnections(10 * time.Second) {
		t.Fatal("Unable to start NATS Server in Go Routine")
	}
	return s
}

// StartServer starts a NATS server with all ports randomized.
// For clustered/gateway setups where servers must find each other, use NewSuperCluster instead.
func StartServer(t *testing.T, confFile string) *server.Server {
	t.Helper()
	opts, err := server.ProcessConfigFile(confFile)
	if err != nil {
		t.Fatalf("Error processing config file: %v", err)
	}

	// Use random ports to avoid conflicts with other tests or locally
	// running servers.
	opts.Port = -1
	opts.HTTPPort = -1
	if opts.Cluster.Port > 0 {
		opts.Cluster.Port = -1
		opts.Cluster.NoAdvertise = true
	}
	if opts.Gateway.Name != "" {
		opts.Gateway.Port = -1
	}

	return startServerFromOpts(t, opts)
}

// SuperCluster holds client connections and NATS servers
type SuperCluster struct {
	Servers []*server.Server
	Clients []*nats.Conn
}

var (
	configFiles          = []string{"../test/r1s1.conf", "../test/r1s2.conf", "../test/r2s1.conf"}
	jetStreamConfigFiles = []string{"../test/js1.conf", "../test/js2.conf", "../test/js3.conf"}
)

// NewSuperCluster creates a small supercluster for testing, with one client per server.
// All ports are randomized; routes and gateways are wired dynamically using actual ports.
func NewSuperCluster(t *testing.T) *SuperCluster {
	t.Helper()
	sc := &SuperCluster{}

	// Load all configs and randomize all ports.
	allOpts := make([]*server.Options, len(configFiles))
	for i, f := range configFiles {
		opts, err := server.ProcessConfigFile(f)
		if err != nil {
			t.Fatalf("Error processing config file: %v", err)
		}
		opts.Port = -1
		opts.HTTPPort = -1
		opts.Cluster.Port = -1
		opts.Gateway.Port = -1
		// Clear pre-configured routes/gateways; we'll wire them dynamically.
		opts.Routes = nil
		opts.Gateway.Gateways = nil
		allOpts[i] = opts
	}

	// Start r1s1 (seed for region1 cluster and gateway).
	s0 := startServerFromOpts(t, allOpts[0])
	sc.Servers = append(sc.Servers, s0)

	// Start r1s2 with route pointing to r1s1's cluster port.
	allOpts[1].Routes = server.RoutesFromStr(fmt.Sprintf("nats://127.0.0.1:%d", s0.ClusterAddr().Port))
	s1 := startServerFromOpts(t, allOpts[1])
	sc.Servers = append(sc.Servers, s1)

	// Start r2s1 with gateways pointing to region1's actual gateway ports.
	allOpts[2].Gateway.Gateways = []*server.RemoteGatewayOpts{
		{
			Name: "region1",
			URLs: []*url.URL{
				{Host: fmt.Sprintf("127.0.0.1:%d", s0.GatewayAddr().Port)},
				{Host: fmt.Sprintf("127.0.0.1:%d", s1.GatewayAddr().Port)},
			},
		},
	}
	s2 := startServerFromOpts(t, allOpts[2])
	sc.Servers = append(sc.Servers, s2)

	sc.setupClientsAndVerify(t)
	return sc
}

// NewSingleServer creates a single NATS server with a system account
func NewSingleServer(t *testing.T) *server.Server {
	s := StartServer(t, "../test/r1s1.conf")
	ConnectAndVerify(t, s.ClientURL(), nats.UserCredentials("../test/myuser.creds"))
	return s
}

// NewJetStreamServer creates a single NATS server with JetStream enabled globally
func NewJetStreamServer(t *testing.T) *server.Server {
	s := StartServer(t, "../test/jetstream.conf")
	ConnectAndVerify(t, s.ClientURL())
	return s
}

// NewServerFromConfig creates a single NATS server using provided config file
func NewServerFromConfig(t *testing.T, configFile string) *server.Server {
	s := StartServer(t, configFile)
	ConnectAndVerify(t, s.ClientURL())
	return s
}

func NewJetStreamCluster(t *testing.T) *SuperCluster {
	t.Helper()
	cluster := SuperCluster{
		Servers: make([]*server.Server, 0),
	}

	// JetStream cluster needs RAFT quorum, so all servers must know about
	// each other upfront. Keep cluster ports from configs (fixed) and only
	// randomize client/HTTP ports.
	for _, confFile := range jetStreamConfigFiles {
		srv := StartJetStreamServerFromConfig(t, confFile)
		cluster.Servers = append(cluster.Servers, srv)
	}
	for _, s := range cluster.Servers {
		nc, err := nats.Connect(s.ClientURL())
		if err != nil {
			t.Fatalf("Unable to connect to server %q: %s", s.Name(), err)
		}

		// wait until JetStream is ready
		timeout := time.Now().Add(10 * time.Second)
		for time.Now().Before(timeout) {
			jsm, err := nc.JetStream()
			if err != nil {
				t.Fatal(err)
			}
			_, err = jsm.AccountInfo()
			if err != nil {
				time.Sleep(500 * time.Millisecond)
			}
			break
		}
		if err != nil {
			t.Fatalf("Unexpected error creating stream: %v", err)
		}
		cluster.Clients = append(cluster.Clients, nc)
	}
	return &cluster
}

// StartJetStreamServerFromConfig starts a JetStream server using the provided configuration.
// StoreDir from config will not be used - instead, a tmp directory will be created for the test.
// Cluster ports are kept from config for RAFT quorum formation; only client/HTTP ports are randomized.
func StartJetStreamServerFromConfig(t *testing.T, confFile string) *server.Server {
	t.Helper()
	opts, err := server.ProcessConfigFile(confFile)
	if err != nil {
		t.Fatalf("Error processing config file: %v", err)
	}

	opts.Port = -1
	opts.HTTPPort = -1
	opts.NoLog = true
	tdir, err := os.MkdirTemp(os.TempDir(), fmt.Sprintf("%s_%s-", opts.ServerName, opts.Cluster.Name))
	if err != nil {
		t.Fatalf("Error creating jetstream store directory: %s", err)
	}
	opts.StoreDir = tdir

	s, err := server.NewServer(opts)
	if err != nil {
		t.Fatalf("Error creating server: %s", err)
	}

	s.Start()

	if !s.ReadyForConnections(10 * time.Second) {
		t.Fatalf("Unable to start JetStream server %q", opts.ServerName)
	}
	return s
}

// ClientURL returns the client URL of the first server in the cluster.
func (sc *SuperCluster) ClientURL() string {
	return sc.Servers[0].ClientURL()
}

// NewSingleServerOnPort creates a single NATS server with a system account on a specific port.
// Useful for reconnect tests where the server must restart on the same port.
func NewSingleServerOnPort(t *testing.T, port int) *server.Server {
	t.Helper()
	resetPreviousHTTPConnections()
	opts, err := server.ProcessConfigFile("../test/r1s1.conf")
	if err != nil {
		t.Fatalf("Error processing config file: %v", err)
	}
	opts.Port = port
	opts.HTTPPort = -1
	opts.NoLog = true

	s, err := server.NewServer(opts)
	if err != nil || s == nil {
		t.Fatalf("No NATS Server object returned: %v", err)
	}

	go s.Start()

	if !s.ReadyForConnections(10 * time.Second) {
		t.Fatal("Unable to start NATS Server in Go Routine")
	}
	ConnectAndVerify(t, s.ClientURL(), nats.UserCredentials("../test/myuser.creds"))
	return s
}

// Shutdown shuts the supercluster down
func (sc *SuperCluster) Shutdown() {
	for _, c := range sc.Clients {
		c.Close()
	}
	for _, s := range sc.Servers {
		s.Shutdown()
	}
	for _, s := range sc.Servers {
		s.WaitForShutdown()
	}
}

// ConnectAndVerify connects to a server a verifies it is
// ready to process messages.
func ConnectAndVerify(t *testing.T, url string, options ...nats.Option) *nats.Conn {
	c, err := nats.Connect(url, options...)
	if err != nil {
		t.Fatalf("Couldn't connect a client to %s: %v", url, err)
	}
	_, err = c.Subscribe("test.ready", func(msg *nats.Msg) {
		c.Flush()
		c.Publish(msg.Reply, nil)
	})
	if err != nil {
		t.Fatalf("Couldn't subscribe to \"test.ready\": %v", err)
	}
	_, err = c.Subscribe(("test.data"), func(msg *nats.Msg) {
		c.Flush()
		c.Publish(msg.Reply, []byte("response"))
	})
	if err != nil {
		t.Fatalf("Couldn't subscribe to \"test.data\": %v", err)
	}
	return c
}

func (sc *SuperCluster) setupClientsAndVerify(t *testing.T) {
	for _, s := range sc.Servers {
		c := ConnectAndVerify(t, s.ClientURL(), nats.UserCredentials("../test/myuser.creds"))
		sc.Clients = append(sc.Clients, c)
	}

	// now poll until we get responses from all subscribers.
	c := sc.Clients[0]
	inbox := nats.NewInbox()
	s, err := c.SubscribeSync(inbox)
	if err != nil {
		t.Fatalf("couldn't subscribe to test data:  %v", err)
	}
	var j int

	for i := 0; i < 10; i++ {
		c.PublishRequest("test.ready", inbox, nil)
		for j = 0; j < len(sc.Clients); j++ {
			_, err := s.NextMsg(time.Second * 5)
			if err != nil {
				break
			}
		}
		if j == len(sc.Clients) {
			break
		}
	}
	if j != len(sc.Clients) {
		t.Fatalf("couldn't ensure the supercluster was formed")
	}
}
