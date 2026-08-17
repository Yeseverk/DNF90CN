package adventuregroup

import (
	"context"
	"errors"
	"testing"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestOwnerRememberSelectorSlotUpdatesOnlyOwnedMetadataKey(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	now := time.Unix(2_000_000_000, 0).UTC()
	if err := repositories.Account.Save(ctx, dnfrepo.AccountRecord{
		AccountID:            "account-1",
		State:                "active",
		HonorExp:             77,
		RepresentAccountName: "group",
		Metadata: map[string]string{
			"account_cera": "123",
		},
	}); err != nil {
		t.Fatal(err)
	}
	owner, err := NewOwner(repositories)
	if err != nil {
		t.Fatal(err)
	}
	result, err := owner.RememberSelectorSlot(ctx, RememberSelectorSlotCommand{
		AccountID: "account-1",
		Slot:      4,
		SlotLimit: 32,
		UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.Slot != 4 {
		t.Fatalf("result=%+v", result)
	}
	account, found, err := repositories.Account.Load(ctx, "account-1")
	if err != nil || !found {
		t.Fatalf("load found=%t err=%v", found, err)
	}
	if account.Metadata[SelectorSlotMetadataKey] != "4" ||
		account.Metadata["account_cera"] != "123" ||
		account.HonorExp != 77 ||
		account.RepresentAccountName != "group" ||
		!account.UpdatedAt.Equal(now) {
		t.Fatalf("account=%+v", account)
	}
}

func TestOwnerRememberSelectorSlotPreservesExistingValue(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Account.Save(ctx, dnfrepo.AccountRecord{
		AccountID: "account-1",
		Metadata:  map[string]string{SelectorSlotMetadataKey: "4"},
	}); err != nil {
		t.Fatal(err)
	}
	owner, _ := NewOwner(repositories)
	result, err := owner.RememberSelectorSlot(ctx, RememberSelectorSlotCommand{
		AccountID: "account-1",
		Slot:      4,
		SlotLimit: 32,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed {
		t.Fatalf("result=%+v", result)
	}
}

func TestOwnerRememberSelectorSlotRejectsInvalidScope(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Account.Save(ctx, dnfrepo.AccountRecord{AccountID: "account-1"}); err != nil {
		t.Fatal(err)
	}
	owner, _ := NewOwner(repositories)
	if _, err := owner.RememberSelectorSlot(ctx, RememberSelectorSlotCommand{
		AccountID: "account-1",
		Slot:      32,
		SlotLimit: 32,
	}); !errors.Is(err, ErrSelectorSlotRange) {
		t.Fatalf("slot error=%v", err)
	}
	if _, err := owner.RememberSelectorSlot(ctx, RememberSelectorSlotCommand{
		AccountID: "missing",
		Slot:      1,
		SlotLimit: 32,
	}); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("account error=%v", err)
	}
}
