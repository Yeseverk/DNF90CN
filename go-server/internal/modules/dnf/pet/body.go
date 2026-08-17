// This file owns the current-EXE creature-list body. Inventory list-7 rows
// are delegated to inventory.BuildPetItemListRefreshBody so both paths use
// the same proved 0x77-byte item row.
package pet

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	dnfinventory "longheng.io/server/internal/modules/dnf/inventory"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

var (
	errCreatureCountTooLarge = errors.New("creature count exceeds 255")
	errCreatureKeyInvalid    = errors.New("creature key is invalid")
	errCreatureModeInvalid   = errors.New("creature mode flag is invalid")
	errCreatureGrowthInvalid = errors.New("creature growth state is invalid")
	errCreatureNameTooLong   = errors.New("creature name exceeds 30 bytes")
	errCreatureTailInvalid   = errors.New("creature tail flag is invalid")
)

// BuildCreatureListBody exposes the current NoPack class0/op105 business body
// to the bridge's scene-lifecycle owner. Callers must still choose a proven
// scene-ready send point; this function only owns the exact typed body.
func BuildCreatureListBody(entries []dnfrepo.PetEntry) ([]byte, error) {
	return buildCreatureListBody(entries)
}

// BuildCreatureGrowthBody follows current NoPack sub_1D5AF60 exactly. This is
// the scene class0/op102 direction and must not be confused with the unrelated
// C2S hatch request or its class1/status ACK, which share the numeric opcode.
func BuildCreatureGrowthBody(entry dnfrepo.PetEntry) ([]byte, error) {
	if entry.ModeFlag > 1 {
		return nil, fmt.Errorf("%w: mode=%d", errCreatureModeInvalid, entry.ModeFlag)
	}
	if entry.Level < 1 || entry.Level > 255 || entry.Exp < 0 || entry.Exp > int64(^uint32(0)) {
		return nil, fmt.Errorf("%w: level=%d progress=%d", errCreatureGrowthInvalid, entry.Level, entry.Exp)
	}
	body := make([]byte, 6, 8)
	body[0] = byte(entry.Level)
	body[1] = entry.ModeFlag
	binary.LittleEndian.PutUint32(body[2:6], uint32(entry.Exp))
	if entry.ModeFlag == 1 {
		body = append(body, entry.Mode1Field0A, entry.Mode1Field0B)
	}
	return body, nil
}

type packetWriter struct {
	buf []byte
}

func (w *packetWriter) writeByte(value byte) {
	w.buf = append(w.buf, value)
}

func (w *packetWriter) writeUint32(value uint32) {
	var tmp [4]byte
	binary.LittleEndian.PutUint32(tmp[:], value)
	w.buf = append(w.buf, tmp[:]...)
}

func (w *packetWriter) writeRawDstr(value []byte) {
	w.writeUint32(uint32(len(value)))
	w.buf = append(w.buf, value...)
}

func (w *packetWriter) bytes() []byte {
	return append([]byte(nil), w.buf...)
}

func buildSuccessAck() []byte {
	return []byte{1}
}

// buildRenameCreatureSuccessAck follows current NoPack sub_1D1D5C0 exactly:
// u16 slot + u8 list_type. The response does not echo the name.
func buildRenameCreatureSuccessAck(slotIndex int16, listType byte) []byte {
	body := make([]byte, 3)
	binary.LittleEndian.PutUint16(body[0:2], uint16(slotIndex))
	body[2] = listType
	return body
}

// buildRenameCreatureNameNotificationBody follows current NoPack
// sub_1D84FE0 exactly: u16 actor object key followed by the renamed creature
// DSTR. This is the native in-place rename notification; it updates the
// already-created actor/creature objects without replaying mode0 or replacing
// the complete op105 creature table.
func buildRenameCreatureNameNotificationBody(objectKey uint16, nameRaw []byte) []byte {
	body := make([]byte, 2+currentDstrLengthBytes+len(nameRaw))
	binary.LittleEndian.PutUint16(body[0:2], objectKey)
	binary.LittleEndian.PutUint32(body[2:6], uint32(len(nameRaw)))
	copy(body[6:], nameRaw)
	return body
}

// buildCreatureListBody follows current NoPack sub_1D57AB0 exactly:
// u8 count; repeat {u32 key,u8 satiety,u8 mode,u32 exp,
// [mode==1:u8,u8],u8 level,DSTR rawName,u8 tailFlag}.
func buildCreatureListBody(entries []dnfrepo.PetEntry) ([]byte, error) {
	if len(entries) > 255 {
		return nil, errCreatureCountTooLarge
	}
	ordered := append([]dnfrepo.PetEntry(nil), entries...)
	sort.SliceStable(ordered, func(i, j int) bool {
		left, leftOK := creatureWireKey(ordered[i])
		right, rightOK := creatureWireKey(ordered[j])
		if leftOK != rightOK {
			return leftOK
		}
		if left != right {
			return left < right
		}
		return ordered[i].PetKey < ordered[j].PetKey
	})

	var writer packetWriter
	writer.writeByte(byte(len(ordered)))
	for _, entry := range ordered {
		key, ok := creatureWireKey(entry)
		if !ok {
			return nil, fmt.Errorf("%w: %q", errCreatureKeyInvalid, entry.PetKey)
		}
		if entry.ModeFlag > 1 {
			return nil, fmt.Errorf("%w: %d", errCreatureModeInvalid, entry.ModeFlag)
		}
		// sub_1D57AB0 passes max=30 to upper_pkt_read_wstr. Its reader accepts
		// only lengths strictly below max; a 30-byte name would be rejected
		// without consuming the bytes and desynchronize the remainder.
		if len(entry.NameRaw) >= 30 {
			return nil, fmt.Errorf("%w: %d", errCreatureNameTooLong, len(entry.NameRaw))
		}
		if entry.TailFlag > 1 {
			return nil, fmt.Errorf("%w: %d", errCreatureTailInvalid, entry.TailFlag)
		}

		writer.writeUint32(key)
		writer.writeByte(entry.Satiety)
		writer.writeByte(entry.ModeFlag)
		writer.writeUint32(clampUint32(entry.Exp))
		if entry.ModeFlag == 1 {
			writer.writeByte(entry.Mode1Field0A)
			writer.writeByte(entry.Mode1Field0B)
		}
		writer.writeByte(clampByte(entry.Level))
		writer.writeRawDstr(entry.NameRaw)
		writer.writeByte(entry.TailFlag)
	}
	return writer.bytes(), nil
}

func creatureWireKey(entry dnfrepo.PetEntry) (uint32, bool) {
	if entry.CreatureKey != 0 {
		return entry.CreatureKey, true
	}
	raw := strings.TrimSpace(entry.PetKey)
	value, err := strconv.ParseUint(raw, 10, 32)
	if err != nil || value == 0 {
		return 0, false
	}
	return uint32(value), true
}

func buildPetItemListRefreshBody(slots map[string]dnfrepo.ItemStack) []byte {
	return dnfinventory.BuildPetItemListRefreshBody(slots)
}

func clampUint32(value int64) uint32 {
	if value <= 0 {
		return 0
	}
	if value > int64(^uint32(0)) {
		return ^uint32(0)
	}
	return uint32(value)
}

func clampByte(value int64) byte {
	if value <= 0 {
		return 0
	}
	if value > 255 {
		return 255
	}
	return byte(value)
}
