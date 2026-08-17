package dnfbridge

import (
	"context"
	"fmt"
	"strconv"

	dnfquest "longheng.io/server/internal/modules/dnf/quest"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

// currentDungeonQuestObjectRoomClearTarget resolves the guarded room-clear
// credit for a client-side quest-scene object. Live evidence (2026-07-17,
// quest 3146 run, conn game-000010) proves the current EXE destroys the quest
// object inside the boss room and reports it with a variable op39 owner
// 0xffff, but the boss-death forced room clear retires every remaining
// announced hostile server-side before those reports can be matched, so the
// destruction can only be credited from the room clear itself.
//
// The real map 76136 PVF does not statically place passive object 13099 (the
// questscene/dummy_quest.obj is spawned client-side for the quest scene), so
// the binding is anchored on the maze quest connection instead: the current
// maze must be the PVF quest-connected maze selected because the parent main
// quest is active. The remaining guards mirror
// currentDungeonQuestPassiveObjectTarget: exactly one active type-3 hunt
// target in dungeon scope, a no-reward [sub] quest, and an active parent.
//
// The returned reason is empty only when the single guarded target matches.
func (s *Service) currentDungeonQuestObjectRoomClearTarget(
	ctx context.Context,
	session *gameSession,
	runtime *runtimeDungeonState,
	scene worldmap.DungeonRoomScene,
) (dnfquest.ActiveHuntEnemyTarget, string, error) {
	if session == nil || runtime == nil || runtime.Room == nil || session.dungeon.runtime != runtime ||
		!dungeonRuntimeOwnsCharacter(runtime, session.selectedCharacterID) {
		return dnfquest.ActiveHuntEnemyTarget{}, "runtime_not_owned_by_session", nil
	}
	if !scene.Boss || !runtime.BossSet || scene.Coordinate != runtime.BossCoordinate {
		return dnfquest.ActiveHuntEnemyTarget{}, "current_room_not_authoritative_runtime_boss", nil
	}
	if runtime.MazeIndex < 0 || runtime.MazeIndex >= len(runtime.Dungeon.Mazes) {
		return dnfquest.ActiveHuntEnemyTarget{}, "maze_index_out_of_range", nil
	}
	connection := runtime.Dungeon.Mazes[runtime.MazeIndex].QuestConnection
	if len(connection) < 2 || connection[0] != 0 {
		return dnfquest.ActiveHuntEnemyTarget{}, "maze_not_quest_connected", nil
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Quest == nil {
		return dnfquest.ActiveHuntEnemyTarget{}, "", errDungeonHuntEnemyCompletionUnavailable
	}
	characterID := strconv.FormatUint(uint64(session.selectedCharacterID), 10)
	record, found, err := repositories.Quest.Load(ctx, characterID)
	if err != nil {
		return dnfquest.ActiveHuntEnemyTarget{}, "", err
	}
	if !found {
		return dnfquest.ActiveHuntEnemyTarget{}, "quest_record_not_found", nil
	}
	catalog, err := s.loadQuestCatalog(ctx)
	if err != nil {
		return dnfquest.ActiveHuntEnemyTarget{}, "", err
	}
	targets := catalog.ActiveHuntEnemyTargets(
		record,
		runtime.Dungeon.ID,
		int64(runtime.Request.Difficulty),
		currentDungeonQuestEnemyTypePassiveObject,
	)
	if len(targets) != 1 {
		return dnfquest.ActiveHuntEnemyTarget{}, "active_type3_target_count_not_unique", nil
	}
	target := targets[0]
	definition, known := catalog.Find(target.QuestID)
	if !known || normalizeDungeonPVFSymbol(definition.Grade) != "sub" || definition.MainQuestID <= 0 ||
		definition.HasGoldReward || len(definition.RewardItems) != 0 || len(definition.RewardSelectionItems) != 0 {
		return dnfquest.ActiveHuntEnemyTarget{}, "target_not_no_reward_sub_quest", nil
	}
	parent, parentKnown := currentDungeonHuntEnemyQuestState(record, definition.MainQuestID)
	if !parentKnown || parent.Status != "active" || parent.ProgressValue <= 0 {
		return dnfquest.ActiveHuntEnemyTarget{}, "parent_main_quest_not_active", nil
	}
	if connection[1] != definition.MainQuestID {
		return dnfquest.ActiveHuntEnemyTarget{}, "maze_quest_connection_not_parent", nil
	}
	return target, "", nil
}

// creditCurrentDungeonQuestObjectRoomClearLocked persists the guarded
// room-clear hunt-enemy completion through the established Phase A owner. The
// forced room clear already sent zero-drop op38 visual deaths for every
// remaining announced hostile and the client already destroyed the quest
// object locally, so only the active-quest snapshot goes back on the wire.
// The caller owns session.dungeon.mu.
func (s *Service) creditCurrentDungeonQuestObjectRoomClearLocked(
	session *gameSession,
	runtime *runtimeDungeonState,
	scene worldmap.DungeonRoomScene,
	sourceObjectKey uint32,
	source string,
) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	target, blockedReason, err := s.currentDungeonQuestObjectRoomClearTarget(ctx, session, runtime, scene)
	if err != nil {
		return false, err
	}
	if blockedReason != "" {
		s.logGameEvent(session, "game-dungeon-quest-object-room-clear-blocked",
			"char_id", session.selectedCharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"maze_index", runtime.MazeIndex,
			"room", scene.Coordinate.String(),
			"map_id", scene.Map.Map.ID,
			"source_object_key", sourceObjectKey,
			"source", source,
			"reason", blockedReason)
		return false, nil
	}
	if err := s.persistCurrentDungeonHuntEnemyTargetPhaseA(
		ctx,
		session,
		runtime,
		scene,
		target.EnemyCode,
		target.EnemyType,
		sourceObjectKey,
		source,
	); err != nil {
		return false, fmt.Errorf("persist room-clear hunt target: %w", err)
	}
	s.logGameEvent(session, "game-dungeon-quest-object-room-clear-credited",
		"char_id", session.selectedCharacterID,
		"dungeon_id", runtime.Dungeon.ID,
		"maze_index", runtime.MazeIndex,
		"room", scene.Coordinate.String(),
		"map_id", scene.Map.Map.ID,
		"source_object_key", sourceObjectKey,
		"quest_id", target.QuestID,
		"enemy_code", target.EnemyCode,
		"enemy_type", target.EnemyType,
		"source", source)
	return true, nil
}
