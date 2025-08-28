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

	// Get the constant labels of the metric
	ConstLabels() prometheus.Labels

	// Get the description of the metric
	Desc() *prometheus.Desc
}

type Metric interface {
	MetricInfo

	// Get the underlying metric
	Metric() prometheus.Metric
}

// generic metric type that can be used to wrap any prometheus metric
type genericMetric struct {
	metric      prometheus.Metric
	name        string
	help        string
	typ         dto.MetricType
	constLabels prometheus.Labels
}

var _ Metric = &genericMetric{}

func newMetric(m prometheus.Metric, name string, help string, typ dto.MetricType, constLabels prometheus.Labels) *genericMetric {
	return &genericMetric{
		metric:      m,
		name:        name,
		help:        help,
		typ:         typ,
		constLabels: constLabels,
	}
}

func (m *genericMetric) Metric() prometheus.Metric {
	return m.metric
}

func (m *genericMetric) Name() string {
	return m.name
}

func (m *genericMetric) Help() string {
	return m.help
}

func (m *genericMetric) Type() dto.MetricType {
	return m.typ
}

func (m *genericMetric) ConstLabels() prometheus.Labels {
	return m.constLabels
}

func (m *genericMetric) Desc() *prometheus.Desc {
	return prometheus.NewDesc(m.name, m.help, nil, m.constLabels)
}

type Gauge struct {
	prometheus.Gauge
	name        string
	help        string
	constLabels prometheus.Labels
}

var _ Metric = &Gauge{}

func newGauge(name string, help string, constLabels prometheus.Labels) *Gauge {
	metric := prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        name,
		Help:        help,
		ConstLabels: constLabels,
	})
	return &Gauge{
		Gauge:       metric,
		name:        name,
		help:        help,
		constLabels: constLabels,
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

func (g *Gauge) ConstLabels() prometheus.Labels {
	return g.constLabels
}

func (g *Gauge) Desc() *prometheus.Desc {
	return prometheus.NewDesc(g.name, g.help, nil, g.constLabels)
}

type MetricVec interface {
	MetricInfo

	// Get the underlying metric vector
	Vec() *prometheus.MetricVec
}

type CounterVec struct {
	*prometheus.CounterVec
	name        string
	help        string
	constLabels prometheus.Labels
	labelNames  []string
}

var _ MetricVec = &CounterVec{}

func newCounterVec(name string, help string, constLabels prometheus.Labels, labelNames []string) *CounterVec {
	vec := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:        name,
		Help:        help,
		ConstLabels: constLabels,
	}, labelNames)
	return &CounterVec{
		CounterVec:  vec,
		name:        name,
		help:        help,
		constLabels: constLabels,
		labelNames:  labelNames,
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

func (c *CounterVec) ConstLabels() prometheus.Labels {
	return c.constLabels
}

func (c *CounterVec) Desc() *prometheus.Desc {
	return prometheus.NewDesc(c.name, c.help, c.labelNames, c.constLabels)
}

type GaugeVec struct {
	*prometheus.GaugeVec
	name        string
	help        string
	constLabels prometheus.Labels
	labelNames  []string
}

var _ MetricVec = &GaugeVec{}

func newGaugeVec(name string, help string, constLabels prometheus.Labels, labelNames []string) *GaugeVec {
	vec := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        name,
		Help:        help,
		ConstLabels: constLabels,
	}, labelNames)
	return &GaugeVec{
		GaugeVec:    vec,
		name:        name,
		help:        help,
		constLabels: constLabels,
		labelNames:  labelNames,
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

func (g *GaugeVec) ConstLabels() prometheus.Labels {
	return g.constLabels
}

func (g *GaugeVec) Desc() *prometheus.Desc {
	return prometheus.NewDesc(g.name, g.help, g.labelNames, g.constLabels)
}

type SummaryVec struct {
	*prometheus.SummaryVec
	name        string
	help        string
	constLabels prometheus.Labels
	labelNames  []string
}

var _ MetricVec = &SummaryVec{}

func newSummaryVec(name string, help string, constLabels prometheus.Labels, labelNames []string) *SummaryVec {
	vec := prometheus.NewSummaryVec(prometheus.SummaryOpts{
		Name:        name,
		Help:        help,
		ConstLabels: constLabels,
	}, labelNames)
	return &SummaryVec{
		SummaryVec:  vec,
		name:        name,
		help:        help,
		constLabels: constLabels,
		labelNames:  labelNames,
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

func (s *SummaryVec) ConstLabels() prometheus.Labels {
	return s.constLabels
}

func (s *SummaryVec) Desc() *prometheus.Desc {
	return prometheus.NewDesc(s.name, s.help, s.labelNames, s.constLabels)
}

type HistogramVec struct {
	*prometheus.HistogramVec
	name        string
	help        string
	constLabels prometheus.Labels
	labelNames  []string
}

var _ MetricVec = &HistogramVec{}

func newHistogramVec(name string, help string, constLabels prometheus.Labels, labelNames []string) *HistogramVec {
	vec := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:        name,
		Help:        help,
		ConstLabels: constLabels,
	}, labelNames)
	return &HistogramVec{
		HistogramVec: vec,
		name:         name,
		help:         help,
		constLabels:  constLabels,
		labelNames:   labelNames,
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

func (h *HistogramVec) ConstLabels() prometheus.Labels {
	return h.constLabels
}

func (h *HistogramVec) Desc() *prometheus.Desc {
	return prometheus.NewDesc(h.name, h.help, h.labelNames, h.constLabels)
}
