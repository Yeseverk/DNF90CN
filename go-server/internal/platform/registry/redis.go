package registry

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gomodule/redigo/redis"
)

var redisRegNodeScript = redis.NewScript(2, `
local ttl = tonumber(ARGV[3])
if ttl ~= nil and ttl > 0 then
  redis.call("SETEX", KEYS[2], ttl, ARGV[2])
else
  redis.call("SET", KEYS[2], ARGV[2])
end
return redis.call("SADD", KEYS[1], ARGV[1])
`)

// closeRedisConnErr 在主操作成功时把 Redis 连接关闭错误回传给调用方。
func closeRedisConnErr(conn redis.Conn, err *error) {
	if conn == nil || err == nil {
		return
	}
	if closeErr := conn.Close(); closeErr != nil && *err == nil {
		*err = closeErr
	}
}

var redisDropNodeScript = redis.NewScript(2, `
if redis.call("GET", KEYS[2]) == false then
  return redis.call("SREM", KEYS[1], ARGV[1])
end
return 0
`)

type Redis struct {
	logger   *slog.Logger
	pool     *redis.Pool
	prefix   string
	leaseTTL int

	mu     sync.Mutex
	closed bool
}

func NewRedis(endpoints []string, namespace string, leaseTTL int64, password string, database int, logger *slog.Logger) (*Redis, error) {
	endpoint := firstEndpoint(endpoints)
	if endpoint == "" {
		return nil, fmt.Errorf("redis registry requires at least one endpoint")
	}
	if leaseTTL <= 0 {
		leaseTTL = 15
	}
	pool := &redis.Pool{
		MaxIdle:     8,
		MaxActive:   32,
		IdleTimeout: time.Minute,
		Wait:        true,
		Dial: func() (redis.Conn, error) {
			dialOptions := []redis.DialOption{
				redis.DialConnectTimeout(5 * time.Second),
				redis.DialReadTimeout(5 * time.Second),
				redis.DialWriteTimeout(5 * time.Second),
			}
			if password != "" {
				dialOptions = append(dialOptions, redis.DialPassword(password))
			}
			if database > 0 {
				dialOptions = append(dialOptions, redis.DialDatabase(database))
			}
			return redis.Dial(
				"tcp",
				endpoint,
				dialOptions...,
			)
		},
		TestOnBorrow: func(conn redis.Conn, lastUsed time.Time) error {
			if time.Since(lastUsed) < time.Minute {
				return nil
			}
			_, err := conn.Do("PING")
			return err
		},
	}
	return &Redis{
		logger:   logger,
		pool:     pool,
		prefix:   normalizeNamespace(namespace),
		leaseTTL: int(leaseTTL),
	}, nil
}

func (r *Redis) Register(ctx context.Context, node Node) (err error) {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	node.Service = strings.TrimSpace(node.Service)
	node.NodeID = strings.TrimSpace(node.NodeID)
	node.Tags = normalizeTags(node.Tags)
	if node.Service == "" || node.NodeID == "" {
		return fmt.Errorf("registry node service and node_id are required")
	}
	node.RegisteredAt = time.Now().UTC()
	data, err := json.Marshal(node)
	if err != nil {
		return err
	}

	conn, err := r.getConn(ctx)
	if err != nil {
		return err
	}
	defer closeRedisConnErr(conn, &err)
	_, err = redisRegNodeScript.Do(conn, r.serviceKey(node.Service), r.nodeKey(node.Service, node.NodeID), node.NodeID, data, r.leaseTTL)
	return err
}

func (r *Redis) Check(ctx context.Context) (err error) {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	conn, err := r.getConn(ctx)
	if err != nil {
		return err
	}
	defer closeRedisConnErr(conn, &err)
	_, err = redis.String(conn.Do("PING"))
	return err
}

func (r *Redis) Deregister(ctx context.Context, service, nodeID string) (err error) {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	service = strings.TrimSpace(service)
	nodeID = strings.TrimSpace(nodeID)
	if service == "" || nodeID == "" {
		return nil
	}
	conn, err := r.getConn(ctx)
	if err != nil {
		return err
	}
	defer closeRedisConnErr(conn, &err)
	if _, err := conn.Do("DEL", r.nodeKey(service, nodeID)); err != nil {
		return err
	}
	_, err = conn.Do("SREM", r.serviceKey(service), nodeID)
	return err
}

func (r *Redis) List(ctx context.Context, service string) (nodes []Node, err error) {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	service = strings.TrimSpace(service)
	if service == "" {
		return nil, nil
	}
	conn, err := r.getConn(ctx)
	if err != nil {
		return nil, err
	}
	defer closeRedisConnErr(conn, &err)

	nodeIDs, err := redis.Strings(conn.Do("SMEMBERS", r.serviceKey(service)))
	if errors.Is(err, redis.ErrNil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	nodes = make([]Node, 0, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		nodeID = strings.TrimSpace(nodeID)
		if nodeID == "" {
			continue
		}
		data, err := redis.Bytes(conn.Do("GET", r.nodeKey(service, nodeID)))
		if errors.Is(err, redis.ErrNil) {
			_, _ = redisDropNodeScript.Do(conn, r.serviceKey(service), r.nodeKey(service, nodeID), nodeID)
			continue
		}
		if err != nil {
			return nil, err
		}
		var node Node
		if err := json.Unmarshal(data, &node); err != nil {
			if r.logger != nil {
				r.logger.Warn("redis registry decode failed", "service", service, "node_id", nodeID, "error", err)
			}
			continue
		}
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].NodeID < nodes[j].NodeID
	})
	return nodes, nil
}

func (r *Redis) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	pool := r.pool
	r.pool = nil
	r.mu.Unlock()
	if pool == nil {
		return nil
	}
	return pool.Close()
}

func (r *Redis) getConn(ctx context.Context) (redis.Conn, error) {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, ErrClosed
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, ErrClosed
	}
	pool := r.pool
	r.mu.Unlock()
	if pool == nil {
		return nil, ErrClosed
	}
	return pool.GetContext(ctx)
}

func (r *Redis) serviceKey(service string) string {
	return r.prefix + ":registry:" + encodeRedisKeyPart(strings.TrimSpace(service)) + ":nodes"
}

func (r *Redis) nodeKey(service, nodeID string) string {
	return r.prefix + ":registry:" + encodeRedisKeyPart(strings.TrimSpace(service)) + ":" + encodeRedisKeyPart(strings.TrimSpace(nodeID))
}

func encodeRedisKeyPart(value string) string {
	if value == "" {
		return "_"
	}
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func firstEndpoint(endpoints []string) string {
	for _, endpoint := range endpoints {
		endpoint = strings.TrimSpace(endpoint)
		if endpoint != "" {
			return endpoint
		}
	}
	return ""
}
