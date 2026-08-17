package app

import (
	"net/url"
	"strconv"
	"strings"

	"longheng.io/server/internal/platform/audit"
	"longheng.io/server/internal/platform/config"
)

func auditDebugPayload(cfg config.ServiceConfig, auditLogger *audit.Logger, query url.Values) map[string]any {
	events := []audit.Event(nil)
	if auditLogger != nil {
		events = auditLogger.Snapshot()
	}
	filtered := filterAuditEvents(events, query)
	return map[string]any{
		"enabled": cfg.Audit.Enabled,
		"kind":    cfg.Audit.Kind,
		"query":   auditQuerySummary(query),
		"count":   len(filtered),
		"events":  filtered,
	}
}

func filterAuditEvents(events []audit.Event, query url.Values) []audit.Event {
	if len(events) == 0 {
		return nil
	}
	out := make([]audit.Event, 0, len(events))
	for _, event := range events {
		if !auditEventMatches(event, query) {
			continue
		}
		out = append(out, event)
	}
	if limit := auditQueryLimit(query); limit > 0 && len(out) > limit {
		return append([]audit.Event(nil), out[len(out)-limit:]...)
	}
	return out
}

func auditEventMatches(event audit.Event, query url.Values) bool {
	return auditQueryEquals(query, "actor", event.Actor) &&
		auditQueryEquals(query, "action", event.Action) &&
		auditQueryEquals(query, "target", event.Target) &&
		auditQueryEquals(query, "service", event.Service) &&
		auditQueryEquals(query, "node_id", event.NodeID) &&
		auditQueryEquals(query, "trace_id", event.TraceID) &&
		auditQueryEquals(query, "request_id", event.RequestID) &&
		auditQueryEquals(query, "idempotency_key", event.IdempotencyKey) &&
		auditQueryEquals(query, "reason_code", event.ReasonCode) &&
		auditQueryEquals(query, "scope", event.Fields["scope"]) &&
		auditQueryEquals(query, "status", event.Fields["status"])
}

func auditQueryEquals(query url.Values, key string, value string) bool {
	want := strings.TrimSpace(query.Get(key))
	if want == "" {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(value), want)
}

func auditQueryLimit(query url.Values) int {
	raw := strings.TrimSpace(query.Get("limit"))
	if raw == "" {
		return 0
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		return 0
	}
	if limit > 1000 {
		return 1000
	}
	return limit
}

func auditQuerySummary(query url.Values) map[string]string {
	keys := []string{
		"actor",
		"action",
		"target",
		"service",
		"node_id",
		"trace_id",
		"request_id",
		"idempotency_key",
		"reason_code",
		"scope",
		"status",
		"limit",
	}
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		if value := strings.TrimSpace(query.Get(key)); value != "" {
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
