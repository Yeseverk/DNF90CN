package mysql

import (
	"context"
	"database/sql"
	"longheng.io/server/internal/modules/dnf/repository"
	"strings"
	"time"

	"longheng.io/server/internal/platform/db"
)

const mysqlDungeonPermissionTable = "dungeon_permissions"

type mysqlDungeonPermissionStore struct {
	mysqlStoreBase
}

func (s *mysqlDungeonPermissionStore) Load(
	ctx context.Context,
	characterID string,
) (repository.DungeonPermissionRecord, bool, error) {
	characterID = strings.TrimSpace(characterID)
	if characterID == "" {
		return repository.DungeonPermissionRecord{}, false, db.ErrRecordKeyRequired
	}
	table, err := s.router.readTable(mysqlDungeonPermissionTable, characterID)
	if err != nil {
		return repository.DungeonPermissionRecord{}, false, err
	}
	query := s.router.selectQuery(
		"SELECT dungeon_id, clear_state, sort_order, updated_at FROM " + table +
			" WHERE character_id = ? ORDER BY sort_order, dungeon_id")
	rows, err := s.router.db.QueryContext(ctx, query, characterID)
	if err != nil {
		return repository.DungeonPermissionRecord{}, false, err
	}
	defer rows.Close()
	record := repository.DungeonPermissionRecord{CharacterID: characterID}
	for rows.Next() {
		var dungeonID uint64
		var clearState uint8
		var sortOrder int
		var updatedAt sql.NullTime
		if err := rows.Scan(&dungeonID, &clearState, &sortOrder, &updatedAt); err != nil {
			return repository.DungeonPermissionRecord{}, false, err
		}
		if dungeonID == 0 || dungeonID > uint64(^uint32(0)) || clearState == 0 {
			continue
		}
		entry := repository.DungeonPermissionEntry{
			DungeonID:  uint32(dungeonID),
			ClearState: byte(clearState),
			SortOrder:  sortOrder,
			UpdatedAt:  scanTime(updatedAt),
		}
		if entry.UpdatedAt.After(record.UpdatedAt) {
			record.UpdatedAt = entry.UpdatedAt
		}
		record.Entries = append(record.Entries, entry)
	}
	if err := rows.Err(); err != nil {
		return repository.DungeonPermissionRecord{}, false, err
	}
	if len(record.Entries) == 0 {
		return repository.DungeonPermissionRecord{}, false, nil
	}
	return repository.CloneDungeonPermission(record), true, nil
}

func (s *mysqlDungeonPermissionStore) UpsertMax(
	ctx context.Context,
	characterID string,
	dungeonID uint32,
	clearState byte,
) (repository.DungeonPermissionEntry, bool, error) {
	characterID = strings.TrimSpace(characterID)
	if characterID == "" {
		return repository.DungeonPermissionEntry{}, false, db.ErrRecordKeyRequired
	}
	if dungeonID == 0 || clearState == 0 {
		return repository.DungeonPermissionEntry{}, false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return repository.DungeonPermissionEntry{}, false, err
	}
	table, err := s.router.writeTable(mysqlDungeonPermissionTable, characterID)
	if err != nil {
		return repository.DungeonPermissionEntry{}, false, err
	}
	now := timeOrNow(time.Time{}, s.router.now)
	var existingState uint8
	var existingOrder int
	var existingUpdated sql.NullTime
	err = s.router.db.QueryRowContext(ctx,
		s.router.selectQuery("SELECT clear_state, sort_order, updated_at FROM "+table+" WHERE character_id = ? AND dungeon_id = ?"),
		characterID,
		uint64(dungeonID),
	).Scan(&existingState, &existingOrder, &existingUpdated)
	if err == nil {
		entry := repository.DungeonPermissionEntry{
			DungeonID:  dungeonID,
			ClearState: byte(existingState),
			SortOrder:  existingOrder,
			UpdatedAt:  scanTime(existingUpdated),
		}
		if existingState >= uint8(clearState) {
			return entry, false, nil
		}
		result, err := s.router.db.ExecContext(ctx,
			"UPDATE "+table+" SET clear_state = ?, updated_at = ? WHERE character_id = ? AND dungeon_id = ? AND clear_state < ?",
			uint8(clearState),
			sqlTime(now),
			characterID,
			uint64(dungeonID),
			uint8(clearState),
		)
		if err != nil {
			return repository.DungeonPermissionEntry{}, false, err
		}
		affected, _ := result.RowsAffected()
		entry.ClearState = clearState
		entry.UpdatedAt = now
		return entry, affected > 0, nil
	}
	if !errorsIsSQLNoRows(err) {
		return repository.DungeonPermissionEntry{}, false, err
	}

	var nextOrder sql.NullInt64
	if err := s.router.db.QueryRowContext(ctx,
		s.router.selectQuery("SELECT COALESCE(MAX(sort_order), -1) + 1 FROM "+table+" WHERE character_id = ?"),
		characterID,
	).Scan(&nextOrder); err != nil {
		return repository.DungeonPermissionEntry{}, false, err
	}
	sortOrder := 0
	if nextOrder.Valid && nextOrder.Int64 > 0 {
		sortOrder = int(nextOrder.Int64)
	}
	result, err := s.router.db.ExecContext(ctx,
		"INSERT IGNORE INTO "+table+" (character_id, dungeon_id, clear_state, sort_order, updated_at) VALUES (?, ?, ?, ?, ?)",
		characterID,
		uint64(dungeonID),
		uint8(clearState),
		sortOrder,
		sqlTime(now),
	)
	if err != nil {
		return repository.DungeonPermissionEntry{}, false, err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		// Another writer inserted first. Re-run through the update/no-op branch.
		return s.UpsertMax(ctx, characterID, dungeonID, clearState)
	}
	return repository.DungeonPermissionEntry{
		DungeonID:  dungeonID,
		ClearState: clearState,
		SortOrder:  sortOrder,
		UpdatedAt:  now,
	}, true, nil
}

func errorsIsSQLNoRows(err error) bool {
	return err == sql.ErrNoRows
}
