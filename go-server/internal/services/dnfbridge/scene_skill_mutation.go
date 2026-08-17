package dnfbridge

import (
	"context"
	"fmt"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfskill "longheng.io/server/internal/modules/dnf/skill"
	"longheng.io/server/internal/modules/dnf/skillcmd"
)

func currentSceneSkillOwner(repositories dnfrepo.Group) (*dnfskill.SceneOwner, error) {
	return dnfskill.NewSceneOwner(repositories)
}

func syncCurrentSkillPointState(
	current dnfrepo.SkillPointState,
	target dnfrepo.SkillPointState,
) (dnfrepo.SkillPointState, bool, error) {
	return dnfskill.SyncPointState(current, target)
}

func (s *Service) loadOrBackfillCurrentSceneSkillRecord(ctx context.Context, repos dnfrepo.Group, character dnfrepo.CharacterRecord, job byte) (dnfrepo.SkillRecord, bool, error) {
	record, found, err := repos.Skill.Load(ctx, character.CharacterID)
	if err != nil {
		return dnfrepo.SkillRecord{}, false, err
	}
	if found && len(record.Skills) > 0 {
		record, _, err = s.syncCurrentSceneSkillPointLedger(ctx, repos, character, record)
		return record, false, err
	}
	initial, err := s.initialCharacterSkills(ctx, job)
	if err != nil {
		return dnfrepo.SkillRecord{}, false, fmt.Errorf("load job %d initial skills from PVF: %w", job, err)
	}
	if len(initial) == 0 {
		return dnfrepo.SkillRecord{}, false, fmt.Errorf("job %d has no PVF initial skills", job)
	}
	points, err := s.initialSkillPoints(ctx, character.Level)
	if err != nil {
		return dnfrepo.SkillRecord{}, false, fmt.Errorf("load level %d initial skill points from PVF: %w", character.Level, err)
	}
	s.initialSkillsMu.Lock()
	catalog := s.skillCatalog
	s.initialSkillsMu.Unlock()
	if catalog == nil {
		return dnfrepo.SkillRecord{}, false, fmt.Errorf("job %d initial skill catalog is unavailable", job)
	}

	seed := initialSkillRecord(character, characterPVFInitialization{
		Job:         job,
		Level:       character.Level,
		Skills:      initial,
		SkillPoints: points,
	}, time.Now().UTC())
	layout, err := skillcmd.BuildInitialSkillLayout(catalog, job, currentSkillInfoTreeIndex, seed.Skills)
	if err != nil {
		return dnfrepo.SkillRecord{}, false, fmt.Errorf("build job %d initial skill layout: %w", job, err)
	}
	seed.Layouts = map[int]dnfrepo.SkillLayout{currentSkillInfoTreeIndex: layout}

	owner, err := currentSceneSkillOwner(repos)
	if err != nil {
		return dnfrepo.SkillRecord{}, false, err
	}
	return owner.Backfill(ctx, dnfskill.BackfillCommand{
		CharacterID: character.CharacterID,
		Seed:        seed,
		UpdatedAt:   time.Now().UTC(),
	})
}

// syncCurrentSceneSkillPointLedger is the login/reselect repair for characters
// whose level changed outside the ordinary EXP owner or whose historical
// ledger predates TP advancement. The target totals come from the runtime PVF
// SP/TP tables plus persisted bonus points. Remaining points preserve the
// already-spent amount and clamp exactly as the 86JP LoadAndSync domain rule.
func (s *Service) syncCurrentSceneSkillPointLedger(
	ctx context.Context,
	repos dnfrepo.Group,
	character dnfrepo.CharacterRecord,
	record dnfrepo.SkillRecord,
) (dnfrepo.SkillRecord, bool, error) {
	target, err := s.currentPVFSkillPointTarget(ctx, character)
	if err != nil {
		return dnfrepo.SkillRecord{}, false, err
	}
	_, changed, err := syncCurrentSkillPointState(record.Points, target)
	if err != nil || !changed {
		return record, false, err
	}
	owner, err := currentSceneSkillOwner(repos)
	if err != nil {
		return dnfrepo.SkillRecord{}, false, err
	}
	return owner.SyncPoints(ctx, dnfskill.SyncPointsCommand{
		CharacterID: character.CharacterID,
		Target:      target,
		UpdatedAt:   time.Now().UTC(),
	})
}

func (s *Service) ensureCurrentSceneSkillLayout(ctx context.Context, repos dnfrepo.Group, character dnfrepo.CharacterRecord, job byte, catalog *dnfskill.Table, record dnfrepo.SkillRecord) (dnfrepo.SkillRecord, dnfrepo.SkillLayout, bool, error) {
	if existing := record.Layouts[currentSkillInfoTreeIndex]; len(existing) > 0 {
		layout, err := skillcmd.BuildCurrentSkillLayout(catalog, job, currentSkillInfoTreeIndex, record.Skills, existing)
		return record, layout, false, err
	}
	initial, err := s.initialCharacterSkills(ctx, job)
	if err != nil {
		return dnfrepo.SkillRecord{}, nil, false, fmt.Errorf("load job %d initial skills for layout: %w", job, err)
	}
	build := func(current dnfrepo.SkillRecord) (dnfrepo.SkillLayout, error) {
		if existing := current.Layouts[currentSkillInfoTreeIndex]; len(existing) > 0 {
			return skillcmd.BuildCurrentSkillLayout(catalog, job, currentSkillInfoTreeIndex, current.Skills, existing)
		}
		if skillRecordMatchesPVFInitialSkills(current.Skills, initial) {
			return skillcmd.BuildInitialSkillLayout(catalog, job, currentSkillInfoTreeIndex, current.Skills)
		}
		return skillcmd.BuildCurrentSkillLayout(catalog, job, currentSkillInfoTreeIndex, current.Skills, nil)
	}
	candidate, err := build(record)
	if err != nil {
		return dnfrepo.SkillRecord{}, nil, false, err
	}
	if repos.CharacterSkills == nil {
		return record, candidate, false, nil
	}
	owner, err := currentSceneSkillOwner(repos)
	if err != nil {
		return dnfrepo.SkillRecord{}, nil, false, err
	}
	return owner.EnsureLayout(ctx, dnfskill.EnsureLayoutCommand{
		CharacterID: character.CharacterID,
		TreeIndex:   currentSkillInfoTreeIndex,
		UpdatedAt:   time.Now().UTC(),
		Build:       build,
	})
}

func skillRecordMatchesPVFInitialSkills(states map[int64]dnfrepo.SkillState, initial []initialSkillEntry) bool {
	expected := make(map[int64]int, len(initial))
	for _, entry := range initial {
		if entry.SkillID > 0 && entry.Level > 0 {
			expected[entry.SkillID] = entry.Level
		}
	}
	if len(states) != len(expected) || len(expected) == 0 {
		return false
	}
	for skillID, level := range expected {
		state, ok := states[skillID]
		if !ok || !state.Enabled || state.Level != level {
			return false
		}
	}
	return true
}
