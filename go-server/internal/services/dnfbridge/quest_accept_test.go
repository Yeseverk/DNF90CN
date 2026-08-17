package dnfbridge

import (
	"bytes"
	"context"
	"testing"

	"longheng.io/server/internal/modules/dnf/channelcatalog"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	dnfquest "longheng.io/server/internal/modules/dnf/quest"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestCurrentAcceptQuestPersistsBeforeExactCurrentEXEAck(t *testing.T) {
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(context.Background(), dnfrepo.CharacterRecord{
		CharacterID: "17",
		AccountID:   defaultAccountPrefix + "1",
		Job:         "2",
		Level:       1,
		Stats:       map[string]int64{"grow_type": 0},
	}); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := &Service{
		options:            options{gameUpperBodyCodec: gameUpperBodyCodecPlain},
		questCatalog:       buildAcceptQuestTestCatalog(t, false),
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	session := &gameSession{
		conn:                        connection,
		connID:                      "accept-quest-test",
		channel:                     channelcatalog.Channel{ID: 38},
		selectedCharacterID:         17,
		initialTownRouteCharacterID: 17,
		initialTownRouteStage:       currentInitialTownRoutePlayerStateSent,
	}
	staleTriggerKey := newCurrentSetQuestTriggerReplayKey(17, dnfquest.SetTriggerRequest{
		QuestID:     3145,
		TriggerType: 0x10,
	})
	session.suppressCurrentSetQuestTriggerReplay(staleTriggerKey)
	request, err := dnfproto.BuildChannelPacket(uint16(dnfenum.CmdPacketAcceptQuest), []byte{0x1f, 0x00, 0x49, 0x0c}, 9, dnfproto.DefaultChannelClassification)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.handleGameUpper(session, request); err != nil {
		t.Fatal(err)
	}
	stored, ok, err := repositories.Quest.Load(context.Background(), "17")
	if err != nil || !ok {
		t.Fatalf("load persisted quest ok=%t err=%v", ok, err)
	}
	if state := stored.States[3145]; state.Status != "active" || state.ProgressValue != 1 {
		t.Fatalf("persisted quest state = %+v", state)
	}
	if session.currentSetQuestTriggerReplaySuppressed(staleTriggerKey) {
		t.Fatal("newly activated quest retained the preceding generation's terminal op33 replay key")
	}
	packet, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if packet.Header.Classification != dnfproto.DefaultChannelClassification || packet.Header.MsgID != uint16(dnfenum.CmdPacketAcceptQuest) {
		t.Fatalf("ack header = %+v", packet.Header)
	}
	wantBody := []byte{0x01, 0x49, 0x0c, 0x01, 0x00, 0x00, 0x00, 0x00}
	if !bytes.Equal(packet.Body, wantBody) {
		t.Fatalf("ack body = % x, want % x", packet.Body, wantBody)
	}
	active, trailing := splitGameServerUpperPacket(t, rest)
	if len(trailing) != 0 || active.Header.Classification != 0 || active.Header.MsgID != currentActiveQuestSnapshotMsgID {
		t.Fatalf("active snapshot header=%+v trailing=%x", active.Header, trailing)
	}
	wantActive := []byte{0x01, 0x00, 0x00, 0x00, 0x49, 0x0c, 0x01, 0x00, 0x00, 0x00}
	if !bytes.Equal(active.Body, wantActive) {
		t.Fatalf("active snapshot body=%x want=%x", active.Body, wantActive)
	}
}

func TestSuppressSceneInitializationAcceptQuestAllowsFinalizedTownScene(t *testing.T) {
	session := &gameSession{
		selectedCharacterID:       19,
		townSceneReadyCharacterID: 19,
		initialTownRouteStage:     currentInitialTownRouteIdle,
	}
	suppressed, readiness, stage := (&Service{}).suppressSceneInitializationAcceptQuestRequest(session)
	if suppressed || readiness != "town_scene_player_state_finalized" || stage != currentInitialTownRouteIdle {
		t.Fatalf("town-ready accept suppression=%t readiness=%q stage=%d", suppressed, readiness, stage)
	}
}

func TestCurrentAcceptQuestAlreadyActiveDoesNotReply(t *testing.T) {
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(context.Background(), dnfrepo.CharacterRecord{
		CharacterID: "20",
		AccountID:   defaultAccountPrefix + "1",
		Job:         "2",
		Level:       1,
		Stats:       map[string]int64{"grow_type": 0},
	}); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := &Service{
		options:            options{gameUpperBodyCodec: gameUpperBodyCodecPlain},
		questCatalog:       buildAcceptQuestTestCatalog(t, false),
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	session := &gameSession{
		conn:                        connection,
		connID:                      "accept-quest-idempotent-test",
		channel:                     channelcatalog.Channel{ID: 38},
		selectedCharacterID:         20,
		initialTownRouteCharacterID: 20,
		initialTownRouteStage:       currentInitialTownRoutePlayerStateSent,
	}
	body := []byte{0x1f, 0x00, 0x49, 0x0c}
	if err := service.handleCurrentAcceptQuest(session, body); err != nil {
		t.Fatal(err)
	}
	connection.write.Reset()
	if err := service.handleCurrentAcceptQuest(session, body); err != nil {
		t.Fatal(err)
	}
	if connection.write.Len() != 0 {
		t.Fatalf("idempotent accept emitted response=%x", connection.write.Bytes())
	}
}

func TestCurrentAcceptQuestEventItemGapDoesNotMutateOrAck(t *testing.T) {
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(context.Background(), dnfrepo.CharacterRecord{
		CharacterID: "18", AccountID: defaultAccountPrefix + "1", Job: "2", Level: 1,
	}); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := &Service{
		questCatalog:       buildAcceptQuestTestCatalog(t, true),
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	session := &gameSession{
		conn:                        connection,
		selectedCharacterID:         18,
		initialTownRouteCharacterID: 18,
		initialTownRouteStage:       currentInitialTownRoutePlayerStateSent,
	}
	if err := service.handleCurrentAcceptQuest(session, []byte{0x1f, 0x00, 0x49, 0x0c}); err != nil {
		t.Fatal(err)
	}
	if connection.write.Len() != 0 {
		t.Fatalf("blocked event-item quest emitted ACK = %x", connection.write.Bytes())
	}
	if _, ok, err := repositories.Quest.Load(context.Background(), "18"); err != nil || ok {
		t.Fatalf("blocked event-item quest mutated record ok=%t err=%v", ok, err)
	}
}

func TestCurrentAcceptQuestPersistedActiveAcknowledgesOnceWithoutSnapshot(t *testing.T) {
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(context.Background(), dnfrepo.CharacterRecord{
		CharacterID: "20",
		AccountID:   defaultAccountPrefix + "1",
		Job:         "2",
		Level:       1,
		Stats:       map[string]int64{"grow_type": 0},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Quest.Save(context.Background(), dnfrepo.QuestRecord{
		CharacterID: "20",
		States: map[int64]dnfrepo.QuestState{
			3145: {Status: "active", ProgressValue: 1},
		},
	}); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := &Service{
		options:            options{gameUpperBodyCodec: gameUpperBodyCodecPlain},
		questCatalog:       buildAcceptQuestTestCatalog(t, false),
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	session := &gameSession{
		conn:                        connection,
		selectedCharacterID:         20,
		initialTownRouteCharacterID: 20,
		initialTownRouteStage:       currentInitialTownRoutePlayerStateSent,
	}
	if err := service.handleCurrentAcceptQuest(session, []byte{0x1f, 0x00, 0x49, 0x0c}); err != nil {
		t.Fatal(err)
	}
	ack, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if len(rest) != 0 || ack.Header.MsgID != uint16(dnfenum.CmdPacketAcceptQuest) ||
		ack.Header.Classification != dnfproto.DefaultChannelClassification ||
		!bytes.Equal(ack.Body, []byte{1, 0x49, 0x0c, 0x01, 0, 0, 0, 0}) {
		t.Fatalf("persisted-active accept ACK header=%+v body=%x trailing=%x", ack.Header, ack.Body, rest)
	}
	connection.write.Reset()
	if err := service.handleCurrentAcceptQuest(session, []byte{0x1f, 0x00, 0x49, 0x0c}); err != nil {
		t.Fatal(err)
	}
	if got := connection.write.Bytes(); len(got) != 0 {
		t.Fatalf("same-session persisted-active replay emitted response=%x", got)
	}
}

func TestKnownTerminalQuestReplaySkipsPreDispatchLoggingOnlyForExactRequest(t *testing.T) {
	session := &gameSession{selectedCharacterID: 20}
	acceptRequest := dnfquest.QuestIDRequest{QuestID: 3145}
	session.suppressCurrentAcceptQuestReplay(newCurrentAcceptQuestReplayKey(session.selectedCharacterID, acceptRequest))
	acceptBody := []byte{0x1f, 0x00, 0x49, 0x0c}
	if !shouldSuppressKnownQuestReplayBeforeGameUpperLog(session, uint16(dnfenum.CmdPacketAcceptQuest), dnfproto.DefaultChannelClassification, acceptBody) {
		t.Fatal("known active op31 replay was not suppressed before logging")
	}
	if shouldSuppressKnownQuestReplayBeforeGameUpperLog(session, uint16(dnfenum.CmdPacketAcceptQuest), dnfproto.DefaultChannelClassification, []byte{0x1f, 0x00, 0x4a, 0x0c}) {
		t.Fatal("different op31 quest was incorrectly suppressed")
	}
	if shouldSuppressKnownQuestReplayBeforeGameUpperLog(session, uint16(dnfenum.CmdPacketAcceptQuest), dnfproto.DefaultChannelClassification, []byte{0x20, 0x00, 0x49, 0x0c}) {
		t.Fatal("malformed op31 echo was incorrectly suppressed")
	}

	triggerRequest := dnfquest.SetTriggerRequest{QuestID: 4728, TriggerType: 16}
	session.suppressCurrentSetQuestTriggerReplay(newCurrentSetQuestTriggerReplayKey(session.selectedCharacterID, triggerRequest))
	triggerBody := []byte{0x21, 0x00, 0x78, 0x12, 0x10, 0x00}
	if !shouldSuppressKnownQuestReplayBeforeGameUpperLog(session, uint16(dnfenum.CmdPacketSetQuestTrigger), dnfproto.DefaultChannelClassification, triggerBody) {
		t.Fatal("known zero op33 replay was not suppressed before logging")
	}
	if shouldSuppressKnownQuestReplayBeforeGameUpperLog(session, uint16(dnfenum.CmdPacketSetQuestTrigger), dnfproto.DefaultChannelClassification, []byte{0x21, 0x00, 0x78, 0x12, 0x11, 0x00}) {
		t.Fatal("different op33 trigger channel was incorrectly suppressed")
	}
}

func TestCurrentAcceptQuestKnownIneligiblePassiveRepeatIsSilent(t *testing.T) {
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(context.Background(), dnfrepo.CharacterRecord{
		CharacterID: "19", AccountID: defaultAccountPrefix + "1", Job: "2", Level: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Quest.Save(context.Background(), dnfrepo.QuestRecord{
		CharacterID: "19",
		States: map[int64]dnfrepo.QuestState{
			3145: {Status: "completed"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := &Service{
		options:            options{gameUpperBodyCodec: gameUpperBodyCodecPlain},
		questCatalog:       buildAcceptQuestTestCatalog(t, false),
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	session := &gameSession{
		conn:                        connection,
		selectedCharacterID:         19,
		initialTownRouteCharacterID: 19,
		initialTownRouteStage:       currentInitialTownRoutePlayerStateSent,
	}
	if err := service.handleCurrentAcceptQuest(session, []byte{0x1f, 0x00, 0x49, 0x0c}); err != nil {
		t.Fatal(err)
	}
	if connection.write.Len() != 0 {
		t.Fatalf("known ineligible passive repeat emitted failure popup packet=%x", connection.write.Bytes())
	}
	stored, ok, err := repositories.Quest.Load(context.Background(), "19")
	if err != nil || !ok || stored.States[3145].Status != "completed" {
		t.Fatalf("ineligible quest state changed ok=%t err=%v state=%+v", ok, err, stored.States[3145])
	}
}

func TestCurrentAcceptQuestUnknownDefinitionKeepsCurrentFailure23(t *testing.T) {
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(context.Background(), dnfrepo.CharacterRecord{
		CharacterID: "19", AccountID: defaultAccountPrefix + "1", Job: "2", Level: 1,
	}); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := &Service{
		options:            options{gameUpperBodyCodec: gameUpperBodyCodecPlain},
		questCatalog:       buildAcceptQuestTestCatalog(t, false),
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	session := &gameSession{
		conn:                        connection,
		selectedCharacterID:         19,
		initialTownRouteCharacterID: 19,
		initialTownRouteStage:       currentInitialTownRoutePlayerStateSent,
	}
	if err := service.handleCurrentAcceptQuest(session, []byte{0x1f, 0x00, 0x0f, 0x27}); err != nil {
		t.Fatal(err)
	}
	packet, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if len(rest) != 0 || packet.Header.MsgID != uint16(dnfenum.CmdPacketAcceptQuest) ||
		!bytes.Equal(packet.Body, []byte{0x00, 0x17}) {
		t.Fatalf("unknown-definition failure header=%+v body=%x rest=%x", packet.Header, packet.Body, rest)
	}
	if _, ok, err := repositories.Quest.Load(context.Background(), "19"); err != nil || ok {
		t.Fatalf("unknown definition mutated quest record ok=%t err=%v", ok, err)
	}
}

func TestCurrentAcceptQuestKnownIneligibleBeforeInitialTownTransitionSendsNoFailure(t *testing.T) {
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(context.Background(), dnfrepo.CharacterRecord{
		CharacterID: "19", AccountID: defaultAccountPrefix + "1", Job: "2", Level: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Quest.Save(context.Background(), dnfrepo.QuestRecord{
		CharacterID: "19",
		States: map[int64]dnfrepo.QuestState{
			3145: {Status: "completed"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := &Service{
		options:            options{gameUpperBodyCodec: gameUpperBodyCodecPlain},
		questCatalog:       buildAcceptQuestTestCatalog(t, false),
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	session := &gameSession{
		conn:                        connection,
		selectedCharacterID:         19,
		initialTownRouteCharacterID: 19,
		initialTownRouteStage:       currentInitialTownRouteArmed,
	}
	if err := service.handleCurrentAcceptQuest(session, []byte{0x1f, 0x00, 0x49, 0x0c}); err != nil {
		t.Fatal(err)
	}
	if connection.write.Len() != 0 {
		t.Fatalf("initial-town automatic ineligible accept emitted packet=%x", connection.write.Bytes())
	}
	stored, ok, err := repositories.Quest.Load(context.Background(), "19")
	if err != nil || !ok || stored.States[3145].Status != "completed" {
		t.Fatalf("initial-town ineligible quest state changed ok=%t err=%v state=%+v", ok, err, stored.States[3145])
	}
}

func TestCurrentAcceptQuestEligibleBeforeInitialTownTransitionDoesNotMutateOrReply(t *testing.T) {
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(context.Background(), dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   defaultAccountPrefix + "1",
		Job:         "2",
		Level:       1,
		Stats:       map[string]int64{"grow_type": 0},
	}); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := &Service{
		options:            options{gameUpperBodyCodec: gameUpperBodyCodecPlain},
		questCatalog:       buildAcceptQuestTestCatalog(t, false),
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	session := &gameSession{
		conn:                        connection,
		selectedCharacterID:         19,
		initialTownRouteCharacterID: 19,
		initialTownRouteStage:       currentInitialTownRouteArmed,
	}
	if err := service.handleCurrentAcceptQuest(session, []byte{0x1f, 0x00, 0x49, 0x0c}); err != nil {
		t.Fatal(err)
	}
	if connection.write.Len() != 0 {
		t.Fatalf("initial-town automatic eligible accept emitted packet=%x", connection.write.Bytes())
	}
	if _, ok, err := repositories.Quest.Load(context.Background(), "19"); err != nil || ok {
		t.Fatalf("initial-town automatic eligible accept mutated quest record ok=%t err=%v", ok, err)
	}
}

func TestCurrentAcceptQuestEligibleOnColdFirstLoginBeforeSceneRouteDoesNotMutateOrReply(t *testing.T) {
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(context.Background(), dnfrepo.CharacterRecord{
		CharacterID: "21",
		AccountID:   defaultAccountPrefix + "1",
		Job:         "2",
		Level:       1,
		Stats:       map[string]int64{"grow_type": 0},
	}); err != nil {
		t.Fatal(err)
	}
	connection := &bufferConn{}
	service := &Service{
		options:            options{gameUpperBodyCodec: gameUpperBodyCodecPlain},
		questCatalog:       buildAcceptQuestTestCatalog(t, false),
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	// A cold first-login can emit the passive op31 before either a town or a
	// tutorial/dungeon scene route has reached actor-ready state.
	session := &gameSession{conn: connection, selectedCharacterID: 21}
	if err := service.handleCurrentAcceptQuest(session, []byte{0x1f, 0x00, 0x49, 0x0c}); err != nil {
		t.Fatal(err)
	}
	if connection.write.Len() != 0 {
		t.Fatalf("cold first-login passive accept emitted packet=%x", connection.write.Bytes())
	}
	if _, ok, err := repositories.Quest.Load(context.Background(), "21"); err != nil || ok {
		t.Fatalf("cold first-login passive accept mutated quest record ok=%t err=%v", ok, err)
	}
}

func buildAcceptQuestTestCatalog(t *testing.T, eventItem bool) *dnfquest.Catalog {
	t.Helper()
	extra := "[type]\n`[clear map]`\n[int data]\n76126\n"
	if eventItem {
		extra += "[depend give item]\n1001 1\n"
	}
	source := questListTestSource{
		dnfquest.DefaultList: "3145 `accept.qst`\n",
		"n_quest/accept.qst": "[grade]\n`[epic]`\n[level]\n1 99\n[job]\n`[gunner]`\n[exposed by npc]\n1\n" + extra,
	}
	index, err := dnfpvf.Build(context.Background(), source, dnfpvf.BuildOptions{Lists: []string{dnfquest.DefaultList}})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := dnfquest.Load(context.Background(), index)
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}
