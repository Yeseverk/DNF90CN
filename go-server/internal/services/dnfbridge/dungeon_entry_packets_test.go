package dnfbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/channelcatalog"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	"longheng.io/server/internal/modules/dnf/dungeoncmd"
	dnfitemlock "longheng.io/server/internal/modules/dnf/itemlock"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

func TestPartyLeaderDungeonSelectionEntersEveryOnlineMemberWithStablePartyIndex(t *testing.T) {
	source := bridgeDungeonPVF(false)
	source["dungeon/test.dgn"] = strings.Replace(source["dungeon/test.dgn"], "[limit party count]\n1\n", "[limit party count]\n4\n", 1)
	table, resolver, monsters := loadBridgeDungeonStaticData(t, source)
	repositories := dnfrepomemory.NewMemoryGroup()
	for _, id := range []string{"99", "100"} {
		if err := repositories.Character.Save(context.Background(), dnfrepo.CharacterRecord{
			CharacterID: id,
			AccountID:   "account-1",
			Level:       20,
			Stats: map[string]int64{
				"fatigue": 100, "town_id": 38, "area_id": 1,
				"pos_x": 450, "pos_y": 234, "direction": 5, "area_state": 3,
			},
		}); err != nil {
			t.Fatal(err)
		}
		if err := repositories.Inventory.Save(context.Background(), dnfrepo.InventoryRecord{CharacterID: id, Slots: map[string]dnfrepo.ItemStack{}}); err != nil {
			t.Fatal(err)
		}
	}
	service := &Service{
		options:             options{accountID: "account-1", gameUpperBodyCodec: gameUpperBodyCodecPlain},
		onlinePlayers:       newOnlinePlayerManager(),
		worldMapTable:       table,
		worldMapResolver:    resolver,
		dungeonMonsterTable: monsters,
		dungeonChoice:       func(int) (int, error) { return 0, nil },
		dungeonSeed:         func() (uint32, error) { return 0x12345678, nil },
		repositoryProvider:  func() (dnfrepo.Group, bool) { return repositories, true },
	}
	channel := channelcatalog.Channel{ServerID: 1, ID: 42, Type: 11, Name: "ch.42", Port: 10042}
	leaderConn, followerConn := &bufferConn{}, &bufferConn{}
	leader := &gameSession{conn: leaderConn, channel: channel, residentChannel: channel, selectedCharacterID: 99}
	follower := &gameSession{conn: followerConn, channel: channel, residentChannel: channel, selectedCharacterID: 100}
	service.bindGameSessionCharacter(leader, 99)
	service.bindGameSessionCharacter(follower, 100)
	state := alignedcmd.PartyState{
		PartyID: 99,
		UserID:  99,
		Members: []alignedcmd.PartyMemberState{
			{UserID: 99, UserState: 1, HPPercent: 100, MPPercent: 100},
			{UserID: 100, UserState: 1, HPPercent: 100, MPPercent: 100},
		},
	}
	storeRuntimePartyState(leader, state)
	storeRuntimePartyState(follower, state)
	service.onlinePlayers.EnterArea(&onlinePlayerInfo{CharacterID: 99, TownID: 38, AreaID: 1, Session: leader})
	service.onlinePlayers.EnterArea(&onlinePlayerInfo{CharacterID: 100, TownID: 38, AreaID: 1, Session: follower})
	bindDungeonSelectorOriginForTestAt(t, service, leader, 38, 1, 450, 234)
	bindDungeonSelectorOriginForTestAt(t, service, follower, 38, 1, 450, 234)
	if got := runtimePartyStateSnapshot(leader); got.PartyID != 99 || len(got.Members) != 2 {
		t.Fatalf("leader party before dungeon entry=%+v", got)
	}
	if index, ok := runtimePartyMemberIndexForSession(leader); !ok || index != 0 {
		t.Fatalf("leader party index before dungeon entry=%d ok=%t", index, ok)
	}

	body := make([]byte, dungeoncmd.SelectDungeonRequestSize)
	binary.LittleEndian.PutUint32(body[:4], 700)
	if err := service.handleDungeonSelectUpper(leader, body); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name       string
		session    *gameSession
		connection *bufferConn
		index      byte
	}{
		{name: "leader", session: leader, connection: leaderConn, index: 0},
		{name: "follower", session: follower, connection: followerConn, index: 1},
	} {
		if test.session.dungeon.runtime == nil || !test.session.dungeon.runtime.partyMemberIndexed ||
			test.session.dungeon.runtime.partyMemberIndex != test.index {
			t.Fatalf("%s runtime party index=%d indexed=%t", test.name,
				currentDungeonRuntimePartyMemberIndex(test.session.dungeon.runtime),
				test.session.dungeon.runtime != nil && test.session.dungeon.runtime.partyMemberIndexed)
		}
		if test.session.dungeon.runtime.Seed != 0x12345678 {
			t.Fatalf("%s seed=%08x", test.name, test.session.dungeon.runtime.Seed)
		}
		assertDungeonStartMapPartyIndex(t, test.connection.write.Bytes(), test.index)
		assertDungeonPartyRealtimeSnapshot(t, test.connection.write.Bytes(), 2)
		wantPartyFrames := 1
		if test.name == "follower" {
			// Passive selector actor reconstruction rebinds op9 once before
			// the ordinary dungeon-start op9 projection.
			wantPartyFrames = 2
		}
		assertDungeonPartyFrameCount(t, test.connection.write.Bytes(), test.session.selectedCharacterID, wantPartyFrames)
		if test.name == "follower" {
			assertDungeonPartyFollowerHasNoUnsolicitedSelectAck(t, test.connection.write.Bytes())
			assertDungeonPartyFollowerPrerequisitePrecedesRuntime(t, test.connection.write.Bytes())
		}
		if resident := service.onlinePlayers.SessionForCharacter(test.session.selectedCharacterID); resident != nil {
			t.Fatalf("%s retained town presence after dungeon entry: %p", test.name, resident)
		}
	}
}

func TestPartyLeaderDungeonRoomMoveSynchronizesEveryMemberAndLeaderSeed(t *testing.T) {
	source := bridgeDungeonMovePVF(false)
	source[bridgeDungeonMoveDungeonPath] = strings.Replace(
		source[bridgeDungeonMoveDungeonPath],
		"[limit party count]\n1\n",
		"[limit party count]\n4\n",
		1,
	)
	table, resolver, monsters := loadBridgeDungeonStaticData(t, source)
	repositories := dnfrepomemory.NewMemoryGroup()
	for _, id := range []string{"99", "100"} {
		if err := repositories.Character.Save(context.Background(), dnfrepo.CharacterRecord{
			CharacterID: id,
			AccountID:   "account-1",
			Job:         atSwordmanTutorialJob,
			Level:       20,
			Stats: map[string]int64{
				"fatigue": 100, "town_id": 38, "area_id": 1,
				"pos_x": 450, "pos_y": 234, "direction": 5, "area_state": 3,
			},
		}); err != nil {
			t.Fatal(err)
		}
		if err := repositories.Inventory.Save(context.Background(), dnfrepo.InventoryRecord{CharacterID: id, Slots: map[string]dnfrepo.ItemStack{}}); err != nil {
			t.Fatal(err)
		}
	}
	seedCalls := uint32(0)
	service := &Service{
		options:             options{accountID: "account-1", gameUpperBodyCodec: gameUpperBodyCodecPlain},
		onlinePlayers:       newOnlinePlayerManager(),
		worldMapTable:       table,
		worldMapResolver:    resolver,
		dungeonMonsterTable: monsters,
		dungeonChoice:       func(int) (int, error) { return 0, nil },
		dungeonSeed: func() (uint32, error) {
			seedCalls++
			return 0x1000 + seedCalls, nil
		},
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	channel := channelcatalog.Channel{ServerID: 1, ID: 42, Type: 11, Name: "ch.42", Port: 10042}
	leaderConn, followerConn := &bufferConn{}, &bufferConn{}
	leader := &gameSession{conn: leaderConn, channel: channel, residentChannel: channel, selectedCharacterID: 99}
	follower := &gameSession{conn: followerConn, channel: channel, residentChannel: channel, selectedCharacterID: 100}
	service.bindGameSessionCharacter(leader, 99)
	service.bindGameSessionCharacter(follower, 100)
	state := alignedcmd.PartyState{
		PartyID: 99, UserID: 99,
		Members: []alignedcmd.PartyMemberState{
			{UserID: 99, UserState: 1, HPPercent: 100, MPPercent: 100},
			{UserID: 100, UserState: 1, HPPercent: 100, MPPercent: 100},
		},
	}
	storeRuntimePartyState(leader, state)
	storeRuntimePartyState(follower, state)
	service.onlinePlayers.EnterArea(&onlinePlayerInfo{CharacterID: 99, TownID: 38, AreaID: 1, Session: leader})
	service.onlinePlayers.EnterArea(&onlinePlayerInfo{CharacterID: 100, TownID: 38, AreaID: 1, Session: follower})
	bindDungeonSelectorOriginForTestAt(t, service, leader, 38, 1, 450, 234)
	bindDungeonSelectorOriginForTestAt(t, service, follower, 38, 1, 450, 234)

	selectBody := make([]byte, dungeoncmd.SelectDungeonRequestSize)
	binary.LittleEndian.PutUint32(selectBody[:4], 700)
	if err := service.handleDungeonSelectUpper(leader, selectBody); err != nil {
		t.Fatal(err)
	}
	if seedCalls != 1 {
		t.Fatalf("party entry independently rolled follower seed calls=%d", seedCalls)
	}
	leaderConn.write.Reset()
	followerConn.write.Reset()
	moveBody := make([]byte, dungeoncmd.MoveMapRequestSize)
	moveBody[0] = 1
	if err := service.handleDungeonMoveMap(leader, moveBody); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name       string
		session    *gameSession
		connection *bufferConn
	}{
		{name: "leader", session: leader, connection: leaderConn},
		{name: "follower", session: follower, connection: followerConn},
	} {
		snapshot := test.session.dungeon.runtime.Session.Snapshot()
		if snapshot.Scene.Coordinate != (worldmap.RoomCoordinate{X: 1, Y: 0}) ||
			snapshot.Scene.Map.Map.ID != 101 || test.session.dungeon.runtime.Seed != 0x1002 {
			t.Fatalf("%s move snapshot=%+v seed=%x", test.name, snapshot, test.session.dungeon.runtime.Seed)
		}
		packet, trailing := splitGameServerUpperPacket(t, test.connection.write.Bytes())
		if packet.Header.Classification != 0 || packet.Header.MsgID != currentDungeonStartNotification || len(trailing) != 0 {
			t.Fatalf("%s move packet=%+v trailing=%x", test.name, packet.Header, trailing)
		}
	}
	if seedCalls != 3 {
		t.Fatalf("room move seed chooser calls=%d want leader+discarded follower=3", seedCalls)
	}
}

