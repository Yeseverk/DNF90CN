package servergroup

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"longheng.io/server/internal/platform/httpx"
)

// AdminOptions 是服务器分组管理路由的存储、归档、延迟和包装器配置。
type AdminOptions struct {
	Prefix                 string
	Store                  Store
	Archives               MergeArchiveStore
	WarZoneInZoneDelaySecs int
	WarZoneNoticeLeadSecs  int
	Wrap                   func(string, http.HandlerFunc) http.HandlerFunc
	WrapDangerous          func(string, string, http.HandlerFunc) http.HandlerFunc
}

const maxAdminJSONBytes = 1 << 20

// RegisterAdminRoutes 注册服务器分组查询、合服、分片操作、战区和回滚管理路由。
func RegisterAdminRoutes(mux *http.ServeMux, manager *Manager, options AdminOptions) {
	if mux == nil || manager == nil {
		return
	}
	prefix := strings.TrimRight(strings.TrimSpace(options.Prefix), "/")
	if prefix == "" {
		prefix = "/debug/servergroup"
	}
	wrap := options.Wrap
	if wrap == nil {
		wrap = func(_ string, next http.HandlerFunc) http.HandlerFunc { return next }
	}
	wrapDangerous := options.WrapDangerous
	if wrapDangerous == nil {
		wrapDangerous = func(_ string, _ string, next http.HandlerFunc) http.HandlerFunc { return next }
	}
	regArchiveRoutes(mux, prefix, wrap, options.Archives)

	mux.HandleFunc(prefix, wrap("servergroup", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeMethodNA(w, http.MethodGet)
			return
		}
		snapshot := manager.Snapshot()
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"plan":      snapshot,
			"war_zones": WarZoneSnapshot(snapshot),
		})
	}))
	mux.HandleFunc(prefix+"/resolve", wrap("servergroup", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeMethodNA(w, http.MethodGet)
			return
		}
		feature := strings.TrimSpace(r.URL.Query().Get("feature"))
		shardID := strings.TrimSpace(r.URL.Query().Get("shard_id"))
		mode := strings.TrimSpace(r.URL.Query().Get("mode"))
		if mode != "" && mode != "read" && mode != "write" {
			httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "mode must be read or write"})
			return
		}
		var (
			target Target
			ok     bool
			err    error
		)
		if mode == "write" {
			target, ok, err = manager.ResolveWrite(r.Context(), feature, shardID)
		} else {
			target, ok, err = manager.Resolve(r.Context(), feature, shardID)
		}
		if err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, ErrNotFound) {
				status = http.StatusNotFound
			}
			httpx.WriteJSON(w, status, map[string]string{"error": err.Error()})
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"found": ok, "target": target})
	}))
	mux.HandleFunc(prefix+"/merge/dry-run", wrapDangerous("servergroup", httpx.AdminOperationID(http.MethodPost, prefix+"/merge/dry-run"), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNA(w, http.MethodPost)
			return
		}
		var request MergeRequest
		if err := decodeJSON(w, r, &request); err != nil {
			httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		report, err := DryRunMerge(r.Context(), manager.Snapshot(), request)
		if err != nil {
			httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		httpx.WriteJSON(w, http.StatusOK, report)
	}))
	mux.HandleFunc(prefix+"/merge/apply", wrapDangerous("servergroup", httpx.AdminOperationID(http.MethodPost, prefix+"/merge/apply"), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNA(w, http.MethodPost)
			return
		}
		var request MergeRequest
		if err := decodeJSON(w, r, &request); err != nil {
			httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		result, err := prepareMergeApply(r.Context(), manager.Snapshot(), request)
		if err != nil {
			httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if options.Store != nil {
			if err := options.Store.Save(r.Context(), result.DryRun.Next); err != nil {
				httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
				return
			}
		}
		if err := manager.Replace(context.Background(), result.DryRun.Next); err != nil {
			httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
			return
		}
		httpx.WriteJSON(w, http.StatusOK, result)
	}))
	mux.HandleFunc(prefix+"/shard/operation/dry-run", wrapDangerous("servergroup", httpx.AdminOperationID(http.MethodPost, prefix+"/shard/operation/dry-run"), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNA(w, http.MethodPost)
			return
		}
		var request ShardOperationRequest
		if err := decodeJSON(w, r, &request); err != nil {
			httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		report, err := DryRunShardOperation(r.Context(), manager.Snapshot(), request)
		if err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, ErrNotFound) {
				status = http.StatusNotFound
			}
			httpx.WriteJSON(w, status, map[string]string{"error": err.Error()})
			return
		}
		httpx.WriteJSON(w, http.StatusOK, report)
	}))
	mux.HandleFunc(prefix+"/shard/operation/apply", wrapDangerous("servergroup", httpx.AdminOperationID(http.MethodPost, prefix+"/shard/operation/apply"), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNA(w, http.MethodPost)
			return
		}
		var request ShardOperationRequest
		if err := decodeJSON(w, r, &request); err != nil {
			httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		result, err := DryRunShardOperation(r.Context(), manager.Snapshot(), request)
		if err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, ErrNotFound) {
				status = http.StatusNotFound
			}
			httpx.WriteJSON(w, status, map[string]string{"error": err.Error()})
			return
		}
		if options.Store != nil {
			if err := options.Store.Save(r.Context(), result.Next); err != nil {
				httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
				return
			}
		}
		if err := manager.Replace(context.Background(), result.Next); err != nil {
			httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
			return
		}
		httpx.WriteJSON(w, http.StatusOK, result)
	}))
	mux.HandleFunc(prefix+"/warzone/refresh", wrapDangerous("servergroup", httpx.AdminOperationID(http.MethodPost, prefix+"/warzone/refresh"), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNA(w, http.MethodPost)
			return
		}
		var request WarZoneRefreshRequest
		if err := decodeJSON(w, r, &request); err != nil {
			httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		request = applyWarZoneDefaults(request, options)
		apply := strings.EqualFold(r.URL.Query().Get("apply"), "true")
		if !apply {
			report, err := DryRunWarZoneRefresh(r.Context(), manager.Snapshot(), request)
			if err != nil {
				httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			httpx.WriteJSON(w, http.StatusOK, report)
			return
		}
		report, err := DryRunWarZoneRefresh(r.Context(), manager.Snapshot(), request)
		if err != nil {
			httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if options.Store != nil {
			if err := options.Store.Save(r.Context(), report.Next); err != nil {
				httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
				return
			}
		}
		if err := manager.Replace(context.Background(), report.Next); err != nil {
			httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
			return
		}
		httpx.WriteJSON(w, http.StatusOK, report)
	}))
	mux.HandleFunc(prefix+"/rollback/apply", wrapDangerous("servergroup", httpx.AdminOperationID(http.MethodPost, prefix+"/rollback/apply"), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNA(w, http.MethodPost)
			return
		}
		var request RollbackApplyRequest
		if err := decodeJSON(w, r, &request); err != nil {
			httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		result, err := PrepareRollbackApply(r.Context(), manager.Snapshot(), request)
		if err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, ErrRollbackVersionMismatch) {
				status = http.StatusConflict
			}
			httpx.WriteJSON(w, status, map[string]string{"error": err.Error()})
			return
		}
		if options.Store != nil {
			if err := options.Store.Save(r.Context(), result.Restored); err != nil {
				httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
				return
			}
		}
		if err := manager.Replace(context.Background(), result.Restored); err != nil {
			httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
			return
		}
		result.Applied = true
		httpx.WriteJSON(w, http.StatusOK, result)
	}))
	mux.HandleFunc(prefix+"/save", wrapDangerous("servergroup", httpx.AdminOperationID(http.MethodPost, prefix+"/save"), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNA(w, http.MethodPost)
			return
		}
		if options.Store == nil {
			httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": ErrStoreEmpty.Error()})
			return
		}
		if err := options.Store.Save(r.Context(), manager.Snapshot()); err != nil {
			httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "saved"})
	}))
}

func applyWarZoneDefaults(request WarZoneRefreshRequest, options AdminOptions) WarZoneRefreshRequest {
	if request.InZoneDelaySeconds <= 0 && options.WarZoneInZoneDelaySecs > 0 {
		request.InZoneDelaySeconds = options.WarZoneInZoneDelaySecs
	}
	if request.NoticeLeadSeconds <= 0 && options.WarZoneNoticeLeadSecs > 0 {
		request.NoticeLeadSeconds = options.WarZoneNoticeLeadSecs
	}
	return request
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	return httpx.DecodeStrictJSON(w, r, maxAdminJSONBytes, target)
}

func writeMethodNA(w http.ResponseWriter, allowed string) {
	if allowed == http.MethodGet {
		allowed = http.MethodGet + ", " + http.MethodHead
	}
	w.Header().Set("Allow", allowed)
	httpx.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
}
