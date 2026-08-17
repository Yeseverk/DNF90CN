package worldmap

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestRealScriptPVFWorldMapSmoke(t *testing.T) {
	pvfPath := os.Getenv("DNF_WORLDMAP_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNF_WORLDMAP_REAL_PVF_SMOKE to run the real Script.pvf smoke test")
	}
	archive, err := platformpvf.Open(pvfPath)
	if err != nil {
		t.Fatalf("open real pvf: %v", err)
	}
	index, err := dnfpvf.Build(context.Background(), archive, dnfpvf.BuildOptions{Lists: []string{DefaultWorldMapList}})
	if err != nil {
		t.Fatalf("build real worldmap index: %v", err)
	}
	table, err := Load(context.Background(), index, Options{SkipMaps: true, SkipDungeons: true})
	if err != nil {
		t.Fatalf("load real worldmap: %v", err)
	}
	if snapshot := table.Snapshot(); snapshot.Areas == 0 || snapshot.DungeonRefs == 0 {
		t.Fatalf("empty real worldmap snapshot: %+v", snapshot)
	}

	listText, err := archive.ReadText(DefaultMapList)
	if err != nil {
		t.Fatalf("read real map list: %v", err)
	}
	listDoc, err := dnfpvf.Parse(DefaultMapList, listText)
	if err != nil {
		t.Fatalf("parse real map list: %v", err)
	}
	entries := dnfpvf.ParseList(listDoc)
	if len(entries) == 0 {
		t.Fatal("real map list is empty")
	}
	mapPath := strings.TrimPrefix(strings.ReplaceAll(entries[0].Path, "\\", "/"), "/")
	if !strings.HasPrefix(strings.ToLower(mapPath), "map/") {
		mapPath = "map/" + mapPath
	}
	mapText, err := archive.ReadText(mapPath)
	if err != nil {
		t.Fatalf("read real map %s: %v", mapPath, err)
	}
	mapDoc, err := ParseDocument(mapPath, mapText)
	if err != nil {
		t.Fatalf("parse real map %s: %v", mapPath, err)
	}
	parsed := ParseMap(entries[0].ID, mapPath, mapDoc)
	if len(parsed.SourceSections) == 0 || len(parsed.PathgatePositions) == 0 || len(parsed.PassiveObjects) == 0 {
		t.Fatalf("real map core fields missing: path=%s sections=%d pathgates=%d passive=%d", mapPath, len(parsed.SourceSections), len(parsed.PathgatePositions), len(parsed.PassiveObjects))
	}

	smokeRealDungeonGraph(t, archive)
}

func TestRealScriptPVFProfessionTutorialMetadata(t *testing.T) {
	pvfPath := os.Getenv("DNF_WORLDMAP_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNF_WORLDMAP_REAL_PVF_SMOKE to verify profession tutorial metadata")
	}
	archive, err := platformpvf.Open(pvfPath)
	if err != nil {
		t.Fatalf("open real pvf: %v", err)
	}
	table, err := LoadSource(context.Background(), archive, Options{SkipMaps: true, SkipAreas: true})
	if err != nil {
		t.Fatalf("load real dungeon source: %v", err)
	}
	professionTutorials := []struct {
		id   int64
		path string
	}{
		{7110, "dungeon/Cataclysm/NewTutorial/Gunner_M_tutoral.dgn"},
		{7111, "dungeon/Cataclysm/NewTutorial/7111_TheWayToCentralPark.dgn"},
		{7112, "dungeon/Cataclysm/NewTutorial/Suzhouforest.dgn"},
		{7113, "dungeon/Cataclysm/NewTutorial/7113_Priest_M_Tutorial.dgn"},
		{7114, "dungeon/Cataclysm/NewTutorial/7114_Fighter_F_Tutorial.dgn"},
		{7115, "dungeon/Cataclysm/NewTutorial/Swordman_M.dgn"},
		{7116, "dungeon/Cataclysm/NewTutorial/Gunner_F.dgn"},
		{7117, "dungeon/Cataclysm/NewTutorial/Mage_M.dgn"},
		{7118, "dungeon/Cataclysm/NewTutorial/Tutorial_ATSwordman.dgn"},
		{7119, "dungeon/Cataclysm/NewTutorial/Thief_F.dgn"},
		{7124, "dungeon/Cataclysm/NewTutorial/knight_F.dgn"},
		{7125, "dungeon/Cataclysm/NewTutorial/Priest_F.dgn"},
		{7145, "dungeon/Cataclysm/NewTutorial/GunBlader_M_tutorial.dgn"},
	}
	for _, want := range professionTutorials {
		dungeon, ok := table.FindDungeonPath(want.path)
		if !ok {
			t.Errorf("profession tutorial missing: id=%d path=%s", want.id, want.path)
			continue
		}
		if dungeon.ID != want.id || !dungeon.Metadata.TutorialDungeon.Set || dungeon.Metadata.TutorialDungeon.Value != 1 {
			t.Errorf("profession tutorial metadata mismatch: id=%d path=%s parsed_id=%d metadata=%+v",
				want.id, want.path, dungeon.ID, dungeon.Metadata.TutorialDungeon)
		}
	}

	for _, dungeonPath := range []string{
		"dungeon/Impossible/tutorial_Cosmofiend.dgn",
		"dungeon/Impossible/tutorial_bakalcastle.dgn",
	} {
		dungeon, ok := table.FindDungeonPath(dungeonPath)
		if !ok {
			t.Errorf("named tutorial negative fixture missing: %s", dungeonPath)
			continue
		}
		if dungeon.Metadata.TutorialDungeon.Set {
			t.Errorf("name-only tutorial was treated as a profession tutorial: path=%s metadata=%+v",
				dungeonPath, dungeon.Metadata.TutorialDungeon)
		}
	}
}

