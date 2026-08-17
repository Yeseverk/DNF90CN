package dnfbridge

import (
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	"strconv"
	"strings"
	"unicode/utf16"
)

func (r *csharpLegacyUserInfoReader) rows(table string, columns []string, order []string) []dnfrepo.LegacyUserInfoRow {
	return r.queryRows(table, columns, order, true)
}

func (r *csharpLegacyUserInfoReader) queryRows(table string, columns []string, order []string, markLoaded bool) []dnfrepo.LegacyUserInfoRow {
	if r == nil || r.repo == nil {
		return nil
	}
	rows, err := r.repo.SelectRows(r.ctx, r.characterID, table, columns, order)
	if err != nil {
		r.logQueryError(table, err)
		return nil
	}
	if markLoaded && len(rows) > 0 {
		r.loaded = true
	}
	return rows
}

func (r *csharpLegacyUserInfoReader) one(table string, columns []string) dnfrepo.LegacyUserInfoRow {
	if r == nil || r.repo == nil {
		return nil
	}
	row, ok, err := r.repo.SelectOne(r.ctx, r.characterID, table, columns)
	if err != nil {
		r.logQueryError(table, err)
		return nil
	}
	if !ok {
		return nil
	}
	r.loaded = true
	return row
}

func (r *csharpLegacyUserInfoReader) logQueryError(table string, err error) {
	if r.service != nil && r.session != nil {
		r.service.logGameEvent(r.session, "game-upper-select-legacy-userinfo-load-failed", "character_id", r.characterID, "table", table, "error", err)
	}
}

func (r *csharpLegacyUserInfoReader) buildOneU8(table string, column string) []byte {
	row := r.one(table, []string{column})
	var w packetWriter
	w.writeByte(rowU8(row, column, 0))
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) buildOneU16(table string, column string) []byte {
	row := r.one(table, []string{column})
	var w packetWriter
	w.writeUint16(rowU16(row, column, 0))
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) buildOneU32(table string, column string) []byte {
	row := r.one(table, []string{column})
	var w packetWriter
	w.writeUint32(rowU32(row, column, 0))
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) buildTwoU32(table string, a string, b string) []byte {
	row := r.one(table, []string{a, b})
	var w packetWriter
	w.writeUint32(rowU32(row, a, 0))
	w.writeUint32(rowU32(row, b, 0))
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) buildConditionalOneU16(table string, column string) []byte {
	row, ok := r.rawOne(table, []string{column})
	if !ok {
		return nil
	}
	var w packetWriter
	w.writeUint16(rowU16(row, column, 0))
	r.loaded = true
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) buildConditionalOneU32(table string, column string) []byte {
	row, ok := r.rawOne(table, []string{column})
	if !ok {
		return nil
	}
	var w packetWriter
	w.writeUint32(rowU32(row, column, 0))
	r.loaded = true
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) buildOneU8IfPresent(table string, column string) []byte {
	row, ok := r.rawOne(table, []string{column})
	if !ok {
		return nil
	}
	var w packetWriter
	w.writeByte(rowU8(row, column, 0))
	r.loaded = true
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) buildOneU32IfPresent(table string, column string) []byte {
	row, ok := r.rawOne(table, []string{column})
	if !ok {
		return nil
	}
	var w packetWriter
	w.writeUint32(rowU32(row, column, 0))
	r.loaded = true
	return w.bytes()
}

func rowString(row dnfrepo.LegacyUserInfoRow, column string, fallback string) string {
	if row == nil {
		return fallback
	}
	if value, ok := row[column]; ok {
		return value
	}
	return fallback
}

func rowBytes(row dnfrepo.LegacyUserInfoRow, column string) []byte {
	if row == nil {
		return nil
	}
	value, ok := row[column]
	if !ok {
		return nil
	}
	return []byte(value)
}

func rowU8(row dnfrepo.LegacyUserInfoRow, column string, fallback byte) byte {
	value := rowUint(row, column, uint64(fallback), 8)
	return byte(value)
}

func rowU16(row dnfrepo.LegacyUserInfoRow, column string, fallback uint16) uint16 {
	value := rowUint(row, column, uint64(fallback), 16)
	return uint16(value)
}

func rowU32(row dnfrepo.LegacyUserInfoRow, column string, fallback uint32) uint32 {
	value := rowUint(row, column, uint64(fallback), 32)
	return uint32(value)
}

func rowInt(row dnfrepo.LegacyUserInfoRow, column string, fallback int) int {
	if row == nil {
		return fallback
	}
	value := strings.TrimSpace(row[column])
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fallback
	}
	return int(parsed)
}

func rowUint(row dnfrepo.LegacyUserInfoRow, column string, fallback uint64, bits int) uint64 {
	if row == nil {
		return fallback
	}
	value := strings.TrimSpace(row[column])
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseUint(value, 10, bits)
	if err != nil {
		return fallback
	}
	return parsed
}

func writeWideNullTerminatedString(w *packetWriter, value string) {
	units := utf16.Encode([]rune(value))
	if len(units) > 255 {
		units = units[:255]
	}
	for _, unit := range units {
		w.writeUint16(unit)
	}
	w.writeUint16(0)
}

func writeWideNullTerminatedStringMax(w *packetWriter, value string, maxChars int) {
	units := utf16.Encode([]rune(value))
	// C# 参考 builder 会拒绝超长字段；Go 运行期这里截断库内文本，保证旧客户端包体长度有界。
	if maxChars > 0 && len(units) >= maxChars {
		units = units[:maxChars-1]
	}
	for _, unit := range units {
		w.writeUint16(unit)
	}
	w.writeUint16(0)
}
