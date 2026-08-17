package dnfbridge

import (
	"encoding/binary"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func currentItemListEntriesFromMap(items map[string]dnfrepo.ItemStack, listType byte) []currentItemListEntry {
	if len(items) == 0 {
		return nil
	}
	entries := make([]currentItemListEntry, 0, len(items))
	for key, stack := range items {
		keyListType, slot, ok := parseSceneInventorySlotKey(key)
		goldWallet := keyListType == dnfrepo.MainInventoryListType && slot == 0 && stack.ItemID == 0
		if !ok || keyListType != listType || slot < 0 || (stack.ItemID <= 0 && !goldWallet) {
			continue
		}
		entries = append(entries, currentItemListEntryFromStack(listType, slot, stack))
	}
	return entries
}

func currentItemListEntryFromStack(listType byte, slot int16, stack dnfrepo.ItemStack) currentItemListEntry {
	if listType == dnfrepo.MainInventoryListType && slot == 0 && stack.ItemID == 0 {
		return currentGoldWalletItemListEntry(stack.Count)
	}
	extra := stack.Extra
	raw := append([]byte(nil), stack.RawEntry...)
	entry := currentItemListEntryFromRaw(raw, extra)
	entry.patchCore(slot, sceneInventoryUint32FromInt64(stack.ItemID), currentItemListStackAmount(listType, stack))
	entry.setByte(0x0A, sceneInventoryExtraByte(extra, "ext_data0", "ext0", "packed_flag_byte", "packed_flag", "packed"))
	entry.setUint16(0x0B, sceneInventoryExtraUint16(extra, "durability", "max_durability"))
	if usePeriod, ok := currentItemListStackableUsePeriod(stack); ok {
		binary.LittleEndian.PutUint16(entry.data[0x0B:0x0D], usePeriod)
	}
	// Fallback: when expire_time is set but the Extra-based use_period probe
	// did not fire (missing item_kind/stackable_type), derive the remaining
	// seconds directly from expire_time so the client never sees 0x0B==0
	// on an item that still has a live expiration timestamp.
	if binary.LittleEndian.Uint16(entry.data[0x0B:0x0D]) == 0 {
		if expire := currentItemListStackExpire(stack); expire != 0 {
			binary.LittleEndian.PutUint16(entry.data[0x0B:0x0D],
				currentPVFStackableUsePeriodSeconds(time.Unix(int64(expire), 0).UTC(), time.Now().UTC()))
		}
	}
	entry.setByte(0x0D, currentItemListBindFlag(stack.Bind, extra))
	valueA := currentItemListStackValueA(listType, stack)
	if listType == currentPetInventoryListType {
		// A list-7 row is authoritative even when the field is zero: clear a
		// stale serial mirrored here by an older encoder.
		binary.LittleEndian.PutUint32(entry.data[0x0E:0x12], valueA)
	} else {
		entry.setUint32(0x0E, valueA)
	}
	entry.setByte(0x12, sceneInventoryExtraByte(extra, "byte_12", "value_12"))
	entry.setByte(0x13, sceneInventoryExtraByte(extra, "byte_13", "value_13", "value_c"))
	entry.setUint16(0x14, sceneInventoryExtraUint16(extra, "marker_16", "marker16", "value_d"))
	expire := currentItemListStackExpire(stack)
	if listType == currentPetInventoryListType {
		// The current EXE copies this dword into the creature detail's
		// remaining-seconds field. Legacy 47-byte equipped-pet rows keep the
		// creature serial at byte 0x18; when raw_entry_hex is padded to a 0x77
		// list row, that serial otherwise becomes serial<<16 seconds (serial 41
		// displays as 32 days). Pet time is authoritative here: zero for a
		// permanent pet, remaining seconds for a genuinely expiring instance.
		binary.LittleEndian.PutUint32(
			entry.data[currentPetRemainSecondsOffset:currentPetRemainSecondsOffset+4],
			currentPetRemainingSecondsAt(expire, time.Now().UTC()),
		)
	}
	entry.clearLegacyWrongExpiration(expire)
	if listType == currentPetInventoryListType {
		binary.LittleEndian.PutUint32(
			entry.data[currentItemListExpireTimeOffset:currentItemListExpireTimeOffset+4],
			expire,
		)
	} else {
		entry.setUint32(currentItemListExpireTimeOffset, expire)
	}
	entry.setByte(0x57, sceneInventoryExtraByte(extra, "byte_57", "value_57"))
	entry.setByte(0x58, sceneInventoryExtraByte(extra, "byte_58", "value_58"))
	entry.setByte(0x59, sceneInventoryExtraByte(extra, "byte_59", "value_59"))
	entry.copyFixed(0x72, currentItemListFixedExtraBytes(extra, 5, "tail_data_72", "tail72"))
	if listType == 1 {
		entry.avatarSocketData = currentItemListAvatarSocketData(extra)
		entry.avatarColorData = currentItemListAvatarColorData(extra)
	}
	if listType == 0 || listType == 3 {
		currentApplyEquipmentSocketVectorToEntry(&entry, extra)
		currentApplyEquipmentEffectRuneToEntry(&entry, extra)
	}
	return entry
}

func currentItemListStackAmount(listType byte, stack dnfrepo.ItemStack) uint32 {
	if listType == 7 {
		if value := sceneInventoryExtraUint32(stack.Extra, "creature_serial_or_handle", "serial", "handle", "instance_value", "item_uid"); value != 0 {
			return value
		}
	}
	if listType == dnfrepo.MainInventoryListType || listType == 3 {
		if seed := currentEquipmentQualitySeedFromStack(stack); seed != 0 {
			return seed
		}
	}
	if stack.Count > 0 && (listType == dnfrepo.MainInventoryListType ||
		strings.EqualFold(strings.TrimSpace(stack.Extra["item_kind"]), "stackable")) {
		// ItemStack.Count is the authoritative durable quantity for ordinary
		// main-inventory rows and PVF-proven stackables in personal cargo.
		// Legacy amount aliases in Extra and RawEntry can lag behind after a
		// stack use or cross-container merge; preferring them here makes the
		// repository-backed refresh redraw the pre-mutation count.
		return sceneInventoryUint32FromInt64(stack.Count)
	}
	return sceneInventoryStackAmount(stack)
}

func currentItemListStackValueA(listType byte, stack dnfrepo.ItemStack) uint32 {
	if listType == currentPetInventoryListType {
		// Pet rows keep the creature serial/handle only at raw+0x06. Mirroring
		// that serial into raw+0x0E makes the current client interpret serial 3
		// as a three-day duration (and serial 19 as nineteen hours). This
		// matches the pet module's dedicated list-7 encoder.
		return sceneInventoryExtraUint32(stack.Extra, "value_a")
	}
	return currentItemStackValueA(stack)
}

func currentItemListEntryFromLegacyItem(item dnfrepo.LegacyInventoryItem) currentItemListEntry {
	extra := item.Extra
	entry := currentItemListEntryFromRawExtra(extra)
	entry.patchCore(item.SlotIndex, sceneInventoryUint32FromInt64(item.ItemTemplateID), sceneInventoryLegacyAmount(item))
	entry.setByte(0x0A, sceneInventoryLegacyPacked(item))
	entry.setUint16(0x0B, sceneInventoryLegacyUint16(item.Durability, extra, "durability", "max_durability"))
	entry.setByte(0x0D, sceneInventoryLegacyByte(item.SealFlag, extra, "seal_flag", "seal", "bind_flag", "bind"))
	entry.setUint32(0x0E, sceneInventoryLegacyUint32(item.InstanceValue, extra, "value_a", "instance_value", "item_uid", "serial", "count_or_instance"))
	entry.setByte(0x13, sceneInventoryLegacyByte(item.OptionValue, extra, "byte_13", "value_13", "value_c"))
	entry.setUint16(0x14, sceneInventoryLegacyUint16(item.Marker16, extra, "marker_16", "marker16", "value_d"))
	return entry
}

func currentItemListEntryFromRawExtra(extra map[string]string) currentItemListEntry {
	return currentItemListEntryFromRaw(nil, extra)
}

func currentItemListEntryFromRaw(rawEntry []byte, extra map[string]string) currentItemListEntry {
	var entry currentItemListEntry
	raw := rawEntry
	if len(raw) != currentItemListEntryWireSize {
		raw = currentItemListFixedExtraBytes(extra, currentItemListEntryWireSize, "raw_entry_hex", "current_raw_entry_77", "raw_entry_77")
	}
	if len(raw) == currentItemListEntryWireSize {
		copy(entry.data[:], raw)
	}
	return entry
}
