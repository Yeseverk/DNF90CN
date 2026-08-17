package dnfbridge

import (
	"errors"
	"strings"
	"sync"
	"time"
)

const clientAccountRegistrationTTL = 24 * time.Hour

type clientAccountRegistration struct {
	accountID                    string
	expiresAt                    time.Time
	spendTimeEventInfoAuthorized bool
	spendTimeEventInfoSendState  uint8
}

const (
	spendTimeEventInfoUnsent uint8 = iota
	spendTimeEventInfoSending
	spendTimeEventInfoSent
)

type clientAccountRegistry struct {
	mu    sync.Mutex
	byPID map[uint32]clientAccountRegistration
}

func newClientAccountRegistry() clientAccountRegistry {
	return clientAccountRegistry{
		byPID: make(map[uint32]clientAccountRegistration),
	}
}

// RegisterClientAccount binds one launcher-authenticated local DNF process to
// its repository account. The loopback authenticated admin endpoint is the
// only production caller; no password or session secret enters the bridge.
func (s *Service) RegisterClientAccount(pid uint32, accountID string) error {
	if s == nil {
		return errors.New("dnf bridge is unavailable")
	}
	accountID = strings.TrimSpace(accountID)
	if pid == 0 {
		return errors.New("client PID must be positive")
	}
	if accountID == "" {
		return errors.New("client account ID is required")
	}
	now := time.Now().UTC()
	s.clientAccounts.mu.Lock()
	defer s.clientAccounts.mu.Unlock()
	if s.clientAccounts.byPID == nil {
		s.clientAccounts.byPID = make(map[uint32]clientAccountRegistration)
	}
	for registeredPID, entry := range s.clientAccounts.byPID {
		if !entry.expiresAt.After(now) {
			delete(s.clientAccounts.byPID, registeredPID)
		}
	}
	entry, found := s.clientAccounts.byPID[pid]
	if found && entry.expiresAt.After(now) && strings.TrimSpace(entry.accountID) == accountID &&
		entry.spendTimeEventInfoAuthorized &&
		entry.spendTimeEventInfoSendState == spendTimeEventInfoUnsent {
		// A transport retry before the new process reaches op108 is idempotent.
		// Once sending has begun, a later launcher registration is a new process
		// lifecycle (including OS PID reuse) and must receive a fresh gate.
		entry.expiresAt = now.Add(clientAccountRegistrationTTL)
		s.clientAccounts.byPID[pid] = entry
		return nil
	}
	s.clientAccounts.byPID[pid] = clientAccountRegistration{
		accountID:                    accountID,
		expiresAt:                    now.Add(clientAccountRegistrationTTL),
		spendTimeEventInfoAuthorized: true,
	}
	return nil
}

func (s *Service) registeredClientAccount(pid uint32) (string, bool) {
	if s == nil || pid == 0 {
		return "", false
	}
	now := time.Now().UTC()
	s.clientAccounts.mu.Lock()
	defer s.clientAccounts.mu.Unlock()
	entry, found := s.clientAccounts.byPID[pid]
	if !found {
		return "", false
	}
	if !entry.expiresAt.After(now) {
		delete(s.clientAccounts.byPID, pid)
		return "", false
	}
	accountID := strings.TrimSpace(entry.accountID)
	return accountID, accountID != ""
}

// beginCurrentSpendTimeEventInfo reserves the single process-lifetime op108
// descriptor. A second game session for the same local client PID must not
// emit the first-process base-catalog grammar again. The in-flight state also keeps
// concurrent reconnect sessions from sending progress ahead of that first
// descriptor. PID=0 is not authoritative and therefore remains progress-only.
func (s *Service) beginCurrentSpendTimeEventInfo(session *gameSession) (send bool, ready bool) {
	if s == nil || session == nil {
		return false, false
	}
	if session.clientPID == 0 {
		session.spendTime.eventInfoSent = true
		return false, true
	}
	now := time.Now().UTC()
	s.clientAccounts.mu.Lock()
	defer s.clientAccounts.mu.Unlock()
	if s.clientAccounts.byPID == nil {
		s.clientAccounts.byPID = make(map[uint32]clientAccountRegistration)
	}
	entry, found := s.clientAccounts.byPID[session.clientPID]
	if !found || !entry.expiresAt.After(now) {
		entry = clientAccountRegistration{expiresAt: now.Add(clientAccountRegistrationTTL)}
	}
	if !entry.spendTimeEventInfoAuthorized {
		// An unknown PID can be an already-running client reconnecting after a
		// server restart. Its singleton may already have consumed op108, so only
		// the launcher-authenticated new-process registration may authorize the
		// first-process grammar. Progress-only is the safe recovery behavior.
		session.spendTime.eventInfoSent = true
		s.clientAccounts.byPID[session.clientPID] = entry
		return false, true
	}
	switch entry.spendTimeEventInfoSendState {
	case spendTimeEventInfoSent:
		session.spendTime.eventInfoSent = true
		s.clientAccounts.byPID[session.clientPID] = entry
		return false, true
	case spendTimeEventInfoSending:
		s.clientAccounts.byPID[session.clientPID] = entry
		return false, false
	default:
		entry.spendTimeEventInfoSendState = spendTimeEventInfoSending
		s.clientAccounts.byPID[session.clientPID] = entry
		return true, true
	}
}

// finishCurrentSpendTimeEventInfo commits or releases a reservation. sent=true
// must be used as soon as the op108 socket write succeeds, even if the
// following op1206 write fails; retrying the first-process grammar would then
// be invalid for that client process.
func (s *Service) finishCurrentSpendTimeEventInfo(session *gameSession, sent bool) {
	if s == nil || session == nil {
		return
	}
	if sent {
		session.spendTime.eventInfoSent = true
	}
	if session.clientPID == 0 {
		return
	}
	now := time.Now().UTC()
	s.clientAccounts.mu.Lock()
	defer s.clientAccounts.mu.Unlock()
	if s.clientAccounts.byPID == nil {
		s.clientAccounts.byPID = make(map[uint32]clientAccountRegistration)
	}
	entry, found := s.clientAccounts.byPID[session.clientPID]
	if !found || !entry.expiresAt.After(now) {
		entry = clientAccountRegistration{expiresAt: now.Add(clientAccountRegistrationTTL)}
	}
	if sent {
		entry.spendTimeEventInfoSendState = spendTimeEventInfoSent
	} else if entry.spendTimeEventInfoSendState == spendTimeEventInfoSending {
		entry.spendTimeEventInfoSendState = spendTimeEventInfoUnsent
	}
	s.clientAccounts.byPID[session.clientPID] = entry
}
