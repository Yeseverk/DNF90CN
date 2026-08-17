package protocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

func TestDprotoClientEnvelopeRoundTrip(t *testing.T) {
	protected := []byte{0x00, 0xad, 0x00, 0x10, 0x00, 0x21, 0x22}
	wire, err := BuildDprotoClientEnvelope(protected, 55)
	if err != nil {
		t.Fatalf("build dproto envelope: %v", err)
	}
	envelope, err := ParseDprotoClientEnvelope(wire, 1024)
	if err != nil {
		t.Fatalf("parse dproto envelope: %v", err)
	}
	if envelope.Header.MsgID != DprotoClientEnvelopeOpcode || envelope.Header.Seq != 55 {
		t.Fatalf("header=%+v", envelope.Header)
	}
	if !bytes.Equal(envelope.Protected, protected) || !bytes.Equal(envelope.Raw, wire) {
		t.Fatalf("protected=%x raw=%x", envelope.Protected, envelope.Raw)
	}
}

func TestParseDprotoClientEnvelopeRejectsDeclaredLengthMismatch(t *testing.T) {
	wire, err := BuildDprotoClientEnvelope([]byte{1, 2, 3}, 1)
	if err != nil {
		t.Fatal(err)
	}
	binary.LittleEndian.PutUint32(wire[ChannelHeaderSize:ChannelHeaderSize+4], 2)
	sum, err := ChecksumRange(wire, 11, len(wire)-11)
	if err != nil {
		t.Fatal(err)
	}
	binary.LittleEndian.PutUint32(wire[7:11], sum)
	if _, err := ParseDprotoClientEnvelope(wire, 1024); !errors.Is(err, ErrDprotoProtectedSize) {
		t.Fatalf("error=%v, want %v", err, ErrDprotoProtectedSize)
	}
}

func TestParseDprotoClientEnvelopeRejectsCorruptOuterChecksum(t *testing.T) {
	wire, err := BuildDprotoClientEnvelope([]byte{1, 2, 3}, 1)
	if err != nil {
		t.Fatal(err)
	}
	wire[len(wire)-1] ^= 0xff
	if _, err := ParseDprotoClientEnvelope(wire, 1024); !errors.Is(err, ErrChecksumInvalid) {
		t.Fatalf("error=%v, want %v", err, ErrChecksumInvalid)
	}
}
