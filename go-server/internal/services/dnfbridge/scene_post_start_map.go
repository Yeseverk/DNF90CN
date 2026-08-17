package dnfbridge

import (
	"context"
	"fmt"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

const (
	currentSceneOverseerPageCount      = 5
	currentDungeonPlayerUserState byte = 1
)

func buildCurrentDungeonUserStateBody(objectKey uint16) ([]byte, error) {
	if objectKey == 0 {
		return nil, fmt.Errorf("current dungeon user state requires a nonzero object key")
	}
	var writer packetWriter
	writer.writeByte(1)
	writer.writeUint16(objectKey)
	writer.writeByte(currentDungeonPlayerUserState)
	return writer.bytes(), nil
}

func (s *Service) sendCurrentSceneOverseerPages(ctx context.Context, session *gameSession) error {
	for page := 0; page < currentSceneOverseerPageCount; page++ {
		fallback := buildCurrentRequestOverseerBody(uint32(page))
		body := s.buildCurrentRequestOverseerBodyForSession(ctx, session, fallback)
		if err := s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketRequestOverseer), body, 0); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) sendCurrentSelectPreviewObjectState(
	ctx context.Context,
	session *gameSession,
	charID uint16,
	charName string,
	character dnfrepo.CharacterRecord,
	hasCharacter bool,
	source string,
) error {
	if session == nil || session.selectPreviewObjectStateSent {
		return nil
	}
	if charID == 0 {
		return fmt.Errorf("select preview object state requires selected character")
	}
	objectKey := currentSceneActorObjectKey(charID)
	s.logPacketEvent("game-upper-select-preview-object-state-send",
		"conn_id", session.connID,
		"channel_id", session.channel.ID,
		"source", source,
		"char_id", charID,
		"object_key", objectKey,
		"sequence", "op358_pages_action_table_msg2_mode0_actor_binding_mode1_op359_op356_op124",
		"lifetime", "removed_by_op9_kind3_after_validated_op16_before_final_actor_rebind",
		"mode1", "actor_binding_only_before_op359_full_equipment_after_op29")

	if err := s.sendCurrentSceneOverseerPages(ctx, session); err != nil {
		return err
	}
	if err := s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketPVPMissionHpPercent), buildCurrentActionTableStateBody(), 0); err != nil {
		return err
	}
	objectBody := s.buildCurrentSceneObjectListBodyForSession(ctx, session, charID, charName, character, hasCharacter)
	objectPacket := csharpSelectInitPacket{
		class: 0,
		msgID: uint16(dnfenum.CmdPacketSetUDPIPPort),
		kind:  csharpCurrentSceneObjectListKind,
	}
	if err := s.sendCSharpSelectInitPacket(session, objectPacket, objectBody); err != nil {
		return err
	}
	// Current NoPack's op359 path enters sub_1D8C0C0 and dereferences the
	// native selected-character descriptor. Mode0 creates the preview actor but
	// does not install that descriptor; binding-only mode1 performs the native
	// sub_2693E90 assignment without publishing equipment prematurely.
	adventureSummary := s.currentAccountAdventureGroupSummaryForPacket(ctx, session, character, hasCharacter)
	mode1Body := s.buildCurrentActorBindingMode1BodyForSelectedInContext(
		ctx,
		session,
		character,
		hasCharacter,
		charID,
		uint32(adventureSummary.ManageLevel),
		currentSceneObjectContext,
	)
	s.logPacketEvent("game-upper-select-preview-actor-binding-mode1-send",
		"conn_id", session.connID,
		"channel_id", session.channel.ID,
		"source", source,
		"char_id", charID,
		"object_key", objectKey,
		"owner_channel", currentSceneObjectContext,
		"body_len", len(mode1Body),
		"equipment_create_count", 0,
		"body_source", "current_exe_sub_2008010_sub_2002fc0_sub_2693e90_before_sub_1d8c0c0")
	if err := s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketSetUDPIPPort), mode1Body, 0); err != nil {
		return err
	}
	insertBody := s.buildCurrentInsertOverseerBodyForSession(ctx, session, buildCurrentInsertOverseerBody())
	insertPacket := csharpSelectInitPacket{
		class: 0,
		msgID: uint16(dnfenum.CmdPacketInsertOverseer),
		kind:  csharpLongHengSceneBootstrapKind,
	}
	if err := s.sendCSharpSelectInitPacket(session, insertPacket, insertBody); err != nil {
		return err
	}
	clearQuestListBody, err := s.buildCurrentClearQuestListTransportBodyForSession(
		ctx,
		session,
		source+"_select_preview_before_scene_commit",
	)
	if err != nil {
		return err
	}
	if err := s.sendGameUpperFixed16Transport(
		session,
		currentClearQuestListMsgID,
		clearQuestListBody,
		0,
		1,
		true,
		currentClearQuestListTransportCodec,
	); err != nil {
		return err
	}
	if err := s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketReportClientSpec), nil, 0); err != nil {
		return err
	}

	session.selectPreviewObjectStateSent = true
	s.logPacketEvent("game-upper-select-preview-object-state-finished",
		"conn_id", session.connID,
		"channel_id", session.channel.ID,
		"source", source,
		"char_id", charID,
		"object_key", objectKey,
		"packet_count", currentSceneOverseerPageCount+6,
		"personal_panel", "not_sent",
		"removal", "await_validated_op16")
	return nil
}

