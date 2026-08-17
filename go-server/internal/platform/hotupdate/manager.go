package hotupdate

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"
)

type ManagerOptions struct {
	Target        string
	NodeID        string
	Workspace     string
	ApplyTimeout  time.Duration
	ReportTimeout time.Duration
	Control       Control
	Source        Source
	Applier       Applier
	Logger        *slog.Logger
	Now           func() time.Time
}

type Manager struct {
	target        string
	nodeID        string
	workspace     string
	applyTimeout  time.Duration
	reportTimeout time.Duration
	control       Control
	source        Source
	applier       Applier
	logger        *slog.Logger
	now           func() time.Time

	mu       sync.RWMutex
	started  bool
	cancel   context.CancelFunc
	done     chan struct{}
	snapshot Snapshot

	lifecycleCtx     context.Context
	lifecycleCancel  context.CancelFunc
	autoRestoreTimer *time.Timer
}

type patchWindowApplier interface {
	PatchLiveDuration() time.Duration
}

func NewManager(options ManagerOptions) (*Manager, error) {
	target, err := normalizeTarget(options.Target)
	if err != nil {
		return nil, err
	}
	if options.Control == nil {
		options.Control = NewMemoryControl()
	}
	if options.Source == nil {
		options.Source = LocalSource{}
	}
	if options.Applier == nil {
		return nil, ErrApplyRequired
	}
	now := options.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	nodeID := strings.TrimSpace(options.NodeID)
	if nodeID == "" {
		nodeID = "local"
	}
	reportTimeout := options.ReportTimeout
	if reportTimeout <= 0 {
		reportTimeout = 30 * time.Second
	}
	lifecycleCtx, lifecycleCancel := context.WithCancel(context.Background())
	return &Manager{
		target:          target,
		nodeID:          nodeID,
		workspace:       strings.TrimSpace(options.Workspace),
		applyTimeout:    options.ApplyTimeout,
		reportTimeout:   reportTimeout,
		control:         options.Control,
		source:          options.Source,
		applier:         options.Applier,
		logger:          options.Logger,
		now:             now,
		lifecycleCtx:    lifecycleCtx,
		lifecycleCancel: lifecycleCancel,
		snapshot: Snapshot{
			Target: target,
			NodeID: nodeID,
		},
	}, nil
}

func (m *Manager) Name() string {
	if m == nil {
		return "hot-update"
	}
	return "hot-update-" + m.target
}

func (m *Manager) Start(ctx context.Context) error {
	if m == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return nil
	}
	m.ensureLifeLocked()
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	m.started = true
	m.cancel = cancel
	m.done = done
	m.snapshot.Started = true
	m.mu.Unlock()
	// Manager 只监听控制面 intent；真正下载、校验、替换由 source/applier 执行，便于替换成 etcd/SQL 控制面。
	go m.loop(runCtx, done)
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
	autoRestoreTimer := m.autoRestoreTimer
	lifecycleCancel := m.lifecycleCancel
	m.cancel = nil
	m.done = nil
	m.autoRestoreTimer = nil
	m.lifecycleCtx = nil
	m.lifecycleCancel = nil
	m.started = false
	m.snapshot.Started = false
	m.snapshot.AutoRestoreAt = time.Time{}
	m.mu.Unlock()
	if lifecycleCancel != nil {
		lifecycleCancel()
	}
	if autoRestoreTimer != nil {
		autoRestoreTimer.Stop()
	}
	// Stop 取消 watcher 和自动恢复计时器，并等待 loop 退出，避免停机后继续应用热更。
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

