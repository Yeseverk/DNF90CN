package dnfbridge

import "longheng.io/server/internal/modules/dnf/dnfenum"

const (
	currentDungeonPlayResultClientRankPointOffset = 10
	currentDungeonCharacterStateMsgID             = uint16(dnfenum.CmdPacketFinishLoading)
)

// sendCurrentDungeonSettlementResultsLocked sends only a previously frozen,
// committed plan. It never calculates or fills a reward while handling op46.
// The caller owns session.dungeon.mu.
func (s *Service) sendCurrentDungeonSettlementResultsLocked(
	session *gameSession,
	runtime *runtimeDungeonState,
	source string,
) error {
	if session == nil || runtime == nil || session.dungeon.runtime != runtime ||
		!runtime.settlementPlayResultReceived ||
		runtime.settlementPhase < currentDungeonSettlementPhaseClearEnabled ||
		runtime.settlementPhase >= currentDungeonSettlementPhaseEnding {
		return nil
	}
	plan := runtime.settlementResultPlan
	if plan == nil {
		s.logGameEvent(session, "game-dungeon-settlement-results-blocked",
			"char_id", session.selectedCharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"source", source,
			"reason", "committed_dungeon_reward_snapshot_missing")
		return nil
	}
	if plan.CharacterID != session.selectedCharacterID ||
		!dungeonRuntimeOwnsCharacter(runtime, plan.CharacterID) ||
		plan.CompletionKey == "" || plan.Source == "" ||
		(runtime.clearMapCompletionKey != "" && plan.CompletionKey != runtime.clearMapCompletionKey) {
		s.logGameEvent(session, "game-dungeon-settlement-results-blocked",
			"char_id", session.selectedCharacterID,
			"plan_char_id", plan.CharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"plan_completion_key", plan.CompletionKey,
			"runtime_completion_key", runtime.clearMapCompletionKey,
			"source", source,
			"reason", "committed_settlement_plan_owner_mismatch")
		return nil
	}
	if len(runtime.settlementPlayResultBody) <= currentDungeonPlayResultClientRankPointOffset ||
		runtime.settlementPlayResultBody[currentDungeonPlayResultClientRankPointOffset] != plan.ClientRankPoint {
		s.logGameEvent(session, "game-dungeon-settlement-results-blocked",
			"char_id", session.selectedCharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"request_body_len", len(runtime.settlementPlayResultBody),
			"planned_client_rank_point", plan.ClientRankPoint,
			"source", source,
			"reason", "op46_client_rank_point_conflicts_with_committed_plan")
		return nil
	}
	if len(plan.PlayResultBody) == 0 || len(plan.CharacterBody) != currentFinishLoadingCharacterStateBodySize ||
		len(plan.ClearRewardBody) < currentDungeonClearRewardMinimumBodySize {
		s.logGameEvent(session, "game-dungeon-settlement-results-blocked",
			"char_id", session.selectedCharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"op34_body_len", len(plan.PlayResultBody),
			"op37_body_len", len(plan.CharacterBody),
			"op35_body_len", len(plan.ClearRewardBody),
			"source", source,
			"reason", "frozen_settlement_packet_plan_shape_invalid")
		return nil
	}

	if !runtime.settlementResultNoticeSent {
		if err := s.sendGameUpperRawClass(session, currentDungeonPlayResultNoticeMsgID, plan.PlayResultBody, 0); err != nil {
			return err
		}
		runtime.settlementResultNoticeSent = true
		s.logGameEvent(session, "game-dungeon-settlement-result-op34-sent",
			"char_id", plan.CharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"completion_key", plan.CompletionKey,
			"body_len", len(plan.PlayResultBody),
			"body_source", "current_exe_sub_1D3BAE0_committed_runtime_result")
	}
	if !runtime.settlementCharacterStateSent {
		if err := s.sendGameUpperRawClass(session, currentDungeonCharacterStateMsgID, plan.CharacterBody, 0); err != nil {
			return err
		}
		runtime.settlementCharacterStateSent = true
		s.logGameEvent(session, "game-dungeon-settlement-character-op37-sent",
			"char_id", plan.CharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"completion_key", plan.CompletionKey,
			"body_len", len(plan.CharacterBody),
			"body_source", "current_exe_sub_1D78240_post_commit_character_snapshot")
	}
	if !runtime.settlementClearRewardSent {
		if err := s.sendGameUpperRawClass(session, currentDungeonClearRewardMsgID, plan.ClearRewardBody, 0); err != nil {
			return err
		}
		runtime.settlementClearRewardSent = true
		s.logGameEvent(session, "game-dungeon-settlement-clear-reward-op35-sent",
			"char_id", plan.CharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"completion_key", plan.CompletionKey,
			"reward_source", plan.Source,
			"body_len", len(plan.ClearRewardBody),
			"body_source", "current_exe_sub_1D4D380_committed_reward_snapshot")
	}
	if len(plan.DungeonPermissionBody) != 0 && !runtime.settlementDungeonPermissionSent {
		if err := s.sendGameUpperRawClass(session, currentDungeonResourceStateMsgID, plan.DungeonPermissionBody, 0); err != nil {
			return err
		}
		runtime.settlementDungeonPermissionSent = true
		s.logGameEvent(session, "game-dungeon-settlement-permission-op5-sent",
			"char_id", plan.CharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"completion_key", plan.CompletionKey,
			"body_len", len(plan.DungeonPermissionBody),
			"body_source", "current_exe_sub_1D37AC0_u16_count_u32_key_u8_state",
			"state_source", "persisted_dungeon_clear_state_after_settlement")
	}
	runtime.advanceSettlementPhase(currentDungeonSettlementPhaseResultShown)
	// Award lucky star for clearing a suitable-level dungeon (86JP PR #461).
	s.awardSuitableDungeonLuckyStar(session, runtime)
	return nil
}
