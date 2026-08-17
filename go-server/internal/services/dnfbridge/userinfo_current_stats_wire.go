package dnfbridge

import (
	"golang.org/x/text/encoding/simplifiedchinese"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func (r *csharpLegacyUserInfoReader) buildCurrentUserInfoMode1StatBlob(row dnfrepo.LegacyUserInfoRow, character dnfrepo.CharacterRecord) []byte {
	var w packetWriter
	/*
		// MCP: 女鬼剑 actor 虚函数 sub_24A5B20 读取当前 EXE 的 92 字节块：
		// +0/+4 为 dword，+8..+14 为 4 个 u16，+16 起 4 个 s16，+24 起 18 个 s16，后段固定读取 +60/+64/+66/+68/+72/+74/+76/+78/+80/+84/+85。
		hpMax := rowStatU32NonZero(row, character, "stat_hp_max", 11600)
	*/
	// Current EXE sub_3841230 expands this 92-byte actor state and sub_1D1EDE0
	// maps +8..+14 to strength, vitality, intelligence, and spirit. The next
	// four s16 fields are elemental resistances, followed by 18 status
	// resistances and the inventory/speed/weight fields.
	hpMax := r.rowStatU32Real(row, character, "stat_hp_max")
	mpMax := r.rowStatU32Real(row, character, "stat_mp_max")
	w.writeUint32(hpMax)
	w.writeUint32(mpMax)
	for _, value := range []int{
		r.rowStatIntReal(row, character, "stat_strength"),
		r.rowStatIntReal(row, character, "stat_vitality"),
		r.rowStatIntReal(row, character, "stat_intelligence"),
		r.rowStatIntReal(row, character, "stat_spirit"),
		r.rowStatIntReal(row, character, "stat_fire_resistance"),
		r.rowStatIntReal(row, character, "stat_water_resistance"),
		r.rowStatIntReal(row, character, "stat_dark_resistance"),
		r.rowStatIntReal(row, character, "stat_light_resistance"),
	} {
		writeLegacyInt16(&w, currentActorStatS16Wire(value))
	}
	for idx := 0; idx < 18; idx++ {
		value := int(rowStatU16(row, character, "active_status_resistance_"+twoDigit(idx), 0))
		writeLegacyInt16(&w, currentActorStatS16Wire(value))
	}
	w.writeUint32(r.rowStatU32Real(row, character, "stat_inventory_limit"))
	w.writeUint16(r.rowStatU16Real(row, character, "stat_hp_regen_speed"))
	w.writeUint16(r.rowStatU16Real(row, character, "stat_mp_regen_speed"))
	w.writeUint32(r.rowStatU32Real(row, character, "stat_move_speed"))
	w.writeUint16(r.rowStatU16Real(row, character, "stat_attack_speed"))
	w.writeUint16(r.rowStatU16Real(row, character, "stat_cast_speed"))
	w.writeUint16(r.rowStatU16Real(row, character, "stat_hit_recovery"))
	w.writeUint16(r.rowStatU16Real(row, character, "stat_jump_power"))
	w.writeUint32(r.rowStatU32Real(row, character, "stat_weight"))
	// Current NoPack sub_3841230 copies +0x54 to actor+0x5F04. sub_24D5640
	// consumes it as the base-stat percentage: four dimensions use
	// percentage*wire/1000 and current HP uses percentage*maxHP/100. The
	// neutral protocol value is 100; actor level comes from the separate mode0
	// level byte propagated into ability context 3.
	w.writeByte(currentActorBaseStatScalePercent)
	// Current NoPack sub_3841230 copies source +0x55 into the actor state, and
	// sub_24A5B20 consumes it as a float scaled by 0.1. It is not the durable
	// ex_equip_slot_stat byte carried by the selected-character subtype1 body.
	// Keep this unresolved runtime float neutral instead of leaking slot bits.
	w.writeUint32(0)

	out := w.bytes()
	switch {
	case len(out) < currentMode1StatBlobWireSize:
		padded := make([]byte, currentMode1StatBlobWireSize)
		copy(padded, out)
		return padded
	case len(out) > currentMode1StatBlobWireSize:
		return append([]byte(nil), out[:currentMode1StatBlobWireSize]...)
	default:
		return out
	}
}

func currentActorStatS16Wire(value int) int {
	scaled := value * 10
	switch {
	case scaled > 32767:
		return 32767
	case scaled < -32768:
		return -32768
	default:
		return scaled
	}
}

func clampUint16ForUserInfo(value uint32) uint16 {
	if value > 0xffff {
		return 0xffff
	}
	return uint16(value)
}

func csharpUserInfoSubtype1Columns() []string {
	columns := []string{
		"stat_hp_max", "stat_mp_max", "stat_strength", "stat_intelligence", "stat_vitality", "stat_spirit",
		"stat_physical_attack", "stat_physical_defense", "stat_magical_attack", "stat_magical_defense",
		"stat_independent_attack", "stat_fire_resistance", "stat_water_resistance", "stat_dark_resistance", "stat_light_resistance",
	}
	for idx := 0; idx < 17; idx++ {
		columns = append(columns, "active_status_resistance_"+twoDigit(idx))
	}
	columns = append(columns,
		"stat_inventory_limit", "stat_hp_regen_speed", "stat_mp_regen_speed", "stat_move_speed",
		"stat_attack_speed", "stat_cast_speed", "stat_hit_recovery", "stat_jump_power", "stat_weight",
		"stat_level", "name_tag_item_id", "name_tag_expire_time", "skill_tree_index",
		"equipped_creature_level", "equip_list_trailing", "manage_level", "flag_byte",
		"guild_power_war", "server_timestamp", "quest_shop_count", "progress1", "progress2")
	return columns
}

func writeClientNameDstr(w *packetWriter, name string) {
	if encoded, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(name)); err == nil {
		w.writeRawDstr(encoded)
		return
	}
	w.writeRawDstr([]byte(name))
}

func writeFixedBytes(w *packetWriter, data []byte, size int) {
	for idx := 0; idx < size; idx++ {
		var value byte
		if idx < len(data) {
			value = data[idx]
		}
		w.writeByte(value)
	}
}

func writeLegacyInt16(w *packetWriter, value int) {
	w.writeUint16(uint16(int16(value)))
}

func (r *csharpLegacyUserInfoReader) rowStatU16Real(row dnfrepo.LegacyUserInfoRow, character dnfrepo.CharacterRecord, column string) uint16 {
	value, ok := r.realStatInt64(row, character, column)
	if !ok || value <= 0 {
		return 0
	}
	if value > 0xffff {
		return 0xffff
	}
	return uint16(value)
}

func (r *csharpLegacyUserInfoReader) rowStatU32Real(row dnfrepo.LegacyUserInfoRow, character dnfrepo.CharacterRecord, column string) uint32 {
	value, ok := r.realStatInt64(row, character, column)
	if !ok || value <= 0 {
		return 0
	}
	if value > 0xffffffff {
		return 0xffffffff
	}
	return uint32(value)
}

func (r *csharpLegacyUserInfoReader) rowStatIntReal(row dnfrepo.LegacyUserInfoRow, character dnfrepo.CharacterRecord, column string) int {
	value, ok := r.realStatInt64(row, character, column)
	if !ok {
		return 0
	}
	if value > int64(^uint(0)>>1) {
		return int(^uint(0) >> 1)
	}
	if value < -int64(^uint(0)>>1)-1 {
		return -int(^uint(0)>>1) - 1
	}
	return int(value)
}
