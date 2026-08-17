// Package dnf90 contains the single composition root for the DNF90 server.
package dnf90

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"

	appkit "longheng.io/server/internal/platform/app"
	"longheng.io/server/internal/services/dnfbridge"
	logicdnf "longheng.io/server/internal/services/logic/dnf"
)

const (
	ServiceName       = "dnfbridge"
	DefaultConfigPath = "configs/dnfbridge.toml"
	DefaultLogicPath  = "configs/dnf/logic.toml"
)

// Components wires storage before the protocol service.
func Components(env *appkit.Env) ([]appkit.Component, error) {
	return components(env, nil)
}

// ComponentsWithShutdown adds a loopback admin endpoint that cancels the
// application context, allowing the Windows launcher to stop cleanly.
func ComponentsWithShutdown(cancel context.CancelFunc) appkit.Builder {
	return func(env *appkit.Env) ([]appkit.Component, error) {
		return components(env, cancel)
	}
}

func components(env *appkit.Env, cancel context.CancelFunc) ([]appkit.Component, error) {
	if cancel != nil && env != nil && env.AdminMux != nil {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				w.Header().Set("Allow", http.MethodPost)
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte("shutdown requested\n"))
			go cancel()
		})
		if env.AdminAuth != nil {
			handler = env.AdminAuth("platform", handler)
		}
		env.AdminMux.HandleFunc("/local/shutdown", handler)
	}

	configPath := strings.TrimSpace(os.Getenv(logicdnf.EnvConfigPath))
	if configPath == "" {
		configPath = DefaultLogicPath
	}
	cfg, err := logicdnf.LoadConfigForEnv(configPath, env)
	if err != nil {
		return nil, err
	}
	runtime, err := logicdnf.NewRuntime(env, cfg)
	if err != nil {
		return nil, err
	}
	bridge := dnfbridge.NewWithOptions(env, dnfbridge.ServiceOptions{
		RepositoryProvider: runtime.Group,
	})
	if env != nil && env.AdminMux != nil {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				w.Header().Set("Allow", http.MethodPost)
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			var request struct {
				PID       uint32 `json:"pid"`
				AccountID string `json:"account_id"`
			}
			decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&request); err != nil {
				http.Error(w, "invalid request", http.StatusBadRequest)
				return
			}
			if err := bridge.RegisterClientAccount(request.PID, request.AccountID); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
		if env.AdminAuth != nil {
			handler = env.AdminAuth("platform", handler)
		}
		env.AdminMux.HandleFunc("/local/client-account", handler)
	}
	components := make([]appkit.Component, 0, 2)
	if runtime.Repository != nil {
		components = append(components, runtime.Repository)
	}
	components = append(components, bridge)
	return components, nil
}
