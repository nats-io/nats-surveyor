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

// Package surveyor is used to garner data from a NATS deployment for Prometheus
package surveyor

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/go-cmp/cmp"
	nats "github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/crypto/bcrypt"
)

// Defaults
var (
	DefaultListenPort      = 7777
	DefaultListenAddress   = "0.0.0.0"
	DefaultURL             = nats.DefaultURL
	DefaultExpectedServers = 3
	DefaultPollTimeout     = time.Second * 5

	// bcryptPrefix from nats-server
	bcryptPrefix = "$2a$"
)

// Options are used to configure the NATS collector
type Options struct {
	Name                 string
	URLs                 string
	Credentials          string
	Nkey                 string
	NATSUser             string
	NATSPassword         string
	PollTimeout          time.Duration
	ExpectedServers      int
	ListenAddress        string
	ListenPort           int
	CertFile             string
	KeyFile              string
	CaFile               string
	HTTPCertFile         string
	HTTPKeyFile          string
	HTTPCaFile           string
	NATSServerURL        string
	HTTPUser             string // User in metrics scrape by Prometheus.
	HTTPPassword         string
	Prefix               string // TODO
	ObservationConfigDir string
	JetStreamConfigDir   string
}

// GetDefaultOptions returns the default set of options
func GetDefaultOptions() *Options {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "127.0.0.1"
	}

	opts := &Options{
		Name:            fmt.Sprintf("NATS_Surveyor - %s", hostname),
		ListenAddress:   DefaultListenAddress,
		ListenPort:      DefaultListenPort,
		URLs:            DefaultURL,
		PollTimeout:     DefaultPollTimeout,
		ExpectedServers: DefaultExpectedServers,
	}
	return opts
}

// A Surveyor instance
type Surveyor struct {
	sync.Mutex
	opts         Options
	nc           *nats.Conn
	http         net.Listener
	statzC       *StatzCollector
	observations []*ServiceObsListener
	jsAPIAudits  []*JSAdvisoryListener
}

func connect(opts *Options) (*nats.Conn, error) {
	reconnCtr := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName("nats", "survey", "nats_reconnects"),
		Help: "Number of times the surveyor reconnected to the NATS cluster",
	}, []string{"name"})
	prometheus.Register(reconnCtr)

	var nopts []nats.Option

	switch {
	case opts.Credentials != "":
		nopts = append(nopts, nats.UserCredentials(opts.Credentials))
	case opts.Nkey != "":
		o, err := nats.NkeyOptionFromSeed(opts.Nkey)
		if err != nil {
			return nil, fmt.Errorf("unable to load nkey: %v", err)
		}
		nopts = append(nopts, o)
	case opts.NATSUser != "":
		nopts = append(nopts, nats.UserInfo(opts.NATSUser, opts.NATSPassword))
	}

	nopts = append(nopts, nats.DisconnectErrHandler(func(c *nats.Conn, err error) {
		log.Printf("%q disconnected, will possibly miss replies: %s", c.Opts.Name, err)
	}))
	nopts = append(nopts, nats.ReconnectHandler(func(c *nats.Conn) {
		reconnCtr.WithLabelValues(c.Opts.Name).Inc()
		log.Printf("%q reconnected to %v", c.Opts.Name, c.ConnectedAddr())
	}))
	nopts = append(nopts, nats.ClosedHandler(func(c *nats.Conn) {
		log.Printf("%q connection permanently lost!", c.Opts.Name)
	}))
	nopts = append(nopts, nats.ErrorHandler(func(c *nats.Conn, s *nats.Subscription, err error) {
		if s != nil {
			log.Printf("Error: name=%q err=%v", c.Opts.Name, err)
		} else {
			log.Printf("Error: name=%q, subject=%s, err=%v", c.Opts.Name, s.Subject, err)
		}
	}))
	nopts = append(nopts, nats.MaxReconnects(10240))

	// NATS TLS Options
	if opts.CaFile != "" {
		nopts = append(nopts, nats.RootCAs(opts.CaFile))
	}
	if opts.CertFile != "" {
		nopts = append(nopts, nats.ClientCert(opts.CertFile, opts.KeyFile))
	}

	nc, err := nats.Connect(opts.URLs, nopts...)
	if err != nil {
		return nil, err
	}
	log.Printf("Connected to NATS Deployment: %v", nc.ConnectedAddr())

	return nc, err
}

