package dnfbridge

import (
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
)

func (s *Service) handleLegacyGamePacket(session *gameSession, raw []byte) error {
	packet, err := dnfproto.ParseLegacyGamePacket(raw)
	if err != nil {
		return err
	}
	body := normalizeLegacyGameBody(packet.Header.Type, packet.Body)
	runtimeName := runtimeCmdPacketName(packet.Header.Cmd, packet.Header.Type)
	runtimeKnown := runtimeCmdPacketKnown(packet.Header.Cmd, packet.Header.Type)
	s.logGamePacket(session, "RECV", "game-legacy", raw,
		"cmd", packet.Header.Cmd,
		"type", packet.Header.Type,
		"runtime_cmd_name", runtimeName,
		"runtime_cmd_known", runtimeKnown,
		"header_seq", packet.Header.Sequence,
		"body_len", len(packet.Body))
	s.logPacketEvent("game-legacy-meta",
		"conn_id", session.connID,
		"channel_id", session.channel.ID,
		"cmd", packet.Header.Cmd,
		"type", packet.Header.Type,
		"runtime_cmd_name", runtimeName,
		"runtime_cmd_known", runtimeKnown,
		"sequence", packet.Header.Sequence,
		"body_len", len(packet.Body),
		"dispatch_body_len", len(body))
	if len(body) != len(packet.Body) {
		s.logPacketEvent("game-legacy-body-normalized",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"type", packet.Header.Type,
			"runtime_cmd_name", runtimeName,
			"runtime_cmd_known", runtimeKnown,
			"raw_body_len", len(packet.Body),
			"dispatch_body_len", len(body))
	}
	return s.handleGameCommand(session, packet.Header.Cmd, packet.Header.Type, body)
}

func normalizeLegacyGameBody(typ uint16, body []byte) []byte {
	if normalized, ok := currentGameplayModules.NormalizeLegacy(typ, body); ok {
		return normalized
	}
	if dnfenum.GameType(typ) == dnfenum.GameTypeSelectCharacter {
		return stripLegacyCodecPrefix(body, 11)

	}
	return body
}

func stripLegacyBuySkillTransportTrailer(body []byte) []byte {
	const (
		headerLen  = 2
		entryLen   = 4
		tailLen    = 1
		trailerLen = 4
	)
	if len(body) < headerLen+tailLen+trailerLen {
		return body
	}
	semanticLen := headerLen + int(body[1])*entryLen + tailLen
	if len(body) != semanticLen+trailerLen {
		return body
	}
	return append([]byte(nil), body[:semanticLen]...)
}

func stripLegacyEmblemCompoundTransportTrailer(body []byte) []byte {
	if len(body) < 1+currentEmblemCompoundMinInputs*currentEmblemCompoundInputSize+4 {
		return body
	}
	count := int(body[0])
	semanticLength := 1 + count*currentEmblemCompoundInputSize
	if count < currentEmblemCompoundMinInputs || count > currentEmblemCompoundMaxInputs || len(body) != semanticLength+4 {
		return body
	}
	return append([]byte(nil), body[:semanticLength]...)
}

func stripLegacyCodecPrefix(body []byte, payloadLen int) []byte {
	if payloadLen <= 0 || len(body) != payloadLen+5 {
		return body
	}
	return append([]byte(nil), body[5:]...)
}

func stripLegacyTransportTrailer(body []byte, payloadLen int) []byte {
	const trailerLen = 4
	if payloadLen <= 0 || len(body) != payloadLen+trailerLen {
		return body
	}
	return append([]byte(nil), body[:payloadLen]...)
}

