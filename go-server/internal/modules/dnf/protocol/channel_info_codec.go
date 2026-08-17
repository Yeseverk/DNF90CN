package protocol

import "math/bits"

// EncodeLatestUpperClass0Opcode1Body applies the inverse of the current
// NoPack.exe class0/op1 receive transform.
//
// sub_2261E30 copies the wire body and calls sub_2FA1E90(buffer, length, 0).
// That function decodes every byte as ROL8(cipher, 6) XOR 0xB5. Therefore the
// server-side inverse is ROL8(plain XOR 0xB5, 2). The transform preserves the
// exact body length and has no block padding.
func EncodeLatestUpperClass0Opcode1Body(body []byte) []byte {
	encoded := make([]byte, len(body))
	for i, value := range body {
		encoded[i] = bits.RotateLeft8(value^0xb5, 2)
	}
	return encoded
}
