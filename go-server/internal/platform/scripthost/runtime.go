package scripthost

import (
	"context"
	"errors"
	"strings"
	"sync"

	"longheng.io/server/internal/platform/hotreload"
)

var (
	// ErrBadScriptRequest 表示热更脚本执行入口收到了非脚本请求。
	ErrBadScriptRequest = errors.New("script request payload is invalid")

	// ErrBadScriptResult 表示脚本 route 返回了非脚本结果。
	ErrBadScriptResult = errors.New("script result payload is invalid")
)

// ScriptHook 是脚本版本切换过程中的预检、激活和停用扩展点。
type ScriptHook func(context.Context, Program) error

// Runtime 是逻辑脚本不停服热更的统一扩展接口。
type Runtime interface {
	Register(context.Context, ScriptModule) error
	Activate(context.Context, string, string) (hotreload.Result, error)
	Rollback(context.Context, string, string) (hotreload.Result, error)
	Execute(context.Context, string, Request) (Result, error)
	Snapshot() hotreload.Snapshot
}

// ScriptModule 描述一个可热更的脚本 route 版本。
type ScriptModule struct {
	Route      string         `json:"route"`
	Version    string         `json:"version"`
	Script     Script         `json:"script"`
	Function   string         `json:"function,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	Preflight  ScriptHook     `json:"-"`
	Activate   ScriptHook     `json:"-"`
	Deactivate ScriptHook     `json:"-"`
}

// RuntimeOptions 定义脚本热更运行时的可注入依赖。
type RuntimeOptions struct {
	Host     Host
	Registry *hotreload.Registry
}

// HotRuntime 把脚本宿主和 hotreload registry 组合成逻辑脚本热更入口。
type HotRuntime struct {
	host     Host
	registry *hotreload.Registry
	mu       sync.Mutex
	bars     map[string]*hotreload.DrainBarrier
	regLocks map[string]*sync.Mutex
}

// NewRuntime 创建可注册、激活、回滚和执行脚本版本的热更运行时。
func NewRuntime(options RuntimeOptions) (*HotRuntime, error) {
	if options.Host == nil {
		return nil, ErrScriptHostRequired
	}
	registry := options.Registry
	if registry == nil {
		registry = hotreload.NewRegistry(hotreload.Options{})
	}
	return &HotRuntime{host: options.Host, registry: registry}, nil
}

// Register 编译脚本并把它注册成可原子切换的 route 版本。
func (r *HotRuntime) Register(ctx context.Context, module ScriptModule) error {
	if r == nil || r.host == nil || r.registry == nil {
		return ErrScriptHostRequired
	}
	if ctx == nil {
		ctx = context.Background()
	}
	module, err := normModule(module)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	program, err := r.host.Compile(ctx, module.Script)
	if err != nil {
		return err
	}
	if program == nil {
		return ErrScriptHostRequired
	}
	regLock := r.regLock(module.Route)
	regLock.Lock()
	defer regLock.Unlock()
	r.bar(module.Route, true)
	_, hadActive := r.registry.ActiveVersion(module.Route)
	hotModule := hotreload.Module{
		Route:    module.Route,
		Version:  module.Version,
		Metadata: scriptMeta(module),
		Handler:  makeHandler(module, program),
		Preflight: func(ctx context.Context, _ hotreload.Module) error {
			return runHook(ctx, module.Preflight, program)
		},
		Activate: func(ctx context.Context, _ hotreload.Module) error {
			return runHook(ctx, module.Activate, program)
		},
		Deactivate: func(ctx context.Context, _ hotreload.Module) error {
			return runHook(ctx, module.Deactivate, program)
		},
	}
	if !hadActive {
		if err := hotModule.Preflight(ctx, hotModule); err != nil {
			return err
		}
		if err := hotModule.Activate(ctx, hotModule); err != nil {
			return err
		}
	}
	return r.registry.Register(hotModule)
}

// Activate 将指定 route 切到目标脚本版本。
func (r *HotRuntime) Activate(ctx context.Context, route, version string) (hotreload.Result, error) {
	return r.switchTo(ctx, route, version)
}

// Rollback 将指定 route 切回旧脚本版本。
func (r *HotRuntime) Rollback(ctx context.Context, route, version string) (hotreload.Result, error) {
	return r.switchTo(ctx, route, version)
}

// Execute 执行当前激活版本的脚本 route。
func (r *HotRuntime) Execute(ctx context.Context, route string, request Request) (Result, error) {
	if r == nil || r.registry == nil {
		return Result{}, ErrScriptHostRequired
	}
	if ctx == nil {
		ctx = context.Background()
	}
	route = strings.TrimSpace(route)
	if route == "" {
		return Result{}, hotreload.ErrRouteRequired
	}
	barrier := r.bar(route, false)
	if barrier != nil {
		leave, err := barrier.EnterWhenReady(ctx)
		if err != nil {
			return Result{}, err
		}
		defer leave()
	}
	response, err := r.registry.Dispatch(ctx, route, request, nil)
	if err != nil {
		return Result{}, err
	}
	result, ok := response.Payload.(Result)
	if !ok {
		return Result{}, ErrBadScriptResult
	}
	return result, nil
}

// Snapshot 返回脚本热更 route 和版本快照。
func (r *HotRuntime) Snapshot() hotreload.Snapshot {
	if r == nil || r.registry == nil {
		return hotreload.Snapshot{}
	}
	return r.registry.Snapshot()
}

func (r *HotRuntime) switchTo(ctx context.Context, route, version string) (hotreload.Result, error) {
	if r == nil || r.registry == nil {
		return hotreload.Result{}, ErrScriptHostRequired
	}
	if ctx == nil {
		ctx = context.Background()
	}
	route = strings.TrimSpace(route)
	if route == "" {
		return hotreload.Result{}, hotreload.ErrRouteRequired
	}
	barrier := r.bar(route, false)
	if barrier != nil {
		if err := barrier.Drain(ctx); err != nil {
			barrier.Resume()
			return hotreload.Result{}, err
		}
		defer barrier.Resume()
	}
	return r.registry.Activate(ctx, route, version)
}

func (r *HotRuntime) bar(route string, create bool) *hotreload.DrainBarrier {
	if r == nil {
		return nil
	}
	route = strings.TrimSpace(route)
	if route == "" {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.bars == nil {
		if !create {
			return nil
		}
		r.bars = make(map[string]*hotreload.DrainBarrier)
	}
	barrier := r.bars[route]
	if barrier == nil && create {
		barrier = hotreload.NewDrainBarrier()
		r.bars[route] = barrier
	}
	return barrier
}

func (r *HotRuntime) regLock(route string) *sync.Mutex {
	route = strings.TrimSpace(route)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.regLocks == nil {
		r.regLocks = make(map[string]*sync.Mutex)
	}
	lock := r.regLocks[route]
	if lock == nil {
		lock = &sync.Mutex{}
		r.regLocks[route] = lock
	}
	return lock
}

func normModule(module ScriptModule) (ScriptModule, error) {
	module.Route = strings.TrimSpace(module.Route)
	if module.Route == "" {
		return ScriptModule{}, hotreload.ErrRouteRequired
	}
	module.Version = strings.TrimSpace(module.Version)
	if module.Version == "" {
		return ScriptModule{}, hotreload.ErrVersionRequired
	}
	if err := ValidateScript(module.Script); err != nil {
		return ScriptModule{}, err
	}
	module.Script.Name = strings.TrimSpace(module.Script.Name)
	module.Script.Language = strings.TrimSpace(module.Script.Language)
	module.Script.Metadata = copyStrMap(module.Script.Metadata)
	module.Function = strings.TrimSpace(module.Function)
	module.Metadata = copyAnyMap(module.Metadata)
	return module, nil
}

func makeHandler(module ScriptModule, program Program) hotreload.Handler {
	return func(ctx context.Context, request hotreload.Request) (hotreload.Response, error) {
		scriptReq, ok := request.Payload.(Request)
		if !ok {
			return hotreload.Response{}, ErrBadScriptRequest
		}
		if strings.TrimSpace(scriptReq.Script) == "" {
			scriptReq.Script = module.Script.Name
		}
		if strings.TrimSpace(scriptReq.Function) == "" {
			scriptReq.Function = module.Function
		}
		scriptReq.Metadata = copyStrMap(scriptReq.Metadata)
		result, err := program.Execute(ctx, scriptReq)
		if err != nil {
			return hotreload.Response{}, err
		}
		return hotreload.Response{Payload: result}, nil
	}
}

func runHook(ctx context.Context, hook ScriptHook, program Program) error {
	if hook == nil {
		return nil
	}
	return hook(ctx, program)
}

func scriptMeta(module ScriptModule) map[string]any {
	metadata := copyAnyMap(module.Metadata)
	if metadata == nil {
		metadata = make(map[string]any, 4)
	}
	metadata["script"] = module.Script.Name
	metadata["version"] = module.Version
	if module.Script.Language != "" {
		metadata["language"] = module.Script.Language
	}
	if module.Function != "" {
		metadata["function"] = module.Function
	}
	return metadata
}

func copyStrMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = value
	}
	return out
}

func copyAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = value
	}
	return out
}
