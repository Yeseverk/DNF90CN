package notice

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	mysql "github.com/go-sql-driver/mysql"
)

type sqlNoticeQuerier interface {
	QueryContext(context.Context, string, ...any) (SQLRows, error)
}

type sqlNoticeExecutor interface {
	sqlNoticeQuerier
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func (s *SQLStore) lookupPublishKey(ctx context.Context, db sqlNoticeQuerier, lookupKey string) (string, bool, error) {
	return s.lookupKey(ctx, db, "SELECT notice_id FROM "+quoteSQLIdentifier(s.publish)+" WHERE lookup_key = ? LIMIT 1", lookupKey)
}

func (s *SQLStore) lookupAcknowledgeKey(ctx context.Context, db sqlNoticeQuerier, lookupKey string) (string, bool, error) {
	return s.lookupKey(ctx, db, "SELECT delivery_id FROM "+quoteSQLIdentifier(s.ack)+" WHERE lookup_key = ? LIMIT 1", lookupKey)
}

func (s *SQLStore) lookupKey(ctx context.Context, db sqlNoticeQuerier, query string, lookupKey string) (string, bool, error) {
	rows, err := db.QueryContext(ctx, query, lookupKey)
	if err != nil {
		return "", false, err
	}
	values, err := scanSQLStrings(rows)
	if err != nil {
		return "", false, err
	}
	if len(values) == 0 {
		return "", false, nil
	}
	return values[0], true, nil
}

func (s *SQLStore) getNotice(ctx context.Context, db sqlNoticeQuerier, id string) (Notice, bool, error) {
	notice, _, ok, err := s.getNoticePubAt(ctx, db, id)
	return notice, ok, err
}

func (s *SQLStore) getNoticePubAt(ctx context.Context, db sqlNoticeQuerier, id string) (Notice, time.Time, bool, error) {
	rows, err := db.QueryContext(ctx, "SELECT "+noticeColumns()+" FROM "+quoteSQLIdentifier(s.notices)+" WHERE id = ? LIMIT 1", id)
	if err != nil {
		return Notice{}, time.Time{}, false, err
	}
	notices, publishedAt, err := scanSQLPubNotices(rows)
	if err != nil {
		return Notice{}, time.Time{}, false, err
	}
	if len(notices) == 0 {
		return Notice{}, time.Time{}, false, nil
	}
	return cloneNotice(notices[0]), publishedAt[0], true, nil
}

func (s *SQLStore) getDelivery(ctx context.Context, db sqlNoticeQuerier, id string) (Delivery, bool, error) {
	rows, err := db.QueryContext(ctx, "SELECT "+deliveryColumns()+" FROM "+quoteSQLIdentifier(s.deliveries)+" WHERE id = ? LIMIT 1", id)
	if err != nil {
		return Delivery{}, false, err
	}
	deliveries, err := scanSQLDeliveries(rows)
	if err != nil {
		return Delivery{}, false, err
	}
	if len(deliveries) == 0 {
		return Delivery{}, false, nil
	}
	return cloneDelivery(deliveries[0]), true, nil
}

func (s *SQLStore) deliveriesForNotice(ctx context.Context, db sqlNoticeQuerier, noticeID string) ([]Delivery, error) {
	rows, err := db.QueryContext(ctx, "SELECT "+deliveryColumns()+" FROM "+quoteSQLIdentifier(s.deliveries)+" WHERE notice_id = ? ORDER BY account_id ASC, id ASC", noticeID)
	if err != nil {
		return nil, err
	}
	deliveries, err := scanSQLDeliveries(rows)
	if err != nil {
		return nil, err
	}
	sortDeliveries(deliveries)
	return deliveries, nil
}

func (s *SQLStore) publishMeta(ctx context.Context, db sqlNoticeQuerier, lookupKey string) (map[string]string, error) {
	rows, err := db.QueryContext(ctx, "SELECT meta_json FROM "+quoteSQLIdentifier(s.publish)+" WHERE lookup_key = ? LIMIT 1", lookupKey)
	if err != nil {
		return nil, err
	}
	values, err := scanSQLNullStrings(rows)
	if err != nil {
		return nil, err
	}
	if len(values) == 0 {
		return nil, nil
	}
	return decodeSQLStringMap(values[0])
}

func (s *SQLStore) insertNotice(ctx context.Context, db sqlNoticeExecutor, notice Notice, publishedAt time.Time) error {
	meta, err := encodeSQLStringMap(notice.Meta)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx,
		"INSERT INTO "+quoteSQLIdentifier(s.notices)+" (id, kind, scope, shard_id, title, body, attachment_ref, starts_at, ends_at, disabled, meta_json, published_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		notice.ID,
		notice.Kind,
		notice.Scope,
		notice.ShardID,
		notice.Title,
		nullableSQLString(notice.Body),
		notice.AttachmentRef,
		nullableSQLTime(notice.StartsAt),
		nullableSQLTime(notice.EndsAt),
		notice.Disabled,
		meta,
		publishedAt.UTC(),
	)
	return err
}

