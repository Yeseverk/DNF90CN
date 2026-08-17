package equip

import (
	"encoding/binary"
	"testing"
)

func TestPetSerialFromEquippedRawUsesAuthoritativeOffset24(t *testing.T) {
	raw := make([]byte, 28)
	binary.LittleEndian.PutUint32(raw[5:9], 111)
	binary.LittleEndian.PutUint32(raw[24:28], 222)
	if got := petSerialFromEquippedRaw(raw); got != 222 {
		t.Fatalf("serial=%d want=222", got)
	}

	ordinarySlot24 := make([]byte, 9)
	binary.LittleEndian.PutUint32(ordinarySlot24[5:9], 333)
	if got := petSerialFromEquippedRaw(ordinarySlot24); got != 0 {
		t.Fatalf("ordinary slot24 +5 instance was accepted as creature serial: %d", got)
	}
}

func TestBuildPetCreatureEquipEntryRepeatsSerialAtCurrentCreatureOffset(t *testing.T) {
	raw := buildPetCreatureEquipEntry(24, 63000, 37)
	if len(raw) < 28 || raw[0] != 24 ||
		binary.LittleEndian.Uint32(raw[1:5]) != 63000 ||
		binary.LittleEndian.Uint32(raw[5:9]) != 37 ||
		binary.LittleEndian.Uint32(raw[24:28]) != 37 {
		t.Fatalf("pet raw layout=%x", raw)
	}
}
