package npcshop

import (
	"context"
	"errors"
	"testing"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestOwnerMutateCommitsSelectedNPCShopAssets(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	seedNPCShopOwner(t, ctx, repositories)
	owner, err := NewOwner(repositories)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}

	err = owner.Mutate(ctx, Command{
		AccountID:   "dnf:1",
		CharacterID: "77",
		Project: func(assets *Assets) (Changes, error) {
			assets.Character.Stats["gold"] = 700
			assets.Inventory.Slots["0:10"] = dnfrepo.ItemStack{ItemID: 600, Count: 3}
			assets.AccountInventory.Slots[dnfrepo.AccountSharedInventorySlotKey(3)] = dnfrepo.ItemStack{ItemID: 3033, Count: 2}
			return Changes{AccountInventory: true, Character: true, Inventory: true}, nil
		},
	})
	if err != nil {
		t.Fatalf("Mutate error = %v", err)
	}

	character, found, err := repositories.Character.Load(ctx, "77")
	if err != nil || !found || character.Stats["gold"] != 700 {
		t.Fatalf("character = %+v found=%t error=%v", character, found, err)
	}
	inventory, found, err := repositories.Inventory.Load(ctx, "77")
	if err != nil || !found || inventory.Slots["0:10"].Count != 3 {
		t.Fatalf("inventory = %+v found=%t error=%v", inventory, found, err)
	}
	account, found, err := repositories.AccountInventory.Load(ctx, "dnf:1")
	if err != nil || !found || account.Slots[dnfrepo.AccountSharedInventorySlotKey(3)].Count != 2 {
		t.Fatalf("account inventory = %+v found=%t error=%v", account, found, err)
	}
}

func TestOwnerMutateRejectsAccountMismatchBeforeProjection(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	seedNPCShopOwner(t, ctx, repositories)
	owner, err := NewOwner(repositories)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	called := false
	err = owner.Mutate(ctx, Command{
		AccountID:   "dnf:2",
		CharacterID: "77",
		Project: func(*Assets) (Changes, error) {
			called = true
			return Changes{}, nil
		},
	})
	if !errors.Is(err, ErrAccountMismatch) || called {
		t.Fatalf("Mutate error = %v called=%t", err, called)
	}
}

func TestOwnerMutateRollsBackAllAssetsWhenProjectionFails(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	seedNPCShopOwner(t, ctx, repositories)
	owner, err := NewOwner(repositories)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	wantErr := errors.New("reject shop projection")
	err = owner.Mutate(ctx, Command{
		AccountID:   "dnf:1",
		CharacterID: "77",
		Project: func(assets *Assets) (Changes, error) {
			assets.Character.Stats["gold"] = 1
			assets.Inventory.Slots["0:10"] = dnfrepo.ItemStack{ItemID: 600, Count: 3}
			return Changes{Character: true, Inventory: true}, wantErr
		},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Mutate error = %v, want %v", err, wantErr)
	}
	character, _, _ := repositories.Character.Load(ctx, "77")
	inventory, _, _ := repositories.Inventory.Load(ctx, "77")
	if character.Stats["gold"] != 1000 || len(inventory.Slots) != 0 {
		t.Fatalf("rollback character=%+v inventory=%+v", character, inventory)
	}
}

func seedNPCShopOwner(t *testing.T, ctx context.Context, repositories dnfrepo.Group) {
	t.Helper()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "77",
		AccountID:   "dnf:1",
		Stats:       map[string]int64{"gold": 1000, "sp": 30},
	}); err != nil {
		t.Fatalf("save character: %v", err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots:       map[string]dnfrepo.ItemStack{},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	if err := repositories.AccountInventory.Save(ctx, dnfrepo.AccountInventoryRecord{
		AccountID: "dnf:1",
		Slots:     map[string]dnfrepo.ItemStack{},
	}); err != nil {
		t.Fatalf("save account inventory: %v", err)
	}
}
