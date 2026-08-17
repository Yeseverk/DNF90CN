package worldmap

import (
	"errors"
	"testing"
)

func TestResolverUsesSpecificationAndExactDirectoryCoordinates(t *testing.T) {
	table := resolverTestTable()
	resolver, err := NewResolver(table)
	if err != nil {
		t.Fatalf("new resolver: %v", err)
	}

	start, err := resolver.Resolve(ResolveRequest{DungeonID: 700, MazeIndex: 0, X: 0, Y: 0})
	if err != nil || start.Map.ID != 100 || start.Source != ResolutionMapSpecification {
		t.Fatalf("start resolution = %+v err=%v", start, err)
	}
	boss, err := resolver.Resolve(ResolveRequest{DungeonID: 700, MazeIndex: 0, X: 1, Y: 0})
	if err != nil || boss.Map.ID != 101 || boss.Source != ResolutionDirectoryPath {
		t.Fatalf("boss resolution = %+v err=%v", boss, err)
	}
	normal, err := resolver.Resolve(ResolveRequest{DungeonID: 700, MazeIndex: 0, X: 0, Y: 1})
	if err != nil || normal.Map.ID != 102 || normal.Source != ResolutionDirectoryPath {
		t.Fatalf("directory resolution = %+v err=%v", normal, err)
	}
	if byPath, ok := resolver.ResolvePath(700, 0, "./MAP/ALPHA/102(0,1).MAP"); !ok || byPath.ID != 102 {
		t.Fatalf("path lookup = %+v %v", byPath, ok)
	}
	if _, ok := resolver.ResolvePath(700, 0, "map/other/200(9,9).map"); ok {
		t.Fatal("resolver accepted a map outside explicit specification directories")
	}
	snapshot, ok := resolver.Snapshot(700)
	if !ok || snapshot.Mazes != 1 || snapshot.Specifications != 2 || snapshot.CoordinateEntries < 5 {
		t.Fatalf("resolver snapshot = %+v %v", snapshot, ok)
	}
}

func TestResolverReturnsExplicitErrorsWithoutFallback(t *testing.T) {
	resolver, err := NewResolver(resolverTestTable())
	if err != nil {
		t.Fatal(err)
	}
	_, err = resolver.Resolve(ResolveRequest{DungeonID: 700, MazeIndex: 0, X: 2, Y: 0})
	if !errors.Is(err, ErrMapAmbiguous) {
		t.Fatalf("expected ErrMapAmbiguous, got %v", err)
	}
	candidates, err := resolver.ResolveCandidates(ResolveRequest{DungeonID: 700, MazeIndex: 0, X: 2, Y: 0})
	if err != nil || len(candidates) != 2 || candidates[0].Map.ID != 103 || candidates[1].Map.ID != 104 {
		t.Fatalf("explicit ambiguity candidates = %+v err=%v", candidates, err)
	}
	_, err = resolver.Resolve(ResolveRequest{DungeonID: 700, MazeIndex: 0, X: 3, Y: 0})
	if !errors.Is(err, ErrMapUnresolved) {
		t.Fatalf("boss-typed path must not satisfy a normal room: %v", err)
	}
	_, err = resolver.Resolve(ResolveRequest{DungeonID: 700, MazeIndex: 0, X: 99, Y: 99})
	if !errors.Is(err, ErrMapUnresolved) {
		t.Fatalf("expected ErrMapUnresolved, got %v", err)
	}
	_, err = resolver.Resolve(ResolveRequest{DungeonID: 999, MazeIndex: 0, X: 0, Y: 0})
	if !errors.Is(err, ErrDungeonNotIndexed) {
		t.Fatalf("expected ErrDungeonNotIndexed, got %v", err)
	}
	_, err = resolver.Resolve(ResolveRequest{DungeonID: 700, MazeIndex: 9, X: 0, Y: 0})
	if !errors.Is(err, ErrMazeNotFound) {
		t.Fatalf("expected ErrMazeNotFound, got %v", err)
	}
	if _, ok := resolver.TryResolve(ResolveRequest{DungeonID: 700, MazeIndex: 0, X: 99, Y: 99}); ok {
		t.Fatal("TryResolve reported success for an unresolved room")
	}
}

