// 本文件实现 DNF 账号仓储的 MySQL 读写。
package mysql

import (
	"context"
	"database/sql"
	"errors"
	"longheng.io/server/internal/modules/dnf/repository"
	"strings"
	"time"

	mysql "github.com/go-sql-driver/mysql"
	"longheng.io/server/internal/platform/db"
)

const (
	mysqlAccountTable                  = "accounts"
	mysqlAccountHonorColumn            = "honor_exp"
	mysqlAccountHonorDDL               = "BIGINT UNSIGNED NOT NULL DEFAULT 0"
	mysqlAccountRepresentNameColumn    = "represent_account_name"
	mysqlAccountRepresentNameDDL       = "VARCHAR(64) NULL"
	mysqlAccountRepresentNameUniqueKey = "uk_dnf_accounts_represent_account_name"
)

type mysqlAccountStore struct {
	mysqlStoreBase
}

// Load 按账号 ID 从 MySQL 读取 DNF 账号记录。
func (s *mysqlAccountStore) Load(ctx context.Context, accountID string) (repository.AccountRecord, bool, error) {
	table, err := s.router.readTable(mysqlAccountTable, accountID)
	if err != nil {
		return repository.AccountRecord{}, false, err
	}
	metadataTable, err := s.router.readTable(mysqlAccountMetadataTable, accountID)
	if err != nil {
		return repository.AccountRecord{}, false, err
	}
	query := s.router.selectQuery("SELECT account_id, state, honor_exp, represent_account_name, created_at, updated_at FROM " + table + " WHERE account_id = ?")
	var record repository.AccountRecord
	var representName sql.NullString
	var createdAt, updatedAt sql.NullTime
	err = s.router.db.QueryRowContext(ctx, query, accountID).Scan(
		&record.AccountID,
		&record.State,
		&record.HonorExp,
		&representName,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		ok, scanErr := scanErr(err)
		return repository.AccountRecord{}, ok, scanErr
	}
	if representName.Valid {
		record.RepresentAccountName = strings.TrimSpace(representName.String)
	}
	record.Metadata, err = loadStringMap(
		ctx,
		s.router.db,
		s.router.selectQuery("SELECT entry_key, entry_value FROM "+metadataTable+" WHERE account_id = ? ORDER BY entry_key"),
		accountID,
	)
	if err != nil {
		return repository.AccountRecord{}, false, err
	}
	record.CreatedAt = scanTime(createdAt)
	record.UpdatedAt = scanTime(updatedAt)
	return repository.CloneAccount(record), true, nil
}

// Save 保存完整 DNF 账号记录到 MySQL。
func (s *mysqlAccountStore) Save(ctx context.Context, record repository.AccountRecord) error {
	accountID, err := requireRecordKey(repository.AccountKey, record, "account")
	if err != nil {
		return err
	}
	table, err := s.router.writeTable(mysqlAccountTable, accountID)
	if err != nil {
		return err
	}
	metadataTable, err := s.router.writeTable(mysqlAccountMetadataTable, accountID)
	if err != nil {
		return err
	}
	updatedAt := timeOrNow(record.UpdatedAt, s.router.now)
	createdAt := record.CreatedAt
	if createdAt.IsZero() {
		createdAt = updatedAt
	}
	representName := strings.TrimSpace(record.RepresentAccountName)
	var representNameArg any
	if representName != "" {
		representNameArg = representName
	}
	columns := []string{"account_id", "state", mysqlAccountHonorColumn, mysqlAccountRepresentNameColumn, "created_at", "updated_at"}
	args := []any{accountID, record.State, record.HonorExp, representNameArg, sqlTime(createdAt), updatedAt}
	updates := []string{
		updateValue("state"),
		updateValue(mysqlAccountHonorColumn),
		updateValue(mysqlAccountRepresentNameColumn),
		keepCreatedAt("created_at"),
		updateValue("updated_at"),
	}
	err = withMySQLWriteExecutor(ctx, s.router.db, func(database SQLDB) error {
		if _, execErr := database.ExecContext(ctx, buildUpsert(table, columns, updates), args...); execErr != nil {
			return execErr
		}
		return replaceStringMap(ctx, database, metadataTable, "account_id", accountID, record.Metadata)
	})
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		return repository.ErrRepresentAccountNameExists
	}
	return err
}

func (s *mysqlAccountStore) SaveMetadataEntry(
	ctx context.Context,
	accountID string,
	key string,
	value string,
	updatedAt time.Time,
) error {
	accountID = strings.TrimSpace(accountID)
	key = strings.TrimSpace(key)
	if accountID == "" {
		return db.ErrRecordKeyRequired
	}
	if key == "" {
		return repository.ErrAccountMetadataKeyRequired
	}
	table, err := s.router.writeTable(mysqlAccountTable, accountID)
	if err != nil {
		return err
	}
	metadataTable, err := s.router.writeTable(mysqlAccountMetadataTable, accountID)
	if err != nil {
		return err
	}
	updatedAt = timeOrNow(updatedAt, s.router.now)
	return withMySQLWriteExecutor(ctx, s.router.db, func(database SQLDB) error {
		updateQuery := "UPDATE " + table + " SET updated_at = ? WHERE account_id = ?"
		if _, err := database.ExecContext(ctx, updateQuery, updatedAt, accountID); err != nil {
			return err
		}
		columns := []string{"account_id", "entry_key", "entry_value"}
		updates := []string{updateValue("entry_value")}
		_, err := database.ExecContext(
			ctx,
			buildUpsert(metadataTable, columns, updates),
			accountID,
			key,
			value,
		)
		return err
	})
}

func (s *mysqlAccountStore) FindAccountIDByRepresentName(ctx context.Context, name string) (string, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return "", false, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", false, nil
	}
	for _, database := range s.router.readDBs {
		table := mysqlTable(database, s.router.tablePrefix, mysqlAccountTable)
		query := "SELECT account_id FROM " + table + " WHERE represent_account_name = ? LIMIT 1"
		var accountID string
		err := s.router.db.QueryRowContext(ctx, query, name).Scan(&accountID)
		if err == nil {
			return accountID, true, nil
		}
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		return "", false, err
	}
	return "", false, nil
}
