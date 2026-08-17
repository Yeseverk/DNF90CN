// 本文件覆盖装备 owner 的修理、穿戴、卸下和宠物装备 raw 构造。
package equip

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"testing"
	"time"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestOwnerRepairPatchesEquippedRawDurability(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	raw := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 12, 0, 13, 14}
	saveTestCharacter(t, ctx, repos, dnfrepo.CharacterRecord{CharacterID: "77", Stats: map[string]int64{"gold": 9999}})
	saveTestEquipment(t, ctx, repos, dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"11": {SlotIndex: 11, ItemID: 700, RawEntry: raw, Extra: map[string]string{"max_durability": "20", "repair_gold": "0"}},
		},
	})

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	result, err := owner.Repair(ctx, RepairCommand{SelectedCharacterID: 77, SlotIndex: 11}, repairTestResolver(20, 0, 20))
	if err != nil {
		t.Fatalf("Repair error = %v", err)
	}
	if !result.Changed || result.OldDurability != 12 || result.NewDurability != 20 || result.ItemID != 700 || result.UpdatedGold != 9999 {
		t.Fatalf("result = %+v", result)
	}

	loaded := loadTestEquipment(t, ctx, repos, "77")
	entry := loaded.Entries["11"]
	if got := uint16(entry.RawEntry[10]) | uint16(entry.RawEntry[11])<<8; got != 20 {
		t.Fatalf("raw durability = %d, want 20", got)
	}
	if raw[10] != 12 {
		t.Fatalf("caller raw was mutated: %v", raw)
	}
}

func repairTestResolver(maxDurability int64, repairPrice int64, grade int64) alignedcmd.RepairCostResolver {
	return func(itemID int64) (alignedcmd.RepairCostEvidence, error) {
		return alignedcmd.RepairCostEvidence{
			EquipmentType:   "[weapon]",
			MaxDurability:   maxDurability,
			RepairPrice:     repairPrice,
			Grade:           grade,
			RepairCostRate:  0.08,
			QuickRepairRate: 1.5,
			UpgradeRates:    []float64{1, 1, 1},
		}, nil
	}
}

func TestOwnerRepairDeductsFormulaCostAndGoldAtomically(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	raw := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 12, 0, 13, 14}
	saveTestCharacter(t, ctx, repos, dnfrepo.CharacterRecord{CharacterID: "77", Stats: map[string]int64{"gold": 1000}})
	saveTestEquipment(t, ctx, repos, dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"11": {SlotIndex: 11, ItemID: 700, RawEntry: raw},
		},
	})

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	// 86JP formula: 6400*(20+5)/10=16000; 16000*0.08/20*8 = 512.
	result, err := owner.Repair(ctx, RepairCommand{SelectedCharacterID: 77, SlotIndex: 11}, repairTestResolver(20, 6400, 20))
	if err != nil {
		t.Fatalf("Repair error = %v", err)
	}
	if !result.Changed || result.Cost != 512 || result.UpdatedGold != 488 || result.FreeRepair {
		t.Fatalf("result = %+v, want cost=512 gold=488", result)
	}
	character, _, _ := repos.Character.Load(ctx, "77")
	if character.Stats["gold"] != 488 {
		t.Fatalf("persisted gold = %d, want 488", character.Stats["gold"])
	}
	loaded := loadTestEquipment(t, ctx, repos, "77")
	if got := uint16(loaded.Entries["11"].RawEntry[10]) | uint16(loaded.Entries["11"].RawEntry[11])<<8; got != 20 {
		t.Fatalf("raw durability = %d, want 20", got)
	}
}

func TestOwnerRepairQuickRepairPaysQuickRate(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	raw := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 12, 0, 13, 14}
	saveTestCharacter(t, ctx, repos, dnfrepo.CharacterRecord{CharacterID: "77", Stats: map[string]int64{"gold": 1000}})
	saveTestEquipment(t, ctx, repos, dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"11": {SlotIndex: 11, ItemID: 700, RawEntry: raw},
		},
	})

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	// 512 * 1.5 = 768.
	result, err := owner.Repair(ctx, RepairCommand{SelectedCharacterID: 77, SlotIndex: 11, QuickRepair: true}, repairTestResolver(20, 6400, 20))
	if err != nil {
		t.Fatalf("Repair error = %v", err)
	}
	if result.Cost != 768 || result.UpdatedGold != 232 {
		t.Fatalf("result = %+v, want cost=768 gold=232", result)
	}
}

func TestOwnerRepairInsufficientGoldRollsBack(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	raw := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 12, 0, 13, 14}
	saveTestCharacter(t, ctx, repos, dnfrepo.CharacterRecord{CharacterID: "77", Stats: map[string]int64{"gold": 100}})
	saveTestEquipment(t, ctx, repos, dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"11": {SlotIndex: 11, ItemID: 700, RawEntry: raw},
		},
	})

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	if _, err := owner.Repair(ctx, RepairCommand{SelectedCharacterID: 77, SlotIndex: 11}, repairTestResolver(20, 6400, 20)); !errors.Is(err, ErrRepairGoldInsufficient) {
		t.Fatalf("Repair error = %v, want ErrRepairGoldInsufficient", err)
	}
	loaded := loadTestEquipment(t, ctx, repos, "77")
	if got := uint16(loaded.Entries["11"].RawEntry[10]) | uint16(loaded.Entries["11"].RawEntry[11])<<8; got != 12 {
		t.Fatalf("raw durability = %d, want unchanged 12", got)
	}
	character, _, _ := repos.Character.Load(ctx, "77")
	if character.Stats["gold"] != 100 {
		t.Fatalf("gold mutated = %d, want 100", character.Stats["gold"])
	}
}

func TestOwnerRepairRejectsShortRawWithoutMutation(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestCharacter(t, ctx, repos, dnfrepo.CharacterRecord{CharacterID: "77", Stats: map[string]int64{"gold": 9999}})
	saveTestEquipment(t, ctx, repos, dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"11": {SlotIndex: 11, ItemID: 700, RawEntry: []byte{1, 2, 3}, Extra: map[string]string{"max_durability": "20", "repair_gold": "0"}},
		},
	})

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	_, err = owner.Repair(ctx, RepairCommand{SelectedCharacterID: 77, SlotIndex: 11}, repairTestResolver(20, 0, 20))
	if !errors.Is(err, ErrRawEntryTooShort) {
		t.Fatalf("Repair error = %v, want ErrRawEntryTooShort", err)
	}
}

func TestOwnerRepairRejectsNotRepairableItem(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestCharacter(t, ctx, repos, dnfrepo.CharacterRecord{CharacterID: "77", Stats: map[string]int64{"gold": 9999}})
	saveTestEquipment(t, ctx, repos, dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"11": {SlotIndex: 11, ItemID: 700, RawEntry: []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 12, 0}},
		},
	})

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	_, err = owner.Repair(ctx, RepairCommand{SelectedCharacterID: 77, SlotIndex: 11}, repairTestResolver(-1, 0, 0))
	if !errors.Is(err, ErrRepairNotRepairable) {
		t.Fatalf("Repair error = %v, want ErrRepairNotRepairable", err)
	}
}

func TestOwnerRepairNilResolverFailsClosed(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestCharacter(t, ctx, repos, dnfrepo.CharacterRecord{CharacterID: "77", Stats: map[string]int64{"gold": 9999}})
	saveTestEquipment(t, ctx, repos, dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"11": {SlotIndex: 11, ItemID: 700, RawEntry: []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 12, 0}},
		},
	})

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	_, err = owner.Repair(ctx, RepairCommand{SelectedCharacterID: 77, SlotIndex: 11}, nil)
	if !errors.Is(err, ErrRepairCostMissing) {
		t.Fatalf("Repair error = %v, want ErrRepairCostMissing", err)
	}
}

func TestOwnerRepairFullDurabilityIsNoOp(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestCharacter(t, ctx, repos, dnfrepo.CharacterRecord{CharacterID: "77", Stats: map[string]int64{"gold": 4321}})
	saveTestEquipment(t, ctx, repos, dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"11": {SlotIndex: 11, ItemID: 700, RawEntry: []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 20, 0}},
		},
	})

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	result, err := owner.Repair(ctx, RepairCommand{SelectedCharacterID: 77, SlotIndex: 11}, repairTestResolver(20, 6400, 20))
	if err != nil {
		t.Fatalf("Repair error = %v", err)
	}
	if result.Changed || result.Cost != 0 || result.UpdatedGold != 4321 || result.NewDurability != 20 {
		t.Fatalf("result = %+v, want unchanged no-cost", result)
	}
	character, _, _ := repos.Character.Load(ctx, "77")
	if character.Stats["gold"] != 4321 {
		t.Fatalf("gold mutated = %d", character.Stats["gold"])
	}
}

func TestOwnerMoveEquipsInventoryStackToEmptySlot(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	raw := "000102030405060708090c000d0e"
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"0:5": {ItemID: 700, Count: 1, Extra: map[string]string{"raw_entry_hex": raw}},
		},
	})
	saveTestEquipment(t, ctx, repos, dnfrepo.EquipmentRecord{CharacterID: "77", Entries: map[string]dnfrepo.EquipmentEntry{}})

	owner, err := newValidatedTestOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	result, err := owner.Move(ctx, MoveCommand{
		SelectedCharacterID:  77,
		SourceListType:       wireListMain,
		SourceSlotIndex:      5,
		MoveCount:            1,
		DestinationListType:  wireListEquipment,
		DestinationSlotIndex: 11,
	})
	if err != nil {
		t.Fatalf("Move error = %v", err)
	}
	if !result.Changed || result.Mode != "equip" || result.ItemID != 700 {
		t.Fatalf("result = %+v", result)
	}

	inventory := loadTestInventory(t, ctx, repos, "77")
	if _, ok := inventory.Slots["0:5"]; ok {
		t.Fatalf("inventory source slot should be empty: %+v", inventory.Slots)
	}
	equipment := loadTestEquipment(t, ctx, repos, "77")
	entry := equipment.Entries["11"]
	if entry.ItemID != 700 || entry.SlotIndex != 11 || len(entry.RawEntry) != 14 {
		t.Fatalf("equipment entry = %+v", entry)
	}
}

func TestOwnerMoveRequiresPlacementValidatorBeforeMutation(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"0:5": {ItemID: 700, Count: 1, RawEntry: []byte{1, 2, 3}},
		},
	})
	saveTestEquipment(t, ctx, repos, dnfrepo.EquipmentRecord{CharacterID: "77", Entries: map[string]dnfrepo.EquipmentEntry{}})

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	_, err = owner.Move(ctx, MoveCommand{
		SelectedCharacterID:  77,
		SourceListType:       wireListMain,
		SourceSlotIndex:      5,
		MoveCount:            1,
		DestinationListType:  wireListEquipment,
		DestinationSlotIndex: 11,
	})
	if !errors.Is(err, ErrMoveValidatorRequired) {
		t.Fatalf("Move error = %v, want ErrMoveValidatorRequired", err)
	}
	if stack := loadTestInventory(t, ctx, repos, "77").Slots["0:5"]; stack.ItemID != 700 || stack.Count != 1 {
		t.Fatalf("inventory mutated: %+v", stack)
	}
	if entries := loadTestEquipment(t, ctx, repos, "77").Entries; len(entries) != 0 {
		t.Fatalf("equipment mutated: %+v", entries)
	}
}

