package disjoint

import (
	"context"
	"errors"
	"testing"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestOwnerCommitsAccountAndCharacterInventoryTogether(t *testing.T) {
	ctx := context.Background()
	repositories := newOwnerTestRepositories(t, "dnf:1")
	owner, err := NewOwner(repositories)
	if err != nil {
		t.Fatal(err)
	}
	err = owner.DisjointEquipment(ctx, Command{
		AccountID:   "dnf:1",
		CharacterID: "77",
		Project: func(assets *Assets) (Changes, error) {
			delete(assets.Inventory.Slots, "0:5")
			assets.AccountInventory.Slots[dnfrepo.AccountSharedInventorySlotKey(358)] = dnfrepo.ItemStack{ItemID: 3037, Count: 10}
			return Changes{AccountInventory: true, Inventory: true}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	inventory, found, err := repositories.Inventory.Load(ctx, "77")
	if err != nil || !found {
		t.Fatalf("load inventory found=%v err=%v", found, err)
	}
	if _, found := inventory.Slots["0:5"]; found {
		t.Fatalf("source item was not removed: %+v", inventory.Slots)
	}
	account, found, err := repositories.AccountInventory.Load(ctx, "dnf:1")
	if err != nil || !found {
		t.Fatalf("load account inventory found=%v err=%v", found, err)
	}
	if account.Slots[dnfrepo.AccountSharedInventorySlotKey(358)].Count != 10 {
		t.Fatalf("account reward missing: %+v", account.Slots)
	}
}

func TestOwnerRollsBackProjectorFailure(t *testing.T) {
	ctx := context.Background()
	repositories := newOwnerTestRepositories(t, "dnf:1")
	owner, err := NewOwner(repositories)
	if err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("projection failed")
	err = owner.CompoundEmblems(ctx, Command{
		AccountID:   "dnf:1",
		CharacterID: "77",
		Project: func(assets *Assets) (Changes, error) {
			delete(assets.Inventory.Slots, "0:5")
			return Changes{Inventory: true}, wantErr
		},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("CompoundEmblems() error=%v want=%v", err, wantErr)
	}
	inventory, found, err := repositories.Inventory.Load(ctx, "77")
	if err != nil || !found {
		t.Fatalf("load inventory found=%v err=%v", found, err)
	}
	if inventory.Slots["0:5"].Count != 1 {
		t.Fatalf("failed projection leaked mutation: %+v", inventory.Slots)
	}
}

func TestOwnerRejectsCrossAccountCharacter(t *testing.T) {
	ctx := context.Background()
	repositories := newOwnerTestRepositories(t, "dnf:2")
	owner, err := NewOwner(repositories)
	if err != nil {
		t.Fatal(err)
	}
	err = owner.DisjointAvatar(ctx, Command{
		AccountID:   "dnf:1",
		CharacterID: "77",
		Project: func(*Assets) (Changes, error) {
			return Changes{}, nil
		},
	})
	if !errors.Is(err, ErrAccountMismatch) {
		t.Fatalf("DisjointAvatar() error=%v want=%v", err, ErrAccountMismatch)
	}
}

func TestOwnerExposesThreeGameplayBoundaries(t *testing.T) {
	ctx := context.Background()
	repositories := newOwnerTestRepositories(t, "dnf:1")
	owner, err := NewOwner(repositories)
	if err != nil {
		t.Fatal(err)
	}
	command := Command{
		AccountID:   "dnf:1",
		CharacterID: "77",
		Project: func(*Assets) (Changes, error) {
			return Changes{}, nil
		},
	}
	for name, apply := range map[string]func(context.Context, Command) error{
		"equipment disjoint": owner.DisjointEquipment,
		"avatar disjoint":    owner.DisjointAvatar,
		"emblem compound":    owner.CompoundEmblems,
	} {
		if err := apply(ctx, command); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func newOwnerTestRepositories(t *testing.T, characterAccountID string) dnfrepo.Group {
	t.Helper()
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "77", AccountID: characterAccountID}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"0:5": {ItemID: 9001, Count: 1},
		},
	}); err != nil {
		t.Fatal(err)
	}
	return repositories
}
