package progression

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestMonsterExperienceSourcesPreservePVFValuesWithoutInventingAwardFormula(t *testing.T) {
	sources, err := LoadMonsterExperienceSources(monsterExperienceTestSource())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot := sources.Snapshot(); snapshot != (MonsterExperienceSourceSnapshot{
		MonsterLevels: 3, MonsterGlobalRates: 1, PartyRates: 4,
		StarterPartyRates: 4, DifficultyRates: 5, DungeonIncreasingRates: 2,
		PenaltyRows: 37, ClearGlobalRates: 1, ClearRankRates: 5,
	}) {
		t.Fatalf("snapshot=%+v", snapshot)
	}
	if value, found := sources.MonsterTableValue(2); !found || value != 60 {
		t.Fatalf("level 2=%d,%t", value, found)
	}
	if _, found := sources.MonsterTableValue(4); found {
		t.Fatal("out-of-table monster level fabricated")
	}
	if value, found := sources.DungeonIncreasingRate(7146); !found || value != 2.3 {
		t.Fatalf("dungeon 7146=%v,%t", value, found)
	}
	if _, found := sources.DungeonIncreasingRate(3); found {
		t.Fatal("dungeon 3 rate fabricated")
	}

	modifiers := sources.RawModifiers()
	if !reflect.DeepEqual(modifiers.MonsterGlobalRates, []float64{1}) ||
		modifiers.PartyRates != ([4]float64{1, 2, 3, 4}) ||
		modifiers.StarterPartyRates != ([4]float64{1, 2, 3, 4}) ||
		modifiers.DifficultyRates != ([5]float64{1.3, 2, 2.5, 3, 4}) ||
		!reflect.DeepEqual(modifiers.ClearGlobalRates, []float64{1}) ||
		modifiers.ClearRankRates != ([5]float64{0.05, 0.1, 0.12, 0.15, 0.2}) {
		t.Fatalf("raw modifiers=%+v", modifiers)
	}
	if rate, found := sources.PenaltyRateBySourceKey(-16); !found || rate != 0.3 {
		t.Fatalf("penalty -16=%v,%t", rate, found)
	}
	if rate, found := sources.PenaltyRateBySourceKey(5); !found || rate != 1 {
		t.Fatalf("penalty 5=%v,%t", rate, found)
	}
	if _, found := sources.PenaltyRateBySourceKey(21); found {
		t.Fatal("penalty key outside PVF table fabricated")
	}

	plan, err := sources.PlanSource(1, 1, 0, 7146)
	if err != nil {
		t.Fatal(err)
	}
	if plan.MonsterTableValue != 30 || plan.PartyRate != 1 || plan.StarterPartyRate != 1 ||
		plan.DifficultyRate != 1.3 || !plan.DungeonRateFound || plan.DungeonIncreasingRate != 2.3 {
		t.Fatalf("plan source values=%+v", plan)
	}
	if plan.AwardReady || !reflect.DeepEqual(plan.Blockers, []MonsterExperienceBlocker{
		MonsterExperienceCombinationUnproved,
		MonsterExperiencePenaltyDirectionUnproved,
		MonsterExperienceActorTypeUnproved,
	}) {
		t.Fatalf("unproved award was enabled: %+v", plan)
	}

	missingDungeon, err := sources.PlanSource(1, 1, 0, 3)
	if err != nil {
		t.Fatal(err)
	}
	if missingDungeon.DungeonRateFound || missingDungeon.DungeonIncreasingRate != 0 || missingDungeon.AwardReady {
		t.Fatalf("missing dungeon modifier got a fallback: %+v", missingDungeon)
	}
	for _, request := range []struct {
		level      int
		party      int
		difficulty int
	}{
		{level: 0, party: 1, difficulty: 0},
		{level: 1, party: 0, difficulty: 0},
		{level: 1, party: 1, difficulty: 5},
	} {
		if _, err := sources.PlanSource(request.level, request.party, request.difficulty, 3); !errors.Is(err, ErrMonsterExperienceLookup) {
			t.Fatalf("request=%+v error=%v", request, err)
		}
	}
}