func TestOwnerMoveRejectsOutOfRangeEquipmentSlotBeforeValidation(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"0:5": {ItemID: 700, Count: 1, RawEntry: []byte{1, 2, 3}},
		},
	})
	saveTestEquipment(t, ctx, repos, dnfrepo.EquipmentRecord{CharacterID: "77", Entries: map[string]dnfrepo.EquipmentEntry{}})
	validator := &recordingPlacementValidator{}
	owner, err := NewOwnerWithPlacementValidator(repos, validator)
	if err != nil {
		t.Fatalf("NewOwnerWithPlacementValidator error = %v", err)
	}

	_, err = owner.Move(ctx, MoveCommand{
		SelectedCharacterID:  77,
		SourceListType:       wireListMain,
		SourceSlotIndex:      5,
		MoveCount:            1,
		DestinationListType:  wireListEquipment,
		DestinationSlotIndex: currentEquipmentSlotCount,
	})
	if !errors.Is(err, ErrMoveSlotOutOfRange) {
		t.Fatalf("Move error = %v, want ErrMoveSlotOutOfRange", err)
	}
	if len(validator.placements) != 0 {
		t.Fatalf("validator called for structurally invalid slot: %+v", validator.placements)
	}
	if stack := loadTestInventory(t, ctx, repos, "77").Slots["0:5"]; stack.ItemID != 700 || stack.Count != 1 {
		t.Fatalf("inventory mutated: %+v", stack)
	}
}

func TestOwnerMoveValidatorRejectionDoesNotMutate(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"0:5": {ItemID: 700, Count: 1, RawEntry: []byte{1, 2, 3}},
		},
	})
	saveTestEquipment(t, ctx, repos, dnfrepo.EquipmentRecord{CharacterID: "77", Entries: map[string]dnfrepo.EquipmentEntry{}})
	rejected := errors.New("placement rejected")
	validator := &recordingPlacementValidator{err: rejected}
	owner, err := NewOwnerWithPlacementValidator(repos, validator)
	if err != nil {
		t.Fatalf("NewOwnerWithPlacementValidator error = %v", err)
	}

	_, err = owner.Move(ctx, MoveCommand{
		SelectedCharacterID:  77,
		SourceListType:       wireListMain,
		SourceSlotIndex:      5,
		MoveCount:            1,
		DestinationListType:  wireListEquipment,
		DestinationSlotIndex: 11,
	})
	if !errors.Is(err, rejected) {
		t.Fatalf("Move error = %v, want validator rejection", err)
	}
	if len(validator.placements) != 1 || validator.placements[0].ItemID != 700 || validator.placements[0].TargetSlotIndex != 11 {
		t.Fatalf("placements = %+v", validator.placements)
	}
	if stack := loadTestInventory(t, ctx, repos, "77").Slots["0:5"]; stack.ItemID != 700 || stack.Count != 1 {
		t.Fatalf("inventory mutated: %+v", stack)
	}
	if entries := loadTestEquipment(t, ctx, repos, "77").Entries; len(entries) != 0 {
		t.Fatalf("equipment mutated: %+v", entries)
	}
}

func TestOwnerMoveValidatesEveryEquipmentSwapTarget(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestEquipment(t, ctx, repos, dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"11": {SlotIndex: 11, ItemID: 700, RawEntry: []byte{1}},
			"13": {SlotIndex: 13, ItemID: 800, RawEntry: []byte{2}},
		},
	})
	validator := &recordingPlacementValidator{}
	owner, err := NewOwnerWithPlacementValidator(repos, validator)
	if err != nil {
		t.Fatalf("NewOwnerWithPlacementValidator error = %v", err)
	}

	result, err := owner.Move(ctx, MoveCommand{
		SelectedCharacterID:  77,
		SourceListType:       wireListEquipment,
		SourceSlotIndex:      11,
		MoveCount:            1,
		DestinationListType:  wireListEquipment,
		DestinationSlotIndex: 13,
	})
	if err != nil {
		t.Fatalf("Move error = %v", err)
	}
	if !result.Changed || len(validator.placements) != 2 {
		t.Fatalf("result=%+v placements=%+v", result, validator.placements)
	}
	if first, second := validator.placements[0], validator.placements[1]; first.ItemID != 700 || first.TargetSlotIndex != 13 || second.ItemID != 800 || second.TargetSlotIndex != 11 {
		t.Fatalf("placements = %+v", validator.placements)
	}
	entries := loadTestEquipment(t, ctx, repos, "77").Entries
	if entries["11"].ItemID != 800 || entries["13"].ItemID != 700 {
		t.Fatalf("equipment swap = %+v", entries)
	}
}

func TestOwnerMoveEquipsTypedRawEntryWithoutLegacyHex(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	raw := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 12, 0, 13, 14}
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"0:5": {ItemID: 700, Count: 1, RawEntry: raw},
		},
	})
	saveTestEquipment(t, ctx, repos, dnfrepo.EquipmentRecord{CharacterID: "77", Entries: map[string]dnfrepo.EquipmentEntry{}})

	owner, err := newValidatedTestOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	if _, err := owner.Move(ctx, MoveCommand{
		SelectedCharacterID:  77,
		SourceListType:       wireListMain,
		SourceSlotIndex:      5,
		MoveCount:            1,
		DestinationListType:  wireListEquipment,
		DestinationSlotIndex: 11,
	}); err != nil {
		t.Fatalf("Move error = %v", err)
	}

	entry := loadTestEquipment(t, ctx, repos, "77").Entries["11"]
	if !bytes.Equal(entry.RawEntry, raw) {
		t.Fatalf("equipment raw = % X, want % X", entry.RawEntry, raw)
	}
	raw[0] = 0xFF
	if entry.RawEntry[0] == 0xFF {
		t.Fatal("equipment raw aliases caller memory")
	}
}

func TestOwnerMoveEquipsTitleBookRewardWithoutDuplicatingInventoryItem(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"0:12": {
				ItemID: 26691,
				Count:  1,
				Extra: map[string]string{
					"source":              "title_book_get",
					"title_book_category": "1",
					"title_book_index":    "73",
				},
			},
		},
	})
	saveTestEquipment(t, ctx, repos, dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries:     map[string]dnfrepo.EquipmentEntry{},
	})
	owner, err := newValidatedTestOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	result, err := owner.Move(ctx, MoveCommand{
		SelectedCharacterID:  77,
		SourceListType:       wireListMain,
		SourceSlotIndex:      12,
		MoveCount:            1,
		DestinationListType:  wireListEquipment,
		DestinationSlotIndex: 13,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.Mode != "equip" || result.ItemID != 26691 {
		t.Fatalf("move result = %+v", result)
	}
	if _, found := loadTestInventory(t, ctx, repos, "77").Slots["0:12"]; found {
		t.Fatal("title remained in inventory after successful equip")
	}
	entry, found := loadTestEquipment(t, ctx, repos, "77").Entries["13"]
	if !found || entry.ItemID != 26691 {
		t.Fatalf("equipped title = %+v found=%t", entry, found)
	}
	if len(entry.RawEntry) != 43 ||
		entry.RawEntry[0] != 13 ||
		binary.LittleEndian.Uint32(entry.RawEntry[1:5]) != 26691 ||
		binary.LittleEndian.Uint32(entry.RawEntry[5:9]) != 1 {
		t.Fatalf("equipped title raw = %x", entry.RawEntry)
	}
}

func TestOwnerMoveStillRejectsUnprovenMissingRawEntry(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"0:12": {ItemID: 26691, Count: 1},
		},
	})
	saveTestEquipment(t, ctx, repos, dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries:     map[string]dnfrepo.EquipmentEntry{},
	})
	owner, err := newValidatedTestOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	_, err = owner.Move(ctx, MoveCommand{
		SelectedCharacterID:  77,
		SourceListType:       wireListMain,
		SourceSlotIndex:      12,
		MoveCount:            1,
		DestinationListType:  wireListEquipment,
		DestinationSlotIndex: 13,
	})
	if !errors.Is(err, ErrMoveRawEntryMissing) {
		t.Fatalf("missing unproven raw error = %v, want %v", err, ErrMoveRawEntryMissing)
	}
}

func TestOwnerMoveMaterializesRawFromCurrentEquipmentMoveEvidence(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"0:9": {
				ItemID: 100180108,
				Count:  1,
				Extra: map[string]string{
					"item_kind":    "equipment",
					"pvf_path":     "equipment/character/common/shoulder/harmor/100180108.equ",
					"quality_seed": "999999998",
					"durability":   "32",
					"seal_flag":    "1",
				},
			},
		},
	})
	saveTestEquipment(t, ctx, repos, dnfrepo.EquipmentRecord{CharacterID: "77", Entries: map[string]dnfrepo.EquipmentEntry{}})
	owner, err := newValidatedTestOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	result, err := owner.Move(ctx, MoveCommand{
		SelectedCharacterID:      77,
		SourceListType:           wireListMain,
		SourceSlotIndex:          9,
		SourceInstanceValue:      0x05F8A08C,
		MoveCount:                1,
		DestinationListType:      wireListEquipment,
		DestinationSlotIndex:     15,
		DestinationInstanceValue: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.Mode != "equip" || result.ItemID != 100180108 {
		t.Fatalf("result=%+v", result)
	}
	entry, found := loadTestEquipment(t, ctx, repos, "77").Entries["15"]
	if !found || len(entry.RawEntry) != currentEquipmentRawEntryWireSize {
		t.Fatalf("entry=%+v found=%t", entry, found)
	}
	if got := binary.LittleEndian.Uint16(entry.RawEntry[0:2]); got != 15 {
		t.Fatalf("raw slot=%d", got)
	}
	if got := binary.LittleEndian.Uint32(entry.RawEntry[2:6]); got != 100180108 {
		t.Fatalf("raw item=%d", got)
	}
	if got := binary.LittleEndian.Uint32(entry.RawEntry[6:10]); got != 999999998 {
		t.Fatalf("raw quality=%d", got)
	}
	if got := binary.LittleEndian.Uint16(entry.RawEntry[0x0B:0x0D]); got != 32 {
		t.Fatalf("raw durability=%d", got)
	}
	if entry.RawEntry[0x0D] != 0 || entry.Extra["seal_removed_by_first_equip"] != "1" {
		t.Fatalf("first-equip seal was not consumed: raw=%x extra=%+v", entry.RawEntry[0x0D], entry.Extra)
	}
	if got := binary.LittleEndian.Uint32(entry.RawEntry[0x0E:0x12]); got != 0x05F8A08C {
		t.Fatalf("raw source identity=%#x", got)
	}
}

func TestOwnerMoveEquipsStackWithTailDataAliasPreservesRaw(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	raw := make([]byte, 0x77)
	tail := make([]byte, equipmentTailDataBytes)
	tail[0] = 2
	binary.LittleEndian.PutUint32(tail[1:5], 0xFFFFFFFF)
	binary.LittleEndian.PutUint32(tail[5:9], 0xFFFFFFFF)
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"0:5": {ItemID: 700, Count: 1, RawEntry: raw, Extra: map[string]string{"tailData2F": hex.EncodeToString(tail)}},
		},
	})
	saveTestEquipment(t, ctx, repos, dnfrepo.EquipmentRecord{CharacterID: "77", Entries: map[string]dnfrepo.EquipmentEntry{}})

	owner, err := newValidatedTestOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	if _, err := owner.Move(ctx, MoveCommand{
		SelectedCharacterID:  77,
		SourceListType:       wireListMain,
		SourceSlotIndex:      5,
		MoveCount:            1,
		DestinationListType:  wireListEquipment,
		DestinationSlotIndex: 11,
	}); err != nil {
		t.Fatalf("Move error = %v", err)
	}

	entry := loadTestEquipment(t, ctx, repos, "77").Entries["11"]
	if !bytes.Equal(entry.RawEntry, raw) {
		t.Fatalf("equipment raw = %x, want preserved %x", entry.RawEntry, raw)
	}
}

