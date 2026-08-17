package dnfbridge

import (
	"encoding/binary"
	"errors"
	"testing"
)

func TestDecodeCurrentDungeonCardRequestsUseExactCurrentShapes(t *testing.T) {
	if err := validateCurrentDungeonBodylessRequest(nil); err != nil {
		t.Fatalf("bodyless request: %v", err)
	}
	if err := validateCurrentDungeonBodylessRequest([]byte{0}); !errors.Is(err, errCurrentDungeonCardPacketShape) {
		t.Fatalf("non-bodyless error = %v", err)
	}
	op71, err := decodeCurrentDungeonOp71Request([]byte{2, 3})
	if err != nil || op71.ValueA != 2 || op71.ValueB != 3 || op71.ValueCount != 2 {
		t.Fatalf("op71 = %+v err=%v", op71, err)
	}
	op71, err = decodeCurrentDungeonOp71Request([]byte{7})
	if err != nil || op71.ValueA != 7 || op71.ValueB != 0 || op71.ValueCount != 1 {
		t.Fatalf("one-byte op71 = %+v err=%v", op71, err)
	}
	// The legacy game transport pads the op71 body (live bodies run 8 bytes);
	// the decoder must read the proved one/two-byte prefix and ignore the rest.
	op71, err = decodeCurrentDungeonOp71Request([]byte{2, 3, 0, 0, 0, 0, 0, 0})
	if err != nil || op71.ValueA != 2 || op71.ValueB != 3 || op71.ValueCount != 2 {
		t.Fatalf("padded op71 = %+v err=%v", op71, err)
	}
	for _, invalid := range [][]byte{nil, {}} {
		if _, err := decodeCurrentDungeonOp71Request(invalid); !errors.Is(err, errCurrentDungeonCardPacketShape) {
			t.Fatalf("invalid op71 body=%x error=%v", invalid, err)
		}
	}
	op72, err := decodeCurrentDungeonOp72Request([]byte{1, 2})
	if err != nil || op72.ValueA != 1 || op72.ValueB != 2 {
		t.Fatalf("op72 = %+v err=%v", op72, err)
	}
}

func TestBuildCurrentDungeonOp69SuccessIsOnlyCommonSuccess(t *testing.T) {
	if body := buildCurrentDungeonOp69SuccessBody(); len(body) != 1 || body[0] != 1 {
		t.Fatalf("op69 success = %x", body)
	}
}

func TestBuildCurrentDungeonOp70SuccessMatchesBothConditionalReaderShapes(t *testing.T) {
	if body := buildCurrentDungeonOp70CommonOnlySuccessBody(); len(body) != 1 || body[0] != 1 {
		t.Fatalf("op70 common-only success = %x", body)
	}
	values := [dungeonCardWireSlotCount]uint16{0x0102, 0x0304, 0x0506, 0x0708, 0x090A, 0x0B0C, 0x0D0E, 0x0F10}
	body := buildCurrentDungeonOp70EightValueSuccessBody(values)
	if len(body) != 17 || body[0] != 1 {
		t.Fatalf("op70 eight-value len/success = %d/%d", len(body), body[0])
	}
	for index, want := range values {
		if got := binary.LittleEndian.Uint16(body[1+index*2 : 1+(index+1)*2]); got != want {
			t.Fatalf("op70 value[%d]=%04x want=%04x", index, got, want)
		}
	}
}

func TestBuildCurrentDungeonOp71SuccessMatchesEightSlotReader(t *testing.T) {
	var slots [dungeonCardWireSlotCount]currentDungeonOp71Slot
	slots[0] = currentDungeonOp71Slot{
		StateA: 0x10,
		StateB: 0x11,
		Rewards: []currentDungeonOp71RewardTuple{
			{ValueA: 0x11223344, ValueB: 0x55667788},
		},
		TerminalFlag: 0x12,
	}
	for index := 1; index < len(slots); index++ {
		slots[index].StateA = byte(index)
		slots[index].StateB = byte(index + 0x20)
		slots[index].TerminalFlag = byte(index + 0x40)
	}
	body, err := buildCurrentDungeonOp71SuccessBody(slots)
	if err != nil {
		t.Fatalf("build op71: %v", err)
	}
	if len(body) != 41 || body[0] != 1 {
		t.Fatalf("op71 len/success = %d/%d", len(body), body[0])
	}
	if body[1] != 0x10 || body[2] != 0x11 || body[3] != 1 || binary.LittleEndian.Uint32(body[4:8]) != 0x11223344 || binary.LittleEndian.Uint32(body[8:12]) != 0x55667788 || body[12] != 0x12 {
		t.Fatalf("op71 first slot = %x", body[1:13])
	}
	if body[13] != 1 || body[14] != 0x21 || body[15] != 0 || body[16] != 0x41 {
		t.Fatalf("op71 second slot = %x", body[13:17])
	}
}

func TestBuildCurrentDungeonOp71MinimumBodyIs33Bytes(t *testing.T) {
	var slots [dungeonCardWireSlotCount]currentDungeonOp71Slot
	body, err := buildCurrentDungeonOp71SuccessBody(slots)
	if err != nil {
		t.Fatalf("build empty op71: %v", err)
	}
	if len(body) != 33 {
		t.Fatalf("minimum op71 body len = %d, want 33", len(body))
	}
}

func TestBuildCurrentDungeonOp72BodiesMatchReader(t *testing.T) {
	if got := buildCurrentDungeonOp72SuccessBody(7, 9); len(got) != 3 || got[0] != 1 || got[1] != 7 || got[2] != 9 {
		t.Fatalf("op72 success = %x", got)
	}
	if got := buildCurrentDungeonOp72FailureBody(4); len(got) != 2 || got[0] != 0 || got[1] != 4 {
		t.Fatalf("op72 failure = %x", got)
	}
}