func TestMonsterExperienceSourcesRejectMalformedOrPartialPVF(t *testing.T) {
	if _, err := LoadMonsterExperienceSources(nil); !errors.Is(err, ErrMonsterExperienceSourceRequired) {
		t.Fatalf("nil source error=%v", err)
	}
	tests := []struct {
		name   string
		mutate func(progressionMemorySource)
	}{
		{
			name: "odd monster table",
			mutate: func(source progressionMemorySource) {
				source[MonsterExperienceTablePath] = "1 30 2"
			},
		},
		{
			name: "noncontiguous monster level",
			mutate: func(source progressionMemorySource) {
				source[MonsterExperienceTablePath] = "1 30 3 72"
			},
		},
		{
			name: "nonnumeric monster value",
			mutate: func(source progressionMemorySource) {
				source[MonsterExperienceTablePath] = "1 30 2 missing"
			},
		},
		{
			name: "missing global section",
			mutate: func(source progressionMemorySource) {
				source[ServerParameterPath] = strings.Replace(source[ServerParameterPath], "[monster exp bonusrate]\n1.00\n", "", 1)
			},
		},
		{
			name: "short party vector",
			mutate: func(source progressionMemorySource) {
				source[ServerParameterPath] = strings.Replace(source[ServerParameterPath], "[party user number exp bonusrate]\n1 2 3 4", "[party user number exp bonusrate]\n1 2 3", 1)
			},
		},
		{
			name: "short difficulty vector",
			mutate: func(source progressionMemorySource) {
				source[ServerParameterPath] = strings.Replace(source[ServerParameterPath], "1.30 2.00 2.50 3.00 4.00", "1.30 2.00", 1)
			},
		},
		{
			name: "missing clear rate",
			mutate: func(source progressionMemorySource) {
				source[ServerParameterPath] = strings.Replace(source[ServerParameterPath], "[clear exp bonusrate]\n1.00\n", "", 1)
			},
		},
		{
			name: "short clear rank vector",
			mutate: func(source progressionMemorySource) {
				source[ServerParameterPath] = strings.Replace(source[ServerParameterPath], "0.05 0.10 0.12 0.15 0.20", "0.05 0.10", 1)
			},
		},
		{
			name: "duplicate dungeon modifier",
			mutate: func(source progressionMemorySource) {
				source[ServerParameterPath] = strings.Replace(source[ServerParameterPath], "7146 2.30 4907 22.00", "7146 2.30 7146 22.00", 1)
			},
		},
		{
			name: "nonnumeric dungeon rate",
			mutate: func(source progressionMemorySource) {
				source[ServerParameterPath] = strings.Replace(source[ServerParameterPath], "7146 2.30", "7146 unknown", 1)
			},
		},
		{
			name: "short penalty table",
			mutate: func(source progressionMemorySource) {
				source[WorldMapExperiencePenaltyPath] = "[penalty table info]\n-16 0.30\n[/penalty table info]\n"
			},
		},
		{
			name: "noncontiguous penalty key",
			mutate: func(source progressionMemorySource) {
				source[WorldMapExperiencePenaltyPath] = strings.Replace(source[WorldMapExperiencePenaltyPath], "-15 0.50", "-14 0.50", 1)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := monsterExperienceTestSource()
			test.mutate(source)
			_, err := LoadMonsterExperienceSources(source)
			if !errors.Is(err, ErrMonsterExperienceSectionShape) && !errors.Is(err, ErrMonsterExperienceSectionMissing) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestRealScriptPVFMonsterExperienceSourcesPreserveExactRuntimeRows(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		pvfPath = os.Getenv("DNF_WORLDMAP_REAL_PVF_SMOKE")
	}
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to verify real monster experience PVF sources")
	}
	archive, err := platformpvf.Open(pvfPath)
	if err != nil {
		t.Fatal(err)
	}
	sources, err := LoadMonsterExperienceSources(archive)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot := sources.Snapshot(); snapshot != (MonsterExperienceSourceSnapshot{
		MonsterLevels: 300, MonsterGlobalRates: 1, PartyRates: 4,
		StarterPartyRates: 4, DifficultyRates: 5, DungeonIncreasingRates: 11,
		PenaltyRows: 37, ClearGlobalRates: 1, ClearRankRates: 5,
	}) {
		t.Fatalf("real snapshot=%+v", snapshot)
	}
	for level, want := range map[int]uint32{1: 30, 2: 60, 3: 72, 100: 9517, 300: 64906} {
		if got, found := sources.MonsterTableValue(level); !found || got != want {
			t.Fatalf("real monster table level=%d value=%d,%t want=%d", level, got, found, want)
		}
	}
	modifiers := sources.RawModifiers()
	if !reflect.DeepEqual(modifiers.MonsterGlobalRates, []float64{1}) ||
		modifiers.PartyRates != ([4]float64{1, 2, 3, 4}) ||
		modifiers.StarterPartyRates != ([4]float64{1, 2, 3, 4}) ||
		modifiers.DifficultyRates != ([5]float64{1.3, 2, 2.5, 3, 4}) ||
		!reflect.DeepEqual(modifiers.ClearGlobalRates, []float64{1}) ||
		modifiers.ClearRankRates != ([5]float64{0.05, 0.1, 0.12, 0.15, 0.2}) {
		t.Fatalf("real raw modifiers=%+v", modifiers)
	}
	for key, want := range map[int]float64{-16: 0.3, -7: 0.8, -3: 1, 5: 1, 6: 0.9, 20: 0.3} {
		if got, found := sources.PenaltyRateBySourceKey(key); !found || got != want {
			t.Fatalf("real penalty key=%d value=%v,%t want=%v", key, got, found, want)
		}
	}
	for dungeonID, want := range map[int64]float64{7146: 2.3, 7155: 2.9, 4907: 22} {
		if got, found := sources.DungeonIncreasingRate(dungeonID); !found || got != want {
			t.Fatalf("real dungeon=%d value=%v,%t want=%v", dungeonID, got, found, want)
		}
	}
	if plan, err := sources.PlanSource(1, 1, 0, 3); err != nil || plan.AwardReady || plan.DungeonRateFound {
		t.Fatalf("real dungeon 3 source plan=%+v err=%v", plan, err)
	}
}

func monsterExperienceTestSource() progressionMemorySource {
	return progressionMemorySource{
		MonsterExperienceTablePath: "1 30 2 60 3 72\n",
		ServerParameterPath: `[monster exp bonusrate]
1.00
[party user number exp bonusrate]
1 2 3 4
[party user number exp bonusrate starter server]
1 2 3 4
[dungeon difficulty exp bonusrate]
1.30 2.00 2.50 3.00 4.00
[clear exp bonusrate]
1.00
[clear rank exp bonusrate]
0.05 0.10 0.12 0.15 0.20
[experience increasing point]
7146 2.30 4907 22.00
[/experience increasing point]
`,
		WorldMapExperiencePenaltyPath: `[penalty table info]
-16 0.30 -15 0.50 -14 0.50 -13 0.50 -12 0.70 -11 0.70 -10 0.70 -9 0.80 -8 0.80 -7 0.80 -6 0.90 -5 0.90 -4 0.90 -3 1.00 -2 1.00 -1 1.00 0 1.00 1 1.00 2 1.00 3 1.00 4 1.00 5 1.00 6 0.90 7 0.90 8 0.90 9 0.80 10 0.80 11 0.80 12 0.70 13 0.70 14 0.70 15 0.50 16 0.50 17 0.50 18 0.30 19 0.30 20 0.30
[/penalty table info]
`,
	}
}
