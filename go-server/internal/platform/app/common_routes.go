package app

import (
	"log/slog"
	"net/http"
	"net/http/pprof"

	"longheng.io/server/internal/platform/adapters"
	"longheng.io/server/internal/platform/adminspec"
	"longheng.io/server/internal/platform/audit"
	"longheng.io/server/internal/platform/bilog"
	"longheng.io/server/internal/platform/config"
	"longheng.io/server/internal/platform/datatable"
	"longheng.io/server/internal/platform/discovery"
	"longheng.io/server/internal/platform/framework"
	"longheng.io/server/internal/platform/health"
	"longheng.io/server/internal/platform/httpx"
	"longheng.io/server/internal/platform/lifecycle"
	"longheng.io/server/internal/platform/logiclog"
	"longheng.io/server/internal/platform/logx"
	"longheng.io/server/internal/platform/metrics"
	"longheng.io/server/internal/platform/pvf"
	"longheng.io/server/internal/platform/ratelimit"
	"longheng.io/server/internal/platform/readiness"
	"longheng.io/server/internal/platform/reload"
	rpckit "longheng.io/server/internal/platform/rpc"
	"longheng.io/server/internal/platform/scheduler"
	"longheng.io/server/internal/platform/sessioncontract"
	"longheng.io/server/internal/platform/topology"
	"longheng.io/server/internal/platform/tracing"
)

// commonRouteContext 集中承载平台公共管理路由依赖，避免装配层继续堆叠长参数列表。
type commonRouteContext struct {
	Mux            *http.ServeMux
	ConfigProvider func() config.ServiceConfig
	Health         *health.Service
	Metrics        *metrics.Registry
	RateLimiter    *ratelimit.Limiter
	Discovery      *discovery.Resolver
	RPC            *rpckit.Endpoint
	Topology       *topology.Manager
	Scheduler      *scheduler.Scheduler
	Tracer         *tracing.Tracer
	PVF            *pvf.Archive
	DataTables     *datatable.Registry
	DataTableViews *datatable.ViewManager
	Audit          *audit.Logger
	LogicLog       *logiclog.Logger
	BILog          *bilog.Logger
	Reload         *reload.Manager
	Logger         *slog.Logger
}

