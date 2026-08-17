package servergroup

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
	defArchiveLimit     = 100
	maxArchiveListLimit = 500
)

// MergeArchiveSummary 是后台列表页使用的轻量归档摘要。
type MergeArchiveSummary struct {
	ArchiveID         string            `json:"archive_id"`
	Workflow          string            `json:"workflow"`
	Stage             string            `json:"stage"`
	WorkflowID        string            `json:"workflow_id,omitempty"`
	ApprovalID        string            `json:"approval_id,omitempty"`
	IdempotencyKey    string            `json:"idempotency_key,omitempty"`
	OperatorID        string            `json:"operator_id,omitempty"`
	Reason            string            `json:"reason,omitempty"`
	RequestID         string            `json:"request_id,omitempty"`
	MainShardID       string            `json:"main_shard_id,omitempty"`
	Shards            []string          `json:"shards,omitempty"`
	GeneratedAt       time.Time         `json:"generated_at"`
	Ready             bool              `json:"ready"`
	OK                bool              `json:"ok"`
	Applied           bool              `json:"applied"`
	RollbackPointID   string            `json:"rollback_point_id,omitempty"`
	EvidenceCount     int               `json:"evidence_count"`
	FindingCount      int               `json:"finding_count"`
	WarningCount      int               `json:"warning_count"`
	RollbackCount     int               `json:"rollback_count"`
	ModuleReportCount int               `json:"module_report_count"`
	Meta              map[string]string `json:"meta,omitempty"`
}

type archiveListFilter struct {
	stage          string
	workflowID     string
	approvalID     string
	requestID      string
	operatorID     string
	idempotencyKey string
	ok             *bool
	ready          *bool
	applied        *bool
	limit          int
}

func regArchiveRoutes(mux *http.ServeMux, prefix string, wrap func(string, http.HandlerFunc) http.HandlerFunc, archives MergeArchiveStore) {
	listPath := prefix + "/merge/workflow/archives"
	mux.HandleFunc(listPath, wrap("servergroup", func(w http.ResponseWriter, r *http.Request) {
		handleArchiveList(w, r, archives)
	}))
	mux.HandleFunc(listPath+"/", wrap("servergroup", func(w http.ResponseWriter, r *http.Request) {
		suffix := strings.TrimPrefix(r.URL.Path, listPath+"/")
		handleArchiveItem(w, r, archives, suffix)
	}))
}

func handleArchiveList(w http.ResponseWriter, r *http.Request, archives MergeArchiveStore) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeMethodNA(w, http.MethodGet)
		return
	}
	if archives == nil {
		httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": ErrStoreEmpty.Error()})
		return
	}
	filter, err := archiveFilterFromReq(r)
	if err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	all, err := archives.ListMergeArchives(r.Context())
	if err != nil {
		writeArchiveErr(w, err)
		return
	}
	sort.SliceStable(all, func(i, j int) bool {
		left := normalizeTime(all[i].GeneratedAt)
		right := normalizeTime(all[j].GeneratedAt)
		if !left.Equal(right) {
			return left.After(right)
		}
		return all[i].ArchiveID < all[j].ArchiveID
	})
	summaries := make([]MergeArchiveSummary, 0, minInt(len(all), filter.limit))
	total := 0
	for _, archive := range all {
		if !filter.match(archive) {
			continue
		}
		total++
		if len(summaries) >= filter.limit {
			continue
		}
		summaries = append(summaries, MergeArchiveSummaryFromArchive(archive))
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"archives": summaries,
		"count":    len(summaries),
		"total":    total,
		"limit":    filter.limit,
	})
}

func handleArchiveItem(w http.ResponseWriter, r *http.Request, archives MergeArchiveStore, suffix string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeMethodNA(w, http.MethodGet)
		return
	}
	if archives == nil {
		httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": ErrStoreEmpty.Error()})
		return
	}
	archiveID, export, ok := parseArchiveItemPath(suffix)
	if !ok {
		httpx.WriteJSON(w, http.StatusNotFound, map[string]string{"error": ErrMergeArchiveNotFound.Error()})
		return
	}
	archive, found, err := archives.GetMergeArchive(r.Context(), archiveID)
	if err != nil {
		writeArchiveErr(w, err)
		return
	}
	if !found {
		httpx.WriteJSON(w, http.StatusNotFound, map[string]string{"error": ErrMergeArchiveNotFound.Error()})
		return
	}
	if export {
		fileName, err := archiveFileName(archive.ArchiveID)
		if err != nil {
			httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fileName))
		httpx.WriteJSON(w, http.StatusOK, archive)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"archive": archive,
		"summary": MergeArchiveSummaryFromArchive(archive),
	})
}