// NewSurveyor creates a surveyor
func NewSurveyor(opts *Options) (*Surveyor, error) {
	nc, err := connect(opts)
	if err != nil {
		return nil, err
	}
	return &Surveyor{
		nc:           nc,
		opts:         *opts,
		observations: []*ServiceObsListener{},
		jsAPIAudits:  []*JSAdvisoryListener{},
	}, nil
}

func (s *Surveyor) createCollector() error {
	if s.opts.ExpectedServers == 0 {
		return nil
	}

	s.Lock()
	s.statzC = NewStatzCollector(s.nc, s.opts.ExpectedServers, s.opts.PollTimeout)
	s.Unlock()

	err := prometheus.Register(s.statzC)
	for i := 0; i < 50 && err != nil; i++ {
		if _, ok := err.(prometheus.AlreadyRegisteredError); ok {
			// ignore
			return nil
		}

		// If we're here usually the Prometheus server is unreachable.
		// Prometheus error types are not documented.
		log.Printf("Error registering collector: %v", err)
		log.Printf("Retrying in 500 ms...")
		time.Sleep(500 * time.Millisecond)
		err = prometheus.Register(s.statzC)
	}
	return err
}

// generates the TLS config for https
func (s *Surveyor) generateHTTPTLSConfig() (*tls.Config, error) {
	//  Load in cert and private key
	cert, err := tls.LoadX509KeyPair(s.opts.HTTPCertFile, s.opts.HTTPKeyFile)
	if err != nil {
		return nil, fmt.Errorf("error parsing X509 certificate/key pair (%s, %s): %v",
			s.opts.HTTPCertFile, s.opts.HTTPKeyFile, err)
	}
	cert.Leaf, err = x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("error parsing certificate (%s): %v",
			s.opts.HTTPCertFile, err)
	}
	// Create our TLS configuration
	config := &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.NoClientCert,
		MinVersion:   tls.VersionTLS12,
	}
	// Add in CAs if applicable.
	if s.opts.HTTPCaFile != "" {
		rootPEM, err := ioutil.ReadFile(s.opts.HTTPCaFile)
		if err != nil || rootPEM == nil {
			return nil, fmt.Errorf("failed to load root ca certificate (%s): %v", s.opts.HTTPCaFile, err)
		}
		pool := x509.NewCertPool()
		ok := pool.AppendCertsFromPEM(rootPEM)
		if !ok {
			return nil, fmt.Errorf("failed to parse root ca certificate")
		}
		config.ClientCAs = pool
	}
	return config, nil
}

// isBcrypt checks whether the given password or token is bcrypted.
func isBcrypt(password string) bool {
	return strings.HasPrefix(password, bcryptPrefix)
}

func (s *Surveyor) isValidUserPass(user, password string) bool {
	if user != s.opts.HTTPUser {
		return false
	}
	exporterPassword := s.opts.HTTPPassword
	if isBcrypt(exporterPassword) {
		if err := bcrypt.CompareHashAndPassword([]byte(exporterPassword), []byte(password)); err != nil {
			return false
		}
	} else if exporterPassword != password {
		return false
	}
	return true
}

func (s *Surveyor) httpAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if s.opts.HTTPUser != "" {
			auth := strings.SplitN(r.Header.Get("Authorization"), " ", 2)
			if len(auth) != 2 || auth[0] != "Basic" {
				http.Error(rw, "authorization failed", http.StatusUnauthorized)
				return
			}

			payload, err := base64.StdEncoding.DecodeString(auth[1])
			if err != nil {
				http.Error(rw, "authorization failed", http.StatusBadRequest)
				return
			}
			pair := strings.SplitN(string(payload), ":", 2)

			if len(pair) != 2 || !s.isValidUserPass(pair[0], pair[1]) {
				http.Error(rw, "authorization failed", http.StatusUnauthorized)
				return
			}
		}
		next.ServeHTTP(rw, r)
	})
}

func (s *Surveyor) httpConcurrentPollBlockMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		s.Lock()
		sz := s.statzC
		s.Unlock()

		if sz == nil {
			next.ServeHTTP(rw, r)
			return
		}

		if sz.Polling() {
			rw.WriteHeader(http.StatusServiceUnavailable)
			rw.Write([]byte("Concurrent polls are not supported"))
			log.Printf("Concurrent access detected and blocked\n")
			return
		}

		next.ServeHTTP(rw, r)
	})
}

