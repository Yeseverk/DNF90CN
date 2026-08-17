package progression

import (
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

type questExperienceRules struct {
	baseByLevel      []uint32
	goldByLevel      []uint32
	difficultyWeight map[rune]int
	greenPenalty     int
	greyPenalty      int
}

// DungeonClearBaseExperience returns the exact level row from the runtime
// PVF [exp reward table]. It deliberately applies no difficulty, rank,
// premium, party, or account modifier; the dungeon settlement owner must
// combine only modifiers it has independently proved.
func (t *Tables) DungeonClearBaseExperience(dungeonBasisLevel int) (uint32, error) {
	if t == nil || dungeonBasisLevel <= 0 || dungeonBasisLevel > len(t.quest.baseByLevel) {
		return 0, fmt.Errorf(
			"%w: dungeon basis level=%d entries=%d",
			ErrQuestExperienceLookup,
			dungeonBasisLevel,
			func() int {
				if t == nil {
					return 0
				}
				return len(t.quest.baseByLevel)
			}(),
		)
	}
	return t.quest.baseByLevel[dungeonBasisLevel-1], nil
}

// QuestExperience computes the PVF quest-parameter value only. It is a dormant
// domain rule until a quest definition's reward fields and the current EXE's
// settlement category/timing are both proven.
func (t *Tables) QuestExperience(playerLevel, questMinLevel int, difficulty rune, ignoreLevel bool) (uint32, error) {
	if t == nil || playerLevel <= 0 || questMinLevel <= 0 {
		return 0, fmt.Errorf("%w: player=%d quest_min=%d", ErrLevelOutOfRange, playerLevel, questMinLevel)
	}
	lookupLevel := questMinLevel
	if ignoreLevel {
		lookupLevel = playerLevel
	}
	if lookupLevel > len(t.quest.baseByLevel) {
		return 0, fmt.Errorf("%w: level=%d entries=%d", ErrQuestExperienceLookup, lookupLevel, len(t.quest.baseByLevel))
	}
	weight, ok := t.quest.difficultyWeight[normalizeDifficulty(difficulty)]
	if !ok {
		return 0, fmt.Errorf("%w: %q", ErrQuestDifficulty, string(difficulty))
	}
	penalty := 100
	if !ignoreLevel {
		difference := playerLevel - questMinLevel
		switch {
		case difference > 11:
			penalty = t.quest.greyPenalty
		case difference > 6:
			penalty = t.quest.greenPenalty
		}
	}
	value := uint64(t.quest.baseByLevel[lookupLevel-1])
	value = uint64(penalty) * ((uint64(weight) * value / 100) / 100)
	if value > math.MaxUint32 {
		return 0, fmt.Errorf("%w: quest experience=%d", ErrExperienceOutOfRange, value)
	}
	return uint32(value), nil
}

// QuestGold ports the C# QuestParameterTable.ComputeGoldReward domain rule:
// [gold reward table] supplies the base row and quest [gold multiple] (or a
// reward-int item 0 row with count) supplies the percentage multiplier. This is
// pure PVF/accounting data; packet ACK shape is still owned by the bridge.
func (t *Tables) QuestGold(playerLevel, questMinLevel, goldMultiple int, ignoreLevel bool) (uint32, error) {
	if t == nil || playerLevel <= 0 || questMinLevel <= 0 {
		return 0, fmt.Errorf("%w: player=%d quest_min=%d", ErrLevelOutOfRange, playerLevel, questMinLevel)
	}
	if len(t.quest.goldByLevel) == 0 {
		return 0, fmt.Errorf("%w: %s [gold reward table]", ErrTableEmpty, QuestParameterPath)
	}
	if goldMultiple <= 0 {
		goldMultiple = 100
	}
	lookupLevel := questMinLevel
	if ignoreLevel {
		lookupLevel = playerLevel
	}
	if lookupLevel > len(t.quest.goldByLevel) {
		return 0, fmt.Errorf("%w: level=%d entries=%d", ErrQuestExperienceLookup, lookupLevel, len(t.quest.goldByLevel))
	}
	penalty := 100
	if !ignoreLevel {
		difference := playerLevel - questMinLevel
		switch {
		case difference > 11:
			penalty = t.quest.greyPenalty
		case difference > 6:
			penalty = t.quest.greenPenalty
		}
	}
	value := uint64(goldMultiple) * uint64(penalty) * uint64(t.quest.goldByLevel[lookupLevel-1]) / 100 / 100
	if value > math.MaxUint32 {
		return 0, fmt.Errorf("%w: quest gold=%d", ErrExperienceOutOfRange, value)
	}
	return uint32(value), nil
}

func parseQuestExperienceRules(doc *dnfpvf.Document) (questExperienceRules, error) {
	if doc == nil {
		return questExperienceRules{}, fmt.Errorf("%w: %s", ErrTableEmpty, QuestParameterPath)
	}
	rules := questExperienceRules{
		difficultyWeight: make(map[rune]int),
	}
	expValues := doc.Ints("exp reward table")
	if len(expValues) == 0 || len(expValues)%2 != 0 {
		return questExperienceRules{}, fmt.Errorf("%w: %s [exp reward table] integers=%d", ErrTableMalformed, QuestParameterPath, len(expValues))
	}
	rules.baseByLevel = make([]uint32, 0, len(expValues)/2)
	for index := 0; index < len(expValues); index += 2 {
		value := expValues[index]
		if value < 0 || value > math.MaxUint32 {
			return questExperienceRules{}, fmt.Errorf("%w: %s quest base=%d", ErrTableMalformed, QuestParameterPath, value)
		}
		rules.baseByLevel = append(rules.baseByLevel, uint32(value))
	}
	goldValues := doc.Ints("gold reward table")
	if len(goldValues)%2 != 0 {
		return questExperienceRules{}, fmt.Errorf("%w: %s [gold reward table] integers=%d", ErrTableMalformed, QuestParameterPath, len(goldValues))
	}
	rules.goldByLevel = make([]uint32, 0, len(goldValues)/2)
	for index := 0; index < len(goldValues); index += 2 {
		value := goldValues[index]
		if value < 0 || value > math.MaxUint32 {
			return questExperienceRules{}, fmt.Errorf("%w: %s quest gold=%d", ErrTableMalformed, QuestParameterPath, value)
		}
		rules.goldByLevel = append(rules.goldByLevel, uint32(value))
	}
	difficultyTokens := sectionTokens(doc, "difficulty")
	for index := 0; index < len(difficultyTokens); index++ {
		token := difficultyTokens[index]
		if token.Kind != dnfpvf.TokenString && token.Kind != dnfpvf.TokenIdent {
			continue
		}
		key := strings.TrimSpace(token.Value)
		r, width := utf8.DecodeRuneInString(key)
		if r == utf8.RuneError || width != len(key) {
			continue
		}
		for next := index + 1; next < len(difficultyTokens); next++ {
			if difficultyTokens[next].Kind != dnfpvf.TokenInt {
				continue
			}
			value := difficultyTokens[next].Int
			if value <= 0 || value > math.MaxInt {
				return questExperienceRules{}, fmt.Errorf("%w: %s difficulty=%q weight=%d", ErrTableMalformed, QuestParameterPath, key, value)
			}
			rules.difficultyWeight[normalizeDifficulty(r)] = int(value)
			index = next
			break
		}
	}
	if len(rules.difficultyWeight) == 0 {
		return questExperienceRules{}, fmt.Errorf("%w: %s [difficulty]", ErrTableEmpty, QuestParameterPath)
	}
	green, ok := doc.Int("green level penalty")
	if !ok || green < 0 || green > 100 {
		return questExperienceRules{}, fmt.Errorf("%w: %s green penalty=%d present=%t", ErrTableMalformed, QuestParameterPath, green, ok)
	}
	grey, ok := doc.Int("grey level penalty")
	if !ok || grey < 0 || grey > 100 {
		return questExperienceRules{}, fmt.Errorf("%w: %s grey penalty=%d present=%t", ErrTableMalformed, QuestParameterPath, grey, ok)
	}
	rules.greenPenalty = int(green)
	rules.greyPenalty = int(grey)
	return rules, nil
}

func normalizeDifficulty(value rune) rune {
	return []rune(strings.ToUpper(string(value)))[0]
}
