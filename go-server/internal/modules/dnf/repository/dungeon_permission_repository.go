package repository

import (
	"context"
	"strings"
	"time"
)

type DungeonPermissionEntry struct {
	DungeonID  uint32    `json:"dungeon_id"`
	ClearState byte      `json:"clear_state"`
	SortOrder  int       `json:"sort_order,omitempty"`
	UpdatedAt  time.Time `json:"updated_at,omitempty"`
}

type DungeonPermissionRecord struct {
	CharacterID string                   `json:"character_id"`
	Entries     []DungeonPermissionEntry `json:"entries,omitempty"`
	UpdatedAt   time.Time                `json:"updated_at,omitempty"`
}

type DungeonPermissionRepository interface {
	Load(context.Context, string) (DungeonPermissionRecord, bool, error)
	UpsertMax(context.Context, string, uint32, byte) (DungeonPermissionEntry, bool, error)
}

func dungeonPermissionKey(record DungeonPermissionRecord) string {
	return strings.TrimSpace(record.CharacterID)
}

func CloneDungeonPermission(record DungeonPermissionRecord) DungeonPermissionRecord {
	record.CharacterID = strings.TrimSpace(record.CharacterID)
	if len(record.Entries) != 0 {
		record.Entries = append([]DungeonPermissionEntry(nil), record.Entries...)
	}
	return record
}
