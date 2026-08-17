package dnfbridge

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"longheng.io/server/internal/modules/dnf/dungeoncmd"
	dnfmonster "longheng.io/server/internal/modules/dnf/monster"
	"longheng.io/server/internal/modules/dnf/progression"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

func TestCurrentDungeonMonsterRewardSourcesPreserveEvidenceAndRemainFailClosed(t *testing.T) {
	sources, err := progression.LoadMonsterExperienceSources(monsterRewardEvidenceTestSource())
	if err != nil {
		t.Fatal(err)
	}
	runtime := &runtimeDungeonState{
		Request: dungeoncmd.SelectDungeonRequest{DungeonID: 7146, Difficulty: 2},
		Dungeon: worldmap.Dungeon{
			ID: 7146,
			Metadata: worldmap.DungeonMetadata{
				BasisLevel:                worldmap.OptionalInt{Value: 2, Set: true},
				ExperienceIncreasingPoint: worldmap.OptionalNumber{Value: 1.75, Set: true},
			},
		},
		Character: dnfrepo.CharacterRecord{CharacterID: "99", AccountID: "account-1", Level: 10},
	}
	monster := runtimeDungeonMonster{
		ObjectKey: 501,
		Spawn: worldmap.MonsterSpawn{
			MonsterID: 77, Level: 1, AutoLevel: 1, Rank: "[boss]",
			RandomDropCount: 3, FixedDropCount: 1,
		},
		Definition: dnfmonster.Monster{ID: 77, Rank: "[boss]", Kind: "[beast]", Exp: 456},
	}
	pool := []dungeonMonsterDropPoolEntry{{ItemID: 1001, Weight: 70}, {ItemID: 1002, Weight: 30}}
	plan, err := currentDungeonMonsterRewardSources(runtime, monster, sources, pool)
	if err != nil {
		t.Fatal(err)
	}
	if plan.MonsterID != 77 || plan.SpawnRank != "[boss]" || plan.DefinitionRank != "[boss]" ||
		plan.DefinitionKind != "[beast]" || plan.WireActorType != 3 {
		t.Fatalf("monster evidence=%+v", plan)
	}
	experience := plan.Experience
	if experience.CharacterLevel != 10 || experience.MonsterLevel != 3 || experience.MonsterTable != 72 ||
		experience.DefinitionEXP != 456 || !reflect.DeepEqual(experience.MonsterGlobalRates, []float64{1}) ||
		experience.PartyRate != 1 || experience.StarterPartyRate != 1 || experience.DifficultyIndex != 2 ||
		experience.DifficultyRate != 2.5 || experience.DungeonID != 7146 || !experience.DungeonRateFound ||
		experience.DungeonRate != 2.3 || !experience.DungeonLocalRateSpecified || experience.DungeonLocalRate != 1.75 {
		t.Fatalf("experience evidence=%+v", experience)
	}
	if experience.MonsterMinusCharacter != (currentDungeonMonsterPenaltySourceCandidate{Key: -7, Rate: 0.8, Found: true}) ||
		experience.CharacterMinusMonster != (currentDungeonMonsterPenaltySourceCandidate{Key: 7, Rate: 0.9, Found: true}) {
		t.Fatalf("penalty candidates=%+v/%+v", experience.MonsterMinusCharacter, experience.CharacterMinusMonster)
	}
	wantExperienceBlockers := []string{
		"modifier_combination_unproved",
		"level_difference_direction_unproved",
		"actor_type_rate_mapping_unproved",
		"monster_definition_exp_semantics_unproved",
		"dungeon_local_experience_rate_composition_unproved",
		"op38_op37_natural_order_unproved",
	}
	if experience.AwardReady || plan.FullAwardReady || !reflect.DeepEqual(experience.Blockers, wantExperienceBlockers) {
		t.Fatalf("unproved experience was enabled: %+v", plan)
	}
	if plan.Drop.RandomDropCount != 3 || plan.Drop.FixedDropCount != 1 || plan.Drop.ExplicitWeight != 100 ||
		!plan.Drop.ExplicitPoolCompatibilityReady || plan.Drop.GenericDropAwardReady ||
		!reflect.DeepEqual(plan.Drop.ExplicitPool, pool) {
		t.Fatalf("drop evidence=%+v", plan.Drop)
	}
	pool[0].ItemID = 9999
	if plan.Drop.ExplicitPool[0].ItemID != 1001 {
		t.Fatal("source plan retained caller-owned pool slice")
	}
}

