package dnfbridge

import (
	"encoding/binary"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
)

func (s *Service) handleGameUpper(session *gameSession, raw []byte) error {
	packet, err := dnfproto.ParseChannelPacketUnchecked(raw)
	if err != nil {
		return err
	}
	runtimeName := upperRuntimeCmdPacketName(packet.Header.MsgID, packet.Header.Classification)
	runtimeKnown := upperRuntimeCmdPacketKnown(packet.Header.MsgID, packet.Header.Classification)
	wireBodyLen := len(packet.Body)
	channelReconnectLifecycleBody := isCurrentChannelReconnectLifecycleBody(
		packet.Header.MsgID,
		packet.Header.Classification,
		wireBodyLen,
	)
	clientBodyCodecMode := s.gameUpperClientBodyCodecMode()
	clientBodyCodec := gameUpperClientBodyCodecPlain
	clientBodyDecoded := false
	// Current live quest replays use the plaintext upper body mode. Once the
	// exact terminal request has been acknowledged in this TCP session, skip it
	// before packet logging as well as before the owner. Otherwise a client
	// retry storm turns into synchronous disk I/O even when the handler replies
	// silently.
	if clientBodyCodecMode == gameUpperClientBodyCodecPlain &&
		shouldSuppressKnownQuestReplayBeforeGameUpperLog(session, packet.Header.MsgID, packet.Header.Classification, packet.Body) {
		return nil
	}
	s.logGamePacket(session, "RECV", "game-upper", raw,
		"msg_id", packet.Header.MsgID,
		"runtime_cmd_name", runtimeName,
		"runtime_cmd_known", runtimeKnown,
		"classification", packet.Header.Classification,
		"header_seq", packet.Header.Seq,
		"wire_body_len", wireBodyLen,
		"client_body_codec_mode", clientBodyCodecMode)
	if !channelReconnectLifecycleBody &&
		clientBodyCodecMode != gameUpperClientBodyCodecPlain &&
		packet.Header.Classification == dnfproto.DefaultChannelClassification &&
		len(packet.Body) > 0 {
		decodedBody, codecName, supported, decodeErr := decodeCurrentUpperClientBody(packet.Header.MsgID, packet.Body)
		if supported && decodeErr == nil {
			s.logPacketEvent("game-upper-client-body-decode",
				"conn_id", session.connID,
				"channel_id", session.channel.ID,
				"msg_id", packet.Header.MsgID,
				"runtime_cmd_name", runtimeName,
				"runtime_cmd_known", runtimeKnown,
				"classification", packet.Header.Classification,
				"sequence", packet.Header.Seq,
				"mode", clientBodyCodecMode,
				"codec", codecName,
				"wire_body_len", wireBodyLen,
				"decoded_body_len", len(decodedBody))
			if clientBodyCodecMode == gameUpperClientBodyCodecNative {
				packet.Body = decodedBody
				clientBodyCodec = codecName
				clientBodyDecoded = true
			}
		} else {
			reason := "unsupported_codec_slot"
			codecForLog := codecName
			if supported && decodeErr != nil {
				reason = decodeErr.Error()
			}
			allowOpaqueControl := shouldAllowOpaqueUpperClientBodyControl(packet.Header.MsgID, packet.Header.Classification, supported, decodeErr)
			s.logPacketEvent("game-upper-client-body-decode-deferred",
				"conn_id", session.connID,
				"channel_id", session.channel.ID,
				"msg_id", packet.Header.MsgID,
				"runtime_cmd_name", runtimeName,
				"runtime_cmd_known", runtimeKnown,
				"classification", packet.Header.Classification,
				"sequence", packet.Header.Seq,
				"mode", clientBodyCodecMode,
				"codec", codecForLog,
				"wire_body_len", wireBodyLen,
				"reason", reason,
				"opaque_control_allowed", allowOpaqueControl)
			if clientBodyCodecMode == gameUpperClientBodyCodecNative {
				if allowOpaqueControl {
					clientBodyCodec = "idx6-kasumi-opaque-control"
					s.logPacketEvent("game-upper-client-body-opaque-control",
						"conn_id", session.connID,
						"channel_id", session.channel.ID,
						"msg_id", packet.Header.MsgID,
						"runtime_cmd_name", runtimeName,
						"runtime_cmd_known", runtimeKnown,
						"classification", packet.Header.Classification,
						"sequence", packet.Header.Seq,
						"mode", clientBodyCodecMode,
						"codec", clientBodyCodec,
						"wire_body_len", wireBodyLen,
						"reason", "ida_1518_body_not_consumed_current_live_idx6_key_is_instance_local")
				} else {
					return nil
				}
			}
		}
	}
	s.logPacketEvent("game-upper-meta",
		"conn_id", session.connID,
		"channel_id", session.channel.ID,
		"msg_id", packet.Header.MsgID,
		"runtime_cmd_name", runtimeName,
		"runtime_cmd_known", runtimeKnown,
		"classification", packet.Header.Classification,
		"sequence", packet.Header.Seq,
		"wire_body_len", wireBodyLen,
		"body_len", len(packet.Body),
		"client_body_codec_mode", clientBodyCodecMode,
		"body_decoded", clientBodyDecoded,
		"body_codec", clientBodyCodec)
	if handled, err := currentGameplayModules.DispatchUpper(
		s,
		session,
		packet.Header.MsgID,
		packet.Header.Classification,
		packet.Body,
	); handled || err != nil {
		return err
	}
	switch dnfenum.UpperMsg(packet.Header.MsgID) {
	case dnfenum.UpperMsgGameEndpoint:
		return s.handleCurrentGameEndpointRequest(session, packet.Header.Classification, wireBodyLen)
	case dnfenum.UpperMsgCharacterRoster:
		if packet.Header.Classification == dnfproto.DefaultChannelClassification &&
			wireBodyLen == currentChannelReconnectProbeSize {
			// This 31-byte op2 is also the current EXE's dynamic P2P UDP
			// endpoint registration. Retain the reconnect lifecycle, but do
			// not discard the port required by op11 party peer exchange.
			s.captureCurrentPartyPeerEndpointRegistration(session, packet.Body)
			if markCurrentChannelReconnect(session, wireBodyLen) {
				s.logGameEvent(session, "game-channel-reconnect-detected",
					"source", "op2_31_byte_probe",
					"body_len", wireBodyLen,
					"target_channel", session.channel.ID)
			}
		}
		return nil
	case dnfenum.UpperMsgSelectCharacter:
		if markCurrentChannelReconnectOnSelection(session) {
			s.logGameEvent(session, "game-channel-reconnect-detected",
				"source", "op4_without_roster",
				"body_len", wireBodyLen,
				"target_channel", session.channel.ID)
		}
		s.logInfo("dnfbridge latest upper select character received; sending csharp init stream",
			"msg_id", packet.Header.MsgID,
			"body_len", len(packet.Body))
		s.logPacketEvent("game-upper-select-character-csharp-init",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"msg_id", packet.Header.MsgID,
			"runtime_cmd_name", runtimeName,
			"runtime_cmd_known", runtimeKnown,
			"body_len", len(packet.Body))
		return s.sendUpperCSharpSelectInit(session, packet.Body)
	case dnfenum.UpperMsgGetUserInfo:
		session.rosterRequested = true
		s.logInfo("dnfbridge latest upper get-userinfo received; sending roster",
			"msg_id", packet.Header.MsgID,
			"runtime_cmd_name", runtimeName,
			"body_len", len(packet.Body))
		s.logPacketEvent("game-upper-get-userinfo-bootstrap",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"msg_id", packet.Header.MsgID,
			"runtime_cmd_name", runtimeName,
			"runtime_cmd_known", runtimeKnown,
			"body_len", len(packet.Body))
		return s.sendUpperGetUserInfoBootstrap(session)
	case dnfenum.UpperMsgCharacViewHiddenInfo:
		s.logInfo("dnfbridge latest upper hidden character info probe received",
			"msg_id", packet.Header.MsgID,
			"runtime_cmd_name", runtimeName,
			"body_len", len(packet.Body))
		s.logPacketEvent("game-upper-hidden-charac-info-rebirth-hardcore-success",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"msg_id", packet.Header.MsgID,
			"runtime_cmd_name", runtimeName,
			"runtime_cmd_known", runtimeKnown,
			"body_len", len(packet.Body),
			"response_msg_id", uint16(dnfenum.UpperMsgRebirthHardcoreCharac),
			"reason", "snapshot_112000_646_success_body")
		if err := s.sendGameUpperSuccess(session, uint16(dnfenum.UpperMsgRebirthHardcoreCharac), nil); err != nil {
			return err
		}
		if session.pendingCharacterRosterBootstrap {
			session.pendingCharacterRosterBootstrap = false
			s.logGameEvent(session, "game-upper-hidden-charac-info-flush-pending-roster",
				"source", "charac_view_hidden_info",
				"reason", "legacy_getuserinfo_roster_waited_for_hidden_probe")
			if err := s.sendUpperGetUserInfoRosterBootstrapFixed16(session); err != nil {
				return err
			}
		}
		return s.sendCurrentSelectorAdventureInfoAfterHiddenProbe(session)
	case dnfenum.UpperMsgCreateCharacter:
		return s.handleUpperCreateCharacter(session, packet.Body)
	case dnfenum.UpperMsgCheckDoubleCharName:
		return s.handleGameCheckName(session, packet.Body)
	case dnfenum.UpperMsg(dnfenum.CmdPacketChangeCharacSlot):
		if packet.Header.Classification != dnfproto.DefaultChannelClassification {
			s.logGameEvent(session, "game-character-slot-change-blocked",
				"classification", packet.Header.Classification,
				"body_len", len(packet.Body),
				"reason", "current_exe_command_class_mismatch")
			return nil
		}
		return s.handleChangeCharacterSlot(session, packet.Body, true)
	case dnfenum.UpperMsg(currentCmdRepresentNameDuplicateCheck):
		if packet.Header.Classification != dnfproto.DefaultChannelClassification {
			s.logGameEvent(session, "game-represent-account-name-duplicate-check-blocked",
				"classification", packet.Header.Classification,
				"body_len", len(packet.Body),
				"reason", "current_exe_command_class_mismatch")
			return nil
		}
		return s.handleRepresentAccountNameDuplicateCheck(session, packet.Body)
	case dnfenum.UpperMsg(currentCmdUpdateRepresentAccountName), dnfenum.UpperMsg(currentCmdChangeRepresentAccountName):
		if packet.Header.Classification != dnfproto.DefaultChannelClassification {
			s.logGameEvent(session, "game-represent-account-name-update-blocked",
				"classification", packet.Header.Classification,
				"msg_id", packet.Header.MsgID,
				"body_len", len(packet.Body),
				"reason", "current_exe_command_class_mismatch")
			return nil
		}
		return s.handleUpdateRepresentAccountName(session, packet.Body, packet.Header.MsgID)
	case dnfenum.UpperMsg(dnfenum.CmdPacketStoryDigestUpdate):
		return s.handleCurrentStoryDigestAccepted(session, packet.Header.Classification, packet.Body)
	case dnfenum.UpperMsgSelectAck:
		// UpperMsgSelectAck is the historical bridge name for current
		// ENUM_CMDPACKET_RETURN_SELECT_CHARACTER (op7).  A bare ACK leaves the
		// client bound to the old selected actor and never rebuilds the roster.
		if packet.Header.Classification != dnfproto.DefaultChannelClassification {
			s.logGameEvent(session, "game-upper-return-select-character-blocked",
				"classification", packet.Header.Classification,
				"body_len", len(packet.Body),
				"reason", "current_exe_op7_command_class_mismatch")
			return nil
		}
		return s.handleUpperReturnSelectCharacter(session, packet.Body)
	case dnfenum.UpperMsgFollowUpStatus:
		s.logInfo("dnfbridge latest upper follow-up received", "msg_id", packet.Header.MsgID, "body_len", len(packet.Body))
		// 63 在当前枚举里是 Cera 查询，不是进场景 gate；用它触发 DOVE tail 会让 HUD 包早于客户端 runtime 阶段。
		return s.sendGameUpperSuccess(session, packet.Header.MsgID, nil)

	case dnfenum.UpperMsgFollowUpReady:
		return s.sendRequestBlacklistState(session, "upper_120_request", packet.Body)
	case dnfenum.UpperMsg(dnfenum.CmdPacketGuildAllMemberList):
		return s.sendGuildAllMemberListState(session, "upper_140_request", packet.Body)
	case dnfenum.UpperMsgLoadExtendCharacs:
		s.logInfo("dnfbridge latest upper load-extend-characs received",
			"msg_id", packet.Header.MsgID,
			"runtime_cmd_name", runtimeName,
			"body_len", len(packet.Body))
		if packet.Header.Classification == dnfproto.DefaultChannelClassification &&
			session.emptyRosterSlotProbePending &&
			len(packet.Body) == 2 &&
			binary.LittleEndian.Uint16(packet.Body) == upperMaxCharacter {
			session.emptyRosterSlotProbePending = false
			s.logPacketEvent("game-upper-load-extend-characs-empty-roster-complete",
				"conn_id", session.connID,
				"channel_id", session.channel.ID,
				"msg_id", packet.Header.MsgID,
				"runtime_cmd_name", runtimeName,
				"runtime_cmd_known", runtimeKnown,
				"body_len", len(packet.Body),
				"requested_slot", upperMaxCharacter,
				"reason", "current_exe_empty_roster_ready_handshake")
			// Current EXE mode-2 roster finalization uses UINT16_MAX as the
			// selected slot when there are no rows. It clears selector ready
			// while class1/op679 is outstanding; the class-1 failure result
			// completes that empty-page probe and restores the Create button.
			// Class0/op679 is a different {u32 count, repeated u32 pair}
			// parser and must not receive this common-result envelope.
			return s.sendGameUpperFailure(session, uint16(dnfenum.UpperMsgLoadExtendCharacs), 0)
		}
		s.logPacketEvent("game-upper-load-extend-characs-deferred",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"msg_id", packet.Header.MsgID,
			"runtime_cmd_name", runtimeName,
			"runtime_cmd_known", runtimeKnown,
			"body_len", len(packet.Body),
			"reason", "character_slot_bar_must_not_push_unlock_state")
		// 679 is the client character-slot extension query. Without a pending
		// database-backed extension state, keep the original silent behavior;
		// an invented failure response mutates the selector's slot presentation.
		return nil
	case dnfenum.UpperMsgCharacSlotExtendEffect:
		return s.handleUpperCharacSlotExtendEffect(session, packet.Body)
	case dnfenum.UpperMsgCreatePostState:
		return s.handleUpperCreatePostState(session, packet.Body)
	case dnfenum.UpperMsgStaticsRuntimeTing:
		s.logInfo("dnfbridge latest upper statics runtime report received",
			"msg_id", packet.Header.MsgID,
			"runtime_cmd_name", runtimeName,
			"body_len", len(packet.Body))
		s.logPacketEvent("game-upper-statics-runtime-ting-deferred",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"msg_id", packet.Header.MsgID,
			"runtime_cmd_name", runtimeName,
			"runtime_cmd_known", runtimeKnown,
			"body_len", len(packet.Body),
			"reason", "dove_login_endpoint_already_sent")
		return nil
	case dnfenum.UpperMsgDprotoCallback:
		if session.dproto != nil {
			s.logPacketEvent("game-upper-dproto-callback-native",
				"conn_id", session.connID,
				"channel_id", session.channel.ID,
				"msg_id", packet.Header.MsgID,
				"body_len", len(packet.Body))
			if err := s.handleGameDprotoControl(session, packet.Header.MsgID, raw); err != nil {
				return err
			}
			return s.sendCurrentSelectSceneReadyBoundary(session, "upper_dproto_native_control")
		}
		s.logInfo("dnfbridge latest upper dproto callback received; sending empty success",
			"msg_id", packet.Header.MsgID,
			"runtime_cmd_name", runtimeName,
			"body_len", len(packet.Body))
		s.logPacketEvent("game-upper-dproto-callback-empty-success",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"msg_id", packet.Header.MsgID,
			"runtime_cmd_name", runtimeName,
			"runtime_cmd_known", runtimeKnown,
			"body_len", len(packet.Body),
			"reason", "ida_1518_success_empty_count")
		if err := s.sendUpperDprotoCallbackSuccess(session); err != nil {
			return err
		}
		return s.sendCurrentSelectSceneReadyBoundary(session, "upper_dproto_callback_after_ack")
	case dnfenum.UpperMsgAntibot:
		s.logInfo("dnfbridge latest upper antibot report received; deferring response",
			"msg_id", packet.Header.MsgID,
			"runtime_cmd_name", runtimeName,
			"body_len", len(packet.Body))
		s.logPacketEvent("game-upper-antibot-report-deferred",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"msg_id", packet.Header.MsgID,
			"runtime_cmd_name", runtimeName,
			"runtime_cmd_known", runtimeKnown,
			"body_len", len(packet.Body),
			"reason", "client_report_no_reference_s2c_ack")
		// 1516 在这里是客户端登录阶段的 antibot 上报；IDA sub_1D0EA20 只证明服务端主动发 1516 时客户端如何处理。
		// 最新实测中额外回 1516 会立刻触发 reset，因此按 DOVE 登录参考流静默接收。
		return nil
	case dnfenum.UpperMsgCheckCharacterGate:
		return s.handleUpperCheckCharacterGate(session, packet.Body)
	case dnfenum.UpperMsgCheckUserConnection:
		// DOVE 登录到选角参考流没有服务端 1276 ACK；旧探针成功包会让当前客户端在角色列表阶段断线。
		// 选角成功后 NoPack 会持续发送 1276 等待后续进场门闩。这时返回当前
		// EXE 成功处理器完整消费的三个中性 u32，不能再使用只有 success 的短包。
		if session.selectedCharacterID != 0 {
			s.logPacketEvent("game-upper-check-user-connection-selected-success",
				"conn_id", session.connID,
				"channel_id", session.channel.ID,
				"msg_id", packet.Header.MsgID,
				"runtime_cmd_name", runtimeName,
				"runtime_cmd_known", runtimeKnown,
				"body_len", len(packet.Body),
				"char_id", session.selectedCharacterID)
			if err := s.sendUpperCheckUserConnectionSuccess(session); err != nil {
				return err
			}
			if session.channelReconnect {
				s.logGameEvent(session, "game-channel-reconnect-check-user-connection-ack-only",
					"char_id", session.selectedCharacterID,
					"route_pending", session.channelReconnect)
				return nil
			}
			return s.sendCurrentSelectSceneReadyBoundary(session, "upper_check_user_connection_after_ack")
		}
		s.logPacketEvent("game-upper-check-user-connection-deferred",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"msg_id", packet.Header.MsgID,
			"runtime_cmd_name", runtimeName,
			"runtime_cmd_known", runtimeKnown,
			"body_len", len(packet.Body))
		return nil
	case dnfenum.UpperMsgSelectStart:
		return s.sendEnterSelectDungeonState(session, "upper_15", false, true)

	default:
		if packet.Header.Classification == dnfproto.DefaultChannelClassification {
			if handled, err := s.handleAlignedGameCommand(session, byte(dnfenum.GameCmdCommand), packet.Header.MsgID, packet.Body); handled || err != nil {
				if err != nil {
					return err
				}
				s.logPacketEvent("game-upper-aligned-fallback-handled",
					"conn_id", session.connID,
					"channel_id", session.channel.ID,
					"msg_id", packet.Header.MsgID,
					"runtime_cmd_name", runtimeName,
					"runtime_cmd_known", runtimeKnown,
					"body_len", len(packet.Body),
					"reason", "unlisted_upper_command_routed_to_server_aligned_owner")
				return nil
			}
		}
		s.logInfo("dnfbridge unhandled latest upper packet",
			"msg_id", packet.Header.MsgID,
			"runtime_cmd_name", runtimeName,
			"runtime_cmd_known", runtimeKnown,
			"body_len", len(packet.Body))
		return nil
	}
}

