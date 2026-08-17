package dnfbridge

import (
	"encoding/binary"
	"sort"
	"strings"
)

const currentMode1EquipmentCreateRowBaseWireSize = 56
const currentMode1EquipmentRawBlockHeaderSize = 4
const currentMode1EquipmentRawBlockMaxLen = 1024
const currentMode1EquipmentVectorRecordSize = 8
const currentMode1EquipmentVectorRecordMaxCount = 2
const currentMode1EquipmentDwordVectorMaxCount = 2

const currentMode1EquipmentIndexedStateMaxRecords = 3

type currentMode1EquipmentIndexedStateRecord struct {
	value0 byte
	value1 byte
	value2 byte
}

type currentMode1EquipmentIndexedState struct {
	records             []currentMode1EquipmentIndexedStateRecord
	activeValue         byte
	selectedIndex       byte
	selectedValue       byte
	selectedRecord      currentMode1EquipmentIndexedStateRecord
	hasSelectedRecord   bool
	currentItemMismatch bool
}

type currentMode1EquipmentObjectRow struct {
	createEnabled              bool
	currentItemStateDerived    bool
	slot                       byte
	itemID                     uint32
	equipmentType              uint64
	equipmentTypeKnown         bool
	instance                   uint32
	extData                    byte
	durability                 uint16
	durabilityKnown            bool
	linkedItemID               uint32
	auxValue                   uint32
	auxByte                    byte
	bindFlag                   byte
	marker16                   uint16
	linkedRaw                  []byte
	readsRawBlocks             bool
	rawA                       []byte
	rawB                       []byte
	readsDurabilityOverrideU32 bool
	durabilityOverride         uint32
	vector1D6E020              []byte
	state112                   uint32
	vector225CCA0              []byte
	state1E636E0               uint16
	indexedState1C61F40        currentMode1EquipmentIndexedState
	state120                   byte
	state160                   byte
	state128                   byte
	state140                   uint16
	state144                   byte
	resourceByte               byte
	state168                   byte
	state169                   byte
	state170                   byte
	lateRawC                   []byte
	lateRawD                   []byte
	state183                   byte
	lateRawE                   []byte
	update                     currentItemListEntry
}