func TestPartyDungeonEntryRejectsEveryMemberBeforeCommitWhenFollowerLevelIsTooLow(t *testing.T) {
	source := bridgeDungeonPVF(false)
	source["dungeon/test.dgn"] = strings.Replace(source["dungeon/test.dgn"], "[limit party count]\n1\n", "[limit party count]\n4\n", 1)
	table, resolver, monsters := loadBridgeDungeonStaticData(t, source)
	repositories := dnfrepomemory.NewMemoryGroup()
	for id, level := range map[string]int{"99": 20, "100": 5} {
		if err := repositories.Character.Save(context.Background(), dnfrepo.CharacterRecord{
			CharacterID: id,
			AccountID:   "account-1",
			Level:       level,
			Stats: map[string]int64{
				"fatigue": 100, "town_id": 38, "area_id": 1,
				"pos_x": 450, "pos_y": 234, "direction": 5, "area_state": 3,
			},
		}); err != nil {
			t.Fatal(err)
		}
		if err := repositories.Inventory.Save(context.Background(), dnfrepo.InventoryRecord{CharacterID: id, Slots: map[string]dnfrepo.ItemStack{}}); err != nil {
			t.Fatal(err)
		}
	}
	service := &Service{
		options:             options{accountID: "account-1", gameUpperBodyCodec: gameUpperBodyCodecPlain},
		onlinePlayers:       newOnlinePlayerManager(),
		worldMapTable:       table,
		worldMapResolver:    resolver,
		dungeonMonsterTable: monsters,
		dungeonChoice:       func(int) (int, error) { return 0, nil },
		dungeonSeed:         func() (uint32, error) { return 0x12345678, nil },
		repositoryProvider:  func() (dnfrepo.Group, bool) { return repositories, true },
	}
	channel := channelcatalog.Channel{ServerID: 1, ID: 42, Type: 11, Name: "ch.42", Port: 10042}
	leaderConn, followerConn := &bufferConn{}, &bufferConn{}
	leader := &gameSession{conn: leaderConn, channel: channel, residentChannel: channel, selectedCharacterID: 99}
	follower := &gameSession{conn: followerConn, channel: channel, residentChannel: channel, selectedCharacterID: 100}
	service.bindGameSessionCharacter(leader, 99)
	service.bindGameSessionCharacter(follower, 100)
	state := alignedcmd.PartyState{
		PartyID: 99,
		UserID:  99,
		Members: []alignedcmd.PartyMemberState{
			{UserID: 99, UserState: 1, HPPercent: 100, MPPercent: 100},
			{UserID: 100, UserState: 1, HPPercent: 100, MPPercent: 100},
		},
	}
	storeRuntimePartyState(leader, state)
	storeRuntimePartyState(follower, state)
	service.onlinePlayers.EnterArea(&onlinePlayerInfo{CharacterID: 99, TownID: 38, AreaID: 1, Session: leader})
	service.onlinePlayers.EnterArea(&onlinePlayerInfo{CharacterID: 100, TownID: 38, AreaID: 1, Session: follower})
	bindDungeonSelectorOriginForTestAt(t, service, leader, 38, 1, 450, 234)
	bindDungeonSelectorOriginForTestAt(t, service, follower, 38, 1, 450, 234)

	body := make([]byte, dungeoncmd.SelectDungeonRequestSize)
	binary.LittleEndian.PutUint32(body[:4], 700)
	if err := service.handleDungeonSelectUpper(leader, body); err != nil {
		t.Fatal(err)
	}
	if leader.dungeon.runtime != nil || follower.dungeon.runtime != nil {
		t.Fatalf("party entry partially committed leader_runtime=%p follower_runtime=%p", leader.dungeon.runtime, follower.dungeon.runtime)
	}
	if leaderConn.write.Len() != 0 || followerConn.write.Len() != 0 {
		t.Fatalf("blocked party entry wrote leader=%x follower=%x", leaderConn.write.Bytes(), followerConn.write.Bytes())
	}
	if service.onlinePlayers.SessionForCharacter(99) != leader || service.onlinePlayers.SessionForCharacter(100) != follower {
		t.Fatal("blocked party entry removed a town presence")
	}
}

func assertDungeonPartyFollowerPrerequisitePrecedesRuntime(t *testing.T, stream []byte) {
	t.Helper()
	fatigueIndex, contextIndex, infoIndex, startIndex := -1, -1, -1, -1
	for index := 0; len(stream) > 0; index++ {
		packet, rest := splitGameServerUpperPacket(t, stream)
		if packet.Header.Classification == 0 {
			switch packet.Header.MsgID {
			case currentFatigueMsgID:
				if fatigueIndex < 0 {
					fatigueIndex = index
				}
			case currentDungeonContextMsgID:
				if contextIndex < 0 {
					contextIndex = index
				}
			case currentDungeonInfoNotification:
				if infoIndex < 0 {
					infoIndex = index
				}
			case currentDungeonStartNotification:
				if startIndex < 0 {
					startIndex = index
				}
			}
		}
		stream = rest
	}
	if fatigueIndex < 0 || contextIndex < 0 || infoIndex < 0 || startIndex < 0 ||
		!(fatigueIndex < contextIndex && contextIndex < infoIndex && infoIndex < startIndex) {
		t.Fatalf("party follower sequence fatigue=%d context=%d dungeon_info=%d start=%d", fatigueIndex, contextIndex, infoIndex, startIndex)
	}
}

func assertDungeonPartyFollowerHasNoUnsolicitedSelectAck(t *testing.T, stream []byte) {
	t.Helper()
	for len(stream) > 0 {
		packet, rest := splitGameServerUpperPacket(t, stream)
		if packet.Header.Classification == dnfproto.DefaultChannelClassification &&
			packet.Header.MsgID == uint16(dnfenum.CmdPacketSelectDungeon) {
			t.Fatalf("party follower received unsolicited class1 op16 ack body=%x", packet.Body)
		}
		stream = rest
	}
}

func assertDungeonPartyRealtimeSnapshot(t *testing.T, stream []byte, wantMembers byte) {
	t.Helper()
	foundRealtime := false
	for len(stream) > 0 {
		packet, rest := splitGameServerUpperPacket(t, stream)
		if packet.Header.Classification == 0 && packet.Header.MsgID == 0x0099 && len(packet.Body) > 0 && packet.Body[0] == wantMembers {
			if len(packet.Body) != 1+int(wantMembers)*5 {
				t.Fatalf("dungeon party realtime body_len=%d want=%d body=%x", len(packet.Body), 1+int(wantMembers)*5, packet.Body)
			}
			for slot := byte(0); slot < wantMembers; slot++ {
				offset := 1 + int(slot)*5
				if packet.Body[offset+2] == 0 || packet.Body[offset+4] != slot {
					t.Fatalf("dungeon party realtime slot=%d row=%x", slot, packet.Body[offset:offset+5])
				}
			}
			foundRealtime = true
		}
		stream = rest
	}
	if !foundRealtime {
		t.Fatalf("dungeon party snapshot realtime=%t want_members=%d", foundRealtime, wantMembers)
	}
}

