// Package honor provides typed, read-only honor progression rules from the
// runtime PVF. It intentionally contains no packet, persistence, or award
// side effects.
package honor

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

const TablePath = "etc/honorlevel.etc"

const (
	// ExpertLevelStatKey and ExpertProgressExperienceStatKey are the durable
	// character-stat keys for the current client's HonorExpert actor state.
	ExpertLevelStatKey              = "honor_expert_level"
	ExpertProgressExperienceStatKey = "honor_expert_progress_experience"
)

var (
	ErrSourceRequired      = errors.New("dnf honor PVF source is required")
	ErrTablesRequired      = errors.New("dnf honor tables are required")
	ErrSectionMissing      = errors.New("dnf honor PVF section is missing")
	ErrSectionShape        = errors.New("dnf honor PVF section shape is invalid")
	ErrCharacterCapInvalid = errors.New("dnf honor character experience cap is invalid")
	ErrExpertStateInvalid  = errors.New("dnf honor expert state is invalid")
)

// ImageReference preserves one exact PVF image path/index pair. It does not
// imply that the server owns client rendering of that image.
type ImageReference struct {
	Path  string
	Index int
}

// LevelRequirement is the amount consumed when entering Level. Level 1 is
// required to be present with a zero requirement.
type LevelRequirement struct {
	Level              int
	RequiredExperience uint64
}

// OrdinaryGrade preserves one repeated [grade] section and its ordinary
// honor-level rows.
type OrdinaryGrade struct {
	Grade      int
	EffectPath string
	Medal      ImageReference
	Icon       ImageReference
	Levels     []LevelRequirement
}

// ExpertGradeInfo preserves one [grade info] block under [expert info].
// Medal/Icon are optional because the runtime PVF's grade zero has neither.
// MaxLevel == -1 is preserved as the PVF's open-ended sentinel.
type ExpertGradeInfo struct {
	Grade    int
	Name     string
	Medal    ImageReference
	HasMedal bool
	Icon     ImageReference
	HasIcon  bool
	MinLevel int
	MaxLevel int
}

// ExpertExperienceRow is the amount required to enter Level from the prior
// HonorExpert level. The current EXE keeps level zero as the challenger state,
// so row one is the threshold for the first expert-level advance.
type ExpertExperienceRow struct {
	Level      int
	Experience uint64
}

// ExpertProgress is the character-scoped HonorExpert state carried by the
// current client. CurrentLevelExperience is always progress towards the next
// level, never a cumulative cross-level total.
type ExpertProgress struct {
	Level                  uint32
	CurrentLevelExperience uint64
}

// ExpertProgressFromStats decodes the durable character-scoped state. Older
// records with no expert keys are the valid level-zero challenger state.
func ExpertProgressFromStats(stats map[string]int64) (ExpertProgress, error) {
	level := int64(0)
	progress := int64(0)
	if stats != nil {
		level = stats[ExpertLevelStatKey]
		progress = stats[ExpertProgressExperienceStatKey]
	}
	if level < 0 || uint64(level) > math.MaxUint32 || progress < 0 {
		return ExpertProgress{}, fmt.Errorf(
			"%w: level=%d progress=%d",
			ErrExpertStateInvalid,
			level,
			progress,
		)
	}
	return ExpertProgress{Level: uint32(level), CurrentLevelExperience: uint64(progress)}, nil
}

// ExpertStats returns the durable representation of one already-validated
// HonorExpert state.
func ExpertStats(progress ExpertProgress) (map[string]int64, error) {
	if progress.CurrentLevelExperience > math.MaxInt64 {
		return nil, fmt.Errorf("%w: progress=%d exceeds persistence range", ErrExpertStateInvalid, progress.CurrentLevelExperience)
	}
	return map[string]int64{
		ExpertLevelStatKey:              int64(progress.Level),
		ExpertProgressExperienceStatKey: int64(progress.CurrentLevelExperience),
	}, nil
}

// Snapshot reports source coverage without exposing mutable internal maps.
type Snapshot struct {
	OrdinaryGrades         int
	OrdinaryLevels         int
	MaxOrdinaryLevel       int
	MaxLevelExperience     uint64
	MaxTotalExperience     uint64
	ExpertGrades           int
	ExpertExperienceRows   int
	ExpertCalculationReady bool
}

