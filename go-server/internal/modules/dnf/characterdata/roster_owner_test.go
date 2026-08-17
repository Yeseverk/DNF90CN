package characterdata

import (
	"context"
	"errors"
	"testing"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestRosterOwnerSwapsOnlyTheRequestedAccountsOccupiedSlots(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	for _, record := range []dnfrepo.CharacterRecord{
		{CharacterID: "10", AccountID: "acc", Slot: 2, Name: "left"},
		{CharacterID: "20", AccountID: "acc", Slot: 7, Name: "right"},
		{CharacterID: "30", AccountID: "other", Slot: 2, Name: "other"},
	} {
		if err := repositories.Character.Save(ctx, record); err != nil {
			t.Fatalf("save character %s: %v", record.CharacterID, err)
		}
	}
	owner, err := NewRosterOwner(repositories)
	if err != nil {
		t.Fatalf("NewRosterOwner error = %v", err)
	}
	if err := owner.SwapSlots(ctx, "acc", 2, 7); err != nil {
		t.Fatalf("SwapSlots error = %v", err)
	}
	assertCharacterSlot(t, ctx, repositories.Character, "10", 7)
	assertCharacterSlot(t, ctx, repositories.Character, "20", 2)
	assertCharacterSlot(t, ctx, repositories.Character, "30", 2)
}

func TestRosterOwnerPreservesAnOccupiedSlotWhenTheOtherSlotIsEmpty(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "10",
		AccountID:   "acc",
		Slot:        2,
	}); err != nil {
		t.Fatalf("save character: %v", err)
	}
	owner, err := NewRosterOwner(repositories)
	if err != nil {
		t.Fatalf("NewRosterOwner error = %v", err)
	}
	if err := owner.SwapSlots(ctx, "acc", 2, 7); err != nil {
		t.Fatalf("SwapSlots error = %v", err)
	}
	assertCharacterSlot(t, ctx, repositories.Character, "10", 2)
}

func TestRosterOwnerRejectsUnavailableRepositoryAndInvalidSlot(t *testing.T) {
	if _, err := NewRosterOwner(dnfrepo.Group{}); !errors.Is(err, ErrRosterOwnerUnavailable) {
		t.Fatalf("NewRosterOwner error = %v, want %v", err, ErrRosterOwnerUnavailable)
	}
	owner, err := NewRosterOwner(dnfrepomemory.NewMemoryGroup())
	if err != nil {
		t.Fatalf("NewRosterOwner error = %v", err)
	}
	if err := owner.SwapSlots(context.Background(), "acc", -1, 7); !errors.Is(err, dnfrepo.ErrCharacterSlotMissing) {
		t.Fatalf("SwapSlots error = %v, want %v", err, dnfrepo.ErrCharacterSlotMissing)
	}
}

func assertCharacterSlot(t *testing.T, ctx context.Context, repository dnfrepo.CharacterRepository, characterID string, want int) {
	t.Helper()
	record, found, err := repository.Load(ctx, characterID)
	if err != nil {
		t.Fatalf("load character %s: %v", characterID, err)
	}
	if !found {
		t.Fatalf("character %s not found", characterID)
	}
	if record.Slot != want {
		t.Fatalf("character %s slot = %d, want %d", characterID, record.Slot, want)
	}
}
