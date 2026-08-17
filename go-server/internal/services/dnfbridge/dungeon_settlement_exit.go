package dnfbridge

import (
	"bytes"
	"context"
	"encoding/binary"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

const currentDungeonCharacterStatisticBodySize = 24

// handleDungeonCharacterStatistic records the six-u32 telemetry packet emitted
// by the current EXE's completed tutorial/settlement manager. It is a barrier
// observation only: the values never become authoritative rewards or assets,
// and this request has no proven server response.
func (s *Service) handleDungeonCharacterStatistic(session *gameSession, body []byte) error {
	if session == nil {
		return nil
	}
	if len(body) != currentDungeonCharacterStatisticBodySize {
		s.logGameEvent(session, "game-dungeon-character-statistic-blocked",
			"body_len", len(body),
			"expected_body_len", currentDungeonCharacterStatisticBodySize,
			"reason", "current_exe_op123_six_u32_boundary_mismatch")
		return nil
	}

	session.dungeon.mu.Lock()
	defer session.dungeon.mu.Unlock()
	runtime := session.dungeon.runtime
	if !completedDungeonSettlementEntryReady(runtime, session.selectedCharacterID) {
		s.logGameEvent(session, "game-dungeon-character-statistic-blocked",
			"char_id", session.selectedCharacterID,
			"body_len", len(body),
			"reason", "completed_settlement_entry_owner_not_ready")
		return nil
	}
	if runtime.settlementStatisticReceived {
		if bytes.Equal(runtime.settlementStatisticBody, body) {
			s.logGameEvent(session, "game-dungeon-character-statistic-replay",
				"char_id", session.selectedCharacterID,
				"dungeon_id", runtime.Dungeon.ID,
				"body_len", len(body),
				"reason", "exact_replay_is_idempotent")
			return nil
		}
		s.logGameEvent(session, "game-dungeon-character-statistic-blocked",
			"char_id", session.selectedCharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"body_len", len(body),
			"reason", "conflicting_replay_after_first_capture")
		return nil
	}

	runtime.settlementStatisticReceived = true
	runtime.settlementStatisticBody = append([]byte(nil), body...)
	s.logGameEvent(session, "game-dungeon-character-statistic-captured",
		"char_id", session.selectedCharacterID,
		"dungeon_id", runtime.Dungeon.ID,
		"body_len", len(body),
		"value0", binary.LittleEndian.Uint32(body[0:4]),
		"value1", binary.LittleEndian.Uint32(body[4:8]),
		"value2", binary.LittleEndian.Uint32(body[8:12]),
		"value3", binary.LittleEndian.Uint32(body[12:16]),
		"value4", binary.LittleEndian.Uint32(body[16:20]),
		"value5", binary.LittleEndian.Uint32(body[20:24]),
		"authoritative_for_rewards", false,
		"response_sent", false,
		"source", "current_exe_sub_1CAECD0_six_u32_writer")
	return nil
}

// handleDungeonGiveupGame closes both current EXE op42 meanings without
// conflating them: an active run remains a give-up request, while a completed
// run is accepted only after the server's settlement-entry op31, current EXE
// op46 play-result request, and post-settlement op123 statistic barrier. The
// client owns the wait before it emits op42; the server must not replace that
// lifecycle with an unrelated timer. An active-run typed op24 remains pending
// until positive town-side scene evidence commits it. A completed tutorial's
// first op42 instead uses the already accepted full completed-town route and
// detaches only after that route writes its typed op24.
func (s *Service) handleDungeonGiveupGame(session *gameSession, body []byte) error {
	if session == nil {
		return nil
	}
	if len(body) != 0 {
		s.logGameEvent(session, "game-dungeon-giveup-game-blocked",
			"body_len", len(body),
			"reason", "current_exe_op42_body_must_be_empty")
		return nil
	}
	if session.selectedCharacterID == 0 {
		s.logGameEvent(session, "game-dungeon-giveup-game-blocked",
			"body_len", len(body),
			"reason", "selected_character_missing")
		return nil
	}
	if _, err := s.persistCurrentActiveTutorialExit(
		session,
		session.selectedCharacterID,
		"confirmed_mid_tutorial_bodyless_op42_active_run_giveup",
	); err != nil {
		s.logGameEvent(session, "game-dungeon-giveup-game-blocked",
			"char_id", session.selectedCharacterID,
			"reason", "confirmed_tutorial_abandonment_persistence_failed",
			"error", err)
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	transition, err := s.prepareCurrentDungeonTownTransitionForSession(ctx, session, session.selectedCharacterID)
	if err != nil {
		s.logGameEvent(session, "game-dungeon-giveup-game-blocked",
			"char_id", session.selectedCharacterID,
			"reason", "persisted_town_transition_unavailable",
			"error", err)
		return nil
	}

	session.dungeon.mu.Lock()
	runtime := session.dungeon.runtime
	if runtime == nil || runtime.Session == nil || !dungeonRuntimeOwnsCharacter(runtime, session.selectedCharacterID) {
		session.dungeon.mu.Unlock()
		s.logGameEvent(session, "game-dungeon-giveup-game-blocked",
			"char_id", session.selectedCharacterID,
			"reason", "active_dungeon_runtime_owner_missing")
		return nil
	}
	snapshot := runtime.Session.Snapshot()
	if snapshot.Run.Status == worldmap.DungeonRunCompleted &&
		!completedDungeonSettlementResultReady(runtime, session.selectedCharacterID) {
		session.dungeon.mu.Unlock()
		s.logGameEvent(session, "game-dungeon-giveup-game-blocked",
			"char_id", session.selectedCharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"run_status", snapshot.Run.Status,
			"settlement_entry_sent", runtime.settlementEntrySent,
			"settlement_play_result_received", runtime.settlementPlayResultReceived,
			"settlement_statistic_received", runtime.settlementStatisticReceived,
			"reason", "completed_run_missing_play_result_or_statistic_barrier")
		return nil
	}
	if snapshot.Run.Status != worldmap.DungeonRunActive && snapshot.Run.Status != worldmap.DungeonRunCompleted {
		session.dungeon.mu.Unlock()
		s.logGameEvent(session, "game-dungeon-giveup-game-blocked",
			"char_id", session.selectedCharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"run_status", snapshot.Run.Status,
			"reason", "dungeon_run_not_active_or_completed")
		return nil
	}
	if runtime.townReturnPending && snapshot.Run.Status != worldmap.DungeonRunCompleted {
		session.dungeon.mu.Unlock()
		s.logGameEvent(session, "game-dungeon-giveup-game-replay",
			"char_id", session.selectedCharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"run_status", snapshot.Run.Status,
			"op24_sent", runtime.townReturnOp24Sent,
			"reason", "town_transition_already_pending")
		return nil
	}

	source := "current_exe_bodyless_op42_active_run_giveup"
	if snapshot.Run.Status == worldmap.DungeonRunCompleted {
		source = "current_exe_op31_completed_tutorial_or_settlement_timed_op42"
		completedTutorial := isPVFTutorialDungeon(runtime)
		if completedTutorial {
			source = "current_exe_completed_pvf_tutorial_first_op42_return_to_town"
		}
		if !completedTutorial && runtime.settlementCardLayoutSent &&
			!runtime.settlementCardSideSelectionSent[dungeonCardSideFree] {
			if err := s.selectCurrentDungeonCardLocked(
				session,
				runtime,
				dungeonCardSideFree,
				0,
				"current_exe_completed_op42_auto_flip_free_row_before_exit",
			); err != nil {
				s.logGameEvent(session, "game-dungeon-giveup-game-completed-auto-flip-blocked",
					"char_id", session.selectedCharacterID,
					"dungeon_id", runtime.Dungeon.ID,
					"request_msg_id", uint16(dnfenum.CmdPacketGiveupGame),
					"error", err)
			}
		}
		if !completedTutorial && runtime.settlementCardSelectionSent && !runtime.settlementCardRewardCommitted {
			if err := s.commitCurrentDungeonCardRewardLocked(session, runtime, "current_exe_completed_op42_reward_retry_before_exit"); err != nil {
				s.logGameEvent(session, "game-dungeon-giveup-game-completed-exit-reward-retry-blocked",
					"char_id", session.selectedCharacterID,
					"dungeon_id", runtime.Dungeon.ID,
					"request_msg_id", uint16(dnfenum.CmdPacketGiveupGame),
					"error", err)
			}
		}
		if !completedTutorial && (!runtime.settlementCardLayoutSent ||
			!runtime.settlementCardSelectionSent || !runtime.settlementCardRewardCommitted ||
			runtime.settlementPhase < currentDungeonSettlementPhaseRewardCommitted) {
			s.logGameEvent(session, "game-dungeon-giveup-game-completed-exit-deferred-before-card-reward",
				"char_id", session.selectedCharacterID,
				"dungeon_id", runtime.Dungeon.ID,
				"request_msg_id", uint16(dnfenum.CmdPacketGiveupGame),
				"layout_sent", runtime.settlementCardLayoutSent,
				"selection_sent", runtime.settlementCardSelectionSent,
				"reward_committed", runtime.settlementCardRewardCommitted,
				"settlement_phase", runtime.settlementPhase.String(),
				"reason", "current_exe_card_request_chain_not_finished")
			session.dungeon.mu.Unlock()
			return nil
		}
		runtime.advanceSettlementPhase(currentDungeonSettlementPhaseEnding)
		session.dungeon.mu.Unlock()
		return s.sendCurrentCompletedDungeonReturnToTown(
			session,
			runtime,
			transition,
			uint16(dnfenum.CmdPacketGiveupGame),
			source,
		)
	}
	err = s.sendCurrentDungeonReturnToTownLocked(
		session,
		runtime,
		transition,
		uint16(dnfenum.CmdPacketGiveupGame),
		source,
	)
	session.dungeon.mu.Unlock()
	return err
}

func completedDungeonSettlementExitReady(runtime *runtimeDungeonState, characterID uint16) bool {
	if !completedDungeonSettlementResultReady(runtime, characterID) {
		return false
	}
	if isPVFTutorialDungeon(runtime) {
		return true
	}
	return runtime.settlementCardLayoutSent && runtime.settlementCardSelectionSent &&
		runtime.settlementCardRewardCommitted &&
		runtime.settlementPhase >= currentDungeonSettlementPhaseRewardCommitted
}

func completedDungeonSettlementResultReady(runtime *runtimeDungeonState, characterID uint16) bool {
	return completedDungeonSettlementEntryReady(runtime, characterID) &&
		runtime.settlementPlayResultReceived && runtime.settlementStatisticReceived &&
		runtime.settlementClearRewardSent &&
		runtime.settlementPhase >= currentDungeonSettlementPhaseResultShown
}

func completedDungeonSettlementEntryReady(runtime *runtimeDungeonState, characterID uint16) bool {
	if runtime == nil || runtime.Session == nil || !dungeonRuntimeOwnsCharacter(runtime, characterID) ||
		!runtime.settlementEntrySent {
		return false
	}
	return runtime.Session.Snapshot().Run.Status == worldmap.DungeonRunCompleted
}
