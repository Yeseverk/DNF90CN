package dnfbridge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"testing"

	"longheng.io/server/internal/modules/dnf/dungeoncmd"
	"longheng.io/server/internal/modules/dnf/worldmap"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

const (
	realDungeonReadinessTotal     = 2654
	realDungeonReadinessSupported = 1780
	realDungeonReadinessBlocked   = 874
)

var realDungeonReadinessBlockedCategories = map[string]struct{}{
	"AI_definition_malformed":    {},
	"AI_definition_missing":      {},
	"AI_faction":                 {},
	"AI_type":                    {},
	"actor_code":                 {},
	"actor_count":                {},
	"actor_level":                {},
	"actor_object_key":           {},
	"attach":                     {},
	"boss_missing":               {},
	"boss_range":                 {},
	"coordinate_range":           {},
	"extra_count":                {},
	"group_count":                {},
	"layout":                     {},
	"map_id_range":               {},
	"maze_index_range":           {},
	"monster_definition_missing": {},
	"monster_id":                 {},
	"opaque_hostile":             {},
	"randomized_object":          {},
	"run":                        {},
	"scene":                      {},
	"scene_mismatch":             {},
	"session":                    {},
	"special_object":             {},
	"special_spawn_kind":         {},
}

var realDungeonReadinessExpectedBlockers = map[string]int{
	"layout":                826,
	"randomized_object":     36,
	"boss_missing":          11,
	"AI_definition_missing": 1,
}

func TestRealScriptPVFDungeonEntryReadiness(t *testing.T) {
	pvfPath := os.Getenv("DNF_WORLDMAP_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNF_WORLDMAP_REAL_PVF_SMOKE to run the real dungeon entry readiness smoke test")
	}
	archive, err := platformpvf.Open(pvfPath)
	if err != nil {
		t.Fatal(err)
	}
	table, err := worldmap.LoadSource(context.Background(), archive, worldmap.Options{})
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := worldmap.NewResolver(table)
	if err != nil {
		t.Fatal(err)
	}
	monsterCatalog, err := newPVFDungeonMonsterCatalog(archive)
	if err != nil {
		t.Fatal(err)
	}
	aiCatalog, err := newPVFDungeonAICharacterCatalog(archive)
	if err != nil {
		t.Fatal(err)
	}

	total := 0
	ready := 0
	failures := make(map[string]int)
	samples := make(map[string][]string)
	readySamples := make([]string, 0, 10)
	for _, dungeon := range table.Dungeons() {
		for mazeIndex := range dungeon.Mazes {
			total++
			topology, buildErr := worldmap.BuildDungeonLayout(
				resolver,
				dungeon.ID,
				mazeIndex,
				func(choice worldmap.DungeonMapChoice) (int64, error) {
					if len(choice.Candidates) == 0 {
						return 0, worldmap.ErrMapChoiceRequired
					}
					return choice.Candidates[0].ID, nil
				},
			)
			if buildErr != nil {
				recordDungeonReadinessFailure(failures, samples, "layout", dungeon.ID, mazeIndex, buildErr)
				continue
			}
			if len(topology.Bosses) == 0 {
				recordDungeonReadinessFailure(failures, samples, "boss_missing", dungeon.ID, mazeIndex, errDungeonBossCoordinateRequired)
				continue
			}
			run, runErr := worldmap.NewDungeonRun(topology)
			if runErr != nil {
				recordDungeonReadinessFailure(failures, samples, "run", dungeon.ID, mazeIndex, runErr)
				continue
			}
			session, sessionErr := worldmap.NewDungeonSession(run)
			if sessionErr != nil {
				recordDungeonReadinessFailure(failures, samples, "session", dungeon.ID, mazeIndex, sessionErr)
				continue
			}
			scene, ok := session.Scene()
			if !ok {
				recordDungeonReadinessFailure(failures, samples, "scene", dungeon.ID, mazeIndex, worldmap.ErrDungeonRunRequired)
				continue
			}
			room, nextObjectKey, roomErr := newRuntimeDungeonRoom(scene, monsterCatalog, firstDungeonMonsterObjectKey)
			if roomErr != nil {
				recordDungeonReadinessFailure(failures, samples, classifyDungeonReadinessError(roomErr), dungeon.ID, mazeIndex, roomErr)
				continue
			}
			var roomAICatalog *pvfDungeonAICharacterCatalog
			if len(scene.AICharacters) != 0 {
				roomAICatalog = aiCatalog
			}
			plan, planErr := planRuntimeDungeonExtendedActors(
				scene,
				monsterCatalog,
				roomAICatalog,
				dungeon.Metadata.BasisLevel,
				nextObjectKey,
			)
			if planErr != nil {
				recordDungeonReadinessFailure(failures, samples, classifyDungeonReadinessError(planErr), dungeon.ID, mazeIndex, planErr)
				continue
			}
			if attachErr := room.AttachExtendedActors(plan); attachErr != nil {
				recordDungeonReadinessFailure(failures, samples, "attach", dungeon.ID, mazeIndex, attachErr)
				continue
			}
			runtime := &runtimeDungeonState{
				Request:        dungeoncmd.SelectDungeonRequest{DungeonID: uint32(dungeon.ID)},
				Dungeon:        dungeon,
				MazeIndex:      mazeIndex,
				Session:        session,
				Room:           room,
				NextObjectKey:  plan.NextObjectKey,
				BossCoordinate: topology.Bosses[0],
				BossSet:        true,
				Seed:           1,
			}
			if _, packetErr := buildCurrentDungeonEntryPackets(runtime, scene); packetErr != nil {
				recordDungeonReadinessFailure(failures, samples, classifyDungeonReadinessError(packetErr), dungeon.ID, mazeIndex, packetErr)
				continue
			}
			ready++
			if len(readySamples) < 10 {
				readySamples = append(readySamples, fmt.Sprintf("%d/%d map=%d actors=%d", dungeon.ID, mazeIndex, scene.Map.Map.ID, len(scene.Monsters)+len(plan.Actors)))
			}
		}
	}
	if baselineErr := validateRealDungeonReadinessBaseline(total, ready, failures); baselineErr != nil {
		t.Fatalf("real dungeon readiness baseline regression: %v; blockers=%v samples=%v",
			baselineErr,
			sortedDungeonReadinessCounts(failures),
			samples,
		)
	}
	t.Logf("real dungeon entry supported baseline (not full dungeon support): total=%d supported=%d blocked=%d blockers=%v supported_samples=%v blocked_samples=%v",
		total,
		ready,
		total-ready,
		sortedDungeonReadinessCounts(failures),
		readySamples,
		samples,
	)
}

