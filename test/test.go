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
		Port:     4222,
		HTTPHost: "127.0.0.1",
		HTTPPort: 8222,
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

// StartServer starts a a NATS server
func StartServer(t *testing.T, confFile string) *server.Server {
	resetPreviousHTTPConnections()
	opts, err := server.ProcessConfigFile(confFile)
	if err != nil {
		t.Fatalf("Error processing config file: %v", err)
	}

	// remove this line for debugging
	opts.NoLog = true

	s, err := server.NewServer(opts)
	if err != nil || s == nil {
		panic(fmt.Sprintf("No NATS Server object returned: %v", err))
	}

	if !opts.NoLog {
		s.ConfigureLogger()
	}

	// Run server in Go routine.
	go s.Start()

	// Wait for accept loop(s) to be started
	if !s.ReadyForConnections(10 * time.Second) {
		panic("Unable to start NATS Server in Go Routine")
	}
	return s
}

// SuperCluster holds client connections and NATS servers
type SuperCluster struct {
	Servers []*server.Server
	Clients []*nats.Conn
}

var configFiles = []string{"../test/r1s1.conf", "../test/r1s2.conf", "../test/r2s1.conf"}
var jetStreamConfigFiles = []string{"../test/js1.conf", "../test/js2.conf", "../test/js3.conf"}

// NewSuperCluster creates a small supercluster for testing, with one client per server.
func NewSuperCluster(t *testing.T) *SuperCluster {
	sc := &SuperCluster{}
	for _, f := range configFiles {
		sc.Servers = append(sc.Servers, StartServer(t, f))
	}
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

// StartJetStreamServerFromConfig starts a JetStream server using the provided configuration
// StoreDir from config will not be used - instead, a tmp directory will be created for the test
func StartJetStreamServerFromConfig(t *testing.T, confFile string) *server.Server {
	opts, err := server.ProcessConfigFile(confFile)

	if err != nil {
		t.Fatalf("Error processing config file: %v", err)
	}

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
		t.Fatal("Unable to start NATS Server")
	}
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
