package quest

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	dnfprofession "longheng.io/server/internal/modules/dnf/profession"
	"longheng.io/server/internal/modules/dnf/progression"
	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
	dnfskill "longheng.io/server/internal/modules/dnf/skill"
)

func TestOwnerApplyFinishSettlementCommitsAndReplaysOneReceipt(t *testing.T) {
	ctx := context.Background()
	repos := finishOwnerSeed(t, ctx)
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	tables := finishOwnerProgression(t)
	input := finishOwnerInput(tables, finishOwnerAllocator(nil))

	first, err := owner.ApplyFinishSettlement(ctx, finishOwnerCatalog(), input)
	if err != nil {
		t.Fatal(err)
	}
	if !first.AtomicCommitted || first.Replayed || first.CompletionKey != "run-19/op117/426" || first.QuestID != 3145 {
		t.Fatalf("first result identity=%+v", first)
	}
	if first.ExperienceDelta != 100 || first.PreviousLevel != 1 || first.NewLevel != 2 || first.NewExperience != 100 || first.SPDelta != 30 {
		t.Fatalf("first progression=%+v", first)
	}
	if len(first.Items) != 1 || first.Items[0].ItemID != 10403 || first.Items[0].Delta != 2 || first.Items[0].PostCount != 2 {
		t.Fatalf("first items=%+v", first.Items)
	}
	if len(first.Currency) != 1 || first.Currency[0].Name != "gold" || first.Currency[0].Delta != 10 || first.Currency[0].PostValue != 10 {
		t.Fatalf("first currency=%+v", first.Currency)
	}
	questRecord, ok, err := repos.Quest.Load(ctx, "19")
	if err != nil || !ok {
		t.Fatalf("load quest ok=%t err=%v", ok, err)
	}
	state := questRecord.States[3145]
	if state.Status != "completed" || state.Extra["reward_state"] != "granted" || state.Extra["completion_key"] != "run-19/op117/426" || state.Extra[finishSettlementReceiptKey] == "" {
		t.Fatalf("persisted quest=%+v", state)
	}

	second, err := owner.ApplyFinishSettlement(ctx, finishOwnerCatalog(), input)
	if err != nil {
		t.Fatal(err)
	}
	if !second.AtomicCommitted || !second.Replayed || second.ExperienceDelta != first.ExperienceDelta || second.SPDelta != first.SPDelta || len(second.Items) != 1 || second.Items[0].PostCount != 2 {
		t.Fatalf("replay result=%+v", second)
	}
	if len(second.Currency) != 1 || second.Currency[0].PostValue != 10 {
		t.Fatalf("replay currency=%+v", second.Currency)
	}
	inventory, ok, err := repos.Inventory.Load(ctx, "19")
	if err != nil || !ok || inventory.Slots["0:65"].Count != 2 {
		t.Fatalf("replay duplicated inventory ok=%t err=%v inventory=%+v", ok, err, inventory)
	}
	character, ok, err := repos.Character.Load(ctx, "19")
	if err != nil || !ok || character.Stats["gold"] != 10 {
		t.Fatalf("replay duplicated gold ok=%t err=%v character=%+v", ok, err, character)
	}
}

func TestOwnerApplyFinishSettlementAppliesAndReceiptsGrowthContractQuestExperience(t *testing.T) {
	ctx := context.Background()
	repos := finishOwnerSeed(t, ctx)
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	input := finishOwnerInput(finishOwnerProgression(t), finishOwnerAllocator(nil))
	input.ExperienceBonusPercent = 20

	first, err := owner.ApplyFinishSettlement(ctx, finishOwnerCatalog(), input)
	if err != nil {
		t.Fatal(err)
	}
	if first.BaseExperienceDelta != 100 ||
		first.ExperienceBonusDelta != 20 ||
		first.ExperienceDelta != 120 ||
		first.NewExperience != 120 {
		t.Fatalf("growth-contract quest progression=%+v, want base=100 bonus=20 total=120", first)
	}

	// The durable receipt owns the committed total. Expiry before a reconnect
	// replay must neither remove nor duplicate the already-granted bonus.
	input.ExperienceBonusPercent = 0
	replayed, err := owner.ApplyFinishSettlement(ctx, finishOwnerCatalog(), input)
	if err != nil {
		t.Fatal(err)
	}
	if !replayed.Replayed ||
		replayed.BaseExperienceDelta != 100 ||
		replayed.ExperienceBonusDelta != 20 ||
		replayed.ExperienceDelta != 120 ||
		replayed.NewExperience != 120 {
		t.Fatalf("growth-contract quest replay=%+v", replayed)
	}
}

