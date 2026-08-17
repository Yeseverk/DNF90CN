package dnfbridge

import (
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"math/bits"

	"golang.org/x/crypto/blowfish"
	"golang.org/x/crypto/cast5"
	"golang.org/x/crypto/twofish"
)

var errUpperNativeCodecBody = errors.New("invalid upper native codec body")
var errUpperNativeCodecUnsupported = errors.New("unsupported upper native codec")

var upperCurrentCodecBaseKey = []byte("2D@+(vhxnw8h!xvb")

func decodeCurrentUpperClientBody(msgID uint16, body []byte) ([]byte, string, bool, error) {
	if len(body) == 0 {
		return nil, gameUpperClientBodyCodecPlain, false, nil
	}
	idx := int(msgID % 14)
	switch idx {
	case 0:
		out, err := decodeUpperNativeIdx0(body)
		return out, "idx0-xtea-be-ecb16", true, err
	case 1:
		out, err := decodeUpperNativeBlock(body, 8, upperCurrentCodecKey(0, 16), newUpperNativeCAST5Block)
		return out, "idx1-cast5-ecb", true, err
	case 2:
		out, err := decodeUpperNativeIdx2(body)
		return out, "idx2-rc6-byte-s-ecb", true, err
	case 3:
		out, err := decodeUpperNativeBlock(body, 16, upperCurrentCodecKey(12, 32), newUpperNativeTwofishBlock)
		return out, "idx3-twofish-ecb", true, err
	case 4:
		out, err := decodeUpperKey4(body)
		return out, "idx4-aes-ecb", true, err
	case 5:
		out, err := decodeUpperKey5(body)
		return out, "idx5-skipjack-ecb", true, err
	case 7:
		out, err := decodeUpperNativeBlock(body, 8, upperCurrentCodecKey(6, 56), newUpperNativeBlowfishBlock)
		return out, "idx7-blowfish-ecb", true, err
	case 8:
		out, err := decodeUpperNativeIdx8(body)
		return out, "idx8-xtea-le-ecb", true, err
	default:
		return cloneUpperNativeBody(body), "idx-unsupported", false, nil
	}
}

func newUpperNativeCAST5Block(key []byte) (cipher.Block, error) {
	return cast5.NewCipher(key)
}

func newUpperNativeTwofishBlock(key []byte) (cipher.Block, error) {
	return twofish.NewCipher(key)
}

func newUpperNativeBlowfishBlock(key []byte) (cipher.Block, error) {
	return blowfish.NewCipher(key)
}

func cloneUpperNativeBody(body []byte) []byte {
	if len(body) == 0 {
		return nil
	}
	return append([]byte(nil), body...)
}

func upperCurrentCodecKey(offset, length int) []byte {
	key := make([]byte, length)
	for i := range key {
		key[i] = upperCurrentCodecBaseKey[(offset+i)%len(upperCurrentCodecBaseKey)]
	}
	return key
}

func decodeUpperNativeBlock(body []byte, blockSize int, key []byte, newCipher func([]byte) (cipher.Block, error)) ([]byte, error) {
	if len(body) == 0 || len(body)%blockSize != 0 {
		return nil, errUpperNativeCodecBody
	}
	block, err := newCipher(key)
	if err != nil {
		return nil, err
	}
	if block.BlockSize() != blockSize {
		return nil, errUpperNativeCodecBody
	}
	out := make([]byte, len(body))
	for offset := 0; offset < len(body); offset += blockSize {
		block.Decrypt(out[offset:offset+blockSize], body[offset:offset+blockSize])
	}
	return out, nil
}

