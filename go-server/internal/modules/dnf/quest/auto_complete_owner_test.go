package quest

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestOwnerAutoCompleteMainClosesOnlySelectedActiveEpicWithoutRewards(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	character := dnfrepo.CharacterRecord{
		CharacterID: "17",
		AccountID:   "dnf:1",
		Job:         "2",
		Level:       90,
		Stats:       map[string]int64{"grow_type": 0, "sp": 777, "exp": 12345},
	}
	if err := repositories.Character.Save(ctx, character); err != nil {
		t.Fatal(err)
	}
	inventory := dnfrepo.InventoryRecord{
		CharacterID: "17",
		Slots: map[string]dnfrepo.ItemStack{
			"0:10": {ItemID: 9001, Count: 3},
		},
	}
	if err := repositories.Inventory.Save(ctx, inventory); err != nil {
		t.Fatal(err)
	}
	skill := dnfrepo.SkillRecord{CharacterID: "17", Skills: map[int64]dnfrepo.SkillState{1001: {Level: 5, Enabled: true}}}
	if err := repositories.Skill.Save(ctx, skill); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Quest.Save(ctx, dnfrepo.QuestRecord{
		CharacterID: "17",
		States: map[int64]dnfrepo.QuestState{
			100: {Status: "active", ProgressValue: 1},
			102: {Status: "active", ProgressValue: 1},
			103: {Status: "active", ProgressValue: 1},
			106: {Status: "active", ProgressValue: 1},
		},
		Progress: map[int64]dnfrepo.QuestState{
			150: {Status: "active", ProgressValue: 3},
		},
	}); err != nil {
		t.Fatal(err)
	}

	catalog := autoCompleteMainTestCatalog(t)
	owner, err := NewOwner(repositories)
	if err != nil {
		t.Fatal(err)
	}
	completedAt := time.Date(2026, 7, 22, 7, 30, 0, 0, time.UTC)
	result, err := owner.ApplyAutoCompleteMain(ctx, catalog, AutoCompleteMainInput{
		CharacterID:   "17",
		Eligibility:   CharacterEligibility{Level: 90, Job: 2, GrowType: 0},
		CutoffLevel:   89,
		TargetQuestID: 100,
		CompletedAt:   completedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.ChangedQuestIDs, []int64{100}) ||
		len(result.ChangedLinkedSubIDs) != 0 ||
		result.ActiveCompletedCount != 1 || result.HighestCompletedQuest != 100 || result.Idempotent {
		t.Fatalf("auto-complete result=%+v", result)
	}
	stored := result.PostCommitQuest
	for _, questID := range []int64{100} {
		state := stored.States[questID]
		if state.Status != "completed" || state.ProgressValue != 0 ||
			state.Extra["reward_state"] != finishRewardGranted ||
			state.Extra["auto_complete_reason"] != "epic_below_character_level_no_reward" ||
			state.Extra["auto_complete_level_cutoff"] != "89" {
			t.Fatalf("completed epic quest %d state=%+v", questID, state)
		}
	}
	if _, exists := stored.States[101]; exists {
		t.Fatalf("absent successor quest 101 was manufactured: %+v", stored.States[101])
	}
	if acceptable := catalog.Acceptable(CharacterEligibility{Level: 90, Job: 2, GrowType: 0}, stored); !containsQuestID(acceptable.IDs, 101) {
		t.Fatalf("successor main quest 101 missing after active-only completion: %v", acceptable.IDs)
	}
	if state := stored.Progress[150]; state.Status != "active" || state.ProgressValue != 3 {
		t.Fatalf("linked sub state=%+v", state)
	}
	for _, questID := range []int64{102, 103, 106} {
		if state := stored.States[questID]; state.Status != "active" || state.ProgressValue != 1 {
			t.Fatalf("non-target quest %d changed: %+v", questID, state)
		}
	}
	for _, questID := range []int64{104, 105} {
		if _, exists := stored.States[questID]; exists {
			t.Fatalf("inapplicable/answer quest %d was manufactured", questID)
		}
	}

	postCharacter, found, err := repositories.Character.Load(ctx, "17")
	if err != nil || !found || !reflect.DeepEqual(postCharacter, character) {
		t.Fatalf("character changed found=%t err=%v got=%+v want=%+v", found, err, postCharacter, character)
	}
	postInventory, found, err := repositories.Inventory.Load(ctx, "17")
	if err != nil || !found || !reflect.DeepEqual(postInventory, inventory) {
		t.Fatalf("inventory changed found=%t err=%v got=%+v want=%+v", found, err, postInventory, inventory)
	}
	postSkill, found, err := repositories.Skill.Load(ctx, "17")
	if err != nil || !found || !reflect.DeepEqual(postSkill, skill) {
		t.Fatalf("skill changed found=%t err=%v got=%+v want=%+v", found, err, postSkill, skill)
	}

	replayed, err := owner.ApplyAutoCompleteMain(ctx, catalog, AutoCompleteMainInput{
		CharacterID:   "17",
		Eligibility:   CharacterEligibility{Level: 90, Job: 2, GrowType: 0},
		CutoffLevel:   89,
		TargetQuestID: 100,
		CompletedAt:   completedAt.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !replayed.Idempotent || len(replayed.ChangedQuestIDs) != 0 || len(replayed.ChangedLinkedSubIDs) != 0 {
		t.Fatalf("idempotent replay=%+v", replayed)
	}
}

func TestPlanAutoCompleteMainZeroTargetCompletesEveryEligibleActiveEpic(t *testing.T) {
	catalog := autoCompleteMainTestCatalog(t)
	record := dnfrepo.QuestRecord{
		CharacterID: "17",
		States: map[int64]dnfrepo.QuestState{
			100: {Status: "active", ProgressValue: 1},
			106: {Status: "active", ProgressValue: 1},
		},
	}
	plan, err := catalog.PlanAutoCompleteMainQuests(
		record,
		CharacterEligibility{Level: 90, Job: 2},
		89,
		0,
		time.Date(2026, 7, 22, 8, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(plan.EligibleQuestIDs, []int64{100, 106}) ||
		!reflect.DeepEqual(plan.ChangedQuestIDs, []int64{100, 106}) ||
		plan.ActiveCompletedCount != 2 || plan.HighestCompletedQuest != 106 {
		t.Fatalf("zero-selector plan=%+v", plan)
	}
	for _, questID := range []int64{100, 106} {
		if state := plan.Record.States[questID]; state.Status != "completed" || state.Extra["auto_complete_reason"] != "epic_below_character_level_no_reward" {
			t.Fatalf("bulk-completed quest %d state=%+v", questID, state)
		}
	}
	if record.States[100].Status != "active" || record.States[106].Status != "active" {
		t.Fatalf("planning mutated source record=%+v", record)
	}
}

func TestPlanAutoCompleteMainZeroTargetRejectsWhenNoEligibleActiveEpicExists(t *testing.T) {
	catalog := autoCompleteMainTestCatalog(t)
	_, err := catalog.PlanAutoCompleteMainQuests(
		dnfrepo.QuestRecord{CharacterID: "17", States: map[int64]dnfrepo.QuestState{
			102: {Status: "active", ProgressValue: 1},
			103: {Status: "active", ProgressValue: 1},
		}},
		CharacterEligibility{Level: 90, Job: 2},
		89,
		0,
		time.Date(2026, 7, 22, 8, 0, 0, 0, time.UTC),
	)
	if !errors.Is(err, ErrAutoCompleteTargetInvalid) {
		t.Fatalf("zero target without eligible main quest error=%v", err)
	}
}

func autoCompleteMainTestCatalog(t *testing.T) *Catalog {
	t.Helper()
	source := catalogTestSource{
		DefaultList:                     "100 `epic_start.qst`\n101 `epic_next.qst`\n102 `epic_level_90.qst`\n103 `side.qst`\n104 `other_job.qst`\n105 `answer.qst`\n106 `other_active_epic.qst`\n150 `linked_sub.qst`\n",
		"n_quest/epic_start.qst":        questCatalogTestDefinition("[epic]", 20, 99, "[gunner]", "[type]\n`[clear map]`\n"),
		"n_quest/epic_next.qst":         questCatalogTestDefinition("[epic]", 30, 99, "[gunner]", "[type]\n`[meet npc]`\n[pre required quest]\n100\n"),
		"n_quest/epic_level_90.qst":     questCatalogTestDefinition("[epic]", 90, 99, "[gunner]", "[type]\n`[meet npc]`\n[pre required quest]\n101\n"),
		"n_quest/side.qst":              questCatalogTestDefinition("[side]", 10, 99, "[gunner]", "[type]\n`[meet npc]`\n"),
		"n_quest/other_job.qst":         questCatalogTestDefinition("[epic]", 10, 99, "[fighter]", "[type]\n`[meet npc]`\n"),
		"n_quest/answer.qst":            questCatalogTestDefinition("[epic]", 40, 99, "[gunner]", "[type]\n`[meet npc]`\n[pre required quest answer]\n1\n"),
		"n_quest/other_active_epic.qst": questCatalogTestDefinition("[epic]", 35, 99, "[gunner]", "[type]\n`[meet npc]`\n"),
		"n_quest/linked_sub.qst":        questCatalogTestDefinition("[sub]", 30, 99, "[gunner]", "[type]\n`[hunt enemy]`\n[main quest]\n100\n"),
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
