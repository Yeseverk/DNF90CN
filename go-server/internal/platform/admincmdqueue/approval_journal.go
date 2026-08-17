package admincmdqueue

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"longheng.io/server/internal/platform/admincmd"
	"longheng.io/server/internal/platform/eventlog"
)

const DefaultApprovalStream = "admin.approval"

const (
	ApprovalEventSubmitted = "submitted"
	ApprovalEventApproved  = "approved"
	ApprovalEventRejected  = "rejected"
	ApprovalEventExecuted  = "executed"

	approvalEventDecided = "decided"
)

var ErrApprovalJournalRequired = errors.New("admin approval journal eventlog is required")

type ApprovalJournalOptions struct {
	Log    *eventlog.Log
	Stream string
	Policy admincmd.ApprovalPolicy
	Now    func() time.Time
}

type ApprovalJournal struct {
	log    *eventlog.Log
	stream string
	policy admincmd.ApprovalPolicy
	now    func() time.Time
}

type ApprovalEvent struct {
	Request admincmd.ApprovalRequest `json:"request"`
}

type ApprovalResult struct {
	Request   admincmd.ApprovalRequest `json:"request"`
	Event     eventlog.Event           `json:"event"`
	Duplicate bool                     `json:"duplicate,omitempty"`
}

func NewApprovalJournal(options ApprovalJournalOptions) (*ApprovalJournal, error) {
	if options.Log == nil {
		return nil, ErrApprovalJournalRequired
	}
	stream := strings.TrimSpace(options.Stream)
	if stream == "" {
		stream = DefaultApprovalStream
	}
	policy := options.Policy
	if policy.CommandPolicy == (admincmd.Policy{}) {
		policy = admincmd.DangerousApprovalPolicy()
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &ApprovalJournal{log: options.Log, stream: stream, policy: policy, now: now}, nil
}

func (j *ApprovalJournal) Submit(ctx context.Context, command admincmd.Command) (ApprovalResult, error) {
	if err := ctxErr(ctx); err != nil {
		return ApprovalResult{}, err
	}
	if j == nil || j.log == nil {
		return ApprovalResult{}, ErrApprovalJournalRequired
	}
	command = admincmd.Normalize(command)
	if err := admincmd.Validate(command, j.policy.CommandPolicy); err != nil {
		return ApprovalResult{}, err
	}
	id, err := admincmd.ApprovalID(command)
	if err != nil {
		return ApprovalResult{}, err
	}
	request := admincmd.ApprovalRequest{
		ID:          id,
		Command:     command,
		Status:      admincmd.ApprovalPending,
		RequestedAt: j.now().UTC(),
	}
	event, duplicate, err := j.append(ctx, request, ApprovalEventSubmitted, approvalIdemKey(command.IdempotencyKey, ApprovalEventSubmitted))
	if err != nil {
		return ApprovalResult{}, err
	}
	got, err := decodeApprovalEvent(event)
	if err != nil {
		return ApprovalResult{}, err
	}
	if got.ID != request.ID {
		return ApprovalResult{}, fmt.Errorf("%w: approval=%s", ErrIdempotencyConflict, got.ID)
	}
	return ApprovalResult{Request: got, Event: event, Duplicate: duplicate}, nil
}

func (j *ApprovalJournal) Approve(ctx context.Context, id string, decision admincmd.ApprovalDecision) (ApprovalResult, error) {
	return j.decide(ctx, id, decision, admincmd.ApprovalApproved, ApprovalEventApproved)
}

func (j *ApprovalJournal) Reject(ctx context.Context, id string, decision admincmd.ApprovalDecision) (ApprovalResult, error) {
	return j.decide(ctx, id, decision, admincmd.ApprovalRejected, ApprovalEventRejected)
}

func (j *ApprovalJournal) ExecuteApproved(ctx context.Context, id string) (admincmd.Receipt, ApprovalResult, error) {
	if err := ctxErr(ctx); err != nil {
		return admincmd.Receipt{}, ApprovalResult{}, err
	}
	request, ok, err := j.Latest(ctx, id)
	if err != nil || !ok {
		if err != nil {
			return admincmd.Receipt{}, ApprovalResult{}, err
		}
		return admincmd.Receipt{}, ApprovalResult{}, admincmd.ErrApprovalNotFound
	}
	if request.Status == admincmd.ApprovalExecuted && request.Receipt != nil {
		return *request.Receipt, ApprovalResult{Request: request}, nil
	}
	if request.Status != admincmd.ApprovalApproved {
		return admincmd.Receipt{}, ApprovalResult{}, admincmd.ErrApprovalNotApproved
	}
	if err := admincmd.Validate(request.Command, admincmd.DangerousPolicy()); err != nil {
		return admincmd.Receipt{}, ApprovalResult{}, err
	}
	receipt, err := admincmd.NewReceipt(request.Command, admincmd.ApprovalExecuted, j.now().UTC())
	if err != nil {
		return admincmd.Receipt{}, ApprovalResult{}, err
	}
	request.Status = admincmd.ApprovalExecuted
	request.ExecutedAt = receipt.CreatedAt
	request.Receipt = &receipt
	event, duplicate, err := j.append(ctx, request, ApprovalEventExecuted, approvalIdemKey(request.ID, ApprovalEventExecuted))
	if err != nil {
		return admincmd.Receipt{}, ApprovalResult{}, err
	}
	got, err := decodeApprovalEvent(event)
	if err != nil {
		return admincmd.Receipt{}, ApprovalResult{}, err
	}
	if got.Receipt != nil {
		receipt = *got.Receipt
	}
	return receipt, ApprovalResult{Request: got, Event: event, Duplicate: duplicate}, nil
}

func (j *ApprovalJournal) Latest(ctx context.Context, id string) (admincmd.ApprovalRequest, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return admincmd.ApprovalRequest{}, false, err
	}
	if j == nil || j.log == nil {
		return admincmd.ApprovalRequest{}, false, ErrApprovalJournalRequired
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return admincmd.ApprovalRequest{}, false, nil
	}
	events, err := j.log.ListByStreamAggregate(ctx, j.stream, id, 1)
	if err != nil {
		return admincmd.ApprovalRequest{}, false, err
	}
	if len(events) == 0 {
		return admincmd.ApprovalRequest{}, false, nil
	}
	request, err := decodeApprovalEvent(events[0])
	if err != nil {
		return admincmd.ApprovalRequest{}, false, err
	}
	return request, true, nil
}