func TestOwnerApplyFinishSettlementConsumesSeekingMaterialsAndReplays(t *testing.T) {
	ctx := context.Background()
	repos := finishOwnerSeed(t, ctx)
	acceptedAt := time.Date(2026, 7, 23, 15, 40, 0, 0, time.UTC)
	if err := repos.Quest.Save(ctx, dnfrepo.QuestRecord{CharacterID: "19", States: map[int64]dnfrepo.QuestState{
		600: {Status: "active", ProgressValue: 0, UpdatedAt: acceptedAt},
	}}); err != nil {
		t.Fatal(err)
	}
	raw := make([]byte, 0x77)
	binary.LittleEndian.PutUint16(raw[0:2], 8)
	binary.LittleEndian.PutUint32(raw[2:6], 9001)
	binary.LittleEndian.PutUint32(raw[6:10], 30)
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			"0:8": {ItemID: 9001, Count: 30, RawEntry: raw, Extra: map[string]string{"count": "30"}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	definition := Definition{
		ID: 600, Path: "n_quest/submit_material.qst", Type: "[seeking]",
		LevelMin: 1, LevelMax: 99, Difficulty: "N", IntData: []int64{9001, 30},
	}
	catalog := &Catalog{ordered: []Definition{definition}, byID: map[int64]Definition{definition.ID: definition}}
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	input := FinishCommitInput{
		AccountID: "acc", CharacterID: "19", QuestID: definition.ID, Multiplier: 1,
		CommittedAt: time.Date(2026, 7, 23, 15, 41, 0, 0, time.UTC), Progression: finishOwnerProgression(t),
	}
	first, err := owner.ApplyFinishSettlement(ctx, catalog, input)
	if err != nil {
		t.Fatal(err)
	}
	if !first.AtomicCommitted || first.Replayed || len(first.ConsumedItems) != 1 {
		t.Fatalf("first result=%+v consumed=%+v", first, first.ConsumedItems)
	}
	consumed := first.ConsumedItems[0]
	if consumed.SlotKey != "0:8" || consumed.SlotIndex != 8 || consumed.ItemID != 9001 || consumed.Delta != 30 || consumed.PostCount != 0 || len(consumed.RawEntry) != 0 {
		t.Fatalf("consumed material=%+v", consumed)
	}
	inventory, found, err := repos.Inventory.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load inventory found=%t err=%v", found, err)
	}
	if _, exists := inventory.Slots["0:8"]; exists {
		t.Fatalf("material slot was not removed: %+v", inventory.Slots["0:8"])
	}

	second, err := owner.ApplyFinishSettlement(ctx, catalog, input)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Replayed || len(second.ConsumedItems) != 1 || second.ConsumedItems[0].Delta != 30 {
		t.Fatalf("replay consumed=%+v", second.ConsumedItems)
	}
	inventory, found, err = repos.Inventory.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("reload inventory found=%t err=%v", found, err)
	}
	if _, exists := inventory.Slots["0:8"]; exists {
		t.Fatalf("replay consumed material twice or left stale slot: %+v", inventory.Slots["0:8"])
	}
}

