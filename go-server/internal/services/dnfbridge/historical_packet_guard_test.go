package dnfbridge

import (
	"testing"

	"longheng.io/server/internal/modules/dnf/channelcatalog"
)

func TestHistoricalScenePacketSourcesAreFailClosed(t *testing.T) {
	service := &Service{}
	connection := &bufferConn{}
	session := &gameSession{
		conn:    connection,
		connID:  "historical-scene-guard",
		channel: channelcatalog.Channel{ID: 19},
	}

	tests := []csharpSelectInitPacket{
		{
			class: 0,
			msgID: 1021,
			file:  "fixtures/dove_scene/captured_transport.bin",
		},
		{
			class:     0,
			msgID:     1021,
			bodyCodec: "dove_scene_op1021_transport_zlib",
		},
	}
	for _, packet := range tests {
		if err := service.sendCSharpSelectInitPacket(session, packet, []byte{1, 2, 3}); err != nil {
			t.Fatalf("block historical scene packet %+v: %v", packet, err)
		}
	}
	if connection.write.Len() != 0 {
		t.Fatalf("historical scene packet wrote %d bytes", connection.write.Len())
	}
}

func TestFixed16TransportBlocksHistoricalCodecAndAllowsCurrentBuilder(t *testing.T) {
	service := &Service{}
	connection := &bufferConn{}
	session := &gameSession{
		conn:    connection,
		connID:  "historical-transport-guard",
		channel: channelcatalog.Channel{ID: 19},
	}

	for _, codec := range []string{
		"dove_reference_s2c_wire",
		"captured_transport",
		"pcap_fixture_transport",
		"legacy_replay_transport",
	} {
		if err := service.sendGameUpperFixed16Transport(
			session,
			1021,
			[]byte{1, 2, 3},
			0,
			1,
			true,
			codec,
		); err != nil {
			t.Fatalf("block historical fixed16 body codec %q: %v", codec, err)
		}
	}
	if connection.write.Len() != 0 {
		t.Fatalf("historical fixed16 body wrote %d bytes", connection.write.Len())
	}

	if err := service.sendGameUpperFixed16Transport(
		session,
		1021,
		[]byte{1, 2, 3},
		0,
		1,
		true,
		"current_op1021_scene_state_transport_zlib",
	); err != nil {
		t.Fatalf("send current fixed16 body: %v", err)
	}
	if connection.write.Len() == 0 {
		t.Fatal("current fixed16 body wrote no bytes")
	}
}
