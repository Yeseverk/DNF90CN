package dnfbridge

func (r *csharpLegacyUserInfoReader) build1BF() []byte {
	control := r.one("legacy_character_userinfo1bf_control", []string{"refresh_flag"})
	groups := limitLegacyRowsU8(r.rows("legacy_character_userinfo1bf_groups",
		[]string{"sort_order", "context_state"},
		[]string{"sort_order"}))
	selectors := r.rows("legacy_character_userinfo1bf_selectors",
		[]string{"group_sort_order", "sort_order", "selector"},
		[]string{"group_sort_order", "sort_order"})
	values := r.rows("legacy_character_userinfo1bf_values",
		[]string{"group_sort_order", "selector_sort_order", "sort_order", "value"},
		[]string{"group_sort_order", "selector_sort_order", "sort_order"})
	selectorsByGroup := legacyRowsByKey(selectors, "group_sort_order")
	valuesBySelector := legacyRowsByKey(values, "group_sort_order", "selector_sort_order")

	var w packetWriter
	w.writeByte(rowU8(control, "refresh_flag", 0))
	w.writeByte(byte(len(groups)))
	for _, group := range groups {
		groupSort := rowInt(group, "sort_order", 0)
		groupSelectors := limitLegacyRowsU8(selectorsByGroup[legacyGroupKey(groupSort)])
		w.writeByte(rowU8(group, "context_state", 0))
		w.writeByte(byte(len(groupSelectors)))
		for _, selector := range groupSelectors {
			selectorSort := rowInt(selector, "sort_order", 0)
			selectorValues := limitLegacyRowsU8(valuesBySelector[legacyGroupKey(groupSort, selectorSort)])
			w.writeUint16(rowU16(selector, "selector", 0))
			w.writeByte(byte(len(selectorValues)))
			for _, value := range selectorValues {
				w.writeUint16(rowU16(value, "value", 0))
			}
		}
	}
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build327() []byte {
	rows := limitLegacyRowsU8(r.rows("legacy_character_userinfo327_blobs",
		[]string{"blob_key", "payload"},
		[]string{"sort_order"}))
	var w packetWriter
	w.writeByte(byte(len(rows)))
	for _, row := range rows {
		payload := rowBytes(row, "payload")
		w.writeByte(rowU8(row, "blob_key", 0))
		w.writeUint32(uint32(len(payload)))
		w.writeBytes(payload)
	}
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build329() []byte {
	targets := limitLegacyRowsU8(r.rows("legacy_character_userinfo329_targets",
		[]string{"sort_order", "target_key", "refresh_flag"},
		[]string{"sort_order"}))
	groups := r.rows("legacy_character_userinfo329_groups",
		[]string{"target_sort_order", "sort_order", "context_state"},
		[]string{"target_sort_order", "sort_order"})
	selectors := r.rows("legacy_character_userinfo329_selectors",
		[]string{"target_sort_order", "group_sort_order", "sort_order", "selector"},
		[]string{"target_sort_order", "group_sort_order", "sort_order"})
	values := r.rows("legacy_character_userinfo329_values",
		[]string{"target_sort_order", "group_sort_order", "selector_sort_order", "sort_order", "value"},
		[]string{"target_sort_order", "group_sort_order", "selector_sort_order", "sort_order"})
	groupsByTarget := legacyRowsByKey(groups, "target_sort_order")
	selectorsByGroup := legacyRowsByKey(selectors, "target_sort_order", "group_sort_order")
	valuesBySelector := legacyRowsByKey(values, "target_sort_order", "group_sort_order", "selector_sort_order")

	var w packetWriter
	w.writeByte(byte(len(targets)))
	for _, target := range targets {
		targetSort := rowInt(target, "sort_order", 0)
		targetGroups := limitLegacyRowsU8(groupsByTarget[legacyGroupKey(targetSort)])
		w.writeByte(rowU8(target, "target_key", 0))
		w.writeByte(rowU8(target, "refresh_flag", 0))
		w.writeByte(byte(len(targetGroups)))
		for _, group := range targetGroups {
			groupSort := rowInt(group, "sort_order", 0)
			groupSelectors := limitLegacyRowsU8(selectorsByGroup[legacyGroupKey(targetSort, groupSort)])
			w.writeByte(rowU8(group, "context_state", 0))
			w.writeByte(byte(len(groupSelectors)))
			for _, selector := range groupSelectors {
				selectorSort := rowInt(selector, "sort_order", 0)
				selectorValues := limitLegacyRowsU8(valuesBySelector[legacyGroupKey(targetSort, groupSort, selectorSort)])
				w.writeUint16(rowU16(selector, "selector", 0))
				w.writeByte(byte(len(selectorValues)))
				for _, value := range selectorValues {
					w.writeUint16(rowU16(value, "value", 0))
				}
			}
		}
	}
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build34B() []byte {
	row := r.one("legacy_character_userinfo34b_state", []string{"value_a", "word", "state", "flag"})
	var w packetWriter
	w.writeUint32(rowU32(row, "value_a", 0))
	w.writeUint16(rowU16(row, "word", 0))
	w.writeByte(rowU8(row, "state", 0))
	w.writeByte(rowU8(row, "flag", 0))
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build34C() []byte {
	control := r.one("legacy_character_userinfo34c_control", []string{"word", "value_a", "value_b"})
	first := limitLegacyRowsU8(r.rows("legacy_character_userinfo34c_first_rows",
		[]string{"key", "word", "value", "flag_a", "flag_b", "flag_c", "flag_d"},
		[]string{"sort_order"}))
	second := limitLegacyRowsU8(r.rows("legacy_character_userinfo34c_second_rows",
		[]string{"row_type", "value_a", "value_b", "value_c", "word_a", "flag", "word_b", "word_c"},
		[]string{"sort_order"}))
	var w packetWriter
	w.writeUint16(rowU16(control, "word", 0))
	w.writeUint32(rowU32(control, "value_a", 0))
	w.writeUint32(rowU32(control, "value_b", 0))
	w.writeByte(byte(len(first)))
	for _, row := range first {
		w.writeUint32(rowU32(row, "key", 0))
		w.writeUint16(rowU16(row, "word", 0))
		w.writeUint32(rowU32(row, "value", 0))
		w.writeByte(rowU8(row, "flag_a", 0))
		w.writeByte(rowU8(row, "flag_b", 0))
		w.writeByte(rowU8(row, "flag_c", 0))
		w.writeByte(rowU8(row, "flag_d", 0))
	}
	w.writeByte(byte(len(second)))
	for _, row := range second {
		w.writeByte(rowU8(row, "row_type", 0))
		w.writeUint32(rowU32(row, "value_a", 0))
		w.writeUint32(rowU32(row, "value_b", 0))
		w.writeUint32(rowU32(row, "value_c", 0))
		w.writeUint16(rowU16(row, "word_a", 0))
		w.writeByte(rowU8(row, "flag", 0))
		w.writeUint16(rowU16(row, "word_b", 0))
		w.writeUint16(rowU16(row, "word_c", 0))
	}
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build34D() []byte {
	control := r.one("legacy_character_userinfo34d_control", []string{"value"})
	rows := r.rows("legacy_character_userinfo34d_rows",
		[]string{"group_index", "value_a", "value_b"},
		[]string{"group_index", "sort_order"})
	rowsByGroup := legacyRowsByKey(rows, "group_index")
	var w packetWriter
	w.writeUint32(rowU32(control, "value", 0))
	for groupIndex := 0; groupIndex < 8; groupIndex++ {
		groupRows := limitLegacyRowsU8(rowsByGroup[legacyGroupKey(groupIndex)])
		w.writeByte(byte(len(groupRows)))
		for _, row := range groupRows {
			w.writeUint32(rowU32(row, "value_a", 0))
			w.writeUint32(rowU32(row, "value_b", 0))
		}
	}
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build34E() []byte {
	return r.buildOneU8("legacy_character_userinfo34e_state", "state")
}

func (r *csharpLegacyUserInfoReader) build2A9() []byte {
	state, ok := r.rawOne("legacy_character_userinfo2a9_state", []string{"header_word"})
	if !ok {
		return nil
	}
	rows := limitLegacyRowsU8(r.queryRows("legacy_character_userinfo2a9_rows",
		[]string{"sort_order", "byte_a", "word_a", "value", "word_b"},
		[]string{"sort_order"},
		false))
	values := limitLegacyRowsU8(r.queryRows("legacy_character_userinfo2a9_values",
		[]string{"sort_order", "value"},
		[]string{"sort_order"},
		false))
	var w packetWriter
	w.writeUint16(rowU16(state, "header_word", 0))
	w.writeByte(byte(len(rows)))
	for _, row := range rows {
		w.writeByte(rowU8(row, "byte_a", 0))
		w.writeUint16(rowU16(row, "word_a", 0))
		w.writeUint32(rowU32(row, "value", 0))
		w.writeUint16(rowU16(row, "word_b", 0))
	}
	w.writeByte(byte(len(values)))
	for _, value := range values {
		w.writeByte(rowU8(value, "value", 0))
	}
	r.loaded = true
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build2AA() []byte {
	row, ok := r.rawOne("legacy_character_userinfo2aa_state", []string{"value"})
	if !ok {
		return nil
	}
	var w packetWriter
	w.writeUint16(rowU16(row, "value", 0))
	r.loaded = true
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build2B0() []byte {
	row, ok := r.rawOne("legacy_character_userinfo2b0_state", []string{"key", "value_a", "value_b", "value_c"})
	if !ok {
		return nil
	}
	var w packetWriter
	w.writeUint32(rowU32(row, "key", 0))
	w.writeUint32(rowU32(row, "value_a", 0))
	w.writeUint32(rowU32(row, "value_b", 0))
	w.writeUint32(rowU32(row, "value_c", 0))
	r.loaded = true
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build2BC() []byte {
	return r.buildOneU8IfPresent("legacy_character_userinfo2bc_state", "value")
}
