package worldmap

import (
	"context"
	"errors"
	"testing"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

func TestLoadBuildsReadOnlyMapAndAreaIndexes(t *testing.T) {
	index, err := dnfpvf.Build(context.Background(), textSource{
		DefaultMapList:      "10 `map/town/test.map`\n",
		"map/town/test.map": "[map name]\n`Test Town`\n[npc]\n100 `[left]` 1 2 3\n",
		DefaultDungeonList:  "30 `dungeon/test.dgn`\n",
		"dungeon/test.dgn":  "[name]\n`Test Dungeon`\n[maze info]\n[size]\n1 1\n[map specification]\n`map` 0 0 10\n[start map]\n0 0\n[boss map]\n0 0\n",
		DefaultWorldMapList: "20 `worldmap/test.wdm`\n",
		"worldmap/test.wdm": "[name]\n`Test Area`\n[dungeon]\n700 -1\n[/dungeon]\n",
	}, dnfpvf.BuildOptions{Lists: []string{DefaultMapList, DefaultDungeonList, DefaultWorldMapList}})
	if err != nil {
		t.Fatalf("build pvf index: %v", err)
	}
	table, err := Load(context.Background(), index, Options{})
	if err != nil {
		t.Fatalf("load worldmap: %v", err)
	}
	if snapshot := table.Snapshot(); snapshot.Maps != 1 || snapshot.Areas != 1 || snapshot.Dungeons != 1 || snapshot.Mazes != 1 || snapshot.DungeonRefs != 1 {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
	parsedMap, ok := table.FindMap(10)
	if !ok || parsedMap.Name != "Test Town" || len(parsedMap.NPCs) != 1 {
		t.Fatalf("unexpected map lookup: %+v %v", parsedMap, ok)
	}
	area, ok := table.FindAreaByDungeon(700)
	if !ok || area.ID != 20 {
		t.Fatalf("unexpected area lookup: %+v %v", area, ok)
	}
	parsedMap.NPCs[0].Position.X = 999
	again, _ := table.FindMapPath("./MAP/TOWN/TEST.MAP")
	if again.NPCs[0].Position.X != 1 {
		t.Fatalf("table returned mutable map data: %+v", again.NPCs)
	}
	area.Dungeons[0].DungeonID = 999
	againArea, _ := table.FindAreaPath("WORLDMAP/TEST.WDM")
	if againArea.Dungeons[0].DungeonID != 700 {
		t.Fatalf("table returned mutable area data: %+v", againArea.Dungeons)
	}
	dungeon, ok := table.FindDungeon(30)
	if !ok || dungeon.Metadata.Name != "Test Dungeon" || len(dungeon.Mazes) != 1 {
		t.Fatalf("unexpected dungeon lookup: %+v %v", dungeon, ok)
	}
	dungeon.Mazes[0].MapSpecifications[0].MapIDs[0] = 999
	againDungeon, _ := table.FindDungeonPath("./DUNGEON/TEST.DGN")
	if againDungeon.Mazes[0].MapSpecifications[0].MapIDs[0] != 10 {
		t.Fatalf("table returned mutable dungeon data: %+v", againDungeon.Mazes)
	}
}

func TestLoadBoundaries(t *testing.T) {
	if _, err := Load(context.Background(), nil, Options{}); !errors.Is(err, ErrIndexRequired) {
		t.Fatalf("expected ErrIndexRequired, got %v", err)
	}
	index, err := dnfpvf.Build(context.Background(), textSource{
		DefaultMapList: "1 `missing.map`\n",
	}, dnfpvf.BuildOptions{Paths: []string{DefaultMapList}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Load(context.Background(), index, Options{SkipAreas: true}); !errors.Is(err, ErrListEmpty) {
		t.Fatalf("expected ErrListEmpty, got %v", err)
	}
}

func TestLoadSourceUsesLocalPVFMultilineCompatibility(t *testing.T) {
	source := textSource{
		DefaultDungeonList:          "3 `Act1/MirkWood.dgn`\n",
		"dungeon/Act1/MirkWood.dgn": "[name]\n`MirkWood`\n[maze info]\n[size]\n2 1\n[greed]\n`AAII\n BBEE`\n[map specification]\n`map` 0 0 10 `boss` 1 0 11\n[start map]\n0 0\n[boss map]\n1 0\n",
	}
	table, err := LoadSource(context.Background(), source, Options{SkipMaps: true, SkipAreas: true})
	if err != nil {
		t.Fatalf("load source: %v", err)
	}
	if snapshot := table.Snapshot(); snapshot.Dungeons != 1 || snapshot.Mazes != 1 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	dungeon, ok := table.FindDungeon(3)
	if !ok || dungeon.Mazes[0].Greed != "AAII\n BBEE" || len(dungeon.Mazes[0].MapSpecifications) != 2 {
		t.Fatalf("multiline source dungeon = %+v, %v", dungeon, ok)
	}
}

func TestLoadMapImportsExposeSharedNPCSpawns(t *testing.T) {
	source := textSource{
		DefaultMapList:        "10 `town/root.map`\n",
		"map/town/root.map":   "[import script]\n`Common/Gate.map`\n[npc]\n23 `[left]` 10 20 0\n",
		"map/Common/Gate.map": "[npc]\n22 `[right]` 187 200 0\n[passive object]\n99 1 2 0\n",
		DefaultDungeonList:    "",
		DefaultWorldMapList:   "",
	}
	table, err := LoadSource(context.Background(), source, Options{SkipDungeons: true, SkipAreas: true})
	if err != nil {
		t.Fatalf("load source with map import: %v", err)
	}
	parsed, ok := table.FindMap(10)
	if !ok {
		t.Fatal("importing map missing")
	}
	if len(parsed.NPCs) != 2 || parsed.NPCs[0].NPCID != 22 || parsed.NPCs[0].Position.X != 187 || parsed.NPCs[0].Position.Y != 200 {
		t.Fatalf("imported NPCs = %+v", parsed.NPCs)
	}
	if len(parsed.PassiveObjects) != 1 || parsed.PassiveObjects[0].ObjectID != 99 {
		t.Fatalf("imported passive objects = %+v", parsed.PassiveObjects)
	}
}

func TestLoadIndexMapImportsExposeSharedNPCSpawns(t *testing.T) {
	source := textSource{
		DefaultMapList:        "10 `town/root.map`\n",
		"map/town/root.map":   "[import script]\n`Common/Gate.map`\n",
		"map/Common/Gate.map": "[npc]\n22 `[right]` 187 200 0\n",
	}
	index, err := dnfpvf.Build(context.Background(), source, dnfpvf.BuildOptions{
		Lists: []string{DefaultMapList},
		Paths: []string{"map/town/root.map", "map/Common/Gate.map"},
	})
	if err != nil {
		t.Fatalf("build index: %v", err)
	}
	table, err := Load(context.Background(), index, Options{SkipDungeons: true, SkipAreas: true})
	if err != nil {
		t.Fatalf("load index with map import: %v", err)
	}
	parsed, ok := table.FindMap(10)
	if !ok || len(parsed.NPCs) != 1 || parsed.NPCs[0].NPCID != 22 {
		t.Fatalf("imported index NPCs = %+v, exists=%v", parsed.NPCs, ok)
	}
}

type textSource map[string]string

func (s textSource) ReadText(relativePath string) (string, error) {
	for key, value := range s {
		if pathKey(key) == pathKey(relativePath) {
			return value, nil
		}
	}
	return "", dnfpvf.ErrDocNotFound
}
