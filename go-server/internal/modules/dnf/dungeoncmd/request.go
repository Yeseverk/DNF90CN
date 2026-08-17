// 本文件解析副本类 C2S 请求体。
// 只记录当前已经对齐的请求字段，复杂场景状态交给后续 dungeon owner 处理。
package dungeoncmd

import (
	"encoding/binary"
	"fmt"
)

const (
	SelectDungeonRequestSize          = 21
	GetItemRequestSize                = 20
	MoveMapRequestSize                = 100
	DieMonsterRequestSize             = 62
	DieMonsterVariableBaseSize        = 66
	DieMonsterVariableCombatEntrySize = 10
	DieMonsterVariableRuntimeTailSize = 43
	DieMonsterVariableMaxCombatCount  = 254
	BossDieCheckRequestSize           = 39
	ChangeTutorialFlagRequestSize     = 6
)

// SelectDungeonRequest 描述选择副本请求。
type SelectDungeonRequest struct {
	DungeonID     uint32
	Difficulty    byte
	EntryOption   uint16
	SelectionMode byte
	RuntimeState  byte
	RuntimeToken  uint16
	Reserved      uint32
	PartyState    byte
	// LeaderObjectKey is the legacy name for the u32 at offset 16. Its
	// semantics are not proved for the current EXE, so owners must retain it
	// as opaque request state and must not use it as an authority boundary.
	LeaderObjectKey uint32
	SpecialMode     byte
	OpaqueTail      []byte
}

// GetItemRequest is the exact 20-byte ordinary scene-item pickup body written
// by the current EXE sub_2326920. Only DropObjectKey has proved ownership
// semantics; the remaining coordinates/tokens are retained by wire position.
// The separate 18-byte op43 writer is a script/special-object variant and is
// deliberately rejected by DecodeGetItemRequest.
type GetItemRequest struct {
	DropObjectKey uint32
	FixedZero     byte
	ObjectContext byte
	PlayerX       uint16
	PlayerY       uint16
	Token0        uint16
	DropX         uint16
	DropY         uint16
	Token1        uint16
	Token2        uint16
}

// MoveMapRequest 描述切换房间请求。
type MoveMapRequest struct {
	NextX         byte
	NextY         byte
	PositionX     uint32
	PositionY     uint32
	MoveKind      byte
	TimingToken   uint16
	ShortState    [8]uint16
	IntegerState  [8]uint32
	SequenceToken uint16
	RuntimeTail   [36]byte
	RuntimeState  byte
	OpaqueTail    []byte
}

// DieMonsterRequest is the current EXE op39 body. Unknown combat scalars are
// retained by position so the room owner can use the proven runtime object key
// without inventing semantics for the remaining fields.
type DieMonsterRequest struct {
	Layout           DieMonsterRequestLayout
	RuntimeObjectKey uint32
	OwnerObjectKey   uint16
	CombatState      [4]uint32
	StateFlag        byte
	ShortState       [2]uint16
	ByteState        [4]byte
	RandomState      uint16
	HasActorState    byte
	ActorState       uint32
	ActorValue       uint64
	ActorShortState  [7]uint16
	TailState        [2]byte
	CombatEntryCount byte
	CombatEntries    []byte
	RuntimeTail      []byte
	OpaqueTail       []byte
}

// BossDieCheckRequest is the exact 39-byte plaintext body written by the
// current EXE sub_24351D0 for class-1/op117. Only the two object keys have
// proven ownership semantics. The remaining values are retained by their
// native offsets so the bridge does not invent names for unproven fields.
type BossDieCheckRequest struct {
	RelatedActorObjectKey uint16
	TargetObjectKey       uint16
	ReservedZero          uint32
	Field08               uint32
	Field12               byte
	Field13               uint32
	Field17               uint64
	Field25               [7]uint16
}

type DieMonsterRequestLayout byte

const (
	DieMonsterRequestLayoutFixed62 DieMonsterRequestLayout = iota + 1
	DieMonsterRequestLayoutVariableCombat
)

// ChangeTutorialFlagRequest is the exact six-byte C2S body written by the
// current EXE sub_33BDAF0: u8 prefix, u32 progress, u8 commit flag. The
// count/entry layout belongs to the S2C success callback and must not be used
// to decode this request direction.
type ChangeTutorialFlagRequest struct {
	Prefix     byte
	Progress   uint32
	CommitFlag byte
}

