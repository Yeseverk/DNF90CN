package dnfbridge

import "encoding/binary"

type selectCharacterRequest struct {
	charID uint16
	slot   int
	clear  bool
	source string
}

func csharpSelectCharacterID(request []byte) uint16 {
	parsed := parseSelectCharacterRequest(request)
	return parsed.charID
}

func csharpSelectCharacterSlot(request []byte) int {
	return parseSelectCharacterRequest(request).slot
}

func parseSelectCharacterRequest(request []byte) selectCharacterRequest {
	parsed := selectCharacterRequest{slot: -1, source: "unknown"}
	if plain, ok := parseNoPackPlainSelectCharacterRequest(request); ok {
		return plain
	}
	if protected, ok := parseNoPackProtectedSelectCharacterRequest(request); ok {
		return protected
	}
	if len(request) >= 11 {
		charID := binary.LittleEndian.Uint32(request[:4])
		slot := int(request[4])
		option := request[5]
		flag := request[10]
		if charID != 0 && slot < defaultCharacterSlots && option <= 3 && flag <= 1 {
			parsed.charID = uint16(charID)
			parsed.slot = slot
			parsed.clear = true
			parsed.source = "latest_type3"
			return parsed
		}
		parsed.source = "opaque"
		return parsed
	}
	if len(request) >= 2 {
		value := binary.LittleEndian.Uint16(request[:2])
		if value <= uint16(defaultCharacterSlots-1) {
			parsed.slot = int(value)
			// The old two-byte compatibility request is ambiguous once valid
			// character IDs overlap the expanded 0..31 slot range. Prefer a
			// real record at that slot, but retain the same value as a verified
			// character-ID fallback when that slot is empty.
			parsed.charID = value
			parsed.source = "legacy_slot_or_char_id"
		} else {
			parsed.charID = value
			parsed.source = "legacy_char_id"
		}
		parsed.clear = true
	}
	return parsed
}

func parseNoPackPlainSelectCharacterRequest(request []byte) (selectCharacterRequest, bool) {
	if len(request) != 16 {
		return selectCharacterRequest{}, false
	}
	for _, b := range request[4:] {
		if b != 0 {
			return selectCharacterRequest{}, false
		}
	}
	slot := binary.LittleEndian.Uint32(request[:4])
	if slot >= defaultCharacterSlots {
		return selectCharacterRequest{}, false
	}
	return selectCharacterRequest{charID: 1, slot: int(slot), clear: true, source: "nopack_plain_slot"}, true
}

func parseNoPackProtectedSelectCharacterRequest(request []byte) (selectCharacterRequest, bool) {
	if len(request) != 16 {
		return selectCharacterRequest{}, false
	}
	plain, err := decodeUpperKey4(request)
	if err != nil || len(plain) < 4 {
		return selectCharacterRequest{}, false
	}
	for _, b := range plain[4:] {
		if b != 0 {
			return selectCharacterRequest{}, false
		}
	}
	slot := binary.LittleEndian.Uint32(plain[:4])
	if slot >= defaultCharacterSlots {
		return selectCharacterRequest{}, false
	}
	return selectCharacterRequest{charID: 1, slot: int(slot), clear: true, source: "nopack_key4_slot"}, true
}