func (s *SQLStore) insertDelivery(ctx context.Context, db sqlNoticeExecutor, delivery Delivery) error {
	meta, err := encodeSQLStringMap(delivery.Meta)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx,
		"INSERT INTO "+quoteSQLIdentifier(s.deliveries)+" (id, notice_id, account_id, shard_id, status, attachment_ref, idempotency_key, meta_json, created_at, acknowledged_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		delivery.ID,
		delivery.NoticeID,
		delivery.AccountID,
		delivery.ShardID,
		delivery.Status,
		delivery.AttachmentRef,
		delivery.IdempotencyKey,
		meta,
		delivery.CreatedAt.UTC(),
		nullableSQLTime(delivery.AcknowledgedAt),
	)
	return err
}

func (s *SQLStore) insertPublishKey(ctx context.Context, db sqlNoticeExecutor, lookupKey string, noticeID string, idempotencyKey string, meta map[string]string, createdAt time.Time) error {
	metaJSON, err := encodeSQLStringMap(meta)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx,
		"INSERT INTO "+quoteSQLIdentifier(s.publish)+" (lookup_key, notice_id, idempotency_key, meta_json, created_at) VALUES (?, ?, ?, ?, ?)",
		lookupKey,
		noticeID,
		idempotencyKey,
		metaJSON,
		createdAt.UTC(),
	)
	return err
}

func (s *SQLStore) insertAcknowledgeKey(ctx context.Context, db sqlNoticeExecutor, lookupKey string, deliveryID string, idempotencyKey string, createdAt time.Time) error {
	_, err := db.ExecContext(ctx,
		"INSERT INTO "+quoteSQLIdentifier(s.ack)+" (lookup_key, delivery_id, idempotency_key, created_at) VALUES (?, ?, ?, ?) ON DUPLICATE KEY UPDATE delivery_id = delivery_id",
		lookupKey,
		deliveryID,
		idempotencyKey,
		createdAt.UTC(),
	)
	return err
}

func (s *SQLStore) updateDeliveryAck(ctx context.Context, db sqlNoticeExecutor, delivery Delivery) error {
	result, err := db.ExecContext(ctx,
		"UPDATE "+quoteSQLIdentifier(s.deliveries)+" SET status = ?, acknowledged_at = ? WHERE id = ?",
		delivery.Status,
		nullableSQLTime(delivery.AcknowledgedAt),
		delivery.ID,
	)
	if err != nil {
		return err
	}
	if result == nil {
		return nil
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrDeliveryNotFound
	}
	return nil
}

func (s *SQLStore) updateDeliveryStatus(ctx context.Context, db sqlNoticeExecutor, deliveryID string, status string) error {
	result, err := db.ExecContext(ctx,
		"UPDATE "+quoteSQLIdentifier(s.deliveries)+" SET status = ? WHERE id = ?",
		status,
		deliveryID,
	)
	if err != nil {
		return err
	}
	if result == nil {
		return nil
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrDeliveryNotFound
	}
	return nil
}

func (s *SQLStore) queryStringColumn(ctx context.Context, query string, args ...any) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return scanSQLStrings(rows)
}

func scanSQLNotices(rows SQLRows) ([]Notice, error) {
	notices, _, err := scanSQLPubNotices(rows)
	return notices, err
}

