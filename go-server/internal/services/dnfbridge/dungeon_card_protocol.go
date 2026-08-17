package dnfbridge

import (
	"encoding/binary"
	"errors"

	"longheng.io/server/internal/modules/dnf/dungeoncmd"
)

var errCurrentDungeonCardPacketShape = errors.New("current dungeon card packet shape is invalid")

type currentDungeonOp71Request struct {
	ValueA     byte
	ValueB     byte
	ValueCount byte
}

type currentDungeonOp72Request struct {
	ValueA byte
	ValueB byte
}

func decodeCurrentDungeonOp71Request(body []byte) (currentDungeonOp71Request, error) {
	// Current class1/op71 writers (sub_32B1410/sub_32B14F0 and the direct path in
	// sub_1D00F60) emit one or two semantic bytes. Live unpatched clients
	// deliver an opaque protected 8-byte block whose first bytes are not the
	// selector; the selective plaintext clone delivers the semantic bytes plus
	// the private four-byte trailer, stripped at the exact 6-byte boundary by
	// normalizeLegacyGameBody. Accept any body of at least one byte and read
	// only the proved prefix instead of rejecting the trailing bytes, mirroring
	// the op72 relaxation.
	if len(body) < 1 {
		return currentDungeonOp71Request{}, errCurrentDungeonCardPacketShape
	}
	request := currentDungeonOp71Request{ValueA: body[0], ValueCount: 1}
	if len(body) >= 2 {
		request.ValueB = body[1]
		request.ValueCount = 2
	}
	return request, nil
}

func decodeCurrentDungeonOp72Request(body []byte) (currentDungeonOp72Request, error) {
	// Current class1/op72 (sub_1CF68F0) reads exactly two u8s — state then
	// option. Live unpatched clients deliver an opaque protected 16-byte body
	// whose first bytes are ciphertext, so a state/option read from it is
	// meaningless (the ACK echo is ignored by the client and no action is
	// taken); the selective plaintext clone instead delivers the two semantic
	// bytes plus a private four-byte trailer, which normalizeLegacyGameBody
	// strips at the exact 6-byte boundary. Accept any body of at least two
	// bytes and read only the proved prefix, like the reference handler,
	// instead of rejecting the trailing bytes.
	if len(body) < 2 {
		return currentDungeonOp72Request{}, errCurrentDungeonCardPacketShape
	}
	return currentDungeonOp72Request{ValueA: body[0], ValueB: body[1]}, nil
}

func validateCurrentDungeonBodylessRequest(body []byte) error {
	if len(body) != 0 {
		return errCurrentDungeonCardPacketShape
	}
	return nil
}

// buildCurrentDungeonOp69SuccessBody is only the common success byte. The
// current class1/op69 handler sub_1CFE660 reads no command payload before it
// advances the result UI or emits bodyless op70.
func buildCurrentDungeonOp69SuccessBody() []byte {
	return []byte{1}
}

// buildCurrentDungeonOp70CommonOnlySuccessBody matches the conditional branch
// in sub_1D00F60 that consumes no command payload. The condition selecting
// this branch has not yet been mapped to a safe server-side runtime predicate.
func buildCurrentDungeonOp70CommonOnlySuccessBody() []byte {
	return []byte{1}
}

// buildCurrentDungeonOp70EightValueSuccessBody matches the other proven
// sub_1D00F60 branch: common success followed by exactly eight opaque u16s.
func buildCurrentDungeonOp70EightValueSuccessBody(values [dungeonCardWireSlotCount]uint16) []byte {
	body := make([]byte, 1+dungeonCardWireSlotCount*2)
	body[0] = 1
	for index, value := range values {
		binary.LittleEndian.PutUint16(body[1+index*2:1+(index+1)*2], value)
	}
	return body
}

type currentDungeonOp71RewardTuple struct {
	ValueA uint32
	ValueB uint32
}

type currentDungeonOp71Slot struct {
	StateA       byte
	StateB       byte
	Rewards      []currentDungeonOp71RewardTuple
	TerminalFlag byte
}

func buildCurrentDungeonOp71SuccessBody(slots [dungeonCardWireSlotCount]currentDungeonOp71Slot) ([]byte, error) {
	size := 1
	for _, slot := range slots {
		if len(slot.Rewards) > 255 {
			return nil, errCurrentDungeonCardPacketShape
		}
		size += 3 + len(slot.Rewards)*8
	}
	body := make([]byte, 0, size)
	body = append(body, 1)
	for _, slot := range slots {
		body = append(body, slot.StateA, slot.StateB, byte(len(slot.Rewards)))
		for _, reward := range slot.Rewards {
			var raw [8]byte
			binary.LittleEndian.PutUint32(raw[0:4], reward.ValueA)
			binary.LittleEndian.PutUint32(raw[4:8], reward.ValueB)
			body = append(body, raw[:]...)
		}
		body = append(body, slot.TerminalFlag)
	}
	return body, nil
}

func buildCurrentDungeonOp72SuccessBody(valueA byte, valueB byte) []byte {
	return []byte{1, valueA, valueB}
}

// encodeCurrentDungeonSelectRequest re-encodes the exact 21-byte op16 request
// layout parsed by dungeoncmd.DecodeSelectDungeonRequest. The settlement
// retry owner reuses it to replay the retired run's own proven request
// through the standard entry flow; no field is invented.
func encodeCurrentDungeonSelectRequest(request dungeoncmd.SelectDungeonRequest) []byte {
	body := make([]byte, dungeoncmd.SelectDungeonRequestSize)
	binary.LittleEndian.PutUint32(body[0:4], request.DungeonID)
	body[4] = request.Difficulty
	binary.LittleEndian.PutUint16(body[5:7], request.EntryOption)
	body[7] = request.SelectionMode
	body[8] = request.RuntimeState
	binary.LittleEndian.PutUint16(body[9:11], request.RuntimeToken)
	binary.LittleEndian.PutUint32(body[11:15], request.Reserved)
	body[15] = request.PartyState
	binary.LittleEndian.PutUint32(body[16:20], request.LeaderObjectKey)
	body[20] = request.SpecialMode
	return body
}

func buildCurrentDungeonOp72FailureBody(detail byte) []byte {
	return []byte{0, detail}
}
