package servergroup

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// MergeRunnerOptions 组合合服 worker、模块注册器和归档存储。
type MergeRunnerOptions struct {
	Worker   *MergeWorker
	MergeOps *MergeOpRegistry
	Archives MergeArchiveStore
	Now      func() time.Time
}

// MergeRunner 串起 dry-run、业务模块报告、平台 apply、回滚和归档。
type MergeRunner struct {
	worker   *MergeWorker
	mergeOps *MergeOpRegistry
	archives MergeArchiveStore
	now      func() time.Time
}

// MergeCmd 是后台、CLI 或流水线传给合服工作流的通用命令。
type MergeCmd struct {
	Mode                 string            `json:"mode"`
	ArchiveID            string            `json:"archive_id,omitempty"`
	RollbackArchiveID    string            `json:"rollback_archive_id,omitempty"`
	WorkflowID           string            `json:"workflow_id,omitempty"`
	ApprovalID           string            `json:"approval_id,omitempty"`
	IdempotencyKey       string            `json:"idempotency_key,omitempty"`
	OperatorID           string            `json:"operator_id,omitempty"`
	Reason               string            `json:"reason,omitempty"`
	Request              MergeRequest      `json:"request"`
	RollbackPoint        RollbackPoint     `json:"rollback_point,omitempty"`
	RestoreVersion       string            `json:"restore_version,omitempty"`
	AllowVersionMismatch bool              `json:"allow_version_mismatch,omitempty"`
	Meta                 map[string]string `json:"meta,omitempty"`
}

// MergeRunResult 是一次合服工作流预检或执行后的结构化结果。
type MergeRunResult struct {
	Mode          string                 `json:"mode"`
	Applied       bool                   `json:"applied"`
	GeneratedAt   time.Time              `json:"generated_at"`
	Archive       MergeArchive           `json:"archive"`
	Migration     *MigrationPlan         `json:"migration,omitempty"`
	Apply         *MergeApplyResult      `json:"apply,omitempty"`
	RollbackApply *RollbackApplyResult   `json:"rollback_apply,omitempty"`
	ModuleReports []MergeModuleRunReport `json:"module_reports,omitempty"`
}

