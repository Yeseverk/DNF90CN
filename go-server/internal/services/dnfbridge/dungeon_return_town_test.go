package dnfbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"testing"

	"longheng.io/server/internal/modules/dnf/channelcatalog"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	"longheng.io/server/internal/modules/dnf/dungeoncmd"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

func TestHandleDungeonBackToVillageKeepsRunPendingAfterCurrentOp24(t *testing.T) {
	service, session, runtime := newBackToVillageRuntime(t)
	service.markCurrentTownSceneReady(session)
	session.enterSelectDungeonSent = true
	session.enterSelectDungeonAckSent = true
	session.enterSelectDungeonContextSent = true
	session.runtimeAfterBlacklistSent = true
	session.runtimeFinishLoadingGateSent = true
	session.fpsFinishLoadingGateSent = true
	session.selectPreviewActorRemoved = true
	session.preDungeonContextPlayerStateSent = true
	session.currentFinishLoadingStateSent = true
	session.currentFinishLoadingCompletionSent = true
	session.postFinishLoadingPlayerStateSent = true

	request, err := dnfproto.BuildChannelPacket(
		uint16(dnfenum.CmdPacketBack2Village), nil, 0, dnfproto.DefaultChannelClassification,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.handleGameUpper(session, request); err != nil {
		t.Fatalf("handle op132: %v", err)
	}

	response, rest := splitGameServerUpperPacket(t, session.conn.(*bufferConn).write.Bytes())
	if response.Header.MsgID != currentSceneTransitionMsgID || response.Header.Classification != 0 {
		t.Fatalf("return response header = %+v", response.Header)
	}
	if want := wantDungeonTownTransitionBody(); !bytes.Equal(response.Body, want) {
		t.Fatalf("return response body = %x, want %x", response.Body, want)
	}
	if len(rest) != 0 {
		t.Fatalf("unconfirmed return injected a second scene packet=%x", rest)
	}
	if status := runtime.Session.Snapshot().Run.Status; status != worldmap.DungeonRunActive {
		t.Fatalf("pending run status = %s, want %s", status, worldmap.DungeonRunActive)
	}
	if session.dungeon.runtime != runtime || !runtime.townReturnPending || !runtime.townReturnOp24Sent {
		t.Fatalf("pending return lost runtime/state: owner=%p state=%+v", session.dungeon.runtime, runtime)
	}
	if !session.enterSelectDungeonSent || !session.enterSelectDungeonAckSent || !session.enterSelectDungeonContextSent {
		t.Fatal("active-runtime op132 incorrectly consumed the town selector context")
	}
	if !session.runtimeAfterBlacklistSent || !session.runtimeFinishLoadingGateSent || !session.fpsFinishLoadingGateSent ||
		!session.selectPreviewActorRemoved || !session.preDungeonContextPlayerStateSent ||
		!session.currentFinishLoadingStateSent || !session.currentFinishLoadingCompletionSent ||
		!session.postFinishLoadingPlayerStateSent {
		t.Fatal("unconfirmed return cleared a live dungeon loading gate")
	}
}

func TestConfirmedActiveTutorialExitPersistsBeforeImmediateCharacterSwitch(t *testing.T) {
	for _, fixture := range []struct {
		name string
		exit func(*Service, *gameSession) error
	}{
		{
			name: "in-dungeon give-up confirmation op42",
			exit: func(service *Service, session *gameSession) error {
				return service.handleDungeonGiveupGame(session, nil)
			},
		},
		{
			name: "system back-to-village confirmation op132",
			exit: func(service *Service, session *gameSession) error {
				return service.handleDungeonBackToVillage(session, nil)
			},
		},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			service, runtime := prepareTutorialScopeRuntime(t, tutorialScopeFixtureOptions{})
			connection := &bufferConn{}
			session := &gameSession{
				conn:                connection,
				connID:              "tutorial-abandon-return-test",
				selectedCharacterID: 99,
				dungeon:             dungeonSessionState{runtime: runtime},
			}

			if err := fixture.exit(service, session); err != nil {
				t.Fatalf("handle active tutorial exit: %v", err)
			}

			response, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
			if response.Header.MsgID != currentSceneTransitionMsgID ||
				response.Header.Classification != 0 ||
				!bytes.Equal(response.Body, wantTutorialCompletionTownTransitionBody()) ||
				len(rest) != 0 {
				t.Fatalf("tutorial abandon response=%+v body=%x rest=%x",
					response.Header, response.Body, rest)
			}
			if !runtime.tutorialCompletionPersisted ||
				!hasPersistedDungeonTutorialCompletion(runtime.Character) ||
				!runtime.townReturnPending ||
				!runtime.townReturnOp24Sent {
				t.Fatalf("tutorial abandon runtime=%+v", runtime)
			}
			repositories, ok := service.repositoryGroup()
			if !ok || repositories.Character == nil {
				t.Fatal("tutorial abandon character repository unavailable")
			}
			character, found, err := repositories.Character.Load(context.Background(), "99")
			if err != nil || !found {
				t.Fatalf("load tutorial abandon character found=%t err=%v", found, err)
			}
			if !hasPersistedDungeonTutorialCompletion(character) ||
				character.Stats["town_id"] != newCharacterInitialTownID ||
				character.Stats["area_id"] != newCharacterInitialAreaID {
				t.Fatalf("tutorial abandon character stats=%+v", character.Stats)
			}

			// Match the reported sequence: switch characters immediately after
			// op24, before any later town-side request commits the retained run.
			previousCharacterID, abandoned := service.resetGameSessionForCharacterSelect(session)
			if previousCharacterID != 99 || !abandoned || session.dungeon.runtime != nil {
				t.Fatalf("immediate character switch char=%d abandoned=%t runtime=%+v",
					previousCharacterID, abandoned, session.dungeon.runtime)
			}
			character, found, err = repositories.Character.Load(context.Background(), "99")
			if err != nil || !found || !hasPersistedDungeonTutorialCompletion(character) {
				t.Fatalf("tutorial completion lost after character switch found=%t stats=%+v err=%v",
					found, character.Stats, err)
			}
		})
	}
}

