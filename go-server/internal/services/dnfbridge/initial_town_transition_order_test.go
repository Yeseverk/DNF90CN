package dnfbridge

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"

	"longheng.io/server/internal/modules/dnf/channelcatalog"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func TestInitialTownTransitionIsWithheldWhenPlayerStateInitializationFails(t *testing.T) {
	service, session, _ := newTownMoveTest(t)
	session.selectedCharacterID = 29
	service.armCurrentInitialTownRoute(session, 29)

	ctx := context.Background()
	characterID, characterName, character, hasCharacter := service.selectedCharacterForEnter(ctx, session)
	if characterID != 29 || !hasCharacter {
		t.Fatalf("selected character id=%d found=%t", characterID, hasCharacter)
	}
	objectBody := service.buildCurrentSceneObjectListBodyForSession(
		ctx,
		session,
		characterID,
		characterName,
		character,
		hasCharacter,
	)
	objectKey := currentSceneActorObjectKey(characterID)
	mode1Body := buildCurrentActorBindingMode1Body(objectKey, 0)
	transitionBody, err := buildCurrentSceneTransitionBody(1, 0, []currentSceneTransitionRow{{
		ObjectOrResourceKey: objectKey,
		Value1:              474,
		Value2:              234,
		Value3:              5,
		Value4:              3,
	}})
	if err != nil {
		t.Fatal(err)
	}

	wantErr := errors.New("pre-transition player-state write failed")
	connection := &failNthDungeonWriteConn{failAt: 4, err: wantErr}
	session.conn = connection
	session.townMu.Lock()
	err = service.sendCurrentInitialTownActorRoutePacketsLocked(
		session,
		characterID,
		1,
		0,
		currentSceneTransitionRow{
			ObjectOrResourceKey: objectKey,
			Value1:              474,
			Value2:              234,
			Value3:              5,
			Value4:              3,
		},
		objectBody,
		mode1Body,
		transitionBody,
	)
	stage := session.initialTownRouteStage
	session.townMu.Unlock()
	if !errors.Is(err, wantErr) {
		t.Fatalf("initialization failure=%v want=%v", err, wantErr)
	}
	if stage >= currentInitialTownRouteTransitionSent {
		t.Fatalf("failed initialization committed route stage=%d", stage)
	}

	for stream := connection.write.Bytes(); len(stream) > 0; {
		packet, rest := splitCurrentGameServerUpperPacketAuto(t, stream)
		if packetLooksLikeInitialTownTransition(packet) {
			t.Fatalf("failed initialization emitted op24 body=%x", packet.Body)
		}
		stream = rest
	}
}