// NewMergeRunner 创建合服工作流 runner。
func NewMergeRunner(options MergeRunnerOptions) (*MergeRunner, error) {
	if options.Worker == nil {
		return nil, ErrNotFound
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &MergeRunner{
		worker:   options.Worker,
		mergeOps: options.MergeOps,
		archives: options.Archives,
		now:      now,
	}, nil
}

// MergeCmdFromParams 把 admincmd 模板参数或 HTTP 参数转换为合服工作流命令。
func MergeCmdFromParams(params map[string]any) (MergeCmd, error) {
	mode, err := mergeStrParam(params, "mode")
	if err != nil {
		return MergeCmd{}, err
	}
	mergeID, err := mergeStrParam(params, "merge_id")
	if err != nil {
		return MergeCmd{}, err
	}
	mainShardID, err := mergeStrParam(params, "main_shard_id")
	if err != nil {
		return MergeCmd{}, err
	}
	shards, err := stringListParam(params, "shards")
	if err != nil {
		return MergeCmd{}, err
	}
	checkFeatures, err := stringListParam(params, "check_features")
	if err != nil {
		return MergeCmd{}, err
	}
	blockFeatures, err := stringListParam(params, "block_features")
	if err != nil {
		return MergeCmd{}, err
	}
	nextVersion, err := mergeStrParam(params, "next_version")
	if err != nil {
		return MergeCmd{}, err
	}
	archiveID, err := mergeStrParam(params, "archive_id")
	if err != nil {
		return MergeCmd{}, err
	}
	rollbackArchiveID, err := mergeStrParam(params, "rollback_archive_id")
	if err != nil {
		return MergeCmd{}, err
	}
	workflowID, err := mergeStrParam(params, "workflow_id")
	if err != nil {
		return MergeCmd{}, err
	}
	approvalID, err := mergeStrParam(params, "approval_id")
	if err != nil {
		return MergeCmd{}, err
	}
	restoreVersion, err := mergeStrParam(params, "restore_version")
	if err != nil {
		return MergeCmd{}, err
	}
	allowMismatch, err := mergeBoolParam(params, "allow_version_mismatch")
	if err != nil {
		return MergeCmd{}, err
	}
	return normalizeMergeCmd(MergeCmd{
		Mode:              mode,
		ArchiveID:         archiveID,
		RollbackArchiveID: rollbackArchiveID,
		WorkflowID:        workflowID,
		ApprovalID:        approvalID,
		Request: MergeRequest{
			ID:            mergeID,
			MainShardID:   mainShardID,
			Shards:        shards,
			NextVersion:   nextVersion,
			CheckFeatures: checkFeatures,
			BlockFeatures: blockFeatures,
		},
		RestoreVersion:       restoreVersion,
		AllowVersionMismatch: allowMismatch,
	})
}

// Preview 只生成计划、模块预检报告和归档，不修改平台路由计划。
func (r *MergeRunner) Preview(ctx context.Context, command MergeCmd) (MergeRunResult, error) {
	if r == nil || r.worker == nil {
		return MergeRunResult{}, ErrNotFound
	}
	if err := ctxErr(ctx); err != nil {
		return MergeRunResult{}, err
	}
	command, err := normalizeMergeCmd(command)
	if err != nil {
		return MergeRunResult{}, err
	}
	switch command.Mode {
	case MergeStageDryRun, MergeStageApply:
		return r.previewMerge(ctx, command)
	case MergeStageRollbackDry, MergeStageRollback:
		return r.previewRollback(ctx, command)
	default:
		return MergeRunResult{}, ErrInvalidMigration
	}
}

// Execute 按命令模式执行 apply 或 rollback；dry-run 模式仍只做预检。
func (r *MergeRunner) Execute(ctx context.Context, command MergeCmd) (MergeRunResult, error) {
	if r == nil || r.worker == nil {
		return MergeRunResult{}, ErrNotFound
	}
	if err := ctxErr(ctx); err != nil {
		return MergeRunResult{}, err
	}
	command, err := normalizeMergeCmd(command)
	if err != nil {
		return MergeRunResult{}, err
	}
	switch command.Mode {
	case MergeStageDryRun:
		return r.previewMerge(ctx, command)
	case MergeStageApply:
		return r.executeMerge(ctx, command)
	case MergeStageRollbackDry:
		return r.previewRollback(ctx, command)
	case MergeStageRollback:
		return r.executeRollback(ctx, command)
	default:
		return MergeRunResult{}, ErrInvalidMigration
	}
}

func (r *MergeRunner) previewMerge(ctx context.Context, command MergeCmd) (MergeRunResult, error) {
	current := r.worker.Snapshot()
	plan, err := r.worker.Plan(ctx, command.Request)
	if err != nil {
		return MergeRunResult{}, err
	}
	reports, err := r.runGenDBReport(ctx, current, plan, command.Meta)
	if err != nil {
		return MergeRunResult{}, err
	}
	archive, err := NewMergeArchive(MergeArchiveOptions{
		Stage:          MergeStageDryRun,
		ArchiveID:      command.ArchiveID,
		WorkflowID:     command.WorkflowID,
		ApprovalID:     command.ApprovalID,
		IdempotencyKey: command.IdempotencyKey,
		OperatorID:     command.OperatorID,
		Reason:         command.Reason,
		Request:        command.Request,
		GeneratedAt:    r.now().UTC(),
		Migration:      &plan,
		ModuleReports:  reports,
		Meta:           mergeArchiveMeta(command, map[string]string{"command_mode": command.Mode}),
	})
	if err != nil {
		return MergeRunResult{}, err
	}
	if err := r.saveArchive(ctx, archive); err != nil {
		return MergeRunResult{}, err
	}
	return MergeRunResult{
		Mode:          command.Mode,
		Applied:       false,
		GeneratedAt:   archive.GeneratedAt,
		Archive:       archive,
		Migration:     cloneMigrationPtr(plan),
		ModuleReports: cloneMergeReports(reports),
	}, nil
}

func (r *MergeRunner) executeMerge(ctx context.Context, command MergeCmd) (MergeRunResult, error) {
	current := r.worker.Snapshot()
	plan, err := r.worker.Plan(ctx, command.Request)
	if err != nil {
		return MergeRunResult{}, err
	}
	gendbReports, err := r.runGenDBReport(ctx, current, plan, command.Meta)
	if err != nil {
		return MergeRunResult{}, err
	}
	if !plan.Ready || !mergeReportsOK(gendbReports) {
		return MergeRunResult{}, fmt.Errorf("%w: merge dry-run is not ready", ErrInvalidMigration)
	}
	moduleInput := MergeModuleInputFromPlan(current, plan)
	moduleInput.Meta = mergeArchiveMeta(command, map[string]string{"command_mode": command.Mode})
	mergeReports, err := r.runModuleReport(ctx, MergeModulePhaseMerge, moduleInput)
	if err != nil {
		return MergeRunResult{}, err
	}
	reports := append(cloneMergeReports(gendbReports), mergeReports...)
	if !mergeReportsOK(mergeReports) {
		return MergeRunResult{}, fmt.Errorf("%w: merge module report has blockers", ErrInvalidMigration)
	}
	workerReport, err := r.worker.Run(ctx, command.Request, true)
	if err != nil {
		return MergeRunResult{}, err
	}
	if workerReport.Apply == nil {
		return MergeRunResult{}, fmt.Errorf("%w: merge apply result is required", ErrInvalidMigration)
	}
	archive, err := NewMergeArchive(MergeArchiveOptions{
		Stage:          MergeStageApply,
		ArchiveID:      command.ArchiveID,
		WorkflowID:     command.WorkflowID,
		ApprovalID:     command.ApprovalID,
		IdempotencyKey: command.IdempotencyKey,
		OperatorID:     command.OperatorID,
		Reason:         command.Reason,
		Request:        command.Request,
		GeneratedAt:    r.now().UTC(),
		Migration:      &plan,
		Apply:          workerReport.Apply,
		ModuleReports:  reports,
		Meta:           mergeArchiveMeta(command, nil),
	})
	if err != nil {
		return MergeRunResult{}, err
	}
	if err := r.saveArchive(ctx, archive); err != nil {
		return MergeRunResult{}, err
	}
	return MergeRunResult{
		Mode:          command.Mode,
		Applied:       true,
		GeneratedAt:   archive.GeneratedAt,
		Archive:       archive,
		Migration:     cloneMigrationPtr(plan),
		Apply:         cloneMergeApplyRef(*workerReport.Apply),
		ModuleReports: cloneMergeReports(reports),
	}, nil
}

func (r *MergeRunner) previewRollback(ctx context.Context, command MergeCmd) (MergeRunResult, error) {
	source, point, err := r.resolveRollbackPoint(ctx, command)
	if err != nil {
		return MergeRunResult{}, err
	}
	request := rollbackRequest(command, point, false)
	workerReport, err := r.worker.Rollback(ctx, request, false)
	if err != nil {
		return MergeRunResult{}, err
	}
	archiveID := rollbackArchiveID(command, MergeStageRollbackDry, point)
	archive, err := NewMergeArchive(MergeArchiveOptions{
		Stage:          MergeStageRollbackDry,
		ArchiveID:      archiveID,
		WorkflowID:     command.WorkflowID,
		ApprovalID:     command.ApprovalID,
		IdempotencyKey: command.IdempotencyKey,
		OperatorID:     command.OperatorID,
		Reason:         command.Reason,
		Request:        rollbackCmdReq(command, source),
		GeneratedAt:    r.now().UTC(),
		RollbackApply:  &workerReport.Rollback,
		ModuleReports:  nil,
		Meta:           mergeArchiveMeta(command, map[string]string{"rollback_archive_id": source.ArchiveID}),
	})
	if err != nil {
		return MergeRunResult{}, err
	}
	if err := r.saveArchive(ctx, archive); err != nil {
		return MergeRunResult{}, err
	}
	return MergeRunResult{
		Mode:          command.Mode,
		Applied:       false,
		GeneratedAt:   archive.GeneratedAt,
		Archive:       archive,
		RollbackApply: cloneApplyResultPtr(workerReport.Rollback),
	}, nil
}

func (r *MergeRunner) executeRollback(ctx context.Context, command MergeCmd) (MergeRunResult, error) {
	source, point, err := r.resolveRollbackPoint(ctx, command)
	if err != nil {
		return MergeRunResult{}, err
	}
	current := r.worker.Snapshot()
	moduleInput := rollbackModuleInput(current, source, command)
	rollbackReports, err := r.runModuleReport(ctx, MergeModulePhaseRollback, moduleInput)
	if err != nil {
		return MergeRunResult{}, err
	}
	if !mergeReportsOK(rollbackReports) {
		return MergeRunResult{}, fmt.Errorf("%w: rollback module report has blockers", ErrInvalidMigration)
	}
	request := rollbackRequest(command, point, true)
	workerReport, err := r.worker.Rollback(ctx, request, true)
	if err != nil {
		return MergeRunResult{}, err
	}
	archiveID := rollbackArchiveID(command, MergeStageRollback, point)
	archive, err := NewMergeArchive(MergeArchiveOptions{
		Stage:          MergeStageRollback,
		ArchiveID:      archiveID,
		WorkflowID:     command.WorkflowID,
		ApprovalID:     command.ApprovalID,
		IdempotencyKey: command.IdempotencyKey,
		OperatorID:     command.OperatorID,
		Reason:         command.Reason,
		Request:        rollbackCmdReq(command, source),
		GeneratedAt:    r.now().UTC(),
		RollbackApply:  &workerReport.Rollback,
		ModuleReports:  rollbackReports,
		Meta:           mergeArchiveMeta(command, map[string]string{"rollback_archive_id": source.ArchiveID}),
	})
	if err != nil {
		return MergeRunResult{}, err
	}
	if err := r.saveArchive(ctx, archive); err != nil {
		return MergeRunResult{}, err
	}
	return MergeRunResult{
		Mode:          command.Mode,
		Applied:       true,
		GeneratedAt:   archive.GeneratedAt,
		Archive:       archive,
		RollbackApply: cloneApplyResultPtr(workerReport.Rollback),
		ModuleReports: cloneMergeReports(rollbackReports),
	}, nil
}

func (r *MergeRunner) runGenDBReport(ctx context.Context, current Plan, plan MigrationPlan, meta map[string]string) ([]MergeModuleRunReport, error) {
	input := MergeModuleInputFromPlan(current, plan)
	input.Meta = cloneStringMap(meta)
	return r.runModuleReport(ctx, MergeModulePhaseGenDB, input)
}

func (r *MergeRunner) runModuleReport(ctx context.Context, phase string, input MergeModuleInput) ([]MergeModuleRunReport, error) {
	if r.mergeOps == nil {
		return nil, nil
	}
	report, err := r.mergeOps.runPhaseReport(ctx, phase, input)
	if err != nil {
		return nil, err
	}
	return []MergeModuleRunReport{report}, nil
}

func (r *MergeRunner) saveArchive(ctx context.Context, archive MergeArchive) error {
	if r.archives == nil {
		return nil
	}
	return r.archives.SaveMergeArchive(ctx, archive)
}

func (r *MergeRunner) resolveRollbackPoint(ctx context.Context, command MergeCmd) (MergeArchive, RollbackPoint, error) {
	if command.RollbackPoint.ID != "" {
		source := MergeArchive{ArchiveID: firstNonEmpty(command.RollbackArchiveID, command.ArchiveID), Request: normMergeReq(command.Request)}
		return source, cloneRollbackPoint(command.RollbackPoint), nil
	}
	if r.archives == nil {
		return MergeArchive{}, RollbackPoint{}, ErrMergeArchiveNotFound
	}
	archiveID := firstNonEmpty(command.RollbackArchiveID, command.ArchiveID)
	if archiveID == "" {
		return MergeArchive{}, RollbackPoint{}, ErrMergeArchiveNotFound
	}
	archive, ok, err := r.archives.GetMergeArchive(ctx, archiveID)
	if err != nil {
		return MergeArchive{}, RollbackPoint{}, err
	}
	if !ok {
		return MergeArchive{}, RollbackPoint{}, ErrMergeArchiveNotFound
	}
	if archive.RollbackPoint.ID == "" {
		return MergeArchive{}, RollbackPoint{}, fmt.Errorf("%w: rollback point is required", ErrInvalidMigration)
	}
	return archive, cloneRollbackPoint(archive.RollbackPoint), nil
}

func normalizeMergeCmd(command MergeCmd) (MergeCmd, error) {
	command.Mode = normalizeID(command.Mode)
	if command.Mode == "" {
		command.Mode = MergeStageDryRun
	}
	if !validMergeStage(command.Mode) {
		return MergeCmd{}, ErrInvalidMigration
	}
	command.ArchiveID = normalizeID(command.ArchiveID)
	command.RollbackArchiveID = normalizeID(command.RollbackArchiveID)
	command.WorkflowID = firstNonEmpty(command.WorkflowID)
	command.ApprovalID = firstNonEmpty(command.ApprovalID)
	command.IdempotencyKey = firstNonEmpty(command.IdempotencyKey)
	command.OperatorID = firstNonEmpty(command.OperatorID)
	command.Reason = firstNonEmpty(command.Reason)
	command.Request = normMergeReq(command.Request)
	if command.Request.OperatorID == "" {
		command.Request.OperatorID = command.OperatorID
	}
	if command.Request.Reason == "" {
		command.Request.Reason = command.Reason
	}
	command.RollbackPoint = normRollbackPoint(command.RollbackPoint)
	command.RestoreVersion = normalizeID(command.RestoreVersion)
	command.Meta = normalizeStringMap(command.Meta)
	return command, nil
}

func mergeArchiveMeta(command MergeCmd, extra map[string]string) map[string]string {
	meta := cloneStringMap(command.Meta)
	if len(meta) == 0 {
		meta = make(map[string]string, len(extra)+3)
	}
	if command.IdempotencyKey != "" {
		meta["idempotency_key"] = command.IdempotencyKey
	}
	if command.WorkflowID != "" {
		meta["workflow_id"] = command.WorkflowID
	}
	if command.ApprovalID != "" {
		meta["approval_id"] = command.ApprovalID
	}
	if command.OperatorID != "" {
		meta["operator_id"] = command.OperatorID
	}
	if command.Reason != "" {
		meta["reason"] = command.Reason
	}
	for key, value := range extra {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			meta[key] = value
		}
	}
	return normalizeStringMap(meta)
}

