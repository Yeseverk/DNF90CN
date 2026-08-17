package progression

import (
	"fmt"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

// ExperienceSkillPointPlan couples the two values that must be committed
// together when experience crosses a level threshold.
type ExperienceSkillPointPlan struct {
	Experience  ExperienceResult
	SkillPoints SkillPointAdvance
}

// PlanExperienceAndSkillPoints is the single pure entry point for the rule
// "experience levels the character and each crossed level grants PVF SP".
// The caller must commit CharacterRecord.Level/Stats["exp"] and
// SkillRecord.Points atomically; this method does not write either repository.
func (t *Tables) PlanExperienceAndSkillPoints(
	level int,
	totalExperience uint32,
	gain uint32,
	maxLevel int,
	points dnfrepo.SkillPointState,
) (ExperienceSkillPointPlan, error) {
	if points.SyncedLevel != level {
		return ExperienceSkillPointPlan{}, fmt.Errorf("%w: character_level=%d synced_level=%d", ErrSkillPointLedger, level, points.SyncedLevel)
	}
	experience, err := t.ApplyExperience(level, totalExperience, gain, maxLevel)
	if err != nil {
		return ExperienceSkillPointPlan{}, err
	}
	skillPoints, err := t.AdvanceSkillPoints(points, experience.NewLevel)
	if err != nil {
		return ExperienceSkillPointPlan{}, err
	}
	return ExperienceSkillPointPlan{Experience: experience, SkillPoints: skillPoints}, nil
}
