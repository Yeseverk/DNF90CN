package dnfbridge

import (
	"context"
	"encoding/hex"
	"os"
	"testing"

	"longheng.io/server/internal/modules/dnf/channelcatalog"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	"longheng.io/server/internal/modules/dnf/dungeoncmd"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
	"longheng.io/server/internal/modules/dnf/worldmap"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestRealScriptPVFDungeon3SelectsActiveQuest3145Maze(t *testing.T) {
	pvfPath := os.Getenv("DNF_WORLDMAP_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNF_WORLDMAP_REAL_PVF_SMOKE to run real dungeon maze selection smoke")
	}
	archive, err := platformpvf.Open(pvfPath)
	if err != nil {
		t.Fatal(err)
	}
	table, err := worldmap.LoadSource(context.Background(), archive, worldmap.Options{})
	if err != nil {
		t.Fatal(err)
	}
	dungeon, ok := table.FindDungeon(3)
	if !ok {
		t.Fatal("real PVF dungeon 3 is missing")
	}
	wantIndex := -1
	for index := range dungeon.Mazes {
		connection := dungeon.Mazes[index].QuestConnection
		if len(connection) >= 2 && connection[0] == 0 && connection[1] == 3145 &&
			(len(connection) < 3 || connection[2] < 0 || connection[2] <= 0) {
			wantIndex = index
			break
		}
	}
	if wantIndex < 0 {
		t.Fatalf("real PVF dungeon 3 has no difficulty-0 active quest connection for 3145: %+v", dungeon.Mazes)
	}
	selected, err := selectDungeonMaze(dungeon.Mazes, 0, dnfrepo.QuestRecord{States: map[int64]dnfrepo.QuestState{
		3145: {Status: "active", ProgressValue: 1},
	}}, func(int) (int, error) {
		t.Fatal("unique real quest maze must not invoke random chooser")
		return 0, nil
	})
	if err != nil || selected.Index != wantIndex || selected.QuestID != 3145 || selected.Reason != "active_quest_connection" {
		t.Fatalf("real dungeon 3 selection=%+v want_index=%d err=%v", selected, wantIndex, err)
	}
}

