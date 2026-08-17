package servergroup

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrRollbackVersionMismatch 表示回滚点版本和当前计划版本不匹配。
var ErrRollbackVersionMismatch = errors.New("server group rollback version mismatch")

const (
	// RollbackOperationMerge 表示合服操作回滚。
	RollbackOperationMerge = "merge"
	// RollbackOperationShardOperation 表示分片状态操作回滚。
	RollbackOperationShardOperation = "shard_operation"
	// RollbackOperationWarZoneRefresh 表示战区刷新操作回滚。
	RollbackOperationWarZoneRefresh = "warzone_refresh"
)

// RollbackPoint 是区服运营动作的通用回滚锚点，只保存平台路由计划快照，不包含项目业务表数据。
type RollbackPoint struct {
	ID              string    `json:"id"`
	Operation       string    `json:"operation"`
	PreviousVersion string    `json:"previous_version,omitempty"`
	NextVersion     string    `json:"next_version,omitempty"`
	Reason          string    `json:"reason,omitempty"`
	OperatorID      string    `json:"operator_id,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	Snapshot        Plan      `json:"snapshot"`
}

// RollbackApplyRequest 是应用回滚点的请求。
type RollbackApplyRequest struct {
	RollbackPoint        RollbackPoint `json:"rollback_point"`
	RestoreVersion       string        `json:"restore_version,omitempty"`
	Reason               string        `json:"reason,omitempty"`
	OperatorID           string        `json:"operator_id,omitempty"`
	UpdatedAt            time.Time     `json:"updated_at,omitempty"`
	AllowVersionMismatch bool          `json:"allow_version_mismatch,omitempty"`
}

// RollbackApplyResult 是回滚预演或应用的结果。
type RollbackApplyResult struct {
	Applied         bool                 `json:"applied"`
	Operation       string               `json:"operation"`
	Reason          string               `json:"reason,omitempty"`
	OperatorID      string               `json:"operator_id,omitempty"`
	Previous        Plan                 `json:"previous"`
	Restored        Plan                 `json:"restored"`
	RollbackPoint   RollbackPoint        `json:"rollback_point"`
	VersionMismatch bool                 `json:"version_mismatch,omitempty"`
	Request         RollbackApplyRequest `json:"request"`
}

func newRollbackPoint(operation, previousVersion, nextVersion, reason, operatorID string, snapshot Plan, at time.Time) RollbackPoint {
	operation = normalizeID(operation)
	previousVersion = normalizeID(previousVersion)
	nextVersion = normalizeID(nextVersion)
	reason = strings.TrimSpace(reason)
	operatorID = strings.TrimSpace(operatorID)
	at = normalizeTime(at)
	if at.IsZero() {
		at = time.Now().UTC()
	}
	id := fmt.Sprintf("%s-%s-%s-%s", operation, previousVersion, nextVersion, at.Format("20060102t150405z"))
	return RollbackPoint{
		ID:              normalizeID(id),
		Operation:       operation,
		PreviousVersion: previousVersion,
		NextVersion:     nextVersion,
		Reason:          reason,
		OperatorID:      operatorID,
		CreatedAt:       at,
		Snapshot:        clonePlan(snapshot),
	}
}

func cloneRollbackPoint(point RollbackPoint) RollbackPoint {
	point.Snapshot = clonePlan(point.Snapshot)
	return point
}

// PrepareRollbackApply 预演回滚点应用结果。
func PrepareRollbackApply(ctx context.Context, current Plan, request RollbackApplyRequest) (RollbackApplyResult, error) {
	if err := ctxErr(ctx); err != nil {
		return RollbackApplyResult{}, err
	}
	request = normRollbackApply(request)
	point := request.RollbackPoint
	if point.ID == "" || point.Operation == "" {
		return RollbackApplyResult{}, fmt.Errorf("%w: rollback point id and operation are required", ErrInvalidPlan)
	}
	normalizedCurrent, _, err := normalizePlan(current)
	if err != nil {
		return RollbackApplyResult{}, err
	}
	restored, _, err := normalizePlan(point.Snapshot)
	if err != nil {
		return RollbackApplyResult{}, err
	}
	if restored.Version == "" {
		return RollbackApplyResult{}, fmt.Errorf("%w: rollback snapshot version is required", ErrInvalidPlan)
	}
	if request.RestoreVersion != "" {
		restored.Version = request.RestoreVersion
	}
	if request.UpdatedAt.IsZero() {
		request.UpdatedAt = time.Now().UTC()
	}
	restored.UpdatedAt = request.UpdatedAt
	if _, _, err := normalizePlan(restored); err != nil {
		return RollbackApplyResult{}, err
	}
	versionMismatch := point.NextVersion != "" && normalizedCurrent.Version != point.NextVersion
	if versionMismatch && !request.AllowVersionMismatch {
		return RollbackApplyResult{}, fmt.Errorf("%w: current=%s expected=%s", ErrRollbackVersionMismatch, normalizedCurrent.Version, point.NextVersion)
	}
	return RollbackApplyResult{
		Applied:         false,
		Operation:       point.Operation,
		Reason:          request.Reason,
		OperatorID:      request.OperatorID,
		Previous:        normalizedCurrent,
		Restored:        restored,
		RollbackPoint:   cloneRollbackPoint(point),
		VersionMismatch: versionMismatch,
		Request:         request,
	}, nil
}

// ApplyRollbackPoint 将回滚点应用到服务器分组管理器。
func ApplyRollbackPoint(ctx context.Context, manager *Manager, request RollbackApplyRequest) (RollbackApplyResult, error) {
	if manager == nil {
		return RollbackApplyResult{}, ErrNotFound
	}
	result, err := PrepareRollbackApply(ctx, manager.Snapshot(), request)
	if err != nil {
		return RollbackApplyResult{}, err
	}
	if err := manager.Replace(ctx, result.Restored); err != nil {
		return RollbackApplyResult{}, err
	}
	result.Applied = true
	return result, nil
}

func normRollbackApply(request RollbackApplyRequest) RollbackApplyRequest {
	request.RollbackPoint = normRollbackPoint(request.RollbackPoint)
	request.RestoreVersion = normalizeID(request.RestoreVersion)
	request.Reason = strings.TrimSpace(request.Reason)
	request.OperatorID = strings.TrimSpace(request.OperatorID)
	request.UpdatedAt = normalizeTime(request.UpdatedAt)
	return request
}

func normRollbackPoint(point RollbackPoint) RollbackPoint {
	point.ID = normalizeID(point.ID)
	point.Operation = normalizeID(point.Operation)
	point.PreviousVersion = normalizeID(point.PreviousVersion)
	point.NextVersion = normalizeID(point.NextVersion)
	point.Reason = strings.TrimSpace(point.Reason)
	point.OperatorID = strings.TrimSpace(point.OperatorID)
	point.CreatedAt = normalizeTime(point.CreatedAt)
	point.Snapshot = clonePlan(point.Snapshot)
	return point
}
