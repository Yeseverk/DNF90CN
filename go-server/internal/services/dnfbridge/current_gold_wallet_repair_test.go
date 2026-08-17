package dnfbridge

import (
	"context"
	"testing"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestRepairCurrentGoldWalletReservedSlotMovesOnlyIntoRuntimePVFPage(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			"0:0":   {ItemID: 10008101, Count: 1, Extra: map[string]string{"marker": "retain"}},
			"0:121": {ItemID: 4, Count: 1},
		},
	}); err != nil {
		t.Fatal(err)
	}
	catalog := &pvfDungeonDropCatalog{
		source:    dungeonDropCatalogTestSource{},
		itemCache: map[uint32]dungeonDropItemDefinition{10008101: {ItemID: 10008101, Kind: dungeonDropItemStackable, SlotStart: 121, SlotEnd: 176}},
	}
	result, err := (&Service{}).repairCurrentGoldWalletReservedSlot(ctx, repos, "19", catalog)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.FromSlot != 0 || result.ToSlot != 122 || result.ItemID != 10008101 {
		t.Fatalf("result=%+v", result)
	}
	record, found, err := repos.Inventory.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load: found=%t err=%v", found, err)
	}
	if _, found := record.Slots["0:0"]; found {
		t.Fatal("ordinary item remains in wallet slot")
	}
	if got := record.Slots["0:122"]; got.ItemID != 10008101 || got.Extra["marker"] != "retain" {
		t.Fatalf("relocated stack=%+v", got)
	}
}
