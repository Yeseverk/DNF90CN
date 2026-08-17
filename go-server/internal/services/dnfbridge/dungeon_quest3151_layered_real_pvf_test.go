package dnfbridge

import (
	"context"
	"encoding/binary"
	"os"
	"strings"
	"testing"
	"time"

	"longheng.io/server/internal/modules/dnf/dungeoncmd"
	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	dnfquest "longheng.io/server/internal/modules/dnf/quest"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
	"longheng.io/server/internal/modules/dnf/worldmap"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

// TestRealScriptPVFDungeon1000Quest3151LayeredStoryEvidence locks the exact
// runtime-PVF ownership behind the "森林的黑暗" regression.  Dungeon 1000 maze
// 1 first resolves its boss-coordinate base map 76186; the client-proved
// same-coordinate layered transition selects explicit layer zero, map 76187.
// Quest 3151 targets the effective layer map, never the base map.
func TestRealScriptPVFDungeon1000Quest3151LayeredStoryEvidence(t *testing.T) {
	pvfPath := os.Getenv("DNF_WORLDMAP_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNF_WORLDMAP_REAL_PVF_SMOKE to verify dungeon 1000 quest 3151 layered story ownership")
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
	monsterCatalog, err := newPVFDungeonMonsterCatalog(archive)
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

	dungeon, ok := table.FindDungeon(1000)
	if !ok || len(dungeon.Mazes) <= 1 {
		t.Fatalf("real dungeon 1000 found=%t mazes=%d", ok, len(dungeon.Mazes))
	}
	maze := dungeon.Mazes[1]
	if maze.Index != 1 || len(maze.QuestConnection) < 3 ||
		maze.QuestConnection[0] != 0 || maze.QuestConnection[1] != 3151 || maze.QuestConnection[2] != -1 {
		t.Fatalf("real dungeon 1000 maze1 index=%d quest_connection=%v, want [0 3151 -1]", maze.Index, maze.QuestConnection)
	}
	coordinate := worldmap.RoomCoordinate{X: 3, Y: 1}
	if !hasRealTypedMapSpecification(maze.MapSpecifications, coordinate, 76186, "boss") &&
		!hasRealTypedMapSpecification(maze.BossSpecifications, coordinate, 76186, "boss") {
		t.Fatalf("real dungeon 1000 maze1 map/boss specifications=%+v/%+v, want boss room %s map76186",
			maze.MapSpecifications, maze.BossSpecifications, coordinate)
	}
	if !hasRealTypedMapSpecification(maze.MapSpecifications, coordinate, 76187, "layered") &&
		!hasRealTypedMapSpecification(maze.LayeredSpecifications, coordinate, 76187, "layered") {
		t.Fatalf("real dungeon 1000 maze1 map/layered specifications=%+v/%+v, want room %s layer0 map76187",
			maze.MapSpecifications, maze.LayeredSpecifications, coordinate)
	}

	base, err := resolver.Resolve(worldmap.ResolveRequest{
		DungeonID: 1000,
		MazeIndex: 1,
		X:         coordinate.X,
		Y:         coordinate.Y,
	})
	if err != nil {
		t.Fatalf("resolve real dungeon1000 maze1 base room %s: %v", coordinate, err)
	}
	if base.Map.ID != 76186 {
		t.Fatalf("real dungeon1000 maze1 base room %s map=%d source=%s type=%q, want 76186",
			coordinate, base.Map.ID, base.Source, base.SpecificationType)
	}
	layer, err := resolver.ResolveLayered(1000, 1, coordinate, 0)
	if err != nil {
		t.Fatalf("resolve real dungeon1000 maze1 room %s layer0: %v", coordinate, err)
	}
	if layer.Map.ID != 76187 || layer.SpecificationType != "layered" {
		t.Fatalf("real dungeon1000 maze1 room %s layer0=%+v, want explicit layered map76187", coordinate, layer)
	}

	if len(base.Map.Monsters) != 2 {
		t.Fatalf("real base map76186 monsters=%+v, want two scripted boss actors", base.Map.Monsters)
	}
	for index, spawn := range base.Map.Monsters {
		if spawn.MonsterID != 107000904 || normalizeDungeonPVFSymbol(spawn.Rank) != "boss" {
			t.Fatalf("real base map76186 monster[%d]=%+v, want boss 107000904", index, spawn)
		}
	}
	if len(layer.Map.Monsters) != 1 || layer.Map.Monsters[0].MonsterID != 75100 {
		t.Fatalf("real layered map76187 monsters=%+v, want single dungeon-clear actor 75100", layer.Map.Monsters)
	}
	if len(layer.Map.PassiveObjects) != 2 ||
		layer.Map.PassiveObjects[0].ObjectID != 109006863 || layer.Map.PassiveObjects[1].ObjectID != 109006863 {
		t.Fatalf("real layered map76187 passive objects=%+v, want two stone pillars 109006863", layer.Map.PassiveObjects)
	}
	if len(layer.Map.NPCs) != 1 || layer.Map.NPCs[0].NPCID != 1000 {
		t.Fatalf("real layered map76187 NPCs=%+v, want NPC1000", layer.Map.NPCs)
	}
	fakeBoss, ok, err := monsterCatalog.Find(75100)
	if err != nil || !ok {
		t.Fatalf("load real monster75100 found=%t err=%v", ok, err)
	}
	if !strings.Contains(fakeBoss.Name, "自爆BOSS") ||
		!strings.HasSuffix(strings.ReplaceAll(fakeBoss.Path, "\\", "/"), "/75100_DungeonClear.mob") {
		t.Fatalf("real monster75100=%+v, want dungeon-clear self-destruct fake boss", fakeBoss)
	}

	quest, ok := questCatalog.Find(3151)
	if !ok || normalizeDungeonPVFSymbol(quest.Type) != "clear map" ||
		len(quest.IntData) != 1 || quest.IntData[0] != 76187 {
		t.Fatalf("real quest3151 definition=%+v found=%t, want clear-map target76187", quest, ok)
	}
	completionTime := time.Unix(1_700_000_000, 0).UTC()
	basePlan, err := questCatalog.PlanClearMapCompletion(dnfrepo.QuestRecord{
		CharacterID: "quest3151-base",
		States: map[int64]dnfrepo.QuestState{
			3151: {Status: "active", ProgressValue: 1},
		},
	}, dnfquest.ClearMapCompletionInput{
		DungeonID:     1000,
		MapID:         76186,
		CompletionKey: "dungeon1000-maze1-base76186",
		CompletedAt:   completionTime,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(basePlan.Completions) != 0 || basePlan.Record.States[3151].ProgressValue != 1 {
		t.Fatalf("base map76186 incorrectly completed quest3151: plan=%+v", basePlan)
	}
	layerPlan, err := questCatalog.PlanClearMapCompletion(dnfrepo.QuestRecord{
		CharacterID: "quest3151-layer",
		States: map[int64]dnfrepo.QuestState{
			3151: {Status: "active", ProgressValue: 1},
		},
	}, dnfquest.ClearMapCompletionInput{
		DungeonID:     1000,
		MapID:         76187,
		CompletionKey: "dungeon1000-maze1-layer76187",
		CompletedAt:   completionTime,
	})
	if err != nil {
		t.Fatal(err)
	}
	layerState := layerPlan.Record.States[3151]
	if len(layerPlan.Completions) != 1 || layerPlan.Completions[0].QuestID != 3151 ||
		layerState.Status != "active" || layerState.ProgressValue != 0 ||
		layerState.Extra["reward_state"] != "pending" ||
		layerState.Extra["completion_map_id"] != "76187" {
		t.Fatalf("layer map76187 did not complete quest3151 to pending reward: plan=%+v state=%+v", layerPlan, layerState)
	}

	aiCharacters, err := newPVFDungeonAICharacterCatalog(archive)
	if err != nil {
		t.Fatal(err)
	}
	repositories := dnfrepomemory.NewMemoryGroup()
	character := dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "account-1",
		Name:        "Quest3151Layered",
		Job:         "0",
		Level:       14,
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
			3151: {Status: "active", ProgressValue: 1},
		},
	}); err != nil {
		t.Fatal(err)
	}
	service := &Service{
		options:                 options{accountID: "account-1", gameUpperBodyCodec: gameUpperBodyCodecPlain},
		worldMapTable:           table,
		worldMapResolver:        resolver,
		dungeonMonsterTable:     monsterCatalog,
		dungeonAICharacterTable: aiCharacters,
		questCatalog:            questCatalog,
		dungeonChoice:           func(int) (int, error) { return 0, nil },
		dungeonSeed:             func() (uint32, error) { return 1, nil },
		repositoryProvider:      func() (dnfrepo.Group, bool) { return repositories, true },
	}
	conn := &bufferConn{}
	session := &gameSession{
		conn:                conn,
		connID:              "real-pvf-dungeon1000-quest3151-layered",
		selectedCharacterID: 19,
	}
	runtime, startScene, err := service.prepareDungeonRuntime(
		ctx,
		session,
		dungeoncmd.SelectDungeonRequest{DungeonID: 1000, Difficulty: 0},
	)
	if err != nil {
		t.Fatalf("prepare real dungeon1000 quest3151 runtime: %v", err)
	}
	if runtime.MazeIndex != 1 {
		t.Fatalf("real dungeon1000 active quest3151 selected maze=%d, want maze1", runtime.MazeIndex)
	}
	session.dungeon.runtime = runtime
	topology, err := worldmap.BuildDungeonLayout(
		resolver,
		1000,
		1,
		func(choice worldmap.DungeonMapChoice) (int64, error) { return choice.Candidates[0].ID, nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	baseRoom, ok := topology.Room(coordinate)
	if !ok || baseRoom.Map == nil || baseRoom.Map.Map.ID != 76186 {
		t.Fatalf("real dungeon1000 maze1 topology room %s=%+v found=%t, want base map76186", coordinate, baseRoom, ok)
	}
	path := realDungeonPathToRoom(t, topology, startScene.Coordinate, coordinate)
	for _, next := range path[1:] {
		_ = clearCurrentRealPVFSmokeRoom(t, runtime)
		moveBody := make([]byte, dungeoncmd.MoveMapRequestSize)
		moveBody[0] = byte(next.X)
		moveBody[1] = byte(next.Y)
		conn.write.Reset()
		if err := service.handleDungeonMoveMap(session, moveBody); err != nil {
			t.Fatalf("move real dungeon1000 runtime to %s: %v", next, err)
		}
	}
	baseScene := runtime.Session.Snapshot().Scene
	baseRuntimeRoom := runtime.Room.Snapshot()
	if baseScene.Coordinate != coordinate || baseScene.Map.Map.ID != 76186 || baseScene.Cleared {
		t.Fatalf("real dungeon1000 base scene before layer=%+v", baseScene)
	}
	if len(baseRuntimeRoom.Monsters) != 2 || len(baseScene.BlockingHostiles) == 0 {
		t.Fatalf("real dungeon1000 base runtime room=%+v scene=%+v", baseRuntimeRoom, baseScene)
	}
	baseObjectKeys := make([]uint32, 0, len(baseRuntimeRoom.Monsters))
	for _, monster := range baseRuntimeRoom.Monsters {
		baseObjectKeys = append(baseObjectKeys, monster.ObjectKey)
	}

	// Current EXE op45: same coordinate plus MoveKind=1 consumes the first
	// explicit PVF layer.  It does not require the base room to clear first.
	layerMoveBody := make([]byte, dungeoncmd.MoveMapRequestSize)
	layerMoveBody[0] = byte(coordinate.X)
	layerMoveBody[1] = byte(coordinate.Y)
	layerMoveBody[10] = 1
	conn.write.Reset()
	if err := service.handleDungeonMoveMap(session, layerMoveBody); err != nil {
		t.Fatalf("commit real dungeon1000 same-coordinate layer move: %v", err)
	}
	startMapPacket, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if startMapPacket.Header.Classification != 0 || startMapPacket.Header.MsgID != currentDungeonStartNotification || len(rest) != 0 {
		t.Fatalf("real dungeon1000 layered move response header=%+v rest=%x", startMapPacket.Header, rest)
	}
	if len(startMapPacket.Body) < 19 || startMapPacket.Body[0] != byte(coordinate.X) ||
		startMapPacket.Body[1] != byte(coordinate.Y) || startMapPacket.Body[2] != 1 ||
		binary.LittleEndian.Uint32(startMapPacket.Body[14:18]) != 76187 || startMapPacket.Body[18] != 1 {
		t.Fatalf("real dungeon1000 layered op29 body=%x, want room3:1 flag1 map76187 actor_count1", startMapPacket.Body)
	}
	layerScene := runtime.Session.Snapshot().Scene
	layerRuntimeRoom := runtime.Room.Snapshot()
	if layerScene.Coordinate != coordinate || layerScene.Map.Map.ID != 76187 || layerScene.Cleared ||
		!runtime.LayeredMapActive || runtime.LayeredMapIndex != 0 {
		t.Fatalf("real dungeon1000 committed layered scene=%+v runtime_layer=%t/%d", layerScene, runtime.LayeredMapActive, runtime.LayeredMapIndex)
	}
	if len(layerRuntimeRoom.Monsters) != 1 || layerRuntimeRoom.Monsters[0].Spawn.MonsterID != 75100 ||
		layerRuntimeRoom.Monsters[0].State != runtimeDungeonMonsterAnnounced || len(layerScene.BlockingHostiles) != 1 {
		t.Fatalf("real dungeon1000 layered runtime room=%+v scene=%+v, want announced blocking monster75100", layerRuntimeRoom, layerScene)
	}
	layerMonster := layerRuntimeRoom.Monsters[0]
	if reference, bound := layerScene.RuntimeObjects[layerMonster.ObjectKey]; !bound || reference != layerScene.BlockingHostiles[0] {
		t.Fatalf("real dungeon1000 layered monster binding=%+v found=%t scene=%+v", reference, bound, layerScene)
	}
	for _, staleObjectKey := range baseObjectKeys {
		if _, stillOwned := layerScene.RuntimeObjects[staleObjectKey]; stillOwned {
			t.Fatalf("real dungeon1000 old base object key %d survived layer commit: scene=%+v", staleObjectKey, layerScene)
		}
	}
	beforeClearRecord, found, err := repositories.Quest.Load(ctx, "19")
	if err != nil || !found || beforeClearRecord.States[3151].ProgressValue != 1 {
		t.Fatalf("real quest3151 changed before layer blocker death found=%t err=%v record=%+v", found, err, beforeClearRecord)
	}

	// Commit the real layer blocker through the runtime owner, then enter the
	// ordinary final-room completion path.  Completion must carry effective
	// scene map 76187 into the quest plan and persist trigger zero/pending.
	targetObjectKey := clearCurrentRealPVFSmokeRoom(t, runtime)
	if targetObjectKey != layerMonster.ObjectKey {
		t.Fatalf("real dungeon1000 layer clear key=%d want fake boss key=%d", targetObjectKey, layerMonster.ObjectKey)
	}
	clearedLayerScene := runtime.Session.Snapshot().Scene
	conn.write.Reset()
	session.dungeon.mu.Lock()
	err = service.completeCurrentDungeonOrdinaryFinalRoomAfterDeathLocked(
		session,
		runtime,
		clearedLayerScene,
		targetObjectKey,
	)
	session.dungeon.mu.Unlock()
	if err != nil {
		t.Fatalf("complete real dungeon1000 layered final room: %v", err)
	}
	completed := runtime.Session.Snapshot()
	if completed.Run.Status != worldmap.DungeonRunCompleted || completed.Scene.Map.Map.ID != 76187 ||
		!runtime.ordinaryFinalRoomClearAccepted || !runtime.clearMapCompletionPhaseAPersisted || !runtime.settlementEntrySent {
		t.Fatalf("real dungeon1000 layered completion chain incomplete: runtime=%+v snapshot=%+v", runtime, completed)
	}
	completedQuest, found, err := repositories.Quest.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load real quest3151 completion found=%t err=%v", found, err)
	}
	completedState := completedQuest.States[3151]
	if completedState.Status != "active" || completedState.ProgressValue != 0 ||
		completedState.Extra["completion_dungeon_id"] != "1000" ||
		completedState.Extra["completion_map_id"] != "76187" ||
		completedState.Extra["reward_state"] != "pending" {
		t.Fatalf("real quest3151 completed state=%+v, want active trigger0 map76187 pending", completedState)
	}

	t.Logf("real dungeon1000 maze1 room=%s base=%d -> op29_flag1 layer0=%d blocker=%d -> quest3151 trigger0/pending",
		coordinate, base.Map.ID, layer.Map.ID, layer.Map.Monsters[0].MonsterID)
}

func hasRealTypedMapSpecification(
	specifications []worldmap.MapSpecification,
	coordinate worldmap.RoomCoordinate,
	mapID int64,
	typeName string,
) bool {
	for _, specification := range specifications {
		if normalizeDungeonPVFSymbol(specification.Type) != normalizeDungeonPVFSymbol(typeName) {
			continue
		}
		if specification.Coordinate.X != coordinate.X || specification.Coordinate.Y != coordinate.Y {
			continue
		}
		for _, candidate := range specification.MapIDs {
			if candidate == mapID {
				return true
			}
		}
	}
	return false
}
