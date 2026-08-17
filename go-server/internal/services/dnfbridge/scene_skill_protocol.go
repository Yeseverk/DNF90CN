package dnfbridge

import (
	"encoding/binary"
	"fmt"
	"sort"

	"google.golang.org/protobuf/encoding/protowire"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const (
	currentSkillInfoMsgID       = 19
	currentSkillInfoMessageType = 19
	currentSkillInfoTreeIndex   = 0
	currentSkillInfoTreeCount   = 2
)

func buildCurrentSceneSkillInfoBody(record dnfrepo.SkillRecord, layout dnfrepo.SkillLayout) ([]byte, []int, error) {
	slots := make([]int, 0, len(layout))
	for slot, skillID := range layout {
		state, ok := record.Skills[int64(skillID)]
		if !ok || state.Level <= 0 {
			continue
		}
		if slot < 0 || slot > 0x7fffffff {
			return nil, nil, fmt.Errorf("current skill slot out of int32 range: slot=%d skill=%d", slot, skillID)
		}
		slots = append(slots, slot)
	}
	sort.Ints(slots)

	var tree []byte
	tree = appendProtoVarint(tree, 1, uint64(nonNegativeInt(record.Points.RemainingSP)))
	tree = appendProtoVarint(tree, 2, uint64(nonNegativeInt(record.Points.RemainingTP)))
	activeSlots := make([]int, 0, 6)
	for _, slot := range slots {
		skillID := layout[slot]
		state := record.Skills[int64(skillID)]
		if state.Level > 0x7fffffff {
			return nil, nil, fmt.Errorf("current skill level out of client rank range: skill=%d level=%d", skillID, state.Level)
		}
		var skill []byte
		skill = appendProtoVarint(skill, 1, uint64(slot))
		skill = appendProtoVarint(skill, 2, uint64(skillID))
		// sub_2666AE0 preserves the wire rank unless a current actor mapping
		// explicitly replaces it. Persisted learned levels therefore stay direct.
		skill = appendProtoVarint(skill, 3, uint64(state.Level))
		// Current EXE skill field 4 is the optional per-skill command-customization
		// byte vector also applied by op331. SkillRecord has no owner for that
		// state yet, so omit the field instead of fabricating an empty/default
		// command mapping. Protobuf absence is the reader's valid empty state.
		tree = appendProtoBytes(tree, 3, skill)
		if slot >= 0 && slot < 6 {
			activeSlots = append(activeSlots, slot)
		}
	}

	var message []byte
	message = appendProtoVarint(message, 1, currentSkillInfoMessageType)
	// sub_1D6E240 indexes repeated SkillTree messages by their repeated-field
	// position, then restores the actor's selected tree. Fresh characters need
	// the same initial learned skills in both pages, matching the PVF/C# source.
	for index := 0; index < currentSkillInfoTreeCount; index++ {
		message = appendProtoBytes(message, 2, tree)
	}
	body := make([]byte, 4, 4+len(message))
	binary.LittleEndian.PutUint32(body, uint32(len(message)))
	body = append(body, message...)
	return body, activeSlots, nil
}

func appendProtoVarint(dst []byte, field protowire.Number, value uint64) []byte {
	dst = protowire.AppendTag(dst, field, protowire.VarintType)
	return protowire.AppendVarint(dst, value)
}

func appendProtoBytes(dst []byte, field protowire.Number, value []byte) []byte {
	dst = protowire.AppendTag(dst, field, protowire.BytesType)
	return protowire.AppendBytes(dst, value)
}

func nonNegativeInt(value int) int {
	if value < 0 {
		return 0
	}
	return value
}
