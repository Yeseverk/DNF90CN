package dnfbridge

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	dnfcharstat "longheng.io/server/internal/modules/dnf/charstat"
	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func (s *Service) preloadEquipmentStatIndex(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("dnfbridge service is nil")
	}
	s.equipmentStatsMu.Lock()
	if s.equipmentStatPaths != nil {
		s.equipmentStatsMu.Unlock()
		return nil
	}
	if s.equipmentStatsLoadErr != nil {
		err := s.equipmentStatsLoadErr
		s.equipmentStatsMu.Unlock()
		return err
	}
	s.equipmentStatsMu.Unlock()

	s.initialEquipmentMu.Lock()
	archive, err := s.initialEquipmentArchiveLocked()
	s.initialEquipmentMu.Unlock()
	if err != nil {
		return fmt.Errorf("load equipment stat pvf archive: %w", err)
	}
	paths, err := initialEquipmentPathMap(archive)

	s.equipmentStatsMu.Lock()
	defer s.equipmentStatsMu.Unlock()
	if err != nil {
		s.equipmentStatsLoadErr = fmt.Errorf("load equipment stat pvf index: %w", err)
		return s.equipmentStatsLoadErr
	}
	s.equipmentStatPaths = paths
	s.equipmentStats = make(map[int64]dnfcharstat.Vector)
	s.logPacketEvent("dnf-equipment-stat-pvf-index-loaded", "items", len(paths))
	return nil
}

func (s *Service) addEquipmentPVFStatsForUserInfo(ctx context.Context, session *gameSession, character dnfrepo.CharacterRecord, base dnfcharstat.Vector) dnfcharstat.Vector {
	if s == nil || strings.TrimSpace(character.CharacterID) == "" {
		return base
	}
	repos, ok := s.repositoryGroup()
	if !ok || repos.Equipment == nil {
		return base
	}
	equipment, found, err := repos.Equipment.Load(ctx, character.CharacterID)
	if err != nil {
		s.logPacketEvent("dnf-equipment-stat-load-failed",
			"conn_id", sessionConnID(session),
			"character_id", character.CharacterID,
			"error", err)
		return base
	}
	if !found || len(equipment.Entries) == 0 {
		return base
	}
	out := base
	count := 0
	var physAtk, physDef int64
	for _, entry := range equipment.Entries {
		if entry.ItemID <= 0 {
			continue
		}
		stats, ok := s.equipmentPVFStat(ctx, entry.ItemID, entry.Extra)
		if !ok {
			s.logPacketEvent("dnf-equipment-stat-pvf-missing",
				"conn_id", sessionConnID(session),
				"character_id", character.CharacterID,
				"item_id", entry.ItemID,
				"slot", entry.SlotIndex)
			continue
		}
		out.Add(stats)
		count++
		physAtk += stats.PhysicalAttack
		physDef += stats.PhysicalDefense
	}
	if count > 0 {
		s.logPacketEvent("dnf-equipment-stat-pvf-applied",
			"conn_id", sessionConnID(session),
			"character_id", character.CharacterID,
			"count", count,
			"physical_attack_add", physAtk,
			"physical_defense_add", physDef)
	}
	return out
}

