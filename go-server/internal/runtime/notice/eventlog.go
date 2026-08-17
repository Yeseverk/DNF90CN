package notice

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
	defaultPublishStream = "notice.publish"
	defaultPubEventType  = "notice.published"
)

type publishEventPayload struct {
	Notice         Notice            `json:"notice"`
	Deliveries     []Delivery        `json:"deliveries,omitempty"`
	IdempotencyKey string            `json:"idempotency_key"`
	AdminReceiptID string            `json:"admin_receipt_id,omitempty"`
	Meta           map[string]string `json:"meta,omitempty"`
	PublishedAt    time.Time         `json:"published_at"`
}

func (s Service) lookupPublishEvent(ctx context.Context, request PublishRequest) (PublishResult, bool, error) {
	if s.EventLog == nil {
		return PublishResult{}, false, nil
	}
	event, ok, err := s.EventLog.GetByIdempotencyKey(ctx, publishEventKey(request.Notice.ID, request.IdempotencyKey))
	if err != nil || !ok {
		return PublishResult{}, false, err
	}
	result, err := publishResultEvent(event)
	if err != nil {
		return PublishResult{}, false, err
	}
	if publishReplayClash(result, request) {
		return PublishResult{}, false, ErrIdempotencyConflict
	}
	result.Duplicate = true
	return result, true, nil
}

func (s Service) appendPublishEvent(ctx context.Context, result PublishResult) error {
	if s.EventLog == nil {
		return nil
	}
	payload, err := json.Marshal(payloadFromResult(result))
	if err != nil {
		return err
	}
	_, err = s.EventLog.Append(ctx, eventlog.Event{
		ID:             publishEventID(result.Notice.ID, result.IdempotencyKey),
		Stream:         s.publishEventStream(),
		Type:           s.publishEventType(),
		AggregateID:    result.Notice.ID,
		IdempotencyKey: publishEventKey(result.Notice.ID, result.IdempotencyKey),
		Payload:        payload,
		Headers: map[string]string{
			"notice_id":        result.Notice.ID,
			"kind":             result.Notice.Kind,
			"recipient_count":  strconv.Itoa(len(result.Deliveries)),
			"admin_receipt_id": result.AdminReceiptID,
		},
	})
	if errors.Is(err, eventlog.ErrIdempotencyConflict) {
		return ErrIdempotencyConflict
	}
	return err
}

func (s Service) publishEventStream() string {
	if stream := strings.TrimSpace(s.EventLogStream); stream != "" {
		return stream
	}
	return defaultPublishStream
}

func (s Service) publishEventType() string {
	if typ := strings.TrimSpace(s.EventLogType); typ != "" {
		return typ
	}
	return defaultPubEventType
}

func payloadFromResult(result PublishResult) publishEventPayload {
	return publishEventPayload{
		Notice:         cloneNotice(result.Notice),
		Deliveries:     cloneDeliveries(result.Deliveries),
		IdempotencyKey: result.IdempotencyKey,
		AdminReceiptID: result.AdminReceiptID,
		Meta:           cloneStringMap(result.Meta),
		PublishedAt:    result.PublishedAt,
	}
}

func publishResultEvent(event eventlog.Event) (PublishResult, error) {
	var payload publishEventPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return PublishResult{}, err
	}
	return PublishResult{
		Accepted:       true,
		Notice:         cloneNotice(payload.Notice),
		Deliveries:     cloneDeliveries(payload.Deliveries),
		IdempotencyKey: firstNonEmpty(payload.IdempotencyKey, event.IdempotencyKey),
		AdminReceiptID: payload.AdminReceiptID,
		Meta:           cloneStringMap(payload.Meta),
		PublishedAt:    payload.PublishedAt,
	}, nil
}

func publishEventKey(noticeID, key string) string {
	noticeID = strings.TrimSpace(noticeID)
	key = strings.TrimSpace(key)
	if noticeID == "" || key == "" {
		return ""
	}
	return strings.Join([]string{"notice", "publish", "v1", encodeEventKeyPart(noticeID), encodeEventKeyPart(key)}, ":")
}

func encodeEventKeyPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "_"
	}
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func publishEventID(noticeID, key string) string {
	sum := sha256.Sum256([]byte(publishEventKey(noticeID, key)))
	return "notice-publish-" + hex.EncodeToString(sum[:8])
}
