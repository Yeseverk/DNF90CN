package cache

import (
	"strings"

	"longheng.io/server/internal/platform/metrics"
)

const (
	MetricHitsTotal      = "cache_hits_total"
	MetricMissesTotal    = "cache_misses_total"
	MetricEvictionsTotal = "cache_evictions_total"
)

type Metrics struct {
	reg  *metrics.Registry
	kind string
}

func NewMetrics(reg *metrics.Registry, kind string) *Metrics {
	kind = strings.TrimSpace(kind)
	if reg == nil || kind == "" {
		return nil
	}
	return &Metrics{reg: reg, kind: kind}
}

func (m *Metrics) recordHit() {
	if m == nil {
		return
	}
	m.reg.Inc(MetricHitsTotal, m.labels())
}

func (m *Metrics) recordMiss() {
	if m == nil {
		return
	}
	m.reg.Inc(MetricMissesTotal, m.labels())
}

func (m *Metrics) recordEviction() {
	if m == nil {
		return
	}
	m.reg.Inc(MetricEvictionsTotal, m.labels())
}

func (m *Metrics) labels() map[string]string {
	return map[string]string{"kind": m.kind}
}