func TestRealScriptPVFGunbladerTutorialStartRoomTopology(t *testing.T) {
	pvfPath := os.Getenv("DNF_WORLDMAP_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNF_WORLDMAP_REAL_PVF_SMOKE to verify the Gunblader tutorial start room")
	}
	archive, err := platformpvf.Open(pvfPath)
	if err != nil {
		t.Fatalf("open real pvf: %v", err)
	}
	table, err := LoadSource(context.Background(), archive, Options{})
	if err != nil {
		t.Fatalf("load real worldmap source: %v", err)
	}
	resolver, err := NewResolver(table)
	if err != nil {
		t.Fatalf("build real worldmap resolver: %v", err)
	}
	topology, err := BuildDungeonTopology(resolver, 7145, 0)
	if err != nil {
		t.Fatalf("build Gunblader tutorial topology: %v", err)
	}
	start, ok := topology.Room(RoomCoordinate{X: 0, Y: 0})
	if !ok || start.Map == nil {
		t.Fatalf("Gunblader start room missing: %+v found=%t diagnostics=%+v", start, ok, topology.Diagnostics)
	}
	actionText, err := archive.ReadText("map/Cataclysm/NewTutorial/GunBlader_M/Action/70576_6801.act")
	if err != nil {
		t.Fatalf("read Gunblader start basic action: %v", err)
	}
	if start.Map.Map.ID != 70576 || len(start.Neighbors) != 1 ||
		start.Neighbors[0].Direction != RoomDirectionRight ||
		start.Neighbors[0].Coordinate != (RoomCoordinate{X: 1, Y: 0}) {
		t.Fatalf("Gunblader start route=%+v want map 70576 with one right exit", start)
	}
	if len(topology.Diagnostics) != 0 {
		t.Fatalf("Gunblader topology diagnostics=%+v", topology.Diagnostics)
	}
	if start.Map.Map.BasicAction != "Action/70576_6801.act" ||
		!strings.Contains(actionText, "[END BY MOVING MAP]") {
		t.Fatalf("Gunblader start action=%q lacks move-map tutorial boundary", start.Map.Map.BasicAction)
	}
}

func TestRealScriptPVFTowerOfDespairSingleRoomLayout(t *testing.T) {
	pvfPath := os.Getenv("DNF_WORLDMAP_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNF_WORLDMAP_REAL_PVF_SMOKE to verify tower single-room map ownership")
	}
	archive, err := platformpvf.Open(pvfPath)
	if err != nil {
		t.Fatalf("open real pvf: %v", err)
	}
	table, err := LoadSource(context.Background(), archive, Options{})
	if err != nil {
		t.Fatalf("load full real pvf source: %v", err)
	}
	resolver, err := NewResolver(table)
	if err != nil {
		t.Fatalf("index real pvf resolver: %v", err)
	}
	if _, err := resolver.PoolCandidates(MapPoolRequest{
		DungeonID: 11008,
		MazeIndex: 0,
		FileType:  MapFileBoss,
	}); !errors.Is(err, ErrMapPoolEmpty) {
		t.Fatalf("real tower boss pool should be empty before start==boss fallback: %v", err)
	}
	topology, err := BuildDungeonLayout(resolver, 11008, 0, nil)
	if err != nil {
		t.Fatalf("build real tower of despair floor 1 layout: %v", err)
	}
	room, ok := topology.Room(RoomCoordinate{X: 0, Y: 0})
	if !ok || !room.Start || !room.Boss || room.Map == nil || room.Map.Map.ID != 15130 ||
		!strings.EqualFold(room.Map.Map.Path, "map/towerofdespair_down/15130despair001.map") {
		t.Fatalf("real tower room = %+v ok=%v", room, ok)
	}
}

