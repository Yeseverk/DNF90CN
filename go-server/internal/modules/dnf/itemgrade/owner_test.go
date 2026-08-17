package itemgrade

import (
	"context"
	"errors"
	"testing"

	"longheng.io/server/internal/modules/dnf/itemquality"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

type catalogStub map[uint32]ItemDefinition

func (c catalogStub) ResolveItem(itemID uint32) (ItemDefinition, error) {
	definition, ok := c[itemID]
	if !ok {
		return ItemDefinition{}, errors.New("missing item")
	}
	return definition, nil
}

func TestOwnerAdjustPersistsSeedAndConsumesMaterial(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			"0:42": {ItemID: 1001, Count: 1},
			"0:75": {ItemID: 2001, Count: 2},
		},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	owner, err := NewOwner(repos, nil, func() (uint32, error) { return 123456, nil })
	if err != nil {
		t.Fatalf("NewOwner: %v", err)
	}
	result, err := owner.Adjust(ctx, Command{
		SelectedCharacterID: 19,
		TargetSlot:          42,
		TargetItemID:        1001,
		MaterialSlot:        75,
	})
	if err != nil {
		t.Fatalf("Adjust: %v", err)
	}
	if result.NewSeed != 123456 || result.GoldKaleido ||
		result.MaterialRemaining != 1 || result.MaterialRemoved {
		t.Fatalf("result = %+v", result)
	}
	inventory, _, err := repos.Inventory.Load(ctx, "19")
	if err != nil {
		t.Fatalf("load inventory: %v", err)
	}
	if inventory.Slots["0:42"].Extra["quality_seed"] != "123456" ||
		inventory.Slots["0:42"].Extra["item_kind"] != "equipment" ||
		inventory.Slots["0:75"].Count != 1 {
		t.Fatalf("inventory = %+v", inventory.Slots)
	}
}

func TestOwnerAdjustGoldKaleidoUsesTopSeed(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			"0:42": {ItemID: 1001, Count: 1},
			"0:75": {ItemID: 2001, Count: 1},
		},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	owner, err := NewOwner(repos, catalogStub{
		2001: {StackableType: "`gold kaleido`"},
	}, func() (uint32, error) {
		t.Fatal("random seed generator called for gold kaleido")
		return 0, nil
	})
	if err != nil {
		t.Fatalf("NewOwner: %v", err)
	}
	result, err := owner.Adjust(ctx, Command{
		SelectedCharacterID: 19,
		TargetSlot:          42,
		TargetItemID:        1001,
		MaterialSlot:        75,
	})
	if err != nil {
		t.Fatalf("Adjust: %v", err)
	}
	if !result.GoldKaleido || result.NewSeed != itemquality.TopSeed ||
		!result.MaterialRemoved {
		t.Fatalf("result = %+v", result)
	}
	if result.TargetStack.Extra["item_kind"] != "equipment" {
		t.Fatalf("gold target extra=%v", result.TargetStack.Extra)
	}
}
