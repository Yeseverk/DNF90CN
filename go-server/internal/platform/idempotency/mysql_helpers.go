package idempotency

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	mysql "github.com/go-sql-driver/mysql"
)

func mysqlReqStatusSQL() string {
	return "SELECT " + mysqlQuoteIdentifier("status") + " FROM " + mysqlQuoteIdentifier(mysqlRequestsTable) + " WHERE " + mysqlQuoteIdentifier("guard_key") + " = ? AND " + mysqlQuoteIdentifier("expires_at") + " > ? LIMIT 1 FOR UPDATE"
}

func mysqlReqMetaSQL() string {
	return "SELECT " + mysqlQuoteIdentifier("request_key") + ", " + mysqlQuoteIdentifier("expires_at") + " FROM " + mysqlQuoteIdentifier(mysqlRequestsTable) + " WHERE " + mysqlQuoteIdentifier("guard_key") + " = ? AND " + mysqlQuoteIdentifier("expires_at") + " > ? LIMIT 1"
}

func mysqlStatusDecision(status Status, key string, sequence uint64, fingerprint, ownerToken string, expiresAt time.Time) Decision {
	switch status {
	case statusPending:
		return Decision{Status: StatusInFlight, Key: key, Sequence: sequence, fingerprint: fingerprint, ownerToken: ownerToken, expiresAt: expiresAt}
	case StatusReplay:
		return Decision{Status: StatusReplay, Key: key, Sequence: sequence, fingerprint: fingerprint, ownerToken: ownerToken, expiresAt: expiresAt}
	default:
		return Decision{Status: StatusDuplicate, Key: key, Sequence: sequence, fingerprint: fingerprint, ownerToken: ownerToken, expiresAt: expiresAt}
	}
}

func mysqlHighestSeq(ctx context.Context, tx *sql.Tx, seqKey string, now time.Time) (uint64, error) {
	if _, err := tx.ExecContext(ctx, //nolint:gosec // G202：SQL 只拼接框架固定表名/列名，过期时间通过参数绑定。
		"DELETE FROM "+mysqlQuoteIdentifier(mysqlResvTable)+" WHERE "+mysqlQuoteIdentifier("expires_at")+" <= ?", //nolint:gosec // G202：SQL 只拼接框架固定表名/列名，过期时间通过参数绑定。
		now,
	); err != nil {
		return 0, err
	}
	var highest uint64
	err := tx.QueryRowContext(ctx,
		"SELECT COALESCE(MAX("+mysqlQuoteIdentifier("sequence_num")+"), 0) FROM "+mysqlQuoteIdentifier(mysqlResvTable)+" WHERE "+mysqlQuoteIdentifier("sequence_key")+" = ? AND "+mysqlQuoteIdentifier("expires_at")+" > ?",
		seqKey,
		now,
	).Scan(&highest)
	return highest, err
}

func mysqlHighestSeqDB(ctx context.Context, db *sql.DB, seqKey string, now time.Time) (uint64, error) {
	if db == nil {
		return 0, fmt.Errorf("mysql idempotency store is nil")
	}
	var highest uint64
	err := db.QueryRowContext(ctx,
		"SELECT COALESCE(MAX("+mysqlQuoteIdentifier("sequence_num")+"), 0) FROM "+mysqlQuoteIdentifier(mysqlResvTable)+" WHERE "+mysqlQuoteIdentifier("sequence_key")+" = ? AND "+mysqlQuoteIdentifier("expires_at")+" > ?",
		seqKey,
		now,
	).Scan(&highest)
	return highest, err
}

func insertMySQLRequest(ctx context.Context, tx *sql.Tx, guardKey string, item Request, requestKey, fingerprint, ownerToken string, status Status, now, expiresAt time.Time) error {
	_, err := tx.ExecContext(ctx, //nolint:gosec // G202：SQL 只拼接框架固定表名/列名，请求字段通过参数绑定。
		"INSERT INTO "+mysqlQuoteIdentifier(mysqlRequestsTable)+" ("+ //nolint:gosec // G202：SQL 只拼接框架固定表名/列名，请求字段通过参数绑定。
			strings.Join([]string{
				mysqlQuoteIdentifier("guard_key"),
				mysqlQuoteIdentifier("scope"),
				mysqlQuoteIdentifier("subject"),
				mysqlQuoteIdentifier("session_id"),
				mysqlQuoteIdentifier("request_key"),
				mysqlQuoteIdentifier("sequence_num"),
				mysqlQuoteIdentifier("status"),
				mysqlQuoteIdentifier("created_at"),
				mysqlQuoteIdentifier("expires_at"),
			}, ", ")+") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		guardKey,
		limitStorageText(item.Scope, 128),
		limitStorageText(item.Subject, 128),
		limitStorageText(item.Session, 128),
		mysqlMetadataStorage(requestKey, fingerprint, ownerToken),
		item.Sequence,
		string(status),
		now,
		expiresAt,
	)
	return err
}