func TestOwnerMoveEquipsAndSwapsPreviousEntryBack(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"0:5": {ItemID: 700, Count: 1, Extra: map[string]string{"raw_entry_hex": "000102030405060708090c000d0e"}},
		},
	})
	saveTestEquipment(t, ctx, repos, dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"11": {SlotIndex: 11, ItemID: 800, RawEntry: []byte{9, 8, 7, 6}, Extra: map[string]string{"source": "old"}},
		},
	})

	owner, err := newValidatedTestOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	result, err := owner.Move(ctx, MoveCommand{
		SelectedCharacterID:  77,
		SourceListType:       wireListMain,
		SourceSlotIndex:      5,
		MoveCount:            1,
		DestinationListType:  wireListEquipment,
		DestinationSlotIndex: 11,
	})
	if err != nil {
		t.Fatalf("Move error = %v", err)
	}
	if result.Mode != "equip_swap" || result.SwappedItemID != 800 {
		t.Fatalf("result = %+v", result)
	}

	inventory := loadTestInventory(t, ctx, repos, "77")
	if got := inventory.Slots["0:5"]; got.ItemID != 800 || got.Extra["raw_entry_hex"] != "09080706" {
		t.Fatalf("swapped inventory stack = %+v", got)
	}
	equipment := loadTestEquipment(t, ctx, repos, "77")
	if got := equipment.Entries["11"].ItemID; got != 700 {
		t.Fatalf("equipped item = %d, want 700", got)
	}
}

func TestOwnerMoveFirstEquipPermanentlyRemovesExplicitSeal(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	raw := make([]byte, 0x77)
	raw[0x0D] = 1
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"0:10": {
				ItemID:   400330119,
				Count:    1,
				RawEntry: raw,
				Extra:    map[string]string{"seal_flag": "1", "source": "booster_item"},
			},
		},
	})
	saveTestEquipment(t, ctx, repos, dnfrepo.EquipmentRecord{CharacterID: "77", Entries: map[string]dnfrepo.EquipmentEntry{}})

	owner, err := newValidatedTestOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Move(ctx, MoveCommand{
		SelectedCharacterID: 77, SourceListType: wireListMain, SourceSlotIndex: 10, MoveCount: 1,
		DestinationListType: wireListEquipment, DestinationSlotIndex: 13,
	}); err != nil {
		t.Fatal(err)
	}
	equipped := loadTestEquipment(t, ctx, repos, "77").Entries["13"]
	if len(equipped.RawEntry) <= 0x0D || equipped.RawEntry[0x0D] != 0 || equipped.Extra["seal_flag"] != "" ||
		equipped.Extra["seal_removed_by_first_equip"] != "1" || equipped.Extra["trade_locked_by_first_equip"] != "1" {
		t.Fatalf("equipped seal survived: raw=%x extra=%+v", equipped.RawEntry, equipped.Extra)
	}

	if _, err := owner.Move(ctx, MoveCommand{
		SelectedCharacterID: 77, SourceListType: wireListEquipment, SourceSlotIndex: 13, MoveCount: 1,
		DestinationListType: wireListMain, DestinationSlotIndex: 10,
	}); err != nil {
		t.Fatal(err)
	}
	unequipped := loadTestInventory(t, ctx, repos, "77").Slots["0:10"]
	if len(unequipped.RawEntry) <= 0x0D || unequipped.RawEntry[0x0D] != 0 || unequipped.Extra["seal_flag"] != "" ||
		unequipped.Extra["seal_removed_by_first_equip"] != "1" {
		t.Fatalf("unequipped item was resealed: raw=%x extra=%+v", unequipped.RawEntry, unequipped.Extra)
	}
}

func TestOwnerMoveEquipsPetCreatureToSlot26(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	before := richTestPetEntry("37", 0x17E69F80, wireListPet, 48)
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"7:48": {ItemID: 0x17E69F80, Count: 1, Extra: map[string]string{"creature_serial_or_handle": "37"}},
		},
	})
	saveTestEquipment(t, ctx, repos, dnfrepo.EquipmentRecord{CharacterID: "77", Entries: map[string]dnfrepo.EquipmentEntry{}})
	saveTestPet(t, ctx, repos, dnfrepo.PetRecord{
		CharacterID: "77",
		Entries:     map[string]dnfrepo.PetEntry{"37": before},
		TownDisplay: true,
	})

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	result, err := owner.Move(ctx, MoveCommand{
		SelectedCharacterID:  77,
		SourceListType:       wireListPet,
		SourceSlotIndex:      48,
		MoveCount:            1,
		DestinationListType:  wireListEquipment,
		DestinationSlotIndex: 26,
	})
	if err != nil {
		t.Fatalf("Move error = %v", err)
	}
	if result.Mode != "equip" || result.ItemID != 0x17E69F80 || !result.Changed {
		t.Fatalf("result = %+v", result)
	}

	inventory := loadTestInventory(t, ctx, repos, "77")
	if _, ok := inventory.Slots["7:48"]; ok {
		t.Fatalf("pet source slot should be empty: %+v", inventory.Slots)
	}
	equipment := loadTestEquipment(t, ctx, repos, "77")
	entry := equipment.Entries["26"]
	if entry.ItemID != 0x17E69F80 || len(entry.RawEntry) < 28 {
		t.Fatalf("pet equipment entry = %+v raw=% X", entry, entry.RawEntry)
	}
	if got := binary.LittleEndian.Uint32(entry.RawEntry[5:]); got != 37 {
		t.Fatalf("raw instance serial = %d, want 37", got)
	}
	if got := binary.LittleEndian.Uint32(entry.RawEntry[24:]); got != 37 {
		t.Fatalf("raw creature extra = %d, want 37", got)
	}
	pet := loadTestPet(t, ctx, repos, "77")
	if pet.EquippedKey != "37" || !pet.TownDisplay {
		t.Fatalf("pet equipped state = %+v", pet)
	}
	want := before
	want.SourceListType = wireListEquipment
	want.SourceSlotIndex = 26
	if got := pet.Entries["37"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("pet growth/location = %+v, want %+v", got, want)
	}
}

func TestOwnerMoveEquipsPetCreatureAndPreservesEnchant(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"7:48": {
				ItemID: 400990168,
				Count:  1,
				Extra: map[string]string{
					"creature_serial_or_handle": "41",
					"enchant_card_id":           "10008705",
					"enchant_upgrade_count":     "3",
				},
			},
		},
	})
	saveTestEquipment(t, ctx, repos, dnfrepo.EquipmentRecord{CharacterID: "77", Entries: map[string]dnfrepo.EquipmentEntry{}})
	saveTestPet(t, ctx, repos, dnfrepo.PetRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.PetEntry{
			"41": richTestPetEntry("41", 400990168, wireListPet, 48),
		},
	})

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Move(ctx, MoveCommand{
		SelectedCharacterID:  77,
		SourceListType:       wireListPet,
		SourceSlotIndex:      48,
		MoveCount:            1,
		DestinationListType:  wireListEquipment,
		DestinationSlotIndex: 26,
	}); err != nil {
		t.Fatal(err)
	}

	entry := loadTestEquipment(t, ctx, repos, "77").Entries["26"]
	if len(entry.RawEntry) < 0x15 {
		t.Fatalf("pet equipment raw too short: %x", entry.RawEntry)
	}
	if got := binary.LittleEndian.Uint32(entry.RawEntry[0x10:0x14]); got != 10008705 {
		t.Fatalf("raw enchant card=%d want 10008705 raw=%x", got, entry.RawEntry)
	}
	if got := entry.RawEntry[0x14]; got != 3 {
		t.Fatalf("raw enchant upgrade=%d want 3 raw=%x", got, entry.RawEntry)
	}
	if entry.Extra["pet_enchant_card_item_id"] != "10008705" ||
		entry.Extra["enchant_card_id"] != "10008705" ||
		entry.Extra["value_a"] != "10008705" ||
		entry.Extra["enchant_upgrade_count"] != "3" ||
		entry.Extra["byte_12"] != "3" {
		t.Fatalf("pet enchant extras not canonicalized: %+v", entry.Extra)
	}
}

func TestOwnerMoveUnequipsPetCreatureToPetInventory(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	before := richTestPetEntry("37", 0x17E69F80, wireListEquipment, 26)
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{CharacterID: "77", Slots: map[string]dnfrepo.ItemStack{}})
	saveTestEquipment(t, ctx, repos, dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"26": {SlotIndex: 26, ItemID: 0x17E69F80, RawEntry: buildPetCreatureEquipEntry(26, 0x17E69F80, 37), Extra: map[string]string{"source": "equipped-pet"}},
		},
	})
	saveTestPet(t, ctx, repos, dnfrepo.PetRecord{
		CharacterID: "77",
		Entries:     map[string]dnfrepo.PetEntry{"37": before},
		EquippedKey: "37",
		TownDisplay: true,
	})

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	result, err := owner.Move(ctx, MoveCommand{
		SelectedCharacterID:  77,
		SourceListType:       wireListEquipment,
		SourceSlotIndex:      26,
		MoveCount:            1,
		DestinationListType:  wireListPet,
		DestinationSlotIndex: 48,
	})
	if err != nil {
		t.Fatalf("Move error = %v", err)
	}
	if result.Mode != "unequip" || !result.Changed {
		t.Fatalf("result = %+v", result)
	}

	equipment := loadTestEquipment(t, ctx, repos, "77")
	if _, ok := equipment.Entries["26"]; ok {
		t.Fatalf("equipment slot should be empty: %+v", equipment.Entries)
	}
	inventory := loadTestInventory(t, ctx, repos, "77")
	got := inventory.Slots["7:48"]
	if got.ItemID != 0x17E69F80 || got.Extra["creature_serial_or_handle"] != "37" || got.Extra["raw_entry_hex"] == "" {
		t.Fatalf("pet inventory stack = %+v", got)
	}
	pet := loadTestPet(t, ctx, repos, "77")
	if pet.EquippedKey != "" || !pet.TownDisplay {
		t.Fatalf("pet equipped state = %+v", pet)
	}
	want := before
	want.SourceListType = wireListPet
	want.SourceSlotIndex = 48
	if got := pet.Entries["37"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("pet growth/location = %+v, want %+v", got, want)
	}
}