func encodeUpperNativeBlock(body []byte, blockSize int, key []byte, newCipher func([]byte) (cipher.Block, error)) ([]byte, error) {
	if len(body) == 0 || len(body)%blockSize != 0 {
		return nil, errUpperNativeCodecBody
	}
	block, err := newCipher(key)
	if err != nil {
		return nil, err
	}
	if block.BlockSize() != blockSize {
		return nil, errUpperNativeCodecBody
	}
	out := make([]byte, len(body))
	for offset := 0; offset < len(body); offset += blockSize {
		block.Encrypt(out[offset:offset+blockSize], body[offset:offset+blockSize])
	}
	return out, nil
}

func decodeUpperNativeIdx0(body []byte) ([]byte, error) {
	if len(body) == 0 || len(body)%16 != 0 {
		return nil, errUpperNativeCodecBody
	}
	key := upperNativeIdx0Subkeys(upperCurrentCodecKey(0, 16))
	out := make([]byte, len(body))
	for offset := 0; offset < len(body); offset += 16 {
		upperNativeIdx0Decrypt8(out[offset:offset+8], body[offset:offset+8], key)
		upperNativeIdx0Decrypt8(out[offset+8:offset+16], body[offset+8:offset+16], key)
	}
	return out, nil
}

func encodeUpperNativeIdx0(body []byte) ([]byte, error) {
	if len(body) == 0 || len(body)%16 != 0 {
		return nil, errUpperNativeCodecBody
	}
	key := upperNativeIdx0Subkeys(upperCurrentCodecKey(0, 16))
	out := make([]byte, len(body))
	for offset := 0; offset < len(body); offset += 16 {
		upperNativeIdx0Encrypt8(out[offset:offset+8], body[offset:offset+8], key)
		upperNativeIdx0Encrypt8(out[offset+8:offset+16], body[offset+8:offset+16], key)
	}
	return out, nil
}

func upperNativeIdx0Subkeys(key []byte) [4]uint32 {
	return [4]uint32{
		binary.BigEndian.Uint32(key[0:4]),
		binary.BigEndian.Uint32(key[4:8]),
		binary.BigEndian.Uint32(key[8:12]),
		binary.BigEndian.Uint32(key[12:16]),
	}
}

func upperNativeIdx0Round(x uint32) uint32 {
	return x + ((x << 4) ^ (x >> 5))
}

func upperNativeIdx0Encrypt8(dst, src []byte, key [4]uint32) {
	left := binary.BigEndian.Uint32(src[0:4])
	right := binary.BigEndian.Uint32(src[4:8])
	var sum uint32
	for i := 0; i < 32; i++ {
		left += (sum + key[sum&3]) ^ upperNativeIdx0Round(right)
		sum -= 1640531527
		right += (sum + key[(sum>>11)&3]) ^ upperNativeIdx0Round(left)
	}
	binary.BigEndian.PutUint32(dst[0:4], left)
	binary.BigEndian.PutUint32(dst[4:8], right)
}

func upperNativeIdx0Decrypt8(dst, src []byte, key [4]uint32) {
	left := binary.BigEndian.Uint32(src[0:4])
	right := binary.BigEndian.Uint32(src[4:8])
	sum := uint32(0xC6EF3720)
	for i := 0; i < 32; i++ {
		right -= (sum + key[(sum>>11)&3]) ^ upperNativeIdx0Round(left)
		sum += 1640531527
		left -= (sum + key[sum&3]) ^ upperNativeIdx0Round(right)
	}
	binary.BigEndian.PutUint32(dst[0:4], left)
	binary.BigEndian.PutUint32(dst[4:8], right)
}

func decodeUpperNativeIdx2(body []byte) ([]byte, error) {
	if len(body) == 0 || len(body)%16 != 0 {
		return nil, errUpperNativeCodecBody
	}
	subkeys := upperNativeIdx2Subkeys(upperCurrentCodecKey(0, 32))
	out := make([]byte, len(body))
	for offset := 0; offset < len(body); offset += 16 {
		upperNativeIdx2DecryptBlock(out[offset:offset+16], body[offset:offset+16], subkeys)
	}
	return out, nil
}

