package onlineevent

import (
	"context"
	"os"
	"reflect"
	"testing"

	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestRealScriptPVFAttendanceCatalog(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to verify the production attendance catalog")
	}
	archive, err := platformpvf.LoadArchive(platformpvf.Options{Path: pvfPath})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := LoadAttendancePVFCatalog(context.Background(), archive)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := catalog.Snapshot()
	if !reflect.DeepEqual(snapshot.ProcessDurationsSeconds, []int64{1800, 1800, 3600, 3600}) {
		t.Fatalf("production raw process durations=%v", snapshot.ProcessDurationsSeconds)
	}
	if !reflect.DeepEqual(snapshot.SumThresholds, []int64{5, 10, 15, 25}) {
		t.Fatalf("production raw sum thresholds=%v", snapshot.SumThresholds)
	}
	if !reflect.DeepEqual(snapshot.RewardActivityForSum, []int64{0}) {
		t.Fatalf("production raw reward activity for sum=%v", snapshot.RewardActivityForSum)
	}
	wantItemIDs := []int64{490006574, 490006575, 490006576, 490006577, 490006578, 490006579, 490006580, 490006581}
	gotRewards := append([]AttendancePVFItemReward(nil), snapshot.RewardItems...)
	gotRewards = append(gotRewards, snapshot.RewardItemsForSum...)
	if len(gotRewards) != len(wantItemIDs) {
		t.Fatalf("production rewards=%+v", gotRewards)
	}
	for index, reward := range gotRewards {
		if reward.ItemID != wantItemIDs[index] || reward.Count != 1 || reward.DefinitionPath == "" {
			t.Fatalf("production reward[%d]=%+v want_item_id=%d", index, reward, wantItemIDs[index])
		}
	}
}
