package dnfbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"strconv"
	"testing"
	"time"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	"longheng.io/server/internal/modules/dnf/dungeoncmd"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

func TestBosslessPVFTutorialFinalCompletesAfterAllRealBlockersDie(t *testing.T) {
	service, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{
		bossRank:      "[normal]",
		singleMonster: true,
	})
	service.dungeonTutorialScripts = &pvfDungeonTutorialScriptCatalog{
		byMapID: make(map[int64]map[int][]dungeonTutorialScriptEvidence),
	}
	if _, err := runtime.Room.AnnounceAllActors(runtime.Session); err != nil {
		t.Fatal(err)
	}
	conn := &bufferConn{}
	session := &gameSession{
		conn:                conn,
		connID:              "bossless-tutorial-final-test",
		selectedCharacterID: 99,
		dungeon:             dungeonSessionState{runtime: runtime},
	}
	targetKey := runtime.Room.Snapshot().Monsters[0].ObjectKey
	if err := service.handleDungeonMonsterDeath(session, ordinaryTutorialDeathBody(targetKey, 99)); err != nil {
		t.Fatal(err)
	}

	death, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if death.Header.MsgID != uint16(dnfenum.CmdPacketNotifyDieMonster) {
		t.Fatalf("first packet=%+v body=%x", death.Header, death.Body)
	}
	entry, rest := splitGameServerUpperPacket(t, rest)
	if entry.Header.MsgID != currentDungeonSettlementEntryMsgID || !bytes.Equal(entry.Body, []byte{0}) || len(rest) != 0 {
		t.Fatalf("bossless settlement entry=%+v body=%x rest=%x", entry.Header, entry.Body, rest)
	}
	snapshot := runtime.Session.Snapshot()
	if snapshot.Run.Status != worldmap.DungeonRunCompleted || !snapshot.Scene.Cleared ||
		!runtime.tutorialFinalRoomClearAccepted || runtime.tutorialFinalRoomClearPending ||
		!runtime.tutorialCompletionPersisted || !runtime.settlementEntrySent ||
		runtime.bossDieCheckAccepted || runtime.bossDieCheckResponseSent {
		t.Fatalf("bossless final state=%+v runtime=%+v", snapshot, runtime)
	}
	assertTutorialCompletionBirthRoomPersisted(t, service, 99)
}

func TestCMTTutorialFinalGivesImmediateOp117PriorityThenFallsBack(t *testing.T) {
	service, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{
		bossRank:      "[normal]",
		singleMonster: true,
	})
	queue := newFakeCurrentDungeonDeathTimerQueue()
	service.gameplayTimers = queue
	if _, err := runtime.Room.AnnounceAllActors(runtime.Session); err != nil {
		t.Fatal(err)
	}
	conn := &bufferConn{}
	session := &gameSession{
		conn:                conn,
		connID:              "cmt-tutorial-final-grace-test",
		selectedCharacterID: 99,
		dungeon:             dungeonSessionState{runtime: runtime},
	}
	targetKey := runtime.Room.Snapshot().Monsters[0].ObjectKey
	if err := service.handleDungeonMonsterDeath(session, ordinaryTutorialDeathBody(targetKey, 99)); err != nil {
		t.Fatal(err)
	}
	death, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if death.Header.MsgID != uint16(dnfenum.CmdPacketNotifyDieMonster) || len(rest) != 0 ||
		!runtime.tutorialFinalRoomClearPending || runtime.tutorialFinalRoomClearAccepted ||
		runtime.Session.Snapshot().Run.Status != worldmap.DungeonRunActive {
		t.Fatalf("grace state death=%+v rest=%x runtime=%+v", death.Header, rest, runtime)
	}
	task := queue.task(t, 0)
	if task.name != "dnf-dungeon-tutorial-final:cmt-tutorial-final-grace-test:run:0" ||
		task.delay != 750*time.Millisecond {
		t.Fatalf("tutorial grace task=%+v", task)
	}

	conn.write.Reset()
	queue.fire(task, false)
	entry, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if entry.Header.MsgID != currentDungeonSettlementEntryMsgID || !bytes.Equal(entry.Body, []byte{0}) || len(rest) != 0 {
		t.Fatalf("fallback entry=%+v body=%x rest=%x", entry.Header, entry.Body, rest)
	}
	if !runtime.tutorialFinalRoomClearAccepted || runtime.tutorialFinalRoomClearPending ||
		runtime.bossDieCheckAccepted || runtime.bossDieCheckResponseSent {
		t.Fatalf("fallback incorrectly used op117 owner=%+v", runtime)
	}
}

