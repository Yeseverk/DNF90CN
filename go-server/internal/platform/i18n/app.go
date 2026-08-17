package i18n

import (
	"context"
	"fmt"
	"net/http"

	"longheng.io/server/internal/platform/app"
	"longheng.io/server/internal/platform/httpx"
)

func ConfigureApp(env *app.Env) error {
	if env == nil {
		return fmt.Errorf("i18n app env is nil")
	}
	catalog := NewCatalog(env.Config.I18N.DefaultLanguage, env.Config.I18N.Version)
	if env.Config.I18N.Enabled {
		if err := catalog.LoadDir(context.Background(), env.Config.I18N.Directory); err != nil {
			return fmt.Errorf("load i18n catalog: %w", err)
		}
		if !catalog.HasLanguage(env.Config.I18N.DefaultLanguage) {
			return fmt.Errorf("load i18n catalog: default language %q is not loaded", env.Config.I18N.DefaultLanguage)
		}
	}
	env.I18N = catalog
	registerAdminRoute(env, catalog)
	return nil
}

func registerAdminRoute(env *app.Env, catalog *Catalog) {
	if env.AdminMux == nil {
		return
	}
	wrap := func(next http.HandlerFunc) http.HandlerFunc {
		if env.AdminAuth != nil {
			return env.AdminAuth("runtime-i18n", next)
		}
		return httpx.WrapAdmin(env.Config.Admin.Token, env.Logger, "runtime-i18n", next)
	}
	handler := wrap(func(w http.ResponseWriter, _ *http.Request) {
		cfg := env.Config
		if env.ConfigProvider != nil {
			cfg = env.ConfigProvider()
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"enabled": cfg.I18N.Enabled,
			"catalog": catalog.Snapshot(),
		})
	})
	if env.AdminAuth == nil && env.RateLimiter != nil {
		handler = env.RateLimiter.WrapFunc(handler)
	}
	env.AdminMux.HandleFunc("/debug/i18n", handler)
}
