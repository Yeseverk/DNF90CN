package dnfbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"testing"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	"longheng.io/server/internal/modules/dnf/dungeoncmd"
	dnfmonster "longheng.io/server/internal/modules/dnf/monster"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

const (
	bridgeDungeonMoveStartMapPath = "map/Cataclysm/NewTutorial/ATSwordman/start.map"
	bridgeDungeonMoveNextMapPath  = "map/Cataclysm/NewTutorial/ATSwordman/next.map"
	bridgeDungeonMoveLeftMapPath  = "map/Cataclysm/NewTutorial/ATSwordman/left.map"
	bridgeDungeonMoveDungeonPath  = "dungeon/Cataclysm/NewTutorial/Tutorial_ATSwordman.dgn"
)

func TestPlanCurrentDungeonMovePreflightsWithoutCommitting(t *testing.T) {
	service, runtime := prepareSyntheticMoveRuntime(t, false)
	before := runtime.Session.Snapshot()
	beforeRoom := runtime.Room.Snapshot()
	beforeNextObjectKey := runtime.NextObjectKey

	plan, err := service.planCurrentDungeonMove(runtime, dungeoncmd.MoveMapRequest{NextX: 1, NextY: 0})
	if err != nil {
		t.Fatalf("plan current dungeon move: %v", err)
	}
	if plan.Source.Coordinate != (worldmap.RoomCoordinate{X: 0, Y: 0}) || plan.Source.Map.Map.ID != 100 {
		t.Fatalf("source owner = %+v", plan.Source)
	}
	if plan.Target.Coordinate != (worldmap.RoomCoordinate{X: 1, Y: 0}) || plan.Target.Map.Map.ID != 101 {
		t.Fatalf("target owner = %+v", plan.Target)
	}
	if plan.Seed != 1 {
		t.Fatalf("target room seed=%d", plan.Seed)
	}
	targetRoom := plan.TargetRoom.Snapshot()
	if len(targetRoom.Monsters) != 1 || targetRoom.Monsters[0].State != runtimeDungeonMonsterPlanned || targetRoom.Monsters[0].Spawn.MonsterID != 3001 {
		t.Fatalf("target runtime room = %+v", targetRoom)
	}
	startMapBody, err := buildCurrentDungeonMoveStartMapBody(runtime, plan)
	if err != nil || len(startMapBody) < 23 {
		t.Fatalf("target-room start-map body len=%d error=%v body=%x", len(startMapBody), err, startMapBody)
	}

	after := runtime.Session.Snapshot()
	afterRoom := runtime.Room.Snapshot()
	if after.Run.Current != before.Run.Current || after.Scene.Coordinate != before.Scene.Coordinate || after.Scene.Map.Map.ID != before.Scene.Map.Map.ID {
		t.Fatalf("preflight committed session: before=%+v after=%+v", before, after)
	}
	if afterRoom.Coordinate != beforeRoom.Coordinate || afterRoom.MapID != beforeRoom.MapID || runtime.NextObjectKey != beforeNextObjectKey {
		t.Fatalf("preflight committed runtime room/object keys: before=%+v after=%+v next_before=%d next_after=%d",
			beforeRoom, afterRoom, beforeNextObjectKey, runtime.NextObjectKey)
	}
}

func TestHandleDungeonMoveMapRunsOrderedRepeatedBossStoryStages(t *testing.T) {
	service, runtime := prepareSyntheticStoryStageRuntime(t)
	service.options.gameUpperBodyCodec = gameUpperBodyCodecPlain
	conn := &bufferConn{}
	session := &gameSession{
		conn:                conn,
		connID:              "dungeon-story-stage-chain-test",
		selectedCharacterID: 99,
		dungeon:             dungeonSessionState{runtime: runtime},
	}

	move := func(x byte) dnfproto.ChannelPacket {
		t.Helper()
		conn.write.Reset()
		body := make([]byte, dungeoncmd.MoveMapRequestSize)
		body[0] = x
		if err := service.handleDungeonMoveMap(session, body); err != nil {
			t.Fatal(err)
		}
		packet, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
		if packet.Header.MsgID != currentDungeonStartNotification || len(rest) != 0 {
			t.Fatalf("move target=%d packet=%+v rest=%x", x, packet.Header, rest)
		}
		return packet
	}
	assertFullStartMap := func(packet dnfproto.ChannelPacket, operation, payload byte, mapID uint32) {
		t.Helper()
		if len(packet.Body) < 23 || packet.Body[2] != operation || packet.Body[13] != payload ||
			binary.LittleEndian.Uint32(packet.Body[14:18]) != mapID {
			t.Fatalf("start-map body=%x want operation=%d payload=%d map=%d", packet.Body, operation, payload, mapID)
		}
	}

	base := move(1)
	assertFullStartMap(base, currentDungeonStartMapOperationCurrent, currentDungeonStartMapPayloadBuild, 110)
	stage0 := move(2)
	assertFullStartMap(stage0, currentDungeonStartMapOperationAdvanceLayer, currentDungeonStartMapPayloadBuild, 200)
	if runtime.StoryStageIndex != 0 || runtime.Session.Snapshot().Scene.Map.Map.ID != 200 {
		t.Fatalf("stage0 runtime index=%d scene=%+v", runtime.StoryStageIndex, runtime.Session.Snapshot().Scene)
	}

	killStageBoss := func(next worldmap.RoomCoordinate) {
		t.Helper()
		room := runtime.Room.Snapshot()
		var bossObjectKey uint32
		for _, monster := range room.Monsters {
			if currentDungeonOrdinaryMonsterLooksBoss(monster) {
				bossObjectKey = monster.ObjectKey
				break
			}
		}
		if bossObjectKey == 0 {
			t.Fatalf("story stage has no announced boss actor: %+v", room.Monsters)
		}
		conn.write.Reset()
		body := make([]byte, dungeoncmd.DieMonsterRequestSize)
		binary.LittleEndian.PutUint32(body[0:4], bossObjectKey)
		binary.LittleEndian.PutUint16(body[4:6], currentSceneActorObjectKey(99))
		if err := service.handleDungeonMonsterDeath(session, body); err != nil {
			t.Fatal(err)
		}
		packets := splitAllFinishBridgePackets(t, conn.write.Bytes())
		if len(packets) < 2 || packets[0].Header.MsgID != uint16(dnfenum.CmdPacketNotifyDieMonster) ||
			binary.LittleEndian.Uint16(packets[0].Body[:2]) != uint16(bossObjectKey) {
			t.Fatalf("story boss response order packets=%+v bytes=%x", packets, conn.write.Bytes())
		}
		wantTail := []byte{1, byte(next.X), byte(next.Y)}
		if !bytes.Equal(packets[0].Body[len(packets[0].Body)-3:], wantTail) {
			t.Fatalf("story boss op38 tail=%x want=%x body=%x", packets[0].Body[len(packets[0].Body)-3:], wantTail, packets[0].Body)
		}
		snapshot := runtime.Session.Snapshot()
		if !snapshot.Scene.Cleared || snapshot.Run.Status != worldmap.DungeonRunActive ||
			runtime.bossDieCheckAccepted || runtime.ordinaryFinalRoomClearAccepted {
			t.Fatalf("intermediate story stage settled early snapshot=%+v runtime=%+v", snapshot, runtime)
		}
	}

	killStageBoss(worldmap.RoomCoordinate{X: 1, Y: 0})
	stage1 := move(1)
	assertFullStartMap(stage1, currentDungeonStartMapOperationAdvanceLayer, currentDungeonStartMapPayloadRefresh, 201)
	if runtime.StoryStageIndex != 1 || runtime.Session.Snapshot().Scene.Map.Map.ID != 201 {
		t.Fatalf("stage1 runtime index=%d scene=%+v", runtime.StoryStageIndex, runtime.Session.Snapshot().Scene)
	}
	killStageBoss(worldmap.RoomCoordinate{X: 2, Y: 0})
	finalStage := move(2)
	assertFullStartMap(finalStage, currentDungeonStartMapOperationAdvanceLayer, currentDungeonStartMapPayloadRefresh, 202)
	finalScene := runtime.Session.Snapshot().Scene
	if runtime.StoryStageIndex != 2 || finalScene.Map.Map.ID != 202 ||
		!currentDungeonStoryFinalStageMatches(runtime, finalScene) {
		t.Fatalf("final story runtime index=%d scene=%+v stages=%+v", runtime.StoryStageIndex, finalScene, runtime.StoryStages)
	}
}

