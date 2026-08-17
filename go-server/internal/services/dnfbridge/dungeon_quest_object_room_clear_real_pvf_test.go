package dnfbridge

import (
	"context"
	"os"
	"testing"

	"longheng.io/server/internal/modules/dnf/dungeoncmd"
	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	dnfquest "longheng.io/server/internal/modules/dnf/quest"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
	"longheng.io/server/internal/modules/dnf/worldmap"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

// TestRealScriptPVFDungeon3Quest3146RoomClearPassiveTarget codifies the real
// PVF evidence behind the room-clear credit for quest 3157: the story maze is
// quest-connected to parent 3146, its boss map is 76136, and 76136 does NOT
// statically place passive object 13099 — the dummy_quest.obj is spawned
// client-side for the quest scene, so the destruction can only be credited
// from the boss-forced room clear of the quest-connected maze.
func TestRealScriptPVFDungeon3Quest3146RoomClearPassiveTarget(t *testing.T) {
	pvfPath := os.Getenv("DNF_WORLDMAP_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNF_WORLDMAP_REAL_PVF_SMOKE to verify the real quest 3146 room-clear target")
	}
	ctx := context.Background()
	archive, err := platformpvf.LoadArchive(platformpvf.Options{Path: pvfPath})
	if err != nil {
		t.Fatal(err)
	}
	table, err := worldmap.LoadSource(ctx, archive, worldmap.Options{})
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := worldmap.NewResolver(table)
	if err != nil {
		t.Fatal(err)
	}
	index, err := dnfpvf.Build(ctx, archive, dnfpvf.BuildOptions{Lists: []string{dnfquest.DefaultList}})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := dnfquest.Load(ctx, index)
	if err != nil {
		t.Fatal(err)
	}

	dungeon, ok := table.FindDungeon(3)
	if !ok {
		t.Fatal("real PVF dungeon 3 is missing")
	}
	mazeIndex := -1
	for index := range dungeon.Mazes {
		connection := dungeon.Mazes[index].QuestConnection
		if len(connection) >= 2 && connection[0] == 0 && connection[1] == 3146 {
			mazeIndex = index
			break
		}
	}
	if mazeIndex < 0 {
		t.Fatalf("real PVF dungeon 3 has no active quest connection for 3146: %+v", dungeon.Mazes)
	}
	topology, err := worldmap.BuildDungeonLayout(
		resolver,
		dungeon.ID,
		mazeIndex,
		func(choice worldmap.DungeonMapChoice) (int64, error) {
			return choice.Candidates[0].ID, nil
		},
	)
	if err != nil {
		t.Fatalf("build real dungeon 3 quest 3146 topology: %v", err)
	}
	var bossRoom *worldmap.DungeonRoom
	for _, room := range topology.Rooms() {
		if room.Map != nil && room.Map.Map.ID == 76136 {
			room := room
			bossRoom = &room
			break
		}
	}
	if bossRoom == nil || !bossRoom.Boss {
		t.Fatalf("real dungeon 3 quest 3146 maze=%d boss room with map 76136 not found", mazeIndex)
	}

	// The dummy quest object is not statically placed anywhere in map 76136.
	mapValue, ok := table.FindMap(76136)
	if !ok {
		t.Fatal("real PVF map 76136 is missing")
	}
	for _, object := range mapValue.PassiveObjects {
		if object.ObjectID == 13099 {
			t.Fatalf("map 76136 unexpectedly places passive object 13099 at (%d,%d)", object.X, object.Y)
		}
	}
	for _, object := range mapValue.SpecialPassiveObjects {
		for _, spawn := range object.Spawns {
			if spawn.Code == 13099 {
				t.Fatalf("map 76136 unexpectedly spawns special passive object 13099 via object %d", object.ObjectID)
			}
		}
	}
	for _, monster := range mapValue.Monsters {
		if monster.MonsterID == 13099 {
			t.Fatal("map 76136 unexpectedly places monster 13099")
		}
	}
	for _, actor := range mapValue.AICharacters {
		if actor.Code == 13099 {
			t.Fatal("map 76136 unexpectedly places AI character 13099")
		}
	}

	definition, ok := catalog.Find(3157)
	if !ok {
		t.Fatal("real PVF quest 3157 missing")
	}
	if definition.MainQuestID != 3146 {
		t.Fatalf("quest 3157 main quest = %d, want 3146", definition.MainQuestID)
	}
	record := dnfrepo.QuestRecord{
		CharacterID: "19",
		States: map[int64]dnfrepo.QuestState{
			3146: {Status: "active", ProgressValue: 2},
			3157: {Status: "active", ProgressValue: 1},
		},
	}
	targets := catalog.ActiveHuntEnemyTargets(record, 3, 0, currentDungeonQuestEnemyTypePassiveObject)
	if len(targets) != 1 || targets[0].QuestID != 3157 || targets[0].EnemyCode != 13099 || targets[0].EnemyType != 3 {
		t.Fatalf("real PVF active type-3 targets = %+v, want only 3157/13099/3", targets)
	}

	// Exercise the full guard chain against real PVF state.
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "19", AccountID: "acc", Level: 5}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Quest.Save(ctx, record); err != nil {
		t.Fatal(err)
	}
	runtime := &runtimeDungeonState{
		Request:        dungeoncmd.SelectDungeonRequest{DungeonID: 3, Difficulty: 0},
		Dungeon:        dungeon,
		Character:      dnfrepo.CharacterRecord{CharacterID: "19", AccountID: "acc", Level: 5},
		Session:        &worldmap.DungeonSession{},
		Room:           &runtimeDungeonRoom{},
		MazeIndex:      mazeIndex,
		BossSet:        true,
		BossCoordinate: bossRoom.Coordinate,
	}
	session := &gameSession{conn: &bufferConn{}, connID: "real-pvf-room-clear", selectedCharacterID: 19, dungeon: dungeonSessionState{runtime: runtime}}
	service := &Service{
		options:            options{gameUpperBodyCodec: gameUpperBodyCodecPlain},
		questCatalog:       catalog,
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	scene := worldmap.DungeonRoomScene{
		Coordinate: bossRoom.Coordinate,
		Boss:       true,
		Map:        *bossRoom.Map,
	}
	target, blockedReason, err := service.currentDungeonQuestObjectRoomClearTarget(ctx, session, runtime, scene)
	if err != nil || blockedReason != "" {
		t.Fatalf("real PVF room-clear target blocked: reason=%q err=%v", blockedReason, err)
	}
	if target.QuestID != 3157 || target.EnemyCode != 13099 || target.EnemyType != 3 {
		t.Fatalf("real PVF room-clear target = %+v, want 3157/13099/3", target)
	}
	t.Logf("real dungeon 3 maze=%d boss_room=%s map=76136 target=%+v", mazeIndex, bossRoom.Coordinate, target)
}
