package memory

import (
	"context"
	"longheng.io/server/internal/modules/dnf/repository"
	"testing"
)

func TestCeraShopAssetTransactionRejectsCrossSettingsScope(t *testing.T) {
	ctx := context.Background()
	repositories := NewMemoryGroup()
	if err := repositories.Account.Save(ctx, repository.AccountRecord{
		AccountID: "account-1",
		Metadata:  map[string]string{"account_cera": "1000"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Character.Save(ctx, repository.CharacterRecord{
		CharacterID: "22",
		AccountID:   "account-1",
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, repository.InventoryRecord{CharacterID: "22"}); err != nil {
		t.Fatal(err)
	}

	err := repositories.CeraShopAssets.WithinCeraShopAssets(
		ctx,
		"account-1",
		"22",
		repository.CharacterContainerStateScope("22"),
		func(
			accounts repository.AccountRepository,
			_ repository.CharacterRepository,
			_ repository.InventoryRepository,
			_ repository.EquipmentRepository,
			settings repository.SettingsRepository,
		) error {
			account, _, err := accounts.Load(ctx, "account-1")
			if err != nil {
				return err
			}
			account.Metadata["account_cera"] = "900"
			if err := accounts.Save(ctx, account); err != nil {
				return err
			}
			return settings.Save(ctx, repository.SettingsRecord{
				Scope:  repository.CharacterContainerStateScope("23"),
				Values: map[string]string{"main_list_param16": "8"},
			})
		},
	)
	if err == nil {
		t.Fatal("cross-character settings mutation was accepted")
	}
	account, found, err := repositories.Account.Load(ctx, "account-1")
	if err != nil || !found || account.Metadata["account_cera"] != "1000" {
		t.Fatalf("account=%+v found=%t err=%v", account, found, err)
	}
	if _, found, err := repositories.Settings.Load(ctx, repository.CharacterContainerStateScope("23")); err != nil || found {
		t.Fatalf("forged settings found=%t err=%v", found, err)
	}
}
