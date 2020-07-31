// Copyright 2017-2018 The NATS Authors
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

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/nats-io/nats-surveyor/surveyor"
)

var version = "0.1.1"

func main() {
	var printVersion bool

	opts := surveyor.GetDefaultOptions()

	// Parse flags
	flag.BoolVar(&printVersion, "version", false, "Show exporter version and exit.")
	flag.StringVar(&opts.URLs, "s", "nats://localhost:4222", "NATS Cluster url(s)")
	flag.StringVar(&opts.Credentials, "creds", "", "Credentials File")
	flag.StringVar(&opts.Nkey, "nkey", "", "Nkey Seed File")
	flag.StringVar(&opts.NATSUser, "user", "", "NATS user name or token")
	flag.StringVar(&opts.NATSPassword, "password", "", "NATS user password")
	flag.IntVar(&opts.ExpectedServers, "c", 1, "Expected number of servers")
	flag.DurationVar(&opts.PollTimeout, "timeout", 3*time.Second, "Polling timeout")
	flag.IntVar(&opts.ListenPort, "port", surveyor.DefaultListenPort, "Port to listen on.")
	flag.IntVar(&opts.ListenPort, "p", surveyor.DefaultListenPort, "Port to listen on.")
	flag.StringVar(&opts.ListenAddress, "addr", surveyor.DefaultListenAddress, "Network host to listen on.")
	flag.StringVar(&opts.ListenAddress, "a", surveyor.DefaultListenAddress, "Network host to listen on.")
	flag.StringVar(&opts.CertFile, "tlscert", "", "Client certificate file for NATS connections.")
	flag.StringVar(&opts.KeyFile, "tlskey", "", "Client private key for NATS connections.")
	flag.StringVar(&opts.CaFile, "tlscacert", "", "Client certificate CA on NATS connecctions.")
	flag.StringVar(&opts.HTTPCertFile, "http_tlscert", "", "Server certificate file (Enables HTTPS).")
	flag.StringVar(&opts.HTTPKeyFile, "http_tlskey", "", "Private key for server certificate (used with HTTPS).")
	flag.StringVar(&opts.HTTPCaFile, "http_tlscacert", "", "Client certificate CA for verification (used with HTTPS).")
	flag.StringVar(&opts.HTTPUser, "http_user", "", "Enable basic auth and set user name for HTTP scrapes.")
	flag.StringVar(&opts.HTTPPassword, "http_pass", "", "Set the password for HTTP scrapes. NATS bcrypt supported.")
	flag.StringVar(&opts.Prefix, "prefix", "", "Replace the default prefix for all the metrics.")
	flag.StringVar(&opts.ObservationConfigDir, "observe", "", "Listen for observation statistics based on config files in a directory.")
	flag.StringVar(&opts.JetStreamConfigDir, "jetstream", "", "Listen for JetStream Advisories based on config files in a directory.")
	flag.Parse()

	if printVersion {
		fmt.Println("nats-surveyor v", version)
		os.Exit(0)
	}

	s, err := surveyor.NewSurveyor(opts)
	if err != nil {
		log.Fatalf("couldn't start surveyor: %v", err)
	}
	err = s.Start()
	if err != nil {
		log.Fatalf("couldn't start surveyor: %s", err)
	}

	// Setup the interrupt handler to gracefully exit.
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGQUIT)
	go func() {
		for {
			sig := <-c

			switch sig {
			case syscall.SIGQUIT:
				buf := make([]byte, 1<<20)
				stacklen := runtime.Stack(buf, true)
				fmt.Fprintln(os.Stderr, string(buf[:stacklen]))

			default:
				s.Stop()
				os.Exit(0)
			}
		}
	}()

	runtime.Goexit()
}