func encodeUpperNativeIdx2(body []byte) ([]byte, error) {
	if len(body) == 0 || len(body)%16 != 0 {
		return nil, errUpperNativeCodecBody
	}
	subkeys := upperNativeIdx2Subkeys(upperCurrentCodecKey(0, 32))
	out := make([]byte, len(body))
	for offset := 0; offset < len(body); offset += 16 {
		upperNativeIdx2EncryptBlock(out[offset:offset+16], body[offset:offset+16], subkeys)
	}
	return out, nil
}

func upperNativeIdx2Subkeys(key []byte) [44]byte {
	const wordCount = 8
	var l [wordCount]uint32
	for i := len(key) - 1; i >= 0; i-- {
		slot := i >> 2
		l[slot] = (l[slot] << 8) + uint32(key[i])
	}
	var s [44]byte
	s[0] = 99
	for i := 1; i < len(s); i++ {
		s[i] = s[i-1] - 71
	}
	var a, b uint32
	i, j := 0, 0
	for n := 0; n < 3*len(s); n++ {
		a = uint32(byte(bits.RotateLeft32(a+b+uint32(s[i]), 3)))
		s[i] = byte(a)
		b = bits.RotateLeft32(a+b+l[j], int((a+b)&31))
		l[j] = b
		i = (i + 1) % len(s)
		j = (j + 1) % wordCount
	}
	return s
}

func upperNativeIdx2EncryptBlock(dst, src []byte, s [44]byte) {
	a := binary.LittleEndian.Uint32(src[0:4])
	b := binary.LittleEndian.Uint32(src[4:8]) + uint32(s[0])
	c := binary.LittleEndian.Uint32(src[8:12])
	d := binary.LittleEndian.Uint32(src[12:16]) + uint32(s[1])
	for round := 1; round <= 20; round++ {
		t := bits.RotateLeft32(b*(2*b+1), 5)
		u := bits.RotateLeft32(d*(2*d+1), 5)
		nextA := bits.RotateLeft32(a^t, int(u&31)) + uint32(s[2*round])
		nextC := bits.RotateLeft32(c^u, int(t&31)) + uint32(s[2*round+1])
		a, b, c, d = b, nextC, d, nextA
	}
	a += uint32(s[42])
	c += uint32(s[43])
	binary.LittleEndian.PutUint32(dst[0:4], a)
	binary.LittleEndian.PutUint32(dst[4:8], b)
	binary.LittleEndian.PutUint32(dst[8:12], c)
	binary.LittleEndian.PutUint32(dst[12:16], d)
}

func upperNativeIdx2DecryptBlock(dst, src []byte, s [44]byte) {
	a := binary.LittleEndian.Uint32(src[0:4]) - uint32(s[42])
	b := binary.LittleEndian.Uint32(src[4:8])
	c := binary.LittleEndian.Uint32(src[8:12]) - uint32(s[43])
	d := binary.LittleEndian.Uint32(src[12:16])
	for round := 20; round >= 1; round-- {
		a, b, c, d = d, a, b, c
		u := bits.RotateLeft32(d*(2*d+1), 5)
		t := bits.RotateLeft32(b*(2*b+1), 5)
		c = bits.RotateLeft32(c-uint32(s[2*round+1]), -int(t&31)) ^ u
		a = bits.RotateLeft32(a-uint32(s[2*round]), -int(u&31)) ^ t
	}
	b -= uint32(s[0])
	d -= uint32(s[1])
	binary.LittleEndian.PutUint32(dst[0:4], a)
	binary.LittleEndian.PutUint32(dst[4:8], b)
	binary.LittleEndian.PutUint32(dst[8:12], c)
	binary.LittleEndian.PutUint32(dst[12:16], d)
}

