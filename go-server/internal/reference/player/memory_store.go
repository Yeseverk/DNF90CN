package player

import (
	"context"
	"fmt"
	"strings"

	"longheng.io/server/internal/platform/db"
)

// MemoryStore 是用于测试、开发和 Lite 场景的内存玩家 Profile 存储。
type MemoryStore struct {
	inner *db.MemoryStore[Profile]
}

// NewMemoryStore 创建内存玩家 Profile 存储。
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		inner: db.NewMemoryStore(profileKey, cloneProfile),
	}
}

// Load 按账号 ID 从内存读取玩家 Profile。
func (s *MemoryStore) Load(ctx context.Context, accountID string) (Profile, bool, error) {
	return s.inner.Load(ctx, strings.TrimSpace(accountID))
}

// LoadByRoleID 按角色 ID 从内存扫描玩家 Profile。
func (s *MemoryStore) LoadByRoleID(ctx context.Context, roleID string) (Profile, bool, error) {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return Profile{}, false, err
	}
	roleID = strings.TrimSpace(roleID)
	if roleID == "" {
		return Profile{}, false, nil
	}
	for _, profile := range s.Snapshot() {
		if profile.RoleID == roleID {
			return profile, true, nil
		}
	}
	return Profile{}, false, nil
}

// ListProfiles 按账号 ID 游标列出内存玩家 Profile。
func (s *MemoryStore) ListProfiles(ctx context.Context, afterAccountID string, limit int) ([]Profile, error) {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	afterAccountID = strings.TrimSpace(afterAccountID)
	if limit <= 0 {
		return nil, nil
	}
	snapshot := s.Snapshot()
	out := make([]Profile, 0, min(limit, len(snapshot)))
	for _, profile := range snapshot {
		if afterAccountID != "" && profile.AccountID <= afterAccountID {
			continue
		}
		out = append(out, profile)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// Check 检查内存存储状态。
func (s *MemoryStore) Check(ctx context.Context) error {
	return s.inner.Check(ctx)
}

// Save 保存完整玩家 Profile 到内存。
func (s *MemoryStore) Save(ctx context.Context, profile Profile) error {
	profile = normProfileID(profile)
	if profile.AccountID == "" {
		return fmt.Errorf("account id is required")
	}
	return s.inner.Save(ctx, profile)
}

// SaveFields 保存玩家 Profile 指定字段，内存存储会退化为全量保存。
func (s *MemoryStore) SaveFields(ctx context.Context, profile Profile, _ ...ProfileField) error {
	return s.Save(ctx, profile)
}

// Snapshot 返回按账号 ID 排序的玩家 Profile 快照。
func (s *MemoryStore) Snapshot() []Profile {
	return s.inner.Snapshot(func(left, right Profile) bool {
		return left.AccountID < right.AccountID
	})
}
