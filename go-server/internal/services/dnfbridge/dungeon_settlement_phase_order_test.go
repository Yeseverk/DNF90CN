package dnfbridge

import (
	"bytes"
	"testing"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
)

// TestCurrentDungeonSettlementRequiresClientResultBeforeRatingPackets owns the
// causal boundary between final-room clear and the rating screen.  The clear
// path may publish its op31 settlement entry, but op34/op37/op35 belong only to
// the subsequent, current-EXE-shaped op46 request.
func TestCurrentDungeonSettlementRequiresClientResultBeforeRatingPackets(t *testing.T) {
	service, runtime, session, connection, _ := prepareCompletedSettlementRuntime(t)

	var sawEntry bool
	for rest := connection.write.Bytes(); len(rest) > 0; {
		packet, tail := splitGameServerUpperPacket(t, rest)
		switch packet.Header.MsgID {
		case currentDungeonSettlementEntryMsgID:
			if packet.Header.Classification != 0 || !bytes.Equal(packet.Body, []byte{0}) {
				t.Fatalf("op31 entry class=%d body=%x", packet.Header.Classification, packet.Body)
			}
			sawEntry = true
		case currentDungeonPlayResultNoticeMsgID, currentDungeonCharacterStateMsgID, currentDungeonClearRewardMsgID:
			t.Fatalf("rating packet op%d was sent before client op46", packet.Header.MsgID)
		}
		rest = tail
	}
	if !sawEntry || !runtime.settlementEntrySent {
		t.Fatalf("completed clear did not publish op31 state=%+v", runtime)
	}

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
		t.Fatalf("op46 emitted packets after rating chain: %x", rest)
	}
	if op34.Header.MsgID != currentDungeonPlayResultNoticeMsgID ||
		op37.Header.MsgID != currentDungeonCharacterStateMsgID ||
		op35.Header.MsgID != currentDungeonClearRewardMsgID {
		t.Fatalf("rating order got=%d->%d->%d want=%d->%d->%d",
			op34.Header.MsgID, op37.Header.MsgID, op35.Header.MsgID,
			currentDungeonPlayResultNoticeMsgID, currentDungeonCharacterStateMsgID, currentDungeonClearRewardMsgID)
	}
	if !bytes.Equal(op34.Body, plan.PlayResultBody) ||
		!bytes.Equal(op37.Body, plan.CharacterBody) ||
		!bytes.Equal(op35.Body, plan.ClearRewardBody) {
		t.Fatalf("rating bodies do not match frozen plan op34=%x op37_len=%d op35_len=%d",
			op34.Body, len(op37.Body), len(op35.Body))
	}
}

// TestCurrentDungeonCardHandshakeAdvancesOneClientRequestAtATime prevents a
// server-side replay from collapsing op69, op70 and op71 into one write burst.
// Each response is owned by the matching current-EXE request.
func TestCurrentDungeonCardHandshakeAdvancesOneClientRequestAtATime(t *testing.T) {
	service, runtime, session, connection, _ := prepareCompletedSettlementRuntime(t)
	completeCurrentDungeonRatingForCardPhaseTest(t, service, runtime, session, connection)

	request69 := buildCurrentDungeonClientUpperForPhaseTest(t, uint16(dnfenum.CmdPacketScoreScrollState), nil)
	if err := service.handleGameUpper(session, request69); err != nil {
		t.Fatal(err)
	}
	op69, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if len(rest) != 0 || op69.Header.MsgID != uint16(dnfenum.CmdPacketScoreScrollState) ||
		op69.Header.Classification != currentDungeonCardResponseClass ||
		!bytes.Equal(op69.Body, buildCurrentDungeonOp69SuccessBody()) {
		t.Fatalf("op69 request must receive only op69 ACK header=%+v body=%x rest=%x", op69.Header, op69.Body, rest)
	}
	if !runtime.settlementCardScrollStateSent || runtime.settlementCardRightStateSent ||
		runtime.settlementCardLayoutSent || runtime.settlementCardSelectionSent {
		t.Fatalf("op69 advanced beyond score-scroll ACK: %+v", runtime)
	}

	connection.write.Reset()
	request70 := buildCurrentDungeonClientUpperForPhaseTest(t, uint16(dnfenum.CmdPacketCardSelectRightState), nil)
	if err := service.handleGameUpper(session, request70); err != nil {
		t.Fatal(err)
	}
	op70, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if len(rest) != 0 || op70.Header.MsgID != uint16(dnfenum.CmdPacketCardSelectRightState) ||
		op70.Header.Classification != currentDungeonCardResponseClass ||
		!bytes.Equal(op70.Body, buildCurrentDungeonOp70EightValueSuccessBody(currentDungeonCardLayoutValues())) {
		t.Fatalf("op70 request must receive only op70 layout header=%+v body=%x rest=%x", op70.Header, op70.Body, rest)
	}
	if !runtime.settlementCardRightStateSent || !runtime.settlementCardLayoutSent ||
		runtime.settlementCardSelectionSent {
		t.Fatalf("op70 did not stop at card layout: %+v", runtime)
	}

	connection.write.Reset()
	request71 := buildCurrentDungeonClientUpperForPhaseTest(t, uint16(dnfenum.CmdPacketSelectCard), []byte{0})
	if err := service.handleGameUpper(session, request71); err != nil {
		t.Fatal(err)
	}
	op71, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	if op71.Header.MsgID != uint16(dnfenum.CmdPacketSelectCard) ||
		op71.Header.Classification != currentDungeonCardResponseClass || len(op71.Body) < 33 || op71.Body[0] != 1 {
		t.Fatalf("op71 request did not receive selection/reward body header=%+v body=%x", op71.Header, op71.Body)
	}
	for len(rest) > 0 {
		packet, tail := splitGameServerUpperPacket(t, rest)
		if packet.Header.MsgID == uint16(dnfenum.CmdPacketScoreScrollState) ||
			packet.Header.MsgID == uint16(dnfenum.CmdPacketCardSelectRightState) {
			t.Fatalf("op71 replayed an earlier card phase op%d", packet.Header.MsgID)
		}
		rest = tail
	}
	if !runtime.settlementCardSelectionSent {
		t.Fatalf("op71 did not finish card selection state=%+v", runtime)
	}
}

