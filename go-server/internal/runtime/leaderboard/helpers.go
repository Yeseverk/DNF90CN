package leaderboard

import (
	"context"
	"encoding/base64"
	"fmt"
	"math"
	"strings"
)

func normalizeSubmission(submission Submission) (Submission, error) {
	submission.OwnerID = strings.TrimSpace(submission.OwnerID)
	if submission.OwnerID == "" {
		return Submission{}, fmt.Errorf("%w: owner_id is required", ErrInvalidSubmission)
	}
	submission.Metadata = cloneStringMap(submission.Metadata)
	return submission, nil
}

func addScoreValue(left int64, right int64) (int64, error) {
	if (right > 0 && left > math.MaxInt64-right) || (right < 0 && left < math.MinInt64-right) {
		return 0, fmt.Errorf("%w: %w", ErrInvalidSubmission, ErrScoreOverflow)
	}
	return left + right, nil
}

func normRepairReq(request RepairRequest) (RepairRequest, error) {
	request.OwnerID = strings.TrimSpace(request.OwnerID)
	if request.OwnerID == "" {
		return RepairRequest{}, fmt.Errorf("%w: owner_id is required", ErrInvalidSubmission)
	}
	request.Reason = strings.TrimSpace(request.Reason)
	request.OperatorID = strings.TrimSpace(request.OperatorID)
	request.RequestID = strings.TrimSpace(request.RequestID)
	request.Metadata = cloneStringMap(request.Metadata)
	return request, nil
}

func repairIdempotencyKey(leaderboardID string, requestID string) string {
	leaderboardID = strings.TrimSpace(leaderboardID)
	requestID = strings.TrimSpace(requestID)
	if leaderboardID == "" || requestID == "" {
		return ""
	}
	return strings.Join([]string{"v1", encodeKeyPart(leaderboardID), encodeKeyPart(requestID)}, ":")
}

func encodeKeyPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "_"
	}
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func normCaptureOpts(options CaptureOptions) CaptureOptions {
	if options.Limit <= 0 {
		options.Limit = defaultListLimit
	}
	if options.Limit > maxListLimit {
		options.Limit = maxListLimit
	}
	options.Reason = strings.TrimSpace(options.Reason)
	options.OperatorID = strings.TrimSpace(options.OperatorID)
	options.RequestID = strings.TrimSpace(options.RequestID)
	options.Metadata = cloneStringMap(options.Metadata)
	return options
}

func normalizeSortOrder(sortOrder string) (string, error) {
	sortOrder = strings.ToLower(strings.TrimSpace(sortOrder))
	switch sortOrder {
	case "", SortDescending:
		return SortDescending, nil
	case SortAscending:
		return SortAscending, nil
	default:
		return "", fmt.Errorf("%w: unsupported sort_order %q", ErrInvalidDefinition, sortOrder)
	}
}

func normalizeOperator(operator string) (string, error) {
	operator = strings.ToLower(strings.TrimSpace(operator))
	switch operator {
	case "", OperatorBest:
		return OperatorBest, nil
	case OperatorSet, OperatorIncrement:
		return operator, nil
	default:
		return "", fmt.Errorf("%w: unsupported operator %q", ErrInvalidDefinition, operator)
	}
}

func normalizeListOptions(options ListOptions) (int, int) {
	offset := options.Offset
	if offset < 0 {
		offset = 0
	}
	limit := options.Limit
	if limit <= 0 {
		limit = defaultListLimit
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}
	return offset, limit
}

func betterThan(candidate Record, existing Record, sortOrder string) bool {
	switch {
	case candidate.Score != existing.Score:
		if sortOrder == SortAscending {
			return candidate.Score < existing.Score
		}
		return candidate.Score > existing.Score
	case candidate.Subscore != existing.Subscore:
		if sortOrder == SortAscending {
			return candidate.Subscore < existing.Subscore
		}
		return candidate.Subscore > existing.Subscore
	default:
		return false
	}
}

func compareRecords(a Record, b Record, sortOrder string) int {
	if a.Score != b.Score {
		if sortOrder == SortAscending {
			if a.Score < b.Score {
				return -1
			}
			return 1
		}
		if a.Score > b.Score {
			return -1
		}
		return 1
	}
	if a.Subscore != b.Subscore {
		if sortOrder == SortAscending {
			if a.Subscore < b.Subscore {
				return -1
			}
			return 1
		}
		if a.Subscore > b.Subscore {
			return -1
		}
		return 1
	}
	if !a.CreatedAt.Equal(b.CreatedAt) {
		if a.CreatedAt.Before(b.CreatedAt) {
			return -1
		}
		return 1
	}
	if a.OwnerID < b.OwnerID {
		return -1
	}
	if a.OwnerID > b.OwnerID {
		return 1
	}
	if a.LeaderboardID < b.LeaderboardID {
		return -1
	}
	if a.LeaderboardID > b.LeaderboardID {
		return 1
	}
	return 0
}

func cloneDefinition(definition Definition) Definition {
	definition.Metadata = cloneStringMap(definition.Metadata)
	return definition
}

func cloneRecord(record Record) Record {
	record.Metadata = cloneStringMap(record.Metadata)
	return record
}

func cloneRecords(records []Record) []Record {
	out := make([]Record, len(records))
	for i, record := range records {
		out[i] = cloneRecord(record)
	}
	return out
}

func cloneDefinitionMap(definitions map[string]Definition) map[string]Definition {
	out := make(map[string]Definition, len(definitions))
	for id, definition := range definitions {
		out[id] = cloneDefinition(definition)
	}
	return out
}

func cloneRecordMap(records map[string]map[string]Record) map[string]map[string]Record {
	out := make(map[string]map[string]Record, len(records))
	for leaderboardID, boardRecords := range records {
		out[leaderboardID] = cloneRecordSet(boardRecords)
	}
	return out
}

func cloneRecordSet(records map[string]Record) map[string]Record {
	out := make(map[string]Record, len(records))
	for ownerID, record := range records {
		out[ownerID] = cloneRecord(record)
	}
	return out
}

func cloneRepairMap(repairs map[string]RepairReceipt) map[string]RepairReceipt {
	out := make(map[string]RepairReceipt, len(repairs))
	for key, receipt := range repairs {
		out[key] = cloneRepairReceipt(receipt)
	}
	return out
}

func cloneRepairReceipt(receipt RepairReceipt) RepairReceipt {
	if receipt.Before != nil {
		before := cloneRecord(*receipt.Before)
		receipt.Before = &before
	}
	if receipt.After != nil {
		after := cloneRecord(*receipt.After)
		receipt.After = &after
	}
	receipt.Metadata = cloneStringMap(receipt.Metadata)
	return receipt
}

func cloneCapture(capture Capture) Capture {
	capture.Records = cloneRecords(capture.Records)
	capture.Metadata = cloneStringMap(capture.Metadata)
	return capture
}

func repairHistoryEntry(receipt RepairReceipt, record *Record) HistoryEntry {
	entry := HistoryEntry{
		Action:        HistoryActionRepairRecord,
		LeaderboardID: receipt.LeaderboardID,
		OwnerID:       receipt.OwnerID,
		Reason:        receipt.Reason,
		OperatorID:    receipt.OperatorID,
		RequestID:     receipt.RequestID,
		Metadata:      cloneStringMap(receipt.Metadata),
		At:            receipt.At,
	}
	if record != nil {
		cloned := cloneRecord(*record)
		entry.Record = &cloned
	} else if receipt.Before != nil {
		cloned := cloneRecord(*receipt.Before)
		entry.Record = &cloned
	}
	return entry
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func ctxErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}
