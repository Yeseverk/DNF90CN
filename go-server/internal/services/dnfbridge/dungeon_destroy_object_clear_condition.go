package dnfbridge

import (
	"fmt"

	"longheng.io/server/internal/modules/dnf/worldmap"
)

// currentDungeonDestroyObjectClearConditionSource binds a client-owned
// passive object to the active maze's explicit one-object clear condition.
// These PVF object codes can exceed op38's u16 target-key range, so they are
// never installed in the ordinary actor table and must be proven from the
// current map and maze instead.
func currentDungeonDestroyObjectClearConditionSource(
	runtime *runtimeDungeonState,
	scene worldmap.DungeonRoomScene,
	objectKey uint32,
) string {
	if runtime == nil || runtime.Room == nil || runtime.Session == nil ||
		objectKey <= uint32(^uint16(0)) || !scene.Cleared || !scene.Boss ||
		!runtime.BossSet || scene.Coordinate != runtime.BossCoordinate ||
		runtime.MazeIndex < 0 || runtime.MazeIndex >= len(runtime.Dungeon.Mazes) {
		return ""
	}
	room := runtime.Room.Snapshot()
	if room.Coordinate != scene.Coordinate || room.MapID != scene.Map.Map.ID {
		return ""
	}

	placementCount := int64(0)
	for _, object := range scene.PassiveObjects {
		if object.ObjectID == int64(objectKey) {
			placementCount++
		}
	}
	if placementCount != 1 {
		return ""
	}

	for _, condition := range runtime.Dungeon.Mazes[runtime.MazeIndex].ClearConditions {
		if normalizeDungeonPVFSymbol(condition.Type) == "destroy object" &&
			condition.TargetID == int64(objectKey) && condition.Count == placementCount {
			return fmt.Sprintf(
				"current_pvf_clear_condition_destroy_object_target_%d_count_%d",
				condition.TargetID,
				condition.Count,
			)
		}
	}
	return ""
}

// handleCurrentDungeonDestroyObjectClearConditionDeathLocked accepts the
// variable op39 emitted after the current client has already removed a PVF
// passive object locally. No op38 is sent because its target field is u16.
// The caller owns session.dungeon.mu.
func (s *Service) handleCurrentDungeonDestroyObjectClearConditionDeathLocked(
	session *gameSession,
	runtime *runtimeDungeonState,
	scene worldmap.DungeonRoomScene,
	objectKey uint32,
) (bool, error) {
	source := currentDungeonDestroyObjectClearConditionSource(runtime, scene, objectKey)
	if source == "" {
		return false, nil
	}
	s.logGameEvent(session, "game-dungeon-destroy-object-clear-condition-accepted",
		"char_id", session.selectedCharacterID,
		"dungeon_id", runtime.Dungeon.ID,
		"maze_index", runtime.MazeIndex,
		"room", scene.Coordinate.String(),
		"map_id", scene.Map.Map.ID,
		"object_key", objectKey,
		"death_ack_sent", false,
		"reason", "current_exe_op38_target_is_u16_and_client_owned_passive_object_is_already_destroyed",
		"source", source)
	if err := s.completeCurrentDungeonOrdinaryFinalRoomAfterDeathLocked(
		session,
		runtime,
		scene,
		objectKey,
	); err != nil {
		return true, err
	}
	return true, nil
}