// Progress is the ordinary honor state derived from account-scoped cumulative
// honor experience.
type Progress struct {
	TotalExperience        uint64
	Level                  int
	CurrentLevelExperience uint64
	Grade                  int
}

// CharacterExperienceCap is supplied by the caller because the character
// level cap and cumulative EXP threshold are version/runtime data, not honor
// constants.
type CharacterExperienceCap struct {
	MaxLevel                int
	MaxLevelEntryExperience uint32
}

// Tables is an immutable typed view of etc/honorlevel.etc.
type Tables struct {
	ordinaryGrades       []OrdinaryGrade
	requirementByLevel   []uint64
	gradeByLevel         []int
	maxLevelExperience   uint64
	maxTotalExperience   uint64
	expertGrades         []ExpertGradeInfo
	expertExperienceRows []ExpertExperienceRow
}

// LoadTables reads and strictly parses the current runtime PVF honor table.
func LoadTables(source dnfpvf.Source) (*Tables, error) {
	if source == nil {
		return nil, ErrSourceRequired
	}
	text, err := source.ReadText(TablePath)
	if err != nil {
		return nil, fmt.Errorf("read dnf honor PVF path %s: %w", TablePath, err)
	}
	document, err := dnfpvf.Parse(TablePath, text)
	if err != nil {
		return nil, fmt.Errorf("parse dnf honor PVF path %s: %w", TablePath, err)
	}
	return ParseTables(document)
}

// ParseTables strictly converts one already-tokenized honor PVF document.
func ParseTables(document *dnfpvf.Document) (*Tables, error) {
	if document == nil {
		return nil, ErrTablesRequired
	}
	if !strings.EqualFold(strings.TrimSpace(document.Path), TablePath) {
		return nil, fmt.Errorf("%w: path=%s want=%s", ErrSectionShape, document.Path, TablePath)
	}

	ordinaryGrades, requirements, gradeByLevel, maxSectionIndex, err := parseOrdinaryGrades(document)
	if err != nil {
		return nil, err
	}
	maxLevelExperience, err := parseMaxLevelExperience(document, maxSectionIndex)
	if err != nil {
		return nil, err
	}
	expertGrades, expertCloseIndex, err := parseExpertGrades(document, maxSectionIndex)
	if err != nil {
		return nil, err
	}
	expertRows, err := parseExpertExperienceRows(document, expertCloseIndex)
	if err != nil {
		return nil, err
	}

	maxTotalExperience := maxLevelExperience
	for level := 2; level < len(requirements); level++ {
		required := requirements[level]
		if math.MaxUint64-maxTotalExperience < required {
			return nil, fmt.Errorf("%w: ordinary total experience overflow at level=%d", ErrSectionShape, level)
		}
		maxTotalExperience += required
	}

	return &Tables{
		ordinaryGrades:       ordinaryGrades,
		requirementByLevel:   requirements,
		gradeByLevel:         gradeByLevel,
		maxLevelExperience:   maxLevelExperience,
		maxTotalExperience:   maxTotalExperience,
		expertGrades:         expertGrades,
		expertExperienceRows: expertRows,
	}, nil
}