// getScrapeHandler returns a chain of handlers that handles auth, concurency checks
// and the prometheus handlers
func (s *Surveyor) getScrapeHandler() http.Handler {
	return s.httpAuthMiddleware(s.httpConcurrentPollBlockMiddleware(promhttp.Handler()))
}

// startHTTP configures and starts the HTTP server for applications to poll data from
// exporter.
// caller must lock
func (s *Surveyor) startHTTP() error {
	var hp string
	var err error
	var proto string
	var config *tls.Config

	hp = net.JoinHostPort(s.opts.ListenAddress, strconv.Itoa(s.opts.ListenPort))

	// If a certificate file has been specified, setup TLS with the
	// key provided.
	if s.opts.HTTPCertFile != "" {
		proto = "https"
		// debug
		log.Printf("Certificate file specfied; using https.")
		config, err = s.generateHTTPTLSConfig()
		if err != nil {
			return err
		}
		s.http, err = tls.Listen("tcp", hp, config)
	} else {
		proto = "http"

		// debug
		log.Printf("No certificate file specified; using http.")
		s.http, err = net.Listen("tcp", hp)
	}

	log.Printf("Prometheus exporter listening at %s://%s/metrics", proto, hp)

	if err != nil {
		log.Printf("can't start HTTP listener: %v", err)
		return err
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", s.getScrapeHandler())
	mux.HandleFunc("/healthz", func(resp http.ResponseWriter, req *http.Request) {
		resp.Write([]byte("ok"))
	})

	srv := &http.Server{
		Addr:           hp,
		Handler:        mux,
		MaxHeaderBytes: 1 << 20,
		TLSConfig:      config,
	}

	sHTTP := s.http
	go func() {
		for i := 0; i < 10; i++ {
			var err error
			if err = srv.Serve(sHTTP); err != nil {
				// In a test environment, this can fail because the server is already running.
				// debugf
				log.Printf("Unable to start HTTP server (may already be running): %v", err)
			}
		}
	}()

	return nil
}

func (s *Surveyor) startJetStreamAdvisories() error {
	jsAdvisoriesGauge.Set(0)

	dir := s.opts.JetStreamConfigDir
	if dir == "" {
		log.Printf("Skipping JetStream API Audit startup, no directory configured")
		return nil
	}

	fs, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("could not start JetStream API Audit, %s does not exist", dir)
	}

	if !fs.IsDir() {
		return fmt.Errorf("JetStream API Audit dir %s is not a directory", dir)
	}

	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if filepath.Ext(info.Name()) != ".json" {
			return nil
		}

		obs, err := NewJetStreamAdvisoryListener(path, s.opts)
		if err != nil {
			return fmt.Errorf("could not create JetStream API Audit from %s: %s", path, err)
		}

		err = obs.Start()
		if err != nil {
			return fmt.Errorf("could not start observation from %s: %s", path, err)
		}

		s.jsAPIAudits = append(s.jsAPIAudits, obs)

		return nil
	})

	return err
}

func (s *Surveyor) startObservations() error {
	observationsGauge.Set(0)

	dir := s.opts.ObservationConfigDir
	if dir == "" {
		log.Printf("Skipping observation startup, no directory configured")
		return nil
	}

	fs, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("could not start observations, %s does not exist", dir)
	}

	if !fs.IsDir() {
		return fmt.Errorf("observations dir %s is not a directory", dir)
	}

	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if filepath.Ext(info.Name()) != ".json" {
			return nil
		}

		obs, err := NewServiceObservation(path, s.opts)
		if err != nil {
			return fmt.Errorf("could not create observation from %s: %s", path, err)
		}

		for _, o := range s.observations {
			if cmp.Equal(obs.opts, o.opts) {
				return nil
			}
		}

		err = obs.Start()
		if err != nil {
			return fmt.Errorf("could not start observation from %s: %s", path, err)
		}

		s.observations = append(s.observations, obs)

		return nil
	})

	return err
}

// Start starts the surveyor
func (s *Surveyor) Start() error {
	if err := s.startHTTP(); err != nil {
		return err
	}

	if err := s.createCollector(); err != nil {
		return err
	}

	if err := s.startObservations(); err != nil {
		return err
	}

	if err := s.startJetStreamAdvisories(); err != nil {
		return err
	}

	return nil
}

// Stop stops the surveyor
func (s *Surveyor) Stop() {
	s.Lock()

	for _, o := range s.observations {
		o.nc.Close()
	}

	prometheus.Unregister(s.statzC)
	s.http.Close()
	s.nc.Drain()
	s.Unlock()
}