// DecodeSelectDungeonRequest 解析选择副本请求。
func DecodeSelectDungeonRequest(body []byte) (SelectDungeonRequest, error) {
	if len(body) < SelectDungeonRequestSize {
		return SelectDungeonRequest{}, fmt.Errorf("body too short: got %d want >= %d", len(body), SelectDungeonRequestSize)
	}
	request := SelectDungeonRequest{
		DungeonID:       binary.LittleEndian.Uint32(body[0:4]),
		Difficulty:      body[4],
		EntryOption:     binary.LittleEndian.Uint16(body[5:7]),
		SelectionMode:   body[7],
		RuntimeState:    body[8],
		RuntimeToken:    binary.LittleEndian.Uint16(body[9:11]),
		Reserved:        binary.LittleEndian.Uint32(body[11:15]),
		PartyState:      body[15],
		LeaderObjectKey: binary.LittleEndian.Uint32(body[16:20]),
		SpecialMode:     body[20],
	}
	request.OpaqueTail = append([]byte(nil), body[SelectDungeonRequestSize:]...)
	return request, nil
}

// DecodeGetItemRequest accepts only the current ordinary-drop writer boundary.
func DecodeGetItemRequest(body []byte) (GetItemRequest, error) {
	if len(body) != GetItemRequestSize {
		return GetItemRequest{}, fmt.Errorf("invalid body length: got %d want %d", len(body), GetItemRequestSize)
	}
	return GetItemRequest{
		DropObjectKey: binary.LittleEndian.Uint32(body[0:4]),
		FixedZero:     body[4],
		ObjectContext: body[5],
		PlayerX:       binary.LittleEndian.Uint16(body[6:8]),
		PlayerY:       binary.LittleEndian.Uint16(body[8:10]),
		Token0:        binary.LittleEndian.Uint16(body[10:12]),
		DropX:         binary.LittleEndian.Uint16(body[12:14]),
		DropY:         binary.LittleEndian.Uint16(body[14:16]),
		Token1:        binary.LittleEndian.Uint16(body[16:18]),
		Token2:        binary.LittleEndian.Uint16(body[18:20]),
	}, nil
}

// DecodeMoveMapRequest parses the fixed 100-byte plaintext body written by the
// current EXE sub_17C9D80. Its first 63 bytes are followed by the exact 36-byte
// sub_17C8490 runtime block and one final state byte. The native opcode-45
// transport normally Twofish-pads this plaintext to 112 wire bytes; the client
// compatibility path must bypass that process-local cipher before this decoder.
func DecodeMoveMapRequest(body []byte) (MoveMapRequest, error) {
	if len(body) != MoveMapRequestSize {
		return MoveMapRequest{}, fmt.Errorf("invalid body length: got %d want %d", len(body), MoveMapRequestSize)
	}
	request := MoveMapRequest{
		NextX:        body[0],
		NextY:        body[1],
		PositionX:    binary.LittleEndian.Uint32(body[2:6]),
		PositionY:    binary.LittleEndian.Uint32(body[6:10]),
		MoveKind:     body[10],
		TimingToken:  binary.LittleEndian.Uint16(body[11:13]),
		RuntimeState: body[99],
	}
	offset := 13
	for index := range request.ShortState {
		request.ShortState[index] = binary.LittleEndian.Uint16(body[offset : offset+2])
		offset += 2
	}
	for index := range request.IntegerState {
		request.IntegerState[index] = binary.LittleEndian.Uint32(body[offset : offset+4])
		offset += 4
	}
	request.SequenceToken = binary.LittleEndian.Uint16(body[offset : offset+2])
	copy(request.RuntimeTail[:], body[63:99])
	return request, nil
}

