package idempotency

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalidRequest      = errors.New("idempotency request is invalid")
	ErrRequestConflict     = errors.New("idempotency request fingerprint conflicts with existing request")
	ErrReservationLost     = errors.New("idempotency reservation ownership was lost")
	ErrStoreRequired       = errors.New("idempotency store is required")
	ErrResultStoreRequired = errors.New("idempotency result store is required")
)

type Status string

const (
	StatusAccepted  Status = "accepted"
	StatusDuplicate Status = "duplicate"
	StatusInFlight  Status = "in_flight"
	StatusReplay    Status = "replay"
	statusPending   Status = "pending"
)

type Store interface {
	Check(context.Context, Request) (Decision, error)
	Snapshot() map[string]any
}

type transactionalStore interface {
	Begin(context.Context, Request) (Decision, error)
	Commit(context.Context, Request, Decision) error
	Abort(context.Context, Request, Decision) error
}

// resultStore 把幂等状态与可重放结果提交到同一个权威后端。
// 它只用于 handler 已成功、不能再安全重跑的路径，避免独立响应缓存失败后留下不可恢复的 committed 状态。
type resultStore interface {
	CommitResult(context.Context, Request, Decision, []byte) error
	LookupResult(context.Context, Decision) ([]byte, bool, error)
}

type Options struct {
	TTL   time.Duration
	Now   func() time.Time
	Store Store
	Kind  string
}

type Request struct {
	Scope       string
	Subject     string
	Session     string
	Key         string
	Fingerprint string
	Sequence    uint64
	Now         time.Time
	// reservationToken 由 Guard.Begin 在进入后端前生成，不属于外部请求契约。
	reservationToken string
}

type Decision struct {
	Status      Status `json:"status"`
	Key         string `json:"key,omitempty"`
	Sequence    uint64 `json:"sequence,omitempty"`
	fingerprint string
	ownerToken  string
	expiresAt   time.Time
}

type Reservation struct {
	guard    *Guard
	request  Request
	decision Decision
	active   bool
}

type Guard struct {
	kind  string
	store Store
	stats guardStats
}

type guardStats struct {
	mu           sync.Mutex
	accepted     int64
	duplicate    int64
	inFlight     int64
	replay       int64
	backendError int64
	latencyNanos int64
}

func New(options Options) *Guard {
	options = normalizeOptions(options)
	store := options.Store
	if store == nil {
		store = newMemoryStore(options.TTL, options.Now)
	}
	kind := strings.TrimSpace(options.Kind)
	if kind == "" {
		kind = "memory"
	}
	return &Guard{kind: kind, store: store}
}

func (g *Guard) Check(ctx context.Context, request Request) (Decision, error) {
	if err := ctxErr(ctx); err != nil {
		return Decision{}, err
	}
	reservation, decision, err := g.Begin(ctx, request)
	if err != nil {
		return Decision{}, err
	}
	if decision.Status == StatusAccepted {
		if err := reservation.Commit(ctx); err != nil {
			return Decision{}, err
		}
	}
	return decision, nil
}

func (g *Guard) Begin(ctx context.Context, request Request) (Reservation, Decision, error) {
	if err := ctxErr(ctx); err != nil {
		return Reservation{}, Decision{}, err
	}
	if g == nil || g.store == nil {
		return Reservation{}, Decision{Status: StatusAccepted}, nil
	}
	item, err := normalizeRequest(request)
	if err != nil {
		return Reservation{}, Decision{}, err
	}
	if item.Key == "" && item.Sequence == 0 {
		// 没有 key 和 sequence 的旧协议请求无法提供幂等语义，只能放行。
		// 新协议入口应尽量在网关侧派生 IdempotencyKey。
		decision := Decision{Status: StatusAccepted}
		g.stats.record(decision, nil, 0)
		return Reservation{guard: g, request: item, decision: decision}, decision, nil
	}
	ownerToken, err := newReservationToken()
	if err != nil {
		return Reservation{}, Decision{}, err
	}
	item.reservationToken = ownerToken
	start := time.Now()
	var decision Decision
	// 支持三段式后端时，Begin 只做占位；业务成功后必须 Commit，失败必须 Abort。
	// 非事务后端退化为一次 Check，用于内存或纯缓存场景。
	if store, ok := g.store.(transactionalStore); ok {
		decision, err = store.Begin(ctx, item)
	} else {
		decision, err = g.store.Check(ctx, item)
	}
	g.stats.record(decision, err, time.Since(start))
	if err != nil {
		return Reservation{}, Decision{}, err
	}
	if decision.Status == StatusAccepted {
		decision.ownerToken = ownerToken
	}
	reservation := Reservation{
		guard:    g,
		request:  item,
		decision: decision,
		active:   decision.Status == StatusAccepted,
	}
	return reservation, decision, nil
}