func assertDungeonPartyFrameCount(t *testing.T, stream []byte, characterID uint16, wantCount int) {
	t.Helper()
	wantObjectKey := currentSceneActorObjectKey(characterID)
	count := 0
	for len(stream) > 0 {
		packet, rest := splitGameServerUpperPacket(t, stream)
		if packet.Header.Classification == 0 && packet.Header.MsgID == uint16(dnfenum.CmdPacketRecoverStamina) &&
			len(packet.Body) >= 10 && packet.Body[9] == currentSceneOp9ActorDisplayKind &&
			binary.LittleEndian.Uint16(packet.Body[4:6]) == wantObjectKey {
			count++
		}
		stream = rest
	}
	if count != wantCount {
		t.Fatalf("dungeon local party frame count=%d want=%d object_key=%d", count, wantCount, wantObjectKey)
	}
}

func assertDungeonStartMapPartyIndex(t *testing.T, stream []byte, want byte) {
	t.Helper()
	for len(stream) > 0 {
		packet, rest := splitGameServerUpperPacket(t, stream)
		if packet.Header.Classification == 0 && packet.Header.MsgID == currentDungeonStartNotification {
			if len(packet.Body) == 0 || packet.Body[len(packet.Body)-1] != want {
				t.Fatalf("op29 party index body_tail=%x want=%d", packet.Body, want)
			}
			return
		}
		stream = rest
	}
	t.Fatal("op29 start-map packet not found")
}

func TestBuildCurrentDungeonUserStateBodyUsesDynamicObjectKey(t *testing.T) {
	body, err := buildCurrentDungeonUserStateBody(19)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, []byte{1, 0x13, 0x00, currentDungeonPlayerUserState}) {
		t.Fatalf("user-state body=%x", body)
	}
	if _, err := buildCurrentDungeonUserStateBody(0); err == nil {
		t.Fatal("zero object key accepted")
	}
}

func TestBuildCurrentDungeonEntryPacketsUsesRuntimeAndPVFOwners(t *testing.T) {
	coordinate := worldmap.RoomCoordinate{X: 2, Y: 3}
	spawn := worldmap.MonsterSpawn{MonsterID: 3001, AutoLevel: 20, Rank: "[normal]"}
	room := &runtimeDungeonRoom{
		coordinate: coordinate,
		mapID:      70000,
		monsters: []runtimeDungeonMonster{{
			ObjectKey: 402,
			Reference: worldmap.HostileReference{Kind: worldmap.HostileMonster, Index: 0},
			Spawn:     spawn,
			State:     runtimeDungeonMonsterPlanned,
		}},
		extendedActors: []runtimeDungeonExtendedActor{{
			Kind:      runtimeDungeonActorAICharacter,
			ObjectKey: 403,
			Packet: currentDungeonStartMapActor{
				ObjectKey: 403, Code: 4001, Level: 21, Type: 5, Blocking: 0,
			},
			State: runtimeDungeonMonsterPlanned,
		}},
		aiCharacterCount: 1,
	}
	runtime := &runtimeDungeonState{
		Request:        dungeoncmd.SelectDungeonRequest{DungeonID: 900, Difficulty: 2},
		Dungeon:        worldmap.Dungeon{ID: 900, Mazes: []worldmap.Maze{{}}},
		MazeIndex:      0,
		Session:        &worldmap.DungeonSession{},
		Room:           room,
		BossCoordinate: worldmap.RoomCoordinate{X: 8, Y: 9},
		BossSet:        true,
		Seed:           0x12345678,
	}
	scene := worldmap.DungeonRoomScene{
		Coordinate:   coordinate,
		Map:          worldmap.ResolvedMap{Map: worldmap.Map{ID: 70000}},
		Monsters:     []worldmap.MonsterSpawn{spawn},
		AICharacters: []worldmap.AICharacter{{Code: 4001, Faction: "[character]", AIType: "[normal]"}},
	}
	packets, err := buildCurrentDungeonEntryPackets(runtime, scene)
	if err != nil {
		t.Fatal(err)
	}
	if len(packets.DungeonInfo) != 36 {
		t.Fatalf("dungeon-info len=%d body=%x", len(packets.DungeonInfo), packets.DungeonInfo)
	}
	if got := binary.LittleEndian.Uint32(packets.DungeonInfo[0:4]); got != 900 {
		t.Fatalf("dungeon id=%d", got)
	}
	if packets.DungeonInfo[4] != 2 || packets.DungeonInfo[7] != 0 || packets.DungeonInfo[8] != 8 || packets.DungeonInfo[9] != 9 {
		t.Fatalf("dungeon-info runtime fields=%x", packets.DungeonInfo[:10])
	}
	if packets.DungeonInfo[10] != 0xff || packets.DungeonInfo[11] != 0xff || binary.LittleEndian.Uint32(packets.DungeonInfo[20:24]) != ^uint32(0) {
		t.Fatalf("dungeon-info ordinary sentinels=%x", packets.DungeonInfo)
	}
	if len(packets.StartMap) != 65 {
		t.Fatalf("start-map len=%d body=%x", len(packets.StartMap), packets.StartMap)
	}
	if packets.StartMap[0] != 2 || packets.StartMap[1] != 3 || binary.LittleEndian.Uint32(packets.StartMap[3:7]) != 0x12345678 {
		t.Fatalf("start-map room/seed=%x", packets.StartMap[:7])
	}
	if got := binary.LittleEndian.Uint32(packets.StartMap[14:18]); got != 70000 || packets.StartMap[18] != 2 {
		t.Fatalf("start-map map/count map=%d count=%d", got, packets.StartMap[18])
	}
	if binary.LittleEndian.Uint16(packets.StartMap[25:27]) != 402 || binary.LittleEndian.Uint32(packets.StartMap[27:31]) != 3001 || packets.StartMap[39] != 0 {
		t.Fatalf("normal actor row=%x", packets.StartMap[19:40])
	}
	if binary.LittleEndian.Uint16(packets.StartMap[46:48]) != 403 || binary.LittleEndian.Uint32(packets.StartMap[48:52]) != 4001 || packets.StartMap[53] != 5 || packets.StartMap[60] != 0 {
		t.Fatalf("APC actor row=%x", packets.StartMap[40:61])
	}
}

func TestBuildCurrentDungeonEntryPacketsRejectsUnownedState(t *testing.T) {
	base := func() (*runtimeDungeonState, worldmap.DungeonRoomScene) {
		coordinate := worldmap.RoomCoordinate{X: 0, Y: 0}
		room := &runtimeDungeonRoom{coordinate: coordinate, mapID: 100}
		return &runtimeDungeonState{
				Request:        dungeoncmd.SelectDungeonRequest{DungeonID: 700},
				Dungeon:        worldmap.Dungeon{ID: 700, Mazes: []worldmap.Maze{{}}},
				Session:        &worldmap.DungeonSession{},
				Room:           room,
				BossCoordinate: coordinate,
				BossSet:        true,
			}, worldmap.DungeonRoomScene{
				Coordinate: coordinate,
				Map:        worldmap.ResolvedMap{Map: worldmap.Map{ID: 100}},
			}
	}
	tests := []struct {
		name   string
		mutate func(*runtimeDungeonState, *worldmap.DungeonRoomScene)
		want   error
	}{
		{name: "request mode", want: errDungeonEntryModeUnsupported, mutate: func(runtime *runtimeDungeonState, _ *worldmap.DungeonRoomScene) { runtime.Request.SpecialMode = 1 }},
		{name: "boss missing", want: errDungeonBossCoordinateRequired, mutate: func(runtime *runtimeDungeonState, _ *worldmap.DungeonRoomScene) { runtime.BossSet = false }},
		{name: "boss range", want: errDungeonBossCoordinateRange, mutate: func(runtime *runtimeDungeonState, _ *worldmap.DungeonRoomScene) { runtime.BossCoordinate.X = 256 }},
		{name: "randomized object", want: errDungeonRandomizedObjectOwner, mutate: func(runtime *runtimeDungeonState, _ *worldmap.DungeonRoomScene) {
			runtime.Dungeon.Mazes[0].RandomizedObjects = []worldmap.RandomizedObjectScript{{}}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtime, scene := base()
			test.mutate(runtime, &scene)
			_, err := buildCurrentDungeonEntryPackets(runtime, scene)
			if !errors.Is(err, test.want) {
				t.Fatalf("error=%v want=%v", err, test.want)
			}
		})
	}
}

func TestValidateCurrentDungeonEntryRequestAcceptsCapturedTrainingRoomModeOnly(t *testing.T) {
	request := dungeoncmd.SelectDungeonRequest{
		DungeonID:    5000,
		RuntimeState: 1,
		RuntimeToken: 0xffff,
	}
	if err := validateCurrentDungeonEntryRequest(request); err != nil {
		t.Fatalf("captured training-room mode rejected: %v", err)
	}

	request.DungeonID = 4999
	if err := validateCurrentDungeonEntryRequest(request); !errors.Is(err, errDungeonEntryModeUnsupported) {
		t.Fatalf("ordinary dungeon runtime state 1 error=%v", err)
	}
	request.DungeonID = 5000
	request.RuntimeToken = 0
	if err := validateCurrentDungeonEntryRequest(request); !errors.Is(err, errDungeonEntryModeUnsupported) {
		t.Fatalf("training-room runtime state 1 without captured token error=%v", err)
	}
}

