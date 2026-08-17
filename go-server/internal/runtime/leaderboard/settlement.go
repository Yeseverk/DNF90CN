package leaderboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalidSettlement = fmt.Errorf("%w: settlement is invalid", ErrInvalidSubmission)
	ErrNilRewardOutbox   = errors.New("leaderboard reward outbox is required")
)

const RewardOutboxPending = "pending"

type RewardRule struct {
	MinRank int             `json:"min_rank"`
	MaxRank int             `json:"max_rank"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type RewardOptions struct {
	SettlementID string       `json:"settlement_id"`
	Rules        []RewardRule `json:"rules"`
}

type RewardGrant struct {
	SettlementID   string          `json:"settlement_id"`
	LeaderboardID  string          `json:"leaderboard_id"`
	OwnerID        string          `json:"owner_id"`
	Rank           int             `json:"rank"`
	Score          int64           `json:"score"`
	Subscore       int64           `json:"subscore,omitempty"`
	IdempotencyKey string          `json:"idempotency_key"`
	Payload        json.RawMessage `json:"payload,omitempty"`
}

type RewardOutboxEvent struct {
	ID        string      `json:"id"`
	Grant     RewardGrant `json:"grant"`
	Status    string      `json:"status"`
	Attempts  int         `json:"attempts,omitempty"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
}

type RewardOutbox interface {
	EnqueueRewardGrants(context.Context, []RewardGrant) ([]RewardOutboxEvent, error)
	PendingRewardGrants(context.Context, int) ([]RewardOutboxEvent, error)
}

type MemoryRewardOutbox struct {
	mu     sync.Mutex
	now    func() time.Time
	order  []string
	events map[string]RewardOutboxEvent
}

type SeasonSettlementOptions struct {
	SettlementID string       `json:"settlement_id"`
	RewardRules  []RewardRule `json:"reward_rules,omitempty"`
	RewardOutbox RewardOutbox `json:"-"`
	CaptureLimit int          `json:"capture_limit,omitempty"`
	Reason       string       `json:"reason,omitempty"`
	OperatorID   string       `json:"operator_id,omitempty"`
	RequestID    string       `json:"request_id,omitempty"`
}

type SeasonSettlement struct {
	SettlementID  string              `json:"settlement_id"`
	LeaderboardID string              `json:"leaderboard_id"`
	Capture       Capture             `json:"capture"`
	Grants        []RewardGrant       `json:"grants"`
	OutboxEvents  []RewardOutboxEvent `json:"outbox_events,omitempty"`
	Reset         Definition          `json:"reset"`
}

func NewMemoryRewardOutbox(now func() time.Time) *MemoryRewardOutbox {
	if now == nil {
		now = time.Now
	}
	return &MemoryRewardOutbox{
		now:    now,
		events: make(map[string]RewardOutboxEvent),
	}
}

func (o *MemoryRewardOutbox) EnqueueRewardGrants(ctx context.Context, grants []RewardGrant) ([]RewardOutboxEvent, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if o == nil {
		return nil, ErrNilRewardOutbox
	}
	o.mu.Lock()
	defer o.mu.Unlock()

	// 内存 outbox 以 grant.IdempotencyKey 去重；同 key 不重复追加奖励事件，方便结算任务安全重跑。
	events := make([]RewardOutboxEvent, 0, len(grants))
	for _, grant := range grants {
		normalized := cloneRewardGrant(grant)
		normalized.IdempotencyKey = strings.TrimSpace(normalized.IdempotencyKey)
		if normalized.IdempotencyKey == "" {
			return nil, ErrInvalidSettlement
		}
		if existing, ok := o.events[normalized.IdempotencyKey]; ok {
			events = append(events, cloneRewardOutbox(existing))
			continue
		}
		now := o.now().UTC()
		event := RewardOutboxEvent{
			ID:        normalized.IdempotencyKey,
			Grant:     normalized,
			Status:    RewardOutboxPending,
			CreatedAt: now,
			UpdatedAt: now,
		}
		o.order = append(o.order, event.ID)
		o.events[event.ID] = event
		events = append(events, cloneRewardOutbox(event))
	}
	return events, nil
}

func (o *MemoryRewardOutbox) PendingRewardGrants(ctx context.Context, limit int) ([]RewardOutboxEvent, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if o == nil {
		return nil, ErrNilRewardOutbox
	}
	if limit <= 0 || limit > maxListLimit {
		limit = maxListLimit
	}
	o.mu.Lock()
	defer o.mu.Unlock()

	events := make([]RewardOutboxEvent, 0, limit)
	for _, id := range o.order {
		event := o.events[id]
		if event.Status != RewardOutboxPending {
			continue
		}
		events = append(events, cloneRewardOutbox(event))
		if len(events) >= limit {
			break
		}
	}
	return events, nil
}

