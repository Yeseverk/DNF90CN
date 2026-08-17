package player

import (
	"context"
	"strings"

	"longheng.io/server/internal/platform/db"
)

// 玩家 Profile、Redis/MySQL 字段记录、dirty-field helper、操作包装和回填分发
// 由 configs/profiledb/player.json 生成。
// 新游戏应替换该 Schema 及其默认值/回填 hook，而不是修改平台运行时。
//go:generate go run ../../../cmd/profilegen -schema ../../../configs/profiledb/player.json -out profile_schema_gen.go

// ProfileField 表示 profiledb schema 中可独立持久化的玩家字段。
type ProfileField string

// ProfileOperation 表示 profiledb schema 中声明的关键玩家操作。
type ProfileOperation string

// FieldStore 定义支持玩家 Profile 字段级保存的存储接口。
type FieldStore = db.FieldStore[Profile, ProfileField]

// ProfileFieldDescriptor 描述玩家 Profile 字段的模块、Hash 字段和列映射。
type ProfileFieldDescriptor struct {
	Field       ProfileField                   `json:"field"`
	Module      string                         `json:"module,omitempty"`
	HashField   string                         `json:"hash_field"`
	Record      string                         `json:"record"`
	Required    bool                           `json:"required,omitempty"`
	Backfill    bool                           `json:"backfill,omitempty"`
	DirtyFields []ProfileField                 `json:"dirty_fields,omitempty"`
	Description string                         `json:"description,omitempty"`
	Columns     []ProfileRecordFieldDescriptor `json:"columns,omitempty"`
}

// ProfileRecordFieldDescriptor 描述字段记录中的单个列映射。
type ProfileRecordFieldDescriptor struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	JSONName string `json:"json"`
	Profile  string `json:"profile"`
}

// ProfileOperationDescriptor 描述玩家操作和它会写脏的字段集合。
type ProfileOperationDescriptor struct {
	Operation   ProfileOperation `json:"operation"`
	Module      string           `json:"module,omitempty"`
	DirtyFields []ProfileField   `json:"dirty_fields,omitempty"`
	Description string           `json:"description,omitempty"`
}

var profileFieldRegistry = db.NewFieldRegistry(AllProfileFields())

type profileFieldSet = db.FieldSet[ProfileField]

func newProfileFieldSet(fields ...ProfileField) profileFieldSet {
	return profileFieldRegistry.NewSet(fields...)
}

func saveProfileFields(ctx context.Context, store Store, profile Profile, fields ...ProfileField) error {
	return db.SaveFields(ctx, store, profile, normProfileFields, fields...)
}

func profileKey(profile Profile) string {
	return strings.TrimSpace(profile.AccountID)
}

func normProfileID(profile Profile) Profile {
	return normAccountID(profile, profile.AccountID)
}

func normAccountID(profile Profile, accountID string) Profile {
	accountID = strings.TrimSpace(accountID)
	profile.AccountID = strings.TrimSpace(profile.AccountID)
	if profile.AccountID == "" {
		profile.AccountID = accountID
	}
	profile.RoleID = strings.TrimSpace(profile.RoleID)
	if profile.RoleID == "" && profile.AccountID != "" {
		profile.RoleID = "role-" + profile.AccountID
	}
	return profile
}
