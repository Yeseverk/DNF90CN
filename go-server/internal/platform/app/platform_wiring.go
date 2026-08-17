package app

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"longheng.io/server/internal/platform/accountcenter"
	"longheng.io/server/internal/platform/admincmdqueue"
	"longheng.io/server/internal/platform/audit"
	"longheng.io/server/internal/platform/bilog"
	"longheng.io/server/internal/platform/bus"
	cachekit "longheng.io/server/internal/platform/cache"
	"longheng.io/server/internal/platform/config"
	"longheng.io/server/internal/platform/datatable"
	"longheng.io/server/internal/platform/eventlog"
	"longheng.io/server/internal/platform/httpx"
	lockkit "longheng.io/server/internal/platform/lock"
	"longheng.io/server/internal/platform/logiclog"
	"longheng.io/server/internal/platform/metrics"
	"longheng.io/server/internal/platform/onlinepush"
	"longheng.io/server/internal/platform/otelx"
	"longheng.io/server/internal/platform/presence"
	"longheng.io/server/internal/platform/ratelimit"
	"longheng.io/server/internal/platform/registry"
	"longheng.io/server/internal/platform/storageobject"
	"longheng.io/server/internal/platform/topology"

	_ "github.com/go-sql-driver/mysql"
)

func topologyConfig(cfg config.ServiceConfig) topology.Config {
	return topology.Config{
		Enabled:               cfg.Topology.Enabled,
		Services:              cfg.Topology.Services,
		GatewayService:        cfg.Topology.GatewayService,
		GatewayBackendService: cfg.Topology.GatewayBackendService,
		RejectMaintaining:     cfg.Topology.RejectMaintaining,
		RefreshInterval:       time.Duration(cfg.Topology.RefreshIntervalSeconds) * time.Second,
	}
}

func newStorageObjSvc(cfg config.ServiceConfig, mux *http.ServeMux, wrapAdmin func(string, http.HandlerFunc) http.HandlerFunc, wrapDangerous func(string, string, http.HandlerFunc) http.HandlerFunc) (*storageobject.Service, error) {
	if !cfg.StorageObject.Enabled {
		return nil, nil
	}
	var store storageobject.Store
	switch cfg.StorageObject.StoreKind {
	case "", "memory":
		store = storageobject.NewMemoryStore(nil)
	case "mysql":
		db, err := sql.Open("mysql", cfg.StorageObject.MySQLDSN)
		if err != nil {
			return nil, fmt.Errorf("open storage object mysql store: %w", err)
		}
		sqlStore, err := storageobject.NewSQLStore(storageobject.SQLOptions{
			DB:    db,
			Table: cfg.StorageObject.MySQLTable,
		})
		if err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("create storage object mysql store: %w", err)
		}
		if cfg.StorageObject.MySQLEnsureSchema {
			if err := sqlStore.EnsureSchema(context.Background()); err != nil {
				_ = sqlStore.Close()
				return nil, fmt.Errorf("ensure storage object mysql schema: %w", err)
			}
		}
		store = sqlStore
	default:
		return nil, fmt.Errorf("unsupported storage_object.store_kind %q", cfg.StorageObject.StoreKind)
	}
	service := storageobject.NewService(store)
	if mux != nil {
		wrap := func(next http.HandlerFunc) http.HandlerFunc {
			if wrapAdmin != nil {
				return wrapAdmin("storageobject", next)
			}
			return next
		}
		dangerous := func(operation string, next http.HandlerFunc) http.HandlerFunc {
			if wrapDangerous != nil {
				return wrapDangerous("storageobject", operation, next)
			}
			return wrap(next)
		}
		if err := storageobject.RegisterAdminRoutes(mux, service, storageobject.AdminOptions{Prefix: cfg.StorageObject.AdminPrefix, Wrap: wrap, WrapDangerous: dangerous}); err != nil {
			_ = service.Close()
			return nil, err
		}
	}
	return service, nil
}

