package idempotency

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	mysqlRequestsTable  = "idempotency_requests"
	mysqlSequencesTable = "idempotency_sequences"
	mysqlResvTable      = "idempotency_sequence_reservations"
	mysqlResultsTable   = "idempotency_results"
	idemRecoverTO       = 3 * time.Second
	mysqlFingerprintTag = "fingerprint:v1:"
	mysqlMetadataTag    = "idempotency:v2:"
)

type mysqlRequestMeta struct {
	fingerprint     string
	fingerprintHash string
	ownerToken      string
	legacy          bool
	expiresAt       time.Time
}

type MySQLOptions struct {
	DSN              string
	MaxOpenConns     int
	MaxIdleConns     int
	ConnMaxLifetime  time.Duration
	KeyPrefix        string
	TTL              time.Duration
	Now              func() time.Time
	EnsureSchema     bool
	DB               *sql.DB
	CleanupInterval  time.Duration
	CleanupBatchSize int
}

type mySQLStore struct {
	db           *sql.DB
	initErr      error
	ensureSchema bool
	schemaMu     sync.Mutex
	schemaReady  bool
	schemaErr    error
	keyPrefix    string
	ttl          time.Duration
	now          func() time.Time
	cleanupEvery time.Duration
	cleanupLimit int
	cleanupOnce  sync.Once
	cleanupMu    sync.Mutex
	cleanupStop  context.CancelFunc
	cleanupWG    sync.WaitGroup
	closed       bool
}

func NewMySQL(options MySQLOptions) *Guard {
	options = normMySQLOpts(options)
	return New(Options{
		TTL:   options.TTL,
		Now:   options.Now,
		Kind:  "mysql",
		Store: newMySQLStore(options),
	})
}

func EnsureMySQLSchema(ctx context.Context, options MySQLOptions) error {
	options.EnsureSchema = true
	options.CleanupInterval = -1
	store := newMySQLStore(options)
	if options.DB == nil {
		defer func() { _ = store.Close(context.Background()) }()
	}
	return store.ready(ctx)
}

func newMySQLStore(options MySQLOptions) *mySQLStore {
	options = normMySQLOpts(options)
	conn := options.DB
	var err error
	if conn == nil {
		conn, err = sql.Open("mysql", options.DSN)
	}
	if conn != nil {
		conn.SetMaxOpenConns(options.MaxOpenConns)
		conn.SetMaxIdleConns(options.MaxIdleConns)
		conn.SetConnMaxLifetime(options.ConnMaxLifetime)
	}
	return &mySQLStore{
		db:           conn,
		initErr:      err,
		ensureSchema: options.EnsureSchema,
		keyPrefix:    options.KeyPrefix,
		ttl:          options.TTL,
		now:          options.Now,
		cleanupEvery: options.CleanupInterval,
		cleanupLimit: options.CleanupBatchSize,
	}
}

