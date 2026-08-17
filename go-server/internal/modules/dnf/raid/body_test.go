// 本文件验证攻坚队包体解析、0x24F 成员刷新和 889 检查结果构造。
package raid

import (
	"context"
	"encoding/binary"
	"reflect"
	"strings"
	"testing"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
)

func TestDecodeManagerWorkRequest(t *testing.T) {
	body := make([]byte, 12)
	binary.LittleEndian.PutUint32(body[0:4], 0)
	binary.LittleEndian.PutUint32(body[4:8], 1001)
	binary.LittleEndian.PutUint32(body[8:12], 3)

	got, err := DecodeManagerWorkRequest(body)
	if err != nil {
		t.Fatalf("DecodeManagerWorkRequest error = %v", err)
	}
	if got.ActionOrMode != 0 || got.MemberCharKey != 1001 || got.TargetGroup != 3 {
		t.Fatalf("manager work = %+v, want action=0 member=1001 group=3", got)
	}
}

func TestBuildCreateAndLeaveRaidResultBodies(t *testing.T) {
	if got, want := BuildCreateRaidResultBody(0x12345678), []byte{1, 0x78, 0x56, 0x34, 0x12}; !reflect.DeepEqual(got, want) {
		t.Fatalf("create raid result body = % X, want % X", got, want)
	}
	for _, tc := range []struct {
		name      string
		raidEnded bool
		want      []byte
	}{
		{name: "member left", raidEnded: false, want: []byte{1, 0}},
		{name: "raid ended", raidEnded: true, want: []byte{1, 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := BuildLeaveRaidResultBody(tc.raidEnded); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("leave raid result body = % X, want % X", got, tc.want)
			}
		})
	}
}

func TestDecodeCreateAndModifyRaidInfoRequest(t *testing.T) {
	body := buildRaidInfoBodyForTest(2, []byte("anton"), 7)

	create, err := DecodeCreateRaidRequest(body)
	if err != nil {
		t.Fatalf("DecodeCreateRaidRequest error = %v", err)
	}
	modify, err := DecodeModifyRaidInfoRequest(body)
	if err != nil {
		t.Fatalf("DecodeModifyRaidInfoRequest error = %v", err)
	}
	for name, got := range map[string]InfoRequest{"create": create, "modify": modify} {
		if got.RouteOrRaidType != 2 || !bytesEqual(got.NameBytes, []byte("anton")) || got.TailFlag != 7 {
			t.Fatalf("%s request = %+v, want route=2 name=anton tail=7", name, got)
		}
	}
}

func TestDecodeCreateRaidInfoRequestRejectsTruncatedName(t *testing.T) {
	body := []byte{1, 5, 0, 0, 0, 'a', 'b'}
	if _, err := DecodeCreateRaidRequest(body); err == nil {
		t.Fatalf("DecodeCreateRaidRequest accepted truncated dstr")
	}
}

func TestRaidRequestDecodersRejectTrailingBytes(t *testing.T) {
	info := append(buildRaidInfoBodyForTest(2, []byte("anton"), 7), 0)
	if _, err := DecodeCreateRaidRequest(info); err == nil {
		t.Fatalf("DecodeCreateRaidRequest accepted trailing byte")
	}

	tests := []struct {
		name string
		body []byte
		call func([]byte) error
	}{
		{name: "leave", body: []byte{1, 0, 0}, call: func(body []byte) error { _, err := DecodeLeaveRaidRequest(body); return err }},
		{name: "rejoin", body: []byte{1, 0, 0, 0, 0}, call: func(body []byte) error { _, err := DecodeRejoinRaidRequest(body); return err }},
		{name: "waiting", body: []byte{0, 2, 0}, call: func(body []byte) error { _, err := DecodeSetWaitingRequest(body); return err }},
		{name: "entry cost", body: []byte{1, 0}, call: func(body []byte) error { _, err := DecodeEntryCostInfoRequest(body); return err }},
		{name: "set symbol", body: make([]byte, 10), call: func(body []byte) error { _, err := DecodeSetSymbolRequest(body); return err }},
		{name: "manager work", body: make([]byte, 13), call: func(body []byte) error { _, err := DecodeManagerWorkRequest(body); return err }},
		{name: "other channel join", body: make([]byte, 10), call: func(body []byte) error { _, err := DecodeOtherChannelRequestJoinRequest(body); return err }},
		{name: "member state", body: []byte{1, 0}, call: func(body []byte) error { _, err := DecodeMemberChangeStateRequest(body); return err }},
		{name: "move channel fail", body: []byte{1, 2, 0, 0}, call: func(body []byte) error { _, err := DecodeUserMoveChannelFailRequest(body); return err }},
		{name: "simple other channel list", body: []byte{1, 0}, call: func(body []byte) error { _, err := DecodeOtherChannelListRequest(body); return err }},
		{name: "check user", body: []byte{1, 2, 0, 0}, call: func(body []byte) error { _, err := DecodeCheckRaidUserRequest(body); return err }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(tc.body); err == nil {
				t.Fatalf("decoder accepted trailing byte: % X", tc.body)
			}
		})
	}

	fullList := make([]byte, 14)
	fullList[0] = 0
	if _, err := DecodeOtherChannelListRequest(fullList); err == nil {
		t.Fatalf("DecodeOtherChannelListRequest accepted trailing byte after empty dstr")
	}
}

