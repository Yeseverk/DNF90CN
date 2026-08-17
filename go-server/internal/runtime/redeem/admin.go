package redeem

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"longheng.io/server/internal/platform/admincmd"
	"longheng.io/server/internal/platform/httpx"
)

var ErrAdminMuxRequired = errors.New("redeem admin mux is required")

const maxAdminBodyBytes = 1 << 20

type AdminOptions struct {
	Prefix       string
	Wrap         func(http.HandlerFunc) http.HandlerFunc
	MutateWrap   func(string, http.HandlerFunc) http.HandlerFunc
	CommandHooks AdminCommandHooks
}

type AdminCommandHooks struct {
	Submit               func(*http.Request, string, string, string, any) (admincmd.Receipt, bool, bool, error)
	MarkSucceeded        func(context.Context, admincmd.Receipt) admincmd.Receipt
	MarkFailed           func(context.Context, admincmd.Receipt, error)
	WriteError           func(http.ResponseWriter, error) bool
	WriteDuplicate       func(http.ResponseWriter, admincmd.Receipt)
	WriteJSONWithReceipt func(http.ResponseWriter, int, map[string]any, admincmd.Receipt)
}

func RegisterAdminRoutes(mux *http.ServeMux, service Service, options AdminOptions) error {
	if mux == nil {
		return ErrAdminMuxRequired
	}
	prefix := normalizeAdminPrefix(options.Prefix)
	wrap := options.Wrap
	if wrap == nil {
		wrap = func(next http.HandlerFunc) http.HandlerFunc { return next }
	}
	mutateWrap := options.MutateWrap
	if mutateWrap == nil {
		mutateWrap = func(_ string, next http.HandlerFunc) http.HandlerFunc {
			return wrap(next)
		}
	}
	mux.HandleFunc(prefix, wrap(SnapshotHandler(service)))
	mux.HandleFunc(prefix+"/snapshot", wrap(SnapshotHandler(service)))
	mux.HandleFunc(prefix+"/codes", mutateWrap(httpx.AdminOperationID(http.MethodPost, prefix+"/codes"), PutCodeHandlerWithCommands(service, options.CommandHooks)))
	mux.HandleFunc(prefix+"/claim", mutateWrap(httpx.AdminOperationID(http.MethodPost, prefix+"/claim"), ClaimHandlerWithCommands(service, options.CommandHooks)))
	return nil
}

func SnapshotHandler(service Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNA(w, http.MethodGet)
			return
		}
		snapshot, err := service.Snapshot(r.Context())
		if err != nil {
			writeRedeemError(w, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, snapshot)
	}
}

func PutCodeHandler(service Service) http.HandlerFunc {
	return PutCodeHandlerWithCommands(service, AdminCommandHooks{})
}

func PutCodeHandlerWithCommands(service Service, commands AdminCommandHooks) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNA(w, http.MethodPost)
			return
		}
		var code Code
		if err := decodeRedeemJSON(w, r, &code); err != nil {
			httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		receipt, duplicate, submitted, err := submitRedeemCmd(commands, r, "redeem.code.put", firstNonEmpty(code.Code, "code"), "redeem code put", code)
		if writeRedeemCmdErr(commands, w, err) {
			return
		}
		if submitted && duplicate {
			// 管理命令已提交过时直接回放 receipt，不再重复写礼包码。
			writeCmdDuplicate(commands, w, receipt)
			return
		}
		if submitted {
			code.Meta = mergeStringMap(code.Meta, map[string]string{"admin_receipt_id": receipt.ID})
		}
		saved, err := service.PutCode(r.Context(), code)
		if err != nil {
			if submitted {
				markRedeemCmdFail(commands, r.Context(), receipt, err)
			}
			writeRedeemError(w, err)
			return
		}
		response := map[string]any{"code": saved}
		if submitted {
			receipt = markRedeemCmdOK(commands, r.Context(), receipt)
			writeRedeemReceipt(commands, w, http.StatusOK, response, receipt)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, response)
	}
}

func ClaimHandler(service Service) http.HandlerFunc {
	return ClaimHandlerWithCommands(service, AdminCommandHooks{})
}

