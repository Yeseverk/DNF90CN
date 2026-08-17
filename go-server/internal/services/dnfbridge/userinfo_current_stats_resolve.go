package dnfbridge

import (
	"strings"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func (r *csharpLegacyUserInfoReader) realStatInt64(row dnfrepo.LegacyUserInfoRow, character dnfrepo.CharacterRecord, column string) (int64, bool) {
	if value, ok := r.pvfStatInt64(column); ok && value != 0 {
		if rowValue, rowOK := rowIntOK(row, column); rowOK && rowValue != 0 &&
			!isKnownLegacyFallbackStat(column, int64(rowValue)) &&
			!isPreCSharpHPMPCompatibilityStat(column, int64(rowValue), value) {
			return int64(rowValue), true
		}
		if statValue, statOK := statInt64OK(character, column); statOK && statValue != 0 &&
			!isKnownLegacyFallbackStat(column, statValue) &&
			!isPreCSharpHPMPCompatibilityStat(column, statValue, value) {
			return statValue, true
		}
		return value, true
	}
	if value, ok := rowIntOK(row, column); ok && value != 0 {
		return int64(value), true
	}
	if value, ok := statInt64OK(character, column); ok && value != 0 {
		return value, true
	}
	return 0, false
}

func isKnownLegacyFallbackStat(column string, value int64) bool {
	switch column {
	case "stat_hp_max":
		return value == 11600
	case "stat_mp_max":
		return value == 11900
	case "stat_physical_attack":
		return value == 75
	case "stat_physical_defense":
		return value == 75
	case "stat_magical_attack":
		return value == 45
	case "stat_magical_defense":
		return value == 45
	case "stat_inventory_limit":
		return value == 480000
	case "stat_mp_regen_speed":
		return value == 500
	case "stat_move_speed":
		return value == 8500
	case "stat_attack_speed":
		return value == 8500
	case "stat_cast_speed":
		return value == 7000
	case "stat_hit_recovery":
		return value == 6000
	case "stat_jump_power":
		return value == 4300
	case "stat_weight":
		return value == 500000
	default:
		return false
	}
}

func (r *csharpLegacyUserInfoReader) pvfStatInt64(column string) (int64, bool) {
	if r == nil || !r.hasPVFStats {
		return 0, false
	}
	switch column {
	case "stat_hp_max":
		return r.pvfStats.HPMax, true
	case "stat_mp_max":
		return r.pvfStats.MPMax, true
	case "stat_strength":
		return r.pvfStats.Strength, true
	case "stat_intelligence":
		return r.pvfStats.Intelligence, true
	case "stat_vitality":
		return r.pvfStats.Vitality, true
	case "stat_spirit":
		return r.pvfStats.Spirit, true
	case "stat_physical_attack":
		return r.pvfStats.PhysicalAttack, true
	case "stat_physical_defense":
		return r.pvfStats.PhysicalDefense, true
	case "stat_magical_attack":
		return r.pvfStats.MagicalAttack, true
	case "stat_magical_defense":
		return r.pvfStats.MagicalDefense, true
	case "stat_independent_attack":
		return r.pvfStats.IndependentAttack, true
	case "stat_fire_resistance":
		return r.pvfStats.FireResistance, true
	case "stat_water_resistance":
		return r.pvfStats.WaterResistance, true
	case "stat_dark_resistance":
		return r.pvfStats.DarkResistance, true
	case "stat_light_resistance":
		return r.pvfStats.LightResistance, true
	case "stat_inventory_limit":
		return r.pvfStats.InventoryLimit, true
	case "stat_hp_regen_speed":
		return r.pvfStats.HPRegenSpeed, true
	case "stat_mp_regen_speed":
		return r.pvfStats.MPRegenSpeed, true
	case "stat_move_speed":
		return r.pvfStats.MoveSpeed, true
	case "stat_attack_speed":
		return r.pvfStats.AttackSpeed, true
	case "stat_cast_speed":
		return r.pvfStats.CastSpeed, true
	case "stat_hit_recovery":
		return r.pvfStats.HitRecovery, true
	case "stat_jump_power":
		return r.pvfStats.JumpPower, true
	case "stat_weight":
		return r.pvfStats.Weight, true
	default:
		return 0, false
	}
}

func (r *csharpLegacyUserInfoReader) logMissingCurrentUserInfoStats(row dnfrepo.LegacyUserInfoRow, character dnfrepo.CharacterRecord) {
	if r == nil || r.service == nil {
		return
	}
	required := []string{
		"stat_hp_max", "stat_mp_max", "stat_strength", "stat_intelligence", "stat_vitality", "stat_spirit",
		"stat_physical_attack", "stat_physical_defense", "stat_magical_attack", "stat_magical_defense",
		"stat_inventory_limit", "stat_move_speed", "stat_attack_speed", "stat_cast_speed", "stat_weight",
	}
	missing := make([]string, 0)
	for _, column := range required {
		if _, ok := r.realStatInt64(row, character, column); !ok {
			missing = append(missing, column)
		}
	}
	if len(missing) == 0 {
		return
	}
	r.service.logPacketEvent("dnf-character-stat-fields-missing",
		"conn_id", sessionConnID(r.session),
		"character_id", character.CharacterID,
		"missing", strings.Join(missing, ","),
		"has_pvf_stats", r.hasPVFStats)
}
