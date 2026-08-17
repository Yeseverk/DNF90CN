package inventory

import (
	"context"
	"testing"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestMigrateReviveCoinWalletConsolidatesLegacyRowsAtomically(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			"0:99":  {ItemID: 1, Count: 30},
			"0:108": {ItemID: 1, Count: 30},
			"0:12":  {ItemID: 600, Count: 1},
		},
	})
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	result, err := owner.MigrateReviveCoinWallet(ctx, "19")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.Total != 60 || result.RemovedRows != 2 {
		t.Fatalf("result=%+v", result)
	}
	loaded := loadTestInventory(t, ctx, repos, "19")
	if _, found := loaded.Slots["0:99"]; found {
		t.Fatal("legacy row 0:99 remains")
	}
	if _, found := loaded.Slots["0:108"]; found {
		t.Fatal("legacy row 0:108 remains")
	}
	if wallet := loaded.Slots["0:1"]; wallet.ItemID != 1 || wallet.Count != 60 ||
		wallet.Extra["amount_or_count"] != "60" {
		t.Fatalf("wallet=%+v", wallet)
	}

	repeated, err := owner.MigrateReviveCoinWallet(ctx, "19")
	if err != nil {
		t.Fatal(err)
	}
	if repeated.Changed || repeated.Total != 60 {
		t.Fatalf("repeated result=%+v", repeated)
	}
}

func TestMigrateReviveCoinWalletConvertsLegacyConsumableStackAtomically(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			"0:1": {
				ItemID: 1,
				Count:  30,
			},
			"0:113": {
				ItemID: 42,
				Count:  856,
				Extra: map[string]string{
					"item_kind": "stackable",
					"pvf_path":  "stackable/cash/coin_general.stk",
				},
			},
		},
	})
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}

	result, err := owner.MigrateReviveCoinWallet(ctx, "19")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed ||
		result.ConvertedConsumableRows != 1 ||
		result.ConvertedConsumableUnits != 856 ||
		result.Total != 886 {
		t.Fatalf("result=%+v", result)
	}
	loaded := loadTestInventory(t, ctx, repos, "19")
	if _, found := loaded.Slots["0:113"]; found {
		t.Fatal("legacy coin_general backpack row remains")
	}
	if wallet := loaded.Slots["0:1"]; wallet.ItemID != 1 ||
		wallet.Count != 886 ||
		wallet.Extra["amount_or_count"] != "886" ||
		wallet.Extra["instance_value"] != "886" {
		t.Fatalf("wallet=%+v", wallet)
	}
}
