package dnfbridge

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"testing"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	"longheng.io/server/internal/modules/dnf/dungeoncmd"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

func TestHandleGameUpperRoutesProgress30TutorialCheckpoint(t *testing.T) {
	service, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{singleMonster: true})
	conn := &bufferConn{}
	session := &gameSession{
		conn:                conn,
		connID:              "progress30-op143-upper-route-test",
		selectedCharacterID: 99,
		dungeon:             dungeonSessionState{runtime: runtime},
	}
	request, err := dnfproto.BuildChannelPacket(
		uint16(dnfenum.CmdPacketChangeTutorialFlag),
		[]byte{currentDungeonTutorialFinalPrefix, 30, 0, 0, 0, currentDungeonTutorialFinalCommit},
		0,
		dnfproto.DefaultChannelClassification,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.handleGameUpper(session, request); err != nil {
		t.Fatal(err)
	}
	ack, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if ack.Header.MsgID != uint16(dnfenum.CmdPacketChangeTutorialFlag) ||
		ack.Header.Classification != dnfproto.DefaultChannelClassification ||
		!bytes.Equal(ack.Body, []byte{1, 0}) {
		t.Fatalf("progress30 route ack=%+v body=%x rest=%x", ack.Header, ack.Body, rest)
	}
	if len(rest) != 0 {
		t.Fatalf("progress30 route sent unexpected packets=%x", rest)
	}
	if session.dungeon.runtime != runtime || runtime.tutorialCompletionPersisted {
		t.Fatalf("progress30 route changed runtime=%p persisted=%t", session.dungeon.runtime, runtime.tutorialCompletionPersisted)
	}
}

func TestHandleDungeonTutorialFlagAcknowledgesProgress30WithoutMutation(t *testing.T) {
	service, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{singleMonster: true})
	conn := &bufferConn{}
	session := &gameSession{
		conn:                conn,
		connID:              "progress30-op143-ack-test",
		selectedCharacterID: 99,
		dungeon:             dungeonSessionState{runtime: runtime},
	}
	before := runtime.Session.Snapshot()
	body := []byte{currentDungeonTutorialFinalPrefix, 30, 0, 0, 0, currentDungeonTutorialFinalCommit}
	if err := service.handleDungeonTutorialFlag(session, body); err != nil {
		t.Fatal(err)
	}

	ack, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if ack.Header.MsgID != uint16(dnfenum.CmdPacketChangeTutorialFlag) ||
		ack.Header.Classification != dnfproto.DefaultChannelClassification ||
		!bytes.Equal(ack.Body, []byte{1, 0}) || len(rest) != 0 {
		t.Fatalf("progress30 ack header=%+v body=%x rest=%x", ack.Header, ack.Body, rest)
	}
	if session.dungeon.runtime != runtime {
		t.Fatalf("progress30 changed runtime owner before=%p after=%p", runtime, session.dungeon.runtime)
	}
	after := runtime.Session.Snapshot()
	if after.Run.Status != before.Run.Status || after.Run.Current != before.Run.Current ||
		runtime.tutorialCompletionPersisted || runtime.tutorialFinalFlagAckSent ||
		runtime.bossDieCheckResponseSent {
		t.Fatalf("progress30 mutated completion before=%+v after=%+v runtime=%+v", before.Run, after.Run, runtime)
	}
	repositories, ok := service.repositoryGroup()
	if !ok || repositories.Character == nil {
		t.Fatal("progress30 character repository unavailable")
	}
	character, found, err := repositories.Character.Load(context.Background(), "99")
	if err != nil || !found {
		t.Fatalf("load progress30 character found=%t err=%v", found, err)
	}
	if got := character.Stats[currentDungeonTutorialCompletedKey]; got != 0 {
		t.Fatalf("progress30 persisted tutorial completion=%d", got)
	}
}

