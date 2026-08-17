package worldmap

import (
	"errors"
	"reflect"
	"testing"
)

func TestDungeonSessionBindsEXEObjectKeysAndEnforcesRoomClear(t *testing.T) {
	start := RoomCoordinate{X: 0, Y: 0}
	topology := &DungeonTopology{
		DungeonID: 700,
		MazeIndex: 0,
		Start:     &start,
		rooms: map[coordinateKey]DungeonRoom{
			{x: 0, y: 0}: {
				Coordinate: start,
				Start:      true,
				Map: &ResolvedMap{Source: ResolutionMapSpecification, Map: Map{
					ID:           100,
					Monsters:     []MonsterSpawn{{MonsterID: 3001}, {MonsterID: 3002}},
					AICharacters: []AICharacter{{Code: 4001, Faction: "[monster]"}, {Code: 4002, Faction: "[neutral]"}},
					NPCs:         []NPCSpawn{{NPCID: 5001}},
				}},
				Neighbors: []RoomNeighbor{{Direction: RoomDirectionRight, Coordinate: RoomCoordinate{X: 1, Y: 0}}},
			},
			{x: 1, y: 0}: {
				Coordinate: RoomCoordinate{X: 1, Y: 0},
				Boss:       true,
				Map:        &ResolvedMap{Source: ResolutionMapSpecification, Map: Map{ID: 101}},
				Neighbors:  []RoomNeighbor{{Direction: RoomDirectionLeft, Coordinate: start}},
			},
		},
		resolutionErrors: make(map[coordinateKey]error),
	}
	run, err := NewDungeonRun(topology)
	if err != nil {
		t.Fatal(err)
	}
	session, err := NewDungeonSession(run)
	if err != nil {
		t.Fatal(err)
	}
	scene, ok := session.Scene()
	if !ok || len(scene.ExpectedHostiles) != 3 || len(scene.BlockingHostiles) != 2 || len(scene.NPCs) != 1 || scene.Cleared {
		t.Fatalf("start scene = %+v", scene)
	}
	if _, err := session.PreviewMoveByte(1, 0); !errors.Is(err, ErrRunCurrentRoomNotCleared) {
		t.Fatalf("preview before clear error = %v", err)
	}
	if snapshot := session.Snapshot(); snapshot.Run.Current != start || snapshot.Scene.Coordinate != start {
		t.Fatalf("failed preview changed session = %+v", snapshot)
	}
	if _, err := session.MoveByte(1, 0); !errors.Is(err, ErrRunCurrentRoomNotCleared) {
		t.Fatalf("move before clear error = %v", err)
	}
	bindings := []struct {
		ref HostileReference
		key uint32
	}{
		{HostileReference{Kind: HostileMonster, Index: 0}, 9001},
		{HostileReference{Kind: HostileMonster, Index: 1}, 9002},
		{HostileReference{Kind: HostileAICharacter, Index: 0}, 9003},
	}
	for _, binding := range bindings {
		if err := session.BindHostileObject(binding.ref, binding.key); err != nil {
			t.Fatalf("bind %+v: %v", binding, err)
		}
	}
	if err := session.BindHostileObject(bindings[0].ref, 9010); !errors.Is(err, ErrHostileAlreadyBound) {
		t.Fatalf("duplicate hostile bind error = %v", err)
	}
	if err := session.BindHostileObject(HostileReference{Kind: HostileAICharacter, Index: 1}, 9011); !errors.Is(err, ErrHostileReferenceInvalid) {
		t.Fatalf("neutral actor bind error = %v", err)
	}
	for index, binding := range bindings[:2] {
		cleared, err := session.MarkHostileDefeated(binding.key)
		if err != nil {
			t.Fatalf("defeat %d: %v", binding.key, err)
		}
		if cleared != (index == 1) {
			t.Fatalf("defeat %d cleared=%v", binding.key, cleared)
		}
	}
	cleared, err := session.MarkHostileDefeated(bindings[2].key)
	if err != nil || cleared {
		t.Fatalf("non-blocking AI defeat cleared=%v err=%v", cleared, err)
	}
	if _, err := session.MarkHostileDefeated(9003); !errors.Is(err, ErrHostileAlreadyDefeated) {
		t.Fatalf("duplicate defeat error = %v", err)
	}
	preview, err := session.PreviewMoveByte(1, 0)
	if err != nil {
		t.Fatalf("preview cleared transition: %v", err)
	}
	if preview.Map.Map.ID != 101 || !preview.Boss || !preview.Cleared {
		t.Fatalf("preview target scene = %+v", preview)
	}
	if snapshot := session.Snapshot(); snapshot.Run.Current != start || snapshot.Scene.Map.Map.ID != 100 {
		t.Fatalf("successful preview committed transition = %+v", snapshot)
	}
	next, err := session.MoveByte(1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if next.Map.Map.ID != 101 || !next.Boss || !next.Cleared || len(next.RuntimeObjects) != 0 {
		t.Fatalf("empty boss room scene = %+v", next)
	}
	snapshot := session.Snapshot()
	if snapshot.Run.Current.X != 1 || len(snapshot.Run.Cleared) != 2 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}

func TestDungeonSessionScriptedNonBlockingMonsterDoesNotHoldDoor(t *testing.T) {
	start := RoomCoordinate{X: 0, Y: 0}
	topology := &DungeonTopology{
		DungeonID: 701,
		MazeIndex: 0,
		Start:     &start,
		rooms: map[coordinateKey]DungeonRoom{
			{x: 0, y: 0}: {
				Coordinate: start,
				Start:      true,
				Map: &ResolvedMap{Map: Map{ID: 200, Monsters: []MonsterSpawn{
					{MonsterID: 3001}, {MonsterID: 3002},
				}}},
				Neighbors: []RoomNeighbor{{Direction: RoomDirectionRight, Coordinate: RoomCoordinate{X: 1, Y: 0}}},
			},
			{x: 1, y: 0}: {
				Coordinate: RoomCoordinate{X: 1, Y: 0},
				Boss:       true,
				Map:        &ResolvedMap{Map: Map{ID: 201}},
				Neighbors:  []RoomNeighbor{{Direction: RoomDirectionLeft, Coordinate: start}},
			},
		},
		resolutionErrors: make(map[coordinateKey]error),
	}
	run, err := NewDungeonRun(topology)
	if err != nil {
		t.Fatal(err)
	}
	session, err := NewDungeonSession(run)
	if err != nil {
		t.Fatal(err)
	}
	scripted := HostileReference{Kind: HostileMonster, Index: 1}
	removed, err := session.MarkHostileNonBlocking(scripted)
	if err != nil || !removed {
		t.Fatalf("mark scripted non-blocking removed=%t err=%v", removed, err)
	}
	for index, reference := range []HostileReference{
		{Kind: HostileMonster, Index: 0},
		scripted,
	} {
		if err := session.BindHostileObject(reference, uint32(9100+index)); err != nil {
			t.Fatal(err)
		}
	}
	cleared, err := session.MarkHostileDefeated(9100)
	if err != nil || !cleared {
		t.Fatalf("ordinary blocker death cleared=%t err=%v", cleared, err)
	}
	scene, _ := session.Scene()
	if len(scene.ExpectedHostiles) != 2 || len(scene.BlockingHostiles) != 1 || !scene.Cleared {
		t.Fatalf("scene=%+v", scene)
	}
	if _, err := session.PreviewMoveByte(1, 0); err != nil {
		t.Fatalf("preview after real blocker death: %v", err)
	}
}

func TestDungeonSessionMoveFailureLeavesRunAndSceneUnchanged(t *testing.T) {
	start := RoomCoordinate{X: 0, Y: 0}
	target := RoomCoordinate{X: 1, Y: 0}
	topology := &DungeonTopology{
		DungeonID: 701,
		MazeIndex: 0,
		Start:     &start,
		rooms: map[coordinateKey]DungeonRoom{
			{x: 0, y: 0}: {
				Coordinate: start,
				Start:      true,
				Map:        &ResolvedMap{Map: Map{ID: 100}},
			},
			{x: 1, y: 0}: {
				Coordinate: target,
			},
		},
		resolutionErrors: map[coordinateKey]error{{x: 1, y: 0}: ErrMapUnresolved},
	}
	run, err := NewDungeonRun(topology)
	if err != nil {
		t.Fatal(err)
	}
	session, err := NewDungeonSession(run)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := session.MoveByte(1, 0); !errors.Is(err, ErrRoomMapUnresolved) {
		t.Fatalf("move unresolved target error = %v", err)
	}
	snapshot := session.Snapshot()
	if snapshot.Run.Current != start || snapshot.Scene.Coordinate != start || snapshot.Scene.Map.Map.ID != 100 {
		t.Fatalf("failed transition changed owner state = %+v", snapshot)
	}
	if len(snapshot.Run.Visited) != 1 || snapshot.Run.Visited[0] != start {
		t.Fatalf("failed transition changed visited rooms = %+v", snapshot.Run.Visited)
	}
}

func TestDungeonSessionLayeredTransitionInstallsFreshSameCoordinateScene(t *testing.T) {
	table := resolverTestTable()
	table.dungeons[0].Mazes[0].MapSpecifications[0].Type = "start"
	appendResolverTestMap(table, Map{
		ID:       107,
		Path:     "map/alpha/107(0,0).map",
		Monsters: []MonsterSpawn{{MonsterID: 3001}},
	})
	appendResolverTestMap(table, Map{
		ID:       108,
		Path:     "map/alpha/108(0,0).map",
		Monsters: []MonsterSpawn{{MonsterID: 3002}},
	})
	table.dungeons[0].Mazes[0].MapSpecifications = append(
		table.dungeons[0].Mazes[0].MapSpecifications,
		MapSpecification{
			Type:       "layered",
			Coordinate: Point{X: 0, Y: 0},
			MapIDs:     []int64{107, 108},
		},
	)
	resolver, err := NewResolver(table)
	if err != nil {
		t.Fatal(err)
	}
	topology, err := BuildDungeonTopology(resolver, 700, 0)
	if err != nil {
		t.Fatal(err)
	}
	run, err := NewDungeonRun(topology)
	if err != nil {
		t.Fatal(err)
	}
	session, err := NewDungeonSession(run)
	if err != nil {
		t.Fatal(err)
	}
	coordinate := RoomCoordinate{X: 0, Y: 0}
	before := session.Snapshot()
	if before.Scene.Map.Map.ID != 100 || !before.Scene.Cleared || len(before.Run.Cleared) != 1 {
		t.Fatalf("initial scene=%+v", before)
	}

	preview, err := session.PreviewLayered(coordinate, 0)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Map.Map.ID != 107 || preview.Map.SpecificationType != "layered" || preview.Cleared || len(preview.ExpectedHostiles) != 1 {
		t.Fatalf("layer preview=%+v", preview)
	}
	if after := session.Snapshot(); !reflect.DeepEqual(after, before) {
		t.Fatalf("layer preview mutated session: before=%+v after=%+v", before, after)
	}

	first, err := session.CommitLayered(coordinate, 0)
	if err != nil {
		t.Fatal(err)
	}
	if first.Map.Map.ID != 107 || first.Coordinate != coordinate || first.Cleared ||
		len(first.RuntimeObjects) != 0 || len(first.DefeatedObjects) != 0 {
		t.Fatalf("first committed layer=%+v", first)
	}
	afterFirst := session.Snapshot()
	if afterFirst.Run.Current != coordinate || len(afterFirst.Run.Visited) != 1 || len(afterFirst.Run.Cleared) != 0 {
		t.Fatalf("first layer run state=%+v", afterFirst.Run)
	}

	// Layer changes are cinematic state changes, not door movement: a second
	// declared layer may be installed while the source layer is still uncleared.
	second, err := session.CommitLayered(coordinate, 1)
	if err != nil {
		t.Fatal(err)
	}
	if second.Map.Map.ID != 108 || second.Cleared || len(second.ExpectedHostiles) != 1 {
		t.Fatalf("second committed layer=%+v", second)
	}
	if _, err := session.PreviewLayered(RoomCoordinate{X: 1, Y: 0}, 0); !errors.Is(err, ErrLayeredCoordinateMismatch) {
		t.Fatalf("cross-coordinate layer preview error=%v", err)
	}
	if _, err := session.PreviewLayered(coordinate, 2); !errors.Is(err, ErrLayeredIndexInvalid) {
		t.Fatalf("exhausted layer preview error=%v", err)
	}
	if err := session.Abandon(); err != nil {
		t.Fatal(err)
	}
	if _, err := session.CommitLayered(coordinate, 0); !errors.Is(err, ErrRunNotActive) {
		t.Fatalf("inactive run layer commit error=%v", err)
	}
}

func TestDungeonSessionLayeredTransitionRequiresExplicitPVFSpecification(t *testing.T) {
	table := resolverTestTable()
	table.dungeons[0].Mazes[0].MapSpecifications[0].Type = "start"
	resolver, err := NewResolver(table)
	if err != nil {
		t.Fatal(err)
	}
	topology, err := BuildDungeonTopology(resolver, 700, 0)
	if err != nil {
		t.Fatal(err)
	}
	run, err := NewDungeonRun(topology)
	if err != nil {
		t.Fatal(err)
	}
	session, err := NewDungeonSession(run)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.PreviewLayered(RoomCoordinate{X: 0, Y: 0}, 0); !errors.Is(err, ErrLayeredSpecMissing) {
		t.Fatalf("missing explicit layered specification error=%v", err)
	}
}

func TestDungeonSessionStoryStagesRefreshVisitedCoordinatesWithFreshScenes(t *testing.T) {
	table := resolverTestTable()
	appendResolverTestMap(table, Map{ID: 107, Path: "map/alpha/107(1,0).map", Type: "[dummy]"})
	appendResolverTestMap(table, Map{ID: 108, Path: "map/alpha/108(0,0).map", Type: "[dummy]"})
	appendResolverTestMap(table, Map{ID: 109, Path: "map/alpha/109(1,0)boss.map", Type: "[boss]"})
	maze := &table.dungeons[0].Mazes[0]
	maze.QuestConnection = []int64{0, 9000, -1}
	maze.Boss = &MazePoint{X: 1, Y: 0, Params: []int64{0, 0, 1, 0}}
	maze.MapSpecifications = []MapSpecification{
		{Type: "map", Coordinate: Point{X: 0, Y: 0}, MapIDs: []int64{108, 100}},
		{Type: "map", Coordinate: Point{X: 1, Y: 0}, MapIDs: []int64{107}},
		{Type: "boss", Coordinate: Point{X: 1, Y: 0}, MapIDs: []int64{109}},
	}
	resolver, err := NewResolver(table)
	if err != nil {
		t.Fatal(err)
	}
	topology, err := BuildDungeonTopology(resolver, 700, 0)
	if err != nil {
		t.Fatal(err)
	}
	run, err := NewDungeonRun(topology)
	if err != nil {
		t.Fatal(err)
	}
	session, err := NewDungeonSession(run)
	if err != nil {
		t.Fatal(err)
	}
	base, _ := session.Scene()
	if base.Map.Map.ID != 100 || !base.Cleared {
		t.Fatalf("base scene=%+v", base)
	}

	for stageIndex, want := range []struct {
		coordinate RoomCoordinate
		mapID      int64
		revisit    bool
	}{
		{coordinate: RoomCoordinate{X: 1, Y: 0}, mapID: 107},
		{coordinate: RoomCoordinate{X: 0, Y: 0}, mapID: 108, revisit: true},
		{coordinate: RoomCoordinate{X: 1, Y: 0}, mapID: 109, revisit: true},
	} {
		preview, previewErr := session.PreviewStoryStage(stageIndex)
		if previewErr != nil || preview.Scene.Coordinate != want.coordinate ||
			preview.Scene.Map.Map.ID != want.mapID || preview.Revisit != want.revisit {
			t.Fatalf("preview stage %d=%+v err=%v", stageIndex, preview, previewErr)
		}
		committed, commitErr := session.CommitStoryStage(stageIndex)
		if commitErr != nil || committed.Scene.Coordinate != want.coordinate ||
			committed.Scene.Map.Map.ID != want.mapID || committed.Revisit != want.revisit {
			t.Fatalf("commit stage %d=%+v err=%v", stageIndex, committed, commitErr)
		}
	}
	snapshot := session.Snapshot()
	if snapshot.Run.Current != (RoomCoordinate{X: 1, Y: 0}) || snapshot.Scene.Map.Map.ID != 109 ||
		len(snapshot.Run.Visited) != 2 {
		t.Fatalf("final story snapshot=%+v", snapshot)
	}
}

func layeredBaseReturnTestSession(t *testing.T) (*DungeonSession, RoomCoordinate) {
	t.Helper()
	table := resolverTestTable()
	table.dungeons[0].Mazes[0].MapSpecifications[0].Type = "start"
	table.maps[0].Monsters = []MonsterSpawn{{MonsterID: 3001}}
	appendResolverTestMap(table, Map{
		ID:       107,
		Path:     "map/alpha/107(0,0).map",
		Monsters: []MonsterSpawn{{MonsterID: 3002}},
	})
	table.dungeons[0].Mazes[0].MapSpecifications = append(
		table.dungeons[0].Mazes[0].MapSpecifications,
		MapSpecification{
			Type:       "layered",
			Coordinate: Point{X: 0, Y: 0},
			MapIDs:     []int64{107},
		},
	)
	resolver, err := NewResolver(table)
	if err != nil {
		t.Fatal(err)
	}
	topology, err := BuildDungeonTopology(resolver, 700, 0)
	if err != nil {
		t.Fatal(err)
	}
	run, err := NewDungeonRun(topology)
	if err != nil {
		t.Fatal(err)
	}
	session, err := NewDungeonSession(run)
	if err != nil {
		t.Fatal(err)
	}
	return session, RoomCoordinate{X: 0, Y: 0}
}

func TestDungeonSessionLayeredBaseReturnRestoresCachedBaseScene(t *testing.T) {
	session, coordinate := layeredBaseReturnTestSession(t)
	reference := HostileReference{Kind: HostileMonster, Index: 0}
	if err := session.BindHostileObject(reference, 9001); err != nil {
		t.Fatal(err)
	}
	if cleared, err := session.MarkHostileDefeated(9001); err != nil || !cleared {
		t.Fatalf("clear base room: cleared=%t err=%v", cleared, err)
	}
	cachedBase, ok := session.Scene()
	if !ok || !cachedBase.Cleared || len(cachedBase.RuntimeObjects) != 1 || len(cachedBase.DefeatedObjects) != 1 {
		t.Fatalf("cached base scene=%+v", cachedBase)
	}

	layer, err := session.CommitLayered(coordinate, 0)
	if err != nil {
		t.Fatal(err)
	}
	if layer.Map.Map.ID != 107 || layer.Cleared {
		t.Fatalf("committed layer=%+v", layer)
	}
	if snapshot := session.Snapshot(); len(snapshot.Run.Cleared) != 0 {
		t.Fatalf("layer commit kept base clear bit=%+v", snapshot.Run)
	}

	beforeValidate := session.Snapshot()
	validated, err := session.ValidateLayeredBase(coordinate, cachedBase)
	if err != nil {
		t.Fatal(err)
	}
	if validated.Map.Map.ID != 100 || !validated.Cleared ||
		!reflect.DeepEqual(validated.RuntimeObjects, cachedBase.RuntimeObjects) ||
		!reflect.DeepEqual(validated.DefeatedObjects, cachedBase.DefeatedObjects) {
		t.Fatalf("validated base=%+v cached=%+v", validated, cachedBase)
	}
	if after := session.Snapshot(); !reflect.DeepEqual(after, beforeValidate) {
		t.Fatalf("base validation mutated session: before=%+v after=%+v", beforeValidate, after)
	}

	restored, err := session.CommitLayeredBase(coordinate, cachedBase)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Map.Map.ID != 100 || !restored.Cleared ||
		!reflect.DeepEqual(restored.RuntimeObjects, cachedBase.RuntimeObjects) ||
		!reflect.DeepEqual(restored.DefeatedObjects, cachedBase.DefeatedObjects) {
		t.Fatalf("restored base=%+v cached=%+v", restored, cachedBase)
	}
	if err := session.BindHostileObject(reference, 9010); !errors.Is(err, ErrHostileAlreadyBound) {
		t.Fatalf("restored base binding was lost: %v", err)
	}
	if _, err := session.MarkHostileDefeated(9001); !errors.Is(err, ErrHostileAlreadyDefeated) {
		t.Fatalf("restored base defeated set was lost: %v", err)
	}
	snapshot := session.Snapshot()
	if snapshot.Run.Current != coordinate || snapshot.Scene.Map.Map.ID != 100 ||
		len(snapshot.Run.Visited) != 1 || len(snapshot.Run.Cleared) != 1 {
		t.Fatalf("restored base run state=%+v", snapshot)
	}
}

func TestDungeonSessionLayeredBaseReturnRejectsStaleOrForeignCache(t *testing.T) {
	session, coordinate := layeredBaseReturnTestSession(t)
	reference := HostileReference{Kind: HostileMonster, Index: 0}
	if err := session.BindHostileObject(reference, 9001); err != nil {
		t.Fatal(err)
	}
	if cleared, err := session.MarkHostileDefeated(9001); err != nil || !cleared {
		t.Fatalf("clear base room: cleared=%t err=%v", cleared, err)
	}
	cachedBase, ok := session.Scene()
	if !ok {
		t.Fatal("missing cached base scene")
	}
	if _, err := session.CommitLayered(coordinate, 0); err != nil {
		t.Fatal(err)
	}

	if _, err := session.ValidateLayeredBase(RoomCoordinate{X: 1, Y: 0}, cachedBase); !errors.Is(err, ErrLayeredCoordinateMismatch) {
		t.Fatalf("cross-coordinate base validation error=%v", err)
	}
	invalidScenes := []struct {
		name   string
		mutate func(*DungeonRoomScene)
	}{
		{name: "map mismatch", mutate: func(scene *DungeonRoomScene) { scene.Map.Map.ID = 999 }},
		{name: "hostile binding missing", mutate: func(scene *DungeonRoomScene) { delete(scene.RuntimeObjects, 9001) }},
		{name: "clear state conflict", mutate: func(scene *DungeonRoomScene) { scene.Cleared = false }},
		{name: "defeated object unbound", mutate: func(scene *DungeonRoomScene) {
			scene.DefeatedObjects = append(scene.DefeatedObjects, 9999)
		}},
	}
	for _, test := range invalidScenes {
		t.Run(test.name, func(t *testing.T) {
			invalid := cloneDungeonRoomScene(cachedBase)
			test.mutate(&invalid)
			before := session.Snapshot()
			if _, err := session.ValidateLayeredBase(coordinate, invalid); !errors.Is(err, ErrRevisitSceneMismatch) {
				t.Fatalf("invalid base validation error=%v", err)
			}
			if after := session.Snapshot(); !reflect.DeepEqual(after, before) {
				t.Fatalf("invalid base validation mutated session: before=%+v after=%+v", before, after)
			}
			if _, err := session.CommitLayeredBase(coordinate, invalid); !errors.Is(err, ErrRevisitSceneMismatch) {
				t.Fatalf("invalid base commit error=%v", err)
			}
			if snapshot := session.Snapshot(); snapshot.Scene.Map.Map.ID != 107 || snapshot.Run.Current != coordinate {
				t.Fatalf("invalid base commit mutated session=%+v", snapshot)
			}
		})
	}

	if err := session.Abandon(); err != nil {
		t.Fatal(err)
	}
	if _, err := session.ValidateLayeredBase(coordinate, cachedBase); !errors.Is(err, ErrRunNotActive) {
		t.Fatalf("inactive run base validation error=%v", err)
	}
	if _, err := session.CommitLayeredBase(coordinate, cachedBase); !errors.Is(err, ErrRunNotActive) {
		t.Fatalf("inactive run base commit error=%v", err)
	}
}

func TestDungeonSessionAICharacterOnlyRoomIsImmediatelyPassable(t *testing.T) {
	start := RoomCoordinate{X: 0, Y: 0}
	target := RoomCoordinate{X: 1, Y: 0}
	topology := &DungeonTopology{
		DungeonID: 703,
		Start:     &start,
		rooms: map[coordinateKey]DungeonRoom{
			{x: 0, y: 0}: {
				Coordinate: start,
				Start:      true,
				Map: &ResolvedMap{Map: Map{
					ID:           100,
					AICharacters: []AICharacter{{Code: 4001, Faction: "[monster]"}},
				}},
				Neighbors: []RoomNeighbor{{Direction: RoomDirectionRight, Coordinate: target}},
			},
			{x: 1, y: 0}: {
				Coordinate: target,
				Map:        &ResolvedMap{Map: Map{ID: 101}},
				Neighbors:  []RoomNeighbor{{Direction: RoomDirectionLeft, Coordinate: start}},
			},
		},
		resolutionErrors: make(map[coordinateKey]error),
	}
	run, err := NewDungeonRun(topology)
	if err != nil {
		t.Fatal(err)
	}
	session, err := NewDungeonSession(run)
	if err != nil {
		t.Fatal(err)
	}
	scene, ok := session.Scene()
	if !ok || !scene.Cleared || len(scene.ExpectedHostiles) != 1 || len(scene.BlockingHostiles) != 0 {
		t.Fatalf("AI-only start scene=%+v ok=%t", scene, ok)
	}
	reference := HostileReference{Kind: HostileAICharacter, Index: 0}
	if err := session.BindHostileObject(reference, 9001); err != nil {
		t.Fatal(err)
	}
	cleared, err := session.MarkHostileDefeated(9001)
	if err != nil || cleared {
		t.Fatalf("AI death changed already-passable room: cleared=%v err=%v", cleared, err)
	}
	if _, err := session.PreviewMoveByte(1, 0); err != nil {
		t.Fatalf("AI-only room blocked adjacent move: %v", err)
	}
}

func TestDungeonSessionFriendlyMonsterTeamDoesNotBlockRoom(t *testing.T) {
	start := RoomCoordinate{}
	topology := &DungeonTopology{
		DungeonID: 7114,
		Start:     &start,
		rooms: map[coordinateKey]DungeonRoom{
			{}: {
				Coordinate: start,
				Start:      true,
				Boss:       true,
				Map: &ResolvedMap{Map: Map{
					ID: 76031,
					Monsters: []MonsterSpawn{
						{MonsterID: 75034},
						{MonsterID: 75035},
						{MonsterID: 75034},
					},
					MonsterTeam: []int64{100, 0, 100},
				}},
			},
		},
		resolutionErrors: make(map[coordinateKey]error),
	}
	run, err := NewDungeonRun(topology)
	if err != nil {
		t.Fatal(err)
	}
	session, err := NewDungeonSession(run)
	if err != nil {
		t.Fatal(err)
	}
	scene, ok := session.Scene()
	if !ok || scene.Cleared || len(scene.ExpectedHostiles) != 3 || len(scene.BlockingHostiles) != 2 {
		t.Fatalf("team-aware scene=%+v ok=%t", scene, ok)
	}
	for index, objectKey := range []uint32{409, 410, 411} {
		if err := session.BindHostileObject(HostileReference{Kind: HostileMonster, Index: index}, objectKey); err != nil {
			t.Fatalf("bind monster %d: %v", index, err)
		}
	}
	cleared, err := session.MarkHostileDefeated(409)
	if err != nil || cleared {
		t.Fatalf("first hostile death cleared=%t err=%v", cleared, err)
	}
	cleared, err = session.MarkHostileDefeated(411)
	if err != nil || !cleared {
		t.Fatalf("second hostile death cleared=%t err=%v", cleared, err)
	}
	if !session.Snapshot().Scene.Cleared {
		t.Fatal("friendly team-0 story monster kept the room blocked")
	}
}

func TestDungeonSessionDoesNotClearUntilEveryExpectedHostileIsBound(t *testing.T) {
	start := RoomCoordinate{}
	topology := &DungeonTopology{
		DungeonID: 1,
		Start:     &start,
		rooms: map[coordinateKey]DungeonRoom{
			{}: {Coordinate: start, Start: true, Map: &ResolvedMap{Map: Map{Monsters: []MonsterSpawn{{MonsterID: 1}, {MonsterID: 2}}}}},
		},
		resolutionErrors: make(map[coordinateKey]error),
	}
	run, err := NewDungeonRun(topology)
	if err != nil {
		t.Fatal(err)
	}
	session, err := NewDungeonSession(run)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.BindHostileObject(HostileReference{Kind: HostileMonster, Index: 0}, 10); err != nil {
		t.Fatal(err)
	}
	cleared, err := session.MarkHostileDefeated(10)
	if err != nil || cleared {
		t.Fatalf("partial binding cleared=%v err=%v", cleared, err)
	}
	if session.Snapshot().Scene.Cleared {
		t.Fatal("partially bound room became cleared")
	}
}

func TestDungeonSessionAbandonEndsActiveRun(t *testing.T) {
	start := RoomCoordinate{}
	topology := &DungeonTopology{
		DungeonID: 702,
		Start:     &start,
		rooms: map[coordinateKey]DungeonRoom{
			{}: {Coordinate: start, Start: true, Map: &ResolvedMap{Map: Map{ID: 100}}},
		},
		resolutionErrors: make(map[coordinateKey]error),
	}
	run, err := NewDungeonRun(topology)
	if err != nil {
		t.Fatal(err)
	}
	session, err := NewDungeonSession(run)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Abandon(); err != nil {
		t.Fatalf("abandon active run: %v", err)
	}
	if snapshot := session.Snapshot(); snapshot.Run.Status != DungeonRunAbandoned {
		t.Fatalf("run status = %s, want %s", snapshot.Run.Status, DungeonRunAbandoned)
	}
	if err := session.Abandon(); !errors.Is(err, ErrRunNotActive) {
		t.Fatalf("second abandon error = %v, want %v", err, ErrRunNotActive)
	}
}

func TestDungeonSessionCompleteCurrentRoomDoesNotInventDefeatedActors(t *testing.T) {
	start := RoomCoordinate{}
	topology := &DungeonTopology{
		DungeonID: 703,
		Start:     &start,
		rooms: map[coordinateKey]DungeonRoom{
			{}: {
				Coordinate: start,
				Start:      true,
				Boss:       true,
				Map: &ResolvedMap{Map: Map{
					ID:       100,
					Monsters: []MonsterSpawn{{MonsterID: 1}, {MonsterID: 2}},
				}},
			},
		},
		resolutionErrors: make(map[coordinateKey]error),
	}
	run, err := NewDungeonRun(topology)
	if err != nil {
		t.Fatal(err)
	}
	session, err := NewDungeonSession(run)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.CompleteCurrentRoom(); err != nil {
		t.Fatalf("complete scripted current room: %v", err)
	}
	snapshot := session.Snapshot()
	if snapshot.Run.Status != DungeonRunCompleted || len(snapshot.Run.Cleared) != 1 || !snapshot.Scene.Cleared {
		t.Fatalf("completion snapshot=%+v", snapshot)
	}
	if len(snapshot.Scene.DefeatedObjects) != 0 {
		t.Fatalf("completion fabricated defeated objects=%v", snapshot.Scene.DefeatedObjects)
	}
	if err := session.CompleteCurrentRoom(); !errors.Is(err, ErrRunNotActive) {
		t.Fatalf("second completion error=%v want=%v", err, ErrRunNotActive)
	}
}

func TestDungeonSessionCompleteRequiresAuthoritativelyClearedCurrentRoom(t *testing.T) {
	start := RoomCoordinate{}
	topology := &DungeonTopology{
		DungeonID: 705,
		Start:     &start,
		rooms: map[coordinateKey]DungeonRoom{
			{}: {
				Coordinate: start,
				Start:      true,
				Boss:       true,
				Map: &ResolvedMap{Map: Map{
					ID:       100,
					Monsters: []MonsterSpawn{{MonsterID: 1}},
				}},
			},
		},
		resolutionErrors: make(map[coordinateKey]error),
	}
	run, err := NewDungeonRun(topology)
	if err != nil {
		t.Fatal(err)
	}
	session, err := NewDungeonSession(run)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Complete(); !errors.Is(err, ErrRunCurrentRoomNotCleared) {
		t.Fatalf("uncleared completion error=%v want=%v", err, ErrRunCurrentRoomNotCleared)
	}
	reference := HostileReference{Kind: HostileMonster, Index: 0}
	if err := session.BindHostileObject(reference, 402); err != nil {
		t.Fatal(err)
	}
	if cleared, err := session.MarkHostileDefeated(402); err != nil || !cleared {
		t.Fatalf("mark final blocker cleared=%t err=%v", cleared, err)
	}
	if err := session.Complete(); err != nil {
		t.Fatal(err)
	}
	if snapshot := session.Snapshot(); snapshot.Run.Status != DungeonRunCompleted || !snapshot.Scene.Cleared {
		t.Fatalf("ordinary completion snapshot=%+v", snapshot)
	}
}

func TestDungeonSessionRevisitRequiresAndRestoresCachedScene(t *testing.T) {
	start := RoomCoordinate{X: 0, Y: 0}
	target := RoomCoordinate{X: 1, Y: 0}
	topology := &DungeonTopology{
		DungeonID: 704,
		Start:     &start,
		rooms: map[coordinateKey]DungeonRoom{
			{x: 0, y: 0}: {
				Coordinate: start,
				Start:      true,
				Map: &ResolvedMap{Map: Map{
					ID:       100,
					Monsters: []MonsterSpawn{{MonsterID: 3001}},
				}},
				Neighbors: []RoomNeighbor{{Direction: RoomDirectionRight, Coordinate: target}},
			},
			{x: 1, y: 0}: {
				Coordinate: target,
				Map:        &ResolvedMap{Map: Map{ID: 101}},
				Neighbors:  []RoomNeighbor{{Direction: RoomDirectionLeft, Coordinate: start}},
			},
		},
		resolutionErrors: make(map[coordinateKey]error),
	}
	run, err := NewDungeonRun(topology)
	if err != nil {
		t.Fatal(err)
	}
	session, err := NewDungeonSession(run)
	if err != nil {
		t.Fatal(err)
	}
	reference := HostileReference{Kind: HostileMonster, Index: 0}
	if err := session.BindHostileObject(reference, 9001); err != nil {
		t.Fatal(err)
	}
	if cleared, err := session.MarkHostileDefeated(9001); err != nil || !cleared {
		t.Fatalf("clear start room: cleared=%t err=%v", cleared, err)
	}
	cachedStart, ok := session.Scene()
	if !ok {
		t.Fatal("missing cached start scene")
	}

	firstPreview, err := session.PreviewMoveByteTransition(1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if firstPreview.Revisit || firstPreview.Scene.Coordinate != target || firstPreview.Scene.Map.Map.ID != 101 {
		t.Fatalf("first preview=%+v", firstPreview)
	}
	beforeFirstValidation := session.Snapshot()
	validatedFirst, err := session.ValidateMoveByteTransition(1, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if validatedFirst.Revisit || validatedFirst.Scene.Coordinate != target || validatedFirst.Scene.Map.Map.ID != 101 {
		t.Fatalf("validated first transition=%+v", validatedFirst)
	}
	if after := session.Snapshot(); !reflect.DeepEqual(after, beforeFirstValidation) {
		t.Fatalf("first transition validation mutated session: before=%+v after=%+v", beforeFirstValidation, after)
	}
	if _, err := session.ValidateMoveByteTransition(1, 0, &firstPreview.Scene); !errors.Is(err, ErrRevisitSceneUnexpected) {
		t.Fatalf("first validation accepted cached scene: %v", err)
	}
	if _, err := session.MoveByteTransition(1, 0, &firstPreview.Scene); !errors.Is(err, ErrRevisitSceneUnexpected) {
		t.Fatalf("first visit accepted cached scene: %v", err)
	}
	firstMove, err := session.MoveByteTransition(1, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if firstMove.Revisit || firstMove.Scene.Coordinate != target {
		t.Fatalf("first move=%+v", firstMove)
	}
	cachedTarget := firstMove.Scene

	revisitPreview, err := session.PreviewMoveByteTransition(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !revisitPreview.Revisit || revisitPreview.Scene.Coordinate != start || revisitPreview.Scene.Map.Map.ID != 100 {
		t.Fatalf("revisit preview=%+v", revisitPreview)
	}
	beforeRevisitValidation := session.Snapshot()
	if _, err := session.ValidateMoveByteTransition(0, 0, nil); !errors.Is(err, ErrRevisitSceneRequired) {
		t.Fatalf("revisit validation accepted missing scene: %v", err)
	}
	if after := session.Snapshot(); !reflect.DeepEqual(after, beforeRevisitValidation) {
		t.Fatalf("failed revisit validation mutated session: before=%+v after=%+v", beforeRevisitValidation, after)
	}
	if _, err := session.MoveByte(0, 0); !errors.Is(err, ErrRevisitSceneRequired) {
		t.Fatalf("legacy MoveByte silently rebuilt revisit: %v", err)
	}
	if snapshot := session.Snapshot(); snapshot.Run.Current != target || snapshot.Scene.Coordinate != target {
		t.Fatalf("missing-cache revisit mutated session=%+v", snapshot)
	}

	invalidScenes := []struct {
		name   string
		mutate func(*DungeonRoomScene)
	}{
		{name: "map mismatch", mutate: func(scene *DungeonRoomScene) { scene.Map.Map.ID = 999 }},
		{name: "hostile binding missing", mutate: func(scene *DungeonRoomScene) { delete(scene.RuntimeObjects, 9001) }},
		{name: "clear state conflict", mutate: func(scene *DungeonRoomScene) { scene.Cleared = false }},
		{name: "defeated object unbound", mutate: func(scene *DungeonRoomScene) {
			scene.DefeatedObjects = append(scene.DefeatedObjects, 9999)
		}},
	}
	for _, test := range invalidScenes {
		t.Run(test.name, func(t *testing.T) {
			invalid := cloneDungeonRoomScene(cachedStart)
			test.mutate(&invalid)
			before := session.Snapshot()
			if _, err := session.ValidateMoveByteTransition(0, 0, &invalid); !errors.Is(err, ErrRevisitSceneMismatch) {
				t.Fatalf("invalid revisit validation error=%v", err)
			}
			if after := session.Snapshot(); !reflect.DeepEqual(after, before) {
				t.Fatalf("invalid revisit validation mutated session: before=%+v after=%+v", before, after)
			}
			if _, err := session.MoveByteTransition(0, 0, &invalid); !errors.Is(err, ErrRevisitSceneMismatch) {
				t.Fatalf("invalid revisit error=%v", err)
			}
			if snapshot := session.Snapshot(); snapshot.Run.Current != target || snapshot.Scene.Coordinate != target {
				t.Fatalf("invalid revisit mutated session=%+v", snapshot)
			}
		})
	}

	validatedRevisit, err := session.ValidateMoveByteTransition(0, 0, &cachedStart)
	if err != nil {
		t.Fatal(err)
	}
	if !validatedRevisit.Revisit ||
		!reflect.DeepEqual(validatedRevisit.Scene.RuntimeObjects, cachedStart.RuntimeObjects) ||
		!reflect.DeepEqual(validatedRevisit.Scene.DefeatedObjects, cachedStart.DefeatedObjects) ||
		!validatedRevisit.Scene.Cleared {
		t.Fatalf("validated revisit=%+v cached=%+v", validatedRevisit, cachedStart)
	}
	if snapshot := session.Snapshot(); snapshot.Run.Current != target || snapshot.Scene.Coordinate != target {
		t.Fatalf("valid revisit validation committed transition=%+v", snapshot)
	}

	restored, err := session.MoveByteTransition(0, 0, &cachedStart)
	if err != nil {
		t.Fatal(err)
	}
	if !restored.Revisit || !reflect.DeepEqual(restored.Scene.RuntimeObjects, cachedStart.RuntimeObjects) ||
		!reflect.DeepEqual(restored.Scene.DefeatedObjects, cachedStart.DefeatedObjects) || !restored.Scene.Cleared {
		t.Fatalf("restored scene=%+v cached=%+v", restored.Scene, cachedStart)
	}
	if err := session.BindHostileObject(reference, 9010); !errors.Is(err, ErrHostileAlreadyBound) {
		t.Fatalf("restored binding was lost: %v", err)
	}
	if _, err := session.MarkHostileDefeated(9001); !errors.Is(err, ErrHostileAlreadyDefeated) {
		t.Fatalf("restored defeated set was lost: %v", err)
	}
	if snapshot := session.Snapshot(); snapshot.Run.Current != start || len(snapshot.Run.Cleared) != 2 {
		t.Fatalf("restored run state=%+v", snapshot)
	}

	backAgain, err := session.MoveByteTransition(1, 0, &cachedTarget)
	if err != nil {
		t.Fatal(err)
	}
	if !backAgain.Revisit || backAgain.Scene.Coordinate != target || !backAgain.Scene.Cleared {
		t.Fatalf("second revisit=%+v", backAgain)
	}
}

func TestDungeonSessionRevisitRestoresPartialProgressWhenPVFAllowsEarlyMove(t *testing.T) {
	start := RoomCoordinate{X: 0, Y: 0}
	target := RoomCoordinate{X: 1, Y: 0}
	topology := &DungeonTopology{
		DungeonID:            705,
		Start:                &start,
		AllowMoveBeforeClear: true,
		rooms: map[coordinateKey]DungeonRoom{
			{x: 0, y: 0}: {
				Coordinate: start,
				Start:      true,
				Map: &ResolvedMap{Map: Map{
					ID:       100,
					Monsters: []MonsterSpawn{{MonsterID: 3001}, {MonsterID: 3002}},
				}},
				Neighbors: []RoomNeighbor{{Direction: RoomDirectionRight, Coordinate: target}},
			},
			{x: 1, y: 0}: {
				Coordinate: target,
				Map:        &ResolvedMap{Map: Map{ID: 101}},
				Neighbors:  []RoomNeighbor{{Direction: RoomDirectionLeft, Coordinate: start}},
			},
		},
		resolutionErrors: make(map[coordinateKey]error),
	}
	run, err := NewDungeonRun(topology)
	if err != nil {
		t.Fatal(err)
	}
	session, err := NewDungeonSession(run)
	if err != nil {
		t.Fatal(err)
	}
	for index, objectKey := range []uint32{7001, 7002} {
		if err := session.BindHostileObject(HostileReference{Kind: HostileMonster, Index: index}, objectKey); err != nil {
			t.Fatal(err)
		}
	}
	if cleared, err := session.MarkHostileDefeated(7001); err != nil || cleared {
		t.Fatalf("partial defeat: cleared=%t err=%v", cleared, err)
	}
	partial, _ := session.Scene()
	if partial.Cleared || !reflect.DeepEqual(partial.DefeatedObjects, []uint32{7001}) {
		t.Fatalf("partial scene=%+v", partial)
	}
	if _, err := session.MoveByteTransition(1, 0, nil); err != nil {
		t.Fatalf("PVF early move: %v", err)
	}

	validatedPartial, err := session.ValidateMoveByteTransition(0, 0, &partial)
	if err != nil {
		t.Fatal(err)
	}
	if !validatedPartial.Revisit || validatedPartial.Scene.Cleared ||
		!reflect.DeepEqual(validatedPartial.Scene.RuntimeObjects, partial.RuntimeObjects) ||
		!reflect.DeepEqual(validatedPartial.Scene.DefeatedObjects, partial.DefeatedObjects) {
		t.Fatalf("validated partial revisit=%+v", validatedPartial)
	}
	if snapshot := session.Snapshot(); snapshot.Run.Current != target || snapshot.Scene.Coordinate != target {
		t.Fatalf("partial validation committed transition=%+v", snapshot)
	}

	restored, err := session.MoveByteTransition(0, 0, &partial)
	if err != nil {
		t.Fatal(err)
	}
	if !restored.Revisit || restored.Scene.Cleared ||
		!reflect.DeepEqual(restored.Scene.RuntimeObjects, partial.RuntimeObjects) ||
		!reflect.DeepEqual(restored.Scene.DefeatedObjects, partial.DefeatedObjects) {
		t.Fatalf("partial revisit=%+v", restored)
	}
	if _, err := session.MarkHostileDefeated(7001); !errors.Is(err, ErrHostileAlreadyDefeated) {
		t.Fatalf("partial revisit forgot old defeat: %v", err)
	}
	if cleared, err := session.MarkHostileDefeated(7002); err != nil || !cleared {
		t.Fatalf("remaining hostile did not clear restored room: cleared=%t err=%v", cleared, err)
	}
	if snapshot := session.Snapshot(); !snapshot.Scene.Cleared || len(snapshot.Run.Cleared) != 2 {
		t.Fatalf("final restored state=%+v", snapshot)
	}
}