func TestResolverUsesMapDungeonOwnershipWithoutSpecificationFallback(t *testing.T) {
	table := resolverTestTable()
	table.maps = append(table.maps,
		Map{ID: 300, Path: "map/owned/300(0,0)start.map", DungeonIDs: []int64{701}},
		Map{ID: 301, Path: "map/owned/301(1,0)normal.map", DungeonIDs: []int64{701}},
		Map{ID: 302, Path: "map/owned/302(2,0)normal.map", DungeonIDs: []int64{701}},
		Map{ID: 303, Path: "map/owned/303(2,0)normal.map", DungeonIDs: []int64{701}},
	)
	table.dungeons = append(table.dungeons, Dungeon{
		ID: 701, Path: "dungeon/owned.dgn",
		Mazes: []Maze{{
			Index: 0, Start: &MazePoint{X: 0, Y: 0}, Width: OptionalInt{Value: 3, Set: true},
			Height: OptionalInt{Value: 1, Set: true},
		}},
	})
	table.dungeonByID[701] = 1
	table.dungeonByPath["dungeon/owned.dgn"] = 1
	for pos := len(table.maps) - 4; pos < len(table.maps); pos++ {
		table.mapByID[table.maps[pos].ID] = pos
		table.mapByPath[pathKey(table.maps[pos].Path)] = pos
	}

	resolver, err := NewResolver(table)
	if err != nil {
		t.Fatal(err)
	}
	start, err := resolver.Resolve(ResolveRequest{DungeonID: 701, MazeIndex: 0, X: 0, Y: 0})
	if err != nil || start.Map.ID != 300 || start.Source != ResolutionDungeonOwnership {
		t.Fatalf("ownership start = %+v err=%v", start, err)
	}
	normal, err := resolver.Resolve(ResolveRequest{DungeonID: 701, MazeIndex: 0, X: 1, Y: 0})
	if err != nil || normal.Map.ID != 301 || normal.Source != ResolutionDungeonOwnership {
		t.Fatalf("ownership normal = %+v err=%v", normal, err)
	}
	if _, err := resolver.Resolve(ResolveRequest{DungeonID: 701, MazeIndex: 0, X: 2, Y: 0}); !errors.Is(err, ErrMapAmbiguous) {
		t.Fatalf("ownership ambiguity = %v", err)
	}
	snapshot, ok := resolver.Snapshot(701)
	if !ok || snapshot.OwnershipEntries != 4 {
		t.Fatalf("ownership snapshot = %+v ok=%v", snapshot, ok)
	}
}

func TestResolverExposesTypedPoolsWithoutChoosingRandomMap(t *testing.T) {
	table := resolverTestTable()
	table.maps = append(table.maps,
		Map{ID: 400, Path: "map/pool/s400.map", DungeonIDs: []int64{702}},
		Map{ID: 401, Path: "map/pool/s401.map", DungeonIDs: []int64{702}},
		Map{ID: 402, Path: "map/pool/402.map", DungeonIDs: []int64{702}},
	)
	table.dungeons = append(table.dungeons, Dungeon{
		ID: 702, Path: "dungeon/pool.dgn",
		Mazes: []Maze{{Index: 0, Start: &MazePoint{X: 0, Y: 0}}},
	})
	table.dungeonByID[702] = 1
	table.dungeonByPath["dungeon/pool.dgn"] = 1
	for pos := len(table.maps) - 3; pos < len(table.maps); pos++ {
		table.mapByID[table.maps[pos].ID] = pos
		table.mapByPath[pathKey(table.maps[pos].Path)] = pos
	}

	resolver, err := NewResolver(table)
	if err != nil {
		t.Fatal(err)
	}
	request := MapPoolRequest{DungeonID: 702, MazeIndex: 0, FileType: MapFileStart}
	candidates, err := resolver.PoolCandidates(request)
	if err != nil || len(candidates) != 2 || candidates[0].ID != 400 || candidates[1].ID != 401 {
		t.Fatalf("start pool = %+v err=%v", candidates, err)
	}
	resolved, err := resolver.ResolvePoolChoice(request, 401)
	if err != nil || resolved.Map.ID != 401 || resolved.Source != ResolutionTypePool {
		t.Fatalf("pool choice = %+v err=%v", resolved, err)
	}
	if _, err := resolver.ResolvePoolChoice(request, 402); !errors.Is(err, ErrMapPoolChoiceInvalid) {
		t.Fatalf("wrong-type pool choice = %v", err)
	}
	if _, err := resolver.PoolCandidates(MapPoolRequest{DungeonID: 702, MazeIndex: 0, FileType: MapFileBoss}); !errors.Is(err, ErrMapPoolEmpty) {
		t.Fatalf("empty boss pool = %v", err)
	}
}

