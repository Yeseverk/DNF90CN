package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"longheng.io/server/internal/platform/health"
	"longheng.io/server/internal/platform/otelx"
	"longheng.io/server/internal/platform/systemd"
)

// Build 按 manifest 配置顺序生成组件列表，并在返回前拒绝 nil 组件。
func (m ServiceManifest) Build(env *Env) ([]Component, error) {
	if env == nil {
		env = &Env{}
	}
	for _, configure := range m.Configure {
		if configure == nil {
			continue
		}
		if err := configure(env); err != nil {
			return nil, err
		}
	}
	configured := append([]Component(nil), env.Components...)
	if m.Components == nil {
		for idx, component := range configured {
			if component == nil {
				return configured, fmt.Errorf("configured service component %d is nil", idx)
			}
		}
		return configured, nil
	}
	components, err := m.Components(env)
	if err != nil {
		return components, err
	}
	components = append(configured, components...)
	for idx, component := range components {
		if component == nil {
			return components, fmt.Errorf("service component %d is nil", idx)
		}
	}
	return components, nil
}

func (a *Application) start(ctx context.Context) error {
	a.health.SetServiceState(health.StateStarting, "bootstrapping")

	if err := a.preflight(ctx); err != nil {
		_ = cleanupComponents(context.Background(), a.health, a.constructed)
		_, _ = a.closePlatformRes()
		_ = shutdownOTel(context.Background(), a.otelShutdown)
		return err
	}

	a.health.SetStartupStep("components", health.StateStarting, "starting components")

	for idx, comp := range a.components {
		a.health.SetComponent(comp.Name(), health.StateStarting, "")
		if err := comp.Start(ctx); err != nil {
			a.health.SetComponent(comp.Name(), health.StateDegraded, err.Error())
			a.health.SetStartupStep("components", health.StateDegraded, err.Error())
			a.incMetric("service_start_errors_total", map[string]string{"component": comp.Name(), "phase": "start"})
			_ = a.stopComponents(ctx, idx+1)
			_, _ = a.closePlatformRes()
			_ = shutdownOTel(context.Background(), a.otelShutdown)
			return fmt.Errorf("start component %s: %w", comp.Name(), err)
		}
		a.health.SetComponent(comp.Name(), health.StateReady, "running")
	}
	a.health.SetStartupStep("components", health.StateReady, "components running")

	a.health.SetStartupStep("after-start-hooks", health.StateStarting, "running after start hooks")
	for _, comp := range a.components {
		hook, ok := comp.(AfterStarter)
		if !ok {
			continue
		}
		if err := hook.AfterStart(ctx); err != nil {
			a.health.SetComponent(comp.Name(), health.StateDegraded, err.Error())
			a.health.SetStartupStep("after-start-hooks", health.StateDegraded, err.Error())
			a.incMetric("service_start_errors_total", map[string]string{"component": comp.Name(), "phase": "after_start"})
			_ = a.stopComponents(ctx, len(a.components))
			_, _ = a.closePlatformRes()
			_ = shutdownOTel(context.Background(), a.otelShutdown)
			return fmt.Errorf("after start component %s: %w", comp.Name(), err)
		}
	}
	a.health.SetStartupStep("after-start-hooks", health.StateReady, "after start hooks complete")

	a.health.SetServiceState(health.StateReady, "running")
	if err := systemd.Ready(); err != nil && a.logger != nil {
		a.logger.Warn("systemd ready notification failed", "error", err)
	}
	a.watchdogStop = systemd.StartWatchdog(context.Background(), func(err error) {
		if a.logger != nil {
			a.logger.Warn("systemd watchdog notification failed", "error", err)
		}
	})
	a.incMetric("service_start_total", nil)
	if a.logger != nil {
		cfg := a.configSnapshot()
		a.logger.Info("service started", "service", cfg.Service.Name, "node_id", cfg.Service.NodeID)
	}
	return nil
}

func (a *Application) stop(ctx context.Context) error {
	a.health.SetServiceState(health.StateStopping, "shutting down")
	if a.watchdogStop != nil {
		a.watchdogStop()
		a.watchdogStop = nil
	}
	if err := systemd.Stopping(); err != nil && a.logger != nil {
		a.logger.Warn("systemd stopping notification failed", "error", err)
	}

	err := a.beforeStopComponents(ctx, len(a.components))
	drain := a.waitStopDrain(ctx)
	a.recordDrainResult(drain)
	err = errors.Join(err, a.stopCompInstances(ctx, len(a.components)))
	busErr, regErr := a.closePlatformRes()
	otelErr := shutdownOTel(ctx, a.otelShutdown)

	a.health.SetServiceState(health.StateStopped, "stopped")
	a.incMetric("service_stop_total", nil)
	if a.logger != nil {
		cfg := a.configSnapshot()
		a.logger.Info("service stopped", "service", cfg.Service.Name)
	}

	if err != nil {
		return err
	}
	if busErr != nil {
		return busErr
	}
	if regErr != nil {
		return regErr
	}
	return otelErr
}