func (s *Service) sendCurrentPreDungeonContextPlayerState(session *gameSession, source string) error {
	if session == nil {
		return nil
	}
	if session.preDungeonContextPlayerStateSent {
		s.logGameEvent(session, "game-upper-pre-dungeon-context-player-state-skipped",
			"source", source,
			"char_id", session.selectedCharacterID,
			"reason", "already_sent")
		return nil
	}
	if session.selectedCharacterID == 0 {
		return fmt.Errorf("pre-dungeon-context player state requires selected character")
	}

	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	charID, charName, character, hasCharacter := s.selectedCharacterForEnter(ctx, session)
	if charID == 0 {
		charID = session.selectedCharacterID
	}
	objectKey := currentSceneActorObjectKey(charID)
	ownerChannel := byte(currentSceneObjectContext)
	s.logGameEvent(session, "game-upper-pre-dungeon-context-player-state-send",
		"source", source,
		"char_id", charID,
		"object_key", objectKey,
		"owner_channel", ownerChannel,
		"sequence", "msg2_mode0_then_actor_binding_only_mode1_before_op27",
		"mode3", "not_sent",
		"evidence", "pickup_proven_current_client_dungeon_context_uses_owner_zero_for_mode0_and_mode1")

	objectBody := s.buildCurrentSceneObjectListBodyForSessionInContext(
		ctx,
		session,
		charID,
		charName,
		character,
		hasCharacter,
		ownerChannel,
	)
	objectPacket := csharpSelectInitPacket{
		class: 0,
		msgID: uint16(dnfenum.CmdPacketSetUDPIPPort),
		kind:  csharpCurrentSceneObjectListKind,
	}
	if err := s.sendCSharpSelectInitPacket(session, objectPacket, objectBody); err != nil {
		return err
	}

	adventureSummary := s.currentAccountAdventureGroupSummaryForPacket(ctx, session, character, hasCharacter)
	adventureLevel := uint32(adventureSummary.ManageLevel)
	mode1Body := s.buildCurrentActorBindingMode1BodyForSelectedInContext(
		ctx,
		session,
		character,
		hasCharacter,
		charID,
		adventureLevel,
		ownerChannel,
	)
	s.logGameEvent(session, "game-upper-pre-dungeon-context-actor-binding-mode1-send",
		"source", source,
		"char_id", charID,
		"object_key", objectKey,
		"owner_channel", ownerChannel,
		"adventure_total_point", adventureSummary.TotalPoint,
		"adventure_manage_level", adventureSummary.ManageLevel,
		"body_len", len(mode1Body),
		"equipment_create_count", 0,
		"equipment_update_count", 0,
		"stat_blob_len", currentMode1StatBlobWireSize,
		"body_source", "current_exe_sub_2008010_real_92b_state_sub_2002fc0_actor_create_and_sub_2693e90_descriptor_bind")
	if err := s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketSetUDPIPPort), mode1Body, 0); err != nil {
		return err
	}

	session.preDungeonContextPlayerStateSent = true
	s.logGameEvent(session, "game-upper-pre-dungeon-context-player-state-finished",
		"source", source,
		"char_id", charID,
		"object_key", objectKey,
		"packet_count", 2,
		"next_stage", "op27_page_enter_actor_vtable_0x254_existing_runtime_recompute_gate")
	return nil
}

