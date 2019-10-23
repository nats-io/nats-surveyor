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
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/logger"
	nats "github.com/nats-io/nats.go"
	ns "github.com/nats-io/nats-server/v2/server"
)

// Test variables
var (
	SystemCreds = "../test/SYS.creds"
)

func resetPreviousHTTPConnections() {
	http.DefaultTransport = &http.Transport{}
}

// StartBasicServer runs the NATS server with a monitor port in a go routine
func StartBasicServer() *ns.Server {
	resetPreviousHTTPConnections()

	// To enable debug/trace output in the NATS server,
	// flip the enableLogging flag.
	// enableLogging = true

	opts := &ns.Options{
		Host:     "127.0.0.1",
		Port:     4222,
		HTTPHost: "127.0.0.1",
		HTTPPort: 8222,
		NoLog:    true,
		NoSigs:   true,
	}

	s := ns.New(opts)
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

// startServer starts a a NATS server
func startServer(t *testing.T, confFile string) *ns.Server {
	resetPreviousHTTPConnections()
	opts, err := ns.ProcessConfigFile(confFile)

	// remove this line for debugging
	opts.NoLog = true

	if err != nil {
		t.Fatalf("Error processing config file: %v", err)
	}

	s, err := ns.NewServer(opts)
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
	Servers []*ns.Server
	Clients []*nats.Conn
}

var configFiles = []string{"../test/r1s1.conf", "../test/r1s2.conf", "../test/r2s1.conf"}

// NewSuperCluster creates a small supercluster for testing, with one client per server.
func NewSuperCluster(t *testing.T) *SuperCluster {
	sc := &SuperCluster{}
	for _, f := range configFiles {
		sc.Servers = append(sc.Servers, startServer(t, f))
	}
	sc.setupClientsAndVerify(t)
	return sc
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

func (sc *SuperCluster) setupClientsAndVerify(t *testing.T) {
	for _, s := range sc.Servers {
		c, err := nats.Connect(s.ClientURL(), nats.UserCredentials("../test/myuser.creds"))
		if err != nil {
			t.Fatalf("Couldn't connect a client to %s: %v", s.ClientURL(), err)
		}
		_, err = c.Subscribe("test.ready", func(msg *nats.Msg) {
			c.Publish(msg.Reply, nil)
		})
		if err != nil {
			t.Fatalf("Couldn't subscribe to \"test.ready\": %v", err)
		}
		_, err = c.Subscribe(("test.data"), func(msg *nats.Msg) {
			c.Publish(msg.Reply, []byte("response"))
		})
		if err != nil {
			t.Fatalf("Couldn't subscribe to \"test.data\": %v", err)
		}
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
