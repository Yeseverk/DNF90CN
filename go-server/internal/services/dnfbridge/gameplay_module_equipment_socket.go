package dnfbridge

import (
	"context"
	"errors"
	"fmt"

	dnfsocketemblem "longheng.io/server/internal/modules/dnf/socketemblem"
)

func equipmentSocketGameplayModule() gameplayModuleDefinition {
	opcode := uint16(currentEquipmentSocketOpenOpcode)
	handler := func(service *Service, session *gameSession, request gameplayRequest) error {
		return service.handleCurrentEquipmentSocketOpen(session, request.Body)
	}
	return gameplayModuleDefinition{
		Name:           "equipment-socket",
		LegacyHandlers: map[uint16]gameplayHandler{opcode: handler},
		UpperHandlers: map[uint16]gameplayHandler{
			opcode: defaultClassGameplayHandler(
				"game-current-equipment-socket-open-blocked",
				"current_exe_socket_command_class_mismatch",
				func(service *Service, session *gameSession, body []byte) error {
					return service.handleCurrentEquipmentSocketOpen(session, body)
				},
			),
		},
		LegacyNormalizers: map[uint16]gameplayLegacyNormalizer{
			opcode: stripLegacySocketOpenTransportTrailer,
		},
	}
}

func (s *Service) handleCurrentEquipmentSocketOpen(session *gameSession, body []byte) error {
	request, err := decodeCurrentSocketOpenRequest(body)
	if err != nil {
		s.logGameEvent(session, "game-current-equipment-socket-open-parse-failed", "body_len", len(body), "err", err)
		return s.sendCurrentSocketParseFailure(session, currentEquipmentSocketOpenOpcode)
	}
	result, err := s.commitCurrentEquipmentSocketOpen(session, request)
	if err != nil {
		s.logGameEvent(session, "game-current-equipment-socket-open-blocked",
			"target_slot", request.TargetSlot,
			"target_item_id", request.TargetItemID,
			"material_slot", request.MaterialSlot,
			"reason", err)
		return s.sendGameUpperFailure(session, currentEquipmentSocketOpenOpcode, 4)
	}
	if err := s.sendGameUpperSuccess(session, currentEquipmentSocketOpenOpcode, buildCurrentEquipmentSocketOpenAckBody(request)); err != nil {
		return err
	}
	s.logGameEvent(session, "game-current-equipment-socket-open-success",
		"target_slot", request.TargetSlot,
		"target_item_id", request.TargetItemID,
		"material_slot", request.MaterialSlot,
		"material_consumed", len(result.Consumed) > 0)
	if err := s.sendCurrentSocketMutationRefresh(session, result, "equipment_socket_open"); err != nil {
		return err
	}
	return nil
}

func (s *Service) commitCurrentEquipmentSocketOpen(session *gameSession, request currentSocketOpenRequest) (currentSocketMutationResult, error) {
	characterID, owner, catalog, err := s.currentSocketMutationOwner(session)
	if err != nil {
		return currentSocketMutationResult{}, err
	}
	rule, err := currentSocketEquipmentRule(catalog, request.TargetItemID)
	if err != nil {
		return currentSocketMutationResult{}, err
	}
	if rule.class != currentEquipmentPlacementClassNormal {
		return currentSocketMutationResult{}, fmt.Errorf("%w: item=%d type=%s", errCurrentSocketTargetKindMismatch, request.TargetItemID, rule.pvfType)
	}

	var result currentSocketMutationResult
	err = owner.OpenEquipmentSocket(context.Background(), dnfsocketemblem.Command{
		CharacterID: characterID,
		Project: func(assets *dnfsocketemblem.Assets) (dnfsocketemblem.Changes, error) {
			inventory := assets.Inventory
			targetKey, targetSlot, target, err := currentFindMainInventorySocketTarget(*inventory, request.TargetSlot, request.TargetItemID)
			if err != nil {
				return dnfsocketemblem.Changes{}, err
			}
			data := currentEquipmentEmblemData(target.Extra, target.RawEntry)
			maxOpen := currentEquipmentSocketOpenCount(rule)
			openCount := int(data[0])
			if openCount > maxOpen {
				openCount = maxOpen
			}
			if openCount <= 0 {
				openCount, err = currentOpenEquipmentSocketWithMaterial(catalog, inventory, request.MaterialSlot, rule)
				if err != nil {
					return dnfsocketemblem.Changes{}, err
				}
				result.Consumed = append(result.Consumed, currentSocketChangedSlot{ListType: currentSocketListMain, Slot: request.MaterialSlot})
			}
			currentEnsureEquipmentEmblemSocketsOpen(&data, maxOpen, openCount)
			currentApplyEquipmentEmblemDataToStack(&target, currentSocketListMain, targetSlot, data, currentEquipmentJewelSocketType(rule))
			inventory.Slots[targetKey] = target
			result.Target = currentSocketChangedSlot{ListType: currentSocketListMain, Slot: targetSlot}
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
