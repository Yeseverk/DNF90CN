package logic

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	appkit "longheng.io/server/internal/platform/app"
	"longheng.io/server/internal/platform/dispatch"
	"longheng.io/server/internal/platform/httpx"
	"longheng.io/server/internal/platform/registry"
)

func logicAdminWrapper(env *appkit.Env, scope string) func(http.HandlerFunc) http.HandlerFunc {
	if env != nil && env.AdminAuth != nil {
		return func(next http.HandlerFunc) http.HandlerFunc {
			return env.AdminAuth(scope, next)
		}
	}
	var logger *slog.Logger
	if env != nil {
		logger = env.Logger
	}
	return func(next http.HandlerFunc) http.HandlerFunc {
		return httpx.WrapAdminPolicy(func() httpx.AdminPolicy {
			return logicAdminPolicy(env)
		}, logger, scope, next)
	}
}

func logicAdminGuard(env *appkit.Env, scope string) func(string, http.HandlerFunc) http.HandlerFunc {
	if env != nil && env.AdminDangerous != nil {
		return func(operation string, next http.HandlerFunc) http.HandlerFunc {
			return env.AdminDangerous(scope, operation, next)
		}
	}
	var logger *slog.Logger
	if env != nil {
		logger = env.Logger
	}
	return func(operation string, next http.HandlerFunc) http.HandlerFunc {
		return httpx.WrapDangerousAdminPolicy(func() httpx.AdminPolicy {
			return logicAdminPolicy(env)
		}, logger, scope, operation, next)
	}
}

func logicAdminPolicy(env *appkit.Env) httpx.AdminPolicy {
	if env == nil {
		return httpx.AdminPolicy{}
	}
	tokens := make([]httpx.AdminToken, 0, len(env.Config.Admin.Tokens))
	for _, token := range env.Config.Admin.Tokens {
		tokens = append(tokens, httpx.AdminToken{
			Name:   token.Name,
			Token:  token.Token,
			Role:   token.Role,
			Scopes: append([]string(nil), token.Scopes...),
		})
	}
	return httpx.AdminPolicy{
		Token:               env.Config.Admin.Token,
		RBACEnabled:         env.Config.Admin.RBACEnabled,
		RequireConfirmation: env.Config.Admin.DangerousConfirm,
		Tokens:              tokens,
	}
}

func routesFromHandlers(handlers []dispatch.HandlerMeta) []registry.Route {
	routes := make([]registry.Route, 0, len(handlers))
	for _, meta := range handlers {
		if meta.MsgID == 0 {
			continue
		}
		routes = append(routes, registry.Route{
			ID:         fmt.Sprint(meta.MsgID),
			Group:      strings.TrimSpace(meta.RouteGroup),
			Internal:   meta.Internal,
			Stateful:   meta.Stateful || meta.PlayerScoped,
			Authorized: meta.Authorized || meta.AuthRequired,
			Idempotent: meta.Idempotent,
		})
	}
	return routes
}