func TestDeferredSelectSceneTailResumesInitialTownRouteFromHeartbeatWithoutProgress36(t *testing.T) {
	service, session, repositories := newTownMoveTest(t)
	ctx := context.Background()
	skillCatalog, err := buildSkillCatalogFromSource(ctx, initialEquipmentMemSource{
		"skill/skilllist.lst":    "0 `job0.lst`\n",
		"skill/job0.lst":         "46 `job0/initial.skl`\n",
		"skill/job0/initial.skl": "[skill type]\n`active`\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	service.initialSkillsByJob = map[byte][]initialSkillEntry{0: {{SkillID: 46, Level: 1}}}
	service.initialSPTable = map[int]int{90: 3710}
	service.initialTPTable = map[int]int{90: 41}
	service.skillCatalog = skillCatalog
	service.questCatalog = buildQuestListTestCatalog(t)
	service.options.accountPrefix = defaultAccountPrefix
	service.adventureGroupTable = loadAdventureGroupTestTables(t)
	if err := repositories.Account.Save(ctx, dnfrepo.AccountRecord{
		AccountID: defaultAccountPrefix + "1",
		Metadata:  map[string]string{currentRentalPointMetadataKey: "0"},
	}); err != nil {
		t.Fatal(err)
	}
	character, found, err := repositories.Character.Load(ctx, "29")
	if err != nil || !found {
		t.Fatalf("load character found=%t err=%v", found, err)
	}
	character.Level = 90
	character.Stats[currentDungeonTutorialCompletedKey] = currentDungeonTutorialCompleteFlag
	character.Stats[currentOpenAuraSkinSlotFlagStat] = 1
	if err := repositories.Character.Save(ctx, character); err != nil {
		t.Fatal(err)
	}

	session.selectedCharacterID = 29
	session.townSceneReadyCharacterID = 0
	session.sceneBootstrapTailDeferred = true
	session.channel = channelcatalog.Channel{
		ServerID: 1,
		ID:       11,
		Type:     1,
		Name:     "ch.11",
		Port:     10011,
	}
	session.residentChannel = session.channel
	service.armCurrentInitialTownRoute(session, 29)
	connection := session.conn.(*bufferConn)
	connection.write.Reset()

	if err := service.sendDeferredSelectSceneTail(session, "upper_check_user_connection_after_ack"); err != nil {
		t.Fatalf("resume scene tail from heartbeat: %v", err)
	}
	if connection.write.Len() == 0 {
		t.Fatal("heartbeat resume wrote no town route or scene tail")
	}
	fullStream := append([]byte(nil), connection.write.Bytes()...)
	targetOwner := currentTownActorOwnerContext(session)
	mode0Count := 0
	mode1Count := 0
	op9Count := 0
	lastContainerIndex := -1
	auraRestoreCount := 0
	auraRestoreIndex := -1
	adventureInfoIndex := -1
	packetIndex := 0
	for ownerStream := fullStream; len(ownerStream) > 0; {
		packet, next := splitCurrentGameServerUpperPacketAuto(t, ownerStream)
		ownerStream = next
		if packet.Header.Classification == 0 &&
			packet.Header.MsgID == uint16(dnfenum.CmdPacketLeaveParty) {
			lastContainerIndex = packetIndex
		}
		if packet.Header.Classification == dnfproto.DefaultChannelClassification &&
			packet.Header.MsgID == uint16(dnfenum.CmdPacketOpenAuraSkinSlot) {
			auraRestoreCount++
			auraRestoreIndex = packetIndex
			if string(packet.Body) != string([]byte{1, 'A', 'U', 'R', 'A'}) {
				t.Fatalf("cold-login aura restore body=%x want 0141555241", packet.Body)
			}
		}
		if packet.Header.Classification == 0 &&
			packet.Header.MsgID == currentAdventureInfoPushMsgID {
			adventureInfoIndex = packetIndex
		}
		switch {
		case packet.Header.Classification == 0 &&
			packet.Header.MsgID == uint16(dnfenum.CmdPacketSetUDPIPPort) &&
			len(packet.Body) >= 5 &&
			(packet.Body[0] == 0 || packet.Body[0] == 1):
			if packet.Body[3] != 0 || packet.Body[4] != targetOwner {
				t.Fatalf(
					"heartbeat town/deferred mode%d owner=%x want 00/%02x",
					packet.Body[0],
					packet.Body[:5],
					targetOwner,
				)
			}
			if packet.Body[0] == 0 {
				mode0Count++
			} else {
				mode1Count++
			}
		case packet.Header.Classification == 0 &&
			packet.Header.MsgID == uint16(dnfenum.CmdPacketRecoverStamina):
			assertCurrentPartylessTownOp9Body(t, packet.Body, targetOwner)
			op9Count++
		}
		packetIndex++
	}
	if auraRestoreCount != 1 || auraRestoreIndex <= lastContainerIndex ||
		adventureInfoIndex <= auraRestoreIndex {
		t.Fatalf(
			"cold-login aura restore count=%d index=%d containers_end=%d adventure=%d",
			auraRestoreCount,
			auraRestoreIndex,
			lastContainerIndex,
			adventureInfoIndex,
		)
	}
	if mode0Count < 1 || mode1Count < 1 || op9Count < 1 {
		t.Fatalf(
			"heartbeat owner-bearing packets mode0=%d mode1=%d op9=%d, want all present",
			mode0Count,
			mode1Count,
			op9Count,
		)
	}

	_, transition, rest := splitInitialTownPreparedStateBeforeTransition(t, fullStream)
	if transition.Header.Classification != 0 || transition.Header.MsgID != currentSceneTransitionMsgID {
		t.Fatalf("transition header=%+v", transition.Header)
	}
	rest = requireNoCurrentChannelNoticeAfterTownTransition(t, rest)
	if len(rest) != 0 {
		t.Fatalf("heartbeat resumed deferred tail before type1345: %x", rest)
	}
	if session.townPostTransition.stage != currentTownPostTransitionIdle {
		t.Fatalf("heartbeat actor tail post-transition stage=%d want idle before type1345", session.townPostTransition.stage)
	}
	if session.initialTownRouteStage != currentInitialTownRoutePlayerStateSent ||
		session.sceneBootstrapTailSent || !session.sceneBootstrapTailDeferred ||
		!session.selectedUserInfoRefreshSent || session.selectedUserInfoMode3Sent {
		t.Fatalf("route/tail flags stage=%d tail_sent=%t tail_deferred=%t mode1=%t mode3_sent=%t",
			session.initialTownRouteStage,
			session.sceneBootstrapTailSent,
			session.sceneBootstrapTailDeferred,
			session.selectedUserInfoRefreshSent,
			session.selectedUserInfoMode3Sent)
	}

	connection.write.Reset()
	if err := service.handleLegacyGamePacket(session, buildLegacyTownSceneReadyAckPacket()); err != nil {
		t.Fatalf("type1345 HUD boundary: %v", err)
	}
	playerState, gotInitialHUDSkillBody, type1345Trailing := splitInitialTownType1345PlayerState(
		t,
		connection.write.Bytes(),
		targetOwner,
	)
	if len(playerState) != 0 || len(gotInitialHUDSkillBody) != 0 {
		t.Fatalf(
			"type1345 player state=%x skill=%x want no actor/skill replay",
			playerState,
			gotInitialHUDSkillBody,
		)
	}
	sceneTail := type1345Trailing
	if len(sceneTail) == 0 {
		t.Fatal("type1345 did not resume the existing deferred scene tail")
	}
	rentalStates := 0
	for stream := sceneTail; len(stream) > 0; {
		packet, next := splitCurrentGameServerUpperPacketAuto(t, stream)
		stream = next
		if packet.Header.Classification != 0 {
			continue
		}
		switch packet.Header.MsgID {
		case currentRentalStateMsgID:
			rentalStates++
		case uint16(dnfenum.CmdPacketSetUDPIPPort),
			currentCreatureStateTableMsgID,
			currentCreatureGrowthMsgID,
			uint16(dnfenum.CmdPacketFinishLoading),
			currentIncreaseStatusResultMsgID,
			uint16(dnfenum.CmdPacketRequestBlacklist):
			t.Fatalf(
				"type1345 deferred tail rebuilt player state msg_id=%d body=%x",
				packet.Header.MsgID,
				packet.Body,
			)
		}
	}
	if rentalStates != 1 {
		t.Fatalf("type1345 deferred tail rental=%d want=1", rentalStates)
	}
	if session.townPostTransition.stage != currentTownPostTransitionIdle {
		t.Fatalf("type1345 post-transition stage=%d want idle", session.townPostTransition.stage)
	}
	if !session.sceneBootstrapTailSent || session.sceneBootstrapTailDeferred {
		t.Fatalf("type1345 tail sent=%t deferred=%t", session.sceneBootstrapTailSent, session.sceneBootstrapTailDeferred)
	}
	firstHUDWriteLen := connection.write.Len()
	if err := service.handleLegacyGamePacket(session, buildLegacyTownSceneReadyAckPacket()); err != nil {
		t.Fatalf("duplicate type1345 HUD boundary: %v", err)
	}
	if connection.write.Len() != firstHUDWriteLen {
		t.Fatalf("duplicate type1345 grew HUD stream from %d to %d", firstHUDWriteLen, connection.write.Len())
	}
}

func requireNoCurrentChannelNoticeAfterTownTransition(t *testing.T, stream []byte) []byte {
	t.Helper()
	for remaining := stream; len(remaining) > 0; {
		packet, rest := splitCurrentGameServerUpperPacketAuto(t, remaining)
		remaining = rest
		if packet.Header.Classification == 0 &&
			packet.Header.MsgID == uint16(dnfenum.UpperMsgGameEndpoint) {
			t.Fatalf(
				"town route emitted class0/op1 after endpoint initialization body=%x",
				packet.Body,
			)
		}
	}
	return stream
}

func assertNoCurrentChannelNoticeInTownRoute(t *testing.T, stream []byte) {
	t.Helper()
	op24Index := -1
	for index := 0; len(stream) > 0; index++ {
		packet, rest := splitCurrentGameServerUpperPacketAuto(t, stream)
		stream = rest
		if packetLooksLikeInitialTownTransition(packet) {
			if op24Index >= 0 {
				t.Fatalf("ordinary town route emitted multiple typed op24 packets")
			}
			op24Index = index
		}
		if packet.Header.Classification == 0 &&
			packet.Header.MsgID == uint16(dnfenum.UpperMsgGameEndpoint) {
			t.Fatalf(
				"ordinary town route emitted class0/op1 at packet %d after endpoint initialization body=%x",
				index,
				packet.Body,
			)
		}
	}
	if op24Index < 0 {
		t.Fatal("ordinary town route emitted no typed op24")
	}
}

func countCurrentActorModePackets(t *testing.T, stream []byte, mode byte) int {
	t.Helper()
	count := 0
	for len(stream) > 0 {
		packet, rest := splitCurrentGameServerUpperPacketAuto(t, stream)
		stream = rest
		if packet.Header.Classification == 0 &&
			packet.Header.MsgID == uint16(dnfenum.CmdPacketSetUDPIPPort) &&
			len(packet.Body) > 0 &&
			packet.Body[0] == mode {
			count++
		}
	}
	return count
}

func packetLooksLikeInitialTownTransition(packet dnfproto.ChannelPacket) bool {
	if packet.Header.Classification != 0 || packet.Header.MsgID != currentSceneTransitionMsgID {
		return false
	}
	if len(packet.Body) < 4 || (len(packet.Body)-4)%8 != 0 {
		return false
	}
	rowCount := int(binary.LittleEndian.Uint16(packet.Body[2:4]))
	if len(packet.Body) != 4+rowCount*8 {
		return false
	}
	return packet.Body[0] != 0 || packet.Body[1] != 0
}

func splitInitialTownPreparedStateBeforeTransition(
	t *testing.T,
	stream []byte,
) ([]byte, dnfproto.ChannelPacket, []byte) {
	t.Helper()
	return splitInitialTownPreparedStateBeforeTransitionWithPolicies(t, stream, len(currentSelectInventoryBootstrapListTypes), true)
}

func splitInitialTownPreparedStateBeforeTransitionWithoutFullMode1(
	t *testing.T,
	stream []byte,
) ([]byte, dnfproto.ChannelPacket, []byte) {
	t.Helper()
	return splitInitialTownPreparedStateBeforeTransitionWithPolicies(t, stream, len(currentSelectInventoryBootstrapListTypes), false)
}

func splitInitialTownPreparedStateBeforeTransitionWithOp13Policy(
	t *testing.T,
	stream []byte,
	wantContainerLists int,
) ([]byte, dnfproto.ChannelPacket, []byte) {
	t.Helper()
	return splitInitialTownPreparedStateBeforeTransitionWithPolicies(t, stream, wantContainerLists, true)
}

func splitInitialTownPreparedStateBeforeTransitionWithPolicies(
	t *testing.T,
	stream []byte,
	wantContainerLists int,
	wantFullMode1 bool,
) ([]byte, dnfproto.ChannelPacket, []byte) {
	t.Helper()
	original := stream
	consumed := 0
	overseerPages := 0
	containerLists := 0
	lastContainerListIndex := -1
	containerListTypeCap := wantContainerLists
	if containerListTypeCap < 0 {
		containerListTypeCap = 1
	}
	containerListTypes := make([]byte, 0, containerListTypeCap)
	actionTable := false
	cargoReset := false
	adventureInfo := false
	adventureInfoIndex := -1
	crystalStateCount := 0
	crystalStateIndex := -1
	fullMode1 := false
	fullMode1AfterContainers := false
	mode3 := false
	skillInfoBeforeTransition := 0
	lastFullMode1Index := -1
	creatureStateCount := 0
	creatureStateIndex := -1
	creatureItemRefreshCount := 0
	creatureItemRefreshIndex := -1
	guardianGemMedalRefreshCount := 0
	guardianGemMedalRefreshIndex := -1
	creatureGrowthCount := 0
	creatureGrowthIndex := -1
	skillInfoIndex := -1
	acceptableQuestCount := 0
	acceptableQuestIndex := -1
	activeQuestCount := 0
	activeQuestIndex := -1
	adventureActor := false
	insertOverseer := false
	passGate := false
	clientSpec := false
	actorDisplay := false
	actorPlacement := false
	actorSceneSnapshot := false
	userPosition := false
	userArea := false
	var msgIDs []uint16

	for len(stream) > 0 {
		packet, rest := splitCurrentGameServerUpperPacketAuto(t, stream)
		packetBytes := len(stream) - len(rest)
		if packetLooksLikeInitialTownTransition(packet) {
			containerListPolicyOK := containerLists == wantContainerLists
			if wantContainerLists < 0 {
				containerListPolicyOK = containerLists == 0 ||
					(containerLists == 1 && len(containerListTypes) == 1 && containerListTypes[0] == currentPetInventoryListType)
			}
			preTransitionOrderOK := creatureStateCount == 1 &&
				skillInfoBeforeTransition == 1 &&
				acceptableQuestCount == 1 &&
				activeQuestCount == 1 &&
				creatureStateIndex >= 0 &&
				skillInfoIndex > creatureStateIndex &&
				acceptableQuestIndex > skillInfoIndex &&
				activeQuestIndex > acceptableQuestIndex
			if lastFullMode1Index >= 0 {
				preTransitionOrderOK = preTransitionOrderOK &&
					creatureStateIndex > lastFullMode1Index
			}
			if creatureItemRefreshCount > 0 {
				preTransitionOrderOK = preTransitionOrderOK &&
					creatureItemRefreshCount == 1 &&
					creatureItemRefreshIndex > lastFullMode1Index &&
					creatureStateIndex > creatureItemRefreshIndex
			}
			if guardianGemMedalRefreshCount > 0 {
				preTransitionOrderOK = preTransitionOrderOK &&
					guardianGemMedalRefreshCount == 1 &&
					(lastFullMode1Index < 0 || guardianGemMedalRefreshIndex > lastFullMode1Index) &&
					creatureStateIndex > guardianGemMedalRefreshIndex
			}
			if creatureGrowthCount > 0 {
				preTransitionOrderOK = preTransitionOrderOK &&
					creatureGrowthCount == 1 &&
					creatureGrowthIndex > creatureStateIndex &&
					skillInfoIndex > creatureGrowthIndex
			}
			crystalOrderOK := crystalStateCount == 1 &&
				crystalStateIndex > lastContainerListIndex &&
				adventureInfoIndex > crystalStateIndex
			if lastFullMode1Index >= 0 {
				crystalOrderOK = crystalOrderOK && lastFullMode1Index > crystalStateIndex
			}
			if overseerPages != 5 ||
				!containerListPolicyOK || !actionTable || !cargoReset || !adventureInfo || fullMode1AfterContainers != wantFullMode1 ||
				mode3 || !preTransitionOrderOK || !crystalOrderOK ||
				!adventureActor || !insertOverseer || !passGate ||
				!clientSpec || !actorDisplay || !actorPlacement || !actorSceneSnapshot || !userPosition || !userArea {
				t.Fatalf(
					"op24 town initialization mismatch: pages=%d late_lists=%d late_list_types=%v action=%t cargo=%t crystal=%d@%d crystal_order=%t adventure=%t@%d mode1=%t mode3_sent=%t op105=%d@%d op102=%d@%d guardian_op14=%d@%d skill_preop24=%d@%d op21=%d@%d op574=%d@%d final_mode1=%d order_ok=%t actor=%t op359=%t op356=%t op124=%t op9=%t op120=%t noti320=%t noti16=%t noti17=%t msg_ids=%v",
					overseerPages,
					containerLists,
					containerListTypes,
					actionTable,
					cargoReset,
					crystalStateCount,
					crystalStateIndex,
					crystalOrderOK,
					adventureInfo,
					adventureInfoIndex,
					fullMode1,
					mode3,
					creatureStateCount,
					creatureStateIndex,
					creatureGrowthCount,
					creatureGrowthIndex,
					guardianGemMedalRefreshCount,
					guardianGemMedalRefreshIndex,
					skillInfoBeforeTransition,
					skillInfoIndex,
					acceptableQuestCount,
					acceptableQuestIndex,
					activeQuestCount,
					activeQuestIndex,
					lastFullMode1Index,
					preTransitionOrderOK,
					adventureActor,
					insertOverseer,
					passGate,
					clientSpec,
					actorDisplay,
					actorPlacement,
					actorSceneSnapshot,
					userPosition,
					userArea,
					msgIDs,
				)
			}
			return append([]byte(nil), original[:consumed]...), packet, rest
		}

		packetIndex := len(msgIDs)
		msgIDs = append(msgIDs, packet.Header.MsgID)
		switch packet.Header.MsgID {
		case uint16(dnfenum.CmdPacketRequestOverseer):
			overseerPages++
		case uint16(dnfenum.CmdPacketPVPMissionHpPercent):
			actionTable = true
		case uint16(dnfenum.CmdPacketCancelCargoPad):
			cargoReset = true
		case uint16(dnfenum.CmdPacketLeaveParty):
			if !cargoReset {
				t.Fatalf("initial town op13 preceded cargo reset: body=%x", packet.Body)
			}
			if wantContainerLists < 0 || wantContainerLists == 1 {
				if len(packet.Body) == 0 || packet.Body[0] != currentPetInventoryListType {
					t.Fatalf("initial town late op13 body=%x: only list7 pet-scene rehydrate is allowed before op24", packet.Body)
				}
			} else if containerLists >= len(currentSelectInventoryBootstrapListTypes) ||
				len(packet.Body) == 0 ||
				packet.Body[0] != currentSelectInventoryBootstrapListTypes[containerLists] {
				t.Fatalf(
					"initial town actor-bound op13[%d] body=%x want list%d",
					containerLists,
					packet.Body,
					currentSelectInventoryBootstrapListTypes[containerLists],
				)
			}
			containerLists++
			lastContainerListIndex = packetIndex
			if len(packet.Body) > 0 {
				containerListTypes = append(containerListTypes, packet.Body[0])
			}
		case currentCrystalContractStateMsgID:
			if packet.Header.Classification != 0 || len(packet.Body) != 2 || packet.Body[0] != 0 {
				t.Fatalf("initial town crystal state header=%+v body=%x, want class0/op898 00+selection", packet.Header, packet.Body)
			}
			crystalStateCount++
			crystalStateIndex = packetIndex
		case currentAdventureInfoPushMsgID:
			adventureInfo = true
			adventureInfoIndex = packetIndex
		case uint16(dnfenum.CmdPacketSetUDPIPPort):
			if len(packet.Body) > 0 && packet.Body[0] == 1 {
				fullMode1 = true
				lastFullMode1Index = packetIndex
				if containerLists == wantContainerLists {
					fullMode1AfterContainers = true
				}
			}
			if len(packet.Body) > 0 && packet.Body[0] == 3 {
				mode3 = true
			}
		case currentCreatureStateTableMsgID:
			creatureStateCount++
			creatureStateIndex = packetIndex
		case currentCreatureGrowthMsgID:
			creatureGrowthCount++
			creatureGrowthIndex = packetIndex
		case currentSkillInfoMsgID:
			skillInfoBeforeTransition++
			skillInfoIndex = packetIndex
		case currentAcceptableQuestListMsgID:
			acceptableQuestCount++
			acceptableQuestIndex = packetIndex
		case currentActiveQuestSnapshotMsgID:
			activeQuestCount++
			activeQuestIndex = packetIndex
		case uint16(dnfenum.CmdPacketWalkoutPartyMember):
			if packet.Header.Classification != 0 ||
				len(packet.Body) != 3+currentItemListEntryWireSize ||
				packet.Body[0] != currentSocketListEquipment ||
				binary.LittleEndian.Uint16(packet.Body[1:3]) != 1 {
				t.Fatalf("initial town emitted malformed acquisition-style op14 body=%x", packet.Body)
			}
			switch binary.LittleEndian.Uint16(packet.Body[3:5]) {
			case 26:
				creatureItemRefreshCount++
				creatureItemRefreshIndex = packetIndex
			case uint16(currentGuildMedalActorSlot):
				if !currentGuardianGemRawSocketOccupied(packet.Body[3:]) {
					t.Fatalf("initial town guardian-medal op14 omitted raw socket state body=%x", packet.Body)
				}
				guardianGemMedalRefreshCount++
				guardianGemMedalRefreshIndex = packetIndex
			default:
				t.Fatalf("initial town emitted unsupported acquisition-style op14 slot=%d body=%x", binary.LittleEndian.Uint16(packet.Body[3:5]), packet.Body)
			}
		case currentAdventureActorRefreshMsgID:
			adventureActor = true
		case uint16(dnfenum.CmdPacketInsertOverseer):
			if len(packet.Body) < 4+currentInsertOverseerTailWireSize ||
				(len(packet.Body)-4-currentInsertOverseerTailWireSize)%currentInsertOverseerRowWireSize != 0 {
				t.Fatalf("op359 malformed current body len=%d body=%x", len(packet.Body), packet.Body)
			}
			rowCount := int(binary.LittleEndian.Uint32(packet.Body[:4]))
			if len(packet.Body) != 4+rowCount*currentInsertOverseerRowWireSize+currentInsertOverseerTailWireSize {
				t.Fatalf("op359 row count=%d len=%d body=%x", rowCount, len(packet.Body), packet.Body)
			}
			insertOverseer = true
		case uint16(dnfenum.CmdPacketPassGateObject):
			passGate = true
		case uint16(dnfenum.CmdPacketReportClientSpec):
			clientSpec = true
		case uint16(dnfenum.CmdPacketRecoverStamina):
			actorDisplay = true
		case uint16(dnfenum.CmdPacketRequestBlacklist):
			actorPlacement = true
		case currentTownActorSceneSnapshotMsgID:
			if len(packet.Body) != 4 || packet.Body[0] != 1 ||
				binary.LittleEndian.Uint16(packet.Body[1:3]) == 0 || packet.Body[3] != 0 {
				t.Fatalf("noti0x320=%x: want one existing actor with zero children", packet.Body)
			}
			actorSceneSnapshot = true
		case currentTownUserPositionNotificationMsgID:
			userPosition = true
		case currentTownUserAreaNotificationMsgID:
			userArea = true
		}
		consumed += packetBytes
		stream = rest
	}

	t.Fatal("initial town stream ended before typed op24 transition")
	return nil, dnfproto.ChannelPacket{}, nil
}

func splitInitialTownType1345PlayerState(
	t *testing.T,
	stream []byte,
	_ byte,
) ([]byte, []byte, []byte) {
	t.Helper()
	var skillBody []byte
	trailing := make([]byte, 0, len(stream))
	for remaining := stream; len(remaining) > 0; {
		packetWire := remaining
		packet, rest := splitCurrentGameServerUpperPacketAuto(t, remaining)
		wireLen := len(remaining) - len(rest)
		remaining = rest
		if skillBody != nil {
			t.Fatalf(
				"type1345 sent packet after retained skill msg_id=%d class=%d body=%x",
				packet.Header.MsgID,
				packet.Header.Classification,
				packet.Body,
			)
		}
		if packet.Header.Classification != 0 {
			trailing = append(trailing, packetWire[:wireLen]...)
			continue
		}
		switch packet.Header.MsgID {
		case uint16(dnfenum.CmdPacketSetUDPIPPort),
			currentCreatureStateTableMsgID,
			currentCreatureGrowthMsgID,
			uint16(dnfenum.CmdPacketFinishLoading),
			currentIncreaseStatusResultMsgID,
			uint16(dnfenum.CmdPacketRequestBlacklist):
			t.Fatalf(
				"type1345 rebuilt actor/HUD state msg_id=%d body=%x",
				packet.Header.MsgID,
				packet.Body,
			)
		case currentSkillInfoMsgID:
			if skillBody != nil {
				t.Fatalf("type1345 sent duplicate skill projections first=%x second=%x", skillBody, packet.Body)
			}
			skillBody = append([]byte(nil), packet.Body...)
		default:
			trailing = append(trailing, packetWire[:wireLen]...)
		}
	}
	return nil, skillBody, trailing
}
