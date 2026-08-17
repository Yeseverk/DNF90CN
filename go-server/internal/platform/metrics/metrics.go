package metrics

import (
	"sort"
	"strings"
	"sync"
)

type Type string

const (
	TypeCounter   Type = "counter"
	TypeGauge     Type = "gauge"
	TypeHistogram Type = "histogram"
)

// Sample 是 registry / observer 输出的最小单元。Value 用 int64 适合 counter / gauge
// 以及 histogram 的 bucket / count；当样本表示浮点值（如 histogram 的 sum）时，
// 设置 Float=true 并把数值写到 FloatValue。这种双字段写法兼容已有所有 counter/gauge
// 调用方，新增字段对存量 Sample 字面量保持零值，不影响 reflect.DeepEqual 比较。
type Sample struct {
	Name       string            `json:"name"`
	Type       Type              `json:"type"`
	Labels     map[string]string `json:"labels,omitempty"`
	Value      int64             `json:"value"`
	Float      bool              `json:"float,omitempty"`
	FloatValue float64           `json:"float_value,omitempty"`
}

type Observer func(*Registry)

type observerEntry struct {
	name string
	fn   Observer
}

type Registry struct {
	mu        sync.RWMutex
	samples   map[string]Sample
	observers []observerEntry
}

func New() *Registry {
	return &Registry{samples: make(map[string]Sample)}
}

// RegisterObserver 注册 scrape 时执行的回调。
// Snapshot 向 Prometheus 客户端返回样本前会调用 observer；observer 应保持低成本
// （例如 O(1) Redis SCARD 或本地 atomics），并且必须能承受并发调用。
// 使用同名重新注册会替换旧回调；fn 为 nil 表示注销。
func (r *Registry) RegisterObserver(name string, fn Observer) {
	if r == nil || strings.TrimSpace(name) == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for idx := range r.observers {
		if r.observers[idx].name == name {
			if fn == nil {
				r.observers = append(r.observers[:idx], r.observers[idx+1:]...)
				return
			}
			r.observers[idx].fn = fn
			return
		}
	}
	if fn == nil {
		return
	}
	r.observers = append(r.observers, observerEntry{name: name, fn: fn})
}

// RunObservers 调用所有已注册 observer。
// Prometheus handler 和 /debug/metrics 会在序列化前调用它，让观测样本反映当前状态。
// observer 会顺序执行，不能长时间阻塞。
func (r *Registry) RunObservers() {
	if r == nil {
		return
	}
	r.mu.RLock()
	entries := make([]observerEntry, len(r.observers))
	copy(entries, r.observers)
	r.mu.RUnlock()
	for _, entry := range entries {
		if entry.fn == nil {
			continue
		}
		entry.fn(r)
	}
}

func (r *Registry) Inc(name string, labels map[string]string) {
	r.Add(name, labels, 1)
}

func (r *Registry) Add(name string, labels map[string]string, delta int64) {
	if r == nil || name == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ensureSamplesLocked()

	key := sampleKey(TypeCounter, name, labels)
	sample := r.samples[key]
	if sample.Name == "" {
		sample = Sample{Name: name, Type: TypeCounter, Labels: cloneLabels(labels)}
	}
	sample.Value += delta
	r.samples[key] = sample
}

func (r *Registry) SetCounter(name string, labels map[string]string, value int64) {
	if r == nil || name == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ensureSamplesLocked()

	key := sampleKey(TypeCounter, name, labels)
	r.samples[key] = Sample{Name: name, Type: TypeCounter, Labels: cloneLabels(labels), Value: value}
}

func (r *Registry) SetGauge(name string, labels map[string]string, value int64) {
	if r == nil || name == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ensureSamplesLocked()

	key := sampleKey(TypeGauge, name, labels)
	r.samples[key] = Sample{Name: name, Type: TypeGauge, Labels: cloneLabels(labels), Value: value}
}

// SetHistogramSample 写入或替换 histogram family 中的一项样本。
// Histogram family 会拆成多条共享 base 名的样本：
//   - <base>_bucket{le="..."}    累计计数，int
//   - <base>_count               总观察次数，int
//   - <base>_sum                 观察值总和（float 秒，set float=true）
//
// observer 会在 RunObservers 中调用它发布 histogram 的当前绝对状态；
// 和 SetGauge 一样，这里是替换语义，不做累加。Histogram.Observe 是公开 API，
// 调用方通常不直接使用 SetHistogramSample。
func (r *Registry) SetHistogramSample(name string, labels map[string]string, value int64, float bool, floatValue float64) {
	if r == nil || name == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ensureSamplesLocked()
	key := sampleKey(TypeHistogram, name, labels)
	r.samples[key] = Sample{
		Name:       name,
		Type:       TypeHistogram,
		Labels:     cloneLabels(labels),
		Value:      value,
		Float:      float,
		FloatValue: floatValue,
	}
}

func (r *Registry) AddGauge(name string, labels map[string]string, delta int64) {
	if r == nil || name == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ensureSamplesLocked()

	key := sampleKey(TypeGauge, name, labels)
	sample := r.samples[key]
	if sample.Name == "" {
		sample = Sample{Name: name, Type: TypeGauge, Labels: cloneLabels(labels)}
	}
	sample.Value += delta
	r.samples[key] = sample
}

func (r *Registry) Snapshot() []Sample {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	out := make([]Sample, 0, len(r.samples))
	for _, sample := range r.samples {
		sample.Labels = cloneLabels(sample.Labels)
		out = append(out, sample)
	}
	r.mu.RUnlock()

	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		if out[i].Type != out[j].Type {
			return out[i].Type < out[j].Type
		}
		return labelsKey(out[i].Labels) < labelsKey(out[j].Labels)
	})
	return out
}

func (r *Registry) ensureSamplesLocked() {
	if r.samples == nil {
		r.samples = make(map[string]Sample)
	}
}

func sampleKey(sampleType Type, name string, labels map[string]string) string {
	return string(sampleType) + "\x00" + name + "\x00" + labelsKey(labels)
}

func labelsKey(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, key := range keys {
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(labels[key])
		b.WriteByte('\x00')
	}
	return b.String()
}

func cloneLabels(labels map[string]string) map[string]string {
	if len(labels) == 0 {
		return nil
	}
	out := make(map[string]string, len(labels))
	for key, value := range labels {
		out[key] = value
	}
	return out
}
