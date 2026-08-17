package party

import (
	"bytes"
	"context"
	"encoding/binary"
	"reflect"
	"testing"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
)

func TestDecodeSetPartyInfoRequestCurrentPresetLayout(t *testing.T) {
	body := []byte{
		0x00, 0x05,
		0x04,
		0x00, 0x00, 0x00, 0x00,
		0x01,
		0x00, 0x00,
		0x00,
		0x01,
		0xe9, 0x12,
	}
	got, err := DecodeSetPartyInfoRequest(body)
	if err != nil {
		t.Fatalf("DecodeSetPartyInfoRequest error = %v", err)
	}
	if got.Prefix0 != 0 || got.Prefix1 != 5 ||
		got.MemberSelectCode != 4 || got.MaxMembers != 4 ||
		got.SelectionID != 0 || got.SelectionCode != 1 || got.SelectionValue != 0 ||
		got.RecruitFlag != 0 || got.TargetMode != 1 || got.TargetDungeonID != 0x12e9 {
		t.Fatalf("decoded preset request = %+v", got)
	}
	if len(got.NameBytes) == 0 {
		t.Fatalf("preset name was not resolved")
	}
}

func TestDecodeSetPartyInfoRequestCurrentSpecialChannelLayout(t *testing.T) {
	body := []byte{
		0x00, 0x02,
		0x04,
		0x00, 0x00, 0x00, 0x00,
		0x05,
		0x00, 0x00,
		0x00,
		0x00,
		0x00, 0x00,
	}
	got, err := DecodeSetPartyInfoRequest(body)
	if err != nil {
		t.Fatalf("DecodeSetPartyInfoRequest error = %v", err)
	}
	if got.SelectionCode != 5 || got.TargetMode != 0 || got.TargetDungeonID != 0 {
		t.Fatalf("decoded special-channel request = %+v", got)
	}
}

func TestDecodeSetPartyInfoRequestCurrentCustomNameLayout(t *testing.T) {
	body := []byte{
		0x00, 0x00,
		0x03, 0x00, 0x00, 0x00,
		'a', 'b', 'c',
		0x04,
		0x44, 0x33, 0x22, 0x11,
		0x02,
		0x66, 0x55,
		0x01,
		0x02,
		0xaa, 0x99,
	}
	got, err := DecodeSetPartyInfoRequest(body)
	if err != nil {
		t.Fatalf("DecodeSetPartyInfoRequest error = %v", err)
	}
	if string(got.NameBytes) != "abc" || got.SelectionID != 0x11223344 ||
		got.SelectionCode != 2 || got.SelectionValue != 0x5566 ||
		got.RecruitFlag != 1 || got.TargetMode != 2 || got.TargetDungeonID != 0x99aa {
		t.Fatalf("decoded custom-name request = %+v", got)
	}
}

func TestDecodeSetPartyInfoRequestRejectsLegacyOrTrailingLayout(t *testing.T) {
	for _, body := range [][]byte{
		{0, 3, 4, 0x9c, 0, 5, 0},
		{0, 3, 4, 0, 0, 0, 0, 1, 0, 0, 0, 1, 0x9c, 0, 0},
	} {
		if _, err := DecodeSetPartyInfoRequest(body); err == nil {
			t.Fatalf("DecodeSetPartyInfoRequest accepted invalid layout: % X", body)
		}
	}
}

