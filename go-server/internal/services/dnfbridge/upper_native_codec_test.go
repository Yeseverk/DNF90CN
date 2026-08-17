package dnfbridge

import (
	"bytes"
	"crypto/aes"
	"encoding/hex"
	"testing"
)

func TestDecodeCurrentUpperClientBodyNativeRoundTrips(t *testing.T) {
	tests := []struct {
		name   string
		msgID  uint16
		plain  []byte
		encode func([]byte) ([]byte, error)
		codec  string
	}{
		{
			name:  "idx0",
			msgID: 476,
			plain: upperNativeTestBytes(16),
			encode: func(body []byte) ([]byte, error) {
				return encodeUpperNativeIdx0(body)
			},
			codec: "idx0-xtea-be-ecb16",
		},
		{
			name:  "idx1",
			msgID: 1,
			plain: upperNativeTestBytes(8),
			encode: func(body []byte) ([]byte, error) {
				return encodeUpperNativeBlock(body, 8, upperCurrentCodecKey(0, 16), newUpperNativeCAST5Block)
			},
			codec: "idx1-cast5-ecb",
		},
		{
			name:  "idx2",
			msgID: 1262,
			plain: upperNativeTestBytes(32),
			encode: func(body []byte) ([]byte, error) {
				return encodeUpperNativeIdx2(body)
			},
			codec: "idx2-rc6-byte-s-ecb",
		},
		{
			name:  "idx3",
			msgID: 171,
			plain: upperNativeTestBytes(16),
			encode: func(body []byte) ([]byte, error) {
				return encodeUpperNativeBlock(body, 16, upperCurrentCodecKey(12, 32), newUpperNativeTwofishBlock)
			},
			codec: "idx3-twofish-ecb",
		},
		{
			name:  "idx4",
			msgID: 4,
			plain: upperNativeTestBytes(16),
			encode: func(body []byte) ([]byte, error) {
				return encodeUpperNativeBlock(body, 16, upperAESKey4[:], aes.NewCipher)
			},
			codec: "idx4-aes-ecb",
		},
		{
			name:  "idx7",
			msgID: 441,
			plain: upperNativeTestBytes(8),
			encode: func(body []byte) ([]byte, error) {
				return encodeUpperNativeBlock(body, 8, upperCurrentCodecKey(6, 56), newUpperNativeBlowfishBlock)
			},
			codec: "idx7-blowfish-ecb",
		},
		{
			name:  "idx8",
			msgID: 8,
			plain: upperNativeTestBytes(8),
			encode: func(body []byte) ([]byte, error) {
				return encodeUpperNativeIdx8(body)
			},
			codec: "idx8-xtea-le-ecb",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wire, err := tt.encode(tt.plain)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			got, codec, supported, err := decodeCurrentUpperClientBody(tt.msgID, wire)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if !supported {
				t.Fatalf("decode reported unsupported")
			}
			if codec != tt.codec {
				t.Fatalf("codec = %q, want %q", codec, tt.codec)
			}
			if !bytes.Equal(got, tt.plain) {
				t.Fatalf("decoded body = %x, want %x", got, tt.plain)
			}
		})
	}
}

func TestDecodeCurrentUpperClientBodyUsesObservedAESSelectVector(t *testing.T) {
	wire := upperNativeMustHex(t, "a7c12b5ac5f5e2f0ce15e9e5d753a3e1")
	got, codec, supported, err := decodeCurrentUpperClientBody(4, wire)
	if err != nil {
		t.Fatalf("decode observed select: %v", err)
	}
	if !supported {
		t.Fatalf("observed select reported unsupported")
	}
	if codec != "idx4-aes-ecb" {
		t.Fatalf("codec = %q, want idx4-aes-ecb", codec)
	}
	want := []byte{
		0x04, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("decoded observed select = %x, want %x", got, want)
	}
}

func TestDecodeCurrentUpperClientBodyUnsupportedSlotClonesPlaintext(t *testing.T) {
	body := []byte{1, 2, 3, 4}
	got, codec, supported, err := decodeCurrentUpperClientBody(6, body)
	if err != nil {
		t.Fatalf("decode unsupported: %v", err)
	}
	if supported {
		t.Fatalf("unsupported slot reported supported")
	}
	if codec != "idx-unsupported" {
		t.Fatalf("codec = %q, want idx-unsupported", codec)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("clone = %x, want %x", got, body)
	}
	got[0] = 99
	if body[0] == 99 {
		t.Fatalf("unsupported slot returned original body, want clone")
	}
}

func TestNormalizeGameUpperClientBodyCodec(t *testing.T) {
	if got := normalizeGameUpperClientBodyCodec("plain"); got != gameUpperClientBodyCodecPlain {
		t.Fatalf("plain normalized to %q", got)
	}
	if got := normalizeGameUpperClientBodyCodec("log_only"); got != gameUpperClientBodyCodecProbe {
		t.Fatalf("log_only normalized to %q", got)
	}
	if got := normalizeGameUpperClientBodyCodec("server_native"); got != gameUpperClientBodyCodecNative {
		t.Fatalf("server_native normalized to %q", got)
	}
	if got := normalizeGameUpperClientBodyCodec("bad-value"); got != gameUpperClientBodyCodecPlain {
		t.Fatalf("bad-value normalized to %q", got)
	}
}

func upperNativeTestBytes(n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = byte(0x31 + i*17)
	}
	return out
}

func upperNativeMustHex(t *testing.T, s string) []byte {
	t.Helper()
	out, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("decode hex: %v", err)
	}
	return out
}
