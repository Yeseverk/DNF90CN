package logic

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"longheng.io/server/internal/platform/config"
	"longheng.io/server/pkg/contracts"
)

type mysqlRespCache struct {
	db           *sql.DB
	initErr      error
	ensureSchema bool
	schemaMu     sync.Mutex
	schemaReady  bool
	schemaErr    error
	keyPrefix    string
	ttl          time.Duration
	now          func() time.Time
}

func newMySQLRespCache(cfg config.IdempotencySection, ttl time.Duration) *mysqlRespCache {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	dsn := strings.TrimSpace(cfg.MySQLDSN)
	if dsn == "" {
		dsn = "longheng:longheng@tcp(127.0.0.1:3306)/longheng?parseTime=true&charset=utf8mb4,utf8"
	}
	conn, err := sql.Open("mysql", dsn)
	if err == nil {
		maxOpen := cfg.MySQLMaxOpenConns
		if maxOpen <= 0 {
			maxOpen = 32
		}
		maxIdle := cfg.MySQLMaxIdleConns
		if maxIdle <= 0 {
			maxIdle = 8
		}
		lifetime := time.Duration(cfg.MySQLConnMaxLifetimeSec) * time.Second
		if lifetime <= 0 {
			lifetime = 5 * time.Minute
		}
		conn.SetMaxOpenConns(maxOpen)
		conn.SetMaxIdleConns(maxIdle)
		conn.SetConnMaxLifetime(lifetime)
	}
	return &mysqlRespCache{
		db:           conn,
		initErr:      err,
		ensureSchema: true,
		keyPrefix:    respCachePrefix(cfg.KeyPrefix),
		ttl:          ttl,
		now:          time.Now,
	}
}

// Store 将响应缓存写入 MySQL 权威存储。
func (c *mysqlRespCache) Store(ctx context.Context, key string, response contracts.LogicPlayerResponse) error {
	if ctx == nil {
		ctx = context.Background()
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	if err := c.ready(ctx); err != nil {
		return err
	}
	now := c.nowUTC()
	data, err := json.Marshal(clonePlayerResp(response))
	if err != nil {
		return err
	}
	_, err = c.db.ExecContext(ctx, //nolint:gosec // G202：SQL 只拼接框架固定表名/列名，缓存内容通过参数绑定。
		"INSERT INTO "+quoteMySQLIdent(logicRespCacheTable)+" ("+ //nolint:gosec // G202：SQL 只拼接框架固定表名/列名，缓存内容通过参数绑定。
			strings.Join([]string{
				quoteMySQLIdent("cache_key"),
				quoteMySQLIdent("original_key"),
				quoteMySQLIdent("response_json"),
				quoteMySQLIdent("expires_at"),
				quoteMySQLIdent("updated_at"),
			}, ", ")+") VALUES (?, ?, ?, ?, ?) ON DUPLICATE KEY UPDATE "+
			quoteMySQLIdent("original_key")+" = VALUES("+quoteMySQLIdent("original_key")+"), "+
			quoteMySQLIdent("response_json")+" = VALUES("+quoteMySQLIdent("response_json")+"), "+
			quoteMySQLIdent("expires_at")+" = VALUES("+quoteMySQLIdent("expires_at")+"), "+
			quoteMySQLIdent("updated_at")+" = VALUES("+quoteMySQLIdent("updated_at")+")",
		c.cacheKey(key),
		limitRespCacheText(key, 512),
		string(data),
		now.Add(c.ttl),
		now,
	)
	return err
}

// Get 从 MySQL 读取未过期的响应缓存。
func (c *mysqlRespCache) Get(ctx context.Context, key string) (contracts.LogicPlayerResponse, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return contracts.LogicPlayerResponse{}, false, nil
	}
	if err := c.ready(ctx); err != nil {
		return contracts.LogicPlayerResponse{}, false, err
	}
	now := c.nowUTC()
	var data []byte
	err := c.db.QueryRowContext(ctx,
		"SELECT "+quoteMySQLIdent("response_json")+" FROM "+quoteMySQLIdent(logicRespCacheTable)+" WHERE "+quoteMySQLIdent("cache_key")+" = ? AND "+quoteMySQLIdent("expires_at")+" > ? LIMIT 1",
		c.cacheKey(key),
		now,
	).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		_, _ = c.db.ExecContext(ctx, //nolint:gosec // G202：SQL 只拼接框架固定表名/列名，清理条件通过参数绑定。
			"DELETE FROM "+quoteMySQLIdent(logicRespCacheTable)+" WHERE "+quoteMySQLIdent("cache_key")+" = ? AND "+quoteMySQLIdent("expires_at")+" <= ?", //nolint:gosec // G202：SQL 只拼接框架固定表名/列名，清理条件通过参数绑定。
			c.cacheKey(key),
			now,
		)
		return contracts.LogicPlayerResponse{}, false, nil
	}
	if err != nil {
		return contracts.LogicPlayerResponse{}, false, err
	}
	var response contracts.LogicPlayerResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return contracts.LogicPlayerResponse{}, false, err
	}
	return clonePlayerResp(response), true, nil
}

