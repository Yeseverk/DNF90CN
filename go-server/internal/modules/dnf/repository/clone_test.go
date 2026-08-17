// 本文件由 transaction_test.go 按后端拆分而来。
package repository

import (
	"testing"
)

func TestCloneInventoryClonesTypedRawEntry(t *testing.T) {
	raw := []byte{1, 2, 3}
	clone := CloneInventory(InventoryRecord{Slots: map[string]ItemStack{"0:1": {RawEntry: raw}}})
	raw[0] = 9
	if got := clone.Slots["0:1"].RawEntry[0]; got != 1 {
		t.Fatalf("cloned raw entry changed to %d", got)
	}
}
