package dnfbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"
	"time"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	"longheng.io/server/internal/modules/dnf/premium"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func newCrystalContractTestRuntime(
	t *testing.T,
	slots map[string]dnfrepo.ItemStack,
) (*Service, *gameSession, dnfrepo.Group, *bufferConn) {
	t.Helper()
	service, session, repositories, connection := newPremiumServiceTestRuntime(t)
	account, found, err := repositories.Account.Load(context.Background(), "account-1")
	if err != nil || !found {
		t.Fatalf("load account found=%t err=%v", found, err)
	}
	premium.Upsert(&account, premium.TypeCrystal, int64(time.Hour/time.Second), 1, time.Now().UTC())
	if err := repositories.Account.Save(context.Background(), account); err != nil {
		t.Fatal(err)
	}
	if err := repositories.AccountInventory.Save(context.Background(), dnfrepo.AccountInventoryRecord{
		AccountID: "account-1",
		Slots:     slots,
	}); err != nil {
		t.Fatal(err)
	}
	service.premiumCatalog = &currentPremiumCatalog{
		contractsByItem: make(map[int64]currentPremiumContractInfo),
		devilSlots:      make(map[uint32]currentPremiumDevilSlotInfo),
		crystalCubeIDs:  [6]int64{3033, 3034, 3035, 3036, 3037, 3262},
	}
	return service, session, repositories, connection
}

func nextCrystalPacket(t *testing.T, data []byte) (dnfproto.ChannelPacket, []byte) {
	t.Helper()
	return splitGameServerUpperPacket(t, data)
}

func assertCurrentCrystalStatePacket(t *testing.T, packet dnfproto.ChannelPacket, wantBody []byte) {
	t.Helper()
	if packet.Header.Classification != 0 ||
		packet.Header.MsgID != currentCrystalContractStateMsgID ||
		!bytes.Equal(packet.Body, wantBody) {
		t.Fatalf("crystal state header=%+v body=%x, want class0/op898 body=%x", packet.Header, packet.Body, wantBody)
	}
}

func TestCurrentCrystalContractSelectionSendsOp535AckThenOp898State(t *testing.T) {
	service, session, _, connection := newCrystalContractTestRuntime(t, map[string]dnfrepo.ItemStack{
		"0:358": {ItemID: 3037, Count: 12},
	})
	if err := service.handleCurrentCrystalContractUpdate(session, []byte{0, 4}); err != nil {
		t.Fatal(err)
	}
	ack, rest := nextCrystalPacket(t, connection.write.Bytes())
	if ack.Header.Classification != dnfproto.DefaultChannelClassification ||
		ack.Header.MsgID != uint16(dnfenum.CmdPacketUpdateContractOfCubeInfo) ||
		!bytes.Equal(ack.Body, []byte{1, 0, 4}) {
		t.Fatalf("op535 packet header=%+v body=%x", ack.Header, ack.Body)
	}
	state, trailing := nextCrystalPacket(t, rest)
	assertCurrentCrystalStatePacket(t, state, []byte{0, 4})
	if len(trailing) != 0 {
		t.Fatalf("op898 packet header=%+v body=%x trailing=%x", state.Header, state.Body, trailing)
	}
}

func TestLegacyCrystalContractSelectionPersistsAndRestoresAfterRelog(t *testing.T) {
	service, session, _, connection := newCrystalContractTestRuntime(t, map[string]dnfrepo.ItemStack{
		"0:357": {ItemID: 3036, Count: 12},
	})
	if err := service.handleGameCommand(
		session,
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.CmdPacketUpdateContractOfCubeInfo),
		[]byte{0, 3},
	); err != nil {
		t.Fatal(err)
	}
	ack, rest := nextCrystalPacket(t, connection.write.Bytes())
	if ack.Header.Classification != dnfproto.DefaultChannelClassification ||
		ack.Header.MsgID != uint16(dnfenum.CmdPacketUpdateContractOfCubeInfo) ||
		!bytes.Equal(ack.Body, []byte{1, 0, 3}) {
		t.Fatalf("legacy op535 packet header=%+v body=%x", ack.Header, ack.Body)
	}
	state, trailing := nextCrystalPacket(t, rest)
	assertCurrentCrystalStatePacket(t, state, []byte{0, 3})
	if len(trailing) != 0 {
		t.Fatalf("legacy op898 packet header=%+v body=%x trailing=%x", state.Header, state.Body, trailing)
	}

	connection.write.Reset()
	relogged := &gameSession{
		conn:                connection,
		connID:              "crystal-contract-relogged",
		selectedCharacterID: session.selectedCharacterID,
		accountID:           session.accountID,
	}
	if err := service.sendCurrentCrystalContractState(relogged, "test_relog"); err != nil {
		t.Fatal(err)
	}
	restored, trailing := nextCrystalPacket(t, connection.write.Bytes())
	assertCurrentCrystalStatePacket(t, restored, []byte{0, 3})
	if len(trailing) != 0 {
		t.Fatalf("restored op898 packet header=%+v body=%x trailing=%x", restored.Header, restored.Body, trailing)
	}
}

