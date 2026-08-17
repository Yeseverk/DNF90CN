package dnfbridge

import dnfrepo "longheng.io/server/internal/modules/dnf/repository"

func writeNoPackRosterEntryFromStats(writer *packetWriter, index int, character dnfrepo.CharacterRecord) {
	job := rosterByteValue(int64(numericCharacterStat(character.Job)), 0)
	grow := rosterByteValue(numericCharacterStatValue(character, "grow_type"), 0)
	level := rosterLevel(character)
	slot := rosterSlotValue(character.Slot, index)
	objectID := numericCharacterID(character)

	writer.writeUint16(uint16(slot))
	// IDA sub_200B250 证明新版 roster 名字是 DSTR，名字后还有 2 个保留字节，再读 job、grow、level。
	writer.writeRawDstr(rosterRawNameBytes(character))
	writer.writeByte(job)
	writer.writeByte(grow)
	writer.writeByte(level)
	writer.writeByte(0)
	writer.writeByte(rosterState0Value(numericCharacterStatValue(character, "roster_state0")))
	writer.writeUint32(statU32(character, "roster_time_a", 0))
	writer.writeUint32(statU32(character, "roster_time_b", 0))
	writeNoPackRosterEquipSummary(writer, nil)
	writer.writeUint32(statU32(character, "roster_value0", 0))
	writer.writeByte(statU8(character, "roster_value1", 0))
	writer.writeByte(statU8(character, "roster_value2", 0))
	writer.writeByte(statU8(character, "roster_reserved_a", 0))
	writer.writeByte(statU8(character, "roster_reserved_b", 0))
	writeRosterByteBlock(writer, rosterLinkedIDBlock(character), rosterLinkedIDBlockSize)
	writer.writeByte(statU8(character, "roster_value3", 0))
	writer.writeUint32(uint32(objectID))
	writeNoPackRosterUITail(writer, character)
}

func writeNoPackRosterEntry(writer *packetWriter, index int, character dnfrepo.CharacterRecord) {
	job := rosterByteValue(int64(numericCharacterStat(character.Job)), 0)
	grow := rosterByteValue(numericCharacterStatValue(character, "grow_type"), 0)
	level := rosterLevel(character)
	slot := rosterSlotValue(character.Slot, index)

	writer.writeUint16(uint16(slot))
	writer.writeRawDstr(rosterRawNameBytes(character))
	writer.writeZero(noPackRosterPreJobBytes)
	// IDA sub_200B250 -> upper_pkt_read_wstr 先读 u32 len，再读 <31 字节名字并转宽字符；
	// 不是固定 UTF16。固定写 62 字节会让名字失败并把后续 job/grow/level/state 整体读偏。
	// The 2026-07-19 visible role-select chain writes job and grow as separate
	// bytes. Packing them into one byte kept the row length but poisoned the
	// current selector's class/grow cursor for non-base roles.
	writer.writeByte(job)
	writer.writeByte(grow)
	writer.writeByte(level)
	writer.writeByte(0)
	writer.writeByte(0)
	writer.writeUint32(0)
	writer.writeUint32(0)
	// The visible 2026-07-19/23:15 role-select chain uses the SQL slot as the
	// row key and keeps the real DB/PVF equipment summary in the roster row.
	// The later character-id/empty-summary experiment left the selector black
	// with the current source-built upper-body-bypass DLL.
	writeNoPackRosterEquipSummary(writer, characterEquipSummary(character))
	writeNoPackRosterNormalPostEquip(writer)
}

func writeNoPackRosterNormalPostEquip(writer *packetWriter) {
	writer.writeZero(24)
	writer.writeByte(3)
	writer.writeByte(0)
	writer.writeByte(0)
	writer.writeByte(4)
	writer.writeZero(10)
}