func upsertMySQLStatus(ctx context.Context, tx *sql.Tx, guardKey string, item Request, requestKey, fingerprint, ownerToken string, status Status, now, expiresAt time.Time) error {
	result, err := tx.ExecContext(ctx, //nolint:gosec // G202：SQL 只拼接框架固定表名/列名，状态字段通过参数绑定。
		"UPDATE "+mysqlQuoteIdentifier(mysqlRequestsTable)+" SET "+mysqlQuoteIdentifier("status")+" = ?, "+mysqlQuoteIdentifier("expires_at")+" = ? WHERE "+mysqlQuoteIdentifier("guard_key")+" = ?", //nolint:gosec // G202：SQL 只拼接框架固定表名/列名，状态字段通过参数绑定。
		string(status),
		expiresAt,
		guardKey,
	)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected > 0 {
		return nil
	}
	insertErr := insertMySQLRequest(ctx, tx, guardKey, item, requestKey, fingerprint, ownerToken, status, now, expiresAt)
	if insertErr != nil && !isMySQLDuplicate(insertErr) {
		return insertErr
	}
	if isMySQLDuplicate(insertErr) {
		_, err = tx.ExecContext(ctx, //nolint:gosec // G202：SQL 只拼接框架固定表名/列名，状态字段通过参数绑定。
			"UPDATE "+mysqlQuoteIdentifier(mysqlRequestsTable)+" SET "+mysqlQuoteIdentifier("status")+" = ?, "+mysqlQuoteIdentifier("expires_at")+" = ? WHERE "+mysqlQuoteIdentifier("guard_key")+" = ?", //nolint:gosec // G202：SQL 只拼接框架固定表名/列名，状态字段通过参数绑定。
			string(status),
			expiresAt,
			guardKey,
		)
	}
	return err
}

func mysqlMetadataStorage(requestKey, fingerprint, ownerToken string) string {
	fingerprint = strings.TrimSpace(fingerprint)
	ownerToken = strings.TrimSpace(ownerToken)
	if fingerprint == "" && ownerToken == "" {
		// 直接调用旧 Store 的路径保留 request_key 原义；新 Guard 总会携带 owner token。
		return limitStorageText(requestKey, 512)
	}
	hash := ""
	if fingerprint != "" {
		sum := sha256.Sum256([]byte(fingerprint))
		hash = hex.EncodeToString(sum[:])
	}
	return mysqlMetadataTag + hash + ":" + ownerToken
}

func mysqlMetaFromDB(value, requestKey string) mysqlRequestMeta {
	if value == limitStorageText(requestKey, 512) {
		return mysqlRequestMeta{legacy: true}
	}
	if strings.HasPrefix(value, mysqlMetadataTag) {
		hash, ownerToken, ok := strings.Cut(strings.TrimPrefix(value, mysqlMetadataTag), ":")
		if ok && (hash == "" || len(hash) == sha256.Size*2) {
			return mysqlRequestMeta{fingerprintHash: hash, ownerToken: ownerToken}
		}
		return mysqlRequestMeta{legacy: true}
	}
	if strings.HasPrefix(value, mysqlFingerprintTag) {
		return mysqlRequestMeta{fingerprint: strings.TrimPrefix(value, mysqlFingerprintTag)}
	}
	return mysqlRequestMeta{legacy: true}
}

func (m mysqlRequestMeta) conflicts(incoming string) bool {
	incoming = strings.TrimSpace(incoming)
	if incoming == "" {
		return false
	}
	if m.fingerprint != "" {
		return m.fingerprint != incoming
	}
	if m.fingerprintHash != "" {
		sum := sha256.Sum256([]byte(incoming))
		return m.fingerprintHash != hex.EncodeToString(sum[:])
	}
	return false
}