// DecodeDieMonsterRequest parses both current EXE death-writer families.
// sub_2276500/sub_22767C0 emit the fixed 62-byte form. Tutorial APC writer
// sub_2570F20 emits 66+10*count bytes: a 23-byte prefix, count ten-byte combat
// entries, and a 43-byte runtime tail which includes sub_226F700 output.
// Unknown values are retained by exact boundary; extra bytes remain opaque so
// the runtime owner can reject an unproven extension before changing state.
func DecodeDieMonsterRequest(body []byte) (DieMonsterRequest, error) {
	if len(body) < DieMonsterRequestSize {
		return DieMonsterRequest{}, fmt.Errorf("body too short: got %d want >= %d", len(body), DieMonsterRequestSize)
	}
	if len(body) != DieMonsterRequestSize {
		return decodeVariableDieMonsterRequest(body)
	}
	request := DieMonsterRequest{
		Layout:           DieMonsterRequestLayoutFixed62,
		RuntimeObjectKey: binary.LittleEndian.Uint32(body[0:4]),
		OwnerObjectKey:   binary.LittleEndian.Uint16(body[4:6]),
		StateFlag:        body[22],
		ShortState: [2]uint16{
			binary.LittleEndian.Uint16(body[23:25]),
			binary.LittleEndian.Uint16(body[25:27]),
		},
		ByteState:     [4]byte{body[27], body[28], body[29], body[30]},
		RandomState:   binary.LittleEndian.Uint16(body[31:33]),
		HasActorState: body[33],
		ActorState:    binary.LittleEndian.Uint32(body[34:38]),
		ActorValue:    binary.LittleEndian.Uint64(body[38:46]),
		TailState:     [2]byte{body[60], body[61]},
		OpaqueTail:    append([]byte(nil), body[DieMonsterRequestSize:]...),
	}
	for index := range request.CombatState {
		offset := 6 + index*4
		request.CombatState[index] = binary.LittleEndian.Uint32(body[offset : offset+4])
	}
	for index := range request.ActorShortState {
		offset := 46 + index*2
		request.ActorShortState[index] = binary.LittleEndian.Uint16(body[offset : offset+2])
	}
	return request, nil
}

// DecodeBossDieCheckRequest parses only the current EXE's exact plaintext
// boundary. The ordinary protected path pads the same body to 40 wire bytes;
// that ciphertext must not be accepted by this decoder.
func DecodeBossDieCheckRequest(body []byte) (BossDieCheckRequest, error) {
	if len(body) != BossDieCheckRequestSize {
		return BossDieCheckRequest{}, fmt.Errorf(
			"invalid body length: got %d want %d",
			len(body),
			BossDieCheckRequestSize,
		)
	}
	request := BossDieCheckRequest{
		RelatedActorObjectKey: binary.LittleEndian.Uint16(body[0:2]),
		TargetObjectKey:       binary.LittleEndian.Uint16(body[2:4]),
		ReservedZero:          binary.LittleEndian.Uint32(body[4:8]),
		Field08:               binary.LittleEndian.Uint32(body[8:12]),
		Field12:               body[12],
		Field13:               binary.LittleEndian.Uint32(body[13:17]),
		Field17:               binary.LittleEndian.Uint64(body[17:25]),
	}
	for index := range request.Field25 {
		offset := 25 + index*2
		request.Field25[index] = binary.LittleEndian.Uint16(body[offset : offset+2])
	}
	return request, nil
}

func decodeVariableDieMonsterRequest(body []byte) (DieMonsterRequest, error) {
	if len(body) < DieMonsterVariableBaseSize {
		return DieMonsterRequest{}, fmt.Errorf(
			"unsupported die-monster boundary: got %d want %d or >= %d",
			len(body), DieMonsterRequestSize, DieMonsterVariableBaseSize,
		)
	}
	count := int(body[22])
	if count > DieMonsterVariableMaxCombatCount {
		return DieMonsterRequest{}, fmt.Errorf(
			"die-monster combat count exceeds current writer limit: got %d want <= %d",
			count, DieMonsterVariableMaxCombatCount,
		)
	}
	expected := DieMonsterVariableBaseSize + count*DieMonsterVariableCombatEntrySize
	if len(body) < expected {
		return DieMonsterRequest{}, fmt.Errorf(
			"variable die-monster body too short: got %d want >= %d for count %d",
			len(body), expected, count,
		)
	}
	entriesStart := 23
	entriesEnd := entriesStart + count*DieMonsterVariableCombatEntrySize
	runtimeTailEnd := entriesEnd + DieMonsterVariableRuntimeTailSize
	request := DieMonsterRequest{
		Layout:           DieMonsterRequestLayoutVariableCombat,
		RuntimeObjectKey: binary.LittleEndian.Uint32(body[0:4]),
		OwnerObjectKey:   binary.LittleEndian.Uint16(body[4:6]),
		CombatEntryCount: byte(count),
		CombatEntries:    append([]byte(nil), body[entriesStart:entriesEnd]...),
		RuntimeTail:      append([]byte(nil), body[entriesEnd:runtimeTailEnd]...),
		OpaqueTail:       append([]byte(nil), body[runtimeTailEnd:]...),
	}
	for index := range request.CombatState {
		offset := 6 + index*4
		request.CombatState[index] = binary.LittleEndian.Uint32(body[offset : offset+4])
	}
	return request, nil
}

