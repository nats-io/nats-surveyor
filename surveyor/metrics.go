package surveyor

import (
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

type MetricInfo interface {
	// Get the fully qualified name of the metric
	Name() string

	// Get the help string of the metric
	Help() string

	// Get the type of the metric
	Type() dto.MetricType
}

type Metric interface {
	MetricInfo

	// Get the underlying metric
	Metric() prometheus.Metric
}

type Gauge struct {
	prometheus.Gauge
	name string
	help string
}

var _ Metric = &Gauge{}

func newGauge(name string, help string, constLabels prometheus.Labels) *Gauge {
	metric := prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        name,
		Help:        help,
		ConstLabels: constLabels,
	})
	return &Gauge{
		Gauge: metric,
		name:  name,
		help:  help,
	}
}

func (g *Gauge) Metric() prometheus.Metric {
	return g.Gauge
}

func (g *Gauge) Name() string {
	return g.name
}

func (g *Gauge) Help() string {
	return g.help
}

func (g *Gauge) Type() dto.MetricType {
	return dto.MetricType_GAUGE
}

type MetricVec interface {
	MetricInfo

	// Get the underlying metric vector
	Vec() *prometheus.MetricVec
}

type CounterVec struct {
	*prometheus.CounterVec
	name string
	help string
}

var _ MetricVec = &CounterVec{}

func newCounterVec(name string, help string, labelNames []string, constLabels prometheus.Labels) *CounterVec {
	vec := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:        name,
		Help:        help,
		ConstLabels: constLabels,
	}, labelNames)
	return &CounterVec{
		CounterVec: vec,
		name:       name,
		help:       help,
	}
}

func (c *CounterVec) Vec() *prometheus.MetricVec {
	return c.CounterVec.MetricVec
}

func (c *CounterVec) Name() string {
	return c.name
}

func (c *CounterVec) Help() string {
	return c.help
}

func (c *CounterVec) Type() dto.MetricType {
	return dto.MetricType_COUNTER
}

type GaugeVec struct {
	*prometheus.GaugeVec
	name string
	help string
}

var _ MetricVec = &GaugeVec{}

func newGaugeVec(name string, help string, labelNames []string, constLabels prometheus.Labels) *GaugeVec {
	vec := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        name,
		Help:        help,
		ConstLabels: constLabels,
	}, labelNames)
	return &GaugeVec{
		GaugeVec: vec,
		name:     name,
		help:     help,
	}
}

func (g *GaugeVec) Vec() *prometheus.MetricVec {
	return g.GaugeVec.MetricVec
}

func (g *GaugeVec) Name() string {
	return g.name
}

func (g *GaugeVec) Help() string {
	return g.help
}

func (g *GaugeVec) Type() dto.MetricType {
	return dto.MetricType_GAUGE
}

type SummaryVec struct {
	*prometheus.SummaryVec
	name string
	help string
}

var _ MetricVec = &SummaryVec{}

func newSummaryVec(name string, help string, labelNames []string, constLabels prometheus.Labels) *SummaryVec {
	vec := prometheus.NewSummaryVec(prometheus.SummaryOpts{
		Name:        name,
		Help:        help,
		ConstLabels: constLabels,
	}, labelNames)
	return &SummaryVec{
		SummaryVec: vec,
		name:       name,
		help:       help,
	}
}

func (s *SummaryVec) Vec() *prometheus.MetricVec {
	return s.SummaryVec.MetricVec
}

func (s *SummaryVec) Name() string {
	return s.name
}

func (s *SummaryVec) Help() string {
	return s.help
}

func (s *SummaryVec) Type() dto.MetricType {
	return dto.MetricType_SUMMARY
}

type HistogramVec struct {
	*prometheus.HistogramVec
	name string
	help string
}

var _ MetricVec = &HistogramVec{}

func newHistogramVec(name string, help string, labelNames []string, constLabels prometheus.Labels) *HistogramVec {
	vec := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:        name,
		Help:        help,
		ConstLabels: constLabels,
	}, labelNames)
	return &HistogramVec{
		HistogramVec: vec,
		name:         name,
		help:         help,
	}
}

func (h *HistogramVec) Vec() *prometheus.MetricVec {
	return h.HistogramVec.MetricVec
}

func (h *HistogramVec) Name() string {
	return h.name
}

func (h *HistogramVec) Help() string {
	return h.help
}

func (h *HistogramVec) Type() dto.MetricType {
	return dto.MetricType_HISTOGRAM
}