func cleanupComponents(ctx context.Context, healthSvc *health.Service, components []Component) error {
	cleanupCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var firstErr error
	for i := len(components) - 1; i >= 0; i-- {
		comp := components[i]
		if comp == nil {
			continue
		}
		if healthSvc != nil {
			healthSvc.SetComponent(comp.Name(), health.StateStopping, "preflight cleanup")
		}
		if err := comp.Stop(cleanupCtx); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("cleanup component %s: %w", comp.Name(), err)
			if healthSvc != nil {
				healthSvc.SetComponent(comp.Name(), health.StateDegraded, err.Error())
			}
			continue
		}
		if healthSvc != nil {
			healthSvc.SetComponent(comp.Name(), health.StateStopped, "preflight cleanup")
		}
	}
	return firstErr
}

func shutdownOTel(ctx context.Context, shutdown otelx.Shutdown) error {
	if shutdown == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return shutdown(ctx)
}

func (a *Application) stopComponents(ctx context.Context, count int) error {
	err := a.beforeStopComponents(ctx, count)
	return errors.Join(err, a.stopCompInstances(ctx, count))
}

func (a *Application) beforeStopComponents(ctx context.Context, count int) error {
	var firstErr error
	for i := count - 1; i >= 0; i-- {
		comp := a.components[i]
		hook, ok := comp.(BeforeStopper)
		if !ok {
			continue
		}
		if err := hook.BeforeStop(ctx); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("before stop component %s: %w", comp.Name(), err)
			a.health.SetComponent(comp.Name(), health.StateDegraded, err.Error())
		}
	}
	return firstErr
}

func (a *Application) stopCompInstances(ctx context.Context, count int) error {
	var firstErr error
	for i := count - 1; i >= 0; i-- {
		comp := a.components[i]
		a.health.SetComponent(comp.Name(), health.StateStopping, "")
		if err := comp.Stop(ctx); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("stop component %s: %w", comp.Name(), err)
			a.health.SetComponent(comp.Name(), health.StateDegraded, err.Error())
			continue
		}
		a.health.SetComponent(comp.Name(), health.StateStopped, "stopped")
	}
	return firstErr
}

func (a *Application) recordDrainResult(result StopPolicyResult) {
	if result.Outcome == StopPolicyNoProbe {
		return
	}
	labels := map[string]string{"outcome": string(result.Outcome)}
	if result.Err != nil {
		a.incMetric("service_stop_drain_errors_total", labels)
	} else {
		a.incMetric("service_stop_drain_total", labels)
	}
	if a.health != nil && result.Outcome == StopPolicyStableAbandoned {
		a.health.SetStartupStep("stop-drain", health.StateDegraded, formatDrainDetail(result))
	} else if a.health != nil {
		a.health.SetStartupStep("stop-drain", health.StateReady, formatDrainDetail(result))
	}
	if a.logger == nil {
		return
	}
	args := []any{
		"outcome", result.Outcome,
		"remaining", result.Remaining,
		"probes", result.Probes,
		"elapsed", result.Elapsed.String(),
	}
	if result.Detail != "" {
		args = append(args, "detail", result.Detail)
	}
	if result.Err != nil {
		args = append(args, "error", result.Err)
		a.logger.Warn("service stop drain probe failed", args...)
		return
	}
	a.logger.Info("service stop drain observed", args...)
}

func (a *Application) incMetric(name string, labels map[string]string) {
	if a.metrics != nil {
		a.metrics.Inc(name, a.metricLabels(labels))
	}
}

func (a *Application) metricLabels(labels map[string]string) map[string]string {
	cfg := a.configSnapshot()
	out := make(map[string]string, len(labels)+2)
	if cfg.Service.Name != "" {
		out["service"] = cfg.Service.Name
	}
	if cfg.Service.NodeID != "" {
		out["node_id"] = cfg.Service.NodeID
	}
	for key, value := range labels {
		out[key] = value
	}
	return out
}

// Run 启动 Application 并在收到取消或系统信号后按停服策略关闭组件。
func (a *Application) Run(ctx context.Context) error {
	runCtx, stopSignal := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
	defer stopSignal()

	if err := a.start(runCtx); err != nil {
		return err
	}

	<-runCtx.Done()

	stopTimeout := a.configSnapshot().Service.StopTimeout()
	if stopTimeout <= 0 {
		stopTimeout = 10 * time.Second
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), stopTimeout)
	defer cancel()
	var dumpOnce sync.Once
	dumpShutdown := func() {
		cfg := a.configSnapshot()
		path, err := DumpGoroutineStack(ShutdownDumpOptions{Service: cfg.Service.Name})
		if a.logger == nil {
			return
		}
		if err != nil {
			a.logger.Warn("failed to dump goroutine stack on shutdown timeout", "error", err)
			return
		}
		a.logger.Warn("dumped goroutine stack on shutdown timeout", "path", path)
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-stopCtx.Done():
			if errors.Is(stopCtx.Err(), context.DeadlineExceeded) {
				dumpOnce.Do(dumpShutdown)
			}
		case <-done:
		}
	}()
	err := a.stop(stopCtx)
	close(done)
	if errors.Is(stopCtx.Err(), context.DeadlineExceeded) {
		dumpOnce.Do(dumpShutdown)
	}
	return err
}
