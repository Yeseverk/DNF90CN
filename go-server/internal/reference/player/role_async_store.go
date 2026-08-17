package player

import (
	"context"
	"fmt"
)

// RoleAwareAsyncStore 在异步写入外保留角色 ID 查询和扫描能力。
type RoleAwareAsyncStore struct {
	*AsyncStore
	roleStore RoleProfileStore
}

// NewRoleAwareAsyncStore 创建带角色查询能力的异步玩家存储包装器。
func NewRoleAwareAsyncStore(async *AsyncStore, roleStore RoleProfileStore) *RoleAwareAsyncStore {
	return &RoleAwareAsyncStore{
		AsyncStore: async,
		roleStore:  roleStore,
	}
}

// LoadByRoleID 通过底层角色索引存储读取玩家 Profile。
func (s *RoleAwareAsyncStore) LoadByRoleID(ctx context.Context, roleID string) (Profile, bool, error) {
	if s == nil || s.roleStore == nil {
		return Profile{}, false, nil
	}
	return s.roleStore.LoadByRoleID(ctx, roleID)
}

// ListProfiles 通过底层扫描存储按账号游标列出玩家 Profile。
func (s *RoleAwareAsyncStore) ListProfiles(ctx context.Context, afterAccountID string, limit int) ([]Profile, error) {
	if s == nil || s.roleStore == nil {
		return nil, fmt.Errorf("profile store is nil")
	}
	scanner, ok := s.roleStore.(ProfileScanStore)
	if !ok {
		return nil, fmt.Errorf("profile store %T does not support profile scans", s.roleStore)
	}
	return scanner.ListProfiles(ctx, afterAccountID, limit)
}
