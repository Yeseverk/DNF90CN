package dnfbridge

import (
	"context"
)

func (s *Service) markCurrentTownSceneReady(session *gameSession) {
	if session == nil || session.selectedCharacterID == 0 {
		return
	}
	session.townMu.Lock()
	session.townSceneReadyCharacterID = session.selectedCharacterID
	session.townMu.Unlock()
}

func (s *Service) currentTownEnterSelectReady(session *gameSession) (bool, string) {
	if session == nil || session.selectedCharacterID == 0 {
		return false, "selected_character_missing"
	}
	session.townMu.Lock()
	readyCharacterID := session.townSceneReadyCharacterID
	positionSnapshot := session.townPositionSnapshot
	session.townMu.Unlock()
	if readyCharacterID == 0 || readyCharacterID != session.selectedCharacterID {
		return false, "town_scene_player_state_not_finalized"
	}
	if positionSnapshot.CharacterID != session.selectedCharacterID {
		return false, "town_scene_location_owner_missing"
	}
	if !positionSnapshot.PositionValid {
		return false, "town_scene_position_not_reported"
	}
	session.dungeon.mu.Lock()
	hasDungeonRuntime := session.dungeon.runtime != nil
	session.dungeon.mu.Unlock()
	if hasDungeonRuntime {
		return false, "active_dungeon_runtime"
	}
	return true, "current_town_scene_player_state_finalized"
}

// commitPendingDungeonReturnBeforeTownEnterSelect repairs the current EXE
// route where the client returns to town from an active dungeon and immediately
// clicks the dungeon gate again. That op15 can be the first town-side request
// after the typed return op24, so the wrapper commits the pending runtime and
// restores the town position owner needed by sendCurrentTownEnterSelectContext.
func (s *Service) commitPendingDungeonReturnBeforeTownEnterSelect(session *gameSession, source string) error {
	if session == nil || session.selectedCharacterID == 0 {
		return nil
	}
	characterID := session.selectedCharacterID

	session.dungeon.mu.Lock()
	runtime := session.dungeon.runtime
	shouldCommit := runtime != nil && runtime.Session != nil &&
		runtime.townReturnPending && runtime.townReturnOp24Sent
	transition := currentDungeonTownTransition{}
	origin := currentTownPositionSnapshot{}
	if shouldCommit {
		transition = cloneCurrentDungeonTownTransition(runtime.townReturnTransition)
		origin = runtime.townReturnOrigin
	}
	session.dungeon.mu.Unlock()
	if !shouldCommit {
		return s.ensureCurrentConfirmedDungeonReturnPlayerState(
			session,
			source+"_without_retained_runtime",
		)
	}
	if err := s.commitPendingDungeonReturnForSceneRequest(session, "current_exe_op15_enter_select_after_pending_town_return"); err != nil {
		return err
	}

	session.dungeon.mu.Lock()
	stillHasRuntime := session.dungeon.runtime != nil
	session.dungeon.mu.Unlock()
	if stillHasRuntime || session.selectedCharacterID != characterID {
		s.logGameEvent(session, "game-upper-enter-select-dungeon-pending-return-committed",
			"source", source,
			"char_id", characterID,
			"runtime_retained", stillHasRuntime,
			"selected_character", session.selectedCharacterID,
			"reason", "pending_return_committed_but_town_context_changed")
		return nil
	}

	snapshot := currentTownPositionSnapshot{
		CharacterID:   characterID,
		TownID:        transition.TownID,
		AreaID:        transition.AreaID,
		PositionX:     uint16(transition.PositionX),
		PositionY:     uint16(transition.PositionY),
		MovementCode:  transition.Direction,
		PositionValid: true,
	}
	positionSource := transition.PositionSource
	if origin.CharacterID == characterID &&
		origin.TownID == transition.TownID &&
		origin.AreaID == transition.AreaID &&
		origin.PositionValid {
		snapshot = origin
		positionSource = "current_exe_op35_runtime_origin_snapshot"
	}

	session.townMu.Lock()
	clearCurrentTownSelectorOriginLocked(session)
	session.townSceneReadyCharacterID = characterID
	session.townPositionSnapshot = snapshot
	session.townMu.Unlock()

	s.logGameEvent(session, "game-upper-enter-select-dungeon-pending-return-committed",
		"source", source,
		"char_id", characterID,
		"town_id", snapshot.TownID,
		"area_id", snapshot.AreaID,
		"position_x", snapshot.PositionX,
		"position_y", snapshot.PositionY,
		"movement_code", snapshot.MovementCode,
		"position_source", positionSource,
		"reason", "op15_can_be_first_town_side_request_after_back_to_village")
	return nil
}

