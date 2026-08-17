package dnfbridge

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func currentRentalEffectiveLevel(
	ctx context.Context,
	accounts dnfrepo.AccountRepository,
	accountID string,
	level int,
	now time.Time,
) (int, bool) {
	if accounts == nil || strings.TrimSpace(accountID) == "" {
		return level, false
	}
	account, found, err := accounts.Load(ctx, accountID)
	if err != nil || !found || !currentPremiumActive(account, currentPremiumTypeOverEquip, now) {
		return level, false
	}
	return level + 10, true
}

func (s *Service) sendSelectedCurrentRentalInventoryItemUpdate(
	session *gameSession,
	repository dnfrepo.InventoryRepository,
	characterID string,
	itemID uint32,
	result currentRentalAssetResult,
) error {
	if result.Equipped {
		s.logGameEvent(session, "game-upper-rental-inventory-item-update-skipped",
			"character_id", characterID,
			"item_id", itemID,
			"slot", result.Slot,
			"reason", "existing_rental_is_equipped")
		return nil
	}
	if repository == nil || characterID == "" || itemID == 0 || result.Slot < 0 {
		return fmt.Errorf("invalid rental inventory update character=%q item=%d slot=%d", characterID, itemID, result.Slot)
	}

	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	inventory, found, err := repository.Load(ctx, characterID)
	if err != nil {
		return fmt.Errorf("load committed rental inventory character=%s: %w", characterID, err)
	}
	if !found {
		return fmt.Errorf("load committed rental inventory character=%s: %w", characterID, errCurrentRentalStateMissing)
	}
	key := fmt.Sprintf("0:%d", result.Slot)
	stack, found := inventory.Slots[key]
	if !found || stack.ItemID != int64(itemID) || !strings.EqualFold(strings.TrimSpace(stack.Extra["source"]), currentRentalItemSource) {
		return fmt.Errorf("committed rental inventory row missing character=%s key=%s item=%d", characterID, key, itemID)
	}

	entry := currentItemListEntryFromStack(0, result.Slot, stack)
	body := buildCurrentItemUpdateBody(0, []currentItemListEntry{entry})
	s.logGameEvent(session, "game-upper-rental-inventory-item-update-send",
		"character_id", characterID,
		"item_id", itemID,
		"slot", result.Slot,
		"msg_id", uint16(dnfenum.CmdPacketWalkoutPartyMember),
		"classification", 0,
		"list_type", 0,
		"entry_count", 1,
		"entry_size", currentItemListEntryWireSize,
		"body_len", len(body),
		"body_source", "committed_pvf_rental_inventory_op14_single_raw77")
	return s.sendGameUpperRawClass(session, uint16(dnfenum.CmdPacketWalkoutPartyMember), body, 0)
}

func (s *Service) currentRentalSelectedCharacter(ctx context.Context, session *gameSession) (dnfrepo.Group, string, dnfrepo.CharacterRecord, error) {
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.RentalAssets == nil {
		return dnfrepo.Group{}, "", dnfrepo.CharacterRecord{}, dnfrepo.ErrRentalAssetTransactionUnavailable
	}
	if session == nil || session.selectedCharacterID == 0 {
		return dnfrepo.Group{}, "", dnfrepo.CharacterRecord{}, errCurrentRentalStateMissing
	}
	characterID := strconv.Itoa(int(session.selectedCharacterID))
	character, found, err := repositories.Character.Load(ctx, characterID)
	if err != nil {
		return dnfrepo.Group{}, "", dnfrepo.CharacterRecord{}, err
	}
	if !found {
		return dnfrepo.Group{}, "", dnfrepo.CharacterRecord{}, errCurrentRentalStateMissing
	}
	if strings.TrimSpace(character.CharacterID) != characterID || strings.TrimSpace(character.AccountID) != strings.TrimSpace(s.accountIDForSession(session)) {
		return dnfrepo.Group{}, "", dnfrepo.CharacterRecord{}, errCurrentRentalOwnerMismatch
	}
	return repositories, characterID, character, nil
}

func (s *Service) sendSelectedRentalWalletStateWithRefresh(session *gameSession, source string, refresh bool) error {
	if session == nil || session.selectedCharacterID == 0 {
		return nil
	}
	if session.selectedRentalWalletStateSent && !refresh {
		return nil
	}
	// Current class0/op14 is a real item-container update reader
	// (sub_1D73120), not a gold-wallet packet. The old login refresh wrote a
	// synthetic item_id=0 row into list0 slot0 and polluted the shared client
	// item-object state. Gold remains committed in the character repository;
	// only the proven rental-point state is sent here.
	return s.sendSelectedCurrentRentalPointState(session, source, refresh)
}

