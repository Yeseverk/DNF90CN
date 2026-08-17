package dnfbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"testing"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	"longheng.io/server/internal/modules/dnf/dungeoncmd"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
	"longheng.io/server/internal/modules/dnf/worldmap"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestHandleDungeonBossDieCheckCompletesGenericPVFScriptedFinal(t *testing.T) {
	service, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{
		job:              "12",
		dungeonReference: tutorialScopeKnightFDungeonReference,
		mapDirectory:     tutorialScopeKnightFMapDirectory,
	})
	if _, err := runtime.Room.AnnounceAllActors(runtime.Session); err != nil {
		t.Fatal(err)
	}
	room := runtime.Room.Snapshot()
	targetKey := room.Monsters[0].ObjectKey
	remainingKey := room.Monsters[1].ObjectKey
	conn := &bufferConn{}
	session := &gameSession{
		conn:                conn,
		connID:              "generic-pvf-scripted-final-op117-test",
		selectedCharacterID: 99,
		dungeon:             dungeonSessionState{runtime: runtime},
	}
	if err := service.handleDungeonMonsterDeath(session, tutorialScopeVariableZeroCombatDeathBody(targetKey)); err != nil {
		t.Fatalf("commit PVF scripted target death: %v", err)
	}
	conn.write.Reset()

	if err := service.handleDungeonBossDieCheck(session, bossDieCheckRequestBody(99, uint16(targetKey))); err != nil {
		t.Fatalf("handle op117: %v", err)
	}
	assertBossResponseAndSettlementEntry(t, conn.write.Bytes(), uint16(targetKey))

	snapshot := runtime.Session.Snapshot()
	if snapshot.Run.Status != worldmap.DungeonRunCompleted || !snapshot.Scene.Cleared {
		t.Fatalf("final lifecycle=%+v", snapshot)
	}
	if len(snapshot.Scene.DefeatedObjects) != 1 || snapshot.Scene.DefeatedObjects[0] != targetKey {
		t.Fatalf("final completion fabricated defeated actors=%v", snapshot.Scene.DefeatedObjects)
	}
	after := runtime.Room.Snapshot()
	if after.Monsters[0].State != runtimeDungeonMonsterDefeated ||
		after.Monsters[1].ObjectKey != remainingKey || after.Monsters[1].State != runtimeDungeonMonsterAnnounced {
		t.Fatalf("final completion changed unrelated actor state=%+v", after.Monsters)
	}
	if !runtime.bossDieCheckAccepted || !runtime.tutorialCompletionPersisted ||
		!runtime.bossDieCheckResponseSent || !runtime.settlementEntrySent ||
		session.dungeon.runtime != runtime || runtime.townReturnPending || runtime.townReturnOp24Sent {
		t.Fatalf("completion ownership accepted=%t persisted=%t response=%t runtime=%p",
			runtime.bossDieCheckAccepted, runtime.tutorialCompletionPersisted,
			runtime.bossDieCheckResponseSent, session.dungeon.runtime)
	}
	assertTutorialCompletionPersistedAndSelectAcked(t, service, 99)

	beforeReplay := conn.write.Len()
	if err := service.handleDungeonBossDieCheck(session, bossDieCheckRequestBody(99, uint16(targetKey))); err != nil {
		t.Fatalf("replayed op117: %v", err)
	}
	if conn.write.Len() != beforeReplay {
		t.Fatalf("replayed op117 emitted another completion packet: before=%d after=%d", beforeReplay, conn.write.Len())
	}
}

func TestHandleDungeonBossDieCheckCompletesClearedTutorialFinal(t *testing.T) {
	service, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{
		job:              "12",
		dungeonReference: tutorialScopeKnightFDungeonReference,
		mapDirectory:     tutorialScopeKnightFMapDirectory,
		singleMonster:    true,
	})
	if _, err := runtime.Room.AnnounceAllActors(runtime.Session); err != nil {
		t.Fatal(err)
	}
	targetKey := runtime.Room.Snapshot().Monsters[0].ObjectKey
	conn := &bufferConn{}
	session := &gameSession{
		conn:                conn,
		connID:              "generic-cleared-final-op117-test",
		selectedCharacterID: 99,
		dungeon:             dungeonSessionState{runtime: runtime},
	}
	deathBody := make([]byte, dungeoncmd.DieMonsterRequestSize)
	binary.LittleEndian.PutUint32(deathBody[0:4], targetKey)
	binary.LittleEndian.PutUint16(deathBody[4:6], 99)
	if err := service.handleDungeonMonsterDeath(session, deathBody); err != nil {
		t.Fatalf("clear final room: %v", err)
	}
	if scene := runtime.Session.Snapshot().Scene; !scene.Cleared {
		t.Fatalf("single-monster final was not cleared: %+v", scene)
	}
	conn.write.Reset()
	if err := service.handleDungeonBossDieCheck(session, bossDieCheckRequestBody(99, uint16(targetKey))); err != nil {
		t.Fatal(err)
	}
	assertBossResponseAndSettlementEntry(t, conn.write.Bytes(), uint16(targetKey))
	if status := runtime.Session.Snapshot().Run.Status; status != worldmap.DungeonRunCompleted {
		t.Fatalf("cleared final status=%s", status)
	}
	if session.dungeon.runtime != runtime || !runtime.settlementEntrySent || runtime.townReturnPending {
		t.Fatalf("cleared final lost pending runtime after op117=%+v", session.dungeon.runtime)
	}
}

func TestHandleDungeonBossDieCheckCompletesClearedOrdinaryRuntimeBossWithoutTutorialPersistence(t *testing.T) {
	service, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{
		disableTutorial: true,
		singleMonster:   true,
	})
	if isPVFTutorialDungeon(runtime) {
		t.Fatal("ordinary dungeon fixture retained tutorial ownership")
	}
	if _, err := runtime.Room.AnnounceAllActors(runtime.Session); err != nil {
		t.Fatal(err)
	}
	targetKey := runtime.Room.Snapshot().Monsters[0].ObjectKey
	if _, cleared, err := runtime.Room.CommitActorDeathReport(targetKey, runtime.Session); err != nil || !cleared {
		t.Fatalf("commit ordinary boss death cleared=%t err=%v", cleared, err)
	}
	repositories, ok := service.repositoryGroup()
	if !ok || repositories.Character == nil {
		t.Fatal("ordinary fixture character repository unavailable")
	}
	before, found, err := repositories.Character.Load(context.Background(), "99")
	if err != nil || !found {
		t.Fatalf("load ordinary character before op117 found=%t err=%v", found, err)
	}

	conn := &bufferConn{}
	session := &gameSession{
		conn:                conn,
		connID:              "ordinary-cleared-runtime-boss-op117-test",
		selectedCharacterID: 99,
		dungeon:             dungeonSessionState{runtime: runtime},
	}
	request := bossDieCheckRequestBody(99, uint16(targetKey))
	if err := service.handleDungeonBossDieCheck(session, request); err != nil {
		t.Fatalf("ordinary op117: %v", err)
	}
	assertBossResponseAndSettlementEntry(t, conn.write.Bytes(), uint16(targetKey))
	if !runtime.bossDieCheckAccepted || !runtime.bossDieCheckResponseSent ||
		!runtime.settlementEntrySent || runtime.Session.Snapshot().Run.Status != worldmap.DungeonRunCompleted {
		t.Fatalf("ordinary completion state=%+v", runtime)
	}
	after, found, err := repositories.Character.Load(context.Background(), "99")
	if err != nil || !found {
		t.Fatalf("load ordinary character after op117 found=%t err=%v", found, err)
	}
	if before.Stats[currentDungeonTutorialCompletedKey] != 0 ||
		after.Stats[currentDungeonTutorialCompletedKey] != 0 ||
		runtime.Character.Stats[currentDungeonTutorialCompletedKey] != 0 {
		t.Fatalf("ordinary completion mutated tutorial state before=%v after=%v runtime=%v",
			before.Stats, after.Stats, runtime.Character.Stats)
	}

	beforeReplay := conn.write.Len()
	if err := service.handleDungeonBossDieCheck(session, request); err != nil {
		t.Fatalf("ordinary op117 replay: %v", err)
	}
	if conn.write.Len() != beforeReplay {
		t.Fatalf("ordinary replay emitted duplicate packets before=%d after=%d", beforeReplay, conn.write.Len())
	}
}

