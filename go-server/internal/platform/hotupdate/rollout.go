package hotupdate

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var (
	ErrRolloutInvalid      = errors.New("hot update rollout is invalid")
	ErrRolloutBatchPending = errors.New("hot update rollout batch is pending")
	ErrRolloutComplete     = errors.New("hot update rollout is complete")
)

type RolloutSpec struct {
	Target          string    `json:"target"`
	Version         string    `json:"version"`
	SourceURI       string    `json:"source_uri"`
	Checksum        string    `json:"checksum,omitempty"`
	NodeIDs         []string  `json:"node_ids"`
	BatchSize       int       `json:"batch_size"`
	AvailableAt     time.Time `json:"available_at,omitempty"`
	Reason          string    `json:"reason,omitempty"`
	RequestedBy     string    `json:"requested_by,omitempty"`
	SequenceBase    int64     `json:"sequence_base,omitempty"`
	RollbackVersion string    `json:"rollback_version,omitempty"`
}

type RolloutNodeStatus struct {
	NodeID            string    `json:"node_id"`
	Target            string    `json:"target"`
	Batch             int       `json:"batch"`
	Published         bool      `json:"published"`
	ApplyPublished    bool      `json:"apply_published"`
	RollbackPublished bool      `json:"rollback_published"`
	Applied           bool      `json:"applied"`
	Failed            bool      `json:"failed"`
	Restored          bool      `json:"restored"`
	Stage             Stage     `json:"stage,omitempty"`
	Status            string    `json:"status,omitempty"`
	Detail            string    `json:"detail,omitempty"`
	UpdatedAt         time.Time `json:"updated_at,omitempty"`
}

type RolloutBatchStatus struct {
	Index             int                 `json:"index"`
	NodeIDs           []string            `json:"node_ids"`
	Published         int                 `json:"published"`
	ApplyPublished    int                 `json:"apply_published"`
	RollbackPublished int                 `json:"rollback_published"`
	Unpublished       int                 `json:"unpublished"`
	Pending           int                 `json:"pending"`
	InProgress        int                 `json:"in_progress"`
	Applied           int                 `json:"applied"`
	Failed            int                 `json:"failed"`
	Restored          int                 `json:"restored"`
	Complete          bool                `json:"complete"`
	RollbackComplete  bool                `json:"rollback_complete"`
	Nodes             []RolloutNodeStatus `json:"nodes,omitempty"`
}

type RolloutReport struct {
	Target            string               `json:"target"`
	Version           string               `json:"version"`
	BatchSize         int                  `json:"batch_size"`
	Total             int                  `json:"total"`
	Published         int                  `json:"published"`
	ApplyPublished    int                  `json:"apply_published"`
	RollbackPublished int                  `json:"rollback_published"`
	Unpublished       int                  `json:"unpublished"`
	Pending           int                  `json:"pending"`
	InProgress        int                  `json:"in_progress"`
	Applied           int                  `json:"applied"`
	Failed            int                  `json:"failed"`
	Restored          int                  `json:"restored"`
	Complete          bool                 `json:"complete"`
	RollbackComplete  bool                 `json:"rollback_complete"`
	NeedsRollback     bool                 `json:"needs_rollback"`
	Batches           []RolloutBatchStatus `json:"batches,omitempty"`
}

func PublishNextBatch(ctx context.Context, control Control, spec RolloutSpec) (RolloutBatchStatus, error) {
	if err := contextErr(ctx); err != nil {
		return RolloutBatchStatus{}, err
	}
	normalized, err := normalizeRolloutSpec(spec, true)
	if err != nil {
		return RolloutBatchStatus{}, err
	}
	report, err := EvaluateRollout(control, normalized)
	if err != nil {
		return RolloutBatchStatus{}, err
	}
	// 分批发布只推进第一个尚未发布且前序已完成的批次。
	// 如果当前批次还没全部成功，返回 ErrRolloutBatchPending，避免误把后续节点一起放开。
	for _, batch := range report.Batches {
		if batch.Published == 0 {
			if err := publishApplyBatch(ctx, control, normalized, batch.Index); err != nil {
				return RolloutBatchStatus{}, err
			}
			updated, err := EvaluateRollout(control, normalized)
			if err != nil {
				return RolloutBatchStatus{}, err
			}
			if batch.Index < len(updated.Batches) {
				return updated.Batches[batch.Index], nil
			}
			return batch, nil
		}
		if !batch.Complete {
			return batch, ErrRolloutBatchPending
		}
	}
	return RolloutBatchStatus{}, ErrRolloutComplete
}

