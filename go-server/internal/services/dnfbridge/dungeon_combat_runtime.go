package dnfbridge

import (
	"context"
	"errors"
	"fmt"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	"longheng.io/server/internal/modules/dnf/dungeoncmd"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

// Friendly PVF APCs can follow the player into later rooms while retaining
// the object key announced by their original room.
func dungeonRuntimeContainsDeathOwnerObjectKey(runtime *runtimeDungeonState, objectKey uint32) bool {
	if runtime == nil || runtime.Room == nil || objectKey == 0 {
		return false
	}
	if runtime.Room.ContainsAnnouncedActorObjectKey(objectKey) {
		return true
	}
	for _, visit := range runtime.Rooms {
		if visit == nil || visit.Room == nil || visit.Room == runtime.Room {
			continue
		}
		if visit.Room.ContainsAnnouncedNonHostileAICharacterObjectKey(objectKey) {
			return true
		}
	}
	return false
}

func (s *Service) handleDungeonMonsterDeath(session *gameSession, body []byte) error {
	request, err := dungeoncmd.DecodeDieMonsterRequest(body)
	if err != nil {
		s.logGameEvent(session, "game-dungeon-monster-death-blocked",
			"body_len", len(body),
			"reason", "current_exe_op39_request_malformed",
			"error", err)
		return nil
	}
	if len(request.OpaqueTail) != 0 {
		s.logGameEvent(session, "game-dungeon-monster-death-blocked",
			"object_key", request.RuntimeObjectKey,
			"body_len", len(body),
			"tail_len", len(request.OpaqueTail),
			"reason", "current_exe_op39_request_tail_unproven")
		return nil
	}
	if session == nil {
		return nil
	}
	session.dungeon.mu.Lock()
	defer session.dungeon.mu.Unlock()
	runtime := session.dungeon.runtime
	if runtime == nil || runtime.Session == nil || runtime.Room == nil {
		s.logGameEvent(session, "game-dungeon-monster-death-blocked",
			"object_key", request.RuntimeObjectKey,
			"reason", "dungeon_runtime_missing")
		return nil
	}
	scene, hasScene := runtime.Session.Scene()
	ownerIsSentinel := request.OwnerObjectKey == ^uint16(0)
	var scriptedTutorialMonster runtimeDungeonMonster
	var scriptedTutorialEvidence dungeonTutorialScriptEvidence
	scriptedTutorialDeath := false
	ownerSentinelOrdinaryTarget := false
	ownerSentinelBlockingClearTarget := false
	ownerSentinelBlockingClearSource := ""
	if ownerIsSentinel {
		if request.Layout != dungeoncmd.DieMonsterRequestLayoutVariableCombat || request.CombatEntryCount != 0 {
			s.logGameEvent(session, "game-dungeon-monster-death-blocked",
				"dungeon_id", runtime.Dungeon.ID,
				"maze_index", runtime.MazeIndex,
				"object_key", request.RuntimeObjectKey,
				"related_actor_object_key", request.OwnerObjectKey,
				"request_layout", request.Layout,
				"combat_entry_count", request.CombatEntryCount,
				"reason", "non_hostile_ai_request_shape_unproven")
			return nil
		}
		if hasScene {
			scriptedTutorialMonster, scriptedTutorialEvidence, scriptedTutorialDeath = s.isPVFTutorialScriptedMonsterDeath(
				runtime,
				scene,
				request.RuntimeObjectKey,
			)
			if !scriptedTutorialDeath {
				_, ownerSentinelBlockingClearSource, ownerSentinelBlockingClearTarget =
					currentDungeonOwnerSentinelBlockingClearTarget(runtime, scene, request.RuntimeObjectKey)
				if !ownerSentinelBlockingClearTarget {
					_, ownerSentinelOrdinaryTarget = currentDungeonOwnerSentinelAnnouncedOrdinaryMonster(
						runtime,
						scene,
						request.RuntimeObjectKey,
					)
				}
			}
		}
	}
	if ownerIsSentinel && !scriptedTutorialDeath && !ownerSentinelBlockingClearTarget && !ownerSentinelOrdinaryTarget {
		target, retirementErr := runtime.Room.RetireNonHostileAIActor(request.RuntimeObjectKey)
		if retirementErr != nil {
			if hasScene {
				handled, clearErr := s.handleCurrentDungeonDestroyObjectClearConditionDeathLocked(
					session,
					runtime,
					scene,
					request.RuntimeObjectKey,
				)
				if clearErr != nil {
					return clearErr
				}
				if handled {
					s.cancelCurrentDungeonReturnAfterDungeonEvidenceLocked(
						session,
						runtime,
						"accepted_destroy_object_clear_condition_op39_after_pending_op24",
					)
					return nil
				}
				handled, passiveErr := s.handleCurrentDungeonQuestPassiveObjectDeathLocked(
					session,
					runtime,
					scene,
					request.RuntimeObjectKey,
				)
				if passiveErr != nil {
					s.logGameEvent(session, "game-dungeon-quest-passive-object-death-deferred",
						"dungeon_id", runtime.Dungeon.ID,
						"maze_index", runtime.MazeIndex,
						"object_key", request.RuntimeObjectKey,
						"related_actor_object_key", request.OwnerObjectKey,
						"reason", "unique_active_pvf_passive_target_owner_failed",
						"error", passiveErr)
					return nil
				}
				if handled {
					s.cancelCurrentDungeonReturnAfterDungeonEvidenceLocked(
						session,
						runtime,
						"accepted_quest_passive_object_op39_after_pending_op24",
					)
					return nil
				}
			}
			s.logGameEvent(session, "game-dungeon-monster-death-blocked",
				"dungeon_id", runtime.Dungeon.ID,
				"maze_index", runtime.MazeIndex,
				"object_key", request.RuntimeObjectKey,
				"related_actor_object_key", request.OwnerObjectKey,
				"request_layout", request.Layout,
				"reason", "non_hostile_ai_retirement_rejected",
				"error", retirementErr)
			return nil
		}
		responseBody, err := buildCurrentDungeonDeathNotificationBody(
			target.ObjectKey,
			target.ResponseKind,
		)
		if err != nil {
			return err
		}
		if err := s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketNotifyDieMonster), responseBody, 0); err != nil {
			return err
		}
		scene, _ = runtime.Session.Scene()
		s.logGameEvent(session, "game-dungeon-non-hostile-ai-retired",
			"dungeon_id", runtime.Dungeon.ID,
			"maze_index", runtime.MazeIndex,
			"room", scene.Coordinate.String(),
			"map_id", scene.Map.Map.ID,
			"object_key", request.RuntimeObjectKey,
			"related_actor_object_key", request.OwnerObjectKey,
			"request_layout", request.Layout,
			"combat_entry_count", request.CombatEntryCount,
			"response_kind", target.ResponseKind,
			"response_opcode", uint16(dnfenum.CmdPacketNotifyDieMonster),
			"response_body_len", len(responseBody),
			"room_cleared", scene.Cleared,
			"boss_room", scene.Boss,
			"reward_assets", "unchanged",
			"response", "current_exe_op38_non_hostile_ai_retirement_ack")
		s.cancelCurrentDungeonReturnAfterDungeonEvidenceLocked(
			session,
			runtime,
			"accepted_non_hostile_ai_op39_after_pending_op24",
		)
		return nil
	}
	playerObjectKey := currentSceneActorObjectKey(session.selectedCharacterID)
	if !scriptedTutorialDeath && !ownerSentinelBlockingClearTarget && !ownerSentinelOrdinaryTarget &&
		request.OwnerObjectKey != playerObjectKey &&
		!dungeonRuntimeContainsDeathOwnerObjectKey(runtime, uint32(request.OwnerObjectKey)) {
		s.logGameEvent(session, "game-dungeon-monster-death-blocked",
			"object_key", request.RuntimeObjectKey,
			"related_actor_object_key", request.OwnerObjectKey,
			"player_object_key", playerObjectKey,
			"request_layout", request.Layout,
			"reason", "room_related_actor_object_key_mismatch",
			"error", errDungeonMonsterOwnerMismatch)
		return nil
	}
	if target, matchedSpecialMonster, retirementErr := runtime.Room.RetireSpecialMonsterActor(request.RuntimeObjectKey); matchedSpecialMonster {
		if retirementErr != nil {
			s.logGameEvent(session, "game-dungeon-monster-death-blocked",
				"dungeon_id", runtime.Dungeon.ID,
				"maze_index", runtime.MazeIndex,
				"object_key", request.RuntimeObjectKey,
				"related_actor_object_key", request.OwnerObjectKey,
				"request_layout", request.Layout,
				"combat_entry_count", request.CombatEntryCount,
				"reason", "special_monster_retirement_rejected",
				"error", retirementErr)
			return nil
		}
		if err := s.completeCurrentSuspiciousVillageElevatorLocked(session, runtime, target.ObjectKey); err != nil {
			return err
		}
		responseBody, err := buildCurrentDungeonDeathNotificationBody(
			target.ObjectKey,
			target.ResponseKind,
		)
		if err != nil {
			return err
		}
		if err := s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketNotifyDieMonster), responseBody, 0); err != nil {
			return err
		}
		if err := runtime.cacheCurrentDungeonRoom(); err != nil {
			return fmt.Errorf("cache dungeon room after special monster retirement: %w", err)
		}
		scene, _ = runtime.Session.Scene()
		s.logGameEvent(session, "game-dungeon-special-monster-retired",
			"dungeon_id", runtime.Dungeon.ID,
			"maze_index", runtime.MazeIndex,
			"room", scene.Coordinate.String(),
			"map_id", scene.Map.Map.ID,
			"object_key", request.RuntimeObjectKey,
			"related_actor_object_key", request.OwnerObjectKey,
			"request_layout", request.Layout,
			"combat_entry_count", request.CombatEntryCount,
			"response_kind", target.ResponseKind,
			"response_opcode", uint16(dnfenum.CmdPacketNotifyDieMonster),
			"response_body_len", len(responseBody),
			"room_cleared", scene.Cleared,
			"boss_room", scene.Boss,
			"reward_assets", "unchanged",
			"response", "current_exe_op38_special_monster_retirement_ack")
		s.cancelCurrentDungeonReturnAfterDungeonEvidenceLocked(
			session,
			runtime,
			"accepted_special_monster_op39_after_pending_op24",
		)
		return nil
	}
	dropMonster, ordinaryDropCandidate := runtime.Room.AnnouncedMonster(request.RuntimeObjectKey)
	bossActorDeath, bossActorType, bossActorSource := currentDungeonRuntimeObjectBossActor(
		runtime,
		request.RuntimeObjectKey,
		runtimeDungeonMonsterAnnounced,
	)
	clearConditionActorDeath := false
	clearConditionSource := ""
	if ordinaryDropCandidate {
		clearConditionSource = currentDungeonClearConditionSource(runtime, dropMonster)
		clearConditionActorDeath = clearConditionSource != ""
		if clearConditionActorDeath && !bossActorDeath {
			bossActorDeath = true
			bossActorType = 3
			bossActorSource = clearConditionSource
		}
	}
	target, cleared, reportErr := runtime.Room.CommitActorDeathReport(request.RuntimeObjectKey, runtime.Session)
	if reportErr != nil {
		s.logGameEvent(session, "game-dungeon-monster-death-blocked",
			"dungeon_id", runtime.Dungeon.ID,
			"maze_index", runtime.MazeIndex,
			"object_key", request.RuntimeObjectKey,
			"reason", "runtime_actor_owner_rejected",
			"error", reportErr)
		return nil
	}
	scene, _ = runtime.Session.Scene()
	storyAIBossGate, storyAIBossGateActive := currentDungeonStoryAIBossDeathGate(runtime, scene)
	rewardMonster := dropMonster
	rewardMonsterCandidate := ordinaryDropCandidate
	if !rewardMonsterCandidate {
		if monster, ok := runtime.Room.MonsterByReference(
			target.Reference,
			runtimeDungeonMonsterAnnounced,
			runtimeDungeonMonsterDefeated,
		); ok {
			rewardMonster = monster
			rewardMonsterCandidate = true
		}
	}
	if rewardMonsterCandidate && !clearConditionActorDeath {
		clearConditionSource = currentDungeonClearConditionSource(runtime, rewardMonster)
		clearConditionActorDeath = clearConditionSource != ""
		if clearConditionActorDeath && !bossActorDeath {
			bossActorDeath = true
			bossActorType = 3
			bossActorSource = clearConditionSource
		}
	}
	var deathDrops []currentDungeonDeathDropWire
	dropSource := "none"
	if rewardMonsterCandidate {
		deathDrops, err = s.planCurrentDungeonMonsterDrops(runtime, rewardMonster, playerObjectKey)
		if err != nil {
			reason := "runtime_pvf_csharp_formula_drop_plan_failed_zero_drop_fallback"
			dropSource = "zero_drop_fallback"
			s.logGameEvent(session, "game-dungeon-monster-drop-deferred",
				"dungeon_id", runtime.Dungeon.ID,
				"maze_index", runtime.MazeIndex,
				"room", scene.Coordinate.String(),
				"map_id", scene.Map.Map.ID,
				"monster_id", rewardMonster.Spawn.MonsterID,
				"monster_object_key", request.RuntimeObjectKey,
				"logical_monster_object_key", rewardMonster.ObjectKey,
				"map_random_drop_count", rewardMonster.Spawn.RandomDropCount,
				"map_offset7_specified_pool_count", rewardMonster.Spawn.FixedDropCount,
				"reason", reason,
				"error", err)
			deathDrops = nil
		} else if len(deathDrops) != 0 {
			dropSource = "runtime_pvf_csharp_drop_formula_standard_item_branch"
		} else {
			dropSource = "runtime_pvf_csharp_drop_formula_no_standard_item"
		}
	}
	var monsterExperience currentDungeonMonsterExperienceCommitResult
	var monsterExperienceBody []byte
	if rewardMonsterCandidate {
		experienceContext, cancelExperience := context.WithTimeout(context.Background(), createWriteTimeout)
		monsterExperience, err = s.awardCurrentDungeonMonsterExperience(
			experienceContext,
			session,
			runtime,
			rewardMonster,
		)
		cancelExperience()
		if err != nil {
			s.logGameEvent(session, "game-dungeon-monster-experience-deferred",
				"dungeon_id", runtime.Dungeon.ID,
				"maze_index", runtime.MazeIndex,
				"room", scene.Coordinate.String(),
				"map_id", scene.Map.Map.ID,
				"monster_id", rewardMonster.Spawn.MonsterID,
				"monster_object_key", request.RuntimeObjectKey,
				"logical_monster_object_key", rewardMonster.ObjectKey,
				"reason", "runtime_pvf_csharp_domain_formula_or_atomic_progression_unavailable",
				"error", err)
		} else {
			recordCurrentDungeonMonsterExperience(runtime, rewardMonster, monsterExperience)
			if monsterExperience.Award.Gain > 0 {
				runtime.Character = dnfrepo.CloneCharacter(monsterExperience.Character)
				monsterExperienceBody = buildCurrentFinishLoadingCharacterStateBodyWithPresentation(
					monsterExperience.Character,
					monsterExperience.Skill.Points,
					&currentFinishLoadingExperiencePresentation{
						GrowthContractBonus: monsterExperience.GrowthContractBonus,
					},
				)
				if len(monsterExperienceBody) != currentFinishLoadingCharacterStateBodySize {
					s.logGameEvent(session, "game-dungeon-monster-experience-deferred",
						"dungeon_id", runtime.Dungeon.ID,
						"monster_id", dropMonster.Spawn.MonsterID,
						"monster_object_key", request.RuntimeObjectKey,
						"reason", "current_exe_op37_body_shape_invalid_after_atomic_commit",
						"body_len", len(monsterExperienceBody))
					monsterExperienceBody = nil
				}
			}
		}
	}
	forcedBossClearCount := 0
	forcedBossVisualDeathCount := 0
	var stagedStoryForcedCombatEnd *currentDungeonForcedCombatEnd
	nextStoryCoordinate, nextStoryStageIndex, pendingStoryAdvance := currentDungeonPendingStoryAdvance(runtime, scene)
	if pendingStoryAdvance && bossActorDeath && !cleared && !storyAIBossGateActive &&
		!isPVFTutorialDungeonScene(runtime, scene) {
		prepared, prepareErr := s.prepareCurrentDungeonForcedCombatEndLocked(runtime, request.RuntimeObjectKey)
		if prepareErr != nil {
			s.logGameEvent(session, "game-dungeon-story-stage-force-clear-deferred",
				"dungeon_id", runtime.Dungeon.ID,
				"maze_index", runtime.MazeIndex,
				"room", scene.Coordinate.String(),
				"map_id", scene.Map.Map.ID,
				"story_stage_index", runtime.StoryStageIndex,
				"next_story_stage_index", nextStoryStageIndex,
				"next_story_room", nextStoryCoordinate.String(),
				"boss_object_key", request.RuntimeObjectKey,
				"reason", "story_stage_boss_forced_clear_prepare_failed",
				"error", prepareErr)
		} else {
			stagedStoryForcedCombatEnd = &prepared
			forcedBossClearCount = len(prepared.Targets)
			cleared = cleared || prepared.Cleared
			if prepared.SceneOK {
				scene = prepared.Scene
			}
		}
	}
	var optionalStoryRoom []worldmap.RoomCoordinate
	if pendingStoryAdvance && cleared && (!storyAIBossGateActive || storyAIBossGate.Ready) {
		optionalStoryRoom = append(optionalStoryRoom, nextStoryCoordinate)
	}
	responseBody, err := buildCurrentDungeonDeathNotificationBodyWithDrops(
		target.ObjectKey,
		target.ResponseKind,
		deathDrops,
		optionalStoryRoom...,
	)
	if err != nil {
		return err
	}
	if err := s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketNotifyDieMonster), responseBody, 0); err != nil {
		return err
	}
	if stagedStoryForcedCombatEnd != nil {
		forcedVisual, forcedCleared, flushErr := s.flushCurrentDungeonForcedCombatEndLocked(
			session,
			runtime,
			request.RuntimeObjectKey,
			"op39_story_stage_boss_actor_death_after_source_coordinate_response",
			*stagedStoryForcedCombatEnd,
		)
		if flushErr != nil {
			return flushErr
		}
		forcedBossVisualDeathCount = forcedVisual
		cleared = cleared || forcedCleared
	}
	if len(monsterExperienceBody) != 0 {
		if err := s.sendGameUpperRawClass(session, currentDungeonCharacterStateMsgID, monsterExperienceBody, 0); err != nil {
			return err
		}
		s.logGameEvent(session, "game-dungeon-monster-experience-awarded",
			"dungeon_id", runtime.Dungeon.ID,
			"maze_index", runtime.MazeIndex,
			"room", scene.Coordinate.String(),
			"map_id", scene.Map.Map.ID,
			"monster_id", monsterExperience.Award.MonsterID,
			"monster_object_key", request.RuntimeObjectKey,
			"logical_monster_object_key", rewardMonster.ObjectKey,
			"monster_level", monsterExperience.Award.MonsterLevel,
			"monster_type", monsterExperience.Award.MonsterType,
			"named_monster", monsterExperience.Award.NamedMonster,
			"monster_table_exp", monsterExperience.Award.MonsterTableEXP,
			"dungeon_weight", monsterExperience.Award.DungeonWeight,
			"difficulty_rate", monsterExperience.Award.DifficultyRate,
			"monster_type_rate", monsterExperience.Award.MonsterTypeRate,
			"level_penalty", monsterExperience.Award.LevelPenalty,
			"pre_penalty_gain", monsterExperience.Award.PrePenaltyGain,
			"experience_gain", monsterExperience.Award.Gain,
			"growth_contract_bonus", monsterExperience.GrowthContractBonus,
			"committed_experience_gain", uint64(monsterExperience.Award.Gain)+uint64(monsterExperience.GrowthContractBonus),
			"honor_expert_gain", monsterExperience.HonorExpertGain,
			"honor_expert_level", numericCharacterStatValue(monsterExperience.Character, currentHonorExpertLevelStatKey),
			"honor_expert_progress", numericCharacterStatValue(monsterExperience.Character, currentHonorExpertProgressExperienceStatKey),
			"sp_gain", monsterExperience.SPGain,
			"tp_gain", monsterExperience.TPGain,
			"remaining_sp", monsterExperience.Skill.Points.RemainingSP,
			"remaining_tp", monsterExperience.Skill.Points.RemainingTP,
			"new_level", monsterExperience.Character.Level,
			"new_total_exp", statU32(monsterExperience.Character, "exp", 0),
			"op38_then_op37", true,
			"op37_body_len", len(monsterExperienceBody),
			"formula_source", "86jp_domain_formula_with_runtime_pvf_monsterexp_table",
			"packet_source", "current_exe_sub_1D78240_full_character_state")
	}
	if rewardMonsterCandidate {
		huntCtx, cancelHunt := context.WithTimeout(context.Background(), createWriteTimeout)
		huntErr := s.persistCurrentDungeonHuntEnemyKillPhaseA(
			huntCtx,
			session,
			runtime,
			scene,
			rewardMonster,
			request.RuntimeObjectKey,
		)
		cancelHunt()
		if huntErr != nil && !errors.Is(huntErr, errDungeonHuntEnemyCompletionUnavailable) {
			s.logGameEvent(session, "game-dungeon-hunt-enemy-progress-deferred",
				"dungeon_id", runtime.Dungeon.ID,
				"maze_index", runtime.MazeIndex,
				"room", scene.Coordinate.String(),
				"map_id", scene.Map.Map.ID,
				"monster_id", rewardMonster.Spawn.MonsterID,
				"monster_object_key", request.RuntimeObjectKey,
				"logical_monster_object_key", rewardMonster.ObjectKey,
				"reason", "86jp_hunt_enemy_progress_owner_failed",
				"error", huntErr)
		}
		s.checkCurrentDungeonQuestMonsterDrops(session, runtime, rewardMonster.Spawn.MonsterID)
	}
	if bossActorDeath && !storyAIBossGateActive && !isPVFTutorialDungeonScene(runtime, scene) &&
		stagedStoryForcedCombatEnd == nil {
		forcedDefeated, forcedVisual, forcedCleared, forceErr := s.forceCurrentDungeonRemainingHostilesForCombatEndLocked(
			session,
			runtime,
			request.RuntimeObjectKey,
			"op39_boss_actor_death",
		)
		if forceErr != nil {
			s.logGameEvent(session, "game-dungeon-boss-forced-clear-deferred",
				"dungeon_id", runtime.Dungeon.ID,
				"maze_index", runtime.MazeIndex,
				"room", scene.Coordinate.String(),
				"map_id", scene.Map.Map.ID,
				"boss_object_key", request.RuntimeObjectKey,
				"monster_id", rewardMonster.Spawn.MonsterID,
				"boss_rank", rewardMonster.Spawn.Rank,
				"boss_actor_type", bossActorType,
				"boss_actor_source", bossActorSource,
				"forced_defeated_count", forcedDefeated,
				"reason", "86jp_boss_death_forced_clear_commit_failed",
				"error", forceErr)
		} else if forcedDefeated > 0 || forcedCleared {
			forcedBossClearCount = forcedDefeated
			forcedBossVisualDeathCount = forcedVisual
			cleared = cleared || forcedCleared
			scene, _ = runtime.Session.Scene()
			s.logGameEvent(session, "game-dungeon-boss-forced-room-clear",
				"dungeon_id", runtime.Dungeon.ID,
				"maze_index", runtime.MazeIndex,
				"room", scene.Coordinate.String(),
				"map_id", scene.Map.Map.ID,
				"boss_object_key", request.RuntimeObjectKey,
				"monster_id", rewardMonster.Spawn.MonsterID,
				"boss_rank", rewardMonster.Spawn.Rank,
				"boss_actor_type", bossActorType,
				"boss_actor_source", bossActorSource,
				"forced_defeated_count", forcedDefeated,
				"forced_visual_death_count", forcedBossVisualDeathCount,
				"defeated_actor_count", len(scene.DefeatedObjects),
				"tracked_hostile_count", len(scene.ExpectedHostiles),
				"blocking_hostile_count", len(scene.BlockingHostiles),
				"room_cleared", scene.Cleared,
				"flow_source", "86jp_boss_death_means_combat_end")
		}
	}
	if err := runtime.cacheCurrentDungeonRoom(); err != nil {
		return fmt.Errorf("cache dungeon room after actor death: %w", err)
	}
	s.logGameEvent(session, "game-dungeon-monster-death-accepted",
		"dungeon_id", runtime.Dungeon.ID,
		"maze_index", runtime.MazeIndex,
		"room", scene.Coordinate.String(),
		"map_id", scene.Map.Map.ID,
		"object_key", request.RuntimeObjectKey,
		"related_actor_object_key", request.OwnerObjectKey,
		"request_layout", request.Layout,
		"combat_entry_count", request.CombatEntryCount,
		"actor_kind", target.Reference.Kind,
		"scripted_tutorial_death", scriptedTutorialDeath,
		"scripted_tutorial_monster_id", scriptedTutorialMonster.Spawn.MonsterID,
		"scripted_tutorial_rank", scriptedTutorialMonster.Spawn.Rank,
		"scripted_tutorial_monster_index", scriptedTutorialEvidence.MonsterIndex,
		"scripted_tutorial_cinematic_id", scriptedTutorialEvidence.CinematicID,
		"scripted_tutorial_cinematic_path", scriptedTutorialEvidence.CinematicPath,
		"owner_sentinel_blocking_clear_target", ownerSentinelBlockingClearTarget,
		"owner_sentinel_blocking_clear_source", ownerSentinelBlockingClearSource,
		"owner_sentinel_ordinary_target", ownerSentinelOrdinaryTarget,
		"response_kind", target.ResponseKind,
		"response_opcode", uint16(dnfenum.CmdPacketNotifyDieMonster),
		"response_body_len", len(responseBody),
		"story_stage_index", runtime.StoryStageIndex,
		"next_story_stage_index", nextStoryStageIndex,
		"story_stage_advance", len(optionalStoryRoom) == 1,
		"story_stage_next_room", nextStoryCoordinate.String(),
		"defeated_actor_count", len(scene.DefeatedObjects),
		"tracked_hostile_count", len(scene.ExpectedHostiles),
		"blocking_hostile_count", len(scene.BlockingHostiles),
		"room_cleared", cleared,
		"boss_actor_death", bossActorDeath,
		"boss_actor_type", bossActorType,
		"boss_actor_source", bossActorSource,
		"clear_condition_actor_death", clearConditionActorDeath,
		"clear_condition_source", clearConditionSource,
		"logical_monster_resolved", rewardMonsterCandidate,
		"logical_monster_id", rewardMonster.Spawn.MonsterID,
		"logical_monster_object_key", rewardMonster.ObjectKey,
		"boss_forced_defeated_count", forcedBossClearCount,
		"boss_forced_visual_death_count", forcedBossVisualDeathCount,
		"story_ai_boss_dual_death_gate", storyAIBossGateActive,
		"story_ai_boss_dual_death_ready", storyAIBossGate.Ready,
		"story_dummy_boss_object_keys", storyAIBossGate.DummyBossObjectKeys,
		"story_ai_boss_object_keys", storyAIBossGate.AIBossObjectKeys,
		"boss_room", scene.Boss,
		"drop_count", len(deathDrops),
		"drop_source", dropSource,
		"monster_experience_gain", monsterExperience.Award.Gain,
		"monster_experience_state_sent", len(monsterExperienceBody) != 0,
		"response", "current_exe_op38_actor_death_notification")
	if runtime.bossDieCheckPending &&
		uint32(runtime.bossDieCheckPendingRequest.TargetObjectKey) == request.RuntimeObjectKey {
		pendingRequest := runtime.bossDieCheckPendingRequest
		s.logGameEvent(session, "game-dungeon-boss-die-check-resume-after-death",
			"dungeon_id", runtime.Dungeon.ID,
			"maze_index", runtime.MazeIndex,
			"room", scene.Coordinate.String(),
			"map_id", scene.Map.Map.ID,
			"related_actor_object_key", pendingRequest.RelatedActorObjectKey,
			"target_object_key", pendingRequest.TargetObjectKey,
			"reason", "same_target_authoritative_op39_committed")
		return s.handleDungeonBossDieCheckLocked(session, runtime, pendingRequest, true)
	}
	if cleared || (storyAIBossGateActive && storyAIBossGate.Ready && scene.Cleared) {
		if err := s.completeCurrentDungeonOrdinaryFinalRoomAfterDeathLocked(session, runtime, scene, request.RuntimeObjectKey); err != nil {
			return err
		}
		if err := s.completePVFTutorialBosslessFinalRoomAfterDeathLocked(session, runtime, scene); err != nil {
			return err
		}
	}
	s.cancelCurrentDungeonReturnAfterDungeonEvidenceLocked(
		session,
		runtime,
		"accepted_hostile_op39_after_pending_op24",
	)
	return nil
}

func (runtime *runtimeDungeonState) accumulateCurrentDungeonSettlementMonsterExperience(
	monsterType byte,
	gain uint32,
) {
	if runtime == nil || gain == 0 {
		return
	}
	runtime.settlementMonsterExperienceTotal = saturatingCurrentDungeonUint32Add(
		runtime.settlementMonsterExperienceTotal,
		gain,
	)
	switch monsterType {
	case 1:
		runtime.settlementChampionExperience = saturatingCurrentDungeonUint32Add(
			runtime.settlementChampionExperience,
			gain,
		)
	case 2:
		runtime.settlementSuperChampionExperience = saturatingCurrentDungeonUint32Add(
			runtime.settlementSuperChampionExperience,
			gain,
		)
	case 3:
		runtime.settlementBossExperience = saturatingCurrentDungeonUint32Add(
			runtime.settlementBossExperience,
			gain,
		)
	}
}

func saturatingCurrentDungeonUint32Add(current uint32, delta uint32) uint32 {
	if ^uint32(0)-current < delta {
		return ^uint32(0)
	}
	return current + delta
}
