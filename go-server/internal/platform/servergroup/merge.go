package servergroup

import (
	"context"
	"fmt"
	"time"
)

// MergeRequest 是一次合服操作的目标主分片、参与分片和检查范围。
type MergeRequest struct {
	ID            string            `json:"id"`
	MainShardID   string            `json:"main_shard_id"`
	Shards        []string          `json:"shards"`
	State         string            `json:"state,omitempty"`
	NextVersion   string            `json:"next_version,omitempty"`
	BlockFeatures []string          `json:"block_features,omitempty"`
	CheckFeatures []string          `json:"check_features,omitempty"`
	Reason        string            `json:"reason,omitempty"`
	OperatorID    string            `json:"operator_id,omitempty"`
	Meta          map[string]string `json:"meta,omitempty"`
	UpdatedAt     time.Time         `json:"updated_at,omitempty"`
}

// MergeDryRun 是合服预演生成的新计划、回滚点和写路由检查结果。
type MergeDryRun struct {
	Request        MergeRequest  `json:"request"`
	Valid          bool          `json:"valid"`
	Next           Plan          `json:"next"`
	Rollback       Plan          `json:"rollback"`
	RollbackPoint  RollbackPoint `json:"rollback_point"`
	AddedConflicts []Conflict    `json:"added_conflicts,omitempty"`
	WriteChecks    []Target      `json:"write_checks,omitempty"`
	Warnings       []string      `json:"warnings,omitempty"`
}

// MergeApplyResult 是合服正式应用后的计划变更和执行步骤。
type MergeApplyResult struct {
	Request       MergeRequest       `json:"request"`
	Applied       bool               `json:"applied"`
	DryRun        MergeDryRun        `json:"dry_run"`
	Change        Change             `json:"change"`
	Execution     MergeExecutionPlan `json:"execution"`
	Rollback      Plan               `json:"rollback"`
	RollbackPoint RollbackPoint      `json:"rollback_point"`
	Warnings      []string           `json:"warnings,omitempty"`
}

// MergeExecutionPlan 描述合服执行时需要项目侧按阶段完成的步骤。
type MergeExecutionPlan struct {
	ID        string               `json:"id"`
	MainShard string               `json:"main_shard"`
	Shards    []string             `json:"shards"`
	Steps     []MergeExecutionStep `json:"steps"`
}

// MergeExecutionStep 是合服执行计划中的单个步骤。
type MergeExecutionStep struct {
	Name        string   `json:"name"`
	Phase       string   `json:"phase"`
	Required    bool     `json:"required"`
	Description string   `json:"description,omitempty"`
	Checks      []string `json:"checks,omitempty"`
}

// DryRunMerge 预演合服计划并生成回滚点，不修改当前管理器。
func DryRunMerge(ctx context.Context, current Plan, request MergeRequest) (MergeDryRun, error) {
	if err := ctxErr(ctx); err != nil {
		return MergeDryRun{}, err
	}
	request = normMergeReq(request)
	if request.ID == "" || request.MainShardID == "" || len(request.Shards) == 0 {
		return MergeDryRun{}, fmt.Errorf("%w: merge request id, main shard and shards are required", ErrInvalidPlan)
	}
	if !containsID(request.Shards, request.MainShardID) {
		return MergeDryRun{}, fmt.Errorf("%w: merge request %s does not include main shard %s", ErrInvalidPlan, request.ID, request.MainShardID)
	}
	normalized, _, err := normalizePlan(current)
	if err != nil {
		return MergeDryRun{}, err
	}
	next := clonePlan(normalized)
	next.Version = mergeNextVersion(next.Version, request.ID, request.NextVersion)
	next.UpdatedAt = firstTime(request.UpdatedAt, time.Now().UTC())
	next.MergeGroups = append(next.MergeGroups, MergeGroup{
		ID:          request.ID,
		MainShardID: request.MainShardID,
		Shards:      append([]string(nil), request.Shards...),
		State:       request.State,
		UpdatedAt:   request.UpdatedAt,
		Meta:        cloneStringMap(request.Meta),
	})
	addedConflicts := mergeReqConflicts(request)
	next.Conflicts = append(next.Conflicts, addedConflicts...)
	manager, err := New(next)
	if err != nil {
		return MergeDryRun{}, err
	}
	writeChecks, err := mergeWriteChecks(ctx, manager, request)
	if err != nil {
		return MergeDryRun{}, err
	}
	normalizedNext := manager.Snapshot()
	rollbackPoint := newRollbackPoint(RollbackOperationMerge, normalized.Version, normalizedNext.Version, request.Reason, request.OperatorID, normalized, normalizedNext.UpdatedAt)
	return MergeDryRun{
		Request:        request,
		Valid:          true,
		Next:           normalizedNext,
		Rollback:       normalized,
		RollbackPoint:  rollbackPoint,
		AddedConflicts: addedConflicts,
		WriteChecks:    writeChecks,
		Warnings:       mergeWarnings(request, writeChecks),
	}, nil
}

// ApplyMerge 校验并应用合服计划到管理器。
func ApplyMerge(ctx context.Context, manager *Manager, request MergeRequest) (MergeApplyResult, error) {
	if manager == nil {
		return MergeApplyResult{}, ErrNotFound
	}
	result, err := prepareMergeApply(ctx, manager.Snapshot(), request)
	if err != nil {
		return MergeApplyResult{}, err
	}
	if err := manager.Replace(ctx, result.DryRun.Next); err != nil {
		return MergeApplyResult{}, err
	}
	return result, nil
}

