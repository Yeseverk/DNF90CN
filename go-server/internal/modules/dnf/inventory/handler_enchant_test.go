package inventory

import (
	"context"
	"testing"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func handleEnchantRequest(t *testing.T, repos dnfrepo.Group, resolver alignedcmd.EnchantBeadResolver, body []byte) alignedcmd.Result {
	t.Helper()
	got, err := NewHandler().Handle(context.Background(), alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketEnchantByBead),
		Body:                body,
		AccountID:           "acc",
		SelectedCharacterID: 77,
		Repositories:        repos,
		EnchantBeadResolver: resolver,
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	return got
}

func enchantRequestBody(beadList byte, beadSlot int16, targetList byte, targetSlot int16) []byte {
	return []byte{
		beadList,
		byte(beadSlot), byte(beadSlot >> 8),
		targetList,
		byte(targetSlot), byte(targetSlot >> 8),
	}
}

func TestHandlerEnchantByBeadSendsRefreshThenAck(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveEnchantFixture(t, ctx, repos, 2, 0, 0)

	got := handleEnchantRequest(t, repos, coatEnchantResolver(9001, nil), enchantRequestBody(listTypeMain, 12, listTypeMain, 30))
	if !got.Handled || !got.ResponseAllowed {
		t.Fatalf("result = %+v", got)
	}
	if len(got.UpperResponses) != 2 {
		t.Fatalf("responses = %d, want refresh then ack", len(got.UpperResponses))
	}
	refresh := got.UpperResponses[0]
	if refresh.MsgID != msgItemListRefresh || refresh.Classification != 0 {
		t.Fatalf("first response = msg 0x%04X class %d, want class0 0x000D refresh", refresh.MsgID, refresh.Classification)
	}
	ack := got.UpperResponses[1]
	if ack.MsgID != uint16(dnfenum.CmdPacketEnchantByBead) {
		t.Fatalf("ack msg = 0x%04X, want 0x0110", ack.MsgID)
	}
	wantAck := []byte{1, listTypeMain, 30, 0}
	if string(ack.Body) != string(wantAck) {
		t.Fatalf("ack body = %x, want %x", ack.Body, wantAck)
	}
	if len(got.PostActions) != 0 {
		t.Fatalf("post actions = %v, want no duplicate refresh after ACK", got.PostActions)
	}
}

func TestHandlerEnchantPetCreatureSendsTargetAndBeadUpdatesBeforeAck(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	savePetEnchantFixture(t, ctx, repos, 1)

	got := handleEnchantRequest(t, repos, creatureEnchantResolver(10008663, 400990168), enchantRequestBody(listTypeMain, 12, listTypePet, 24))
	if !got.Handled || !got.ResponseAllowed || len(got.UpperResponses) != 3 {
		t.Fatalf("result = %+v", got)
	}
	petUpdate := got.UpperResponses[0]
	if petUpdate.Classification != 0 || petUpdate.MsgID != msgItemListUpdate || len(petUpdate.Body) != 3+currentItemListEntrySize || petUpdate.Body[0] != listTypePet {
		t.Fatalf("pet update = class%d msg0x%04X body=%x", petUpdate.Classification, petUpdate.MsgID, petUpdate.Body)
	}
	petRow := petUpdate.Body[3:]
	if gotCard := uint32(petRow[0x0E]) | uint32(petRow[0x0F])<<8 | uint32(petRow[0x10])<<16 | uint32(petRow[0x11])<<24; gotCard != 10008663 {
		t.Fatalf("pet update card=%d want=10008663", gotCard)
	}
	beadUpdate := got.UpperResponses[1]
	if beadUpdate.Classification != 0 || beadUpdate.MsgID != msgItemListUpdate || len(beadUpdate.Body) != 3+currentItemListEntrySize || beadUpdate.Body[0] != listTypeMain {
		t.Fatalf("bead update = class%d msg0x%04X body=%x", beadUpdate.Classification, beadUpdate.MsgID, beadUpdate.Body)
	}
	beadRow := beadUpdate.Body[3:]
	if beadRow[0x02] != 0xFF || beadRow[0x03] != 0xFF || beadRow[0x04] != 0xFF || beadRow[0x05] != 0xFF {
		t.Fatalf("consumed bead row did not carry empty item marker: %x", beadRow[:10])
	}
	ack := got.UpperResponses[2]
	if ack.MsgID != uint16(dnfenum.CmdPacketEnchantByBead) || string(ack.Body) != string([]byte{1, listTypePet, 24, 0}) {
		t.Fatalf("ack = msg0x%04X body=%x", ack.MsgID, ack.Body)
	}
}

func TestHandlerEnchantByBeadErrorAckOnly(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveEnchantFixture(t, ctx, repos, 2, 0, 0)

	got := handleEnchantRequest(t, repos, coatEnchantResolver(0, nil), enchantRequestBody(listTypeMain, 12, listTypeMain, 30))
	if !got.Handled || !got.ResponseAllowed {
		t.Fatalf("result = %+v", got)
	}
	if len(got.UpperResponses) != 1 {
		t.Fatalf("responses = %d, want single error ack", len(got.UpperResponses))
	}
	ack := got.UpperResponses[0]
	if ack.MsgID != uint16(dnfenum.CmdPacketEnchantByBead) {
		t.Fatalf("ack msg = 0x%04X, want 0x0110", ack.MsgID)
	}
	if string(ack.Body) != string([]byte{0, enchantErrorInvalidBead}) {
		t.Fatalf("ack body = %x, want {0,0x11}", ack.Body)
	}
}

func TestHandlerEnchantByBeadParseFailureAcksInvalidBead(t *testing.T) {
	repos := dnfrepomemory.NewMemoryGroup()
	got := handleEnchantRequest(t, repos, coatEnchantResolver(9001, nil), []byte{1, 2})
	if !got.Handled || !got.ResponseAllowed || len(got.UpperResponses) != 1 {
		t.Fatalf("result = %+v", got)
	}
	if string(got.UpperResponses[0].Body) != string([]byte{0, enchantErrorInvalidBead}) {
		t.Fatalf("ack body = %x, want {0,0x11}", got.UpperResponses[0].Body)
	}
}

func TestHandlerEnchantByBeadNilResolverFailsClosed(t *testing.T) {
	repos := dnfrepomemory.NewMemoryGroup()
	got := handleEnchantRequest(t, repos, nil, enchantRequestBody(listTypeMain, 12, listTypeMain, 30))
	if !got.Handled {
		t.Fatalf("result = %+v", got)
	}
	if got.ResponseAllowed || len(got.UpperResponses) != 0 {
		t.Fatalf("nil resolver must fail closed without any response: %+v", got)
	}
}
