package dnfbridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"

	"longheng.io/server/internal/modules/dnf/channelcatalog"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	"longheng.io/server/internal/modules/dnf/dungeoncmd"
	dnfmonster "longheng.io/server/internal/modules/dnf/monster"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	dnfquest "longheng.io/server/internal/modules/dnf/quest"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

func TestHandleDungeonSetPlayResultDefersCardLayoutAfterClearReward(t *testing.T) {
	service, runtime, session, connection, _ := prepareCompletedSettlementRuntime(t)
	connection.write.Reset()

	const clientRankPoint = byte(73)
	plan := currentDungeonSettlementPacketPlanForTest(t, runtime, clientRankPoint)
	runtime.settlementResultPlan = &plan
	request := make([]byte, currentDungeonPlayResultBaseSize)
	request[currentDungeonPlayResultClientRankPointOffset] = clientRankPoint
	if err := service.handleDungeonSetPlayResult(session, request); err != nil {
		t.Fatal(err)
	}

	op34, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	op37, rest := splitGameServerUpperPacket(t, rest)
	op35, rest := splitGameServerUpperPacket(t, rest)
	if len(rest) != 0 {
		t.Fatalf("unexpected immediate card stream=%x", rest)
	}
	if op34.Header.MsgID != currentDungeonPlayResultNoticeMsgID || op37.Header.MsgID != currentDungeonCharacterStateMsgID ||
		op35.Header.MsgID != currentDungeonClearRewardMsgID {
		t.Fatalf("settlement order op34=%d op37=%d op35=%d", op34.Header.MsgID, op37.Header.MsgID, op35.Header.MsgID)
	}
	if runtime.settlementCardScrollStateSent || runtime.settlementCardLayoutSent ||
		runtime.settlementCardSelectionSent {
		t.Fatalf("op46 must stop at rating result until client op69: %+v", runtime)
	}
}

func TestDungeonCardRequestsAreDispatchedAfterSettlement(t *testing.T) {
	service, runtime, session, connection, _ := prepareCompletedSettlementRuntime(t)
	completeCurrentDungeonRatingForCardPhaseTest(t, service, runtime, session, connection)

	for _, step := range []struct {
		msgID uint16
		body  []byte
	}{
		{msgID: uint16(dnfenum.CmdPacketScoreScrollState)},
		{msgID: uint16(dnfenum.CmdPacketCardSelectRightState)},
	} {
		packet, err := dnfproto.BuildChannelPacket(step.msgID, step.body, 0, dnfproto.DefaultChannelClassification)
		if err != nil {
			t.Fatal(err)
		}
		if err := service.handleGameUpper(session, packet); err != nil {
			t.Fatal(err)
		}
		response, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
		if response.Header.MsgID != step.msgID || len(rest) != 0 {
			t.Fatalf("card step op%d response=%d rest=%x", step.msgID, response.Header.MsgID, rest)
		}
		connection.write.Reset()
	}

	packet, err := dnfproto.BuildChannelPacket(uint16(dnfenum.CmdPacketSelectCard), []byte{0}, 0, dnfproto.DefaultChannelClassification)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.handleGameUpper(session, packet); err != nil {
		t.Fatal(err)
	}
	op71, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if len(rest) != 0 {
		t.Fatalf("unexpected select stream=%x", rest)
	}
	if op71.Header.MsgID != uint16(dnfenum.CmdPacketSelectCard) {
		t.Fatalf("card select op=%d", op71.Header.MsgID)
	}
	if op71.Header.Classification != currentDungeonCardResponseClass || len(op71.Body) < 33 || op71.Body[0] != 1 {
		t.Fatalf("op71 packet = %+v body=%x", op71.Header, op71.Body)
	}
}

func TestDungeonCardSelectDeliversFreeGoldOnceAndRefreshesCurrentAssets(t *testing.T) {
	service, runtime, session, connection, _ := prepareCompletedSettlementRuntime(t)
	runtime.settlementClearRewardSent = true
	runtime.advanceSettlementPhase(currentDungeonSettlementPhaseResultShown)
	plan, err := newDungeonCardRewardPlan(
		dungeonCardPlanIdentity{CharacterID: "99", DungeonID: runtime.Dungeon.ID, MazeIndex: runtime.MazeIndex, RunSeed: runtime.Seed},
		"test_current_runtime_free_gold",
		dungeonCardRewardBundle{Gold: 33},
		dungeonCardRewardBundle{},
	)
	if err != nil {
		t.Fatal(err)
	}
	state, err := newDungeonCardState(plan)
	if err != nil {
		t.Fatal(err)
	}
	runtime.settlementCardRewardState = state

	if err := service.sendCurrentDungeonCardScrollStateLocked(session, runtime, "test_prepared_scroll"); err != nil {
		t.Fatal(err)
	}
	if err := service.sendCurrentDungeonCardLayoutLocked(session, runtime, "test_prepared_layout"); err != nil {
		t.Fatal(err)
	}
	connection.write.Reset()

	packet, err := dnfproto.BuildChannelPacket(uint16(dnfenum.CmdPacketSelectCard), []byte{0}, 0, dnfproto.DefaultChannelClassification)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.handleGameUpper(session, packet); err != nil {
		t.Fatal(err)
	}

	op71, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	op14, rest := splitGameServerUpperPacket(t, rest)
	if len(rest) != 0 {
		t.Fatalf("unexpected card reward stream=%x", rest)
	}
	if op71.Header.MsgID != uint16(dnfenum.CmdPacketSelectCard) ||
		op71.Header.Classification != currentDungeonCardResponseClass ||
		len(op71.Body) != 33 || op71.Body[0] != 1 {
		t.Fatalf("op71 packet = %+v body=%x", op71.Header, op71.Body)
	}
	slots := decodeCurrentDungeonOp71SlotsForTest(t, op71.Body)
	if slots[0].StateA != 0 || slots[0].StateB != 0xff ||
		len(slots[0].Rewards) != 0 || slots[0].TerminalFlag != 0 {
		t.Fatalf("free-row op71 snapshot=%+v", slots)
	}
	if op14.Header.MsgID != uint16(dnfenum.CmdPacketWalkoutPartyMember) ||
		op14.Header.Classification != 0 ||
		!bytes.Equal(op14.Body, buildCurrentGoldStateBody(33)) {
		t.Fatalf("gold refresh packet = %+v body=%x", op14.Header, op14.Body)
	}

	repositories, ok := service.repositoryGroup()
	if !ok {
		t.Fatal("repository group unavailable")
	}
	character, _, err := repositories.Character.Load(context.Background(), "99")
	if err != nil {
		t.Fatal(err)
	}
	if character.Stats["gold"] != 33 {
		t.Fatalf("persisted gold=%d want=33", character.Stats["gold"])
	}

	connection.write.Reset()
	if err := service.handleGameUpper(session, packet); err != nil {
		t.Fatal(err)
	}
	replay71, replayRest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if replay71.Header.MsgID != uint16(dnfenum.CmdPacketSelectCard) ||
		replay71.Header.Classification != currentDungeonCardResponseClass ||
		len(replayRest) != 0 {
		t.Fatalf("card replay response=%+v rest=%x", replay71.Header, replayRest)
	}
	character, _, err = repositories.Character.Load(context.Background(), "99")
	if err != nil {
		t.Fatal(err)
	}
	if character.Stats["gold"] != 33 {
		t.Fatalf("replay mutated gold=%d want=33", character.Stats["gold"])
	}
}