func parseOrdinaryGrades(document *dnfpvf.Document) ([]OrdinaryGrade, []uint64, []int, int, error) {
	gradeIndices := sectionIndices(document, "grade")
	if len(gradeIndices) == 0 {
		return nil, nil, nil, -1, fmt.Errorf("%w: path=%s section=grade", ErrSectionMissing, TablePath)
	}
	maxIndices := sectionIndices(document, "maxexp on maxlevel")
	if len(maxIndices) != 1 {
		return nil, nil, nil, -1, fmt.Errorf("%w: path=%s section=maxexp on maxlevel count=%d want=1", ErrSectionShape, TablePath, len(maxIndices))
	}
	maxSectionIndex := maxIndices[0]

	grades := make([]OrdinaryGrade, 0, len(gradeIndices))
	requirements := []uint64{0}
	gradeByLevel := []int{0}
	wantGrade := 1
	wantLevel := 1
	wantSectionIndex := 0
	for _, sectionIndex := range gradeIndices {
		if sectionIndex != wantSectionIndex {
			return nil, nil, nil, -1, fmt.Errorf("%w: path=%s ordinary grade section_index=%d want=%d", ErrSectionShape, TablePath, sectionIndex, wantSectionIndex)
		}
		if sectionIndex >= maxSectionIndex {
			return nil, nil, nil, -1, fmt.Errorf("%w: path=%s ordinary grade after maxexp section", ErrSectionShape, TablePath)
		}
		if sectionIndex+1 >= len(document.Sections) || sectionName(document.Sections[sectionIndex+1].Name) != "/grade" {
			return nil, nil, nil, -1, fmt.Errorf("%w: path=%s section=grade index=%d missing_close", ErrSectionShape, TablePath, sectionIndex)
		}
		if err := requireEmptySection(document, sectionIndex+1); err != nil {
			return nil, nil, nil, -1, err
		}
		tokens, err := tokensForSection(document, sectionIndex)
		if err != nil {
			return nil, nil, nil, -1, err
		}
		if len(tokens) < 8 || (len(tokens)-6)%2 != 0 {
			return nil, nil, nil, -1, fmt.Errorf("%w: path=%s section=grade values=%d", ErrSectionShape, TablePath, len(tokens))
		}
		grade, err := positiveIntToken(tokens[0], "grade.grade")
		if err != nil {
			return nil, nil, nil, -1, err
		}
		if grade != wantGrade {
			return nil, nil, nil, -1, fmt.Errorf("%w: path=%s section=grade grade=%d want=%d", ErrSectionShape, TablePath, grade, wantGrade)
		}
		effectPath, err := nonemptyStringToken(tokens[1], "grade.effect")
		if err != nil {
			return nil, nil, nil, -1, err
		}
		medalPath, err := nonemptyStringToken(tokens[2], "grade.medal.path")
		if err != nil {
			return nil, nil, nil, -1, err
		}
		medalIndex, err := nonnegativeIntToken(tokens[3], "grade.medal.index")
		if err != nil {
			return nil, nil, nil, -1, err
		}
		iconPath, err := nonemptyStringToken(tokens[4], "grade.icon.path")
		if err != nil {
			return nil, nil, nil, -1, err
		}
		iconIndex, err := nonnegativeIntToken(tokens[5], "grade.icon.index")
		if err != nil {
			return nil, nil, nil, -1, err
		}

		ordinary := OrdinaryGrade{
			Grade:      grade,
			EffectPath: effectPath,
			Medal:      ImageReference{Path: medalPath, Index: medalIndex},
			Icon:       ImageReference{Path: iconPath, Index: iconIndex},
			Levels:     make([]LevelRequirement, 0, (len(tokens)-6)/2),
		}
		for offset := 6; offset < len(tokens); offset += 2 {
			level, err := positiveIntToken(tokens[offset], "grade.level")
			if err != nil {
				return nil, nil, nil, -1, err
			}
			if level != wantLevel {
				return nil, nil, nil, -1, fmt.Errorf("%w: path=%s section=grade level=%d want=%d", ErrSectionShape, TablePath, level, wantLevel)
			}
			required, err := nonnegativeIntegerUint64Token(tokens[offset+1], "grade.required_experience")
			if err != nil {
				return nil, nil, nil, -1, err
			}
			if (level == 1 && required != 0) || (level > 1 && required == 0) {
				return nil, nil, nil, -1, fmt.Errorf("%w: path=%s section=grade level=%d required_experience=%d", ErrSectionShape, TablePath, level, required)
			}
			ordinary.Levels = append(ordinary.Levels, LevelRequirement{Level: level, RequiredExperience: required})
			requirements = append(requirements, required)
			gradeByLevel = append(gradeByLevel, grade)
			wantLevel++
		}
		grades = append(grades, ordinary)
		wantGrade++
		wantSectionIndex = sectionIndex + 2
	}
	if maxSectionIndex != wantSectionIndex {
		return nil, nil, nil, -1, fmt.Errorf("%w: path=%s section=maxexp on maxlevel index=%d want=%d", ErrSectionShape, TablePath, maxSectionIndex, wantSectionIndex)
	}
	return grades, requirements, gradeByLevel, maxSectionIndex, nil
}

