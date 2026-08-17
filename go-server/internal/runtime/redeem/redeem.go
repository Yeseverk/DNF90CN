package redeem

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
	"longheng.io/server/internal/platform/eventlog"
)

var (
	ErrCodeRequired           = errors.New("redeem code is required")
	ErrAccountRequired        = errors.New("redeem account is required")
	ErrStoreRequired          = errors.New("redeem store is required")
	ErrCodeNotFound           = errors.New("redeem code not found")
	ErrCodeInactive           = errors.New("redeem code is inactive")
	ErrCodeExhausted          = errors.New("redeem code is exhausted")
	ErrAccountLimitExceeded   = errors.New("redeem account limit exceeded")
	ErrClaimConflict          = errors.New("redeem claim idempotency conflict")
	ErrInvalidRedeemCode      = errors.New("redeem code is invalid")
	ErrInvalidRedeemOperation = errors.New("redeem operation is invalid")
)

type Code struct {
	Code            string            `json:"code"`
	Campaign        string            `json:"campaign,omitempty"`
	RewardRef       string            `json:"reward_ref"`
	MaxUses         int               `json:"max_uses,omitempty"`
	PerAccountLimit int               `json:"per_account_limit,omitempty"`
	StartsAt        time.Time         `json:"starts_at,omitempty"`
	EndsAt          time.Time         `json:"ends_at,omitempty"`
	Disabled        bool              `json:"disabled,omitempty"`
	Meta            map[string]string `json:"meta,omitempty"`
}

type ClaimRequest struct {
	Code           string            `json:"code"`
	AccountID      string            `json:"account_id"`
	ShardID        string            `json:"shard_id,omitempty"`
	Source         string            `json:"source,omitempty"`
	IdempotencyKey string            `json:"idempotency_key,omitempty"`
	Meta           map[string]string `json:"meta,omitempty"`
	AdminCommand   *admincmd.Command `json:"admin_command,omitempty"`
}

