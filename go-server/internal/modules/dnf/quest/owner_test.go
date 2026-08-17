package quest

import (
	"context"
	"errors"
	"testing"
	"time"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestOwnerPlanReadsQuestState(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "46",
		AccountID:   "acc-quest",
	}); err != nil {
		t.Fatalf("save character: %v", err)
	}
	if err := repos.Quest.Save(ctx, dnfrepo.QuestRecord{
		CharacterID: "46",
		States: map[int64]dnfrepo.QuestState{
			0x1234: {Status: "active", ProgressValue: 2},
		},
	}); err != nil {
		t.Fatalf("save quest: %v", err)
	}

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner() error = %v", err)
	}
	got, err := owner.Plan(ctx, NewFinishCommand(alignedRequest("acc-quest", 46), FinishQuestRequest{
		QuestID:           0x1234,
		RewardSelectIndex: 1,
		HasRewardSelect:   true,
		Multiplier:        2,
	}))
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if got.AccountID != "acc-quest" || got.CharacterID != "46" || !got.Known || got.Status != "active" || got.ProgressValue != 2 || got.RewardSelectIndex != 1 || got.Multiplier != 2 {
		t.Fatalf("result = %+v", got)
	}
}

func TestOwnerApplySetTriggerType1RecomputesSeekingItems(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "48",
		AccountID:   "acc-quest",
	}); err != nil {
		t.Fatalf("save character: %v", err)
	}
	if err := repos.Quest.Save(ctx, dnfrepo.QuestRecord{
		CharacterID: "48",
		States: map[int64]dnfrepo.QuestState{
			0x2222: {
				Status:        "active",
				ProgressValue: 1,
				Extra: map[string]string{
					"seeking_item_ids": "9001",
				},
			},
		},
	}); err != nil {
		t.Fatalf("save quest: %v", err)
	}
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "48",
		Slots: map[string]dnfrepo.ItemStack{
			"0:9": {ItemID: 9001, Count: 1},
		},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner() error = %v", err)
	}
	got, err := owner.ApplySetTrigger(ctx, NewTriggerCommand(alignedRequest("acc-quest", 48), SetTriggerRequest{
		QuestID:     0x2222,
		TriggerType: 1,
		IsIncrement: true,
	}))
	if err != nil {
		t.Fatalf("ApplySetTrigger() error = %v", err)
	}
	if got.ProgressValue != 0 {
		t.Fatalf("progress = %d, want 0 after held seeking item", got.ProgressValue)
	}
	if !got.StateChanged {
		t.Fatalf("first trigger result = %+v, want durable state change", got)
	}
	loaded, ok, err := repos.Quest.Load(ctx, "48")
	if err != nil || !ok {
		t.Fatalf("load quest ok=%v err=%v", ok, err)
	}
	if got := loaded.States[0x2222].ProgressValue; got != 0 {
		t.Fatalf("stored progress = %d, want 0", got)
	}
	replayed, err := owner.ApplySetTrigger(ctx, NewTriggerCommand(alignedRequest("acc-quest", 48), SetTriggerRequest{
		QuestID:     0x2222,
		TriggerType: 1,
		IsIncrement: true,
	}))
	if err != nil {
		t.Fatalf("ApplySetTrigger replay error = %v", err)
	}
	if replayed.StateChanged {
		t.Fatalf("replay result = %+v, want no durable state change", replayed)
	}
}

