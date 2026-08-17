package town

import (
	"context"
	"errors"
	"testing"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestOwnerApplyLoginLocationCommitsProjectedStats(t *testing.T) {
	ctx := context.Background()
	repositories := seededTownOwnerRepositories(t, ctx)
	owner, err := NewOwner(repositories)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(2_000_000_000, 0).UTC()
	result, err := owner.ApplyLoginLocation(ctx, LoginLocationCommand{
		AccountID:   "account-1",
		CharacterID: "19",
		UpdatedAt:   now,
		Project: func(character *dnfrepo.CharacterRecord) (bool, error) {
			character.Stats["town_id"] = 38
			character.Stats["area_id"] = 1
			return true, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.Character.Stats["town_id"] != 38 || !result.Character.UpdatedAt.Equal(now) {
		t.Fatalf("result=%+v", result)
	}
	stored, found, err := repositories.Character.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load found=%t err=%v", found, err)
	}
	if stored.Stats["town_id"] != 38 || stored.Stats["area_id"] != 1 || !stored.UpdatedAt.Equal(now) {
		t.Fatalf("stored=%+v", stored)
	}
}

func TestOwnerApplyLoginLocationRollsBackProjectorFailure(t *testing.T) {
	ctx := context.Background()
	repositories := seededTownOwnerRepositories(t, ctx)
	owner, err := NewOwner(repositories)
	if err != nil {
		t.Fatal(err)
	}
	projectErr := errors.New("projection failed")
	_, err = owner.ApplyLoginLocation(ctx, LoginLocationCommand{
		AccountID:   "account-1",
		CharacterID: "19",
		Project: func(character *dnfrepo.CharacterRecord) (bool, error) {
			character.Stats["town_id"] = 38
			return true, projectErr
		},
	})
	if !errors.Is(err, projectErr) {
		t.Fatalf("err=%v", err)
	}
	stored, _, _ := repositories.Character.Load(ctx, "19")
	if stored.Stats["town_id"] != 39 {
		t.Fatalf("projector failure committed: %+v", stored)
	}
}

func TestOwnerApplyLoginLocationRejectsCrossAccountCharacter(t *testing.T) {
	ctx := context.Background()
	repositories := seededTownOwnerRepositories(t, ctx)
	owner, err := NewOwner(repositories)
	if err != nil {
		t.Fatal(err)
	}
	projected := false
	_, err = owner.ApplyLoginLocation(ctx, LoginLocationCommand{
		AccountID:   "account-2",
		CharacterID: "19",
		Project: func(*dnfrepo.CharacterRecord) (bool, error) {
			projected = true
			return false, nil
		},
	})
	if !errors.Is(err, ErrAccountMismatch) {
		t.Fatalf("err=%v", err)
	}
	if projected {
		t.Fatal("cross-account command reached projector")
	}
}

func seededTownOwnerRepositories(t *testing.T, ctx context.Context) dnfrepo.Group {
	t.Helper()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{
		AccountID:   "account-1",
		CharacterID: "19",
		Stats: map[string]int64{
			"town_id": 39,
			"area_id": 0,
		},
	}); err != nil {
		t.Fatal(err)
	}
	return repositories
}
