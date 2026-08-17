package dnfbridge

import (
	"context"
	"testing"

	"longheng.io/server/internal/modules/dnf/dungeoncmd"
	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	dnfquest "longheng.io/server/internal/modules/dnf/quest"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

func TestQuestPassiveObjectOp39Completes3157AndRecomputes3146(t *testing.T) {
	ctx := context.Background()
	index, err := dnfpvf.Build(ctx, bridgePVFSource{
		dnfquest.DefaultList:  "3146 `parent.qst`\n3157 `passive.qst`\n",
		"n_quest/parent.qst":  "[grade]\n`[epic]`\n[type]\n`[quest clear]`\n[int data]\n3157\n",
		"n_quest/passive.qst": "[grade]\n`[sub]`\n[type]\n`[hunt enemy]`\n[main quest]\n3146\n[int data]\n3 -1 13099 3 1\n",
	}, dnfpvf.BuildOptions{Lists: []string{dnfquest.DefaultList}})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := dnfquest.Load(ctx, index)
	if err != nil {
		t.Fatal(err)
	}
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "19", AccountID: "acc", Level: 5}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Quest.Save(ctx, dnfrepo.QuestRecord{CharacterID: "19", States: map[int64]dnfrepo.QuestState{
		3146: {Status: "active", ProgressValue: 1},
		3157: {Status: "active", ProgressValue: 1},
	}}); err != nil {
		t.Fatal(err)
	}
	firstMonsterKey := uint32(437)
	room := &runtimeDungeonRoom{
		monsters:    []runtimeDungeonMonster{{ObjectKey: firstMonsterKey, State: runtimeDungeonMonsterAnnounced}},
		byObjectKey: map[uint32]int{firstMonsterKey: 0},
	}
	runtime := &runtimeDungeonState{
		Request:   dungeoncmd.SelectDungeonRequest{DungeonID: 3, Difficulty: 0},
		Dungeon:   worldmap.Dungeon{ID: 3},
		Character: dnfrepo.CharacterRecord{CharacterID: "19", AccountID: "acc", Level: 5},
		Session:   &worldmap.DungeonSession{},
		Room:      room,
		MazeIndex: 2,
	}
	conn := &bufferConn{}
	session := &gameSession{conn: conn, connID: "quest-passive-object", selectedCharacterID: 19, dungeon: dungeonSessionState{runtime: runtime}}
	service := &Service{
		options:            options{gameUpperBodyCodec: gameUpperBodyCodecPlain},
		questCatalog:       catalog,
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	scene := worldmap.DungeonRoomScene{Map: worldmap.ResolvedMap{Map: worldmap.Map{ID: 76136}}}

	handled, err := service.handleCurrentDungeonQuestPassiveObjectDeathLocked(session, runtime, scene, firstMonsterKey-1)
	if err != nil || !handled {
		t.Fatalf("handle passive object: handled=%t err=%v", handled, err)
	}
	firstPacket, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if firstPacket.Header.MsgID != 38 || firstPacket.Header.Classification != 0 {
		t.Fatalf("passive death ACK header=%+v body=%x", firstPacket.Header, firstPacket.Body)
	}
	questPacket, trailing := splitGameServerUpperPacket(t, rest)
	if questPacket.Header.MsgID != currentActiveQuestSnapshotMsgID || questPacket.Header.Classification != 0 || len(trailing) != 0 {
		t.Fatalf("quest snapshot header=%+v body=%x trailing=%x", questPacket.Header, questPacket.Body, trailing)
	}
	persisted, found, err := repositories.Quest.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load persisted quest: found=%t err=%v", found, err)
	}
	if child := persisted.States[3157]; child.Status != "completed" || child.ProgressValue != 0 || child.Extra["completion_enemy_code"] != "13099" {
		t.Fatalf("3157=%+v", child)
	}
	if parent := persisted.States[3146]; parent.Status != "active" || parent.ProgressValue != 0 {
		t.Fatalf("3146=%+v", parent)
	}
}

func TestQuestPassiveObjectCandidateRejectsNonAdjacentUnknownObject(t *testing.T) {
	room := &runtimeDungeonRoom{monsters: []runtimeDungeonMonster{{ObjectKey: 437, State: runtimeDungeonMonsterAnnounced}}}
	if currentDungeonQuestObjectImmediatelyPrecedesAnnouncedActor(room, 435) {
		t.Fatal("non-adjacent unknown object was accepted")
	}
	if !currentDungeonQuestObjectImmediatelyPrecedesAnnouncedActor(room, 436) {
		t.Fatal("adjacent quest-scene object was not recognized")
	}
}