func parseMaxLevelExperience(document *dnfpvf.Document, maxSectionIndex int) (uint64, error) {
	tokens, err := tokensForSection(document, maxSectionIndex)
	if err != nil {
		return 0, err
	}
	if len(tokens) != 1 {
		return 0, fmt.Errorf("%w: path=%s section=maxexp on maxlevel values=%d want=1", ErrSectionShape, TablePath, len(tokens))
	}
	value, err := positiveIntegerUint64Token(tokens[0], "maxexp on maxlevel")
	if err != nil {
		return 0, err
	}
	return value, nil
}

func parseExpertGrades(document *dnfpvf.Document, afterSectionIndex int) ([]ExpertGradeInfo, int, error) {
	outerStarts := sectionIndices(document, "expert info")
	outerCloses := sectionIndices(document, "/expert info")
	if len(outerStarts) != 1 || len(outerCloses) != 1 {
		return nil, -1, fmt.Errorf("%w: path=%s section=expert info opens=%d closes=%d want=1/1", ErrSectionShape, TablePath, len(outerStarts), len(outerCloses))
	}
	start, closeIndex := outerStarts[0], outerCloses[0]
	if start != afterSectionIndex+1 || closeIndex <= start {
		return nil, -1, fmt.Errorf("%w: path=%s section=expert info invalid_order", ErrSectionShape, TablePath)
	}
	outerTokens, err := tokensForSection(document, start)
	if err != nil {
		return nil, -1, err
	}
	if len(outerTokens) != 0 {
		return nil, -1, fmt.Errorf("%w: path=%s section=expert info values=%d want=0", ErrSectionShape, TablePath, len(outerTokens))
	}

	grades := make([]ExpertGradeInfo, 0)
	for index := start + 1; index < closeIndex; {
		if sectionName(document.Sections[index].Name) != "grade info" {
			return nil, -1, fmt.Errorf("%w: path=%s section=expert info unexpected=%s", ErrSectionShape, TablePath, document.Sections[index].Name)
		}
		blockClose := -1
		for cursor := index + 1; cursor < closeIndex; cursor++ {
			if sectionName(document.Sections[cursor].Name) == "/grade info" {
				blockClose = cursor
				break
			}
		}
		if blockClose < 0 {
			return nil, -1, fmt.Errorf("%w: path=%s section=grade info missing_close", ErrSectionShape, TablePath)
		}
		grade, err := parseExpertGradeBlock(document, index, blockClose, len(grades))
		if err != nil {
			return nil, -1, err
		}
		grades = append(grades, grade)
		index = blockClose + 1
	}
	if len(grades) == 0 {
		return nil, -1, fmt.Errorf("%w: path=%s section=grade info", ErrSectionMissing, TablePath)
	}
	if err := requireEmptySection(document, closeIndex); err != nil {
		return nil, -1, err
	}
	return grades, closeIndex, nil
}