func TestBuildCurrentPartyStateSupportsFourRealMembers(t *testing.T) {
	state := alignedcmd.PartyState{
		PartyID:         1,
		UserID:          1001,
		NameBytes:       []byte("party"),
		MaxMembers:      4,
		TargetDungeonID: 0x009c,
		Members: []alignedcmd.PartyMemberState{
			{UserID: 1001, UserState: 1, HPPercent: 100, MPPercent: 100},
			{UserID: 1002, UserState: 1, HPPercent: 90, MPPercent: 80},
			{UserID: 1003, UserState: 1, HPPercent: 70, MPPercent: 60},
			{UserID: 1004, UserState: 1, HPPercent: 50, MPPercent: 40},
		},
	}

	realtime := BuildSingleMemberRealtimeInfo(state)
	if realtime[0] != 4 {
		t.Fatalf("realtime count = %d, want 4", realtime[0])
	}
	for i, want := range []uint16{1001, 1002, 1003, 1004} {
		offset := 1 + i*5
		if got := binary.LittleEndian.Uint16(realtime[offset : offset+2]); got != want {
			t.Fatalf("realtime member %d userID = %d, want %d", i, got, want)
		}
		if got, wantHP := realtime[offset+2], []byte{100, 90, 70, 50}[i]; got != wantHP {
			t.Fatalf("realtime member %d HP = %d, want %d", i, got, wantHP)
		}
		if got := realtime[offset+4]; got != byte(i) {
			t.Fatalf("realtime member %d slot = %d, want %d", i, got, i)
		}
	}
}

func TestBuildPeerEndpointInfoCurrentTwentyTwoByteMemberLayout(t *testing.T) {
	got := BuildPeerEndpointInfo([]PeerEndpoint{
		{UserID: 1001, IPv4: [4]byte{192, 168, 1, 10}, Port: 10000, AccountID: 1001},
		{UserID: 1002, IPv4: [4]byte{127, 0, 0, 1}, Port: 2311, AccountID: 1002},
	})
	if len(got) != 1+2*partyPeerEndpointSize {
		t.Fatalf("peer endpoint body len=%d want=%d body=%x", len(got), 1+2*partyPeerEndpointSize, got)
	}
	want := []byte{
		2,
		0xe9, 0x03,
		192, 168, 1, 10,
		192, 168, 1, 10,
		0x27, 0x10,
		0xe9, 0x03, 0x00, 0x00,
		0,
		0xdc, 0x05, 0x00, 0x00,
		0,
		0xea, 0x03,
		127, 0, 0, 1,
		127, 0, 0, 1,
		0x09, 0x07,
		0xea, 0x03, 0x00, 0x00,
		0,
		0xdc, 0x05, 0x00, 0x00,
		0,
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("peer endpoint body=%x want=%x", got, want)
	}
}

func TestBuildDirectorySnapshotCurrentOp87Layout(t *testing.T) {
	got := BuildDirectorySnapshot([]DirectoryRecord{
		{
			PartyID:     1001,
			SelectionID: 0x12e9,
			MemberIDs:   []uint16{1001, 1002},
		},
	})
	if len(got) != 2+28 {
		t.Fatalf("directory body len = %d, want 30", len(got))
	}
	wantPrefix := []byte{
		1, 0,
		0xe9, 0x03,
		0,
		2,
		0,
		0xff,
		0xe9, 0x12, 0, 0,
		0, 0xe9, 0x03,
		0, 0xea, 0x03,
	}
	if !reflect.DeepEqual(got[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("directory prefix = % X, want % X", got[:len(wantPrefix)], wantPrefix)
	}
	for offset := len(wantPrefix); offset < len(got); offset += 3 {
		if !reflect.DeepEqual(got[offset:offset+3], []byte{0xff, 0xff, 0xff}) {
			t.Fatalf("directory empty slot at %d = % X", offset, got[offset:offset+3])
		}
	}
}

func TestBuildDirectorySnapshotEmpty(t *testing.T) {
	if got := BuildDirectorySnapshot(nil); !reflect.DeepEqual(got, []byte{0, 0}) {
		t.Fatalf("empty directory body = % X, want 00 00", got)
	}
}

func TestSetPartyInfoDefersCurrentPartyFrameProjectionToBridge(t *testing.T) {
	state := alignedcmd.PartyState{}
	got, err := NewHandler().Handle(context.Background(), alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketSetPartyInfo),
		Body:                []byte{0, 3, 4, 0, 0, 0, 0, 1, 0, 0, 0, 1, 0x9c, 0},
		SelectedCharacterID: 1001,
		Party:               &state,
	})
	if err != nil {
		t.Fatalf("Handle(SetPartyInfo) error = %v", err)
	}
	if !got.Handled || !got.ResponseAllowed {
		t.Fatalf("handled=%t responseAllowed=%t", got.Handled, got.ResponseAllowed)
	}
	if len(got.UpperResponses) != 1 {
		t.Fatalf("response count = %d, want ACK before bridge projection", len(got.UpperResponses))
	}
	for _, response := range got.UpperResponses {
		if response.Classification == 0 && response.MsgID == 0x0009 {
			t.Fatalf("unsafe class0/op9 scene record emitted: % X", response.Body)
		}
	}
	ack := got.UpperResponses[0]
	if ack.Classification != dnfproto.DefaultChannelClassification ||
		ack.MsgID != uint16(dnfenum.CmdPacketSetPartyInfo) ||
		!reflect.DeepEqual(ack.Body, []byte{1}) {
		t.Fatalf("ack = class %d msg %d body % X", ack.Classification, ack.MsgID, ack.Body)
	}
	if !reflect.DeepEqual(got.PostActions, []alignedcmd.PostAction{alignedcmd.PostActionRefreshSelectedPartyFrame}) {
		t.Fatalf("post actions = %v, want party frame refresh", got.PostActions)
	}
	if state.PartyID != 1001 {
		t.Fatalf("party ID = %d, want current leader character ID 1001", state.PartyID)
	}
}

