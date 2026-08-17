package inventory

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestDecodePurifyItemRequestExactCurrentEXELayout(t *testing.T) {
	body := make([]byte, 12)
	binary.LittleEndian.PutUint16(body[0:2], 9)
	binary.LittleEndian.PutUint32(body[2:6], 700)
	binary.LittleEndian.PutUint16(body[6:8], 12)
	binary.LittleEndian.PutUint32(body[8:12], 1183)
	request, err := DecodePurifyItemRequest(body)
	if err != nil {
		t.Fatal(err)
	}
	if request.TargetSlotIndex != 9 || request.TargetItemTemplateID != 700 || request.MaterialSlotIndex != 12 || request.MaterialItemTemplateID != 1183 {
		t.Fatalf("request = %+v", request)
	}
	if _, err := DecodePurifyItemRequest(append(body, 0)); err == nil {
		t.Fatal("op204 accepted trailing bytes")
	}
}

func TestDecodeInvestItemAmplifyOptionRequestConditionalDSTR(t *testing.T) {
	fixed := make([]byte, 14)
	fixed[0] = investAmplifyActionInvest
	binary.LittleEndian.PutUint16(fixed[1:3], 9)
	binary.LittleEndian.PutUint32(fixed[3:7], 700)
	binary.LittleEndian.PutUint16(fixed[7:9], 12)
	binary.LittleEndian.PutUint32(fixed[9:13], 1286)
	fixed[13] = 3
	request, err := DecodeInvestItemAmplifyOptionRequest(fixed)
	if err != nil {
		t.Fatal(err)
	}
	if request.Action != 0 || request.SelectedOption != 3 || request.TargetItemName != "" {
		t.Fatalf("fixed request = %+v", request)
	}
	if _, err := DecodeInvestItemAmplifyOptionRequest(append(fixed, 0)); err == nil {
		t.Fatal("non-Pure-Gold op205 accepted trailing bytes")
	}

	pureGold := append([]byte(nil), fixed...)
	pureGold[0] = investAmplifyActionPureGold
	name := []byte("target-name")
	length := make([]byte, 4)
	binary.LittleEndian.PutUint32(length, uint32(len(name)))
	pureGold = append(pureGold, length...)
	pureGold = append(pureGold, name...)
	request, err = DecodeInvestItemAmplifyOptionRequest(pureGold)
	if err != nil {
		t.Fatal(err)
	}
	if request.Action != investAmplifyActionPureGold || request.TargetItemName != string(name) {
		t.Fatalf("Pure Gold request = %+v", request)
	}
	if _, err := DecodeInvestItemAmplifyOptionRequest(pureGold[:len(pureGold)-1]); err == nil {
		t.Fatal("Pure Gold op205 accepted truncated DSTR")
	}
}

func TestAmplifyACKLayoutsMatchCurrentEXEReaders(t *testing.T) {
	result := AmplifyMutationResult{
		Action:                 investAmplifyActionPureGold,
		MaterialSlotIndex:      12,
		MaterialRemainingCount: 2,
		TargetSlotIndex:        9,
		AmplifyType:            3,
		AmplifyValue:           0x1234,
		AmplifyLevel:           7,
	}
	wantPurify := []byte{1, 12, 0, 2, 0, 0, 0, 9, 0, 3, 0x34, 0x12}
	if got := buildPurifyItemSuccessAck(result); !bytes.Equal(got, wantPurify) {
		t.Fatalf("op204 success = %x, want %x", got, wantPurify)
	}
	if got := buildPurifyItemErrorAck(); !bytes.Equal(got, []byte{0, 1}) {
		t.Fatalf("op204 error = %x", got)
	}
	wantInvest := []byte{1, 2, 12, 0, 2, 0, 0, 0, 9, 0, 3, 0x34, 0x12, 7}
	if got := buildInvestItemAmplifyOptionSuccessAck(result); !bytes.Equal(got, wantInvest) {
		t.Fatalf("op205 success = %x, want %x", got, wantInvest)
	}
	if got := buildInvestItemAmplifyOptionErrorAck(23); !bytes.Equal(got, []byte{0, 23}) {
		t.Fatalf("op205 error = %x", got)
	}
}
