package hotreload

import (
	"context"
	"errors"
	"strings"
	"time"
)

var (
	// ErrRouteRequired 表示热重载 route 为空。
	ErrRouteRequired = errors.New("hot reload route is required")

	// ErrVersionRequired 表示热重载版本为空。
	ErrVersionRequired = errors.New("hot reload version is required")

	// ErrHandlerRequired 表示模块缺少 handler。
	ErrHandlerRequired = errors.New("hot reload handler is required")

	// ErrRouteNotFound 表示 route 尚未注册。
	ErrRouteNotFound = errors.New("hot reload route is not found")

	// ErrVersionNotFound 表示目标版本尚未注册。
	ErrVersionNotFound = errors.New("hot reload version is not found")

	// ErrVersionExists 表示同一 route 下的版本已经注册，不能被静默覆盖。
	ErrVersionExists = errors.New("hot reload version already exists")

	// ErrHandlerUnavailable 表示当前激活版本没有可用 handler。
	ErrHandlerUnavailable = errors.New("hot reload handler is unavailable")

	// ErrBarrierDraining 表示热切换屏障正在排空活跃调用。
	ErrBarrierDraining = errors.New("hot reload barrier is draining")

	// ErrInvalidMetadataName 表示 metadata 字段名非法。
	ErrInvalidMetadataName = errors.New("hot reload metadata name is invalid")
)

// Request 是安全热重载 handler 的最小调用上下文。
type Request struct {
	Route    string         `json:"route"`
	Version  string         `json:"version,omitempty"`
	Payload  any            `json:"payload,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Response 是安全热重载 handler 的最小返回结构。
type Response struct {
	Payload  any            `json:"payload,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Handler 是热重载 route 的业务处理函数。
type Handler func(context.Context, Request) (Response, error)

// Hook 是热重载模块激活、预检和停用时执行的钩子。
type Hook func(context.Context, Module) error

// Module 表示一个可在线切换的 route 实现版本。
type Module struct {
	Route      string         `json:"route"`
	Version    string         `json:"version"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	Handler    Handler        `json:"-"`
	Preflight  Hook           `json:"-"`
	Activate   Hook           `json:"-"`
	Deactivate Hook           `json:"-"`
}

// Options 定义热重载 registry 的可注入依赖。
type Options struct {
	Now func() time.Time
}

// Result 描述一次版本切换或回滚的结果。
type Result struct {
	Route           string    `json:"route"`
	PreviousVersion string    `json:"previous_version,omitempty"`
	ActiveVersion   string    `json:"active_version"`
	ActivatedAt     time.Time `json:"activated_at"`
	DeactivateError string    `json:"deactivate_error,omitempty"`
}

// RouteSnapshot 描述单个 route 的当前版本和已注册版本。
type RouteSnapshot struct {
	Route         string    `json:"route"`
	ActiveVersion string    `json:"active_version,omitempty"`
	Versions      []string  `json:"versions,omitempty"`
	Swaps         int64     `json:"swaps"`
	UpdatedAt     time.Time `json:"updated_at,omitempty"`
}

// Snapshot 描述热重载 registry 的整体快照。
type Snapshot struct {
	Routes []RouteSnapshot `json:"routes,omitempty"`
}

func normalizeModule(module Module) (Module, error) {
	module.Route = normalizeName(module.Route)
	if module.Route == "" {
		return Module{}, ErrRouteRequired
	}
	module.Version = normalizeName(module.Version)
	if module.Version == "" {
		return Module{}, ErrVersionRequired
	}
	if module.Handler == nil {
		return Module{}, ErrHandlerRequired
	}
	module.Metadata = copyMetadata(module.Metadata)
	return module, nil
}

func normRouteVersion(route, version string) (string, string, error) {
	route = normalizeName(route)
	if route == "" {
		return "", "", ErrRouteRequired
	}
	version = normalizeName(version)
	if version == "" {
		return "", "", ErrVersionRequired
	}
	return route, version, nil
}

func normalizeName(value string) string {
	return strings.TrimSpace(value)
}

func copyMetadata(in map[string]any) map[string]any {
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