func (j *ApprovalJournal) decide(ctx context.Context, id string, decision admincmd.ApprovalDecision, status string, eventType string) (ApprovalResult, error) {
	if err := ctxErr(ctx); err != nil {
		return ApprovalResult{}, err
	}
	request, ok, err := j.Latest(ctx, id)
	if err != nil || !ok {
		if err != nil {
			return ApprovalResult{}, err
		}
		return ApprovalResult{}, admincmd.ErrApprovalNotFound
	}
	if request.Status != admincmd.ApprovalPending {
		return ApprovalResult{}, admincmd.ErrApprovalNotPending
	}
	decision.Approver = strings.TrimSpace(decision.Approver)
	decision.Reason = strings.TrimSpace(decision.Reason)
	decision.Confirmation = strings.TrimSpace(decision.Confirmation)
	if decision.Approver == "" {
		return ApprovalResult{}, admincmd.ErrMissingActor
	}
	if j.policy.RequireDifferentApprover && strings.EqualFold(decision.Approver, request.Command.Actor) {
		return ApprovalResult{}, admincmd.ErrApprovalSelfApprove
	}
	if status == admincmd.ApprovalApproved && j.policy.RequireConfirmation && decision.Confirmation != request.Command.Operation {
		return ApprovalResult{}, fmt.Errorf("%w: expected %s", admincmd.ErrApprovalConfirmation, request.Command.Operation)
	}
	request.Status = status
	request.Approver = decision.Approver
	request.DecisionReason = decision.Reason
	request.DecidedAt = j.now().UTC()
	if status == admincmd.ApprovalApproved {
		request.Command.Confirmation = decision.Confirmation
	}
	event, duplicate, err := j.appendDecision(ctx, request, eventType)
	if err != nil {
		return ApprovalResult{}, err
	}
	got, err := decodeApprovalEvent(event)
	if err != nil {
		return ApprovalResult{}, err
	}
	if got.Status != status {
		return ApprovalResult{Request: got, Event: event, Duplicate: duplicate}, admincmd.ErrApprovalNotPending
	}
	return ApprovalResult{Request: got, Event: event, Duplicate: duplicate}, nil
}

