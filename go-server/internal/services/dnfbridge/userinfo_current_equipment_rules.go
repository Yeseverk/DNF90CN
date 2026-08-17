package dnfbridge

import (
	"strconv"
	"strings"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func (r *csharpLegacyUserInfoReader) currentMode1EquipmentType(equipped dnfrepo.EquipmentEntry) (uint64, bool) {
	return currentEXEActorEquipmentSlot(equipped)
}

func currentEXEActorEquipmentSlot(equipped dnfrepo.EquipmentEntry) (uint64, bool) {
	if value, ok := sceneInventoryExtraUint(equipped.Extra, "current_exe_equipment_type"); ok {
		if !isPVFInitialEquipment(equipped) || sceneInventoryExtraByte(equipped.Extra, "current_exe_runtime_move") != 0 {
			return value, true
		}
	}
	// Current runtime equipment records use the same direct base appearance
	// slots 0..13 as mode0. Only untouched PVF starter rows retain the legacy
	// worn-slot numbering translated below.
	if !isPVFInitialEquipment(equipped) &&
		equipped.SlotIndex >= 0 &&
		equipped.SlotIndex < currentActorMode0AppearanceSlotCount {
		return uint64(equipped.SlotIndex), true
	}
	// Legacy equipped-creature rows predate current_exe_runtime_move but carry
	// the same current slot plus a concrete creature identity. This is enough
	// to preserve slot 26 in a complete op342 group without treating arbitrary
	// 26..29 records as ordinary equipment.
	if equipped.SlotIndex == 26 {
		equipmentSlot, hasEquipmentSlot := sceneInventoryExtraUint(
			equipped.Extra,
			"equipment_slot",
			"equipped_slot",
		)
		creatureKey, hasCreatureKey := sceneInventoryExtraUint(
			equipped.Extra,
			"creature_key",
			"creature_serial_or_handle",
		)
		if hasEquipmentSlot && equipmentSlot == 26 &&
			hasCreatureKey && creatureKey > 0 {
			return 26, true
		}
	}
	return currentEXEActorEquipmentSlotForWornSlot(equipped.SlotIndex)
}

func currentEXEActorEquipmentSlotForWornSlot(slot int16) (uint64, bool) {
	switch {
	case slot == 11:
		return 12, true
	case slot >= 13 && slot <= 22:
		return uint64(slot + 1), true
	case slot == 23:
		return 25, true
	case slot == 32:
		return 32, true
	default:
		return 0, false
	}
}

func currentMode1EquipmentCreateRows(rows []currentMode1EquipmentObjectRow) []currentMode1EquipmentObjectRow {
	if len(rows) == 0 {
		return nil
	}
	createRows := make([]currentMode1EquipmentObjectRow, 0, len(rows))
	for _, row := range rows {
		if row.createEnabled && currentMode1EquipmentCreateRowHasVerifiedState(row) {
			createRows = append(createRows, row)
		}
	}
	return createRows
}

func currentMode1EquipmentDeferredUpdateRows(rows []currentMode1EquipmentObjectRow) []currentMode1EquipmentObjectRow {
	if len(rows) == 0 {
		return nil
	}
	updateRows := make([]currentMode1EquipmentObjectRow, 0, len(rows))
	for _, row := range rows {
		if row.createEnabled && currentMode1EquipmentCreateRowHasVerifiedState(row) {
			continue
		}
		updateRows = append(updateRows, row)
	}
	return updateRows
}

func currentMode1EquipmentCreateRowHasVerifiedState(row currentMode1EquipmentObjectRow) bool {
	if _, ok := currentMode1EquipmentActorSlot(row); !ok || !row.durabilityKnown || row.itemID == 0 {
		return false
	}
	// The current actor slot also selects the item-table conditional readers.
	if _, ok := currentMode1EquipmentRuntimeType(row); !ok {
		return false
	}
	if row.indexedState1C61F40.currentItemMismatch {
		return false
	}
	// Current op25 and sub_21792A0 prove that the plain mode1 detail fields are
	// the same values carried by the current 0x77 item record. This branch is
	// intentionally limited to a detail state that can be reproduced exactly.
	if row.currentItemStateDerived && !row.readsRawBlocks && !row.readsDurabilityOverrideU32 &&
		row.linkedItemID == 0 && row.auxValue == 0 && row.auxByte == 0 && row.bindFlag == 0 && row.marker16 == 0 &&
		len(row.vector1D6E020) == 0 && row.state112 == 0 && len(row.vector225CCA0) == 0 &&
		row.state120 == 0 && row.state160 == 0 && row.state128 == 0 && row.state140 == 0 &&
		row.state144 == 0 && row.resourceByte == 0 && row.state168 == 0 && row.state169 == 0 &&
		row.state170 == 0 && len(row.lateRawC) == 0 && len(row.lateRawD) == 0 &&
		row.state183 == 0 && len(row.lateRawE) == 0 {
		return true
	}
	if (row.readsRawBlocks && (len(row.rawA) > 0 || len(row.rawB) > 0)) ||
		(row.linkedItemID != 0 && row.slot == 9 && len(row.linkedRaw) > 0) ||
		len(row.vector1D6E020) > 0 || len(row.vector225CCA0) > 0 ||
		len(row.lateRawC) > 0 || len(row.lateRawD) > 0 || len(row.lateRawE) > 0 {
		return true
	}
	return row.linkedItemID != 0 || row.auxValue != 0 || row.auxByte != 0 ||
		row.state112 != 0xffffffff ||
		row.state120 != 0 || row.state160 != 0 || row.state128 != 0 ||
		row.state140 != 0 || row.state144 != 0 || row.resourceByte != 0 ||
		row.state168 != 0 || row.state169 != 0 || row.state170 != 0 || row.state183 != 0
}

func currentMode1EquipmentCreateEnabled(equipped dnfrepo.EquipmentEntry) bool {
	extra := equipped.Extra
	for _, key := range []string{"mode1_create_enabled", "mode1_create_row_verified", "current_exe_mode1_create_verified"} {
		raw := strings.TrimSpace(strings.ToLower(extra[key]))
		switch raw {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		}
	}
	// The working Python implementation creates every equipped row in mode1.
	// Go previously defaulted this to untouched legacy starter slots only.
	// After op19 normalized those rows, or for runtime/rental equipment, mode1
	// emitted update rows without first creating actor equipment objects.
	// Preserve explicit overrides and the verified-state gate, but enable every
	// row whose current actor slot is known.
	_, ok := currentEXEActorEquipmentSlot(equipped)
	return ok
}

func currentMode1EquipmentReadsRawBlocks(extra map[string]string, equipmentType uint64, equipmentTypeKnown bool) bool {
	for _, key := range []string{"mode1_reads_raw_blocks", "current_exe_reads_equipment_raw_blocks"} {
		raw := strings.TrimSpace(strings.ToLower(extra[key]))
		switch raw {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		}
	}
	if equipmentTypeKnown {
		return equipmentType <= 0x0b
	}
	return false
}

func currentMode1EquipmentUnknownTypeCount(rows []currentMode1EquipmentObjectRow) int {
	count := 0
	for _, row := range rows {
		if !row.equipmentTypeKnown {
			count++
		}
	}
	return count
}

func currentMode1EquipmentRowSummary(rows []currentMode1EquipmentObjectRow) string {
	if len(rows) == 0 {
		return ""
	}
	parts := make([]string, 0, len(rows))
	for _, row := range rows {
		typePart := "unknown"
		if row.equipmentTypeKnown {
			typePart = strconv.FormatUint(row.equipmentType, 10)
		}
		parts = append(parts, strconv.Itoa(int(row.slot))+":"+strconv.FormatUint(uint64(row.itemID), 10)+":type="+typePart+":durability="+strconv.Itoa(int(row.durability))+":derived="+boolDigit(row.currentItemStateDerived)+":raw="+boolDigit(row.readsRawBlocks)+":create="+boolDigit(row.createEnabled))
	}
	return strings.Join(parts, ";")
}

func boolDigit(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

func currentMode1EquipmentReadsDurabilityOverrideU32(extra map[string]string, equipmentType uint64, equipmentTypeKnown bool) bool {
	for _, key := range []string{"mode1_reads_durability_override_u32", "current_exe_reads_type26_u32"} {
		raw := strings.TrimSpace(strings.ToLower(extra[key]))
		switch raw {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		}
	}
	if equipmentTypeKnown {
		return equipmentType == 26
	}
	return false
}

func currentMode1EquipmentGradeOrInstance(equipped dnfrepo.EquipmentEntry) uint32 {
	if value, ok := sceneInventoryExtraUint(equipped.Extra, "mode1_v74", "mode1_grade_or_instance"); ok {
		return sceneInventoryClampUint32(value)
	}
	if value := currentEquippedCreatureInstance(equipped); value != 0 {
		return value
	}
	if isPVFInitialEquipment(equipped) {
		return 0
	}
	if seed := sceneInventoryExtraUint32(equipped.Extra, "quality_seed"); validCurrentEquipmentQualitySeed(seed) {
		return seed
	}
	if !currentEquipmentEntryHasCurrentRaw77(equipped) {
		return 0
	}
	value := currentItemListEquipmentInstance(equipped)
	if !validCurrentEquipmentQualitySeed(value) {
		return 0
	}
	return value
}
