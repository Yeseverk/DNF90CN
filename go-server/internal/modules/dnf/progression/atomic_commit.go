package progression

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const characterExperienceStatKey = "exp"

var (
	ErrCommitPlanInvalid         = errors.New("dnf progression commit plan is invalid")
	ErrCommitOwnershipMismatch   = errors.New("dnf progression commit ownership mismatch")
	ErrCommitStateMissing        = errors.New("dnf progression commit state is missing")
	ErrCommitStateInvalid        = errors.New("dnf progression persisted state is invalid")
	ErrCommitVersionPrecondition = errors.New("dnf progression commit version precondition failed")
)

// CommitState is the complete optimistic version used by one progression
// commit. Expected is compared before any write; Next is the caller-computed
// state to persist. The repository layer does not calculate EXP, levels, SP,
// or TP.
type CommitState struct {
	Level       int
	Experience  uint32
	SkillPoints dnfrepo.SkillPointState
}

// CommitPlan binds a caller-computed Expected -> Next transition to one
// account-owned character. Expected is the version precondition; it avoids a
// lost update without coupling progression to unrelated character fields.
type CommitPlan struct {
	CharacterID string
	AccountID   string
	Expected    CommitState
	Next        CommitState
	// ExpectedCharacterStats protects state that shares the character stats
	// record with EXP but has its own domain progression, such as HonorExpert.
	ExpectedCharacterStats map[string]int64
	NextCharacterStats     map[string]int64
}

// CommitPlanFromExperienceSkillPoints maps the existing pure planner result
// into the atomic repository plan without recalculating any rule.
func CommitPlanFromExperienceSkillPoints(
	characterID string,
	accountID string,
	planned ExperienceSkillPointPlan,
) CommitPlan {
	return CommitPlan{
		CharacterID: characterID,
		AccountID:   accountID,
		Expected: CommitState{
			Level:       planned.Experience.PreviousLevel,
			Experience:  planned.Experience.PreviousExperience,
			SkillPoints: planned.SkillPoints.Previous,
		},
		Next: CommitState{
			Level:       planned.Experience.NewLevel,
			Experience:  planned.Experience.NewExperience,
			SkillPoints: planned.SkillPoints.New,
		},
	}
}

// CommitResult reports the exact state observed in the transaction. A retry
// that observes Next is successful and Idempotent without issuing a write. If
// a later progression state is present, the Expected precondition fails.
type CommitResult struct {
	Previous   CommitState
	Current    CommitState
	Applied    bool
	Idempotent bool
}

