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
	DefaultListenPort         = 7777
	DefaultListenAddress      = "0.0.0.0"
	DefaultURL                = nats.DefaultURL
	DefaultExpectedServers    = 1
	DefaultPollTimeout        = time.Second * 3
	DefaultServerResponseWait = 500 * time.Millisecond

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
	ServerResponseWait   time.Duration
	ListenAddress        string
	ListenPort           int
	CertFile             string
	KeyFile              string
	CaFile               string
	HTTPCertFile         string
	HTTPKeyFile          string
	HTTPCaFile           string
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
		Name:               fmt.Sprintf("NATS_Surveyor - %s", hostname),
		ListenAddress:      DefaultListenAddress,
		ListenPort:         DefaultListenPort,
		URLs:               DefaultURL,
		PollTimeout:        DefaultPollTimeout,
		ExpectedServers:    DefaultExpectedServers,
		ServerResponseWait: DefaultServerResponseWait,
		Logger:             logrus.New(),
	}
	return opts
}

// A Surveyor instance
type Surveyor struct {
	sync.Mutex
	cp                  *natsConnPool
	httpServer          *http.Server
	jsAdvisoryManager   *JSAdvisoryManager
	jsAdvisoryFSWatcher *jsAdvisoryFSWatcher
	listener            net.Listener
	logger              *logrus.Logger
	opts                Options
	promRegistry        *prometheus.Registry
	statzC              *StatzCollector
	serviceObsManager   *ServiceObsManager
	serviceObsFSWatcher *serviceObsFSWatcher
	sysAcctPC           *pooledNatsConn
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
	cp := newSurveyorConnPool(opts, reconnectCtr)
	serviceObsMetrics := NewServiceObservationMetrics(promRegistry, opts.ConstLabels)
	serviceObsManager := newServiceObservationManager(cp, opts.Logger, serviceObsMetrics)
	serviceFsWatcher := newServiceObservationFSWatcher(opts.Logger, serviceObsManager)
	jsAdvisoryMetrics := NewJetStreamAdvisoryMetrics(promRegistry, opts.ConstLabels)
	jsAdvisoryManager := newJetStreamAdvisoryManager(cp, opts.Logger, jsAdvisoryMetrics)
	jsFsWatcher := newJetStreamAdvisoryFSWatcher(opts.Logger, jsAdvisoryManager)

	return &Surveyor{
		cp:                  cp,
		jsAdvisoryManager:   jsAdvisoryManager,
		jsAdvisoryFSWatcher: jsFsWatcher,
		logger:              opts.Logger,
		opts:                *opts,
		promRegistry:        promRegistry,
		serviceObsManager:   serviceObsManager,
		serviceObsFSWatcher: serviceFsWatcher,
	}, nil
}

func newSurveyorConnPool(opts *Options, reconnectCtr *prometheus.CounterVec) *natsConnPool {
	natsDefaults := &natsContextDefaults{
		Name:    opts.Name,
		URL:     opts.URLs,
		TLSCert: opts.CertFile,
		TLSKey:  opts.KeyFile,
		TLSCA:   opts.CaFile,
	}
	natsOpts := []nats.Option{
		nats.DisconnectErrHandler(func(c *nats.Conn, err error) {
			if err != nil {
				opts.Logger.Warnf("%q disconnected, will possibly miss replies: %v", c.Opts.Name, err)
			}
		}),
		nats.ReconnectHandler(func(c *nats.Conn) {
			reconnectCtr.WithLabelValues(c.Opts.Name).Inc()
			opts.Logger.Infof("%q reconnected to %v", c.Opts.Name, c.ConnectedAddr())
		}),
		nats.ClosedHandler(func(c *nats.Conn) {
			opts.Logger.Infof("%q connection closing", c.Opts.Name)
		}),
		nats.ErrorHandler(func(c *nats.Conn, s *nats.Subscription, err error) {
			if s != nil {
				opts.Logger.Warnf("Error: name=%q, subject=%s, err=%v", c.Opts.Name, s.Subject, err)
			} else {
				opts.Logger.Warnf("Error: name=%q err=%v", c.Opts.Name, err)
			}
		}),
		nats.MaxReconnects(10240),
	}
	return newNatsConnPool(opts.Logger, natsDefaults, natsOpts)
}

