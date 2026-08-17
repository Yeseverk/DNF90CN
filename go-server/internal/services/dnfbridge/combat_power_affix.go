package dnfbridge

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	dnfcharstat "longheng.io/server/internal/modules/dnf/charstat"
	dnfcombatpower "longheng.io/server/internal/modules/dnf/combatpower"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

// Class0/op413 type 1 is the DLL-private compatibility envelope for the exact
// 70-byte PVF combat snapshot. Seria-luck HUD state is deliberately not sent
// through this private opcode; that display belongs to the native EXE path.
const (
	currentCombatPowerAffixMsgID      = uint16(413)
	currentCombatPowerAffixType       = byte(1)
	currentCombatPowerAffixVersion    = byte(4)
	currentCombatPowerAffixBodyLength = 70
	currentCombatPowerAffixPrivateABI = "90cn_private_combat_power_affixes_v4"
	currentCombatPowerProfessionBytes = 32
)

type currentCombatPowerProjection struct {
	Result         dnfcombatpower.Result
	Job            byte
	GrowType       byte
	Level          byte
	ProfessionName string
	Stats          dnfcharstat.Vector
}

func (s *Service) preloadCombatPowerAffixCatalog(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("dnfbridge service is nil")
	}
	s.combatPowerMu.Lock()
	if s.combatPowerCatalog != nil {
		s.combatPowerMu.Unlock()
		return nil
	}
	if s.combatPowerCatalogErr != nil {
		err := s.combatPowerCatalogErr
		s.combatPowerMu.Unlock()
		return err
	}
	s.combatPowerMu.Unlock()

	s.initialEquipmentMu.Lock()
	archive, err := s.initialEquipmentArchiveLocked()
	s.initialEquipmentMu.Unlock()
	if err != nil {
		return fmt.Errorf("load combat power pvf archive: %w", err)
	}
	catalog, err := dnfcombatpower.Load(ctx, archive)

	s.combatPowerMu.Lock()
	defer s.combatPowerMu.Unlock()
	if err != nil {
		s.combatPowerCatalogErr = fmt.Errorf("load combat power affix catalog: %w", err)
		return s.combatPowerCatalogErr
	}
	s.combatPowerCatalog = catalog
	items, sets := catalog.Snapshot()
	s.logPacketEvent("dnf-combat-power-affix-pvf-catalog-loaded", "items", items, "sets", sets)
	return nil
}

func buildCurrentCombatPowerAffixBody(projection currentCombatPowerProjection) []byte {
	result := projection.Result
	body := make([]byte, currentCombatPowerAffixBodyLength)
	body[0] = currentCombatPowerAffixType
	body[1] = currentCombatPowerAffixVersion
	binary.LittleEndian.PutUint16(body[2:4], combatPowerPercentTenths(result.Affixes.WhiteDamage))
	binary.LittleEndian.PutUint16(body[4:6], combatPowerPercentTenths(result.Affixes.YellowDamage))
	binary.LittleEndian.PutUint16(body[6:8], combatPowerPercentTenths(result.Affixes.CriticalDamage))
	binary.LittleEndian.PutUint16(body[8:10], combatPowerPercentTenths(result.Affixes.YellowAdditional))
	binary.LittleEndian.PutUint16(body[10:12], combatPowerPercentTenths(result.Affixes.CriticalAdditional))
	binary.LittleEndian.PutUint16(body[12:14], combatPowerPercentTenths(result.Affixes.AllAttack))
	binary.LittleEndian.PutUint16(body[14:16], clampUint16(result.EquippedItems))
	binary.LittleEndian.PutUint16(body[16:18], clampUint16(len(result.ActiveSets)))
	body[18] = projection.Job
	body[19] = projection.GrowType
	body[20] = projection.Level
	profession := boundedCombatPowerProfession(projection.ProfessionName)
	body[21] = byte(len(profession))
	binary.LittleEndian.PutUint32(body[22:26], clampUint32(projection.Stats.PhysicalAttack))
	binary.LittleEndian.PutUint32(body[26:30], clampUint32(projection.Stats.MagicalAttack))
	binary.LittleEndian.PutUint32(body[30:34], clampUint32(projection.Stats.IndependentAttack))
	copy(body[34:34+currentCombatPowerProfessionBytes], profession)
	binary.LittleEndian.PutUint32(body[66:70], clampUint32(int64(result.PVFEquipmentScore)))
	return body
}

func boundedCombatPowerProfession(value string) []byte {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	const maximum = currentCombatPowerProfessionBytes - 1
	out := make([]byte, 0, maximum)
	for _, character := range value {
		encoded := []byte(string(character))
		if len(out)+len(encoded) > maximum {
			break
		}
		out = append(out, encoded...)
	}
	return out
}

func combatPowerPercentTenths(value float64) uint16 {
	if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	value = math.Round(value * 10)
	if value >= math.MaxUint16 {
		return math.MaxUint16
	}
	return uint16(value)
}

func clampUint16(value int) uint16 {
	if value <= 0 {
		return 0
	}
	if value >= math.MaxUint16 {
		return math.MaxUint16
	}
	return uint16(value)
}

