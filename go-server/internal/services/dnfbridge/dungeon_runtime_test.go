package dnfbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"strings"
	"testing"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	"longheng.io/server/internal/modules/dnf/dungeoncmd"
	dnfmonster "longheng.io/server/internal/modules/dnf/monster"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

func TestHandleDungeonSelectUpperBuildsRuntimeFromPVF(t *testing.T) {
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
		t.Fatalf("save character: %v", err)
	}
	if err := repositories.Inventory.Save(context.Background(), dnfrepo.InventoryRecord{
		CharacterID: "99",
		Slots: map[string]dnfrepo.ItemStack{
			"0:1": {ItemID: 1, Count: 7},
		},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	service := &Service{
		options:             options{accountID: "account-1", gameUpperBodyCodec: gameUpperBodyCodecPlain},
		worldMapTable:       table,
		worldMapResolver:    resolver,
		dungeonMonsterTable: monsters,
		dungeonChoice: func(limit int) (int, error) {
			if limit != 1 {
				t.Fatalf("choice limit = %d, want 1", limit)
			}
			return 0, nil
		},
		dungeonSeed:        func() (uint32, error) { return 0x12345678, nil },
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	conn := &bufferConn{}
	session := &gameSession{
		conn:                         conn,
		connID:                       "dungeon-test",
		selectedCharacterID:          99,
		selectPreviewObjectStateSent: true,
		currentSceneObjectListSent:   true,
	}
	bindDungeonSelectorOriginForTestAt(t, service, session, 38, 1, 450, 234)

	body := make([]byte, 21)
	binary.LittleEndian.PutUint32(body[0:4], 700)
	body[4] = 1
	frame, err := dnfproto.BuildChannelPacket(
		uint16(dnfenum.UpperMsgSelectEnter),
		body,
		0,
		dnfproto.DefaultChannelClassification,
	)
	if err != nil {
		t.Fatalf("build select dungeon upper packet: %v", err)
	}
	if err := service.handleGameUpper(session, frame); err != nil {
		t.Fatalf("handle select dungeon: %v", err)
	}

	previewRemoval, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if previewRemoval.Header.MsgID != uint16(dnfenum.CmdPacketRecoverStamina) ||
		previewRemoval.Header.Classification != 0 ||
		!bytes.Equal(previewRemoval.Body, buildCurrentSceneOp9ActorRemovalBody(99)) {
		t.Fatalf("preview removal = header=%+v body=%x", previewRemoval.Header, previewRemoval.Body)
	}
	selectAck, rest := splitGameServerUpperPacket(t, rest)
	if selectAck.Header.MsgID != uint16(dnfenum.CmdPacketSelectDungeon) ||
		selectAck.Header.Classification != dnfproto.DefaultChannelClassification ||
		!bytes.Equal(selectAck.Body, upperSuccessBody(buildSelectDungeonBodyForCharacter(99))) {
		t.Fatalf("select ACK = header=%+v body=%x", selectAck.Header, selectAck.Body)
	}
	resourcePacket, rest := splitGameServerUpperPacket(t, rest)
	if resourcePacket.Header.MsgID != currentDungeonResourceStateMsgID ||
		resourcePacket.Header.Classification != 0 ||
		!bytes.Equal(resourcePacket.Body, []byte{1, 0, 0xbc, 2, 0, 0, currentDungeonResourceSelectedState}) {
		t.Fatalf("op5 dungeon resource state = header=%+v body=%x rest=%x", resourcePacket.Header, resourcePacket.Body, rest)
	}
	rest = assertCurrentPreDungeonContextPlayerState(t, session, rest)
	contextPacket, rest := splitGameServerUpperPacket(t, rest)
	if contextPacket.Header.MsgID != currentDungeonContextMsgID ||
		contextPacket.Header.Classification != 0 ||
		len(contextPacket.Body) != 37 {
		t.Fatalf("op27 handoff = header=%+v body=%x rest=%x", contextPacket.Header, contextPacket.Body, rest)
	}
	infoPacket, rest := splitGameServerUpperPacket(t, rest)
	if infoPacket.Header.MsgID != currentDungeonInfoNotification ||
		infoPacket.Header.Classification != 0 ||
		len(infoPacket.Body) != 36 {
		t.Fatalf("op28 dungeon info = header=%+v body=%x rest=%x", infoPacket.Header, infoPacket.Body, rest)
	}
	startPacket, rest := splitGameServerUpperPacket(t, rest)
	if startPacket.Header.MsgID != currentDungeonStartNotification ||
		startPacket.Header.Classification != 0 ||
		len(startPacket.Body) < 23 {
		t.Fatalf("op29 start map = header=%+v body=%x rest=%x", startPacket.Header, startPacket.Body, rest)
	}
	rest = assertCurrentPostStartMapPlayerState(t, session, rest, true, true)
	if len(rest) != 0 {
		t.Fatalf("trailing dungeon-entry packets=%x", rest)
	}
	if !session.selectPreviewActorRemoved {
		t.Fatal("validated first op16 did not remove the transition preview actor")
	}

	session.dungeon.mu.Lock()
	runtime := session.dungeon.runtime
	session.dungeon.mu.Unlock()
	if runtime == nil || runtime.Session == nil || runtime.Dungeon.ID != 700 || runtime.MazeIndex != 0 {
		t.Fatalf("runtime = %+v", runtime)
	}
	scene, ok := runtime.Session.Scene()
	if !ok || scene.Map.Map.ID != 100 || scene.Map.Map.Path != "map/dungeon/test/start.map" {
		t.Fatalf("scene map = %+v, ok=%v", scene.Map, ok)
	}
	if len(scene.Monsters) != 1 || scene.Monsters[0].MonsterID != 3001 || len(scene.ExpectedHostiles) != 1 {
		t.Fatalf("scene hostiles = monsters=%+v expected=%+v", scene.Monsters, scene.ExpectedHostiles)
	}
	room := runtime.Room.Snapshot()
	if room.MapID != 100 || len(room.Monsters) != 1 || room.Monsters[0].ObjectKey != firstDungeonMonsterObjectKey {
		t.Fatalf("runtime monster room = %+v", room)
	}
	monster := room.Monsters[0]
	if monster.State != runtimeDungeonMonsterAnnounced || monster.Definition.ID != 3001 || monster.Definition.Name != "Synthetic Goblin" {
		t.Fatalf("runtime monster = %+v", monster)
	}
	announcedScene, _ := runtime.Session.Scene()
	if got, announced := announcedScene.RuntimeObjects[monster.ObjectKey]; !announced || got != monster.Reference {
		t.Fatalf("runtime object after start-map notification: key=%d binding=%+v announced=%v", monster.ObjectKey, got, announced)
	}
	conn.write.Reset()
	if err := service.handleGameUpper(session, frame); err != nil {
		t.Fatalf("handle duplicate select dungeon: %v", err)
	}
	duplicateAck, duplicateRest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if duplicateAck.Header.MsgID != uint16(dnfenum.CmdPacketSelectDungeon) ||
		duplicateAck.Header.Classification != dnfproto.DefaultChannelClassification ||
		!bytes.Equal(duplicateAck.Body, upperSuccessBody(buildSelectDungeonBodyForCharacter(99))) {
		t.Fatalf("duplicate select ACK = header=%+v body=%x", duplicateAck.Header, duplicateAck.Body)
	}
	if len(duplicateRest) != 0 {
		t.Fatalf("duplicate select replayed post-ACK packets: %x", duplicateRest)
	}
	if session.dungeon.runtime != runtime {
		t.Fatal("duplicate select replaced the active dungeon runtime")
	}

	mismatchBody := append([]byte(nil), body...)
	mismatchBody[4]++
	mismatchFrame, err := dnfproto.BuildChannelPacket(
		uint16(dnfenum.UpperMsgSelectEnter), mismatchBody, 0, dnfproto.DefaultChannelClassification,
	)
	if err != nil {
		t.Fatalf("build mismatched duplicate select packet: %v", err)
	}
	conn.write.Reset()
	if err := service.handleGameUpper(session, mismatchFrame); err != nil {
		t.Fatalf("handle mismatched duplicate select: %v", err)
	}
	if conn.write.Len() != 0 {
		t.Fatalf("mismatched duplicate select emitted response: %x", conn.write.Bytes())
	}
	deathBody := make([]byte, 62)
	binary.LittleEndian.PutUint32(deathBody[0:4], monster.ObjectKey)
	binary.LittleEndian.PutUint16(deathBody[4:6], 999)
	if err := service.handleDungeonMonsterDeath(session, deathBody); err != nil {
		t.Fatalf("handle mismatched monster owner: %v", err)
	}
	if got := runtime.Room.Snapshot().Monsters[0].State; got != runtimeDungeonMonsterAnnounced {
		t.Fatalf("mismatched owner advanced monster state to %s", got)
	}
	sentinelBody := append([]byte(nil), deathBody...)
	binary.LittleEndian.PutUint16(sentinelBody[4:6], ^uint16(0))
	writesBeforeSentinel := conn.write.Len()
	if err := service.handleDungeonMonsterDeath(session, sentinelBody); err != nil {
		t.Fatalf("handle non-hostile-owner sentinel on monster: %v", err)
	}
	if got := runtime.Room.Snapshot().Monsters[0].State; got != runtimeDungeonMonsterAnnounced {
		t.Fatalf("owner=ffff advanced hostile monster state to %s", got)
	}
	if conn.write.Len() != writesBeforeSentinel {
		t.Fatalf("owner=ffff hostile monster emitted response bytes: before=%d after=%d", writesBeforeSentinel, conn.write.Len())
	}
	playerObjectKey := currentSceneActorObjectKey(session.selectedCharacterID)
	binary.LittleEndian.PutUint16(deathBody[4:6], playerObjectKey)
	writesBeforeTail := conn.write.Len()
	if err := service.handleDungeonMonsterDeath(session, append(append([]byte(nil), deathBody...), 0xaa)); err != nil {
		t.Fatalf("handle tailed monster report: %v", err)
	}
	if got := runtime.Room.Snapshot().Monsters[0].State; got != runtimeDungeonMonsterAnnounced {
		t.Fatalf("unproven op39 tail advanced monster state to %s", got)
	}
	if conn.write.Len() != writesBeforeTail {
		t.Fatalf("unproven op39 tail emitted response bytes: before=%d after=%d", writesBeforeTail, conn.write.Len())
	}
	writesBeforeDeath := conn.write.Len()
	deathFrame, err := dnfproto.BuildChannelPacket(
		uint16(dnfenum.CmdPacketDieMonster), deathBody, 1, dnfproto.DefaultChannelClassification,
	)
	if err != nil {
		t.Fatalf("build current EXE monster death upper packet: %v", err)
	}
	if err := service.handleGameUpper(session, deathFrame); err != nil {
		t.Fatalf("dispatch current EXE monster death: %v", err)
	}
	reportedScene, _ := runtime.Session.Scene()
	reportedRoom := runtime.Room.Snapshot()
	if !reportedScene.Cleared || reportedRoom.Monsters[0].State != runtimeDungeonMonsterDefeated {
		t.Fatalf("validated client death report did not clear runtime = scene=%+v room=%+v", reportedScene, reportedRoom)
	}
	deathPacket, deathRest := splitGameServerUpperPacket(t, conn.write.Bytes()[writesBeforeDeath:])
	wantDeathBody, err := buildCurrentDungeonDeathNotificationBody(
		monster.ObjectKey,
		currentDungeonDeathResponseMonster,
	)
	if err != nil {
		t.Fatal(err)
	}
	if deathPacket.Header.MsgID != uint16(dnfenum.CmdPacketNotifyDieMonster) ||
		deathPacket.Header.Classification != 0 ||
		!bytes.Equal(deathPacket.Body, wantDeathBody) {
		t.Fatalf("monster death notification = header=%+v body=%x rest=%x", deathPacket.Header, deathPacket.Body, deathRest)
	}
	assertCurrentDungeonFinalClearTail(t, deathRest)
	inventory, found, err := repositories.Inventory.Load(context.Background(), "99")
	if err != nil || !found || inventory.Slots["0:1"].ItemID != 1 || inventory.Slots["0:1"].Count != 7 {
		t.Fatalf("client death report changed inventory: found=%t inventory=%+v err=%v", found, inventory, err)
	}

	writesBeforeDuplicate := conn.write.Len()
	if err := service.handleGameUpper(session, deathFrame); err != nil {
		t.Fatalf("dispatch duplicate current EXE monster death: %v", err)
	}
	if conn.write.Len() != writesBeforeDuplicate {
		t.Fatalf("duplicate death emitted response: before=%d after=%d", writesBeforeDuplicate, conn.write.Len())
	}
	if _, _, err := runtime.Room.CommitActorDeathReport(monster.ObjectKey, runtime.Session); !errors.Is(err, errDungeonMonsterAlreadyDefeated) {
		t.Fatalf("duplicate death error = %v", err)
	}
}

func TestHandleLegacySelectDungeonSharesCurrentOp16Owner(t *testing.T) {
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
		t.Fatalf("save character: %v", err)
	}
	if err := repositories.Inventory.Save(context.Background(), dnfrepo.InventoryRecord{
		CharacterID: "99",
		Slots:       map[string]dnfrepo.ItemStack{},
	}); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	service := &Service{
		options:             options{accountID: "account-1", gameUpperBodyCodec: gameUpperBodyCodecPlain},
		worldMapTable:       table,
		worldMapResolver:    resolver,
		dungeonMonsterTable: monsters,
		dungeonChoice:       func(int) (int, error) { return 0, nil },
		dungeonSeed:         func() (uint32, error) { return 0x12345678, nil },
		repositoryProvider:  func() (dnfrepo.Group, bool) { return repositories, true },
	}
	conn := &bufferConn{}
	session := &gameSession{
		conn:                         conn,
		connID:                       "legacy-op16-test",
		selectedCharacterID:          99,
		selectPreviewObjectStateSent: true,
		currentSceneObjectListSent:   true,
	}
	bindDungeonSelectorOriginForTestAt(t, service, session, 38, 1, 450, 234)

	body := make([]byte, dungeoncmd.SelectDungeonRequestSize)
	binary.LittleEndian.PutUint32(body[0:4], 700)
	body[4] = 1
	if err := service.handleGameCommand(session, byte(dnfenum.GameCmdCommand), uint16(dnfenum.GameTypeSelectDungeon), body); err != nil {
		t.Fatalf("legacy select dungeon: %v", err)
	}

	previewRemoval, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if previewRemoval.Header.MsgID != uint16(dnfenum.CmdPacketRecoverStamina) {
		t.Fatalf("preview removal header=%+v", previewRemoval.Header)
	}
	selectAck, _ := splitGameServerUpperPacket(t, rest)
	if selectAck.Header.MsgID != uint16(dnfenum.CmdPacketSelectDungeon) ||
		selectAck.Header.Classification != dnfproto.DefaultChannelClassification ||
		!bytes.Equal(selectAck.Body, upperSuccessBody(buildSelectDungeonBodyForCharacter(99))) {
		t.Fatalf("legacy op16 did not reach current owner: header=%+v body=%x", selectAck.Header, selectAck.Body)
	}
}

func TestHandleDungeonSelectUpperRejectsMalformedRequestWithoutStateOrReply(t *testing.T) {
	for _, bodyLen := range []int{4, dungeoncmd.SelectDungeonRequestSize + 1} {
		service := &Service{}
		conn := &bufferConn{}
		session := &gameSession{conn: conn, selectedCharacterID: 99}

		service.handleDungeonSelectUpper(session, make([]byte, bodyLen))
		if conn.write.Len() != 0 {
			t.Fatalf("%d-byte malformed request produced reply: %x", bodyLen, conn.write.Bytes())
		}
		if session.dungeon.runtime != nil {
			t.Fatalf("%d-byte malformed request committed runtime: %+v", bodyLen, session.dungeon.runtime)
		}
	}
}

func TestHandleDungeonSelectUpperReportsEntryLevelTooLowWithoutRuntime(t *testing.T) {
	table, resolver, monsters := loadBridgeDungeonStaticData(t, bridgeDungeonPVF(false))
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(context.Background(), dnfrepo.CharacterRecord{
		CharacterID: "99",
		AccountID:   "account-1",
		Level:       9,
		Stats: map[string]int64{
			"fatigue": 100, "town_id": 38, "area_id": 1,
			"pos_x": 450, "pos_y": 234, "direction": 5, "area_state": 3,
		},
	}); err != nil {
		t.Fatal(err)
	}
	service := &Service{
		options:             options{accountID: "account-1", gameUpperBodyCodec: gameUpperBodyCodecPlain},
		worldMapTable:       table,
		worldMapResolver:    resolver,
		dungeonMonsterTable: monsters,
		repositoryProvider:  func() (dnfrepo.Group, bool) { return repositories, true },
	}
	connection := &bufferConn{}
	session := &gameSession{conn: connection, selectedCharacterID: 99}
	body := make([]byte, dungeoncmd.SelectDungeonRequestSize)
	binary.LittleEndian.PutUint32(body[0:4], 700)
	binary.LittleEndian.PutUint16(body[9:11], 0xffff)

	if err := service.handleDungeonSelectUpper(session, body); err != nil {
		t.Fatal(err)
	}
	packet, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if packet.Header.Classification != dnfproto.DefaultChannelClassification ||
		packet.Header.MsgID != uint16(dnfenum.CmdPacketSelectDungeon) ||
		!bytes.Equal(packet.Body, []byte{0, 14, 0}) {
		t.Fatalf("level failure packet=%+v body=%x rest=%x", packet.Header, packet.Body, rest)
	}
	if len(rest) != 0 {
		t.Fatalf("level failure emitted trailing entry packets: %x", rest)
	}
	session.dungeon.mu.Lock()
	runtime := session.dungeon.runtime
	session.dungeon.mu.Unlock()
	if runtime != nil {
		t.Fatalf("level failure committed runtime: %+v", runtime)
	}
	if session.selectPreviewActorRemoved || session.preDungeonContextPlayerStateSent ||
		session.enterSelectDungeonContextSent || session.postStartMapPlayerStateSent ||
		session.sceneBootstrapTailSent {
		t.Fatalf("level failure advanced entry state: %+v", session)
	}
}

func TestPrepareDungeonRuntimeValidatesPVFEntryRules(t *testing.T) {
	table, resolver, monsters := loadBridgeDungeonStaticData(t, bridgeDungeonPVF(false))
	tests := []struct {
		name      string
		character dnfrepo.CharacterRecord
		wantErr   error
	}{
		{
			name:      "minimum level",
			character: dnfrepo.CharacterRecord{CharacterID: "99", AccountID: "account-1", Level: 9, Stats: map[string]int64{"fatigue": 100}},
			wantErr:   errDungeonLevelTooLow,
		},
		{
			name:      "fatigue unavailable",
			character: dnfrepo.CharacterRecord{CharacterID: "99", AccountID: "account-1", Level: 20},
			wantErr:   errDungeonFatigueUnknown,
		},
		{
			name:      "fatigue exhausted",
			character: dnfrepo.CharacterRecord{CharacterID: "99", AccountID: "account-1", Level: 20, Stats: map[string]int64{"fatigue": 0}},
			wantErr:   errDungeonFatigueExhausted,
		},
		{
			name:      "account mismatch",
			character: dnfrepo.CharacterRecord{CharacterID: "99", AccountID: "account-2", Level: 20, Stats: map[string]int64{"fatigue": 100}},
			wantErr:   errDungeonAccountMismatch,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repositories := dnfrepomemory.NewMemoryGroup()
			if err := repositories.Character.Save(context.Background(), test.character); err != nil {
				t.Fatal(err)
			}
			service := &Service{
				options:             options{accountID: "account-1"},
				worldMapTable:       table,
				worldMapResolver:    resolver,
				dungeonMonsterTable: monsters,
				dungeonChoice:       func(int) (int, error) { return 0, nil },
				repositoryProvider: func() (dnfrepo.Group, bool) {
					return repositories, true
				},
			}
			_, _, err := service.prepareDungeonRuntime(
				context.Background(),
				&gameSession{selectedCharacterID: 99},
				dungeoncmd.SelectDungeonRequest{DungeonID: 700, Difficulty: 1},
			)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("prepare error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestPrepareDungeonRuntimeHonorsPVFNoFatigue(t *testing.T) {
	table, resolver, monsters := loadBridgeDungeonStaticData(t, bridgeDungeonPVF(true))
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(context.Background(), dnfrepo.CharacterRecord{
		CharacterID: "99",
		AccountID:   "account-1",
		Level:       20,
	}); err != nil {
		t.Fatal(err)
	}
	service := &Service{
		options:             options{accountID: "account-1"},
		worldMapTable:       table,
		worldMapResolver:    resolver,
		dungeonMonsterTable: monsters,
		dungeonChoice:       func(int) (int, error) { return 0, nil },
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repositories, true
		},
	}
	runtime, scene, err := service.prepareDungeonRuntime(
		context.Background(),
		&gameSession{selectedCharacterID: 99},
		dungeoncmd.SelectDungeonRequest{DungeonID: 700, Difficulty: 1},
	)
	if err != nil {
		t.Fatalf("prepare no-fatigue dungeon: %v", err)
	}
	if runtime == nil || scene.Map.Map.ID != 100 {
		t.Fatalf("runtime=%+v scene=%+v", runtime, scene)
	}
}

func TestChooseDungeonIndexUsesServerRandomnessForPVFAmbiguity(t *testing.T) {
	service := &Service{}
	if index, err := service.chooseDungeonIndex(1); err != nil || index != 0 {
		t.Fatalf("single PVF candidate = index=%d err=%v", index, err)
	}
	if index, err := service.chooseDungeonIndex(2); err != nil || index < 0 || index >= 2 {
		t.Fatalf("server-random PVF candidate = index=%d err=%v", index, err)
	}
	service.dungeonChoice = func(limit int) (int, error) {
		if limit != 2 {
			t.Fatalf("choice limit = %d", limit)
		}
		return 1, nil
	}
	if index, err := service.chooseDungeonIndex(2); err != nil || index != 1 {
		t.Fatalf("explicit PVF candidate = index=%d err=%v", index, err)
	}
}

func TestNewRuntimeDungeonRoomRejectsMissingDefinitionAndObjectKeyOverflow(t *testing.T) {
	_, _, monsters := loadBridgeDungeonStaticData(t, bridgeDungeonPVF(false))
	missing := worldmap.DungeonRoomScene{
		Coordinate: worldmap.RoomCoordinate{X: 2, Y: 3},
		Map:        worldmap.ResolvedMap{Map: worldmap.Map{ID: 101}},
		Monsters:   []worldmap.MonsterSpawn{{MonsterID: 9999}},
	}
	if _, _, err := newRuntimeDungeonRoom(missing, monsters, firstDungeonMonsterObjectKey); !errors.Is(err, errDungeonMonsterDefinitionMiss) {
		t.Fatalf("missing monster definition error = %v", err)
	}
	overflow := missing
	overflow.Monsters[0].MonsterID = 3001
	if _, _, err := newRuntimeDungeonRoom(overflow, monsters, uint32(^uint16(0))+1); !errors.Is(err, errDungeonMonsterObjectKeyRange) {
		t.Fatalf("monster object key overflow error = %v", err)
	}
}

func TestNewRuntimeDungeonRoomPreservesUnsupportedHostiles(t *testing.T) {
	_, _, monsters := loadBridgeDungeonStaticData(t, bridgeDungeonPVF(false))
	reference := worldmap.HostileReference{Kind: worldmap.HostileAICharacter, Index: 4}
	room, next, err := newRuntimeDungeonRoom(worldmap.DungeonRoomScene{
		Coordinate:       worldmap.RoomCoordinate{X: 1, Y: 1},
		Map:              worldmap.ResolvedMap{Map: worldmap.Map{ID: 102}},
		ExpectedHostiles: []worldmap.HostileReference{reference},
	}, monsters, firstDungeonMonsterObjectKey)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := room.Snapshot()
	if next != firstDungeonMonsterObjectKey || len(snapshot.Monsters) != 0 || len(snapshot.OpaqueHostiles) != 1 || snapshot.OpaqueHostiles[0] != reference {
		t.Fatalf("opaque hostile preservation = snapshot=%+v next=%d", snapshot, next)
	}
}

type bridgePVFSource map[string]string

func (s bridgePVFSource) ReadText(relativePath string) (string, error) {
	for path, text := range s {
		if worldmapPathKey(path) == worldmapPathKey(relativePath) {
			return text, nil
		}
	}
	return "", dnfpvf.ErrDocNotFound
}

func worldmapPathKey(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	value = strings.TrimPrefix(value, "./")
	return strings.ToLower(value)
}

func loadBridgeDungeonStaticData(t *testing.T, source bridgePVFSource) (*worldmap.Table, *worldmap.Resolver, *pvfDungeonMonsterCatalog) {
	t.Helper()
	table, err := worldmap.LoadSource(context.Background(), source, worldmap.Options{})
	if err != nil {
		t.Fatalf("load synthetic worldmap PVF: %v", err)
	}
	resolver, err := worldmap.NewResolver(table)
	if err != nil {
		t.Fatalf("build synthetic worldmap resolver: %v", err)
	}
	monsters, err := newPVFDungeonMonsterCatalog(source)
	if err != nil {
		t.Fatalf("load synthetic monster catalog: %v", err)
	}
	return table, resolver, monsters
}

func bridgeDungeonPVF(noFatigue bool) bridgePVFSource {
	noFatigueSection := ""
	if noFatigue {
		noFatigueSection = "[no fatigue]\n"
	}
	return bridgePVFSource{
		worldmap.DefaultMapList: "100 `dungeon/test/start.map`\n",
		"map/dungeon/test/start.map": "[map name]\n`start`\n" +
			"[dungeon]\n700\n" +
			"[type]\n`[start]`\n" +
			"[monster]\n3001 10 0 100 200 0 0 0 `[fixed]` `[normal]`\n",
		worldmap.DefaultDungeonList: "700 `test.dgn`\n",
		"dungeon/test.dgn": "[name]\n`Synthetic Dungeon`\n" +
			"[minimum required level]\n10\n" +
			"[basis level]\n20\n" +
			noFatigueSection +
			"[limit party count]\n1\n" +
			"[maze info]\n" +
			"[size]\n1 1\n" +
			"[greed]\n`A`\n" +
			"[map specification]\n`map` 0 0 100\n" +
			"[start map]\n0 0\n" +
			"[boss map]\n0 0\n",
		worldmap.DefaultWorldMapList: "1 `test.wdm`\n",
		"worldmap/test.wdm":          "[name]\n`Synthetic Area`\n[dungeon]\n700 -1\n[/dungeon]\n",
		dnfmonster.DefaultList:       "3001 `test.gob`\n",
		"monster/test.gob": "[name]\n`Synthetic Goblin`\n" +
			"[level]\n10\n" +
			"[hp]\n500\n" +
			"[exp]\n25\n",
	}
}
