package dnfbridge

import (
	"encoding/binary"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func buildRosterSlots(characters []dnfrepo.CharacterRecord) []byte {
	// 褰撳墠瀹㈡埛绔粯璁ゅ彧寮€鏀?8 涓鑹叉Ы锛岄澶栨Ы浣嶅繀椤荤敱閬撳叿/鎵╁睍鏍忎綅璇锋眰閾捐矾瑙﹀彂銆
	var writer packetWriter
	writer.writeByte(2)
	writer.writeByte(latestRosterRouteNormal)
	writer.writeByte(latestRosterContextNormal)
	writer.writeUint16(defaultCharacterSlots)
	writer.writeUint16(0)
	writer.writeUint16(0)
	writer.writeUint32(0)
	writer.writeUint16(0)
	writer.writeByte(0)
	writer.writeByte(0)
	writer.writeUint32(0)
	writer.writeUint32(0)
	return writer.bytes()
}

func buildRosterChars(characters []dnfrepo.CharacterRecord) []byte {
	var writer packetWriter
	count := rosterCount(characters)
	writer.writeByte(3)
	writer.writeUint16(uint16(count))
	for idx, character := range characters {
		if idx >= count {
			break
		}
		writer.writeByte(latestRosterRouteNormal)
		writer.writeByte(latestRosterContextNormal)
		writeRosterChar(&writer, character)
	}
	return writer.bytes()
}

func rosterCount(characters []dnfrepo.CharacterRecord) int {
	count := len(characters)
	if count > defaultCharacterSlots {
		return defaultCharacterSlots
	}
	if count > 0xffff {
		return 0xffff
	}
	return count
}

func writeRosterChar(writer *packetWriter, character dnfrepo.CharacterRecord) {
	charID := uint32(numericCharacterID(character))
	job := byte(numericCharacterStat(character.Job))
	growType := byte(numericCharacterStatValue(character, "grow_type"))
	level := byte(character.Level)
	if level == 0 {
		level = 1
	}
	slot := uint16(character.Slot)

	writer.writeZero(8)
	writer.writeUint16(uint16(charID))
	writer.writeByte(latestCharacterStateActive)
	writer.writeByte(0)

	writer.writeUint32(charID)
	writer.writeUint32(0)
	writer.writeUint32(0)
	writer.writeByte(job)
	writer.writeByte(growType)
	writer.writeByte(level)
	writer.writeByte(0)
	writer.writeByte(0)

	writer.writeUint32(0)
	writer.writeUint16(slot)
	writer.writeUint32(charID)
	writeFixedUTF16(writer, character.Name, 32)
	writer.writeByte(job)
	writer.writeByte(0)

	writer.writeUint32(0)
	writer.writeByte(0)
}

func buildRosterBodies(characters []dnfrepo.CharacterRecord) [][]byte {
	return [][]byte{
		buildRosterSlots(characters),
		buildRosterChars(characters),
	}
}

func buildCSharpRosterBody(characters []dnfrepo.CharacterRecord) []byte {
	return buildNoPackRosterBody(characters)
}

func buildNoPackRosterBody(characters []dnfrepo.CharacterRecord) []byte {
	var writer packetWriter
	count := rosterCount(characters)
	header := defaultNoPackRosterHeader(count)

	// sub_200BEA0 mode=2 consumes this exact 15-byte prefix before it begins
	// calling sub_200B250 for role rows.  Use fixed offsets rather than a
	// sequential writer: byte 13..14 is the only row-loop count, and a zero
	// there produces a healthy-but-empty (0/17) character selection screen.
	prefix := make([]byte, 15)
	prefix[0] = 2
	prefix[1] = byte(count)
	prefix[2] = 5
	binary.LittleEndian.PutUint16(prefix[3:5], clampRosterUint16(header.TotalOrSlotLimit))
	binary.LittleEndian.PutUint16(prefix[5:7], clampRosterUint16(header.UsedOrRemain))
	binary.LittleEndian.PutUint16(prefix[7:9], rosterUint16Value(header.SelectedOrPage, 0))
	binary.LittleEndian.PutUint32(prefix[9:13], rosterUint32Value(header.RosterState, 0))
	binary.LittleEndian.PutUint16(prefix[13:15], uint16(count))
	writer.writeBytes(prefix)
	for idx, character := range characters {
		if idx >= count {
			break
		}
		writeNoPackRosterEntry(&writer, idx, character)
	}
	writer.writeByte(rosterByteValue(header.PageCount, rosterDefaultPageCount))
	writer.writeByte(rosterHeaderByte(header.RosterFlag, 0))
	// mode=2 尾部状态 flag 会触发客户端角色栏状态刷新；默认置 0，避免每次打开选角页播放扩栏解锁光效。
	writer.writeUint32(rosterUint32Value(header.RosterValueA, 0))
	writer.writeUint32(rosterUint32Value(header.RosterValueB, 0))
	return writer.bytes()
}

func noteEmptyRosterSlotProbe(session *gameSession, body []byte) {
	// Current EXE sub_200BEA0(mode=2) finalizes an empty selector with index
	// UINT16_MAX, then immediately sends class1/op679 and clears selector ready.
	// Remember only that exact mode-2 empty snapshot so the request handler can
	// complete the client's no-extension handshake without changing ordinary
	// slot-extension behavior.
	session.emptyRosterSlotProbePending = len(body) >= 15 &&
		body[0] == 2 &&
		body[1] == 0 &&
		binary.LittleEndian.Uint16(body[7:9]) == 0 &&
		binary.LittleEndian.Uint16(body[13:15]) == 0
}

// defaultNoPackRosterHeader 保留旧端安全的槽位容量字段。
func defaultNoPackRosterHeader(count int) dnfrepo.CharacterRosterHeader {
	return dnfrepo.CharacterRosterHeader{
		UnkA:             latestRosterRouteNormal,
		UnkB:             latestRosterContextNormal,
		TotalOrSlotLimit: noPackRosterWireSlotLimit,
		UsedOrRemain:     noPackRosterWireSlotLimit,
		SelectedOrPage:   int64(count),
		PageCount:        rosterDefaultPageCount,
		RosterFlag:       0,
	}
}
