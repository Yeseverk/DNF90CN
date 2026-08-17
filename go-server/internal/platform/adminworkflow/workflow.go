package adminworkflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"longheng.io/server/internal/platform/admincmd"
)

var (
	ErrWorkflowNotFound      = errors.New("admin workflow not found")
	ErrWorkflowInvalid       = errors.New("admin workflow is invalid")
	ErrWorkflowPhase         = errors.New("admin workflow phase is invalid")
	ErrPreviewerRequired     = errors.New("admin workflow previewer is required")
	ErrExecutorRequired      = errors.New("admin workflow executor is required")
	ErrApprovalStoreRequired = errors.New("admin workflow approval store is required")
	ErrDryRunWarnings        = errors.New("admin workflow dry-run has blocking warnings")
	ErrStoreRequired         = errors.New("admin workflow store is required")
)

type Phase string

const (
	PhaseDryRun          Phase = "dry_run"
	PhasePendingApproval Phase = "pending_approval"
	PhaseApproved        Phase = "approved"
	PhaseRejected        Phase = "rejected"
	PhaseExecuted        Phase = "executed"
	PhaseFailed          Phase = "failed"
	PhaseRollbackNoted   Phase = "rollback_noted"
)

type Diff struct {
	Path   string `json:"path"`
	Before any    `json:"before,omitempty"`
	After  any    `json:"after,omitempty"`
	Risk   string `json:"risk,omitempty"`
}

type DryRun struct {
	Summary  string            `json:"summary,omitempty"`
	Diffs    []Diff            `json:"diffs,omitempty"`
	Warnings []string          `json:"warnings,omitempty"`
	Meta     map[string]string `json:"meta,omitempty"`
}

type RollbackNote struct {
	Summary     string            `json:"summary,omitempty"`
	Steps       []string          `json:"steps,omitempty"`
	EvidenceRef string            `json:"evidence_ref,omitempty"`
	Meta        map[string]string `json:"meta,omitempty"`
}

type Record struct {
	ID           string                    `json:"id"`
	Workflow     string                    `json:"workflow,omitempty"`
	Command      admincmd.Command          `json:"command"`
	Phase        Phase                     `json:"phase"`
	DryRun       DryRun                    `json:"dry_run,omitempty"`
	Approval     *admincmd.ApprovalRequest `json:"approval,omitempty"`
	Receipt      *admincmd.Receipt         `json:"receipt,omitempty"`
	RollbackNote *RollbackNote             `json:"rollback_note,omitempty"`
	Error        string                    `json:"error,omitempty"`
	CreatedAt    time.Time                 `json:"created_at"`
	UpdatedAt    time.Time                 `json:"updated_at"`
}

type StartResult struct {
	Record    Record `json:"record"`
	Duplicate bool   `json:"duplicate,omitempty"`
}

type Previewer interface {
	DryRunAdminWorkflow(context.Context, admincmd.Command) (DryRun, error)
}

type PreviewerFunc func(context.Context, admincmd.Command) (DryRun, error)

func (fn PreviewerFunc) DryRunAdminWorkflow(ctx context.Context, command admincmd.Command) (DryRun, error) {
	if fn == nil {
		return DryRun{}, ErrPreviewerRequired
	}
	return fn(ctx, command)
}

type Executor interface {
	ExecuteAdminWorkflow(context.Context, Record) (admincmd.Receipt, RollbackNote, error)
}

type ExecutorFunc func(context.Context, Record) (admincmd.Receipt, RollbackNote, error)

func (fn ExecutorFunc) ExecuteAdminWorkflow(ctx context.Context, record Record) (admincmd.Receipt, RollbackNote, error) {
	if fn == nil {
		return admincmd.Receipt{}, RollbackNote{}, ErrExecutorRequired
	}
	return fn(ctx, record)
}

type Store interface {
	SaveWorkflow(context.Context, Record) (Record, error)
	GetWorkflow(context.Context, string) (Record, bool, error)
	ListWorkflows(context.Context) ([]Record, error)
}

type Policy struct {
	CommandPolicy   admincmd.Policy
	RequireApproval bool
	BlockWarnings   bool
}

func DangerousPolicy() Policy {
	return Policy{
		CommandPolicy:   admincmd.DangerousApprovalPolicy().CommandPolicy,
		RequireApproval: true,
		BlockWarnings:   false,
	}
}

