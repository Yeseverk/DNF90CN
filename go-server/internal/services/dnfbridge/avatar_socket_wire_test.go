package dnfbridge

import (
	"encoding/binary"
	"encoding/hex"
	"testing"
)

func TestCurrentAvatarUniversalSocketUsesCurrentEXESpecialWireType(t *testing.T) {
	var data [currentAvatarSocketBytes]byte
	currentSetAvatarSocketTypes(&data, []byte{0xEF, 0x01})

	if got := binary.LittleEndian.Uint16(data[0:2]); got != 0xFFEF {
		t.Fatalf("universal socket wire type=%#04x want=0xFFEF data=%x", got, data)
	}
	if got := binary.LittleEndian.Uint16(data[6:8]); got != 0x0001 {
		t.Fatalf("ordinary socket wire type=%#04x want=0x0001 data=%x", got, data)
	}
	if got := currentAvatarSocketType(data, 0); got != 0xEF {
		t.Fatalf("universal socket family=%#02x want=0xEF", got)
	}
}

func TestCurrentAvatarSocketProjectionRepairsLegacyUniversalSocketType(t *testing.T) {
	legacy := make([]byte, currentAvatarSocketBytes)
	binary.LittleEndian.PutUint16(legacy[0:2], 0x00EF)
	binary.LittleEndian.PutUint16(legacy[6:8], 0x00EF)

	projected := currentItemListAvatarSocketData(map[string]string{
		"avatar_socket_data": hex.EncodeToString(legacy),
	})
	if len(projected) != currentAvatarSocketBytes {
		t.Fatalf("projected socket bytes=%d want=%d", len(projected), currentAvatarSocketBytes)
	}
	for _, offset := range []int{0, 6} {
		if got := binary.LittleEndian.Uint16(projected[offset : offset+2]); got != 0xFFEF {
			t.Fatalf("projected socket at offset %d=%#04x want=0xFFEF data=%x", offset, got, projected)
		}
	}

	if got := currentItemListAvatarSocketData(nil); got != nil {
		t.Fatalf("avatar without socket metadata projected a blob: %x", got)
	}
}
