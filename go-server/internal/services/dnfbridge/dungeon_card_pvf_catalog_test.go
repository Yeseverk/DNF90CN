package dnfbridge

import (
	"errors"
	"os"
	"reflect"
	"testing"

	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestPVFDungeonCardRewardCatalogPreservesTypedRowsWithoutSelectingRewards(t *testing.T) {
	source := dungeonDropCatalogTestSource{
		dungeonCardGoldReferencePath: "[gold drop ref table]\n1 17 13\n16 141 13\n",
		dungeonCardDropRulePath: "[drop prob count]\n2\n" +
			"[drop prob]\n1 15 986 60 250 0 0\n16 23 750 30 210 17 15\n" +
			"[item drop ref table]\n1 0 3\n16 7 3\n",
		"monster/monster.lst":                       "1 `dummy.mob`\n",
		"stackable/stackable.lst":                   "3227 `material/solvent_magicpower.stk`\n",
		"equipment/equipment.lst":                   "9001 `weapon/test_sword.equ`\n",
		"stackable/material/solvent_magicpower.stk": "[stackable type] `[material]`\n[stack limit] 999\n",
		"equipment/weapon/test_sword.equ":           "[durability] 57\n",
	}
	catalog, err := newPVFDungeonCardRewardCatalog(source)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot := catalog.Snapshot(); snapshot != (dungeonCardPVFCatalogSnapshot{
		GoldReferences: 2, DropProbabilityRows: 2, ItemDropReferences: 2,
	}) {
		t.Fatalf("snapshot=%+v", snapshot)
	}
	if row, found := catalog.GoldReference(1); !found || row != (dungeonCardPVFGoldReference{LookupKey: 1, ValueA: 17, ValueB: 13}) {
		t.Fatalf("gold row=%+v found=%t", row, found)
	}
	if row, found := catalog.DropProbability(16); !found || row.KeyMin != 16 || row.KeyMax != 23 ||
		!reflect.DeepEqual(row.RateValues, [5]int64{750, 30, 210, 17, 15}) {
		t.Fatalf("drop row=%+v found=%t", row, found)
	}
	if row, found := catalog.ItemDropReference(16); !found || row != (dungeonCardPVFItemDropReference{LookupKey: 16, ValueA: 7, ValueB: 3}) {
		t.Fatalf("item ref=%+v found=%t", row, found)
	}
	item, err := catalog.ResolveItem(9001)
	if err != nil || item.Kind != dungeonDropItemEquipment || item.Durability != 57 {
		t.Fatalf("resolved item=%+v err=%v", item, err)
	}
	if _, found := catalog.GoldReference(2); found {
		t.Fatal("catalog fabricated a missing gold level")
	}
	if _, found := catalog.DropProbability(24); found {
		t.Fatal("catalog fabricated a missing drop range")
	}
}

func TestPVFDungeonCardRewardCatalogRejectsMissingOrMalformedSourceRows(t *testing.T) {
	if _, err := newPVFDungeonCardRewardCatalog(nil); !errors.Is(err, errDungeonCardPVFSourceRequired) {
		t.Fatalf("nil source error=%v", err)
	}
	base := dungeonDropCatalogTestSource{
		dungeonCardGoldReferencePath: "[gold drop ref table]\n1 17 13\n",
		dungeonCardDropRulePath:      "[drop prob count]\n1\n[drop prob]\n1 15 986 60 250 0 0\n[item drop ref table]\n1 0 3\n",
		"monster/monster.lst":        "1 `dummy.mob`\n",
		"stackable/stackable.lst":    "1 `dummy.stk`\n",
		"equipment/equipment.lst":    "2 `dummy.equ`\n",
	}
	tests := []struct {
		name   string
		mutate func(dungeonDropCatalogTestSource)
		want   error
	}{
		{
			name: "gold row width",
			mutate: func(source dungeonDropCatalogTestSource) {
				source[dungeonCardGoldReferencePath] = "[gold drop ref table]\n1 17\n"
			},
			want: errDungeonCardPVFSectionShape,
		},
		{
			name: "drop declared count",
			mutate: func(source dungeonDropCatalogTestSource) {
				source[dungeonCardDropRulePath] = "[drop prob count]\n2\n[drop prob]\n1 15 986 60 250 0 0\n[item drop ref table]\n1 0 3\n"
			},
			want: errDungeonCardPVFSectionShape,
		},
		{
			name: "drop count missing",
			mutate: func(source dungeonDropCatalogTestSource) {
				source[dungeonCardDropRulePath] = "[drop prob]\n1 15 986 60 250 0 0\n[item drop ref table]\n1 0 3\n"
			},
			want: errDungeonCardPVFSectionMissing,
		},
		{
			name: "item table missing",
			mutate: func(source dungeonDropCatalogTestSource) {
				source[dungeonCardDropRulePath] = "[drop prob count]\n1\n[drop prob]\n1 15 986 60 250 0 0\n"
			},
			want: errDungeonCardPVFSectionMissing,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := make(dungeonDropCatalogTestSource, len(base))
			for path, text := range base {
				source[path] = text
			}
			test.mutate(source)
			_, err := newPVFDungeonCardRewardCatalog(source)
			if !errors.Is(err, test.want) {
				t.Fatalf("error=%v want=%v", err, test.want)
			}
		})
	}
}

func TestRealScriptPVFDungeonCardRewardCatalogMapsOnlyExactSourceRows(t *testing.T) {
	pvfPath := os.Getenv("DNF_WORLDMAP_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNF_WORLDMAP_REAL_PVF_SMOKE to run real dungeon card source smoke")
	}
	archive, err := platformpvf.Open(pvfPath)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := newPVFDungeonCardRewardCatalog(archive)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot := catalog.Snapshot(); snapshot != (dungeonCardPVFCatalogSnapshot{
		GoldReferences: 200, DropProbabilityRows: 10, ItemDropReferences: 200,
	}) {
		t.Fatalf("real catalog snapshot=%+v", snapshot)
	}
	if gold, found := catalog.GoldReference(1); !found || gold.ValueA != 17 || gold.ValueB != 13 {
		t.Fatalf("real level-1 gold=%+v found=%t", gold, found)
	}
	if gold, found := catalog.GoldReference(86); !found || gold.ValueA != 1357 || gold.ValueB != 9 {
		t.Fatalf("real level-86 gold=%+v found=%t", gold, found)
	}
	if rates, found := catalog.DropProbability(1); !found || rates.RateValues != ([5]int64{986, 60, 250, 0, 0}) {
		t.Fatalf("real level-1 drop row=%+v found=%t", rates, found)
	}
	if reference, found := catalog.ItemDropReference(86); !found || reference.ValueA != 7 || reference.ValueB != 3 {
		t.Fatalf("real level-86 item reference=%+v found=%t", reference, found)
	}
}
