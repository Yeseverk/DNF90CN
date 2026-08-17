// 本文件提供 DNF 仓储记录的深拷贝函数。
package repository

import "time"

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneInt64Map(in map[string]int64) map[string]int64 {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]int64, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneInt64Slice(in []int64) []int64 {
	if len(in) == 0 {
		return nil
	}
	return append([]int64(nil), in...)
}

func cloneCharacterRoster(in CharacterRoster) CharacterRoster {
	if len(in.Entry.EquipSummary) != 0 {
		in.Entry.EquipSummary = cloneCharacterRosterEquipSummarySlice(in.Entry.EquipSummary)
	}
	in.Entry.LinkedIDBlock = cloneInt64Slice(in.Entry.LinkedIDBlock)
	in.Entry.Flags = cloneInt64Slice(in.Entry.Flags)
	return in
}

func cloneCharacterRosterEquipSummarySlice(in []CharacterRosterEquipSummary) []CharacterRosterEquipSummary {
	if len(in) == 0 {
		return nil
	}
	out := make([]CharacterRosterEquipSummary, len(in))
	for idx, value := range in {
		value.RawEntry = append([]byte(nil), value.RawEntry...)
		out[idx] = value
	}
	return out
}

func cloneItemMap(in map[string]ItemStack) map[string]ItemStack {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]ItemStack, len(in))
	for key, value := range in {
		value.RawEntry = append([]byte(nil), value.RawEntry...)
		value.Extra = cloneStringMap(value.Extra)
		out[key] = value
	}
	return out
}

func cloneSkillMap(in map[int64]SkillState) map[int64]SkillState {
	if len(in) == 0 {
		return nil
	}
	out := make(map[int64]SkillState, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneTimeMap(in map[int64]time.Time) map[int64]time.Time {
	if len(in) == 0 {
		return nil
	}
	out := make(map[int64]time.Time, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