func shouldAllowOpaqueUpperClientBodyControl(msgID uint16, classification byte, supported bool, decodeErr error) bool {
	if supported || decodeErr != nil {
		return false
	}
	if classification != dnfproto.DefaultChannelClassification {
		return false
	}
	if dnfenum.UpperMsg(msgID) != dnfenum.UpperMsgDprotoCallback {
		return false
	}
	return msgID%14 == 6
}

func (s *Service) sendUpperDprotoCallbackSuccess(session *gameSession) error {
	// IDA sub_1D2B020 显示 op1518 成功分支读取 success 后的 u8 count、u32、u32、u8。
	// 当前登录阶段没有 dproto 条目，返回 count=0 的最小成功体，避免客户端等待回包后主动断开。
	return s.sendGameUpperRawClassNoCodec(session, uint16(dnfenum.UpperMsgDprotoCallback), emptyDprotoCallbackBody, dnfproto.DefaultChannelClassification)
}

const currentCheckUserConnectionSuccessPayloadSize = 12

func buildCurrentCheckUserConnectionSuccessPayload() []byte {
	// Current EXE sub_1D0C760 is registered as extended opcode 0x4FC. Its
	// success branch unconditionally consumes three u32 values and discards
	// them. There is no persisted owner, so the exact neutral state is 0/0/0.
	return make([]byte, currentCheckUserConnectionSuccessPayloadSize)
}

