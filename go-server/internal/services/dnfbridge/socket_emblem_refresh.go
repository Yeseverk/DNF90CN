package dnfbridge

import (
	"context"
	"strconv"

	"longheng.io/server/internal/modules/dnf/dnfenum"
)

func (s *Service) sendCurrentSocketMutationRefresh(session *gameSession, result currentSocketMutationResult, source string) error {
	if session == nil || session.selectedCharacterID == 0 {
		return nil
	}
	if result.TargetEquipped || result.Target.ListType == currentSocketListEquipment {
		if err := s.sendCurrentSocketEquipmentSlotUpdate(session, result.Target.Slot, source+"_target"); err != nil {
			return err
		}
	} else if result.Target.ListType != 0 || result.Target.Slot != 0 {
		if err := s.sendCurrentSocketInventorySlotUpdate(session, result.Target.ListType, result.Target.Slot, source+"_target"); err != nil {
			return err
		}
	}
	for _, changed := range result.Consumed {
		if err := s.sendCurrentSocketInventorySlotUpdate(session, changed.ListType, changed.Slot, source+"_consume"); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) sendCurrentSocketInventorySlotUpdate(session *gameSession, listType byte, slot int16, source string) error {
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
		if stack, ok := inventory.Slots[currentSocketInventoryKey(listType, slot)]; ok && stack.ItemID > 0 {
			entry = currentItemListEntryFromStack(listType, slot, stack)
		} else {
			entry.patchCore(slot, 0, 0)
		}
	} else {
		entry.patchCore(slot, 0, 0)
	}
	body := buildCurrentItemUpdateBody(listType, []currentItemListEntry{entry})
	s.logGameEvent(session, "game-current-socket-inventory-slot-refresh",
		"source", source,
		"msg_id", uint16(dnfenum.CmdPacketWalkoutPartyMember),
		"list_type", listType,
		"slot", slot,
		"body_len", len(body))
	return s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketWalkoutPartyMember), body, 0)
}

func (s *Service) sendCurrentSocketEquipmentSlotUpdate(session *gameSession, slot int16, source string) error {
	if session == nil || session.selectedCharacterID == 0 {
		return nil
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Equipment == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	characterID := strconv.Itoa(int(session.selectedCharacterID))
	equipment, found, err := repositories.Equipment.Load(ctx, characterID)
	if err != nil {
		return err
	}
	var entry currentItemListEntry
	entry.patchCore(slot, 0, 0)
	if found {
		for _, equipped := range equipment.Entries {
			if equipped.SlotIndex == slot {
				if row, ok := currentItemListEntryFromEquipment(equipped); ok {
					entry = row
				}
				break
			}
		}
	}
	body := buildCurrentItemUpdateBody(currentSocketListEquipment, []currentItemListEntry{entry})
	s.logGameEvent(session, "game-current-socket-equipment-slot-refresh",
		"source", source,
		"msg_id", uint16(dnfenum.CmdPacketWalkoutPartyMember),
		"list_type", currentSocketListEquipment,
		"slot", slot,
		"body_len", len(body))
	return s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketWalkoutPartyMember), body, 0)
}
