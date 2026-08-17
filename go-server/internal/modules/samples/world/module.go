package world

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"
)

type Snapshot struct {
	OnlinePlayers int       `json:"online_players"`
	LastEvent     string    `json:"last_event"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type Module struct {
	logger *slog.Logger

	mu      sync.RWMutex
	active  map[string]bool
	summary Snapshot
}

func New(logger *slog.Logger) *Module {
	return &Module{
		logger: logger,
		active: make(map[string]bool),
	}
}

func (m *Module) Name() string {
	return "world-module"
}

func (m *Module) Start(context.Context) error {
	return nil
}

func (m *Module) Stop(context.Context) error {
	return nil
}

func (m *Module) ObservePlayer(accountID, state string) Snapshot {
	accountID = strings.TrimSpace(accountID)
	state = strings.TrimSpace(state)
	if accountID == "" {
		return m.Snapshot()
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	switch state {
	case "online":
		m.active[accountID] = true
	case "offline":
		delete(m.active, accountID)
	default:
		m.active[accountID] = true
	}

	m.summary = Snapshot{
		OnlinePlayers: len(m.active),
		LastEvent:     accountID + ":" + state,
		UpdatedAt:     time.Now().UTC(),
	}

	if m.logger != nil {
		m.logger.Info("world snapshot updated", "online_players", m.summary.OnlinePlayers, "last_event", m.summary.LastEvent)
	}

	return m.summary
}

func (m *Module) Snapshot() Snapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.summary
}
