package app

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"longheng.io/server/internal/platform/config"
	"longheng.io/server/internal/platform/datatable"
	"longheng.io/server/internal/platform/discovery"
	"longheng.io/server/internal/platform/reload"
	rpckit "longheng.io/server/internal/platform/rpc"
)

func (a *Application) applyRuntimeReload(ctx context.Context, trigger reload.Trigger, reason string) (reload.Result, error) {
	result := reload.Result{
		Trigger: string(trigger),
		Reason:  reason,
		At:      time.Now().UTC(),
	}
	if a == nil {
		return result, errors.New("application is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	oldCfg := a.configSnapshot()
	newCfg, err := config.Load(a.configPath, oldCfg.Service.Name)
	if err != nil {
		result.Error = err.Error()
		return result, err
	}
	result.Changed = !reflect.DeepEqual(oldCfg, newCfg)
	if trigger == reload.TriggerWatch && !result.Changed {
		result.Skipped = append(result.Skipped, "config unchanged")
		return result, nil
	}

	effective := oldCfg
	var applyErr error
	apply := func(name string, changed bool, fn func() error) {
		if applyErr != nil || (!changed && trigger != reload.TriggerManual) {
			return
		}
		if err := fn(); err != nil {
			applyErr = fmt.Errorf("reload %s: %w", name, err)
			result.Error = applyErr.Error()
			return
		}
		result.Applied = append(result.Applied, name)
	}

	apply("rate_limit", !reflect.DeepEqual(oldCfg.RateLimit, newCfg.RateLimit), func() error {
		cfg, err := rateLimitConfig(newCfg)
		if err != nil {
			return err
		}
		a.rateLimiter.Configure(cfg)
		effective.RateLimit = newCfg.RateLimit
		return nil
	})
	apply("discovery", !reflect.DeepEqual(oldCfg.Discovery, newCfg.Discovery), func() error {
		a.discovery.Configure(
			time.Duration(newCfg.Discovery.CacheTTLSeconds)*time.Second,
			time.Duration(newCfg.Discovery.FailureTTLSeconds)*time.Second,
			newCfg.Discovery.FailureThreshold,
			!newCfg.Discovery.AllowMaintaining,
			discovery.Strategy(newCfg.Discovery.Strategy),
		)
		effective.Discovery = newCfg.Discovery
		return nil
	})
	apply("rpc", !reflect.DeepEqual(oldCfg.RPC, newCfg.RPC), func() error {
		a.rpc.SetOptionsExact(rpckit.Options{
			CallTimeout:     time.Duration(newCfg.RPC.CallTimeoutSeconds) * time.Second,
			MaxPending:      newCfg.RPC.MaxPending,
			MaxPayloadBytes: newCfg.RPC.MaxPayloadBytes,
		})
		effective.RPC = newCfg.RPC
		return nil
	})
	apply("topology", !reflect.DeepEqual(oldCfg.Topology, newCfg.Topology), func() error {
		a.topology.Configure(topologyConfig(newCfg))
		if newCfg.Topology.Enabled {
			if err := a.topology.Refresh(ctx); err != nil {
				return err
			}
		}
		effective.Topology = newCfg.Topology
		return nil
	})
	apply("tracing", !reflect.DeepEqual(oldCfg.Tracing, newCfg.Tracing), func() error {
		a.tracer.Configure(newCfg.Tracing.Enabled || oldCfg.OTel.Enabled, newCfg.Tracing.MaxSpans)
		effective.Tracing = newCfg.Tracing
		return nil
	})
	apply("data_tables", !reflect.DeepEqual(oldCfg.DataTables, newCfg.DataTables), func() error {
		if newCfg.DataTables.Enabled {
			options := datatable.ReloadOptions{}
			if a.dataTableViews != nil {
				options.Validators = append(options.Validators, a.dataTableViews)
			}
			loadResult, err := a.dataTables.ReloadDir(ctx, newCfg.DataTables.Directory, newCfg.DataTables.Version, options)
			if err != nil {
				return err
			}
			effective.DataTables = newCfg.DataTables
			if a.dataTableViews != nil {
				viewResult, err := a.dataTableViews.Refresh(context.WithoutCancel(ctx), a.dataTables, loadResult)
				if err != nil {
					return err
				}
				addReloadDetail(&result, "data_table_views", viewResult)
			}
			addReloadDetail(&result, "data_tables", loadResult)
		} else {
			a.dataTables.Configure(newCfg.DataTables.Directory, newCfg.DataTables.Version)
			a.dataTables.Clear()
			if a.dataTableViews != nil {
				addReloadDetail(&result, "data_table_views", a.dataTableViews.Clear())
			}
			addReloadDetail(&result, "data_tables", map[string]any{
				"enabled":  false,
				"manifest": a.dataTables.Manifest(),
			})
		}
		effective.DataTables = newCfg.DataTables
		return nil
	})
	apply("i18n", !reflect.DeepEqual(oldCfg.I18N, newCfg.I18N), func() error {
		if a.i18nCatalog == nil {
			effective.I18N = newCfg.I18N
			return nil
		}
		if newCfg.I18N.Enabled {
			if err := a.i18nCatalog.LoadDirWithConfig(ctx, newCfg.I18N.Directory, newCfg.I18N.DefaultLanguage, newCfg.I18N.Version); err != nil {
				return err
			}
			if !a.i18nCatalog.HasLanguage(newCfg.I18N.DefaultLanguage) {
				return fmt.Errorf("default language %q is not loaded", newCfg.I18N.DefaultLanguage)
			}
			a.i18nCatalog.Configure(newCfg.I18N.DefaultLanguage, newCfg.I18N.Version)
		} else {
			a.i18nCatalog.Configure(newCfg.I18N.DefaultLanguage, newCfg.I18N.Version)
			a.i18nCatalog.Clear()
		}
		effective.I18N = newCfg.I18N
		return nil
	})
	apply("reload", !reflect.DeepEqual(oldCfg.Reload, newCfg.Reload), func() error {
		if a.reloader != nil {
			a.reloader.Configure(reloadConfig(newCfg))
		}
		effective.Reload = newCfg.Reload
		return nil
	})
	apply("admin_security", adminSecurityChanged(oldCfg.Admin, newCfg.Admin), func() error {
		effective.Admin.Token = newCfg.Admin.Token
		effective.Admin.Tokens = cloneAdminTokens(newCfg.Admin.Tokens)
		effective.Admin.RBACEnabled = newCfg.Admin.RBACEnabled
		effective.Admin.DangerousConfirm = newCfg.Admin.DangerousConfirm
		return nil
	})
	apply("metrics_admin_policy", oldCfg.Metrics.RequireAdminToken != newCfg.Metrics.RequireAdminToken, func() error {
		effective.Metrics.RequireAdminToken = newCfg.Metrics.RequireAdminToken
		return nil
	})

	result.RestartRequired = restartReqSections(oldCfg, newCfg)
	if applyErr != nil {
		a.setConfig(effective)
		return result, applyErr
	}
	a.setConfig(effective)
	if len(result.Applied) == 0 && len(result.RestartRequired) == 0 {
		result.Skipped = append(result.Skipped, "no runtime-applicable changes")
	}
	return result, nil
}

func adminSecurityChanged(oldAdmin, newAdmin config.AdminSection) bool {
	return oldAdmin.Token != newAdmin.Token ||
		oldAdmin.RBACEnabled != newAdmin.RBACEnabled ||
		oldAdmin.DangerousConfirm != newAdmin.DangerousConfirm ||
		!reflect.DeepEqual(oldAdmin.Tokens, newAdmin.Tokens)
}

func cloneAdminTokens(tokens []config.AdminTokenSection) []config.AdminTokenSection {
	if len(tokens) == 0 {
		return nil
	}
	out := make([]config.AdminTokenSection, len(tokens))
	copy(out, tokens)
	for idx := range out {
		if len(out[idx].Scopes) > 0 {
			out[idx].Scopes = append([]string(nil), out[idx].Scopes...)
		}
	}
	return out
}

func restartReqSections(oldCfg, newCfg config.ServiceConfig) []string {
	var out []string
	if !reflect.DeepEqual(oldCfg.Service, newCfg.Service) {
		out = append(out, "service")
	}
	if !reflect.DeepEqual(oldCfg.Cluster, newCfg.Cluster) {
		out = append(out, "cluster")
	}
	if oldCfg.Admin.Listen != newCfg.Admin.Listen ||
		oldCfg.Admin.ReadTimeoutSeconds != newCfg.Admin.ReadTimeoutSeconds ||
		oldCfg.Admin.ReadHeaderTimeoutSeconds != newCfg.Admin.ReadHeaderTimeoutSeconds ||
		oldCfg.Admin.WriteTimeoutSeconds != newCfg.Admin.WriteTimeoutSeconds ||
		oldCfg.Admin.IdleTimeoutSeconds != newCfg.Admin.IdleTimeoutSeconds {
		out = append(out, "admin_http")
	}
	if oldCfg.Metrics.PrometheusEnabled != newCfg.Metrics.PrometheusEnabled {
		out = append(out, "metrics_endpoint")
	}
	if !reflect.DeepEqual(oldCfg.OTel, newCfg.OTel) {
		out = append(out, "otel")
	}
	if !reflect.DeepEqual(oldCfg.Debug, newCfg.Debug) {
		out = append(out, "debug_routes")
	}
	if !reflect.DeepEqual(oldCfg.PVF, newCfg.PVF) {
		out = append(out, "pvf")
	}
	if !reflect.DeepEqual(oldCfg.Audit, newCfg.Audit) {
		out = append(out, "audit_sink")
	}
	if !reflect.DeepEqual(oldCfg.Registry, newCfg.Registry) {
		out = append(out, "registry")
	}
	if !reflect.DeepEqual(oldCfg.Bus, newCfg.Bus) {
		out = append(out, "bus")
	}
	if !reflect.DeepEqual(oldCfg.Worker, newCfg.Worker) {
		out = append(out, "worker_pool")
	}
	if !reflect.DeepEqual(oldCfg.ProfileStore, newCfg.ProfileStore) {
		out = append(out, "profile_store")
	}
	if !reflect.DeepEqual(oldCfg.Gateway, newCfg.Gateway) {
		out = append(out, "gateway")
	}
	return out
}

func addReloadDetail(result *reload.Result, key string, value any) {
	if result == nil {
		return
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	if result.Details == nil {
		result.Details = make(map[string]any)
	}
	result.Details[key] = value
}

func reloadConfig(cfg config.ServiceConfig) reload.Config {
	return reload.Config{
		Enabled:      cfg.Reload.Enabled,
		PollInterval: time.Duration(cfg.Reload.PollIntervalSeconds) * time.Second,
		ApplyTimeout: time.Duration(cfg.Reload.ApplyTimeoutSeconds) * time.Second,
	}
}
