package servergroup

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	// OperationOpenShard 表示打开分片。
	OperationOpenShard = "open_shard"
	// OperationDrainShard 表示排空分片。
	OperationDrainShard = "drain_shard"
	// OperationCloseShard 表示关闭分片。
	OperationCloseShard = "close_shard"
)

// ShardOperationRequest 是分片开关服或排空操作请求。
type ShardOperationRequest struct {
	ShardID     string            `json:"shard_id"`
	State       string            `json:"state"`
	NextVersion string            `json:"next_version,omitempty"`
	Reason      string            `json:"reason,omitempty"`
	OperatorID  string            `json:"operator_id,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	UpdatedAt   time.Time         `json:"updated_at,omitempty"`
}

// ShardOperationPlan 是分片操作预演后的计划变更。
type ShardOperationPlan struct {
	Operation       string                `json:"operation"`
	Request         ShardOperationRequest `json:"request"`
	Valid           bool                  `json:"valid"`
	Previous        Plan                  `json:"previous"`
	Next            Plan                  `json:"next"`
	RollbackPoint   RollbackPoint         `json:"rollback_point"`
	AffectedTargets []Target              `json:"affected_targets,omitempty"`
	Warnings        []string              `json:"warnings,omitempty"`
}

// DryRunShardOperation 预演分片状态变更并生成回滚点。
func DryRunShardOperation(ctx context.Context, current Plan, request ShardOperationRequest) (ShardOperationPlan, error) {
	if err := ctxErr(ctx); err != nil {
		return ShardOperationPlan{}, err
	}
	request = normShardOpReq(request)
	if request.ShardID == "" || !validShardOpState(request.State) {
		return ShardOperationPlan{}, fmt.Errorf("%w: shard_id and supported state are required", ErrInvalidPlan)
	}
	normalized, _, err := normalizePlan(current)
	if err != nil {
		return ShardOperationPlan{}, err
	}
	next := clonePlan(normalized)
	updated := false
	for idx := range next.Shards {
		if next.Shards[idx].ID != request.ShardID {
			continue
		}
		next.Shards[idx].State = request.State
		next.Shards[idx].UpdatedAt = request.UpdatedAt
		next.Shards[idx].Meta = mergeOperationMeta(next.Shards[idx].Meta, request)
		updated = true
		break
	}
	if !updated {
		return ShardOperationPlan{}, ErrNotFound
	}
	next.Version = operationNextVersion(normalized.Version, request)
	next.UpdatedAt = request.UpdatedAt
	manager, err := New(next)
	if err != nil {
		return ShardOperationPlan{}, err
	}
	affected, err := shardOpTargets(ctx, normalized, manager, request.ShardID)
	if err != nil {
		return ShardOperationPlan{}, err
	}
	normalizedNext := manager.Snapshot()
	return ShardOperationPlan{
		Operation:       shardOperationName(request.State),
		Request:         request,
		Valid:           true,
		Previous:        normalized,
		Next:            normalizedNext,
		RollbackPoint:   newRollbackPoint(RollbackOperationShardOperation, normalized.Version, normalizedNext.Version, request.Reason, request.OperatorID, normalized, normalizedNext.UpdatedAt),
		AffectedTargets: affected,
		Warnings:        shardOpWarnings(request, affected),
	}, nil
}

// ApplyShardOperation 应用分片状态变更到管理器。
func ApplyShardOperation(ctx context.Context, manager *Manager, request ShardOperationRequest) (ShardOperationPlan, error) {
	if manager == nil {
		return ShardOperationPlan{}, ErrNotFound
	}
	plan, err := DryRunShardOperation(ctx, manager.Snapshot(), request)
	if err != nil {
		return ShardOperationPlan{}, err
	}
	if err := manager.Replace(ctx, plan.Next); err != nil {
		return ShardOperationPlan{}, err
	}
	return plan, nil
}

// OpenShard 打开指定分片。
func OpenShard(ctx context.Context, manager *Manager, shardID, nextVersion, reason, operatorID string) (ShardOperationPlan, error) {
	return ApplyShardOperation(ctx, manager, ShardOperationRequest{
		ShardID:     shardID,
		State:       StateOpen,
		NextVersion: nextVersion,
		Reason:      reason,
		OperatorID:  operatorID,
	})
}

// DrainShard 将指定分片切为排空状态。
func DrainShard(ctx context.Context, manager *Manager, shardID, nextVersion, reason, operatorID string) (ShardOperationPlan, error) {
	return ApplyShardOperation(ctx, manager, ShardOperationRequest{
		ShardID:     shardID,
		State:       StateDraining,
		NextVersion: nextVersion,
		Reason:      reason,
		OperatorID:  operatorID,
	})
}

// CloseShard 关闭指定分片。
func CloseShard(ctx context.Context, manager *Manager, shardID, nextVersion, reason, operatorID string) (ShardOperationPlan, error) {
	return ApplyShardOperation(ctx, manager, ShardOperationRequest{
		ShardID:     shardID,
		State:       StateClosed,
		NextVersion: nextVersion,
		Reason:      reason,
		OperatorID:  operatorID,
	})
}

func normShardOpReq(request ShardOperationRequest) ShardOperationRequest {
	request.ShardID = normalizeID(request.ShardID)
	request.State = normalizeState(request.State)
	request.NextVersion = normalizeID(request.NextVersion)
	request.Reason = strings.TrimSpace(request.Reason)
	request.OperatorID = strings.TrimSpace(request.OperatorID)
	request.Metadata = normalizeStringMap(request.Metadata)
	request.UpdatedAt = normalizeTime(request.UpdatedAt)
	if request.UpdatedAt.IsZero() {
		request.UpdatedAt = time.Now().UTC()
	}
	return request
}

func validShardOpState(state string) bool {
	return validState(state)
}

func shardOperationName(state string) string {
	switch state {
	case StateOpen:
		return OperationOpenShard
	case StateDraining:
		return OperationDrainShard
	case StateClosed:
		return OperationCloseShard
	default:
		return "shard_operation"
	}
}

func operationNextVersion(current string, request ShardOperationRequest) string {
	if request.NextVersion != "" {
		return request.NextVersion
	}
	current = normalizeID(current)
	suffix := request.ShardID + "-" + request.State
	if current == "" {
		return suffix
	}
	return current + "-" + suffix
}

func mergeOperationMeta(base map[string]string, request ShardOperationRequest) map[string]string {
	out := cloneStringMap(base)
	if out == nil {
		out = make(map[string]string)
	}
	for key, value := range request.Metadata {
		out[key] = value
	}
	if request.Reason != "" {
		out["operation.reason"] = request.Reason
	}
	if request.OperatorID != "" {
		out["operation.operator_id"] = request.OperatorID
	}
	out["operation.state"] = request.State
	return out
}

func shardOpTargets(ctx context.Context, previous Plan, manager *Manager, shardID string) ([]Target, error) {
	features := shardOpFeatures(previous, shardID)
	out := make([]Target, 0, len(features))
	for _, feature := range features {
		target, ok, err := manager.Resolve(ctx, feature, shardID)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, target)
		}
	}
	return out, nil
}

func shardOpFeatures(plan Plan, shardID string) []string {
	seen := map[string]struct{}{}
	var features []string
	for _, route := range plan.Routes {
		if route.ShardID != shardID {
			continue
		}
		if route.Feature == "" {
			continue
		}
		if _, ok := seen[route.Feature]; ok {
			continue
		}
		seen[route.Feature] = struct{}{}
		features = append(features, route.Feature)
	}
	for _, shard := range plan.Shards {
		if shard.ID != shardID {
			continue
		}
		for _, group := range plan.Groups {
			if group.ID != shard.GroupID {
				continue
			}
			feature := firstNonEmpty(group.Service, "default")
			if _, ok := seen[feature]; !ok {
				seen[feature] = struct{}{}
				features = append(features, feature)
			}
			break
		}
	}
	if len(features) == 0 {
		features = append(features, "default")
	}
	sort.Strings(features)
	return features
}

func shardOpWarnings(request ShardOperationRequest, targets []Target) []string {
	var warnings []string
	if request.State != StateOpen && request.Reason == "" {
		warnings = append(warnings, "non-open shard operation should include reason")
	}
	for _, target := range targets {
		if request.State == StateOpen && !target.Available {
			warnings = append(warnings, "opened shard still unavailable:"+target.Reason)
		}
		if request.State != StateOpen && target.Available {
			warnings = append(warnings, "non-open shard still routes as available:"+target.Feature)
		}
	}
	return warnings
}
