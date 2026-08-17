package otelx

import (
	"bytes"
	"context"
	"encoding/json"
	"hash/fnv"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"longheng.io/server/internal/platform/config"
	"longheng.io/server/internal/platform/tracing"
)

type Shutdown func(context.Context) error

type Recorder interface {
	ExportSpan(context.Context, tracing.Span)
}

type HTTPExporter struct {
	endpoint    string
	serviceName string
	nodeID      string
	environment string
	sampleRatio float64
	timeout     time.Duration
	logger      *slog.Logger
	client      *http.Client
	inflight    chan struct{}

	closed      atomic.Bool
	lifecycleMu sync.Mutex
	wg          sync.WaitGroup
}

const defMaxExports = 256

func Setup(ctx context.Context, cfg config.OTelSection, serviceName, nodeID, environment string, logger *slog.Logger) (Shutdown, error) {
	if !cfg.Enabled {
		tracing.SetExporter(nil)
		return func(context.Context) error { return nil }, nil
	}
	endpoint, err := normalizeEndpoint(cfg.Endpoint, cfg.Insecure)
	if err != nil {
		return nil, err
	}
	serviceName = firstNonEmpty(cfg.ServiceName, serviceName, "longheng")
	timeout := time.Duration(cfg.BatchTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	sampleRatio := cfg.SampleRatio
	if sampleRatio < 0 || sampleRatio > 1 {
		sampleRatio = 1
	}
	exporter := &HTTPExporter{
		endpoint:    endpoint,
		serviceName: serviceName,
		nodeID:      strings.TrimSpace(nodeID),
		environment: strings.TrimSpace(environment),
		sampleRatio: sampleRatio,
		timeout:     timeout,
		logger:      logger,
		client:      &http.Client{Timeout: timeout},
		inflight:    make(chan struct{}, defMaxExports),
	}
	tracing.SetExporter(exporter)
	if logger != nil {
		logger.Info("opentelemetry http trace export enabled", "endpoint", endpoint, "service_name", serviceName, "sample_ratio", sampleRatio)
	}
	return exporter.Shutdown, nil
}

func (e *HTTPExporter) ExportSpan(ctx context.Context, span tracing.Span) {
	if e == nil || e.closed.Load() || !e.shouldSample(span) {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	payload, err := json.Marshal(e.payload(span))
	if err != nil {
		if e.logger != nil {
			e.logger.Warn("otel marshal span failed", "trace_id", span.TraceID, "error", err)
		}
		return
	}
	release, ok := e.tryAcquireExportSlot(span.TraceID)
	if !ok {
		return
	}
	// Shutdown 必须在开始 Wait 前关闭准入；这里在同一把锁内二次检查并 Add，
	// 防止 ExportSpan 通过快速检查后在 Wait 已返回时才加入 WaitGroup。
	e.lifecycleMu.Lock()
	if e.closed.Load() {
		e.lifecycleMu.Unlock()
		release()
		return
	}
	e.wg.Add(1)
	e.lifecycleMu.Unlock()
	go func() {
		defer e.wg.Done()
		defer release()
		reqCtx, cancel := context.WithTimeout(ctx, e.timeout)
		defer cancel()
		req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, e.endpoint, bytes.NewReader(payload))
		if err != nil {
			if e.logger != nil {
				e.logger.Warn("otel create export request failed", "trace_id", span.TraceID, "error", err)
			}
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := e.client.Do(req)
		if err != nil {
			if e.logger != nil {
				e.logger.Warn("otel export span failed", "trace_id", span.TraceID, "error", err)
			}
			return
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			if e.logger != nil {
				e.logger.Warn("otel export span returned non-2xx", "trace_id", span.TraceID, "status", resp.StatusCode)
			}
		}
	}()
}

func (e *HTTPExporter) tryAcquireExportSlot(traceID string) (func(), bool) {
	if e.inflight == nil {
		return func() {}, true
	}
	select {
	case e.inflight <- struct{}{}:
		return func() { <-e.inflight }, true
	default:
		if e.logger != nil {
			e.logger.Warn("otel export queue full; dropping span", "trace_id", traceID)
		}
		return nil, false
	}
}

func (e *HTTPExporter) Shutdown(ctx context.Context) error {
	if e == nil {
		return nil
	}
	e.lifecycleMu.Lock()
	e.closed.Store(true)
	e.lifecycleMu.Unlock()
	tracing.SetExporter(nil)
	done := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(done)
	}()
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	}
}

func (e *HTTPExporter) payload(span tracing.Span) map[string]any {
	attrs := []map[string]any{
		stringAttribute("service.name", e.serviceName),
		stringAttribute("service.instance.id", e.nodeID),
		stringAttribute("deployment.environment.name", e.environment),
	}
	spanAttrs := make([]map[string]any, 0, len(span.Attributes))
	for key, value := range span.Attributes {
		spanAttrs = append(spanAttrs, stringAttribute(key, value))
	}
	status := map[string]any{"code": "STATUS_CODE_OK"}
	if span.Error != "" {
		status = map[string]any{
			"code":    "STATUS_CODE_ERROR",
			"message": span.Error,
		}
		spanAttrs = append(spanAttrs, stringAttribute("error.message", span.Error))
	}
	otelSpan := map[string]any{
		"traceId":           span.TraceID,
		"spanId":            span.SpanID,
		"name":              span.Name,
		"startTimeUnixNano": formatUnixNano(span.StartedAt),
		"endTimeUnixNano":   formatUnixNano(span.EndedAt),
		"attributes":        spanAttrs,
		"status":            status,
	}
	if span.ParentSpanID != "" {
		otelSpan["parentSpanId"] = span.ParentSpanID
	}
	return map[string]any{
		"resourceSpans": []map[string]any{
			{
				"resource": map[string]any{"attributes": attrs},
				"scopeSpans": []map[string]any{
					{
						"scope": map[string]any{"name": "longheng.io/server/runtime"},
						"spans": []map[string]any{otelSpan},
					},
				},
			},
		},
	}
}

func (e *HTTPExporter) shouldSample(span tracing.Span) bool {
	if span.Error != "" {
		return true
	}
	if e.sampleRatio >= 1 {
		return true
	}
	if e.sampleRatio <= 0 {
		return false
	}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(span.TraceID))
	return float64(hash.Sum32())/float64(^uint32(0)) <= e.sampleRatio
}

func normalizeEndpoint(raw string, insecure bool) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = "127.0.0.1:4318"
	}
	if !strings.Contains(raw, "://") {
		scheme := "https"
		if insecure {
			scheme = "http"
		}
		raw = scheme + "://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if parsed.Path == "" || parsed.Path == "/" {
		parsed.Path = "/v1/traces"
	}
	return parsed.String(), nil
}

func stringAttribute(key, value string) map[string]any {
	return map[string]any{
		"key": key,
		"value": map[string]any{
			"stringValue": value,
		},
	}
}

func formatUnixNano(ts time.Time) string {
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	return strconv.FormatInt(ts.UnixNano(), 10)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