func (s *Service) sendUpperCheckUserConnectionSuccess(session *gameSession) error {
	return s.sendGameUpperSuccess(
		session,
		uint16(dnfenum.UpperMsgCheckUserConnection),
		buildCurrentCheckUserConnectionSuccessPayload(),
	)
}

func upperRuntimeCmdPacketName(msgID uint16, classification byte) string {
	if classification != dnfproto.DefaultChannelClassification {
		return ""
	}
	return dnfenum.CmdPacketName(msgID)
}

func upperRuntimeCmdPacketKnown(msgID uint16, classification byte) bool {
	if classification != dnfproto.DefaultChannelClassification {
		return false
	}
	return dnfenum.IsKnownCmdPacket(msgID)
}

// handleUpperCharacSlotExtendEffect 处理客户端自动探测的扩栏光效消费请求。
// 默认 8 栏由 roster 开放；没有道具/DB 产生的待消费扩栏状态时不能回成功 ACK，否则客户端每次打开选角页都会播放解锁光效。
func (s *Service) handleUpperCharacSlotExtendEffect(session *gameSession, body []byte) error {
	s.logGameEvent(session, "game-upper-charac-slot-extend-effect-deferred",
		"body_len", len(body),
		"reason", "no_pending_slot_unlock")
	return nil
}

