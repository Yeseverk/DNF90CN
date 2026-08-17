package inventory

import (
	"context"
	"encoding/binary"
	"strings"
	"testing"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
)

func TestHandlerRecognizesDirectInventoryCommand(t *testing.T) {
	handler := NewHandler()
	got, err := handler.Handle(context.Background(), alignedcmd.Request{
		Opcode: uint16(dnfenum.CmdPacketDisjointItem),
		Body:   []byte{0x02, 0x00, 0x00, 0x04, 0x00},
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled {
		t.Fatalf("disjoint item should be handled by inventory module")
	}
	if got.ResponseAllowed {
		t.Fatalf("inventory module must not allow response before writer evidence is complete")
	}
	if got.Operation != "disjoint_item" {
		t.Fatalf("operation = %q, want disjoint_item", got.Operation)
	}
	if got.Reason == "" {
		t.Fatalf("pending reason is empty")
	}
}

func TestHandlerRejectsNonInventoryOpcode(t *testing.T) {
	handler := NewHandler()
	got, err := handler.Handle(context.Background(), alignedcmd.Request{Opcode: uint16(dnfenum.CmdPacketMailboxOpen)})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if got.Handled {
		t.Fatalf("mailbox opcode must not be handled by inventory module")
	}
}

func TestDecodeDeleteRequestExtended(t *testing.T) {
	body := []byte{
		0x00, 0x01,
		0x02, 0x00,
		0x05, 0x00,
		0x44, 0x33, 0x22, 0x11,
		0x03, 0x00, 0x00, 0x00,
		0x00,
	}
	got, err := DecodeDeleteRequest(body)
	if err != nil {
		t.Fatalf("DecodeDeleteRequest error = %v", err)
	}
	if !got.Extended || got.ListType != listTypeMain || len(got.Entries) != 1 {
		t.Fatalf("decoded extended header = %+v", got)
	}
	entry := got.Entries[0]
	if entry.OpType != 2 || entry.SlotIndex != 5 || entry.ItemID != 0x11223344 || entry.DeleteCount != 3 {
		t.Fatalf("decoded entry = %+v", entry)
	}
}

func TestDecodeCurrentSortItemRequestFromPacketLog(t *testing.T) {
	body := []byte{0x06, 0x00, 0x00, 0x00, 0x10, 0x00, 0x18, 0x02, 0x20, 0x00}
	got, err := DecodeSortItemRequest(body)
	if err != nil {
		t.Fatalf("DecodeSortItemRequest error = %v", err)
	}
	if got.ListType != listTypeMain || got.Category != 2 || got.Condition != 0 {
		t.Fatalf("decoded sort request = %+v", got)
	}
}

func TestDecodeCurrentDeleteRequestFromPacketLog(t *testing.T) {
	body := []byte{
		0x11, 0x00, 0x00, 0x00,
		0x10, 0x00,
		0x1A, 0x0B,
		0x08, 0x01,
		0x10, 0x09,
		0x18, 0x8B, 0x8A, 0xA8, 0x37,
		0x20, 0x01,
		0x20, 0x00,
		0x00,
	}
	got, err := DecodeDeleteRequest(body)
	if err != nil {
		t.Fatalf("DecodeDeleteRequest error = %v", err)
	}
	if !got.Extended || got.ListType != listTypeMain || len(got.Entries) != 1 {
		t.Fatalf("decoded delete request = %+v", got)
	}
	entry := got.Entries[0]
	if entry.OpType != 1 || entry.SlotIndex != 9 || entry.ItemID != 116000011 || entry.DeleteCount != 1 {
		t.Fatalf("decoded delete entry = %+v", entry)
	}
}

func TestDecodeCurrentDeleteRequestWithSkillStateTrailer(t *testing.T) {
	body := []byte{
		0x10, 0x00, 0x00, 0x00,
		0x10, 0x00,
		0x1A, 0x0A,
		0x08, 0x02,
		0x10, 0xE6, 0x02,
		0x18, 0xDD, 0x17,
		0x20, 0x01,
		0x20, 0x00,
		0x01,
	}
	got, err := DecodeDeleteRequest(body)
	if err != nil {
		t.Fatalf("DecodeDeleteRequest error = %v", err)
	}
	if !got.Extended || got.ListType != listTypeMain || len(got.Entries) != 1 {
		t.Fatalf("decoded delete request = %+v", got)
	}
	entry := got.Entries[0]
	if entry.OpType != 2 || entry.SlotIndex != 358 || entry.ItemID != 3037 || entry.DeleteCount != 1 {
		t.Fatalf("decoded skill material delete entry = %+v", entry)
	}
}

func TestCurrentInventoryProtobufRequestsRejectMalformedBodies(t *testing.T) {
	tests := []struct {
		name   string
		decode func([]byte) error
		body   []byte
	}{
		{
			name: "sort truncated envelope",
			decode: func(body []byte) error {
				_, err := DecodeSortItemRequest(body)
				return err
			},
			body: []byte{0x06, 0, 0, 0, 0x10, 0},
		},
		{
			name: "sort wrong wire type",
			decode: func(body []byte) error {
				_, err := DecodeSortItemRequest(body)
				return err
			},
			body: []byte{0x06, 0, 0, 0, 0x12, 0, 0x18, 2, 0x20, 0},
		},
		{
			name: "sort byte overflow",
			decode: func(body []byte) error {
				_, err := DecodeSortItemRequest(body)
				return err
			},
			body: []byte{0x07, 0, 0, 0, 0x10, 0x80, 0x02, 0x18, 2, 0x20, 0},
		},
		{
			name: "delete non-zero terminator",
			decode: func(body []byte) error {
				_, err := DecodeDeleteRequest(body)
				return err
			},
			body: []byte{0x11, 0, 0, 0, 0x10, 0, 0x1A, 0x0B, 0x08, 1, 0x10, 9, 0x18, 0x8B, 0x8A, 0xA8, 0x37, 0x20, 1, 0x20, 0, 2},
		},
		{
			name: "delete missing item id",
			decode: func(body []byte) error {
				_, err := DecodeDeleteRequest(body)
				return err
			},
			body: []byte{0x0C, 0, 0, 0, 0x10, 0, 0x1A, 5, 0x08, 1, 0x10, 9, 0x20, 1, 0x20, 0},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.decode(test.body); err == nil {
				t.Fatal("malformed current protobuf body unexpectedly accepted")
			}
		})
	}
}

func TestDecodeCurrentDeleteRequestRejectsMoreThan100Entries(t *testing.T) {
	payload := []byte{0x10, listTypeMain}
	entry := []byte{0x1A, 0x08, 0x08, 1, 0x10, 1, 0x18, 1, 0x20, 1}
	for range maxDeleteEntries + 1 {
		payload = append(payload, entry...)
	}
	payload = append(payload, 0x20, 0)
	body := make([]byte, 4, 4+len(payload))
	binary.LittleEndian.PutUint32(body, uint32(len(payload)))
	body = append(body, payload...)

	if _, err := DecodeDeleteRequest(body); err == nil || !strings.Contains(err.Error(), "more than 100 entries") {
		t.Fatalf("DecodeDeleteRequest error = %v, want entry limit rejection", err)
	}
}

func TestDecodeDeleteOrSellRequestFormats(t *testing.T) {
	withList, err := DecodeDeleteOrSellRequest([]byte{listTypePet, 0x10, 0x00, 0x02, 0x00})
	if err != nil {
		t.Fatalf("DecodeDeleteOrSellRequest with list error = %v", err)
	}
	if !withList.HasListType || withList.ListType != listTypePet || withList.SlotIndex != 16 || withList.Count != 2 {
		t.Fatalf("with list = %+v", withList)
	}

	legacy, err := DecodeDeleteOrSellRequest([]byte{0x10, 0x00, 0x03, 0x00})
	if err != nil {
		t.Fatalf("DecodeDeleteOrSellRequest legacy error = %v", err)
	}
	if legacy.HasListType || legacy.ListType != listTypeMain || legacy.SlotIndex != 16 || legacy.Count != 3 {
		t.Fatalf("legacy = %+v", legacy)
	}
}

func TestDecodeMoveItemspaceRequest(t *testing.T) {
	body := []byte{
		listTypeMain,
		0x02, 0x00,
		0x44, 0x33, 0x22, 0x11,
		0x05, 0x00, 0x00, 0x00,
		listTypePersonalCargo,
		0x09, 0x00,
		0x88, 0x77, 0x66, 0x55,
		0x03, 0x00, 0x00, 0x00,
		0xFF, 0xFF, 0xFF, 0xFF,
		0x00, 0x00,
	}
	got, err := DecodeMoveItemspaceRequest(body)
	if err != nil {
		t.Fatalf("DecodeMoveItemspaceRequest error = %v", err)
	}
	if got.SourceListType != listTypeMain || got.SourceSlotIndex != 2 || got.SourceInstanceValue != 0x11223344 || got.MoveCount != 5 {
		t.Fatalf("source = %+v", got)
	}
	if got.DestinationListType != listTypePersonalCargo || got.DestinationSlotIndex != 9 || got.DestinationInstanceValue != 0x55667788 || got.DestinationStack != 3 {
		t.Fatalf("destination = %+v", got)
	}
	if got.ActorIndex != -1 || got.TrailingState0 != 0 || got.TrailingState1 != 0 {
		t.Fatalf("current tail = %+v", got)
	}
	if _, err := DecodeMoveItemspaceRequest(body[:22]); err == nil {
		t.Fatal("22-byte legacy/partial move body unexpectedly accepted")
	}
}

func TestDecodeUseStackableRequest(t *testing.T) {
	body := []byte{
		0x04, 0x00,
		listTypePet,
		0x78, 0x56, 0x34, 0x12,
		0x21, 0x43, 0x65, 0x07,
		0x00, 0x00, 0x00, 0x00,
	}
	got, err := DecodeUseStackableRequest(body)
	if err != nil {
		t.Fatalf("DecodeUseStackableRequest error = %v", err)
	}
	if got.SlotIndex != 4 || got.ListType != listTypePet || got.InstanceValue != 0x12345678 || uint32(got.ItemCode) != 0x07654321 || got.Reserved != 0 {
		t.Fatalf("got = %+v", got)
	}
	if _, err := DecodeUseStackableRequest(body[:14]); err == nil {
		t.Fatal("14-byte use-stackable body unexpectedly accepted")
	}
	if _, err := DecodeUseStackableRequest(append(append([]byte(nil), body...), 0)); err == nil {
		t.Fatal("16-byte use-stackable body unexpectedly accepted")
	}
	reserved := append([]byte(nil), body...)
	reserved[11] = 1
	if _, err := DecodeUseStackableRequest(reserved); err == nil {
		t.Fatal("non-zero use-stackable reserved field unexpectedly accepted")
	}
}

func TestDecodeUpgradeAndEnchantRequests(t *testing.T) {
	upgradeBody := []byte{
		0x01, 0x00,
		0x08, 0x00,
		0x44, 0x33, 0x22, 0x11,
		0x0A, 0x00,
		0xFF, 0xFF,
		0x03, 0x00, 0x00, 0x00,
		'a', 'b', 'c',
	}
	upgrade, err := DecodeUpgradeItemRequest(upgradeBody)
	if err != nil {
		t.Fatalf("DecodeUpgradeItemRequest error = %v", err)
	}
	if upgrade.Mode != "amplify" || upgrade.TargetSlotIndex != 8 || upgrade.TargetItemTemplateID != 0x11223344 || upgrade.MaterialSlotIndex != 10 || upgrade.OptionalTicketSlotIndex != -1 || upgrade.TargetItemName != "abc" {
		t.Fatalf("upgrade = %+v", upgrade)
	}

	enchant, err := DecodeEnchantByBeadRequest([]byte{listTypeMain, 0x03, 0x00, listTypeEquipment, 0x09, 0x00})
	if err != nil {
		t.Fatalf("DecodeEnchantByBeadRequest error = %v", err)
	}
	if enchant.BeadListType != listTypeMain || enchant.BeadSlotIndex != 3 || enchant.TargetListType != listTypeEquipment || enchant.TargetSlotIndex != 9 {
		t.Fatalf("enchant = %+v", enchant)
	}
}

func TestMoveCommandRecordsInventoryOwnerGap(t *testing.T) {
	cmd := NewMoveCommand(alignedcmd.Request{
		AccountID:           " acc ",
		SelectedCharacterID: 77,
	}, MoveItemspaceRequest{
		SourceListType:           listTypeMain,
		SourceSlotIndex:          2,
		SourceInstanceValue:      0x11223344,
		MoveCount:                5,
		DestinationListType:      listTypeEquipment,
		DestinationSlotIndex:     9,
		DestinationInstanceValue: 0x55667788,
		DestinationStack:         3,
		ActorIndex:               -1,
		TrailingState0:           4,
		TrailingState1:           5,
	})
	summary := cmd.String()
	for _, want := range []string{`account="acc"`, "char=77", "src=(0,2,0x11223344)", "dst=(3,9,0x55667788)", "actor=-1", "tail=(4,5)", "mutation id", "USERINFO"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary %q missing %q", summary, want)
		}
	}
}

func TestDisjointCommandRecordsRewardOwnerGap(t *testing.T) {
	cmd := NewDisjointCommand(alignedcmd.Request{
		AccountID:           "acc",
		SelectedCharacterID: 88,
	}, DisjointItemRequest{
		TargetSlotIndex:       4,
		ItemSpace:             listTypeMain,
		DisjointItemSlotIndex: 9,
		ContextValue:          0x12345678,
	})
	summary := cmd.String()
	for _, want := range []string{"targetSlot=4", "disjointSlot=9", "0x12345678", "disjoint reward owner", "popup"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary %q missing %q", summary, want)
		}
	}
}
