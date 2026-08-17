package admincmd

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var (
	ErrApprovalNotFound     = errors.New("admin approval not found")
	ErrApprovalNotPending   = errors.New("admin approval is not pending")
	ErrApprovalNotApproved  = errors.New("admin approval is not approved")
	ErrApprovalSelfApprove  = errors.New("admin approval requires different approver")
	ErrApprovalConfirmation = errors.New("admin approval confirmation is invalid")
)

const (
	ApprovalPending  = "pending"
	ApprovalApproved = "approved"
	ApprovalRejected = "rejected"
	ApprovalExecuted = "executed"
)

type ApprovalPolicy struct {
	CommandPolicy            Policy
	RequireDifferentApprover bool
	RequireConfirmation      bool
}

type ApprovalDecision struct {
	Approver     string `json:"approver"`
	Reason       string `json:"reason,omitempty"`
	Confirmation string `json:"confirmation,omitempty"`
}

type ApprovalRequest struct {
	ID             string    `json:"id"`
	Command        Command   `json:"command"`
	Status         string    `json:"status"`
	Approver       string    `json:"approver,omitempty"`
	DecisionReason string    `json:"decision_reason,omitempty"`
	RequestedAt    time.Time `json:"requested_at"`
	DecidedAt      time.Time `json:"decided_at,omitempty"`
	ExecutedAt     time.Time `json:"executed_at,omitempty"`
	Receipt        *Receipt  `json:"receipt,omitempty"`
}

type ApprovalStore struct {
	mu       sync.Mutex
	now      func() time.Time
	policy   ApprovalPolicy
	requests map[string]ApprovalRequest
}

func DangerousApprovalPolicy() ApprovalPolicy {
	return ApprovalPolicy{
		CommandPolicy: Policy{
			RequireActor:          true,
			RequireTarget:         true,
			RequireReason:         true,
			RequireIdempotencyKey: true,
			RequireConfirmation:   false,
		},
		RequireDifferentApprover: true,
		RequireConfirmation:      true,
	}
}

func NewApprovalStore(policy ApprovalPolicy, now func() time.Time) *ApprovalStore {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	if policy.CommandPolicy == (Policy{}) {
		policy = DangerousApprovalPolicy()
	}
	return &ApprovalStore{
		now:      now,
		policy:   policy,
		requests: make(map[string]ApprovalRequest),
	}
}