func TestOwnerApplySetTriggerUpdatesOnlyRequestedPackedChannel(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "49",
		AccountID:   "acc-quest",
	}); err != nil {
		t.Fatal(err)
	}
	if err := repos.Quest.Save(ctx, dnfrepo.QuestRecord{
		CharacterID: "49",
		States: map[int64]dnfrepo.QuestState{
			3193: {Status: "active", ProgressValue: int64(packTriggerChannels(1, 1))},
			3203: {Status: "active", ProgressValue: int64(packTriggerChannels(4, 6, 7))},
		},
	}); err != nil {
		t.Fatal(err)
	}
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}

	apply := func(questID uint16, triggerType byte, increment bool, want int64) {
		t.Helper()
		result, err := owner.ApplySetTrigger(ctx, NewTriggerCommand(alignedRequest("acc-quest", 49), SetTriggerRequest{
			QuestID: questID, TriggerType: triggerType, IsIncrement: increment,
		}))
		if err != nil {
			t.Fatalf("ApplySetTrigger quest=%d type=%#x: %v", questID, triggerType, err)
		}
		if !result.StateChanged || result.ProgressValue != want {
			t.Fatalf("ApplySetTrigger quest=%d type=%#x result=%+v want progress=%d", questID, triggerType, result, want)
		}
	}

	apply(3193, 0x10, false, int64(packTriggerChannels(0, 1)))
	apply(3193, 0x20, false, 0)
	apply(3203, 0x40, false, int64(packTriggerChannels(4, 6, 6)))
	apply(3203, 0x20, true, int64(packTriggerChannels(4, 7, 6)))
}

func TestPackedTriggerChannelArithmeticSaturatesAndPreservesReservedBits(t *testing.T) {
	const reserved = uint32(0xf8000000)
	start := reserved | packTriggerChannels(511, 6, 7)
	if got := incrementPackedTriggerChannel(start, 0); got != start {
		t.Fatalf("saturated packed trigger=%#08x want unchanged %#08x", got, start)
	}
	if got := decrementPackedTriggerChannel(reserved|packTriggerChannels(0, 6, 7), 0); got != reserved|packTriggerChannels(0, 6, 7) {
		t.Fatalf("zero packed trigger=%#08x want unchanged", got)
	}
	want := reserved | packTriggerChannels(1, 7, 7)
	if got := incrementPackedTriggerChannel(reserved|packTriggerChannels(1, 6, 7), 1); got != want {
		t.Fatalf("packed trigger increment=%#08x want=%#08x", got, want)
	}
}

func TestOwnerPlanRejectsMissingCharacter(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner() error = %v", err)
	}
	_, err = owner.Plan(ctx, NewQuestIDCommand(alignedRequest("acc-quest", 47), "accept_quest", QuestIDRequest{QuestID: 1}))
	if !errors.Is(err, ErrCharacterNotFound) {
		t.Fatalf("Plan() error = %v, want ErrCharacterNotFound", err)
	}
}

func TestOwnerApplyAcceptPersistsThenReturnsIdempotentResult(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "17",
		AccountID:   "acc-quest",
		Job:         "2",
		Level:       1,
	}); err != nil {
		t.Fatalf("save character: %v", err)
	}
	catalog := testAcceptCatalog(t, "[type]\n`[clear map]`\n[int data]\n76126\n")
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	cmd := NewQuestIDCommand(alignedRequest("acc-quest", 17), "accept_quest", QuestIDRequest{QuestID: 3145})
	first, err := owner.ApplyAccept(ctx, catalog, CharacterEligibility{Level: 1, Job: 2}, cmd)
	if err != nil {
		t.Fatalf("ApplyAccept first error = %v", err)
	}
	if first.QuestID != 3145 || first.InitTrigger != 1 || first.Idempotent {
		t.Fatalf("first result = %+v", first)
	}
	stored, ok, err := repos.Quest.Load(ctx, "17")
	if err != nil || !ok {
		t.Fatalf("load stored quest ok=%t err=%v", ok, err)
	}
	state := stored.States[3145]
	if state.Status != "active" || state.ProgressValue != 1 || state.Extra["pvf_path"] == "" || normalizeQuestTag(state.Extra["quest_type"]) != "clear map" {
		t.Fatalf("stored state = %+v", state)
	}
	second, err := owner.ApplyAccept(ctx, catalog, CharacterEligibility{Level: 1, Job: 2}, cmd)
	if err != nil {
		t.Fatalf("ApplyAccept retry error = %v", err)
	}
	if !second.Idempotent || second.InitTrigger != 1 {
		t.Fatalf("retry result = %+v", second)
	}
}