func newAccountCenter(cfg config.ServiceConfig, mux *http.ServeMux, wrapAdmin func(string, http.HandlerFunc) http.HandlerFunc, wrapDangerous func(string, string, http.HandlerFunc) http.HandlerFunc) (*accountcenter.Center, error) {
	if !cfg.AccountCenter.Enabled {
		return nil, nil
	}
	var store accountcenter.Store
	switch cfg.AccountCenter.StoreKind {
	case "", "memory":
	case "mysql":
		db, err := sql.Open("mysql", cfg.AccountCenter.MySQLDSN)
		if err != nil {
			return nil, fmt.Errorf("open account center mysql store: %w", err)
		}
		db.SetMaxOpenConns(16)
		db.SetMaxIdleConns(4)
		db.SetConnMaxLifetime(300 * time.Second)
		sqlStore, err := accountcenter.NewSQLStore(accountcenter.SQLOptions{
			DB:    db,
			Table: cfg.AccountCenter.MySQLTable,
		})
		if err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("create account center mysql store: %w", err)
		}
		if cfg.AccountCenter.MySQLEnsureSchema {
			if err := sqlStore.EnsureSchema(context.Background()); err != nil {
				_ = sqlStore.Close()
				return nil, fmt.Errorf("ensure account center mysql schema: %w", err)
			}
		}
		store = sqlStore
	default:
		return nil, fmt.Errorf("unsupported account_center.store_kind %q", cfg.AccountCenter.StoreKind)
	}
	center, err := accountcenter.NewCenter(context.Background(), accountcenter.CenterOptions{Store: store})
	if err != nil {
		if closer, ok := store.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
		return nil, err
	}
	if mux != nil {
		wrap := func(next http.HandlerFunc) http.HandlerFunc {
			if wrapAdmin != nil {
				return wrapAdmin("accountcenter", next)
			}
			return next
		}
		dangerous := func(operation string, next http.HandlerFunc) http.HandlerFunc {
			if wrapDangerous != nil {
				return wrapDangerous("accountcenter", operation, next)
			}
			return wrap(next)
		}
		if err := accountcenter.RegisterAdminRoutes(mux, center, accountcenter.AdminOptions{Prefix: cfg.AccountCenter.AdminPrefix, Wrap: wrap, WrapDangerous: dangerous}); err != nil {
			_ = center.Close()
			return nil, err
		}
	}
	return center, nil
}

func newOnlinePushService(eventBus bus.Bus, presenceRuntime presence.Runtime, mux *http.ServeMux, wrapAdmin func(string, http.HandlerFunc) http.HandlerFunc, wrapDangerous func(string, string, http.HandlerFunc) http.HandlerFunc) (*onlinepush.Service, error) {
	service := onlinepush.New(onlinepush.Options{Bus: eventBus, Presence: presenceRuntime})
	if mux == nil {
		return service, nil
	}
	wrap := func(next http.HandlerFunc) http.HandlerFunc {
		if wrapAdmin != nil {
			return wrapAdmin("onlinepush", next)
		}
		return next
	}
	dangerous := func(operation string, next http.HandlerFunc) http.HandlerFunc {
		if wrapDangerous != nil {
			return wrapDangerous("onlinepush", operation, next)
		}
		return wrap(next)
	}
	if err := onlinepush.RegisterAdminRoutes(mux, service, onlinepush.AdminOptions{Prefix: "/onlinepush", Wrap: wrap, WrapDangerous: dangerous}); err != nil {
		return nil, err
	}
	return service, nil
}

func newRateLimiter(cfg config.ServiceConfig) (*ratelimit.Limiter, error) {
	limitConfig, err := rateLimitConfig(cfg)
	if err != nil {
		return nil, err
	}
	return ratelimit.New(limitConfig), nil
}

func rateLimitConfig(cfg config.ServiceConfig) (ratelimit.Config, error) {
	rules, err := ratelimit.ParseRules(cfg.RateLimit.Rules)
	if err != nil {
		return ratelimit.Config{}, fmt.Errorf("parse rate_limit.rules: %w", err)
	}
	return ratelimit.Config{
		Enabled:       cfg.RateLimit.Enabled,
		Algorithm:     cfg.RateLimit.Algorithm,
		Window:        time.Duration(cfg.RateLimit.WindowSeconds) * time.Second,
		MaxRequests:   cfg.RateLimit.MaxRequests,
		Rules:         rules,
		CleanupEvery:  time.Duration(cfg.RateLimit.CleanupIntervalSeconds) * time.Second,
		TrustedHeader: cfg.RateLimit.TrustedHeader,
	}, nil
}

