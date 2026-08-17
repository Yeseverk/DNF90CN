package dispatch

import (
	"context"
	"fmt"
	"strings"

	"longheng.io/server/internal/platform/hotreload"
)

type HotReloadOptions struct {
	Metadata   map[string]any
	Preflight  func(context.Context, HandlerMeta) error
	Activate   func(context.Context, HandlerMeta) error
	Deactivate func(context.Context, HandlerMeta) error
}

type HotReloadSnapshot struct {
	Drain    hotreload.BarrierSnapshot            `json:"drain"`
	Drains   map[string]hotreload.BarrierSnapshot `json:"drains,omitempty"`
	Registry hotreload.Snapshot                   `json:"registry,omitempty"`
}

// RegisterHotReloadHandler 把 dispatch handler 注册为可版本化热重载模块。
func (m *Mux) RegisterHotReloadHandler(registry *hotreload.Registry, meta HandlerMeta, version string, handler HandlerFunc, options HotReloadOptions) error {
	if m == nil || registry == nil {
		return hotreload.ErrRouteNotFound
	}
	version = strings.TrimSpace(version)
	if version == "" {
		return hotreload.ErrVersionRequired
	}
	if handler == nil {
		return hotreload.ErrHandlerRequired
	}
	meta = NormalizeHandlerMeta(meta)
	if meta.MsgID == 0 {
		return hotreload.ErrRouteRequired
	}
	if err := ValidateHandlerMeta(meta); err != nil {
		return err
	}
	m.hotReloadBarrier(meta.MsgID, true)
	route := HotReloadRoute(meta.MsgID)
	_, hadActive := registry.ActiveVersion(route)
	module := hotreload.Module{
		Route:    route,
		Version:  version,
		Metadata: hotReloadMetadata(meta, version, options.Metadata),
		Handler: func(ctx context.Context, request hotreload.Request) (hotreload.Response, error) {
			dispatchRequest, ok := request.Payload.(Request)
			if !ok {
				return hotreload.Response{}, fmt.Errorf("hot reload route %s requires dispatch.Request payload", route)
			}
			response, err := handler(ctx, dispatchRequest)
			if err != nil {
				return hotreload.Response{}, err
			}
			return hotreload.Response{Payload: response}, nil
		},
		Preflight: func(ctx context.Context, hotModule hotreload.Module) error {
			if options.Preflight != nil {
				return options.Preflight(ctx, meta)
			}
			return nil
		},
		Activate: func(ctx context.Context, hotModule hotreload.Module) error {
			if options.Activate != nil {
				if err := options.Activate(ctx, meta); err != nil {
					return err
				}
			}
			return m.HandleMetaChecked(meta, handler)
		},
		Deactivate: func(ctx context.Context, hotModule hotreload.Module) error {
			if options.Deactivate != nil {
				return options.Deactivate(ctx, meta)
			}
			return nil
		},
	}
	if !hadActive {
		if module.Preflight != nil {
			if err := module.Preflight(context.Background(), module); err != nil {
				return err
			}
		}
		if module.Activate != nil {
			if err := module.Activate(context.Background(), module); err != nil {
				return err
			}
		}
	}
	return registry.Register(module)
}

func (m *Mux) ActivateHotReloadHandler(ctx context.Context, registry *hotreload.Registry, msgID uint32, version string) (hotreload.Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if m == nil {
		return hotreload.Result{}, hotreload.ErrRouteNotFound
	}
	if registry == nil {
		return hotreload.Result{}, hotreload.ErrRouteNotFound
	}
	state := m.hotReloadState(msgID, true)
	if state != nil {
		state.activationMu.Lock()
		defer state.activationMu.Unlock()
		if err := state.barrier.Drain(ctx); err != nil {
			state.barrier.Resume()
			return hotreload.Result{}, err
		}
		defer state.barrier.Resume()
	}
	return registry.Activate(ctx, HotReloadRoute(msgID), version)
}

func (m *Mux) HotReloadSnapshot(registry *hotreload.Registry) HotReloadSnapshot {
	snapshot := HotReloadSnapshot{}
	if m == nil {
		snapshot.Drain = hotreload.BarrierSnapshot{Accepting: true}
	} else {
		snapshot.Drains = m.reloadDrainSnaps()
		snapshot.Drain = aggReloadDrain(snapshot.Drains)
	}
	if registry != nil {
		snapshot.Registry = registry.Snapshot()
	}
	return snapshot
}

func (m *Mux) reloadDrainSnaps() map[string]hotreload.BarrierSnapshot {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	items := make(map[uint32]*hotreload.DrainBarrier, len(m.hotReloadStates))
	for msgID, state := range m.hotReloadStates {
		if state == nil {
			continue
		}
		items[msgID] = state.barrier
	}
	m.mu.RUnlock()
	if len(items) == 0 {
		return nil
	}
	out := make(map[string]hotreload.BarrierSnapshot, len(items))
	for msgID, barrier := range items {
		if barrier == nil {
			continue
		}
		out[HotReloadRoute(msgID)] = barrier.Snapshot()
	}
	return out
}

func aggReloadDrain(drains map[string]hotreload.BarrierSnapshot) hotreload.BarrierSnapshot {
	if len(drains) == 0 {
		return hotreload.BarrierSnapshot{Accepting: true}
	}
	snapshot := hotreload.BarrierSnapshot{Accepting: true}
	for _, item := range drains {
		if !item.Accepting {
			snapshot.Accepting = false
		}
		snapshot.Active += item.Active
		snapshot.Waiters += item.Waiters
		snapshot.DrainWaiters += item.DrainWaiters
		snapshot.EnterWaiters += item.EnterWaiters
	}
	return snapshot
}

func HotReloadRoute(msgID uint32) string {
	return fmt.Sprintf("msg:%d", msgID)
}

func hotReloadMetadata(meta HandlerMeta, version string, extra map[string]any) map[string]any {
	metadata := map[string]any{
		"msg_id":  meta.MsgID,
		"name":    meta.Name,
		"version": version,
	}
	for key, value := range extra {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		metadata[key] = value
	}
	return metadata
}