func TestRealScriptPVFDungeon3ActiveQuestRuntimePrepares(t *testing.T) {
	pvfPath := os.Getenv("DNF_WORLDMAP_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNF_WORLDMAP_REAL_PVF_SMOKE to run real dungeon runtime smoke")
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
	monsters, err := newPVFDungeonMonsterCatalog(archive)
	if err != nil {
		t.Fatal(err)
	}
	aiCharacters, err := newPVFDungeonAICharacterCatalog(archive)
	if err != nil {
		t.Fatal(err)
	}
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(context.Background(), dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "account-1",
		Level:       1,
		Stats:       map[string]int64{"fatigue": 156},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Quest.Save(context.Background(), dnfrepo.QuestRecord{
		CharacterID: "19",
		States: map[int64]dnfrepo.QuestState{
			3145: {Status: "active", ProgressValue: 1},
		},
	}); err != nil {
		t.Fatal(err)
	}
	service := &Service{
		options:                 options{accountID: "account-1"},
		worldMapTable:           table,
		worldMapResolver:        resolver,
		dungeonMonsterTable:     monsters,
		dungeonAICharacterTable: aiCharacters,
		dungeonChoice:           func(int) (int, error) { return 0, nil },
		dungeonSeed:             func() (uint32, error) { return 1, nil },
		repositoryProvider:      func() (dnfrepo.Group, bool) { return repositories, true },
	}
	runtime, scene, err := service.prepareDungeonRuntime(
		context.Background(),
		&gameSession{selectedCharacterID: 19},
		dungeoncmd.SelectDungeonRequest{DungeonID: 3, Difficulty: 0},
	)
	if err != nil {
		t.Fatalf("prepare real dungeon 3 quest runtime: %v", err)
	}
	if runtime == nil || runtime.Dungeon.ID != 3 || runtime.MazeIndex < 0 || scene.Map.Map.ID <= 0 {
		t.Fatalf("real dungeon 3 runtime=%+v scene=%+v", runtime, scene)
	}
	connection := runtime.Dungeon.Mazes[runtime.MazeIndex].QuestConnection
	if len(connection) < 2 || connection[0] != 0 || connection[1] != 3145 {
		t.Fatalf("real dungeon 3 selected maze=%d quest_connection=%v", runtime.MazeIndex, connection)
	}
}

func TestRealScriptPVFTrainingDungeon5000BuildsCurrentEntryPackets(t *testing.T) {
	pvfPath := os.Getenv("DNF_WORLDMAP_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNF_WORLDMAP_REAL_PVF_SMOKE to run real training-room entry smoke")
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
	monsters, err := newPVFDungeonMonsterCatalog(archive)
	if err != nil {
		t.Fatal(err)
	}
	aiCharacters, err := newPVFDungeonAICharacterCatalog(archive)
	if err != nil {
		t.Fatal(err)
	}
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(context.Background(), dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "account-1",
		Level:       90,
		Stats: map[string]int64{
			"fatigue": 156, "town_id": 38, "area_id": 1,
			"pos_x": 450, "pos_y": 234, "direction": 5, "area_state": 3,
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(context.Background(), dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots:       map[string]dnfrepo.ItemStack{},
	}); err != nil {
		t.Fatal(err)
	}
	service := &Service{
		options:                 options{accountID: "account-1", gameUpperBodyCodec: gameUpperBodyCodecPlain},
		worldMapTable:           table,
		worldMapResolver:        resolver,
		dungeonMonsterTable:     monsters,
		dungeonAICharacterTable: aiCharacters,
		dungeonChoice:           func(int) (int, error) { return 0, nil },
		dungeonSeed:             func() (uint32, error) { return 1, nil },
		repositoryProvider:      func() (dnfrepo.Group, bool) { return repositories, true },
	}
	capturedBody, err := hex.DecodeString("881300000000000001ffff00000000000000000000")
	if err != nil {
		t.Fatal(err)
	}
	capturedRequest, err := dungeoncmd.DecodeSelectDungeonRequest(capturedBody)
	if err != nil {
		t.Fatalf("decode captured training-room op16 body: %v", err)
	}

	runtime, scene, err := service.prepareDungeonRuntime(
		context.Background(),
		&gameSession{selectedCharacterID: 19},
		capturedRequest,
	)
	if err != nil {
		t.Fatalf("prepare real training-room runtime: %v", err)
	}
	if runtime == nil || runtime.Dungeon.ID != 5000 || runtime.MazeIndex != 0 ||
		scene.Map.Map.ID != 36250 || scene.Coordinate != (worldmap.RoomCoordinate{X: 0, Y: 0}) {
		t.Fatalf("real training-room runtime=%+v scene=%+v", runtime, scene)
	}
	if runtime.Request.RuntimeState != 1 || runtime.Request.RuntimeToken != 0xffff {
		t.Fatalf("real training-room captured request was not retained: %+v", runtime.Request)
	}
	packets, err := buildCurrentDungeonEntryPackets(runtime, scene)
	if err != nil {
		t.Fatalf("build current training-room entry packets: %v", err)
	}
	if len(packets.DungeonInfo) == 0 || len(packets.StartMap) == 0 {
		t.Fatalf("training-room entry packets are empty: info=%x start=%x", packets.DungeonInfo, packets.StartMap)
	}

	connection := &bufferConn{}
	channel := channelcatalog.Channel{ServerID: 1, ID: 206, Type: 1, Name: "ch.206", Port: 10206}
	session := &gameSession{
		conn:                  connection,
		channel:               channel,
		residentChannel:       channel,
		selectedCharacterID:   19,
		townActorOwnerChannel: byte(channel.ID),
	}
	bindDungeonSelectorOriginForTestAt(t, service, session, 38, 1, 450, 234)
	if err := service.handleDungeonSelectUpper(session, capturedBody); err != nil {
		t.Fatalf("handle captured training-room op16 body: %v", err)
	}
	var selectACK, dungeonInfo, startMap bool
	remaining := connection.write.Bytes()
	for len(remaining) > 0 {
		packet, rest := splitGameServerUpperPacket(t, remaining)
		switch packet.Header.MsgID {
		case uint16(dnfenum.CmdPacketSelectDungeon):
			selectACK = true
		case currentDungeonInfoNotification:
			dungeonInfo = len(packet.Body) > 0
		case currentDungeonStartNotification:
			startMap = len(packet.Body) > 0
		}
		remaining = rest
	}
	if !selectACK || !dungeonInfo || !startMap {
		t.Fatalf("captured training-room response sequence: op16=%t op28=%t op29=%t", selectACK, dungeonInfo, startMap)
	}
}

func TestRealScriptPVFDungeon3SelectsActiveQuest3146StoryMazeWithClearMap3054(t *testing.T) {
	pvfPath := os.Getenv("DNF_WORLDMAP_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNF_WORLDMAP_REAL_PVF_SMOKE to run real dungeon 3 quest 3146 smoke")
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
	dungeon, ok := table.FindDungeon(3)
	if !ok {
		t.Fatal("real PVF dungeon 3 is missing")
	}
	selected, err := selectDungeonMaze(dungeon.Mazes, 0, dnfrepo.QuestRecord{States: map[int64]dnfrepo.QuestState{
		3146: {Status: "active", ProgressValue: 2},
		3157: {Status: "completed", ProgressValue: 0},
	}}, func(int) (int, error) {
		t.Fatal("unique real quest 3146 maze must not invoke random chooser")
		return 0, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	connection := dungeon.Mazes[selected.Index].QuestConnection
	if len(connection) < 2 || connection[0] != 0 || connection[1] != 3146 {
		t.Fatalf("real dungeon 3 quest 3146 selection=%+v quest_connection=%v", selected, connection)
	}
	topology, err := worldmap.BuildDungeonLayout(
		resolver,
		dungeon.ID,
		selected.Index,
		func(choice worldmap.DungeonMapChoice) (int64, error) {
			return choice.Candidates[0].ID, nil
		},
	)
	if err != nil {
		t.Fatalf("build real dungeon 3 quest 3146 topology: %v", err)
	}
	foundClearMap := false
	for _, room := range topology.Rooms() {
		if room.Map != nil && room.Map.Map.ID == 76136 {
			foundClearMap = true
			t.Logf("real dungeon 3 quest 3146 clear-map target room=%s boss=%t", room.Coordinate, room.Boss)
		}
	}
	if !foundClearMap {
		t.Fatalf("real dungeon 3 quest 3146 maze=%d has no quest 3054 clear-map target 76136", selected.Index)
	}
}
