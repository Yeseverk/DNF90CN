package dnfbridge

import (
	"context"

	"longheng.io/server/internal/modules/dnf/dnfenum"
)

func (s *Service) armCurrentInitialTownRoute(session *gameSession, characterID uint16) {
	if session == nil || characterID == 0 {
		return
	}
	session.townMu.Lock()
	resetCurrentTownPostTransitionPlayerState(session)
	session.initialTownRouteCharacterID = characterID
	session.initialTownRouteStage = currentInitialTownRouteArmed
	session.initialTownActorSceneSnapshotSent = false
	session.initialTownLocationNotificationsSent = false
	session.initialTownQuestSnapshotsSent = false
	session.initialTownSkillInfoPrepared = false
	session.initialTownSkillInfoSent = false
	session.initialTownSkillInfo = currentSceneSkillInfoProjection{}
	session.initialTownLegacySceneReadyAccepted = false
	session.initialTownAdventureOverheadRefreshSent = false
	session.initialTownCombatPowerAffixesSent = false
	session.crystalContractMu.Lock()
	session.crystalContractTownUIReadyStateSent = false
	session.crystalContractMu.Unlock()
	session.auraSkinMu.Lock()
	session.auraSkinTownUIReadyStateSent = false
	session.auraSkinMu.Unlock()
	session.townMu.Unlock()
	s.logGameEvent(session, "game-initial-town-route-armed",
		"char_id", characterID,
		"tutorial_route_index", currentSelectAckPage1RouteIndex,
		"expected_client_progress", currentInitialTownProgress,
		"reason", "wait_for_current_exe_page_controller_before_selected_actor_and_town_transition")
}

func (s *Service) handleCurrentInitialTownProgress(
	session *gameSession,
	progress uint32,
) error {
	return s.sendCurrentInitialTownRoute(session, progress, true, false)
}

// resumeCurrentInitialTownRouteAfterReturnSelect owns the selector re-entry
// boundary. The current client emits progress36 only on the first selector-page
// controller lifecycle. After a successful op7 module return it sends op8 and a
// new op4, but does not repeat op143. Resume the already-proved town transition
// after the new op4 ACK without inventing an op143 request or success response.
func (s *Service) resumeCurrentInitialTownRouteAfterReturnSelect(session *gameSession) error {
	return s.sendCurrentInitialTownRoute(session, currentInitialTownProgress, false, true)
}

// resumeCurrentInitialTownRouteAfterSelectHeartbeat covers the current client
// path where a completed character reaches the town loading page but never
// emits the old op143/progress36 page-controller request. The heartbeat is not
// acknowledged as op143; it only lets the server resume the already armed town
// actor/transition stream that select-character prepared.
func (s *Service) resumeCurrentInitialTownRouteAfterSelectHeartbeat(session *gameSession, source string) error {
	if session == nil || session.selectedCharacterID == 0 {
		return nil
	}
	session.townMu.Lock()
	routeCharacterID := session.initialTownRouteCharacterID
	routeStage := session.initialTownRouteStage
	session.townMu.Unlock()
	if routeCharacterID == 0 ||
		routeCharacterID != session.selectedCharacterID ||
		routeStage < currentInitialTownRouteArmed ||
		routeStage >= currentInitialTownRouteTransitionSent {
		return nil
	}
	s.logGameEvent(session, "game-initial-town-route-heartbeat-resume",
		"source", source,
		"char_id", session.selectedCharacterID,
		"route_char_id", routeCharacterID,
		"route_stage", routeStage,
		"progress", currentInitialTownProgress,
		"op143_ack_sent", false,
		"reason", "client_reached_town_loading_page_without_progress36")
	return s.sendCurrentInitialTownRoute(session, currentInitialTownProgress, false, false)
}