func TestResolverRejectsMissingAndAmbiguousSpecificationReferences(t *testing.T) {
	table := resolverTestTable()
	table.dungeons[0].Mazes[0].MapSpecifications[0].MapIDs = []int64{9999}
	if _, err := NewResolver(table); !errors.Is(err, ErrMapReferenceMissing) {
		t.Fatalf("expected ErrMapReferenceMissing, got %v", err)
	}

	table = resolverTestTable()
	table.dungeons[0].Mazes[0].MapSpecifications[0].MapIDs = []int64{100, 105}
	resolver, err := NewResolver(table)
	if err != nil {
		t.Fatal(err)
	}
	_, err = resolver.Resolve(ResolveRequest{DungeonID: 700, MazeIndex: 0, X: 0, Y: 0})
	if !errors.Is(err, ErrMapAmbiguous) {
		t.Fatalf("expected explicit candidate ambiguity, got %v", err)
	}
}

func TestResolverResolveLayeredUsesExplicitPVFOrder(t *testing.T) {
	table := resolverTestTable()
	appendResolverTestMap(table, Map{ID: 107, Path: "map/alpha/107(1,0).map"})
	appendResolverTestMap(table, Map{ID: 108, Path: "map/alpha/108(1,0).map"})
	table.dungeons[0].Mazes[0].MapSpecifications = append(
		table.dungeons[0].Mazes[0].MapSpecifications,
		MapSpecification{
			Type:       "layered",
			Coordinate: Point{X: 1, Y: 0},
			MapIDs:     []int64{108, 107},
		},
	)

	resolver, err := NewResolver(table)
	if err != nil {
		t.Fatal(err)
	}
	first, err := resolver.ResolveLayered(700, 0, RoomCoordinate{X: 1, Y: 0}, 0)
	if err != nil || first.Map.ID != 108 || first.Source != ResolutionMapSpecification || first.SpecificationType != "layered" {
		t.Fatalf("first layered map=%+v err=%v", first, err)
	}
	second, err := resolver.ResolveLayered(700, 0, RoomCoordinate{X: 1, Y: 0}, 1)
	if err != nil || second.Map.ID != 107 {
		t.Fatalf("second layered map=%+v err=%v", second, err)
	}
	// Ordinary resolution at the boss coordinate must keep selecting the boss
	// map; layered maps are entered only through the explicit API.
	boss, err := resolver.Resolve(ResolveRequest{DungeonID: 700, MazeIndex: 0, X: 1, Y: 0})
	if err != nil || boss.Map.ID != 101 {
		t.Fatalf("ordinary boss resolution=%+v err=%v", boss, err)
	}
	if _, err := resolver.ResolveLayered(700, 0, RoomCoordinate{X: 1, Y: 0}, -1); !errors.Is(err, ErrLayeredIndexInvalid) {
		t.Fatalf("negative layer error=%v", err)
	}
	if _, err := resolver.ResolveLayered(700, 0, RoomCoordinate{X: 1, Y: 0}, 2); !errors.Is(err, ErrLayeredIndexInvalid) {
		t.Fatalf("exhausted layer error=%v", err)
	}
	if _, err := resolver.ResolveLayered(700, 0, RoomCoordinate{X: 0, Y: 1}, 0); !errors.Is(err, ErrLayeredSpecMissing) {
		t.Fatalf("missing layered specification error=%v", err)
	}
	ordinary, err := resolver.ResolveCandidates(ResolveRequest{DungeonID: 700, MazeIndex: 0, X: 1, Y: 0})
	if err != nil || len(ordinary) != 1 || ordinary[0].Map.ID != 101 {
		t.Fatalf("ordinary candidates leaked layered maps=%+v err=%v", ordinary, err)
	}
	if _, ok := resolver.ResolvePath(700, 0, "map/alpha/108(1,0).map"); !ok {
		t.Fatal("path catalog unexpectedly lost an explicitly addressable layered map")
	}
}

func TestResolverExcludesLayeredMapsFromOrdinaryOwnershipAndPoolsPerMaze(t *testing.T) {
	table := resolverTestTable()
	appendResolverTestMap(table, Map{ID: 107, Path: "map/alpha/107(0,1).map", DungeonIDs: []int64{700}})
	appendResolverTestMap(table, Map{ID: 108, Path: "map/alpha/108.map", DungeonIDs: []int64{700}})
	table.dungeons[0].Mazes[0].LayeredSpecifications = []MapSpecification{
		{Type: "layered", Coordinate: Point{X: 0, Y: 1}, MapIDs: []int64{107, 108}},
	}

	resolver, err := NewResolver(table)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := resolver.Resolve(ResolveRequest{DungeonID: 700, MazeIndex: 0, X: 0, Y: 1})
	if err != nil || resolved.Map.ID != 102 {
		t.Fatalf("ordinary room selected layered ownership map=%+v err=%v", resolved, err)
	}
	pool, err := resolver.PoolCandidates(MapPoolRequest{DungeonID: 700, MazeIndex: 0, FileType: MapFileNormal})
	if err == nil {
		for _, candidate := range pool {
			if candidate.ID == 108 {
				t.Fatalf("ordinary pool leaked layered map=%+v", pool)
			}
		}
	}
	for index, want := range []int64{107, 108} {
		layered, layerErr := resolver.ResolveLayered(700, 0, RoomCoordinate{X: 0, Y: 1}, index)
		if layerErr != nil || layered.Map.ID != want {
			t.Fatalf("layer %d=%+v err=%v want=%d", index, layered, layerErr, want)
		}
	}
}