func TestQuickPartyAckOnlyCommands(t *testing.T) {
	h := NewHandler()
	for _, opcode := range []dnfenum.CmdPacket{
		dnfenum.CmdPacketCancelQuickParty,
		dnfenum.CmdPacketDirectEntranceDungeonQuickParty,
	} {
		t.Run(dnfenum.CmdPacketName(uint16(opcode)), func(t *testing.T) {
			got, err := h.Handle(context.Background(), alignedcmd.Request{Opcode: uint16(opcode)})
			if err != nil {
				t.Fatalf("Handle(%d) error = %v", opcode, err)
			}
			if !got.Handled || !got.ResponseAllowed {
				t.Fatalf("Handle(%d) handled=%v responseAllowed=%v", opcode, got.Handled, got.ResponseAllowed)
			}
			if len(got.UpperResponses) != 1 {
				t.Fatalf("Handle(%d) response count = %d, want 1", opcode, len(got.UpperResponses))
			}
			resp := got.UpperResponses[0]
			if resp.MsgID != uint16(opcode) || !reflect.DeepEqual(resp.Body, []byte{1}) {
				t.Fatalf("Handle(%d) response msg=%d body=% X", opcode, resp.MsgID, resp.Body)
			}
		})
	}
}

func TestRegisterQuickPartyPending(t *testing.T) {
	body := make([]byte, 4+5*2)
	binary.LittleEndian.PutUint32(body[:4], 2)
	binary.LittleEndian.PutUint32(body[4:8], 1001)
	body[8] = 1
	binary.LittleEndian.PutUint32(body[9:13], 1002)
	body[13] = 0

	got, err := NewHandler().Handle(context.Background(), alignedcmd.Request{
		Opcode: uint16(dnfenum.CmdPacketRegisterQuickParty),
		Body:   body,
	})
	if err != nil {
		t.Fatalf("Handle(register quick party) error = %v", err)
	}
	if !got.Handled || got.ResponseAllowed || len(got.UpperResponses) != 0 {
		t.Fatalf("register quick party handled=%v responseAllowed=%v responses=%d",
			got.Handled, got.ResponseAllowed, len(got.UpperResponses))
	}
}

