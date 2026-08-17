package accountcenter

import (
	"errors"
	"net/http"
	"strings"

	"longheng.io/server/internal/platform/httpx"
)

const maxAdminJSONBytes = 1 << 20

type AdminOptions struct {
	Prefix        string
	Wrap          func(http.HandlerFunc) http.HandlerFunc
	WrapDangerous func(operation string, next http.HandlerFunc) http.HandlerFunc
}

type AdminAccountView struct {
	Account Account       `json:"account"`
	Access  AccountAccess `json:"access"`
}

func RegisterAdminRoutes(mux *http.ServeMux, center *Center, options AdminOptions) error {
	if mux == nil {
		return errors.New("account center admin mux is required")
	}
	if center == nil {
		return errors.New("account center is required")
	}
	prefix := strings.TrimRight(strings.TrimSpace(options.Prefix), "/")
	if prefix == "" {
		prefix = "/accountcenter"
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
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"snapshot": center.Snapshot(),
			"state":    center.Export(),
		})
	}))
	mux.HandleFunc(prefix+"/account", wrap(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			httpx.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		account, ok, err := lookupAdminAccount(r, center)
		if writeAdminError(w, err) {
			return
		}
		if !ok {
			httpx.WriteJSON(w, http.StatusNotFound, map[string]string{"error": ErrAccountNotFound.Error()})
			return
		}
		access, err := center.AccountAccess(r.Context(), account.ID)
		if writeAdminError(w, err) {
			return
		}
		httpx.WriteJSON(w, http.StatusOK, AdminAccountView{Account: account, Access: access})
	}))
	mux.HandleFunc(prefix+"/login", wrapDangerous("accountcenter.login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			httpx.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		var request LoginRequest
		if !decodeAdminJSON(w, r, &request) {
			return
		}
		result, err := center.Login(r.Context(), request)
		if writeAdminError(w, err) {
			return
		}
		httpx.WriteJSON(w, http.StatusOK, result)
	}))
	mux.HandleFunc(prefix+"/bind", wrapDangerous("accountcenter.bind", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			httpx.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		var request BindRequest
		if !decodeAdminJSON(w, r, &request) {
			return
		}
		account, err := center.Bind(r.Context(), request)
		if writeAdminError(w, err) {
			return
		}
		httpx.WriteJSON(w, http.StatusOK, account)
	}))
	mux.HandleFunc(prefix+"/ban", wrapDangerous("accountcenter.ban", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			httpx.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		var record BanRecord
		if !decodeAdminJSON(w, r, &record) {
			return
		}
		if writeAdminError(w, center.SetBan(r.Context(), record)) {
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"saved": true})
	}))
	mux.HandleFunc(prefix+"/allow", wrapDangerous("accountcenter.allow", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			httpx.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		var record AllowRecord
		if !decodeAdminJSON(w, r, &record) {
			return
		}
		if writeAdminError(w, center.SetAllow(r.Context(), record)) {
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"saved": true})
	}))
	mux.HandleFunc(prefix+"/shards", wrapDangerous("accountcenter.shards", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			httpx.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		var request struct {
			Shards []Shard `json:"shards"`
		}
		if !decodeAdminJSON(w, r, &request) {
			return
		}
		if writeAdminError(w, center.SetShards(r.Context(), request.Shards)) {
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"saved": len(request.Shards)})
	}))
	mux.HandleFunc(prefix+"/gates", wrapDangerous("accountcenter.gates", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			httpx.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		var request struct {
			Gates []GateNode `json:"gates"`
		}
		if !decodeAdminJSON(w, r, &request) {
			return
		}
		if writeAdminError(w, center.SetGates(r.Context(), request.Gates)) {
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"saved": len(request.Gates)})
	}))
	return nil
}

func lookupAdminAccount(r *http.Request, center *Center) (Account, bool, error) {
	query := r.URL.Query()
	if accountID := strings.TrimSpace(query.Get("account_id")); accountID != "" {
		return center.GetAccount(r.Context(), accountID)
	}
	return center.FindAccountByIdentity(r.Context(), Identity{
		Kind:     query.Get("kind"),
		Issuer:   query.Get("issuer"),
		Subject:  query.Get("subject"),
		Email:    query.Get("email"),
		Channel:  query.Get("channel"),
		DeviceID: query.Get("device_id"),
	})
}

func decodeAdminJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	if err := httpx.DecodeStrictJSON(w, r, maxAdminJSONBytes, out); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return false
	}
	return true
}

func writeAdminError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	status := http.StatusBadRequest
	if errors.Is(err, ErrAccountBanned) || errors.Is(err, ErrAllowListRequired) || errors.Is(err, ErrShardClosed) {
		status = http.StatusForbidden
	}
	if errors.Is(err, ErrShardNotFound) || errors.Is(err, ErrGateUnavailable) || errors.Is(err, ErrAccountNotFound) {
		status = http.StatusNotFound
	}
	httpx.WriteJSON(w, status, map[string]string{"error": err.Error()})
	return true
}
