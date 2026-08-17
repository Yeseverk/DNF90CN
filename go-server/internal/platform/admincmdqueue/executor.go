package admincmdqueue

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"longheng.io/server/internal/platform/admincmd"
	"longheng.io/server/internal/platform/eventlog"
)

var (
	ErrEventLogRequired     = errors.New("admin command eventlog is required")
	ErrIdempotencyConflict  = errors.New("admin command idempotency key conflicts with an existing command")
	ErrInvalidCommandEvent  = errors.New("admin command event payload is invalid")
	ErrCommandHandlerNeeded = errors.New("admin command handler is required")
)

const (
	DefaultStream = "admin.command"

	ReceiptStatusAccepted   = "accepted"
	ReceiptStatusProcessing = "processing"
	ReceiptStatusSucceeded  = "succeeded"
	ReceiptStatusFailed     = "failed"
	ReceiptStatusDeadLetter = "dead_letter"
)

type ExecutorOptions struct {
	Log    *eventlog.Log
	Stream string
	Now    func() time.Time
}

type Executor struct {
	log    *eventlog.Log
	stream string
	now    func() time.Time
}

type SubmitResult struct {
	Receipt   admincmd.Receipt `json:"receipt"`
	Event     eventlog.Event   `json:"event"`
	Duplicate bool             `json:"duplicate"`
}

type CommandEnvelope struct {
	Command     admincmd.Command `json:"command"`
	Receipt     admincmd.Receipt `json:"receipt"`
	CommandHash string           `json:"command_hash"`
}

type Handler interface {
	ExecuteAdminCommand(context.Context, admincmd.Command) error
}

type HandlerFunc func(context.Context, admincmd.Command) error

func (fn HandlerFunc) ExecuteAdminCommand(ctx context.Context, command admincmd.Command) error {
	if fn == nil {
		return ErrCommandHandlerNeeded
	}
	return fn(ctx, command)
}

