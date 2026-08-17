package logic

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"longheng.io/server/internal/platform/httpx"
	"longheng.io/server/internal/platform/readmodel"
	"longheng.io/server/internal/reference/player"
)

func (s *Service) regReadModelAdmins() {
	if s.readModels == nil {
		s.readModels = readmodel.NewAdminRegistry()
	}
	s.readModels.Register(readmodel.AdminModel{
		Name: "player_summaries",
		Search: func(ctx context.Context, values url.Values) (any, error) {
			query, err := parsePlayerSumQuery(values)
			if err != nil {
				return nil, err
			}
			summaries, err := s.players.SearchSummaries(ctx, query)
			if err != nil {
				return nil, err
			}
			return map[string]any{"summaries": summaries}, nil
		},
		Rebuild: func(ctx context.Context, values url.Values) (any, error) {
			options, err := parseSummaryRebuild(values)
			if err != nil {
				return nil, err
			}
			return s.players.RebuildSummaries(ctx, options)
		},
	})
}

func (s *Service) handleSummaries(w http.ResponseWriter, r *http.Request) {
	s.handleViewSearch("player_summaries")(w, r)
}

func (s *Service) handleSummaryRebuild(w http.ResponseWriter, r *http.Request) {
	s.handleViewRebuild("player_summaries")(w, r)
}

func (s *Service) handleViewSearch(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		result, found, err := s.readModels.Search(r.Context(), name, r.URL.Query())
		if !found {
			httpx.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "read model not found"})
			return
		}
		if err != nil {
			status := http.StatusInternalServerError
			if readmodel.IsBadRequest(err) {
				status = http.StatusBadRequest
			}
			httpx.WriteJSON(w, status, map[string]string{"error": err.Error()})
			return
		}
		httpx.WriteJSON(w, http.StatusOK, result)
	}
}

func (s *Service) handleViewRebuild(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			httpx.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}

		result, found, err := s.readModels.Rebuild(r.Context(), name, r.URL.Query())
		if !found {
			httpx.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "read model not found"})
			return
		}
		if err != nil {
			s.incMetric("logic_profile_summary_rebuild_total", map[string]string{"result": "error"})
			status := http.StatusInternalServerError
			if readmodel.IsBadRequest(err) {
				status = http.StatusBadRequest
			}
			httpx.WriteJSON(w, status, map[string]string{"error": err.Error()})
			return
		}
		s.incMetric("logic_profile_summary_rebuild_total", map[string]string{"result": "ok"})
		httpx.WriteJSON(w, http.StatusOK, result)
	}
}

func parsePlayerSumQuery(values url.Values) (player.PlayerSummaryQuery, error) {
	query := player.PlayerSummaryQuery{
		AccountIDs:   splitCSV(values.Get("account_id")),
		RoleIDs:      splitCSV(values.Get("role_id")),
		State:        strings.TrimSpace(values.Get("state")),
		NameContains: strings.TrimSpace(values.Get("name")),
	}
	if online := strings.TrimSpace(values.Get("online")); online != "" {
		value, err := strconv.ParseBool(online)
		if err != nil {
			return player.PlayerSummaryQuery{}, readmodel.BadRequest("online must be true or false")
		}
		query.OnlineOnly = value
	}
	if limit := strings.TrimSpace(values.Get("limit")); limit != "" {
		value, err := strconv.Atoi(limit)
		if err != nil || value < 0 {
			return player.PlayerSummaryQuery{}, readmodel.BadRequest("limit must be a non-negative integer")
		}
		query.Limit = value
	}
	return query, nil
}

func parseSummaryRebuild(values url.Values) (player.SummaryRebuildOptions, error) {
	options := player.SummaryRebuildOptions{
		AfterAccountID: strings.TrimSpace(values.Get("after_account_id")),
	}
	if batchSize := strings.TrimSpace(values.Get("batch_size")); batchSize != "" {
		value, err := strconv.Atoi(batchSize)
		if err != nil || value <= 0 {
			return player.SummaryRebuildOptions{}, readmodel.BadRequest("batch_size must be a positive integer")
		}
		options.BatchSize = value
	}
	if maxProfiles := strings.TrimSpace(values.Get("max_profiles")); maxProfiles != "" {
		value, err := strconv.Atoi(maxProfiles)
		if err != nil || value <= 0 {
			return player.SummaryRebuildOptions{}, readmodel.BadRequest("max_profiles must be a positive integer")
		}
		options.MaxProfiles = value
	}
	return options, nil
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
