package protocol

import "encoding/binary"

// LatestGameStreamKind 标记 game 端口里混合出现的最新协议帧类型。
type LatestGameStreamKind uint8

const (
	LatestGameStreamTransport LatestGameStreamKind = 1
	LatestGameStreamUpper     LatestGameStreamKind = 2
	LatestGameStreamLegacy    LatestGameStreamKind = 3
	LatestGameStreamDproto    LatestGameStreamKind = 4

	currentDungeonMoveMapPlainBodySize        = 100
	currentTownSetUserAreaPlainBodySize       = 16
	currentAcceptQuestPlainBodySize           = 4
	currentSetQuestTriggerPlainBodySize       = 6
	currentDieCharacterPlainBodySize          = 4
	currentUseCoinPlainBodySize               = 2
	currentBossDieCheckPlainBodySize          = 39
	currentTutorialFlagPlainBodySize          = 6
	currentPlayResultBaseBodySize             = 42
	currentPlayResultOptionalBodySize         = 46
	currentPlayResultDynamicRowSize           = 7
	currentPlayResultMaximumDynamicRows       = 8
	currentChannelReconnectProbeSize          = 31
	currentChannelDisplayRebindSize           = 590
	reference90CNChannelDisplayRebindBodySize = 598
)

// LatestGameStreamPacket 是从 game TCP 字节流切出的完整包。
type LatestGameStreamPacket struct {
	Kind LatestGameStreamKind
	Data []byte
}

// SplitLatestGameStream 从 game TCP 流中同时切出 transport frame 和 raw upper 包。
//
// 最新客户端在 game 连接上既会发送 12+8+7 的 game transport，也会发送
// 13 字节频道包头格式的 raw upper 包；拆流时必须先识别 raw upper，再按
// legacy game 和 frame+2 的 u16 totalLen 处理 transport。
func SplitLatestGameStream(buffer []byte, maxPacketBytes int) (packets []LatestGameStreamPacket, remaining []byte, skipped int, err error) {
	if maxPacketBytes <= 0 {
		maxPacketBytes = int(^uint16(0))
	}
	offset := 0
	for offset <= len(buffer)-4 {
		if packet, wait, ok := splitDprotoClientPacket(buffer, offset, maxPacketBytes); ok {
			packets = append(packets, LatestGameStreamPacket{
				Kind: LatestGameStreamDproto,
				Data: packet,
			})
			offset += len(packet)
			continue
		} else if wait {
			break
		}
		if packet, wait, ok := splitUpperPacket(buffer, offset, maxPacketBytes); ok {
			packets = append(packets, LatestGameStreamPacket{
				Kind: LatestGameStreamUpper,
				Data: packet,
			})
			offset += len(packet)
			continue
		} else if wait {
			break
		}
		if packet, wait, ok := splitLegacyGamePacket(buffer, offset, maxPacketBytes); ok {
			packets = append(packets, LatestGameStreamPacket{
				Kind: LatestGameStreamLegacy,
				Data: packet,
			})
			offset += len(packet)
			continue
		} else if wait {
			break
		}

		frameLength := int(binary.LittleEndian.Uint16(buffer[offset+2 : offset+4]))
		if frameLength < MinTCPFrameSize {
			offset++
			skipped++
			continue
		}
		if frameLength > maxPacketBytes {
			return nil, nil, skipped, ErrPacketTooLarge
		}
		if len(buffer)-offset < frameLength {
			break
		}
		packets = append(packets, LatestGameStreamPacket{
			Kind: LatestGameStreamTransport,
			Data: cloneBytes(buffer[offset : offset+frameLength]),
		})
		offset += frameLength
	}
	return packets, cloneBytes(buffer[offset:]), skipped, nil
}

func splitDprotoClientPacket(buffer []byte, offset int, maxPacketBytes int) ([]byte, bool, bool) {
	available := len(buffer) - offset
	if available < ChannelHeaderSize {
		return nil, false, false
	}
	if buffer[offset] != DefaultChannelClassification ||
		binary.LittleEndian.Uint16(buffer[offset+1:offset+3]) != DprotoClientEnvelopeOpcode {
		return nil, false, false
	}
	length := int(binary.LittleEndian.Uint32(buffer[offset+3 : offset+7]))
	if length < ChannelHeaderSize || length > maxPacketBytes {
		return nil, false, false
	}
	if available < length {
		return nil, true, false
	}
	return cloneBytes(buffer[offset : offset+length]), false, true
}

