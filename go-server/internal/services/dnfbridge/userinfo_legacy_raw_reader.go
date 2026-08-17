package dnfbridge

import dnfrepo "longheng.io/server/internal/modules/dnf/repository"

func (r *csharpLegacyUserInfoReader) rawOne(table string, columns []string) (dnfrepo.LegacyUserInfoRow, bool) {
	rows := r.queryRows(table, columns, nil, false)
	if len(rows) == 0 {
		return nil, false
	}
	return rows[0], true
}

func (r *csharpLegacyUserInfoReader) rawPayloadRows(table string, groupKind string, expectedLength int) ([][]byte, bool) {
	rows := r.queryRows(table,
		[]string{"group_kind", "sort_order", "payload"},
		[]string{"group_kind", "sort_order"},
		false)
	out := make([][]byte, 0, len(rows))
	for _, row := range rows {
		if rowString(row, "group_kind", "") != groupKind {
			continue
		}
		payload := rowBytes(row, "payload")
		if !legacyPayloadLengthOK(payload, expectedLength) {
			return nil, false
		}
		out = append(out, payload)
	}
	return out, true
}

func writeLegacyRawRows(w *packetWriter, rows [][]byte) {
	w.writeUint32(uint32(len(rows)))
	for _, row := range rows {
		w.writeBytes(row)
	}
}