func (m *Manager) Process(ctx context.Context, intent Intent) (ApplyResult, error) {
	if m == nil {
		return ApplyResult{}, errors.New("hot update manager is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	intent, err := normalizeIntent(intent)
	if err != nil {
		return ApplyResult{}, err
	}
	// Process 是单个 intent 的状态机入口：restore 走回滚，其余动作按下载、校验、应用、报告推进。
	m.setIntent(intent)
	m.report(ctx, intent, StageAccepted, "ok", "")
	if intent.Action == ActionRestore {
		return m.restore(ctx, intent)
	}
	m.report(ctx, intent, StageDownloading, "ok", "")
	pkg, err := m.source.Fetch(ctx, intent, m.workspace)
	if err != nil {
		m.fail(ctx, intent, err)
		return ApplyResult{}, err
	}
	m.setPackage(pkg)
	if wait := time.Until(intent.AvailableAt); !intent.AvailableAt.IsZero() && wait > 0 {
		m.report(ctx, intent, StageScheduled, "ok", intent.AvailableAt.Format(time.RFC3339))
		timer := time.NewTimer(wait)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			m.fail(ctx, intent, ctx.Err())
			return ApplyResult{}, ctx.Err()
		}
	}
	m.report(ctx, intent, StageApplying, "ok", "")
	applyCtx := ctx
	cancel := func() {}
	if m.applyTimeout > 0 {
		applyCtx, cancel = context.WithTimeout(ctx, m.applyTimeout)
	}
	defer cancel()
	result, err := m.applier.Apply(applyCtx, pkg)
	if err != nil {
		m.fail(ctx, intent, err)
		return ApplyResult{}, err
	}
	m.mu.Lock()
	m.snapshot.LastApplied = m.now()
	m.mu.Unlock()
	m.report(ctx, intent, StageApplied, "ok", "")
	m.scheduleAutoRestore(intent)
	return result, nil
}

func (m *Manager) restore(ctx context.Context, intent Intent) (ApplyResult, error) {
	restorer, ok := m.applier.(Restorer)
	if !ok {
		err := ErrRestoreRequired
		m.fail(ctx, intent, err)
		return ApplyResult{}, err
	}
	m.cancelAutoRestore()
	// restore 会取消自动恢复计时器，避免手动回滚和定时回滚并发执行。
	m.report(ctx, intent, StageRestoring, "ok", "")
	applyCtx := ctx
	cancel := func() {}
	if m.applyTimeout > 0 {
		applyCtx, cancel = context.WithTimeout(ctx, m.applyTimeout)
	}
	defer cancel()
	result, err := restorer.Restore(applyCtx, intent)
	if err != nil {
		m.fail(ctx, intent, err)
		return ApplyResult{}, err
	}
	m.mu.Lock()
	m.snapshot.LastApplied = m.now()
	m.mu.Unlock()
	m.report(ctx, intent, StageRestored, "ok", "")
	return result, nil
}

func (m *Manager) NativePatchEnabled() bool {
	if m == nil {
		return false
	}
	_, ok := m.applier.(NativePatchApplier)
	return ok
}

func (m *Manager) Snapshot() Snapshot {
	if m == nil {
		return Snapshot{}
	}
	m.mu.RLock()
	snapshot := m.snapshot
	m.mu.RUnlock()
	return snapshot
}

func (m *Manager) Control() Control {
	if m == nil {
		return nil
	}
	return m.control
}

func (m *Manager) scheduleAutoRestore(intent Intent) {
	if m == nil {
		return
	}
	duration := time.Duration(0)
	if applier, ok := m.applier.(patchWindowApplier); ok {
		duration = applier.PatchLiveDuration()
	}
	if duration <= 0 {
		m.cancelAutoRestore()
		return
	}
	// 原生补丁必须有存活窗口，到期自动 restore，防止紧急 patch 长期遗留在线上。
	restoreAt := m.now().UTC().Add(duration)
	restoreIntent := Intent{
		Action:      ActionRestore,
		Version:     intent.Version,
		Reason:      "native patch auto restore after live window",
		RequestedBy: "system",
	}
	m.mu.Lock()
	m.ensureLifeLocked()
	lifecycleCtx := m.lifecycleCtx
	if m.autoRestoreTimer != nil {
		m.autoRestoreTimer.Stop()
	}
	m.snapshot.AutoRestoreAt = restoreAt
	m.autoRestoreTimer = time.AfterFunc(duration, func() {
		if lifecycleCtx != nil && lifecycleCtx.Err() != nil {
			return
		}
		if _, err := m.restore(lifecycleCtx, restoreIntent); err != nil && m.logger != nil && !errors.Is(err, context.Canceled) {
			m.logger.Warn("native patch auto restore failed", "target", m.target, "version", restoreIntent.Version, "error", err)
		}
	})
	m.mu.Unlock()
}

func (m *Manager) cancelAutoRestore() {
	if m == nil {
		return
	}
	m.mu.Lock()
	if m.autoRestoreTimer != nil {
		m.autoRestoreTimer.Stop()
		m.autoRestoreTimer = nil
	}
	m.snapshot.AutoRestoreAt = time.Time{}
	m.mu.Unlock()
}

func (m *Manager) ensureLifeLocked() {
	if m.lifecycleCtx != nil && m.lifecycleCtx.Err() == nil && m.lifecycleCancel != nil {
		return
	}
	m.lifecycleCtx, m.lifecycleCancel = context.WithCancel(context.Background())
}

func (m *Manager) loop(ctx context.Context, done chan struct{}) {
	defer close(done)
	// Watch 返回的是控制面变更流；同一版本重复 intent 由 Process/Progress 负责幂等记录。
	changes, err := m.control.Watch(ctx, m.target)
	if err != nil {
		if m.logger != nil {
			m.logger.Warn("hot update watch failed", "target", m.target, "error", err)
		}
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case intent, ok := <-changes:
			if !ok {
				return
			}
			if _, err := m.Process(ctx, intent); err != nil && m.logger != nil {
				m.logger.Warn("hot update apply failed", "target", m.target, "version", intent.Version, "error", err)
			}
		}
	}
}

func (m *Manager) report(ctx context.Context, intent Intent, stage Stage, status, detail string) {
	// progress 写失败只记录日志，不阻断 apply；因此 release gate 必须检查报告完整性。
	progress := Progress{
		Target:    m.target,
		NodeID:    m.nodeID,
		Version:   intent.Version,
		Stage:     stage,
		Status:    strings.TrimSpace(status),
		Detail:    strings.TrimSpace(detail),
		UpdatedAt: m.now(),
	}
	m.mu.Lock()
	m.snapshot.LastProgress = progress
	m.mu.Unlock()
	reportCtx, cancel := m.reportContext(ctx)
	defer cancel()
	if err := m.control.Report(reportCtx, progress); err != nil {
		m.mu.Lock()
		m.snapshot.Failures++
		m.mu.Unlock()
		if m.logger != nil && !errors.Is(err, context.Canceled) {
			m.logger.Warn("hot update progress report failed", "target", m.target, "version", intent.Version, "stage", stage, "error", err)
		}
	}
}

func (m *Manager) reportContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	timeout := 30 * time.Second
	if m != nil && m.reportTimeout > 0 {
		timeout = m.reportTimeout
	}
	return context.WithTimeout(ctx, timeout)
}

func (m *Manager) fail(ctx context.Context, intent Intent, err error) {
	detail := ""
	if err != nil {
		detail = err.Error()
	}
	m.mu.Lock()
	m.snapshot.Failures++
	m.mu.Unlock()
	m.report(ctx, intent, StageFailed, "failed", detail)
}

func (m *Manager) setIntent(intent Intent) {
	m.mu.Lock()
	m.snapshot.LastIntent = intent
	m.mu.Unlock()
}

func (m *Manager) setPackage(pkg Package) {
	m.mu.Lock()
	m.snapshot.LastPackage = pkg
	m.mu.Unlock()
}
