package itemexpiration

import (
	"context"
	"errors"
	"testing"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestOwnerReconcileCommitsAllItemContainersAtomically(t *testing.T) {
	ctx := context.Background()
	repositories := seededRepositories(t, ctx)
	owner, err := NewOwner(repositories)
	if err != nil {
		t.Fatal(err)
	}
	updatedAt := time.Unix(2_000_000_000, 0).UTC()
	summary, err := owner.Reconcile(ctx, Command{
		AccountID:   "account-1",
		CharacterID: "19",
		UpdatedAt:   updatedAt,
		Project: func(assets *Assets) (Summary, error) {
			assets.Inventory.Slots["0:1"] = dnfrepo.ItemStack{ItemID: 101, Count: 1}
			assets.Inventory.Warehouse["2:1"] = dnfrepo.ItemStack{ItemID: 102, Count: 1}
			assets.AccountInventory.Slots["0:1"] = dnfrepo.ItemStack{ItemID: 103, Count: 1}
			assets.Equipment.Entries["0"] = dnfrepo.EquipmentEntry{SlotIndex: 0, ItemID: 104}
			return Summary{Inventory: 1, Warehouse: 1, Account: 1, Equipment: 1}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Total() != 4 {
		t.Fatalf("summary=%+v", summary)
	}

	inventory, found, err := repositories.Inventory.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load inventory found=%t err=%v", found, err)
	}
	if inventory.Slots["0:1"].ItemID != 101 || inventory.Warehouse["2:1"].ItemID != 102 || !inventory.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("inventory=%+v", inventory)
	}
	accountInventory, found, err := repositories.AccountInventory.Load(ctx, "account-1")
	if err != nil || !found {
		t.Fatalf("load account inventory found=%t err=%v", found, err)
	}
	if accountInventory.Slots["0:1"].ItemID != 103 || !accountInventory.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("account inventory=%+v", accountInventory)
	}
	equipment, found, err := repositories.Equipment.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load equipment found=%t err=%v", found, err)
	}
	if equipment.Entries["0"].ItemID != 104 || !equipment.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("equipment=%+v", equipment)
	}
}

func TestOwnerReconcileRollsBackProjectorFailure(t *testing.T) {
	ctx := context.Background()
	repositories := seededRepositories(t, ctx)
	owner, err := NewOwner(repositories)
	if err != nil {
		t.Fatal(err)
	}
	projectErr := errors.New("projection failed")
	summary, err := owner.Reconcile(ctx, Command{
		AccountID:   "account-1",
		CharacterID: "19",
		Project: func(assets *Assets) (Summary, error) {
			assets.Inventory.Slots["0:1"] = dnfrepo.ItemStack{ItemID: 201, Count: 1}
			assets.AccountInventory.Slots["0:1"] = dnfrepo.ItemStack{ItemID: 202, Count: 1}
			assets.Equipment.Entries["0"] = dnfrepo.EquipmentEntry{SlotIndex: 0, ItemID: 203}
			return Summary{Inventory: 1, Account: 1, Equipment: 1}, projectErr
		},
	})
	if !errors.Is(err, projectErr) {
		t.Fatalf("err=%v", err)
	}
	if summary.Total() != 3 {
		t.Fatalf("summary=%+v", summary)
	}

	inventory, _, _ := repositories.Inventory.Load(ctx, "19")
	accountInventory, _, _ := repositories.AccountInventory.Load(ctx, "account-1")
	equipment, _, _ := repositories.Equipment.Load(ctx, "19")
	if inventory.Slots["0:1"].ItemID != 1 || accountInventory.Slots["0:1"].ItemID != 2 || equipment.Entries["0"].ItemID != 3 {
		t.Fatalf("rollback failed inventory=%+v account=%+v equipment=%+v", inventory, accountInventory, equipment)
	}
}

func TestOwnerReconcileRejectsCrossAccountCharacter(t *testing.T) {
	ctx := context.Background()
	repositories := seededRepositories(t, ctx)
	owner, err := NewOwner(repositories)
	if err != nil {
		t.Fatal(err)
	}
	projected := false
	_, err = owner.Reconcile(ctx, Command{
		AccountID:   "account-2",
		CharacterID: "19",
		Project: func(*Assets) (Summary, error) {
			projected = true
			return Summary{}, nil
		},
	})
	if !errors.Is(err, ErrAccountMismatch) {
		t.Fatalf("err=%v", err)
	}
	if projected {
		t.Fatal("cross-account request reached projector")
	}
}

func seededRepositories(t *testing.T, ctx context.Context) dnfrepo.Group {
	t.Helper()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{
		AccountID:   "account-1",
		CharacterID: "19",
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots:       map[string]dnfrepo.ItemStack{"0:1": {ItemID: 1, Count: 1}},
		Warehouse:   map[string]dnfrepo.ItemStack{"2:1": {ItemID: 1, Count: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.AccountInventory.Save(ctx, dnfrepo.AccountInventoryRecord{
		AccountID: "account-1",
		Slots:     map[string]dnfrepo.ItemStack{"0:1": {ItemID: 2, Count: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Equipment.Save(ctx, dnfrepo.EquipmentRecord{
		CharacterID: "19",
		Entries:     map[string]dnfrepo.EquipmentEntry{"0": {SlotIndex: 0, ItemID: 3}},
	}); err != nil {
		t.Fatal(err)
	}
	return repositories
}