func TestHandleDungeonBackToVillagePreservesCompletedFinalStatus(t *testing.T) {
	service, session, runtime := newBackToVillageRuntime(t)
	if err := runtime.Session.CompleteCurrentRoom(); err != nil {
		t.Fatalf("complete final room: %v", err)
	}
	if err := service.handleDungeonBackToVillage(session, nil); err != nil {
		t.Fatalf("handle completed op132: %v", err)
	}

	response, rest := splitGameServerUpperPacket(t, session.conn.(*bufferConn).write.Bytes())
	if response.Header.MsgID != currentSceneTransitionMsgID || response.Header.Classification != 0 ||
		!bytes.Equal(response.Body, wantDungeonTownTransitionBody()) {
		t.Fatalf("completed return response=%+v body=%x rest=%x", response.Header, response.Body, rest)
	}
	if len(rest) != 0 {
		t.Fatalf("completed unconfirmed return injected a second scene packet=%x", rest)
	}
	if status := runtime.Session.Snapshot().Run.Status; status != worldmap.DungeonRunCompleted {
		t.Fatalf("completed return status=%s want=%s", status, worldmap.DungeonRunCompleted)
	}
	if session.dungeon.runtime != runtime || !runtime.townReturnPending {
		t.Fatalf("completed pending return lost runtime=%+v", session.dungeon.runtime)
	}
	connection := session.conn.(*bufferConn)
	connection.write.Reset()
	if err := service.commitPendingDungeonReturnForSceneRequest(session, "test_current_op16_confirmation"); err != nil {
		t.Fatalf("commit completed return: %v", err)
	}
	if trailing := splitTownPostTransitionPlayerState(
		t,
		connection.write.Bytes(),
		session,
		session.selectedCharacterID,
		backToVillageSkillProjectionBody(t, service),
		false,
	); len(trailing) != 0 {
		t.Fatalf("confirmed completed return trailing packets=%x", trailing)
	}
	if session.dungeon.runtime != nil {
		t.Fatalf("confirmed completed return retained runtime=%+v", session.dungeon.runtime)
	}
	if !session.returnTownFinishLoadingAckOnly {
		t.Fatal("confirmed completed return did not arm op37 ACK-only guard")
	}
	if status := runtime.Session.Snapshot().Run.Status; status != worldmap.DungeonRunCompleted {
		t.Fatalf("confirmed completed return status=%s want completed", status)
	}
}