func parseExpertGradeBlock(document *dnfpvf.Document, start, closeIndex, wantGrade int) (ExpertGradeInfo, error) {
	tokens, err := tokensForSection(document, start)
	if err != nil {
		return ExpertGradeInfo{}, err
	}
	if len(tokens) != 2 {
		return ExpertGradeInfo{}, fmt.Errorf("%w: path=%s section=grade info values=%d want=2", ErrSectionShape, TablePath, len(tokens))
	}
	grade, err := nonnegativeIntToken(tokens[0], "grade info.grade")
	if err != nil {
		return ExpertGradeInfo{}, err
	}
	if grade != wantGrade {
		return ExpertGradeInfo{}, fmt.Errorf("%w: path=%s section=grade info grade=%d want=%d", ErrSectionShape, TablePath, grade, wantGrade)
	}
	name, err := nonemptyStringToken(tokens[1], "grade info.name")
	if err != nil {
		return ExpertGradeInfo{}, err
	}
	result := ExpertGradeInfo{Grade: grade, Name: name}
	seen := make(map[string]bool, 4)
	for index := start + 1; index < closeIndex; index++ {
		key := sectionName(document.Sections[index].Name)
		if seen[key] {
			return ExpertGradeInfo{}, fmt.Errorf("%w: path=%s section=grade info duplicate=%s", ErrSectionShape, TablePath, key)
		}
		seen[key] = true
		values, err := tokensForSection(document, index)
		if err != nil {
			return ExpertGradeInfo{}, err
		}
		switch key {
		case "medal img", "icon img":
			if len(values) != 2 {
				return ExpertGradeInfo{}, fmt.Errorf("%w: path=%s section=%s values=%d want=2", ErrSectionShape, TablePath, key, len(values))
			}
			imagePath, err := nonemptyStringToken(values[0], key+".path")
			if err != nil {
				return ExpertGradeInfo{}, err
			}
			imageIndex, err := nonnegativeIntToken(values[1], key+".index")
			if err != nil {
				return ExpertGradeInfo{}, err
			}
			if key == "medal img" {
				result.Medal, result.HasMedal = ImageReference{Path: imagePath, Index: imageIndex}, true
			} else {
				result.Icon, result.HasIcon = ImageReference{Path: imagePath, Index: imageIndex}, true
			}
		case "min lv", "max lv":
			if len(values) != 1 {
				return ExpertGradeInfo{}, fmt.Errorf("%w: path=%s section=%s values=%d want=1", ErrSectionShape, TablePath, key, len(values))
			}
			value, err := signedIntToken(values[0], key)
			if err != nil {
				return ExpertGradeInfo{}, err
			}
			if key == "min lv" {
				result.MinLevel = value
			} else {
				result.MaxLevel = value
			}
		default:
			return ExpertGradeInfo{}, fmt.Errorf("%w: path=%s section=grade info unexpected=%s", ErrSectionShape, TablePath, key)
		}
	}
	if !seen["min lv"] || !seen["max lv"] || result.MinLevel < 0 || (result.MaxLevel != -1 && result.MaxLevel < result.MinLevel) {
		return ExpertGradeInfo{}, fmt.Errorf("%w: path=%s section=grade info grade=%d min=%d max=%d", ErrSectionShape, TablePath, grade, result.MinLevel, result.MaxLevel)
	}
	if result.HasMedal != result.HasIcon {
		return ExpertGradeInfo{}, fmt.Errorf("%w: path=%s section=grade info grade=%d incomplete_images", ErrSectionShape, TablePath, grade)
	}
	if err := requireEmptySection(document, closeIndex); err != nil {
		return ExpertGradeInfo{}, err
	}
	return result, nil
}

func parseExpertExperienceRows(document *dnfpvf.Document, afterSectionIndex int) ([]ExpertExperienceRow, error) {
	starts := sectionIndices(document, "honor expert exp table")
	closes := sectionIndices(document, "/honor expert exp table")
	if len(starts) != 1 || len(closes) != 1 {
		return nil, fmt.Errorf("%w: path=%s section=honor expert exp table opens=%d closes=%d want=1/1", ErrSectionShape, TablePath, len(starts), len(closes))
	}
	start, closeIndex := starts[0], closes[0]
	if start != afterSectionIndex+1 || closeIndex != start+1 || closeIndex != len(document.Sections)-1 {
		return nil, fmt.Errorf("%w: path=%s section=honor expert exp table invalid_order_or_nested_section", ErrSectionShape, TablePath)
	}
	tokens, err := tokensForSection(document, start)
	if err != nil {
		return nil, err
	}
	if len(tokens) == 0 || len(tokens)%2 != 0 {
		return nil, fmt.Errorf("%w: path=%s section=honor expert exp table values=%d row_width=2", ErrSectionShape, TablePath, len(tokens))
	}
	rows := make([]ExpertExperienceRow, 0, len(tokens)/2)
	for offset := 0; offset < len(tokens); offset += 2 {
		level, err := positiveIntToken(tokens[offset], "honor expert exp table.level")
		if err != nil {
			return nil, err
		}
		wantLevel := len(rows) + 1
		if level != wantLevel {
			return nil, fmt.Errorf("%w: path=%s section=honor expert exp table level=%d want=%d", ErrSectionShape, TablePath, level, wantLevel)
		}
		experience, err := positiveUint64Token(tokens[offset+1], "honor expert exp table.experience")
		if err != nil {
			return nil, err
		}
		rows = append(rows, ExpertExperienceRow{Level: level, Experience: experience})
	}
	if err := requireEmptySection(document, closeIndex); err != nil {
		return nil, err
	}
	return rows, nil
}

