package dnfbridge

import (
	"sort"
	"strconv"
	"strings"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func (r *csharpLegacyUserInfoReader) buildCSharpUserInfoSubtype1(character dnfrepo.CharacterRecord, hasCharacter bool, charID uint16) []byte {
	row := r.one("legacy_character_subtype1_fields", csharpUserInfoSubtype1Columns())
	if !hasCharacter {
		character = dnfrepo.CharacterRecord{CharacterID: strconv.Itoa(int(charID)), Level: 1}
	}

	var w packetWriter
	w.writeByte(1)
	w.writeUint16(1)
	w.writeUint16(charID)
	w.writeUint32(uint32(numericCharacterStatValue(character, "exp")))
	// C# CharacterStatComputer 在 PVF 失败或旧角色没有 subtype1 行时会给 HP/MP/攻击等字段一组非零基础值。
	// MySQL 旧列默认 0 不能当作客户端可读属性，否则进场景后面板 HP/攻击等会显示 0 或 -1。
	w.writeUint32(rowStatU32(row, character, "stat_block_marker", 83))
	w.writeUint32(r.rowStatU32Real(row, character, "stat_hp_max"))
	w.writeUint32(r.rowStatU32Real(row, character, "stat_mp_max"))
	writeLegacyInt16(&w, r.rowStatIntReal(row, character, "stat_physical_attack"))
	writeLegacyInt16(&w, r.rowStatIntReal(row, character, "stat_physical_defense"))
	writeLegacyInt16(&w, r.rowStatIntReal(row, character, "stat_magical_attack"))
	writeLegacyInt16(&w, r.rowStatIntReal(row, character, "stat_magical_defense"))
	writeLegacyInt16(&w, rowStatInt(row, character, "stat_fire_resistance", 0))
	writeLegacyInt16(&w, rowStatInt(row, character, "stat_water_resistance", 0))
	writeLegacyInt16(&w, rowStatInt(row, character, "stat_dark_resistance", 0))
	writeLegacyInt16(&w, rowStatInt(row, character, "stat_light_resistance", 0))
	for idx := 0; idx < 17; idx++ {
		w.writeUint16(rowStatU16(row, character, "active_status_resistance_"+twoDigit(idx), 0))
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
	w.writeByte(csharpUserInfoStatLevel(row, character))
	w.writeByte(byte(numericCharacterStatValue(character, "ex_equip_slot_stat")))
	equippedEntries := r.loadSubtype1EquippedEntries(character)
	w.writeByte(byte(len(equippedEntries)))
	for _, entry := range equippedEntries {
		w.writeBytes(entry)
	}
	w.writeUint32(rowStatU32(row, character, "equip_list_trailing", 0))
	w.writeUint32(rowStatU32(row, character, "name_tag_item_id", 0))
	w.writeUint32(rowStatU32(row, character, "name_tag_expire_time", 0))
	w.writeByte(rowStatU8(row, character, "skill_tree_index", 0))
	w.writeByte(0)
	w.writeByte(0)
	w.writeByte(rowStatU8(row, character, "equipped_creature_level", 0))
	w.writeByte(0)
	w.writeByte(0)
	w.writeByte(0)
	w.writeByte(0)
	w.writeByte(0)
	w.writeByte(0)
	w.writeByte(rowStatU8(row, character, "manage_level", 0))
	w.writeUint32(0)
	w.writeByte(rowStatU8(row, character, "flag_byte", 0))
	w.writeUint32(rowStatU32(row, character, "guild_power_war", 0))
	w.writeUint32(rowStatU32(row, character, "server_timestamp", 0))
	w.writeUint16(rowStatU16(row, character, "quest_shop_count", 0))
	w.writeUint32(rowStatU32(row, character, "progress1", 0))
	w.writeUint32(rowStatU32(row, character, "progress2", 0))
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) loadSubtype1EquippedEntries(character dnfrepo.CharacterRecord) [][]byte {
	if r == nil || r.service == nil {
		return nil
	}
	if strings.TrimSpace(character.CharacterID) == "" {
		return nil
	}
	repos, ok := r.service.repositoryGroup()
	if !ok || repos.Equipment == nil {
		return nil
	}
	equipment, found, err := repos.Equipment.Load(r.ctx, character.CharacterID)
	if err != nil {
		if r.session != nil {
			r.service.logGameEvent(r.session, "game-upper-select-subtype1-equipment-load-failed", "character_id", character.CharacterID, "error", err)
		}
		return nil
	}
	if !found || len(equipment.Entries) == 0 {
		return nil
	}
	entries := make([]dnfrepo.EquipmentEntry, 0, len(equipment.Entries))
	for _, entry := range equipment.Entries {
		if entry.SlotIndex <= 0 || len(entry.RawEntry) == 0 {
			continue
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].SlotIndex < entries[j].SlotIndex
	})
	if len(entries) > 0xff {
		entries = entries[:0xff]
	}
	out := make([][]byte, 0, len(entries))
	for _, entry := range entries {
		out = append(out, currentEquipmentRawEntry(entry))
	}
	return out
}