func rollbackRequest(command MergeCmd, point RollbackPoint, apply bool) RollbackApplyRequest {
	reason := firstNonEmpty(command.Reason, point.Reason)
	if apply {
		reason = firstNonEmpty(reason, "merge rollback:"+point.ID)
	}
	return RollbackApplyRequest{
		RollbackPoint:        cloneRollbackPoint(point),
		RestoreVersion:       command.RestoreVersion,
		Reason:               reason,
		OperatorID:           firstNonEmpty(command.OperatorID, point.OperatorID),
		AllowVersionMismatch: command.AllowVersionMismatch,
	}
}

func rollbackArchiveID(command MergeCmd, stage string, point RollbackPoint) string {
	if command.ArchiveID != "" && command.RollbackArchiveID != "" {
		return command.ArchiveID
	}
	if command.IdempotencyKey != "" {
		return command.IdempotencyKey
	}
	seed := firstNonEmpty(point.ID, command.RollbackArchiveID, command.ArchiveID, command.Request.ID)
	if seed == "" {
		return ""
	}
	return normalizeID(seed + "-" + stage)
}

func rollbackCmdReq(command MergeCmd, source MergeArchive) MergeRequest {
	request := normMergeReq(command.Request)
	if request.ID != "" {
		return request
	}
	return normMergeReq(source.Request)
}