func TestHandleDungeonTutorialFlagRejectsNonFinalOrEarlyRequests(t *testing.T) {
	tests := []struct {
		name string
		body []byte
	}{
		{name: "short", body: make([]byte, dungeoncmd.ChangeTutorialFlagRequestSize-1)},
		{name: "protected padded boundary", body: make([]byte, 16)},
		{name: "wrong prefix", body: []byte{1, 31, 0, 0, 0, 1}},
		{name: "unsupported progress", body: []byte{0, 29, 0, 0, 0, 1}},
		{name: "progress36 owned by active dungeon instead of completed select", body: []byte{0, 36, 0, 0, 0, 1}},
		{name: "wrong commit", body: []byte{0, 31, 0, 0, 0, 0}},
		{name: "final progress outside persisted reentry", body: tutorialFinalFlagRequestBody()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{singleMonster: true})
			conn := &bufferConn{}
			session := &gameSession{
				conn:                conn,
				connID:              "rejected-op143-test",
				selectedCharacterID: 99,
				dungeon:             dungeonSessionState{runtime: runtime},
			}
			if err := service.handleDungeonTutorialFlag(session, test.body); err != nil {
				t.Fatal(err)
			}
			if conn.write.Len() != 0 || session.dungeon.runtime != runtime {
				t.Fatalf("rejected op143 wrote=%x runtime=%p", conn.write.Bytes(), session.dungeon.runtime)
			}
		})
	}
}

func TestHandleDungeonTutorialFlagRejectsWrongClass(t *testing.T) {
	service, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{singleMonster: true})
	conn := &bufferConn{}
	session := &gameSession{
		conn:                conn,
		connID:              "wrong-class-op143-test",
		selectedCharacterID: 99,
		dungeon:             dungeonSessionState{runtime: runtime},
	}
	request, err := dnfproto.BuildChannelPacket(
		uint16(dnfenum.CmdPacketChangeTutorialFlag),
		tutorialFinalFlagRequestBody(),
		0,
		2,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.handleGameUpper(session, request); err != nil {
		t.Fatal(err)
	}
	if conn.write.Len() != 0 || session.dungeon.runtime != runtime {
		t.Fatalf("wrong-class op143 wrote=%x runtime=%p", conn.write.Bytes(), session.dungeon.runtime)
	}
}

func TestTutorialFlagSuccessBodyIsEmptyRewardList(t *testing.T) {
	service, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{singleMonster: true})
	conn := &bufferConn{}
	session := &gameSession{conn: conn, selectedCharacterID: 99, dungeon: dungeonSessionState{runtime: runtime}}
	if err := service.handleDungeonTutorialFlag(session, []byte{0, 30, 0, 0, 0, 1}); err != nil {
		t.Fatal(err)
	}
	ack, _ := splitGameServerUpperPacket(t, conn.write.Bytes())
	if !bytes.Equal(ack.Body, []byte{1, 0}) {
		t.Fatalf("op143 success body=%x want success=1 reward_count=0", ack.Body)
	}
}

