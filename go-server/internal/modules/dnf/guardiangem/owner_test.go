package guardiangem

import (
	"context"
	"errors"
	"testing"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestOwnerInsertCommitsInventoryAndEquipmentAtomically(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	seedGuardianGemOwner(t, ctx, repositories)
	owner, err := NewOwner(repositories)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}

	err = owner.Insert(ctx, Command{
		CharacterID: "77",
		Project: func(assets *Assets) (Changes, error) {
			source := assets.Inventory.Slots["38:49"]
			source.Count--
			assets.Inventory.Slots["38:49"] = source
			target := assets.Equipment.Entries["32"]
			target.Extra = map[string]string{"raw_data_65": "socket"}
			assets.Equipment.Entries["32"] = target
			return Changes{InventorySlots: true, Equipment: true}, nil
		},
	})
	if err != nil {
		t.Fatalf("Insert error = %v", err)
	}
	inventory, _, _ := repositories.Inventory.Load(ctx, "77")
	equipment, _, _ := repositories.Equipment.Load(ctx, "77")
	if inventory.Slots["38:49"].Count != 1 || equipment.Entries["32"].Extra["raw_data_65"] != "socket" {
		t.Fatalf("inventory=%+v equipment=%+v", inventory, equipment)
	}
}

func TestOwnerInsertRollsBackProjectedContainers(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	seedGuardianGemOwner(t, ctx, repositories)
	owner, err := NewOwner(repositories)
	if err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("reject guardian gem projection")
	err = owner.Insert(ctx, Command{
		CharacterID: "77",
		Project: func(assets *Assets) (Changes, error) {
			delete(assets.Inventory.Slots, "38:49")
			delete(assets.Equipment.Entries, "32")
			return Changes{InventorySlots: true, Equipment: true}, wantErr
		},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Insert error = %v, want %v", err, wantErr)
	}
	inventory, _, _ := repositories.Inventory.Load(ctx, "77")
	equipment, _, _ := repositories.Equipment.Load(ctx, "77")
	if inventory.Slots["38:49"].Count != 2 || equipment.Entries["32"].ItemID != 100380017 {
		t.Fatalf("rollback inventory=%+v equipment=%+v", inventory, equipment)
	}
}

func TestOwnerInsertRejectsMissingInventoryBeforeProjection(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	owner, err := NewOwner(repositories)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	err = owner.Insert(ctx, Command{
		CharacterID: "77",
		Project: func(*Assets) (Changes, error) {
			called = true
			return Changes{}, nil
		},
	})
	if !errors.Is(err, ErrInventoryMissing) || called {
		t.Fatalf("Insert error=%v called=%t", err, called)
	}
}

func seedGuardianGemOwner(t *testing.T, ctx context.Context, repositories dnfrepo.Group) {
	t.Helper()
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"38:49": {ItemID: 90002, Count: 2},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Equipment.Save(ctx, dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"32": {ItemID: 100380017, SlotIndex: 32},
		},
	}); err != nil {
		t.Fatal(err)
	}
}
