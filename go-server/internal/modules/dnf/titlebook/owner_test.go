package titlebook

import (
	"context"
	"errors"
	"testing"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestOwnerPutAndGetTitle(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			"0:42": {ItemID: 9001, Count: 1, Extra: map[string]string{"bind": "1"}},
		},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner: %v", err)
	}
	put, err := owner.Put(ctx, PutCommand{
		SelectedCharacterID: 19,
		ItemSpace:           0,
		SourceSlot:          42,
		ItemID:              9001,
		Category:            2,
		BookIndex:           7,
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if put.TargetSlot != 2007 || put.BookIndex != 7 {
		t.Fatalf("put result = %+v", put)
	}
	inventory, _, err := repos.Inventory.Load(ctx, "19")
	if err != nil {
		t.Fatalf("load after put: %v", err)
	}
	if _, found := inventory.Slots["0:42"]; found ||
		inventory.Slots["100:2007"].ItemID != 9001 {
		t.Fatalf("slots after put = %+v", inventory.Slots)
	}

	get, err := owner.Get(ctx, GetCommand{
		SelectedCharacterID: 19,
		ItemSpace:           0,
		SourceSlot:          42,
		ItemID:              9001,
		Category:            2,
		BookIndex:           7,
	})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if get.TargetSlot != 9 || get.TargetStack.ItemID != 9001 ||
		get.TargetStack.Extra["bind"] != "1" {
		t.Fatalf("get result = %+v", get)
	}
}

func TestOwnerPutRejectsFullCategory(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	slots := map[string]dnfrepo.ItemStack{
		"0:42": {ItemID: 9001, Count: 1},
	}
	for index := int32(0); index < MaxPerCategory; index++ {
		slots[BookSlotKey(2, index)] = dnfrepo.ItemStack{ItemID: int64(10000 + index), Count: 1}
	}
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots:       slots,
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner: %v", err)
	}
	_, err = owner.Put(ctx, PutCommand{
		SelectedCharacterID: 19,
		ItemSpace:           0,
		SourceSlot:          42,
		ItemID:              9001,
		Category:            2,
		BookIndex:           7,
	})
	if !errors.Is(err, ErrCategoryFull) {
		t.Fatalf("Put error = %v, want ErrCategoryFull", err)
	}
}