func classifyDungeonReadinessError(err error) string {
	switch {
	case errors.Is(err, errDungeonRandomizedObjectOwner):
		return "randomized_object"
	case errors.Is(err, errDungeonAICharacterDefinitionMiss):
		return "AI_definition_missing"
	case errors.Is(err, errDungeonAICharacterMinimumInfo):
		return "AI_definition_malformed"
	case errors.Is(err, errDungeonAICharacterType):
		return "AI_type"
	case errors.Is(err, errDungeonAICharacterFaction):
		return "AI_faction"
	case errors.Is(err, errDungeonMonsterDefinitionMiss):
		return "monster_definition_missing"
	case errors.Is(err, errDungeonMonsterIDInvalid):
		return "monster_id"
	case errors.Is(err, errDungeonStartMapMonsterRank):
		return "monster_rank"
	case errors.Is(err, errDungeonSpecialSpawnKind):
		return "special_spawn_kind"
	case errors.Is(err, errDungeonStartMapActorCount):
		return "actor_count"
	case errors.Is(err, errDungeonStartMapMonsterLevel), errors.Is(err, errDungeonSpecialSpawnLevel):
		return "actor_level"
	case errors.Is(err, errDungeonStartMapActorCodeRange), errors.Is(err, errDungeonExtendedActorCodeRange):
		return "actor_code"
	case errors.Is(err, errDungeonStartMapObjectKeyRange),
		errors.Is(err, errDungeonMonsterObjectKeyRange),
		errors.Is(err, errDungeonExtendedActorObjectKeyRange),
		errors.Is(err, errDungeonExtendedActorIndexRange):
		return "actor_object_key"
	case errors.Is(err, errDungeonBossCoordinateRange):
		return "boss_range"
	case errors.Is(err, errDungeonStartMapCoordinateRange):
		return "coordinate_range"
	case errors.Is(err, errDungeonStartMapMapIDRange):
		return "map_id_range"
	case errors.Is(err, errDungeonMazeIndexRange):
		return "maze_index_range"
	case errors.Is(err, errDungeonStartMapOpaqueHostile):
		return "opaque_hostile"
	case errors.Is(err, errDungeonStartMapSpecialObject):
		return "special_object"
	case errors.Is(err, errDungeonStartMapSceneMismatch):
		return "scene_mismatch"
	case errors.Is(err, errDungeonStartMapExtraCount), errors.Is(err, errDungeonInfoOpaqueCount):
		return "extra_count"
	case errors.Is(err, errDungeonStartMapGroupCount), errors.Is(err, errDungeonInfoPairGroupCount):
		return "group_count"
	default:
		return "other"
	}
}