func PublishRollback(ctx context.Context, control Control, spec RolloutSpec, nodeIDs []string) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	normalized, err := normalizeRolloutSpec(spec, false)
	if err != nil {
		return err
	}
	if strings.TrimSpace(normalized.RollbackVersion) == "" {
		return fmt.Errorf("%w: rollback_version is required", ErrRolloutInvalid)
	}
	targets := normRollbackNodes(normalized.NodeIDs, nodeIDs)
	if len(targets) == 0 {
		return fmt.Errorf("%w: rollback node_ids are required", ErrRolloutInvalid)
	}
	// 回滚按节点发布 restore 意图，不直接操作节点本地数据；节点侧仍要上报进度。
	for _, nodeID := range targets {
		intent := Intent{
			Action:      ActionRestore,
			Version:     normalized.RollbackVersion,
			Reason:      firstNonEmpty(normalized.Reason, "rollback"),
			RequestedBy: normalized.RequestedBy,
			Sequence:    rolloutSequence(normalized, 10000+rolloutNodeIndex(normalized.NodeIDs, nodeID), 0),
		}
		if err := control.Publish(ctx, rolloutNodeTarget(normalized.Target, nodeID), intent); err != nil {
			return err
		}
	}
	return nil
}

func EvaluateRollout(control Control, spec RolloutSpec) (RolloutReport, error) {
	if control == nil {
		return RolloutReport{}, ErrControlLogRequired
	}
	normalized, err := normalizeRolloutSpec(spec, false)
	if err != nil {
		return RolloutReport{}, err
	}
	snapshot := control.Snapshot()
	report := RolloutReport{
		Target:    normalized.Target,
		Version:   normalized.Version,
		BatchSize: normalized.BatchSize,
		Total:     len(normalized.NodeIDs),
	}
	batches := rolloutBatches(normalized.NodeIDs, normalized.BatchSize)
	report.Batches = make([]RolloutBatchStatus, 0, len(batches))
	// 评估只读取控制面快照，不产生副作用；release gate 和 GM 页面都可以安全调用。
	for batchIndex, nodes := range batches {
		batch := RolloutBatchStatus{
			Index:   batchIndex,
			NodeIDs: append([]string(nil), nodes...),
			Nodes:   make([]RolloutNodeStatus, 0, len(nodes)),
		}
		for _, nodeID := range nodes {
			status := rolloutNodeStatus(snapshot, normalized, batchIndex, nodeID)
			if status.Published {
				report.Published++
				batch.Published++
			}
			if status.ApplyPublished {
				report.ApplyPublished++
				batch.ApplyPublished++
			}
			if status.RollbackPublished {
				report.RollbackPublished++
				batch.RollbackPublished++
			}
			if !status.Published {
				report.Unpublished++
				batch.Unpublished++
			}
			if rolloutNodePending(status) {
				report.Pending++
				batch.Pending++
			}
			if rolloutNodeBusy(status) {
				report.InProgress++
				batch.InProgress++
			}
			if status.Applied {
				report.Applied++
				batch.Applied++
			}
			if status.Failed {
				report.Failed++
				batch.Failed++
			}
			if status.Restored {
				report.Restored++
				batch.Restored++
			}
			batch.Nodes = append(batch.Nodes, status)
		}
		batch.Complete = len(nodes) > 0 && batch.Applied == len(nodes)
		batch.RollbackComplete = batch.RollbackPublished > 0 && batch.Restored == batch.RollbackPublished
		report.Batches = append(report.Batches, batch)
	}
	report.Complete = report.Total > 0 && report.Applied == report.Total
	report.RollbackComplete = report.RollbackPublished > 0 && report.Restored == report.RollbackPublished
	report.NeedsRollback = report.Failed > 0
	return report, nil
}

func publishApplyBatch(ctx context.Context, control Control, spec RolloutSpec, batchIndex int) error {
	if control == nil {
		return ErrControlLogRequired
	}
	batches := rolloutBatches(spec.NodeIDs, spec.BatchSize)
	if batchIndex < 0 || batchIndex >= len(batches) {
		return fmt.Errorf("%w: batch %d out of range", ErrRolloutInvalid, batchIndex)
	}
	// 每个节点写独立 target，方便单节点失败、补偿和回滚定位。
	for idx, nodeID := range batches[batchIndex] {
		intent := Intent{
			Action:      ActionApply,
			Version:     spec.Version,
			SourceURI:   spec.SourceURI,
			Checksum:    spec.Checksum,
			AvailableAt: spec.AvailableAt,
			Reason:      spec.Reason,
			RequestedBy: spec.RequestedBy,
			Sequence:    rolloutSequence(spec, batchIndex, idx),
		}
		if err := control.Publish(ctx, rolloutNodeTarget(spec.Target, nodeID), intent); err != nil {
			return err
		}
	}
	return nil
}

