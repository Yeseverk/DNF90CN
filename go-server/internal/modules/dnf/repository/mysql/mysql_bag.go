// 本文件实现 DNF 背包仓储的 MySQL 字段化读写。
package mysql

import (
	"context"
	"database/sql"
	"longheng.io/server/internal/modules/dnf/repository"
)

const mysqlBagTable = "inventories"

type mysqlBagStore struct {
	mysqlStoreBase
}

// Load 按角色 ID 从 MySQL 读取 DNF 背包记录。
func (s *mysqlBagStore) Load(ctx context.Context, characterID string) (repository.InventoryRecord, bool, error) {
	table, err := s.router.readTable(mysqlBagTable, characterID)
	if err != nil {
		return repository.InventoryRecord{}, false, err
	}
	itemsTable, err := s.router.readTable(mysqlInventoryItemsTable, characterID)
	if err != nil {
		return repository.InventoryRecord{}, false, err
	}
	extraTable, err := s.router.readTable(mysqlInventoryExtraTable, characterID)
	if err != nil {
		return repository.InventoryRecord{}, false, err
	}
	query := s.router.selectQuery("SELECT character_id, updated_at FROM " + table + " WHERE character_id = ?")
	var record repository.InventoryRecord
	var updatedAt sql.NullTime
	err = s.router.db.QueryRowContext(ctx, query, characterID).Scan(
		&record.CharacterID,
		&updatedAt,
	)
	if err != nil {
		ok, scanErr := scanErr(err)
		return repository.InventoryRecord{}, ok, scanErr
	}
	record.Slots, err = loadItemStackCollection(
		ctx, s.router.db, itemsTable, extraTable, "character_id", characterID, "slots", true,
	)
	if err != nil {
		return repository.InventoryRecord{}, false, err
	}
	record.Warehouse, err = loadItemStackCollection(
		ctx, s.router.db, itemsTable, extraTable, "character_id", characterID, "warehouse", true,
	)
	if err != nil {
		return repository.InventoryRecord{}, false, err
	}
	record.UpdatedAt = scanTime(updatedAt)
	return repository.CloneInventory(record), true, nil
}

// Save 保存完整 DNF 背包记录到 MySQL。
func (s *mysqlBagStore) Save(ctx context.Context, record repository.InventoryRecord) error {
	return s.SaveFields(ctx, record, repository.AllInventoryFields()...)
}

// SaveFields 保存 DNF 背包指定字段到 MySQL。
// 它只更新 slots 或 warehouse dirty 字段，便于后续异步落库降低写放大。
func (s *mysqlBagStore) SaveFields(ctx context.Context, record repository.InventoryRecord, fields ...repository.InventoryField) error {
	characterID, err := requireRecordKey(repository.InventoryKey, record, "inventory")
	if err != nil {
		return err
	}
	fields = repository.InventoryFields.Normalize(fields)
	if len(fields) == 0 {
		return nil
	}
	table, err := s.router.writeTable(mysqlBagTable, characterID)
	if err != nil {
		return err
	}
	itemsTable, err := s.router.writeTable(mysqlInventoryItemsTable, characterID)
	if err != nil {
		return err
	}
	extraTable, err := s.router.writeTable(mysqlInventoryExtraTable, characterID)
	if err != nil {
		return err
	}
	columns := []string{"character_id", "updated_at"}
	args := []any{characterID, timeOrNow(record.UpdatedAt, s.router.now)}
	updates := []string{updateValue("updated_at")}
	return withMySQLWriteExecutor(ctx, s.router.db, func(database SQLDB) error {
		if _, execErr := database.ExecContext(ctx, buildUpsert(table, columns, updates), args...); execErr != nil {
			return execErr
		}
		for _, field := range fields {
			switch field {
			case repository.InventoryFieldSlots:
				if err := replaceItemStackCollection(
					ctx, database, itemsTable, extraTable, "character_id", characterID, "slots", true, record.Slots,
				); err != nil {
					return err
				}
			case repository.InventoryFieldWarehouse:
				if err := replaceItemStackCollection(
					ctx, database, itemsTable, extraTable, "character_id", characterID, "warehouse", true, record.Warehouse,
				); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
