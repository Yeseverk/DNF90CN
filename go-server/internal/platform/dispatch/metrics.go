package dispatch

import (
	"strings"

	"longheng.io/server/internal/platform/metrics"
)

const (
	MetricDispatchHandlerCount = "dispatch_handler_count"
	MetricDispatchSlowCount    = "dispatch_slow_count"
	MetricDispatchPlayerStats  = "dispatch_player_stats_count"
)

type MetricsObserver interface {
	ObserveMetrics(reg *metrics.Registry, labels map[string]string)
}

func (m *Mux) ObserveMetrics(reg *metrics.Registry, labels map[string]string) {
	if m == nil || reg == nil {
		return
	}
	m.mu.RLock()
	handlers := int64(len(m.handlers))
	playerStats := int64(len(m.playerStats))
	m.mu.RUnlock()
	tags := normMetricLabels(labels)
	reg.SetGauge(MetricDispatchHandlerCount, tags, handlers)
	reg.SetGauge(MetricDispatchPlayerStats, tags, playerStats)
	reg.SetCounter(MetricDispatchSlowCount, tags, metrics.Int64FromUint64(m.slowCount.Load()))
}

func normMetricLabels(labels map[string]string) map[string]string {
	if len(labels) == 0 {
		return nil
	}
	out := make(map[string]string, len(labels))
	for key, value := range labels {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