type ServiceOptions struct {
	Store    Store
	Approval *admincmd.ApprovalStore
	Policy   Policy
	Now      func() time.Time
}

type Service struct {
	store    Store
	approval *admincmd.ApprovalStore
	policy   Policy
	now      func() time.Time
}

func NewService(options ServiceOptions) *Service {
	policy := options.Policy
	if policy.CommandPolicy == (admincmd.Policy{}) {
		policy = DangerousPolicy()
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Service{
		store:    options.Store,
		approval: options.Approval,
		policy:   policy,
		now:      now,
	}
}

func (s *Service) StartDryRun(ctx context.Context, command admincmd.Command, previewer Previewer) (StartResult, error) {
	if err := ctxErr(ctx); err != nil {
		return StartResult{}, err
	}
	if s == nil || s.store == nil {
		return StartResult{}, ErrStoreRequired
	}
	if previewer == nil {
		return StartResult{}, ErrPreviewerRequired
	}
	command = admincmd.Normalize(command)
	if err := admincmd.Validate(command, s.policy.CommandPolicy); err != nil {
		return StartResult{}, err
	}
	id, err := WorkflowID(command)
	if err != nil {
		return StartResult{}, err
	}
	if current, ok, err := s.store.GetWorkflow(ctx, id); err != nil || ok {
		return StartResult{Record: current, Duplicate: ok}, err
	}
	dryRun, err := previewer.DryRunAdminWorkflow(ctx, command)
	if err != nil {
		return StartResult{}, err
	}
	dryRun = normalizeDryRun(dryRun)
	if !hasDryRunPreview(dryRun) {
		return StartResult{}, fmt.Errorf("%w: dry-run must include summary or diffs", ErrWorkflowInvalid)
	}
	if s.policy.BlockWarnings && len(dryRun.Warnings) > 0 {
		return StartResult{}, fmt.Errorf("%w: %s", ErrDryRunWarnings, strings.Join(dryRun.Warnings, "; "))
	}
	now := s.now().UTC()
	record := Record{
		ID:        id,
		Command:   cloneCommand(command),
		Phase:     PhaseDryRun,
		DryRun:    dryRun,
		CreatedAt: now,
		UpdatedAt: now,
	}
	record, err = s.store.SaveWorkflow(ctx, record)
	if err != nil {
		return StartResult{}, err
	}
	return StartResult{Record: record}, nil
}

func (s *Service) SubmitApproval(ctx context.Context, id string) (Record, error) {
	if err := ctxErr(ctx); err != nil {
		return Record{}, err
	}
	if s == nil || s.store == nil {
		return Record{}, ErrStoreRequired
	}
	if s.approval == nil {
		return Record{}, ErrApprovalStoreRequired
	}
	record, err := s.mustGet(ctx, id)
	if err != nil {
		return Record{}, err
	}
	if record.Phase != PhaseDryRun {
		if record.Approval != nil {
			return cloneRecord(record), nil
		}
		return Record{}, fmt.Errorf("%w: expected %s got %s", ErrWorkflowPhase, PhaseDryRun, record.Phase)
	}
	request, err := s.approval.Submit(ctx, record.Command)
	if err != nil {
		return Record{}, err
	}
	record.Approval = &request
	record.Phase = PhasePendingApproval
	record.UpdatedAt = s.now().UTC()
	return s.store.SaveWorkflow(ctx, record)
}

func (s *Service) Approve(ctx context.Context, id string, decision admincmd.ApprovalDecision) (Record, error) {
	return s.decide(ctx, id, decision, true)
}

func (s *Service) Reject(ctx context.Context, id string, decision admincmd.ApprovalDecision) (Record, error) {
	return s.decide(ctx, id, decision, false)
}

func (s *Service) Execute(ctx context.Context, id string, executor Executor) (Record, error) {
	if err := ctxErr(ctx); err != nil {
		return Record{}, err
	}
	if s == nil || s.store == nil {
		return Record{}, ErrStoreRequired
	}
	if executor == nil {
		return Record{}, ErrExecutorRequired
	}
	record, err := s.mustGet(ctx, id)
	if err != nil {
		return Record{}, err
	}
	if record.Phase == PhaseExecuted || record.Phase == PhaseRollbackNoted {
		return record, nil
	}
	if s.policy.RequireApproval {
		if record.Phase != PhaseApproved && record.Phase != PhaseFailed {
			return Record{}, fmt.Errorf("%w: expected %s got %s", ErrWorkflowPhase, PhaseApproved, record.Phase)
		}
		if s.approval == nil || record.Approval == nil {
			return Record{}, ErrApprovalStoreRequired
		}
		approvalReceipt, err := s.approval.ExecuteApproved(ctx, record.Approval.ID)
		if err != nil {
			return Record{}, err
		}
		request, ok := s.approval.Get(record.Approval.ID)
		if ok {
			record.Approval = &request
			record.Command = cloneCommand(request.Command)
		}
		if record.Receipt == nil {
			record.Receipt = &approvalReceipt
		}
	} else if record.Phase != PhaseDryRun && record.Phase != PhaseFailed {
		return Record{}, fmt.Errorf("%w: expected %s got %s", ErrWorkflowPhase, PhaseDryRun, record.Phase)
	}

	receipt, rollback, err := executor.ExecuteAdminWorkflow(ctx, cloneRecord(record))
	if err != nil {
		record.Phase = PhaseFailed
		record.Error = strings.TrimSpace(err.Error())
		record.UpdatedAt = s.now().UTC()
		_, _ = s.store.SaveWorkflow(ctx, record)
		return Record{}, err
	}
	if receipt.ID == "" {
		receipt = fallbackReceipt(record.Command, s.now)
	}
	record.Receipt = &receipt
	rollback = normRollbackNote(rollback)
	if hasRollbackNote(rollback) {
		record.RollbackNote = &rollback
		record.Phase = PhaseRollbackNoted
	} else {
		record.Phase = PhaseExecuted
	}
	record.Error = ""
	record.UpdatedAt = s.now().UTC()
	return s.store.SaveWorkflow(ctx, record)
}

func (s *Service) Get(ctx context.Context, id string) (Record, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return Record{}, false, err
	}
	if s == nil || s.store == nil {
		return Record{}, false, ErrStoreRequired
	}
	return s.store.GetWorkflow(ctx, strings.TrimSpace(id))
}

