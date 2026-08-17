package packageitem

import (
	"context"
	"encoding/binary"
	"testing"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func handleMagicBoxRequest(t *testing.T, repos dnfrepo.Group, boxResolver alignedcmd.MagicBoxResolver, rewardResolver alignedcmd.MagicBoxRewardItemResolver, body []byte) alignedcmd.Result {
	t.Helper()
	got, err := NewHandler().Handle(context.Background(), alignedcmd.Request{
		Opcode:                     uint16(dnfenum.CmdPacketUseRandomboxItem),
		Body:                       body,
		AccountID:                  "acc",
		SelectedCharacterID:        77,
		Repositories:               repos,
		MagicBoxResolver:           boxResolver,
		MagicBoxRewardItemResolver: rewardResolver,
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	return got
}

func magicBoxRequestBody(boxSlot int16, materialSlot int16) []byte {
	body := []byte{0, byte(boxSlot), byte(boxSlot >> 8)}
	if materialSlot >= 0 {
		body = append(body, byte(materialSlot), byte(materialSlot>>8))
	}
	return body
}

func TestHandlerMagicBoxSendsSingleAckThenRefresh(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveMagicBoxFixture(t, ctx, repos, magicBoxBaseSlots())

	got := handleMagicBoxRequest(t, repos, staticMagicBoxResolver(magicBoxTestResolution()), staticMagicBoxRewardResolver(magicBoxTestRewardItems()), magicBoxRequestBody(10, 121))
	if !got.Handled || !got.ResponseAllowed || len(got.UpperResponses) != 1 {
		t.Fatalf("result = %+v", got)
	}
	ack := got.UpperResponses[0]
	if ack.MsgID != uint16(dnfenum.CmdPacketUseRandomboxItem) {
		t.Fatalf("ack msg = 0x%04X, want 0x00D0", ack.MsgID)
	}
	row := func(slot uint16, itemID uint32, count uint32) []byte {
		r := make([]byte, currentMagicBoxEntrySize)
		binary.LittleEndian.PutUint16(r[0x00:0x02], slot)
		binary.LittleEndian.PutUint32(r[0x02:0x06], itemID)
		binary.LittleEndian.PutUint32(r[0x06:0x0A], count)
		return r
	}
	want := []byte{0x01, 0x00, 0x00, 0x0A, 0x00, 0x79, 0x00, 0x02, 0x00}
	want = append(want, row(65, 2600014, 2)...)
	want = append(want, row(66, 2682272, 1)...)
	if string(ack.Body) != string(want) {
		t.Fatalf("ack body = %x, want %x", ack.Body, want)
	}
	if got, want := len(ack.Body), 9+2*currentMagicBoxEntrySize; got != want {
		t.Fatalf("ack body len = %d, want %d", got, want)
	}
	if len(got.PostActions) == 0 {
		t.Fatalf("expected container refresh post action")
	}
}

func TestOwnerAppliedMagicBoxOverflowRequestsMailboxAlarm(t *testing.T) {
	got := ownerAppliedMagicBoxResult(uint16(dnfenum.CmdPacketUseRandomboxItem), "use_magic_box", MagicBoxResult{
		CharacterID:    "77",
		Success:        true,
		Changed:        true,
		OverflowMailID: "1",
	})
	if got.MailboxAlarmRecipientID != 77 {
		t.Fatalf("mailbox alarm recipient = %d, want 77", got.MailboxAlarmRecipientID)
	}
}

func TestOwnerAppliedMagicBoxDoesNotScheduleCustomSeriaGauge(t *testing.T) {
	tests := []struct {
		name      string
		opcode    uint16
		boxItemID int64
	}{
		{name: "seria_single", opcode: uint16(dnfenum.CmdPacketUseRandomboxItem), boxItemID: currentSeriaLuckItemID},
		{name: "seria_batch", opcode: uint16(dnfenum.CmdPacketUseRandomboxItemExpand), boxItemID: currentSeriaLuckItemID},
		{name: "ordinary_single", opcode: uint16(dnfenum.CmdPacketUseRandomboxItem), boxItemID: 8001},
		{name: "ordinary_batch", opcode: uint16(dnfenum.CmdPacketUseRandomboxItemExpand), boxItemID: 8001},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ownerAppliedMagicBoxResult(test.opcode, "magic_box_test", MagicBoxResult{
				Success:    true,
				BoxItemID:  test.boxItemID,
				ClientType: clientMainListType,
			})
			if len(got.PostActions) != 1 ||
				got.PostActions[0] != alignedcmd.PostActionRefreshSelectedItemContainers {
				t.Fatalf("post actions=%v, want only selected container refresh", got.PostActions)
			}
		})
	}
}

func TestBuildMagicBoxSingleAckCarriesSeriaDoubleFlagBeforeSlots(t *testing.T) {
	result := MagicBoxResult{
		ClientType:               clientMainListType,
		BoxSlotIndex:             65,
		MaterialSlotIndex:        -1,
		SeriaLuckDoubleTriggered: true,
		Rewards: []MagicBoxGrantedReward{{
			Slot:   82,
			ItemID: 2600014,
			Count:  20,
		}},
	}

	got := buildMagicBoxSingleAck(result)
	want := []byte{0x01, 0x04, 0x01, 0x41, 0x00, 0xFF, 0xFF, 0x01, 0x00}
	row := make([]byte, currentMagicBoxEntrySize)
	binary.LittleEndian.PutUint16(row[0x00:0x02], 82)
	binary.LittleEndian.PutUint32(row[0x02:0x06], 2600014)
	binary.LittleEndian.PutUint32(row[0x06:0x0A], 20)
	want = append(want, row...)
	if string(got) != string(want) {
		t.Fatalf("single seria ack = %x, want %x", got, want)
	}
	if got, want := len(got), 9+currentMagicBoxEntrySize; got != want {
		t.Fatalf("single seria ack len = %d, want %d", got, want)
	}
}

func TestBuildMagicBoxSingleAckCarriesZeroDoubleFlagForOrdinarySeriaOpen(t *testing.T) {
	result := MagicBoxResult{
		ClientType:        clientMainListType,
		BoxSlotIndex:      85,
		MaterialSlotIndex: -1,
		Rewards: []MagicBoxGrantedReward{{
			Slot:   108,
			ItemID: 10000391,
			Count:  2,
		}},
	}

	got := buildMagicBoxSingleAck(result)
	want := []byte{0x01, 0x04, 0x00, 0x55, 0x00, 0xFF, 0xFF, 0x01, 0x00}
	row := make([]byte, currentMagicBoxEntrySize)
	binary.LittleEndian.PutUint16(row[0x00:0x02], 108)
	binary.LittleEndian.PutUint32(row[0x02:0x06], 10000391)
	binary.LittleEndian.PutUint32(row[0x06:0x0A], 2)
	want = append(want, row...)
	if string(got) != string(want) {
		t.Fatalf("ordinary seria ack = %x, want %x", got, want)
	}
	if got, want := len(got), 9+currentMagicBoxEntrySize; got != want {
		t.Fatalf("ordinary seria ack len = %d, want %d", got, want)
	}
}

func TestHandlerMagicBoxSingleSeriaUsesCurrentEXECase4Header(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveMagicBoxFixture(t, ctx, repos, map[string]dnfrepo.ItemStack{
		"0:85": {
			ItemID: 2682272,
			Count:  1,
			Extra:  map[string]string{"item_kind": "stackable"},
		},
	})
	if err := repos.Account.Save(ctx, dnfrepo.AccountRecord{
		AccountID: "acc",
		Metadata:  map[string]string{currentSeriaLuckMetadataKey: "0"},
	}); err != nil {
		t.Fatal(err)
	}
	resolution := alignedcmd.MagicBoxResolution{
		Kind:   "random",
		Groups: []alignedcmd.MagicBoxRewardGroup{{DrawCount: 1, Entries: []alignedcmd.MagicBoxRewardEntry{{ItemID: 10000391, Weight: 100, Count: 2}}}},
	}
	got := handleMagicBoxRequest(
		t,
		repos,
		staticMagicBoxResolver(resolution),
		staticMagicBoxRewardResolver(map[int64]alignedcmd.MagicBoxRewardItem{
			10000391: {ItemID: 10000391, Kind: "stackable", SlotStart: 65, SlotEnd: 120},
		}),
		[]byte{0x04, 0x55, 0x00, 0xFF, 0xFF},
	)
	if !got.Handled || !got.ResponseAllowed || len(got.UpperResponses) != 1 {
		t.Fatalf("result = %+v", got)
	}
	ack := got.UpperResponses[0]
	want := []byte{0x01, 0x04, 0x00, 0x55, 0x00, 0xFF, 0xFF, 0x01, 0x00}
	row := make([]byte, currentMagicBoxEntrySize)
	binary.LittleEndian.PutUint16(row[0x00:0x02], 65)
	binary.LittleEndian.PutUint32(row[0x02:0x06], 10000391)
	binary.LittleEndian.PutUint32(row[0x06:0x0A], 2)
	want = append(want, row...)
	if string(ack.Body) != string(want) {
		t.Fatalf("single seria case4 ack = %x, want %x", ack.Body, want)
	}
}

func TestHandlerMagicBoxFailureAcksZeroByte(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveMagicBoxFixture(t, ctx, repos, magicBoxBaseSlots())

	got := handleMagicBoxRequest(t, repos, staticMagicBoxResolver(magicBoxTestResolution()), staticMagicBoxRewardResolver(magicBoxTestRewardItems()), magicBoxRequestBody(99, 121))
	if !got.Handled || !got.ResponseAllowed || len(got.UpperResponses) != 1 {
		t.Fatalf("result = %+v", got)
	}
	if string(got.UpperResponses[0].Body) != string([]byte{0x00}) {
		t.Fatalf("failure ack = %x, want {00}", got.UpperResponses[0].Body)
	}
}

func TestHandlerMagicBoxParseFailureFailsClosed(t *testing.T) {
	repos := dnfrepomemory.NewMemoryGroup()
	got := handleMagicBoxRequest(t, repos, staticMagicBoxResolver(magicBoxTestResolution()), staticMagicBoxRewardResolver(magicBoxTestRewardItems()), []byte{9})
	if !got.Handled {
		t.Fatalf("result = %+v", got)
	}
	if got.ResponseAllowed || len(got.UpperResponses) != 0 {
		t.Fatalf("parse failure must fail closed without any response: %+v", got)
	}
}

func TestHandlerMagicBoxNilResolversFailClosed(t *testing.T) {
	repos := dnfrepomemory.NewMemoryGroup()
	got := handleMagicBoxRequest(t, repos, nil, nil, magicBoxRequestBody(10, 121))
	if !got.Handled {
		t.Fatalf("result = %+v", got)
	}
	if got.ResponseAllowed || len(got.UpperResponses) != 0 {
		t.Fatalf("nil resolvers must fail closed without any response: %+v", got)
	}
}
