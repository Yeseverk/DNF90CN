package protocol

import (
	"bytes"
	"errors"
	"testing"
)

func TestBuildAndParseLatestGamePacket(t *testing.T) {
	body := []byte{1, 2, 3}
	got, err := BuildLatestGamePacket(1, 3, body)
	if err != nil {
		t.Fatalf("build latest game packet: %v", err)
	}

	want := mustHex(t, "0103001df67fa8010203")
	if !bytes.Equal(got, want) {
		t.Fatalf("latest game packet mismatch:\n got %x\nwant %x", got, want)
	}

	packet, err := ParseLatestGamePacket(got)
	if err != nil {
		t.Fatalf("parse latest game packet: %v", err)
	}
	if packet.Header.Cmd != 1 || packet.Header.Type != 3 || packet.Header.Checksum != 2826958365 {
		t.Fatalf("unexpected header: %+v", packet.Header)
	}
	if !bytes.Equal(packet.Body, body) {
		t.Fatalf("body mismatch: got %x", packet.Body)
	}
}

func TestParseLatestGamePacketRejectsChecksum(t *testing.T) {
	packet, err := BuildLatestGamePacket(1, 3, []byte{1, 2, 3})
	if err != nil {
		t.Fatalf("build latest game packet: %v", err)
	}
	packet[len(packet)-1] ^= 255
	if _, err := ParseLatestGamePacket(packet); !errors.Is(err, ErrChecksumInvalid) {
		t.Fatalf("expected checksum error, got %v", err)
	}
}
