package expertjob

import (
	"context"
	"errors"
	"testing"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestOwnerCompoundCommitsCharacterAndInventoryAtomically(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "19", AccountID: "dnf:1", Stats: map[string]int64{"expert_job_exp": 0}}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{CharacterID: "19", Slots: map[string]dnfrepo.ItemStack{"0:9": {ItemID: 3001, Count: 2}}}); err != nil {
		t.Fatal(err)
	}
	owner, err := NewOwner(repositories)
	if err != nil {
		t.Fatal(err)
	}
	err = owner.Compound(ctx, Command{AccountID: "dnf:1", CharacterID: "19", Project: func(assets *Assets) (Changes, error) {
		assets.Character.Stats["expert_job_exp"] = 3
		delete(assets.Inventory.Slots, "0:9")
		return Changes{Character: true, Inventory: true}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	character, _, _ := repositories.Character.Load(ctx, "19")
	inventory, _, _ := repositories.Inventory.Load(ctx, "19")
	if character.Stats["expert_job_exp"] != 3 || len(inventory.Slots) != 0 {
		t.Fatalf("character=%+v inventory=%+v", character, inventory)
	}
}

func TestOwnerLearnRecipeRollsBackBothAggregates(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "19", AccountID: "dnf:1", Stats: map[string]int64{}}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{CharacterID: "19", Slots: map[string]dnfrepo.ItemStack{"0:9": {ItemID: 1002, Count: 1}}}); err != nil {
		t.Fatal(err)
	}
	owner, _ := NewOwner(repositories)
	want := errors.New("late failure")
	err := owner.LearnRecipe(ctx, Command{AccountID: "dnf:1", CharacterID: "19", Project: func(assets *Assets) (Changes, error) {
		if assets.Character.Stats == nil {
			assets.Character.Stats = make(map[string]int64)
		}
		assets.Character.Stats["expert_job_recipe_2_1002"] = 1
		delete(assets.Inventory.Slots, "0:9")
		return Changes{Character: true, Inventory: true}, want
	}})
	if !errors.Is(err, want) {
		t.Fatalf("error = %v", err)
	}
	character, _, _ := repositories.Character.Load(ctx, "19")
	inventory, _, _ := repositories.Inventory.Load(ctx, "19")
	if character.Stats["expert_job_recipe_2_1002"] != 0 || inventory.Slots["0:9"].Count != 1 {
		t.Fatalf("rollback character=%+v inventory=%+v", character, inventory)
	}
}

func TestOwnerGiveUpCommitsCharacterWalletAndQuestAtomically(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "19", AccountID: "dnf:1", Stats: map[string]int64{"expert_job_type": 3, "expert_job_exp": 20}}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{CharacterID: "19", Slots: map[string]dnfrepo.ItemStack{"0:0": {ItemID: 0, Count: 60_000}}}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Quest.Save(ctx, dnfrepo.QuestRecord{CharacterID: "19", States: map[int64]dnfrepo.QuestState{2702: {Status: "completed"}, 5000: {Status: "active"}}, Progress: map[int64]dnfrepo.QuestState{2708: {Status: "active"}}}); err != nil {
		t.Fatal(err)
	}
	owner, err := NewOwner(repositories)
	if err != nil {
		t.Fatal(err)
	}
	err = owner.GiveUp(ctx, Command{AccountID: "dnf:1", CharacterID: "19", Project: func(assets *Assets) (Changes, error) {
		assets.Character.Stats["expert_job_type"] = 0
		assets.Character.Stats["expert_job_exp"] = 0
		wallet := assets.Inventory.Slots["0:0"]
		wallet.Count = 59_000
		assets.Inventory.Slots["0:0"] = wallet
		delete(assets.Quest.States, 2702)
		delete(assets.Quest.Progress, 2708)
		return Changes{Character: true, Inventory: true, Quest: true}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	character, _, _ := repositories.Character.Load(ctx, "19")
	inventory, _, _ := repositories.Inventory.Load(ctx, "19")
	quest, _, _ := repositories.Quest.Load(ctx, "19")
	if character.Stats["expert_job_type"] != 0 || inventory.Slots["0:0"].Count != 59_000 {
		t.Fatalf("character=%+v inventory=%+v", character, inventory)
	}
	if _, exists := quest.States[2702]; exists {
		t.Fatalf("transition state was not removed: %+v", quest)
	}
	if _, exists := quest.Progress[2708]; exists {
		t.Fatalf("transition progress was not removed: %+v", quest)
	}
	if quest.States[5000].Status != "active" {
		t.Fatalf("unrelated quest was changed: %+v", quest)
	}
}

func TestOwnerGiveUpRollsBackQuestWithWallet(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	_ = repositories.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "19", AccountID: "dnf:1", Stats: map[string]int64{"expert_job_type": 2}})
	_ = repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{CharacterID: "19", Slots: map[string]dnfrepo.ItemStack{"0:0": {ItemID: 0, Count: 60_000}}})
	_ = repositories.Quest.Save(ctx, dnfrepo.QuestRecord{CharacterID: "19", States: map[int64]dnfrepo.QuestState{2702: {Status: "completed"}}})
	owner, _ := NewOwner(repositories)
	want := errors.New("late give-up failure")
	err := owner.GiveUp(ctx, Command{AccountID: "dnf:1", CharacterID: "19", Project: func(assets *Assets) (Changes, error) {
		assets.Character.Stats["expert_job_type"] = 0
		wallet := assets.Inventory.Slots["0:0"]
		wallet.Count = 59_000
		assets.Inventory.Slots["0:0"] = wallet
		delete(assets.Quest.States, 2702)
		return Changes{Character: true, Inventory: true, Quest: true}, want
	}})
	if !errors.Is(err, want) {
		t.Fatalf("error=%v", err)
	}
	character, _, _ := repositories.Character.Load(ctx, "19")
	inventory, _, _ := repositories.Inventory.Load(ctx, "19")
	quest, _, _ := repositories.Quest.Load(ctx, "19")
	if character.Stats["expert_job_type"] != 2 || inventory.Slots["0:0"].Count != 60_000 || quest.States[2702].Status != "completed" {
		t.Fatalf("rollback character=%+v inventory=%+v quest=%+v", character, inventory, quest)
	}
}