func TestCompletedDungeonReturnDoesNotReplayInitialTownPlayerInfoAfterTransition(t *testing.T) {
	service, session, runtime := newBackToVillageRuntime(t)
	repositories, ok := service.repositoryGroup()
	if !ok {
		t.Fatal("completed return repository group unavailable")
	}
	if err := repositories.Equipment.Save(context.Background(), dnfrepo.EquipmentRecord{
		CharacterID: "99",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"11": {
				SlotIndex: 11,
				ItemID:    900001,
				RawEntry:  buildInitialEquipmentRawEntry(11, 900001, 27),
			},
		},
	}); err != nil {
		t.Fatalf("save completed-return equipment: %v", err)
	}
	enableTownMoveSkillProjection(t, service, repositories, "99")
	session.channel.ID = 253
	session.channel.Name = "ch.253"
	session.channel.Port = 10253
	session.residentChannel = session.channel
	session.connectionTownActorOwnerChannel = 253
	session.townActorOwnerChannel = currentSceneObjectContext
	if err := runtime.Session.CompleteCurrentRoom(); err != nil {
		t.Fatalf("complete final room: %v", err)
	}
	transition, err := service.prepareCurrentDungeonTownTransition(context.Background(), session.selectedCharacterID)
	if err != nil {
		t.Fatal(err)
	}
	connection := session.conn.(*bufferConn)
	connection.write.Reset()

	if err := service.sendCurrentCompletedDungeonReturnToTown(
		session,
		runtime,
		transition,
		uint16(dnfenum.CmdPacketEplpCommand),
		"test_card_exit_return_to_town",
	); err != nil {
		t.Fatalf("completed dungeon town route: %v", err)
	}
	if session.initialTownRouteStage != currentInitialTownRoutePlayerStateSent ||
		!session.sceneBootstrapTailDeferred ||
		!session.selectedUserInfoRefreshSent ||
		session.selectedUserInfoMode3Sent {
		t.Fatalf("completed return flags stage=%d tail_deferred=%t mode1=%t mode3=%t",
			session.initialTownRouteStage,
			session.sceneBootstrapTailDeferred,
			session.selectedUserInfoRefreshSent,
			session.selectedUserInfoMode3Sent)
	}
	if containsClass0Op2ModeForTest(t, connection.write.Bytes(), 3) {
		t.Fatalf("completed dungeon return route replayed mode3 before town transition: %x", connection.write.Bytes())
	}
	targetOwner := byte(253)
	mode0Count := 0
	mode1Count := 0
	for ownerStream := connection.write.Bytes(); len(ownerStream) > 0; {
		packet, next := splitCurrentGameServerUpperPacketAuto(t, ownerStream)
		ownerStream = next
		if packet.Header.Classification != 0 ||
			packet.Header.MsgID != uint16(dnfenum.CmdPacketSetUDPIPPort) ||
			len(packet.Body) < 5 {
			continue
		}
		switch packet.Body[0] {
		case 0:
			mode0Count++
		case 1:
			mode1Count++
			if len(packet.Body) <= currentMode1CreateRowsOffset+5 ||
				packet.Body[currentMode1CreateCountOffset] != 1 ||
				binary.LittleEndian.Uint32(
					packet.Body[currentMode1CreateRowsOffset+1:currentMode1CreateRowsOffset+5],
				) != 900001 {
				t.Fatalf("completed dungeon return mode1 missing equipped-item create row body=%x", packet.Body)
			}
		default:
			continue
		}
		if packet.Body[3] != 0 || packet.Body[4] != targetOwner {
			t.Fatalf(
				"completed dungeon return mode%d owner=%x want 00/%02x",
				packet.Body[0],
				packet.Body[:5],
				targetOwner,
			)
		}
	}
	if mode0Count != 1 || mode1Count != 1 ||
		session.townActorOwnerChannel != targetOwner {
		t.Fatalf(
			"completed dungeon return owner packets mode0=%d mode1=%d owner=%d want 1/1/%d",
			mode0Count,
			mode1Count,
			session.townActorOwnerChannel,
			targetOwner,
		)
	}

	connection.write.Reset()
	if err := service.sendDeferredSelectSceneTail(session, "test_after_town_transition"); err != nil {
		t.Fatalf("deferred scene tail after completed return: %v", err)
	}
	if session.sceneBootstrapTailSent || !session.sceneBootstrapTailDeferred ||
		connection.write.Len() != 0 {
		t.Fatalf(
			"pre-type1345 deferred tail sent=%t deferred=%t bytes=%x",
			session.sceneBootstrapTailSent,
			session.sceneBootstrapTailDeferred,
			connection.write.Bytes(),
		)
	}
	if session.townPostTransition.stage != currentTownPostTransitionIdle {
		t.Fatalf("completed return pre-type1345 stage=%d want idle", session.townPostTransition.stage)
	}

	if err := service.handleLegacyGamePacket(session, buildLegacyTownSceneReadyAckPacket()); err != nil {
		t.Fatalf("completed return type1345 boundary: %v", err)
	}
	if !session.sceneBootstrapTailSent || session.sceneBootstrapTailDeferred ||
		session.townPostTransition.stage != currentTownPostTransitionIdle {
		t.Fatalf(
			"post-type1345 tail sent=%t deferred=%t stage=%d",
			session.sceneBootstrapTailSent,
			session.sceneBootstrapTailDeferred,
			session.townPostTransition.stage,
		)
	}
	if session.selectedUserInfoMode3Sent ||
		containsClass0Op2ModeForTest(t, connection.write.Bytes(), 3) ||
		containsInitialTownTransitionForTest(t, connection.write.Bytes()) {
		t.Fatalf("deferred tail replayed initial-town player info: mode3_sent=%t stream=%x",
			session.selectedUserInfoMode3Sent,
			connection.write.Bytes())
	}
}

func TestCompletedDungeonReturnRefreshesQuestSnapshotsBeforeAndAfterTransition(t *testing.T) {
	service, session, runtime := newBackToVillageRuntime(t)
	service.questCatalog = buildQuestListTestCatalog(t)
	if err := runtime.Session.CompleteCurrentRoom(); err != nil {
		t.Fatalf("complete final room: %v", err)
	}
	transition, err := service.prepareCurrentDungeonTownTransition(context.Background(), session.selectedCharacterID)
	if err != nil {
		t.Fatal(err)
	}
	connection := session.conn.(*bufferConn)
	connection.write.Reset()

	if err := service.sendCurrentCompletedDungeonReturnToTown(
		session,
		runtime,
		transition,
		uint16(dnfenum.CmdPacketEplpCommand),
		"test_completed_return_quest_refresh",
	); err != nil {
		t.Fatalf("completed dungeon town route: %v", err)
	}

	var beforeTransitionAcceptable, beforeTransitionActive int
	var afterTransitionAcceptable, afterTransitionActive int
	transitionSeen := false
	stream := connection.write.Bytes()
	for len(stream) > 0 {
		packet, rest := splitCurrentGameServerUpperPacketAuto(t, stream)
		switch {
		case packetLooksLikeInitialTownTransition(packet):
			transitionSeen = true
		case packet.Header.Classification == 0 && packet.Header.MsgID == currentAcceptableQuestListMsgID:
			if transitionSeen {
				afterTransitionAcceptable++
			} else {
				beforeTransitionAcceptable++
			}
		case packet.Header.Classification == 0 && packet.Header.MsgID == currentActiveQuestSnapshotMsgID:
			if transitionSeen {
				afterTransitionActive++
			} else {
				beforeTransitionActive++
			}
		}
		stream = rest
	}
	if !transitionSeen {
		t.Fatal("completed dungeon return did not emit typed op24")
	}
	if beforeTransitionAcceptable != 1 || beforeTransitionActive != 1 ||
		afterTransitionAcceptable != 1 || afterTransitionActive != 1 {
		t.Fatalf(
			"quest refresh counts before op24=(op21:%d op574:%d) after op24=(op21:%d op574:%d)",
			beforeTransitionAcceptable,
			beforeTransitionActive,
			afterTransitionAcceptable,
			afterTransitionActive,
		)
	}
}

