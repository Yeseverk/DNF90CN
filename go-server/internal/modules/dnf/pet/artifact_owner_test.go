package pet

import (
	"context"
	"errors"
	"testing"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

type fakeArtifactResolver map[int64]PetArtifactKind

func (resolver fakeArtifactResolver) ResolveArtifact(itemID int64) (PetArtifactDefinition, error) {
	kind, ok := resolver[itemID]
	if !ok {
		return PetArtifactDefinition{}, ErrPetPVFArtifactTypeInvalid
	}
	return PetArtifactDefinition{ItemID: itemID, Kind: kind}, nil
}

func TestArtifactOwnerEquipSwapAndUnequipAreAtomic(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "71",
		Slots: map[string]dnfrepo.ItemStack{
			"7:140": {ItemID: 63500, Count: 1, RawEntry: []byte{1}, Extra: map[string]string{"serial": "100"}},
			"7:141": {ItemID: 63501, Count: 1, RawEntry: []byte{2}, Extra: map[string]string{"serial": "101"}},
			"7:142": {ItemID: 64000, Count: 1, RawEntry: []byte{3}, Extra: map[string]string{"serial": "102"}},
		},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	resolver := fakeArtifactResolver{
		63500: PetArtifactKindRed,
		63501: PetArtifactKindRed,
		64000: PetArtifactKindBlue,
	}
	owner, err := NewArtifactOwner(repos, resolver)
	if err != nil {
		t.Fatalf("NewArtifactOwner: %v", err)
	}

	first, err := owner.Equip(ctx, ArtifactEquipCommand{SelectedCharacterID: 71, ListType: listTypePet, SlotIndex: 140})
	if err != nil {
		t.Fatalf("first Equip: %v", err)
	}
	if !first.Changed || first.Kind != PetArtifactKindRed || first.ItemID != 63500 || first.SwappedItem != 0 {
		t.Fatalf("first result = %+v", first)
	}

	swap, err := owner.Equip(ctx, ArtifactEquipCommand{SelectedCharacterID: 71, ListType: listTypePet, SlotIndex: 141})
	if err != nil {
		t.Fatalf("swap Equip: %v", err)
	}
	if swap.ItemID != 63501 || swap.SwappedItem != 63500 {
		t.Fatalf("swap result = %+v", swap)
	}
	if stack := swap.PetInventory["7:141"]; stack.ItemID != 63500 || stack.Extra["serial"] != "100" || string(stack.RawEntry) != string([]byte{1}) {
		t.Fatalf("swapped source = %+v", stack)
	}

	if _, err := owner.Equip(ctx, ArtifactEquipCommand{SelectedCharacterID: 71, ListType: listTypePet, SlotIndex: 142}); err != nil {
		t.Fatalf("blue Equip: %v", err)
	}
	record, ok, err := repos.Pet.Load(ctx, "71")
	if err != nil || !ok {
		t.Fatalf("load pets ok=%v err=%v", ok, err)
	}
	ids, err := EquippedArtifactItemIDs(record)
	if err != nil || len(ids) != 2 || ids[0] != 63501 || ids[1] != 64000 {
		t.Fatalf("artifact ids=%v err=%v record=%+v", ids, err, record.Artifacts)
	}

	unequipped, err := owner.Unequip(ctx, ArtifactUnequipCommand{
		SelectedCharacterID: 71,
		ListType:            listTypePet,
		SlotIndex:           150,
		Kind:                PetArtifactKindRed,
	})
	if err != nil {
		t.Fatalf("Unequip: %v", err)
	}
	if unequipped.ItemID != 63501 || unequipped.PetInventory["7:150"].Extra["serial"] != "101" {
		t.Fatalf("unequip result = %+v", unequipped)
	}
	record, _, _ = repos.Pet.Load(ctx, "71")
	if _, exists := record.Artifacts["red"]; exists || record.Artifacts["blue"].ItemID != 64000 {
		t.Fatalf("artifacts after unequip = %+v", record.Artifacts)
	}
}

func TestArtifactOwnerRejectsWrongRangeAndCorruptStateWithoutMutation(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "72",
		Slots: map[string]dnfrepo.ItemStack{
			"7:140": {ItemID: 63500, Count: 1},
		},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	resolver := fakeArtifactResolver{63500: PetArtifactKindRed, 64000: PetArtifactKindBlue}
	owner, _ := NewArtifactOwner(repos, resolver)

	if _, err := owner.Equip(ctx, ArtifactEquipCommand{SelectedCharacterID: 72, ListType: listTypePet, SlotIndex: 139}); !errors.Is(err, ErrSlotNotFound) {
		t.Fatalf("wrong range error = %v", err)
	}
	if err := repos.Pet.Save(ctx, dnfrepo.PetRecord{
		CharacterID: "72",
		Artifacts: map[string]dnfrepo.ItemStack{
			"red": {ItemID: 64000, Count: 1},
		},
	}); err != nil {
		t.Fatalf("save corrupt pet state: %v", err)
	}
	if _, err := owner.Equip(ctx, ArtifactEquipCommand{SelectedCharacterID: 72, ListType: listTypePet, SlotIndex: 140}); !errors.Is(err, ErrPetArtifactStateInvalid) {
		t.Fatalf("corrupt state error = %v", err)
	}
	inventory, _, _ := repos.Inventory.Load(ctx, "72")
	if inventory.Slots["7:140"].ItemID != 63500 {
		t.Fatalf("source mutated after failure: %+v", inventory.Slots)
	}
}

func TestArtifactOwnerRejectsOccupiedUnequipTarget(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "73",
		Slots:       map[string]dnfrepo.ItemStack{"7:150": {ItemID: 999, Count: 1}},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	if err := repos.Pet.Save(ctx, dnfrepo.PetRecord{
		CharacterID: "73",
		Artifacts:   map[string]dnfrepo.ItemStack{"green": {ItemID: 64500, Count: 1}},
	}); err != nil {
		t.Fatalf("save pets: %v", err)
	}
	owner, _ := NewArtifactOwner(repos, fakeArtifactResolver{64500: PetArtifactKindGreen})
	_, err := owner.Unequip(ctx, ArtifactUnequipCommand{
		SelectedCharacterID: 73,
		ListType:            listTypePet,
		SlotIndex:           150,
		Kind:                PetArtifactKindGreen,
	})
	if !errors.Is(err, ErrPetArtifactTargetOccupied) {
		t.Fatalf("Unequip error = %v", err)
	}
	record, _, _ := repos.Pet.Load(ctx, "73")
	if record.Artifacts["green"].ItemID != 64500 {
		t.Fatalf("artifact mutated after failure: %+v", record.Artifacts)
	}
}
