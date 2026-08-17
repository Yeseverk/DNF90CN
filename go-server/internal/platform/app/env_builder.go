package app

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"longheng.io/server/internal/platform/admincmdqueue"
	"longheng.io/server/internal/platform/cluster"
	"longheng.io/server/internal/platform/config"
	"longheng.io/server/internal/platform/discovery"
	"longheng.io/server/internal/platform/reload"
	rpckit "longheng.io/server/internal/platform/rpc"
	"longheng.io/server/internal/platform/servergroup"
	"longheng.io/server/internal/platform/topology"
	"longheng.io/server/internal/platform/workerpool"
)

// New 创建只装配不运行的 Application，适合测试或上层进程自行托管生命周期。
func New(serviceName, configPath string, builder Builder) (*Application, error) {
	return NewWithManifest(serviceName, configPath, ServiceManifest{Components: builder})
}

// Run 创建并运行基于 Builder 的服务，直到传入 context 或系统信号触发停服。
func Run(ctx context.Context, serviceName, configPath string, builder Builder) error {
	return RunWithManifest(ctx, serviceName, configPath, ServiceManifest{Components: builder})
}

// RunWithManifest 创建并运行基于 ServiceManifest 的服务，统一复用平台基础设施装配。
func RunWithManifest(ctx context.Context, serviceName, configPath string, manifest ServiceManifest) error {
	if ctx == nil {
		ctx = context.Background()
	}
	application, err := NewWithManifest(serviceName, configPath, manifest)
	if err != nil {
		return err
	}
	return application.Run(ctx)
}

