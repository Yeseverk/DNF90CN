package dnfbridge

import (
	"context"
	"fmt"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func (s *Service) currentInitialTownTransition(
	ctx context.Context,
	session *gameSession,
	characterID uint16,
	character dnfrepo.CharacterRecord,
) (dnfrepo.CharacterRecord, byte, byte, currentSceneTransitionRow, string, error) {
	return s.currentPersistedTownTransition(ctx, session, characterID, character, "selected_character_persisted_location_read_only")
}

func (s *Service) currentChannelReconnectTownTransition(
	ctx context.Context,
	session *gameSession,
	characterID uint16,
	character dnfrepo.CharacterRecord,
) (dnfrepo.CharacterRecord, byte, byte, currentSceneTransitionRow, string, error) {
	return s.currentPersistedTownTransition(ctx, session, characterID, character, "channel_reconnect_persisted_location_read_only")
}

func (s *Service) currentPersistedTownTransition(
	ctx context.Context,
	session *gameSession,
	characterID uint16,
	character dnfrepo.CharacterRecord,
	locationSource string,
) (dnfrepo.CharacterRecord, byte, byte, currentSceneTransitionRow, string, error) {
	townID, err := requiredCurrentInitialTownU8(character, "town_id")
	if err != nil {
		return dnfrepo.CharacterRecord{}, 0, 0, currentSceneTransitionRow{}, "", err
	}
	areaID, err := requiredCurrentInitialTownU8(character, "area_id")
	if err != nil {
		return dnfrepo.CharacterRecord{}, 0, 0, currentSceneTransitionRow{}, "", err
	}
	positionX, err := requiredCurrentInitialTownCoordinate(character, "pos_x")
	if err != nil {
		return dnfrepo.CharacterRecord{}, 0, 0, currentSceneTransitionRow{}, "", err
	}
	positionY, err := requiredCurrentInitialTownCoordinate(character, "pos_y")
	if err != nil {
		return dnfrepo.CharacterRecord{}, 0, 0, currentSceneTransitionRow{}, "", err
	}
	direction, err := requiredCurrentInitialTownU8(character, "direction")
	if err != nil {
		return dnfrepo.CharacterRecord{}, 0, 0, currentSceneTransitionRow{}, "", err
	}
	areaState, err := requiredCurrentInitialTownU8(character, "area_state")
	if err != nil {
		return dnfrepo.CharacterRecord{}, 0, 0, currentSceneTransitionRow{}, "", err
	}
	area, found := s.townArea(int64(townID), int64(areaID))
	if !found || area.MapPath == "" {
		return dnfrepo.CharacterRecord{}, 0, 0, currentSceneTransitionRow{}, "", fmt.Errorf(
			"persisted town %d area %d has no runtime PVF map path",
			townID, areaID,
		)
	}
	if locationSource == "" {
		locationSource = "persisted_location_read_only"
	}
	s.logGameEvent(session, "game-initial-town-persisted-location-used",
		"char_id", characterID,
		"town_id", townID,
		"area_id", areaID,
		"position_x", int16(positionX),
		"position_y", int16(positionY),
		"direction", direction,
		"area_state", areaState,
		"map_path", area.MapPath,
		"need_quest_unlock_allowed", false,
		"source", locationSource)
	return character, townID, areaID, currentSceneTransitionRow{
		ObjectOrResourceKey: currentSceneActorObjectKey(characterID),
		Value1:              positionX,
		Value2:              positionY,
		Value3:              direction,
		Value4:              areaState,
	}, area.MapPath, nil
}

func requiredCurrentInitialTownCoordinate(character dnfrepo.CharacterRecord, key string) (uint16, error) {
	if character.Stats == nil {
		return 0, fmt.Errorf("selected character stats not loaded")
	}
	value, found := character.Stats[key]
	if !found {
		return 0, fmt.Errorf("selected character %s not loaded", key)
	}
	if value < -1<<15 || value > 1<<15-1 {
		return 0, fmt.Errorf("selected character %s %d is outside i16", key, value)
	}
	return uint16(int16(value)), nil
}

func requiredCurrentInitialTownU8(character dnfrepo.CharacterRecord, key string) (byte, error) {
	if character.Stats == nil {
		return 0, fmt.Errorf("selected character stats not loaded")
	}
	value, found := character.Stats[key]
	if !found {
		return 0, fmt.Errorf("selected character %s not loaded", key)
	}
	if value < 0 || value > int64(^byte(0)) {
		return 0, fmt.Errorf("selected character %s %d is outside u8", key, value)
	}
	return byte(value), nil
}