// Delete 从 MySQL 删除指定响应缓存。
func (c *mysqlRespCache) Delete(ctx context.Context, key string) error {
	if c == nil || c.db == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	_, err := c.db.ExecContext(ctx, //nolint:gosec // G202：SQL 只拼接框架固定表名/列名，缓存键通过参数绑定。
		"DELETE FROM "+quoteMySQLIdent(logicRespCacheTable)+" WHERE "+quoteMySQLIdent("cache_key")+" = ?", //nolint:gosec // G202：SQL 只拼接框架固定表名/列名，缓存键通过参数绑定。
		c.cacheKey(key),
	)
	return err
}

// Snapshot 返回 MySQL 响应缓存的配置状态。
func (c *mysqlRespCache) Snapshot() map[string]any {
	out := map[string]any{
		"backend": "mysql",
	}
	if c == nil {
		return out
	}
	out["ttl_seconds"] = int64(c.ttl / time.Second)
	out["key_prefix"] = c.keyPrefix
	return out
}

// Close 关闭 MySQL 响应缓存连接。
func (c *mysqlRespCache) Close(context.Context) error {
	if c == nil || c.db == nil {
		return nil
	}
	return c.db.Close()
}

func (c *mysqlRespCache) ready(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if c == nil || c.db == nil {
		return errors.New("logic mysql response cache is nil")
	}
	if c.initErr != nil {
		return c.initErr
	}
	if c.ensureSchema {
		c.schemaMu.Lock()
		defer c.schemaMu.Unlock()
		if c.schemaReady {
			return nil
		}
		_, c.schemaErr = c.db.ExecContext(ctx, responseCacheDDL())
		if c.schemaErr == nil {
			c.schemaReady = true
		}
	}
	return c.schemaErr
}

func (c *mysqlRespCache) nowUTC() time.Time {
	if c == nil || c.now == nil {
		return time.Now().UTC()
	}
	return c.now().UTC()
}

func (c *mysqlRespCache) cacheKey(key string) string {
	return digestRespCacheKey(c.keyPrefix + "\x00" + key)
}

func responseCacheDDL() string {
	columns := []string{
		quoteMySQLIdent("cache_key") + " VARCHAR(128) NOT NULL",
		quoteMySQLIdent("original_key") + " VARCHAR(512) NOT NULL",
		quoteMySQLIdent("response_json") + " JSON NOT NULL",
		quoteMySQLIdent("expires_at") + " DATETIME(6) NOT NULL",
		quoteMySQLIdent("updated_at") + " DATETIME(6) NOT NULL",
		"PRIMARY KEY (" + quoteMySQLIdent("cache_key") + ")",
		"KEY " + quoteMySQLIdent("idx_logic_idempotency_responses_expires_at") + " (" + quoteMySQLIdent("expires_at") + ")",
	}
	return fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS %s (\n  %s\n) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci",
		quoteMySQLIdent(logicRespCacheTable),
		strings.Join(columns, ",\n  "),
	)
}

func quoteMySQLIdent(identifier string) string {
	return "`" + strings.ReplaceAll(identifier, "`", "``") + "`"
}

func limitRespCacheText(value string, limit int) string {
	original := value
	value = strings.ToValidUTF8(value, "")
	if limit <= 0 || len(value) <= limit {
		return value
	}
	sum := sha256.Sum256([]byte(original))
	suffix := ":" + hex.EncodeToString(sum[:8])
	if limit <= len(suffix) {
		return suffix[:limit]
	}
	return truncRespCacheText(value, limit-len(suffix)) + suffix
}

func truncRespCacheText(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		if limit <= 0 {
			return ""
		}
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
