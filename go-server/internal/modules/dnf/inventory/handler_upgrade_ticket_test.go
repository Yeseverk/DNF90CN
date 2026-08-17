package inventory

import (
	"context"
	"testing"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func handleUpgradeTicketRequest(t *testing.T, repos dnfrepo.Group, resolver alignedcmd.UpgradeTicketResolver, body []byte) alignedcmd.Result {
	t.Helper()
	got, err := NewHandler().Handle(context.Background(), alignedcmd.Request{
		Opcode:                uint16(dnfenum.CmdPacketUpgradeItem),
		Body:                  body,
		AccountID:             "acc",
		SelectedCharacterID:   77,
		Repositories:          repos,
		UpgradeTicketResolver: resolver,
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	return got
}

func upgradeTicketRequestBody(targetSlot int16, targetItemID int32, materialSlot int16) []byte {
	body := []byte{
		0x00, 0x00,
		byte(targetSlot), byte(targetSlot >> 8),
		byte(targetItemID), byte(targetItemID >> 8), byte(targetItemID >> 16), byte(targetItemID >> 24),
		byte(materialSlot), byte(materialSlot >> 8),
		0xFF, 0xFF,
		0x04, 0x00, 0x00, 0x00,
		't', 'e', 's', 't',
	}
	return body
}

func TestHandlerUpgradeTicketSendsAckThenSlotUpdate(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveUpgradeTicketFixture(t, ctx, repos, nil, 2, 2)

	got := handleUpgradeTicketRequest(t, repos, reinforceTicketResolver(10, 100000), upgradeTicketRequestBody(9, 700, 121))
	if !got.Handled || !got.ResponseAllowed {
		t.Fatalf("result = %+v", got)
	}
	if len(got.UpperResponses) != 2 {
		t.Fatalf("responses = %d, want ack then refresh", len(got.UpperResponses))
	}
	ack := got.UpperResponses[0]
	if ack.MsgID != uint16(dnfenum.CmdPacketUpgradeItem) {
		t.Fatalf("ack msg = 0x%04X, want 0x0050", ack.MsgID)
	}
	wantAck := []byte{
		0x01,
		0x00,
		0x79, 0x00,
		0x01, 0x00, 0x00, 0x00,
		0xFF, 0xFF,
		0x00,
		0x02,
		0x00,
		0x0A,
		0x00,
		0x09, 0x00,
		0xFF, 0xFF,
	}
	if string(ack.Body) != string(wantAck) {
		t.Fatalf("ack body = %x, want %x", ack.Body, wantAck)
	}
	refresh := got.UpperResponses[1]
	if refresh.MsgID != msgItemListUpdate || refresh.Classification != 0 {
		t.Fatalf("second response = msg 0x%04X class %d, want class0 0x000E slot update", refresh.MsgID, refresh.Classification)
	}
	if len(got.PostActions) != 0 {
		t.Fatalf("unexpected post actions: %+v", got.PostActions)
	}
}

func TestHandlerUpgradeTicketErrorAckOnly(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveUpgradeTicketFixture(t, ctx, repos, nil, 31, 2)

	got := handleUpgradeTicketRequest(t, repos, reinforceTicketResolver(10, 100000), upgradeTicketRequestBody(9, 700, 121))
	if !got.Handled || !got.ResponseAllowed || len(got.UpperResponses) != 1 {
		t.Fatalf("result = %+v", got)
	}
	if string(got.UpperResponses[0].Body) != string([]byte{0, upgradeTicketErrorMaxLevel}) {
		t.Fatalf("ack body = %x, want {0,95}", got.UpperResponses[0].Body)
	}
}

func TestHandlerUpgradeTicketNonTicketStaysBlocked(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveUpgradeTicketFixture(t, ctx, repos, nil, 2, 2)

	got := handleUpgradeTicketRequest(t, repos, staticUpgradeTicketResolver(alignedcmd.UpgradeTicketResolution{TargetKind: "equipment"}), upgradeTicketRequestBody(9, 700, 121))
	if !got.Handled {
		t.Fatalf("result = %+v", got)
	}
	if got.ResponseAllowed || len(got.UpperResponses) != 0 {
		t.Fatalf("non-ticket material must stay on the pending path: %+v", got)
	}
}

func TestHandlerUpgradeTicketParseFailureAcksInvalidTarget(t *testing.T) {
	repos := dnfrepomemory.NewMemoryGroup()
	got := handleUpgradeTicketRequest(t, repos, reinforceTicketResolver(10, 100000), []byte{1, 2})
	if !got.Handled || !got.ResponseAllowed || len(got.UpperResponses) != 1 {
		t.Fatalf("result = %+v", got)
	}
	if string(got.UpperResponses[0].Body) != string([]byte{0, upgradeTicketErrorInvalidTarget}) {
		t.Fatalf("ack body = %x, want {0,4}", got.UpperResponses[0].Body)
	}
}

func TestHandlerUpgradeTicketNilResolverFailsClosed(t *testing.T) {
	repos := dnfrepomemory.NewMemoryGroup()
	got := handleUpgradeTicketRequest(t, repos, nil, upgradeTicketRequestBody(9, 700, 121))
	if !got.Handled {
		t.Fatalf("result = %+v", got)
	}
	if got.ResponseAllowed || len(got.UpperResponses) != 0 {
		t.Fatalf("nil resolver must fail closed without any response: %+v", got)
	}
}
