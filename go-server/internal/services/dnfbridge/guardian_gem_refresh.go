package dnfbridge

import (
	"context"
	"encoding/binary"
	"strconv"

	"longheng.io/server/internal/modules/dnf/dnfenum"
)

func (s *Service) sendCurrentGuardianGemMutationRefresh(session *gameSession, result currentGuardianGemMutationResult) error {
	if session == nil || session.selectedCharacterID == 0 {
		return nil
	}
	switch result.Target.Container {
	case currentGuardianGemTargetInventory:
		if err := s.sendCurrentSocketInventorySlotUpdate(session, result.Target.ListType, result.Target.Slot, "guardian_gem_target"); err != nil {
			return err
		}
	case currentGuardianGemTargetWarehouse:
		if err := s.sendCurrentGuardianGemWarehouseSlotUpdate(session, result.Target.ListType, result.Target.Slot); err != nil {
			return err
		}
	case currentGuardianGemTargetEquipped:
		if err := s.sendCurrentSocketEquipmentSlotUpdate(session, result.Target.Slot, "guardian_gem_target"); err != nil {
			return err
		}
	}
	// Current NoPack accepts the equipped list-3 op14 row, but the list-38
	// guardian-gem page does not remove a consumed row from its tab on that
	// incremental form. Its proven class0/op13 reader rebuilds the page from
	// the ordinary 0x77 list instead, so refresh the authoritative list-38
	// container after the committed source consumption.
	return s.sendCurrentGuardianGemList38Refresh(session, "guardian_gem_consume")
}

func (s *Service) sendCurrentGuardianGemList38Refresh(session *gameSession, source string) error {
	if session == nil || session.selectedCharacterID == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	body, entrySource, entryCount, ok := s.buildCurrentItemListBodyForSession(ctx, session, currentGuardianGemInventoryListType)
	if !ok {
		return errCurrentGuardianGemInventoryMissing
	}
	s.logGameEvent(session, "game-current-guardian-gem-list38-refresh",
		"source", source,
		"msg_id", uint16(dnfenum.CmdPacketLeaveParty),
		"list_type", currentGuardianGemInventoryListType,
		"entry_count", entryCount,
		"body_len", len(body),
		"body_source", entrySource,
		"wire_contract", "current_exe_sub_1D72380_raw_0x77")
	return s.sendCurrentSceneItemList(session, uint16(dnfenum.CmdPacketLeaveParty), body)
}

// sendSelectedGuardianGemWornMedalRefresh restores raw socket words after
// current mode1/mode3 creates the actor equipment objects. Those creation
// rows establish the medal object but do not carry generic raw 0x77 offsets
// 101..109; the current client has already proved it applies a following
// list-3 op14 row to the worn medal.
func (s *Service) sendSelectedGuardianGemWornMedalRefresh(session *gameSession, source string) error {
	if session == nil || session.selectedCharacterID == 0 {
		return nil
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Equipment == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	equipment, found, err := repositories.Equipment.Load(ctx, strconv.Itoa(int(session.selectedCharacterID)))
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	for _, equipped := range equipment.Entries {
		actorSlot, actorSlotOK := currentEXEActorEquipmentSlot(equipped)
		if !actorSlotOK || actorSlot != currentGuildMedalActorSlot || !currentGuardianGemRawSocketOccupied(equipped.RawEntry) {
			continue
		}
		s.logGameEvent(session, "game-current-guardian-gem-worn-refresh",
			"source", source,
			"actor_slot", currentGuildMedalActorSlot,
			"item_id", equipped.ItemID,
			"reason", "mode1_mode3_create_rows_do_not_carry_raw_socket_words")
		return s.sendCurrentSocketEquipmentSlotUpdate(session, int16(actorSlot), "guardian_gem_select_rehydrate")
	}
	return nil
}

func currentGuardianGemRawSocketOccupied(raw []byte) bool {
	if len(raw) < currentGuardianGemRawSocketOffset+currentGuardianGemSocketCount*currentGuardianGemRawSocketWidth {
		return false
	}
	for socket := 0; socket < currentGuardianGemSocketCount; socket++ {
		offset := currentGuardianGemRawSocketOffset + socket*currentGuardianGemRawSocketWidth
		if binary.LittleEndian.Uint16(raw[offset:offset+currentGuardianGemRawSocketWidth]) != 0 {
			return true
		}
	}
	return false
}

func (s *Service) sendCurrentGuardianGemWarehouseSlotUpdate(session *gameSession, listType byte, slot int16) error {
	if session == nil || session.selectedCharacterID == 0 {
		return nil
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Inventory == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	characterID := strconv.Itoa(int(session.selectedCharacterID))
	inventory, found, err := repositories.Inventory.Load(ctx, characterID)
	if err != nil {
		return err
	}
	var entry currentItemListEntry
	if found {
		if item, ok := inventory.Warehouse[currentSocketInventoryKey(listType, slot)]; ok && item.ItemID > 0 {
			entry = currentItemListEntryFromStack(listType, slot, item)
		} else {
			entry.patchCore(slot, 0, 0)
		}
	} else {
		entry.patchCore(slot, 0, 0)
	}
	body := buildCurrentItemUpdateBody(listType, []currentItemListEntry{entry})
	s.logGameEvent(session, "game-current-guardian-gem-warehouse-slot-refresh",
		"msg_id", uint16(dnfenum.CmdPacketWalkoutPartyMember),
		"list_type", listType,
		"slot", slot,
		"body_len", len(body))
	return s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketWalkoutPartyMember), body, 0)
}