func decodeUpperNativeIdx8(body []byte) ([]byte, error) {
	if len(body) == 0 || len(body)%8 != 0 {
		return nil, errUpperNativeCodecBody
	}
	subkeys := upperNativeIdx8Subkeys(upperCurrentCodecKey(14, 16))
	out := make([]byte, len(body))
	for offset := 0; offset < len(body); offset += 8 {
		upperNativeIdx8Decrypt8(out[offset:offset+8], body[offset:offset+8], subkeys)
	}
	return out, nil
}

func encodeUpperNativeIdx8(body []byte) ([]byte, error) {
	if len(body) == 0 || len(body)%8 != 0 {
		return nil, errUpperNativeCodecBody
	}
	subkeys := upperNativeIdx8Subkeys(upperCurrentCodecKey(14, 16))
	out := make([]byte, len(body))
	for offset := 0; offset < len(body); offset += 8 {
		upperNativeIdx8Encrypt8(out[offset:offset+8], body[offset:offset+8], subkeys)
	}
	return out, nil
}

func upperNativeIdx8Subkeys(key []byte) [64]uint32 {
	words := [4]uint32{
		binary.LittleEndian.Uint32(key[0:4]),
		binary.LittleEndian.Uint32(key[4:8]),
		binary.LittleEndian.Uint32(key[8:12]),
		binary.LittleEndian.Uint32(key[12:16]),
	}
	var subkeys [64]uint32
	var sum uint32
	for i := 0; i < 32; i++ {
		subkeys[i] = sum + words[sum&3]
		sum -= 1640531527
		subkeys[i+32] = sum + words[(sum>>11)&3]
	}
	return subkeys
}

func upperNativeIdx8Round(x uint32) uint32 {
	return x + ((x << 4) ^ (x >> 5))
}

func upperNativeIdx8Encrypt8(dst, src []byte, subkeys [64]uint32) {
	left := binary.LittleEndian.Uint32(src[0:4])
	right := binary.LittleEndian.Uint32(src[4:8])
	for group := 0; group < 8; group++ {
		base := group * 4
		v8 := (subkeys[base] ^ upperNativeIdx8Round(right)) + left
		v9 := (subkeys[base+32] ^ upperNativeIdx8Round(v8)) + right
		v10 := (subkeys[base+1] ^ upperNativeIdx8Round(v9)) + v8
		v11 := (subkeys[base+33] ^ upperNativeIdx8Round(v10)) + v9
		v12 := (subkeys[base+2] ^ upperNativeIdx8Round(v11)) + v10
		v13 := (subkeys[base+34] ^ upperNativeIdx8Round(v12)) + v11
		left = (subkeys[base+3] ^ upperNativeIdx8Round(v13)) + v12
		right = (subkeys[base+35] ^ upperNativeIdx8Round(left)) + v13
	}
	binary.LittleEndian.PutUint32(dst[0:4], left)
	binary.LittleEndian.PutUint32(dst[4:8], right)
}

func upperNativeIdx8Decrypt8(dst, src []byte, subkeys [64]uint32) {
	left := binary.LittleEndian.Uint32(src[0:4])
	right := binary.LittleEndian.Uint32(src[4:8])
	for group := 7; group >= 0; group-- {
		base := group * 4
		v8 := right - (subkeys[base+35] ^ upperNativeIdx8Round(left))
		v9 := left - (subkeys[base+3] ^ upperNativeIdx8Round(v8))
		v10 := v8 - (subkeys[base+34] ^ upperNativeIdx8Round(v9))
		v11 := v9 - (subkeys[base+2] ^ upperNativeIdx8Round(v10))
		v12 := v10 - (subkeys[base+33] ^ upperNativeIdx8Round(v11))
		v13 := v11 - (subkeys[base+1] ^ upperNativeIdx8Round(v12))
		right = v12 - (subkeys[base+32] ^ upperNativeIdx8Round(v13))
		left = v13 - (subkeys[base] ^ upperNativeIdx8Round(right))
	}
	binary.LittleEndian.PutUint32(dst[0:4], left)
	binary.LittleEndian.PutUint32(dst[4:8], right)
}
