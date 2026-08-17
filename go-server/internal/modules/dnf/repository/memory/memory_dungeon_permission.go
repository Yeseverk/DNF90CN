package memory

import (
	"context"
	"longheng.io/server/internal/modules/dnf/repository"
	"strings"
	"sync"
	"time"

	"longheng.io/server/internal/platform/db"
)

type memoryDungeonPermissionStore struct {
	mu      sync.RWMutex
	records map[string]repository.DungeonPermissionRecord
	now     func() time.Time
}

func newMemoryDungeonPermissionStore() *memoryDungeonPermissionStore {
	return &memoryDungeonPermissionStore{records: make(map[string]repository.DungeonPermissionRecord)}
}

func (s *memoryDungeonPermissionStore) Check(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	return ctx.Err()
}

func (s *memoryDungeonPermissionStore) Load(ctx context.Context, characterID string) (repository.DungeonPermissionRecord, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return repository.DungeonPermissionRecord{}, false, err
	}
	characterID = strings.TrimSpace(characterID)
	if characterID == "" {
		return repository.DungeonPermissionRecord{}, false, db.ErrRecordKeyRequired
	}
	s.mu.RLock()
	record, ok := s.records[characterID]
	s.mu.RUnlock()
	if !ok {
		return repository.DungeonPermissionRecord{}, false, nil
	}
	return repository.CloneDungeonPermission(record), true, nil
}

func (s *memoryDungeonPermissionStore) UpsertMax(
	ctx context.Context,
	characterID string,
	dungeonID uint32,
	clearState byte,
) (repository.DungeonPermissionEntry, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return repository.DungeonPermissionEntry{}, false, err
	}
	characterID = strings.TrimSpace(characterID)
	if characterID == "" {
		return repository.DungeonPermissionEntry{}, false, db.ErrRecordKeyRequired
	}
	if dungeonID == 0 || clearState == 0 {
		return repository.DungeonPermissionEntry{}, false, nil
	}
	now := timeOrNow(time.Time{}, s.now)
	s.mu.Lock()
	defer s.mu.Unlock()
	record := repository.CloneDungeonPermission(s.records[characterID])
	record.CharacterID = characterID
	record.UpdatedAt = now
	for idx := range record.Entries {
		if record.Entries[idx].DungeonID != dungeonID {
			continue
		}
		if record.Entries[idx].ClearState >= clearState {
			return record.Entries[idx], false, nil
		}
		record.Entries[idx].ClearState = clearState
		record.Entries[idx].UpdatedAt = now
		s.records[characterID] = repository.CloneDungeonPermission(record)
		return record.Entries[idx], true, nil
	}
	entry := repository.DungeonPermissionEntry{
		DungeonID:  dungeonID,
		ClearState: clearState,
		SortOrder:  len(record.Entries),
		UpdatedAt:  now,
	}
	record.Entries = append(record.Entries, entry)
	s.records[characterID] = repository.CloneDungeonPermission(record)
	return entry, true, nil
}
