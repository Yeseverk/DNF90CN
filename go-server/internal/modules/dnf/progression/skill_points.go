package progression

import (
	"fmt"
	"math"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

type SkillPointAdvance struct {
	Previous dnfrepo.SkillPointState
	New      dnfrepo.SkillPointState
	SPGain   int
	TPGain   int
}

func (t *Tables) SkillPointsAtLevel(level int) (int, bool) {
	if t == nil {
		return 0, false
	}
	value, ok := t.spByLevel[level]
	return value, ok
}

func (t *Tables) TechniquePointsAtLevel(level int) (int, bool) {
	if t == nil {
		return 0, false
	}
	value, ok := t.tpByLevel[level]
	return value, ok
}

// TotalSkillPoints returns the cumulative PVF SP through level. A missing row
// is an error rather than an implicit zero because silently skipping a runtime
// PVF level would permanently underpay a character.
func (t *Tables) TotalSkillPoints(level int) (int, error) {
	if t == nil || level <= 0 {
		return 0, fmt.Errorf("%w: SP level=%d", ErrLevelOutOfRange, level)
	}
	total := 0
	for current := 1; current <= level; current++ {
		award, ok := t.spByLevel[current]
		if !ok {
			return 0, fmt.Errorf("%w: %s missing level=%d", ErrTableMalformed, SkillPointTablePath, current)
		}
		if award > math.MaxInt-total {
			return 0, fmt.Errorf("%w: %s cumulative SP overflow at level=%d", ErrTableMalformed, SkillPointTablePath, current)
		}
		total += award
	}
	return total, nil
}

// TotalTechniquePoints returns the cumulative PVF TP through level. Levels
// below the first TP row award zero; once the table starts, every crossed
// level must have an explicit row so a malformed runtime PVF cannot silently
// underpay the ledger.
func (t *Tables) TotalTechniquePoints(level int) (int, error) {
	if t == nil || level <= 0 {
		return 0, fmt.Errorf("%w: TP level=%d", ErrLevelOutOfRange, level)
	}
	firstLevel := t.firstTechniquePointLevel()
	if firstLevel == 0 || level < firstLevel {
		return 0, nil
	}
	total := 0
	for current := firstLevel; current <= level; current++ {
		award, ok := t.tpByLevel[current]
		if !ok {
			return 0, fmt.Errorf("%w: %s [tp table] missing level=%d", ErrTableMalformed, SkillPointTablePath, current)
		}
		if award > math.MaxInt-total {
			return 0, fmt.Errorf("%w: %s cumulative TP overflow at level=%d", ErrTableMalformed, SkillPointTablePath, current)
		}
		total += award
	}
	return total, nil
}

func (t *Tables) firstTechniquePointLevel() int {
	first := 0
	if t == nil {
		return first
	}
	for level := range t.tpByLevel {
		if first == 0 || level < first {
			first = level
		}
	}
	return first
}

// AdvanceSkillPoints adds only the PVF awards for levels crossed since the
// ledger's SyncedLevel. Both total and remaining SP grow by the same delta, so
// previously spent SP is preserved exactly.
func (t *Tables) AdvanceSkillPoints(points dnfrepo.SkillPointState, newLevel int) (SkillPointAdvance, error) {
	result := SkillPointAdvance{Previous: points, New: points}
	if points.SyncedLevel <= 0 || newLevel < points.SyncedLevel ||
		points.TotalSP < 0 || points.RemainingSP < 0 || points.RemainingSP > points.TotalSP ||
		points.TotalTP < 0 || points.RemainingTP < 0 || points.RemainingTP > points.TotalTP {
		return SkillPointAdvance{}, fmt.Errorf("%w: target_level=%d ledger=%+v", ErrSkillPointLedger, newLevel, points)
	}
	spDelta := 0
	tpDelta := 0
	firstTPLevel := t.firstTechniquePointLevel()
	for level := points.SyncedLevel + 1; level <= newLevel; level++ {
		award, ok := t.SkillPointsAtLevel(level)
		if !ok {
			return SkillPointAdvance{}, fmt.Errorf("%w: %s missing level=%d", ErrTableMalformed, SkillPointTablePath, level)
		}
		if award > math.MaxInt-spDelta {
			return SkillPointAdvance{}, fmt.Errorf("%w: %s SP delta overflow at level=%d", ErrTableMalformed, SkillPointTablePath, level)
		}
		spDelta += award
		if firstTPLevel != 0 && level >= firstTPLevel {
			tpAward, ok := t.TechniquePointsAtLevel(level)
			if !ok {
				return SkillPointAdvance{}, fmt.Errorf("%w: %s [tp table] missing level=%d", ErrTableMalformed, SkillPointTablePath, level)
			}
			if tpAward > math.MaxInt-tpDelta {
				return SkillPointAdvance{}, fmt.Errorf("%w: %s TP delta overflow at level=%d", ErrTableMalformed, SkillPointTablePath, level)
			}
			tpDelta += tpAward
		}
	}
	if spDelta > math.MaxInt-points.TotalSP || spDelta > math.MaxInt-points.RemainingSP ||
		tpDelta > math.MaxInt-points.TotalTP || tpDelta > math.MaxInt-points.RemainingTP {
		return SkillPointAdvance{}, fmt.Errorf("%w: SP/TP ledger overflow sp_delta=%d tp_delta=%d ledger=%+v", ErrSkillPointLedger, spDelta, tpDelta, points)
	}
	result.SPGain = spDelta
	result.TPGain = tpDelta
	result.New.TotalSP += spDelta
	result.New.RemainingSP += spDelta
	result.New.TotalTP += tpDelta
	result.New.RemainingTP += tpDelta
	result.New.SyncedLevel = newLevel
	return result, nil
}