func normMySQLOpts(options MySQLOptions) MySQLOptions {
	options.DSN = strings.TrimSpace(options.DSN)
	if options.DSN == "" {
		options.DSN = "longheng:longheng@tcp(127.0.0.1:3306)/longheng?parseTime=true&charset=utf8mb4,utf8"
	}
	options.KeyPrefix = strings.TrimSpace(options.KeyPrefix)
	if options.KeyPrefix == "" {
		options.KeyPrefix = "idempotency"
	}
	if options.MaxOpenConns <= 0 {
		options.MaxOpenConns = 32
	}
	if options.MaxIdleConns <= 0 {
		options.MaxIdleConns = 8
	}
	if options.ConnMaxLifetime <= 0 {
		options.ConnMaxLifetime = 5 * time.Minute
	}
	if options.TTL <= 0 {
		options.TTL = 10 * time.Minute
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.CleanupInterval == 0 {
		options.CleanupInterval = options.TTL / 2
		if options.CleanupInterval < 30*time.Second {
			options.CleanupInterval = 30 * time.Second
		}
		if options.CleanupInterval > 5*time.Minute {
			options.CleanupInterval = 5 * time.Minute
		}
	}
	if options.CleanupBatchSize <= 0 {
		options.CleanupBatchSize = 500
	}
	return options
}

func (s *mySQLStore) Check(ctx context.Context, item Request) (Decision, error) {
	decision, err := s.Begin(ctx, item)
	if err != nil {
		return Decision{}, err
	}
	if decision.Status == StatusAccepted {
		if err := s.Commit(ctx, item, decision); err != nil {
			return Decision{}, err
		}
	}
	return decision, nil
}

func (s *mySQLStore) Begin(ctx context.Context, item Request) (Decision, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := s.ready(ctx); err != nil {
		return Decision{}, err
	}
	now := item.Now
	if now.IsZero() {
		now = s.nowOrDefault()
	} else {
		now = now.UTC()
	}
	// Begin 在 MySQL 事务里同时检查 committed、pending 和 sequence 水位；这是多 logic 节点的权威防重入口。
	key := item.Key
	if key == "" {
		key = DerivedKey(item.Scope, item.Subject, item.Session, item.Sequence)
	}
	guardKey := s.requestKey(key)
	seqScope := sequenceScope(item.Scope, item.Subject, item.Session)
	seqKey := s.sequenceKey(seqScope)
	expiresAt := now.Add(s.ttlOrDefault())

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Decision{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if _, err := tx.ExecContext(ctx, //nolint:gosec // G202：SQL 只拼接框架固定表名/列名，业务值全部通过参数绑定。
		"DELETE FROM "+mysqlQuoteIdentifier(mysqlRequestsTable)+" WHERE "+mysqlQuoteIdentifier("guard_key")+" = ? AND "+mysqlQuoteIdentifier("expires_at")+" <= ?", //nolint:gosec // G202：SQL 只拼接框架固定表名/列名，业务值全部通过参数绑定。
		guardKey,
		now,
	); err != nil {
		return Decision{}, err
	}
	status, exists, err := mysqlRequestStatus(ctx, tx, guardKey, now)
	if err != nil {
		return Decision{}, err
	}
	if exists {
		metadata, _, metadataErr := mysqlRequestMetadata(ctx, tx, guardKey, now, key)
		if metadataErr != nil {
			return Decision{}, metadataErr
		}
		if metadata.conflicts(item.Fingerprint) {
			return Decision{}, ErrRequestConflict
		}
		if err := tx.Commit(); err != nil {
			if recovered, ok := s.beginDecisionRec(ctx, guardKey, now, key, item); ok {
				committed = true
				return recovered, nil
			}
			return Decision{}, err
		}
		committed = true
		return mysqlStatusDecision(status, key, item.Sequence, metadata.decisionFingerprint(item.Fingerprint), metadata.ownerToken, metadata.expiresAt), nil
	}
	if item.Sequence > 0 {
		highest, err := s.lockSequence(ctx, tx, item, seqKey, now)
		if err != nil {
			return Decision{}, err
		}
		if highest >= item.Sequence {
			if err := insertMySQLRequest(ctx, tx, guardKey, item, key, item.Fingerprint, item.reservationToken, StatusReplay, now, expiresAt); err != nil && !isMySQLDuplicate(err) {
				return Decision{}, err
			}
			if err := tx.Commit(); err != nil {
				if s.beginStatusRecovered(ctx, guardKey, key, now, StatusReplay, item.Fingerprint, item.reservationToken) {
					committed = true
					return Decision{Status: StatusReplay, Key: key, Sequence: item.Sequence, fingerprint: item.Fingerprint, expiresAt: expiresAt}, nil
				}
				return Decision{}, err
			}
			committed = true
			return Decision{Status: StatusReplay, Key: key, Sequence: item.Sequence, fingerprint: item.Fingerprint, expiresAt: expiresAt}, nil
		}
		pendingHighest, err := mysqlHighestSeq(ctx, tx, seqKey, now)
		if err != nil {
			return Decision{}, err
		}
		if pendingHighest > 0 {
			if err := tx.Commit(); err != nil {
				if s.beginSeqRecovered(ctx, seqKey, now) {
					committed = true
					return Decision{Status: StatusInFlight, Key: key, Sequence: item.Sequence}, nil
				}
				return Decision{}, err
			}
			committed = true
			return Decision{Status: StatusInFlight, Key: key, Sequence: item.Sequence}, nil
		}
	}
	if err := insertMySQLRequest(ctx, tx, guardKey, item, key, item.Fingerprint, item.reservationToken, statusPending, now, expiresAt); err != nil {
		if isMySQLDuplicate(err) {
			status, exists, statusErr := mysqlRequestStatus(ctx, tx, guardKey, now)
			if statusErr != nil {
				return Decision{}, statusErr
			}
			metadata, _, metadataErr := mysqlRequestMetadata(ctx, tx, guardKey, now, key)
			if metadataErr != nil {
				return Decision{}, metadataErr
			}
			if metadata.conflicts(item.Fingerprint) {
				return Decision{}, ErrRequestConflict
			}
			if err := tx.Commit(); err != nil {
				if recovered, ok := s.beginDecisionRec(ctx, guardKey, now, key, item); ok {
					committed = true
					return recovered, nil
				}
				return Decision{}, err
			}
			committed = true
			if exists {
				return mysqlStatusDecision(status, key, item.Sequence, metadata.decisionFingerprint(item.Fingerprint), metadata.ownerToken, metadata.expiresAt), nil
			}
			return Decision{Status: StatusInFlight, Key: key, Sequence: item.Sequence}, nil
		}
		return Decision{}, err
	}
	if item.Sequence > 0 {
		if err := insertMySQLReserve(ctx, tx, seqKey, guardKey, item.Sequence, expiresAt); err != nil {
			return Decision{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		if s.beginStatusRecovered(ctx, guardKey, key, now, statusPending, item.Fingerprint, item.reservationToken) {
			committed = true
			return Decision{Status: StatusAccepted, Key: key, Sequence: item.Sequence, ownerToken: item.reservationToken}, nil
		}
		return Decision{}, err
	}
	committed = true
	return Decision{Status: StatusAccepted, Key: key, Sequence: item.Sequence, ownerToken: item.reservationToken}, nil
}

func (s *mySQLStore) Commit(ctx context.Context, item Request, decision Decision) error {
	_, err := s.commitWithExpiry(ctx, item, decision, nil, false)
	return err
}

func (s *mySQLStore) CommitResult(ctx context.Context, item Request, decision Decision, payload []byte) error {
	_, err := s.commitWithExpiry(ctx, item, decision, payload, true)
	return err
}

func (s *mySQLStore) commitWithExpiry(ctx context.Context, item Request, decision Decision, payload []byte, withResult bool) (time.Time, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := s.ready(ctx); err != nil {
		return time.Time{}, err
	}
	// committed/result 从真实提交时刻起算 TTL，不继承请求入口时间。
	now := s.nowOrDefault()
	expiresAt := now.Add(s.ttlOrDefault())
	if err := s.commitAt(ctx, item, decision, payload, withResult, now, expiresAt); err != nil {
		return time.Time{}, err
	}
	return expiresAt, nil
}

func (s *mySQLStore) commitAt(ctx context.Context, item Request, decision Decision, payload []byte, withResult bool, now, expiresAt time.Time) error {
	// Commit 删除 reservation 并写 committed/sequence；业务成功前不能调用，否则会造成“请求未执行但重试被吞”。
	key := decision.Key
	if key == "" {
		key = item.Key
	}
	if key == "" {
		key = DerivedKey(item.Scope, item.Subject, item.Session, item.Sequence)
	}
	guardKey := s.requestKey(key)
	seqScope := sequenceScope(item.Scope, item.Subject, item.Session)
	seqKey := s.sequenceKey(seqScope)
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
	status, exists, err := mysqlRequestStatus(ctx, tx, guardKey, now)
	if err != nil {
		return err
	}
	if exists {
		metadata, _, metadataErr := mysqlRequestMetadata(ctx, tx, guardKey, now, key)
		if metadataErr != nil {
			return metadataErr
		}
		if metadata.conflicts(item.Fingerprint) {
			return ErrRequestConflict
		}
		if metadata.ownerToken != decision.ownerToken {
			return ErrReservationLost
		}
		if decision.ownerToken != "" && status != statusPending && status != StatusAccepted {
			return ErrReservationLost
		}
	} else if decision.ownerToken != "" {
		return ErrReservationLost
	}

	if item.Sequence > 0 {
		highest, err := s.lockSequence(ctx, tx, item, seqKey, now)
		if err != nil {
			return err
		}
		if highest < item.Sequence {
			if _, err := tx.ExecContext(ctx, //nolint:gosec // G202：SQL 只拼接框架固定表名/列名，sequence 值通过参数绑定。
				"UPDATE "+mysqlQuoteIdentifier(mysqlSequencesTable)+" SET "+mysqlQuoteIdentifier("highest_sequence")+" = ?, "+mysqlQuoteIdentifier("updated_at")+" = ? WHERE "+mysqlQuoteIdentifier("sequence_key")+" = ?", //nolint:gosec // G202：SQL 只拼接框架固定表名/列名，sequence 值通过参数绑定。
				item.Sequence,
				now,
				seqKey,
			); err != nil {
				return err
			}
		}
	}
	if err := upsertMySQLStatus(ctx, tx, guardKey, item, key, item.Fingerprint, decision.ownerToken, StatusAccepted, now, expiresAt); err != nil {
		return err
	}
	if withResult {
		if err := upsertMySQLResult(ctx, tx, guardKey, payload, now, expiresAt); err != nil {
			return err
		}
	}
	if item.Sequence > 0 {
		if _, err := tx.ExecContext(ctx, //nolint:gosec // G202：SQL 只拼接框架固定表名/列名，业务值通过参数绑定。
			"DELETE FROM "+mysqlQuoteIdentifier(mysqlResvTable)+" WHERE "+mysqlQuoteIdentifier("guard_key")+" = ?", //nolint:gosec // G202：SQL 只拼接框架固定表名/列名，业务值通过参数绑定。
			guardKey,
		); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		recovered := s.commitRecovered(ctx, guardKey, key, now, decision.ownerToken)
		if withResult {
			recovered = recovered && s.resultRecovered(ctx, guardKey, now, payload)
		}
		if recovered {
			committed = true
			return nil
		}
		return err
	}
	committed = true
	return nil
}

func (s *mySQLStore) LookupResult(ctx context.Context, decision Decision) ([]byte, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := s.ready(ctx); err != nil {
		return nil, false, err
	}
	key := strings.TrimSpace(decision.Key)
	if key == "" {
		return nil, false, nil
	}
	payload, found, err := mysqlResultDB(ctx, s.db, s.requestKey(key), s.nowOrDefault())
	if err != nil || !found {
		return nil, found, err
	}
	return append([]byte(nil), payload...), true, nil
}

func (s *mySQLStore) commitRecovered(parent context.Context, guardKey, key string, now time.Time, ownerToken string) bool {
	ctx, cancel := idemRecoverCtx(parent)
	defer cancel()
	status, exists, err := mysqlStatusDB(ctx, s.db, guardKey, now)
	if err != nil || !exists || status != StatusAccepted {
		return false
	}
	metadata, _, err := mysqlMetadataDB(ctx, s.db, guardKey, now, key)
	return err == nil && metadata.ownerToken == ownerToken
}

func (s *mySQLStore) resultRecovered(parent context.Context, guardKey string, now time.Time, want []byte) bool {
	ctx, cancel := idemRecoverCtx(parent)
	defer cancel()
	payload, found, err := mysqlResultDB(ctx, s.db, guardKey, now)
	return err == nil && found && string(payload) == string(want)
}

func (s *mySQLStore) beginDecisionRec(parent context.Context, guardKey string, now time.Time, key string, item Request) (Decision, bool) {
	ctx, cancel := idemRecoverCtx(parent)
	defer cancel()
	status, exists, err := mysqlStatusDB(ctx, s.db, guardKey, now)
	if err != nil || !exists {
		return Decision{}, false
	}
	metadata, _, err := mysqlMetadataDB(ctx, s.db, guardKey, now, key)
	if err != nil || metadata.conflicts(item.Fingerprint) {
		return Decision{}, false
	}
	return mysqlStatusDecision(status, key, item.Sequence, metadata.decisionFingerprint(item.Fingerprint), metadata.ownerToken, metadata.expiresAt), true
}

func (s *mySQLStore) beginStatusRecovered(parent context.Context, guardKey, key string, now time.Time, want Status, fingerprint, ownerToken string) bool {
	ctx, cancel := idemRecoverCtx(parent)
	defer cancel()
	status, exists, err := mysqlStatusDB(ctx, s.db, guardKey, now)
	if err != nil || !exists || status != want {
		return false
	}
	metadata, _, err := mysqlMetadataDB(ctx, s.db, guardKey, now, key)
	return err == nil && !metadata.conflicts(fingerprint) && metadata.ownerToken == ownerToken
}

func (s *mySQLStore) beginSeqRecovered(parent context.Context, seqKey string, now time.Time) bool {
	ctx, cancel := idemRecoverCtx(parent)
	defer cancel()
	highest, err := mysqlHighestSeqDB(ctx, s.db, seqKey, now)
	return err == nil && highest > 0
}

func (s *mySQLStore) Abort(ctx context.Context, item Request, decision Decision) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := s.ready(ctx); err != nil {
		return err
	}
	// Abort 只删除 reservation，保留重试机会，不推进 committed 或 sequence 水位。
	key := decision.Key
	if key == "" {
		key = item.Key
	}
	if key == "" {
		key = DerivedKey(item.Scope, item.Subject, item.Session, item.Sequence)
	}
	guardKey := s.requestKey(key)
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
	now := s.nowOrDefault()
	status, exists, err := mysqlRequestStatus(ctx, tx, guardKey, now)
	if err != nil {
		return err
	}
	if exists {
		metadata, _, metadataErr := mysqlRequestMetadata(ctx, tx, guardKey, now, key)
		if metadataErr != nil {
			return metadataErr
		}
		if metadata.conflicts(item.Fingerprint) {
			return ErrRequestConflict
		}
		if status != statusPending || metadata.ownerToken != decision.ownerToken {
			return ErrReservationLost
		}
	} else if decision.ownerToken != "" {
		return ErrReservationLost
	}
	if _, err := tx.ExecContext(ctx, //nolint:gosec // G202：SQL 只拼接框架固定表名/列名，删除条件通过参数绑定。
		"DELETE FROM "+mysqlQuoteIdentifier(mysqlRequestsTable)+" WHERE "+mysqlQuoteIdentifier("guard_key")+" = ? AND "+mysqlQuoteIdentifier("status")+" = ?", //nolint:gosec // G202：SQL 只拼接框架固定表名/列名，删除条件通过参数绑定。
		guardKey,
		string(statusPending),
	); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, //nolint:gosec // G202：SQL 只拼接框架固定表名/列名，删除条件通过参数绑定。
		"DELETE FROM "+mysqlQuoteIdentifier(mysqlResvTable)+" WHERE "+mysqlQuoteIdentifier("guard_key")+" = ?", //nolint:gosec // G202：SQL 只拼接框架固定表名/列名，删除条件通过参数绑定。
		guardKey,
	); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		if s.abortRecovered(ctx, guardKey, s.nowOrDefault()) {
			committed = true
			return nil
		}
		return err
	}
	committed = true
	return nil
}

func (s *mySQLStore) abortRecovered(parent context.Context, guardKey string, now time.Time) bool {
	ctx, cancel := idemRecoverCtx(parent)
	defer cancel()
	status, exists, err := mysqlStatusDB(ctx, s.db, guardKey, now)
	if err != nil {
		return false
	}
	if exists && status == statusPending {
		return false
	}
	exists, err = mysqlReservationDB(ctx, s.db, guardKey, now)
	return err == nil && !exists
}

func (s *mySQLStore) Snapshot() map[string]any {
	if s == nil {
		return map[string]any{}
	}
	return map[string]any{
		"backend":                  "mysql",
		"ttl_seconds":              int64(s.ttlOrDefault() / time.Second),
		"key_prefix":               s.keyPrefixOrDefault(),
		"cleanup_interval_seconds": int64(s.cleanupEvery / time.Second),
		"cleanup_batch_size":       s.cleanupLimit,
	}
}

func (s *mySQLStore) Close(context.Context) error {
	if s == nil || s.db == nil {
		return nil
	}
	s.cleanupMu.Lock()
	s.closed = true
	stop := s.cleanupStop
	s.cleanupMu.Unlock()
	if stop != nil {
		// 先停后台清理 goroutine，再关闭 DB，避免清理任务和 Close 竞争同一个连接池。
		stop()
		s.cleanupWG.Wait()
	}
	return s.db.Close()
}

func (s *mySQLStore) ready(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || s.db == nil {
		return fmt.Errorf("mysql idempotency store is nil")
	}
	if s.initErr != nil {
		return s.initErr
	}
	if s.ensureSchema {
		if err := s.ensureSchemaReady(ctx); err != nil {
			return err
		}
	}
	s.startCleanup()
	return nil
}

func (s *mySQLStore) ensureSchemaReady(ctx context.Context) error {
	s.schemaMu.Lock()
	defer s.schemaMu.Unlock()
	if s.schemaReady {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, mysqlReqTableDDL()); err != nil {
		s.schemaErr = err
		return err
	}
	if _, err := s.db.ExecContext(ctx, mysqlSeqTableDDL()); err != nil {
		s.schemaErr = err
		return err
	}
	if _, err := s.db.ExecContext(ctx, mysqlResvTableDDL()); err != nil {
		s.schemaErr = err
		return err
	}
	if _, err := s.db.ExecContext(ctx, mysqlResultTableDDL()); err != nil {
		s.schemaErr = err
		return err
	}
	s.schemaErr = nil
	s.schemaReady = true
	return nil
}

func (s *mySQLStore) startCleanup() {
	if s == nil || s.cleanupEvery <= 0 || s.cleanupLimit <= 0 {
		return
	}
	s.cleanupOnce.Do(func() {
		// 清理只删除过期 pending/committed/sequence，不能影响仍在 ttl 窗口内的幂等判定。
		s.cleanupMu.Lock()
		defer s.cleanupMu.Unlock()
		if s.closed {
			return
		}
		ctx, cancel := context.WithCancel(context.Background())
		s.cleanupStop = cancel
		s.cleanupWG.Add(1)
		go s.cleanupLoop(ctx)
	})
}

func (s *mySQLStore) cleanupLoop(ctx context.Context) {
	defer s.cleanupWG.Done()
	// cleanupLoop 使用独立后台 ctx；Close 会 cancel 并等待它退出，防止测试和服务停机泄漏 goroutine。
	ticker := time.NewTicker(s.cleanupEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			timeout := s.cleanupEvery
			if timeout > 30*time.Second {
				timeout = 30 * time.Second
			}
			cleanupCtx, cancel := context.WithTimeout(ctx, timeout)
			_ = s.cleanupExpired(cleanupCtx)
			cancel()
		}
	}
}

