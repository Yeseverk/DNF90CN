package dnfbridge

import (
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
)

func selectInitPacketAllowedBeforeScene(packet csharpSelectInitPacket) bool {
	return packet.kind == "select_ack"
}

func selectInitPacketSupersededByLongHengScene(packet csharpSelectInitPacket) bool {
	switch packet.kind {
	case "select_load_empty", "select_scene_probe", "select_scene_pet_list", "select_scene_userinfo", "select_scene_safe_error":
		return true
	}
	return packet.kind == "" &&
		packet.class == 0 &&
		packet.msgID == uint16(dnfenum.CmdPacketLeaveParty) &&
		len(packet.body) == 5 &&
		packet.body[0] <= 2
}

func selectInitPacketRequiresCurrentSceneObject(packet csharpSelectInitPacket) bool {
	return packet.msgID == uint16(dnfenum.CmdPacketInsertOverseer)
}

func (s *Service) sendFinishLoadingStatus(session *gameSession, request []byte) error {
	if session == nil {
		return nil
	}
	// sub_217E0C0 writes two semantic u32 zero values, but the current client
	// can send op37 through more than one protected/legacy boundary. Production
	// evidence on 2026-07-21 shows the live EXE sends the eight-byte semantic
	// body (two zero u32 values) after dungeon op29, while older captures used
	// the 16-byte protected wrapper. Accept only these two proven shapes before
	// sending the ACK or advancing any finish-loading lifecycle gate.
	if !currentFinishLoadingRequestBodyAccepted(request) {
		s.logGameEvent(session, "game-upper-finish-loading-request-rejected",
			"request_body_len", len(request),
			"expected_body_lens", "8_or_16",
			"reason", "current_live_legacy_op37_body_length_mismatch")
		return nil
	}
	actorReady, actorReadySource := s.currentSceneActorReadyForState(session)
	s.logGameEvent(session, "game-upper-finish-loading-status-send",
		"msg_id", uint16(dnfenum.CmdPacketFinishLoading),
		"classification", dnfproto.DefaultChannelClassification,
		"request_body_len", len(request),
		"plain_body_len", 1,
		"body_source", "current_exe_sub_1CF50C0_success_reads_no_body_after_discriminator",
		"actor_ready_source", actorReadySource,
		"main_op37", "sent_after_status_when_selected_actor_and_scene_transition_are_complete")
	if err := s.sendGameUpperSuccess(session, uint16(dnfenum.CmdPacketFinishLoading), nil); err != nil {
		return err
	}
	// A completed dungeon return already published its town actor/scene state.
	// The current client nevertheless emits its normal op37 request after that
	// transition. Replaying the class0/op37 character-state reader at this
	// point opens the personal-information refresh UI. Keep the proven empty
	// status ACK, but do not turn this return-side request into a second
	// progression snapshot.
	if session.returnTownFinishLoadingAckOnly {
		if err := s.ensureCurrentConfirmedDungeonReturnPlayerState(
			session,
			"request37_after_confirmed_direct_return",
		); err != nil {
			s.logWarn("dnfbridge deferred confirmed return player state after op37 ACK",
				"conn_id", session.connID,
				"char_id", session.selectedCharacterID,
				"error", err)
			// The success ACK may already be on the wire, but the town
			// actor/HUD generation is not complete. Propagate the failure so
			// the connection cannot continue in a falsely-ready state; the
			// staged cursor and confirmation marker remain available to the
			// next current-client confirmation or reconnect.
			return err
		}
		s.logGameEvent(session, "game-upper-finish-loading-return-town-ack-only",
			"request_body_len", len(request),
			"reason", "completed_dungeon_town_state_already_published")
		return nil
	}
	if !actorReady {
		s.logGameEvent(session, "game-main-finish-loading-state-deferred",
			"request_body_len", len(request),
			"actor_ready_source", actorReadySource,
			"reason", "selected_actor_or_scene_transition_incomplete")
		return nil
	}
	if err := s.sendCurrentFinishLoadingCharacterState(session, "request37_after_upper_status"); err != nil {
		return err
	}
	if err := s.sendCompletedTutorialReentryExit(session, "request37_after_current_finish_loading_state"); err != nil {
		return err
	}
	return s.startCurrentSuspiciousVillageElevator(session, "request37_after_current_finish_loading_state")
}

func (s *Service) sendContentsPlayInfoState(session *gameSession, body []byte) error {
	s.logGameEvent(session, "game-contents-play-info-deferred",
		"body_len", len(body),
		"reason", "dove_scene_flow_has_no_s2c_704_and_current_client_opens_side_panel")
	// DOVE 完整进场景 S2C 流没有 704；当前 NoPack 收到 704 成功体会打开右侧公告/聊天面板。
	return nil
}

func (s *Service) sendRequestBlacklistState(session *gameSession, source string, request []byte) error {
	s.logPacketEvent("game-upper-request-blacklist-passive-send",
		"conn_id", session.connID,
		"channel_id", session.channel.ID,
		"source", source,
		"char_id", session.selectedCharacterID,
		"msg_id", uint16(dnfenum.CmdPacketRequestBlacklist),
		"classification", dnfproto.DefaultChannelClassification,
		"request_body_len", len(request),
		"plain_body_len", len(currentRequestBlacklistResponseBody),
		"body_source", "current_upper_op120_blacklist_date_empty_success",
		"reason", "current_nopack_upper_op120_is_distinct_from_main_scene_op120")
	if err := s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketRequestBlacklist), currentRequestBlacklistResponseBody, dnfproto.DefaultChannelClassification); err != nil {
		return err
	}
	if !session.sceneBootstrapTailSent {
		s.logPacketEvent("game-upper-request-blacklist-before-hud-replay-skipped",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"source", source,
			"char_id", session.selectedCharacterID,
			"reason", "current_object_create_mode0_is_one_shot_runtime_uses_after_blacklist_structures")
	}
	if !session.postStartMapPlayerStateSent {
		s.logPacketEvent("game-upper-request-blacklist-runtime-seed-deferred",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"source", source,
			"char_id", session.selectedCharacterID,
			"reason", "blacklist_ack_is_request_driven_but_runtime_seed_waits_for_op29_player_state")
		return nil
	}
	if err := s.sendRuntimeAfterBlacklistSeedOnce(session, source+"_runtime_seed"); err != nil {
		return err
	}
	s.logPacketEvent("game-upper-request-blacklist-runtime-seed-current-struct-complete",
		"conn_id", session.connID,
		"channel_id", session.channel.ID,
		"source", source,
		"char_id", session.selectedCharacterID,
		"reason", "runtime_after_blacklist_sequence_sent_without_replaying_before_hud_object_create")
	return nil
}

func (s *Service) sendGuildAllMemberListState(session *gameSession, source string, request []byte) error {
	s.logPacketEvent("game-upper-guild-all-member-list-passive-deferred",
		"conn_id", session.connID,
		"channel_id", session.channel.ID,
		"source", source,
		"char_id", session.selectedCharacterID,
		"msg_id", uint16(dnfenum.CmdPacketGuildAllMemberList),
		"classification", dnfproto.DefaultChannelClassification,
		"request_body_len", len(request),
		"plain_body_len", 0,
		"reason", "defer_guild_member_list_during_select_scene_bootstrap_no_dove_replay")
	// 当前 NoPack 在选角进场景前收到 op140 大表会拉起右侧社交/系统面板；先延后响应，等场景稳定后再恢复真实公会面板。
	return nil
}
