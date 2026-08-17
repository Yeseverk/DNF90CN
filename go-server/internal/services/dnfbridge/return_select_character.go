package dnfbridge

import (
	"longheng.io/server/internal/modules/dnf/dnfenum"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

// handleUpperReturnSelectCharacter closes the selected-character lifecycle and
// acknowledges op7. The current client switches to the character-select module
// from the ACK handler, then issues op8 to request a fresh repository roster.
func (s *Service) handleUpperReturnSelectCharacter(session *gameSession, body []byte) error {
	if s == nil || session == nil {
		return nil
	}
	if len(body) != 0 {
		s.logGameEvent(session, "game-upper-return-select-character-blocked",
			"body_len", len(body),
			"expected_body_len", 0,
			"reason", "current_exe_op7_body_must_be_empty")
		return nil
	}

	previousCharacterID, abandonedDungeon := s.resetGameSessionForCharacterSelect(session)
	s.logGameEvent(session, "game-upper-return-select-character-send",
		"previous_char_id", previousCharacterID,
		"dungeon_runtime_abandoned", abandonedDungeon,
		"sequence", "class1_op7_success_then_wait_for_client_op8")
	return s.sendGameUpperSuccess(session, uint16(dnfenum.CmdPacketReturnSelectCharacter), nil)
}

func (s *Service) resetGameSessionForCharacterSelect(session *gameSession) (uint16, bool) {
	if s == nil || session == nil {
		return 0, false
	}
	s.closeCurrentExpertJobStoreSession(session, true)
	s.detachRuntimePartySession(session, "return_select_character")
	s.stopCurrentSpendTimeClock(session, "return_select_character")
	s.stopCurrentPetGrowthClock(session, "return_select_character")

	abandonedDungeon := false
	session.dungeon.mu.Lock()
	if runtime := session.dungeon.runtime; runtime != nil {
		if runtime.Session != nil && runtime.Session.Snapshot().Run.Status == worldmap.DungeonRunActive {
			if err := runtime.Session.Abandon(); err != nil {
				s.logGameEvent(session, "game-upper-return-select-character-dungeon-abandon-failed", "error", err)
			} else {
				abandonedDungeon = true
			}
		}
		s.cancelCurrentDungeonCardAutoFlipLocked(session, runtime, "return_select_character")
		session.dungeon.runtime = nil
	}

	s.mu.Lock()
	previousCharacterID := session.selectedCharacterID
	if previousCharacterID != 0 && s.gameSessions[previousCharacterID] == session {
		delete(s.gameSessions, previousCharacterID)
	}
	if previousCharacterID != 0 {
		advanceGameSessionCharacterGeneration(session)
	}
	session.selectedCharacterID = 0
	s.mu.Unlock()
	session.dungeon.mu.Unlock()

	resetDungeonEntrySceneGates(session)
	markReturnSelectTownReentry(session, previousCharacterID)
	clearCurrentDungeonSelectContext(session)
	clearCurrentTownSelectorOrigin(session)
	session.selectPreviewObjectStateSent = false
	session.rosterRequested = false
	session.channelReconnect = false
	session.townActorOwnerChannel = currentConnectionTownActorOwnerContext(session)
	return previousCharacterID, abandonedDungeon
}