func TestReserveLeavePartyAck(t *testing.T) {
	state := alignedcmd.PartyState{}
	got, err := NewHandler().Handle(context.Background(), alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketReserveLeaveParty),
		Body:                []byte{1},
		SelectedCharacterID: 1001,
		Party:               &state,
	})
	if err != nil {
		t.Fatalf("Handle(reserve leave party) error = %v", err)
	}
	if !got.Handled || !got.ResponseAllowed {
		t.Fatalf("reserve leave party handled=%v responseAllowed=%v", got.Handled, got.ResponseAllowed)
	}
	if state.ReserveLeaveFlag != 1 {
		t.Fatalf("ReserveLeaveFlag = %d, want 1", state.ReserveLeaveFlag)
	}
	if len(got.UpperResponses) != 1 {
		t.Fatalf("response count = %d, want 1", len(got.UpperResponses))
	}
	want := []byte{1, 1, 0xe9, 0x03}
	resp := got.UpperResponses[0]
	if resp.MsgID != uint16(dnfenum.CmdPacketReserveLeaveParty) || !reflect.DeepEqual(resp.Body, want) {
		t.Fatalf("reserve response msg=%d body=% X, want msg=%d body=% X",
			resp.MsgID, resp.Body, dnfenum.CmdPacketReserveLeaveParty, want)
	}
}

func TestEntryIntoPartyRequestAndAck(t *testing.T) {
	body := make([]byte, 4)
	binary.LittleEndian.PutUint32(body, 1002)
	parsed, err := DecodeEntryIntoPartyRequest(body)
	if err != nil {
		t.Fatalf("DecodeEntryIntoPartyRequest error = %v", err)
	}
	if parsed.TargetID != 1002 {
		t.Fatalf("target = %d, want 1002", parsed.TargetID)
	}
	want := []byte{1, 0xea, 0x03, 0x00, 0x00, 0xe9, 0x03, 0x00, 0x00}
	if got := BuildEntryIntoPartyAck(1002, 1001); !reflect.DeepEqual(got, want) {
		t.Fatalf("entry ack = % X, want % X", got, want)
	}
}

func TestBuildQuickPartyInvite(t *testing.T) {
	got := BuildQuickPartyInvite(1001, 13)
	want := []byte{0xe9, 0x03, 13}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("quick party invite = % X, want % X", got, want)
	}
}

func TestBuildRequestPeerNoticesUseCurrentModeSpecificShapes(t *testing.T) {
	tests := []struct {
		name string
		req  RequestPeerRequest
		want []byte
	}{
		{
			name: "party",
			req:  RequestPeerRequest{Mode: 0, Value0: 0x11223344, Value1: 0x5566, Value2: 0x778899aa},
			want: []byte{0xe9, 0x03, 0, 0x44, 0x33, 0x22, 0x11, 0x66, 0x55, 0, 0, 0, 0, 0xaa, 0x99, 0x88, 0x77},
		},
		{
			name: "trade",
			req:  RequestPeerRequest{Mode: 1, Value0: 0x11223344, Value2: 0x55667788},
			want: []byte{0xe9, 0x03, 1, 0x44, 0x33, 0x22, 0x11, 0x88, 0x77, 0x66, 0x55},
		},
		{
			name: "quick party",
			req:  RequestPeerRequest{Mode: 13, Value0: 0x11223344},
			want: []byte{0xe9, 0x03, 13, 0x44, 0x33, 0x22, 0x11},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := BuildRequestPeerNotice(1001, test.req); !bytes.Equal(got, test.want) {
				t.Fatalf("notice=% X want=% X", got, test.want)
			}
		})
	}
}

func TestBuildResponsePeerBodies(t *testing.T) {
	if got, want := BuildResponsePeerAckPayload(1001, 0), []byte{0xe9, 0x03, 0}; !bytes.Equal(got, want) {
		t.Fatalf("party ack payload=% X want=% X", got, want)
	}
	if got, want := BuildResponsePeerAckPayload(1001, 1), []byte{0xe9, 0x03, 1, 0}; !bytes.Equal(got, want) {
		t.Fatalf("trade ack payload=% X want=% X", got, want)
	}
	if got, want := BuildResponsePeerNotice(1002, 1, 0), []byte{0xea, 0x03, 1, 0, 0, 0, 0}; !bytes.Equal(got, want) {
		t.Fatalf("response notice=% X want=% X", got, want)
	}
}

