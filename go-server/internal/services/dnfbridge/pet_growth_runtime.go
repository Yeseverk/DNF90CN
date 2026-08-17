package dnfbridge

import (
	"context"
	"encoding/binary"
	"fmt"
	"strconv"
	"time"

	dnfpet "longheng.io/server/internal/modules/dnf/pet"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

const currentCreatureGrowthMsgID uint16 = 102

const (
	currentPetGrowthDungeonTick = 60 * time.Second
	currentPetGrowthTownTick    = 360 * time.Second
)

type currentPetGrowthClockMode byte

const (
	currentPetGrowthClockStopped currentPetGrowthClockMode = iota
	currentPetGrowthClockTown
	currentPetGrowthClockDungeon
)

func (mode currentPetGrowthClockMode) String() string {
	switch mode {
	case currentPetGrowthClockTown:
		return "town_recovery"
	case currentPetGrowthClockDungeon:
		return "dungeon_decay"
	default:
		return "stopped"
	}
}

func (s *Service) currentPetGrowthOwner() (*dnfpet.GrowthOwner, *dnfpet.PVFCatalog, dnfrepo.Group, error) {
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.CharacterPets == nil || repositories.Pet == nil {
		return nil, nil, dnfrepo.Group{}, dnfpet.ErrPetGrowthTransaction
	}
	catalog, err := s.currentPetPVFCatalog()
	if err != nil {
		return nil, nil, dnfrepo.Group{}, err
	}
	engine, err := dnfpet.NewPetGrowthEngine(catalog)
	if err != nil {
		return nil, nil, dnfrepo.Group{}, err
	}
	owner, err := dnfpet.NewGrowthOwner(repositories, engine)
	if err != nil {
		return nil, nil, dnfrepo.Group{}, err
	}
	return owner, catalog, repositories, nil
}

func (s *Service) switchCurrentPetGrowthClock(
	session *gameSession,
	mode currentPetGrowthClockMode,
	now time.Time,
	source string,
) error {
	if session == nil || session.selectedCharacterID == 0 || mode == currentPetGrowthClockStopped {
		return nil
	}
	if now.IsZero() {
		now = s.gameplayNow()
	}
	session.petGrowth.mu.Lock()
	defer session.petGrowth.mu.Unlock()
	if session.petGrowth.mode == mode && session.petGrowth.characterID == session.selectedCharacterID && !session.petGrowth.anchor.IsZero() {
		return nil
	}
	if session.petGrowth.mode != currentPetGrowthClockStopped && !session.petGrowth.anchor.IsZero() {
		if err := s.settleCurrentPetGrowthClockLocked(session, now, source+"_before_mode_switch"); err != nil {
			return err
		}
	}
	previous := session.petGrowth.mode
	session.petGrowth.mode = mode
	session.petGrowth.characterID = session.selectedCharacterID
	session.petGrowth.anchor = now
	session.petGrowth.generation++
	if err := s.armCurrentPetGrowthTickLocked(session); err != nil {
		session.petGrowth.mode = currentPetGrowthClockStopped
		session.petGrowth.characterID = 0
		session.petGrowth.anchor = time.Time{}
		return err
	}
	s.logGameEvent(session, "game-pet-growth-clock-switched",
		"char_id", session.petGrowth.characterID,
		"previous_mode", previous.String(),
		"mode", mode.String(),
		"source", source,
		"anchor_unix_ms", now.UnixMilli(),
		"timing_source", "current_exe_60s_decay_360s_recovery_and_attach_detach_state")
	return nil
}

func (s *Service) settleCurrentPetGrowthClock(session *gameSession, now time.Time, source string) error {
	if session == nil {
		return nil
	}
	if now.IsZero() {
		now = s.gameplayNow()
	}
	session.petGrowth.mu.Lock()
	defer session.petGrowth.mu.Unlock()
	if err := s.settleCurrentPetGrowthClockLocked(session, now, source); err != nil {
		return err
	}
	return s.armCurrentPetGrowthTickLocked(session)
}

func (s *Service) settleCurrentPetGrowthClockLocked(session *gameSession, now time.Time, source string) error {
	if session.petGrowth.mode == currentPetGrowthClockStopped || session.petGrowth.characterID == 0 || session.petGrowth.anchor.IsZero() {
		return nil
	}
	elapsed := now.Sub(session.petGrowth.anchor)
	if elapsed < 0 {
		return fmt.Errorf("pet growth clock moved backwards: %s", elapsed)
	}
	if elapsed == 0 {
		return nil
	}
	owner, catalog, repositories, err := s.currentPetGrowthOwner()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	command := dnfpet.PetElapsedCommand{
		SelectedCharacterID: session.petGrowth.characterID,
		Elapsed:             elapsed,
	}
	var result dnfpet.PetGrowthPersistenceResult
	if session.petGrowth.mode == currentPetGrowthClockDungeon {
		artifactItemIDs, err := currentEquippedPetArtifactItemIDs(ctx, repositories, session.petGrowth.characterID)
		if err != nil {
			return err
		}
		command.Modifiers, err = catalog.ResolveSatietyModifiers(artifactItemIDs)
		if err != nil {
			return err
		}
		result, err = owner.ApplyDungeonElapsed(ctx, command)
	} else {
		result, err = owner.ApplyTownElapsed(ctx, command)
	}
	if err != nil {
		return err
	}
	session.petGrowth.anchor = now
	s.logGameEvent(session, "game-pet-satiety-settled",
		"char_id", session.petGrowth.characterID,
		"mode", session.petGrowth.mode.String(),
		"source", source,
		"elapsed_ms", elapsed.Milliseconds(),
		"equipped", result.Equipped,
		"changed", result.Changed,
		"pet_key", result.PetKey,
		"before", result.Before.Satiety,
		"after", result.After.Satiety,
		"before_micros", result.Before.SatietyMicros,
		"after_micros", result.After.SatietyMicros,
		"visible_delta", result.SatietyDelta,
		"formula_source", "current_exe_60s_decay_360s_recovery_runtime_constants")
	return nil
}

func (s *Service) armCurrentPetGrowthTickLocked(session *gameSession) error {
	if s == nil || session == nil || session.petGrowth.mode == currentPetGrowthClockStopped ||
		session.petGrowth.characterID == 0 {
		return nil
	}
	queue := s.ensureGameplayTimeQueue()
	if queue == nil {
		return errGameplayTimeQueueUnavailable
	}
	delay := currentPetGrowthDungeonTick
	if session.petGrowth.mode == currentPetGrowthClockTown {
		delay = currentPetGrowthTownTick
	}
	timerName := fmt.Sprintf("dnf-pet-growth:%s", session.connID)
	generation := session.petGrowth.generation
	characterID, characterGeneration, err := gameSessionCharacterEventIdentity(session, session.petGrowth.characterID)
	if err != nil {
		return err
	}
	session.petGrowth.timerName = timerName
	return queue.ScheduleAfter(timerName, delay, func(time.Time) {
		err := s.postGameSessionCharacterEvent(
			session,
			"pet-growth-timequeue-tick",
			characterID,
			characterGeneration,
			func() error {
				s.runCurrentPetGrowthTick(session, generation)
				return nil
			},
		)
		if err != nil && !isClosedGameSessionEventError(err) {
			s.logPacketEvent("game-session-event-submit-failed",
				"conn_id", session.connID,
				"source", "pet-growth-timequeue-tick",
				"char_id", characterID,
				"character_generation", characterGeneration,
				"error", err)
		}
	})
}

func (s *Service) runCurrentPetGrowthTick(session *gameSession, generation uint64) {
	if s == nil || session == nil {
		return
	}
	now := s.gameplayNow()
	session.petGrowth.mu.Lock()
	defer session.petGrowth.mu.Unlock()
	if session.petGrowth.mode == currentPetGrowthClockStopped ||
		session.petGrowth.generation != generation ||
		session.petGrowth.characterID == 0 {
		return
	}
	if err := s.settleCurrentPetGrowthClockLocked(session, now, "timequeue_tick"); err != nil {
		s.logGameEvent(session, "game-pet-growth-timequeue-tick-deferred",
			"char_id", session.petGrowth.characterID,
			"mode", session.petGrowth.mode.String(),
			"generation", generation,
			"error", err)
	}
	if err := s.armCurrentPetGrowthTickLocked(session); err != nil {
		s.logGameEvent(session, "game-pet-growth-timequeue-rearm-deferred",
			"char_id", session.petGrowth.characterID,
			"mode", session.petGrowth.mode.String(),
			"generation", generation,
			"error", err)
	}
}

func currentEquippedPetArtifactItemIDs(ctx context.Context, repositories dnfrepo.Group, characterID uint16) ([]int64, error) {
	if repositories.Pet == nil || characterID == 0 {
		return nil, nil
	}
	record, found, err := repositories.Pet.Load(ctx, strconv.Itoa(int(characterID)))
	if err != nil || !found {
		return nil, err
	}
	itemIDs := make([]int64, 0, 3)
	for _, kind := range []string{"red", "blue", "green"} {
		artifact, ok := record.Artifacts[kind]
		if ok && artifact.ItemID > 0 && artifact.Count > 0 {
			itemIDs = append(itemIDs, artifact.ItemID)
		}
	}
	return itemIDs, nil
}

func currentPetMoveTouchesGrowthState(body []byte) bool {
	if len(body) != 28 || body[0] != 7 || (body[11] != 3 && body[11] != 17) {
		return false
	}
	target := int16(binary.LittleEndian.Uint16(body[12:14]))
	return target >= 26 && target <= 29
}

func (s *Service) stopCurrentPetGrowthClock(session *gameSession, source string) {
	if session == nil {
		return
	}
	now := s.gameplayNow()
	session.petGrowth.mu.Lock()
	err := s.settleCurrentPetGrowthClockLocked(session, now, source+"_final_settlement")
	previousMode := session.petGrowth.mode
	characterID := session.petGrowth.characterID
	timerName := session.petGrowth.timerName
	session.petGrowth.generation++
	session.petGrowth.mode = currentPetGrowthClockStopped
	session.petGrowth.characterID = 0
	session.petGrowth.anchor = time.Time{}
	session.petGrowth.timerName = ""
	session.petGrowth.mu.Unlock()
	if timerName != "" {
		if queue := s.ensureGameplayTimeQueue(); queue != nil {
			queue.Cancel(timerName)
		}
	}
	if err != nil {
		s.logGameEvent(session, "game-pet-growth-clock-stop-settlement-failed",
			"char_id", characterID,
			"mode", previousMode.String(),
			"source", source,
			"error", err)
	}
}

func (s *Service) awardCurrentPetRoomClearLocked(
	session *gameSession,
	runtime *runtimeDungeonState,
	scene worldmap.DungeonRoomScene,
	source string,
) {
	if session == nil || runtime == nil || session.selectedCharacterID == 0 || runtime.lifecycleToken == 0 {
		return
	}
	now := s.gameplayNow()
	if err := s.settleCurrentPetGrowthClock(session, now, source+"_before_experience"); err != nil {
		s.logGameEvent(session, "game-pet-room-clear-growth-deferred",
			"char_id", session.selectedCharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"room", scene.Coordinate.String(),
			"source", source,
			"reason", "satiety_settlement_failed",
			"error", err)
		return
	}
	owner, _, repositories, err := s.currentPetGrowthOwner()
	if err != nil {
		s.logGameEvent(session, "game-pet-room-clear-growth-deferred", "source", source, "reason", "growth_owner_unavailable", "error", err)
		return
	}
	clearToken := fmt.Sprintf(
		"pet-room:%s:%d:%d:%d:%d:%s",
		session.connID,
		runtime.startedAt.UnixNano(),
		runtime.lifecycleToken,
		runtime.Dungeon.ID,
		runtime.MazeIndex,
		scene.Coordinate.String(),
	)
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	result, err := owner.ApplyDungeonClear(ctx, dnfpet.DungeonClearGrowthCommand{
		SelectedCharacterID: session.selectedCharacterID,
		ClearToken:          clearToken,
		ConsumedFatigue:     1,
		ApplyEvolution:      false,
	})
	if err != nil {
		s.logGameEvent(session, "game-pet-room-clear-growth-deferred",
			"char_id", session.selectedCharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"room", scene.Coordinate.String(),
			"source", source,
			"reason", "growth_transaction_failed",
			"error", err)
		return
	}
	if result.Replayed || result.ExperienceGained == 0 {
		s.logGameEvent(session, "game-pet-room-clear-growth-committed",
			"char_id", session.selectedCharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"room", scene.Coordinate.String(),
			"source", source,
			"pet_key", result.PetKey,
			"equipped", result.Equipped,
			"replayed", result.Replayed,
			"experience_gain", result.ExperienceGained,
			"notification_sent", false,
			"domain_source", "servers4a12_room_clear_reference_consumed_fatigue_one",
			"evolution", "disabled_until_current_exe_wire_closes")
		return
	}
	entry, err := currentPetGrowthEntry(ctx, repositories, session.selectedCharacterID, result.PetKey)
	if err != nil {
		s.logGameEvent(session, "game-pet-room-clear-growth-notification-deferred", "source", source, "pet_key", result.PetKey, "error", err)
		return
	}
	body, err := dnfpet.BuildCreatureGrowthBody(entry)
	if err != nil {
		s.logGameEvent(session, "game-pet-room-clear-growth-notification-deferred", "source", source, "pet_key", result.PetKey, "error", err)
		return
	}
	if err := s.sendGameUpperRawClass(session, currentCreatureGrowthMsgID, body, 0); err != nil {
		s.logGameEvent(session, "game-pet-room-clear-growth-notification-deferred", "source", source, "pet_key", result.PetKey, "error", err)
		return
	}
	s.logGameEvent(session, "game-pet-room-clear-growth-committed",
		"char_id", session.selectedCharacterID,
		"dungeon_id", runtime.Dungeon.ID,
		"room", scene.Coordinate.String(),
		"source", source,
		"pet_key", result.PetKey,
		"experience_before", result.Before.Experience,
		"experience_after", result.After.Experience,
		"experience_gain", result.ExperienceGained,
		"level_before", result.Before.Level,
		"level_after", result.After.Level,
		"msg_id", currentCreatureGrowthMsgID,
		"classification", 0,
		"body_len", len(body),
		"body_source", "current_exe_sub_1D5AF60_scene_op102",
		"domain_source", "servers4a12_room_clear_reference_consumed_fatigue_one",
		"evolution", "disabled_until_current_exe_wire_closes")
}

func currentPetGrowthEntry(ctx context.Context, repositories dnfrepo.Group, characterID uint16, petKey string) (dnfrepo.PetEntry, error) {
	if repositories.Pet == nil || characterID == 0 || petKey == "" {
		return dnfrepo.PetEntry{}, fmt.Errorf("pet growth entry owner is unavailable")
	}
	record, found, err := repositories.Pet.Load(ctx, strconv.Itoa(int(characterID)))
	if err != nil {
		return dnfrepo.PetEntry{}, err
	}
	if !found {
		return dnfrepo.PetEntry{}, fmt.Errorf("pet record was not found")
	}
	entry, found := record.Entries[petKey]
	if !found {
		return dnfrepo.PetEntry{}, fmt.Errorf("pet entry %s was not found", petKey)
	}
	return entry, nil
}
