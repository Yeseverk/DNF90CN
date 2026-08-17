package dnfbridge

import (
	"context"
	"strconv"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const (
	currentTownPrevVillageTownStat      = "prev_village_town_id"
	currentTownPrevVillageAreaStat      = "prev_village_area_id"
	currentTownPrevVillagePosXStat      = "prev_village_pos_x"
	currentTownPrevVillagePosYStat      = "prev_village_pos_y"
	currentTownPrevVillageDirectionStat = "prev_village_direction"
)

func townMapPathLooksLikeSeria(mapPath string) bool {
	normalizedPath := strings.ToLower(strings.ReplaceAll(mapPath, "\\", "/"))
	return strings.Contains(normalizedPath, "seria")
}

func currentTownPrevVillageOriginLocked(
	session *gameSession,
	characterID uint16,
	character dnfrepo.CharacterRecord,
) (currentTownPositionSnapshot, bool) {
	if session == nil || characterID == 0 {
		return currentTownPositionSnapshot{}, false
	}
	townID, areaID, err := currentSceneTransitionLocation(character, true)
	if err != nil {
		return currentTownPositionSnapshot{}, false
	}
	if snapshot := session.townPositionSnapshot; snapshot.CharacterID == characterID &&
		snapshot.TownID == townID &&
		snapshot.AreaID == areaID &&
		snapshot.PositionValid {
		return snapshot, true
	}
	posX, okX := character.Stats["pos_x"]
	posY, okY := character.Stats["pos_y"]
	direction, okDirection := character.Stats["direction"]
	if !okX || !okY || !okDirection ||
		posX < 0 || posX > int64(^uint16(0)) ||
		posY < 0 || posY > int64(^uint16(0)) ||
		direction < 0 || direction > int64(^byte(0)) {
		return currentTownPositionSnapshot{}, false
	}
	return currentTownPositionSnapshot{
		CharacterID:   characterID,
		TownID:        townID,
		AreaID:        areaID,
		PositionX:     uint16(posX),
		PositionY:     uint16(posY),
		MovementCode:  byte(direction),
		PositionValid: true,
	}, true
}

func currentTownPrevVillageOriginFromPersistedStats(
	characterID uint16,
	character dnfrepo.CharacterRecord,
) (currentTownPositionSnapshot, bool) {
	if characterID == 0 || character.Stats == nil {
		return currentTownPositionSnapshot{}, false
	}
	townID, okTown := character.Stats[currentTownPrevVillageTownStat]
	areaID, okArea := character.Stats[currentTownPrevVillageAreaStat]
	posX, okX := character.Stats[currentTownPrevVillagePosXStat]
	posY, okY := character.Stats[currentTownPrevVillagePosYStat]
	direction, okDirection := character.Stats[currentTownPrevVillageDirectionStat]
	if !okTown || !okArea || !okX || !okY || !okDirection ||
		townID < 0 || townID > int64(^byte(0)) ||
		areaID < 0 || areaID > int64(^byte(0)) ||
		posX < 0 || posX > int64(^uint16(0)) ||
		posY < 0 || posY > int64(^uint16(0)) ||
		direction < 0 || direction > int64(^byte(0)) {
		return currentTownPositionSnapshot{}, false
	}
	return currentTownPositionSnapshot{
		CharacterID:   characterID,
		TownID:        byte(townID),
		AreaID:        byte(areaID),
		PositionX:     uint16(posX),
		PositionY:     uint16(posY),
		MovementCode:  byte(direction),
		PositionValid: true,
	}, true
}

func currentTownPrevVillageApplyOriginStats(stats map[string]int64, origin currentTownPositionSnapshot) bool {
	if stats == nil || origin.CharacterID == 0 || !origin.PositionValid {
		return false
	}
	values := map[string]int64{
		currentTownPrevVillageTownStat:      int64(origin.TownID),
		currentTownPrevVillageAreaStat:      int64(origin.AreaID),
		currentTownPrevVillagePosXStat:      int64(origin.PositionX),
		currentTownPrevVillagePosYStat:      int64(origin.PositionY),
		currentTownPrevVillageDirectionStat: int64(origin.MovementCode),
	}
	changed := false
	for key, value := range values {
		if current, ok := stats[key]; !ok || current != value {
			stats[key] = value
			changed = true
		}
	}
	return changed
}

func (s *Service) currentTownPrevVillageOriginMapPath(origin currentTownPositionSnapshot) (string, bool) {
	if s == nil || origin.CharacterID == 0 || !origin.PositionValid {
		return "", false
	}
	area, found := s.townArea(int64(origin.TownID), int64(origin.AreaID))
	if !found || area.MapPath == "" {
		return "", false
	}
	return area.MapPath, true
}

func (s *Service) currentTownPrevVillageOriginIsPersistable(origin currentTownPositionSnapshot) (string, bool) {
	mapPath, ok := s.currentTownPrevVillageOriginMapPath(origin)
	if !ok || townMapPathLooksLikeSeria(mapPath) {
		return mapPath, false
	}
	return mapPath, true
}

func (s *Service) handleCurrentPrevVillage(session *gameSession, requestBody []byte) error {
	if session == nil {
		return nil
	}
	if len(requestBody) != 0 {
		s.logCurrentPrevVillageBlocked(session, currentTownPositionSnapshot{}, "current_exe_op1425_body_boundary_mismatch")
		return nil
	}
	characterID := session.selectedCharacterID
	if characterID == 0 {
		s.logCurrentPrevVillageBlocked(session, currentTownPositionSnapshot{}, "selected_character_missing")
		return nil
	}
	session.dungeon.mu.Lock()
	hasDungeonRuntime := session.dungeon.runtime != nil
	session.dungeon.mu.Unlock()
	if hasDungeonRuntime {
		s.logCurrentPrevVillageBlocked(session, currentTownPositionSnapshot{}, "active_dungeon_runtime_rejects_prev_village")
		return nil
	}
	session.townMu.Lock()
	readyCharacterID := session.townSceneReadyCharacterID
	origin := session.townPrevVillageSnapshot
	session.townMu.Unlock()
	if readyCharacterID != characterID {
		s.logCurrentPrevVillageBlocked(session, origin, "town_scene_player_state_not_finalized")
		return nil
	}

	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Character == nil {
		s.logCurrentPrevVillageBlocked(session, origin, "character_repository_unavailable")
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	characterKey := strconv.FormatUint(uint64(characterID), 10)
	character, found, err := repositories.Character.Load(ctx, characterKey)
	if err != nil {
		return err
	}
	if !found || character.Stats == nil {
		s.logCurrentPrevVillageBlocked(session, origin, "selected_character_location_missing")
		return nil
	}
	if origin.CharacterID != characterID || !origin.PositionValid {
		if persistedOrigin, ok := currentTownPrevVillageOriginFromPersistedStats(characterID, character); ok {
			origin = persistedOrigin
			session.townMu.Lock()
			session.townPrevVillageSnapshot = origin
			session.townMu.Unlock()
			s.logTownPrevVillageOriginBound(session, origin, "current_exe_op1425_restored_from_persisted_prev_village")
		}
	}
	if origin.CharacterID != characterID || !origin.PositionValid {
		s.logCurrentPrevVillageBlocked(session, origin, "prev_village_origin_missing")
		return nil
	}
	currentTownID, currentAreaID, err := currentSceneTransitionLocation(character, true)
	if err != nil {
		s.logCurrentPrevVillageBlocked(session, origin, err.Error())
		return nil
	}
	currentArea, found := s.townArea(int64(currentTownID), int64(currentAreaID))
	if !found || !townMapPathLooksLikeSeria(currentArea.MapPath) {
		s.logCurrentPrevVillageBlocked(session, origin, "current_area_is_not_seria_room")
		return nil
	}
	targetArea, found := s.townArea(int64(origin.TownID), int64(origin.AreaID))
	if !found {
		s.logCurrentPrevVillageBlocked(session, origin, "prev_village_target_area_missing_from_runtime_pvf")
		return nil
	}
	areaState, ok := character.Stats["area_state"]
	if !ok || areaState < 0 || areaState > int64(^byte(0)) {
		s.logCurrentPrevVillageBlocked(session, origin, "selected_character_area_state_missing_or_invalid")
		return nil
	}
	direction := origin.MovementCode

	previous := dnfrepo.CloneCharacter(character)
	character = dnfrepo.CloneCharacter(character)
	character.Stats["town_id"] = int64(origin.TownID)
	character.Stats["area_id"] = int64(origin.AreaID)
	character.Stats["pos_x"] = int64(origin.PositionX)
	character.Stats["pos_y"] = int64(origin.PositionY)
	character.Stats["direction"] = int64(direction)
	character.UpdatedAt = time.Now().UTC()
	if err := dnfrepo.SaveCharacterFields(ctx, repositories.Character, character, dnfrepo.CharacterFieldStats); err != nil {
		return err
	}

	row := currentSceneTransitionRow{
		ObjectOrResourceKey: currentSceneActorObjectKey(characterID),
		Value1:              origin.PositionX,
		Value2:              origin.PositionY,
		Value3:              byte(direction),
		Value4:              byte(areaState),
	}
	response, err := buildCurrentSceneTransitionBody(origin.TownID, origin.AreaID, []currentSceneTransitionRow{row})
	if err != nil {
		return err
	}
	rollbackLocation := func(writeErr error) error {
		rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), createWriteTimeout)
		rollbackErr := dnfrepo.SaveCharacterFields(rollbackCtx, repositories.Character, previous, dnfrepo.CharacterFieldStats)
		rollbackCancel()
		if rollbackErr != nil {
			s.logGameEvent(session, "game-town-prev-village-rollback-failed",
				"char_id", characterID,
				"town_id", origin.TownID,
				"area_id", origin.AreaID,
				"write_error", writeErr,
				"rollback_error", rollbackErr)
		}
		return writeErr
	}
	s.logGameEvent(session, "game-town-prev-village-op24-send",
		"char_id", characterID,
		"request_msg_id", uint16(1425),
		"response_msg_id", currentSceneTransitionMsgID,
		"town_id", origin.TownID,
		"area_id", origin.AreaID,
		"position_x", origin.PositionX,
		"position_y", origin.PositionY,
		"direction", int64(direction),
		"area_state", areaState,
		"map_path", targetArea.MapPath,
		"body_len", len(response),
		"body_source", "current_exe_sub_1D901D0_typed_from_legacy_op1425_prev_village")
	if err := s.sendGameUpperRawClass(session, currentSceneTransitionMsgID, response, 0); err != nil {
		return rollbackLocation(err)
	}

	session.townMu.Lock()
	setCurrentTownPositionSceneLocked(session, characterID, origin.TownID, origin.AreaID)
	session.townPrevVillageSnapshot = currentTownPositionSnapshot{}
	session.initialTownRouteCharacterID = 0
	session.initialTownRouteStage = currentInitialTownRouteIdle
	session.initialTownLocationNotificationsSent = false
	session.initialTownQuestSnapshotsSent = false
	session.currentFinishLoadingStateSent = false
	session.currentFinishLoadingCompletionSent = false
	session.postFinishLoadingPlayerStateSent = false
	session.initialTownLegacySceneReadyAccepted = false
	session.initialTownAdventureOverheadRefreshSent = false
	session.initialTownCombatPowerAffixesSent = false
	session.enterSelectDungeonSent = false
	session.enterSelectDungeonAckSent = false
	session.enterSelectDungeonContextSent = false
	session.townSceneReadyCharacterID = characterID
	session.townMu.Unlock()
	s.logGameEvent(session, "game-town-prev-village-committed",
		"char_id", characterID,
		"town_id", origin.TownID,
		"area_id", origin.AreaID,
		"position_x", origin.PositionX,
		"position_y", origin.PositionY,
		"map_path", targetArea.MapPath,
		"finish_loading_state_rearmed", true)
	return nil
}

