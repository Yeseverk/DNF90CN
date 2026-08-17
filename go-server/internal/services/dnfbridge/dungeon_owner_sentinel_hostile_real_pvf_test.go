package dnfbridge

import (
	"context"
	"os"
	"strconv"
	"testing"

	"longheng.io/server/internal/modules/dnf/dungeoncmd"
	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	dnfquest "longheng.io/server/internal/modules/dnf/quest"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
	"longheng.io/server/internal/modules/dnf/worldmap"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

// TestRealScriptPVFDungeon9Quest3148OwnerSentinelBlockingBoss verifies the
// exact real-data regression without adding any production ID exception:
// quest 3148 selects dungeon 9's story maze, map 76155 contains an announced
// blocking ordinary monster whose PVF row is [dummy] [boss], and the current
// EXE's variable/count-zero/owner-ffff op39 clears that room so map 76156 can
// be entered.
func TestRealScriptPVFDungeon9Quest3148OwnerSentinelBlockingBoss(t *testing.T) {
	pvfPath := os.Getenv("DNF_WORLDMAP_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNF_WORLDMAP_REAL_PVF_SMOKE to verify quest 3148 owner-sentinel boss-room transition")
	}
	ctx := context.Background()
	archive, err := platformpvf.Open(pvfPath)
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
	monsters, err := newPVFDungeonMonsterCatalog(archive)
	if err != nil {
		t.Fatal(err)
	}
	aiCharacters, err := newPVFDungeonAICharacterCatalog(archive)
	if err != nil {
		t.Fatal(err)
	}
	questIndex, err := dnfpvf.Build(ctx, archive, dnfpvf.BuildOptions{Lists: []string{dnfquest.DefaultList}})
	if err != nil {
		t.Fatal(err)
	}
	questCatalog, err := dnfquest.Load(ctx, questIndex)
	if err != nil {
		t.Fatal(err)
	}
	questDefinition, ok := questCatalog.Find(3148)
	if !ok || normalizeDungeonPVFSymbol(questDefinition.Type) != "clear map" ||
		len(questDefinition.IntData) != 1 || questDefinition.IntData[0] != 76156 {
		t.Fatalf("real quest 3148 definition=%+v found=%t, want clear-map target 76156", questDefinition, ok)
	}
	repositories := dnfrepomemory.NewMemoryGroup()
	character := dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "account-1",
		Name:        "Quest3148",
		Job:         "0",
		Level:       8,
		Stats: map[string]int64{
			"fatigue": 156,
			"exp":     0,
		},
	}
	if err := repositories.Character.Save(ctx, character); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots:       map[string]dnfrepo.ItemStack{},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Quest.Save(ctx, dnfrepo.QuestRecord{
		CharacterID: "19",
		States: map[int64]dnfrepo.QuestState{
			3148: {Status: "active", ProgressValue: 1},
		},
	}); err != nil {
		t.Fatal(err)
	}
	service := &Service{
		options:                 options{accountID: "account-1", gameUpperBodyCodec: gameUpperBodyCodecPlain},
		worldMapTable:           table,
		worldMapResolver:        resolver,
		dungeonMonsterTable:     monsters,
		dungeonAICharacterTable: aiCharacters,
		questCatalog:            questCatalog,
		dungeonChoice:           func(int) (int, error) { return 0, nil },
		dungeonSeed:             func() (uint32, error) { return 1, nil },
		repositoryProvider:      func() (dnfrepo.Group, bool) { return repositories, true },
	}
	conn := &bufferConn{}
	session := &gameSession{conn: conn, connID: "real-pvf-quest3148-owner-sentinel", selectedCharacterID: 19}
	runtime, startScene, err := service.prepareDungeonRuntime(
		ctx,
		session,
		dungeoncmd.SelectDungeonRequest{DungeonID: 9, Difficulty: 0},
	)
	if err != nil {
		t.Fatalf("prepare real dungeon 9 quest 3148 runtime: %v", err)
	}
	session.dungeon.runtime = runtime
	connection := runtime.Dungeon.Mazes[runtime.MazeIndex].QuestConnection
	if len(connection) < 2 || connection[0] != 0 || connection[1] != 3148 {
		t.Fatalf("real dungeon 9 selected maze=%d quest_connection=%v", runtime.MazeIndex, connection)
	}

	topology, err := worldmap.BuildDungeonLayout(
		resolver,
		runtime.Dungeon.ID,
		runtime.MazeIndex,
		func(choice worldmap.DungeonMapChoice) (int64, error) { return choice.Candidates[0].ID, nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	var room76155, room76156 worldmap.DungeonRoom
	found76155, found76156 := false, false
	for _, room := range topology.Rooms() {
		if room.Map == nil {
			continue
		}
		switch room.Map.Map.ID {
		case 76155:
			room76155, found76155 = room, true
		case 76156:
			room76156, found76156 = room, true
		}
	}
	if !found76155 || !found76156 {
		t.Fatalf("real quest 3148 maze=%d maps: found76155=%t found76156=%t", runtime.MazeIndex, found76155, found76156)
	}
	neighbor76156 := false
	for _, neighbor := range room76155.Neighbors {
		if neighbor.Coordinate == room76156.Coordinate {
			neighbor76156 = true
			break
		}
	}
	if !neighbor76156 {
		t.Fatalf("real map 76156 room %s is not adjacent to 76155 room %s", room76156.Coordinate, room76155.Coordinate)
	}

	path := realDungeonPathToRoom(t, topology, startScene.Coordinate, room76155.Coordinate)
	for _, coordinate := range path[1:] {
		_ = clearCurrentRealPVFSmokeRoom(t, runtime)
		moveBody := make([]byte, dungeoncmd.MoveMapRequestSize)
		moveBody[0] = byte(coordinate.X)
		moveBody[1] = byte(coordinate.Y)
		conn.write.Reset()
		if err := service.handleDungeonMoveMap(session, moveBody); err != nil {
			t.Fatalf("move real quest 3148 runtime to %s: %v", coordinate, err)
		}
	}
	if scene := runtime.Session.Snapshot().Scene; scene.Map.Map.ID != 76155 || scene.Coordinate != room76155.Coordinate {
		t.Fatalf("real quest 3148 target scene=%+v want map76155 room=%s", scene, room76155.Coordinate)
	}
	room := runtime.Room.Snapshot()
	var target runtimeDungeonMonster
	foundTarget := false
	for _, monster := range room.Monsters {
		if monster.Spawn.MonsterID == 107000909 {
			target, foundTarget = monster, true
			break
		}
	}
	if !foundTarget || normalizeDungeonPVFSymbol(target.Spawn.Rank) != "dummy" ||
		normalizeDungeonPVFSymbol(target.Spawn.SuffixMarker) != "boss" ||
		target.State != runtimeDungeonMonsterAnnounced {
		t.Fatalf("real map 76155 owner-sentinel target=%+v found=%t room=%+v", target, foundTarget, room)
	}
	scene := runtime.Session.Snapshot().Scene
	if _, source, ok := currentDungeonOwnerSentinelBlockingClearTarget(runtime, scene, target.ObjectKey); !ok || source != "current_pvf_monster_suffix_boss" {
		t.Fatalf("real map 76155 target proof ok=%t source=%q target=%+v scene=%+v", ok, source, target, scene)
	}

	conn.write.Reset()
	if err := service.handleDungeonMonsterDeath(
		session,
		currentDungeonVariableOwnerSentinelDeathBody(target.ObjectKey),
	); err != nil {
		t.Fatal(err)
	}
	cleared := runtime.Session.Snapshot()
	if !cleared.Scene.Cleared || cleared.Run.Status != worldmap.DungeonRunActive || runtime.settlementEntrySent {
		t.Fatalf("real map 76155 did not remain an intermediate cleared room: runtime=%+v snapshot=%+v", runtime, cleared)
	}

	moveBody := make([]byte, dungeoncmd.MoveMapRequestSize)
	moveBody[0] = byte(room76156.Coordinate.X)
	moveBody[1] = byte(room76156.Coordinate.Y)
	conn.write.Reset()
	if err := service.handleDungeonMoveMap(session, moveBody); err != nil {
		t.Fatalf("move real quest 3148 map76155 to map76156: %v", err)
	}
	moved := runtime.Session.Snapshot().Scene
	if moved.Coordinate != room76156.Coordinate || moved.Map.Map.ID != 76156 {
		t.Fatalf("real quest 3148 next scene=%+v want map76156 room=%s", moved, room76156.Coordinate)
	}
	if !room76156.Boss || !moved.Boss || moved.Coordinate != runtime.BossCoordinate {
		t.Fatalf("real quest 3148 target map is not the owned terminal boss room: topology=%+v scene=%+v boss=%s",
			room76156, moved, runtime.BossCoordinate)
	}

	// The current EXE evidence in this regression proves the unusual variable
	// owner-ffff op39 only for map 76155. Do not invent a C2S layout for 76156:
	// commit its real runtime blockers through the existing domain owner, then
	// exercise the already-established ordinary final-room completion entry.
	terminalTargetKey := clearCurrentRealPVFSmokeRoom(t, runtime)
	if terminalTargetKey == 0 {
		t.Fatalf("real quest 3148 map76156 had no committed blocking target: room=%+v scene=%+v",
			runtime.Room.Snapshot(), runtime.Session.Snapshot().Scene)
	}
	terminalScene := runtime.Session.Snapshot().Scene
	conn.write.Reset()
	session.dungeon.mu.Lock()
	err = service.completeCurrentDungeonOrdinaryFinalRoomAfterDeathLocked(
		session,
		runtime,
		terminalScene,
		terminalTargetKey,
	)
	session.dungeon.mu.Unlock()
	if err != nil {
		t.Fatalf("complete real quest 3148 map76156: %v", err)
	}
	assertClearMapNotificationThenBossResponseAndSettlementEntry(
		t,
		conn.write.Bytes(),
		uint16(terminalTargetKey),
		3148,
	)
	completed := runtime.Session.Snapshot()
	if completed.Run.Status != worldmap.DungeonRunCompleted ||
		!runtime.ordinaryFinalRoomClearAccepted || !runtime.bossDieCheckAccepted ||
		!runtime.clearMapCompletionPhaseAPersisted || !runtime.clearMapCompletionNotificationClosed ||
		!runtime.bossDieCheckResponseSent || !runtime.settlementEntrySent {
		t.Fatalf("real quest 3148 completion chain incomplete: runtime=%+v snapshot=%+v", runtime, completed)
	}
	questRecord, found, err := repositories.Quest.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load real quest 3148 completion found=%t err=%v", found, err)
	}
	questState, found := currentDungeonClearMapQuestState(questRecord, 3148)
	if !found || questState.Status != "active" || questState.ProgressValue != 0 ||
		questState.Extra["completion_key"] != runtime.clearMapCompletionKey ||
		questState.Extra["completion_dungeon_id"] != strconv.FormatInt(runtime.Dungeon.ID, 10) ||
		questState.Extra["completion_map_id"] != "76156" ||
		questState.Extra["reward_state"] != "pending" {
		t.Fatalf("real quest 3148 pending-reward state=%+v found=%t runtime_key=%q",
			questState, found, runtime.clearMapCompletionKey)
	}
	t.Logf("real quest 3148 maze=%d owner-ffff boss key=%d map76155=%s -> map76156=%s -> trigger0/pending op574/op115/op31",
		runtime.MazeIndex, target.ObjectKey, room76155.Coordinate, room76156.Coordinate)
}