func TestOwnerMoveSwapsPetCreaturesAndPreservesGrowth(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	equippedBefore := richTestPetEntry("37", 9001, wireListEquipment, 26)
	inventoryBefore := richTestPetEntry("38", 9002, wireListPet, 48)
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"7:48": {ItemID: 9002, Count: 1, Extra: map[string]string{"creature_serial_or_handle": "38"}},
		},
	})
	saveTestEquipment(t, ctx, repos, dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"26": {SlotIndex: 26, ItemID: 9001, RawEntry: buildPetCreatureEquipEntry(26, 9001, 37), Extra: map[string]string{"source": "equipped-pet"}},
		},
	})
	saveTestPet(t, ctx, repos, dnfrepo.PetRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.PetEntry{
			"37": equippedBefore,
			"38": inventoryBefore,
		},
		EquippedKey: "37",
		TownDisplay: true,
	})

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	result, err := owner.Move(ctx, MoveCommand{
		SelectedCharacterID:  77,
		SourceListType:       wireListPet,
		SourceSlotIndex:      48,
		MoveCount:            1,
		DestinationListType:  wireListEquipment,
		DestinationSlotIndex: 26,
	})
	if err != nil {
		t.Fatalf("Move error = %v", err)
	}
	if result.Mode != "equip_swap" || result.ItemID != 9002 || result.SwappedItemID != 9001 {
		t.Fatalf("result = %+v", result)
	}
	if got := loadTestInventory(t, ctx, repos, "77").Slots["7:48"]; got.ItemID != 9001 || got.Extra["creature_serial_or_handle"] != "37" {
		t.Fatalf("swapped inventory pet = %+v", got)
	}
	if got := loadTestEquipment(t, ctx, repos, "77").Entries["26"]; got.ItemID != 9002 || petEquipmentSerial(got) != 38 {
		t.Fatalf("swapped equipped pet = %+v", got)
	}
	pet := loadTestPet(t, ctx, repos, "77")
	if pet.EquippedKey != "38" || !pet.TownDisplay {
		t.Fatalf("pet equipped state = %+v", pet)
	}
	wantEquipped := equippedBefore
	wantEquipped.SourceListType = wireListPet
	wantEquipped.SourceSlotIndex = 48
	wantInventory := inventoryBefore
	wantInventory.SourceListType = wireListEquipment
	wantInventory.SourceSlotIndex = 26
	if got := pet.Entries["37"]; !reflect.DeepEqual(got, wantEquipped) {
		t.Fatalf("old equipped pet = %+v, want %+v", got, wantEquipped)
	}
	if got := pet.Entries["38"]; !reflect.DeepEqual(got, wantInventory) {
		t.Fatalf("new equipped pet = %+v, want %+v", got, wantInventory)
	}
}

func TestOwnerMovePetMismatchRollsBackInventoryEquipmentAndPet(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	inventoryBefore := dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"7:48": {ItemID: 9001, Count: 1, Extra: map[string]string{"creature_serial_or_handle": "37"}},
		},
	}
	equipmentBefore := dnfrepo.EquipmentRecord{CharacterID: "77", Entries: map[string]dnfrepo.EquipmentEntry{}}
	petBefore := dnfrepo.PetRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.PetEntry{
			"38": richTestPetEntry("38", 9002, wireListPet, 49),
		},
		TownDisplay: true,
	}
	saveTestInventory(t, ctx, repos, inventoryBefore)
	saveTestEquipment(t, ctx, repos, equipmentBefore)
	saveTestPet(t, ctx, repos, petBefore)
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	_, err = owner.Move(ctx, MoveCommand{
		SelectedCharacterID:  77,
		SourceListType:       wireListPet,
		SourceSlotIndex:      48,
		MoveCount:            1,
		DestinationListType:  wireListEquipment,
		DestinationSlotIndex: 26,
	})
	if !errors.Is(err, ErrPetEntryNotFound) {
		t.Fatalf("Move error = %v, want ErrPetEntryNotFound", err)
	}
	if got := loadTestInventory(t, ctx, repos, "77"); !reflect.DeepEqual(got.Slots, inventoryBefore.Slots) {
		t.Fatalf("inventory mutated after rollback: %+v", got.Slots)
	}
	if got := loadTestEquipment(t, ctx, repos, "77"); len(got.Entries) != 0 {
		t.Fatalf("equipment mutated after rollback: %+v", got.Entries)
	}
	if got := loadTestPet(t, ctx, repos, "77"); !reflect.DeepEqual(got.Entries, petBefore.Entries) || got.EquippedKey != petBefore.EquippedKey || got.TownDisplay != petBefore.TownDisplay {
		t.Fatalf("pet mutated after rollback: %+v", got)
	}
}

func TestOwnerMoveUnequipsEntryToInventory(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{CharacterID: "77", Slots: map[string]dnfrepo.ItemStack{}})
	saveTestEquipment(t, ctx, repos, dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"11": {SlotIndex: 11, ItemID: 700, RawEntry: []byte{0, 1, 2, 3}, Extra: map[string]string{"source": "equipped"}},
		},
	})

	owner, err := newValidatedTestOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	result, err := owner.Move(ctx, MoveCommand{
		SelectedCharacterID:  77,
		SourceListType:       wireListEquipment,
		SourceSlotIndex:      11,
		MoveCount:            1,
		DestinationListType:  wireListMain,
		DestinationSlotIndex: 8,
	})
	if err != nil {
		t.Fatalf("Move error = %v", err)
	}
	if result.Mode != "unequip" || !result.Changed {
		t.Fatalf("result = %+v", result)
	}

	equipment := loadTestEquipment(t, ctx, repos, "77")
	if _, ok := equipment.Entries["11"]; ok {
		t.Fatalf("equipment slot should be empty: %+v", equipment.Entries)
	}
	inventory := loadTestInventory(t, ctx, repos, "77")
	if got := inventory.Slots["0:8"]; got.ItemID != 700 || got.Count != 1 || got.Extra["raw_entry_hex"] != "00010203" {
		t.Fatalf("inventory stack = %+v", got)
	}
}

func TestOwnerMoveUnequipsEntryWithTailDataAliasPreservesRaw(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	raw := make([]byte, 0x77)
	tail := make([]byte, equipmentTailDataBytes)
	tail[0] = 2
	binary.LittleEndian.PutUint32(tail[1:5], 0xFFFFFFFF)
	binary.LittleEndian.PutUint32(tail[5:9], 0xFFFFFFFF)
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{CharacterID: "77", Slots: map[string]dnfrepo.ItemStack{}})
	saveTestEquipment(t, ctx, repos, dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"11": {SlotIndex: 11, ItemID: 700, RawEntry: raw, Extra: map[string]string{"tailData2F": hex.EncodeToString(tail)}},
		},
	})

	owner, err := newValidatedTestOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	if _, err := owner.Move(ctx, MoveCommand{
		SelectedCharacterID:  77,
		SourceListType:       wireListEquipment,
		SourceSlotIndex:      11,
		MoveCount:            1,
		DestinationListType:  wireListMain,
		DestinationSlotIndex: 8,
	}); err != nil {
		t.Fatalf("Move error = %v", err)
	}

	stack := loadTestInventory(t, ctx, repos, "77").Slots["0:8"]
	if !bytes.Equal(stack.RawEntry, raw) {
		t.Fatalf("inventory raw = %x, want preserved %x", stack.RawEntry, raw)
	}
}

func TestOwnerMoveUnequipUsesNextEmptyMainSlotFromStaleClientDestination(t *testing.T) {
	for occupiedThrough := int16(10); occupiedThrough <= 12; occupiedThrough++ {
		t.Run(fmt.Sprintf("occupied_through_%d", occupiedThrough), func(t *testing.T) {
			ctx := context.Background()
			repos := dnfrepomemory.NewMemoryGroup()
			slots := make(map[string]dnfrepo.ItemStack)
			for slot := int16(10); slot <= occupiedThrough; slot++ {
				slots[inventoryKey(wireListMain, slot)] = dnfrepo.ItemStack{
					ItemID:   int64(800 + slot),
					Count:    1,
					RawEntry: []byte{byte(slot), 0xA5},
				}
			}
			saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{CharacterID: "77", Slots: slots})
			saveTestEquipment(t, ctx, repos, dnfrepo.EquipmentRecord{
				CharacterID: "77",
				Entries: map[string]dnfrepo.EquipmentEntry{
					"17": {SlotIndex: 17, ItemID: 700, RawEntry: []byte{0x11, 0x22}, Extra: map[string]string{"source": "equipped"}},
				},
			})

			validator := &recordingPlacementValidator{}
			owner, err := NewOwnerWithPlacementValidator(repos, validator)
			if err != nil {
				t.Fatal(err)
			}
			result, err := owner.Move(ctx, MoveCommand{
				SelectedCharacterID:      77,
				SourceListType:           wireListMain,
				SourceSlotIndex:          10,
				SourceInstanceValue:      0,
				MoveCount:                1,
				DestinationListType:      wireListEquipment,
				DestinationSlotIndex:     17,
				DestinationInstanceValue: 0x47E0,
			})
			if err != nil {
				t.Fatalf("Move error = %v", err)
			}
			wantSlot := occupiedThrough + 1
			if result.Mode != "unequip" || !result.Changed || result.SourceListType != wireListMain || result.SourceSlotIndex != wantSlot || result.DestinationListType != wireListEquipment || result.DestinationSlotIndex != 17 {
				t.Fatalf("result = %+v, want redirected main slot %d", result, wantSlot)
			}
			loadedInventory := loadTestInventory(t, ctx, repos, "77")
			for slot := int16(10); slot <= occupiedThrough; slot++ {
				if got := loadedInventory.Slots[inventoryKey(wireListMain, slot)].ItemID; got != int64(800+slot) {
					t.Fatalf("occupied slot %d changed to item %d", slot, got)
				}
			}
			if got := loadedInventory.Slots[inventoryKey(wireListMain, wantSlot)]; got.ItemID != 700 || !bytes.Equal(got.RawEntry, []byte{0x11, 0x22}) {
				t.Fatalf("redirected stack = %+v", got)
			}
			if entries := loadTestEquipment(t, ctx, repos, "77").Entries; len(entries) != 0 {
				t.Fatalf("equipment after redirected unequip = %+v", entries)
			}
			if len(validator.placements) != 0 {
				t.Fatalf("unequip relocation must not validate a fake swap: %+v", validator.placements)
			}
		})
	}
}

func TestNextEmptyUnequipGuildMedalNeverCrossesIntoGuardianGemPage(t *testing.T) {
	const guardianGemSlot int16 = 49
	items := make(map[string]dnfrepo.ItemStack)
	for slot := guildMedalInventorySlotStart; slot < guildMedalInventorySlotEnd; slot++ {
		items[inventoryKey(wireListGuildMedal, slot)] = dnfrepo.ItemStack{ItemID: int64(1000 + slot), Count: 1}
	}
	items[inventoryKey(wireListGuildMedal, guardianGemSlot)] = dnfrepo.ItemStack{ItemID: 90003, Count: 1}

	if got, ok := nextEmptyUnequipInventorySlot(items, wireListGuildMedal, guildMedalInventorySlotEnd); !ok || got != guildMedalInventorySlotEnd {
		t.Fatalf("last medal-page empty slot=(%d,%v), want=(%d,true)", got, ok, guildMedalInventorySlotEnd)
	}
	items[inventoryKey(wireListGuildMedal, guildMedalInventorySlotEnd)] = dnfrepo.ItemStack{ItemID: 2000, Count: 1}
	if got, ok := nextEmptyUnequipInventorySlot(items, wireListGuildMedal, guildMedalInventorySlotEnd); ok {
		t.Fatalf("full medal page returned guardian-page slot=(%d,true)", got)
	}
	if got, ok := nextEmptyUnequipInventorySlot(items, wireListGuildMedal, guardianGemSlot); ok {
		t.Fatalf("guardian-page requested slot=(%d,true), want rejected", got)
	}
}

