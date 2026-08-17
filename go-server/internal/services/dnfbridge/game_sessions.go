// game_sessions.go 维护 DNF game 连接到在线角色的短生命周期索引。
// 这里的状态只用于旧客户端实时回包投影，不是队伍、角色或玩法的持久 owner。
package dnfbridge

import "longheng.io/server/internal/modules/dnf/alignedcmd"

type boundGameSessionCharacter struct {
	session    *gameSession
	character  uint16
	generation uint64
}

func (s *Service) bindGameSessionCharacter(session *gameSession, charID uint16) {
	if s == nil || session == nil || charID == 0 {
		return
	}
	if oldID := session.selectedCharacterID; oldID != 0 && oldID != charID {
		s.closeCurrentExpertJobStoreSession(session, true)
		s.detachOnlineItemTrade(session, "selected_character_changed")
		s.detachRuntimePartySession(session, "selected_character_changed")
		s.stopCurrentSpendTimeClock(session, "selected_character_changed")
		s.stopCurrentPetGrowthClock(session, "selected_character_changed")
	}
	session.dungeon.mu.Lock()
	defer session.dungeon.mu.Unlock()
	if oldID := session.selectedCharacterID; oldID != 0 && oldID != charID {
		s.cancelCurrentDungeonDeathReturnLocked(session, session.dungeon.runtime, "selected_character_changed")
		s.cancelCurrentDungeonCardAutoFlipLocked(session, session.dungeon.runtime, "selected_character_changed")
	}
	resident := (*gameSession)(nil)
	residentParty := alignedcmd.PartyState{}
	if s.onlinePlayers != nil {
		resident = s.onlinePlayers.SessionForCharacter(charID)
		if resident != nil && resident != session && session.channelReconnect {
			// A channel-reconnect session binds before its op13 inventory
			// bootstrap. Settle and stop the resident clock synchronously here,
			// so any threshold reward is already durable and therefore included
			// in that new bootstrap. Waiting for the old socket's later unbind
			// would allow the reward commit to race after op13 and remain visually
			// absent until another login.
			s.stopCurrentSpendTimeClock(resident, "replacement_game_session_bound_before_inventory_bootstrap")
			residentParty = runtimePartyStateSnapshot(resident)
		} else if resident != nil && resident != session {
			residentParty = runtimePartyStateSnapshot(resident)
		}
	}
	s.mu.Lock()
	if s.gameSessions == nil {
		s.gameSessions = make(map[uint16]*gameSession)
	}
	if oldID := session.selectedCharacterID; oldID != 0 && oldID != charID {
		if s.gameSessions[oldID] == session {
			delete(s.gameSessions, oldID)
		}
	}
	previous := s.gameSessions[charID]
	characterChanged := session.selectedCharacterID != charID
	if characterChanged || session.characterGeneration == 0 ||
		(previous != nil && previous != session && session.characterGeneration == previous.characterGeneration) {
		s.allocateGameSessionCharacterGenerationLocked(session, previous)
	} else if session.characterGeneration > s.nextGameSessionGeneration {
		s.nextGameSessionGeneration = session.characterGeneration
	}
	if characterChanged {
		session.expertJobInfoCharacterID = 0
	}
	session.selectedCharacterID = charID
	if resident != nil && resident != session {
		if current := s.gameSessions[charID]; current == resident {
			storeRuntimePartyState(session, residentParty)
			s.mu.Unlock()
			return
		}
	}
	s.gameSessions[charID] = session
	s.mu.Unlock()
	if previous != nil && previous != session {
		s.rebindManagedRuntimePartySession(previous, session)
	}
}

// allocateGameSessionCharacterGenerationLocked assigns a nonce that is unique
// across the bridge process. RuntimePartyManager stores this generation without
// a session pointer, so a reconnect must never reuse the retired socket's
// initial value (both fresh sockets otherwise begin at generation one).
// s.mu must be held.
func (s *Service) allocateGameSessionCharacterGenerationLocked(session *gameSession, competing *gameSession) uint64 {
	if s == nil || session == nil {
		return 0
	}
	next := s.nextGameSessionGeneration
	if session.characterGeneration > next {
		next = session.characterGeneration
	}
	if competing != nil && competing.characterGeneration > next {
		next = competing.characterGeneration
	}
	next++
	if next == 0 {
		next++
	}
	s.nextGameSessionGeneration = next
	session.characterGeneration = next
	return next
}