func (s *Service) sendCurrentInitialTownRoute(
	session *gameSession,
	progress uint32,
	acknowledgeProgress bool,
	requireReturnSelectReentry bool,
) error {
	if session == nil {
		return nil
	}

	session.dungeon.mu.Lock()
	hasDungeonRuntime := session.dungeon.runtime != nil
	session.dungeon.mu.Unlock()
	if hasDungeonRuntime {
		s.logGameEvent(session, "game-initial-town-route-blocked",
			"char_id", session.selectedCharacterID,
			"progress", progress,
			"reason", "active_dungeon_runtime_owns_op143")
		return nil
	}

	session.townMu.Lock()
	defer session.townMu.Unlock()
	if requireReturnSelectReentry && !session.returnSelectTownReentryPending {
		s.logGameEvent(session, "game-initial-town-route-deferred",
			"char_id", session.selectedCharacterID,
			"route_char_id", session.initialTownRouteCharacterID,
			"progress", progress,
			"route_stage", session.initialTownRouteStage,
			"reason", "return_select_reentry_not_owned_by_server_op7")
		return nil
	}
	if progress != currentInitialTownProgress ||
		session.initialTownRouteStage < currentInitialTownRouteArmed ||
		session.initialTownRouteCharacterID == 0 ||
		session.initialTownRouteCharacterID != session.selectedCharacterID {
		s.logGameEvent(session, "game-initial-town-route-deferred",
			"char_id", session.selectedCharacterID,
			"route_char_id", session.initialTownRouteCharacterID,
			"progress", progress,
			"route_stage", session.initialTownRouteStage,
			"reason", "progress36_not_owned_by_completed_select_route")
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	characterID, characterName, character, hasCharacter := s.selectedCharacterForEnter(ctx, session)
	if characterID != session.initialTownRouteCharacterID || !hasCharacter ||
		selectedCharacterStartsInTutorial(character, hasCharacter) {
		s.logGameEvent(session, "game-initial-town-route-blocked",
			"char_id", session.selectedCharacterID,
			"route_char_id", session.initialTownRouteCharacterID,
			"loaded_char_id", characterID,
			"has_character", hasCharacter,
			"tutorial_completed", hasPersistedDungeonTutorialCompletion(character),
			"progress", progress,
			"reason", "fresh_character_state_does_not_own_completed_select_route")
		return nil
	}

	// Character-list login is intentionally different from a dungeon spawn:
	// every completed character is placed at the real Seria gate from the
	// runtime town PVF.  A tutorial-pending character returned above never
	// reaches this branch, so its first in-dungeon position remains client/PVF
	// owned by that dungeon map's [dungeon start area].
	character, townID, areaID, row, mapPath, err := s.currentCharacterListLoginTransition(ctx, session, characterID, character)
	if err != nil {
		s.logGameEvent(session, "game-initial-town-route-blocked",
			"char_id", session.selectedCharacterID,
			"progress", progress,
			"reason", "seria_character_list_login_location_unavailable_or_not_persisted",
			"error", err)
		return nil
	}
	transitionBody, err := buildCurrentSceneTransitionBody(townID, areaID, []currentSceneTransitionRow{row})
	if err != nil {
		return err
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
	adventureLevel := uint32(adventureSummary.ManageLevel)
	mode1Body := s.buildCurrentActorBindingMode1BodyForSelectedInContext(
		ctx,
		session,
		character,
		hasCharacter,
		characterID,
		adventureLevel,
		ownerChannel,
	)

	acceptedEvent := "game-initial-town-route-progress36-accepted"
	sequence := "op143_ack_then_op376_then_msg2_mode0_then_actor_binding_mode1_then_full_player_state_then_typed_op24"
	trigger := "client_op143_progress36"
	if requireReturnSelectReentry {
		acceptedEvent = "game-initial-town-route-return-select-reentry-accepted"
		sequence = "op376_then_msg2_mode0_then_actor_binding_mode1_then_full_player_state_then_typed_op24"
		trigger = "server_owned_op7_then_client_op8_then_client_op4"
	}
	s.logGameEvent(session, acceptedEvent,
		"char_id", characterID,
		"progress", progress,
		"route_stage", session.initialTownRouteStage,
		"trigger", trigger,
		"op143_ack_sent", acknowledgeProgress,
		"tutorial_completed", hasPersistedDungeonTutorialCompletion(character),
		"town_id", townID,
		"area_id", areaID,
		"map_path", mapPath,
		"position_x", row.Value1,
		"position_y", row.Value2,
		"direction", row.Value3,
		"area_state", row.Value4,
		"adventure_total_point", adventureSummary.TotalPoint,
		"adventure_manage_level", adventureSummary.ManageLevel,
		"owner_server", 0,
		"owner_channel", ownerChannel,
		"sequence", sequence,
		"evidence_boundary", "current_exe_live_first_select_and_return_select_reentry_lifecycle")

	// The current op143 success reader consumes success plus a reward count.
	// A duplicate progress-36 request receives another response, but the
	// one-shot selected-object packets below resume from their committed stage.
	if acknowledgeProgress {
		if err := s.sendGameUpperSuccess(
			session,
			uint16(dnfenum.CmdPacketChangeTutorialFlag),
			[]byte{0},
		); err != nil {
			return err
		}
	}
	if session.initialTownRouteStage >= currentInitialTownRouteTransitionSent {
		duplicateEvent := "game-initial-town-route-duplicate-ack-only"
		if requireReturnSelectReentry {
			session.returnSelectTownReentryPending = false
			duplicateEvent = "game-initial-town-route-return-select-reentry-duplicate-skipped"
		}
		s.logGameEvent(session, duplicateEvent,
			"char_id", characterID,
			"progress", progress,
			"route_stage", session.initialTownRouteStage)
		return nil
	}

	session.townActorOwnerChannel = ownerChannel
	if err := s.sendCurrentInitialTownActorRoutePacketsLocked(
		session,
		characterID,
		townID,
		areaID,
		row,
		objectBody,
		mode1Body,
		transitionBody,
	); err != nil {
		return err
	}
	if requireReturnSelectReentry {
		session.returnSelectTownReentryPending = false
	}

	s.logGameEvent(session, "game-initial-town-route-transition-sent",
		"char_id", characterID,
		"progress", progress,
		"town_id", townID,
		"area_id", areaID,
		"map_path", mapPath,
		"row_count", 1,
		"trigger", trigger,
		"op143_ack_sent", acknowledgeProgress,
		"route_stage", session.initialTownRouteStage,
		"next_owner", "current_client_scene_request_or_finish_loading")
	return nil
}
