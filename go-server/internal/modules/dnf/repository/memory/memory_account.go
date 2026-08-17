package memory

import (
	"context"
	"longheng.io/server/internal/modules/dnf/repository"
	"strings"
	"sync"
	"time"

	"longheng.io/server/internal/platform/db"
)

type memoryAccountStore struct {
	*db.MemoryStore[repository.AccountRecord]
	nameMu sync.Mutex
}

func newMemoryAccountStore() *memoryAccountStore {
	return &memoryAccountStore{
		MemoryStore: db.NewMemoryStore(repository.AccountKey, repository.CloneAccount),
	}
}

func (s *memoryAccountStore) Save(ctx context.Context, record repository.AccountRecord) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	s.nameMu.Lock()
	defer s.nameMu.Unlock()
	name := strings.TrimSpace(record.RepresentAccountName)
	if name != "" {
		for _, existing := range s.Snapshot(nil) {
			if existing.AccountID != record.AccountID && strings.EqualFold(strings.TrimSpace(existing.RepresentAccountName), name) {
				return repository.ErrRepresentAccountNameExists
			}
		}
	}
	record.RepresentAccountName = name
	return s.MemoryStore.Save(ctx, record)
}

func (s *memoryAccountStore) SaveMetadataEntry(
	ctx context.Context,
	accountID string,
	key string,
	value string,
	updatedAt time.Time,
) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	accountID = strings.TrimSpace(accountID)
	key = strings.TrimSpace(key)
	if accountID == "" {
		return db.ErrRecordKeyRequired
	}
	if key == "" {
		return repository.ErrAccountMetadataKeyRequired
	}
	s.nameMu.Lock()
	defer s.nameMu.Unlock()
	record, found, err := s.MemoryStore.Load(ctx, accountID)
	if err != nil {
		return err
	}
	if !found {
		return repository.ErrAccountMetadataUnavailable
	}
	record = repository.CloneAccount(record)
	if record.Metadata == nil {
		record.Metadata = make(map[string]string)
	}
	record.Metadata[key] = value
	record.UpdatedAt = updatedAt.UTC()
	return s.MemoryStore.Save(ctx, record)
}

func (s *memoryAccountStore) FindAccountIDByRepresentName(ctx context.Context, name string) (string, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return "", false, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", false, nil
	}
	for _, record := range s.Snapshot(nil) {
		if strings.EqualFold(strings.TrimSpace(record.RepresentAccountName), name) {
			return record.AccountID, true, nil
		}
	}
	return "", false, nil
}
