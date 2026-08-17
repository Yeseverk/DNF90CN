package notice

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var (
	ErrNoticeSQLDBRequired = errors.New("notice sql db is required")
	ErrNoticeInvalidTable  = errors.New("notice sql table name is invalid")
	ErrNoticeInvalidRows   = errors.New("notice sql rows are invalid")
)

const defSQLTablePrefix = "notice"

type SQLRows interface {
	Next() bool
	Scan(...any) error
	Close() error
	Err() error
}

type SQLTx interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (SQLRows, error)
	Commit() error
	Rollback() error
}

type SQLDB interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (SQLRows, error)
	BeginTx(context.Context, *sql.TxOptions) (SQLTx, error)
}

type SQLDBAdapter struct {
	DB *sql.DB
}

type SQLTxAdapter struct {
	Tx *sql.Tx
}

var (
	_ SQLDB = SQLDBAdapter{}
	_ SQLTx = SQLTxAdapter{}
)

func (a SQLDBAdapter) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if a.DB == nil {
		return nil, ErrNoticeSQLDBRequired
	}
	return a.DB.ExecContext(ctx, query, args...)
}

func (a SQLDBAdapter) QueryContext(ctx context.Context, query string, args ...any) (SQLRows, error) { //nolint:rowserrcheck // 适配层只返回 rows，调用方负责遍历结束后的 Err 检查。
	if a.DB == nil {
		return nil, ErrNoticeSQLDBRequired
	}
	return a.DB.QueryContext(ctx, query, args...) //nolint:rowserrcheck // rows 返回后由上层统一关闭并检查 Err。
}

func (a SQLDBAdapter) BeginTx(ctx context.Context, options *sql.TxOptions) (SQLTx, error) {
	if a.DB == nil {
		return nil, ErrNoticeSQLDBRequired
	}
	tx, err := a.DB.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	return SQLTxAdapter{Tx: tx}, nil
}

func (a SQLTxAdapter) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if a.Tx == nil {
		return nil, ErrNoticeSQLDBRequired
	}
	return a.Tx.ExecContext(ctx, query, args...)
}

func (a SQLTxAdapter) QueryContext(ctx context.Context, query string, args ...any) (SQLRows, error) { //nolint:rowserrcheck // 事务适配层只透传 rows，调用方负责遍历结束后的 Err 检查。
	if a.Tx == nil {
		return nil, ErrNoticeSQLDBRequired
	}
	return a.Tx.QueryContext(ctx, query, args...) //nolint:rowserrcheck // rows 返回后由上层统一关闭并检查 Err。
}

func (a SQLTxAdapter) Commit() error {
	if a.Tx == nil {
		return ErrNoticeSQLDBRequired
	}
	return a.Tx.Commit()
}

func (a SQLTxAdapter) Rollback() error {
	if a.Tx == nil {
		return ErrNoticeSQLDBRequired
	}
	return a.Tx.Rollback()
}

type SQLStoreOptions struct {
	Name                string
	TablePrefix         string
	NoticeTable         string
	DeliveryTable       string
	PublishKeyTable     string
	AcknowledgeKeyTable string
}

type SQLStore struct {
	name       string
	db         SQLDB
	notices    string
	deliveries string
	publish    string
	ack        string
	closer     interface{ Close() error }
}

func NewSQLStore(db SQLDB, options SQLStoreOptions) (*SQLStore, error) {
	if db == nil {
		return nil, ErrNoticeSQLDBRequired
	}
	tables, err := normSQLStoreTables(options)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(options.Name)
	if name == "" {
		name = "sql-notice"
	}
	return &SQLStore{
		name:       name,
		db:         db,
		notices:    tables.notices,
		deliveries: tables.deliveries,
		publish:    tables.publish,
		ack:        tables.ack,
	}, nil
}

func NewSQLStoreFromDB(db *sql.DB, options SQLStoreOptions) (*SQLStore, error) {
	if db == nil {
		return nil, ErrNoticeSQLDBRequired
	}
	store, err := NewSQLStore(SQLDBAdapter{DB: db}, options)
	if err != nil {
		return nil, err
	}
	store.closer = db
	return store, nil
}

func (s *SQLStore) Close() error {
	if s == nil || s.closer == nil {
		return nil
	}
	return s.closer.Close()
}