func rollbackModuleInput(current Plan, source MergeArchive, command MergeCmd) MergeModuleInput {
	if source.Migration != nil {
		input := MergeModuleInputFromPlan(current, *source.Migration)
		input.Meta = mergeArchiveMeta(command, map[string]string{"rollback_archive_id": source.ArchiveID})
		return input
	}
	return MergeModuleInput{
		Request:  rollbackCmdReq(command, source),
		Current:  clonePlan(current),
		Findings: cloneClashFindings(source.Findings),
		Meta:     mergeArchiveMeta(command, map[string]string{"rollback_archive_id": source.ArchiveID}),
	}
}

func mergeReportsOK(reports []MergeModuleRunReport) bool {
	for _, report := range reports {
		if !report.OK {
			return false
		}
	}
	return true
}

func cloneMigrationPtr(plan MigrationPlan) *MigrationPlan {
	cloned := cloneMigrationPlan(plan)
	return &cloned
}

func cloneMergeApplyRef(result MergeApplyResult) *MergeApplyResult {
	cloned := cloneMergeResult(result)
	return &cloned
}

func cloneApplyResultPtr(result RollbackApplyResult) *RollbackApplyResult {
	cloned := cloneRollbackApply(result)
	return &cloned
}

func mergeStrParam(params map[string]any, name string) (string, error) {
	value, ok := params[name]
	if !ok || value == nil {
		return "", nil
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed), nil
	case fmt.Stringer:
		return strings.TrimSpace(typed.String()), nil
	case int:
		return strconv.Itoa(typed), nil
	case int64:
		return strconv.FormatInt(typed, 10), nil
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10), nil
		}
		return "", fmt.Errorf("%w: %s must be string", ErrInvalidMigration, name)
	default:
		return "", fmt.Errorf("%w: %s must be string", ErrInvalidMigration, name)
	}
}