func TestResolverResolveLayeredAcceptsTypedSectionAndRejectsAmbiguity(t *testing.T) {
	table := resolverTestTable()
	appendResolverTestMap(table, Map{ID: 107, Path: "map/alpha/107(0,1).map"})
	appendResolverTestMap(table, Map{ID: 108, Path: "map/alpha/108(0,1).map"})
	table.dungeons[0].Mazes[0].LayeredSpecifications = []MapSpecification{{
		Type:       "layered",
		Coordinate: Point{X: 0, Y: 1},
		MapIDs:     []int64{107, 108},
	}}
	resolver, err := NewResolver(table)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := resolver.ResolveLayered(700, 0, RoomCoordinate{X: 0, Y: 1}, 1)
	if err != nil || resolved.Map.ID != 108 {
		t.Fatalf("typed layered map=%+v err=%v", resolved, err)
	}

	table.dungeons[0].Mazes[0].MapSpecifications = append(
		table.dungeons[0].Mazes[0].MapSpecifications,
		MapSpecification{Type: "layered", Coordinate: Point{X: 0, Y: 1}, MapIDs: []int64{107}},
	)
	resolver, err = NewResolver(table)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.ResolveLayered(700, 0, RoomCoordinate{X: 0, Y: 1}, 0); !errors.Is(err, ErrLayeredSpecAmbiguous) {
		t.Fatalf("ambiguous layered specification error=%v", err)
	}

	table = resolverTestTable()
	table.dungeons[0].Mazes[0].LayeredSpecifications = []MapSpecification{{
		Type:       "layered",
		Coordinate: Point{X: 0, Y: 1},
		MapIDs:     []int64{9999},
	}}
	if _, err = NewResolver(table); !errors.Is(err, ErrMapReferenceMissing) {
		t.Fatalf("missing typed layered reference error=%v", err)
	}
}

func TestResolverBuildsRepeatedBossCoordinateStoryStagesWithoutRandomizingBase(t *testing.T) {
	table := resolverTestTable()
	appendResolverTestMap(table, Map{ID: 107, Path: "map/alpha/107(1,0).map", Type: "[dummy]"})
	appendResolverTestMap(table, Map{ID: 108, Path: "map/alpha/108(2,0).map", Type: "[dummy]"})
	appendResolverTestMap(table, Map{ID: 109, Path: "map/alpha/109(1,0)boss.map", Type: "[boss]"})
	appendResolverTestMap(table, Map{ID: 110, Path: "map/alpha/110(2,0).map", Type: "[normal]"})
	maze := &table.dungeons[0].Mazes[0]
	maze.QuestConnection = []int64{0, 9000, -1}
	maze.Boss = &MazePoint{X: 1, Y: 0, Params: []int64{2, 0, 1, 0}}
	maze.MapSpecifications = []MapSpecification{
		{Type: "map", Coordinate: Point{X: 0, Y: 0}, MapIDs: []int64{100}},
		{Type: "map", Coordinate: Point{X: 1, Y: 0}, MapIDs: []int64{107}},
		{Type: "map", Coordinate: Point{X: 2, Y: 0}, MapIDs: []int64{108, 110}},
		{Type: "boss", Coordinate: Point{X: 1, Y: 0}, MapIDs: []int64{109}},
	}

	resolver, err := NewResolver(table)
	if err != nil {
		t.Fatal(err)
	}
	stages, err := resolver.StoryStages(700, 0)
	if err != nil || len(stages) != 3 {
		t.Fatalf("story stages=%+v err=%v", stages, err)
	}
	wantCoordinates := []RoomCoordinate{{X: 1, Y: 0}, {X: 2, Y: 0}, {X: 1, Y: 0}}
	wantMapIDs := []int64{107, 108, 109}
	for index := range wantMapIDs {
		resolved, stage, resolveErr := resolver.ResolveStoryStage(700, 0, index)
		if resolveErr != nil || resolved.Map.ID != wantMapIDs[index] ||
			stage.Coordinate != wantCoordinates[index] || stage.MapID != wantMapIDs[index] {
			t.Fatalf("stage %d resolved=%+v descriptor=%+v err=%v", index, resolved, stage, resolveErr)
		}
	}
	firstBase, err := resolver.Resolve(ResolveRequest{DungeonID: 700, MazeIndex: 0, X: 1, Y: 0})
	if err != nil || firstBase.Map.ID != 107 {
		t.Fatalf("first story coordinate base=%+v err=%v", firstBase, err)
	}
	secondBase, err := resolver.Resolve(ResolveRequest{DungeonID: 700, MazeIndex: 0, X: 2, Y: 0})
	if err != nil || secondBase.Map.ID != 110 {
		t.Fatalf("rendered base coordinate=%+v err=%v", secondBase, err)
	}
	ordinary, err := resolver.ResolveCandidates(ResolveRequest{DungeonID: 700, MazeIndex: 0, X: 2, Y: 0})
	if err != nil || len(ordinary) != 1 || ordinary[0].Map.ID != 110 {
		t.Fatalf("ordinary candidates leaked story stage=%+v err=%v", ordinary, err)
	}
	if _, _, err := resolver.ResolveStoryStage(700, 0, 3); !errors.Is(err, ErrStoryStageIndexInvalid) {
		t.Fatalf("exhausted story stage error=%v", err)
	}
}

