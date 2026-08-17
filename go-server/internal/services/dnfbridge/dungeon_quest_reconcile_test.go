package dnfbridge

import (
	"context"
	"testing"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	dnfquest "longheng.io/server/internal/modules/dnf/quest"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestReconcileActiveQuestClearLinkedSubQuestsForDungeonRepairsExistingParent(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "dnf:1",
		Job:         "2",
		Level:       3,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Quest.Save(ctx, dnfrepo.QuestRecord{
		CharacterID: "19",
		States: map[int64]dnfrepo.QuestState{
			3146: {Status: "active", ProgressValue: 2},
		},
	}); err != nil {
		t.Fatal(err)
	}
	index, err := dnfpvf.Build(ctx, questListTestSource{
		dnfquest.DefaultList: "3146 `parent.qst`\n3157 `hunt.qst`\n3054 `clear.qst`\n",
		"n_quest/parent.qst": "[grade]\n`[epic]`\n[level]\n3 99\n[job]\n`[gunner]`\n[exposed by npc]\n1\n[type]\n`[quest clear]`\n[int data]\n3157 3054\n",
		"n_quest/hunt.qst":   "[grade]\n`[sub]`\n[level]\n3 99\n[job]\n`[gunner]`\n[exposed by npc]\n1\n[type]\n`[hunt enemy]`\n[main quest]\n3146\n[int data]\n3 -1 13099 3 1\n",
		"n_quest/clear.qst":  "[grade]\n`[sub]`\n[level]\n1 99\n[job]\n`[gunner]`\n[exposed by npc]\n1\n[type]\n`[clear map]`\n[main quest]\n3146\n[int data]\n76136\n",
	}, dnfpvf.BuildOptions{Lists: []string{dnfquest.DefaultList}})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := dnfquest.Load(ctx, index)
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
		questCatalog:       catalog,
	}
	session := &gameSession{connID: "quest-reconcile", selectedCharacterID: 19}
	reconciled, err := service.reconcileActiveQuestClearLinkedSubQuestsForDungeon(ctx, session, "test_select_dungeon")
	if err != nil {
		t.Fatal(err)
	}
	if !reconciled {
		t.Fatal("reconcile returned false, want linked subtasks persisted")
	}
	persisted, found, err := repositories.Quest.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load persisted found=%t err=%v", found, err)
	}
	for _, questID := range []int64{3157, 3054} {
		state := persisted.States[questID]
		if state.Status != "active" || state.ProgressValue != 1 ||
			state.Extra["main_quest_id"] != "3146" ||
			state.Extra["auto_activated_by_main_quest"] != "true" {
			t.Fatalf("linked quest %d state=%+v", questID, state)
		}
	}
}
