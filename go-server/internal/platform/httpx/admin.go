package httpx

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"longheng.io/server/internal/platform/admincmd"
	"longheng.io/server/internal/platform/audit"
)

const AdminTokenHeader = "X-Admin-Token"
const AdminActorHeader = "X-Admin-Actor"
const AdminConfirmHeader = "X-Admin-Confirm"
const AdminTraceHeader = "X-Admin-Trace-ID"
const AdminIdempotencyHeader = "X-Admin-Idempotency-Key"
const AdminOperationHeader = "X-Admin-Operation-ID"
const AdminReceiptHeader = "X-Admin-Receipt-ID"

type AdminToken struct {
	Name   string
	Token  string
	Role   string
	Scopes []string
}

type AdminPolicy struct {
	Token               string
	RBACEnabled         bool
	RequireConfirmation bool
	Tokens              []AdminToken
	Audit               *audit.Logger
}

type adminPrincipal struct {
	Name   string
	Role   string
	Scopes []string
	Root   bool
}

type adminPrincipalKey struct{}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func WrapAdmin(token string, logger *slog.Logger, scope string, next http.HandlerFunc) http.HandlerFunc {
	return wrapAdminPolicy(func() AdminPolicy {
		return AdminPolicy{Token: token}
	}, logger, scope, "", false, next)
}

func WrapAdminPolicy(policyProvider func() AdminPolicy, logger *slog.Logger, scope string, next http.HandlerFunc) http.HandlerFunc {
	return wrapAdminPolicy(policyProvider, logger, scope, "", false, next)
}

func WrapDangerousAdminPolicy(policyProvider func() AdminPolicy, logger *slog.Logger, scope, operation string, next http.HandlerFunc) http.HandlerFunc {
	return wrapAdminPolicy(policyProvider, logger, scope, operation, true, next)
}

func wrapAdminPolicy(policyProvider func() AdminPolicy, logger *slog.Logger, scope, operation string, dangerous bool, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		traceID := adminTraceID(r)
		w.Header().Set(AdminTraceHeader, traceID)
		writer := &statusWriter{
			ResponseWriter: w,
			status:         http.StatusOK,
		}
		policy := AdminPolicy{}
		if policyProvider != nil {
			policy = policyProvider()
		}
		principal := adminPrincipal{Name: "unauthenticated", Role: "none"}
		requiredScope := adminRequiredScope(scope, r.Method)
		operationID := strings.TrimSpace(operation)
		if operationID == "" {
			operationID = AdminOperationID(r.Method, r.URL.Path)
		}
		w.Header().Set(AdminOperationHeader, operationID)
		receipt := admincmd.Receipt{}
		defer func() {
			if rec := recover(); rec != nil {
				if logger != nil {
					logger.Error("admin handler panic recovered", "scope", scope, "path", r.URL.Path, "trace_id", traceID, "panic", rec)
				}
				writer.status = http.StatusInternalServerError
				WriteJSON(writer, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			}
			recordAdminAudit(r, policy, principal, scope, requiredScope, operationID, receipt, traceID, writer.status, time.Since(start))
			if logger != nil {
				logger.Info(
					"admin request",
					"scope", scope,
					"actor", principal.Name,
					"role", principal.Role,
					"method", r.Method,
					"path", r.URL.Path,
					"status", writer.status,
					"trace_id", traceID,
					"duration_ms", fmt.Sprintf("%.3f", float64(time.Since(start).Microseconds())/1000.0),
				)
			}
		}()

		if !adminPolicyOK(policy) {
			writer.status = http.StatusServiceUnavailable
			WriteJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "admin token not configured"})
			return
		}

		var ok bool
		principal, ok = authenticateAdmin(policy, r.Header.Get(AdminTokenHeader), r.Header.Get(AdminActorHeader))
		if !ok {
			writer.status = http.StatusUnauthorized
			WriteJSON(writer, http.StatusUnauthorized, map[string]string{"error": "invalid admin token"})
			return
		}
		if policy.RBACEnabled && !principalAllows(principal, scope, requiredScope) {
			writer.status = http.StatusForbidden
			WriteJSON(writer, http.StatusForbidden, map[string]string{
				"error":          "admin scope forbidden",
				"required_scope": requiredScope,
			})
			return
		}
		if policy.RequireConfirmation && adminMethodMutates(r.Method) {
			if r.Header.Get(AdminConfirmHeader) != operationID {
				writer.status = http.StatusPreconditionRequired
				WriteJSON(writer, http.StatusPreconditionRequired, map[string]string{
					"error":        "dangerous admin operation requires confirmation",
					"header":       AdminConfirmHeader,
					"operation_id": operationID,
				})
				return
			}
		}
		if dangerous && adminMethodMutates(r.Method) && strings.TrimSpace(r.Header.Get(AdminIdempotencyHeader)) == "" {
			writer.status = http.StatusPreconditionRequired
			WriteJSON(writer, http.StatusPreconditionRequired, map[string]string{
				"error":        "dangerous admin operation requires idempotency key",
				"header":       AdminIdempotencyHeader,
				"operation_id": operationID,
			})
			return
		}
		if adminMethodMutates(r.Method) {
			receipt = adminCommandReceipt(r, principal, scope, operationID, start)
			if receipt.ID != "" {
				w.Header().Set(AdminReceiptHeader, receipt.ID)
			}
		}

		r = r.WithContext(context.WithValue(r.Context(), adminPrincipalKey{}, principal.Name))
		next(writer, r)
	}
}

