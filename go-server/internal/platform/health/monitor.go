package health

import (
	"context"
	"strings"
	"sync"
	"time"

	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

const (
	defMonitorEvery   = 5 * time.Second
	defMonitorTimeout = 2 * time.Second
)

type ProbeTarget struct {
	Name    string
	Service string
	Client  healthpb.HealthClient
}

type ProbeResult struct {
	Target    string    `json:"target"`
	Service   string    `json:"service,omitempty"`
	State     State     `json:"state"`
	Error     string    `json:"error,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}

type MonitorOptions struct {
	Interval time.Duration
	Timeout  time.Duration
	Targets  []ProbeTarget
	Notify   func(ProbeResult)
	Now      func() time.Time
}

type Monitor struct {
	interval time.Duration
	timeout  time.Duration
	targets  []ProbeTarget
	notify   func(ProbeResult)
	now      func() time.Time

	mu   sync.Mutex
	last map[string]State
}

func NewMonitor(options MonitorOptions) *Monitor {
	interval := options.Interval
	if interval <= 0 {
		interval = defMonitorEvery
	}
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = defMonitorTimeout
	}
	now := options.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	targets := make([]ProbeTarget, 0, len(options.Targets))
	for _, target := range options.Targets {
		target.Name = strings.TrimSpace(target.Name)
		target.Service = strings.TrimSpace(target.Service)
		if target.Name == "" {
			target.Name = target.Service
		}
		if target.Name == "" {
			continue
		}
		targets = append(targets, target)
	}
	return &Monitor{
		interval: interval,
		timeout:  timeout,
		targets:  targets,
		notify:   options.Notify,
		now:      now,
		last:     make(map[string]State),
	}
}

func (m *Monitor) CheckOnce(ctx context.Context) []ProbeResult {
	if m == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	results := make([]ProbeResult, 0, len(m.targets))
	for _, target := range m.targets {
		result := m.probe(ctx, target)
		results = append(results, result)
		m.notifyIfChanged(result)
	}
	return results
}

func (m *Monitor) Run(ctx context.Context) error {
	if m == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	m.CheckOnce(ctx)
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			m.CheckOnce(ctx)
		}
	}
}

func (m *Monitor) probe(ctx context.Context, target ProbeTarget) ProbeResult {
	probeCtx := ctx
	cancel := func() {}
	if m.timeout > 0 {
		probeCtx, cancel = context.WithTimeout(ctx, m.timeout)
	}
	defer cancel()
	state, err := ProbeGRPC(probeCtx, target.Client, target.Service)
	result := ProbeResult{
		Target:    target.Name,
		Service:   target.Service,
		State:     state,
		CheckedAt: m.now().UTC(),
	}
	if err != nil {
		result.Error = err.Error()
	}
	return result
}

func (m *Monitor) notifyIfChanged(result ProbeResult) {
	if m.notify == nil {
		return
	}
	key := result.Target + "\x00" + result.Service
	m.mu.Lock()
	last, ok := m.last[key]
	if ok && last == result.State {
		m.mu.Unlock()
		return
	}
	m.last[key] = result.State
	m.mu.Unlock()
	m.notify(result)
}