func TestDecodeSetSymbolRejectsOutOfRangeSymbol(t *testing.T) {
	body := make([]byte, 9)
	body[8] = 3
	if _, err := DecodeSetSymbolRequest(body); err == nil {
		t.Fatalf("DecodeSetSymbolRequest accepted symbol 3")
	}
}

func TestDecodeLeaveStartAndRejoinRaidRequests(t *testing.T) {
	leave, err := DecodeLeaveRaidRequest([]byte{0x34, 0x12})
	if err != nil {
		t.Fatalf("DecodeLeaveRaidRequest error = %v", err)
	}
	if leave.RaidOrMemberKey != 0x1234 {
		t.Fatalf("leave key = 0x%04X, want 0x1234", leave.RaidOrMemberKey)
	}
	if err := DecodeStartRaidRequest(nil); err != nil {
		t.Fatalf("DecodeStartRaidRequest empty error = %v", err)
	}
	if err := DecodeStartRaidRequest([]byte{1}); err == nil {
		t.Fatalf("DecodeStartRaidRequest accepted non-empty body")
	}
	rejoin, err := DecodeRejoinRaidRequest([]byte{0x78, 0x56, 0x34, 0x12})
	if err != nil {
		t.Fatalf("DecodeRejoinRaidRequest error = %v", err)
	}
	if rejoin.RaidKey != 0x12345678 {
		t.Fatalf("rejoin raid key = 0x%08X, want 0x12345678", rejoin.RaidKey)
	}
}

func TestDecodeRaidNeighborRequests(t *testing.T) {
	waiting, err := DecodeSetWaitingRequest([]byte{0, 2})
	if err != nil {
		t.Fatalf("DecodeSetWaitingRequest error = %v", err)
	}
	if waiting.Flag != 0 || waiting.RouteRaidType != 2 {
		t.Fatalf("waiting = %+v, want flag=0 route=2", waiting)
	}

	entryCost, err := DecodeEntryCostInfoRequest([]byte{1})
	if err != nil {
		t.Fatalf("DecodeEntryCostInfoRequest error = %v", err)
	}
	if entryCost.Enabled != 1 {
		t.Fatalf("entry cost enabled = %d, want 1", entryCost.Enabled)
	}

	setSymbolBody := make([]byte, 9)
	binary.LittleEndian.PutUint32(setSymbolBody[0:4], 0x11223344)
	binary.LittleEndian.PutUint32(setSymbolBody[4:8], 0x55667788)
	setSymbolBody[8] = 2
	setSymbol, err := DecodeSetSymbolRequest(setSymbolBody)
	if err != nil {
		t.Fatalf("DecodeSetSymbolRequest error = %v", err)
	}
	if setSymbol.SourceValue != 0x11223344 || setSymbol.TargetValue != 0x55667788 || setSymbol.Symbol != 2 {
		t.Fatalf("set symbol = %+v, want source=0x11223344 target=0x55667788 symbol=2", setSymbol)
	}

	joinBody := []byte{1, 0x34, 0x12, 0x78, 0x56, 0x34, 0x12, 0x02, 0x00}
	join, err := DecodeOtherChannelRequestJoinRequest(joinBody)
	if err != nil {
		t.Fatalf("DecodeOtherChannelRequestJoinRequest error = %v", err)
	}
	if join.Mode != 1 || join.TargetKey != 0x1234 || join.ClientValue != 0x12345678 || join.RouteRaidType != 2 {
		t.Fatalf("join = %+v, want mode=1 target=0x1234 client=0x12345678 route=2", join)
	}

	state, err := DecodeMemberChangeStateRequest([]byte{3})
	if err != nil {
		t.Fatalf("DecodeMemberChangeStateRequest error = %v", err)
	}
	if state.State != 3 {
		t.Fatalf("state = %d, want 3", state.State)
	}

	moveFail, err := DecodeUserMoveChannelFailRequest([]byte{4, 0x78, 0x56})
	if err != nil {
		t.Fatalf("DecodeUserMoveChannelFailRequest error = %v", err)
	}
	if moveFail.Mode != 4 || moveFail.TargetKey != 0x5678 {
		t.Fatalf("move fail = %+v, want mode=4 target=0x5678", moveFail)
	}

	check, err := DecodeCheckRaidUserRequest([]byte{5, 0xbc, 0x9a})
	if err != nil {
		t.Fatalf("DecodeCheckRaidUserRequest error = %v", err)
	}
	if check.Mode != 5 || check.TargetKey != 0x9abc {
		t.Fatalf("check = %+v, want mode=5 target=0x9abc", check)
	}
}

