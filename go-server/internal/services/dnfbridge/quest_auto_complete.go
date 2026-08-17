package dnfbridge

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"time"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfquest "longheng.io/server/internal/modules/dnf/quest"
)

const (
	currentAutoCompleteMainQuestRequestSize  = 4
	currentAutoCompleteMainQuestResponseSize = 13
	currentAutoCompleteMainQuestMaxLevel     = 89
)

// The current EXE's sub_79EB70 consumes one packed 13-byte result directly:
// request selector, highest completed quest id, cutoff level, committed flag.
// This is not the common one-byte success ACK used by op31/op34.
func buildCurrentAutoCompleteMainQuestResponse(
	requestSelector uint32,
	highestCompletedQuest uint32,
	cutoffLevel uint32,
	committed bool,
) []byte {
	body := make([]byte, currentAutoCompleteMainQuestResponseSize)
	binary.LittleEndian.PutUint32(body[0:4], requestSelector)
	binary.LittleEndian.PutUint32(body[4:8], highestCompletedQuest)
	binary.LittleEndian.PutUint32(body[8:12], cutoffLevel)
	if committed {
		body[12] = 1
	}
	return body
}

func (s *Service) handleCurrentAutoCompleteMainQuest(session *gameSession, body []byte) error {
	if session == nil || session.selectedCharacterID == 0 {
		return nil
	}
	if len(body) != currentAutoCompleteMainQuestRequestSize {
		s.logGameEvent(session, "game-auto-complete-main-quest-blocked",
			"body_len", len(body),
			"reason", "current_exe_op1429_requires_exact_u32_request")
		return nil
	}
	requestSelector := binary.LittleEndian.Uint32(body)

	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.CharacterSettlement == nil {
		s.logGameEvent(session, "game-auto-complete-main-quest-blocked",
			"request_selector", requestSelector,
			"reason", "character_settlement_transaction_unavailable")
		return nil
	}
	_, _, character, found := s.selectedCharacterForEnter(ctx, session)
	if !found || character.Level <= 1 {
		s.logGameEvent(session, "game-auto-complete-main-quest-blocked",
			"request_selector", requestSelector,
			"character_found", found,
			"level", character.Level,
			"reason", "selected_character_or_previous_level_unavailable")
		return nil
	}
	job, validJob := characterJobByte(character)
	if !validJob {
		s.logGameEvent(session, "game-auto-complete-main-quest-blocked",
			"request_selector", requestSelector,
			"job", character.Job,
			"reason", "selected_character_job_invalid")
		return nil
	}
	cutoffLevel := character.Level - 1
	if cutoffLevel > currentAutoCompleteMainQuestMaxLevel {
		cutoffLevel = currentAutoCompleteMainQuestMaxLevel
	}
	catalog, err := s.loadQuestCatalog(ctx)
	if err != nil {
		s.logGameEvent(session, "game-auto-complete-main-quest-blocked",
			"request_selector", requestSelector,
			"reason", "quest_catalog_unavailable",
			"error", err)
		return nil
	}
	owner, err := dnfquest.NewOwner(repositories)
	if err != nil {
		return err
	}
	characterID := character.CharacterID
	if characterID == "" {
		characterID = strconv.Itoa(int(session.selectedCharacterID))
	}
	targetQuestID := int64(requestSelector)
	targetSource := "current_exe_nonzero_per_entry_selector"
	if requestSelector == 0 {
		targetSource = "zero_selector_requires_exactly_one_eligible_active_epic"
	}
	result, err := owner.ApplyAutoCompleteMain(ctx, catalog, dnfquest.AutoCompleteMainInput{
		CharacterID: characterID,
		Eligibility: dnfquest.CharacterEligibility{
			Level:    character.Level,
			Job:      int(job),
			GrowType: int(numericCharacterStatValue(character, "grow_type")),
		},
		CutoffLevel:   cutoffLevel,
		TargetQuestID: targetQuestID,
		CompletedAt:   time.Now().UTC(),
	})
	if err != nil {
		if errors.Is(err, dnfquest.ErrAutoCompleteTargetAmbiguous) ||
			errors.Is(err, dnfquest.ErrAutoCompleteTargetInvalid) {
			response := buildCurrentAutoCompleteMainQuestResponse(
				requestSelector,
				0,
				uint32(cutoffLevel),
				false,
			)
			s.logGameEvent(session, "game-auto-complete-main-quest-single-target-required",
				"request_selector", requestSelector,
				"target_quest_id", targetQuestID,
				"target_source", targetSource,
				"cutoff_level", cutoffLevel,
				"reason", err,
				"quest_mutation", "none",
				"msg_id", uint16(dnfenum.CmdPacketClearQuestTicket),
				"classification", dnfproto.DefaultChannelClassification,
				"body_len", len(response))
			return s.sendGameUpperRawClass(
				session,
				uint16(dnfenum.CmdPacketClearQuestTicket),
				response,
				dnfproto.DefaultChannelClassification,
			)
		}
		s.logGameEvent(session, "game-auto-complete-main-quest-blocked",
			"request_selector", requestSelector,
			"target_quest_id", targetQuestID,
			"target_source", targetSource,
			"cutoff_level", cutoffLevel,
			"reason", "pvf_db_atomic_selected_main_quest_completion_rejected",
			"error", err)
		return nil
	}
	if result.HighestCompletedQuest < 0 || result.HighestCompletedQuest > int64(^uint32(0)) {
		return fmt.Errorf("auto-complete highest quest id out of wire range: %d", result.HighestCompletedQuest)
	}
	response := buildCurrentAutoCompleteMainQuestResponse(
		requestSelector,
		uint32(result.HighestCompletedQuest),
		uint32(cutoffLevel),
		true,
	)
	s.logGameEvent(session, "game-auto-complete-main-quest-success-send",
		"request_selector", requestSelector,
		"target_quest_id", result.HighestCompletedQuest,
		"target_source", targetSource,
		"cutoff_level", cutoffLevel,
		"targeted_active_epic_count", len(result.EligibleQuestIDs),
		"changed_epic_count", len(result.ChangedQuestIDs),
		"changed_linked_sub_count", len(result.ChangedLinkedSubIDs),
		"active_completed_count", result.ActiveCompletedCount,
		"highest_completed_quest", result.HighestCompletedQuest,
		"idempotent", result.Idempotent,
		"msg_id", uint16(dnfenum.CmdPacketClearQuestTicket),
		"classification", dnfproto.DefaultChannelClassification,
		"body_len", len(response),
		"body_source", "current_exe_sub_79EB70_exact_13_byte_reader_and_atomic_single_selected_pvf_epic_completion_without_rewards")
	if err := s.sendGameUpperRawClass(
		session,
		uint16(dnfenum.CmdPacketClearQuestTicket),
		response,
		dnfproto.DefaultChannelClassification,
	); err != nil {
		return err
	}
	if err := s.sendCurrentClearQuestListFromCommittedQuest(
		session,
		result.PostCommitQuest,
		"current_exe_op1429_after_atomic_no_reward_main_quest_completion",
	); err != nil {
		return err
	}
	if err := s.sendCurrentActiveQuestSnapshotForSession(
		session,
		"current_exe_op1429_after_completed_quest_bitmap",
	); err != nil {
		return err
	}
	return s.sendCurrentAcceptableQuestListOnlyForSession(
		session,
		"current_exe_op1429_after_active_quest_snapshot",
	)
}
