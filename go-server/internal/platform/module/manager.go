package module

import (
	"context"
	"fmt"
	"log/slog"

	"longheng.io/server/internal/platform/health"
	"longheng.io/server/internal/platform/runtimeguard"
)

type Module interface {
	Name() string
	Start(context.Context) error
	Stop(context.Context) error
}

// AfterStarter 必须幂等；Manager 可能在失败回滚时调用 Stop 清理已完成 AfterStart 的模块。
type AfterStarter interface {
	AfterStart(context.Context) error
}

// BeforeStopper 不应阻塞等待外部流量自然耗尽；优雅排水应由模块自己的 Stop ctx 控制。
type BeforeStopper interface {
	BeforeStop(context.Context) error
}

// PreflightChecker 只校验装配和后端可达性，不应改变业务状态。
type PreflightChecker interface {
	Preflight(context.Context) error
}

type Manager struct {
	name    string
	logger  *slog.Logger
	health  *health.Service
	modules []Module
	profile *runtimeguard.Profile
}

func New(name string, logger *slog.Logger, healthSvc *health.Service) *Manager {
	return &Manager{
		name:   name,
		logger: logger,
		health: healthSvc,
	}
}

func (m *Manager) Name() string {
	return m.name
}

func (m *Manager) Add(mod Module) {
	m.modules = append(m.modules, mod)
}

func (m *Manager) SetRuntimeGuardProfile(profile runtimeguard.Profile) {
	if m == nil {
		return
	}
	m.profile = &profile
}

func (m *Manager) Preflight(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	for _, mod := range m.modules {
		if err := m.checkRuntimeBackend(mod); err != nil {
			return err
		}
		checker, ok := mod.(PreflightChecker)
		if !ok {
			continue
		}
		if m.health != nil {
			m.health.SetComponent(mod.Name(), health.StateStarting, "preflight")
		}
		if err := checker.Preflight(ctx); err != nil {
			if m.health != nil {
				m.health.SetComponent(mod.Name(), health.StateDegraded, err.Error())
			}
			return fmt.Errorf("preflight module %s: %w", mod.Name(), err)
		}
		if m.health != nil {
			m.health.SetComponent(mod.Name(), health.StateStarting, "preflight ok")
		}
	}
	return nil
}

func (m *Manager) checkRuntimeBackend(mod Module) error {
	if m == nil || m.profile == nil || mod == nil {
		return nil
	}
	describer, ok := mod.(runtimeguard.BackendDescriber)
	if !ok {
		return nil
	}
	if err := runtimeguard.CheckDescriber(mod.Name(), describer, *m.profile); err != nil {
		return fmt.Errorf("preflight module %s runtime backend: %w", mod.Name(), err)
	}
	return nil
}

func (m *Manager) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	for i, mod := range m.modules {
		if m.health != nil {
			m.health.SetComponent(mod.Name(), health.StateStarting, "")
		}
		if err := mod.Start(ctx); err != nil {
			if m.health != nil {
				m.health.SetComponent(mod.Name(), health.StateDegraded, err.Error())
			}
			_ = m.stopStarted(ctx, i+1)
			return fmt.Errorf("start module %s: %w", mod.Name(), err)
		}
		if m.health != nil {
			m.health.SetComponent(mod.Name(), health.StateReady, "running")
		}
	}
	for _, mod := range m.modules {
		hook, ok := mod.(AfterStarter)
		if !ok {
			continue
		}
		if err := hook.AfterStart(ctx); err != nil {
			if m.health != nil {
				m.health.SetComponent(mod.Name(), health.StateDegraded, err.Error())
			}
			_ = m.stopStarted(ctx, len(m.modules))
			return fmt.Errorf("after start module %s: %w", mod.Name(), err)
		}
	}
	if m.logger != nil {
		m.logger.Info("module manager started", "name", m.name, "count", len(m.modules))
	}
	return nil
}

func (m *Manager) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	return m.stopStarted(ctx, len(m.modules))
}

func (m *Manager) stopStarted(ctx context.Context, count int) error {
	var firstErr error
	for i := count - 1; i >= 0; i-- {
		mod := m.modules[i]
		hook, ok := mod.(BeforeStopper)
		if !ok {
			continue
		}
		if err := hook.BeforeStop(ctx); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("before stop module %s: %w", mod.Name(), err)
			if m.health != nil {
				m.health.SetComponent(mod.Name(), health.StateDegraded, err.Error())
			}
		}
	}
	for i := count - 1; i >= 0; i-- {
		mod := m.modules[i]
		if m.health != nil {
			m.health.SetComponent(mod.Name(), health.StateStopping, "")
		}
		if err := mod.Stop(ctx); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("stop module %s: %w", mod.Name(), err)
			if m.health != nil {
				m.health.SetComponent(mod.Name(), health.StateDegraded, err.Error())
			}
			continue
		}
		if m.health != nil {
			m.health.SetComponent(mod.Name(), health.StateStopped, "stopped")
		}
	}
	if m.logger != nil {
		m.logger.Info("module manager stopped", "name", m.name)
	}
	return firstErr
}
