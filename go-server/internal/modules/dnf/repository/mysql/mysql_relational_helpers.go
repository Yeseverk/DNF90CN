package mysql

import (
	"context"
	"database/sql"
	"errors"
	"longheng.io/server/internal/modules/dnf/repository"
	"sort"
)

func withMySQLWriteExecutor(ctx context.Context, database SQLDB, apply func(SQLDB) error) error {
	if apply == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	beginner, ok := database.(sqlTxBeginner)
	if !ok {
		return apply(database)
	}
	tx, err := beginner.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	txDB := sqlTransactionDB{tx: tx}
	if err := apply(txDB); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	return tx.Commit()
}

func sortedStringKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func loadStringMap(
	ctx context.Context,
	database SQLDB,
	query string,
	ownerID string,
) (map[string]string, error) {
	rows, err := database.QueryContext(ctx, query, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values map[string]string
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		if values == nil {
			values = make(map[string]string)
		}
		values[key] = value
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

func replaceStringMap(
	ctx context.Context,
	database SQLDB,
	table, ownerColumn, ownerID string,
	values map[string]string,
) error {
	if _, err := database.ExecContext(ctx, "DELETE FROM "+table+" WHERE "+quoteSQLIdentifier(ownerColumn)+" = ?", ownerID); err != nil {
		return err
	}
	query := "INSERT INTO " + table + " (" + quoteSQLIdentifier(ownerColumn) + ", entry_key, entry_value) VALUES (?, ?, ?)"
	for _, key := range sortedStringKeys(values) {
		if _, err := database.ExecContext(ctx, query, ownerID, key, values[key]); err != nil {
			return err
		}
	}
	return nil
}

func loadItemStackCollection(
	ctx context.Context,
	database SQLDB,
	itemTable, extraTable, ownerColumn, ownerID, collection string,
	withCollection bool,
) (map[string]repository.ItemStack, error) {
	where := quoteSQLIdentifier(ownerColumn) + " = ?"
	args := []any{ownerID}
	if withCollection {
		where += " AND collection_name = ?"
		args = append(args, collection)
	}
	query := "SELECT entry_key, item_id, item_count, bind_flag, expire_at, raw_entry FROM " + itemTable + " WHERE " + where + " ORDER BY entry_key"
	rows, err := database.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	var values map[string]repository.ItemStack
	for rows.Next() {
		var key string
		var stack repository.ItemStack
		var expireAt sql.NullTime
		var rawEntry []byte
		if err := rows.Scan(&key, &stack.ItemID, &stack.Count, &stack.Bind, &expireAt, &rawEntry); err != nil {
			rows.Close()
			return nil, err
		}
		stack.ExpireAt = scanTime(expireAt)
		stack.RawEntry = append([]byte(nil), rawEntry...)
		if values == nil {
			values = make(map[string]repository.ItemStack)
		}
		values[key] = stack
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	extraWhere := quoteSQLIdentifier(ownerColumn) + " = ?"
	extraArgs := []any{ownerID}
	if withCollection {
		extraWhere += " AND collection_name = ?"
		extraArgs = append(extraArgs, collection)
	}
	extraQuery := "SELECT entry_key, extra_key, extra_value FROM " + extraTable + " WHERE " + extraWhere + " ORDER BY entry_key, extra_key"
	extras, err := database.QueryContext(ctx, extraQuery, extraArgs...)
	if err != nil {
		return nil, err
	}
	defer extras.Close()
	for extras.Next() {
		var entryKey, extraKey, extraValue string
		if err := extras.Scan(&entryKey, &extraKey, &extraValue); err != nil {
			return nil, err
		}
		stack, ok := values[entryKey]
		if !ok {
			continue
		}
		if stack.Extra == nil {
			stack.Extra = make(map[string]string)
		}
		stack.Extra[extraKey] = extraValue
		values[entryKey] = stack
	}
	if err := extras.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

func replaceItemStackCollection(
	ctx context.Context,
	database SQLDB,
	itemTable, extraTable, ownerColumn, ownerID, collection string,
	withCollection bool,
	values map[string]repository.ItemStack,
) error {
	where := quoteSQLIdentifier(ownerColumn) + " = ?"
	args := []any{ownerID}
	if withCollection {
		where += " AND collection_name = ?"
		args = append(args, collection)
	}
	if _, err := database.ExecContext(ctx, "DELETE FROM "+extraTable+" WHERE "+where, args...); err != nil {
		return err
	}
	if _, err := database.ExecContext(ctx, "DELETE FROM "+itemTable+" WHERE "+where, args...); err != nil {
		return err
	}

	itemColumns := quoteSQLIdentifier(ownerColumn) + ", entry_key, item_id, item_count, bind_flag, expire_at, raw_entry"
	itemPlaceholders := "?, ?, ?, ?, ?, ?, ?"
	if withCollection {
		itemColumns = quoteSQLIdentifier(ownerColumn) + ", collection_name, entry_key, item_id, item_count, bind_flag, expire_at, raw_entry"
		itemPlaceholders = "?, ?, ?, ?, ?, ?, ?, ?"
	}
	itemQuery := "INSERT INTO " + itemTable + " (" + itemColumns + ") VALUES (" + itemPlaceholders + ")"
	extraColumns := quoteSQLIdentifier(ownerColumn) + ", entry_key, extra_key, extra_value"
	extraPlaceholders := "?, ?, ?, ?"
	if withCollection {
		extraColumns = quoteSQLIdentifier(ownerColumn) + ", collection_name, entry_key, extra_key, extra_value"
		extraPlaceholders = "?, ?, ?, ?, ?"
	}
	extraQuery := "INSERT INTO " + extraTable + " (" + extraColumns + ") VALUES (" + extraPlaceholders + ")"

	for _, entryKey := range sortedStringKeys(values) {
		stack := values[entryKey]
		itemArgs := []any{ownerID, entryKey, stack.ItemID, stack.Count, stack.Bind, sqlTime(stack.ExpireAt), stack.RawEntry}
		if withCollection {
			itemArgs = []any{ownerID, collection, entryKey, stack.ItemID, stack.Count, stack.Bind, sqlTime(stack.ExpireAt), stack.RawEntry}
		}
		if _, err := database.ExecContext(ctx, itemQuery, itemArgs...); err != nil {
			return err
		}
		for _, extraKey := range sortedStringKeys(stack.Extra) {
			extraArgs := []any{ownerID, entryKey, extraKey, stack.Extra[extraKey]}
			if withCollection {
				extraArgs = []any{ownerID, collection, entryKey, extraKey, stack.Extra[extraKey]}
			}
			if _, err := database.ExecContext(ctx, extraQuery, extraArgs...); err != nil {
				return err
			}
		}
	}
	return nil
}