func TestPendingDungeonReturnKeepsMonsterDeathAuthoritativeAndCancelsWithoutUnsolicitedRetry(t *testing.T) {
	service, session, runtime := newBackToVillageRuntime(t)
	if _, err := runtime.Room.AnnounceAllActors(runtime.Session); err != nil {
		t.Fatal(err)
	}
	if err := service.handleDungeonBackToVillage(session, nil); err != nil {
		t.Fatalf("start pending return: %v", err)
	}
	conn := session.conn.(*bufferConn)
	conn.write.Reset()
	room := runtime.Room.Snapshot()
	if len(room.Monsters) == 0 {
		t.Fatal("return fixture has no monster")
	}
	deathBody := make([]byte, dungeoncmd.DieMonsterRequestSize)
	binary.LittleEndian.PutUint32(deathBody[0:4], room.Monsters[0].ObjectKey)
	binary.LittleEndian.PutUint16(deathBody[4:6], session.selectedCharacterID)
	if err := service.handleDungeonMonsterDeath(session, deathBody); err != nil {
		t.Fatalf("monster death after pending return: %v", err)
	}

	deathAck, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if deathAck.Header.MsgID != uint16(dnfenum.CmdPacketNotifyDieMonster) || deathAck.Header.Classification != 0 {
		t.Fatalf("death ACK header=%+v body=%x", deathAck.Header, deathAck.Body)
	}
	if len(rest) != 0 {
		t.Fatalf("monster death triggered unsolicited return packets=%x", rest)
	}
	if session.dungeon.runtime != runtime || runtime.townReturnPending || runtime.townReturnOp24Sent ||
		runtime.Room.Snapshot().Monsters[0].State != runtimeDungeonMonsterDefeated {
		t.Fatalf("post-death pending state owner=%p runtime=%+v monster=%+v",
			session.dungeon.runtime, runtime, runtime.Room.Snapshot().Monsters[0])
	}
}

func TestPendingActiveDungeonReturnCommitsOnlyAfterTownSideSceneRequest(t *testing.T) {
	service, session, runtime := newBackToVillageRuntime(t)
	if err := service.handleDungeonBackToVillage(session, nil); err != nil {
		t.Fatal(err)
	}
	connection := session.conn.(*bufferConn)
	connection.write.Reset()
	if err := service.commitPendingDungeonReturnForSceneRequest(session, "test_current_op16_confirmation"); err != nil {
		t.Fatal(err)
	}
	if trailing := splitTownPostTransitionPlayerState(
		t,
		connection.write.Bytes(),
		session,
		session.selectedCharacterID,
		backToVillageSkillProjectionBody(t, service),
		false,
	); len(trailing) != 0 {
		t.Fatalf("confirmed active return trailing packets=%x", trailing)
	}
	if session.dungeon.runtime != nil {
		t.Fatalf("confirmed return retained runtime=%+v", session.dungeon.runtime)
	}
	if status := runtime.Session.Snapshot().Run.Status; status != worldmap.DungeonRunAbandoned {
		t.Fatalf("confirmed active return status=%s want abandoned", status)
	}
}

func TestPendingDungeonReturnPlayerStateFailureResumesOnlyUnfinishedSuffix(t *testing.T) {
	service, session, runtime := newBackToVillageRuntime(t)
	if err := service.handleDungeonBackToVillage(session, nil); err != nil {
		t.Fatal(err)
	}

	wantErr := errors.New("direct return op37 write failed")
	connection := &failNthDungeonWriteConn{failAt: 4, err: wantErr}
	session.conn = connection
	err := service.commitPendingDungeonReturnForSceneRequest(session, "test_direct_return_failed_chain")
	if !errors.Is(err, wantErr) {
		t.Fatalf("direct return chain failure=%v want=%v", err, wantErr)
	}
	if got := townPostTransitionPacketSignatures(t, connection.write.Bytes()); len(got) != 3 ||
		got[0] != "mode0" || got[1] != "mode1" || got[2] != "op105" {
		t.Fatalf("direct return prefix=%v want=[mode0 mode1 op105]", got)
	}
	if session.dungeon.runtime != nil ||
		runtime.Session.Snapshot().Run.Status != worldmap.DungeonRunAbandoned ||
		!session.confirmedDungeonReturnStatePending ||
		session.townPostTransition.stage != currentTownPostTransitionCreatureGrowthSent {
		t.Fatalf(
			"failed direct return owner=%p run=%s pending=%t stage=%d",
			session.dungeon.runtime,
			runtime.Session.Snapshot().Run.Status,
			session.confirmedDungeonReturnStatePending,
			session.townPostTransition.stage,
		)
	}

	prefixLen := connection.write.Len()
	if err := service.commitPendingDungeonReturnForSceneRequest(session, "test_direct_return_resume_chain"); err != nil {
		t.Fatalf("resume direct return chain: %v", err)
	}
	if got := townPostTransitionPacketSignatures(t, connection.write.Bytes()[prefixLen:]); len(got) != 4 ||
		got[0] != "op37" || got[1] != "op30" || got[2] != "op19" || got[3] != "op120" {
		t.Fatalf("direct return resumed suffix=%v want=[op37 op30 op19 op120]", got)
	}
	if session.confirmedDungeonReturnStatePending ||
		session.townPostTransition.stage != currentTownPostTransitionComplete {
		t.Fatalf(
			"resumed direct return pending=%t stage=%d",
			session.confirmedDungeonReturnStatePending,
			session.townPostTransition.stage,
		)
	}
	completeLen := connection.write.Len()
	if err := service.commitPendingDungeonReturnForSceneRequest(session, "test_direct_return_duplicate_confirmation"); err != nil {
		t.Fatalf("duplicate direct return confirmation: %v", err)
	}
	if connection.write.Len() != completeLen {
		t.Fatalf("duplicate direct return confirmation replayed=%x", connection.write.Bytes()[completeLen:])
	}
}