// TestCurrentDungeonExitActionsCannotRetireRuntimeBeforeCardSelection locks the
// A real exit action after op70 must first synthesize the reference auto-flip
// for the free row, durably grant it, and only then execute the requested
// retry/select-other/return route.
func TestCurrentDungeonExitActionsAutoFlipFreeCardBeforeTransition(t *testing.T) {
	for _, option := range []byte{0, 1, 2} {
		t.Run(map[byte]string{0: "retry", 1: "select_other", 2: "return_town"}[option], func(t *testing.T) {
			service, runtime, session, connection, _ := prepareCompletedSettlementRuntime(t)
			completeCurrentDungeonRatingForCardPhaseTest(t, service, runtime, session, connection)

			if err := service.handleGameUpper(session,
				buildCurrentDungeonClientUpperForPhaseTest(t, uint16(dnfenum.CmdPacketScoreScrollState), nil)); err != nil {
				t.Fatal(err)
			}
			connection.write.Reset()
			if err := service.handleGameUpper(session,
				buildCurrentDungeonClientUpperForPhaseTest(t, uint16(dnfenum.CmdPacketCardSelectRightState), nil)); err != nil {
				t.Fatal(err)
			}
			if !runtime.settlementCardLayoutSent || runtime.settlementCardSelectionSent {
				t.Fatalf("pre-op71 fixture phase invalid: %+v", runtime)
			}

			connection.write.Reset()
			if err := service.handleDungeonEplpCommand(session, []byte{1, option}); err != nil {
				t.Fatal(err)
			}
			sawSelectCard := false
			for rest := connection.write.Bytes(); len(rest) > 0; {
				packet, tail := splitGameServerUpperPacket(t, rest)
				if packet.Header.MsgID == uint16(dnfenum.CmdPacketSelectCard) {
					sawSelectCard = true
				}
				rest = tail
			}
			if !sawSelectCard ||
				!runtime.settlementCardSideSelectionSent[dungeonCardSideFree] ||
				!runtime.settlementCardSideRewardCommitted[dungeonCardSideFree] ||
				!runtime.settlementCardExitAckSent {
				t.Fatalf("option=%d did not auto-flip/commit free row before action: %+v", option, runtime)
			}
		})
	}
}

func completeCurrentDungeonRatingForCardPhaseTest(
	t *testing.T,
	service *Service,
	runtime *runtimeDungeonState,
	session *gameSession,
	connection *bufferConn,
) {
	t.Helper()
	connection.write.Reset()
	const clientRankPoint = byte(73)
	plan := currentDungeonSettlementPacketPlanForTest(t, runtime, clientRankPoint)
	runtime.settlementResultPlan = &plan
	request := make([]byte, currentDungeonPlayResultBaseSize)
	request[currentDungeonPlayResultClientRankPointOffset] = clientRankPoint
	if err := service.handleDungeonSetPlayResult(session, request); err != nil {
		t.Fatal(err)
	}
	plan71, err := newDungeonCardRewardPlan(
		dungeonCardPlanIdentity{
			CharacterID: "99",
			DungeonID:   runtime.Dungeon.ID,
			MazeIndex:   runtime.MazeIndex,
			RunSeed:     runtime.Seed,
		},
		"test_request_driven_empty_card_plan",
		dungeonCardRewardBundle{},
		dungeonCardRewardBundle{},
	)
	if err != nil {
		t.Fatal(err)
	}
	state71, err := newDungeonCardState(plan71)
	if err != nil {
		t.Fatal(err)
	}
	runtime.settlementCardRewardState = state71
	connection.write.Reset()
}

func buildCurrentDungeonClientUpperForPhaseTest(t *testing.T, msgID uint16, body []byte) []byte {
	t.Helper()
	packet, err := dnfproto.BuildChannelPacket(msgID, body, 0, dnfproto.DefaultChannelClassification)
	if err != nil {
		t.Fatal(err)
	}
	return packet
}

func markCurrentDungeonCardRewardCommittedForTest(runtime *runtimeDungeonState) {
	if runtime == nil {
		return
	}
	runtime.settlementClearRewardSent = true
	runtime.settlementCardScrollStateSent = true
	runtime.settlementCardRightStateSent = true
	runtime.settlementCardLayoutSent = true
	runtime.settlementCardSelectionKnown = true
	runtime.settlementCardSelected = 0
	runtime.settlementCardSelectionSent = true
	runtime.settlementCardRewardCommitted = true
	runtime.advanceSettlementPhase(currentDungeonSettlementPhaseRewardCommitted)
}
