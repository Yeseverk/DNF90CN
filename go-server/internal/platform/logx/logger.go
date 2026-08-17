package logx

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
)

type LevelController struct {
	level slog.LevelVar
}

type Format string

const (
	FormatText Format = "text"
	FormatJSON Format = "json"
)

type HandlerProvider interface {
	NewHandler(io.Writer, *slog.HandlerOptions) slog.Handler
}

type HandlerProviderFunc func(io.Writer, *slog.HandlerOptions) slog.Handler

func (f HandlerProviderFunc) NewHandler(w io.Writer, options *slog.HandlerOptions) slog.Handler {
	if f == nil {
		return nil
	}
	return f(w, options)
}

type Options struct {
	Service         string
	NodeID          string
	Env             string
	Controller      *LevelController
	Format          Format
	Output          io.Writer
	ExtraOutputs    []io.Writer
	HandlerProvider HandlerProvider
	RedactKeys      []string
	SampleEvery     uint64
}

var defaultLevel LevelController

func New(service, nodeID, env string) *slog.Logger {
	return NewWithLevel(service, nodeID, env, &defaultLevel)
}

func NewWithLevel(service, nodeID, env string, controller *LevelController) *slog.Logger {
	return NewWithOptions(Options{Service: service, NodeID: nodeID, Env: env, Controller: controller})
}

func NewWithOptions(options Options) *slog.Logger {
	controller := options.Controller
	if controller == nil {
		controller = &defaultLevel
	}
	writer := options.Output
	if writer == nil {
		writer = os.Stdout
	}
	handlerOptions := &slog.HandlerOptions{Level: &controller.level}
	writers := append([]io.Writer{writer}, options.ExtraOutputs...)
	handlers := make([]slog.Handler, 0, len(writers))
	for _, item := range writers {
		if item == nil {
			continue
		}
		handlers = append(handlers, newBaseHandler(item, options.Format, options.HandlerProvider, handlerOptions))
	}
	if len(handlers) == 0 {
		handlers = append(handlers, newBaseHandler(io.Discard, options.Format, options.HandlerProvider, handlerOptions))
	}
	var handler slog.Handler
	if len(handlers) == 1 {
		handler = handlers[0]
	} else {
		handler = fanoutHandler{handlers: handlers}
	}
	if len(options.RedactKeys) > 0 {
		handler = newRedactingHandler(handler, options.RedactKeys)
	}
	if options.SampleEvery > 1 {
		handler = &samplingHandler{next: handler, every: options.SampleEvery}
	}
	return slog.New(handler).With(
		"service", options.Service,
		"node_id", options.NodeID,
		"env", options.Env,
	)
}

func DefaultLevel() slog.Level {
	return defaultLevel.Level()
}

func SetDefaultLevel(value string) (slog.Level, error) {
	return defaultLevel.SetText(value)
}

func (c *LevelController) Level() slog.Level {
	if c == nil {
		return slog.LevelInfo
	}
	return c.level.Level()
}

func (c *LevelController) Set(level slog.Level) {
	if c == nil {
		return
	}
	c.level.Set(level)
}

func (c *LevelController) SetText(value string) (slog.Level, error) {
	level, err := ParseLevel(value)
	if err != nil {
		return slog.LevelInfo, err
	}
	c.Set(level)
	return level, nil
}

func ParseLevel(value string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return slog.LevelDebug, nil
	case "", "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("unsupported log level %q", value)
	}
}

func newBaseHandler(w io.Writer, format Format, provider HandlerProvider, options *slog.HandlerOptions) slog.Handler {
	if provider != nil {
		if handler := provider.NewHandler(w, options); handler != nil {
			return handler
		}
	}
	switch format {
	case FormatJSON:
		return slog.NewJSONHandler(w, options)
	case "", FormatText:
		return slog.NewTextHandler(w, options)
	default:
		return slog.NewTextHandler(w, options)
	}
}

type fanoutHandler struct {
	handlers []slog.Handler
}

func (h fanoutHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (h fanoutHandler) Handle(ctx context.Context, record slog.Record) error {
	var firstErr error
	for _, handler := range h.handlers {
		if err := handler.Handle(ctx, record.Clone()); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (h fanoutHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make([]slog.Handler, 0, len(h.handlers))
	for _, handler := range h.handlers {
		out = append(out, handler.WithAttrs(attrs))
	}
	return fanoutHandler{handlers: out}
}

func (h fanoutHandler) WithGroup(name string) slog.Handler {
	out := make([]slog.Handler, 0, len(h.handlers))
	for _, handler := range h.handlers {
		out = append(out, handler.WithGroup(name))
	}
	return fanoutHandler{handlers: out}
}

type redactingHandler struct {
	next slog.Handler
	keys map[string]struct{}
}

func newRedactingHandler(next slog.Handler, keys []string) slog.Handler {
	set := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		key = strings.ToLower(strings.TrimSpace(key))
		if key != "" {
			set[key] = struct{}{}
		}
	}
	if len(set) == 0 {
		return next
	}
	return redactingHandler{next: next, keys: set}
}

func (h redactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h redactingHandler) Handle(ctx context.Context, record slog.Record) error {
	out := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	record.Attrs(func(attr slog.Attr) bool {
		out.AddAttrs(h.redactAttr(attr))
		return true
	})
	return h.next.Handle(ctx, out)
}

func (h redactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	redacted := make([]slog.Attr, 0, len(attrs))
	for _, attr := range attrs {
		redacted = append(redacted, h.redactAttr(attr))
	}
	return redactingHandler{next: h.next.WithAttrs(redacted), keys: h.keys}
}

func (h redactingHandler) WithGroup(name string) slog.Handler {
	return redactingHandler{next: h.next.WithGroup(name), keys: h.keys}
}

func (h redactingHandler) redactAttr(attr slog.Attr) slog.Attr {
	attr.Value = attr.Value.Resolve()
	if _, ok := h.keys[strings.ToLower(attr.Key)]; ok {
		return slog.String(attr.Key, "[redacted]")
	}
	if attr.Value.Kind() == slog.KindGroup {
		group := attr.Value.Group()
		out := make([]slog.Attr, 0, len(group))
		for _, item := range group {
			out = append(out, h.redactAttr(item))
		}
		return slog.Group(attr.Key, attrsToAny(out)...)
	}
	return attr
}

func attrsToAny(attrs []slog.Attr) []any {
	out := make([]any, 0, len(attrs))
	for _, attr := range attrs {
		out = append(out, attr)
	}
	return out
}

type samplingHandler struct {
	next  slog.Handler
	every uint64
	seen  uint64
}

func (h *samplingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *samplingHandler) Handle(ctx context.Context, record slog.Record) error {
	if h.every > 1 && (atomic.AddUint64(&h.seen, 1)-1)%h.every != 0 {
		return nil
	}
	return h.next.Handle(ctx, record)
}

func (h *samplingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &samplingHandler{next: h.next.WithAttrs(attrs), every: h.every}
}

func (h *samplingHandler) WithGroup(name string) slog.Handler {
	return &samplingHandler{next: h.next.WithGroup(name), every: h.every}
}
