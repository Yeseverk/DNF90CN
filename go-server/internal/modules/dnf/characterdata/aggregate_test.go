package characterdata

import (
	"context"
	"errors"
	"testing"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestLoaderLoadsRequestedCharacterParts(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	character := dnfrepo.CharacterRecord{
		CharacterID: "17",
		AccountID:   "account-1",
		Name:        "hero",
		Level:       1,
		Stats:       map[string]int64{"strength": 21},
	}
	if err := repositories.Character.Save(ctx, character); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Equipment.Save(ctx, dnfrepo.EquipmentRecord{
		CharacterID: "17",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"11": {SlotIndex: 11, ItemID: 1001, RawEntry: []byte{1, 2, 3}},
		},
	}); err != nil {
		t.Fatal(err)
	}

	aggregate, found, err := NewLoader(repositories).Load(ctx, "17", PartEquipment|PartSkill)
	if err != nil || !found {
		t.Fatalf("Load() found=%v err=%v", found, err)
	}
	if !aggregate.Presence.Equipment || aggregate.Equipment.Entries["11"].ItemID != 1001 {
		t.Fatalf("equipment not loaded: %+v", aggregate)
	}
	if aggregate.Presence.Skill || aggregate.Skill.CharacterID != "17" {
		t.Fatalf("missing skill presence not preserved: %+v", aggregate)
	}

	character.Stats["strength"] = 999
	entry := aggregate.Equipment.Entries["11"]
	entry.RawEntry[0] = 9
	stored, _, err := repositories.Equipment.Load(ctx, "17")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Entries["11"].RawEntry[0] != 1 {
		t.Fatalf("aggregate shares mutable equipment bytes: %v", stored.Entries["11"].RawEntry)
	}
}

func TestCreatorPersistsCompleteInitialization(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	now := time.Unix(100, 0).UTC()
	inventory := dnfrepo.InventoryRecord{Slots: map[string]dnfrepo.ItemStack{"0:1": {ItemID: 1}}}
	equipment := dnfrepo.EquipmentRecord{Entries: map[string]dnfrepo.EquipmentEntry{"11": {SlotIndex: 11, ItemID: 1001}}}
	skill := dnfrepo.SkillRecord{Skills: map[int64]dnfrepo.SkillState{10: {Level: 1, Enabled: true}}}
	err := NewCreator(repositories).Create(ctx, Creation{
		Account: &dnfrepo.AccountRecord{AccountID: "account-1", State: "active"},
		Character: dnfrepo.CharacterRecord{
			CharacterID: "21",
			AccountID:   "account-1",
			Slot:        0,
			Name:        "hero",
			Level:       1,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		Inventory: &inventory,
		Equipment: &equipment,
		Skill:     &skill,
		Settings: []dnfrepo.SettingsRecord{{
			Scope:  dnfrepo.CharacterContainerStateScope("21"),
			Values: map[string]string{"main_slots": "24"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := repositories.Character.Load(ctx, "21"); err != nil || !found {
		t.Fatalf("character found=%v err=%v", found, err)
	}
	for name, load := range map[string]func() (bool, error){
		"inventory": func() (bool, error) { _, found, err := repositories.Inventory.Load(ctx, "21"); return found, err },
		"equipment": func() (bool, error) { _, found, err := repositories.Equipment.Load(ctx, "21"); return found, err },
		"skill":     func() (bool, error) { _, found, err := repositories.Skill.Load(ctx, "21"); return found, err },
		"settings": func() (bool, error) {
			_, found, err := repositories.Settings.Load(ctx, dnfrepo.CharacterContainerStateScope("21"))
			return found, err
		},
	} {
		found, err := load()
		if err != nil || !found {
			t.Fatalf("%s found=%v err=%v", name, found, err)
		}
	}
}

func TestCreatorPreservesExistingAccountHonorExperience(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	createdAt := time.Unix(50, 0).UTC()
	if err := repositories.Account.Save(ctx, dnfrepo.AccountRecord{
		AccountID: "account-1",
		State:     "existing",
		HonorExp:  17699999999,
		Metadata:  map[string]string{"owner": "existing"},
		CreatedAt: createdAt,
	}); err != nil {
		t.Fatal(err)
	}
	err := NewCreator(repositories).Create(ctx, Creation{
		Account: &dnfrepo.AccountRecord{
			AccountID: "account-1",
			State:     "active",
			HonorExp:  0,
			Metadata:  map[string]string{"source": "dnfbridge"},
		},
		Character: dnfrepo.CharacterRecord{
			CharacterID: "22",
			AccountID:   "account-1",
			Slot:        1,
			Level:       1,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	account, found, err := repositories.Account.Load(ctx, "account-1")
	if err != nil || !found {
		t.Fatalf("load account: found=%v err=%v", found, err)
	}
	if account.HonorExp != 17699999999 || account.State != "active" || account.CreatedAt != createdAt {
		t.Fatalf("account after character creation = %+v", account)
	}
	if account.Metadata["owner"] != "existing" || account.Metadata["source"] != "dnfbridge" {
		t.Fatalf("account metadata after character creation = %+v", account.Metadata)
	}
}

func TestCreatorRejectsMismatchedChildBeforeWrite(t *testing.T) {
	repositories := dnfrepomemory.NewMemoryGroup()
	inventory := dnfrepo.InventoryRecord{CharacterID: "other"}
	err := NewCreator(repositories).Create(context.Background(), Creation{
		Character: dnfrepo.CharacterRecord{CharacterID: "31", AccountID: "account-1"},
		Inventory: &inventory,
	})
	if !errors.Is(err, ErrChildCharacterMismatch) {
		t.Fatalf("Create() error=%v, want ErrChildCharacterMismatch", err)
	}
	if _, found, loadErr := repositories.Character.Load(context.Background(), "31"); loadErr != nil || found {
		t.Fatalf("mismatched creation wrote character: found=%v err=%v", found, loadErr)
	}
}