func TestBuildCurrentDungeonEntryPacketsKeepsSpecialSpawnMetadataOutOfActorAndDropTables(t *testing.T) {
	coordinate := worldmap.RoomCoordinate{X: 0, Y: 0}
	specialObject := worldmap.SpecialPassiveObject{
		PassiveObject: worldmap.PassiveObject{ObjectID: 109006908},
		Spawns: []worldmap.SpecialObjectSpawn{
			{Kind: "[item]", Code: 1001},
			{Kind: "[trap]", Code: 1002},
		},
	}
	room := &runtimeDungeonRoom{
		coordinate: coordinate,
		mapID:      76191,
		extendedActors: []runtimeDungeonExtendedActor{{
			Kind:      runtimeDungeonActorSpecialObject,
			ObjectKey: 402,
			Packet: currentDungeonStartMapActor{
				PacketIndex: 0,
				ObjectKey:   402,
				Code:        uint32(specialObject.ObjectID),
				Type:        9,
			},
			State: runtimeDungeonMonsterPlanned,
		}},
		retainedSpecialSpawns: []runtimeDungeonRetainedSpecialSpawn{
			{ObjectIndex: 0, SpawnIndex: 0, Spawn: specialObject.Spawns[0]},
			{ObjectIndex: 0, SpawnIndex: 1, Spawn: specialObject.Spawns[1]},
		},
		specialObjectCount: 1,
	}
	runtime := &runtimeDungeonState{
		Request:        dungeoncmd.SelectDungeonRequest{DungeonID: 1000},
		Dungeon:        worldmap.Dungeon{ID: 1000, Mazes: []worldmap.Maze{{}}},
		MazeIndex:      0,
		Session:        &worldmap.DungeonSession{},
		Room:           room,
		BossCoordinate: coordinate,
		BossSet:        true,
		Seed:           1,
	}
	scene := worldmap.DungeonRoomScene{
		Coordinate:            coordinate,
		Map:                   worldmap.ResolvedMap{Map: worldmap.Map{ID: 76191}},
		SpecialPassiveObjects: []worldmap.SpecialPassiveObject{specialObject},
	}

	packets, err := buildCurrentDungeonEntryPackets(runtime, scene)
	if err != nil {
		t.Fatal(err)
	}
	if len(packets.DungeonInfo) != 36 {
		t.Fatalf("dungeon-info len=%d body=%x", len(packets.DungeonInfo), packets.DungeonInfo)
	}
	if len(packets.StartMap) != 44 {
		t.Fatalf("start-map len=%d body=%x", len(packets.StartMap), packets.StartMap)
	}
	if packets.StartMap[18] != 1 {
		t.Fatalf("actor count=%d body=%x", packets.StartMap[18], packets.StartMap)
	}
	if got := binary.LittleEndian.Uint32(packets.StartMap[27:31]); got != uint32(specialObject.ObjectID) || packets.StartMap[32] != 9 {
		t.Fatalf("special object code=%d type=%d body=%x", got, packets.StartMap[32], packets.StartMap)
	}
	if packets.StartMap[40] != 0 {
		t.Fatalf("metadata child rows were invented as materialized drops: count=%d body=%x", packets.StartMap[40], packets.StartMap)
	}
}

func TestRuntimeDungeonRoomAnnounceAllActorsCommitsAfterPreflight(t *testing.T) {
	runtime := prepareSyntheticDungeonRuntimeForEntryTest(t, bridgeDungeonPVF(false), nil)
	count, err := runtime.Room.AnnounceAllActors(runtime.Session)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("announced count=%d", count)
	}
	snapshot := runtime.Room.Snapshot()
	if snapshot.Monsters[0].State != runtimeDungeonMonsterAnnounced {
		t.Fatalf("monster state=%s", snapshot.Monsters[0].State)
	}
	scene, _ := runtime.Session.Scene()
	if got := scene.RuntimeObjects[402]; got != (worldmap.HostileReference{Kind: worldmap.HostileMonster, Index: 0}) {
		t.Fatalf("runtime binding=%+v", got)
	}
	if _, err := runtime.Room.AnnounceAllActors(runtime.Session); !errors.Is(err, errDungeonActorAlreadyAnnounced) {
		t.Fatalf("duplicate announce error=%v", err)
	}
}

func TestRuntimeDungeonRoomAnnounceAllActorsIncludesHostileAPC(t *testing.T) {
	source := bridgeDungeonPVF(false)
	source["map/dungeon/test/start.map"] += "[ai character]\n4001 10 20 0 `[monster]` `[boss]` 0 0\n"
	source[defaultDungeonAICharacterList] = "4001 `Test/Enemy.aic`\n"
	source["AICharacter/Test/Enemy.aic"] = "[minimum info]\n`Enemy APC` 1 2 3 4 25\n"
	aiCatalog, err := newPVFDungeonAICharacterCatalog(source)
	if err != nil {
		t.Fatal(err)
	}
	runtime := prepareSyntheticDungeonRuntimeForEntryTest(t, source, aiCatalog)
	if runtime.NextObjectKey != 404 {
		t.Fatalf("next object key=%d", runtime.NextObjectKey)
	}
	count, err := runtime.Room.AnnounceAllActors(runtime.Session)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("announced count=%d", count)
	}
	snapshot := runtime.Room.Snapshot()
	if len(snapshot.ExtendedActors) != 1 || snapshot.ExtendedActors[0].State != runtimeDungeonMonsterAnnounced || snapshot.ExtendedActors[0].ObjectKey != 403 {
		t.Fatalf("extended actors=%+v", snapshot.ExtendedActors)
	}
	scene, _ := runtime.Session.Scene()
	if got := scene.RuntimeObjects[403]; got != (worldmap.HostileReference{Kind: worldmap.HostileAICharacter, Index: 0}) {
		t.Fatalf("APC runtime binding=%+v", got)
	}
}

