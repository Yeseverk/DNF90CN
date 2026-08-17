package dnfbridge

import (
	"bytes"
	"testing"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
)

func TestSendAlignedWearAckDoesNotAppendItemRowsOrUserStateRebuild(t *testing.T) {
	for _, test := range []struct {
		name    string
		ackBody []byte
	}{
		{
			name:    "ordinary_equipment",
			ackBody: []byte{1, 0, 9, 0, 1, 0, 0, 0, 3, 11, 0},
		},
		{
			name:    "fashion",
			ackBody: []byte{1, 1, 9, 0, 1, 0, 0, 0, 17, 3, 0},
		},
		{
			name:    "pet_artifact",
			ackBody: []byte{1, 7, 140, 0, 1, 0, 0, 0, 17, 27, 0},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			connection := &bufferConn{}
			service := &Service{
				options: options{
					gameUpperHeader:    gameUpperHeaderServer16,
					gameUpperBodyCodec: gameUpperBodyCodecPlain,
				},
			}
			session := &gameSession{
				conn:                      connection,
				connID:                    "aligned-native-wear-ack-" + test.name,
				selectedCharacterID:       19,
				townSceneReadyCharacterID: 19,
			}
			result := alignedcmd.Result{
				Operation: "move_itemspace",
				UpperResponses: []alignedcmd.UpperResponse{{
					MsgID:          uint16(dnfenum.CmdPacketMoveItemspace),
					Body:           test.ackBody,
					Classification: dnfproto.DefaultChannelClassification,
					AllowCodec:     true,
				}},
			}

			if err := service.sendAlignedUpperResponses(session, result); err != nil {
				t.Fatalf("sendAlignedUpperResponses error = %v", err)
			}
			ack, trailing := splitLongHengGameServerUpperPacket(t, connection.write.Bytes())
			if ack.Header.MsgID != uint16(dnfenum.CmdPacketMoveItemspace) ||
				ack.Header.Classification != dnfproto.DefaultChannelClassification ||
				!bytes.Equal(ack.Body, test.ackBody) {
				t.Fatalf("ACK header=%+v body=%x", ack.Header, ack.Body)
			}
			if len(trailing) != 0 {
				t.Fatalf("wear ACK appended op13/op14/op357 packets=%x", trailing)
			}
		})
	}
}