func TestDungeonCardSelectCommitsPVFPageItemAndPushesIncrementalRow(t *testing.T) {
	service, runtime, session, connection, _ := prepareCompletedSettlementRuntime(t)
	runtime.settlementClearRewardSent = true
	runtime.advanceSettlementPhase(currentDungeonSettlementPhaseResultShown)
	plan, err := newDungeonCardRewardPlan(
		dungeonCardPlanIdentity{
			CharacterID: "99",
			DungeonID:   runtime.Dungeon.ID,
			MazeIndex:   runtime.MazeIndex,
			RunSeed:     runtime.Seed,
		},
		"test_current_runtime_free_item",
		dungeonCardRewardBundle{
			Gold: 19,
			Items: []dungeonCardItemReward{{
				ItemID: 5001, Count: 1, SlotStart: 9, SlotEnd: 64,
				Extra: map[string]string{"item_kind": "equipment"},
			}},
		},
		dungeonCardRewardBundle{},
	)
	if err != nil {
		t.Fatal(err)
	}
	state, err := newDungeonCardState(plan)
	if err != nil {
		t.Fatal(err)
	}
	runtime.settlementCardRewardState = state
	if err := service.sendCurrentDungeonCardScrollStateLocked(session, runtime, "test_item_scroll"); err != nil {
		t.Fatal(err)
	}
	if err := service.sendCurrentDungeonCardLayoutLocked(session, runtime, "test_item_layout"); err != nil {
		t.Fatal(err)
	}
	connection.write.Reset()

	packet, err := dnfproto.BuildChannelPacket(
		uint16(dnfenum.CmdPacketSelectCard),
		[]byte{0, 0},
		0,
		dnfproto.DefaultChannelClassification,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.handleGameUpper(session, packet); err != nil {
		t.Fatal(err)
	}

	op71, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	goldUpdate, rest := splitGameServerUpperPacket(t, rest)
	itemUpdate, rest := splitGameServerUpperPacket(t, rest)
	if len(rest) != 0 ||
		op71.Header.MsgID != uint16(dnfenum.CmdPacketSelectCard) ||
		goldUpdate.Header.MsgID != uint16(dnfenum.CmdPacketWalkoutPartyMember) ||
		itemUpdate.Header.MsgID != uint16(dnfenum.CmdPacketWalkoutPartyMember) {
		t.Fatalf(
			"card item stream op71=%d gold=%d item=%d rest=%x",
			op71.Header.MsgID,
			goldUpdate.Header.MsgID,
			itemUpdate.Header.MsgID,
			rest,
		)
	}
	if len(itemUpdate.Body) < 3+currentItemListEntryWireSize ||
		itemUpdate.Body[0] != dnfrepo.MainInventoryListType ||
		binary.LittleEndian.Uint16(itemUpdate.Body[1:3]) != 1 ||
		binary.LittleEndian.Uint16(itemUpdate.Body[3:5]) != 9 ||
		binary.LittleEndian.Uint32(itemUpdate.Body[5:9]) != 5001 {
		t.Fatalf("incremental card item update=%x", itemUpdate.Body)
	}

	repositories, ok := service.repositoryGroup()
	if !ok {
		t.Fatal("repository group unavailable")
	}
	inventory, found, err := repositories.Inventory.Load(context.Background(), "99")
	if err != nil || !found || inventory.Slots["0:9"].ItemID != 5001 {
		t.Fatalf("persisted card item found=%t err=%v inventory=%+v", found, err, inventory.Slots)
	}
	if !runtime.settlementCardRewardCommitted ||
		runtime.settlementPhase < currentDungeonSettlementPhaseRewardCommitted {
		t.Fatalf("card reward state not committed: %+v", runtime)
	}

	connection.write.Reset()
	if err := service.handleDungeonEplpCommand(session, []byte{1, 2}); err != nil {
		t.Fatal(err)
	}
	op72, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if op72.Header.MsgID != uint16(dnfenum.CmdPacketEplpCommand) ||
		op72.Header.Classification != currentDungeonCardResponseClass ||
		!bytes.Equal(op72.Body, buildCurrentDungeonOp72SuccessBody(1, 2)) ||
		len(rest) == 0 {
		t.Fatalf("item reward return route op72=%+v body=%x rest=%x", op72.Header, op72.Body, rest)
	}
	if session.dungeon.runtime != nil || !runtime.townReturnOp24Sent {
		t.Fatalf("item reward return route owner=%p runtime=%+v", session.dungeon.runtime, runtime)
	}
}

func assertCurrentDungeonOp71SelectedReward(
	t *testing.T,
	body []byte,
	selected int,
	want currentDungeonOp71RewardTuple,
) {
	t.Helper()
	if selected < 0 || selected >= dungeonCardWireSlotCount || len(body) < 1 || body[0] != 1 {
		t.Fatalf("op71 selected reward precondition selected=%d body=%x", selected, body)
	}
	offset := 1
	for slot := 0; slot < dungeonCardWireSlotCount; slot++ {
		if offset+4 > len(body) {
			t.Fatalf("op71 slot %d truncated offset=%d body=%x", slot, offset, body)
		}
		stateA := body[offset]
		stateB := body[offset+1]
		rewardCount := int(body[offset+2])
		offset += 3
		rewards := make([]currentDungeonOp71RewardTuple, 0, rewardCount)
		for rewardIndex := 0; rewardIndex < rewardCount; rewardIndex++ {
			if offset+8 > len(body) {
				t.Fatalf("op71 reward slot=%d index=%d truncated body=%x", slot, rewardIndex, body)
			}
			rewards = append(rewards, currentDungeonOp71RewardTuple{
				ValueA: binary.LittleEndian.Uint32(body[offset : offset+4]),
				ValueB: binary.LittleEndian.Uint32(body[offset+4 : offset+8]),
			})
			offset += 8
		}
		terminal := body[offset]
		offset++
		if slot != selected {
			if stateA != 0xff || stateB != 0xff || len(rewards) != 0 || terminal != 0 {
				t.Fatalf("op71 non-selected slot %d state=%d/%d rewards=%+v terminal=%d", slot, stateA, stateB, rewards, terminal)
			}
			continue
		}
		wantStateA, wantStateB := byte(0), byte(0xff)
		if selected >= dungeonCardSlotsPerSide {
			wantStateA, wantStateB = 0xff, 0
		}
		if stateA != wantStateA || stateB != wantStateB || terminal != 0 || len(rewards) != 1 || rewards[0] != want {
			t.Fatalf("op71 selected slot %d state=%d/%d rewards=%+v terminal=%d want=%+v", slot, stateA, stateB, rewards, terminal, want)
		}
	}
	if offset != len(body) {
		t.Fatalf("op71 trailing bytes offset=%d len=%d body=%x", offset, len(body), body)
	}
}

func decodeCurrentDungeonOp71SlotsForTest(
	t *testing.T,
	body []byte,
) [dungeonCardWireSlotCount]currentDungeonOp71Slot {
	t.Helper()
	var slots [dungeonCardWireSlotCount]currentDungeonOp71Slot
	if len(body) < 1 || body[0] != 1 {
		t.Fatalf("op71 success body invalid: %x", body)
	}
	offset := 1
	for index := range slots {
		if offset+4 > len(body) {
			t.Fatalf("op71 slot %d truncated: %x", index, body)
		}
		slot := currentDungeonOp71Slot{
			StateA: body[offset],
			StateB: body[offset+1],
		}
		rewardCount := int(body[offset+2])
		offset += 3
		for range rewardCount {
			if offset+8 > len(body) {
				t.Fatalf("op71 slot %d reward truncated: %x", index, body)
			}
			slot.Rewards = append(slot.Rewards, currentDungeonOp71RewardTuple{
				ValueA: binary.LittleEndian.Uint32(body[offset : offset+4]),
				ValueB: binary.LittleEndian.Uint32(body[offset+4 : offset+8]),
			})
			offset += 8
		}
		slot.TerminalFlag = body[offset]
		offset++
		slots[index] = slot
	}
	if offset != len(body) {
		t.Fatalf("op71 trailing bytes offset=%d len=%d body=%x", offset, len(body), body)
	}
	return slots
}

func TestDungeonCardPaidAndFreeRowsSelectAndCommitIndependently(t *testing.T) {
	service, runtime, session, connection, _ := prepareCompletedSettlementRuntime(t)
	queue := newFakeCurrentDungeonDeathTimerQueue()
	service.gameplayTimers = queue
	runtime.settlementClearRewardSent = true
	runtime.advanceSettlementPhase(currentDungeonSettlementPhaseResultShown)
	plan, err := newDungeonCardRewardPlan(
		dungeonCardPlanIdentity{
			CharacterID: "99",
			DungeonID:   runtime.Dungeon.ID,
			MazeIndex:   runtime.MazeIndex,
			RunSeed:     runtime.Seed,
		},
		"test_current_runtime_two_rows",
		dungeonCardRewardBundle{Gold: 11},
		dungeonCardRewardBundle{Gold: 22},
	)
	if err != nil {
		t.Fatal(err)
	}
	state, err := newDungeonCardState(plan)
	if err != nil {
		t.Fatal(err)
	}
	runtime.settlementCardRewardState = state
	if err := service.sendCurrentDungeonCardScrollStateLocked(session, runtime, "test_two_rows_scroll"); err != nil {
		t.Fatal(err)
	}
	if err := service.sendCurrentDungeonCardLayoutLocked(session, runtime, "test_two_rows_layout"); err != nil {
		t.Fatal(err)
	}
	connection.write.Reset()

	for _, request := range [][]byte{{1, 0}, {0, 0}} {
		packet, err := dnfproto.BuildChannelPacket(
			uint16(dnfenum.CmdPacketSelectCard),
			request,
			0,
			dnfproto.DefaultChannelClassification,
		)
		if err != nil {
			t.Fatal(err)
		}
		if err := service.handleGameUpper(session, packet); err != nil {
			t.Fatal(err)
		}
		op71, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
		op14, rest := splitGameServerUpperPacket(t, rest)
		if op71.Header.MsgID != uint16(dnfenum.CmdPacketSelectCard) ||
			op14.Header.MsgID != uint16(dnfenum.CmdPacketWalkoutPartyMember) ||
			len(rest) != 0 {
			t.Fatalf("row request=%x op71=%d op14=%d rest=%x", request, op71.Header.MsgID, op14.Header.MsgID, rest)
		}
		slots := decodeCurrentDungeonOp71SlotsForTest(t, op71.Body)
		if request[0] == 1 {
			if slots[0].StateA != 0xff || slots[0].StateB != 0 ||
				len(slots[0].Rewards) != 1 || slots[0].Rewards[0].ValueB != 22 {
				t.Fatalf("paid row snapshot=%+v", slots)
			}
		} else {
			if slots[0].StateA != 0 || slots[0].StateB != 0 ||
				len(slots[0].Rewards) != 1 || slots[0].Rewards[0].ValueB != 22 {
				t.Fatalf("combined row snapshot=%+v", slots)
			}
		}
		connection.write.Reset()
	}
	if !runtime.settlementCardSideRewardCommitted[dungeonCardSideFree] ||
		!runtime.settlementCardSideRewardCommitted[dungeonCardSidePaid] ||
		!runtime.settlementCardRewardCommitted {
		t.Fatalf("row commits=%+v aggregate=%t", runtime.settlementCardSideRewardCommitted, runtime.settlementCardRewardCommitted)
	}
	if scheduled, cancelled, active := queue.counts(); scheduled != 1 || cancelled != 1 || active != 0 {
		t.Fatalf("manual free selection timer scheduled=%d cancelled=%d active=%d", scheduled, cancelled, active)
	}
	repositories, ok := service.repositoryGroup()
	if !ok {
		t.Fatal("repository group unavailable")
	}
	character, found, err := repositories.Character.Load(context.Background(), "99")
	if err != nil || !found || character.Stats["gold"] != 33 {
		t.Fatalf("two-row durable gold=%d found=%t err=%v", character.Stats["gold"], found, err)
	}
}

func TestCurrentDungeonCardRequestMapsBothPaidModesToPaidRow(t *testing.T) {
	for value, want := range map[byte]dungeonCardSide{
		0: dungeonCardSideFree,
		1: dungeonCardSidePaid,
		2: dungeonCardSidePaid,
	} {
		got, ok := currentDungeonCardRequestSide(value)
		if !ok || got != want {
			t.Fatalf("request value=%d side=%d ok=%t want=%d", value, got, ok, want)
		}
	}
	if _, ok := currentDungeonCardRequestSide(3); ok {
		t.Fatal("unknown card request mode was accepted")
	}
}

func TestDungeonCardLayoutSchedulesFreeRowAutoFlipOnGameplayQueue(t *testing.T) {
	service, runtime, session, connection, _ := prepareCompletedSettlementRuntime(t)
	queue := newFakeCurrentDungeonDeathTimerQueue()
	service.gameplayTimers = queue
	runtime.settlementClearRewardSent = true
	runtime.advanceSettlementPhase(currentDungeonSettlementPhaseResultShown)
	plan, err := newDungeonCardRewardPlan(
		dungeonCardPlanIdentity{
			CharacterID: "99",
			DungeonID:   runtime.Dungeon.ID,
			MazeIndex:   runtime.MazeIndex,
			RunSeed:     runtime.Seed,
		},
		"test_current_runtime_auto_flip",
		dungeonCardRewardBundle{Gold: 17},
		dungeonCardRewardBundle{Gold: 19},
	)
	if err != nil {
		t.Fatal(err)
	}
	state, err := newDungeonCardState(plan)
	if err != nil {
		t.Fatal(err)
	}
	runtime.settlementCardRewardState = state
	if err := service.sendCurrentDungeonCardScrollStateLocked(session, runtime, "test_auto_flip_scroll"); err != nil {
		t.Fatal(err)
	}
	if err := service.sendCurrentDungeonCardLayoutLocked(session, runtime, "test_auto_flip_layout"); err != nil {
		t.Fatal(err)
	}
	task := queue.task(t, 0)
	if task.delay != currentDungeonCardAutoFlipDelay {
		t.Fatalf("auto-flip delay=%s want=%s", task.delay, currentDungeonCardAutoFlipDelay)
	}
	connection.write.Reset()
	queue.fire(task, false)
	op71, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	op14, rest := splitGameServerUpperPacket(t, rest)
	if op71.Header.MsgID != uint16(dnfenum.CmdPacketSelectCard) ||
		op14.Header.MsgID != uint16(dnfenum.CmdPacketWalkoutPartyMember) ||
		len(rest) != 0 ||
		!runtime.settlementCardSideRewardCommitted[dungeonCardSideFree] {
		t.Fatalf("auto-flip op71=%d op14=%d rest=%x runtime=%+v", op71.Header.MsgID, op14.Header.MsgID, rest, runtime)
	}
}

func TestDungeonCardExitStateOneReturnsToTownAfterAck(t *testing.T) {
	service, runtime, session, connection, _ := prepareCompletedSettlementRuntime(t)
	markCurrentDungeonCardRewardCommittedForTest(runtime)
	connection.write.Reset()

	// Live plaintext evidence: the return-to-town button is {state=1, option=2}.
	if err := service.handleDungeonEplpCommand(session, []byte{1, 2}); err != nil {
		t.Fatal(err)
	}

	op72, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if op72.Header.MsgID != uint16(dnfenum.CmdPacketEplpCommand) ||
		op72.Header.Classification != currentDungeonCardResponseClass ||
		!bytes.Equal(op72.Body, buildCurrentDungeonOp72SuccessBody(1, 2)) {
		t.Fatalf("op72 ack = %+v body=%x", op72.Header, op72.Body)
	}
	if len(rest) == 0 {
		t.Fatal("op72 {1,2} emitted no completed town route")
	}
	foundTransition := false
	for len(rest) > 0 {
		packet, tail := splitGameServerUpperPacket(t, rest)
		if packet.Header.Classification == 0 && packet.Header.MsgID == currentSceneTransitionMsgID &&
			bytes.Equal(packet.Body, wantTutorialCompletionTownTransitionBody()) {
			foundTransition = true
		}
		rest = tail
	}
	if !foundTransition {
		t.Fatalf("op72 {1,2} route did not include expected town transition stream=%x", connection.write.Bytes())
	}
	if session.dungeon.runtime != nil || !runtime.townReturnOp24Sent {
		t.Fatalf("op72 {1,2} did not commit completed return owner=%p runtime=%+v", session.dungeon.runtime, runtime)
	}
}

func TestDungeonCardExitStateOneBeforeOp71DoesNotAdvanceOrRetire(t *testing.T) {
	service, runtime, session, connection, _ := prepareCompletedSettlementRuntime(t)
	runtime.settlementClearRewardSent = true
	runtime.advanceSettlementPhase(currentDungeonSettlementPhaseResultShown)
	connection.write.Reset()

	if err := service.handleDungeonEplpCommand(session, []byte{1, 0}); err != nil {
		t.Fatal(err)
	}

	if connection.write.Len() != 0 {
		t.Fatalf("pre-op71 op72 must not synthesize card or exit packets=%x", connection.write.Bytes())
	}
	if runtime.settlementCardLayoutSent || runtime.settlementCardSelectionSent ||
		runtime.settlementCardExitAckSent || session.dungeon.runtime != runtime || runtime.townReturnOp24Sent {
		t.Fatalf("pre-op71 op72 state layout=%t select=%t exit=%t runtime=%p return=%t",
			runtime.settlementCardLayoutSent,
			runtime.settlementCardSelectionSent,
			runtime.settlementCardExitAckSent,
			session.dungeon.runtime,
			runtime.townReturnOp24Sent)
	}
}

func TestDungeonCardExitLegacyRouteHonorsPreOp71Gate(t *testing.T) {
	service, runtime, session, connection, _ := prepareCompletedSettlementRuntime(t)
	runtime.settlementClearRewardSent = true
	runtime.advanceSettlementPhase(currentDungeonSettlementPhaseResultShown)
	connection.write.Reset()

	// The current client delivers op72 only through the legacy game decoder
	// (live log: game-legacy type=72), so the exit owner must fire from
	// handleGameCommand, not only from the upper dispatcher.
	if err := service.handleGameCommand(session, byte(dnfenum.GameCmdCommand), uint16(dnfenum.CmdPacketEplpCommand), []byte{1, 0}); err != nil {
		t.Fatal(err)
	}

	if connection.write.Len() != 0 {
		t.Fatalf("legacy pre-op71 op72 synthesized packets=%x", connection.write.Bytes())
	}
	if runtime.settlementCardLayoutSent || runtime.settlementCardSelectionSent ||
		runtime.settlementCardExitAckSent || session.dungeon.runtime != runtime {
		t.Fatalf("legacy pre-op71 op72 advanced state=%+v owner=%p", runtime, session.dungeon.runtime)
	}
}

func TestDungeonCardExitOwnerTreatsSixteenByteBodyAsOpaqueProtected(t *testing.T) {
	service, runtime, session, connection, _ := prepareCompletedSettlementRuntime(t)
	markCurrentDungeonCardRewardCommittedForTest(runtime)
	connection.write.Reset()

	// Live unpatched clients deliver an opaque protected 16-byte body whose
	// first bytes are ciphertext (live prefix 67 f6 -> request_value 103/246),
	// never the semantic state/option. The owner must echo them without ever
	// treating ciphertext as a return-to-town command.
	body := append([]byte{0x67, 0xf6}, make([]byte, 14)...)
	if err := service.handleGameCommand(session, byte(dnfenum.GameCmdCommand), uint16(dnfenum.CmdPacketEplpCommand), body); err != nil {
		t.Fatal(err)
	}

	op72, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if op72.Header.MsgID != uint16(dnfenum.CmdPacketEplpCommand) ||
		!bytes.Equal(op72.Body, buildCurrentDungeonOp72SuccessBody(0x67, 0xf6)) {
		t.Fatalf("opaque 16-byte op72 ack = %+v body=%x", op72.Header, op72.Body)
	}
	if len(rest) != 0 {
		t.Fatalf("opaque op72 ciphertext triggered trailing packets rest=%x", rest)
	}
	if session.dungeon.runtime != runtime || runtime.townReturnOp24Sent {
		t.Fatalf("opaque op72 ciphertext mutated runtime=%p return=%t", session.dungeon.runtime, runtime.townReturnOp24Sent)
	}
}

func TestDungeonCardExitPlaintextCloneBodyReturnsToTown(t *testing.T) {
	service, runtime, session, connection, _ := prepareCompletedSettlementRuntime(t)
	markCurrentDungeonCardRewardCommittedForTest(runtime)
	connection.write.Reset()

	// The selective plaintext clone delivers {state, option} plus the private
	// four-byte trailer; the legacy boundary strips the trailer before decode.
	// Live evidence pins return-to-town at option 2.
	cloneBody := append([]byte{1, 2}, 0xde, 0xad, 0xbe, 0xef)
	if err := service.handleGameCommand(session, byte(dnfenum.GameCmdCommand), uint16(dnfenum.CmdPacketEplpCommand), cloneBody); err != nil {
		t.Fatal(err)
	}

	op72, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if op72.Header.MsgID != uint16(dnfenum.CmdPacketEplpCommand) ||
		!bytes.Equal(op72.Body, buildCurrentDungeonOp72SuccessBody(1, 2)) {
		t.Fatalf("plaintext-clone op72 ack = %+v body=%x", op72.Header, op72.Body)
	}
	foundTransition := false
	for len(rest) > 0 {
		packet, tail := splitGameServerUpperPacket(t, rest)
		if packet.Header.Classification == 0 && packet.Header.MsgID == currentSceneTransitionMsgID &&
			bytes.Equal(packet.Body, wantTutorialCompletionTownTransitionBody()) {
			foundTransition = true
		}
		rest = tail
	}
	if !foundTransition {
		t.Fatalf("plaintext-clone {1,2} op72 emitted no town transition stream=%x", connection.write.Bytes())
	}
	if session.dungeon.runtime != nil || !runtime.townReturnOp24Sent {
		t.Fatalf("plaintext-clone {1,2} op72 did not commit completed return owner=%p runtime=%+v", session.dungeon.runtime, runtime)
	}
}

func TestDungeonCardExitUnknownOptionIsAckOnly(t *testing.T) {
	service, runtime, session, connection, _ := prepareCompletedSettlementRuntime(t)
	markCurrentDungeonCardRewardCommittedForTest(runtime)
	connection.write.Reset()

	if err := service.handleGameCommand(session, byte(dnfenum.GameCmdCommand), uint16(dnfenum.CmdPacketEplpCommand), []byte{1, 3}); err != nil {
		t.Fatal(err)
	}

	op72, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if op72.Header.MsgID != uint16(dnfenum.CmdPacketEplpCommand) ||
		op72.Header.Classification != currentDungeonCardResponseClass ||
		!bytes.Equal(op72.Body, buildCurrentDungeonOp72SuccessBody(1, 3)) {
		t.Fatalf("op72 ack = %+v body=%x want echo {1,3}", op72.Header, op72.Body)
	}
	if len(rest) != 0 {
		t.Fatalf("op72 {1,3} must not emit a town route or any trailing packet rest=%x", rest)
	}
	if !runtime.settlementCardExitAckSent || session.dungeon.runtime != runtime || runtime.townReturnOp24Sent {
		t.Fatalf("op72 {1,3} mutated exit=%t runtime=%p return=%t",
			runtime.settlementCardExitAckSent, session.dungeon.runtime, runtime.townReturnOp24Sent)
	}
}

func TestDungeonCardExitRetryReEntersSameDungeonAfterAck(t *testing.T) {
	service, runtime, session, connection, _ := prepareCompletedSettlementRuntime(t)
	runtime.settlementPlayResultReceived = true
	runtime.settlementStatisticReceived = true
	markCurrentDungeonCardRewardCommittedForTest(runtime)
	wantDungeon := runtime.Dungeon.ID
	wantDifficulty := runtime.Request.Difficulty
	connection.write.Reset()

	// Live evidence: 再次挑战 is {state=1, option=0}; the client waits for a
	// server-driven re-entry instead of sending op16.
	if err := service.handleGameCommand(session, byte(dnfenum.GameCmdCommand), uint16(dnfenum.CmdPacketEplpCommand), []byte{1, 0}); err != nil {
		t.Fatal(err)
	}

	op72, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if op72.Header.MsgID != uint16(dnfenum.CmdPacketEplpCommand) ||
		op72.Header.Classification != currentDungeonCardResponseClass ||
		!bytes.Equal(op72.Body, buildCurrentDungeonOp72SuccessBody(1, 0)) {
		t.Fatalf("op72 ack = %+v body=%x want echo {1,0}", op72.Header, op72.Body)
	}
	foundEntryAck := false
	foundTownTransition := false
	for len(rest) > 0 {
		packet, tail := splitGameServerUpperPacket(t, rest)
		if packet.Header.Classification == 1 && packet.Header.MsgID == uint16(dnfenum.CmdPacketSelectDungeon) {
			foundEntryAck = true
		}
		if packet.Header.Classification == 0 && packet.Header.MsgID == currentSceneTransitionMsgID {
			foundTownTransition = true
		}
		rest = tail
	}
	if !foundEntryAck {
		t.Fatalf("retry {1,0} emitted no op16 entry ack stream=%x", connection.write.Bytes())
	}
	if foundTownTransition {
		t.Fatalf("retry {1,0} unexpectedly emitted a town transition stream=%x", connection.write.Bytes())
	}
	newRuntime := session.dungeon.runtime
	if newRuntime == nil || newRuntime == runtime {
		t.Fatalf("retry {1,0} did not attach a fresh runtime old=%p new=%p", runtime, newRuntime)
	}
	if newRuntime.Dungeon.ID != wantDungeon || newRuntime.Request.Difficulty != wantDifficulty {
		t.Fatalf("retry re-entered wrong dungeon got=%d/%d want=%d/%d",
			newRuntime.Dungeon.ID, newRuntime.Request.Difficulty, wantDungeon, wantDifficulty)
	}
	if newRuntime.Session.Snapshot().Run.Status != worldmap.DungeonRunActive {
		t.Fatalf("retry runtime status=%v want active", newRuntime.Session.Snapshot().Run.Status)
	}
}

func TestDungeonCardExitSelectOtherOpensSelectorAfterAck(t *testing.T) {
	service, runtime, session, connection, _ := prepareCompletedSettlementRuntime(t)
	runtime.settlementPlayResultReceived = true
	runtime.settlementStatisticReceived = true
	markCurrentDungeonCardRewardCommittedForTest(runtime)
	connection.write.Reset()

	// Live evidence: 选择其它地下城 is {state=1, option=1}; the client waits
	// for the selector context instead of sending op15.
	if err := service.handleGameCommand(session, byte(dnfenum.GameCmdCommand), uint16(dnfenum.CmdPacketEplpCommand), []byte{1, 1}); err != nil {
		t.Fatal(err)
	}

	op72, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if op72.Header.MsgID != uint16(dnfenum.CmdPacketEplpCommand) ||
		op72.Header.Classification != currentDungeonCardResponseClass ||
		!bytes.Equal(op72.Body, buildCurrentDungeonOp72SuccessBody(1, 1)) {
		t.Fatalf("op72 ack = %+v body=%x want echo {1,1}", op72.Header, op72.Body)
	}
	foundSelectAck := false
	foundFatigue := false
	foundContext := false
	foundTownTransition := false
	for len(rest) > 0 {
		packet, tail := splitGameServerUpperPacket(t, rest)
		switch {
		case packet.Header.MsgID == uint16(dnfenum.CmdPacketEnterSelectDungeon):
			foundSelectAck = true
		case packet.Header.MsgID == currentFatigueMsgID:
			foundFatigue = true
		case packet.Header.MsgID == currentDungeonContextMsgID:
			foundContext = true
		case packet.Header.Classification == 0 && packet.Header.MsgID == currentSceneTransitionMsgID:
			foundTownTransition = true
		}
		rest = tail
	}
	if !foundSelectAck || !foundFatigue || !foundContext {
		t.Fatalf("select-other {1,1} selector push incomplete select=%t fatigue=%t context=%t stream=%x",
			foundSelectAck, foundFatigue, foundContext, connection.write.Bytes())
	}
	if foundTownTransition {
		t.Fatalf("select-other {1,1} unexpectedly emitted a town transition stream=%x", connection.write.Bytes())
	}
	if session.dungeon.runtime != nil {
		t.Fatalf("select-other {1,1} did not retire the completed runtime=%+v", session.dungeon.runtime)
	}
}

func TestDungeonCardExitFocusNotificationKeepsGateForRealClick(t *testing.T) {
	service, runtime, session, connection, _ := prepareCompletedSettlementRuntime(t)
	markCurrentDungeonCardRewardCommittedForTest(runtime)
	connection.write.Reset()

	// Live sequence: hovering the return-to-town button sends {2,2}, then the
	// click sends {1,2}. The focus ACK must not consume the one-shot exit
	// gate, or the real click loses its ACK and the town route.
	if err := service.handleGameCommand(session, byte(dnfenum.GameCmdCommand), uint16(dnfenum.CmdPacketEplpCommand), []byte{2, 2}); err != nil {
		t.Fatal(err)
	}
	op72, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if op72.Header.MsgID != uint16(dnfenum.CmdPacketEplpCommand) ||
		op72.Header.Classification != currentDungeonCardResponseClass ||
		!bytes.Equal(op72.Body, buildCurrentDungeonOp72SuccessBody(2, 2)) {
		t.Fatalf("focus op72 ack = %+v body=%x", op72.Header, op72.Body)
	}
	if len(rest) != 0 {
		t.Fatalf("focus op72 emitted trailing packets rest=%x", rest)
	}
	if runtime.settlementCardExitAckSent {
		t.Fatal("focus notification consumed the one-shot exit gate")
	}

	if err := service.handleGameCommand(session, byte(dnfenum.GameCmdCommand), uint16(dnfenum.CmdPacketEplpCommand), []byte{1, 2}); err != nil {
		t.Fatal(err)
	}
	// Skip the first (focus) packet; the second packet must be the click ACK.
	first, tail := splitGameServerUpperPacket(t, connection.write.Bytes())
	second, rest := splitGameServerUpperPacket(t, tail)
	if !bytes.Equal(first.Body, buildCurrentDungeonOp72SuccessBody(2, 2)) ||
		second.Header.MsgID != uint16(dnfenum.CmdPacketEplpCommand) ||
		!bytes.Equal(second.Body, buildCurrentDungeonOp72SuccessBody(1, 2)) {
		t.Fatalf("focus-then-click acks first=%+v second=%+v", first.Header, second.Header)
	}
	foundTransition := false
	for len(rest) > 0 {
		packet, tail2 := splitGameServerUpperPacket(t, rest)
		if packet.Header.Classification == 0 && packet.Header.MsgID == currentSceneTransitionMsgID &&
			bytes.Equal(packet.Body, wantTutorialCompletionTownTransitionBody()) {
			foundTransition = true
		}
		rest = tail2
	}
	if !foundTransition {
		t.Fatalf("click after focus emitted no town transition stream=%x", connection.write.Bytes())
	}
	if session.dungeon.runtime != nil || !runtime.townReturnOp24Sent {
		t.Fatalf("click after focus did not commit completed return owner=%p runtime=%+v", session.dungeon.runtime, runtime)
	}
}

func prepareOrdinaryCompletedSettlementRuntime(t *testing.T) (*Service, *runtimeDungeonState, *gameSession, *bufferConn) {
	t.Helper()
	table, resolver, monsters := loadBridgeDungeonStaticData(t, bridgeDungeonPVF(false))
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(context.Background(), dnfrepo.CharacterRecord{
		CharacterID: "99",
		AccountID:   "account-1",
		Level:       20,
		Stats: map[string]int64{
			"fatigue":    100,
			"town_id":    38,
			"area_id":    1,
			"pos_x":      450,
			"pos_y":      234,
			"direction":  5,
			"area_state": 3,
		},
	}); err != nil {
		t.Fatal(err)
	}
	service := &Service{
		options:             options{accountID: "account-1", gameUpperBodyCodec: gameUpperBodyCodecPlain},
		worldMapTable:       table,
		worldMapResolver:    resolver,
		dungeonMonsterTable: monsters,
		dungeonChoice:       func(int) (int, error) { return 0, nil },
		dungeonSeed:         func() (uint32, error) { return 0x12345678, nil },
		repositoryProvider:  func() (dnfrepo.Group, bool) { return repositories, true },
	}
	conn := &bufferConn{}
	channel := channelcatalog.Channel{ServerID: 1, ID: 19, Type: 1, Name: "ch.19", Port: 10019}
	session := &gameSession{
		conn:                conn,
		connID:              "ordinary-completed-settlement-test",
		channel:             channel,
		residentChannel:     channel,
		selectedCharacterID: 99,
	}
	runtime, _, err := service.prepareDungeonRuntime(
		context.Background(),
		session,
		dungeoncmd.SelectDungeonRequest{DungeonID: 700, Difficulty: 1},
	)
	if err != nil {
		t.Fatalf("prepare ordinary runtime: %v", err)
	}
	session.dungeon.runtime = runtime
	// Mirror the live op16 entry: the selector origin is bound from the town
	// scene snapshot and frozen into the run for its eventual town return.
	session.townMu.Lock()
	session.townSceneReadyCharacterID = 99
	session.townPositionSnapshot = currentTownPositionSnapshot{
		CharacterID:   99,
		TownID:        38,
		AreaID:        1,
		PositionX:     450,
		PositionY:     234,
		MovementCode:  5,
		PositionValid: true,
	}
	session.townMu.Unlock()
	if _, bound := bindCurrentTownSelectorOrigin(session); !bound {
		t.Fatal("fixture selector origin did not bind")
	}
	if err := freezeCurrentDungeonTownReturnOrigin(session, runtime); err != nil {
		t.Fatalf("fixture origin freeze: %v", err)
	}
	completeSettlementRuntimeForTest(t, service, runtime, session)
	return service, runtime, session, conn
}

func TestDungeonCardExitRetryReEntersOrdinaryDungeonWithOriginFreeze(t *testing.T) {
	service, runtime, session, connection := prepareOrdinaryCompletedSettlementRuntime(t)
	runtime.settlementPlayResultReceived = true
	runtime.settlementStatisticReceived = true
	markCurrentDungeonCardRewardCommittedForTest(runtime)
	connection.write.Reset()

	// Ordinary dungeons freeze the new run's town-return origin from the bound
	// selector origin; the retry owner must re-bind it from the retirement's
	// restored town snapshot, or the entry flow is rejected
	// (ordinary_dungeon_town_return_origin_unavailable, live proof).
	if err := service.handleGameCommand(session, byte(dnfenum.GameCmdCommand), uint16(dnfenum.CmdPacketEplpCommand), []byte{1, 0}); err != nil {
		t.Fatal(err)
	}

	op72, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if op72.Header.MsgID != uint16(dnfenum.CmdPacketEplpCommand) ||
		op72.Header.Classification != currentDungeonCardResponseClass ||
		!bytes.Equal(op72.Body, buildCurrentDungeonOp72SuccessBody(1, 0)) {
		t.Fatalf("op72 ack = %+v body=%x want echo {1,0}", op72.Header, op72.Body)
	}
	foundEntryAck := false
	for len(rest) > 0 {
		packet, tail := splitGameServerUpperPacket(t, rest)
		if packet.Header.Classification == 1 && packet.Header.MsgID == uint16(dnfenum.CmdPacketSelectDungeon) {
			foundEntryAck = true
		}
		rest = tail
	}
	if !foundEntryAck {
		t.Fatalf("ordinary retry {1,0} emitted no op16 entry ack stream=%x", connection.write.Bytes())
	}
	newRuntime := session.dungeon.runtime
	if newRuntime == nil || newRuntime == runtime {
		t.Fatalf("ordinary retry {1,0} did not attach a fresh runtime old=%p new=%p", runtime, newRuntime)
	}
	if newRuntime.Session.Snapshot().Run.Status != worldmap.DungeonRunActive {
		t.Fatalf("ordinary retry runtime status=%v want active", newRuntime.Session.Snapshot().Run.Status)
	}
	if !newRuntime.townReturnOrigin.PositionValid || newRuntime.townReturnOrigin.TownID != 38 || newRuntime.townReturnOrigin.AreaID != 1 {
		t.Fatalf("ordinary retry town-return origin=%+v want valid 38/1", newRuntime.townReturnOrigin)
	}
}

func prepareStoryChainCompletedSettlementRuntime(t *testing.T) (*Service, *runtimeDungeonState, *gameSession, *bufferConn) {
	t.Helper()
	source := bridgePVFSource{
		worldmap.DefaultMapList: "100 `dungeon/test/story_a.map`\n200 `dungeon/test/story_b.map`\n",
		"map/dungeon/test/story_a.map": "[map name]\n`story_a`\n" +
			"[dungeon]\n700\n" +
			"[type]\n`[start]`\n" +
			"[monster]\n3001 10 0 100 200 0 0 0 `[fixed]` `[normal]`\n",
		"map/dungeon/test/story_b.map": "[map name]\n`story_b`\n" +
			"[dungeon]\n800\n" +
			"[type]\n`[start]`\n" +
			"[monster]\n3001 10 0 100 200 0 0 0 `[fixed]` `[normal]`\n",
		worldmap.DefaultDungeonList: "700 `story_a.dgn`\n800 `story_b.dgn`\n",
		"dungeon/story_a.dgn": "[name]\n`Story Dungeon A`\n" +
			"[minimum required level]\n10\n" +
			"[basis level]\n20\n" +
			"[limit party count]\n1\n" +
			"[maze info]\n" +
			"[quest connection]\n0 3148\n" +
			"[size]\n1 1\n" +
			"[greed]\n`A`\n" +
			"[map specification]\n`map` 0 0 100\n" +
			"[start map]\n0 0\n" +
			"[boss map]\n0 0\n",
		"dungeon/story_b.dgn": "[name]\n`Story Dungeon B`\n" +
			"[minimum required level]\n10\n" +
			"[basis level]\n20\n" +
			"[limit party count]\n1\n" +
			"[maze info]\n" +
			"[quest connection]\n0 3149\n" +
			"[size]\n1 1\n" +
			"[greed]\n`A`\n" +
			"[map specification]\n`map` 0 0 200\n" +
			"[start map]\n0 0\n" +
			"[boss map]\n0 0\n",
		worldmap.DefaultWorldMapList: "1 `test.wdm`\n",
		"worldmap/test.wdm":          "[name]\n`Synthetic Area`\n[dungeon]\n700 -1\n800 -1\n[/dungeon]\n",
		dnfmonster.DefaultList:       "3001 `test.gob`\n",
		"monster/test.gob": "[name]\n`Synthetic Goblin`\n" +
			"[level]\n10\n" +
			"[hp]\n500\n" +
			"[exp]\n25\n",
	}
	table, resolver, monsters := loadBridgeDungeonStaticData(t, source)
	questSource := questListTestSource{
		dnfquest.DefaultList: "3148 `story_a.qst`\n3149 `story_b.qst`\n",
		"n_quest/story_a.qst": "[grade]\n`[epic]`\n[level]\n1 99\n[job]\n`[all]`\n" +
			"[type]\n`[clear map]`\n[int data]\n76156\n",
		"n_quest/story_b.qst": "[grade]\n`[epic]`\n[level]\n1 99\n[job]\n`[all]`\n" +
			"[pre required quest]\n3148\n" +
			"[type]\n`[clear map]`\n[int data]\n76166\n",
	}
	questIndex, err := dnfpvf.Build(context.Background(), questSource, dnfpvf.BuildOptions{Lists: []string{dnfquest.DefaultList}})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := dnfquest.Load(context.Background(), questIndex)
	if err != nil {
		t.Fatal(err)
	}
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Character.Save(context.Background(), dnfrepo.CharacterRecord{
		CharacterID: "99",
		AccountID:   "account-1",
		Level:       20,
		Stats: map[string]int64{
			"fatigue":    100,
			"town_id":    38,
			"area_id":    1,
			"pos_x":      450,
			"pos_y":      234,
			"direction":  5,
			"area_state": 3,
		},
	}); err != nil {
		t.Fatal(err)
	}
	service := &Service{
		options:             options{accountID: "account-1", gameUpperBodyCodec: gameUpperBodyCodecPlain},
		worldMapTable:       table,
		worldMapResolver:    resolver,
		dungeonMonsterTable: monsters,
		questCatalog:        catalog,
		dungeonChoice:       func(int) (int, error) { return 0, nil },
		dungeonSeed:         func() (uint32, error) { return 0x12345678, nil },
		repositoryProvider:  func() (dnfrepo.Group, bool) { return repositories, true },
	}
	conn := &bufferConn{}
	channel := channelcatalog.Channel{ServerID: 1, ID: 19, Type: 1, Name: "ch.19", Port: 10019}
	session := &gameSession{
		conn:                conn,
		connID:              "story-chain-completed-settlement-test",
		channel:             channel,
		residentChannel:     channel,
		selectedCharacterID: 99,
	}
	runtime, _, err := service.prepareDungeonRuntime(
		context.Background(),
		session,
		dungeoncmd.SelectDungeonRequest{DungeonID: 700, Difficulty: 1},
	)
	if err != nil {
		t.Fatalf("prepare story runtime: %v", err)
	}
	session.dungeon.runtime = runtime
	session.townMu.Lock()
	session.townSceneReadyCharacterID = 99
	session.townPositionSnapshot = currentTownPositionSnapshot{
		CharacterID:   99,
		TownID:        38,
		AreaID:        1,
		PositionX:     450,
		PositionY:     234,
		MovementCode:  5,
		PositionValid: true,
	}
	session.townMu.Unlock()
	if _, bound := bindCurrentTownSelectorOrigin(session); !bound {
		t.Fatal("fixture selector origin did not bind")
	}
	if err := freezeCurrentDungeonTownReturnOrigin(session, runtime); err != nil {
		t.Fatalf("fixture origin freeze: %v", err)
	}
	completeSettlementRuntimeForTest(t, service, runtime, session)
	return service, runtime, session, conn
}

func TestDungeonCardExitStoryNextQuestEntersNextDungeon(t *testing.T) {
	service, runtime, session, connection := prepareStoryChainCompletedSettlementRuntime(t)
	runtime.settlementPlayResultReceived = true
	runtime.settlementStatisticReceived = true
	markCurrentDungeonCardRewardCommittedForTest(runtime)
	connection.write.Reset()

	// 剧情副本模式's 开始下个任务 is the same {1,0} wire value as 再次挑战.
	// Because the completed maze is quest-connected, the owner must follow the
	// PVF quest chain (3148 -> 3149) into the successor's dungeon (800)
	// instead of re-entering the finished one (700).
	if err := service.handleGameCommand(session, byte(dnfenum.GameCmdCommand), uint16(dnfenum.CmdPacketEplpCommand), []byte{1, 0}); err != nil {
		t.Fatal(err)
	}

	op72, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if op72.Header.MsgID != uint16(dnfenum.CmdPacketEplpCommand) ||
		op72.Header.Classification != currentDungeonCardResponseClass ||
		!bytes.Equal(op72.Body, buildCurrentDungeonOp72SuccessBody(1, 0)) {
		t.Fatalf("op72 ack = %+v body=%x want echo {1,0}", op72.Header, op72.Body)
	}
	foundEntryAck := false
	for len(rest) > 0 {
		packet, tail := splitGameServerUpperPacket(t, rest)
		if packet.Header.Classification == 1 && packet.Header.MsgID == uint16(dnfenum.CmdPacketSelectDungeon) {
			foundEntryAck = true
		}
		rest = tail
	}
	if !foundEntryAck {
		t.Fatalf("story next-quest {1,0} emitted no op16 entry ack stream=%x", connection.write.Bytes())
	}
	newRuntime := session.dungeon.runtime
	if newRuntime == nil || newRuntime == runtime {
		t.Fatalf("story next-quest {1,0} did not attach a fresh runtime old=%p new=%p", runtime, newRuntime)
	}
	if newRuntime.Dungeon.ID != 800 {
		t.Fatalf("story next-quest entered dungeon %d, want 800 (successor quest 3149)", newRuntime.Dungeon.ID)
	}
	if newRuntime.Session.Snapshot().Run.Status != worldmap.DungeonRunActive {
		t.Fatalf("story next-quest runtime status=%v want active", newRuntime.Session.Snapshot().Run.Status)
	}
}

func TestDungeonCardExitOrdinaryRetryStillEntersSameDungeon(t *testing.T) {
	service, runtime, session, connection := prepareOrdinaryCompletedSettlementRuntime(t)
	runtime.settlementPlayResultReceived = true
	runtime.settlementStatisticReceived = true
	markCurrentDungeonCardRewardCommittedForTest(runtime)
	connection.write.Reset()

	// The ordinary (non quest-connected) dungeon has no story successor, so
	// {1,0} stays a same-dungeon retry.
	if err := service.handleGameCommand(session, byte(dnfenum.GameCmdCommand), uint16(dnfenum.CmdPacketEplpCommand), []byte{1, 0}); err != nil {
		t.Fatal(err)
	}
	newRuntime := session.dungeon.runtime
	if newRuntime == nil || newRuntime == runtime {
		t.Fatalf("ordinary retry did not attach a fresh runtime old=%p new=%p", runtime, newRuntime)
	}
	if newRuntime.Dungeon.ID != runtime.Dungeon.ID {
		t.Fatalf("ordinary retry entered dungeon %d, want same %d", newRuntime.Dungeon.ID, runtime.Dungeon.ID)
	}
}