func TestOwnerApplyAcceptActivatesNoRewardMainSubQuests(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "acc-quest",
		Job:         "2",
		Level:       3,
	}); err != nil {
		t.Fatalf("save character: %v", err)
	}
	if err := repos.Quest.Save(ctx, dnfrepo.QuestRecord{
		CharacterID: "19",
		States: map[int64]dnfrepo.QuestState{
			3145: {Status: "completed"},
		},
	}); err != nil {
		t.Fatalf("save quest: %v", err)
	}
	source := catalogTestSource{
		DefaultList: "3145 `pre.qst`\n3146 `parent.qst`\n3157 `hunt.qst`\n3054 `clear.qst`\n",
		"n_quest/pre.qst": questCatalogTestDefinition("[epic]", 1, 99, "[gunner]",
			"[type]\n`[clear map]`\n[int data]\n76126\n"),
		"n_quest/parent.qst": questCatalogTestDefinition("[epic]", 3, 99, "[gunner]",
			"[type]\n`[quest clear]`\n[pre required quest]\n3145\n[int data]\n3157 3054\n"),
		"n_quest/hunt.qst": questCatalogTestDefinition("[sub]", 3, 99, "[gunner]",
			"[type]\n`[hunt enemy]`\n[main quest]\n3146\n[int data]\n3 -1 13099 3 1\n"),
		"n_quest/clear.qst": questCatalogTestDefinition("[sub]", 1, 99, "[gunner]",
			"[type]\n`[clear map]`\n[main quest]\n3146\n[int data]\n76136\n"),
	}
	index, err := dnfpvf.Build(context.Background(), source, dnfpvf.BuildOptions{Lists: []string{DefaultList}})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := Load(context.Background(), index)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	result, err := owner.ApplyAccept(ctx, catalog, CharacterEligibility{Level: 3, Job: 2}, NewQuestIDCommand(alignedRequest("acc-quest", 19), "accept_quest", QuestIDRequest{QuestID: 3146}))
	if err != nil {
		t.Fatalf("ApplyAccept parent error = %v", err)
	}
	if result.QuestID != 3146 || result.InitTrigger != 2 || result.Idempotent {
		t.Fatalf("parent accept result = %+v", result)
	}
	stored, ok, err := repos.Quest.Load(ctx, "19")
	if err != nil || !ok {
		t.Fatalf("load stored quest ok=%t err=%v", ok, err)
	}
	for _, check := range []struct {
		id      int64
		trigger int64
		linked  bool
	}{
		{id: 3146, trigger: 2},
		{id: 3157, trigger: 1, linked: true},
		{id: 3054, trigger: 1, linked: true},
	} {
		state := stored.States[check.id]
		if state.Status != "active" || state.ProgressValue != check.trigger {
			t.Fatalf("quest %d state = %+v", check.id, state)
		}
		if check.linked && (state.Extra["main_quest_id"] != "3146" || state.Extra["auto_activated_by_main_quest"] != "true") {
			t.Fatalf("quest %d linked extra = %+v", check.id, state.Extra)
		}
	}
}

