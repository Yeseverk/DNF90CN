package servergroup

import "time"

const (
	// MergeWorkflowName 是合服运营工作流归档名。
	MergeWorkflowName = "servergroup_merge"
)

const (
	// MergeStageDryRun 表示合服工作流处于预演阶段。
	MergeStageDryRun = "dry_run"
	// MergeStageApply 表示合服工作流处于正式应用阶段。
	MergeStageApply = "apply"
	// MergeStageRollbackDry 表示合服工作流处于回滚预演阶段。
	MergeStageRollbackDry = "rollback_dry_run"
	// MergeStageRollback 表示合服工作流处于正式回滚阶段。
	MergeStageRollback = "rollback"
)

// MergeArchiveOptions 描述一次合服运营归档需要汇总的输入。
type MergeArchiveOptions struct {
	// Stage 是归档阶段，取值为 dry_run、apply、rollback_dry_run 或 rollback。
	Stage string
	// ArchiveID 是外部工作流或工单系统传入的归档 ID，空值会自动推导。
	ArchiveID string
	// WorkflowID 是 adminworkflow 记录 ID，便于从归档反查审批记录。
	WorkflowID string
	// ApprovalID 是审批单或工单 ID。
	ApprovalID string
	// IdempotencyKey 是执行侧幂等键。
	IdempotencyKey string
	// OperatorID 是触发本次归档的操作者。
	OperatorID string
	// Reason 是本次合服或回滚的原因。
	Reason string
	// Request 是合服请求，空值时会从迁移计划、执行结果或模块报告中推导。
	Request MergeRequest
	// GeneratedAt 是归档生成时间，空值时使用当前 UTC 时间。
	GeneratedAt time.Time
	// Migration 是 dry-run 迁移计划。
	Migration *MigrationPlan
	// Apply 是平台路由 apply 结果。
	Apply *MergeApplyResult
	// RollbackApply 是平台路由回滚预检或 apply 结果。
	RollbackApply *RollbackApplyResult
	// ModuleReports 是项目侧模块 GenDB、Merge、Rollback 阶段报告。
	ModuleReports []MergeModuleRunReport
	// Meta 是项目侧附加元数据。
	Meta map[string]string
}

// MergeArchive 是合服运营工作流可落库、可审计、可导出的归档包。
type MergeArchive struct {
	// ArchiveID 是归档主键，通常等于 adminworkflow ID、幂等键或合服请求 ID。
	ArchiveID string `json:"archive_id"`
	// Workflow 是运营工作流名，固定为 servergroup_merge。
	Workflow string `json:"workflow"`
	// Stage 是归档阶段。
	Stage string `json:"stage"`
	// WorkflowID 是 adminworkflow 记录 ID。
	WorkflowID string `json:"workflow_id,omitempty"`
	// ApprovalID 是审批单或工单 ID。
	ApprovalID string `json:"approval_id,omitempty"`
	// IdempotencyKey 是执行侧幂等键。
	IdempotencyKey string `json:"idempotency_key,omitempty"`
	// OperatorID 是操作者。
	OperatorID string `json:"operator_id,omitempty"`
	// Reason 是操作原因。
	Reason string `json:"reason,omitempty"`
	// Request 是合服请求快照。
	Request MergeRequest `json:"request"`
	// GeneratedAt 是归档生成时间，统一保存为 UTC。
	GeneratedAt time.Time `json:"generated_at"`
	// Ready 表示 dry-run 迁移计划是否允许进入执行窗口。
	Ready bool `json:"ready"`
	// OK 表示迁移计划、平台执行和模块报告都没有 blocker 或失败。
	OK bool `json:"ok"`
	// Applied 表示平台路由 apply 或 rollback apply 已真正执行。
	Applied bool `json:"applied,omitempty"`
	// RollbackPoint 是本次操作可用于恢复平台路由的回滚锚点。
	RollbackPoint RollbackPoint `json:"rollback_point,omitempty"`
	// Migration 是 dry-run 迁移计划快照。
	Migration *MigrationPlan `json:"migration,omitempty"`
	// Apply 是平台路由 apply 结果。
	Apply *MergeApplyResult `json:"apply,omitempty"`
	// RollbackApply 是平台路由回滚预检或 apply 结果。
	RollbackApply *RollbackApplyResult `json:"rollback_apply,omitempty"`
	// ModuleReports 是项目侧模块阶段报告。
	ModuleReports []MergeModuleRunReport `json:"module_reports,omitempty"`
	// Findings 汇总迁移计划和模块报告中的风险项。
	Findings []ConflictFinding `json:"findings,omitempty"`
	// Warnings 汇总 dry-run 警告。
	Warnings []string `json:"warnings,omitempty"`
	// Evidence 汇总模块报告中的证据引用。
	Evidence []string `json:"evidence,omitempty"`
	// Rollback 汇总模块报告中的业务回滚动作。
	Rollback []string `json:"rollback,omitempty"`
	// Meta 是项目侧附加元数据。
	Meta map[string]string `json:"meta,omitempty"`
}