func TestOwnerApplyFinishSettlementConsumesAccountSharedSeekingMaterialsAndReplays(t *testing.T) {
	ctx := context.Background()
	repos := finishOwnerSeed(t, ctx)
	acceptedAt := time.Date(2026, 7, 27, 16, 0, 0, 0, time.UTC)
	if err := repos.Quest.Save(ctx, dnfrepo.QuestRecord{CharacterID: "19", States: map[int64]dnfrepo.QuestState{
		602: {Status: "active", ProgressValue: 0, UpdatedAt: acceptedAt},
	}}); err != nil {
		t.Fatal(err)
	}
	raw := make([]byte, 0x77)
	binary.LittleEndian.PutUint16(raw[0:2], 358)
	binary.LittleEndian.PutUint32(raw[2:6], 3037)
	binary.LittleEndian.PutUint32(raw[6:10], 130)
	if err := repos.AccountInventory.Save(ctx, dnfrepo.AccountInventoryRecord{
		AccountID: "acc",
		Slots: map[string]dnfrepo.ItemStack{
			dnfrepo.AccountSharedInventorySlotKey(358): {ItemID: 3037, Count: 130, RawEntry: raw, Extra: map[string]string{"count": "130"}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	definition := Definition{
		ID: 602, Path: "n_quest/account_shared_submit_material.qst", Type: "[seeking]",
		LevelMin: 1, LevelMax: 99, Difficulty: "N", IntData: []int64{3037, 100},
	}
	catalog := &Catalog{ordered: []Definition{definition}, byID: map[int64]Definition{definition.ID: definition}}
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	input := FinishCommitInput{
		AccountID: "acc", CharacterID: "19", QuestID: definition.ID, Multiplier: 1,
		CommittedAt: time.Date(2026, 7, 27, 16, 1, 0, 0, time.UTC), Progression: finishOwnerProgression(t),
	}
	first, err := owner.ApplyFinishSettlement(ctx, catalog, input)
	if err != nil {
		t.Fatal(err)
	}
	if !first.AtomicCommitted || first.Replayed || len(first.ConsumedItems) != 1 {
		t.Fatalf("first result=%+v consumed=%+v", first, first.ConsumedItems)
	}
	consumed := first.ConsumedItems[0]
	if consumed.SlotKey != "0:358" || consumed.SlotIndex != 358 || consumed.ItemID != 3037 || consumed.Delta != 100 || consumed.PostCount != 30 {
		t.Fatalf("shared consumed material=%+v", consumed)
	}
	account, found, err := repos.AccountInventory.Load(ctx, "acc")
	if err != nil || !found {
		t.Fatalf("load account inventory found=%t err=%v", found, err)
	}
	stack := account.Slots[dnfrepo.AccountSharedInventorySlotKey(358)]
	if stack.Count != 30 || binary.LittleEndian.Uint32(stack.RawEntry[6:10]) != 30 || stack.Extra["count"] != "30" {
		t.Fatalf("shared material after finish=%+v", stack)
	}
	if got := first.PostCommitAccountInventory.Slots["0:358"].Count; got != 30 {
		t.Fatalf("post-commit shared count=%d want=30", got)
	}

	second, err := owner.ApplyFinishSettlement(ctx, catalog, input)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Replayed || len(second.ConsumedItems) != 1 || second.ConsumedItems[0].Delta != 100 {
		t.Fatalf("replay consumed=%+v", second.ConsumedItems)
	}
	account, _, _ = repos.AccountInventory.Load(ctx, "acc")
	if got := account.Slots["0:358"].Count; got != 30 {
		t.Fatalf("replay consumed shared material twice: count=%d", got)
	}
}

func TestOwnerApplyFinishSettlementRollsBackAccountSharedMaterialsOnAllocatorFailure(t *testing.T) {
	ctx := context.Background()
	repos := finishOwnerSeed(t, ctx)
	acceptedAt := time.Date(2026, 7, 27, 16, 2, 0, 0, time.UTC)
	if err := repos.Quest.Save(ctx, dnfrepo.QuestRecord{CharacterID: "19", States: map[int64]dnfrepo.QuestState{
		603: {Status: "active", ProgressValue: 0, UpdatedAt: acceptedAt},
	}}); err != nil {
		t.Fatal(err)
	}
	raw := make([]byte, 0x77)
	binary.LittleEndian.PutUint16(raw[0:2], 358)
	binary.LittleEndian.PutUint32(raw[2:6], 3037)
	binary.LittleEndian.PutUint32(raw[6:10], 100)
	if err := repos.AccountInventory.Save(ctx, dnfrepo.AccountInventoryRecord{
		AccountID: "acc",
		Slots:     map[string]dnfrepo.ItemStack{"0:358": {ItemID: 3037, Count: 100, RawEntry: raw}},
	}); err != nil {
		t.Fatal(err)
	}
	definition := Definition{
		ID: 603, Path: "n_quest/account_shared_rollback.qst", Type: "[seeking]",
		LevelMin: 1, LevelMax: 99, Difficulty: "N", IntData: []int64{3037, 100},
		RewardType: "[item]", RewardItems: []RewardItemRule{{ItemID: 10403, Count: 1}},
	}
	catalog := &Catalog{ordered: []Definition{definition}, byID: map[int64]Definition{definition.ID: definition}}
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("reward allocation rejected")
	_, err = owner.ApplyFinishSettlement(ctx, catalog, FinishCommitInput{
		AccountID: "acc", CharacterID: "19", QuestID: definition.ID, Multiplier: 1,
		CommittedAt: time.Date(2026, 7, 27, 16, 3, 0, 0, time.UTC), Progression: finishOwnerProgression(t),
		AllocateItem: finishOwnerAllocator(wantErr),
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("ApplyFinishSettlement error=%v want=%v", err, wantErr)
	}
	account, _, _ := repos.AccountInventory.Load(ctx, "acc")
	if got := account.Slots["0:358"].Count; got != 100 {
		t.Fatalf("failed settlement changed shared material count=%d", got)
	}
	quests, _, _ := repos.Quest.Load(ctx, "19")
	if state := quests.States[603]; state.Status != "active" || state.Extra[finishSettlementReceiptKey] != "" {
		t.Fatalf("failed settlement changed quest=%+v", state)
	}
}

func TestOwnerApplyFinishSettlementRejectsMissingSeekingMaterials(t *testing.T) {
	ctx := context.Background()
	repos := finishOwnerSeed(t, ctx)
	if err := repos.Quest.Save(ctx, dnfrepo.QuestRecord{CharacterID: "19", States: map[int64]dnfrepo.QuestState{
		601: {Status: "active", ProgressValue: 0, UpdatedAt: time.Date(2026, 7, 23, 15, 42, 0, 0, time.UTC)},
	}}); err != nil {
		t.Fatal(err)
	}
	raw := make([]byte, 0x77)
	binary.LittleEndian.PutUint16(raw[0:2], 9)
	binary.LittleEndian.PutUint32(raw[2:6], 9001)
	binary.LittleEndian.PutUint32(raw[6:10], 29)
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			"0:9": {ItemID: 9001, Count: 29, RawEntry: raw},
		},
	}); err != nil {
		t.Fatal(err)
	}
	definition := Definition{
		ID: 601, Path: "n_quest/missing_submit_material.qst", Type: "[seeking]",
		LevelMin: 1, LevelMax: 99, Difficulty: "N", IntData: []int64{9001, 30},
	}
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	_, err = owner.ApplyFinishSettlement(ctx, &Catalog{ordered: []Definition{definition}, byID: map[int64]Definition{definition.ID: definition}}, FinishCommitInput{
		AccountID: "acc", CharacterID: "19", QuestID: definition.ID, Multiplier: 1,
		CommittedAt: time.Date(2026, 7, 23, 15, 43, 0, 0, time.UTC), Progression: finishOwnerProgression(t),
	})
	if !errors.Is(err, ErrFinishRequiredItemsMissing) {
		t.Fatalf("missing material error=%v", err)
	}
	inventory, _, _ := repos.Inventory.Load(ctx, "19")
	if got := inventory.Slots["0:9"].Count; got != 29 {
		t.Fatalf("missing material rollback count=%d want=29", got)
	}
	quests, _, _ := repos.Quest.Load(ctx, "19")
	if state := quests.States[601]; state.Status != "active" || state.Extra[finishSettlementReceiptKey] != "" {
		t.Fatalf("missing material mutated quest=%+v", state)
	}
}

func TestFinishRequiredItemAvailableCountTreatsEmptyAccountSharedCellAsZero(t *testing.T) {
	stack := dnfrepo.ItemStack{ItemID: 3037, Count: 0}
	if got := finishRequiredItemAvailableCount(stack, true); got != 0 {
		t.Fatalf("empty account-shared material count=%d want=0", got)
	}
	if got := finishRequiredItemAvailableCount(stack, false); got != 1 {
		t.Fatalf("ordinary single-item count=%d want=1", got)
	}
}

func TestOwnerApplyFinishSettlementRollsBackLateAllocatorFailure(t *testing.T) {
	ctx := context.Background()
	repos := finishOwnerSeed(t, ctx)
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("second asset write rejected")
	_, err = owner.ApplyFinishSettlement(ctx, finishOwnerCatalog(), finishOwnerInput(finishOwnerProgression(t), finishOwnerAllocator(wantErr)))
	if !errors.Is(err, wantErr) {
		t.Fatalf("ApplyFinishSettlement error=%v want=%v", err, wantErr)
	}
	character, _, _ := repos.Character.Load(ctx, "19")
	questRecord, _, _ := repos.Quest.Load(ctx, "19")
	skill, _, _ := repos.Skill.Load(ctx, "19")
	inventory, _, _ := repos.Inventory.Load(ctx, "19")
	if character.Level != 1 || character.Stats["exp"] != 0 || character.Stats["gold"] != 0 || skill.Points.SyncedLevel != 1 || skill.Points.TotalSP != 0 || len(inventory.Slots) != 0 {
		t.Fatalf("rollback aggregates character=%+v skill=%+v inventory=%+v", character, skill, inventory)
	}
	state := questRecord.States[3145]
	if state.Status != "active" || state.ProgressValue != 0 || state.Extra["reward_state"] != "pending" {
		t.Fatalf("rollback quest=%+v", state)
	}
}

func TestOwnerApplyFinishSettlementRejectsConflictingAndStaleReplay(t *testing.T) {
	ctx := context.Background()
	repos := finishOwnerSeed(t, ctx)
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	input := finishOwnerInput(finishOwnerProgression(t), finishOwnerAllocator(nil))
	if _, err := owner.ApplyFinishSettlement(ctx, finishOwnerCatalog(), input); err != nil {
		t.Fatal(err)
	}
	conflict := input
	conflict.ExpectedCompletionKey = "different-run"
	if _, err := owner.ApplyFinishSettlement(ctx, finishOwnerCatalog(), conflict); !errors.Is(err, ErrFinishQuestCompletionConflict) {
		t.Fatalf("conflicting replay error=%v", err)
	}
	inventory, ok, err := repos.Inventory.Load(ctx, "19")
	if err != nil || !ok {
		t.Fatal(err)
	}
	inventory = dnfrepo.CloneInventory(inventory)
	stack := inventory.Slots["0:65"]
	stack.Count++
	inventory.Slots["0:65"] = stack
	if err := repos.Inventory.Save(ctx, inventory); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.ApplyFinishSettlement(ctx, finishOwnerCatalog(), input); !errors.Is(err, ErrFinishQuestSettlementStale) {
		t.Fatalf("stale replay error=%v", err)
	}
}

func TestOwnerApplyFinishSettlementRecalculatesActiveQuestClearParents(t *testing.T) {
	ctx := context.Background()
	repos := finishOwnerSeed(t, ctx)
	record, found, err := repos.Quest.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load seed quest found=%t err=%v", found, err)
	}
	record = dnfrepo.CloneQuest(record)
	record.States[4100] = dnfrepo.QuestState{Status: "active", ProgressValue: 1}
	record.Progress = map[int64]dnfrepo.QuestState{
		4101: {Status: "active", ProgressValue: 2},
	}
	if err := repos.Quest.Save(ctx, record); err != nil {
		t.Fatal(err)
	}
	catalog := finishOwnerCatalog()
	parentInStates := Definition{ID: 4100, Type: "[quest clear]", IntData: []int64{3145}}
	parentInProgress := Definition{ID: 4101, Type: "[clear quest]", IntData: []int64{3145, 9999}}
	catalog.ordered = append(catalog.ordered, parentInStates, parentInProgress)
	catalog.byID[parentInStates.ID] = parentInStates
	catalog.byID[parentInProgress.ID] = parentInProgress

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	result, err := owner.ApplyFinishSettlement(ctx, catalog, finishOwnerInput(finishOwnerProgression(t), finishOwnerAllocator(nil)))
	if err != nil {
		t.Fatal(err)
	}
	if got := result.PostCommitQuest.States[4100].ProgressValue; got != 0 {
		t.Fatalf("same-field parent trigger=%d want=0", got)
	}
	if got := result.PostCommitQuest.Progress[4101].ProgressValue; got != 1 {
		t.Fatalf("cross-field parent trigger=%d want=1", got)
	}
	persisted, found, err := repos.Quest.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load persisted quest found=%t err=%v", found, err)
	}
	if got := persisted.States[4100].ProgressValue; got != 0 {
		t.Fatalf("persisted same-field parent trigger=%d want=0", got)
	}
	if got := persisted.Progress[4101].ProgressValue; got != 1 {
		t.Fatalf("persisted cross-field parent trigger=%d want=1", got)
	}
}

