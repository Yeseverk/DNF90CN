package inventory

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func purifyRequestBody(targetSlot int16, targetItem int32, materialSlot int16, materialItem int32) []byte {
	body := make([]byte, 12)
	binary.LittleEndian.PutUint16(body[0:2], uint16(targetSlot))
	binary.LittleEndian.PutUint32(body[2:6], uint32(targetItem))
	binary.LittleEndian.PutUint16(body[6:8], uint16(materialSlot))
	binary.LittleEndian.PutUint32(body[8:12], uint32(materialItem))
	return body
}

func investRequestBody(action byte, targetSlot int16, targetItem int32, materialSlot int16, materialItem int32, selected byte, name string) []byte {
	body := make([]byte, 14)
	body[0] = action
	binary.LittleEndian.PutUint16(body[1:3], uint16(targetSlot))
	binary.LittleEndian.PutUint32(body[3:7], uint32(targetItem))
	binary.LittleEndian.PutUint16(body[7:9], uint16(materialSlot))
	binary.LittleEndian.PutUint32(body[9:13], uint32(materialItem))
	body[13] = selected
	if action == investAmplifyActionPureGold {
		length := make([]byte, 4)
		binary.LittleEndian.PutUint32(length, uint32(len(name)))
		body = append(body, length...)
		body = append(body, name...)
	}
	return body
}

func handleAmplifyRequest(t *testing.T, repos dnfrepo.Group, opcode dnfenum.CmdPacket, body []byte, resolver alignedcmd.AmplifyItemResolver) alignedcmd.Result {
	t.Helper()
	result, err := NewHandler().Handle(context.Background(), alignedcmd.Request{
		Opcode:              uint16(opcode),
		Body:                body,
		SelectedCharacterID: 77,
		Repositories:        repos,
		AmplifyItemResolver: resolver,
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestHandlerAmplifySuccessUsesOnlySelfContainedACK(t *testing.T) {
	ctx := context.Background()
	t.Run("op204", func(t *testing.T) {
		repos := dnfrepomemory.NewMemoryGroup()
		saveAmplifyFixture(t, ctx, repos, unidentifiedAmplifyFlag, 0, 0, 1183, 1)
		resolution := validAmplifyResolution()
		resolution.PurifyMaterialCount = 1
		got := handleAmplifyRequest(t, repos, dnfenum.CmdPacketPurifyItem, purifyRequestBody(9, 700, 12, 1183), staticAmplifyResolver(resolution))
		if !got.Handled || !got.ResponseAllowed || len(got.UpperResponses) != 1 || len(got.PostActions) != 0 {
			t.Fatalf("result = %+v", got)
		}
		ack := got.UpperResponses[0]
		if ack.Classification != 1 || ack.MsgID != uint16(dnfenum.CmdPacketPurifyItem) || len(ack.Body) != 12 || ack.Body[0] != 1 {
			t.Fatalf("ACK = %+v", ack)
		}
	})

	t.Run("op205 Pure Gold", func(t *testing.T) {
		repos := dnfrepomemory.NewMemoryGroup()
		saveAmplifyFixture(t, ctx, repos, 1, 7, 0, 8238, 1)
		resolution := validAmplifyResolution()
		resolution.PureGoldOption = amplifyOptionAll
		resolution.PureGoldMaterialCount = 1
		resolution.PureGoldLevels = []alignedcmd.AmplifyWeightedLevel{{Level: 7, Weight: 1}}
		got := handleAmplifyRequest(t, repos, dnfenum.CmdPacketInvestItemAmplifyOption, investRequestBody(2, 9, 700, 12, 8238, 4, "target"), staticAmplifyResolver(resolution))
		if !got.Handled || !got.ResponseAllowed || len(got.UpperResponses) != 1 || len(got.PostActions) != 0 {
			t.Fatalf("result = %+v", got)
		}
		ack := got.UpperResponses[0]
		if ack.Classification != 1 || ack.MsgID != uint16(dnfenum.CmdPacketInvestItemAmplifyOption) || len(ack.Body) != 14 || ack.Body[0] != 1 || ack.Body[13] != 7 {
			t.Fatalf("ACK = %+v", ack)
		}
	})
}

func TestHandlerAmplifyFailuresReturnProvenErrorEnvelopes(t *testing.T) {
	repos := dnfrepomemory.NewMemoryGroup()
	got := handleAmplifyRequest(t, repos, dnfenum.CmdPacketPurifyItem, []byte{1, 2}, nil)
	if len(got.UpperResponses) != 1 || !bytes.Equal(got.UpperResponses[0].Body, []byte{0, 1}) {
		t.Fatalf("op204 error result = %+v", got)
	}
	got = handleAmplifyRequest(t, repos, dnfenum.CmdPacketInvestItemAmplifyOption, []byte{1, 2}, nil)
	if len(got.UpperResponses) != 1 || !bytes.Equal(got.UpperResponses[0].Body, []byte{0, investAmplifyErrorInvalid}) {
		t.Fatalf("op205 error result = %+v", got)
	}
}