func (s *SQLStore) EnsureSchema(ctx context.Context) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	ctx = sqlContext(ctx)
	if s == nil || s.db == nil {
		return ErrNoticeSQLDBRequired
	}
	for _, schema := range s.Schema() {
		if _, err := s.db.ExecContext(ctx, schema); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLStore) Schema() []string {
	noticeTable := quoteSQLIdentifier(s.notices)
	deliveryTable := quoteSQLIdentifier(s.deliveries)
	publishTable := quoteSQLIdentifier(s.publish)
	ackTable := quoteSQLIdentifier(s.ack)
	return []string{
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  id VARCHAR(128) NOT NULL,
  kind VARCHAR(32) NOT NULL,
  scope VARCHAR(128) NOT NULL DEFAULT '',
  shard_id VARCHAR(128) NOT NULL DEFAULT '',
  title VARCHAR(255) NOT NULL DEFAULT '',
  body TEXT NULL,
  attachment_ref VARCHAR(255) NOT NULL DEFAULT '',
  starts_at DATETIME(6) NULL,
  ends_at DATETIME(6) NULL,
  disabled BOOLEAN NOT NULL DEFAULT FALSE,
  meta_json JSON NULL,
  published_at DATETIME(6) NOT NULL,
  PRIMARY KEY (id),
  KEY idx_notice_kind (kind, shard_id, starts_at, ends_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`, noticeTable),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  id VARCHAR(64) NOT NULL,
  notice_id VARCHAR(128) NOT NULL,
  account_id VARCHAR(128) NOT NULL,
  shard_id VARCHAR(128) NOT NULL DEFAULT '',
  status VARCHAR(32) NOT NULL,
  attachment_ref VARCHAR(255) NOT NULL DEFAULT '',
  idempotency_key VARCHAR(191) NOT NULL DEFAULT '',
  meta_json JSON NULL,
  created_at DATETIME(6) NOT NULL,
  acknowledged_at DATETIME(6) NULL,
  PRIMARY KEY (id),
  KEY idx_notice_delivery_account (account_id, status, created_at),
  KEY idx_notice_delivery_notice (notice_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`, deliveryTable),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  lookup_key CHAR(64) NOT NULL,
  notice_id VARCHAR(128) NOT NULL,
  idempotency_key VARCHAR(191) NOT NULL,
  meta_json JSON NULL,
  created_at DATETIME(6) NOT NULL,
  PRIMARY KEY (lookup_key),
  KEY idx_notice_publish_notice (notice_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`, publishTable),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  lookup_key CHAR(64) NOT NULL,
  delivery_id VARCHAR(64) NOT NULL,
  idempotency_key VARCHAR(191) NOT NULL,
  created_at DATETIME(6) NOT NULL,
  PRIMARY KEY (lookup_key),
  KEY idx_notice_ack_delivery (delivery_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`, ackTable),
	}
}

