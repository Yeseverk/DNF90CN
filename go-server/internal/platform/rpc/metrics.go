package rpc

import (
	"context"
	"errors"
	"strings"

	"longheng.io/server/internal/platform/metrics"
)

const (
	// MetricRPCPending 记录当前等待响应的 RPC 调用数量。
	MetricRPCPending = "rpc_pending"
	// MetricRPCHandlers 记录当前已注册的 RPC handler 数量。
	MetricRPCHandlers = "rpc_handlers_count"
	// MetricRPCStarted 记录 endpoint 是否处于启动状态。
	MetricRPCStarted = "rpc_started"
	// MetricRPCCallsTotal 记录 RPC 客户端调用总数。
	MetricRPCCallsTotal = "rpc_calls_total"
	// MetricRPCHandlerCallsTotal 记录 RPC 服务端 handler 调用总数。
	MetricRPCHandlerCallsTotal = "rpc_handler_calls_total"
)

// MetricsObserver 沿用 presence 的模式：bus / presence / rpc 都只暴露一个
// ObserveMetrics 方法，由平台 wiring 注册到 metrics.Registry，供 Prometheus scrape 统一 fanout。
type MetricsObserver interface {
	ObserveMetrics(reg *metrics.Registry, labels map[string]string)
}

// ObserveMetrics 发布 rpc_pending（进行中的客户端调用 gauge）、rpc_handlers_count
// （已注册服务端 handler gauge）和 rpc_started（0/1）。
// 它只获取 endpoint RLock，可安全在 /metrics scrape goroutine 中调用。
func (e *Endpoint) ObserveMetrics(reg *metrics.Registry, labels map[string]string) {
	if e == nil || reg == nil {
		return
	}
	e.mu.RLock()
	pending := int64(len(e.pending))
	handlers := int64(len(e.handlers))
	started := int64(0)
	if e.started {
		started = 1
	}
	e.mu.RUnlock()

	tags := normObserverLabels(labels)
	reg.SetGauge(MetricRPCPending, tags, pending)
	reg.SetGauge(MetricRPCHandlers, tags, handlers)
	reg.SetGauge(MetricRPCStarted, tags, started)
}

// SetMetrics 设置 endpoint 调用指标使用的 registry 和基础标签。
func (e *Endpoint) SetMetrics(reg *metrics.Registry, labels map[string]string) {
	if e == nil {
		return
	}
	e.mu.Lock()
	e.metrics = reg
	e.metricLabels = normObserverLabels(labels)
	e.mu.Unlock()
}

func (e *Endpoint) recordCallMetric(route string, err error) {
	e.recordRPCMetric(MetricRPCCallsTotal, route, err)
}

func (e *Endpoint) recordHandlerMetric(route string, err error) {
	e.recordRPCMetric(MetricRPCHandlerCallsTotal, route, err)
}

func (e *Endpoint) recordRPCMetric(name, route string, err error) {
	if e == nil {
		return
	}
	e.mu.RLock()
	reg := e.metrics
	labels := normObserverLabels(e.metricLabels)
	e.mu.RUnlock()
	if reg == nil {
		return
	}
	route = strings.TrimSpace(route)
	if route == "" {
		route = "unknown"
	}
	labels = withRPCMetricLabel(labels, "route", route)
	labels = withRPCMetricLabel(labels, "result", rpcMetricResult(err))
	reg.Inc(name, labels)
}

func rpcMetricResult(err error) string {
	if err == nil {
		return "ok"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline"
	}
	if errors.Is(err, ErrInvalidTarget) {
		return "invalid_target"
	}
	if errors.Is(err, ErrInvalidService) {
		return "invalid_service"
	}
	if errors.Is(err, ErrInvalidRoute) {
		return "invalid_route"
	}
	if errors.Is(err, ErrInvalidRequestID) {
		return "invalid_request"
	}
	if errors.Is(err, ErrInvalidNodeID) {
		return "invalid_node"
	}
	if errors.Is(err, ErrNoTargetNodes) {
		return "no_target"
	}
	if errors.Is(err, ErrTooManyPending) {
		return "too_many_pending"
	}
	if errors.Is(err, ErrPayloadTooLarge) {
		return "payload_too_large"
	}
	if errors.Is(err, ErrNotStarted) {
		return "not_started"
	}
	var remoteErr RemoteError
	if errors.As(err, &remoteErr) {
		return "remote_error"
	}
	if strings.Contains(err.Error(), "no rpc handler registered") {
		return "missing_handler"
	}
	return "error"
}

func withRPCMetricLabel(labels map[string]string, key, value string) map[string]string {
	next := normObserverLabels(labels)
	if next == nil {
		next = make(map[string]string, 1)
	}
	next[key] = value
	return next
}

func normObserverLabels(labels map[string]string) map[string]string {
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