// Commit atomically persists Character.Level/Stats["exp"] and the complete
// SkillRecord SP/TP ledger. It consumes only a precomputed plan and deliberately
// does not read PVF, calculate a reward, or emit a protocol packet.
func Commit(
	ctx context.Context,
	uow dnfrepo.CharacterProgressionUnitOfWork,
	plan CommitPlan,
) (CommitResult, error) {
	plan.CharacterID = strings.TrimSpace(plan.CharacterID)
	plan.AccountID = strings.TrimSpace(plan.AccountID)
	if uow == nil {
		return CommitResult{}, dnfrepo.ErrCharacterProgressionTransactionUnavailable
	}
	if err := validateCommitPlan(plan); err != nil {
		return CommitResult{}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return CommitResult{}, err
	}

	var result CommitResult
	err := uow.WithinCharacterProgression(ctx, plan.CharacterID, func(
		characters dnfrepo.CharacterRepository,
		skills dnfrepo.SkillRepository,
	) error {
		character, characterExists, err := characters.Load(ctx, plan.CharacterID)
		if err != nil {
			return err
		}
		if !characterExists {
			return fmt.Errorf("%w: character=%s", ErrCommitStateMissing, plan.CharacterID)
		}
		if strings.TrimSpace(character.CharacterID) != plan.CharacterID || strings.TrimSpace(character.AccountID) != plan.AccountID {
			return fmt.Errorf(
				"%w: character=%s expected_account=%s actual_character=%s actual_account=%s",
				ErrCommitOwnershipMismatch,
				plan.CharacterID,
				plan.AccountID,
				character.CharacterID,
				character.AccountID,
			)
		}

		skill, skillExists, err := skills.Load(ctx, plan.CharacterID)
		if err != nil {
			return err
		}
		if !skillExists {
			return fmt.Errorf("%w: skill character=%s", ErrCommitStateMissing, plan.CharacterID)
		}
		if strings.TrimSpace(skill.CharacterID) != plan.CharacterID {
			return fmt.Errorf(
				"%w: character=%s skill_character=%s",
				ErrCommitOwnershipMismatch,
				plan.CharacterID,
				skill.CharacterID,
			)
		}

		current, err := persistedCommitState(character, skill)
		if err != nil {
			return err
		}
		result.Previous = current
		result.Current = current
		if commitStatesEqual(current, plan.Next) {
			if !characterStatsMatch(character.Stats, plan.NextCharacterStats) {
				return fmt.Errorf(
					"%w: character=%s progression reached next state without matching level stats",
					ErrCommitStateInvalid,
					plan.CharacterID,
				)
			}
			result.Idempotent = true
			return nil
		}
		if !commitStatesEqual(current, plan.Expected) {
			return fmt.Errorf(
				"%w: character=%s expected=%+v actual=%+v next=%+v",
				ErrCommitVersionPrecondition,
				plan.CharacterID,
				plan.Expected,
				current,
				plan.Next,
			)
		}
		if !characterStatsMatch(character.Stats, plan.ExpectedCharacterStats) {
			return fmt.Errorf(
				"%w: character=%s expected character stats do not match",
				ErrCommitVersionPrecondition,
				plan.CharacterID,
			)
		}

		character.Level = plan.Next.Level
		character.Stats[characterExperienceStatKey] = int64(plan.Next.Experience)
		for key, value := range plan.NextCharacterStats {
			character.Stats[key] = value
		}
		skill.Points = plan.Next.SkillPoints
		if err := dnfrepo.SaveCharacterFields(
			ctx,
			characters,
			character,
			dnfrepo.CharacterFieldBase,
			dnfrepo.CharacterFieldStats,
		); err != nil {
			return err
		}
		if err := dnfrepo.SaveSkillFields(ctx, skills, skill, dnfrepo.SkillFieldPoints); err != nil {
			return err
		}

		result.Current = plan.Next
		result.Applied = true
		return nil
	})
	if err != nil {
		return CommitResult{}, err
	}
	return result, nil
}

