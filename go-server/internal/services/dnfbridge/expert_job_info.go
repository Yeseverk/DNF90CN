package dnfbridge

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	dnfexpertjob "longheng.io/server/internal/modules/dnf/expertjob"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

// 0x00CD is a local class-0 notification here, distinct from command op205.
// Current NoPack sub_1D89C20 reads state, expert-job mode, recipe count, then
// recipe IDs for alchemist (2) and doll controller (4).
const currentExpertJobInfoNotification uint16 = 0x00CD

func (s *Service) sendCurrentExpertJobInfoForCharacter(session *gameSession, character dnfrepo.CharacterRecord, force bool) error {
	rawJobType := character.Stats["expert_job_type"]
	if rawJobType < int64(dnfexpertjob.EnchanterType) || rawJobType > int64(dnfexpertjob.DollControllerType) {
		return nil
	}
	jobType := byte(rawJobType)
	experience := character.Stats["expert_job_exp"]
	if experience < 0 {
		experience = 0
	}
	return s.sendCurrentExpertJobInfo(session, jobType, experience, currentExpertJobLearnedRecipes(character.Stats, jobType), character.Stats, force)
}

// sendCurrentExpertJobClearedInfo publishes the two-byte type-zero form read
// by current op205. The compatibility unit mirrors that zero into the current
// actor so the system-menu and self-click gates stop advertising the old job.
func (s *Service) sendCurrentExpertJobClearedInfo(session *gameSession, giveUpState byte) error {
	if s == nil || session == nil || session.selectedCharacterID == 0 {
		return nil
	}
	w := packetWriter{}
	w.writeByte(giveUpState)
	w.writeByte(0)
	if err := s.sendGameUpperRawClass(session, currentExpertJobInfoNotification, w.bytes(), 0); err != nil {
		return err
	}
	session.expertJobInfoCharacterID = session.selectedCharacterID
	s.logPacketEvent("game-current-expert-job-info-cleared",
		"char_id", session.selectedCharacterID,
		"give_up_state", giveUpState,
		"sent_at", time.Now().UTC())
	return nil
}