func TestOwnerApplyActiveQuestClearLinkedSubQuestActivationRepairsExistingParent(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "acc-quest",
		Job:         "2",
		Level:       3,
	}); err != nil {
		t.Fatalf("save character: %v", err)
	}
	if err := repos.Quest.Save(ctx, dnfrepo.QuestRecord{
		CharacterID: "19",
		States: map[int64]dnfrepo.QuestState{
			3146: {Status: "active", ProgressValue: 2},
		},
	}); err != nil {
		t.Fatalf("save quest: %v", err)
	}
	source := catalogTestSource{
		DefaultList: "3146 `parent.qst`\n3157 `hunt.qst`\n3054 `clear.qst`\n",
		"n_quest/parent.qst": questCatalogTestDefinition("[epic]", 3, 99, "[gunner]",
			"[type]\n`[quest clear]`\n[int data]\n3157 3054\n"),
		"n_quest/hunt.qst": questCatalogTestDefinition("[sub]", 3, 99, "[gunner]",
			"[type]\n`[hunt enemy]`\n[main quest]\n3146\n[int data]\n3 -1 13099 3 1\n"),
		"n_quest/clear.qst": questCatalogTestDefinition("[sub]", 1, 99, "[gunner]",
			"[type]\n`[clear map]`\n[main quest]\n3146\n[int data]\n76136\n"),
	}
	index, err := dnfpvf.Build(context.Background(), source, dnfpvf.BuildOptions{Lists: []string{DefaultList}})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := Load(context.Background(), index)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	activatedAt := time.Date(2026, 7, 17, 14, 20, 0, 0, time.UTC)
	result, err := owner.ApplyActiveQuestClearLinkedSubQuestActivation(ctx, catalog, "19", activatedAt)
	if err != nil {
		t.Fatalf("ApplyActiveQuestClearLinkedSubQuestActivation error = %v", err)
	}
	if result.Idempotent || len(result.Activations) != 2 || len(result.ChangedFields) != 1 || result.ChangedFields[0] != dnfrepo.QuestFieldStates {
		t.Fatalf("activation result=%+v", result)
	}
	stored, ok, err := repos.Quest.Load(ctx, "19")
	if err != nil || !ok {
		t.Fatalf("load stored quest ok=%t err=%v", ok, err)
	}
	for _, questID := range []int64{3157, 3054} {
		state := stored.States[questID]
		if state.Status != "active" || state.ProgressValue != 1 ||
			state.Extra["main_quest_id"] != "3146" ||
			state.Extra["auto_activated_by_main_quest"] != "true" ||
			state.Extra["auto_activation_reason"] != "active_quest_clear_parent_reconcile" {
			t.Fatalf("linked quest %d state=%+v", questID, state)
		}
	}
	replay, err := owner.ApplyActiveQuestClearLinkedSubQuestActivation(ctx, catalog, "19", activatedAt.Add(time.Second))
	if err != nil {
		t.Fatalf("replay activation error = %v", err)
	}
	if !replay.Idempotent || len(replay.Activations) != 0 || len(replay.ChangedFields) != 0 {
		t.Fatalf("replay result=%+v", replay)
	}
}

func TestOwnerApplyAcceptRejectsEventItemQuestWithoutMutation(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "18", AccountID: "acc-quest"}); err != nil {
		t.Fatal(err)
	}
	catalog := testAcceptCatalog(t, "[type]\n`[clear map]`\n[int data]\n76126\n[depend give item]\n1001 1\n")
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	cmd := NewQuestIDCommand(alignedRequest("acc-quest", 18), "accept_quest", QuestIDRequest{QuestID: 3145})
	if _, err := owner.ApplyAccept(ctx, catalog, CharacterEligibility{Level: 1, Job: 2}, cmd); !errors.Is(err, ErrQuestAcceptEventItemsRequired) {
		t.Fatalf("ApplyAccept error = %v", err)
	}
	if _, ok, err := repos.Quest.Load(ctx, "18"); err != nil || ok {
		t.Fatalf("event-item rejection mutated quest record ok=%t err=%v", ok, err)
	}
}