func validateCommitPlan(plan CommitPlan) error {
	if plan.CharacterID == "" || plan.AccountID == "" {
		return fmt.Errorf("%w: character/account ownership is required", ErrCommitPlanInvalid)
	}
	if err := validateCommitState(plan.Expected); err != nil {
		return fmt.Errorf("%w: expected: %v", ErrCommitPlanInvalid, err)
	}
	if err := validateCommitState(plan.Next); err != nil {
		return fmt.Errorf("%w: next: %v", ErrCommitPlanInvalid, err)
	}
	if plan.Next.Level < plan.Expected.Level {
		return fmt.Errorf("%w: level regressed %d -> %d", ErrCommitPlanInvalid, plan.Expected.Level, plan.Next.Level)
	}
	if plan.Next.Experience < plan.Expected.Experience {
		return fmt.Errorf("%w: cumulative experience regressed %d -> %d", ErrCommitPlanInvalid, plan.Expected.Experience, plan.Next.Experience)
	}
	if plan.Next.SkillPoints.TotalSP < plan.Expected.SkillPoints.TotalSP ||
		plan.Next.SkillPoints.TotalTP < plan.Expected.SkillPoints.TotalTP {
		return fmt.Errorf("%w: total SP/TP regressed expected=%+v next=%+v", ErrCommitPlanInvalid, plan.Expected.SkillPoints, plan.Next.SkillPoints)
	}
	if err := validateCharacterStatMap(plan.ExpectedCharacterStats); err != nil {
		return err
	}
	if err := validateCharacterStatMap(plan.NextCharacterStats); err != nil {
		return err
	}
	for key := range plan.ExpectedCharacterStats {
		if _, found := plan.NextCharacterStats[key]; !found {
			return fmt.Errorf("%w: expected character stat %q has no next value", ErrCommitPlanInvalid, key)
		}
	}
	expectedSpentSP := plan.Expected.SkillPoints.TotalSP - plan.Expected.SkillPoints.RemainingSP
	nextSpentSP := plan.Next.SkillPoints.TotalSP - plan.Next.SkillPoints.RemainingSP
	expectedSpentTP := plan.Expected.SkillPoints.TotalTP - plan.Expected.SkillPoints.RemainingTP
	nextSpentTP := plan.Next.SkillPoints.TotalTP - plan.Next.SkillPoints.RemainingTP
	if expectedSpentSP != nextSpentSP || expectedSpentTP != nextSpentTP {
		return fmt.Errorf(
			"%w: progression changed spent points SP=%d->%d TP=%d->%d",
			ErrCommitPlanInvalid,
			expectedSpentSP,
			nextSpentSP,
			expectedSpentTP,
			nextSpentTP,
		)
	}
	return nil
}

func validateCharacterStatMap(values map[string]int64) error {
	for key := range values {
		if strings.TrimSpace(key) == "" || key == characterExperienceStatKey {
			return fmt.Errorf("%w: invalid character stat key %q", ErrCommitPlanInvalid, key)
		}
	}
	return nil
}

func characterStatsMatch(current map[string]int64, expected map[string]int64) bool {
	for key, value := range expected {
		if current[key] != value {
			return false
		}
	}
	return true
}

func validateCommitState(state CommitState) error {
	if state.Level <= 0 || state.Level > math.MaxUint8 {
		return fmt.Errorf("level=%d outside current EXE u8 range", state.Level)
	}
	points := state.SkillPoints
	if points.SyncedLevel != state.Level {
		return fmt.Errorf("level=%d synced_level=%d", state.Level, points.SyncedLevel)
	}
	if points.TotalSP < 0 || points.TotalSP > math.MaxUint16 ||
		points.RemainingSP < 0 || points.RemainingSP > points.TotalSP ||
		points.TotalTP < 0 || points.TotalTP > math.MaxUint16 ||
		points.RemainingTP < 0 || points.RemainingTP > points.TotalTP {
		return fmt.Errorf("SP/TP ledger outside current EXE u16 range: %+v", points)
	}
	return nil
}

func persistedCommitState(character dnfrepo.CharacterRecord, skill dnfrepo.SkillRecord) (CommitState, error) {
	experience, ok := character.Stats[characterExperienceStatKey]
	if !ok {
		return CommitState{}, fmt.Errorf("%w: character=%s stat=%s", ErrCommitStateMissing, character.CharacterID, characterExperienceStatKey)
	}
	if experience < 0 || uint64(experience) > math.MaxUint32 {
		return CommitState{}, fmt.Errorf(
			"%w: character=%s experience=%d outside current EXE u32 range",
			ErrCommitStateInvalid,
			character.CharacterID,
			experience,
		)
	}
	state := CommitState{
		Level:       character.Level,
		Experience:  uint32(experience),
		SkillPoints: skill.Points,
	}
	if err := validateCommitState(state); err != nil {
		return CommitState{}, fmt.Errorf("%w: character=%s: %v", ErrCommitStateInvalid, character.CharacterID, err)
	}
	return state, nil
}

func commitStatesEqual(left, right CommitState) bool {
	return left.Level == right.Level &&
		left.Experience == right.Experience &&
		left.SkillPoints == right.SkillPoints
}