func adminPolicyOK(policy AdminPolicy) bool {
	if strings.TrimSpace(policy.Token) != "" {
		return true
	}
	for _, token := range policy.Tokens {
		if strings.TrimSpace(token.Token) != "" {
			return true
		}
	}
	return false
}

func authenticateAdmin(policy AdminPolicy, headerToken, actorHeader string) (adminPrincipal, bool) {
	headerToken = strings.TrimSpace(headerToken)
	if headerToken == "" {
		return adminPrincipal{}, false
	}
	if !policy.RBACEnabled && policy.Token != "" && subtle.ConstantTimeCompare([]byte(headerToken), []byte(policy.Token)) == 1 {
		name := strings.TrimSpace(actorHeader)
		if name == "" {
			name = "root"
		}
		return adminPrincipal{Name: name, Role: "admin", Scopes: []string{"*"}, Root: true}, true
	}
	if !policy.RBACEnabled {
		return adminPrincipal{}, false
	}
	for _, token := range policy.Tokens {
		if token.Token == "" || subtle.ConstantTimeCompare([]byte(headerToken), []byte(token.Token)) != 1 {
			continue
		}
		// RBAC token 的名称就是已认证身份，不能再由请求头覆盖，否则审计和
		// 持久化管理命令的 actor 都可被 token 持有者任意伪造。
		name := strings.TrimSpace(token.Name)
		role := strings.ToLower(strings.TrimSpace(token.Role))
		if role == "" {
			role = "operator"
		}
		return adminPrincipal{
			Name:   name,
			Role:   role,
			Scopes: append([]string(nil), token.Scopes...),
		}, true
	}
	return adminPrincipal{}, false
}

// AuthenticatedAdminActor 返回管理鉴权绑定的 actor。经过 WrapAdminPolicy 的
// RBAC 请求始终取 token.Name；未经过 wrapper 的内部测试和非 RBAC 兼容入口才回退请求头。
func AuthenticatedAdminActor(r *http.Request) string {
	if r == nil {
		return ""
	}
	if actor, ok := r.Context().Value(adminPrincipalKey{}).(string); ok {
		return strings.TrimSpace(actor)
	}
	return strings.TrimSpace(r.Header.Get(AdminActorHeader))
}

func principalAllows(principal adminPrincipal, scope, requiredScope string) bool {
	if principal.Root || principal.Role == "admin" {
		return true
	}
	if len(principal.Scopes) > 0 {
		return scopesAllow(principal.Scopes, scope, requiredScope)
	}
	switch principal.Role {
	case "operator":
		return true
	case "viewer", "auditor":
		return strings.HasSuffix(requiredScope, ":read")
	default:
		return false
	}
}

func scopesAllow(scopes []string, scope, requiredScope string) bool {
	scope = strings.ToLower(strings.TrimSpace(scope))
	requiredScope = strings.ToLower(strings.TrimSpace(requiredScope))
	for _, item := range scopes {
		item = strings.ToLower(strings.TrimSpace(item))
		switch item {
		case "*", requiredScope, scope + ":*":
			return true
		}
		if strings.HasSuffix(requiredScope, ":read") && item == "*:read" {
			return true
		}
		if !strings.HasSuffix(requiredScope, ":read") && item == "*:write" {
			return true
		}
	}
	return false
}

