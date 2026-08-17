package logic

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"longheng.io/server/internal/platform/admincmd"
	"longheng.io/server/internal/platform/admincmdqueue"
	"longheng.io/server/internal/platform/httpx"
	"longheng.io/server/internal/services/adminops"
)

func (s *Service) submitOpsCmd(r *http.Request, operation, target, reason string, params any) (admincmd.Receipt, bool, bool, error) {
	if s == nil || s.adminCommands == nil {
		return admincmd.Receipt{}, false, false, nil
	}
	ctx := context.Background()
	command := admincmd.Command{
		Operation:   strings.TrimSpace(operation),
		Scope:       "logic.ops",
		Environment: s.environment,
		ShardID:     s.nodeID,
		Target:      strings.TrimSpace(target),
		Actor:       adminActor(r),
		Reason:      strings.TrimSpace(reason),
		Params:      adminCommandParams(params),
	}
	if r != nil {
		ctx = r.Context()
		command.IdempotencyKey = strings.TrimSpace(r.Header.Get(httpx.AdminIdempotencyHeader))
		command.Confirmation = strings.TrimSpace(r.Header.Get(httpx.AdminConfirmHeader))
	}
	result, err := s.adminCommands.Submit(ctx, command, admincmd.DangerousPolicy())
	if err != nil {
		return admincmd.Receipt{}, false, true, err
	}
	return result.Receipt, result.Duplicate, true, nil
}

func (s *Service) markOpsAdminOK(ctx context.Context, receipt admincmd.Receipt) admincmd.Receipt {
	if s == nil || s.adminCommands == nil || receipt.ID == "" {
		return receipt
	}
	updated, err := adminops.MarkSucceededWithError(ctx, s.adminCommands, receipt)
	if err != nil {
		if s.logger != nil {
			s.logger.Error("mark admin command succeeded failed", "receipt_id", receipt.ID, "error", err)
		}
		return receipt
	}
	return updated
}

func (s *Service) markOpsCmdFail(ctx context.Context, receipt admincmd.Receipt, err error) {
	if s == nil || s.adminCommands == nil || receipt.ID == "" || err == nil {
		return
	}
	if markErr := adminops.MarkFailedWithError(ctx, s.adminCommands, receipt, err); markErr != nil && s.logger != nil {
		s.logger.Error("mark admin command failed failed", "receipt_id", receipt.ID, "error", markErr)
	}
}

func writeOpsCmdErr(w http.ResponseWriter, err error) bool {
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

func writeOpsDup(w http.ResponseWriter, receipt admincmd.Receipt) {
	if receipt.ID != "" {
		w.Header().Set(httpx.AdminReceiptHeader, receipt.ID)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"status":        "duplicate",
		"admin_receipt": receipt,
	})
}

func withAdminReceipt(response map[string]any, receipt admincmd.Receipt) map[string]any {
	if response == nil {
		response = make(map[string]any)
	}
	if receipt.ID != "" {
		response["admin_receipt"] = receipt
	}
	return response
}

func writeOpsAdminReceipt(w http.ResponseWriter, status int, response map[string]any, receipt admincmd.Receipt) {
	if receipt.ID != "" {
		w.Header().Set(httpx.AdminReceiptHeader, receipt.ID)
	}
	httpx.WriteJSON(w, status, withAdminReceipt(response, receipt))
}

func adminActor(r *http.Request) string {
	actor := httpx.AuthenticatedAdminActor(r)
	if actor == "" {
		actor = "admin"
	}
	return actor
}

func adminCommandParams(value any) map[string]any {
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
