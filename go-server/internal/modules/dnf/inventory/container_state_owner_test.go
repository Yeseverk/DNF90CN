package inventory

import (
	"context"
	"errors"
	"testing"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestContainerStateOwnerEnsureCreatesValidatedMissingState(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	owner, err := NewContainerStateOwner(repositories.Settings)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(2_000_000_000, 0).UTC()
	result, err := owner.Ensure(ctx, EnsureContainerStateCommand{
		CharacterID: "88",
		UpdatedAt:   now,
		Initial:     testInitialContainerState,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Created || result.State.MainSlotCount != 24 || result.State.PersonalCargoSlotCount != 8 ||
		!result.State.UpdatedAt.Equal(now) {
		t.Fatalf("result=%+v", result)
	}
}

func TestContainerStateOwnerEnsurePreservesExistingState(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	existing := testInitialContainerState("88", time.Unix(1_900_000_000, 0).UTC())
	existing.Values["main_list_param16"] = "16"
	if err := repositories.Settings.Save(ctx, existing); err != nil {
		t.Fatal(err)
	}
	owner, err := NewContainerStateOwner(repositories.Settings)
	if err != nil {
		t.Fatal(err)
	}
	factoryCalled := false
	result, err := owner.Ensure(ctx, EnsureContainerStateCommand{
		CharacterID: "88",
		Initial: func(characterID string, now time.Time) dnfrepo.SettingsRecord {
			factoryCalled = true
			return testInitialContainerState(characterID, now)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Created || result.State.MainSlotCount != 16 || factoryCalled {
		t.Fatalf("result=%+v factory_called=%t", result, factoryCalled)
	}
}

func TestContainerStateOwnerEnsureRejectsWrongScopeAndInvalidValues(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	owner, err := NewContainerStateOwner(repositories.Settings)
	if err != nil {
		t.Fatal(err)
	}
	_, err = owner.Ensure(ctx, EnsureContainerStateCommand{
		CharacterID: "88",
		Initial: func(characterID string, now time.Time) dnfrepo.SettingsRecord {
			record := testInitialContainerState(characterID, now)
			record.Scope = dnfrepo.CharacterContainerStateScope("89")
			return record
		},
	})
	if !errors.Is(err, ErrContainerStateScopeMismatch) {
		t.Fatalf("scope err=%v", err)
	}
	_, found, loadErr := repositories.Settings.Load(ctx, dnfrepo.CharacterContainerStateScope("88"))
	if loadErr != nil || found {
		t.Fatalf("wrong-scope row persisted found=%t err=%v", found, loadErr)
	}

	_, err = owner.Ensure(ctx, EnsureContainerStateCommand{
		CharacterID: "88",
		Initial: func(characterID string, now time.Time) dnfrepo.SettingsRecord {
			record := testInitialContainerState(characterID, now)
			record.Values["personal_cargo_list_param16"] = "9"
			return record
		},
	})
	if !errors.Is(err, dnfrepo.ErrCharacterContainerStateInvalid) {
		t.Fatalf("invalid-value err=%v", err)
	}
}

func testInitialContainerState(characterID string, now time.Time) dnfrepo.SettingsRecord {
	return dnfrepo.SettingsRecord{
		Scope: dnfrepo.CharacterContainerStateScope(characterID),
		Values: map[string]string{
			"source":                      "test",
			"main_list_param16":           "24",
			"avatar_list_param16":         "0",
			"personal_cargo_list_param16": "8",
			"account_cargo_selection_key": "0",
			"account_cargo_value32":       "0",
		},
		UpdatedAt: now,
	}
}
