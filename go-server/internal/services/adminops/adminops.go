package adminops

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"longheng.io/server/internal/platform/admincmd"
	"longheng.io/server/internal/platform/admincmdqueue"
	appkit "longheng.io/server/internal/platform/app"
	"longheng.io/server/internal/platform/httpx"
)

const (
	MetricAdminRequestsTotal = "admin_requests_total"

	markTimeout = 5 * time.Second
)

type CommandOptions struct {
	Commands    *admincmdqueue.Executor
	Scope       string
	Environment string
	ShardID     string
	Operation   string
	Target      string
	Reason      string
	Params      any
}

type RuntimeEnv interface {
	appkit.CoreEnv
	appkit.AdminEnv
}

func Wrapper(env *appkit.Env, scope string) func(http.HandlerFunc) http.HandlerFunc {
	return WrapperFor(env, scope)
}

func WrapperFor(env RuntimeEnv, scope string) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		var wrapped http.HandlerFunc
		admin := adminDeps(env)
		if admin.Auth != nil {
			wrapped = admin.Auth(scope, next)
		} else {
			wrapped = httpx.WrapAdminPolicy(func() httpx.AdminPolicy {
				return PolicyFor(env)
			}, loggerFor(env), scope, next)
		}
		return observeAdminReq(env, scope, wrapped)
	}
}

func DangerousWrapper(env *appkit.Env, scope string) func(string, http.HandlerFunc) http.HandlerFunc {
	return DangerousWrapperFor(env, scope)
}

func DangerousWrapperFor(env RuntimeEnv, scope string) func(string, http.HandlerFunc) http.HandlerFunc {
	return func(operation string, next http.HandlerFunc) http.HandlerFunc {
		var wrapped http.HandlerFunc
		admin := adminDeps(env)
		if admin.Dangerous != nil {
			wrapped = admin.Dangerous(scope, operation, next)
		} else {
			wrapped = httpx.WrapDangerousAdminPolicy(func() httpx.AdminPolicy {
				return PolicyFor(env)
			}, loggerFor(env), scope, operation, next)
		}
		return observeAdminReq(env, scope, wrapped)
	}
}

func LocalSampleRoutesEnabled(environment string) bool {
	switch strings.ToLower(strings.TrimSpace(environment)) {
	case "", "local", "dev", "development", "test", "docker":
		return true
	default:
		return false
	}
}

func Policy(env *appkit.Env) httpx.AdminPolicy {
	return PolicyFor(env)
}

func PolicyFor(env RuntimeEnv) httpx.AdminPolicy {
	core := coreDeps(env)
	adminConfig := core.Config.Admin
	tokens := make([]httpx.AdminToken, 0, len(adminConfig.Tokens))
	for _, token := range adminConfig.Tokens {
		tokens = append(tokens, httpx.AdminToken{
			Name:   token.Name,
			Token:  token.Token,
			Role:   token.Role,
			Scopes: append([]string(nil), token.Scopes...),
		})
	}
	return httpx.AdminPolicy{
		Token:               adminConfig.Token,
		RBACEnabled:         adminConfig.RBACEnabled,
		RequireConfirmation: adminConfig.DangerousConfirm,
		Tokens:              tokens,
	}
}

func SubmitDangerous(ctx context.Context, r *http.Request, options CommandOptions) (admincmd.Receipt, bool, bool, error) {
	if options.Commands == nil {
		return admincmd.Receipt{}, false, false, nil
	}
	command := admincmd.Command{
		Operation:   strings.TrimSpace(options.Operation),
		Scope:       strings.TrimSpace(options.Scope),
		Environment: strings.TrimSpace(options.Environment),
		ShardID:     strings.TrimSpace(options.ShardID),
		Target:      strings.TrimSpace(options.Target),
		Actor:       Actor(r),
		Reason:      strings.TrimSpace(options.Reason),
		Params:      Params(options.Params),
	}
	if r != nil {
		command.IdempotencyKey = strings.TrimSpace(r.Header.Get(httpx.AdminIdempotencyHeader))
		command.Confirmation = strings.TrimSpace(r.Header.Get(httpx.AdminConfirmHeader))
	}
	result, err := options.Commands.Submit(ctx, command, admincmd.DangerousPolicy())
	if err != nil {
		return admincmd.Receipt{}, false, true, err
	}
	return result.Receipt, result.Duplicate, true, nil
}

func MarkSucceeded(ctx context.Context, commands *admincmdqueue.Executor, receipt admincmd.Receipt) admincmd.Receipt {
	updated, err := MarkSucceededWithError(ctx, commands, receipt)
	if err != nil {
		slog.Default().Error("mark admin command succeeded failed", "receipt_id", receipt.ID, "error", err)
		return receipt
	}
	return updated
}

func MarkSucceededWithError(ctx context.Context, commands *admincmdqueue.Executor, receipt admincmd.Receipt) (admincmd.Receipt, error) {
	if commands == nil || receipt.ID == "" {
		return receipt, nil
	}
	markCtx, cancel := markContext(ctx)
	defer cancel()
	updated, err := commands.MarkSucceeded(markCtx, receipt.ID)
	if err != nil {
		return receipt, err
	}
	return updated, nil
}