// Resolve derives ordinary honor state and caps cumulative experience at the
// exact ordinary-table maximum.
func (tables *Tables) Resolve(totalExperience uint64) (Progress, error) {
	if tables == nil || len(tables.requirementByLevel) <= 1 {
		return Progress{}, ErrTablesRequired
	}
	if totalExperience > tables.maxTotalExperience {
		totalExperience = tables.maxTotalExperience
	}
	remaining := totalExperience
	level := 1
	for nextLevel := 2; nextLevel < len(tables.requirementByLevel); nextLevel++ {
		required := tables.requirementByLevel[nextLevel]
		if remaining < required {
			break
		}
		remaining -= required
		level = nextLevel
	}
	currentCap := tables.maxLevelExperience
	if level+1 < len(tables.requirementByLevel) {
		currentCap = tables.requirementByLevel[level+1]
	}
	if remaining > currentCap {
		remaining = currentCap
	}
	return Progress{
		TotalExperience:        totalExperience,
		Level:                  level,
		CurrentLevelExperience: remaining,
		Grade:                  tables.gradeByLevel[level],
	}, nil
}

// ResolveExpert validates and returns one current-client HonorExpert state.
// A level N state keeps only its progress toward N+1; the final table level
// has no following threshold and therefore always carries zero progress.
func (tables *Tables) ResolveExpert(progress ExpertProgress) (ExpertProgress, error) {
	if tables == nil || len(tables.expertExperienceRows) == 0 {
		return ExpertProgress{}, ErrTablesRequired
	}
	maximumLevel := uint32(len(tables.expertExperienceRows))
	if progress.Level > maximumLevel {
		return ExpertProgress{}, fmt.Errorf(
			"%w: level=%d maximum=%d",
			ErrExpertStateInvalid,
			progress.Level,
			maximumLevel,
		)
	}
	if progress.Level == maximumLevel {
		if progress.CurrentLevelExperience != 0 {
			return ExpertProgress{}, fmt.Errorf(
				"%w: final level=%d progress=%d",
				ErrExpertStateInvalid,
				progress.Level,
				progress.CurrentLevelExperience,
			)
		}
		return progress, nil
	}
	required := tables.expertExperienceRows[progress.Level].Experience
	if progress.CurrentLevelExperience >= required {
		return ExpertProgress{}, fmt.Errorf(
			"%w: level=%d progress=%d required=%d",
			ErrExpertStateInvalid,
			progress.Level,
			progress.CurrentLevelExperience,
			required,
		)
	}
	return progress, nil
}

// AdvanceExpert applies accepted post-character-cap experience to one
// HonorExpert state. A single award may cross several levels. Reaching the
// final PVF level discards any excess because the current client exposes no
// progress field for a level above the final table row.
func (tables *Tables) AdvanceExpert(progress ExpertProgress, gain uint64) (ExpertProgress, error) {
	progress, err := tables.ResolveExpert(progress)
	if err != nil || gain == 0 {
		return progress, err
	}
	maximumLevel := uint32(len(tables.expertExperienceRows))
	for gain > 0 && progress.Level < maximumLevel {
		required := tables.expertExperienceRows[progress.Level].Experience
		remaining := required - progress.CurrentLevelExperience
		if gain < remaining {
			progress.CurrentLevelExperience += gain
			break
		}
		gain -= remaining
		progress.Level++
		progress.CurrentLevelExperience = 0
	}
	return progress, nil
}

