package dnfbridge

import (
	"os"
	"testing"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestParseCurrentPremiumListDocumentMapsItemToTypeAndDuration(t *testing.T) {
	text := "[type]\n22\n[target item]\n2\n[item]\n30\n[term]\n1\n[/term]\n[/item]\n[item]\n10096109\n[term]\n60\n[is term unit minute]\n1\n[/is term unit minute]\n[/item]\n[/type]\n" +
		"[type]\n27\n[target item]\n1\n[item]\n43\n[term]\n1\n[/term]\n[/item]\n[/type]\n" +
		"[type]\n84\n[item]\n2660409\n[term]\n3\n[/term]\n[/item]\n[bonus exp]\n20\n[/bonus exp]\n[quest item drop rate]\n20\n[/quest item drop rate]\n[independent drop rate]\n20 23 27 30\n[/independent drop rate]\n[/type]\n"
	document, err := dnfpvf.Parse("etc/premiumlist_new.etc", text)
	if err != nil {
		t.Fatal(err)
	}
	catalog := &currentPremiumCatalog{contractsByItem: make(map[int64]currentPremiumContractInfo)}
	parseCurrentPremiumListDocument(document, catalog)

	monarch, ok := catalog.contractsByItem[30]
	if !ok || monarch.PremiumType != 22 || monarch.DurationSeconds != 86400 {
		t.Fatalf("monarch = %+v", monarch)
	}
	minuteItem, ok := catalog.contractsByItem[10096109]
	if !ok || minuteItem.PremiumType != 22 || minuteItem.DurationSeconds != 3600 {
		t.Fatalf("minute item = %+v, want 3600s", minuteItem)
	}
	expert, ok := catalog.contractsByItem[43]
	if !ok || expert.PremiumType != 27 || expert.DurationSeconds != 86400 {
		t.Fatalf("expert = %+v", expert)
	}
	growth, ok := catalog.contractsByItem[2660409]
	if !ok || growth.PremiumType != 84 || growth.DurationSeconds != 3*86400 {
		t.Fatalf("growth = %+v", growth)
	}
	effect := catalog.effectsByType[84]
	if effect.BonusExperiencePercent != 20 ||
		effect.QuestItemDropRatePercent != 20 ||
		len(effect.IndependentDropRatePercents) != 4 ||
		effect.independentDropRatePercent(1) != 20 ||
		effect.independentDropRatePercent(4) != 30 ||
		effect.independentDropRatePercent(9) != 30 {
		t.Fatalf("growth effect = %+v", effect)
	}
}

func TestParseCurrentPremiumDevilSlotsParsesAllPerks(t *testing.T) {
	text := "[selectable character premium]\n" +
		"-1 2681927 -1 7 0 2000 `魔王之契约` 0 -1 " +
		"100817 2682205 6 7 0 250 `自动修理` 0 -1 " +
		"100882 2682206 7 7 0 200 `高效开罐` 0 -1 " +
		"100542 2681934 0 7 0 200 `黄金卡牌免费翻` 0 -1 " +
		"100818 2682205 6 30 0 500 `自动修理` 0 -1\n" +
		"[/selectable character premium]\n"
	document, err := dnfpvf.Parse("etc/cerashop.etc", text)
	if err != nil {
		t.Fatal(err)
	}
	catalog := &currentPremiumCatalog{devilSlots: make(map[uint32]currentPremiumDevilSlotInfo)}
	parseCurrentPremiumDevilSlots(document, catalog)

	if len(catalog.devilSlots) != 4 {
		t.Fatalf("devil slots = %d, want 4: %+v", len(catalog.devilSlots), catalog.devilSlots)
	}
	repair, ok := catalog.devilSlots[100817]
	if !ok || repair.Slot != 6 || repair.Days != 7 || repair.CeraPrice != 250 || repair.ItemID != 2682205 {
		t.Fatalf("repair perk = %+v", repair)
	}
	card, ok := catalog.devilSlots[100542]
	if !ok || card.Slot != 0 || card.Days != 7 || card.CeraPrice != 200 {
		t.Fatalf("card perk = %+v", card)
	}
	if _, ok := catalog.devilSlots[2681927]; ok {
		t.Fatalf("display-only contract row must be skipped")
	}
}

func TestParseCurrentCrystalContractCubesUsesNativeSelectionOrder(t *testing.T) {
	text := "3037 `material/cubepiece_clear.stk`\n" +
		"3035 `material/cubepiece_red.stk`\n" +
		"3262 `material/cubepiece_gold.stk`\n" +
		"3033 `material/cubepiece_black.stk`\n" +
		"3036 `material/cubepiece_blue.stk`\n" +
		"3034 `material/cubepiece_white.stk`\n"
	document, err := dnfpvf.Parse("stackable/stackable.lst", text)
	if err != nil {
		t.Fatal(err)
	}
	catalog := &currentPremiumCatalog{}
	if err := parseCurrentCrystalContractCubes(document, catalog); err != nil {
		t.Fatal(err)
	}
	want := [6]int64{3033, 3034, 3035, 3036, 3037, 3262}
	if catalog.crystalCubeIDs != want {
		t.Fatalf("crystal cube IDs = %v, want %v", catalog.crystalCubeIDs, want)
	}
}

func TestRealPVFPremiumCatalogCoversNamedContracts(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("DNFBRIDGE_REAL_PVF_SMOKE not set")
	}
	archive, err := platformpvf.Open(pvfPath)
	if err != nil {
		t.Fatalf("open PVF: %v", err)
	}
	catalog, err := buildCurrentPremiumCatalog(archive)
	if err != nil {
		t.Fatalf("build catalog: %v", err)
	}
	expect := map[int64]int64{
		30:       22, // 霸王 1天
		34:       22, // 霸王 15天
		43:       27, // 达人 1天
		46:       27, // 达人 15天
		2660703:  84, // 成长 [item] 1天
		10000389: 97, // 晶之契约 7天
	}
	for itemID, wantType := range expect {
		info, ok := catalog.contractsByItem[itemID]
		if !ok || info.PremiumType != wantType {
			t.Fatalf("item %d = %+v, want type %d", itemID, info, wantType)
		}
		if info.DurationSeconds < 86400 {
			t.Fatalf("item %d duration = %d, want >= 1 day", itemID, info.DurationSeconds)
		}
	}
	blackDiamond, ok := catalog.contractsByItem[193]
	if !ok || blackDiamond.PremiumType != 29 || blackDiamond.DurationSeconds < 86400 {
		t.Fatalf("Black Diamond item 193 = %+v, want current type 29 and >= one day", blackDiamond)
	}
	for itemID, wantType := range map[int64]int64{
		31:       22, // 霸王之契约 3天
		44:       27, // 达人之契约 3天
		2660409:  84, // 成长之契约 3天
		10000388: 97, // 晶之契约 3天
	} {
		info, ok := catalog.contractsByItem[itemID]
		if !ok || info.PremiumType != wantType || info.DurationSeconds != 3*86400 {
			t.Fatalf("three-day contract item %d = %+v, want type=%d duration=%d", itemID, info, wantType, 3*86400)
		}
	}
	growthEffect := catalog.effectsByType[84]
	if growthEffect.BonusExperiencePercent != 20 ||
		growthEffect.QuestItemDropRatePercent != 20 ||
		len(growthEffect.IndependentDropRatePercents) != 4 ||
		growthEffect.independentDropRatePercent(1) != 20 ||
		growthEffect.independentDropRatePercent(4) != 30 {
		t.Fatalf("runtime-PVF growth effect = %+v, want exp=20 quest_drop=20 independent=[20 23 27 30]", growthEffect)
	}
	for _, premiumType := range []int64{29, 80, 88, 92, 100} {
		found := false
		for _, info := range catalog.contractsByItem {
			if info.PremiumType == premiumType {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("runtime-PVF premium type %d has no activation item", premiumType)
		}
	}
	// [target item] values (e.g. 2660408) are display metadata, never an
	// activation item (86JP PremiumCatalog ignores them).
	if _, ok := catalog.contractsByItem[2660408]; ok {
		t.Fatalf("[target item] 2660408 must not become an activation item")
	}
	if len(catalog.devilSlots) != 16 {
		t.Fatalf("devil perks = %d, want 16 (8 slots x 7/30 days)", len(catalog.devilSlots))
	}
	if want := [6]int64{3033, 3034, 3035, 3036, 3037, 3262}; catalog.crystalCubeIDs != want {
		t.Fatalf("runtime-PVF crystal cube IDs = %v, want %v", catalog.crystalCubeIDs, want)
	}
	expectedDevilProducts := map[uint32]struct {
		slot int64
		days int64
	}{
		100542: {slot: 0, days: 7},
		100539: {slot: 1, days: 7},
		100540: {slot: 2, days: 7},
		100541: {slot: 3, days: 7},
		100537: {slot: 4, days: 7},
		100538: {slot: 5, days: 7},
		100817: {slot: 6, days: 7},
		100882: {slot: 7, days: 7},
		100550: {slot: 0, days: 30},
		100547: {slot: 1, days: 30},
		100548: {slot: 2, days: 30},
		100549: {slot: 3, days: 30},
		100545: {slot: 4, days: 30},
		100546: {slot: 5, days: 30},
		100818: {slot: 6, days: 30},
		100883: {slot: 7, days: 30},
	}
	for commodityID, expected := range expectedDevilProducts {
		info, found := catalog.devilSlots[commodityID]
		if !found || info.Slot != expected.slot || info.Days != expected.days {
			t.Fatalf(
				"devil commodity %d = %+v found=%t, want slot=%d days=%d",
				commodityID,
				info,
				found,
				expected.slot,
				expected.days,
			)
		}
	}
}
