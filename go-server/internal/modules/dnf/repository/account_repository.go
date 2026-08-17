// 本文件定义 DNF 账号仓储接口和记录。
package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"longheng.io/server/internal/platform/db"
)

// AccountRepository 保存账号级可变状态。
type AccountRepository interface {
	db.Store[AccountRecord]
}

// AccountMetadataEntryWriter persists one normalized account-metadata row
// without replacing unrelated keys.
type AccountMetadataEntryWriter interface {
	SaveMetadataEntry(context.Context, string, string, string, time.Time) error
}

// RepresentAccountNameFinder exposes the account-wide nickname uniqueness
// lookup required by the current client's adventure-group registration flow.
// It is kept separate from AccountRepository so transaction-scoped stores do
// not need to support a cross-account query.
type RepresentAccountNameFinder interface {
	FindAccountIDByRepresentName(context.Context, string) (string, bool, error)
}

var ErrRepresentAccountNameExists = errors.New("dnf represent account name already exists")

var (
	ErrAccountMetadataKeyRequired = errors.New("dnf account metadata key is required")
	ErrAccountMetadataUnavailable = errors.New("dnf account metadata repository is unavailable")
)

// AccountRecord 是 DNF 账号仓储记录。
type AccountRecord struct {
	AccountID            string            `json:"account_id"`
	State                string            `json:"state,omitempty"`
	HonorExp             uint64            `json:"honor_exp,omitempty"`
	RepresentAccountName string            `json:"represent_account_name,omitempty"`
	Metadata             map[string]string `json:"metadata,omitempty"`
	CreatedAt            time.Time         `json:"created_at,omitempty"`
	UpdatedAt            time.Time         `json:"updated_at,omitempty"`
}

// CloneAccount 拷贝账号记录，避免调用方和底层存储共享 Metadata map。
func CloneAccount(record AccountRecord) AccountRecord {
	record.Metadata = cloneStringMap(record.Metadata)
	return record
}

// SaveAccountMetadataEntry updates one metadata key. MySQL and memory stores
// implement a native key-level write; other repositories fall back to saving
// the supplied authoritative account snapshot.
func SaveAccountMetadataEntry(
	ctx context.Context,
	repo AccountRepository,
	account AccountRecord,
	key string,
	value string,
	updatedAt time.Time,
) error {
	if repo == nil {
		return ErrAccountMetadataUnavailable
	}
	accountID := strings.TrimSpace(account.AccountID)
	if accountID == "" {
		return db.ErrRecordKeyRequired
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return ErrAccountMetadataKeyRequired
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	updatedAt = updatedAt.UTC()
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	if writer, ok := repo.(AccountMetadataEntryWriter); ok {
		return writer.SaveMetadataEntry(ctx, accountID, key, value, updatedAt)
	}
	account = CloneAccount(account)
	if account.Metadata == nil {
		account.Metadata = make(map[string]string)
	}
	account.Metadata[key] = value
	account.UpdatedAt = updatedAt
	return repo.Save(ctx, account)
}

func AccountKey(record AccountRecord) string {
	return strings.TrimSpace(record.AccountID)
}
