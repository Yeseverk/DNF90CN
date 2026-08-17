// 本文件把 DNF PVF 角色成长表接入 dnfbridge 的 USERINFO 拼包流程。
package dnfbridge

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	dnfcharstat "longheng.io/server/internal/modules/dnf/charstat"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const (
	csharpPremiumHPBonus int64 = 9800
	csharpPremiumMPBonus int64 = 10500
)

func (s *Service) preloadCharacterStatTable(ctx context.Context) error {
	table, err := s.loadCharacterStatTable(ctx)
	if err != nil {
		return err
	}
	s.logPacketEvent("dnf-character-stat-pvf-loaded", "jobs", table.Snapshot().Jobs)
	return nil
}

func (s *Service) loadCharacterStatTable(ctx context.Context) (*dnfcharstat.Table, error) {
	if s == nil {
		return nil, fmt.Errorf("dnfbridge service is nil")
	}
	s.characterStatsMu.Lock()
	if s.characterStats != nil {
		table := s.characterStats
		s.characterStatsMu.Unlock()
		return table, nil
	}
	if s.characterStatsLoadErr != nil {
		err := s.characterStatsLoadErr
		s.characterStatsMu.Unlock()
		return nil, err
	}
	s.characterStatsMu.Unlock()

	s.initialEquipmentMu.Lock()
	archive, err := s.initialEquipmentArchiveLocked()
	s.initialEquipmentMu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("load character stat pvf archive: %w", err)
	}
	table, err := dnfcharstat.Load(ctx, archive, dnfcharstat.Options{})

	s.characterStatsMu.Lock()
	defer s.characterStatsMu.Unlock()
	if err != nil {
		s.characterStatsLoadErr = fmt.Errorf("load character stat pvf table: %w", err)
		return nil, s.characterStatsLoadErr
	}
	s.characterStats = table
	return table, nil
}

func (s *Service) characterPVFStatsForUserInfo(ctx context.Context, session *gameSession, character dnfrepo.CharacterRecord, hasCharacter bool) (dnfcharstat.Vector, bool) {
	if !hasCharacter {
		return dnfcharstat.Vector{}, false
	}
	jobText := strings.TrimSpace(character.Job)
	if jobText == "" {
		s.logCharacterStatMissing(session, character, "job_missing", nil)
		return dnfcharstat.Vector{}, false
	}
	job, err := strconv.Atoi(jobText)
	if err != nil || job < 0 || job > 0xff {
		s.logCharacterStatMissing(session, character, "job_invalid", err)
		return dnfcharstat.Vector{}, false
	}
	level := character.Level
	if level <= 0 {
		level = 1
	}
	growType := byte(numericCharacterStatValue(character, "grow_type"))
	s.characterStatsMu.Lock()
	table := s.characterStats
	loadErr := s.characterStatsLoadErr
	s.characterStatsMu.Unlock()
	if table == nil {
		s.logCharacterStatMissing(session, character, "pvf_table_not_preloaded", loadErr)
		return dnfcharstat.Vector{}, false
	}
	stats, err := table.Compute(byte(job), growType, level)
	if err != nil {
		s.logCharacterStatMissing(session, character, "pvf_compute_failed", err)
		return dnfcharstat.Vector{}, false
	}
	pvfHPMax := stats.HPMax
	pvfMPMax := stats.MPMax
	stats = applyCSharpHPMPCompatibility(stats)
	s.logPacketEvent("dnf-character-stat-pvf-computed",
		"conn_id", sessionConnID(session),
		"character_id", character.CharacterID,
		"job", job,
		"grow_type", growType,
		"level", level,
		"hp_max", pvfHPMax,
		"mp_max", pvfMPMax,
		"userinfo_hp_max", stats.HPMax,
		"userinfo_mp_max", stats.MPMax,
		"hp_compatibility_bonus", csharpPremiumHPBonus,
		"mp_compatibility_bonus", csharpPremiumMPBonus,
		"strength", stats.Strength,
		"physical_attack", stats.PhysicalAttack,
		"source", "pvf_character_growth_base_plus_csharp_hpmp_compatibility")
	return stats, true
}

func applyCSharpHPMPCompatibility(stats dnfcharstat.Vector) dnfcharstat.Vector {
	// 86JP CharacterStatComputer.BuildAdditionalInfo starts from a freshly
	// computed PVF vector and then adds these two fixed compatibility values.
	// Keeping the operation on the fresh vector makes repeated recalculation
	// idempotent; no other stat is changed here.
	stats.HPMax += csharpPremiumHPBonus
	stats.MPMax += csharpPremiumMPBonus
	return stats
}

func isPreCSharpHPMPCompatibilityStat(column string, storedValue int64, computedValue int64) bool {
	switch column {
	case "stat_hp_max":
		return computedValue >= csharpPremiumHPBonus && storedValue == computedValue-csharpPremiumHPBonus
	case "stat_mp_max":
		return computedValue >= csharpPremiumMPBonus && storedValue == computedValue-csharpPremiumMPBonus
	default:
		return false
	}
}

