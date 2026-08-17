package dnfbridge

import (
	"encoding/binary"
	"unicode/utf16"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func writeCurrentSceneObjectEntryTail(writer *packetWriter, character dnfrepo.CharacterRecord, hasCharacter bool) {
	writeCurrentSceneObjectEntryTailWithCreature(writer, character, hasCharacter, currentEquippedCreatureSnapshot{})
}

func writeCurrentSceneObjectEntryTailWithCreature(writer *packetWriter, character dnfrepo.CharacterRecord, hasCharacter bool, creature currentEquippedCreatureSnapshot) {
	writeCurrentSceneObjectEntryTailWithCreatureAndAdventureName(writer, character, hasCharacter, creature, "")
}

func writeCurrentSceneObjectEntryTailWithCreatureAndAdventureName(writer *packetWriter, character dnfrepo.CharacterRecord, hasCharacter bool, creature currentEquippedCreatureSnapshot, adventureName string) {
	job := byte(0)
	grow := byte(0)
	level := byte(0)
	if hasCharacter {
		job = byte(numericCharacterStat(character.Job))
		grow = byte(numericCharacterStatValue(character, "grow_type"))
		level = rosterLevel(character)
	}
	writer.writeByte(job)
	writer.writeByte(packCurrentSceneObjectGrow(grow, 0))
	// Current EXE proof: sub_2003810 stores this byte at template+20. sub_24DC280
	// copies it to actor ability context 0, and the recompute chain copies contexts
	// 0 -> 1 -> 2 -> 3. The level getter sub_2416DB0 reads context 3 offset 0.
	writer.writeByte(level)
	// 下面严格对应 sub_2009160 在 selector 之后的所有 upper_pkt_read_*。
	// 旧 DOVE entry 在这里带版本相关资源号；当前 NoPack 先用安全空状态创建对象，再让后续 359/124 刷新。
	writer.writeByte(0) // sub_20038C0
	writer.writeByte(0) // sub_2003970
	writer.writeByte(0) // v112[1]
	writeCurrentSceneObjectEquipSummary(writer, currentSceneObjectMode0EquipSummary(character, hasCharacter))
	writer.writeUint32(0) // v113: 对象资源/动作参数，随后传入对象虚表 +0x10。
	writer.writeByte(0)   // v114
	writer.writeByte(0)   // v115
	writer.writeByte(0)   // v104
	writer.writeByte(0)   // v97
	// Current EXE ReadAndApplyActorNameTagState (sub_2008D80) reads this exact
	// pair and resolves worn endpoint 30. Keep the existing empty sentinel when
	// no name tag is equipped; otherwise project the durable item and expiry.
	nameTagItemID, nameTagExpireTime := currentSceneObjectNameTagState(character, hasCharacter)
	writer.writeUint32(nameTagItemID)
	writer.writeUint32(nameTagExpireTime)
	writer.writeByte(0)   // sub_2003A20
	writer.writeUint32(0) // sub_2003AD0
	writer.writeByte(0)   // v116
	// Current EXE sub_20028B0 reads the equipped creature template ID here.
	// A live trace proved that UINT32_MAX resolves to runtime type 8 and leaves
	// the empty green overhead marker.  The C# no-equipped-creature path writes
	// zero, so do not reuse the DOVE template sentinel for this field.
	creatureItemID := currentSceneNoCreatureTemplateID
	var creatureName []byte
	creatureAliveState := byte(0)
	if creature.valid() {
		creatureItemID = creature.itemID
		creatureName = rosterNameBytes(creature.name)
		creatureAliveState = creature.aliveState
	}
	writer.writeUint32(creatureItemID)
	writer.writeRawDstr(creatureName)
	writer.writeByte(creatureAliveState) // sub_20028B0 equipped-creature alive state
	writer.writeByte(0)                  // sub_2003B80
	writer.writeByte(0)                  // sub_2003C90
	writer.writeUint32(0)                // v120
	// The current EXE's native organization header is {u8 guild level, DSTR
	// guild name, u32 chaos point}. The level byte is mandatory even for a
	// local display projection: omitting it shifts the DSTR length into the
	// level field and leaves the actor nameplate with no organization line.
	// Retain a zero level for characters with no persisted adventure name.
	adventureNameBytes := rosterNameBytes(adventureName)
	organizationLevel := byte(0)
	if len(adventureNameBytes) != 0 {
		organizationLevel = 1
	}
	writer.writeByte(organizationLevel)
	writer.writeRawDstr(adventureNameBytes)
	writer.writeUint32(0)                               // sub_2003D40 chaos point
	writer.writeUint32(currentSceneActorStateEventArg)  // v121 -> selected actor event 17/6599
	writer.writeByte(0)                                 // v122
	writer.writeByte(0)                                 // sub_2005B70
	writer.writeByte(0)                                 // sub_2005B70
	writer.writeByte(0)                                 // sub_2003F20
	writer.writeUint32(0)                               // sub_2003F20
	writer.writeByte(0)                                 // v125
	writer.writeUint32(0)                               // v126
	writer.writeUint16(0)                               // v128
	writer.writeByte(0)                                 // sub_2004E00
	writer.writeByte(0)                                 // sub_2004E00
	writer.writeUint16(0)                               // sub_2004E00
	writeCurrentHonorExpertState(writer, character)     // sub_2003150 HonorExpert {u32 level, u64 progress EXP}
	writer.writeByte(currentSceneNormalTownVisualState) // sub_20042F0 normal-town actor visual state
	writer.writeUint32(0)                               // sub_20042F0 后的对象 flag dword
	writer.writeByte(0)                                 // internal field +197
	writer.writeUint16(0)                               // sub_2691CF0/sub_2691B70
	writer.writeByte(0)                                 // sub_34AEA80
	writer.writeUint16(0)                               // sub_20029A0
	writer.writeByte(0)                                 // sub_20029A0
	writer.writeUint16(0)                               // sub_2002A20
	writer.writeByte(0)                                 // sub_2004510
	writer.writeByte(0)                                 // sub_2690AF0
	writer.writeUint32(0)                               // object internal +267
}

func currentSceneObjectNameTagState(character dnfrepo.CharacterRecord, hasCharacter bool) (uint32, uint32) {
	if !hasCharacter {
		return currentSceneNoAttachedUIResourceID, currentSceneNoAttachedUIResourceID
	}
	itemID := statU32(character, "name_tag_item_id", 0)
	if itemID == 0 {
		return currentSceneNoAttachedUIResourceID, currentSceneNoAttachedUIResourceID
	}
	return itemID, statU32(character, "name_tag_expire_time", 0)
}

func currentSceneObjectMode0EquipSummary(character dnfrepo.CharacterRecord, hasCharacter bool) []dnfrepo.CharacterRosterEquipSummary {
	return currentSceneObjectEquipSummary(character, hasCharacter)
}

func rewriteCurrentSceneObjectTailEquipSummary(tail []byte, rows []dnfrepo.CharacterRosterEquipSummary) ([]byte, bool) {
	const equipOffset = 6
	oldEnd, ok := currentSceneObjectEquipSummaryEnd(tail, equipOffset)
	if !ok {
		return nil, false
	}
	rows = cloneRosterEquipSummary(rows)
	sortRosterEquipSummary(rows)
	var writer packetWriter
	writeCurrentSceneObjectEquipSummary(&writer, rows)
	replacement := writer.bytes()
	out := make([]byte, 0, len(tail)-oldEnd+equipOffset+len(replacement))
	out = append(out, tail[:equipOffset]...)
	out = append(out, replacement...)
	out = append(out, tail[oldEnd:]...)
	return out, true
}

const currentSceneObjectEquipSummaryRowBytes = 19

func currentSceneObjectEquipSummaryEnd(tail []byte, offset int) (int, bool) {
	if offset < 0 || len(tail) <= offset {
		return 0, false
	}
	pos := offset + 1
	count := int(tail[offset])
	for idx := 0; idx < count; idx++ {
		if pos+5 > len(tail) {
			return 0, false
		}
		pos += 5
		if pos+4 > len(tail) {
			return 0, false
		}
		rawLen := int(binary.LittleEndian.Uint32(tail[pos : pos+4]))
		pos += 4
		if rawLen < 0 || pos+rawLen > len(tail) {
			return 0, false
		}
		pos += rawLen
		if pos+10 > len(tail) {
			return 0, false
		}
		pos += 10
	}
	return pos, true
}

func writeCurrentSceneObjectEquipSummary(writer *packetWriter, rows []dnfrepo.CharacterRosterEquipSummary) {
	count := len(rows)
	if count > 0xff {
		count = 0xff
	}
	writer.writeByte(byte(count))
	for idx := 0; idx < count; idx++ {
		row := rows[idx]
		writer.writeByte(rosterByteValue(row.Slot, 0))
		writer.writeUint32(rosterUint32Value(row.ItemIDOrIcon, 0))
		writer.writeUint32(0) // sub_225C9B0(type=44) -> sub_3457C50 u32 len + raw; legacy InvenItem raw is not this current block.
		writer.writeByte(rosterByteValue(row.PackedFlags, 0))
		writer.writeUint32(rosterUint32Value(row.OptionalIDOrExpire, 0))
		writer.writeUint32(rosterUint32Value(row.AuxValue, 0))
		writer.writeByte(rosterByteValue(row.AuxFlag, 0))
	}
}

func packCurrentSceneObjectGrow(grow byte, subGrow byte) byte {
	// MCP sub_2009160：name 后第二个 u8 直接拆成 low4=grow、high3=subgrow；
	// 这里不能复用 roster 的 job|grow 打包，否则会把 job 写进动作/状态链并跳到 0x1818181C。
	return (grow & 0x0f) | ((subGrow & 0x07) << 4)
}

func writeCurrentSceneObjectName(writer *packetWriter, value string) {
	// MCP sub_2009160 -> upper_pkt_read_wstr -> sub_3457C80：
	// 字符串实际是 u32 字节长度 + raw name，再由客户端转宽字；不能写固定 UTF-16，
	// 否则名字长度和后续 job/grow/resource selector 会整体错位，城镇对象会变成“没有名字”。
	writer.writeRawDstr(rosterNameBytes(value))
}

func writeFixedUTF16(writer *packetWriter, value string, units int) {
	encoded := utf16.Encode([]rune(value))
	for idx := 0; idx < units; idx++ {
		var unit uint16
		if idx < len(encoded) {
			unit = encoded[idx]
		}
		writer.writeUint16(unit)
	}
}

func writeNullTerminatedUTF16(writer *packetWriter, value string, maxUnits int) {
	encoded := utf16.Encode([]rune(value))
	if maxUnits > 0 && len(encoded) >= maxUnits {
		encoded = encoded[:maxUnits-1]
	}
	for _, unit := range encoded {
		writer.writeUint16(unit)
	}
	writer.writeUint16(0)
}
