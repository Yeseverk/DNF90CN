package achievement

import (
	"context"
	"testing"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

type definitionStub Definition

func (r definitionStub) ResolveAchievementDefinition(context.Context, int32) (Definition, error) {
	return Definition(r), nil
}

func TestOwnerTriggerPersistsRealTargetAndGrantsTitleOnce(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots:       map[string]dnfrepo.ItemStack{},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	owner, err := NewOwner(repos, definitionStub{
		QuestID: 6533,
		Target1: 4,
		Reward: TitleReward{
			ItemID:    9001,
			Category:  2,
			BookIndex: 7,
		},
	})
	if err != nil {
		t.Fatalf("NewOwner: %v", err)
	}

	first, err := owner.Trigger(ctx, Command{
		SelectedCharacterID: 19,
		QuestID:             6533,
		Delta1:              1,
	})
	if err != nil {
		t.Fatalf("first Trigger: %v", err)
	}
	if first.Remain1 != 3 || first.Completed {
		t.Fatalf("first result = %+v", first)
	}
	second, err := owner.Trigger(ctx, Command{
		SelectedCharacterID: 19,
		QuestID:             6533,
		Delta1:              3,
	})
	if err != nil {
		t.Fatalf("second Trigger: %v", err)
	}
	if !second.Completed || !second.TitleGranted || second.TitleSlot != 2007 {
		t.Fatalf("second result = %+v", second)
	}
	third, err := owner.Trigger(ctx, Command{
		SelectedCharacterID: 19,
		QuestID:             6533,
		Delta1:              1,
	})
	if err != nil {
		t.Fatalf("third Trigger: %v", err)
	}
	if third.Completed || third.TitleGranted || !third.AlreadyCompleted ||
		third.Remain1 != 0 || third.Remain2 != 0 || third.Remain3 != 0 {
		t.Fatalf("third result = %+v", third)
	}

	inventory, found, err := repos.Inventory.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load inventory found=%t err=%v", found, err)
	}
	if title := inventory.Slots["100:2007"]; title.ItemID != 9001 || title.Count != 1 {
		t.Fatalf("title = %+v", title)
	}
	for key, stack := range inventory.Slots {
		if IsLegacyInventoryProgress(key, stack) {
			t.Fatalf("achievement progress leaked into title inventory: %s=%+v", key, stack)
		}
	}
	quests, found, err := repos.Quest.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load quests found=%t err=%v", found, err)
	}
	progress := Snapshot(quests)
	if len(progress) != 1 || progress[0].QuestID != 6533 || !progress[0].Completed {
		t.Fatalf("progress = %+v", progress)
	}
}

func TestOwnerTriggerFallsBackToFirstFreeTitleSlot(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			"100:2007": {ItemID: 8001, Count: 1},
			"100:2000": {ItemID: 8002, Count: 1},
		},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	owner, err := NewOwner(repos, definitionStub{
		QuestID: 6533,
		Target1: 1,
		Reward:  TitleReward{ItemID: 9001, Category: 2, BookIndex: 7},
	})
	if err != nil {
		t.Fatalf("NewOwner: %v", err)
	}
	result, err := owner.Trigger(ctx, Command{SelectedCharacterID: 19, QuestID: 6533, Delta1: 1})
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if result.TitleSlot != 2001 {
		t.Fatalf("title slot = %d, want 2001", result.TitleSlot)
	}
}

func TestRepairLegacyRowsRemovesOnlyBrokenProgressSignature(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			"100:933": {
				ItemID: 6533,
				Count:  1,
				Extra: map[string]string{
					"initialized": "1",
					"quest_id":    "6533",
					"remain1":     "1",
				},
			},
			"100:7": {ItemID: 9001, Count: 1},
		},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	owner, err := NewOwner(repos, nil)
	if err != nil {
		t.Fatalf("NewOwner: %v", err)
	}
	removed, err := owner.RepairLegacyRows(ctx, 19)
	if err != nil {
		t.Fatalf("RepairLegacyRows: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	inventory, _, _ := repos.Inventory.Load(ctx, "19")
	if _, found := inventory.Slots["100:933"]; found {
		t.Fatalf("legacy progress survived: %+v", inventory.Slots["100:933"])
	}
	if inventory.Slots["100:7"].ItemID != 9001 {
		t.Fatalf("real title was removed: %+v", inventory.Slots)
	}
}
