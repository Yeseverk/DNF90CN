package dnfbridge

import (
	"bytes"
	"context"
	"encoding/hex"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

const (
	// Opcode 31 is direction-dependent. In this server-to-client dungeon
	// context the current EXE registers it as the settlement entry;
	// the generated command enum names the client-to-server opcode 31 as
	// ACCEPT_QUEST and must not be used to describe this notification.
	currentDungeonSettlementEntryMsgID = uint16(31)

	currentDungeonPlayResultBaseSize           = 42
	currentDungeonPlayResultOptionalBaseSize   = 46
	currentDungeonPlayResultDynamicRowSize     = 7
	currentDungeonPlayResultMaximumDynamicRows = 8
)

// sendCurrentDungeonSettlementEntryLocked advances only the current-EXE
// handshake that is proven by the op31 reader/writer pair. 86JP's
// BuildEnableClearDungeon writes one zero byte; sending an empty body leaves
// the current client in a cleared-but-not-result-ready state and it never
// constructs op46.
func (s *Service) sendCurrentDungeonSettlementEntryLocked(
	session *gameSession,
	runtime *runtimeDungeonState,
	source string,
) error {
	if session == nil || runtime == nil || runtime.Session == nil || session.dungeon.runtime != runtime {
		return nil
	}
	if !dungeonRuntimeOwnsCharacter(runtime, session.selectedCharacterID) {
		s.logGameEvent(session, "game-dungeon-settlement-entry-blocked",
			"char_id", session.selectedCharacterID,
			"runtime_char_id", runtime.Character.CharacterID,
			"source", source,
			"reason", "active_dungeon_runtime_character_owner_mismatch")
		return nil
	}
	snapshot := runtime.Session.Snapshot()
	if !currentDungeonSettlementCompletionReady(runtime, snapshot.Run.Status) {
		s.logGameEvent(session, "game-dungeon-settlement-entry-blocked",
			"char_id", session.selectedCharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"boss_die_check_accepted", runtime.bossDieCheckAccepted,
			"tutorial_final_room_clear_accepted", runtime.tutorialFinalRoomClearAccepted,
			"completion_persisted", runtime.tutorialCompletionPersisted,
			"op115_sent", runtime.bossDieCheckResponseSent,
			"run_status", snapshot.Run.Status,
			"source", source,
			"reason", "validated_completion_stages_incomplete")
		return nil
	}
	s.cancelCurrentDungeonDeathReturnLocked(session, runtime, "normal_settlement_entry")
	if runtime.settlementEntrySent {
		s.logGameEvent(session, "game-dungeon-settlement-entry-replay",
			"char_id", session.selectedCharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"msg_id", currentDungeonSettlementEntryMsgID,
			"source", source,
			"reason", "bodyless_op31_already_sent")
		return nil
	}
	// Freeze the tutorial-compatible result timestamp before the first op31
	// write. Ordinary completions own clearMapCompletionAt from Phase A; a
	// tutorial may not have that row and therefore uses this stable boundary.
	// Keeping it across a failed write makes the later op46 result plan replay
	// deterministic rather than measuring socket retry time.
	if runtime.settlementEntryAt.IsZero() {
		runtime.settlementEntryAt = s.gameplayNow()
	}

	s.logGameEvent(session, "game-dungeon-settlement-entry-op31-send",
		"char_id", session.selectedCharacterID,
		"dungeon_id", runtime.Dungeon.ID,
		"maze_index", runtime.MazeIndex,
		"room", snapshot.Run.Current.String(),
		"msg_id", currentDungeonSettlementEntryMsgID,
		"classification", 0,
		"body_len", 1,
		"source", source,
		"body_source", "current_exe_enable_clear_dungeon_byte0",
		"next_stage", "await_current_exe_c2s_op46_without_reward_mutation")
	if err := s.sendGameUpperRawClass(session, currentDungeonSettlementEntryMsgID, []byte{0}, 0); err != nil {
		return err
	}
	runtime.settlementEntrySent = true
	runtime.advanceSettlementPhase(currentDungeonSettlementPhaseClearEnabled)
	return nil
}

func (s *Service) handleDungeonSetPlayResult(session *gameSession, body []byte) error {
	dynamicRows, optionalField, valid := currentDungeonPlayResultShape(body)
	if !valid {
		s.logGameEvent(session, "game-dungeon-set-play-result-blocked",
			"body_len", len(body),
			"reason", "current_exe_op46_writer_boundary_mismatch")
		return nil
	}
	if session == nil || session.selectedCharacterID == 0 {
		return nil
	}

	session.dungeon.mu.Lock()
	defer session.dungeon.mu.Unlock()
	runtime := session.dungeon.runtime
	if runtime == nil || runtime.Session == nil {
		s.logGameEvent(session, "game-dungeon-set-play-result-blocked",
			"body_len", len(body),
			"dynamic_rows", dynamicRows,
			"optional_u32", optionalField,
			"reason", "active_dungeon_runtime_missing")
		return nil
	}
	if !dungeonRuntimeOwnsCharacter(runtime, session.selectedCharacterID) {
		s.logGameEvent(session, "game-dungeon-set-play-result-blocked",
			"char_id", session.selectedCharacterID,
			"runtime_char_id", runtime.Character.CharacterID,
			"body_len", len(body),
			"reason", "active_dungeon_runtime_character_owner_mismatch")
		return nil
	}
	snapshot := runtime.Session.Snapshot()
	if !runtime.settlementEntrySent ||
		runtime.settlementPhase < currentDungeonSettlementPhaseClearEnabled ||
		!currentDungeonSettlementCompletionReady(runtime, snapshot.Run.Status) {
		s.logGameEvent(session, "game-dungeon-set-play-result-blocked",
			"char_id", session.selectedCharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"body_len", len(body),
			"settlement_entry_sent", runtime.settlementEntrySent,
			"settlement_phase", runtime.settlementPhase.String(),
			"boss_die_check_accepted", runtime.bossDieCheckAccepted,
			"tutorial_final_room_clear_accepted", runtime.tutorialFinalRoomClearAccepted,
			"completion_persisted", runtime.tutorialCompletionPersisted,
			"op115_sent", runtime.bossDieCheckResponseSent,
			"run_status", snapshot.Run.Status,
			"reason", "settlement_handshake_not_ready")
		return nil
	}
	if runtime.settlementPlayResultReceived {
		if bytes.Equal(runtime.settlementPlayResultBody, body) {
			s.logGameEvent(session, "game-dungeon-set-play-result-replay",
				"char_id", session.selectedCharacterID,
				"dungeon_id", runtime.Dungeon.ID,
				"body_len", len(body),
				"dynamic_rows", dynamicRows,
				"optional_u32", optionalField,
				"reason", "exact_op46_already_captured_resume_frozen_result_plan")
			if runtime.settlementResultPlan == nil {
				if err := s.produceCurrentDungeonSettlementPlanLocked(
					context.Background(), session, runtime,
				); err != nil {
					s.logGameEvent(session, "game-dungeon-settlement-plan-blocked",
						"char_id", session.selectedCharacterID,
						"dungeon_id", runtime.Dungeon.ID,
						"completion_key", runtime.clearMapCompletionKey,
						"source", "exact_op46_replay",
						"error", err)
					return nil
				}
			}
			return s.sendCurrentDungeonSettlementResultsLocked(
				session,
				runtime,
				"exact_op46_replay",
			)
		}
		s.logGameEvent(session, "game-dungeon-set-play-result-blocked",
			"char_id", session.selectedCharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"body_len", len(body),
			"accepted_body_len", len(runtime.settlementPlayResultBody),
			"reason", "conflicting_op46_after_settlement_owner_bound")
		return nil
	}

	runtime.settlementPlayResultReceived = true
	runtime.settlementPlayResultBody = append([]byte(nil), body...)
	runtime.settlementPlayResultDynamicRows = uint8(dynamicRows)
	runtime.settlementPlayResultOptionalField = optionalField
	s.logGameEvent(session, "game-dungeon-set-play-result-captured",
		"char_id", session.selectedCharacterID,
		"dungeon_id", runtime.Dungeon.ID,
		"maze_index", runtime.MazeIndex,
		"room", snapshot.Run.Current.String(),
		"request_msg_id", uint16(dnfenum.CmdPacketSetPlayResult),
		"body_len", len(body),
		"dynamic_rows", dynamicRows,
		"optional_u32", optionalField,
		"body_hex", hex.EncodeToString(body),
		"reward_assets", "already_committed_or_send_blocked",
		"next_stage", "send_frozen_current_op34_op37_op35_plan")
	if err := s.produceCurrentDungeonSettlementPlanLocked(
		context.Background(), session, runtime,
	); err != nil {
		s.logGameEvent(session, "game-dungeon-settlement-plan-blocked",
			"char_id", session.selectedCharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"completion_key", runtime.clearMapCompletionKey,
			"source", "first_op46_capture",
			"error", err)
		return nil
	}
	return s.sendCurrentDungeonSettlementResultsLocked(
		session,
		runtime,
		"first_op46_capture",
	)
}

func currentDungeonSettlementCompletionReady(runtime *runtimeDungeonState, status worldmap.DungeonRunStatus) bool {
	if runtime == nil || !runtime.tutorialCompletionPersisted || status != worldmap.DungeonRunCompleted {
		return false
	}
	if runtime.tutorialFinalRoomClearAccepted {
		return true
	}
	if runtime.ordinaryFinalRoomClearAccepted {
		return runtime.bossDieCheckAccepted
	}
	return runtime.bossDieCheckAccepted && runtime.bossDieCheckResponseSent
}

func currentDungeonPlayResultShape(body []byte) (dynamicRows int, optionalField bool, valid bool) {
	for rows := 0; rows <= currentDungeonPlayResultMaximumDynamicRows; rows++ {
		if len(body) == currentDungeonPlayResultBaseSize+rows*currentDungeonPlayResultDynamicRowSize {
			return rows, false, true
		}
		if len(body) == currentDungeonPlayResultOptionalBaseSize+rows*currentDungeonPlayResultDynamicRowSize {
			return rows, true, true
		}
	}
	return 0, false, false
}
