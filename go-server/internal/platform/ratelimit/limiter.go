package ratelimit

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Config 定义单机限流器的开关、默认规则和可信代理来源。
type Config struct {
	Enabled           bool
	Algorithm         string
	Window            time.Duration
	MaxRequests       int
	Rules             []Rule
	CleanupEvery      time.Duration
	TrustedHeader     string
	TrustedProxyCIDRs []string
}

// Rule 定义一个 path 的窗口、最大请求数和前缀匹配方式。
type Rule struct {
	Path        string        `json:"path"`
	Window      time.Duration `json:"-"`
	WindowSec   int64         `json:"window_seconds"`
	MaxRequests int           `json:"max_requests"`
	Prefix      bool          `json:"prefix"`
}

// Snapshot 描述限流器当前配置和内存桶数量。
type Snapshot struct {
	Enabled       bool   `json:"enabled"`
	Algorithm     string `json:"algorithm"`
	WindowSeconds int64  `json:"window_seconds"`
	MaxRequests   int    `json:"max_requests"`
	Rules         []Rule `json:"rules"`
	Buckets       int    `json:"buckets"`
}

// Limiter 提供单进程内存限流，适合单节点或 Redis 限流不可用时的降级场景。
type Limiter struct {
	enabled           bool
	algorithm         string
	defaultRule       Rule
	rules             []Rule
	cleanupEvery      time.Duration
	trustedHeader     string
	trustedProxyCIDRs []*net.IPNet

	mu          sync.Mutex
	buckets     map[string]bucket
	lastCleanup time.Time
	now         func() time.Time
}

type bucket struct {
	windowStart time.Time
	count       int
	window      time.Duration
	tokens      float64
	updatedAt   time.Time
}

const (
	AlgorithmWindow      = "window"
	AlgorithmTokenBucket = "token_bucket"
)

// New 根据配置创建单机限流器，并补齐安全默认值。
func New(cfg Config) *Limiter {
	if cfg.Window <= 0 {
		cfg.Window = time.Second
	}
	if cfg.MaxRequests <= 0 {
		cfg.MaxRequests = 60
	}
	if cfg.CleanupEvery <= 0 {
		cfg.CleanupEvery = time.Minute
	}
	cfg.Algorithm = normalizeAlgorithm(cfg.Algorithm)
	if !ValidAlgorithm(cfg.Algorithm) {
		cfg.Algorithm = AlgorithmWindow
	}
	rules := normalizeRules(cfg.Rules, cfg.Window, cfg.MaxRequests)
	return &Limiter{
		enabled:   cfg.Enabled,
		algorithm: cfg.Algorithm,
		defaultRule: Rule{
			Path:        "*",
			Window:      cfg.Window,
			WindowSec:   int64(cfg.Window / time.Second),
			MaxRequests: cfg.MaxRequests,
			Prefix:      true,
		},
		rules:             rules,
		cleanupEvery:      cfg.CleanupEvery,
		trustedHeader:     strings.TrimSpace(cfg.TrustedHeader),
		trustedProxyCIDRs: parseProxyCIDRs(cfg.TrustedProxyCIDRs),
		buckets:           make(map[string]bucket),
		now:               time.Now,
	}
}

// Enabled 返回当前限流开关状态。
func (l *Limiter) Enabled() bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	l.ensureReadyLocked()
	enabled := l.enabled
	l.mu.Unlock()
	return enabled
}

