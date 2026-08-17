package app

import (
	"log/slog"
	"net/http"

	"longheng.io/server/internal/platform/audit"
	"longheng.io/server/internal/platform/config"
	"longheng.io/server/internal/platform/httpx"
	"longheng.io/server/internal/platform/ratelimit"
)

type adminSecurity struct {
	configProvider func() config.ServiceConfig
	audit          *audit.Logger
	logger         *slog.Logger
	rateLimiter    *ratelimit.Limiter
}

func newAdminSecurity(configProvider func() config.ServiceConfig, auditLogger *audit.Logger, logger *slog.Logger, limiter *ratelimit.Limiter) adminSecurity {
	return adminSecurity{
		configProvider: configProvider,
		audit:          auditLogger,
		logger:         logger,
		rateLimiter:    limiter,
	}
}

func (s adminSecurity) wrap(scope string, next http.HandlerFunc) http.HandlerFunc {
	handler := httpx.WrapAdminPolicy(s.policy, s.logger, scope, next)
	if s.rateLimiter == nil {
		return handler
	}
	return s.rateLimiter.WrapFunc(handler)
}

func (s adminSecurity) wrapDangerous(scope, operation string, next http.HandlerFunc) http.HandlerFunc {
	handler := httpx.WrapDangerousAdminPolicy(s.policy, s.logger, scope, operation, next)
	if s.rateLimiter == nil {
		return handler
	}
	return s.rateLimiter.WrapFunc(handler)
}

func (s adminSecurity) policy() httpx.AdminPolicy {
	cfg := config.ServiceConfig{}
	if s.configProvider != nil {
		cfg = s.configProvider()
	}
	tokens := make([]httpx.AdminToken, 0, len(cfg.Admin.Tokens))
	for _, token := range cfg.Admin.Tokens {
		tokens = append(tokens, httpx.AdminToken{
			Name:   token.Name,
			Token:  token.Token,
			Role:   token.Role,
			Scopes: append([]string(nil), token.Scopes...),
		})
	}
	return httpx.AdminPolicy{
		Token:               cfg.Admin.Token,
		RBACEnabled:         cfg.Admin.RBACEnabled,
		RequireConfirmation: cfg.Admin.DangerousConfirm,
		Tokens:              tokens,
		Audit:               s.audit,
	}
}
