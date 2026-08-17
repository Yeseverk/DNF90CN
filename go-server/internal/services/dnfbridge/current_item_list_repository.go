package dnfbridge

import (
	"context"
	"encoding/binary"
	"strings"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func currentItemListEntriesFromInventory(ctx context.Context, repo dnfrepo.InventoryRepository, characterID string, listType byte) ([]currentItemListEntry, string, bool) {
	if repo == nil {
		return nil, "", false
	}
	record, found, err := repo.Load(ctx, characterID)
	if err != nil || !found {
		return nil, "", false
	}
	entries := make([]currentItemListEntry, 0)
	entries = append(entries, currentItemListEntriesFromMap(record.Slots, listType)...)
	if listType == 2 || listType == 12 {
		entries = append(entries, currentItemListEntriesFromMap(record.Warehouse, listType)...)
	}
	sortCurrentItemListEntries(entries)
	return entries, "inventory", true
}

func (s *Service) mergeCurrentAccountSharedItemListEntries(
	ctx context.Context,
	session *gameSession,
	repo dnfrepo.AccountInventoryRepository,
	characterEntries []currentItemListEntry,
) ([]currentItemListEntry, bool) {
	if repo == nil {
		return characterEntries, false
	}
	accountID := strings.TrimSpace(s.accountIDForSession(session))
	account, found, err := repo.Load(ctx, accountID)
	if err != nil {
		s.logPacketEvent("game-upper-current-item-list-account-inventory-load-failed",
			"conn_id", session.connID,
			"char_id", session.selectedCharacterID,
			"account_id", accountID,
			"list_type", dnfrepo.MainInventoryListType,
			"err", err)
		return characterEntries, false
	}
	if !found {
		account = dnfrepo.AccountInventoryRecord{AccountID: accountID}
	}

	accountView := dnfrepo.MergeAccountSharedInventory(dnfrepo.InventoryRecord{}, account)
	merged := make([]currentItemListEntry, 0, len(characterEntries)+len(accountView.Slots))
	for _, entry := range characterEntries {
		slot := int16(binary.LittleEndian.Uint16(entry.data[0x00:0x02]))
		if dnfrepo.IsAccountSharedInventorySlot(dnfrepo.MainInventoryListType, slot) {
			continue
		}
		merged = append(merged, entry)
	}
	// The current client does not treat slots 354..365 as anonymous numeric
	// counters.  Their side-panel cells resolve the native list-0 item objects,
	// including the persisted 0x77-byte identity tail.  Rebuild those rows
	// through the ordinary list-0 projector, then patch only the authoritative
	// slot, item ID, and quantity just like every other persisted stack.
	merged = append(merged, currentItemListEntriesFromMap(accountView.Slots, dnfrepo.MainInventoryListType)...)
	sortCurrentItemListEntries(merged)
	s.logPacketEvent("game-upper-current-item-list-account-inventory-merged",
		"conn_id", session.connID,
		"char_id", session.selectedCharacterID,
		"account_id", accountID,
		"list_type", dnfrepo.MainInventoryListType,
		"account_found", found,
		"account_shared_entry_count", len(accountView.Slots),
		"merged_entry_count", len(merged))
	return merged, true
}

func currentItemListEntriesFromLegacy(ctx context.Context, repo dnfrepo.LegacyInventoryRepository, characterID string, listType byte) ([]currentItemListEntry, error) {
	items, err := repo.SelectItems(ctx, characterID, listType)
	if err != nil {
		return nil, err
	}
	entries := make([]currentItemListEntry, 0, len(items))
	for _, item := range items {
		if item.SlotIndex < 0 || item.ItemTemplateID <= 0 {
			continue
		}
		entries = append(entries, currentItemListEntryFromLegacyItem(item))
	}
	sortCurrentItemListEntries(entries)
	return entries, nil
}
