package logic

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"longheng.io/server/internal/platform/eventlog"
	"longheng.io/server/internal/platform/httpx"
	"longheng.io/server/internal/reference/player"
	"longheng.io/server/internal/runtime/leaderboard"
	"longheng.io/server/internal/runtime/moderation"
)

type lbRepairReq struct {
	Action        string            `json:"action"`
	LeaderboardID string            `json:"leaderboard_id"`
	OwnerID       string            `json:"owner_id,omitempty"`
	Title         string            `json:"title,omitempty"`
	SortOrder     string            `json:"sort_order,omitempty"`
	Operator      string            `json:"operator,omitempty"`
	MaxSize       int               `json:"max_size,omitempty"`
	Score         int64             `json:"score,omitempty"`
	Subscore      int64             `json:"subscore,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}

type sanctionRequest struct {
	ID        string            `json:"id,omitempty"`
	Subject   string            `json:"subject"`
	Scope     string            `json:"scope,omitempty"`
	Kind      string            `json:"kind,omitempty"`
	Reason    string            `json:"reason,omitempty"`
	Source    string            `json:"source,omitempty"`
	Until     time.Time         `json:"until,omitempty"`
	TTLSecond int64             `json:"ttl_seconds,omitempty"`
	Meta      map[string]string `json:"meta,omitempty"`
}

const maxOpsAdminJSONBytes = 1 << 20

func (s *Service) registerOpsRoutes(mux *http.ServeMux, prefix string) {
	if mux == nil {
		return
	}
	opsPrefix := prefix + "/ops"
	dangerous := func(next http.HandlerFunc) http.HandlerFunc {
		return s.adminDangerous("", next)
	}
	mux.HandleFunc(opsPrefix, s.admin(s.handleOpsIndex))
	mux.HandleFunc(opsPrefix+"/players", s.admin(s.handleOpsPlayers))
	mux.HandleFunc(opsPrefix+"/profile", s.admin(s.handleOpsProfile))
	mux.HandleFunc(opsPrefix+"/profile/repair", dangerous(s.handleOpsRepair))
	mux.HandleFunc(opsPrefix+"/events/replay", dangerous(s.handleOpsEventReplay))
	mux.HandleFunc(opsPrefix+"/leaderboard", s.admin(s.handleOpsLeaderboard))
	mux.HandleFunc(opsPrefix+"/leaderboard/repair", dangerous(s.handleOpsRankRepair))
	mux.HandleFunc(opsPrefix+"/bans", dangerous(s.handleOpsBans))
}

func (s *Service) handleOpsIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", http.MethodGet)
		httpx.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"players":      s.adminPrefix + "/ops/players?account_id=",
		"profile":      s.adminPrefix + "/ops/profile?account_id=",
		"profile_fix":  s.adminPrefix + "/ops/profile/repair",
		"event_replay": s.adminPrefix + "/ops/events/replay?id=",
		"leaderboard":  s.adminPrefix + "/ops/leaderboard",
		"bans":         s.adminPrefix + "/ops/bans",
	})
}

func (s *Service) handleOpsPlayers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", http.MethodGet)
		httpx.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	accountID := strings.TrimSpace(r.URL.Query().Get("account_id"))
	if accountID == "" {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"accounts": s.accountSnapshots(),
			"players":  s.playerSnapshots(),
		})
		return
	}
	profile, active, found, err := s.lookupProfile(r, accountID)
	if err != nil {
		httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	if !found {
		httpx.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "player not found"})
		return
	}
	response := map[string]any{
		"account_id": accountID,
		"profile":    profile,
		"active":     active,
		"account":    s.accountSnapshot(accountID),
	}
	if s.players != nil {
		if summary, ok, err := s.players.GetSummary(r.Context(), accountID); err == nil && ok {
			response["summary"] = summary
		}
	}
	if s.sanctions != nil {
		if sanctions, err := s.sanctions.Active(r.Context(), moderation.SanctionQuery{Subject: accountID, Scope: "global"}); err == nil {
			response["active_sanctions"] = sanctions
		}
	}
	httpx.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleOpsProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", http.MethodGet)
		httpx.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	accountID := strings.TrimSpace(r.URL.Query().Get("account_id"))
	if accountID == "" {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "account_id is required"})
		return
	}
	profile, active, found, err := s.lookupProfile(r, accountID)
	if err != nil {
		httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	if !found {
		httpx.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "profile not found"})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"profile": profile,
		"active":  active,
	})
}

func (s *Service) handleOpsRepair(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		httpx.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if s.players == nil {
		httpx.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "player module not configured"})
		return
	}
	var repair player.ProfileRepair
	if err := decodeJSONOrQuery(w, r, &repair); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if repair.AccountID == "" {
		repair.AccountID = strings.TrimSpace(r.URL.Query().Get("account_id"))
	}
	applyRepairQuery(r, &repair)
	receipt, duplicate, submitted, err := s.submitOpsCmd(r, "logic.profile.repair", repair.AccountID, repair.Reason, repair)
	if writeOpsCmdErr(w, err) {
		return
	}
	if duplicate {
		writeOpsDup(w, receipt)
		return
	}
	profile, ok, err := s.players.RepairProfile(r.Context(), repair)
	if err != nil {
		if submitted {
			s.markOpsCmdFail(r.Context(), receipt, err)
		}
		httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	if !ok {
		if submitted {
			s.markOpsCmdFail(r.Context(), receipt, errors.New("profile not found"))
		}
		httpx.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "profile not found"})
		return
	}
	if submitted {
		receipt = s.markOpsAdminOK(r.Context(), receipt)
	}
	writeOpsAdminReceipt(w, http.StatusOK, map[string]any{
		"status":  "repaired",
		"profile": profile,
	}, receipt)
}

func (s *Service) handleOpsEventReplay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		httpx.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if s.eventLog == nil {
		httpx.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "eventlog is not configured"})
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		var body struct {
			ID string `json:"id"`
		}
		if err := decodeJSONOrQuery(w, r, &body); err != nil {
			httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		id = strings.TrimSpace(body.ID)
	}
	if id == "" {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "id is required"})
		return
	}
	receipt, duplicate, submitted, submitErr := s.submitOpsCmd(r, "logic.event.replay", id, "event replay", map[string]any{"id": id})
	if writeOpsCmdErr(w, submitErr) {
		return
	}
	if duplicate {
		writeOpsDup(w, receipt)
		return
	}
	event, err := s.eventLog.RequeueDeadLetter(r.Context(), id, eventlog.RequeueOptions{})
	if err != nil {
		if submitted {
			s.markOpsCmdFail(r.Context(), receipt, err)
		}
		status := http.StatusServiceUnavailable
		if errors.Is(err, eventlog.ErrEventNotFound) {
			status = http.StatusNotFound
		}
		httpx.WriteJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	if submitted {
		receipt = s.markOpsAdminOK(r.Context(), receipt)
	}
	writeOpsAdminReceipt(w, http.StatusOK, map[string]any{
		"status": "requeued",
		"event":  event,
	}, receipt)
}

func (s *Service) handleOpsLeaderboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", http.MethodGet)
		httpx.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if s.leaderboards == nil {
		httpx.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "leaderboard runtime not configured"})
		return
	}
	boardID := strings.TrimSpace(r.URL.Query().Get("leaderboard_id"))
	ownerID := strings.TrimSpace(r.URL.Query().Get("owner_id"))
	if boardID == "" {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"snapshot":    s.leaderboards.Snapshot(),
			"definitions": s.leaderboards.Definitions(),
		})
		return
	}
	response := map[string]any{"leaderboard_id": boardID}
	if definition, ok := s.leaderboards.Definition(boardID); ok {
		response["definition"] = definition
	} else {
		httpx.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "leaderboard not found"})
		return
	}
	if ownerID != "" {
		if record, ok := s.leaderboards.Record(boardID, ownerID); ok {
			response["record"] = record
		} else {
			response["record_missing"] = true
		}
	}
	limit := queryInt(r, "limit", 50)
	records, err := s.leaderboards.List(r.Context(), boardID, leaderboard.ListOptions{Limit: limit})
	if err != nil {
		httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	response["records"] = records
	httpx.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleOpsRankRepair(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		httpx.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if s.leaderboards == nil {
		httpx.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "leaderboard runtime not configured"})
		return
	}
	var req lbRepairReq
	if err := decodeJSONOrQuery(w, r, &req); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	fillRankRepairQuery(r, &req)
	reason := "leaderboard repair"
	if action := strings.TrimSpace(req.Action); action != "" {
		reason = "leaderboard " + action
	}
	target := strings.TrimSpace(req.LeaderboardID)
	if req.OwnerID != "" {
		target += ":" + strings.TrimSpace(req.OwnerID)
	}
	receipt, duplicate, submitted, submitErr := s.submitOpsCmd(r, "logic.leaderboard.repair", target, reason, req)
	if writeOpsCmdErr(w, submitErr) {
		return
	}
	if duplicate {
		writeOpsDup(w, receipt)
		return
	}
	result, err := s.applyLbRepair(r, req)
	if err != nil {
		if submitted {
			s.markOpsCmdFail(r.Context(), receipt, err)
		}
		status := http.StatusServiceUnavailable
		if errors.Is(err, leaderboard.ErrLeaderboardNotFound) || errors.Is(err, leaderboard.ErrRecordNotFound) {
			status = http.StatusNotFound
		}
		if errors.Is(err, leaderboard.ErrInvalidDefinition) || errors.Is(err, leaderboard.ErrInvalidSubmission) || errors.Is(err, leaderboard.ErrLeaderboardExists) {
			status = http.StatusBadRequest
		}
		httpx.WriteJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	if submitted {
		receipt = s.markOpsAdminOK(r.Context(), receipt)
	}
	writeOpsAdminReceipt(w, http.StatusOK, result, receipt)
}

func (s *Service) handleOpsBans(w http.ResponseWriter, r *http.Request) {
	if s.sanctions == nil {
		httpx.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "moderation sanction store not configured"})
		return
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		snapshot, err := s.sanctions.Snapshot(r.Context())
		if err != nil {
			httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
			return
		}
		httpx.WriteJSON(w, http.StatusOK, snapshot)
	case http.MethodPost:
		var req sanctionRequest
		if err := decodeJSONOrQuery(w, r, &req); err != nil {
			httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		fillSanctionQuery(r, &req)
		receipt, duplicate, submitted, submitErr := s.submitOpsCmd(r, "logic.sanction.upsert", req.Subject, req.Reason, req)
		if writeOpsCmdErr(w, submitErr) {
			return
		}
		if duplicate {
			writeOpsDup(w, receipt)
			return
		}
		item, err := s.sanctions.Upsert(r.Context(), req.toSanction())
		if err != nil {
			if submitted {
				s.markOpsCmdFail(r.Context(), receipt, err)
			}
			httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if submitted {
			receipt = s.markOpsAdminOK(r.Context(), receipt)
		}
		writeOpsAdminReceipt(w, http.StatusOK, map[string]any{"status": "upserted", "sanction": item}, receipt)
	case http.MethodDelete:
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		if id == "" {
			httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "id is required"})
			return
		}
		reason := strings.TrimSpace(r.URL.Query().Get("reason"))
		if reason == "" {
			reason = "remove sanction"
		}
		receipt, duplicate, submitted, submitErr := s.submitOpsCmd(r, "logic.sanction.remove", id, reason, map[string]any{"id": id, "reason": reason})
		if writeOpsCmdErr(w, submitErr) {
			return
		}
		if duplicate {
			writeOpsDup(w, receipt)
			return
		}
		if err := s.sanctions.Remove(r.Context(), id); err != nil {
			if submitted {
				s.markOpsCmdFail(r.Context(), receipt, err)
			}
			status := http.StatusServiceUnavailable
			if errors.Is(err, moderation.ErrSanctionMissing) {
				status = http.StatusNotFound
			}
			httpx.WriteJSON(w, status, map[string]string{"error": err.Error()})
			return
		}
		if submitted {
			receipt = s.markOpsAdminOK(r.Context(), receipt)
		}
		writeOpsAdminReceipt(w, http.StatusOK, map[string]any{"status": "removed", "id": id}, receipt)
	default:
		w.Header().Set("Allow", "GET, POST, DELETE")
		httpx.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (s *Service) lookupProfile(r *http.Request, accountID string) (player.Profile, bool, bool, error) {
	if s.players == nil {
		return player.Profile{}, false, false, nil
	}
	if profile, ok := s.players.Get(accountID); ok {
		return profile, true, true, nil
	}
	profile, ok, err := s.players.LoadReadonly(r.Context(), accountID)
	return profile, false, ok, err
}

func (s *Service) accountSnapshots() []AccountSnapshot {
	if s.accounts == nil {
		return nil
	}
	return s.accounts.Snapshot()
}

func (s *Service) playerSnapshots() []player.Record {
	if s.players == nil {
		return nil
	}
	return s.players.Snapshot()
}

func (s *Service) accountSnapshot(accountID string) *AccountSnapshot {
	if s.accounts == nil {
		return nil
	}
	for _, item := range s.accounts.Snapshot() {
		if item.AccountID == accountID {
			copy := item
			return &copy
		}
	}
	return nil
}

func decodeJSONOrQuery(w http.ResponseWriter, r *http.Request, dst any) error {
	if r.Body == nil || r.ContentLength == 0 {
		return nil
	}
	return httpx.DecodeStrictJSON(w, r, maxOpsAdminJSONBytes, dst)
}

func applyRepairQuery(r *http.Request, repair *player.ProfileRepair) {
	query := r.URL.Query()
	if repair.Reason == "" {
		repair.Reason = strings.TrimSpace(query.Get("reason"))
	}
	if repair.State == nil {
		if raw := strings.TrimSpace(query.Get("state")); raw != "" {
			repair.State = &raw
		}
	}
	if repair.Name == nil {
		if raw := strings.TrimSpace(query.Get("name")); raw != "" {
			repair.Name = &raw
		}
	}
	if repair.Level == nil {
		if raw := strings.TrimSpace(query.Get("level")); raw != "" {
			if value, err := strconv.Atoi(raw); err == nil {
				repair.Level = &value
			}
		}
	}
}

func fillRankRepairQuery(r *http.Request, req *lbRepairReq) {
	query := r.URL.Query()
	if req.Action == "" {
		req.Action = strings.TrimSpace(query.Get("action"))
	}
	if req.LeaderboardID == "" {
		req.LeaderboardID = strings.TrimSpace(query.Get("leaderboard_id"))
	}
	if req.OwnerID == "" {
		req.OwnerID = strings.TrimSpace(query.Get("owner_id"))
	}
	if req.Title == "" {
		req.Title = strings.TrimSpace(query.Get("title"))
	}
	if req.SortOrder == "" {
		req.SortOrder = strings.TrimSpace(query.Get("sort_order"))
	}
	if req.Operator == "" {
		req.Operator = strings.TrimSpace(query.Get("operator"))
	}
	if raw := strings.TrimSpace(query.Get("score")); raw != "" && req.Score == 0 {
		if value, err := strconv.ParseInt(raw, 10, 64); err == nil {
			req.Score = value
		}
	}
	if raw := strings.TrimSpace(query.Get("subscore")); raw != "" && req.Subscore == 0 {
		if value, err := strconv.ParseInt(raw, 10, 64); err == nil {
			req.Subscore = value
		}
	}
	if raw := strings.TrimSpace(query.Get("max_size")); raw != "" && req.MaxSize == 0 {
		if value, err := strconv.Atoi(raw); err == nil {
			req.MaxSize = value
		}
	}
}

func fillSanctionQuery(r *http.Request, req *sanctionRequest) {
	query := r.URL.Query()
	if req.ID == "" {
		req.ID = strings.TrimSpace(query.Get("id"))
	}
	if req.Subject == "" {
		req.Subject = strings.TrimSpace(query.Get("subject"))
	}
	if req.Scope == "" {
		req.Scope = strings.TrimSpace(query.Get("scope"))
	}
	if req.Kind == "" {
		req.Kind = strings.TrimSpace(query.Get("kind"))
	}
	if req.Reason == "" {
		req.Reason = strings.TrimSpace(query.Get("reason"))
	}
	if req.Source == "" {
		req.Source = strings.TrimSpace(query.Get("source"))
	}
	if raw := strings.TrimSpace(query.Get("ttl_seconds")); raw != "" && req.TTLSecond == 0 {
		if value, err := strconv.ParseInt(raw, 10, 64); err == nil {
			req.TTLSecond = value
		}
	}
}

func (req sanctionRequest) toSanction() moderation.Sanction {
	kind := moderation.SanctionKind(strings.TrimSpace(req.Kind))
	if kind == "" {
		kind = moderation.SanctionBan
	}
	scope := strings.TrimSpace(req.Scope)
	if scope == "" {
		scope = "global"
	}
	until := req.Until
	if until.IsZero() && req.TTLSecond > 0 {
		until = time.Now().UTC().Add(time.Duration(req.TTLSecond) * time.Second)
	}
	source := strings.TrimSpace(req.Source)
	if source == "" {
		source = "admin"
	}
	return moderation.Sanction{
		ID:      req.ID,
		Subject: req.Subject,
		Scope:   scope,
		Kind:    kind,
		Reason:  req.Reason,
		Source:  source,
		Until:   until,
		Meta:    req.Meta,
	}
}

func (s *Service) applyLbRepair(r *http.Request, req lbRepairReq) (map[string]any, error) {
	action := strings.TrimSpace(req.Action)
	if action == "" {
		action = "submit"
	}
	switch action {
	case "create":
		definition, err := s.leaderboards.Create(r.Context(), leaderboard.Definition{
			ID:        req.LeaderboardID,
			Title:     req.Title,
			SortOrder: req.SortOrder,
			Operator:  req.Operator,
			MaxSize:   req.MaxSize,
			Metadata:  req.Metadata,
		})
		return map[string]any{"status": "created", "definition": definition}, err
	case "submit", "set_score":
		record, err := s.leaderboards.Submit(r.Context(), req.LeaderboardID, leaderboard.Submission{
			OwnerID:  req.OwnerID,
			Score:    req.Score,
			Subscore: req.Subscore,
			Metadata: req.Metadata,
		})
		return map[string]any{"status": "updated", "record": record}, err
	case "delete_record":
		record, err := s.leaderboards.DeleteRecord(r.Context(), req.LeaderboardID, req.OwnerID)
		return map[string]any{"status": "record_deleted", "record": record}, err
	case "reset":
		definition, err := s.leaderboards.Reset(r.Context(), req.LeaderboardID)
		return map[string]any{"status": "reset", "definition": definition}, err
	case "delete_leaderboard":
		definition, err := s.leaderboards.Delete(r.Context(), req.LeaderboardID)
		return map[string]any{"status": "deleted", "definition": definition}, err
	default:
		return nil, leaderboard.ErrInvalidDefinition
	}
}

func queryInt(r *http.Request, key string, fallback int) int {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}