func splitLegacyGamePacket(buffer []byte, offset int, maxPacketBytes int) ([]byte, bool, bool) {
	available := len(buffer) - offset
	if available < LegacyGameHeaderSize {
		return nil, false, false
	}
	cmd := buffer[offset]
	if cmd != 0 && cmd != 1 {
		return nil, false, false
	}
	length := int(binary.LittleEndian.Uint32(buffer[offset+3 : offset+7]))
	if length < LegacyGameHeaderSize {
		return nil, false, false
	}
	if length > maxPacketBytes {
		return nil, false, false
	}
	if available < length {
		return nil, true, false
	}
	packet := cloneBytes(buffer[offset : offset+length])
	if _, err := ParseLegacyGamePacket(packet); err != nil {
		return nil, false, false
	}
	return packet, false, true
}

func splitUpperPacket(buffer []byte, offset int, maxPacketBytes int) ([]byte, bool, bool) {
	available := len(buffer) - offset
	if available < ChannelHeaderSize {
		return nil, false, false
	}
	classification := buffer[offset]
	if classification != 0 && classification != DefaultChannelClassification {
		return nil, false, false
	}
	length := int(binary.LittleEndian.Uint32(buffer[offset+3 : offset+7]))
	if length < ChannelHeaderSize {
		return nil, false, false
	}
	if length > maxPacketBytes {
		return nil, false, false
	}
	if packet, ok := splitHookedShortCreateUpper(buffer, offset, available, length); ok {
		return packet, false, true
	}
	if packet, ok := splitHookedCurrentSelectUpper(buffer, offset, available, length); ok {
		return packet, false, true
	}
	if available < length {
		return nil, true, false
	}
	packet := cloneBytes(buffer[offset : offset+length])
	parsed, err := ParseChannelPacketUnchecked(packet)
	if err != nil {
		return nil, false, false
	}
	if !isKnownClientUpperMsg(parsed.Header.MsgID, parsed.Body) {
		return nil, false, false
	}
	return packet, false, true
}

func splitHookedCurrentSelectUpper(buffer []byte, offset int, available int, headerLength int) ([]byte, bool) {
	const (
		selectMessageID       = 16
		staleSelectBodyLength = 32
		currentSelectBodySize = 21
	)
	msgID := binary.LittleEndian.Uint16(buffer[offset+1 : offset+3])
	if msgID != selectMessageID || headerLength != ChannelHeaderSize+staleSelectBodyLength {
		return nil, false
	}
	actualLength := ChannelHeaderSize + currentSelectBodySize
	if available < actualLength {
		return nil, false
	}
	packet := cloneBytes(buffer[offset : offset+actualLength])
	// The local hook copies the current 21-byte op16 body but leaves the old
	// 32-byte body length in the upper header. Correct only that observed shape
	// so the following packet header is not consumed as op16 data.
	binary.LittleEndian.PutUint32(packet[3:7], uint32(actualLength))
	return packet, true
}

func splitHookedShortCreateUpper(buffer []byte, offset int, available int, headerLength int) ([]byte, bool) {
	msgID := binary.LittleEndian.Uint16(buffer[offset+1 : offset+3])
	if msgID != 5 || headerLength <= ChannelHeaderSize {
		return nil, false
	}
	bodyOffset := offset + ChannelHeaderSize
	actualBodyLength, ok := hookedPlainCreateBodyLength(buffer[bodyOffset:])
	if !ok {
		return nil, false
	}
	actualLength := ChannelHeaderSize + actualBodyLength
	if actualLength >= headerLength {
		return nil, false
	}
	if available < actualLength {
		return nil, false
	}
	packet := cloneBytes(buffer[offset : offset+actualLength])
	// NoPack 本地 msg5 hook 只替换 body，没有同步修正 upper 头里的 length。
	// 真实创建体长度由 nameLen+固定 8 字节外观选项决定，不能写死，否则会吞掉后续 1276 心跳头。
	binary.LittleEndian.PutUint32(packet[3:7], uint32(actualLength))
	// NoPack 本地 msg5 hook 只替换 body，未同步修正 upper header length；
	// 服务端按实际 21 字节创建体切包，避免吞掉后续 1276 心跳头。
	binary.LittleEndian.PutUint32(packet[3:7], uint32(actualLength))
	return packet, true
}

func hookedPlainCreateBodyLength(body []byte) (int, bool) {
	if len(body) < 5 {
		return 0, false
	}
	if body[0] > 0x3f {
		return 0, false
	}
	nameLen := int(binary.LittleEndian.Uint32(body[1:5]))
	if nameLen < 2 || nameLen > 30 {
		return 0, false
	}
	actualBodyLength := 5 + nameLen + 8
	if len(body) < actualBodyLength {
		return 0, false
	}
	return actualBodyLength, true
}