func TestCurrentDungeonOrdinaryFinalRoomReadyRejectsBossCoordinateMismatch(t *testing.T) {
	service, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{
		disableTutorial: true,
		singleMonster:   true,
	})
	if _, err := runtime.Room.AnnounceAllActors(runtime.Session); err != nil {
		t.Fatal(err)
	}
	targetKey := runtime.Room.Snapshot().Monsters[0].ObjectKey
	conn := &bufferConn{}
	session := &gameSession{
		conn:                conn,
		connID:              "ordinary-op39-without-op117-coordinate-mismatch-rejected",
		selectedCharacterID: 99,
		dungeon:             dungeonSessionState{runtime: runtime},
	}
	scene := runtime.Session.Snapshot().Scene
	runtime.BossCoordinate = worldmap.RoomCoordinate{X: scene.Coordinate.X + 7, Y: scene.Coordinate.Y + 7}
	deathBody := make([]byte, dungeoncmd.DieMonsterRequestSize)
	binary.LittleEndian.PutUint32(deathBody[0:4], targetKey)
	binary.LittleEndian.PutUint16(deathBody[4:6], 99)
	if err := service.handleDungeonMonsterDeath(session, deathBody); err != nil {
		t.Fatal(err)
	}
	if runtime.ordinaryFinalRoomClearAccepted || runtime.bossDieCheckAccepted ||
		runtime.settlementEntrySent || runtime.Session.Snapshot().Run.Status != worldmap.DungeonRunActive {
		t.Fatalf("ordinary coordinate mismatch entered settlement: runtime=%+v snapshot=%+v bytes=%x",
			runtime, runtime.Session.Snapshot(), conn.write.Bytes())
	}
}

func TestCurrentDungeonOrdinaryFinalRoomReadyRejectsClearedNonBossTerminalRoom(t *testing.T) {
	_, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{
		disableTutorial: true,
		singleMonster:   true,
	})
	scene := runtime.Session.Snapshot().Scene
	scene.Cleared = true
	scene.Boss = false
	if ready, source := currentDungeonOrdinaryFinalRoomReady(runtime, scene); ready || source != "" {
		t.Fatalf("non-boss terminal room accepted as final ready=%t source=%q", ready, source)
	}
}

func TestHandleDungeonMonsterDeathBossRankForcesOrdinaryFinalRoomClear(t *testing.T) {
	service, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{
		disableTutorial: true,
	})
	if isPVFTutorialDungeon(runtime) {
		t.Fatal("ordinary boss forced-clear fixture retained tutorial ownership")
	}
	if _, err := runtime.Room.AnnounceAllActors(runtime.Session); err != nil {
		t.Fatal(err)
	}
	room := runtime.Room.Snapshot()
	if len(room.Monsters) != 2 {
		t.Fatalf("fixture monster count=%d want=2", len(room.Monsters))
	}
	targetKey := room.Monsters[0].ObjectKey
	remainingKey := room.Monsters[1].ObjectKey
	if normalizeDungeonPVFSymbol(room.Monsters[0].Spawn.Rank) != "boss" ||
		normalizeDungeonPVFSymbol(room.Monsters[1].Spawn.Rank) == "boss" {
		t.Fatalf("fixture ranks=%q/%q", room.Monsters[0].Spawn.Rank, room.Monsters[1].Spawn.Rank)
	}
	conn := &bufferConn{}
	session := &gameSession{
		conn:                conn,
		connID:              "ordinary-boss-op39-forces-final-clear",
		selectedCharacterID: 99,
		dungeon:             dungeonSessionState{runtime: runtime},
	}
	deathBody := make([]byte, dungeoncmd.DieMonsterRequestSize)
	binary.LittleEndian.PutUint32(deathBody[0:4], targetKey)
	binary.LittleEndian.PutUint16(deathBody[4:6], 99)
	if err := service.handleDungeonMonsterDeath(session, deathBody); err != nil {
		t.Fatal(err)
	}
	snapshot := runtime.Session.Snapshot()
	if snapshot.Run.Status != worldmap.DungeonRunCompleted || !snapshot.Scene.Cleared ||
		!runtime.ordinaryFinalRoomClearAccepted || !runtime.settlementEntrySent {
		t.Fatalf("boss op39 did not enter ordinary final settlement: runtime=%+v snapshot=%+v",
			runtime, snapshot)
	}
	if !dungeonSceneObjectDefeated(snapshot.Scene.DefeatedObjects, targetKey) ||
		!dungeonSceneObjectDefeated(snapshot.Scene.DefeatedObjects, remainingKey) {
		t.Fatalf("boss forced clear did not mark both actors defeated: defeated=%v boss=%d remaining=%d",
			snapshot.Scene.DefeatedObjects, targetKey, remainingKey)
	}
	after := runtime.Room.Snapshot()
	if after.Monsters[0].State != runtimeDungeonMonsterDefeated ||
		after.Monsters[1].State != runtimeDungeonMonsterDefeated {
		t.Fatalf("boss forced clear left runtime actor states=%+v", after.Monsters)
	}
	bossDeath, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if bossDeath.Header.MsgID != uint16(dnfenum.CmdPacketNotifyDieMonster) ||
		binary.LittleEndian.Uint16(bossDeath.Body[:2]) != uint16(targetKey) {
		t.Fatalf("boss death packet header=%+v body=%x", bossDeath.Header, bossDeath.Body)
	}
	forcedDeath, _ := splitGameServerUpperPacket(t, rest)
	if forcedDeath.Header.MsgID != uint16(dnfenum.CmdPacketNotifyDieMonster) ||
		binary.LittleEndian.Uint16(forcedDeath.Body[:2]) != uint16(remainingKey) {
		t.Fatalf("forced visual death packet header=%+v body=%x want_key=%d",
			forcedDeath.Header, forcedDeath.Body, remainingKey)
	}
}

func TestHandleDungeonMonsterDeathSettlementContinuesWhenQuestSyncUnavailable(t *testing.T) {
	service, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{
		disableTutorial: true,
	})
	if _, err := runtime.Room.AnnounceAllActors(runtime.Session); err != nil {
		t.Fatal(err)
	}
	room := runtime.Room.Snapshot()
	if len(room.Monsters) != 2 {
		t.Fatalf("fixture monster count=%d want=2", len(room.Monsters))
	}
	targetKey := room.Monsters[0].ObjectKey
	service.repositoryProvider = nil
	conn := &bufferConn{}
	session := &gameSession{
		conn:                conn,
		connID:              "ordinary-boss-quest-sync-unavailable-still-settlement",
		selectedCharacterID: 99,
		dungeon:             dungeonSessionState{runtime: runtime},
	}
	deathBody := make([]byte, dungeoncmd.DieMonsterRequestSize)
	binary.LittleEndian.PutUint32(deathBody[0:4], targetKey)
	binary.LittleEndian.PutUint16(deathBody[4:6], 99)
	if err := service.handleDungeonMonsterDeath(session, deathBody); err != nil {
		t.Fatal(err)
	}
	snapshot := runtime.Session.Snapshot()
	if snapshot.Run.Status != worldmap.DungeonRunCompleted || !snapshot.Scene.Cleared ||
		!runtime.ordinaryFinalRoomClearAccepted || !runtime.bossDieCheckAccepted ||
		!runtime.bossDieCheckResponseSent || !runtime.settlementEntrySent {
		t.Fatalf("quest-sync failure blocked 86JP settlement flow: runtime=%+v snapshot=%+v",
			runtime, snapshot)
	}
	packets := splitAllFinishBridgePackets(t, conn.write.Bytes())
	if len(packets) < 4 {
		t.Fatalf("packet count=%d want boss death, forced death, op115, op31", len(packets))
	}
	if packets[len(packets)-2].Header.MsgID != uint16(dnfenum.CmdPacketNotifyBossDieCheck) ||
		packets[len(packets)-1].Header.MsgID != currentDungeonSettlementEntryMsgID {
		t.Fatalf("final packets msg=%d/%d want op115/op31",
			packets[len(packets)-2].Header.MsgID,
			packets[len(packets)-1].Header.MsgID)
	}
}