func (s *Service) equipmentPVFStat(ctx context.Context, itemID int64, extra map[string]string) (dnfcharstat.Vector, bool) {
	if s == nil || itemID <= 0 {
		return dnfcharstat.Vector{}, false
	}
	s.equipmentStatsMu.Lock()
	if s.equipmentStats != nil {
		if stats, ok := s.equipmentStats[itemID]; ok {
			s.equipmentStatsMu.Unlock()
			return stats, true
		}
	}
	refPath := ""
	if s.equipmentStatPaths != nil {
		refPath = s.equipmentStatPaths[itemID]
	}
	s.equipmentStatsMu.Unlock()

	if refPath == "" && extra != nil {
		refPath = strings.TrimSpace(extra["pvf_path"])
	}
	if refPath == "" {
		return dnfcharstat.Vector{}, false
	}
	if err := ctx.Err(); err != nil {
		s.logPacketEvent("dnf-equipment-stat-pvf-canceled", "item_id", itemID, "error", err)
		return dnfcharstat.Vector{}, false
	}

	s.initialEquipmentMu.Lock()
	archive, err := s.initialEquipmentArchiveLocked()
	s.initialEquipmentMu.Unlock()
	if err != nil {
		s.logPacketEvent("dnf-equipment-stat-pvf-archive-missing", "item_id", itemID, "error", err)
		return dnfcharstat.Vector{}, false
	}
	text, actualPath, err := readInitialPVFText(archive, initialPVFPath("equipment", refPath), refPath)
	if err != nil {
		s.logPacketEvent("dnf-equipment-stat-pvf-read-failed", "item_id", itemID, "path", refPath, "error", err)
		return dnfcharstat.Vector{}, false
	}
	doc, err := dnfpvf.Parse(actualPath, text)
	if err != nil {
		s.logPacketEvent("dnf-equipment-stat-pvf-parse-failed", "item_id", itemID, "path", actualPath, "error", err)
		return dnfcharstat.Vector{}, false
	}
	stats := equipmentStatVector(doc)

	s.equipmentStatsMu.Lock()
	if s.equipmentStats == nil {
		s.equipmentStats = make(map[int64]dnfcharstat.Vector)
	}
	s.equipmentStats[itemID] = stats
	s.equipmentStatsMu.Unlock()
	return stats, true
}

func equipmentStatVector(doc *dnfpvf.Document) dnfcharstat.Vector {
	return dnfcharstat.Vector{
		HPMax:             equipmentStatNumber(doc, "HP MAX", "hp max"),
		MPMax:             equipmentStatNumber(doc, "MP MAX", "mp max"),
		Strength:          equipmentStatNumber(doc, "strength"),
		Intelligence:      equipmentStatNumber(doc, "intelligence"),
		Vitality:          equipmentStatNumber(doc, "vitality"),
		Spirit:            equipmentStatNumber(doc, "spirit"),
		PhysicalAttack:    equipmentStatNumber(doc, "physical attack", "physical weapon attack", "equipment physical attack"),
		PhysicalDefense:   equipmentStatNumber(doc, "physical defense", "equipment physical defense"),
		MagicalAttack:     equipmentStatNumber(doc, "magical attack", "magical weapon attack", "equipment magical attack"),
		MagicalDefense:    equipmentStatNumber(doc, "magical defense", "equipment magical defense"),
		IndependentAttack: equipmentStatNumber(doc, "independent attack", "separate attack"),
		FireResistance:    equipmentStatNumber(doc, "fire resistance"),
		WaterResistance:   equipmentStatNumber(doc, "water resistance"),
		DarkResistance:    equipmentStatNumber(doc, "dark resistance"),
		LightResistance:   equipmentStatNumber(doc, "light resistance"),
		InventoryLimit:    equipmentStatNumber(doc, "inventory limit"),
		HPRegenSpeed:      equipmentStatNumber(doc, "HP regen speed", "hp regen speed"),
		MPRegenSpeed:      equipmentStatNumber(doc, "MP regen speed", "mp regen speed"),
		MoveSpeed:         equipmentStatNumber(doc, "move speed"),
		AttackSpeed:       equipmentStatNumber(doc, "attack speed"),
		CastSpeed:         equipmentStatNumber(doc, "cast speed"),
		HitRecovery:       equipmentStatNumber(doc, "hit recovery"),
		JumpPower:         equipmentStatNumber(doc, "jump power"),
	}
}

func equipmentStatNumber(doc *dnfpvf.Document, names ...string) int64 {
	if doc == nil {
		return 0
	}
	for _, name := range names {
		if values := doc.Numbers(name); len(values) > 0 {
			return int64(values[0])
		}
		if texts := doc.Texts(name); len(texts) > 0 {
			value, err := strconv.ParseFloat(strings.TrimSpace(texts[0]), 64)
			if err == nil {
				return int64(value)
			}
		}
	}
	return 0
}
