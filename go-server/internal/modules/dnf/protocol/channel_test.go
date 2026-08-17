package protocol

import (
	"bytes"
	"encoding/hex"
	"errors"
	"testing"
)

func TestBuildAndParseChannelPacket(t *testing.T) {
	body := []byte{1, 2, 3}
	got, err := BuildChannelPacket(1, body, 7, DefaultChannelClassification)
	if err != nil {
		t.Fatalf("build channel packet: %v", err)
	}

	want := mustHex(t, "0101001000000028a15aee0700010203")
	if !bytes.Equal(got, want) {
		t.Fatalf("channel packet mismatch:\n got %x\nwant %x", got, want)
	}

	packet, err := ParseChannelPacket(got)
	if err != nil {
		t.Fatalf("parse channel packet: %v", err)
	}
	if packet.Header.Classification != DefaultChannelClassification ||
		packet.Header.MsgID != 1 ||
		packet.Header.Length != uint32(len(got)) ||
		packet.Header.Checksum != 3998916904 ||
		packet.Header.Seq != 7 {
		t.Fatalf("unexpected header: %+v", packet.Header)
	}
	if !bytes.Equal(packet.Body, body) {
		t.Fatalf("body mismatch: got %x", packet.Body)
	}
}

func TestParseChannelPacketRejectsChecksum(t *testing.T) {
	packet, err := BuildChannelPacket(1, []byte{1, 2, 3}, 7, DefaultChannelClassification)
	if err != nil {
		t.Fatalf("build channel packet: %v", err)
	}
	packet[len(packet)-1] ^= 255

	if _, err := ParseChannelPacket(packet); !errors.Is(err, ErrChecksumInvalid) {
		t.Fatalf("expected checksum error, got %v", err)
	}
}

func TestParseChannelPacketUncheckedAcceptsCodecUpper(t *testing.T) {
	raw := mustHex(t, "01b2021d00000038ab3cdc000015b101e6d58515bd9dbb3490ac32e75a")

	if _, err := ParseChannelPacket(raw); !errors.Is(err, ErrChecksumInvalid) {
		t.Fatalf("strict parse error = %v, want checksum invalid", err)
	}
	packet, err := ParseChannelPacketUnchecked(raw)
	if err != nil {
		t.Fatalf("unchecked parse: %v", err)
	}
	if packet.Header.MsgID != 0x02b2 || packet.Header.Length != uint32(len(raw)) || len(packet.Body) != 16 {
		t.Fatalf("packet header=%+v body_len=%d", packet.Header, len(packet.Body))
	}
}

func TestBuildGameServerUpperPacketUsesChannel13Header(t *testing.T) {
	body := []byte{1, 2, 3}
	got, err := BuildGameServerUpperPacket(1, body, 7, DefaultChannelClassification)
	if err != nil {
		t.Fatalf("build game server upper packet: %v", err)
	}

	if len(got) != GameServerUpperHeaderSize+len(body) {
		t.Fatalf("packet len = %d", len(got))
	}
	if got[0] != DefaultChannelClassification {
		t.Fatalf("classification = %d", got[0])
	}
	if got[1] != 1 || got[2] != 0 {
		t.Fatalf("msg bytes = %x", got[1:3])
	}
	if !bytes.Equal(got[GameServerUpperHeaderSize:], body) {
		t.Fatalf("body mismatch: got %x", got[GameServerUpperHeaderSize:])
	}
}

func TestBuildGameServerUpperPacketAllowsServer16Header(t *testing.T) {
	body := []byte{1, 2, 3}
	got, err := BuildGameServerUpperPacketWithHeaderSize(1, body, 7, DefaultChannelClassification, GameServerUpperHeaderSize16)
	if err != nil {
		t.Fatalf("build game server upper packet: %v", err)
	}

	if len(got) != GameServerUpperHeaderSize16+len(body) {
		t.Fatalf("packet len = %d", len(got))
	}
	if got[13] != 0 || got[14] != 0 || got[15] != 0 {
		t.Fatalf("reserved bytes = %x", got[13:16])
	}
	if !bytes.Equal(got[GameServerUpperHeaderSize16:], body) {
		t.Fatalf("body mismatch: got %x", got[GameServerUpperHeaderSize16:])
	}
}

func TestBuildGameServerUpperPacketAllowsSecondaryClass(t *testing.T) {
	body := []byte{2, 0x20, 0}
	got, err := BuildGameServerUpperPacket(2, body, 7, 0)
	if err != nil {
		t.Fatalf("build game server secondary upper packet: %v", err)
	}

	if got[0] != 0 {
		t.Fatalf("classification = %d, want 0", got[0])
	}
	if got[1] != 2 || got[2] != 0 {
		t.Fatalf("msg bytes = %x", got[1:3])
	}
	if !bytes.Equal(got[GameServerUpperHeaderSize:], body) {
		t.Fatalf("body mismatch: got %x", got[GameServerUpperHeaderSize:])
	}
}

func mustHex(t *testing.T, value string) []byte {
	t.Helper()
	data, err := hex.DecodeString(value)
	if err != nil {
		t.Fatalf("decode hex: %v", err)
	}
	return data
}