func stringListParam(params map[string]any, name string) ([]string, error) {
	value, ok := params[name]
	if !ok || value == nil {
		return nil, nil
	}
	switch typed := value.(type) {
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil, nil
		}
		return normalizeIDs(strings.Split(typed, ",")), nil
	case []string:
		return normalizeIDs(typed), nil
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			text, err := mergeStrValue(item, name)
			if err != nil {
				return nil, err
			}
			out = append(out, text)
		}
		return normalizeIDs(out), nil
	default:
		return nil, fmt.Errorf("%w: %s must be string list", ErrInvalidMigration, name)
	}
}

func mergeBoolParam(params map[string]any, name string) (bool, error) {
	value, ok := params[name]
	if !ok || value == nil {
		return false, nil
	}
	switch typed := value.(type) {
	case bool:
		return typed, nil
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return false, nil
		}
		parsed, err := strconv.ParseBool(text)
		if err != nil {
			return false, fmt.Errorf("%w: %s must be bool", ErrInvalidMigration, name)
		}
		return parsed, nil
	default:
		return false, fmt.Errorf("%w: %s must be bool", ErrInvalidMigration, name)
	}
}

func mergeStrValue(value any, name string) (string, error) {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed), nil
	case int:
		return strconv.Itoa(typed), nil
	case int64:
		return strconv.FormatInt(typed, 10), nil
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10), nil
		}
	}
	return "", fmt.Errorf("%w: %s must contain strings", ErrInvalidMigration, name)
}
