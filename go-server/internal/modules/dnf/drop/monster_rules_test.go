package drop

import (
	"errors"
	"os"
	"reflect"
	"testing"

	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestLoadMonsterRulesPreservesEveryCurrentPVFMatrixWithoutInventingRateNames(t *testing.T) {
	rules, err := LoadMonsterRules(monsterRuleTestSource{MonsterRulePath: validMonsterRuleText()})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot := rules.Snapshot(); snapshot != (MonsterRuleSnapshot{
		ProbabilityRows: 2, RarityRows: 4, PartyBonusRows: 5,
		DifficultyBonusRows: 5, MonsterTypeRows: 5, ItemReferences: 2, ConditionRows: 4,
	}) {
		t.Fatalf("snapshot=%+v", snapshot)
	}
	plan := rules.PlanSource(16)
	if !plan.ProbabilityFound || !plan.ItemReferenceFound ||
		plan.Probability != (MonsterProbabilityRow{KeyMin: 16, KeyMax: 23, Rates: [5]int64{750, 30, 210, 17, 15}}) ||
		plan.ItemReference != (MonsterItemReference{LookupKey: 16, ValueA: 7, ValueB: 3}) {
		t.Fatalf("plan=%+v", plan)
	}
	missing := rules.PlanSource(24)
	if missing.ProbabilityFound || missing.ItemReferenceFound {
		t.Fatalf("missing lookup fabricated source rows: %+v", missing)
	}

	tables := rules.RawTables()
	if tables.RarityDecision[0] != ([7]int64{700000, 964900, 990000, 1000000, 1000001, 1000002, 1000003}) ||
		tables.PartyBonus[1] != ([4]float64{1, 1.3, 1.6, 2}) ||
		tables.DifficultyBonus[3] != ([5]float64{1, 1.4, 1.6, 2.2, 2}) ||
		tables.MonsterTypeBonus[4] != ([4]float64{1, 2, 3, 4}) ||
		tables.FirstBossNamed != ([2]int64{1, 1}) ||
		tables.ConditionRates[3] != ([2]int64{85, 15}) ||
		tables.GoldQuantity != ([4]int64{1, 2, 4, 6}) ||
		tables.GoldVolume != ([2]int64{100, 110}) ||
		tables.RarityControl != ([2][5]int64{{2, 13, 17, 0, -5}, {3, 13, 17, 0, -5}}) {
		t.Fatalf("raw matrices changed: %+v", tables)
	}
}

func TestLoadMonsterRulesRejectsPartialOrReinterpretedPVFShapes(t *testing.T) {
	if _, err := LoadMonsterRules(nil); !errors.Is(err, ErrMonsterRuleSourceRequired) {
		t.Fatalf("nil source error=%v", err)
	}
	tests := []struct {
		name   string
		mutate func(string) string
	}{
		{
			name: "declared row count mismatch",
			mutate: func(text string) string {
				return replaceOnce(t, text, "[drop prob count]\n2", "[drop prob count]\n3")
			},
		},
		{
			name: "overlapping lookup ranges",
			mutate: func(text string) string {
				return replaceOnce(t, text, "16 23 750", "15 23 750")
			},
		},
		{
			name: "party matrix short",
			mutate: func(text string) string {
				return replaceOnce(t, text,
					"1 1 1 1 1 1.3 1.6 2 1 1.2 1.4 1.6 1 1.2 1.4 1.6 1 1.2 1.4 1.6",
					"1 1 1 1")
			},
		},
		{
			name: "nonnumeric rate token",
			mutate: func(text string) string {
				return replaceOnce(t, text, "1.00 1.00 1.00 1.40", "1.00 unknown 1.00 1.40")
			},
		},
		{
			name: "rarity threshold decreases",
			mutate: func(text string) string {
				return replaceOnce(t, text, "700000 964900 990000", "700000 600000 990000")
			},
		},
		{
			name: "item reference keys unsorted",
			mutate: func(text string) string {
				return replaceOnce(t, text, "1 0 3 16 7 3", "16 7 3 1 0 3")
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := LoadMonsterRules(monsterRuleTestSource{MonsterRulePath: test.mutate(validMonsterRuleText())})
			if !errors.Is(err, ErrMonsterRuleSectionShape) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestRealScriptPVFMonsterRulesPreserveExactRuntimeRows(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		pvfPath = os.Getenv("DNF_WORLDMAP_REAL_PVF_SMOKE")
	}
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to run real monster-rule PVF smoke")
	}
	archive, err := platformpvf.Open(pvfPath)
	if err != nil {
		t.Fatal(err)
	}
	rules, err := LoadMonsterRules(archive)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot := rules.Snapshot(); snapshot != (MonsterRuleSnapshot{
		ProbabilityRows: 10, RarityRows: 4, PartyBonusRows: 5,
		DifficultyBonusRows: 5, MonsterTypeRows: 5, ItemReferences: 200, ConditionRows: 4,
	}) {
		t.Fatalf("real snapshot=%+v", snapshot)
	}
	if plan := rules.PlanSource(1); !plan.ProbabilityFound || !plan.ItemReferenceFound ||
		plan.Probability.Rates != ([5]int64{986, 60, 250, 0, 0}) ||
		plan.ItemReference != (MonsterItemReference{LookupKey: 1, ValueA: 0, ValueB: 3}) {
		t.Fatalf("real lookup 1=%+v", plan)
	}
	if plan := rules.PlanSource(86); !plan.ProbabilityFound || !plan.ItemReferenceFound ||
		plan.Probability.Rates != ([5]int64{750, 10, 135, 8, 5}) ||
		plan.ItemReference != (MonsterItemReference{LookupKey: 86, ValueA: 7, ValueB: 3}) {
		t.Fatalf("real lookup 86=%+v", plan)
	}
	if tables := rules.RawTables(); !reflect.DeepEqual(tables.DifficultyBonus[0], [5]float64{1, 1, 1, 1.4, 1.1}) ||
		!reflect.DeepEqual(tables.MonsterTypeBonus[0], [4]float64{1, 2, 3, 6.6}) {
		t.Fatalf("real raw modifiers changed: %+v", tables)
	}
}

func validMonsterRuleText() string {
	return `[drop prob count]
2
[drop prob]
1 15 986 60 250 0 0 16 23 750 30 210 17 15
[basis of rarity dicision]
700000 964900 990000 1000000 1000001 1000002 1000003
545500 899500 999500 1000000 1000001 1000002 1000003
500000 944900 999900 1000000 1000001 1000002 1000003
700000 944900 999000 1000000 1000001 1000002 1000003
[party member drop bonusrate]
1 1 1 1 1 1.3 1.6 2 1 1.2 1.4 1.6 1 1.2 1.4 1.6 1 1.2 1.4 1.6
[dungeon difficulty drop bonusrate]
1.00 1.00 1.00 1.40 1.10 1.00 1.40 1.60 2.20 2.00 1.00 1.40 1.60 2.20 2.00 1.00 1.40 1.60 2.20 2.00 1.00 1.40 1.60 2.20 2.00
[monster type drop bonusrate]
1.00 2.00 3.00 6.60 1.00 2.00 3.00 4.00 0.20 2.00 7.00 16.80 1.00 2.00 3.00 4.00 1.00 2.00 3.00 4.00
[item drop ref table]
1 0 3 16 7 3
[first boss/named mob hunting]
1 1
[condition rate]
95 5 93 7 90 10 85 15
[gold quantity]
1 2 4 6
[gold volume]
100 110
[item drop rarity control]
2 13 17 0 -5 3 13 17 0 -5
[/item drop rarity control]
`
}

func replaceOnce(t *testing.T, source, old, replacement string) string {
	t.Helper()
	if source == "" || old == "" || !containsOnce(source, old) {
		t.Fatalf("test replacement source does not contain exactly one %q", old)
	}
	return stringReplaceOnce(source, old, replacement)
}

func containsOnce(source, value string) bool {
	first := indexString(source, value)
	return first >= 0 && indexString(source[first+len(value):], value) < 0
}

func stringReplaceOnce(source, old, replacement string) string {
	index := indexString(source, old)
	return source[:index] + replacement + source[index+len(old):]
}

func indexString(source, value string) int {
	for index := 0; index+len(value) <= len(source); index++ {
		if source[index:index+len(value)] == value {
			return index
		}
	}
	return -1
}

type monsterRuleTestSource map[string]string

func (source monsterRuleTestSource) ReadText(path string) (string, error) {
	if text, found := source[path]; found {
		return text, nil
	}
	return "", errors.New("missing test PVF path: " + path)
}
