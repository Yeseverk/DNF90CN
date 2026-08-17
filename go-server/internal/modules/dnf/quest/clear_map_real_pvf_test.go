package quest

import (
	"context"
	"os"
	"testing"
	"time"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestRealScriptPVFQuest3145ClearMapCompletionPlan(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to verify real clear-map completion")
	}
	archive, err := platformpvf.LoadArchive(platformpvf.Options{Path: pvfPath})
	if err != nil {
		t.Fatal(err)
	}
	index, err := dnfpvf.Build(context.Background(), archive, dnfpvf.BuildOptions{Lists: []string{DefaultList}})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := Load(context.Background(), index)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := catalog.PlanClearMapCompletion(dnfrepo.QuestRecord{
		CharacterID: "19",
		States: map[int64]dnfrepo.QuestState{
			3145: {Status: "active", ProgressValue: 1},
		},
	}, ClearMapCompletionInput{
		DungeonID: 3, MapID: 76126, CompletionKey: "real-pvf-smoke/run/op117", CompletedAt: time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Completions) != 1 || plan.Completions[0].QuestID != 3145 || plan.Record.States[3145].ProgressValue != 0 || plan.Record.States[3145].Extra["reward_state"] != "pending" {
		t.Fatalf("real quest 3145 clear-map plan = %+v", plan)
	}
	t.Logf("real quest 3145 clear-map completion path=%q map=%d", plan.Completions[0].PVFPath, 76126)
}
