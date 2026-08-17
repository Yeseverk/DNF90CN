package combatpower

import (
	"context"
	"math"
	"testing"
)

type memorySource map[string]string

func (s memorySource) ReadText(relativePath string) (string, error) {
	return s[relativePath], nil
}

func TestCatalogAggregatesPVFDamageCategoriesWithoutConflatingAdditions(t *testing.T) {
	source := memorySource{
		"equipment/equipment.lst": `
1 ` + "`character/common/a.equ`" + `
2 ` + "`character/common/b.equ`" + `
3 ` + "`character/common/c.equ`" + `
`,
		"etc/equipmentpartset.etc": `
90 ` + "`character/partset2/set_item_90.equ`" + ` ` + "`测试套装`" + `
`,
		"equipment/character/common/a.equ": `
[rarity]
4
[minimum level]
90
[grade]
96
[equipment type]
` + "`[weapon]`" + `
[part set index]
90
[stat by condition]
` + "`add increase damage` `%`" + ` 16
[stat by condition]
` + "`add increase critical damage` `%`" + ` 18
`,
		"equipment/character/common/b.equ": `
[rarity]
3
[minimum level]
85
[grade]
90
[equipment type]
` + "`[ring]`" + `
[part set index]
90
[unique increase damage]
0 ` + "`%`" + ` 12
[unique increase critical damage]
0 ` + "`%`" + ` 10
`,
		"equipment/character/common/c.equ": `
[rarity]
2
[minimum level]
60
[grade]
50
[equipment type]
` + "`[artifact red]`" + `
[part set index]
90
[unique increase damage]
0 ` + "`%`" + ` 20
`,
		"equipment/character/partset2/set_item_90.equ": `
[piece set ability]
2
[add absolute damage]
` + "`all` `%`" + ` 22
[/piece set ability]
[piece set ability]
3
[add absolute damage]
` + "`fire` `%`" + ` 15
[add absolute damage]
` + "`water` `%`" + ` 15
[stat by condition]
` + "`all attack bonus rate` `%`" + ` 35
[/piece set ability]
`,
	}
	catalog, err := Load(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	result, err := catalog.Aggregate(context.Background(), []int64{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	want := Affixes{
		WhiteDamage:        37,
		YellowDamage:       20,
		CriticalDamage:     10,
		YellowAdditional:   16,
		CriticalAdditional: 18,
		AllAttack:          35,
	}
	assertAffixesEqual(t, result.Affixes, want)
	if result.EquippedItems != 3 || len(result.ActiveSets) != 1 ||
		result.ActiveSets[0].ID != 90 || result.ActiveSets[0].Pieces != 3 {
		t.Fatalf("result metadata = %+v", result)
	}
	// 90/96/4 -> 2680, 85/90/3 -> 2450, 60/50/2 -> 1650.
	if result.PVFEquipmentScore != 6780 || result.ScoredItems != 3 ||
		result.Level90EpicItems != 1 {
		t.Fatalf("equipment grade metadata = %+v", result)
	}
}

func TestPVFEquipmentBaseScoreBoundsUntrustedMetadata(t *testing.T) {
	item := ItemDefinition{Rarity: 10, MinimumLevel: 200, Grade: 200}
	if got, want := pvfEquipmentBaseScore(item), 6000; got != want {
		t.Fatalf("bounded equipment score=%d want=%d", got, want)
	}
	if got := boundedPVFScoreMetadata(-1, 200); got != 0 {
		t.Fatalf("negative metadata bound=%d", got)
	}
	if got := boundedPVFScoreMetadata(9999, 200); got != 200 {
		t.Fatalf("large metadata bound=%d", got)
	}
}

func TestCatalogDoesNotActivateIncompleteSet(t *testing.T) {
	source := memorySource{
		"equipment/equipment.lst":                      "1 `character/common/a.equ`\n",
		"etc/equipmentpartset.etc":                     "90 `character/partset2/set_item_90.equ` `测试套装`\n",
		"equipment/character/common/a.equ":             "[part set index]\n90\n",
		"equipment/character/partset2/set_item_90.equ": "[piece set ability]\n2\n[add absolute damage]\n`all` `%` 22\n[/piece set ability]\n",
	}
	catalog, err := Load(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	result, err := catalog.Aggregate(context.Background(), []int64{1})
	if err != nil {
		t.Fatal(err)
	}
	if result.Affixes.WhiteDamage != 0 || len(result.ActiveSets) != 0 {
		t.Fatalf("incomplete set result = %+v", result)
	}
}

func assertAffixesEqual(t *testing.T, got, want Affixes) {
	t.Helper()
	values := [][3]any{
		{"white", got.WhiteDamage, want.WhiteDamage},
		{"yellow", got.YellowDamage, want.YellowDamage},
		{"critical", got.CriticalDamage, want.CriticalDamage},
		{"yellow_additional", got.YellowAdditional, want.YellowAdditional},
		{"critical_additional", got.CriticalAdditional, want.CriticalAdditional},
		{"all_attack", got.AllAttack, want.AllAttack},
	}
	for _, value := range values {
		if math.Abs(value[1].(float64)-value[2].(float64)) > 0.0001 {
			t.Fatalf("%s = %v want %v", value[0], value[1], value[2])
		}
	}
}
