package dnfbridge

import (
	"os"
	"testing"

	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestCurrentDungeonIndependentDropParsesFixedInlineAndSkipsListReference(t *testing.T) {
	text := `[independent drop]
0 100 5001 1000000 0 0 0 0 2 0 0 0 0 1 20 -1 0
0 100 0 1000000 0 0 0 0 2 0 0 0 0 1 20 -1 1
[list]
5002 1 5003 3
[/list]
0 100 0 1000000 0 0 0 0 2 0 0 0 0 1 20 -1 2
[list]
7
[/list]
[/independent drop]`
	entries, skipped, err := parseCurrentDungeonIndependentDrops(text)
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 1 || len(entries[100]) != 2 {
		t.Fatalf("entries=%+v skipped=%d", entries[100], skipped)
	}
	catalog := &currentDungeonSpecialDropCatalog{IndependentByMonster: entries}
	items := currentDungeonIndependentDropItems(catalog, 100, 0, 10, 0, newCurrentDungeonDropLCG(1))
	if len(items) != 4 || items[0] != 5001 || items[1] != 5001 {
		t.Fatalf("selected items=%v", items)
	}
	for _, itemID := range items[2:] {
		if itemID != 5002 && itemID != 5003 {
			t.Fatalf("inline selection item=%d", itemID)
		}
	}
}

func TestCurrentDungeonIndependentDropAppliesLevelDifficultyAndAttempts(t *testing.T) {
	text := `[independent drop]
0 200 6001 1000000 1000000 0 0 0 1 2 0 0 0 10 20 1 0
[/independent drop]`
	entries, _, err := parseCurrentDungeonIndependentDrops(text)
	if err != nil {
		t.Fatal(err)
	}
	catalog := &currentDungeonSpecialDropCatalog{IndependentByMonster: entries}
	if got := currentDungeonIndependentDropItems(catalog, 200, 0, 15, 0, newCurrentDungeonDropLCG(1)); len(got) != 0 {
		t.Fatalf("difficulty mismatch selected %v", got)
	}
	if got := currentDungeonIndependentDropItems(catalog, 200, 1, 9, 0, newCurrentDungeonDropLCG(1)); len(got) != 0 {
		t.Fatalf("level mismatch selected %v", got)
	}
	if got := currentDungeonIndependentDropItems(catalog, 200, 1, 15, 0, newCurrentDungeonDropLCG(1)); len(got) != 2 || got[0] != 6001 || got[1] != 6001 {
		t.Fatalf("matching selection=%v", got)
	}
}

func TestCurrentDungeonIndependentDropProbabilityAppliesGrowthContractAndCaps(t *testing.T) {
	tests := []struct {
		base    int
		percent int64
		want    int
	}{
		{base: 500000, percent: 0, want: 500000},
		{base: 500000, percent: 20, want: 600000},
		{base: 833333, percent: 20, want: 1000000},
		{base: 1000000, percent: 30, want: 1000000},
		{base: 0, percent: 20, want: 0},
	}
	for _, test := range tests {
		if got := currentDungeonIndependentDropProbability(test.base, test.percent); got != test.want {
			t.Fatalf("base=%d percent=%d got=%d want=%d", test.base, test.percent, got, test.want)
		}
	}
}

func TestCurrentDungeonWorldDropParsesWeightedLevelAndRolls(t *testing.T) {
	text := `[world drop]
10 0 7001 25000 7002 75000 7999 0 -1
200 0 8001 100000 -1
[/world drop]`
	levels, err := parseCurrentDungeonWorldDrops(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(levels) != 1 || levels[10].TotalWeight != 100000 || len(levels[10].Items) != 2 {
		t.Fatalf("levels=%+v", levels)
	}
	catalog := &currentDungeonSpecialDropCatalog{WorldByLevel: levels}
	itemID, selected := currentDungeonWorldDropItem(catalog, 10, newCurrentDungeonDropLCG(2))
	if !selected || (itemID != 7001 && itemID != 7002) {
		t.Fatalf("selected=%t item=%d", selected, itemID)
	}
	if _, selected := currentDungeonWorldDropItem(catalog, 200, newCurrentDungeonDropLCG(2)); selected {
		t.Fatal("level 200 must not generate a world drop")
	}
}

func TestRealPVFSpecialDropCatalog(t *testing.T) {
	pvfPath := os.Getenv("DNF_WORLDMAP_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNF_WORLDMAP_REAL_PVF_SMOKE to verify special drop tables")
	}
	archive, err := platformpvf.Open(pvfPath)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := loadCurrentDungeonSpecialDrops(archive)
	if err != nil {
		t.Fatal(err)
	}
	items, err := newPVFDungeonDropCatalog(archive)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.IndependentByMonster) == 0 || len(catalog.WorldByLevel) == 0 {
		t.Fatalf("independent monsters=%d world levels=%d", len(catalog.IndependentByMonster), len(catalog.WorldByLevel))
	}
	independentEntries := 0
	itemIDs := make(map[uint32]struct{})
	for _, entries := range catalog.IndependentByMonster {
		independentEntries += len(entries)
		for _, entry := range entries {
			if entry.ItemID > 0 {
				itemIDs[entry.ItemID] = struct{}{}
			}
			for _, item := range entry.Items {
				itemIDs[item.ItemID] = struct{}{}
			}
		}
	}
	worldItems := 0
	for _, level := range catalog.WorldByLevel {
		worldItems += len(level.Items)
		for _, item := range level.Items {
			itemIDs[item.ItemID] = struct{}{}
		}
	}
	for itemID := range itemIDs {
		if _, resolveErr := items.ResolveItem(itemID); resolveErr != nil {
			t.Fatalf("special drop item %d is not resolvable: %v", itemID, resolveErr)
		}
	}
	for _, levelNumber := range []int{26, 27} {
		level := catalog.WorldByLevel[levelNumber]
		t.Logf("world level %d: total_weight=%d candidates=%d", levelNumber, level.TotalWeight, len(level.Items))
	}
	for _, monsterID := range []int64{200, 250, 260, 270, 500, 501, 62993} {
		t.Logf("independent monster %d: entries=%d", monsterID, len(catalog.IndependentByMonster[monsterID]))
	}
	t.Logf("real special drops: independent monsters=%d entries=%d skipped_list_flag2=%d world_levels=%d weighted_world_items=%d unique_items=%d",
		len(catalog.IndependentByMonster), independentEntries, catalog.SkippedListFlag2, len(catalog.WorldByLevel), worldItems, len(itemIDs))
}
