package quest

import (
	"context"
	"testing"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestActiveHuntEnemyTargetsSeparatesPassiveObjectTypeFromMonsterType(t *testing.T) {
	catalog := &Catalog{byID: map[int64]Definition{
		3157: {ID: 3157, Path: "n_quest/elvengard_epic_13.qst", Grade: "[sub]", Type: "[hunt enemy]", MainQuestID: 3146, IntData: []int64{3, -1, 13099, 3, 1}},
		4000: {ID: 4000, Path: "n_quest/monster.qst", Type: "[hunt enemy]", IntData: []int64{3, -1, 107000908, 1, 1}},
	}}
	record := dnfrepo.QuestRecord{CharacterID: "19", States: map[int64]dnfrepo.QuestState{
		3157: {Status: "active", ProgressValue: 1},
		4000: {Status: "active", ProgressValue: 1},
	}}

	passive := catalog.ActiveHuntEnemyTargets(record, 3, 0, 3)
	if len(passive) != 1 || passive[0].QuestID != 3157 || passive[0].EnemyCode != 13099 || passive[0].EnemyType != 3 {
		t.Fatalf("passive targets=%+v", passive)
	}
	monsters := catalog.ActiveHuntEnemyTargets(record, 3, 0, 1)
	if len(monsters) != 1 || monsters[0].QuestID != 4000 || monsters[0].EnemyCode != 107000908 || monsters[0].EnemyType != 1 {
		t.Fatalf("monster targets=%+v", monsters)
	}
	record.States[3157] = dnfrepo.QuestState{Status: "completed", ProgressValue: 0}
	if completed := catalog.ActiveHuntEnemyTargets(record, 3, 0, 3); len(completed) != 0 {
		t.Fatalf("completed passive target remained active=%+v", completed)
	}
}

func TestPlanHuntEnemyKillUsesPVFStrideFiveAndMarksPendingReward(t *testing.T) {
	completedAt := time.Date(2026, 7, 16, 22, 10, 0, 0, time.UTC)
	catalog := &Catalog{byID: map[int64]Definition{
		3157: {ID: 3157, Path: "n_quest/hunt.qst", Type: "[hunt enemy]", IntData: []int64{
			-1, -1, 77110, 1, 1,
		}},
	}}
	record := dnfrepo.QuestRecord{
		CharacterID: "19",
		States: map[int64]dnfrepo.QuestState{
			3157: {Status: "active", ProgressValue: 1, Extra: map[string]string{"kept": "yes"}},
		},
	}

	plan, err := catalog.PlanHuntEnemyKill(record, HuntEnemyKillInput{
		DungeonID:     3,
		Difficulty:    0,
		EnemyCode:     77110,
		EnemyType:     1,
		CompletionKey: "run-19/op39/77110",
		CompletedAt:   completedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.ChangedFields) != 1 || plan.ChangedFields[0] != dnfrepo.QuestFieldStates {
		t.Fatalf("changed fields=%v", plan.ChangedFields)
	}
	if len(plan.Completions) != 1 || plan.Completions[0].QuestID != 3157 || plan.Completions[0].PreviousTrigger != 1 {
		t.Fatalf("completions=%+v", plan.Completions)
	}
	state := plan.Record.States[3157]
	if state.ProgressValue != 0 || state.Extra["reward_state"] != "pending" ||
		state.Extra["completion_kind"] != "hunt_enemy" ||
		state.Extra["completion_key"] != "run-19/op39/77110" ||
		state.Extra["kept"] != "yes" {
		t.Fatalf("state=%+v", state)
	}
	if original := record.States[3157]; original.ProgressValue != 1 || original.Extra["reward_state"] != "" {
		t.Fatalf("input record mutated=%+v", original)
	}
}

func TestPlanHuntEnemyKillIgnoresScopeMismatch(t *testing.T) {
	catalog := &Catalog{byID: map[int64]Definition{
		3157: {ID: 3157, Type: "[hunt enemy]", IntData: []int64{3, 1, 77110, 1, 1}},
	}}
	record := dnfrepo.QuestRecord{
		CharacterID: "19",
		States:      map[int64]dnfrepo.QuestState{3157: {Status: "active", ProgressValue: 1}},
	}
	plan, err := catalog.PlanHuntEnemyKill(record, HuntEnemyKillInput{
		DungeonID:     4,
		Difficulty:    1,
		EnemyCode:     77110,
		EnemyType:     1,
		CompletionKey: "run",
		CompletedAt:   time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.ChangedFields) != 0 || plan.Record.States[3157].ProgressValue != 1 {
		t.Fatalf("unexpected change fields=%v state=%+v", plan.ChangedFields, plan.Record.States[3157])
	}
}

func TestPlanHuntEnemyKillAutoCompletesNoRewardSubAndSyncsQuestClearParent(t *testing.T) {
	completedAt := time.Date(2026, 7, 17, 12, 35, 0, 0, time.UTC)
	catalog := &Catalog{byID: map[int64]Definition{
		3157: {ID: 3157, Path: "n_quest/elvengard_epic_13.qst", Grade: "[sub]", Type: "[hunt enemy]", IntData: []int64{3, -1, 13099, 3, 1}},
		3146: {ID: 3146, Path: "n_quest/elvengard_epic_02.qst", Grade: "[epic]", Type: "[quest clear]", IntData: []int64{3157, 3054}},
	}}
	record := dnfrepo.QuestRecord{
		CharacterID: "19",
		States: map[int64]dnfrepo.QuestState{
			3157: {Status: "active", ProgressValue: 1},
			3054: {Status: "completed", ProgressValue: 0},
			3146: {Status: "active", ProgressValue: 1},
		},
	}

	plan, err := catalog.PlanHuntEnemyKill(record, HuntEnemyKillInput{
		DungeonID:     3,
		Difficulty:    0,
		EnemyCode:     13099,
		EnemyType:     3,
		CompletionKey: "run-3146/3157",
		CompletedAt:   completedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Completions) != 1 || plan.Completions[0].QuestID != 3157 || !plan.Completions[0].Completed {
		t.Fatalf("completions=%+v", plan.Completions)
	}
	child := plan.Record.States[3157]
	if child.Status != "completed" || child.ProgressValue != 0 ||
		child.Extra["reward_state"] != "granted" ||
		child.Extra["completion_key"] != "run-3146/3157" ||
		child.Extra["auto_completed"] != "true" {
		t.Fatalf("auto-completed child=%+v", child)
	}
	parent := plan.Record.States[3146]
	if parent.Status != "active" || parent.ProgressValue != 0 {
		t.Fatalf("quest-clear parent=%+v", parent)
	}
	if len(plan.ChangedFields) != 1 || plan.ChangedFields[0] != dnfrepo.QuestFieldStates {
		t.Fatalf("changed fields=%v", plan.ChangedFields)
	}
}

func TestPlanHuntEnemyKillDerivesMissingNoRewardSubFromActiveQuestClearParent(t *testing.T) {
	completedAt := time.Date(2026, 7, 17, 13, 25, 0, 0, time.UTC)
	catalog := &Catalog{byID: map[int64]Definition{
		3157: {ID: 3157, Path: "n_quest/elvengard_epic_13.qst", Grade: "[sub]", Type: "[hunt enemy]", MainQuestID: 3146, IntData: []int64{3, -1, 13099, 3, 1}},
		3146: {ID: 3146, Path: "n_quest/elvengard_epic_02.qst", Grade: "[epic]", Type: "[quest clear]", IntData: []int64{3157, 3054}},
	}}
	record := dnfrepo.QuestRecord{
		CharacterID: "19",
		States: map[int64]dnfrepo.QuestState{
			3054: {Status: "completed", ProgressValue: 0},
			3146: {Status: "active", ProgressValue: 2},
		},
	}

	plan, err := catalog.PlanHuntEnemyKill(record, HuntEnemyKillInput{
		DungeonID:     3,
		Difficulty:    0,
		EnemyCode:     13099,
		EnemyType:     3,
		CompletionKey: "run-3146/stale-parent-only/3157",
		CompletedAt:   completedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Completions) != 1 || plan.Completions[0].QuestID != 3157 ||
		plan.Completions[0].PreviousTrigger != 1 || !plan.Completions[0].Completed {
		t.Fatalf("completions=%+v", plan.Completions)
	}
	child := plan.Record.States[3157]
	if child.Status != "completed" || child.ProgressValue != 0 ||
		child.Extra["reward_state"] != "granted" ||
		child.Extra["main_quest_id"] != "3146" ||
		child.Extra["auto_activated_by_main_quest"] != "true" ||
		child.Extra["completion_key"] != "run-3146/stale-parent-only/3157" ||
		child.Extra["auto_completed"] != "true" {
		t.Fatalf("derived child=%+v", child)
	}
	if parent := plan.Record.States[3146]; parent.Status != "active" || parent.ProgressValue != 0 {
		t.Fatalf("quest-clear parent=%+v", parent)
	}
	if len(plan.ChangedFields) != 1 || plan.ChangedFields[0] != dnfrepo.QuestFieldStates {
		t.Fatalf("changed fields=%v", plan.ChangedFields)
	}
}

func TestPlanHuntEnemyKillReconcilesAllMissingNoRewardSubTasksBeforeCompletion(t *testing.T) {
	completedAt := time.Date(2026, 7, 17, 14, 10, 0, 0, time.UTC)
	catalog := &Catalog{byID: map[int64]Definition{
		3157: {ID: 3157, Path: "n_quest/elvengard_epic_13.qst", Grade: "[sub]", Type: "[hunt enemy]", MainQuestID: 3146, IntData: []int64{3, -1, 13099, 3, 1}},
		3054: {ID: 3054, Path: "n_quest/elvengard_epic_13_1.qst", Grade: "[sub]", Type: "[clear map]", MainQuestID: 3146, IntData: []int64{76136}},
		3146: {ID: 3146, Path: "n_quest/elvengard_epic_02.qst", Grade: "[epic]", Type: "[quest clear]", IntData: []int64{3157, 3054}},
	}}
	record := dnfrepo.QuestRecord{
		CharacterID: "19",
		States: map[int64]dnfrepo.QuestState{
			3146: {Status: "active", ProgressValue: 2},
		},
	}

	plan, err := catalog.PlanHuntEnemyKill(record, HuntEnemyKillInput{
		DungeonID:     3,
		Difficulty:    0,
		EnemyCode:     13099,
		EnemyType:     3,
		CompletionKey: "run-3146/only-parent/3157",
		CompletedAt:   completedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Completions) != 1 || plan.Completions[0].QuestID != 3157 || !plan.Completions[0].Completed {
		t.Fatalf("completions=%+v", plan.Completions)
	}
	huntChild := plan.Record.States[3157]
	if huntChild.Status != "completed" || huntChild.ProgressValue != 0 ||
		huntChild.Extra["auto_activated_by_main_quest"] != "true" ||
		huntChild.Extra["completion_key"] != "run-3146/only-parent/3157" {
		t.Fatalf("hunt child=%+v", huntChild)
	}
	clearChild := plan.Record.States[3054]
	if clearChild.Status != "active" || clearChild.ProgressValue != 1 ||
		clearChild.Extra["auto_activated_by_main_quest"] != "true" ||
		clearChild.Extra["auto_activation_reason"] != "active_quest_clear_parent_reconcile" {
		t.Fatalf("clear child=%+v", clearChild)
	}
	if parent := plan.Record.States[3146]; parent.Status != "active" || parent.ProgressValue != 1 {
		t.Fatalf("quest-clear parent=%+v", parent)
	}
	if len(plan.ChangedFields) != 1 || plan.ChangedFields[0] != dnfrepo.QuestFieldStates {
		t.Fatalf("changed fields=%v", plan.ChangedFields)
	}
}

func TestOwnerApplyHuntEnemyKillPersistsAndVerifiesPendingReward(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "19", AccountID: "acc", Job: "2", Level: 1}); err != nil {
		t.Fatal(err)
	}
	if err := repos.Quest.Save(ctx, dnfrepo.QuestRecord{
		CharacterID: "19",
		States:      map[int64]dnfrepo.QuestState{3157: {Status: "active", ProgressValue: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	catalog := &Catalog{byID: map[int64]Definition{
		3157: {ID: 3157, Path: "n_quest/hunt.qst", Type: "[hunt enemy]", IntData: []int64{-1, -1, 77110, 1, 1}},
	}}
	result, err := owner.ApplyHuntEnemyKill(ctx, catalog, "19", HuntEnemyKillInput{
		DungeonID:     3,
		Difficulty:    0,
		EnemyCode:     77110,
		EnemyType:     1,
		CompletionKey: "run-19/op39/77110",
		CompletedAt:   time.Date(2026, 7, 16, 22, 10, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Completions) != 1 || result.Completions[0].QuestID != 3157 {
		t.Fatalf("result=%+v", result)
	}
	persisted, ok, err := repos.Quest.Load(ctx, "19")
	if err != nil || !ok {
		t.Fatalf("load persisted ok=%t err=%v", ok, err)
	}
	state := persisted.States[3157]
	if state.ProgressValue != 0 || state.Extra["reward_state"] != "pending" || state.Extra["completion_key"] != "run-19/op39/77110" {
		t.Fatalf("persisted state=%+v", state)
	}
}
