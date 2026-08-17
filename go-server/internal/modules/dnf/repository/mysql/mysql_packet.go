// 本文件实现 DNF packet 模板仓储的 MySQL 读写。
package mysql

import (
	"context"
	"database/sql"
	"longheng.io/server/internal/modules/dnf/repository"
)

const mysqlPacketTable = "packet_templates"

type mysqlPacketStore struct {
	mysqlStoreBase
}

// Load 按模板 ID 从 MySQL 读取 DNF packet 模板。
func (s *mysqlPacketStore) Load(ctx context.Context, templateID string) (repository.PacketTemplateRecord, bool, error) {
	table, err := s.router.readTable(mysqlPacketTable, templateID)
	if err != nil {
		return repository.PacketTemplateRecord{}, false, err
	}
	metadataTable, err := s.router.readTable(mysqlPacketMetadataTable, templateID)
	if err != nil {
		return repository.PacketTemplateRecord{}, false, err
	}
	query := s.router.selectQuery("SELECT template_id, name, body, updated_at FROM " + table + " WHERE template_id = ?")
	var record repository.PacketTemplateRecord
	var body []byte
	var updatedAt sql.NullTime
	err = s.router.db.QueryRowContext(ctx, query, templateID).Scan(
		&record.TemplateID,
		&record.Name,
		&body,
		&updatedAt,
	)
	if err != nil {
		ok, scanErr := scanErr(err)
		return repository.PacketTemplateRecord{}, ok, scanErr
	}
	record.Body = append([]byte(nil), body...)
	record.Metadata, err = loadStringMap(
		ctx,
		s.router.db,
		s.router.selectQuery("SELECT entry_key, entry_value FROM "+metadataTable+" WHERE template_id = ? ORDER BY entry_key"),
		templateID,
	)
	if err != nil {
		return repository.PacketTemplateRecord{}, false, err
	}
	record.UpdatedAt = scanTime(updatedAt)
	return repository.ClonePacketTemplate(record), true, nil
}

// Save 保存完整 DNF packet 模板到 MySQL。
func (s *mysqlPacketStore) Save(ctx context.Context, record repository.PacketTemplateRecord) error {
	templateID, err := requireRecordKey(repository.PacketTemplateKey, record, "packet template")
	if err != nil {
		return err
	}
	table, err := s.router.writeTable(mysqlPacketTable, templateID)
	if err != nil {
		return err
	}
	metadataTable, err := s.router.writeTable(mysqlPacketMetadataTable, templateID)
	if err != nil {
		return err
	}
	columns := []string{"template_id", "name", "body", "updated_at"}
	args := []any{
		templateID,
		record.Name,
		append([]byte(nil), record.Body...),
		timeOrNow(record.UpdatedAt, s.router.now),
	}
	updates := []string{
		updateValue("name"),
		updateValue("body"),
		updateValue("updated_at"),
	}
	return withMySQLWriteExecutor(ctx, s.router.db, func(database SQLDB) error {
		if _, execErr := database.ExecContext(ctx, buildUpsert(table, columns, updates), args...); execErr != nil {
			return execErr
		}
		return replaceStringMap(ctx, database, metadataTable, "template_id", templateID, record.Metadata)
	})
}
