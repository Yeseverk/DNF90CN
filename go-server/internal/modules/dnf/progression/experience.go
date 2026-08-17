package progression

import (
	"fmt"
	"math"
)

type ExperienceResult struct {
	PreviousLevel      int
	PreviousExperience uint32
	Gain               uint32
	NewLevel           int
	NewExperience      uint32
	LevelsGained       int
	Saturated          bool
}

// ThresholdToNext returns the cumulative experience required to advance from
// level to level+1. The caller supplies the server's proven level cap when
// applying experience; the threshold-file length is not guessed to be that cap.
func (t *Tables) ThresholdToNext(level int) (uint32, error) {
	if t == nil || level <= 0 || level > len(t.experienceThresholds) {
		return 0, fmt.Errorf("%w: threshold level=%d entries=%d", ErrLevelOutOfRange, level, thresholdCount(t))
	}
	return t.experienceThresholds[level-1], nil
}

// ApplyExperience saturating-adds a current-EXE u32 experience gain and applies
// cumulative PVF thresholds up to maxLevel. It does not persist or emit an op37.
func (t *Tables) ApplyExperience(level int, totalExperience, gain uint32, maxLevel int) (ExperienceResult, error) {
	result := ExperienceResult{
		PreviousLevel:      level,
		PreviousExperience: totalExperience,
		Gain:               gain,
		NewLevel:           level,
		NewExperience:      totalExperience,
	}
	if t == nil || level <= 0 || maxLevel <= 0 || level > maxLevel || maxLevel-1 > len(t.experienceThresholds) {
		return ExperienceResult{}, fmt.Errorf("%w: level=%d max=%d thresholds=%d", ErrLevelOutOfRange, level, maxLevel, thresholdCount(t))
	}
	sum := uint64(totalExperience) + uint64(gain)
	if sum > math.MaxUint32 {
		sum = math.MaxUint32
		result.Saturated = true
	}
	result.NewExperience = uint32(sum)
	for result.NewLevel < maxLevel {
		threshold := t.experienceThresholds[result.NewLevel-1]
		if result.NewExperience < threshold {
			break
		}
		result.NewLevel++
	}
	result.LevelsGained = result.NewLevel - result.PreviousLevel
	return result, nil
}

func thresholdCount(t *Tables) int {
	if t == nil {
		return 0
	}
	return len(t.experienceThresholds)
}