func registerCommonRoutes(routes commonRouteContext) {
	mux := routes.Mux
	configProvider := routes.ConfigProvider
	healthSvc := routes.Health
	metricsRegistry := routes.Metrics
	rateLimiter := routes.RateLimiter
	discoveryResolver := routes.Discovery
	rpcEndpoint := routes.RPC
	topologyManager := routes.Topology
	schedulerSvc := routes.Scheduler
	tracer := routes.Tracer
	pvfArchive := routes.PVF
	dataTables := routes.DataTables
	dataTableViews := routes.DataTableViews
	auditLogger := routes.Audit
	logicLogger := routes.LogicLog
	biLogger := routes.BILog
	reloadManager := routes.Reload
	logger := routes.Logger

	adminGuard := newAdminSecurity(configProvider, auditLogger, logger, rateLimiter)
	wrapAdmin := adminGuard.wrap
	wrapDangerousAdmin := adminGuard.wrapDangerous
	mux.HandleFunc("/healthz/live", healthSvc.LiveHandler)
	mux.HandleFunc("/healthz/ready", healthSvc.ReadyHandler)
	mux.HandleFunc("/healthz", healthSvc.DebugHandler)
	mux.HandleFunc("/admin/openapi.json", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", http.MethodGet)
			httpx.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		httpx.WriteJSON(w, http.StatusOK, adminspec.Default())
	})
	mux.HandleFunc("/admin", adminConsole)
	mux.HandleFunc("/admin/", adminConsole)
	mux.HandleFunc("/debug/config", wrapAdmin("platform", func(w http.ResponseWriter, _ *http.Request) {
		cfg := configProvider()
		httpx.WriteJSON(w, http.StatusOK, cfg.Redacted())
	}))
	mux.HandleFunc("/debug/metrics", wrapAdmin("platform", func(w http.ResponseWriter, _ *http.Request) {
		metrics.ObserveRuntime(metricsRegistry)
		metricsRegistry.RunObservers()
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"metrics": metricsRegistry.Snapshot(),
		})
	}))
	mux.HandleFunc("/debug/rate_limit", wrapAdmin("platform", func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, rateLimiter.Snapshot())
	}))
	mux.HandleFunc("/debug/discovery", wrapAdmin("platform", func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, discoveryResolver.Snapshot())
	}))
	mux.HandleFunc("/debug/rpc", wrapAdmin("platform", func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, rpcEndpoint.Snapshot())
	}))
	mux.HandleFunc("/debug/topology", wrapAdmin("platform", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("refresh") == "true" {
			if err := topologyManager.Refresh(r.Context()); err != nil {
				httpx.WriteJSON(w, http.StatusServiceUnavailable, topologyManager.Snapshot())
				return
			}
		}
		httpx.WriteJSON(w, http.StatusOK, topologyManager.Snapshot())
	}))
	mux.HandleFunc("/debug/framework", wrapAdmin("platform", func(w http.ResponseWriter, _ *http.Request) {
		contract := framework.DefaultRealtimeContract()
		status := "ok"
		errText := ""
		if err := framework.Validate(contract); err != nil {
			status = "invalid"
			errText = err.Error()
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"status":   status,
			"error":    errText,
			"contract": contract,
		})
	}))
	mux.HandleFunc("/debug/adapters", wrapAdmin("platform", func(w http.ResponseWriter, _ *http.Request) {
		matrix := adapters.MatrixFromConfig(configProvider())
		status := "ok"
		errText := ""
		if err := adapters.ValidateSelected(matrix); err != nil {
			status = "degraded"
			errText = err.Error()
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"status": status,
			"error":  errText,
			"matrix": matrix,
		})
	}))
	mux.HandleFunc("/debug/session-contract", wrapAdmin("platform", func(w http.ResponseWriter, _ *http.Request) {
		contract := sessioncontract.Default()
		status := "ok"
		errText := ""
		if err := sessioncontract.Validate(contract); err != nil {
			status = "invalid"
			errText = err.Error()
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"status":   status,
			"error":    errText,
			"contract": contract,
		})
	}))
	mux.HandleFunc("/debug/lifecycle", wrapAdmin("platform", func(w http.ResponseWriter, _ *http.Request) {
		contract := lifecycle.DefaultContract()
		status := "ok"
		errText := ""
		if err := lifecycle.Validate(contract); err != nil {
			status = "invalid"
			errText = err.Error()
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"status":   status,
			"error":    errText,
			"contract": contract,
		})
	}))
	mux.HandleFunc("/debug/readiness", wrapAdmin("platform", func(w http.ResponseWriter, _ *http.Request) {
		plan := readiness.DefaultPlan()
		status := "ok"
		errText := ""
		if err := readiness.Validate(plan); err != nil {
			status = "invalid"
			errText = err.Error()
		}
		if len(plan.Gaps) > 0 && status == "ok" {
			status = "gaps"
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"status": status,
			"error":  errText,
			"plan":   plan,
		})
	}))
	mux.HandleFunc("/debug/reload", wrapDangerousAdmin("platform", httpx.AdminOperationID(http.MethodPost, "/debug/reload"), func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			httpx.WriteJSON(w, http.StatusOK, reloadManager.Snapshot())
		case http.MethodPost:
			result, err := reloadManager.Reload(r.Context(), reload.TriggerManual, r.URL.Query().Get("reason"))
			if err != nil {
				httpx.WriteJSON(w, http.StatusServiceUnavailable, result)
				return
			}
			httpx.WriteJSON(w, http.StatusOK, result)
		default:
			httpx.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
	}))
	mux.HandleFunc("/debug/scheduler", wrapAdmin("platform", func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"jobs": schedulerSvc.Snapshot()})
	}))
	mux.HandleFunc("/debug/traces", wrapAdmin("platform", func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"enabled": tracer.Enabled(),
			"spans":   tracer.Snapshot(),
		})
	}))
	mux.HandleFunc("/debug/pvf", wrapAdmin("platform", func(w http.ResponseWriter, _ *http.Request) {
		cfg := configProvider()
		payload := map[string]any{
			"enabled": cfg.PVF.Enabled,
			"loaded":  pvfArchive != nil,
		}
		if pvfArchive != nil {
			payload["snapshot"] = pvfArchive.Snapshot()
		}
		httpx.WriteJSON(w, http.StatusOK, payload)
	}))
	mux.HandleFunc("/debug/datatables", wrapAdmin("platform", func(w http.ResponseWriter, _ *http.Request) {
		cfg := configProvider()
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"enabled":  cfg.DataTables.Enabled,
			"root":     dataTables.Root(),
			"version":  dataTables.Version(),
			"manifest": dataTables.Manifest(),
			"tables":   dataTables.Snapshot(),
		})
	}))
	mux.HandleFunc("/debug/datatable_views", wrapAdmin("platform", func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, dataTableViews.Snapshot())
	}))
	mux.HandleFunc("/debug/audit", wrapAdmin("platform", func(w http.ResponseWriter, r *http.Request) {
		cfg := configProvider()
		httpx.WriteJSON(w, http.StatusOK, auditDebugPayload(cfg, auditLogger, r.URL.Query()))
	}))
	mux.HandleFunc("/debug/logiclog", wrapAdmin("platform", func(w http.ResponseWriter, _ *http.Request) {
		cfg := configProvider()
		events := []logiclog.Event(nil)
		if logicLogger != nil {
			events = logicLogger.Events()
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"enabled": cfg.LogicLog.Enabled,
			"kind":    cfg.LogicLog.Kind,
			"events":  events,
		})
	}))
	mux.HandleFunc("/debug/bilog", wrapAdmin("platform", func(w http.ResponseWriter, _ *http.Request) {
		cfg := configProvider()
		events := []bilog.Event(nil)
		if biLogger != nil {
			events = biLogger.Events()
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"enabled": cfg.BILog.Enabled,
			"kind":    cfg.BILog.Kind,
			"events":  events,
		})
	}))
	mux.HandleFunc("/debug/log_level", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			wrapAdmin("platform", func(w http.ResponseWriter, _ *http.Request) {
				httpx.WriteJSON(w, http.StatusOK, map[string]any{
					"level": logx.DefaultLevel().String(),
				})
			})(w, r)
		case http.MethodPost:
			wrapDangerousAdmin("platform", httpx.AdminOperationID(http.MethodPost, "/debug/log_level"), func(w http.ResponseWriter, r *http.Request) {
				level := r.URL.Query().Get("level")
				parsed, err := logx.SetDefaultLevel(level)
				if err != nil {
					httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
					return
				}
				if logger != nil {
					logger.Warn("log level changed by admin", "level", parsed.String())
				}
				httpx.WriteJSON(w, http.StatusOK, map[string]any{
					"level": parsed.String(),
				})
			})(w, r)
		default:
			w.Header().Set("Allow", "GET, POST")
			httpx.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
	})
	cfg := configProvider()
	if cfg.Metrics.PrometheusEnabled {
		handler := metrics.Handler(metricsRegistry)
		mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
			cfg := configProvider()
			if cfg.Metrics.RequireAdminToken {
				wrapAdmin("platform", handler)(w, r)
				return
			}
			rateLimiter.Wrap(handler).ServeHTTP(w, r)
		})
	}
	if cfg.Debug.PprofEnabled {
		mux.HandleFunc("/debug/pprof/", wrapAdmin("platform-pprof", pprof.Index))
		mux.HandleFunc("/debug/pprof/cmdline", wrapAdmin("platform-pprof", pprof.Cmdline))
		mux.HandleFunc("/debug/pprof/profile", wrapAdmin("platform-pprof", pprof.Profile))
		mux.HandleFunc("/debug/pprof/symbol", wrapAdmin("platform-pprof", pprof.Symbol))
		mux.HandleFunc("/debug/pprof/trace", wrapAdmin("platform-pprof", pprof.Trace))
	}
}