func rolloutNodeStatus(snapshot ControlSnapshot, spec RolloutSpec, batch int, nodeID string) RolloutNodeStatus {
	target := rolloutNodeTarget(spec.Target, nodeID)
	status := RolloutNodeStatus{NodeID: nodeID, Target: target, Batch: batch}
	// 同一个 target 只看匹配版本的 apply/restore 意图，避免旧发布污染当前报告。
	if intent, ok := snapshot.Intents[target]; ok && intent.Version == spec.Version && intent.Action == ActionApply {
		status.ApplyPublished = true
		status.Published = true
	}
	if rollbackVersion := strings.TrimSpace(spec.RollbackVersion); rollbackVersion != "" {
		if intent, ok := snapshot.Intents[target]; ok && intent.Version == rollbackVersion && intent.Action == ActionRestore {
			status.RollbackPublished = true
			status.Published = true
		}
	}
	for _, progress := range snapshot.Progress[target] {
		if progress.Version != spec.Version && progress.Version != spec.RollbackVersion {
			continue
		}
		// 进度按 UpdatedAt 取最新一条，节点重复上报不会把状态倒退。
		if progress.UpdatedAt.Before(status.UpdatedAt) {
			continue
		}
		status.Stage = progress.Stage
		status.Status = progress.Status
		status.Detail = progress.Detail
		status.UpdatedAt = progress.UpdatedAt
		status.Applied = progress.Version == spec.Version && progress.Stage == StageApplied
		status.Failed = progress.Version == spec.Version && progress.Stage == StageFailed
		status.Restored = progress.Version == spec.RollbackVersion && progress.Stage == StageRestored
	}
	return status
}

func rolloutNodePending(status RolloutNodeStatus) bool {
	if !status.Published || status.Applied || status.Failed || status.Restored {
		return false
	}
	return status.Stage == ""
}

func rolloutNodeBusy(status RolloutNodeStatus) bool {
	if !status.Published || status.Applied || status.Failed || status.Restored {
		return false
	}
	switch status.Stage {
	case StageAccepted, StageDownloading, StageScheduled, StageApplying, StageRestoring:
		return true
	default:
		return false
	}
}

func normalizeRolloutSpec(spec RolloutSpec, requireSource bool) (RolloutSpec, error) {
	target, err := normalizeTarget(spec.Target)
	if err != nil {
		return RolloutSpec{}, err
	}
	spec.Target = strings.Trim(target, "/")
	spec.Version = strings.TrimSpace(spec.Version)
	spec.SourceURI = strings.TrimSpace(spec.SourceURI)
	spec.Checksum = strings.TrimSpace(spec.Checksum)
	spec.Reason = strings.TrimSpace(spec.Reason)
	spec.RequestedBy = strings.TrimSpace(spec.RequestedBy)
	spec.RollbackVersion = strings.TrimSpace(spec.RollbackVersion)
	if spec.Version == "" {
		return RolloutSpec{}, ErrVersionRequired
	}
	if requireSource && spec.SourceURI == "" {
		return RolloutSpec{}, ErrSourceRequired
	}
	spec.NodeIDs = normalizeNodeIDs(spec.NodeIDs)
	if len(spec.NodeIDs) == 0 {
		return RolloutSpec{}, fmt.Errorf("%w: node_ids are required", ErrRolloutInvalid)
	}
	if spec.BatchSize <= 0 {
		spec.BatchSize = 1
	}
	return spec, nil
}

func normalizeNodeIDs(nodeIDs []string) []string {
	seen := make(map[string]struct{}, len(nodeIDs))
	out := make([]string, 0, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		nodeID = strings.Trim(strings.TrimSpace(nodeID), "/")
		if nodeID == "" {
			continue
		}
		if _, ok := seen[nodeID]; ok {
			continue
		}
		seen[nodeID] = struct{}{}
		out = append(out, nodeID)
	}
	sort.Strings(out)
	return out
}

func normRollbackNodes(all []string, selected []string) []string {
	if len(selected) == 0 {
		return append([]string(nil), all...)
	}
	allowed := make(map[string]struct{}, len(all))
	for _, nodeID := range all {
		allowed[nodeID] = struct{}{}
	}
	out := make([]string, 0, len(selected))
	seen := make(map[string]struct{}, len(selected))
	for _, nodeID := range normalizeNodeIDs(selected) {
		if _, ok := allowed[nodeID]; !ok {
			continue
		}
		if _, ok := seen[nodeID]; ok {
			continue
		}
		seen[nodeID] = struct{}{}
		out = append(out, nodeID)
	}
	return out
}

func rolloutBatches(nodeIDs []string, batchSize int) [][]string {
	if batchSize <= 0 {
		batchSize = 1
	}
	var batches [][]string
	for start := 0; start < len(nodeIDs); start += batchSize {
		end := start + batchSize
		if end > len(nodeIDs) {
			end = len(nodeIDs)
		}
		batches = append(batches, append([]string(nil), nodeIDs[start:end]...))
	}
	return batches
}

func rolloutNodeTarget(target, nodeID string) string {
	return strings.Trim(strings.TrimSpace(target), "/") + "/" + strings.Trim(strings.TrimSpace(nodeID), "/")
}

func rolloutSequence(spec RolloutSpec, batchIndex int, nodeIndex int) int64 {
	if spec.SequenceBase == 0 {
		return 0
	}
	return spec.SequenceBase + int64(batchIndex*1000+nodeIndex+1)
}

func rolloutNodeIndex(nodes []string, nodeID string) int {
	for idx, current := range nodes {
		if current == nodeID {
			return idx
		}
	}
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
