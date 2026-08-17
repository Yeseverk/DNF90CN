package player

import (
	"context"
	"strings"

	"longheng.io/server/internal/platform/db"
)

// FileStore 用本地 JSON 文件保存玩家 Profile，适合开发和单机演示。
type FileStore struct {
	inner *db.JSONFileStore[Profile]
}

// NewFileStore 创建文件型玩家 Profile 存储。
func NewFileStore(dir string) *FileStore {
	return &FileStore{
		inner: db.NewJSONFileStore(db.JSONFileStoreOptions[Profile]{
			Directory:        dir,
			DefaultDirectory: "data/profiles",
			Key:              profileKey,
			Clone:            cloneProfile,
			WithLoadedKey:    normAccountID,
		}),
	}
}

// Load 按账号 ID 从文件存储读取玩家 Profile。
func (s *FileStore) Load(ctx context.Context, accountID string) (Profile, bool, error) {
	return s.inner.Load(ctx, strings.TrimSpace(accountID))
}

// Check 检查文件存储目录是否可用。
func (s *FileStore) Check(ctx context.Context) error {
	return s.inner.Check(ctx)
}

// Save 保存完整玩家 Profile 到文件存储。
func (s *FileStore) Save(ctx context.Context, profile Profile) error {
	return s.inner.Save(ctx, normProfileID(profile))
}

// SaveFields 保存玩家 Profile 指定字段，文件存储会退化为全量保存。
func (s *FileStore) SaveFields(ctx context.Context, profile Profile, _ ...ProfileField) error {
	return s.Save(ctx, profile)
}
