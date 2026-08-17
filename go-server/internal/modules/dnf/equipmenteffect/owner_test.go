package equipmenteffect

import (
	"context"
	"errors"
	"testing"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

type catalogStub map[uint32]ItemDefinition

func (c catalogStub) ResolveEquipmentEffectItem(itemID uint32) (ItemDefinition, error) {
	definition, found := c[itemID]
	if !found {
		return ItemDefinition{}, errors.New("item missing")
	}
	return definition, nil
}

func TestOwnerApplyRecoversOneRuneFromStaleRequestedSlot(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "1",
		Slots: map[string]dnfrepo.ItemStack{
			"0:11": {ItemID: 101, Count: 1},
			"0:81": {ItemID: 201, Count: 18},
			"0:83": {ItemID: 301, Count: 1},
		},
	}); err != nil {
		t.Fatal(err)
	}
	owner, err := NewOwner(repositories, catalogStub{
		101: {IsEquipment: true, EquipmentType: "[weapon]", Grade: 96},
		201: {StackableType: "[etc]"},
		301: {StackableType: "[equipment effect]", EffectID: 1},
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := owner.Apply(ctx, Command{
		CharacterID: "1", RequestedSourceSlot: 81, TargetListType: 0, TargetSlot: 11,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !result.SourceRecovered || result.SourceSlot != 83 || result.SourceItemID != 301 ||
		!result.SourceRemoved || result.EffectID != 1 || result.TargetItemID != 101 {
		t.Fatalf("result = %+v", result)
	}
	inventory, found, err := repositories.Inventory.Load(ctx, "1")
	if err != nil || !found {
		t.Fatalf("load inventory found=%t err=%v", found, err)
	}
	if _, found := inventory.Slots["0:83"]; found {
		t.Fatalf("rune remains in inventory: %+v", inventory.Slots["0:83"])
	}
	if inventory.Slots["0:81"].Count != 18 {
		t.Fatalf("non-rune source was changed: %+v", inventory.Slots["0:81"])
	}
	if got := inventory.Slots["0:11"].Extra["equipment_effect_id"]; got != "1" {
		t.Fatalf("target effect id = %q", got)
	}
}

func TestOwnerApplyRejectsAmbiguousRuneFallbackWithoutMutation(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "1",
		Slots: map[string]dnfrepo.ItemStack{
			"0:11": {ItemID: 101, Count: 1},
			"0:81": {ItemID: 201, Count: 1},
			"0:83": {ItemID: 301, Count: 1},
			"0:84": {ItemID: 302, Count: 1},
		},
	}); err != nil {
		t.Fatal(err)
	}
	owner, err := NewOwner(repositories, catalogStub{
		101: {IsEquipment: true, EquipmentType: "[weapon]", Grade: 96},
		201: {StackableType: "[etc]"},
		301: {StackableType: "[equipment effect]", EffectID: 1},
		302: {StackableType: "[equipment effect]", EffectID: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = owner.Apply(ctx, Command{CharacterID: "1", RequestedSourceSlot: 81, TargetListType: 0, TargetSlot: 11})
	if !errors.Is(err, ErrSourceAmbiguous) {
		t.Fatalf("Apply error = %v", err)
	}
	inventory, _, err := repositories.Inventory.Load(ctx, "1")
	if err != nil {
		t.Fatal(err)
	}
	if inventory.Slots["0:83"].Count != 1 || inventory.Slots["0:84"].Count != 1 ||
		inventory.Slots["0:11"].Extra["equipment_effect_id"] != "" {
		t.Fatalf("inventory mutated on rejected request: %+v", inventory.Slots)
	}
}

func TestOwnerApplyRejectsNonWeaponBeforeConsumingRune(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "1",
		Slots: map[string]dnfrepo.ItemStack{
			"0:11": {ItemID: 101, Count: 1},
			"0:83": {ItemID: 301, Count: 1},
		},
	}); err != nil {
		t.Fatal(err)
	}
	owner, err := NewOwner(repositories, catalogStub{
		101: {IsEquipment: true, EquipmentType: "[ring]", Grade: 96},
		301: {StackableType: "[equipment effect]", EffectID: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = owner.Apply(ctx, Command{CharacterID: "1", RequestedSourceSlot: 83, TargetListType: 0, TargetSlot: 11})
	if !errors.Is(err, ErrTargetInvalid) {
		t.Fatalf("Apply error = %v", err)
	}
	inventory, _, err := repositories.Inventory.Load(ctx, "1")
	if err != nil {
		t.Fatal(err)
	}
	if inventory.Slots["0:83"].Count != 1 {
		t.Fatalf("rune was consumed for invalid target")
	}
}