var currentUpper689RequestLengths = map[uint32]int{
	222:  16, // create-character post state: discriminator, character ID, 0xff, 0
	349:  12,
	2099: 16,
	2208: 16,
	2384: 16,
	2427: 12,
	2428: 8,
	2443: 4,
}

func (s *Service) handleUpperCreatePostState(session *gameSession, body []byte) error {
	// Current EXE uses class1/op689 as a multiplexed request.  Every current
	// writer starts with a u32 discriminator, and sub_1D019A0 consumes that
	// same discriminator after the common success byte.  Echoing a hard-coded
	// 0xde is correct only for the create-character branch and corrupts every
	// other current op689 request.
	if len(body) < 4 {
		s.logGameEvent(session, "game-upper-multiplexed-state-malformed",
			"msg_id", uint16(dnfenum.UpperMsgCreatePostState),
			"body_len", len(body),
			"reason", "missing_u32_discriminator")
		return nil
	}
	discriminator := binary.LittleEndian.Uint32(body[:4])
	expectedLength, known := currentUpper689RequestLengths[discriminator]
	if !known || len(body) != expectedLength {
		s.logGameEvent(session, "game-upper-multiplexed-state-deferred",
			"msg_id", uint16(dnfenum.UpperMsgCreatePostState),
			"discriminator", discriminator,
			"body_len", len(body),
			"expected_body_len", expectedLength,
			"known_discriminator", known,
			"reason", "current_exe_request_shape_not_proved")
		return nil
	}
	var writer packetWriter
	writer.writeUint32(discriminator)
	return s.sendGameUpperSuccess(session, uint16(dnfenum.UpperMsgCreatePostState), writer.bytes())
}