func TestHandleDungeonMonsterDeathBossSuffixForcesOrdinaryFinalRoomClear(t *testing.T) {
	service, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{
		disableTutorial:  true,
		bossRank:         "[dummy]",
		bossSuffixMarker: "[boss]",
	})
	if _, err := runtime.Room.AnnounceAllActors(runtime.Session); err != nil {
		t.Fatal(err)
	}
	room := runtime.Room.Snapshot()
	if len(room.Monsters) != 2 {
		t.Fatalf("fixture monster count=%d want=2", len(room.Monsters))
	}
	targetKey := room.Monsters[0].ObjectKey
	remainingKey := room.Monsters[1].ObjectKey
	if normalizeDungeonPVFSymbol(room.Monsters[0].Spawn.Rank) == "boss" ||
		normalizeDungeonPVFSymbol(room.Monsters[0].Spawn.SuffixMarker) != "boss" {
		t.Fatalf("fixture boss suffix not represented as rank+suffix: %+v", room.Monsters[0].Spawn)
	}
	conn := &bufferConn{}
	session := &gameSession{
		conn:                conn,
		connID:              "ordinary-boss-suffix-op39-forces-final-clear",
		selectedCharacterID: 99,
		dungeon:             dungeonSessionState{runtime: runtime},
	}
	deathBody := make([]byte, dungeoncmd.DieMonsterRequestSize)
	binary.LittleEndian.PutUint32(deathBody[0:4], targetKey)
	binary.LittleEndian.PutUint16(deathBody[4:6], 99)
	if err := service.handleDungeonMonsterDeath(session, deathBody); err != nil {
		t.Fatal(err)
	}
	snapshot := runtime.Session.Snapshot()
	if snapshot.Run.Status != worldmap.DungeonRunCompleted || !snapshot.Scene.Cleared ||
		!runtime.ordinaryFinalRoomClearAccepted || !runtime.settlementEntrySent {
		t.Fatalf("boss suffix op39 did not enter settlement: runtime=%+v snapshot=%+v",
			runtime, snapshot)
	}
	if !dungeonSceneObjectDefeated(snapshot.Scene.DefeatedObjects, targetKey) ||
		!dungeonSceneObjectDefeated(snapshot.Scene.DefeatedObjects, remainingKey) {
		t.Fatalf("boss suffix forced clear did not mark both actors defeated: defeated=%v boss=%d remaining=%d",
			snapshot.Scene.DefeatedObjects, targetKey, remainingKey)
	}
	bossDeath, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if bossDeath.Header.MsgID != uint16(dnfenum.CmdPacketNotifyDieMonster) ||
		binary.LittleEndian.Uint16(bossDeath.Body[:2]) != uint16(targetKey) {
		t.Fatalf("boss suffix death packet header=%+v body=%x", bossDeath.Header, bossDeath.Body)
	}
	forcedDeath, _ := splitGameServerUpperPacket(t, rest)
	if forcedDeath.Header.MsgID != uint16(dnfenum.CmdPacketNotifyDieMonster) ||
		binary.LittleEndian.Uint16(forcedDeath.Body[:2]) != uint16(remainingKey) {
		t.Fatalf("boss suffix forced visual death packet header=%+v body=%x want_key=%d",
			forcedDeath.Header, forcedDeath.Body, remainingKey)
	}
}

func TestHandleDungeonMonsterDeathClearConditionTargetForcesOrdinaryFinalRoomClear(t *testing.T) {
	service, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{
		disableTutorial: true,
		bossRank:        "[normal]",
		clearCondition:  "[clear condition]\n[hunt monster]\n3001 1\n[/clear condition]\n",
	})
	if _, err := runtime.Room.AnnounceAllActors(runtime.Session); err != nil {
		t.Fatal(err)
	}
	room := runtime.Room.Snapshot()
	if len(room.Monsters) != 2 {
		t.Fatalf("fixture monster count=%d want=2", len(room.Monsters))
	}
	targetKey := room.Monsters[0].ObjectKey
	remainingKey := room.Monsters[1].ObjectKey
	if normalizeDungeonPVFSymbol(room.Monsters[0].Spawn.Rank) == "boss" ||
		currentDungeonClearConditionSource(runtime, room.Monsters[0]) == "" {
		t.Fatalf("fixture should rely on clear condition, not boss rank: monster=%+v conditions=%+v",
			room.Monsters[0], runtime.Dungeon.Mazes[runtime.MazeIndex].ClearConditions)
	}
	conn := &bufferConn{}
	session := &gameSession{
		conn:                conn,
		connID:              "ordinary-clear-condition-op39-forces-final-clear",
		selectedCharacterID: 99,
		dungeon:             dungeonSessionState{runtime: runtime},
	}
	deathBody := make([]byte, dungeoncmd.DieMonsterRequestSize)
	binary.LittleEndian.PutUint32(deathBody[0:4], targetKey)
	binary.LittleEndian.PutUint16(deathBody[4:6], 99)
	if err := service.handleDungeonMonsterDeath(session, deathBody); err != nil {
		t.Fatal(err)
	}
	snapshot := runtime.Session.Snapshot()
	if snapshot.Run.Status != worldmap.DungeonRunCompleted || !snapshot.Scene.Cleared ||
		!runtime.ordinaryFinalRoomClearAccepted || !runtime.settlementEntrySent {
		t.Fatalf("clear-condition op39 did not enter settlement: runtime=%+v snapshot=%+v",
			runtime, snapshot)
	}
	if !dungeonSceneObjectDefeated(snapshot.Scene.DefeatedObjects, targetKey) ||
		!dungeonSceneObjectDefeated(snapshot.Scene.DefeatedObjects, remainingKey) {
		t.Fatalf("clear-condition forced clear did not mark both actors defeated: defeated=%v target=%d remaining=%d",
			snapshot.Scene.DefeatedObjects, targetKey, remainingKey)
	}
	firstDeath, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if firstDeath.Header.MsgID != uint16(dnfenum.CmdPacketNotifyDieMonster) ||
		binary.LittleEndian.Uint16(firstDeath.Body[:2]) != uint16(targetKey) {
		t.Fatalf("clear-condition death packet header=%+v body=%x", firstDeath.Header, firstDeath.Body)
	}
	forcedDeath, _ := splitGameServerUpperPacket(t, rest)
	if forcedDeath.Header.MsgID != uint16(dnfenum.CmdPacketNotifyDieMonster) ||
		binary.LittleEndian.Uint16(forcedDeath.Body[:2]) != uint16(remainingKey) {
		t.Fatalf("clear-condition forced visual death packet header=%+v body=%x want_key=%d",
			forcedDeath.Header, forcedDeath.Body, remainingKey)
	}
}