func (s *mySQLStore) cleanupExpired(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := s.ready(ctx); err != nil {
		return err
	}
	now := s.nowOrDefault()
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
	limit := s.cleanupLimit
	if limit <= 0 {
		limit = 500
	}
	if _, err := tx.ExecContext(ctx, // #nosec G202 -- SQL 只拼接框架固定表名/列名，删除条件通过参数绑定。
		mysqlCleanupSQL(mysqlResvTable, "expires_at", limit),
		now,
	); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, // #nosec G202 -- SQL 只拼接框架固定表名/列名，过期时间通过参数绑定。
		mysqlCleanupSQL(mysqlRequestsTable, "expires_at", limit),
		now,
	); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, // #nosec G202 -- SQL 只拼接框架固定表名/列名，过期时间通过参数绑定。
		mysqlCleanupSQL(mysqlResultsTable, "expires_at", limit),
		now,
	); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, // #nosec G202 -- SQL 只拼接框架固定表名/列名，序列键通过参数绑定。
		mysqlCleanupSQL(mysqlSequencesTable, "updated_at", limit),
		mysqlSeqExpiryCutoff(now, s.ttlOrDefault()),
	); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *mySQLStore) lockSequence(ctx context.Context, tx *sql.Tx, item Request, seqKey string, now time.Time) (uint64, error) {
	// sequence 行通过 SELECT ... FOR UPDATE 串行化同一账号/session 的序列水位，防止并发包乱序通过。
	if _, err := tx.ExecContext(ctx, //nolint:gosec // G202：SQL 只拼接框架固定表名/列名，序列清理条件通过参数绑定。
		"DELETE FROM "+mysqlQuoteIdentifier(mysqlSequencesTable)+" WHERE "+mysqlQuoteIdentifier("sequence_key")+" = ? AND "+mysqlQuoteIdentifier("updated_at")+" <= ?", //nolint:gosec // G202：SQL 只拼接框架固定表名/列名，序列清理条件通过参数绑定。
		seqKey,
		mysqlSeqExpiryCutoff(now, s.ttlOrDefault()),
	); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, //nolint:gosec // G202：SQL 只拼接框架固定表名/列名，sequence 行字段通过参数绑定。
		"INSERT INTO "+mysqlQuoteIdentifier(mysqlSequencesTable)+" ("+ //nolint:gosec // G202：SQL 只拼接框架固定表名/列名，sequence 行字段通过参数绑定。
			strings.Join([]string{
				mysqlQuoteIdentifier("sequence_key"),
				mysqlQuoteIdentifier("scope"),
				mysqlQuoteIdentifier("subject"),
				mysqlQuoteIdentifier("session_id"),
				mysqlQuoteIdentifier("highest_sequence"),
				mysqlQuoteIdentifier("updated_at"),
			}, ", ")+") VALUES (?, ?, ?, ?, 0, ?) ON DUPLICATE KEY UPDATE "+mysqlQuoteIdentifier("sequence_key")+" = "+mysqlQuoteIdentifier("sequence_key"),
		seqKey,
		limitStorageText(item.Scope, 128),
		limitStorageText(item.Subject, 128),
		limitStorageText(item.Session, 128),
		now,
	); err != nil {
		return 0, err
	}
	var highest uint64
	if err := tx.QueryRowContext(ctx,
		"SELECT "+mysqlQuoteIdentifier("highest_sequence")+" FROM "+mysqlQuoteIdentifier(mysqlSequencesTable)+" WHERE "+mysqlQuoteIdentifier("sequence_key")+" = ? FOR UPDATE",
		seqKey,
	).Scan(&highest); err != nil {
		return 0, err
	}
	return highest, nil
}