func setupOTel(cfg config.ServiceConfig, logger *slog.Logger) (otelx.Shutdown, error) {
	return otelx.Setup(context.Background(), cfg.OTel, cfg.Service.Name, cfg.Service.NodeID, cfg.Service.Environment, logger)
}

func newDataTables(cfg config.ServiceConfig) (*datatable.Registry, error) {
	registry := datatable.NewRegistry(cfg.DataTables.Directory, cfg.DataTables.Version)
	if !cfg.DataTables.Enabled {
		return registry, nil
	}
	if err := registry.Load(context.Background()); err != nil {
		return nil, fmt.Errorf("load data tables: %w", err)
	}
	return registry, nil
}

func newAuditLogger(cfg config.ServiceConfig) (*audit.Logger, error) {
	if !cfg.Audit.Enabled {
		return audit.NewLogger(cfg.Service.Name, cfg.Service.NodeID, nil), nil
	}
	var sink audit.Sink
	switch cfg.Audit.Kind {
	case "memory":
		sink = audit.NewMemory(cfg.Audit.MemoryLimit)
	case "file":
		fileSink, err := audit.NewFile(cfg.Audit.FilePath, cfg.Audit.MemoryLimit)
		if err != nil {
			return nil, err
		}
		sink = fileSink
	default:
		return nil, fmt.Errorf("unsupported audit.kind %q", cfg.Audit.Kind)
	}
	return audit.NewLogger(cfg.Service.Name, cfg.Service.NodeID, sink), nil
}

func newLogicLogger(cfg config.ServiceConfig) (*logiclog.Logger, error) {
	if !cfg.LogicLog.Enabled {
		return logiclog.NewLogger(nil, nil), nil
	}
	catalog, err := logiclog.LoadCatalogCSV(cfg.LogicLog.ReasonsPath)
	if err != nil {
		return nil, fmt.Errorf("load logic_log.reasons_path: %w", err)
	}
	var sink logiclog.Sink
	switch cfg.LogicLog.Kind {
	case "memory":
		sink = logiclog.NewBoundedMemorySink(cfg.LogicLog.MemoryLimit)
	case "file":
		fileSink, err := logiclog.NewFileSink(cfg.LogicLog.FilePath, cfg.LogicLog.MemoryLimit)
		if err != nil {
			return nil, err
		}
		sink = fileSink
	default:
		return nil, fmt.Errorf("unsupported logic_log.kind %q", cfg.LogicLog.Kind)
	}
	return logiclog.NewLogger(catalog, sink), nil
}

func newBILogger(cfg config.ServiceConfig) (*bilog.Logger, error) {
	if !cfg.BILog.Enabled {
		return bilog.NewLogger(nil, nil), nil
	}
	schema, err := bilog.LoadSchema(cfg.BILog.SchemaPath)
	if err != nil {
		return nil, fmt.Errorf("load bi_log.schema_path: %w", err)
	}
	var sink bilog.Sink
	switch cfg.BILog.Kind {
	case "memory":
		sink = bilog.NewBoundedMemorySink(cfg.BILog.MemoryLimit)
	case "file":
		fileSink, err := bilog.NewFileSink(cfg.BILog.FilePath, cfg.BILog.MemoryLimit)
		if err != nil {
			return nil, err
		}
		sink = fileSink
	default:
		return nil, fmt.Errorf("unsupported bi_log.kind %q", cfg.BILog.Kind)
	}
	return bilog.NewLogger(schema, sink), nil
}

