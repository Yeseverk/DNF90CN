package dnfbridge

import (
	"context"
	"errors"
	"fmt"

	dnfsocketemblem "longheng.io/server/internal/modules/dnf/socketemblem"
)

func avatarSocketGameplayModule() gameplayModuleDefinition {
	opcode := uint16(currentAvatarSocketOpenOpcode)
	handler := func(service *Service, session *gameSession, request gameplayRequest) error {
		return service.handleCurrentAvatarSocketOpen(session, request.Body)
	}
	return gameplayModuleDefinition{
		Name:           "avatar-socket",
		LegacyHandlers: map[uint16]gameplayHandler{opcode: handler},
		UpperHandlers: map[uint16]gameplayHandler{
			opcode: defaultClassGameplayHandler(
				"game-current-avatar-socket-open-blocked",
				"current_exe_socket_command_class_mismatch",
				func(service *Service, session *gameSession, body []byte) error {
					return service.handleCurrentAvatarSocketOpen(session, body)
				},
			),
		},
		LegacyNormalizers: map[uint16]gameplayLegacyNormalizer{
			opcode: stripLegacySocketOpenTransportTrailer,
		},
	}
}

func (s *Service) handleCurrentAvatarSocketOpen(session *gameSession, body []byte) error {
	request, err := decodeCurrentSocketOpenRequest(body)
	if err != nil {
		s.logGameEvent(session, "game-current-avatar-socket-open-parse-failed", "body_len", len(body), "err", err)
		return s.sendCurrentSocketParseFailure(session, currentAvatarSocketOpenOpcode)
	}
	result, err := s.commitCurrentAvatarSocketOpen(session, request)
	if err != nil {
		s.logGameEvent(session, "game-current-avatar-socket-open-blocked",
			"target_slot", request.TargetSlot,
			"target_item_id", request.TargetItemID,
			"material_slot", request.MaterialSlot,
			"reason", err)
		return s.sendGameUpperFailure(session, currentAvatarSocketOpenOpcode, 4)
	}
	if err := s.sendGameUpperSuccess(session, currentAvatarSocketOpenOpcode, buildCurrentSocketOpenAckBody(request)); err != nil {
		return err
	}
	s.logGameEvent(session, "game-current-avatar-socket-open-success",
		"target_slot", request.TargetSlot,
		"target_item_id", request.TargetItemID,
		"material_slot", request.MaterialSlot,
		"material_consumed", len(result.Consumed) > 0)
	return s.sendCurrentSocketMutationRefresh(session, result, "avatar_socket_open")
}

func (s *Service) commitCurrentAvatarSocketOpen(session *gameSession, request currentSocketOpenRequest) (currentSocketMutationResult, error) {
	characterID, owner, catalog, err := s.currentSocketMutationOwner(session)
	if err != nil {
		return currentSocketMutationResult{}, err
	}
	rule, err := currentSocketEquipmentRule(catalog, request.TargetItemID)
	if err != nil {
		return currentSocketMutationResult{}, err
	}
	if rule.class != currentEquipmentPlacementClassAvatar {
		return currentSocketMutationResult{}, fmt.Errorf("%w: item=%d type=%s", errCurrentSocketTargetKindMismatch, request.TargetItemID, rule.pvfType)
	}
	socketTypes, err := currentSocketAvatarSocketTypes(catalog, request.TargetItemID)
	if err != nil {
		return currentSocketMutationResult{}, err
	}
	if len(socketTypes) == 0 {
		return currentSocketMutationResult{}, fmt.Errorf("%w: avatar socket definitions missing item=%d", errCurrentSocketPVFInvalid, request.TargetItemID)
	}

	var result currentSocketMutationResult
	err = owner.OpenAvatarSocket(context.Background(), dnfsocketemblem.Command{
		CharacterID: characterID,
		Project: func(assets *dnfsocketemblem.Assets) (dnfsocketemblem.Changes, error) {
			inventory := assets.Inventory
			targetKey := currentSocketInventoryKey(currentSocketListAvatar, request.TargetSlot)
			target, ok := inventory.Slots[targetKey]
			if !ok || target.ItemID != request.TargetItemID || target.ItemID <= 0 {
				return dnfsocketemblem.Changes{}, fmt.Errorf("%w: avatar list=1 slot=%d item=%d", errCurrentSocketTargetMissing, request.TargetSlot, request.TargetItemID)
			}
			data := currentAvatarSocketData(target.Extra)
			if currentAvatarSocketOpenCount(data) <= 0 {
				if _, _, err := consumeCurrentSocketStack(inventory, currentSocketListMain, request.MaterialSlot, 0, false); err != nil {
					return dnfsocketemblem.Changes{}, err
				}
				result.Consumed = append(result.Consumed, currentSocketChangedSlot{ListType: currentSocketListMain, Slot: request.MaterialSlot})
			}
			currentSetAvatarSocketTypes(&data, socketTypes)
			currentApplyAvatarSocketDataToStack(&target, currentSocketListAvatar, request.TargetSlot, data)
			inventory.Slots[targetKey] = target
			result.Target = currentSocketChangedSlot{ListType: currentSocketListAvatar, Slot: request.TargetSlot}
			return dnfsocketemblem.Changes{Inventory: true}, nil
		},
	})
	if errors.Is(err, dnfsocketemblem.ErrInventoryNotFound) {
		err = errCurrentSocketInventoryMissing
	}
	if err != nil {
		return currentSocketMutationResult{}, err
	}
	return result, nil
}