func (s *ApprovalStore) Submit(ctx context.Context, command Command) (ApprovalRequest, error) {
	if err := ctxErr(ctx); err != nil {
		return ApprovalRequest{}, err
	}
	if s == nil {
		return ApprovalRequest{}, ErrApprovalNotFound
	}
	command = Normalize(command)
	if err := Validate(command, s.policy.CommandPolicy); err != nil {
		return ApprovalRequest{}, err
	}
	id, err := approvalID(command)
	if err != nil {
		return ApprovalRequest{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if current, ok := s.requests[id]; ok {
		return cloneApprovalRequest(current), nil
	}
	request := ApprovalRequest{
		ID:          id,
		Command:     command,
		Status:      ApprovalPending,
		RequestedAt: s.now().UTC(),
	}
	s.requests[id] = request
	return cloneApprovalRequest(request), nil
}

func (s *ApprovalStore) Approve(ctx context.Context, id string, decision ApprovalDecision) (ApprovalRequest, error) {
	return s.decide(ctx, id, decision, ApprovalApproved)
}

func (s *ApprovalStore) Reject(ctx context.Context, id string, decision ApprovalDecision) (ApprovalRequest, error) {
	return s.decide(ctx, id, decision, ApprovalRejected)
}

func (s *ApprovalStore) ExecuteApproved(ctx context.Context, id string) (Receipt, error) {
	if err := ctxErr(ctx); err != nil {
		return Receipt{}, err
	}
	if s == nil {
		return Receipt{}, ErrApprovalNotFound
	}
	id = strings.TrimSpace(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	request, ok := s.requests[id]
	if !ok {
		return Receipt{}, ErrApprovalNotFound
	}
	if request.Status == ApprovalExecuted && request.Receipt != nil {
		return *request.Receipt, nil
	}
	if request.Status != ApprovalApproved {
		return Receipt{}, ErrApprovalNotApproved
	}
	if err := Validate(request.Command, DangerousPolicy()); err != nil {
		return Receipt{}, err
	}
	receipt, err := NewReceipt(request.Command, ApprovalExecuted, s.now().UTC())
	if err != nil {
		return Receipt{}, err
	}
	request.Status = ApprovalExecuted
	request.ExecutedAt = receipt.CreatedAt
	request.Receipt = &receipt
	s.requests[id] = request
	return receipt, nil
}

func (s *ApprovalStore) Get(id string) (ApprovalRequest, bool) {
	if s == nil {
		return ApprovalRequest{}, false
	}
	id = strings.TrimSpace(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	request, ok := s.requests[id]
	return cloneApprovalRequest(request), ok
}

func (s *ApprovalStore) List() []ApprovalRequest {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ApprovalRequest, 0, len(s.requests))
	for _, request := range s.requests {
		out = append(out, cloneApprovalRequest(request))
	}
	return out
}

func (s *ApprovalStore) decide(ctx context.Context, id string, decision ApprovalDecision, status string) (ApprovalRequest, error) {
	if err := ctxErr(ctx); err != nil {
		return ApprovalRequest{}, err
	}
	if s == nil {
		return ApprovalRequest{}, ErrApprovalNotFound
	}
	id = strings.TrimSpace(id)
	decision.Approver = strings.TrimSpace(decision.Approver)
	decision.Reason = strings.TrimSpace(decision.Reason)
	decision.Confirmation = strings.TrimSpace(decision.Confirmation)
	if decision.Approver == "" {
		return ApprovalRequest{}, ErrMissingActor
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	request, ok := s.requests[id]
	if !ok {
		return ApprovalRequest{}, ErrApprovalNotFound
	}
	if request.Status != ApprovalPending {
		return ApprovalRequest{}, ErrApprovalNotPending
	}
	if s.policy.RequireDifferentApprover && strings.EqualFold(decision.Approver, strings.TrimSpace(request.Command.Actor)) {
		return ApprovalRequest{}, ErrApprovalSelfApprove
	}
	if status == ApprovalApproved && s.policy.RequireConfirmation && !confirmMatches(request.Command.Operation, decision.Confirmation) {
		return ApprovalRequest{}, fmt.Errorf("%w: expected %s", ErrApprovalConfirmation, request.Command.Operation)
	}
	request.Status = status
	request.Approver = decision.Approver
	request.DecisionReason = decision.Reason
	request.DecidedAt = s.now().UTC()
	if status == ApprovalApproved {
		request.Command.Confirmation = request.Command.Operation
	}
	s.requests[id] = request
	return cloneApprovalRequest(request), nil
}

func confirmMatches(operation string, confirmation string) bool {
	expected := strings.ToLower(strings.TrimSpace(operation))
	got := strings.ToLower(strings.TrimSpace(confirmation))
	if expected == "" || got == "" || len(expected) != len(got) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(got)) == 1
}

func approvalID(command Command) (string, error) {
	hash, err := ParamsHash(command.Params)
	if err != nil {
		return "", err
	}
	seed := strings.Join([]string{
		command.Operation,
		command.Scope,
		command.Environment,
		command.ShardID,
		command.Target,
		command.Actor,
		command.IdempotencyKey,
		hash,
	}, "|")
	sum := sha256.Sum256([]byte(seed))
	return "approval-" + hex.EncodeToString(sum[:12]), nil
}

func ApprovalID(command Command) (string, error) {
	return approvalID(Normalize(command))
}

func cloneApprovalRequest(request ApprovalRequest) ApprovalRequest {
	request.Command.Params = cloneParams(request.Command.Params)
	if request.Receipt != nil {
		receipt := *request.Receipt
		request.Receipt = &receipt
	}
	return request
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

func ctxErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}
