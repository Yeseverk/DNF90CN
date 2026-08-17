package dnfbridge

import (
	"context"
	"testing"

	"longheng.io/server/internal/modules/dnf/channelcatalog"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestLegacyType1345DoesNotConsumePreparedSkillWithoutArmedPostOp24PlayerState(t *testing.T) {
	service, session, connection, _ := newLegacyTownSceneReadySkillTest(t)
	armedStage := session.townPostTransition.stage
	session.initialTownRouteStage = currentInitialTownRoutePlayerStatePrepared

	if err := service.handleLegacyGamePacket(session, buildLegacyTownSceneReadyAckPacket()); err != nil {
		t.Fatal(err)
	}
	if connection.write.Len() != 0 {
		t.Fatalf("pre-op24 type1345 wrote %d bytes", connection.write.Len())
	}
	requireInitialTownSkillPrepared(t, session)
	if session.townPostTransition.stage != armedStage {
		t.Fatalf(
			"pre-op24 type1345 advanced post-transition stage=%d want=%d",
			session.townPostTransition.stage,
			armedStage,
		)
	}

	session.initialTownRouteStage = currentInitialTownRoutePlayerStateSent
	if err := service.handleLegacyGamePacket(session, buildLegacyTownSceneReadyAckPacket()); err != nil {
		t.Fatal(err)
	}
	requireNoCustomSeriaLuckGauge(
		t,
		splitAllCurrentUpperPackets(t, connection.write.Bytes()),
	)
	requireInitialTownSkillPrepared(t, session)
	if session.currentFinishLoadingStateSent ||
		session.currentFinishLoadingCompletionSent ||
		session.postFinishLoadingPlayerStateSent ||
		session.townPostTransition.stage != armedStage {
		t.Fatalf(
			"type1345 consumed actor/HUD generation state=%t completion=%t placement=%t stage=%d want stage=%d",
			session.currentFinishLoadingStateSent,
			session.currentFinishLoadingCompletionSent,
			session.postFinishLoadingPlayerStateSent,
			session.townPostTransition.stage,
			armedStage,
		)
	}

	firstWriteLen := connection.write.Len()
	clientOp2, err := dnfproto.BuildChannelPacket(
		uint16(dnfenum.UpperMsgCharacterRoster),
		make([]byte, currentChannelReconnectProbeSize),
		1,
		dnfproto.DefaultChannelClassification,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.handleGameUpper(session, clientOp2); err != nil {
		t.Fatal(err)
	}
	if connection.write.Len() != firstWriteLen {
		t.Fatalf("client op2 replayed initial state: stream grew from %d to %d", firstWriteLen, connection.write.Len())
	}
	requireInitialTownSkillPrepared(t, session)
}

func TestLegacyType1345RepeatedReportSendsPrivateGaugeOnlyOnce(t *testing.T) {
	service, session, connection, _ := newLegacyTownSceneReadySkillTest(t)

	if err := service.handleLegacyGamePacket(session, buildLegacyTownSceneReadyAckPacket()); err != nil {
		t.Fatal(err)
	}
	firstWriteLen := connection.write.Len()
	if err := service.handleLegacyGamePacket(session, buildLegacyTownSceneReadyAckPacket()); err != nil {
		t.Fatal(err)
	}
	if connection.write.Len() != firstWriteLen {
		t.Fatalf("repeated type1345 grew stream from %d to %d", firstWriteLen, connection.write.Len())
	}
	requireNoCustomSeriaLuckGauge(
		t,
		splitAllCurrentUpperPackets(t, connection.write.Bytes()),
	)
	requireInitialTownSkillPrepared(t, session)
}

func TestLegacyType1345WithoutArmedPostTransitionLeavesPreparedSkillUntouched(t *testing.T) {
	service, session, connection, _ := newLegacyTownSceneReadySkillTest(t)
	resetCurrentTownPostTransitionPlayerState(session)

	if err := service.handleLegacyGamePacket(session, buildLegacyTownSceneReadyAckPacket()); err != nil {
		t.Fatal(err)
	}
	requireNoCustomSeriaLuckGauge(
		t,
		splitAllCurrentUpperPackets(t, connection.write.Bytes()),
	)
	requireInitialTownSkillPrepared(t, session)
}

func TestLegacyType1345RejectsUnprovedBodyWithoutChangingSkillState(t *testing.T) {
	service, session, connection, _ := newLegacyTownSceneReadySkillTest(t)
	raw := buildLegacyGamePacketForBridgeTest(
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.CmdPacketAgitWarInfo),
		0,
		[]byte{1, 0, 0, 0},
	)

	if err := service.handleLegacyGamePacket(session, raw); err != nil {
		t.Fatal(err)
	}
	if connection.write.Len() != 0 {
		t.Fatalf("unproved type1345 body wrote %d bytes", connection.write.Len())
	}
	requireInitialTownSkillPrepared(t, session)
	if session.townPostTransition.stage != currentTownPostTransitionPending {
		t.Fatalf("unproved type1345 advanced post-transition stage=%d", session.townPostTransition.stage)
	}
}

func newLegacyTownSceneReadySkillTest(t *testing.T) (*Service, *gameSession, *bufferConn, []byte) {
	t.Helper()
	connection := &bufferConn{}
	channel := channelcatalog.Channel{ServerID: 1, ID: 12, Type: 1, Name: "ch.12", Port: 10012}
	skillBody := []byte{1, 0, 0, 0, 0x08}
	repositories := dnfrepomemory.NewMemoryGroup()
	ctx := context.Background()
	if err := repositories.Account.Save(ctx, dnfrepo.AccountRecord{AccountID: "dnf:1"}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "29",
		AccountID:   "dnf:1",
		Job:         "11",
		Level:       90,
	}); err != nil {
		t.Fatal(err)
	}
	session := &gameSession{
		conn:                            connection,
		connID:                          "legacy-type1345-test",
		accountID:                       "dnf:1",
		channel:                         channel,
		residentChannel:                 channel,
		selectedCharacterID:             29,
		initialTownRouteCharacterID:     29,
		initialTownRouteStage:           currentInitialTownRoutePlayerStateSent,
		sceneBootstrapTailDeferred:      false,
		sceneBootstrapTailSent:          true,
		townActorOwnerChannel:           12,
		connectionTownActorOwnerChannel: 12,
		initialTownSkillInfoPrepared:    true,
		initialTownSkillInfo: currentSceneSkillInfoProjection{
			body:        append([]byte(nil), skillBody...),
			characterID: "29",
			job:         11,
		},
	}
	service := &Service{
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repositories, true
		},
	}
	service.armCurrentTownPostTransitionPlayerState(session, "test_after_typed_op24")
	return service, session, connection, skillBody
}