func (s *Service) handleUpperCheckCharacterGate(session *gameSession, body []byte) error {
	// sub_342D480 is the sole current writer: exactly two u32 values.  The
	// class1/op693 success handler sub_1CFA0D0 consumes only the first value
	// and treats 0xffffffff as the inactive sentinel.  Zero is not a sentinel,
	// so never substitute the second value or a fabricated default.
	if len(body) != 8 {
		s.logGameEvent(session, "game-upper-check-character-gate-malformed",
			"msg_id", uint16(dnfenum.UpperMsgCheckCharacterGate),
			"body_len", len(body),
			"reason", "current_exe_writer_requires_two_u32")
		return nil
	}
	candidate := binary.LittleEndian.Uint32(body[:4])
	pair := binary.LittleEndian.Uint32(body[4:8])
	if candidate == ^uint32(0) || pair == ^uint32(0) {
		s.logGameEvent(session, "game-upper-check-character-gate-deferred",
			"msg_id", uint16(dnfenum.UpperMsgCheckCharacterGate),
			"candidate", candidate,
			"pair", pair,
			"reason", "current_exe_lookup_inactive_sentinel")
		return nil
	}
	var writer packetWriter
	writer.writeUint32(candidate)
	return s.sendGameUpperSuccess(session, uint16(dnfenum.UpperMsgCheckCharacterGate), writer.bytes())
}
