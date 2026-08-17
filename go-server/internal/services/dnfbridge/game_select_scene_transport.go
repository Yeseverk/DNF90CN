package dnfbridge

import (
	"context"
	"longheng.io/server/internal/modules/dnf/dnfenum"
)

func (s *Service) sendCSharpSelectInitPacket(session *gameSession, packet csharpSelectInitPacket, body []byte) error {
	if packet.file != "" || historicalUpperBodyCodec(packet.bodyCodec) {
		s.logGameEvent(session, "game-upper-historical-scene-packet-blocked",
			"msg_id", packet.msgID,
			"classification", packet.class,
			"fixture_file", packet.file,
			"body_len", len(body),
			"body_codec", packet.bodyCodec,
			"reason", "scene_fixtures_are_ordering_evidence_only")
		return nil
	}
	if packet.kind == currentAcceptableQuestListKind {
		if session != nil && session.initialTownQuestSnapshotsSent {
			s.logGameEvent(session, "game-upper-current-acceptable-quest-list-duplicate-skipped",
				"char_id", session.selectedCharacterID,
				"msg_id", currentAcceptableQuestListMsgID,
				"source", "deferred_scene_tail",
				"reason", "initial_town_already_sent_op21_and_op574_before_typed_op24")
			return nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
		var ok bool
		body, ok = s.buildCurrentAcceptableQuestListBodyForSession(ctx, session)
		cancel()
		if !ok {
			return nil
		}
	}
	if packet.msgID == currentClearQuestListMsgID && packet.bodyCodec == currentClearQuestListTransportCodec {
		ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
		var err error
		body, err = s.buildCurrentClearQuestListTransportBodyForSession(ctx, session, "deferred_scene_template")
		cancel()
		if err != nil {
			return err
		}
	}
	if packet.msgID == uint16(dnfenum.CmdPacketRequestOverseer) && !packet.bodyEncoded && packet.marker == 0 && packet.bodyCodec == "" {
		ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
		body = s.buildCurrentRequestOverseerBodyForSession(ctx, session, body)
		cancel()
	}
	if packet.msgID == uint16(dnfenum.CmdPacketInsertOverseer) && !packet.bodyEncoded && packet.marker == 0 && packet.bodyCodec == "" {
		ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
		body = s.buildCurrentInsertOverseerBodyForSession(ctx, session, body)
		cancel()
	}
	if selectInitPacketRequiresCurrentSceneObject(packet) && (session == nil || !session.currentSceneObjectListSent) {
		if session != nil {
			s.logPacketEvent("game-upper-insert-overseer-finalizer-deferred",
				"conn_id", session.connID,
				"channel_id", session.channel.ID,
				"char_id", session.selectedCharacterID,
				"msg_id", packet.msgID,
				"classification", packet.class,
				"body_len", len(body),
				"reason", "current_scene_object_list_not_sent",
				"evidence", "fresh_crash_20260709_181307_after_msg359_sub_34DBE70")
		}
		return nil
	}
	var err error
	if packet.bodyEncoded || packet.marker != 0 || packet.bodyCodec != "" {
		err = s.sendGameUpperFixed16Transport(session, packet.msgID, body, packet.class, packet.marker, packet.bodyEncoded, packet.bodyCodec)
	} else {
		err = s.sendGameUpperRawClass(session, packet.msgID, body, packet.class)
	}
	if err != nil {
		return err
	}
	if packet.kind == currentAcceptableQuestListKind {
		if err := s.sendCurrentActiveQuestSnapshotForSession(session, "deferred_scene_tail_after_acceptable_op21"); err != nil {
			return err
		}
	}
	s.markCurrentSceneObjectListSent(session, packet, len(body))
	return nil
}

func (s *Service) markCurrentSceneObjectListSent(session *gameSession, packet csharpSelectInitPacket, bodyLen int) {
	if session == nil || packet.kind != csharpCurrentSceneObjectListKind {
		return
	}
	session.currentSceneObjectListSent = true
	s.logPacketEvent("game-upper-current-scene-object-list-sent",
		"conn_id", session.connID,
		"channel_id", session.channel.ID,
		"char_id", session.selectedCharacterID,
		"msg_id", packet.msgID,
		"classification", packet.class,
		"body_len", bodyLen,
		"body_codec", packet.bodyCodec,
		"reason", "current_object_stream_available_for_followup_model_layers")
}