func (m mysqlRequestMeta) decisionFingerprint(incoming string) string {
	if m.legacy {
		return ""
	}
	if m.fingerprint != "" {
		return m.fingerprint
	}
	if m.fingerprintHash != "" && !m.conflicts(incoming) {
		return strings.TrimSpace(incoming)
	}
	return ""
}

func upsertMySQLResult(ctx context.Context, tx *sql.Tx, guardKey string, payload []byte, now, expiresAt time.Time) error {
	_, err := tx.ExecContext(ctx, //nolint:gosec // G202：SQL 只拼接框架固定表名/列名，结果内容通过参数绑定。
		"INSERT INTO "+mysqlQuoteIdentifier(mysqlResultsTable)+" ("+ //nolint:gosec // G202：SQL 只拼接框架固定表名/列名，结果内容通过参数绑定。
			strings.Join([]string{
				mysqlQuoteIdentifier("guard_key"),
				mysqlQuoteIdentifier("result_data"),
				mysqlQuoteIdentifier("created_at"),
				mysqlQuoteIdentifier("expires_at"),
			}, ", ")+") VALUES (?, ?, ?, ?) ON DUPLICATE KEY UPDATE "+
			mysqlQuoteIdentifier("result_data")+" = VALUES("+mysqlQuoteIdentifier("result_data")+"), "+
			mysqlQuoteIdentifier("expires_at")+" = VALUES("+mysqlQuoteIdentifier("expires_at")+")",
		guardKey,
		payload,
		now,
		expiresAt,
	)
	return err
}

func insertMySQLReserve(ctx context.Context, tx *sql.Tx, seqKey, guardKey string, sequence uint64, expiresAt time.Time) error {
	_, err := tx.ExecContext(ctx, //nolint:gosec // G202：SQL 只拼接框架固定表名/列名，reservation 字段通过参数绑定。
		"INSERT INTO "+mysqlQuoteIdentifier(mysqlResvTable)+" ("+ //nolint:gosec // G202：SQL 只拼接框架固定表名/列名，reservation 字段通过参数绑定。
			strings.Join([]string{
				mysqlQuoteIdentifier("sequence_key"),
				mysqlQuoteIdentifier("sequence_num"),
				mysqlQuoteIdentifier("guard_key"),
				mysqlQuoteIdentifier("expires_at"),
			}, ", ")+") VALUES (?, ?, ?, ?)",
		seqKey,
		sequence,
		guardKey,
		expiresAt,
	)
	return err
}

func mysqlReqTableDDL() string {
	return "CREATE TABLE IF NOT EXISTS " + mysqlQuoteIdentifier(mysqlRequestsTable) + " (\n" +
		"  " + mysqlQuoteIdentifier("guard_key") + " CHAR(64) NOT NULL,\n" +
		"  " + mysqlQuoteIdentifier("scope") + " VARCHAR(128) NOT NULL,\n" +
		"  " + mysqlQuoteIdentifier("subject") + " VARCHAR(128) NOT NULL,\n" +
		"  " + mysqlQuoteIdentifier("session_id") + " VARCHAR(128) NOT NULL DEFAULT '',\n" +
		"  " + mysqlQuoteIdentifier("request_key") + " VARCHAR(512) NOT NULL,\n" +
		"  " + mysqlQuoteIdentifier("sequence_num") + " BIGINT UNSIGNED NOT NULL DEFAULT 0,\n" +
		"  " + mysqlQuoteIdentifier("status") + " VARCHAR(32) NOT NULL,\n" +
		"  " + mysqlQuoteIdentifier("created_at") + " DATETIME(6) NOT NULL,\n" +
		"  " + mysqlQuoteIdentifier("expires_at") + " DATETIME(6) NOT NULL,\n" +
		"  PRIMARY KEY (" + mysqlQuoteIdentifier("guard_key") + "),\n" +
		"  KEY " + mysqlQuoteIdentifier("idx_idempotency_requests_expires_at") + " (" + mysqlQuoteIdentifier("expires_at") + "),\n" +
		"  KEY " + mysqlQuoteIdentifier("idx_idempotency_requests_subject") + " (" + mysqlQuoteIdentifier("scope") + ", " + mysqlQuoteIdentifier("subject") + ")\n" +
		") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci"
}