func (s *Surveyor) createStatszCollector() error {
	if s.opts.ExpectedServers == 0 {
		return nil
	}

	if !s.opts.Accounts {
		s.logger.Debugln("Skipping per-account exports")
	}

	s.statzC = NewStatzCollector(s.sysAcctPC.nc, s.logger, s.opts.ExpectedServers, s.opts.ServerResponseWait, s.opts.PollTimeout, s.opts.Accounts, s.opts.ConstLabels)
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

func (s *Surveyor) startJetStreamAdvisories() {
	if s.jsAdvisoryManager.IsRunning() {
		return
	}

	s.jsAdvisoryManager.start()
	dir := s.opts.JetStreamConfigDir
	if dir == "" {
		s.logger.Debugln("skipping JetStream advisory startup, no directory configured")
		return
	}

	fs, err := os.Stat(dir)
	if err != nil {
		s.logger.Warnf("could not start JetStream advisories, dir %s does not exist", dir)
		return
	}

	if !fs.IsDir() {
		s.logger.Warnf("JetStream advisories dir %s is not a directory", dir)
		return
	}

	if err := s.jsAdvisoryFSWatcher.start(); err != nil {
		s.logger.Warnf("could not start JetStream advisories filesystem watcher: %s", err)
	}

	err = filepath.WalkDir(dir, s.jsAdvisoryFSWatcher.startAdvisoriesInDir())
	if err != nil {
		s.logger.Warnf("error traversing JetStream advisories dir: %s, error: %s", dir, err)
		return
	}
}

func (s *Surveyor) startServiceObservations() {
	if s.serviceObsManager.IsRunning() {
		return
	}

	s.serviceObsManager.start()
	dir := s.opts.ObservationConfigDir
	if dir == "" {
		s.logger.Debugln("skipping service observation startup, no directory configured")
		return
	}

	fs, err := os.Stat(dir)
	if err != nil {
		s.logger.Warnf("could not start service observations, dir %s does not exist", dir)
		return
	}

	if !fs.IsDir() {
		s.logger.Warnf("service observations dir %s is not a directory", dir)
		return
	}

	if err := s.serviceObsFSWatcher.start(); err != nil {
		s.logger.Warnf("could not start service observations filesystem watcher: %s", err)
	}

	err = filepath.WalkDir(dir, s.serviceObsFSWatcher.startObservationsInDir())
	if err != nil {
		s.logger.Warnf("error traversing service observations dir: %s, error: %s", dir, err)
		return
	}
}

func (s *Surveyor) ServiceObservationManager() *ServiceObsManager {
	return s.serviceObsManager
}

func (s *Surveyor) JetStreamAdvisoryManager() *JSAdvisoryManager {
	return s.jsAdvisoryManager
}

// Start starts the surveyor
func (s *Surveyor) Start() error {
	s.Lock()
	defer s.Unlock()
	if s.sysAcctPC != nil {
		// already running
		return nil
	}

	var err error
	natsCtx := &natsContext{
		Credentials: s.opts.Credentials,
		Nkey:        s.opts.Nkey,
		Username:    s.opts.NATSUser,
		Password:    s.opts.NATSPassword,
	}

	s.sysAcctPC, err = s.cp.Get(natsCtx)
	if err != nil {
		return err
	}

	if s.statzC == nil {
		if err := s.createStatszCollector(); err != nil {
			return err
		}
	}

	s.startServiceObservations()
	s.startJetStreamAdvisories()

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
	if s.sysAcctPC == nil {
		// already stopped
		return
	}

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
		s.statzC = nil
	}

	s.serviceObsFSWatcher.stop()
	s.serviceObsManager.stop()

	s.jsAdvisoryFSWatcher.stop()
	s.jsAdvisoryManager.stop()

	s.sysAcctPC.ReturnToPool()
	s.sysAcctPC = nil
}

// Gather implements the prometheus.Gatherer interface
func (s *Surveyor) Gather() ([]*dto.MetricFamily, error) {
	return s.promRegistry.Gather()
}