func (r *Reservation) Commit(ctx context.Context) error {
	if r == nil || !r.active || r.guard == nil || r.guard.store == nil {
		return nil
	}
	store, ok := r.guard.store.(transactionalStore)
	if !ok {
		r.active = false
		return nil
	}
	// Commit 只有在业务 handler 成功后调用，负责把 pending 占位转成可重放结果。
	err := store.Commit(ctx, r.request, r.decision)
	if err != nil {
		r.guard.stats.record(r.decision, err, 0)
	}
	if err == nil {
		r.active = false
	}
	return err
}

// CommitResult 原子提交幂等状态与可重放结果。
// 默认 memory/Redis/MySQL 后端都在自身锁、Lua 或 SQL 事务内实现，不跨后端拼接伪事务。
func (r *Reservation) CommitResult(ctx context.Context, payload []byte) error {
	if r == nil || !r.active || r.guard == nil || r.guard.store == nil {
		return nil
	}
	store, ok := r.guard.store.(resultStore)
	if !ok {
		return ErrResultStoreRequired
	}
	err := store.CommitResult(ctx, r.request, r.decision, append([]byte(nil), payload...))
	if err != nil {
		r.guard.stats.record(r.decision, err, 0)
		return err
	}
	r.active = false
	return nil
}

func (r *Reservation) Abort(ctx context.Context) error {
	if r == nil || !r.active || r.guard == nil || r.guard.store == nil {
		return nil
	}
	store, ok := r.guard.store.(transactionalStore)
	if !ok {
		r.active = false
		return nil
	}
	// Abort 负责清掉 pending 占位；否则客户端重试会被误认为仍在处理。
	err := store.Abort(ctx, r.request, r.decision)
	if err != nil {
		r.guard.stats.record(r.decision, err, 0)
	}
	if err == nil {
		r.active = false
	}
	return err
}

// LookupResult 读取和 committed 状态同后端、同 TTL 的可重放结果。
// 自定义旧 Store 未实现结果契约时按未命中处理，避免破坏只使用 Check 的兼容调用方。
func (g *Guard) LookupResult(ctx context.Context, decision Decision) ([]byte, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, false, err
	}
	if g == nil || g.store == nil || strings.TrimSpace(decision.Key) == "" {
		return nil, false, nil
	}
	store, ok := g.store.(resultStore)
	if !ok {
		return nil, false, nil
	}
	payload, found, err := store.LookupResult(ctx, decision)
	if err != nil || !found {
		return nil, found, err
	}
	return append([]byte(nil), payload...), true, nil
}

func (g *Guard) Snapshot() map[string]any {
	if g == nil {
		return map[string]any{}
	}
	out := map[string]any{
		"kind": g.kind,
	}
	if g.store != nil {
		for key, value := range g.store.Snapshot() {
			out[key] = value
		}
	}
	out["metrics"] = g.stats.snapshot()
	return out
}

func (g *Guard) Close(ctx context.Context) error {
	if g == nil || g.store == nil {
		return nil
	}
	if closer, ok := g.store.(interface {
		Close(context.Context) error
	}); ok {
		return closer.Close(ctx)
	}
	return nil
}

