package dnfbridge

import (
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	"strconv"
)

func (r *csharpLegacyUserInfoReader) build109() []byte {
	row := r.one("legacy_character_userinfo109_state", []string{"value0", "value1", "value2", "value3"})
	var w packetWriter
	for idx := 0; idx < 4; idx++ {
		w.writeUint32(rowU32(row, "value"+strconv.Itoa(idx), 0))
	}
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build118() []byte {
	rows := r.rows("legacy_character_userinfo118_rows",
		[]string{"key", "flag_a", "value_a", "value_b", "value_c", "value_d", "flag_b"},
		[]string{"sort_order"})
	var w packetWriter
	w.writeUint32(uint32(len(rows)))
	for _, row := range rows {
		w.writeUint32(rowU32(row, "key", 0))
		w.writeByte(rowU8(row, "flag_a", 0))
		w.writeUint32(rowU32(row, "value_a", 0))
		w.writeUint32(rowU32(row, "value_b", 0))
		w.writeUint32(rowU32(row, "value_c", 0))
		w.writeUint32(rowU32(row, "value_d", 0))
		w.writeByte(rowU8(row, "flag_b", 0))
	}
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build11D() []byte {
	row := r.one("legacy_character_userinfo11d_state", []string{"mode", "byte_a", "value_a", "byte_b", "word"})
	mode := rowU8(row, "mode", 0)
	var w packetWriter
	w.writeByte(mode)
	switch mode {
	case 0:
		w.writeByte(rowU8(row, "byte_a", 0))
		w.writeUint32(rowU32(row, "value_a", 0))
	case 1:
		w.writeByte(rowU8(row, "byte_a", 0))
		w.writeUint32(rowU32(row, "value_a", 0))
		w.writeByte(rowU8(row, "byte_b", 0))
		w.writeUint16(rowU16(row, "word", 0))
	}
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build126() []byte {
	row := r.one("legacy_character_userinfo126_state", []string{"text", "value_a", "value_b", "word_a", "word_b"})
	var w packetWriter
	writeWideNullTerminatedString(&w, rowString(row, "text", ""))
	w.writeUint32(rowU32(row, "value_a", 0))
	w.writeUint32(rowU32(row, "value_b", 0))
	w.writeUint16(rowU16(row, "word_a", 0))
	w.writeUint16(rowU16(row, "word_b", 0))
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build154() []byte {
	row := r.one("legacy_character_userinfo154_state", []string{"key_a", "slot_or_value_a", "key_b", "delta"})
	var w packetWriter
	w.writeUint32(rowU32(row, "key_a", 0))
	w.writeUint16(rowU16(row, "slot_or_value_a", 0))
	w.writeUint32(rowU32(row, "key_b", 0))
	w.writeUint16(rowU16(row, "delta", 0))
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build159() []byte {
	rows := r.rows("legacy_character_userinfo159_rows", []string{"slot", "value"}, []string{"sort_order"})
	rows = limitLegacyRowsU8(rows)
	var w packetWriter
	w.writeByte(byte(len(rows)))
	for _, row := range rows {
		w.writeByte(rowU8(row, "slot", 0xff))
		w.writeUint32(rowU32(row, "value", 0))
	}
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build17C() []byte {
	control := r.one("legacy_character_userinfo17c_control", []string{"header_flag"})
	rows := r.rows("legacy_character_userinfo17c_rows",
		[]string{"selector", "item_or_key", "value"},
		[]string{"sort_order"})
	var w packetWriter
	w.writeByte(rowU8(control, "header_flag", 0))
	w.writeUint16(uint16(len(rows)))
	for _, row := range rows {
		w.writeUint16(rowU16(row, "selector", 0))
		w.writeUint32(rowU32(row, "item_or_key", 0))
		w.writeUint32(rowU32(row, "value", 0))
	}
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build182() []byte {
	control := r.one("legacy_character_userinfo182_control", []string{"actor_key", "header_flag", "outer_count"})
	groupRows := r.rows("legacy_character_userinfo182_groups",
		[]string{"phase", "group_index", "group_flag"},
		[]string{"phase", "group_index"})
	firstRows := r.rows("legacy_character_userinfo182_first_rows",
		[]string{"group_index", "sort_order", "row_state"},
		[]string{"group_index", "sort_order"})
	firstValues := r.rows("legacy_character_userinfo182_first_values",
		[]string{"group_index", "row_sort_order", "value_index", "value", "word"},
		[]string{"group_index", "row_sort_order", "value_index"})
	secondValues := r.rows("legacy_character_userinfo182_second_values",
		[]string{"group_index", "value_index", "word", "value", "flag_a", "flag_b"},
		[]string{"group_index", "value_index"})

	groups := map[string]dnfrepo.LegacyUserInfoRow{}
	for _, row := range groupRows {
		groups[legacyGroupKey(rowInt(row, "phase", 0), rowInt(row, "group_index", 0))] = row
	}
	firstByGroup := map[int][]dnfrepo.LegacyUserInfoRow{}
	for _, row := range firstRows {
		group := rowInt(row, "group_index", 0)
		firstByGroup[group] = append(firstByGroup[group], row)
	}
	firstValueByKey := map[string]dnfrepo.LegacyUserInfoRow{}
	for _, row := range firstValues {
		firstValueByKey[legacyGroupKey(rowInt(row, "group_index", 0), rowInt(row, "row_sort_order", 0), rowInt(row, "value_index", 0))] = row
	}
	secondValueByKey := map[string]dnfrepo.LegacyUserInfoRow{}
	for _, row := range secondValues {
		secondValueByKey[legacyGroupKey(rowInt(row, "group_index", 0), rowInt(row, "value_index", 0))] = row
	}

	outerCount := int(rowU8(control, "outer_count", 0))
	var w packetWriter
	w.writeUint32(rowU32(control, "actor_key", 0))
	w.writeByte(rowU8(control, "header_flag", 0))
	w.writeByte(byte(outerCount))
	for groupIndex := 0; groupIndex < 4; groupIndex++ {
		group := groups[legacyGroupKey(0, groupIndex)]
		rows := limitLegacyRowsU8(firstByGroup[groupIndex])
		w.writeByte(rowU8(group, "group_flag", 0))
		w.writeByte(byte(len(rows)))
		for _, row := range rows {
			sortOrder := rowInt(row, "sort_order", 0)
			w.writeByte(rowU8(row, "row_state", 0))
			for valueIndex := 0; valueIndex < outerCount; valueIndex++ {
				value := firstValueByKey[legacyGroupKey(groupIndex, sortOrder, valueIndex)]
				w.writeUint32(rowU32(value, "value", 0))
				w.writeUint16(rowU16(value, "word", 0))
			}
		}
	}
	for groupIndex := 0; groupIndex < 4; groupIndex++ {
		group := groups[legacyGroupKey(1, groupIndex)]
		w.writeByte(rowU8(group, "group_flag", 0))
		for valueIndex := 0; valueIndex < outerCount; valueIndex++ {
			value := secondValueByKey[legacyGroupKey(groupIndex, valueIndex)]
			w.writeUint16(rowU16(value, "word", 0))
			w.writeUint32(rowU32(value, "value", 0))
			w.writeByte(rowU8(value, "flag_a", 0))
			w.writeByte(rowU8(value, "flag_b", 0))
		}
	}
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build183() []byte {
	control := r.one("legacy_character_userinfo183_control", []string{"header_a", "header_b", "header_value", "global_flag", "mode", "extra_value", "tail_flag"})
	var w packetWriter
	w.writeByte(rowU8(control, "header_a", 0))
	w.writeByte(rowU8(control, "header_b", 0))
	w.writeUint32(rowU32(control, "header_value", 0))
	w.writeByte(rowU8(control, "global_flag", 0))
	mode := rowU8(control, "mode", 0)
	w.writeByte(mode)
	if mode != 1 && mode != 2 {
		return w.bytes()
	}
	primary := limitLegacyRowsU8(r.rows("legacy_character_userinfo183_primary_rows",
		[]string{"word0", "key_or_value", "word1", "word2", "flag0", "flag1", "bool_flag", "flag2"},
		[]string{"sort_order"}))
	secondary := limitLegacyRowsU8(r.rows("legacy_character_userinfo183_secondary_rows",
		[]string{"row_type", "value0", "value1", "value2", "word0", "flag", "word1", "word2"},
		[]string{"sort_order"}))
	w.writeUint32(rowU32(control, "extra_value", 0))
	w.writeByte(byte(len(primary)))
	for _, row := range primary {
		w.writeUint16(rowU16(row, "word0", 0))
		w.writeUint32(rowU32(row, "key_or_value", 0))
		w.writeUint16(rowU16(row, "word1", 0))
		w.writeUint16(rowU16(row, "word2", 0))
		w.writeByte(rowU8(row, "flag0", 0))
		w.writeByte(rowU8(row, "flag1", 0))
		w.writeByte(rowU8(row, "bool_flag", 0))
		w.writeByte(rowU8(row, "flag2", 0))
	}
	w.writeByte(byte(len(secondary)))
	for _, row := range secondary {
		w.writeByte(rowU8(row, "row_type", 0))
		w.writeUint32(rowU32(row, "value0", 0))
		w.writeUint32(rowU32(row, "value1", 0))
		w.writeUint32(rowU32(row, "value2", 0))
		w.writeUint16(rowU16(row, "word0", 0))
		w.writeByte(rowU8(row, "flag", 0))
		w.writeUint16(rowU16(row, "word1", 0))
		w.writeUint16(rowU16(row, "word2", 0))
	}
	w.writeByte(rowU8(control, "tail_flag", 0))
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build184() []byte {
	control := r.one("legacy_character_userinfo184_control", []string{"header", "value"})
	first := limitLegacyRowsU8(r.rows("legacy_character_userinfo184_first_rows",
		[]string{"word", "value_a", "value_b", "value_c", "flag_a", "flag_b"},
		[]string{"sort_order"}))
	second := limitLegacyRowsU8(r.rows("legacy_character_userinfo184_second_rows",
		[]string{"value_a", "value_b", "word"},
		[]string{"sort_order"}))
	third := limitLegacyRowsU8(r.rows("legacy_character_userinfo184_third_rows",
		[]string{"value_a", "value_b", "word"},
		[]string{"sort_order"}))
	var w packetWriter
	w.writeByte(rowU8(control, "header", 0))
	w.writeUint32(rowU32(control, "value", 0))
	w.writeByte(byte(len(first)))
	for _, row := range first {
		w.writeUint16(rowU16(row, "word", 0))
		w.writeUint32(rowU32(row, "value_a", 0))
		w.writeUint32(rowU32(row, "value_b", 0))
		w.writeUint16(rowU16(row, "value_c", 0))
		w.writeByte(rowU8(row, "flag_a", 0))
		w.writeByte(rowU8(row, "flag_b", 0))
	}
	write184ValueRows(&w, second)
	write184ValueRows(&w, third)
	return w.bytes()
}

func write184ValueRows(w *packetWriter, rows []dnfrepo.LegacyUserInfoRow) {
	w.writeByte(byte(len(rows)))
	for _, row := range rows {
		w.writeUint32(rowU32(row, "value_a", 0))
		w.writeUint32(rowU32(row, "value_b", 0))
		w.writeUint16(rowU16(row, "word", 0))
	}
}