// CalculateHonorExperienceGain applies only the proved ordinary split at the
// character level cap. It does not persist, calculate character levels, or
// activate expert honor progression.
func CalculateHonorExperienceGain(previousLevel int, previousExperience, gainedExperience uint32, cap CharacterExperienceCap) (uint32, error) {
	if previousLevel <= 0 || cap.MaxLevel <= 0 || (cap.MaxLevel > 1 && cap.MaxLevelEntryExperience == 0) {
		return 0, fmt.Errorf("%w: previous_level=%d max_level=%d", ErrCharacterCapInvalid, previousLevel, cap.MaxLevel)
	}
	if gainedExperience == 0 {
		return 0, nil
	}
	if previousLevel >= cap.MaxLevel {
		return gainedExperience, nil
	}
	nextExperience := previousExperience + gainedExperience
	if nextExperience < previousExperience {
		nextExperience = math.MaxUint32
	}
	if nextExperience <= cap.MaxLevelEntryExperience {
		return 0, nil
	}
	baseline := previousExperience
	if baseline < cap.MaxLevelEntryExperience {
		baseline = cap.MaxLevelEntryExperience
	}
	if nextExperience <= baseline {
		return 0, nil
	}
	return nextExperience - baseline, nil
}

func (tables *Tables) Snapshot() Snapshot {
	if tables == nil {
		return Snapshot{}
	}
	return Snapshot{
		OrdinaryGrades:         len(tables.ordinaryGrades),
		OrdinaryLevels:         len(tables.requirementByLevel) - 1,
		MaxOrdinaryLevel:       len(tables.requirementByLevel) - 1,
		MaxLevelExperience:     tables.maxLevelExperience,
		MaxTotalExperience:     tables.maxTotalExperience,
		ExpertGrades:           len(tables.expertGrades),
		ExpertExperienceRows:   len(tables.expertExperienceRows),
		ExpertCalculationReady: true,
	}
}

// MaxOrdinaryLevel returns the highest strictly validated ordinary honor level.
func (tables *Tables) MaxOrdinaryLevel() int {
	if tables == nil {
		return 0
	}
	return len(tables.requirementByLevel) - 1
}

// MaxLevelExperience returns [maxexp on maxlevel].
func (tables *Tables) MaxLevelExperience() uint64 {
	if tables == nil {
		return 0
	}
	return tables.maxLevelExperience
}

// MaxTotalExperience returns all ordinary entry requirements plus the maximum
// current-level experience at the highest ordinary level.
func (tables *Tables) MaxTotalExperience() uint64 {
	if tables == nil {
		return 0
	}
	return tables.maxTotalExperience
}

func (tables *Tables) OrdinaryGrades() []OrdinaryGrade {
	if tables == nil {
		return nil
	}
	result := make([]OrdinaryGrade, len(tables.ordinaryGrades))
	for index, grade := range tables.ordinaryGrades {
		result[index] = grade
		result[index].Levels = append([]LevelRequirement(nil), grade.Levels...)
	}
	return result
}

func (tables *Tables) ExpertGrades() []ExpertGradeInfo {
	if tables == nil {
		return nil
	}
	return append([]ExpertGradeInfo(nil), tables.expertGrades...)
}

func (tables *Tables) ExpertExperienceRows() []ExpertExperienceRow {
	if tables == nil {
		return nil
	}
	return append([]ExpertExperienceRow(nil), tables.expertExperienceRows...)
}

func (tables *Tables) RequiredExperienceToEnter(level int) (uint64, bool) {
	if tables == nil || level <= 0 || level >= len(tables.requirementByLevel) {
		return 0, false
	}
	return tables.requirementByLevel[level], true
}

func (tables *Tables) GradeForLevel(level int) (int, bool) {
	if tables == nil || level <= 0 || level >= len(tables.gradeByLevel) {
		return 0, false
	}
	return tables.gradeByLevel[level], true
}

func sectionIndices(document *dnfpvf.Document, name string) []int {
	want := sectionName(name)
	indices := make([]int, 0)
	for index, section := range document.Sections {
		if sectionName(section.Name) == want {
			indices = append(indices, index)
		}
	}
	return indices
}

func sectionName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func tokensForSection(document *dnfpvf.Document, sectionIndex int) ([]dnfpvf.Token, error) {
	if document == nil || sectionIndex < 0 || sectionIndex >= len(document.Sections) {
		return nil, fmt.Errorf("%w: path=%s section_index=%d", ErrSectionShape, TablePath, sectionIndex)
	}
	section := document.Sections[sectionIndex]
	if section.Start < 0 || section.End < section.Start || section.End > len(document.Tokens) {
		return nil, fmt.Errorf("%w: path=%s section=%s start=%d end=%d tokens=%d", ErrSectionShape, TablePath, section.Name, section.Start, section.End, len(document.Tokens))
	}
	result := make([]dnfpvf.Token, section.End-section.Start)
	copy(result, document.Tokens[section.Start:section.End])
	return result, nil
}

