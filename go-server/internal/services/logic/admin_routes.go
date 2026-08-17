package logic

import (
	"errors"
	"net/http"
	"strings"

	"longheng.io/server/internal/platform/httpx"
	"longheng.io/server/internal/reference/player"
	"longheng.io/server/pkg/contracts"
)

func (s *Service) registerRoutes(mux *http.ServeMux) {
	prefix := s.adminPrefix
	mux.HandleFunc(prefix+"/players", s.admin(s.handlePlayers))
	mux.HandleFunc(prefix+"/player_summaries", s.admin(s.handleSummaries))
	mux.HandleFunc(prefix+"/player_summaries/rebuild", s.admin(s.handleSummaryRebuild))
	mux.HandleFunc(prefix+"/readmodels/player_summaries", s.admin(s.handleViewSearch("player_summaries")))
	mux.HandleFunc(prefix+"/readmodels/player_summaries/rebuild", s.admin(s.handleViewRebuild("player_summaries")))
	mux.HandleFunc(prefix+"/accounts", s.admin(s.handleAccounts))
	mux.HandleFunc(prefix+"/playerstore", s.admin(s.handlePlayerStore))
	mux.HandleFunc(prefix+"/playerstore/deadletters", s.admin(s.handleDeadLetters))
	mux.HandleFunc(prefix+"/playerstore/deadletters/requeue", s.adminDangerous(httpx.AdminOperationID(http.MethodPost, prefix+"/playerstore/deadletters/requeue"), s.handleDLRequeue))
	mux.HandleFunc(prefix+"/world", s.admin(s.handleWorld))
	mux.HandleFunc(prefix+"/playerloops", s.admin(s.handlePlayerLoops))
	mux.HandleFunc(prefix+"/idempotency", s.admin(s.handleIdempotency))
	mux.HandleFunc(prefix+"/handlers", s.admin(s.handleHandlers))
	mux.HandleFunc(prefix+"/profiledb", s.admin(s.handleProfileDB))
	mux.HandleFunc(prefix+"/mock/player", s.adminDangerous(httpx.AdminOperationID(http.MethodPost, prefix+"/mock/player"), s.handlePlayerMutation))
	s.registerAdminRoutes(mux)
	s.registerOpsRoutes(mux, prefix)
}

func (s *Service) handlePlayers(w http.ResponseWriter, _ *http.Request) {
	if s.players == nil {
		httpx.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "player module not configured"})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, s.players.Snapshot())
}

func (s *Service) handleAccounts(w http.ResponseWriter, _ *http.Request) {
	if s.accounts == nil {
		httpx.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "account runtime not configured"})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, s.accounts.Snapshot())
}

func (s *Service) handlePlayerStore(w http.ResponseWriter, _ *http.Request) {
	if s.players == nil {
		httpx.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "player module not configured"})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, s.players.StoreStatus())
}

func (s *Service) handleDeadLetters(w http.ResponseWriter, r *http.Request) {
	if s.players == nil {
		httpx.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "player module not configured"})
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		httpx.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"dead_letters": s.players.DeadLetters(),
	})
}

func (s *Service) handleDLRequeue(w http.ResponseWriter, r *http.Request) {
	if s.players == nil {
		httpx.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "player module not configured"})
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		httpx.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	accountID := strings.TrimSpace(r.URL.Query().Get("account_id"))
	if accountID == "" {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "account_id is required"})
		return
	}
	receipt, duplicate, submitted, err := s.submitOpsCmd(r, "logic.playerstore.deadletter_requeue", accountID, "deadletter requeue", map[string]any{
		"account_id": accountID,
	})
	if writeOpsCmdErr(w, err) {
		return
	}
	if submitted && duplicate {
		writeOpsDup(w, receipt)
		return
	}
	if !s.players.RequeueDeadLetter(accountID) {
		if submitted {
			s.markOpsCmdFail(r.Context(), receipt, errors.New("dead letter not found"))
		}
		httpx.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "dead letter not found"})
		return
	}
	response := map[string]any{
		"status":     "requeued",
		"account_id": accountID,
	}
	if submitted {
		receipt = s.markOpsAdminOK(r.Context(), receipt)
		writeOpsAdminReceipt(w, http.StatusOK, response, receipt)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleWorld(w http.ResponseWriter, _ *http.Request) {
	if s.world == nil {
		httpx.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "world module not configured"})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, s.world.Snapshot())
}

func (s *Service) handlePlayerLoops(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, s.playerLoops.Snapshot())
}

func (s *Service) handleIdempotency(w http.ResponseWriter, _ *http.Request) {
	responseCache := map[string]any{}
	if s.responses != nil {
		responseCache = s.responses.Snapshot()
	}
	if s.idempotency == nil {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"response_cache": responseCache})
		return
	}
	snapshot := s.idempotency.Snapshot()
	snapshot["response_cache"] = responseCache
	httpx.WriteJSON(w, http.StatusOK, snapshot)
}

func (s *Service) handleHandlers(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, s.dispatcher.Snapshot())
}

func (s *Service) handleProfileDB(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"fields":     player.ProfileFieldDescriptors(),
		"operations": player.ProfileOperationDescriptors(),
	})
}

func (s *Service) handlePlayerMutation(w http.ResponseWriter, r *http.Request) {
	if s.players == nil {
		httpx.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "player module not configured"})
		return
	}
	accountID := strings.TrimSpace(r.URL.Query().Get("account_id"))
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if accountID == "" {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "account_id is required"})
		return
	}
	if state == "" {
		state = "online"
	}

	receipt, duplicate, submitted, err := s.submitOpsCmd(r, "logic.mock.player_mutation", accountID, "mock player mutation", map[string]any{
		"account_id": accountID,
		"state":      state,
	})
	if writeOpsCmdErr(w, err) {
		return
	}
	if submitted && duplicate {
		writeOpsDup(w, receipt)
		return
	}

	record := s.players.Upsert(accountID, state)
	event := contracts.PlayerStateChanged{
		AccountID: record.AccountID,
		State:     record.State,
		UpdatedAt: record.UpdatedAt,
	}
	s.publishAsync(contracts.TopicPlayerStateChange, event)
	if submitted {
		receipt = s.markOpsAdminOK(r.Context(), receipt)
		writeOpsAdminReceipt(w, http.StatusOK, map[string]any{"player": record}, receipt)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, record)
}
