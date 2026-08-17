package dnfbridge

import (
	"context"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func (s *Service) sendRuntimeFinishLoadingGateOnce(session *gameSession, source string) error {
	if session.selectedCharacterID == 0 {
		s.logPacketEvent("game-upper-runtime-finish-loading-gate-deferred",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"source", source,
			"reason", "character_not_selected")
		return nil
	}
	if session.runtimeFinishLoadingGateSent || session.sceneBootstrapTailSent {
		s.logPacketEvent("game-upper-runtime-finish-loading-gate-skipped",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"source", source,
			"char_id", session.selectedCharacterID,
			"runtime_gate_sent", session.runtimeFinishLoadingGateSent,
			"scene_tail_sent", session.sceneBootstrapTailSent,
			"reason", "already_sent")
		return nil
	}
	if err := s.sendRuntimeSceneReadySequence(session, source+"_scene_ready"); err != nil {
		return err
	}
	// 当前 NoPack 在 120 黑名单被动 ACK 后只继续心跳；按 DOVE 顺序补安全场景前置、851 HUD-ready，再补最小 37 门闩。
	if err := s.sendUpperFinishLoadingGate(session, source); err != nil {
		return err
	}
	session.runtimeFinishLoadingGateSent = true
	return nil
}

func (s *Service) sendRuntimeSceneReadySequence(session *gameSession, source string) error {
	s.logPacketEvent("game-upper-runtime-scene-bootstrap-send",
		"conn_id", session.connID,
		"channel_id", session.channel.ID,
		"source", source,
		"char_id", session.selectedCharacterID,
		"packet_count", len(longHengSceneBootstrapBeforeHudPackets),
		"reason", "dove_scene_bootstrap_sanitized_before_hud")
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	for _, packet := range longHengSceneBootstrapBeforeHudPackets {
		body := packet.body
		if packet.kind == csharpCurrentSceneObjectListKind {
			body = s.buildCurrentSceneObjectListBodyForSession(ctx, session, 0, "", dnfrepo.CharacterRecord{}, false)
		}
		if err := s.sendCSharpSelectInitPacket(session, packet, body); err != nil {
			return err
		}
	}
	// The actor-bound post-start-map stage owns the complete item-list
	// bootstrap. This fallback scene-ready path must not replay the containers.
	if err := s.sendSelectedSceneUserInfo23Refresh(session, source); err != nil {
		return err
	}
	return nil
}

func (s *Service) sendRuntimeAfterBlacklistSeedOnce(session *gameSession, source string) error {
	if session == nil || !session.postStartMapPlayerStateSent {
		if session != nil {
			s.logPacketEvent("game-upper-request-blacklist-runtime-seed-deferred",
				"conn_id", session.connID,
				"channel_id", session.channel.ID,
				"source", source,
				"char_id", session.selectedCharacterID,
				"reason", "wait_for_current_op29_and_post_start_map_player_state")
		}
		return nil
	}
	if session.runtimeAfterBlacklistSent {
		s.logPacketEvent("game-upper-request-blacklist-runtime-seed-skipped",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"source", source,
			"char_id", session.selectedCharacterID,
			"reason", "already_sent")
		return nil
	}
	packets := longHengSceneRuntimeAfterBlacklistSafePrefixPackets()
	s.logPacketEvent("game-upper-request-blacklist-runtime-seed-send",
		"conn_id", session.connID,
		"channel_id", session.channel.ID,
		"source", source,
		"char_id", session.selectedCharacterID,
		"packet_count", len(packets),
		"reason", "mcp_current_handlers_release_runtime_op9_noop_filter_op22_op356_class1_exit")
	session.runtimeAfterBlacklistSent = true
	for idx, packet := range packets {
		if err := s.sendCSharpSelectInitPacket(session, packet, packet.body); err != nil {
			s.logPacketEvent("game-upper-request-blacklist-runtime-seed-send-failed",
				"conn_id", session.connID,
				"channel_id", session.channel.ID,
				"source", source,
				"char_id", session.selectedCharacterID,
				"packet_index", idx,
				"msg_id", packet.msgID,
				"classification", packet.class,
				"body_len", len(packet.body),
				"error", err)
			return err
		}
	}
	s.logPacketEvent("game-upper-request-blacklist-runtime-seed-finished",
		"conn_id", session.connID,
		"channel_id", session.channel.ID,
		"source", source,
		"char_id", session.selectedCharacterID,
		"packet_count", len(packets))
	return s.sendSelectedSceneUserInfoMode3RefreshOnce(session, source+"_after_runtime_seed")
}

func (s *Service) sendUpperContentsPlayInfoGate(session *gameSession, source string) error {
	s.logPacketEvent("game-upper-contents-play-info-gate-deferred",
		"conn_id", session.connID,
		"channel_id", session.channel.ID,
		"source", source,
		"char_id", session.selectedCharacterID,
		"msg_id", uint16(dnfenum.CmdPacketContentsPlayInfo),
		"classification", 0,
		"reason", "dove_scene_flow_has_no_s2c_704_and_current_client_opens_side_panel")
	return nil
}

func (s *Service) handleGameFpsDevideCollect(session *gameSession, body []byte) error {
	s.logPacketEvent("game-legacy-fps-devide-collect-received",
		"conn_id", session.connID,
		"channel_id", session.channel.ID,
		"char_id", session.selectedCharacterID,
		"body_len", len(body))
	if session.selectedCharacterID == 0 {
		s.logPacketEvent("game-legacy-fps-devide-collect-gate-deferred",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"body_len", len(body),
			"reason", "character_not_selected")
		return nil
	}
	if session.fpsFinishLoadingGateSent {
		s.logPacketEvent("game-legacy-fps-devide-collect-gate-skipped",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"char_id", session.selectedCharacterID,
			"body_len", len(body),
			"reason", "already_sent")
		return nil
	}
	if session.sceneBootstrapTailSent {
		s.logPacketEvent("game-legacy-fps-devide-collect-gate-skipped",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"char_id", session.selectedCharacterID,
			"body_len", len(body),
			"reason", "scene_bootstrap_already_sent")
		return nil
	}
	// 旧 DOVE 没有当前 1426 样本；实测 NoPack 已到运行期 FPS 采集但仍停在选角层时，用它作为更晚的加载完成门闩。
	if err := s.sendUpperFinishLoadingGate(session, "legacy_1426_fps_devide_collect"); err != nil {
		return err
	}
	session.fpsFinishLoadingGateSent = true
	return nil
}
