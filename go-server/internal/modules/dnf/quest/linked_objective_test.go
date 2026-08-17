package quest

import (
	"context"
	"reflect"
	"testing"
	"time"

	"longheng.io/server/internal/modules/dnf/pvf"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func linkedObjectiveTestCatalog(t *testing.T) *Catalog {
	t.Helper()
	source := catalogTestSource{
		DefaultList:              "3249 `parent3249.qst`\n3609 `meet.qst`\n3610 `seekmeet.qst`\n3347 `parent3347.qst`\n3425 `hunt.qst`\n3426 `seeking.qst`\n",
		"n_quest/parent3249.qst": "[grade]\n`[epic]`\n[type]\n`[quest clear]`\n[int data]\n3609 3610\n[reward type]\n`[item]`\n[reward int data]\n0 0\n",
		"n_quest/meet.qst":       "[grade]\n`[sub]`\n[attribute]\n`not give exp quest`\n[/attribute]\n[type]\n`[meet npc]`\n[main quest]\n3249\n[int data]\n303\n[reward type]\n`[item]`\n[reward int data]\n0 0\n",
		"n_quest/seekmeet.qst":   "[grade]\n`[sub]`\n[attribute]\n`not give exp quest`\n[/attribute]\n[type]\n`[seek n meet npc]`\n[main quest]\n3249\n[int data]\n3037 1 309\n[reward type]\n`[item]`\n[reward int data]\n0 0\n",
		"n_quest/parent3347.qst": "[grade]\n`[epic]`\n[type]\n`[quest clear]`\n[int data]\n3425 3426\n[reward type]\n`[item]`\n[reward int data]\n0 0\n",
		"n_quest/hunt.qst":       "[grade]\n`[sub]`\n[attribute]\n`not give exp quest`\n[/attribute]\n[type]\n`[hunt enemy]`\n[main quest]\n3347\n[int data]\n3 -1 13099 3 1\n[reward type]\n`[item]`\n[reward int data]\n0 0\n",
		"n_quest/seeking.qst":    "[grade]\n`[sub]`\n[attribute]\n`not give exp quest`\n[/attribute]\n[type]\n`[seeking]`\n[main quest]\n3347\n[int data]\n3037 1\n[reward type]\n`[item]`\n[reward int data]\n0 0\n",
	}
	index, err := pvf.Build(context.Background(), source, pvf.BuildOptions{Lists: []string{DefaultList}})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := Load(context.Background(), index)
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func TestPlanLinkedObjectiveProgressCompletesNoRewardChildrenAndKeepsDungeonOwners(t *testing.T) {
	catalog := linkedObjectiveTestCatalog(t)
	now := time.Date(2026, 8, 5, 15, 0, 0, 0, time.UTC)
	record := dnfrepo.QuestRecord{CharacterID: "19", States: map[int64]dnfrepo.QuestState{
		3347: {Status: "active", ProgressValue: 1},
		3425: {Status: "active", ProgressValue: 1},
		3426: {Status: "active", ProgressValue: 0},
	}}
	town, err := catalog.PlanTownLinkedProgress(record, 3347, now)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(town.CompletedQuestIDs, []int64{3426}) || town.ParentQuestID != 3347 || town.ParentProgress != 1 {
		t.Fatalf("town plan=%+v", town)
	}
	if state := town.Record.States[3426]; state.Status != "completed" || state.Extra["reward_state"] != finishRewardGranted {
		t.Fatalf("3426 state=%+v", state)
	}

	town.Record.States[3425] = dnfrepo.QuestState{Status: "active", ProgressValue: 0}
	finish, err := catalog.PlanLinkedObjectiveFinish(town.Record, 3425, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(finish.CompletedQuestIDs, []int64{3425}) || finish.ParentProgress != 0 || finish.Record.States[3347].ProgressValue != 0 {
		t.Fatalf("finish plan=%+v", finish)
	}
}

func TestOwnerApplyLinkedObjectiveFinishCommitsQuestOnly(t *testing.T) {
	catalog := linkedObjectiveTestCatalog(t)
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "19", AccountID: "dnf:1", Level: 60}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Quest.Save(ctx, dnfrepo.QuestRecord{CharacterID: "19", States: map[int64]dnfrepo.QuestState{
		3347: {Status: "active", ProgressValue: 1},
		3425: {Status: "active", ProgressValue: 0},
		3426: {Status: "completed", ProgressValue: 0},
	}}); err != nil {
		t.Fatal(err)
	}
	owner, err := NewOwner(repositories)
	if err != nil {
		t.Fatal(err)
	}
	result, err := owner.ApplyLinkedObjectiveFinish(ctx, catalog, "19", 3425, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Applied || result.ParentProgress != 0 || !reflect.DeepEqual(result.CompletedQuestIDs, []int64{3425}) {
		t.Fatalf("commit=%+v", result)
	}
	stored, found, err := repositories.Quest.Load(ctx, "19")
	if err != nil || !found || stored.States[3425].Status != "completed" || stored.States[3347].ProgressValue != 0 {
		t.Fatalf("stored=%+v found=%t err=%v", stored, found, err)
	}
}
