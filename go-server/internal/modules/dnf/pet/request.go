// This file decodes current-EXE pet requests. Item identity is never accepted
// from the request body; the owner resolves it from the selected durable slot.
package pet

import (
	"encoding/binary"
	"fmt"
)

const (
	listTypePet                byte = 7
	maxCreatureRenameNameBytes      = 12
	currentDstrLengthBytes          = 4
)

// RenameCreatureRequest is the exact current-EXE op100 body:
// u16 slot + u8 list_type + DSTR raw_name. DSTR is decoded using the current
// bridge's u32 byte-length convention; suffix bytes are never accepted.
type RenameCreatureRequest struct {
	SlotIndex int16
	ListType  byte
	NameRaw   []byte
}

// DecodeRenameCreatureRequest keeps the current wire layout separate from
// the rename domain. The 12-byte encoded limit, non-empty rule, and literal
// forbidden set are proved by current NoPack: the rename text control is
// created with maximum 12 (sub_31F5CB0 -> sub_3539D80, read back by
// sub_353C2E0), and the visible validator sub_31F57A0 rejects empty names and
// the nine literal characters below. The legacy 86JP 13-byte rule is not
// current-EXE proof and is not used.
func DecodeRenameCreatureRequest(body []byte) (RenameCreatureRequest, error) {
	const fixedBytes = 2 + 1 + currentDstrLengthBytes
	if len(body) < fixedBytes {
		return RenameCreatureRequest{}, fmt.Errorf("invalid body length: got %d want at least %d", len(body), fixedBytes)
	}
	request := RenameCreatureRequest{
		SlotIndex: int16(binary.LittleEndian.Uint16(body[0:2])),
		ListType:  body[2],
	}
	if request.ListType != listTypePet {
		return RenameCreatureRequest{}, fmt.Errorf("invalid list type: got %d want %d", request.ListType, listTypePet)
	}
	if request.SlotIndex < 0 || request.SlotIndex > 139 {
		return RenameCreatureRequest{}, fmt.Errorf("slot %d outside creature range 0..139", request.SlotIndex)
	}
	nameLength := uint64(binary.LittleEndian.Uint32(body[3:7]))
	if nameLength > maxCreatureRenameNameBytes {
		return RenameCreatureRequest{}, fmt.Errorf("creature name is %d bytes, maximum is %d", nameLength, maxCreatureRenameNameBytes)
	}
	if nameLength != uint64(len(body)-fixedBytes) {
		return RenameCreatureRequest{}, fmt.Errorf("DSTR length mismatch: declared %d actual %d", nameLength, len(body)-fixedBytes)
	}
	request.NameRaw = append([]byte(nil), body[fixedBytes:]...)
	if err := validateCreatureRenameName(request.NameRaw); err != nil {
		return RenameCreatureRequest{}, err
	}
	return request, nil
}

// validateCreatureRenameName enforces the current-EXE-proved subset: encoded
// length at most 12 bytes, non-empty, no NUL/C0/DEL control bytes, and none
// of the nine literal characters rejected by sub_31F57A0 (apostrophe, space,
// tab, backslash, percent, less-than, greater-than, double quote, vertical
// bar). All checks are byte-level so GBK/codepage names are not mistaken for
// control data; the dynamic blacklist, virtual filter, and single/double-byte
// range tables are not yet exported and therefore not claimed.
func validateCreatureRenameName(name []byte) error {
	if len(name) > maxCreatureRenameNameBytes {
		return fmt.Errorf("creature name is %d bytes, maximum is %d", len(name), maxCreatureRenameNameBytes)
	}
	if len(name) == 0 {
		return fmt.Errorf("creature name must not be empty")
	}
	for _, value := range name {
		switch value {
		case '\'', ' ', '\t', '\\', '%', '<', '>', '"', '|':
			return fmt.Errorf("creature name contains forbidden character %q", value)
		}
		if value < 0x20 || value == 0x7f {
			return fmt.Errorf("creature name contains a control character")
		}
	}
	return nil
}

func (r RenameCreatureRequest) String() string {
	return fmt.Sprintf("list=%d slot=%d nameBytes=%d", r.ListType, r.SlotIndex, len(r.NameRaw))
}

// HatchCreatureRequest is the exact current-EXE op102 body:
// u8 list_type + u16 slot.
type HatchCreatureRequest struct {
	ListType  byte
	SlotIndex int16
}

// DecodeHatchCreatureRequest rejects suffix guesses and non-pet lists.
func DecodeHatchCreatureRequest(body []byte) (HatchCreatureRequest, error) {
	if len(body) != 3 {
		return HatchCreatureRequest{}, fmt.Errorf("invalid body length: got %d want 3", len(body))
	}
	if body[0] != listTypePet {
		return HatchCreatureRequest{}, fmt.Errorf("invalid list type: got %d want %d", body[0], listTypePet)
	}
	req := HatchCreatureRequest{ListType: body[0], SlotIndex: int16(binary.LittleEndian.Uint16(body[1:3]))}
	if req.SlotIndex < 0 || req.SlotIndex > 139 {
		return HatchCreatureRequest{}, fmt.Errorf("slot %d outside creature range 0..139", req.SlotIndex)
	}
	return req, nil
}

func (r HatchCreatureRequest) String() string {
	return fmt.Sprintf("list=%d slot=%d", r.ListType, r.SlotIndex)
}
