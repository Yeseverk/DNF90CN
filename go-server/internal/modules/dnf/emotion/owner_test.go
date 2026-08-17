package emotion

import (
	"context"
	"errors"
	"testing"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestOwnerApplyPersistsEmotion(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "account-1",
		Stats:       map[string]int64{"gold": 100},
	}); err != nil {
		t.Fatalf("save character: %v", err)
	}
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner: %v", err)
	}
	updatedAt := time.Date(2026, 7, 26, 12, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	result, err := owner.Apply(ctx, Command{
		AccountID:           "account-1",
		SelectedCharacterID: 19,
		EmotionIndex:        7,
		UpdatedAt:           updatedAt,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.CharacterID != "19" || result.EmotionIndex != 7 {
		t.Fatalf("result = %+v", result)
	}
	character, found, err := repos.Character.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load character found=%t err=%v", found, err)
	}
	if character.Stats["emotion_index"] != 7 || character.Stats["gold"] != 100 {
		t.Fatalf("stats = %+v", character.Stats)
	}
}

func TestOwnerApplyRejectsAnotherAccount(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "account-1",
	}); err != nil {
		t.Fatalf("save character: %v", err)
	}
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner: %v", err)
	}
	_, err = owner.Apply(ctx, Command{
		AccountID:           "account-2",
		SelectedCharacterID: 19,
		EmotionIndex:        7,
	})
	if !errors.Is(err, ErrAccountMismatch) {
		t.Fatalf("Apply error = %v, want ErrAccountMismatch", err)
	}
}
