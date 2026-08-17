package dnfbridge

import dnfrepo "longheng.io/server/internal/modules/dnf/repository"

func defaultNoPackCharacterRoster(objectID int) dnfrepo.CharacterRoster {
	return dnfrepo.CharacterRoster{
		Header: dnfrepo.CharacterRosterHeader{
			UnkA:             latestRosterRouteNormal,
			UnkB:             latestRosterContextNormal,
			TotalOrSlotLimit: defaultCharacterSlots,
			UsedOrRemain:     defaultCharacterSlots,
			PageCount:        rosterDefaultPageCount,
			RosterFlag:       latestCharacterCreateEnabled,
		},
		Entry: dnfrepo.CharacterRosterEntry{
			ObjectID: int64(objectID),
		},
	}
}

func rosterState0Value(value int64) byte {
	// 早期误把 1 当普通态写入 roster_json；最新客户端会把它显示成改名/特殊状态，老数据这里降回普通态。
	if value == 1 {
		value = 0
	}
	return rosterByteValue(value, latestCharacterStateNormal)
}

func rosterObjectID(character dnfrepo.CharacterRecord) int {
	if value := numericCharacterStatValue(character, "roster_object_id"); value > 0 {
		if value > int64(^uint(0)>>1) {
			return int(^uint(0) >> 1)
		}
		return int(value)
	}
	return numericCharacterID(character)
}

func rosterSlotValue(slot int, fallback int) int {
	if slot >= 0 && slot < defaultCharacterSlots {
		return slot
	}
	if fallback >= 0 && fallback < defaultCharacterSlots {
		return fallback
	}
	return 0
}

func packRosterJobGrow(job, grow byte) byte {
	return (job & 0x0f) | ((grow & 0x07) << 4)
}

func defaultCreatedCharacterStats(optionLen int) map[string]int64 {
	// 这些键只给 legacy USERINFO fallback 使用；创建角色的主数据以 MySQL 列为准，不再写入 stats_json。
	return map[string]int64{
		"grow_type":              0,
		"exp":                    0,
		"ex_equip_slot_stat":     0,
		"bonus_sp":               0,
		"bonus_tp":               0,
		"pvp_grade":              0,
		"pvp_rating_grade":       0,
		"user_state":             0,
		"tutorial_completed":     0,
		"gold":                   0,
		"coin":                   0,
		"town_id":                newCharacterInitialTownID,
		"area_id":                newCharacterInitialAreaID,
		"pos_x":                  newCharacterInitialPosX,
		"pos_y":                  newCharacterInitialPosY,
		"direction":              newCharacterInitialDirection,
		"area_state":             newCharacterInitialAreaState,
		"delete_flag":            0,
		"is_event_character":     0,
		"name_tag_item_id":       0,
		"stamina":                0,
		"fatigue":                newCharacterInitialFatigue,
		"fatigue_limit":          newCharacterFatigueLimit,
		"fatigue_penalty":        0,
		"pc_room_id":             0x00010001,
		"is_private_store":       0,
		"is_premium_pc_room":     0,
		"server_group_id":        0,
		"black_count":            0,
		"guild_level":            0,
		"chaos_point":            0,
		"disguise_kind":          0,
		"is_disguised":           0,
		"expert_job_type":        0,
		"expert_job_exp":         0,
		"extra46":                0,
		"extra47":                0,
		"extra51":                0,
		"is_hardcore_mode":       0,
		"is_hardcore_dead":       0,
		"hardcore_death_count":   0,
		"user_state_bits":        3,
		"chat_ban_end_time":      0,
		"fatigue_update":         0,
		"return_user_flag":       1,
		"channel_display_mode":   0,
		"channel_type":           0,
		"channel_id":             newCharacterInitialChannelID,
		"is_return_user":         0,
		"link_slot_enabled":      0,
		"link_type_a":            0,
		"link_type_b":            0,
		"emotion_index":          0,
		"action_byte":            0,
		"fatigue_display_update": 0,
		"costume_flag":           0,
		"aura_flag":              0,
		"pet_display_flag":       0,
		"title_display_flag":     0,
		"pvp_stat_a":             0,
		"pvp_win_streak":         0,
		"pvp_lose_streak":        0,
		"pvp_rank_point":         0,
		"trailing_byte":          0,
		"create_option_len":      int64(optionLen),
	}
}

