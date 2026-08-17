package limitedcube

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestOwnerUseConsumesTicketConditionAndSplitMaterialStacks(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	expires := time.Date(2027, time.March, 1, 0, 0, 0, 0, time.UTC)
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			"0:2":  {ItemID: 9000, Count: 2},
			"0:7":  {ItemID: 9001, Count: 1, Bind: true, ExpireAt: expires, RawEntry: []byte{1}, Extra: map[string]string{"old": "item"}},
			"0:12": {ItemID: 3037, Count: 6},
			"0:13": {ItemID: 3037, Count: 7},
		},
	}); err != nil {
		t.Fatal(err)
	}
	owner, err := NewOwner(repositories)
	if err != nil {
		t.Fatal(err)
	}
	result, err := owner.Use(ctx, Command{AccountID: "dnf:19", CharacterID: "19", TicketSlot: 2, TicketItemID: 9000, TargetSlot: 7, TargetItemID: 9001}, limitedCubePolicy())
	if err != nil {
		t.Fatal(err)
	}
	if result.TicketRemaining != 1 || result.InputSlot != 7 || result.InputItemID != 9001 || result.ResultItemID != 9002 || result.ResultItemID == result.InputItemID ||
		!reflect.DeepEqual(result.ChangedSlots, []int16{2, 7, 12, 13}) {
		t.Fatalf("result = %+v", result)
	}
	stored, found, err := repositories.Inventory.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load inventory found=%t err=%v", found, err)
	}
	if got := stored.Slots["0:2"].Count; got != 1 {
		t.Fatalf("ticket count = %d, want 1", got)
	}
	changed := stored.Slots["0:7"]
	if changed.ItemID != 9002 || changed.Count != 1 || !changed.Bind || !changed.ExpireAt.Equal(expires) || len(changed.RawEntry) != 0 || changed.Extra["pvf_path"] != "stackable/bead/result.stk" {
		t.Fatalf("changed bead = %+v", changed)
	}
	if _, found := stored.Slots["0:12"]; found {
		t.Fatalf("first material stack was not consumed: %+v", stored.Slots)
	}
	if got := stored.Slots["0:13"].Count; got != 3 {
		t.Fatalf("second material count = %d, want 3", got)
	}
}

func TestOwnerUseRejectsResultPoolContainingOnlyInputItemWithoutMutatingInventory(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	initial := dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			"0:2": {ItemID: 9000, Count: 1},
			"0:7": {ItemID: 9001, Count: 1},
			"0:9": {ItemID: 3037, Count: 10},
		},
	}
	if err := repositories.Inventory.Save(ctx, initial); err != nil {
		t.Fatal(err)
	}
	owner, err := NewOwner(repositories)
	if err != nil {
		t.Fatal(err)
	}
	policy := limitedCubePolicy()
	policy.Results[0].Stack.ItemID = 9001
	if _, err := owner.Use(ctx, limitedCubeCommand(), policy); !errors.Is(err, ErrResultSelectionFailed) {
		t.Fatalf("Use error=%v want result selection failure", err)
	}
	stored, found, err := repositories.Inventory.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load inventory found=%t err=%v", found, err)
	}
	if !reflect.DeepEqual(stored.Slots, initial.Slots) {
		t.Fatalf("failed use mutated slots = %+v, want %+v", stored.Slots, initial.Slots)
	}
}

func TestOwnerUseRejectsMissingMaterialWithoutChangingInventory(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	initial := dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			"0:2": {ItemID: 9000, Count: 1},
			"0:7": {ItemID: 9001, Count: 1},
			"0:9": {ItemID: 3037, Count: 9},
		},
	}
	if err := repositories.Inventory.Save(ctx, initial); err != nil {
		t.Fatal(err)
	}
	owner, err := NewOwner(repositories)
	if err != nil {
		t.Fatal(err)
	}
	_, err = owner.Use(ctx, Command{AccountID: "dnf:19", CharacterID: "19", TicketSlot: 2, TicketItemID: 9000, TargetSlot: 7, TargetItemID: 9001}, limitedCubePolicy())
	if !errors.Is(err, ErrMaterialInsufficient) {
		t.Fatalf("Use error = %v, want insufficient material", err)
	}
	stored, found, loadErr := repositories.Inventory.Load(ctx, "19")
	if loadErr != nil || !found {
		t.Fatalf("load inventory found=%t err=%v", found, loadErr)
	}
	if !reflect.DeepEqual(stored.Slots, initial.Slots) {
		t.Fatalf("failed use mutated slots = %+v, want %+v", stored.Slots, initial.Slots)
	}
}

