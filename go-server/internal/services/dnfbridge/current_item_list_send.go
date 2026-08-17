package dnfbridge

import (
	"context"
	"fmt"
	"math"
	"strconv"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func (s *Service) sendSelectedCurrentItemLists(session *gameSession, source string) error {
	return s.sendSelectedCurrentItemListsWithRefresh(session, source, false)
}

func (s *Service) sendSelectedCurrentItemListsRefresh(session *gameSession, source string) error {
	return s.sendSelectedCurrentItemListsWithRefresh(session, source, true)
}

func (s *Service) sendSelectedCurrentItemListsWithRefresh(session *gameSession, source string, refresh bool) error {
	s.cleanupSelectedExpiredRentalEquipment(session, source)
	if err := s.sendSelectedCurrentContainerListsWithRefresh(session, source, refresh); err != nil {
		return err
	}
	// Full mode1/mode3 creates the equipped objects. The working Python
	// sequence does not append a generic equipment refresh. Guardian-gem raw
	// socket words are the one proved exception: they are not contained by the
	// mode1/mode3 create rows and must be replayed through the accepted list-3
	// op14 carrier after the character containers are established.
	if err := s.sendSelectedRentalWalletStateWithRefresh(session, source, refresh); err != nil {
		return err
	}
	return s.sendSelectedGuardianGemWornMedalRefresh(session, source+"_after_containers")
}

func (s *Service) sendSelectedCurrentContainerListsRefresh(session *gameSession, source string) error {
	return s.sendSelectedCurrentContainerListsWithRefresh(session, source, true)
}

func (s *Service) sendSelectedCurrentContainerListsWithRefresh(session *gameSession, source string, refresh bool) error {
	if session == nil {
		return nil
	}
	if session.selectedItemListRefreshSent && !refresh {
		s.logPacketEvent("game-upper-current-item-list-refresh-skipped",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"source", source,
			"char_id", session.selectedCharacterID,
			"reason", "already_sent")
		return nil
	}
	if session.selectedCharacterID == 0 {
		s.logPacketEvent("game-upper-current-item-list-refresh-deferred",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"source", source,
			"reason", "character_not_selected")
		if refresh {
			return fmt.Errorf("current item-list refresh requires a selected character")
		}
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), currentRepositorySnapshotTimeout)
	defer cancel()

	type currentContainerListPlan struct {
		listType    byte
		body        []byte
		entrySource string
		entryCount  int
	}
	plan := make([]currentContainerListPlan, 0, len(currentSelectedItemListTypes))
	for _, listType := range currentSelectedItemListTypes {
		body, entrySource, entryCount, ok := s.buildCurrentItemListBodyForSession(ctx, session, listType)
		if !ok {
			if refresh {
				return fmt.Errorf(
					"current item-list refresh could not build required list %d for character %d",
					listType,
					session.selectedCharacterID,
				)
			}
			continue
		}
		plan = append(plan, currentContainerListPlan{
			listType:    listType,
			body:        body,
			entrySource: entrySource,
			entryCount:  entryCount,
		})
	}

	sent := 0
	for _, entry := range plan {
		s.logPacketEvent("game-upper-current-item-list-refresh-send",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"source", source,
			"char_id", session.selectedCharacterID,
			"msg_id", uint16(dnfenum.CmdPacketLeaveParty),
			"list_type", entry.listType,
			"entry_count", entry.entryCount,
			"body_len", len(entry.body),
			"body_source", entry.entrySource,
			"entry_size", currentItemListEntrySizeForType(entry.listType),
			"mcp_handler", "sub_1D72380")
		if err := s.sendCurrentSceneItemList(session, uint16(dnfenum.CmdPacketLeaveParty), entry.body); err != nil {
			return err
		}
		sent++
	}
	if sent > 0 {
		session.selectedItemListRefreshSent = true
	}
	return nil
}

