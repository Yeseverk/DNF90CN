package metrics

import (
	"strings"
	"time"
)

// 这些名称是框架层的最低共同语言，避免每个项目自造一套不可聚合的运营指标。
const (
	REDRequestsMetric = "requests_total"
	REDErrorsMetric   = "errors_total"
	REDDurationMetric = "request_duration"

	OnlinePlayersMetric       = "online_players"
	LoginEventsMetric         = "login_events_total"
	PaymentEventsMetric       = "payment_events_total"
	PaymentAmountMicrosMetric = "payment_amount_micros_total"
	RetentionEventsMetric     = "retention_events_total"
)

// RED 把 rate、errors、duration 绑定到同一套 label 约束，避免 SLO 口径漂移。
type RED struct {
	scope    Scope
	service  string
	duration *Histogram
}

type REDOptions struct {
	Buckets []float64
}

func NewRED(scope Scope, service string, opts REDOptions) *RED {
	service = strings.TrimSpace(service)
	if service == "" {
		service = "default"
	}
	baseLabels := map[string]string{"service": service}
	histogram := NewHistogram(scope, REDDurationMetric, HistogramOptions{
		Buckets: opts.Buckets,
		Labels:  baseLabels,
	})
	return &RED{
		scope:    scope.WithLabels(baseLabels),
		service:  service,
		duration: histogram,
	}
}

func (r *RED) Record(route string, code string, elapsed time.Duration) {
	if r == nil {
		return
	}
	route = sanitizeLabelValue(route, "unknown")
	code = sanitizeLabelValue(code, "OK")
	labels := map[string]string{"route": route, "code": code}
	r.scope.Inc(REDRequestsMetric, labels)
	if isErrorCode(code) {
		r.scope.Inc(REDErrorsMetric, labels)
	}
	if elapsed < 0 {
		elapsed = 0
	}
	if r.duration != nil {
		r.duration.ObserveWithLabels(elapsed.Seconds(), labels)
	}
}

func (r *RED) RecordError(route string, code string, elapsed time.Duration) {
	if strings.TrimSpace(code) == "" {
		code = "INTERNAL_ERR"
	}
	r.Record(route, code, elapsed)
}

func (r *RED) Service() string {
	if r == nil {
		return ""
	}
	return r.service
}

func isErrorCode(code string) bool {
	upper := strings.ToUpper(strings.TrimSpace(code))
	if upper == "" || upper == "OK" {
		return false
	}
	if strings.HasPrefix(upper, "ERR") {
		return true
	}
	switch upper {
	case "FAIL", "FAILED", "INTERNAL_ERR", "TIMEOUT", "PANIC":
		return true
	}
	return len(upper) == 3 && upper[0] == '5'
}

// OnlinePlayers 固定 segment、region、server_id 三个维度，避免在线人数指标被 user_id 拉爆。
type OnlinePlayers struct {
	scope Scope
}

func NewOnlinePlayers(scope Scope, service string) *OnlinePlayers {
	service = strings.TrimSpace(service)
	if service == "" {
		service = "default"
	}
	return &OnlinePlayers{
		scope: scope.WithLabels(map[string]string{"service": service}),
	}
}

func (o *OnlinePlayers) Set(segment, region, serverID string, count int64) {
	if o == nil {
		return
	}
	labels := map[string]string{
		"segment":   sanitizeLabelValue(segment, "all"),
		"region":    sanitizeLabelValue(region, "any"),
		"server_id": sanitizeLabelValue(serverID, "any"),
	}
	o.scope.SetGauge(OnlinePlayersMetric, labels, count)
}

func (o *OnlinePlayers) Add(segment, region, serverID string, delta int64) {
	if o == nil {
		return
	}
	labels := map[string]string{
		"segment":   sanitizeLabelValue(segment, "all"),
		"region":    sanitizeLabelValue(region, "any"),
		"server_id": sanitizeLabelValue(serverID, "any"),
	}
	o.scope.AddGauge(OnlinePlayersMetric, labels, delta)
}

func sanitizeLabelValue(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	if len(value) > 64 {
		return fallback
	}
	for _, r := range value {
		if r < 0x20 {
			return fallback
		}
	}
	return value
}