func TestHandleDungeonMoveMapMoveKindOneCommitsExplicitSameCoordinateLayer(t *testing.T) {
	service, runtime := prepareSyntheticMoveRuntimeFromPVF(t, bridgeDungeonLayerPVF(true))
	service.options.gameUpperBodyCodec = gameUpperBodyCodecPlain
	if _, err := runtime.Room.AnnounceAllActors(runtime.Session); err != nil {
		t.Fatalf("announce base actors before client move: %v", err)
	}
	if err := runtime.cacheCurrentDungeonRoom(); err != nil {
		t.Fatalf("cache announced base room: %v", err)
	}
	before := runtime.Session.Snapshot()
	if before.Scene.Cleared {
		t.Fatal("layer regression requires an uncleared source scene")
	}
	conn := &bufferConn{}
	session := &gameSession{
		conn:                               conn,
		connID:                             "dungeon-layer-move-test",
		selectedCharacterID:                99,
		dungeon:                            dungeonSessionState{runtime: runtime},
		currentFinishLoadingStateSent:      true,
		currentFinishLoadingCompletionSent: true,
	}
	body := make([]byte, dungeoncmd.MoveMapRequestSize)
	body[0] = byte(before.Scene.Coordinate.X)
	body[1] = byte(before.Scene.Coordinate.Y)
	body[10] = 1
	request, err := dungeoncmd.DecodeMoveMapRequest(body)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.planCurrentDungeonMove(runtime, request); err != nil {
		t.Fatalf("preflight same-coordinate layered move: %v", err)
	}
	if err := service.handleDungeonMoveMap(session, body); err != nil {
		t.Fatalf("same-coordinate layered move: %v", err)
	}
	packet, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if packet.Header.MsgID != currentDungeonStartNotification || packet.Header.Classification != 0 || len(rest) != 0 {
		t.Fatalf("layered move response header=%+v rest=%x", packet.Header, rest)
	}
	after := runtime.Session.Snapshot()
	if after.Scene.Coordinate != before.Scene.Coordinate || after.Scene.Map.Map.ID != 201 {
		t.Fatalf("layered scene=%s/%d want=%s/201", after.Scene.Coordinate, after.Scene.Map.Map.ID, before.Scene.Coordinate)
	}
	if !runtime.LayeredMapActive || runtime.LayeredMapIndex != 0 {
		t.Fatalf("layered runtime state active=%t index=%d", runtime.LayeredMapActive, runtime.LayeredMapIndex)
	}
	if runtime.Room.Snapshot().MapID != 201 {
		t.Fatalf("layered runtime room map=%d", runtime.Room.Snapshot().MapID)
	}
	if len(runtime.Rooms) != 2 || runtime.Rooms[runtimeDungeonRoomKeyFromScene(before.Scene)] == nil ||
		runtime.Rooms[runtimeDungeonRoomKeyFromScene(after.Scene)] == nil {
		t.Fatalf("base/layer cache ownership=%+v", runtime.Rooms)
	}
	wantPacket, err := currentDungeonStartMapFromRuntime(runtime, after.Scene, currentDungeonStartMapState{
		LayeredRoomFlag:  1,
		Seed:             runtime.Seed,
		RoomStateValue:   1,
		RoomStateFlag:    1,
		PartyMemberIndex: 0xff,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantBody, err := wantPacket.Build()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(packet.Body, wantBody) {
		t.Fatalf("layered start-map body=%x want=%x", packet.Body, wantBody)
	}
}

func TestHandleDungeonMoveMapAdvancesMultipleLayersAndRestoresCachedBase(t *testing.T) {
	service, runtime := prepareSyntheticMoveRuntimeFromPVF(t, bridgeDungeonMultiLayerPVF())
	service.options.gameUpperBodyCodec = gameUpperBodyCodecPlain
	baseScene, ok := runtime.Session.Scene()
	if !ok || baseScene.Map.Map.ID != 200 {
		t.Fatalf("base scene=%+v ok=%t", baseScene, ok)
	}
	if _, err := runtime.Room.AnnounceAllActors(runtime.Session); err != nil {
		t.Fatalf("announce base actors: %v", err)
	}
	if err := runtime.cacheCurrentDungeonRoom(); err != nil {
		t.Fatalf("cache announced base: %v", err)
	}
	baseKey := runtimeDungeonRoomKeyFromScene(baseScene)
	baseVisit := runtime.Rooms[baseKey]
	baseVisit.DropRNG = 0x11223344
	baseRoom := baseVisit.Room
	baseSnapshot := baseRoom.Snapshot()
	if len(baseSnapshot.Monsters) != 1 {
		t.Fatalf("base actors=%+v", baseSnapshot)
	}
	baseObjectKey := baseSnapshot.Monsters[0].ObjectKey

	conn := &bufferConn{}
	session := &gameSession{
		conn:                conn,
		connID:              "dungeon-multi-layer-return-test",
		selectedCharacterID: 99,
		dungeon:             dungeonSessionState{runtime: runtime},
	}
	moveBody := make([]byte, dungeoncmd.MoveMapRequestSize)
	moveBody[0] = byte(baseScene.Coordinate.X)
	moveBody[1] = byte(baseScene.Coordinate.Y)
	moveBody[10] = 1

	move := func() []byte {
		t.Helper()
		conn.write.Reset()
		if err := service.handleDungeonMoveMap(session, moveBody); err != nil {
			t.Fatalf("layer move: %v", err)
		}
		packet, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
		if packet.Header.MsgID != currentDungeonStartNotification || packet.Header.Classification != 0 || len(rest) != 0 {
			t.Fatalf("layer response header=%+v rest=%x", packet.Header, rest)
		}
		return append([]byte(nil), packet.Body...)
	}

	first := move()
	if len(first) < 23 || first[2] != currentDungeonStartMapOperationAdvanceLayer ||
		first[13] != currentDungeonStartMapPayloadBuild || binary.LittleEndian.Uint32(first[14:18]) != 201 {
		t.Fatalf("first layer op29=%x want operation1/mode1/map201", first)
	}
	second := move()
	if len(second) < 23 || second[2] != currentDungeonStartMapOperationAdvanceLayer ||
		second[13] != currentDungeonStartMapPayloadRefresh || binary.LittleEndian.Uint32(second[14:18]) != 202 {
		t.Fatalf("second layer op29=%x want operation1/mode2/map202", second)
	}
	finalRoom := runtime.Room.Snapshot()
	if len(finalRoom.Monsters) != 1 {
		t.Fatalf("final layer actors=%+v", finalRoom)
	}
	if _, cleared, err := runtime.Room.CommitActorDeathReport(finalRoom.Monsters[0].ObjectKey, runtime.Session); err != nil || !cleared {
		t.Fatalf("clear final layer cleared=%t err=%v", cleared, err)
	}
	if err := runtime.Session.Complete(); err != nil {
		t.Fatalf("complete final layer: %v", err)
	}
	nextObjectKey := runtime.NextObjectKey

	returnedBody := move()
	wantReturnedBody, err := (currentDungeonCachedStartMap{
		X:                byte(baseScene.Coordinate.X),
		Y:                byte(baseScene.Coordinate.Y),
		Operation:        currentDungeonStartMapOperationRestoreBase,
		Seed:             baseVisit.Seed,
		RoomStateValue:   1,
		PartyMemberIndex: 0xff,
	}).Build()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(returnedBody, wantReturnedBody) {
		t.Fatalf("base return op29=%x want operation2/mode0=%x", returnedBody, wantReturnedBody)
	}
	returned := runtime.Session.Snapshot()
	chain := runtime.LayerChains[baseScene.Coordinate]
	if returned.Scene.Map.Map.ID != 200 || returned.Run.Status != worldmap.DungeonRunCompleted ||
		runtime.Room != baseRoom || runtime.Seed != baseVisit.Seed || runtime.NextObjectKey != nextObjectKey ||
		runtime.LayeredMapActive || runtime.LayeredMapIndex != -1 || chain == nil || !chain.Consumed || !chain.FinalAckPending {
		t.Fatalf("returned scene=%+v runtime active=%t index=%d chain=%+v", returned, runtime.LayeredMapActive, runtime.LayeredMapIndex, chain)
	}
	if baseVisit.DropRNG != 0x11223344 {
		t.Fatalf("base drop RNG=%08x want=11223344", baseVisit.DropRNG)
	}
	returnedRoom := runtime.Room.Snapshot()
	if len(returnedRoom.Monsters) != 1 || returnedRoom.Monsters[0].ObjectKey != baseObjectKey ||
		returnedRoom.Monsters[0].State != runtimeDungeonMonsterAnnounced {
		t.Fatalf("returned base actors=%+v", returnedRoom.Monsters)
	}
	if reference, bound := returned.Scene.RuntimeObjects[baseObjectKey]; !bound || reference != returned.Scene.ExpectedHostiles[0] {
		t.Fatalf("returned base binding=%+v bound=%t", reference, bound)
	}

	conn.write.Reset()
	session.currentFinishLoadingStateSent = true
	session.currentFinishLoadingCompletionSent = true
	if err := service.handleDungeonMoveMap(session, moveBody); err != nil {
		t.Fatalf("consume final base ACK: %v", err)
	}
	if conn.write.Len() != 0 || chain.FinalAckPending {
		t.Fatalf("final base ACK wrote=%x pending=%t", conn.write.Bytes(), chain.FinalAckPending)
	}
	if !session.currentFinishLoadingStateSent || !session.currentFinishLoadingCompletionSent {
		t.Fatalf("final ACK rearmed finish-loading state=%t completion=%t",
			session.currentFinishLoadingStateSent, session.currentFinishLoadingCompletionSent)
	}

	conn.write.Reset()
	if err := service.handleDungeonMoveMap(session, moveBody); err != nil {
		t.Fatalf("reject consumed layer replay: %v", err)
	}
	if conn.write.Len() != 0 {
		t.Fatalf("consumed layer replay wrote=%x", conn.write.Bytes())
	}
}

func TestHandleDungeonMoveMapReturnsFromFinalLayerWithoutCombatDeathReports(t *testing.T) {
	service, runtime := prepareSyntheticMoveRuntimeFromPVF(t, bridgeDungeonLayerPVF(true))
	service.options.gameUpperBodyCodec = gameUpperBodyCodecPlain
	baseScene, ok := runtime.Session.Scene()
	if !ok {
		t.Fatal("base scene missing")
	}
	if _, err := runtime.Room.AnnounceAllActors(runtime.Session); err != nil {
		t.Fatalf("announce base actors: %v", err)
	}
	if err := runtime.cacheCurrentDungeonRoom(); err != nil {
		t.Fatalf("cache base room: %v", err)
	}
	baseVisit := runtime.Rooms[runtimeDungeonRoomKeyFromScene(baseScene)]
	conn := &bufferConn{}
	session := &gameSession{
		conn: conn, connID: "dungeon-scripted-layer-return-test",
		selectedCharacterID: 99, dungeon: dungeonSessionState{runtime: runtime},
	}
	moveBody := make([]byte, dungeoncmd.MoveMapRequestSize)
	moveBody[0] = byte(baseScene.Coordinate.X)
	moveBody[1] = byte(baseScene.Coordinate.Y)
	moveBody[10] = 1

	if err := service.handleDungeonMoveMap(session, moveBody); err != nil {
		t.Fatalf("enter scripted layer: %v", err)
	}
	if runtime.Session.Snapshot().Scene.Cleared {
		t.Fatal("regression requires a layered scene without combat death reports")
	}
	conn.write.Reset()
	if err := service.handleDungeonMoveMap(session, moveBody); err != nil {
		t.Fatalf("return scripted layer to cached base: %v", err)
	}
	packet, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	wantBody, err := (currentDungeonCachedStartMap{
		X:                byte(baseScene.Coordinate.X),
		Y:                byte(baseScene.Coordinate.Y),
		Operation:        currentDungeonStartMapOperationRestoreBase,
		Seed:             baseVisit.Seed,
		RoomStateValue:   1,
		PartyMemberIndex: 0xff,
	}).Build()
	if err != nil {
		t.Fatal(err)
	}
	if packet.Header.MsgID != currentDungeonStartNotification || len(rest) != 0 || !bytes.Equal(packet.Body, wantBody) {
		t.Fatalf("scripted layer return packet=%+v body=%x rest=%x want=%x", packet.Header, packet.Body, rest, wantBody)
	}
	chain := runtime.LayerChains[baseScene.Coordinate]
	if runtime.Room != baseVisit.Room || runtime.LayeredMapActive || runtime.LayeredMapIndex != -1 ||
		chain == nil || !chain.Consumed || !chain.FinalAckPending {
		t.Fatalf("scripted layer return runtime active=%t index=%d chain=%+v", runtime.LayeredMapActive, runtime.LayeredMapIndex, chain)
	}
}

func TestHandleDungeonMoveMapClearedFinalLayerCanExitToExplicitAdjacentTarget(t *testing.T) {
	service, runtime := prepareSyntheticMoveRuntimeFromPVF(t, bridgeDungeonTerminalLayerExitPVF(false))
	service.options.gameUpperBodyCodec = gameUpperBodyCodecPlain
	conn := &bufferConn{}
	session := &gameSession{conn: conn, connID: "dungeon-final-layer-explicit-target-test", selectedCharacterID: 99, dungeon: dungeonSessionState{runtime: runtime}}
	if _, err := runtime.Room.AnnounceAllActors(runtime.Session); err != nil {
		t.Fatalf("announce base actors: %v", err)
	}
	if err := runtime.cacheCurrentDungeonRoom(); err != nil {
		t.Fatalf("cache announced base: %v", err)
	}
	baseScene, ok := runtime.Session.Scene()
	if !ok {
		t.Fatal("base scene missing")
	}
	layerBody := make([]byte, dungeoncmd.MoveMapRequestSize)
	layerBody[0] = byte(baseScene.Coordinate.X)
	layerBody[1] = byte(baseScene.Coordinate.Y)
	layerBody[10] = 1
	if err := service.handleDungeonMoveMap(session, layerBody); err != nil {
		t.Fatal(err)
	}
	conn.write.Reset()
	layerRoom := runtime.Room.Snapshot()
	if len(layerRoom.Monsters) != 1 {
		t.Fatalf("layer actors=%+v", layerRoom.Monsters)
	}
	if _, cleared, err := runtime.Room.CommitActorDeathReport(layerRoom.Monsters[0].ObjectKey, runtime.Session); err != nil || !cleared {
		t.Fatalf("clear final layer cleared=%t err=%v", cleared, err)
	}
	exitBody := make([]byte, dungeoncmd.MoveMapRequestSize)
	exitBody[0] = 1
	if err := service.handleDungeonMoveMap(session, exitBody); err != nil {
		t.Fatal(err)
	}
	packet, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if packet.Header.MsgID != currentDungeonStartNotification || len(rest) != 0 ||
		len(packet.Body) < 23 || packet.Body[2] != currentDungeonStartMapOperationCurrent ||
		packet.Body[13] != currentDungeonStartMapPayloadBuild || binary.LittleEndian.Uint32(packet.Body[14:18]) != 101 {
		t.Fatalf("terminal layer exit response=%+v body=%x rest=%x", packet.Header, packet.Body, rest)
	}
	after := runtime.Session.Snapshot()
	chain := runtime.LayerChains[baseScene.Coordinate]
	if after.Scene.Coordinate != (worldmap.RoomCoordinate{X: 1, Y: 0}) || after.Scene.Map.Map.ID != 101 ||
		runtime.LayeredMapActive || runtime.LayeredMapIndex != -1 || chain == nil || !chain.Consumed || chain.FinalAckPending {
		t.Fatalf("terminal layer exit scene=%+v active=%t index=%d chain=%+v", after.Scene, runtime.LayeredMapActive, runtime.LayeredMapIndex, chain)
	}
}

func TestHandleDungeonMoveMapCannotSkipPendingLayerWithExplicitAdjacentTarget(t *testing.T) {
	service, runtime := prepareSyntheticMoveRuntimeFromPVF(t, bridgeDungeonTerminalLayerExitPVF(true))
	service.options.gameUpperBodyCodec = gameUpperBodyCodecPlain
	conn := &bufferConn{}
	session := &gameSession{conn: conn, connID: "dungeon-pending-layer-explicit-target-test", selectedCharacterID: 99, dungeon: dungeonSessionState{runtime: runtime}}
	if _, err := runtime.Room.AnnounceAllActors(runtime.Session); err != nil {
		t.Fatalf("announce base actors: %v", err)
	}
	if err := runtime.cacheCurrentDungeonRoom(); err != nil {
		t.Fatalf("cache announced base: %v", err)
	}
	baseScene, ok := runtime.Session.Scene()
	if !ok {
		t.Fatal("base scene missing")
	}
	layerBody := make([]byte, dungeoncmd.MoveMapRequestSize)
	layerBody[0] = byte(baseScene.Coordinate.X)
	layerBody[1] = byte(baseScene.Coordinate.Y)
	layerBody[10] = 1
	if err := service.handleDungeonMoveMap(session, layerBody); err != nil {
		t.Fatal(err)
	}
	conn.write.Reset()
	layerRoom := runtime.Room.Snapshot()
	if _, cleared, err := runtime.Room.CommitActorDeathReport(layerRoom.Monsters[0].ObjectKey, runtime.Session); err != nil || !cleared {
		t.Fatalf("clear intermediate layer cleared=%t err=%v", cleared, err)
	}
	exitBody := make([]byte, dungeoncmd.MoveMapRequestSize)
	exitBody[0] = 1
	if err := service.handleDungeonMoveMap(session, exitBody); err != nil {
		t.Fatal(err)
	}
	after := runtime.Session.Snapshot()
	if conn.write.Len() != 0 || after.Scene.Map.Map.ID != 201 || !runtime.LayeredMapActive || runtime.LayeredMapIndex != 0 {
		t.Fatalf("pending layer was skipped bytes=%x scene=%+v active=%t index=%d", conn.write.Bytes(), after.Scene, runtime.LayeredMapActive, runtime.LayeredMapIndex)
	}
}

func TestHandleDungeonMoveMapOrdinarySameCoordinateStillRejected(t *testing.T) {
	service, runtime := prepareSyntheticMoveRuntimeFromPVF(t, bridgeDungeonLayerPVF(false))
	service.options.gameUpperBodyCodec = gameUpperBodyCodecPlain
	conn := &bufferConn{}
	session := &gameSession{conn: conn, connID: "ordinary-same-coordinate-test", selectedCharacterID: 99, dungeon: dungeonSessionState{runtime: runtime}}
	before := runtime.Session.Snapshot()
	body := make([]byte, dungeoncmd.MoveMapRequestSize)
	body[0] = byte(before.Scene.Coordinate.X)
	body[1] = byte(before.Scene.Coordinate.Y)
	if err := service.handleDungeonMoveMap(session, body); err != nil {
		t.Fatalf("ordinary same-coordinate move: %v", err)
	}
	after := runtime.Session.Snapshot()
	if conn.write.Len() != 0 || after.Scene.Map.Map.ID != before.Scene.Map.Map.ID || after.Scene.Coordinate != before.Scene.Coordinate {
		t.Fatalf("ordinary same-coordinate move mutated state: bytes=%x before=%+v after=%+v", conn.write.Bytes(), before, after)
	}
	if runtime.LayeredMapActive || runtime.LayeredMapIndex != -1 {
		t.Fatalf("ordinary rejection changed layer state active=%t index=%d", runtime.LayeredMapActive, runtime.LayeredMapIndex)
	}
}

func TestOrdinaryFinalClearDefersWhileExplicitLayerRemains(t *testing.T) {
	service, runtime := prepareSyntheticMoveRuntimeFromPVF(t, bridgeDungeonLayerPVF(false))
	service.options.gameUpperBodyCodec = gameUpperBodyCodecPlain
	conn := &bufferConn{}
	session := &gameSession{conn: conn, connID: "pending-layer-final-clear-test", selectedCharacterID: 99, dungeon: dungeonSessionState{runtime: runtime}}
	scene, ok := runtime.Session.Scene()
	if !ok || !scene.Cleared || !scene.Boss {
		t.Fatalf("base scene must be an intrinsically-cleared boss room: %+v ok=%t", scene, ok)
	}
	if err := service.completeCurrentDungeonOrdinaryFinalRoomAfterDeathLocked(session, runtime, scene, 0); err != nil {
		t.Fatalf("defer base completion: %v", err)
	}
	after := runtime.Session.Snapshot()
	if after.Run.Status != worldmap.DungeonRunActive || runtime.ordinaryFinalRoomClearAccepted ||
		runtime.bossDieCheckAccepted || runtime.settlementEntrySent || conn.write.Len() != 0 {
		t.Fatalf("base layer completed early: run=%+v ordinary=%t boss=%t settlement=%t bytes=%x",
			after.Run, runtime.ordinaryFinalRoomClearAccepted, runtime.bossDieCheckAccepted, runtime.settlementEntrySent, conn.write.Bytes())
	}
}

func TestBossDieCheckDefersWhileExplicitLayerRemains(t *testing.T) {
	service, runtime := prepareSyntheticMoveRuntimeFromPVF(t, bridgeDungeonLayerPVF(true))
	service.options.gameUpperBodyCodec = gameUpperBodyCodecPlain
	conn := &bufferConn{}
	session := &gameSession{conn: conn, connID: "pending-layer-op117-test", selectedCharacterID: 99, dungeon: dungeonSessionState{runtime: runtime}}
	if _, err := runtime.Room.AnnounceAllActors(runtime.Session); err != nil {
		t.Fatalf("announce base boss: %v", err)
	}
	room := runtime.Room.Snapshot()
	if len(room.Monsters) != 1 {
		t.Fatalf("base boss room=%+v", room)
	}
	targetKey := room.Monsters[0].ObjectKey
	if _, cleared, err := runtime.Room.CommitActorDeathReport(targetKey, runtime.Session); err != nil || !cleared {
		t.Fatalf("commit base boss death cleared=%t error=%v", cleared, err)
	}
	request := dungeoncmd.BossDieCheckRequest{
		RelatedActorObjectKey: currentSceneActorObjectKey(session.selectedCharacterID),
		TargetObjectKey:       uint16(targetKey),
	}
	if err := service.handleDungeonBossDieCheckLocked(session, runtime, request, true); err != nil {
		t.Fatalf("defer base boss op117: %v", err)
	}
	after := runtime.Session.Snapshot()
	if after.Run.Status != worldmap.DungeonRunActive || runtime.bossDieCheckAccepted ||
		runtime.settlementEntrySent || conn.write.Len() != 0 {
		t.Fatalf("base op117 completed early: run=%+v boss=%t settlement=%t bytes=%x",
			after.Run, runtime.bossDieCheckAccepted, runtime.settlementEntrySent, conn.write.Bytes())
	}
}

func TestCurrentDungeonPendingLayerAllowsConnectedClearMapBaseTarget(t *testing.T) {
	service, runtime := prepareSyntheticMoveRuntimeFromPVF(t, bridgeDungeonLayerPVF(false))
	runtime.Dungeon.Mazes[runtime.MazeIndex].QuestConnection = []int64{0, 3145, -1}
	service.questCatalog = buildDungeonClearMapCompletionCatalog(t, 200)
	scene := runtime.Session.Snapshot().Scene

	pending, layerIndex, layerMapID, err := service.currentDungeonPendingLayer(runtime, scene)
	if err != nil {
		t.Fatal(err)
	}
	if pending || layerIndex != 0 || layerMapID != 201 {
		t.Fatalf("base-target clear-map pending=%t layer_index=%d layer_map=%d, want false/0/201",
			pending, layerIndex, layerMapID)
	}
}

func TestCurrentDungeonPendingLayerKeepsConnectedClearMapLayerTarget(t *testing.T) {
	service, runtime := prepareSyntheticMoveRuntimeFromPVF(t, bridgeDungeonLayerPVF(false))
	runtime.Dungeon.Mazes[runtime.MazeIndex].QuestConnection = []int64{0, 3145, -1}
	service.questCatalog = buildDungeonClearMapCompletionCatalog(t, 201)
	scene := runtime.Session.Snapshot().Scene

	pending, layerIndex, layerMapID, err := service.currentDungeonPendingLayer(runtime, scene)
	if err != nil {
		t.Fatal(err)
	}
	if !pending || layerIndex != 0 || layerMapID != 201 {
		t.Fatalf("layer-target clear-map pending=%t layer_index=%d layer_map=%d, want true/0/201",
			pending, layerIndex, layerMapID)
	}
}

func TestHandleDungeonMoveMapSendsStartMapThenCommitsTargetRoom(t *testing.T) {
	service, runtime := prepareSyntheticMoveRuntime(t, false)
	service.options.gameUpperBodyCodec = gameUpperBodyCodecPlain
	conn := &bufferConn{}
	session := &gameSession{
		conn:                               conn,
		connID:                             "dungeon-move-test",
		selectedCharacterID:                99,
		dungeon:                            dungeonSessionState{runtime: runtime},
		postStartMapPlayerStateSent:        true,
		currentFinishLoadingStateSent:      true,
		currentFinishLoadingCompletionSent: true,
		postFinishLoadingPlayerStateSent:   true,
		selectedEquipmentUpdateSent:        true,
		selectedUserInfoMode3Sent:          true,
		runtimeAfterBlacklistSent:          true,
		runtimeFinishLoadingGateSent:       true,
		fpsFinishLoadingGateSent:           true,
	}
	body := make([]byte, dungeoncmd.MoveMapRequestSize)
	body[0] = 1
	body[1] = 0
	binary.LittleEndian.PutUint32(body[2:6], 123)
	binary.LittleEndian.PutUint32(body[6:10], 456)
	frame, err := dnfproto.BuildChannelPacket(
		uint16(dnfenum.CmdPacketMoveMap),
		body,
		1,
		dnfproto.DefaultChannelClassification,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.handleGameUpper(session, frame); err != nil {
		t.Fatalf("dispatch current EXE move-map: %v", err)
	}
	startMapPacket, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if startMapPacket.Header.MsgID != currentDungeonStartNotification || startMapPacket.Header.Classification != 0 || len(rest) != 0 {
		t.Fatalf("move-map response = header=%+v body=%x rest=%x", startMapPacket.Header, startMapPacket.Body, rest)
	}

	scene, ok := runtime.Session.Scene()
	room := runtime.Room.Snapshot()
	if !ok || scene.Coordinate != (worldmap.RoomCoordinate{X: 1, Y: 0}) || scene.Map.Map.ID != 101 {
		t.Fatalf("committed target scene=%+v ok=%t", scene, ok)
	}
	if room.Coordinate != scene.Coordinate || room.MapID != 101 || len(room.Monsters) != 1 || room.Monsters[0].State != runtimeDungeonMonsterAnnounced {
		t.Fatalf("committed target room=%+v", room)
	}
	if got := scene.RuntimeObjects[room.Monsters[0].ObjectKey]; got != room.Monsters[0].Reference {
		t.Fatalf("target runtime binding=%+v want=%+v", got, room.Monsters[0].Reference)
	}
	wantPacket, err := currentDungeonStartMapFromRuntime(runtime, scene, currentDungeonStartMapState{
		Seed:             runtime.Seed,
		RoomStateValue:   1,
		RoomStateFlag:    1,
		PartyMemberIndex: 0xff,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantBody, err := wantPacket.Build()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(startMapPacket.Body, wantBody) {
		t.Fatalf("target start-map body=%x want=%x", startMapPacket.Body, wantBody)
	}
	if session.currentFinishLoadingStateSent || session.currentFinishLoadingCompletionSent {
		t.Fatalf("target room finish-loading was not rearmed: state=%t completion=%t",
			session.currentFinishLoadingStateSent, session.currentFinishLoadingCompletionSent)
	}
	if !session.postStartMapPlayerStateSent || !session.postFinishLoadingPlayerStateSent ||
		!session.selectedEquipmentUpdateSent || !session.selectedUserInfoMode3Sent ||
		!session.runtimeAfterBlacklistSent || !session.runtimeFinishLoadingGateSent ||
		!session.fpsFinishLoadingGateSent {
		t.Fatalf("ordinary room move reset one-shot player state: %+v", session)
	}

	conn.write.Reset()
	if err := service.sendFinishLoadingStatus(session, make([]byte, 16)); err != nil {
		t.Fatalf("target-room finish-loading: %v", err)
	}
	status, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if status.Header.Classification != dnfproto.DefaultChannelClassification ||
		status.Header.MsgID != uint16(dnfenum.CmdPacketFinishLoading) ||
		!bytes.Equal(status.Body, []byte{1}) {
		t.Fatalf("target-room finish-loading status=%+v body=%x", status.Header, status.Body)
	}
	state, rest := splitGameServerUpperPacket(t, rest)
	if state.Header.Classification != 0 ||
		state.Header.MsgID != uint16(dnfenum.CmdPacketFinishLoading) ||
		len(state.Body) != currentFinishLoadingCharacterStateBodySize {
		t.Fatalf("target-room finish-loading state=%+v body_len=%d", state.Header, len(state.Body))
	}
	commit, rest := splitGameServerUpperPacket(t, rest)
	if commit.Header.Classification != 0 ||
		commit.Header.MsgID != currentIncreaseStatusResultMsgID ||
		!bytes.Equal(commit.Body, make([]byte, currentIncreaseStatusResultBodySize)) ||
		len(rest) != 0 {
		t.Fatalf("target-room finish-loading commit=%+v body=%x rest=%x", commit.Header, commit.Body, rest)
	}
	if !session.currentFinishLoadingStateSent || !session.currentFinishLoadingCompletionSent ||
		!session.postFinishLoadingPlayerStateSent {
		t.Fatalf("target-room finish-loading gates current=%v completion=%v post=%v",
			session.currentFinishLoadingStateSent,
			session.currentFinishLoadingCompletionSent,
			session.postFinishLoadingPlayerStateSent)
	}

	conn.write.Reset()
	if err := service.sendFinishLoadingStatus(session, make([]byte, 16)); err != nil {
		t.Fatalf("duplicate target-room finish-loading: %v", err)
	}
	status, rest = splitGameServerUpperPacket(t, conn.write.Bytes())
	if status.Header.Classification != dnfproto.DefaultChannelClassification ||
		status.Header.MsgID != uint16(dnfenum.CmdPacketFinishLoading) ||
		!bytes.Equal(status.Body, []byte{1}) || len(rest) != 0 {
		t.Fatalf("duplicate target-room finish-loading status=%+v body=%x rest=%x", status.Header, status.Body, rest)
	}
}

func TestHandleDungeonMoveMapCancelsFailedTownReturnAndStillSendsExactlyOneStartMap(t *testing.T) {
	service, runtime := prepareSyntheticMoveRuntime(t, false)
	service.options.gameUpperBodyCodec = gameUpperBodyCodecPlain
	runtime.townReturnPending = true
	runtime.townReturnOp24Sent = true
	runtime.townReturnRequestMsgID = uint16(dnfenum.CmdPacketBack2Village)
	runtime.townReturnSource = "test_failed_town_transition"
	runtime.townReturnTransition = currentDungeonTownTransition{TownID: 7, AreaID: 3}
	conn := &bufferConn{}
	session := &gameSession{
		conn:                          conn,
		connID:                        "dungeon-move-after-failed-town-return-test",
		selectedCharacterID:           99,
		dungeon:                       dungeonSessionState{runtime: runtime},
		currentFinishLoadingStateSent: true,
	}
	body := make([]byte, dungeoncmd.MoveMapRequestSize)
	body[0] = 1
	if err := service.handleDungeonMoveMap(session, body); err != nil {
		t.Fatal(err)
	}
	startMap, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if startMap.Header.MsgID != currentDungeonStartNotification || startMap.Header.Classification != 0 || len(rest) != 0 {
		t.Fatalf("move after failed return response=%+v body=%x rest=%x", startMap.Header, startMap.Body, rest)
	}
	if runtime.townReturnPending || runtime.townReturnOp24Sent || session.dungeon.runtime != runtime {
		t.Fatalf("move retained failed return or lost runtime: owner=%p state=%+v", session.dungeon.runtime, runtime)
	}
	if snapshot := runtime.Session.Snapshot(); snapshot.Run.Current != (worldmap.RoomCoordinate{X: 1, Y: 0}) {
		t.Fatalf("move after failed return did not commit target=%+v", snapshot)
	}
}

func TestHandleDungeonMoveMapKeepsFailedTownReturnPendingWhenMovePreflightFails(t *testing.T) {
	service, runtime := prepareSyntheticMoveRuntime(t, false)
	service.options.gameUpperBodyCodec = gameUpperBodyCodecPlain
	runtime.townReturnPending = true
	runtime.townReturnOp24Sent = true
	runtime.townReturnRequestMsgID = uint16(dnfenum.CmdPacketBack2Village)
	runtime.townReturnSource = "test_failed_town_transition"
	runtime.townReturnTransition = currentDungeonTownTransition{TownID: 7, AreaID: 3}
	conn := &bufferConn{}
	session := &gameSession{
		conn:                conn,
		connID:              "invalid-dungeon-move-after-failed-town-return-test",
		dungeon:             dungeonSessionState{runtime: runtime},
		selectedCharacterID: 99,
	}
	body := make([]byte, dungeoncmd.MoveMapRequestSize)
	body[0] = 2
	if err := service.handleDungeonMoveMap(session, body); err != nil {
		t.Fatal(err)
	}
	if conn.write.Len() != 0 {
		t.Fatalf("invalid move emitted response=%x", conn.write.Bytes())
	}
	if !runtime.townReturnPending || !runtime.townReturnOp24Sent || session.dungeon.runtime != runtime {
		t.Fatalf("invalid move cancelled pending return or lost runtime: owner=%p state=%+v", session.dungeon.runtime, runtime)
	}
	if snapshot := runtime.Session.Snapshot(); snapshot.Run.Current != (worldmap.RoomCoordinate{}) {
		t.Fatalf("invalid move changed room=%+v", snapshot)
	}
}

func TestHandleDungeonMoveMapRevisitsCachedRoomsWithoutRespawningActors(t *testing.T) {
	service, runtime := prepareSyntheticMoveRuntime(t, false)
	service.options.gameUpperBodyCodec = gameUpperBodyCodecPlain

	const targetSeed = uint32(0x44332211)
	service.dungeonSeed = func() (uint32, error) { return targetSeed, nil }

	conn := &bufferConn{}
	session := &gameSession{
		conn:                          conn,
		connID:                        "dungeon-move-revisit-cache-test",
		selectedCharacterID:           99,
		dungeon:                       dungeonSessionState{runtime: runtime},
		currentFinishLoadingStateSent: true,
	}
	move := func(x, y byte) []byte {
		t.Helper()
		conn.write.Reset()
		body := make([]byte, dungeoncmd.MoveMapRequestSize)
		body[0] = x
		body[1] = y
		if err := service.handleDungeonMoveMap(session, body); err != nil {
			t.Fatalf("move to (%d,%d): %v", x, y, err)
		}
		packet, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
		if packet.Header.MsgID != currentDungeonStartNotification ||
			packet.Header.Classification != 0 || len(rest) != 0 {
			t.Fatalf("move to (%d,%d) response = header=%+v body=%x rest=%x", x, y, packet.Header, packet.Body, rest)
		}
		return append([]byte(nil), packet.Body...)
	}
	wantRevisitBody := func(x, y byte, seed uint32, _ uint32) []byte {
		t.Helper()
		body := make([]byte, 16)
		body[0] = x
		body[1] = y
		binary.LittleEndian.PutUint32(body[3:7], seed)
		binary.LittleEndian.PutUint32(body[9:13], 1)
		body[13] = 0
		body[14] = 0
		body[15] = 0xff
		return body
	}

	startRoom := runtime.Room
	startScene := runtime.Session.Snapshot().Scene
	startSeed := runtime.Seed
	startNextObjectKey := runtime.NextObjectKey
	if !startScene.Cleared || len(startScene.BlockingHostiles) != 0 {
		t.Fatalf("synthetic start room must be empty and cleared: %+v", startScene)
	}

	firstVisitBody := move(1, 0)
	if len(firstVisitBody) != 44 || firstVisitBody[13] != 1 ||
		binary.LittleEndian.Uint32(firstVisitBody[14:18]) != 101 || firstVisitBody[18] != 1 {
		t.Fatalf("first target visit op29 must be full flag1 with one actor: len=%d body=%x", len(firstVisitBody), firstVisitBody)
	}
	if binary.LittleEndian.Uint32(firstVisitBody[3:7]) != targetSeed {
		t.Fatalf("first target visit seed=%08x want=%08x", binary.LittleEndian.Uint32(firstVisitBody[3:7]), targetSeed)
	}
	targetRoom := runtime.Room
	if targetRoom == startRoom || runtime.Seed != targetSeed {
		t.Fatalf("first target visit room=%p start_room=%p seed=%08x", targetRoom, startRoom, runtime.Seed)
	}
	targetSnapshot := targetRoom.Snapshot()
	if len(targetSnapshot.Monsters) != 1 || targetSnapshot.Monsters[0].State != runtimeDungeonMonsterAnnounced {
		t.Fatalf("first target visit runtime room=%+v", targetSnapshot)
	}
	targetObjectKey := targetSnapshot.Monsters[0].ObjectKey
	if binary.LittleEndian.Uint16(firstVisitBody[25:27]) != uint16(targetObjectKey) {
		t.Fatalf("first target visit actor key=%d want=%d body=%x",
			binary.LittleEndian.Uint16(firstVisitBody[25:27]), targetObjectKey, firstVisitBody)
	}
	nextObjectKeyAfterFirstVisit := runtime.NextObjectKey
	if nextObjectKeyAfterFirstVisit != startNextObjectKey+1 {
		t.Fatalf("first target visit next object key=%d want=%d", nextObjectKeyAfterFirstVisit, startNextObjectKey+1)
	}
	if len(runtime.Rooms) != 2 {
		t.Fatalf("first target visit cached rooms=%d want=2", len(runtime.Rooms))
	}

	if _, cleared, err := targetRoom.CommitActorDeathReport(targetObjectKey, runtime.Session); err != nil || !cleared {
		t.Fatalf("clear target room object_key=%d cleared=%t error=%v", targetObjectKey, cleared, err)
	}
	deadTargetScene := runtime.Session.Snapshot().Scene
	if !deadTargetScene.Cleared || len(deadTargetScene.DefeatedObjects) != 1 ||
		deadTargetScene.DefeatedObjects[0] != targetObjectKey {
		t.Fatalf("target death state=%+v", deadTargetScene)
	}

	backBody := move(0, 0)
	wantBackBody := wantRevisitBody(0, 0, startSeed, 100)
	if !bytes.Equal(backBody, wantBackBody) {
		t.Fatalf("backward revisit op29=%x want exact 16-byte mode0 body=%x", backBody, wantBackBody)
	}
	if runtime.Room != startRoom || runtime.Seed != startSeed || runtime.NextObjectKey != nextObjectKeyAfterFirstVisit {
		t.Fatalf("backward revisit owner room=%p want=%p seed=%08x want=%08x next=%d want=%d",
			runtime.Room, startRoom, runtime.Seed, startSeed, runtime.NextObjectKey, nextObjectKeyAfterFirstVisit)
	}
	if len(runtime.Rooms) != 2 {
		t.Fatalf("backward revisit created a room cache entry: count=%d", len(runtime.Rooms))
	}
	cachedTarget := runtime.Rooms[runtimeDungeonRoomKey{X: 1, Y: 0, MapID: 101}]
	if cachedTarget == nil || cachedTarget.Room != targetRoom || cachedTarget.Seed != targetSeed ||
		!cachedTarget.Scene.Cleared || len(cachedTarget.Scene.DefeatedObjects) != 1 ||
		cachedTarget.Scene.DefeatedObjects[0] != targetObjectKey {
		t.Fatalf("backward revisit did not preserve target cache=%+v", cachedTarget)
	}

	forwardRevisitBody := move(1, 0)
	wantForwardRevisitBody := wantRevisitBody(1, 0, targetSeed, 101)
	if !bytes.Equal(forwardRevisitBody, wantForwardRevisitBody) {
		t.Fatalf("forward revisit op29=%x want exact 16-byte mode0 body=%x", forwardRevisitBody, wantForwardRevisitBody)
	}
	if runtime.Room != targetRoom || runtime.Seed != targetSeed || runtime.NextObjectKey != nextObjectKeyAfterFirstVisit {
		t.Fatalf("forward revisit owner room=%p want=%p seed=%08x want=%08x next=%d want=%d",
			runtime.Room, targetRoom, runtime.Seed, targetSeed, runtime.NextObjectKey, nextObjectKeyAfterFirstVisit)
	}
	restoredTarget := targetRoom.Snapshot()
	restoredScene := runtime.Session.Snapshot().Scene
	if len(restoredTarget.Monsters) != 1 || restoredTarget.Monsters[0].ObjectKey != targetObjectKey ||
		restoredTarget.Monsters[0].State != runtimeDungeonMonsterDefeated {
		t.Fatalf("forward revisit respawned or re-announced target room=%+v", restoredTarget)
	}
	if !restoredScene.Cleared || len(restoredScene.DefeatedObjects) != 1 ||
		restoredScene.DefeatedObjects[0] != targetObjectKey ||
		restoredScene.RuntimeObjects[targetObjectKey] != restoredTarget.Monsters[0].Reference {
		t.Fatalf("forward revisit did not restore target death bindings=%+v", restoredScene)
	}
	if len(runtime.Rooms) != 2 {
		t.Fatalf("forward revisit created a room cache entry: count=%d", len(runtime.Rooms))
	}
}

func TestHandleDungeonMoveMapRejectsUnclearedRequestWithoutRetainingIt(t *testing.T) {
	for _, fixture := range []struct {
		name   string
		source bridgePVFSource
		job    string
	}{
		{name: "PVF tutorial other profession", source: func() bridgePVFSource {
			source := bridgeGenericDungeonMovePVF(true)
			source["dungeon/test.dgn"] += "[tutorial dungeon]\n1\n"
			return source
		}(), job: "12"},
		{name: "ordinary dungeon", source: bridgeGenericDungeonMovePVF(true), job: "0"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			service, runtime := prepareSyntheticMoveRuntimeFromPVF(t, fixture.source)
			runtime.Character.Job = fixture.job
			conn := &bufferConn{}
			session := &gameSession{
				conn:                          conn,
				connID:                        "dungeon-move-one-request-one-response-test",
				dungeon:                       dungeonSessionState{runtime: runtime},
				currentFinishLoadingStateSent: true,
			}
			body := make([]byte, dungeoncmd.MoveMapRequestSize)
			body[0] = 1
			before := runtime.Session.Snapshot()
			if err := service.handleDungeonMoveMap(session, body); err != nil {
				t.Fatal(err)
			}
			after := runtime.Session.Snapshot()
			if conn.write.Len() != 0 {
				t.Fatalf("uncleared op45 emitted unsolicited response=%x", conn.write.Bytes())
			}
			if after.Run.Current != before.Run.Current || after.Scene.Coordinate != before.Scene.Coordinate ||
				after.Scene.Map.Map.ID != before.Scene.Map.Map.ID || after.Scene.Cleared {
				t.Fatalf("uncleared op45 changed source: before=%+v after=%+v", before, after)
			}
			if !session.currentFinishLoadingStateSent {
				t.Fatal("rejected op45 rearmed FINISH_LOADING")
			}
		})
	}
}

func TestHandleDungeonMoveMapRequiresFreshOp45AfterEveryBlockingMonsterDies(t *testing.T) {
	source := bridgeGenericDungeonMovePVF(true)
	source["dungeon/test.dgn"] += "[tutorial dungeon]\n1\n"
	source["map/dungeon/test/start.map"] += "3001 10 0 140 200 0 0 0 `[fixed]` `[normal]`\n"
	service, runtime := prepareSyntheticMoveRuntimeFromPVF(t, source)
	service.options.gameUpperBodyCodec = gameUpperBodyCodecPlain
	runtime.Character.Job = "12"
	if _, err := runtime.Room.AnnounceAllActors(runtime.Session); err != nil {
		t.Fatal(err)
	}
	monsters := runtime.Room.Snapshot().Monsters
	if len(monsters) != 2 {
		t.Fatalf("fixture monsters=%+v", monsters)
	}
	conn := &bufferConn{}
	session := &gameSession{
		conn:                          conn,
		connID:                        "dungeon-move-all-blockers-fresh-request-test",
		selectedCharacterID:           99,
		dungeon:                       dungeonSessionState{runtime: runtime},
		currentFinishLoadingStateSent: true,
	}
	moveBody := make([]byte, dungeoncmd.MoveMapRequestSize)
	moveBody[0] = 1
	if err := service.handleDungeonMoveMap(session, moveBody); err != nil {
		t.Fatal(err)
	}
	if conn.write.Len() != 0 {
		t.Fatalf("early op45 wrote=%x", conn.write.Bytes())
	}

	kill := func(monster runtimeDungeonMonster) {
		t.Helper()
		deathBody := make([]byte, dungeoncmd.DieMonsterRequestSize)
		binary.LittleEndian.PutUint32(deathBody[0:4], monster.ObjectKey)
		binary.LittleEndian.PutUint16(deathBody[4:6], currentSceneActorObjectKey(99))
		if err := service.handleDungeonMonsterDeath(session, deathBody); err != nil {
			t.Fatal(err)
		}
	}
	kill(monsters[0])
	firstDeath, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if firstDeath.Header.MsgID != uint16(dnfenum.CmdPacketNotifyDieMonster) || len(rest) != 0 {
		t.Fatalf("first death stream header=%+v rest=%x", firstDeath.Header, rest)
	}
	if snapshot := runtime.Session.Snapshot(); snapshot.Scene.Cleared || snapshot.Run.Current != (worldmap.RoomCoordinate{}) {
		t.Fatalf("one of two deaths opened room=%+v", snapshot)
	}
	conn.write.Reset()
	if err := service.handleDungeonMoveMap(session, moveBody); err != nil {
		t.Fatal(err)
	}
	if conn.write.Len() != 0 {
		t.Fatalf("partial-clear op45 wrote=%x", conn.write.Bytes())
	}

	kill(monsters[1])
	finalDeath, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if finalDeath.Header.MsgID != uint16(dnfenum.CmdPacketNotifyDieMonster) || len(rest) != 0 {
		t.Fatalf("final death proactively opened room header=%+v rest=%x", finalDeath.Header, rest)
	}
	if snapshot := runtime.Session.Snapshot(); !snapshot.Scene.Cleared || snapshot.Run.Current != (worldmap.RoomCoordinate{}) {
		t.Fatalf("all deaths did not leave a cleared source awaiting fresh op45=%+v", snapshot)
	}
	conn.write.Reset()
	if err := service.handleDungeonMoveMap(session, moveBody); err != nil {
		t.Fatal(err)
	}
	startMap, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if startMap.Header.MsgID != currentDungeonStartNotification || len(rest) != 0 {
		t.Fatalf("fresh cleared op45 response header=%+v rest=%x", startMap.Header, rest)
	}
	if snapshot := runtime.Session.Snapshot(); snapshot.Run.Current != (worldmap.RoomCoordinate{X: 1, Y: 0}) {
		t.Fatalf("fresh cleared op45 did not commit target=%+v", snapshot)
	}
}

func TestHandleDungeonMoveMapRejectsInvalidTargetWhileUncleared(t *testing.T) {
	service, runtime := prepareSyntheticMoveRuntime(t, true)
	conn := &bufferConn{}
	session := &gameSession{
		conn:    conn,
		connID:  "dungeon-move-invalid-target-test",
		dungeon: dungeonSessionState{runtime: runtime},
	}
	body := make([]byte, dungeoncmd.MoveMapRequestSize)
	body[0] = 2

	if err := service.handleDungeonMoveMap(session, body); err != nil {
		t.Fatal(err)
	}
	if conn.write.Len() != 0 {
		t.Fatalf("invalid target emitted response=%x", conn.write.Bytes())
	}
}

func TestHandleDungeonMoveMapRejectsNonPlainBoundariesWithoutWrite(t *testing.T) {
	for _, bodyLen := range []int{
		dungeoncmd.MoveMapRequestSize - 1,
		dungeoncmd.MoveMapRequestSize + 1,
		112, // current native Twofish-padded ciphertext must never be parsed as fields
	} {
		t.Run(fmt.Sprintf("body_%d", bodyLen), func(t *testing.T) {
			service, runtime := prepareSyntheticMoveRuntime(t, false)
			conn := &bufferConn{}
			session := &gameSession{
				conn:                          conn,
				connID:                        "dungeon-move-boundary-test",
				dungeon:                       dungeonSessionState{runtime: runtime},
				currentFinishLoadingStateSent: true,
			}
			body := make([]byte, bodyLen)
			body[0] = 1
			before := runtime.Session.Snapshot()
			beforeRoom := runtime.Room.Snapshot()
			beforeNextObjectKey := runtime.NextObjectKey

			if err := service.handleDungeonMoveMap(session, body); err != nil {
				t.Fatal(err)
			}
			after := runtime.Session.Snapshot()
			afterRoom := runtime.Room.Snapshot()
			if conn.write.Len() != 0 {
				t.Fatalf("invalid boundary emitted response=%x", conn.write.Bytes())
			}
			if after.Run.Current != before.Run.Current || after.Scene.Coordinate != before.Scene.Coordinate || after.Scene.Map.Map.ID != before.Scene.Map.Map.ID {
				t.Fatalf("invalid boundary changed session: before=%+v after=%+v", before, after)
			}
			if afterRoom.Coordinate != beforeRoom.Coordinate || afterRoom.MapID != beforeRoom.MapID || runtime.NextObjectKey != beforeNextObjectKey {
				t.Fatalf("invalid boundary changed runtime room/object keys: before=%+v after=%+v next_before=%d next_after=%d",
					beforeRoom, afterRoom, beforeNextObjectKey, runtime.NextObjectKey)
			}
			if !session.currentFinishLoadingStateSent {
				t.Fatal("invalid boundary rearmed finish-loading state")
			}
		})
	}
}

func TestHandleDungeonMoveMapWriteFailureKeepsFinishLoadingState(t *testing.T) {
	service, runtime := prepareSyntheticMoveRuntime(t, false)
	service.options.gameUpperBodyCodec = gameUpperBodyCodecPlain
	wantErr := errors.New("target op29 write failed")
	conn := &failNthDungeonWriteConn{failAt: 1, err: wantErr}
	session := &gameSession{
		conn:                          conn,
		connID:                        "dungeon-move-write-failure-test",
		dungeon:                       dungeonSessionState{runtime: runtime},
		currentFinishLoadingStateSent: true,
	}
	body := make([]byte, dungeoncmd.MoveMapRequestSize)
	body[0] = 1
	before := runtime.Session.Snapshot()

	if err := service.handleDungeonMoveMap(session, body); !errors.Is(err, wantErr) {
		t.Fatalf("move-map write error=%v want=%v", err, wantErr)
	}
	after := runtime.Session.Snapshot()
	if after.Run.Current != before.Run.Current || after.Scene.Coordinate != before.Scene.Coordinate || after.Scene.Map.Map.ID != before.Scene.Map.Map.ID {
		t.Fatalf("failed op29 write changed session: before=%+v after=%+v", before, after)
	}
	if !session.currentFinishLoadingStateSent {
		t.Fatal("failed op29 write rearmed finish-loading state")
	}
}

func TestPlanCurrentDungeonMoveRollsBackEveryFailedGate(t *testing.T) {
	tests := []struct {
		name         string
		startHostile bool
		mutate       func(*runtimeDungeonState, *dungeoncmd.MoveMapRequest)
		want         error
	}{
		{
			name:         "source room not server-cleared",
			startHostile: true,
			want:         worldmap.ErrRunCurrentRoomNotCleared,
		},
		{
			name: "unproven request tail",
			mutate: func(_ *runtimeDungeonState, request *dungeoncmd.MoveMapRequest) {
				request.OpaqueTail = []byte{1}
			},
			want: errDungeonMoveRequestTailUnknown,
		},
		{
			name: "runtime room owner mismatch",
			mutate: func(runtime *runtimeDungeonState, _ *dungeoncmd.MoveMapRequest) {
				runtime.Room.mapID = 999
			},
			want: errDungeonMoveRuntimeOwnerMismatch,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, runtime := prepareSyntheticMoveRuntime(t, test.startHostile)
			request := dungeoncmd.MoveMapRequest{NextX: 1, NextY: 0}
			if test.mutate != nil {
				test.mutate(runtime, &request)
			}
			before := runtime.Session.Snapshot()
			beforeNextObjectKey := runtime.NextObjectKey
			if _, err := service.planCurrentDungeonMove(runtime, request); !errors.Is(err, test.want) {
				t.Fatalf("plan error = %v, want %v", err, test.want)
			}
			after := runtime.Session.Snapshot()
			if after.Run.Current != before.Run.Current || after.Scene.Coordinate != before.Scene.Coordinate || after.Scene.Map.Map.ID != before.Scene.Map.Map.ID {
				t.Fatalf("failed gate changed session: before=%+v after=%+v", before, after)
			}
			if runtime.NextObjectKey != beforeNextObjectKey {
				t.Fatalf("failed gate advanced object key: before=%d after=%d", beforeNextObjectKey, runtime.NextObjectKey)
			}
		})
	}
}

func prepareSyntheticMoveRuntime(t *testing.T, startHostile bool) (*Service, *runtimeDungeonState) {
	t.Helper()
	return prepareSyntheticMoveRuntimeFromPVF(t, bridgeDungeonMovePVF(startHostile))
}

func prepareSyntheticStoryStageRuntime(t *testing.T) (*Service, *runtimeDungeonState) {
	t.Helper()
	table, resolver, monsters := loadBridgeDungeonStaticData(t, bridgeDungeonStoryStagePVF())
	dungeon, ok := table.FindDungeon(700)
	if !ok || len(dungeon.Mazes) != 1 {
		t.Fatalf("story dungeon=%+v found=%t", dungeon, ok)
	}
	topology, err := worldmap.BuildDungeonLayout(
		resolver,
		700,
		0,
		func(choice worldmap.DungeonMapChoice) (int64, error) { return choice.Candidates[0].ID, nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	run, err := worldmap.NewDungeonRun(topology)
	if err != nil {
		t.Fatal(err)
	}
	dungeonSession, err := worldmap.NewDungeonSession(run)
	if err != nil {
		t.Fatal(err)
	}
	scene, ok := dungeonSession.Scene()
	if !ok || scene.Map.Map.ID != 100 {
		t.Fatalf("story start scene=%+v found=%t", scene, ok)
	}
	room, nextObjectKey, err := newRuntimeDungeonRoom(scene, monsters, firstDungeonMonsterObjectKey)
	if err != nil {
		t.Fatal(err)
	}
	stages, err := resolver.StoryStages(700, 0)
	if err != nil || len(stages) != 3 {
		t.Fatalf("story stages=%+v err=%v", stages, err)
	}
	service := &Service{
		worldMapTable:       table,
		worldMapResolver:    resolver,
		dungeonMonsterTable: monsters,
		dungeonSeed:         func() (uint32, error) { return 1, nil },
	}
	runtime := &runtimeDungeonState{
		Request:         dungeoncmd.SelectDungeonRequest{DungeonID: 700},
		Dungeon:         dungeon,
		MazeIndex:       0,
		Character:       dnfrepo.CharacterRecord{CharacterID: "99", AccountID: "account-1", Level: 20},
		Session:         dungeonSession,
		Room:            room,
		NextObjectKey:   nextObjectKey,
		BossCoordinate:  stages[len(stages)-1].Coordinate,
		BossSet:         true,
		LayeredMapIndex: -1,
		LayerChains:     make(map[worldmap.RoomCoordinate]*runtimeDungeonLayerChain),
		StoryStages:     append([]worldmap.DungeonStoryStage(nil), stages...),
		StoryStageIndex: -1,
		Seed:            1,
		Rooms:           make(map[runtimeDungeonRoomKey]*runtimeDungeonRoomVisit),
	}
	runtime.Rooms[runtimeDungeonRoomKeyFromScene(scene)] = &runtimeDungeonRoomVisit{
		Scene: scene, Room: room, Seed: 1, DropRNG: 1,
	}
	return service, runtime
}

func prepareSyntheticMoveRuntimeFromPVF(t *testing.T, source bridgePVFSource) (*Service, *runtimeDungeonState) {
	t.Helper()
	table, resolver, monsters := loadBridgeDungeonStaticData(t, source)
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(context.Background(), dnfrepo.CharacterRecord{
		CharacterID: "99",
		AccountID:   "account-1",
		Job:         atSwordmanTutorialJob,
		Level:       20,
		Stats:       map[string]int64{"fatigue": 100},
	}); err != nil {
		t.Fatal(err)
	}
	service := &Service{
		options:             options{accountID: "account-1"},
		worldMapTable:       table,
		worldMapResolver:    resolver,
		dungeonMonsterTable: monsters,
		dungeonChoice:       func(int) (int, error) { return 0, nil },
		dungeonSeed:         func() (uint32, error) { return 1, nil },
		repositoryProvider:  func() (dnfrepo.Group, bool) { return repositories, true },
	}
	if _, ok := source[defaultDungeonAICharacterList]; ok {
		aiCatalog, err := newPVFDungeonAICharacterCatalog(source)
		if err != nil {
			t.Fatalf("load move-test AI catalog: %v", err)
		}
		service.dungeonAICharacterTable = aiCatalog
	}
	runtime, _, err := service.prepareDungeonRuntime(
		context.Background(),
		&gameSession{selectedCharacterID: 99},
		dungeoncmd.SelectDungeonRequest{DungeonID: 700, Difficulty: 1},
	)
	if err != nil {
		t.Fatalf("prepare move runtime: %v", err)
	}
	return service, runtime
}

func bridgeDungeonMovePVF(startHostile bool) bridgePVFSource {
	startMonster := ""
	if startHostile {
		startMonster = "[monster]\n3001 10 0 100 200 0 0 0 `[fixed]` `[normal]`\n"
	}
	return bridgePVFSource{
		worldmap.DefaultMapList: "100 `Cataclysm/NewTutorial/ATSwordman/start.map`\n101 `Cataclysm/NewTutorial/ATSwordman/next.map`\n",
		bridgeDungeonMoveStartMapPath: "[map name]\n`start`\n" +
			"[dungeon]\n700\n" +
			"[type]\n`[start]`\n" +
			startMonster,
		bridgeDungeonMoveNextMapPath: "[map name]\n`next`\n" +
			"[dungeon]\n700\n" +
			"[monster]\n3001 10 0 100 200 0 0 0 `[fixed]` `[normal]`\n",
		worldmap.DefaultDungeonList: "700 `Cataclysm/NewTutorial/Tutorial_ATSwordman.dgn`\n",
		bridgeDungeonMoveDungeonPath: "[name]\n`Synthetic Move Dungeon`\n" +
			"[minimum required level]\n10\n" +
			"[basis level]\n20\n" +
			"[limit party count]\n1\n" +
			"[tutorial dungeon]\n1\n" +
			"[maze info]\n" +
			"[size]\n2 1\n" +
			"[greed]\n`AA`\n" +
			"[map specification]\n`map` 0 0 100 `boss` 1 0 101\n" +
			"[start map]\n0 0\n" +
			"[boss map]\n1 0\n",
		worldmap.DefaultWorldMapList: "1 `test.wdm`\n",
		"worldmap/test.wdm":          "[name]\n`Synthetic Area`\n[dungeon]\n700 -1\n[/dungeon]\n",
		dnfmonster.DefaultList:       "3001 `test.gob`\n",
		"monster/test.gob": "[name]\n`Synthetic Goblin`\n" +
			"[level]\n10\n" +
			"[hp]\n500\n" +
			"[exp]\n25\n",
	}
}

func bridgeDungeonStoryStagePVF() bridgePVFSource {
	return bridgePVFSource{
		worldmap.DefaultMapList: "100 `dungeon/story/start.map`\n110 `dungeon/story/base.map`\n" +
			"200 `dungeon/story/stage0.map`\n201 `dungeon/story/stage1.map`\n202 `dungeon/story/final.map`\n",
		"map/dungeon/story/start.map": "[map name]\n`start`\n[dungeon]\n700\n[type]\n`[start]`\n",
		"map/dungeon/story/base.map":  "[map name]\n`base`\n[dungeon]\n700\n[type]\n`[normal]`\n",
		"map/dungeon/story/stage0.map": "[map name]\n`stage0`\n[dungeon]\n700\n[type]\n`[dummy]`\n" +
			"[monster]\n3001 10 0 100 200 0 0 0 `[fixed]` `[normal]`\n" +
			"3001 10 0 200 200 0 0 0 `[fixed]` `[boss]`\n",
		"map/dungeon/story/stage1.map": "[map name]\n`stage1`\n[dungeon]\n700\n[type]\n`[dummy]`\n" +
			"[monster]\n3001 10 0 100 200 0 0 0 `[fixed]` `[normal]`\n" +
			"3001 10 0 200 200 0 0 0 `[fixed]` `[boss]`\n",
		"map/dungeon/story/final.map": "[map name]\n`final`\n[dungeon]\n700\n[type]\n`[boss]`\n" +
			"[monster]\n3001 10 0 200 200 0 0 0 `[fixed]` `[boss]`\n",
		worldmap.DefaultDungeonList: "700 `story.dgn`\n",
		"dungeon/story.dgn": "[name]\n`Synthetic Story Dungeon`\n" +
			"[minimum required level]\n10\n[basis level]\n20\n[limit party count]\n1\n" +
			"[maze info]\n[size]\n3 1\n[greed]\n`AAA`\n" +
			"[map specification]\n" +
			"`map` 0 0 100 `map` 1 0 201 110 `map` 2 0 200 `boss` 2 0 202\n" +
			"[start map]\n0 0\n[boss map]\n2 0 1 0 2 0\n[quest connection]\n0 9000 -1\n",
		worldmap.DefaultWorldMapList: "1 `test.wdm`\n",
		"worldmap/test.wdm":          "[name]\n`Synthetic Area`\n[dungeon]\n700 -1\n[/dungeon]\n",
		dnfmonster.DefaultList:       "3001 `test.gob`\n",
		"monster/test.gob": "[name]\n`Synthetic Story Monster`\n" +
			"[level]\n10\n[hp]\n500\n[exp]\n0\n",
	}
}

func bridgeDungeonLayerPVF(startHostile bool) bridgePVFSource {
	baseMonster := ""
	if startHostile {
		baseMonster = "[monster]\n3001 10 0 100 200 0 0 0 `[fixed]` `[boss]`\n"
	}
	return bridgePVFSource{
		worldmap.DefaultMapList: "200 `dungeon/layer/base_boss.map`\n201 `dungeon/layer/story_layer.map`\n",
		"map/dungeon/layer/base_boss.map": "[map name]\n`base boss`\n" +
			"[dungeon]\n700\n[type]\n`[boss]`\n" + baseMonster,
		"map/dungeon/layer/story_layer.map": "[map name]\n`story layer`\n" +
			"[dungeon]\n700\n[type]\n`[boss]`\n" +
			"[monster]\n3001 10 0 120 200 0 0 0 `[fixed]` `[boss]`\n",
		worldmap.DefaultDungeonList: "700 `layer.dgn`\n",
		"dungeon/layer.dgn": "[name]\n`Synthetic Layered Dungeon`\n" +
			"[minimum required level]\n10\n[basis level]\n20\n[limit party count]\n1\n" +
			"[maze info]\n[size]\n1 1\n[greed]\n`A`\n" +
			"[map specification]\n`boss` 0 0 200\n" +
			"[start map]\n0 0\n[boss map]\n0 0\n" +
			"[layered map specification]\n0 0 201\n",
		worldmap.DefaultWorldMapList: "1 `test.wdm`\n",
		"worldmap/test.wdm":          "[name]\n`Synthetic Area`\n[dungeon]\n700 -1\n[/dungeon]\n",
		dnfmonster.DefaultList:       "3001 `test.gob`\n",
		"monster/test.gob": "[name]\n`Synthetic Goblin`\n" +
			"[level]\n10\n[hp]\n500\n[exp]\n25\n",
	}
}

func bridgeDungeonMultiLayerPVF() bridgePVFSource {
	source := bridgeDungeonLayerPVF(true)
	source[worldmap.DefaultMapList] = "200 `dungeon/layer/base_boss.map`\n" +
		"201 `dungeon/layer/story_layer.map`\n" +
		"202 `dungeon/layer/story_layer_2.map`\n"
	source["map/dungeon/layer/story_layer_2.map"] = "[map name]\n`story layer 2`\n" +
		"[dungeon]\n700\n[type]\n`[boss]`\n" +
		"[monster]\n3001 10 0 140 200 0 0 0 `[fixed]` `[boss]`\n"
	source["dungeon/layer.dgn"] = "[name]\n`Synthetic Multi Layered Dungeon`\n" +
		"[minimum required level]\n10\n[basis level]\n20\n[limit party count]\n1\n" +
		"[maze info]\n[size]\n1 1\n[greed]\n`A`\n" +
		"[map specification]\n`boss` 0 0 200\n" +
		"[start map]\n0 0\n[boss map]\n0 0\n" +
		"[layered map specification]\n0 0 201 202\n"
	return source
}

func bridgeDungeonTerminalLayerExitPVF(multipleLayers bool) bridgePVFSource {
	source := bridgeDungeonLayerPVF(true)
	source[worldmap.DefaultMapList] = "200 `dungeon/layer/base_boss.map`\n" +
		"201 `dungeon/layer/story_layer.map`\n" +
		"101 `Cataclysm/NewTutorial/ATSwordman/next.map`\n"
	source[bridgeDungeonMoveNextMapPath] = "[map name]\n`next`\n[dungeon]\n700\n" +
		"[monster]\n3001 10 0 100 200 0 0 0 `[fixed]` `[normal]`\n"
	layerIDs := "201"
	if multipleLayers {
		source[worldmap.DefaultMapList] += "202 `dungeon/layer/story_layer_2.map`\n"
		source["map/dungeon/layer/story_layer_2.map"] = "[map name]\n`story layer 2`\n" +
			"[dungeon]\n700\n[type]\n`[normal]`\n" +
			"[monster]\n3001 10 0 140 200 0 0 0 `[fixed]` `[normal]`\n"
		layerIDs += " 202"
	}
	source["dungeon/layer.dgn"] = "[name]\n`Synthetic Terminal Layer Exit Dungeon`\n" +
		"[minimum required level]\n10\n[basis level]\n20\n[limit party count]\n1\n" +
		"[maze info]\n[size]\n2 1\n[greed]\n`AA`\n" +
		"[map specification]\n`map` 0 0 200 `boss` 1 0 101\n" +
		"[start map]\n0 0\n[boss map]\n1 0\n" +
		"[layered map specification]\n0 0 " + layerIDs + "\n"
	return source
}

func bridgeDungeonMoveBranchPVF() bridgePVFSource {
	source := bridgeDungeonMovePVF(true)
	source[worldmap.DefaultMapList] = "100 `Cataclysm/NewTutorial/ATSwordman/start.map`\n" +
		"101 `Cataclysm/NewTutorial/ATSwordman/next.map`\n" +
		"102 `Cataclysm/NewTutorial/ATSwordman/left.map`\n"
	source[bridgeDungeonMoveLeftMapPath] = "[map name]\n`left`\n[dungeon]\n700\n"
	source[bridgeDungeonMoveDungeonPath] = "[name]\n`Synthetic Branch Move Dungeon`\n" +
		"[minimum required level]\n10\n" +
		"[basis level]\n20\n" +
		"[limit party count]\n1\n" +
		"[tutorial dungeon]\n1\n" +
		"[maze info]\n" +
		"[size]\n3 1\n" +
		"[greed]\n`AAA`\n" +
		"[map specification]\n`map` 0 0 102 `map` 1 0 100 `boss` 2 0 101\n" +
		"[start map]\n1 0\n" +
		"[boss map]\n2 0\n"
	return source
}

func bridgeGenericDungeonMovePVF(startHostile bool) bridgePVFSource {
	startMonster := ""
	if startHostile {
		startMonster = "[monster]\n3001 10 0 100 200 0 0 0 `[fixed]` `[normal]`\n"
	}
	return bridgePVFSource{
		worldmap.DefaultMapList: "100 `dungeon/test/start.map`\n101 `dungeon/test/next.map`\n",
		"map/dungeon/test/start.map": "[map name]\n`start`\n" +
			"[dungeon]\n700\n" +
			"[type]\n`[start]`\n" +
			startMonster,
		"map/dungeon/test/next.map": "[map name]\n`next`\n" +
			"[dungeon]\n700\n" +
			"[monster]\n3001 10 0 100 200 0 0 0 `[fixed]` `[normal]`\n",
		worldmap.DefaultDungeonList: "700 `test.dgn`\n",
		"dungeon/test.dgn": "[name]\n`Synthetic Generic Move Dungeon`\n" +
			"[minimum required level]\n10\n" +
			"[basis level]\n20\n" +
			"[limit party count]\n1\n" +
			"[maze info]\n" +
			"[size]\n2 1\n" +
			"[greed]\n`AA`\n" +
			"[map specification]\n`map` 0 0 100 `boss` 1 0 101\n" +
			"[start map]\n0 0\n" +
			"[boss map]\n1 0\n",
		worldmap.DefaultWorldMapList: "1 `test.wdm`\n",
		"worldmap/test.wdm":          "[name]\n`Synthetic Area`\n[dungeon]\n700 -1\n[/dungeon]\n",
		dnfmonster.DefaultList:       "3001 `test.gob`\n",
		"monster/test.gob": "[name]\n`Synthetic Goblin`\n" +
			"[level]\n10\n" +
			"[hp]\n500\n" +
			"[exp]\n25\n",
	}
}
