package profile

import (
	"strings"
	"sync"
	"time"
)

type ValidState string

const (
	ValidStateInvalid ValidState = "invalid"
	ValidStateLoading ValidState = "loading"
	ValidStateValid   ValidState = "valid"
	ValidStateClosing ValidState = "closing"
)

type OnlineState string

const (
	OnlineStateOffline       OnlineState = "offline"
	OnlineStateConnecting    OnlineState = "connecting"
	OnlineStateOnline        OnlineState = "online"
	OnlineStateDisconnecting OnlineState = "disconnecting"
)

type AccountOnlineState struct {
	AccountID string      `json:"account_id"`
	SessionID string      `json:"session_id,omitempty"`
	Valid     ValidState  `json:"valid"`
	Online    OnlineState `json:"online"`
	UpdatedAt time.Time   `json:"updated_at"`
}

func (s AccountOnlineState) CanHandleRequest(sessionID string) bool {
	sessionID = strings.TrimSpace(sessionID)
	if s.Valid != ValidStateValid || s.Online != OnlineStateOnline {
		return false
	}
	return s.SessionID == "" || sessionID == "" || s.SessionID == sessionID
}

type OnlineStateMachine struct {
	mu     sync.RWMutex
	states map[string]AccountOnlineState
	now    func() time.Time
}

func NewOnlineStateMachine() *OnlineStateMachine {
	return NewOnlineStateClock(time.Now)
}

func NewOnlineStateClock(now func() time.Time) *OnlineStateMachine {
	if now == nil {
		now = time.Now
	}
	return &OnlineStateMachine{
		states: make(map[string]AccountOnlineState),
		now:    now,
	}
}

func (m *OnlineStateMachine) BeginLoad(accountID string) AccountOnlineState {
	return m.set(accountID, "", ValidStateLoading, OnlineStateOffline)
}

func (m *OnlineStateMachine) MarkValid(accountID string) AccountOnlineState {
	return m.update(accountID, func(state AccountOnlineState) AccountOnlineState {
		state.Valid = ValidStateValid
		if state.Online == "" {
			state.Online = OnlineStateOffline
		}
		return state
	})
}

func (m *OnlineStateMachine) BindSession(accountID, sessionID string) AccountOnlineState {
	sessionID = strings.TrimSpace(sessionID)
	return m.update(accountID, func(state AccountOnlineState) AccountOnlineState {
		state.Valid = ValidStateValid
		state.Online = OnlineStateOnline
		state.SessionID = sessionID
		return state
	})
}

func (m *OnlineStateMachine) Disconnect(accountID, sessionID string) AccountOnlineState {
	sessionID = strings.TrimSpace(sessionID)
	return m.update(accountID, func(state AccountOnlineState) AccountOnlineState {
		if sessionID != "" && state.SessionID != "" && state.SessionID != sessionID {
			return state
		}
		state.Online = OnlineStateOffline
		state.SessionID = ""
		if state.Valid == "" {
			state.Valid = ValidStateInvalid
		}
		return state
	})
}

func (m *OnlineStateMachine) MarkClosing(accountID string) AccountOnlineState {
	return m.update(accountID, func(state AccountOnlineState) AccountOnlineState {
		state.Valid = ValidStateClosing
		if state.Online == OnlineStateOnline {
			state.Online = OnlineStateDisconnecting
		}
		return state
	})
}

func (m *OnlineStateMachine) Invalidate(accountID string) AccountOnlineState {
	return m.set(accountID, "", ValidStateInvalid, OnlineStateOffline)
}

func (m *OnlineStateMachine) Get(accountID string) (AccountOnlineState, bool) {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" || m == nil {
		return AccountOnlineState{}, false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	state, ok := m.states[accountID]
	return state, ok
}

func (m *OnlineStateMachine) Snapshot() []AccountOnlineState {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]AccountOnlineState, 0, len(m.states))
	for _, state := range m.states {
		out = append(out, state)
	}
	return out
}

func (m *OnlineStateMachine) set(accountID, sessionID string, valid ValidState, online OnlineState) AccountOnlineState {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" || m == nil {
		return AccountOnlineState{}
	}
	state := AccountOnlineState{
		AccountID: accountID,
		SessionID: strings.TrimSpace(sessionID),
		Valid:     valid,
		Online:    online,
		UpdatedAt: m.now().UTC(),
	}
	m.mu.Lock()
	m.states[accountID] = state
	m.mu.Unlock()
	return state
}

func (m *OnlineStateMachine) update(accountID string, mutate func(AccountOnlineState) AccountOnlineState) AccountOnlineState {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" || m == nil {
		return AccountOnlineState{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.states[accountID]
	state.AccountID = accountID
	state = mutate(state)
	state.UpdatedAt = m.now().UTC()
	m.states[accountID] = state
	return state
}
