package avatartitle

import (
	"context"
	"errors"
	"testing"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestOwnerPlansAvatarSocketFromRepositories(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "88", AccountID: "acc"}); err != nil {
		t.Fatalf("Character.Save error = %v", err)
	}
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "88",
		Slots: map[string]dnfrepo.ItemStack{
			"1:5": {ItemID: 0x11223344, Count: 1},
			"0:6": {ItemID: 7001, Count: 1},
		},
	}); err != nil {
		t.Fatalf("Inventory.Save error = %v", err)
	}
	if err := repos.Equipment.Save(ctx, dnfrepo.EquipmentRecord{
		CharacterID: "88",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"0": {SlotIndex: 0, ItemID: 9001},
		},
	}); err != nil {
		t.Fatalf("Equipment.Save error = %v", err)
	}

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	got, err := owner.Plan(ctx, Command{
		Operation:           "add_avatar_socket",
		SelectedCharacterID: 88,
		TargetSlot:          5,
		TargetItemID:        0x11223344,
		MaterialSlot:        6,
	})
	if err != nil {
		t.Fatalf("Plan error = %v", err)
	}
	if got.AccountID != "acc" || got.CharacterID != "88" {
		t.Fatalf("identity = (%q,%q)", got.AccountID, got.CharacterID)
	}
	if !got.TargetFound || got.TargetItemID != 0x11223344 {
		t.Fatalf("target = found %t item %d", got.TargetFound, got.TargetItemID)
	}
	if !got.MaterialFound || got.MaterialItemID != 7001 {
		t.Fatalf("material = found %t item %d", got.MaterialFound, got.MaterialItemID)
	}
	if !got.EquipmentKnown || got.EquipmentEntryCount != 1 {
		t.Fatalf("equipment = known %t count %d", got.EquipmentKnown, got.EquipmentEntryCount)
	}
}

func TestOwnerRejectsMismatchedAvatarTarget(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "88", AccountID: "acc"}); err != nil {
		t.Fatalf("Character.Save error = %v", err)
	}
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "88",
		Slots: map[string]dnfrepo.ItemStack{
			"1:5": {ItemID: 1001, Count: 1},
			"0:6": {ItemID: 7001, Count: 1},
		},
	}); err != nil {
		t.Fatalf("Inventory.Save error = %v", err)
	}
	if err := repos.Equipment.Save(ctx, dnfrepo.EquipmentRecord{CharacterID: "88"}); err != nil {
		t.Fatalf("Equipment.Save error = %v", err)
	}

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	_, err = owner.Plan(ctx, Command{
		Operation:           "add_avatar_socket",
		SelectedCharacterID: 88,
		TargetSlot:          5,
		TargetItemID:        0x11223344,
		MaterialSlot:        6,
	})
	if !errors.Is(err, ErrItemMismatch) {
		t.Fatalf("Plan error = %v, want ErrItemMismatch", err)
	}
}