func TestMapPathClassificationAndCoordinates(t *testing.T) {
	tests := []struct {
		path          string
		typeValue     MapFileType
		x, y          int64
		hasCoordinate bool
	}{
		{"map/a/8147_(1.0)boss.map", MapFileBoss, 1, 0, true},
		{"map/a/s407(4,0).map", MapFileStart, 4, 0, true},
		{"map/a/q_12(-2,3).map", MapFileQuest, -2, 3, true},
		{"map/a/bn12.map", MapFileNamed, 0, 0, false},
		{"map/a/123.map", MapFileNormal, 0, 0, false},
	}
	for _, test := range tests {
		if got := ClassifyMapFileType(test.path); got != test.typeValue {
			t.Errorf("ClassifyMapFileType(%q) = %s, want %s", test.path, got, test.typeValue)
		}
		x, y, ok := ParseMapFileCoordinate(test.path)
		if ok != test.hasCoordinate || x != test.x || y != test.y {
			t.Errorf("ParseMapFileCoordinate(%q) = (%d,%d,%v)", test.path, x, y, ok)
		}
	}
}

func resolverTestTable() *Table {
	maps := []Map{
		{ID: 100, Path: "map/alpha/100(0,0).map"},
		{ID: 101, Path: "map/alpha/101(1,0)boss.map"},
		{ID: 102, Path: "map/alpha/102(0,1).map"},
		{ID: 103, Path: "map/alpha/103(2,0).map"},
		{ID: 104, Path: "map/alpha/104(2,0).map"},
		{ID: 105, Path: "map/alpha/105(0,0).map"},
		{ID: 106, Path: "map/alpha/106(3,0)boss.map"},
		{ID: 200, Path: "map/other/200(9,9).map"},
	}
	dungeons := []Dungeon{{
		ID: 700, Path: "dungeon/test.dgn",
		Mazes: []Maze{{
			Index: 0, Start: &MazePoint{X: 0, Y: 0}, Boss: &MazePoint{X: 1, Y: 0},
			MapSpecifications: []MapSpecification{
				{Type: "map", Coordinate: Point{X: 0, Y: 0}, MapIDs: []int64{100}},
				{Type: "boss", Coordinate: Point{X: 1, Y: 0}, MapIDs: []int64{101}},
			},
		}},
	}}
	table := &Table{
		maps: maps, dungeons: dungeons,
		mapByID: make(map[int64]int), mapByPath: make(map[string]int),
		dungeonByID: map[int64]int{700: 0}, dungeonByPath: map[string]int{"dungeon/test.dgn": 0},
	}
	for pos, mapValue := range maps {
		table.mapByID[mapValue.ID] = pos
		table.mapByPath[pathKey(mapValue.Path)] = pos
	}
	return table
}

func appendResolverTestMap(table *Table, mapValue Map) {
	mapPos := len(table.maps)
	table.maps = append(table.maps, mapValue)
	table.mapByID[mapValue.ID] = mapPos
	table.mapByPath[pathKey(mapValue.Path)] = mapPos
}
