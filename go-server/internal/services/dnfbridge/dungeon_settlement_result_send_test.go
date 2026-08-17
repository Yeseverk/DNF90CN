package dnfbridge

import (
	"bytes"
	"errors"
	"testing"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func TestHandleDungeonSetPlayResultSendsFrozenCommittedPlanInCurrentEXEOrder(t *testing.T) {
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
		t.Fatalf("unexpected trailing settlement stream=%x", rest)
	}
	if op34.Header.Classification != 0 || op34.Header.MsgID != currentDungeonPlayResultNoticeMsgID ||
		!bytes.Equal(op34.Body, plan.PlayResultBody) {
		t.Fatalf("op34 packet class=%d msg=%d body=%x", op34.Header.Classification, op34.Header.MsgID, op34.Body)
	}
	if op37.Header.Classification != 0 || op37.Header.MsgID != currentDungeonCharacterStateMsgID ||
		!bytes.Equal(op37.Body, plan.CharacterBody) {
		t.Fatalf("op37 packet class=%d msg=%d body=%x", op37.Header.Classification, op37.Header.MsgID, op37.Body)
	}
	if op35.Header.Classification != 0 || op35.Header.MsgID != currentDungeonClearRewardMsgID ||
		!bytes.Equal(op35.Body, plan.ClearRewardBody) {
		t.Fatalf("op35 packet class=%d msg=%d body_len=%d", op35.Header.Classification, op35.Header.MsgID, len(op35.Body))
	}
	if !runtime.settlementResultNoticeSent || !runtime.settlementCharacterStateSent || !runtime.settlementClearRewardSent {
		t.Fatalf("settlement send state=%+v", runtime)
	}
	if runtime.settlementCardScrollStateSent || runtime.settlementCardLayoutSent ||
		runtime.settlementCardSelectionSent {
		t.Fatalf("op46 must stop after rating chain: %+v", runtime)
	}

	written := connection.write.Len()
	if err := service.handleDungeonSetPlayResult(session, append([]byte(nil), request...)); err != nil {
		t.Fatal(err)
	}
	if connection.write.Len() != written {
		t.Fatalf("exact op46 replay resent completed plan bytes=%d", connection.write.Len()-written)
	}
}

func TestCompletedDungeonDoesNotSynthesizeOp46OrRatingPackets(t *testing.T) {
	_, runtime, _, connection, _ := prepareCompletedSettlementRuntime(t)
	connection.write.Reset()

	// The removed fallback used to forge an op46 about 800ms after op31. Wait
	// beyond that boundary and prove the server remains at the clear-entry gate.
	time.Sleep(900 * time.Millisecond)
	if connection.write.Len() != 0 || runtime.settlementPlayResultReceived ||
		runtime.settlementResultNoticeSent || runtime.settlementCharacterStateSent ||
		runtime.settlementClearRewardSent || runtime.settlementCardScrollStateSent ||
		runtime.settlementCardLayoutSent {
		t.Fatalf("missing client op46 advanced settlement bytes=%x state=%+v", connection.write.Bytes(), runtime)
	}
}

func TestHandleDungeonSetPlayResultResumesAfterMiddlePacketWriteFailure(t *testing.T) {
	service, runtime, session, _, _ := prepareCompletedSettlementRuntime(t)

	const clientRankPoint = byte(91)
	plan := currentDungeonSettlementPacketPlanForTest(t, runtime, clientRankPoint)
	runtime.settlementResultPlan = &plan
	request := make([]byte, currentDungeonPlayResultBaseSize)
	request[currentDungeonPlayResultClientRankPointOffset] = clientRankPoint
	wantErr := errors.New("settlement op37 write failed")
	connection := &failNthDungeonWriteConn{failAt: 2, err: wantErr}
	session.conn = connection

	if err := service.handleDungeonSetPlayResult(session, request); !errors.Is(err, wantErr) {
		t.Fatalf("first op46 error=%v want=%v", err, wantErr)
	}
	if !runtime.settlementResultNoticeSent || runtime.settlementCharacterStateSent || runtime.settlementClearRewardSent {
		t.Fatalf("failed middle write committed wrong flags=%+v", runtime)
	}
	if err := service.handleDungeonSetPlayResult(session, append([]byte(nil), request...)); err != nil {
		t.Fatal(err)
	}
	if !runtime.settlementResultNoticeSent || !runtime.settlementCharacterStateSent || !runtime.settlementClearRewardSent {
		t.Fatalf("retry did not complete flags=%+v", runtime)
	}

	op34, rest := splitGameServerUpperPacket(t, connection.write.Bytes())
	op37, rest := splitGameServerUpperPacket(t, rest)
	op35, rest := splitGameServerUpperPacket(t, rest)
	if len(rest) != 0 || op34.Header.MsgID != currentDungeonPlayResultNoticeMsgID ||
		op37.Header.MsgID != currentDungeonCharacterStateMsgID || op35.Header.MsgID != currentDungeonClearRewardMsgID {
		t.Fatalf("resumed order op34=%d op37=%d op35=%d rest=%x",
			op34.Header.MsgID, op37.Header.MsgID, op35.Header.MsgID, rest)
	}
	if runtime.settlementCardScrollStateSent || runtime.settlementCardLayoutSent ||
		runtime.settlementCardSelectionSent {
		t.Fatalf("retried op46 must stop after rating chain: %+v", runtime)
	}
}