func newEventLog(cfg config.ServiceConfig, eventBus bus.Bus, mux *http.ServeMux, wrapAdmin func(string, http.HandlerFunc) http.HandlerFunc, logger *slog.Logger) (*eventlog.Log, Component, error) {
	if !cfg.EventLog.Enabled {
		return nil, nil, nil
	}
	var store eventlog.Store
	switch cfg.EventLog.StoreKind {
	case "", "memory":
		store = eventlog.NewMemoryStore(cfg.Service.Name + "-outbox")
	case "mysql":
		db, err := sql.Open("mysql", cfg.EventLog.MySQLDSN)
		if err != nil {
			return nil, nil, fmt.Errorf("open eventlog mysql store: %w", err)
		}
		db.SetMaxOpenConns(cfg.EventLog.MySQLMaxOpenConns)
		db.SetMaxIdleConns(cfg.EventLog.MySQLMaxIdleConns)
		db.SetConnMaxLifetime(time.Duration(cfg.EventLog.MySQLConnMaxLifetime) * time.Second)
		sqlStore, err := eventlog.NewSQLStoreFromDB(db, eventlog.SQLStoreOptions{
			Name:  cfg.Service.Name + "-outbox",
			Table: cfg.EventLog.MySQLTable,
		})
		if err != nil {
			_ = db.Close()
			return nil, nil, fmt.Errorf("create eventlog mysql store: %w", err)
		}
		if cfg.EventLog.MySQLEnsureSchema {
			if err := sqlStore.EnsureSchema(context.Background()); err != nil {
				_ = sqlStore.Close()
				return nil, nil, fmt.Errorf("ensure eventlog mysql schema: %w", err)
			}
		}
		store = sqlStore
	default:
		return nil, nil, fmt.Errorf("unsupported eventlog.store_kind %q", cfg.EventLog.StoreKind)
	}
	log := eventlog.New(eventlog.Options{
		Name:        cfg.Service.Name + "-outbox",
		Store:       store,
		IDGenerator: newEventIDGen(cfg),
	})
	if mux != nil {
		wrap := func(next http.HandlerFunc) http.HandlerFunc {
			if wrapAdmin != nil {
				return wrapAdmin("eventlog", next)
			}
			return httpx.WrapAdmin(cfg.Admin.Token, logger, "eventlog", next)
		}
		if err := eventlog.RegisterAdminRoutes(mux, log, eventlog.AdminOptions{Prefix: cfg.EventLog.AdminPrefix, Wrap: wrap}); err != nil {
			_ = log.Close()
			return nil, nil, err
		}
	}
	if !cfg.EventLog.PublishEnabled {
		return log, nil, nil
	}
	publisher, err := newEventLogPublisher(cfg, eventBus)
	if err != nil {
		_ = log.Close()
		return nil, nil, err
	}
	worker, err := eventlog.NewPublishWorker(eventlog.PublishWorkerOptions{
		Name:           cfg.Service.Name + "-eventlog-publisher",
		Log:            log,
		Publisher:      publisher,
		Interval:       time.Duration(cfg.EventLog.PublishIntervalSeconds) * time.Second,
		Limit:          cfg.EventLog.PublishBatchSize,
		RetryDelay:     time.Duration(cfg.EventLog.PublishRetrySeconds) * time.Second,
		MaxAttempts:    cfg.EventLog.PublishMaxAttempts,
		ExcludeStreams: []string{admincmdqueue.DefaultStream},
		Logger:         logger,
	})
	if err != nil {
		_ = log.Close()
		return nil, nil, err
	}
	return log, worker, nil
}

func newEventLogPublisher(cfg config.ServiceConfig, eventBus bus.Bus) (eventlog.Publisher, error) {
	switch cfg.EventLog.PublishKind {
	case "", "bus":
		return newEventBusPub(eventBus)
	case "http":
		return newEventLogPub(cfg)
	case "bus_http":
		busPublisher, err := newEventBusPub(eventBus)
		if err != nil {
			return nil, err
		}
		httpPublisher, err := newEventLogPub(cfg)
		if err != nil {
			return nil, err
		}
		return eventlog.NewFanoutPublisher(busPublisher, httpPublisher)
	default:
		return nil, fmt.Errorf("unsupported eventlog.publish_kind %q", cfg.EventLog.PublishKind)
	}
}

func newEventBusPub(eventBus bus.Bus) (*eventlog.BusPublisher, error) {
	return eventlog.NewBusPublisher(eventBus, eventlog.BusPublisherOptions{
		Topic:   eventlog.OriginalTopic,
		Payload: eventlog.OriginalTopicPayload,
	})
}