func TestRealScriptPVFFullSourceLoad(t *testing.T) {
	pvfPath := os.Getenv("DNF_WORLDMAP_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNF_WORLDMAP_REAL_PVF_SMOKE to run the full real Script.pvf source smoke test")
	}
	archive, err := platformpvf.Open(pvfPath)
	if err != nil {
		t.Fatalf("open full real pvf: %v", err)
	}
	table, err := LoadSource(context.Background(), archive, Options{})
	if err != nil {
		t.Fatalf("load full real pvf source: %v", err)
	}
	snapshot := table.Snapshot()
	if snapshot.Maps == 0 || snapshot.Dungeons != 1450 || snapshot.Areas == 0 || snapshot.Mazes == 0 {
		t.Fatalf("unexpected full real pvf snapshot: %+v", snapshot)
	}
	resolver, err := NewResolver(table)
	if err != nil {
		t.Fatalf("index full real pvf resolver: %v", err)
	}
	if len(resolver.byDungeon) != snapshot.Dungeons {
		t.Fatalf("resolver dungeons=%d, table dungeons=%d", len(resolver.byDungeon), snapshot.Dungeons)
	}
	topologies := 0
	topologyFailures := 0
	rooms := 0
	unresolvedRooms := 0
	mapUnresolved := 0
	mapAmbiguous := 0
	startMissing := 0
	startUnresolved := 0
	startIsolated := 0
	gridShort := 0
	gridMalformed := 0
	gridPairMismatch := 0
	gridCodeUnknown := 0
	var startIssueSamples []string
	var gridIssueSamples []string
	for _, dungeon := range table.dungeons {
		for mazeIndex := range dungeon.Mazes {
			topology, err := BuildDungeonTopology(resolver, dungeon.ID, mazeIndex)
			if err != nil {
				topologyFailures++
				continue
			}
			topologies++
			rooms += len(topology.rooms)
			unresolvedRooms += len(topology.UnresolvedRooms())
			for _, resolutionError := range topology.resolutionErrors {
				switch {
				case errors.Is(resolutionError, ErrMapAmbiguous):
					mapAmbiguous++
				case errors.Is(resolutionError, ErrMapUnresolved):
					mapUnresolved++
				}
			}
			if topology.Start == nil {
				startMissing++
				if len(startIssueSamples) < 5 {
					startIssueSamples = append(startIssueSamples, fmt.Sprintf("%d/%d:missing", dungeon.ID, mazeIndex))
				}
			} else if start, ok := topology.Room(*topology.Start); !ok || start.Map == nil {
				startUnresolved++
				if len(startIssueSamples) < 5 {
					startIssueSamples = append(startIssueSamples, fmt.Sprintf("%d/%d:%s", dungeon.ID, mazeIndex, topology.Start))
				}
			} else if len(start.Neighbors) == 0 && len(topology.rooms) > 1 {
				startIsolated++
			}
			for _, diagnostic := range topology.Diagnostics {
				switch diagnostic.Code {
				case "maze_grid_short", "maze_grid_rows_short", "maze_grid_row_short":
					gridShort++
				case "maze_grid_malformed":
					gridMalformed++
				case "maze_grid_pair_mismatch":
					gridPairMismatch++
				case "maze_grid_code_unknown":
					gridCodeUnknown++
				}
				if strings.HasPrefix(diagnostic.Code, "maze_grid_") && len(gridIssueSamples) < 5 {
					gridIssueSamples = append(
						gridIssueSamples,
						fmt.Sprintf("%d/%d:%s:%s", dungeon.ID, mazeIndex, diagnostic.Code, diagnostic.Message),
					)
				}
			}
		}
	}
	if topologies == 0 || rooms == 0 {
		t.Fatalf("real PVF yielded no dungeon topology: built=%d failures=%d rooms=%d", topologies, topologyFailures, rooms)
	}
	t.Logf(
		"full real PVF source: %+v resolver_dungeons=%d topologies=%d topology_failures=%d rooms=%d unresolved_rooms=%d map_unresolved=%d map_ambiguous=%d start_missing=%d start_unresolved=%d start_isolated=%d grid_short=%d grid_malformed=%d grid_pair_mismatch=%d grid_code_unknown=%d",
		snapshot, len(resolver.byDungeon), topologies, topologyFailures, rooms, unresolvedRooms,
		mapUnresolved, mapAmbiguous, startMissing, startUnresolved, startIsolated,
		gridShort, gridMalformed, gridPairMismatch, gridCodeUnknown,
	)
	t.Logf("real PVF topology issue samples: starts=%v grids=%v", startIssueSamples, gridIssueSamples)
}

