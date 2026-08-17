package redeem

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"longheng.io/server/internal/platform/eventlog"
)

const (
	defaultClaimEvStream = "redeem.claim"
	defaultClaimEvType   = "redeem.claimed"
	claimRollbackTimeout = 5 * time.Second
)

type claimEventPayload struct {
	ClaimID        string            `json:"claim_id"`
	Code           string            `json:"code"`
	AccountID      string            `json:"account_id"`
	ShardID        string            `json:"shard_id,omitempty"`
	Source         string            `json:"source,omitempty"`
	Campaign       string            `json:"campaign,omitempty"`
	RewardRef      string            `json:"reward_ref"`
	IdempotencyKey string            `json:"idempotency_key"`
	Uses           int               `json:"uses"`
	Remaining      int               `json:"remaining"`
	AdminReceiptID string            `json:"admin_receipt_id,omitempty"`
	Meta           map[string]string `json:"meta,omitempty"`
	ClaimedAt      time.Time         `json:"claimed_at"`
}

func (s Service) lookupClaimEvent(ctx context.Context, request ClaimRequest) (ClaimResult, bool, error) {
	if s.EventLog == nil {
		return ClaimResult{}, false, nil
	}
	event, ok, err := s.EventLog.GetByIdempotencyKey(ctx, claimEventKey(request.Code, request.AccountID, request.IdempotencyKey))
	if err != nil || !ok {
		return ClaimResult{}, false, err
	}
	result, err := claimResultFromEvent(event)
	if err != nil {
		return ClaimResult{}, false, err
	}
	if claimReplayConflict(result, request) {
		return ClaimResult{}, false, ErrClaimConflict
	}
	result.Duplicate = true
	return result, true, nil
}

func (s Service) appendClaimEvent(ctx context.Context, result ClaimResult) error {
	if s.EventLog == nil {
		return nil
	}
	payload, err := json.Marshal(claimPayloadResult(result))
	if err != nil {
		return err
	}
	_, err = s.EventLog.Append(ctx, eventlog.Event{
		ID:             claimEventID(result.Code, result.AccountID, result.IdempotencyKey),
		Stream:         s.claimEventStream(),
		Type:           s.claimEventType(),
		AggregateID:    result.AccountID,
		IdempotencyKey: claimEventKey(result.Code, result.AccountID, result.IdempotencyKey),
		Payload:        payload,
		Headers: map[string]string{
			"code":             result.Code,
			"account_id":       result.AccountID,
			"campaign":         result.Campaign,
			"reward_ref":       result.RewardRef,
			"remaining":        strconv.Itoa(result.Remaining),
			"admin_receipt_id": result.AdminReceiptID,
		},
	})
	if errors.Is(err, eventlog.ErrIdempotencyConflict) {
		return ErrClaimConflict
	}
	return err
}

func (s Service) claimEventStream() string {
	if stream := strings.TrimSpace(s.EventLogStream); stream != "" {
		return stream
	}
	return defaultClaimEvStream
}

func (s Service) claimEventType() string {
	if typ := strings.TrimSpace(s.EventLogType); typ != "" {
		return typ
	}
	return defaultClaimEvType
}

func runClaimRollback(parent context.Context, rollback func(context.Context) error) error {
	// outbox 写失败可能来自请求超时；回滚不继承取消，但保留 trace/value 方便排障。
	base := context.Background()
	if parent != nil {
		base = context.WithoutCancel(parent)
	}
	ctx, cancel := context.WithTimeout(base, claimRollbackTimeout)
	defer cancel()
	return rollback(ctx)
}

func claimPayloadResult(result ClaimResult) claimEventPayload {
	return claimEventPayload{
		ClaimID:        result.ClaimID,
		Code:           result.Code,
		AccountID:      result.AccountID,
		ShardID:        result.ShardID,
		Source:         result.Source,
		Campaign:       result.Campaign,
		RewardRef:      result.RewardRef,
		IdempotencyKey: result.IdempotencyKey,
		Uses:           result.Uses,
		Remaining:      result.Remaining,
		AdminReceiptID: result.AdminReceiptID,
		Meta:           cloneStringMap(result.Meta),
		ClaimedAt:      result.ClaimedAt,
	}
}

func claimResultFromEvent(event eventlog.Event) (ClaimResult, error) {
	var payload claimEventPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return ClaimResult{}, err
	}
	return ClaimResult{
		Accepted:       true,
		ClaimID:        payload.ClaimID,
		Code:           payload.Code,
		AccountID:      payload.AccountID,
		ShardID:        payload.ShardID,
		Source:         payload.Source,
		Campaign:       payload.Campaign,
		RewardRef:      payload.RewardRef,
		IdempotencyKey: firstNonEmpty(payload.IdempotencyKey, event.IdempotencyKey),
		Uses:           payload.Uses,
		Remaining:      payload.Remaining,
		AdminReceiptID: payload.AdminReceiptID,
		Meta:           cloneStringMap(payload.Meta),
		ClaimedAt:      payload.ClaimedAt,
	}, nil
}

func claimEventKey(code, accountID, key string) string {
	code = strings.TrimSpace(code)
	accountID = strings.TrimSpace(accountID)
	key = strings.TrimSpace(key)
	if code == "" || accountID == "" || key == "" {
		return ""
	}
	return strings.Join([]string{"redeem", "claim", "v1", encodeEventKeyPart(code), encodeEventKeyPart(accountID), encodeEventKeyPart(key)}, ":")
}

func encodeEventKeyPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "_"
	}
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func claimEventID(code, accountID, key string) string {
	sum := sha256.Sum256([]byte(claimEventKey(code, accountID, key)))
	return "redeem-claim-" + hex.EncodeToString(sum[:8])
}