func (s *Service) applyCharacterPVFStatsForCreate(ctx context.Context, record *dnfrepo.CharacterRecord) {
	if record == nil {
		return
	}
	stats, ok := s.characterPVFStatsForUserInfo(ctx, nil, *record, true)
	if !ok {
		return
	}
	applyCharacterPVFStats(record, stats)
}

func characterPVFStatsNeedRepair(record dnfrepo.CharacterRecord, stats dnfcharstat.Vector) bool {
	if _, ok := record.Stats["stat_level"]; !ok {
		return true
	}
	for key, value := range characterPVFStatValues(stats) {
		if record.Stats[key] != value {
			return true
		}
	}
	return false
}

func repairSelectedCharacterPVFStats(
	ctx context.Context,
	repository dnfrepo.CharacterRepository,
	record dnfrepo.CharacterRecord,
) error {
	return dnfrepo.SaveCharacterFields(ctx, repository, record, dnfrepo.CharacterFieldStats)
}

func applyCharacterPVFStats(record *dnfrepo.CharacterRecord, stats dnfcharstat.Vector) {
	if record == nil {
		return
	}
	if record.Stats == nil {
		record.Stats = make(map[string]int64)
	}
	for key, value := range characterPVFStatValues(stats) {
		record.Stats[key] = value
	}
	if _, ok := record.Stats["stat_level"]; !ok {
		record.Stats["stat_level"] = csharpSubtype1ProtocolStatLevel
	}
}

func characterPVFStatValues(stats dnfcharstat.Vector) map[string]int64 {
	return map[string]int64{
		"stat_hp_max":             stats.HPMax,
		"stat_mp_max":             stats.MPMax,
		"stat_strength":           stats.Strength,
		"stat_intelligence":       stats.Intelligence,
		"stat_vitality":           stats.Vitality,
		"stat_spirit":             stats.Spirit,
		"stat_physical_attack":    stats.PhysicalAttack,
		"stat_physical_defense":   stats.PhysicalDefense,
		"stat_magical_attack":     stats.MagicalAttack,
		"stat_magical_defense":    stats.MagicalDefense,
		"stat_independent_attack": stats.IndependentAttack,
		"stat_fire_resistance":    stats.FireResistance,
		"stat_water_resistance":   stats.WaterResistance,
		"stat_dark_resistance":    stats.DarkResistance,
		"stat_light_resistance":   stats.LightResistance,
		"stat_inventory_limit":    stats.InventoryLimit,
		"stat_hp_regen_speed":     stats.HPRegenSpeed,
		"stat_mp_regen_speed":     stats.MPRegenSpeed,
		"stat_move_speed":         stats.MoveSpeed,
		"stat_attack_speed":       stats.AttackSpeed,
		"stat_cast_speed":         stats.CastSpeed,
		"stat_hit_recovery":       stats.HitRecovery,
		"stat_jump_power":         stats.JumpPower,
		"stat_weight":             stats.Weight,
	}
}

func (s *Service) characterPVFStatValuesForLevel(character dnfrepo.CharacterRecord, level int) (map[string]int64, error) {
	jobText := strings.TrimSpace(character.Job)
	job, err := strconv.Atoi(jobText)
	if err != nil || job < 0 || job > 0xff {
		return nil, fmt.Errorf("compute character level stats: invalid job %q", character.Job)
	}
	s.characterStatsMu.Lock()
	table := s.characterStats
	loadErr := s.characterStatsLoadErr
	s.characterStatsMu.Unlock()
	if table == nil {
		if loadErr == nil {
			table, loadErr = s.loadCharacterStatTable(context.Background())
		}
		if loadErr != nil {
			return nil, fmt.Errorf("compute character level stats: %w", loadErr)
		}
	}
	stats, err := table.Compute(byte(job), byte(numericCharacterStatValue(character, "grow_type")), level)
	if err != nil {
		return nil, fmt.Errorf("compute character level stats: %w", err)
	}
	return characterPVFStatValues(applyCSharpHPMPCompatibility(stats)), nil
}

func (s *Service) logCharacterStatMissing(session *gameSession, character dnfrepo.CharacterRecord, reason string, err error) {
	fields := []any{
		"conn_id", sessionConnID(session),
		"character_id", character.CharacterID,
		"job", character.Job,
		"level", character.Level,
		"grow_type", numericCharacterStatValue(character, "grow_type"),
		"reason", reason,
	}
	if err != nil {
		fields = append(fields, "error", err)
	}
	s.logPacketEvent("dnf-character-stat-pvf-missing", fields...)
}

func sessionConnID(session *gameSession) string {
	if session == nil {
		return ""
	}
	return session.connID
}
