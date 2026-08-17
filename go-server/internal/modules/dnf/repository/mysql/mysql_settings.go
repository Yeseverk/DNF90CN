// 本文件实现 DNF 设置仓储的 MySQL 字段化读写。
package mysql

import (
	"context"
	"database/sql"
	"longheng.io/server/internal/modules/dnf/repository"
)

const mysqlSettingsTable = "settings"

type mysqlSettingsStore struct {
	mysqlStoreBase
}

// Load 按 scope 从 MySQL 读取 DNF 设置记录。
func (s *mysqlSettingsStore) Load(ctx context.Context, scope string) (repository.SettingsRecord, bool, error) {
	table, err := s.router.readTable(mysqlSettingsTable, scope)
	if err != nil {
		return repository.SettingsRecord{}, false, err
	}
	valuesTable, err := s.router.readTable(mysqlSettingValuesTable, scope)
	if err != nil {
		return repository.SettingsRecord{}, false, err
	}
	query := s.router.selectQuery("SELECT scope, updated_at FROM " + table + " WHERE scope = ?")
	var record repository.SettingsRecord
	var updatedAt sql.NullTime
	err = s.router.db.QueryRowContext(ctx, query, scope).Scan(
		&record.Scope,
		&updatedAt,
	)
	if err != nil {
		ok, scanErr := scanErr(err)
		return repository.SettingsRecord{}, ok, scanErr
	}
	record.Values, err = loadStringMap(
		ctx,
		s.router.db,
		s.router.selectQuery("SELECT entry_key, entry_value FROM "+valuesTable+" WHERE scope = ? ORDER BY entry_key"),
		scope,
	)
	if err != nil {
		return repository.SettingsRecord{}, false, err
	}
	record.UpdatedAt = scanTime(updatedAt)
	return repository.CloneSettings(record), true, nil
}

// Save 保存完整 DNF 设置记录到 MySQL。
func (s *mysqlSettingsStore) Save(ctx context.Context, record repository.SettingsRecord) error {
	return s.SaveFields(ctx, record, repository.AllSettingsFields()...)
}

// SaveFields 保存 DNF 设置指定字段到 MySQL。
// 当前只有 Values 字段，保留字段化接口是为了和其他玩家仓储统一。
func (s *mysqlSettingsStore) SaveFields(ctx context.Context, record repository.SettingsRecord, fields ...repository.SettingsField) error {
	scope, err := requireRecordKey(repository.SettingsKey, record, "settings")
	if err != nil {
		return err
	}
	fields = repository.SettingsFields.Normalize(fields)
	if len(fields) == 0 {
		return nil
	}
	table, err := s.router.writeTable(mysqlSettingsTable, scope)
	if err != nil {
		return err
	}
	valuesTable, err := s.router.writeTable(mysqlSettingValuesTable, scope)
	if err != nil {
		return err
	}
	columns := []string{"scope", "updated_at"}
	args := []any{scope, timeOrNow(record.UpdatedAt, s.router.now)}
	updates := []string{updateValue("updated_at")}
	return withMySQLWriteExecutor(ctx, s.router.db, func(database SQLDB) error {
		if _, execErr := database.ExecContext(ctx, buildUpsert(table, columns, updates), args...); execErr != nil {
			return execErr
		}
		return replaceStringMap(ctx, database, valuesTable, "scope", scope, record.Values)
	})
}
