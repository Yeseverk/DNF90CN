package inventory

import (
	"bytes"
	"context"
	"testing"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func handleRandomOptionRequest(t *testing.T, repos dnfrepo.Group, opcode dnfenum.CmdPacket, body []byte, resolver alignedcmd.RandomOptionResolver) alignedcmd.Result {
	t.Helper()
	result, err := NewHandler().Handle(context.Background(), alignedcmd.Request{
		Opcode:               uint16(opcode),
		Body:                 body,
		SelectedCharacterID:  77,
		Repositories:         repos,
		RandomOptionResolver: resolver,
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestHandlerRandomOptionSuccessOrdersUpdateThenACK(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveRandomOptionFixture(t, ctx, repos, 1000, nil, nil)
	got := handleRandomOptionRequest(t, repos, dnfenum.CmdPacketUnsealRandomOption, []byte{9, 0, 0xFF, 0xFF}, staticRandomOptionResolver(testRandomOptionResolution()))
	if !got.Handled || !got.ResponseAllowed || got.Operation != "unseal_random_option" || len(got.UpperResponses) != 2 || len(got.PostActions) != 1 {
		t.Fatalf("result = %+v", got)
	}
	update := got.UpperResponses[0]
	if update.Classification != 0 || update.MsgID != msgItemListUpdate || len(update.Body) != 3+currentItemListEntrySize || update.Body[0] != listTypeMain || update.Body[1] != 1 {
		t.Fatalf("update = %+v body=% X", update, update.Body)
	}
	if update.Body[3] != 9 || update.Body[3+randomOptionCountOffset] != 3 {
		t.Fatalf("update row header/options = % X", update.Body)
	}
	ack := got.UpperResponses[1]
	if ack.Classification != 1 || ack.MsgID != uint16(dnfenum.CmdPacketUnsealRandomOption) || !bytes.Equal(ack.Body, []byte{1}) {
		t.Fatalf("ACK = %+v", ack)
	}
	if got.PostActions[0] != alignedcmd.PostActionRefreshSelectedItemContainers {
		t.Fatalf("post actions = %+v", got.PostActions)
	}
}

func TestHandlerRandomOptionFailureReturnsSameOpcodeStatusOnly(t *testing.T) {
	repos := dnfrepomemory.NewMemoryGroup()
	for _, test := range []struct {
		opcode dnfenum.CmdPacket
		body   []byte
	}{
		{opcode: dnfenum.CmdPacketUnsealRandomOption, body: []byte{9, 0, 0}},
		{opcode: dnfenum.CmdPacketChangeRandomOption, body: []byte{9, 0, 1, 0, 0}},
	} {
		got := handleRandomOptionRequest(t, repos, test.opcode, test.body, nil)
		if !got.Handled || !got.ResponseAllowed || len(got.UpperResponses) != 1 || len(got.PostActions) != 0 {
			t.Fatalf("result = %+v", got)
		}
		ack := got.UpperResponses[0]
		if ack.Classification != 1 || ack.MsgID != uint16(test.opcode) || !bytes.Equal(ack.Body, []byte{0}) {
			t.Fatalf("ACK = %+v", ack)
		}
	}
}
