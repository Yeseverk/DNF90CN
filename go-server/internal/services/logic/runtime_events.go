package logic

import (
	"context"
	"errors"
	"strings"
	"time"

	"longheng.io/server/internal/platform/dispatch"
	"longheng.io/server/internal/platform/eventloop"
	"longheng.io/server/pkg/contracts"
	"longheng.io/server/pkg/protocol"
)

const (
	asyncPublishTimeout  = 3 * time.Second
	logicHandlerTimeout  = 5 * time.Second
	logicMetricEventType = "logic.metric.inc"
	logicDrainWakeType   = "logic.metric.drain_wake"
)

type logicMetricEvent struct {
	Name   string
	Labels map[string]string
}

func (s *Service) handleSessConnected(ctx context.Context, evt contracts.SessionConnected) error {
	evt.AccountID = strings.TrimSpace(evt.AccountID)
	evt.SessionID = strings.TrimSpace(evt.SessionID)
	if s.accounts == nil {
		return nil
	}
	profile, err := s.accounts.Connect(ctx, evt)
	if err != nil {
		return err
	}
	if s.world != nil {
		s.world.ObservePlayer(profile.AccountID, profile.State)
	}
	return nil
}

func (s *Service) handleSessionDisc(ctx context.Context, evt contracts.SessionDisconnected) error {
	evt.AccountID = strings.TrimSpace(evt.AccountID)
	evt.SessionID = strings.TrimSpace(evt.SessionID)
	if s.accounts == nil {
		return nil
	}
	profile, saved, err := s.accounts.Disconnect(ctx, evt)
	if err != nil {
		return err
	}
	if saved && s.world != nil {
		s.world.ObservePlayer(profile.AccountID, profile.State)
	}
	return nil
}

func (s *Service) newLogicDispatcher() (*dispatch.Mux, error) {
	mux := dispatch.NewWithOptions(dispatch.MuxOptions{HandlerTimeout: logicHandlerTimeout})
	if s.registerHandlers != nil {
		if err := s.registerHandlers(mux); err != nil {
			return nil, err
		}
	}
	mux.HandleDefault(func(_ context.Context, req dispatch.Request) (dispatch.Response, error) {
		return dispatch.Response{}, protocol.Errorf(protocol.CodeNotImplemented, "unsupported msg_id %d", req.MsgID)
	})
	return mux, nil
}

func (s *Service) incMetric(name string, labels map[string]string) {
	if s.metrics == nil {
		return
	}
	s.metrics.Inc(name, s.metricLabels(labels))
}

func (s *Service) metricLabels(labels map[string]string) map[string]string {
	out := make(map[string]string, len(labels)+2)
	out["service"] = s.metricServiceLabel
	if s.nodeID != "" {
		out["node_id"] = s.nodeID
	}
	for key, value := range labels {
		out[key] = value
	}
	return out
}

func (s *Service) incMetricLow(name string, labels map[string]string) {
	if s == nil || s.metricEventLoop == nil {
		s.incMetric(name, labels)
		return
	}
	event := eventloop.Event{
		Type:     logicMetricEventType,
		Priority: eventloop.PriorityLow,
		Data: logicMetricEvent{
			Name:   name,
			Labels: cloneMetricLabels(labels),
		},
	}
	if err := s.metricEventLoop.Submit(event); err != nil {
		s.incMetric(name, labels)
	}
}

func (s *Service) startMetricEventLoop(ctx context.Context) error {
	if s == nil || s.metrics == nil {
		return nil
	}
	if s.metricEventLoop != nil {
		return nil
	}
	loop := eventloop.New("logic-metrics", eventloop.ProcessorFunc(func(_ context.Context, event eventloop.Event) eventloop.Result {
		metric, ok := event.Data.(logicMetricEvent)
		if !ok || event.Type != logicMetricEventType {
			return eventloop.Result{}
		}
		s.incMetric(metric.Name, metric.Labels)
		return eventloop.Result{}
	}), eventloop.Options{QueueSize: 1024, Logger: s.logger})
	if err := loop.Start(ctx); err != nil {
		return err
	}
	s.metricEventLoop = loop
	s.regEventLoopMetrics(loop)
	return nil
}

func (s *Service) drainMetricEventLoop(ctx context.Context) error {
	if s == nil || s.metricEventLoop == nil {
		return nil
	}
	_ = s.metricEventLoop.Submit(eventloop.Event{Type: logicDrainWakeType, Priority: eventloop.PriorityLow})
	err := s.metricEventLoop.Drain(ctx)
	if errors.Is(err, eventloop.ErrNotStarted) || errors.Is(err, eventloop.ErrClosed) {
		err = nil
	}
	if err == nil {
		s.metricEventLoop = nil
	}
	return err
}

func cloneMetricLabels(labels map[string]string) map[string]string {
	if len(labels) == 0 {
		return nil
	}
	out := make(map[string]string, len(labels))
	for key, value := range labels {
		out[key] = value
	}
	return out
}

func (s *Service) publishAsync(topic string, payload any) {
	pub := s.ensureEventPub()
	if pub == nil {
		return
	}
	if err := pub.Publish(topic, payload); err != nil && s.logger != nil {
		s.logger.Error("logic async publish enqueue failed", "topic", topic, "error", err)
	}
}

func (s *Service) ensureEventPub() *eventPub {
	if s == nil || s.bus == nil {
		return nil
	}
	s.eventOutMu.Lock()
	defer s.eventOutMu.Unlock()
	if s.eventOut == nil {
		s.eventOut = newEventPub(s.bus, s.logger)
	}
	return s.eventOut
}

func (s *Service) eventPublisher() *eventPub {
	if s == nil {
		return nil
	}
	s.eventOutMu.Lock()
	defer s.eventOutMu.Unlock()
	return s.eventOut
}
