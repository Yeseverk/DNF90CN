package dnfbridge

import (
	"fmt"
	"time"

	"longheng.io/server/internal/modules/dnf/worldmap"
)

const (
	currentSuspiciousVillageElevatorSummonID    = int64(1112)
	currentSuspiciousVillageElevatorControlID   = int64(1113)
	currentSuspiciousVillageElevatorScrollID    = int64(1111)
	currentSuspiciousVillageElevatorMonsterID   = int64(56716)
	currentSuspiciousVillageElevatorMsgID       = uint16(254)
	currentSuspiciousVillageElevatorPacketSize  = 2
	currentSuspiciousVillageElevatorTickDelay   = 15 * time.Second
	currentSuspiciousVillageElevatorLastCounter = byte(4)
)

type currentDungeonElevatorState struct {
	Started    bool
	Completed  bool
	Counter    byte
	State      byte
	Generation uint64
}

type currentDungeonElevatorNotification struct {
	Counter byte
	State   byte
}

func newCurrentDungeonElevatorState() currentDungeonElevatorState {
	return currentDungeonElevatorState{Started: true, State: 2, Generation: 1}
}

func (state *currentDungeonElevatorState) tick() (currentDungeonElevatorNotification, bool, bool) {
	if state == nil || !state.Started || state.Completed || state.State != 2 || state.Counter >= currentSuspiciousVillageElevatorLastCounter {
		return currentDungeonElevatorNotification{}, false, false
	}
	state.Counter++
	notification := currentDungeonElevatorNotification{Counter: state.Counter, State: 0}
	return notification, true, state.Counter <= 3
}

func (state *currentDungeonElevatorState) complete() currentDungeonElevatorNotification {
	if state == nil {
		return currentDungeonElevatorNotification{}
	}
	if !state.Started {
		*state = newCurrentDungeonElevatorState()
	}
	if state.Counter <= 3 {
		state.State = 1
	} else {
		state.State = 2
	}
	state.Completed = true
	state.Generation++
	return currentDungeonElevatorNotification{Counter: state.Counter, State: state.State}
}

func buildCurrentDungeonElevatorNotificationBody(notification currentDungeonElevatorNotification) []byte {
	return []byte{notification.Counter, notification.State}
}

func currentSuspiciousVillageElevatorScope(
	runtime *runtimeDungeonState,
	scene worldmap.DungeonRoomScene,
	room runtimeDungeonRoomSnapshot,
) bool {
	if runtime == nil || room.Coordinate != scene.Coordinate || room.MapID != scene.Map.Map.ID {
		return false
	}

	// The current EXE routes op254 solely through passive object 1113. The
	// surrounding PVF objects identify the complete elevator encounter: scroll
	// 1111, controller 1113, special object 1112 spawning monster 56716, and
	// four event-monster positions. These are content semantics, so do not bind
	// them to one dungeon, maze, coordinate, map ID, or map path.
	if len(scene.Map.Map.EventMonsterPositions) != 4 ||
		!currentDungeonSceneHasPassiveObject(scene, currentSuspiciousVillageElevatorScrollID) ||
		!currentDungeonSceneHasPassiveObject(scene, currentSuspiciousVillageElevatorControlID) ||
		!currentDungeonSceneHasElevatorSummon(scene) {
		return false
	}
	for _, actor := range room.ExtendedActors {
		if actor.Kind == runtimeDungeonActorSpecialMonster && int64(actor.Packet.Code) == currentSuspiciousVillageElevatorMonsterID {
			return true
		}
	}
	return false
}

func currentDungeonSceneHasPassiveObject(scene worldmap.DungeonRoomScene, objectID int64) bool {
	for _, object := range scene.PassiveObjects {
		if object.ObjectID == objectID {
			return true
		}
	}
	return false
}