func realDungeonPathToRoom(
	t *testing.T,
	topology *worldmap.DungeonTopology,
	start worldmap.RoomCoordinate,
	target worldmap.RoomCoordinate,
) []worldmap.RoomCoordinate {
	t.Helper()
	visited := map[worldmap.RoomCoordinate]bool{start: true}
	previous := make(map[worldmap.RoomCoordinate]worldmap.RoomCoordinate)
	queue := []worldmap.RoomCoordinate{start}
	for len(queue) != 0 && !visited[target] {
		current := queue[0]
		queue = queue[1:]
		room, ok := topology.Room(current)
		if !ok {
			t.Fatalf("real dungeon topology room missing: %s", current)
		}
		for _, neighbor := range room.Neighbors {
			if visited[neighbor.Coordinate] {
				continue
			}
			visited[neighbor.Coordinate] = true
			previous[neighbor.Coordinate] = current
			queue = append(queue, neighbor.Coordinate)
		}
	}
	if !visited[target] {
		t.Fatalf("real dungeon target room %s is unreachable from %s", target, start)
	}
	path := []worldmap.RoomCoordinate{target}
	for path[len(path)-1] != start {
		path = append(path, previous[path[len(path)-1]])
	}
	for left, right := 0, len(path)-1; left < right; left, right = left+1, right-1 {
		path[left], path[right] = path[right], path[left]
	}
	return path
}

