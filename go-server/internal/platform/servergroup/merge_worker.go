package servergroup

import (
	"context"
	"fmt"
	"time"
)

// MergeWorkerOptions 是合服 worker 的存储、初始计划、步骤和检查器配置。
type MergeWorkerOptions struct {
	Store    Store
	Initial  Plan
	Steps    []MigrationStep
	Checkers []ConflictChecker
	// MergeOps 挂接项目侧模块级合服扩展点，用于把 GenDB 预检并入迁移计划。
	MergeOps *MergeOpRegistry
	Now      func() time.Time
}

// MergeWorker 封装合服预演、应用和回滚执行流程。
type MergeWorker struct {
	manager  *Manager
	store    Store
	steps    []MigrationStep
	checkers []ConflictChecker
	now      func() time.Time
}

// MergeWorkerReport 是合服 worker 预演或应用后的报告。
type MergeWorkerReport struct {
	Mode        string            `json:"mode"`
	Applied     bool              `json:"applied"`
	GeneratedAt time.Time         `json:"generated_at"`
	Migration   MigrationPlan     `json:"migration"`
	Apply       *MergeApplyResult `json:"apply,omitempty"`
}

// MergeRollbackWorkerReport 是合服回滚 worker 的预演或应用报告。
type MergeRollbackWorkerReport struct {
	Mode        string              `json:"mode"`
	Applied     bool                `json:"applied"`
	GeneratedAt time.Time           `json:"generated_at"`
	Rollback    RollbackApplyResult `json:"rollback"`
}

// NewMergeWorker 创建合服 worker 并加载当前服务器分组计划。
func NewMergeWorker(ctx context.Context, options MergeWorkerOptions) (*MergeWorker, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	manager, err := LoadManager(ctx, options.Store, options.Initial)
	if err != nil {
		return nil, err
	}
	checkers := append([]ConflictChecker(nil), options.Checkers...)
	if options.MergeOps != nil {
		checkers = append(checkers, options.MergeOps.BuildChecker())
	}
	return &MergeWorker{
		manager:  manager,
		store:    options.Store,
		steps:    cloneMigrationSteps(options.Steps),
		checkers: checkers,
		now:      now,
	}, nil
}

// Snapshot 返回合服 worker 当前计划快照。
func (w *MergeWorker) Snapshot() Plan {
	if w == nil || w.manager == nil {
		return Plan{}
	}
	return w.manager.Snapshot()
}

// Plan 生成合服迁移计划但不应用。
func (w *MergeWorker) Plan(ctx context.Context, request MergeRequest) (MigrationPlan, error) {
	if w == nil || w.manager == nil {
		return MigrationPlan{}, ErrNotFound
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return BuildMergeMigrationPlan(ctx, w.manager.Snapshot(), request, w.steps, w.checkers...)
}

// Run 执行合服预演，apply 为 true 时正式应用。
func (w *MergeWorker) Run(ctx context.Context, request MergeRequest, apply bool) (MergeWorkerReport, error) {
	if w == nil || w.manager == nil {
		return MergeWorkerReport{}, ErrNotFound
	}
	if ctx == nil {
		ctx = context.Background()
	}
	plan, err := w.Plan(ctx, request)
	if err != nil {
		return MergeWorkerReport{}, err
	}
	report := MergeWorkerReport{
		Mode:        "dry-run",
		Applied:     false,
		GeneratedAt: w.now().UTC(),
		Migration:   plan,
	}
	if !apply {
		return report, nil
	}
	if !plan.Ready {
		return MergeWorkerReport{}, fmt.Errorf("%w: merge migration plan is not ready", ErrInvalidMigration)
	}
	applied, err := ApplyMerge(ctx, w.manager, plan.Request)
	if err != nil {
		return MergeWorkerReport{}, err
	}
	if w.store != nil {
		if err := SaveManager(ctx, w.store, w.manager); err != nil {
			return MergeWorkerReport{}, err
		}
	}
	report.Mode = "apply"
	report.Applied = true
	report.Migration = plan
	report.Apply = &applied
	return report, nil
}

// Rollback 执行合服回滚预演，apply 为 true 时正式回滚。
func (w *MergeWorker) Rollback(ctx context.Context, request RollbackApplyRequest, apply bool) (MergeRollbackWorkerReport, error) {
	if w == nil || w.manager == nil {
		return MergeRollbackWorkerReport{}, ErrNotFound
	}
	if ctx == nil {
		ctx = context.Background()
	}
	result, err := PrepareRollbackApply(ctx, w.manager.Snapshot(), request)
	if err != nil {
		return MergeRollbackWorkerReport{}, err
	}
	report := MergeRollbackWorkerReport{
		Mode:        "rollback-dry-run",
		Applied:     false,
		GeneratedAt: w.now().UTC(),
		Rollback:    result,
	}
	if !apply {
		return report, nil
	}
	result, err = ApplyRollbackPoint(ctx, w.manager, request)
	if err != nil {
		return MergeRollbackWorkerReport{}, err
	}
	if w.store != nil {
		if err := SaveManager(ctx, w.store, w.manager); err != nil {
			return MergeRollbackWorkerReport{}, err
		}
	}
	report.Mode = "rollback"
	report.Applied = true
	report.Rollback = result
	return report, nil
}
