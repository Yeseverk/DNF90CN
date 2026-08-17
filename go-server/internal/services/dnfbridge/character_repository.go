package dnfbridge

import (
	"context"
	"sort"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func (s *Service) listCharacters(ctx context.Context, repos dnfrepo.Group, accountID string) ([]dnfrepo.CharacterRecord, error) {
	if repos.Character == nil {
		return nil, errCharacterRepositoryMissing
	}
	records, err := repos.Character.ListByAccount(ctx, accountID, defaultCharacterSlots)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(records, func(i, j int) bool {
		if records[i].Slot == records[j].Slot {
			return records[i].CharacterID < records[j].CharacterID
		}
		return records[i].Slot < records[j].Slot
	})
	for idx := range records {
		s.attachEquipmentSummary(ctx, repos, &records[idx])
	}
	return records, nil
}

func (s *Service) characterNameExists(ctx context.Context, repos dnfrepo.Group, records []dnfrepo.CharacterRecord, name string) (bool, error) {
	if repos.Character == nil {
		return false, errCharacterRepositoryMissing
	}
	_, found, err := repos.Character.FindIDByName(ctx, name)
	return found, err
}

func (s *Service) nextCharacterID(ctx context.Context, repos dnfrepo.Group, records []dnfrepo.CharacterRecord) (int, error) {
	if repos.Character == nil {
		return 0, errCharacterRepositoryMissing
	}
	return repos.Character.NextNumericID(ctx)
}

func nextCharacterSlot(records []dnfrepo.CharacterRecord) int {
	used := make(map[int]struct{}, len(records))
	for _, record := range records {
		used[record.Slot] = struct{}{}
	}
	for slot := 0; slot < defaultCharacterSlots; slot++ {
		if _, ok := used[slot]; !ok {
			return slot
		}
	}
	return len(records)
}