func (s *Service) promoteResidentGameSession(session *gameSession, charID uint16) {
	if s == nil || session == nil || charID == 0 || session.selectedCharacterID != charID {
		return
	}
	s.mu.Lock()
	if s.gameSessions == nil {
		s.gameSessions = make(map[uint16]*gameSession)
	}
	previous := s.gameSessions[charID]
	s.gameSessions[charID] = session
	s.mu.Unlock()
	if previous != nil && previous != session {
		s.rebindManagedRuntimePartySession(previous, session)
	}
}

// rebindManagedRuntimePartySession promotes a channel-reconnect socket without
// treating it as a leave/join. The old socket's later close is generation
// stale and therefore cannot remove the replacement from the party.
func (s *Service) rebindManagedRuntimePartySession(previous *gameSession, replacement *gameSession) {
	if previous == nil || replacement == nil || previous.selectedCharacterID == 0 || previous.selectedCharacterID != replacement.selectedCharacterID {
		return
	}
	priorGeneration := previous.characterGeneration
	identity, current := s.boundGameSessionCharacterSnapshot(replacement)
	if !current || priorGeneration == 0 {
		return
	}
	member, memberOK := s.runtimePartyMemberForIdentity(identity)
	if !memberOK {
		return
	}
	manager := s.runtimePartyManagerForService()
	if manager == nil {
		return
	}
	result := manager.RebindSession(identity.character, priorGeneration, member)
	if result.OK {
		s.publishRuntimePartySnapshot(result.Party)
		return
	}
	// The first reconnect after deploying the central manager can still carry
	// only the legacy client projection. Import that exact projection under the
	// new session generation rather than leaving the replacement partyless.
	s.bootstrapManagedRuntimePartyForSession(replacement)
}

func (s *Service) unbindGameSession(session *gameSession) {
	if s == nil || session == nil {
		return
	}
	s.mu.Lock()
	canonical := session.selectedCharacterID != 0 && s.gameSessions[session.selectedCharacterID] == session
	s.mu.Unlock()
	if canonical {
		s.detachOnlineItemTrade(session, "game_session_unbound_or_disconnected")
		s.detachRuntimePartySession(session, "game_session_unbound_or_disconnected")
	}
	s.cancelCurrentDungeonDeathReturn(session, "game_session_unbound_or_disconnected")
	session.dungeon.mu.Lock()
	s.cancelCurrentDungeonCardAutoFlipLocked(session, session.dungeon.runtime, "game_session_unbound_or_disconnected")
	session.dungeon.mu.Unlock()
	s.stopCurrentPetGrowthClock(session, "game_session_unbound_or_disconnected")
	s.stopCurrentSpendTimeClock(session, "game_session_unbound_or_disconnected")
	s.mu.Lock()
	defer s.mu.Unlock()
	if session.selectedCharacterID != 0 && s.gameSessions[session.selectedCharacterID] == session {
		delete(s.gameSessions, session.selectedCharacterID)
	}
	if session.selectedCharacterID != 0 {
		advanceGameSessionCharacterGeneration(session)
	}
}

func (s *Service) onlineGameSession(charID uint16) (*gameSession, bool) {
	if s == nil || charID == 0 {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.gameSessions[charID]
	return session, ok && session != nil
}

func (s *Service) boundGameSessionCharacterSnapshot(session *gameSession) (boundGameSessionCharacter, bool) {
	if s == nil || session == nil {
		return boundGameSessionCharacter{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	characterID := session.selectedCharacterID
	if characterID == 0 || s.gameSessions[characterID] != session {
		return boundGameSessionCharacter{}, false
	}
	return boundGameSessionCharacter{
		session:    session,
		character:  characterID,
		generation: session.characterGeneration,
	}, true
}

func (s *Service) onlineGameSessionCharacterSnapshot(characterID uint16) (boundGameSessionCharacter, bool) {
	if s == nil || characterID == 0 {
		return boundGameSessionCharacter{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	session := s.gameSessions[characterID]
	if session == nil || session.selectedCharacterID != characterID {
		return boundGameSessionCharacter{}, false
	}
	return boundGameSessionCharacter{
		session:    session,
		character:  characterID,
		generation: session.characterGeneration,
	}, true
}

func (s *Service) boundGameSessionCharacterCurrent(identity boundGameSessionCharacter) bool {
	if s == nil || identity.session == nil || identity.character == 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gameSessions[identity.character] == identity.session &&
		identity.session.selectedCharacterID == identity.character &&
		identity.session.characterGeneration == identity.generation
}
