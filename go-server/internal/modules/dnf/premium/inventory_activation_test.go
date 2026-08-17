package premium

import (
	"errors"
	"math"
	"testing"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func TestActivateInventoryContractsConsumesMainBagAndStacksDurations(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	account := dnfrepo.AccountRecord{AccountID: "account-1", Metadata: make(map[string]string)}
	Upsert(&account, TypeOverSkill, 86400, 1, now)
	inventory := dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			"0:10": {ItemID: 44, Count: 2},
			"0:11": {ItemID: 46, Count: 1},
			"0:12": {ItemID: 500, Count: 7},
			"1:10": {ItemID: 44, Count: 9},
		},
		Warehouse: map[string]dnfrepo.ItemStack{
			"2:5": {ItemID: 44, Count: 4},
		},
	}
	result, err := ActivateInventoryContracts(&account, &inventory, map[int64]InventoryContract{
		44: {ItemID: 44, PremiumType: TypeOverSkill, DurationSeconds: 3 * 86400},
		46: {ItemID: 46, PremiumType: TypeOverSkill, DurationSeconds: 15 * 86400},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Activations) != 1 || result.Activations[0].PremiumType != TypeOverSkill ||
		result.Activations[0].Units != 3 || result.Activations[0].DurationSeconds != 21*86400 ||
		result.Activations[0].ExpireAt != now.Unix()+22*86400 {
		t.Fatalf("activation = %+v", result.Activations)
	}
	if len(result.RemovedSlots) != 2 || result.RemovedSlots[0] != 10 || result.RemovedSlots[1] != 11 {
		t.Fatalf("removed slots = %v", result.RemovedSlots)
	}
	if _, exists := inventory.Slots["0:10"]; exists {
		t.Fatal("first contract stack remains in main inventory")
	}
	if _, exists := inventory.Slots["0:11"]; exists {
		t.Fatal("second contract stack remains in main inventory")
	}
	if inventory.Slots["0:12"].Count != 7 || inventory.Slots["1:10"].Count != 9 || inventory.Warehouse["2:5"].Count != 4 {
		t.Fatalf("unrelated containers changed: %+v warehouse=%+v", inventory.Slots, inventory.Warehouse)
	}
}

func TestActivateInventoryContractsRejectsOverflowBeforeMutation(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	account := dnfrepo.AccountRecord{AccountID: "account-1", Metadata: make(map[string]string)}
	inventory := dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			"0:10": {ItemID: 44, Count: math.MaxInt64},
		},
	}
	_, err := ActivateInventoryContracts(&account, &inventory, map[int64]InventoryContract{
		44: {ItemID: 44, PremiumType: TypeOverSkill, DurationSeconds: 3},
	}, now)
	if !errors.Is(err, ErrInventoryActivationDurationOverflow) {
		t.Fatalf("error = %v", err)
	}
	if inventory.Slots["0:10"].Count != math.MaxInt64 || ExpireAt(account, TypeOverSkill) != 0 {
		t.Fatalf("overflow mutated state: inventory=%+v account=%+v", inventory, account)
	}
}