func adminRequiredScope(scope, method string) string {
	scope = strings.Trim(strings.ToLower(strings.TrimSpace(scope)), ":")
	if scope == "" {
		scope = "admin"
	}
	if adminMethodMutates(method) {
		return scope + ":write"
	}
	return scope + ":read"
}

func adminMethodMutates(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func AdminOperationID(method, path string) string {
	method = strings.ToUpper(strings.TrimSpace(method))
	path = strings.TrimSpace(path)
	if method == "" {
		method = "GET"
	}
	if path == "" {
		path = "/"
	}
	return method + " " + path
}

func adminTraceID(r *http.Request) string {
	if r != nil {
		if traceID := strings.TrimSpace(r.Header.Get(AdminTraceHeader)); traceID != "" {
			return traceID
		}
		if requestID := strings.TrimSpace(r.Header.Get("X-Request-ID")); requestID != "" {
			return requestID
		}
	}
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("admin-%d", time.Now().UTC().UnixNano())
	}
	return "admin-" + hex.EncodeToString(raw[:])
}

func adminCommandReceipt(r *http.Request, principal adminPrincipal, scope, operationID string, now time.Time) admincmd.Receipt {
	if r == nil {
		return admincmd.Receipt{}
	}
	receipt, err := admincmd.NewReceipt(admincmd.Command{
		Operation:      operationID,
		Scope:          scope,
		Target:         r.URL.Path,
		Actor:          principal.Name,
		IdempotencyKey: r.Header.Get(AdminIdempotencyHeader),
		Confirmation:   r.Header.Get(AdminConfirmHeader),
		Params: map[string]any{
			"method": r.Method,
			"query":  redactedAdminQuery(r.URL.RawQuery),
		},
	}, "accepted", now)
	if err != nil {
		return admincmd.Receipt{}
	}
	return receipt
}

func recordAdminAudit(r *http.Request, policy AdminPolicy, principal adminPrincipal, scope, requiredScope, operationID string, receipt admincmd.Receipt, traceID string, status int, duration time.Duration) {
	if policy.Audit == nil {
		return
	}
	fields := map[string]string{
		"method":         r.Method,
		"path":           r.URL.Path,
		"scope":          scope,
		"required_scope": requiredScope,
		"operation_id":   operationID,
		"status":         fmt.Sprint(status),
		"duration_ms":    fmt.Sprintf("%.3f", float64(duration.Microseconds())/1000.0),
		"role":           principal.Role,
	}
	if receipt.ID != "" {
		fields["receipt_id"] = receipt.ID
		fields["receipt_status"] = receipt.Status
	}
	if receipt.ParamsHash != "" {
		fields["params_hash"] = receipt.ParamsHash
	}
	if receipt.IdempotencyKey != "" {
		fields["idempotency_key"] = receipt.IdempotencyKey
	}
	if r.URL.RawQuery != "" {
		fields["query"] = redactedAdminQuery(r.URL.RawQuery)
	}
	_ = policy.Audit.Record(r.Context(), audit.Event{
		Actor:   principal.Name,
		Action:  "admin.request",
		Target:  r.URL.Path,
		TraceID: traceID,
		Fields:  fields,
	})
}

// redactedAdminQuery 在查询参数进入审计、回执或日志前统一清除凭证值。
// 解析失败时宁可丢失原始查询，也不能把未知格式的密钥原样落盘。
func redactedAdminQuery(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return "[redacted-invalid-query]"
	}
	for key, items := range values {
		if !secretQueryKey(key) {
			continue
		}
		for idx := range items {
			items[idx] = "[redacted]"
		}
		values[key] = items
	}
	return values.Encode()
}

func secretQueryKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" {
		return false
	}
	switch key {
	case "token", "password", "passwd", "authorization", "api_key", "apikey", "credential", "credentials":
		return true
	}
	return strings.HasSuffix(key, "_token") ||
		strings.HasSuffix(key, "_password") ||
		strings.Contains(key, "secret") ||
		strings.Contains(key, "credential")
}

func WriteJSON(w http.ResponseWriter, code int, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		code = http.StatusInternalServerError
		data = []byte(`{"error":"json encode failed"}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write(append(data, '\n'))
}
