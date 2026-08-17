package player

import (
	"errors"
	"strings"
	"time"

	"longheng.io/server/internal/platform/db"
)

// ErrAsyncStoreClosed 表示异步玩家存储已经关闭，不能再接收写入。
var ErrAsyncStoreClosed = errors.New("async player store is closed")

// AsyncStore 是带缓冲、重试和死信能力的玩家 Profile 异步存储。
type AsyncStore = db.AsyncStore[Profile, ProfileField]

// AsyncStoreStats 是玩家异步存储的待写、重试和死信统计。
type AsyncStoreStats = db.AsyncStoreStats

// AsyncStoreOptions 是玩家异步存储的刷新、重试、过期和死信配置。
type AsyncStoreOptions struct {
	FlushInterval   time.Duration
	MaxPending      int
	RetryBackoff    time.Duration
	MaxRetries      int
	AutoExpireTTL   time.Duration
	DeadLetterLimit int
	DeadLetterStore DeadLetterStore
}

// NewAsyncStore 创建玩家异步存储，base 为空时使用内存存储兜底。
func NewAsyncStore(base Store, options AsyncStoreOptions) *AsyncStore {
	if base == nil {
		base = NewMemoryStore()
	}
	return db.MustNewAsyncStore[Profile, ProfileField](base, db.AsyncStoreOptions[Profile, ProfileField]{
		FlushInterval:   options.FlushInterval,
		MaxPending:      options.MaxPending,
		RetryBackoff:    options.RetryBackoff,
		MaxRetries:      options.MaxRetries,
		AutoExpire:      options.AutoExpireTTL > 0,
		AutoExpireTTL:   options.AutoExpireTTL,
		DeadLetterLimit: options.DeadLetterLimit,
		DeadLetterStore: options.DeadLetterStore,
		RecordKey:       profileKey,
		NormalizeKey:    strings.TrimSpace,
		Clone:           cloneProfile,
		AllFields:       AllProfileFields,
		NormalizeFields: normProfileFields,
		SaveFields:      saveProfileFields,
		ClosedError:     ErrAsyncStoreClosed,
	})
}
