package onlinepush

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"longheng.io/server/internal/platform/httpx"
)

// AdminOptions 控制在线推送管理路由的路径和鉴权包装。
type AdminOptions struct {
	Prefix        string
	Wrap          func(http.HandlerFunc) http.HandlerFunc
	WrapDangerous func(operation string, next http.HandlerFunc) http.HandlerFunc
}

const maxAdminJSONBytes = 1 << 20

// RegisterAdminRoutes 注册在线推送管理路由。
func RegisterAdminRoutes(mux *http.ServeMux, service *Service, options AdminOptions) error {
	if mux == nil {
		return errors.New("online push admin mux is required")
	}
	if service == nil {
		return errors.New("online push service is required")
	}
	prefix := strings.TrimRight(strings.TrimSpace(options.Prefix), "/")
	if prefix == "" {
		prefix = "/onlinepush"
	}
	wrap := options.Wrap
	if wrap == nil {
		wrap = func(next http.HandlerFunc) http.HandlerFunc { return next }
	}
	wrapDangerous := options.WrapDangerous
	if wrapDangerous == nil {
		wrapDangerous = func(_ string, next http.HandlerFunc) http.HandlerFunc { return wrap(next) }
	}
	mux.HandleFunc(prefix, wrap(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			httpx.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		httpx.WriteJSON(w, http.StatusOK, service.Snapshot(r.Context()))
	}))
	mux.HandleFunc(prefix+"/send", wrapDangerous("onlinepush.send", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			httpx.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		var request Request
		if err := httpx.DecodeStrictJSON(w, r, maxAdminJSONBytes, &request); err != nil {
			httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		receipt, err := service.Send(r.Context(), request)
		if err != nil {
			httpx.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error(), "receipt": receipt})
			return
		}
		httpx.WriteJSON(w, http.StatusOK, receipt)
	}))
	mux.HandleFunc(prefix+"/offline", wrap(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
			items, err := service.Offline(r.Context(), r.URL.Query().Get("account_id"), limit)
			if err != nil {
				httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
		case http.MethodDelete:
			if err := service.DeleteOffline(r.Context(), r.URL.Query().Get("id")); err != nil {
				status := http.StatusBadRequest
				if errors.Is(err, ErrOfflineNotFound) {
					status = http.StatusNotFound
				}
				httpx.WriteJSON(w, status, map[string]string{"error": err.Error()})
				return
			}
			httpx.WriteJSON(w, http.StatusOK, map[string]any{"deleted": true})
		default:
			w.Header().Set("Allow", "GET, DELETE")
			httpx.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
	}))
	return nil
}
