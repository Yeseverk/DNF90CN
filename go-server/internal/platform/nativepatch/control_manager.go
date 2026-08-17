package nativepatch

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrControlNotDue = errors.New("native patch control intent is not due yet")

type ControlManagerOptions struct {
	Target string
	NodeID string
	Store  ControlStore
	Engine Engine
	Policy Policy
	Now    func() time.Time
	// 控制面上报必须有硬超时，否则补丁已 apply 后可能被不可达后端卡在同步验收路径。
	ReportTimeout time.Duration
}

type ControlManager struct {
	target        string
	nodeID        string
	store         ControlStore
	engine        Engine
	policy        Policy
	now           func() time.Time
	reportTimeout time.Duration

	opMu              sync.Mutex
	mu                sync.Mutex
	restoreTimer      *time.Timer
	restoreGeneration int64
}

func NewControlManager(options ControlManagerOptions) (*ControlManager, error) {
	if options.Target == "" {
		return nil, fmt.Errorf("native patch control target is required")
	}
	if options.NodeID == "" {
		return nil, fmt.Errorf("native patch control node id is required")
	}
	if options.Store == nil {
		return nil, fmt.Errorf("native patch control store is required")
	}
	if options.Engine == nil {
		return nil, fmt.Errorf("native patch engine is required")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	reportTimeout := options.ReportTimeout
	if reportTimeout <= 0 {
		reportTimeout = 30 * time.Second
	}
	return &ControlManager{
		target:        options.Target,
		nodeID:        options.NodeID,
		store:         options.Store,
		engine:        options.Engine,
		policy:        options.Policy,
		now:           options.Now,
		reportTimeout: reportTimeout,
	}, nil
}

func (m *ControlManager) Process(ctx context.Context, intent ControlIntent) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if !intent.AvailableAt.IsZero() && intent.AvailableAt.After(m.now().UTC()) {
		_ = m.report(ctx, intent, "pending", "waiting")
		return Result{}, ErrControlNotDue
	}
	_ = m.report(ctx, intent, "applying", "")
	var result Result
	var err error
	armedDuration := time.Duration(0)
	armedRestore := false
	switch intent.Action {
	case "", ControlApply:
		// apply 只消费控制面下发的包路径；包内容仍要经过 LoadPackage 和 Policy 双重校验。
		pkg, loadErr := LoadPackage(ctx, intent.PackageDir)
		if loadErr != nil {
			err = loadErr
			break
		}
		if validateErr := m.policy.Validate(pkg.Plan); validateErr != nil {
			err = validateErr
			break
		}
		m.opMu.Lock()
		result, err = m.engine.Apply(ctx, pkg)
		if err == nil {
			armedDuration, armedRestore = m.armAutoRestore(intent)
		}
		m.opMu.Unlock()
	case ControlRestore:
		m.opMu.Lock()
		result, err = m.engine.Restore(ctx)
		if err == nil {
			m.cancelAutoRestore()
		}
		m.opMu.Unlock()
	default:
		err = fmt.Errorf("unsupported native patch control action %s", intent.Action)
	}
	if err != nil {
		_ = m.report(ctx, intent, "failed", err.Error())
		return result, err
	}
	_ = m.report(ctx, intent, "applied", "")
	if armedRestore {
		m.reportArmed(intent, armedDuration)
	}
	return result, nil
}

func (m *ControlManager) Watch(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ch, err := m.store.Watch(ctx, m.target)
	if err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case intent, ok := <-ch:
			if !ok {
				return ctx.Err()
			}
			// 未到生效时间不是失败；Watcher 继续等待下一条控制意图。
			if _, err := m.Process(ctx, intent); err != nil && !errors.Is(err, ErrControlNotDue) {
				return err
			}
		}
	}
}

func (m *ControlManager) report(ctx context.Context, intent ControlIntent, status string, detail string) error {
	reportCtx, cancel := m.reportContext(ctx)
	defer cancel()
	return m.store.Report(reportCtx, ControlProgress{
		Target:    m.target,
		NodeID:    m.nodeID,
		Version:   intent.Version,
		Action:    intent.Action,
		Status:    status,
		Detail:    detail,
		UpdatedAt: m.now().UTC(),
	})
}

func (m *ControlManager) reportContext(ctx context.Context) (context.Context, context.CancelFunc) {
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

func (m *ControlManager) armAutoRestore(intent ControlIntent) (time.Duration, bool) {
	if m == nil || m.policy.MaxLiveDuration <= 0 {
		return 0, false
	}
	duration := m.policy.MaxLiveDuration
	m.mu.Lock()
	if m.restoreTimer != nil {
		m.restoreTimer.Stop()
	}
	m.restoreGeneration++
	generation := m.restoreGeneration
	m.restoreTimer = time.AfterFunc(duration, func() {
		m.runAutoRestore(generation, intent)
	})
	m.mu.Unlock()
	return duration, true
}

func (m *ControlManager) reportArmed(intent ControlIntent, duration time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), m.reportTimeout)
	defer cancel()
	_ = m.report(ctx, intent, "restore_scheduled", duration.String())
}

func (m *ControlManager) runAutoRestore(generation int64, intent ControlIntent) {
	if !m.autoRestoreCurrent(generation) {
		return
	}
	// 自动恢复走同一套进度上报，方便验收报告确认补丁没有超期滞留。
	restoreIntent := ControlIntent{
		Action:      ControlRestore,
		Target:      intent.Target,
		Version:     intent.Version,
		RequestedBy: "auto_restore",
		Reason:      "native patch max live duration reached",
		Sequence:    intent.Sequence,
	}
	ctx, cancel := context.WithTimeout(context.Background(), m.reportTimeout)
	defer cancel()
	_ = m.report(ctx, restoreIntent, "restoring", "max_live_duration")
	if !m.autoRestoreCurrent(generation) {
		return
	}
	m.opMu.Lock()
	defer m.opMu.Unlock()
	if !m.autoRestoreCurrent(generation) {
		return
	}
	if _, err := m.engine.Restore(ctx); err != nil {
		if m.autoRestoreCurrent(generation) {
			_ = m.report(ctx, restoreIntent, "restore_failed", err.Error())
		}
		return
	}
	if !m.autoRestoreCurrent(generation) {
		return
	}
	m.finishAutoRestore(generation)
	_ = m.report(ctx, restoreIntent, "restored", "max_live_duration")
}

func (m *ControlManager) autoRestoreCurrent(generation int64) bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.restoreTimer != nil && m.restoreGeneration == generation
}

func (m *ControlManager) finishAutoRestore(generation int64) {
	if m == nil {
		return
	}
	m.mu.Lock()
	if m.restoreGeneration == generation {
		m.restoreTimer = nil
		m.restoreGeneration++
	}
	m.mu.Unlock()
}

func (m *ControlManager) cancelAutoRestore() {
	if m == nil {
		return
	}
	m.mu.Lock()
	if m.restoreTimer != nil {
		m.restoreTimer.Stop()
		m.restoreTimer = nil
	}
	m.restoreGeneration++
	m.mu.Unlock()
}