func (s *Service) cleanupSelectedExpiredRentalEquipment(session *gameSession, source string) {
	if session == nil || session.selectedCharacterID == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	repositories, characterID, _, err := s.currentRentalSelectedCharacter(ctx, session)
	if err != nil {
		return
	}
	owner, err := currentRentalAssetOwner(repositories)
	if err != nil {
		return
	}
	removed, err := cleanupExpiredCurrentRentalEquipment(ctx, owner, s.accountIDForSession(session), characterID, time.Now().UTC())
	if err != nil {
		s.logGameEvent(session, "game-upper-expired-rental-cleanup-failed", "source", source, "character_id", characterID, "reason", err)
		return
	}
	if removed > 0 {
		s.logGameEvent(session, "game-upper-expired-rental-cleanup-applied", "source", source, "character_id", characterID, "removed_count", removed)
	}
}

func (s *Service) sendSelectedCurrentRentalPointState(session *gameSession, source string, refresh bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	repositories, characterID, _, err := s.currentRentalSelectedCharacter(ctx, session)
	if err != nil {
		s.logGameEvent(session, "game-upper-current-rental-state-deferred", "source", source, "reason", err)
		return nil
	}
	account, found, err := repositories.Account.Load(ctx, s.accountIDForSession(session))
	if err != nil || !found {
		s.logGameEvent(session, "game-upper-current-rental-state-deferred", "source", source, "character_id", characterID, "reason", errCurrentRentalStateMissing)
		return nil
	}
	points, err := currentRentalPoints(account)
	if err != nil {
		s.logGameEvent(session, "game-upper-current-rental-state-deferred", "source", source, "character_id", characterID, "reason", err)
		return nil
	}
	active := currentRentalActiveEntries(ctx, repositories, characterID, time.Now().UTC())
	body := buildCurrentRentalPointStateBody(points, active)
	s.logGameEvent(session, "game-upper-current-rental-state-send",
		"source", source,
		"character_id", characterID,
		"msg_id", currentRentalStateMsgID,
		"classification", 0,
		"points", points,
		"active_count", len(active),
		"body_len", len(body),
		"refresh", refresh,
		"body_source", "current_exe_sub_1AE2990_u32_points_u32_count_rows_item_expire")
	if err := s.sendGameUpperRawClass(session, currentRentalStateMsgID, body, 0); err != nil {
		return err
	}
	session.selectedRentalWalletStateSent = true
	return nil
}

func currentRentalActiveEntries(ctx context.Context, repositories dnfrepo.Group, characterID string, now time.Time) []currentRentalActiveEntry {
	active := make(map[uint32]uint32)
	if inventory, found, err := repositories.Inventory.Load(ctx, characterID); err == nil && found {
		for _, stack := range inventory.Slots {
			if !strings.EqualFold(strings.TrimSpace(stack.Extra["source"]), currentRentalItemSource) || stack.ItemID <= 0 || stack.ItemID > math.MaxUint32 {
				continue
			}
			expire := currentItemListStackExpire(stack)
			if expire <= uint32(now.Unix()) {
				continue
			}
			id := uint32(stack.ItemID)
			if expire > active[id] {
				active[id] = expire
			}
		}
	}
	if equipment, found, err := repositories.Equipment.Load(ctx, characterID); err == nil && found {
		for _, entry := range equipment.Entries {
			if !strings.EqualFold(strings.TrimSpace(entry.Extra["source"]), currentRentalItemSource) || entry.ItemID <= 0 || entry.ItemID > math.MaxUint32 {
				continue
			}
			expire := currentItemListEquipmentExpire(entry)
			if expire <= uint32(now.Unix()) {
				continue
			}
			id := uint32(entry.ItemID)
			if expire > active[id] {
				active[id] = expire
			}
		}
	}
	entries := make([]currentRentalActiveEntry, 0, len(active))
	for itemID, expire := range active {
		entries = append(entries, currentRentalActiveEntry{ItemID: itemID, ExpireUnix: expire})
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].ItemID < entries[right].ItemID })
	return entries
}
