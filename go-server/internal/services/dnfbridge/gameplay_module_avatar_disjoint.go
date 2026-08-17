package dnfbridge

import (
	"context"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

func avatarDisjointGameplayModule() gameplayModuleDefinition {
	opcode := uint16(dnfenum.CmdPacketDisjointAvatar)
	return gameplayModuleDefinition{
		Name: "avatar-disjoint",
		LegacyHandlers: map[uint16]gameplayHandler{
			opcode: func(service *Service, session *gameSession, request gameplayRequest) error {
				return service.handleCurrentAvatarDisjoint(session, request.Body)
			},
		},
		UpperHandlers: map[uint16]gameplayHandler{
			opcode: func(service *Service, session *gameSession, request gameplayRequest) error {
				if !requireDefaultGameplayClassification(service, session, request, "game-avatar-disjoint-blocked", "current_exe_op202_command_class_mismatch") {
					return nil
				}
				return service.handleCurrentAvatarDisjoint(session, request.Body)
			},
		},
		LegacyNormalizers: map[uint16]gameplayLegacyNormalizer{
			opcode: func(body []byte) []byte {
				if len(body) == currentAvatarDisjointRequestFullSize+4 {
					return append([]byte(nil), body[:len(body)-4]...)
				}
				return body
			},
		},
	}
}

func (s *Service) handleCurrentAvatarDisjoint(session *gameSession, body []byte) error {
	request, err := parseCurrentAvatarDisjointRequest(body)
	if err != nil {
		s.logGameEvent(session, "game-avatar-disjoint-rejected", "body_len", len(body), "reason", err)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketDisjointAvatar), 4)
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	catalog, err := s.currentPVFItemCatalog()
	if err == nil {
		err = s.commitCurrentAvatarDisjoint(ctx, session, catalog, request)
	}
	if err != nil {
		s.logGameEvent(session, "game-avatar-disjoint-rejected", "slot", request.SourceSlot, "reason", err)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketDisjointAvatar), 4)
	}
	return nil
}

func (s *Service) commitCurrentAvatarDisjoint(ctx context.Context, session *gameSession, catalog *pvfDungeonDropCatalog, request currentAvatarDisjointRequest) error {
	if s == nil || session == nil || catalog == nil {
		return errCurrentDisjointUnavailable
	}
	config, err := loadCurrentDisjointPVFConfig(catalog.source)
	if err != nil {
		return err
	}
	result, err := s.commitCurrentDisjoint(ctx, session, catalog, request.SourceSlot, currentAvatarInventoryListType, func(def dungeonDropItemDefinition, document *dnfpvf.Document) ([]currentDisjointReward, error) {
		if request.ExpectedItemID != 0 && def.ItemID != request.ExpectedItemID {
			return nil, errCurrentDisjointSourceInvalid
		}
		if def.Kind != dungeonDropItemEquipment || !currentDisjointDocumentIsAvatar(document) {
			return nil, errCurrentDisjointSourceInvalid
		}
		return currentAvatarDisjointRewards(config, document)
	})
	if err != nil {
		return err
	}
	if err := s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketDisjointAvatar), buildCurrentAvatarDisjointSuccessBody(request, result.Rewards), dnfproto.DefaultChannelClassification); err != nil {
		return err
	}
	s.logGameEvent(session, "game-avatar-disjoint-committed", "slot", request.SourceSlot, "expected_item", request.ExpectedItemID, "reward_rows", len(result.Rewards), "popup", "current_exe_op202")
	return nil
}