func DerivedKey(scope, subject, session string, sequence uint64) string {
	return fmt.Sprintf("v1:%s:%s:%s:%d",
		encodeDerivedKeyPart(normalizeToken(scope)),
		encodeDerivedKeyPart(normalizeToken(subject)),
		encodeDerivedKeyPart(normalizeToken(session)),
		sequence,
	)
}

// CanonicalKey 对任意文本字段做长度前缀编码后生成稳定键，不改变字段大小写。
func CanonicalKey(namespace string, parts ...string) string {
	hash := sha256.New()
	var size [8]byte
	writePart := func(part string) {
		binary.BigEndian.PutUint64(size[:], uint64(len(part)))
		_, _ = hash.Write(size[:])
		_, _ = hash.Write([]byte(part))
	}
	writePart(namespace)
	for _, part := range parts {
		writePart(part)
	}
	return "ck1_" + hex.EncodeToString(hash.Sum(nil))
}

func normalizeOptions(options Options) Options {
	if options.TTL <= 0 {
		options.TTL = 10 * time.Minute
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return options
}

func normalizeRequest(request Request) (Request, error) {
	request.Scope = normalizeToken(request.Scope)
	if request.Scope == "" {
		request.Scope = "default"
	}
	request.Subject = normalizeToken(request.Subject)
	request.Session = normalizeToken(request.Session)
	request.Key = normalizeToken(request.Key)
	request.Fingerprint = strings.TrimSpace(request.Fingerprint)
	if request.Subject == "" {
		return Request{}, fmt.Errorf("%w: subject is required", ErrInvalidRequest)
	}
	if len(request.Fingerprint) > 512 || strings.ContainsAny(request.Fingerprint, "\r\n") {
		return Request{}, fmt.Errorf("%w: fingerprint must be a single line of at most 512 bytes", ErrInvalidRequest)
	}
	if request.Sequence > maxRedisSeq {
		return Request{}, fmt.Errorf("%w: sequence must not exceed %d", ErrInvalidRequest, maxRedisSeq)
	}
	if !request.Now.IsZero() {
		request.Now = request.Now.UTC()
	}
	return request, nil
}

const maxRedisSeq = uint64(1<<53 - 1)

func newReservationToken() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate idempotency reservation token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func resvOwnerMatch(present bool, stored, expected string) bool {
	if expected == "" {
		// 旧版 reservation 没有 owner token：只能操作同样无 token 的占位，
		// 不得回退成可删除新版占位的“万能 token”。
		return !present || stored == ""
	}
	return present && stored == expected
}

func reqFPConflict(stored, incoming string) bool {
	stored = strings.TrimSpace(stored)
	incoming = strings.TrimSpace(incoming)
	return stored != "" && incoming != "" && stored != incoming
}

func (s *guardStats) record(decision Decision, err error, latency time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.backendError++
	} else {
		switch decision.Status {
		case StatusDuplicate:
			s.duplicate++
		case StatusInFlight:
			s.inFlight++
		case StatusReplay:
			s.replay++
		default:
			s.accepted++
		}
	}
	s.latencyNanos += latency.Nanoseconds()
}

func (s *guardStats) snapshot() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	total := s.accepted + s.duplicate + s.inFlight + s.replay + s.backendError
	avgMillis := int64(0)
	if total > 0 {
		avgMillis = s.latencyNanos / total / int64(time.Millisecond)
	}
	return map[string]any{
		"accepted":           s.accepted,
		"duplicate":          s.duplicate,
		"in_flight":          s.inFlight,
		"replay":             s.replay,
		"backend_error":      s.backendError,
		"checks":             total,
		"latency_ms_total":   s.latencyNanos / int64(time.Millisecond),
		"latency_ms_average": avgMillis,
	}
}

func sequenceScope(scope, subject, session string) string {
	return normalizeToken(scope) + "\x00" + normalizeToken(subject) + "\x00" + normalizeToken(session)
}

func encodeDerivedKeyPart(value string) string {
	if value == "" {
		return "_"
	}
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func normalizeToken(value string) string {
	return strings.TrimSpace(strings.ToLower(value))
}

func ctxErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}
