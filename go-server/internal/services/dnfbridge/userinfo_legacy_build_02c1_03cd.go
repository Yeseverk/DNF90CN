package dnfbridge

import (
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	"strings"
)

func (r *csharpLegacyUserInfoReader) build2C1() []byte {
	row, ok := r.rawOne("legacy_character_userinfo2c1_state", []string{"value_a", "value_b"})
	if !ok {
		return nil
	}
	var w packetWriter
	w.writeUint32(rowU32(row, "value_a", 0))
	w.writeUint32(rowU32(row, "value_b", 0))
	r.loaded = true
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build2D2() []byte {
	state, ok := r.rawOne("legacy_character_userinfo2d2_state", []string{"flag", "value_a", "value_b", "value_c"})
	if !ok {
		return nil
	}
	groups := r.queryRows("legacy_character_userinfo2d2_groups",
		[]string{"sort_order", "group_key", "word_a", "word_b"},
		[]string{"sort_order"},
		false)
	rows := r.queryRows("legacy_character_userinfo2d2_rows",
		[]string{"group_sort_order", "sort_order", "word", "value_a", "value_b"},
		[]string{"group_sort_order", "sort_order"},
		false)
	pairs := r.queryRows("legacy_character_userinfo2d2_pairs",
		[]string{"group_sort_order", "row_sort_order", "pair_kind", "sort_order", "word", "flag"},
		[]string{"group_sort_order", "row_sort_order", "pair_kind", "sort_order"},
		false)
	rowsByGroup := legacyRowsByKey(rows, "group_sort_order")
	pairsByRow := legacyRowsByKey(pairs, "group_sort_order", "row_sort_order")

	var w packetWriter
	w.writeByte(rowU8(state, "flag", 0))
	w.writeUint32(rowU32(state, "value_a", 0))
	w.writeUint32(rowU32(state, "value_b", 0))
	w.writeUint32(rowU32(state, "value_c", 0))
	w.writeUint32(uint32(len(groups)))
	for _, group := range groups {
		groupSort := rowInt(group, "sort_order", 0)
		groupRows := rowsByGroup[legacyGroupKey(groupSort)]
		w.writeUint32(rowU32(group, "group_key", 0))
		w.writeUint16(rowU16(group, "word_a", 0))
		w.writeUint16(rowU16(group, "word_b", 0))
		w.writeUint32(uint32(len(groupRows)))
		for _, row := range groupRows {
			rowSort := rowInt(row, "sort_order", 0)
			rowPairs := pairsByRow[legacyGroupKey(groupSort, rowSort)]
			w.writeUint16(rowU16(row, "word", 0))
			w.writeUint32(rowU32(row, "value_a", 0))
			w.writeUint32(rowU32(row, "value_b", 0))
			write2D2Pairs(&w, rowPairs, "first")
			write2D2Pairs(&w, rowPairs, "second")
		}
	}
	r.loaded = true
	return w.bytes()
}

func write2D2Pairs(w *packetWriter, rows []dnfrepo.LegacyUserInfoRow, kind string) {
	filtered := make([]dnfrepo.LegacyUserInfoRow, 0, len(rows))
	for _, row := range rows {
		if strings.EqualFold(rowString(row, "pair_kind", ""), kind) {
			filtered = append(filtered, row)
		}
	}
	filtered = limitLegacyRowsU16(filtered)
	w.writeUint16(uint16(len(filtered)))
	for _, row := range filtered {
		w.writeUint16(rowU16(row, "word", 0))
		w.writeByte(rowU8(row, "flag", 0))
	}
}

func (r *csharpLegacyUserInfoReader) build324() []byte {
	row, ok := r.rawOne("legacy_character_userinfo324_state", []string{"byte_a", "text", "byte_b", "byte_c"})
	if !ok {
		return nil
	}
	var w packetWriter
	w.writeByte(rowU8(row, "byte_a", 0))
	writeWideNullTerminatedStringMax(&w, rowString(row, "text", ""), 50)
	w.writeByte(rowU8(row, "byte_b", 0))
	w.writeByte(rowU8(row, "byte_c", 0))
	r.loaded = true
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build336() []byte {
	if _, ok := r.rawOne("legacy_character_userinfo336_state", []string{"note"}); !ok {
		return nil
	}
	rows := limitLegacyRowsU8(r.queryRows("legacy_character_userinfo336_rows",
		[]string{"sort_order", "byte_a", "text", "byte_b", "byte_c", "packed_flag", "byte_d"},
		[]string{"sort_order"},
		false))
	var w packetWriter
	w.writeByte(byte(len(rows)))
	for _, row := range rows {
		w.writeByte(rowU8(row, "byte_a", 0))
		writeWideNullTerminatedStringMax(&w, rowString(row, "text", ""), 30)
		w.writeByte(rowU8(row, "byte_b", 0))
		w.writeByte(rowU8(row, "byte_c", 0))
		w.writeByte(rowU8(row, "packed_flag", 0))
		w.writeByte(rowU8(row, "byte_d", 0))
	}
	r.loaded = true
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build34CText() []byte {
	row, ok := r.rawOne("legacy_character_userinfo34c_text_state", []string{"category", "text"})
	if !ok {
		return nil
	}
	var w packetWriter
	w.writeByte(rowU8(row, "category", 0))
	writeWideNullTerminatedStringMax(&w, rowString(row, "text", ""), 64)
	r.loaded = true
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build34DValue() []byte {
	return r.buildOneU32IfPresent("legacy_character_userinfo34d_value_state", "value")
}

func (r *csharpLegacyUserInfoReader) build34EByte() []byte {
	return r.buildOneU8IfPresent("legacy_character_userinfo34e_byte_state", "value")
}

func (r *csharpLegacyUserInfoReader) build352() []byte {
	row, ok := r.rawOne("legacy_character_userinfo352_state", []string{"value_a", "value_b"})
	if !ok {
		return nil
	}
	var w packetWriter
	w.writeUint32(rowU32(row, "value_a", 0))
	w.writeUint32(rowU32(row, "value_b", 0))
	r.loaded = true
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build354() []byte {
	row, ok := r.rawOne("legacy_character_userinfo354_state", []string{"word0", "word1", "word2", "word3", "word4"})
	if !ok {
		return nil
	}
	var w packetWriter
	w.writeUint16(rowU16(row, "word0", 0))
	w.writeUint16(rowU16(row, "word1", 0))
	w.writeUint16(rowU16(row, "word2", 0))
	w.writeUint16(rowU16(row, "word3", 0))
	w.writeUint16(rowU16(row, "word4", 0))
	r.loaded = true
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build359() []byte {
	state, ok := r.rawOne("legacy_character_userinfo359_state", []string{"byte_a", "byte_b"})
	if !ok {
		return nil
	}
	groups := r.queryRows("legacy_character_userinfo359_groups",
		[]string{"sort_order", "group_key", "raw0", "raw1", "raw2"},
		[]string{"sort_order"},
		false)
	var w packetWriter
	w.writeByte(rowU8(state, "byte_a", 0))
	w.writeByte(rowU8(state, "byte_b", 0))
	w.writeUint32(uint32(len(groups)))
	for _, group := range groups {
		raw0 := rowBytes(group, "raw0")
		raw1 := rowBytes(group, "raw1")
		raw2 := rowBytes(group, "raw2")
		if !legacyPayloadLengthOK(raw0, 52) || !legacyPayloadLengthOK(raw1, 52) || !legacyPayloadLengthOK(raw2, 52) {
			return nil
		}
		w.writeUint32(rowU32(group, "group_key", 0))
		w.writeBytes(raw0)
		w.writeBytes(raw1)
		w.writeBytes(raw2)
	}
	r.loaded = true
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build36B() []byte {
	if _, ok := r.rawOne("legacy_character_userinfo36b_state", []string{"note"}); !ok {
		return nil
	}
	rows := limitLegacyRowsU8(r.queryRows("legacy_character_userinfo36b_rows",
		[]string{"sort_order", "kind", "byte_a", "byte_b", "byte_c", "word_a", "value", "byte_d", "word_b"},
		[]string{"sort_order"},
		false))
	var w packetWriter
	w.writeByte(byte(len(rows)))
	for _, row := range rows {
		kind := rowU8(row, "kind", 0)
		w.writeByte(kind)
		w.writeByte(rowU8(row, "byte_a", 0))
		if kind == 0 {
			w.writeByte(rowU8(row, "byte_b", 0))
			w.writeByte(rowU8(row, "byte_c", 0))
			continue
		}
		w.writeUint16(rowU16(row, "word_a", 0))
		w.writeUint32(rowU32(row, "value", 0))
		w.writeByte(rowU8(row, "byte_d", 0))
		w.writeUint16(rowU16(row, "word_b", 0))
	}
	r.loaded = true
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build37B() []byte {
	row, ok := r.rawOne("legacy_character_userinfo37b_state", []string{"value", "raw124", "raw64"})
	if !ok {
		return nil
	}
	raw124 := rowBytes(row, "raw124")
	raw64 := rowBytes(row, "raw64")
	if !legacyPayloadLengthOK(raw124, 124) || !legacyPayloadLengthOK(raw64, 64) {
		return nil
	}
	var w packetWriter
	w.writeUint32(rowU32(row, "value", 0))
	w.writeBytes(raw124)
	w.writeBytes(raw64)
	r.loaded = true
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build393() []byte {
	if _, ok := r.rawOne("legacy_character_userinfo393_state", []string{"note"}); !ok {
		return nil
	}
	rows := limitLegacyRowsU8(r.queryRows("legacy_character_userinfo393_rows",
		[]string{"sort_order", "value_a", "value_b"},
		[]string{"sort_order"},
		false))
	var w packetWriter
	w.writeByte(byte(len(rows)))
	for _, row := range rows {
		w.writeUint32(rowU32(row, "value_a", 0))
		w.writeUint32(rowU32(row, "value_b", 0))
	}
	r.loaded = true
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build3CD() []byte {
	row, ok := r.rawOne("legacy_character_userinfo3cd_state", []string{"flag", "value"})
	if !ok {
		return nil
	}
	var w packetWriter
	w.writeByte(rowU8(row, "flag", 0))
	w.writeUint32(rowU32(row, "value", 0))
	r.loaded = true
	return w.bytes()
}