func (j *ApprovalJournal) append(ctx context.Context, request admincmd.ApprovalRequest, eventType string, idempotencyKey string) (eventlog.Event, bool, error) {
	return j.appendWithEventID(ctx, request, eventType, idempotencyKey, approvalEventID(request.ID, eventType))
}

func (j *ApprovalJournal) appendDecision(ctx context.Context, request admincmd.ApprovalRequest, eventType string) (eventlog.Event, bool, error) {
	id := approvalEventID(request.ID, approvalEventDecided)
	event, duplicate, err := j.appendWithEventID(ctx, request, eventType, approvalIdemKey(request.ID, approvalEventDecided), id)
	if errors.Is(err, eventlog.ErrEventExists) || errors.Is(err, eventlog.ErrIdempotencyConflict) {
		existing, ok, getErr := j.log.Get(ctx, id)
		if getErr != nil {
			return eventlog.Event{}, false, getErr
		}
		if ok {
			return existing, true, nil
		}
	}
	return event, duplicate, err
}

func (j *ApprovalJournal) appendWithEventID(ctx context.Context, request admincmd.ApprovalRequest, eventType string, idempotencyKey string, eventID string) (eventlog.Event, bool, error) {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey != "" {
		if existing, ok, err := j.log.GetByIdempotencyKey(ctx, idempotencyKey); err != nil {
			return eventlog.Event{}, false, err
		} else if ok {
			return existing, true, nil
		}
	}
	payload, err := json.Marshal(ApprovalEvent{Request: request})
	if err != nil {
		return eventlog.Event{}, false, err
	}
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		eventID = approvalEventID(request.ID, eventType)
	}
	event, err := j.log.Append(ctx, eventlog.Event{
		ID:             eventID,
		Stream:         j.stream,
		Type:           eventType,
		AggregateID:    request.ID,
		IdempotencyKey: idempotencyKey,
		Payload:        payload,
		Status:         eventlog.StatusPublished,
		Headers: map[string]string{
			"admin_operation": request.Command.Operation,
			"admin_actor":     request.Command.Actor,
			"admin_approval":  request.ID,
			"approval_status": request.Status,
		},
	})
	return event, false, err
}

func decodeApprovalEvent(event eventlog.Event) (admincmd.ApprovalRequest, error) {
	var envelope ApprovalEvent
	if len(event.Payload) == 0 {
		return admincmd.ApprovalRequest{}, ErrInvalidCommandEvent
	}
	if err := json.Unmarshal(event.Payload, &envelope); err != nil {
		return admincmd.ApprovalRequest{}, fmt.Errorf("%w: %w", ErrInvalidCommandEvent, err)
	}
	envelope.Request.Command = admincmd.Normalize(envelope.Request.Command)
	if envelope.Request.ID == "" || envelope.Request.Command.Operation == "" || envelope.Request.Status == "" {
		return admincmd.ApprovalRequest{}, ErrInvalidCommandEvent
	}
	return envelope.Request, nil
}

func approvalEventID(approvalID string, eventType string) string {
	return strings.Join([]string{"adminapproval", "event", "v1", encodeApprovalPart(approvalID), encodeApprovalPart(eventType)}, ":")
}

func approvalIdemKey(key string, eventType string) string {
	return strings.Join([]string{"adminapproval", "v1", encodeApprovalPart(key), encodeApprovalPart(eventType)}, ":")
}

func encodeApprovalPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "_"
	}
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func ctxErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
