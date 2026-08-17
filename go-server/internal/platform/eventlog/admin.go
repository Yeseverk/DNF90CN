package eventlog

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"longheng.io/server/internal/platform/httpx"
)

// ErrAdminMuxRequired 表示管理路由注册缺少 HTTP mux。
var ErrAdminMuxRequired = errors.New("eventlog admin mux is required")

// AdminOptions 描述 eventlog 管理接口的挂载前缀和鉴权包装器。
type AdminOptions struct {
	Prefix string
	Wrap   func(http.HandlerFunc) http.HandlerFunc
}

// RegisterAdminRoutes 把快照、死信查询和死信重投接口注册到管理 mux。
func RegisterAdminRoutes(mux *http.ServeMux, log *Log, options AdminOptions) error {
	if mux == nil {
		return ErrAdminMuxRequired
	}
	prefix := normalizeAdminPrefix(options.Prefix)
	wrap := options.Wrap
	if wrap == nil {
		wrap = func(next http.HandlerFunc) http.HandlerFunc { return next }
	}
	handler := wrap(SnapshotHandler(log))
	mux.HandleFunc(prefix, handler)
	mux.HandleFunc(prefix+"/snapshot", handler)
	mux.HandleFunc(prefix+"/deadletters", wrap(DeadLettersHandler(log)))
	mux.HandleFunc(prefix+"/deadletters/requeue", wrap(RequeueDeadLetterHandler(log)))
	return nil
}

// SnapshotHandler 返回 eventlog 当前状态快照的只读管理接口。
func SnapshotHandler(log *Log) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			httpx.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		if log == nil {
			httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": ErrStoreRequired.Error()})
			return
		}
		snapshot, err := log.Snapshot(r.Context())
		if err != nil {
			httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
			return
		}
		httpx.WriteJSON(w, http.StatusOK, snapshot)
	}
}

// DeadLettersHandler 返回死信事件列表的只读管理接口。
func DeadLettersHandler(log *Log) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			httpx.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		if log == nil {
			httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": ErrStoreRequired.Error()})
			return
		}
		limit, err := parsePositiveLimit(r.URL.Query().Get("limit"))
		if err != nil {
			httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		events, err := log.DeadLetters(r.Context(), limit)
		if err != nil {
			httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"dead_letters": events})
	}
}

// RequeueDeadLetterHandler 返回死信事件重新入队的管理接口。
func RequeueDeadLetterHandler(log *Log) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			httpx.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		if log == nil {
			httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": ErrStoreRequired.Error()})
			return
		}
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		if id == "" {
			id = strings.TrimSpace(r.URL.Query().Get("event_id"))
		}
		if id == "" {
			httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "id is required"})
			return
		}
		preserveAttempts, err := parseOptionalBool(r.URL.Query().Get("preserve_attempts"))
		if err != nil {
			httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		event, err := log.RequeueDeadLetter(r.Context(), id, RequeueOptions{PreserveAttempts: preserveAttempts})
		if err != nil {
			status := http.StatusServiceUnavailable
			if errors.Is(err, ErrEventNotFound) {
				status = http.StatusNotFound
			}
			httpx.WriteJSON(w, status, map[string]string{"error": err.Error()})
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"status": "requeued", "event": event})
	}
}

func normalizeAdminPrefix(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return "/eventlog"
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	prefix = strings.TrimRight(prefix, "/")
	if prefix == "" {
		return "/eventlog"
	}
	return prefix
}

func parsePositiveLimit(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 0 {
		return 0, ErrInvalidLimit
	}
	return limit, nil
}

func parseOptionalBool(raw string) (bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, err
	}
	return value, nil
}
