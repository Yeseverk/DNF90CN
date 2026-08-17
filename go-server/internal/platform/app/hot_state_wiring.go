package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"longheng.io/server/internal/platform/admincmd"
	"longheng.io/server/internal/platform/admincmdqueue"
	"longheng.io/server/internal/platform/config"
	"longheng.io/server/internal/platform/datatable"
	"longheng.io/server/internal/platform/eventlog"
	"longheng.io/server/internal/platform/hotupdate"
	"longheng.io/server/internal/platform/httpx"
	"longheng.io/server/internal/platform/nativepatch"
	"longheng.io/server/internal/platform/statesync"
)

const maxUpdateJSONBytes = 1 << 20

func newStateSyncStore(cfg config.ServiceConfig) (statesync.Store, error) {
	if !cfg.StateSync.Enabled {
		return nil, nil
	}
	return statesync.NewMemory(statesync.Options{
		MaxPayloadBytes: cfg.StateSync.MaxPayloadBytes,
	}), nil
}

func newHotUpdateManager(cfg config.ServiceConfig, dataTables *datatable.Registry, dataTableViews *datatable.ViewManager, eventLog *eventlog.Log, logger *slog.Logger) (*hotupdate.Manager, error) {
	if !cfg.HotUpdate.Enabled {
		return nil, nil
	}
	var control hotupdate.Control
	switch cfg.HotUpdate.ControlKind {
	case "", "memory":
		control = hotupdate.NewMemoryControl()
	case "eventlog":
		if eventLog == nil {
			return nil, hotupdate.ErrControlLogRequired
		}
		var err error
		control, err = hotupdate.NewEventLogControl(hotupdate.EventLogControlOptions{Log: eventLog})
		if err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("unsupported hot update control kind")
	}
	applier := hotupdate.Applier(hotupdate.DataTableApplier{Registry: dataTables, Views: dataTableViews})
	if cfg.HotUpdate.ApplierKind == "native_patch" {
		applier = hotupdate.NativePatchApplier{Policy: nativepatch.Policy{
			AllowedTargets:     cfg.HotUpdate.NativePatchAllowedTargets,
			AllowedOldSymbols:  cfg.HotUpdate.PatchOldSymbols,
			RequireBuildID:     cfg.HotUpdate.NativePatchRequireBuildID,
			RequireRequestedBy: cfg.HotUpdate.PatchRequireActor,
			RequireReason:      cfg.HotUpdate.NativePatchRequireReason,
			MinReasonLength:    cfg.HotUpdate.NativePatchMinReasonLength,
			MaxSymbols:         cfg.HotUpdate.NativePatchMaxSymbols,
			MaxLiveDuration:    time.Duration(cfg.HotUpdate.NativePatchMaxLiveSeconds) * time.Second,
		}}
	}
	return hotupdate.NewManager(hotupdate.ManagerOptions{
		Target:       cfg.HotUpdate.Target,
		NodeID:       cfg.Service.NodeID,
		Workspace:    cfg.HotUpdate.Workspace,
		ApplyTimeout: time.Duration(cfg.HotUpdate.ApplyTimeoutSeconds) * time.Second,
		Control:      control,
		Source: hotupdate.LocalSource{VerifyOptions: hotupdate.BundleVerifyOptions{
			SigningKey:       hotUpdateSigningKey(cfg),
			RequireSignature: cfg.HotUpdate.RequireSignature,
		}},
		Applier: applier,
		Logger:  logger,
	})
}

func hotUpdateSigningKey(cfg config.ServiceConfig) []byte {
	keyEnv := strings.TrimSpace(cfg.HotUpdate.SigningKeyEnv)
	if keyEnv == "" {
		return nil
	}
	key := strings.TrimSpace(os.Getenv(keyEnv))
	if key == "" {
		return nil
	}
	return []byte(key)
}