func TestHandleDungeonSetPlayResultDoesNotSendPlanForDifferentOp46Rank(t *testing.T) {
	service, runtime, session, connection, _ := prepareCompletedSettlementRuntime(t)
	connection.write.Reset()

	plan := currentDungeonSettlementPacketPlanForTest(t, runtime, 44)
	runtime.settlementResultPlan = &plan
	request := make([]byte, currentDungeonPlayResultBaseSize)
	request[currentDungeonPlayResultClientRankPointOffset] = 45
	if err := service.handleDungeonSetPlayResult(session, request); err != nil {
		t.Fatal(err)
	}
	if connection.write.Len() != 0 || runtime.settlementResultNoticeSent ||
		runtime.settlementCharacterStateSent || runtime.settlementClearRewardSent {
		t.Fatalf("mismatched rank emitted settlement bytes=%x state=%+v", connection.write.Bytes(), runtime)
	}
}

func TestCurrentDungeonSettlementPlanRejectsOwnerRankAndZeroRewardMismatch(t *testing.T) {
	_, runtime, _, _, _ := prepareCompletedSettlementRuntime(t)
	const clientRankPoint = byte(55)
	character := runtime.Character
	character.Level = 5
	character.Stats = map[string]int64{"exp": 1234}
	notice := currentDungeonPlayResultNotice{
		RankGrade:      3,
		ClearTimeMS:    2222,
		RankPoint:      clientRankPoint,
		Participants:   []currentDungeonPlayResultParticipant{{ObjectKey: 99, ClearTimeMS: 2222}},
		TimeBonusPoint: 0,
	}
	reward := currentDungeonClearRewardSnapshot{
		CharacterID: 99, CompletionKey: "test-run", Source: "committed_test", Committed: true,
		Base: [4]uint32{100}, Tail: currentDungeonClearRewardTail{ShowResult: true},
	}

	wrongOwner := reward
	wrongOwner.CharacterID = 98
	if _, err := buildCurrentDungeonSettlementPacketPlan(clientRankPoint, notice, wrongOwner, character, dnfrepo.SkillPointState{}); !errors.Is(err, errCurrentDungeonSettlementPlanOwner) {
		t.Fatalf("wrong owner error=%v", err)
	}
	if _, err := buildCurrentDungeonSettlementPacketPlan(clientRankPoint+1, notice, reward, character, dnfrepo.SkillPointState{}); !errors.Is(err, errCurrentDungeonSettlementPlanShape) {
		t.Fatalf("wrong rank error=%v", err)
	}
	zeroReward := reward
	zeroReward.Base = [4]uint32{}
	if _, err := buildCurrentDungeonSettlementPacketPlan(clientRankPoint, notice, zeroReward, character, dnfrepo.SkillPointState{}); !errors.Is(err, errCurrentDungeonClearRewardShape) {
		t.Fatalf("zero reward error=%v", err)
	}
}

func currentDungeonSettlementPacketPlanForTest(
	t *testing.T,
	runtime *runtimeDungeonState,
	clientRankPoint byte,
) currentDungeonSettlementPacketPlan {
	t.Helper()
	character := runtime.Character
	character.Level = 5
	character.Stats = map[string]int64{"exp": 12345, "bonus_sp": 25}
	completionKey := runtime.clearMapCompletionKey
	if completionKey == "" {
		completionKey = "committed-test-run"
	}
	notice := currentDungeonPlayResultNotice{
		RankGrade:       3,
		ClearTimeMS:     12345,
		RankPoint:       clientRankPoint,
		AllVisitedClear: true,
		Participants: []currentDungeonPlayResultParticipant{{
			ObjectKey: 99, ClearTimeMS: 12345, NewBest: true,
		}},
	}
	reward := currentDungeonClearRewardSnapshot{
		CharacterID: 99, CompletionKey: completionKey, Source: "committed_test_reward", Committed: true,
		Base: [4]uint32{100}, Tail: currentDungeonClearRewardTail{ShowResult: true},
	}
	plan, err := buildCurrentDungeonSettlementPacketPlan(
		clientRankPoint,
		notice,
		reward,
		character,
		dnfrepo.SkillPointState{RemainingSP: 30, RemainingTP: 1},
	)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}
