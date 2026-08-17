package servergroup

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"longheng.io/server/internal/platform/admincmd"
	"longheng.io/server/internal/platform/adminworkflow"
)

// MergeAdminOperation 是合服工作流接入 admincmd 的操作名。
const MergeAdminOperation = "servergroup.merge.workflow"

// MergeAdminOptions 描述合服工作流接入 Admin 工作流状态机所需依赖。
type MergeAdminOptions struct {
	Runner *MergeRunner
	Now    func() time.Time
}

// MergeAdminAdapter 把合服 runner 适配为 adminworkflow 的 Previewer 和 Executor。
type MergeAdminAdapter struct {
	runner *MergeRunner
	now    func() time.Time
}

var _ adminworkflow.Previewer = (*MergeAdminAdapter)(nil)
var _ adminworkflow.Executor = (*MergeAdminAdapter)(nil)

// NewMergeAdminAdapter 创建 Admin 工作流适配器。
func NewMergeAdminAdapter(options MergeAdminOptions) (*MergeAdminAdapter, error) {
	if options.Runner == nil {
		return nil, ErrNotFound
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &MergeAdminAdapter{runner: options.Runner, now: now}, nil
}

// MergeCmdFromAdmin 把 admincmd.Command 转为合服工作流命令。
func MergeCmdFromAdmin(command admincmd.Command) (MergeCmd, error) {
	command = admincmd.Normalize(command)
	if command.Operation != MergeAdminOperation {
		return MergeCmd{}, fmt.Errorf("%w: admin command operation %s is not %s", ErrInvalidMigration, command.Operation, MergeAdminOperation)
	}
	workflowCommand, err := MergeCmdFromParams(command.Params)
	if err != nil {
		return MergeCmd{}, err
	}
	workflowCommand.IdempotencyKey = firstNonEmpty(workflowCommand.IdempotencyKey, command.IdempotencyKey)
	workflowCommand.OperatorID = firstNonEmpty(workflowCommand.OperatorID, command.Actor)
	workflowCommand.Reason = firstNonEmpty(workflowCommand.Reason, command.Reason)
	workflowCommand.Meta = mergeArchiveMeta(workflowCommand, map[string]string{
		"admin_operation":   command.Operation,
		"admin_scope":       command.Scope,
		"admin_target":      command.Target,
		"admin_environment": command.Environment,
		"admin_shard_id":    command.ShardID,
	})
	return normalizeMergeCmd(workflowCommand)
}

// DryRunAdminWorkflow 生成合服工作流预览，不执行真实写入。
func (a *MergeAdminAdapter) DryRunAdminWorkflow(ctx context.Context, command admincmd.Command) (adminworkflow.DryRun, error) {
	if a == nil || a.runner == nil {
		return adminworkflow.DryRun{}, ErrNotFound
	}
	workflowCommand, err := MergeCmdFromAdmin(command)
	if err != nil {
		return adminworkflow.DryRun{}, err
	}
	result, err := a.runner.Preview(ctx, workflowCommand)
	if err != nil {
		return adminworkflow.DryRun{}, err
	}
	return adminDryRunResult(result), nil
}

// ExecuteAdminWorkflow 执行已审批的合服工作流，并返回运营回执和回滚说明。
func (a *MergeAdminAdapter) ExecuteAdminWorkflow(ctx context.Context, record adminworkflow.Record) (admincmd.Receipt, adminworkflow.RollbackNote, error) {
	if a == nil || a.runner == nil {
		return admincmd.Receipt{}, adminworkflow.RollbackNote{}, ErrNotFound
	}
	workflowCommand, err := MergeCmdFromAdmin(record.Command)
	if err != nil {
		return admincmd.Receipt{}, adminworkflow.RollbackNote{}, err
	}
	workflowCommand.WorkflowID = firstNonEmpty(workflowCommand.WorkflowID, record.ID)
	if record.Approval != nil {
		workflowCommand.ApprovalID = firstNonEmpty(workflowCommand.ApprovalID, record.Approval.ID)
	}
	result, err := a.runner.Execute(ctx, workflowCommand)
	if err != nil {
		return admincmd.Receipt{}, adminworkflow.RollbackNote{}, err
	}
	receipt, err := admincmd.NewReceipt(record.Command, "succeeded", a.now().UTC())
	if err != nil {
		return admincmd.Receipt{}, adminworkflow.RollbackNote{}, err
	}
	return receipt, rollbackNoteResult(result), nil
}

func adminDryRunResult(result MergeRunResult) adminworkflow.DryRun {
	return adminworkflow.DryRun{
		Summary:  mergeAdminSummary(result),
		Diffs:    mergeAdminDiffs(result),
		Warnings: mergeAdminWarnings(result.Archive),
		Meta:     mergeAdminMeta(result),
	}
}

func mergeAdminSummary(result MergeRunResult) string {
	archive := result.Archive
	return fmt.Sprintf("区服合服工作流预览 mode=%s archive=%s ready=%t ok=%t applied=%t", result.Mode, archive.ArchiveID, archive.Ready, archive.OK, result.Applied)
}

func mergeAdminDiffs(result MergeRunResult) []adminworkflow.Diff {
	archive := result.Archive
	risk := mergeAdminRisk(archive)
	var diffs []adminworkflow.Diff
	if result.Migration != nil {
		diffs = append(diffs, adminworkflow.Diff{
			Path:   "servergroup.plan.version",
			Before: result.Migration.Rollback.Version,
			After:  result.Migration.DryRun.Next.Version,
			Risk:   risk,
		})
		diffs = append(diffs, adminworkflow.Diff{
			Path:   "servergroup.merge.request",
			Before: result.Migration.Request.Shards,
			After:  result.Migration.Request.MainShardID,
			Risk:   risk,
		})
	}
	if result.Apply != nil {
		diffs = append(diffs, adminworkflow.Diff{
			Path:   "servergroup.merge.apply",
			Before: result.Apply.Change.PreviousVersion,
			After:  result.Apply.Change.CurrentVersion,
			Risk:   risk,
		})
	}
	if result.RollbackApply != nil {
		diffs = append(diffs, adminworkflow.Diff{
			Path:   "servergroup.rollback.version",
			Before: result.RollbackApply.Previous.Version,
			After:  result.RollbackApply.Restored.Version,
			Risk:   risk,
		})
	}
	if archive.RollbackPoint.ID != "" {
		diffs = append(diffs, adminworkflow.Diff{
			Path:   "servergroup.rollback_point",
			Before: "",
			After:  archive.RollbackPoint.ID,
			Risk:   "rollback",
		})
	}
	if len(diffs) == 0 {
		diffs = append(diffs, adminworkflow.Diff{
			Path:   "servergroup.merge.archive",
			Before: "",
			After:  archive.ArchiveID,
			Risk:   risk,
		})
	}
	return diffs
}

func mergeAdminWarnings(archive MergeArchive) []string {
	warnings := cloneMergeStrings(archive.Warnings)
	if !archive.Ready {
		warnings = append(warnings, "合服迁移计划未 ready")
	}
	if !archive.OK {
		warnings = append(warnings, "合服归档存在 blocker 或失败模块")
	}
	for _, finding := range archive.Findings {
		if finding.Severity != MigrationSeverityWarning && finding.Severity != MigrationSeverityBlocker {
			continue
		}
		warnings = append(warnings, mergeFindingText(finding))
	}
	return cloneMergeStrings(warnings)
}

func mergeFindingText(finding ConflictFinding) string {
	parts := []string{finding.Severity, finding.Code}
	if finding.Subject != "" {
		parts = append(parts, finding.Subject)
	}
	if finding.Detail != "" {
		parts = append(parts, finding.Detail)
	}
	return strings.Join(parts, ":")
}

func mergeAdminMeta(result MergeRunResult) map[string]string {
	archive := result.Archive
	meta := map[string]string{
		"archive_id":        archive.ArchiveID,
		"workflow":          archive.Workflow,
		"stage":             archive.Stage,
		"mode":              result.Mode,
		"ready":             strconv.FormatBool(archive.Ready),
		"ok":                strconv.FormatBool(archive.OK),
		"applied":           strconv.FormatBool(result.Applied),
		"request_id":        archive.Request.ID,
		"rollback_point_id": archive.RollbackPoint.ID,
	}
	if archive.WorkflowID != "" {
		meta["workflow_id"] = archive.WorkflowID
	}
	if archive.ApprovalID != "" {
		meta["approval_id"] = archive.ApprovalID
	}
	if archive.IdempotencyKey != "" {
		meta["idempotency_key"] = archive.IdempotencyKey
	}
	return normalizeStringMap(meta)
}

func mergeAdminRisk(archive MergeArchive) string {
	switch {
	case !archive.OK:
		return "blocker"
	case !archive.Ready:
		return "warning"
	case archive.Stage == MergeStageApply || archive.Stage == MergeStageRollback:
		return "dangerous"
	default:
		return "review"
	}
}

func rollbackNoteResult(result MergeRunResult) adminworkflow.RollbackNote {
	archive := result.Archive
	if archive.ArchiveID == "" {
		return adminworkflow.RollbackNote{}
	}
	switch archive.Stage {
	case MergeStageApply:
		return adminworkflow.RollbackNote{
			Summary: "合服执行已归档，可按归档 ID 发起回滚",
			Steps: cloneMergeStrings(append([]string{
				"使用 servergroup_merge mode=rollback_dry_run rollback_archive_id=" + archive.ArchiveID + " 预检回滚",
				"审批后使用 servergroup_merge mode=rollback rollback_archive_id=" + archive.ArchiveID + " 执行业务模块和平台路由回滚",
			}, archive.Rollback...)),
			EvidenceRef: "merge_workflow_archive:" + archive.ArchiveID,
			Meta:        mergeAdminMeta(result),
		}
	case MergeStageRollback:
		steps := []string{"核对回滚归档和模块 rollback 报告"}
		if result.RollbackApply != nil && result.RollbackApply.Restored.Version != "" {
			steps = append(steps, "确认平台路由版本恢复到 "+result.RollbackApply.Restored.Version)
		}
		return adminworkflow.RollbackNote{
			Summary:     "合服回滚已归档",
			Steps:       steps,
			EvidenceRef: "merge_workflow_archive:" + archive.ArchiveID,
			Meta:        mergeAdminMeta(result),
		}
	default:
		return adminworkflow.RollbackNote{}
	}
}