func (r *csharpLegacyUserInfoReader) currentMode1EquipmentObjectRows() []currentMode1EquipmentObjectRow {
	if r == nil || r.service == nil || strings.TrimSpace(r.characterID) == "" {
		return nil
	}
	repos, ok := r.service.repositoryGroup()
	if !ok || repos.Equipment == nil {
		return nil
	}
	record, found, err := repos.Equipment.Load(r.ctx, r.characterID)
	if err != nil {
		if r.session != nil {
			r.service.logGameEvent(r.session, "game-upper-selected-userinfo-mode1-equipment-load-failed", "character_id", r.characterID, "error", err)
		}
		return nil
	}
	if !found || len(record.Entries) == 0 {
		return nil
	}
	rows := make([]currentMode1EquipmentObjectRow, 0, len(record.Entries))
	for _, equipped := range record.Entries {
		if equipped.SlotIndex < 0 || equipped.SlotIndex > 0xff || equipped.ItemID <= 0 {
			continue
		}
		entry, entryOK := currentItemListEntryFromEquipment(equipped)
		if !entryOK {
			continue
		}
		equipmentType, equipmentTypeKnown := r.currentMode1EquipmentType(equipped)
		durability, durabilityKnown := currentMode1EquipmentDurabilityWord(equipped, entry)
		indexedState := currentMode1EquipmentIndexedStateFromCurrentItem(entry)
		readsRawBlocks := currentMode1EquipmentReadsRawBlocks(equipped.Extra, equipmentType, equipmentTypeKnown)
		rawA, rawB := currentMode1EquipmentAvatarRawBlocks(equipped.Extra, entry, readsRawBlocks)
		rows = append(rows, currentMode1EquipmentObjectRow{
			createEnabled:           currentMode1EquipmentCreateEnabled(equipped),
			currentItemStateDerived: true,
			slot:                    byte(equipped.SlotIndex),
			itemID:                  sceneInventoryUint32FromInt64(equipped.ItemID),
			equipmentType:           equipmentType,
			equipmentTypeKnown:      equipmentTypeKnown,
			// sub_1D77560's create-row v74 field is the equipped object's
			// grade/instance value. A zero default happens to render as top
			// quality, so rebuilding an already-owned actor after op19 must
			// carry the same equipment quality seed as the authoritative
			// 0x77 item row. Keep the historical PVF starter projection at
			// zero because its create scalar is an initialization marker,
			// not a durable quality seed.
			instance:                   currentMode1EquipmentGradeOrInstance(equipped),
			extData:                    entry.data[0x0a],
			durability:                 durability,
			durabilityKnown:            durabilityKnown,
			linkedItemID:               sceneInventoryExtraUint32(equipped.Extra, "mode1_linked_item_id", "mode1_clone_or_linked_id", "clone_or_linked_id"),
			auxValue:                   currentMode1EquipmentExtraUint32Or(equipped.Extra, binary.LittleEndian.Uint32(entry.data[0x0e:0x12]), "mode1_aux_value", "mode1_state_24", "state_24"),
			auxByte:                    currentMode1EquipmentExtraByteOr(equipped.Extra, entry.data[0x12], "mode1_aux_byte", "mode1_state_32", "state_32"),
			bindFlag:                   currentMode1EquipmentExtraByteOr(equipped.Extra, entry.data[0x13], "mode1_state_56", "state_56", "value_c"),
			marker16:                   currentMode1EquipmentExtraUint16Or(equipped.Extra, binary.LittleEndian.Uint16(entry.data[0x14:0x16]), "marker_16", "marker16", "value_d"),
			linkedRaw:                  currentMode1EquipmentExtraHex(equipped.Extra, currentMode1EquipmentRawBlockMaxLen, "mode1_linked_raw_hex", "mode1_clone_raw_hex"),
			readsRawBlocks:             readsRawBlocks,
			rawA:                       rawA,
			rawB:                       rawB,
			readsDurabilityOverrideU32: currentMode1EquipmentReadsDurabilityOverrideU32(equipped.Extra, equipmentType, equipmentTypeKnown),
			durabilityOverride:         currentMode1EquipmentDurabilityOverride(equipped),
			vector1D6E020:              currentMode1EquipmentVectorRecords(equipped.Extra, "mode1_sub_1d6e020_records_hex", "mode1_vector_records_hex"),
			state112:                   currentMode1EquipmentState112(equipped, entry),
			// sub_1D77560 reads this through sub_225CCA0 and applies it to
			// ordinary equipment through the same vtable +280 method used
			// after op14. Derive it from the durable raw+0x3C vector so a
			// cold-login create row restores sockets without a wear cycle.
			vector225CCA0: currentMode1EquipmentDwordVectorFromCurrentItem(
				entry,
				equipped.Extra,
				"mode1_sub_225cca0_dwords_hex",
				"mode1_dword_vector_hex",
			),
			state1E636E0:        binary.LittleEndian.Uint16(entry.data[0x45:0x47]),
			indexedState1C61F40: indexedState,
			state120:            sceneInventoryExtraByte(equipped.Extra, "mode1_state_120", "state_120"),
			state160:            sceneInventoryExtraByte(equipped.Extra, "mode1_state_160", "state_160"),
			state128:            sceneInventoryExtraByte(equipped.Extra, "mode1_state_128", "state_128"),
			state140:            sceneInventoryExtraUint16(equipped.Extra, "mode1_state_140", "state_140"),
			state144:            sceneInventoryExtraByte(equipped.Extra, "mode1_state_144", "state_144"),
			resourceByte:        sceneInventoryExtraByte(equipped.Extra, "mode1_resource_byte", "resource_byte"),
			state168:            sceneInventoryExtraByte(equipped.Extra, "mode1_state_168", "state_168"),
			state169:            sceneInventoryExtraByte(equipped.Extra, "mode1_state_169", "state_169"),
			state170:            sceneInventoryExtraByte(equipped.Extra, "mode1_state_170", "state_170"),
			lateRawC:            currentMode1EquipmentExtraHex(equipped.Extra, currentMode1EquipmentRawBlockMaxLen, "mode1_raw_c_hex", "mode1_state_171_raw_hex"),
			lateRawD:            currentMode1EquipmentExtraHex(equipped.Extra, currentMode1EquipmentRawBlockMaxLen, "mode1_raw_d_hex", "mode1_state_175_179_raw_hex"),
			state183:            sceneInventoryExtraByte(equipped.Extra, "mode1_state_183", "state_183"),
			lateRawE:            currentMode1EquipmentExtraHex(equipped.Extra, currentMode1EquipmentRawBlockMaxLen, "mode1_raw_e_hex", "mode1_state_188_192_raw_hex"),
			update:              entry,
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].slot < rows[j].slot
	})
	if len(rows) > 0xff {
		rows = rows[:0xff]
	}
	if len(rows) > 0 && r.session != nil {
		r.service.logGameEvent(r.session, "game-upper-selected-userinfo-mode1-equipment-create-rows",
			"character_id", r.characterID,
			"entry_count", len(rows),
			"unknown_type_count", currentMode1EquipmentUnknownTypeCount(rows),
			"row_summary", currentMode1EquipmentRowSummary(rows),
			"mcp_handler", "sub_1D77560")
	}
	return rows
}

func currentMode1EquipmentAvatarRawBlocks(extra map[string]string, entry currentItemListEntry, readsRawBlocks bool) ([]byte, []byte) {
	rawA := currentMode1EquipmentExtraHex(extra, currentMode1EquipmentRawBlockMaxLen, "mode1_raw_a_hex", "mode1_sub_225c960_raw_hex", "mode1_raw_block_a_hex")
	rawB := currentMode1EquipmentExtraHex(extra, currentMode1EquipmentRawBlockMaxLen, "mode1_raw_b_hex", "mode1_sub_225c9b0_raw_hex", "mode1_raw_block_b_hex")
	if !readsRawBlocks {
		return rawA, rawB
	}
	// Current EXE sub_1D77560 uses the same two type<=11 extension readers as
	// list-1/op13 and the avatar branch of op14: socket state first, color state
	// second. Socket mutations persist those canonical blobs on the item row,
	// so use them as the mode1 create-state fallback instead of requiring a
	// second set of mode1-only aliases. Explicit captured mode1 blocks retain
	// precedence when present.
	if len(rawA) == 0 {
		rawA = append([]byte(nil), entry.avatarSocketData...)
	}
	if len(rawB) == 0 {
		rawB = append([]byte(nil), entry.avatarColorData...)
	}
	return rawA, rawB
}
