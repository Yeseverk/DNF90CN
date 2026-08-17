package db

import (
	"context"
	"errors"
	"time"

	"github.com/gomodule/redigo/redis"
)

// ErrRedisExecutorClosed 表示 Redis 执行器已关闭或未初始化。
var ErrRedisExecutorClosed = errors.New("redis executor is closed")

// closeRedisConnErr 在 Redis 操作本身成功时保留连接关闭错误，避免池连接异常被静默吞掉。
func closeRedisConnErr(conn redis.Conn, err *error) {
	if conn == nil || err == nil {
		return
	}
	if closeErr := conn.Close(); closeErr != nil && *err == nil {
		*err = closeErr
	}
}

// RedigoOptions 配置 redigo 连接池。
type RedigoOptions struct {
	Address           string
	Password          string
	DB                int
	PoolSize          int
	MaxActive         int
	IdleTimeout       time.Duration
	Timeout           time.Duration
	ConnectTimeout    time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	TestOnBorrowAfter time.Duration
}

// RedisPoolStats 描述 Redis 连接池状态。
type RedisPoolStats struct {
	Address      string        `json:"address"`
	DB           int           `json:"db"`
	MaxIdle      int           `json:"max_idle"`
	MaxActive    int           `json:"max_active"`
	IdleTimeout  time.Duration `json:"idle_timeout"`
	ActiveCount  int           `json:"active_count"`
	IdleCount    int           `json:"idle_count"`
	WaitCount    int64         `json:"wait_count"`
	WaitDuration time.Duration `json:"wait_duration"`
}

// RedigoExecutor 用 redigo 连接池实现 RedisExecutor。
type RedigoExecutor struct {
	options RedigoOptions
	pool    *redis.Pool
}

// NormalizeRedigoOptions 补齐 redigo 默认连接参数。
func NormalizeRedigoOptions(options RedigoOptions) RedigoOptions {
	if options.Address == "" {
		options.Address = "127.0.0.1:6379"
	}
	if options.PoolSize <= 0 {
		options.PoolSize = 8
	}
	if options.MaxActive <= 0 {
		options.MaxActive = options.PoolSize * 2
	}
	if options.IdleTimeout <= 0 {
		options.IdleTimeout = time.Minute
	}
	if options.Timeout <= 0 {
		options.Timeout = 2 * time.Second
	}
	if options.ConnectTimeout <= 0 {
		options.ConnectTimeout = options.Timeout
	}
	if options.ReadTimeout <= 0 {
		options.ReadTimeout = options.Timeout
	}
	if options.WriteTimeout <= 0 {
		options.WriteTimeout = options.Timeout
	}
	if options.TestOnBorrowAfter < 0 {
		options.TestOnBorrowAfter = 0
	}
	if options.TestOnBorrowAfter == 0 {
		options.TestOnBorrowAfter = time.Minute
	}
	return options
}

// NewRedigoExecutor 创建 redigo Redis 执行器。
func NewRedigoExecutor(options RedigoOptions) *RedigoExecutor {
	options = NormalizeRedigoOptions(options)
	return &RedigoExecutor{options: options, pool: &redis.Pool{
		MaxIdle:     options.PoolSize,
		MaxActive:   options.MaxActive,
		IdleTimeout: options.IdleTimeout,
		Wait:        true,
		Dial: func() (redis.Conn, error) {
			dialOptions := []redis.DialOption{
				redis.DialConnectTimeout(options.ConnectTimeout),
				redis.DialReadTimeout(options.ReadTimeout),
				redis.DialWriteTimeout(options.WriteTimeout),
				redis.DialDatabase(options.DB),
			}
			if options.Password != "" {
				dialOptions = append(dialOptions, redis.DialPassword(options.Password))
			}
			return redis.Dial("tcp", options.Address, dialOptions...)
		},
		TestOnBorrow: func(conn redis.Conn, lastUsed time.Time) error {
			if time.Since(lastUsed) < options.TestOnBorrowAfter {
				return nil
			}
			_, err := conn.Do("PING")
			return err
		},
	}}
}

// Do 执行单条 Redis 命令。
func (e *RedigoExecutor) Do(ctx context.Context, command string, args ...any) (reply any, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if e == nil || e.pool == nil {
		return nil, ErrRedisExecutorClosed
	}
	conn, err := e.pool.GetContext(ctx)
	if err != nil {
		return nil, err
	}
	defer closeRedisConnErr(conn, &err)
	return conn.Do(command, args...)
}

// DoBatch 使用同一连接批量发送 Redis 命令。
func (e *RedigoExecutor) DoBatch(ctx context.Context, commands []RedisCommand) (err error) {
	if len(commands) == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if e == nil || e.pool == nil {
		return ErrRedisExecutorClosed
	}
	conn, err := e.pool.GetContext(ctx)
	if err != nil {
		return err
	}
	defer closeRedisConnErr(conn, &err)
	return RunRedigoBatch(ctx, conn, commands)
}

// RunRedigoBatch 在已获取的 redigo 连接上发送批量命令，并在 Flush 后完整接收响应。
func RunRedigoBatch(ctx context.Context, conn redis.Conn, commands []RedisCommand) error {
	if len(commands) == 0 {
		return nil
	}
	if conn == nil {
		return ErrRedisExecutorClosed
	}
	sent := 0
	for _, command := range commands {
		if err := ctx.Err(); err != nil {
			return err
		}
		if command.Name == "" {
			continue
		}
		if err := conn.Send(command.Name, command.Args...); err != nil {
			return err
		}
		sent++
	}
	if sent == 0 {
		return nil
	}
	if err := conn.Flush(); err != nil {
		return err
	}
	// Flush 成功后命令可能已经被 Redis 执行；此时必须排空响应，不能再让调用方 ctx
	// 中途打断 Receive，否则异步刷盘会把已提交写误判成失败并重复入队。
	for idx := 0; idx < sent; idx++ {
		if _, err := conn.Receive(); err != nil {
			return err
		}
	}
	return nil
}

// PoolStats 返回连接池统计信息。
func (e *RedigoExecutor) PoolStats() RedisPoolStats {
	if e == nil || e.pool == nil {
		return RedisPoolStats{}
	}
	stats := e.pool.Stats()
	return RedisPoolStats{
		Address:      e.options.Address,
		DB:           e.options.DB,
		MaxIdle:      e.options.PoolSize,
		MaxActive:    e.options.MaxActive,
		IdleTimeout:  e.options.IdleTimeout,
		ActiveCount:  stats.ActiveCount,
		IdleCount:    stats.IdleCount,
		WaitCount:    stats.WaitCount,
		WaitDuration: stats.WaitDuration,
	}
}

// Close 关闭 Redis 连接池。
func (e *RedigoExecutor) Close() error {
	if e == nil || e.pool == nil {
		return nil
	}
	return e.pool.Close()
}
