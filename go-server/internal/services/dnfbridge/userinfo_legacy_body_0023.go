package dnfbridge

import (
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	"strings"
)

type userInfo23Scalar struct {
	family      string
	scalarIndex int
	sourceOrder int
	value       uint32
}

type userInfo23Pair struct {
	section string
	key     uint32
	value   uint32
}

type userInfo23Fixed struct {
	section string
	slot    int
	value   uint32
}

type userInfoSlotEntry struct {
	groupID  int
	category int
	key      uint32
	value    uint32
}

func buildCSharpLegacyUserInfo23FallbackBody() []byte {
	// 0x23 是当前 USERINFO 主通知。legacy seed 表为空时仍发送空结构，避免选择角色阶段 deferred 后进场景完全缺包。
	reader := csharpLegacyUserInfoReader{}
	return reader.build23()
}

func (r *csharpLegacyUserInfoReader) build23() []byte {
	scalars := r.load23Scalars()
	fixed := r.load23Fixed()
	pairs := r.load23Pairs()
	objects := r.rows("legacy_character_userinfo23_object_rows",
		[]string{"sort_order", "object_or_slot_id", "value_a", "value_b", "value_c", "value_d", "value_e"},
		[]string{"sort_order"})
	control := r.one("legacy_character_userinfo_slot_control",
		[]string{"group0_tail_refresh_value", "mode_bits", "tail_value_a", "tail_flag_a", "tail_bool_b", "tail_value_b", "tail_pair_a", "tail_pair_b", "tail_index6_value"})
	slotEntries := r.loadSlotEntries()

	var w packetWriter
	write23U32(&w, scalars, 1, pick23(scalars, 0, ref23("direct", 401)))
	write23U32(&w, scalars, 2, pick23(scalars, 0, ref23("e90", 1)))
	write23U32(&w, scalars, 3, pick23(scalars, 0, ref23("e90", 2)))
	write23U32(&w, scalars, 4, pick23(scalars, 0, ref23("e90", 3), ref23("db0", 0)))
	write23U8(&w, scalars, 5, pick23(scalars, 0, ref23("direct", 399)))
	write23U32(&w, scalars, 6, pick23(scalars, 0, ref23("e90", 6), ref23("db0", 9)))
	write23U32(&w, scalars, 7, pick23(scalars, 0, ref23("direct", 397), ref23("db0", 7)))
	write23U32(&w, scalars, 8, pick23(scalars, 0, ref23("e90", 5), ref23("db0", 2)))
	write23U32(&w, scalars, 9, pick23(scalars, 0, ref23("e90", 9), ref23("db0", 13)))
	write23U32(&w, scalars, 10, pick23(scalars, 0, ref23("direct", 398)))
	write23U32(&w, scalars, 11, pick23(scalars, 0, ref23("e90", 4), ref23("db0", 1)))
	write23U32(&w, scalars, 12, pick23(scalars, 0, ref23("e90", 8)))
	// C# UserInfo23BodyBuilder 是当前 0x0023 的字节顺序证据；这里不能沿用旧提取的 d90/db0 顺序。
	write23U32(&w, scalars, 13, pick23(scalars, 0, ref23("e90", 10), ref23("db0", 6)))
	write23U32(&w, scalars, 14, pick23(scalars, 0, ref23("e90", 11)))
	write23U32(&w, scalars, 15, pick23(scalars, 0, ref23("e90", 12), ref23("db0", 10)))
	write23U32(&w, scalars, 16, pick23(scalars, 0, ref23("e90", 13)))
	write23U32(&w, scalars, 17, pick23(scalars, 0, ref23("e90", 14)))
	write23U32(&w, scalars, 18, pick23(scalars, 0, ref23("db0", 5)))
	write23U32(&w, scalars, 19, pick23(scalars, 0, ref23("d90", 2)))
	write23U32(&w, scalars, 20, pick23(scalars, 0, ref23("d90", 10)))
	write23U32(&w, scalars, 21, computeAttr268Index7Remainder(scalars))
	write23U32(&w, scalars, 22, pick23(scalars, 0, ref23("d90", 4)))
	write23U32(&w, scalars, 23, pick23(scalars, 0, ref23("d90", 5)))
	write23U32(&w, scalars, 24, pick23(scalars, 0, ref23("d90", 8)))
	write23U32(&w, scalars, 25, pick23(scalars, 0, ref23("d90", 9)))
	write23U32(&w, scalars, 26, pick23(scalars, 0, ref23("d90", 6)))
	write23U32(&w, scalars, 27, pick23(scalars, 0, ref23("d90", 3)))
	write23U32(&w, scalars, 28, 0)
	write23U32(&w, scalars, 29, pick23(scalars, 0, ref23("d90", 0)))
	write23U32(&w, scalars, 30, pick23(scalars, 0, ref23("d90", 1)))
	write23PairSectionU8(&w, scalars, pairs, "d90_ext", ref23("d90", 11))
	write23PairSectionU8(&w, scalars, pairs, "db0_ext", ref23("db0", 11))
	write23U32(&w, scalars, 31, pick23(scalars, 0, ref23("e90", 16)))
	write23U32(&w, scalars, 32, pick23(scalars, 0, ref23("e90", 17)))
	write23U32(&w, scalars, 33, pick23(scalars, 0, ref23("e90", 18)))
	write23U32(&w, scalars, 34, pick23(scalars, 0, ref23("e90", 19)))
	write23U32(&w, scalars, 35, pick23(scalars, 0, ref23("e90", 20), ref23("db0", 14)))
	write23U32(&w, scalars, 36, pick23(scalars, 0, ref23("d90", 13)))
	write23U32(&w, scalars, 37, pick23(scalars, 0, ref23("e90", 21), ref23("db0", 15)))
	write23U32(&w, scalars, 38, pick23(scalars, 0, ref23("e90", 22)))
	for i := 0; i < 4; i++ {
		w.writeUint32(fixed23(fixed, "fixed4", i))
	}
	write23PairSectionU32(&w, pairs, "map")
	write23ObjectRows(&w, objects)
	write23SlotGroup(&w, slotEntries, 0)
	w.writeUint32(rowU32(control, "group0_tail_refresh_value", 0))
	write23SlotGroup(&w, slotEntries, 1)
	write23SlotGroup(&w, slotEntries, 2)
	write23U32(&w, scalars, 39, rowU32(control, "mode_bits", 0))
	write23U32(&w, scalars, 40, rowU32(control, "tail_value_a", 0))
	write23U8(&w, scalars, 41, uint32(rowU8(control, "tail_flag_a", 0)))
	write23U8(&w, scalars, 42, uint32(rowU8(control, "tail_bool_b", 0)))
	write23U32(&w, scalars, 43, rowU32(control, "tail_value_b", 0))
	write23U32(&w, scalars, 44, rowU32(control, "tail_pair_a", 0))
	write23U32(&w, scalars, 45, rowU32(control, "tail_pair_b", 0))
	write23U32(&w, scalars, 46, rowU32(control, "tail_index6_value", 0))
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) load23Scalars() []userInfo23Scalar {
	rows := r.rows("legacy_character_userinfo23_scalar_values",
		[]string{"family", "scalar_index", "source_order", "value"},
		[]string{"source_order"})
	out := make([]userInfo23Scalar, 0, len(rows))
	for _, row := range rows {
		out = append(out, userInfo23Scalar{
			family:      strings.ToLower(rowString(row, "family", "")),
			scalarIndex: rowInt(row, "scalar_index", 0),
			sourceOrder: rowInt(row, "source_order", 0),
			value:       rowU32(row, "value", 0),
		})
	}
	return out
}

