package adminworkflow

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"longheng.io/server/internal/platform/httpx"
)

const (
	defRecordLimit  = 100
	maxFlowRecLimit = 500
)

// AdminOptions 描述运营工作流只读后台接口的挂载方式。
type AdminOptions struct {
	Prefix string
	Wrap   func(string, http.HandlerFunc) http.HandlerFunc
}

// WorkflowRecordSummary 是工作流列表页使用的轻量记录摘要。
type WorkflowRecordSummary struct {
	ID                  string    `json:"id"`
	Workflow            string    `json:"workflow,omitempty"`
	Operation           string    `json:"operation,omitempty"`
	Scope               string    `json:"scope,omitempty"`
	Target              string    `json:"target,omitempty"`
	Actor               string    `json:"actor,omitempty"`
	IdempotencyKey      string    `json:"idempotency_key,omitempty"`
	Phase               Phase     `json:"phase"`
	ApprovalID          string    `json:"approval_id,omitempty"`
	ReceiptID           string    `json:"receipt_id,omitempty"`
	ReceiptStatus       string    `json:"receipt_status,omitempty"`
	HasRollbackNote     bool      `json:"has_rollback_note"`
	RollbackEvidenceRef string    `json:"rollback_evidence_ref,omitempty"`
	Error               string    `json:"error,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type flowRecordFilter struct {
	workflow        string
	phase           Phase
	operation       string
	actor           string
	idempotencyKey  string
	approvalID      string
	receiptID       string
	hasRollbackNote *bool
	limit           int
}

// RegisterAdminRoutes 注册工作流记录和 rollback note 的只读后台接口。
func RegisterAdminRoutes(mux *http.ServeMux, service *Service, options AdminOptions) {
	if mux == nil {
		return
	}
	prefix := strings.TrimRight(strings.TrimSpace(options.Prefix), "/")
	if prefix == "" {
		prefix = "/debug/admin/workflows"
	}
	wrap := options.Wrap
	if wrap == nil {
		wrap = func(_ string, next http.HandlerFunc) http.HandlerFunc { return next }
	}
	mux.HandleFunc(prefix, wrap("adminworkflow", func(w http.ResponseWriter, r *http.Request) {
		handleWorkflowList(w, r, service)
	}))
	mux.HandleFunc(prefix+"/", wrap("adminworkflow", func(w http.ResponseWriter, r *http.Request) {
		suffix := strings.TrimPrefix(r.URL.Path, prefix+"/")
		handleWorkflowItem(w, r, service, suffix)
	}))
}

func handleWorkflowList(w http.ResponseWriter, r *http.Request, service *Service) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeMethodNA(w, http.MethodGet)
		return
	}
	filter, err := workflowFilterReq(r)
	if err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	records, err := service.List(r.Context())
	if err != nil {
		writeWorkflowErr(w, err)
		return
	}
	sort.SliceStable(records, func(i, j int) bool {
		left := records[i].UpdatedAt.UTC()
		right := records[j].UpdatedAt.UTC()
		if !left.Equal(right) {
			return left.After(right)
		}
		return records[i].ID < records[j].ID
	})
	summaries := make([]WorkflowRecordSummary, 0, minWorkflowInt(len(records), filter.limit))
	total := 0
	for _, record := range records {
		if !filter.match(record) {
			continue
		}
		total++
		if len(summaries) >= filter.limit {
			continue
		}
		summaries = append(summaries, WorkflowRecordSummaryFromRecord(record))
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"records": summaries,
		"count":   len(summaries),
		"total":   total,
		"limit":   filter.limit,
	})
}

func handleWorkflowItem(w http.ResponseWriter, r *http.Request, service *Service, suffix string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeMethodNA(w, http.MethodGet)
		return
	}
	id, rollbackNote, ok := parseRecordItemPath(suffix)
	if !ok {
		httpx.WriteJSON(w, http.StatusNotFound, map[string]string{"error": ErrWorkflowNotFound.Error()})
		return
	}
	record, found, err := service.Get(r.Context(), id)
	if err != nil {
		writeWorkflowErr(w, err)
		return
	}
	if !found {
		httpx.WriteJSON(w, http.StatusNotFound, map[string]string{"error": ErrWorkflowNotFound.Error()})
		return
	}
	if rollbackNote {
		if record.RollbackNote == nil || !hasRollbackNote(*record.RollbackNote) {
			httpx.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "rollback note not found"})
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"workflow_id":   record.ID,
			"rollback_note": record.RollbackNote,
		})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"record":  record,
		"summary": WorkflowRecordSummaryFromRecord(record),
	})
}

// WorkflowRecordSummaryFromRecord 从完整工作流记录生成列表摘要。
func WorkflowRecordSummaryFromRecord(record Record) WorkflowRecordSummary {
	record = cloneRecord(record)
	summary := WorkflowRecordSummary{
		ID:             strings.TrimSpace(record.ID),
		Workflow:       strings.TrimSpace(record.Workflow),
		Operation:      record.Command.Operation,
		Scope:          record.Command.Scope,
		Target:         record.Command.Target,
		Actor:          record.Command.Actor,
		IdempotencyKey: record.Command.IdempotencyKey,
		Phase:          record.Phase,
		Error:          strings.TrimSpace(record.Error),
		CreatedAt:      record.CreatedAt.UTC(),
		UpdatedAt:      record.UpdatedAt.UTC(),
	}
	if record.Approval != nil {
		summary.ApprovalID = strings.TrimSpace(record.Approval.ID)
	}
	if record.Receipt != nil {
		summary.ReceiptID = strings.TrimSpace(record.Receipt.ID)
		summary.ReceiptStatus = strings.TrimSpace(record.Receipt.Status)
	}
	if record.RollbackNote != nil && hasRollbackNote(*record.RollbackNote) {
		summary.HasRollbackNote = true
		summary.RollbackEvidenceRef = strings.TrimSpace(record.RollbackNote.EvidenceRef)
	}
	return summary
}

func workflowFilterReq(r *http.Request) (flowRecordFilter, error) {
	query := r.URL.Query()
	limit, err := parseRecordListLimit(query.Get("limit"))
	if err != nil {
		return flowRecordFilter{}, err
	}
	hasRollbackNote, err := parseWorkflowBool(query.Get("has_rollback_note"), "has_rollback_note")
	if err != nil {
		return flowRecordFilter{}, err
	}
	return flowRecordFilter{
		workflow:        strings.TrimSpace(query.Get("workflow")),
		phase:           Phase(strings.TrimSpace(query.Get("phase"))),
		operation:       strings.TrimSpace(query.Get("operation")),
		actor:           strings.TrimSpace(query.Get("actor")),
		idempotencyKey:  strings.TrimSpace(query.Get("idempotency_key")),
		approvalID:      strings.TrimSpace(query.Get("approval_id")),
		receiptID:       strings.TrimSpace(query.Get("receipt_id")),
		hasRollbackNote: hasRollbackNote,
		limit:           limit,
	}, nil
}

func (f flowRecordFilter) match(record Record) bool {
	record = cloneRecord(record)
	if f.workflow != "" && record.Workflow != f.workflow {
		return false
	}
	if f.phase != "" && record.Phase != f.phase {
		return false
	}
	if f.operation != "" && record.Command.Operation != f.operation {
		return false
	}
	if f.actor != "" && record.Command.Actor != f.actor {
		return false
	}
	if f.idempotencyKey != "" && record.Command.IdempotencyKey != f.idempotencyKey {
		return false
	}
	if f.approvalID != "" {
		if record.Approval == nil || record.Approval.ID != f.approvalID {
			return false
		}
	}
	if f.receiptID != "" {
		if record.Receipt == nil || record.Receipt.ID != f.receiptID {
			return false
		}
	}
	if f.hasRollbackNote != nil {
		hasNote := record.RollbackNote != nil && hasRollbackNote(*record.RollbackNote)
		if hasNote != *f.hasRollbackNote {
			return false
		}
	}
	return true
}

func parseRecordItemPath(suffix string) (string, bool, bool) {
	suffix = strings.Trim(suffix, "/")
	if suffix == "" {
		return "", false, false
	}
	rollbackNote := false
	if strings.HasSuffix(suffix, "/rollback-note") {
		rollbackNote = true
		suffix = strings.TrimSuffix(suffix, "/rollback-note")
	}
	if suffix == "" || strings.Contains(suffix, "/") {
		return "", false, false
	}
	id := strings.TrimSpace(suffix)
	if id == "" {
		return "", false, false
	}
	return id, rollbackNote, true
}

func parseRecordListLimit(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defRecordLimit, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 0 {
		return 0, fmt.Errorf("%w: limit must be a non-negative integer", ErrWorkflowInvalid)
	}
	if limit > maxFlowRecLimit {
		return maxFlowRecLimit, nil
	}
	return limit, nil
}

func parseWorkflowBool(raw, name string) (*bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %s must be boolean", ErrWorkflowInvalid, name)
	}
	return &value, nil
}

func writeMethodNA(w http.ResponseWriter, allowed string) {
	if allowed == http.MethodGet {
		allowed = http.MethodGet + ", " + http.MethodHead
	}
	w.Header().Set("Allow", allowed)
	httpx.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
}

func writeWorkflowErr(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, ErrStoreRequired) {
		status = http.StatusServiceUnavailable
	}
	httpx.WriteJSON(w, status, map[string]string{"error": err.Error()})
}

func minWorkflowInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
