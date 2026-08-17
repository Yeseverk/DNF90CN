package notice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"longheng.io/server/internal/platform/admincmd"
	"longheng.io/server/internal/platform/httpx"
)

var ErrAdminMuxRequired = errors.New("notice admin mux is required")

const maxNoticeJSONBytes = 1 << 20

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
	mux.HandleFunc(prefix+"/publish", mutateWrap(httpx.AdminOperationID(http.MethodPost, prefix+"/publish"), PublishHandlerWithCommands(service, options.CommandHooks)))
	mux.HandleFunc(prefix+"/acknowledge", mutateWrap(httpx.AdminOperationID(http.MethodPost, prefix+"/acknowledge"), AckHandlerCommands(service, options.CommandHooks)))
	mux.HandleFunc(prefix+"/delete", mutateWrap(httpx.AdminOperationID(http.MethodPost, prefix+"/delete"), DeleteHandlerWithCommands(service, options.CommandHooks)))
	mux.HandleFunc(prefix+"/account", wrap(AccountHandler(service)))
	mux.HandleFunc(prefix+"/announcements", wrap(AnnouncementsHandler(service)))
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
			writeNoticeError(w, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, snapshot)
	}
}

func PublishHandler(service Service) http.HandlerFunc {
	return PublishHandlerWithCommands(service, AdminCommandHooks{})
}

func PublishHandlerWithCommands(service Service, commands AdminCommandHooks) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNA(w, http.MethodPost)
			return
		}
		var request PublishRequest
		if err := decodeNoticeJSON(w, r, &request); err != nil {
			httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		receipt, duplicate, submitted, err := submitNoticeCmd(commands, r, "logic.notice.publish", firstNonEmpty(request.Notice.ID, "notice"), "notice publish", request)
		if writeNoticeCmdErr(commands, w, err) {
			return
		}
		if submitted && duplicate {
			writeCmdDuplicate(commands, w, receipt)
			return
		}
		if submitted {
			request.AdminCommand = nil
		}
		result, err := service.Publish(r.Context(), request)
		if err != nil {
			if submitted {
				markNoticeCmdFail(commands, r.Context(), receipt, err)
			}
			writeNoticeError(w, err)
			return
		}
		if submitted {
			receipt = markNoticeCmdOK(commands, r.Context(), receipt)
			writeNoticeReceipt(commands, w, http.StatusOK, noticeResponseMap(result), receipt)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, result)
	}
}

func AcknowledgeHandler(service Service) http.HandlerFunc {
	return AckHandlerCommands(service, AdminCommandHooks{})
}

func AckHandlerCommands(service Service, commands AdminCommandHooks) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNA(w, http.MethodPost)
			return
		}
		var request AcknowledgeRequest
		if err := decodeNoticeJSON(w, r, &request); err != nil {
			httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		target := request.DeliveryID
		if target == "" {
			target = strings.TrimSpace(request.NoticeID + ":" + request.AccountID)
		}
		receipt, duplicateCommand, submitted, err := submitNoticeCmd(commands, r, "logic.notice.acknowledge", target, "notice acknowledge", request)
		if writeNoticeCmdErr(commands, w, err) {
			return
		}
		if submitted && duplicateCommand {
			writeCmdDuplicate(commands, w, receipt)
			return
		}
		delivery, duplicate, err := service.Acknowledge(r.Context(), request)
		if err != nil {
			if submitted {
				markNoticeCmdFail(commands, r.Context(), receipt, err)
			}
			writeNoticeError(w, err)
			return
		}
		response := map[string]any{
			"delivery":  delivery,
			"duplicate": duplicate,
		}
		if submitted {
			receipt = markNoticeCmdOK(commands, r.Context(), receipt)
			writeNoticeReceipt(commands, w, http.StatusOK, response, receipt)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, response)
	}
}

func DeleteHandler(service Service) http.HandlerFunc {
	return DeleteHandlerWithCommands(service, AdminCommandHooks{})
}

