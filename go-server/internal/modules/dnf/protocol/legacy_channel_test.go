package protocol

import (
	"bytes"
	"encoding/binary"
	"testing"

	"longheng.io/server/internal/modules/dnf/dnfenum"
)

func TestDofLoginAckUsesLegacyPrefixAndKey(t *testing.T) {
	ack := BuildDofLoginAck("20260629000006")
	if len(ack) != 47 {
		t.Fatalf("ack len = %d, want 47", len(ack))
	}
	if ack[0] != dnfenum.ChannelPacketClass || ack[1] != legacyMsgID(dnfenum.LegacyMsgDofLoginAck) {
		t.Fatalf("ack header = %x", ack[:LegacyChannelHeaderSize])
	}
	if got := binary.LittleEndian.Uint32(ack[2:6]); got != uint32(len(ack)) {
		t.Fatalf("ack len field = %d, want %d", got, len(ack))
	}
	if got := string(bytes.TrimRight(ack[15:], "\x00")); got != "20260629000006" {
		t.Fatalf("ack key = %q", got)
	}
}

func TestDofLegacyPrefixDetection(t *testing.T) {
	preface := make([]byte, DofLoginPrefaceSize)
	copy(preface, BuildLegacyClientPacket(dnfenum.LegacyMsgDofLoginPreface, nil))
	if !IsDofLoginPrefacePrefix(preface) {
		t.Fatal("expected login preface prefix")
	}
	if IsDofAskChannelPrefix(preface) {
		t.Fatal("login preface must not match ask channel")
	}

	ask := make([]byte, DofAskChannelSize)
	copy(ask, BuildLegacyClientPacket(dnfenum.LegacyMsgAskChannelInfo, nil))
	if !IsDofAskChannelPrefix(ask) {
		t.Fatal("expected ask channel prefix")
	}
}

func TestLegacyHeaderUsesActualPacketLength(t *testing.T) {
	packet := BuildLegacyChannelPacket(dnfenum.LegacyMsgChannelInfo, []byte{1, 2, 3})
	if packet[1] != legacyMsgID(dnfenum.LegacyMsgChannelInfo) {
		t.Fatalf("msg id = %d", packet[1])
	}
	if got := binary.LittleEndian.Uint32(packet[2:6]); got != uint32(len(packet)) {
		t.Fatalf("len field = %d, want %d", got, len(packet))
	}
}

func legacyMsgID(msg dnfenum.LegacyChannelMsg) byte {
	return byte(uint16(msg))
}