func closeHotCtl(manager *hotupdate.Manager) {
	if manager == nil || manager.Control() == nil {
		return
	}
	_ = manager.Control().Close()
}

func newStateSyncSweeper(store statesync.Store, interval time.Duration, logger *slog.Logger) Component {
	if store == nil || interval <= 0 {
		return nil
	}
	return &stateSyncSweeper{
		store:    store,
		interval: interval,
		logger:   logger,
	}
}

type stateSyncSweeper struct {
	store    statesync.Store
	interval time.Duration
	logger   *slog.Logger

	mu       sync.Mutex
	cancel   context.CancelFunc
	done     chan struct{}
	stopping bool
}

// Name 返回状态同步清理组件名。
func (s *stateSyncSweeper) Name() string {
	return "state-sync-sweeper"
}

// Start 启动状态同步过期数据清理循环。
func (s *stateSyncSweeper) Start(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	if s.done != nil || s.stopping {
		s.mu.Unlock()
		return nil
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	s.cancel = cancel
	s.done = done
	s.mu.Unlock()

	go func() {
		defer func() {
			s.clearStopped(done)
			close(done)
		}()
		sweepStateSync(runCtx, s.store, s.interval, s.logger)
	}()
	return nil
}

// Stop 停止状态同步过期数据清理循环。
func (s *stateSyncSweeper) Stop(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	cancel := s.cancel
	done := s.done
	shouldSignal := cancel != nil
	if shouldSignal {
		s.cancel = nil
		s.stopping = true
	}
	s.mu.Unlock()
	if shouldSignal {
		cancel()
	}
	if done == nil {
		return nil
	}
	select {
	case <-done:
		s.clearStopped(done)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *stateSyncSweeper) clearStopped(done chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done != done {
		return
	}
	s.cancel = nil
	s.done = nil
	s.stopping = false
}

func regUpdateStateRoutes(mux *http.ServeMux, wrapDangerous func(string, string, http.HandlerFunc) http.HandlerFunc, adminCommands *admincmdqueue.Executor, hotUpdateManager *hotupdate.Manager, stateSyncStore statesync.Store, stateSyncDefaultTTL time.Duration) {
	if stateSyncDefaultTTL <= 0 {
		stateSyncDefaultTTL = time.Minute
	}
	hotUpdateScope := hotUpdateAdminScope(hotUpdateManager)
	mux.HandleFunc("/debug/hotupdate", wrapDangerous(hotUpdateScope, httpx.AdminOperationID(http.MethodPost, "/debug/hotupdate"), func(w http.ResponseWriter, r *http.Request) {
		if hotUpdateManager == nil {
			httpx.WriteJSON(w, http.StatusOK, map[string]any{"enabled": false})
			return
		}
		switch r.Method {
		case http.MethodGet:
			httpx.WriteJSON(w, http.StatusOK, map[string]any{
				"enabled":  true,
				"snapshot": hotUpdateManager.Snapshot(),
				"control":  hotUpdateManager.Control().Snapshot(),
			})
		case http.MethodPost:
			var intent hotupdate.Intent
			if err := httpx.DecodeStrictJSON(w, r, maxUpdateJSONBytes, &intent); err != nil {
				httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			target := hotUpdateManager.Snapshot().Target
			receipt, duplicate, submitted, err := submitHotUpdateCmd(r, adminCommands, hotUpdateScope, target, intent)
			if writeUpdateCmdError(w, err) {
				return
			}
			if duplicate {
				writeAdminDuplicate(w, receipt)
				return
			}
			if err := hotUpdateManager.Control().Publish(r.Context(), target, intent); err != nil {
				if submitted {
					markUpdateCmdFailed(r.Context(), adminCommands, receipt, err)
				}
				httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			if submitted {
				receipt = markUpdateOK(r.Context(), adminCommands, receipt)
			}
			setAdminReceipt(w, receipt)
			httpx.WriteJSON(w, http.StatusAccepted, map[string]any{
				"accepted":      true,
				"target":        target,
				"intent":        intent,
				"admin_receipt": receipt,
			})
		default:
			httpx.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
	}))
	mux.HandleFunc("/debug/statesync", wrapDangerous("platform", httpx.AdminOperationID(http.MethodPost, "/debug/statesync"), func(w http.ResponseWriter, r *http.Request) {
		if stateSyncStore == nil {
			httpx.WriteJSON(w, http.StatusOK, map[string]any{"enabled": false})
			return
		}
		switch r.Method {
		case http.MethodGet:
			scope := r.URL.Query().Get("scope")
			if scope == "" {
				httpx.WriteJSON(w, http.StatusOK, map[string]any{"enabled": true, "snapshot": stateSyncStore.Snapshot()})
				return
			}
			records, err := stateSyncStore.List(r.Context(), scope)
			if err != nil {
				httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			httpx.WriteJSON(w, http.StatusOK, map[string]any{"enabled": true, "scope": scope, "records": records})
		case http.MethodPost:
			var request stateSyncAdminReq
			if err := httpx.DecodeStrictJSON(w, r, maxUpdateJSONBytes, &request); err != nil {
				httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			record := request.Record
			expiresAtProvided := !record.ExpiresAt.IsZero()
			if record.ExpiresAt.IsZero() {
				record.ExpiresAt = time.Now().UTC().Add(stateSyncDefaultTTL)
			}
			receipt, duplicate, submitted, err := submitStateSyncCmd(r, adminCommands, record, request.Reason, expiresAtProvided)
			if writeUpdateCmdError(w, err) {
				return
			}
			if duplicate {
				writeAdminDuplicate(w, receipt)
				return
			}
			stored, err := stateSyncStore.Upsert(r.Context(), record)
			if err != nil {
				if submitted {
					markUpdateCmdFailed(r.Context(), adminCommands, receipt, err)
				}
				httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			if submitted {
				receipt = markUpdateOK(r.Context(), adminCommands, receipt)
			}
			setAdminReceipt(w, receipt)
			httpx.WriteJSON(w, http.StatusOK, map[string]any{"record": stored, "admin_receipt": receipt})
		default:
			httpx.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
	}))
}

func hotUpdateAdminScope(manager *hotupdate.Manager) string {
	if manager != nil && manager.NativePatchEnabled() {
		return "platform.nativepatch"
	}
	return "platform.hotupdate"
}

type stateSyncAdminReq struct {
	statesync.Record
	Reason string `json:"reason,omitempty"`
}

func submitHotUpdateCmd(r *http.Request, commands *admincmdqueue.Executor, scope, target string, intent hotupdate.Intent) (admincmd.Receipt, bool, bool, error) {
	if commands == nil {
		return admincmd.Receipt{}, false, false, nil
	}
	action := strings.TrimSpace(string(intent.Action))
	if action == "" {
		action = string(hotupdate.ActionApply)
	}
	operation := "platform.hotupdate." + action
	result, err := commands.Submit(r.Context(), admincmd.Command{
		Operation:      operation,
		Scope:          scope,
		Target:         strings.TrimSpace(target),
		Actor:          hotUpdateAdminActor(r),
		Reason:         strings.TrimSpace(intent.Reason),
		IdempotencyKey: strings.TrimSpace(r.Header.Get(httpx.AdminIdempotencyHeader)),
		Confirmation:   strings.TrimSpace(r.Header.Get(httpx.AdminConfirmHeader)),
		Params: map[string]any{
			"action":       action,
			"version":      intent.Version,
			"source_uri":   intent.SourceURI,
			"checksum":     intent.Checksum,
			"available_at": intent.AvailableAt,
			"requested_by": intent.RequestedBy,
			"sequence":     intent.Sequence,
		},
	}, admincmd.DangerousPolicy())
	if err != nil {
		return admincmd.Receipt{}, false, true, err
	}
	return result.Receipt, result.Duplicate, true, nil
}

func hotUpdateAdminActor(r *http.Request) string {
	actor := httpx.AuthenticatedAdminActor(r)
	if actor == "" {
		actor = "admin"
	}
	return actor
}

func submitStateSyncCmd(r *http.Request, commands *admincmdqueue.Executor, record statesync.Record, reason string, expiresAtProvided bool) (admincmd.Receipt, bool, bool, error) {
	if commands == nil {
		return admincmd.Receipt{}, false, false, nil
	}
	target := strings.TrimSpace(record.Scope)
	if key := strings.TrimSpace(record.Key); key != "" {
		target += "/" + key
	}
	params := map[string]any{
		"scope":      record.Scope,
		"key":        record.Key,
		"version":    record.Version,
		"owner_node": record.OwnerNode,
		"payload":    record.Payload,
	}
	if expiresAtProvided {
		params["expires_at"] = record.ExpiresAt
	}
	result, err := commands.Submit(r.Context(), admincmd.Command{
		Operation:      "platform.statesync.upsert",
		Scope:          "platform",
		Target:         target,
		Actor:          hotUpdateAdminActor(r),
		Reason:         strings.TrimSpace(reason),
		IdempotencyKey: strings.TrimSpace(r.Header.Get(httpx.AdminIdempotencyHeader)),
		Confirmation:   strings.TrimSpace(r.Header.Get(httpx.AdminConfirmHeader)),
		Params:         params,
	}, admincmd.DangerousPolicy())
	if err != nil {
		return admincmd.Receipt{}, false, true, err
	}
	return result.Receipt, result.Duplicate, true, nil
}

func markUpdateOK(ctx context.Context, commands *admincmdqueue.Executor, receipt admincmd.Receipt) admincmd.Receipt {
	if commands == nil || receipt.ID == "" {
		return receipt
	}
	updated, err := commands.MarkSucceeded(ctx, receipt.ID)
	if err != nil {
		slog.Default().Error("mark hotupdate admin command succeeded failed", "receipt_id", receipt.ID, "error", err)
		return receipt
	}
	return updated
}

func markUpdateCmdFailed(ctx context.Context, commands *admincmdqueue.Executor, receipt admincmd.Receipt, err error) {
	if commands == nil || receipt.ID == "" || err == nil {
		return
	}
	if _, markErr := commands.MarkFailed(ctx, receipt.ID, err.Error(), time.Now().UTC().Add(time.Minute)); markErr != nil {
		slog.Default().Error("mark hotupdate admin command failed failed", "receipt_id", receipt.ID, "error", markErr)
	}
}

func writeUpdateCmdError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	status := http.StatusBadRequest
	if errors.Is(err, admincmdqueue.ErrIdempotencyConflict) {
		status = http.StatusConflict
	}
	if errors.Is(err, admincmd.ErrMissingIdempotencyKey) ||
		errors.Is(err, admincmd.ErrMissingConfirmation) ||
		errors.Is(err, admincmd.ErrMissingReason) {
		status = http.StatusPreconditionRequired
	}
	httpx.WriteJSON(w, status, map[string]string{"error": err.Error()})
	return true
}

func writeAdminDuplicate(w http.ResponseWriter, receipt admincmd.Receipt) {
	setAdminReceipt(w, receipt)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"status":        "duplicate",
		"admin_receipt": receipt,
	})
}

func setAdminReceipt(w http.ResponseWriter, receipt admincmd.Receipt) {
	if w != nil && receipt.ID != "" {
		w.Header().Set(httpx.AdminReceiptHeader, receipt.ID)
	}
}

func sweepStateSync(ctx context.Context, store statesync.Store, interval time.Duration, logger *slog.Logger) {
	if store == nil || interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := store.SweepExpired(ctx); err != nil && logger != nil {
				logger.Warn("state sync cleanup failed", "error", err)
			}
		}
	}
}
