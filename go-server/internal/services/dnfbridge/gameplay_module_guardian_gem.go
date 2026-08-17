package dnfbridge

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfguardiangem "longheng.io/server/internal/modules/dnf/guardiangem"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func currentGuardianGemOwner(repositories dnfrepo.Group) (*dnfguardiangem.Owner, error) {
	owner, err := dnfguardiangem.NewOwner(repositories)
	if err != nil {
		return nil, errCurrentGuardianGemTransactionMissing
	}
	return owner, nil
}

func currentGuardianGemMutationError(err error) error {
	switch {
	case errors.Is(err, dnfguardiangem.ErrOwnerUnavailable),
		errors.Is(err, dnfguardiangem.ErrCharacterRequired):
		return errors.Join(errCurrentGuardianGemTransactionMissing, err)
	case errors.Is(err, dnfguardiangem.ErrInventoryMissing):
		return errors.Join(errCurrentGuardianGemInventoryMissing, err)
	default:
		return err
	}
}

func guardianGemGameplayModule() gameplayModuleDefinition {
	opcode := uint16(dnfenum.CmdPacketUseGem)
	handler := func(service *Service, session *gameSession, request gameplayRequest) error {
		return service.handleCurrentGuardianGemUse(session, request.Body)
	}
	return gameplayModuleDefinition{
		Name:           "guardian-gem",
		LegacyHandlers: map[uint16]gameplayHandler{opcode: handler},
		UpperHandlers: map[uint16]gameplayHandler{
			opcode: defaultClassGameplayHandler(
				"game-current-guardian-gem-use-blocked",
				"current_exe_op829_command_class_mismatch",
				func(service *Service, session *gameSession, body []byte) error {
					return service.handleCurrentGuardianGemUse(session, body)
				},
			),
		},
		LegacyNormalizers: map[uint16]gameplayLegacyNormalizer{
			opcode: func(body []byte) []byte {
				return stripLegacyTransportTrailer(body, currentGuardianGemUseRequestWireSize)
			},
		},
	}
}

func (s *Service) handleCurrentGuardianGemUse(session *gameSession, body []byte) error {
	request, err := decodeCurrentGuardianGemUseRequest(body)
	if err != nil {
		s.logGameEvent(session, "game-current-guardian-gem-use-parse-failed", "body_len", len(body), "err", err)
		return nil
	}
	result, err := s.commitCurrentGuardianGemUse(session, request)
	if err != nil {
		s.logGameEvent(session, "game-current-guardian-gem-use-blocked",
			"target_medal_item_id", request.TargetMedalItemID,
			"guardian_gem_source_slot", request.GuardianGemSourceSlot,
			"guardian_gem_item_id", request.GuardianGemItemID,
			"socket_index", request.SocketIndex,
			"reason", err)
		return nil
	}
	s.logGameEvent(session, "game-current-guardian-gem-use-success",
		"target_medal_item_id", request.TargetMedalItemID,
		"guardian_gem_source_slot", request.GuardianGemSourceSlot,
		"guardian_gem_item_id", request.GuardianGemItemID,
		"socket_index", request.SocketIndex,
		"target_container", result.Target.Container,
		"target_list_type", result.Target.ListType,
		"target_refresh_slot", result.Target.Slot,
		"source_slot", result.Source.Slot)
	// Current NoPack 829/830 only maintains a type-46 UI state table. It is
	// not an acknowledgement nor an item update. The current item-list row is
	// the real client-visible state carrier, so deliberately send no 829 reply.
	return s.sendCurrentGuardianGemMutationRefresh(session, result)
}

