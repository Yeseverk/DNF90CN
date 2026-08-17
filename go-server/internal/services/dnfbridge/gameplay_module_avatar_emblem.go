package dnfbridge

import (
	"context"
	"errors"
	"fmt"

	dnfsocketemblem "longheng.io/server/internal/modules/dnf/socketemblem"
)

func avatarEmblemGameplayModule() gameplayModuleDefinition {
	opcode := uint16(currentAvatarEmblemAttachOpcode)
	handler := func(service *Service, session *gameSession, request gameplayRequest) error {
		return service.handleCurrentAvatarEmblemAttach(session, request.Body, currentAvatarEmblemAttachOpcode)
	}
	return gameplayModuleDefinition{
		Name:           "avatar-emblem",
		LegacyHandlers: map[uint16]gameplayHandler{opcode: handler},
		UpperHandlers: map[uint16]gameplayHandler{
			opcode: defaultClassGameplayHandler(
				"game-current-avatar-emblem-attach-blocked",
				"current_exe_socket_command_class_mismatch",
				func(service *Service, session *gameSession, body []byte) error {
					return service.handleCurrentAvatarEmblemAttach(session, body, currentAvatarEmblemAttachOpcode)
				},
			),
		},
		LegacyNormalizers: map[uint16]gameplayLegacyNormalizer{
			opcode: stripLegacyEmblemAttachTransportTrailer,
		},
	}
}

func (s *Service) handleCurrentAvatarEmblemAttach(session *gameSession, body []byte, ackOpcode uint16) error {
	request, err := decodeCurrentAvatarEmblemAttachRequest(body)
	if err != nil {
		s.logGameEvent(session, "game-current-avatar-emblem-attach-parse-failed", "body_len", len(body), "err", err)
		return s.sendCurrentSocketParseFailure(session, ackOpcode)
	}
	result, err := s.commitCurrentAvatarEmblemAttach(session, request)
	if err != nil {
		s.logGameEvent(session, "game-current-avatar-emblem-attach-blocked",
			"target_slot", request.TargetSlot,
			"target_item_id", request.TargetItemID,
			"emblems", len(request.Emblems),
			"reason", err)
		return s.sendGameUpperFailure(session, ackOpcode, 4)
	}
	if err := s.sendGameUpperSuccess(session, ackOpcode, buildCurrentAvatarEmblemAttachAckBody(request)); err != nil {
		return err
	}
	s.logGameEvent(session, "game-current-avatar-emblem-attach-success",
		"target_slot", request.TargetSlot,
		"target_item_id", request.TargetItemID,
		"emblems", len(request.Emblems),
		"target_equipped", result.TargetEquipped)
	return s.sendCurrentSocketMutationRefresh(session, result, "avatar_emblem_attach")
}

func (s *Service) commitCurrentAvatarEmblemAttach(session *gameSession, request currentEmblemAttachRequest) (currentSocketMutationResult, error) {
	if request.TargetItemID <= 0 || len(request.Emblems) == 0 {
		return currentSocketMutationResult{}, errCurrentSocketTargetMissing
	}
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

	var result currentSocketMutationResult
	err = owner.AttachAvatarEmblems(context.Background(), dnfsocketemblem.Command{
		CharacterID: characterID,
		Project: func(assets *dnfsocketemblem.Assets) (dnfsocketemblem.Changes, error) {
			inventory := assets.Inventory
			targetKey := currentSocketInventoryKey(currentSocketListAvatar, request.TargetSlot)
			if target, ok := inventory.Slots[targetKey]; ok && target.ItemID == request.TargetItemID && target.ItemID > 0 {
				data := currentAvatarSocketData(target.Extra)
				if err := currentApplyAvatarEmblems(catalog, inventory, &data, request.Emblems); err != nil {
					return dnfsocketemblem.Changes{}, err
				}
				currentApplyAvatarSocketDataToStack(&target, currentSocketListAvatar, request.TargetSlot, data)
				inventory.Slots[targetKey] = target
				result.Target = currentSocketChangedSlot{ListType: currentSocketListAvatar, Slot: request.TargetSlot}
				result.Consumed = currentEmblemConsumedSlots(request.Emblems)
				return dnfsocketemblem.Changes{Inventory: true}, nil
			}

			if !assets.EquipmentFound {
				return dnfsocketemblem.Changes{}, fmt.Errorf("%w: equipped avatar slot=%d", errCurrentSocketTargetMissing, request.TargetSlot)
			}
			entryKey, entry, ok := currentFindEquippedEntry(*assets.Equipment, request.TargetSlot, request.TargetItemID, currentEquipmentPlacementClassAvatar)
			if !ok {
				return dnfsocketemblem.Changes{}, fmt.Errorf("%w: equipped avatar slot=%d item=%d", errCurrentSocketTargetMissing, request.TargetSlot, request.TargetItemID)
			}
			data := currentAvatarSocketData(entry.Extra)
			if err := currentApplyAvatarEmblems(catalog, inventory, &data, request.Emblems); err != nil {
				return dnfsocketemblem.Changes{}, err
			}
			currentApplyAvatarSocketDataToEquipment(&entry, data)
			assets.Equipment.Entries[entryKey] = entry
			result.Target = currentSocketChangedSlot{ListType: currentSocketListEquipment, Slot: entry.SlotIndex}
			result.TargetEquipped = true
			result.Consumed = currentEmblemConsumedSlots(request.Emblems)
			return dnfsocketemblem.Changes{Inventory: true, Equipment: true}, nil
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