func (s *Service) sendSelectedIncrementalItemSlotRefreshes(
	session *gameSession,
	operation string,
	refreshes []alignedcmd.ItemSlotRefresh,
) error {
	if len(refreshes) == 0 {
		return nil
	}
	if session == nil || session.selectedCharacterID == 0 {
		return fmt.Errorf("aligned item-slot refresh requires a selected character")
	}
	repos, ok := s.repositoryGroup()
	if !ok || repos.Inventory == nil {
		return fmt.Errorf("aligned item-slot refresh requires inventory repository")
	}

	ctx, cancel := context.WithTimeout(context.Background(), currentRepositorySnapshotTimeout)
	defer cancel()
	characterID := strconv.Itoa(int(session.selectedCharacterID))
	inventory, found, err := repos.Inventory.Load(ctx, characterID)
	if err != nil {
		return fmt.Errorf("aligned item-slot refresh load character %s: %w", characterID, err)
	}
	if !found {
		return fmt.Errorf("aligned item-slot refresh inventory %s not found", characterID)
	}
	var account dnfrepo.AccountInventoryRecord
	accountLoaded := false
	loadAccount := func() error {
		if accountLoaded {
			return nil
		}
		if repos.AccountInventory == nil {
			return fmt.Errorf("aligned item-slot refresh requires account inventory repository")
		}
		accountID := s.accountIDForSession(session)
		if accountID == "" {
			return fmt.Errorf("aligned item-slot refresh requires account id for shared slot")
		}
		loaded, _, err := repos.AccountInventory.Load(ctx, accountID)
		if err != nil {
			return fmt.Errorf("aligned item-slot refresh load account %s: %w", accountID, err)
		}
		account = loaded
		accountLoaded = true
		return nil
	}

	entriesByList := make(map[byte][]currentItemListEntry)
	listOrder := make([]byte, 0, len(refreshes))
	seen := make(map[string]struct{}, len(refreshes))
	for _, refresh := range refreshes {
		if refresh.SlotIndex < 0 {
			return fmt.Errorf("aligned item-slot refresh has invalid slot %d", refresh.SlotIndex)
		}
		key := fmt.Sprintf("%d:%d", refresh.ListType, refresh.SlotIndex)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		if _, knownList := entriesByList[refresh.ListType]; !knownList {
			listOrder = append(listOrder, refresh.ListType)
		}

		var stack dnfrepo.ItemStack
		var occupied bool
		if dnfrepo.IsAccountSharedInventorySlot(refresh.ListType, refresh.SlotIndex) {
			if err := loadAccount(); err != nil {
				return err
			}
			stack, occupied = account.Slots[key]
		} else {
			stack, occupied = inventory.Slots[key]
			if !occupied && refresh.ListType == 2 {
				stack, occupied = inventory.Warehouse[key]
			}
		}
		var entry currentItemListEntry
		if occupied && stack.ItemID > 0 && stack.Count >= 0 {
			entry = currentItemListEntryFromStack(refresh.ListType, refresh.SlotIndex, stack)
		} else {
			entry.patchCore(refresh.SlotIndex, math.MaxUint32, 0)
		}
		entriesByList[refresh.ListType] = append(entriesByList[refresh.ListType], entry)
	}

	for _, listType := range listOrder {
		entries := entriesByList[listType]
		if len(entries) == 0 {
			continue
		}
		body := buildCurrentItemUpdateBody(listType, entries)
		s.logPacketEvent("game-aligned-command-item-slot-refresh-send",
			"conn_id", session.connID,
			"operation", operation,
			"char_id", session.selectedCharacterID,
			"msg_id", uint16(dnfenum.CmdPacketWalkoutPartyMember),
			"classification", 0,
			"list_type", listType,
			"entry_count", len(entries),
			"body_len", len(body),
			"sequence", "durable_owner_then_class1_ack_then_repository_backed_op14_changed_rows")
		if err := s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketWalkoutPartyMember), body, 0); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) sendSelectedEquipmentItemUpdate(session *gameSession, source string) error {
	return s.sendSelectedEquipmentItemUpdateWithRefresh(session, source, false)
}