func (s *SQLStore) Publish(ctx context.Context, request PublishRequest, now time.Time) (PublishResult, error) {
	if err := ctxErr(ctx); err != nil {
		return PublishResult{}, err
	}
	ctx = sqlContext(ctx)
	if s == nil || s.db == nil {
		return PublishResult{}, ErrNoticeSQLDBRequired
	}
	request = normPublishReq(request)
	if err := validPublishReq(request); err != nil {
		return PublishResult{}, err
	}
	now = normalizeNow(now)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PublishResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	lookupKey := sqlLookupKey(publishLookupKey(request))
	if existingNoticeID, ok, err := s.lookupPublishKey(ctx, tx, lookupKey); err != nil {
		return PublishResult{}, err
	} else if ok {
		result, err := s.publishNoticeResult(ctx, tx, existingNoticeID, request.IdempotencyKey, true)
		if err != nil {
			return PublishResult{}, err
		}
		if publishReplayClash(result, request) {
			return PublishResult{}, ErrIdempotencyConflict
		}
		if err := tx.Commit(); err != nil {
			return PublishResult{}, err
		}
		committed = true
		return result, nil
	}
	if _, ok, err := s.getNotice(ctx, tx, request.Notice.ID); err != nil {
		return PublishResult{}, err
	} else if ok {
		return PublishResult{}, ErrNoticeExists
	}
	if err := s.insertNotice(ctx, tx, request.Notice, now); err != nil {
		return PublishResult{}, err
	}
	deliveries := make([]Delivery, 0, len(request.Recipients))
	for _, accountID := range request.Recipients {
		delivery := Delivery{
			ID:             DeliveryID(request.Notice.ID, accountID),
			NoticeID:       request.Notice.ID,
			AccountID:      accountID,
			ShardID:        request.Notice.ShardID,
			Status:         StatusPending,
			AttachmentRef:  request.Notice.AttachmentRef,
			IdempotencyKey: request.IdempotencyKey,
			Meta:           mergeStringMap(request.Notice.Meta, request.Meta),
			CreatedAt:      now,
		}
		if err := s.insertDelivery(ctx, tx, delivery); err != nil {
			return PublishResult{}, err
		}
		deliveries = append(deliveries, cloneDelivery(delivery))
	}
	sortDeliveries(deliveries)
	if err := s.insertPublishKey(ctx, tx, lookupKey, request.Notice.ID, request.IdempotencyKey, request.Meta, now); err != nil {
		if isNoticeSQLDuplicate(err) {
			_ = tx.Rollback()
			committed = true
			existingNoticeID, ok, lookupErr := s.lookupPublishKey(ctx, s.db, lookupKey)
			if lookupErr != nil {
				return PublishResult{}, lookupErr
			}
			if !ok {
				return PublishResult{}, err
			}
			result, getErr := s.publishNoticeResult(ctx, s.db, existingNoticeID, request.IdempotencyKey, true)
			if getErr != nil {
				return PublishResult{}, getErr
			}
			if publishReplayClash(result, request) {
				return PublishResult{}, ErrIdempotencyConflict
			}
			return result, nil
		}
		return PublishResult{}, err
	}
	if err := tx.Commit(); err != nil {
		if recovered, ok := s.recoverPublishCommit(ctx, request); ok {
			committed = true
			return recovered, nil
		}
		return PublishResult{}, err
	}
	committed = true
	result := PublishResult{
		Accepted:       true,
		Notice:         cloneNotice(request.Notice),
		Deliveries:     deliveries,
		IdempotencyKey: request.IdempotencyKey,
		AdminReceiptID: request.Meta["admin_receipt_id"],
		Meta:           cloneStringMap(request.Meta),
		PublishedAt:    now,
	}
	return clonePublishResult(result), nil
}

func (s *SQLStore) RollbackPublish(ctx context.Context, result PublishResult) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	ctx = sqlContext(ctx)
	if s == nil || s.db == nil {
		return ErrNoticeSQLDBRequired
	}
	if result.Notice.ID == "" || result.IdempotencyKey == "" {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	lookupKey := sqlLookupKey(publishLookupKey(PublishRequest{Notice: result.Notice, IdempotencyKey: result.IdempotencyKey}))
	if _, err := tx.ExecContext(ctx, "DELETE FROM "+quoteSQLIdentifier(s.publish)+" WHERE lookup_key = ?", lookupKey); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM "+quoteSQLIdentifier(s.ack)+" WHERE delivery_id IN (SELECT id FROM "+quoteSQLIdentifier(s.deliveries)+" WHERE notice_id = ?)", result.Notice.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM "+quoteSQLIdentifier(s.deliveries)+" WHERE notice_id = ?", result.Notice.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM "+quoteSQLIdentifier(s.notices)+" WHERE id = ?", result.Notice.ID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *SQLStore) Acknowledge(ctx context.Context, request AcknowledgeRequest, now time.Time) (Delivery, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return Delivery{}, false, err
	}
	ctx = sqlContext(ctx)
	if s == nil || s.db == nil {
		return Delivery{}, false, ErrNoticeSQLDBRequired
	}
	request = normalizeAckRequest(request)
	if request.IdempotencyKey == "" {
		return Delivery{}, false, ErrIdempotencyRequired
	}
	now = normalizeNow(now)

	deliveryID := request.DeliveryID
	if deliveryID == "" {
		deliveryID = DeliveryID(request.NoticeID, request.AccountID)
	}
	if deliveryID == "" {
		return Delivery{}, false, ErrInvalidNoticeRequest
	}
	lookupKey := sqlLookupKey(acknowledgeLookupKey(deliveryID, request.IdempotencyKey))

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Delivery{}, false, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if knownDeliveryID, ok, err := s.lookupAcknowledgeKey(ctx, tx, lookupKey); err != nil {
		return Delivery{}, false, err
	} else if ok {
		delivery, ok, err := s.getDelivery(ctx, tx, knownDeliveryID)
		if err != nil {
			return Delivery{}, true, err
		}
		if !ok {
			return Delivery{}, true, ErrDeliveryNotFound
		}
		if err := tx.Commit(); err != nil {
			return Delivery{}, true, err
		}
		committed = true
		return cloneDelivery(delivery), true, nil
	}
	delivery, ok, err := s.getDelivery(ctx, tx, deliveryID)
	if err != nil {
		return Delivery{}, false, err
	}
	if !ok {
		return Delivery{}, false, ErrDeliveryNotFound
	}
	if delivery.Status == StatusDeleted {
		return Delivery{}, false, ErrDeliveryNotFound
	}
	duplicate := false
	if delivery.Status == StatusAcknowledged {
		duplicate = true
	} else {
		delivery.Status = StatusAcknowledged
		delivery.AcknowledgedAt = now
		if err := s.updateDeliveryAck(ctx, tx, delivery); err != nil {
			return Delivery{}, false, err
		}
	}
	if err := s.insertAcknowledgeKey(ctx, tx, lookupKey, delivery.ID, request.IdempotencyKey, now); err != nil && !isNoticeSQLDuplicate(err) {
		return Delivery{}, false, err
	}
	if err := tx.Commit(); err != nil {
		if recovered, ok := s.recoverAckCommit(ctx, lookupKey, delivery.ID); ok {
			committed = true
			return recovered, duplicate, nil
		}
		return Delivery{}, duplicate, err
	}
	committed = true
	return cloneDelivery(delivery), duplicate, nil
}

