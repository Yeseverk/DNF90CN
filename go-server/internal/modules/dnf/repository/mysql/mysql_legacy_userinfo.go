// 本文件实现 C# USERINFO legacy 表的 MySQL 只读查询。
package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"longheng.io/server/internal/modules/dnf/repository"
	"strings"

	"longheng.io/server/internal/platform/db"
)

type mysqlLegacyUserInfoStore struct {
	mysqlStoreBase
}

// SelectRows 从指定 C# USERINFO legacy 表读取一组行。
func (s *mysqlLegacyUserInfoStore) SelectRows(ctx context.Context, characterID string, tableSuffix string, columns []string, orderBy []string) ([]repository.LegacyUserInfoRow, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	characterID = strings.TrimSpace(characterID)
	if characterID == "" {
		return nil, db.ErrRecordKeyRequired
	}
	if err := validateLegacyUserInfoQuery(tableSuffix, columns, orderBy); err != nil {
		return nil, err
	}
	table, err := s.router.readTable(tableSuffix, characterID)
	if err != nil {
		return nil, err
	}
	query := "SELECT " + quotedColumns(columns) + " FROM " + table + " WHERE `character_id` = ?"
	if len(orderBy) > 0 {
		query += " ORDER BY " + quotedColumns(orderBy)
	}
	rows, err := s.router.db.QueryContext(ctx, query, characterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]repository.LegacyUserInfoRow, 0)
	for rows.Next() {
		values := make([]sql.NullString, len(columns))
		dest := make([]any, len(columns))
		for idx := range values {
			dest[idx] = &values[idx]
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		row := make(repository.LegacyUserInfoRow, len(columns))
		for idx, column := range columns {
			if values[idx].Valid {
				row[column] = values[idx].String
			} else {
				row[column] = ""
			}
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// SelectOne 从指定 C# USERINFO legacy 表读取一行。
func (s *mysqlLegacyUserInfoStore) SelectOne(ctx context.Context, characterID string, tableSuffix string, columns []string) (repository.LegacyUserInfoRow, bool, error) {
	rows, err := s.SelectRows(ctx, characterID, tableSuffix, columns, nil)
	if err != nil {
		return nil, false, err
	}
	if len(rows) == 0 {
		return nil, false, nil
	}
	return rows[0], true, nil
}

func validateLegacyUserInfoQuery(tableSuffix string, columns []string, orderBy []string) error {
	tableSuffix = strings.TrimSpace(tableSuffix)
	if !repository.IsSQLIdentifier(tableSuffix) || !isLegacyUserInfoReadableTable(tableSuffix) {
		return fmt.Errorf("%w: legacy userinfo table %q is invalid", ErrMySQLConfigInvalid, tableSuffix)
	}
	if len(columns) == 0 {
		return fmt.Errorf("%w: legacy userinfo columns are required", ErrMySQLConfigInvalid)
	}
	for _, column := range columns {
		if !repository.IsSQLIdentifier(column) {
			return fmt.Errorf("%w: legacy userinfo column %q is invalid", ErrMySQLConfigInvalid, column)
		}
	}
	for _, column := range orderBy {
		if !repository.IsSQLIdentifier(column) {
			return fmt.Errorf("%w: legacy userinfo order column %q is invalid", ErrMySQLConfigInvalid, column)
		}
	}
	return nil
}

func isLegacyUserInfoReadableTable(tableSuffix string) bool {
	// USERINFO 0x0002 的 subtype0/subtype1 是 C# schema 里的独立表；这里只额外放行这两张表，避免 legacy 查询入口变成任意表扫描。
	return strings.HasPrefix(tableSuffix, "legacy_character_userinfo") ||
		tableSuffix == "legacy_character_subtype0_fields" ||
		tableSuffix == "legacy_character_subtype1_fields" ||
		tableSuffix == "legacy_character_achievement_chunks" ||
		tableSuffix == "legacy_character_achievement_complete"
}
