package dnfbridge

import (
	"context"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func (s *Service) sendUpperFinishLoadingGate(session *gameSession, source string) error {
	s.logPacketEvent("game-upper-finish-loading-gate-deferred",
		"conn_id", session.connID,
		"channel_id", session.channel.ID,
		"source", source,
		"char_id", session.selectedCharacterID,
		"msg_id", uint16(dnfenum.CmdPacketFinishLoading),
		"classification", 0,
		"reason", "current_exe_treats_class0_op37_as_exp_notice_not_loading_gate")
	return nil
}

func (s *Service) sendSelectedSceneUserInfoRefresh(session *gameSession, source string) error {
	if session == nil {
		return nil
	}
	if session.selectedUserInfoRefreshSent {
		s.logPacketEvent("game-upper-selected-userinfo-refresh-skipped",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"source", source,
			"char_id", session.selectedCharacterID,
			"reason", "already_sent")
		return nil
	}
	if session.selectedCharacterID == 0 {
		s.logPacketEvent("game-upper-selected-userinfo-refresh-deferred",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"source", source,
			"reason", "character_not_selected")
		return nil
	}
	// 当前 NoPack 的 class0/op2 入口 sub_200BEA0 已经按 mode0 current-object 解析。
	// 旧 C# subtype0/subtype1 直接塞进 op2 会被当成 raw[0x47]，污染 byte_50C74C0 并导致模型/属性错位。
	s.logPacketEvent("game-upper-selected-userinfo-refresh-skipped",
		"conn_id", session.connID,
		"channel_id", session.channel.ID,
		"source", source,
		"char_id", session.selectedCharacterID,
		"reason", "current_exe_op2_mode0_incompatible")
	session.selectedUserInfoRefreshSent = true
	return nil
}

func (s *Service) sendSelectedSceneUserInfo23Refresh(session *gameSession, source string) error {
	// Ensure the current scene's character-owned container subset precedes full
	// mode1. The select-ACK owner normally makes this an idempotent no-op.
	if err := s.sendSelectedCurrentContainerListsWithRefresh(session, source+"_before_mode1", false); err != nil {
		return err
	}
	if err := s.sendSelectedSceneUserInfo23Mode1Only(session, source); err != nil {
		return err
	}
	return s.sendSelectedRentalWalletStateWithRefresh(session, source, false)
}

func (s *Service) sendSelectedSceneUserInfo23Mode1Only(session *gameSession, source string) error {
	if session == nil {
		return nil
	}
	if session.selectedUserInfoRefreshSent {
		s.logPacketEvent("game-upper-selected-userinfo-refresh-skipped",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"source", source,
			"char_id", session.selectedCharacterID,
			"reason", "already_sent")
		return nil
	}
	if session.selectedCharacterID == 0 {
		s.logPacketEvent("game-upper-selected-userinfo-refresh-deferred",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"source", source,
			"reason", "character_not_selected")
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()

	charID, _, character, hasCharacter := s.selectedCharacterForEnter(ctx, session)
	if charID == 0 {
		charID = session.selectedCharacterID
	}
	var legacyRepo dnfrepo.LegacyUserInfoRepository
	if repos, ok := s.repositoryGroup(); ok {
		legacyRepo = repos.LegacyUserInfo
	}

	s.logPacketEvent("game-upper-selected-userinfo-mode3-refresh-deferred",
		"conn_id", session.connID,
		"channel_id", session.channel.ID,
		"source", source,
		"char_id", charID,
		"msg_id", uint16(dnfenum.CmdPacketSetUDPIPPort),
		"body_source", "current_exe_mode3_stat",
		"object_key", currentSceneActorObjectKey(charID),
		"stat_blob_len", currentMode1StatBlobWireSize,
		"tail_source", "current_exe_sub_1D77560_aligned",
		"reason", "early_mode3_remains_blocked_until_safe_runtime_seed_mode1_already_applies_real_state")

	body := s.buildCurrentSelectedUserInfoMode1BodyInContext(
		ctx,
		session,
		legacyRepo,
		character,
		hasCharacter,
		charID,
		currentTownActorOwnerContext(session),
	)
	s.logPacketEvent("game-upper-selected-userinfo-mode1-refresh-send",
		"conn_id", session.connID,
		"channel_id", session.channel.ID,
		"source", source,
		"char_id", charID,
		"msg_id", uint16(dnfenum.CmdPacketSetUDPIPPort),
		"body_len", len(body),
		"stat_blob_len", currentMode1StatBlobWireSize,
		"body_source", "current_exe_mode1_real_92b_state_and_equipment",
		"object_key", currentSceneActorObjectKey(charID))
	if err := s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketSetUDPIPPort), body, 0); err != nil {
		return err
	}
	// The following current-town op23 carries the same repository-backed
	// visibility bits while binding the selected actor. Sending op357 here
	// would detach and reinsert fashion components a second time during login.
	session.selectedUserInfoRefreshSent = true
	return nil
}

func (s *Service) sendSelectedSceneUserInfoMode3RefreshOnce(session *gameSession, source string) error {
	if session == nil {
		return nil
	}
	if session.selectedUserInfoMode3Sent {
		s.logPacketEvent("game-upper-selected-userinfo-mode3-refresh-skipped",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"source", source,
			"char_id", session.selectedCharacterID,
			"reason", "already_sent")
		return nil
	}
	if session.selectedCharacterID == 0 {
		s.logPacketEvent("game-upper-selected-userinfo-mode3-refresh-deferred",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"source", source,
			"reason", "character_not_selected")
		return nil
	}
	if !session.selectedUserInfoRefreshSent {
		s.logPacketEvent("game-upper-selected-userinfo-mode3-refresh-deferred",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"source", source,
			"char_id", session.selectedCharacterID,
			"reason", "mode1_object_refresh_not_sent")
		return nil
	}
	if !session.runtimeAfterBlacklistSent {
		s.logPacketEvent("game-upper-selected-userinfo-mode3-refresh-deferred",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"source", source,
			"char_id", session.selectedCharacterID,
			"reason", "runtime_seed_not_finished")
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()

	charID, _, character, hasCharacter := s.selectedCharacterForEnter(ctx, session)
	if charID == 0 {
		charID = session.selectedCharacterID
	}
	var legacyRepo dnfrepo.LegacyUserInfoRepository
	if repos, ok := s.repositoryGroup(); ok {
		legacyRepo = repos.LegacyUserInfo
	}

	body := s.buildCurrentSelectedUserInfoMode3BodyInContext(
		ctx,
		session,
		legacyRepo,
		character,
		hasCharacter,
		charID,
		currentTownActorOwnerContext(session),
	)
	s.logPacketEvent("game-upper-selected-userinfo-mode3-refresh-send",
		"conn_id", session.connID,
		"channel_id", session.channel.ID,
		"source", source,
		"char_id", charID,
		"msg_id", uint16(dnfenum.CmdPacketSetUDPIPPort),
		"body_len", len(body),
		"body_source", "current_exe_mode3_stat",
		"object_key", currentSceneActorObjectKey(charID),
		"stat_blob_len", 92,
		"reason", "after_runtime_seed_current_sub_2008600_len172")
	if err := s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketSetUDPIPPort), body, 0); err != nil {
		return err
	}
	session.selectedUserInfoMode3Sent = true
	return nil
}
