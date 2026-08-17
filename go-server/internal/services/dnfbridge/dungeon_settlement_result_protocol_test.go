package dnfbridge

import (
	"encoding/binary"
	"errors"
	"testing"
)

func TestCurrentDungeonPlayResultNoticeMatchesCurrentOp34Reader(t *testing.T) {
	body, err := buildCurrentDungeonPlayResultNoticeBody(currentDungeonPlayResultNotice{
		RankGrade:       99,
		ClearTimeMS:     12345,
		TimeBonusPoint:  7,
		RankPoint:       99,
		AllVisitedClear: true,
		Participants: []currentDungeonPlayResultParticipant{{
			ObjectKey:   20,
			ClearTimeMS: 12345,
			NewBest:     true,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != 16 {
		t.Fatalf("op34 body length = %d, want 16", len(body))
	}
	if body[0] != 99 || binary.LittleEndian.Uint32(body[1:5]) != 12345 ||
		body[5] != 7 || body[6] != 99 || body[7] != 1 || body[8] != 1 ||
		binary.LittleEndian.Uint16(body[9:11]) != 20 ||
		binary.LittleEndian.Uint32(body[11:15]) != 12345 || body[15] != 1 {
		t.Fatalf("unexpected op34 body: %x", body)
	}
}

func TestCurrentDungeonPlayResultNoticeRejectsUnownedParticipantShape(t *testing.T) {
	if _, err := buildCurrentDungeonPlayResultNoticeBody(currentDungeonPlayResultNotice{}); !errors.Is(err, errCurrentDungeonPlayResultNoticeShape) {
		t.Fatalf("empty participants error = %v", err)
	}
	participants := make([]currentDungeonPlayResultParticipant, currentDungeonPlayResultMaximumParticipants+1)
	for index := range participants {
		participants[index].ObjectKey = uint16(index + 1)
	}
	if _, err := buildCurrentDungeonPlayResultNoticeBody(currentDungeonPlayResultNotice{Participants: participants}); !errors.Is(err, errCurrentDungeonPlayResultNoticeShape) {
		t.Fatalf("overflow participants error = %v", err)
	}
	if _, err := buildCurrentDungeonPlayResultNoticeBody(currentDungeonPlayResultNotice{Participants: []currentDungeonPlayResultParticipant{{}}}); !errors.Is(err, errCurrentDungeonPlayResultNoticeShape) {
		t.Fatalf("zero participant owner error = %v", err)
	}
}

func TestCurrentDungeonClearRewardMatchesCurrentOp35ReaderGrammar(t *testing.T) {
	snapshot := currentDungeonClearRewardSnapshot{
		CharacterID:   20,
		CompletionKey: "run-20-3-1",
		Source:        "committed_test_reward",
		Committed:     true,
		Base:          [4]uint32{11, 12, 13, 14},
		BaseFlag:      15,
		BonusGroupsA:  []currentDungeonClearRewardPair{{Key: 21, Value: 22}},
		BonusGroupsB:  []currentDungeonClearRewardPair{{Key: 23, Value: 24}},
		PostBase:      [currentDungeonClearRewardPostBaseFieldCount]uint32{31, 32, 33, 34, 35, 36, 37},
		Score:         [currentDungeonClearRewardScoreFieldCount]uint32{41, 42, 43, 44},
		Quest:         []currentDungeonClearRewardPair{{Key: 51, Value: 52}},
		Drops: []currentDungeonClearRewardDrop{{
			ObjectKey: 61, TemplateID: 62, StackCount: 63, Value16A: 64, Value8: 65, Value16B: 66,
		}},
		TotalReward:  71,
		PreTailValue: 80,
		Tail: currentDungeonClearRewardTail{
			Value0: 81, ShowResult: true, Flag0: true, MonsterTotalExp: 85, Value2: 86, Value3: 87, Value4: 88,
		},
	}
	snapshot.Bonus[0] = 16
	snapshot.CardSlots[0] = []currentDungeonClearRewardPair{{Key: 67, Value: 68}}
	snapshot.AuxGroupsA[1] = []currentDungeonClearRewardPair{{Key: 72, Value: 73}}
	snapshot.AuxGroupsB[2] = []currentDungeonClearRewardPair{{Key: 74, Value: 75}}

	body, err := buildCurrentDungeonClearRewardBody(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	wantLength := currentDungeonClearRewardMinimumBodySize +
		8*len(snapshot.BonusGroupsA) + 8*len(snapshot.BonusGroupsB) +
		8*len(snapshot.Quest) + 15*len(snapshot.Drops) +
		8 + 8 + 8
	if len(body) != wantLength {
		t.Fatalf("op35 body length = %d, want %d", len(body), wantLength)
	}
	reader := settlementProtocolReader{body: body}
	for _, want := range snapshot.Base {
		if got := reader.u32(); got != want {
			t.Fatalf("base value = %d, want %d", got, want)
		}
	}
	if got := reader.u8(); got != snapshot.BaseFlag {
		t.Fatalf("base flag = %d, want %d", got, snapshot.BaseFlag)
	}
	for _, want := range snapshot.Bonus {
		if got := reader.u32(); got != want {
			t.Fatalf("bonus value = %d, want %d", got, want)
		}
	}
	reader.u8Pairs(t, snapshot.BonusGroupsA)
	reader.u8Pairs(t, snapshot.BonusGroupsB)
	for _, want := range snapshot.PostBase {
		if got := reader.u32(); got != want {
			t.Fatalf("post-base value = %d, want %d", got, want)
		}
	}
	for _, want := range snapshot.Score {
		if got := reader.u32(); got != want {
			t.Fatalf("score value = %d, want %d", got, want)
		}
	}
	if got := reader.u32(); got != uint32(len(snapshot.Quest)) {
		t.Fatalf("quest pair count = %d", got)
	}
	reader.pairs(t, snapshot.Quest)
	if got := reader.u8(); got != byte(len(snapshot.Drops)) {
		t.Fatalf("drop count = %d", got)
	}
	for _, drop := range snapshot.Drops {
		if got := reader.u16(); got != drop.ObjectKey || reader.u32() != drop.TemplateID ||
			reader.u32() != drop.StackCount || reader.u16() != drop.Value16A || reader.u8() != drop.Value8 || reader.u16() != drop.Value16B {
			t.Fatalf("drop row mismatch at offset %d", reader.offset)
		}
	}
	for _, slot := range snapshot.CardSlots {
		reader.u8Pairs(t, slot)
	}
	if got := reader.u32(); got != snapshot.TotalReward {
		t.Fatalf("total reward = %d", got)
	}
	for _, group := range snapshot.AuxGroupsA {
		reader.u8Pairs(t, group)
	}
	for _, group := range snapshot.AuxGroupsB {
		reader.u8Pairs(t, group)
	}
	if got := reader.u32(); got != snapshot.PreTailValue {
		t.Fatalf("pre-tail value = %d, want %d", got, snapshot.PreTailValue)
	}
	tailOffset := reader.offset
	if reader.u32() != snapshot.Tail.Value0 ||
		reader.u8() != boolByte(snapshot.Tail.ShowResult) || reader.u8() != boolByte(snapshot.Tail.Flag0) ||
		reader.u32() != snapshot.Tail.MonsterTotalExp || reader.u32() != snapshot.Tail.Value2 ||
		reader.u32() != snapshot.Tail.Value3 || reader.u32() != snapshot.Tail.Value4 ||
		reader.offset-tailOffset != currentDungeonClearRewardTailSize || reader.offset != len(body) {
		t.Fatalf("tail/read boundary mismatch offset=%d len=%d", reader.offset, len(body))
	}
}

func TestCurrentDungeonClearRewardRejectsUncommittedAndFabricatedZeroSnapshots(t *testing.T) {
	base := currentDungeonClearRewardSnapshot{CharacterID: 20, CompletionKey: "run", Source: "test"}
	if _, err := buildCurrentDungeonClearRewardBody(base); !errors.Is(err, errCurrentDungeonClearRewardUncommitted) {
		t.Fatalf("uncommitted error = %v", err)
	}
	base.Committed = true
	base.Tail.ShowResult = true
	if _, err := buildCurrentDungeonClearRewardBody(base); !errors.Is(err, errCurrentDungeonClearRewardShape) {
		t.Fatalf("zero reward error = %v", err)
	}
	base.Base[0] = 1
	body, err := buildCurrentDungeonClearRewardBody(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != currentDungeonClearRewardMinimumBodySize {
		t.Fatalf("minimum body length = %d", len(body))
	}
}

func TestCurrentDungeonClearRewardMinimumCurrentEXEOffsets(t *testing.T) {
	// All count-bearing sections are empty here. The standalone pre-tail u32
	// starts at 184. The final current reader segment starts at 188 and spans
	// exactly u32/u8/u8/u32/u32/u32/u32 (22 bytes), ending at 210.
	snapshot := currentDungeonClearRewardSnapshot{
		CharacterID: 20, CompletionKey: "run-20-min", Source: "committed_test", Committed: true,
		Base: [4]uint32{333}, Tail: currentDungeonClearRewardTail{ShowResult: true},
	}
	body, err := buildCurrentDungeonClearRewardBody(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if currentDungeonClearRewardMinimumBodySize != 210 {
		t.Fatalf("declared minimum=%d, want current-reader 210", currentDungeonClearRewardMinimumBodySize)
	}
	if len(body) != currentDungeonClearRewardMinimumBodySize {
		t.Fatalf("minimum body length=%d, want current-reader 210", len(body))
	}
	if got := binary.LittleEndian.Uint32(body[0:4]); got != 333 {
		t.Fatalf("base[0]=%d, want committed clear experience 333", got)
	}
	const preTailOffset = 184
	const tailOffset = preTailOffset + 4
	if got := binary.LittleEndian.Uint32(body[preTailOffset:tailOffset]); got != 0 {
		t.Fatalf("neutral pre-tail value=%d, want 0", got)
	}
	if tailOffset+currentDungeonClearRewardTailSize != len(body) {
		t.Fatalf("tail offset=%d size=%d body=%d", tailOffset, currentDungeonClearRewardTailSize, len(body))
	}
	reader := settlementProtocolReader{body: body, offset: tailOffset}
	if reader.u32() != 0 || reader.u8() != 1 || reader.u8() != 0 ||
		reader.u32() != 0 || reader.u32() != 0 || reader.u32() != 0 || reader.u32() != 0 ||
		reader.offset != len(body) {
		t.Fatalf("current-exe op35 tail mismatch offset=%d body=%x", reader.offset, body)
	}
}

type settlementProtocolReader struct {
	body   []byte
	offset int
}

func (r *settlementProtocolReader) u8() byte {
	value := r.body[r.offset]
	r.offset++
	return value
}

func (r *settlementProtocolReader) u16() uint16 {
	value := binary.LittleEndian.Uint16(r.body[r.offset : r.offset+2])
	r.offset += 2
	return value
}

func (r *settlementProtocolReader) u32() uint32 {
	value := binary.LittleEndian.Uint32(r.body[r.offset : r.offset+4])
	r.offset += 4
	return value
}

func (r *settlementProtocolReader) pairs(t *testing.T, want []currentDungeonClearRewardPair) {
	t.Helper()
	for _, pair := range want {
		if gotKey, gotValue := r.u32(), r.u32(); gotKey != pair.Key || gotValue != pair.Value {
			t.Fatalf("pair = (%d,%d), want (%d,%d)", gotKey, gotValue, pair.Key, pair.Value)
		}
	}
}

func (r *settlementProtocolReader) u8Pairs(t *testing.T, want []currentDungeonClearRewardPair) {
	t.Helper()
	if got := int(r.u8()); got != len(want) {
		t.Fatalf("u8 pair count = %d, want %d", got, len(want))
	}
	r.pairs(t, want)
}