func (s *Service) logCurrentPrevVillageBlocked(session *gameSession, origin currentTownPositionSnapshot, reason string) {
	s.logGameEvent(session, "game-town-prev-village-blocked",
		"char_id", session.selectedCharacterID,
		"origin_character_id", origin.CharacterID,
		"origin_town_id", origin.TownID,
		"origin_area_id", origin.AreaID,
		"origin_position_x", origin.PositionX,
		"origin_position_y", origin.PositionY,
		"origin_position_valid", origin.PositionValid,
		"reason", reason)
}

func (s *Service) logTownPrevVillageOriginBound(session *gameSession, origin currentTownPositionSnapshot, source string) {
	s.logGameEvent(session, "game-town-prev-village-origin-bound",
		"char_id", session.selectedCharacterID,
		"origin_town_id", origin.TownID,
		"origin_area_id", origin.AreaID,
		"origin_position_x", origin.PositionX,
		"origin_position_y", origin.PositionY,
		"origin_position_valid", origin.PositionValid,
		"source", source)
}

func (s *Service) logTownPrevVillageOriginBindFailed(session *gameSession, characterID uint16, reason string) {
	s.logGameEvent(session, "game-town-prev-village-origin-bind-failed",
		"char_id", characterID,
		"reason", reason)
}
