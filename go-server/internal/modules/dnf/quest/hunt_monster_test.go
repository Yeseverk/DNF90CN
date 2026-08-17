package quest

import (
	"context"
	"testing"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestPlanHuntMonsterKillUsesPVFStrideFourAndMarksPendingReward(t *testing.T) {
	completedAt := time.Date(2026, 7, 23, 22, 20, 0, 0, time.UTC)
	catalog := &Catalog{byID: map[int64]Definition{
		2635: {ID: 2635, Path: "n_quest/earring.qst", Type: "[hunt monster]", IntData: []int64{
			311, 2, 9600, 1,
			311, 2, 9601, 1,
			311, 2, 9602, 1,
		}},
	}}
	record := dnfrepo.QuestRecord{
		CharacterID: "19",
		States: map[int64]dnfrepo.QuestState{
			2635: {Status: "active", ProgressValue: int64(packTriggerChannels(0, 0, 1)), Extra: map[string]string{"kept": "yes"}},
		},
	}

	plan, err := catalog.PlanHuntMonsterKill(record, HuntMonsterKillInput{
		DungeonID:     311,
		Difficulty:    2,
		MonsterCode:   9602,
		CompletionKey: "run-19/op39/9602",
		CompletedAt:   completedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.ChangedFields) != 1 || plan.ChangedFields[0] != dnfrepo.QuestFieldStates {
		t.Fatalf("changed fields=%v", plan.ChangedFields)
	}
	if len(plan.Completions) != 1 || plan.Completions[0].QuestID != 2635 ||
		plan.Completions[0].PreviousTrigger != int64(packTriggerChannels(0, 0, 1)) ||
		!plan.Completions[0].Completed {
		t.Fatalf("completions=%+v", plan.Completions)
	}
	state := plan.Record.States[2635]
	if state.ProgressValue != 0 || state.Extra["reward_state"] != "pending" ||
		state.Extra["completion_kind"] != "hunt_monster" ||
		state.Extra["completion_monster_code"] != "9602" ||
		state.Extra["completion_key"] != "run-19/op39/9602" ||
		state.Extra["kept"] != "yes" {
		t.Fatalf("state=%+v", state)
	}
	if original := record.States[2635]; original.ProgressValue != int64(packTriggerChannels(0, 0, 1)) || original.Extra["reward_state"] != "" {
		t.Fatalf("input record mutated=%+v", original)
	}
}

func TestPlanHuntMonsterKillIgnoresScopeMismatch(t *testing.T) {
	catalog := &Catalog{byID: map[int64]Definition{
		2635: {ID: 2635, Type: "[hunt monster]", IntData: []int64{311, 2, 9602, 1}},
	}}
	record := dnfrepo.QuestRecord{
		CharacterID: "19",
		States:      map[int64]dnfrepo.QuestState{2635: {Status: "active", ProgressValue: 1}},
	}
	plan, err := catalog.PlanHuntMonsterKill(record, HuntMonsterKillInput{
		DungeonID:     312,
		Difficulty:    2,
		MonsterCode:   9602,
		CompletionKey: "run",
		CompletedAt:   time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.ChangedFields) != 0 || plan.Record.States[2635].ProgressValue != 1 {
		t.Fatalf("unexpected change fields=%v state=%+v", plan.ChangedFields, plan.Record.States[2635])
	}
}

func TestOwnerApplyHuntMonsterKillPersistsAndVerifiesPendingReward(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "19", AccountID: "acc", Job: "2", Level: 90}); err != nil {
		t.Fatal(err)
	}
	if err := repos.Quest.Save(ctx, dnfrepo.QuestRecord{
		CharacterID: "19",
		States:      map[int64]dnfrepo.QuestState{2635: {Status: "active", ProgressValue: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	catalog := &Catalog{byID: map[int64]Definition{
		2635: {ID: 2635, Path: "n_quest/earring.qst", Type: "[hunt monster]", IntData: []int64{-1, -1, 9602, 1}},
	}}
	result, err := owner.ApplyHuntMonsterKill(ctx, catalog, "19", HuntMonsterKillInput{
		DungeonID:     311,
		Difficulty:    2,
		MonsterCode:   9602,
		CompletionKey: "run-19/op39/9602",
		CompletedAt:   time.Date(2026, 7, 23, 22, 25, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Completions) != 1 || result.Completions[0].QuestID != 2635 {
		t.Fatalf("result=%+v", result)
	}
	persisted, ok, err := repos.Quest.Load(ctx, "19")
	if err != nil || !ok {
		t.Fatalf("load persisted ok=%t err=%v", ok, err)
	}
	state := persisted.States[2635]
	if state.ProgressValue != 0 || state.Extra["reward_state"] != "pending" || state.Extra["completion_key"] != "run-19/op39/9602" {
		t.Fatalf("persisted state=%+v", state)
	}
}