func requireEmptySection(document *dnfpvf.Document, sectionIndex int) error {
	tokens, err := tokensForSection(document, sectionIndex)
	if err != nil {
		return err
	}
	if len(tokens) != 0 {
		return fmt.Errorf("%w: path=%s section=%s values=%d want=0", ErrSectionShape, TablePath, document.Sections[sectionIndex].Name, len(tokens))
	}
	return nil
}

func positiveIntToken(token dnfpvf.Token, field string) (int, error) {
	value, err := signedIntToken(token, field)
	if err != nil {
		return 0, err
	}
	if value <= 0 {
		return 0, fmt.Errorf("%w: path=%s field=%s value=%d want_positive", ErrSectionShape, TablePath, field, value)
	}
	return value, nil
}

func nonnegativeIntToken(token dnfpvf.Token, field string) (int, error) {
	value, err := signedIntToken(token, field)
	if err != nil {
		return 0, err
	}
	if value < 0 {
		return 0, fmt.Errorf("%w: path=%s field=%s value=%d want_nonnegative", ErrSectionShape, TablePath, field, value)
	}
	return value, nil
}

func signedIntToken(token dnfpvf.Token, field string) (int, error) {
	if token.Kind != dnfpvf.TokenInt || token.Int < math.MinInt32 || token.Int > math.MaxInt32 {
		return 0, fmt.Errorf("%w: path=%s field=%s kind=%s value=%d", ErrSectionShape, TablePath, field, token.Kind, token.Int)
	}
	return int(token.Int), nil
}

func nonnegativeIntegerUint64Token(token dnfpvf.Token, field string) (uint64, error) {
	if token.Kind != dnfpvf.TokenInt {
		return 0, fmt.Errorf("%w: path=%s field=%s kind=%s want=int", ErrSectionShape, TablePath, field, token.Kind)
	}
	value, err := uint64Token(token, field)
	if err != nil {
		return 0, err
	}
	return value, nil
}

func positiveIntegerUint64Token(token dnfpvf.Token, field string) (uint64, error) {
	value, err := nonnegativeIntegerUint64Token(token, field)
	if err != nil {
		return 0, err
	}
	if value == 0 {
		return 0, fmt.Errorf("%w: path=%s field=%s value=0 want_positive", ErrSectionShape, TablePath, field)
	}
	return value, nil
}

func positiveUint64Token(token dnfpvf.Token, field string) (uint64, error) {
	value, err := uint64Token(token, field)
	if err != nil {
		return 0, err
	}
	if value == 0 {
		return 0, fmt.Errorf("%w: path=%s field=%s value=0 want_positive", ErrSectionShape, TablePath, field)
	}
	return value, nil
}

func uint64Token(token dnfpvf.Token, field string) (uint64, error) {
	switch token.Kind {
	case dnfpvf.TokenInt:
		if token.Int < 0 {
			return 0, fmt.Errorf("%w: path=%s field=%s value=%d want_nonnegative", ErrSectionShape, TablePath, field, token.Int)
		}
		return uint64(token.Int), nil
	case dnfpvf.TokenString:
		value, err := strconv.ParseUint(strings.TrimSpace(token.Value), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("%w: path=%s field=%s value=%q: %v", ErrSectionShape, TablePath, field, token.Value, err)
		}
		return value, nil
	default:
		return 0, fmt.Errorf("%w: path=%s field=%s kind=%s", ErrSectionShape, TablePath, field, token.Kind)
	}
}

func nonemptyStringToken(token dnfpvf.Token, field string) (string, error) {
	if token.Kind != dnfpvf.TokenString || strings.TrimSpace(token.Value) == "" {
		return "", fmt.Errorf("%w: path=%s field=%s kind=%s value=%q", ErrSectionShape, TablePath, field, token.Kind, token.Value)
	}
	return token.Value, nil
}
