package dnfbridge

import (
	"encoding/binary"
	"encoding/hex"
	"strings"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func writeCurrentMode1EquipmentCreateRow(w *packetWriter, row currentMode1EquipmentObjectRow) {
	// MCP/IDA sub_1D77560 creates and attaches current actor equipment objects.
	actorSlot, _ := currentMode1EquipmentActorSlot(row)
	w.writeByte(actorSlot)
	w.writeUint32(row.itemID)
	w.writeUint32(row.instance)
	w.writeByte(row.extData)
	w.writeUint16(row.durability)
	w.writeUint32(row.linkedItemID)
	if row.linkedItemID != 0 && actorSlot == 9 {
		writeCurrentMode1EquipmentRawBlock(w, row.linkedRaw)
	}
	w.writeUint32(row.auxValue)
	w.writeByte(row.auxByte)
	w.writeByte(row.bindFlag)
	w.writeUint16(row.marker16)
	if row.readsRawBlocks {
		writeCurrentMode1EquipmentRawBlock(w, row.rawA) // sub_225C960(type=3).
		writeCurrentMode1EquipmentRawBlock(w, row.rawB) // sub_225C9B0(type=3).
	}
	if row.readsDurabilityOverrideU32 {
		w.writeUint32(row.durabilityOverride)
	}
	w.writeByte(byte(len(row.vector1D6E020) / currentMode1EquipmentVectorRecordSize)) // sub_1D6E020 count.
	w.writeBytes(row.vector1D6E020)
	w.writeUint32(row.state112)
	w.writeByte(byte(len(row.vector225CCA0) / 4)) // sub_225CCA0 count.
	w.writeBytes(row.vector225CCA0)
	// sub_1E636E0 and sub_1C61F40 are mandatory readers in the current EXE.
	// Their values come from the same current 0x77 item state used by op25.
	w.writeUint16(row.state1E636E0)
	writeCurrentMode1EquipmentIndexedState(w, row.indexedState1C61F40)
	w.writeByte(row.state120)
	w.writeByte(row.state160)
	w.writeByte(row.state128)
	w.writeUint16(row.state140)
	w.writeByte(row.state144)
	w.writeByte(row.resourceByte)
	w.writeByte(row.state168)
	w.writeByte(row.state169)
	w.writeByte(row.state170)
	writeCurrentMode1EquipmentRawBlock(w, row.lateRawC) // sub_3457C50 raw block.
	writeCurrentMode1EquipmentRawBlock(w, row.lateRawD) // sub_3457C50 raw block -> sub_2176DB0 a24.
	w.writeByte(row.state183)
	writeCurrentMode1EquipmentRawBlock(w, row.lateRawE) // sub_3457C50 raw block -> sub_2176DB0 a25.
}

func currentMode1EquipmentState112(equipped dnfrepo.EquipmentEntry, entry currentItemListEntry) uint32 {
	if value, ok := sceneInventoryExtraUint(equipped.Extra, "mode1_state_112", "state_112", "value_after_sub_1d6e020"); ok {
		return sceneInventoryClampUint32(value)
	}
	return binary.LittleEndian.Uint32(entry.data[0x38:0x3c])
}

func currentMode1EquipmentDurabilityWord(equipped dnfrepo.EquipmentEntry, entry currentItemListEntry) (uint16, bool) {
	if value, ok := sceneInventoryExtraUint(equipped.Extra, "mode1_durability", "mode1_detail_state_word", "current_exe_detail_state_word", "mode1_state_word"); ok {
		if value > uint64(^uint16(0)) {
			return 0, false
		}
		return uint16(value), true
	}
	return binary.LittleEndian.Uint16(entry.data[0x0b:0x0d]), true
}

func currentMode1EquipmentExtraByteOr(extra map[string]string, fallback byte, keys ...string) byte {
	if value, ok := sceneInventoryExtraUint(extra, keys...); ok && value <= 0xff {
		return byte(value)
	}
	return fallback
}

func currentMode1EquipmentExtraUint16Or(extra map[string]string, fallback uint16, keys ...string) uint16 {
	if value, ok := sceneInventoryExtraUint(extra, keys...); ok && value <= 0xffff {
		return uint16(value)
	}
	return fallback
}

func currentMode1EquipmentExtraUint32Or(extra map[string]string, fallback uint32, keys ...string) uint32 {
	if value, ok := sceneInventoryExtraUint(extra, keys...); ok && value <= uint64(^uint32(0)) {
		return uint32(value)
	}
	return fallback
}

func currentMode1EquipmentDurabilityOverride(equipped dnfrepo.EquipmentEntry) uint32 {
	if value, ok := sceneInventoryExtraUint(equipped.Extra, "mode1_durability_override", "mode1_type26_u32", "type26_u32"); ok {
		return sceneInventoryClampUint32(value)
	}
	return 0xffffffff
}

func currentMode1EquipmentCreateRowWireSizeFor(row currentMode1EquipmentObjectRow) int {
	size := currentMode1EquipmentCreateRowBaseWireSize +
		len(row.vector1D6E020) +
		len(row.vector225CCA0) +
		currentMode1EquipmentIndexedStateVariableWireSize(row.indexedState1C61F40) +
		len(row.lateRawC) +
		len(row.lateRawD) +
		len(row.lateRawE)
	if actorSlot, ok := currentMode1EquipmentActorSlot(row); ok && row.linkedItemID != 0 && actorSlot == 9 {
		size += currentMode1EquipmentRawBlockHeaderSize + len(row.linkedRaw)
	}
	if row.readsRawBlocks {
		size += currentMode1EquipmentRawBlockHeaderSize*2 + len(row.rawA) + len(row.rawB)
	}
	if row.readsDurabilityOverrideU32 {
		size += 4
	}
	return size
}

func currentMode1EquipmentActorSlot(row currentMode1EquipmentObjectRow) (byte, bool) {
	// sub_1D77560 stores presence in a 33-byte bitmap and cleans slots 0..32.
	// sub_2504C10 forwards the byte to sub_2467A60, which stores the object at
	// actorArray[target]. The target is the current runtime equipment slot.
	runtimeSlot, ok := currentMode1EquipmentRuntimeType(row)
	if !ok || runtimeSlot > 32 {
		return 0, false
	}
	return runtimeSlot, true
}

func currentMode1EquipmentRuntimeType(row currentMode1EquipmentObjectRow) (byte, bool) {
	if !row.equipmentTypeKnown || row.equipmentType > uint64(^uint8(0)) {
		return 0, false
	}
	return byte(row.equipmentType), true
}

func currentMode1EquipmentIndexedStateFromCurrentItem(entry currentItemListEntry) currentMode1EquipmentIndexedState {
	state := currentMode1EquipmentIndexedState{
		activeValue:   entry.data[0x51],
		selectedIndex: entry.data[0x52],
		selectedValue: entry.data[0x53],
		selectedRecord: currentMode1EquipmentIndexedStateRecord{
			value0: entry.data[0x54],
			value1: entry.data[0x55],
			value2: entry.data[0x56],
		},
	}

	wantCount := int(entry.data[0x47])
	if wantCount > currentMode1EquipmentIndexedStateMaxRecords {
		state.currentItemMismatch = true
		return state
	}
	for index := 0; index < currentMode1EquipmentIndexedStateMaxRecords; index++ {
		value0 := entry.data[0x48+index]
		if value0 == 0 {
			continue
		}
		state.records = append(state.records, currentMode1EquipmentIndexedStateRecord{
			value0: value0,
			value1: entry.data[0x4b+index],
			value2: entry.data[0x4e+index],
		})
	}
	if len(state.records) != wantCount {
		state.currentItemMismatch = true
		return state
	}
	if wantCount > 0 && state.selectedIndex != 0xff {
		state.hasSelectedRecord = true
	}
	return state
}

func writeCurrentMode1EquipmentIndexedState(w *packetWriter, state currentMode1EquipmentIndexedState) {
	w.writeByte(byte(len(state.records)))
	for _, record := range state.records {
		w.writeByte(record.value0)
		w.writeByte(record.value1)
		w.writeByte(record.value2)
	}
	if len(state.records) == 0 {
		return
	}
	w.writeByte(state.activeValue)
	w.writeByte(state.selectedIndex)
	if !state.hasSelectedRecord {
		return
	}
	w.writeByte(state.selectedValue)
	w.writeByte(state.selectedRecord.value0)
	w.writeByte(state.selectedRecord.value1)
	w.writeByte(state.selectedRecord.value2)
}

func currentMode1EquipmentIndexedStateVariableWireSize(state currentMode1EquipmentIndexedState) int {
	size := len(state.records) * 3
	if len(state.records) == 0 {
		return size
	}
	size += 2
	if state.hasSelectedRecord {
		size += 4
	}
	return size
}

func writeCurrentMode1EquipmentRawBlock(w *packetWriter, data []byte) {
	w.writeUint32(uint32(len(data)))
	w.writeBytes(data)
}

func currentMode1EquipmentVectorRecords(extra map[string]string, keys ...string) []byte {
	data := currentMode1EquipmentExtraHex(extra, currentMode1EquipmentVectorRecordSize*currentMode1EquipmentVectorRecordMaxCount, keys...)
	if len(data) == 0 || len(data)%currentMode1EquipmentVectorRecordSize != 0 {
		return nil
	}
	return data
}

func currentMode1EquipmentDwordVector(extra map[string]string, keys ...string) []byte {
	data := currentMode1EquipmentExtraHex(extra, 4*currentMode1EquipmentDwordVectorMaxCount, keys...)
	if len(data) == 0 || len(data)%4 != 0 {
		return nil
	}
	return data
}

func currentMode1EquipmentDwordVectorFromCurrentItem(
	entry currentItemListEntry,
	extra map[string]string,
	keys ...string,
) []byte {
	if data := currentMode1EquipmentDwordVector(extra, keys...); len(data) != 0 {
		return data
	}
	count := int(entry.data[currentEquipmentVectorOffset])
	if count <= 0 || count > currentMode1EquipmentDwordVectorMaxCount {
		return nil
	}
	start := currentEquipmentVectorOffset + 1
	end := start + count*4
	return append([]byte(nil), entry.data[start:end]...)
}

func currentMode1EquipmentExtraHex(extra map[string]string, maxLen int, keys ...string) []byte {
	if maxLen <= 0 {
		return nil
	}
	for _, key := range keys {
		raw := strings.TrimSpace(extra[key])
		if raw == "" {
			continue
		}
		raw = strings.NewReplacer(" ", "", "\t", "", "\r", "", "\n", "", "0x", "", "0X", "").Replace(raw)
		decoded, err := hex.DecodeString(raw)
		if err != nil || len(decoded) == 0 || len(decoded) > maxLen {
			continue
		}
		return decoded
	}
	return nil
}
