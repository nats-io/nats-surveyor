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
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	dto "github.com/prometheus/client_model/go"
	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/bcrypt"
)

// Defaults
var (
	DefaultListenPort      = 7777
	DefaultListenAddress   = "0.0.0.0"
	DefaultURL             = nats.DefaultURL
	DefaultExpectedServers = 1
	DefaultPollTimeout     = time.Second * 3

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
	Accounts             bool
	Logger               *logrus.Logger
	ConstLabels          prometheus.Labels // not exposed by CLI
	DisableHTTPServer    bool              // not exposed by CLI
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
		Logger:          logrus.New(),
	}
	return opts
}

// A Surveyor instance
type Surveyor struct {
	sync.Mutex
	opts                Options
	logger              *logrus.Logger
	listener            net.Listener
	httpServer          *http.Server
	promRegistry        *prometheus.Registry
	reconnectCtr        *prometheus.CounterVec
	statzC              *StatzCollector
	observationMetrics  *ServiceObsMetrics
	observations        []*ServiceObsListener
	jsAPIMetrics        *JSAdvisoryMetrics
	jsAPIAudits         []*JSAdvisoryListener
	observationWatchers map[string]struct{}
	stop                chan struct{}
}

func connect(opts *Options, reconnectCtr *prometheus.CounterVec) (*nats.Conn, error) {
	nopts := []nats.Option{
		nats.Name(opts.Name),
	}

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
		if err != nil {
			opts.Logger.Warnf("%q disconnected, will possibly miss replies: %v", c.Opts.Name, err)
		}
	}))
	nopts = append(nopts, nats.ReconnectHandler(func(c *nats.Conn) {
		reconnectCtr.WithLabelValues(c.Opts.Name).Inc()
		opts.Logger.Infof("%q reconnected to %v", c.Opts.Name, c.ConnectedAddr())
	}))
	nopts = append(nopts, nats.ClosedHandler(func(c *nats.Conn) {
		opts.Logger.Infof("%q connection closing", c.Opts.Name)
	}))
	nopts = append(nopts, nats.ErrorHandler(func(c *nats.Conn, s *nats.Subscription, err error) {
		if s != nil {
			opts.Logger.Warnf("Error: name=%q err=%v", c.Opts.Name, err)
		} else {
			opts.Logger.Warnf("Error: name=%q, subject=%s, err=%v", c.Opts.Name, s.Subject, err)
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
	opts.Logger.Infof("%s connected to NATS Deployment: %v", opts.Name, nc.ConnectedAddr())

	return nc, err
}

// NewSurveyor creates a surveyor
func NewSurveyor(opts *Options) (*Surveyor, error) {
	if opts.URLs == "" {
		return nil, fmt.Errorf("surveyor URLs field is required")
	}

	promRegistry := prometheus.NewRegistry()
	reconnectCtr := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:        prometheus.BuildFQName("nats", "survey", "nats_reconnects"),
		Help:        "Number of times the surveyor reconnected to the NATS cluster",
		ConstLabels: opts.ConstLabels,
	}, []string{"name"})
	promRegistry.MustRegister(reconnectCtr)
	return &Surveyor{
		opts:               *opts,
		logger:             opts.Logger,
		promRegistry:       promRegistry,
		reconnectCtr:       reconnectCtr,
		observationMetrics: NewServiceObservationMetrics(promRegistry, opts.ConstLabels),
		jsAPIMetrics:       NewJetStreamAdvisoryMetrics(promRegistry, opts.ConstLabels),
	}, nil
}

func (s *Surveyor) createStatszCollector() error {
	if s.opts.ExpectedServers == 0 {
		return nil
	}

	nc, err := connect(&s.opts, s.reconnectCtr)
	if err != nil {
		return err
	}

	if !s.opts.Accounts {
		s.logger.Debugln("Skipping per-account exports")
	}

	s.statzC = NewStatzCollector(nc, s.logger, s.opts.ExpectedServers, s.opts.PollTimeout, s.opts.Accounts, s.opts.ConstLabels)
	s.promRegistry.MustRegister(s.statzC)
	return nil
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
		rootPEM, err := os.ReadFile(s.opts.HTTPCaFile)
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
		sz := s.statzC

		if sz == nil {
			next.ServeHTTP(rw, r)
			return
		}

		if sz.Polling() {
			rw.WriteHeader(http.StatusServiceUnavailable)
			rw.Write([]byte("Concurrent polls are not supported"))
			s.logger.Warnln("Concurrent access detected and blocked")
			return
		}

		next.ServeHTTP(rw, r)
	})
}

// getScrapeHandler returns a chain of handlers that handles auth, concurency checks
// and the prometheus handlers
func (s *Surveyor) getScrapeHandler() http.Handler {
	return s.httpAuthMiddleware(s.httpConcurrentPollBlockMiddleware(promhttp.InstrumentMetricHandler(
		s.promRegistry, promhttp.HandlerFor(s.promRegistry, promhttp.HandlerOpts{}),
	)))
}

