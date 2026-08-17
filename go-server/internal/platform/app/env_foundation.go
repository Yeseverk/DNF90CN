package app

import (
	"context"
	"fmt"
	"log/slog"

	"longheng.io/server/internal/platform/audit"
	"longheng.io/server/internal/platform/bilog"
	"longheng.io/server/internal/platform/config"
	"longheng.io/server/internal/platform/datatable"
	"longheng.io/server/internal/platform/health"
	"longheng.io/server/internal/platform/httpx"
	"longheng.io/server/internal/platform/logiclog"
	"longheng.io/server/internal/platform/logx"
	"longheng.io/server/internal/platform/metrics"
	"longheng.io/server/internal/platform/otelx"
	"longheng.io/server/internal/platform/pvf"
	"longheng.io/server/internal/platform/ratelimit"
	"longheng.io/server/internal/platform/scheduler"
	"longheng.io/server/internal/platform/servergroup"
	"longheng.io/server/internal/platform/statesync"
	"longheng.io/server/internal/platform/tracing"
)

type platformFoundation struct {
	cfg                 config.ServiceConfig
	logger              *slog.Logger
	otelShutdown        otelx.Shutdown
	pvfArchive          *pvf.Archive
	dataTables          *datatable.Registry
	dataTableViews      *datatable.ViewManager
	stateSyncStore      statesync.Store
	serverGroupManager  *servergroup.Manager
	serverGroupStore    servergroup.Store
	serverGroupArchives servergroup.MergeArchiveStore
	auditLogger         *audit.Logger
	logicLogger         *logiclog.Logger
	biLogger            *bilog.Logger
	healthSvc           *health.Service
	metricsRegistry     *metrics.Registry
	tracer              *tracing.Tracer
	rateLimiter         *ratelimit.Limiter
	adminTimeouts       httpx.Timeouts
	adminServer         *httpx.Server
	schedulerSvc        *scheduler.Scheduler
}

func newPlatformBase(serviceName, configPath string) (*platformFoundation, error) {
	cfg, err := config.Load(configPath, serviceName)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	pvfArchive, err := newPVFArchive(cfg)
	if err != nil {
		return nil, err
	}
	logger := logx.New(cfg.Service.Name, cfg.Service.NodeID, cfg.Service.Environment)
	otelShutdown, err := setupOTel(cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("setup opentelemetry: %w", err)
	}
	foundation := &platformFoundation{
		cfg:          cfg,
		logger:       logger,
		otelShutdown: otelShutdown,
		pvfArchive:   pvfArchive,
	}
	if foundation.dataTables, err = newDataTables(cfg); err != nil {
		_ = foundation.close(context.Background())
		return nil, err
	}
	if foundation.dataTableViews, err = datatable.NewViewManager(); err != nil {
		_ = foundation.close(context.Background())
		return nil, err
	}
	if foundation.stateSyncStore, err = newStateSyncStore(cfg); err != nil {
		_ = foundation.close(context.Background())
		return nil, err
	}
	foundation.serverGroupManager, foundation.serverGroupStore, foundation.serverGroupArchives, err = newGroupManager(context.Background(), cfg)
	if err != nil {
		_ = foundation.close(context.Background())
		return nil, err
	}
	if foundation.auditLogger, err = newAuditLogger(cfg); err != nil {
		_ = foundation.close(context.Background())
		return nil, err
	}
	if foundation.logicLogger, err = newLogicLogger(cfg); err != nil {
		_ = foundation.close(context.Background())
		return nil, err
	}
	if foundation.biLogger, err = newBILogger(cfg); err != nil {
		_ = foundation.close(context.Background())
		return nil, err
	}
	foundation.healthSvc = health.New(cfg.Service.Name)
	foundation.metricsRegistry = metrics.New()
	foundation.tracer = tracing.New(cfg.Tracing.Enabled || cfg.OTel.Enabled, cfg.Tracing.MaxSpans, logger)
	if foundation.rateLimiter, err = newRateLimiter(cfg); err != nil {
		_ = foundation.close(context.Background())
		return nil, err
	}
	foundation.adminTimeouts = httpx.Timeouts{
		ReadTimeout:       cfg.Admin.ReadTimeout(),
		ReadHeaderTimeout: cfg.Admin.ReadHeaderTimeout(),
		WriteTimeout:      cfg.Admin.WriteTimeout(),
		IdleTimeout:       cfg.Admin.IdleTimeout(),
	}
	foundation.adminServer = httpx.New("admin-http", cfg.Admin.Listen, logger, foundation.adminTimeouts)
	foundation.schedulerSvc = scheduler.New("scheduler", logger)
	return foundation, nil
}

func (f *platformFoundation) close(ctx context.Context) error {
	if f == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var err error
	if f.stateSyncStore != nil {
		err = f.stateSyncStore.Close()
		f.stateSyncStore = nil
	}
	if f.auditLogger != nil {
		if closeErr := f.auditLogger.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
		f.auditLogger = nil
	}
	if f.logicLogger != nil {
		if closeErr := f.logicLogger.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
		f.logicLogger = nil
	}
	if f.biLogger != nil {
		if closeErr := f.biLogger.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
		f.biLogger = nil
	}
	if closeErr := shutdownOTel(ctx, f.otelShutdown); closeErr != nil && err == nil {
		err = closeErr
	}
	f.otelShutdown = nil
	return err
}