func (s *Service) sendSelectedEquipmentItemUpdateWithRefresh(session *gameSession, source string, refresh bool) error {
	if session == nil {
		return nil
	}
	if session.selectedEquipmentUpdateSent && !refresh {
		s.logPacketEvent("game-upper-current-equipment-item-update-skipped",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"source", source,
			"char_id", session.selectedCharacterID,
			"reason", "already_sent")
		return nil
	}
	if session.selectedCharacterID == 0 {
		s.logPacketEvent("game-upper-current-equipment-item-update-deferred",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"source", source,
			"reason", "character_not_selected")
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), currentRepositorySnapshotTimeout)
	defer cancel()

	body, entrySource, entryCount, ok := s.buildCurrentEquippedItemListBodyForSession(ctx, session)
	if !ok {
		return nil
	}
	if entryCount == 0 {
		s.logPacketEvent("game-upper-current-equipment-item-update-skipped",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"source", source,
			"char_id", session.selectedCharacterID,
			"msg_id", uint16(dnfenum.CmdPacketWalkoutPartyMember),
			"list_type", 3,
			"entry_count", entryCount,
			"body_len", len(body),
			"body_source", entrySource,
			"reason", "no_equipped_entries")
		session.selectedEquipmentUpdateSent = true
		return nil
	}

	s.logPacketEvent("game-upper-current-equipment-item-update-send",
		"conn_id", session.connID,
		"channel_id", session.channel.ID,
		"source", source,
		"char_id", session.selectedCharacterID,
		"msg_id", uint16(dnfenum.CmdPacketWalkoutPartyMember),
		"list_type", 3,
		"entry_count", entryCount,
		"body_len", len(body),
		"body_source", entrySource,
		"entry_size", currentItemListEntryWireSize,
		"evidence", "current_0x77_rows_restored_after_0x54_update_crashed")
	if err := s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketWalkoutPartyMember), body, 0); err != nil {
		return err
	}
	session.selectedEquipmentUpdateSent = true
	return nil
}

func (s *Service) sendSelectedCurrentEquipmentSlotItemUpdates(session *gameSession, source string) error {
	if session == nil {
		return nil
	}
	if session.selectedCharacterID == 0 {
		s.logPacketEvent("game-upper-current-equipment-slot-items-refresh-deferred",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"source", source,
			"reason", "character_not_selected")
		return nil
	}
	repos, ok := s.repositoryGroup()
	if !ok || repos.Equipment == nil {
		s.logPacketEvent("game-upper-current-equipment-slot-items-refresh-skipped",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"source", source,
			"char_id", session.selectedCharacterID,
			"reason", "equipment_repository_unavailable")
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), currentRepositorySnapshotTimeout)
	defer cancel()

	characterID := strconv.Itoa(int(session.selectedCharacterID))
	entries, found, err := currentItemListEntriesFromEquipment(ctx, repos.Equipment, characterID)
	if err != nil {
		return err
	}
	if !found || len(entries) == 0 {
		s.logPacketEvent("game-upper-current-equipment-slot-items-refresh-skipped",
			"conn_id", session.connID,
			"channel_id", session.channel.ID,
			"source", source,
			"char_id", session.selectedCharacterID,
			"reason", "no_occupied_equipment_entries")
		return nil
	}
	patchedUsePeriods, usePeriodErr := s.applyCurrentPVFUsePeriodsToEntriesWithLoadedCatalog(ctx, entries)
	if usePeriodErr != nil {
		s.logPacketEvent("game-upper-current-equipment-slot-items-use-period-wire-projection-failed",
			"conn_id", session.connID,
			"char_id", session.selectedCharacterID,
			"list_type", currentSocketListEquipment,
			"err", usePeriodErr)
	}
	if patchedUsePeriods > 0 {
		s.logPacketEvent("game-upper-current-equipment-slot-items-use-period-wire-projection-applied",
			"conn_id", session.connID,
			"char_id", session.selectedCharacterID,
			"list_type", currentSocketListEquipment,
			"patched_rows", patchedUsePeriods)
	}
	body := buildCurrentItemUpdateBody(currentSocketListEquipment, entries)
	s.logPacketEvent("game-upper-current-equipment-slot-items-refresh-send",
		"conn_id", session.connID,
		"channel_id", session.channel.ID,
		"source", source,
		"char_id", session.selectedCharacterID,
		"msg_id", uint16(dnfenum.CmdPacketWalkoutPartyMember),
		"classification", 0,
		"list_type", currentSocketListEquipment,
		"entry_count", len(entries),
		"body_len", len(body),
		"entry_size", currentItemListEntryWireSize,
		"sequence", "op14_list3_occupied_equipment_item_rows")
	return s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketWalkoutPartyMember), body, 0)
}
