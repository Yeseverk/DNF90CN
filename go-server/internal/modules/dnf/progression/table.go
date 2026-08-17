package progression

import (
	"context"
	"errors"
	"fmt"
	"math"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

const (
	ExperienceTablePath = "character/ExpTable.tbl"
	SkillPointTablePath = "Etc/spTable.etc"
	QuestParameterPath  = "n_quest/questParameter.etc"
)

var (
	ErrTableEmpty            = errors.New("dnf progression PVF table is empty")
	ErrTableMalformed        = errors.New("dnf progression PVF table is malformed")
	ErrLevelOutOfRange       = errors.New("dnf progression level is outside the loaded PVF table")
	ErrExperienceOutOfRange  = errors.New("dnf progression experience is outside the current EXE u32 range")
	ErrSkillPointLedger      = errors.New("dnf progression skill-point ledger is invalid")
	ErrQuestDifficulty       = errors.New("dnf progression quest difficulty is absent from PVF")
	ErrQuestExperienceLookup = errors.New("dnf progression quest experience level is absent from PVF")
)

// Tables is an immutable snapshot of the current runtime PVF progression data.
type Tables struct {
	experienceThresholds []uint32
	spByLevel            map[int]int
	tpByLevel            map[int]int
	quest                questExperienceRules
}

type Snapshot struct {
	ExperienceThresholds  int
	SkillPointLevels      int
	TechniquePointLevels  int
	QuestExperienceLevels int
	QuestGoldLevels       int
	QuestDifficulties     int
}

// Load reads only the three typed progression files. No C# constants or packet
// captures are used as data fallbacks.
func Load(ctx context.Context, source dnfpvf.Source) (*Tables, error) {
	if source == nil {
		return nil, dnfpvf.ErrSourceRequired
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	expDoc, err := readDocument(source, ExperienceTablePath)
	if err != nil {
		return nil, err
	}
	thresholds, err := parseExperienceThresholds(expDoc)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	spDoc, err := readDocument(source, SkillPointTablePath)
	if err != nil {
		return nil, err
	}
	spByLevel, tpByLevel, err := parseSkillPointTable(spDoc)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	questDoc, err := readDocument(source, QuestParameterPath)
	if err != nil {
		return nil, err
	}
	quest, err := parseQuestExperienceRules(questDoc)
	if err != nil {
		return nil, err
	}
	return &Tables{
		experienceThresholds: thresholds,
		spByLevel:            spByLevel,
		tpByLevel:            tpByLevel,
		quest:                quest,
	}, nil
}

func (t *Tables) Snapshot() Snapshot {
	if t == nil {
		return Snapshot{}
	}
	return Snapshot{
		ExperienceThresholds:  len(t.experienceThresholds),
		SkillPointLevels:      len(t.spByLevel),
		TechniquePointLevels:  len(t.tpByLevel),
		QuestExperienceLevels: len(t.quest.baseByLevel),
		QuestGoldLevels:       len(t.quest.goldByLevel),
		QuestDifficulties:     len(t.quest.difficultyWeight),
	}
}

func readDocument(source dnfpvf.Source, path string) (*dnfpvf.Document, error) {
	text, err := source.ReadText(path)
	if err != nil {
		return nil, fmt.Errorf("read dnf progression PVF %q: %w", path, err)
	}
	doc, err := dnfpvf.Parse(path, text)
	if err != nil {
		return nil, fmt.Errorf("parse dnf progression PVF %q: %w", path, err)
	}
	return doc, nil
}

func parseExperienceThresholds(doc *dnfpvf.Document) ([]uint32, error) {
	if doc == nil {
		return nil, fmt.Errorf("%w: %s", ErrTableEmpty, ExperienceTablePath)
	}
	values := make([]uint32, 0, len(doc.Tokens))
	for _, token := range doc.Tokens {
		if token.Kind != dnfpvf.TokenInt {
			continue
		}
		if token.Int < 0 || token.Int > math.MaxUint32 {
			return nil, fmt.Errorf("%w: %s value=%d", ErrExperienceOutOfRange, ExperienceTablePath, token.Int)
		}
		value := uint32(token.Int)
		// The runtime table uses repeated MaxInt32 sentinels above its playable
		// range. Equal adjacent sentinels are valid; a decrease is not.
		if len(values) > 0 && value < values[len(values)-1] {
			return nil, fmt.Errorf("%w: %s threshold[%d]=%d previous=%d", ErrTableMalformed, ExperienceTablePath, len(values), value, values[len(values)-1])
		}
		values = append(values, value)
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrTableEmpty, ExperienceTablePath)
	}
	return values, nil
}

func parseSkillPointTable(doc *dnfpvf.Document) (map[int]int, map[int]int, error) {
	if doc == nil {
		return nil, nil, fmt.Errorf("%w: %s", ErrTableEmpty, SkillPointTablePath)
	}
	sp, err := parseSkillPointSection(doc, "sp table")
	if err != nil {
		return nil, nil, err
	}
	tp, err := parseSkillPointSection(doc, "tp table")
	if err != nil {
		return nil, nil, err
	}
	return sp, tp, nil
}

func parseSkillPointSection(doc *dnfpvf.Document, section string) (map[int]int, error) {
	values := doc.Ints(section)
	if len(values) == 0 {
		return nil, fmt.Errorf("%w: %s [%s]", ErrTableEmpty, SkillPointTablePath, section)
	}
	if len(values)%2 != 0 {
		return nil, fmt.Errorf("%w: %s [%s] integers=%d", ErrTableMalformed, SkillPointTablePath, section, len(values))
	}
	table := make(map[int]int, len(values)/2)
	for index := 0; index < len(values); index += 2 {
		levelValue, spValue := values[index], values[index+1]
		if levelValue <= 0 || levelValue > math.MaxInt || spValue < 0 || spValue > math.MaxInt {
			return nil, fmt.Errorf("%w: %s [%s] level=%d points=%d", ErrTableMalformed, SkillPointTablePath, section, levelValue, spValue)
		}
		level := int(levelValue)
		if _, duplicate := table[level]; duplicate {
			return nil, fmt.Errorf("%w: %s [%s] duplicate level=%d", ErrTableMalformed, SkillPointTablePath, section, level)
		}
		table[level] = int(spValue)
	}
	return table, nil
}

func sectionTokens(doc *dnfpvf.Document, name string) []dnfpvf.Token {
	if doc == nil {
		return nil
	}
	tokens, ok := doc.Section(name)
	if !ok {
		return nil
	}
	return tokens
}
