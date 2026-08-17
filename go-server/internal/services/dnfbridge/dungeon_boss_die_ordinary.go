package dnfbridge

import (
	"longheng.io/server/internal/modules/dnf/dungeoncmd"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

// completeCurrentDungeonOrdinaryFinalRoomAfterDeathLocked mirrors the domain
// path used by the reference 86JP flow: after the final room's real blocking
// actors are gone, ClearDungeon is reached from prepare_dungeon_clear even
// when the client does not send a separate boss-die-check request. It does not
// fabricate a monster death; the caller has already accepted the authoritative
// op39 and the current scene is cleared.
func (s *Service) completeCurrentDungeonOrdinaryFinalRoomAfterDeathLocked(
	session *gameSession,
	runtime *runtimeDungeonState,
	scene worldmap.DungeonRoomScene,
	targetObjectKey uint32,
) error {
	if session == nil || runtime == nil || runtime.Session == nil || session.dungeon.runtime != runtime ||
		runtime.ordinaryFinalRoomClearAccepted || runtime.bossDieCheckAccepted ||
		runtime.townReturnPending || runtime.townReturnOp24Sent {
		return nil
	}
	if isPVFTutorialDungeonScene(runtime, scene) ||
		!dungeonRuntimeOwnsCharacter(runtime, session.selectedCharacterID) {
		return nil
	}
	pendingLayer, nextLayerIndex, nextLayerMapID, layerErr := s.currentDungeonPendingLayer(runtime, scene)
	if layerErr != nil {
		s.logGameEvent(session, "game-dungeon-ordinary-final-clear-deferred",
			"char_id", session.selectedCharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"maze_index", runtime.MazeIndex,
			"room", scene.Coordinate.String(),
			"map_id", scene.Map.Map.ID,
			"target_object_key", targetObjectKey,
			"reason", "pending_layer_resolution_failed",
			"error", layerErr)
		return nil
	}
	if pendingLayer {
		s.logGameEvent(session, "game-dungeon-ordinary-final-clear-deferred",
			"char_id", session.selectedCharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"maze_index", runtime.MazeIndex,
			"room", scene.Coordinate.String(),
			"map_id", scene.Map.Map.ID,
			"target_object_key", targetObjectKey,
			"next_layer_index", nextLayerIndex,
			"next_layer_map_id", nextLayerMapID,
			"reason", "explicit_pvf_layer_must_be_consumed_before_settlement")
		return nil
	}
	finalReady, finalSource := currentDungeonOrdinaryFinalRoomReady(runtime, scene)
	if !finalReady {
		return nil
	}
	if err := runtime.Session.Complete(); err != nil {
		return err
	}
	runtime.ordinaryFinalRoomClearAccepted = true
	runtime.bossDieCheckAccepted = true
	runtime.bossDieCheckRelatedActorObjectKey = currentSceneActorObjectKey(session.selectedCharacterID)
	runtime.bossDieCheckTargetObjectKey = uint16(targetObjectKey)
	runtime.bossDieCheckPending = false
	runtime.bossDieCheckPendingRequest = dungeoncmd.BossDieCheckRequest{}
	completed := runtime.Session.Snapshot()
	s.logGameEvent(session, "game-dungeon-ordinary-final-clear-committed",
		"char_id", session.selectedCharacterID,
		"dungeon_id", runtime.Dungeon.ID,
		"maze_index", runtime.MazeIndex,
		"room", completed.Run.Current.String(),
		"map_id", completed.Scene.Map.Map.ID,
		"target_object_key", targetObjectKey,
		"run_status", completed.Run.Status,
		"defeated_actor_count", len(completed.Scene.DefeatedObjects),
		"completion_source", finalSource,
		"op117_required", false,
		"op115_sent", false,
		"next_stage", "persist_completion_then_bodyless_settlement_op31")
	return s.completeCurrentDungeonBossDieCheckLocked(
		session,
		runtime,
		"ordinary_final_room_authoritative_op39_without_op117",
	)
}

func currentDungeonOrdinaryFinalRoomReady(
	runtime *runtimeDungeonState,
	scene worldmap.DungeonRoomScene,
) (bool, string) {
	if runtime == nil || runtime.Session == nil || !scene.Cleared {
		return false, ""
	}
	if !currentDungeonStoryFinalStageMatches(runtime, scene) {
		return false, ""
	}
	if gate, active := currentDungeonStoryAIBossDeathGate(runtime, scene); active && !gate.Ready {
		return false, ""
	}
	if scene.Boss {
		if runtime.BossSet && scene.Coordinate == runtime.BossCoordinate {
			if _, active := currentDungeonStoryAIBossDeathGate(runtime, scene); active {
				return true, "current_exe_story_ai_boss_and_dummy_boss_dual_authoritative_op39"
			}
			return true, "86jp_prepare_dungeon_clear_boss_room_after_authoritative_op39"
		}
	}
	return false, ""
}

func currentDungeonClearedRoomHasUnvisitedExit(
	session *worldmap.DungeonSession,
	coordinate worldmap.RoomCoordinate,
) bool {
	if session == nil {
		return false
	}
	directions := []struct {
		dx int64
		dy int64
	}{
		{dx: -1}, {dx: 1}, {dy: -1}, {dy: 1},
	}
	for _, direction := range directions {
		x := coordinate.X + direction.dx
		y := coordinate.Y + direction.dy
		if x < 0 || x > 0xff || y < 0 || y > 0xff {
			continue
		}
		transition, err := session.PreviewMoveByteTransition(byte(x), byte(y))
		if err == nil && !transition.Revisit {
			return true
		}
	}
	return false
}
