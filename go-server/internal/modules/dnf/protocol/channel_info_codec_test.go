package protocol

import (
	"bytes"
	"math/bits"
	"testing"
)

func TestEncodeLatestUpperClass0Opcode1BodyMatchesCurrentEXEInverse(t *testing.T) {
	plain := []byte{0x37, 0x00, 0x00, 0x00, 0x08, 0xef, 0x0b, 0x10}

	encoded := EncodeLatestUpperClass0Opcode1Body(plain)
	want := []byte{0x0a, 0xd6, 0xd6, 0xd6, 0xf6, 0x69, 0xfa, 0x96}
	if !bytes.Equal(encoded, want) {
		t.Fatalf("encoded body = %x, want %x", encoded, want)
	}
	if len(encoded) != len(plain) {
		t.Fatalf("encoded len = %d, want %d", len(encoded), len(plain))
	}

	decoded := make([]byte, len(encoded))
	for i, value := range encoded {
		decoded[i] = bits.RotateLeft8(value, 6) ^ 0xb5
	}
	if !bytes.Equal(decoded, plain) {
		t.Fatalf("current EXE decode result = %x, want %x", decoded, plain)
	}
}

func TestEncodeLatestUpperClass0Opcode1BodyReturnsIndependentCopy(t *testing.T) {
	plain := []byte{1, 2, 3}
	encoded := EncodeLatestUpperClass0Opcode1Body(plain)
	encoded[0] = 0
	if plain[0] != 1 {
		t.Fatalf("plain body was mutated: %x", plain)
	}
}
