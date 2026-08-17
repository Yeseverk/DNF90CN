package playerloop

import (
	"strings"

	"longheng.io/server/internal/platform/metrics"
)

const (
	// MetricPlayerLoopQueueLen 记录所有账号循环的待处理事件总数。
	MetricPlayerLoopQueueLen = "playerloop_queue_len"

	// MetricPlayerLoopLoopCount 记录当前活跃账号循环数量。
	MetricPlayerLoopLoopCount = "playerloop_loop_count"

	// MetricPlayerLoopSweepsTotal 记录空闲循环扫描累计次数。
	MetricPlayerLoopSweepsTotal = "playerloop_sweeps_total"

	// MetricPlayerLoopHandlerErrorsTotal 记录玩家事件 handler 错误累计数。
	MetricPlayerLoopHandlerErrorsTotal = "playerloop_handler_errors_total"
)

// MetricsObserver 是可写入平台指标注册表的玩家循环观测接口。
type MetricsObserver interface {
	ObserveMetrics(reg *metrics.Registry, labels map[string]string)
}

// ObserveMetrics 把队列长度、循环数量和错误计数写入指标注册表。
func (m *Manager) ObserveMetrics(reg *metrics.Registry, labels map[string]string) {
	if m == nil || reg == nil {
		return
	}
	var queued int64
	m.mu.RLock()
	loops := int64(len(m.loops))
	for _, playerLoop := range m.loops {
		if playerLoop != nil {
			queued += int64(len(playerLoop.ch))
		}
	}
	m.mu.RUnlock()

	tags := normMetricLabels(labels)
	if m.name != "" {
		tags = withMetricLabel(tags, "manager", m.name)
	}
	reg.SetGauge(MetricPlayerLoopQueueLen, tags, queued)
	reg.SetGauge(MetricPlayerLoopLoopCount, tags, loops)
	reg.SetCounter(MetricPlayerLoopSweepsTotal, tags, metrics.Int64FromUint64(m.sweepCount.Load()))
	reg.SetCounter(MetricPlayerLoopHandlerErrorsTotal, tags, metrics.Int64FromUint64(m.handlerErrorCount.Load()))
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

func withMetricLabel(labels map[string]string, key, value string) map[string]string {
	key = strings.TrimSpace(key)
	if key == "" {
		return labels
	}
	if len(labels) == 0 {
		return map[string]string{key: value}
	}
	out := make(map[string]string, len(labels)+1)
	for existingKey, existingValue := range labels {
		out[existingKey] = existingValue
	}
	out[key] = value
	return out
}
