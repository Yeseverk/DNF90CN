package logic

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"longheng.io/server/internal/platform/sessioncontract"
	"longheng.io/server/internal/reference/player"
	"longheng.io/server/pkg/contracts"
)

const (
	accountStateLoading = string(sessioncontract.PlayerStateLoading)
	accountStateOnline  = string(sessioncontract.PlayerStateOnline)
	accountStateValid   = string(sessioncontract.PlayerStateValid)
	accountStateReconn  = string(sessioncontract.PlayerStateReconnecting)
	accountStateClosing = string(sessioncontract.PlayerStateClosing)
	accountStateKicked  = string(sessioncontract.PlayerStateKicked)
	accountStateInvalid = string(sessioncontract.PlayerStateInvalid)
	accountStateOffline = "offline"
	acctProfileActive   = "active"
)

// AccountManagerOptions 是账号管理器的空闲回收和停机回收配置。
type AccountManagerOptions struct {
	IdleTTL        time.Duration
	SweepInterval  time.Duration
	ReclaimTimeout time.Duration
}

// AccountSnapshot 是账号运行时生命周期状态快照。
type AccountSnapshot struct {
	AccountID      string    `json:"account_id"`
	SessionID      string    `json:"session_id,omitempty"`
	State          string    `json:"state"`
	Generation     int64     `json:"generation"`
	ConnectedAt    time.Time `json:"connected_at,omitempty"`
	DisconnectedAt time.Time `json:"disconnected_at,omitempty"`
	LastActiveAt   time.Time `json:"last_active_at"`
}

type playerRuntime struct {
	accountID      string
	sessionID      string
	state          string
	generation     int64
	connectedAt    time.Time
	disconnectedAt time.Time
	lastActiveAt   time.Time
}

// AccountManager 管理账号在线、断线、重连和玩家资料回收。
type AccountManager struct {
	players *player.Module
	logger  *slog.Logger

	idleTTL        time.Duration
	sweepInterval  time.Duration
	reclaimTimeout time.Duration

	mu       sync.RWMutex
	accounts map[string]*playerRuntime
	started  bool
	stopping bool
	cancel   context.CancelFunc
	done     chan struct{}
}

func newAccountManager(players *player.Module, logger *slog.Logger, options AccountManagerOptions) *AccountManager {
	if options.IdleTTL <= 0 {
		options.IdleTTL = 10 * time.Minute
	}
	if options.SweepInterval <= 0 {
		options.SweepInterval = options.IdleTTL / 2
		if options.SweepInterval <= 0 {
			options.SweepInterval = time.Minute
		}
	}
	if options.ReclaimTimeout <= 0 {
		options.ReclaimTimeout = 5 * time.Second
	}
	return &AccountManager{
		players:        players,
		logger:         logger,
		idleTTL:        options.IdleTTL,
		sweepInterval:  options.SweepInterval,
		reclaimTimeout: options.ReclaimTimeout,
		accounts:       make(map[string]*playerRuntime),
	}
}

// Start 启动账号空闲回收循环。
func (m *AccountManager) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	if m.started || m.stopping {
		m.mu.Unlock()
		return nil
	}
	runCtx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.done = make(chan struct{})
	m.started = true
	done := m.done
	m.mu.Unlock()

	go m.sweep(runCtx, done)
	return nil
}

// Stop 停止账号管理器并保存卸载已加载账号。
func (m *AccountManager) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	if !m.started && !m.stopping {
		m.mu.Unlock()
		return nil
	}
	cancel := m.cancel
	done := m.done
	shouldSignal := m.started
	if shouldSignal {
		m.cancel = nil
		m.started = false
		m.stopping = true
	}
	accountIDs := make([]string, 0, len(m.accounts))
	for accountID, runtime := range m.accounts {
		if shouldSignal {
			runtime.state = accountStateClosing
			runtime.generation++
		}
		accountIDs = append(accountIDs, accountID)
	}
	m.mu.Unlock()

	if shouldSignal && cancel != nil {
		cancel()
	}
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	var firstErr error
	for _, accountID := range accountIDs {
		if _, _, err := m.players.SaveAndUnload(ctx, accountID, "account_manager_stop"); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	m.mu.Lock()
	m.accounts = make(map[string]*playerRuntime)
	m.cancel = nil
	m.done = nil
	m.stopping = false
	m.mu.Unlock()
	return firstErr
}