func normalizeLegacySetQuestTriggerBody(body []byte) []byte {
	const (
		opcodeEchoLow  = byte(dnfenum.CmdPacketSetQuestTrigger)
		opcodeEchoHigh = byte(uint16(dnfenum.CmdPacketSetQuestTrigger) >> 8)
	)
	if len(body) == currentSetQuestTriggerRequestBodySize+1 &&
		body[0] == opcodeEchoLow &&
		body[1] == opcodeEchoHigh &&
		body[len(body)-1] == 0 {
		return append([]byte(nil), body[:currentSetQuestTriggerRequestBodySize]...)
	}
	return stripLegacyTransportTrailer(body, currentSetQuestTriggerRequestBodySize)
}

func stripLegacyDieCharacterZeroTail(body []byte) []byte {
	const legacyTailLen = 2
	if len(body) != currentDungeonDieCharacterBodySize+legacyTailLen ||
		body[currentDungeonDieCharacterBodySize] != 0 ||
		body[currentDungeonDieCharacterBodySize+1] != 0 {
		return body
	}
	return append([]byte(nil), body[:currentDungeonDieCharacterBodySize]...)
}

func runtimeCmdPacketName(cmd byte, typ uint16) string {
	if dnfenum.GameCmd(cmd) != dnfenum.GameCmdCommand {
		return ""
	}
	return dnfenum.CmdPacketName(typ)
}

func runtimeCmdPacketKnown(cmd byte, typ uint16) bool {
	if dnfenum.GameCmd(cmd) != dnfenum.GameCmdCommand {
		return false
	}
	return dnfenum.IsKnownCmdPacket(typ)
}