func newEventLogPub(cfg config.ServiceConfig) (*eventlog.HTTPPublisher, error) {
	body := eventlog.EventJSONBody
	if cfg.EventLog.PublishHTTPPayload == "payload" {
		body = eventlog.PayloadJSONBody
	}
	return eventlog.NewHTTPPublisher(eventlog.HTTPPublisherOptions{
		URL:     cfg.EventLog.PublishHTTPURL,
		Method:  cfg.EventLog.PublishHTTPMethod,
		Client:  &http.Client{Timeout: time.Duration(cfg.EventLog.PublishHTTPTimeout) * time.Second},
		Headers: cfg.EventLog.PublishHTTPHeaders,
		Body:    body,
	})
}

func newEventIDGen(cfg config.ServiceConfig) eventlog.IDGenerator {
	prefix := eventLogIDPrefix(cfg)
	var seq uint64
	return func() string {
		n := atomic.AddUint64(&seq, 1)
		return fmt.Sprintf("%s-%d-%d", prefix, time.Now().UTC().UnixNano(), n)
	}
}

func eventLogIDPrefix(cfg config.ServiceConfig) string {
	service := cleanEventIDPart(cfg.Service.Name)
	node := cleanEventIDPart(cfg.Service.NodeID)
	if service == "" {
		service = "service"
	}
	if node == "" {
		node = "node"
	}
	prefix := service + "-" + node
	if len(prefix) > 96 {
		return prefix[:96]
	}
	return prefix
}

func cleanEventIDPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_'
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func newRegistry(cfg config.ServiceConfig, logger *slog.Logger) (registry.Registry, error) {
	switch cfg.Registry.Kind {
	case "", "memory":
		return registry.NewMemory(), nil
	case "redis":
		return registry.NewRedis(cfg.Registry.Endpoints, cfg.Registry.Namespace, cfg.Registry.LeaseTTL, cfg.Registry.RedisPassword, cfg.Registry.RedisDB, logger)
	case "etcd":
		return registry.NewETCD(cfg.Registry.Endpoints, cfg.Registry.Namespace, cfg.Registry.LeaseTTL, logger)
	default:
		return nil, fmt.Errorf("unsupported registry.kind %q", cfg.Registry.Kind)
	}
}

func newBus(cfg config.ServiceConfig, logger *slog.Logger) (bus.Bus, error) {
	switch cfg.Bus.Kind {
	case "", "memory":
		return bus.NewMemory(logger), nil
	case "redis":
		return bus.NewRedisWithOptions(cfg.Bus.Endpoints, cfg.Bus.Namespace, logger, bus.RedisOptions{
			Username:      cfg.Bus.RedisUsername,
			Password:      cfg.Bus.RedisPassword,
			Database:      cfg.Bus.RedisDB,
			TLSEnabled:    cfg.Bus.RedisTLSEnabled,
			TLSServerName: cfg.Bus.RedisTLSServerName,
		})
	case "nats":
		return bus.NewNATSWithOptions(cfg.Bus.Endpoints, cfg.Bus.Namespace, logger, bus.NATSOptions{
			Name:                 cfg.Bus.NATSName,
			Timeout:              time.Duration(cfg.Bus.NATSTimeoutSeconds) * time.Second,
			PingInterval:         time.Duration(cfg.Bus.NATSPingIntervalSeconds) * time.Second,
			MaxPingsOutstanding:  cfg.Bus.NATSMaxPingsOutstanding,
			MaxReconnects:        cfg.Bus.NATSMaxReconnects,
			ReconnectWait:        time.Duration(cfg.Bus.NATSReconnectWaitSeconds) * time.Second,
			RetryOnFailedConnect: true,
			WireEncoding:         cfg.Bus.NATSWireEncoding,
			Token:                cfg.Bus.NATSToken,
			Username:             cfg.Bus.NATSUsername,
			Password:             cfg.Bus.NATSPassword,
			Credentials:          cfg.Bus.NATSCredentials,
			TLSEnabled:           cfg.Bus.NATSTLSEnabled,
			TLSServerName:        cfg.Bus.NATSTLSServerName,
			CAFile:               cfg.Bus.NATSCAFile,
			CertFile:             cfg.Bus.NATSCertFile,
			KeyFile:              cfg.Bus.NATSKeyFile,
		})
	default:
		return nil, fmt.Errorf("unsupported bus.kind %q", cfg.Bus.Kind)
	}
}