func (s *Service) List(ctx context.Context) ([]Record, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if s == nil || s.store == nil {
		return nil, ErrStoreRequired
	}
	return s.store.ListWorkflows(ctx)
}

func (s *Service) decide(ctx context.Context, id string, decision admincmd.ApprovalDecision, approve bool) (Record, error) {
	if err := ctxErr(ctx); err != nil {
		return Record{}, err
	}
	if s == nil || s.store == nil {
		return Record{}, ErrStoreRequired
	}
	if s.approval == nil {
		return Record{}, ErrApprovalStoreRequired
	}
	record, err := s.mustGet(ctx, id)
	if err != nil {
		return Record{}, err
	}
	if record.Phase != PhasePendingApproval || record.Approval == nil {
		return Record{}, fmt.Errorf("%w: expected %s got %s", ErrWorkflowPhase, PhasePendingApproval, record.Phase)
	}
	var request admincmd.ApprovalRequest
	if approve {
		request, err = s.approval.Approve(ctx, record.Approval.ID, decision)
		record.Phase = PhaseApproved
	} else {
		request, err = s.approval.Reject(ctx, record.Approval.ID, decision)
		record.Phase = PhaseRejected
	}
	if err != nil {
		return Record{}, err
	}
	record.Approval = &request
	record.Command = cloneCommand(request.Command)
	record.UpdatedAt = s.now().UTC()
	return s.store.SaveWorkflow(ctx, record)
}

func (s *Service) mustGet(ctx context.Context, id string) (Record, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Record{}, ErrWorkflowNotFound
	}
	record, ok, err := s.store.GetWorkflow(ctx, id)
	if err != nil {
		return Record{}, err
	}
	if !ok {
		return Record{}, ErrWorkflowNotFound
	}
	return record, nil
}

func WorkflowID(command admincmd.Command) (string, error) {
	command = admincmd.Normalize(command)
	paramsHash, err := admincmd.ParamsHash(command.Params)
	if err != nil {
		return "", err
	}
	if command.Operation == "" || command.IdempotencyKey == "" {
		return "", ErrWorkflowInvalid
	}
	seed := strings.Join([]string{
		command.Operation,
		command.Scope,
		command.Environment,
		command.ShardID,
		command.Target,
		command.Actor,
		command.IdempotencyKey,
		paramsHash,
	}, "|")
	sum := sha256.Sum256([]byte(seed))
	return "workflow-" + hex.EncodeToString(sum[:12]), nil
}