func validateRealDungeonReadinessBaseline(total int, supported int, failures map[string]int) error {
	blocked := total - supported
	if total != realDungeonReadinessTotal || supported != realDungeonReadinessSupported || blocked != realDungeonReadinessBlocked {
		return fmt.Errorf(
			"got total=%d supported=%d blocked=%d, want total=%d supported=%d blocked=%d",
			total,
			supported,
			blocked,
			realDungeonReadinessTotal,
			realDungeonReadinessSupported,
			realDungeonReadinessBlocked,
		)
	}
	classified := 0
	for category, count := range failures {
		if count <= 0 {
			return fmt.Errorf("blocked category %q has invalid count %d", category, count)
		}
		if _, ok := realDungeonReadinessBlockedCategories[category]; !ok {
			return fmt.Errorf("blocked category %q is not explicitly classified", category)
		}
		classified += count
	}
	if classified != blocked {
		return fmt.Errorf("classified blocked=%d, want %d", classified, blocked)
	}
	if len(failures) != len(realDungeonReadinessExpectedBlockers) {
		return fmt.Errorf("blocked category count=%d, want %d", len(failures), len(realDungeonReadinessExpectedBlockers))
	}
	for category, want := range realDungeonReadinessExpectedBlockers {
		if got := failures[category]; got != want {
			return fmt.Errorf("blocked category %q=%d, want %d", category, got, want)
		}
	}
	return nil
}

func TestRealDungeonReadinessBaselineGate(t *testing.T) {
	tests := []struct {
		name      string
		total     int
		supported int
		failures  map[string]int
		wantErr   bool
	}{
		{
			name:      "audited supported and blocked baseline",
			total:     realDungeonReadinessTotal,
			supported: realDungeonReadinessSupported,
			failures:  cloneDungeonReadinessCounts(realDungeonReadinessExpectedBlockers),
		},
		{
			name:      "supported regression",
			total:     realDungeonReadinessTotal,
			supported: realDungeonReadinessSupported - 1,
			failures: func() map[string]int {
				failures := cloneDungeonReadinessCounts(realDungeonReadinessExpectedBlockers)
				failures["layout"]++
				return failures
			}(),
			wantErr: true,
		},
		{
			name:      "classification count mismatch",
			total:     realDungeonReadinessTotal,
			supported: realDungeonReadinessSupported,
			failures: func() map[string]int {
				failures := cloneDungeonReadinessCounts(realDungeonReadinessExpectedBlockers)
				failures["layout"]--
				return failures
			}(),
			wantErr: true,
		},
		{
			name:      "unclassified blocker",
			total:     realDungeonReadinessTotal,
			supported: realDungeonReadinessSupported,
			failures:  map[string]int{"new_unknown_owner": realDungeonReadinessBlocked},
			wantErr:   true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateRealDungeonReadinessBaseline(test.total, test.supported, test.failures)
			if (err != nil) != test.wantErr {
				t.Fatalf("validate error = %v, wantErr=%t", err, test.wantErr)
			}
		})
	}
}

func cloneDungeonReadinessCounts(values map[string]int) map[string]int {
	cloned := make(map[string]int, len(values))
	for category, count := range values {
		cloned[category] = count
	}
	return cloned
}

func recordDungeonReadinessFailure(
	counts map[string]int,
	samples map[string][]string,
	category string,
	dungeonID int64,
	mazeIndex int,
	err error,
) {
	counts[category]++
	if len(samples[category]) < 3 {
		samples[category] = append(samples[category], fmt.Sprintf("%d/%d: %v", dungeonID, mazeIndex, err))
	}
}

type dungeonReadinessCount struct {
	Category string
	Count    int
}

func sortedDungeonReadinessCounts(values map[string]int) []dungeonReadinessCount {
	result := make([]dungeonReadinessCount, 0, len(values))
	for category, count := range values {
		result = append(result, dungeonReadinessCount{Category: category, Count: count})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Count != result[j].Count {
			return result[i].Count > result[j].Count
		}
		return result[i].Category < result[j].Category
	})
	return result
}
