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

package surveyor

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/textproto"
	"os"
	"strconv"
	"strings"

	"github.com/nats-io/jsm.go"
	"github.com/nats-io/jsm.go/api/server/metric"
	"github.com/nats-io/nats.go"
	"github.com/opentracing/opentracing-go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/uber/jaeger-client-go"
	jaegercfg "github.com/uber/jaeger-client-go/config"
	"github.com/uber/jaeger-lib/metrics"
)

// ServiceObsListener listens for observations from nats service latency checks
type ServiceObsListener struct {
	nc    *nats.Conn
	opts  *serviceObsOptions
	sopts *Options

	openTracingTracer opentracing.Tracer
	openTracingCloser io.Closer

	tracing bool
}

type serviceObsOptions struct {
	ServiceName string `json:"name"`
	Topic       string `json:"topic"`
	Credentials string `json:"credential"`
	JaegerHost  string `json:"jaeger_host"`
	UserName    string `json:"username"`
	Password    string `json:"password"`
}

func (o *serviceObsOptions) Validate() error {
	errs := []string{}

	if o.ServiceName == "" {
		errs = append(errs, "name is required")
	}

	if o.Topic == "" {
		errs = append(errs, "topic is required")
	}

	if o.Credentials != "" {
		_, err := os.Stat(o.Credentials)
		if err != nil {
			errs = append(errs, fmt.Sprintf("invalid credential file: %s", err))
		}
	}

	if len(errs) == 0 {
		return nil
	}

	return errors.New(strings.Join(errs, ", "))
}

var (
	observationsGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: prometheus.BuildFQName("nats", "latency", "observations_count"),
		Help: "Number of Service Latency listeners that are running",
	})

	observationsReceived = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName("nats", "latency", "observations_received_count"),
		Help: "Number of observations received by this surveyor across all services",
	}, []string{"service", "app"})

	serviceRequestStatus = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName("nats", "latency", "observation_status_count"),
		Help: "The status result codes for requests to a service",
	}, []string{"service", "status"})

	invalidObservationsReceived = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName("nats", "latency", "observation_error_count"),
		Help: "Number of observations received by this surveyor across all services that could not be handled",
	}, []string{"service"})

	serviceLatency = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: prometheus.BuildFQName("nats", "latency", "service_duration"),
		Help: "Time spent serving the request in the service",
	}, []string{"service", "app"})

	totalLatency = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: prometheus.BuildFQName("nats", "latency", "total_duration"),
		Help: "Total time spent serving a service including network overheads",
	}, []string{"service", "app"})

	requestorRTT = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: prometheus.BuildFQName("nats", "latency", "requestor_rtt"),
		Help: "The RTT to the client making a request",
	}, []string{"service", "app"})

	responderRTT = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: prometheus.BuildFQName("nats", "latency", "responder_rtt"),
		Help: "The RTT to the service serving the request",
	}, []string{"service", "app"})

	systemRTT = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: prometheus.BuildFQName("nats", "latency", "system_rtt"),
		Help: "The RTT within the NATS system - time traveling clusters, gateways and leaf nodes",
	}, []string{"service", "app"})

	trcUber = textproto.CanonicalMIMEHeaderKey("Uber-Trace-Id")
	trcTp   = textproto.CanonicalMIMEHeaderKey("Traceparent")
	trcB3Sm = textproto.CanonicalMIMEHeaderKey("X-B3-Sampled")
)

func init() {
	prometheus.MustRegister(invalidObservationsReceived)
	prometheus.MustRegister(observationsReceived)
	prometheus.MustRegister(serviceRequestStatus)
	prometheus.MustRegister(serviceLatency)
	prometheus.MustRegister(totalLatency)
	prometheus.MustRegister(requestorRTT)
	prometheus.MustRegister(responderRTT)
	prometheus.MustRegister(systemRTT)
	prometheus.MustRegister(observationsGauge)
}

// NewServiceObservation creates a new performance observation listener
func NewServiceObservation(f string, sopts Options) (*ServiceObsListener, error) {
	js, err := ioutil.ReadFile(f)
	if err != nil {
		return nil, err
	}

	opts := &serviceObsOptions{}
	err = json.Unmarshal(js, opts)
	if err != nil {
		return nil, fmt.Errorf("invalid service observation configuration: %s: %s", f, err)
	}

	err = opts.Validate()
	if err != nil {
		return nil, fmt.Errorf("invalid service observation configuration: %s: %s", f, err)
	}

	sopts.Name = fmt.Sprintf("%s (observing %s)", sopts.Name, opts.ServiceName)
	sopts.Credentials = opts.Credentials
	sopts.NATSUser = opts.UserName
	sopts.NATSPassword = opts.Password

	nc, err := connect(&sopts)
	if err != nil {
		return nil, fmt.Errorf("nats connection failed: %s", err)
	}

	listener := &ServiceObsListener{
		nc:    nc,
		opts:  opts,
		sopts: &sopts,
	}

	if opts.JaegerHost != "" {
		cfg := jaegercfg.Configuration{
			ServiceName: opts.ServiceName,
			Sampler: &jaegercfg.SamplerConfig{
				Type:  jaeger.SamplerTypeConst,
				Param: 1,
			},
			Reporter: &jaegercfg.ReporterConfig{LocalAgentHostPort: opts.JaegerHost},
		}

		listener.openTracingTracer, listener.openTracingCloser, err = cfg.NewTracer(jaegercfg.Metrics(metrics.NullFactory))
		if err != nil {
			return nil, err
		}

		listener.tracing = true
	}

	return listener, nil
}

