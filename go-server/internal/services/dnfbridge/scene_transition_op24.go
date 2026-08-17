package dnfbridge

import (
	"fmt"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const currentSceneTransitionMsgID = uint16(dnfenum.CmdPacketSetItemtradeState)

type currentSceneTransitionRow struct {
	ObjectOrResourceKey uint16
	Value1              uint16
	Value2              uint16
	Value3              byte
	Value4              byte
}

func buildCurrentSceneTransitionBody(townID byte, areaID byte, rows []currentSceneTransitionRow) ([]byte, error) {
	if len(rows) > int(^uint16(0)) {
		return nil, fmt.Errorf("current scene transition row count %d exceeds u16", len(rows))
	}

	var writer packetWriter
	writer.writeByte(townID)
	writer.writeByte(areaID)
	writer.writeUint16(uint16(len(rows)))
	for _, row := range rows {
		writer.writeUint16(row.ObjectOrResourceKey)
		writer.writeUint16(row.Value1)
		writer.writeUint16(row.Value2)
		writer.writeByte(row.Value3)
		writer.writeByte(row.Value4)
	}
	return writer.bytes(), nil
}

func currentSceneTransitionLocation(character dnfrepo.CharacterRecord, hasCharacter bool) (byte, byte, error) {
	if !hasCharacter {
		return 0, 0, fmt.Errorf("selected character record not found")
	}
	if character.Stats == nil {
		return 0, 0, fmt.Errorf("selected character stats not loaded")
	}
	townID, hasTownID := character.Stats["town_id"]
	if !hasTownID {
		return 0, 0, fmt.Errorf("selected character town_id not loaded")
	}
	areaID, hasAreaID := character.Stats["area_id"]
	if !hasAreaID {
		return 0, 0, fmt.Errorf("selected character area_id not loaded")
	}
	if townID < 0 || townID > int64(^byte(0)) {
		return 0, 0, fmt.Errorf("selected character town_id %d is outside u8", townID)
	}
	if areaID < 0 || areaID > int64(^byte(0)) {
		return 0, 0, fmt.Errorf("selected character area_id %d is outside u8", areaID)
	}
	return byte(townID), byte(areaID), nil
}

func (s *Service) sendCurrentSceneTransitionOp24(session *gameSession, character dnfrepo.CharacterRecord, hasCharacter bool, source string) error {
	townID, areaID, err := currentSceneTransitionLocation(character, hasCharacter)
	if err != nil {
		s.logGameEvent(session, "game-upper-current-scene-transition-op24-deferred",
			"source", source,
			"char_id", session.selectedCharacterID,
			"msg_id", currentSceneTransitionMsgID,
			"reason", err.Error())
		return nil
	}

	rows := []currentSceneTransitionRow(nil)
	body, err := buildCurrentSceneTransitionBody(townID, areaID, rows)
	if err != nil {
		return err
	}
	s.logGameEvent(session, "game-upper-current-scene-transition-op24-send",
		"source", source,
		"char_id", session.selectedCharacterID,
		"msg_id", currentSceneTransitionMsgID,
		"classification", 0,
		"town_id", townID,
		"area_id", areaID,
		"row_count", len(rows),
		"body_len", len(body),
		"body_source", "current_exe_sub_1D901D0_typed",
		"row_source", "no_current_owned_resource_rows")
	return s.sendGameUpperRawClass(session, currentSceneTransitionMsgID, body, 0)
}