func mysqlSeqTableDDL() string {
	return "CREATE TABLE IF NOT EXISTS " + mysqlQuoteIdentifier(mysqlSequencesTable) + " (\n" +
		"  " + mysqlQuoteIdentifier("sequence_key") + " CHAR(64) NOT NULL,\n" +
		"  " + mysqlQuoteIdentifier("scope") + " VARCHAR(128) NOT NULL,\n" +
		"  " + mysqlQuoteIdentifier("subject") + " VARCHAR(128) NOT NULL,\n" +
		"  " + mysqlQuoteIdentifier("session_id") + " VARCHAR(128) NOT NULL DEFAULT '',\n" +
		"  " + mysqlQuoteIdentifier("highest_sequence") + " BIGINT UNSIGNED NOT NULL DEFAULT 0,\n" +
		"  " + mysqlQuoteIdentifier("updated_at") + " DATETIME(6) NOT NULL,\n" +
		"  PRIMARY KEY (" + mysqlQuoteIdentifier("sequence_key") + "),\n" +
		"  KEY " + mysqlQuoteIdentifier("idx_idempotency_sequences_subject") + " (" + mysqlQuoteIdentifier("scope") + ", " + mysqlQuoteIdentifier("subject") + "),\n" +
		"  KEY " + mysqlQuoteIdentifier("idx_idempotency_sequences_updated_at") + " (" + mysqlQuoteIdentifier("updated_at") + ")\n" +
		") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci"
}

func mysqlResvTableDDL() string {
	return "CREATE TABLE IF NOT EXISTS " + mysqlQuoteIdentifier(mysqlResvTable) + " (\n" +
		"  " + mysqlQuoteIdentifier("sequence_key") + " CHAR(64) NOT NULL,\n" +
		"  " + mysqlQuoteIdentifier("sequence_num") + " BIGINT UNSIGNED NOT NULL,\n" +
		"  " + mysqlQuoteIdentifier("guard_key") + " CHAR(64) NOT NULL,\n" +
		"  " + mysqlQuoteIdentifier("expires_at") + " DATETIME(6) NOT NULL,\n" +
		"  PRIMARY KEY (" + mysqlQuoteIdentifier("sequence_key") + ", " + mysqlQuoteIdentifier("sequence_num") + "),\n" +
		"  UNIQUE KEY " + mysqlQuoteIdentifier("idx_idempotency_reservations_guard_key") + " (" + mysqlQuoteIdentifier("guard_key") + "),\n" +
		"  KEY " + mysqlQuoteIdentifier("idx_idempotency_reservations_expires_at") + " (" + mysqlQuoteIdentifier("expires_at") + ")\n" +
		") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci"
}

func mysqlResultTableDDL() string {
	return "CREATE TABLE IF NOT EXISTS " + mysqlQuoteIdentifier(mysqlResultsTable) + " (\n" +
		"  " + mysqlQuoteIdentifier("guard_key") + " CHAR(64) NOT NULL,\n" +
		"  " + mysqlQuoteIdentifier("result_data") + " LONGBLOB NOT NULL,\n" +
		"  " + mysqlQuoteIdentifier("created_at") + " DATETIME(6) NOT NULL,\n" +
		"  " + mysqlQuoteIdentifier("expires_at") + " DATETIME(6) NOT NULL,\n" +
		"  PRIMARY KEY (" + mysqlQuoteIdentifier("guard_key") + "),\n" +
		"  KEY " + mysqlQuoteIdentifier("idx_idempotency_results_expires_at") + " (" + mysqlQuoteIdentifier("expires_at") + ")\n" +
		") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci"
}

func mysqlQuoteIdentifier(identifier string) string {
	return "`" + strings.ReplaceAll(identifier, "`", "``") + "`"
}

func mysqlCleanupSQL(table, column string, limit int) string {
	if limit <= 0 {
		limit = 500
	}
	return fmt.Sprintf(
		"DELETE FROM %s WHERE %s <= ? ORDER BY %s LIMIT %d",
		mysqlQuoteIdentifier(table),
		mysqlQuoteIdentifier(column),
		mysqlQuoteIdentifier(column),
		limit,
	)
}

func isMySQLDuplicate(err error) bool {
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}

func limitStorageText(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	last := 0
	for idx := range value {
		if idx > limit {
			return value[:last]
		}
		last = idx
	}
	return value[:last]
}
