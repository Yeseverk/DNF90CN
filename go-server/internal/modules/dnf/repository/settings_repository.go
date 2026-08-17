// 本文件定义 DNF 设置仓储接口、记录和字段保存入口。
package repository

import (
	"context"
	"strings"
	"time"

	"longheng.io/server/internal/platform/db"
)

// SettingsRepository 保存账号、角色或全局设置。
type SettingsRepository interface {
	db.Store[SettingsRecord]
}

// SettingsRecord 是账号、角色或全局设置仓储记录。
type SettingsRecord struct {
	Scope     string            `json:"scope"`
	Values    map[string]string `json:"values,omitempty"`
	UpdatedAt time.Time         `json:"updated_at,omitempty"`
}

// SettingsField 表示设置记录可局部保存的字段。
type SettingsField string

const (
	SettingsFieldValues SettingsField = "values"
)

// SaveSettingsFields 保存设置指定字段；底层不支持局部保存时退化为整条保存。
func SaveSettingsFields(ctx context.Context, repo SettingsRepository, record SettingsRecord, fields ...SettingsField) error {
	return db.SaveFields(ctx, repo, record, SettingsFields.Normalize, fields...)
}

// CloneSettings 拷贝设置记录，避免 Values map 与调用方共享。
func CloneSettings(record SettingsRecord) SettingsRecord {
	record.Values = cloneStringMap(record.Values)
	return record
}

func SettingsKey(record SettingsRecord) string {
	return strings.TrimSpace(record.Scope)
}