func TestHandleDungeonBossDieCheckOrdinaryRuntimeRequiresOp39DeathAndClearedRoom(t *testing.T) {
	t.Run("op117 before op39 is not retained", func(t *testing.T) {
		service, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{
			disableTutorial: true,
			singleMonster:   true,
		})
		if _, err := runtime.Room.AnnounceAllActors(runtime.Session); err != nil {
			t.Fatal(err)
		}
		targetKey := runtime.Room.Snapshot().Monsters[0].ObjectKey
		conn := &bufferConn{}
		session := &gameSession{
			conn:                conn,
			connID:              "ordinary-op117-before-op39-test",
			selectedCharacterID: 99,
			dungeon:             dungeonSessionState{runtime: runtime},
		}
		if err := service.handleDungeonBossDieCheck(session, bossDieCheckRequestBody(99, uint16(targetKey))); err != nil {
			t.Fatal(err)
		}
		if conn.write.Len() != 0 || runtime.bossDieCheckPending || runtime.bossDieCheckAccepted ||
			runtime.Session.Snapshot().Run.Status != worldmap.DungeonRunActive {
			t.Fatalf("early ordinary op117 changed state=%+v wrote=%x", runtime, conn.write.Bytes())
		}
	})

	t.Run("defeated boss target force-clears another blocker before completion", func(t *testing.T) {
		service, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{disableTutorial: true})
		if _, err := runtime.Room.AnnounceAllActors(runtime.Session); err != nil {
			t.Fatal(err)
		}
		room := runtime.Room.Snapshot()
		targetKey := room.Monsters[0].ObjectKey
		remainingKey := room.Monsters[1].ObjectKey
		if _, cleared, err := runtime.Room.CommitActorDeathReport(targetKey, runtime.Session); err != nil || cleared {
			t.Fatalf("commit partial ordinary boss death cleared=%t err=%v", cleared, err)
		}
		conn := &bufferConn{}
		session := &gameSession{
			conn:                conn,
			connID:              "ordinary-uncleared-boss-op117-test",
			selectedCharacterID: 99,
			dungeon:             dungeonSessionState{runtime: runtime},
		}
		if err := service.handleDungeonBossDieCheck(session, bossDieCheckRequestBody(99, uint16(targetKey))); err != nil {
			t.Fatal(err)
		}
		snapshot := runtime.Session.Snapshot()
		if !runtime.bossDieCheckAccepted || !runtime.settlementEntrySent ||
			snapshot.Run.Status != worldmap.DungeonRunCompleted ||
			!dungeonSceneObjectDefeated(snapshot.Scene.DefeatedObjects, remainingKey) {
			t.Fatalf("ordinary op117 did not force-clear and settle: state=%+v snapshot=%+v wrote=%x",
				runtime, snapshot, conn.write.Bytes())
		}
		forcedDeath, _ := splitGameServerUpperPacket(t, conn.write.Bytes())
		if forcedDeath.Header.MsgID != uint16(dnfenum.CmdPacketNotifyDieMonster) ||
			binary.LittleEndian.Uint16(forcedDeath.Body[:2]) != uint16(remainingKey) {
			t.Fatalf("op117 force-clear first packet header=%+v body=%x want_key=%d",
				forcedDeath.Header, forcedDeath.Body, remainingKey)
		}
	})
}

