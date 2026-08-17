package dnfbridge

import (
	"encoding/binary"
	"testing"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func TestCurrentSceneObjectRawStateCarriesProvenRealFields(t *testing.T) {
	character := dnfrepo.CharacterRecord{
		Name:  "hero",
		Job:   "15",
		Level: 86,
		Stats: map[string]int64{"grow_type": 1},
	}
	raw, ok := buildCurrentSceneObjectRawState(character, true, "hero")
	if !ok || len(raw) != 0x47 {
		t.Fatalf("raw state ok=%t len=%d", ok, len(raw))
	}
	if raw[0x07] != 1 || raw[0x0A] != 86 || raw[0x2C] != 1 ||
		binary.LittleEndian.Uint32(raw[0x43:0x47]) != 0xFFFFFFFF {
		t.Fatalf("raw proven fields = %x", raw)
	}
	for offset, value := range raw {
		switch offset {
		case 0x07, 0x0A, 0x2C, 0x43, 0x44, 0x45, 0x46:
			continue
		}
		if value != 0 {
			t.Fatalf("raw offset %#x carries unproven value %#x: %x", offset, value, raw)
		}
	}

	guest, ok := buildCurrentSceneObjectRawState(dnfrepo.CharacterRecord{}, false, "hero")
	if !ok || guest[0x07] != 1 || guest[0x0A] != 0 || guest[0x2C] != 1 ||
		binary.LittleEndian.Uint32(guest[0x43:0x47]) != 0xFFFFFFFF {
		t.Fatalf("guest raw proven fields = %x", guest)
	}
}
