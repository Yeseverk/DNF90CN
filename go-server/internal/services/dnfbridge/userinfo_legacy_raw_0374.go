package dnfbridge

func (r *csharpLegacyUserInfoReader) build374() []byte {
	row, ok := r.rawOne("legacy_character_userinfo374_state", []string{"header"})
	if !ok {
		return nil
	}
	header := rowBytes(row, "header")
	if !legacyPayloadLengthOK(header, 20) {
		return nil
	}
	rows12, ok := r.rawPayloadRows("legacy_character_userinfo374_rows", "raw12", 12)
	if !ok {
		return nil
	}
	rows13, ok := r.rawPayloadRows("legacy_character_userinfo374_rows", "raw13", 13)
	if !ok {
		return nil
	}
	var w packetWriter
	w.writeBytes(header)
	writeLegacyRawRows(&w, rows12)
	writeLegacyRawRows(&w, rows13)
	r.loaded = true
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build379() []byte {
	row, ok := r.rawOne("legacy_character_userinfo379_state", []string{"header"})
	if !ok {
		return nil
	}
	header := rowBytes(row, "header")
	if !legacyPayloadLengthOK(header, 25) {
		return nil
	}
	rows12A, ok := r.rawPayloadRows("legacy_character_userinfo379_rows", "raw12_a", 12)
	if !ok {
		return nil
	}
	rows12B, ok := r.rawPayloadRows("legacy_character_userinfo379_rows", "raw12_b", 12)
	if !ok {
		return nil
	}
	rows5, ok := r.rawPayloadRows("legacy_character_userinfo379_rows", "raw5", 5)
	if !ok {
		return nil
	}
	var w packetWriter
	w.writeBytes(header)
	writeLegacyRawRows(&w, rows12A)
	writeLegacyRawRows(&w, rows12B)
	writeLegacyRawRows(&w, rows5)
	r.loaded = true
	return w.bytes()
}

func (r *csharpLegacyUserInfoReader) build37A() []byte {
	row, ok := r.rawOne("legacy_character_userinfo37a_state", []string{"header1", "header33"})
	if !ok {
		return nil
	}
	header1 := rowBytes(row, "header1")
	header33 := rowBytes(row, "header33")
	if !legacyPayloadLengthOK(header1, 1) || !legacyPayloadLengthOK(header33, 33) {
		return nil
	}
	rows8, ok := r.rawPayloadRows("legacy_character_userinfo37a_rows", "raw8", 8)
	if !ok {
		return nil
	}
	rows12A, ok := r.rawPayloadRows("legacy_character_userinfo37a_rows", "raw12_a", 12)
	if !ok {
		return nil
	}
	rows12B, ok := r.rawPayloadRows("legacy_character_userinfo37a_rows", "raw12_b", 12)
	if !ok {
		return nil
	}
	rows12C, ok := r.rawPayloadRows("legacy_character_userinfo37a_rows", "raw12_c", 12)
	if !ok {
		return nil
	}
	rows10A, ok := r.rawPayloadRows("legacy_character_userinfo37a_rows", "raw10_a", 10)
	if !ok {
		return nil
	}
	rows10B, ok := r.rawPayloadRows("legacy_character_userinfo37a_rows", "raw10_b", 10)
	if !ok {
		return nil
	}
	rows5, ok := r.rawPayloadRows("legacy_character_userinfo37a_rows", "raw5", 5)
	if !ok {
		return nil
	}
	var w packetWriter
	w.writeBytes(header1)
	w.writeBytes(header33)
	writeLegacyRawRows(&w, rows8)
	writeLegacyRawRows(&w, rows12A)
	writeLegacyRawRows(&w, rows12B)
	writeLegacyRawRows(&w, rows12C)
	writeLegacyRawRows(&w, rows10A)
	writeLegacyRawRows(&w, rows10B)
	writeLegacyRawRows(&w, rows5)
	r.loaded = true
	return w.bytes()
}