// NewMergeArchive 把合服计划、平台执行结果和模块报告聚合成运营归档包。
func NewMergeArchive(options MergeArchiveOptions) (MergeArchive, error) {
	stage := normalizeID(options.Stage)
	if stage == "" {
		stage = MergeStageDryRun
	}
	if !validMergeStage(stage) {
		return MergeArchive{}, ErrInvalidMigration
	}
	generatedAt := normalizeTime(options.GeneratedAt)
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}
	request := mergeArchiveRequest(options)
	archive := MergeArchive{
		ArchiveID:      mergeArchiveID(options, request),
		Workflow:       MergeWorkflowName,
		Stage:          stage,
		WorkflowID:     firstNonEmpty(options.WorkflowID),
		ApprovalID:     firstNonEmpty(options.ApprovalID),
		IdempotencyKey: firstNonEmpty(options.IdempotencyKey),
		OperatorID:     firstNonEmpty(options.OperatorID, request.OperatorID),
		Reason:         firstNonEmpty(options.Reason, request.Reason),
		Request:        request,
		GeneratedAt:    generatedAt,
		Ready:          true,
		OK:             true,
		Meta:           normalizeStringMap(options.Meta),
	}
	if options.Migration != nil {
		migration := cloneMigrationPlan(*options.Migration)
		archive.Migration = &migration
		archive.Ready = migration.Ready
		archive.Findings = append(archive.Findings, migration.Findings...)
		archive.Warnings = append(archive.Warnings, migration.DryRun.Warnings...)
		archive.RollbackPoint = cloneRollbackPoint(migration.RollbackPoint)
		if !migration.Ready || hasBlockerFinding(migration.Findings) {
			archive.OK = false
		}
	}
	if options.Apply != nil {
		apply := cloneMergeResult(*options.Apply)
		archive.Apply = &apply
		archive.Applied = apply.Applied
		if archive.RollbackPoint.ID == "" {
			archive.RollbackPoint = cloneRollbackPoint(apply.RollbackPoint)
		}
		if stage == MergeStageApply && !apply.Applied {
			archive.OK = false
		}
	}
	if options.RollbackApply != nil {
		rollback := cloneRollbackApply(*options.RollbackApply)
		archive.RollbackApply = &rollback
		archive.Applied = rollback.Applied
		if archive.RollbackPoint.ID == "" {
			archive.RollbackPoint = cloneRollbackPoint(rollback.RollbackPoint)
		}
		if stage == MergeStageRollback && !rollback.Applied {
			archive.OK = false
		}
	}
	archive.ModuleReports = cloneMergeReports(options.ModuleReports)
	for _, report := range archive.ModuleReports {
		archive.Findings = append(archive.Findings, report.Findings...)
		archive.Evidence = append(archive.Evidence, report.Evidence...)
		archive.Rollback = append(archive.Rollback, report.Rollback...)
		if !report.OK {
			archive.OK = false
		}
	}
	archive.Findings = cloneClashFindings(archive.Findings)
	archive.Warnings = cloneMergeStrings(archive.Warnings)
	archive.Evidence = cloneMergeStrings(archive.Evidence)
	archive.Rollback = cloneMergeStrings(archive.Rollback)
	if hasBlockerFinding(archive.Findings) {
		archive.OK = false
	}
	if archive.ArchiveID == "" {
		return MergeArchive{}, ErrInvalidMigration
	}
	return archive, nil
}

func validMergeStage(stage string) bool {
	switch stage {
	case MergeStageDryRun, MergeStageApply, MergeStageRollbackDry, MergeStageRollback:
		return true
	default:
		return false
	}
}

func mergeArchiveRequest(options MergeArchiveOptions) MergeRequest {
	request := normMergeReq(options.Request)
	if request.ID != "" {
		return request
	}
	if options.Migration != nil {
		request = normMergeReq(options.Migration.Request)
		if request.ID != "" {
			return request
		}
	}
	if options.Apply != nil {
		request = normMergeReq(options.Apply.Request)
		if request.ID != "" {
			return request
		}
	}
	for _, report := range options.ModuleReports {
		request = normMergeReq(report.Request)
		if request.ID != "" {
			return request
		}
	}
	return request
}