func currentDungeonSceneHasElevatorSummon(scene worldmap.DungeonRoomScene) bool {
	for _, object := range scene.SpecialPassiveObjects {
		if object.ObjectID != currentSuspiciousVillageElevatorSummonID {
			continue
		}
		for _, spawn := range object.Spawns {
			if normalizeDungeonPVFSymbol(spawn.Kind) == "monster" && spawn.Code == currentSuspiciousVillageElevatorMonsterID {
				return true
			}
		}
	}
	return false
}

func currentSuspiciousVillageElevatorMoveBlocked(
	runtime *runtimeDungeonState,
	scene worldmap.DungeonRoomScene,
	room runtimeDungeonRoomSnapshot,
) bool {
	return currentSuspiciousVillageElevatorScope(runtime, scene, room) && !runtime.suspiciousVillageElevator.Completed
}

func (s *Service) startCurrentSuspiciousVillageElevator(session *gameSession, source string) error {
	if s == nil || session == nil {
		return nil
	}
	session.dungeon.mu.Lock()
	defer session.dungeon.mu.Unlock()
	runtime := session.dungeon.runtime
	if runtime == nil || runtime.Session == nil || runtime.Room == nil {
		return nil
	}
	scene, ok := runtime.Session.Scene()
	if !ok || !currentSuspiciousVillageElevatorScope(runtime, scene, runtime.Room.Snapshot()) {
		return nil
	}
	if runtime.suspiciousVillageElevator.Started {
		return nil
	}
	if runtime.lifecycleToken == 0 || s.gameplayTimers == nil {
		return fmt.Errorf("start suspicious-village elevator: dungeon timer unavailable")
	}
	runtime.suspiciousVillageElevator = newCurrentDungeonElevatorState()
	generation := runtime.suspiciousVillageElevator.Generation
	if err := s.scheduleCurrentSuspiciousVillageElevatorTickLocked(session, runtime, generation); err != nil {
		runtime.suspiciousVillageElevator = currentDungeonElevatorState{}
		return err
	}
	s.logGameEvent(session, "game-dungeon-elevator-started",
		"source", source,
		"dungeon_id", runtime.Dungeon.ID,
		"maze_index", runtime.MazeIndex,
		"room", scene.Coordinate.String(),
		"map_id", scene.Map.Map.ID,
		"run_token", runtime.lifecycleToken,
		"generation", generation,
		"counter", 0,
		"state", 2,
		"tick_delay_ms", currentSuspiciousVillageElevatorTickDelay.Milliseconds())
	return nil
}

// scheduleCurrentSuspiciousVillageElevatorTickLocked runs with
// session.dungeon.mu held. Timer-originated sends follow the same session event
// loop boundary as the dungeon death return: the character identity is frozen
// at schedule time and a stale selection drops the tick before any packet is
// written.
func (s *Service) scheduleCurrentSuspiciousVillageElevatorTickLocked(
	session *gameSession,
	runtime *runtimeDungeonState,
	generation uint64,
) error {
	nextCounter := runtime.suspiciousVillageElevator.Counter + 1
	timerName := fmt.Sprintf(
		"dnf-dungeon-elevator:%s:run:%d:generation:%d:counter:%d",
		session.connID,
		runtime.lifecycleToken,
		generation,
		nextCounter,
	)
	characterID, characterGeneration, err := gameSessionCharacterEventIdentity(session, session.selectedCharacterID)
	if err != nil {
		return err
	}
	runToken := runtime.lifecycleToken
	return s.gameplayTimers.ScheduleAfter(timerName, currentSuspiciousVillageElevatorTickDelay, func(due time.Time) {
		postErr := s.postGameSessionCharacterEvent(
			session,
			"dungeon-elevator-tick-timequeue",
			characterID,
			characterGeneration,
			func() error {
				s.fireCurrentSuspiciousVillageElevatorTick(session, runtime, generation, due)
				return nil
			},
		)
		if postErr != nil && !isClosedGameSessionEventError(postErr) {
			s.logGameEvent(session, "game-dungeon-elevator-timer-post-failed",
				"run_token", runToken,
				"generation", generation,
				"counter", nextCounter,
				"error", postErr)
		}
	})
}

