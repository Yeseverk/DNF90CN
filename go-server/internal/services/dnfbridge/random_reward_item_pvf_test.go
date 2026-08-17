package dnfbridge

import (
	"os"
	"testing"

	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestResolveCurrentRandomRewardItemUsesPVFVisualOutcomeWeights(t *testing.T) {
	source := dungeonDropCatalogTestSource{
		"monster/monster.lst":          "",
		"equipment/equipment.lst":      "",
		"stackable/stackable.lst":      "100 `event/firework.stk`\n200 `event/surprise.stk`\n",
		"stackable/event/firework.stk": "[stackable type] `[random reward item]`\n[chn random image percent]\n0 99000 -1 `default.ani` ``\n1 1000 200 `surprise.ani` ``\n[/chn random image percent]\n",
		"stackable/event/surprise.stk": "[stackable type] `[booster]`\n[stack limit] 1000\n",
	}
	catalog, err := newPVFDungeonDropCatalog(source)
	if err != nil {
		t.Fatal(err)
	}
	resolution, err := resolveCurrentRandomRewardItem(catalog, source, 100)
	if err != nil {
		t.Fatal(err)
	}
	if resolution.SourceItemID != 100 || resolution.SourcePVFPath != "stackable/event/firework.stk" || resolution.StackableType != "random reward item" {
		t.Fatalf("resolution source = %+v", resolution)
	}
	if len(resolution.Outcomes) != 2 || resolution.Outcomes[0].Weight != 99000 || resolution.Outcomes[0].Reward.ItemID != 0 ||
		resolution.Outcomes[1].Weight != 1000 || resolution.Outcomes[1].Reward.ItemID != 200 || resolution.Outcomes[1].Reward.Kind != "stackable" {
		t.Fatalf("outcomes = %+v", resolution.Outcomes)
	}
}

func TestResolveCurrentRandomRewardItemRealFirecrackers(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to verify runtime random-reward firecrackers")
	}
	archive, err := platformpvf.Open(pvfPath)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := newPVFDungeonDropCatalog(archive)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		itemID       int64
		firstWeight  int64
		secondWeight int64
		rewardID     int64
	}{
		{itemID: 490007317, firstWeight: 99000, secondWeight: 1000, rewardID: 490701733},
		{itemID: 490701730, firstWeight: 99000, secondWeight: 1000, rewardID: 490701733},
		{itemID: 490005593, firstWeight: 80000, secondWeight: 20000, rewardID: 490005595},
	} {
		resolution, err := resolveCurrentRandomRewardItem(catalog, archive, test.itemID)
		if err != nil {
			t.Fatalf("item=%d: %v", test.itemID, err)
		}
		if resolution.SourceItemID != test.itemID || len(resolution.Outcomes) != 2 ||
			resolution.Outcomes[0].Weight != test.firstWeight || resolution.Outcomes[0].Reward.ItemID != 0 ||
			resolution.Outcomes[1].Weight != test.secondWeight || resolution.Outcomes[1].Reward.ItemID != test.rewardID {
			t.Fatalf("item=%d resolution=%+v", test.itemID, resolution)
		}
	}
}
