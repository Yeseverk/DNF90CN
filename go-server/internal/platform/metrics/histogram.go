package metrics

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// DurationBucketsSec 匹配 Prometheus client_golang 的默认延迟桶布局。
// 适合 HTTP/RPC 调用、handler 分发、Profile 读写、Redis/MySQL 命令、Lua 限流等
// 大部分游戏服务端"亚秒级到秒级"的请求延迟。需要更长尾时显式传 Buckets 覆盖。
var DurationBucketsSec = []float64{
	0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10,
}

// Histogram 是观察值（默认按秒）的分布统计；按 Prometheus 协议输出为
// <base>_bucket / <base>_count / <base>_sum 三类样本族。
//
// Histogram 自身只维护内存累加状态；最终样本通过注册的 Observer 在每次
// scrape 触发时写回 Registry。这使得 Histogram 可以承载高频 Observe 而不
// 每次都 touch Registry 的全局 map。
//
// 调用方应在启动时构造一次 Histogram 并复用，不要在每次请求里 New。
type Histogram struct {
	registry *Registry
	name     string
	buckets  []float64
	base     map[string]string

	mu     sync.Mutex
	series map[string]*histogramSeries
}

type histogramSeries struct {
	labels    map[string]string
	bucketCnt []uint64
	count     uint64
	sumSec    float64
}

// HistogramOptions 用于构造 Histogram。空字段会回退到合理默认。
type HistogramOptions struct {
	// Buckets 是上界（秒）切片，必须严格递增且为正；空切片回退到 DurationBucketsSec。
	// 不需要手动加 +Inf 桶，Histogram 在序列化时自动追加。
	Buckets []float64
	// Labels 是该 Histogram 上所有样本共享的基础标签，常见用法是固定 service、node。
	Labels map[string]string
}

// NewHistogram 构造一个 Histogram 并注册成该 Scope 对应 Registry 的 observer。
// observerKey 与 metric name 关联，重复构造同名 Histogram 会替换前一个 observer。
func NewHistogram(scope Scope, name string, opts HistogramOptions) *Histogram {
	if scope.registry == nil {
		return &Histogram{name: scope.Name(name)}
	}
	fullName := scope.Name(name)
	if fullName == "" {
		return &Histogram{}
	}
	buckets := normalizeBuckets(opts.Buckets)
	h := &Histogram{
		registry: scope.registry,
		name:     fullName,
		buckets:  buckets,
		base:     scope.Labels(opts.Labels),
		series:   make(map[string]*histogramSeries),
	}
	scope.registry.RegisterObserver("histogram:"+fullName, func(r *Registry) {
		h.flush(r)
	})
	return h
}

// Buckets 返回该 Histogram 当前生效的上界（不含 +Inf）。仅用于调试 / 测试。
func (h *Histogram) Buckets() []float64 {
	if h == nil {
		return nil
	}
	out := make([]float64, len(h.buckets))
	copy(out, h.buckets)
	return out
}

// Observe 记录一次观察值，默认无附加 labels。
func (h *Histogram) Observe(seconds float64) {
	h.ObserveWithLabels(seconds, nil)
}

// ObserveWithLabels 记录一次观察值并附加 per-observation labels。labels 会与
// HistogramOptions.Labels 合并；每组不同的 labels 都会成为独立的样本序列。
func (h *Histogram) ObserveWithLabels(seconds float64, labels map[string]string) {
	if h == nil || h.registry == nil {
		return
	}
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		return
	}
	if seconds < 0 {
		seconds = 0
	}
	merged := mergeLabels(h.base, labels)
	key := labelsKey(merged)
	h.mu.Lock()
	series, ok := h.series[key]
	if !ok {
		series = &histogramSeries{
			labels:    cloneLabels(merged),
			bucketCnt: make([]uint64, len(h.buckets)),
		}
		h.series[key] = series
	}
	series.count++
	series.sumSec += seconds
	for i, upper := range h.buckets {
		if seconds <= upper {
			series.bucketCnt[i]++
		}
	}
	h.mu.Unlock()
}

// flush 把内存累加状态写回 Registry。在每次 scrape 由 RunObservers 触发。
// 我们以"绝对值"写回（SetHistogramSample 替换语义），避免与 normalizeSamples
// 的累加路径互相干扰。
func (h *Histogram) flush(registry *Registry) {
	if h == nil || registry == nil {
		return
	}
	h.mu.Lock()
	snapshot := make([]*histogramSeries, 0, len(h.series))
	for _, series := range h.series {
		snapshot = append(snapshot, &histogramSeries{
			labels:    cloneLabels(series.labels),
			bucketCnt: append([]uint64(nil), series.bucketCnt...),
			count:     series.count,
			sumSec:    series.sumSec,
		})
	}
	h.mu.Unlock()
	for _, series := range snapshot {
		for i, upper := range h.buckets {
			leLabels := withLabel(series.labels, "le", formatBucketBound(upper))
			registry.SetHistogramSample(h.name+"_bucket", leLabels, Int64FromUint64(series.bucketCnt[i]), false, 0)
		}
		// +Inf 桶等价于 count，但 Prometheus 协议要求它显式出现。
		infLabels := withLabel(series.labels, "le", "+Inf")
		registry.SetHistogramSample(h.name+"_bucket", infLabels, Int64FromUint64(series.count), false, 0)
		registry.SetHistogramSample(h.name+"_count", series.labels, Int64FromUint64(series.count), false, 0)
		registry.SetHistogramSample(h.name+"_sum", series.labels, 0, true, series.sumSec)
	}
}

// normalizeBuckets 校验 / 复制 / 排序 buckets。空输入返回默认；输入会被复制
// 以避免调用方后续修改影响 Histogram 内部状态。
func normalizeBuckets(in []float64) []float64 {
	if len(in) == 0 {
		out := make([]float64, len(DurationBucketsSec))
		copy(out, DurationBucketsSec)
		return out
	}
	cleaned := make([]float64, 0, len(in))
	for _, b := range in {
		if math.IsNaN(b) || math.IsInf(b, 0) || b <= 0 {
			continue
		}
		cleaned = append(cleaned, b)
	}
	if len(cleaned) == 0 {
		return normalizeBuckets(nil)
	}
	sort.Float64s(cleaned)
	// 去重，避免 le 标签冲突。
	deduped := cleaned[:0]
	for i, b := range cleaned {
		if i == 0 || b != cleaned[i-1] {
			deduped = append(deduped, b)
		}
	}
	out := make([]float64, len(deduped))
	copy(out, deduped)
	return out
}

func withLabel(labels map[string]string, key, value string) map[string]string {
	out := make(map[string]string, len(labels)+1)
	for k, v := range labels {
		out[k] = v
	}
	out[key] = value
	return out
}

func formatBucketBound(value float64) string {
	// Prometheus 习惯把 0.005 显示成 "0.005" 而非 "5e-03"。用 %g 在大多数桶
	// 上会给出干净小数；遇到极小或极大值时再退到科学计数。
	s := strconv.FormatFloat(value, 'g', -1, 64)
	// 进一步避免 "1" 这种没有小数点的形式造成与整数 bucket 混淆。
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}