// retireCompletedDungeonForTownSelect maps the settlement page's
// "select another dungeon" action onto the current op15/op27 selector owner.
// 86JP establishes that this action retires the completed run before entering
// the selector; the location itself is reconstructed from the current run's
// frozen op35 origin or the current Go/PVF town-transition builder.
func (s *Service) retireCompletedDungeonForTownSelect(session *gameSession) (bool, error) {
	if session == nil || session.selectedCharacterID == 0 {
		return false, nil
	}
	characterID := session.selectedCharacterID
	session.dungeon.mu.Lock()
	runtime := session.dungeon.runtime
	if runtime == nil || runtime.Session == nil ||
		!completedDungeonSettlementExitReady(runtime, characterID) {
		session.dungeon.mu.Unlock()
		return false, nil
	}
	origin := runtime.townReturnOrigin
	session.dungeon.mu.Unlock()

	originSource := "frozen_pre_dungeon_op35_origin"
	if origin.CharacterID != characterID || !origin.PositionValid {
		ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
		transition, err := s.prepareCurrentDungeonTownTransitionForSession(ctx, session, characterID)
		cancel()
		if err != nil {
			return false, err
		}
		origin = currentTownPositionSnapshot{
			CharacterID:   characterID,
			TownID:        transition.TownID,
			AreaID:        transition.AreaID,
			PositionX:     uint16(transition.PositionX),
			PositionY:     uint16(transition.PositionY),
			MovementCode:  transition.Direction,
			PositionValid: true,
		}
		originSource = transition.PositionSource
	}

	session.dungeon.mu.Lock()
	if session.dungeon.runtime != runtime ||
		!completedDungeonSettlementExitReady(runtime, characterID) {
		session.dungeon.mu.Unlock()
		return false, nil
	}
	s.cancelCurrentDungeonDeathReturnLocked(session, runtime, "completed_run_retires_into_other_dungeon_selector")
	s.cancelCurrentDungeonCardAutoFlipLocked(session, runtime, "completed_run_retires_into_other_dungeon_selector")
	session.dungeon.runtime = nil
	session.dungeon.mu.Unlock()
	if err := s.switchCurrentPetGrowthClock(session, currentPetGrowthClockTown, s.gameplayNow(), "completed_run_other_dungeon_selector"); err != nil {
		s.logGameEvent(session, "game-pet-growth-clock-start-deferred",
			"char_id", characterID,
			"mode", currentPetGrowthClockTown.String(),
			"source", "completed_run_other_dungeon_selector",
			"error", err)
	}

	resetDungeonEntrySceneGates(session)
	clearCurrentDungeonSelectContext(session)
	session.townMu.Lock()
	clearCurrentTownSelectorOriginLocked(session)
	session.townSceneReadyCharacterID = characterID
	session.townPositionSnapshot = origin
	session.townMu.Unlock()
	s.logGameEvent(session, "game-dungeon-completed-other-selector-committed",
		"char_id", characterID,
		"dungeon_id", runtime.Dungeon.ID,
		"town_id", origin.TownID,
		"area_id", origin.AreaID,
		"position_x", origin.PositionX,
		"position_y", origin.PositionY,
		"position_source", originSource,
		"runtime_detached", true,
		"next_owner", "current_exe_op15_ack_fatigue_op27_selector_context")
	return true, nil
}

func (s *Service) sendCurrentTownEnterSelectContext(session *gameSession, source string) error {
	ready, reason := s.currentTownEnterSelectReady(session)
	if !ready {
		s.logGameEvent(session, "game-upper-town-enter-select-context-deferred",
			"source", source,
			"char_id", session.selectedCharacterID,
			"msg_id", currentDungeonContextMsgID,
			"reason", reason)
		return nil
	}
	if err := s.sendCurrentDungeonContextOp27(session, source); err != nil {
		return err
	}
	origin, originBound := bindCurrentTownSelectorOrigin(session)
	s.logGameEvent(session, "game-upper-town-enter-select-origin-bound",
		"source", source,
		"char_id", session.selectedCharacterID,
		"town_id", origin.TownID,
		"area_id", origin.AreaID,
		"position_x", origin.PositionX,
		"position_y", origin.PositionY,
		"position_valid", origin.PositionValid,
		"origin_bound", originBound,
		"reason", "freeze_current_town_origin_after_successful_op27_write")
	session.enterSelectDungeonContextSent = true
	return nil
}