func defaultCreatedCharacterStatsFromRequest(req createCharacterRequest) map[string]int64 {
	stats := defaultCreatedCharacterStats(len(req.options))
	// 当前 Go 仓储还没有完整 subtype/init-body 写接口，USERINFO 构造保留这些只读兜底字段。
	stats["pc_room_id"] = 0x00010001
	stats["user_state_bits"] = 3
	stats["return_user_flag"] = 1
	stats["channel_id"] = 2
	// Current-EXE op1378 consumes a per-character last story-summary level.
	// Characters created by this Origin-era server start already migrated, so
	// they must not replay legacy summaries on their first scene entry.
	stats[dnfrepo.CharacterStoryDigestLastLevelStatKey] = int64(newCharacterInitialLevel)
	stats[dnfrepo.CharacterStoryDigestMigrationVersionStatKey] = int64(dnfrepo.CurrentCharacterStoryDigestMigrationVersion)
	seedCreatedSubtype1Stats(stats)
	seedCreatedRosterStats(stats)
	stats["create_option_len"] = int64(len(req.options))
	for idx := 0; idx < 64; idx++ {
		stats["create_option_byte_"+twoDigit(idx)] = 0
	}
	storeByteStats(stats, "create_option_byte", req.options, 64)
	return stats
}

func seedCreatedSubtype1Stats(stats map[string]int64) {
	/*
		// C# CharacterStatComputer 在 PVF 失败时使用这组 fallback，并额外加 Premium HP/MP；
		// dnfbridge 当前没有静态数据注入，先按 C# fallback 保证新角色不是空 subtype1。
		for key, value := range map[string]int64{
		// 数值属性由 PVF 计算；这里只初始化 subtype1 的非数值状态字段。
		for key, value := range map[string]int64{
	*/
	for key, value := range map[string]int64{
		"stat_level":              csharpSubtype1ProtocolStatLevel,
		"stat_block_marker":       csharpSubtype1StatBlockMarker,
		"name_tag_expire_time":    0,
		"equip_list_trailing":     0,
		"skill_tree_index":        0,
		"equipped_creature_level": 0,
		"manage_level":            0,
		"flag_byte":               0,
		"guild_power_war":         0,
		"server_timestamp":        0,
		"quest_shop_count":        0,
		"progress1":               0,
		"progress2":               0,
	} {
		stats[key] = value
	}
	for idx := 0; idx < 18; idx++ {
		stats["active_status_resistance_"+twoDigit(idx)] = 0
	}
}

func seedCreatedRosterStats(stats map[string]int64) {
	for _, key := range []string{
		"roster_state0",
		"roster_time_a",
		"roster_time_b",
		"roster_value0",
		"roster_value1",
		"roster_value2",
		"roster_reserved_a",
		"roster_reserved_b",
		"roster_value3",
		"roster_object_id",
		"roster_flag0_eq1",
		"roster_card_flag",
		"roster_value5",
		"roster_display_flags",
		"roster_flag6_eq1",
	} {
		stats[key] = 0
	}
	for idx := 0; idx < rosterLinkedIDBlockSize; idx++ {
		stats["roster_linked_id_"+twoDigit(idx)] = 0
	}
	for idx := 0; idx < 12; idx++ {
		stats["roster_tail_"+twoDigit(idx)] = 0
	}
}

func storeByteStats(stats map[string]int64, prefix string, data []byte, limit int) {
	if limit < 0 {
		limit = 0
	}
	for idx, value := range data {
		if idx >= limit {
			break
		}
		stats[prefix+"_"+twoDigit(idx)] = int64(value)
	}
}
