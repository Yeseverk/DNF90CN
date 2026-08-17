package repository

import (
	"fmt"
	"strings"
	"time"

	"longheng.io/server/internal/platform/db"
)

const (
	MainInventoryListType     byte  = 0
	CrystalWarehouseFirstSlot int16 = 354
	CrystalWarehouseLastSlot  int16 = 359
	SoulWarehouseFirstSlot    int16 = 360
	SoulWarehouseLastSlot     int16 = 365
)

// AccountInventoryRepository stores the account-owned portion of list type 0.
// The current client still addresses these entries as ordinary list-0 slots;
// only their durable owner changes from character to account.
type AccountInventoryRepository interface {
	db.Store[AccountInventoryRecord]
}

type AccountInventoryRecord struct {
	AccountID string               `json:"account_id"`
	Slots     map[string]ItemStack `json:"slots,omitempty"`
	UpdatedAt time.Time            `json:"updated_at,omitempty"`
}

func CloneAccountInventory(record AccountInventoryRecord) AccountInventoryRecord {
	record.Slots = cloneItemMap(record.Slots)
	return record
}

func AccountInventoryKey(record AccountInventoryRecord) string {
	return strings.TrimSpace(record.AccountID)
}

// IsAccountSharedInventorySlot reports whether a wire list/slot pair belongs
// to the account-scoped crystal or soul warehouse.
func IsAccountSharedInventorySlot(listType byte, slot int16) bool {
	return listType == MainInventoryListType &&
		slot >= CrystalWarehouseFirstSlot && slot <= SoulWarehouseLastSlot
}

func AccountSharedInventorySlotKey(slot int16) string {
	return fmt.Sprintf("%d:%d", MainInventoryListType, slot)
}

// MergeAccountSharedInventory produces the list-0 view for one character.
// Stale character-owned copies of slots 354..365 are removed before the
// account record is overlaid, so callers never expose two competing owners.
func MergeAccountSharedInventory(character InventoryRecord, account AccountInventoryRecord) InventoryRecord {
	merged := CloneInventory(character)
	if merged.Slots == nil {
		merged.Slots = make(map[string]ItemStack)
	}
	for slot := CrystalWarehouseFirstSlot; slot <= SoulWarehouseLastSlot; slot++ {
		delete(merged.Slots, AccountSharedInventorySlotKey(slot))
	}
	for slot := CrystalWarehouseFirstSlot; slot <= SoulWarehouseLastSlot; slot++ {
		key := AccountSharedInventorySlotKey(slot)
		if stack, ok := account.Slots[key]; ok {
			merged.Slots[key] = cloneStackRecord(stack)
		}
	}
	return merged
}

func cloneStackRecord(stack ItemStack) ItemStack {
	stack.RawEntry = append([]byte(nil), stack.RawEntry...)
	stack.Extra = cloneStringMap(stack.Extra)
	return stack
}
