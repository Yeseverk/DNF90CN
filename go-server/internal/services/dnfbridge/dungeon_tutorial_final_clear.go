package dnfbridge

import (
	"context"
	"fmt"
	"strings"
	"time"

	"longheng.io/server/internal/modules/dnf/worldmap"
)

const currentDungeonTutorialFinalOp117Grace = 750 * time.Millisecond

// completePVFTutorialBosslessFinalRoomAfterDeathLocked owns the tutorial
// finals that have a PVF boss coordinate but no boss-ranked monster and no CMT
// destroy target. The caller has already committed and acknowledged the last
// real blocking actor death; this path never fabricates a boss or actor death.
func (s *Service) completePVFTutorialBosslessFinalRoomAfterDeathLocked(
	session *gameSession,
	runtime *runtimeDungeonState,
	scene worldmap.DungeonRoomScene,
) error {
	if session == nil || runtime == nil || runtime.Session == nil || runtime.Room == nil ||
		session.dungeon.runtime != runtime || runtime.tutorialFinalRoomClearAccepted {
		return nil
	}
	if !scene.Cleared || !scene.Boss || !runtime.BossSet ||
		scene.Coordinate != runtime.BossCoordinate || !isPVFTutorialDungeonScene(runtime, scene) {
		return nil
	}
	if len(scene.BlockingHostiles) == 0 {
		return nil
	}
	remaining, owned := currentDungeonRemainingBlockingMonsterIndexes(scene)
	if !owned || len(remaining) != 0 {
		return nil
	}
	catalog, ready := s.dungeonTutorialScriptCatalog()
	if !ready {
		s.logGameEvent(session, "game-dungeon-tutorial-final-clear-blocked",
			"dungeon_id", runtime.Dungeon.ID,
			"room", scene.Coordinate.String(),
			"map_id", scene.Map.Map.ID,
			"reason", "tutorial_script_catalog_unavailable")
		return nil
	}
	for _, monster := range scene.Monsters {
		if strings.EqualFold(strings.Trim(monster.Rank, "[] \t\r\n"), "boss") {
			s.logGameEvent(session, "game-dungeon-tutorial-final-clear-deferred",
				"dungeon_id", runtime.Dungeon.ID,
				"room", scene.Coordinate.String(),
				"map_id", scene.Map.Map.ID,
				"reason", "pvf_boss_rank_requires_op117")
			return nil
		}
	}
	if catalog.HasMonsterDestroyTargets(scene.Map.Map.ID) {
		runtime.tutorialFinalRoomClearPending = true
		s.logGameEvent(session, "game-dungeon-tutorial-final-clear-op117-grace-armed",
			"dungeon_id", runtime.Dungeon.ID,
			"room", scene.Coordinate.String(),
			"map_id", scene.Map.Map.ID,
			"grace_ms", currentDungeonTutorialFinalOp117Grace.Milliseconds(),
			"reason", "pvf_cmt_target_all_real_blockers_defeated_allow_immediate_client_op117_first")
		timerName := fmt.Sprintf(
			"dnf-dungeon-tutorial-final:%s:run:%d",
			session.connID,
			runtime.lifecycleToken,
		)
		queue := s.ensureGameplayTimeQueue()
		if queue == nil {
			return errGameplayTimeQueueUnavailable
		}
		if err := queue.ScheduleAfter(timerName, currentDungeonTutorialFinalOp117Grace, func(time.Time) {
			s.completePVFTutorialFinalRoomAfterOp117Grace(session, runtime, scene.Coordinate, scene.Map.Map.ID)
		}); err != nil {
			runtime.tutorialFinalRoomClearPending = false
			return err
		}
		return nil
	}
	return s.commitPVFTutorialBosslessFinalRoomLocked(session, runtime, scene, "authoritative_last_blocking_op39_no_cmt_target")
}

func (s *Service) completePVFTutorialFinalRoomAfterOp117Grace(
	session *gameSession,
	runtime *runtimeDungeonState,
	coordinate worldmap.RoomCoordinate,
	mapID int64,
) {
	if session == nil {
		return
	}
	session.dungeon.mu.Lock()
	defer session.dungeon.mu.Unlock()
	if session.dungeon.runtime != runtime || runtime == nil || runtime.Session == nil ||
		!runtime.tutorialFinalRoomClearPending || runtime.bossDieCheckAccepted ||
		runtime.tutorialFinalRoomClearAccepted {
		return
	}
	scene, ok := runtime.Session.Scene()
	if !ok || scene.Coordinate != coordinate || scene.Map.Map.ID != mapID || !scene.Cleared {
		return
	}
	if err := s.commitPVFTutorialBosslessFinalRoomLocked(
		session,
		runtime,
		scene,
		"op117_grace_elapsed_after_all_real_blockers_defeated",
	); err != nil {
		s.logGameEvent(session, "game-dungeon-tutorial-final-clear-grace-failed",
			"dungeon_id", runtime.Dungeon.ID,
			"room", coordinate.String(),
			"map_id", mapID,
			"error", err)
	}
}

func (s *Service) commitPVFTutorialBosslessFinalRoomLocked(
	session *gameSession,
	runtime *runtimeDungeonState,
	scene worldmap.DungeonRoomScene,
	source string,
) error {
	if runtime == nil || runtime.Session == nil || runtime.tutorialFinalRoomClearAccepted {
		return nil
	}

	if err := runtime.Session.Complete(); err != nil {
		return err
	}
	runtime.tutorialFinalRoomClearPending = false
	runtime.tutorialFinalRoomClearAccepted = true
	completed := runtime.Session.Snapshot()
	s.logGameEvent(session, "game-dungeon-tutorial-final-clear-committed",
		"char_id", session.selectedCharacterID,
		"dungeon_id", runtime.Dungeon.ID,
		"maze_index", runtime.MazeIndex,
		"room", completed.Run.Current.String(),
		"map_id", completed.Scene.Map.Map.ID,
		"blocking_hostile_count", len(completed.Scene.BlockingHostiles),
		"defeated_actor_count", len(completed.Scene.DefeatedObjects),
		"run_status", completed.Run.Status,
		"boss_object_fabricated", false,
		"op115_sent", false,
		"completion_source", source)
	return s.completeCurrentDungeonBosslessTutorialFinalLocked(session, runtime, source)
}

func (s *Service) completeCurrentDungeonBosslessTutorialFinalLocked(
	session *gameSession,
	runtime *runtimeDungeonState,
	source string,
) error {
	if session == nil || runtime == nil || runtime.Session == nil || session.dungeon.runtime != runtime ||
		!runtime.tutorialFinalRoomClearAccepted {
		return nil
	}
	if !dungeonRuntimeOwnsCharacter(runtime, session.selectedCharacterID) {
		return nil
	}
	snapshot := runtime.Session.Snapshot()
	if snapshot.Run.Status != worldmap.DungeonRunCompleted {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	if !runtime.tutorialCompletionPersisted {
		previousFlag, err := s.persistCurrentDungeonTutorialCompletion(ctx, session.selectedCharacterID)
		if err != nil {
			s.logGameEvent(session, "game-dungeon-tutorial-final-clear-completion-blocked",
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
			"town_id", newCharacterInitialTownID,
			"area_id", newCharacterInitialAreaID,
			"source", "bossless_final_room_authoritative_clear")
	}

	return s.sendCurrentDungeonSettlementEntryLocked(
		session,
		runtime,
		"bossless_pvf_tutorial_final_bodyless_op31_"+source,
	)
}