func ClaimHandlerWithCommands(service Service, commands AdminCommandHooks) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNA(w, http.MethodPost)
			return
		}
		var request ClaimRequest
		if err := decodeRedeemJSON(w, r, &request); err != nil {
			httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		request.AdminCommand = nil
		target := firstNonEmpty(request.AccountID, request.Code, "claim")
		receipt, duplicateCommand, submitted, err := submitRedeemCmd(commands, r, "redeem.claim.repair", target, "redeem claim repair", request)
		if writeRedeemCmdErr(commands, w, err) {
			return
		}
		if submitted && duplicateCommand {
			// 补领/修复类操作必须先挡住重复命令，避免再次消耗礼包码或重复发奖。
			writeCmdDuplicate(commands, w, receipt)
			return
		}
		if submitted {
			request.Meta = mergeStringMap(request.Meta, map[string]string{"admin_receipt_id": receipt.ID})
		}
		result, err := service.Claim(r.Context(), request)
		if err != nil {
			if submitted {
				markRedeemCmdFail(commands, r.Context(), receipt, err)
			}
			writeRedeemError(w, err)
			return
		}
		response := redeemResponseMap(result)
		if submitted {
			receipt = markRedeemCmdOK(commands, r.Context(), receipt)
			writeRedeemReceipt(commands, w, http.StatusOK, response, receipt)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, result)
	}
}

func submitRedeemCmd(commands AdminCommandHooks, r *http.Request, operation, target, reason string, params any) (admincmd.Receipt, bool, bool, error) {
	if commands.Submit == nil {
		return admincmd.Receipt{}, false, false, nil
	}
	return commands.Submit(r, operation, target, reason, params)
}

func markRedeemCmdOK(commands AdminCommandHooks, ctx context.Context, receipt admincmd.Receipt) admincmd.Receipt {
	if commands.MarkSucceeded == nil || receipt.ID == "" {
		return receipt
	}
	return commands.MarkSucceeded(ctx, receipt)
}

func markRedeemCmdFail(commands AdminCommandHooks, ctx context.Context, receipt admincmd.Receipt, err error) {
	if commands.MarkFailed == nil || receipt.ID == "" || err == nil {
		return
	}
	commands.MarkFailed(ctx, receipt, err)
}

func writeRedeemCmdErr(commands AdminCommandHooks, w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if commands.WriteError != nil {
		return commands.WriteError(w, err)
	}
	writeRedeemError(w, err)
	return true
}

func writeCmdDuplicate(commands AdminCommandHooks, w http.ResponseWriter, receipt admincmd.Receipt) {
	if commands.WriteDuplicate != nil {
		commands.WriteDuplicate(w, receipt)
		return
	}
	if receipt.ID != "" {
		w.Header().Set(httpx.AdminReceiptHeader, receipt.ID)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"status":        "duplicate",
		"admin_receipt": receipt,
	})
}

func writeRedeemReceipt(commands AdminCommandHooks, w http.ResponseWriter, status int, response map[string]any, receipt admincmd.Receipt) {
	if commands.WriteJSONWithReceipt != nil {
		commands.WriteJSONWithReceipt(w, status, response, receipt)
		return
	}
	if response == nil {
		response = make(map[string]any)
	}
	if receipt.ID != "" {
		w.Header().Set(httpx.AdminReceiptHeader, receipt.ID)
		response["admin_receipt"] = receipt
	}
	httpx.WriteJSON(w, status, response)
}

func redeemResponseMap(value any) map[string]any {
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

func decodeRedeemJSON(w http.ResponseWriter, r *http.Request, out any) error {
	return httpx.DecodeStrictJSON(w, r, maxAdminBodyBytes, out)
}

func writeRedeemError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	switch {
	case errors.Is(err, ErrStoreRequired):
		status = http.StatusServiceUnavailable
	case errors.Is(err, ErrCodeNotFound):
		status = http.StatusNotFound
	case errors.Is(err, ErrCodeInactive), errors.Is(err, ErrCodeExhausted), errors.Is(err, ErrAccountLimitExceeded), errors.Is(err, ErrClaimConflict):
		status = http.StatusConflict
	case errors.Is(err, admincmd.ErrMissingIdempotencyKey),
		errors.Is(err, admincmd.ErrMissingConfirmation),
		errors.Is(err, admincmd.ErrMissingActor),
		errors.Is(err, admincmd.ErrMissingReason):
		status = http.StatusForbidden
	}
	httpx.WriteJSON(w, status, map[string]string{"error": err.Error()})
}

func writeMethodNA(w http.ResponseWriter, methods ...string) {
	w.Header().Set("Allow", strings.Join(methods, ", "))
	httpx.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
}

func normalizeAdminPrefix(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return "/debug/redeem"
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	prefix = strings.TrimRight(prefix, "/")
	if prefix == "" {
		return "/debug/redeem"
	}
	return prefix
}