func TestNextEmptyUnequipInventorySlotStaysInsideCurrentEXEContainerPage(t *testing.T) {
	tests := []struct {
		name        string
		listType    byte
		requested   int16
		occupiedEnd int16
		want        int16
	}{
		{
			name:        "avatar",
			listType:    wireListAvatar,
			requested:   207,
			occupiedEnd: 208,
			want:        209,
		},
		{
			name:        "pet_body",
			listType:    wireListPet,
			requested:   137,
			occupiedEnd: 138,
			want:        139,
		},
		{
			name:        "pet_artifact",
			listType:    wireListPet,
			requested:   186,
			occupiedEnd: 187,
			want:        188,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			items := make(map[string]dnfrepo.ItemStack)
			for slot := test.requested; slot <= test.occupiedEnd; slot++ {
				items[inventoryKey(test.listType, slot)] = dnfrepo.ItemStack{
					ItemID: int64(1000 + slot),
					Count:  1,
				}
			}
			got, ok := nextEmptyUnequipInventorySlot(items, test.listType, test.requested)
			if !ok || got != test.want {
				t.Fatalf("next empty slot=(%d,%t), want=(%d,true)", got, ok, test.want)
			}
		})
	}

	// A full creature-body page must not spill into the artifact page, and a
	// full artifact page must not spill into pet consumables.
	for _, page := range []struct {
		name      string
		requested int16
		start     int16
		end       int16
	}{
		{name: "pet_body_full", requested: 139, start: petBodyInventorySlotStart, end: petBodyInventorySlotEnd},
		{name: "pet_artifact_full", requested: 188, start: petArtifactInventorySlotStart, end: petArtifactInventorySlotEnd},
	} {
		t.Run(page.name, func(t *testing.T) {
			items := make(map[string]dnfrepo.ItemStack)
			for slot := page.start; slot <= page.end; slot++ {
				items[inventoryKey(wireListPet, slot)] = dnfrepo.ItemStack{
					ItemID: int64(2000 + slot),
					Count:  1,
				}
			}
			if got, ok := nextEmptyUnequipInventorySlot(items, wireListPet, page.requested); ok {
				t.Fatalf("full page returned cross-page slot=(%d,true)", got)
			}
		})
	}
}

func TestOwnerMoveRejectsGuildMedalEndpointOnGuardianGemPage(t *testing.T) {
	ctx := context.Background()
	for _, test := range []struct {
		name      string
		inventory dnfrepo.InventoryRecord
		equipment dnfrepo.EquipmentRecord
		command   MoveCommand
	}{
		{
			name: "equip_from_guardian_page",
			inventory: dnfrepo.InventoryRecord{CharacterID: "77", Slots: map[string]dnfrepo.ItemStack{
				"38:49": {ItemID: 100380060, Count: 1, RawEntry: []byte{0x31, 0xA5}},
			}},
			equipment: dnfrepo.EquipmentRecord{CharacterID: "77", Entries: map[string]dnfrepo.EquipmentEntry{}},
			command:   MoveCommand{SelectedCharacterID: 77, SourceListType: wireListGuildMedal, SourceSlotIndex: 49, MoveCount: 1, DestinationListType: wireListEquipment, DestinationSlotIndex: 32},
		},
		{
			name:      "unequip_to_guardian_page",
			inventory: dnfrepo.InventoryRecord{CharacterID: "77", Slots: map[string]dnfrepo.ItemStack{}},
			equipment: dnfrepo.EquipmentRecord{CharacterID: "77", Entries: map[string]dnfrepo.EquipmentEntry{
				"32": {SlotIndex: 32, ItemID: 100380060, RawEntry: []byte{0x20, 0xA5}},
			}},
			command: MoveCommand{SelectedCharacterID: 77, SourceListType: wireListGuildMedal, SourceSlotIndex: 49, MoveCount: 1, DestinationListType: wireListEquipment, DestinationSlotIndex: 32},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			repos := dnfrepomemory.NewMemoryGroup()
			saveTestInventory(t, ctx, repos, test.inventory)
			saveTestEquipment(t, ctx, repos, test.equipment)
			owner, err := NewOwnerWithPlacementValidator(repos, &recordingPlacementValidator{})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := owner.Move(ctx, test.command); !errors.Is(err, ErrMoveUnsupported) {
				t.Fatalf("Move error=%v, want %v", err, ErrMoveUnsupported)
			}
		})
	}
}

func TestOwnerMoveRejectsEquipWithoutRawEntryEvidence(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"0:5": {ItemID: 700, Count: 1},
		},
	})
	saveTestEquipment(t, ctx, repos, dnfrepo.EquipmentRecord{CharacterID: "77", Entries: map[string]dnfrepo.EquipmentEntry{}})

	owner, err := newValidatedTestOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	_, err = owner.Move(ctx, MoveCommand{
		SelectedCharacterID:  77,
		SourceListType:       wireListMain,
		SourceSlotIndex:      5,
		MoveCount:            1,
		DestinationListType:  wireListEquipment,
		DestinationSlotIndex: 11,
	})
	if !errors.Is(err, ErrMoveRawEntryMissing) {
		t.Fatalf("Move error = %v, want ErrMoveRawEntryMissing", err)
	}
}

func TestOwnerMoveRejectsEquipmentStackCountGreaterThanOne(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"0:5": {ItemID: 700, Count: 1, Extra: map[string]string{"raw_entry_hex": "00010203"}},
		},
	})
	saveTestEquipment(t, ctx, repos, dnfrepo.EquipmentRecord{CharacterID: "77", Entries: map[string]dnfrepo.EquipmentEntry{}})

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	_, err = owner.Move(ctx, MoveCommand{
		SelectedCharacterID:  77,
		SourceListType:       wireListMain,
		SourceSlotIndex:      5,
		MoveCount:            2,
		DestinationListType:  wireListEquipment,
		DestinationSlotIndex: 11,
	})
	if !errors.Is(err, ErrMoveStackCountInvalid) {
		t.Fatalf("Move error = %v, want ErrMoveStackCountInvalid", err)
	}

	inventory := loadTestInventory(t, ctx, repos, "77")
	if got := inventory.Slots["0:5"].ItemID; got != 700 {
		t.Fatalf("inventory item = %d, want unchanged 700", got)
	}
}

func TestOwnerMoveResolvesFixedEndpointsForEquipAndUnequip(t *testing.T) {
	tests := []struct {
		name       string
		listType   byte
		sourceSlot int16
		targetSlot int16
	}{
		{name: "main_weapon", listType: wireListMain, sourceSlot: 9, targetSlot: 12},
		{name: "avatar", listType: wireListAvatar, sourceSlot: 19, targetSlot: 0},
		{name: "personal_cargo_title", listType: wireListPersonalCargo, sourceSlot: 29, targetSlot: 13},
		{name: "guild_medal", listType: wireListGuildMedal, sourceSlot: 7, targetSlot: 32},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			repos := dnfrepomemory.NewMemoryGroup()
			itemID := int64(700 + tt.targetSlot)
			stack := dnfrepo.ItemStack{ItemID: itemID, Count: 1, RawEntry: []byte{byte(tt.targetSlot + 1), 0xA5}}
			inventory := dnfrepo.InventoryRecord{CharacterID: "77", Slots: map[string]dnfrepo.ItemStack{}, Warehouse: map[string]dnfrepo.ItemStack{}}
			if tt.listType == wireListPersonalCargo {
				inventory.Warehouse[inventoryKey(tt.listType, tt.sourceSlot)] = stack
			} else {
				inventory.Slots[inventoryKey(tt.listType, tt.sourceSlot)] = stack
			}
			saveTestInventory(t, ctx, repos, inventory)
			saveTestEquipment(t, ctx, repos, dnfrepo.EquipmentRecord{CharacterID: "77", Entries: map[string]dnfrepo.EquipmentEntry{}})

			validator := &recordingPlacementValidator{}
			owner, err := NewOwnerWithPlacementValidator(repos, validator)
			if err != nil {
				t.Fatalf("NewOwnerWithPlacementValidator error = %v", err)
			}
			cmd := MoveCommand{
				SelectedCharacterID:  77,
				SourceListType:       tt.listType,
				SourceSlotIndex:      tt.sourceSlot,
				MoveCount:            1,
				DestinationListType:  wireListEquipment,
				DestinationSlotIndex: tt.targetSlot,
			}

			equipped, err := owner.Move(ctx, cmd)
			if err != nil {
				t.Fatalf("equip Move error = %v", err)
			}
			if equipped.Mode != "equip" || equipped.ItemID != itemID {
				t.Fatalf("equip result = %+v", equipped)
			}
			assertMoveResultEndpoints(t, equipped, cmd)
			if entry := loadTestEquipment(t, ctx, repos, "77").Entries[entryKey(tt.targetSlot)]; entry.ItemID != itemID || entry.SlotIndex != tt.targetSlot || !bytes.Equal(entry.RawEntry, stack.RawEntry) {
				t.Fatalf("equipped entry = %+v", entry)
			} else if entry.Extra["current_exe_equipment_type"] != strconv.Itoa(int(tt.targetSlot)) || entry.Extra["current_exe_runtime_move"] != "1" {
				t.Fatalf("equipped runtime slot metadata = %+v", entry.Extra)
			}

			unequipped, err := owner.Move(ctx, cmd)
			if err != nil {
				t.Fatalf("unequip fixed-endpoint Move error = %v", err)
			}
			if unequipped.Mode != "unequip" || unequipped.ItemID != itemID {
				t.Fatalf("unequip result = %+v", unequipped)
			}
			assertMoveResultEndpoints(t, unequipped, cmd)
			if entries := loadTestEquipment(t, ctx, repos, "77").Entries; len(entries) != 0 {
				t.Fatalf("equipment after unequip = %+v", entries)
			}
			loadedInventory := loadTestInventory(t, ctx, repos, "77")
			items, _, ok := inventoryMap(&loadedInventory, tt.listType)
			if !ok {
				t.Fatalf("inventoryMap list=%d unsupported", tt.listType)
			}
			if got := items[inventoryKey(tt.listType, tt.sourceSlot)]; got.ItemID != itemID || !bytes.Equal(got.RawEntry, stack.RawEntry) {
				t.Fatalf("restored stack = %+v", got)
			}
			if len(validator.placements) != 1 || validator.placements[0].TargetSlotIndex != tt.targetSlot {
				t.Fatalf("placements = %+v", validator.placements)
			}
		})
	}
}

func TestOwnerMovePermanentAvatarPreservesZeroContainerCount(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	avatar := dnfrepo.ItemStack{
		ItemID:   710,
		Count:    0, // current-EXE permanent-avatar amount
		RawEntry: []byte{0xCA, 0xFE},
		Extra: map[string]string{
			"item_kind":       "avatar",
			"amount_or_count": "0",
		},
	}
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			inventoryKey(wireListAvatar, 19): avatar,
		},
	})
	saveTestEquipment(t, ctx, repos, dnfrepo.EquipmentRecord{CharacterID: "77", Entries: map[string]dnfrepo.EquipmentEntry{}})

	validator := &recordingPlacementValidator{}
	owner, err := NewOwnerWithPlacementValidator(repos, validator)
	if err != nil {
		t.Fatalf("NewOwnerWithPlacementValidator error = %v", err)
	}
	cmd := MoveCommand{
		SelectedCharacterID:  77,
		SourceListType:       wireListAvatar,
		SourceSlotIndex:      19,
		MoveCount:            1,
		DestinationListType:  wireListEquipment,
		DestinationSlotIndex: 0,
	}

	if result, err := owner.Move(ctx, cmd); err != nil || result.Mode != "equip" {
		t.Fatalf("equip permanent avatar result=%+v err=%v", result, err)
	}
	entry := loadTestEquipment(t, ctx, repos, "77").Entries[entryKey(0)]
	if entry.ItemID != avatar.ItemID || entry.Extra["current_exe_inventory_count"] != "0" {
		t.Fatalf("equipped permanent avatar=%+v", entry)
	}
	if len(validator.placements) != 1 || validator.placements[0].SourceListType != wireListAvatar || validator.placements[0].TargetSlotIndex != 0 {
		t.Fatalf("placements=%+v", validator.placements)
	}

	if result, err := owner.Move(ctx, cmd); err != nil || result.Mode != "unequip" {
		t.Fatalf("unequip permanent avatar result=%+v err=%v", result, err)
	}
	restored := loadTestInventory(t, ctx, repos, "77").Slots[inventoryKey(wireListAvatar, 19)]
	if restored.ItemID != avatar.ItemID || restored.Count != 0 || !bytes.Equal(restored.RawEntry, avatar.RawEntry) || restored.Extra["amount_or_count"] != "0" {
		t.Fatalf("restored permanent avatar=%+v", restored)
	}
}

