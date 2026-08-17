package lock

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"
)

var (
	// ErrKeyRequired 表示锁 key 为空或只包含空白字符。
	ErrKeyRequired = errors.New("lock key is required")
	// ErrLockHeld 表示目标锁仍被有效租约持有。
	ErrLockHeld = errors.New("lock is already held")
	// ErrTokenMismatch 表示释放锁时租约令牌与当前持有者不匹配。
	ErrTokenMismatch = errors.New("lock token mismatch")
	// ErrClosed 表示锁管理器已经关闭。
	ErrClosed = errors.New("lock manager is closed")
	// ErrManagerRequired 表示调用方传入了 nil 锁管理器。
	ErrManagerRequired = errors.New("lock manager is required")
)

const releaseTimeout = 5 * time.Second

// Manager 定义框架内存锁和分布式锁的统一租约接口。
type Manager interface {
	Acquire(context.Context, string, time.Duration) (Lease, error)
	Snapshot() Snapshot
	Close() error
}

// Lease 表示一次成功加锁后获得的可释放租约。
type Lease interface {
	Key() string
	Token() string
	ExpiresAt() time.Time
	Release(context.Context) error
}

// Snapshot 描述当前锁管理器的运行状态，供管理端和自检使用。
type Snapshot struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Locks     int    `json:"locks,omitempty"`
	KeyPrefix string `json:"key_prefix,omitempty"`
	Closed    bool   `json:"closed,omitempty"`
}

// TokenGenerator 为租约生成不可预测的持有令牌。
type TokenGenerator func() (string, error)

// MemoryOptions 配置进程内锁管理器，主要用于单进程开发和单元测试。
type MemoryOptions struct {
	Name           string
	DefaultTTL     time.Duration
	Now            func() time.Time
	TokenGenerator TokenGenerator
}

// MemoryManager 提供单进程内可过期的互斥租约。
type MemoryManager struct {
	mu             sync.Mutex
	name           string
	defaultTTL     time.Duration
	now            func() time.Time
	tokenGenerator TokenGenerator
	locks          map[string]memoryLock
	closed         bool
	metrics        *Metrics
}

type memoryLock struct {
	token     string
	expiresAt time.Time
}

// NewMemory 创建进程内锁管理器，并补齐默认名称、TTL、时钟和令牌生成器。
func NewMemory(options MemoryOptions) *MemoryManager {
	name := strings.TrimSpace(options.Name)
	if name == "" {
		name = "memory-lock"
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	defaultTTL := options.DefaultTTL
	if defaultTTL <= 0 {
		defaultTTL = 10 * time.Second
	}
	tokenGenerator := options.TokenGenerator
	if tokenGenerator == nil {
		tokenGenerator = randomToken
	}
	return &MemoryManager{
		name:           name,
		defaultTTL:     defaultTTL,
		now:            now,
		tokenGenerator: tokenGenerator,
		locks:          make(map[string]memoryLock),
	}
}

// SetMetrics 替换内存锁采集器；热路径会在锁内取快照后再记录。
func (m *MemoryManager) SetMetrics(metrics *Metrics) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.ensureReadyLocked()
	m.metrics = metrics
	m.mu.Unlock()
}

// Acquire 尝试获取指定 key 的租约；ttl <= 0 时使用管理器默认 TTL。
func (m *MemoryManager) Acquire(ctx context.Context, key string, ttl time.Duration) (lease Lease, err error) {
	if m == nil {
		return nil, ErrManagerRequired
	}
	m.mu.Lock()
	m.ensureReadyLocked()
	metricsRef := m.metrics
	m.mu.Unlock()
	defer func() {
		metricsRef.recordAcquire(classifyAcquire(err))
	}()
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	key = normalizeKey(key)
	if key == "" {
		return nil, ErrKeyRequired
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureReadyLocked()
	if m.closed {
		return nil, ErrClosed
	}
	if ttl <= 0 {
		ttl = m.defaultTTL
	}
	now := m.now().UTC()
	expiresAt := now.Add(ttl)
	if current, ok := m.locks[key]; ok && current.expiresAt.After(now) {
		return nil, ErrLockHeld
	}
	token, err := m.tokenGenerator()
	if err != nil {
		return nil, err
	}
	m.locks[key] = memoryLock{token: token, expiresAt: expiresAt}
	return &memoryLease{manager: m, key: key, token: token, expiresAt: expiresAt}, nil
}

// Snapshot 返回当前未过期锁数量，并顺手清理已过期的进程内记录。
func (m *MemoryManager) Snapshot() Snapshot {
	if m == nil {
		return Snapshot{Kind: "memory", Closed: true}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureReadyLocked()
	now := m.now().UTC()
	for key, lock := range m.locks {
		if !lock.expiresAt.After(now) {
			delete(m.locks, key)
		}
	}
	return Snapshot{
		Name:   m.name,
		Kind:   "memory",
		Locks:  len(m.locks),
		Closed: m.closed,
	}
}

// Close 关闭内存锁管理器，并清空本进程内仍持有的租约。
func (m *MemoryManager) Close() error {
	if m == nil {
		return ErrManagerRequired
	}
	m.mu.Lock()
	m.ensureReadyLocked()
	if m.closed {
		m.mu.Unlock()
		return ErrClosed
	}
	m.closed = true
	m.locks = make(map[string]memoryLock)
	m.mu.Unlock()
	return nil
}

func (m *MemoryManager) release(ctx context.Context, key, token string) error {
	if m == nil {
		return ErrManagerRequired
	}
	if err := ctxErr(ctx); err != nil {
		return err
	}
	key = normalizeKey(key)
	if key == "" {
		return ErrKeyRequired
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureReadyLocked()
	if m.closed {
		return ErrClosed
	}
	current, ok := m.locks[key]
	if !ok {
		return nil
	}
	if current.token != token {
		return ErrTokenMismatch
	}
	delete(m.locks, key)
	return nil
}

func (m *MemoryManager) ensureReadyLocked() {
	if m.name == "" {
		m.name = "memory-lock"
	}
	if m.defaultTTL <= 0 {
		m.defaultTTL = 10 * time.Second
	}
	if m.now == nil {
		m.now = time.Now
	}
	if m.tokenGenerator == nil {
		m.tokenGenerator = randomToken
	}
	if m.locks == nil {
		m.locks = make(map[string]memoryLock)
	}
}

type memoryLease struct {
	manager   *MemoryManager
	key       string
	token     string
	expiresAt time.Time
}

func (l *memoryLease) Key() string {
	return l.key
}

func (l *memoryLease) Token() string {
	return l.token
}

func (l *memoryLease) ExpiresAt() time.Time {
	return l.expiresAt
}

func (l *memoryLease) Release(ctx context.Context) error {
	if l == nil || l.manager == nil {
		return nil
	}
	releaseCtx, cancel := releaseContext(ctx)
	defer cancel()
	return l.manager.release(releaseCtx, l.key, l.token)
}

func normalizeKey(key string) string {
	return strings.TrimSpace(key)
}

func ctxErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func releaseContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		return context.WithTimeout(context.Background(), releaseTimeout)
	}
	return context.WithTimeout(context.WithoutCancel(parent), releaseTimeout)
}

func randomToken() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}
