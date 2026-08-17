package dnfbridge

import (
	"context"
	"fmt"
	"strings"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const (
	currentActorMode0AppearanceSlotCount = 14
	currentActorMode0AppearanceRowBytes  = 19
	currentActorMode0AppearanceEmptyItem = ^uint32(0)
	currentActorMode0WeaponSlot          = 12
	currentActorMode0ReinforceMax        = 0x7f
)

// buildCurrentActorMode0AppearanceSnapshot loads the authoritative worn-item
// record and returns the complete current-EXE base-slot-record snapshot. A
// found record always produces all slots 0..13, including explicit empty rows
// that clear stale client appearance state after an item is removed.
func buildCurrentActorMode0AppearanceSnapshot(
	ctx context.Context,
	repo dnfrepo.EquipmentRepository,
	characterID string,
) ([]byte, bool, error) {
	characterID = strings.TrimSpace(characterID)
	if repo == nil || characterID == "" {
		return nil, false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	record, found, err := repo.Load(ctx, characterID)
	if err != nil || !found {
		return nil, found, err
	}
	body, err := buildCurrentActorMode0AppearanceSnapshotFromEquipment(record)
	if err != nil {
		return nil, true, err
	}
	return body, true, nil
}

func buildCurrentActorMode0AppearanceSnapshotFromEquipment(record dnfrepo.EquipmentRecord) ([]byte, error) {
	rows, err := buildCurrentActorMode0AppearanceSummaryFromEquipment(record)
	if err != nil {
		return nil, err
	}
	var writer packetWriter
	writeCurrentSceneObjectEquipSummary(&writer, rows)
	return writer.bytes(), nil
}

// buildCurrentActorMode0AppearanceSummaryFromEquipment returns the complete
// current-EXE base appearance table. All fourteen rows are present so an empty
// slot explicitly removes stale client state left by an earlier mode0 update.
func buildCurrentActorMode0AppearanceSummaryFromEquipment(record dnfrepo.EquipmentRecord) ([]dnfrepo.CharacterRosterEquipSummary, error) {
	itemIDs := [currentActorMode0AppearanceSlotCount]uint32{}
	packedFlags := [currentActorMode0AppearanceSlotCount]byte{}
	occupied := [currentActorMode0AppearanceSlotCount]bool{}
	for slot := range itemIDs {
		itemIDs[slot] = currentActorMode0AppearanceEmptyItem
	}

	for _, equipped := range record.Entries {
		if equipped.ItemID <= 0 {
			continue
		}
		appearanceSlot, ok := currentActorMode0AppearanceSlot(equipped)
		if !ok || appearanceSlot >= currentActorMode0AppearanceSlotCount {
			continue
		}
		if equipped.ItemID > int64(^uint32(0)) {
			return nil, fmt.Errorf("current actor mode0 appearance item id %d exceeds uint32", equipped.ItemID)
		}
		slot := int(appearanceSlot)
		if occupied[slot] {
			return nil, fmt.Errorf("current actor mode0 appearance slot %d has duplicate equipment", slot)
		}
		occupied[slot] = true
		itemIDs[slot] = uint32(equipped.ItemID)
		if slot == currentActorMode0WeaponSlot {
			packedFlags[slot] = currentActorMode0WeaponPackedFlags(equipped)
		}
	}

	rows := make([]dnfrepo.CharacterRosterEquipSummary, currentActorMode0AppearanceSlotCount)
	for slot, itemID := range itemIDs {
		rows[slot] = dnfrepo.CharacterRosterEquipSummary{
			Slot:         int64(slot),
			ItemIDOrIcon: int64(itemID),
			PackedFlags:  int64(packedFlags[slot]),
		}
	}
	return rows, nil
}

func currentActorMode0WeaponPackedFlags(equipped dnfrepo.EquipmentEntry) byte {
	reinforce := currentItemListEquipmentExtData(equipped)
	if reinforce > currentActorMode0ReinforceMax {
		reinforce = currentActorMode0ReinforceMax
	}
	// sub_20026C0 stores packed_byte>>1 as the per-slot value and bit 0 as
	// a separate flag. Reinforcement occupies the value portion; the unrelated
	// flag remains clear for the authoritative worn-weapon projection.
	return reinforce << 1
}

func currentActorMode0AppearanceSlot(equipped dnfrepo.EquipmentEntry) (uint64, bool) {
	if slot, ok := sceneInventoryExtraUint(
		equipped.Extra,
		"current_exe_equipment_type",
		"current_exe_appearance_slot",
		"appearance_slot",
	); ok {
		return slot, true
	}

	// The same-EXE runtime wire uses direct appearance slots 0..13. Legacy Go
	// PVF creation records instead store worn slots, so only those records use
	// the existing worn-slot conversion (11->12, 13..22->14..23, 23->25).
	if !isPVFInitialEquipment(equipped) && equipped.SlotIndex >= 0 && equipped.SlotIndex < currentActorMode0AppearanceSlotCount {
		return uint64(equipped.SlotIndex), true
	}
	return currentEXEActorEquipmentSlot(equipped)
}
