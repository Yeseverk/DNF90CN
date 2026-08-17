package moderation

import (
	"errors"
	"net/http"
	"strings"

	"longheng.io/server/internal/platform/httpx"
)

var ErrAdminMuxRequired = errors.New("moderation admin mux is required")

const maxAdminJSONBytes = 1 << 20

type AdminOptions struct {
	Prefix string
	Wrap   func(http.HandlerFunc) http.HandlerFunc
}

func RegisterAdminRoutes(mux *http.ServeMux, engine *Engine, options AdminOptions) error {
	return RegisterAdminRoutesWithEvaluator(mux, engine, engine, options)
}

func RegisterAdminRoutesWithEvaluator(mux *http.ServeMux, engine *Engine, evaluator Evaluator, options AdminOptions) error {
	if mux == nil {
		return ErrAdminMuxRequired
	}
	prefix := normalizeAdminPrefix(options.Prefix)
	wrap := options.Wrap
	if wrap == nil {
		wrap = func(next http.HandlerFunc) http.HandlerFunc { return next }
	}
	mux.HandleFunc(prefix, wrap(SnapshotHandler(engine)))
	mux.HandleFunc(prefix+"/snapshot", wrap(SnapshotHandler(engine)))
	mux.HandleFunc(prefix+"/evaluate", wrap(EvaluateWithHandler(evaluator)))
	mux.HandleFunc(prefix+"/sanctions", wrap(SanctionsHandler(engine)))
	return nil
}

func SnapshotHandler(engine *Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			httpx.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		if engine == nil {
			httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "moderation engine is required"})
			return
		}
		var filter FilterSnapshot
		if engine.Filter() != nil {
			filter = engine.Filter().Snapshot()
		}
		var sanctions SanctionSnapshot
		var err error
		if store := engine.Sanctions(); store != nil {
			sanctions, err = store.Snapshot(r.Context())
			if err != nil {
				httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
				return
			}
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"filter":    filter,
			"sanctions": sanctions,
		})
	}
}

func EvaluateHandler(engine *Engine) http.HandlerFunc {
	return EvaluateWithHandler(engine)
}

func EvaluateWithHandler(evaluator Evaluator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			httpx.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		if evaluator == nil {
			httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "moderation evaluator is required"})
			return
		}
		var request Request
		if err := decodeModAdminJSON(w, r, &request); err != nil {
			httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		decision, err := evaluator.Evaluate(r.Context(), request)
		if err != nil {
			httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		httpx.WriteJSON(w, http.StatusOK, decision)
	}
}

func SanctionsHandler(engine *Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if engine == nil || engine.Sanctions() == nil {
			httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "moderation sanction store is required"})
			return
		}
		switch r.Method {
		case http.MethodGet:
			handleGetSanctions(w, r, engine.Sanctions())
		case http.MethodPost:
			handlePostSanction(w, r, engine.Sanctions())
		case http.MethodDelete:
			handleDeleteSanction(w, r, engine.Sanctions())
		default:
			w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPost, http.MethodDelete}, ", "))
			httpx.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
	}
}

func handleGetSanctions(w http.ResponseWriter, r *http.Request, store SanctionStore) {
	subject := strings.TrimSpace(r.URL.Query().Get("subject"))
	if subject != "" {
		items, err := store.Active(r.Context(), SanctionQuery{
			Subject: subject,
			Scope:   r.URL.Query().Get("scope"),
		})
		if err != nil {
			httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
		return
	}
	snapshot, err := store.Snapshot(r.Context())
	if err != nil {
		httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, snapshot)
}

func handlePostSanction(w http.ResponseWriter, r *http.Request, store SanctionStore) {
	var sanction Sanction
	if err := decodeModAdminJSON(w, r, &sanction); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	item, err := store.Upsert(r.Context(), sanction)
	if err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"sanction": item})
}

// decodeModAdminJSON 统一限制管理端 JSON 的大小、字段和单文档格式。
func decodeModAdminJSON(w http.ResponseWriter, r *http.Request, out any) error {
	return httpx.DecodeStrictJSON(w, r, maxAdminJSONBytes, out)
}

func handleDeleteSanction(w http.ResponseWriter, r *http.Request, store SanctionStore) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "id is required"})
		return
	}
	if err := store.Remove(r.Context(), id); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, ErrSanctionMissing) {
			status = http.StatusNotFound
		}
		httpx.WriteJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func normalizeAdminPrefix(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return "/debug/moderation"
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	prefix = strings.TrimRight(prefix, "/")
	if prefix == "" {
		return "/debug/moderation"
	}
	return prefix
}
