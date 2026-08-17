package dnfbridge

import (
	"fmt"

	"longheng.io/server/internal/modules/dnf/worldmap"
)

func (s *Service) configurePVFTutorialBasicActionRoom(
	dungeon worldmap.Dungeon,
	scene worldmap.DungeonRoomScene,
	room *runtimeDungeonRoom,
) ([]dungeonTutorialBasicActionEvidence, error) {
	if room == nil || !dungeon.Metadata.TutorialDungeon.Set || dungeon.Metadata.TutorialDungeon.Value != 1 {
		return nil, nil
	}
	catalog, ready := s.dungeonTutorialScriptCatalog()
	if !ready {
		return nil, nil
	}
	targets := catalog.BasicActionMonsterDestroyTargets(scene.Map.Map.ID)
	for _, target := range targets {
		if target.MonsterIndex < 0 || target.MonsterIndex >= len(scene.Monsters) ||
			scene.Monsters[target.MonsterIndex].MonsterID != target.MonsterID {
			return nil, fmt.Errorf("tutorial basic-action target mismatch: map=%d monster_index=%d monster_id=%d",
				scene.Map.Map.ID, target.MonsterIndex, target.MonsterID)
		}
		monster, err := room.MarkMonsterNonBlocking(target.MonsterIndex)
		if err != nil {
			return nil, err
		}
		if monster.Reference.Kind != worldmap.HostileMonster || monster.Reference.Index != target.MonsterIndex ||
			monster.Spawn.MonsterID != target.MonsterID {
			return nil, fmt.Errorf("tutorial basic-action runtime target mismatch: map=%d monster_index=%d monster_id=%d",
				scene.Map.Map.ID, target.MonsterIndex, target.MonsterID)
		}
	}
	return targets, nil
}

func (s *Service) applyPVFTutorialBasicActionRoomToSession(
	session *gameSession,
	runtime *runtimeDungeonState,
	scene worldmap.DungeonRoomScene,
	source string,
) (int, error) {
	if runtime == nil || runtime.Session == nil || runtime.Room == nil {
		return 0, nil
	}
	references := runtime.Room.NonBlockingMonsterReferences()
	applied := 0
	for _, reference := range references {
		removed, err := runtime.Session.MarkHostileNonBlocking(reference)
		if err != nil {
			return applied, err
		}
		if removed {
			applied++
		}
	}
	if applied != 0 {
		current, _ := runtime.Session.Scene()
		s.logGameEvent(session, "game-dungeon-tutorial-basic-action-nonblocking-applied",
			"dungeon_id", runtime.Dungeon.ID,
			"maze_index", runtime.MazeIndex,
			"room", scene.Coordinate.String(),
			"map_id", scene.Map.Map.ID,
			"scripted_target_count", applied,
			"blocking_hostile_count", len(current.BlockingHostiles),
			"room_cleared", current.Cleared,
			"source", source)
	}
	return applied, nil
}