func TestRealScriptPVFDungeon3BossRoomOp117EntersSettlementWithoutTutorialMutation(t *testing.T) {
	pvfPath := os.Getenv("DNF_WORLDMAP_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNF_WORLDMAP_REAL_PVF_SMOKE to run real dungeon 3 boss settlement smoke")
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
		CharacterID: "99",
		AccountID:   "account-1",
		Name:        "Dungeon3BossSmoke",
		Job:         "2",
		Level:       20,
		Stats:       map[string]int64{"fatigue": 156},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Quest.Save(context.Background(), dnfrepo.QuestRecord{
		CharacterID: "99",
		States: map[int64]dnfrepo.QuestState{
			3145: {Status: "active", ProgressValue: 1},
		},
	}); err != nil {
		t.Fatal(err)
	}
	service := &Service{
		options:                 options{accountID: "account-1", gameUpperBodyCodec: gameUpperBodyCodecPlain},
		initialEquipmentArchive: archive,
		worldMapTable:           table,
		worldMapResolver:        resolver,
		dungeonMonsterTable:     monsters,
		dungeonAICharacterTable: aiCharacters,
		dungeonChoice:           func(int) (int, error) { return 0, nil },
		dungeonSeed:             func() (uint32, error) { return 1, nil },
		repositoryProvider:      func() (dnfrepo.Group, bool) { return repositories, true },
	}
	session := &gameSession{
		conn:                &bufferConn{},
		connID:              "real-dungeon3-boss-op117-test",
		selectedCharacterID: 99,
	}
	runtime, startScene, err := service.prepareDungeonRuntime(
		context.Background(),
		session,
		dungeoncmd.SelectDungeonRequest{DungeonID: 3, Difficulty: 0},
	)
	if err != nil {
		t.Fatalf("prepare real dungeon 3 runtime: %v", err)
	}
	session.dungeon.runtime = runtime
	if runtime.MazeIndex != 1 || runtime.BossCoordinate != (worldmap.RoomCoordinate{X: 4, Y: 0}) ||
		isPVFTutorialDungeon(runtime) {
		t.Fatalf("real dungeon 3 owner maze=%d boss=%s tutorial=%t",
			runtime.MazeIndex, runtime.BossCoordinate, isPVFTutorialDungeon(runtime))
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
	queue := []worldmap.RoomCoordinate{startScene.Coordinate}
	visited := map[worldmap.RoomCoordinate]bool{startScene.Coordinate: true}
	previous := make(map[worldmap.RoomCoordinate]worldmap.RoomCoordinate)
	for len(queue) != 0 && !visited[runtime.BossCoordinate] {
		current := queue[0]
		queue = queue[1:]
		room, ok := topology.Room(current)
		if !ok {
			t.Fatalf("real dungeon 3 path room missing: %s", current)
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
	if !visited[runtime.BossCoordinate] {
		t.Fatalf("real dungeon 3 boss room %s unreachable from %s", runtime.BossCoordinate, startScene.Coordinate)
	}
	path := []worldmap.RoomCoordinate{runtime.BossCoordinate}
	for path[len(path)-1] != startScene.Coordinate {
		path = append(path, previous[path[len(path)-1]])
	}
	for left, right := 0, len(path)-1; left < right; left, right = left+1, right-1 {
		path[left], path[right] = path[right], path[left]
	}

	clearCurrentRoom := func() {
		t.Helper()
		room := runtime.Room.Snapshot()
		actorsPlanned := false
		for _, monster := range room.Monsters {
			actorsPlanned = actorsPlanned || monster.State == runtimeDungeonMonsterPlanned
		}
		for _, actor := range room.ExtendedActors {
			actorsPlanned = actorsPlanned || actor.State == runtimeDungeonMonsterPlanned
		}
		if actorsPlanned {
			if _, err := runtime.Room.AnnounceAllActors(runtime.Session); err != nil {
				t.Fatalf("announce real dungeon 3 room: %v", err)
			}
		}
		scene := runtime.Session.Snapshot().Scene
		for _, reference := range scene.BlockingHostiles {
			var objectKey uint32
			for candidate, bound := range scene.RuntimeObjects {
				if bound == reference {
					objectKey = candidate
					break
				}
			}
			if objectKey == 0 {
				t.Fatalf("real dungeon 3 blocker has no runtime object: %+v", reference)
			}
			if _, _, defeated := currentDungeonDefeatedActor(runtime, scene, objectKey); defeated {
				continue
			}
			if _, _, err := runtime.Room.CommitActorDeathReport(objectKey, runtime.Session); err != nil {
				t.Fatalf("defeat real dungeon 3 blocker key=%d reference=%+v: %v", objectKey, reference, err)
			}
			scene = runtime.Session.Snapshot().Scene
		}
		if !runtime.Session.Snapshot().Scene.Cleared {
			t.Fatalf("real dungeon 3 room did not clear: %+v", runtime.Session.Snapshot().Scene)
		}
	}

	for _, target := range path[1:] {
		clearCurrentRoom()
		body := make([]byte, dungeoncmd.MoveMapRequestSize)
		body[0] = byte(target.X)
		body[1] = byte(target.Y)
		session.conn.(*bufferConn).write.Reset()
		if err := service.handleDungeonMoveMap(session, body); err != nil {
			t.Fatalf("move real dungeon 3 to %s: %v", target, err)
		}
		if scene := runtime.Session.Snapshot().Scene; scene.Coordinate != target {
			t.Fatalf("real dungeon 3 move committed %s want %s", scene.Coordinate, target)
		}
	}
	if scene := runtime.Session.Snapshot().Scene; !scene.Boss || scene.Map.Map.ID != 76126 ||
		scene.Coordinate != runtime.BossCoordinate {
		t.Fatalf("real dungeon 3 boss scene=%+v", scene)
	}
	roomBeforeDeath := runtime.Room.Snapshot()
	var targetKey uint32
	for _, monster := range roomBeforeDeath.Monsters {
		if normalizeDungeonPVFSymbol(monster.Spawn.Rank) == "boss" {
			targetKey = monster.ObjectKey
			break
		}
	}
	if targetKey == 0 {
		t.Fatalf("real dungeon 3 boss room has no PVF boss-ranked monster: %+v", roomBeforeDeath.Monsters)
	}
	clearCurrentRoom()
	conn := session.conn.(*bufferConn)
	conn.write.Reset()
	if err := service.handleDungeonBossDieCheck(session, bossDieCheckRequestBody(99, uint16(targetKey))); err != nil {
		t.Fatalf("real dungeon 3 op117: %v", err)
	}
	assertClearMapNotificationThenBossResponseAndSettlementEntry(t, conn.write.Bytes(), uint16(targetKey), 3145)
	if runtime.Session.Snapshot().Run.Status != worldmap.DungeonRunCompleted ||
		!runtime.bossDieCheckAccepted || !runtime.clearMapCompletionPhaseAPersisted || !runtime.settlementEntrySent {
		t.Fatalf("real dungeon 3 completion state=%+v", runtime)
	}
	questRecord, found, err := repositories.Quest.Load(context.Background(), "99")
	if err != nil || !found {
		t.Fatalf("load real dungeon 3 quest after op117 found=%t err=%v", found, err)
	}
	questState := questRecord.States[3145]
	if questState.Status != "active" || questState.ProgressValue != 0 ||
		questState.Extra["completion_key"] != runtime.clearMapCompletionKey ||
		questState.Extra["completion_dungeon_id"] != "3" ||
		questState.Extra["completion_map_id"] != "76126" ||
		questState.Extra["reward_state"] != "pending" {
		t.Fatalf("real dungeon 3 clear-map state=%+v runtime_key=%q", questState, runtime.clearMapCompletionKey)
	}
	character, found, err := repositories.Character.Load(context.Background(), "99")
	if err != nil || !found || character.Stats[currentDungeonTutorialCompletedKey] != 0 {
		t.Fatalf("real dungeon 3 tutorial flag found=%t value=%d err=%v",
			found, character.Stats[currentDungeonTutorialCompletedKey], err)
	}
}

func TestHandleGameUpperRoutesPlainBossDieCheckAndRejectsWrongClass(t *testing.T) {
	for _, test := range []struct {
		name           string
		classification byte
		wantResponse   bool
	}{
		{name: "class one request", classification: dnfproto.DefaultChannelClassification, wantResponse: true},
		{name: "wrong class", classification: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{singleMonster: true})
			if _, err := runtime.Room.AnnounceAllActors(runtime.Session); err != nil {
				t.Fatal(err)
			}
			targetKey := runtime.Room.Snapshot().Monsters[0].ObjectKey
			if _, _, err := runtime.Room.CommitActorDeathReport(targetKey, runtime.Session); err != nil {
				t.Fatal(err)
			}
			conn := &bufferConn{}
			session := &gameSession{
				conn:                conn,
				connID:              "op117-upper-dispatch-test",
				selectedCharacterID: 99,
				dungeon:             dungeonSessionState{runtime: runtime},
			}
			request, err := dnfproto.BuildChannelPacket(
				uint16(dnfenum.CmdPacketBossDieCheck),
				bossDieCheckRequestBody(99, uint16(targetKey)),
				0,
				test.classification,
			)
			if err != nil {
				t.Fatal(err)
			}
			if err := service.handleGameUpper(session, request); err != nil {
				t.Fatalf("handle upper op117: %v", err)
			}
			if test.wantResponse {
				assertBossResponseAndSettlementEntry(t, conn.write.Bytes(), uint16(targetKey))
				if !runtime.bossDieCheckResponseSent || !runtime.tutorialCompletionPersisted ||
					!runtime.settlementEntrySent || session.dungeon.runtime != runtime || runtime.townReturnPending {
					t.Fatalf("upper op117 completion response=%t persisted=%t runtime=%p",
						runtime.bossDieCheckResponseSent, runtime.tutorialCompletionPersisted, session.dungeon.runtime)
				}
				return
			}
			if conn.write.Len() != 0 || runtime.Session.Snapshot().Run.Status != worldmap.DungeonRunActive {
				t.Fatalf("wrong class wrote=%x snapshot=%+v", conn.write.Bytes(), runtime.Session.Snapshot())
			}
		})
	}
}

func TestHandleDungeonBossDieCheckRejectsUnownedOrUnprovenFinals(t *testing.T) {
	tests := []struct {
		name       string
		fixture    tutorialScopeFixtureOptions
		mutate     func(*runtimeDungeonState, *gameSession, uint32, []byte)
		body       func(uint32) []byte
		killTarget bool
	}{
		{
			name: "protected forty byte boundary",
			body: func(uint32) []byte { return make([]byte, 40) },
		},
		{
			name: "writer reserved invariant",
			body: func(key uint32) []byte {
				body := bossDieCheckRequestBody(99, uint16(key))
				body[4] = 1
				return body
			},
		},
		{
			name: "target not defeated",
			body: func(key uint32) []byte { return bossDieCheckRequestBody(99, uint16(key)) },
		},
		{
			name:       "unowned related actor",
			killTarget: true,
			body:       func(key uint32) []byte { return bossDieCheckRequestBody(0x7777, uint16(key)) },
		},
		{
			name:       "active runtime room map owner mismatch",
			killTarget: true,
			mutate: func(runtime *runtimeDungeonState, _ *gameSession, _ uint32, _ []byte) {
				runtime.Room.mu.Lock()
				runtime.Room.mapID++
				runtime.Room.mu.Unlock()
			},
			body: func(key uint32) []byte { return bossDieCheckRequestBody(99, uint16(key)) },
		},
		{
			name:       "not selected boss coordinate",
			killTarget: true,
			mutate: func(runtime *runtimeDungeonState, _ *gameSession, _ uint32, _ []byte) {
				runtime.BossCoordinate.X++
			},
			body: func(key uint32) []byte { return bossDieCheckRequestBody(99, uint16(key)) },
		},
		{
			name: "single cinematic does not cover every remaining blocker",
			fixture: tutorialScopeFixtureOptions{
				incompleteCinematicCoverage: true,
			},
			killTarget: true,
			body:       func(key uint32) []byte { return bossDieCheckRequestBody(99, uint16(key)) },
		},
		{
			name: "current room is not boss",
			fixture: tutorialScopeFixtureOptions{
				currentRoomNonBoss: true,
				singleMonster:      true,
			},
			killTarget: true,
			body:       func(key uint32) []byte { return bossDieCheckRequestBody(99, uint16(key)) },
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, runtime := prepareTutorialScopeRuntime(t, test.fixture)
			if _, err := runtime.Room.AnnounceAllActors(runtime.Session); err != nil {
				t.Fatal(err)
			}
			room := runtime.Room.Snapshot()
			if len(room.Monsters) == 0 {
				t.Fatal("fixture has no target monster")
			}
			targetKey := room.Monsters[0].ObjectKey
			conn := &bufferConn{}
			session := &gameSession{
				conn:                conn,
				connID:              "rejected-op117-test",
				selectedCharacterID: 99,
				dungeon:             dungeonSessionState{runtime: runtime},
			}
			if test.killTarget {
				if _, _, err := runtime.Room.CommitActorDeathReport(targetKey, runtime.Session); err != nil {
					t.Fatalf("commit target death: %v", err)
				}
			}
			body := test.body(targetKey)
			if test.mutate != nil {
				test.mutate(runtime, session, targetKey, body)
			}
			before := runtime.Session.Snapshot()
			if err := service.handleDungeonBossDieCheck(session, body); err != nil {
				t.Fatalf("handle rejected op117: %v", err)
			}
			if conn.write.Len() != 0 || runtime.bossDieCheckAccepted {
				t.Fatalf("rejected op117 wrote=%x accepted=%t", conn.write.Bytes(), runtime.bossDieCheckAccepted)
			}
			after := runtime.Session.Snapshot()
			if after.Run.Status != before.Run.Status || after.Scene.Cleared != before.Scene.Cleared ||
				len(after.Scene.DefeatedObjects) != len(before.Scene.DefeatedObjects) {
				t.Fatalf("rejected op117 changed state before=%+v after=%+v", before, after)
			}
		})
	}
}