// Wrap 将限流器包装到 HTTP handler 前面。
func (l *Limiter) Wrap(next http.Handler) http.Handler {
	if l == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := l.identity(r)
		path := r.URL.Path
		allowed, retryAfter := l.Allow(key, path)
		if !allowed {
			if retryAfter < time.Second {
				retryAfter = time.Second
			}
			w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter/time.Second)))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"rate limit exceeded"}` + "\n"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// WrapFunc 将限流器包装到 HTTP handler 函数前面。
func (l *Limiter) WrapFunc(next http.HandlerFunc) http.HandlerFunc {
	if l == nil {
		return next
	}
	return l.Wrap(next).ServeHTTP
}

// Allow 使用 identity/path 判断一次请求是否允许通过。
func (l *Limiter) Allow(identity, path string) (bool, time.Duration) {
	return l.AllowKey(identity, path)
}

// AllowComposite 将多个业务维度组成安全 key 后执行限流判断。
func (l *Limiter) AllowComposite(path string, parts ...string) (bool, time.Duration) {
	return l.AllowKey(CompositeKey(parts...), path)
}

// AllowKey 使用调用方给出的稳定 key 执行限流判断。
func (l *Limiter) AllowKey(key, path string) (bool, time.Duration) {
	if l == nil {
		return true, 0
	}
	key = strings.TrimSpace(key)
	if key == "" {
		key = "unknown"
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	l.ensureReadyLocked()
	if !l.enabled {
		return true, 0
	}
	now := l.now()
	rule := l.matchLocked(path)
	window := rule.Window
	if window <= 0 {
		window = time.Second
	}
	l.cleanupLocked(now)

	bucketKey := key + "\x00" + rule.Path
	if l.algorithm == AlgorithmTokenBucket {
		return l.allowBucketLocked(bucketKey, rule, window, now)
	}
	return l.allowWindowLocked(bucketKey, rule, window, now)
}

func (l *Limiter) allowWindowLocked(key string, rule Rule, window time.Duration, now time.Time) (bool, time.Duration) {
	b := l.buckets[key]
	if b.windowStart.IsZero() || now.Sub(b.windowStart) >= window {
		b = bucket{windowStart: now, window: window}
	}
	b.window = window
	b.count++
	l.buckets[key] = b
	if b.count <= rule.MaxRequests {
		return true, 0
	}
	return false, window - now.Sub(b.windowStart)
}

func (l *Limiter) allowBucketLocked(key string, rule Rule, window time.Duration, now time.Time) (bool, time.Duration) {
	capacity := float64(rule.MaxRequests)
	if capacity <= 0 {
		capacity = 1
	}
	refillPerSecond := capacity / window.Seconds()
	if refillPerSecond <= 0 {
		refillPerSecond = capacity
	}
	b := l.buckets[key]
	if b.updatedAt.IsZero() {
		b = bucket{windowStart: now, window: window, tokens: capacity, updatedAt: now}
	}
	if elapsed := now.Sub(b.updatedAt).Seconds(); elapsed > 0 {
		b.tokens += elapsed * refillPerSecond
		if b.tokens > capacity {
			b.tokens = capacity
		}
	}
	b.window = window
	b.updatedAt = now
	if b.tokens >= 1 {
		b.tokens--
		b.count++
		l.buckets[key] = b
		return true, 0
	}
	l.buckets[key] = b
	missing := 1 - b.tokens
	retry := time.Duration(missing/refillPerSecond*float64(time.Second) + 0.5)
	if retry <= 0 {
		retry = time.Nanosecond
	}
	return false, retry
}

// Configure 原子替换限流配置，并清空旧桶避免不同规则间互相污染。
func (l *Limiter) Configure(cfg Config) {
	if l == nil {
		return
	}
	if cfg.Window <= 0 {
		cfg.Window = time.Second
	}
	if cfg.MaxRequests <= 0 {
		cfg.MaxRequests = 60
	}
	if cfg.CleanupEvery <= 0 {
		cfg.CleanupEvery = time.Minute
	}
	cfg.Algorithm = normalizeAlgorithm(cfg.Algorithm)
	if !ValidAlgorithm(cfg.Algorithm) {
		cfg.Algorithm = AlgorithmWindow
	}
	rules := normalizeRules(cfg.Rules, cfg.Window, cfg.MaxRequests)
	l.mu.Lock()
	l.enabled = cfg.Enabled
	l.algorithm = cfg.Algorithm
	l.defaultRule = Rule{
		Path:        "*",
		Window:      cfg.Window,
		WindowSec:   int64(cfg.Window / time.Second),
		MaxRequests: cfg.MaxRequests,
		Prefix:      true,
	}
	l.rules = rules
	l.cleanupEvery = cfg.CleanupEvery
	l.trustedHeader = strings.TrimSpace(cfg.TrustedHeader)
	l.trustedProxyCIDRs = parseProxyCIDRs(cfg.TrustedProxyCIDRs)
	l.buckets = make(map[string]bucket)
	l.lastCleanup = time.Time{}
	l.ensureReadyLocked()
	l.mu.Unlock()
}

// Snapshot 返回当前限流配置和桶数量。
func (l *Limiter) Snapshot() Snapshot {
	if l == nil {
		return Snapshot{}
	}
	l.mu.Lock()
	l.ensureReadyLocked()
	buckets := len(l.buckets)
	rules := make([]Rule, len(l.rules))
	copy(rules, l.rules)
	snapshot := Snapshot{
		Enabled:       l.enabled,
		Algorithm:     l.algorithm,
		WindowSeconds: int64(l.defaultRule.Window / time.Second),
		MaxRequests:   l.defaultRule.MaxRequests,
		Rules:         rules,
		Buckets:       buckets,
	}
	l.mu.Unlock()
	return snapshot
}

func (l *Limiter) ensureReadyLocked() {
	if l.algorithm == "" {
		l.algorithm = AlgorithmWindow
	}
	if !ValidAlgorithm(l.algorithm) {
		l.algorithm = AlgorithmWindow
	}
	if l.defaultRule.Path == "" {
		l.defaultRule.Path = "*"
		l.defaultRule.Prefix = true
	}
	if l.defaultRule.Window <= 0 {
		l.defaultRule.Window = time.Second
	}
	if l.defaultRule.MaxRequests <= 0 {
		l.defaultRule.MaxRequests = 60
	}
	l.defaultRule.WindowSec = int64(l.defaultRule.Window / time.Second)
	if l.cleanupEvery <= 0 {
		l.cleanupEvery = time.Minute
	}
	if l.buckets == nil {
		l.buckets = make(map[string]bucket)
	}
	if l.now == nil {
		l.now = time.Now
	}
}

func (l *Limiter) matchLocked(path string) Rule {
	path = strings.TrimSpace(path)
	best := Rule{}
	for _, rule := range l.rules {
		if rule.Prefix {
			if strings.HasPrefix(path, rule.Path) && len(rule.Path) > len(best.Path) {
				best = rule
			}
			continue
		}
		if path == rule.Path && len(rule.Path) > len(best.Path) {
			best = rule
		}
	}
	if best.Path != "" {
		return best
	}
	return l.defaultRule
}

func (l *Limiter) cleanupLocked(now time.Time) {
	if l.lastCleanup.IsZero() {
		l.lastCleanup = now
		return
	}
	if now.Sub(l.lastCleanup) < l.cleanupEvery {
		return
	}
	for key, b := range l.buckets {
		window := b.window
		if window <= 0 {
			window = l.defaultRule.Window
		}
		base := b.windowStart
		if l.algorithm == AlgorithmTokenBucket && !b.updatedAt.IsZero() {
			base = b.updatedAt
		}
		if now.Sub(base) > 2*window+l.cleanupEvery {
			delete(l.buckets, key)
		}
	}
	l.lastCleanup = now
}

func (l *Limiter) identity(r *http.Request) string {
	l.mu.Lock()
	trustedHeader := l.trustedHeader
	trustedProxyCIDRs := append([]*net.IPNet(nil), l.trustedProxyCIDRs...)
	l.mu.Unlock()
	if trustedHeader != "" && trustedProxyAllowed(r.RemoteAddr, trustedProxyCIDRs) {
		if value := strings.TrimSpace(r.Header.Get(trustedHeader)); value != "" {
			if idx := strings.IndexByte(value, ','); idx >= 0 {
				value = strings.TrimSpace(value[:idx])
			}
			return value
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

func trustedProxyAllowed(remoteAddr string, trustedProxyCIDRs []*net.IPNet) bool {
	if len(trustedProxyCIDRs) == 0 {
		return true
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddr))
	if err != nil {
		host = strings.TrimSpace(remoteAddr)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, cidr := range trustedProxyCIDRs {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

func parseProxyCIDRs(values []string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if ip := net.ParseIP(value); ip != nil {
			bits := 32
			if ip.To4() == nil {
				bits = 128
			}
			_, network, _ := net.ParseCIDR(ip.String() + "/" + strconv.Itoa(bits))
			out = append(out, network)
			continue
		}
		if _, network, err := net.ParseCIDR(value); err == nil {
			out = append(out, network)
		}
	}
	return out
}

// ParseRules 解析 path,window_seconds,max_requests 格式的分号分隔规则。
func ParseRules(raw string) ([]Rule, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ";")
	rules := make([]Rule, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		fields := strings.Split(part, ",")
		if len(fields) != 3 {
			return nil, fmt.Errorf("rate limit rule %q must be path,window_seconds,max_requests", part)
		}
		path := strings.TrimSpace(fields[0])
		if path == "" {
			return nil, fmt.Errorf("rate limit rule %q has empty path", part)
		}
		prefix := false
		if strings.HasSuffix(path, "*") {
			prefix = true
			path = strings.TrimSuffix(path, "*")
		}
		if strings.HasSuffix(path, "/") {
			prefix = true
		}
		windowSec, err := strconv.Atoi(strings.TrimSpace(fields[1]))
		if err != nil || windowSec <= 0 {
			return nil, fmt.Errorf("rate limit rule %q has invalid window_seconds", part)
		}
		maxRequests, err := strconv.Atoi(strings.TrimSpace(fields[2]))
		if err != nil || maxRequests <= 0 {
			return nil, fmt.Errorf("rate limit rule %q has invalid max_requests", part)
		}
		rules = append(rules, Rule{
			Path:        path,
			Window:      time.Duration(windowSec) * time.Second,
			WindowSec:   int64(windowSec),
			MaxRequests: maxRequests,
			Prefix:      prefix,
		})
	}
	return rules, nil
}

func normalizeRules(rules []Rule, defaultWindow time.Duration, defaultMax int) []Rule {
	out := make([]Rule, 0, len(rules))
	for _, rule := range rules {
		rule.Path = strings.TrimSpace(rule.Path)
		if rule.Path == "" {
			continue
		}
		if rule.Window <= 0 {
			rule.Window = defaultWindow
		}
		if rule.MaxRequests <= 0 {
			rule.MaxRequests = defaultMax
		}
		rule.WindowSec = int64(rule.Window / time.Second)
		out = append(out, rule)
	}
	return out
}

func normalizeAlgorithm(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "", AlgorithmWindow:
		return AlgorithmWindow
	case "tokenbucket", "token-bucket", AlgorithmTokenBucket:
		return AlgorithmTokenBucket
	default:
		return value
	}
}

// ValidAlgorithm 判断限流算法是否被当前实现支持。
func ValidAlgorithm(value string) bool {
	switch normalizeAlgorithm(value) {
	case AlgorithmWindow, AlgorithmTokenBucket:
		return true
	default:
		return false
	}
}

// CompositeKey 使用长度前缀组合多个业务维度，避免分隔符碰撞。
func CompositeKey(parts ...string) string {
	if len(parts) == 0 {
		return "unknown"
	}
	var b strings.Builder
	for _, part := range parts {
		part = strings.TrimSpace(part)
		b.WriteString(strconv.Itoa(len(part)))
		b.WriteByte(':')
		b.WriteString(part)
		b.WriteByte('|')
	}
	if b.Len() == 0 {
		return "unknown"
	}
	return b.String()
}