func (s *SQLStore) Delete(ctx context.Context, request DeleteRequest) (DeleteResult, error) {
	if err := ctxErr(ctx); err != nil {
		return DeleteResult{}, err
	}
	ctx = sqlContext(ctx)
	if s == nil || s.db == nil {
		return DeleteResult{}, ErrNoticeSQLDBRequired
	}
	request = normDeleteReq(request)
	if len(request.DeliveryIDs) == 0 {
		return DeleteResult{}, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return DeleteResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	result := DeleteResult{}
	for _, id := range request.DeliveryIDs {
		delivery, ok, err := s.getDelivery(ctx, tx, id)
		if err != nil {
			return DeleteResult{}, err
		}
		if !ok || delivery.Status == StatusDeleted || (request.AccountID != "" && delivery.AccountID != request.AccountID) {
			result.MissingIDs = append(result.MissingIDs, id)
			continue
		}
		delivery.Status = StatusDeleted
		if err := s.updateDeliveryStatus(ctx, tx, delivery.ID, StatusDeleted); err != nil {
			return DeleteResult{}, err
		}
		result.DeletedIDs = append(result.DeletedIDs, id)
	}
	if err := tx.Commit(); err != nil {
		return DeleteResult{}, err
	}
	committed = true
	sort.Strings(result.DeletedIDs)
	sort.Strings(result.MissingIDs)
	return result, nil
}

func (s *SQLStore) ListForAccount(ctx context.Context, query Query) ([]Delivery, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	ctx = sqlContext(ctx)
	if s == nil || s.db == nil {
		return nil, ErrNoticeSQLDBRequired
	}
	query.AccountID = strings.TrimSpace(query.AccountID)
	query.ShardID = strings.TrimSpace(query.ShardID)
	if query.AccountID == "" {
		return nil, ErrRecipientRequired
	}
	now := normalizeNow(query.Now)
	rows, err := s.db.QueryContext(ctx, "SELECT "+deliveryColumns()+" FROM "+quoteSQLIdentifier(s.deliveries)+" WHERE account_id = ? AND status <> ? ORDER BY created_at ASC, id ASC", query.AccountID, StatusDeleted)
	if err != nil {
		return nil, err
	}
	deliveries, err := scanSQLDeliveries(rows)
	if err != nil {
		return nil, err
	}
	out := make([]Delivery, 0, len(deliveries))
	for _, delivery := range deliveries {
		notice, ok, err := s.getNotice(ctx, s.db, delivery.NoticeID)
		if err != nil {
			return nil, err
		}
		if !ok || !noticeVisible(notice, query.ShardID, now) {
			continue
		}
		out = append(out, cloneDelivery(delivery))
	}
	sortAcctDeliveries(out)
	return out, nil
}

func (s *SQLStore) ActiveAnnouncements(ctx context.Context, query Query) ([]Notice, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	ctx = sqlContext(ctx)
	if s == nil || s.db == nil {
		return nil, ErrNoticeSQLDBRequired
	}
	query.ShardID = strings.TrimSpace(query.ShardID)
	now := normalizeNow(query.Now)
	rows, err := s.db.QueryContext(ctx, "SELECT "+noticeColumns()+" FROM "+quoteSQLIdentifier(s.notices)+" WHERE kind = ? ORDER BY id ASC", KindAnnouncement)
	if err != nil {
		return nil, err
	}
	notices, err := scanSQLNotices(rows)
	if err != nil {
		return nil, err
	}
	out := make([]Notice, 0, len(notices))
	for _, notice := range notices {
		if noticeVisible(notice, query.ShardID, now) {
			out = append(out, cloneNotice(notice))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *SQLStore) Snapshot(ctx context.Context) (Snapshot, error) {
	if err := ctxErr(ctx); err != nil {
		return Snapshot{}, err
	}
	ctx = sqlContext(ctx)
	if s == nil || s.db == nil {
		return Snapshot{}, ErrNoticeSQLDBRequired
	}
	notices, err := s.queryStringColumn(ctx, "SELECT kind FROM "+quoteSQLIdentifier(s.notices))
	if err != nil {
		return Snapshot{}, err
	}
	statuses, err := s.queryStringColumn(ctx, "SELECT status FROM "+quoteSQLIdentifier(s.deliveries))
	if err != nil {
		return Snapshot{}, err
	}
	publishKeys, err := s.queryStringColumn(ctx, "SELECT lookup_key FROM "+quoteSQLIdentifier(s.publish))
	if err != nil {
		return Snapshot{}, err
	}
	ackKeys, err := s.queryStringColumn(ctx, "SELECT lookup_key FROM "+quoteSQLIdentifier(s.ack))
	if err != nil {
		return Snapshot{}, err
	}
	snapshot := Snapshot{
		Notices:         len(notices),
		Deliveries:      len(statuses),
		ByKind:          make(map[string]int),
		ByStatus:        make(map[string]int),
		PublishKeys:     len(publishKeys),
		AcknowledgeKeys: len(ackKeys),
	}
	for _, kind := range notices {
		snapshot.ByKind[kind]++
	}
	for _, status := range statuses {
		snapshot.ByStatus[status]++
	}
	if len(snapshot.ByKind) == 0 {
		snapshot.ByKind = nil
	}
	if len(snapshot.ByStatus) == 0 {
		snapshot.ByStatus = nil
	}
	return snapshot, nil
}

func (s *SQLStore) publishNoticeResult(ctx context.Context, db sqlNoticeQuerier, noticeID string, idempotencyKey string, duplicate bool) (PublishResult, error) {
	notice, publishedAt, ok, err := s.getNoticePubAt(ctx, db, noticeID)
	if err != nil {
		return PublishResult{}, err
	}
	if !ok {
		return PublishResult{}, ErrNoticeNotFound
	}
	deliveries, err := s.deliveriesForNotice(ctx, db, noticeID)
	if err != nil {
		return PublishResult{}, err
	}
	meta, err := s.publishMeta(ctx, db, sqlLookupKey(publishLookupKey(PublishRequest{Notice: notice, IdempotencyKey: idempotencyKey})))
	if err != nil {
		return PublishResult{}, err
	}
	result := PublishResult{
		Accepted:       true,
		Duplicate:      duplicate,
		Notice:         notice,
		Deliveries:     deliveries,
		IdempotencyKey: idempotencyKey,
		AdminReceiptID: meta["admin_receipt_id"],
		Meta:           meta,
		PublishedAt:    publishedAt,
	}
	return clonePublishResult(result), nil
}

func (s *SQLStore) recoverPublishCommit(parent context.Context, request PublishRequest) (PublishResult, bool) {
	ctx, cancel := sqlRecoveryCtx(parent)
	defer cancel()
	lookupKey := sqlLookupKey(publishLookupKey(request))
	existingNoticeID, ok, err := s.lookupPublishKey(ctx, s.db, lookupKey)
	if err != nil || !ok {
		return PublishResult{}, false
	}
	result, err := s.publishNoticeResult(ctx, s.db, existingNoticeID, request.IdempotencyKey, false)
	if err != nil || publishReplayClash(result, request) {
		return PublishResult{}, false
	}
	return result, true
}

func (s *SQLStore) recoverAckCommit(parent context.Context, lookupKey string, deliveryID string) (Delivery, bool) {
	ctx, cancel := sqlRecoveryCtx(parent)
	defer cancel()
	knownDeliveryID, ok, err := s.lookupAcknowledgeKey(ctx, s.db, lookupKey)
	if err != nil || !ok || knownDeliveryID != deliveryID {
		return Delivery{}, false
	}
	delivery, ok, err := s.getDelivery(ctx, s.db, knownDeliveryID)
	if err != nil || !ok || delivery.Status == StatusDeleted {
		return Delivery{}, false
	}
	return cloneDelivery(delivery), true
}