func mysqlSeqExpiryCutoff(now time.Time, ttl time.Duration) time.Time {
	if ttl <= 0 {
		return now
	}
	return now.Add(-ttl)
}

func mysqlRequestStatus(ctx context.Context, tx *sql.Tx, guardKey string, now time.Time) (Status, bool, error) {
	var status string
	err := tx.QueryRowContext(ctx, // #nosec G202 -- SQL 只拼接框架固定表名/列名，查询条件通过参数绑定。
		mysqlReqStatusSQL(),
		guardKey,
		now,
	).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return Status(status), true, nil
}

func mysqlRequestMetadata(ctx context.Context, tx *sql.Tx, guardKey string, now time.Time, key string) (mysqlRequestMeta, bool, error) {
	var stored string
	var expiresAt time.Time
	err := tx.QueryRowContext(ctx, mysqlReqMetaSQL(), guardKey, now).Scan(&stored, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return mysqlRequestMeta{}, false, nil
	}
	if err != nil {
		return mysqlRequestMeta{}, false, err
	}
	metadata := mysqlMetaFromDB(stored, key)
	metadata.expiresAt = expiresAt.UTC()
	return metadata, true, nil
}

func mysqlStatusDB(ctx context.Context, db *sql.DB, guardKey string, now time.Time) (Status, bool, error) {
	if db == nil {
		return "", false, fmt.Errorf("mysql idempotency store is nil")
	}
	var status string
	err := db.QueryRowContext(ctx, mysqlReqStatusSQL(), guardKey, now).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return Status(status), true, nil
}