// MergeArchiveSummaryFromArchive 从完整归档生成列表摘要。
func MergeArchiveSummaryFromArchive(archive MergeArchive) MergeArchiveSummary {
	archive = cloneMergeArchive(archive)
	return MergeArchiveSummary{
		ArchiveID:         archive.ArchiveID,
		Workflow:          archive.Workflow,
		Stage:             archive.Stage,
		WorkflowID:        archive.WorkflowID,
		ApprovalID:        archive.ApprovalID,
		IdempotencyKey:    archive.IdempotencyKey,
		OperatorID:        archive.OperatorID,
		Reason:            archive.Reason,
		RequestID:         archive.Request.ID,
		MainShardID:       archive.Request.MainShardID,
		Shards:            append([]string(nil), archive.Request.Shards...),
		GeneratedAt:       normalizeTime(archive.GeneratedAt),
		Ready:             archive.Ready,
		OK:                archive.OK,
		Applied:           archive.Applied,
		RollbackPointID:   archive.RollbackPoint.ID,
		EvidenceCount:     len(archive.Evidence),
		FindingCount:      len(archive.Findings),
		WarningCount:      len(archive.Warnings),
		RollbackCount:     len(archive.Rollback),
		ModuleReportCount: len(archive.ModuleReports),
		Meta:              cloneStringMap(archive.Meta),
	}
}

func archiveFilterFromReq(r *http.Request) (archiveListFilter, error) {
	query := r.URL.Query()
	limit, err := parseArchiveLimit(query.Get("limit"))
	if err != nil {
		return archiveListFilter{}, err
	}
	ok, err := parseArchiveBool(query.Get("ok"), "ok")
	if err != nil {
		return archiveListFilter{}, err
	}
	ready, err := parseArchiveBool(query.Get("ready"), "ready")
	if err != nil {
		return archiveListFilter{}, err
	}
	applied, err := parseArchiveBool(query.Get("applied"), "applied")
	if err != nil {
		return archiveListFilter{}, err
	}
	return archiveListFilter{
		stage:          normalizeID(query.Get("stage")),
		workflowID:     normalizeID(query.Get("workflow_id")),
		approvalID:     normalizeID(query.Get("approval_id")),
		requestID:      normalizeID(query.Get("request_id")),
		operatorID:     normalizeID(query.Get("operator_id")),
		idempotencyKey: strings.TrimSpace(query.Get("idempotency_key")),
		ok:             ok,
		ready:          ready,
		applied:        applied,
		limit:          limit,
	}, nil
}

func (f archiveListFilter) match(archive MergeArchive) bool {
	archive = cloneMergeArchive(archive)
	if f.stage != "" && normalizeID(archive.Stage) != f.stage {
		return false
	}
	if f.workflowID != "" && normalizeID(archive.WorkflowID) != f.workflowID {
		return false
	}
	if f.approvalID != "" && normalizeID(archive.ApprovalID) != f.approvalID {
		return false
	}
	if f.requestID != "" && normalizeID(archive.Request.ID) != f.requestID {
		return false
	}
	if f.operatorID != "" && normalizeID(archive.OperatorID) != f.operatorID {
		return false
	}
	if f.idempotencyKey != "" && strings.TrimSpace(archive.IdempotencyKey) != f.idempotencyKey {
		return false
	}
	if f.ok != nil && archive.OK != *f.ok {
		return false
	}
	if f.ready != nil && archive.Ready != *f.ready {
		return false
	}
	if f.applied != nil && archive.Applied != *f.applied {
		return false
	}
	return true
}

func parseArchiveItemPath(suffix string) (string, bool, bool) {
	suffix = strings.Trim(suffix, "/")
	if suffix == "" {
		return "", false, false
	}
	export := false
	if strings.HasSuffix(suffix, "/export") {
		export = true
		suffix = strings.TrimSuffix(suffix, "/export")
	}
	if suffix == "" || strings.Contains(suffix, "/") {
		return "", false, false
	}
	archiveID := normalizeID(suffix)
	if archiveID == "" {
		return "", false, false
	}
	return archiveID, export, true
}

func parseArchiveLimit(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defArchiveLimit, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 0 {
		return 0, fmt.Errorf("%w: limit must be a non-negative integer", ErrInvalidMigration)
	}
	if limit > maxArchiveListLimit {
		return maxArchiveListLimit, nil
	}
	return limit, nil
}

func parseArchiveBool(raw, name string) (*bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %s must be boolean", ErrInvalidMigration, name)
	}
	return &value, nil
}

func writeArchiveErr(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, ErrStoreEmpty) {
		status = http.StatusServiceUnavailable
	}
	httpx.WriteJSON(w, status, map[string]string{"error": err.Error()})
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
