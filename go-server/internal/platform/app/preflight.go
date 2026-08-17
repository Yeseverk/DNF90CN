package app

import (
	"context"
	"fmt"
	"strings"

	"longheng.io/server/internal/platform/config"
	"longheng.io/server/internal/platform/health"
)

func (a *Application) preflight(ctx context.Context) error {
	a.health.SetStartupStep("preflight", health.StateStarting, "checking dependencies")
	if err := a.checkStartupSafety(); err != nil {
		a.health.SetStartupStep("preflight", health.StateDegraded, err.Error())
		a.incMetric("service_start_errors_total", map[string]string{"component": "config", "phase": "preflight"})
		return err
	}
	checkCtx, cancel := context.WithTimeout(ctx, defPreflight)
	defer cancel()

	if err := a.checkDependency(checkCtx, "registry", a.registry); err != nil {
		return err
	}
	if err := a.checkDependency(checkCtx, "bus", a.bus); err != nil {
		return err
	}
	for _, comp := range a.components {
		checker, ok := comp.(PreflightChecker)
		if !ok {
			continue
		}
		a.health.SetComponent(comp.Name(), health.StateStarting, "preflight")
		if err := checker.Preflight(checkCtx); err != nil {
			a.health.SetComponent(comp.Name(), health.StateDegraded, err.Error())
			a.health.SetStartupStep("preflight", health.StateDegraded, err.Error())
			a.incMetric("service_start_errors_total", map[string]string{"component": comp.Name(), "phase": "preflight"})
			return fmt.Errorf("preflight component %s: %w", comp.Name(), err)
		}
		a.health.SetComponent(comp.Name(), health.StateStarting, "preflight ok")
	}

	a.health.SetStartupStep("preflight", health.StateReady, "dependencies ready")
	return nil
}

func (a *Application) checkStartupSafety() error {
	cfg := a.configSnapshot()
	environment := strings.ToLower(strings.TrimSpace(cfg.Service.Environment))
	if !config.IsProdLikeEnv(environment) {
		return nil
	}
	if strings.EqualFold(strings.TrimSpace(cfg.Admin.Token), "local-admin-token") {
		return fmt.Errorf("admin.token must not use local-admin-token outside local/dev/test environments")
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("startup config safety: %w", err)
	}
	return nil
}

func (a *Application) checkDependency(ctx context.Context, name string, dependency any) error {
	if dependency == nil {
		return nil
	}
	checker, ok := dependency.(interface {
		Check(context.Context) error
	})
	if !ok {
		return nil
	}
	if err := checker.Check(ctx); err != nil {
		a.health.SetStartupStep("preflight", health.StateDegraded, err.Error())
		a.incMetric("service_start_errors_total", map[string]string{"component": name, "phase": "preflight"})
		return fmt.Errorf("preflight %s: %w", name, err)
	}
	return nil
}
