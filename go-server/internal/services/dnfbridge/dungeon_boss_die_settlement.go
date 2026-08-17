package dnfbridge

import (
	"context"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

// completeCurrentDungeonBossDieCheckLocked closes the current-EXE final CMT
// request/response path and enters the independently observed normal settlement
// handshake. For an ordinary dungeon, persist the clear-map quest marker,
// publish its trigger-zero op574 snapshot, reply with op115, then send the
// bodyless op31 that can make the current EXE construct op46. Town transition
// and rewards do not occur at this stage.
func (s *Service) completeCurrentDungeonBossDieCheckLocked(
	session *gameSession,
	runtime *runtimeDungeonState,
	source string,
) error {
	if session == nil || runtime == nil || runtime.Session == nil || session.dungeon.runtime != runtime {
		return nil
	}
	if !dungeonRuntimeOwnsCharacter(runtime, session.selectedCharacterID) {
		s.logGameEvent(session, "game-dungeon-boss-die-check-completion-blocked",
			"char_id", session.selectedCharacterID,
			"runtime_char_id", runtime.Character.CharacterID,
			"source", source,
			"reason", "active_dungeon_runtime_character_owner_mismatch")
		return nil
	}
	snapshot := runtime.Session.Snapshot()
	if !runtime.bossDieCheckAccepted || snapshot.Run.Status != worldmap.DungeonRunCompleted {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	tutorialDungeon := isPVFTutorialDungeon(runtime)
	if tutorialDungeon && !runtime.tutorialCompletionPersisted {
		previousFlag, err := s.persistCurrentDungeonTutorialCompletion(ctx, session.selectedCharacterID)
		if err != nil {
			s.logGameEvent(session, "game-dungeon-boss-die-check-completion-blocked",
				"char_id", session.selectedCharacterID,
				"dungeon_id", runtime.Dungeon.ID,
				"previous_tutorial_completed", previousFlag,
				"source", source,
				"reason", "tutorial_completion_persistence_failed",
				"error", err)
			return nil
		}
		runtime.tutorialCompletionPersisted = true
		applyCurrentDungeonTutorialCompletionStats(&runtime.Character)
		s.logGameEvent(session, "game-dungeon-tutorial-completion-persisted",
			"char_id", session.selectedCharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"previous_tutorial_completed", previousFlag,
			"tutorial_completed", currentDungeonTutorialCompleteFlag,
			"select_tutorial_index_count", 0,
			"source", "validated_final_op117_after_current_exe_cmt_on_end")
	}
	if !tutorialDungeon && !runtime.tutorialCompletionPersisted {
		// The settlement owner predates ordinary-dungeon completion and uses
		// this historical flag as its persistence barrier. Ordinary dungeons
		// have no tutorial row to persist: satisfy only that in-memory barrier;
		// Character.Stats and the character repository remain unchanged.
		runtime.tutorialCompletionPersisted = true
		s.logGameEvent(session, "game-dungeon-ordinary-completion-validated",
			"char_id", session.selectedCharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"tutorial_completion_database_mutated", false,
			"tutorial_completion_character_snapshot_mutated", false,
			"settlement_persistence_barrier_ready", true,
			"source", "owned_cleared_runtime_boss_op117")
	}
	if !runtime.clearMapCompletionPhaseAPersisted {
		if err := s.persistCurrentDungeonClearMapCompletionPhaseA(ctx, session, runtime, snapshot); err != nil {
			s.logGameEvent(session, "game-dungeon-boss-die-check-completion-quest-sync-nonblocking",
				"char_id", session.selectedCharacterID,
				"dungeon_id", runtime.Dungeon.ID,
				"maze_index", runtime.MazeIndex,
				"map_id", snapshot.Scene.Map.Map.ID,
				"completion_key", runtime.clearMapCompletionKey,
				"source", source,
				"reason", "clear_map_phase_a_persistence_failed",
				"op115_sent", runtime.bossDieCheckResponseSent,
				"settlement_entry_sent", runtime.settlementEntrySent,
				"error", err)
			// 86JP treats clear-map quest synchronization as a side effect of
			// ClearDungeon, not as the owner of the result screen.  A quest/PVF
			// snapshot failure must not strand an already-completed dungeon in
			// the cleared-but-no-score state: continue to op115/op31 and let the
			// quest owner repair/refresh the task state independently.
			s.logGameEvent(session, "game-dungeon-clear-map-phase-a-deferred-nonblocking",
				"char_id", session.selectedCharacterID,
				"dungeon_id", runtime.Dungeon.ID,
				"maze_index", runtime.MazeIndex,
				"map_id", snapshot.Scene.Map.Map.ID,
				"completion_key", runtime.clearMapCompletionKey,
				"source", source,
				"reason", "86jp_clear_dungeon_result_not_blocked_by_quest_sync",
				"error", err)
		}
	}
	if !tutorialDungeon && runtime.clearMapCompletionPhaseAPersisted {
		if err := s.sendCurrentDungeonClearMapCompletionNotificationLocked(ctx, session, runtime); err != nil {
			s.logGameEvent(session, "game-dungeon-boss-die-check-completion-active-snapshot-nonblocking",
				"char_id", session.selectedCharacterID,
				"dungeon_id", runtime.Dungeon.ID,
				"maze_index", runtime.MazeIndex,
				"map_id", snapshot.Scene.Map.Map.ID,
				"completion_key", runtime.clearMapCompletionKey,
				"source", source,
				"reason", "clear_map_active_quest_snapshot_failed",
				"notification_closed", runtime.clearMapCompletionNotificationClosed,
				"op115_sent", runtime.bossDieCheckResponseSent,
				"settlement_entry_sent", runtime.settlementEntrySent,
				"error", err)
			// Same as Phase-A above: the active-task snapshot is useful for UI,
			// but it cannot own the dungeon score/card lifecycle once combat is
			// authoritatively complete.
			s.logGameEvent(session, "game-dungeon-clear-map-active-quest-snapshot-deferred-nonblocking",
				"char_id", session.selectedCharacterID,
				"dungeon_id", runtime.Dungeon.ID,
				"maze_index", runtime.MazeIndex,
				"map_id", snapshot.Scene.Map.Map.ID,
				"completion_key", runtime.clearMapCompletionKey,
				"source", source,
				"reason", "86jp_clear_dungeon_result_not_blocked_by_task_snapshot",
				"error", err)
		}
	} else if !tutorialDungeon {
		s.logGameEvent(session, "game-dungeon-clear-map-active-quest-snapshot-skipped",
			"char_id", session.selectedCharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"maze_index", runtime.MazeIndex,
			"map_id", snapshot.Scene.Map.Map.ID,
			"completion_key", runtime.clearMapCompletionKey,
			"reason", "phase_a_not_persisted_but_result_flow_continues",
			"reward_granted", false)
	}

	if !runtime.bossDieCheckResponseSent {
		responseBody := buildCurrentDungeonBossDieCheckResponse(runtime.bossDieCheckTargetObjectKey)
		s.logGameEvent(session, "game-dungeon-boss-die-check-op115-send",
			"request_msg_id", uint16(dnfenum.CmdPacketBossDieCheck),
			"response_msg_id", uint16(dnfenum.CmdPacketNotifyBossDieCheck),
			"classification", 0,
			"char_id", session.selectedCharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"maze_index", runtime.MazeIndex,
			"room", snapshot.Run.Current.String(),
			"related_actor_object_key", runtime.bossDieCheckRelatedActorObjectKey,
			"target_object_key", runtime.bossDieCheckTargetObjectKey,
			"body_len", len(responseBody),
			"ordering", "ordinary_phase_a_op574_then_validated_op117_response_before_bodyless_settlement_op31",
			"body_source", "current_exe_sub_1D3F4D0_status1_complete1_real_target_key")
		if err := s.sendGameUpperRawClass(
			session,
			uint16(dnfenum.CmdPacketNotifyBossDieCheck),
			responseBody,
			0,
		); err != nil {
			return err
		}
		runtime.bossDieCheckResponseSent = true
	}

	return s.sendCurrentDungeonSettlementEntryLocked(
		session,
		runtime,
		"current_exe_final_cmt_op117_op115_bodyless_op31_"+source,
	)
}
