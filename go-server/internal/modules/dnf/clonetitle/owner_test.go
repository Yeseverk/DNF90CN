package clonetitle

import (
	"context"
	"errors"
	"testing"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestOwnerApplyPersistsCloneTitle(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "account-1",
		Stats:       map[string]int64{"emotion_index": 3},
	}); err != nil {
		t.Fatalf("save character: %v", err)
	}
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			"100:0": {ItemID: 123456, Count: 1},
		},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner: %v", err)
	}
	result, err := owner.Apply(ctx, Command{
		AccountID:           "account-1",
		SelectedCharacterID: 19,
		ItemID:              123456,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.CharacterID != "19" || result.ItemID != 123456 {
		t.Fatalf("result = %+v", result)
	}
	character, found, err := repos.Character.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load character found=%t err=%v", found, err)
	}
	if character.Stats["clone_title_item_id"] != 123456 ||
		character.Stats["emotion_index"] != 3 {
		t.Fatalf("stats = %+v", character.Stats)
	}
}

func TestOwnerApplyRejectsUnownedCloneTitle(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "account-1",
	}); err != nil {
		t.Fatalf("save character: %v", err)
	}
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			"0:9": {ItemID: 123456, Count: 1},
		},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner: %v", err)
	}
	_, err = owner.Apply(ctx, Command{
		AccountID:           "account-1",
		SelectedCharacterID: 19,
		ItemID:              123456,
	})
	if !errors.Is(err, ErrTitleNotOwned) {
		t.Fatalf("Apply error = %v, want ErrTitleNotOwned", err)
	}
}

func TestOwnerApplyZeroClearsCloneTitleWithoutBookItem(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "account-1",
		Stats:       map[string]int64{statKey: 123456},
	}); err != nil {
		t.Fatalf("save character: %v", err)
	}
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots:       map[string]dnfrepo.ItemStack{},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner: %v", err)
	}
	if _, err := owner.Apply(ctx, Command{
		AccountID:           "account-1",
		SelectedCharacterID: 19,
		ItemID:              0,
	}); err != nil {
		t.Fatalf("Apply clear: %v", err)
	}
	character, found, err := repos.Character.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load character found=%t err=%v", found, err)
	}
	if got := character.Stats[statKey]; got != 0 {
		t.Fatalf("%s = %d, want 0", statKey, got)
	}
}

func TestOwnerApplyRejectsMissingCharacter(t *testing.T) {
	repos := dnfrepomemory.NewMemoryGroup()
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner: %v", err)
	}
	_, err = owner.Apply(context.Background(), Command{SelectedCharacterID: 19})
	if !errors.Is(err, ErrCharacterNotFound) {
		t.Fatalf("Apply error = %v, want ErrCharacterNotFound", err)
	}
}
