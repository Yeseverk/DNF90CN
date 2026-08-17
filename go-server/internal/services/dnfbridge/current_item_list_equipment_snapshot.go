package dnfbridge

import (
	"context"
	"strconv"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func (s *Service) buildCurrentEquippedItemListBodyForSession(ctx context.Context, session *gameSession) ([]byte, string, int, bool) {
	repos, ok := s.repositoryGroup()
	if !ok {
		return nil, "", 0, false
	}
	characterID := strconv.Itoa(int(session.selectedCharacterID))
	entries, known, err := currentItemListEntriesFromEquipment(ctx, repos.Equipment, characterID)
	if err != nil {
		s.logPacketEvent("game-upper-current-item-list-equipment-load-failed",
			"conn_id", session.connID,
			"char_id", session.selectedCharacterID,
			"list_type", 3,
			"err", err)
	}
	if !known {
		return nil, "", 0, false
	}
	patchedUsePeriods, usePeriodErr := s.applyCurrentPVFUsePeriodsToEntriesWithLoadedCatalog(ctx, entries)
	if usePeriodErr != nil {
		s.logPacketEvent("game-upper-current-equipment-use-period-wire-projection-failed",
			"conn_id", session.connID,
			"char_id", session.selectedCharacterID,
			"list_type", 3,
			"err", usePeriodErr)
	}
	source := "equipment"
	if patchedUsePeriods > 0 {
		source += "+runtime_pvf_wrong_expiration_cleanup"
	}
	return buildCurrentItemListBody(3, entries, dnfrepo.CharacterContainerState{}), source, len(entries), true
}

func (s *Service) buildCurrentEquipmentItemUpdateBodyForSession(ctx context.Context, session *gameSession) ([]byte, string, int, bool) {
	repos, ok := s.repositoryGroup()
	if !ok {
		return nil, "", 0, false
	}
	characterID := strconv.Itoa(int(session.selectedCharacterID))
	entries, known, err := currentEquipmentUpdateEntriesFromEquipment(ctx, repos.Equipment, characterID)
	if err != nil {
		s.logPacketEvent("game-upper-current-equipment-item-update-load-failed",
			"conn_id", session.connID,
			"char_id", session.selectedCharacterID,
			"list_type", 3,
			"err", err)
	}
	if !known {
		return nil, "", 0, false
	}
	source := "equipment"
	return buildCurrentEquipmentUpdateBody(3, entries), source, len(entries), true
}

func currentItemListEntriesFromEquipment(ctx context.Context, repo dnfrepo.EquipmentRepository, characterID string) ([]currentItemListEntry, bool, error) {
	if repo == nil {
		return nil, false, nil
	}
	record, found, err := repo.Load(ctx, characterID)
	if err != nil || !found {
		return nil, found, err
	}
	if len(record.Entries) == 0 {
		return nil, true, nil
	}
	entries := make([]currentItemListEntry, 0, len(record.Entries))
	for _, equipped := range record.Entries {
		if equipped.SlotIndex < 0 || equipped.ItemID <= 0 {
			continue
		}
		entry, ok := currentItemListEntryFromEquipment(equipped)
		if !ok {
			continue
		}
		entries = append(entries, entry)
	}
	sortCurrentItemListEntries(entries)
	return entries, true, nil
}

func currentEquipmentUpdateEntriesFromEquipment(ctx context.Context, repo dnfrepo.EquipmentRepository, characterID string) ([]currentEquipmentUpdateEntry, bool, error) {
	if repo == nil {
		return nil, false, nil
	}
	record, found, err := repo.Load(ctx, characterID)
	if err != nil || !found {
		return nil, found, err
	}
	if len(record.Entries) == 0 {
		return nil, true, nil
	}
	entries := make([]currentEquipmentUpdateEntry, 0, len(record.Entries))
	for _, equipped := range record.Entries {
		if equipped.SlotIndex < 0 || equipped.ItemID <= 0 {
			continue
		}
		entries = append(entries, currentEquipmentUpdateEntryFromEquipment(equipped))
	}
	sortCurrentEquipmentUpdateEntries(entries)
	return entries, true, nil
}