func TestOwnerMoveMigratesLegacyPVFStarterSlotsForCurrentEndpointRoundTrip(t *testing.T) {
	tests := []struct {
		name        string
		legacySlot  int16
		currentSlot int16
		itemID      int64
	}{
		{name: "weapon", legacySlot: 11, currentSlot: 12, itemID: 101010912},
		{name: "coat", legacySlot: 13, currentSlot: 14, itemID: 10400},
		{name: "pants", legacySlot: 15, currentSlot: 16, itemID: 12400},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			repos := dnfrepomemory.NewMemoryGroup()
			raw := []byte{byte(tt.legacySlot), 0xA5, 0x5A}
			saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
				CharacterID: "77",
				Slots:       map[string]dnfrepo.ItemStack{},
				Warehouse:   map[string]dnfrepo.ItemStack{},
			})
			saveTestEquipment(t, ctx, repos, dnfrepo.EquipmentRecord{
				CharacterID: "77",
				Entries: map[string]dnfrepo.EquipmentEntry{
					entryKey(tt.legacySlot): {
						SlotIndex: tt.legacySlot,
						ItemID:    tt.itemID,
						RawEntry:  raw,
						Extra: map[string]string{
							"source":        "pvf_create_equipment_list",
							"preserve_test": "yes",
						},
					},
				},
			})
			validator := &recordingPlacementValidator{}
			owner, err := NewOwnerWithPlacementValidator(repos, validator)
			if err != nil {
				t.Fatalf("NewOwnerWithPlacementValidator error = %v", err)
			}
			cmd := MoveCommand{
				SelectedCharacterID:  77,
				SourceListType:       wireListMain,
				SourceSlotIndex:      9,
				MoveCount:            1,
				DestinationListType:  wireListEquipment,
				DestinationSlotIndex: tt.currentSlot,
			}

			unequipped, err := owner.Move(ctx, cmd)
			if err != nil {
				t.Fatalf("legacy unequip Move error = %v", err)
			}
			if unequipped.Mode != "unequip" || unequipped.ItemID != tt.itemID {
				t.Fatalf("legacy unequip result = %+v", unequipped)
			}
			assertMoveResultEndpoints(t, unequipped, cmd)
			if entries := loadTestEquipment(t, ctx, repos, "77").Entries; len(entries) != 0 {
				t.Fatalf("equipment after legacy unequip = %+v", entries)
			}
			stack := loadTestInventory(t, ctx, repos, "77").Slots[inventoryKey(wireListMain, 9)]
			if stack.ItemID != tt.itemID || !bytes.Equal(stack.RawEntry, raw) || stack.Extra["preserve_test"] != "yes" {
				t.Fatalf("legacy unequip stack = %+v", stack)
			}
			if stack.Extra["current_exe_equipment_type"] != strconv.Itoa(int(tt.currentSlot)) || stack.Extra["current_exe_runtime_move"] != "1" {
				t.Fatalf("legacy migration stack metadata = %+v", stack.Extra)
			}

			equipped, err := owner.Move(ctx, cmd)
			if err != nil {
				t.Fatalf("legacy re-equip Move error = %v", err)
			}
			if equipped.Mode != "equip" || equipped.ItemID != tt.itemID {
				t.Fatalf("legacy re-equip result = %+v", equipped)
			}
			assertMoveResultEndpoints(t, equipped, cmd)
			entries := loadTestEquipment(t, ctx, repos, "77").Entries
			if _, exists := entries[entryKey(tt.legacySlot)]; exists {
				t.Fatalf("legacy alias still exists after round trip: %+v", entries)
			}
			entry := entries[entryKey(tt.currentSlot)]
			if entry.ItemID != tt.itemID || entry.SlotIndex != tt.currentSlot || !bytes.Equal(entry.RawEntry, raw) {
				t.Fatalf("current slot entry = %+v", entry)
			}
			if entry.Extra["current_exe_equipment_type"] != strconv.Itoa(int(tt.currentSlot)) || entry.Extra["current_exe_runtime_move"] != "1" {
				t.Fatalf("current slot metadata = %+v", entry.Extra)
			}
			if len(validator.placements) != 1 || validator.placements[0].TargetSlotIndex != tt.currentSlot {
				t.Fatalf("placements = %+v", validator.placements)
			}

			// Replaying the same fixed-endpoint request must see the persisted
			// current slot, not reinterpret it as another legacy category.
			replayed, err := owner.Move(ctx, cmd)
			if err != nil || replayed.Mode != "unequip" || replayed.ItemID != tt.itemID {
				t.Fatalf("replayed current-slot unequip = %+v err=%v", replayed, err)
			}
		})
	}
}

func TestOwnerMoveLegacyPVFSlotCollisionRollsBackEveryAlias(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots:       map[string]dnfrepo.ItemStack{},
		Warehouse:   map[string]dnfrepo.ItemStack{},
	})
	before := dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"11": {SlotIndex: 11, ItemID: 701, RawEntry: []byte{11}, Extra: map[string]string{"source": "pvf_create_equipment_list"}},
			"13": {SlotIndex: 13, ItemID: 702, RawEntry: []byte{13}, Extra: map[string]string{"source": "pvf_create_equipment_list"}},
			"15": {SlotIndex: 15, ItemID: 703, RawEntry: []byte{15}, Extra: map[string]string{"source": "pvf_create_equipment_list"}},
			"16": {SlotIndex: 16, ItemID: 999, RawEntry: []byte{16}, Extra: map[string]string{"source": "runtime"}},
		},
	}
	saveTestEquipment(t, ctx, repos, before)
	owner, err := newValidatedTestOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}

	_, err = owner.Move(ctx, MoveCommand{
		SelectedCharacterID:  77,
		SourceListType:       wireListMain,
		SourceSlotIndex:      9,
		MoveCount:            1,
		DestinationListType:  wireListEquipment,
		DestinationSlotIndex: 12,
	})
	if !errors.Is(err, ErrLegacyPVFSlotCollision) {
		t.Fatalf("Move error = %v, want ErrLegacyPVFSlotCollision", err)
	}
	after := loadTestEquipment(t, ctx, repos, "77")
	if !reflect.DeepEqual(after.Entries, before.Entries) {
		t.Fatalf("collision partially migrated aliases: before=%+v after=%+v", before.Entries, after.Entries)
	}
	if slots := loadTestInventory(t, ctx, repos, "77").Slots; len(slots) != 0 {
		t.Fatalf("collision mutated inventory: %+v", slots)
	}
}

func TestOwnerMovePlacementFailureRollsBackLegacyPVFNormalization(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	inventoryBefore := dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"0:9": {ItemID: 812, Count: 1, RawEntry: []byte{0x81, 0x02}},
		},
	}
	equipmentBefore := dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"11": {SlotIndex: 11, ItemID: 712, RawEntry: []byte{0x71, 0x02}, Extra: map[string]string{"source": "pvf_create_equipment_list"}},
		},
	}
	saveTestInventory(t, ctx, repos, inventoryBefore)
	saveTestEquipment(t, ctx, repos, equipmentBefore)
	rejected := errors.New("placement rejected after legacy normalization")
	owner, err := NewOwnerWithPlacementValidator(repos, &recordingPlacementValidator{err: rejected})
	if err != nil {
		t.Fatalf("NewOwnerWithPlacementValidator error = %v", err)
	}

	_, err = owner.Move(ctx, MoveCommand{
		SelectedCharacterID:  77,
		SourceListType:       wireListMain,
		SourceSlotIndex:      9,
		MoveCount:            1,
		DestinationListType:  wireListEquipment,
		DestinationSlotIndex: 12,
	})
	if !errors.Is(err, rejected) {
		t.Fatalf("Move error = %v, want placement rejection", err)
	}
	if after := loadTestEquipment(t, ctx, repos, "77"); !reflect.DeepEqual(after.Entries, equipmentBefore.Entries) {
		t.Fatalf("placement failure committed legacy migration: before=%+v after=%+v", equipmentBefore.Entries, after.Entries)
	}
	if after := loadTestInventory(t, ctx, repos, "77"); !reflect.DeepEqual(after.Slots, inventoryBefore.Slots) {
		t.Fatalf("placement failure mutated inventory: before=%+v after=%+v", inventoryBefore.Slots, after.Slots)
	}
}

func TestOwnerMoveDoesNotMigrateRuntimeSlotsThatAliasLegacyPVFSlots(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{CharacterID: "77", Slots: map[string]dnfrepo.ItemStack{}})
	before := dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"11": {SlotIndex: 11, ItemID: 811, RawEntry: []byte{11}, Extra: map[string]string{"source": "runtime"}},
			"13": {SlotIndex: 13, ItemID: 813, RawEntry: []byte{13}, Extra: map[string]string{
				"source":                   "pvf_create_equipment_list",
				"current_exe_runtime_move": "1",
			}},
		},
	}
	saveTestEquipment(t, ctx, repos, before)
	owner, err := newValidatedTestOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}

	_, err = owner.Move(ctx, MoveCommand{
		SelectedCharacterID:  77,
		SourceListType:       wireListMain,
		SourceSlotIndex:      9,
		MoveCount:            1,
		DestinationListType:  wireListEquipment,
		DestinationSlotIndex: 12,
	})
	if !errors.Is(err, ErrMoveSlotNotFound) {
		t.Fatalf("Move error = %v, want empty current endpoint", err)
	}
	after := loadTestEquipment(t, ctx, repos, "77")
	if !reflect.DeepEqual(after.Entries, before.Entries) {
		t.Fatalf("runtime aliases were migrated: before=%+v after=%+v", before.Entries, after.Entries)
	}
}