func TestConfirmedReturnFinishLoadingAckOnlyPropagatesEnsureFailure(t *testing.T) {
	service, session, runtime := newBackToVillageRuntime(t)
	if err := service.handleDungeonBackToVillage(session, nil); err != nil {
		t.Fatal(err)
	}

	initialErr := errors.New("confirmed return initial op37 write failed")
	initial := &failNthDungeonWriteConn{failAt: 4, err: initialErr}
	session.conn = initial
	if err := service.commitPendingDungeonReturnForSceneRequest(
		session,
		"test_op37_ack_only_prepare_incomplete_generation",
	); !errors.Is(err, initialErr) {
		t.Fatalf("prepare incomplete generation error=%v want=%v", err, initialErr)
	}
	if session.dungeon.runtime != nil ||
		runtime.Session.Snapshot().Run.Status != worldmap.DungeonRunAbandoned ||
		!session.returnTownFinishLoadingAckOnly ||
		!session.confirmedDungeonReturnStatePending ||
		session.townPostTransition.stage != currentTownPostTransitionCreatureGrowthSent {
		t.Fatalf(
			"prepared ACK-only return owner=%p run=%s ack_only=%t pending=%t stage=%d",
			session.dungeon.runtime,
			runtime.Session.Snapshot().Run.Status,
			session.returnTownFinishLoadingAckOnly,
			session.confirmedDungeonReturnStatePending,
			session.townPostTransition.stage,
		)
	}

	wantErr := errors.New("confirmed return op37 retry failed")
	connection := &failNthDungeonWriteConn{failAt: 2, err: wantErr}
	session.conn = connection
	err := service.sendFinishLoadingStatus(
		session,
		make([]byte, currentFinishLoadingLegacyRequestBodySize),
	)
	if !errors.Is(err, wantErr) {
		t.Fatalf("ACK-only op37 ensure failure=%v want=%v", err, wantErr)
	}
	status, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if status.Header.Classification != dnfproto.DefaultChannelClassification ||
		status.Header.MsgID != uint16(dnfenum.CmdPacketFinishLoading) ||
		!bytes.Equal(status.Body, []byte{1}) ||
		len(rest) != 0 {
		t.Fatalf("ACK-only op37 status=%+v body=%x trailing=%x", status.Header, status.Body, rest)
	}
	if !session.confirmedDungeonReturnStatePending ||
		session.townPostTransition.stage != currentTownPostTransitionCreatureGrowthSent {
		t.Fatalf(
			"ACK-only op37 swallowed state pending=%t stage=%d",
			session.confirmedDungeonReturnStatePending,
			session.townPostTransition.stage,
		)
	}

	resume := &bufferConn{}
	session.conn = resume
	if err := service.ensureCurrentConfirmedDungeonReturnPlayerState(
		session,
		"test_op37_ack_only_explicit_resume",
	); err != nil {
		t.Fatalf("resume after ACK-only failure: %v", err)
	}
	if got := townPostTransitionPacketSignatures(t, resume.write.Bytes()); len(got) != 4 ||
		got[0] != "op37" || got[1] != "op30" || got[2] != "op19" || got[3] != "op120" {
		t.Fatalf("ACK-only resumed suffix=%v want=[op37 op30 op19 op120]", got)
	}
}

func TestTownReturnOp24WriteFailureRetainsRuntimeAndRetriesOnlyOp24(t *testing.T) {
	service, session, runtime := newBackToVillageRuntime(t)
	transition, err := service.prepareCurrentDungeonTownTransition(context.Background(), session.selectedCharacterID)
	if err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("town op24 write failed")
	failingConn := &failNthDungeonWriteConn{failAt: 1, err: wantErr}
	session.conn = failingConn
	session.dungeon.mu.Lock()
	err = service.sendCurrentDungeonReturnToTownLocked(
		session,
		runtime,
		transition,
		uint16(dnfenum.CmdPacketBack2Village),
		"test_actor_state_write_failure",
	)
	session.dungeon.mu.Unlock()
	if !errors.Is(err, wantErr) {
		t.Fatalf("op24 failure=%v want=%v", err, wantErr)
	}
	if failingConn.write.Len() != 0 {
		t.Fatalf("failed op24 partially wrote=%x", failingConn.write.Bytes())
	}
	if session.dungeon.runtime != runtime || !runtime.townReturnPending || runtime.townReturnOp24Sent {
		t.Fatalf("op24 failure state owner=%p runtime=%+v", session.dungeon.runtime, runtime)
	}

	session.dungeon.mu.Lock()
	err = service.sendCurrentDungeonReturnToTownLocked(
		session,
		runtime,
		transition,
		uint16(dnfenum.CmdPacketBack2Village),
		"test_op24_write_retry",
	)
	session.dungeon.mu.Unlock()
	if err != nil {
		t.Fatalf("retry op24: %v", err)
	}
	op24, rest := splitGameServerUpperPacket(t, failingConn.write.Bytes())
	if op24.Header.MsgID != currentSceneTransitionMsgID ||
		!bytes.Equal(op24.Body, wantDungeonTownTransitionBody()) || len(rest) != 0 {
		t.Fatalf("retried op24 header=%+v body=%x rest=%x", op24.Header, op24.Body, rest)
	}
	if !runtime.townReturnOp24Sent || session.dungeon.runtime != runtime {
		t.Fatalf("op24 retry state owner=%p runtime=%+v", session.dungeon.runtime, runtime)
	}
}

