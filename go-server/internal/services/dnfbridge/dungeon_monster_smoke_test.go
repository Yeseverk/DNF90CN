package dnfbridge

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"longheng.io/server/internal/modules/dnf/worldmap"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestRealScriptPVFDungeonMonsterAlignment(t *testing.T) {
	pvfPath := os.Getenv("DNF_WORLDMAP_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNF_WORLDMAP_REAL_PVF_SMOKE to run the real Script.pvf monster alignment smoke test")
	}
	archive, err := platformpvf.Open(pvfPath)
	if err != nil {
		t.Fatalf("open real Script.pvf: %v", err)
	}
	worldMapTable, err := worldmap.LoadSource(context.Background(), archive, worldmap.Options{})
	if err != nil {
		t.Fatalf("load real worldmap: %v", err)
	}
	monsterTable, err := newPVFDungeonMonsterCatalog(archive)
	if err != nil {
		t.Fatalf("load real monster catalog: %v", err)
	}
	if monsterTable.Count() == 0 {
		t.Fatal("real monster catalog is empty")
	}
	aiCharacterTable, err := newPVFDungeonAICharacterCatalog(archive)
	if err != nil {
		t.Fatalf("load real AI character catalog: %v", err)
	}
	if aiCharacterTable.Count() == 0 {
		t.Fatal("real AI character catalog is empty")
	}
	catalogIDs := make([]int64, 0, len(monsterTable.paths))
	for id := range monsterTable.paths {
		catalogIDs = append(catalogIDs, id)
	}
	sort.Slice(catalogIDs, func(i, j int) bool { return catalogIDs[i] < catalogIDs[j] })
	parseFailures := make([]string, 0)
	for _, id := range catalogIDs {
		if _, found, findErr := monsterTable.Find(id); findErr != nil || !found {
			if len(parseFailures) < 20 {
				if findErr != nil {
					parseFailures = append(parseFailures, findErr.Error())
				} else {
					parseFailures = append(parseFailures, "listed monster was not found")
				}
			}
		}
	}
	if len(parseFailures) > 0 {
		t.Fatalf("real monster catalog parse failures: %v", parseFailures)
	}
	aiCatalogIDs := make([]int64, 0, len(aiCharacterTable.paths))
	for id := range aiCharacterTable.paths {
		aiCatalogIDs = append(aiCatalogIDs, id)
	}
	sort.Slice(aiCatalogIDs, func(i, j int) bool { return aiCatalogIDs[i] < aiCatalogIDs[j] })
	aiParseFailures := make([]string, 0)
	for _, id := range aiCatalogIDs {
		if _, found, findErr := aiCharacterTable.Find(id); findErr != nil || !found {
			if len(aiParseFailures) < 20 {
				if findErr != nil {
					aiParseFailures = append(aiParseFailures, findErr.Error())
				} else {
					aiParseFailures = append(aiParseFailures, fmt.Sprintf("listed AI character %d was not found", id))
				}
			}
		}
	}
	if len(aiParseFailures) > 0 {
		t.Fatalf("real AI character catalog parse failures: %v", aiParseFailures)
	}

	spawnReferences := 0
	resolvedReferences := 0
	mapsWithMonsters := 0
	missing := make(map[int64]int)
	ranks := make(map[string]int)
	levelModes := make(map[string]int)
	specialPassiveObjects := 0
	specialMonsterTemplates := 0
	aiCharacters := 0
	aiResolved := 0
	aiMissing := make(map[int64]int)
	aiFactions := make(map[string]int)
	aiTypes := make(map[string]int)
	specialSpawnKinds := make(map[string]int)
	invalidSpecialObjectIDs := 0
	for _, mapValue := range worldMapTable.Maps() {
		specialPassiveObjects += len(mapValue.SpecialPassiveObjects)
		aiCharacters += len(mapValue.AICharacters)
		for _, object := range mapValue.SpecialPassiveObjects {
			if object.ObjectID <= 0 {
				invalidSpecialObjectIDs++
			}
			for _, spawn := range object.Spawns {
				kind := strings.ToLower(strings.Trim(spawn.Kind, "[] \t\r\n"))
				specialSpawnKinds[kind]++
				if kind == "monster" {
					specialMonsterTemplates++
				}
			}
		}
		for _, actor := range mapValue.AICharacters {
			aiFactions[strings.ToLower(strings.Trim(actor.Faction, "[] \t\r\n"))]++
			aiTypes[strings.ToLower(strings.Trim(actor.AIType, "[] \t\r\n"))]++
			if _, found, findErr := aiCharacterTable.Find(actor.Code); findErr == nil && found {
				aiResolved++
			} else {
				aiMissing[actor.Code]++
			}
		}
		if len(mapValue.Monsters) == 0 {
			continue
		}
		mapsWithMonsters++
		for _, spawn := range mapValue.Monsters {
			spawnReferences++
			ranks[strings.ToLower(strings.Trim(spawn.Rank, "[] \t\r\n"))]++
			levelModes[fmt.Sprintf("lv=%d auto=%d", spawn.Level, spawn.AutoLevel)]++
			if _, ok, findErr := monsterTable.Find(spawn.MonsterID); findErr == nil && ok {
				resolvedReferences++
			} else {
				missing[spawn.MonsterID]++
			}
		}
	}
	if spawnReferences == 0 {
		t.Fatal("real maps contain no parsed monster spawns")
	}
	missingIDs := make([]int64, 0, len(missing))
	for id := range missing {
		missingIDs = append(missingIDs, id)
	}
	sort.Slice(missingIDs, func(i, j int) bool { return missingIDs[i] < missingIDs[j] })
	sample := missingIDs
	if len(sample) > 20 {
		sample = sample[:20]
	}
	t.Logf(
		"real monster alignment: catalog=%d maps_with_monsters=%d spawn_refs=%d resolved=%d missing_refs=%d missing_ids=%d sample=%v ranks=%v level_modes_top20=%v special_objects=%d special_monster_templates=%d special_spawn_kinds=%v invalid_special_object_ids=%d ai_catalog=%d ai_characters=%d ai_resolved=%d ai_missing=%v ai_factions=%v ai_types=%v",
		monsterTable.Count(),
		mapsWithMonsters,
		spawnReferences,
		resolvedReferences,
		spawnReferences-resolvedReferences,
		len(missingIDs),
		sample,
		topStringCounts(ranks, 20),
		topStringCounts(levelModes, 20),
		specialPassiveObjects,
		specialMonsterTemplates,
		topStringCounts(specialSpawnKinds, 20),
		invalidSpecialObjectIDs,
		aiCharacterTable.Count(),
		aiCharacters,
		aiResolved,
		topInt64Counts(aiMissing, 20),
		topStringCounts(aiFactions, 20),
		topStringCounts(aiTypes, 20),
	)
}

type stringCount struct {
	Value string
	Count int
}

func topStringCounts(values map[string]int, limit int) []stringCount {
	result := make([]stringCount, 0, len(values))
	for value, count := range values {
		result = append(result, stringCount{Value: value, Count: count})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Count != result[j].Count {
			return result[i].Count > result[j].Count
		}
		return result[i].Value < result[j].Value
	})
	if len(result) > limit {
		result = result[:limit]
	}
	return result
}

type int64Count struct {
	Value int64
	Count int
}

func topInt64Counts(values map[int64]int, limit int) []int64Count {
	result := make([]int64Count, 0, len(values))
	for value, count := range values {
		result = append(result, int64Count{Value: value, Count: count})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Count != result[j].Count {
			return result[i].Count > result[j].Count
		}
		return result[i].Value < result[j].Value
	})
	if len(result) > limit {
		result = result[:limit]
	}
	return result
}
