package pvf

import (
	"encoding/binary"
	"unicode/utf16"
)

func decryptGuard(buf []byte) {
	if len(buf) < 28 {
		return
	}
	for idx := 24; idx < 28; idx++ {
		buf[idx] ^= 0x55
	}
}

func decrypt(key string, buf []byte) {
	decryptCore(key, buf, 0x269EC3)
}

func decrypt2(key string, buf []byte) {
	decryptCore(key, buf, 0x269EC9)
}

func decryptProtected(key string, buf []byte) {
	decryptProtectedCore(key, buf, 0x269EC3)
}

func decryptPString(key string, buf []byte) {
	decryptProtectedCore(key, buf, 0x269EC9)
}

func decryptCore(key string, buf []byte, magic uint32) {
	if key == "" || len(buf) == 0 {
		return
	}
	k := []byte(key)
	if len(k) < 4 {
		return
	}

	size := len(buf)
	tail := size
	seed := uint32(0x76826701)*uint32(k[0]) +
		0x1C1*(uint32(k[3])+0x1C1*(uint32(k[2])+0x1C1*uint32(k[1])))

	if size >= 4 {
		quadCount := size >> 2
		tail = size - (quadCount << 2)
		for idx := 0; idx < quadCount; idx++ {
			t1 := 0x343FD*seed + magic
			seed = 0x343FD*t1 + magic
			xorKey := ((seed >> 16) & 0xFFFF) + (t1 & 0xFFFF0000)
			off := idx << 2
			value := binary.LittleEndian.Uint32(buf[off:off+4]) ^ xorKey
			binary.LittleEndian.PutUint32(buf[off:off+4], value)
		}
	}

	if tail > 0 {
		t1 := 0x343FD*seed + magic
		t2 := 0x343FD*t1 + magic
		finalKey := (t1 & 0xFFFF0000) + ((t2 >> 16) & 0xFFFF)
		var keyBytes [4]byte
		binary.LittleEndian.PutUint32(keyBytes[:], finalKey)
		start := size - tail
		for idx := 0; idx < tail; idx++ {
			buf[start+idx] ^= keyBytes[idx]
		}
	}
}

// decryptProtectedCore 复刻新版 DNF PVF 的 UTF-16 seed 密钥流。
// 它只用于 protected_nkpi 归档的 header、HASH、GRPI、字符串池和 body chunk 解密。
func decryptProtectedCore(key string, buf []byte, magic uint32) {
	if key == "" || len(buf) == 0 {
		return
	}
	words := utf16.Encode([]rune(key))
	if len(words) < 4 {
		return
	}

	size := len(buf)
	tail := size
	seed := uint32(0x339E9711)*uint32(words[0]) +
		0x393*(uint32(words[3])+0x393*(uint32(words[2])+0x393*uint32(words[1])))

	if size >= 4 {
		quadCount := size >> 2
		tail = size - (quadCount << 2)
		for idx := 0; idx < quadCount; idx++ {
			t1 := 0x343FD*seed + magic
			seed = 0x343FD*t1 + magic
			xorKey := (t1 & 0xFFFF0000) + (seed >> 16)
			off := idx << 2
			value := binary.LittleEndian.Uint32(buf[off:off+4]) ^ xorKey
			binary.LittleEndian.PutUint32(buf[off:off+4], value)
		}
	}

	if tail > 0 {
		t1 := 0x343FD*seed + magic
		t2 := 0x343FD*t1 + magic
		finalKey := (t1 & 0xFFFF0000) + (t2 >> 16)
		var keyBytes [4]byte
		binary.LittleEndian.PutUint32(keyBytes[:], finalKey)
		start := size - tail
		for idx := 0; idx < tail; idx++ {
			buf[start+idx] ^= keyBytes[idx]
		}
	}
}