func TestHandleDungeonBackToVillageRejectsMalformedOrWrongClass(t *testing.T) {
	tests := []struct {
		name           string
		body           []byte
		classification byte
	}{
		{name: "non-empty body", body: []byte{0}, classification: dnfproto.DefaultChannelClassification},
		{name: "wrong class", classification: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, session, runtime := newBackToVillageRuntime(t)
			request, err := dnfproto.BuildChannelPacket(
				uint16(dnfenum.CmdPacketBack2Village), test.body, 0, test.classification,
			)
			if err != nil {
				t.Fatal(err)
			}
			if err := service.handleGameUpper(session, request); err != nil {
				t.Fatalf("handle rejected op132: %v", err)
			}
			if session.conn.(*bufferConn).write.Len() != 0 {
				t.Fatalf("rejected op132 emitted response = %x", session.conn.(*bufferConn).write.Bytes())
			}
			if session.dungeon.runtime != runtime {
				t.Fatal("rejected op132 changed runtime owner")
			}
			if status := runtime.Session.Snapshot().Run.Status; status != worldmap.DungeonRunActive {
				t.Fatalf("rejected op132 changed run status = %s", status)
			}
		})
	}
}

func TestHandleDungeonBackToVillageRejectsMissingDungeonSelectContextWithoutRuntime(t *testing.T) {
	service, session, _ := newBackToVillageRuntime(t)
	session.dungeon.runtime = nil
	bindDungeonSelectorOriginForTest(t, service, session)
	session.enterSelectDungeonSent = true
	session.enterSelectDungeonAckSent = true
	if err := service.handleDungeonBackToVillage(session, nil); err != nil {
		t.Fatalf("handle op132 without selector context: %v", err)
	}
	if session.conn.(*bufferConn).write.Len() != 0 {
		t.Fatalf("op132 without selector context emitted response = %x", session.conn.(*bufferConn).write.Bytes())
	}
}

func TestHandleDungeonBackToVillageReturnsFromConfirmedDungeonSelectContextWithoutRuntime(t *testing.T) {
	service, session, _ := newBackToVillageRuntime(t)
	session.dungeon.runtime = nil
	bindDungeonSelectorOriginForTest(t, service, session)
	session.enterSelectDungeonSent = true
	session.enterSelectDungeonAckSent = true
	session.enterSelectDungeonContextSent = true
	session.runtimeAfterBlacklistSent = true
	session.runtimeFinishLoadingGateSent = true
	session.fpsFinishLoadingGateSent = true
	session.selectPreviewActorRemoved = true
	session.preDungeonContextPlayerStateSent = true
	session.currentFinishLoadingStateSent = true
	session.currentFinishLoadingCompletionSent = true
	session.postFinishLoadingPlayerStateSent = true

	if err := service.handleDungeonBackToVillage(session, nil); err != nil {
		t.Fatalf("handle selector op132: %v", err)
	}

	response, rest := splitGameServerUpperPacket(t, session.conn.(*bufferConn).write.Bytes())
	if response.Header.MsgID != currentSceneTransitionMsgID || response.Header.Classification != 0 {
		t.Fatalf("selector return response header = %+v", response.Header)
	}
	if want := wantDungeonTownTransitionBody(); !bytes.Equal(response.Body, want) {
		t.Fatalf("selector return response body = %x, want %x", response.Body, want)
	}
	if len(rest) != 0 {
		t.Fatalf("selector return emitted trailing packets = %x", rest)
	}
	if session.dungeon.runtime != nil {
		t.Fatalf("selector return created dungeon runtime = %+v", session.dungeon.runtime)
	}
	if session.enterSelectDungeonSent || session.enterSelectDungeonAckSent || session.enterSelectDungeonContextSent {
		t.Fatal("successful selector return retained selector context")
	}
	if session.townSelectorOriginBound || session.townSelectorOriginSnapshot.CharacterID != 0 {
		t.Fatalf("successful selector return retained origin=%+v bound=%t", session.townSelectorOriginSnapshot, session.townSelectorOriginBound)
	}
	if session.runtimeAfterBlacklistSent || session.runtimeFinishLoadingGateSent || session.fpsFinishLoadingGateSent ||
		session.selectPreviewActorRemoved || session.preDungeonContextPlayerStateSent ||
		session.currentFinishLoadingStateSent || session.currentFinishLoadingCompletionSent ||
		session.postFinishLoadingPlayerStateSent {
		t.Fatal("successful selector return retained town scene loading gates")
	}
}

func TestHandleDungeonBackToVillageSelectorReturnDuplicateIsIdempotent(t *testing.T) {
	service, session, _ := newBackToVillageRuntime(t)
	session.dungeon.runtime = nil
	bindDungeonSelectorOriginForTest(t, service, session)
	session.enterSelectDungeonSent = true
	session.enterSelectDungeonAckSent = true
	session.enterSelectDungeonContextSent = true

	if err := service.handleDungeonBackToVillage(session, nil); err != nil {
		t.Fatalf("first selector op132: %v", err)
	}
	connection := session.conn.(*bufferConn)
	firstLen := connection.write.Len()
	if firstLen == 0 {
		t.Fatal("first selector op132 emitted no typed op24")
	}
	if !session.backToVillageEnterSelectPending {
		t.Fatal("selector op132 did not arm the client scene-finalizer guard")
	}
	if err := service.sendEnterSelectDungeonState(session, "test_selector_return_scene_finalizer", false, true); err != nil {
		t.Fatalf("consume selector-return scene finalizer: %v", err)
	}
	finalizerBytes := connection.write.Bytes()[firstLen:]
	if trailing := splitTownPostTransitionPlayerState(
		t,
		finalizerBytes,
		session,
		session.selectedCharacterID,
		backToVillageSkillProjectionBody(t, service),
		false,
	); len(trailing) != 0 {
		t.Fatalf("selector-return scene finalizer opened selector or emitted trailing packets=%x", trailing)
	}
	if session.backToVillageEnterSelectPending || session.enterSelectDungeonContextSent {
		t.Fatalf("scene finalizer state pending=%t context=%t", session.backToVillageEnterSelectPending, session.enterSelectDungeonContextSent)
	}
	finalizedLen := connection.write.Len()
	if err := service.handleDungeonBackToVillage(session, nil); err != nil {
		t.Fatalf("duplicate selector op132: %v", err)
	}
	if connection.write.Len() != finalizedLen {
		t.Fatalf("duplicate selector op132 emitted another response: finalized_len=%d now=%d", finalizedLen, connection.write.Len())
	}
}