func TestCurrentCrystalContractInitialBootstrapSendsOnceAndOp36IsFallbackPerTownRoute(t *testing.T) {
	service, session, _, connection := newCrystalContractTestRuntime(t, map[string]dnfrepo.ItemStack{
		"0:357": {ItemID: 3036, Count: 12},
	})
	if err := service.sendCurrentCrystalContractStateOnce(
		session,
		"test_after_inventory_bootstrap_before_adventure_mode1",
	); err != nil {
		t.Fatal(err)
	}
	if err := service.sendCurrentCrystalContractTownUIReadyState(session); err != nil {
		t.Fatal(err)
	}
	state, trailing := nextCrystalPacket(t, connection.write.Bytes())
	assertCurrentCrystalStatePacket(t, state, []byte{0, 0xff})
	if len(trailing) != 0 {
		t.Fatalf("town UI-ready state header=%+v body=%x trailing=%x", state.Header, state.Body, trailing)
	}

	connection.write.Reset()
	service.armCurrentInitialTownRoute(session, session.selectedCharacterID)
	if err := service.sendCurrentCrystalContractTownUIReadyState(session); err != nil {
		t.Fatal(err)
	}
	rearmed, trailing := nextCrystalPacket(t, connection.write.Bytes())
	assertCurrentCrystalStatePacket(t, rearmed, []byte{0, 0xff})
	if len(trailing) != 0 {
		t.Fatalf("rearmed town UI-ready state header=%+v body=%x trailing=%x", rearmed.Header, rearmed.Body, trailing)
	}
}

func TestDeferredSelectSceneTailLeavesCrystalStateForTownUIReadyBoundary(t *testing.T) {
	service, session, _, connection := newCrystalContractTestRuntime(t, map[string]dnfrepo.ItemStack{
		"0:358": {ItemID: 3037, Count: 12},
	})
	session.sceneBootstrapTailDeferred = true
	session.initialTownRouteCharacterID = session.selectedCharacterID
	session.initialTownRouteStage = currentInitialTownRoutePlayerStateSent
	session.initialTownLegacySceneReadyAccepted = true

	if err := service.sendDeferredSelectSceneTail(session, "test_scene_ready"); err != nil {
		t.Fatal(err)
	}
	if !session.sceneBootstrapTailSent || session.sceneBootstrapTailDeferred {
		t.Fatalf("scene tail flags sent=%t deferred=%t", session.sceneBootstrapTailSent, session.sceneBootstrapTailDeferred)
	}

	premiumCount := 0
	crystalCount := 0
	for stream := connection.write.Bytes(); len(stream) > 0; {
		packet, rest := splitCurrentGameServerUpperPacketAuto(t, stream)
		stream = rest
		switch {
		case packet.Header.Classification == dnfproto.DefaultChannelClassification &&
			packet.Header.MsgID == uint16(dnfenum.CmdPacketPremiumService):
			premiumCount++
		case packet.Header.Classification == 0 &&
			packet.Header.MsgID == currentCrystalContractStateMsgID:
			crystalCount++
		}
	}
	if premiumCount != 0 || crystalCount != 0 {
		t.Fatalf("scene tail premium/crystal packets=%d/%d, want 0/0", premiumCount, crystalCount)
	}

	connection.write.Reset()
	if err := service.sendCurrentCrystalContractTownUIReadyState(session); err != nil {
		t.Fatal(err)
	}
	state, trailing := nextCrystalPacket(t, connection.write.Bytes())
	assertCurrentCrystalStatePacket(t, state, []byte{0, 0xff})
	if len(trailing) != 0 {
		t.Fatalf("town UI-ready state header=%+v body=%x trailing=%x", state.Header, state.Body, trailing)
	}
}

