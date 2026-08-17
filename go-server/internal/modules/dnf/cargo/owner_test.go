package cargo

import (
	"context"
	"errors"
	"testing"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestOwnerPlanAccountReadsCargoMetadata(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Account.Save(ctx, dnfrepo.AccountRecord{
		AccountID: "acc-1",
		Metadata: map[string]string{
			"account_cargo_gold":    "1200",
			"account_cargo_level":   "3",
			"account_cargo_created": "1",
		},
	}); err != nil {
		t.Fatalf("save account: %v", err)
	}

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner() error = %v", err)
	}
	got, err := owner.PlanAccount(ctx, "upgrade_account_cargo", AccountCommand{AccountID: " acc-1 "})
	if err != nil {
		t.Fatalf("PlanAccount() error = %v", err)
	}
	if got.AccountID != "acc-1" || got.CargoGold != 1200 || got.CargoLevel != 3 || !got.CargoCreated {
		t.Fatalf("result = %+v", got)
	}
}

func TestOwnerPlanMoneyDepositChecksCharacterGold(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Account.Save(ctx, dnfrepo.AccountRecord{
		AccountID: "acc-2",
		Metadata:  map[string]string{"account_cargo_gold": "50"},
	}); err != nil {
		t.Fatalf("save account: %v", err)
	}
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "77",
		AccountID:   "acc-2",
		Stats:       map[string]int64{"gold": 500},
	}); err != nil {
		t.Fatalf("save character: %v", err)
	}

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner() error = %v", err)
	}
	got, err := owner.PlanMoney(ctx, MoneyCommand{
		AccountID:           "acc-2",
		SelectedCharacterID: 77,
		Direction:           MoneyDeposit,
		Amount:              300,
	})
	if err != nil {
		t.Fatalf("PlanMoney() error = %v", err)
	}
	if got.CharacterGold != 500 || got.CargoGold != 50 || got.Amount != 300 || got.Direction != MoneyDeposit {
		t.Fatalf("result = %+v", got)
	}
}

func TestOwnerPlanMoneyWithdrawChecksCargoGold(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Account.Save(ctx, dnfrepo.AccountRecord{
		AccountID: "acc-3",
		Metadata:  map[string]string{"account_cargo_gold": "900"},
	}); err != nil {
		t.Fatalf("save account: %v", err)
	}
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "78",
		AccountID:   "acc-3",
		Stats:       map[string]int64{"gold": 20},
	}); err != nil {
		t.Fatalf("save character: %v", err)
	}

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner() error = %v", err)
	}
	got, err := owner.PlanMoney(ctx, MoneyCommand{
		AccountID:           "acc-3",
		SelectedCharacterID: 78,
		Direction:           MoneyWithdraw,
		Amount:              700,
	})
	if err != nil {
		t.Fatalf("PlanMoney() error = %v", err)
	}
	if got.CharacterGold != 20 || got.CargoGold != 900 || got.Amount != 700 || got.Direction != MoneyWithdraw {
		t.Fatalf("result = %+v", got)
	}
}

func TestOwnerPlanMoneyRejectsInsufficientGold(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Account.Save(ctx, dnfrepo.AccountRecord{
		AccountID: "acc-4",
		Metadata:  map[string]string{"account_cargo_gold": "5"},
	}); err != nil {
		t.Fatalf("save account: %v", err)
	}
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "79",
		AccountID:   "acc-4",
		Stats:       map[string]int64{"gold": 10},
	}); err != nil {
		t.Fatalf("save character: %v", err)
	}

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner() error = %v", err)
	}
	_, err = owner.PlanMoney(ctx, MoneyCommand{
		AccountID:           "acc-4",
		SelectedCharacterID: 79,
		Direction:           MoneyWithdraw,
		Amount:              6,
	})
	if !errors.Is(err, ErrGoldInsufficient) {
		t.Fatalf("PlanMoney() error = %v, want ErrGoldInsufficient", err)
	}
}