func (s *Service) commitCurrentGuardianGemUse(session *gameSession, request currentGuardianGemUseRequest) (currentGuardianGemMutationResult, error) {
	if request.GuardianGemSourceSlot > 32767 || !currentGuardianGemPageContains(int16(request.GuardianGemSourceSlot)) {
		return currentGuardianGemMutationResult{}, fmt.Errorf("%w: slot=%d", errCurrentGuardianGemSourceSlotRange, request.GuardianGemSourceSlot)
	}
	if _, err := currentGuardianGemSocketValue(request.GuardianGemItemID); err != nil {
		return currentGuardianGemMutationResult{}, err
	}
	characterID, repositories, catalog, err := s.currentGuardianGemMutationContext(session)
	if err != nil {
		return currentGuardianGemMutationResult{}, err
	}
	if _, err := resolveCurrentGuardianGem(catalog, request.GuardianGemItemID); err != nil {
		return currentGuardianGemMutationResult{}, err
	}
	if _, err := resolveCurrentGuardianGemMedal(catalog, request.TargetMedalItemID); err != nil {
		return currentGuardianGemMutationResult{}, err
	}

	owner, err := currentGuardianGemOwner(repositories)
	if err != nil {
		return currentGuardianGemMutationResult{}, err
	}
	var result currentGuardianGemMutationResult
	err = owner.Insert(context.Background(), dnfguardiangem.Command{
		CharacterID: characterID,
		Project: func(assets *dnfguardiangem.Assets) (dnfguardiangem.Changes, error) {
			inventory := assets.Inventory
			equipment := assets.Equipment
			equipmentFound := len(equipment.Entries) > 0
			target, err := currentGuardianGemFindTarget(*inventory, *equipment, equipmentFound, request)
			if err != nil {
				return dnfguardiangem.Changes{}, err
			}
			if err := currentGuardianGemWriteTarget(inventory, equipment, target, request); err != nil {
				return dnfguardiangem.Changes{}, err
			}

			sourceKey, sourceSlot, err := currentGuardianGemFindMedalBagSource(
				inventory.Slots,
				request.GuardianGemSourceSlot,
				request.GuardianGemItemID,
			)
			if err != nil {
				return dnfguardiangem.Changes{}, err
			}
			source := inventory.Slots[sourceKey]
			if source.Count <= 0 || source.ItemID != int64(request.GuardianGemItemID) {
				return dnfguardiangem.Changes{}, fmt.Errorf("%w: key=%s", errCurrentGuardianGemSourceMissing, sourceKey)
			}
			if source.Count == 1 {
				delete(inventory.Slots, sourceKey)
			} else {
				source.Count--
				currentRefreshStackRawEntry(&source, currentGuardianGemInventoryListType, sourceSlot)
				inventory.Slots[sourceKey] = source
			}
			result = currentGuardianGemMutationResult{
				Target: target,
				Source: currentSocketChangedSlot{ListType: currentGuardianGemInventoryListType, Slot: sourceSlot},
			}
			return dnfguardiangem.Changes{
				InventorySlots:     true,
				InventoryWarehouse: target.Container == currentGuardianGemTargetWarehouse,
				Equipment:          target.Container == currentGuardianGemTargetEquipped,
			}, nil
		},
	})
	if err != nil {
		return currentGuardianGemMutationResult{}, currentGuardianGemMutationError(err)
	}
	return result, nil
}

func (s *Service) currentGuardianGemMutationContext(session *gameSession) (string, dnfrepo.Group, *pvfDungeonDropCatalog, error) {
	if session == nil || session.selectedCharacterID == 0 {
		return "", dnfrepo.Group{}, nil, errCurrentGuardianGemCharacterMissing
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Inventory == nil || repositories.Equipment == nil {
		return "", dnfrepo.Group{}, nil, errCurrentGuardianGemRepositoryMissing
	}
	if repositories.CharacterItems == nil {
		return "", dnfrepo.Group{}, nil, errCurrentGuardianGemTransactionMissing
	}
	catalog, err := s.currentPVFItemCatalog()
	if err != nil {
		return "", dnfrepo.Group{}, nil, err
	}
	return strconv.Itoa(int(session.selectedCharacterID)), repositories, catalog, nil
}