type ClaimResult struct {
	Accepted       bool              `json:"accepted"`
	Duplicate      bool              `json:"duplicate,omitempty"`
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

type Store interface {
	PutCode(context.Context, Code) (Code, error)
	Claim(context.Context, ClaimRequest, time.Time) (ClaimResult, error)
	Snapshot(context.Context) (Snapshot, error)
}

type Snapshot struct {
	Codes  int            `json:"codes"`
	Claims int            `json:"claims"`
	Uses   map[string]int `json:"uses,omitempty"`
}

type Service struct {
	Store          Store
	EventLog       *eventlog.Log
	EventLogStream string
	EventLogType   string
	Now            func() time.Time
}

func (s Service) PutCode(ctx context.Context, code Code) (Code, error) {
	if err := ctxErr(ctx); err != nil {
		return Code{}, err
	}
	if s.Store == nil {
		return Code{}, ErrStoreRequired
	}
	return s.Store.PutCode(ctx, code)
}

func (s Service) Claim(ctx context.Context, request ClaimRequest) (ClaimResult, error) {
	if err := ctxErr(ctx); err != nil {
		return ClaimResult{}, err
	}
	if s.Store == nil {
		return ClaimResult{}, ErrStoreRequired
	}
	request = normClaimReq(request)
	if request.Code == "" {
		return ClaimResult{}, ErrCodeRequired
	}
	if request.AccountID == "" {
		return ClaimResult{}, ErrAccountRequired
	}
	if request.IdempotencyKey == "" {
		request.IdempotencyKey = IdempotencyKey(request.Code, request.AccountID)
	}
	if request.AdminCommand != nil {
		// 运营补发礼包码也走危险命令校验，确保有 reason、确认信息和幂等键。
		command := *request.AdminCommand
		command.Operation = firstNonEmpty(command.Operation, "redeem.repair")
		command.Scope = firstNonEmpty(command.Scope, "redeem")
		command.Target = firstNonEmpty(command.Target, request.AccountID)
		command.Params = mergeCommandParams(command.Params, request)
		if err := admincmd.Validate(command, admincmd.DangerousPolicy()); err != nil {
			return ClaimResult{}, err
		}
		receipt, err := admincmd.NewReceipt(command, "accepted", s.now())
		if err != nil {
			return ClaimResult{}, err
		}
		request.AdminCommand = &command
		request.Meta = mergeStringMap(request.Meta, map[string]string{"admin_receipt_id": receipt.ID})
	}
	if result, ok, err := s.lookupClaimEvent(ctx, request); err != nil || ok {
		return result, err
	}
	result, err := s.Store.Claim(ctx, request, s.now())
	if err != nil {
		return result, err
	}
	if err := s.appendClaimEvent(ctx, result); err != nil {
		return result, errors.Join(err, runClaimRollback(ctx, func(ctx context.Context) error {
			return s.rollbackClaim(ctx, result)
		}))
	}
	return result, nil
}

type claimRollbackStore interface {
	RollbackClaim(context.Context, ClaimResult) error
}

func (s Service) rollbackClaim(ctx context.Context, result ClaimResult) error {
	if result.Duplicate || s.Store == nil {
		return nil
	}
	store, ok := s.Store.(claimRollbackStore)
	if !ok {
		return nil
	}
	return store.RollbackClaim(ctx, result)
}

func (s Service) Snapshot(ctx context.Context) (Snapshot, error) {
	if err := ctxErr(ctx); err != nil {
		return Snapshot{}, err
	}
	if s.Store == nil {
		return Snapshot{}, ErrStoreRequired
	}
	return s.Store.Snapshot(ctx)
}

func (s Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

type MemoryStore struct {
	mu              sync.Mutex
	codes           map[string]Code
	claimsByKey     map[string]ClaimResult
	claimsByAccount map[string]int
	uses            map[string]int
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		codes:           make(map[string]Code),
		claimsByKey:     make(map[string]ClaimResult),
		claimsByAccount: make(map[string]int),
		uses:            make(map[string]int),
	}
}

func (s *MemoryStore) PutCode(ctx context.Context, code Code) (Code, error) {
	if err := ctxErr(ctx); err != nil {
		return Code{}, err
	}
	if s == nil {
		return Code{}, ErrStoreRequired
	}
	code = normalizeCode(code)
	if code.Code == "" {
		return Code{}, ErrCodeRequired
	}
	if code.RewardRef == "" {
		return Code{}, fmt.Errorf("%w: reward_ref is required", ErrInvalidRedeemCode)
	}
	if code.MaxUses < 0 || code.PerAccountLimit < 0 {
		return Code{}, fmt.Errorf("%w: limits cannot be negative", ErrInvalidRedeemCode)
	}
	if code.PerAccountLimit == 0 {
		code.PerAccountLimit = 1
	}
	if !code.StartsAt.IsZero() && !code.EndsAt.IsZero() && code.EndsAt.Before(code.StartsAt) {
		return Code{}, fmt.Errorf("%w: ends_at is before starts_at", ErrInvalidRedeemCode)
	}
	s.mu.Lock()
	s.codes[code.Code] = cloneCode(code)
	s.mu.Unlock()
	return cloneCode(code), nil
}

func (s *MemoryStore) Claim(ctx context.Context, request ClaimRequest, now time.Time) (ClaimResult, error) {
	if err := ctxErr(ctx); err != nil {
		return ClaimResult{}, err
	}
	if s == nil {
		return ClaimResult{}, ErrStoreRequired
	}
	request = normClaimReq(request)
	if request.Code == "" {
		return ClaimResult{}, ErrCodeRequired
	}
	if request.AccountID == "" {
		return ClaimResult{}, ErrAccountRequired
	}
	if request.IdempotencyKey == "" {
		request.IdempotencyKey = IdempotencyKey(request.Code, request.AccountID)
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	lookupKey := claimLookupKey(request)
	if existing, ok := s.claimsByKey[lookupKey]; ok {
		if claimReplayConflict(existing, request) {
			return ClaimResult{}, ErrClaimConflict
		}
		// 礼包码领取按 code+account+key 重放，防止客户端超时重试重复扣次数。
		existing.Duplicate = true
		return cloneClaimResult(existing), nil
	}
	code, ok := s.codes[request.Code]
	if !ok {
		return ClaimResult{}, ErrCodeNotFound
	}
	if !codeActive(code, now) {
		return ClaimResult{}, ErrCodeInactive
	}
	if code.MaxUses > 0 && s.uses[code.Code] >= code.MaxUses {
		return ClaimResult{}, ErrCodeExhausted
	}
	accountKey := code.Code + "\x00" + request.AccountID
	if s.claimsByAccount[accountKey] >= code.PerAccountLimit {
		return ClaimResult{}, ErrAccountLimitExceeded
	}

	// 次数扣减和领取记录在同一把锁内完成；生产存储应使用同等原子写能力。
	s.uses[code.Code]++
	s.claimsByAccount[accountKey]++
	remaining := -1
	if code.MaxUses > 0 {
		remaining = code.MaxUses - s.uses[code.Code]
	}
	result := ClaimResult{
		Accepted:       true,
		ClaimID:        ClaimID(request.Code, request.AccountID, request.IdempotencyKey),
		Code:           code.Code,
		AccountID:      request.AccountID,
		ShardID:        request.ShardID,
		Source:         request.Source,
		Campaign:       code.Campaign,
		RewardRef:      code.RewardRef,
		IdempotencyKey: request.IdempotencyKey,
		Uses:           s.uses[code.Code],
		Remaining:      remaining,
		AdminReceiptID: request.Meta["admin_receipt_id"],
		Meta:           mergeStringMap(code.Meta, request.Meta),
		ClaimedAt:      now,
	}
	s.claimsByKey[lookupKey] = cloneClaimResult(result)
	return cloneClaimResult(result), nil
}

func (s *MemoryStore) RollbackClaim(ctx context.Context, result ClaimResult) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if s == nil {
		return ErrStoreRequired
	}
	if result.Code == "" || result.AccountID == "" || result.IdempotencyKey == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	request := ClaimRequest{Code: result.Code, AccountID: result.AccountID, IdempotencyKey: result.IdempotencyKey}
	lookupKey := claimLookupKey(request)
	existing, ok := s.claimsByKey[lookupKey]
	if !ok || existing.ClaimID != result.ClaimID {
		return nil
	}
	delete(s.claimsByKey, lookupKey)
	accountKey := result.Code + "\x00" + result.AccountID
	if s.claimsByAccount[accountKey] <= 1 {
		delete(s.claimsByAccount, accountKey)
	} else {
		s.claimsByAccount[accountKey]--
	}
	if s.uses[result.Code] <= 1 {
		delete(s.uses, result.Code)
	} else {
		s.uses[result.Code]--
	}
	return nil
}

func (s *MemoryStore) Snapshot(ctx context.Context) (Snapshot, error) {
	if err := ctxErr(ctx); err != nil {
		return Snapshot{}, err
	}
	if s == nil {
		return Snapshot{}, ErrStoreRequired
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return Snapshot{
		Codes:  len(s.codes),
		Claims: len(s.claimsByKey),
		Uses:   cloneIntMap(s.uses),
	}, nil
}

func normalizeCode(code Code) Code {
	code.Code = strings.TrimSpace(code.Code)
	code.Campaign = strings.TrimSpace(code.Campaign)
	code.RewardRef = strings.TrimSpace(code.RewardRef)
	code.StartsAt = normalizeTime(code.StartsAt)
	code.EndsAt = normalizeTime(code.EndsAt)
	code.Meta = normalizeStringMap(code.Meta)
	return code
}

func normClaimReq(request ClaimRequest) ClaimRequest {
	request.Code = strings.TrimSpace(request.Code)
	request.AccountID = strings.TrimSpace(request.AccountID)
	request.ShardID = strings.TrimSpace(request.ShardID)
	request.Source = strings.TrimSpace(request.Source)
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	request.Meta = normalizeStringMap(request.Meta)
	return request
}

func codeActive(code Code, now time.Time) bool {
	if code.Disabled {
		return false
	}
	if !code.StartsAt.IsZero() && now.Before(code.StartsAt) {
		return false
	}
	if !code.EndsAt.IsZero() && now.After(code.EndsAt) {
		return false
	}
	return true
}

func IdempotencyKey(code, accountID string) string {
	code = strings.TrimSpace(code)
	accountID = strings.TrimSpace(accountID)
	if code == "" || accountID == "" {
		return ""
	}
	return strings.Join([]string{"redeem", "v1", encodeEventKeyPart(code), encodeEventKeyPart(accountID)}, ":")
}

func ClaimID(code, accountID, key string) string {
	seed := strings.Join([]string{strings.TrimSpace(code), strings.TrimSpace(accountID), strings.TrimSpace(key)}, "\x00")
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:16])
}

