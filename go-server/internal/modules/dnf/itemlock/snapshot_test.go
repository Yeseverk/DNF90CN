package itemlock

import (
	"bytes"
	"testing"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func TestBuildLockListSnapshotUsesPersistedRowsInContainerOrder(t *testing.T) {
	record := dnfrepo.InventoryRecord{
		Slots: map[string]dnfrepo.ItemStack{
			"0:8": {ItemID: 1001, Count: 1, Extra: map[string]string{"equipment_lock_state": "1"}},
			"1:3": {ItemID: 1002, Count: 1, Extra: map[string]string{"equipment_lock_state": "active"}},
			"7:9": {
				ItemID: 1003,
				Count:  1,
				Extra: map[string]string{
					"equipment_lock_state":             "2",
					"equipment_lock_remaining_seconds": "540",
				},
			},
			"0:12": {ItemID: 1004, Count: 1},
		},
		Warehouse: map[string]dnfrepo.ItemStack{
			"2:5": {ItemID: 2001, Count: 1, Extra: map[string]string{"equipment_lock_state": "locked"}},
		},
	}

	got := BuildLockListSnapshot(record)
	want := []byte{
		4, 0,
		0, 8, 0, itemLockStateActive,
		1, 3, 0, itemLockStateActive,
		2, 5, 0, itemLockStateActive,
		7, 9, 0, itemLockStatePending, 0x1c, 0x02, 0, 0,
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("snapshot=%x want=%x", got, want)
	}
}

func TestBuildLockListSnapshotWritesExplicitEmptyList(t *testing.T) {
	got := BuildLockListSnapshot(dnfrepo.InventoryRecord{
		Slots: map[string]dnfrepo.ItemStack{
			"0:108": {ItemID: 490701734, Count: 36},
			"0:112": {ItemID: 490007240, Count: 1},
		},
	})
	if want := []byte{0, 0}; !bytes.Equal(got, want) {
		t.Fatalf("snapshot=%x want=%x", got, want)
	}
}