func TestOwnerApplyFinishSettlementRecomputesQuestClearParentBeforeFinish(t *testing.T) {
	ctx := context.Background()
	repos := finishOwnerSeed(t, ctx)
	if err := repos.Quest.Save(ctx, dnfrepo.QuestRecord{
		CharacterID: "19",
		States: map[int64]dnfrepo.QuestState{
			3146: {Status: "active", ProgressValue: 1},
			3157: {Status: "completed", ProgressValue: 0},
			3054: {Status: "completed", ProgressValue: 0},
		},
	}); err != nil {
		t.Fatal(err)
	}
	definition := Definition{
		ID: 3146, Path: "n_quest/elvengard_epic_02.qst", Type: "[quest clear]",
		LevelMin: 1, LevelMax: 99, Difficulty: "N", RewardType: "[item]",
		IntData: []int64{3157, 3054},
	}
	catalog := &Catalog{ordered: []Definition{definition}, byID: map[int64]Definition{definition.ID: definition}}
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	result, err := owner.ApplyFinishSettlement(ctx, catalog, FinishCommitInput{
		AccountID:    "acc",
		CharacterID:  "19",
		QuestID:      3146,
		Multiplier:   1,
		CommittedAt:  time.Date(2026, 7, 17, 12, 45, 0, 0, time.UTC),
		Progression:  finishOwnerProgression(t),
		AllocateItem: nil,
	})
	if err != nil {
		t.Fatal(err)
	}
	state := result.PostCommitQuest.States[3146]
	if state.Status != "completed" || state.ProgressValue != 0 ||
		state.Extra["completion_kind"] != "active_trigger_zero_op34" ||
		state.Extra["reward_state"] != finishRewardGranted {
		t.Fatalf("quest-clear parent state=%+v", state)
	}
}

func TestOwnerApplyFinishSettlementRejectsQuestClearParentWithMissingChildEvenIfTriggerZero(t *testing.T) {
	ctx := context.Background()
	repos := finishOwnerSeed(t, ctx)
	if err := repos.Quest.Save(ctx, dnfrepo.QuestRecord{
		CharacterID: "19",
		States: map[int64]dnfrepo.QuestState{
			3146: {Status: "active", ProgressValue: 0},
			3157: {Status: "completed", ProgressValue: 0},
		},
	}); err != nil {
		t.Fatal(err)
	}
	definition := Definition{
		ID: 3146, Path: "n_quest/elvengard_epic_02.qst", Type: "[quest clear]",
		LevelMin: 1, LevelMax: 99, Difficulty: "N", RewardType: "[item]",
		IntData: []int64{3157, 3054},
	}
	catalog := &Catalog{ordered: []Definition{definition}, byID: map[int64]Definition{definition.ID: definition}}
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	_, err = owner.ApplyFinishSettlement(ctx, catalog, FinishCommitInput{
		AccountID:    "acc",
		CharacterID:  "19",
		QuestID:      3146,
		Multiplier:   1,
		CommittedAt:  time.Date(2026, 7, 17, 12, 46, 0, 0, time.UTC),
		Progression:  finishOwnerProgression(t),
		AllocateItem: nil,
	})
	if !errors.Is(err, ErrFinishQuestNotPending) {
		t.Fatalf("finish missing-child quest-clear error=%v", err)
	}
}

