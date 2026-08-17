package dnfbridge

import (
	"encoding/binary"

	dnfquest "longheng.io/server/internal/modules/dnf/quest"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func (r *csharpLegacyUserInfoReader) buildCurrentUserInfoMode1(character dnfrepo.CharacterRecord, hasCharacter bool, objectKey uint16, adventureLevel uint32) []byte {
	return r.buildCurrentUserInfoMode1InContext(character, hasCharacter, objectKey, adventureLevel, currentSceneObjectContext)
}

func (r *csharpLegacyUserInfoReader) buildCurrentUserInfoMode1InContext(character dnfrepo.CharacterRecord, hasCharacter bool, objectKey uint16, adventureLevel uint32, ownerChannel byte) []byte {
	return r.buildCurrentUserInfoMode1WithEquipmentInContext(character, hasCharacter, objectKey, true, adventureLevel, ownerChannel)
}

func (r *csharpLegacyUserInfoReader) buildCurrentUserInfoMode1WithEquipment(character dnfrepo.CharacterRecord, hasCharacter bool, objectKey uint16, includeEquipment bool, adventureLevel uint32) []byte {
	return r.buildCurrentUserInfoMode1WithEquipmentInContext(character, hasCharacter, objectKey, includeEquipment, adventureLevel, currentSceneObjectContext)
}

func (r *csharpLegacyUserInfoReader) buildCurrentUserInfoMode1WithEquipmentInContext(character dnfrepo.CharacterRecord, hasCharacter bool, objectKey uint16, includeEquipment bool, adventureLevel uint32, ownerChannel byte) []byte {
	if !hasCharacter {
		character = dnfrepo.CharacterRecord{Level: 1}
	}
	row := r.one("legacy_character_subtype1_fields", csharpUserInfoSubtype1Columns())
	r.logMissingCurrentUserInfoStats(row, character)
	statBlob := r.buildCurrentUserInfoMode1StatBlob(row, character)
	creature := r.currentEquippedCreature()
	extraEquipmentSlotState := currentExtraEquipmentSlotState(character)
	var w packetWriter
	w.writeByte(1)
	w.writeUint16(1)
	w.writeByte(currentSceneObjectRoute)
	w.writeByte(ownerChannel)
	// MCP/IDA sub_2008010：mode=1 每条记录先读 raw16，再读 u16 object key。
	// 这里不再写旧 subtype1 的 level/exp/stat 字段；那些字节会让 actor 查找 key 错位。
	// raw16+2 is u32 HonorExpert level and raw16+6 is u64 HonorExpert
	// progress EXP. Ordinary account honor must never enter these fields.
	w.writeUint16(0) // independent state
	writeCurrentHonorExpertState(&w, character)
	w.writeByte(0) // independent flag A
	w.writeByte(0) // independent flag B
	w.writeUint16(objectKey)
	r.writeCurrentUserInfoMode1ObjectTail(
		&w,
		objectKey,
		includeEquipment,
		adventureLevel,
		statU32(character, "exp", 0),
		statBlob,
		extraEquipmentSlotState,
		creature,
	)
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) buildCurrentUserInfoMode3(character dnfrepo.CharacterRecord, hasCharacter bool, objectKey uint16, adventureLevel uint32) []byte {
	return r.buildCurrentUserInfoMode3InContext(
		character,
		hasCharacter,
		objectKey,
		adventureLevel,
		currentSceneObjectContext,
	)
}

func (r *csharpLegacyUserInfoReader) buildCurrentUserInfoMode3InContext(character dnfrepo.CharacterRecord, hasCharacter bool, objectKey uint16, adventureLevel uint32, ownerChannel byte) []byte {
	if !hasCharacter {
		character = dnfrepo.CharacterRecord{Level: 1}
	}
	row := r.one("legacy_character_subtype1_fields", csharpUserInfoSubtype1Columns())
	r.logMissingCurrentUserInfoStats(row, character)
	statBlob := r.buildCurrentUserInfoMode1StatBlob(row, character)
	creature := r.currentEquippedCreature()
	extraEquipmentSlotState := currentExtraEquipmentSlotState(character)

	var w packetWriter
	w.writeByte(3)
	w.writeUint16(1)
	w.writeByte(currentSceneObjectRoute)
	w.writeByte(ownerChannel)
	w.writeUint32(adventureLevel) // sub_24A0970 adventure-group manage level
	w.writeUint16(0)              // mode3 raw8 auxiliary u16
	w.writeByte(0)                // mode3 raw8 flag A
	w.writeByte(0)                // mode3 raw8 flag B
	w.writeUint16(objectKey)
	// Current EXE mode3 path is sub_200BEA0 -> sub_2008600:
	// after the object key, sub_2004AE0 reads one u32, then sub_3457C50 reads
	// a u32 length and the stat blob bytes.
	// sub_2004AE0 reads this cumulative EXP value and silently commits it
	// through actor vtable +0xA9C using the actor template's current level.
	w.writeUint32(statU32(character, "exp", 0))
	w.writeUint32(uint32(len(statBlob)))
	w.writeBytes(statBlob)
	r.writeCurrentUserInfoMode3ObjectTail(&w, character, extraEquipmentSlotState, creature)
	return w.bytes()
}

func currentExtraEquipmentSlotState(character dnfrepo.CharacterRecord) byte {
	value := numericCharacterStatValue(character, "ex_equip_slot_stat")
	if value <= 0 {
		return 0
	}
	return byte(value) & dnfquest.ExEquipSlotAll
}

func (r *csharpLegacyUserInfoReader) writeCurrentUserInfoMode3ObjectTail(w *packetWriter, character dnfrepo.CharacterRecord, extraEquipmentSlotState byte, creature currentEquippedCreatureSnapshot) {
	// MCP/IDA sub_2008600: mode=3 is the current EXE object stat refresh path.
	// The first byte after sub_2002B30 is stored at
	// actor.vfunc_0xA98()+0x1AC. This is the current reader's durable
	// support/magic-stone/earring bitset, matching the database-backed byte
	// carried after the stat blob by the old selected-character body.
	w.writeByte(extraEquipmentSlotState)
	// sub_2008600 calls sub_1D77560 after applying the stat blob. A zero count
	// clears worn actor slots that mode1 just created, so repeat the same real
	// DB/PVF-backed create rows in this combined stat/equipment phase.
	equipmentRows := r.currentMode1EquipmentObjectRows()
	createRows := currentMode1EquipmentCreateRows(equipmentRows)
	w.writeByte(byte(len(createRows)))
	for _, row := range createRows {
		writeCurrentMode1EquipmentCreateRow(w, row)
	}
	w.writeUint32(0) // sub_1D77560 final state dword
	w.writeUint32(creature.itemID)
	w.writeUint32(creature.serialOrHandle) // sub_20077D0 equipped creature {item id, serial/handle}
	w.writeByte(0xff)                      // sub_2003420 selected resource group
	w.writeByte(0)                         // group0 count when target is not current object
	w.writeByte(0)                         // group1 count when target is not current object
	w.writeByte(creature.level)
	w.writeUint32(0)
	w.writeUint32(0)
	w.writeUint32(0)
	w.writeByte(0)
	w.writeByte(0)
	w.writeByte(0)
	w.writeByte(0)
	w.writeByte(0) // sub_2006940 list count
	w.writeUint32(0)
	// Current sub_2005520 stores this pair as the target actor's duel grade
	// and rank points. Project the durable fields instead of redrawing every
	// inspected player as the zero-rank placeholder.
	w.writeUint16(clampUint16ForUserInfo(statU32(character, "pvp_grade", 0)))
	w.writeUint32(statU32(character, "pvp_rank_point", 0))
	w.writeRawDstr(nil)
	// sub_2005520 stores this byte at actor state +0x34C. The personal-info
	// panel reads the same field for the target's expert/sub-profession row.
	w.writeByte(byte(numericCharacterStatValue(character, "expert_job_type")))
	w.writeByte(0)
	w.writeUint32(0) // sub_2002DC0 count
	w.writeByte(0)   // sub_2006BB0 count
	w.writeByte(0)   // sub_20070C0 count
}

func (r *csharpLegacyUserInfoReader) writeCurrentUserInfoMode1ObjectTail(w *packetWriter, objectKey uint16, includeEquipment bool, adventureLevel uint32, totalExperience uint32, statBlob []byte, extraEquipmentSlotState byte, creature currentEquippedCreatureSnapshot) {
	// MCP/IDA sub_2008010 reads two compact current helper blocks after
	// raw16+objectKey before the extra-equipment-slot state.
	// Keep repeated mode1 refreshes on the same authoritative cumulative
	// EXP baseline before a later op37 computes its visible delta.
	w.writeUint32(totalExperience)
	w.writeUint32(uint32(len(statBlob)))
	w.writeBytes(statBlob) // sub_2002B30 always applies this state; zero length would overwrite the actor with 92 zero bytes
	w.writeByte(extraEquipmentSlotState)
	var equipmentRows []currentMode1EquipmentObjectRow
	if includeEquipment {
		equipmentRows = r.currentMode1EquipmentObjectRows()
	}
	createRows := currentMode1EquipmentCreateRows(equipmentRows)
	updateRows := currentMode1EquipmentDeferredUpdateRows(equipmentRows)
	if len(equipmentRows) > 0 && r != nil && r.session != nil && r.service != nil {
		r.service.logGameEvent(r.session, "game-upper-selected-userinfo-mode1-equipment-projection",
			"character_id", r.characterID,
			"equipment_entry_count", len(equipmentRows),
			"create_entry_count", len(createRows),
			"deferred_update_count", len(updateRows),
			"create_v74_source", "authoritative_equipment_quality_seed_with_pvf_starter_zero")
	}
	if len(updateRows) > 0 && r != nil && r.session != nil && r.service != nil {
		r.service.logGameEvent(r.session, "game-upper-selected-userinfo-mode1-equipment-create-rows-deferred",
			"character_id", r.characterID,
			"create_entry_count", len(createRows),
			"update_entry_count", len(updateRows),
			"deferred_entry_count", len(updateRows),
			"reason", "rows_without_verified_current_create_state")
	}
	w.writeByte(byte(len(createRows))) // sub_1D77560 equipped object create count
	for _, row := range createRows {
		writeCurrentMode1EquipmentCreateRow(w, row)
	}
	w.writeUint32(0) // sub_1D77560 final state dword
	if len(equipmentRows) > 0 && len(updateRows) == 0 {
		// The working same-EXE Python mode1 stops after the create list and
		// zero final state. The current reader still consumes the following
		// sub_2007B00 key/count pair, but a zero count does not use the key.
		// Do not immediately update objects that were just created.
		w.writeUint16(0)
	} else {
		w.writeUint16(objectKey)
	}
	w.writeByte(byte(len(updateRows))) // sub_2007B00 deferred equipment/object row count
	for _, row := range updateRows {
		slot := binary.LittleEndian.Uint16(row.update.data[0:2])
		w.writeByte(0) // sub_2007B00 reads but does not use this row-kind byte.
		w.writeByte(3) // Current actor equipped-body path; type 1 is avatar/list manager.
		// sub_2007B00 passes this value directly to actor vfunc +2856. Current
		// op14/sub_1D73120 uses the raw u16 target the same way, so both paths
		// address the current actor equipment slot rather than the DB worn slot.
		w.writeUint16(slot)
		w.writeBytes(row.update.data[:])
		if row.readsRawBlocks {
			w.writeUint32(0) // sub_225C960 -> sub_3457C50 u32 len + raw; current raw variant still unmapped.
			w.writeUint32(0) // sub_225C9B0 -> sub_3457C50 u32 len + raw; current raw variant still unmapped.
		}
	}
	w.writeUint32(creature.itemID)
	w.writeUint32(creature.serialOrHandle) // sub_20077D0 equipped creature {item id, serial/handle}
	// sub_2003420 reads the selected resource byte and then two empty resource
	// group lists. For the current actor those lists are consumed by sub_2002F10.
	w.writeByte(0xff)             // sub_2003420 selected resource group, -1 keeps current/default
	w.writeByte(0)                // sub_2003420/sub_2002F10 group 0 count
	w.writeByte(0)                // sub_2003420/sub_2002F10 group 1 count
	w.writeByte(creature.level)   // post sub_2003420 equipped creature level
	w.writeByte(0)                // sub_2002BE0 count
	w.writeByte(0)                // sub_2002BE0 flag A
	w.writeByte(0)                // sub_2002BE0 flag B
	w.writeByte(0)                // sub_2002BE0 flag C
	w.writeByte(0)                // sub_2002CC0 count
	w.writeUint32(0)              // sub_2002DC0 count
	w.writeUint32(adventureLevel) // post object dword -> sub_24A0970 adventure-group manage level
	w.writeUint32(0)              // sub_20025B0 dword
	w.writeByte(0)                // sub_20070C0 count
	w.writeByte(0)                // final fatigue/aux byte
}
