package dnfbridge

import (
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	"strconv"
)

func (r *csharpLegacyUserInfoReader) buildA0() []byte {
	control := r.one("legacy_character_userinfoa0_control", []string{"header_flag"})
	rows := r.rows("legacy_character_userinfoa0_rows", []string{"selector"}, []string{"sort_order"})
	var w packetWriter
	w.writeByte(rowU8(control, "header_flag", 0))
	w.writeUint16(uint16(len(rows)))
	for _, row := range rows {
		w.writeUint16(rowU16(row, "selector", 0))
	}
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) buildA1() []byte {
	row := r.one("legacy_character_userinfoa1_state", []string{"value0", "value1", "value2", "value3"})
	var w packetWriter
	for idx := 0; idx < 4; idx++ {
		w.writeByte(rowU8(row, "value"+strconv.Itoa(idx), 0))
	}
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) buildA2() []byte {
	row := r.one("legacy_character_userinfoa2_state", []string{"mode", "value_a", "value_b"})
	var w packetWriter
	w.writeByte(rowU8(row, "mode", 0))
	w.writeUint32(rowU32(row, "value_a", 0))
	w.writeUint32(rowU32(row, "value_b", 0))
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) buildAA() []byte {
	row := r.one("legacy_character_userinfoaa_state", []string{"value", "flag_a", "flag_b"})
	var w packetWriter
	w.writeUint16(rowU16(row, "value", 0))
	w.writeByte(rowU8(row, "flag_a", 0))
	w.writeByte(rowU8(row, "flag_b", 0))
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) buildB6() []byte {
	rows := r.rows("legacy_character_userinfob6_rows",
		[]string{"sort_order", "text_a", "flag_a", "flag_b", "flag_c", "text_b", "value"},
		[]string{"sort_order"})
	valueRows := r.rows("legacy_character_userinfob6_values",
		[]string{"row_sort_order", "value_index", "value"},
		[]string{"row_sort_order", "value_index"})
	values := map[int]map[int]uint32{}
	for _, row := range valueRows {
		order := rowInt(row, "row_sort_order", 0)
		idx := rowInt(row, "value_index", 0)
		if values[order] == nil {
			values[order] = map[int]uint32{}
		}
		values[order][idx] = rowU32(row, "value", 0)
	}
	if len(rows) > 3 {
		rows = rows[:3]
	}
	var w packetWriter
	w.writeByte(byte(len(rows)))
	for _, row := range rows {
		writeWideNullTerminatedString(&w, rowString(row, "text_a", ""))
		w.writeByte(rowU8(row, "flag_a", 0))
		w.writeByte(rowU8(row, "flag_b", 0))
		w.writeByte(rowU8(row, "flag_c", 0))
		writeWideNullTerminatedString(&w, rowString(row, "text_b", ""))
		w.writeUint32(rowU32(row, "value", 0))
		order := rowInt(row, "sort_order", 0)
		// C# UserInfoB6TextListBodyBuilder 与 EXE skeleton 已确认这里只能写 12 个 u32，写 13 个会顶歪后续 USERINFO 流。
		for idx := 0; idx < 12; idx++ {
			w.writeUint32(values[order][idx])
		}
	}
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) buildC9() []byte {
	row := r.one("legacy_character_userinfoc9_state", []string{"value0", "value1", "value2", "value3", "value4"})
	var w packetWriter
	for idx := 0; idx < 5; idx++ {
		w.writeUint32(rowU32(row, "value"+strconv.Itoa(idx), 0))
	}
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) buildD0() []byte {
	row := r.one("legacy_character_userinfod0_state", []string{"value0", "value1", "value2", "value3", "value4", "value5"})
	var w packetWriter
	for idx := 0; idx < 6; idx++ {
		w.writeUint32(rowU32(row, "value"+strconv.Itoa(idx), 0))
	}
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) buildD1() []byte {
	control := r.one("legacy_character_userinfod1_control", []string{"header_a", "header_b"})
	rows := r.rows("legacy_character_userinfod1_rows",
		[]string{"group_index", "key", "value"},
		[]string{"group_index", "sort_order"})
	groups := map[int][]dnfrepo.LegacyUserInfoRow{}
	for _, row := range rows {
		group := rowInt(row, "group_index", 0)
		groups[group] = append(groups[group], row)
	}
	var w packetWriter
	w.writeByte(rowU8(control, "header_a", 0))
	w.writeByte(rowU8(control, "header_b", 0))
	for group := 0; group < 4; group++ {
		groupRows := groups[group]
		if len(groupRows) > 0xff {
			groupRows = groupRows[:0xff]
		}
		w.writeByte(byte(len(groupRows)))
		for _, row := range groupRows {
			w.writeUint32(rowU32(row, "key", 0))
			w.writeUint32(rowU32(row, "value", 0))
		}
	}
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) buildD2() []byte {
	control := r.one("legacy_character_userinfod2_control", []string{"tail_value"})
	rows := r.rows("legacy_character_userinfod2_rows",
		[]string{"row_type", "context_key", "key", "flag_a", "flag_b", "value_a", "value_b", "value_c", "value_d"},
		[]string{"sort_order"})
	var w packetWriter
	w.writeUint16(uint16(len(rows)))
	for _, row := range rows {
		w.writeByte(rowU8(row, "row_type", 0))
		w.writeUint16(rowU16(row, "context_key", 0))
		w.writeUint32(rowU32(row, "key", 0))
		w.writeByte(rowU8(row, "flag_a", 0))
		w.writeByte(rowU8(row, "flag_b", 0))
		w.writeUint16(rowU16(row, "value_a", 0))
		w.writeUint16(rowU16(row, "value_b", 0))
		w.writeUint16(rowU16(row, "value_c", 0))
		w.writeUint16(rowU16(row, "value_d", 0))
	}
	w.writeUint16(rowU16(control, "tail_value", 0))
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) buildD3() []byte {
	control := r.one("legacy_character_userinfod3_control", []string{"header_a", "header_b", "header_value", "global_flag", "mode", "extra_value", "tail_flag"})
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
	primary := r.rows("legacy_character_userinfod3_primary_rows",
		[]string{"word0", "byte0", "byte1", "word1", "word2", "word3", "byte2", "word4", "value", "byte3", "byte4", "bool_flag", "byte5"},
		[]string{"sort_order"})
	secondary := r.rows("legacy_character_userinfod3_secondary_rows",
		[]string{"row_type", "value0", "value1", "value2", "word0", "flag", "word1", "word2"},
		[]string{"sort_order"})
	if len(primary) > 0xff {
		primary = primary[:0xff]
	}
	if len(secondary) > 0xff {
		secondary = secondary[:0xff]
	}
	w.writeUint32(rowU32(control, "extra_value", 0))
	w.writeByte(byte(len(primary)))
	for _, row := range primary {
		w.writeUint16(rowU16(row, "word0", 0))
		w.writeByte(rowU8(row, "byte0", 0))
		w.writeByte(rowU8(row, "byte1", 0))
		w.writeUint16(rowU16(row, "word1", 0))
		w.writeUint16(rowU16(row, "word2", 0))
		w.writeUint16(rowU16(row, "word3", 0))
		w.writeByte(rowU8(row, "byte2", 0))
		w.writeUint16(rowU16(row, "word4", 0))
		w.writeUint32(rowU32(row, "value", 0))
		w.writeByte(rowU8(row, "byte3", 0))
		w.writeByte(rowU8(row, "byte4", 0))
		w.writeByte(rowU8(row, "bool_flag", 0))
		w.writeByte(rowU8(row, "byte5", 0))
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

func (r *csharpLegacyUserInfoReader) buildD5Like(table string) []byte {
	row := r.one(table, []string{"value", "flag"})
	var w packetWriter
	w.writeUint32(rowU32(row, "value", 0))
	w.writeByte(rowU8(row, "flag", 0))
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) buildD6() []byte {
	row := r.one("legacy_character_userinfod6_state", []string{"flag_a", "flag_b", "value_a", "value_b"})
	var w packetWriter
	w.writeByte(rowU8(row, "flag_a", 0))
	w.writeByte(rowU8(row, "flag_b", 0))
	w.writeUint32(rowU32(row, "value_a", 0))
	w.writeUint32(rowU32(row, "value_b", 0))
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) buildD7() []byte {
	row := r.one("legacy_character_userinfod7_state", []string{"flag", "value"})
	var w packetWriter
	w.writeByte(rowU8(row, "flag", 0))
	w.writeUint32(rowU32(row, "value", 0))
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) buildD8() []byte {
	control := r.one("legacy_character_userinfod8_control", []string{"mode"})
	rows := r.rows("legacy_character_userinfod8_rows", []string{"value"}, []string{"sort_order"})
	var w packetWriter
	w.writeByte(rowU8(control, "mode", 0))
	w.writeUint16(uint16(len(rows)))
	for _, row := range rows {
		w.writeUint16(rowU16(row, "value", 0))
	}
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) buildDF() []byte {
	row := r.one("legacy_character_userinfodf_state", []string{"flag", "value0", "value1", "value2", "value3", "value4", "value5"})
	var w packetWriter
	w.writeByte(rowU8(row, "flag", 0))
	for idx := 0; idx < 6; idx++ {
		w.writeUint32(rowU32(row, "value"+strconv.Itoa(idx), 0))
	}
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) buildE0() []byte {
	control := r.one("legacy_character_userinfoe0_control", []string{"value", "flag"})
	rows := r.rows("legacy_character_userinfoe0_rows", []string{"text"}, []string{"sort_order"})
	rows = limitLegacyRowsU8(rows)
	var w packetWriter
	w.writeByte(byte(len(rows)))
	for _, row := range rows {
		writeWideNullTerminatedString(&w, rowString(row, "text", ""))
	}
	w.writeUint32(rowU32(control, "value", 0))
	w.writeByte(rowU8(control, "flag", 0))
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) buildFE() []byte {
	row := r.one("legacy_character_userinfofe_state", []string{"value_a", "value_b"})
	var w packetWriter
	w.writeByte(rowU8(row, "value_a", 0))
	w.writeByte(rowU8(row, "value_b", 0))
	return w.bytes()
}
