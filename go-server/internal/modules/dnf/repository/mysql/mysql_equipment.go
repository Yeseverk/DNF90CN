// 本文件实现 DNF 穿戴装备仓储的 MySQL 字段化读写。
package mysql

import (
	"context"
	"database/sql"
	"longheng.io/server/internal/modules/dnf/repository"
)

const mysqlEquipmentTable = "equipments"

type mysqlEquipmentStore struct {
	mysqlStoreBase
}

// Load 按角色 ID 从 MySQL 读取穿戴装备记录。
func (s *mysqlEquipmentStore) Load(ctx context.Context, characterID string) (repository.EquipmentRecord, bool, error) {
	table, err := s.router.readTable(mysqlEquipmentTable, characterID)
	if err != nil {
		return repository.EquipmentRecord{}, false, err
	}
	entriesTable, err := s.router.readTable(mysqlEquipmentEntriesTable, characterID)
	if err != nil {
		return repository.EquipmentRecord{}, false, err
	}
	extraTable, err := s.router.readTable(mysqlEquipmentExtraTable, characterID)
	if err != nil {
		return repository.EquipmentRecord{}, false, err
	}
	query := s.router.selectQuery("SELECT character_id, updated_at FROM " + table + " WHERE character_id = ?")
	var record repository.EquipmentRecord
	var updatedAt sql.NullTime
	err = s.router.db.QueryRowContext(ctx, query, characterID).Scan(
		&record.CharacterID,
		&updatedAt,
	)
	if err != nil {
		ok, scanErr := scanErr(err)
		return repository.EquipmentRecord{}, ok, scanErr
	}
	record.Entries, err = loadEquipmentEntries(ctx, s.router, entriesTable, extraTable, characterID)
	if err != nil {
		return repository.EquipmentRecord{}, false, err
	}
	record.UpdatedAt = scanTime(updatedAt)
	return repository.CloneEquipment(record), true, nil
}

// Save 保存完整穿戴装备记录。
func (s *mysqlEquipmentStore) Save(ctx context.Context, record repository.EquipmentRecord) error {
	return s.SaveFields(ctx, record, repository.AllEquipmentFields()...)
}

// SaveFields 保存穿戴装备 dirty 字段。
func (s *mysqlEquipmentStore) SaveFields(ctx context.Context, record repository.EquipmentRecord, fields ...repository.EquipmentField) error {
	characterID, err := requireRecordKey(repository.EquipmentKey, record, "equipment")
	if err != nil {
		return err
	}
	fields = repository.EquipmentFields.Normalize(fields)
	if len(fields) == 0 {
		return nil
	}
	table, err := s.router.writeTable(mysqlEquipmentTable, characterID)
	if err != nil {
		return err
	}
	entriesTable, err := s.router.writeTable(mysqlEquipmentEntriesTable, characterID)
	if err != nil {
		return err
	}
	extraTable, err := s.router.writeTable(mysqlEquipmentExtraTable, characterID)
	if err != nil {
		return err
	}
	columns := []string{"character_id", "updated_at"}
	args := []any{characterID, timeOrNow(record.UpdatedAt, s.router.now)}
	updates := []string{updateValue("updated_at")}
	saveEntries := false
	for _, field := range fields {
		if field == repository.EquipmentFieldEntries {
			saveEntries = true
		}
	}
	return withMySQLWriteExecutor(ctx, s.router.db, func(database SQLDB) error {
		if _, execErr := database.ExecContext(ctx, buildUpsert(table, columns, updates), args...); execErr != nil {
			return execErr
		}
		if !saveEntries {
			return nil
		}
		return replaceEquipmentEntries(ctx, database, entriesTable, extraTable, characterID, record.Entries)
	})
}

func loadEquipmentEntries(
	ctx context.Context,
	router mysqlRouter,
	entriesTable, extraTable, characterID string,
) (map[string]repository.EquipmentEntry, error) {
	query := router.selectQuery("SELECT entry_key, slot_index, item_id, bind_flag, expire_at, raw_entry FROM " + entriesTable + " WHERE character_id = ? ORDER BY entry_key")
	rows, err := router.db.QueryContext(ctx, query, characterID)
	if err != nil {
		return nil, err
	}
	var entries map[string]repository.EquipmentEntry
	for rows.Next() {
		var key string
		var entry repository.EquipmentEntry
		var expireAt sql.NullTime
		var rawEntry []byte
		if err := rows.Scan(&key, &entry.SlotIndex, &entry.ItemID, &entry.Bind, &expireAt, &rawEntry); err != nil {
			rows.Close()
			return nil, err
		}
		entry.ExpireAt = scanTime(expireAt)
		entry.RawEntry = append([]byte(nil), rawEntry...)
		if entries == nil {
			entries = make(map[string]repository.EquipmentEntry)
		}
		entries[key] = entry
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	extras, err := router.db.QueryContext(ctx, router.selectQuery("SELECT entry_key, extra_key, extra_value FROM "+extraTable+" WHERE character_id = ? ORDER BY entry_key, extra_key"), characterID)
	if err != nil {
		return nil, err
	}
	defer extras.Close()
	for extras.Next() {
		var entryKey, extraKey, extraValue string
		if err := extras.Scan(&entryKey, &extraKey, &extraValue); err != nil {
			return nil, err
		}
		entry, ok := entries[entryKey]
		if !ok {
			continue
		}
		if entry.Extra == nil {
			entry.Extra = make(map[string]string)
		}
		entry.Extra[extraKey] = extraValue
		entries[entryKey] = entry
	}
	if err := extras.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

func replaceEquipmentEntries(
	ctx context.Context,
	database SQLDB,
	entriesTable, extraTable, characterID string,
	entries map[string]repository.EquipmentEntry,
) error {
	if _, err := database.ExecContext(ctx, "DELETE FROM "+extraTable+" WHERE character_id = ?", characterID); err != nil {
		return err
	}
	if _, err := database.ExecContext(ctx, "DELETE FROM "+entriesTable+" WHERE character_id = ?", characterID); err != nil {
		return err
	}
	entryQuery := "INSERT INTO " + entriesTable + " (character_id, entry_key, slot_index, item_id, bind_flag, expire_at, raw_entry) VALUES (?, ?, ?, ?, ?, ?, ?)"
	extraQuery := "INSERT INTO " + extraTable + " (character_id, entry_key, extra_key, extra_value) VALUES (?, ?, ?, ?)"
	for _, entryKey := range sortedStringKeys(entries) {
		entry := entries[entryKey]
		if _, err := database.ExecContext(
			ctx, entryQuery, characterID, entryKey, entry.SlotIndex, entry.ItemID, entry.Bind, sqlTime(entry.ExpireAt), entry.RawEntry,
		); err != nil {
			return err
		}
		for _, extraKey := range sortedStringKeys(entry.Extra) {
			if _, err := database.ExecContext(ctx, extraQuery, characterID, entryKey, extraKey, entry.Extra[extraKey]); err != nil {
				return err
			}
		}
	}
	return nil
}