func TestCMTTutorialFinalOp117WinsBeforeFallback(t *testing.T) {
	service, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{
		bossRank:      "[normal]",
		singleMonster: true,
	})
	if _, err := runtime.Room.AnnounceAllActors(runtime.Session); err != nil {
		t.Fatal(err)
	}
	conn := &bufferConn{}
	session := &gameSession{
		conn:                conn,
		connID:              "cmt-tutorial-final-op117-wins-test",
		selectedCharacterID: 99,
		dungeon:             dungeonSessionState{runtime: runtime},
	}
	targetKey := runtime.Room.Snapshot().Monsters[0].ObjectKey
	if err := service.handleDungeonMonsterDeath(session, ordinaryTutorialDeathBody(targetKey, 99)); err != nil {
		t.Fatal(err)
	}
	if !runtime.tutorialFinalRoomClearPending {
		t.Fatal("CMT final did not arm op117 grace")
	}
	conn.write.Reset()
	if err := service.handleDungeonBossDieCheck(session, bossDieCheckRequestBody(99, uint16(targetKey))); err != nil {
		t.Fatal(err)
	}
	assertBossResponseAndSettlementEntry(t, conn.write.Bytes(), uint16(targetKey))
	if !runtime.bossDieCheckAccepted || !runtime.bossDieCheckResponseSent ||
		runtime.tutorialFinalRoomClearAccepted || runtime.tutorialFinalRoomClearPending {
		t.Fatalf("op117 did not retain settlement ownership=%+v", runtime)
	}
}

func TestBossRankTutorialFinalNeverUsesBosslessFallback(t *testing.T) {
	service, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{singleMonster: true})
	if _, err := runtime.Room.AnnounceAllActors(runtime.Session); err != nil {
		t.Fatal(err)
	}
	conn := &bufferConn{}
	session := &gameSession{
		conn:                conn,
		connID:              "boss-rank-no-fallback-test",
		selectedCharacterID: 99,
		dungeon:             dungeonSessionState{runtime: runtime},
	}
	targetKey := runtime.Room.Snapshot().Monsters[0].ObjectKey
	if err := service.handleDungeonMonsterDeath(session, ordinaryTutorialDeathBody(targetKey, 99)); err != nil {
		t.Fatal(err)
	}
	_, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if len(rest) != 0 || runtime.tutorialFinalRoomClearPending ||
		runtime.tutorialFinalRoomClearAccepted || runtime.settlementEntrySent {
		t.Fatalf("boss-ranked final entered fallback rest=%x runtime=%+v", rest, runtime)
	}
}

func ordinaryTutorialDeathBody(objectKey uint32, ownerObjectKey uint16) []byte {
	body := make([]byte, dungeoncmd.DieMonsterRequestSize)
	binary.LittleEndian.PutUint32(body[0:4], objectKey)
	binary.LittleEndian.PutUint16(body[4:6], ownerObjectKey)
	return body
}

func assertTutorialCompletionBirthRoomPersisted(t *testing.T, service *Service, characterID uint16) {
	t.Helper()
	repositories, ok := service.repositoryGroup()
	if !ok || repositories.Character == nil {
		t.Fatal("tutorial character repository unavailable")
	}
	character, found, err := repositories.Character.Load(context.Background(), strconv.Itoa(int(characterID)))
	if err != nil || !found {
		t.Fatalf("load character found=%t err=%v", found, err)
	}
	want := map[string]int64{
		currentDungeonTutorialCompletedKey: currentDungeonTutorialCompleteFlag,
		"town_id":                          newCharacterInitialTownID,
		"area_id":                          newCharacterInitialAreaID,
		"pos_x":                            newCharacterInitialPosX,
		"pos_y":                            newCharacterInitialPosY,
		"direction":                        newCharacterInitialDirection,
		"area_state":                       newCharacterInitialAreaState,
	}
	for key, value := range want {
		if character.Stats[key] != value {
			t.Fatalf("persisted %s=%d want=%d stats=%v", key, character.Stats[key], value, character.Stats)
		}
	}
	if !bytes.Equal([]byte(character.Name), []byte("ATSwordman")) {
		t.Fatalf("completion changed character base record=%+v", character)
	}
}