func DeleteHandlerWithCommands(service Service, commands AdminCommandHooks) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNA(w, http.MethodPost)
			return
		}
		var request DeleteRequest
		if err := decodeNoticeJSON(w, r, &request); err != nil {
			httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		target := strings.Join(request.DeliveryIDs, ",")
		if target == "" {
			target = strings.TrimSpace(request.NoticeID + ":" + request.AccountID)
		}
		receipt, duplicateCommand, submitted, err := submitNoticeCmd(commands, r, "logic.notice.delete", target, "notice delete", request)
		if writeNoticeCmdErr(commands, w, err) {
			return
		}
		if submitted && duplicateCommand {
			writeCmdDuplicate(commands, w, receipt)
			return
		}
		result, err := service.Delete(r.Context(), request)
		if err != nil {
			if submitted {
				markNoticeCmdFail(commands, r.Context(), receipt, err)
			}
			writeNoticeError(w, err)
			return
		}
		response := map[string]any{"result": result}
		if submitted {
			receipt = markNoticeCmdOK(commands, r.Context(), receipt)
			writeNoticeReceipt(commands, w, http.StatusOK, response, receipt)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, response)
	}
}

func submitNoticeCmd(commands AdminCommandHooks, r *http.Request, operation, target, reason string, params any) (admincmd.Receipt, bool, bool, error) {
	if commands.Submit == nil {
		return admincmd.Receipt{}, false, false, nil
	}
	return commands.Submit(r, operation, target, reason, params)
}

// decodeNoticeJSON 统一限制管理端写请求的大小、字段和单文档格式。
func decodeNoticeJSON(w http.ResponseWriter, r *http.Request, out any) error {
	return httpx.DecodeStrictJSON(w, r, maxNoticeJSONBytes, out)
}

func markNoticeCmdOK(commands AdminCommandHooks, ctx context.Context, receipt admincmd.Receipt) admincmd.Receipt {
	if commands.MarkSucceeded == nil || receipt.ID == "" {
		return receipt
	}
	return commands.MarkSucceeded(ctx, receipt)
}

func markNoticeCmdFail(commands AdminCommandHooks, ctx context.Context, receipt admincmd.Receipt, err error) {
	if commands.MarkFailed == nil || receipt.ID == "" || err == nil {
		return
	}
	commands.MarkFailed(ctx, receipt, err)
}

func writeNoticeCmdErr(commands AdminCommandHooks, w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if commands.WriteError != nil {
		return commands.WriteError(w, err)
	}
	writeNoticeError(w, err)
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

func writeNoticeReceipt(commands AdminCommandHooks, w http.ResponseWriter, status int, response map[string]any, receipt admincmd.Receipt) {
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

func noticeResponseMap(value any) map[string]any {
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

func AccountHandler(service Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNA(w, http.MethodGet)
			return
		}
		query := Query{
			AccountID: r.URL.Query().Get("account_id"),
			ShardID:   r.URL.Query().Get("shard_id"),
			Cursor:    r.URL.Query().Get("cursor"),
		}
		if limit, err := parseQueryInt(r.URL.Query().Get("limit")); err != nil {
			httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		} else {
			query.Limit = limit
		}
		now, err := parseQueryTime(r.URL.Query().Get("now"))
		if err != nil {
			httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		query.Now = now
		result, err := service.ListPageForAccount(r.Context(), query)
		if err != nil {
			writeNoticeError(w, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, result)
	}
}

func AnnouncementsHandler(service Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNA(w, http.MethodGet)
			return
		}
		query := Query{
			ShardID: r.URL.Query().Get("shard_id"),
		}
		now, err := parseQueryTime(r.URL.Query().Get("now"))
		if err != nil {
			httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		query.Now = now
		items, err := service.ActiveAnnouncements(r.Context(), query)
		if err != nil {
			writeNoticeError(w, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

func writeNoticeError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	if errors.Is(err, ErrStoreRequired) {
		status = http.StatusServiceUnavailable
	}
	if errors.Is(err, ErrNoticeNotFound) || errors.Is(err, ErrDeliveryNotFound) {
		status = http.StatusNotFound
	}
	if errors.Is(err, ErrIdempotencyConflict) {
		status = http.StatusConflict
	}
	httpx.WriteJSON(w, status, map[string]string{"error": err.Error()})
}

func writeMethodNA(w http.ResponseWriter, methods ...string) {
	w.Header().Set("Allow", strings.Join(methods, ", "))
	httpx.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
}

func parseQueryTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid now query time: %w", err)
	}
	return parsed.UTC(), nil
}

func parseQueryInt(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	out, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid limit query value: %w", err)
	}
	if out < 0 {
		return 0, fmt.Errorf("invalid limit query value: %d", out)
	}
	return out, nil
}

func normalizeAdminPrefix(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return "/debug/notice"
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	prefix = strings.TrimRight(prefix, "/")
	if prefix == "" {
		return "/debug/notice"
	}
	return prefix
}
