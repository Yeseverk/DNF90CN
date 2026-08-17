package packageitem

import (
	"context"
	"errors"
	"testing"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestOwnerPlanSelectableReadsSourceSlot(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "81",
		Slots: map[string]dnfrepo.ItemStack{
			"0:5": {ItemID: 7001, Count: 1},
		},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner() error = %v", err)
	}
	got, err := owner.PlanSelectable(ctx, SelectableCommand{
		SelectedCharacterID:    81,
		SlotIndex:              5,
		SelectedItemTemplateID: 9001,
	})
	if err != nil {
		t.Fatalf("PlanSelectable() error = %v", err)
	}
	if got.SourceItemID != 7001 || got.SelectedItemTemplateID != 9001 || got.SourceSlotIndex != 5 {
		t.Fatalf("result = %+v", got)
	}
}

func TestOwnerPlanMagicBoxChecksMaterialSlot(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "82",
		Slots: map[string]dnfrepo.ItemStack{
			"0:6": {ItemID: 8001, Count: 1},
			"0:7": {ItemID: 8002, Count: 1},
		},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner() error = %v", err)
	}
	got, err := owner.PlanMagicBox(ctx, MagicBoxCommand{
		SelectedCharacterID: 82,
		ListType:            listTypeMain,
		SlotIndex:           6,
		MaterialSlotIndex:   7,
	})
	if err != nil {
		t.Fatalf("PlanMagicBox() error = %v", err)
	}
	if got.SourceItemID != 8001 || got.MaterialItemID != 8002 || got.MaterialSlotIndex != 7 {
		t.Fatalf("result = %+v", got)
	}
}

func TestOwnerPlanMagicBoxRejectsMissingMaterial(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "83",
		Slots: map[string]dnfrepo.ItemStack{
			"0:6": {ItemID: 8001, Count: 1},
		},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner() error = %v", err)
	}
	_, err = owner.PlanMagicBox(ctx, MagicBoxCommand{
		SelectedCharacterID: 83,
		ListType:            listTypeMain,
		SlotIndex:           6,
		MaterialSlotIndex:   7,
	})
	if !errors.Is(err, ErrMaterialNotFound) {
		t.Fatalf("PlanMagicBox() error = %v, want ErrMaterialNotFound", err)
	}
}
