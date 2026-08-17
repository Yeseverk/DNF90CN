package dnfbridge

import (
	"context"
	"testing"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	"longheng.io/server/internal/modules/dnf/dungeoncmd"
	dnfmonster "longheng.io/server/internal/modules/dnf/monster"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

const dimensionSpacePassiveObjectID = uint32(69292)

func TestDestroyObjectClearConditionOp39CompletesBossRoomWithoutTruncatedOp38(t *testing.T) {
	source := bridgePVFSource{
		worldmap.DefaultMapList: "70185 `dimension/boss.map`\n",
		"map/dimension/boss.map": "[map name]\n`Dimension Space`\n" +
			"[dungeon]\n7156\n[type]\n`[boss]`\n" +
			"[passive object]\n69292 476 234 0\n",
		worldmap.DefaultDungeonList: "7156 `dimension.dgn`\n",
		"dungeon/dimension.dgn": "[name]\n`Dimension Space`\n" +
			"[minimum required level]\n54\n[basis level]\n55\n[limit party count]\n1\n" +
			"[maze info]\n[size]\n1 1\n[greed]\n`A`\n" +
			"[map specification]\n`boss` 0 0 70185\n[start map]\n0 0\n[boss map]\n0 0\n" +
			"[clear condition]\n[destroy object]\n69292 1\n[/clear condition]\n",
		worldmap.DefaultWorldMapList: "1 `dimension.wdm`\n",
		"worldmap/dimension.wdm":     "[name]\n`Dimension`\n[dungeon]\n7156 -1\n[/dungeon]\n",
		dnfmonster.DefaultList:       "3001 `unused.gob`\n",
		"monster/unused.gob":         "[name]\n`Unused`\n[level]\n55\n[hp]\n1\n[exp]\n0\n",
	}
	table, resolver, monsters := loadBridgeDungeonStaticData(t, source)
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(context.Background(), dnfrepo.CharacterRecord{
		CharacterID: "2",
		AccountID:   "account-1",
		Name:        "TEST",
		Job:         "0",
		Level:       55,
		Stats:       map[string]int64{"fatigue": 100},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(context.Background(), dnfrepo.InventoryRecord{
		CharacterID: "2",
		Slots:       map[string]dnfrepo.ItemStack{},
	}); err != nil {
		t.Fatal(err)
	}
	service := &Service{
		options:             options{accountID: "account-1", gameUpperBodyCodec: gameUpperBodyCodecPlain},
		worldMapTable:       table,
		worldMapResolver:    resolver,
		dungeonMonsterTable: monsters,
		dungeonChoice:       func(int) (int, error) { return 0, nil },
		dungeonSeed:         func() (uint32, error) { return 1, nil },
		repositoryProvider:  func() (dnfrepo.Group, bool) { return repositories, true },
	}
	runtime, _, err := service.prepareDungeonRuntime(
		context.Background(),
		&gameSession{selectedCharacterID: 2},
		dungeoncmd.SelectDungeonRequest{DungeonID: 7156, Difficulty: 0},
	)
	if err != nil {
		t.Fatal(err)
	}
	scene, ok := runtime.Session.Scene()
	if !ok || !scene.Boss || !scene.Cleared || len(scene.PassiveObjects) != 1 {
		t.Fatalf("fixture scene=%+v found=%t", scene, ok)
	}
	conn := &bufferConn{}
	session := &gameSession{
		conn:                conn,
		connID:              "dimension-space-destroy-object",
		selectedCharacterID: 2,
		dungeon:             dungeonSessionState{runtime: runtime},
	}
	if err := service.handleDungeonMonsterDeath(
		session,
		tutorialScopeVariableZeroCombatDeathBody(dimensionSpacePassiveObjectID),
	); err != nil {
		t.Fatal(err)
	}
	snapshot := runtime.Session.Snapshot()
	if snapshot.Run.Status != worldmap.DungeonRunCompleted ||
		!runtime.ordinaryFinalRoomClearAccepted || !runtime.settlementEntrySent {
		t.Fatalf("destroy-object clear did not enter settlement: runtime=%+v snapshot=%+v", runtime, snapshot)
	}
	packets := splitAllFinishBridgePackets(t, conn.write.Bytes())
	if len(packets) < 2 ||
		packets[len(packets)-2].Header.MsgID != uint16(dnfenum.CmdPacketNotifyBossDieCheck) ||
		packets[len(packets)-1].Header.MsgID != currentDungeonSettlementEntryMsgID {
		t.Fatalf("final packet order=%+v", packets)
	}
	for _, packet := range packets {
		if packet.Header.MsgID == uint16(dnfenum.CmdPacketNotifyDieMonster) {
			t.Fatalf("out-of-range passive object emitted truncated op38 body=%x", packet.Body)
		}
	}
	op115 := packets[len(packets)-2].Body
	if len(op115) != 4 || op115[0] != 1 || op115[1] != 1 {
		t.Fatalf("op115 body=%x", op115)
	}
}

func TestDestroyObjectClearConditionRequiresExactCurrentPVFOwnership(t *testing.T) {
	runtime := &runtimeDungeonState{
		Dungeon: worldmap.Dungeon{Mazes: []worldmap.Maze{{ClearConditions: []worldmap.ClearCondition{{
			Type: "destroy object", TargetID: int64(dimensionSpacePassiveObjectID), Count: 1,
		}}}}},
		MazeIndex:      0,
		Session:        &worldmap.DungeonSession{},
		Room:           &runtimeDungeonRoom{coordinate: worldmap.RoomCoordinate{}, mapID: 70185},
		BossSet:        true,
		BossCoordinate: worldmap.RoomCoordinate{},
	}
	scene := worldmap.DungeonRoomScene{
		Boss:           true,
		Cleared:        true,
		Map:            worldmap.ResolvedMap{Map: worldmap.Map{ID: 70185}},
		PassiveObjects: []worldmap.PassiveObject{{ObjectID: int64(dimensionSpacePassiveObjectID)}},
	}
	if source := currentDungeonDestroyObjectClearConditionSource(
		runtime,
		scene,
		dimensionSpacePassiveObjectID,
	); source == "" {
		t.Fatal("exact destroy-object ownership was not recognized")
	}

	tests := []struct {
		name   string
		key    uint32
		mutate func(*runtimeDungeonState, *worldmap.DungeonRoomScene)
	}{
		{name: "u16 key", key: 60000},
		{name: "room not clear", key: dimensionSpacePassiveObjectID, mutate: func(_ *runtimeDungeonState, scene *worldmap.DungeonRoomScene) {
			scene.Cleared = false
		}},
		{name: "not boss", key: dimensionSpacePassiveObjectID, mutate: func(_ *runtimeDungeonState, scene *worldmap.DungeonRoomScene) {
			scene.Boss = false
		}},
		{name: "map mismatch", key: dimensionSpacePassiveObjectID, mutate: func(_ *runtimeDungeonState, scene *worldmap.DungeonRoomScene) {
			scene.Map.Map.ID++
		}},
		{name: "passive missing", key: dimensionSpacePassiveObjectID, mutate: func(_ *runtimeDungeonState, scene *worldmap.DungeonRoomScene) {
			scene.PassiveObjects = nil
		}},
		{name: "count unsupported", key: dimensionSpacePassiveObjectID, mutate: func(runtime *runtimeDungeonState, _ *worldmap.DungeonRoomScene) {
			runtime.Dungeon.Mazes[0].ClearConditions[0].Count = 2
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidateRuntime := *runtime
			candidateRuntime.Dungeon = runtime.Dungeon
			candidateRuntime.Dungeon.Mazes = append([]worldmap.Maze(nil), runtime.Dungeon.Mazes...)
			candidateRuntime.Dungeon.Mazes[0].ClearConditions =
				append([]worldmap.ClearCondition(nil), runtime.Dungeon.Mazes[0].ClearConditions...)
			candidateScene := scene
			candidateScene.PassiveObjects =
				append([]worldmap.PassiveObject(nil), scene.PassiveObjects...)
			if test.mutate != nil {
				test.mutate(&candidateRuntime, &candidateScene)
			}
			if source := currentDungeonDestroyObjectClearConditionSource(
				&candidateRuntime,
				candidateScene,
				test.key,
			); source != "" {
				t.Fatalf("unowned destroy-object accepted: %s", source)
			}
		})
	}
}