func TestOwnerApplyCreateCargoChargesGold(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Account.Save(ctx, dnfrepo.AccountRecord{AccountID: "acc-5"}); err != nil {
		t.Fatalf("save account: %v", err)
	}
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "80",
		AccountID:   "acc-5",
		Stats:       map[string]int64{"gold": 500000},
	}); err != nil {
		t.Fatalf("save character: %v", err)
	}

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner() error = %v", err)
	}
	got, err := owner.ApplyAccount(ctx, "create_account_cargo", AccountCommand{
		AccountID:           "acc-5",
		SelectedCharacterID: 80,
	})
	if err != nil {
		t.Fatalf("ApplyAccount() error = %v", err)
	}
	if !got.Changed || got.CharacterGold != 400000 || got.CargoLevel != 1 || !got.CargoCreated {
		t.Fatalf("result = %+v", got)
	}

	account, ok, err := repos.Account.Load(ctx, "acc-5")
	if err != nil || !ok {
		t.Fatalf("load account ok=%t err=%v", ok, err)
	}
	if account.Metadata["account_cargo_level"] != "1" || account.Metadata["account_cargo_created"] != "true" {
		t.Fatalf("account metadata = %+v", account.Metadata)
	}
	character, ok, err := repos.Character.Load(ctx, "80")
	if err != nil || !ok {
		t.Fatalf("load character ok=%t err=%v", ok, err)
	}
	if got := character.Stats["gold"]; got != 400000 {
		t.Fatalf("character gold = %d, want 400000", got)
	}
}

func TestOwnerApplyAccountUpgradeDebitsAccountSharedCera(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Account.Save(ctx, dnfrepo.AccountRecord{
		AccountID: "acc-cera",
		Metadata: map[string]string{
			"account_cargo_created": "true",
			"account_cargo_level":   "8",
			"account_cargo_gold":    "0",
			"account_cera":          "3000",
		},
	}); err != nil {
		t.Fatalf("save account: %v", err)
	}
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "87", AccountID: "acc-cera", Stats: map[string]int64{"gold": 1}}); err != nil {
		t.Fatalf("save character: %v", err)
	}

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner() error = %v", err)
	}
	got, err := owner.ApplyAccount(ctx, "upgrade_account_cargo", AccountCommand{AccountID: "acc-cera", SelectedCharacterID: 87})
	if err != nil {
		t.Fatalf("ApplyAccount() error = %v", err)
	}
	if !got.Changed || got.Cost.Kind != CostCera || got.Cost.Amount != 2000 || got.CharacterCera != 1000 || got.CargoLevel != 16 {
		t.Fatalf("result = %+v", got)
	}
	account, ok, err := repos.Account.Load(ctx, "acc-cera")
	if err != nil || !ok {
		t.Fatalf("load account ok=%t err=%v", ok, err)
	}
	if account.Metadata["account_cera"] != "1000" || account.Metadata["account_cargo_level"] != "16" {
		t.Fatalf("account metadata = %+v", account.Metadata)
	}
}

func TestOwnerApplyAccountUpgradeRejectsInsufficientAccountCera(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Account.Save(ctx, dnfrepo.AccountRecord{
		AccountID: "acc-cera-poor",
		Metadata: map[string]string{
			"account_cargo_created": "true",
			"account_cargo_level":   "8",
			"account_cargo_gold":    "0",
			"account_cera":          "100",
		},
	}); err != nil {
		t.Fatalf("save account: %v", err)
	}
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "88", AccountID: "acc-cera-poor", Stats: map[string]int64{"gold": 1}}); err != nil {
		t.Fatalf("save character: %v", err)
	}

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner() error = %v", err)
	}
	if _, err := owner.ApplyAccount(ctx, "upgrade_account_cargo", AccountCommand{AccountID: "acc-cera-poor", SelectedCharacterID: 88}); !errors.Is(err, ErrGoldInsufficient) {
		t.Fatalf("ApplyAccount() error = %v, want ErrGoldInsufficient", err)
	}
	account, _, _ := repos.Account.Load(ctx, "acc-cera-poor")
	if account.Metadata["account_cera"] != "100" || account.Metadata["account_cargo_level"] != "8" {
		t.Fatalf("insufficient-cera attempt mutated account: %+v", account.Metadata)
	}
}

