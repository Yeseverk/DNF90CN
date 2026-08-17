package dnfbridge

import (
	"encoding/binary"
	"testing"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func TestCurrentItemListEntryFromEquipmentMapsGuildMedalToSlot32(t *testing.T) {
	entry, ok := currentItemListEntryFromEquipment(dnfrepo.EquipmentEntry{
		SlotIndex: 32,
		ItemID:    100380017,
		RawEntry:  make([]byte, currentItemListEntryWireSize),
	})
	if !ok {
		t.Fatal("guild medal equipment entry was omitted from current item list")
	}
	if got := binary.LittleEndian.Uint16(entry.data[0:2]); got != 32 {
		t.Fatalf("guild medal item-list slot=%d, want 32", got)
	}
	if got := binary.LittleEndian.Uint32(entry.data[2:6]); got != 100380017 {
		t.Fatalf("guild medal item-list id=%d, want 100380017", got)
	}
}

func TestCurrentMode1EquipmentTypeMapsGuildMedalToSlot32(t *testing.T) {
	reader := csharpLegacyUserInfoReader{}
	got, ok := reader.currentMode1EquipmentType(dnfrepo.EquipmentEntry{SlotIndex: 32, ItemID: 100380017})
	if !ok || got != 32 {
		t.Fatalf("guild medal mode1 type=%d ok=%t, want 32/true", got, ok)
	}
}