func bindDungeonSelectorOriginForTest(t *testing.T, service *Service, session *gameSession) {
	t.Helper()
	bindDungeonSelectorOriginForTestAt(t, service, session, 7, 3, 474, 234)
}

func bindDungeonSelectorOriginForTestAt(
	t *testing.T,
	service *Service,
	session *gameSession,
	townID byte,
	areaID byte,
	positionX uint16,
	positionY uint16,
) {
	t.Helper()
	session.townMu.Lock()
	setCurrentTownPositionSceneLocked(session, session.selectedCharacterID, townID, areaID)
	session.townPositionSnapshot.PositionX = positionX
	session.townPositionSnapshot.PositionY = positionY
	session.townPositionSnapshot.PositionValid = true
	session.townMu.Unlock()
	service.markCurrentTownSceneReady(session)
	if _, ok := bindCurrentTownSelectorOrigin(session); !ok {
		t.Fatal("failed to bind selector origin")
	}
}

func TestCurrentDungeonTownTransitionRowUsesPersistedSignedPosition(t *testing.T) {
	row, x, y, direction, state, err := currentDungeonTownTransitionRow(99, map[string]int64{
		"pos_x":      -2,
		"pos_y":      -32768,
		"direction":  6,
		"area_state": 0x81,
	})
	if err != nil {
		t.Fatal(err)
	}
	if row.ObjectOrResourceKey != 99 || row.Value1 != 0xfffe || row.Value2 != 0x8000 ||
		row.Value3 != 6 || row.Value4 != 0x81 || x != -2 || y != -32768 || direction != 6 || state != 0x81 {
		t.Fatalf("row=%+v x=%d y=%d direction=%d state=%d", row, x, y, direction, state)
	}
}

func TestCurrentDungeonTownTransitionRowRequiresRealPersistedFields(t *testing.T) {
	tests := []struct {
		name  string
		id    uint16
		stats map[string]int64
	}{
		{name: "missing stats", id: 99},
		{name: "missing x", id: 99, stats: map[string]int64{"pos_y": 1, "direction": 5, "area_state": 3}},
		{name: "missing y", id: 99, stats: map[string]int64{"pos_x": 1, "direction": 5, "area_state": 3}},
		{name: "missing direction", id: 99, stats: map[string]int64{"pos_x": 1, "pos_y": 2, "area_state": 3}},
		{name: "missing state", id: 99, stats: map[string]int64{"pos_x": 1, "pos_y": 2, "direction": 5}},
		{name: "x overflow", id: 99, stats: map[string]int64{"pos_x": 32768, "pos_y": 2, "direction": 5, "area_state": 3}},
		{name: "y underflow", id: 99, stats: map[string]int64{"pos_x": 1, "pos_y": -32769, "direction": 5, "area_state": 3}},
		{name: "direction overflow", id: 99, stats: map[string]int64{"pos_x": 1, "pos_y": 2, "direction": 256, "area_state": 3}},
		{name: "state negative", id: 99, stats: map[string]int64{"pos_x": 1, "pos_y": 2, "direction": 5, "area_state": -1}},
		{name: "zero character", stats: map[string]int64{"pos_x": 1, "pos_y": 2, "direction": 5, "area_state": 3}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, _, _, _, _, err := currentDungeonTownTransitionRow(test.id, test.stats); err == nil {
				t.Fatal("invalid persisted transition fields unexpectedly accepted")
			}
		})
	}
}

func TestResetDungeonEntrySceneGatesForcesPlayerStateRebuild(t *testing.T) {
	session := &gameSession{
		sceneBootstrapTailDeferred:         true,
		sceneBootstrapTailSent:             true,
		runtimeAfterBlacklistSent:          true,
		runtimeFinishLoadingGateSent:       true,
		fpsFinishLoadingGateSent:           true,
		selectedUserInfoRefreshSent:        true,
		selectedUserInfoMode3Sent:          true,
		currentSceneObjectListSent:         true,
		selectedItemListRefreshSent:        true,
		selectedEquipmentUpdateSent:        true,
		selectPreviewObjectStateSent:       true,
		selectPreviewActorRemoved:          true,
		preDungeonContextPlayerStateSent:   true,
		postStartMapPlayerStateSent:        true,
		currentFinishLoadingStateSent:      true,
		currentFinishLoadingCompletionSent: true,
		postFinishLoadingPlayerStateSent:   true,
		initialTownQuestSnapshotsSent:      true,
	}

	resetDungeonEntrySceneGates(session)

	if session.sceneBootstrapTailDeferred || session.sceneBootstrapTailSent ||
		session.runtimeAfterBlacklistSent || session.runtimeFinishLoadingGateSent || session.fpsFinishLoadingGateSent ||
		session.selectedUserInfoRefreshSent || session.selectedUserInfoMode3Sent || session.currentSceneObjectListSent ||
		session.selectedItemListRefreshSent ||
		session.selectedEquipmentUpdateSent || session.selectPreviewActorRemoved ||
		session.preDungeonContextPlayerStateSent || session.postStartMapPlayerStateSent ||
		session.currentFinishLoadingStateSent || session.currentFinishLoadingCompletionSent ||
		session.postFinishLoadingPlayerStateSent ||
		session.initialTownQuestSnapshotsSent {
		t.Fatal("dungeon entry retained a one-shot scene gate")
	}
	if !session.selectPreviewObjectStateSent {
		t.Fatal("dungeon entry cleared the preview owner needed by op9 removal")
	}
}

