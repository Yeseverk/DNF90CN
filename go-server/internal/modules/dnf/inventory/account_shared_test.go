package inventory

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"testing"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestMigrateAccountSharedSlotsMovesLegacyStackWithoutLosingState(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	expires := time.Date(2027, time.March, 4, 5, 6, 7, 0, time.FixedZone("CST", 8*60*60))
	legacy := dnfrepo.ItemStack{
		ItemID:   3033,
		Count:    25,
		Bind:     true,
		ExpireAt: expires,
		RawEntry: []byte{0x77, 0x35, 0x40},
		Extra:    map[string]string{"kind": "crystal", "serial": "991"},
	}
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"0:9":   {ItemID: 700, Count: 1},
			"0:354": legacy,
		},
	})
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}

	if err := owner.MigrateAccountSharedSlots(ctx, "dnf:1", "77"); err != nil {
		t.Fatalf("MigrateAccountSharedSlots error = %v", err)
	}

	character := loadTestInventory(t, ctx, repos, "77")
	if _, exists := character.Slots["0:354"]; exists {
		t.Fatalf("legacy account-shared slot remains on character: %+v", character.Slots)
	}
	if got := character.Slots["0:9"]; got.ItemID != 700 || got.Count != 1 {
		t.Fatalf("ordinary character slot changed: %+v", got)
	}
	account, exists, err := repos.AccountInventory.Load(ctx, "dnf:1")
	if err != nil || !exists {
		t.Fatalf("Load account inventory exists=%t err=%v", exists, err)
	}
	got := account.Slots["0:354"]
	if got.ItemID != legacy.ItemID || got.Count != legacy.Count || got.Bind != legacy.Bind ||
		!got.ExpireAt.Equal(legacy.ExpireAt) || !bytes.Equal(got.RawEntry, legacy.RawEntry) || !maps.Equal(got.Extra, legacy.Extra) {
		t.Fatalf("migrated stack = %+v, want complete legacy state %+v", got, legacy)
	}
}

func TestMigrateAccountSharedSlotsRemovesIdenticalCharacterDuplicate(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	stack := dnfrepo.ItemStack{
		ItemID:   4012,
		Count:    9,
		Bind:     true,
		ExpireAt: time.Date(2028, time.January, 2, 3, 4, 5, 0, time.UTC),
		RawEntry: []byte{4, 0, 1, 2},
		Extra:    map[string]string{"kind": "soul"},
	}
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots:       map[string]dnfrepo.ItemStack{"0:360": stack},
	})
	if err := repos.AccountInventory.Save(ctx, dnfrepo.AccountInventoryRecord{
		AccountID: "dnf:1",
		Slots:     map[string]dnfrepo.ItemStack{"0:360": stack},
	}); err != nil {
		t.Fatalf("Save account inventory error = %v", err)
	}
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}

	if err := owner.MigrateAccountSharedSlots(ctx, "dnf:1", "77"); err != nil {
		t.Fatalf("MigrateAccountSharedSlots error = %v", err)
	}
	if _, exists := loadTestInventory(t, ctx, repos, "77").Slots["0:360"]; exists {
		t.Fatal("identical character duplicate was not removed")
	}
	account, exists, err := repos.AccountInventory.Load(ctx, "dnf:1")
	if err != nil || !exists {
		t.Fatalf("Load account inventory exists=%t err=%v", exists, err)
	}
	if got := account.Slots["0:360"]; !accountSharedStacksEqual(got, stack) {
		t.Fatalf("account owner changed: %+v", got)
	}
}

func TestMigrateAccountSharedSlotsConflictRollsBackEverySlot(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	first := dnfrepo.ItemStack{ItemID: 3033, Count: 5, RawEntry: []byte{3, 5, 4}, Extra: map[string]string{"origin": "legacy"}}
	conflictingLegacy := dnfrepo.ItemStack{ItemID: 3034, Count: 6, RawEntry: []byte{3, 5, 5}}
	conflictingAccount := dnfrepo.ItemStack{ItemID: 9999, Count: 1, RawEntry: []byte{9}}
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"0:354": first,
			"0:355": conflictingLegacy,
		},
	})
	if err := repos.AccountInventory.Save(ctx, dnfrepo.AccountInventoryRecord{
		AccountID: "dnf:1",
		Slots:     map[string]dnfrepo.ItemStack{"0:355": conflictingAccount},
	}); err != nil {
		t.Fatalf("Save account inventory error = %v", err)
	}
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}

	err = owner.MigrateAccountSharedSlots(ctx, "dnf:1", "77")
	if !errors.Is(err, ErrAccountSharedMigrationConflict) {
		t.Fatalf("MigrateAccountSharedSlots error = %v, want ErrAccountSharedMigrationConflict", err)
	}

	character := loadTestInventory(t, ctx, repos, "77")
	if !accountSharedStacksEqual(character.Slots["0:354"], first) ||
		!accountSharedStacksEqual(character.Slots["0:355"], conflictingLegacy) {
		t.Fatalf("character slots changed after conflict: %+v", character.Slots)
	}
	account, exists, loadErr := repos.AccountInventory.Load(ctx, "dnf:1")
	if loadErr != nil || !exists {
		t.Fatalf("Load account inventory exists=%t err=%v", exists, loadErr)
	}
	if _, exists := account.Slots["0:354"]; exists {
		t.Fatalf("earlier slot partially migrated despite later conflict: %+v", account.Slots)
	}
	if !accountSharedStacksEqual(account.Slots["0:355"], conflictingAccount) {
		t.Fatalf("conflicting account slot changed: %+v", account.Slots["0:355"])
	}
}

func TestMigrateAccountSharedSlotsValidatesOwnersAndKeys(t *testing.T) {
	ctx := context.Background()
	owner, err := NewOwner(dnfrepomemory.NewMemoryGroup())
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	if err := owner.MigrateAccountSharedSlots(ctx, "", "77"); !errors.Is(err, ErrAccountRequired) {
		t.Fatalf("empty account error = %v", err)
	}
	if err := owner.MigrateAccountSharedSlots(ctx, "dnf:1", ""); !errors.Is(err, ErrCharacterRequired) {
		t.Fatalf("empty character error = %v", err)
	}
	if err := owner.MigrateAccountSharedSlots(ctx, "dnf:1", "77"); !errors.Is(err, ErrInventoryNotFound) {
		t.Fatalf("missing character error = %v", err)
	}
}