// Start starts listening for observations
func (o *ServiceObsListener) Start() error {
	_, err := o.nc.Subscribe(o.opts.Topic, o.observationHandler)
	if err != nil {
		return fmt.Errorf("could not subscribe to observation topic for %s (%s): %s", o.opts.ServiceName, o.opts.Topic, err)
	}
	err = o.nc.Flush()
	if err != nil {
		return err
	}

	observationsGauge.Inc()
	log.Printf("Started observing stats on %s for %s", o.opts.Topic, o.opts.ServiceName)

	return nil
}

func (o *ServiceObsListener) observationHandler(m *nats.Msg) {
	kind, obs, err := jsm.ParseEvent(m.Data)
	if err != nil {
		invalidObservationsReceived.WithLabelValues(o.opts.ServiceName).Inc()
		log.Printf("Unparsable observation received on %s: %s", o.opts.Topic, err)
		return
	}

	switch obs := obs.(type) {
	case *metric.ServiceLatencyV1:
		observationsReceived.WithLabelValues(o.opts.ServiceName, obs.Responder.Name).Inc()
		serviceLatency.WithLabelValues(o.opts.ServiceName, obs.Responder.Name).Observe(obs.ServiceLatency.Seconds())
		totalLatency.WithLabelValues(o.opts.ServiceName, obs.Responder.Name).Observe(obs.TotalLatency.Seconds())
		requestorRTT.WithLabelValues(o.opts.ServiceName, obs.Responder.Name).Observe(obs.Requestor.RTT.Seconds())
		responderRTT.WithLabelValues(o.opts.ServiceName, obs.Responder.Name).Observe(obs.Responder.RTT.Seconds())
		systemRTT.WithLabelValues(o.opts.ServiceName, obs.Responder.Name).Observe(obs.SystemLatency.Seconds())

		if obs.Status == 0 {
			serviceRequestStatus.WithLabelValues(o.opts.ServiceName, "500").Inc()
		} else {
			serviceRequestStatus.WithLabelValues(o.opts.ServiceName, strconv.Itoa(obs.Status)).Inc()
		}

		if o.tracing {
			switch {
			case o.openTracingTracer != nil && len(obs.RequestHeader[trcUber]) > 0:
				o.processJaegerTrace(obs)

			case len(obs.RequestHeader[trcB3Sm]) > 0:
				// TODO zipkin not supported
			case len(obs.RequestHeader[trcTp]) > 0:
				// TODO tracecontext not supported
			}
		}

	default:
		invalidObservationsReceived.WithLabelValues(o.opts.ServiceName).Inc()
		log.Printf("Unsupported observation received on %s: %s", o.opts.Topic, kind)
		return
	}
}

func (o *ServiceObsListener) processJaegerTrace(obs *metric.ServiceLatencyV1) {
	ctx, err := o.openTracingTracer.Extract(opentracing.HTTPHeaders, opentracing.HTTPHeadersCarrier(obs.RequestHeader))
	if err != nil {
		log.Printf("Could not extract open tracing headers: %s", err)
		return
	}

	// overall request duration including all RTTs and processing times
	natsSpan := o.openTracingTracer.StartSpan("nats service", opentracing.FollowsFrom(ctx), opentracing.StartTime(obs.RequestStart))
	defer natsSpan.FinishWithOptions(opentracing.FinishOptions{FinishTime: obs.RequestStart.Add(obs.TotalLatency)})

	if obs.Error != "" {
		natsSpan.SetTag("error", true)
		natsSpan.SetTag("message", obs.Error)
	}

	requestStart := obs.RequestStart
	serviceComplete := requestStart.Add(obs.TotalLatency)

	// requestor rtt
	requestorStart := requestStart
	requestorRtt := requestorStart.Add(obs.Requestor.RTT)
	o.openTracingTracer.StartSpan("requestor connection", opentracing.ChildOf(natsSpan.Context()), opentracing.StartTime(requestorStart)).
		FinishWithOptions(opentracing.FinishOptions{FinishTime: requestorRtt})

	// nats network time
	systemStart := requestorStart.Add(obs.Requestor.RTT / 2)
	systemEnd := systemStart.Add(obs.SystemLatency)
	o.openTracingTracer.StartSpan("nats system", opentracing.ChildOf(natsSpan.Context()), opentracing.StartTime(systemStart)).
		FinishWithOptions(opentracing.FinishOptions{FinishTime: systemEnd})

	// service
	serviceRTTStart := serviceComplete.Add(-1 * obs.Requestor.RTT / 2).Add(-1 * obs.Responder.RTT).Add(-1 * obs.ServiceLatency)
	serviceRTTEnd := serviceRTTStart.Add(obs.Responder.RTT)
	serviceStart := serviceRTTStart.Add(obs.Responder.RTT / 2)
	serviceEnd := serviceStart.Add(obs.ServiceLatency)

	// service latency
	o.openTracingTracer.StartSpan("responder latency", opentracing.ChildOf(natsSpan.Context()), opentracing.StartTime(serviceStart)).
		FinishWithOptions(opentracing.FinishOptions{FinishTime: serviceEnd})

	// service rtt
	o.openTracingTracer.StartSpan("responder connection", opentracing.ChildOf(natsSpan.Context()), opentracing.StartTime(serviceRTTStart)).
		FinishWithOptions(opentracing.FinishOptions{FinishTime: serviceRTTEnd})
}

// Stop closes the connection to the network
func (o *ServiceObsListener) Stop() {
	if o.openTracingCloser != nil {
		o.openTracingCloser.Close()
	}

	o.nc.Close()
}