func TestHandleDungeonBossDieCheckRejectsStaleRuntimeCharacterOwner(t *testing.T) {
	t.Run("initial request", func(t *testing.T) {
		service, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{singleMonster: true})
		if _, err := runtime.Room.AnnounceAllActors(runtime.Session); err != nil {
			t.Fatal(err)
		}
		targetKey := runtime.Room.Snapshot().Monsters[0].ObjectKey
		if _, _, err := runtime.Room.CommitActorDeathReport(targetKey, runtime.Session); err != nil {
			t.Fatal(err)
		}
		conn := &bufferConn{}
		session := &gameSession{
			conn:                conn,
			connID:              "stale-owner-initial-op117-test",
			selectedCharacterID: 100,
			dungeon:             dungeonSessionState{runtime: runtime},
		}
		if err := service.handleDungeonBossDieCheck(session, bossDieCheckRequestBody(100, uint16(targetKey))); err != nil {
			t.Fatal(err)
		}
		if conn.write.Len() != 0 || runtime.bossDieCheckAccepted ||
			runtime.Session.Snapshot().Run.Status != worldmap.DungeonRunActive {
			t.Fatalf("stale initial op117 wrote=%x accepted=%t status=%s",
				conn.write.Bytes(), runtime.bossDieCheckAccepted,
				runtime.Session.Snapshot().Run.Status)
		}
	})

	t.Run("accepted replay after op115 failure", func(t *testing.T) {
		service, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{singleMonster: true})
		if _, err := runtime.Room.AnnounceAllActors(runtime.Session); err != nil {
			t.Fatal(err)
		}
		targetKey := runtime.Room.Snapshot().Monsters[0].ObjectKey
		if _, _, err := runtime.Room.CommitActorDeathReport(targetKey, runtime.Session); err != nil {
			t.Fatal(err)
		}
		wantErr := errors.New("first op115 write failed")
		failingConn := &failNthDungeonWriteConn{failAt: 1, err: wantErr}
		session := &gameSession{
			conn:                failingConn,
			connID:              "stale-owner-replay-op117-test",
			selectedCharacterID: 99,
			dungeon:             dungeonSessionState{runtime: runtime},
		}
		request := bossDieCheckRequestBody(99, uint16(targetKey))
		if err := service.handleDungeonBossDieCheck(session, request); !errors.Is(err, wantErr) {
			t.Fatalf("first op115 failure=%v want=%v", err, wantErr)
		}
		if !runtime.bossDieCheckAccepted || !runtime.tutorialCompletionPersisted || runtime.bossDieCheckResponseSent ||
			runtime.Session.Snapshot().Run.Status != worldmap.DungeonRunCompleted {
			t.Fatalf("failed op115 state accepted=%t persisted=%t response=%t status=%s",
				runtime.bossDieCheckAccepted, runtime.tutorialCompletionPersisted, runtime.bossDieCheckResponseSent,
				runtime.Session.Snapshot().Run.Status)
		}

		conn := &bufferConn{}
		session.conn = conn
		session.selectedCharacterID = 100
		if err := service.handleDungeonBossDieCheck(session, request); err != nil {
			t.Fatal(err)
		}
		if conn.write.Len() != 0 || runtime.bossDieCheckResponseSent || session.dungeon.runtime != runtime {
			t.Fatalf("stale replay wrote=%x response=%t runtime=%p",
				conn.write.Bytes(),
				runtime.bossDieCheckResponseSent, session.dungeon.runtime)
		}
	})
}

func TestHandleDungeonBossDieCheckOp115WriteFailureKeepsPersistedRuntimeForRetry(t *testing.T) {
	service, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{})
	if _, err := runtime.Room.AnnounceAllActors(runtime.Session); err != nil {
		t.Fatal(err)
	}
	targetKey := runtime.Room.Snapshot().Monsters[0].ObjectKey
	if _, _, err := runtime.Room.CommitActorDeathReport(targetKey, runtime.Session); err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("boss-die-check write failed")
	conn := &failNthDungeonWriteConn{failAt: 1, err: wantErr}
	session := &gameSession{
		conn:                conn,
		connID:              "op117-write-failure-test",
		selectedCharacterID: 99,
		dungeon:             dungeonSessionState{runtime: runtime},
	}
	err := service.handleDungeonBossDieCheck(session, bossDieCheckRequestBody(99, uint16(targetKey)))
	if !errors.Is(err, wantErr) {
		t.Fatalf("write failure error=%v want=%v", err, wantErr)
	}
	if !runtime.bossDieCheckAccepted || !runtime.tutorialCompletionPersisted ||
		runtime.bossDieCheckResponseSent || session.dungeon.runtime != runtime {
		t.Fatalf("write failure state accepted=%t persisted=%t response=%t runtime=%p",
			runtime.bossDieCheckAccepted, runtime.tutorialCompletionPersisted,
			runtime.bossDieCheckResponseSent, session.dungeon.runtime)
	}
	snapshot := runtime.Session.Snapshot()
	if snapshot.Run.Status != worldmap.DungeonRunCompleted || !snapshot.Scene.Cleared {
		t.Fatalf("op115 write failure did not retain committed completion=%+v", snapshot)
	}
}