// busMetricsAttacher 由所有 bus adapter 实现，让平台 wiring 可以挂接 Metrics 发射器，
// 而不需要绑定具体类型。
type busMetricsAttacher interface {
	SetMetrics(*bus.Metrics)
}

func attachBusMetrics(eventBus bus.Bus, reg *metrics.Registry, cfg config.ServiceConfig) {
	if reg == nil || eventBus == nil {
		return
	}
	kind := cfg.Bus.Kind
	if kind == "" {
		kind = "memory"
	}
	attacher, ok := eventBus.(busMetricsAttacher)
	if !ok {
		return
	}
	attacher.SetMetrics(bus.NewMetrics(reg, kind))
}

func newCache(cfg config.ServiceConfig, reg *metrics.Registry) (cachekit.Store, error) {
	ttl := time.Duration(cfg.Cache.DefaultTTLSeconds) * time.Second
	kind := cfg.Cache.Kind
	if kind == "" {
		kind = "memory"
	}
	var store cachekit.Store
	switch cfg.Cache.Kind {
	case "", "memory":
		store = cachekit.NewMemory(cachekit.MemoryOptions{
			Name:       cfg.Service.Name + "-cache",
			DefaultTTL: ttl,
			MaxEntries: cfg.Cache.MaxEntries,
		})
	case "redis":
		store = cachekit.NewRedis(cachekit.RedisOptions{
			Name:           cfg.Service.Name + "-cache",
			KeyPrefix:      cfg.Cache.KeyPrefix,
			Address:        cfg.Cache.RedisAddress,
			Password:       cfg.Cache.RedisPassword,
			DB:             cfg.Cache.RedisDB,
			PoolSize:       cfg.Cache.RedisPoolSize,
			Timeout:        time.Duration(cfg.Cache.RedisTimeoutSeconds) * time.Second,
			ConnectTimeout: time.Duration(cfg.Cache.RedisConnectTimeout) * time.Second,
			ReadTimeout:    time.Duration(cfg.Cache.RedisReadTimeout) * time.Second,
			WriteTimeout:   time.Duration(cfg.Cache.RedisWriteTimeout) * time.Second,
			DefaultTTL:     ttl,
		})
	default:
		return nil, fmt.Errorf("unsupported cache.kind %q", cfg.Cache.Kind)
	}
	if attacher, ok := store.(interface{ SetMetrics(*cachekit.Metrics) }); ok {
		attacher.SetMetrics(cachekit.NewMetrics(reg, kind))
	}
	return store, nil
}

func newLockManager(cfg config.ServiceConfig, reg *metrics.Registry) (lockkit.Manager, error) {
	ttl := time.Duration(cfg.Lock.TTLSeconds) * time.Second
	kind := cfg.Lock.Kind
	if kind == "" {
		kind = "memory"
	}
	var manager lockkit.Manager
	switch cfg.Lock.Kind {
	case "", "memory":
		manager = lockkit.NewMemory(lockkit.MemoryOptions{Name: cfg.Service.Name + "-lock", DefaultTTL: ttl})
	case "redis":
		manager = lockkit.NewRedis(lockkit.RedisOptions{
			Name:           cfg.Service.Name + "-lock",
			KeyPrefix:      cfg.Lock.KeyPrefix,
			Address:        cfg.Lock.RedisAddress,
			Password:       cfg.Lock.RedisPassword,
			DB:             cfg.Lock.RedisDB,
			PoolSize:       cfg.Lock.RedisPoolSize,
			Timeout:        time.Duration(cfg.Lock.RedisTimeoutSeconds) * time.Second,
			ConnectTimeout: time.Duration(cfg.Lock.RedisConnectTimeout) * time.Second,
			ReadTimeout:    time.Duration(cfg.Lock.RedisReadTimeout) * time.Second,
			WriteTimeout:   time.Duration(cfg.Lock.RedisWriteTimeout) * time.Second,
			DefaultTTL:     ttl,
		})
	default:
		return nil, fmt.Errorf("unsupported lock.kind %q", cfg.Lock.Kind)
	}
	if attacher, ok := manager.(interface{ SetMetrics(*lockkit.Metrics) }); ok {
		attacher.SetMetrics(lockkit.NewMetrics(reg, kind))
	}
	return manager, nil
}
