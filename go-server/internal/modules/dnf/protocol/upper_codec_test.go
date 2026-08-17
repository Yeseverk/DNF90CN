// 本文件验证 DNF latest raw upper 服务端包体 codec 的固定样本。
// 样本来自 2026-06-30 MCP/hook 对 opcode 1 CAST5 key 的动态确认。
package protocol

import (
	"bytes"
	"testing"

	"golang.org/x/crypto/cast5"
)

func TestEncodeLatestUpperServerBodyMsg1CAST5(t *testing.T) {
	plain := []byte{1, 1, 1, 0, 3, 0, 0, 0, 0}

	encoded, ok, err := EncodeLatestUpperServerBody(1, plain)
	if err != nil {
		t.Fatalf("encode msg1: %v", err)
	}
	if !ok {
		t.Fatal("msg1 should be encoded")
	}
	if len(encoded) != 16 {
		t.Fatalf("encoded len = %d, want 16", len(encoded))
	}
	wantPrefix := []byte{0xb2, 0xc4, 0x32, 0x83, 0x0f, 0xa9, 0x45, 0x57}
	if !bytes.Equal(encoded[:8], wantPrefix) {
		t.Fatalf("encoded first block = %x, want %x", encoded[:8], wantPrefix)
	}

	decoded := decryptLatestUpperCAST5ForTest(t, encoded)
	if !bytes.Equal(decoded[:len(plain)], plain) {
		t.Fatalf("decoded body = %x, want prefix %x", decoded[:len(plain)], plain)
	}
	for i, b := range decoded[len(plain):] {
		if b != 0 {
			t.Fatalf("padding byte %d = %x, want 00", i, b)
		}
	}
}

func TestEncodeLatestUpperServerBodySkipsUntracedOpcode(t *testing.T) {
	plain := []byte{1, 2, 3}

	encoded, ok, err := EncodeLatestUpperServerBody(4, plain)
	if err != nil {
		t.Fatalf("encode msg4: %v", err)
	}
	if ok {
		t.Fatal("msg4 should stay unencoded until its codec key is traced")
	}
	if !bytes.Equal(encoded, plain) {
		t.Fatalf("encoded body = %x, want %x", encoded, plain)
	}
	encoded[0] = 9
	if plain[0] == 9 {
		t.Fatal("skip path must return a copy")
	}
}

func decryptLatestUpperCAST5ForTest(t *testing.T, data []byte) []byte {
	t.Helper()
	block, err := cast5.NewCipher(latestUpperOpcode1Key)
	if err != nil {
		t.Fatalf("cast5 cipher: %v", err)
	}
	if len(data)%block.BlockSize() != 0 {
		t.Fatalf("ciphertext len = %d, block = %d", len(data), block.BlockSize())
	}
	out := make([]byte, len(data))
	for offset := 0; offset < len(data); offset += block.BlockSize() {
		block.Decrypt(out[offset:offset+block.BlockSize()], data[offset:offset+block.BlockSize()])
	}
	return out
}
