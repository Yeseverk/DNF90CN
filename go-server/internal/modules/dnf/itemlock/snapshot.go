package itemlock

import (
	"sort"
	"strconv"
	"strings"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

type lockSnapshotEntry struct {
	listType         byte
	slotIndex        int16
	state            byte
	remainingSeconds int32
}

// BuildLockListSnapshot serializes the current selected-character lock state
// in the current client's op251 list format. An empty inventory or one with no
// locked rows deliberately produces the explicit empty-list sentinel 00 00.
func BuildLockListSnapshot(record dnfrepo.InventoryRecord) []byte {
	entries := make([]lockSnapshotEntry, 0)
	entries = appendLockSnapshotEntries(entries, record.Slots, listTypeMain)
	entries = appendLockSnapshotEntries(entries, record.Slots, listTypeAvatar)
	entries = appendLockSnapshotEntries(entries, record.Warehouse, listTypePersonalCargo)
	entries = appendLockSnapshotEntries(entries, record.Slots, listTypePet)
	sort.Slice(entries, func(left, right int) bool {
		if entries[left].listType != entries[right].listType {
			return entries[left].listType < entries[right].listType
		}
		return entries[left].slotIndex < entries[right].slotIndex
	})

	var writer packetWriter
	writer.writeUint16(uint16(len(entries)))
	for _, entry := range entries {
		writer.writeByte(entry.listType)
		writer.writeInt16(entry.slotIndex)
		writer.writeByte(entry.state)
		if entry.state == itemLockStatePending {
			writer.writeInt32(entry.remainingSeconds)
		}
	}
	return writer.bytes()
}

func appendLockSnapshotEntries(entries []lockSnapshotEntry, items map[string]dnfrepo.ItemStack, wantListType byte) []lockSnapshotEntry {
	for key, stack := range items {
		listType, slotIndex, ok := parseLockSnapshotSlotKey(key)
		if !ok || listType != wantListType || stack.ItemID <= 0 || stack.Count <= 0 {
			continue
		}
		state, remainingSeconds, locked := lockSnapshotState(stack)
		if !locked {
			continue
		}
		entries = append(entries, lockSnapshotEntry{
			listType:         listType,
			slotIndex:        slotIndex,
			state:            state,
			remainingSeconds: remainingSeconds,
		})
	}
	return entries
}

func parseLockSnapshotSlotKey(key string) (byte, int16, bool) {
	listText, slotText, found := strings.Cut(strings.TrimSpace(key), ":")
	if !found {
		return 0, 0, false
	}
	listValue, err := strconv.ParseUint(strings.TrimSpace(listText), 10, 8)
	if err != nil {
		return 0, 0, false
	}
	slotValue, err := strconv.ParseInt(strings.TrimSpace(slotText), 10, 16)
	if err != nil || slotValue < 0 {
		return 0, 0, false
	}
	return byte(listValue), int16(slotValue), true
}

func lockSnapshotState(stack dnfrepo.ItemStack) (byte, int32, bool) {
	switch strings.ToLower(strings.TrimSpace(currentLockState(stack))) {
	case "1", "active", "locked":
		return itemLockStateActive, 0, true
	case "2", "unlocking", "pending_unlock":
		return itemLockStatePending, lockSnapshotRemainingSeconds(stack), true
	default:
		return 0, 0, false
	}
}

func lockSnapshotRemainingSeconds(stack dnfrepo.ItemStack) int32 {
	if stack.Extra == nil {
		return 0
	}
	seconds, err := strconv.ParseInt(strings.TrimSpace(stack.Extra["equipment_lock_remaining_seconds"]), 10, 32)
	if err != nil || seconds < 0 {
		return 0
	}
	return int32(seconds)
}