func requireInitialTownSkillPrepared(t *testing.T, session *gameSession) {
	t.Helper()
	if !session.initialTownSkillInfoPrepared ||
		session.initialTownSkillInfoSent ||
		len(session.initialTownSkillInfo.body) == 0 {
		t.Fatalf(
			"initial skill state prepared=%t sent=%t retained=%d, want true/false/nonzero",
			session.initialTownSkillInfoPrepared,
			session.initialTownSkillInfoSent,
			len(session.initialTownSkillInfo.body),
		)
	}
}

func requireInitialTownSkillAlreadyInstalled(t *testing.T, session *gameSession) {
	t.Helper()
	if session.initialTownSkillInfoPrepared ||
		!session.initialTownSkillInfoSent ||
		len(session.initialTownSkillInfo.body) != 0 {
		t.Fatalf(
			"initial skill state prepared=%t sent=%t retained=%d, want false/true/0",
			session.initialTownSkillInfoPrepared,
			session.initialTownSkillInfoSent,
			len(session.initialTownSkillInfo.body),
		)
	}
}

func splitAllCurrentUpperPackets(t *testing.T, stream []byte) []dnfproto.ChannelPacket {
	t.Helper()
	packets := make([]dnfproto.ChannelPacket, 0, 8)
	for len(stream) > 0 {
		packet, rest := splitCurrentGameServerUpperPacketAuto(t, stream)
		packets = append(packets, packet)
		stream = rest
	}
	return packets
}

func requireNoCustomSeriaLuckGauge(t *testing.T, packets []dnfproto.ChannelPacket) {
	t.Helper()
	for _, packet := range packets {
		if packet.Header.MsgID == 413 && len(packet.Body) == 2 && packet.Body[0] == 0 {
			t.Fatalf("custom Seria-luck gauge packet still sent: %+v body=%x", packet.Header, packet.Body)
		}
	}
}

func buildLegacyTownSceneReadyAckPacket() []byte {
	return buildLegacyGamePacketForBridgeTest(
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.CmdPacketAgitWarInfo),
		0,
		[]byte{2, 0, 0, 0},
	)
}