func TestBuildWalkoutPartyMemberNotice(t *testing.T) {
	got := BuildWalkoutPartyMemberNotice(1, 0)
	want := []byte{1, 0}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("walkout party member notice = % X, want % X", got, want)
	}
}

func TestBuildChangePartyMemberPositionAck(t *testing.T) {
	got := BuildChangePartyMemberPositionAck(1, 3)
	want := []byte{1, 3}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("change party member position ack = % X, want % X", got, want)
	}
}

func TestBuildRequestPeerAck(t *testing.T) {
	if got := BuildRequestPeerAck(); !reflect.DeepEqual(got, []byte{1}) {
		t.Fatalf("request peer ack = % X, want 01", got)
	}
}

func TestBuildRequestPeerSelectionNotice(t *testing.T) {
	partyless := BuildRequestPeerSelectionNotice(1002, 0x11223344, false)
	wantPartyless := []byte{0xea, 0x03, 15, 0x44, 0x33, 0x22, 0x11, 0xff, 0xff}
	if !reflect.DeepEqual(partyless, wantPartyless) {
		t.Fatalf("partyless request peer selection notice = % X, want % X", partyless, wantPartyless)
	}
	active := BuildRequestPeerSelectionNotice(1002, 0x11223344, true)
	wantActive := []byte{0xea, 0x03, 15, 0x44, 0x33, 0x22, 0x11, 0, 0}
	if !reflect.DeepEqual(active, wantActive) {
		t.Fatalf("active request peer selection notice = % X, want % X", active, wantActive)
	}
}

func TestDecodeRequestPeerRequest(t *testing.T) {
	body := []byte{
		0xea, 0x03,
		15,
		0x44, 0x33, 0x22, 0x11,
		0x66, 0x55,
		0xaa, 0x99, 0x88, 0x77,
	}
	got, err := DecodeRequestPeerRequest(body)
	if err != nil {
		t.Fatalf("DecodeRequestPeerRequest error = %v", err)
	}
	if got.TargetID != 1002 || got.Mode != 15 ||
		got.Value0 != 0x11223344 || got.Value1 != 0x5566 || got.Value2 != 0x778899aa {
		t.Fatalf("request peer = %+v", got)
	}
}

func TestDecodeResponsePeerRequest(t *testing.T) {
	got, err := DecodeResponsePeerRequest([]byte{0xea, 0x03, 13, 1, 0, 0, 0})
	if err != nil {
		t.Fatalf("DecodeResponsePeerRequest error = %v", err)
	}
	if got.TargetID != 1002 || got.Mode != 13 || got.Value != 1 {
		t.Fatalf("response peer = %+v, want target=1002 mode=13 value=1", got)
	}
}