func TestHandleDungeonMonsterDeathSupportsHostileAPCAndClearsLastHostile(t *testing.T) {
	source := bridgeDungeonPVF(false)
	source["map/dungeon/test/start.map"] += "[ai character]\n4001 10 20 0 `[monster]` `[boss]` 0 0\n4002 30 20 0 `[character]` `[normal]` 0 0\n"
	source[defaultDungeonAICharacterList] = "4001 `Test/Enemy.aic`\n4002 `Test/Friend.aic`\n"
	source["AICharacter/Test/Enemy.aic"] = "[minimum info]\n`Enemy APC` 1 2 3 4 25\n"
	source["AICharacter/Test/Friend.aic"] = "[minimum info]\n`Friend APC` 1 2 3 4 25\n"
	aiCatalog, err := newPVFDungeonAICharacterCatalog(source)
	if err != nil {
		t.Fatal(err)
	}
	runtime := prepareSyntheticDungeonRuntimeForEntryTest(t, source, aiCatalog)
	if _, err := runtime.Room.AnnounceAllActors(runtime.Session); err != nil {
		t.Fatal(err)
	}

	conn := &bufferConn{}
	service := &Service{options: options{gameUpperBodyCodec: gameUpperBodyCodecPlain}}
	session := &gameSession{conn: conn, connID: "dungeon-death-ai-test", dungeon: dungeonSessionState{runtime: runtime}}

	aiObjectKey := uint32(403)
	aiDeathBody, err := hex.DecodeString("9301000094010000000000000000910d0000130000000211019401bf0b00000d001101130094010000060064024d01000000001e3c0001000000430d00000000000064024d0164024d0112002f0003000000300280d6")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.handleDungeonMonsterDeath(session, aiDeathBody); err != nil {
		t.Fatal(err)
	}
	aiPacket, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	wantAI, err := buildCurrentDungeonDeathNotificationBody(
		aiObjectKey,
		currentDungeonDeathResponseAICharacter,
	)
	if err != nil {
		t.Fatal(err)
	}
	if aiPacket.Header.MsgID != uint16(dnfenum.CmdPacketNotifyDieMonster) ||
		aiPacket.Header.Classification != 0 ||
		!bytes.Equal(aiPacket.Body, wantAI) {
		t.Fatalf("AI death notification = header=%+v body=%x rest=%x", aiPacket.Header, aiPacket.Body, rest)
	}
	forcedPacket, rest := splitGameServerUpperPacket(t, rest)
	wantForced, err := buildCurrentDungeonDeathNotificationBody(
		402,
		currentDungeonDeathResponseMonster,
	)
	if err != nil {
		t.Fatal(err)
	}
	if forcedPacket.Header.MsgID != uint16(dnfenum.CmdPacketNotifyDieMonster) ||
		forcedPacket.Header.Classification != 0 ||
		!bytes.Equal(forcedPacket.Body, wantForced) ||
		len(rest) != 0 {
		t.Fatalf("AI boss forced-clear packet = header=%+v body=%x rest=%x", forcedPacket.Header, forcedPacket.Body, rest)
	}
	snapshot := runtime.Room.Snapshot()
	scene, _ := runtime.Session.Scene()
	if snapshot.ExtendedActors[0].State != runtimeDungeonMonsterDefeated ||
		snapshot.Monsters[0].State != runtimeDungeonMonsterDefeated ||
		!scene.Cleared {
		t.Fatalf("APC death state = room=%+v scene=%+v", snapshot, scene)
	}

	conn.write.Reset()
	fixedSentinelBody := make([]byte, dungeoncmd.DieMonsterRequestSize)
	binary.LittleEndian.PutUint32(fixedSentinelBody[0:4], 404)
	binary.LittleEndian.PutUint16(fixedSentinelBody[4:6], ^uint16(0))
	if err := service.handleDungeonMonsterDeath(session, fixedSentinelBody); err != nil {
		t.Fatal(err)
	}
	if conn.write.Len() != 0 || runtime.Room.Snapshot().ExtendedActors[1].State != runtimeDungeonMonsterAnnounced {
		t.Fatalf("fixed62 owner=ffff was not rejected: writes=%d room=%+v", conn.write.Len(), runtime.Room.Snapshot())
	}

	variableCombatSentinelBody := make([]byte, dungeoncmd.DieMonsterVariableBaseSize+dungeoncmd.DieMonsterVariableCombatEntrySize)
	binary.LittleEndian.PutUint32(variableCombatSentinelBody[0:4], 404)
	binary.LittleEndian.PutUint16(variableCombatSentinelBody[4:6], ^uint16(0))
	variableCombatSentinelBody[22] = 1
	if err := service.handleDungeonMonsterDeath(session, variableCombatSentinelBody); err != nil {
		t.Fatal(err)
	}
	if conn.write.Len() != 0 || runtime.Room.Snapshot().ExtendedActors[1].State != runtimeDungeonMonsterAnnounced {
		t.Fatalf("variable count=1 owner=ffff was not rejected: writes=%d room=%+v", conn.write.Len(), runtime.Room.Snapshot())
	}

	conn.write.Reset()
	nonHostileDeathBody := make([]byte, dungeoncmd.DieMonsterVariableBaseSize)
	binary.LittleEndian.PutUint32(nonHostileDeathBody[0:4], 404)
	binary.LittleEndian.PutUint16(nonHostileDeathBody[4:6], ^uint16(0))
	if err := service.handleDungeonMonsterDeath(session, nonHostileDeathBody); err != nil {
		t.Fatal(err)
	}
	nonHostilePacket, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	wantNonHostile, err := buildCurrentDungeonDeathNotificationBody(
		404,
		currentDungeonDeathResponseAICharacter,
	)
	if err != nil {
		t.Fatal(err)
	}
	if nonHostilePacket.Header.MsgID != uint16(dnfenum.CmdPacketNotifyDieMonster) ||
		nonHostilePacket.Header.Classification != 0 ||
		!bytes.Equal(nonHostilePacket.Body, wantNonHostile) ||
		len(rest) != 0 {
		t.Fatalf("non-hostile AI retirement notification = header=%+v body=%x rest=%x", nonHostilePacket.Header, nonHostilePacket.Body, rest)
	}
	snapshot = runtime.Room.Snapshot()
	scene, _ = runtime.Session.Scene()
	if snapshot.ExtendedActors[1].State != runtimeDungeonMonsterDefeated || !scene.Cleared {
		t.Fatalf("non-hostile AI retirement changed completed clear state = room=%+v scene=%+v", snapshot, scene)
	}

	conn.write.Reset()
	if err := service.handleDungeonMonsterDeath(session, nonHostileDeathBody); err != nil {
		t.Fatal(err)
	}
	duplicateNonHostilePacket, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if duplicateNonHostilePacket.Header.MsgID != uint16(dnfenum.CmdPacketNotifyDieMonster) ||
		duplicateNonHostilePacket.Header.Classification != 0 ||
		!bytes.Equal(duplicateNonHostilePacket.Body, wantNonHostile) ||
		len(rest) != 0 {
		t.Fatalf("duplicate non-hostile AI retirement notification = header=%+v body=%x rest=%x", duplicateNonHostilePacket.Header, duplicateNonHostilePacket.Body, rest)
	}
	snapshot = runtime.Room.Snapshot()
	scene, _ = runtime.Session.Scene()
	if snapshot.ExtendedActors[1].State != runtimeDungeonMonsterDefeated || !scene.Cleared {
		t.Fatalf("duplicate non-hostile AI retirement changed completed clear state = room=%+v scene=%+v", snapshot, scene)
	}

	conn.write.Reset()
	monsterObjectKey := uint32(firstDungeonMonsterObjectKey)
	monsterDeathBody := make([]byte, dungeoncmd.DieMonsterRequestSize)
	binary.LittleEndian.PutUint32(monsterDeathBody[0:4], monsterObjectKey)
	binary.LittleEndian.PutUint16(monsterDeathBody[4:6], currentSceneBootstrapObjectKey)
	if err := service.handleDungeonMonsterDeath(session, monsterDeathBody); err != nil {
		t.Fatal(err)
	}
	if conn.write.Len() != 0 {
		t.Fatalf("forced-cleared monster death replay wrote=%x", conn.write.Bytes())
	}
	snapshot = runtime.Room.Snapshot()
	scene, _ = runtime.Session.Scene()
	if snapshot.Monsters[0].State != runtimeDungeonMonsterDefeated || !scene.Cleared {
		t.Fatalf("last hostile death state = room=%+v scene=%+v", snapshot, scene)
	}
}

func TestHandleDungeonMonsterDeathRetiresSpecialMonsterVisualActor(t *testing.T) {
	source := bridgeDungeonPVF(false)
	source["map/dungeon/test/start.map"] += "[special passive object]\n7001 30 20 0 1 `[monster]` 3001 20 0 0 0\n"
	runtime := prepareSyntheticDungeonRuntimeForEntryTest(t, source, nil)
	count, err := runtime.Room.AnnounceAllActors(runtime.Session)
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("announced count=%d", count)
	}
	snapshot := runtime.Room.Snapshot()
	if len(snapshot.ExtendedActors) != 2 ||
		snapshot.ExtendedActors[0].Kind != runtimeDungeonActorSpecialObject ||
		snapshot.ExtendedActors[0].ObjectKey != 403 ||
		snapshot.ExtendedActors[1].Kind != runtimeDungeonActorSpecialMonster ||
		snapshot.ExtendedActors[1].ObjectKey != 404 ||
		snapshot.ExtendedActors[1].HostileReference != nil ||
		snapshot.ExtendedActors[1].State != runtimeDungeonMonsterAnnounced {
		t.Fatalf("special actors=%+v", snapshot.ExtendedActors)
	}
	scene, _ := runtime.Session.Scene()
	if got := scene.RuntimeObjects[402]; got != (worldmap.HostileReference{Kind: worldmap.HostileMonster, Index: 0}) {
		t.Fatalf("ordinary monster binding=%+v", got)
	}
	if _, bound := scene.RuntimeObjects[404]; bound {
		t.Fatalf("special monster was bound as a room owner: %+v", scene.RuntimeObjects)
	}

	conn := &bufferConn{}
	service := &Service{options: options{gameUpperBodyCodec: gameUpperBodyCodecPlain}}
	session := &gameSession{conn: conn, connID: "dungeon-special-monster-test", dungeon: dungeonSessionState{runtime: runtime}}

	specialDeathBody := make([]byte, dungeoncmd.DieMonsterVariableBaseSize+dungeoncmd.DieMonsterVariableCombatEntrySize)
	binary.LittleEndian.PutUint32(specialDeathBody[0:4], 404)
	binary.LittleEndian.PutUint16(specialDeathBody[4:6], currentSceneBootstrapObjectKey)
	specialDeathBody[22] = 1
	if err := service.handleDungeonMonsterDeath(session, specialDeathBody); err != nil {
		t.Fatal(err)
	}
	packet, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	want, err := buildCurrentDungeonDeathNotificationBody(
		404,
		currentDungeonDeathResponseMonster,
	)
	if err != nil {
		t.Fatal(err)
	}
	if packet.Header.MsgID != uint16(dnfenum.CmdPacketNotifyDieMonster) ||
		packet.Header.Classification != 0 ||
		!bytes.Equal(packet.Body, want) ||
		len(rest) != 0 {
		t.Fatalf("special monster retirement packet = header=%+v body=%x rest=%x", packet.Header, packet.Body, rest)
	}
	snapshot = runtime.Room.Snapshot()
	scene, _ = runtime.Session.Scene()
	if snapshot.ExtendedActors[1].State != runtimeDungeonMonsterDefeated ||
		snapshot.Monsters[0].State != runtimeDungeonMonsterAnnounced ||
		scene.Cleared {
		t.Fatalf("special monster retirement changed owner state = room=%+v scene=%+v", snapshot, scene)
	}

	conn.write.Reset()
	if err := service.handleDungeonMonsterDeath(session, specialDeathBody); err != nil {
		t.Fatal(err)
	}
	duplicate, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if duplicate.Header.MsgID != uint16(dnfenum.CmdPacketNotifyDieMonster) ||
		duplicate.Header.Classification != 0 ||
		!bytes.Equal(duplicate.Body, want) ||
		len(rest) != 0 {
		t.Fatalf("duplicate special monster retirement packet = header=%+v body=%x rest=%x", duplicate.Header, duplicate.Body, rest)
	}
	snapshot = runtime.Room.Snapshot()
	scene, _ = runtime.Session.Scene()
	if snapshot.ExtendedActors[1].State != runtimeDungeonMonsterDefeated ||
		snapshot.Monsters[0].State != runtimeDungeonMonsterAnnounced ||
		scene.Cleared {
		t.Fatalf("duplicate special monster retirement changed owner state = room=%+v scene=%+v", snapshot, scene)
	}
}