func mysqlMetadataDB(ctx context.Context, db *sql.DB, guardKey string, now time.Time, key string) (mysqlRequestMeta, bool, error) {
	if db == nil {
		return mysqlRequestMeta{}, false, fmt.Errorf("mysql idempotency store is nil")
	}
	var stored string
	var expiresAt time.Time
	err := db.QueryRowContext(ctx, mysqlReqMetaSQL(), guardKey, now).Scan(&stored, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return mysqlRequestMeta{}, false, nil
	}
	if err != nil {
		return mysqlRequestMeta{}, false, err
	}
	metadata := mysqlMetaFromDB(stored, key)
	metadata.expiresAt = expiresAt.UTC()
	return metadata, true, nil
}

func mysqlResultDB(ctx context.Context, db *sql.DB, guardKey string, now time.Time) ([]byte, bool, error) {
	if db == nil {
		return nil, false, fmt.Errorf("mysql idempotency store is nil")
	}
	var payload []byte
	err := db.QueryRowContext(ctx, // #nosec G202 -- SQL 只拼接框架固定表名/列名，查询条件通过参数绑定。
		"SELECT "+mysqlQuoteIdentifier("result_data")+" FROM "+mysqlQuoteIdentifier(mysqlResultsTable)+" WHERE "+mysqlQuoteIdentifier("guard_key")+" = ? AND "+mysqlQuoteIdentifier("expires_at")+" > ? LIMIT 1", // #nosec G202 -- SQL 只拼接框架固定表名/列名，查询条件通过参数绑定。
		guardKey,
		now,
	).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return append([]byte(nil), payload...), true, nil
}

