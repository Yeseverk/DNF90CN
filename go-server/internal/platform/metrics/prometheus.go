package metrics

import (
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
)

var metricNameRE = regexp.MustCompile(`[^a-zA-Z0-9_:]`)
var labelNameRE = regexp.MustCompile(`[^a-zA-Z0-9_]`)

type prometheusSample struct {
	name       string
	typ        Type
	labels     string
	value      int64
	float      bool
	floatValue float64
}

func Handler(registry *Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		ObserveRuntime(registry)
		registry.RunObservers()
		WritePrometheus(w, registry.Snapshot())
	}
}

func WritePrometheus(w interface{ Write([]byte) (int, error) }, samples []Sample) {
	seenTypes := make(map[string]Type)
	for _, sample := range normalizeSamples(samples) {
		// Histogram 多个子样本共享同一 base 名字 (<base>_bucket / _count / _sum)，
		// TYPE 标注必须按 family 去重；其他类型按完整 metric 名去重保持原行为。
		typeKey := sample.name
		if sample.typ == TypeHistogram {
			typeKey = histogramFamily(sample.name)
		}
		if existing, ok := seenTypes[typeKey]; ok {
			if existing != sample.typ {
				continue
			}
		} else {
			seenTypes[typeKey] = sample.typ
			_, _ = fmt.Fprintf(w, "# TYPE %s %s\n", typeKey, prometheusType(sample.typ))
		}
		if sample.float {
			// 用 %g 而非 %f：histogram 的 sum 既要避免 1.0e-06 这种科学计数，
			// 又要避免拖一长串无意义的零；%g 在大多数 latency 场景下输出可读小数。
			_, _ = fmt.Fprintf(w, "%s%s %g\n", sample.name, sample.labels, sample.floatValue)
		} else {
			_, _ = fmt.Fprintf(w, "%s%s %d\n", sample.name, sample.labels, sample.value)
		}
	}
}

// histogramFamily 把 <base>_bucket / <base>_count / <base>_sum
// 还原成 <base>，用于 TYPE 标注去重。未识别后缀时返回原名，等价于"该样本自己就是 family"。
func histogramFamily(name string) string {
	for _, suffix := range []string{"_sum", "_count", "_bucket"} {
		if strings.HasSuffix(name, suffix) {
			return strings.TrimSuffix(name, suffix)
		}
	}
	return name
}

func normalizeSamples(samples []Sample) []prometheusSample {
	merged := make(map[string]prometheusSample, len(samples))
	for _, sample := range samples {
		name := normalizeName(sample.Name)
		if name == "" {
			continue
		}
		labels := formatLabels(sample.Labels)
		key := string(sample.Type) + "\x00" + name + "\x00" + labels
		current := merged[key]
		if current.name == "" {
			current = prometheusSample{name: name, typ: sample.Type, labels: labels}
		}
		// Histogram observers 写入的是绝对值（替换语义，跟 gauge 一致），不能累加；
		// 否则 bucket / count / sum 会被多次叠加。Gauge 同理。
		switch sample.Type {
		case TypeGauge, TypeHistogram:
			current.value = sample.Value
			current.float = sample.Float
			current.floatValue = sample.FloatValue
		default:
			current.value += sample.Value
		}
		merged[key] = current
	}
	out := make([]prometheusSample, 0, len(merged))
	for _, sample := range merged {
		out = append(out, sample)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].name != out[j].name {
			return out[i].name < out[j].name
		}
		if out[i].typ != out[j].typ {
			return out[i].typ < out[j].typ
		}
		return out[i].labels < out[j].labels
	})
	return out
}

func prometheusType(t Type) string {
	switch t {
	case TypeGauge:
		return "gauge"
	case TypeHistogram:
		return "histogram"
	default:
		return "counter"
	}
}

func normalizeName(name string) string {
	return normalizeIdentifier(name, metricNameRE, true)
}

func normalizeLabelName(name string) string {
	return normalizeIdentifier(name, labelNameRE, false)
}

func normalizeIdentifier(name string, invalid *regexp.Regexp, allowColonFirst bool) string {
	name = strings.TrimSpace(name)
	name = invalid.ReplaceAllString(name, "_")
	name = strings.Trim(name, "_")
	if name == "" {
		return ""
	}
	first := name[0]
	if isAlpha(first) || first == '_' || (allowColonFirst && first == ':') {
		return name
	}
	return "_" + name
}

func isAlpha(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func formatLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	normalized := make(map[string]string, len(labels))
	keys := make([]string, 0, len(labels))
	originalKeys := make([]string, 0, len(labels))
	for key := range labels {
		originalKeys = append(originalKeys, key)
	}
	sort.Strings(originalKeys)
	for _, key := range originalKeys {
		name := normalizeLabelName(key)
		if name != "" {
			if _, exists := normalized[name]; exists {
				continue
			}
			normalized[name] = labels[key]
			keys = append(keys, name)
		}
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteByte('{')
	for idx, key := range keys {
		if idx > 0 {
			b.WriteByte(',')
		}
		b.WriteString(key)
		b.WriteString(`="`)
		b.WriteString(escapeLabel(normalized[key]))
		b.WriteByte('"')
	}
	b.WriteByte('}')
	return b.String()
}

func escapeLabel(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, "\n", `\n`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return value
}
