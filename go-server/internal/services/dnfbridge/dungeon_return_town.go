package dnfbridge

import (
	"context"
	"fmt"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

type currentDungeonTownTransition struct {
	TownID         byte
	AreaID         byte
	ActorObjectKey uint16
	PositionX      int16
	PositionY      int16
	Direction      byte
	AreaState      byte
	PositionSource string
	Body           []byte
}

func (s *Service) handleDungeonBackToVillage(session *gameSession, body []byte) error {
	if session == nil {
		return nil
	}
	if len(body) != 0 {
		s.logGameEvent(session, "game-dungeon-back-to-village-blocked",
			"body_len", len(body),
			"reason", "current_exe_op132_body_must_be_empty")
		return nil
	}
	if session.selectedCharacterID == 0 {
		s.logGameEvent(session, "game-dungeon-back-to-village-blocked",
			"body_len", len(body),
			"reason", "selected_character_missing")
		return nil
	}
	selectedCharacterID := session.selectedCharacterID

	if _, err := s.persistCurrentActiveTutorialExit(
		session,
		selectedCharacterID,
		"confirmed_mid_tutorial_bodyless_op132_back_to_village",
	); err != nil {
		s.logGameEvent(session, "game-dungeon-back-to-village-blocked",
			"char_id", selectedCharacterID,
			"reason", "confirmed_tutorial_abandonment_persistence_failed",
			"error", err)
		return nil
	}

	// The current client uses the same bodyless op132 from two distinct owners:
	// an active dungeon run and the town-side dungeon-selection page.  The
	// latter has no dungeon runtime by design, so require the request-driven
	// op15/op27 selector context before allowing that branch.
	session.dungeon.mu.Lock()
	hasDungeonRuntime := session.dungeon.runtime != nil && session.dungeon.runtime.Session != nil
	session.dungeon.mu.Unlock()
	selectorReady := false
	selectorReason := "active_dungeon_runtime_changed_before_return"
	if !hasDungeonRuntime {
		selectorReady, selectorReason = currentDungeonSelectTownReturnReady(session)
		if !selectorReady {
			s.logGameEvent(session, "game-dungeon-back-to-village-blocked",
				"char_id", selectedCharacterID,
				"body_len", len(body),
				"reason", selectorReason)
			return nil
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	transition, err := s.prepareCurrentDungeonTownTransitionForSession(ctx, session, selectedCharacterID)
	if err != nil {
		s.logGameEvent(session, "game-dungeon-back-to-village-blocked",
			"char_id", session.selectedCharacterID,
			"body_len", len(body),
			"reason", "persisted_town_transition_unavailable",
			"error", err)
		return nil
	}

	session.dungeon.mu.Lock()
	runtime := session.dungeon.runtime
	if runtime == nil || runtime.Session == nil {
		if session.selectedCharacterID != selectedCharacterID {
			selectorReady = false
			selectorReason = "selected_character_changed_before_dungeon_select_return"
		} else if !session.enterSelectDungeonSent || !session.enterSelectDungeonAckSent || !session.enterSelectDungeonContextSent {
			selectorReady = false
			selectorReason = "confirmed_dungeon_select_context_cleared_before_return"
		}
		if !selectorReady {
			s.logGameEvent(session, "game-dungeon-back-to-village-blocked",
				"char_id", selectedCharacterID,
				"town_id", transition.TownID,
				"area_id", transition.AreaID,
				"reason", selectorReason)
			session.dungeon.mu.Unlock()
			return nil
		}
		s.logGameEvent(session, "game-dungeon-select-back-to-village-op24-send",
			"char_id", session.selectedCharacterID,
			"request_msg_id", uint16(dnfenum.CmdPacketBack2Village),
			"response_msg_id", currentSceneTransitionMsgID,
			"classification", 0,
			"town_id", transition.TownID,
			"area_id", transition.AreaID,
			"row_count", 1,
			"actor_object_key", transition.ActorObjectKey,
			"position_x", transition.PositionX,
			"position_y", transition.PositionY,
			"direction", transition.Direction,
			"area_state", transition.AreaState,
			"position_source", transition.PositionSource,
			"body_len", len(transition.Body),
			"source", "current_exe_bodyless_op132_from_confirmed_dungeon_select_context",
			"body_source", "current_exe_sub_1D901D0_typed")
		if err := s.sendGameUpperRawClass(session, currentSceneTransitionMsgID, transition.Body, 0); err != nil {
			session.dungeon.mu.Unlock()
			return err
		}
		clearCurrentDungeonSelectContext(session)
		resetDungeonReturnSceneGates(session)
		session.returnTownFinishLoadingAckOnly = true
		session.confirmedDungeonReturnStatePending = true
		markBackToVillageEnterSelectPending(session)
		s.logGameEvent(session, "game-dungeon-select-back-to-village-committed",
			"char_id", session.selectedCharacterID,
			"town_id", transition.TownID,
			"area_id", transition.AreaID,
			"selector_context_cleared", true,
			"finish_loading_state_rearmed", true,
			"reason", "typed_op24_written_for_confirmed_town_side_dungeon_select_context")
		session.dungeon.mu.Unlock()
		clearCurrentTownSelectorOrigin(session)
		return nil
	}
	defer session.dungeon.mu.Unlock()
	if runtime.townReturnPending {
		session.confirmedDungeonReturnStatePending = false
		resetCurrentDungeonReturnAttempt(runtime)
		s.logGameEvent(session, "game-dungeon-back-to-village-retry",
			"char_id", session.selectedCharacterID,
			"request_msg_id", uint16(dnfenum.CmdPacketBack2Village),
			"reason", "explicit_current_exe_op132_retries_unconfirmed_transition")
	}
	return s.sendCurrentDungeonReturnToTownLocked(
		session,
		runtime,
		transition,
		uint16(dnfenum.CmdPacketBack2Village),
		"current_exe_bodyless_op132",
	)
}

// persistCurrentActiveTutorialExit records the player's explicit decision to
// leave an unfinished tutorial before any town transition packet is built.
// Both the in-dungeon give-up confirmation (op42) and the system-menu
// back-to-village confirmation (op132) use this boundary. Holding dungeon.mu
// keeps an immediate character switch from discarding the runtime before the
// durable character-stat update finishes.
func (s *Service) persistCurrentActiveTutorialExit(
	session *gameSession,
	characterID uint16,
	source string,
) (bool, error) {
	if session == nil || characterID == 0 {
		return false, nil
	}
	session.dungeon.mu.Lock()
	defer session.dungeon.mu.Unlock()

	runtime := session.dungeon.runtime
	if runtime == nil || runtime.Session == nil ||
		session.selectedCharacterID != characterID ||
		!dungeonRuntimeOwnsCharacter(runtime, characterID) ||
		runtime.Session.Snapshot().Run.Status != worldmap.DungeonRunActive ||
		!isPVFTutorialDungeon(runtime) {
		return false, nil
	}
	if runtime.tutorialCompletionPersisted {
		return true, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	previousFlag, err := s.persistCurrentDungeonTutorialCompletion(ctx, characterID)
	cancel()
	if err != nil {
		return false, err
	}
	runtime.tutorialCompletionPersisted = true
	applyCurrentDungeonTutorialCompletionStats(&runtime.Character)
	s.logGameEvent(session, "game-dungeon-tutorial-completion-persisted",
		"char_id", characterID,
		"dungeon_id", runtime.Dungeon.ID,
		"previous_tutorial_completed", previousFlag,
		"tutorial_completed", currentDungeonTutorialCompleteFlag,
		"town_id", newCharacterInitialTownID,
		"area_id", newCharacterInitialAreaID,
		"source", source)
	return true, nil
}

func currentDungeonSelectTownReturnReady(session *gameSession) (bool, string) {
	if session == nil || session.selectedCharacterID == 0 {
		return false, "selected_character_missing"
	}
	if !session.enterSelectDungeonSent || !session.enterSelectDungeonAckSent || !session.enterSelectDungeonContextSent {
		return false, "confirmed_dungeon_select_context_missing"
	}
	session.townMu.Lock()
	readyCharacterID := session.townSceneReadyCharacterID
	originBound := session.townSelectorOriginBound
	origin := session.townSelectorOriginSnapshot
	session.townMu.Unlock()
	if readyCharacterID == 0 || readyCharacterID != session.selectedCharacterID {
		return false, "dungeon_select_context_character_not_town_ready"
	}
	if !originBound || origin.CharacterID != session.selectedCharacterID {
		return false, "dungeon_select_context_town_origin_not_bound"
	}
	if !origin.PositionValid {
		return false, "dungeon_select_context_town_origin_position_missing"
	}
	return true, "current_town_dungeon_select_context_confirmed"
}

func clearCurrentDungeonSelectContext(session *gameSession) {
	if session == nil {
		return
	}
	session.enterSelectDungeonSent = false
	session.enterSelectDungeonAckSent = false
	session.enterSelectDungeonContextSent = false
	session.backToVillageEnterSelectPending = false
}

// markBackToVillageEnterSelectPending records the one scene-finalizer op15
// emitted by the current client after the typed op24 sent for system-menu
// op132. It is not a player request to reopen the dungeon selector.
func markBackToVillageEnterSelectPending(session *gameSession) {
	if session == nil {
		return
	}
	session.backToVillageEnterSelectPending = true
}

func (s *Service) prepareCurrentDungeonTownTransition(
	ctx context.Context,
	characterID uint16,
	sessions ...*gameSession,
) (currentDungeonTownTransition, error) {
	character, err := s.dungeonCharacter(ctx, characterID, sessions...)
	if err != nil {
		return currentDungeonTownTransition{}, err
	}
	townID, areaID, err := currentSceneTransitionLocation(character, true)
	if err != nil {
		return currentDungeonTownTransition{}, err
	}
	row, positionX, positionY, direction, areaState, err := currentDungeonTownTransitionRow(characterID, character.Stats)
	if err != nil {
		return currentDungeonTownTransition{}, err
	}
	body, err := buildCurrentSceneTransitionBody(townID, areaID, []currentSceneTransitionRow{row})
	if err != nil {
		return currentDungeonTownTransition{}, err
	}
	return currentDungeonTownTransition{
		TownID:         townID,
		AreaID:         areaID,
		ActorObjectKey: row.ObjectOrResourceKey,
		PositionX:      positionX,
		PositionY:      positionY,
		Direction:      direction,
		AreaState:      areaState,
		PositionSource: "character_repository_stats",
		Body:           body,
	}, nil
}

func (s *Service) prepareCurrentDungeonTownTransitionForSession(
	ctx context.Context,
	session *gameSession,
	characterID uint16,
) (currentDungeonTownTransition, error) {
	transition, err := s.prepareCurrentDungeonTownTransition(ctx, characterID, session)
	if err != nil {
		return currentDungeonTownTransition{}, err
	}
	session.dungeon.mu.Lock()
	runtime := session.dungeon.runtime
	session.dungeon.mu.Unlock()
	if runtime != nil && runtime.Session != nil {
		if !dungeonRuntimeOwnsCharacter(runtime, characterID) {
			return currentDungeonTownTransition{}, fmt.Errorf(
				"%w: active runtime character owner mismatch",
				errCurrentDungeonTownReturnOriginUnavailable,
			)
		}
		if isPVFTutorialDungeon(runtime) {
			return transition, nil
		}
		return applyCurrentTownPositionSnapshotToTransition(
			transition,
			runtime.townReturnOrigin,
			characterID,
			"current_exe_op35_runtime_origin_snapshot",
		)
	}
	transition, _, err = s.applyCurrentTownPositionToTransition(session, characterID, transition)
	if err != nil {
		return currentDungeonTownTransition{}, err
	}
	if transition.PositionSource == "character_repository_stats" {
		return currentDungeonTownTransition{}, fmt.Errorf(
			"%w: selector return has no matching frozen op35 origin",
			errCurrentDungeonTownReturnOriginUnavailable,
		)
	}
	return transition, nil
}

func currentDungeonTownTransitionRow(
	characterID uint16,
	characterStats map[string]int64,
) (currentSceneTransitionRow, int16, int16, byte, byte, error) {
	if characterID == 0 {
		return currentSceneTransitionRow{}, 0, 0, 0, 0, fmt.Errorf("selected character id is zero")
	}
	if characterStats == nil {
		return currentSceneTransitionRow{}, 0, 0, 0, 0, fmt.Errorf("selected character stats not loaded")
	}
	positionX, hasPositionX := characterStats["pos_x"]
	if !hasPositionX {
		return currentSceneTransitionRow{}, 0, 0, 0, 0, fmt.Errorf("selected character pos_x not loaded")
	}
	positionY, hasPositionY := characterStats["pos_y"]
	if !hasPositionY {
		return currentSceneTransitionRow{}, 0, 0, 0, 0, fmt.Errorf("selected character pos_y not loaded")
	}
	direction, hasDirection := characterStats["direction"]
	if !hasDirection {
		return currentSceneTransitionRow{}, 0, 0, 0, 0, fmt.Errorf("selected character direction not loaded")
	}
	areaState, hasAreaState := characterStats["area_state"]
	if !hasAreaState {
		return currentSceneTransitionRow{}, 0, 0, 0, 0, fmt.Errorf("selected character area_state not loaded")
	}
	if positionX < -1<<15 || positionX > 1<<15-1 {
		return currentSceneTransitionRow{}, 0, 0, 0, 0, fmt.Errorf("selected character pos_x %d is outside i16", positionX)
	}
	if positionY < -1<<15 || positionY > 1<<15-1 {
		return currentSceneTransitionRow{}, 0, 0, 0, 0, fmt.Errorf("selected character pos_y %d is outside i16", positionY)
	}
	if direction < 0 || direction > int64(^byte(0)) {
		return currentSceneTransitionRow{}, 0, 0, 0, 0, fmt.Errorf("selected character direction %d is outside u8", direction)
	}
	if areaState < 0 || areaState > int64(^byte(0)) {
		return currentSceneTransitionRow{}, 0, 0, 0, 0, fmt.Errorf("selected character area_state %d is outside u8", areaState)
	}
	positionX16 := int16(positionX)
	positionY16 := int16(positionY)
	return currentSceneTransitionRow{
		ObjectOrResourceKey: currentSceneActorObjectKey(characterID),
		Value1:              uint16(positionX16),
		Value2:              uint16(positionY16),
		Value3:              byte(direction),
		Value4:              byte(areaState),
	}, positionX16, positionY16, byte(direction), byte(areaState), nil
}

// sendCurrentDungeonReturnToTownLocked starts the typed current-EXE town
// transition. A successful socket write is not a scene acknowledgement, so the
// dungeon runtime remains authoritative until a later town-side client request
// proves that the page transition completed. The caller must hold
// session.dungeon.mu so final completion, death reports, and op132 cannot race.
func (s *Service) sendCurrentDungeonReturnToTownLocked(
	session *gameSession,
	runtime *runtimeDungeonState,
	transition currentDungeonTownTransition,
	requestMsgID uint16,
	source string,
) error {
	if session == nil || runtime == nil || runtime.Session == nil || session.dungeon.runtime != runtime {
		return nil
	}
	snapshot := runtime.Session.Snapshot()
	if snapshot.Run.Status != worldmap.DungeonRunActive && snapshot.Run.Status != worldmap.DungeonRunCompleted {
		s.logGameEvent(session, "game-dungeon-back-to-village-blocked",
			"char_id", session.selectedCharacterID,
			"dungeon_id", snapshot.Run.DungeonID,
			"maze_index", snapshot.Run.MazeIndex,
			"run_status", snapshot.Run.Status,
			"reason", "dungeon_run_not_active")
		return nil
	}
	s.cancelCurrentDungeonDeathReturnLocked(session, runtime, "town_return_transition_started")
	if !runtime.townReturnPending {
		runtime.townReturnPending = true
		runtime.townReturnRequestMsgID = requestMsgID
		runtime.townReturnSource = source
		runtime.townReturnTransition = cloneCurrentDungeonTownTransition(transition)
	}
	transition = runtime.townReturnTransition

	if !runtime.townReturnOp24Sent {
		s.logGameEvent(session, "game-dungeon-back-to-village-op24-send",
			"char_id", session.selectedCharacterID,
			"request_msg_id", requestMsgID,
			"response_msg_id", currentSceneTransitionMsgID,
			"classification", 0,
			"dungeon_id", snapshot.Run.DungeonID,
			"maze_index", snapshot.Run.MazeIndex,
			"room", snapshot.Run.Current.String(),
			"town_id", transition.TownID,
			"area_id", transition.AreaID,
			"row_count", 1,
			"actor_object_key", transition.ActorObjectKey,
			"position_x", transition.PositionX,
			"position_y", transition.PositionY,
			"direction", transition.Direction,
			"area_state", transition.AreaState,
			"position_source", transition.PositionSource,
			"body_len", len(transition.Body),
			"source", source,
			"body_source", "current_exe_sub_1D901D0_typed")
		if err := s.sendGameUpperRawClass(session, currentSceneTransitionMsgID, transition.Body, 0); err != nil {
			return err
		}
		runtime.townReturnOp24Sent = true
		session.confirmedDungeonReturnStatePending = true
		markBackToVillageEnterSelectPending(session)
	}
	s.logGameEvent(session, "game-dungeon-back-to-village-pending",
		"char_id", session.selectedCharacterID,
		"dungeon_id", snapshot.Run.DungeonID,
		"maze_index", snapshot.Run.MazeIndex,
		"room", snapshot.Run.Current.String(),
		"run_status", snapshot.Run.Status,
		"town_id", transition.TownID,
		"area_id", transition.AreaID,
		"position_source", transition.PositionSource,
		"op24_sent", runtime.townReturnOp24Sent,
		"actor_state_sent", false,
		"runtime_retained", session.dungeon.runtime == runtime,
		"reason", "socket_write_is_not_client_scene_commit_and_mode0_is_not_safe_without_town_confirmation")
	return nil
}

// sendCurrentCompletedDungeonReturnToTown reconstructs the selected town actor
// before the typed op24 transition. Live current-EXE evidence shows that op24
// alone is parsed but does not leave the settlement page; the accepted town
// route is action-table -> mode0 object -> equipment-bearing mode1 actor
// state -> typed op24.
// The packet bodies come from the same current character/PVF builders used by
// completed character login. A later real client callback owns the deferred
// full player-state tail.
func (s *Service) sendCurrentCompletedDungeonReturnToTown(
	session *gameSession,
	runtime *runtimeDungeonState,
	transition currentDungeonTownTransition,
	requestMsgID uint16,
	source string,
) error {
	if session == nil || runtime == nil || runtime.Session == nil {
		return nil
	}

	session.dungeon.mu.Lock()
	if session.dungeon.runtime != runtime ||
		runtime.Session.Snapshot().Run.Status != worldmap.DungeonRunCompleted {
		session.dungeon.mu.Unlock()
		return nil
	}
	firstAttempt := !runtime.townReturnPending
	if firstAttempt {
		s.cancelCurrentDungeonDeathReturnLocked(session, runtime, "completed_town_actor_route_started")
		runtime.townReturnPending = true
		runtime.townReturnRequestMsgID = requestMsgID
		runtime.townReturnSource = source
		runtime.townReturnTransition = cloneCurrentDungeonTownTransition(transition)
	}
	transition = cloneCurrentDungeonTownTransition(runtime.townReturnTransition)
	session.dungeon.mu.Unlock()

	if firstAttempt {
		resetDungeonEntrySceneGates(session)
		session.townMu.Lock()
		session.townActorOwnerChannel = currentConnectionTownActorOwnerContext(session)
		session.townMu.Unlock()
		clearCurrentDungeonSelectContext(session)
		clearCurrentTownSelectorOrigin(session)
		s.armCurrentInitialTownRoute(session, session.selectedCharacterID)
		session.sceneBootstrapTailDeferred = true
	}

	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	characterID, characterName, character, hasCharacter := s.selectedCharacterForEnter(ctx, session)
	if characterID == 0 || characterID != session.selectedCharacterID || !hasCharacter {
		return fmt.Errorf(
			"completed dungeon town route character mismatch: selected=%d loaded=%d found=%t",
			session.selectedCharacterID,
			characterID,
			hasCharacter,
		)
	}
	ownerChannel := currentTownActorOwnerContext(session)
	objectBody := s.buildCurrentSceneObjectListBodyForSessionInContext(
		ctx,
		session,
		characterID,
		characterName,
		character,
		hasCharacter,
		ownerChannel,
	)
	adventureSummary := s.currentAccountAdventureGroupSummaryForPacket(ctx, session, character, hasCharacter)
	mode1Body := s.buildCurrentActorBindingMode1BodyForSelectedWithEquipmentInContext(
		ctx,
		session,
		character,
		hasCharacter,
		characterID,
		uint32(adventureSummary.ManageLevel),
		ownerChannel,
		true,
	)

	s.logGameEvent(session, "game-dungeon-completed-town-route-send",
		"char_id", characterID,
		"request_msg_id", requestMsgID,
		"dungeon_id", runtime.Dungeon.ID,
		"town_id", transition.TownID,
		"area_id", transition.AreaID,
		"position_x", transition.PositionX,
		"position_y", transition.PositionY,
		"position_source", transition.PositionSource,
		"source", source,
		"sequence", "op376_then_msg2_mode0_then_equipment_bearing_mode1_then_op21_op574_then_typed_op24_then_op21_op574_actor_ready_refresh",
		"body_source", "current_exe_completed_login_builders_and_persisted_town_transition")

	session.townMu.Lock()
	err := s.sendCurrentTownActorRoutePacketsLocked(
		session,
		characterID,
		transition.TownID,
		transition.AreaID,
		objectBody,
		mode1Body,
		transition.Body,
	)
	routeStage := session.initialTownRouteStage
	questSnapshotsSeeded := session.initialTownQuestSnapshotsSent
	session.townMu.Unlock()
	if err != nil {
		return err
	}
	if routeStage < currentInitialTownRouteTransitionSent {
		return nil
	}
	// Match the current 90CN actor-ready boundary: the pre-op24 pair seeds the
	// task manual, and this second repository/PVF pair refreshes visible NPC
	// task markers after the town transition has been committed by the route.
	// This is a state snapshot only; it never accepts or completes a quest.
	if questSnapshotsSeeded {
		if err := s.sendCurrentAcceptableQuestListForSession(
			session,
			source+"_post_op24_actor_ready_refresh",
		); err != nil {
			return err
		}
	}

	session.dungeon.mu.Lock()
	if session.dungeon.runtime == runtime {
		runtime.townReturnOp24Sent = true
		s.cancelCurrentDungeonDeathReturnLocked(session, runtime, "completed_town_actor_route_committed")
		s.cancelCurrentDungeonCardAutoFlipLocked(session, runtime, "completed_town_actor_route_committed")
		session.dungeon.runtime = nil
	}
	detached := session.dungeon.runtime != runtime
	session.dungeon.mu.Unlock()
	if detached {
		if err := s.switchCurrentPetGrowthClock(session, currentPetGrowthClockTown, s.gameplayNow(), source+"_after_completed_town_route"); err != nil {
			s.logGameEvent(session, "game-pet-growth-clock-start-deferred",
				"char_id", characterID,
				"mode", currentPetGrowthClockTown.String(),
				"source", source,
				"error", err)
		}
	}

	s.logGameEvent(session, "game-dungeon-completed-town-route-committed",
		"char_id", characterID,
		"request_msg_id", requestMsgID,
		"dungeon_id", runtime.Dungeon.ID,
		"town_id", transition.TownID,
		"area_id", transition.AreaID,
		"route_stage", routeStage,
		"runtime_detached", detached,
		"scene_tail_deferred", session.sceneBootstrapTailDeferred,
		"next_owner", "real_client_scene_callback_then_deferred_town_player_state")
	return nil
}

func cloneCurrentDungeonTownTransition(transition currentDungeonTownTransition) currentDungeonTownTransition {
	transition.Body = append([]byte(nil), transition.Body...)
	return transition
}

func resetCurrentDungeonReturnAttempt(runtime *runtimeDungeonState) {
	if runtime == nil {
		return
	}
	runtime.townReturnPending = false
	runtime.townReturnOp24Sent = false
	runtime.townReturnRequestMsgID = 0
	runtime.townReturnSource = ""
	runtime.townReturnTransition = currentDungeonTownTransition{}
}

// cancelCurrentDungeonReturnAfterDungeonEvidenceLocked cancels an unconfirmed
// return when a valid dungeon-only request proves that the client never left
// the dungeon. An unrelated death or move request must not trigger another
// unsolicited op24. The caller must hold session.dungeon.mu.
func (s *Service) cancelCurrentDungeonReturnAfterDungeonEvidenceLocked(
	session *gameSession,
	runtime *runtimeDungeonState,
	evidence string,
) {
	if session == nil || runtime == nil || session.dungeon.runtime != runtime || !runtime.townReturnPending {
		return
	}
	requestMsgID := runtime.townReturnRequestMsgID
	source := runtime.townReturnSource
	transition := runtime.townReturnTransition
	resetCurrentDungeonReturnAttempt(runtime)
	session.confirmedDungeonReturnStatePending = false
	s.logGameEvent(session, "game-dungeon-back-to-village-cancelled",
		"char_id", session.selectedCharacterID,
		"request_msg_id", requestMsgID,
		"town_id", transition.TownID,
		"area_id", transition.AreaID,
		"source", source,
		"evidence", evidence,
		"runtime_retained", session.dungeon.runtime == runtime,
		"reason", "accepted_dungeon_request_proves_transition_uncommitted_no_unsolicited_retry")
}

// commitPendingDungeonReturnForSceneRequest clears the old dungeon owner only
// after the client sends a town-side dungeon-selection request. Until that
// positive scene evidence exists, op39/op45 must continue to use the retained
// runtime so real deaths and room state are not lost.
func (s *Service) commitPendingDungeonReturnForSceneRequest(session *gameSession, source string) error {
	if session == nil {
		return nil
	}
	session.dungeon.mu.Lock()
	runtime := session.dungeon.runtime
	if runtime == nil || runtime.Session == nil || !runtime.townReturnPending {
		session.dungeon.mu.Unlock()
		return s.ensureCurrentConfirmedDungeonReturnPlayerState(
			session,
			source+"_resume_confirmed_return",
		)
	}
	snapshot := runtime.Session.Snapshot()
	committedStatus := snapshot.Run.Status
	if snapshot.Run.Status == worldmap.DungeonRunActive {
		if err := runtime.Session.Abandon(); err != nil {
			session.dungeon.mu.Unlock()
			return err
		}
		committedStatus = worldmap.DungeonRunAbandoned
	}
	if err := s.switchCurrentPetGrowthClock(session, currentPetGrowthClockTown, s.gameplayNow(), source+"_confirmed_town_scene"); err != nil {
		s.logGameEvent(session, "game-pet-growth-clock-start-deferred",
			"char_id", session.selectedCharacterID,
			"mode", currentPetGrowthClockTown.String(),
			"source", source,
			"error", err)
	}
	s.cancelCurrentDungeonDeathReturnLocked(session, runtime, "town_scene_transition_committed")
	s.cancelCurrentDungeonCardAutoFlipLocked(session, runtime, "town_scene_transition_committed")
	session.dungeon.runtime = nil
	resetDungeonReturnSceneGates(session)
	session.returnTownFinishLoadingAckOnly = true
	session.confirmedDungeonReturnStatePending = true
	townID := runtime.townReturnTransition.TownID
	areaID := runtime.townReturnTransition.AreaID
	session.dungeon.mu.Unlock()
	s.logGameEvent(session, "game-dungeon-back-to-village-committed",
		"char_id", session.selectedCharacterID,
		"dungeon_id", snapshot.Run.DungeonID,
		"maze_index", snapshot.Run.MazeIndex,
		"room", snapshot.Run.Current.String(),
		"run_status", committedStatus,
		"town_id", townID,
		"area_id", areaID,
		"source", source,
		"reason", "client_town_side_scene_request_confirmed_transition")
	return s.ensureCurrentConfirmedDungeonReturnPlayerState(
		session,
		source+"_after_confirmed_typed_op24",
	)
}

// ensureCurrentConfirmedDungeonReturnPlayerState consumes the direct
// dungeon-return op24 generation only after a real town-side request confirms
// that the client accepted it. The dungeon owner is detached before this
// function runs: op105 can consult dungeon/session state, so no dungeon lock
// may be held while the actor/HUD chain is written.
func (s *Service) ensureCurrentConfirmedDungeonReturnPlayerState(
	session *gameSession,
	source string,
) error {
	if session == nil || session.selectedCharacterID == 0 {
		return nil
	}
	session.dungeon.mu.Lock()
	pending := session.confirmedDungeonReturnStatePending
	session.dungeon.mu.Unlock()
	if !pending {
		return nil
	}

	characterID := session.selectedCharacterID
	ownerChannel := currentConnectionTownActorOwnerContext(session)
	session.townMu.Lock()
	if session.selectedCharacterID != characterID {
		session.townMu.Unlock()
		return fmt.Errorf(
			"confirmed dungeon return character changed before actor rebuild: armed=%d selected=%d",
			characterID,
			session.selectedCharacterID,
		)
	}
	session.townActorOwnerChannel = ownerChannel
	session.townMu.Unlock()

	session.townPostTransition.mu.Lock()
	stage := session.townPostTransition.stage
	armedCharacterID := session.townPostTransition.characterID
	session.townPostTransition.mu.Unlock()
	switch {
	case stage == currentTownPostTransitionIdle || stage == currentTownPostTransitionComplete:
		s.armCurrentTownPostTransitionPlayerState(
			session,
			source+"_arm_direct_return_generation",
		)
	case armedCharacterID != characterID:
		return fmt.Errorf(
			"confirmed dungeon return actor rebuild owner changed: armed=%d selected=%d stage=%d",
			armedCharacterID,
			characterID,
			stage,
		)
	}
	if err := s.sendCurrentTownPostTransitionPlayerState(
		session,
		source+"_consume_direct_return_generation",
	); err != nil {
		return err
	}

	session.townMu.Lock()
	if session.selectedCharacterID == characterID {
		session.townSceneReadyCharacterID = characterID
	}
	session.townMu.Unlock()
	session.dungeon.mu.Lock()
	if session.selectedCharacterID == characterID {
		session.confirmedDungeonReturnStatePending = false
	}
	session.dungeon.mu.Unlock()
	s.logGameEvent(session, "game-dungeon-back-to-village-player-state-rebuilt",
		"source", source,
		"char_id", characterID,
		"owner_context", ownerChannel,
		"sequence", "mode0_mode1_op105_optional_op102_op37_op30_op19_op120")
	return nil
}

func resetDungeonReturnSceneGates(session *gameSession) {
	if session == nil {
		return
	}
	session.runtimeAfterBlacklistSent = false
	session.runtimeFinishLoadingGateSent = false
	session.fpsFinishLoadingGateSent = false
	session.selectPreviewActorRemoved = false
	session.preDungeonContextPlayerStateSent = false
	session.deferredDungeonUserStateObjectKey = 0
	session.currentFinishLoadingStateSent = false
	session.currentFinishLoadingCompletionSent = false
	session.postFinishLoadingPlayerStateSent = false
	session.returnTownFinishLoadingAckOnly = false
}

func resetDungeonEntrySceneGates(session *gameSession) {
	if session == nil {
		return
	}
	session.dungeon.mu.Lock()
	session.confirmedDungeonReturnStatePending = false
	session.dungeon.mu.Unlock()
	session.townMu.Lock()
	resetCurrentTownPostTransitionPlayerState(session)
	session.initialTownRouteCharacterID = 0
	session.initialTownRouteStage = currentInitialTownRouteIdle
	session.initialTownLocationNotificationsSent = false
	session.initialTownQuestSnapshotsSent = false
	session.initialTownSkillInfoPrepared = false
	session.initialTownSkillInfoSent = false
	session.initialTownSkillInfo = currentSceneSkillInfoProjection{}
	session.initialTownLegacySceneReadyAccepted = false
	session.initialTownAdventureOverheadRefreshSent = false
	session.initialTownCombatPowerAffixesSent = false
	session.townSceneReadyCharacterID = 0
	session.townActorOwnerChannel = currentSceneObjectContext
	session.townMu.Unlock()
	session.sceneBootstrapTailDeferred = false
	session.sceneBootstrapTailSent = false
	resetCurrentDeferredSelectSceneTailProgress(session)
	session.runtimeAfterBlacklistSent = false
	session.runtimeFinishLoadingGateSent = false
	session.fpsFinishLoadingGateSent = false
	session.selectedUserInfoRefreshSent = false
	session.selectedUserInfoMode3Sent = false
	session.currentSceneObjectListSent = false
	session.selectedItemListRefreshSent = false
	session.selectedItemListBootstrapCharacterID = 0
	session.selectedEquipmentUpdateSent = false
	session.selectedCreatureStateTableSent = false
	session.selectedRentalWalletStateSent = false
	session.expertJobInfoCharacterID = 0
	session.selectPreviewActorRemoved = false
	session.preDungeonContextPlayerStateSent = false
	session.postStartMapPlayerStateSent = false
	session.deferredDungeonUserStateObjectKey = 0
	session.currentFinishLoadingStateSent = false
	session.currentFinishLoadingCompletionSent = false
	session.postFinishLoadingPlayerStateSent = false
	session.returnTownFinishLoadingAckOnly = false
}
