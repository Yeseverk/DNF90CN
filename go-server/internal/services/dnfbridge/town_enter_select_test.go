package dnfbridge

import (
	"bytes"
	"context"
	"testing"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

func TestTownEnterSelectOp15SendsCurrentOp27AfterAckAndFatigue(t *testing.T) {
	service, session, _ := newTownMoveTest(t)
	if err := service.handleTownSetUserArea(session, buildTownMoveRequest(38, 0, 900, 250, 5)); err != nil {
		t.Fatal(err)
	}
	if err := service.handleTownSetUserPosition(session, buildTownPositionRequest(884, 248, 6, 100)); err != nil {
		t.Fatal(err)
	}
	connection := session.conn.(*bufferConn)
	connection.write.Reset()

	if err := service.sendEnterSelectDungeonState(session, "test_town_op15", false, true); err != nil {
		t.Fatal(err)
	}
	ack, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if ack.Header.Classification != dnfproto.DefaultChannelClassification || ack.Header.MsgID != uint16(dnfenum.CmdPacketEnterSelectDungeon) || !bytes.Equal(ack.Body, []byte{1, 1, 0}) {
		t.Fatalf("op15 ack header=%+v body=%x rest=%x", ack.Header, ack.Body, rest)
	}
	fatigue, rest := splitGameServerUpperPacket(t, rest)
	if fatigue.Header.Classification != 0 || fatigue.Header.MsgID != currentFatigueMsgID || len(fatigue.Body) != 10 {
		t.Fatalf("fatigue header=%+v body=%x rest=%x", fatigue.Header, fatigue.Body, rest)
	}
	contextPacket, trailing := splitGameServerUpperPacket(t, rest)
	if contextPacket.Header.Classification != 0 || contextPacket.Header.MsgID != currentDungeonContextMsgID || len(contextPacket.Body) != 37 || len(trailing) != 0 {
		t.Fatalf("op27 header=%+v body=%x trailing=%x", contextPacket.Header, contextPacket.Body, trailing)
	}
	if !session.enterSelectDungeonContextSent {
		t.Fatal("town enter-select op27 was not recorded")
	}
	if !session.townSelectorOriginBound ||
		session.townSelectorOriginSnapshot.CharacterID != 29 ||
		session.townSelectorOriginSnapshot.TownID != 38 ||
		session.townSelectorOriginSnapshot.AreaID != 0 ||
		session.townSelectorOriginSnapshot.PositionX != 884 ||
		session.townSelectorOriginSnapshot.PositionY != 248 ||
		!session.townSelectorOriginSnapshot.PositionValid {
		t.Fatalf("selector origin=%+v bound=%t", session.townSelectorOriginSnapshot, session.townSelectorOriginBound)
	}
}

func TestPartyMemberTownEnterSelectSynchronizesCompleteOtherMemberStateWithCurrentSelectorAck(t *testing.T) {
	service, leader, repositories := newTownMoveTest(t)
	service.onlinePlayers = newOnlinePlayerManager()
	if err := repositories.Character.Save(context.Background(), dnfrepo.CharacterRecord{
		CharacterID: "17",
		AccountID:   "account-1",
		Name:        "follower",
		Level:       20,
		Stats: map[string]int64{
			"town_id": 38, "area_id": 0, "pos_x": 870, "pos_y": 248,
			"direction": 6, "area_state": 3, "fatigue": 20,
		},
	}); err != nil {
		t.Fatal(err)
	}
	followerConn := &bufferConn{}
	follower := &gameSession{
		conn:                            followerConn,
		channel:                         leader.channel,
		residentChannel:                 leader.residentChannel,
		connectionTownActorOwnerChannel: leader.connectionTownActorOwnerChannel,
		townActorOwnerChannel:           leader.townActorOwnerChannel,
		selectedCharacterID:             17,
		townSceneReadyCharacterID:       17,
	}
	service.bindGameSessionCharacter(follower, 17)

	if err := service.handleTownSetUserArea(leader, buildTownMoveRequest(38, 0, 900, 250, 5)); err != nil {
		t.Fatal(err)
	}
	if err := service.handleTownSetUserPosition(leader, buildTownPositionRequest(884, 248, 6, 100)); err != nil {
		t.Fatal(err)
	}
	if err := service.handleTownSetUserArea(follower, buildTownMoveRequest(38, 0, 870, 248, 6)); err != nil {
		t.Fatal(err)
	}
	if err := service.handleTownSetUserPosition(follower, buildTownPositionRequest(870, 248, 6, 100)); err != nil {
		t.Fatal(err)
	}
	partyState := alignedcmd.PartyState{
		PartyID: 29,
		UserID:  29,
		Members: []alignedcmd.PartyMemberState{
			{UserID: 29, UserState: 1, HPPercent: 100, MPPercent: 100},
			{UserID: 17, UserState: 1, HPPercent: 100, MPPercent: 100},
		},
	}
	storeRuntimePartyState(leader, partyState)
	storeRuntimePartyState(follower, partyState)
	if !service.onlinePlayers.PeerInSameArea(29, 17) {
		t.Fatalf("party fixture members are not in the same area: leader_session=%p follower_session=%p",
			service.onlinePlayers.SessionForCharacter(29),
			service.onlinePlayers.SessionForCharacter(17))
	}
	if onlineFollower, ok := service.onlineGameSession(17); !ok || onlineFollower != follower {
		t.Fatalf("party fixture follower is not online: ok=%t session=%p want=%p", ok, onlineFollower, follower)
	}
	if ready, reason := service.currentTownEnterSelectReady(follower); !ready {
		t.Fatalf("party fixture follower selector not ready: %s snapshot=%+v", reason, follower.townPositionSnapshot)
	}
	leader.conn.(*bufferConn).write.Reset()
	followerConn.write.Reset()

	// Character 17 is an ordinary member. Its selector request must bring the
	// leader into the same selector scene without changing the party leader.
	if err := service.sendEnterSelectDungeonState(follower, "test_party_member_op15", false, true); err != nil {
		t.Fatal(err)
	}
	stream := leader.conn.(*bufferConn).write.Bytes()
	area, stream := splitCurrentGameServerUpperPacketAuto(t, stream)
	if area.Header.Classification != 0 || area.Header.MsgID != currentTownUserAreaNotificationMsgID ||
		len(area.Body) != 10 || area.Body[3] != 0xff {
		t.Fatalf("follower selector area header=%+v body=%x rest=%x", area.Header, area.Body, stream)
	}
	mode0, stream := splitGameServerUpperPacket(t, stream)
	if mode0.Header.Classification != 0 || mode0.Header.MsgID != uint16(dnfenum.CmdPacketSetUDPIPPort) ||
		len(mode0.Body) == 0 || mode0.Body[0] != 0 {
		t.Fatalf("follower selector mode0 header=%+v body=%x rest=%x", mode0.Header, mode0.Body, stream)
	}
	mode1, stream := splitGameServerUpperPacket(t, stream)
	if mode1.Header.Classification != 0 || mode1.Header.MsgID != uint16(dnfenum.CmdPacketSetUDPIPPort) ||
		len(mode1.Body) == 0 || mode1.Body[0] != 1 {
		t.Fatalf("follower selector mode1 header=%+v body=%x rest=%x", mode1.Header, mode1.Body, stream)
	}
	userState, stream := splitGameServerUpperPacket(t, stream)
	if userState.Header.Classification != 0 || userState.Header.MsgID != uint16(dnfenum.CmdPacketExit) ||
		len(userState.Body) != 4 {
		t.Fatalf("follower selector user-state header=%+v body=%x rest=%x", userState.Header, userState.Body, stream)
	}
	stream = assertRuntimePartySceneRefresh(t, stream, 2)
	selectorAck, stream := splitGameServerUpperPacket(t, stream)
	if selectorAck.Header.Classification != dnfproto.DefaultChannelClassification ||
		selectorAck.Header.MsgID != uint16(dnfenum.CmdPacketEnterSelectDungeon) ||
		!bytes.Equal(selectorAck.Body, []byte{1, 1, 0}) {
		t.Fatalf("follower selector ack header=%+v body=%x rest=%x", selectorAck.Header, selectorAck.Body, stream)
	}
	fatigue, stream := splitGameServerUpperPacket(t, stream)
	if fatigue.Header.Classification != 0 || fatigue.Header.MsgID != currentFatigueMsgID {
		t.Fatalf("follower selector fatigue header=%+v body=%x rest=%x", fatigue.Header, fatigue.Body, stream)
	}
	contextPacket, trailing := splitGameServerUpperPacket(t, stream)
	if contextPacket.Header.Classification != 0 || contextPacket.Header.MsgID != currentDungeonContextMsgID || len(trailing) != 0 {
		t.Fatalf("follower selector context header=%+v body=%x trailing=%x", contextPacket.Header, contextPacket.Body, trailing)
	}
	if !leader.enterSelectDungeonSent || !leader.enterSelectDungeonContextSent ||
		!leader.preDungeonContextPlayerStateSent || !leader.enterSelectDungeonAckSent {
		t.Fatalf("passive leader selector flags sent=%t context=%t request_ack=%t",
			leader.enterSelectDungeonSent,
			leader.enterSelectDungeonContextSent,
			leader.enterSelectDungeonAckSent)
	}
}

func TestTownTransportSceneFinalizerOp15DoesNotOpenDungeonSelector(t *testing.T) {
	service, session, _ := newTownMoveTest(t)
	session.townTransportEnterSelectPending = true
	connection := session.conn.(*bufferConn)
	connection.write.Reset()

	if err := service.sendEnterSelectDungeonState(session, "test_town_transport_scene_finalizer", false, true); err != nil {
		t.Fatal(err)
	}
	if connection.write.Len() != 0 || session.townTransportEnterSelectPending ||
		session.enterSelectDungeonSent || session.enterSelectDungeonContextSent {
		t.Fatalf("town transport finalizer wrote=%x pending=%t selector_sent=%t context_sent=%t",
			connection.write.Bytes(), session.townTransportEnterSelectPending,
			session.enterSelectDungeonSent, session.enterSelectDungeonContextSent)
	}
}

func TestTownEnterSelectOp15DoesNotSendOp27BeforeTownReady(t *testing.T) {
	service, session, _ := newTownMoveTest(t)
	if err := service.sendEnterSelectDungeonState(session, "test_unready_op15", false, true); err != nil {
		t.Fatal(err)
	}
	_, rest := splitGameServerUpperPacket(t, session.conn.(*bufferConn).write.Bytes())
	_, trailing := splitGameServerUpperPacket(t, rest)
	if len(trailing) != 0 || session.enterSelectDungeonContextSent {
		t.Fatalf("unready town op15 emitted trailing=%x context_sent=%t", trailing, session.enterSelectDungeonContextSent)
	}
}

func TestTownEnterSelectOp15DoesNotSendOp27WithoutTownLocationOwner(t *testing.T) {
	service, session, _ := newTownMoveTest(t)
	service.markCurrentTownSceneReady(session)
	if err := service.sendEnterSelectDungeonState(session, "test_missing_location_op15", false, true); err != nil {
		t.Fatal(err)
	}
	_, rest := splitGameServerUpperPacket(t, session.conn.(*bufferConn).write.Bytes())
	_, trailing := splitGameServerUpperPacket(t, rest)
	if len(trailing) != 0 || session.enterSelectDungeonContextSent || session.townSelectorOriginBound {
		t.Fatalf("missing-location op15 emitted trailing=%x context_sent=%t origin_bound=%t", trailing, session.enterSelectDungeonContextSent, session.townSelectorOriginBound)
	}
}

func TestTownEnterSelectOp15DoesNotSendOp27WithoutCurrentOp35Position(t *testing.T) {
	service, session, _ := newTownMoveTest(t)
	if err := service.handleTownSetUserArea(session, buildTownMoveRequest(38, 0, 900, 250, 5)); err != nil {
		t.Fatal(err)
	}
	connection := session.conn.(*bufferConn)
	connection.write.Reset()

	if err := service.sendEnterSelectDungeonState(session, "test_missing_position_op15", false, true); err != nil {
		t.Fatal(err)
	}
	_, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	_, trailing := splitGameServerUpperPacket(t, rest)
	if len(trailing) != 0 || session.enterSelectDungeonContextSent || session.townSelectorOriginBound {
		t.Fatalf(
			"missing-position op15 emitted trailing=%x context_sent=%t origin_bound=%t",
			trailing,
			session.enterSelectDungeonContextSent,
			session.townSelectorOriginBound,
		)
	}
}

func TestTownEnterSelectOp15DoesNotSendOp27WithActiveDungeonRuntime(t *testing.T) {
	service, session, _ := newTownMoveTest(t)
	service.markCurrentTownSceneReady(session)
	session.dungeon.runtime = &runtimeDungeonState{}
	if err := service.sendEnterSelectDungeonState(session, "test_active_runtime_op15", false, true); err != nil {
		t.Fatal(err)
	}
	_, rest := splitGameServerUpperPacket(t, session.conn.(*bufferConn).write.Bytes())
	_, trailing := splitGameServerUpperPacket(t, rest)
	if len(trailing) != 0 || session.enterSelectDungeonContextSent {
		t.Fatalf("active-runtime op15 emitted trailing=%x context_sent=%t", trailing, session.enterSelectDungeonContextSent)
	}
}

func TestTownEnterSelectOp15AfterBackToVillageIsSceneFinalizerOnly(t *testing.T) {
	service, session, runtime := newBackToVillageRuntime(t)
	if err := service.handleDungeonBackToVillage(session, nil); err != nil {
		t.Fatal(err)
	}
	if !runtime.townReturnPending || !runtime.townReturnOp24Sent {
		t.Fatalf("return was not left pending: %+v", runtime)
	}
	connection := session.conn.(*bufferConn)
	connection.write.Reset()

	if err := service.sendEnterSelectDungeonState(session, "test_op15_after_pending_town_return", false, true); err != nil {
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
		t.Fatalf("post-return scene-finalizer op15 opened selector or emitted trailing packets=%x", trailing)
	}
	if session.dungeon.runtime != nil ||
		runtime.Session.Snapshot().Run.Status != worldmap.DungeonRunAbandoned ||
		session.enterSelectDungeonContextSent ||
		session.townSelectorOriginBound ||
		session.backToVillageEnterSelectPending {
		t.Fatalf("post-return runtime=%p run=%+v context=%t origin_bound=%t pending=%t",
			session.dungeon.runtime,
			runtime.Session.Snapshot().Run,
			session.enterSelectDungeonContextSent,
			session.townSelectorOriginBound,
			session.backToVillageEnterSelectPending)
	}
	if snapshot := session.townPositionSnapshot; snapshot.CharacterID != session.selectedCharacterID ||
		snapshot.TownID != 7 ||
		snapshot.AreaID != 3 ||
		snapshot.PositionX != 474 ||
		snapshot.PositionY != 234 ||
		!snapshot.PositionValid {
		t.Fatalf("post-return town snapshot=%+v", snapshot)
	}
}

func TestTownEnterSelectAfterBackToVillageFinalizerCanOpenSelectorOnNextIntent(t *testing.T) {
	service, session, runtime := newBackToVillageRuntime(t)
	if err := service.handleDungeonBackToVillage(session, nil); err != nil {
		t.Fatalf("start pending back-to-village return: %v", err)
	}
	if session.dungeon.runtime != runtime || !runtime.townReturnPending || !runtime.townReturnOp24Sent {
		t.Fatalf("fixture did not retain pending return runtime owner=%p state=%+v", session.dungeon.runtime, runtime)
	}
	connection := session.conn.(*bufferConn)
	connection.write.Reset()

	if err := service.sendEnterSelectDungeonState(session, "test_op15_after_back_to_village", false, true); err != nil {
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
		t.Fatalf("scene-finalizer op15 opened selector or emitted trailing packets=%x", trailing)
	}
	if session.dungeon.runtime != nil || session.enterSelectDungeonContextSent || session.backToVillageEnterSelectPending {
		t.Fatalf("scene-finalizer runtime=%p context=%t pending=%t", session.dungeon.runtime, session.enterSelectDungeonContextSent, session.backToVillageEnterSelectPending)
	}
	finalizerLen := connection.write.Len()

	if err := service.sendEnterSelectDungeonState(session, "test_op15_after_back_to_village_player_intent", false, true); err != nil {
		t.Fatal(err)
	}

	ack, rest := splitGameServerUpperPacket(t, connection.write.Bytes()[finalizerLen:])
	if ack.Header.Classification != dnfproto.DefaultChannelClassification ||
		ack.Header.MsgID != uint16(dnfenum.CmdPacketEnterSelectDungeon) ||
		!bytes.Equal(ack.Body, []byte{1, 1, 0}) {
		t.Fatalf("op15 ack header=%+v body=%x rest=%x", ack.Header, ack.Body, rest)
	}
	fatigue, rest := splitGameServerUpperPacket(t, rest)
	if fatigue.Header.Classification != 0 || fatigue.Header.MsgID != currentFatigueMsgID {
		t.Fatalf("op15 fatigue header=%+v body=%x rest=%x", fatigue.Header, fatigue.Body, rest)
	}
	contextPacket, trailing := splitGameServerUpperPacket(t, rest)
	if contextPacket.Header.Classification != 0 || contextPacket.Header.MsgID != currentDungeonContextMsgID ||
		len(contextPacket.Body) != 37 || len(trailing) != 0 {
		t.Fatalf("op15 context header=%+v body=%x trailing=%x", contextPacket.Header, contextPacket.Body, trailing)
	}
	if session.dungeon.runtime != nil || !session.enterSelectDungeonContextSent {
		t.Fatalf("player-intent selector did not open runtime=%p context=%t", session.dungeon.runtime, session.enterSelectDungeonContextSent)
	}
	if status := runtime.Session.Snapshot().Run.Status; status != worldmap.DungeonRunAbandoned {
		t.Fatalf("pending active return status=%s want abandoned", status)
	}
}

func TestCompletedSettlementOp15RetiresRuntimeAndOpensOtherDungeonSelector(t *testing.T) {
	service, runtime, session, connection, _ := prepareCompletedSettlementRuntime(t)
	connection.write.Reset()
	plan := currentDungeonSettlementPacketPlanForTest(t, runtime, 0)
	runtime.settlementResultPlan = &plan
	if err := service.handleDungeonSetPlayResult(session, make([]byte, currentDungeonPlayResultBaseSize)); err != nil {
		t.Fatal(err)
	}
	if err := service.handleDungeonCharacterStatistic(session, make([]byte, currentDungeonCharacterStatisticBodySize)); err != nil {
		t.Fatal(err)
	}
	connection.write.Reset()
	if err := service.sendEnterSelectDungeonState(session, "test_completed_other_dungeon", false, true); err != nil {
		t.Fatal(err)
	}

	ack, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if ack.Header.Classification != dnfproto.DefaultChannelClassification ||
		ack.Header.MsgID != uint16(dnfenum.CmdPacketEnterSelectDungeon) ||
		!bytes.Equal(ack.Body, []byte{1, 1, 0}) {
		t.Fatalf("completed op15 ack=%+v body=%x rest=%x", ack.Header, ack.Body, rest)
	}
	fatigue, rest := splitGameServerUpperPacket(t, rest)
	if fatigue.Header.Classification != 0 || fatigue.Header.MsgID != currentFatigueMsgID {
		t.Fatalf("completed op15 fatigue=%+v body=%x rest=%x", fatigue.Header, fatigue.Body, rest)
	}
	contextPacket, trailing := splitGameServerUpperPacket(t, rest)
	if contextPacket.Header.Classification != 0 || contextPacket.Header.MsgID != currentDungeonContextMsgID ||
		len(contextPacket.Body) != 37 || len(trailing) != 0 {
		t.Fatalf("completed op15 context=%+v body=%x trailing=%x", contextPacket.Header, contextPacket.Body, trailing)
	}
	if session.dungeon.runtime != nil || !session.enterSelectDungeonContextSent ||
		session.townSceneReadyCharacterID != session.selectedCharacterID ||
		session.townPositionSnapshot.CharacterID != session.selectedCharacterID ||
		!session.townPositionSnapshot.PositionValid ||
		runtime.Session.Snapshot().Run.Status != worldmap.DungeonRunCompleted {
		t.Fatalf("completed op15 runtime=%p context=%t ready=%d position=%+v run=%+v",
			session.dungeon.runtime,
			session.enterSelectDungeonContextSent,
			session.townSceneReadyCharacterID,
			session.townPositionSnapshot,
			runtime.Session.Snapshot().Run)
	}
}
