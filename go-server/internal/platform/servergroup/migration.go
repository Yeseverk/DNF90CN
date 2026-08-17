package servergroup

import (
	"context"
	"errors"
	"fmt"
)

// ErrInvalidMigration 表示合服迁移计划缺少步骤、阶段或回滚信息。
var ErrInvalidMigration = errors.New("server group migration plan is invalid")

const (
	// MigrationSeverityInfo 表示迁移检查发现普通提示。
	MigrationSeverityInfo = "info"
	// MigrationSeverityWarning 表示迁移检查发现警告。
	MigrationSeverityWarning = "warning"
	// MigrationSeverityBlocker 表示迁移检查发现阻断项。
	MigrationSeverityBlocker = "blocker"
)

const (
	// MigrationPhasePrepare 表示合服迁移准备阶段。
	MigrationPhasePrepare = "prepare"
	// MigrationPhaseMigrate 表示业务数据迁移阶段。
	MigrationPhaseMigrate = "migrate"
	// MigrationPhaseRoute 表示路由发布阶段。
	MigrationPhaseRoute = "route"
	// MigrationPhaseVerify 表示合服结果验证阶段。
	MigrationPhaseVerify = "verify"
	// MigrationPhaseRollback 表示回滚阶段。
	MigrationPhaseRollback = "rollback"
	// MigrationPhaseFinish 表示收尾阶段。
	MigrationPhaseFinish = "finish"
)

// MigrationStep 只描述通用迁移控制动作，不承载具体项目表结构或业务数据。
type MigrationStep struct {
	Name        string            `json:"name"`
	Phase       string            `json:"phase"`
	Target      string            `json:"target,omitempty"`
	Owner       string            `json:"owner,omitempty"`
	Required    bool              `json:"required"`
	Description string            `json:"description,omitempty"`
	Checks      []string          `json:"checks,omitempty"`
	Rollback    []string          `json:"rollback,omitempty"`
	Meta        map[string]string `json:"meta,omitempty"`
}

// ConflictCheckRequest 是合服冲突检查器的输入快照。
type ConflictCheckRequest struct {
	Current Plan            `json:"current"`
	DryRun  MergeDryRun     `json:"dry_run"`
	Steps   []MigrationStep `json:"steps"`
}