func writeNoPackRosterEntryLatest(writer *packetWriter, index int, character dnfrepo.CharacterRecord) {
	job := byte(numericCharacterStat(character.Job))
	grow := byte(numericCharacterStatValue(character, "grow_type"))
	level := rosterLevel(character)
	slot := rosterSlotValue(character.Slot, index)
	objectID := rosterObjectID(character)

	// sub_200B250 用首个 u16 查找客户端槽位对象；这里必须写真实 slot，而不是压缩后的列表序号。
	writer.writeUint16(uint16(slot))
	writeFixedUTF16(writer, character.Name, rosterWideNameUnits)
	// WStr31 后 sub_2005D90 还会先读两个 u8；普通角色用 0，避免 level/job 被整体读偏。
	writer.writeByte(level)
	writer.writeByte(packRosterJobGrow(job, grow))
	writer.writeByte(0)
	writer.writeByte(0)
	writer.writeByte(0)
	writer.writeUint32(0)
	writer.writeUint32(0)
	writeNoPackRosterEquipSummary(writer, nil)
	writer.writeUint32(0)
	writer.writeByte(0)
	writer.writeByte(0)
	writer.writeByte(0)
	writer.writeByte(0)
	writeRosterByteBlock(writer, nil, rosterLinkedIDBlockSize)
	writer.writeByte(0)
	writer.writeUint32(uint32(objectID))
	writer.writeByte(3)
	// 普通角色的特殊、决斗、改名、display flag 状态必须全清零；透传旧 roster_json 会把 UI 路由到目标/决斗卡片。
	writer.writeByte(3)
	writer.writeByte(0)
	writer.writeByte(0)
	writer.writeByte(4)
	writer.writeZero(13)
}

func writeNoPackRosterEntryDstr(writer *packetWriter, index int, character dnfrepo.CharacterRecord) {
	job := byte(numericCharacterStat(character.Job))
	grow := byte(numericCharacterStatValue(character, "grow_type"))
	level := rosterLevel(character)
	name := rosterRawNameBytes(character)

	// 对齐 C# CharacterSelectHandler.BuildCharacterListBody：
	// u16 entryIndex + DSTR(rawName) + 两个保留字节 + job/grow/level。
	// 当前客户端从这个顺序读取名字、职业和普通/决斗卡片状态。
	writer.writeUint16(uint16(rosterSlot(index)))
	writer.writeRawDstr(name)
	writer.writeByte(0)
	writer.writeByte(0)
	writer.writeByte(job)
	writer.writeByte(grow)
	writer.writeByte(level)
	writer.writeZero(10)
	// MCP/live hook: sub_20026C0 会按装备摘要里的 item id 立即进资源加载链；
	// 当前 DB/PVF 装备 id 与 NoPack 场景对象资源表还没闭合，先不在 op2 场景对象里内联装备，避免 0x30303030 崩溃。
	writeNoPackRosterEquipSummary(writer, nil)
	writer.writeZero(24)
	writer.writeByte(3)
	writer.writeByte(0)
	writer.writeByte(0)
	writer.writeByte(4)
	writer.writeZero(4)
}

func writeNoPackRosterEntryCurrent(writer *packetWriter, index int, character dnfrepo.CharacterRecord) {
	job := byte(numericCharacterStat(character.Job))
	grow := byte(numericCharacterStatValue(character, "grow_type"))
	level := rosterLevel(character)
	slot := rosterSlotValue(character.Slot, index)
	objectID := rosterObjectID(character)

	// sub_200B250 的首个 u16 是槽位对象索引；后续字段按客户端 116 字节 entry 读取。
	writer.writeUint16(uint16(slot))
	writeFixedUTF16(writer, character.Name, rosterWideNameUnits)
	writer.writeZero(noPackRosterPreJobBytes)
	writer.writeByte(level)
	writer.writeByte(packRosterJobGrow(job, grow))
	writer.writeByte(0)
	writer.writeByte(0)
	writer.writeByte(0)
	writer.writeUint32(0)
	writer.writeUint32(0)
	writeNoPackRosterEquipSummary(writer, nil)
	writer.writeUint32(0)
	writer.writeByte(0)
	writer.writeByte(0)
	writer.writeByte(0)
	writer.writeByte(0)
	writeRosterByteBlock(writer, nil, rosterLinkedIDBlockSize)
	writer.writeByte(0)
	writer.writeUint32(uint32(objectID))
	// C# account_character_entries 普通角色尾状态为 03 00 00 04，避免改名/特殊状态 UI。
	writer.writeByte(3)
	writer.writeByte(0)
	writer.writeByte(0)
	writer.writeByte(4)
	writer.writeZero(13)
}

