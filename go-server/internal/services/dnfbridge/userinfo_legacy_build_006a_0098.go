package dnfbridge

import (
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	"strconv"
)

func (r *csharpLegacyUserInfoReader) build6A() []byte {
	row := r.one("legacy_character_userinfo6a_state", []string{"value", "object_key"})
	var w packetWriter
	w.writeUint16(rowU16(row, "value", 0))
	w.writeUint16(rowU16(row, "object_key", 0))
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build73() []byte {
	row := r.one("legacy_character_userinfo73_state", []string{"mode", "state", "value"})
	mode := rowU8(row, "mode", 0)
	var w packetWriter
	w.writeByte(mode)
	w.writeByte(rowU8(row, "state", 0))
	if mode != 0 {
		w.writeUint16(rowU16(row, "value", 0xffff))
	}
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build7A() []byte {
	control := r.one("legacy_character_userinfo7a_control", []string{"mode", "value_a", "value_b"})
	rows := r.rows("legacy_character_userinfo7a_rows",
		[]string{"key_a", "key_b", "value_a", "value_b"},
		[]string{"sort_order"})
	var w packetWriter
	w.writeByte(rowU8(control, "mode", 0))
	w.writeUint32(rowU32(control, "value_a", 0))
	w.writeUint32(rowU32(control, "value_b", 0))
	w.writeUint16(uint16(len(rows)))
	for _, row := range rows {
		w.writeUint16(rowU16(row, "key_a", 0))
		w.writeUint16(rowU16(row, "key_b", 0))
		w.writeUint32(rowU32(row, "value_a", 0))
		w.writeUint32(rowU32(row, "value_b", 0))
	}
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build80() []byte {
	control := r.one("legacy_character_userinfo80_control", []string{"actor_key", "profile_a", "profile_b", "route", "word_a", "word_b"})
	slots := r.rows("legacy_character_userinfo80_slots",
		[]string{"category", "mode", "value"},
		[]string{"sort_order"})
	extras := r.rows("legacy_character_userinfo80_extra_words",
		[]string{"value"},
		[]string{"sort_order"})
	if len(slots) > 0xff {
		slots = slots[:0xff]
	}
	if len(extras) > 0xff {
		extras = extras[:0xff]
	}
	var w packetWriter
	w.writeUint32(rowU32(control, "actor_key", 0))
	w.writeByte(rowU8(control, "profile_a", 0))
	w.writeByte(rowU8(control, "profile_b", 0))
	w.writeByte(rowU8(control, "route", 0))
	w.writeUint16(rowU16(control, "word_a", 0))
	w.writeUint16(rowU16(control, "word_b", 0))
	w.writeByte(byte(len(slots)))
	for _, row := range slots {
		w.writeByte(rowU8(row, "category", 0))
		w.writeByte(rowU8(row, "mode", 0xff))
		w.writeUint16(rowU16(row, "value", 0xffff))
	}
	w.writeByte(byte(len(extras)))
	for _, row := range extras {
		w.writeUint16(rowU16(row, "value", 0))
	}
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build86() []byte {
	rows := r.rows("legacy_character_userinfo86_rows",
		[]string{"sort_order", "key_value", "flag", "value_a", "value_b"},
		[]string{"sort_order"})
	childRows := r.rows("legacy_character_userinfo86_children",
		[]string{"row_sort_order", "child_key", "state"},
		[]string{"row_sort_order", "sort_order"})
	children := map[int][]dnfrepo.LegacyUserInfoRow{}
	for _, row := range childRows {
		order := rowInt(row, "row_sort_order", 0)
		children[order] = append(children[order], row)
	}
	var w packetWriter
	w.writeUint16(uint16(len(rows)))
	for _, row := range rows {
		w.writeUint32(rowU32(row, "key_value", 0))
		flag := rowU8(row, "flag", 0)
		w.writeByte(flag)
		if flag == 0 {
			continue
		}
		w.writeUint16(rowU16(row, "value_a", 0))
		w.writeUint16(rowU16(row, "value_b", 0))
		group := children[rowInt(row, "sort_order", 0)]
		w.writeUint16(uint16(len(group)))
		for _, child := range group {
			w.writeUint16(rowU16(child, "child_key", 0))
			w.writeByte(rowU8(child, "state", 0))
		}
	}
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build88() []byte {
	row := r.one("legacy_character_userinfo88_state", []string{"object_key", "value"})
	var w packetWriter
	w.writeUint16(rowU16(row, "object_key", 0))
	w.writeUint32(rowU32(row, "value", 0))
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build8F() []byte {
	control := r.one("legacy_character_userinfo8f_control", []string{"context_key", "root_value", "header_value"})
	listA := r.rows("legacy_character_userinfo8f_list_a",
		[]string{"key_value", "value_a", "value_b", "flag_a", "flag_b", "bool_flag", "state"},
		[]string{"sort_order"})
	listB := r.rows("legacy_character_userinfo8f_list_b",
		[]string{"row_type", "key_a", "key_b", "key_c", "value_a", "flag", "value_b", "value_c"},
		[]string{"sort_order"})
	if len(listA) > 0xff {
		listA = listA[:0xff]
	}
	if len(listB) > 0xff {
		listB = listB[:0xff]
	}
	var w packetWriter
	w.writeUint16(rowU16(control, "context_key", 0))
	w.writeUint32(rowU32(control, "root_value", 0))
	w.writeUint32(rowU32(control, "header_value", 0))
	w.writeByte(byte(len(listA)))
	for _, row := range listA {
		w.writeUint32(rowU32(row, "key_value", 0))
		w.writeUint16(rowU16(row, "value_a", 0))
		w.writeUint32(rowU32(row, "value_b", 0))
		w.writeByte(rowU8(row, "flag_a", 0))
		w.writeByte(rowU8(row, "flag_b", 0))
		w.writeByte(rowU8(row, "bool_flag", 0))
		w.writeByte(rowU8(row, "state", 0))
	}
	w.writeByte(byte(len(listB)))
	for _, row := range listB {
		w.writeByte(rowU8(row, "row_type", 0))
		w.writeUint32(rowU32(row, "key_a", 0))
		w.writeUint32(rowU32(row, "key_b", 0))
		w.writeUint32(rowU32(row, "key_c", 0))
		w.writeUint16(rowU16(row, "value_a", 0))
		w.writeByte(rowU8(row, "flag", 0))
		w.writeUint16(rowU16(row, "value_b", 0))
		w.writeUint16(rowU16(row, "value_c", 0))
	}
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build90() []byte {
	control := r.one("legacy_character_userinfo90_control", []string{"flag_a", "value_a", "value_b", "flag_b", "value_c", "include_primary_block"})
	textRows := r.rows("legacy_character_userinfo90_text_rows",
		[]string{"is_primary", "group_index", "slot_index", "text_value", "flag_a", "flag_b"},
		[]string{"is_primary", "group_index", "slot_index"})
	summaryRows := r.rows("legacy_character_userinfo90_summaries",
		[]string{"is_primary", "group_index", "summary_word", "value_a", "value_b"},
		[]string{"is_primary", "group_index"})
	texts := map[string]dnfrepo.LegacyUserInfoRow{}
	for _, row := range textRows {
		texts[text90Key(rowInt(row, "is_primary", 0) != 0, rowInt(row, "group_index", -1), rowInt(row, "slot_index", 0))] = row
	}
	summaries := map[string]dnfrepo.LegacyUserInfoRow{}
	for _, row := range summaryRows {
		summaries[summary90Key(rowInt(row, "is_primary", 0) != 0, rowInt(row, "group_index", -1))] = row
	}
	var w packetWriter
	w.writeByte(rowU8(control, "flag_a", 0))
	w.writeUint32(rowU32(control, "value_a", 0))
	w.writeUint32(rowU32(control, "value_b", 1))
	w.writeByte(rowU8(control, "flag_b", 0))
	w.writeUint32(rowU32(control, "value_c", 0))
	includePrimary := rowU8(control, "include_primary_block", 0)
	w.writeByte(includePrimary)
	if includePrimary != 0 {
		for slot := 0; slot < 8; slot++ {
			write90TextRow(&w, texts[text90Key(true, -1, slot)])
		}
		write90Summary(&w, summaries[summary90Key(true, -1)])
	}
	for group := 0; group < 5; group++ {
		for slot := 0; slot < 8; slot++ {
			write90TextRow(&w, texts[text90Key(false, group, slot)])
		}
		write90Summary(&w, summaries[summary90Key(false, group)])
	}
	return w.bytes()
}

func text90Key(primary bool, group int, slot int) string {
	prefix := "g"
	if primary {
		prefix = "p"
	}
	return prefix + ":" + strconv.Itoa(group) + ":" + strconv.Itoa(slot)
}

func summary90Key(primary bool, group int) string {
	prefix := "g"
	if primary {
		prefix = "p"
	}
	return prefix + ":" + strconv.Itoa(group)
}

func write90TextRow(w *packetWriter, row dnfrepo.LegacyUserInfoRow) {
	writeWideNullTerminatedString(w, rowString(row, "text_value", ""))
	w.writeByte(rowU8(row, "flag_a", 0))
	w.writeByte(rowU8(row, "flag_b", 0))
}

func write90Summary(w *packetWriter, row dnfrepo.LegacyUserInfoRow) {
	w.writeUint16(rowU16(row, "summary_word", 0))
	w.writeUint32(rowU32(row, "value_a", 0))
	w.writeUint32(rowU32(row, "value_b", 0))
}

func (r *csharpLegacyUserInfoReader) build91() []byte {
	control := r.one("legacy_character_userinfo91_control", []string{"header_value"})
	rows := r.rows("legacy_character_userinfo91_rows",
		[]string{"group_index", "key_value", "value"},
		[]string{"group_index", "sort_order"})
	groups := map[int][]dnfrepo.LegacyUserInfoRow{}
	for _, row := range rows {
		group := rowInt(row, "group_index", 0)
		groups[group] = append(groups[group], row)
	}
	var w packetWriter
	w.writeUint32(rowU32(control, "header_value", 0))
	for group := 0; group < 4; group++ {
		groupRows := groups[group]
		if len(groupRows) > 0xff {
			groupRows = groupRows[:0xff]
		}
		w.writeByte(byte(len(groupRows)))
		for _, row := range groupRows {
			w.writeUint32(rowU32(row, "key_value", 0))
			w.writeUint32(rowU32(row, "value", 0))
		}
	}
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build98() []byte {
	row := r.one("legacy_character_userinfo98_state", []string{"header", "word_a", "word_b", "state0", "state1", "state2", "state3", "apply_flag"})
	var w packetWriter
	w.writeByte(rowU8(row, "header", 0))
	w.writeUint16(rowU16(row, "word_a", 0))
	w.writeUint16(rowU16(row, "word_b", 0))
	for idx := 0; idx < 4; idx++ {
		w.writeByte(rowU8(row, "state"+strconv.Itoa(idx), 0))
	}
	w.writeByte(rowU8(row, "apply_flag", 0))
	return w.bytes()
}
