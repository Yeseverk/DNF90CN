package dnfbridge

import (
	"longheng.io/server/internal/modules/dnf/dungeoncmd"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

// completeValidatedDungeonBossDieCheckLocked owns the clear decision after the
// request target has been proven defeated by the authoritative room runtime.
func (s *Service) completeValidatedDungeonBossDieCheckLocked(
	session *gameSession,
	runtime *runtimeDungeonState,
	request dungeoncmd.BossDieCheckRequest,
	scene worldmap.DungeonRoomScene,
	tutorialScene bool,
	targetReference worldmap.HostileReference,
	targetIsOrdinaryMonster bool,
	targetObjectKey uint32,
) error {
	completionSource := "cleared_ordinary_runtime_boss_room"
	if tutorialScene {
		completionSource = "cleared_tutorial_boss_room"
	}
	storyAIBossGate, storyAIBossGateActive := currentDungeonStoryAIBossDeathGate(runtime, scene)
	if !tutorialScene && storyAIBossGateActive && !storyAIBossGate.Ready {
		s.logGameEvent(session, "game-dungeon-boss-die-check-blocked",
			"dungeon_id", runtime.Dungeon.ID,
			"maze_index", runtime.MazeIndex,
			"room", scene.Coordinate.String(),
			"map_id", scene.Map.Map.ID,
			"target_object_key", request.TargetObjectKey,
			"dummy_boss_object_keys", storyAIBossGate.DummyBossObjectKeys,
			"AI_boss_object_keys", storyAIBossGate.AIBossObjectKeys,
			"reason", "story_ai_boss_and_dummy_boss_require_separate_authoritative_op39")
		return nil
	}
	scriptEvidence := dungeonTutorialScriptEvidence{}
	remainingScriptedMonsterIndexes := []int(nil)
	if !scene.Cleared {
		targetIsBossActor, targetBossActorType, targetBossActorSource := currentDungeonRuntimeObjectBossActor(
			runtime,
			targetObjectKey,
			runtimeDungeonMonsterDefeated,
		)
		if !tutorialScene && targetIsBossActor && !storyAIBossGateActive {
			forcedDefeated, forcedVisual, forcedCleared, forceErr := s.forceCurrentDungeonRemainingHostilesForCombatEndLocked(
				session,
				runtime,
				targetObjectKey,
				"op117_clear_decision_after_boss_actor_defeated",
			)
			if forceErr != nil {
				s.logGameEvent(session, "game-dungeon-boss-die-check-force-clear-blocked",
					"dungeon_id", runtime.Dungeon.ID,
					"room", scene.Coordinate.String(),
					"map_id", scene.Map.Map.ID,
					"target_object_key", request.TargetObjectKey,
					"boss_actor_type", targetBossActorType,
					"boss_actor_source", targetBossActorSource,
					"forced_defeated_count", forcedDefeated,
					"forced_visual_death_count", forcedVisual,
					"reason", "86jp_boss_death_clear_decision_force_clear_failed",
					"error", forceErr)
				return nil
			}
			snapshot := runtime.Session.Snapshot()
			scene = snapshot.Scene
			s.logGameEvent(session, "game-dungeon-boss-die-check-force-clear-applied",
				"dungeon_id", runtime.Dungeon.ID,
				"room", scene.Coordinate.String(),
				"map_id", scene.Map.Map.ID,
				"target_object_key", request.TargetObjectKey,
				"boss_actor_type", targetBossActorType,
				"boss_actor_source", targetBossActorSource,
				"forced_defeated_count", forcedDefeated,
				"forced_visual_death_count", forcedVisual,
				"forced_cleared", forcedCleared,
				"room_cleared", scene.Cleared,
				"source", "86jp_clear_condition_type4_boss_kills_remaining_hostiles")
		}
		if !tutorialScene {
			if scene.Cleared {
				goto bossRoomClearReady
			}
			s.logGameEvent(session, "game-dungeon-boss-die-check-blocked",
				"dungeon_id", runtime.Dungeon.ID,
				"room", scene.Coordinate.String(),
				"map_id", scene.Map.Map.ID,
				"target_object_key", request.TargetObjectKey,
				"reason", "ordinary_boss_room_not_authoritatively_cleared")
			return nil
		}
		if !targetIsOrdinaryMonster || targetReference.Kind != worldmap.HostileMonster {
			s.logGameEvent(session, "game-dungeon-boss-die-check-blocked",
				"dungeon_id", runtime.Dungeon.ID,
				"room", scene.Coordinate.String(),
				"map_id", scene.Map.Map.ID,
				"target_object_key", request.TargetObjectKey,
				"target_kind", targetReference.Kind,
				"reason", "uncleared_final_target_not_ordinary_scripted_monster")
			return nil
		}
		catalog, catalogReady := s.dungeonTutorialScriptCatalog()
		if !catalogReady {
			s.logGameEvent(session, "game-dungeon-boss-die-check-blocked",
				"dungeon_id", runtime.Dungeon.ID,
				"room", scene.Coordinate.String(),
				"map_id", scene.Map.Map.ID,
				"target_object_key", request.TargetObjectKey,
				"reason", "tutorial_script_catalog_unavailable")
			return nil
		}
		var remainingOwned bool
		remainingScriptedMonsterIndexes, remainingOwned = currentDungeonRemainingBlockingMonsterIndexes(scene)
		if !remainingOwned {
			s.logGameEvent(session, "game-dungeon-boss-die-check-blocked",
				"dungeon_id", runtime.Dungeon.ID,
				"room", scene.Coordinate.String(),
				"map_id", scene.Map.Map.ID,
				"target_object_key", request.TargetObjectKey,
				"reason", "uncleared_final_blocking_monster_runtime_ownership_incomplete")
			return nil
		}
		var scripted bool
		scriptEvidence, scripted = catalog.FindMonsterDestroyCovering(
			scene.Map.Map.ID,
			targetReference.Index,
			remainingScriptedMonsterIndexes,
		)
		if !scripted {
			s.logGameEvent(session, "game-dungeon-boss-die-check-blocked",
				"dungeon_id", runtime.Dungeon.ID,
				"room", scene.Coordinate.String(),
				"map_id", scene.Map.Map.ID,
				"target_object_key", request.TargetObjectKey,
				"target_monster_index", targetReference.Index,
				"remaining_blocking_monster_indexes", remainingScriptedMonsterIndexes,
				"reason", "no_single_pvf_cmt_covers_destroy_target_and_all_remaining_blockers")
			return nil
		}
		completionSource = "pvf_cmt_destroyed_tutorial_final_actor"
	}

bossRoomClearReady:
	if !tutorialScene {
		pendingLayer, nextLayerIndex, nextLayerMapID, layerErr := s.currentDungeonPendingLayer(runtime, scene)
		if layerErr != nil {
			s.logGameEvent(session, "game-dungeon-boss-die-check-blocked",
				"dungeon_id", runtime.Dungeon.ID,
				"maze_index", runtime.MazeIndex,
				"room", scene.Coordinate.String(),
				"map_id", scene.Map.Map.ID,
				"target_object_key", request.TargetObjectKey,
				"reason", "pending_layer_resolution_failed",
				"error", layerErr)
			return nil
		}
		if pendingLayer {
			s.logGameEvent(session, "game-dungeon-boss-die-check-deferred",
				"dungeon_id", runtime.Dungeon.ID,
				"maze_index", runtime.MazeIndex,
				"room", scene.Coordinate.String(),
				"map_id", scene.Map.Map.ID,
				"target_object_key", request.TargetObjectKey,
				"next_layer_index", nextLayerIndex,
				"next_layer_map_id", nextLayerMapID,
				"reason", "explicit_pvf_layer_must_be_consumed_before_settlement")
			return nil
		}
	}
	if err := runtime.Session.CompleteCurrentRoom(); err != nil {
		return err
	}
	runtime.bossDieCheckAccepted = true
	runtime.tutorialFinalRoomClearPending = false
	runtime.bossDieCheckRelatedActorObjectKey = request.RelatedActorObjectKey
	runtime.bossDieCheckTargetObjectKey = request.TargetObjectKey
	runtime.bossDieCheckPending = false
	runtime.bossDieCheckPendingRequest = dungeoncmd.BossDieCheckRequest{}
	completed := runtime.Session.Snapshot()
	s.logGameEvent(session, "game-dungeon-boss-die-check-committed",
		"dungeon_id", runtime.Dungeon.ID,
		"maze_index", runtime.MazeIndex,
		"room", completed.Run.Current.String(),
		"map_id", scene.Map.Map.ID,
		"target_object_key", request.TargetObjectKey,
		"completion_source", completionSource,
		"cinematic_id", scriptEvidence.CinematicID,
		"cinematic_path", scriptEvidence.CinematicPath,
		"remaining_blocking_monster_indexes", remainingScriptedMonsterIndexes,
		"cinematic_monster_actor_indexes", scriptEvidence.MonsterActorIndexes,
		"cinematic_destroy_monster_indexes", scriptEvidence.DestroyMonsterIndexes,
		"run_status", completed.Run.Status,
		"defeated_actor_count", len(completed.Scene.DefeatedObjects),
		"remaining_actor_deaths_fabricated", false,
		"runtime_retained", true,
		"op115_sent", runtime.bossDieCheckResponseSent,
		"next_stage", "persist_completion_then_reply_op115_then_bodyless_settlement_op31")
	return s.completeCurrentDungeonBossDieCheckLocked(session, runtime, "op117_completion")
}