func TestOwnerApplyFinishSettlementCompletesTerminalZeroTriggerLinkedSubQuest(t *testing.T) {
	ctx := context.Background()
	repos := finishOwnerSeed(t, ctx)
	if err := repos.Quest.Save(ctx, dnfrepo.QuestRecord{
		CharacterID: "19",
		States: map[int64]dnfrepo.QuestState{
			3249: {Status: "active", ProgressValue: 0},
			3609: {Status: "completed", ProgressValue: 0},
			3610: {Status: "active", ProgressValue: 0},
		},
	}); err != nil {
		t.Fatal(err)
	}
	parent := Definition{
		ID: 3249, Path: "n_quest/alphraira_epic_26.qst", Type: "[quest clear]",
		LevelMin: 1, LevelMax: 99, Difficulty: "N", RewardType: "[item]", RewardIntData: []int64{0, 0},
		IntData: []int64{3609, 3610},
	}
	completedChild := Definition{ID: 3609, MainQuestID: 3249, Type: "[meet npc]"}
	terminalChild := Definition{ID: 3610, MainQuestID: 3249, Type: "[seek n meet npc]"}
	catalog := &Catalog{
		ordered: []Definition{parent, completedChild, terminalChild},
		byID: map[int64]Definition{
			parent.ID:         parent,
			completedChild.ID: completedChild,
			terminalChild.ID:  terminalChild,
		},
	}
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	result, err := owner.ApplyFinishSettlement(ctx, catalog, FinishCommitInput{
		AccountID:    "acc",
		CharacterID:  "19",
		QuestID:      parent.ID,
		Multiplier:   1,
		CommittedAt:  time.Date(2026, 7, 29, 17, 50, 0, 0, time.UTC),
		Progression:  finishOwnerProgression(t),
		AllocateItem: nil,
	})
	if err != nil {
		t.Fatal(err)
	}
	if state := result.PostCommitQuest.States[parent.ID]; state.Status != "completed" || state.Extra["reward_state"] != finishRewardGranted {
		t.Fatalf("parent state=%+v", state)
	}
	childState := result.PostCommitQuest.States[terminalChild.ID]
	if childState.Status != "completed" || childState.ProgressValue != 0 ||
		childState.Extra["reward_state"] != finishRewardGranted ||
		childState.Extra["auto_completed_by_parent"] != "3249" ||
		childState.Extra["auto_complete_reason"] != "quest_clear_parent_terminal_zero_trigger" {
		t.Fatalf("terminal child state=%+v", childState)
	}
	persisted, found, err := repos.Quest.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load persisted quest found=%t err=%v", found, err)
	}
	if state := persisted.States[terminalChild.ID]; state.Status != "completed" || state.Extra["auto_completed_by_parent"] != "3249" {
		t.Fatalf("persisted terminal child state=%+v", state)
	}
}

func TestOwnerApplyFinishSettlementRejectsNonzeroTerminalLinkedSubQuest(t *testing.T) {
	ctx := context.Background()
	repos := finishOwnerSeed(t, ctx)
	if err := repos.Quest.Save(ctx, dnfrepo.QuestRecord{
		CharacterID: "19",
		States: map[int64]dnfrepo.QuestState{
			3249: {Status: "active", ProgressValue: 0},
			3609: {Status: "completed", ProgressValue: 0},
			3610: {Status: "active", ProgressValue: 1},
		},
	}); err != nil {
		t.Fatal(err)
	}
	parent := Definition{
		ID: 3249, Path: "n_quest/alphraira_epic_26.qst", Type: "[quest clear]",
		LevelMin: 1, LevelMax: 99, Difficulty: "N", RewardType: "[item]", RewardIntData: []int64{0, 0},
		IntData: []int64{3609, 3610},
	}
	completedChild := Definition{ID: 3609, MainQuestID: 3249, Type: "[meet npc]"}
	terminalChild := Definition{ID: 3610, MainQuestID: 3249, Type: "[seek n meet npc]"}
	catalog := &Catalog{
		ordered: []Definition{parent, completedChild, terminalChild},
		byID: map[int64]Definition{
			parent.ID:         parent,
			completedChild.ID: completedChild,
			terminalChild.ID:  terminalChild,
		},
	}
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	_, err = owner.ApplyFinishSettlement(ctx, catalog, FinishCommitInput{
		AccountID: "acc", CharacterID: "19", QuestID: parent.ID, Multiplier: 1,
		CommittedAt: time.Date(2026, 7, 29, 17, 51, 0, 0, time.UTC),
		Progression: finishOwnerProgression(t),
	})
	if !errors.Is(err, ErrFinishQuestNotPending) {
		t.Fatalf("finish nonzero terminal child error=%v", err)
	}
}

