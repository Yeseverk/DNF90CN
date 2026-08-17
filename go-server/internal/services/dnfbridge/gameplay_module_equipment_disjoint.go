package dnfbridge

import (
	"context"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

func equipmentDisjointGameplayModule() gameplayModuleDefinition {
	opcode := uint16(dnfenum.CmdPacketDisjointItem)
	return gameplayModuleDefinition{
		Name: "equipment-disjoint",
		LegacyHandlers: map[uint16]gameplayHandler{
			opcode: func(service *Service, session *gameSession, request gameplayRequest) error {
				return service.handleCurrentDisjointItem(session, request.Body)
			},
		},
		UpperHandlers: map[uint16]gameplayHandler{
			opcode: func(service *Service, session *gameSession, request gameplayRequest) error {
				if !requireDefaultGameplayClassification(service, session, request, "game-disjoint-item-blocked", "current_exe_op26_command_class_mismatch") {
					return nil
				}
				return service.handleCurrentDisjointItem(session, request.Body)
			},
		},
		LegacyNormalizers: map[uint16]gameplayLegacyNormalizer{
			opcode: func(body []byte) []byte {
				if len(body) == currentDisjointItemRequestSize+4 || len(body) == currentDisjointItemRequestSize+8 {
					return append([]byte(nil), body[:len(body)-4]...)
				}
				return body
			},
		},
	}
}

func (s *Service) handleCurrentDisjointItem(session *gameSession, body []byte) error {
	request, err := parseCurrentDisjointItemRequest(body)
	if err != nil {
		s.logGameEvent(session, "game-disjoint-item-rejected", "body_len", len(body), "reason", err)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketDisjointItem), 4)
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	catalog, err := s.currentPVFItemCatalog()
	if err == nil {
		err = s.commitCurrentDisjointItem(ctx, session, catalog, request)
	}
	if err != nil {
		s.logGameEvent(session, "game-disjoint-item-rejected", "slot", request.SourceSlot, "reason", err)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketDisjointItem), 4)
	}
	return nil
}

func (s *Service) commitCurrentDisjointItem(ctx context.Context, session *gameSession, catalog *pvfDungeonDropCatalog, request currentDisjointItemRequest) error {
	if s == nil || session == nil || catalog == nil {
		return errCurrentDisjointUnavailable
	}
	config, err := loadCurrentDisjointPVFConfig(catalog.source)
	if err != nil {
		return err
	}
	result, err := s.commitCurrentDisjoint(ctx, session, catalog, request.SourceSlot, 0, func(def dungeonDropItemDefinition, document *dnfpvf.Document) ([]currentDisjointReward, error) {
		if def.Kind != dungeonDropItemEquipment || currentDisjointDocumentIsAvatar(document) {
			return nil, errCurrentDisjointSourceInvalid
		}
		return currentEquipmentDisjointRewards(config, document)
	})
	if err != nil {
		return err
	}
	if err := s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketDisjointItem), buildCurrentDisjointItemSuccessBody(request, result.Rewards), dnfproto.DefaultChannelClassification); err != nil {
		return err
	}
	s.logGameEvent(session, "game-disjoint-item-committed", "slot", request.SourceSlot, "tool_slot", request.ToolSlot, "reward_rows", len(result.Rewards), "popup", "current_exe_op26")
	return nil
}