func TestOwnerMoveSupportsActorAppearanceSlotsZeroThroughThirteen(t *testing.T) {
	for targetSlot := int16(0); targetSlot <= 13; targetSlot++ {
		name := "avatar"
		listType := wireListAvatar
		if targetSlot == 12 {
			name = "weapon"
			listType = wireListMain
		} else if targetSlot == 13 {
			name = "title"
			listType = wireListMain
		}
		t.Run(fmt.Sprintf("%s_slot_%d", name, targetSlot), func(t *testing.T) {
			ctx := context.Background()
			repos := dnfrepomemory.NewMemoryGroup()
			sourceSlot := int16(40 + targetSlot)
			itemID := int64(9000 + targetSlot)
			raw := []byte{0x5A, byte(targetSlot)}
			saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
				CharacterID: "77",
				Slots: map[string]dnfrepo.ItemStack{
					inventoryKey(listType, sourceSlot): {ItemID: itemID, Count: 1, RawEntry: raw},
				},
			})
			saveTestEquipment(t, ctx, repos, dnfrepo.EquipmentRecord{CharacterID: "77", Entries: map[string]dnfrepo.EquipmentEntry{}})
			validator := &recordingPlacementValidator{}
			owner, err := NewOwnerWithPlacementValidator(repos, validator)
			if err != nil {
				t.Fatalf("NewOwnerWithPlacementValidator error = %v", err)
			}

			_, err = owner.Move(ctx, MoveCommand{
				SelectedCharacterID:  77,
				SourceListType:       listType,
				SourceSlotIndex:      sourceSlot,
				MoveCount:            1,
				DestinationListType:  wireListEquipment,
				DestinationSlotIndex: targetSlot,
			})
			if err != nil {
				t.Fatalf("Move error = %v", err)
			}
			entry := loadTestEquipment(t, ctx, repos, "77").Entries[entryKey(targetSlot)]
			if entry.ItemID != itemID || entry.SlotIndex != targetSlot || !bytes.Equal(entry.RawEntry, raw) {
				t.Fatalf("entry = %+v", entry)
			}
			if len(validator.placements) != 1 || validator.placements[0].TargetSlotIndex != targetSlot {
				t.Fatalf("placements = %+v", validator.placements)
			}
		})
	}
}

func TestOwnerMoveResolvesOccupiedSecondEquipmentEndpoint(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{CharacterID: "77", Slots: map[string]dnfrepo.ItemStack{}})
	saveTestEquipment(t, ctx, repos, dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"13": {SlotIndex: 13, ItemID: 813, RawEntry: []byte{0x13}},
		},
	})
	validator := &recordingPlacementValidator{}
	owner, err := NewOwnerWithPlacementValidator(repos, validator)
	if err != nil {
		t.Fatalf("NewOwnerWithPlacementValidator error = %v", err)
	}
	cmd := MoveCommand{
		SelectedCharacterID:  77,
		SourceListType:       wireListEquipment,
		SourceSlotIndex:      12,
		MoveCount:            1,
		DestinationListType:  wireListEquipment,
		DestinationSlotIndex: 13,
	}

	result, err := owner.Move(ctx, cmd)
	if err != nil {
		t.Fatalf("Move error = %v", err)
	}
	if result.Mode != "equip_slot_swap" || result.ItemID != 813 {
		t.Fatalf("result = %+v", result)
	}
	assertMoveResultEndpoints(t, result, cmd)
	entries := loadTestEquipment(t, ctx, repos, "77").Entries
	if entries["12"].ItemID != 813 || entries["12"].SlotIndex != 12 {
		t.Fatalf("destination entry = %+v", entries["12"])
	}
	if _, ok := entries["13"]; ok {
		t.Fatalf("source endpoint still occupied: %+v", entries)
	}
	if len(validator.placements) != 1 || validator.placements[0].TargetSlotIndex != 12 {
		t.Fatalf("placements = %+v", validator.placements)
	}
}

func TestOwnerMoveRejectsPVFWrongSlotWithoutMutation(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	raw := []byte{0x12, 0x34}
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"0:5": {ItemID: 712, Count: 1, RawEntry: raw},
		},
	})
	saveTestEquipment(t, ctx, repos, dnfrepo.EquipmentRecord{CharacterID: "77", Entries: map[string]dnfrepo.EquipmentEntry{}})
	wrongSlot := errors.New("pvf wrong equipment slot")
	owner, err := NewOwnerWithPlacementValidator(repos, PlacementValidatorFunc(func(_ context.Context, placement Placement) error {
		if placement.TargetSlotIndex != 12 {
			return wrongSlot
		}
		return nil
	}))
	if err != nil {
		t.Fatalf("NewOwnerWithPlacementValidator error = %v", err)
	}

	_, err = owner.Move(ctx, MoveCommand{
		SelectedCharacterID:  77,
		SourceListType:       wireListMain,
		SourceSlotIndex:      5,
		MoveCount:            1,
		DestinationListType:  wireListEquipment,
		DestinationSlotIndex: 13,
	})
	if !errors.Is(err, wrongSlot) {
		t.Fatalf("Move error = %v, want PVF wrong-slot rejection", err)
	}
	if got := loadTestInventory(t, ctx, repos, "77").Slots["0:5"]; got.ItemID != 712 || !bytes.Equal(got.RawEntry, raw) {
		t.Fatalf("inventory mutated: %+v", got)
	}
	if entries := loadTestEquipment(t, ctx, repos, "77").Entries; len(entries) != 0 {
		t.Fatalf("equipment mutated: %+v", entries)
	}
}

func TestOwnerMoveRollsBackInventoryWhenEquipmentSaveFails(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	raw := []byte{0xAA, 0x55}
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"0:5": {ItemID: 700, Count: 1, RawEntry: raw},
		},
	})
	saveTestEquipment(t, ctx, repos, dnfrepo.EquipmentRecord{CharacterID: "77", Entries: map[string]dnfrepo.EquipmentEntry{}})
	commitErr := errors.New("equipment commit failed")
	repos.CharacterItems = rollbackTestCharacterItems{
		inventory: repos.Inventory,
		equipment: repos.Equipment,
		fail:      commitErr,
	}
	owner, err := newValidatedTestOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}

	_, err = owner.Move(ctx, MoveCommand{
		SelectedCharacterID:  77,
		SourceListType:       wireListMain,
		SourceSlotIndex:      5,
		MoveCount:            1,
		DestinationListType:  wireListEquipment,
		DestinationSlotIndex: 12,
	})
	if !errors.Is(err, commitErr) {
		t.Fatalf("Move error = %v, want equipment commit failure", err)
	}
	if got := loadTestInventory(t, ctx, repos, "77").Slots["0:5"]; got.ItemID != 700 || got.Count != 1 || !bytes.Equal(got.RawEntry, raw) {
		t.Fatalf("inventory was not rolled back: %+v", got)
	}
	if entries := loadTestEquipment(t, ctx, repos, "77").Entries; len(entries) != 0 {
		t.Fatalf("equipment mutated after rollback: %+v", entries)
	}
}

func assertMoveResultEndpoints(t *testing.T, result MoveResult, cmd MoveCommand) {
	t.Helper()
	if result.SourceListType != cmd.SourceListType || result.SourceSlotIndex != cmd.SourceSlotIndex || result.DestinationListType != cmd.DestinationListType || result.DestinationSlotIndex != cmd.DestinationSlotIndex {
		t.Fatalf("result endpoints = (%d,%d)->(%d,%d), want request (%d,%d)->(%d,%d)", result.SourceListType, result.SourceSlotIndex, result.DestinationListType, result.DestinationSlotIndex, cmd.SourceListType, cmd.SourceSlotIndex, cmd.DestinationListType, cmd.DestinationSlotIndex)
	}
}

type failingEquipmentRepository struct {
	dnfrepo.EquipmentRepository
	err error
}

func (r failingEquipmentRepository) Save(context.Context, dnfrepo.EquipmentRecord) error {
	return r.err
}

type rollbackTestCharacterItems struct {
	inventory dnfrepo.InventoryRepository
	equipment dnfrepo.EquipmentRepository
	fail      error
}

func (u rollbackTestCharacterItems) WithinCharacterItems(ctx context.Context, characterID string, apply func(dnfrepo.InventoryRepository, dnfrepo.EquipmentRepository) error) error {
	inventoryBefore, inventoryFound, err := u.inventory.Load(ctx, characterID)
	if err != nil {
		return err
	}
	equipmentBefore, equipmentFound, err := u.equipment.Load(ctx, characterID)
	if err != nil {
		return err
	}
	err = apply(u.inventory, failingEquipmentRepository{EquipmentRepository: u.equipment, err: u.fail})
	if err == nil {
		return nil
	}
	if inventoryFound {
		if restoreErr := u.inventory.Save(ctx, inventoryBefore); restoreErr != nil {
			return restoreErr
		}
	}
	if equipmentFound {
		if restoreErr := u.equipment.Save(ctx, equipmentBefore); restoreErr != nil {
			return restoreErr
		}
	}
	return err
}

func saveTestEquipment(t *testing.T, ctx context.Context, repos dnfrepo.Group, record dnfrepo.EquipmentRecord) {
	t.Helper()
	if err := repos.Equipment.Save(ctx, record); err != nil {
		t.Fatalf("Save equipment error = %v", err)
	}
}

func saveTestCharacter(t *testing.T, ctx context.Context, repos dnfrepo.Group, record dnfrepo.CharacterRecord) {
	t.Helper()
	if err := repos.Character.Save(ctx, record); err != nil {
		t.Fatalf("Save character error = %v", err)
	}
}

func loadTestEquipment(t *testing.T, ctx context.Context, repos dnfrepo.Group, characterID string) dnfrepo.EquipmentRecord {
	t.Helper()
	record, ok, err := repos.Equipment.Load(ctx, characterID)
	if err != nil {
		t.Fatalf("Load equipment error = %v", err)
	}
	if !ok {
		t.Fatalf("equipment %s not found", characterID)
	}
	return record
}

func saveTestInventory(t *testing.T, ctx context.Context, repos dnfrepo.Group, record dnfrepo.InventoryRecord) {
	t.Helper()
	if err := repos.Inventory.Save(ctx, record); err != nil {
		t.Fatalf("Save inventory error = %v", err)
	}
}

func loadTestInventory(t *testing.T, ctx context.Context, repos dnfrepo.Group, characterID string) dnfrepo.InventoryRecord {
	t.Helper()
	record, ok, err := repos.Inventory.Load(ctx, characterID)
	if err != nil {
		t.Fatalf("Load inventory error = %v", err)
	}
	if !ok {
		t.Fatalf("inventory %s not found", characterID)
	}
	return record
}

func saveTestPet(t *testing.T, ctx context.Context, repos dnfrepo.Group, record dnfrepo.PetRecord) {
	t.Helper()
	if err := repos.Pet.Save(ctx, record); err != nil {
		t.Fatalf("Save pet error = %v", err)
	}
}

func loadTestPet(t *testing.T, ctx context.Context, repos dnfrepo.Group, characterID string) dnfrepo.PetRecord {
	t.Helper()
	record, ok, err := repos.Pet.Load(ctx, characterID)
	if err != nil {
		t.Fatalf("Load pet error = %v", err)
	}
	if !ok {
		t.Fatalf("pet %s not found", characterID)
	}
	return record
}

func richTestPetEntry(key string, itemID int64, sourceListType byte, sourceSlot int16) dnfrepo.PetEntry {
	serial, _ := strconv.ParseUint(key, 10, 32)
	return dnfrepo.PetEntry{
		PetKey:          key,
		CreatureKey:     uint32(serial),
		ItemID:          itemID,
		SourceListType:  sourceListType,
		SourceSlotIndex: sourceSlot,
		Name:            "typed-growth-pet",
		NameRaw:         []byte{0xC4, 0xE3, 0xD7, 0xD6},
		Satiety:         73,
		ModeFlag:        1,
		Mode1Field0A:    0xA1,
		Mode1Field0B:    0xB2,
		Level:           19,
		Exp:             456789,
		TailFlag:        0x5A,
		RawEntry:        []byte{0x10, 0x20, 0x30},
		Extra:           map[string]string{"growth_evidence": "keep", "serial": key},
	}
}