func TestOwnerApplyFinishSettlementCommitsNPCProfessionTransitionAndReplays(t *testing.T) {
	ctx := context.Background()
	repos := finishOwnerSeed(t, ctx)
	acceptedAt := time.Date(2026, 7, 17, 1, 0, 0, 0, time.UTC)
	quests := dnfrepo.QuestRecord{CharacterID: "19", States: map[int64]dnfrepo.QuestState{
		500: {Status: "active", ProgressValue: 0, UpdatedAt: acceptedAt},
	}}
	if err := repos.Quest.Save(ctx, quests); err != nil {
		t.Fatal(err)
	}
	skill, _, _ := repos.Skill.Load(ctx, "19")
	skill.Skills = map[int64]dnfrepo.SkillState{99: {Level: 3, Enabled: true}}
	skill.Layouts = map[int]dnfrepo.SkillLayout{0: {0: 99}}
	if err := repos.Skill.Save(ctx, skill); err != nil {
		t.Fatal(err)
	}

	profiles, skillCatalog := finishOwnerProfessionResources(t)
	definition := Definition{
		ID: 500, Path: "n_quest/change.qst", Type: "[meet npc]", LevelMin: 1, LevelMax: 99,
		Difficulty: "N", JobChangeQuest: 1, RewardType: "[grow type]", RewardIntData: []int64{2},
	}
	catalog := &Catalog{ordered: []Definition{definition}, byID: map[int64]Definition{definition.ID: definition}}
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	input := FinishCommitInput{
		AccountID: "acc", CharacterID: "19", QuestID: definition.ID,
		Multiplier: 1, CommittedAt: time.Date(2026, 7, 17, 1, 1, 0, 0, time.UTC),
		Progression: finishOwnerProgression(t), ProfessionProfiles: profiles, SkillCatalog: skillCatalog,
	}
	first, err := owner.ApplyFinishSettlement(ctx, catalog, input)
	if err != nil {
		t.Fatal(err)
	}
	wantKey := "quest-finish/19/500/" + fmt.Sprint(acceptedAt.UnixNano())
	if !first.AtomicCommitted || first.Replayed || !first.HasProfession || first.CompletionKey != wantKey || first.Profession.NewGrowType != 0x02 {
		t.Fatalf("profession result = %+v", first)
	}
	if first.PostCommitCharacter.Stats["grow_type"] != 0x02 || first.PostCommitSkill.Skills[1].Level != 1 ||
		first.PostCommitSkill.Skills[2].Level != 1 || first.PostCommitSkill.Skills[197].Level != 1 ||
		len(first.PostCommitSkill.Skills) != 3 {
		t.Fatalf("profession aggregate = character=%+v skill=%+v", first.PostCommitCharacter, first.PostCommitSkill)
	}
	wantGrants := []dnfprofession.Grant{{SkillID: 1, Level: 1}, {SkillID: 2, Level: 1}, {SkillID: 197, Level: 1}}
	if !reflect.DeepEqual(first.ProfessionGrants, wantGrants) {
		t.Fatalf("profession grants = %+v want=%+v", first.ProfessionGrants, wantGrants)
	}
	if first.PostCommitSkill.Points.RemainingSP != first.PostCommitSkill.Points.TotalSP || first.PostCommitSkill.Points.RemainingTP != first.PostCommitSkill.Points.TotalTP {
		t.Fatalf("class change points were not reset = %+v", first.PostCommitSkill.Points)
	}
	committedSkill := dnfrepo.CloneSkill(first.PostCommitSkill)
	persistedQuest, found, err := repos.Quest.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load persisted profession quest found=%t err=%v", found, err)
	}
	state := persistedQuest.States[definition.ID]
	if state.Status != "completed" || state.Extra["completion_kind"] != "active_trigger_zero_op34" || state.Extra["completion_key"] != wantKey || state.Extra[finishSettlementReceiptKey] == "" {
		t.Fatalf("persisted profession quest = %+v", state)
	}

	second, err := owner.ApplyFinishSettlement(ctx, catalog, input)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Replayed || second.CompletionKey != first.CompletionKey || second.Profession != first.Profession ||
		!reflect.DeepEqual(second.ProfessionGrants, wantGrants) || !reflect.DeepEqual(second.PostCommitSkill, committedSkill) {
		t.Fatalf("profession replay = %+v", second)
	}
}

func TestOwnerApplyFinishSettlementCommitsExpertJobAndReplays(t *testing.T) {
	ctx := context.Background()
	repos := finishOwnerSeed(t, ctx)
	acceptedAt := time.Date(2026, 7, 22, 2, 0, 0, 0, time.UTC)
	if err := repos.Quest.Save(ctx, dnfrepo.QuestRecord{CharacterID: "19", States: map[int64]dnfrepo.QuestState{
		2710: {Status: "active", ProgressValue: 0, UpdatedAt: acceptedAt},
	}}); err != nil {
		t.Fatal(err)
	}
	definition := Definition{
		ID: 2710, Path: "n_quest/Expertjob/normal_60_Expert_job_disjointer_2.qst", Type: "[seeking]",
		LevelMin: 1, LevelMax: 99, Difficulty: "N", JobChangeQuest: 20,
		RewardType: "[expert job]", RewardIntData: []int64{3},
	}
	catalog := &Catalog{ordered: []Definition{definition}, byID: map[int64]Definition{definition.ID: definition}}
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	input := FinishCommitInput{
		AccountID: "acc", CharacterID: "19", QuestID: definition.ID, Multiplier: 1,
		CommittedAt: time.Date(2026, 7, 22, 2, 1, 0, 0, time.UTC), Progression: finishOwnerProgression(t),
	}
	first, err := owner.ApplyFinishSettlement(ctx, catalog, input)
	if err != nil {
		t.Fatal(err)
	}
	if !first.AtomicCommitted || first.Replayed || !first.HasExpertJob || first.ExpertJobType != 3 || first.HasProfession {
		t.Fatalf("expert-job result = %+v", first)
	}
	if got := first.PostCommitCharacter.Stats["expert_job_type"]; got != 3 {
		t.Fatalf("expert job persisted type = %d, want 3", got)
	}
	second, err := owner.ApplyFinishSettlement(ctx, catalog, input)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Replayed || !second.HasExpertJob || second.ExpertJobType != 3 {
		t.Fatalf("expert-job replay = %+v", second)
	}
}

