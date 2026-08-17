package inventory

import (
	"context"
	"errors"
	"testing"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestMoveAccountCargoPersistsRowsUnderAccountOwner(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Account.Save(ctx, dnfrepo.AccountRecord{
		AccountID: "dnf:account-cargo",
		Metadata: map[string]string{
			"account_cargo_created": "true",
			"account_cargo_level":   "8",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"0:21": {ItemID: 100021, Count: 3, Extra: map[string]string{"stack_limit": "100"}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	result, err := owner.Move(ctx, NewMoveCommand(alignedcmd.Request{
		AccountID:           "dnf:account-cargo",
		SelectedCharacterID: 77,
	}, MoveItemspaceRequest{
		SourceListType:       listTypeMain,
		SourceSlotIndex:      21,
		MoveCount:            3,
		DestinationListType:  listTypeAccountCargo,
		DestinationSlotIndex: 2,
	}))
	if err != nil {
		t.Fatalf("move to account cargo: %v", err)
	}
	if !result.Changed || result.Mode != "move" || result.MoveCount != 3 {
		t.Fatalf("move result=%+v", result)
	}
	character, found, err := repos.Inventory.Load(ctx, "77")
	if err != nil || !found {
		t.Fatalf("load character found=%t err=%v", found, err)
	}
	if _, exists := character.Slots["0:21"]; exists {
		t.Fatalf("source still belongs to character: %+v", character.Slots)
	}
	cargo, found, err := repos.AccountInventory.Load(ctx, "dnf:account-cargo")
	if err != nil || !found {
		t.Fatalf("load account cargo found=%t err=%v", found, err)
	}
	if got := cargo.Slots["12:2"]; got.ItemID != 100021 || got.Count != 3 {
		t.Fatalf("account cargo row=%+v", got)
	}

	// A different selected character must see the same account-owned row and
	// can take it back without creating a per-character list-12 duplicate.
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{CharacterID: "78", Slots: map[string]dnfrepo.ItemStack{}}); err != nil {
		t.Fatal(err)
	}
	result, err = owner.Move(ctx, NewMoveCommand(alignedcmd.Request{
		AccountID:           "dnf:account-cargo",
		SelectedCharacterID: 78,
	}, MoveItemspaceRequest{
		SourceListType:       listTypeAccountCargo,
		SourceSlotIndex:      2,
		MoveCount:            3,
		DestinationListType:  listTypeMain,
		DestinationSlotIndex: 22,
	}))
	if err != nil {
		t.Fatalf("withdraw from account cargo: %v", err)
	}
	if !result.Changed || result.Mode != "move" {
		t.Fatalf("withdraw result=%+v", result)
	}
	second, found, err := repos.Inventory.Load(ctx, "78")
	if err != nil || !found {
		t.Fatalf("load second character found=%t err=%v", found, err)
	}
	if got := second.Slots["0:22"]; got.ItemID != 100021 || got.Count != 3 {
		t.Fatalf("withdrawn row=%+v", got)
	}
	cargo, found, err = repos.AccountInventory.Load(ctx, "dnf:account-cargo")
	if err != nil || !found {
		t.Fatalf("reload account cargo found=%t err=%v", found, err)
	}
	if _, exists := cargo.Slots["12:2"]; exists {
		t.Fatalf("account cargo row remained after withdraw: %+v", cargo.Slots)
	}
}

func TestMoveAccountCargoRejectsSlotOutsidePersistedCapacity(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Account.Save(ctx, dnfrepo.AccountRecord{
		AccountID: "dnf:tiny-cargo",
		Metadata:  map[string]string{"account_cargo_created": "true", "account_cargo_level": "1"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{CharacterID: "77", Slots: map[string]dnfrepo.ItemStack{"0:1": {ItemID: 1, Count: 1}}}); err != nil {
		t.Fatal(err)
	}
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	_, err = owner.Move(ctx, NewMoveCommand(alignedcmd.Request{
		AccountID:           "dnf:tiny-cargo",
		SelectedCharacterID: 77,
	}, MoveItemspaceRequest{
		SourceListType:       listTypeMain,
		SourceSlotIndex:      1,
		MoveCount:            1,
		DestinationListType:  listTypeAccountCargo,
		DestinationSlotIndex: 1,
	}))
	if !errors.Is(err, ErrAccountCargoSlotOutOfRange) {
		t.Fatalf("move error=%v, want ErrAccountCargoSlotOutOfRange", err)
	}
}
