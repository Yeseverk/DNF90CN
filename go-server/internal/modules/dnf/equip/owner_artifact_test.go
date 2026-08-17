package equip

import (
	"context"
	"testing"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestOwnerMoveEquipsArtifactToColorTarget(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"7:140": {ItemID: 2747155, Count: 1, RawEntry: make([]byte, 0x77), Extra: map[string]string{"item_kind": "equipment", "pvf_path": "equipment/creature/artifact_red/china_artifact_R1.equ"}},
		},
	})
	saveTestEquipment(t, ctx, repos, dnfrepo.EquipmentRecord{CharacterID: "77", Entries: map[string]dnfrepo.EquipmentEntry{}})

	validator := &recordingPlacementValidator{}
	owner, err := NewOwnerWithPlacementValidator(repos, validator)
	if err != nil {
		t.Fatalf("NewOwnerWithPlacementValidator error = %v", err)
	}
	result, err := owner.Move(ctx, MoveCommand{
		SelectedCharacterID:  77,
		SourceListType:       wireListPet,
		SourceSlotIndex:      140,
		MoveCount:            1,
		DestinationListType:  wireListEquipment,
		DestinationSlotIndex: 27,
	})
	if err != nil {
		t.Fatalf("Move error = %v", err)
	}
	if result.Mode != "equip" || result.ItemID != 2747155 || !result.Changed {
		t.Fatalf("result = %+v", result)
	}
	if len(validator.placements) != 1 || validator.placements[0].ItemID != 2747155 || validator.placements[0].TargetSlotIndex != 27 {
		t.Fatalf("artifact must pass PVF placement validation: %+v", validator.placements)
	}
	if _, ok := loadTestInventory(t, ctx, repos, "77").Slots["7:140"]; ok {
		t.Fatalf("artifact source slot should be empty")
	}
	entry := loadTestEquipment(t, ctx, repos, "77").Entries["27"]
	if entry.ItemID != 2747155 || entry.SlotIndex != 27 {
		t.Fatalf("worn artifact entry = %+v", entry)
	}
	petRecord, ok, err := repos.Pet.Load(ctx, "77")
	if err != nil || !ok || petRecord.Artifacts["red"].ItemID != 2747155 {
		t.Fatalf("artifact projection ok=%t err=%v record=%+v", ok, err, petRecord)
	}
}

func TestOwnerMoveUnequipsArtifactToSharedRange(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{CharacterID: "77", Slots: map[string]dnfrepo.ItemStack{}})
	saveTestEquipment(t, ctx, repos, dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"28": {SlotIndex: 28, ItemID: 2747156, RawEntry: make([]byte, 0x77)},
		},
	})
	if err := repos.Pet.Save(ctx, dnfrepo.PetRecord{
		CharacterID: "77",
		Artifacts: map[string]dnfrepo.ItemStack{
			"blue": {ItemID: 2747156, Count: 1, RawEntry: make([]byte, 0x77)},
		},
	}); err != nil {
		t.Fatalf("Save pet error = %v", err)
	}

	owner, err := NewOwnerWithPlacementValidator(repos, &recordingPlacementValidator{})
	if err != nil {
		t.Fatalf("NewOwnerWithPlacementValidator error = %v", err)
	}
	result, err := owner.Move(ctx, MoveCommand{
		SelectedCharacterID:  77,
		SourceListType:       wireListEquipment,
		SourceSlotIndex:      28,
		MoveCount:            1,
		DestinationListType:  wireListPet,
		DestinationSlotIndex: 140,
	})
	if err != nil {
		t.Fatalf("Move error = %v", err)
	}
	if result.Mode != "unequip" || !result.Changed {
		t.Fatalf("result = %+v", result)
	}
	if _, ok := loadTestEquipment(t, ctx, repos, "77").Entries["28"]; ok {
		t.Fatalf("worn artifact slot should be empty")
	}
	stack, ok := loadTestInventory(t, ctx, repos, "77").Slots["7:140"]
	if !ok || stack.ItemID != 2747156 || stack.Count != 1 {
		t.Fatalf("returned artifact stack = %+v found=%t", stack, ok)
	}
	petRecord, ok, err := repos.Pet.Load(ctx, "77")
	if err != nil || !ok {
		t.Fatalf("load pet ok=%t err=%v", ok, err)
	}
	if _, exists := petRecord.Artifacts["blue"]; exists {
		t.Fatalf("blue artifact projection survived unequip: %+v", petRecord.Artifacts)
	}
}

