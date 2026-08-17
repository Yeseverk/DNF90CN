package reload

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"
)

type Trigger string

const (
	TriggerManual Trigger = "manual"
	TriggerWatch  Trigger = "watch"
)

var ErrApplyRequired = errors.New("reload apply function is required")

type Config struct {
	Enabled      bool
	PollInterval time.Duration
	ApplyTimeout time.Duration
}

type Result struct {
	Trigger         string         `json:"trigger"`
	Reason          string         `json:"reason,omitempty"`
	At              time.Time      `json:"at"`
	DurationMS      int64          `json:"duration_ms"`
	Changed         bool           `json:"changed"`
	Applied         []string       `json:"applied,omitempty"`
	Skipped         []string       `json:"skipped,omitempty"`
	RestartRequired []string       `json:"restart_required,omitempty"`
	Details         map[string]any `json:"details,omitempty"`
	Error           string         `json:"error,omitempty"`
}

type Snapshot struct {
	Enabled              bool      `json:"enabled"`
	PollIntervalSeconds  int64     `json:"poll_interval_seconds"`
	ApplyTimeoutSeconds  int64     `json:"apply_timeout_seconds"`
	Checks               int64     `json:"checks"`
	Reloads              int64     `json:"reloads"`
	Failures             int64     `json:"failures"`
	LastCheckedAt        time.Time `json:"last_checked_at,omitempty"`
	LastSuccessfulReload time.Time `json:"last_successful_reload,omitempty"`
	LastResult           Result    `json:"last_result"`
}

type ApplyFunc func(context.Context, Trigger, string) (Result, error)

type Manager struct {
	name   string
	apply  ApplyFunc
	logger *slog.Logger

	mu       sync.Mutex
	applyMu  sync.Mutex
	cfg      Config
	snapshot Snapshot
	cancel   context.CancelFunc
	done     chan struct{}
}

func New(name string, cfg Config, apply ApplyFunc, logger *slog.Logger) *Manager {
	cfg = normalizeConfig(cfg)
	name = strings.TrimSpace(name)
	if name == "" {
		name = "config-reloader"
	}
	return &Manager{
		name:   name,
		cfg:    cfg,
		apply:  apply,
		logger: logger,
		snapshot: Snapshot{
			Enabled:             cfg.Enabled,
			PollIntervalSeconds: int64(cfg.PollInterval / time.Second),
			ApplyTimeoutSeconds: int64(cfg.ApplyTimeout / time.Second),
		},
	}
}

func (m *Manager) Name() string {
	if m == nil {
		return "config-reloader"
	}
	return m.name
}

func (m *Manager) Start(ctx context.Context) error {
	if m == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	if m.cancel != nil || !m.cfg.Enabled {
		m.mu.Unlock()
		return nil
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	interval := m.cfg.PollInterval
	m.cancel = cancel
	m.done = done
	m.mu.Unlock()

	go m.loop(runCtx, done, interval)
	return nil
}

func (m *Manager) Stop(ctx context.Context) error {
	if m == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	cancel := m.cancel
	done := m.done
	m.cancel = nil
	m.done = nil
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Manager) Configure(cfg Config) {
	if m == nil {
		return
	}
	cfg = normalizeConfig(cfg)
	m.mu.Lock()
	wasEnabled := m.cfg.Enabled
	changedInterval := m.cfg.PollInterval != cfg.PollInterval
	m.cfg = cfg
	m.snapshot.Enabled = cfg.Enabled
	m.snapshot.PollIntervalSeconds = int64(cfg.PollInterval / time.Second)
	m.snapshot.ApplyTimeoutSeconds = int64(cfg.ApplyTimeout / time.Second)
	cancel := m.cancel
	if wasEnabled && (!cfg.Enabled || changedInterval) {
		m.cancel = nil
		m.done = nil
	}
	m.mu.Unlock()

	if wasEnabled && (!cfg.Enabled || changedInterval) && cancel != nil {
		cancel()
	}
	if cfg.Enabled && (!wasEnabled || changedInterval) {
		_ = m.Start(context.Background())
	}
}

func (m *Manager) Reload(ctx context.Context, trigger Trigger, reason string) (Result, error) {
	if m == nil {
		return Result{}, ErrApplyRequired
	}
	m.applyMu.Lock()
	defer m.applyMu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	cfg := m.cfg
	m.snapshot.Checks++
	m.snapshot.LastCheckedAt = time.Now().UTC()
	m.mu.Unlock()

	if m.apply == nil {
		result := Result{Trigger: string(trigger), Reason: strings.TrimSpace(reason), At: time.Now().UTC(), Error: ErrApplyRequired.Error()}
		m.record(result, ErrApplyRequired)
		return result, ErrApplyRequired
	}

	startedAt := time.Now().UTC()
	applyCtx, cancel := context.WithTimeout(ctx, cfg.ApplyTimeout)
	defer cancel()
	result, err := m.apply(applyCtx, trigger, strings.TrimSpace(reason))
	result.Trigger = string(trigger)
	result.Reason = strings.TrimSpace(reason)
	if result.At.IsZero() {
		result.At = startedAt
	}
	result.DurationMS = time.Since(startedAt).Milliseconds()
	if err != nil && result.Error == "" {
		result.Error = err.Error()
	}
	m.record(result, err)
	return result, err
}

func (m *Manager) Snapshot() Snapshot {
	if m == nil {
		return Snapshot{}
	}
	m.mu.Lock()
	snapshot := m.snapshot
	snapshot.LastResult = cloneResult(snapshot.LastResult)
	m.mu.Unlock()
	return snapshot
}

func (m *Manager) loop(ctx context.Context, done chan struct{}, interval time.Duration) {
	defer close(done)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := m.Reload(ctx, TriggerWatch, "watch"); err != nil && m.logger != nil {
				m.logger.Warn("config reload failed", "error", err)
			}
		}
	}
}

func (m *Manager) record(result Result, err error) {
	m.mu.Lock()
	m.snapshot.LastResult = cloneResult(result)
	if result.Changed || len(result.Applied) > 0 {
		m.snapshot.Reloads++
		if err == nil {
			m.snapshot.LastSuccessfulReload = time.Now().UTC()
		}
	}
	if err != nil {
		m.snapshot.Failures++
	}
	m.mu.Unlock()
}

func normalizeConfig(cfg Config) Config {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 5 * time.Second
	}
	if cfg.ApplyTimeout <= 0 {
		cfg.ApplyTimeout = 5 * time.Second
	}
	return cfg
}

func cloneResult(result Result) Result {
	result.Applied = append([]string(nil), result.Applied...)
	result.Skipped = append([]string(nil), result.Skipped...)
	result.RestartRequired = append([]string(nil), result.RestartRequired...)
	if len(result.Details) > 0 {
		details := make(map[string]any, len(result.Details))
		for key, value := range result.Details {
			details[key] = cloneDetailValue(value)
		}
		result.Details = details
	}
	return result
}

func cloneDetailValue(value any) any {
	if value == nil {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		return value
	}
	return out
}
