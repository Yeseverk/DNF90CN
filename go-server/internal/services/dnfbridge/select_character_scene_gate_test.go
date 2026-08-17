package dnfbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"
	"time"

	"longheng.io/server/internal/modules/dnf/channelcatalog"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfinventory "longheng.io/server/internal/modules/dnf/inventory"
	dnfitemlock "longheng.io/server/internal/modules/dnf/itemlock"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	dnfquest "longheng.io/server/internal/modules/dnf/quest"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func TestSelectedCharacterStartsInTutorialUsesPersistedCompletion(t *testing.T) {
	tests := []struct {
		name         string
		character    dnfrepo.CharacterRecord
		hasCharacter bool
		want         bool
	}{
		{
			name:         "new character pending",
			character:    dnfrepo.CharacterRecord{Level: 1, Stats: map[string]int64{currentDungeonTutorialCompletedKey: 0}},
			hasCharacter: true,
			want:         true,
		},
		{
			name:         "pending marker remains authoritative after level change",
			character:    dnfrepo.CharacterRecord{Level: 2, Stats: map[string]int64{currentDungeonTutorialCompletedKey: 0}},
			hasCharacter: true,
			want:         true,
		},
		{
			name:         "completed tutorial starts in town",
			character:    dnfrepo.CharacterRecord{Level: 1, Stats: map[string]int64{currentDungeonTutorialCompletedKey: currentDungeonTutorialCompleteFlag}},
			hasCharacter: true,
			want:         false,
		},
		{
			name:         "legacy established character without marker starts in town",
			character:    dnfrepo.CharacterRecord{Level: 30, Stats: map[string]int64{}},
			hasCharacter: true,
			want:         false,
		},
		{
			name:         "legacy level one character without marker starts tutorial",
			character:    dnfrepo.CharacterRecord{Level: 1, Stats: map[string]int64{}},
			hasCharacter: true,
			want:         true,
		},
		{
			name:         "missing character does not invent tutorial state",
			hasCharacter: false,
			want:         false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := selectedCharacterStartsInTutorial(test.character, test.hasCharacter); got != test.want {
				t.Fatalf("selectedCharacterStartsInTutorial() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestSpecialChannelSelectCharacterHelpersUseOrdinaryScenePath(t *testing.T) {
	service, session, _ := newTownMoveTest(t)
	connection := session.conn.(*bufferConn)
	session.channel = channelcatalog.Channel{ServerID: 1, ID: 200, Type: 23, Name: "special", Port: 10200}
	session.residentChannel = session.channel
	connection.write.Reset()
	request := make([]byte, 2)
	binary.LittleEndian.PutUint16(request, 29)

	if err := service.sendUpperCSharpSelectInit(session, request); err != nil {
		t.Fatal(err)
	}
	if connection.write.Len() == 0 {
		t.Fatal("special channel upper select helper remained blocked")
	}

	connection.write.Reset()
	legacyRequest := make([]byte, 11)
	if err := service.sendSelectCharacterState(session, legacyRequest); err != nil {
		t.Fatal(err)
	}
	if connection.write.Len() == 0 {
		t.Fatal("special channel legacy select helper remained blocked")
	}
}

func TestCompletedTutorialSelectUsesAckPage1RouteWithoutParallelTransition(t *testing.T) {
	const characterID = uint16(60042)
	repos := testRepositoryGroup()
	repos.Character.(*fakeCharacterStore).records = map[string]dnfrepo.CharacterRecord{
		"60042": {
			CharacterID: "60042",
			AccountID:   defaultAccountPrefix + "1",
			Slot:        0,
			Name:        "completed",
			Job:         "0",
			Level:       1,
			Stats: map[string]int64{
				"town_id":                          38,
				"area_id":                          1,
				"pos_x":                            450,
				"pos_y":                            234,
				"direction":                        5,
				"area_state":                       3,
				currentDungeonTutorialCompletedKey: currentDungeonTutorialCompleteFlag,
			},
		},
	}
	service := &Service{
		options:            options{accountPrefix: defaultAccountPrefix},
		repositoryProvider: func() (dnfrepo.Group, bool) { return repos, true },
	}
	conn := &bufferConn{}
	session := &gameSession{
		conn:            conn,
		channel:         channelcatalog.Channel{ID: 16, Type: 1, Name: "ch.16", Port: 10016},
		residentChannel: channelcatalog.Channel{ID: 16, Type: 1, Name: "ch.16", Port: 10016},
	}
	request := make([]byte, 2)
	binary.LittleEndian.PutUint16(request, characterID)

	if err := service.sendUpperCSharpSelectInit(session, request); err != nil {
		t.Fatalf("send completed select init: %v", err)
	}

	ack, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if ack.Header.MsgID != uint16(dnfenum.CmdPacketSelectCharacter) {
		t.Fatalf("first packet msg=%d, want select ACK", ack.Header.MsgID)
	}
	flag, count, ok := currentSelectAckTutorialState(ack.Body)
	if !ok || flag != 0 || count != 1 {
		t.Fatalf("completed select ACK tutorial state flag=%d count=%d ok=%t, want 0/1/true", flag, count, ok)
	}
	indexes, ok := currentSelectAckTutorialIndexes(ack.Body)
	if !ok || len(indexes) != 1 || indexes[0] != currentSelectAckPage1RouteIndex {
		t.Fatalf("completed select ACK tutorial indexes=%v ok=%t, want [%d]/true", indexes, ok, currentSelectAckPage1RouteIndex)
	}
	rest = requireCurrentStoryDigestLastLevelPacket(t, rest, 0)
	if len(rest) != 0 {
		next, _ := splitGameServerUpperPacket(t, rest)
		t.Fatalf("completed character received parallel scene packet class=%d msg=%d body=%x", next.Header.Classification, next.Header.MsgID, next.Body)
	}
	if session.initialTownRouteCharacterID != characterID ||
		session.initialTownRouteStage != currentInitialTownRouteArmed {
		t.Fatalf("completed select route char=%d stage=%d, want %d/armed", session.initialTownRouteCharacterID, session.initialTownRouteStage, characterID)
	}
	if returnSelectTownReentryPending(session) {
		t.Fatal("cold first select invented a return-select re-entry owner")
	}
}

func TestReturnSelectReselectResumesTownRouteWithoutSyntheticProgress36Ack(t *testing.T) {
	service, session, repositories := newTownMoveTest(t)
	if err := repositories.Account.Save(context.Background(), dnfrepo.AccountRecord{
		AccountID: defaultAccountPrefix + "1",
		Metadata:  make(map[string]string),
	}); err != nil {
		t.Fatal(err)
	}
	skillCatalog, err := buildSkillCatalogFromSource(context.Background(), initialEquipmentMemSource{
		"skill/skilllist.lst":    "0 `job0.lst`\n",
		"skill/job0.lst":         "46 `job0/initial.skl`\n",
		"skill/job0/initial.skl": "[skill type]\n`active`\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	service.initialSkillsByJob = map[byte][]initialSkillEntry{0: {{SkillID: 46, Level: 1}}}
	service.initialSPTable = map[int]int{1: 20}
	service.skillCatalog = skillCatalog
	service.questCatalog = buildQuestListTestCatalog(t)
	service.options.accountPrefix = defaultAccountPrefix
	service.adventureGroupTable = loadAdventureGroupTestTables(t)
	questIndex, err := dnfpvf.Build(context.Background(), questListTestSource{
		dnfquest.DefaultList:  "900 `visible.qst`\n901 `active.qst`\n",
		"n_quest/visible.qst": "[grade]\n`[epic]`\n[level]\n1 10\n[exposed by npc]\n1\n",
		"n_quest/active.qst":  "[grade]\n`[epic]`\n[level]\n1 10\n[exposed by npc]\n1\n",
	}, dnfpvf.BuildOptions{Lists: []string{dnfquest.DefaultList}})
	if err != nil {
		t.Fatal(err)
	}
	service.questCatalog, err = dnfquest.Load(context.Background(), questIndex)
	if err != nil {
		t.Fatal(err)
	}
	character, found, err := repositories.Character.Load(context.Background(), "29")
	if err != nil || !found {
		t.Fatalf("load town character found=%t err=%v", found, err)
	}
	character.Name = "return-select-reentry"
	character.Job = "0"
	character.Level = 4
	character.Stats["exp"] = 4948
	character.Stats[currentDungeonTutorialCompletedKey] = currentDungeonTutorialCompleteFlag
	if err := repositories.Character.Save(context.Background(), character); err != nil {
		t.Fatal(err)
	}
	session.channel = channelcatalog.Channel{ServerID: 1, ID: 253, Type: 40, Name: "ch.253", Port: 10253}
	session.residentChannel = session.channel
	session.connectionTownActorOwnerChannel = 253
	session.selectedCharacterID = 0
	markReturnSelectTownReentry(session, 29)
	request := make([]byte, 2)
	binary.LittleEndian.PutUint16(request, 29)

	if err := service.sendUpperCSharpSelectInit(session, request); err != nil {
		t.Fatal(err)
	}

	fullRouteStream := append([]byte(nil), session.conn.(*bufferConn).write.Bytes()...)
	assertNoCurrentChannelNoticeInTownRoute(t, fullRouteStream)
	if got := countCurrentActorModePackets(t, fullRouteStream, 1); got != 2 {
		t.Fatalf("return-select mode1 packet count=%d want prerequisite binding plus pre-op24 full state", got)
	}
	stream := fullRouteStream
	selectAck, stream := splitGameServerUpperPacket(t, stream)
	if selectAck.Header.Classification != dnfproto.DefaultChannelClassification ||
		selectAck.Header.MsgID != uint16(dnfenum.CmdPacketSelectCharacter) {
		t.Fatalf("select ack=%+v body=%x", selectAck.Header, selectAck.Body)
	}
	stream = requireCurrentStoryDigestLastLevelPacket(t, stream, 0)
	actionTable, stream := splitGameServerUpperPacket(t, stream)
	if actionTable.Header.Classification != 0 ||
		actionTable.Header.MsgID != uint16(dnfenum.CmdPacketPVPMissionHpPercent) ||
		!bytes.Equal(actionTable.Body, buildCurrentActionTableStateBody()) {
		t.Fatalf("action table=%+v body=%x", actionTable.Header, actionTable.Body)
	}
	mode0, stream := splitGameServerUpperPacket(t, stream)
	targetOwner := byte(253)
	if mode0.Header.Classification != 0 ||
		mode0.Header.MsgID != uint16(dnfenum.CmdPacketSetUDPIPPort) || len(mode0.Body) < 0x4e || mode0.Body[0] != 0 ||
		mode0.Body[3] != currentSceneObjectRoute || mode0.Body[4] != targetOwner {
		t.Fatalf("mode0=%+v body=%x", mode0.Header, mode0.Body)
	}
	preparedState, transition, rest := splitInitialTownPreparedStateBeforeTransition(t, stream)
	rest = requireNoCurrentChannelNoticeAfterTownTransition(t, rest)
	if transition.Header.Classification != 0 || transition.Header.MsgID != currentSceneTransitionMsgID || len(rest) != 0 {
		t.Fatalf("transition=%+v body=%x rest=%x", transition.Header, transition.Body, rest)
	}
	if session.townPostTransition.stage != currentTownPostTransitionIdle {
		t.Fatalf("return-select post-transition stage=%d want idle", session.townPostTransition.stage)
	}
	if len(preparedState) == 0 {
		t.Fatal("return-select transition had no prepared player state before op24")
	}
	returnSelectOp9 := 0
	for ownerStream := preparedState; len(ownerStream) > 0; {
		packet, next := splitCurrentGameServerUpperPacketAuto(t, ownerStream)
		ownerStream = next
		if packet.Header.Classification == 0 &&
			packet.Header.MsgID == uint16(dnfenum.CmdPacketRecoverStamina) {
			returnSelectOp9++
			assertCurrentPartylessTownOp9Body(t, packet.Body, targetOwner)
		}
	}
	if returnSelectOp9 != 1 {
		t.Fatalf("return-select op9 count=%d want=1", returnSelectOp9)
	}
	if selectAck.Header.MsgID == uint16(dnfenum.CmdPacketChangeTutorialFlag) ||
		actionTable.Header.MsgID == uint16(dnfenum.CmdPacketChangeTutorialFlag) {
		t.Fatal("return-select re-entry emitted a synthetic op143 ACK")
	}
	if returnSelectTownReentryPending(session) ||
		session.initialTownRouteStage != currentInitialTownRoutePlayerStateSent ||
		!session.currentSceneObjectListSent ||
		session.townActorOwnerChannel != targetOwner {
		t.Fatalf(
			"re-entry pending=%t stage=%d object=%t owner=%d want=%d",
			returnSelectTownReentryPending(session),
			session.initialTownRouteStage,
			session.currentSceneObjectListSent,
			session.townActorOwnerChannel,
			targetOwner,
		)
	}

	connection := session.conn.(*bufferConn)
	connection.write.Reset()
	barrier, err := dnfproto.BuildChannelPacket(
		uint16(dnfenum.UpperMsgCheckUserConnection),
		nil,
		0,
		dnfproto.DefaultChannelClassification,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.handleGameUpper(session, barrier); err != nil {
		t.Fatal(err)
	}
	barrierAck, playerState := splitGameServerUpperPacket(t, connection.write.Bytes())
	if barrierAck.Header.MsgID != uint16(dnfenum.UpperMsgCheckUserConnection) ||
		!bytes.Equal(barrierAck.Body, upperSuccessBody(buildCurrentCheckUserConnectionSuccessPayload())) || len(playerState) != 0 {
		t.Fatalf("barrier ack=%+v body=%x player_state=%d", barrierAck.Header, barrierAck.Body, len(playerState))
	}
	if session.initialTownRouteStage != currentInitialTownRoutePlayerStateSent ||
		!session.selectedUserInfoRefreshSent || session.selectedUserInfoMode3Sent ||
		session.sceneBootstrapTailSent || !session.sceneBootstrapTailDeferred {
		t.Fatalf("re-entry full state stage=%d mode1=%t mode3_sent=%t tail_sent=%t tail_deferred=%t",
			session.initialTownRouteStage,
			session.selectedUserInfoRefreshSent,
			session.selectedUserInfoMode3Sent,
			session.sceneBootstrapTailSent,
			session.sceneBootstrapTailDeferred)
	}
	op37Count := 0
	op19Count := 0
	for stream := playerState; len(stream) > 0; {
		packet, rest := splitCurrentGameServerUpperPacketAuto(t, stream)
		stream = rest
		if packet.Header.Classification == 0 && packet.Header.MsgID == currentSkillInfoMsgID {
			op19Count++
		}
		if packet.Header.Classification == 0 && packet.Header.MsgID == uint16(dnfenum.CmdPacketFinishLoading) {
			op37Count++
		}
	}
	if op37Count != 0 || op19Count != 0 || session.currentFinishLoadingStateSent {
		t.Fatalf("return-select unsolicited progression=%d skill_replays=%d gate=%t", op37Count, op19Count, session.currentFinishLoadingStateSent)
	}
}

func TestCompletedSelectProgress36BindsRealActorBeforeTypedTownTransition(t *testing.T) {
	service, session, repositories := newTownMoveTest(t)
	skillCatalog, err := buildSkillCatalogFromSource(context.Background(), initialEquipmentMemSource{
		"skill/skilllist.lst":    "0 `job0.lst`\n",
		"skill/job0.lst":         "46 `job0/initial.skl`\n",
		"skill/job0/initial.skl": "[skill type]\n`active`\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	service.initialSkillsByJob = map[byte][]initialSkillEntry{0: {{SkillID: 46, Level: 1}}}
	service.initialSPTable = map[int]int{1: 20}
	service.skillCatalog = skillCatalog
	service.questCatalog = buildQuestListTestCatalog(t)
	service.options.accountPrefix = defaultAccountPrefix
	service.adventureGroupTable = loadAdventureGroupTestTables(t)
	character, found, err := repositories.Character.Load(context.Background(), "29")
	if err != nil || !found {
		t.Fatalf("load town character found=%t err=%v", found, err)
	}
	character.Name = "completed-town"
	character.Job = "0"
	character.Level = 4
	character.Stats["exp"] = 4948
	character.Stats["area_id"] = 0
	character.Stats["pos_x"] = 55
	character.Stats["pos_y"] = 267
	character.Stats[currentDungeonTutorialCompletedKey] = currentDungeonTutorialCompleteFlag
	if err := repositories.Character.Save(context.Background(), character); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Account.Save(context.Background(), dnfrepo.AccountRecord{
		AccountID: defaultAccountPrefix + "1",
		Metadata:  map[string]string{currentRentalPointMetadataKey: "124"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Quest.Save(context.Background(), dnfrepo.QuestRecord{
		CharacterID: "29",
		States: map[int64]dnfrepo.QuestState{
			901:  {Status: "active", ProgressValue: 1},
			3149: {Status: "completed"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if body, ok := service.buildCurrentAcceptableQuestListBodyForSession(context.Background(), session); !ok || len(body) <= 4 {
		_, _, loadedCharacter, selectedFound := service.selectedCharacterForEnter(context.Background(), session)
		record, recordFound, recordErr := repositories.Quest.Load(context.Background(), "29")
		_, repoOK := service.repositoryGroup()
		t.Fatalf("completed-town quest snapshot fixture skipped ok=%t body_len=%d repo_ok=%t catalog_nil=%t selected_found=%t loaded=%+v quest_found=%t quest_err=%v quest=%+v",
			ok, len(body), repoOK, service.questCatalog == nil, selectedFound, loadedCharacter, recordFound, recordErr, record)
	}
	seededSlots := map[string]dnfrepo.ItemStack{
		"0:3": {ItemID: 1003, Count: 1},
		"0:4": {ItemID: 1004, Count: 2},
		"0:5": {ItemID: 3227, Count: 4},
		"0:6": {ItemID: 1006, Count: 1},
		"0:7": {ItemID: 1007, Count: 1},
		"0:8": {ItemID: 1008, Count: 1},
	}
	if err := repositories.Inventory.Save(context.Background(), dnfrepo.InventoryRecord{
		CharacterID: "29",
		Slots:       seededSlots,
	}); err != nil {
		t.Fatal(err)
	}
	inventoryOwner, err := dnfinventory.NewOwner(repositories)
	if err != nil {
		t.Fatal(err)
	}
	moveResult, err := inventoryOwner.Move(context.Background(), dnfinventory.Command{
		SelectedCharacterID:  29,
		SourceListType:       dnfrepo.MainInventoryListType,
		SourceSlotIndex:      3,
		MoveCount:            1,
		DestinationListType:  dnfrepo.MainInventoryListType,
		DestinationSlotIndex: 4,
		DestinationStack:     2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !moveResult.Changed || moveResult.Mode != "swap" {
		t.Fatalf("quick-slot move result=%+v", moveResult)
	}
	pickupSlot, _, err := grantCurrentDungeonPickupItem(
		context.Background(),
		repositories.CharacterItems,
		"29",
		dungeonDropItemDefinition{
			ItemID:        3227,
			Kind:          dungeonDropItemStackable,
			PVFPath:       "stackable/material/test_drop.stk",
			StackableType: "[material]",
			StackLimit:    999,
			SlotStart:     65,
			SlotEnd:       120,
		},
		2,
		time.Unix(1_700_000_000, 0).UTC(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if pickupSlot != 5 {
		t.Fatalf("material pickup slot=%d, want existing quick slot 5", pickupSlot)
	}
	connection := session.conn.(*bufferConn)
	transitionWaitCalls := 0
	transitionWaitDuration := time.Duration(0)
	transitionWaitSawPassGate := false
	transitionWaitSawSceneCommit := false
	service.initialTownEntryWait = func(delay time.Duration) {
		transitionWaitCalls++
		transitionWaitDuration = delay
		for waitStream := append([]byte(nil), connection.write.Bytes()...); len(waitStream) > 0; {
			packet, rest := splitCurrentGameServerUpperPacketAuto(t, waitStream)
			waitStream = rest
			switch packet.Header.MsgID {
			case uint16(dnfenum.CmdPacketPassGateObject):
				transitionWaitSawPassGate = true
			case uint16(dnfenum.CmdPacketReportClientSpec),
				uint16(dnfenum.CmdPacketRecoverStamina),
				uint16(dnfenum.CmdPacketRequestBlacklist),
				currentTownUserPositionNotificationMsgID,
				currentTownUserAreaNotificationMsgID,
				currentSceneTransitionMsgID:
				transitionWaitSawSceneCommit = true
			}
		}
	}
	session.channel = channelcatalog.Channel{ServerID: 1, ID: 253, Type: 40, Name: "ch.253", Port: 10253}
	session.residentChannel = session.channel
	session.connectionTownActorOwnerChannel = 253
	request := make([]byte, 2)
	binary.LittleEndian.PutUint16(request, 29)
	if err := service.sendUpperCSharpSelectInit(session, request); err != nil {
		t.Fatal(err)
	}
	selectAck, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if selectAck.Header.MsgID != uint16(dnfenum.CmdPacketSelectCharacter) {
		t.Fatalf("completed select ack=%+v rest=%x", selectAck.Header, rest)
	}
	rest = requireCurrentStoryDigestLastLevelPacket(t, rest, 0)
	if len(rest) != 0 {
		t.Fatalf("completed select emitted unexpected scene stream=%x", rest)
	}
	if session.initialTownRouteCharacterID != 29 || session.initialTownRouteStage != currentInitialTownRouteArmed {
		t.Fatalf("route after select char=%d stage=%d", session.initialTownRouteCharacterID, session.initialTownRouteStage)
	}

	connection.write.Reset()
	progress36 := []byte{currentDungeonTutorialFinalPrefix, 36, 0, 0, 0, currentDungeonTutorialFinalCommit}
	if err := service.handleDungeonTutorialFlag(session, progress36); err != nil {
		t.Fatal(err)
	}

	fullRouteStream := append([]byte(nil), connection.write.Bytes()...)
	assertNoCurrentChannelNoticeInTownRoute(t, fullRouteStream)
	targetOwner := byte(253)
	if got := countCurrentActorModePackets(t, fullRouteStream, 1); got != 2 {
		t.Fatalf("cold completed-character mode1 packet count=%d want prerequisite binding plus pre-op24 full state", got)
	}
	ack143, stream := splitGameServerUpperPacket(t, fullRouteStream)
	if ack143.Header.Classification != dnfproto.DefaultChannelClassification ||
		ack143.Header.MsgID != uint16(dnfenum.CmdPacketChangeTutorialFlag) ||
		!bytes.Equal(ack143.Body, []byte{1, 0}) {
		t.Fatalf("progress36 ack=%+v body=%x", ack143.Header, ack143.Body)
	}
	actionTable, stream := splitGameServerUpperPacket(t, stream)
	if actionTable.Header.Classification != 0 ||
		actionTable.Header.MsgID != uint16(dnfenum.CmdPacketPVPMissionHpPercent) ||
		!bytes.Equal(actionTable.Body, buildCurrentActionTableStateBody()) {
		t.Fatalf("action table=%+v body=%x", actionTable.Header, actionTable.Body)
	}
	mode0, stream := splitGameServerUpperPacket(t, stream)
	objectKey := currentSceneActorObjectKey(29)
	if mode0.Header.Classification != 0 ||
		mode0.Header.MsgID != uint16(dnfenum.CmdPacketSetUDPIPPort) ||
		len(mode0.Body) < 0x4e || mode0.Body[0] != 0 ||
		mode0.Body[3] != 0 || mode0.Body[4] != targetOwner ||
		binary.LittleEndian.Uint16(mode0.Body[0x4c:0x4e]) != objectKey {
		t.Fatalf("mode0=%+v body=%x", mode0.Header, mode0.Body)
	}
	preTransitionPlayerStateStream, transition, stream := splitInitialTownPreparedStateBeforeTransition(t, stream)
	if len(stream) != 0 {
		t.Fatalf("initial town route emitted post-op24 player-state bytes before type1345=%x", stream)
	}
	if session.townPostTransition.stage != currentTownPostTransitionIdle {
		t.Fatalf("initial town post-transition stage=%d want idle", session.townPostTransition.stage)
	}
	fullMode1Count := 0
	op9Count := 0
	initializationLists := make(map[byte]dnfproto.ChannelPacket, len(currentSelectInventoryBootstrapListTypes))
	for ownerStream := preTransitionPlayerStateStream; len(ownerStream) > 0; {
		packet, next := splitCurrentGameServerUpperPacketAuto(t, ownerStream)
		ownerStream = next
		switch {
		case packet.Header.Classification == 0 &&
			packet.Header.MsgID == uint16(dnfenum.CmdPacketSetUDPIPPort) &&
			len(packet.Body) >= 5 && packet.Body[0] == 1:
			fullMode1Count++
			if packet.Body[3] != 0 || packet.Body[4] != targetOwner {
				t.Fatalf("cold full mode1 owner=%x want 00/%02x", packet.Body[:5], targetOwner)
			}
		case packet.Header.Classification == 0 &&
			packet.Header.MsgID == uint16(dnfenum.CmdPacketRecoverStamina):
			op9Count++
			assertCurrentPartylessTownOp9Body(t, packet.Body, targetOwner)
		case packet.Header.Classification == 0 &&
			packet.Header.MsgID == uint16(dnfenum.CmdPacketLeaveParty) &&
			len(packet.Body) > 0:
			initializationLists[packet.Body[0]] = packet
		}
	}
	if fullMode1Count != 2 || op9Count != 1 {
		t.Fatalf("cold target-owned finalizers mode1=%d op9=%d want 2/1", fullMode1Count, op9Count)
	}
	stream = requireNoCurrentChannelNoticeAfterTownTransition(t, stream)
	if transitionWaitCalls != 1 || transitionWaitDuration != currentInitialTownEntryDelay {
		t.Fatalf("town entry cover boundary calls=%d duration=%s", transitionWaitCalls, transitionWaitDuration)
	}
	if !transitionWaitSawPassGate || transitionWaitSawSceneCommit {
		t.Fatalf("town entry cover boundary pass_gate=%t scene_commit=%t", transitionWaitSawPassGate, transitionWaitSawSceneCommit)
	}
	wantTransition, err := buildCurrentSceneTransitionBody(newCharacterInitialTownID, newCharacterInitialAreaID, []currentSceneTransitionRow{{
		ObjectOrResourceKey: objectKey,
		Value1:              newCharacterInitialPosX,
		Value2:              newCharacterInitialPosY,
		Value3:              newCharacterInitialDirection,
		Value4:              newCharacterInitialAreaState,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if transition.Header.Classification != 0 ||
		transition.Header.MsgID != currentSceneTransitionMsgID ||
		!bytes.Equal(transition.Body, wantTransition) || len(stream) != 0 {
		t.Fatalf("transition=%+v body=%x want=%x rest=%x", transition.Header, transition.Body, wantTransition, stream)
	}
	preTransitionOp37 := 0
	for stateStream := preTransitionPlayerStateStream; len(stateStream) > 0; {
		packet, next := splitCurrentGameServerUpperPacketAuto(t, stateStream)
		stateStream = next
		if packet.Header.Classification == 0 && packet.Header.MsgID == uint16(dnfenum.CmdPacketFinishLoading) {
			preTransitionOp37++
		}
	}
	if preTransitionOp37 != 0 || session.currentFinishLoadingStateSent {
		t.Fatalf("pre-op24 unsolicited progression snapshots=%d gate=%t", preTransitionOp37, session.currentFinishLoadingStateSent)
	}
	if session.initialTownRouteStage != currentInitialTownRoutePlayerStateSent ||
		!session.currentSceneObjectListSent || session.postStartMapPlayerStateSent ||
		session.townActorOwnerChannel != targetOwner {
		t.Fatalf(
			"route stage=%d object=%t post_map=%t owner=%d want=%d",
			session.initialTownRouteStage,
			session.currentSceneObjectListSent,
			session.postStartMapPlayerStateSent,
			session.townActorOwnerChannel,
			targetOwner,
		)
	}
	stored, found, err := repositories.Character.Load(context.Background(), "29")
	if err != nil || !found || stored.Stats["town_id"] != newCharacterInitialTownID || stored.Stats["area_id"] != newCharacterInitialAreaID ||
		stored.Stats["pos_x"] != newCharacterInitialPosX || stored.Stats["pos_y"] != newCharacterInitialPosY ||
		stored.Stats["direction"] != newCharacterInitialDirection || stored.Stats["area_state"] != newCharacterInitialAreaState {
		t.Fatalf("character-list login did not persist runtime-PVF Seria location found=%t err=%v stats=%+v", found, err, stored.Stats)
	}

	connection.write.Reset()
	barrier, err := dnfproto.BuildChannelPacket(
		uint16(dnfenum.UpperMsgCheckUserConnection),
		nil,
		0,
		dnfproto.DefaultChannelClassification,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.handleGameUpper(session, barrier); err != nil {
		t.Fatal(err)
	}
	if connection.write.Len() == 0 || session.sceneBootstrapTailSent || !session.sceneBootstrapTailDeferred {
		t.Fatalf("town scene tail bytes=%d sent=%t deferred=%t", connection.write.Len(), session.sceneBootstrapTailSent, session.sceneBootstrapTailDeferred)
	}
	if session.initialTownRouteStage != currentInitialTownRoutePlayerStateSent ||
		!session.selectedUserInfoRefreshSent || session.selectedUserInfoMode3Sent {
		t.Fatalf("town player state stage=%d mode1=%t mode3_sent=%t", session.initialTownRouteStage, session.selectedUserInfoRefreshSent, session.selectedUserInfoMode3Sent)
	}
	actorReady, actorReadySource := service.currentSceneActorReadyForState(session)
	if !actorReady || actorReadySource != "initial_town_player_state_finalized" {
		t.Fatalf("pre-type1345 town actor ready=%t source=%q", actorReady, actorReadySource)
	}
	indexes := map[string]int{
		"op376":  -1,
		"op391":  -1,
		"mode1":  -1,
		"mode3":  -1,
		"op1340": -1,
		"op359":  -1,
		"op356":  -1,
		"op124":  -1,
		"op9":    -1,
		"op120":  -1,
		"op37":   -1,
		"op30":   -1,
		"op19":   -1,
		"op120h": -1,
		"op21":   -1,
		"op574":  -1,
		"op105":  -1,
		"op985":  -1,
	}
	barrierAck, sceneTailStream := splitGameServerUpperPacket(t, connection.write.Bytes())
	sceneTailStream = append([]byte(nil), sceneTailStream...)
	if barrierAck.Header.Classification != dnfproto.DefaultChannelClassification ||
		barrierAck.Header.MsgID != uint16(dnfenum.UpperMsgCheckUserConnection) ||
		!bytes.Equal(barrierAck.Body, upperSuccessBody(buildCurrentCheckUserConnectionSuccessPayload())) {
		t.Fatalf("town barrier ack=%+v body=%x", barrierAck.Header, barrierAck.Body)
	}
	postTransitionListCounts := make(map[byte]int)
	postTransitionOp37 := 0
	postTransitionSkillCount := 0
	for postStream := sceneTailStream; len(postStream) > 0; {
		packetWire := postStream
		packet, rest := splitCurrentGameServerUpperPacketAuto(t, postStream)
		postStream = rest
		if packet.Header.Classification == 0 && packet.Header.MsgID == uint16(dnfenum.CmdPacketFinishLoading) {
			postTransitionOp37++
		}
		if packet.Header.Classification == 0 && packet.Header.MsgID == currentSkillInfoMsgID {
			postTransitionSkillCount++
		}
		if packet.Header.MsgID != uint16(dnfenum.CmdPacketLeaveParty) {
			continue
		}
		if packet.Header.Classification != 0 || len(packet.Body) < 3 ||
			len(packetWire) < dnfproto.GameServerUpperHeaderSize16 ||
			!bytes.Equal(packetWire[11:16], make([]byte, 5)) {
			t.Fatalf("post-op24 item-list header=%+v wire=%x", packet.Header, packetWire[:minInt(len(packetWire), int(packet.Header.Length))])
		}
		listType := packet.Body[0]
		postTransitionListCounts[listType]++
	}
	for _, listType := range currentSelectedItemListTypes {
		if postTransitionListCounts[listType] != 0 {
			t.Fatalf("post-op24 scene-ready list%d refreshes=%d want=0, all=%+v", listType, postTransitionListCounts[listType], postTransitionListCounts)
		}
		if _, ok := initializationLists[listType]; !ok {
			t.Fatalf("pre-mode1 initialization missing list%d, all=%+v", listType, initializationLists)
		}
	}
	if postTransitionOp37 != 0 || session.currentFinishLoadingStateSent {
		t.Fatalf("post-op24 scene tail emitted unsolicited progression snapshots=%d gate=%t", postTransitionOp37, session.currentFinishLoadingStateSent)
	}
	if postTransitionSkillCount != 0 ||
		session.townPostTransition.stage != currentTownPostTransitionIdle {
		t.Fatalf(
			"CheckUserConnection actor tail skill packets=%d stage=%d, want 0/idle",
			postTransitionSkillCount,
			session.townPostTransition.stage,
		)
	}
	list0 := initializationLists[dnfrepo.MainInventoryListType].Body
	list0Count := int(binary.LittleEndian.Uint16(list0[3:5]))
	if list0Count != 6 || len(list0) != 5+list0Count*currentItemListEntryWireSize {
		t.Fatalf("post-op24 list0 count=%d body_len=%d body=%x", list0Count, len(list0), list0)
	}
	wantQuickSlots := map[uint16]struct {
		itemID uint32
		count  uint32
	}{
		3: {itemID: 1004, count: 2},
		4: {itemID: 1003, count: 1},
		5: {itemID: 3227, count: 6},
		6: {itemID: 1006, count: 1},
		7: {itemID: 1007, count: 1},
		8: {itemID: 1008, count: 1},
	}
	for rowIndex := 0; rowIndex < list0Count; rowIndex++ {
		row := list0[5+rowIndex*currentItemListEntryWireSize:]
		slot := binary.LittleEndian.Uint16(row[0:2])
		want, ok := wantQuickSlots[slot]
		if !ok {
			t.Fatalf("post-op24 list0 unexpected slot=%d row=%x", slot, row[:currentItemListEntryWireSize])
		}
		if itemID := binary.LittleEndian.Uint32(row[2:6]); itemID != want.itemID {
			t.Fatalf("post-op24 list0 slot=%d item=%d want=%d", slot, itemID, want.itemID)
		}
		if count := binary.LittleEndian.Uint32(row[6:10]); count != want.count {
			t.Fatalf("post-op24 list0 slot=%d count=%d want=%d", slot, count, want.count)
		}
		delete(wantQuickSlots, slot)
	}
	if len(wantQuickSlots) != 0 {
		t.Fatalf("post-op24 list0 missing persisted quick slots=%+v", wantQuickSlots)
	}
	connection.write.Reset()
	if err := service.handleLegacyGamePacket(session, buildLegacyTownSceneReadyAckPacket()); err != nil {
		t.Fatal(err)
	}
	type1345PlayerStateStream, gotInitialHUDSkillBody, type1345Trailing := splitInitialTownType1345PlayerState(
		t,
		connection.write.Bytes(),
		targetOwner,
	)
	if len(type1345PlayerStateStream) != 0 ||
		len(gotInitialHUDSkillBody) != 0 {
		t.Fatalf(
			"type1345 rebuilt player state or replayed skill player=%x skill=%x",
			type1345PlayerStateStream,
			gotInitialHUDSkillBody,
		)
	}
	sceneTailStream = type1345Trailing
	if len(sceneTailStream) == 0 {
		t.Fatal("type1345 omitted deferred scene tail")
	}
	actorReady, actorReadySource = service.currentSceneActorReadyForState(session)
	if !actorReady || actorReadySource != "initial_town_player_state_finalized" {
		t.Fatalf("post-type1345 town actor ready=%t source=%q", actorReady, actorReadySource)
	}
	firstHUDWriteLen := connection.write.Len()
	if err := service.handleLegacyGamePacket(session, buildLegacyTownSceneReadyAckPacket()); err != nil {
		t.Fatal(err)
	}
	if connection.write.Len() != firstHUDWriteLen {
		t.Fatalf("duplicate type1345 grew HUD stream from %d to %d", firstHUDWriteLen, connection.write.Len())
	}
	playerStateStream := append([]byte(nil), preTransitionPlayerStateStream...)
	playerStateStream = append(playerStateStream, type1345PlayerStateStream...)
	playerStateStream = append(playerStateStream, sceneTailStream...)
	packetIndex := 0
	skillInfoCount := 0
	mode1Count := 0
	for stream := playerStateStream; len(stream) > 0; packetIndex++ {
		packetWire := stream
		packet, rest := splitCurrentGameServerUpperPacketAuto(t, stream)
		stream = rest
		switch packet.Header.MsgID {
		case uint16(dnfenum.CmdPacketPVPMissionHpPercent):
			if !bytes.Equal(packet.Body, buildCurrentActionTableStateBody()) {
				t.Fatalf("town player-state op376 body=%x", packet.Body)
			}
			indexes["op376"] = packetIndex
		case uint16(dnfenum.CmdPacketCancelCargoPad):
			if packet.Header.Classification != 0 || len(packetWire) < dnfproto.GameServerUpperHeaderSize16 ||
				packetWire[15] != 1 || !bytes.Equal(packetWire[dnfproto.GameServerUpperHeaderSize16:packet.Header.Length], buildCurrentCancelCargoPadTransportBody()) {
				t.Fatalf("town op391 cargo reset header=%+v wire=%x", packet.Header, packetWire[:minInt(len(packetWire), int(packet.Header.Length))])
			}
			indexes["op391"] = packetIndex
		case uint16(dnfenum.CmdPacketSetUDPIPPort):
			if len(packet.Body) >= currentMode1BaseWireSize && packet.Body[0] == 1 {
				mode1Count++
				// The prerequisite mode1 binds the local controlled actor.
				// The second mode1 is the repository-backed full player state
				// whose order relative to the remaining initializers matters.
				indexes["mode1"] = packetIndex
			}
			if len(packet.Body) > 0 && packet.Body[0] == 3 && indexes["mode3"] < 0 {
				indexes["mode3"] = packetIndex
			}
		case currentAdventureInfoPushMsgID:
			if packet.Header.Classification != 0 || len(packet.Body) != 7442 ||
				binary.LittleEndian.Uint16(packet.Body[0:2]) != objectKey ||
				binary.LittleEndian.Uint32(packet.Body[14:18]) != currentAdventureInfoRawLength {
				t.Fatalf("town scene op1340 header=%+v body_len=%d prefix=%x", packet.Header, len(packet.Body), packet.Body[:minInt(len(packet.Body), 24)])
			}
			// The pre-mode1 model push is required before op24. A separate,
			// HUD-ready replay may follow the legacy scene-ready callback so the
			// native overhead adventure-name widget can bind it; retain the first
			// packet when asserting this pre-transition order.
			if indexes["op1340"] < 0 {
				indexes["op1340"] = packetIndex
			}
		case uint16(dnfenum.CmdPacketInsertOverseer):
			indexes["op359"] = packetIndex
		case currentClearQuestListMsgID:
			if len(packetWire) < dnfproto.GameServerUpperHeaderSize16 || int(packet.Header.Length) > len(packetWire) {
				t.Fatalf("town op356 fixed16 wire malformed: header=%+v wire_len=%d", packet.Header, len(packetWire))
			}
			transport := packetWire[dnfproto.GameServerUpperHeaderSize16:packet.Header.Length]
			plain, err := zlibDecompress(transport)
			if err != nil || len(plain) != 4+currentClearQuestListPayloadSize ||
				binary.LittleEndian.Uint32(plain[:4]) != currentClearQuestListPayloadSize {
				t.Fatalf("town op356 clear-quest body_len=%d plain_len=%d err=%v", len(transport), len(plain), err)
			}
			if plain[4+3149] != 1 {
				t.Fatalf("town op356 missing persisted completed quest 3149: flag=%d", plain[4+3149])
			}
			if plain[4+int(objectKey)] != 0 {
				t.Fatalf("town op356 wrote actor key %#x into clear-quest table", objectKey)
			}
			indexes["op356"] = packetIndex
		case uint16(dnfenum.CmdPacketReportClientSpec):
			if len(packet.Body) != 0 {
				t.Fatalf("town op124 body=%x", packet.Body)
			}
			indexes["op124"] = packetIndex
		case uint16(dnfenum.CmdPacketRecoverStamina):
			indexes["op9"] = packetIndex
		case uint16(dnfenum.CmdPacketRequestBlacklist):
			if indexes["op120"] < 0 {
				indexes["op120"] = packetIndex
			} else {
				indexes["op120h"] = packetIndex
			}
		case uint16(dnfenum.CmdPacketFinishLoading):
			indexes["op37"] = packetIndex
		case currentIncreaseStatusResultMsgID:
			indexes["op30"] = packetIndex
		case currentAcceptableQuestListMsgID:
			indexes["op21"] = packetIndex
		case currentActiveQuestSnapshotMsgID:
			indexes["op574"] = packetIndex
		case currentCreatureStateTableMsgID:
			indexes["op105"] = packetIndex
		case currentRentalStateMsgID:
			if packet.Header.Classification != 0 || len(packet.Body) != 8 ||
				binary.LittleEndian.Uint32(packet.Body[:4]) != 124 ||
				binary.LittleEndian.Uint32(packet.Body[4:]) != 0 {
				t.Fatalf("town op985 rental state header=%+v body=%x", packet.Header, packet.Body)
			}
			indexes["op985"] = packetIndex
		case uint16(dnfenum.CmdPacketWalkoutPartyMember):
			t.Fatalf("town initialization emitted acquisition-style op14 header=%+v body=%x", packet.Header, packet.Body)
		case currentSkillInfoMsgID:
			indexes["op19"] = packetIndex
			skillInfoCount++
		}
	}
	// Mode3's selected-character UI path must not run while the typed op24 town
	// transition has not completed. Mode1 supplies the real state/equipment
	// model required by the remaining pre-transition packets.
	if indexes["mode3"] >= 0 {
		t.Fatalf("town pre-transition unexpectedly emitted mode3: %+v", indexes)
	}
	wantOrder := []string{
		"op376",
		"op391",
		"op1340",
		"mode1",
		"op105",
		"op19",
		"op21",
		"op574",
		"op359",
		"op356",
		"op124",
		"op9",
		"op120",
		"op985",
	}
	for i, name := range wantOrder {
		if indexes[name] < 0 {
			t.Fatalf("town pre-finish packet %s missing: %+v", name, indexes)
		}
		if i > 0 && indexes[wantOrder[i-1]] >= indexes[name] {
			t.Fatalf("town pre-finish order %+v", indexes)
		}
	}
	if skillInfoCount != 1 {
		t.Fatalf("town skill initialization count=%d want=1 before op21/op574/op24, indexes=%+v", skillInfoCount, indexes)
	}
	if mode1Count != 2 {
		t.Fatalf("town actor mode1 count=%d want prerequisite plus pre-op24 full state, indexes=%+v", mode1Count, indexes)
	}
	if indexes["op985"] <= indexes["op105"] {
		t.Fatalf("deferred-tail rental state must follow the pre-op24 actor state: %+v", indexes)
	}
	connection.write.Reset()
	if err := service.handleGameUpper(session, barrier); err != nil {
		t.Fatal(err)
	}
	duplicateBarrierAck, duplicateBarrierRest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if duplicateBarrierAck.Header.MsgID != uint16(dnfenum.UpperMsgCheckUserConnection) ||
		!bytes.Equal(duplicateBarrierAck.Body, upperSuccessBody(buildCurrentCheckUserConnectionSuccessPayload())) || len(duplicateBarrierRest) != 0 {
		t.Fatalf("duplicate town barrier ack=%+v body=%x rest=%x", duplicateBarrierAck.Header, duplicateBarrierAck.Body, duplicateBarrierRest)
	}

	connection.write.Reset()
	if err := service.handleDungeonTutorialFlag(session, progress36); err != nil {
		t.Fatal(err)
	}
	duplicateAck, duplicateRest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if duplicateAck.Header.MsgID != uint16(dnfenum.CmdPacketChangeTutorialFlag) ||
		!bytes.Equal(duplicateAck.Body, []byte{1, 0}) || len(duplicateRest) != 0 {
		t.Fatalf("duplicate progress36 ack=%+v body=%x rest=%x", duplicateAck.Header, duplicateAck.Body, duplicateRest)
	}
}

func TestPendingTutorialSelectKeepsEmptyAckAndEnterDungeonPreview(t *testing.T) {
	const characterID = uint16(60043)
	repos := testRepositoryGroup()
	repos.Character.(*fakeCharacterStore).records = map[string]dnfrepo.CharacterRecord{
		"60043": {
			CharacterID: "60043",
			AccountID:   defaultAccountPrefix + "1",
			Slot:        0,
			Name:        "pending",
			Job:         "0",
			Level:       1,
			Stats: map[string]int64{
				currentDungeonTutorialCompletedKey: 0,
				// A fresh character already has a durable Seria-room login point.
				// Selecting it must not turn that town row into a tutorial-spawn
				// transition; its first scene remains the PVF tutorial dungeon.
				"town_id":    newCharacterInitialTownID,
				"area_id":    newCharacterInitialAreaID,
				"pos_x":      newCharacterInitialPosX,
				"pos_y":      newCharacterInitialPosY,
				"direction":  newCharacterInitialDirection,
				"area_state": newCharacterInitialAreaState,
			},
		},
	}
	service := &Service{
		options:            options{accountPrefix: defaultAccountPrefix},
		repositoryProvider: func() (dnfrepo.Group, bool) { return repos, true },
	}
	conn := &bufferConn{}
	session := &gameSession{
		conn:            conn,
		channel:         channelcatalog.Channel{ID: 16, Type: 1, Name: "ch.16", Port: 10016},
		residentChannel: channelcatalog.Channel{ID: 16, Type: 1, Name: "ch.16", Port: 10016},
	}
	request := make([]byte, 2)
	binary.LittleEndian.PutUint16(request, characterID)

	if err := service.sendUpperCSharpSelectInit(session, request); err != nil {
		t.Fatalf("send pending select init: %v", err)
	}

	ack, rest := splitGameServerUpperPacket(t, conn.write.Bytes())
	if ack.Header.MsgID != uint16(dnfenum.CmdPacketSelectCharacter) {
		t.Fatalf("first packet msg=%d, want select ACK", ack.Header.MsgID)
	}
	flag, count, ok := currentSelectAckTutorialState(ack.Body)
	if !ok || flag != 0 || count != 0 {
		t.Fatalf("pending select ACK tutorial state flag=%d count=%d ok=%t, want 0/0/true", flag, count, ok)
	}
	rest = requireCurrentStoryDigestLastLevelPacket(t, rest, 0)
	enterDungeon, _ := splitGameServerUpperPacket(t, rest)
	if enterDungeon.Header.Classification != dnfproto.DefaultChannelClassification || enterDungeon.Header.MsgID != uint16(dnfenum.CmdPacketEnterSelectDungeon) {
		t.Fatalf("pending next packet class=%d msg=%d, want current upper-success op15", enterDungeon.Header.Classification, enterDungeon.Header.MsgID)
	}
	if session.initialTownRouteCharacterID != 0 || session.initialTownRouteStage != currentInitialTownRouteIdle {
		t.Fatalf("pending tutorial armed town route char=%d stage=%d", session.initialTownRouteCharacterID, session.initialTownRouteStage)
	}
	if session.townSceneReadyCharacterID != 0 || session.townPositionSnapshot.CharacterID != 0 {
		t.Fatalf("pending tutorial initialized a town scene instead of the PVF dungeon: ready=%d snapshot=%+v", session.townSceneReadyCharacterID, session.townPositionSnapshot)
	}
	stored, found, err := repos.Character.Load(context.Background(), "60043")
	if err != nil || !found {
		t.Fatalf("reload pending character found=%t err=%v", found, err)
	}
	for key, want := range map[string]int64{
		"town_id":    newCharacterInitialTownID,
		"area_id":    newCharacterInitialAreaID,
		"pos_x":      newCharacterInitialPosX,
		"pos_y":      newCharacterInitialPosY,
		"direction":  newCharacterInitialDirection,
		"area_state": newCharacterInitialAreaState,
	} {
		if got := stored.Stats[key]; got != want {
			t.Fatalf("tutorial selection changed durable login field %s=%d, want %d", key, got, want)
		}
	}
}

func TestReturnSelectReentryBootstrapsListsAndRehydratesGuardianGemAfterFullMode1(t *testing.T) {
	service, session, repositories := newTownMoveTest(t)
	if err := repositories.Account.Save(context.Background(), dnfrepo.AccountRecord{
		AccountID: defaultAccountPrefix + "1",
		Metadata:  make(map[string]string),
	}); err != nil {
		t.Fatal(err)
	}
	skillCatalog, err := buildSkillCatalogFromSource(context.Background(), initialEquipmentMemSource{
		"skill/skilllist.lst":    "0 `job0.lst`\n",
		"skill/job0.lst":         "46 `job0/initial.skl`\n",
		"skill/job0/initial.skl": "[skill type]\n`active`\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	service.initialSkillsByJob = map[byte][]initialSkillEntry{0: {{SkillID: 46, Level: 1}}}
	service.initialSPTable = map[int]int{1: 20}
	service.skillCatalog = skillCatalog
	service.questCatalog = buildQuestListTestCatalog(t)
	service.options.accountPrefix = defaultAccountPrefix
	service.adventureGroupTable = loadAdventureGroupTestTables(t)
	questIndex, err := dnfpvf.Build(context.Background(), questListTestSource{
		dnfquest.DefaultList:  "900 `visible.qst`\n901 `active.qst`\n",
		"n_quest/visible.qst": "[grade]\n`[epic]`\n[level]\n1 10\n[exposed by npc]\n1\n",
		"n_quest/active.qst":  "[grade]\n`[epic]`\n[level]\n1 10\n[exposed by npc]\n1\n",
	}, dnfpvf.BuildOptions{Lists: []string{dnfquest.DefaultList}})
	if err != nil {
		t.Fatal(err)
	}
	service.questCatalog, err = dnfquest.Load(context.Background(), questIndex)
	if err != nil {
		t.Fatal(err)
	}
	character, found, err := repositories.Character.Load(context.Background(), "29")
	if err != nil || !found {
		t.Fatalf("load town character found=%t err=%v", found, err)
	}
	character.Name = "return-select-reentry-op14"
	character.Job = "0"
	character.Level = 4
	character.Stats["exp"] = 4948
	character.Stats["gold"] = 12345
	character.Stats[currentDungeonTutorialCompletedKey] = currentDungeonTutorialCompleteFlag
	if err := repositories.Character.Save(context.Background(), character); err != nil {
		t.Fatal(err)
	}
	raw := bytes.Repeat([]byte{0xCD}, currentItemListEntryWireSize)
	if err := repositories.Inventory.Save(context.Background(), dnfrepo.InventoryRecord{
		CharacterID: "29",
		Slots: map[string]dnfrepo.ItemStack{
			"0:3": {ItemID: 3227, Count: 2, RawEntry: raw},
			"0:5": {ItemID: 1033, Count: 1},
		},
		Warehouse: map[string]dnfrepo.ItemStack{},
	}); err != nil {
		t.Fatal(err)
	}
	medalRaw := currentGuardianGemTestRaw(int16(currentGuildMedalActorSlot), 100380017, 1)
	binary.LittleEndian.PutUint16(medalRaw[currentGuardianGemRawSocketOffset:currentGuardianGemRawSocketOffset+currentGuardianGemRawSocketWidth], 3)
	if err := repositories.Equipment.Save(context.Background(), dnfrepo.EquipmentRecord{
		CharacterID: "29",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"32": {SlotIndex: 32, ItemID: 100380017, RawEntry: medalRaw},
		},
	}); err != nil {
		t.Fatal(err)
	}
	session.channel = channelcatalog.Channel{ServerID: 1, ID: 11, Type: 1, Name: "ch.11", Port: 10011}
	session.residentChannel = session.channel
	session.selectedCharacterID = 0
	markReturnSelectTownReentry(session, 29)
	request := make([]byte, 2)
	binary.LittleEndian.PutUint16(request, 29)

	if err := service.sendUpperCSharpSelectInit(session, request); err != nil {
		t.Fatal(err)
	}

	stream := session.conn.(*bufferConn).write.Bytes()
	selectAck, stream := splitGameServerUpperPacket(t, stream)
	if selectAck.Header.Classification != dnfproto.DefaultChannelClassification ||
		selectAck.Header.MsgID != uint16(dnfenum.CmdPacketSelectCharacter) {
		t.Fatalf("select ack=%+v body=%x", selectAck.Header, selectAck.Body)
	}
	stream = requireCurrentStoryDigestLastLevelPacket(t, stream, 0)

	actionTable, stream := splitGameServerUpperPacket(t, stream)
	if actionTable.Header.Classification != 0 ||
		actionTable.Header.MsgID != uint16(dnfenum.CmdPacketPVPMissionHpPercent) ||
		!bytes.Equal(actionTable.Body, buildCurrentActionTableStateBody()) {
		t.Fatalf("action table=%+v body=%x", actionTable.Header, actionTable.Body)
	}
	mode0, stream := splitGameServerUpperPacket(t, stream)
	targetOwner := currentTownActorOwnerContext(session)
	if mode0.Header.Classification != 0 ||
		mode0.Header.MsgID != uint16(dnfenum.CmdPacketSetUDPIPPort) || len(mode0.Body) < 0x4e || mode0.Body[0] != 0 ||
		mode0.Body[3] != currentSceneObjectRoute || mode0.Body[4] != targetOwner {
		t.Fatalf("mode0=%+v body=%x", mode0.Header, mode0.Body)
	}
	preparedState, transition, rest := splitInitialTownPreparedStateBeforeTransition(t, stream)
	rest = requireNoCurrentChannelNoticeAfterTownTransition(t, rest)
	if transition.Header.MsgID != currentSceneTransitionMsgID || len(rest) != 0 {
		t.Fatalf("transition=%+v body=%x rest=%x", transition.Header, transition.Body, rest)
	}
	if session.townPostTransition.stage != currentTownPostTransitionIdle {
		t.Fatalf("return-select post-transition stage=%d want idle", session.townPostTransition.stage)
	}

	fullMode1Index := -1
	op13Count := 0
	op13Types := make([]byte, 0, len(currentSelectInventoryBootstrapListTypes))
	bootstrapLists := make(map[byte]dnfproto.ChannelPacket, len(currentSelectInventoryBootstrapListTypes))
	lastOp13Index := -1
	itemLockSnapshotIndex := -1
	itemLockSnapshotBody := []byte(nil)
	op14Count := 0
	guardianGemOp14Index := -1
	for packetIndex, packetStream := 0, preparedState; len(packetStream) > 0; packetIndex++ {
		packet, packetRest := splitCurrentGameServerUpperPacketAuto(t, packetStream)
		packetStream = packetRest
		switch packet.Header.MsgID {
		case uint16(dnfenum.CmdPacketSetUDPIPPort):
			if len(packet.Body) > 0 && packet.Body[0] == 1 {
				fullMode1Index = packetIndex
			}
		case uint16(dnfenum.CmdPacketLeaveParty):
			op13Count++
			if len(packet.Body) > 0 {
				op13Types = append(op13Types, packet.Body[0])
				bootstrapLists[packet.Body[0]] = packet
			}
			lastOp13Index = packetIndex
		case dnfitemlock.LockListMessageID:
			if packet.Header.Classification != 0 {
				t.Fatalf("item-lock snapshot header=%+v body=%x", packet.Header, packet.Body)
			}
			itemLockSnapshotIndex = packetIndex
			itemLockSnapshotBody = packet.Body
		case uint16(dnfenum.CmdPacketWalkoutPartyMember):
			op14Count++
			if packet.Header.Classification != 0 ||
				len(packet.Body) != 3+currentItemListEntryWireSize ||
				packet.Body[0] != currentSocketListEquipment ||
				binary.LittleEndian.Uint16(packet.Body[1:3]) != 1 ||
				binary.LittleEndian.Uint16(packet.Body[3:5]) != uint16(currentGuildMedalActorSlot) ||
				binary.LittleEndian.Uint32(packet.Body[5:9]) != 100380017 ||
				binary.LittleEndian.Uint16(packet.Body[3+currentGuardianGemRawSocketOffset:3+currentGuardianGemRawSocketOffset+currentGuardianGemRawSocketWidth]) != 3 {
				t.Fatalf("guardian-gem login rehydrate op14=%+v body=%x", packet.Header, packet.Body)
			}
			guardianGemOp14Index = packetIndex
		}
	}
	if fullMode1Index < 0 ||
		op13Count != len(currentSelectInventoryBootstrapListTypes) ||
		!bytes.Equal(op13Types, currentSelectInventoryBootstrapListTypes) ||
		lastOp13Index >= itemLockSnapshotIndex ||
		itemLockSnapshotIndex >= fullMode1Index ||
		!bytes.Equal(itemLockSnapshotBody, []byte{0, 0}) ||
		op14Count != 1 || guardianGemOp14Index <= fullMode1Index {
		t.Fatalf("re-entry inventory placement: full_mode1=%d late_op13=%d lock_snapshot=%d/%x op13_types=%v op14=%d guardian_op14=%d", fullMode1Index, lastOp13Index, itemLockSnapshotIndex, itemLockSnapshotBody, op13Types, op14Count, guardianGemOp14Index)
	}
	list0 := bootstrapLists[dnfrepo.MainInventoryListType].Body
	if len(list0) != 5+3*currentItemListEntryWireSize ||
		int(binary.LittleEndian.Uint16(list0[3:5])) != 3 {
		t.Fatalf("re-entry bootstrap list0 body=%x", list0)
	}
	rows := list0[5:]
	wallet := rows[:currentItemListEntryWireSize]
	if binary.LittleEndian.Uint16(wallet[0:2]) != 0 ||
		binary.LittleEndian.Uint32(wallet[2:6]) != 0 ||
		binary.LittleEndian.Uint32(wallet[6:10]) != 12345 {
		t.Fatalf("re-entry bootstrap gold wallet row=%x", wallet[:0x0E])
	}
	quick := rows[currentItemListEntryWireSize : 2*currentItemListEntryWireSize]
	if binary.LittleEndian.Uint16(quick[0:2]) != 3 ||
		binary.LittleEndian.Uint32(quick[2:6]) != 3227 ||
		binary.LittleEndian.Uint32(quick[6:10]) != 2 {
		t.Fatalf("re-entry bootstrap quick-slot row=%x", quick[:0x0E])
	}
	backpack := rows[2*currentItemListEntryWireSize:]
	if binary.LittleEndian.Uint16(backpack[0:2]) != 5 ||
		binary.LittleEndian.Uint32(backpack[2:6]) != 1033 ||
		binary.LittleEndian.Uint32(backpack[6:10]) != 1 {
		t.Fatalf("re-entry bootstrap backpack row=%x", backpack[:0x0E])
	}
}
