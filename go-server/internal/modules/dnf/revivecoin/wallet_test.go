package revivecoin

import (
	"errors"
	"testing"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func TestConsolidateMovesMisplacedWalletRowsToSlotOne(t *testing.T) {
	record := dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			"0:99":  {ItemID: WalletItemID, Count: 30},
			"0:108": {ItemID: WalletItemID, Count: 20},
			"0:12":  {ItemID: 600, Count: 1},
		},
	}

	result, err := Consolidate(&record)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.RemovedRows != 2 || result.Total != 50 {
		t.Fatalf("consolidation=%+v", result)
	}
	if _, found := record.Slots["0:99"]; found {
		t.Fatal("misplaced row 0:99 remains")
	}
	if _, found := record.Slots["0:108"]; found {
		t.Fatal("misplaced row 0:108 remains")
	}
	wallet := record.Slots[WalletKey()]
	if wallet.ItemID != WalletItemID || wallet.Count != 50 ||
		wallet.Extra["amount_or_count"] != "50" ||
		wallet.Extra["value_a"] != "50" {
		t.Fatalf("wallet=%+v", wallet)
	}
	if record.Slots["0:12"].ItemID != 600 {
		t.Fatalf("unrelated stack changed: %+v", record.Slots["0:12"])
	}
}

func TestMigrateLegacyConvertsCoinGeneralStackToWallet(t *testing.T) {
	record := dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			WalletKey(): walletStack(30, "seed"),
			"0:113": {
				ItemID: ConsumableItemID,
				Count:  856,
				Extra: map[string]string{
					"item_kind": "stackable",
					"pvf_path":  "stackable/cash/coin_general.stk",
				},
			},
			"0:114": {
				ItemID: ConsumableItemID,
				Count:  5,
				Extra: map[string]string{
					"pvf_path": "stackable/not_coin_general.stk",
				},
			},
		},
	}

	result, err := MigrateLegacy(&record)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed ||
		result.RemovedRows != 2 ||
		result.ConvertedConsumableRows != 1 ||
		result.ConvertedConsumableUnits != 856 ||
		result.Total != 886 {
		t.Fatalf("consolidation=%+v", result)
	}
	if _, found := record.Slots["0:113"]; found {
		t.Fatal("legacy coin_general backpack row remains")
	}
	if unrelated := record.Slots["0:114"]; unrelated.ItemID != ConsumableItemID || unrelated.Count != 5 {
		t.Fatalf("non-matching item-42 row changed: %+v", unrelated)
	}
	if wallet := record.Slots[WalletKey()]; wallet.ItemID != WalletItemID ||
		wallet.Count != 886 ||
		wallet.Extra["amount_or_count"] != "886" ||
		wallet.Extra["instance_value"] != "886" {
		t.Fatalf("wallet=%+v", wallet)
	}

	repeated, err := MigrateLegacy(&record)
	if err != nil {
		t.Fatal(err)
	}
	if repeated.Changed || repeated.Total != 886 {
		t.Fatalf("repeated consolidation=%+v", repeated)
	}
}

func TestGrantAddsToCanonicalWallet(t *testing.T) {
	record := dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			WalletKey(): walletStack(3, "seed"),
		},
	}
	total, err := Grant(&record, 2, "op44_coin_general")
	if err != nil {
		t.Fatal(err)
	}
	if total != 5 || record.Slots[WalletKey()].Count != 5 ||
		record.Slots[WalletKey()].Extra["amount_or_count"] != "5" {
		t.Fatalf("total=%d wallet=%+v", total, record.Slots[WalletKey()])
	}
}

func TestConsolidateDoesNotOverwriteReservedSlotConflict(t *testing.T) {
	record := dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			WalletKey(): {ItemID: 700, Count: 1},
			"0:99":      {ItemID: WalletItemID, Count: 30},
		},
	}
	if _, err := Consolidate(&record); !errors.Is(err, ErrWalletSlotConflict) {
		t.Fatalf("error=%v, want ErrWalletSlotConflict", err)
	}
	if record.Slots[WalletKey()].ItemID != 700 || record.Slots["0:99"].Count != 30 {
		t.Fatalf("record mutated on conflict: %+v", record.Slots)
	}
}

func TestIsConsumableRequiresCurrentRuntimePVFIdentity(t *testing.T) {
	valid := dnfrepo.ItemStack{
		ItemID: ConsumableItemID,
		Count:  1,
		Extra: map[string]string{
			"pvf_path":       "stackable/cash/coin_general.stk",
			"stackable_type": "[waste]",
		},
	}
	if !IsConsumable(valid) {
		t.Fatal("current runtime coin consumable was not recognized")
	}
	valid.Extra["pvf_path"] = "stackable/coin.stk"
	if IsConsumable(valid) {
		t.Fatal("wallet entity was accepted as the right-click consumable")
	}
}
