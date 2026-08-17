package metrics

import (
	"context"
	"log/slog"
	"strings"
)

type Scope struct {
	registry *Registry
	prefix   string
	labels   map[string]string
}

type DumpOptions struct {
	Level      slog.Level
	Message    string
	MaxSamples int
	Labels     map[string]string
}

func (r *Registry) Scope(parts ...string) Scope {
	return Scope{registry: r, prefix: JoinName(parts...)}
}

func (s Scope) Scope(parts ...string) Scope {
	prefix := JoinName(s.prefix, JoinName(parts...))
	return Scope{registry: s.registry, prefix: prefix, labels: cloneLabels(s.labels)}
}

func (s Scope) WithLabels(labels map[string]string) Scope {
	s.labels = mergeLabels(s.labels, labels)
	return s
}

func (s Scope) Name(parts ...string) string {
	return JoinName(s.prefix, JoinName(parts...))
}

func (s Scope) Labels(labels map[string]string) map[string]string {
	return mergeLabels(s.labels, labels)
}

func (s Scope) Inc(name string, labels map[string]string) {
	s.Add(name, labels, 1)
}

func (s Scope) Add(name string, labels map[string]string, delta int64) {
	if s.registry == nil {
		return
	}
	s.registry.Add(s.Name(name), s.Labels(labels), delta)
}

func (s Scope) SetCounter(name string, labels map[string]string, value int64) {
	if s.registry == nil {
		return
	}
	s.registry.SetCounter(s.Name(name), s.Labels(labels), value)
}

func (s Scope) SetGauge(name string, labels map[string]string, value int64) {
	if s.registry == nil {
		return
	}
	s.registry.SetGauge(s.Name(name), s.Labels(labels), value)
}

func (s Scope) AddGauge(name string, labels map[string]string, delta int64) {
	if s.registry == nil {
		return
	}
	s.registry.AddGauge(s.Name(name), s.Labels(labels), delta)
}

func JoinName(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		for _, field := range strings.FieldsFunc(part, func(r rune) bool {
			return r == '.' || r == '/' || r == '-' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
		}) {
			field = strings.Trim(field, "_")
			if field != "" {
				out = append(out, field)
			}
		}
	}
	return strings.Join(out, "_")
}

func (r *Registry) DumpToLogger(ctx context.Context, logger *slog.Logger, opts DumpOptions) {
	if r == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	msg := strings.TrimSpace(opts.Message)
	if msg == "" {
		msg = "metrics snapshot"
	}
	r.RunObservers()
	samples := r.Snapshot()
	sampleCount := len(samples)
	if opts.MaxSamples > 0 && len(samples) > opts.MaxSamples {
		samples = samples[:opts.MaxSamples]
	}
	attrs := []slog.Attr{
		slog.Int("sample_count", sampleCount),
		slog.Any("samples", samples),
	}
	for key, value := range opts.Labels {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		attrs = append(attrs, slog.String(key, value))
	}
	logger.LogAttrs(ctx, opts.Level, msg, attrs...)
}

func mergeLabels(base, extra map[string]string) map[string]string {
	if len(base) == 0 && len(extra) == 0 {
		return nil
	}
	out := make(map[string]string, len(base)+len(extra))
	for key, value := range base {
		if strings.TrimSpace(key) != "" {
			out[key] = value
		}
	}
	for key, value := range extra {
		if strings.TrimSpace(key) != "" {
			out[key] = value
		}
	}
	return out
}