type MemoryStore struct {
	mu      sync.Mutex
	records map[string]Record
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{records: make(map[string]Record)}
}

func (s *MemoryStore) SaveWorkflow(ctx context.Context, record Record) (Record, error) {
	if err := ctxErr(ctx); err != nil {
		return Record{}, err
	}
	if s == nil {
		return Record{}, ErrStoreRequired
	}
	record.ID = strings.TrimSpace(record.ID)
	if record.ID == "" || record.Phase == "" {
		return Record{}, ErrWorkflowInvalid
	}
	record = cloneRecord(record)
	s.mu.Lock()
	if s.records == nil {
		s.records = make(map[string]Record)
	}
	s.records[record.ID] = record
	s.mu.Unlock()
	return cloneRecord(record), nil
}

func (s *MemoryStore) GetWorkflow(ctx context.Context, id string) (Record, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return Record{}, false, err
	}
	if s == nil {
		return Record{}, false, ErrStoreRequired
	}
	id = strings.TrimSpace(id)
	s.mu.Lock()
	record, ok := s.records[id]
	s.mu.Unlock()
	return cloneRecord(record), ok, nil
}

func (s *MemoryStore) ListWorkflows(ctx context.Context) ([]Record, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if s == nil {
		return nil, ErrStoreRequired
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Record, 0, len(s.records))
	for _, record := range s.records {
		out = append(out, cloneRecord(record))
	}
	return out, nil
}

func fallbackReceipt(command admincmd.Command, now func() time.Time) admincmd.Receipt {
	if now == nil {
		now = time.Now
	}
	receipt, err := admincmd.NewReceipt(command, "executed", now().UTC())
	if err != nil {
		return admincmd.Receipt{Status: "executed", CreatedAt: now().UTC()}
	}
	return receipt
}

func normalizeDryRun(dryRun DryRun) DryRun {
	dryRun.Summary = strings.TrimSpace(dryRun.Summary)
	dryRun.Diffs = cloneDiffs(dryRun.Diffs)
	dryRun.Warnings = normalizeStrings(dryRun.Warnings)
	dryRun.Meta = cloneStringMap(dryRun.Meta)
	return dryRun
}

func normRollbackNote(note RollbackNote) RollbackNote {
	note.Summary = strings.TrimSpace(note.Summary)
	note.Steps = normalizeStrings(note.Steps)
	note.EvidenceRef = strings.TrimSpace(note.EvidenceRef)
	note.Meta = cloneStringMap(note.Meta)
	return note
}

func hasRollbackNote(note RollbackNote) bool {
	return note.Summary != "" || len(note.Steps) > 0 || note.EvidenceRef != "" || len(note.Meta) > 0
}

func hasDryRunPreview(dryRun DryRun) bool {
	return dryRun.Summary != "" || len(dryRun.Diffs) > 0
}

func cloneRecord(record Record) Record {
	record.Command = cloneCommand(record.Command)
	record.DryRun = normalizeDryRun(record.DryRun)
	if record.Approval != nil {
		approval := *record.Approval
		approval.Command = cloneCommand(approval.Command)
		if approval.Receipt != nil {
			receipt := *approval.Receipt
			approval.Receipt = &receipt
		}
		record.Approval = &approval
	}
	if record.Receipt != nil {
		receipt := *record.Receipt
		record.Receipt = &receipt
	}
	if record.RollbackNote != nil {
		note := normRollbackNote(*record.RollbackNote)
		record.RollbackNote = &note
	}
	return record
}

func cloneCommand(command admincmd.Command) admincmd.Command {
	command = admincmd.Normalize(command)
	command.Params = cloneParams(command.Params)
	return command
}

func cloneParams(params map[string]any) map[string]any {
	if len(params) == 0 {
		return nil
	}
	out := make(map[string]any, len(params))
	for key, value := range params {
		out[key] = value
	}
	return out
}

func cloneDiffs(diffs []Diff) []Diff {
	if len(diffs) == 0 {
		return nil
	}
	out := make([]Diff, 0, len(diffs))
	for _, diff := range diffs {
		diff.Path = strings.TrimSpace(diff.Path)
		diff.Risk = strings.TrimSpace(diff.Risk)
		out = append(out, diff)
	}
	return out
}

func normalizeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
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