func MarkFailed(ctx context.Context, commands *admincmdqueue.Executor, receipt admincmd.Receipt, err error) {
	if markErr := MarkFailedWithError(ctx, commands, receipt, err); markErr != nil {
		slog.Default().Error("mark admin command failed failed", "receipt_id", receipt.ID, "error", markErr)
	}
}

func MarkFailedWithError(ctx context.Context, commands *admincmdqueue.Executor, receipt admincmd.Receipt, err error) error {
	if commands == nil || receipt.ID == "" || err == nil {
		return nil
	}
	markCtx, cancel := markContext(ctx)
	defer cancel()
	_, markErr := commands.MarkFailed(markCtx, receipt.ID, err.Error(), time.Now().UTC().Add(time.Minute))
	return markErr
}

func markContext(parent context.Context) (context.Context, context.CancelFunc) {
	base := context.Background()
	if parent != nil {
		base = context.WithoutCancel(parent)
	}
	return context.WithTimeout(base, markTimeout)
}

func WriteCommandError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	status := http.StatusBadRequest
	if errors.Is(err, admincmdqueue.ErrIdempotencyConflict) {
		status = http.StatusConflict
	}
	if errors.Is(err, admincmd.ErrMissingIdempotencyKey) || errors.Is(err, admincmd.ErrMissingConfirmation) {
		status = http.StatusPreconditionRequired
	}
	httpx.WriteJSON(w, status, map[string]string{"error": err.Error()})
	return true
}

func WriteDuplicate(w http.ResponseWriter, receipt admincmd.Receipt) {
	SetReceiptHeader(w, receipt)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"status":        "duplicate",
		"admin_receipt": receipt,
	})
}

func WithReceipt(response map[string]any, receipt admincmd.Receipt) map[string]any {
	if response == nil {
		response = make(map[string]any)
	}
	if receipt.ID != "" {
		response["admin_receipt"] = receipt
	}
	return response
}

func WriteJSONWithReceipt(w http.ResponseWriter, status int, response map[string]any, receipt admincmd.Receipt) {
	SetReceiptHeader(w, receipt)
	httpx.WriteJSON(w, status, WithReceipt(response, receipt))
}

func SetReceiptHeader(w http.ResponseWriter, receipt admincmd.Receipt) {
	if w != nil && receipt.ID != "" {
		w.Header().Set(httpx.AdminReceiptHeader, receipt.ID)
	}
}

func Actor(r *http.Request) string {
	actor := httpx.AuthenticatedAdminActor(r)
	if actor == "" {
		actor = "admin"
	}
	return actor
}

func Params(value any) map[string]any {
	if value == nil {
		return nil
	}
	if params, ok := value.(map[string]any); ok {
		return params
	}
	data, err := json.Marshal(value)
	if err != nil {
		return map[string]any{"marshal_error": err.Error()}
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return map[string]any{"marshal_error": err.Error()}
	}
	return out
}

func loggerFor(env RuntimeEnv) *slog.Logger {
	return coreDeps(env).Logger
}

func coreDeps(env RuntimeEnv) appkit.CoreDeps {
	if env == nil {
		return appkit.CoreDeps{}
	}
	return env.Core()
}

func adminDeps(env RuntimeEnv) appkit.AdminDeps {
	if env == nil {
		return appkit.AdminDeps{}
	}
	return env.AdminRuntime()
}

type adminStatusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *adminStatusRecorder) WriteHeader(status int) {
	if r.status != 0 {
		return
	}
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *adminStatusRecorder) Write(body []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(body)
}

func (r *adminStatusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

func observeAdminReq(env RuntimeEnv, scope string, next http.HandlerFunc) http.HandlerFunc {
	core := coreDeps(env)
	if core.Metrics == nil {
		return next
	}
	return func(w http.ResponseWriter, r *http.Request) {
		recorder := &adminStatusRecorder{ResponseWriter: w}
		defer func() {
			if recovered := recover(); recovered != nil {
				recordAdminMetric(env, scope, "panic")
				panic(recovered)
			}
			status := recorder.status
			if status == 0 {
				status = http.StatusOK
			}
			recordAdminMetric(env, scope, adminResultStatus(status))
		}()
		next(recorder, r)
	}
}

func recordAdminMetric(env RuntimeEnv, scope, result string) {
	core := coreDeps(env)
	if core.Metrics == nil {
		return
	}
	service := strings.TrimSpace(core.Config.Service.Name)
	if service == "" {
		service = strings.TrimSpace(scope)
	}
	if service == "" {
		service = "unknown"
	}
	scope = strings.TrimSpace(scope)
	if scope == "" {
		scope = "unknown"
	}
	result = strings.TrimSpace(result)
	if result == "" {
		result = "unknown"
	}
	core.Metrics.Inc(MetricAdminRequestsTotal, map[string]string{
		"service": service,
		"scope":   scope,
		"result":  result,
	})
}

func adminResultStatus(status int) string {
	if status >= 200 && status < 400 {
		return "ok"
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return "unauthorized"
	}
	if status >= 400 && status < 500 {
		return "client_error_" + strconv.Itoa(status)
	}
	if status >= 500 {
		return "server_error"
	}
	return "unknown"
}
