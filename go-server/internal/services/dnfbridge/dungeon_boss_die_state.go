package dnfbridge

import (
	"encoding/binary"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	"longheng.io/server/internal/modules/dnf/dungeoncmd"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

const (
	currentDungeonBossDieCheckResponseSize = 4
	currentDungeonTutorialExitMsgID        = uint16(dnfenum.CmdPacketSetQuestTrigger)
	currentDungeonTutorialExitReason       = byte(100)
)

func (s *Service) handleDungeonBossDieCheck(session *gameSession, body []byte) error {
	if session == nil {
		return nil
	}
	if len(body) != dungeoncmd.BossDieCheckRequestSize {
		s.logGameEvent(session, "game-dungeon-boss-die-check-blocked",
			"body_len", len(body),
			"expected_body_len", dungeoncmd.BossDieCheckRequestSize,
			"reason", "current_exe_op117_plaintext_boundary_mismatch")
		return nil
	}
	request, err := dungeoncmd.DecodeBossDieCheckRequest(body)
	if err != nil {
		s.logGameEvent(session, "game-dungeon-boss-die-check-blocked",
			"body_len", len(body),
			"reason", "current_exe_op117_request_malformed",
			"error", err)
		return nil
	}
	if request.ReservedZero != 0 || request.Field12 != 0 {
		s.logGameEvent(session, "game-dungeon-boss-die-check-blocked",
			"body_len", len(body),
			"related_actor_object_key", request.RelatedActorObjectKey,
			"target_object_key", request.TargetObjectKey,
			"reserved_zero", request.ReservedZero,
			"field_12", request.Field12,
			"reason", "current_exe_op117_writer_invariant_mismatch")
		return nil
	}
	s.logGameEvent(session, "game-dungeon-boss-die-check-request-decoded",
		"body_len", len(body),
		"related_actor_object_key", request.RelatedActorObjectKey,
		"target_object_key", request.TargetObjectKey,
		"reserved_zero", request.ReservedZero,
		"field_08", request.Field08,
		"field_12", request.Field12,
		"field_13", request.Field13,
		"field_17", request.Field17,
		"field_25", request.Field25,
		"body_source", "current_exe_sub_24351D0_exact_39_byte_plaintext")
	if session.selectedCharacterID == 0 {
		s.logGameEvent(session, "game-dungeon-boss-die-check-blocked",
			"body_len", len(body),
			"reason", "selected_character_missing")
		return nil
	}

	session.dungeon.mu.Lock()
	defer session.dungeon.mu.Unlock()
	runtime := session.dungeon.runtime
	return s.handleDungeonBossDieCheckLocked(session, runtime, request, false)
}

// handleDungeonBossDieCheckLocked owns the final-room request state machine.
// A validated request may arrive immediately before the same target's op39;
// retain only that exact request and finish it after the authoritative death
// report instead of fabricating a death or sending an unsolicited completion.
func (s *Service) handleDungeonBossDieCheckLocked(
	session *gameSession,
	runtime *runtimeDungeonState,
	request dungeoncmd.BossDieCheckRequest,
	afterTargetDeath bool,
) error {
	if runtime == nil || runtime.Session == nil || runtime.Room == nil {
		s.logGameEvent(session, "game-dungeon-boss-die-check-blocked",
			"related_actor_object_key", request.RelatedActorObjectKey,
			"target_object_key", request.TargetObjectKey,
			"reason", "active_dungeon_runtime_missing")
		return nil
	}
	if !dungeonRuntimeOwnsCharacter(runtime, session.selectedCharacterID) {
		s.logGameEvent(session, "game-dungeon-boss-die-check-blocked",
			"char_id", session.selectedCharacterID,
			"runtime_char_id", runtime.Character.CharacterID,
			"related_actor_object_key", request.RelatedActorObjectKey,
			"target_object_key", request.TargetObjectKey,
			"reason", "active_dungeon_runtime_character_owner_mismatch")
		return nil
	}
	if runtime.bossDieCheckAccepted {
		acceptedRequest := runtime.bossDieCheckRelatedActorObjectKey == request.RelatedActorObjectKey &&
			runtime.bossDieCheckTargetObjectKey == request.TargetObjectKey
		acceptedSnapshot := runtime.Session.Snapshot()
		if acceptedRequest && acceptedSnapshot.Run.Status == worldmap.DungeonRunCompleted {
			s.logGameEvent(session, "game-dungeon-boss-die-check-completion-replay",
				"dungeon_id", runtime.Dungeon.ID,
				"related_actor_object_key", request.RelatedActorObjectKey,
				"target_object_key", request.TargetObjectKey,
				"completion_persisted", runtime.tutorialCompletionPersisted,
				"op115_sent", runtime.bossDieCheckResponseSent,
				"settlement_entry_sent", runtime.settlementEntrySent,
				"reason", "retry_persist_op115_or_bodyless_op31_at_exact_failed_stage")
			return s.completeCurrentDungeonBossDieCheckLocked(session, runtime, "op117_exact_replay")
		}
		s.logGameEvent(session, "game-dungeon-boss-die-check-blocked",
			"dungeon_id", runtime.Dungeon.ID,
			"related_actor_object_key", request.RelatedActorObjectKey,
			"target_object_key", request.TargetObjectKey,
			"accepted_related_actor_object_key", runtime.bossDieCheckRelatedActorObjectKey,
			"accepted_target_object_key", runtime.bossDieCheckTargetObjectKey,
			"run_status", acceptedSnapshot.Run.Status,
			"reason", "conflicting_or_invalid_boss_die_check_replay")
		return nil
	}
	if runtime.bossDieCheckPending {
		if runtime.bossDieCheckPendingRequest != request {
			s.logGameEvent(session, "game-dungeon-boss-die-check-blocked",
				"dungeon_id", runtime.Dungeon.ID,
				"related_actor_object_key", request.RelatedActorObjectKey,
				"target_object_key", request.TargetObjectKey,
				"pending_related_actor_object_key", runtime.bossDieCheckPendingRequest.RelatedActorObjectKey,
				"pending_target_object_key", runtime.bossDieCheckPendingRequest.TargetObjectKey,
				"reason", "conflicting_pending_boss_die_check")
			return nil
		}
		if !afterTargetDeath {
			pendingSnapshot := runtime.Session.Snapshot()
			_, _, targetDefeated := currentDungeonDefeatedActor(
				runtime,
				pendingSnapshot.Scene,
				uint32(request.TargetObjectKey),
			)
			if !targetDefeated {
				s.logGameEvent(session, "game-dungeon-boss-die-check-pending-replay",
					"dungeon_id", runtime.Dungeon.ID,
					"related_actor_object_key", request.RelatedActorObjectKey,
					"target_object_key", request.TargetObjectKey,
					"reason", "waiting_for_same_target_authoritative_op39")
				return nil
			}
		}
	}

	snapshot := runtime.Session.Snapshot()
	scene := snapshot.Scene
	if snapshot.Run.Status != worldmap.DungeonRunActive {
		s.logGameEvent(session, "game-dungeon-boss-die-check-blocked",
			"dungeon_id", runtime.Dungeon.ID,
			"run_status", snapshot.Run.Status,
			"reason", "dungeon_run_not_active")
		return nil
	}
	if !currentDungeonRoomOwnsScene(runtime, scene) {
		s.logGameEvent(session, "game-dungeon-boss-die-check-blocked",
			"dungeon_id", runtime.Dungeon.ID,
			"room", scene.Coordinate.String(),
			"map_id", scene.Map.Map.ID,
			"reason", "current_room_not_owned_by_active_runtime")
		return nil
	}
	tutorialScene := isPVFTutorialDungeonScene(runtime, scene)
	if !scene.Boss || !runtime.BossSet || scene.Coordinate != runtime.BossCoordinate ||
		!currentDungeonStoryFinalStageMatches(runtime, scene) {
		s.logGameEvent(session, "game-dungeon-boss-die-check-blocked",
			"dungeon_id", runtime.Dungeon.ID,
			"room", scene.Coordinate.String(),
			"map_id", scene.Map.Map.ID,
			"scene_boss", scene.Boss,
			"runtime_boss_set", runtime.BossSet,
			"runtime_boss_room", runtime.BossCoordinate.String(),
			"story_stage_index", runtime.StoryStageIndex,
			"story_stage_count", len(runtime.StoryStages),
			"reason", "current_room_or_story_map_not_authoritative_runtime_boss")
		return nil
	}

	playerObjectKey := currentSceneActorObjectKey(session.selectedCharacterID)
	if request.RelatedActorObjectKey != playerObjectKey &&
		!dungeonRuntimeContainsDeathOwnerObjectKey(runtime, uint32(request.RelatedActorObjectKey)) {
		s.logGameEvent(session, "game-dungeon-boss-die-check-blocked",
			"dungeon_id", runtime.Dungeon.ID,
			"related_actor_object_key", request.RelatedActorObjectKey,
			"player_object_key", playerObjectKey,
			"target_object_key", request.TargetObjectKey,
			"reason", "related_actor_not_owned_by_dungeon_runtime")
		return nil
	}

	targetObjectKey := uint32(request.TargetObjectKey)
	targetReference, targetIsOrdinaryMonster, ok := currentDungeonDefeatedActor(runtime, scene, targetObjectKey)
	if !ok {
		announcedReference, announcedOrdinaryMonster, announced := currentDungeonAnnouncedActor(runtime, scene, targetObjectKey)
		// The current EXE's ordinary-dungeon op117 must close only a target
		// already committed by the authoritative op39 death path. The narrow
		// op117-before-op39 retention exists solely for PVF tutorial CMT order.
		if !tutorialScene || !announced || afterTargetDeath ||
			!currentDungeonBossDieCheckCanWaitForTarget(scene, targetObjectKey, announcedReference) {
			s.logGameEvent(session, "game-dungeon-boss-die-check-blocked",
				"dungeon_id", runtime.Dungeon.ID,
				"room", scene.Coordinate.String(),
				"map_id", scene.Map.Map.ID,
				"target_object_key", request.TargetObjectKey,
				"target_announced", announced,
				"after_target_death", afterTargetDeath,
				"room_cleared", scene.Cleared,
				"tutorial_scene", tutorialScene,
				"reason", "target_actor_not_authoritatively_defeated")
			return nil
		}
		runtime.bossDieCheckPending = true
		runtime.bossDieCheckPendingRequest = request
		s.logGameEvent(session, "game-dungeon-boss-die-check-retained",
			"dungeon_id", runtime.Dungeon.ID,
			"maze_index", runtime.MazeIndex,
			"room", scene.Coordinate.String(),
			"map_id", scene.Map.Map.ID,
			"related_actor_object_key", request.RelatedActorObjectKey,
			"target_object_key", request.TargetObjectKey,
			"target_kind", announcedReference.Kind,
			"target_is_ordinary_monster", announcedOrdinaryMonster,
			"room_cleared", scene.Cleared,
			"reason", "valid_final_request_arrived_before_same_target_op39")
		return nil
	}
	return s.completeValidatedDungeonBossDieCheckLocked(
		session,
		runtime,
		request,
		scene,
		tutorialScene,
		targetReference,
		targetIsOrdinaryMonster,
		targetObjectKey,
	)
}

func buildCurrentDungeonBossDieCheckResponse(targetObjectKey uint16) []byte {
	body := make([]byte, currentDungeonBossDieCheckResponseSize)
	body[0] = 1
	body[1] = 1
	binary.LittleEndian.PutUint16(body[2:4], targetObjectKey)
	return body
}