func TestOwnerApplyFinishSettlementRejectsOutOfRangePackedGrowType(t *testing.T) {
	ctx := context.Background()
	repos := finishOwnerSeed(t, ctx)
	character, _, _ := repos.Character.Load(ctx, "19")
	character.Stats["grow_type"] = 256
	if err := repos.Character.Save(ctx, character); err != nil {
		t.Fatal(err)
	}
	quests := dnfrepo.QuestRecord{CharacterID: "19", States: map[int64]dnfrepo.QuestState{
		500: {Status: "active", ProgressValue: 0, UpdatedAt: time.Date(2026, 7, 17, 1, 0, 0, 0, time.UTC)},
	}}
	if err := repos.Quest.Save(ctx, quests); err != nil {
		t.Fatal(err)
	}
	profiles, skillCatalog := finishOwnerProfessionResources(t)
	definition := Definition{
		ID: 500, Path: "n_quest/change.qst", Type: "[meet npc]", LevelMin: 1, LevelMax: 99,
		Difficulty: "N", JobChangeQuest: 1, RewardType: "[grow type]", RewardIntData: []int64{2},
	}
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	_, err = owner.ApplyFinishSettlement(ctx, &Catalog{ordered: []Definition{definition}, byID: map[int64]Definition{definition.ID: definition}}, FinishCommitInput{
		AccountID: "acc", CharacterID: "19", QuestID: definition.ID, Multiplier: 1,
		CommittedAt: time.Date(2026, 7, 17, 1, 1, 0, 0, time.UTC), Progression: finishOwnerProgression(t),
		ProfessionProfiles: profiles, SkillCatalog: skillCatalog,
	})
	if !errors.Is(err, ErrQuestNotAcceptable) {
		t.Fatalf("out-of-range grow_type error = %v", err)
	}
	persisted, _, _ := repos.Quest.Load(ctx, "19")
	state := persisted.States[definition.ID]
	if state.Status != "active" || state.Extra[finishSettlementReceiptKey] != "" {
		t.Fatalf("invalid grow_type mutated quest = %+v", state)
	}
}

func finishOwnerSeed(t *testing.T, ctx context.Context) dnfrepo.Group {
	t.Helper()
	repos := dnfrepomemory.NewMemoryGroup()
	for _, save := range []func() error{
		func() error {
			return repos.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "19", AccountID: "acc", Job: "2", Level: 1, Stats: map[string]int64{"exp": 0, "grow_type": 0}})
		},
		func() error {
			return repos.Quest.Save(ctx, dnfrepo.QuestRecord{CharacterID: "19", States: map[int64]dnfrepo.QuestState{3145: {Status: "active", ProgressValue: 0, Extra: map[string]string{"completion_key": "run-19/op117/426", "completion_kind": "clear_map", "reward_state": "pending"}}}})
		},
		func() error {
			return repos.Skill.Save(ctx, dnfrepo.SkillRecord{CharacterID: "19", Points: dnfrepo.SkillPointState{SyncedLevel: 1}})
		},
		func() error {
			return repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{CharacterID: "19", Slots: map[string]dnfrepo.ItemStack{}})
		},
		func() error { return repos.Equipment.Save(ctx, dnfrepo.EquipmentRecord{CharacterID: "19"}) },
	} {
		if err := save(); err != nil {
			t.Fatal(err)
		}
	}
	return repos
}

func finishOwnerCatalog() *Catalog {
	definition := Definition{ID: 3145, Path: "n_quest/finish.qst", Type: "[clear map]", LevelMin: 1, LevelMax: 99, Difficulty: "N", RewardType: "[item]", HasGoldReward: true, RewardItems: []RewardItemRule{{ItemID: 10403, Count: 2}}}
	return &Catalog{ordered: []Definition{definition}, byID: map[int64]Definition{3145: definition}}
}

func finishOwnerInput(tables *progression.Tables, allocator FinishItemAllocator) FinishCommitInput {
	return FinishCommitInput{
		AccountID: "acc", CharacterID: "19", QuestID: 3145, RewardSelectIndex: ^uint16(0),
		Multiplier: 1, ExpectedCompletionKey: "run-19/op117/426",
		CommittedAt: time.Date(2026, 7, 16, 6, 30, 0, 0, time.UTC), Progression: tables, AllocateItem: allocator,
	}
}

func finishOwnerAllocator(fail error) FinishItemAllocator {
	return func(record *dnfrepo.InventoryRecord, request FinishItemGrantRequest) (FinishCommittedItem, error) {
		if record.Slots == nil {
			record.Slots = make(map[string]dnfrepo.ItemStack)
		}
		const slot = uint16(65)
		const key = "0:65"
		postCount := request.Count
		if existing, ok := record.Slots[key]; ok {
			postCount += existing.Count
		}
		raw := make([]byte, 0x77)
		binary.LittleEndian.PutUint16(raw[0:2], slot)
		binary.LittleEndian.PutUint32(raw[2:6], uint32(request.ItemID))
		binary.LittleEndian.PutUint32(raw[6:10], uint32(postCount))
		record.Slots[key] = dnfrepo.ItemStack{ItemID: request.ItemID, Count: postCount, RawEntry: append([]byte(nil), raw...)}
		if fail != nil {
			return FinishCommittedItem{}, fail
		}
		return FinishCommittedItem{SlotKey: key, SlotIndex: slot, ItemID: request.ItemID, Delta: request.Count, PostCount: postCount, CountOrSeed: uint32(request.Count), RawEntry: raw}, nil
	}
}

type finishProgressionSource map[string]string

func (s finishProgressionSource) ReadText(path string) (string, error) {
	text, ok := s[path]
	if !ok {
		return "", fmt.Errorf("missing %s", path)
	}
	return text, nil
}

func finishOwnerProfessionResources(t *testing.T) (*dnfprofession.Profiles, *dnfskill.Table) {
	t.Helper()
	source := finishProgressionSource{
		dnfprofession.DefaultCharacterList: "2 `Gunner/Gunner.chr`\n",
		"character/Gunner/Gunner.chr": `[initial value]
[skill]
1 1
[/skill]
[growtype 3]
[skill]
2 1
197 1
[/skill]
`,
		dnfskill.DefaultList:       "2 `GunnerSkill.lst`\n",
		"skill/GunnerSkill.lst":    "1 `Gunner/a.skl` 2 `Gunner/b.skl` 197 `Gunner/mastery.skl`\n",
		"skill/Gunner/a.skl":       "[name]\n`a`\n[type]\n`[active]`\n",
		"skill/Gunner/b.skl":       "[name]\n`b`\n[type]\n`[passive]`\n",
		"skill/Gunner/mastery.skl": "[name]\n`mastery`\n[type]\n`[passive]`\n",
	}
	index, err := dnfpvf.Build(context.Background(), source, dnfpvf.BuildOptions{Lists: []string{dnfskill.DefaultList}})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := dnfskill.Load(context.Background(), index, dnfskill.Options{})
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := dnfprofession.LoadProfiles(context.Background(), source, catalog)
	if err != nil {
		t.Fatal(err)
	}
	return profiles, catalog
}

