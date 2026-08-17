package dnfbridge

import (
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	"strconv"
	"strings"
)

func legacyPayloadLengthOK(payload []byte, expectedLength int) bool {
	return len(payload) == expectedLength
}

func limitLegacyRowsU8(rows []dnfrepo.LegacyUserInfoRow) []dnfrepo.LegacyUserInfoRow {
	if len(rows) > 0xff {
		return rows[:0xff]
	}
	return rows
}

func limitLegacyRowsU16(rows []dnfrepo.LegacyUserInfoRow) []dnfrepo.LegacyUserInfoRow {
	if len(rows) > 0xffff {
		return rows[:0xffff]
	}
	return rows
}

func legacyRowsByKey(rows []dnfrepo.LegacyUserInfoRow, columns ...string) map[string][]dnfrepo.LegacyUserInfoRow {
	out := make(map[string][]dnfrepo.LegacyUserInfoRow)
	for _, row := range rows {
		values := make([]int, 0, len(columns))
		for _, column := range columns {
			values = append(values, rowInt(row, column, 0))
		}
		key := legacyGroupKey(values...)
		out[key] = append(out[key], row)
	}
	return out
}

func legacyGroupKey(values ...int) string {
	if len(values) == 0 {
		return ""
	}
	var builder strings.Builder
	for idx, value := range values {
		if idx > 0 {
			builder.WriteByte(':')
		}
		builder.WriteString(strconv.Itoa(value))
	}
	return builder.String()
}