func smokeRealDungeonGraph(t *testing.T, archive *platformpvf.Archive) {
	t.Helper()
	dungeonTable, err := LoadSource(context.Background(), archive, Options{SkipMaps: true, SkipAreas: true})
	if err != nil {
		t.Fatalf("load real dungeon source: %v", err)
	}
	mapListText, err := archive.ReadText(DefaultMapList)
	if err != nil {
		t.Fatalf("read real map list for dungeon graph: %v", err)
	}
	mapListDoc, err := dnfpvf.Parse(DefaultMapList, mapListText)
	if err != nil {
		t.Fatalf("parse real map list for dungeon graph: %v", err)
	}
	mapPaths := make(map[int64]string)
	for _, entry := range dnfpvf.ParseList(mapListDoc) {
		mapPaths[entry.ID] = prefixedArchivePath("map", entry.Path)
	}

	parsedDocuments := len(dungeonTable.dungeons)
	extensionNames := make(map[string]struct{})
	randomizedScripts := 0
	randomizedObjects := 0
	clearConditions := 0
	var selected Dungeon
	selectedMaze := -1
	for _, dungeon := range dungeonTable.dungeons {
		dungeonPath := dungeon.Path
		if len(dungeon.Extensions) < len(dungeon.UnknownSections) {
			t.Fatalf("real dungeon extension/raw mismatch: path=%s unknown=%d extensions=%d", dungeonPath, len(dungeon.UnknownSections), len(dungeon.Extensions))
		}
		for _, extension := range dungeon.Extensions {
			extensionNames[sectionKey(extension.Name)] = struct{}{}
		}
		for mazePos, maze := range dungeon.Mazes {
			if len(maze.Extensions) < len(maze.UnknownSections) {
				t.Fatalf("real maze extension/raw mismatch: path=%s maze=%d unknown=%d extensions=%d", dungeonPath, mazePos, len(maze.UnknownSections), len(maze.Extensions))
			}
			for _, extension := range maze.Extensions {
				extensionNames[sectionKey(extension.Name)] = struct{}{}
			}
			randomizedScripts += len(maze.RandomizedObjects)
			clearConditions += len(maze.ClearConditions)
			for _, script := range maze.RandomizedObjects {
				randomizedObjects += len(script.Objects)
			}
			if selectedMaze < 0 && len(maze.MapSpecifications) > 0 && allSpecificationMapsPresent(maze, mapPaths) {
				selected = dungeon
				selectedMaze = mazePos
			}
		}
	}
	if parsedDocuments == 0 || selectedMaze < 0 {
		t.Fatalf("real dungeon graph unavailable: parsed=%d selected_maze=%d", parsedDocuments, selectedMaze)
	}
	if len(extensionNames) == 0 {
		t.Fatal("real PVF contains no parsed C#-missing dungeon extensions")
	}
	if randomizedScripts == 0 || randomizedObjects == 0 || clearConditions == 0 {
		t.Fatalf(
			"real complex dungeon structures missing: randomized_scripts=%d randomized_objects=%d clear_conditions=%d",
			randomizedScripts, randomizedObjects, clearConditions,
		)
	}

	selected.Mazes = []Maze{selected.Mazes[selectedMaze]}
	selected.Mazes[0].Index = 0
	maps := loadSmokeSpecificationMaps(t, archive, selected.Mazes[0], mapPaths)
	table := smokeResolverTable(selected, maps)
	resolver, err := NewResolver(table)
	if err != nil {
		t.Fatalf("index real dungeon graph: %v", err)
	}
	resolved := false
	for _, specification := range selected.Mazes[0].MapSpecifications {
		request := ResolveRequest{
			DungeonID: selected.ID, MazeIndex: 0,
			X: specification.Coordinate.X, Y: specification.Coordinate.Y,
		}
		if _, err := resolver.Resolve(request); err == nil {
			resolved = true
			break
		}
	}
	if !resolved {
		t.Fatalf("real map specifications did not yield one deterministic room: dungeon=%d path=%s", selected.ID, selected.Path)
	}
	topology, err := BuildDungeonTopology(resolver, selected.ID, 0)
	if err != nil {
		t.Fatalf("build real dungeon topology: dungeon=%d path=%s: %v", selected.ID, selected.Path, err)
	}
	run, err := NewDungeonRun(topology)
	if err != nil {
		t.Fatalf("start real dungeon run: dungeon=%d path=%s: %v", selected.ID, selected.Path, err)
	}
	current, ok := run.CurrentRoom()
	if !ok || current.Map == nil {
		t.Fatalf("real dungeon start room unresolved: dungeon=%d room=%+v", selected.ID, current)
	}
	if err := run.MarkCurrentRoomCleared(); err != nil {
		t.Fatalf("clear real dungeon start room: %v", err)
	}
	moved := false
	for _, neighbor := range current.Neighbors {
		if neighbor.Coordinate.X < 0 || neighbor.Coordinate.X > 255 || neighbor.Coordinate.Y < 0 || neighbor.Coordinate.Y > 255 {
			continue
		}
		room, ok := topology.Room(neighbor.Coordinate)
		if !ok || room.Map == nil {
			continue
		}
		if _, err := run.MoveByte(byte(neighbor.Coordinate.X), byte(neighbor.Coordinate.Y)); err != nil {
			t.Fatalf("move through real dungeon topology: %v", err)
		}
		moved = true
		break
	}
	if !moved {
		t.Fatalf("real dungeon start has no EXE-coordinate compatible resolved neighbor: dungeon=%d room=%s", selected.ID, current.Coordinate)
	}
	t.Logf(
		"real dungeon alignment: parsed=%d parse_failures=0 extension_tags=%d randomized_scripts=%d randomized_objects=%d clear_conditions=%d selected_dungeon=%d selected_path=%s maze_specs=%d loaded_maps=%d topology_rooms=%d unresolved_rooms=%d moved_to=%s",
		parsedDocuments, len(extensionNames), randomizedScripts, randomizedObjects, clearConditions, selected.ID, selected.Path,
		len(selected.Mazes[0].MapSpecifications), len(maps), len(topology.rooms), len(topology.UnresolvedRooms()), run.Snapshot().Current,
	)
}

