package dnfbridge

import (
	"encoding/binary"
	"strings"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func currentItemListEntryFromEquipment(equipped dnfrepo.EquipmentEntry) (currentItemListEntry, bool) {
	actorSlot, ok := currentEXEActorEquipmentSlot(equipped)
	if !ok || actorSlot > 32 {
		return currentItemListEntry{}, false
	}
	extra := equipped.Extra
	// Preserve the current-client row as the base, then project any durable
	// emblem state into its proved raw+0x3C..+0x44 vector.
	raw := append([]byte(nil), equipped.RawEntry...)
	entry := currentItemListEntryFromRaw(raw, extra)
	entry.patchCore(int16(actorSlot), sceneInventoryUint32FromInt64(equipped.ItemID), currentItemListEquipmentInstance(equipped))
	entry.setByte(0x0A, currentItemListEquipmentExtData(equipped))
	entry.setUint16(0x0B, currentItemListEquipmentDurability(equipped))
	entry.setByte(0x0D, currentItemListBindFlag(equipped.Bind, extra))
	entry.setUint32(0x0E, currentItemListEquipmentValueA(equipped))
	entry.setByte(0x12, sceneInventoryExtraByte(extra, "byte_12", "value_12"))
	entry.setByte(0x13, sceneInventoryExtraByte(extra, "byte_13", "value_13", "value_c"))
	entry.setUint16(0x14, sceneInventoryExtraUint16(extra, "marker_16", "marker16", "value_d"))
	expire := currentItemListEquipmentExpire(equipped)
	entry.clearLegacyWrongExpiration(expire)
	entry.setUint32(currentItemListExpireTimeOffset, expire)
	entry.setByte(0x57, sceneInventoryExtraByte(extra, "byte_57", "value_57"))
	entry.setByte(0x58, sceneInventoryExtraByte(extra, "byte_58", "value_58"))
	entry.setByte(0x59, sceneInventoryExtraByte(extra, "byte_59", "value_59"))
	entry.copyFixed(0x72, currentItemListFixedExtraBytes(extra, 5, "tail_data_72", "tail72"))
	entry.avatarSocketData = currentItemListAvatarSocketData(extra)
	entry.avatarColorData = currentItemListAvatarColorData(extra)
	currentApplyEquipmentSocketVectorToEntry(&entry, extra)
	currentApplyEquipmentEffectRuneToEntry(&entry, extra)
	return entry, true
}

func currentEquipmentUpdateEntryFromEquipment(equipped dnfrepo.EquipmentEntry) currentEquipmentUpdateEntry {
	extra := equipped.Extra
	var entry currentEquipmentUpdateEntry
	binary.LittleEndian.PutUint16(entry.data[0x00:0x02], uint16(equipped.SlotIndex))
	binary.LittleEndian.PutUint32(entry.data[0x02:0x06], sceneInventoryUint32FromInt64(equipped.ItemID))
	binary.LittleEndian.PutUint32(entry.data[0x06:0x0A], currentItemListEquipmentInstance(equipped))
	entry.data[0x0A] = currentItemListEquipmentExtData(equipped)
	binary.LittleEndian.PutUint16(entry.data[0x0B:0x0D], currentItemListEquipmentDurability(equipped))
	entry.data[0x0D] = currentEquipmentUpdateSealFlag(equipped)
	copy(entry.data[0x0E:0x16], currentEquipmentUpdatePrefixData(equipped))
	binary.LittleEndian.PutUint32(entry.data[0x16:0x1A], 0xFFFFFFFF)
	copy(entry.data[0x1A:0x2B], currentItemListFixedExtraBytes(extra, 17, "middle_data_1a", "middle1a", "raw_data_1a"))
	if expire := currentItemListEquipmentExpire(equipped); expire != 0 {
		binary.LittleEndian.PutUint32(entry.data[currentEquipmentUpdateExpireTimeOffset:currentEquipmentUpdateExpireTimeOffset+4], expire)
	}
	return entry
}

func currentEquipmentUpdateSealFlag(equipped dnfrepo.EquipmentEntry) byte {
	return sceneInventoryExtraByte(equipped.Extra, "equipment_update_seal_flag", "update_seal_flag")
}

func currentEquipmentUpdatePrefixData(equipped dnfrepo.EquipmentEntry) []byte {
	if data := currentItemListFixedExtraBytes(equipped.Extra, 8, "prefix_data_0e", "prefix0e", "raw_data_0e"); len(data) == 8 {
		return data
	}
	raw := equipped.RawEntry
	if currentEquipmentEntryHasCurrentRaw77(equipped) {
		out := make([]byte, 8)
		copy(out, raw[0x0E:0x16])
		return out
	}
	out := make([]byte, 8)
	if len(raw) >= 24 {
		copy(out[0:4], raw[16:20])
		out[4] = raw[20]
		out[5] = raw[21]
		copy(out[6:8], raw[22:24])
	}
	return out
}

func currentItemListEquipmentInstance(equipped dnfrepo.EquipmentEntry) uint32 {
	if value := currentEquippedCreatureInstance(equipped); value != 0 {
		return value
	}
	if value := sceneInventoryExtraUint32(equipped.Extra, "quality_seed", "current_exe_create_value", "amount_or_count", "amount", "count_or_instance", "instance_value", "item_uid", "serial"); value != 0 {
		return normalizeLegacyInitialEquipmentCreateValue(equipped, value)
	}
	raw := equipped.RawEntry
	if currentEquipmentEntryHasCurrentRaw77(equipped) {
		return binary.LittleEndian.Uint32(raw[6:10])
	}
	if len(raw) >= 9 {
		return normalizeLegacyInitialEquipmentCreateValue(equipped, binary.LittleEndian.Uint32(raw[5:9]))
	}
	return 0
}

func currentEquippedCreatureInstance(equipped dnfrepo.EquipmentEntry) uint32 {
	if equipped.SlotIndex != 26 {
		return 0
	}
	// The current creature constructor repeats its durable serial/handle at
	// legacy equipped-row +24. This value, rather than an equipment quality
	// seed, is sub_1D77560's create scalar for actor slot 26.
	if len(equipped.RawEntry) >= 28 {
		if value := binary.LittleEndian.Uint32(equipped.RawEntry[24:28]); value != 0 {
			return value
		}
	}
	return sceneInventoryExtraUint32(
		equipped.Extra,
		"creature_serial_or_handle",
		"creature_key",
		"creature_serial",
		"pet_serial",
	)
}

const legacyCSharpInitialEquipmentInstanceValue = 999999998

func isPVFInitialEquipment(equipped dnfrepo.EquipmentEntry) bool {
	return strings.EqualFold(strings.TrimSpace(equipped.Extra["source"]), "pvf_create_equipment_list")
}

func normalizeLegacyInitialEquipmentCreateValue(equipped dnfrepo.EquipmentEntry, value uint32) uint32 {
	if isPVFInitialEquipment(equipped) && value == legacyCSharpInitialEquipmentInstanceValue {
		return initialEquipmentCreateValue
	}
	return value
}

func currentItemListEquipmentValueA(equipped dnfrepo.EquipmentEntry) uint32 {
	if value := sceneInventoryExtraUint32(equipped.Extra, "value_a"); value != 0 {
		return value
	}
	if validCurrentEquipmentQualitySeed(sceneInventoryExtraUint32(equipped.Extra, "quality_seed")) {
		return sceneInventoryExtraUint32(equipped.Extra, "item_uid", "serial")
	}
	value := sceneInventoryExtraUint32(equipped.Extra, "instance_value", "item_uid", "serial", "count_or_instance")
	if isPVFInitialEquipment(equipped) && value == legacyCSharpInitialEquipmentInstanceValue {
		return 0
	}
	return value
}

func currentEquipmentRawEntry(equipped dnfrepo.EquipmentEntry) []byte {
	raw := append([]byte(nil), equipped.RawEntry...)
	if len(raw) >= 9 && isPVFInitialEquipment(equipped) && binary.LittleEndian.Uint32(raw[5:9]) == legacyCSharpInitialEquipmentInstanceValue {
		binary.LittleEndian.PutUint32(raw[5:9], initialEquipmentCreateValue)
	}
	return raw
}

func currentItemListEquipmentExtData(equipped dnfrepo.EquipmentEntry) byte {
	if value := sceneInventoryExtraByte(equipped.Extra, "ext_data0", "ext0", "packed_flag_byte", "packed_flag", "packed", "reinforce"); value != 0 {
		return value
	}
	raw := equipped.RawEntry
	if currentEquipmentEntryHasCurrentRaw77(equipped) {
		return raw[0x0A]
	}
	if len(raw) > 9 {
		return raw[9]
	}
	return 0
}

func currentItemListEquipmentDurability(equipped dnfrepo.EquipmentEntry) uint16 {
	if value := sceneInventoryExtraUint16(equipped.Extra, "durability", "max_durability"); value != 0 {
		return value
	}
	raw := equipped.RawEntry
	if currentEquipmentEntryHasCurrentRaw77(equipped) {
		return binary.LittleEndian.Uint16(raw[0x0B:0x0D])
	}
	if len(raw) >= 12 {
		return binary.LittleEndian.Uint16(raw[10:12])
	}
	return 0
}
