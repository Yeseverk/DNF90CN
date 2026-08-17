// Package skillcmd decodes skill-related C2S bodies for the current NoPack EXE.
package skillcmd

import (
	"encoding/binary"
	"fmt"
)

const (
	changeSkillSlotBodySize = 8
	buySkillHeaderSize      = 2
	buySkillEntrySize       = 4
	buySkillTailSize        = 1
	skillInitBodySize       = 2
	changeSkillTreeBodySize = 5
)

// ChangeSkillSlotRequest is the current EXE type-28 request.
// ContextIndex is intentionally neutral until its runtime owner is proven.
type ChangeSkillSlotRequest struct {
	SkillTree    byte
	From         byte
	To           byte
	ContextIndex int32
	Mode         byte
}

// BuySkillRequest is the current EXE type-29 learn/refund request.
type BuySkillRequest struct {
	RawSkillTree byte
	Count        byte
	Entries      []BuySkillEntry
	FinalMode    byte
}

// BuySkillEntry carries a job-scoped uint16 skill ID and a level delta.
type BuySkillEntry struct {
	SkillID    uint16
	RefundFlag byte
	LevelDelta byte
}

// SkillInitRequest is the current EXE type-491 reset request. The live
// Skill(K) initialize confirmation path writes exactly u8 tree, u8 mode.
type SkillInitRequest struct {
	SkillTree byte
	Mode      byte
}

// SignedDelta returns the requested level change without assigning policy to
// non-zero refund flag values.
func (e BuySkillEntry) SignedDelta() int {
	if e.RefundFlag != 0 {
		return -int(e.LevelDelta)
	}
	return int(e.LevelDelta)
}

// ChangeAnotherSkillTreeRequest is the current EXE type-260 request.
type ChangeAnotherSkillTreeRequest struct {
	// SkillTree is the currently selected tree. The server switches to the
	// other tree only after matching this value against persisted state.
	SkillTree byte
	// TransportTail is the four-byte transport-owned suffix observed in the
	// live current-EXE writer. It is retained for diagnostics and has no domain
	// authority.
	TransportTail [4]byte
}

// SkillCommandRequest is the current EXE type-331 command customization body.
type SkillCommandRequest struct {
	SkillTree byte
	Records   []SkillCommandRecord
}

// SkillCommandRecord is one command customization record.
type SkillCommandRecord struct {
	SkillID      uint16
	CommandBytes []byte
}

// DecodeChangeSkillSlotRequest decodes:
// u8 tree, u8 from, u8 to, i32 context index, u8 mode.
func DecodeChangeSkillSlotRequest(body []byte) (ChangeSkillSlotRequest, error) {
	if len(body) != changeSkillSlotBodySize {
		return ChangeSkillSlotRequest{}, fmt.Errorf("invalid body length: got %d want %d", len(body), changeSkillSlotBodySize)
	}
	return ChangeSkillSlotRequest{
		SkillTree:    body[0],
		From:         body[1],
		To:           body[2],
		ContextIndex: int32(binary.LittleEndian.Uint32(body[3:7])),
		Mode:         body[7],
	}, nil
}

// DecodeBuySkillRequest decodes:
// u8 tree, u8 count, count * (u16 skill ID, u8 refund flag, u8 delta), u8 mode.
func DecodeBuySkillRequest(body []byte) (BuySkillRequest, error) {
	if len(body) < buySkillHeaderSize+buySkillTailSize {
		return BuySkillRequest{}, fmt.Errorf("body too short: got %d want >= %d", len(body), buySkillHeaderSize+buySkillTailSize)
	}
	req := BuySkillRequest{RawSkillTree: body[0], Count: body[1]}
	expected := buySkillHeaderSize + int(req.Count)*buySkillEntrySize + buySkillTailSize
	if len(body) != expected {
		return BuySkillRequest{}, fmt.Errorf("invalid body length: got %d want %d for count %d", len(body), expected, req.Count)
	}

	req.Entries = make([]BuySkillEntry, 0, int(req.Count))
	offset := buySkillHeaderSize
	for i := 0; i < int(req.Count); i++ {
		req.Entries = append(req.Entries, BuySkillEntry{
			SkillID:    binary.LittleEndian.Uint16(body[offset : offset+2]),
			RefundFlag: body[offset+2],
			LevelDelta: body[offset+3],
		})
		offset += buySkillEntrySize
	}
	req.FinalMode = body[offset]
	return req, nil
}