func clampUint32(value int64) uint32 {
	if value <= 0 {
		return 0
	}
	if value >= math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(value)
}

func (s *Service) sendSelectedCurrentCombatPowerAffixes(session *gameSession, source string) error {
	_, err := s.sendSelectedCurrentCombatPowerAffixProjection(session, source)
	return err
}

func (s *Service) sendSelectedCurrentCombatPowerAffixProjection(session *gameSession, source string) (bool, error) {
	if s == nil || session == nil || session.selectedCharacterID == 0 {
		return false, nil
	}
	s.combatPowerMu.Lock()
	catalog := s.combatPowerCatalog
	catalogErr := s.combatPowerCatalogErr
	s.combatPowerMu.Unlock()
	if catalog == nil {
		s.logGameEvent(session, "game-combat-power-affix-private-projection-deferred",
			"source", source,
			"char_id", session.selectedCharacterID,
			"private_abi", currentCombatPowerAffixPrivateABI,
			"reason", "pvf_catalog_unavailable",
			"error", catalogErr)
		return false, nil
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Character == nil || repositories.Equipment == nil {
		s.logGameEvent(session, "game-combat-power-affix-private-projection-deferred",
			"source", source,
			"char_id", session.selectedCharacterID,
			"private_abi", currentCombatPowerAffixPrivateABI,
			"reason", "character_or_equipment_repository_unavailable")
		return false, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	characterID := strconv.Itoa(int(session.selectedCharacterID))
	character, characterFound, err := repositories.Character.Load(ctx, characterID)
	if err != nil || !characterFound {
		s.logGameEvent(session, "game-combat-power-affix-private-projection-deferred",
			"source", source,
			"char_id", session.selectedCharacterID,
			"private_abi", currentCombatPowerAffixPrivateABI,
			"reason", "character_load_failed",
			"found", characterFound,
			"error", err)
		return false, nil
	}
	jobValue, err := strconv.Atoi(strings.TrimSpace(character.Job))
	if err != nil || jobValue < 0 || jobValue > math.MaxUint8 {
		s.logGameEvent(session, "game-combat-power-affix-private-projection-deferred",
			"source", source,
			"char_id", session.selectedCharacterID,
			"private_abi", currentCombatPowerAffixPrivateABI,
			"reason", "character_job_invalid",
			"job", character.Job,
			"error", err)
		return false, nil
	}
	growType := byte(numericCharacterStatValue(character, "grow_type"))
	profiles, _, err := s.currentProfessionResources(ctx)
	if err != nil {
		s.logGameEvent(session, "game-combat-power-affix-private-projection-deferred",
			"source", source,
			"char_id", session.selectedCharacterID,
			"private_abi", currentCombatPowerAffixPrivateABI,
			"reason", "profession_profile_unavailable",
			"error", err)
		return false, nil
	}
	professionName, professionFound := profiles.DisplayName(byte(jobValue), growType)
	if !professionFound {
		s.logGameEvent(session, "game-combat-power-affix-private-projection-deferred",
			"source", source,
			"char_id", session.selectedCharacterID,
			"private_abi", currentCombatPowerAffixPrivateABI,
			"reason", "profession_name_missing",
			"job", jobValue,
			"grow_type", growType)
		return false, nil
	}
	stats, statsFound := s.characterPVFStatsForUserInfo(ctx, session, character, true)
	if !statsFound {
		s.logGameEvent(session, "game-combat-power-affix-private-projection-deferred",
			"source", source,
			"char_id", session.selectedCharacterID,
			"private_abi", currentCombatPowerAffixPrivateABI,
			"reason", "character_three_attack_stats_unavailable")
		return false, nil
	}
	equipment, equipmentFound, err := repositories.Equipment.Load(ctx, characterID)
	if err != nil {
		s.logGameEvent(session, "game-combat-power-affix-private-projection-deferred",
			"source", source,
			"char_id", session.selectedCharacterID,
			"private_abi", currentCombatPowerAffixPrivateABI,
			"reason", "equipment_load_failed",
			"error", err)
		return false, nil
	}
	if repositories.Pet == nil {
		s.logGameEvent(session, "game-combat-power-affix-private-projection-deferred",
			"source", source,
			"char_id", session.selectedCharacterID,
			"private_abi", currentCombatPowerAffixPrivateABI,
			"reason", "pet_repository_unavailable")
		return false, nil
	}
	petRecord, petFound, err := repositories.Pet.Load(ctx, characterID)
	if err != nil {
		s.logGameEvent(session, "game-combat-power-affix-private-projection-deferred",
			"source", source,
			"char_id", session.selectedCharacterID,
			"private_abi", currentCombatPowerAffixPrivateABI,
			"reason", "pet_load_failed",
			"error", err)
		return false, nil
	}

	itemIDs := combatPowerEquippedItemIDs(
		equipment, equipmentFound, petRecord, petFound, time.Now())
	result, err := catalog.Aggregate(ctx, itemIDs)
	if err != nil {
		s.logGameEvent(session, "game-combat-power-affix-private-projection-deferred",
			"source", source,
			"char_id", session.selectedCharacterID,
			"private_abi", currentCombatPowerAffixPrivateABI,
			"reason", "pvf_affix_aggregate_failed",
			"error", err)
		return false, nil
	}
	missingAttackStats := 0
	for _, itemID := range itemIDs {
		itemStats, found := s.equipmentPVFStat(ctx, itemID, nil)
		if !found {
			missingAttackStats++
			continue
		}
		stats.Add(itemStats)
	}
	level := character.Level
	if level <= 0 {
		level = 1
	}
	if level > math.MaxUint8 {
		level = math.MaxUint8
	}
	body := buildCurrentCombatPowerAffixBody(currentCombatPowerProjection{
		Result:         result,
		Job:            byte(jobValue),
		GrowType:       growType,
		Level:          byte(level),
		ProfessionName: professionName,
		Stats:          stats,
	})
	s.logGameEvent(session, "game-combat-power-affix-private-projection-send",
		"source", source,
		"char_id", session.selectedCharacterID,
		"msg_id", currentCombatPowerAffixMsgID,
		"classification", 0,
		"body_len", len(body),
		"white_tenths", binary.LittleEndian.Uint16(body[2:4]),
		"yellow_tenths", binary.LittleEndian.Uint16(body[4:6]),
		"critical_tenths", binary.LittleEndian.Uint16(body[6:8]),
		"yellow_additional_tenths", binary.LittleEndian.Uint16(body[8:10]),
		"critical_additional_tenths", binary.LittleEndian.Uint16(body[10:12]),
		"all_attack_tenths", binary.LittleEndian.Uint16(body[12:14]),
		"equipped_items", result.EquippedItems,
		"active_sets", len(result.ActiveSets),
		"job", jobValue,
		"grow_type", growType,
		"level", level,
		"profession", professionName,
		"physical_attack", binary.LittleEndian.Uint32(body[22:26]),
		"magical_attack", binary.LittleEndian.Uint32(body[26:30]),
		"independent_attack", binary.LittleEndian.Uint32(body[30:34]),
		"pvf_equipment_score", binary.LittleEndian.Uint32(body[66:70]),
		"pvf_scored_items", result.ScoredItems,
		"pvf_level90_epic_items", result.Level90EpicItems,
		"missing_item_attack_stats", missingAttackStats,
		"private_abi", currentCombatPowerAffixPrivateABI,
		"wire_owner", "90CN.dll",
		"native_exe_protocol", false)
	if err := s.sendGameUpperRawClass(session, currentCombatPowerAffixMsgID, body, 0); err != nil {
		return false, err
	}
	return true, nil
}

func combatPowerEquippedItemIDs(
	equipment dnfrepo.EquipmentRecord,
	equipmentFound bool,
	petRecord dnfrepo.PetRecord,
	petFound bool,
	now time.Time,
) []int64 {
	itemIDs := make([]int64, 0, len(equipment.Entries)+4)
	occupied := make(map[int16]bool, 33)
	for _, entry := range equipment.Entries {
		if entry.ItemID <= 0 || !combatPowerActorSlot(entry.SlotIndex) ||
			(!entry.ExpireAt.IsZero() && !entry.ExpireAt.After(now)) {
			continue
		}
		occupied[entry.SlotIndex] = true
		itemIDs = append(itemIDs, entry.ItemID)
	}

	// A present equipment record is authoritative for creature slot 26. Only
	// imported legacy characters with no equipment record may fall back to the
	// durable pet owner's EquippedKey.
	if petFound && !equipmentFound && !occupied[26] {
		if pet, ok := petRecord.Entries[petRecord.EquippedKey]; ok && pet.ItemID > 0 {
			occupied[26] = true
			itemIDs = append(itemIDs, pet.ItemID)
		}
	}

	// Pet artifacts are owned by PetRecord under semantic color keys instead
	// of historical equipment numbers. Project them into the current actor's
	// three dedicated combat-power slots without double-counting imported rows.
	for index, kind := range []string{"red", "blue", "green"} {
		slot := int16(27 + index)
		if !petFound || occupied[slot] {
			continue
		}
		artifact, ok := petRecord.Artifacts[kind]
		if !ok || artifact.ItemID <= 0 || artifact.Count <= 0 ||
			(!artifact.ExpireAt.IsZero() && !artifact.ExpireAt.After(now)) {
			continue
		}
		occupied[slot] = true
		itemIDs = append(itemIDs, artifact.ItemID)
	}
	sort.Slice(itemIDs, func(i, j int) bool { return itemIDs[i] < itemIDs[j] })
	return itemIDs
}

func combatPowerActorSlot(slot int16) bool {
	// The current EXE owns one 33-slot actor equipment map. Every proved worn
	// slot participates in the PVF affix scan: ordinary equipment, avatars,
	// aura/skin/name-tag/weapon-avatar, creature, three pet artifacts, and
	// guild medal. A slot without a relevant PVF damage entry contributes zero.
	return slot >= 0 && slot <= 32
}
