package storageobject

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"longheng.io/server/internal/platform/httpx"
)

type AdminOptions struct {
	Prefix        string
	Wrap          func(http.HandlerFunc) http.HandlerFunc
	WrapDangerous func(operation string, next http.HandlerFunc) http.HandlerFunc
}

const maxAdminJSONBytes = 1 << 20

type AdminObjectView struct {
	Object    Object `json:"object"`
	OwnerKind string `json:"owner_kind"`
}

func RegisterAdminRoutes(mux *http.ServeMux, service *Service, options AdminOptions) error {
	if mux == nil {
		return errors.New("storage object admin mux is required")
	}
	if service == nil {
		return errors.New("storage object admin service is required")
	}
	prefix := strings.TrimRight(strings.TrimSpace(options.Prefix), "/")
	if prefix == "" {
		prefix = "/storage"
	}
	wrap := options.Wrap
	if wrap == nil {
		wrap = func(next http.HandlerFunc) http.HandlerFunc { return next }
	}
	wrapDangerous := options.WrapDangerous
	if wrapDangerous == nil {
		wrapDangerous = func(_ string, next http.HandlerFunc) http.HandlerFunc { return wrap(next) }
	}
	mux.HandleFunc(prefix+"/objects", wrap(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			httpx.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		result, err := service.List(r.Context(), Subject{Admin: true}, ListRequest{
			Collection: r.URL.Query().Get("collection"),
			UserID:     r.URL.Query().Get("user_id"),
			Index:      adminIndexFromQuery(r),
			Limit:      limit,
			Cursor:     r.URL.Query().Get("cursor"),
		})
		if err != nil {
			httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		owner := strings.TrimSpace(r.URL.Query().Get("owner"))
		views := make([]AdminObjectView, 0, len(result.Objects))
		for _, object := range result.Objects {
			if owner != "" && object.OwnerKind() != owner {
				continue
			}
			views = append(views, AdminObjectView{Object: object, OwnerKind: object.OwnerKind()})
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"objects":     views,
			"next_cursor": result.NextCursor,
		})
	}))
	readObject := wrap(func(w http.ResponseWriter, r *http.Request) {
		object, ok, err := service.Read(r.Context(), Subject{Admin: true}, adminKeyFromQuery(r))
		if err != nil {
			httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if !ok {
			httpx.WriteJSON(w, http.StatusNotFound, map[string]string{"error": ErrObjectNotFound.Error()})
			return
		}
		httpx.WriteJSON(w, http.StatusOK, AdminObjectView{Object: object, OwnerKind: object.OwnerKind()})
	})
	writeOrDeleteObject := wrapDangerous("storageobject.object.write_delete", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			var request WriteRequest
			if !decodeAdminJSON(w, r, &request) {
				return
			}
			object, err := service.Write(r.Context(), Subject{Admin: true}, request)
			if err != nil {
				httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			httpx.WriteJSON(w, http.StatusOK, AdminObjectView{Object: object, OwnerKind: object.OwnerKind()})
		case http.MethodDelete:
			if err := service.Delete(r.Context(), Subject{Admin: true}, adminKeyFromQuery(r), r.URL.Query().Get("version")); err != nil {
				status := http.StatusBadRequest
				if errors.Is(err, ErrObjectNotFound) {
					status = http.StatusNotFound
				}
				httpx.WriteJSON(w, status, map[string]string{"error": err.Error()})
				return
			}
			httpx.WriteJSON(w, http.StatusOK, map[string]any{"deleted": true})
		default:
			w.Header().Set("Allow", "GET, POST, DELETE")
			httpx.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
	})
	readBatch := wrap(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			httpx.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		var request BatchReadRequest
		if !decodeAdminJSON(w, r, &request) {
			return
		}
		httpx.WriteJSON(w, http.StatusOK, service.BatchRead(r.Context(), Subject{Admin: true}, request))
	})
	writeBatch := wrapDangerous("storageobject.objects.write", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			httpx.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		var request BatchWriteRequest
		if !decodeAdminJSON(w, r, &request) {
			return
		}
		httpx.WriteJSON(w, http.StatusOK, service.BatchWrite(r.Context(), Subject{Admin: true}, request))
	})
	deleteBatch := wrapDangerous("storageobject.objects.delete", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			httpx.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		var request BatchDeleteRequest
		if !decodeAdminJSON(w, r, &request) {
			return
		}
		httpx.WriteJSON(w, http.StatusOK, service.BatchDelete(r.Context(), Subject{Admin: true}, request))
	})
	mux.HandleFunc(prefix+"/objects/read", readBatch)
	mux.HandleFunc(prefix+"/objects/write", writeBatch)
	mux.HandleFunc(prefix+"/objects/delete", deleteBatch)
	mux.HandleFunc(prefix+"/object", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			readObject(w, r)
		case http.MethodPost, http.MethodDelete:
			writeOrDeleteObject(w, r)
		default:
			w.Header().Set("Allow", "GET, POST, DELETE")
			httpx.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
	})
	return nil
}

func decodeAdminJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	if err := httpx.DecodeStrictJSON(w, r, maxAdminJSONBytes, out); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return false
	}
	return true
}

func adminKeyFromQuery(r *http.Request) Key {
	query := r.URL.Query()
	return Key{
		Collection: query.Get("collection"),
		Key:        query.Get("key"),
		UserID:     query.Get("user_id"),
	}
}

func adminIndexFromQuery(r *http.Request) map[string]string {
	index := make(map[string]string)
	for key, values := range r.URL.Query() {
		switch {
		case strings.HasPrefix(key, "index."):
			name := strings.TrimSpace(strings.TrimPrefix(key, "index."))
			if name != "" && len(values) > 0 {
				index[name] = values[0]
			}
		case strings.HasPrefix(key, "idx_"):
			name := strings.TrimSpace(strings.TrimPrefix(key, "idx_"))
			if name != "" && len(values) > 0 {
				index[name] = values[0]
			}
		}
	}
	return normalizeIndex(index)
}