func TestOwnerUseRejectsWrongTicketAndMissingCondition(t *testing.T) {
	ctx := context.Background()
	for _, test := range []struct {
		name  string
		slots map[string]dnfrepo.ItemStack
		item  int64
		want  error
	}{
		{
			name:  "wrong ticket identity",
			slots: map[string]dnfrepo.ItemStack{"0:2": {ItemID: 9999, Count: 1}, "0:7": {ItemID: 9001, Count: 1}, "0:9": {ItemID: 3037, Count: 10}},
			item:  9000,
			want:  ErrTicketMismatch,
		},
		{
			name:  "ineligible target bead",
			slots: map[string]dnfrepo.ItemStack{"0:2": {ItemID: 9000, Count: 1}, "0:7": {ItemID: 9999, Count: 1}, "0:9": {ItemID: 3037, Count: 10}},
			item:  9000,
			want:  ErrConditionItemMismatch,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			repositories := dnfrepomemory.NewMemoryGroup()
			if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{CharacterID: "19", Slots: test.slots}); err != nil {
				t.Fatal(err)
			}
			owner, err := NewOwner(repositories)
			if err != nil {
				t.Fatal(err)
			}
			_, err = owner.Use(ctx, Command{AccountID: "dnf:19", CharacterID: "19", TicketSlot: 2, TicketItemID: test.item, TargetSlot: 7, TargetItemID: 9001}, limitedCubePolicy())
			if !errors.Is(err, test.want) {
				t.Fatalf("Use error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestOwnerUseRejectsLockedTicketTargetAndMaterial(t *testing.T) {
	ctx := context.Background()
	for _, test := range []struct {
		name  string
		slots map[string]dnfrepo.ItemStack
		want  error
	}{
		{
			name: "ticket",
			slots: map[string]dnfrepo.ItemStack{
				"0:2": {ItemID: 9000, Count: 1, Extra: map[string]string{"equipment_lock_state": "locked"}},
				"0:7": {ItemID: 9001, Count: 1},
				"0:9": {ItemID: 3037, Count: 10},
			},
			want: ErrTicketLocked,
		},
		{
			name: "target bead",
			slots: map[string]dnfrepo.ItemStack{
				"0:2": {ItemID: 9000, Count: 1},
				"0:7": {ItemID: 9001, Count: 1, Extra: map[string]string{"equipment_lock_state": "1"}},
				"0:9": {ItemID: 3037, Count: 10},
			},
			want: ErrConditionItemLocked,
		},
		{
			name: "material",
			slots: map[string]dnfrepo.ItemStack{
				"0:2": {ItemID: 9000, Count: 1},
				"0:7": {ItemID: 9001, Count: 1},
				"0:9": {ItemID: 3037, Count: 10, Extra: map[string]string{"equipment_lock_state": "pending_unlock"}},
			},
			want: ErrMaterialInsufficient,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			repositories := dnfrepomemory.NewMemoryGroup()
			initial := dnfrepo.InventoryRecord{CharacterID: "19", Slots: test.slots}
			if err := repositories.Inventory.Save(ctx, initial); err != nil {
				t.Fatal(err)
			}
			owner, err := NewOwner(repositories)
			if err != nil {
				t.Fatal(err)
			}
			_, err = owner.Use(ctx, Command{AccountID: "dnf:19", CharacterID: "19", TicketSlot: 2, TicketItemID: 9000, TargetSlot: 7, TargetItemID: 9001}, limitedCubePolicy())
			if !errors.Is(err, test.want) {
				t.Fatalf("Use error=%v want=%v", err, test.want)
			}
			stored, found, loadErr := repositories.Inventory.Load(ctx, "19")
			if loadErr != nil || !found {
				t.Fatalf("load inventory found=%t err=%v", found, loadErr)
			}
			if !reflect.DeepEqual(stored.Slots, initial.Slots) {
				t.Fatalf("locked use mutated slots=%+v want=%+v", stored.Slots, initial.Slots)
			}
		})
	}
}

func TestOwnerUseConsumesAccountSharedMaterialAfterCharacterInventory(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			"0:2": {ItemID: 9000, Count: 1},
			"0:7": {ItemID: 9001, Count: 1},
			"0:9": {ItemID: 3037, Count: 6},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.AccountInventory.Save(ctx, dnfrepo.AccountInventoryRecord{
		AccountID: "dnf:19",
		Slots: map[string]dnfrepo.ItemStack{
			dnfrepo.AccountSharedInventorySlotKey(358): {ItemID: 3037, Count: 20},
		},
	}); err != nil {
		t.Fatal(err)
	}
	owner, err := NewOwner(repositories)
	if err != nil {
		t.Fatal(err)
	}
	result, err := owner.Use(ctx, limitedCubeCommand(), limitedCubePolicy())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.ChangedSlots, []int16{2, 7, 9}) || !reflect.DeepEqual(result.AccountChangedSlots, []int16{358}) {
		t.Fatalf("changed slots = character=%v account=%v", result.ChangedSlots, result.AccountChangedSlots)
	}
	character, found, err := repositories.Inventory.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load character inventory found=%t err=%v", found, err)
	}
	if _, found := character.Slots["0:9"]; found {
		t.Fatalf("character material was not consumed first: %+v", character.Slots)
	}
	account, found, err := repositories.AccountInventory.Load(ctx, "dnf:19")
	if err != nil || !found {
		t.Fatalf("load account inventory found=%t err=%v", found, err)
	}
	if got := account.Slots["0:358"].Count; got != 16 {
		t.Fatalf("account material count=%d want=16", got)
	}
}