func newValidatedTestOwner(repos dnfrepo.Group) (*Owner, error) {
	return NewOwnerWithPlacementValidator(repos, PlacementValidatorFunc(func(context.Context, Placement) error {
		return nil
	}))
}

type recordingPlacementValidator struct {
	placements []Placement
	err        error
}

func (v *recordingPlacementValidator) ValidateEquipmentPlacement(_ context.Context, placement Placement) error {
	v.placements = append(v.placements, placement)
	return v.err
}

func TestOwnerRepairAutoRepairFreeWithDevilContract(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestCharacter(t, ctx, repos, dnfrepo.CharacterRecord{CharacterID: "77", AccountID: "acc-1", Stats: map[string]int64{"gold": 9999}})
	saveTestEquipment(t, ctx, repos, dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"11": {SlotIndex: 11, ItemID: 700, RawEntry: []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 12, 0}},
		},
	})
	if err := repos.Account.Save(ctx, dnfrepo.AccountRecord{AccountID: "acc-1"}); err != nil {
		t.Fatal(err)
	}

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	// Auto-repair without the contract pays the normal formula cost (512).
	paid, err := owner.Repair(ctx, RepairCommand{SelectedCharacterID: 77, AccountID: "acc-1", SlotIndex: 11, AutoRepair: true}, repairTestResolver(20, 6400, 20))
	if err != nil {
		t.Fatalf("no-contract auto repair error = %v", err)
	}
	if paid.FreeRepair || paid.Cost != 512 || paid.UpdatedGold != 9487 {
		t.Fatalf("paid result = %+v, want cost=512 gold=9487", paid)
	}

	// Damage again, then the contract makes the same request free.
	damaged := loadTestEquipment(t, ctx, repos, "77")
	damagedEntry := damaged.Entries["11"]
	damagedEntry.RawEntry[10] = 12
	damagedEntry.RawEntry[11] = 0
	damaged.Entries["11"] = damagedEntry
	if err := repos.Equipment.Save(ctx, damaged); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(24 * time.Hour).Unix()
	if err := repos.Account.Save(ctx, dnfrepo.AccountRecord{
		AccountID: "acc-1",
		Metadata:  map[string]string{"premium_expire_586": strconv.FormatInt(future, 10)},
	}); err != nil {
		t.Fatal(err)
	}
	result, err := owner.Repair(ctx, RepairCommand{SelectedCharacterID: 77, AccountID: "acc-1", SlotIndex: 11, AutoRepair: true}, repairTestResolver(20, 6400, 20))
	if err != nil {
		t.Fatalf("contract auto repair error = %v", err)
	}
	if !result.FreeRepair || result.Cost != 0 || !result.Changed || result.NewDurability != 20 || result.UpdatedGold != 9487 {
		t.Fatalf("result = %+v", result)
	}
	loaded := loadTestEquipment(t, ctx, repos, "77")
	if got := uint16(loaded.Entries["11"].RawEntry[10]) | uint16(loaded.Entries["11"].RawEntry[11])<<8; got != 20 {
		t.Fatalf("raw durability = %d, want 20", got)
	}
	character, _, _ := repos.Character.Load(ctx, "77")
	if character.Stats["gold"] != 9487 {
		t.Fatalf("gold = %d, want 9487 (only the paid repair charged)", character.Stats["gold"])
	}

	// Manual repair (body[5]=0) must not consume the contract's free repair.
	damaged = loadTestEquipment(t, ctx, repos, "77")
	damagedEntry = damaged.Entries["11"]
	damagedEntry.RawEntry[10] = 12
	damagedEntry.RawEntry[11] = 0
	damaged.Entries["11"] = damagedEntry
	if err := repos.Equipment.Save(ctx, damaged); err != nil {
		t.Fatal(err)
	}
	manual, err := owner.Repair(ctx, RepairCommand{SelectedCharacterID: 77, AccountID: "acc-1", SlotIndex: 11}, repairTestResolver(20, 6400, 20))
	if err != nil {
		t.Fatalf("manual repair with contract error = %v", err)
	}
	if manual.FreeRepair || manual.Cost != 512 || manual.UpdatedGold != 8975 {
		t.Fatalf("manual result = %+v, want cost=512 gold=8975", manual)
	}
}

func repairAllTestResolver(types map[int64]string) alignedcmd.RepairCostResolver {
	return func(itemID int64) (alignedcmd.RepairCostEvidence, error) {
		equipmentType, ok := types[itemID]
		if !ok {
			return alignedcmd.RepairCostEvidence{MaxDurability: -1, RepairCostRate: 0.08, QuickRepairRate: 1.5, UpgradeRates: []float64{1, 1, 1}}, nil
		}
		return alignedcmd.RepairCostEvidence{
			EquipmentType:   equipmentType,
			MaxDurability:   20,
			RepairPrice:     6400,
			Grade:           20,
			RepairCostRate:  0.08,
			QuickRepairRate: 1.5,
			UpgradeRates:    []float64{1, 1, 1},
		}, nil
	}
}

func TestOwnerRepairAllRepairsEquippedAndQuickbarWithTypeFilter(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestCharacter(t, ctx, repos, dnfrepo.CharacterRecord{CharacterID: "77", Stats: map[string]int64{"gold": 5000}})
	saveTestEquipment(t, ctx, repos, dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"12": {SlotIndex: 12, ItemID: 700, RawEntry: []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 12, 0}}, // weapon, damaged
			"14": {SlotIndex: 14, ItemID: 701, RawEntry: []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 8, 0}},  // coat, damaged
			"13": {SlotIndex: 13, ItemID: 702, RawEntry: []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 1, 0}},  // title, damaged but ineligible
			"15": {SlotIndex: 15, ItemID: 703, RawEntry: []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 20, 0}}, // shoulder, full
		},
	})
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"0:4": {ItemID: 704, Count: 1, Extra: map[string]string{"durability": "10"}},
			"0:9": {ItemID: 705, Count: 1, Extra: map[string]string{"durability": "5"}},
		},
	})
	resolver := repairAllTestResolver(map[int64]string{
		700: "[weapon]",
		701: "[coat]",
		702: "[title name]",
		703: "[shoulder]",
		704: "[amulet]",
		705: "[weapon]",
	})

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	// weapon 12 lost 8 -> 512; coat 14 lost 12 -> 768; amulet slot4 lost 10
	// -> 640. Title is ineligible, shoulder is full, main slot 9 is outside
	// the quickbar scan: total = 1920.
	result, err := owner.RepairAll(ctx, RepairCommand{SelectedCharacterID: 77, SlotIndex: -1}, resolver)
	if err != nil {
		t.Fatalf("RepairAll error = %v", err)
	}
	if !result.Changed || result.RepairedCount != 3 || result.Cost != 1920 || result.UpdatedGold != 3080 {
		t.Fatalf("result = %+v, want count=3 cost=1920 gold=3080", result)
	}
	equipment := loadTestEquipment(t, ctx, repos, "77")
	for _, key := range []string{"12", "14"} {
		entry := equipment.Entries[key]
		if got := uint16(entry.RawEntry[10]) | uint16(entry.RawEntry[11])<<8; got != 20 {
			t.Fatalf("equipped %s durability = %d, want 20", key, got)
		}
	}
	if got := uint16(equipment.Entries["13"].RawEntry[10]) | uint16(equipment.Entries["13"].RawEntry[11])<<8; got != 1 {
		t.Fatalf("title durability = %d, want untouched 1", got)
	}
	inventory := loadTestInventory(t, ctx, repos, "77")
	if inventory.Slots["0:4"].Extra["durability"] != "20" {
		t.Fatalf("quickbar slot 4 durability = %q, want 20", inventory.Slots["0:4"].Extra["durability"])
	}
	if inventory.Slots["0:9"].Extra["durability"] != "5" {
		t.Fatalf("main slot 9 durability = %q, want untouched 5", inventory.Slots["0:9"].Extra["durability"])
	}
	character, _, _ := repos.Character.Load(ctx, "77")
	if character.Stats["gold"] != result.UpdatedGold {
		t.Fatalf("persisted gold = %d, want %d", character.Stats["gold"], result.UpdatedGold)
	}
}

func TestOwnerRepairAllQuickRateAndFreeRepair(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestCharacter(t, ctx, repos, dnfrepo.CharacterRecord{CharacterID: "77", AccountID: "acc-1", Stats: map[string]int64{"gold": 1000}})
	saveTestEquipment(t, ctx, repos, dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"12": {SlotIndex: 12, ItemID: 700, RawEntry: []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 12, 0}},
		},
	})
	if err := repos.Account.Save(ctx, dnfrepo.AccountRecord{AccountID: "acc-1"}); err != nil {
		t.Fatal(err)
	}
	resolver := repairAllTestResolver(map[int64]string{700: "[weapon]"})
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}

	// Quick repair-all: 512 * 1.5 = 768.
	quick, err := owner.RepairAll(ctx, RepairCommand{SelectedCharacterID: 77, AccountID: "acc-1", SlotIndex: -1, QuickRepair: true}, resolver)
	if err != nil {
		t.Fatalf("quick RepairAll error = %v", err)
	}
	if quick.Cost != 768 || quick.UpdatedGold != 232 || quick.RepairedCount != 1 {
		t.Fatalf("quick result = %+v, want cost=768", quick)
	}

	// Damage again; with the devil contract the auto repair-all is free.
	damaged := loadTestEquipment(t, ctx, repos, "77")
	entry := damaged.Entries["12"]
	entry.RawEntry[10] = 12
	entry.RawEntry[11] = 0
	damaged.Entries["12"] = entry
	if err := repos.Equipment.Save(ctx, damaged); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(24 * time.Hour).Unix()
	if err := repos.Account.Save(ctx, dnfrepo.AccountRecord{
		AccountID: "acc-1",
		Metadata:  map[string]string{"premium_expire_586": strconv.FormatInt(future, 10)},
	}); err != nil {
		t.Fatal(err)
	}
	free, err := owner.RepairAll(ctx, RepairCommand{SelectedCharacterID: 77, AccountID: "acc-1", SlotIndex: -1, AutoRepair: true}, resolver)
	if err != nil {
		t.Fatalf("free RepairAll error = %v", err)
	}
	if !free.FreeRepair || free.Cost != 0 || free.UpdatedGold != 232 || free.RepairedCount != 1 {
		t.Fatalf("free result = %+v, want cost=0", free)
	}
	character, _, _ := repos.Character.Load(ctx, "77")
	if character.Stats["gold"] != 232 {
		t.Fatalf("gold = %d, want 232 (only the quick repair charged)", character.Stats["gold"])
	}
}

func TestOwnerRepairAllNothingDamagedIsNoOp(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestCharacter(t, ctx, repos, dnfrepo.CharacterRecord{CharacterID: "77", Stats: map[string]int64{"gold": 777}})
	saveTestEquipment(t, ctx, repos, dnfrepo.EquipmentRecord{CharacterID: "77", Entries: map[string]dnfrepo.EquipmentEntry{}})
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{CharacterID: "77", Slots: map[string]dnfrepo.ItemStack{}})

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	result, err := owner.RepairAll(ctx, RepairCommand{SelectedCharacterID: 77, SlotIndex: -1}, repairAllTestResolver(nil))
	if err != nil {
		t.Fatalf("RepairAll error = %v", err)
	}
	if result.Changed || result.RepairedCount != 0 || result.Cost != 0 || result.UpdatedGold != 777 {
		t.Fatalf("result = %+v, want no-op", result)
	}
}