func TestRuntimeDungeonRoomAnnounceAllActorsRejectsConflictWithoutStateAdvance(t *testing.T) {
	runtime := prepareSyntheticDungeonRuntimeForEntryTest(t, bridgeDungeonPVF(false), nil)
	reference := worldmap.HostileReference{Kind: worldmap.HostileMonster, Index: 0}
	if err := runtime.Session.BindHostileObject(reference, 999); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Room.AnnounceAllActors(runtime.Session); !errors.Is(err, errDungeonActorOwnerConflict) {
		t.Fatalf("conflict error=%v", err)
	}
	if got := runtime.Room.Snapshot().Monsters[0].State; got != runtimeDungeonMonsterPlanned {
		t.Fatalf("monster advanced to %s", got)
	}
}

func TestHandleDungeonSelectUpperDefersEntryPacketValidationUntilTypedScenePhases(t *testing.T) {
	table, resolver, monsters := loadBridgeDungeonStaticData(t, bridgeDungeonPVF(false))
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(context.Background(), dnfrepo.CharacterRecord{
		CharacterID: "99",
		AccountID:   "account-1",
		Level:       20,
		Stats: map[string]int64{
			"fatigue": 100, "town_id": 38, "area_id": 1,
			"pos_x": 450, "pos_y": 234, "direction": 5, "area_state": 3,
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(context.Background(), dnfrepo.InventoryRecord{CharacterID: "99", Slots: map[string]dnfrepo.ItemStack{}}); err != nil {
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
	connection := &bufferConn{}
	channel := channelcatalog.Channel{ServerID: 1, ID: 253, Type: 40, Name: "ch.253", Port: 10253}
	session := &gameSession{
		conn:                            connection,
		channel:                         channel,
		residentChannel:                 channel,
		selectedCharacterID:             99,
		connectionTownActorOwnerChannel: byte(channel.ID),
		townActorOwnerChannel:           byte(channel.ID),
	}
	bindDungeonSelectorOriginForTestAt(t, service, session, 38, 1, 450, 234)
	body := make([]byte, 21)
	binary.LittleEndian.PutUint32(body[0:4], 700)
	binary.LittleEndian.PutUint16(body[9:11], 0xffff)
	binary.LittleEndian.PutUint32(body[16:20], 3145)
	if err := service.handleDungeonSelectUpper(session, body); err != nil {
		t.Fatal(err)
	}
	packet, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if packet.Header.MsgID != uint16(dnfenum.CmdPacketSelectDungeon) {
		t.Fatalf("select ACK packet=%+v rest=%x", packet.Header, rest)
	}
	resourcePacket, rest := splitGameServerUpperPacket(t, rest)
	if resourcePacket.Header.Classification != 0 || resourcePacket.Header.MsgID != currentDungeonResourceStateMsgID || !bytes.Equal(resourcePacket.Body, []byte{1, 0, 0xbc, 2, 0, 0, currentDungeonResourceSelectedState}) {
		t.Fatalf("op5 packet=%+v body=%x rest=%x", resourcePacket.Header, resourcePacket.Body, rest)
	}
	rest = assertCurrentPreDungeonContextPlayerState(t, session, rest)
	contextPacket, rest := splitGameServerUpperPacket(t, rest)
	if contextPacket.Header.Classification != 0 || contextPacket.Header.MsgID != currentDungeonContextMsgID || len(contextPacket.Body) != 37 {
		t.Fatalf("op27 packet=%+v body_len=%d rest=%x", contextPacket.Header, len(contextPacket.Body), rest)
	}
	infoPacket, rest := splitGameServerUpperPacket(t, rest)
	if infoPacket.Header.Classification != 0 || infoPacket.Header.MsgID != currentDungeonInfoNotification || len(infoPacket.Body) != 36 {
		t.Fatalf("op28 packet=%+v body_len=%d rest=%x", infoPacket.Header, len(infoPacket.Body), rest)
	}
	startPacket, rest := splitGameServerUpperPacket(t, rest)
	if startPacket.Header.Classification != 0 || startPacket.Header.MsgID != currentDungeonStartNotification || len(startPacket.Body) < 23 {
		t.Fatalf("op29 packet=%+v body_len=%d rest=%x", startPacket.Header, len(startPacket.Body), rest)
	}
	rest = assertCurrentPostStartMapPlayerState(t, session, rest, true, true)
	if len(rest) != 0 {
		t.Fatalf("post-op29 trailing bytes=%x", rest)
	}
	if !session.preDungeonContextPlayerStateSent || !session.postStartMapPlayerStateSent || !session.sceneBootstrapTailSent || session.sceneBootstrapTailDeferred {
		t.Fatalf("scene flags pre=%v placed=%v tail=%v deferred=%v", session.preDungeonContextPlayerStateSent, session.postStartMapPlayerStateSent, session.sceneBootstrapTailSent, session.sceneBootstrapTailDeferred)
	}
	if session.dungeon.runtime == nil || session.dungeon.runtime.Request.RuntimeToken != 0xffff || session.dungeon.runtime.Request.LeaderObjectKey != 3145 {
		t.Fatalf("runtime request=%+v", session.dungeon.runtime)
	}
}

func TestHandleDungeonSelectUpperCommitsStartMapBeforePostPlayerState(t *testing.T) {
	table, resolver, monsters := loadBridgeDungeonStaticData(t, bridgeDungeonPVF(false))
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(context.Background(), dnfrepo.CharacterRecord{
		CharacterID: "99",
		AccountID:   "account-1",
		Level:       20,
		Stats: map[string]int64{
			"fatigue": 100, "town_id": 38, "area_id": 1,
			"pos_x": 450, "pos_y": 234, "direction": 5, "area_state": 3,
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(context.Background(), dnfrepo.InventoryRecord{CharacterID: "99", Slots: map[string]dnfrepo.ItemStack{}}); err != nil {
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
	connection := &failNthDungeonWriteConn{failAt: 22, err: errors.New("unexpected twenty-second write")}
	session := &gameSession{conn: connection, selectedCharacterID: 99}
	bindDungeonSelectorOriginForTestAt(t, service, session, 38, 1, 450, 234)
	body := make([]byte, 21)
	binary.LittleEndian.PutUint32(body[0:4], 700)
	if err := service.handleDungeonSelectUpper(session, body); err == nil || !strings.Contains(err.Error(), "unexpected twenty-second write") {
		t.Fatalf("handler error=%v, want post-op29 placement failure", err)
	}
	if connection.writes != 22 {
		t.Fatalf("write calls=%d", connection.writes)
	}
	if session.dungeon.runtime == nil {
		t.Fatal("runtime reservation missing after select-dungeon ACK")
	}
	snapshot := session.dungeon.runtime.Room.Snapshot()
	if snapshot.Monsters[0].State != runtimeDungeonMonsterAnnounced {
		t.Fatalf("monster state after start-map notification: %s", snapshot.Monsters[0].State)
	}
	scene, _ := session.dungeon.runtime.Session.Scene()
	if len(scene.RuntimeObjects) != 1 {
		t.Fatalf("runtime objects after start-map notification: %+v", scene.RuntimeObjects)
	}
	if !session.preDungeonContextPlayerStateSent || session.postStartMapPlayerStateSent || !session.selectedUserInfoRefreshSent || session.selectedUserInfoMode3Sent {
		t.Fatal("post-start-map placement state committed incorrectly after its final write failed")
	}
}

func TestHandleDungeonSelectUpperStopsBeforeOp27WhenLifecycleMode1WriteFails(t *testing.T) {
	table, resolver, monsters := loadBridgeDungeonStaticData(t, bridgeDungeonPVF(false))
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(context.Background(), dnfrepo.CharacterRecord{
		CharacterID: "99",
		AccountID:   "account-1",
		Level:       20,
		Stats: map[string]int64{
			"fatigue": 100, "town_id": 38, "area_id": 1,
			"pos_x": 450, "pos_y": 234, "direction": 5, "area_state": 3,
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(context.Background(), dnfrepo.InventoryRecord{CharacterID: "99", Slots: map[string]dnfrepo.ItemStack{}}); err != nil {
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
	connection := &failNthDungeonWriteConn{failAt: 4, err: errors.New("actor-binding mode1 write failed")}
	session := &gameSession{conn: connection, selectedCharacterID: 99}
	bindDungeonSelectorOriginForTestAt(t, service, session, 38, 1, 450, 234)
	body := make([]byte, 21)
	binary.LittleEndian.PutUint32(body[0:4], 700)
	if err := service.handleDungeonSelectUpper(session, body); err == nil || !strings.Contains(err.Error(), "actor-binding mode1 write failed") {
		t.Fatalf("handler error=%v", err)
	}
	_, rest := splitGameServerUpperPacket(t, connection.write.Bytes()) // op16 ACK
	_, rest = splitGameServerUpperPacket(t, rest)                      // op5
	mode0, rest := splitGameServerUpperPacket(t, rest)
	if mode0.Header.MsgID != uint16(dnfenum.CmdPacketSetUDPIPPort) || len(mode0.Body) == 0 || mode0.Body[0] != 0 || len(rest) != 0 {
		t.Fatalf("writes before actor-binding mode1 failure mode0=%+v body=%x rest=%x", mode0.Header, mode0.Body, rest)
	}
	if session.preDungeonContextPlayerStateSent || session.postStartMapPlayerStateSent {
		t.Fatalf("failed actor-binding stage committed flags pre=%v post=%v", session.preDungeonContextPlayerStateSent, session.postStartMapPlayerStateSent)
	}
}

func TestSendCurrentPreDungeonContextPlayerStateIsOneShot(t *testing.T) {
	repositories := dnfrepomemory.NewMemoryGroup()
	character := dnfrepo.CharacterRecord{
		CharacterID: "77",
		AccountID:   "account-1",
		Name:        "hero",
		Job:         "11",
		Level:       1,
	}
	if err := repositories.Character.Save(context.Background(), character); err != nil {
		t.Fatal(err)
	}
	service := &Service{
		options:            options{accountID: "account-1", gameUpperBodyCodec: gameUpperBodyCodecPlain},
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	connection := &bufferConn{}
	channel := channelcatalog.Channel{ServerID: 1, ID: 16, Type: 1, Name: "ch.16", Port: 10016}
	session := &gameSession{
		conn:                connection,
		channel:             channel,
		residentChannel:     channel,
		selectedCharacterID: 77,
	}
	if err := service.sendCurrentPreDungeonContextPlayerState(session, "test"); err != nil {
		t.Fatal(err)
	}
	rest := assertCurrentPreDungeonContextPlayerState(t, session, connection.write.Bytes())
	if len(rest) != 0 {
		t.Fatalf("pre-op27 actor-binding trailing bytes=%x", rest)
	}
	written := connection.write.Len()
	if err := service.sendCurrentPreDungeonContextPlayerState(session, "test_duplicate"); err != nil {
		t.Fatal(err)
	}
	if connection.write.Len() != written {
		t.Fatalf("duplicate pre-op27 actor-binding wrote %d extra bytes", connection.write.Len()-written)
	}
}

func assertCurrentPreDungeonContextPlayerState(t *testing.T, session *gameSession, rest []byte) []byte {
	t.Helper()
	localObjectKey := currentSceneActorObjectKey(session.selectedCharacterID)
	ownerChannel := byte(currentSceneObjectContext)
	mode0, rest := splitGameServerUpperPacket(t, rest)
	if mode0.Header.Classification != 0 || mode0.Header.MsgID != uint16(dnfenum.CmdPacketSetUDPIPPort) ||
		len(mode0.Body) < 0x4e || mode0.Body[0] != 0 ||
		mode0.Body[3] != currentSceneObjectRoute ||
		mode0.Body[4] != ownerChannel ||
		binary.LittleEndian.Uint16(mode0.Body[0x4c:0x4e]) != localObjectKey {
		t.Fatalf("pre-op27 mode0 packet=%+v body=%x rest=%x", mode0.Header, mode0.Body, rest)
	}
	mode1, rest := splitGameServerUpperPacket(t, rest)
	if mode1.Header.Classification != 0 || mode1.Header.MsgID != uint16(dnfenum.CmdPacketSetUDPIPPort) ||
		len(mode1.Body) != currentMode1BaseWireSize || mode1.Body[0] != 1 || binary.LittleEndian.Uint16(mode1.Body[0x15:0x17]) != localObjectKey ||
		mode1.Body[3] != currentSceneObjectRoute || mode1.Body[4] != ownerChannel ||
		mode1.Body[currentMode1CreateCountOffset] != 0 || mode1.Body[currentMode1CreateRowsOffset+6] != 0 {
		t.Fatalf("pre-op27 actor-binding mode1 packet=%+v body=%x rest=%x", mode1.Header, mode1.Body, rest)
	}
	if !session.preDungeonContextPlayerStateSent {
		t.Fatal("pre-op27 actor-binding stage was not committed")
	}
	return rest
}

func assertCurrentPostStartMapPlayerState(t *testing.T, session *gameSession, rest []byte, expectUserState bool, expectPreLifecycle bool) []byte {
	t.Helper()
	ownerChannel := byte(currentSceneObjectContext)
	for page := 0; page < currentSceneOverseerPageCount; page++ {
		packet, next := splitGameServerUpperPacket(t, rest)
		if packet.Header.Classification != 0 || packet.Header.MsgID != uint16(dnfenum.CmdPacketRequestOverseer) {
			t.Fatalf("post-op29 page[%d] = class %d msg %d body=%x", page, packet.Header.Classification, packet.Header.MsgID, packet.Body)
		}
		rest = next
	}
	actionTable, rest := splitGameServerUpperPacket(t, rest)
	if actionTable.Header.Classification != 0 || actionTable.Header.MsgID != uint16(dnfenum.CmdPacketPVPMissionHpPercent) {
		t.Fatalf("post-op29 action table = class %d msg %d body=%x", actionTable.Header.Classification, actionTable.Header.MsgID, actionTable.Body)
	}
	if !expectPreLifecycle {
		mode0, next := splitGameServerUpperPacket(t, rest)
		if mode0.Header.Classification != 0 ||
			mode0.Header.MsgID != uint16(dnfenum.CmdPacketSetUDPIPPort) ||
			len(mode0.Body) < 0x4e || mode0.Body[0] != 0 ||
			mode0.Body[3] != currentSceneObjectRoute ||
			mode0.Body[4] != ownerChannel {
			t.Fatalf("post-op29 fallback mode0 packet=%+v body=%x", mode0.Header, mode0.Body)
		}
		rest = next
	}
	for _, listType := range currentSelectInventoryBootstrapListTypes {
		itemPacket, next := splitCurrentSceneItemListPacket(t, rest)
		if itemPacket.Header.Classification != 0 || itemPacket.Header.MsgID != uint16(dnfenum.CmdPacketLeaveParty) || len(itemPacket.Body) == 0 || itemPacket.Body[0] != listType {
			t.Fatalf("actor-bound pre-mode1 op13 type=%d packet=%+v body=%x", listType, itemPacket.Header, itemPacket.Body)
		}
		if listType == 2 && (len(itemPacket.Body) != 6 || itemPacket.Body[5] != 0) {
			t.Fatalf("pre-mode0 op13 type2 body=%x want zero group terminator", itemPacket.Body)
		}
		rest = next
	}
	lockSnapshot, next := splitCurrentGameServerUpperPacketAuto(t, rest)
	if lockSnapshot.Header.Classification != 0 ||
		lockSnapshot.Header.MsgID != dnfitemlock.LockListMessageID ||
		!bytes.Equal(lockSnapshot.Body, []byte{0, 0}) {
		t.Fatalf("actor-bound pre-mode1 item-lock snapshot=%+v body=%x", lockSnapshot.Header, lockSnapshot.Body)
	}
	rest = next
	want := make([]struct {
		msgID uint16
		mode  byte
	}, 0, 8)
	want = append(want,
		struct {
			msgID uint16
			mode  byte
		}{uint16(dnfenum.CmdPacketSetUDPIPPort), 1},
	)
	// The pre-op27 mode0 descriptor plus post-op29 mode1 local actor binding
	// establish the wrapper. Mode3 is deliberately absent: a fresh live trace
	// proved it opens the personal-information panel and still emits no op43.
	want = append(want,
		struct {
			msgID uint16
			mode  byte
		}{uint16(dnfenum.CmdPacketInsertOverseer), 0xff},
		struct {
			msgID uint16
			mode  byte
		}{currentClearQuestListMsgID, 0xff},
		struct {
			msgID uint16
			mode  byte
		}{uint16(dnfenum.CmdPacketReportClientSpec), 0xff},
		struct {
			msgID uint16
			mode  byte
		}{uint16(dnfenum.CmdPacketRecoverStamina), 0xff},
		struct {
			msgID uint16
			mode  byte
		}{uint16(dnfenum.CmdPacketRequestBlacklist), 0xff},
	)
	// Ordinary dungeon activation is deliberately last: the working client
	// produces op43 pickup only when op120 commits the room placement before
	// op3 marks the local wrapper active. Tutorials defer op3 to finish-loading.
	if expectUserState {
		want = append(want, struct {
			msgID uint16
			mode  byte
		}{uint16(dnfenum.CmdPacketNotifyUserState), 0xff})
	}
	for idx, expected := range want {
		var packet dnfproto.ChannelPacket
		var next []byte
		if expected.msgID == currentClearQuestListMsgID {
			packet, next = splitLongHengGameServerUpperPacket(t, rest)
		} else {
			packet, next = splitGameServerUpperPacket(t, rest)
		}
		if packet.Header.Classification != 0 || packet.Header.MsgID != expected.msgID {
			t.Fatalf("post-op29 packet[%d] = class %d msg %d, want msg %d body=%x", idx, packet.Header.Classification, packet.Header.MsgID, expected.msgID, packet.Body)
		}
		if expected.mode != 0xff && (len(packet.Body) == 0 || packet.Body[0] != expected.mode) {
			t.Fatalf("post-op29 msg2 mode%d body=%x", expected.mode, packet.Body)
		}
		localObjectKey := currentSceneActorObjectKey(session.selectedCharacterID)
		if expected.mode == 0 && (len(packet.Body) < 0x4e ||
			packet.Body[3] != currentSceneObjectRoute ||
			packet.Body[4] != ownerChannel ||
			binary.LittleEndian.Uint16(packet.Body[0x4c:0x4e]) != localObjectKey) {
			t.Fatalf("post-op29 msg2 mode0 scene key body=%x", packet.Body)
		}
		if expected.msgID == uint16(dnfenum.CmdPacketNotifyUserState) {
			wantBody := []byte{1, byte(localObjectKey), byte(localObjectKey >> 8), currentDungeonPlayerUserState}
			if !bytes.Equal(packet.Body, wantBody) {
				t.Fatalf("post-op29 op3 user-state body=%x want=%x", packet.Body, wantBody)
			}
		}
		if expected.mode == 1 && (len(packet.Body) <= currentMode1CreateCountOffset ||
			packet.Body[3] != currentSceneObjectRoute ||
			packet.Body[4] != ownerChannel ||
			binary.LittleEndian.Uint16(packet.Body[0x15:0x17]) != localObjectKey) {
			t.Fatalf("post-op29 msg2 mode1 local owner/equipment body=%x", packet.Body)
		}
		if expected.mode == 3 && (len(packet.Body) < 15 ||
			packet.Body[3] != currentSceneObjectRoute ||
			packet.Body[4] != ownerChannel ||
			binary.LittleEndian.Uint16(packet.Body[13:15]) != localObjectKey) {
			t.Fatalf("post-op29 msg2 mode3 local owner/runtime finalizer body=%x", packet.Body)
		}
		if expected.msgID == currentClearQuestListMsgID {
			plain, err := zlibDecompress(packet.Body)
			if err != nil || len(plain) != 30004 || binary.LittleEndian.Uint32(plain[:4]) != 30000 {
				t.Fatalf("post-op29 op356 body len=%d plain_len=%d err=%v", len(packet.Body), len(plain), err)
			}
			if plain[4+int(localObjectKey)] != 0 {
				t.Fatalf("post-op29 op356 wrote actor key %#x into clear-quest list", localObjectKey)
			}
		}
		if expected.msgID == uint16(dnfenum.CmdPacketRecoverStamina) && (len(packet.Body) < 12 ||
			binary.LittleEndian.Uint16(packet.Body[0:2]) != 1 ||
			binary.LittleEndian.Uint16(packet.Body[4:6]) != localObjectKey ||
			packet.Body[9] != currentSceneOp9ActorDisplayKind ||
			packet.Body[10] != currentSceneObjectRoute ||
			packet.Body[11] != ownerChannel) {
			t.Fatalf("post-op29 op9 actor display body=%x", packet.Body)
		}
		if expected.msgID == uint16(dnfenum.CmdPacketRequestBlacklist) && !bytes.Equal(packet.Body, []byte{0, 0}) {
			t.Fatalf("post-op29 placement body=%x", packet.Body)
		}
		rest = next
	}
	if session.preDungeonContextPlayerStateSent != expectPreLifecycle || !session.selectedUserInfoRefreshSent || session.selectedUserInfoMode3Sent || !session.selectedItemListRefreshSent {
		t.Fatalf("post-op29 flags pre=%v placed=%v mode1=%v mode3=%v item_lists=%v", session.preDungeonContextPlayerStateSent, session.postStartMapPlayerStateSent, session.selectedUserInfoRefreshSent, session.selectedUserInfoMode3Sent, session.selectedItemListRefreshSent)
	}
	return rest
}

func TestCurrentPostStartMapSuppressesTutorialUserState(t *testing.T) {
	for _, fixture := range []struct {
		name             string
		options          tutorialScopeFixtureOptions
		expectUserState  bool
		expectDeferred   bool
		mutateSceneOwner bool
	}{
		{name: "female Slayer tutorial", expectDeferred: true},
		{
			name: "Knight tutorial",
			options: tutorialScopeFixtureOptions{
				job:              "12",
				dungeonReference: tutorialScopeKnightFDungeonReference,
				mapDirectory:     tutorialScopeKnightFMapDirectory,
			},
			expectDeferred: true,
		},
		{
			name: "PVF tutorial stale scene owner",
			options: tutorialScopeFixtureOptions{
				job:              "12",
				dungeonReference: tutorialScopeKnightFDungeonReference,
				mapDirectory:     tutorialScopeKnightFMapDirectory,
			},
			mutateSceneOwner: true,
		},
		{name: "ordinary non-tutorial dungeon", options: tutorialScopeFixtureOptions{disableTutorial: true}, expectUserState: true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			service, runtime := prepareTutorialScopeRuntime(t, fixture.options)
			service.characterStats = testCharacterStatTable(t)
			connection := &bufferConn{}
			channel := channelcatalog.Channel{ServerID: 1, ID: 16, Type: 1, Name: "ch.16", Port: 10016}
			session := &gameSession{
				conn:                connection,
				channel:             channel,
				residentChannel:     channel,
				selectedCharacterID: 99,
			}
			scene := runtime.Session.Snapshot().Scene
			if fixture.mutateSceneOwner {
				scene.Map.Map.ID++
			}
			if err := service.sendCurrentPostStartMapPlayerPlacement(session, runtime, scene, "test_owned_dungeon_user_state"); err != nil {
				t.Fatal(err)
			}
			rest := assertCurrentPostStartMapPlayerState(t, session, connection.write.Bytes(), fixture.expectUserState, false)
			if len(rest) != 0 || !session.postStartMapPlayerStateSent {
				t.Fatalf("post-op29 state incomplete: rest=%x placed=%v", rest, session.postStartMapPlayerStateSent)
			}
			wantDeferredObjectKey := uint16(0)
			if fixture.expectDeferred {
				wantDeferredObjectKey = currentSceneActorObjectKey(session.selectedCharacterID)
			}
			if session.deferredDungeonUserStateObjectKey != wantDeferredObjectKey {
				t.Fatalf(
					"deferred dungeon user-state object key=%d want=%d",
					session.deferredDungeonUserStateObjectKey,
					wantDeferredObjectKey,
				)
			}
			written := connection.write.Len()
			if err := service.sendCurrentPostStartMapPlayerPlacement(session, runtime, scene, "test_ordinary_room_change_no_replay"); err != nil {
				t.Fatal(err)
			}
			if connection.write.Len() != written {
				t.Fatalf("second room placement replayed %d bytes", connection.write.Len()-written)
			}
		})
	}
}

type failNthDungeonWriteConn struct {
	bufferConn
	writes int
	failAt int
	err    error
}

func (connection *failNthDungeonWriteConn) Write(data []byte) (int, error) {
	connection.writes++
	if connection.writes == connection.failAt {
		return 0, connection.err
	}
	return connection.bufferConn.Write(data)
}

func prepareSyntheticDungeonRuntimeForEntryTest(
	t *testing.T,
	source bridgePVFSource,
	aiCatalog *pvfDungeonAICharacterCatalog,
) *runtimeDungeonState {
	t.Helper()
	table, resolver, monsters := loadBridgeDungeonStaticData(t, source)
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(context.Background(), dnfrepo.CharacterRecord{
		CharacterID: "99",
		AccountID:   "account-1",
		Level:       20,
		Stats:       map[string]int64{"fatigue": 100},
	}); err != nil {
		t.Fatal(err)
	}
	service := &Service{
		options:                 options{accountID: "account-1"},
		worldMapTable:           table,
		worldMapResolver:        resolver,
		dungeonMonsterTable:     monsters,
		dungeonAICharacterTable: aiCatalog,
		dungeonChoice:           func(int) (int, error) { return 0, nil },
		dungeonSeed:             func() (uint32, error) { return 0x12345678, nil },
		repositoryProvider:      func() (dnfrepo.Group, bool) { return repositories, true },
	}
	session := &gameSession{selectedCharacterID: 99}
	runtime, _, err := service.prepareDungeonRuntime(
		context.Background(),
		session,
		dungeoncmd.SelectDungeonRequest{DungeonID: 700, Difficulty: 1},
	)
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}
