package dnfbridge

import (
	"encoding/binary"
	"testing"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func TestDecodeCurrentAddEquipmentEffectRequestPinsCapturedTwentyOneByteLayout(t *testing.T) {
	body := []byte{
		0x01, 0x48, 0x18, 0x53, 0x39, 0x5c, 0xf8, 0x19, 0x00,
		0x51, 0x00, 0x00, 0x00,
		0x0b, 0x00, 0x00, 0x00,
		0x51, 0x00, 0x00, 0x00,
	}
	request, err := decodeCurrentAddEquipmentEffectRequest(body)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if request.RequestedSourceSlot != 81 || request.TargetListType != dnfrepo.MainInventoryListType || request.TargetSlot != 11 {
		t.Fatalf("request = %+v", request)
	}
	if len(request.RawBody) != len(body) || &request.RawBody[0] == &body[0] {
		t.Fatalf("raw body was not retained as an independent exact copy")
	}
}

func TestCurrentEquipmentEffectRuneStateDoesNotOverwriteWireExpiration(t *testing.T) {
	const expiration = uint32(0x12345678)
	const runeID = uint16(1)
	raw := make([]byte, currentItemListEntryWireSize)
	binary.LittleEndian.PutUint32(raw[currentItemListExpireTimeOffset:currentItemListExpireTimeOffset+4], expiration)

	stack := dnfrepo.ItemStack{
		ItemID:   101030741,
		Count:    1,
		RawEntry: append([]byte(nil), raw...),
		Extra: map[string]string{
			"item_kind":           "equipment",
			"equipment_effect_id": "1",
		},
	}
	entry := currentItemListEntryFromStack(dnfrepo.MainInventoryListType, 11, stack)
	if got := binary.LittleEndian.Uint32(entry.data[currentItemListExpireTimeOffset : currentItemListExpireTimeOffset+4]); got != expiration {
		t.Fatalf("stack expiration field = 0x%08x want 0x%08x", got, expiration)
	}
	if got := binary.LittleEndian.Uint16(entry.data[currentEquipmentEffectRuneWireOffset : currentEquipmentEffectRuneWireOffset+2]); got != runeID {
		t.Fatalf("stack rune field = %d want %d", got, runeID)
	}

	equipped, ok := currentItemListEntryFromEquipment(dnfrepo.EquipmentEntry{
		SlotIndex: 12,
		ItemID:    101030741,
		RawEntry:  append([]byte(nil), raw...),
		Extra: map[string]string{
			"item_kind":           "equipment",
			"equipment_effect_id": "1",
		},
	})
	if !ok {
		t.Fatal("equipped weapon projection was rejected")
	}
	if got := binary.LittleEndian.Uint32(equipped.data[currentItemListExpireTimeOffset : currentItemListExpireTimeOffset+4]); got != expiration {
		t.Fatalf("equipped expiration field = 0x%08x want 0x%08x", got, expiration)
	}
	if got := binary.LittleEndian.Uint16(equipped.data[currentEquipmentEffectRuneWireOffset : currentEquipmentEffectRuneWireOffset+2]); got != runeID {
		t.Fatalf("equipped rune field = %d want %d", got, runeID)
	}
}
