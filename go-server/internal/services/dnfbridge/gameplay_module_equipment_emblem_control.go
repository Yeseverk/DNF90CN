package dnfbridge

import (
	"context"
	"errors"
	"fmt"

	dnfsocketemblem "longheng.io/server/internal/modules/dnf/socketemblem"
)

func equipmentEmblemControlGameplayModule() gameplayModuleDefinition {
	controlOpcode := uint16(currentNoBody796Opcode)
	attachOpcode := uint16(currentEquipmentEmblemAttachOpcode)
	controlHandler := func(service *Service, session *gameSession, request gameplayRequest) error {
		return service.handleCurrentNoBody796(session, request.Body)
	}
	attachHandler := func(service *Service, session *gameSession, request gameplayRequest) error {
		return service.handleCurrentEquipmentEmblemAttach(session, request.Body)
	}
	return gameplayModuleDefinition{
		Name: "equipment-emblem-control",
		LegacyHandlers: map[uint16]gameplayHandler{
			controlOpcode: controlHandler,
			attachOpcode:  attachHandler,
		},
		UpperHandlers: map[uint16]gameplayHandler{
			controlOpcode: defaultClassGameplayHandler(
				"game-current-no-body-796-blocked",
				"current_exe_no_body_796_class_mismatch",
				func(service *Service, session *gameSession, body []byte) error {
					return service.handleCurrentNoBody796(session, body)
				},
			),
			attachOpcode: defaultClassGameplayHandler(
				"game-current-equipment-emblem-attach-blocked",
				"current_exe_equipment_emblem_command_class_mismatch",
				func(service *Service, session *gameSession, body []byte) error {
					return service.handleCurrentEquipmentEmblemAttach(session, body)
				},
			),
		},
		LegacyNormalizers: map[uint16]gameplayLegacyNormalizer{
			controlOpcode: stripLegacyNoBody796TransportTrailer,
			attachOpcode:  stripLegacyEmblemAttachTransportTrailer,
		},
	}
}

func (s *Service) handleCurrentNoBody796(session *gameSession, body []byte) error {
	// Current NoPack sub_156ED20 writes only class1/op796, and the registered
	// success reader sub_1D24BD0 consumes no business bytes. Do not infer
	// emblem target/source fields from this acknowledgement or mutate inventory.
	if len(body) != 0 {
		s.logGameEvent(session, "game-current-no-body-796-parse-failed", "body_len", len(body))
		return s.sendGameUpperFailure(session, currentNoBody796Opcode, 4)
	}
	if err := s.sendGameUpperSuccess(session, currentNoBody796Opcode, buildCurrentNoBody796AckBody()); err != nil {
		return err
	}
	s.logGameEvent(session, "game-current-no-body-796-success")
	return nil
}

func (s *Service) handleCurrentEquipmentEmblemAttach(session *gameSession, body []byte) error {
	request, err := decodeCurrentEmblemAttachRequest(body)
	if err != nil {
		s.logGameEvent(session, "game-current-equipment-emblem-attach-parse-failed",
			"body_len", len(body),
			"err", err)
		return s.sendCurrentSocketParseFailure(session, currentEquipmentEmblemAttachOpcode)
	}
	result, err := s.commitCurrentEquipmentEmblemAttach(session, request)
	if err != nil {
		s.logGameEvent(session, "game-current-equipment-emblem-attach-blocked",
			"target_slot", request.TargetSlot,
			"target_item_id", request.TargetItemID,
			"emblem_count", len(request.Emblems),
			"reason", err)
		return s.sendGameUpperFailure(session, currentEquipmentEmblemAttachOpcode, 4)
	}
	if err := s.sendGameUpperSuccess(session, currentEquipmentEmblemAttachOpcode, buildCurrentEquipmentEmblemAttachAckBody(request)); err != nil {
		return err
	}
	s.logGameEvent(session, "game-current-equipment-emblem-attach-success",
		"target_slot", request.TargetSlot,
		"target_item_id", request.TargetItemID,
		"emblem_count", len(request.Emblems))
	if err := s.sendCurrentSocketMutationRefresh(session, result, "equipment_emblem_attach"); err != nil {
		return err
	}
	return nil
}

func (s *Service) commitCurrentEquipmentEmblemAttach(session *gameSession, request currentEmblemAttachRequest) (currentSocketMutationResult, error) {
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
	if rule.class != currentEquipmentPlacementClassNormal {
		return currentSocketMutationResult{}, fmt.Errorf("%w: item=%d type=%s", errCurrentSocketTargetKindMismatch, request.TargetItemID, rule.pvfType)
	}

	var result currentSocketMutationResult
	err = owner.AttachEquipmentEmblems(context.Background(), dnfsocketemblem.Command{
		CharacterID: characterID,
		Project: func(assets *dnfsocketemblem.Assets) (dnfsocketemblem.Changes, error) {
			inventory := assets.Inventory
			targetKey := currentSocketInventoryKey(currentSocketListMain, request.TargetSlot)
			if target, ok := inventory.Slots[targetKey]; ok && target.ItemID == request.TargetItemID && target.ItemID > 0 {
				data := currentEquipmentEmblemData(target.Extra, target.RawEntry)
				if err := currentApplyEquipmentEmblems(catalog, rule, inventory, &data, request.Emblems); err != nil {
					return dnfsocketemblem.Changes{}, err
				}
				currentApplyEquipmentEmblemDataToStack(&target, currentSocketListMain, request.TargetSlot, data, currentEquipmentJewelSocketType(rule))
				inventory.Slots[targetKey] = target
				result.Target = currentSocketChangedSlot{ListType: currentSocketListMain, Slot: request.TargetSlot}
				result.Consumed = currentEmblemConsumedSlots(request.Emblems)
				return dnfsocketemblem.Changes{Inventory: true}, nil
			}

			if !assets.EquipmentFound {
				return dnfsocketemblem.Changes{}, fmt.Errorf("%w: equipped slot=%d", errCurrentSocketTargetMissing, request.TargetSlot)
			}
			entryKey, entry, ok := currentFindEquippedEntry(*assets.Equipment, request.TargetSlot, request.TargetItemID, currentEquipmentPlacementClassNormal)
			if !ok {
				return dnfsocketemblem.Changes{}, fmt.Errorf("%w: equipped slot=%d item=%d", errCurrentSocketTargetMissing, request.TargetSlot, request.TargetItemID)
			}
			data := currentEquipmentEmblemData(entry.Extra, entry.RawEntry)
			if err := currentApplyEquipmentEmblems(catalog, rule, inventory, &data, request.Emblems); err != nil {
				return dnfsocketemblem.Changes{}, err
			}
			currentApplyEquipmentEmblemDataToEquipment(&entry, data, currentEquipmentJewelSocketType(rule))
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
