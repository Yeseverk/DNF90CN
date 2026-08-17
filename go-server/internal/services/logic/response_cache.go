package logic

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"longheng.io/server/internal/platform/config"
	"longheng.io/server/pkg/contracts"
	"longheng.io/server/pkg/protocol"
)

const logicRespCacheTable = "logic_idempotency_responses"

type logicResponseCache interface {
	Store(context.Context, string, contracts.LogicPlayerResponse) error
	Get(context.Context, string) (contracts.LogicPlayerResponse, bool, error)
	Delete(context.Context, string) error
	Snapshot() map[string]any
	Close(context.Context) error
}

func newLogicRespCache(cfg config.IdempotencySection) logicResponseCache {
	ttl := time.Duration(cfg.TTLSeconds) * time.Second
	switch strings.ToLower(strings.TrimSpace(cfg.Kind)) {
	case "redis":
		return newRedisRespCache(cfg, ttl)
	case "mysql":
		return newMySQLRespCache(cfg, ttl)
	case "mysql_redis":
		return &hybridRespCache{
			redis: newRedisRespCache(cfg, ttl),
			mysql: newMySQLRespCache(cfg, ttl),
		}
	default:
		return newMemRespCache(ttl)
	}
}

func clonePlayerResp(response contracts.LogicPlayerResponse) contracts.LogicPlayerResponse {
	response.Body = append([]byte(nil), response.Body...)
	response.Metadata = protocol.CloneFrameMetadata(response.Metadata)
	return response
}

func respCachePrefix(prefix string) string {
	prefix = strings.Trim(strings.TrimSpace(prefix), ":")
	if prefix == "" {
		prefix = "idempotency"
	}
	return prefix + ":responses"
}

func digestRespCacheKey(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
