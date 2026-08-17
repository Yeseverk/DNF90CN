package logic

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"longheng.io/server/internal/platform/dispatch"
)

func (s *Service) observeLoopHandler(meta dispatch.HandlerMeta, msgID uint32, duration time.Duration, err error) {
	if s == nil || s.metrics == nil {
		return
	}
	result := "ok"
	if err != nil {
		result = "error"
	}
	labels := map[string]string{
		"handler": handlerMetricName(meta, msgID),
		"msg_id":  fmt.Sprint(msgID),
		"result":  result,
	}
	s.incMetric(MetricLogicPlayerLoopHandlerCallsTotal, labels)
	s.setMetricGaugeMax(MetricLogicPlayerLoopHandlerLatencyMaxMillis, labels, durationMillis(duration))
}

func handlerMetricName(meta dispatch.HandlerMeta, msgID uint32) string {
	if name := strings.TrimSpace(meta.Name); name != "" {
		return name
	}
	if msgID != 0 {
		return fmt.Sprintf("msg_%d", msgID)
	}
	return "unknown"
}

func durationMillis(duration time.Duration) int64 {
	if duration <= 0 {
		return 0
	}
	millis := duration.Milliseconds()
	if millis == 0 {
		return 1
	}
	return millis
}

func (s *Service) setMetricGaugeMax(name string, labels map[string]string, value int64) {
	if s == nil || s.metrics == nil || name == "" {
		return
	}
	out := s.metricLabels(labels)
	key := metricSampleKey(name, out)
	s.handlerMetricMu.Lock()
	if s.handlerLatencyMax == nil {
		s.handlerLatencyMax = make(map[string]int64)
	}
	current, ok := s.handlerLatencyMax[key]
	if ok && value <= current {
		s.handlerMetricMu.Unlock()
		return
	}
	s.handlerLatencyMax[key] = value
	s.handlerMetricMu.Unlock()
	s.metrics.SetGauge(name, out, value)
}

func metricSampleKey(name string, labels map[string]string) string {
	if len(labels) == 0 {
		return name
	}
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var b strings.Builder
	b.WriteString(name)
	for _, key := range keys {
		b.WriteByte('\x00')
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(labels[key])
	}
	return b.String()
}
