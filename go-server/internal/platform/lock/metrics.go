package lock

import (
	"context"
	"errors"
	"strings"

	"longheng.io/server/internal/platform/metrics"
)

const (
	// MetricAcquireTotal 统计锁获取结果次数。
	MetricAcquireTotal = "lock_acquire_total"

	// ResultOK 表示加锁成功。
	ResultOK = "ok"
	// ResultHeld 表示锁已被其他有效租约持有。
	ResultHeld = "held"
	// ResultCanceled 表示加锁请求被上下文取消或超时。
	ResultCanceled = "canceled"
	// ResultError 表示加锁过程中发生其他错误。
	ResultError = "error"
)

// Metrics 封装锁模块向框架指标注册表写入的计数器。
type Metrics struct {
	reg  *metrics.Registry
	kind string
}

// NewMetrics 创建锁模块指标采集器；缺少注册表或类型时返回 nil。
func NewMetrics(reg *metrics.Registry, kind string) *Metrics {
	kind = strings.TrimSpace(kind)
	if reg == nil || kind == "" {
		return nil
	}
	return &Metrics{reg: reg, kind: kind}
}

func (m *Metrics) recordAcquire(result string) {
	if m == nil {
		return
	}
	m.reg.Inc(MetricAcquireTotal, map[string]string{"kind": m.kind, "result": result})
}

func classifyAcquire(err error) string {
	if err == nil {
		return ResultOK
	}
	switch {
	case errors.Is(err, ErrLockHeld):
		return ResultHeld
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return ResultCanceled
	default:
		return ResultError
	}
}