// Connect 处理 gateway 会话接入并加载或创建玩家资料。
func (m *AccountManager) Connect(ctx context.Context, evt contracts.SessionConnected) (player.Profile, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	evt.AccountID = strings.TrimSpace(evt.AccountID)
	evt.SessionID = strings.TrimSpace(evt.SessionID)
	now := evt.ConnectedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}

	loadingGeneration, err := m.beginLogin(evt.AccountID, evt.SessionID, now)
	if err != nil {
		return player.Profile{}, err
	}
	if _, err := m.players.LoadOrCreate(ctx, evt.AccountID); err != nil {
		m.clearRuntimeGen(evt.AccountID, evt.SessionID, loadingGeneration)
		return player.Profile{}, err
	}
	profile := m.players.Upsert(evt.AccountID, accountStateOnline)
	m.finishLogin(evt.AccountID, evt.SessionID, loadingGeneration, now)
	if m.logger != nil {
		m.logger.Info("account connected", "account_id", evt.AccountID, "session_id", evt.SessionID)
	}
	return profile, nil
}

func (m *AccountManager) beginLogin(accountID, sessionID string, now time.Time) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	runtime := m.accounts[accountID]
	if runtime == nil {
		runtime = &playerRuntime{accountID: accountID}
		m.accounts[accountID] = runtime
	}
	next, err := sessioncontract.NextPlayerLifecycleState(sessioncontract.PlayerLifecycleState(runtime.state), sessioncontract.PlayerEventLoginStart)
	if err != nil {
		return 0, err
	}
	runtime.sessionID = sessionID
	runtime.state = string(next)
	runtime.connectedAt = now
	runtime.disconnectedAt = time.Time{}
	runtime.lastActiveAt = now
	runtime.generation++
	return runtime.generation, nil
}

func (m *AccountManager) finishLogin(accountID, sessionID string, generation int64, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	runtime := m.accounts[accountID]
	if runtime == nil || runtime.generation != generation {
		return
	}
	if runtime.sessionID != "" && sessionID != "" && runtime.sessionID != sessionID {
		return
	}
	next, err := sessioncontract.NextPlayerLifecycleState(sessioncontract.PlayerLifecycleState(runtime.state), sessioncontract.PlayerEventLoginBound)
	if err != nil {
		return
	}
	runtime.state = string(next)
	runtime.connectedAt = now
	runtime.disconnectedAt = time.Time{}
	runtime.lastActiveAt = now
	runtime.generation++
}

func (m *AccountManager) clearRuntimeGen(accountID, sessionID string, generation int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	runtime := m.accounts[accountID]
	if runtime == nil || runtime.generation != generation {
		return
	}
	if runtime.sessionID != "" && sessionID != "" && runtime.sessionID != sessionID {
		return
	}
	delete(m.accounts, accountID)
}

// Disconnect 处理 gateway 会话断开并标记账号离线。
func (m *AccountManager) Disconnect(ctx context.Context, evt contracts.SessionDisconnected) (player.Profile, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	evt.AccountID = strings.TrimSpace(evt.AccountID)
	evt.SessionID = strings.TrimSpace(evt.SessionID)
	now := evt.DisconnectedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}

	m.mu.Lock()
	runtime := m.accounts[evt.AccountID]
	if runtime == nil {
		m.mu.Unlock()
		return player.Profile{}, false, nil
	}
	if runtime.sessionID != "" && evt.SessionID != "" && runtime.sessionID != evt.SessionID {
		m.mu.Unlock()
		return player.Profile{}, false, nil
	}
	next, err := sessioncontract.NextPlayerLifecycleState(sessioncontract.PlayerLifecycleState(runtime.state), sessioncontract.PlayerEventDisconnect)
	if err != nil {
		m.mu.Unlock()
		return player.Profile{}, false, err
	}
	runtime.state = string(next)
	runtime.disconnectedAt = now
	runtime.lastActiveAt = now
	runtime.generation++
	m.mu.Unlock()

	profile := m.players.Upsert(evt.AccountID, accountStateOffline)
	if m.logger != nil {
		m.logger.Info("account disconnected", "account_id", evt.AccountID, "session_id", evt.SessionID, "reason", evt.Reason)
	}
	return profile, true, ctx.Err()
}

// Activate 标记账号已完成逻辑侧激活。
func (m *AccountManager) Activate(ctx context.Context, accountID, sessionID string) (player.Profile, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	accountID = strings.TrimSpace(accountID)
	sessionID = strings.TrimSpace(sessionID)
	now := time.Now().UTC()
	if profile, ok := m.players.Get(accountID); ok && m.touchActiveRuntime(accountID, sessionID, now) {
		return profile, nil
	}
	if _, err := m.players.LoadOrCreate(ctx, accountID); err != nil {
		return player.Profile{}, err
	}
	profile := m.players.Upsert(accountID, acctProfileActive)
	if err := m.activateRuntime(accountID, sessionID, now); err != nil {
		return player.Profile{}, err
	}
	return profile, nil
}