func (r *csharpLegacyUserInfoReader) load23Fixed() []userInfo23Fixed {
	rows := r.rows("legacy_character_userinfo23_fixed_values",
		[]string{"section", "slot_index", "value"},
		[]string{"section", "slot_index"})
	out := make([]userInfo23Fixed, 0, len(rows))
	for _, row := range rows {
		out = append(out, userInfo23Fixed{
			section: strings.ToLower(rowString(row, "section", "")),
			slot:    rowInt(row, "slot_index", 0),
			value:   rowU32(row, "value", 0),
		})
	}
	return out
}

func (r *csharpLegacyUserInfoReader) load23Pairs() []userInfo23Pair {
	rows := r.rows("legacy_character_userinfo23_pair_entries",
		[]string{"section", "entry_key", "value"},
		[]string{"section", "sort_order"})
	out := make([]userInfo23Pair, 0, len(rows))
	for _, row := range rows {
		out = append(out, userInfo23Pair{
			section: strings.ToLower(rowString(row, "section", "")),
			key:     rowU32(row, "entry_key", 0),
			value:   rowU32(row, "value", 0),
		})
	}
	return out
}

func (r *csharpLegacyUserInfoReader) loadSlotEntries() []userInfoSlotEntry {
	rows := r.rows("legacy_character_userinfo_slot_group_entries",
		[]string{"group_id", "category", "key_or_item_id", "value"},
		[]string{"group_id", "category", "sort_order"})
	out := make([]userInfoSlotEntry, 0, len(rows))
	for _, row := range rows {
		out = append(out, userInfoSlotEntry{
			groupID:  rowInt(row, "group_id", 0),
			category: rowInt(row, "category", 0),
			key:      rowU32(row, "key_or_item_id", 0),
			value:    rowU32(row, "value", 0),
		})
	}
	return out
}

type scalarRef23 struct {
	family string
	index  int
}