// DecodeSkillInitRequest decodes the two bytes written by current EXE
// sub_1FE6070/sub_2209BC0.
func DecodeSkillInitRequest(body []byte) (SkillInitRequest, error) {
	if len(body) != skillInitBodySize {
		return SkillInitRequest{}, fmt.Errorf("invalid body length: got %d want %d", len(body), skillInitBodySize)
	}
	return SkillInitRequest{SkillTree: body[0], Mode: body[1]}, nil
}

// DecodeChangeAnotherSkillTreeRequest decodes the fixed five-byte live body.
// Only byte zero belongs to the writer; the remaining bytes are transport tail.
func DecodeChangeAnotherSkillTreeRequest(body []byte) (ChangeAnotherSkillTreeRequest, error) {
	if len(body) != changeSkillTreeBodySize {
		return ChangeAnotherSkillTreeRequest{}, fmt.Errorf("invalid body length: got %d want %d", len(body), changeSkillTreeBodySize)
	}
	if body[0] > 1 {
		return ChangeAnotherSkillTreeRequest{}, fmt.Errorf("invalid current skill tree: got %d want 0 or 1", body[0])
	}
	request := ChangeAnotherSkillTreeRequest{SkillTree: body[0]}
	copy(request.TransportTail[:], body[1:])
	return request, nil
}

// DecodeSkillCommandRequest decodes:
// u8 tree, then records until EOF: u16 skill ID, u8 length, raw command bytes.
func DecodeSkillCommandRequest(body []byte) (SkillCommandRequest, error) {
	if len(body) < 1 {
		return SkillCommandRequest{}, fmt.Errorf("body too short: got %d want >= 1", len(body))
	}
	req := SkillCommandRequest{SkillTree: body[0]}
	for offset := 1; offset < len(body); {
		if len(body)-offset < 3 {
			return SkillCommandRequest{}, fmt.Errorf("record header too short: offset %d body %d", offset, len(body))
		}
		skillID := binary.LittleEndian.Uint16(body[offset : offset+2])
		commandLen := int(body[offset+2])
		offset += 3
		if len(body)-offset < commandLen {
			return SkillCommandRequest{}, fmt.Errorf("record command too short: offset %d len %d body %d", offset, commandLen, len(body))
		}
		req.Records = append(req.Records, SkillCommandRecord{
			SkillID:      skillID,
			CommandBytes: append([]byte(nil), body[offset:offset+commandLen]...),
		})
		offset += commandLen
	}
	return req, nil
}

func (r ChangeSkillSlotRequest) String() string {
	return fmt.Sprintf("tree=%d from=%d to=%d context=%d mode=%d", r.SkillTree, r.From, r.To, r.ContextIndex, r.Mode)
}

func (r BuySkillRequest) String() string {
	return fmt.Sprintf("tree=%d declared=%d entries=%d mode=%d", r.RawSkillTree, r.Count, len(r.Entries), r.FinalMode)
}

func (r SkillInitRequest) String() string {
	return fmt.Sprintf("tree=%d mode=%d", r.SkillTree, r.Mode)
}

func (r ChangeAnotherSkillTreeRequest) String() string {
	return fmt.Sprintf("tree=%d", r.SkillTree)
}

func (r SkillCommandRequest) String() string {
	return fmt.Sprintf("tree=%d records=%d", r.SkillTree, len(r.Records))
}
