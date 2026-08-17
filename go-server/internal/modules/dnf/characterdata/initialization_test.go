package characterdata

import (
	"context"
	"testing"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestInitializerCreatesOnlyMissingRecords(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	now := time.Unix(100, 0).UTC()
	initialization := Initialization{
		CharacterID: "77",
		Inventory: &dnfrepo.InventoryRecord{
			CharacterID: "77",
			Slots:       map[string]dnfrepo.ItemStack{"0:1": {ItemID: 1, Count: 0}},
			Warehouse:   map[string]dnfrepo.ItemStack{},
			UpdatedAt:   now,
		},
		Equipment: &dnfrepo.EquipmentRecord{
			CharacterID: "77",
			Entries:     map[string]dnfrepo.EquipmentEntry{"11": {SlotIndex: 11, ItemID: 1001}},
			UpdatedAt:   now,
		},
		Skill: &dnfrepo.SkillRecord{
			CharacterID: "77",
			Skills:      map[int64]dnfrepo.SkillState{10: {Level: 1, Enabled: true}},
			Cooldowns:   map[int64]time.Time{},
			UpdatedAt:   now,
		},
		Settings: []dnfrepo.SettingsRecord{{
			Scope:     "character:77:init",
			Values:    map[string]string{"source": "initial"},
			UpdatedAt: now,
		}},
	}

	result, err := NewInitializer(repositories).Ensure(ctx, initialization)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Inventory || !result.Equipment || !result.Skill || len(result.Settings) != 1 || result.Settings[0] != "character:77:init" {
		t.Fatalf("initialization result = %+v", result)
	}

	// A later PVF/default change must not reset durable player state. The
	// initializer is allowed to insert absent records only, never replace one.
	replacement := initialization
	replacement.Inventory = &dnfrepo.InventoryRecord{CharacterID: "77", Slots: map[string]dnfrepo.ItemStack{"0:1": {ItemID: 2, Count: 99}}}
	replacement.Equipment = &dnfrepo.EquipmentRecord{CharacterID: "77", Entries: map[string]dnfrepo.EquipmentEntry{"11": {SlotIndex: 11, ItemID: 2002}}}
	replacement.Skill = &dnfrepo.SkillRecord{CharacterID: "77", Skills: map[int64]dnfrepo.SkillState{20: {Level: 9, Enabled: true}}}
	replacement.Settings = []dnfrepo.SettingsRecord{{Scope: "character:77:init", Values: map[string]string{"source": "replacement"}}}
	result, err = NewInitializer(repositories).Ensure(ctx, replacement)
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed() {
		t.Fatalf("present initialization records were changed: %+v", result)
	}

	inventory, _, _ := repositories.Inventory.Load(ctx, "77")
	if got := inventory.Slots["0:1"]; got.ItemID != 1 || got.Count != 0 {
		t.Fatalf("inventory overwritten: %+v", inventory)
	}
	equipment, _, _ := repositories.Equipment.Load(ctx, "77")
	if got := equipment.Entries["11"].ItemID; got != 1001 {
		t.Fatalf("equipment overwritten: %+v", equipment)
	}
	skill, _, _ := repositories.Skill.Load(ctx, "77")
	if _, found := skill.Skills[10]; !found || len(skill.Skills) != 1 {
		t.Fatalf("skill overwritten: %+v", skill)
	}
	setting, _, _ := repositories.Settings.Load(ctx, "character:77:init")
	if setting.Values["source"] != "initial" {
		t.Fatalf("setting overwritten: %+v", setting)
	}
}