// startHTTP configures and starts the HTTP server for applications to poll data from
// exporter.
// caller must lock
func (s *Surveyor) startHTTP() error {
	var hp string
	var err error
	var proto string
	var config *tls.Config
	var listener net.Listener

	hp = net.JoinHostPort(s.opts.ListenAddress, strconv.Itoa(s.opts.ListenPort))

	// If a certificate file has been specified, setup TLS with the
	// key provided.
	if s.opts.HTTPCertFile != "" {
		proto = "https"
		s.logger.Debugln("Certificate file specified; using https.")
		config, err = s.generateHTTPTLSConfig()
		if err != nil {
			return err
		}
		listener, err = tls.Listen("tcp", hp, config)
	} else {
		proto = "http"
		s.logger.Debugln("No certificate file specified; using listener.")
		listener, err = net.Listen("tcp", hp)
	}

	if err != nil {
		s.logger.Errorf("can't start HTTP listener: %v", err)
		return err
	}

	s.listener = listener
	s.logger.Infof("Prometheus exporter listening at %s://%s/metrics", proto, hp)

	mux := http.NewServeMux()
	mux.Handle("/metrics", s.getScrapeHandler())
	mux.HandleFunc("/healthz", func(resp http.ResponseWriter, req *http.Request) {
		resp.Write([]byte("ok"))
	})

	httpServer := &http.Server{
		Addr:           hp,
		Handler:        mux,
		MaxHeaderBytes: 1 << 20,
		TLSConfig:      config,
	}
	s.httpServer = httpServer

	go func() {
		err := httpServer.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Errorf("Unable to start HTTP server (may already be running): %v", err)
		}
	}()

	return nil
}

func (s *Surveyor) startJetStreamAdvisories() error {
	s.jsAPIAudits = []*JSAdvisoryListener{}
	s.jsAPIMetrics.jsAdvisoriesGauge.Set(0)

	dir := s.opts.JetStreamConfigDir
	if dir == "" {
		s.logger.Debugln("Skipping JetStream API Audit startup, no directory configured")
		return nil
	}

	fs, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("could not start JetStream API Audit, %s does not exist", dir)
	}

	if !fs.IsDir() {
		return fmt.Errorf("JetStream API Audit dir %s is not a directory", dir)
	}

	// TODO: new watcher should be created in each directory
	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if filepath.Ext(info.Name()) != ".json" {
			return nil
		}

		obs, err := NewJetStreamAdvisoryListener(path, s.opts, s.jsAPIMetrics, s.reconnectCtr)
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
	s.observations = []*ServiceObsListener{}
	s.observationWatchers = make(map[string]struct{})
	s.observationMetrics.observationsGauge.Set(0)

	dir := s.opts.ObservationConfigDir
	if dir == "" {
		s.logger.Debugln("Skipping observation startup, no directory configured")
		return nil
	}

	fs, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("could not start observations, %s does not exist", dir)
	}

	if !fs.IsDir() {
		return fmt.Errorf("observations dir %s is not a directory", dir)
	}

	err = filepath.WalkDir(dir, s.startObservationsInDir(5))
	if err != nil {
		return err
	}
	if err := s.watchObservations(dir, 5); err != nil {
		return err
	}

	return nil
}

func (s *Surveyor) Observations() []*ServiceObsListener {
	return s.observations
}

// Start starts the surveyor
func (s *Surveyor) Start() error {
	s.Lock()
	defer s.Unlock()

	s.stop = make(chan struct{})

	if s.statzC == nil {
		if err := s.createStatszCollector(); err != nil {
			return err
		}
	}

	if s.observations == nil {
		if err := s.startObservations(); err != nil {
			return err
		}
	}

	if s.jsAPIAudits == nil {
		if err := s.startJetStreamAdvisories(); err != nil {
			return err
		}
	}

	if !s.opts.DisableHTTPServer && s.listener == nil && s.httpServer == nil {
		if err := s.startHTTP(); err != nil {
			return err
		}
	}

	return nil
}

// Stop stops the surveyor
func (s *Surveyor) Stop() {
	s.Lock()
	defer s.Unlock()

	if s.httpServer != nil {
		_ = s.httpServer.Shutdown(context.Background())
		s.httpServer = nil
	}

	if s.listener != nil {
		_ = s.listener.Close()
		s.listener = nil
	}

	if s.statzC != nil {
		s.promRegistry.Unregister(s.statzC)
		s.statzC.nc.Close()
		s.statzC = nil
	}

	if s.observations != nil {
		for _, o := range s.observations {
			o.nc.Close()
		}
		s.observations = nil
	}

	if s.jsAPIAudits != nil {
		for _, j := range s.jsAPIAudits {
			j.nc.Close()
		}
		s.jsAPIAudits = nil
	}
	close(s.stop)
}

// Gather implements the prometheus.Gatherer interface
func (s *Surveyor) Gather() ([]*dto.MetricFamily, error) {
	return s.promRegistry.Gather()
}
