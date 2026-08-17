package quest

import (
	"context"
	"testing"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestOwnerApplyClearMapCompletionPersistsAndVerifiesIdempotently(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "19", AccountID: "account"}); err != nil {
		t.Fatal(err)
	}
	if err := repos.Quest.Save(ctx, dnfrepo.QuestRecord{
		CharacterID: "19",
		States: map[int64]dnfrepo.QuestState{
			3145: {Status: "active", ProgressValue: 1},
		},
	}); err != nil {
		t.Fatal(err)
	}
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	input := ClearMapCompletionInput{
		DungeonID: 3, MapID: 76126, CompletionKey: "runtime-12/op117/430", CompletedAt: time.Date(2026, 7, 15, 20, 45, 0, 0, time.UTC),
	}
	first, err := owner.ApplyClearMapCompletion(ctx, clearMapTestCatalog(t), "19", input)
	if err != nil {
		t.Fatal(err)
	}
	if first.Idempotent || len(first.Completions) != 1 || len(first.ChangedFields) != 1 {
		t.Fatalf("first result = %+v", first)
	}
	persisted, ok, err := repos.Quest.Load(ctx, "19")
	if err != nil || !ok {
		t.Fatalf("load persisted ok=%t err=%v", ok, err)
	}
	state := persisted.States[3145]
	if state.ProgressValue != 0 || state.Extra["completion_key"] != input.CompletionKey || state.Extra["reward_state"] != "pending" {
		t.Fatalf("persisted state = %+v", state)
	}
	second, err := owner.ApplyClearMapCompletion(ctx, clearMapTestCatalog(t), "19", input)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Idempotent || len(second.Completions) != 1 || len(second.ChangedFields) != 0 {
		t.Fatalf("second result = %+v", second)
	}
}

func TestOwnerApplyClearMapCompletionDoesNotCreateFakeQuest(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "20", AccountID: "account"}); err != nil {
		t.Fatal(err)
	}
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	result, err := owner.ApplyClearMapCompletion(ctx, clearMapTestCatalog(t), "20", ClearMapCompletionInput{
		MapID: 76126, CompletionKey: "runtime-empty", CompletedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Completions) != 0 || len(result.ChangedFields) != 0 || result.Idempotent {
		t.Fatalf("result = %+v", result)
	}
	if _, ok, err := repos.Quest.Load(ctx, "20"); err != nil || ok {
		t.Fatalf("empty completion created quest row ok=%t err=%v", ok, err)
	}
}