func mergeArchiveID(options MergeArchiveOptions, request MergeRequest) string {
	for _, value := range []string{
		options.ArchiveID,
		options.WorkflowID,
		options.IdempotencyKey,
		request.ID,
	} {
		if value = normalizeID(value); value != "" {
			return value
		}
	}
	if options.RollbackApply != nil {
		return normalizeID(options.RollbackApply.RollbackPoint.ID)
	}
	return ""
}

func cloneMergeArchive(archive MergeArchive) MergeArchive {
	archive.Request = normMergeReq(archive.Request)
	archive.RollbackPoint = cloneRollbackPoint(archive.RollbackPoint)
	if archive.Migration != nil {
		migration := cloneMigrationPlan(*archive.Migration)
		archive.Migration = &migration
	}
	if archive.Apply != nil {
		apply := cloneMergeResult(*archive.Apply)
		archive.Apply = &apply
	}
	if archive.RollbackApply != nil {
		rollback := cloneRollbackApply(*archive.RollbackApply)
		archive.RollbackApply = &rollback
	}
	archive.ModuleReports = cloneMergeReports(archive.ModuleReports)
	archive.Findings = cloneClashFindings(archive.Findings)
	archive.Warnings = cloneMergeStrings(archive.Warnings)
	archive.Evidence = cloneMergeStrings(archive.Evidence)
	archive.Rollback = cloneMergeStrings(archive.Rollback)
	archive.Meta = cloneStringMap(archive.Meta)
	return archive
}

func cloneMigrationPlan(plan MigrationPlan) MigrationPlan {
	return MigrationPlan{
		Request:       normMergeReq(plan.Request),
		DryRun:        cloneMergeDryRun(plan.DryRun),
		Steps:         cloneMigrationSteps(plan.Steps),
		Findings:      cloneClashFindings(plan.Findings),
		Ready:         plan.Ready,
		Rollback:      clonePlan(plan.Rollback),
		RollbackPoint: cloneRollbackPoint(plan.RollbackPoint),
	}
}

func cloneMergeResult(result MergeApplyResult) MergeApplyResult {
	return MergeApplyResult{
		Request:       normMergeReq(result.Request),
		Applied:       result.Applied,
		DryRun:        cloneMergeDryRun(result.DryRun),
		Change:        result.Change,
		Execution:     cloneMergePlan(result.Execution),
		Rollback:      clonePlan(result.Rollback),
		RollbackPoint: cloneRollbackPoint(result.RollbackPoint),
		Warnings:      cloneMergeStrings(result.Warnings),
	}
}

func cloneMergePlan(plan MergeExecutionPlan) MergeExecutionPlan {
	plan.Shards = append([]string(nil), plan.Shards...)
	if len(plan.Steps) == 0 {
		return plan
	}
	steps := make([]MergeExecutionStep, len(plan.Steps))
	for i, step := range plan.Steps {
		step.Checks = append([]string(nil), step.Checks...)
		steps[i] = step
	}
	plan.Steps = steps
	return plan
}

func cloneRollbackApply(result RollbackApplyResult) RollbackApplyResult {
	result.Previous = clonePlan(result.Previous)
	result.Restored = clonePlan(result.Restored)
	result.RollbackPoint = cloneRollbackPoint(result.RollbackPoint)
	result.Request = cloneRollbackReq(result.Request)
	return result
}

func cloneRollbackReq(request RollbackApplyRequest) RollbackApplyRequest {
	request.RollbackPoint = cloneRollbackPoint(request.RollbackPoint)
	return request
}

func cloneMergeReports(reports []MergeModuleRunReport) []MergeModuleRunReport {
	if len(reports) == 0 {
		return nil
	}
	out := make([]MergeModuleRunReport, len(reports))
	for i, report := range reports {
		out[i] = MergeModuleRunReport{
			Phase:       normalizeID(report.Phase),
			Request:     normMergeReq(report.Request),
			GeneratedAt: normalizeTime(report.GeneratedAt),
			OK:          report.OK,
			Results:     cloneMergeResults(report.Results),
			Findings:    cloneClashFindings(report.Findings),
			Evidence:    cloneMergeStrings(report.Evidence),
			Rollback:    cloneMergeStrings(report.Rollback),
			Meta:        cloneStringMap(report.Meta),
		}
	}
	return out
}
