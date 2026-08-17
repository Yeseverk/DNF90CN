package hotreload

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

// Registry 保存 route 到 handler 版本的映射，并负责原子切换当前版本。
type Registry struct {
	mu     sync.RWMutex
	routes map[string]*routeSlot
	now    func() time.Time
}

type routeSlot struct {
	mu         sync.RWMutex
	activateMu sync.Mutex
	route      string
	modules    map[string]Module
	active     string
	swaps      int64
	updatedAt  time.Time
}

// NewRegistry 创建热重载版本注册表。
func NewRegistry(options Options) *Registry {
	registry := &Registry{now: options.Now}
	_ = registry.ensureReady()
	return registry
}

// Register 注册一个 route 的可切换实现版本。
func (r *Registry) Register(module Module) error {
	module, err := normalizeModule(module)
	if err != nil {
		return err
	}
	if err := r.ensureReady(); err != nil {
		return err
	}
	r.mu.Lock()
	slot := r.routes[module.Route]
	if slot == nil {
		slot = &routeSlot{
			route:   module.Route,
			modules: make(map[string]Module),
		}
		r.routes[module.Route] = slot
	}
	r.mu.Unlock()

	slot.mu.Lock()
	if _, ok := slot.modules[module.Version]; ok {
		slot.mu.Unlock()
		return ErrVersionExists
	}
	slot.modules[module.Version] = module
	if slot.active == "" {
		slot.active = module.Version
		slot.updatedAt = r.now()
	}
	slot.mu.Unlock()
	return nil
}

// Activate 将指定 route 原子切换到目标版本。
func (r *Registry) Activate(ctx context.Context, route, version string) (Result, error) {
	route, version, err := normRouteVersion(route, version)
	if err != nil {
		return Result{}, err
	}
	if err := r.ensureReady(); err != nil {
		return Result{}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	slot := r.routeSlot(route)
	if slot == nil {
		return Result{}, ErrRouteNotFound
	}

	var previous Module
	var hasPrevious bool
	unlock, err := slot.lockActivate(ctx)
	if err != nil {
		return Result{}, err
	}
	defer unlock()
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	slot.mu.RLock()
	module, ok := slot.modules[version]
	if !ok {
		slot.mu.RUnlock()
		return Result{}, ErrVersionNotFound
	}
	if slot.active == version {
		result := Result{Route: route, PreviousVersion: version, ActiveVersion: version, ActivatedAt: slot.updatedAt}
		slot.mu.RUnlock()
		return result, nil
	}
	if active := slot.active; active != "" {
		previous, hasPrevious = slot.modules[active]
	}
	slot.mu.RUnlock()

	if module.Preflight != nil {
		if err := module.Preflight(ctx, module); err != nil {
			return Result{}, err
		}
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if module.Activate != nil {
		if err := module.Activate(ctx, module); err != nil {
			return Result{}, err
		}
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	now := r.now()

	slot.mu.Lock()
	result := Result{
		Route:           route,
		PreviousVersion: slot.active,
		ActiveVersion:   version,
		ActivatedAt:     now,
	}
	slot.active = version
	slot.updatedAt = now
	slot.swaps++
	slot.mu.Unlock()

	if hasPrevious && previous.Version != version && previous.Deactivate != nil {
		if err := previous.Deactivate(ctx, previous); err != nil {
			result.DeactivateError = err.Error()
		}
	}
	return result, nil
}

// Rollback 通过重新激活旧版本完成 route 回滚。
func (r *Registry) Rollback(ctx context.Context, route, version string) (Result, error) {
	return r.Activate(ctx, route, version)
}

// Dispatch 将请求分发到 route 当前激活版本的 handler。
func (r *Registry) Dispatch(ctx context.Context, route string, payload any, metadata map[string]any) (Response, error) {
	route = normalizeName(route)
	if route == "" {
		return Response{}, ErrRouteRequired
	}
	if err := r.ensureReady(); err != nil {
		return Response{}, err
	}
	slot := r.routeSlot(route)
	if slot == nil {
		return Response{}, ErrRouteNotFound
	}
	slot.mu.RLock()
	module, ok := slot.modules[slot.active]
	slot.mu.RUnlock()
	if !ok || module.Handler == nil {
		return Response{}, ErrHandlerUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Response{}, err
	}
	return module.Handler(ctx, Request{
		Route:    route,
		Version:  module.Version,
		Payload:  payload,
		Metadata: copyMetadata(metadata),
	})
}

// ActiveVersion 查询 route 当前激活版本。
func (r *Registry) ActiveVersion(route string) (string, bool) {
	route = normalizeName(route)
	if route == "" {
		return "", false
	}
	if err := r.ensureReady(); err != nil {
		return "", false
	}
	slot := r.routeSlot(route)
	if slot == nil {
		return "", false
	}
	slot.mu.RLock()
	version := slot.active
	slot.mu.RUnlock()
	return version, version != ""
}

// Snapshot 返回所有 route 的稳定排序快照。
func (r *Registry) Snapshot() Snapshot {
	if err := r.ensureReady(); err != nil {
		return Snapshot{}
	}
	r.mu.RLock()
	slots := make([]*routeSlot, 0, len(r.routes))
	for _, slot := range r.routes {
		slots = append(slots, slot)
	}
	r.mu.RUnlock()
	sort.Slice(slots, func(i, j int) bool { return slots[i].route < slots[j].route })

	snapshot := Snapshot{Routes: make([]RouteSnapshot, 0, len(slots))}
	for _, slot := range slots {
		slot.mu.RLock()
		versions := make([]string, 0, len(slot.modules))
		for version := range slot.modules {
			versions = append(versions, version)
		}
		sort.Strings(versions)
		snapshot.Routes = append(snapshot.Routes, RouteSnapshot{
			Route:         slot.route,
			ActiveVersion: slot.active,
			Versions:      versions,
			Swaps:         slot.swaps,
			UpdatedAt:     slot.updatedAt,
		})
		slot.mu.RUnlock()
	}
	return snapshot
}

func (r *Registry) ensureReady() error {
	if r == nil {
		return ErrRouteNotFound
	}
	r.mu.Lock()
	if r.routes == nil {
		r.routes = make(map[string]*routeSlot)
	}
	if r.now == nil {
		r.now = func() time.Time { return time.Now().UTC() }
	}
	r.mu.Unlock()
	return nil
}

func (r *Registry) routeSlot(route string) *routeSlot {
	r.mu.RLock()
	slot := r.routes[route]
	r.mu.RUnlock()
	return slot
}

func (s *routeSlot) lockActivate(ctx context.Context) (func(), error) {
	if s.activateMu.TryLock() {
		return s.activateMu.Unlock, nil
	}
	timer := time.NewTimer(time.Millisecond)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
		if s.activateMu.TryLock() {
			return s.activateMu.Unlock, nil
		}
		timer.Reset(time.Millisecond)
	}
}

// IsNotFound 判断错误是否属于热重载 route 或版本不存在。
func IsNotFound(err error) bool {
	return errors.Is(err, ErrRouteNotFound) || errors.Is(err, ErrVersionNotFound)
}