func isKnownClientUpperMsg(msgID uint16, body []byte) bool {
	bodyLen := len(body)
	switch msgID {
	case 1:
		// The current EXE emits a 590-byte native upper packet before and after
		// the target town route. The clean 90cn reference emits the same
		// lifecycle packet with a 598-byte body. Both are exact, state-gated
		// display-rebind shapes and must be cut before the overlapping legacy
		// parser.
		return bodyLen == currentChannelDisplayRebindSize ||
			bodyLen == reference90CNChannelDisplayRebindBodySize
	case 2:
		// A channel transfer opens the target game port without requesting the
		// ordinary roster and emits this exact 31-byte native upper probe.
		// Recognize it before the overlapping legacy header parser.
		return bodyLen == currentChannelReconnectProbeSize
	case 4, 7, 0x000f, 0x0010, 0x003f, 0x0078, 0x02a7, 0x02b1, 0x02b2, 0x02b5:
		return true
	case 31:
		// Current EXE sub_1C53100 writes class-1/op31 as the exact
		// u16 opcode echo followed by a u16 quest id. Requiring both the
		// four-byte size and echo prevents a same-shaped legacy packet from
		// being cut as raw upper traffic.
		return bodyLen == currentAcceptQuestPlainBodySize && binary.LittleEndian.Uint16(body[:2]) == msgID
	case 33:
		// Current SET_QUEST_TRIGGER is the u16 opcode echo, u16 quest id,
		// u8 trigger channel, and u8 increment flag. The selective DPROTO
		// clone form carries a four-byte trailer and therefore intentionally
		// falls through to the legacy decoder for exact normalization.
		return bodyLen == currentSetQuestTriggerPlainBodySize && binary.LittleEndian.Uint16(body[:2]) == msgID
	case 39:
		if bodyLen == 62 {
			return true
		}
		if bodyLen < 66 {
			return false
		}
		count := int(body[22])
		return count <= 254 && bodyLen == 66+count*10
	case 40:
		// Current EXE normal-dungeon writers emit exactly two u16 actor
		// coordinates. They are not an actor id and are not server authority.
		return bodyLen == currentDieCharacterPlainBodySize
	case 41:
		// Current EXE normal revive requests emit one u16 target object key.
		return bodyLen == currentUseCoinPlainBodySize
	case 42:
		return bodyLen == 0
	case 45:
		return bodyLen == currentDungeonMoveMapPlainBodySize
	case 36:
		return bodyLen == currentTownSetUserAreaPlainBodySize
	case 46:
		return isCurrentPlayResultPlainBodySize(bodyLen)
	case 117:
		return bodyLen == currentBossDieCheckPlainBodySize
	case 123:
		return bodyLen == 24
	case 132:
		return bodyLen == 0
	case 143:
		return bodyLen == currentTutorialFlagPlainBodySize
	case 295:
		// Current EXE character-slot exchange writes exactly two u32 slot
		// indexes. The 13-byte upper header overlaps the legacy game header,
		// so this exact boundary must be recognized before legacy parsing.
		return bodyLen == 8
	case 560:
		// Current EXE sub_1DB82D0/sub_1DB88B0 starts class-1/op560 and
		// immediately flushes it without writing a request body. Recognize the
		// exact boundary so the 13-byte raw upper packet is not mistaken for a
		// same-shaped legacy game packet.
		return bodyLen == 0
	// 2026-06-30 hook/packet_log 对照证明这些号是 msg1 后的 latest raw upper 上行。
	// 其中 3/8/593 等号会和旧 cmd/type 重叠，必须用 body 长度避免旧 legacy 包误切。
	case 3:
		return bodyLen == 16
	case 5:
		if actualBodyLength, ok := hookedPlainCreateBodyLength(body); ok && actualBodyLength == bodyLen {
			return true
		}
		return bodyLen == 24 || bodyLen == 32
	case 8:
		return bodyLen == 8
	case 171:
		return bodyLen == 48
	case 194:
		return bodyLen == 368
	case 415:
		return bodyLen == 0
	case 250:
		return bodyLen == 64
	case 279:
		return bodyLen == 16
	case 441:
		return bodyLen == 8
	case 476:
		return bodyLen == 16
	case 477:
		return bodyLen == 8
	case 593:
		return bodyLen == 8 || bodyLen == 24
	case 645:
		return bodyLen == 0
	case 1262:
		return bodyLen == 32
	case 1276:
		return bodyLen == 0
	case 1286:
		return bodyLen == 16
	case 1287:
		return bodyLen == 8
	case 1320:
		return bodyLen == 32
	case 1516:
		return bodyLen > 0
	case 1518:
		return bodyLen > 0
	default:
		return false
	}
}

func isCurrentPlayResultPlainBodySize(bodyLen int) bool {
	for rows := 0; rows <= currentPlayResultMaximumDynamicRows; rows++ {
		if bodyLen == currentPlayResultBaseBodySize+rows*currentPlayResultDynamicRowSize ||
			bodyLen == currentPlayResultOptionalBodySize+rows*currentPlayResultDynamicRowSize {
			return true
		}
	}
	return false
}
