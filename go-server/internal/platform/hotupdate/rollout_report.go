package hotupdate

import (
	"fmt"
	"strings"
	"time"
)

const RolloutEvidenceKind = "hotupdate_rollout_evidence"

type RolloutEvidence struct {
	SchemaVersion int           `json:"schema_version"`
	Kind          string        `json:"kind"`
	Name          string        `json:"name"`
	GeneratedAt   time.Time     `json:"generated_at"`
	Spec          RolloutSpec   `json:"spec"`
	Report        RolloutReport `json:"report"`
	Result        string        `json:"result"`
	Valid         bool          `json:"valid"`
	Violations    []string      `json:"violations,omitempty"`
}

func BuildRolloutEvidence(name string, spec RolloutSpec, control Control, generatedAt time.Time) (RolloutEvidence, error) {
	if strings.TrimSpace(name) == "" {
		name = "hotupdate-rollout"
	}
	normalized, err := normalizeRolloutSpec(spec, false)
	if err != nil {
		return RolloutEvidence{}, err
	}
	report, err := EvaluateRollout(control, normalized)
	if err != nil {
		return RolloutEvidence{}, err
	}
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}
	violations := ValidateRolloutReport(report)
	return RolloutEvidence{
		SchemaVersion: 1,
		Kind:          RolloutEvidenceKind,
		Name:          strings.TrimSpace(name),
		GeneratedAt:   generatedAt.UTC(),
		Spec:          normalized,
		Report:        report,
		Result:        rolloutEvResult(report),
		Valid:         len(violations) == 0,
		Violations:    violations,
	}, nil
}

func ValidateRolloutEvidence(evidence RolloutEvidence) error {
	var violations []string
	if evidence.SchemaVersion != 1 {
		violations = append(violations, fmt.Sprintf("schema_version must be 1, got %d", evidence.SchemaVersion))
	}
	if evidence.Kind != RolloutEvidenceKind {
		violations = append(violations, fmt.Sprintf("kind must be %q, got %q", RolloutEvidenceKind, evidence.Kind))
	}
	if strings.TrimSpace(evidence.Name) == "" {
		violations = append(violations, "name is required")
	}
	if evidence.GeneratedAt.IsZero() {
		violations = append(violations, "generated_at is required")
	}
	violations = append(violations, ValidateRolloutReport(evidence.Report)...)
	if len(evidence.Violations) > 0 {
		violations = append(violations, evidence.Violations...)
	}
	if len(violations) > 0 {
		return fmt.Errorf("hotupdate rollout evidence invalid: %s", strings.Join(violations, "; "))
	}
	if !evidence.Valid {
		return fmt.Errorf("hotupdate rollout evidence invalid: valid=false")
	}
	return nil
}

func ValidateRolloutReport(report RolloutReport) []string {
	var violations []string
	if strings.TrimSpace(report.Target) == "" {
		violations = append(violations, "target is required")
	}
	if strings.TrimSpace(report.Version) == "" {
		violations = append(violations, "version is required")
	}
	if report.BatchSize <= 0 {
		violations = append(violations, "batch_size must be positive")
	}
	if report.Total < 0 {
		violations = append(violations, "total must not be negative")
	}
	if report.Published > report.Total {
		violations = append(violations, "published must not exceed total")
	}
	if report.ApplyPublished > report.Published {
		violations = append(violations, "apply_published must not exceed published")
	}
	if report.RollbackPublished > report.Published {
		violations = append(violations, "rollback_published must not exceed published")
	}
	if report.Unpublished+report.Published != report.Total {
		violations = append(violations, "unpublished + published must equal total")
	}
	if report.Pending+report.InProgress+report.Applied+report.Failed+report.Restored > report.Total {
		violations = append(violations, "node terminal/in-flight counts exceed total")
	}
	if report.Complete && (report.Applied != report.Total || report.Failed != 0 || report.NeedsRollback) {
		violations = append(violations, "complete report must have every node applied and no rollback need")
	}
	if report.RollbackComplete && (report.RollbackPublished == 0 || report.Restored != report.RollbackPublished || report.NeedsRollback) {
		violations = append(violations, "rollback_complete report must have every rollback intent restored and no rollback need")
	}
	var sumTotal, sumPublished, sumApplyPublished, sumRollbackPublished, sumUnpublished int
	var sumPending, sumInProgress, sumApplied, sumFailed, sumRestored int
	for _, batch := range report.Batches {
		batchTotal := len(batch.NodeIDs)
		sumTotal += batchTotal
		sumPublished += batch.Published
		sumApplyPublished += batch.ApplyPublished
		sumRollbackPublished += batch.RollbackPublished
		sumUnpublished += batch.Unpublished
		sumPending += batch.Pending
		sumInProgress += batch.InProgress
		sumApplied += batch.Applied
		sumFailed += batch.Failed
		sumRestored += batch.Restored
		if len(batch.Nodes) != batchTotal {
			violations = append(violations, fmt.Sprintf("batch %d nodes length does not match node_ids", batch.Index))
		}
		if batch.Published > batchTotal {
			violations = append(violations, fmt.Sprintf("batch %d published exceeds node count", batch.Index))
		}
		if batch.Unpublished+batch.Published != batchTotal {
			violations = append(violations, fmt.Sprintf("batch %d unpublished + published must equal node count", batch.Index))
		}
		if batch.Complete && batch.Applied != batchTotal {
			violations = append(violations, fmt.Sprintf("batch %d complete requires every node applied", batch.Index))
		}
		if batch.RollbackComplete && (batch.RollbackPublished == 0 || batch.Restored != batch.RollbackPublished) {
			violations = append(violations, fmt.Sprintf("batch %d rollback_complete requires every rollback intent restored", batch.Index))
		}
	}
	if sumTotal != report.Total {
		violations = append(violations, "batch node count sum must equal total")
	}
	if sumPublished != report.Published || sumApplyPublished != report.ApplyPublished || sumRollbackPublished != report.RollbackPublished {
		violations = append(violations, "batch published counts must match report totals")
	}
	if sumUnpublished != report.Unpublished || sumPending != report.Pending || sumInProgress != report.InProgress {
		violations = append(violations, "batch in-flight counts must match report totals")
	}
	if sumApplied != report.Applied || sumFailed != report.Failed || sumRestored != report.Restored {
		violations = append(violations, "batch terminal counts must match report totals")
	}
	return violations
}

func rolloutEvResult(report RolloutReport) string {
	switch {
	case report.Complete:
		return "complete"
	case report.RollbackComplete:
		return "rollback_complete"
	case report.NeedsRollback:
		return "rollback_required"
	case report.InProgress > 0:
		return "in_progress"
	case report.Pending > 0 || report.Unpublished > 0:
		return "pending"
	default:
		return "unknown"
	}
}
