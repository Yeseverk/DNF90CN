// 本文件提供角色仓储的内存实现。
// 它只服务本地开发和测试，不作为生产持久化后端。
package memory

import (
	"context"
	"longheng.io/server/internal/modules/dnf/repository"
	"strconv"
	"strings"
	"sync"

	"longheng.io/server/internal/platform/db"
)

type memoryCharStore struct {
	*db.MemoryStore[repository.CharacterRecord]
	storyDigestMu sync.Mutex
}

func newMemoryCharStore() *memoryCharStore {
	return &memoryCharStore{
		MemoryStore: db.NewMemoryStore(repository.CharacterKey, repository.CloneCharacter),
	}
}

// Save preserves the two monotonic story-summary scalars when an unrelated
// character-stats writer saves a stale aggregate snapshot.
func (s *memoryCharStore) Save(ctx context.Context, record repository.CharacterRecord) error {
	s.storyDigestMu.Lock()
	defer s.storyDigestMu.Unlock()
	return s.savePreservingStoryDigest(ctx, record)
}

func (s *memoryCharStore) savePreservingStoryDigest(ctx context.Context, record repository.CharacterRecord) error {
	if existing, found, err := s.MemoryStore.Load(ctx, record.CharacterID); err != nil {
		return err
	} else if found {
		if record.Stats == nil {
			record.Stats = make(map[string]int64, 2)
		}
		for _, key := range []string{
			repository.CharacterStoryDigestLastLevelStatKey,
			repository.CharacterStoryDigestMigrationVersionStatKey,
		} {
			if existing.Stats[key] > record.Stats[key] {
				record.Stats[key] = existing.Stats[key]
			}
		}
	}
	return s.MemoryStore.Save(ctx, record)
}

func (s *memoryCharStore) AdvanceStoryDigest(ctx context.Context, characterID string, level, migrationVersion uint32) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	characterID = strings.TrimSpace(characterID)
	if characterID == "" {
		return repository.ErrCharacterStoryDigestCharacterMissing
	}
	s.storyDigestMu.Lock()
	defer s.storyDigestMu.Unlock()

	record, ok, err := s.Load(ctx, characterID)
	if err != nil {
		return err
	}
	if !ok {
		return repository.ErrCharacterStoryDigestCharacterMissing
	}
	if record.Stats == nil {
		record.Stats = make(map[string]int64, 2)
	}
	if int64(level) > record.Stats[repository.CharacterStoryDigestLastLevelStatKey] {
		record.Stats[repository.CharacterStoryDigestLastLevelStatKey] = int64(level)
	}
	if int64(migrationVersion) > record.Stats[repository.CharacterStoryDigestMigrationVersionStatKey] {
		record.Stats[repository.CharacterStoryDigestMigrationVersionStatKey] = int64(migrationVersion)
	}
	return s.MemoryStore.Save(ctx, record)
}

// CreateCharacter 只用于测试/本地开发的新建路径，模拟 MySQL 的主键和槽位唯一约束。
func (s *memoryCharStore) CreateCharacter(ctx context.Context, record repository.CharacterRecord) error {
	if _, ok, err := s.Load(ctx, record.CharacterID); err != nil || ok {
		if err != nil {
			return err
		}
		return repository.ErrCharacterIDExists
	}
	records, err := s.ListByAccount(ctx, record.AccountID, 0)
	if err != nil {
		return err
	}
	for _, existing := range records {
		if existing.Slot == record.Slot {
			return repository.ErrCharacterSlotOccupied
		}
	}
	return s.Save(ctx, record)
}

// ListByAccount 从内存快照中按槽位顺序返回账号角色列表。
// 该实现只用于测试和本地开发，不写 MySQL/Redis，也不做角色创建规则判断。
func (s *memoryCharStore) ListByAccount(ctx context.Context, accountID string, limit int) ([]repository.CharacterRecord, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return nil, db.ErrRecordKeyRequired
	}
	if limit <= 0 {
		limit = repository.DefaultCharacterSlotLimit
	}
	records := s.Snapshot(func(a, b repository.CharacterRecord) bool {
		if a.Slot == b.Slot {
			return a.CharacterID < b.CharacterID
		}
		return a.Slot < b.Slot
	})
	out := make([]repository.CharacterRecord, 0, minInt(limit, len(records)))
	for _, record := range records {
		if record.AccountID != accountID {
			continue
		}
		if characterDeleteFlag(record) != 0 {
			continue
		}
		out = append(out, repository.CloneCharacter(record))
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (s *memoryCharStore) SwapCharacterSlots(ctx context.Context, accountID string, slotA, slotB int) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return db.ErrRecordKeyRequired
	}
	if slotA == slotB {
		return nil
	}
	if slotA < 0 || slotA >= repository.DefaultCharacterSlotLimit || slotB < 0 || slotB >= repository.DefaultCharacterSlotLimit {
		return repository.ErrCharacterSlotMissing
	}
	records, err := s.ListByAccount(ctx, accountID, repository.DefaultCharacterSlotLimit)
	if err != nil {
		return err
	}
	var left, right repository.CharacterRecord
	leftFound, rightFound := false, false
	for _, record := range records {
		switch record.Slot {
		case slotA:
			left, leftFound = record, true
		case slotB:
			right, rightFound = record, true
		}
	}
	if !leftFound || !rightFound {
		return nil
	}
	left.Slot, right.Slot = slotB, slotA
	if err := s.Save(ctx, left); err != nil {
		return err
	}
	if err := s.Save(ctx, right); err != nil {
		left.Slot = slotA
		_ = s.Save(context.Background(), left)
		return err
	}
	return nil
}

// FindIDByName 从内存快照中按角色名查找角色 ID。
// 这里只提供测试后端的查询能力，生产路径由 MySQL 索引和 Redis 名字缓存承担。
func (s *memoryCharStore) FindIDByName(ctx context.Context, name string) (string, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return "", false, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", false, nil
	}
	for _, record := range s.Snapshot(nil) {
		if record.Name == name && characterDeleteFlag(record) == 0 {
			return record.CharacterID, true, nil
		}
	}
	return "", false, nil
}

func characterDeleteFlag(record repository.CharacterRecord) int64 {
	if record.Stats == nil {
		return 0
	}
	if value, ok := record.Stats["delete_flag"]; ok {
		return value
	}
	return 0
}

// NextNumericID 扫描内存角色 ID 并返回下一个数字 ID。
// 该实现不提供跨进程唯一性，只用于测试；生产路径使用 MySQL 最大值加 Redis 原子自增。
func (s *memoryCharStore) NextNumericID(ctx context.Context) (int, error) {
	if err := ctxErr(ctx); err != nil {
		return 0, err
	}
	maxID := 0
	for _, record := range s.Snapshot(nil) {
		id, err := strconv.Atoi(strings.TrimSpace(record.CharacterID))
		if err == nil && id > maxID {
			maxID = id
		}
	}
	return maxID + 1, nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
