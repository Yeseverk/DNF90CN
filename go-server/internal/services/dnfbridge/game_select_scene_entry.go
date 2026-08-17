package dnfbridge

import (
	"context"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	"strconv"
	"time"
)

func (s *Service) sendUpperCSharpSelectInit(session *gameSession, request []byte) error {
	packets := expandedCSharpUpperSelectInitTemplate()
	var legacyRepo dnfrepo.LegacyUserInfoRepository
	var selectedQuests dnfrepo.QuestRecord
	hasSelectedQuests := false
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()

	charID, charName, selectedCharacter, hasSelectedCharacter, slot := s.resolveSelectedCharacter(ctx, session, request)
	if !hasSelectedCharacter {
		s.logPacketEvent("game-upper-select-character-init-blocked",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"slot", slot,
			"char_id", charID,
			"request_body_len", len(request),
			"reason", "select_request_not_decoded")
		return nil
	}
	s.bindGameSessionCharacter(session, charID)
	if activationErr := s.reconcileCurrentPremiumInventoryBeforeList(
		ctx,
		session,
		"select_character_before_premium_ack_and_inventory_bootstrap",
		false,
	); activationErr != nil {
		// Selection remains recoverable: the untouched item is projected and
		// can still be used manually, while the next list snapshot retries the
		// automatic conversion.
		s.logGameEvent(session, "game-upper-select-character-premium-inventory-auto-activation-deferred",
			"char_id", charID,
			"reason", activationErr,
			"fallback", "retain_contract_item_and_retry_before_list0")
	}
	s.persistCurrentSelectorAdventureInfoSlot(ctx, session, slot)
	session.selectedCreatureStateTableSent = false
	s.resetCurrentInitialTownRoute(session)
	if stats, ok := s.characterPVFStatsForUserInfo(ctx, session, selectedCharacter, hasSelectedCharacter); ok {
		applyCharacterPVFStats(&selectedCharacter, stats)
	}
	selectedSlot, selectedSlotOK := currentSelectAckSelectedSlot(slot)
	if !selectedSlotOK {
		s.logPacketEvent("game-upper-select-character-init-blocked",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"slot", slot,
			"char_id", charID,
			"request_body_len", len(request),
			"reason", "current_select_ack_selected_slot_out_of_u8_range")
		return nil
	}
	if repos, ok := s.repositoryGroup(); ok {
		legacyRepo = repos.LegacyUserInfo
		if repos.Quest != nil {
			characterID := selectedCharacter.CharacterID
			if characterID == "" {
				characterID = strconv.Itoa(int(charID))
			}
			var err error
			selectedQuests, hasSelectedQuests, err = repos.Quest.Load(ctx, characterID)
			if err != nil {
				hasSelectedQuests = false
				s.logGameEvent(session, "game-upper-select-character-quest-load-failed",
					"char_id", charID,
					"error", err)
			}
		}
	}

	s.logPacketEvent("game-upper-select-character-csharp-init-send",
		"conn_id", session.connID,
		"channel_id", session.channel.ID,
		"slot", slot,
		"char_id", charID,
		"packet_count", len(packets))
	accountCera := uint32(0)
	premiumEntries := []byte{0}
	if repos, ok := s.repositoryGroup(); ok {
		if balance := s.loadCurrentAccountCera(ctx, repos, session); balance > 0 {
			if balance > int64(^uint32(0)) {
				balance = int64(^uint32(0))
			}
			accountCera = uint32(balance)
		}
		if repos.Account != nil {
			if account, found, err := repos.Account.Load(ctx, s.accountIDForSession(session)); err == nil && found {
				premiumEntries = currentSelectAckPremiumEntries(account, time.Now().UTC())
			}
		}
	}
	for _, packet := range packets {
		body := packet.body
		if packet.kind == "userinfo" {
			s.logGameEvent(session, "game-upper-select-character-userinfo-deferred",
				"char_id", charID,
				"occurrence", packet.occurrence,
				"reason", "selected_userinfo_refresh_waits_for_enter_scene_state")
			continue
		}
		if packet.kind == "select_deferred" {
			s.logGameEvent(session, "game-upper-select-character-packet-deferred",
				"char_id", charID,
				"msg_id", packet.msgID,
				"reason", "client_crashes_when_sent_before_enter_scene_state")
			continue
		}
		if !selectInitPacketAllowedBeforeScene(packet) {
			s.logGameEvent(session, "game-upper-select-character-packet-deferred",
				"char_id", charID,
				"msg_id", packet.msgID,
				"kind", packet.kind,
				"reason", "wait_for_client_request_or_scene_state")
			continue
		}
		switch packet.kind {
		case "select_ack":
			body = buildCurrentSelectCharacterAckBody(selectedCharacter, hasSelectedCharacter, selectedQuests, hasSelectedQuests, charID, selectedSlot, accountCera, premiumEntries)
			fixedQuestRows, overflowQuestRows, questRowsOK := currentSelectAckQuestRowCounts(body)
			ackCharacterID, ackCharacterIDOK := currentSelectAckCharacterID(body)
			ackSelectedSlot, ackSelectedSlotOK := currentSelectAckSelectedSlotFromBody(body)
			ackIntermediateState, ackIntermediateOffset, ackIntermediateOK := currentSelectAckIntermediateState(body)
			tutorialFlag, tutorialCount, tutorialStateOK := currentSelectAckTutorialState(body)
			tutorialIndexes, tutorialIndexesOK := currentSelectAckTutorialIndexes(body)
			fatigue := currentCharacterFatigueState(selectedCharacter, hasSelectedCharacter)
			s.logPacketEvent("game-upper-select-character-ack-current-fields",
				"conn_id", session.connID,
				"channel_id", session.channel.ID,
				"char_id", charID,
				"slot", slot,
				"quest_record_found", hasSelectedQuests,
				"quest_state_count", len(selectedQuests.States),
				"fixed_quest_rows", fixedQuestRows,
				"overflow_quest_rows", overflowQuestRows,
				"quest_rows_ok", questRowsOK,
				"ack_character_id", ackCharacterID,
				"ack_character_id_ok", ackCharacterIDOK,
				"selected_slot", ackSelectedSlot,
				"selected_slot_ok", ackSelectedSlotOK,
				"intermediate_state_offset", ackIntermediateOffset,
				"intermediate_state_ok", ackIntermediateOK,
				"intermediate_state0", ackIntermediateState[0],
				"intermediate_state1", ackIntermediateState[1],
				"intermediate_state2", ackIntermediateState[2],
				"intermediate_state3", ackIntermediateState[3],
				"tutorial_ignored_flag", tutorialFlag,
				"tutorial_count", tutorialCount,
				"tutorial_state_ok", tutorialStateOK,
				"tutorial_indexes", tutorialIndexes,
				"tutorial_indexes_ok", tutorialIndexesOK,
				"server_tutorial_completed", hasPersistedDungeonTutorialCompletion(selectedCharacter),
				"fatigue_used", fatigue.used,
				"fatigue_limit", fatigue.limit,
				"body_len", len(body),
				"body_source", "current_exe_sub_1D01C50_sub_1A0C3E0_4u32_character_quest_slot_typed")
		case "select_scene_userinfo":
			body = s.buildCSharpSelectedUserInfoBody(ctx, session, legacyRepo, packet.occurrence, selectedCharacter, hasSelectedCharacter, charID, charName)
			s.logPacketEvent("game-upper-select-character-scene-userinfo-send",
				"conn_id", session.connID,
				"channel_id", session.channel.ID,
				"char_id", charID,
				"occurrence", packet.occurrence,
				"msg_id", packet.msgID,
				"body_len", len(body),
				"source", "csharp_select_init_before_scene_object_chain")
		case "userinfo":
			body = s.buildCSharpSelectedUserInfoBody(ctx, session, legacyRepo, packet.occurrence, selectedCharacter, hasSelectedCharacter, charID, charName)
		case csharpLegacyUserInfoKind:
			var ok bool
			body, ok = s.buildCSharpLegacyUserInfoBody(ctx, session, legacyRepo, charID, packet.msgID)
			if !ok {
				continue
			}
		case csharpCurrentActionTableStateKind:
			s.logPacketEvent("game-upper-current-action-table-state-send",
				"conn_id", session.connID,
				"channel_id", session.channel.ID,
				"char_id", charID,
				"msg_id", packet.msgID,
				"body_len", len(body),
				"body_source", "current_exe_sub_217D850_u8_u8_u8",
				"selector_source", "decoded_dword_51B0F14_F18",
				"reason", "initialize_sub_3851100_active_table_before_mode0_object")
		case csharpCurrentSceneObjectListKind:
			body = s.buildCurrentSceneObjectListBodyForSession(ctx, session, charID, charName, selectedCharacter, hasSelectedCharacter)
		case csharpCurrentSceneOp9ActorDisplayKind:
			objectKey := currentSceneActorObjectKey(charID)
			body = buildCurrentSceneOp9ActorDisplayBodyInContext(
				objectKey,
				selectedCharacter,
				hasSelectedCharacter,
				charName,
				currentSceneObjectContext,
			)
			op9Name := charName
			if op9Name == "" && hasSelectedCharacter {
				op9Name = selectedCharacter.Name
			}
			s.logGameEvent(session, "game-upper-scene-op9-current-actor-display-send",
				"char_id", charID,
				"msg_id", packet.msgID,
				"scene_object_key", objectKey,
				"scene_value", currentSceneOp9StableSceneValue,
				"body_len", len(body),
				"record_kind", currentSceneOp9ActorDisplayKind,
				"name_len", len(rosterNameBytes(op9Name)),
				"slot_count", 0,
				"follow_mode", 0)
		case "title_book":
			// Send real title book lists from inventory (list 100) instead of empty fixture.
			characterID := strconv.FormatUint(uint64(session.selectedCharacterID), 10)
			tbCtx, tbCancel := context.WithTimeout(context.Background(), createWriteTimeout)
			repos, _ := s.repositoryGroup()
			if repos.Inventory != nil {
				_ = s.sendAllCurrentTitleBookLists(session, tbCtx, repos, characterID)
			}
			tbCancel()
			// Still send the empty fixture for categories the client expects at init.
			body = buildCSharpTitleBookBody(packet.occurrence)
		case "daily_schedule":
			body = buildCSharpDailyScheduleBody(packet.occurrence)
		}
		if err := s.sendCSharpSelectInitPacket(session, packet, body); err != nil {
			return err
		}
		if packet.kind == csharpCurrentSceneObjectListKind {
			if err := s.sendSelectedSceneUserInfo23Mode1Only(session, "select_character_after_current_object_list"); err != nil {
				return err
			}
		}
		if packet.kind == "select_ack" {
			// The current client materializes the world-map dungeon nodes as soon
			// as it consumes the select ACK. Restore the durable permission snapshot
			// before the later story/scene packets so an already-unlocked character
			// does not need to re-enter the dungeon selector to see its maps.
			if err := s.sendCurrentDungeonPermissionSnapshot(ctx, session, "select_character_after_ack_before_worldmap_scene"); err != nil {
				return err
			}
			// Current EXE sub_11DBE40 consumes class0/op1378 raw u32 before
			// the selected scene is finalized. Send the durable per-character
			// story-summary level immediately after the select ACK, before the
			// tutorial/town route can emit any scene packet.
			if err := s.sendCurrentStoryDigestLastLevel(session, selectedCharacter, hasSelectedCharacter, "select_character_after_ack_before_scene"); err != nil {
				return err
			}
			startsInTutorial := selectedCharacterStartsInTutorial(selectedCharacter, hasSelectedCharacter)
			if session.channelReconnect {
				clearReturnSelectTownReentry(session)
				s.logGameEvent(session, "game-channel-reconnect-normal-select-route-skipped",
					"char_id", charID,
					"target_channel", session.channel.ID,
					"reason", "fresh_target_channel_town_route_runs_after_op4_bootstrap")
			} else if startsInTutorial {
				clearReturnSelectTownReentry(session)
				s.logGameEvent(session, "game-upper-current-scene-transition-op24-deferred",
					"source", "select_character_after_ack",
					"char_id", charID,
					"msg_id", currentSceneTransitionMsgID,
					"tutorial_completed", false,
					"reason", "tutorial_pending_first_scene_is_dungeon_not_town")
				if err := s.sendEnterSelectDungeonState(session, "select_character_after_ack_15", false, false); err != nil {
					return err
				}
				if err := s.sendCurrentSelectPreviewObjectState(ctx, session, charID, charName, selectedCharacter, hasSelectedCharacter, "select_character_after_scene_gate_op15"); err != nil {
					return err
				}
			} else {
				s.armCurrentInitialTownRoute(session, charID)
				if returnSelectTownReentryPending(session) {
					if err := s.resumeCurrentInitialTownRouteAfterReturnSelect(session); err != nil {
						return err
					}
				} else {
					s.logGameEvent(session, "game-upper-select-character-tutorial-preview-skipped",
						"source", "select_character_after_ack",
						"char_id", charID,
						"tutorial_completed", hasPersistedDungeonTutorialCompletion(selectedCharacter),
						"tutorial_route_index", currentSelectAckPage1RouteIndex,
						"reason", "current_exe_select_ack_switches_to_page1_and_arms_client_requested_progress36_town_route")
				}
			}
		}
	}
	if session.channelReconnect {
		return s.sendCurrentChannelReconnectTownEntry(session)
	}
	session.sceneBootstrapTailDeferred = true
	session.sceneBootstrapTailSent = false
	resetCurrentDeferredSelectSceneTailProgress(session)
	s.logGameEvent(session, "game-select-scene-tail-deferred",
		"source", "select_character_wait_for_current_op29",
		"char_id", session.selectedCharacterID,
		"reason", "preview_eviction_then_final_actor_binding_before_op27_and_full_state_after_op29",
		"deferred_sequence", "validated_op16_op9_kind3_preview_remove_then_op5_final_mode0_actor_binding_mode1_op27_op28_op29_full_mode1_mode3")
	return nil
}
