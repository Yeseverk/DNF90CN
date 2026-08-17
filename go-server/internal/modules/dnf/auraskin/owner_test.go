package auraskin

import (
	"context"
	"errors"
	"testing"

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

func TestOwnerOpenConsumesTicketAndPersistsFlag(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "account-1",
		Stats:       map[string]int64{FlagStat: 0},
	}); err != nil {
		t.Fatalf("save character: %v", err)
	}
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			"0:122": {ItemID: 490700411, Count: 2},
		},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	owner, err := NewOwner(repos, catalogStub{
		490700411: {Kind: ItemStackable, PVFPath: `stackable\cash\chn_490700411.stk`},
	})
	if err != nil {
		t.Fatalf("NewOwner: %v", err)
	}
	result, err := owner.Open(ctx, Command{
		AccountID:           "account-1",
		SelectedCharacterID: 19,
		SourceSlot:          122,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !result.Consumed || !result.SourceChanged || result.SourceRemoved ||
		result.RemainingStack.Count != 1 {
		t.Fatalf("result = %+v", result)
	}
	character, _, err := repos.Character.Load(ctx, "19")
	if err != nil {
		t.Fatalf("load character: %v", err)
	}
	if character.Stats[FlagStat] != 1 {
		t.Fatalf("aura flag = %d", character.Stats[FlagStat])
	}
	inventory, _, err := repos.Inventory.Load(ctx, "19")
	if err != nil {
		t.Fatalf("load inventory: %v", err)
	}
	if inventory.Slots["0:122"].Count != 1 {
		t.Fatalf("source stack = %+v", inventory.Slots["0:122"])
	}
}

func TestOwnerOpenAlreadyOpenDoesNotResolveOrConsume(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "account-1",
		Stats:       map[string]int64{FlagStat: 1},
	}); err != nil {
		t.Fatalf("save character: %v", err)
	}
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			"0:122": {ItemID: 490700411, Count: 1},
		},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	owner, err := NewOwner(repos, catalogStub{})
	if err != nil {
		t.Fatalf("NewOwner: %v", err)
	}
	result, err := owner.Open(ctx, Command{
		AccountID:           "account-1",
		SelectedCharacterID: 19,
		SourceSlot:          122,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !result.AlreadyOpen || result.Consumed || result.SourceChanged {
		t.Fatalf("result = %+v", result)
	}
	inventory, _, err := repos.Inventory.Load(ctx, "19")
	if err != nil {
		t.Fatalf("load inventory: %v", err)
	}
	if inventory.Slots["0:122"].Count != 1 {
		t.Fatalf("source stack = %+v", inventory.Slots["0:122"])
	}
}

func TestOwnerOpenRejectsWrongPVFItem(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "account-1",
	}); err != nil {
		t.Fatalf("save character: %v", err)
	}
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			"0:122": {ItemID: 42, Count: 1},
		},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	owner, err := NewOwner(repos, catalogStub{
		42: {Kind: ItemStackable, PVFPath: "stackable/material/not-a-ticket.stk"},
	})
	if err != nil {
		t.Fatalf("NewOwner: %v", err)
	}
	_, err = owner.Open(ctx, Command{
		AccountID:           "account-1",
		SelectedCharacterID: 19,
		SourceSlot:          122,
	})
	if !errors.Is(err, ErrTicketInvalid) {
		t.Fatalf("Open error = %v, want ErrTicketInvalid", err)
	}
}