func finishOwnerProgression(t *testing.T) *progression.Tables {
	t.Helper()
	tables, err := progression.Load(context.Background(), finishProgressionSource{
		progression.ExperienceTablePath: "100 250 500\n",
		progression.SkillPointTablePath: "[sp table]\n1 0\n2 30\n3 30\n4 40\n[/sp table]\n[tp table]\n50 1\n[/tp table]\n",
		progression.QuestParameterPath:  "[difficulty]\n`N` 100\n[/difficulty]\n[exp reward table]\n100 -1\n200 -1\n300 -1\n[gold reward table]\n10 -1\n[green level penalty]\n80\n[grey level penalty]\n30\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	return tables
}

func TestOwnerApplyFinishSettlementCommitsSlotExpansionAndReplays(t *testing.T) {
	ctx := context.Background()
	repos := finishOwnerSeed(t, ctx)
	acceptedAt := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	if err := repos.Quest.Save(ctx, dnfrepo.QuestRecord{CharacterID: "19", States: map[int64]dnfrepo.QuestState{
		649: {Status: "active", ProgressValue: 0, UpdatedAt: acceptedAt, Extra: map[string]string{"completion_key": "run-19/op117/649", "completion_kind": "clear_map", "reward_state": "pending"}},
	}}); err != nil {
		t.Fatal(err)
	}
	definition := Definition{
		ID: 649, Path: "Gent/NightAssault/epic_60_NightAssault_second_2.qst", Type: "[clear map]",
		LevelMin: 1, LevelMax: 99, Difficulty: "N",
		RewardType: "[slot expansion]", RewardIntData: []int64{0},
	}
	catalog := &Catalog{ordered: []Definition{definition}, byID: map[int64]Definition{definition.ID: definition}}
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	input := FinishCommitInput{
		AccountID: "acc", CharacterID: "19", QuestID: 649, Multiplier: 1,
		CommittedAt: time.Date(2026, 7, 22, 10, 1, 0, 0, time.UTC), Progression: finishOwnerProgression(t),
	}
	first, err := owner.ApplyFinishSettlement(ctx, catalog, input)
	if err != nil {
		t.Fatal(err)
	}
	if !first.AtomicCommitted || first.Replayed || !first.HasSlotExpansion ||
		first.SlotExpansionIndex != 0 || first.SlotExpansionBit != ExEquipSlotSupport {
		t.Fatalf("slot expansion result = committed=%t replayed=%t has=%t index=%d bit=%d", first.AtomicCommitted, first.Replayed, first.HasSlotExpansion, first.SlotExpansionIndex, first.SlotExpansionBit)
	}
	if got := first.PostCommitCharacter.Stats["ex_equip_slot_stat"]; got != int64(ExEquipSlotSupport) {
		t.Fatalf("ex_equip_slot_stat = %d, want %d", got, ExEquipSlotSupport)
	}
	// Replay returns the receipt without granting twice.
	second, err := owner.ApplyFinishSettlement(ctx, catalog, input)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Replayed || !second.HasSlotExpansion ||
		second.SlotExpansionIndex != 0 || second.SlotExpansionBit != ExEquipSlotSupport {
		t.Fatalf("slot expansion replay = %+v", second)
	}
}

func TestOwnerApplyFinishSettlementSlotExpansionORsExistingBits(t *testing.T) {
	ctx := context.Background()
	repos := finishOwnerSeed(t, ctx)
	// Pre-set support bit already unlocked.
	character, _, _ := repos.Character.Load(ctx, "19")
	character.Stats["ex_equip_slot_stat"] = int64(ExEquipSlotSupport)
	if err := repos.Character.Save(ctx, character); err != nil {
		t.Fatal(err)
	}
	acceptedAt := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	if err := repos.Quest.Save(ctx, dnfrepo.QuestRecord{CharacterID: "19", States: map[int64]dnfrepo.QuestState{
		650: {Status: "active", ProgressValue: 0, UpdatedAt: acceptedAt, Extra: map[string]string{"completion_key": "run-19/op117/650", "completion_kind": "clear_map", "reward_state": "pending"}},
	}}); err != nil {
		t.Fatal(err)
	}
	definition := Definition{
		ID: 650, Path: "LuftHafen/pirateonthetrain/epic_65_pirateonthetrain_magic_stone_2.qst", Type: "[clear map]",
		LevelMin: 1, LevelMax: 99, Difficulty: "N",
		RewardType: "[slot expansion]", RewardIntData: []int64{1},
	}
	catalog := &Catalog{ordered: []Definition{definition}, byID: map[int64]Definition{definition.ID: definition}}
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	result, err := owner.ApplyFinishSettlement(ctx, catalog, FinishCommitInput{
		AccountID: "acc", CharacterID: "19", QuestID: 650, Multiplier: 1,
		CommittedAt: time.Date(2026, 7, 22, 10, 1, 0, 0, time.UTC), Progression: finishOwnerProgression(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.HasSlotExpansion || result.SlotExpansionIndex != 1 || result.SlotExpansionBit != ExEquipSlotMagicStone {
		t.Fatalf("magic stone expansion = %+v", result)
	}
	want := int64(ExEquipSlotSupport | ExEquipSlotMagicStone)
	if got := result.PostCommitCharacter.Stats["ex_equip_slot_stat"]; got != want {
		t.Fatalf("ex_equip_slot_stat = %d, want %d (support|magicstone)", got, want)
	}
}
