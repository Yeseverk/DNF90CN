package dnfbridge

import (
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	"strconv"
)

func (r *csharpLegacyUserInfoReader) build47() []byte {
	stateRows := r.rows("legacy_character_userinfo_slot_category_state",
		[]string{"category", "state_a", "state_b", "enable_flag"},
		[]string{"category"})
	states := make(map[int]dnfrepo.LegacyUserInfoRow, len(stateRows))
	for _, row := range stateRows {
		states[rowInt(row, "category", 0)] = row
	}
	entries := r.loadSlotEntries()
	var w packetWriter
	for category := 0; category < 8; category++ {
		state := states[category]
		target := rowU8(state, "state_b", byte(category))
		if target > 7 {
			target = byte(category)
		}
		group := collectSlotEntries(entries, 3, int(target))
		if len(group) > 0xff {
			group = group[:0xff]
		}
		w.writeByte(rowU8(state, "state_a", 0))
		w.writeByte(target)
		w.writeByte(byte(len(group)))
		for _, entry := range group {
			w.writeUint32(entry.key)
			w.writeUint32(entry.value)
		}
		w.writeByte(rowU8(state, "enable_flag", 0))
	}
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build57() []byte {
	rows := r.rows("legacy_character_userinfo57_rows",
		[]string{"sort_order", "object_key", "field_a", "route_or_index", "field_c", "state", "value32"},
		[]string{"sort_order"})
	slotRows := r.rows("legacy_character_userinfo57_slots",
		[]string{"row_sort_order", "slot_index", "mode", "value"},
		[]string{"row_sort_order", "slot_index"})
	slots := map[int]map[int]dnfrepo.LegacyUserInfoRow{}
	for _, row := range slotRows {
		order := rowInt(row, "row_sort_order", 0)
		idx := rowInt(row, "slot_index", 0)
		if slots[order] == nil {
			slots[order] = map[int]dnfrepo.LegacyUserInfoRow{}
		}
		slots[order][idx] = row
	}
	var w packetWriter
	w.writeUint16(uint16(len(rows)))
	for _, row := range rows {
		w.writeUint16(rowU16(row, "object_key", 0))
		w.writeByte(rowU8(row, "field_a", 0))
		w.writeByte(rowU8(row, "route_or_index", 0))
		w.writeByte(rowU8(row, "field_c", 0))
		w.writeByte(rowU8(row, "state", 0))
		w.writeUint32(rowU32(row, "value32", 0))
		order := rowInt(row, "sort_order", 0)
		for idx := 0; idx < 6; idx++ {
			slot := slots[order][idx]
			w.writeByte(rowU8(slot, "mode", 0xff))
			w.writeUint16(rowU16(slot, "value", 0xffff))
		}
	}
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build58() []byte {
	rows := r.rows("legacy_character_userinfo58_rows",
		[]string{"object_key", "state", "value"},
		[]string{"sort_order"})
	var w packetWriter
	w.writeUint16(uint16(len(rows)))
	for _, row := range rows {
		w.writeUint16(rowU16(row, "object_key", 0))
		w.writeByte(rowU8(row, "state", 0))
		w.writeUint16(rowU16(row, "value", 0))
	}
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build59() []byte {
	control := r.one("legacy_character_userinfo59_control", []string{"object_key"})
	rows := r.rows("legacy_character_userinfo59_slots",
		[]string{"category", "mode", "value"},
		[]string{"sort_order"})
	if len(rows) > 0xff {
		rows = rows[:0xff]
	}
	var w packetWriter
	w.writeUint16(rowU16(control, "object_key", 0))
	w.writeByte(byte(len(rows)))
	for _, row := range rows {
		w.writeByte(rowU8(row, "category", 0))
		w.writeByte(rowU8(row, "mode", 0xff))
		w.writeUint16(rowU16(row, "value", 0xffff))
	}
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build5B() []byte {
	control := r.one("legacy_character_userinfo5b_control", []string{"header_flag"})
	rows := r.rows("legacy_character_userinfo5b_rows",
		[]string{"value_a", "value_b", "value_c", "value_d", "value_e", "value_f"},
		[]string{"sort_order"})
	var w packetWriter
	w.writeByte(rowU8(control, "header_flag", 0))
	w.writeUint16(uint16(len(rows)))
	for _, row := range rows {
		w.writeUint16(rowU16(row, "value_a", 0))
		w.writeByte(rowU8(row, "value_b", 0))
		w.writeByte(rowU8(row, "value_c", 0))
		w.writeByte(rowU8(row, "value_d", 0))
		w.writeByte(rowU8(row, "value_e", 0))
		w.writeUint16(rowU16(row, "value_f", 0))
	}
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build5C() []byte {
	row := r.one("legacy_character_userinfo5c_state", []string{"header_a", "header_b", "state0", "state1", "state2", "state3", "state4", "state5", "state6", "state7"})
	var w packetWriter
	w.writeByte(rowU8(row, "header_a", 0))
	w.writeByte(rowU8(row, "header_b", 0))
	for idx := 0; idx < 8; idx++ {
		w.writeUint32(rowU32(row, "state"+strconv.Itoa(idx), 0))
	}
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build5F() []byte {
	row := r.one("legacy_character_userinfo5f_state", []string{"category", "mode_or_apply_flag", "scale_or_visual_flag", "delta_value", "existing_slot_value"})
	var w packetWriter
	w.writeByte(rowU8(row, "category", 0))
	w.writeByte(rowU8(row, "mode_or_apply_flag", 0))
	w.writeByte(rowU8(row, "scale_or_visual_flag", 0))
	w.writeUint16(rowU16(row, "delta_value", 0))
	w.writeUint16(rowU16(row, "existing_slot_value", 0xffff))
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build60() []byte {
	pairs := r.rows("legacy_character_userinfo60_pairs",
		[]string{"key", "value"},
		[]string{"sort_order"})
	wide := r.rows("legacy_character_userinfo60_wide_rows",
		[]string{"key", "value0", "value1", "value2", "value3"},
		[]string{"sort_order"})
	if len(pairs) > 0xff {
		pairs = pairs[:0xff]
	}
	if len(wide) > 0xff {
		wide = wide[:0xff]
	}
	var w packetWriter
	w.writeByte(byte(len(pairs)))
	for _, row := range pairs {
		w.writeByte(rowU8(row, "key", 0))
		w.writeByte(rowU8(row, "value", 0))
	}
	w.writeByte(byte(len(wide)))
	for _, row := range wide {
		w.writeByte(rowU8(row, "key", 0))
		w.writeUint32(rowU32(row, "value0", 0))
		w.writeUint32(rowU32(row, "value1", 0))
		w.writeUint32(rowU32(row, "value2", 0))
		w.writeUint32(rowU32(row, "value3", 0))
	}
	return w.bytes()
}