func NewExecutor(options ExecutorOptions) (*Executor, error) {
	if options.Log == nil {
		return nil, ErrEventLogRequired
	}
	stream := strings.TrimSpace(options.Stream)
	if stream == "" {
		stream = DefaultStream
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Executor{log: options.Log, stream: stream, now: now}, nil
}

func (e *Executor) Submit(ctx context.Context, command admincmd.Command, policy admincmd.Policy) (SubmitResult, error) {
	if e == nil || e.log == nil {
		return SubmitResult{}, ErrEventLogRequired
	}
	command = admincmd.Normalize(command)
	if err := admincmd.Validate(command, policy); err != nil {
		return SubmitResult{}, err
	}
	receipt, err := admincmd.NewReceipt(command, ReceiptStatusAccepted, e.now())
	if err != nil {
		return SubmitResult{}, err
	}
	commandHash, err := CommandHash(command)
	if err != nil {
		return SubmitResult{}, err
	}
	envelope := CommandEnvelope{
		Command:     command,
		Receipt:     receipt,
		CommandHash: commandHash,
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		return SubmitResult{}, err
	}
	idempotencyKey := EventIdempotencyKey(command)
	if idempotencyKey != "" {
		existing, ok, err := e.log.GetByIdempotencyKey(ctx, idempotencyKey)
		if err != nil {
			return SubmitResult{}, err
		}
		if ok {
			if err := ensureSameCommand(existing, envelope); err != nil {
				return SubmitResult{}, err
			}
			return SubmitResult{Receipt: receiptForEvent(existing), Event: existing, Duplicate: true}, nil
		}
	}
	event, err := e.log.Append(ctx, eventlog.Event{
		ID:             receipt.ID,
		Stream:         e.stream,
		Type:           "accepted",
		AggregateID:    command.Target,
		IdempotencyKey: idempotencyKey,
		Payload:        payload,
		Headers: map[string]string{
			"admin_operation": command.Operation,
			"admin_scope":     command.Scope,
			"admin_actor":     command.Actor,
			"admin_target":    command.Target,
			"admin_receipt":   receipt.ID,
		},
	})
	if err != nil {
		return SubmitResult{}, err
	}
	if err := ensureSameCommand(event, envelope); err != nil {
		return SubmitResult{}, err
	}
	duplicate := event.ID != receipt.ID
	receipt = receiptForEvent(event)
	return SubmitResult{Receipt: receipt, Event: event, Duplicate: duplicate}, nil
}

func (e *Executor) Receipt(ctx context.Context, id string) (admincmd.Receipt, bool, error) {
	if e == nil || e.log == nil {
		return admincmd.Receipt{}, false, ErrEventLogRequired
	}
	event, ok, err := e.log.Get(ctx, id)
	if err != nil || !ok {
		return admincmd.Receipt{}, ok, err
	}
	return receiptForEvent(event), true, nil
}

func (e *Executor) ReceiptByIdempotencyKey(ctx context.Context, key string) (admincmd.Receipt, bool, error) {
	if e == nil || e.log == nil {
		return admincmd.Receipt{}, false, ErrEventLogRequired
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return admincmd.Receipt{}, false, nil
	}
	event, ok, err := e.log.GetByIdempotencyKey(ctx, "admincmd:"+key)
	if err != nil || !ok {
		return admincmd.Receipt{}, ok, err
	}
	return receiptForEvent(event), true, nil
}

func (e *Executor) PublishPending(ctx context.Context, handler Handler, options eventlog.PublishOptions) (eventlog.PublishStats, error) {
	if e == nil || e.log == nil {
		return eventlog.PublishStats{}, ErrEventLogRequired
	}
	if handler == nil {
		return eventlog.PublishStats{}, ErrCommandHandlerNeeded
	}
	return e.log.PublishPending(ctx, commandPublisher{handler: handler}, options)
}

func (e *Executor) MarkSucceeded(ctx context.Context, receiptID string) (admincmd.Receipt, error) {
	if e == nil || e.log == nil {
		return admincmd.Receipt{}, ErrEventLogRequired
	}
	event, err := e.log.MarkPublished(ctx, receiptID)
	if err != nil {
		return admincmd.Receipt{}, err
	}
	return receiptForEvent(event), nil
}

func (e *Executor) MarkFailed(ctx context.Context, receiptID string, message string, nextAttempt time.Time) (admincmd.Receipt, error) {
	if e == nil || e.log == nil {
		return admincmd.Receipt{}, ErrEventLogRequired
	}
	event, err := e.log.MarkFailed(ctx, receiptID, message, nextAttempt)
	if err != nil {
		return admincmd.Receipt{}, err
	}
	return receiptForEvent(event), nil
}

func EventIdempotencyKey(command admincmd.Command) string {
	key := strings.TrimSpace(command.IdempotencyKey)
	if key == "" {
		return ""
	}
	return "admincmd:" + key
}

func CommandHash(command admincmd.Command) (string, error) {
	command = admincmd.Normalize(command)
	payload, err := json.Marshal(command)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

type commandPublisher struct {
	handler Handler
}

func (p commandPublisher) Publish(ctx context.Context, event eventlog.Event) error {
	if p.handler == nil {
		return ErrCommandHandlerNeeded
	}
	envelope, err := decodeEnvelope(event)
	if err != nil {
		return err
	}
	return p.handler.ExecuteAdminCommand(ctx, envelope.Command)
}

func ensureSameCommand(event eventlog.Event, want CommandEnvelope) error {
	got, err := decodeEnvelope(event)
	if err != nil {
		return err
	}
	if got.CommandHash != "" && want.CommandHash != "" && got.CommandHash != want.CommandHash {
		return fmt.Errorf("%w: receipt=%s", ErrIdempotencyConflict, got.Receipt.ID)
	}
	if got.Receipt.ID != "" && want.Receipt.ID != "" && got.Receipt.ID != want.Receipt.ID {
		return fmt.Errorf("%w: receipt=%s", ErrIdempotencyConflict, got.Receipt.ID)
	}
	return nil
}

func decodeEnvelope(event eventlog.Event) (CommandEnvelope, error) {
	var envelope CommandEnvelope
	if len(event.Payload) == 0 {
		return envelope, ErrInvalidCommandEvent
	}
	if err := json.Unmarshal(event.Payload, &envelope); err != nil {
		return envelope, fmt.Errorf("%w: %w", ErrInvalidCommandEvent, err)
	}
	envelope.Command = admincmd.Normalize(envelope.Command)
	if envelope.Receipt.ID == "" || envelope.Command.Operation == "" {
		return envelope, ErrInvalidCommandEvent
	}
	return envelope, nil
}

func receiptForEvent(event eventlog.Event) admincmd.Receipt {
	envelope, err := decodeEnvelope(event)
	if err != nil {
		return admincmd.Receipt{ID: event.ID, Status: ReceiptStatusFailed, CreatedAt: event.CreatedAt}
	}
	receipt := envelope.Receipt
	receipt.Status = receiptStatusEvent(event.Status)
	return receipt
}

func receiptStatusEvent(status string) string {
	switch status {
	case eventlog.StatusPending:
		return ReceiptStatusAccepted
	case eventlog.StatusProcessing:
		return ReceiptStatusProcessing
	case eventlog.StatusPublished:
		return ReceiptStatusSucceeded
	case eventlog.StatusFailed:
		return ReceiptStatusFailed
	case eventlog.StatusDeadLetter:
		return ReceiptStatusDeadLetter
	default:
		return strings.TrimSpace(status)
	}
}