func (s *Service) sendCurrentExpertJobInfoFromRepository(session *gameSession, expectedType byte, force bool) error {
	if s == nil || session == nil || session.selectedCharacterID == 0 {
		return nil
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Character == nil {
		return dnfexpertjob.ErrOwnerUnavailable
	}
	ctx, cancel := context.WithTimeout(context.Background(), currentRepositorySnapshotTimeout)
	defer cancel()
	characterID := strconv.FormatUint(uint64(session.selectedCharacterID), 10)
	character, found, err := repositories.Character.Load(ctx, characterID)
	if err != nil {
		return err
	}
	if !found || character.CharacterID != characterID || character.Stats["expert_job_type"] != int64(expectedType) {
		return dnfexpertjob.ErrCharacterNotFound
	}
	return s.sendCurrentExpertJobInfoForCharacter(session, character, force)
}

func (s *Service) sendCurrentExpertJobInfo(session *gameSession, jobType byte, experience int64, learned []int64, stats map[string]int64, force bool) error {
	if s == nil || session == nil || session.selectedCharacterID == 0 {
		return nil
	}
	if !force && session.expertJobInfoCharacterID == session.selectedCharacterID {
		return nil
	}
	catalog, err := s.currentExpertJobCatalog()
	if err != nil {
		return err
	}
	config, configured := catalog.Config(jobType)
	if jobType != dnfexpertjob.DisjointerType && !configured {
		return dnfexpertjob.ErrJobUnsupported
	}
	w := packetWriter{}
	w.writeByte(0)
	w.writeByte(jobType)
	if jobType == dnfexpertjob.DisjointerType {
		disjointer, ok := catalog.Disjointer()
		if !ok {
			return dnfexpertjob.ErrJobUnsupported
		}
		grade := currentExpertJobStatDefault(stats, currentExpertJobMachineGradeStat, 1)
		endurance := currentExpertJobStatDefault(stats, currentExpertJobMachineEnduranceStat, disjointer.InitialEndurance)
		w.writeInt32(int(grade))
		w.writeInt32(int(endurance))
		if err := s.sendGameUpperRawClass(session, currentExpertJobInfoNotification, w.bytes(), 0); err != nil {
			return err
		}
		session.expertJobInfoCharacterID = session.selectedCharacterID
		s.logPacketEvent("game-current-expert-job-info-send", "char_id", session.selectedCharacterID, "job_type", jobType, "experience", experience, "machine_grade", grade, "endurance", endurance, "forced", force, "sent_at", time.Now().UTC())
		return nil
	}
	recipes := config.AutoRecipeIDs(experience)
	seen := make(map[int64]struct{}, len(recipes)+len(learned))
	for _, recipeID := range recipes {
		seen[recipeID] = struct{}{}
	}
	for _, recipeID := range learned {
		_, generic := config.Recipes[recipeID]
		card := false
		if jobType == dnfexpertjob.EnchanterType {
			if enchanter, ok := catalog.Enchanter(); ok {
				_, card = enchanter.CardRecipes[recipeID]
			}
		}
		if generic || card {
			seen[recipeID] = struct{}{}
		}
	}
	recipes = recipes[:0]
	for recipeID := range seen {
		recipes = append(recipes, recipeID)
	}
	sort.Slice(recipes, func(i, j int) bool { return recipes[i] < recipes[j] })
	if len(recipes) > math.MaxUint8 {
		return fmt.Errorf("expert job recipe list is too large: type=%d count=%d", jobType, len(recipes))
	}
	w.writeByte(byte(len(recipes)))
	for _, recipeID := range recipes {
		if recipeID <= 0 || recipeID > math.MaxUint32 {
			return fmt.Errorf("expert job recipe id is outside wire range: %d", recipeID)
		}
		w.writeUint32(uint32(recipeID))
	}
	if jobType == dnfexpertjob.EnchanterType {
		enchanter, ok := catalog.Enchanter()
		if !ok {
			return dnfexpertjob.ErrJobUnsupported
		}
		qualifications := enchanter.Qualifications(experience)
		if len(qualifications) > math.MaxUint8 {
			return fmt.Errorf("enchanter qualification list is too large: %d", len(qualifications))
		}
		w.writeByte(byte(len(qualifications)))
		w.writeBytes(qualifications)
		w.writeInt32(config.Level(experience))
		w.writeInt32(int(currentExpertJobStatDefault(stats, currentExpertJobMachineEnduranceStat, enchanter.InitialEndurance)))
	}
	if err := s.sendGameUpperRawClass(session, currentExpertJobInfoNotification, w.bytes(), 0); err != nil {
		return err
	}
	session.expertJobInfoCharacterID = session.selectedCharacterID
	s.logPacketEvent("game-current-expert-job-info-send",
		"char_id", session.selectedCharacterID,
		"job_type", jobType,
		"experience", experience,
		"level", config.Level(experience),
		"recipe_count", len(recipes),
		"forced", force,
		"sent_at", time.Now().UTC())
	return nil
}

const (
	currentExpertJobMachineGradeStat     = "expert_job_machine_grade"
	currentExpertJobMachineEnduranceStat = "expert_job_machine_endurance"
)

func currentExpertJobStatDefault(stats map[string]int64, key string, fallback int64) int64 {
	if value, ok := stats[key]; ok && value >= 0 {
		return value
	}
	return fallback
}

func currentExpertJobRecipeStatKey(jobType byte, recipeID int64) string {
	return "expert_job_recipe_" + strconv.Itoa(int(jobType)) + "_" + strconv.FormatInt(recipeID, 10)
}

func currentExpertJobLearnedRecipes(stats map[string]int64, jobType byte) []int64 {
	prefix := "expert_job_recipe_" + strconv.Itoa(int(jobType)) + "_"
	result := make([]int64, 0)
	for key, value := range stats {
		if value == 0 || !strings.HasPrefix(key, prefix) {
			continue
		}
		recipeID, err := strconv.ParseInt(strings.TrimPrefix(key, prefix), 10, 64)
		if err == nil && recipeID > 0 {
			result = append(result, recipeID)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}
