package dnfbridge

import dnfrepo "longheng.io/server/internal/modules/dnf/repository"

func (r *csharpLegacyUserInfoReader) buildRawFixed(notiType uint16, expectedLength int) []byte {
	rows := r.queryRows("legacy_character_userinfo_fixed_raws",
		[]string{"noti_type", "payload"},
		[]string{"noti_type"},
		false)
	for _, row := range rows {
		if rowU16(row, "noti_type", 0) != notiType {
			continue
		}
		payload := rowBytes(row, "payload")
		if !legacyPayloadLengthOK(payload, expectedLength) {
			return nil
		}
		r.loaded = true
		return append([]byte(nil), payload...)
	}
	return nil
}

func (r *csharpLegacyUserInfoReader) buildRawBody(notiType uint16) []byte {
	rows := r.queryRows("legacy_character_userinfo_fixed_raws",
		[]string{"noti_type", "payload"},
		[]string{"noti_type"},
		false)
	for _, row := range rows {
		if rowU16(row, "noti_type", 0) != notiType {
			continue
		}
		payload := rowBytes(row, "payload")
		if len(payload) == 0 {
			return nil
		}
		r.loaded = true
		return append([]byte(nil), payload...)
	}
	return nil
}

func csharpRawUserInfoBody(notiType uint16) bool {
	switch notiType {
	case 0x040a, 0x0412, 0x041c, 0x041d, 0x041e:
		return true
	default:
		return false
	}
}

func csharpRawFixedUserInfoLength(notiType uint16) (int, bool) {
	switch notiType {
	case 0x0161, 0x01d9, 0x0343, 0x03fd, 0x0424, 0x0547, 0x0549:
		return 1, true
	case 0x0425, 0x0508:
		return 2, true
	case 0x0312, 0x03c7, 0x03e7, 0x040d, 0x040f, 0x0410, 0x0416, 0x0418,
		0x0419, 0x041a, 0x041b, 0x0435, 0x043e, 0x043f, 0x044e, 0x044f,
		0x0458, 0x0459, 0x0462, 0x0469, 0x046f, 0x0470, 0x048f, 0x04c6,
		0x04c7, 0x04ca, 0x04db, 0x04df, 0x050b, 0x050c, 0x0534:
		return 4, true
	case 0x0311, 0x0378, 0x0400, 0x040c, 0x0415, 0x04aa, 0x04f0, 0x04ff:
		return 5, true
	case 0x040e:
		return 6, true
	case 0x042a, 0x042c:
		return 7, true
	case 0x0373, 0x03d0, 0x0411, 0x048c, 0x04a6, 0x04a7, 0x04a9, 0x04f1,
		0x04f8, 0x050a, 0x052b, 0x0532, 0x0533:
		return 8, true
	case 0x03f3:
		return 9, true
	case 0x045a, 0x04c8:
		return 10, true
	case 0x0408:
		return 11, true
	case 0x03e8, 0x0409, 0x0428, 0x046c, 0x04b0, 0x04b1, 0x04da, 0x04ec,
		0x0517, 0x0518:
		return 12, true
	case 0x0407, 0x046b, 0x04fe:
		return 13, true
	case 0x0440:
		return 14, true
	case 0x0430:
		return 15, true
	case 0x042d, 0x0457:
		return 16, true
	case 0x048e, 0x04d5:
		return 17, true
	case 0x0406, 0x04b2:
		return 20, true
	case 0x0413:
		return 22, true
	case 0x0344:
		return 24, true
	case 0x052f:
		return 36, true
	case 0x0375, 0x052c:
		return 38, true
	case 0x0429, 0x042e:
		return 40, true
	case 0x0516:
		return 44, true
	case 0x04e2, 0x0515:
		return 48, true
	case 0x045f:
		return 54, true
	case 0x0514:
		return 58, true
	case 0x046e:
		return 61, true
	case 0x04d4:
		return 68, true
	case 0x040b:
		return 97, true
	case 0x050d:
		return 3504, true
	default:
		return 0, false
	}
}

func (r *csharpLegacyUserInfoReader) buildRawByteCountList(notiType uint16, expectedRowLength int) []byte {
	rows := r.queryRows("legacy_character_userinfo_byte_count_raw_rows",
		[]string{"noti_type", "sort_order", "payload"},
		[]string{"noti_type", "sort_order"},
		false)
	filtered := make([]dnfrepo.LegacyUserInfoRow, 0, len(rows))
	for _, row := range rows {
		if rowU16(row, "noti_type", 0) == notiType {
			filtered = append(filtered, row)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	filtered = limitLegacyRowsU8(filtered)
	var w packetWriter
	w.writeByte(byte(len(filtered)))
	for _, row := range filtered {
		payload := rowBytes(row, "payload")
		if !legacyPayloadLengthOK(payload, expectedRowLength) {
			return nil
		}
		w.writeBytes(payload)
	}
	r.loaded = true
	return w.bytes()
}