func prepareMergeApply(ctx context.Context, current Plan, request MergeRequest) (MergeApplyResult, error) {
	previous, _, err := normalizePlan(current)
	if err != nil {
		return MergeApplyResult{}, err
	}
	report, err := DryRunMerge(ctx, previous, request)
	if err != nil {
		return MergeApplyResult{}, err
	}
	if len(report.Warnings) > 0 {
		return MergeApplyResult{}, fmt.Errorf("%w: merge has warnings: %v", ErrInvalidPlan, report.Warnings)
	}
	change := Change{
		PreviousVersion: previous.Version,
		CurrentVersion:  report.Next.Version,
		UpdatedAt:       report.Next.UpdatedAt,
	}
	return MergeApplyResult{
		Request:       report.Request,
		Applied:       true,
		DryRun:        report,
		Change:        change,
		Execution:     mergeExecutionPlan(report.Request),
		Rollback:      report.Rollback,
		RollbackPoint: report.RollbackPoint,
		Warnings:      report.Warnings,
	}, nil
}

func normMergeReq(request MergeRequest) MergeRequest {
	request.ID = normalizeID(request.ID)
	request.MainShardID = normalizeID(request.MainShardID)
	request.Shards = normalizeIDs(request.Shards)
	request.State = normalizeState(request.State)
	request.NextVersion = normalizeID(request.NextVersion)
	request.BlockFeatures = normalizeIDs(request.BlockFeatures)
	request.CheckFeatures = normalizeIDs(request.CheckFeatures)
	request.Reason = firstNonEmpty(request.Reason, "merge_dry_run:"+request.ID)
	request.OperatorID = firstNonEmpty(request.OperatorID)
	request.Meta = normalizeStringMap(request.Meta)
	request.UpdatedAt = normalizeTime(request.UpdatedAt)
	return request
}

func mergeReqConflicts(request MergeRequest) []Conflict {
	if len(request.BlockFeatures) == 0 {
		return nil
	}
	out := make([]Conflict, 0, len(request.BlockFeatures)*len(request.Shards))
	for _, feature := range request.BlockFeatures {
		for _, shardID := range request.Shards {
			if shardID == request.MainShardID {
				continue
			}
			out = append(out, Conflict{
				Feature: feature,
				ShardID: shardID,
				Reason:  request.Reason,
			})
		}
	}
	return out
}

func mergeWriteChecks(ctx context.Context, manager *Manager, request MergeRequest) ([]Target, error) {
	features := request.CheckFeatures
	if len(features) == 0 {
		features = request.BlockFeatures
	}
	if len(features) == 0 {
		features = []string{"default"}
	}
	checks := make([]Target, 0, len(features)*len(request.Shards))
	for _, feature := range features {
		for _, shardID := range request.Shards {
			target, ok, err := manager.ResolveWrite(ctx, feature, shardID)
			if err != nil || !ok {
				return nil, err
			}
			checks = append(checks, target)
		}
	}
	return checks, nil
}

func mergeWarnings(request MergeRequest, checks []Target) []string {
	var warnings []string
	for _, check := range checks {
		if check.ShardID == request.MainShardID && !check.Available {
			warnings = append(warnings, "main shard write check is unavailable:"+check.Reason)
		}
		if check.ShardID != request.MainShardID && check.RedirectShardID != request.MainShardID {
			warnings = append(warnings, "merged shard does not redirect to main:"+check.ShardID)
		}
	}
	return warnings
}

func mergeNextVersion(currentVersion, mergeID, requested string) string {
	if requested = normalizeID(requested); requested != "" {
		return requested
	}
	currentVersion = normalizeID(currentVersion)
	mergeID = normalizeID(mergeID)
	switch {
	case currentVersion == "":
		return mergeID
	case mergeID == "":
		return currentVersion
	default:
		return currentVersion + "-" + mergeID
	}
}

func mergeExecutionPlan(request MergeRequest) MergeExecutionPlan {
	return MergeExecutionPlan{
		ID:        request.ID,
		MainShard: request.MainShardID,
		Shards:    append([]string(nil), request.Shards...),
		Steps: []MergeExecutionStep{
			{
				Name:        "freeze_non_main_writes",
				Phase:       "prepare",
				Required:    true,
				Description: "block critical writes on non-main shards before data movement",
				Checks:      append([]string(nil), request.BlockFeatures...),
			},
			{
				Name:        "publish_merge_group",
				Phase:       "route",
				Required:    true,
				Description: "publish MergeGroup and route metadata into the server group plan",
			},
			{
				Name:        "verify_write_redirects",
				Phase:       "verify",
				Required:    true,
				Description: "ResolveWrite must keep main shard writable and redirect non-main shards",
				Checks:      append([]string(nil), request.CheckFeatures...),
			},
			{
				Name:        "persist_rollback_snapshot",
				Phase:       "rollback",
				Required:    true,
				Description: "retain DryRun.Rollback until data verification and customer support window finish",
			},
			{
				Name:        "release_blocked_features",
				Phase:       "finish",
				Required:    false,
				Description: "remove temporary conflicts after data merge jobs and business verification pass",
			},
		},
	}
}

func firstTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value.UTC()
		}
	}
	return time.Time{}
}
