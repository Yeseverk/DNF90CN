package inventory

import (
	"bytes"
	"testing"
)

func TestDecodeUnsealRandomOptionRequestCurrentLayout(t *testing.T) {
	got, err := DecodeUnsealRandomOptionRequest([]byte{0x39, 0x00, 0xFF, 0xFF})
	if err != nil {
		t.Fatal(err)
	}
	if got.TargetSlotIndex != 57 || got.InventoryManagerState != 0xFFFF {
		t.Fatalf("request = %+v", got)
	}
	for _, body := range [][]byte{{}, {9, 0, 0}, {9, 0, 0, 0, 0}, {0, 0x80, 0, 0}} {
		if _, err := DecodeUnsealRandomOptionRequest(body); err == nil {
			t.Fatalf("body % X accepted", body)
		}
	}
}

func TestDecodeChangeRandomOptionRequestCurrentLayout(t *testing.T) {
	got, err := DecodeChangeRandomOptionRequest([]byte{0x09, 0x00, 0x00, 0x00, 0x02})
	if err != nil {
		t.Fatal(err)
	}
	if got.TargetSlotIndex != 9 || got.Reserved != 0 || got.OptionIndex != 2 {
		t.Fatalf("request = %+v", got)
	}
	for _, body := range [][]byte{{}, {9, 0, 0, 0}, {9, 0, 0, 0, 0, 0}, {0, 0x80, 0, 0, 0}, {9, 0, 1, 0, 0}} {
		if _, err := DecodeChangeRandomOptionRequest(body); err == nil {
			t.Fatalf("body % X accepted", body)
		}
	}
}

func TestBuildRandomOptionBodies(t *testing.T) {
	if got := buildRandomOptionStatusAck(true); !bytes.Equal(got, []byte{1}) {
		t.Fatalf("success ACK = % X", got)
	}
	if got := buildRandomOptionStatusAck(false); !bytes.Equal(got, []byte{0}) {
		t.Fatalf("failure ACK = % X", got)
	}
	raw := make([]byte, currentItemListEntrySize)
	raw[0x47], raw[0x48], raw[0x4B], raw[0x4E] = 1, 7, 8, 9
	body := buildCommonItemListUpdateBody(listTypeMain, []commonItemListEntry{{slot: 57, stack: testRandomOptionStack(700, raw)}})
	if len(body) != 3+currentItemListEntrySize || body[0] != listTypeMain || body[1] != 1 || body[2] != 0 {
		t.Fatalf("op14 body header/length = % X len=%d", body[:3], len(body))
	}
	row := body[3:]
	if row[0] != 57 || row[1] != 0 || row[0x47] != 1 || row[0x48] != 7 || row[0x4B] != 8 || row[0x4E] != 9 {
		t.Fatalf("op14 row = % X", row)
	}
}
