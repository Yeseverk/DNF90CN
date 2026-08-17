// 本文件固定 23.4.15.0 Script.pvf 的角色职业编号和任务 tag 映射。
package jobmap

import "strings"

// MaxID 是当前 Script.pvf character/character.lst 暴露的最大职业编号。
const MaxID = 15

var questTagsByJob = map[int][]string{
	0:  {"[swordman]"},
	1:  {"[fighter]"},
	2:  {"[gunner]"},
	3:  {"[mage]"},
	4:  {"[priest]"},
	5:  {"[at gunner]"},
	6:  {"[thief]"},
	7:  {"[at fighter]"},
	8:  {"[at mage]"},
	9:  {"[demonic swordman]"},
	10: {"[creator mage]"},
	11: {"[at swordman]"},
	12: {"[knight]"},
	13: {"[demonic lancer]"},
	14: {"[at priest]"},
	15: {"[gun blader]"},
}

// Valid 判断职业编号是否存在于当前 PVF 的 0..15 映射。
func Valid(job int) bool {
	_, ok := questTagsByJob[job]
	return ok
}

// QuestTags 返回 C# QuestData.GetQuestJobTags 对齐的任务职业 tag。
func QuestTags(job int) []string {
	tags := questTagsByJob[job]
	if len(tags) == 0 {
		return nil
	}
	return append([]string(nil), tags...)
}

// MatchesQuestTag 对齐 C# QuestData.MatchesJob/MatchesTargetCharacter 的包含判断。
func MatchesQuestTag(text string, job int) bool {
	normalized := strings.ToLower(strings.TrimSpace(text))
	if normalized == "" {
		return false
	}
	for _, tag := range questTagsByJob[job] {
		if strings.Contains(normalized, tag) {
			return true
		}
	}
	return false
}