// Live current-EXE evidence showed that the persisted reentry op33/op143/op24
// fast exit leaves the client in the dungeon and suppresses its next op45.
// Keep the durable marker but do not emit the unaccepted transition chain.
func TestPersistedCompletedTutorialReentryDoesNotPoisonBoundDungeonScene(t *testing.T) {
	service, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{
		job:               "2",
		dungeonReference:  tutorialScopeGunnerDungeonReference,
		mapDirectory:      tutorialScopeGunnerMapDirectory,
		singleMonster:     true,
		tutorialCompleted: true,
	})
	if !runtime.tutorialCompletionPersisted || !runtime.tutorialCompletedReentry {
		t.Fatalf("persisted tutorial marker was not projected into runtime: %+v", runtime)
	}
	conn := &bufferConn{}
	session := &gameSession{
		conn:                        conn,
		connID:                      "completed-tutorial-reentry-test",
		selectedCharacterID:         99,
		dungeon:                     dungeonSessionState{runtime: runtime},
		postStartMapPlayerStateSent: true,
	}
	if err := service.sendCompletedTutorialReentryExit(session, "before_finish_loading"); err != nil {
		t.Fatal(err)
	}
	if conn.write.Len() != 0 || runtime.tutorialCompletedReentryExitSent {
		t.Fatalf("pre-FINISH_LOADING reentry exit wrote=%x sent=%t", conn.write.Bytes(), runtime.tutorialCompletedReentryExitSent)
	}

	repositories, ok := service.repositoryGroup()
	if !ok || repositories.Skill == nil {
		t.Fatal("tutorial reentry skill repository unavailable")
	}
	if err := repositories.Skill.Save(context.Background(), dnfrepo.SkillRecord{
		CharacterID: "99",
		Skills: map[int64]dnfrepo.SkillState{
			1: {Level: 1, Enabled: true},
		},
	}); err != nil {
		t.Fatal(err)
	}
	skillCatalog, err := buildSkillCatalogFromSource(context.Background(), initialEquipmentMemSource{
		"skill/skilllist.lst":     "2 `job2.lst`\n",
		"skill/job2.lst":          "1 `job2/one.skl`\n",
		"skill/job2/one.skl":      "[skill type]\n`active`\n[skill class]\n1\n[required level]\n1\n",
		"character/character.lst": "2 `job2.chr`\n",
		"character/job2.chr":      "[job]\n`[gunner]`\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	service.initialSkillsByJob = map[byte][]initialSkillEntry{2: {{SkillID: 1, Level: 1}}}
	service.initialSPTable = map[int]int{}
	service.skillCatalog = skillCatalog
	if err := service.sendFinishLoadingStatus(session, make([]byte, 16)); err != nil {
		t.Fatal(err)
	}
	stream := conn.write.Bytes()
	var packets []dnfproto.ChannelPacket
	for len(stream) != 0 {
		packet, rest := splitGameServerUpperPacket(t, stream)
		packets = append(packets, packet)
		stream = rest
	}
	if len(packets) < 2 {
		t.Fatalf("FINISH_LOADING reentry packet count=%d", len(packets))
	}
	status := packets[0]
	if status.Header.MsgID != uint16(dnfenum.CmdPacketFinishLoading) ||
		status.Header.Classification != dnfproto.DefaultChannelClassification ||
		!bytes.Equal(status.Body, []byte{1}) {
		t.Fatalf("FINISH_LOADING status header=%+v body=%x", status.Header, status.Body)
	}
	for _, packet := range packets {
		if packet.Header.MsgID == currentDungeonTutorialExitMsgID {
			t.Fatalf("FINISH_LOADING emitted disabled reentry op33: header=%+v body=%x", packet.Header, packet.Body)
		}
	}
	if !session.currentFinishLoadingStateSent || !session.postFinishLoadingPlayerStateSent ||
		runtime.tutorialCompletedReentryExitSent {
		t.Fatalf("FINISH_LOADING did not commit scene/exit gates: session=%+v runtime=%+v", session, runtime)
	}
	beforeDuplicate := conn.write.Len()
	if err := service.sendCompletedTutorialReentryExit(session, "duplicate_finish_loading"); err != nil {
		t.Fatal(err)
	}
	if conn.write.Len() != beforeDuplicate {
		t.Fatalf("duplicate FINISH_LOADING emitted another op33: before=%d after=%d", beforeDuplicate, conn.write.Len())
	}

	conn.write.Reset()
	if err := service.handleDungeonTutorialFlag(session, tutorialFinalFlagRequestBody()); err != nil {
		t.Fatal(err)
	}
	if conn.write.Len() != 0 {
		t.Fatalf("unsolicited progress31 produced disabled reentry return=%x", conn.write.Bytes())
	}
	if runtime.bossDieCheckResponseSent {
		t.Fatal("persisted reentry emitted the normal boss-completion op115")
	}
	if session.dungeon.runtime != runtime || runtime.townReturnPending || runtime.townReturnOp24Sent {
		t.Fatalf("disabled persisted reentry changed runtime=%+v state=%+v", session.dungeon.runtime, runtime)
	}
	if status := runtime.Session.Snapshot().Run.Status; status != worldmap.DungeonRunActive {
		t.Fatalf("unconfirmed persisted reentry return status=%s want active", status)
	}
	assertTutorialCompletionPersistedAndSelectAcked(t, service, 99)
}

func TestCompletedMarkerDoesNotFastExitANonPVFTutorialDungeon(t *testing.T) {
	service, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{
		singleMonster:     true,
		tutorialCompleted: true,
		omitTutorialFlag:  true,
	})
	conn := &bufferConn{}
	session := &gameSession{
		conn:                             conn,
		selectedCharacterID:              99,
		dungeon:                          dungeonSessionState{runtime: runtime},
		postStartMapPlayerStateSent:      true,
		currentFinishLoadingStateSent:    true,
		postFinishLoadingPlayerStateSent: true,
	}
	if err := service.sendCompletedTutorialReentryExit(session, "non_tutorial"); err != nil {
		t.Fatal(err)
	}
	if conn.write.Len() != 0 || runtime.tutorialCompletedReentry || runtime.tutorialCompletedReentryExitSent {
		t.Fatalf("non-PVF tutorial fast exit wrote=%x runtime=%+v", conn.write.Bytes(), runtime)
	}
}