func TestOwnerMoveSwapsArtifactAndWritesDisplacedBack(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"7:140": {ItemID: 2747157, Count: 1, RawEntry: make([]byte, 0x77), Extra: map[string]string{"item_kind": "equipment"}},
		},
	})
	saveTestEquipment(t, ctx, repos, dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"27": {SlotIndex: 27, ItemID: 2747155, RawEntry: make([]byte, 0x77)},
		},
	})
	if err := repos.Pet.Save(ctx, dnfrepo.PetRecord{
		CharacterID: "77",
		Artifacts: map[string]dnfrepo.ItemStack{
			"red": {ItemID: 2747155, Count: 1, RawEntry: make([]byte, 0x77)},
		},
	}); err != nil {
		t.Fatalf("Save pet error = %v", err)
	}

	owner, err := NewOwnerWithPlacementValidator(repos, &recordingPlacementValidator{})
	if err != nil {
		t.Fatalf("NewOwnerWithPlacementValidator error = %v", err)
	}
	result, err := owner.Move(ctx, MoveCommand{
		SelectedCharacterID:  77,
		SourceListType:       wireListPet,
		SourceSlotIndex:      140,
		MoveCount:            1,
		DestinationListType:  wireListEquipment,
		DestinationSlotIndex: 27,
	})
	if err != nil {
		t.Fatalf("Move error = %v", err)
	}
	if result.Mode != "equip_swap" || result.SwappedItemID != 2747155 || !result.Changed {
		t.Fatalf("result = %+v", result)
	}
	entry := loadTestEquipment(t, ctx, repos, "77").Entries["27"]
	if entry.ItemID != 2747157 {
		t.Fatalf("worn artifact after swap = %+v", entry)
	}
	stack, ok := loadTestInventory(t, ctx, repos, "77").Slots["7:140"]
	if !ok || stack.ItemID != 2747155 {
		t.Fatalf("displaced artifact stack = %+v found=%t", stack, ok)
	}
	petRecord, ok, err := repos.Pet.Load(ctx, "77")
	if err != nil || !ok || petRecord.Artifacts["red"].ItemID != 2747157 {
		t.Fatalf("swapped artifact projection ok=%t err=%v record=%+v", ok, err, petRecord)
	}
}

func TestOwnerMoveArtifactRejectsValidatorMismatchWithoutMutation(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"7:140": {ItemID: 2747155, Count: 1, RawEntry: make([]byte, 0x77)},
		},
	})
	saveTestEquipment(t, ctx, repos, dnfrepo.EquipmentRecord{CharacterID: "77", Entries: map[string]dnfrepo.EquipmentEntry{}})

	validator := &recordingPlacementValidator{err: errArtifactColorMismatchForTest}
	owner, err := NewOwnerWithPlacementValidator(repos, validator)
	if err != nil {
		t.Fatalf("NewOwnerWithPlacementValidator error = %v", err)
	}
	if _, err := owner.Move(ctx, MoveCommand{
		SelectedCharacterID:  77,
		SourceListType:       wireListPet,
		SourceSlotIndex:      140,
		MoveCount:            1,
		DestinationListType:  wireListEquipment,
		DestinationSlotIndex: 28,
	}); err == nil {
		t.Fatalf("red artifact to blue target must be rejected")
	}
	if stack := loadTestInventory(t, ctx, repos, "77").Slots["7:140"]; stack.Count != 1 {
		t.Fatalf("rejected artifact move mutated inventory: %+v", stack)
	}
	if _, ok := loadTestEquipment(t, ctx, repos, "77").Entries["28"]; ok {
		t.Fatalf("rejected artifact move mutated equipment")
	}
}

func TestOwnerMoveArtifactRejectsCreatureSourceRange(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"7:139": {ItemID: 2747155, Count: 1, RawEntry: make([]byte, 0x77)},
		},
	})
	saveTestEquipment(t, ctx, repos, dnfrepo.EquipmentRecord{CharacterID: "77", Entries: map[string]dnfrepo.EquipmentEntry{}})

	owner, err := NewOwnerWithPlacementValidator(repos, &recordingPlacementValidator{})
	if err != nil {
		t.Fatalf("NewOwnerWithPlacementValidator error = %v", err)
	}
	if _, err := owner.Move(ctx, MoveCommand{
		SelectedCharacterID:  77,
		SourceListType:       wireListPet,
		SourceSlotIndex:      139,
		MoveCount:            1,
		DestinationListType:  wireListEquipment,
		DestinationSlotIndex: 27,
	}); err == nil {
		t.Fatalf("creature source range to artifact target must be rejected")
	}
}

var errArtifactColorMismatchForTest = errorString("artifact color target mismatch")

type errorString string

func (e errorString) Error() string { return string(e) }
