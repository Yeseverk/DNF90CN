package app

import (
	"log/slog"
	"net/http"

	"longheng.io/server/internal/platform/admincmdqueue"
	"longheng.io/server/internal/platform/audit"
	"longheng.io/server/internal/platform/bilog"
	"longheng.io/server/internal/platform/bus"
	cachekit "longheng.io/server/internal/platform/cache"
	"longheng.io/server/internal/platform/config"
	"longheng.io/server/internal/platform/discovery"
	"longheng.io/server/internal/platform/eventlog"
	"longheng.io/server/internal/platform/health"
	"longheng.io/server/internal/platform/httpx"
	lockkit "longheng.io/server/internal/platform/lock"
	"longheng.io/server/internal/platform/logiclog"
	"longheng.io/server/internal/platform/metrics"
	"longheng.io/server/internal/platform/onlinepush"
	"longheng.io/server/internal/platform/presence"
	"longheng.io/server/internal/platform/ratelimit"
	"longheng.io/server/internal/platform/registry"
	rpckit "longheng.io/server/internal/platform/rpc"
	"longheng.io/server/internal/platform/servergroup"
	"longheng.io/server/internal/platform/statesync"
	"longheng.io/server/internal/platform/storageobject"
	"longheng.io/server/internal/platform/topology"
	"longheng.io/server/internal/platform/workerpool"
)

// CoreDeps 是服务最小启动上下文。新服务优先依赖 CoreEnv，避免直接消费完整 Env。
type CoreDeps struct {
	Config      config.ServiceConfig
	Logger      *slog.Logger
	Health      *health.Service
	Metrics     *metrics.Registry
	RateLimiter *ratelimit.Limiter
}

// RealtimeDeps 聚合实时通信能力。网关、房间、在线推送等服务按需消费。
type RealtimeDeps struct {
	Bus        bus.Bus
	Presence   presence.Runtime
	OnlinePush *onlinepush.Service
	Discovery  *discovery.Resolver
	RPC        *rpckit.Endpoint
	Topology   *topology.Manager
	Registry   registry.Registry
	Workers    *workerpool.Pool
}

// PersistenceDeps 聚合缓存、锁、可靠事件和通用状态能力。关键写路径应显式依赖这里的 EventLog。
type PersistenceDeps struct {
	Cache          cachekit.Store
	Lock           lockkit.Manager
	EventLog       *eventlog.Log
	StateSync      statesync.Store
	StorageObjects *storageobject.Service
	ServerGroup    *servergroup.Manager
}

// AdminDeps 聚合运营后台能力。危险操作入口应通过这里拿鉴权、审计和 receipt 队列。
type AdminDeps struct {
	Mux      *http.ServeMux
	Timeouts httpx.Timeouts
	Audit    *audit.Logger
	LogicLog *logiclog.Logger
	BILog    *bilog.Logger
	Commands *admincmdqueue.Executor

	Auth      func(string, http.HandlerFunc) http.HandlerFunc
	Dangerous func(string, string, http.HandlerFunc) http.HandlerFunc
}

// CoreEnv 暴露服务最小启动依赖，供模块收窄对完整 Env 的依赖。
type CoreEnv interface {
	Core() CoreDeps
}

// RealtimeEnv 暴露实时通信依赖，供网关、房间和在线模块使用。
type RealtimeEnv interface {
	Realtime() RealtimeDeps
}

// PersistenceEnv 暴露持久化和可靠事件依赖，供关键写路径使用。
type PersistenceEnv interface {
	Persistence() PersistenceDeps
}

// AdminEnv 暴露运营后台依赖，供管理路由和危险操作使用。
type AdminEnv interface {
	AdminRuntime() AdminDeps
}

// Core 返回服务最小启动依赖。
func (e *Env) Core() CoreDeps {
	if e == nil {
		return CoreDeps{}
	}
	return CoreDeps{
		Config:      e.Config,
		Logger:      e.Logger,
		Health:      e.Health,
		Metrics:     e.Metrics,
		RateLimiter: e.RateLimiter,
	}
}

// Realtime 返回实时通信依赖集合。
func (e *Env) Realtime() RealtimeDeps {
	if e == nil {
		return RealtimeDeps{}
	}
	return RealtimeDeps{
		Bus:        e.Bus,
		Presence:   e.Presence,
		OnlinePush: e.OnlinePush,
		Discovery:  e.Discovery,
		RPC:        e.RPC,
		Topology:   e.Topology,
		Registry:   e.Registry,
		Workers:    e.Workers,
	}
}

// Persistence 返回持久化、锁和可靠事件依赖集合。
func (e *Env) Persistence() PersistenceDeps {
	if e == nil {
		return PersistenceDeps{}
	}
	return PersistenceDeps{
		Cache:          e.Cache,
		Lock:           e.Lock,
		EventLog:       e.EventLog,
		StateSync:      e.StateSync,
		StorageObjects: e.StorageObjects,
		ServerGroup:    e.ServerGroup,
	}
}

// AdminRuntime 返回运营后台依赖集合。
func (e *Env) AdminRuntime() AdminDeps {
	if e == nil {
		return AdminDeps{}
	}
	return AdminDeps{
		Mux:       e.AdminMux,
		Timeouts:  e.AdminTimeouts,
		Audit:     e.Audit,
		LogicLog:  e.LogicLog,
		BILog:     e.BILog,
		Commands:  e.AdminCommands,
		Auth:      e.AdminAuth,
		Dangerous: e.AdminDangerous,
	}
}
