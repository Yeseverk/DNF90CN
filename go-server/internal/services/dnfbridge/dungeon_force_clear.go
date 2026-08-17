package dnfbridge

import (
	"errors"
	"fmt"
	"strings"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

// currentDungeonOwnerSentinelAnnouncedOrdinaryMonster proves the exact case
// where the current EXE reports an ordinary hostile monster death with
// owner=0xffff. Live packet evidence shows that some ordinary room monsters use
// this sentinel even though neighboring monsters use the player object key.
// The sentinel is never sufficient by itself: the target must be the exact
// announced, still-live ordinary monster bound by the active room scene.
//
// This deliberately excludes extended APC/AI actors, passive objects, friendly
// team actors, stale object keys, already-defeated actors, and actors from
// another room. Tutorial owner-ffff deaths remain owned by the exact PVF+CMT
// proof in the caller. There are no dungeon, map, monster, or quest ID
// exceptions.
func currentDungeonOwnerSentinelAnnouncedOrdinaryMonster(
	runtime *runtimeDungeonState,
	scene worldmap.DungeonRoomScene,
	objectKey uint32,
) (runtimeDungeonMonster, bool) {
	if runtime == nil || runtime.Room == nil || runtime.Session == nil || objectKey == 0 || scene.Cleared {
		return runtimeDungeonMonster{}, false
	}
	// A malformed or explicitly disabled tutorial definition must not fall
	// through to this ordinary-dungeon compatibility path. Tutorial owner-ffff
	// deaths remain owned by the exact PVF metadata+CMT proof above the caller;
	// the namespace check keeps that boundary fail-closed when the metadata flag
	// itself is absent or zero.
	if isPVFTutorialDungeon(runtime) || currentDungeonPathUsesTutorialNamespace(runtime.Dungeon.Path) {
		return runtimeDungeonMonster{}, false
	}
	room := runtime.Room.Snapshot()
	if room.Coordinate != scene.Coordinate || room.MapID != scene.Map.Map.ID {
		return runtimeDungeonMonster{}, false
	}
	monster, announced := runtime.Room.AnnouncedMonster(objectKey)
	if !announced || monster.Reference.Kind != worldmap.HostileMonster {
		return runtimeDungeonMonster{}, false
	}
	reference, bound := scene.RuntimeObjects[objectKey]
	if !bound || reference != monster.Reference ||
		!runtimeSceneContainsHostile(scene.ExpectedHostiles, reference) ||
		dungeonSceneObjectDefeated(scene.DefeatedObjects, objectKey) {
		return runtimeDungeonMonster{}, false
	}
	return monster, true
}

// currentDungeonOwnerSentinelBlockingClearTarget is the narrower subset that
// may trigger the existing boss/explicit-clear-target combat-end behavior.
// Accepting another announced ordinary owner-sentinel death must not promote it
// to a boss or force-retire neighboring hostiles.
func currentDungeonOwnerSentinelBlockingClearTarget(
	runtime *runtimeDungeonState,
	scene worldmap.DungeonRoomScene,
	objectKey uint32,
) (runtimeDungeonMonster, string, bool) {
	monster, ok := currentDungeonOwnerSentinelAnnouncedOrdinaryMonster(runtime, scene, objectKey)
	if !ok || !runtimeSceneContainsHostile(scene.BlockingHostiles, monster.Reference) {
		return runtimeDungeonMonster{}, "", false
	}

	if normalizeDungeonPVFSymbol(monster.Spawn.SuffixMarker) == "boss" {
		return monster, "current_pvf_monster_suffix_boss", true
	}
	if normalizeDungeonPVFSymbol(monster.Spawn.Rank) == "boss" {
		return monster, "current_pvf_monster_spawn_rank_boss", true
	}
	if normalizeDungeonPVFSymbol(monster.Definition.Rank) == "boss" {
		return monster, "current_pvf_monster_definition_rank_boss", true
	}
	if source := currentDungeonClearConditionSource(runtime, monster); source != "" {
		return monster, source, true
	}
	return runtimeDungeonMonster{}, "", false
}

func currentDungeonPathUsesTutorialNamespace(value string) bool {
	normalized := "/" + strings.Trim(normalizeDungeonRuntimePath(value), "/") + "/"
	return strings.Contains(normalized, "/tutorial/") || strings.Contains(normalized, "/newtutorial/")
}

func currentDungeonRuntimeObjectBossActor(
	runtime *runtimeDungeonState,
	objectKey uint32,
	allowedStates ...runtimeDungeonMonsterState,
) (bool, byte, string) {
	if runtime == nil || runtime.Room == nil || objectKey == 0 {
		return false, 0, ""
	}
	allowed := make(map[runtimeDungeonMonsterState]struct{}, len(allowedStates))
	for _, state := range allowedStates {
		allowed[state] = struct{}{}
	}
	stateAllowed := func(state runtimeDungeonMonsterState) bool {
		if len(allowed) == 0 {
			return true
		}
		_, ok := allowed[state]
		return ok
	}

	room := runtime.Room.Snapshot()
	for _, monster := range room.Monsters {
		if monster.ObjectKey != objectKey || !stateAllowed(monster.State) {
			continue
		}
		if currentDungeonOrdinaryMonsterLooksBoss(monster) {
			return true, 3, "86jp_monster_pvf_boss_rank"
		}
		if currentDungeonOrdinaryMonsterMatchesClearCondition(runtime, monster) {
			return true, 3, "86jp_maze_clear_condition_target"
		}
		actorType, err := currentDungeonMonsterActorType(monster.Spawn)
		if err == nil && currentDungeonIsBossActorType(actorType) {
			return true, actorType, "86jp_monster_actor_type"
		}
		return false, actorType, "monster_actor_type_not_boss"
	}
	for _, actor := range room.ExtendedActors {
		if actor.ObjectKey != objectKey || actor.HostileReference == nil ||
			!stateAllowed(actor.State) {
			continue
		}
		if currentDungeonIsBossActorType(actor.Packet.Type) {
			return true, actor.Packet.Type, "86jp_extended_actor_type"
		}
		return false, actor.Packet.Type, "extended_actor_type_not_boss"
	}
	return false, 0, ""
}

func currentDungeonOrdinaryMonsterLooksBoss(monster runtimeDungeonMonster) bool {
	if normalizeDungeonPVFSymbol(monster.Spawn.Rank) == "boss" ||
		normalizeDungeonPVFSymbol(monster.Spawn.SuffixMarker) == "boss" ||
		normalizeDungeonPVFSymbol(monster.Definition.Rank) == "boss" {
		return true
	}
	actorType, err := currentDungeonMonsterActorType(monster.Spawn)
	return err == nil && currentDungeonIsBossActorType(actorType)
}

func currentDungeonOrdinaryMonsterMatchesClearCondition(
	runtime *runtimeDungeonState,
	monster runtimeDungeonMonster,
) bool {
	return currentDungeonClearConditionSource(runtime, monster) != ""
}

func currentDungeonClearConditionSource(runtime *runtimeDungeonState, monster runtimeDungeonMonster) string {
	if runtime == nil || monster.Spawn.MonsterID <= 0 ||
		runtime.MazeIndex < 0 || runtime.MazeIndex >= len(runtime.Dungeon.Mazes) {
		return ""
	}
	for _, condition := range runtime.Dungeon.Mazes[runtime.MazeIndex].ClearConditions {
		if condition.TargetID != monster.Spawn.MonsterID {
			continue
		}
		kind := normalizeDungeonPVFSymbol(condition.Type)
		switch kind {
		case "hunt boss":
			if currentDungeonOrdinaryMonsterLooksBoss(monster) {
				return fmt.Sprintf("86jp_clear_condition_%s_target_%d", kind, condition.TargetID)
			}
		case "hunt monster", "destroy object":
			return fmt.Sprintf("86jp_clear_condition_%s_target_%d", kind, condition.TargetID)
		}
	}
	return ""
}

type currentDungeonForcedCombatEnd struct {
	Targets []runtimeDungeonDeathTarget
	Scene   worldmap.DungeonRoomScene
	Cleared bool
	SceneOK bool
}

// prepareCurrentDungeonForcedCombatEndLocked commits every remaining hostile
// before any follow-up map transition can be accepted. Its visual op38 packets
// are deliberately deferred so a staged-story boss response can carry the
// next-room coordinate first.
func (s *Service) prepareCurrentDungeonForcedCombatEndLocked(
	runtime *runtimeDungeonState,
	sourceObjectKey uint32,
) (currentDungeonForcedCombatEnd, error) {
	if runtime == nil || runtime.Room == nil || runtime.Session == nil {
		return currentDungeonForcedCombatEnd{}, nil
	}
	targets, forcedCleared, err := runtime.Room.CommitRemainingAnnouncedHostilesAfterBoss(
		sourceObjectKey,
		runtime.Session,
	)
	if err != nil {
		return currentDungeonForcedCombatEnd{Targets: targets}, err
	}
	scene, sceneOK := runtime.Session.Scene()
	roomCleared := forcedCleared
	if sceneOK {
		roomCleared = roomCleared || scene.Cleared
	}
	return currentDungeonForcedCombatEnd{
		Targets: targets,
		Scene:   scene,
		Cleared: roomCleared,
		SceneOK: sceneOK,
	}, nil
}

func (s *Service) flushCurrentDungeonForcedCombatEndLocked(
	session *gameSession,
	runtime *runtimeDungeonState,
	sourceObjectKey uint32,
	source string,
	prepared currentDungeonForcedCombatEnd,
) (int, bool, error) {
	forcedVisualDeathCount := 0
	for _, forcedTarget := range prepared.Targets {
		forcedBody, buildErr := buildCurrentDungeonDeathNotificationBodyWithDrops(
			forcedTarget.ObjectKey,
			forcedTarget.ResponseKind,
			nil,
		)
		if buildErr != nil {
			return forcedVisualDeathCount, prepared.Cleared, buildErr
		}
		if sendErr := s.sendGameUpperRawClass(
			session,
			uint16(dnfenum.CmdPacketNotifyDieMonster),
			forcedBody,
			0,
		); sendErr != nil {
			return forcedVisualDeathCount, prepared.Cleared, sendErr
		}
		forcedVisualDeathCount++
	}

	if len(prepared.Targets) > 0 || prepared.Cleared {
		if !prepared.SceneOK {
			s.logGameEvent(session, "game-dungeon-combat-end-forced-room-clear",
				"dungeon_id", runtime.Dungeon.ID,
				"maze_index", runtime.MazeIndex,
				"source_object_key", sourceObjectKey,
				"source", source,
				"forced_defeated_count", len(prepared.Targets),
				"forced_visual_death_count", forcedVisualDeathCount,
				"room_cleared", prepared.Cleared,
				"flow_source", "86jp_boss_or_clear_decision_kills_remaining_hostiles")
			return forcedVisualDeathCount, prepared.Cleared, nil
		}
		scene := prepared.Scene
		s.logGameEvent(session, "game-dungeon-combat-end-forced-room-clear",
			"dungeon_id", runtime.Dungeon.ID,
			"maze_index", runtime.MazeIndex,
			"room", scene.Coordinate.String(),
			"map_id", scene.Map.Map.ID,
			"source_object_key", sourceObjectKey,
			"source", source,
			"forced_defeated_count", len(prepared.Targets),
			"forced_visual_death_count", forcedVisualDeathCount,
			"defeated_actor_count", len(scene.DefeatedObjects),
			"tracked_hostile_count", len(scene.ExpectedHostiles),
			"blocking_hostile_count", len(scene.BlockingHostiles),
			"room_cleared", prepared.Cleared,
			"flow_source", "86jp_boss_or_clear_decision_kills_remaining_hostiles")
		// Live evidence (2026-07-17 quest 3146 run) proves the client-side
		// quest-scene object dies with this forced room clear while its later
		// owner-0xffff op39 reports can no longer be matched, so the guarded
		// type-3 hunt-enemy credit is owned here. A credit failure must never
		// break the combat-end or settlement flow.
		if _, creditErr := s.creditCurrentDungeonQuestObjectRoomClearLocked(
			session,
			runtime,
			scene,
			sourceObjectKey,
			"boss_forced_room_clear_quest_connected_maze_unique_active_pvf_type3_target",
		); creditErr != nil {
			s.logGameEvent(session, "game-dungeon-quest-object-room-clear-deferred",
				"dungeon_id", runtime.Dungeon.ID,
				"maze_index", runtime.MazeIndex,
				"room", scene.Coordinate.String(),
				"map_id", scene.Map.Map.ID,
				"source_object_key", sourceObjectKey,
				"source", source,
				"reason", "boss_forced_room_clear_quest_object_credit_failed",
				"error", creditErr)
		}
	}
	return forcedVisualDeathCount, prepared.Cleared, nil
}

// forceCurrentDungeonRemainingHostilesForCombatEndLocked preserves the
// immediate visual-death behavior for ordinary boss flows. Staged story maps
// call prepare/flush separately around their source boss op38.
func (s *Service) forceCurrentDungeonRemainingHostilesForCombatEndLocked(
	session *gameSession,
	runtime *runtimeDungeonState,
	sourceObjectKey uint32,
	source string,
) (int, int, bool, error) {
	if session == nil || runtime == nil || runtime.Room == nil || runtime.Session == nil {
		return 0, 0, false, nil
	}
	prepared, err := s.prepareCurrentDungeonForcedCombatEndLocked(runtime, sourceObjectKey)
	if err != nil {
		return len(prepared.Targets), 0, false, err
	}
	forcedVisualDeathCount, roomCleared, err := s.flushCurrentDungeonForcedCombatEndLocked(
		session,
		runtime,
		sourceObjectKey,
		source,
		prepared,
	)
	return len(prepared.Targets), forcedVisualDeathCount, roomCleared, err
}

func isCurrentDungeonForceClearAlreadyDefeated(err error) bool {
	return errors.Is(err, errDungeonMonsterAlreadyDefeated) ||
		errors.Is(err, errDungeonActorNotHostile)
}