func (s *Service) handleGameCommand(session *gameSession, cmd byte, typ uint16, body []byte) error {
	if dnfenum.GameCmd(cmd) != dnfenum.GameCmdCommand {
		return nil
	}
	if handled, err := currentGameplayModules.DispatchLegacy(s, session, typ, body); handled || err != nil {
		return err
	}
	switch dnfenum.GameType(typ) {
	case dnfenum.GameTypeLogin:
		// Current EXE evidence proves only the upper class1/op1 590/598-byte
		// request emitted after CHANNELINFO. Keep legacy-shaped op1 silent.
		s.logGameEvent(session, "game-legacy-endpoint-op1-ignored",
			"body_len", len(body),
			"reason", "legacy_endpoint_request_shape_not_proved")
		return nil
	case dnfenum.GameType(dnfenum.CmdPacketExit):
		// Current op3/one-byte channel-exit is structurally ambiguous with the
		// legacy frame and reaches this decoder in live traffic. It still owns
		// the same class1/op3 ACK as the upper route.
		return s.handleCurrentChannelExit(session, body, "legacy_plain_op3")
	case dnfenum.GameTypeGetUserInfo:
		if handled, err := s.handleCurrentPeerUserInfoRequest(session, body); handled || err != nil {
			return err
		}
		// The hidden-probe-deferred and old WSTR bootstrap experiments both
		// leave the current source-built upper-body-bypass client on a black
		// selector.  The user-verified visible role-select chain uses the
		// fixed15 character-list route for legacy GET_USERINFO; upper op8 keeps
		// the ordinary upper envelope.
		return s.sendLegacyGetUserInfoBootstrap(session)
	case dnfenum.GameTypeSelectCharacter:
		s.logInfo("dnfbridge latest select character received",
			"channel_id", session.channel.ID,
			"body_len", len(body))
		return s.sendSelectCharacterState(session, body)
	case dnfenum.GameTypeEnterSelectDungeon:
		s.logInfo("dnfbridge latest enter-select-dungeon received",
			"channel_id", session.channel.ID,
			"body_len", len(body))
		return s.sendEnterSelectDungeonState(session, "game_cmd_15", true, true)

	case dnfenum.GameTypeFinishLoading:
		return s.sendFinishLoadingStatus(session, body)
	case dnfenum.GameType(dnfenum.CmdPacketAgitWarInfo):
		// Live current-client traffic sends legacy type1345 with u32(2)
		// immediately after the first typed op24. It completes the deferred
		// scene tail and HUD gauge only. Replaying actor/finish-loading state at
		// this callback leaves the current EXE update loop with a null actor.
		if !isCurrentLegacyTownSceneReadyAck(body) {
			s.logPacketEvent("game-legacy-town-scene-ready-ack-deferred",
				"conn_id", session.connID,
				"channel_id", session.channel.ID,
				"type", typ,
				"body_len", len(body),
				"reason", "current_exe_type1345_u32_2_shape_required")
			return nil
		}
		s.logGameEvent(session, "game-legacy-town-scene-ready-ack-accepted",
			"type", typ,
			"body_len", len(body),
			"boundary", "after_typed_op24_deferred_tail_and_hud_gauge_only")
		return s.sendCurrentLegacyTownSceneReadyBoundary(session, "legacy_type1345_scene_ready_ack")
	case dnfenum.GameTypeContentsPlayInfo:
		return s.sendContentsPlayInfoState(session, body)
	case dnfenum.GameType(dnfenum.CmdPacketRequestBlacklist):
		return s.sendRequestBlacklistState(session, "legacy_120_request", body)
	case dnfenum.GameType(dnfenum.CmdPacketGuildAllMemberList):
		return s.sendGuildAllMemberListState(session, "legacy_140_request", body)

	case dnfenum.GameType(dnfenum.CmdPacketFpsDevideCollect):
		return s.handleGameFpsDevideCollect(session, body)
	case dnfenum.GameType(dnfenum.CmdPacketInformNotice2nd), dnfenum.GameType(dnfenum.CmdPacketOverflowInfo):
		s.logPacketEvent("game-legacy-passive-report-deferred",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"type", typ,
			"runtime_cmd_name", runtimeCmdPacketName(cmd, typ),
			"body_len", len(body),
			"reason", "client_runtime_report_no_reference_s2c_ack")
		return nil
	case dnfenum.GameTypeCreateCharacter:
		return s.handleGameCreateCharacter(session, body)
	case dnfenum.GameTypeStaticsRuntimeTing:
		s.logInfo("dnfbridge latest statics runtime report received; no response",
			"type", typ,
			"runtime_cmd_name", runtimeCmdPacketName(cmd, typ),
			"body_len", len(body))
		return nil
	case dnfenum.GameTypeCheckName:
		return s.handleGameCheckName(session, body)
	case dnfenum.GameType(dnfenum.CmdPacketChangeCharacSlot):
		return s.handleChangeCharacterSlot(session, body, false)
	case dnfenum.GameType(currentCmdRepresentNameDuplicateCheck):
		return s.handleRepresentAccountNameDuplicateCheck(session, body)
	case dnfenum.GameType(currentCmdUpdateRepresentAccountName), dnfenum.GameType(currentCmdChangeRepresentAccountName):
		return s.handleUpdateRepresentAccountName(session, body, typ)
	case dnfenum.GameType(dnfenum.CmdPacketStoryDigestUpdate):
		// The current client reaches bodyless op1445 through the legacy game
		// decoder even when the semantic command is a class-1 upper command.
		// Keep one owner for both transports so the accepted story digest is
		// advanced monotonically instead of replaying on every login.
		return s.handleCurrentStoryDigestAccepted(session, dnfproto.DefaultChannelClassification, body)

	default:
		if handled, err := s.handleAlignedGameCommand(session, cmd, typ, body); handled || err != nil {
			return err
		}
		s.logInfo("dnfbridge unhandled latest game command",
			"cmd", cmd,
			"type", typ,
			"runtime_cmd_name", runtimeCmdPacketName(cmd, typ),
			"runtime_cmd_known", runtimeCmdPacketKnown(cmd, typ),
			"body_len", len(body))
	}
	return nil
}

func isCurrentLegacyTownSceneReadyAck(body []byte) bool {
	return len(body) == 4 &&
		body[0] == 2 &&
		body[1] == 0 &&
		body[2] == 0 &&
		body[3] == 0
}
