package eventloop

import (
	"strings"

	"longheng.io/server/internal/platform/metrics"
)

const (
	// MetricEventLoopQueueLen 记录事件循环各优先级队列长度。
	MetricEventLoopQueueLen = "eventloop_queue_len"
	// MetricEventLoopDroppedTotal 记录提交或回调投递被丢弃的事件数量。
	MetricEventLoopDroppedTotal = "eventloop_dropped_total"
	// MetricEventLoopPanickedTotal 记录处理器 panic 被恢复的次数。
	MetricEventLoopPanickedTotal = "eventloop_panicked_total"
	// MetricEventLoopSlowTotal 记录超过慢事件阈值的处理次数。
	MetricEventLoopSlowTotal = "eventloop_slow_total"
	// MetricEventLoopCallbackQueued 记录回调队列长度。
	MetricEventLoopCallbackQueued = "eventloop_callback_queue_len"
)

// MetricsObserver 表示可向框架指标注册表导出观测值的组件。
type MetricsObserver interface {
	ObserveMetrics(reg *metrics.Registry, labels map[string]string)
}

// ObserveMetrics 将事件循环队列长度和累计计数写入指标注册表。
func (l *Loop) ObserveMetrics(reg *metrics.Registry, labels map[string]string) {
	if l == nil || reg == nil {
		return
	}
	queueLengths := l.QueueLengths()
	snapshot := l.MetricsSnapshot()
	base := normMetricLabels(labels)
	if l.name != "" {
		base = withMetricLabel(base, "loop", l.name)
	}
	for _, priority := range []Priority{PriorityHigh, PriorityMedium, PriorityLow} {
		priorityLabels := withMetricLabel(base, "priority", priority.String())
		reg.SetGauge(MetricEventLoopQueueLen, priorityLabels, int64(queueLenPriority(queueLengths, priority)))
		reg.SetCounter(MetricEventLoopDroppedTotal, priorityLabels, metrics.Int64FromUint64(snapshot.Dropped[priority]))
		reg.SetCounter(MetricEventLoopPanickedTotal, priorityLabels, metrics.Int64FromUint64(snapshot.Panicked[priority]))
		reg.SetCounter(MetricEventLoopSlowTotal, priorityLabels, metrics.Int64FromUint64(snapshot.Slow[priority]))
	}
	reg.SetGauge(MetricEventLoopCallbackQueued, base, int64(queueLengths.Callback))
}

func queueLenPriority(lengths QueueLengths, priority Priority) int {
	switch priority {
	case PriorityHigh:
		return lengths.High
	case PriorityLow:
		return lengths.Low
	default:
		return lengths.Medium
	}
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
