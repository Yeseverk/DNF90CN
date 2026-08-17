package dnfbridge

import (
	"context"
	"encoding/binary"
	"fmt"

	"longheng.io/server/internal/modules/dnf/dnfenum"
)

func (s *Service) sendEnterSelectDungeonState(
	session *gameSession,
	source string,
	transport bool,
	requestDriven bool,
) error {
	if !transport && requestDriven && session != nil {
		session.townMu.Lock()
		townTransportFinalizer := session.townTransportEnterSelectPending
		session.townTransportEnterSelectPending = false
		session.townMu.Unlock()
		if townTransportFinalizer {
			s.logGameEvent(session, "game-upper-enter-select-dungeon-after-town-transport-suppressed",
				"source", source,
				"char_id", session.selectedCharacterID,
				"reason", "current_exe_town_transport_scene_finalizer_op15_is_not_dungeon_selector_intent",
				"response", "none")
			return nil
		}
	}
	if !transport && requestDriven && session != nil && session.backToVillageEnterSelectPending {
		// A typed op24 from system-menu op132 is followed by a client-owned
		// scene-finalizer op15. It is not a fresh selector request.
		if err := s.commitPendingDungeonReturnBeforeTownEnterSelect(session, source+"_after_back_to_village_scene_finalizer"); err != nil {
			return err
		}
		session.backToVillageEnterSelectPending = false
		if err := s.rebuildRuntimePartyTownCoPresenceAfterReturn(
			session,
			source+"_after_back_to_village_scene_finalizer",
		); err != nil {
			return err
		}
		s.logGameEvent(session, "game-upper-enter-select-dungeon-after-back-to-village-suppressed",
			"source", source,
			"char_id", session.selectedCharacterID,
			"reason", "current_exe_op132_scene_finalizer_op15_is_not_selector_intent",
			"response", "none")
		return nil
	}
	if !transport && requestDriven {
		if err := s.commitPendingDungeonReturnBeforeTownEnterSelect(session, source); err != nil {
			return err
		}
		retired, err := s.retireCompletedDungeonForTownSelect(session)
		if err != nil {
			s.logGameEvent(session, "game-upper-enter-select-dungeon-completed-runtime-retire-blocked",
				"source", source,
				"char_id", session.selectedCharacterID,
				"reason", "verified_town_origin_unavailable",
				"error", err)
		} else if retired {
			s.logGameEvent(session, "game-upper-enter-select-dungeon-completed-runtime-retired",
				"source", source,
				"char_id", session.selectedCharacterID,
				"reason", "settlement_page_select_other_dungeon_uses_current_op15_op27_route")
		}
	}
	if requestDriven && session.enterSelectDungeonAckSent {
		s.logGameEvent(session, "game-upper-enter-select-dungeon-duplicate-ack-replay",
			"source", source,
			"request_driven", true,
			"packet_count", 2,
			"reason", "current_client_retries_op15_after_post_start_map_state")
	}
	if !requestDriven && session.enterSelectDungeonSent {
		s.logGameEvent(session, "game-upper-enter-select-dungeon-duplicate-skipped", "source", source, "request_driven", false)
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	charID, _, character, hasCharacter := s.selectedCharacterForEnter(ctx, session)

	contextReady := false
	contextReadyReason := "legacy_or_proactive_path"
	if !transport && requestDriven {
		contextReady, contextReadyReason = s.currentTownEnterSelectReady(session)
	}
	packetCount := 2
	if contextReady {
		packetCount++
	}
	s.logPacketEvent("game-enter-select-dungeon-state-send",
		"conn_id", session.connID,
		"channel_id", session.channel.ID,
		"source", source,
		"char_id", charID,
		"scene_object_key", currentSceneActorObjectKey(charID),
		"transport", transport,
		"request_driven", requestDriven,
		"enter_msg_id", uint16(dnfenum.CmdPacketEnterSelectDungeon),
		"fatigue_msg_id", currentFatigueMsgID,
		"packet_count", packetCount,
		"town_context_ready", contextReady,
		"town_context_ready_reason", contextReadyReason,
		"reason", "current_exe_op15_ack_op36_fatigue_and_guarded_op27_enter_select_page")

	if transport {
		enterBody := buildEnterSelectDungeonBody(charID)
		if err := s.sendGame(session, byte(dnfenum.GameCmdCommand), uint16(dnfenum.CmdPacketEnterSelectDungeon), enterBody); err != nil {
			return err
		}
	} else if err := s.sendGameUpperSuccess(session, uint16(dnfenum.CmdPacketEnterSelectDungeon), buildEnterSelectDungeonAckBody()); err != nil {
		return err
	}
	fatigueBody := buildCurrentFatigueBody(character, hasCharacter)
	s.logGameEvent(session, "game-enter-select-dungeon-fatigue-send",
		"char_id", charID,
		"msg_id", currentFatigueMsgID,
		"body_len", len(fatigueBody),
		"fatigue_used", binary.LittleEndian.Uint16(fatigueBody[0:2]),
		"fatigue_limit", binary.LittleEndian.Uint16(fatigueBody[2:4]),
		"actor_aux", binary.LittleEndian.Uint16(fatigueBody[4:6]),
		"display_used", binary.LittleEndian.Uint16(fatigueBody[6:8]),
		"actor_extra", binary.LittleEndian.Uint16(fatigueBody[8:10]),
		"body_source", "current_exe_sub_1D7ABE0_fatigue_five_u16")
	if transport {
		if err := s.sendGame(session, byte(dnfenum.GameCmdCommand), currentFatigueMsgID, fatigueBody); err != nil {
			return err
		}
	} else if err := s.sendGameUpperRawClass(session, currentFatigueMsgID, fatigueBody, 0); err != nil {
		return err
	}
	session.enterSelectDungeonSent = true
	if requestDriven {
		session.enterSelectDungeonAckSent = true
	}
	if !transport && requestDriven {
		if err := s.sendCurrentTownEnterSelectContext(session, source+"_after_op15_ack_fatigue"); err != nil {
			return err
		}
		if contextReady {
			if err := s.synchronizeRuntimePartyMembersEnterSelect(session, source+"_party_members"); err != nil {
				return err
			}
		}
	}
	return nil
}

// synchronizeRuntimePartyMembersEnterSelect moves every other same-area
// member into the selector after any member opens it. Selecting a dungeon map
// is a party scene transition, not a leader-only action: the canonical party
// leader remains state.UserID, while initiator identifies the client that
// triggered the transition. This prevents the former partial transition where
// a non-leader opened the selector and the rest of the party stayed in town.
func (s *Service) synchronizeRuntimePartyMembersEnterSelect(initiator *gameSession, source string) error {
	if initiator == nil || initiator.selectedCharacterID == 0 || s.onlinePlayers == nil {
		return nil
	}
	state := runtimePartyStateSnapshot(initiator)
	if state.PartyID <= 0 || state.UserID == 0 {
		return nil
	}
	for _, member := range runtimePartyMembers(state) {
		if member.UserID == 0 || member.UserID == initiator.selectedCharacterID ||
			!s.onlinePlayers.PeerInSameArea(initiator.selectedCharacterID, member.UserID) {
			continue
		}
		follower, ok := s.onlineGameSession(member.UserID)
		if !ok || follower == initiator {
			continue
		}
		followerState := runtimePartyStateSnapshot(follower)
		if followerState.PartyID != state.PartyID || followerState.UserID != state.UserID {
			continue
		}
		fanoutCtx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
		err := s.callGameSession(fanoutCtx, follower, "runtime-party-enter-select", func() error {
			_, prepareErr := s.prepareRuntimePartyFollowerEnterSelect(
				follower,
				state.UserID,
				state.PartyID,
				source,
			)
			return prepareErr
		})
		cancel()
		if err != nil {
			return fmt.Errorf("synchronize party member %d dungeon selector: %w", member.UserID, err)
		}
	}
	return nil
}

// prepareRuntimePartyFollowerEnterSelect installs the complete native selector
// transition for a same-area ordinary-party member. The old 86JP client could
// enter this state without an op15 response, but current NoPack uses the op15
// success body to install the selectable dungeon page. The passive member must
// therefore receive that same successful selector context after its town actor
// is detached; fatigue/op27 alone leaves a blank, non-interactive world map.
func (s *Service) prepareRuntimePartyFollowerEnterSelect(
	follower *gameSession,
	leaderCharacterID uint16,
	partyID int,
	source string,
) (bool, error) {
	if follower == nil || follower.selectedCharacterID == 0 {
		return false, nil
	}
	if follower.enterSelectDungeonContextSent && follower.enterSelectDungeonSent {
		return true, nil
	}
	ready, reason := s.currentTownEnterSelectReady(follower)
	if !ready {
		s.logGameEvent(follower, "game-party-enter-select-follower-deferred",
			"source", source,
			"leader_char_id", leaderCharacterID,
			"party_id", partyID,
			"reason", reason)
		return false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	charID, _, character, hasCharacter := s.selectedCharacterForEnter(ctx, follower)
	if charID == 0 {
		charID = follower.selectedCharacterID
	}
	follower.townMu.Lock()
	townPosition := follower.townPositionSnapshot
	follower.townMu.Unlock()
	areaState := byte(currentActorDefaultUserStateBits)
	if hasCharacter {
		if value := numericCharacterStatValue(character, "area_state"); value >= 0 && value <= 0xff {
			areaState = byte(value)
		}
	}
	areaBody := buildCurrentTownUserAreaNotificationBody(
		currentSceneActorObjectKey(charID),
		townPosition.TownID,
		0xff,
		townPosition.PositionX,
		townPosition.PositionY,
		townPosition.MovementCode,
		areaState,
	)
	if err := s.sendCurrentSceneFixedClass0Packet(
		follower,
		currentTownUserAreaNotificationMsgID,
		areaBody,
		source+"_passive_party_area_detach",
	); err != nil {
		return false, err
	}
	if err := s.sendCurrentPreDungeonContextPlayerState(follower, source+"_after_area_detach"); err != nil {
		return false, err
	}
	userStateBody, err := buildCurrentDungeonUserStateBody(currentSceneActorObjectKey(charID))
	if err != nil {
		return false, err
	}
	if err := s.sendGameUpperRawClass(follower, uint16(dnfenum.CmdPacketExit), userStateBody, 0); err != nil {
		return false, err
	}
	partyState := runtimePartyStateSnapshot(follower)
	if partyState.PartyID != partyID || partyState.UserID != leaderCharacterID {
		return false, nil
	}
	// mode0/mode1/op3 replace the follower's scene actor owner. Reinstall the
	// authoritative op9 party roster before opening the selector, while keeping
	// the already-established dynamic UDP peer link intact (no repeated op11).
	if err := s.sendRuntimePartyRosterLocal(
		follower,
		partyState,
		source+"_after_passive_actor_rebuild",
	); err != nil {
		return false, err
	}
	if err := s.sendGameUpperSuccess(
		follower,
		uint16(dnfenum.CmdPacketEnterSelectDungeon),
		buildEnterSelectDungeonAckBody(),
	); err != nil {
		return false, err
	}
	fatigueBody := buildCurrentFatigueBody(character, hasCharacter)
	if err := s.sendGameUpperRawClass(follower, currentFatigueMsgID, fatigueBody, 0); err != nil {
		return false, err
	}
	if err := s.sendCurrentDungeonPermissionSnapshot(ctx, follower, source+"_after_fatigue"); err != nil {
		return false, err
	}
	if !follower.enterSelectDungeonContextSent {
		if err := s.sendCurrentTownEnterSelectContext(follower, source+"_after_fatigue"); err != nil {
			return false, err
		}
	}
	if !follower.enterSelectDungeonContextSent {
		return false, nil
	}
	follower.enterSelectDungeonSent = true
	follower.enterSelectDungeonAckSent = true
	s.logGameEvent(follower, "game-party-enter-select-follower-synchronized",
		"source", source,
		"leader_char_id", leaderCharacterID,
		"char_id", charID,
		"party_id", partyID,
		"sequence", "leader_selector_then_follower_op23_area_ff_op2_mode0_mode1_op3_op9_roster_without_op11_rehandshake_class1_op15_success_op36_optional_op5_op27")
	return true, nil
}
