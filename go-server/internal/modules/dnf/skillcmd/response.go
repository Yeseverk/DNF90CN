package skillcmd

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

var (
	ErrResponsePointRange = errors.New("skill response point value is out of uint16 range")
	ErrResponseEntryRange = errors.New("skill response entry is out of range")
	ErrResponseSlot       = errors.New("skill response slot is unresolved")
)

// BuildChangeSkillSlotSuccess matches current class1/op28 sub_1D0A4B0. The
// dispatcher consumes the leading success byte; the handler then reads exactly
// tree, from, and to before swapping the client-side vector.
func BuildChangeSkillSlotSuccess(result SlotMutationResult) ([]byte, error) {
	cmd := Command{
		SkillTree:    result.SkillTree,
		From:         result.From,
		To:           result.To,
		ContextIndex: -1,
		Mode:         0,
	}
	if err := validateSkillSlotCommand(cmd); err != nil {
		return nil, err
	}
	return []byte{1, result.SkillTree, result.From, result.To}, nil
}

// BuildChangeAnotherSkillTreeSuccess matches current class1/op260
// sub_1D0C8F0: the dispatcher consumes success and the reader consumes exactly
// one target-tree byte.
func BuildChangeAnotherSkillTreeSuccess(result TreeSwitchMutationResult) ([]byte, error) {
	if result.Current >= currentEXESkillTreeCount || result.Target >= currentEXESkillTreeCount || result.Target == result.Current {
		return nil, fmt.Errorf("%w: current=%d target=%d", ErrSkillTree, result.Current, result.Target)
	}
	return []byte{1, result.Target}, nil
}

// BuildChangeAnotherSkillTreeFailure is the live server's bounded op260
// rejection: failure followed by resource/error code 19.
func BuildChangeAnotherSkillTreeFailure() []byte {
	return []byte{0, 19}
}

// BuildBuySkillSuccess encodes the complete current upper body. The leading
// success byte is consumed by the dispatcher before sub_1D1E080 reads tree.
func BuildBuySkillSuccess(result MutationResult) ([]byte, error) {
	if err := validateBuySkillTree(result.SkillTree); err != nil {
		return nil, err
	}
	if result.Points.RemainingSP < 0 || result.Points.RemainingSP > math.MaxUint16 ||
		result.Points.RemainingTP < 0 || result.Points.RemainingTP > math.MaxUint16 {
		return nil, ErrResponsePointRange
	}
	if len(result.Entries) > math.MaxUint8 {
		return nil, ErrResponseEntryRange
	}
	size := 8
	for _, entry := range result.Entries {
		if entry.Slot < 0 || entry.Slot > math.MaxUint8 {
			return nil, fmt.Errorf("%w: skill=%d slot=%d", ErrResponseSlot, entry.SkillID, entry.Slot)
		}
		if entry.Level < 0 || entry.Level > math.MaxUint8 || len(entry.CommandData) > math.MaxUint8 {
			return nil, fmt.Errorf("%w: skill=%d level=%d command=%d", ErrResponseEntryRange, entry.SkillID, entry.Level, len(entry.CommandData))
		}
		size += 5
		if len(entry.CommandData) > 0 {
			size += 1 + len(entry.CommandData)
		}
	}
	body := make([]byte, 0, size)
	body = append(body, 1, result.SkillTree)
	var number [2]byte
	binary.LittleEndian.PutUint16(number[:], uint16(result.Points.RemainingSP))
	body = append(body, number[:]...)
	binary.LittleEndian.PutUint16(number[:], uint16(result.Points.RemainingTP))
	body = append(body, number[:]...)
	body = append(body, byte(len(result.Entries)))
	for _, entry := range result.Entries {
		body = append(body, byte(entry.Slot))
		binary.LittleEndian.PutUint16(number[:], entry.SkillID)
		body = append(body, number[:]...)
		body = append(body, byte(entry.Level))
		if len(entry.CommandData) == 0 {
			body = append(body, 0)
			continue
		}
		body = append(body, 1, byte(len(entry.CommandData)))
		body = append(body, entry.CommandData...)
	}
	body = append(body, result.FinalMode)
	return body, nil
}

func BuildBuySkillFailure(code byte) []byte {
	return []byte{0, code}
}

// BuildSkillInitSuccess matches current class1/op491 sub_1FEC390. The common
// dispatcher consumes byte 0, then the handler reads tree and a non-zero
// success/refresh flag.
func BuildSkillInitSuccess(result ResetMutationResult) ([]byte, error) {
	if err := validateBuySkillTree(result.SkillTree); err != nil {
		return nil, err
	}
	return []byte{1, result.SkillTree, 1}, nil
}
