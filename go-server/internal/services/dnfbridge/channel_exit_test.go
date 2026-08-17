package dnfbridge

import (
	"bytes"
	"encoding/binary"
	"testing"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
)

func TestCurrentChannelExitLegacyRouteReturnsExact90CNAckAndKeepsSessionOpen(t *testing.T) {
	connection := &closeTrackingBufferConn{}
	service := &Service{options: options{
		gameUpperHeader:    gameUpperHeaderServer16,
		gameUpperBodyCodec: gameUpperBodyCodecPlain,
	}}
	session := &gameSession{
		conn:   connection,
		connID: "channel-exit-legacy-test",
	}
	request := buildLegacyGamePacketForBridgeTest(
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.CmdPacketExit),
		9,
		[]byte{1},
	)
	packets, remaining, skipped, err := dnfproto.SplitLatestGameStream(request, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if len(packets) != 1 || packets[0].Kind != dnfproto.LatestGameStreamLegacy ||
		len(remaining) != 0 || skipped != 0 {
		t.Fatalf(
			"channel exit split packets=%d kind=%d remaining=%x skipped=%d",
			len(packets),
			func() dnfproto.LatestGameStreamKind {
				if len(packets) == 0 {
					return 0
				}
				return packets[0].Kind
			}(),
			remaining,
			skipped,
		)
	}
	if err := service.handleGameStreamPacket(session, packets[0]); err != nil {
		t.Fatal(err)
	}

	raw := connection.write.Bytes()
	if len(raw) != dnfproto.GameServerUpperHeaderSize16+1 ||
		binary.LittleEndian.Uint32(raw[3:7]) != uint32(len(raw)) ||
		!bytes.Equal(raw[11:16], make([]byte, 5)) {
		t.Fatalf("channel exit ACK raw header=%x len=%d", raw, len(raw))
	}
	packet, rest := splitLongHengGameServerUpperPacket(t, raw)
	if packet.Header.Classification != dnfproto.DefaultChannelClassification ||
		packet.Header.MsgID != uint16(dnfenum.CmdPacketExit) ||
		!bytes.Equal(packet.Body, []byte{1}) ||
		len(rest) != 0 {
		t.Fatalf("channel exit ACK header=%+v body=%x rest=%x", packet.Header, packet.Body, rest)
	}
	if connection.closed {
		t.Fatal("server closed old channel socket before current client owned the teardown")
	}
}

func TestCurrentChannelExitUpperBody16RemainsNonFatalAndDoesNotReply(t *testing.T) {
	connection := &closeTrackingBufferConn{}
	service := &Service{options: options{
		gameUpperHeader:    gameUpperHeaderServer16,
		gameUpperBodyCodec: gameUpperBodyCodecPlain,
	}}
	session := &gameSession{
		conn:   connection,
		connID: "channel-op3-body16-test",
	}
	request, err := dnfproto.BuildChannelPacket(
		uint16(dnfenum.CmdPacketExit),
		make([]byte, 16),
		7,
		dnfproto.DefaultChannelClassification,
	)
	if err != nil {
		t.Fatal(err)
	}

	packets, remaining, skipped, err := dnfproto.SplitLatestGameStream(request, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if len(packets) != 1 || packets[0].Kind != dnfproto.LatestGameStreamUpper ||
		len(remaining) != 0 || skipped != 0 {
		t.Fatalf("op3/body16 split packets=%d remaining=%x skipped=%d", len(packets), remaining, skipped)
	}
	if err := service.handleGameStreamPacket(session, packets[0]); err != nil {
		t.Fatal(err)
	}
	if connection.write.Len() != 0 {
		t.Fatalf("op3/body16 emitted exit ACK bytes=%x", connection.write.Bytes())
	}
	if connection.closed {
		t.Fatal("op3/body16 caused the game socket to close")
	}
}

type closeTrackingBufferConn struct {
	bufferConn
	closed bool
}

func (c *closeTrackingBufferConn) Close() error {
	c.closed = true
	return nil
}

func TestCurrentChannelExitRejectsUnexpectedBodyWithoutReply(t *testing.T) {
	connection := &bufferConn{}
	service := &Service{}
	session := &gameSession{conn: connection}

	if err := service.handleCurrentChannelExit(session, nil, "test"); err == nil {
		t.Fatal("bodyless channel exit accepted")
	}
	if connection.write.Len() != 0 {
		t.Fatalf("invalid channel exit emitted bytes=%x", connection.write.Bytes())
	}
}