func TestDecodeOtherChannelListRequest(t *testing.T) {
	simple, err := DecodeOtherChannelListRequest([]byte{1})
	if err != nil {
		t.Fatalf("DecodeOtherChannelListRequest simple error = %v", err)
	}
	if simple.Mode != 1 || simple.HasContext || simple.HasNameDstr {
		t.Fatalf("simple list = %+v, want mode=1 without context", simple)
	}

	body := make([]byte, 13+4)
	body[0] = 0
	copy(body[1:9], []byte{1, 2, 3, 4, 5, 6, 7, 8})
	binary.LittleEndian.PutUint32(body[9:13], 4)
	copy(body[13:], []byte("raid"))
	full, err := DecodeOtherChannelListRequest(body)
	if err != nil {
		t.Fatalf("DecodeOtherChannelListRequest full error = %v", err)
	}
	if full.Mode != 0 || !full.HasContext || !full.HasNameDstr || !bytesEqual(full.NameBytes, []byte("raid")) {
		t.Fatalf("full list = %+v, want mode=0 context name=raid", full)
	}
	if full.Context != [8]byte{1, 2, 3, 4, 5, 6, 7, 8} {
		t.Fatalf("context = % X", full.Context)
	}
}

func TestBuildCheckUserResultBody(t *testing.T) {
	got := BuildCheckUserResultBody(CheckUserResult{
		Header: CheckUserHeader{
			Mode:        CheckUserModeRefreshWithHeaderState,
			Field4:      0x11223344,
			Field8:      0x55667788,
			Field12:     0x99,
			Raw13To20:   [8]byte{1, 2, 3, 4, 5, 6, 7, 8},
			Field21To24: 0xaabbccdd,
		},
		FirstRows: [][12]byte{
			{0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b},
		},
		SecondRows: [][12]byte{
			{0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28, 0x29, 0x2a, 0x2b},
		},
		StateRows: [][5]byte{
			{0x30, 0x31, 0x32, 0x33, 0x34},
		},
	})
	want := []byte{
		0x01, 0x00, 0x00, 0x00,
		0x44, 0x33, 0x22, 0x11,
		0x88, 0x77, 0x66, 0x55,
		0x99,
		0x01, 0x02, 0x03, 0x04,
		0x05, 0x06, 0x07, 0x08,
		0xdd, 0xcc, 0xbb, 0xaa,
		0x01, 0x00, 0x00, 0x00,
		0x10, 0x11, 0x12, 0x13,
		0x14, 0x15, 0x16, 0x17,
		0x18, 0x19, 0x1a, 0x1b,
		0x01, 0x00, 0x00, 0x00,
		0x20, 0x21, 0x22, 0x23,
		0x24, 0x25, 0x26, 0x27,
		0x28, 0x29, 0x2a, 0x2b,
		0x01, 0x00, 0x00, 0x00,
		0x30, 0x31, 0x32, 0x33, 0x34,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("check user body = % X, want % X", got, want)
	}
}

func TestBuildRequestMembersResultBody(t *testing.T) {
	got := BuildRequestMembersResultBody(RequestMembersResultEnterPublicRaidState, 0x55)
	want := []byte{0x02, 0x00, 0x00, 0x00, 0x55}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("request members result body = % X, want % X", got, want)
	}
}

