package dnfbridge

import dnfrepo "longheng.io/server/internal/modules/dnf/repository"

func (r *csharpLegacyUserInfoReader) build22D() []byte {
	row, ok := r.rawOne("legacy_character_userinfo22d_state", []string{"word_a", "word_b", "word_c", "word_d", "mode"})
	if !ok {
		return nil
	}
	var w packetWriter
	w.writeUint16(rowU16(row, "word_a", 0))
	w.writeUint16(rowU16(row, "word_b", 0))
	w.writeUint16(rowU16(row, "word_c", 0))
	w.writeUint16(rowU16(row, "word_d", 0))
	w.writeByte(rowU8(row, "mode", 0))
	r.loaded = true
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build22E() []byte {
	state, ok := r.rawOne("legacy_character_userinfo22e_state", []string{"object_key", "mode", "byte_a", "byte_b", "value_a", "value_b"})
	if !ok {
		return nil
	}
	pairs := limitLegacyRowsU8(r.queryRows("legacy_character_userinfo22e_pairs",
		[]string{"sort_order", "value_a", "value_b"},
		[]string{"sort_order"},
		false))
	var w packetWriter
	w.writeUint16(rowU16(state, "object_key", 0))
	mode := rowU8(state, "mode", 0)
	w.writeByte(mode)
	switch mode {
	case 0:
		w.writeByte(rowU8(state, "byte_a", 0))
		w.writeByte(rowU8(state, "byte_b", 0))
		write22EPairs(&w, pairs)
	case 1:
		w.writeUint32(rowU32(state, "value_a", 0))
		w.writeByte(rowU8(state, "byte_a", 0))
		w.writeByte(rowU8(state, "byte_b", 0))
		write22EPairs(&w, pairs)
	case 2:
		w.writeByte(rowU8(state, "byte_a", 0))
		w.writeByte(rowU8(state, "byte_b", 0))
		w.writeUint32(rowU32(state, "value_a", 0))
		w.writeUint32(rowU32(state, "value_b", 0))
	case 3:
		write22EPairs(&w, pairs)
	case 4:
		w.writeUint32(rowU32(state, "value_a", 0))
	default:
		// C# 会抛异常；这里直接跳过，避免坏 DB 行变成空包或顶乱后续初始化。
		return nil
	}
	r.loaded = true
	return w.bytes()
}

func write22EPairs(w *packetWriter, pairs []dnfrepo.LegacyUserInfoRow) {
	w.writeByte(byte(len(pairs)))
	for _, pair := range pairs {
		w.writeByte(rowU8(pair, "value_a", 0))
		w.writeByte(rowU8(pair, "value_b", 0))
	}
}

func (r *csharpLegacyUserInfoReader) build253() []byte {
	state, ok := r.rawOne("legacy_character_userinfo253_state", []string{"word_a", "word_b", "tail_word_a", "tail_word_b", "tail_flag"})
	if !ok {
		return nil
	}
	rows := limitLegacyRowsU16(r.queryRows("legacy_character_userinfo253_rows",
		[]string{"sort_order", "word", "value"},
		[]string{"sort_order"},
		false))
	var w packetWriter
	w.writeUint16(rowU16(state, "word_a", 0))
	w.writeUint16(rowU16(state, "word_b", 0))
	w.writeUint16(uint16(len(rows)))
	for _, row := range rows {
		w.writeUint16(rowU16(row, "word", 0))
		w.writeUint32(rowU32(row, "value", 0))
	}
	w.writeUint16(rowU16(state, "tail_word_a", 0))
	w.writeUint16(rowU16(state, "tail_word_b", 0))
	w.writeByte(rowU8(state, "tail_flag", 0))
	r.loaded = true
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build254() []byte {
	state, ok := r.rawOne("legacy_character_userinfo254_state", []string{"word_a", "word_b"})
	if !ok {
		return nil
	}
	rows := limitLegacyRowsU16(r.queryRows("legacy_character_userinfo254_rows",
		[]string{"sort_order", "value"},
		[]string{"sort_order"},
		false))
	var w packetWriter
	w.writeUint16(rowU16(state, "word_a", 0))
	w.writeUint16(rowU16(state, "word_b", 0))
	w.writeUint16(uint16(len(rows)))
	for _, row := range rows {
		w.writeUint32(rowU32(row, "value", 0))
	}
	r.loaded = true
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build25B() []byte {
	row, ok := r.rawOne("legacy_character_userinfo25b_state", []string{"flag_a", "value", "flag_b"})
	if !ok {
		return nil
	}
	var w packetWriter
	w.writeByte(rowU8(row, "flag_a", 0))
	w.writeUint32(rowU32(row, "value", 0))
	w.writeByte(rowU8(row, "flag_b", 0))
	r.loaded = true
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build26E() []byte {
	row, ok := r.rawOne("legacy_character_userinfo26e_state", []string{"value", "word"})
	if !ok {
		return nil
	}
	var w packetWriter
	w.writeUint32(rowU32(row, "value", 0))
	w.writeUint16(rowU16(row, "word", 0))
	r.loaded = true
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build275() []byte {
	row, ok := r.rawOne("legacy_character_userinfo275_state", []string{"word_a", "word_b", "word_c", "word_d", "value_a", "value_b"})
	if !ok {
		return nil
	}
	var w packetWriter
	w.writeUint16(rowU16(row, "word_a", 0))
	w.writeUint16(rowU16(row, "word_b", 0))
	w.writeUint16(rowU16(row, "word_c", 0))
	w.writeUint16(rowU16(row, "word_d", 0))
	w.writeUint32(rowU32(row, "value_a", 0))
	w.writeUint32(rowU32(row, "value_b", 0))
	r.loaded = true
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build276() []byte {
	row, ok := r.rawOne("legacy_character_userinfo276_state", []string{"byte_a", "byte_b", "word", "value"})
	if !ok {
		return nil
	}
	var w packetWriter
	w.writeByte(rowU8(row, "byte_a", 0))
	w.writeByte(rowU8(row, "byte_b", 0))
	w.writeUint16(rowU16(row, "word", 0))
	w.writeUint32(rowU32(row, "value", 0))
	r.loaded = true
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build287() []byte {
	if _, ok := r.rawOne("legacy_character_userinfo287_state", []string{"note"}); !ok {
		return nil
	}
	rows := limitLegacyRowsU8(r.queryRows("legacy_character_userinfo287_rows",
		[]string{"sort_order", "name", "byte_a", "byte_b", "packed_flag", "value_a", "value_b", "text", "value_c", "value_d", "word", "byte_c"},
		[]string{"sort_order"},
		false))
	extraRows := r.queryRows("legacy_character_userinfo287_extras",
		[]string{"row_sort_order", "sort_order", "extra_index", "value"},
		[]string{"row_sort_order", "sort_order"},
		false)
	extrasByRow := legacyRowsByKey(extraRows, "row_sort_order")
	var w packetWriter
	w.writeByte(byte(len(rows)))
	for _, row := range rows {
		writeWideNullTerminatedStringMax(&w, rowString(row, "name", ""), 31)
		w.writeByte(rowU8(row, "byte_a", 0))
		w.writeByte(rowU8(row, "byte_b", 0))
		w.writeByte(rowU8(row, "packed_flag", 0))
		w.writeUint32(rowU32(row, "value_a", 0))
		w.writeUint32(rowU32(row, "value_b", 0))
		writeWideNullTerminatedStringMax(&w, rowString(row, "text", ""), 30)
		w.writeUint32(rowU32(row, "value_c", 0))
		w.writeUint32(rowU32(row, "value_d", 0))
		w.writeUint16(rowU16(row, "word", 0))
		w.writeByte(rowU8(row, "byte_c", 0))
		extras := limitLegacyRowsU8(extrasByRow[legacyGroupKey(rowInt(row, "sort_order", 0))])
		w.writeByte(byte(len(extras)))
		for _, extra := range extras {
			w.writeByte(rowU8(extra, "extra_index", 0))
			w.writeUint32(rowU32(extra, "value", 0))
		}
	}
	r.loaded = true
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build28A() []byte {
	state, ok := r.rawOne("legacy_character_userinfo28a_state", []string{"tail_value_a", "tail_value_b"})
	if !ok {
		return nil
	}
	rows := limitLegacyRowsU8(r.queryRows("legacy_character_userinfo28a_rows",
		[]string{"sort_order", "row_type", "text", "word", "byte_a", "packed_flag", "value_a", "value_b"},
		[]string{"sort_order"},
		false))
	var w packetWriter
	w.writeByte(byte(len(rows)))
	for _, row := range rows {
		w.writeByte(rowU8(row, "row_type", 0))
		writeWideNullTerminatedStringMax(&w, rowString(row, "text", ""), 128)
		w.writeUint16(rowU16(row, "word", 0))
		w.writeByte(rowU8(row, "byte_a", 0))
		w.writeByte(rowU8(row, "packed_flag", 0))
		w.writeUint32(rowU32(row, "value_a", 0))
		w.writeUint32(rowU32(row, "value_b", 0))
	}
	w.writeUint32(rowU32(state, "tail_value_a", 0))
	w.writeUint32(rowU32(state, "tail_value_b", 0))
	r.loaded = true
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build28B() []byte {
	row, ok := r.rawOne("legacy_character_userinfo28b_state", []string{"word", "flag"})
	if !ok {
		return nil
	}
	var w packetWriter
	w.writeUint16(rowU16(row, "word", 0))
	w.writeByte(rowU8(row, "flag", 0))
	r.loaded = true
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build29F() []byte {
	if _, ok := r.rawOne("legacy_character_userinfo29f_state", []string{"note"}); !ok {
		return nil
	}
	rows := limitLegacyRowsU8(r.queryRows("legacy_character_userinfo29f_rows",
		[]string{"sort_order", "word", "category", "value"},
		[]string{"sort_order"},
		false))
	var w packetWriter
	w.writeByte(byte(len(rows)))
	for _, row := range rows {
		w.writeUint16(rowU16(row, "word", 0))
		w.writeByte(rowU8(row, "category", 0))
		w.writeUint32(rowU32(row, "value", 0))
	}
	r.loaded = true
	return w.bytes()
}
