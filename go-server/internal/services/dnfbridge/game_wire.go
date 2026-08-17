package dnfbridge

import (
	"encoding/binary"
	"strings"
	"sync/atomic"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
)

func (s *Service) handleGameFrame(session *gameSession, frame []byte) error {
	records, err := dnfproto.ParseLatestGameTCPRecords(frame)
	if err != nil {
		return err
	}
	for _, record := range records {
		runtimeName := runtimeCmdPacketName(record.GameHeader.Cmd, record.GameHeader.Type)
		runtimeKnown := runtimeCmdPacketKnown(record.GameHeader.Cmd, record.GameHeader.Type)
		s.logGamePacket(session, "RECV", "game", frame,
			"cmd", record.GameHeader.Cmd,
			"type", record.GameHeader.Type,
			"runtime_cmd_name", runtimeName,
			"runtime_cmd_known", runtimeKnown,
			"body_len", len(record.Body))
		s.logPacketEvent("game-transport-meta",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"cmd", record.GameHeader.Cmd,
			"type", record.GameHeader.Type,
			"runtime_cmd_name", runtimeName,
			"runtime_cmd_known", runtimeKnown,
			"body_len", len(record.Body),
			"frame_len", len(frame))
		if err := s.handleGameCommand(session, record.GameHeader.Cmd, record.GameHeader.Type, record.Body); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) sendGame(session *gameSession, cmd byte, typ uint16, body []byte) error {
	return s.sendGameWithTransport(session, cmd, typ, body, dnfproto.TransportOptions{})
}

// sendGameFixed15Route is reserved for the legacy GET_USERINFO character
// roster response.  That exchange is received with the old game header but
// its S2C NOTI is read by the fixed-15 server-header path; wrapping it again
// in the 12+9 latest transport shifts the route and business body boundaries.
func (s *Service) sendGameFixed15Route(session *gameSession, cmd byte, typ uint16, body []byte, route byte) error {
	return s.withGameWire(session, func() error {
		frame, err := dnfproto.BuildFixed15GameServerPacket(cmd, typ, body, route)
		if err != nil {
			return err
		}
		sequence := session.sequence
		session.sequence++
		s.logPacketEvent("game-fixed15-send-meta",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"cmd", cmd,
			"type", typ,
			"sequence", sequence,
			"route", route,
			"body_len", len(body),
			"frame_len", len(frame))
		s.logGamePacket(session, "SEND", "game-fixed15", frame,
			"cmd", cmd,
			"type", typ,
			"header_seq", sequence,
			"body_len", len(body))
		return s.writeGameRawPacketLocked(session, frame)
	})
}

func (s *Service) sendGameWithTransport(session *gameSession, cmd byte, typ uint16, body []byte, options dnfproto.TransportOptions) error {
	return s.withGameWire(session, func() error {
		sequence := session.sequence
		options.Sequence = sequence
		options.OuterValue8 = s.options.gameOuterToken
		frame, err := dnfproto.BuildLatestGameTCP(cmd, typ, body, dnfproto.TransportOptions{
			Sequence:    options.Sequence,
			Route:       options.Route,
			Flag:        options.Flag,
			OuterKind:   options.OuterKind,
			OuterValue4: options.OuterValue4,
			OuterValue8: options.OuterValue8,
		})
		if err != nil {
			return err
		}
		session.sequence++
		s.logPacketEvent("game-send-meta",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"cmd", cmd,
			"type", typ,
			"sequence", sequence,
			"route", options.Route,
			"flag", options.Flag,
			"outer_value4", options.OuterValue4,
			"outer_value8", options.OuterValue8,
			"body_len", len(body),
			"frame_len", len(frame))
		s.logGamePacket(session, "SEND", "game", frame,
			"cmd", cmd,
			"type", typ,
			"header_seq", sequence,
			"body_len", len(body))
		return s.writeGameRawPacketLocked(session, frame)
	})
}

func historicalUpperBodyCodec(bodyCodec string) bool {
	normalized := strings.ToLower(strings.TrimSpace(bodyCodec))
	for _, forbidden := range []string{"dove", "fixture", "capture", "replay", "pcap", "reference_s2c"} {
		if strings.Contains(normalized, forbidden) {
			return true
		}
	}
	return false
}

func (s *Service) sendGameUpperFixed16Transport(session *gameSession, msgID uint16, body []byte, classification byte, marker uint32, bodyEncoded bool, bodyCodec string) error {
	if historicalUpperBodyCodec(bodyCodec) {
		s.logGameEvent(session, "game-upper-historical-body-blocked",
			"msg_id", msgID,
			"classification", classification,
			"body_len", len(body),
			"body_codec", bodyCodec,
			"reason", "captured_or_dove_derived_body_is_not_a_production_packet_source")
		return nil
	}
	const headerSize = dnfproto.GameServerUpperHeaderSize16
	if bodyCodec == "" {
		bodyCodec = "current_fixed16_plain"
	}
	packet := make([]byte, headerSize+len(body))
	packet[0] = classification
	binary.LittleEndian.PutUint16(packet[1:3], msgID)
	binary.LittleEndian.PutUint32(packet[3:7], uint32(len(packet)))
	binary.LittleEndian.PutUint32(packet[7:11], marker)
	// MCP/IDA sub_2261CB0: 16 字节 upper 头的第 15 偏移低位才是 zlib/保护层展开标志。
	// 这里仍保留 header[7..10] 的历史 marker 日志字段，但真实触发客户端展开必须写 packet[15]。
	packet[15] = byte(marker)
	copy(packet[headerSize:], body)
	runtimeName := upperRuntimeCmdPacketName(msgID, classification)
	runtimeKnown := upperRuntimeCmdPacketKnown(msgID, classification)
	s.logPacketEvent("game-upper-send-meta",
		"conn_id", session.connID,
		"channel_id", session.channel.ID,
		"msg_id", msgID,
		"runtime_cmd_name", runtimeName,
		"runtime_cmd_known", runtimeKnown,
		"classification", classification,
		"sequence", 0,
		"header_size", headerSize,
		"header_marker", marker,
		"plain_body_len", len(body),
		"body_len", len(body),
		"body_encoded", bodyEncoded,
		"body_codec", bodyCodec,
		"packet_len", len(packet))
	s.logGamePacket(session, "SEND", "game-upper", packet,
		"msg_id", msgID,
		"runtime_cmd_name", runtimeName,
		"runtime_cmd_known", runtimeKnown,
		"classification", classification,
		"header_seq", 0,
		"header_size", headerSize,
		"header_marker", marker,
		"plain_body_len", len(body),
		"body_len", len(body),
		"body_encoded", bodyEncoded,
		"body_codec", bodyCodec)
	return s.writeGameUpperPacket(session, packet)
}

func (s *Service) sendGameUpperRaw(session *gameSession, msgID uint16, body []byte) error {
	return s.sendGameUpperRawClass(session, msgID, body, dnfproto.DefaultChannelClassification)
}

func (s *Service) sendGameUpperSecondaryRaw(session *gameSession, msgID uint16, body []byte) error {
	return s.sendGameUpperRawClass(session, msgID, body, 0)
}

func (s *Service) sendGameUpperCharacterRosterRaw(session *gameSession, body []byte) error {
	return s.withGameWire(session, func() error {
		const classification byte = 0
		msgID := uint16(2)
		sequence := session.upperSeq
		headerSize := s.gameUpperHeaderSize()
		wireBody := append([]byte(nil), body...)
		bodyEncoded := false
		bodyCodec := gameUpperBodyCodecPlain
		packet, err := dnfproto.BuildGameServerUpperPacketWithHeaderSize(msgID, wireBody, sequence, classification, headerSize)
		if err != nil {
			return err
		}
		session.upperSeq++
		runtimeName := upperRuntimeCmdPacketName(msgID, classification)
		runtimeKnown := upperRuntimeCmdPacketKnown(msgID, classification)
		s.logPacketEvent("game-upper-send-meta",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"msg_id", msgID,
			"runtime_cmd_name", runtimeName,
			"runtime_cmd_known", runtimeKnown,
			"classification", classification,
			"sequence", sequence,
			"header_size", headerSize,
			"plain_body_len", len(body),
			"body_len", len(wireBody),
			"body_encoded", bodyEncoded,
			"body_codec", bodyCodec,
			"packet_len", len(packet))
		s.logGamePacket(session, "SEND", "game-upper", packet,
			"msg_id", msgID,
			"runtime_cmd_name", runtimeName,
			"runtime_cmd_known", runtimeKnown,
			"classification", classification,
			"header_seq", sequence,
			"header_size", headerSize,
			"plain_body_len", len(body),
			"body_len", len(wireBody),
			"body_encoded", bodyEncoded,
			"body_codec", bodyCodec)
		if err := s.writeGameUpperPacketLocked(session, packet); err != nil {
			return err
		}
		noteEmptyRosterSlotProbe(session, body)
		return nil
	})
}

// buildCurrentSceneClass0FixedUpperPacket builds the current scene-state upper
// envelope used by the same-client Python reference for class0 ITEM_LIST.
//
// This is an envelope builder, not a captured packet replay.  The payload is
// still supplied by the caller from the real character inventory.  Unlike the
// ordinary server upper envelope, this scene-state variant has a 16-byte
// header whose bytes 11..15 are all zero; its checksum covers those five zero
// bytes followed by the payload.  The current client retains this protected
// reader state for lazy container UI construction, so substituting an ordinary
// incrementing upper sequence can parse the initial body yet fail later when
// the warehouse window consumes it.
func buildCurrentSceneClass0FixedUpperPacket(msgID uint16, body []byte) ([]byte, error) {
	const headerSize = dnfproto.GameServerUpperHeaderSize16
	packet := make([]byte, headerSize+len(body))
	packet[0] = 0
	binary.LittleEndian.PutUint16(packet[1:3], msgID)
	binary.LittleEndian.PutUint32(packet[3:7], uint32(len(packet)))
	copy(packet[headerSize:], body)
	sum, err := dnfproto.ChecksumRange(packet, 11, len(packet)-11)
	if err != nil {
		return nil, err
	}
	binary.LittleEndian.PutUint32(packet[7:11], sum)
	return packet, nil
}

func (s *Service) sendGameUpperClass0Fixed16ZeroPrefix(session *gameSession, msgID uint16, body []byte, owner string) error {
	packet, err := buildCurrentSceneClass0FixedUpperPacket(msgID, body)
	if err != nil {
		return err
	}
	runtimeName := upperRuntimeCmdPacketName(msgID, 0)
	runtimeKnown := upperRuntimeCmdPacketKnown(msgID, 0)
	s.logPacketEvent("game-upper-fixed16-zero-prefix-send",
		"conn_id", session.connID,
		"channel_id", session.channel.ID,
		"owner", owner,
		"msg_id", msgID,
		"runtime_cmd_name", runtimeName,
		"runtime_cmd_known", runtimeKnown,
		"classification", 0,
		"header_size", dnfproto.GameServerUpperHeaderSize16,
		"header_mode", "fixed16_zero_prefix_checksum",
		"plain_body_len", len(body),
		"body_len", len(body),
		"body_encoded", false,
		"body_codec", "plaintext",
		"packet_len", len(packet))
	s.logGamePacket(session, "SEND", "game-upper", packet,
		"msg_id", msgID,
		"runtime_cmd_name", runtimeName,
		"runtime_cmd_known", runtimeKnown,
		"classification", 0,
		"header_seq", 0,
		"header_size", dnfproto.GameServerUpperHeaderSize16,
		"header_mode", "fixed16_zero_prefix_checksum",
		"owner", owner,
		"plain_body_len", len(body),
		"body_len", len(body),
		"body_encoded", false,
		"body_codec", "plaintext")
	return s.writeGameUpperPacket(session, packet)
}

// sendCurrentSceneItemList sends the real ITEM_LIST body through the scene
// fixed16 envelope.  It deliberately does not consume session.upperSeq: that
// sequence belongs to ordinary upper packets, while this scene-state envelope
// owns an all-zero trailing header region.
func (s *Service) sendCurrentSceneItemList(session *gameSession, msgID uint16, body []byte) error {
	packet, err := buildCurrentSceneClass0FixedUpperPacket(msgID, body)
	if err != nil {
		return err
	}
	runtimeName := upperRuntimeCmdPacketName(msgID, 0)
	runtimeKnown := upperRuntimeCmdPacketKnown(msgID, 0)
	s.logPacketEvent("game-upper-send-meta",
		"conn_id", session.connID,
		"channel_id", session.channel.ID,
		"msg_id", msgID,
		"runtime_cmd_name", runtimeName,
		"runtime_cmd_known", runtimeKnown,
		"classification", 0,
		"sequence", 0,
		"header_size", dnfproto.GameServerUpperHeaderSize16,
		"header_mode", "current_scene_fixed16_zero_prefix",
		"plain_body_len", len(body),
		"body_len", len(body),
		"body_encoded", false,
		"body_codec", "plaintext",
		"packet_len", len(packet))
	s.logGamePacket(session, "SEND", "game-upper", packet,
		"msg_id", msgID,
		"runtime_cmd_name", runtimeName,
		"runtime_cmd_known", runtimeKnown,
		"classification", 0,
		"header_seq", 0,
		"header_size", dnfproto.GameServerUpperHeaderSize16,
		"header_mode", "current_scene_fixed16_zero_prefix",
		"plain_body_len", len(body),
		"body_len", len(body),
		"body_encoded", false,
		"body_codec", "plaintext")
	return s.writeGameUpperPacket(session, packet)
}

// sendCurrentProtectedClass0Packet owns current marker=1 scene transport
// frames whose payload is built from live state. It intentionally does not
// use the DOVE replay sequence: the fixed marker16 envelope is reconstructed
// here and the caller supplies the current structured body.
func (s *Service) sendCurrentProtectedClass0Packet(session *gameSession, msgID uint16, body []byte, bodyCodec, owner string) error {
	const (
		headerSize = dnfproto.GameServerUpperHeaderSize16
		marker     = uint32(1)
	)
	packet := make([]byte, headerSize+len(body))
	packet[0] = 0
	binary.LittleEndian.PutUint16(packet[1:3], msgID)
	binary.LittleEndian.PutUint32(packet[3:7], uint32(len(packet)))
	binary.LittleEndian.PutUint32(packet[7:11], marker)
	packet[15] = byte(marker)
	copy(packet[headerSize:], body)
	runtimeName := upperRuntimeCmdPacketName(msgID, 0)
	runtimeKnown := upperRuntimeCmdPacketKnown(msgID, 0)
	s.logPacketEvent("game-upper-current-protected-scene-send",
		"conn_id", session.connID,
		"channel_id", session.channel.ID,
		"owner", owner,
		"msg_id", msgID,
		"runtime_cmd_name", runtimeName,
		"runtime_cmd_known", runtimeKnown,
		"classification", 0,
		"header_size", headerSize,
		"header_marker", marker,
		"body_len", len(body),
		"body_encoded", true,
		"body_codec", bodyCodec,
		"packet_len", len(packet))
	s.logGamePacket(session, "SEND", "game-upper", packet,
		"msg_id", msgID,
		"runtime_cmd_name", runtimeName,
		"runtime_cmd_known", runtimeKnown,
		"classification", 0,
		"header_seq", 0,
		"header_size", headerSize,
		"header_marker", marker,
		"owner", owner,
		"body_len", len(body),
		"body_encoded", true,
		"body_codec", bodyCodec)
	return s.writeGameUpperPacket(session, packet)
}

// sendCurrentSceneFixedClass0Packet writes a real current scene notification
// through the fixed16 envelope.  It is intentionally separate from ordinary
// upper packets: bytes 11..15 must remain zero for client-side scene state
// owners that lazily consume the notification after scene construction.
func (s *Service) sendCurrentSceneFixedClass0Packet(session *gameSession, msgID uint16, body []byte, owner string) error {
	packet, err := buildCurrentSceneClass0FixedUpperPacket(msgID, body)
	if err != nil {
		return err
	}
	runtimeName := upperRuntimeCmdPacketName(msgID, 0)
	runtimeKnown := upperRuntimeCmdPacketKnown(msgID, 0)
	s.logPacketEvent("game-upper-current-scene-fixed-send",
		"conn_id", session.connID,
		"channel_id", session.channel.ID,
		"owner", owner,
		"msg_id", msgID,
		"runtime_cmd_name", runtimeName,
		"runtime_cmd_known", runtimeKnown,
		"classification", 0,
		"header_size", dnfproto.GameServerUpperHeaderSize16,
		"header_mode", "current_scene_fixed16_zero_prefix",
		"body_len", len(body),
		"packet_len", len(packet))
	s.logGamePacket(session, "SEND", "game-upper", packet,
		"msg_id", msgID,
		"runtime_cmd_name", runtimeName,
		"runtime_cmd_known", runtimeKnown,
		"classification", 0,
		"header_seq", 0,
		"header_size", dnfproto.GameServerUpperHeaderSize16,
		"header_mode", "current_scene_fixed16_zero_prefix",
		"owner", owner,
		"body_len", len(body))
	return s.writeGameUpperPacket(session, packet)
}

func (s *Service) sendGameUpperRawClass(session *gameSession, msgID uint16, body []byte, classification byte) error {
	if classification == 0 && msgID == uint16(dnfenum.CmdPacketWalkoutPartyMember) {
		prepared, err := s.prepareCurrentPremiumContractItemUpdate(session, body)
		if err != nil {
			return err
		}
		body = prepared
	}
	return s.sendGameUpperRawClassCodec(session, msgID, body, classification, true)
}

func (s *Service) sendGameUpperRawClassNoCodec(session *gameSession, msgID uint16, body []byte, classification byte) error {
	return s.sendGameUpperRawClassCodec(session, msgID, body, classification, false)
}

func (s *Service) sendGameUpperRawClassCodec(session *gameSession, msgID uint16, body []byte, classification byte, allowCodec bool) error {
	return s.withGameWire(session, func() error {
		sequence := session.upperSeq
		headerSize := s.gameUpperHeaderSize()
		wireBody := body
		bodyEncoded := false
		bodyCodec := s.gameUpperBodyCodecMode()
		requireCurrentClass0Opcode1Codec := classification == 0 && msgID == 1
		if allowCodec && requireCurrentClass0Opcode1Codec {
			wireBody = dnfproto.EncodeLatestUpperClass0Opcode1Body(body)
			bodyEncoded = true
			bodyCodec = "current_class0_op1_xor_b5_rotl2"
		} else if allowCodec && classification == dnfproto.DefaultChannelClassification && bodyCodec != gameUpperBodyCodecPlain {
			encodedBody, encoded, err := dnfproto.EncodeLatestUpperServerBody(msgID, body)
			if err != nil {
				return err
			}
			wireBody = encodedBody
			bodyEncoded = encoded
		} else if !allowCodec {
			bodyCodec = gameUpperBodyCodecPlain
		}
		packet, err := dnfproto.BuildGameServerUpperPacketWithHeaderSize(msgID, wireBody, sequence, classification, headerSize)
		if err != nil {
			return err
		}
		session.upperSeq++
		runtimeName := upperRuntimeCmdPacketName(msgID, classification)
		runtimeKnown := upperRuntimeCmdPacketKnown(msgID, classification)
		s.logPacketEvent("game-upper-send-meta",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"msg_id", msgID,
			"runtime_cmd_name", runtimeName,
			"runtime_cmd_known", runtimeKnown,
			"classification", classification,
			"sequence", sequence,
			"header_size", headerSize,
			"plain_body_len", len(body),
			"body_len", len(wireBody),
			"body_encoded", bodyEncoded,
			"body_codec", bodyCodec,
			"packet_len", len(packet))
		s.logGamePacket(session, "SEND", "game-upper", packet,
			"msg_id", msgID,
			"runtime_cmd_name", runtimeName,
			"runtime_cmd_known", runtimeKnown,
			"classification", classification,
			"header_seq", sequence,
			"header_size", headerSize,
			"plain_body_len", len(body),
			"body_len", len(wireBody),
			"body_encoded", bodyEncoded,
			"body_codec", bodyCodec)
		return s.writeGameUpperPacketLocked(session, packet)
	})
}

func (s *Service) gameUpperHeaderSize() int {
	if s.options.gameUpperHeader == gameUpperHeaderServer16 {
		return dnfproto.GameServerUpperHeaderSize16
	}
	return dnfproto.GameServerUpperHeaderSize
}

func (s *Service) gameUpperBodyCodecMode() string {
	if s.options.gameUpperBodyCodec == "" {
		return gameUpperBodyCodecAuto
	}
	return s.options.gameUpperBodyCodec
}

func (s *Service) gameUpperClientBodyCodecMode() string {
	if s.options.gameUpperClientBodyCodec == "" {
		return gameUpperClientBodyCodecPlain
	}
	return s.options.gameUpperClientBodyCodec
}

func (s *Service) sendGameUpperSuccess(session *gameSession, msgID uint16, body []byte) error {
	return s.sendGameUpperRaw(session, msgID, upperSuccessBody(body))
}

func (s *Service) sendGameUpperFailure(session *gameSession, msgID uint16, code byte) error {
	return s.sendGameUpperRaw(session, msgID, []byte{0, code})
}

func upperSuccessBody(body []byte) []byte {
	out := make([]byte, 1+len(body))
	out[0] = 1
	copy(out[1:], body)
	return out
}

func (s *Service) logGamePacket(session *gameSession, direction string, kind string, data []byte, fields ...any) {
	if session == nil {
		s.logPacket(direction, kind, data, fields...)
		return
	}
	packetSeq := atomic.AddUint64(&session.packetSeq, 1)
	args := make([]any, 0, len(fields)+4)
	args = append(args, "conn_id", session.connID, "pkt_seq", packetSeq)
	args = append(args, fields...)
	s.logPacket(direction, kind, data, args...)
}

func (s *Service) logGameEvent(session *gameSession, kind string, fields ...any) {
	if session == nil {
		s.logPacketEvent(kind, fields...)
		return
	}
	args := make([]any, 0, len(fields)+4)
	args = append(args, "conn_id", session.connID, "channel_id", session.channel.ID)
	args = append(args, fields...)
	s.logPacketEvent(kind, args...)
}
