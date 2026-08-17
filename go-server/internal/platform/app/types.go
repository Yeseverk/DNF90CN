package app

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"longheng.io/server/internal/platform/accountcenter"
	"longheng.io/server/internal/platform/admincmdqueue"
	"longheng.io/server/internal/platform/audit"
	"longheng.io/server/internal/platform/bilog"
	"longheng.io/server/internal/platform/bus"
	cachekit "longheng.io/server/internal/platform/cache"
	"longheng.io/server/internal/platform/cluster"
	"longheng.io/server/internal/platform/config"
	"longheng.io/server/internal/platform/datatable"
	"longheng.io/server/internal/platform/discovery"
	"longheng.io/server/internal/platform/eventlog"
	"longheng.io/server/internal/platform/health"
	"longheng.io/server/internal/platform/hotupdate"
	"longheng.io/server/internal/platform/httpx"
	lockkit "longheng.io/server/internal/platform/lock"
	"longheng.io/server/internal/platform/logiclog"
	"longheng.io/server/internal/platform/metrics"
	"longheng.io/server/internal/platform/onlinepush"
	"longheng.io/server/internal/platform/otelx"
	"longheng.io/server/internal/platform/presence"
	"longheng.io/server/internal/platform/pvf"
	"longheng.io/server/internal/platform/ratelimit"
	"longheng.io/server/internal/platform/registry"
	"longheng.io/server/internal/platform/reload"
	rpckit "longheng.io/server/internal/platform/rpc"
	"longheng.io/server/internal/platform/scheduler"
	"longheng.io/server/internal/platform/servergroup"
	"longheng.io/server/internal/platform/statesync"
	"longheng.io/server/internal/platform/storageobject"
	"longheng.io/server/internal/platform/topology"
	"longheng.io/server/internal/platform/tracing"
	"longheng.io/server/internal/platform/workerpool"
)

// Component 是应用统一托管的生命周期组件。
type Component interface {
	Name() string
	Start(context.Context) error
	Stop(context.Context) error
}

// AfterStarter 是组件启动完成后的回调扩展点。
type AfterStarter interface {
	AfterStart(context.Context) error
}

// BeforeStopper 是组件停止前的回调扩展点。
type BeforeStopper interface {
	BeforeStop(context.Context) error
}

// PreflightChecker 是组件启动前的健康预检扩展点。
type PreflightChecker interface {
	Preflight(context.Context) error
}

// Builder 根据 Env 构造一组服务组件。
type Builder func(*Env) ([]Component, error)

// EnvConfigurer 在组件构造前补充或替换 Env 里的依赖。
type EnvConfigurer func(*Env) error

// I18NCatalog 是应用层使用的多语言文本目录接口。
type I18NCatalog interface {
	LoadDirWithConfig(context.Context, string, string, string) error
	HasLanguage(string) bool
	Configure(string, string)
	Clear()
	Translate(string, string, map[string]string) (string, bool)
}

// ServiceManifest 描述服务入口需要的环境配置器和组件构造器。
type ServiceManifest struct {
	Configure  []EnvConfigurer
	Components Builder
}

const defPreflight = 5 * time.Second

// Env 保存服务装配阶段共享的运行时依赖。
type Env struct {
	Config              config.ServiceConfig
	ConfigPath          string
	Logger              *slog.Logger
	Bus                 bus.Bus
	Cache               cachekit.Store
	Lock                lockkit.Manager
	Presence            presence.Runtime
	Cluster             *cluster.Coordinator
	Registry            registry.Registry
	Health              *health.Service
	Metrics             *metrics.Registry
	RateLimiter         *ratelimit.Limiter
	Discovery           *discovery.Resolver
	RPC                 *rpckit.Endpoint
	Topology            *topology.Manager
	Scheduler           *scheduler.Scheduler
	Tracer              *tracing.Tracer
	PVF                 *pvf.Archive
	DataTables          *datatable.Registry
	DataTableViews      *datatable.ViewManager
	I18N                I18NCatalog
	Audit               *audit.Logger
	LogicLog            *logiclog.Logger
	BILog               *bilog.Logger
	EventLog            *eventlog.Log
	AdminCommands       *admincmdqueue.Executor
	AccountCenter       *accountcenter.Center
	OnlinePush          *onlinepush.Service
	Reload              *reload.Manager
	HotUpdate           *hotupdate.Manager
	StateSync           statesync.Store
	StorageObjects      *storageobject.Service
	ServerGroup         *servergroup.Manager
	ServerGroupStore    servergroup.Store
	ServerGroupArchives servergroup.MergeArchiveStore
	Workers             *workerpool.Pool
	AdminMux            *http.ServeMux
	AdminTimeouts       httpx.Timeouts
	AdminAuth           func(string, http.HandlerFunc) http.HandlerFunc
	AdminDangerous      func(string, string, http.HandlerFunc) http.HandlerFunc
	ConfigProvider      func() config.ServiceConfig
	Components          []Component
}

// Application 管理服务配置、平台依赖和组件生命周期。
type Application struct {
	cfgMu               sync.RWMutex
	cfg                 config.ServiceConfig
	configPath          string
	logger              *slog.Logger
	bus                 bus.Bus
	cache               cachekit.Store
	lockManager         lockkit.Manager
	presence            presence.Runtime
	registry            registry.Registry
	cluster             *cluster.Coordinator
	health              *health.Service
	metrics             *metrics.Registry
	rateLimiter         *ratelimit.Limiter
	discovery           *discovery.Resolver
	rpc                 *rpckit.Endpoint
	topology            *topology.Manager
	scheduler           *scheduler.Scheduler
	tracer              *tracing.Tracer
	pvfArchive          *pvf.Archive
	dataTables          *datatable.Registry
	dataTableViews      *datatable.ViewManager
	i18nCatalog         I18NCatalog
	audit               *audit.Logger
	logicLog            *logiclog.Logger
	biLog               *bilog.Logger
	eventLog            *eventlog.Log
	adminCommands       *admincmdqueue.Executor
	accountCenter       *accountcenter.Center
	onlinePush          *onlinepush.Service
	reloader            *reload.Manager
	hotUpdate           *hotupdate.Manager
	stateSync           statesync.Store
	storageObjects      *storageobject.Service
	serverGroup         *servergroup.Manager
	serverGroupStore    servergroup.Store
	serverGroupArchives servergroup.MergeArchiveStore
	otelShutdown        otelx.Shutdown
	workers             *workerpool.Pool
	admin               *httpx.Server
	components          []Component
	constructed         []Component
	watchdogStop        func()
}
