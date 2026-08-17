// Package profession owns the PVF-backed character advancement state machine.
// It contains no network opcodes and performs no repository I/O.
package profession

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

var (
	ErrRewardUnsupported   = errors.New("dnf profession reward type is unsupported")
	ErrRewardMalformed     = errors.New("dnf profession reward data is malformed")
	ErrTransitionInvalid   = errors.New("dnf profession transition is invalid")
	ErrTransitionOutOfStep = errors.New("dnf profession transition is out of sequence")
)

type Kind uint8

const (
	KindNone Kind = iota
	KindClassChange
	KindAwakening
)

// Transition is the detached result of applying one final profession quest
// reward to the packed grow_type byte. Low four bits select the class branch;
// high four bits select the awakening stage.
type Transition struct {
	Kind             Kind
	ChainType        byte
	GrowNumber       byte
	PreviousGrowType byte
	NewGrowType      byte
	FirstGrowType    byte
	AwakeningStage   byte
}

// Request is the detached profession reward encoded by one quest definition.
// It contains no character state; the repository owner must resolve it through
// the job's PVF profile inside the same transaction as the quest completion.
type Request struct {
	Kind           Kind
	ChainType      byte
	GrowNumber     byte
	JobChangeQuest int
}

func Decode(growType byte) (first byte, awakening byte) {
	return growType & 0x0f, (growType >> 4) & 0x0f
}

func Encode(first byte, awakening byte) (byte, error) {
	if first > 0x0f || awakening > 0x0f {
		return 0, fmt.Errorf("%w: first=%d awakening=%d", ErrTransitionInvalid, first, awakening)
	}
	return awakening<<4 | first, nil
}

// ParseReward accepts only the two profession reward tags used by the runtime
// PVF. It deliberately does not inspect or change grow_type.
func ParseReward(jobChangeQuest int, rewardType string, rewardData []int64) (Request, error) {
	tag := normalizeTag(rewardType)
	if tag != "grow type" && tag != "awakening type" {
		return Request{}, fmt.Errorf("%w: type=%q", ErrRewardUnsupported, rewardType)
	}
	if len(rewardData) == 0 || rewardData[0] <= 0 || rewardData[0] > math.MaxUint8 {
		return Request{}, fmt.Errorf("%w: type=%q values=%v", ErrRewardMalformed, rewardType, rewardData)
	}

	value := byte(rewardData[0])
	switch tag {
	case "grow type":
		if value > 0x0f {
			return Request{}, fmt.Errorf("%w: class=%d", ErrTransitionInvalid, value)
		}
		if jobChangeQuest != 0 && jobChangeQuest != 1 {
			return Request{}, fmt.Errorf("%w: class change quest marker=%d", ErrTransitionInvalid, jobChangeQuest)
		}
		return Request{
			Kind: KindClassChange, ChainType: 1, GrowNumber: value, JobChangeQuest: jobChangeQuest,
		}, nil
	case "awakening type":
		if value > 2 {
			return Request{}, fmt.Errorf("%w: awakening target=%d", ErrTransitionInvalid, value)
		}
		if jobChangeQuest != 0 && jobChangeQuest != int(value)+1 {
			return Request{}, fmt.Errorf("%w: awakening target=%d quest marker=%d", ErrTransitionInvalid, value, jobChangeQuest)
		}
		return Request{
			Kind: KindAwakening, ChainType: 2, GrowNumber: value, JobChangeQuest: jobChangeQuest,
		}, nil
	default:
		return Request{}, fmt.Errorf("%w: type=%q", ErrRewardUnsupported, rewardType)
	}
}

func normalizeTag(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) >= 2 && value[0] == '[' && value[len(value)-1] == ']' {
		value = strings.TrimSpace(value[1 : len(value)-1])
	}
	return value
}
