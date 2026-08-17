package dnfbridge

import (
	"encoding/binary"
	"testing"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func TestCurrentItemListPersonalCargoStackUsesAuthoritativeDurableCount(t *testing.T) {
	raw := make([]byte, currentItemListEntryWireSize)
	binary.LittleEndian.PutUint32(raw[0x06:0x0A], 20470)
	entry := currentItemListEntryFromStack(2, 0, dnfrepo.ItemStack{
		ItemID:   3330,
		Count:    26320,
		RawEntry: raw,
		Extra: map[string]string{
			"item_kind":       "stackable",
			"amount":          "17970",
			"amount_or_count": "17970",
		},
	})

	if got := binary.LittleEndian.Uint32(entry.data[0x06:0x0A]); got != 26320 {
		t.Fatalf("projected personal-cargo count = %d, want 26320", got)
	}
}