func (s *Service) sendCurrentPostStartMapPlayerPlacement(
	session *gameSession,
	runtime *runtimeDungeonState,
	scene worldmap.DungeonRoomScene,
	source string,
) error {
	if session == nil {
		return nil
	}
	if session.postStartMapPlayerStateSent {
		s.logPacketEvent("game-upper-post-start-map-player-placement-skipped",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"source", source,
			"char_id", session.selectedCharacterID,
			"reason", "already_sent")
		return nil
	}
	if session.selectedCharacterID == 0 {
		return fmt.Errorf("post-start-map player state requires selected character")
	}

	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	charID, charName, character, hasCharacter := s.selectedCharacterForEnter(ctx, session)
	if charID == 0 {
		charID = session.selectedCharacterID
	}
	// The select ACK stores the selected character id as the protected local
	// owner key. Reusing the fixed preview key here creates a second, remote
	// wrapper, so mode1 would update the other-player actor instead of the
	// selected scene actor.
	objectKey := currentSceneActorObjectKey(charID)
	ownerChannel := byte(currentSceneObjectContext)
	// PVF tutorial scenes can still reach sub_1D88A10 before their room object
	// manager is usable, so they defer state 1 until their finish-loading
	// boundary. Ordinary dungeon scenes activate their already-bound mode1
	// actor only after op120 has committed the room placement. Successful
	// pickup traces use that order; sending op3 before op120 leaves the client
	// showing drops without enabling its native op43 pickup writer.
	ownedDungeonScene := currentDungeonRoomOwnsScene(runtime, scene)
	suppressInitialUserState := ownedDungeonScene && isPVFTutorialDungeonScene(runtime, scene)
	sendInitialUserState := ownedDungeonScene && !suppressInitialUserState
	session.deferredDungeonUserStateObjectKey = 0
	if suppressInitialUserState {
		session.deferredDungeonUserStateObjectKey = objectKey
	}
	mode0Stage := "sent_once_before_op27"
	if !session.preDungeonContextPlayerStateSent {
		mode0Stage = "fallback_after_op29"
	}
	s.logPacketEvent("game-upper-post-start-map-player-state-send",
		"conn_id", session.connID,
		"channel_id", session.channel.ID,
		"source", source,
		"char_id", charID,
		"object_key", objectKey,
		"owner_channel", ownerChannel,
		"sequence", "op358_pages_action_table_op13_containers_full_mode1_op359_op356_op124_op9_op120_then_ordinary_op3_or_tutorial_suppress",
		"initial_user_state_eligible", ownedDungeonScene,
		"initial_user_state_suppressed", suppressInitialUserState,
		"mode0", mode0Stage,
		"reason", "op29_room_commit_precedes_full_equipment_and_stat_refresh")

	if err := s.sendCurrentSceneOverseerPages(ctx, session); err != nil {
		return err
	}
	if err := s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketPVPMissionHpPercent), buildCurrentActionTableStateBody(), 0); err != nil {
		return err
	}
	if !session.preDungeonContextPlayerStateSent {
		// Direct callers that did not run the dungeon-context actor-binding stage still
		// need a descriptor before the full mode1 state is applied.
		objectBody := s.buildCurrentSceneObjectListBodyForSessionInContext(
			ctx,
			session,
			charID,
			charName,
			character,
			hasCharacter,
			ownerChannel,
		)
		s.logPacketEvent("game-upper-post-start-map-mode0-built",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"source", source,
			"char_id", charID,
			"object_key", objectKey,
			"body_len", len(objectBody),
			"reason", "fallback_when_pre_op27_descriptor_stage_was_not_sent")
		objectPacket := csharpSelectInitPacket{
			class: 0,
			msgID: uint16(dnfenum.CmdPacketSetUDPIPPort),
			kind:  csharpCurrentSceneObjectListKind,
		}
		if err := s.sendCSharpSelectInitPacket(session, objectPacket, objectBody); err != nil {
			return err
		}
	} else {
		s.logGameEvent(session, "game-upper-post-start-map-mode0-skipped",
			"source", source,
			"char_id", charID,
			"object_key", objectKey,
			"reason", "descriptor_and_actor_bound_before_op27_page_enter")
	}
	// Install the complete container snapshot only after the selected actor and
	// its container managers exist. This changes inventory ownership only; the
	// dungeon scene/camera packet sequence remains untouched.
	if err := s.sendCurrentSelectInventoryBootstrap(session, source+"_after_actor_before_player_object"); err != nil {
		return err
	}
	adventureSummary := s.currentAccountAdventureGroupSummaryForPacket(ctx, session, character, hasCharacter)
	adventureLevel := uint32(adventureSummary.ManageLevel)
	mode1Body := s.buildCurrentActorBindingMode1BodyForSelectedWithEquipmentInContext(
		ctx,
		session,
		character,
		hasCharacter,
		charID,
		adventureLevel,
		ownerChannel,
		true,
	)
	s.logPacketEvent("game-upper-post-start-map-local-mode1-send",
		"conn_id", session.connID,
		"channel_id", session.channel.ID,
		"source", source,
		"char_id", charID,
		"object_key", objectKey,
		"owner_channel", ownerChannel,
		"adventure_total_point", adventureSummary.TotalPoint,
		"adventure_manage_level", adventureSummary.ManageLevel,
		"adventure_manage_option", adventureSummary.ManageOption,
		"body_len", len(mode1Body),
		"stat_blob_len", currentMode1StatBlobWireSize,
		"body_source", "current_exe_sub_2008010_local_owner_real_state_and_equipment_create")
	if err := s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketSetUDPIPPort), mode1Body, 0); err != nil {
		return err
	}
	session.selectedUserInfoRefreshSent = true

	insertBody := s.buildCurrentInsertOverseerBodyForSession(ctx, session, buildCurrentInsertOverseerBody())
	insertPacket := csharpSelectInitPacket{
		class: 0,
		msgID: uint16(dnfenum.CmdPacketInsertOverseer),
		kind:  csharpLongHengSceneBootstrapKind,
	}
	if err := s.sendCSharpSelectInitPacket(session, insertPacket, insertBody); err != nil {
		return err
	}
	clearQuestListBody, err := s.buildCurrentClearQuestListTransportBodyForSession(
		ctx,
		session,
		source+"_post_start_map_before_scene_commit",
	)
	if err != nil {
		return err
	}
	if err := s.sendGameUpperFixed16Transport(
		session,
		currentClearQuestListMsgID,
		clearQuestListBody,
		0,
		1,
		true,
		currentClearQuestListTransportCodec,
	); err != nil {
		return err
	}
	if err := s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketReportClientSpec), nil, 0); err != nil {
		return err
	}

	partyState := runtimePartyStateSnapshot(session)
	_, partyMemberIndexed := runtimePartyMemberIndex(charID, partyState)
	if partyMemberIndexed {
		if err := s.sendRuntimePartyRealtimeInfoLocal(session, partyState); err != nil {
			return err
		}
		frameSent, err := s.sendCurrentPartyFrameProjection(session, partyState, source+"_post_start_map")
		if err != nil {
			return err
		}
		if !frameSent {
			return fmt.Errorf("post-start-map party frame requires bound character %d", charID)
		}
	} else {
		op9Body := buildCurrentSceneOp9ActorDisplayBodyInContext(
			objectKey,
			character,
			hasCharacter,
			charName,
			ownerChannel,
		)
		s.logPacketEvent("game-upper-post-start-map-op9-actor-display-send",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"source", source,
			"char_id", charID,
			"object_key", objectKey,
			"owner_channel", ownerChannel,
			"body_len", len(op9Body),
			"body_source", "current_exe_sub_1D64CA0_after_mode2_room_player_object_and_stats")
		if err := s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketRecoverStamina), op9Body, 0); err != nil {
			return err
		}
	}

	placementBody := buildCurrentSceneActorPlacementBody()
	s.logPacketEvent("game-upper-post-start-map-player-placement-send",
		"conn_id", session.connID,
		"channel_id", session.channel.ID,
		"source", source,
		"char_id", charID,
		"object_key", objectKey,
		"msg_id", uint16(dnfenum.CmdPacketRequestBlacklist),
		"classification", 0,
		"body_len", len(placementBody),
		"actor_slot", placementBody[0],
		"placement_seed", placementBody[1],
		"body_source", "current_exe_sub_1D41DA0_actor_slot_scene_commit")
	if err := s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketRequestBlacklist), placementBody, 0); err != nil {
		return err
	}

	// The working current-client route commits the room object manager with
	// op120 before op3 marks the selected local wrapper active.
	if sendInitialUserState {
		userStateBody, err := buildCurrentDungeonUserStateBody(objectKey)
		if err != nil {
			return err
		}
		s.logPacketEvent("game-upper-post-start-map-user-state-send",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"source", source,
			"char_id", charID,
			"object_key", objectKey,
			"msg_id", uint16(dnfenum.CmdPacketNotifyUserState),
			"classification", 0,
			"body_len", len(userStateBody),
			"user_state", currentDungeonPlayerUserState,
			"body_source", "working_current_client_trace_op3_after_op120_room_object_manager_commit")
		if err := s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketNotifyUserState), userStateBody, 0); err != nil {
			return err
		}
	} else if suppressInitialUserState {
		s.logPacketEvent("game-upper-post-start-map-user-state-skipped",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"source", source,
			"char_id", charID,
			"object_key", objectKey,
			"map_path", scene.Map.Map.Path,
			"deferred_until", "client_finish_loading_request",
			"reason", "current_exe_tutorial_state1_handler_crashes_before_the_client_finish_loading_boundary")
	} else if !sendInitialUserState {
		s.logPacketEvent("game-upper-post-start-map-user-state-skipped",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"source", source,
			"char_id", charID,
			"object_key", objectKey,
			"map_path", scene.Map.Map.Path,
			"reason", "scene_is_not_owned_by_current_dungeon_runtime")
	}

	session.postStartMapPlayerStateSent = true
	session.sceneBootstrapTailDeferred = false
	session.sceneBootstrapTailSent = true
	minimumPacketCount := currentSceneOverseerPageCount + 13
	if !session.preDungeonContextPlayerStateSent {
		minimumPacketCount++
	}
	userStateResult := "skipped_unowned_dungeon_scene"
	if sendInitialUserState {
		minimumPacketCount++
		userStateResult = "normal_state_1_sent_after_op120_room_object_manager_commit_for_owned_dungeon"
	} else if suppressInitialUserState {
		userStateResult = "deferred_to_client_finish_loading_for_pvf_tutorial"
	}
	s.logPacketEvent("game-upper-post-start-map-player-placement-finished",
		"conn_id", session.connID,
		"channel_id", session.channel.ID,
		"source", source,
		"char_id", charID,
		"object_key", objectKey,
		"minimum_packet_count", minimumPacketCount,
		"item_lists", "sent_before_full_equipment_mode1_after_pre_op27_actor_binding",
		"user_state", userStateResult,
		"mode0", mode0Stage,
		"mode1", "full_equipment_refresh_after_op29_for_existing_local_actor",
		"mode3", "not_sent_personal_information_panel_route",
		"equipment", "mode1_create_then_post_finish_op14_update",
		"skills", "post_finish_op19_after_local_actor_state",
		"op9", "sent_after_scene_object_finalizer_before_scene_slot_placement",
		"op3", userStateResult,
		"hud", "deferred")
	return nil
}
