package inventory

import (
	"context"
	"errors"
	"testing"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestRelocateReservedMainSlotPreservesCompleteStackAtomically(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	stack := dnfrepo.ItemStack{
		ItemID:   10008101,
		Count:    1,
		Bind:     true,
		RawEntry: []byte{0x77, 0x01},
		Extra:    map[string]string{"pvf_path": "stackable/ect/test.stk", "marker": "keep"},
	}
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 0):   stack,
			slotKey(listTypeMain, 121): {ItemID: 1, Count: 1},
		},
	})
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	result, err := owner.RelocateReservedMainSlot(ctx, "77", 0, 121, 123)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.ItemID != 10008101 || result.FromSlot != 0 || result.ToSlot != 122 {
		t.Fatalf("result=%+v", result)
	}
	loaded := loadTestInventory(t, ctx, repos, "77")
	if _, found := loaded.Slots[slotKey(listTypeMain, 0)]; found {
		t.Fatal("reserved wallet slot still contains the ordinary item")
	}
	got := loaded.Slots[slotKey(listTypeMain, 122)]
	if got.ItemID != stack.ItemID || got.Count != stack.Count || !got.Bind || string(got.RawEntry) != string(stack.RawEntry) || got.Extra["marker"] != "keep" {
		t.Fatalf("relocated stack=%+v", got)
	}
	if repeat, err := owner.RelocateReservedMainSlot(ctx, "77", 0, 121, 123); err != nil || repeat.Changed {
		t.Fatalf("repeat result=%+v err=%v", repeat, err)
	}
}

func TestRelocateReservedMainSlotFailsWithoutOverwritingFullProvenPage(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 0):   {ItemID: 10008101, Count: 1},
			slotKey(listTypeMain, 121): {ItemID: 1, Count: 1},
		},
	})
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	_, err = owner.RelocateReservedMainSlot(ctx, "77", 0, 121, 121)
	if !errors.Is(err, ErrReservedSlotRelocationFull) {
		t.Fatalf("error=%v", err)
	}
	loaded := loadTestInventory(t, ctx, repos, "77")
	if got := loaded.Slots[slotKey(listTypeMain, 0)].ItemID; got != 10008101 {
		t.Fatalf("reserved source changed after failed relocation: %d", got)
	}
	if got := loaded.Slots[slotKey(listTypeMain, 121)].ItemID; got != 1 {
		t.Fatalf("occupied destination overwritten: %d", got)
	}
}