func CloseSeason(ctx context.Context, runtime Runtime, leaderboardID string, options SeasonSettlementOptions) (SeasonSettlement, error) {
	if err := ctxErr(ctx); err != nil {
		return SeasonSettlement{}, err
	}
	if runtime == nil {
		return SeasonSettlement{}, ErrLeaderboardNotFound
	}
	settlementID := strings.TrimSpace(options.SettlementID)
	if settlementID == "" {
		return SeasonSettlement{}, ErrInvalidSettlement
	}
	// 结算顺序固定为：截榜快照 -> 生成奖励幂等键 -> 写奖励 outbox -> 可选清榜，避免清榜后丢奖励证据。
	capture, err := runtime.Capture(ctx, leaderboardID, CaptureOptions{
		Limit:      options.CaptureLimit,
		Reason:     options.Reason,
		OperatorID: options.OperatorID,
		RequestID:  options.RequestID,
		Metadata:   map[string]string{"settlement_id": settlementID},
	})
	if err != nil {
		return SeasonSettlement{}, err
	}
	grants, err := BuildRewardGrants(capture, RewardOptions{SettlementID: settlementID, Rules: options.RewardRules})
	if err != nil {
		return SeasonSettlement{}, err
	}
	var events []RewardOutboxEvent
	if len(grants) > 0 {
		if options.RewardOutbox == nil {
			return SeasonSettlement{}, ErrNilRewardOutbox
		}
		events, err = options.RewardOutbox.EnqueueRewardGrants(ctx, grants)
		if err != nil {
			return SeasonSettlement{}, err
		}
	}
	reset, err := runtime.Reset(ctx, leaderboardID)
	if err != nil {
		return SeasonSettlement{}, err
	}
	return SeasonSettlement{
		SettlementID:  settlementID,
		LeaderboardID: strings.TrimSpace(leaderboardID),
		Capture:       cloneCapture(capture),
		Grants:        cloneRewardGrants(grants),
		OutboxEvents:  cloneRewardEvents(events),
		Reset:         cloneDefinition(reset),
	}, nil
}

func BuildRewardGrants(capture Capture, options RewardOptions) ([]RewardGrant, error) {
	settlementID := strings.TrimSpace(options.SettlementID)
	if settlementID == "" || strings.TrimSpace(capture.LeaderboardID) == "" {
		return nil, ErrInvalidSettlement
	}
	rules, err := normalizeRewardRules(options.Rules)
	if err != nil {
		return nil, err
	}
	if len(rules) == 0 || len(capture.Records) == 0 {
		return []RewardGrant{}, nil
	}
	// 奖励幂等键由 settlement/leaderboard/player/rank 组成，同一次结算重放不会重复发奖。
	grants := make([]RewardGrant, 0, len(capture.Records))
	for _, record := range capture.Records {
		if strings.TrimSpace(record.OwnerID) == "" || record.Rank <= 0 {
			continue
		}
		rule, ok := rewardRuleForRank(rules, record.Rank)
		if !ok {
			continue
		}
		grants = append(grants, RewardGrant{
			SettlementID:   settlementID,
			LeaderboardID:  strings.TrimSpace(capture.LeaderboardID),
			OwnerID:        strings.TrimSpace(record.OwnerID),
			Rank:           record.Rank,
			Score:          record.Score,
			Subscore:       record.Subscore,
			IdempotencyKey: rewardGrantKey(settlementID, capture.LeaderboardID, record.OwnerID, record.Rank),
			Payload:        cloneRewardPayload(rule.Payload),
		})
	}
	return grants, nil
}

func normalizeRewardRules(rules []RewardRule) ([]RewardRule, error) {
	out := make([]RewardRule, 0, len(rules))
	for _, rule := range rules {
		if rule.MinRank <= 0 {
			return nil, ErrInvalidSettlement
		}
		if rule.MaxRank <= 0 {
			rule.MaxRank = rule.MinRank
		}
		if rule.MaxRank < rule.MinRank {
			return nil, ErrInvalidSettlement
		}
		if len(rule.Payload) > 0 && !json.Valid(rule.Payload) {
			return nil, ErrInvalidSettlement
		}
		rule.Payload = cloneRewardPayload(rule.Payload)
		out = append(out, rule)
	}
	return out, nil
}

func rewardRuleForRank(rules []RewardRule, rank int) (RewardRule, bool) {
	for _, rule := range rules {
		if rank >= rule.MinRank && rank <= rule.MaxRank {
			return rule, true
		}
	}
	return RewardRule{}, false
}

func rewardGrantKey(settlementID, leaderboardID, ownerID string, rank int) string {
	return fmt.Sprintf("leaderboard:settlement:v1:%s:%s:%s:%d",
		encodeKeyPart(settlementID),
		encodeKeyPart(leaderboardID),
		encodeKeyPart(ownerID),
		rank,
	)
}

func cloneRewardPayload(payload json.RawMessage) json.RawMessage {
	if payload == nil {
		return nil
	}
	return append(json.RawMessage(nil), payload...)
}

func cloneRewardGrant(grant RewardGrant) RewardGrant {
	grant.Payload = cloneRewardPayload(grant.Payload)
	return grant
}

func cloneRewardGrants(grants []RewardGrant) []RewardGrant {
	out := make([]RewardGrant, len(grants))
	for i, grant := range grants {
		out[i] = cloneRewardGrant(grant)
	}
	return out
}

func cloneRewardOutbox(event RewardOutboxEvent) RewardOutboxEvent {
	event.Grant = cloneRewardGrant(event.Grant)
	return event
}

func cloneRewardEvents(events []RewardOutboxEvent) []RewardOutboxEvent {
	out := make([]RewardOutboxEvent, len(events))
	for i, event := range events {
		out[i] = cloneRewardOutbox(event)
	}
	return out
}