// ConflictFinding 是合服冲突检查器返回的单条发现。
type ConflictFinding struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Subject  string `json:"subject,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

// ConflictChecker 定义合服迁移计划的冲突检查接口。
type ConflictChecker interface {
	CheckMerge(context.Context, ConflictCheckRequest) ([]ConflictFinding, error)
}

// ConflictCheckFunc 将函数适配为 ConflictChecker。
type ConflictCheckFunc func(context.Context, ConflictCheckRequest) ([]ConflictFinding, error)

// CheckMerge 执行函数型合服冲突检查器。
func (f ConflictCheckFunc) CheckMerge(ctx context.Context, request ConflictCheckRequest) ([]ConflictFinding, error) {
	if f == nil {
		return nil, nil
	}
	return f(ctx, request)
}

// MigrationPlan 是合服迁移预演、步骤、发现和回滚点的组合计划。
type MigrationPlan struct {
	Request       MergeRequest      `json:"request"`
	DryRun        MergeDryRun       `json:"dry_run"`
	Steps         []MigrationStep   `json:"steps"`
	Findings      []ConflictFinding `json:"findings,omitempty"`
	Ready         bool              `json:"ready"`
	Rollback      Plan              `json:"rollback"`
	RollbackPoint RollbackPoint     `json:"rollback_point"`
}

// DefaultMergeMigrationSteps 返回框架内置的合服迁移步骤模板。
func DefaultMergeMigrationSteps() []MigrationStep {
	return []MigrationStep{
		{
			Name:        "freeze_non_main_writes",
			Phase:       MigrationPhasePrepare,
			Target:      "shards",
			Required:    true,
			Description: "冻结非主分片关键写入，避免迁移窗口产生新冲突",
			Checks:      []string{"ResolveWrite", "critical_write_blocked"},
			Rollback:    []string{"release_write_block"},
		},
		{
			Name:        "capture_rollback_snapshot",
			Phase:       MigrationPhasePrepare,
			Target:      "servergroup_plan",
			Required:    true,
			Description: "保存 dry-run rollback 快照，供验证失败或回滚演练使用",
			Checks:      []string{"rollback_snapshot_persisted"},
			Rollback:    []string{"restore_rollback_snapshot"},
		},
		{
			Name:        "migrate_generic_state",
			Phase:       MigrationPhaseMigrate,
			Target:      "runtime_state",
			Required:    true,
			Description: "按项目提供的迁移器搬迁通用状态，核心只约束幂等和校验点",
			Checks:      []string{"idempotent_migration_job", "conflict_report_empty"},
			Rollback:    []string{"run_project_rollback_job"},
		},
		{
			Name:        "publish_merge_routes",
			Phase:       MigrationPhaseRoute,
			Target:      "servergroup_plan",
			Required:    true,
			Description: "发布 MergeGroup 和路由计划，让非主分片写入重定向到主分片",
			Checks:      []string{"ResolveWrite_redirects"},
			Rollback:    []string{"restore_previous_routes"},
		},
		{
			Name:        "verify_reads_and_writes",
			Phase:       MigrationPhaseVerify,
			Target:      "runtime_state",
			Required:    true,
			Description: "校验读写目标、冲突报告、审计回执和抽样数据",
			Checks:      []string{"read_sample_passed", "write_redirect_passed", "audit_receipt_saved"},
			Rollback:    []string{"restore_rollback_snapshot"},
		},
		{
			Name:        "release_temporary_blocks",
			Phase:       MigrationPhaseFinish,
			Target:      "shards",
			Required:    false,
			Description: "验证窗口结束后释放临时冲突和冻结项",
			Checks:      []string{"operator_approved"},
		},
	}
}

// BuildMergeMigrationPlan 构建合服迁移计划并运行冲突检查。
func BuildMergeMigrationPlan(ctx context.Context, current Plan, request MergeRequest, steps []MigrationStep, checkers ...ConflictChecker) (MigrationPlan, error) {
	if err := ctxErr(ctx); err != nil {
		return MigrationPlan{}, err
	}
	normalized, _, err := normalizePlan(current)
	if err != nil {
		return MigrationPlan{}, err
	}
	dryRun, err := DryRunMerge(ctx, normalized, request)
	if err != nil {
		return MigrationPlan{}, err
	}
	if len(steps) == 0 {
		steps = DefaultMergeMigrationSteps()
	}
	steps = cloneMigrationSteps(steps)
	if err := ValidateMigrationSteps(steps); err != nil {
		return MigrationPlan{}, err
	}
	checkRequest := ConflictCheckRequest{
		Current: clonePlan(normalized),
		DryRun:  cloneMergeDryRun(dryRun),
		Steps:   cloneMigrationSteps(steps),
	}
	checkers = append([]ConflictChecker{DefaultConflictChecker()}, checkers...)
	var findings []ConflictFinding
	for _, checker := range checkers {
		if checker == nil {
			continue
		}
		found, err := checker.CheckMerge(ctx, cloneConflictReq(checkRequest))
		if err != nil {
			return MigrationPlan{}, err
		}
		for _, finding := range found {
			normalizedFinding, err := normConflict(finding)
			if err != nil {
				return MigrationPlan{}, err
			}
			findings = append(findings, normalizedFinding)
		}
	}
	return MigrationPlan{
		Request:       dryRun.Request,
		DryRun:        cloneMergeDryRun(dryRun),
		Steps:         cloneMigrationSteps(steps),
		Findings:      cloneClashFindings(findings),
		Ready:         dryRun.Valid && len(dryRun.Warnings) == 0 && !hasBlockerFinding(findings),
		Rollback:      clonePlan(dryRun.Rollback),
		RollbackPoint: cloneRollbackPoint(dryRun.RollbackPoint),
	}, nil
}

// DefaultConflictChecker 返回框架内置的合服冲突检查器。
func DefaultConflictChecker() ConflictChecker {
	return ConflictCheckFunc(func(_ context.Context, request ConflictCheckRequest) ([]ConflictFinding, error) {
		var findings []ConflictFinding
		if request.DryRun.RollbackPoint.ID == "" || request.DryRun.RollbackPoint.Snapshot.Version != request.DryRun.Rollback.Version {
			findings = append(findings, ConflictFinding{
				Code:     "rollback_point_missing",
				Severity: MigrationSeverityBlocker,
				Subject:  request.DryRun.Request.ID,
				Detail:   "merge dry-run must carry a rollback point matching the rollback snapshot",
			})
		}
		for _, warning := range request.DryRun.Warnings {
			findings = append(findings, ConflictFinding{
				Code:     "write_check_warning",
				Severity: MigrationSeverityBlocker,
				Subject:  request.DryRun.Request.ID,
				Detail:   warning,
			})
		}
		for _, check := range request.DryRun.WriteChecks {
			switch {
			case check.ShardID == request.DryRun.Request.MainShardID && !check.Available:
				findings = append(findings, ConflictFinding{
					Code:     "main_shard_unavailable",
					Severity: MigrationSeverityBlocker,
					Subject:  check.ShardID,
					Detail:   check.Reason,
				})
			case check.ShardID != request.DryRun.Request.MainShardID && check.RedirectShardID != request.DryRun.Request.MainShardID:
				findings = append(findings, ConflictFinding{
					Code:     "merged_shard_not_redirected",
					Severity: MigrationSeverityBlocker,
					Subject:  check.ShardID,
					Detail:   "merged shard writes must redirect to the main shard",
				})
			}
		}
		if !hasMigrationPhase(request.Steps, MigrationPhaseRollback) && !hasRollbackActions(request.Steps) {
			findings = append(findings, ConflictFinding{
				Code:     "rollback_phase_missing",
				Severity: MigrationSeverityWarning,
				Subject:  request.DryRun.Request.ID,
				Detail:   "migration steps should include an explicit rollback phase or rollback actions on required steps",
			})
		}
		return findings, nil
	})
}

// ValidateMigrationSteps 校验迁移步骤名称、阶段和必填检查。
func ValidateMigrationSteps(steps []MigrationStep) error {
	if len(steps) == 0 {
		return fmt.Errorf("%w: at least one step is required", ErrInvalidMigration)
	}
	seen := make(map[string]struct{}, len(steps))
	for _, step := range steps {
		step = normMigrationStep(step)
		if step.Name == "" || step.Phase == "" {
			return fmt.Errorf("%w: step name and phase are required", ErrInvalidMigration)
		}
		if _, exists := seen[step.Name]; exists {
			return fmt.Errorf("%w: duplicate step %s", ErrInvalidMigration, step.Name)
		}
		seen[step.Name] = struct{}{}
		if !validMigrationPhase(step.Phase) {
			return fmt.Errorf("%w: step %s has invalid phase %s", ErrInvalidMigration, step.Name, step.Phase)
		}
		if step.Required && step.Description == "" && len(step.Checks) == 0 && len(step.Rollback) == 0 {
			return fmt.Errorf("%w: required step %s must describe checks or rollback", ErrInvalidMigration, step.Name)
		}
	}
	return nil
}

func normMigrationStep(step MigrationStep) MigrationStep {
	step.Name = normalizeID(step.Name)
	step.Phase = normalizeID(step.Phase)
	step.Target = normalizeID(step.Target)
	step.Owner = normalizeID(step.Owner)
	step.Description = firstNonEmpty(step.Description)
	step.Checks = normalizeIDs(step.Checks)
	step.Rollback = normalizeIDs(step.Rollback)
	step.Meta = normalizeStringMap(step.Meta)
	return step
}

func validMigrationPhase(phase string) bool {
	switch phase {
	case MigrationPhasePrepare, MigrationPhaseMigrate, MigrationPhaseRoute, MigrationPhaseVerify, MigrationPhaseRollback, MigrationPhaseFinish:
		return true
	default:
		return false
	}
}

func normConflict(finding ConflictFinding) (ConflictFinding, error) {
	finding.Code = normalizeID(finding.Code)
	finding.Severity = normalizeID(finding.Severity)
	finding.Subject = normalizeID(finding.Subject)
	finding.Detail = firstNonEmpty(finding.Detail)
	if finding.Code == "" {
		return ConflictFinding{}, fmt.Errorf("%w: finding code is required", ErrInvalidMigration)
	}
	if finding.Severity == "" {
		finding.Severity = MigrationSeverityWarning
	}
	if !validFindingSeverity(finding.Severity) {
		return ConflictFinding{}, fmt.Errorf("%w: finding %s has invalid severity %s", ErrInvalidMigration, finding.Code, finding.Severity)
	}
	return finding, nil
}

func validFindingSeverity(severity string) bool {
	switch severity {
	case MigrationSeverityInfo, MigrationSeverityWarning, MigrationSeverityBlocker:
		return true
	default:
		return false
	}
}

func hasBlockerFinding(findings []ConflictFinding) bool {
	for _, finding := range findings {
		if finding.Severity == MigrationSeverityBlocker {
			return true
		}
	}
	return false
}

func hasMigrationPhase(steps []MigrationStep, phase string) bool {
	phase = normalizeID(phase)
	for _, step := range steps {
		if normalizeID(step.Phase) == phase {
			return true
		}
	}
	return false
}

func hasRollbackActions(steps []MigrationStep) bool {
	for _, step := range steps {
		if len(step.Rollback) > 0 {
			return true
		}
	}
	return false
}

func cloneMigrationSteps(steps []MigrationStep) []MigrationStep {
	if len(steps) == 0 {
		return nil
	}
	out := make([]MigrationStep, len(steps))
	for i, step := range steps {
		out[i] = normMigrationStep(step)
	}
	return out
}

func cloneClashFindings(findings []ConflictFinding) []ConflictFinding {
	if len(findings) == 0 {
		return nil
	}
	return append([]ConflictFinding(nil), findings...)
}

func cloneConflictReq(request ConflictCheckRequest) ConflictCheckRequest {
	return ConflictCheckRequest{
		Current: clonePlan(request.Current),
		DryRun:  cloneMergeDryRun(request.DryRun),
		Steps:   cloneMigrationSteps(request.Steps),
	}
}

func cloneMergeDryRun(dryRun MergeDryRun) MergeDryRun {
	dryRun.Request = normMergeReq(dryRun.Request)
	dryRun.Next = clonePlan(dryRun.Next)
	dryRun.Rollback = clonePlan(dryRun.Rollback)
	dryRun.RollbackPoint = cloneRollbackPoint(dryRun.RollbackPoint)
	dryRun.AddedConflicts = append([]Conflict(nil), dryRun.AddedConflicts...)
	dryRun.WriteChecks = cloneTargets(dryRun.WriteChecks)
	dryRun.Warnings = append([]string(nil), dryRun.Warnings...)
	return dryRun
}

func cloneTargets(targets []Target) []Target {
	if len(targets) == 0 {
		return nil
	}
	out := make([]Target, len(targets))
	for i, target := range targets {
		target.Shards = append([]string(nil), target.Shards...)
		target.Meta = cloneStringMap(target.Meta)
		out[i] = target
	}
	return out
}