func TestCurrentDungeonMonsterRewardSourcesRejectInvalidOwnerAndKeepUnprovedDropBranchesClosed(t *testing.T) {
	sources, err := progression.LoadMonsterExperienceSources(monsterRewardEvidenceTestSource())
	if err != nil {
		t.Fatal(err)
	}
	runtime := &runtimeDungeonState{
		Request:   dungeoncmd.SelectDungeonRequest{DungeonID: 3, Difficulty: 0},
		Dungeon:   worldmap.Dungeon{ID: 3, Metadata: worldmap.DungeonMetadata{BasisLevel: worldmap.OptionalInt{Value: 2, Set: true}}},
		Character: dnfrepo.CharacterRecord{CharacterID: "99", Level: 2},
	}
	monster := runtimeDungeonMonster{Spawn: worldmap.MonsterSpawn{MonsterID: 77, AutoLevel: 2, Rank: "[normal]", RandomDropCount: 2}}
	for name, testCase := range map[string]struct {
		runtime *runtimeDungeonState
		monster runtimeDungeonMonster
		sources *progression.MonsterExperienceSources
	}{
		"nil runtime":       {runtime: nil, monster: monster, sources: sources},
		"nil source":        {runtime: runtime, monster: monster, sources: nil},
		"missing character": {runtime: &runtimeDungeonState{Dungeon: runtime.Dungeon}, monster: monster, sources: sources},
		"missing monster":   {runtime: runtime, monster: runtimeDungeonMonster{}, sources: sources},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := currentDungeonMonsterRewardSources(testCase.runtime, testCase.monster, testCase.sources, nil)
			if !errors.Is(err, errCurrentDungeonMonsterRewardSourceUnavailable) {
				t.Fatalf("error=%v", err)
			}
		})
	}
	plan, err := currentDungeonMonsterRewardSources(runtime, monster, sources, []dungeonMonsterDropPoolEntry{{ItemID: 1199, Weight: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Drop.ExplicitPoolCompatibilityReady || plan.Drop.GenericDropAwardReady || plan.FullAwardReady ||
		!containsString(plan.Drop.Blockers, "coin_gold_drop_branch_unproved") {
		t.Fatalf("unproved coin/generic drop was enabled: %+v", plan.Drop)
	}
	if plan.Experience.DungeonRateFound || plan.Experience.DungeonRate != 0 || plan.Experience.AwardReady {
		t.Fatalf("missing dungeon rate received fallback: %+v", plan.Experience)
	}
}

type monsterRewardEvidenceSource map[string]string

func (source monsterRewardEvidenceSource) ReadText(path string) (string, error) {
	value, found := source[path]
	if !found {
		return "", fmt.Errorf("missing %s", path)
	}
	return value, nil
}

func monsterRewardEvidenceTestSource() monsterRewardEvidenceSource {
	return monsterRewardEvidenceSource{
		progression.MonsterExperienceTablePath: "1 30 2 60 3 72\n",
		progression.ServerParameterPath: `[monster exp bonusrate]
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
7146 2.30
[/experience increasing point]
`,
		progression.WorldMapExperiencePenaltyPath: `[penalty table info]
-16 0.30 -15 0.50 -14 0.50 -13 0.50 -12 0.70 -11 0.70 -10 0.70 -9 0.80 -8 0.80 -7 0.80 -6 0.90 -5 0.90 -4 0.90 -3 1.00 -2 1.00 -1 1.00 0 1.00 1 1.00 2 1.00 3 1.00 4 1.00 5 1.00 6 0.90 7 0.90 8 0.90 9 0.80 10 0.80 11 0.80 12 0.70 13 0.70 14 0.70 15 0.50 16 0.50 17 0.50 18 0.30 19 0.30 20 0.30
[/penalty table info]
`,
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
