package bus

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"longheng.io/server/internal/platform/metrics"
)

func isContextCanceled(err error) bool { return errors.Is(err, context.Canceled) }
func isContextDeadline(err error) bool { return errors.Is(err, context.DeadlineExceeded) }

const (
	MetricPublishTotal   = "bus_publish_total"
	MetricPublishBytes   = "bus_publish_bytes_total"
	MetricSubscribeTotal = "bus_subscribe_total"
	MetricHandlerTotal   = "bus_handler_total"
	MetricSubscriptions  = "bus_subscriptions"

	ResultOK      = "ok"
	ResultError   = "error"
	ResultClosed  = "closed"
	ResultPanic   = "panic"
	ResultTimeout = "timeout"
	ResultCancel  = "canceled"
)

// Metrics 是各个 bus adapter 在热路径上调用的轻量指标层。
// nil receiver 是 no-op，因此即使指标 wiring 关闭，调用方也可以保留
// `m.recordX(...)`，无需到处写 nil 检查。
type Metrics struct {
	reg  *metrics.Registry
	kind string
}

// NewMetrics 返回绑定 registry 和 adapter kind 标签（"memory" / "redis" / "nats"）的指标发射器。
// 任一参数为空时返回 nil，让禁用指标的 wiring 保持真正的 no-op。
func NewMetrics(reg *metrics.Registry, kind string) *Metrics {
	kind = strings.TrimSpace(kind)
	if reg == nil || kind == "" {
		return nil
	}
	return &Metrics{reg: reg, kind: kind}
}

func (m *Metrics) recordPublish(result string) {
	if m == nil {
		return
	}
	m.reg.Inc(MetricPublishTotal, m.labels(result))
}

func (m *Metrics) recordPublishBytes(size int64) {
	if m == nil || size <= 0 {
		return
	}
	m.reg.Add(MetricPublishBytes, m.kindOnlyLabels(), size)
}

func (m *Metrics) recordSubscribe(result string) {
	if m == nil {
		return
	}
	m.reg.Inc(MetricSubscribeTotal, m.labels(result))
}

func (m *Metrics) recordHandler(result string) {
	if m == nil {
		return
	}
	m.reg.Inc(MetricHandlerTotal, m.labels(result))
}

func (m *Metrics) trackSubscription(delta int64) {
	if m == nil {
		return
	}
	m.reg.AddGauge(MetricSubscriptions, m.kindOnlyLabels(), delta)
}

func (m *Metrics) labels(result string) map[string]string {
	return map[string]string{
		"kind":   m.kind,
		"result": result,
	}
}

func (m *Metrics) kindOnlyLabels() map[string]string {
	return map[string]string{"kind": m.kind}
}

// classifyHandlerError 返回单次 handler 投递结果对应的 bus_handler_total result 标签。
// context.Canceled 归类为 "canceled" 而不是 "error"，避免调用方取消拉高错误率。
func classifyHandlerError(err error) string {
	if err == nil {
		return ResultOK
	}
	switch {
	case isContextCanceled(err):
		return ResultCancel
	case isContextDeadline(err):
		return ResultTimeout
	default:
		return ResultError
	}
}

func payloadSize(payload any) int64 {
	switch value := payload.(type) {
	case nil:
		return 0
	case []byte:
		return int64(len(value))
	case json.RawMessage:
		return int64(len(value))
	case string:
		return int64(len(value))
	default:
		raw, err := json.Marshal(value)
		if err != nil {
			return 0
		}
		return int64(len(raw))
	}
}
