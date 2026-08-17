package adventuregroup

import (
	"errors"
	"fmt"
	"math"
)

const CharacterManagementPath = "etc/linksystem/charactermanage.etc"

const (
	AdventureSystem2018Path = "etc/adventurersystem/adventurersystem2018.etc"
	ExpeditionSystemPath    = "etc/adventurersystem/adventurerexpeditionsystem.etc"
)

var (
	ErrTableEmpty     = errors.New("dnf adventure-group PVF table is empty")
	ErrTableMalformed = errors.New("dnf adventure-group PVF table is malformed")
	ErrCharacterLevel = errors.New("dnf adventure-group character level is invalid")
	ErrPointOverflow  = errors.New("dnf adventure-group point total overflows uint64")
)

// Character contains only the account-roster state needed by the adventure
// group calculation. Deleted characters never contribute points.
type Character struct {
	Level   int
	Deleted bool
}

// Benefits are exact lookups for one PVF management level. A missing table row
// is represented by zero; values are not inherited from an earlier level.
type Benefits struct {
	ExpBonusPercent  uint64
	GoldBonusPercent uint64
	ManageOption     uint64
}

// Summary is the pure account-wide result derived from character levels and
// the current runtime PVF tables.
type Summary struct {
	TotalPoint  uint64
	ManageLevel int
	Benefits
}

// Snapshot reports typed source coverage without exposing mutable table maps.
type Snapshot struct {
	PointRanges        int
	ManageThresholds   int
	ManageLevelMax     int
	ExpBonusLevels     int
	GoldBonusLevels    int
	ManageOptionLevels int
}

type pointRange struct {
	minLevel int
	maxLevel int
	point    uint64
}

// Tables is an immutable snapshot of etc/linksystem/charactermanage.etc.
type Tables struct {
	pointRanges      []pointRange
	manageThresholds []uint64
	manageLevelMax   int
	expBonusByLevel  map[int]uint64
	goldBonusByLevel map[int]uint64
	optionByLevel    map[int]uint64
	runtime          RuntimeConfig
}

// Snapshot returns table coverage counts useful for startup diagnostics and
// real-PVF smoke tests.
func (t *Tables) Snapshot() Snapshot {
	if t == nil {
		return Snapshot{}
	}
	return Snapshot{
		PointRanges:        len(t.pointRanges),
		ManageThresholds:   len(t.manageThresholds),
		ManageLevelMax:     t.manageLevelMax,
		ExpBonusLevels:     len(t.expBonusByLevel),
		GoldBonusLevels:    len(t.goldBonusByLevel),
		ManageOptionLevels: len(t.optionByLevel),
	}
}

// Runtime returns a detached copy of the current PVF-backed adventure-group
// expedition, shop and growth-capsule configuration.
func (t *Tables) Runtime() RuntimeConfig {
	if t == nil {
		return RuntimeConfig{}
	}
	return t.runtime.Clone()
}

// CharacterPoint sums every per-level point value whose level is at or below
// the character's level. PVF point ranges may contain gaps.
func (t *Tables) CharacterPoint(level int) (uint64, error) {
	if t == nil || len(t.pointRanges) == 0 {
		return 0, ErrTableEmpty
	}
	if level <= 0 {
		return 0, fmt.Errorf("%w: %d", ErrCharacterLevel, level)
	}

	var total uint64
	for _, row := range t.pointRanges {
		if level < row.minLevel {
			break
		}
		end := level
		if end > row.maxLevel {
			end = row.maxLevel
		}
		count := uint64(end - row.minLevel + 1)
		if row.point != 0 && count > math.MaxUint64/row.point {
			return 0, fmt.Errorf("%w: level=%d range=%d..%d", ErrPointOverflow, level, row.minLevel, row.maxLevel)
		}
		contribution := count * row.point
		if total > math.MaxUint64-contribution {
			return 0, fmt.Errorf("%w: level=%d", ErrPointOverflow, level)
		}
		total += contribution
	}
	return total, nil
}

// ManageLevelForPoint resolves passed thresholds in order, then applies the
// explicit PVF management-level maximum. If PVF provides fewer thresholds than
// that maximum, the highest reachable level remains the last threshold.
func (t *Tables) ManageLevelForPoint(totalPoint uint64) int {
	if t == nil {
		return 0
	}
	level := 0
	for _, threshold := range t.manageThresholds {
		if totalPoint < threshold {
			break
		}
		level++
	}
	if level > t.manageLevelMax {
		return t.manageLevelMax
	}
	return level
}

// BenefitsForLevel performs exact PVF table lookups. Sparse tables do not
// inherit the previous level's values.
func (t *Tables) BenefitsForLevel(manageLevel int) Benefits {
	if t == nil || manageLevel <= 0 {
		return Benefits{}
	}
	return Benefits{
		ExpBonusPercent:  t.expBonusByLevel[manageLevel],
		GoldBonusPercent: t.goldBonusByLevel[manageLevel],
		ManageOption:     t.optionByLevel[manageLevel],
	}
}

// Calculate derives one account summary from lightweight character rows.
func (t *Tables) Calculate(characters []Character) (Summary, error) {
	if t == nil || len(t.pointRanges) == 0 {
		return Summary{}, ErrTableEmpty
	}
	var total uint64
	for _, character := range characters {
		if character.Deleted {
			continue
		}
		point, err := t.CharacterPoint(character.Level)
		if err != nil {
			return Summary{}, err
		}
		if total > math.MaxUint64-point {
			return Summary{}, fmt.Errorf("%w: account roster", ErrPointOverflow)
		}
		total += point
	}
	level := t.ManageLevelForPoint(total)
	return Summary{
		TotalPoint:  total,
		ManageLevel: level,
		Benefits:    t.BenefitsForLevel(level),
	}, nil
}

// CalculateLevels is a convenience for callers that already filtered deleted
// characters and only have an account's character levels.
func (t *Tables) CalculateLevels(levels []int) (Summary, error) {
	characters := make([]Character, len(levels))
	for index, level := range levels {
		characters[index] = Character{Level: level}
	}
	return t.Calculate(characters)
}