func ref23(family string, index int) scalarRef23 {
	return scalarRef23{family: strings.ToLower(family), index: index}
}

func write23U32(w *packetWriter, scalars []userInfo23Scalar, order int, fallback uint32) {
	w.writeUint32(valueByOrder23(scalars, order, fallback))
}

func write23U8(w *packetWriter, scalars []userInfo23Scalar, order int, fallback uint32) {
	value := valueByOrder23(scalars, order, fallback)
	if value > 0xff {
		value = 0xff
	}
	w.writeByte(byte(value))
}

func valueByOrder23(scalars []userInfo23Scalar, order int, fallback uint32) uint32 {
	for _, scalar := range scalars {
		if scalar.sourceOrder == order {
			return scalar.value
		}
	}
	return fallback
}

func pick23(scalars []userInfo23Scalar, fallback uint32, refs ...scalarRef23) uint32 {
	for _, ref := range refs {
		if value, ok := scalar23(scalars, ref.family, ref.index); ok {
			return value
		}
	}
	return fallback
}

func scalar23(scalars []userInfo23Scalar, family string, index int) (uint32, bool) {
	family = strings.ToLower(family)
	for _, scalar := range scalars {
		if scalar.family == family && scalar.scalarIndex == index {
			return scalar.value, true
		}
	}
	return 0, false
}

func computeAttr268Index7Remainder(scalars []userInfo23Scalar) uint32 {
	if value, ok := valueByOrder23OK(scalars, 21); ok {
		return value
	}
	total, ok := scalar23(scalars, "d90", 7)
	if !ok {
		return 0
	}
	first := pick23(scalars, 0, ref23("e90", 11))
	if total > first {
		return total - first
	}
	return 0
}

func valueByOrder23OK(scalars []userInfo23Scalar, order int) (uint32, bool) {
	for _, scalar := range scalars {
		if scalar.sourceOrder == order {
			return scalar.value, true
		}
	}
	return 0, false
}

func fixed23(values []userInfo23Fixed, section string, slot int) uint32 {
	section = strings.ToLower(section)
	for _, value := range values {
		if value.section == section && value.slot == slot {
			return value.value
		}
	}
	return 0
}

func write23PairSectionU8(w *packetWriter, scalars []userInfo23Scalar, pairs []userInfo23Pair, section string, synthetic scalarRef23) {
	entries := collect23Pairs(pairs, section)
	if len(entries) == 0 {
		if value, ok := scalar23(scalars, synthetic.family, synthetic.index); ok && value != 0 {
			entries = append(entries, userInfo23Pair{key: 1, value: value})
		}
	}
	if len(entries) > 0xff {
		entries = entries[:0xff]
	}
	w.writeByte(byte(len(entries)))
	for _, entry := range entries {
		w.writeUint32(entry.key)
		w.writeUint32(entry.value)
	}
}

func write23PairSectionU32(w *packetWriter, pairs []userInfo23Pair, section string) {
	entries := collect23Pairs(pairs, section)
	w.writeUint32(uint32(len(entries)))
	for _, entry := range entries {
		w.writeUint32(entry.key)
		w.writeUint32(entry.value)
	}
}

func collect23Pairs(pairs []userInfo23Pair, section string) []userInfo23Pair {
	section = strings.ToLower(section)
	out := make([]userInfo23Pair, 0)
	for _, entry := range pairs {
		if entry.section == section {
			out = append(out, entry)
		}
	}
	return out
}

func write23ObjectRows(w *packetWriter, rows []dnfrepo.LegacyUserInfoRow) {
	if len(rows) > 0xff {
		rows = rows[:0xff]
	}
	w.writeByte(byte(len(rows)))
	for _, row := range rows {
		w.writeUint16(rowU16(row, "object_or_slot_id", 0))
		w.writeUint32(rowU32(row, "value_a", 0))
		w.writeUint32(rowU32(row, "value_b", 0))
		w.writeUint16(rowU16(row, "value_c", 0))
		w.writeByte(rowU8(row, "value_d", 0))
		w.writeUint16(rowU16(row, "value_e", 0))
	}
}

func write23SlotGroup(w *packetWriter, entries []userInfoSlotEntry, groupID int) {
	for category := 0; category < 8; category++ {
		group := collectSlotEntries(entries, groupID, category)
		if len(group) > 0xff {
			group = group[:0xff]
		}
		w.writeByte(byte(len(group)))
		for _, entry := range group {
			w.writeUint32(entry.key)
			w.writeUint32(entry.value)
		}
	}
}

func collectSlotEntries(entries []userInfoSlotEntry, groupID int, category int) []userInfoSlotEntry {
	out := make([]userInfoSlotEntry, 0)
	for _, entry := range entries {
		if entry.groupID == groupID && entry.category == category {
			out = append(out, entry)
		}
	}
	return out
}
