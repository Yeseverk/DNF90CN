package player

import (
	"context"

	"longheng.io/server/internal/platform/readmodel"
)

// 玩家摘要 PlayerSummary、查询 PlayerSummaryQuery、投影、过滤器、Redis 命名和读模型选项
// 由 configs/readmodel/player_summary.json 生成。
//go:generate go run ../../../cmd/viewgen -schema ../../../configs/readmodel/player_summary.json -out summary_model_gen.go

// SummaryStore 定义玩家摘要读模型的读写和查询接口。
type SummaryStore interface {
	SavePlayerSummary(context.Context, PlayerSummary) error
	GetPlayerSummary(context.Context, string) (PlayerSummary, bool, error)
	GetPlayerSummaryByRoleID(context.Context, string) (PlayerSummary, bool, error)
	ListPlayerSummariesByAccountIDs(context.Context, []string) ([]PlayerSummary, error)
	ListByRoleIDs(context.Context, []string) ([]PlayerSummary, error)
	SearchPlayerSummaries(context.Context, PlayerSummaryQuery) ([]PlayerSummary, error)
}

// MemorySummaryStore 是玩家摘要读模型的内存实现。
type MemorySummaryStore struct {
	inner *readmodel.MemoryStore[PlayerSummary, PlayerSummaryQuery]
}

// NewMemorySummaryStore 创建内存玩家摘要读模型存储。
func NewMemorySummaryStore() *MemorySummaryStore {
	inner, err := readmodel.NewMemoryStore[PlayerSummary, PlayerSummaryQuery](summaryReadOpts())
	if err != nil {
		panic(err)
	}
	return &MemorySummaryStore{inner: inner}
}

// SavePlayerSummary 保存玩家摘要到内存读模型。
func (s *MemorySummaryStore) SavePlayerSummary(ctx context.Context, summary PlayerSummary) error {
	return s.inner.Save(ctx, summary)
}

// GetPlayerSummary 按账号 ID 从内存读模型读取玩家摘要。
func (s *MemorySummaryStore) GetPlayerSummary(ctx context.Context, accountID string) (PlayerSummary, bool, error) {
	return s.inner.Get(ctx, accountID)
}

// GetPlayerSummaryByRoleID 按角色 ID 从内存读模型读取玩家摘要。
func (s *MemorySummaryStore) GetPlayerSummaryByRoleID(ctx context.Context, roleID string) (PlayerSummary, bool, error) {
	return s.inner.GetBySecondaryID(ctx, roleID)
}

// ListPlayerSummariesByAccountIDs 按账号 ID 批量读取内存玩家摘要。
func (s *MemorySummaryStore) ListPlayerSummariesByAccountIDs(ctx context.Context, accountIDs []string) ([]PlayerSummary, error) {
	return s.inner.ListByPrimaryIDs(ctx, accountIDs)
}

// ListByRoleIDs 按角色 ID 批量读取内存玩家摘要。
func (s *MemorySummaryStore) ListByRoleIDs(ctx context.Context, roleIDs []string) ([]PlayerSummary, error) {
	return s.inner.ListBySecondaryIDs(ctx, roleIDs)
}

// SearchPlayerSummaries 按查询条件搜索内存玩家摘要。
func (s *MemorySummaryStore) SearchPlayerSummaries(ctx context.Context, query PlayerSummaryQuery) ([]PlayerSummary, error) {
	return s.inner.Search(ctx, query)
}