func TestOwnerUseConsumesAccountSharedMaterialWhenCharacterHasNone(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			"0:2": {ItemID: 9000, Count: 1},
			"0:7": {ItemID: 9001, Count: 1},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.AccountInventory.Save(ctx, dnfrepo.AccountInventoryRecord{
		AccountID: "dnf:19",
		Slots: map[string]dnfrepo.ItemStack{
			dnfrepo.AccountSharedInventorySlotKey(358): {ItemID: 3037, Count: 10},
		},
	}); err != nil {
		t.Fatal(err)
	}
	owner, err := NewOwner(repositories)
	if err != nil {
		t.Fatal(err)
	}
	result, err := owner.Use(ctx, limitedCubeCommand(), limitedCubePolicy())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.ChangedSlots, []int16{2, 7}) || !reflect.DeepEqual(result.AccountChangedSlots, []int16{358}) {
		t.Fatalf("changed slots = character=%v account=%v", result.ChangedSlots, result.AccountChangedSlots)
	}
	account, found, err := repositories.AccountInventory.Load(ctx, "dnf:19")
	if err != nil || !found {
		t.Fatalf("load account inventory found=%t err=%v", found, err)
	}
	if _, found := account.Slots["0:358"]; found {
		t.Fatalf("account material was not consumed: %+v", account.Slots)
	}
}

func TestOwnerUseExcludesLockedAccountSharedMaterial(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			"0:2": {ItemID: 9000, Count: 1},
			"0:7": {ItemID: 9001, Count: 1},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.AccountInventory.Save(ctx, dnfrepo.AccountInventoryRecord{
		AccountID: "dnf:19",
		Slots: map[string]dnfrepo.ItemStack{
			dnfrepo.AccountSharedInventorySlotKey(358): {
				ItemID: 3037,
				Count:  10,
				Extra:  map[string]string{"equipment_lock_state": "locked"},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	owner, err := NewOwner(repositories)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Use(ctx, limitedCubeCommand(), limitedCubePolicy()); !errors.Is(err, ErrMaterialInsufficient) {
		t.Fatalf("Use error=%v want insufficient material", err)
	}
	account, found, err := repositories.AccountInventory.Load(ctx, "dnf:19")
	if err != nil || !found {
		t.Fatalf("load account inventory found=%t err=%v", found, err)
	}
	if got := account.Slots["0:358"].Count; got != 10 {
		t.Fatalf("locked account material count=%d want=10", got)
	}
}

func limitedCubeCommand() Command {
	return Command{AccountID: "dnf:19", CharacterID: "19", TicketSlot: 2, TicketItemID: 9000, TargetSlot: 7, TargetItemID: 9001}
}

func limitedCubePolicy() Policy {
	return Policy{
		TicketItemID: 9000,
		Conditions:   []Requirement{{ItemID: 9001, Count: 1}},
		Materials:    []Requirement{{ItemID: 3037, Count: 10}},
		Results: []WeightedResult{{
			Stack: dnfrepo.ItemStack{
				ItemID: 9002,
				Count:  1,
				Extra:  map[string]string{"item_kind": "stackable", "pvf_path": "stackable/bead/result.stk", "stackable_type": "[enchant waste]"},
			},
			Weight: 1000,
		}},
	}
}