func TestHandleDungeonBossDieCheckWaitsForSameTargetDeathAcrossJobs(t *testing.T) {
	service, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{
		job:              "2",
		dungeonReference: tutorialScopeGunnerDungeonReference,
		mapDirectory:     tutorialScopeGunnerMapDirectory,
		singleMonster:    true,
	})
	if _, err := runtime.Room.AnnounceAllActors(runtime.Session); err != nil {
		t.Fatal(err)
	}
	targetKey := runtime.Room.Snapshot().Monsters[0].ObjectKey
	conn := &bufferConn{}
	session := &gameSession{
		conn:                conn,
		connID:              "job2-op117-before-op39-test",
		selectedCharacterID: 99,
		dungeon:             dungeonSessionState{runtime: runtime},
	}
	request := bossDieCheckRequestBody(99, uint16(targetKey))
	if err := service.handleDungeonBossDieCheck(session, request); err != nil {
		t.Fatal(err)
	}
	if conn.write.Len() != 0 || !runtime.bossDieCheckPending || runtime.bossDieCheckAccepted {
		t.Fatalf("early op117 wrote=%x pending=%t accepted=%t", conn.write.Bytes(), runtime.bossDieCheckPending, runtime.bossDieCheckAccepted)
	}

	deathBody := make([]byte, dungeoncmd.DieMonsterRequestSize)
	binary.LittleEndian.PutUint32(deathBody[0:4], targetKey)
	binary.LittleEndian.PutUint16(deathBody[4:6], 99)
	if err := service.handleDungeonMonsterDeath(session, deathBody); err != nil {
		t.Fatalf("same target op39: %v", err)
	}
	deathAck, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if deathAck.Header.MsgID != uint16(dnfenum.CmdPacketNotifyDieMonster) {
		t.Fatalf("first packet=%+v body=%x", deathAck.Header, deathAck.Body)
	}
	assertBossResponseAndSettlementEntry(t, rest, uint16(targetKey))
	if runtime.bossDieCheckPending || !runtime.bossDieCheckAccepted ||
		!runtime.tutorialCompletionPersisted || !runtime.bossDieCheckResponseSent ||
		session.dungeon.runtime != runtime || !runtime.settlementEntrySent || runtime.townReturnPending {
		t.Fatalf("post-death pending=%t accepted=%t persisted=%t response=%t runtime=%p",
			runtime.bossDieCheckPending, runtime.bossDieCheckAccepted,
			runtime.tutorialCompletionPersisted, runtime.bossDieCheckResponseSent,
			session.dungeon.runtime)
	}
	if status := runtime.Session.Snapshot().Run.Status; status != worldmap.DungeonRunCompleted {
		t.Fatalf("post-death status=%s", status)
	}
}

func TestPendingBossDieCheckRetriesAfterOp115WriteFailure(t *testing.T) {
	service, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{singleMonster: true})
	if _, err := runtime.Room.AnnounceAllActors(runtime.Session); err != nil {
		t.Fatal(err)
	}
	targetKey := runtime.Room.Snapshot().Monsters[0].ObjectKey
	wantErr := errors.New("pending op115 write failed")
	conn := &failNthDungeonWriteConn{failAt: 2, err: wantErr}
	session := &gameSession{
		conn:                conn,
		connID:              "pending-op115-retry-test",
		selectedCharacterID: 99,
		dungeon:             dungeonSessionState{runtime: runtime},
	}
	request := bossDieCheckRequestBody(99, uint16(targetKey))
	if err := service.handleDungeonBossDieCheck(session, request); err != nil {
		t.Fatal(err)
	}
	deathBody := make([]byte, dungeoncmd.DieMonsterRequestSize)
	binary.LittleEndian.PutUint32(deathBody[0:4], targetKey)
	binary.LittleEndian.PutUint16(deathBody[4:6], 99)
	if err := service.handleDungeonMonsterDeath(session, deathBody); !errors.Is(err, wantErr) {
		t.Fatalf("automatic completion error=%v want=%v", err, wantErr)
	}
	if runtime.bossDieCheckPending || !runtime.bossDieCheckAccepted ||
		!runtime.tutorialCompletionPersisted || runtime.bossDieCheckResponseSent ||
		session.dungeon.runtime != runtime {
		t.Fatalf("failed op115 pending=%t accepted=%t persisted=%t response=%t runtime=%p",
			runtime.bossDieCheckPending, runtime.bossDieCheckAccepted,
			runtime.tutorialCompletionPersisted, runtime.bossDieCheckResponseSent,
			session.dungeon.runtime)
	}
	if status := runtime.Session.Snapshot().Run.Status; status != worldmap.DungeonRunCompleted {
		t.Fatalf("failed op115 run status=%s", status)
	}
	if err := service.handleDungeonBossDieCheck(session, request); err != nil {
		t.Fatalf("retry exact op117: %v", err)
	}
	deathAck, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if deathAck.Header.MsgID != uint16(dnfenum.CmdPacketNotifyDieMonster) {
		t.Fatalf("first packet=%+v", deathAck.Header)
	}
	assertBossResponseAndSettlementEntry(t, rest, uint16(targetKey))
}

func TestBossDieCheckOp115WriteFailureRetriesWithoutDuplicatePersistence(t *testing.T) {
	service, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{singleMonster: true})
	if _, err := runtime.Room.AnnounceAllActors(runtime.Session); err != nil {
		t.Fatal(err)
	}
	targetKey := runtime.Room.Snapshot().Monsters[0].ObjectKey
	if _, _, err := runtime.Room.CommitActorDeathReport(targetKey, runtime.Session); err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("op115 write failed")
	conn := &failNthDungeonWriteConn{failAt: 1, err: wantErr}
	session := &gameSession{
		conn:                conn,
		connID:              "op115-retry-test",
		selectedCharacterID: 99,
		dungeon:             dungeonSessionState{runtime: runtime},
	}
	request := bossDieCheckRequestBody(99, uint16(targetKey))
	if err := service.handleDungeonBossDieCheck(session, request); !errors.Is(err, wantErr) {
		t.Fatalf("op115 failure=%v want=%v", err, wantErr)
	}
	if !runtime.bossDieCheckAccepted || !runtime.tutorialCompletionPersisted ||
		runtime.bossDieCheckResponseSent || session.dungeon.runtime != runtime {
		t.Fatalf("failed op115 accepted=%t persisted=%t response=%t runtime=%p",
			runtime.bossDieCheckAccepted, runtime.tutorialCompletionPersisted,
			runtime.bossDieCheckResponseSent, session.dungeon.runtime)
	}
	if err := service.handleDungeonBossDieCheck(session, request); err != nil {
		t.Fatalf("retry op115: %v", err)
	}
	assertBossResponseAndSettlementEntry(t, conn.write.Bytes(), uint16(targetKey))
}

func TestHandleDungeonBossDieCheckOp31WriteFailureRetriesOnlySettlementEntry(t *testing.T) {
	service, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{})
	if _, err := runtime.Room.AnnounceAllActors(runtime.Session); err != nil {
		t.Fatal(err)
	}
	targetKey := runtime.Room.Snapshot().Monsters[0].ObjectKey
	if _, _, err := runtime.Room.CommitActorDeathReport(targetKey, runtime.Session); err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("settlement op31 write failed")
	conn := &failNthDungeonWriteConn{failAt: 2, err: wantErr}
	session := &gameSession{
		conn:                conn,
		connID:              "op117-op31-write-failure-test",
		selectedCharacterID: 99,
		dungeon:             dungeonSessionState{runtime: runtime},
	}
	request := bossDieCheckRequestBody(99, uint16(targetKey))
	if err := service.handleDungeonBossDieCheck(session, request); !errors.Is(err, wantErr) {
		t.Fatalf("op31 write failure=%v want=%v", err, wantErr)
	}
	first, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if first.Header.MsgID != uint16(dnfenum.CmdPacketNotifyBossDieCheck) ||
		!bytes.Equal(first.Body, buildCurrentDungeonBossDieCheckResponse(uint16(targetKey))) || len(rest) != 0 {
		t.Fatalf("failed op31 stream first=%+v body=%x rest=%x", first.Header, first.Body, rest)
	}
	if !runtime.bossDieCheckAccepted || !runtime.tutorialCompletionPersisted ||
		!runtime.bossDieCheckResponseSent || runtime.tutorialFinalFlagAckSent ||
		runtime.settlementEntrySent || runtime.townReturnPending || runtime.townReturnOp24Sent ||
		session.dungeon.runtime != runtime {
		t.Fatalf("failed op31 lost retry owner accepted=%t persisted=%t ack=%t response=%t runtime=%p",
			runtime.bossDieCheckAccepted, runtime.tutorialCompletionPersisted,
			runtime.tutorialFinalFlagAckSent, runtime.bossDieCheckResponseSent,
			session.dungeon.runtime)
	}
	if status := runtime.Session.Snapshot().Run.Status; status != worldmap.DungeonRunCompleted {
		t.Fatalf("failed op31 run status=%s", status)
	}

	if err := service.handleDungeonBossDieCheck(session, request); err != nil {
		t.Fatalf("retry op31: %v", err)
	}
	first, rest = splitGameServerUpperPacket(t, conn.write.Bytes())
	second, rest := splitGameServerUpperPacket(t, rest)
	if first.Header.MsgID != uint16(dnfenum.CmdPacketNotifyBossDieCheck) ||
		second.Header.MsgID != currentDungeonSettlementEntryMsgID ||
		second.Header.Classification != 0 || !bytes.Equal(second.Body, []byte{0}) ||
		len(rest) != 0 {
		t.Fatalf("retried stream first=%+v second=%+v second_body=%x rest=%x",
			first.Header, second.Header, second.Body, rest)
	}
	if session.dungeon.runtime != runtime || !runtime.settlementEntrySent ||
		runtime.townReturnPending || runtime.townReturnOp24Sent {
		t.Fatalf("successful retry lost pending runtime=%+v state=%+v", session.dungeon.runtime, runtime)
	}
}