func TestDecodeRequestPeerModeSpecificLengths(t *testing.T) {
	partyBody := []byte{0xea, 0x03, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	partyReq, err := DecodeRequestPeerRequest(partyBody)
	if err != nil || partyReq.TargetID != 1002 || partyReq.Mode != 0 || partyReq.Value1 != 0x0605 || partyReq.Value2 != 0x0a090807 {
		t.Fatalf("party request=%+v err=%v", partyReq, err)
	}
	tradeBody := []byte{0xea, 0x03, 1, 1, 2, 3, 4, 5, 6, 7, 8}
	tradeReq, err := DecodeRequestPeerRequest(tradeBody)
	if err != nil || tradeReq.TargetID != 1002 || tradeReq.Mode != 1 || tradeReq.Value2 != 0x08070605 {
		t.Fatalf("trade request=%+v err=%v", tradeReq, err)
	}
}

func TestDecodeWalkoutPartyMemberRequest(t *testing.T) {
	got, err := DecodeWalkoutPartyMemberRequest([]byte{1})
	if err != nil {
		t.Fatalf("DecodeWalkoutPartyMemberRequest error = %v", err)
	}
	if got.Slot != 1 {
		t.Fatalf("slot = %d, want 1", got.Slot)
	}
}

func TestDecodeChangePartyMemberPositionRequest(t *testing.T) {
	got, err := DecodeChangePartyMemberPositionRequest([]byte{2, 3})
	if err != nil {
		t.Fatalf("DecodeChangePartyMemberPositionRequest error = %v", err)
	}
	if got.Slot != 2 || got.Position != 3 {
		t.Fatalf("change position = %+v, want slot=2 position=3", got)
	}
	if _, err := DecodeChangePartyMemberPositionRequest([]byte{2, 2}); err == nil {
		t.Fatalf("DecodeChangePartyMemberPositionRequest accepted invalid position")
	}
}

func TestPartyFixedRequestDecodersRejectTrailingBytes(t *testing.T) {
	quick := make([]byte, 10)
	binary.LittleEndian.PutUint32(quick[:4], 1)
	tests := []struct {
		name string
		body []byte
		call func([]byte) error
	}{
		{name: "register quick party", body: quick, call: func(body []byte) error { _, err := DecodeRegisterQuickPartyRequest(body); return err }},
		{name: "reserve leave", body: []byte{1, 0}, call: func(body []byte) error { _, err := DecodeReserveLeavePartyRequest(body); return err }},
		{name: "entry into party", body: []byte{1, 0, 0, 0, 0}, call: func(body []byte) error { _, err := DecodeEntryIntoPartyRequest(body); return err }},
		{name: "request peer", body: make([]byte, 14), call: func(body []byte) error { _, err := DecodeRequestPeerRequest(body); return err }},
		{name: "response peer", body: []byte{1, 0, 13, 1, 0, 0, 0, 0}, call: func(body []byte) error { _, err := DecodeResponsePeerRequest(body); return err }},
		{name: "walkout", body: []byte{1, 0}, call: func(body []byte) error { _, err := DecodeWalkoutPartyMemberRequest(body); return err }},
		{name: "change position", body: []byte{1, 3, 0}, call: func(body []byte) error { _, err := DecodeChangePartyMemberPositionRequest(body); return err }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(tc.body); err == nil {
				t.Fatalf("decoder accepted trailing byte: % X", tc.body)
			}
		})
	}
}

func TestEntryIntoPartyFinishAckRequiresPartyContext(t *testing.T) {
	state := alignedcmd.PartyState{
		PartyID: 1,
		Members: []alignedcmd.PartyMemberState{
			{UserID: 1001},
			{UserID: 1002},
		},
	}
	got, err := NewHandler().Handle(context.Background(), alignedcmd.Request{
		Opcode: uint16(dnfenum.CmdPacketEntryIntoPartyFinish),
		Party:  &state,
	})
	if err != nil {
		t.Fatalf("Handle(entry finish) error = %v", err)
	}
	if !got.Handled || !got.ResponseAllowed || len(got.UpperResponses) != 1 {
		t.Fatalf("entry finish handled=%v responseAllowed=%v responses=%d",
			got.Handled, got.ResponseAllowed, len(got.UpperResponses))
	}
	resp := got.UpperResponses[0]
	if resp.MsgID != uint16(dnfenum.CmdPacketEntryIntoPartyFinish) ||
		resp.Classification != 0 || resp.AllowCodec ||
		!reflect.DeepEqual(resp.Body, []byte{1, 0}) {
		t.Fatalf("entry finish response msg=%d body=% X", resp.MsgID, resp.Body)
	}
}

func TestEntryIntoPartyFinishWithoutContextDoesNotForgeClassOneFailure(t *testing.T) {
	got, err := NewHandler().Handle(context.Background(), alignedcmd.Request{
		Opcode: uint16(dnfenum.CmdPacketEntryIntoPartyFinish),
	})
	if err != nil {
		t.Fatalf("Handle(entry finish without context) error = %v", err)
	}
	if !got.Handled || got.ResponseAllowed || len(got.UpperResponses) != 0 {
		t.Fatalf("entry finish without context handled=%v responseAllowed=%v responses=%d",
			got.Handled, got.ResponseAllowed, len(got.UpperResponses))
	}
}