func claimLookupKey(request ClaimRequest) string {
	return strings.TrimSpace(request.Code) + "\x00" + strings.TrimSpace(request.AccountID) + "\x00" + strings.TrimSpace(request.IdempotencyKey)
}

func claimReplayConflict(existing ClaimResult, request ClaimRequest) bool {
	if request.Code != "" && existing.Code != request.Code {
		return true
	}
	if request.AccountID != "" && existing.AccountID != request.AccountID {
		return true
	}
	if request.ShardID != "" && existing.ShardID != request.ShardID {
		return true
	}
	if request.Source != "" && existing.Source != request.Source {
		return true
	}
	if len(request.Meta) > 0 {
		for key, value := range request.Meta {
			if key == "admin_receipt_id" {
				continue
			}
			if existing.Meta[key] != value {
				return true
			}
		}
	}
	return false
}

func mergeCommandParams(params map[string]any, request ClaimRequest) map[string]any {
	out := make(map[string]any, len(params)+4)
	for key, value := range params {
		out[key] = value
	}
	out["code"] = request.Code
	out["account_id"] = request.AccountID
	out["shard_id"] = request.ShardID
	out["idempotency_key"] = request.IdempotencyKey
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func normalizeTime(value time.Time) time.Time {
	if value.IsZero() {
		return time.Time{}
	}
	return value.UTC()
}

func normalizeStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = strings.TrimSpace(value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func mergeStringMap(base map[string]string, overlays ...map[string]string) map[string]string {
	out := cloneStringMap(base)
	for _, overlay := range overlays {
		for key, value := range overlay {
			if out == nil {
				out = make(map[string]string)
			}
			out[key] = value
		}
	}
	return out
}

func cloneCode(code Code) Code {
	code.Meta = cloneStringMap(code.Meta)
	return code
}

func cloneClaimResult(result ClaimResult) ClaimResult {
	result.Meta = cloneStringMap(result.Meta)
	return result
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func cloneIntMap(values map[string]int) map[string]int {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]int, len(values))
	for key, value := range values {
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
