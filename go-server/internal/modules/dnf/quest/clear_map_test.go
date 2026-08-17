package quest

import (
	"context"
	"reflect"
	"testing"
	"time"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func TestPlanClearMapCompletionUsesPVFTargetAndPreservesPendingReward(t *testing.T) {
	catalog := clearMapTestCatalog(t)
	completedAt := time.Date(2026, 7, 15, 20, 30, 0, 0, time.UTC)
	record := dnfrepo.QuestRecord{
		CharacterID: "19",
		States: map[int64]dnfrepo.QuestState{
			3145: {Status: "active", ProgressValue: 1, Extra: map[string]string{"kept": "yes"}},
			3146: {Status: "active", ProgressValue: 7},
		},
	}
	plan, err := catalog.PlanClearMapCompletion(record, ClearMapCompletionInput{
		DungeonID: 3, MapID: 76126, CompletionKey: "run-17/op117/430", CompletedAt: completedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Completions) != 1 || plan.Completions[0].QuestID != 3145 || plan.Completions[0].PreviousTrigger != 1 || plan.Completions[0].Idempotent {
		t.Fatalf("completions = %+v", plan.Completions)
	}
	if !reflect.DeepEqual(plan.ChangedFields, []dnfrepo.QuestField{dnfrepo.QuestFieldStates}) {
		t.Fatalf("changed fields = %v", plan.ChangedFields)
	}
	state := plan.Record.States[3145]
	if state.Status != "active" || state.ProgressValue != 0 || state.Extra["reward_state"] != "pending" || state.Extra["completion_key"] != "run-17/op117/430" || state.Extra["kept"] != "yes" {
		t.Fatalf("completed state = %+v", state)
	}
	if other := plan.Record.States[3146]; other.ProgressValue != 7 {
		t.Fatalf("unrelated quest changed = %+v", other)
	}
	if original := record.States[3145]; original.ProgressValue != 1 || original.Extra["reward_state"] != "" {
		t.Fatalf("input record mutated = %+v", original)
	}
}

func TestPlanClearMapCompletionReplayIsIdempotent(t *testing.T) {
	catalog := clearMapTestCatalog(t)
	completedAt := time.Date(2026, 7, 15, 20, 30, 0, 0, time.UTC)
	input := ClearMapCompletionInput{MapID: 76126, CompletionKey: "run-17/op117/430", CompletedAt: completedAt}
	first, err := catalog.PlanClearMapCompletion(dnfrepo.QuestRecord{
		CharacterID: "19",
		States:      map[int64]dnfrepo.QuestState{3145: {Status: "active", ProgressValue: 1}},
	}, input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := catalog.PlanClearMapCompletion(first.Record, input)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Completions) != 1 || !second.Completions[0].Idempotent || len(second.ChangedFields) != 0 {
		t.Fatalf("replay plan = %+v", second)
	}
	if !reflect.DeepEqual(first.Record, second.Record) {
		t.Fatalf("replay changed record\nfirst=%+v\nsecond=%+v", first.Record, second.Record)
	}
}

func TestPlanClearMapCompletionMatchesDungeonIDAndProgressField(t *testing.T) {
	catalog := clearMapTestCatalog(t)
	plan, err := catalog.PlanClearMapCompletion(dnfrepo.QuestRecord{
		CharacterID: "19",
		Progress:    map[int64]dnfrepo.QuestState{3147: {Status: "accepted", ProgressValue: 2}},
	}, ClearMapCompletionInput{DungeonID: 3, CompletionKey: "run-dungeon-target", CompletedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Completions) != 1 || plan.Completions[0].Field != dnfrepo.QuestFieldProgress || plan.Record.Progress[3147].ProgressValue != 0 || !reflect.DeepEqual(plan.ChangedFields, []dnfrepo.QuestField{dnfrepo.QuestFieldProgress}) {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestPlanClearMapCompletionAutoCompletesNoRewardSubAndSyncsQuestClearParent(t *testing.T) {
	source := catalogTestSource{
		DefaultList: "3054 `clear_sub.qst`\n3146 `parent.qst`\n",
		"n_quest/clear_sub.qst": questCatalogTestDefinition("[sub]", 1, 99, "[all]",
			"[type]\n`[clear map]`\n[int data]\n76136\n"),
		"n_quest/parent.qst": questCatalogTestDefinition("[epic]", 1, 99, "[all]",
			"[type]\n`[quest clear]`\n[int data]\n3157 3054\n"),
	}
	index, err := dnfpvf.Build(context.Background(), source, dnfpvf.BuildOptions{Lists: []string{DefaultList}})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := Load(context.Background(), index)
	if err != nil {
		t.Fatal(err)
	}
	completedAt := time.Date(2026, 7, 17, 12, 30, 0, 0, time.UTC)
	plan, err := catalog.PlanClearMapCompletion(dnfrepo.QuestRecord{
		CharacterID: "19",
		States: map[int64]dnfrepo.QuestState{
			3054: {Status: "active", ProgressValue: 1},
			3157: {Status: "completed", ProgressValue: 0},
			3146: {Status: "active", ProgressValue: 1},
		},
	}, ClearMapCompletionInput{MapID: 76136, CompletionKey: "run-3146/3054", CompletedAt: completedAt})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Completions) != 1 || plan.Completions[0].QuestID != 3054 {
		t.Fatalf("completions=%+v", plan.Completions)
	}
	child := plan.Record.States[3054]
	if child.Status != "completed" || child.ProgressValue != 0 ||
		child.Extra["reward_state"] != "granted" ||
		child.Extra["completion_key"] != "run-3146/3054" ||
		child.Extra["auto_completed"] != "true" {
		t.Fatalf("auto-completed child=%+v", child)
	}
	parent := plan.Record.States[3146]
	if parent.Status != "active" || parent.ProgressValue != 0 {
		t.Fatalf("quest-clear parent=%+v", parent)
	}
	if !reflect.DeepEqual(plan.ChangedFields, []dnfrepo.QuestField{dnfrepo.QuestFieldStates}) {
		t.Fatalf("changed fields=%v", plan.ChangedFields)
	}
}

func TestPlanClearMapCompletionDerivesMissingNoRewardSubFromActiveQuestClearParent(t *testing.T) {
	source := catalogTestSource{
		DefaultList: "3054 `clear_sub.qst`\n3146 `parent.qst`\n",
		"n_quest/clear_sub.qst": questCatalogTestDefinition("[sub]", 1, 99, "[all]",
			"[type]\n`[clear map]`\n[main quest]\n3146\n[int data]\n76136\n"),
		"n_quest/parent.qst": questCatalogTestDefinition("[epic]", 1, 99, "[all]",
			"[type]\n`[quest clear]`\n[int data]\n3157 3054\n"),
	}
	index, err := dnfpvf.Build(context.Background(), source, dnfpvf.BuildOptions{Lists: []string{DefaultList}})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := Load(context.Background(), index)
	if err != nil {
		t.Fatal(err)
	}
	completedAt := time.Date(2026, 7, 17, 13, 20, 0, 0, time.UTC)
	plan, err := catalog.PlanClearMapCompletion(dnfrepo.QuestRecord{
		CharacterID: "19",
		States: map[int64]dnfrepo.QuestState{
			3157: {Status: "completed", ProgressValue: 0},
			3146: {Status: "active", ProgressValue: 2},
		},
	}, ClearMapCompletionInput{MapID: 76136, CompletionKey: "run-3146/stale-parent-only/3054", CompletedAt: completedAt})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Completions) != 1 || plan.Completions[0].QuestID != 3054 || plan.Completions[0].PreviousTrigger != 1 {
		t.Fatalf("completions=%+v", plan.Completions)
	}
	child := plan.Record.States[3054]
	if child.Status != "completed" || child.ProgressValue != 0 ||
		child.Extra["reward_state"] != "granted" ||
		child.Extra["main_quest_id"] != "3146" ||
		child.Extra["auto_activated_by_main_quest"] != "true" ||
		child.Extra["completion_key"] != "run-3146/stale-parent-only/3054" ||
		child.Extra["auto_completed"] != "true" {
		t.Fatalf("derived child=%+v", child)
	}
	if parent := plan.Record.States[3146]; parent.Status != "active" || parent.ProgressValue != 0 {
		t.Fatalf("quest-clear parent=%+v", parent)
	}
	if !reflect.DeepEqual(plan.ChangedFields, []dnfrepo.QuestField{dnfrepo.QuestFieldStates}) {
		t.Fatalf("changed fields=%v", plan.ChangedFields)
	}
}

func TestPlanClearMapCompletionReconcilesAllMissingNoRewardSubTasksBeforeCompletion(t *testing.T) {
	source := catalogTestSource{
		DefaultList: "3054 `clear_sub.qst`\n3157 `hunt_sub.qst`\n3146 `parent.qst`\n",
		"n_quest/clear_sub.qst": questCatalogTestDefinition("[sub]", 1, 99, "[all]",
			"[type]\n`[clear map]`\n[main quest]\n3146\n[int data]\n76136\n"),
		"n_quest/hunt_sub.qst": questCatalogTestDefinition("[sub]", 1, 99, "[all]",
			"[type]\n`[hunt enemy]`\n[main quest]\n3146\n[int data]\n3 -1 13099 3 1\n"),
		"n_quest/parent.qst": questCatalogTestDefinition("[epic]", 1, 99, "[all]",
			"[type]\n`[quest clear]`\n[int data]\n3157 3054\n"),
	}
	index, err := dnfpvf.Build(context.Background(), source, dnfpvf.BuildOptions{Lists: []string{DefaultList}})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := Load(context.Background(), index)
	if err != nil {
		t.Fatal(err)
	}
	completedAt := time.Date(2026, 7, 17, 14, 5, 0, 0, time.UTC)
	plan, err := catalog.PlanClearMapCompletion(dnfrepo.QuestRecord{
		CharacterID: "19",
		States: map[int64]dnfrepo.QuestState{
			3146: {Status: "active", ProgressValue: 2},
		},
	}, ClearMapCompletionInput{MapID: 76136, CompletionKey: "run-3146/only-parent/3054", CompletedAt: completedAt})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Completions) != 1 || plan.Completions[0].QuestID != 3054 {
		t.Fatalf("completions=%+v", plan.Completions)
	}
	clearChild := plan.Record.States[3054]
	if clearChild.Status != "completed" || clearChild.ProgressValue != 0 ||
		clearChild.Extra["auto_activated_by_main_quest"] != "true" ||
		clearChild.Extra["completion_key"] != "run-3146/only-parent/3054" {
		t.Fatalf("clear child=%+v", clearChild)
	}
	huntChild := plan.Record.States[3157]
	if huntChild.Status != "active" || huntChild.ProgressValue != 1 ||
		huntChild.Extra["auto_activated_by_main_quest"] != "true" ||
		huntChild.Extra["auto_activation_reason"] != "active_quest_clear_parent_reconcile" {
		t.Fatalf("hunt child=%+v", huntChild)
	}
	if parent := plan.Record.States[3146]; parent.Status != "active" || parent.ProgressValue != 1 {
		t.Fatalf("quest-clear parent=%+v", parent)
	}
	if !reflect.DeepEqual(plan.ChangedFields, []dnfrepo.QuestField{dnfrepo.QuestFieldStates}) {
		t.Fatalf("changed fields=%v", plan.ChangedFields)
	}
}

func TestPlanClearMapCompletionRejectsNegativeTriggerAsCompletion(t *testing.T) {
	catalog := clearMapTestCatalog(t)
	record := dnfrepo.QuestRecord{
		CharacterID: "19",
		States:      map[int64]dnfrepo.QuestState{3145: {Status: "active", ProgressValue: -1}},
	}
	plan, err := catalog.PlanClearMapCompletion(record, ClearMapCompletionInput{
		MapID: 76126, CompletionKey: "corrupt-trigger", CompletedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Completions) != 0 || len(plan.ChangedFields) != 0 || plan.Record.States[3145].ProgressValue != -1 {
		t.Fatalf("negative trigger plan = %+v", plan)
	}
}

func clearMapTestCatalog(t *testing.T) *Catalog {
	t.Helper()
	source := catalogTestSource{
		DefaultList:             "3145 `clear_map.qst`\n3146 `other.qst`\n3147 `dungeon.qst`\n",
		"n_quest/clear_map.qst": questCatalogTestDefinition("[epic]", 1, 99, "[gunner]", "[type]\n`[clear map]`\n[int data]\n76126\n"),
		"n_quest/other.qst":     questCatalogTestDefinition("[epic]", 1, 99, "[gunner]", "[type]\n`[clear map]`\n[int data]\n99999\n"),
		"n_quest/dungeon.qst":   questCatalogTestDefinition("[epic]", 1, 99, "[gunner]", "[type]\n`[clear map]`\n[int data]\n3\n"),
	}
	index, err := dnfpvf.Build(context.Background(), source, dnfpvf.BuildOptions{Lists: []string{DefaultList}})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := Load(context.Background(), index)
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}