// NewWithManifest 创建 Application 并完成平台、runtime、admin 与业务组件的统一装配。
func NewWithManifest(serviceName, configPath string, manifest ServiceManifest) (*Application, error) {
	foundation, err := newPlatformBase(serviceName, configPath)
	if err != nil {
		return nil, err
	}
	cfg := foundation.cfg
	logger := foundation.logger
	otelShutdown := foundation.otelShutdown
	pvfArchive := foundation.pvfArchive
	dataTables := foundation.dataTables
	dataTableViews := foundation.dataTableViews
	stateSyncStore := foundation.stateSyncStore
	serverGroupManager := foundation.serverGroupManager
	serverGroupStore := foundation.serverGroupStore
	auditLogger := foundation.auditLogger
	logicLogger := foundation.logicLogger
	biLogger := foundation.biLogger
	healthSvc := foundation.healthSvc
	metricsRegistry := foundation.metricsRegistry
	tracer := foundation.tracer
	rateLimiter := foundation.rateLimiter
	adminTimeouts := foundation.adminTimeouts
	adminServer := foundation.adminServer
	schedulerSvc := foundation.schedulerSvc
	var application *Application
	configProvider := func() config.ServiceConfig {
		if application != nil {
			return application.configSnapshot()
		}
		return cfg
	}
	adminGuard := newAdminSecurity(configProvider, auditLogger, logger, rateLimiter)
	storageObjects, err := newStorageObjSvc(cfg, adminServer.Mux(), adminGuard.wrap, adminGuard.wrapDangerous)
	if err != nil {
		_ = foundation.close(context.Background())
		return nil, err
	}
	accountCenter, err := newAccountCenter(cfg, adminServer.Mux(), adminGuard.wrap, adminGuard.wrapDangerous)
	if err != nil {
		_ = storageObjects.Close()
		_ = foundation.close(context.Background())
		return nil, err
	}

	reg, err := newRegistry(cfg, logger)
	if err != nil {
		_ = foundation.close(context.Background())
		return nil, err
	}
	eventBus, err := newBus(cfg, logger)
	if err != nil {
		_ = reg.Close()
		_ = foundation.close(context.Background())
		return nil, err
	}
	attachBusMetrics(eventBus, metricsRegistry, cfg)
	cacheStore, err := newCache(cfg, metricsRegistry)
	if err != nil {
		_ = eventBus.Close()
		_ = reg.Close()
		_ = foundation.close(context.Background())
		return nil, err
	}
	lockManager, err := newLockManager(cfg, metricsRegistry)
	if err != nil {
		_ = cacheStore.Close()
		_ = eventBus.Close()
		_ = reg.Close()
		_ = foundation.close(context.Background())
		return nil, err
	}
	presenceRuntime, err := newPresenceRuntime(cfg)
	if err != nil {
		_ = lockManager.Close()
		_ = cacheStore.Close()
		_ = eventBus.Close()
		_ = reg.Close()
		_ = foundation.close(context.Background())
		return nil, err
	}
	regPresenceMetrics(metricsRegistry, presenceRuntime, cfg)
	onlinePush, err := newOnlinePushService(eventBus, presenceRuntime, adminServer.Mux(), adminGuard.wrap, adminGuard.wrapDangerous)
	if err != nil {
		if closer, ok := presenceRuntime.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
		_ = lockManager.Close()
		_ = cacheStore.Close()
		_ = eventBus.Close()
		_ = reg.Close()
		_ = foundation.close(context.Background())
		return nil, err
	}
	eventLog, eventLogWorker, err := newEventLog(cfg, eventBus, adminServer.Mux(), adminGuard.wrap, logger)
	if err != nil {
		if closer, ok := presenceRuntime.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
		_ = lockManager.Close()
		_ = cacheStore.Close()
		_ = eventBus.Close()
		_ = reg.Close()
		_ = foundation.close(context.Background())
		return nil, err
	}
	var adminCommands *admincmdqueue.Executor
	if eventLog != nil {
		adminCommands, err = admincmdqueue.NewExecutor(admincmdqueue.ExecutorOptions{Log: eventLog})
		if err != nil {
			_ = eventLog.Close()
			if closer, ok := presenceRuntime.(interface{ Close() error }); ok {
				_ = closer.Close()
			}
			_ = lockManager.Close()
			_ = cacheStore.Close()
			_ = eventBus.Close()
			_ = reg.Close()
			_ = foundation.close(context.Background())
			return nil, err
		}
	}
	hotUpdateManager, err := newHotUpdateManager(cfg, dataTables, dataTableViews, eventLog, logger)
	if err != nil {
		if eventLog != nil {
			_ = eventLog.Close()
		}
		if closer, ok := presenceRuntime.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
		_ = lockManager.Close()
		_ = cacheStore.Close()
		_ = eventBus.Close()
		_ = reg.Close()
		_ = foundation.close(context.Background())
		return nil, err
	}
	workers := workerpool.New("worker-pool", cfg.Worker.Size, cfg.Worker.Queue, logger)
	rpcEndpoint := rpckit.NewEndpoint(cfg.Service.Name, cfg.Service.NodeID, eventBus, logger)
	rpcEndpoint.SetOptionsExact(rpckit.Options{
		CallTimeout:     time.Duration(cfg.RPC.CallTimeoutSeconds) * time.Second,
		MaxPending:      cfg.RPC.MaxPending,
		MaxPayloadBytes: cfg.RPC.MaxPayloadBytes,
	})
	discoveryResolver := discovery.NewResolver(
		reg,
		time.Duration(cfg.Discovery.CacheTTLSeconds)*time.Second,
		time.Duration(cfg.Discovery.FailureTTLSeconds)*time.Second,
		!cfg.Discovery.AllowMaintaining,
	)
	discoveryResolver.SetFailureThreshold(cfg.Discovery.FailureThreshold)
	discoveryResolver.SetStrategy(discovery.Strategy(cfg.Discovery.Strategy))
	rpcEndpoint.SetDiscoveryResolver(discoveryResolver)
	rpcEndpoint.SetTracer(tracer)
	registerRPCMetrics(metricsRegistry, rpcEndpoint, cfg)
	topologyManager := topology.New(topologyConfig(cfg), reg, logger)
	clusterCoord := cluster.New(cfg, reg, eventBus, healthSvc, adminServer.Mux(), cfg.Admin.Token, logger, func(next http.HandlerFunc) http.HandlerFunc {
		return adminGuard.wrap("cluster", next)
	})
	application = &Application{
		cfg:                 cfg,
		configPath:          configPath,
		logger:              logger,
		bus:                 eventBus,
		cache:               cacheStore,
		lockManager:         lockManager,
		presence:            presenceRuntime,
		registry:            reg,
		cluster:             clusterCoord,
		health:              healthSvc,
		metrics:             metricsRegistry,
		rateLimiter:         rateLimiter,
		discovery:           discoveryResolver,
		rpc:                 rpcEndpoint,
		topology:            topologyManager,
		scheduler:           schedulerSvc,
		tracer:              tracer,
		pvfArchive:          pvfArchive,
		dataTables:          dataTables,
		dataTableViews:      dataTableViews,
		audit:               auditLogger,
		logicLog:            logicLogger,
		biLog:               biLogger,
		eventLog:            eventLog,
		adminCommands:       adminCommands,
		accountCenter:       accountCenter,
		onlinePush:          onlinePush,
		otelShutdown:        otelShutdown,
		workers:             workers,
		admin:               adminServer,
		hotUpdate:           hotUpdateManager,
		stateSync:           stateSyncStore,
		storageObjects:      storageObjects,
		serverGroup:         serverGroupManager,
		serverGroupStore:    serverGroupStore,
		serverGroupArchives: foundation.serverGroupArchives,
	}
	reloadManager := reload.New("config-reloader", reloadConfig(cfg), application.applyRuntimeReload, logger)
	application.reloader = reloadManager
	registerCommonRoutes(commonRouteContext{
		Mux:            adminServer.Mux(),
		ConfigProvider: application.configSnapshot,
		Health:         healthSvc,
		Metrics:        metricsRegistry,
		RateLimiter:    rateLimiter,
		Discovery:      discoveryResolver,
		RPC:            rpcEndpoint,
		Topology:       topologyManager,
		Scheduler:      schedulerSvc,
		Tracer:         tracer,
		PVF:            pvfArchive,
		DataTables:     dataTables,
		DataTableViews: dataTableViews,
		Audit:          auditLogger,
		LogicLog:       logicLogger,
		BILog:          biLogger,
		Reload:         reloadManager,
		Logger:         logger,
	})
	regUpdateStateRoutes(adminServer.Mux(), adminGuard.wrapDangerous, adminCommands, hotUpdateManager, stateSyncStore, time.Duration(cfg.StateSync.TTLSeconds)*time.Second)
	if serverGroupManager != nil {
		servergroup.RegisterAdminRoutes(adminServer.Mux(), serverGroupManager, servergroup.AdminOptions{
			Prefix:                 cfg.ServerGroup.AdminPrefix,
			Store:                  serverGroupStore,
			Archives:               foundation.serverGroupArchives,
			WarZoneInZoneDelaySecs: cfg.ServerGroup.InZoneDelaySeconds,
			WarZoneNoticeLeadSecs:  cfg.ServerGroup.NoticeLeadSeconds,
			Wrap:                   adminGuard.wrap,
			WrapDangerous:          adminGuard.wrapDangerous,
		})
	}

	env := &Env{
		Config:              cfg,
		ConfigPath:          configPath,
		Logger:              logger,
		Bus:                 eventBus,
		Cache:               cacheStore,
		Lock:                lockManager,
		Presence:            presenceRuntime,
		Cluster:             clusterCoord,
		Registry:            reg,
		Health:              healthSvc,
		Metrics:             metricsRegistry,
		RateLimiter:         rateLimiter,
		Discovery:           discoveryResolver,
		RPC:                 rpcEndpoint,
		Topology:            topologyManager,
		Scheduler:           schedulerSvc,
		Tracer:              tracer,
		PVF:                 pvfArchive,
		DataTables:          dataTables,
		DataTableViews:      dataTableViews,
		Audit:               auditLogger,
		LogicLog:            logicLogger,
		BILog:               biLogger,
		EventLog:            eventLog,
		AdminCommands:       adminCommands,
		AccountCenter:       accountCenter,
		OnlinePush:          onlinePush,
		Reload:              reloadManager,
		HotUpdate:           hotUpdateManager,
		StateSync:           stateSyncStore,
		StorageObjects:      storageObjects,
		ServerGroup:         serverGroupManager,
		ServerGroupStore:    serverGroupStore,
		ServerGroupArchives: foundation.serverGroupArchives,
		Workers:             workers,
		AdminMux:            adminServer.Mux(),
		AdminTimeouts:       adminTimeouts,
		AdminAuth:           adminGuard.wrap,
		AdminDangerous:      adminGuard.wrapDangerous,
		ConfigProvider:      application.configSnapshot,
	}

	components := []Component{workers, schedulerSvc, rpcEndpoint, reloadManager}
	if hotUpdateManager != nil {
		components = append(components, hotUpdateManager)
	}
	if stateSyncSweeper := newStateSyncSweeper(stateSyncStore, time.Duration(cfg.StateSync.CleanupIntervalSeconds)*time.Second, logger); stateSyncSweeper != nil {
		components = append(components, stateSyncSweeper)
	}
	if eventLogWorker != nil {
		components = append(components, eventLogWorker)
	}
	extra, err := manifest.Build(env)
	if err != nil {
		_ = cleanupComponents(context.Background(), healthSvc, extra)
		closeHotCtl(hotUpdateManager)
		if closer, ok := presenceRuntime.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
		_ = lockManager.Close()
		_ = cacheStore.Close()
		_ = eventBus.Close()
		_ = reg.Close()
		if eventLog != nil {
			_ = eventLog.Close()
		}
		_ = foundation.close(context.Background())
		return nil, err
	}
	application.i18nCatalog = env.I18N
	if cfg.DataTables.Enabled {
		if _, err := dataTableViews.RefreshAll(context.Background(), dataTables); err != nil {
			_ = cleanupComponents(context.Background(), healthSvc, extra)
			closeHotCtl(hotUpdateManager)
			if closer, ok := presenceRuntime.(interface{ Close() error }); ok {
				_ = closer.Close()
			}
			_ = lockManager.Close()
			_ = cacheStore.Close()
			_ = eventBus.Close()
			_ = reg.Close()
			if eventLog != nil {
				_ = eventLog.Close()
			}
			_ = foundation.close(context.Background())
			return nil, fmt.Errorf("refresh data table views: %w", err)
		}
	}
	components = append(components, extra...)
	components = append(components, adminServer)
	components = append(components, clusterCoord)
	components = append(components, topologyManager)

	application.components = components
	application.constructed = extra
	return application, nil
}