func (s *Service) fireCurrentSuspiciousVillageElevatorTick(
	session *gameSession,
	runtime *runtimeDungeonState,
	generation uint64,
	due time.Time,
) {
	if s == nil || session == nil || runtime == nil {
		return
	}
	session.dungeon.mu.Lock()
	defer session.dungeon.mu.Unlock()
	if session.dungeon.runtime != runtime || runtime.lifecycleToken == 0 ||
		runtime.suspiciousVillageElevator.Generation != generation {
		return
	}
	scene, ok := runtime.Session.Scene()
	if !ok || !currentSuspiciousVillageElevatorScope(runtime, scene, runtime.Room.Snapshot()) {
		return
	}
	notification, send, scheduleNext := runtime.suspiciousVillageElevator.tick()
	if !send {
		return
	}
	if err := s.sendCurrentSuspiciousVillageElevatorNotification(session, runtime, scene, notification, "reference_timer_tick"); err != nil {
		s.logGameEvent(session, "game-dungeon-elevator-notification-failed",
			"counter", notification.Counter,
			"state", notification.State,
			"scheduled_due_utc", due.UTC().Format(time.RFC3339Nano),
			"error", err)
		return
	}
	if scheduleNext {
		if err := s.scheduleCurrentSuspiciousVillageElevatorTickLocked(session, runtime, generation); err != nil {
			s.logGameEvent(session, "game-dungeon-elevator-timer-failed",
				"counter", notification.Counter,
				"state", notification.State,
				"error", err)
		}
	}
}

// completeCurrentSuspiciousVillageElevatorLocked runs with session.dungeon.mu
// held, after the special-monster retirement branch of the op38/op39 death
// handler has already validated the retired actor's object key.
func (s *Service) completeCurrentSuspiciousVillageElevatorLocked(
	session *gameSession,
	runtime *runtimeDungeonState,
	objectKey uint32,
) error {
	if s == nil || session == nil || runtime == nil || runtime.Session == nil || runtime.Room == nil {
		return nil
	}
	scene, ok := runtime.Session.Scene()
	room := runtime.Room.Snapshot()
	if !ok || !currentSuspiciousVillageElevatorScope(runtime, scene, room) {
		return nil
	}
	matchedControlMonster := false
	for _, actor := range room.ExtendedActors {
		if actor.ObjectKey == objectKey && actor.Kind == runtimeDungeonActorSpecialMonster &&
			int64(actor.Packet.Code) == currentSuspiciousVillageElevatorMonsterID {
			matchedControlMonster = true
			break
		}
	}
	if !matchedControlMonster || runtime.suspiciousVillageElevator.Completed {
		return nil
	}
	notification := runtime.suspiciousVillageElevator.complete()
	return s.sendCurrentSuspiciousVillageElevatorNotification(session, runtime, scene, notification, "special_monster_56716_retired_before_op38")
}

func (s *Service) sendCurrentSuspiciousVillageElevatorNotification(
	session *gameSession,
	runtime *runtimeDungeonState,
	scene worldmap.DungeonRoomScene,
	notification currentDungeonElevatorNotification,
	source string,
) error {
	body := buildCurrentDungeonElevatorNotificationBody(notification)
	if len(body) != currentSuspiciousVillageElevatorPacketSize {
		return fmt.Errorf("current elevator notification body length %d, want %d", len(body), currentSuspiciousVillageElevatorPacketSize)
	}
	s.logGameEvent(session, "game-dungeon-elevator-notification-send",
		"source", source,
		"dungeon_id", runtime.Dungeon.ID,
		"maze_index", runtime.MazeIndex,
		"room", scene.Coordinate.String(),
		"map_id", scene.Map.Map.ID,
		"msg_id", currentSuspiciousVillageElevatorMsgID,
		"classification", 0,
		"body_len", len(body),
		"counter", notification.Counter,
		"state", notification.State,
		"body_source", "current_exe_HandleElevatorClearTimeCheck_Op254_u8_counter_u8_state")
	return s.sendGameUpperRawClass(session, currentSuspiciousVillageElevatorMsgID, body, 0)
}