func TestOwnerApplyMoneyDepositPersistsBothSides(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Account.Save(ctx, dnfrepo.AccountRecord{
		AccountID: "acc-6",
		Metadata: map[string]string{
			"account_cargo_gold":    "50",
			"account_cargo_level":   "1",
			"account_cargo_created": "true",
		},
	}); err != nil {
		t.Fatalf("save account: %v", err)
	}
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "81",
		AccountID:   "acc-6",
		Stats:       map[string]int64{"gold": 500},
	}); err != nil {
		t.Fatalf("save character: %v", err)
	}

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner() error = %v", err)
	}
	got, err := owner.ApplyMoney(ctx, MoneyCommand{
		AccountID:           "acc-6",
		SelectedCharacterID: 81,
		Direction:           MoneyDeposit,
		Amount:              300,
	})
	if err != nil {
		t.Fatalf("ApplyMoney() error = %v", err)
	}
	if !got.Changed || got.CharacterGold != 200 || got.CargoGold != 350 {
		t.Fatalf("result = %+v", got)
	}

	account, ok, err := repos.Account.Load(ctx, "acc-6")
	if err != nil || !ok {
		t.Fatalf("load account ok=%t err=%v", ok, err)
	}
	if got := account.Metadata["account_cargo_gold"]; got != "350" {
		t.Fatalf("cargo gold = %q, want 350", got)
	}
	character, ok, err := repos.Character.Load(ctx, "81")
	if err != nil || !ok {
		t.Fatalf("load character ok=%t err=%v", ok, err)
	}
	if got := character.Stats["gold"]; got != 200 {
		t.Fatalf("character gold = %d, want 200", got)
	}
}

func TestOwnerApplyAccountUpgradeConsumesVoidMagicStoneForMiddleTiers(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Account.Save(ctx, dnfrepo.AccountRecord{
		AccountID: "acc-void",
		Metadata: map[string]string{
			"account_cargo_created": "true",
			"account_cargo_level":   "16",
			"account_cargo_gold":    "0",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "86", AccountID: "acc-void", Stats: map[string]int64{"gold": 1, "cera": 1}}); err != nil {
		t.Fatal(err)
	}
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{CharacterID: "86", Slots: map[string]dnfrepo.ItemStack{
		"0:10": {ItemID: accountCargoVoidMagicStoneItemID, Count: 200},
		"0:11": {ItemID: accountCargoVoidMagicStoneItemID, Count: 100},
	}}); err != nil {
		t.Fatal(err)
	}
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	got, err := owner.ApplyAccount(ctx, "upgrade_account_cargo", AccountCommand{AccountID: "acc-void", SelectedCharacterID: 86})
	if err != nil {
		t.Fatalf("upgrade account cargo: %v", err)
	}
	if !got.Changed || got.CargoLevel != 24 || got.Cost.Kind != CostMaterial || got.Cost.Amount != 250 {
		t.Fatalf("upgrade result=%+v", got)
	}
	items, found, err := repos.Inventory.Load(ctx, "86")
	if err != nil || !found {
		t.Fatalf("load inventory found=%t err=%v", found, err)
	}
	if _, exists := items.Slots["0:10"]; exists {
		t.Fatalf("first material stack not consumed: %+v", items.Slots)
	}
	if got := items.Slots["0:11"].Count; got != 50 {
		t.Fatalf("remaining void magic stones=%d want=50", got)
	}
}

func TestOwnerApplyMoneyWithdrawPersistsBothSides(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Account.Save(ctx, dnfrepo.AccountRecord{
		AccountID: "acc-7",
		Metadata: map[string]string{
			"account_cargo_gold":    "900",
			"account_cargo_level":   "1",
			"account_cargo_created": "true",
		},
	}); err != nil {
		t.Fatalf("save account: %v", err)
	}
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "82",
		AccountID:   "acc-7",
		Stats:       map[string]int64{"gold": 20},
	}); err != nil {
		t.Fatalf("save character: %v", err)
	}

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner() error = %v", err)
	}
	got, err := owner.ApplyMoney(ctx, MoneyCommand{
		AccountID:           "acc-7",
		SelectedCharacterID: 82,
		Direction:           MoneyWithdraw,
		Amount:              700,
	})
	if err != nil {
		t.Fatalf("ApplyMoney() error = %v", err)
	}
	if !got.Changed || got.CharacterGold != 720 || got.CargoGold != 200 {
		t.Fatalf("result = %+v", got)
	}
}