func mysqlReservationDB(ctx context.Context, db *sql.DB, guardKey string, now time.Time) (bool, error) {
	if db == nil {
		return false, fmt.Errorf("mysql idempotency store is nil")
	}
	var count int
	err := db.QueryRowContext(ctx, // #nosec G202 -- SQL 只拼接框架固定表名/列名，查询条件通过参数绑定。
		"SELECT COUNT(*) FROM "+mysqlQuoteIdentifier(mysqlResvTable)+" WHERE "+mysqlQuoteIdentifier("guard_key")+" = ? AND "+mysqlQuoteIdentifier("expires_at")+" > ?", // #nosec G202 -- SQL 只拼接框架固定表名/列名，查询条件通过参数绑定。
		guardKey,
		now,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func idemRecoverCtx(parent context.Context) (context.Context, context.CancelFunc) {
	base := context.Background()
	if parent != nil {
		base = context.WithoutCancel(parent)
	}
	return context.WithTimeout(base, idemRecoverTO)
}

func (s *mySQLStore) requestKey(key string) string {
	return digestKey(s.keyPrefixOrDefault() + "\x00key\x00" + key)
}

func (s *mySQLStore) sequenceKey(scope string) string {
	return digestKey(s.keyPrefixOrDefault() + "\x00seq\x00" + scope)
}

func (s *mySQLStore) nowOrDefault() time.Time {
	if s == nil || s.now == nil {
		return time.Now().UTC()
	}
	return s.now().UTC()
}

func (s *mySQLStore) ttlOrDefault() time.Duration {
	if s == nil || s.ttl <= 0 {
		return 10 * time.Minute
	}
	return s.ttl
}

func (s *mySQLStore) keyPrefixOrDefault() string {
	if s == nil {
		return "idempotency"
	}
	prefix := strings.TrimSpace(s.keyPrefix)
	if prefix == "" {
		return "idempotency"
	}
	return prefix
}