func TestRaidCheckUserAndRequestMemberConstants(t *testing.T) {
	if CheckUserModeRefresh != 0 || CheckUserModeRefreshWithHeaderState != 1 {
		t.Fatalf("check user modes = %d/%d, want 0/1", CheckUserModeRefresh, CheckUserModeRefreshWithHeaderState)
	}
	if RequestMembersResultEnterPublicRaidState != 2 || RequestMembersResultSkipLocalError != 3 {
		t.Fatalf("request member results = %d/%d, want 2/3", RequestMembersResultEnterPublicRaidState, RequestMembersResultSkipLocalError)
	}
	if requestMembersUIStatePublicRaid != 8 || requestMembersUIStateLocalFailure != 10 {
		t.Fatalf("request member UI states = %d/%d, want 8/10", requestMembersUIStatePublicRaid, requestMembersUIStateLocalFailure)
	}
}

func TestBuildOtherChannelBodies(t *testing.T) {
	userinfo := BuildOtherChannelUserinfoBody([]uint32{0x11223344, 0x55667788})
	wantUserinfo := []byte{
		0x02, 0x00, 0x00, 0x00,
		0x44, 0x33, 0x22, 0x11,
		0x88, 0x77, 0x66, 0x55,
	}
	if !reflect.DeepEqual(userinfo, wantUserinfo) {
		t.Fatalf("other channel userinfo body = % X, want % X", userinfo, wantUserinfo)
	}

	join := BuildOtherChannelRequestJoinResultBody(0x01020304, 0xaabbccdd)
	wantJoin := []byte{0x04, 0x03, 0x02, 0x01, 0xdd, 0xcc, 0xbb, 0xaa}
	if !reflect.DeepEqual(join, wantJoin) {
		t.Fatalf("other channel join result body = % X, want % X", join, wantJoin)
	}

	response := BuildOtherChannelResponseJoinBody(7, 1)
	if !reflect.DeepEqual(response, []byte{7, 1}) {
		t.Fatalf("other channel response join body = % X, want 07 01", response)
	}

	var header [20]byte
	for i := range header {
		header[i] = byte(i + 1)
	}
	page := BuildOtherChannelListPageBody(OtherChannelListPage{
		Header: header,
		Rows12: [][12]byte{
			{0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b},
		},
		Rows13: [][13]byte{
			{0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28, 0x29, 0x2a, 0x2b, 0x2c},
		},
	})
	wantPage := []byte{
		0x01, 0x02, 0x03, 0x04,
		0x05, 0x06, 0x07, 0x08,
		0x09, 0x0a, 0x0b, 0x0c,
		0x0d, 0x0e, 0x0f, 0x10,
		0x11, 0x12, 0x13, 0x14,
		0x01, 0x00, 0x00, 0x00,
		0x10, 0x11, 0x12, 0x13,
		0x14, 0x15, 0x16, 0x17,
		0x18, 0x19, 0x1a, 0x1b,
		0x01, 0x00, 0x00, 0x00,
		0x20, 0x21, 0x22, 0x23,
		0x24, 0x25, 0x26, 0x27,
		0x28, 0x29, 0x2a, 0x2b,
		0x2c,
	}
	if !reflect.DeepEqual(page, wantPage) {
		t.Fatalf("other channel list page body = % X, want % X", page, wantPage)
	}
}