func TestOwnerApplyGiveUpPersistsActiveQuestRemoval(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "18", AccountID: "acc-quest"}); err != nil {
		t.Fatal(err)
	}
	if err := repos.Quest.Save(ctx, dnfrepo.QuestRecord{
		CharacterID: "18",
		States: map[int64]dnfrepo.QuestState{
			3144: {Status: "completed", ProgressValue: 1},
			3145: {Status: "active", ProgressValue: 7},
			3146: {Status: "active", ProgressValue: 2},
		},
		Progress: map[int64]dnfrepo.QuestState{
			3145: {Status: "active", ProgressValue: 7},
		},
	}); err != nil {
		t.Fatal(err)
	}
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	result, err := owner.ApplyGiveUp(
		ctx,
		testAcceptCatalog(t, "[type]\n`[disjoint item]`\n[int data]\n1\n"),
		NewQuestIDCommand(alignedRequest("acc-quest", 18), "giveup_quest", QuestIDRequest{QuestID: 3145}),
	)
	if err != nil {
		t.Fatalf("ApplyGiveUp() error = %v", err)
	}
	if result.AccountID != "acc-quest" || result.CharacterID != "18" || result.QuestID != 3145 {
		t.Fatalf("ApplyGiveUp() result = %+v", result)
	}
	stored, ok, err := repos.Quest.Load(ctx, "18")
	if err != nil || !ok {
		t.Fatalf("load stored quest ok=%t err=%v", ok, err)
	}
	if _, exists := stored.States[3145]; exists {
		t.Fatalf("abandoned quest remains in states: %+v", stored.States[3145])
	}
	if _, exists := stored.Progress[3145]; exists {
		t.Fatalf("abandoned quest remains in progress: %+v", stored.Progress[3145])
	}
	if stored.States[3144].Status != "completed" || stored.States[3146].Status != "active" {
		t.Fatalf("unrelated quest states changed: %+v", stored.States)
	}
	if _, err := owner.ApplyGiveUp(
		ctx,
		testAcceptCatalog(t, "[type]\n`[disjoint item]`\n[int data]\n1\n"),
		NewQuestIDCommand(alignedRequest("acc-quest", 18), "giveup_quest", QuestIDRequest{QuestID: 3145}),
	); !errors.Is(err, ErrQuestNotActive) {
		t.Fatalf("ApplyGiveUp() replay error = %v, want %v", err, ErrQuestNotActive)
	}
}

func TestOwnerApplyGiveUpHonorsPVFAndAssetBoundary(t *testing.T) {
	for _, test := range []struct {
		name    string
		extra   string
		wantErr error
	}{
		{name: "cant giveup", extra: "[type]\n`[disjoint item]`\n[cant giveup]\n", wantErr: ErrQuestCannotGiveUp},
		{name: "quest items", extra: "[type]\n`[seeking]`\n[int data]\n1001 1\n", wantErr: ErrGiveUpNeedsAssets},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			repos := dnfrepomemory.NewMemoryGroup()
			if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "18", AccountID: "acc-quest"}); err != nil {
				t.Fatal(err)
			}
			if err := repos.Quest.Save(ctx, dnfrepo.QuestRecord{
				CharacterID: "18",
				States:      map[int64]dnfrepo.QuestState{3145: {Status: "active", ProgressValue: 1}},
			}); err != nil {
				t.Fatal(err)
			}
			owner, err := NewOwner(repos)
			if err != nil {
				t.Fatal(err)
			}
			_, err = owner.ApplyGiveUp(
				ctx,
				testAcceptCatalog(t, test.extra),
				NewQuestIDCommand(alignedRequest("acc-quest", 18), "giveup_quest", QuestIDRequest{QuestID: 3145}),
			)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("ApplyGiveUp() error = %v, want %v", err, test.wantErr)
			}
			stored, ok, loadErr := repos.Quest.Load(ctx, "18")
			if loadErr != nil || !ok || stored.States[3145].Status != "active" {
				t.Fatalf("rejected give-up mutated quest ok=%t err=%v state=%+v", ok, loadErr, stored.States[3145])
			}
		})
	}
}

func testAcceptCatalog(t *testing.T, extra string) *Catalog {
	t.Helper()
	source := catalogTestSource{
		DefaultList:          "3145 `accept.qst`\n",
		"n_quest/accept.qst": questCatalogTestDefinition("[epic]", 1, 99, "[gunner]", extra),
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

func alignedRequest(accountID string, selectedCharacterID uint16) alignedcmd.Request {
	return alignedcmd.Request{AccountID: accountID, SelectedCharacterID: selectedCharacterID}
}
