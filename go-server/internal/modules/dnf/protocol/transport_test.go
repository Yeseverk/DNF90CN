package protocol

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestLatestGameTCPRoundTrip(t *testing.T) {
	body := []byte{1, 2, 3}
	options := TransportOptions{
		Sequence:    9,
		Route:       4,
		Flag:        5,
		OuterKind:   6,
		OuterValue4: 287454020,
		OuterValue8: 1432778632,
	}

	frame, err := BuildLatestGameTCP(1, 3, body, options)
	if err != nil {
		t.Fatalf("build latest game tcp: %v", err)
	}
	if len(frame) != TCPOuterHeaderSize+InnerHeaderSize+LatestGameHeaderSize+len(body) {
		t.Fatalf("unexpected frame length: %d", len(frame))
	}
	if binary.LittleEndian.Uint16(frame[2:4]) != uint16(len(frame)) {
		t.Fatalf("total length not written")
	}
	if got := frame[12]; got != NormalInnerKind {
		t.Fatalf("inner kind = %#x, want kind 2", got)
	}
	if got := binary.LittleEndian.Uint32(frame[13:17]); got != options.Sequence {
		t.Fatalf("sequence = %d, want %d", got, options.Sequence)
	}
	if got := binary.LittleEndian.Uint16(frame[17:19]); got != uint16(LatestGameHeaderSize+len(body)) {
		t.Fatalf("business length = %d", got)
	}
	if frame[19] != options.Route || frame[20] != options.Flag {
		t.Fatalf("route/flag = %d/%d", frame[19], frame[20])
	}
	if frame[21] != 1 {
		t.Fatalf("business packet should start at inner+9, got first byte %#x", frame[21])
	}

	records, err := ParseLatestGameTCPRecords(frame)
	if err != nil {
		t.Fatalf("parse latest game tcp: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("record count mismatch: %d", len(records))
	}
	record := records[0]
	if record.TransportHeader.OuterKind != options.OuterKind ||
		record.TransportHeader.OuterValue4 != options.OuterValue4 ||
		record.TransportHeader.OuterValue8 != options.OuterValue8 ||
		record.TransportHeader.Sequence != options.Sequence ||
		record.TransportHeader.Route != options.Route ||
		record.TransportHeader.Flag != options.Flag {
		t.Fatalf("unexpected transport header: %+v", record.TransportHeader)
	}
	if record.GameHeader.Cmd != 1 || record.GameHeader.Type != 3 {
		t.Fatalf("unexpected game header: %+v", record.GameHeader)
	}
	if !bytes.Equal(record.Body, body) {
		t.Fatalf("body mismatch: got %x", record.Body)
	}
}

func TestBuildFixed15GameServerPacket(t *testing.T) {
	body := []byte{0x02, 0x03, 0x04}
	packet, err := BuildFixed15GameServerPacket(0, 2, body, 1)
	if err != nil {
		t.Fatalf("build fixed15 packet: %v", err)
	}
	if len(packet) != 15+len(body) || packet[0] != 0 || binary.LittleEndian.Uint16(packet[1:3]) != 2 || binary.LittleEndian.Uint32(packet[3:7]) != uint32(len(packet)) || packet[7] != 1 {
		t.Fatalf("fixed15 header = %x", packet[:15])
	}
	if !bytes.Equal(packet[8:15], make([]byte, 7)) || !bytes.Equal(packet[15:], body) {
		t.Fatalf("fixed15 packet = %x", packet)
	}
}

func TestLatestGameUDPRoundTrip(t *testing.T) {
	body := []byte{9, 8, 7}
	options := TransportOptions{Sequence: 11, Route: 2, Flag: 1}
	packet, err := BuildLatestGameUDP(1, 3, body, options)
	if err != nil {
		t.Fatalf("build latest game udp: %v", err)
	}

	records, err := ParseLatestGameUDPRecords(packet)
	if err != nil {
		t.Fatalf("parse latest game udp: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("record count mismatch: %d", len(records))
	}
	if records[0].TransportHeader.Sequence != options.Sequence ||
		records[0].TransportHeader.Route != options.Route ||
		records[0].TransportHeader.Flag != options.Flag ||
		!bytes.Equal(records[0].Body, body) {
		t.Fatalf("unexpected udp record: %+v", records[0])
	}
}

func TestSplitLatestGameTCPFrames(t *testing.T) {
	frame, err := BuildLatestGameTCP(1, 3, []byte{1, 2, 3}, TransportOptions{})
	if err != nil {
		t.Fatalf("build latest game tcp: %v", err)
	}

	stream := append(cloneBytes(frame), frame[:5]...)
	frames, remaining, skipped := SplitLatestGameTCPFrames(stream)
	if skipped != 0 {
		t.Fatalf("unexpected skipped count: %d", skipped)
	}
	if len(frames) != 1 || !bytes.Equal(frames[0], frame) {
		t.Fatalf("frame split mismatch")
	}
	if !bytes.Equal(remaining, frame[:5]) {
		t.Fatalf("remaining mismatch: got %x", remaining)
	}

	frames, remaining, skipped = SplitLatestGameTCPFrames([]byte{170, 187, 1, 0})
	if len(frames) != 0 || skipped != 1 || !bytes.Equal(remaining, []byte{187, 1, 0}) {
		t.Fatalf("invalid prefix resync mismatch frames=%d skipped=%d remaining=%x", len(frames), skipped, remaining)
	}
}
