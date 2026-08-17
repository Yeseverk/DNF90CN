package equip

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestOwnerCleanupExpiredNameTagCommitsCharacterAndEquipment(t *testing.T) {
	ctx := context.Background()
	repositories := seededExpiredNameTagRepositories(t, ctx)
	owner, err := NewOwner(repositories)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(2_000_000_000, 0).UTC()
	result, err := owner.CleanupExpiredNameTag(ctx, CleanupExpiredNameTagCommand{
		AccountID:   "account-1",
		CharacterID: "19",
		SlotIndex:   30,
		Now:         now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || !result.EquipmentRemoved || result.ItemID != 9003 {
		t.Fatalf("result=%+v", result)
	}
	character, _, _ := repositories.Character.Load(ctx, "19")
	if character.Stats["name_tag_item_id"] != 0 ||
		character.Stats["name_tag_expire_time"] != 0 ||
		!character.UpdatedAt.Equal(now) {
		t.Fatalf("character=%+v", character)
	}
	equipment, _, _ := repositories.Equipment.Load(ctx, "19")
	if _, exists := equipment.Entries[strconv.Itoa(30)]; exists || !equipment.UpdatedAt.Equal(now) {
		t.Fatalf("equipment=%+v", equipment)
	}
}

func TestOwnerCleanupExpiredNameTagPreservesLiveCard(t *testing.T) {
	ctx := context.Background()
	repositories := seededExpiredNameTagRepositories(t, ctx)
	owner, err := NewOwner(repositories)
	if err != nil {
		t.Fatal(err)
	}
	result, err := owner.CleanupExpiredNameTag(ctx, CleanupExpiredNameTagCommand{
		AccountID:   "account-1",
		CharacterID: "19",
		SlotIndex:   30,
		Now:         time.Unix(1_800_000_000, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed || result.EquipmentRemoved {
		t.Fatalf("result=%+v", result)
	}
	character, _, _ := repositories.Character.Load(ctx, "19")
	if character.Stats["name_tag_item_id"] != 9003 {
		t.Fatalf("character=%+v", character)
	}
	equipment, _, _ := repositories.Equipment.Load(ctx, "19")
	if _, exists := equipment.Entries[strconv.Itoa(30)]; !exists {
		t.Fatalf("equipment=%+v", equipment)
	}
}

func TestOwnerCleanupExpiredNameTagRejectsCrossAccountCharacter(t *testing.T) {
	ctx := context.Background()
	repositories := seededExpiredNameTagRepositories(t, ctx)
	owner, err := NewOwner(repositories)
	if err != nil {
		t.Fatal(err)
	}
	_, err = owner.CleanupExpiredNameTag(ctx, CleanupExpiredNameTagCommand{
		AccountID:   "account-2",
		CharacterID: "19",
		SlotIndex:   30,
		Now:         time.Unix(2_000_000_000, 0).UTC(),
	})
	if !errors.Is(err, ErrNameTagAccountMismatch) {
		t.Fatalf("err=%v", err)
	}
	character, _, _ := repositories.Character.Load(ctx, "19")
	if character.Stats["name_tag_item_id"] != 9003 {
		t.Fatalf("cross-account cleanup committed: %+v", character)
	}
}

func seededExpiredNameTagRepositories(t *testing.T, ctx context.Context) dnfrepo.Group {
	t.Helper()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{
		AccountID:   "account-1",
		CharacterID: "19",
		Stats: map[string]int64{
			"name_tag_item_id":     9003,
			"name_tag_expire_time": 1_900_000_000,
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Equipment.Save(ctx, dnfrepo.EquipmentRecord{
		CharacterID: "19",
		Entries: map[string]dnfrepo.EquipmentEntry{
			strconv.Itoa(30): {SlotIndex: 30, ItemID: 9003},
		},
	}); err != nil {
		t.Fatal(err)
	}
	return repositories
}