func TestCurrentCrystalContractCubeUseConsumesOneAndBuildsNativeOp338Result(t *testing.T) {
	service, session, repositories, connection := newCrystalContractTestRuntime(t, map[string]dnfrepo.ItemStack{
		"0:354": {ItemID: 3033, Count: 2},
	})
	if err := service.handleCurrentCrystalContractUpdate(session, []byte{0, 0}); err != nil {
		t.Fatal(err)
	}
	connection.write.Reset()
	session.dungeon.runtime = &runtimeDungeonState{
		Character: dnfrepo.CharacterRecord{CharacterID: "19"},
	}
	request := make([]byte, currentCrystalContractConsumeBodySize)
	binary.LittleEndian.PutUint16(request[0:2], 354)
	binary.LittleEndian.PutUint32(request[2:6], 3033)
	binary.LittleEndian.PutUint16(request[6:8], 2)
	if err := service.handleCurrentCrystalContractCubeUse(session, request); err != nil {
		t.Fatal(err)
	}
	packet, trailing := nextCrystalPacket(t, connection.write.Bytes())
	if packet.Header.Classification != dnfproto.DefaultChannelClassification ||
		packet.Header.MsgID != uint16(dnfenum.CmdPacketUseLimitCube) || len(trailing) != 0 {
		t.Fatalf("op338 header=%+v trailing=%x", packet.Header, trailing)
	}
	wantBody := make([]byte, 12)
	wantBody[0] = 1
	binary.LittleEndian.PutUint32(wantBody[1:5], 3033)
	binary.LittleEndian.PutUint16(wantBody[5:7], 1)
	binary.LittleEndian.PutUint32(wantBody[7:11], 1)
	wantBody[11] = 1
	if !bytes.Equal(packet.Body, wantBody) {
		t.Fatalf("op338 body=%x, want %x", packet.Body, wantBody)
	}
	inventory, found, err := repositories.AccountInventory.Load(context.Background(), "account-1")
	if err != nil || !found {
		t.Fatalf("load account inventory found=%t err=%v", found, err)
	}
	if got := inventory.Slots["0:354"].Count; got != 1 {
		t.Fatalf("cube count=%d, want 1", got)
	}
}

func TestCurrentCrystalContractLastCubePushesDeselectedState(t *testing.T) {
	service, session, _, connection := newCrystalContractTestRuntime(t, map[string]dnfrepo.ItemStack{
		"0:355": {ItemID: 3034, Count: 1},
	})
	if err := service.handleCurrentCrystalContractUpdate(session, []byte{0, 1}); err != nil {
		t.Fatal(err)
	}
	connection.write.Reset()
	session.dungeon.runtime = &runtimeDungeonState{
		Character: dnfrepo.CharacterRecord{CharacterID: "19"},
	}
	request := make([]byte, currentCrystalContractConsumeBodySize)
	binary.LittleEndian.PutUint16(request[0:2], 355)
	binary.LittleEndian.PutUint32(request[2:6], 3034)
	binary.LittleEndian.PutUint16(request[6:8], 1)
	if err := service.handleCurrentCrystalContractCubeUse(session, request); err != nil {
		t.Fatal(err)
	}
	usePacket, rest := nextCrystalPacket(t, connection.write.Bytes())
	if usePacket.Header.Classification != dnfproto.DefaultChannelClassification ||
		usePacket.Header.MsgID != uint16(dnfenum.CmdPacketUseLimitCube) {
		t.Fatalf("first packet=%+v", usePacket.Header)
	}
	statePacket, trailing := nextCrystalPacket(t, rest)
	assertCurrentCrystalStatePacket(t, statePacket, []byte{0, 0xff})
	if len(trailing) != 0 {
		t.Fatalf("last-cube state header=%+v body=%x trailing=%x", statePacket.Header, statePacket.Body, trailing)
	}
}