func allSpecificationMapsPresent(maze Maze, paths map[int64]string) bool {
	for _, specification := range maze.MapSpecifications {
		for _, mapID := range specification.MapIDs {
			if paths[mapID] == "" {
				return false
			}
		}
	}
	return true
}

func loadSmokeSpecificationMaps(t *testing.T, archive *platformpvf.Archive, maze Maze, paths map[int64]string) []Map {
	t.Helper()
	seen := make(map[int64]struct{})
	var maps []Map
	for _, specification := range maze.MapSpecifications {
		for _, mapID := range specification.MapIDs {
			if _, ok := seen[mapID]; ok {
				continue
			}
			seen[mapID] = struct{}{}
			mapPath := paths[mapID]
			text, err := archive.ReadText(mapPath)
			if err != nil {
				t.Fatalf("read real specification map %d %s: %v", mapID, mapPath, err)
			}
			doc, err := ParseDocument(mapPath, text)
			if err != nil {
				t.Fatalf("parse real specification map %d %s: %v", mapID, mapPath, err)
			}
			maps = append(maps, ParseMap(mapID, mapPath, doc))
		}
	}
	return maps
}

func smokeResolverTable(dungeon Dungeon, maps []Map) *Table {
	table := &Table{
		maps: maps, dungeons: []Dungeon{dungeon},
		mapByID: make(map[int64]int), mapByPath: make(map[string]int),
		dungeonByID: map[int64]int{dungeon.ID: 0}, dungeonByPath: map[string]int{pathKey(dungeon.Path): 0},
	}
	for pos, mapValue := range maps {
		table.mapByID[mapValue.ID] = pos
		table.mapByPath[pathKey(mapValue.Path)] = pos
	}
	return table
}

func prefixedArchivePath(prefix, value string) string {
	value = strings.TrimPrefix(strings.ReplaceAll(value, "\\", "/"), "/")
	if !strings.HasPrefix(strings.ToLower(value), strings.ToLower(prefix)+"/") {
		return prefix + "/" + value
	}
	return value
}