func newBackToVillageRuntime(t *testing.T) (*Service, *gameSession, *runtimeDungeonState) {
	t.Helper()
	return newBackToVillageRuntimeAtOrigin(t, 474, 234)
}

func newBackToVillageRuntimeAtOrigin(
	t *testing.T,
	positionX uint16,
	positionY uint16,
) (*Service, *gameSession, *runtimeDungeonState) {
	t.Helper()
	table, resolver, monsters := loadBridgeDungeonStaticData(t, bridgeDungeonPVF(false))
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(context.Background(), dnfrepo.CharacterRecord{
		CharacterID: "99",
		AccountID:   "account-1",
		Level:       20,
		Stats: map[string]int64{
			"fatigue":    100,
			"town_id":    7,
			"area_id":    3,
			"pos_x":      474,
			"pos_y":      234,
			"direction":  5,
			"area_state": 3,
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Account.Save(context.Background(), dnfrepo.AccountRecord{
		AccountID: "account-1",
		Metadata:  map[string]string{currentRentalPointMetadataKey: "0"},
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
		premiumCatalog: &currentPremiumCatalog{
			contractsByItem: make(map[int64]currentPremiumContractInfo),
			devilSlots:      make(map[uint32]currentPremiumDevilSlotInfo),
			crystalCubeIDs:  [6]int64{3033, 3034, 3035, 3036, 3037, 3262},
		},
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	enableTownMoveSkillProjection(t, service, repositories, "99")
	channel := channelcatalog.Channel{ServerID: 1, ID: 19, Type: 1, Name: "ch.19", Port: 10019}
	session := &gameSession{
		conn:                &bufferConn{},
		connID:              "back-to-village-test",
		channel:             channel,
		residentChannel:     channel,
		selectedCharacterID: 99,
	}
	bindDungeonSelectorOriginForTestAt(t, service, session, 7, 3, positionX, positionY)
	runtime, _, err := service.prepareDungeonRuntime(
		context.Background(), session, dungeoncmd.SelectDungeonRequest{DungeonID: 700, Difficulty: 1},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := freezeCurrentDungeonTownReturnOrigin(session, runtime); err != nil {
		t.Fatal(err)
	}
	if err := service.commitDungeonRuntime(session, runtime); err != nil {
		t.Fatal(err)
	}
	return service, session, runtime
}

func backToVillageSkillProjectionBody(t *testing.T, service *Service) []byte {
	t.Helper()
	repositories, ok := service.repositoryGroup()
	if !ok {
		t.Fatal("back-to-village skill repository unavailable")
	}
	return townMoveSkillProjectionBody(t, repositories, "99")
}

func TestCommitOrdinaryDungeonRuntimeRejectsMissingFrozenTownOrigin(t *testing.T) {
	service, session, runtime := newBackToVillageRuntime(t)
	session.dungeon.mu.Lock()
	session.dungeon.runtime = nil
	session.dungeon.mu.Unlock()
	runtime.townReturnOrigin = currentTownPositionSnapshot{}

	err := service.commitDungeonRuntime(session, runtime)
	if !errors.Is(err, errCurrentDungeonTownReturnOriginUnavailable) {
		t.Fatalf("commit error=%v, want missing frozen town origin", err)
	}
	session.dungeon.mu.Lock()
	defer session.dungeon.mu.Unlock()
	if session.dungeon.runtime != nil {
		t.Fatalf("missing-origin runtime became active: %+v", session.dungeon.runtime)
	}
}

func wantDungeonTownTransitionBody() []byte {
	return []byte{
		7, 3, 1, 0,
		99, 0,
		0xda, 0x01,
		0xea, 0x00,
		5, 3,
	}
}

func wantTutorialCompletionTownTransitionBody() []byte {
	return []byte{
		newCharacterInitialTownID, newCharacterInitialAreaID, 1, 0,
		99, 0,
		byte(newCharacterInitialPosX & 0xff), byte(newCharacterInitialPosX >> 8),
		byte(newCharacterInitialPosY & 0xff), byte(newCharacterInitialPosY >> 8),
		newCharacterInitialDirection, newCharacterInitialAreaState,
	}
}

func containsClass0Op2ModeForTest(t *testing.T, stream []byte, mode byte) bool {
	t.Helper()
	for len(stream) > 0 {
		packet, rest := splitCurrentGameServerUpperPacketAuto(t, stream)
		if packet.Header.Classification == 0 &&
			packet.Header.MsgID == uint16(dnfenum.CmdPacketSetUDPIPPort) &&
			len(packet.Body) > 0 &&
			packet.Body[0] == mode {
			return true
		}
		stream = rest
	}
	return false
}

func containsInitialTownTransitionForTest(t *testing.T, stream []byte) bool {
	t.Helper()
	for len(stream) > 0 {
		packet, rest := splitCurrentGameServerUpperPacketAuto(t, stream)
		if packetLooksLikeInitialTownTransition(packet) {
			return true
		}
		stream = rest
	}
	return false
}
