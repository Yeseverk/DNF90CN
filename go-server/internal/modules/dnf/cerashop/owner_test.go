package cerashop

import (
	"context"
	"errors"
	"testing"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestCheckoutCommitsWalletAndAllProjectedAssets(t *testing.T) {
	ctx := context.Background()
	repositories := seededCheckoutRepositories(t, 1000)
	owner, err := NewOwner(repositories)
	if err != nil {
		t.Fatal(err)
	}
	result, err := owner.Checkout(ctx, CheckoutCommand{
		AccountID:     "account-1",
		CharacterID:   "22",
		SettingsScope: dnfrepo.CharacterContainerStateScope("22"),
		Cost:          100,
		Project: func(assets *CheckoutAssets) (CheckoutChanges, error) {
			assets.Character.Stats = map[string]int64{"name_tag_item_id": 123}
			assets.Inventory.Slots = map[string]dnfrepo.ItemStack{"0:9": {ItemID: 456, Count: 1}}
			assets.Equipment.Entries = map[string]dnfrepo.EquipmentEntry{"30": {SlotIndex: 30, ItemID: 123}}
			assets.Settings.Values = map[string]string{"main_list_param16": "8"}
			return CheckoutChanges{Character: true, Inventory: true, Equipment: true, Settings: true}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.CeraBefore != 1000 || result.CeraAfter != 900 {
		t.Fatalf("checkout result=%+v", result)
	}
	account, _, _ := repositories.Account.Load(ctx, "account-1")
	character, _, _ := repositories.Character.Load(ctx, "22")
	inventory, _, _ := repositories.Inventory.Load(ctx, "22")
	equipment, _, _ := repositories.Equipment.Load(ctx, "22")
	settings, found, _ := repositories.Settings.Load(ctx, dnfrepo.CharacterContainerStateScope("22"))
	if Balance(account) != 900 ||
		character.Stats["name_tag_item_id"] != 123 ||
		inventory.Slots["0:9"].ItemID != 456 ||
		equipment.Entries["30"].ItemID != 123 ||
		!found || settings.Values["main_list_param16"] != "8" {
		t.Fatalf("account=%+v character=%+v inventory=%+v equipment=%+v settings=%+v", account, character, inventory, equipment, settings)
	}
}

func TestCheckoutRollsBackProjectionFailure(t *testing.T) {
	ctx := context.Background()
	repositories := seededCheckoutRepositories(t, 1000)
	owner, err := NewOwner(repositories)
	if err != nil {
		t.Fatal(err)
	}
	projectErr := errors.New("projection rejected")
	_, err = owner.Checkout(ctx, CheckoutCommand{
		AccountID:     "account-1",
		CharacterID:   "22",
		SettingsScope: dnfrepo.CharacterContainerStateScope("22"),
		Cost:          100,
		Project: func(assets *CheckoutAssets) (CheckoutChanges, error) {
			assets.Inventory.Slots = make(map[string]dnfrepo.ItemStack)
			assets.Inventory.Slots["0:9"] = dnfrepo.ItemStack{ItemID: 456, Count: 1}
			assets.Settings.Values = map[string]string{"main_list_param16": "8"}
			return CheckoutChanges{Inventory: true, Settings: true}, projectErr
		},
	})
	if !errors.Is(err, projectErr) {
		t.Fatalf("error=%v want=%v", err, projectErr)
	}
	account, _, _ := repositories.Account.Load(ctx, "account-1")
	inventory, _, _ := repositories.Inventory.Load(ctx, "22")
	if Balance(account) != 1000 || len(inventory.Slots) != 0 {
		t.Fatalf("account=%+v inventory=%+v", account, inventory)
	}
	if _, found, _ := repositories.Settings.Load(ctx, dnfrepo.CharacterContainerStateScope("22")); found {
		t.Fatal("failed checkout persisted settings")
	}
}

func TestCheckoutRejectsInsufficientCeraBeforeProjection(t *testing.T) {
	repositories := seededCheckoutRepositories(t, 50)
	owner, err := NewOwner(repositories)
	if err != nil {
		t.Fatal(err)
	}
	projected := false
	_, err = owner.Checkout(context.Background(), CheckoutCommand{
		AccountID:     "account-1",
		CharacterID:   "22",
		SettingsScope: dnfrepo.CharacterContainerStateScope("22"),
		Cost:          100,
		Project: func(*CheckoutAssets) (CheckoutChanges, error) {
			projected = true
			return CheckoutChanges{}, nil
		},
	})
	if !errors.Is(err, ErrCeraInsufficient) || projected {
		t.Fatalf("error=%v projected=%t", err, projected)
	}
}

func seededCheckoutRepositories(t *testing.T, cera int64) dnfrepo.Group {
	t.Helper()
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	account := dnfrepo.AccountRecord{AccountID: "account-1"}
	SetBalance(&account, cera)
	if err := repositories.Account.Save(ctx, account); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "22",
		AccountID:   "account-1",
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "22",
		Slots:       make(map[string]dnfrepo.ItemStack),
	}); err != nil {
		t.Fatal(err)
	}
	return repositories
}