func writeNoPackRosterEquipSummary(writer *packetWriter, rows []dnfrepo.CharacterRosterEquipSummary) {
	rows = currentRosterEquipSummaryRows(rows)
	count := len(rows)
	if count > 0xff {
		count = 0xff
	}
	writer.writeByte(byte(count))
	for idx := 0; idx < count; idx++ {
		row := rows[idx]
		writer.writeByte(rosterByteValue(row.Slot, 0))
		writer.writeUint32(rosterUint32Value(row.ItemIDOrIcon, 0))
		// sub_225C9B0(type=44) always calls sub_3457C50.  The roster
		// summary owns no real type-44 payload yet, but the zero length itself
		// is required grammar and must not be omitted.
		writer.writeUint32(0)
		writer.writeByte(rosterByteValue(row.PackedFlags, 0))
		writer.writeUint32(rosterUint32Value(row.OptionalIDOrExpire, 0))
		writer.writeUint32(rosterUint32Value(row.AuxValue, 0))
		writer.writeByte(rosterByteValue(row.AuxFlag, 0))
	}
}

func currentRosterFullAppearanceRows(rows []dnfrepo.CharacterRosterEquipSummary) []dnfrepo.CharacterRosterEquipSummary {
	itemIDs := [currentActorMode0AppearanceSlotCount]int64{}
	for slot := range itemIDs {
		itemIDs[slot] = int64(currentActorMode0AppearanceEmptyItem)
	}
	for _, row := range currentRosterEquipSummaryRows(rows) {
		if row.Slot < 0 || row.Slot >= currentActorMode0AppearanceSlotCount {
			continue
		}
		itemIDs[int(row.Slot)] = row.ItemIDOrIcon
	}
	out := make([]dnfrepo.CharacterRosterEquipSummary, currentActorMode0AppearanceSlotCount)
	for slot, itemID := range itemIDs {
		out[slot] = dnfrepo.CharacterRosterEquipSummary{
			Slot:         int64(slot),
			ItemIDOrIcon: itemID,
		}
	}
	return out
}

func currentRosterEquipSummaryRows(rows []dnfrepo.CharacterRosterEquipSummary) []dnfrepo.CharacterRosterEquipSummary {
	if len(rows) == 0 {
		return nil
	}
	out := make([]dnfrepo.CharacterRosterEquipSummary, 0, len(rows))
	for _, row := range rows {
		// sub_200B250 indexes a fixed local equipment array without a bounds
		// check.  The current client keeps 33 actor equipment slots (0..32).
		if row.Slot < 0 || row.Slot > 32 || row.ItemIDOrIcon <= 0 || row.ItemIDOrIcon > int64(^uint32(0)) {
			continue
		}
		out = append(out, row)
	}
	return out
}

func writeRosterByteBlock(writer *packetWriter, values []int64, size int) {
	for idx := 0; idx < size; idx++ {
		value := int64(0)
		if idx < len(values) {
			value = values[idx]
		}
		writer.writeByte(rosterByteValue(value, 0))
	}
}

func rosterLinkedIDBlock(character dnfrepo.CharacterRecord) []int64 {
	values := make([]int64, rosterLinkedIDBlockSize)
	for idx := range values {
		values[idx] = numericCharacterStatValue(character, "roster_linked_id_"+twoDigit(idx))
	}
	return values
}

func writeNoPackRosterUITail(writer *packetWriter, character dnfrepo.CharacterRecord) {
	// 这 17 个字节直接进入角色卡 UI 状态；普通角色默认必须全 0，否则会被渲染成改名、特殊或决斗状态。
	for _, key := range []string{
		"roster_flag0_eq1",
		"roster_card_flag",
		"roster_value5",
		"roster_display_flags",
		"roster_tail_00",
		"roster_tail_01",
		"roster_tail_02",
		"roster_tail_03",
		"roster_tail_04",
		"roster_tail_05",
		"roster_tail_06",
		"roster_tail_07",
		"roster_tail_08",
		"roster_tail_09",
		"roster_tail_10",
		"roster_tail_11",
		"roster_flag6_eq1",
	} {
		writer.writeByte(statU8(character, key, 0))
	}
}