func TestHandleDungeonBossDieCheckPersistenceFailureBlocksOp115AndTownUntilReplay(t *testing.T) {
	service, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{singleMonster: true})
	if _, err := runtime.Room.AnnounceAllActors(runtime.Session); err != nil {
		t.Fatal(err)
	}
	targetKey := runtime.Room.Snapshot().Monsters[0].ObjectKey
	if _, _, err := runtime.Room.CommitActorDeathReport(targetKey, runtime.Session); err != nil {
		t.Fatal(err)
	}
	baseRepositories, ok := service.repositoryGroup()
	if !ok || baseRepositories.Character == nil {
		t.Fatal("tutorial fixture character repository unavailable")
	}
	wantErr := errors.New("tutorial completion save failed")
	failingRepositories := baseRepositories
	failingRepositories.Character = &tutorialFailingCharacterStore{
		CharacterRepository: baseRepositories.Character,
		err:                 wantErr,
	}
	service.repositoryProvider = func() (dnfrepo.Group, bool) {
		return failingRepositories, true
	}
	conn := &bufferConn{}
	session := &gameSession{
		conn:                conn,
		connID:              "op117-persistence-retry-test",
		selectedCharacterID: 99,
		dungeon:             dungeonSessionState{runtime: runtime},
	}
	request := bossDieCheckRequestBody(99, uint16(targetKey))
	if err := service.handleDungeonBossDieCheck(session, request); err != nil {
		t.Fatalf("persistence failure must keep connection alive: %v", err)
	}
	if !runtime.bossDieCheckAccepted || runtime.tutorialCompletionPersisted ||
		runtime.bossDieCheckResponseSent || conn.write.Len() != 0 || session.dungeon.runtime != runtime {
		t.Fatalf("failed persistence accepted=%t persisted=%t response=%t wrote=%x runtime=%p",
			runtime.bossDieCheckAccepted, runtime.tutorialCompletionPersisted,
			runtime.bossDieCheckResponseSent, conn.write.Bytes(), session.dungeon.runtime)
	}
	character, found, err := baseRepositories.Character.Load(context.Background(), "99")
	if err != nil || !found {
		t.Fatalf("load character after failed persistence found=%t err=%v", found, err)
	}
	if got := character.Stats[currentDungeonTutorialCompletedKey]; got != 0 {
		t.Fatalf("failed persistence changed tutorial flag=%d", got)
	}

	service.repositoryProvider = func() (dnfrepo.Group, bool) {
		return baseRepositories, true
	}
	if err := service.handleDungeonBossDieCheck(session, request); err != nil {
		t.Fatalf("retry final op117: %v", err)
	}
	assertBossResponseAndSettlementEntry(t, conn.write.Bytes(), uint16(targetKey))
	assertTutorialCompletionPersistedAndSelectAcked(t, service, 99)
	if session.dungeon.runtime != runtime || !runtime.settlementEntrySent || runtime.townReturnPending {
		t.Fatalf("successful persistence replay lost pending runtime=%+v", session.dungeon.runtime)
	}
}

func TestBuildCurrentDungeonBossDieCheckResponseUsesValidatedObjectKey(t *testing.T) {
	if got := buildCurrentDungeonBossDieCheckResponse(0x3412); !bytes.Equal(got, []byte{1, 1, 0x12, 0x34}) {
		t.Fatalf("response=%x", got)
	}
}

func assertBossResponseAndSettlementEntry(t *testing.T, stream []byte, targetObjectKey uint16) {
	t.Helper()
	op115, rest := splitGameServerUpperPacket(t, stream)
	want := buildCurrentDungeonBossDieCheckResponse(targetObjectKey)
	if op115.Header.MsgID != uint16(dnfenum.CmdPacketNotifyBossDieCheck) ||
		op115.Header.Classification != 0 || !bytes.Equal(op115.Body, want) {
		t.Fatalf("op115 response header=%+v body=%x want=%x rest=%x", op115.Header, op115.Body, want, rest)
	}
	entry, rest := splitGameServerUpperPacket(t, rest)
	if entry.Header.MsgID != currentDungeonSettlementEntryMsgID || entry.Header.Classification != 0 ||
		!bytes.Equal(entry.Body, []byte{0}) {
		t.Fatalf("op31 settlement entry header=%+v body=%x rest=%x", entry.Header, entry.Body, rest)
	}
	if len(rest) != 0 {
		t.Fatalf("settlement entry injected an unproven result/reward packet=%x", rest)
	}
}

func assertTutorialFlagAckAndTownReturn(t *testing.T, stream []byte) {
	t.Helper()
	ack, rest := splitGameServerUpperPacket(t, stream)
	if ack.Header.MsgID != uint16(dnfenum.CmdPacketChangeTutorialFlag) ||
		ack.Header.Classification != dnfproto.DefaultChannelClassification ||
		!bytes.Equal(ack.Body, []byte{1, 0}) {
		t.Fatalf("op143 ack header=%+v body=%x rest=%x", ack.Header, ack.Body, rest)
	}
	transition, rest := splitGameServerUpperPacket(t, rest)
	if transition.Header.MsgID != currentSceneTransitionMsgID || transition.Header.Classification != 0 ||
		!bytes.Equal(transition.Body, wantTutorialCompletionTownTransitionBody()) {
		t.Fatalf("op24 response header=%+v body=%x rest=%x", transition.Header, transition.Body, rest)
	}
	if len(rest) != 0 {
		t.Fatalf("tutorial return injected an unconfirmed actor-state packet=%x", rest)
	}
}

func tutorialFinalFlagRequestBody() []byte {
	body := make([]byte, dungeoncmd.ChangeTutorialFlagRequestSize)
	body[0] = currentDungeonTutorialFinalPrefix
	binary.LittleEndian.PutUint32(body[1:5], currentDungeonTutorialFinalProgress)
	body[5] = currentDungeonTutorialFinalCommit
	return body
}

func bossDieCheckRequestBody(relatedActorObjectKey, targetObjectKey uint16) []byte {
	body := make([]byte, dungeoncmd.BossDieCheckRequestSize)
	binary.LittleEndian.PutUint16(body[0:2], relatedActorObjectKey)
	binary.LittleEndian.PutUint16(body[2:4], targetObjectKey)
	return body
}