func TestCurrentCrystalContractCubeUseRejectsTownAndWrongCubeWithoutMutation(t *testing.T) {
	service, session, repositories, connection := newCrystalContractTestRuntime(t, map[string]dnfrepo.ItemStack{
		"0:354": {ItemID: 3033, Count: 4},
		"0:355": {ItemID: 3034, Count: 4},
	})
	if err := service.handleCurrentCrystalContractUpdate(session, []byte{0, 0}); err != nil {
		t.Fatal(err)
	}
	connection.write.Reset()
	request := make([]byte, currentCrystalContractConsumeBodySize)
	binary.LittleEndian.PutUint16(request[0:2], 355)
	binary.LittleEndian.PutUint32(request[2:6], 3034)
	binary.LittleEndian.PutUint16(request[6:8], 4)

	if err := service.handleCurrentCrystalContractCubeUse(session, request); err != nil {
		t.Fatal(err)
	}
	townFailure, trailing := nextCrystalPacket(t, connection.write.Bytes())
	if townFailure.Header.Classification != dnfproto.DefaultChannelClassification ||
		townFailure.Header.MsgID != uint16(dnfenum.CmdPacketUseLimitCube) ||
		!bytes.Equal(townFailure.Body, []byte{0, 1}) ||
		len(trailing) != 0 {
		t.Fatalf("town failure header=%+v body=%x trailing=%x", townFailure.Header, townFailure.Body, trailing)
	}

	connection.write.Reset()
	session.dungeon.runtime = &runtimeDungeonState{
		Character: dnfrepo.CharacterRecord{CharacterID: "19"},
	}
	if err := service.handleCurrentCrystalContractCubeUse(session, request); err != nil {
		t.Fatal(err)
	}
	wrongFailure, trailing := nextCrystalPacket(t, connection.write.Bytes())
	if !bytes.Equal(wrongFailure.Body, []byte{0, 1}) || len(trailing) != 0 {
		t.Fatalf("wrong-cube failure body=%x trailing=%x", wrongFailure.Body, trailing)
	}
	inventory, found, err := repositories.AccountInventory.Load(context.Background(), "account-1")
	if err != nil || !found {
		t.Fatalf("load account inventory found=%t err=%v", found, err)
	}
	if got := inventory.Slots["0:355"].Count; got != 4 {
		t.Fatalf("rejected cube count=%d, want 4", got)
	}
}

func TestTownAreaReadyReplaysPersistedCrystalSelectionWithoutReplacingMainInventory(t *testing.T) {
	service, session, repositories := newTownMoveTest(t)
	session.accountID = "dnf:1"
	service.options.accountID = "dnf:1"
	service.premiumCatalog = &currentPremiumCatalog{
		contractsByItem: make(map[int64]currentPremiumContractInfo),
		devilSlots:      make(map[uint32]currentPremiumDevilSlotInfo),
		crystalCubeIDs:  [6]int64{3033, 3034, 3035, 3036, 3037, 3262},
	}
	now := time.Now().UTC()
	account := dnfrepo.AccountRecord{
		AccountID: "dnf:1",
		Metadata:  make(map[string]string),
	}
	premium.Upsert(&account, premium.TypeCrystal, int64(time.Hour/time.Second), 1, now)
	if err := repositories.Account.Save(context.Background(), account); err != nil {
		t.Fatal(err)
	}
	if err := repositories.AccountInventory.Save(context.Background(), dnfrepo.AccountInventoryRecord{
		AccountID: "dnf:1",
		Slots: map[string]dnfrepo.ItemStack{
			"0:358": {ItemID: 3037, Count: 257},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(context.Background(), dnfrepo.InventoryRecord{
		CharacterID: "29",
		Slots:       map[string]dnfrepo.ItemStack{},
	}); err != nil {
		t.Fatal(err)
	}
	character, found, err := repositories.Character.Load(context.Background(), "29")
	if err != nil || !found {
		t.Fatalf("load character found=%t err=%v", found, err)
	}
	character.Stats["premium_crystal_selection"] = 4
	if err := repositories.Character.Save(context.Background(), character); err != nil {
		t.Fatal(err)
	}

	if err := service.handleTownSetUserArea(session, buildTownMoveRequest(38, 0, 900, 250, 5)); err != nil {
		t.Fatal(err)
	}
	townPacket, rest := splitTownTransitionAndPostState(
		t,
		session.conn.(*bufferConn).write.Bytes(),
		session,
		29,
		38,
		0,
		900,
		250,
		5,
		3,
		townMoveSkillProjectionBody(t, repositories, "29"),
		false,
	)
	if townPacket.Header.MsgID != currentSceneTransitionMsgID {
		t.Fatalf("first packet msg=%d, want town transition %d", townPacket.Header.MsgID, currentSceneTransitionMsgID)
	}
	statePacket, trailing := nextCrystalPacket(t, rest)
	assertCurrentCrystalStatePacket(t, statePacket, []byte{0, 4})
	if len(trailing) != 0 {
		t.Fatalf("late crystal state header=%+v body=%x trailing=%x", statePacket.Header, statePacket.Body, trailing)
	}
}

func TestPremiumGameplayModuleRegistersCrystalContractRoutes(t *testing.T) {
	module := premiumGameplayModule()
	for _, opcode := range []uint16{
		uint16(dnfenum.CmdPacketUpdateContractOfCubeInfo),
		uint16(dnfenum.CmdPacketUseLimitCube),
	} {
		if module.LegacyHandlers[opcode] == nil {
			t.Fatalf("premium module missing legacy opcode %d", opcode)
		}
		if module.UpperHandlers[opcode] == nil {
			t.Fatalf("premium module missing upper opcode %d", opcode)
		}
	}
}