// DecodeChangeTutorialFlagRequest parses the current EXE C2S writer. The
// boundary is exact so a protected/padded body cannot be mistaken for the
// plaintext tutorial-finish handshake.
func DecodeChangeTutorialFlagRequest(body []byte) (ChangeTutorialFlagRequest, error) {
	if len(body) != ChangeTutorialFlagRequestSize {
		return ChangeTutorialFlagRequest{}, fmt.Errorf(
			"body length mismatch: got %d want %d",
			len(body),
			ChangeTutorialFlagRequestSize,
		)
	}
	return ChangeTutorialFlagRequest{
		Prefix:     body[0],
		Progress:   binary.LittleEndian.Uint32(body[1:5]),
		CommitFlag: body[5],
	}, nil
}

func (r SelectDungeonRequest) String() string {
	return fmt.Sprintf(
		"dungeon=%d difficulty=%d entryOption=%d selectionMode=%d runtimeState=%d runtimeToken=%d reserved=%d partyState=%d leader=%d specialMode=%d tail=%d",
		r.DungeonID, r.Difficulty, r.EntryOption, r.SelectionMode, r.RuntimeState,
		r.RuntimeToken, r.Reserved, r.PartyState, r.LeaderObjectKey, r.SpecialMode, len(r.OpaqueTail),
	)
}

func (r GetItemRequest) String() string {
	return fmt.Sprintf(
		"dropObject=%d fixed=%d context=%d player=(%d,%d) token0=%d drop=(%d,%d) token1=%d token2=%d",
		r.DropObjectKey,
		r.FixedZero,
		r.ObjectContext,
		r.PlayerX,
		r.PlayerY,
		r.Token0,
		r.DropX,
		r.DropY,
		r.Token1,
		r.Token2,
	)
}

func (r MoveMapRequest) String() string {
	return fmt.Sprintf(
		"next=(%d,%d) position=(%d,%d) kind=%d sequence=%d tail=%d",
		r.NextX, r.NextY, r.PositionX, r.PositionY, r.MoveKind, r.SequenceToken, len(r.OpaqueTail),
	)
}

func (r DieMonsterRequest) String() string {
	return fmt.Sprintf(
		"layout=%d runtimeObject=%d ownerObject=%d actorState=%d combatEntries=%d runtimeTail=%d tail=%d",
		r.Layout, r.RuntimeObjectKey, r.OwnerObjectKey, r.ActorState,
		r.CombatEntryCount, len(r.RuntimeTail), len(r.OpaqueTail),
	)
}

func (r BossDieCheckRequest) String() string {
	return fmt.Sprintf(
		"relatedActor=%d target=%d reserved=%d field08=%d field12=%d field13=%d field17=%d",
		r.RelatedActorObjectKey,
		r.TargetObjectKey,
		r.ReservedZero,
		r.Field08,
		r.Field12,
		r.Field13,
		r.Field17,
	)
}

func (r ChangeTutorialFlagRequest) String() string {
	return fmt.Sprintf("prefix=%d progress=%d commit=%d", r.Prefix, r.Progress, r.CommitFlag)
}

func readU16(body []byte, offset int) uint16 {
	if offset+2 <= len(body) {
		return binary.LittleEndian.Uint16(body[offset:])
	}
	return 0
}

func readU32(body []byte, offset int) uint32 {
	if offset+4 <= len(body) {
		return binary.LittleEndian.Uint32(body[offset:])
	}
	return 0
}