func TestPersistedCompletedTutorialReentryRejectsStaleRuntimeCharacterOwner(t *testing.T) {
	service, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{
		singleMonster:     true,
		tutorialCompleted: true,
	})
	conn := &bufferConn{}
	session := &gameSession{
		conn:                             conn,
		selectedCharacterID:              100,
		dungeon:                          dungeonSessionState{runtime: runtime},
		postStartMapPlayerStateSent:      true,
		currentFinishLoadingStateSent:    true,
		postFinishLoadingPlayerStateSent: true,
	}
	if err := service.sendCompletedTutorialReentryExit(session, "stale_runtime_owner"); err != nil {
		t.Fatal(err)
	}
	if conn.write.Len() != 0 || runtime.tutorialCompletedReentryExitSent {
		t.Fatalf("stale reentry owner wrote=%x committed=%t", conn.write.Bytes(), runtime.tutorialCompletedReentryExitSent)
	}
}

func TestPersistedCompletedTutorialReentryNeverWritesUnprovenOp33(t *testing.T) {
	service, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{
		singleMonster:     true,
		tutorialCompleted: true,
	})
	conn := &failNthDungeonWriteConn{failAt: 1, err: errors.New("unexpected reentry op33 write")}
	session := &gameSession{
		conn:                             conn,
		selectedCharacterID:              99,
		dungeon:                          dungeonSessionState{runtime: runtime},
		postStartMapPlayerStateSent:      true,
		currentFinishLoadingStateSent:    true,
		postFinishLoadingPlayerStateSent: true,
	}
	if err := service.sendCompletedTutorialReentryExit(session, "first_finish_loading"); err != nil {
		t.Fatalf("disabled reentry exit: %v", err)
	}
	if runtime.tutorialCompletedReentryExitSent || conn.write.Len() != 0 {
		t.Fatalf("disabled reentry op33 committed=%t wrote=%x", runtime.tutorialCompletedReentryExitSent, conn.write.Bytes())
	}
	if err := service.sendCompletedTutorialReentryExit(session, "replayed_finish_loading"); err != nil {
		t.Fatalf("duplicate disabled reentry exit: %v", err)
	}
	if runtime.tutorialCompletedReentryExitSent || conn.write.Len() != 0 {
		t.Fatalf("duplicate disabled reentry op33 committed=%t wrote=%x", runtime.tutorialCompletedReentryExitSent, conn.write.Bytes())
	}
}

func assertTutorialCompletionPersistedAndSelectAcked(t *testing.T, service *Service, characterID uint16) {
	t.Helper()
	repositories, ok := service.repositoryGroup()
	if !ok || repositories.Character == nil {
		t.Fatal("tutorial character repository unavailable")
	}
	character, found, err := repositories.Character.Load(context.Background(), strconv.Itoa(int(characterID)))
	if err != nil || !found {
		t.Fatalf("load persisted tutorial character found=%t err=%v", found, err)
	}
	if got := character.Stats[currentDungeonTutorialCompletedKey]; got != currentDungeonTutorialCompleteFlag {
		t.Fatalf("persisted tutorial flag=%d want=%d", got, currentDungeonTutorialCompleteFlag)
	}
	body := buildCurrentSelectCharacterAckBody(character, true, dnfrepo.QuestRecord{}, false, characterID, 0, 0, []byte{0})
	flag, count, ok := currentSelectAckTutorialState(body)
	if !ok || flag != 0 || count != 1 {
		t.Fatalf("relogin select ACK tutorial wire state flag=%d count=%d ok=%t, want ignored flag 0 and one route index", flag, count, ok)
	}
	indexes, ok := currentSelectAckTutorialIndexes(body)
	if !ok || len(indexes) != 1 || indexes[0] != currentSelectAckPage1RouteIndex {
		t.Fatalf("relogin select ACK tutorial indexes=%v ok=%t, want [%d]/true", indexes, ok, currentSelectAckPage1RouteIndex)
	}
}

type tutorialFailingCharacterStore struct {
	dnfrepo.CharacterRepository
	err error
}

func (s *tutorialFailingCharacterStore) Save(context.Context, dnfrepo.CharacterRecord) error {
	return s.err
}