func TestBuildMemberRefreshMode3Body(t *testing.T) {
	got, err := BuildMemberRefreshMode3Body(MemberRefresh{
		RaidKey: 0x01020304,
		Members: []MemberRecord{
			{
				CharKey:           1001,
				Field4:            1,
				Name:              "A",
				Field40:           2,
				Field44:           3,
				GroupIndex:        4,
				SlotOrder:         5,
				Field48:           0x11223344,
				Field52:           6,
				Field53:           7,
				Field56:           8,
				Field60:           0x55667788,
				Field64:           0x99aa,
				Field66BoolSource: 1,
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildMemberRefreshMode3Body error = %v", err)
	}
	want := []byte{
		0x04, 0x03, 0x02, 0x01,
		0x03, 0x00, 0x00, 0x00,
		0x01,
		0xe9, 0x03,
		0x01,
		0x02, 0x00, 0x00, 0x00,
		0x41, 0x00,
		0x02,
		0x03,
		0x04,
		0x05,
		0x44, 0x33, 0x22, 0x11,
		0x06,
		0x07,
		0x08,
		0x88, 0x77, 0x66, 0x55,
		0xaa, 0x99,
		0x01, 0x00, 0x00, 0x00,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("refresh body = % X, want % X", got, want)
	}
}

func TestBuildMemberRefreshMode3BodyWritesNonZeroEmptyName(t *testing.T) {
	got, err := BuildMemberRefreshMode3Body(MemberRefresh{
		Members: []MemberRecord{{CharKey: 1001}},
	})
	if err != nil {
		t.Fatalf("BuildMemberRefreshMode3Body error = %v", err)
	}
	nameLenOffset := 8 + 1 + 2 + 1
	if length := binary.LittleEndian.Uint32(got[nameLenOffset : nameLenOffset+4]); length != 2 {
		t.Fatalf("empty name length = %d, want 2", length)
	}
	if !reflect.DeepEqual(got[nameLenOffset+4:nameLenOffset+6], []byte{0, 0}) {
		t.Fatalf("empty name bytes = % X, want 00 00", got[nameLenOffset+4:nameLenOffset+6])
	}
}

func TestBuildMemberRefreshMode3BodyRejectsTwentyFirstMember(t *testing.T) {
	members := make([]MemberRecord, MaxAttackPartyMembers+1)
	if _, err := BuildMemberRefreshMode3Body(MemberRefresh{Members: members}); err == nil {
		t.Fatalf("BuildMemberRefreshMode3Body accepted %d members", len(members))
	}
}

func TestBuildMemberRefreshMode3BodyAllowsTwentyMembers(t *testing.T) {
	members := make([]MemberRecord, MaxAttackPartyMembers)
	for i := range members {
		members[i].CharKey = uint16(1000 + i)
	}
	got, err := BuildMemberRefreshMode3Body(MemberRefresh{RaidKey: 0x20010001, Members: members})
	if err != nil {
		t.Fatalf("BuildMemberRefreshMode3Body error = %v", err)
	}
	if got[8] != MaxAttackPartyMembers {
		t.Fatalf("member count = %d, want %d", got[8], MaxAttackPartyMembers)
	}
}

func TestRaidHandlerParsesManagerWorkWithoutAck(t *testing.T) {
	body := make([]byte, 12)
	binary.LittleEndian.PutUint32(body[4:8], 1001)
	binary.LittleEndian.PutUint32(body[8:12], 4)

	got, err := NewHandler().Handle(contextForTest(), alignedcmd.Request{
		Opcode: uint16(dnfenum.CmdPacketRaidManagerWork),
		Body:   body,
	})
	if err != nil {
		t.Fatalf("Handle raid manager work error = %v", err)
	}
	if !got.Handled || got.ResponseAllowed || len(got.UpperResponses) != 0 {
		t.Fatalf("raid manager handled=%v responseAllowed=%v responses=%d", got.Handled, got.ResponseAllowed, len(got.UpperResponses))
	}
	if !strings.Contains(got.Reason, "0x24F mode=3") {
		t.Fatalf("reason = %q, want 0x24F mode=3 hint", got.Reason)
	}
}

func TestRaidHandlerParsesSetSymbolWithoutAck(t *testing.T) {
	body := make([]byte, 9)
	binary.LittleEndian.PutUint32(body[0:4], 0x11223344)
	binary.LittleEndian.PutUint32(body[4:8], 0x55667788)
	body[8] = 1

	got, err := NewHandler().Handle(contextForTest(), alignedcmd.Request{
		Opcode: uint16(dnfenum.CmdPacketRaidSetSymbol),
		Body:   body,
	})
	if err != nil {
		t.Fatalf("Handle raid set symbol error = %v", err)
	}
	if !got.Handled || got.ResponseAllowed || len(got.UpperResponses) != 0 {
		t.Fatalf("raid set symbol handled=%v responseAllowed=%v responses=%d", got.Handled, got.ResponseAllowed, len(got.UpperResponses))
	}
	if !strings.Contains(got.Reason, "RaidSetSymbol") {
		t.Fatalf("reason = %q, want RaidSetSymbol hint", got.Reason)
	}
}

func contextForTest() context.Context { return context.Background() }

func buildRaidInfoBodyForTest(route byte, name []byte, tail byte) []byte {
	body := make([]byte, 1+4+len(name)+1)
	body[0] = route
	binary.LittleEndian.PutUint32(body[1:5], uint32(len(name)))
	copy(body[5:], name)
	body[len(body)-1] = tail
	return body
}

func bytesEqual(left []byte, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
