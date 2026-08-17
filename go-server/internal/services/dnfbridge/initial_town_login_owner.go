package dnfbridge

import (
	"context"
	"fmt"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnftown "longheng.io/server/internal/modules/dnf/town"
)

func (s *Service) currentCharacterListLoginTransition(
	ctx context.Context,
	session *gameSession,
	characterID uint16,
	character dnfrepo.CharacterRecord,
) (dnfrepo.CharacterRecord, byte, byte, currentSceneTransitionRow, string, error) {
	if characterID == 0 || character.Stats == nil {
		return dnfrepo.CharacterRecord{}, 0, 0, currentSceneTransitionRow{}, "", fmt.Errorf("selected character login location is unavailable")
	}
	area, found := s.townArea(int64(newCharacterInitialTownID), int64(newCharacterInitialAreaID))
	if !found || area.MapPath == "" || area.Gate == nil {
		return dnfrepo.CharacterRecord{}, 0, 0, currentSceneTransitionRow{}, "", fmt.Errorf(
			"runtime PVF Seria login area %d/%d has no gate",
			newCharacterInitialTownID,
			newCharacterInitialAreaID,
		)
	}
	if area.Gate.X < -1<<15 || area.Gate.X > 1<<15-1 || area.Gate.Y < -1<<15 || area.Gate.Y > 1<<15-1 {
		return dnfrepo.CharacterRecord{}, 0, 0, currentSceneTransitionRow{}, "", fmt.Errorf(
			"runtime PVF Seria gate (%d,%d) is outside i16",
			area.Gate.X,
			area.Gate.Y,
		)
	}

	repositories, ok := s.repositoryGroup()
	if !ok {
		return dnfrepo.CharacterRecord{}, 0, 0, currentSceneTransitionRow{}, "", fmt.Errorf("character repository is unavailable for Seria login location")
	}
	owner, err := dnftown.NewOwner(repositories)
	if err != nil {
		return dnfrepo.CharacterRecord{}, 0, 0, currentSceneTransitionRow{}, "", fmt.Errorf("character repository is unavailable for Seria login location: %w", err)
	}

	recordCharacterID := character.CharacterID
	if recordCharacterID == "" {
		recordCharacterID = fmt.Sprintf("%d", characterID)
	}
	prevVillageOrigin := currentTownPositionSnapshot{}
	prevVillageOriginSource := ""
	result, err := owner.ApplyLoginLocation(ctx, dnftown.LoginLocationCommand{
		AccountID:   s.accountIDForSession(session),
		CharacterID: recordCharacterID,
		Project: func(current *dnfrepo.CharacterRecord) (bool, error) {
			if current == nil || current.Stats == nil {
				return false, fmt.Errorf("selected character login location is unavailable")
			}
			if origin, found := currentTownPrevVillageOriginLocked(session, characterID, *current); found {
				if _, persistable := s.currentTownPrevVillageOriginIsPersistable(origin); persistable {
					prevVillageOrigin = origin
					prevVillageOriginSource = "character_list_login_previous_non_seria_location"
				}
			}
			if !prevVillageOrigin.PositionValid {
				if origin, found := currentTownPrevVillageOriginFromPersistedStats(characterID, *current); found {
					if _, persistable := s.currentTownPrevVillageOriginIsPersistable(origin); persistable {
						prevVillageOrigin = origin
						prevVillageOriginSource = "character_list_login_existing_persisted_prev_village"
					}
				}
			}

			values := map[string]int64{
				"town_id":    newCharacterInitialTownID,
				"area_id":    newCharacterInitialAreaID,
				"pos_x":      area.Gate.X,
				"pos_y":      area.Gate.Y,
				"direction":  newCharacterInitialDirection,
				"area_state": newCharacterInitialAreaState,
			}
			changed := false
			for key, value := range values {
				if valueBefore, exists := current.Stats[key]; !exists || valueBefore != value {
					current.Stats[key] = value
					changed = true
				}
			}
			if prevVillageOrigin.PositionValid && currentTownPrevVillageApplyOriginStats(current.Stats, prevVillageOrigin) {
				changed = true
			}
			return changed, nil
		},
	})
	if err != nil {
		return dnfrepo.CharacterRecord{}, 0, 0, currentSceneTransitionRow{}, "", fmt.Errorf("persist runtime-PVF Seria login location: %w", err)
	}

	if prevVillageOrigin.PositionValid {
		session.townPrevVillageSnapshot = prevVillageOrigin
		s.logTownPrevVillageOriginBound(session, prevVillageOrigin, prevVillageOriginSource)
	}
	s.logGameEvent(session, "game-character-list-login-seria-location",
		"char_id", characterID,
		"town_id", newCharacterInitialTownID,
		"area_id", newCharacterInitialAreaID,
		"position_x", area.Gate.X,
		"position_y", area.Gate.Y,
		"direction", newCharacterInitialDirection,
		"area_state", newCharacterInitialAreaState,
		"map_path", area.MapPath,
		"prev_village_town_id", prevVillageOrigin.TownID,
		"prev_village_area_id", prevVillageOrigin.AreaID,
		"prev_village_position_x", prevVillageOrigin.PositionX,
		"prev_village_position_y", prevVillageOrigin.PositionY,
		"prev_village_bound", prevVillageOrigin.PositionValid,
		"prev_village_source", prevVillageOriginSource,
		"persisted", result.Changed,
		"source", "runtime_pvf_town_38_area_1_gate")
	return s.currentInitialTownTransition(ctx, session, characterID, result.Character)
}
