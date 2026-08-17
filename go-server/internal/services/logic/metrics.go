package logic

import (
	"longheng.io/server/internal/platform/eventloop"
	"longheng.io/server/internal/platform/metrics"
)

const (
	// MetricLogicPacketsTotal 记录 logic 收到的业务包总数。
	MetricLogicPacketsTotal = "logic_packets_total"
	// MetricLogicResponsesTotal 记录 logic 返回响应总数。
	MetricLogicResponsesTotal = "logic_responses_total"
	// MetricLogicPlayerLoopHandlerCallsTotal 记录玩家队列 handler 调用总数。
	MetricLogicPlayerLoopHandlerCallsTotal = "logic_playerloop_handler_calls_total"
	// MetricLogicPlayerLoopHandlerLatencyMaxMillis 记录玩家队列 handler 最大延迟毫秒值。
	MetricLogicPlayerLoopHandlerLatencyMaxMillis = "logic_playerloop_handler_latency_max_milliseconds"
)

func (s *Service) regRuntimeMetrics() {
	if s == nil || s.metrics == nil {
		return
	}
	labels := map[string]string{
		"service": s.metricServiceLabel,
		"node_id": s.nodeID,
	}
	if s.dispatcher != nil {
		s.metrics.RegisterObserver(s.metricServiceLabel+"-dispatch", func(reg *metrics.Registry) {
			s.dispatcher.ObserveMetrics(reg, labels)
		})
	}
	if s.playerLoops != nil {
		s.metrics.RegisterObserver(s.metricServiceLabel+"-playerloops", func(reg *metrics.Registry) {
			s.playerLoops.ObserveMetrics(reg, labels)
		})
	}
}

func (s *Service) regEventLoopMetrics(loop *eventloop.Loop) {
	if s == nil || s.metrics == nil || loop == nil {
		return
	}
	labels := map[string]string{
		"service": s.metricServiceLabel,
		"node_id": s.nodeID,
	}
	s.metrics.RegisterObserver(s.metricServiceLabel+"-metric-eventloop", func(reg *metrics.Registry) {
		loop.ObserveMetrics(reg, labels)
	})
}