func (m *AccountManager) touchActiveRuntime(accountID, sessionID string, now time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	runtime := m.accounts[accountID]
	if runtime == nil || runtime.state != accountStateValid {
		return false
	}
	if runtime.sessionID != "" && sessionID != "" && runtime.sessionID != sessionID {
		return false
	}
	if sessionID != "" {
		runtime.sessionID = sessionID
	}
	runtime.lastActiveAt = now
	return true
}

func (m *AccountManager) activateRuntime(accountID, sessionID string, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	runtime := m.accounts[accountID]
	if runtime == nil {
		runtime = &playerRuntime{accountID: accountID}
		m.accounts[accountID] = runtime
	}
	if sessionID != "" {
		runtime.sessionID = sessionID
	}
	if runtime.state == "" || runtime.state == accountStateInvalid || runtime.state == accountStateClosing {
		runtime.state = accountStateOnline
	}
	next, err := sessioncontract.NextPlayerLifecycleState(sessioncontract.PlayerLifecycleState(runtime.state), sessioncontract.PlayerEventActivate)
	if err != nil {
		return err
	}
	runtime.state = string(next)
	runtime.lastActiveAt = now
	runtime.generation++
	return nil
}

// Snapshot 返回所有账号运行时状态快照。
func (m *AccountManager) Snapshot() []AccountSnapshot {
	m.mu.RLock()
	out := make([]AccountSnapshot, 0, len(m.accounts))
	for _, runtime := range m.accounts {
		out = append(out, runtime.snapshot())
	}
	m.mu.RUnlock()

	sort.Slice(out, func(i, j int) bool {
		return out[i].AccountID < out[j].AccountID
	})
	return out
}

func (m *AccountManager) sweep(ctx context.Context, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(m.sweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.reclaimIdle(ctx, time.Now().UTC())
		case <-ctx.Done():
			return
		}
	}
}

func (m *AccountManager) reclaimIdle(ctx context.Context, now time.Time) {
	if m.idleTTL <= 0 {
		return
	}
	type candidate struct {
		accountID  string
		generation int64
	}
	candidates := make([]candidate, 0)

	m.mu.Lock()
	for accountID, runtime := range m.accounts {
		if !isReclaimableAcct(runtime.state) || runtime.lastActiveAt.IsZero() {
			continue
		}
		if now.Sub(runtime.lastActiveAt) < m.idleTTL {
			continue
		}
		if runtime.state == accountStateOffline {
			runtime.state = accountStateReconn
		}
		next, err := sessioncontract.NextPlayerLifecycleState(sessioncontract.PlayerLifecycleState(runtime.state), sessioncontract.PlayerEventReclaim)
		if err != nil {
			continue
		}
		runtime.state = string(next)
		runtime.generation++
		candidates = append(candidates, candidate{accountID: accountID, generation: runtime.generation})
	}
	m.mu.Unlock()

	for _, c := range candidates {
		saveCtx, cancel := context.WithTimeout(ctx, m.reclaimTimeout)
		profile, saved, err := m.players.SaveAndUnload(saveCtx, c.accountID, "idle_timeout")
		cancel()
		if err != nil {
			m.restoreReclaimFail(c.accountID, c.generation)
			if m.logger != nil {
				m.logger.Error("account idle save failed", "account_id", c.accountID, "error", err)
			}
			continue
		}
		m.mu.Lock()
		runtime := m.accounts[c.accountID]
		if runtime != nil && runtime.state == accountStateClosing && runtime.generation == c.generation {
			delete(m.accounts, c.accountID)
		}
		m.mu.Unlock()
		if saved && m.logger != nil {
			m.logger.Info("account reclaimed after idle", "account_id", profile.AccountID)
		}
	}
}

func (m *AccountManager) restoreReclaimFail(accountID string, generation int64) {
	accountID = strings.TrimSpace(accountID)
	m.mu.Lock()
	defer m.mu.Unlock()
	runtime := m.accounts[accountID]
	if runtime == nil || runtime.generation != generation || runtime.state != accountStateClosing {
		return
	}
	next, err := sessioncontract.NextPlayerLifecycleState(sessioncontract.PlayerLifecycleState(runtime.state), sessioncontract.EventRestoreFail)
	if err != nil {
		return
	}
	runtime.state = string(next)
	runtime.generation++
	runtime.lastActiveAt = time.Now().UTC()
}

func isReclaimableAcct(state string) bool {
	switch state {
	case accountStateReconn:
		return true
	case accountStateOffline:
		return true
	default:
		return false
	}
}

func (r *playerRuntime) snapshot() AccountSnapshot {
	return AccountSnapshot{
		AccountID:      r.accountID,
		SessionID:      r.sessionID,
		State:          r.state,
		Generation:     r.generation,
		ConnectedAt:    r.connectedAt,
		DisconnectedAt: r.disconnectedAt,
		LastActiveAt:   r.lastActiveAt,
	}
}
