package dnfbridge

import (
	"context"
	"fmt"
	"sort"
	"strings"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

type currentSceneSkillInfoProjection struct {
	body            []byte
	characterID     string
	job             byte
	learnedCount    int
	learnedSkillIDs []int64
	activeSlots     []int
	quickSkillIDs   []uint16
}

func (s *Service) buildCurrentSceneSkillInfoProjection(
	session *gameSession,
	ctx context.Context,
	character dnfrepo.CharacterRecord,
	source string,
) (currentSceneSkillInfoProjection, bool, error) {
	if s == nil || session == nil {
		return currentSceneSkillInfoProjection{}, false, nil
	}
	repos, ok := s.repositoryGroup()
	if !ok || repos.Skill == nil {
		s.logGameEvent(session, "game-main-skill-info-deferred", "source", source, "reason", "skill_repository_missing")
		return currentSceneSkillInfoProjection{}, false, nil
	}
	job, ok := characterJobByte(character)
	if !ok {
		return currentSceneSkillInfoProjection{}, false, fmt.Errorf("current scene skill info has invalid job %q", character.Job)
	}
	record, backfilled, err := s.loadOrBackfillCurrentSceneSkillRecord(ctx, repos, character, job)
	if err != nil {
		return currentSceneSkillInfoProjection{}, false, fmt.Errorf("load current scene skill info: %w", err)
	}
	s.initialSkillsMu.Lock()
	catalog := s.skillCatalog
	s.initialSkillsMu.Unlock()
	if catalog == nil {
		s.logGameEvent(session, "game-main-skill-info-deferred", "source", source, "reason", "skill_catalog_missing")
		return currentSceneSkillInfoProjection{}, false, nil
	}
	record, layout, layoutBackfilled, err := s.ensureCurrentSceneSkillLayout(ctx, repos, character, job, catalog, record)
	if err != nil {
		return currentSceneSkillInfoProjection{}, false, fmt.Errorf("build current scene skill layout: %w", err)
	}
	if backfilled {
		s.logGameEvent(session, "game-main-skill-info-backfilled",
			"source", source,
			"character_id", character.CharacterID,
			"job", job,
			"learned_count", len(record.Skills),
			"layout_count", len(layout),
			"record_source", "runtime_pvf_character_initial_value_skill")
	}
	if layoutBackfilled {
		s.logGameEvent(session, "game-main-skill-layout-backfilled",
			"source", source,
			"character_id", character.CharacterID,
			"job", job,
			"layout_count", len(layout),
			"layout_source", "job_scoped_pvf_initial_skill_layout")
	}
	body, activeSlots, err := buildCurrentSceneSkillInfoBody(record, layout)
	if err != nil {
		return currentSceneSkillInfoProjection{}, false, err
	}
	learnedSkillIDs := make([]int64, 0, len(record.Skills))
	for skillID, state := range record.Skills {
		if state.Enabled && state.Level > 0 {
			learnedSkillIDs = append(learnedSkillIDs, skillID)
		}
	}
	sort.Slice(learnedSkillIDs, func(left, right int) bool {
		return learnedSkillIDs[left] < learnedSkillIDs[right]
	})
	quickSkillIDs := make([]uint16, 0, len(activeSlots))
	for _, slot := range activeSlots {
		quickSkillIDs = append(quickSkillIDs, layout[slot])
	}
	return currentSceneSkillInfoProjection{
		body:            append([]byte(nil), body...),
		characterID:     character.CharacterID,
		job:             job,
		learnedCount:    len(record.Skills),
		learnedSkillIDs: append([]int64(nil), learnedSkillIDs...),
		activeSlots:     append([]int(nil), activeSlots...),
		quickSkillIDs:   append([]uint16(nil), quickSkillIDs...),
	}, true, nil
}

func (s *Service) sendCurrentSceneSkillInfoProjection(
	session *gameSession,
	projection currentSceneSkillInfoProjection,
	source string,
) error {
	s.logGameEvent(session, "game-main-skill-info-send",
		"source", source,
		"character_id", projection.characterID,
		"job", projection.job,
		"msg_id", currentSkillInfoMsgID,
		"classification", 0,
		"body_len", len(projection.body),
		"learned_count", projection.learnedCount,
		"learned_skill_ids", projection.learnedSkillIDs,
		"active_quick_slots", projection.activeSlots,
		"active_quick_skill_ids", projection.quickSkillIDs,
		"tree_count", currentSkillInfoTreeCount,
		"wire_level_encoding", "persisted_level_direct",
		"body_source", "current_exe_sub_1D6E240_length_prefixed_DNFPB_skillinfo")
	return s.sendGameUpperRawClass(session, currentSkillInfoMsgID, projection.body, 0)
}

func (s *Service) sendCurrentSceneSkillInfo(session *gameSession, ctx context.Context, character dnfrepo.CharacterRecord, source string) error {
	projection, ok, err := s.buildCurrentSceneSkillInfoProjection(session, ctx, character, source)
	if err != nil || !ok {
		return err
	}
	return s.sendCurrentSceneSkillInfoProjection(session, projection, source)
}

// sendSelectedActorCurrentSceneSkillInfo reloads the selected character before
// projecting op19. Existing-actor mode0/mode1 rebinds can replace the native
// skill manager, so callers must invoke this only after the final actor rebind,
// never from the DLL or before the repository-backed appearance packets.
func (s *Service) sendSelectedActorCurrentSceneSkillInfo(session *gameSession, source string) error {
	if session == nil || session.selectedCharacterID == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	repos, ok := s.repositoryGroup()
	if !ok || repos.Character == nil {
		return fmt.Errorf("selected actor skill projection could not load character repository")
	}
	characterID := fmt.Sprintf("%d", session.selectedCharacterID)
	character, found, err := repos.Character.Load(ctx, characterID)
	if err != nil {
		return fmt.Errorf("load selected actor %s for skill projection: %w", characterID, err)
	}
	if !found {
		return fmt.Errorf("selected actor %s for skill projection was not found", characterID)
	}
	if accountID := strings.TrimSpace(s.accountIDForSession(session)); accountID != "" &&
		accountID != strings.TrimSpace(character.AccountID) {
		return fmt.Errorf("selected actor %s skill projection owner mismatch", characterID)
	}
	projection, ready, err := s.buildCurrentSceneSkillInfoProjection(
		session,
		ctx,
		character,
		source,
	)
	if err != nil {
		return err
	}
	if !ready {
		return fmt.Errorf("selected actor %s skill projection is unavailable", characterID)
	}
	return s.sendCurrentSceneSkillInfoProjection(session, projection, source)
}