func scanSQLPubNotices(rows SQLRows) (notices []Notice, published []time.Time, err error) {
	if rows == nil {
		return nil, nil, ErrNoticeInvalidRows
	}
	defer closeNoticeRowsErr(rows, &err)
	for rows.Next() {
		notice, publishedAt, err := scanSQLNotice(rows)
		if err != nil {
			return nil, nil, err
		}
		notices = append(notices, notice)
		published = append(published, publishedAt)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return notices, published, nil
}

func scanSQLNotice(rows SQLRows) (Notice, time.Time, error) {
	var notice Notice
	var scope, shardID, body, attachmentRef, meta sql.NullString
	var startsAt, endsAt sql.NullTime
	var publishedAt time.Time
	if err := rows.Scan(
		&notice.ID,
		&notice.Kind,
		&scope,
		&shardID,
		&notice.Title,
		&body,
		&attachmentRef,
		&startsAt,
		&endsAt,
		&notice.Disabled,
		&meta,
		&publishedAt,
	); err != nil {
		return Notice{}, time.Time{}, err
	}
	if scope.Valid {
		notice.Scope = scope.String
	}
	if shardID.Valid {
		notice.ShardID = shardID.String
	}
	if body.Valid {
		notice.Body = body.String
	}
	if attachmentRef.Valid {
		notice.AttachmentRef = attachmentRef.String
	}
	if startsAt.Valid {
		notice.StartsAt = startsAt.Time.UTC()
	}
	if endsAt.Valid {
		notice.EndsAt = endsAt.Time.UTC()
	}
	if meta.Valid {
		decoded, err := decodeSQLStringMap(meta.String)
		if err != nil {
			return Notice{}, time.Time{}, err
		}
		notice.Meta = decoded
	}
	return cloneNotice(notice), publishedAt.UTC(), nil
}

func scanSQLDeliveries(rows SQLRows) (deliveries []Delivery, err error) {
	if rows == nil {
		return nil, ErrNoticeInvalidRows
	}
	defer closeNoticeRowsErr(rows, &err)
	for rows.Next() {
		delivery, err := scanSQLDelivery(rows)
		if err != nil {
			return nil, err
		}
		deliveries = append(deliveries, delivery)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return deliveries, nil
}

func scanSQLDelivery(rows SQLRows) (Delivery, error) {
	var delivery Delivery
	var shardID, attachmentRef, idempotencyKey, meta sql.NullString
	var acknowledgedAt sql.NullTime
	if err := rows.Scan(
		&delivery.ID,
		&delivery.NoticeID,
		&delivery.AccountID,
		&shardID,
		&delivery.Status,
		&attachmentRef,
		&idempotencyKey,
		&meta,
		&delivery.CreatedAt,
		&acknowledgedAt,
	); err != nil {
		return Delivery{}, err
	}
	if shardID.Valid {
		delivery.ShardID = shardID.String
	}
	if attachmentRef.Valid {
		delivery.AttachmentRef = attachmentRef.String
	}
	if idempotencyKey.Valid {
		delivery.IdempotencyKey = idempotencyKey.String
	}
	if meta.Valid {
		decoded, err := decodeSQLStringMap(meta.String)
		if err != nil {
			return Delivery{}, err
		}
		delivery.Meta = decoded
	}
	delivery.CreatedAt = delivery.CreatedAt.UTC()
	if acknowledgedAt.Valid {
		delivery.AcknowledgedAt = acknowledgedAt.Time.UTC()
	}
	return cloneDelivery(delivery), nil
}

func scanSQLStrings(rows SQLRows) ([]string, error) {
	values, err := scanSQLNullStrings(rows)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, strings.TrimSpace(value))
	}
	return out, nil
}

func scanSQLNullStrings(rows SQLRows) (values []string, err error) {
	if rows == nil {
		return nil, ErrNoticeInvalidRows
	}
	defer closeNoticeRowsErr(rows, &err)
	for rows.Next() {
		var value sql.NullString
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		if value.Valid {
			values = append(values, value.String)
		} else {
			values = append(values, "")
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

// closeNoticeRowsErr 在扫描辅助函数返回前合并 Close 错误，避免查询清理失败被静默吞掉。
func closeNoticeRowsErr(rows SQLRows, err *error) {
	if rows == nil || err == nil {
		return
	}
	if closeErr := rows.Close(); closeErr != nil && *err == nil {
		*err = closeErr
	}
}

func noticeColumns() string {
	return "id, kind, scope, shard_id, title, body, attachment_ref, starts_at, ends_at, disabled, meta_json, published_at"
}

func deliveryColumns() string {
	return "id, notice_id, account_id, shard_id, status, attachment_ref, idempotency_key, meta_json, created_at, acknowledged_at"
}

func encodeSQLStringMap(values map[string]string) (any, error) {
	if len(values) == 0 {
		return nil, nil
	}
	data, err := json.Marshal(values)
	if err != nil {
		return nil, err
	}
	return string(data), nil
}

func decodeSQLStringMap(value string) (map[string]string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	var out map[string]string
	if err := json.Unmarshal([]byte(value), &out); err != nil {
		return nil, err
	}
	return normalizeStringMap(out), nil
}

func nullableSQLString(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}

func nullableSQLTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

type sqlStoreTables struct {
	notices    string
	deliveries string
	publish    string
	ack        string
}

func normSQLStoreTables(options SQLStoreOptions) (sqlStoreTables, error) {
	prefix := strings.TrimSpace(options.TablePrefix)
	if prefix == "" {
		prefix = defSQLTablePrefix
	}
	tables := sqlStoreTables{
		notices:    firstNonEmpty(options.NoticeTable, prefix+"_notices"),
		deliveries: firstNonEmpty(options.DeliveryTable, prefix+"_deliveries"),
		publish:    firstNonEmpty(options.PublishKeyTable, prefix+"_publish_keys"),
		ack:        firstNonEmpty(options.AcknowledgeKeyTable, prefix+"_ack_keys"),
	}
	for _, table := range []string{tables.notices, tables.deliveries, tables.publish, tables.ack} {
		if !noticeTablePattern.MatchString(table) {
			return sqlStoreTables{}, ErrNoticeInvalidTable
		}
	}
	return tables, nil
}

func quoteSQLIdentifier(identifier string) string {
	return "`" + identifier + "`"
}

func sqlLookupKey(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func sqlContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func sqlRecoveryCtx(parent context.Context) (context.Context, context.CancelFunc) {
	base := context.Background()
	if parent != nil {
		base = context.WithoutCancel(parent)
	}
	return context.WithTimeout(base, rollbackTimeout)
}

func isNoticeSQLDuplicate(err error) bool {
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}

var noticeTablePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