func clearCurrentRealPVFSmokeRoom(t *testing.T, runtime *runtimeDungeonState) uint32 {
	t.Helper()
	room := runtime.Room.Snapshot()
	needsAnnouncement := false
	for _, monster := range room.Monsters {
		needsAnnouncement = needsAnnouncement || monster.State == runtimeDungeonMonsterPlanned
	}
	for _, actor := range room.ExtendedActors {
		needsAnnouncement = needsAnnouncement || actor.State == runtimeDungeonMonsterPlanned
	}
	if needsAnnouncement {
		if _, err := runtime.Room.AnnounceAllActors(runtime.Session); err != nil {
			t.Fatalf("announce real PVF smoke room: %v", err)
		}
	}
	scene := runtime.Session.Snapshot().Scene
	var lastCommittedObjectKey uint32
	for _, reference := range scene.BlockingHostiles {
		var objectKey uint32
		for candidate, bound := range scene.RuntimeObjects {
			if bound == reference {
				objectKey = candidate
				break
			}
		}
		if objectKey == 0 {
			t.Fatalf("real PVF smoke blocker has no object binding: reference=%+v scene=%+v", reference, scene)
		}
		if dungeonSceneObjectDefeated(scene.DefeatedObjects, objectKey) {
			continue
		}
		if _, _, err := runtime.Room.CommitActorDeathReport(objectKey, runtime.Session); err != nil {
			t.Fatalf("defeat real PVF smoke blocker key=%d reference=%+v: %v", objectKey, reference, err)
		}
		lastCommittedObjectKey = objectKey
		scene = runtime.Session.Snapshot().Scene
	}
	if !runtime.Session.Snapshot().Scene.Cleared {
		t.Fatalf("real PVF smoke room did not clear: %+v", runtime.Session.Snapshot().Scene)
	}
	return lastCommittedObjectKey
}
