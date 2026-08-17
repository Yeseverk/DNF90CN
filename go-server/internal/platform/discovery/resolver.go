package discovery

import (
	"context"
	"encoding/base64"
	"errors"
	"hash/fnv"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"longheng.io/server/internal/platform/registry"
)

var (
	ErrNoRegistry       = errors.New("discovery registry is required")
	ErrNoNodes          = errors.New("discovery service has no nodes")
	ErrNoRoute          = errors.New("discovery route has no nodes")
	ErrNoProbe          = errors.New("discovery health probe is required")
	ErrRouteRequiresKey = errors.New("discovery route requires routing key")
)

type Strategy string

const (
	StrategyHash               Strategy = "hash"
	StrategyRandom             Strategy = "random"
	StrategyRoundRobin         Strategy = "round_robin"
	StrategyWeightedRoundRobin Strategy = "weighted_round_robin"
)

type Resolver struct {
	reg               registry.Registry
	cacheTTL          time.Duration
	failureTTL        time.Duration
	failureThreshold  int
	rejectMaintaining bool
	strategy          Strategy
	clock             func() time.Time

	mu       sync.Mutex
	cache    map[string]cacheEntry
	failures map[string]failureState
	cursors  map[string]uint64
	rng      *rand.Rand
}

type cacheEntry struct {
	nodes     []registry.Node
	expiresAt time.Time
}

type failureState struct {
	count     int
	expiresAt time.Time
}

type Snapshot struct {
	CacheTTLSeconds   int64             `json:"cache_ttl_seconds"`
	FailureTTLSeconds int64             `json:"failure_ttl_seconds"`
	FailureThreshold  int               `json:"failure_threshold"`
	RejectMaintaining bool              `json:"reject_maintaining"`
	Strategy          Strategy          `json:"strategy"`
	CachedServices    []CachedService   `json:"cached_services"`
	Failures          []FailureSnapshot `json:"failures"`
}

type CachedService struct {
	Service   string    `json:"service"`
	NodeIDs   []string  `json:"node_ids"`
	ExpiresAt time.Time `json:"expires_at"`
}

type FailureSnapshot struct {
	NodeID     string    `json:"node_id"`
	Count      int       `json:"count"`
	ExpiresAt  time.Time `json:"expires_at"`
	Suppressed bool      `json:"suppressed"`
}

func NewResolver(reg registry.Registry, cacheTTL, failureTTL time.Duration, rejectMaintaining bool) *Resolver {
	if cacheTTL <= 0 {
		cacheTTL = time.Second
	}
	if failureTTL <= 0 {
		failureTTL = 5 * time.Second
	}
	return &Resolver{
		reg:               reg,
		cacheTTL:          cacheTTL,
		failureTTL:        failureTTL,
		failureThreshold:  1,
		rejectMaintaining: rejectMaintaining,
		strategy:          StrategyHash,
		clock:             time.Now,
		cache:             make(map[string]cacheEntry),
		failures:          make(map[string]failureState),
		cursors:           make(map[string]uint64),
		rng:               rand.New(rand.NewSource(time.Now().UnixNano())), // #nosec G404 -- 非安全用途：服务发现随机负载均衡，不用于凭证或加密。
	}
}

func (r *Resolver) Resolve(ctx context.Context, service, key string) (registry.Node, bool, error) {
	if r == nil || r.reg == nil {
		return registry.Node{}, false, ErrNoRegistry
	}
	service = strings.TrimSpace(service)
	if service == "" {
		return registry.Node{}, false, ErrNoNodes
	}
	nodes, err := r.nodes(ctx, service)
	if err != nil {
		return registry.Node{}, false, err
	}
	candidates := r.candidates(nodes)
	if len(candidates) == 0 {
		return registry.Node{}, false, nil
	}
	return r.selectNode(service, candidates, key), true, nil
}

func (r *Resolver) ResolveWithTags(ctx context.Context, service, key string, tags []string, matchAny bool) (registry.Node, bool, error) {
	if r == nil || r.reg == nil {
		return registry.Node{}, false, ErrNoRegistry
	}
	service = strings.TrimSpace(service)
	if service == "" {
		return registry.Node{}, false, ErrNoNodes
	}
	nodes, err := r.nodes(ctx, service)
	if err != nil {
		return registry.Node{}, false, err
	}
	candidates := filterNodesByTags(r.candidates(nodes), tags, matchAny)
	if len(candidates) == 0 {
		return registry.Node{}, false, nil
	}
	return r.selectNode(service+":"+tagBucket(tags, matchAny), candidates, key), true, nil
}

func (r *Resolver) ResolveRoute(ctx context.Context, service, route, key string) (registry.Node, bool, error) {
	if r == nil || r.reg == nil {
		return registry.Node{}, false, ErrNoRegistry
	}
	service = strings.TrimSpace(service)
	route = strings.TrimSpace(route)
	if service == "" {
		return registry.Node{}, false, ErrNoNodes
	}
	if route == "" {
		return r.Resolve(ctx, service, key)
	}
	nodes, err := r.nodes(ctx, service)
	if err != nil {
		return registry.Node{}, false, err
	}
	candidates, err := r.routeCandidates(nodes, route, key)
	if err != nil {
		return registry.Node{}, false, err
	}
	if len(candidates) == 0 {
		if nodesDeclareRoutes(nodes) {
			return registry.Node{}, false, ErrNoRoute
		}
		return registry.Node{}, false, nil
	}
	return r.selectNode(service+":"+route, candidates, key), true, nil
}

func (r *Resolver) ResolveRouteWithTags(ctx context.Context, service, route, key string, tags []string, matchAny bool) (registry.Node, bool, error) {
	if r == nil || r.reg == nil {
		return registry.Node{}, false, ErrNoRegistry
	}
	service = strings.TrimSpace(service)
	route = strings.TrimSpace(route)
	if service == "" {
		return registry.Node{}, false, ErrNoNodes
	}
	if route == "" {
		return r.ResolveWithTags(ctx, service, key, tags, matchAny)
	}
	nodes, err := r.nodes(ctx, service)
	if err != nil {
		return registry.Node{}, false, err
	}
	candidates, err := r.routeCandidates(nodes, route, key)
	if err != nil {
		return registry.Node{}, false, err
	}
	candidates = filterNodesByTags(candidates, tags, matchAny)
	if len(candidates) == 0 {
		if nodesDeclareRoutes(nodes) {
			return registry.Node{}, false, ErrNoRoute
		}
		return registry.Node{}, false, nil
	}
	return r.selectNode(service+":"+route+":"+tagBucket(tags, matchAny), candidates, key), true, nil
}

func (r *Resolver) MarkFailure(nodeID string) {
	if r == nil {
		return
	}
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return
	}
	now := r.now()
	r.mu.Lock()
	r.pruneFailuresLocked(now)
	state := r.failures[nodeID]
	if state.expiresAt.IsZero() || !state.expiresAt.After(now) {
		state.count = 0
	}
	state.count++
	state.expiresAt = now.Add(r.failureTTL)
	r.failures[nodeID] = state
	r.mu.Unlock()
}

func (r *Resolver) MarkSuccess(nodeID string) {
	if r == nil {
		return
	}
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return
	}
	r.mu.Lock()
	delete(r.failures, nodeID)
	r.mu.Unlock()
}

func (r *Resolver) Invalidate(service string) {
	if r == nil {
		return
	}
	service = strings.TrimSpace(service)
	r.mu.Lock()
	if service == "" {
		r.cache = make(map[string]cacheEntry)
	} else {
		delete(r.cache, service)
	}
	r.mu.Unlock()
}

func (r *Resolver) SetFailureThreshold(threshold int) {
	if r == nil {
		return
	}
	if threshold <= 0 {
		threshold = 1
	}
	r.mu.Lock()
	r.failureThreshold = threshold
	r.mu.Unlock()
}

func (r *Resolver) Configure(cacheTTL, failureTTL time.Duration, failureThreshold int, rejectMaintaining bool, strategy Strategy) {
	if r == nil {
		return
	}
	if cacheTTL <= 0 {
		cacheTTL = time.Second
	}
	if failureTTL <= 0 {
		failureTTL = 5 * time.Second
	}
	if failureThreshold <= 0 {
		failureThreshold = 1
	}
	r.mu.Lock()
	r.cacheTTL = cacheTTL
	r.failureTTL = failureTTL
	r.failureThreshold = failureThreshold
	r.rejectMaintaining = rejectMaintaining
	r.strategy = normalizeStrategy(strategy)
	r.cache = make(map[string]cacheEntry)
	r.cursors = make(map[string]uint64)
	r.pruneFailuresLocked(r.now())
	r.mu.Unlock()
}

func (r *Resolver) SetStrategy(strategy Strategy) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.strategy = normalizeStrategy(strategy)
	r.cursors = make(map[string]uint64)
	r.mu.Unlock()
}

func (r *Resolver) Snapshot() Snapshot {
	if r == nil {
		return Snapshot{}
	}
	now := r.now()
	r.mu.Lock()
	r.pruneFailuresLocked(now)
	cache := make([]CachedService, 0, len(r.cache))
	for service, entry := range r.cache {
		nodeIDs := make([]string, 0, len(entry.nodes))
		for _, node := range entry.nodes {
			nodeID := strings.TrimSpace(node.NodeID)
			if nodeID != "" {
				nodeIDs = append(nodeIDs, nodeID)
			}
		}
		sort.Strings(nodeIDs)
		cache = append(cache, CachedService{
			Service:   service,
			NodeIDs:   nodeIDs,
			ExpiresAt: entry.expiresAt,
		})
	}
	failures := make([]FailureSnapshot, 0, len(r.failures))
	for nodeID, state := range r.failures {
		failures = append(failures, FailureSnapshot{
			NodeID:     nodeID,
			Count:      state.count,
			ExpiresAt:  state.expiresAt,
			Suppressed: state.count >= r.failureThreshold && state.expiresAt.After(now),
		})
	}
	snapshot := Snapshot{
		CacheTTLSeconds:   int64(r.cacheTTL / time.Second),
		FailureTTLSeconds: int64(r.failureTTL / time.Second),
		FailureThreshold:  r.failureThreshold,
		RejectMaintaining: r.rejectMaintaining,
		Strategy:          r.strategy,
		CachedServices:    cache,
		Failures:          failures,
	}
	r.mu.Unlock()

	sort.Slice(snapshot.CachedServices, func(i, j int) bool {
		return snapshot.CachedServices[i].Service < snapshot.CachedServices[j].Service
	})
	sort.Slice(snapshot.Failures, func(i, j int) bool {
		return snapshot.Failures[i].NodeID < snapshot.Failures[j].NodeID
	})
	return snapshot
}

func (r *Resolver) nodes(ctx context.Context, service string) ([]registry.Node, error) {
	now := r.now()
	r.mu.Lock()
	if entry, ok := r.cache[service]; ok && entry.expiresAt.After(now) {
		nodes := cloneNodes(entry.nodes)
		r.mu.Unlock()
		return nodes, nil
	}
	r.mu.Unlock()

	nodes, err := r.reg.List(ctx, service)
	if err != nil {
		return nil, err
	}
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].NodeID < nodes[j].NodeID
	})

	r.mu.Lock()
	r.cache[service] = cacheEntry{
		nodes:     cloneNodes(nodes),
		expiresAt: now.Add(r.cacheTTL),
	}
	r.mu.Unlock()
	return nodes, nil
}

func (r *Resolver) candidates(nodes []registry.Node) []registry.Node {
	now := r.now()
	r.mu.Lock()
	failures := make(map[string]struct{}, len(r.failures))
	rejectMaintaining := r.rejectMaintaining
	r.pruneFailuresLocked(now)
	for nodeID, state := range r.failures {
		if state.count >= r.failureThreshold && state.expiresAt.After(now) {
			failures[nodeID] = struct{}{}
		}
	}
	r.mu.Unlock()

	out := make([]registry.Node, 0, len(nodes))
	for _, node := range nodes {
		nodeID := strings.TrimSpace(node.NodeID)
		if nodeID == "" {
			continue
		}
		if _, failed := failures[nodeID]; failed {
			continue
		}
		if rejectMaintaining && nodeMaintaining(node.Meta) {
			continue
		}
		if !nodeAccepting(node) {
			continue
		}
		out = append(out, node)
	}
	return out
}

func (r *Resolver) routeCandidates(nodes []registry.Node, routeID, key string) ([]registry.Node, error) {
	candidates := r.candidates(nodes)
	if len(candidates) == 0 || !nodesDeclareRoutes(candidates) {
		return candidates, nil
	}

	out := make([]registry.Node, 0, len(candidates))
	for _, node := range candidates {
		route, ok := matchRoute(node.Routes, routeID)
		if !ok {
			continue
		}
		if strings.TrimSpace(key) == "" && (route.Stateful || route.Authorized) {
			return nil, ErrRouteRequiresKey
		}
		out = append(out, node)
	}
	return out, nil
}

func (r *Resolver) selectNode(bucket string, candidates []registry.Node, key string) registry.Node {
	if len(candidates) == 1 {
		return candidates[0]
	}
	r.mu.Lock()
	strategy := r.strategy
	r.mu.Unlock()
	switch strategy {
	case StrategyRandom:
		return candidates[r.randomIndex(len(candidates))]
	case StrategyRoundRobin:
		return candidates[r.nextCursor(bucket, len(candidates))]
	case StrategyWeightedRoundRobin:
		return r.selectWeightedRR(bucket, candidates)
	default:
		return selectWeightedByKey(candidates, key)
	}
}

func (r *Resolver) nextCursor(bucket string, size int) int {
	if size <= 1 {
		return 0
	}
	r.mu.Lock()
	next := r.cursors[bucket]
	r.cursors[bucket] = next + 1
	r.mu.Unlock()
	return int(next % uint64(size)) // #nosec G115 -- 取模结果必定小于 size，size 已经是 int。
}

func (r *Resolver) randomIndex(size int) int {
	if size <= 1 {
		return 0
	}
	r.mu.Lock()
	if r.rng == nil {
		r.rng = rand.New(rand.NewSource(time.Now().UnixNano())) // #nosec G404 -- 非安全用途：服务发现随机负载均衡，不用于凭证或加密。
	}
	index := r.rng.Intn(size)
	r.mu.Unlock()
	return index
}

func (r *Resolver) selectWeightedRR(bucket string, candidates []registry.Node) registry.Node {
	total := 0
	for _, candidate := range candidates {
		total += nodeWeight(candidate)
	}
	if total <= 0 {
		return candidates[r.nextCursor(bucket, len(candidates))]
	}
	index := r.nextCursor(bucket, total)
	for _, candidate := range candidates {
		weight := nodeWeight(candidate)
		if index < weight {
			return candidate
		}
		index -= weight
	}
	return candidates[len(candidates)-1]
}

func (r *Resolver) pruneFailuresLocked(now time.Time) {
	for nodeID, state := range r.failures {
		if !state.expiresAt.After(now) {
			delete(r.failures, nodeID)
		}
	}
}

func (r *Resolver) now() time.Time {
	if r.clock == nil {
		return time.Now()
	}
	return r.clock()
}

func nodeMaintaining(meta map[string]string) bool {
	for _, key := range []string{"maintaining", "is_maintaining", "maintenance"} {
		value := strings.TrimSpace(strings.ToLower(meta[key]))
		switch value {
		case "1", "true", "yes", "y", "on":
			return true
		}
	}
	return false
}

func nodeAccepting(node registry.Node) bool {
	switch strings.TrimSpace(strings.ToLower(nodeState(node))) {
	case "stopping", "stopped", "shut", "hang":
		return false
	default:
		return true
	}
}

func nodeState(node registry.Node) string {
	if strings.TrimSpace(node.State) != "" {
		return strings.TrimSpace(node.State)
	}
	return strings.TrimSpace(node.Meta["state"])
}

func nodeWeight(node registry.Node) int {
	if node.Weight > 0 {
		return node.Weight
	}
	weight, _ := strconv.Atoi(strings.TrimSpace(node.Meta["weight"]))
	if weight > 0 {
		return weight
	}
	return 1
}

func nodesDeclareRoutes(nodes []registry.Node) bool {
	for _, node := range nodes {
		if len(node.Routes) > 0 {
			return true
		}
	}
	return false
}

func matchRoute(routes []registry.Route, routeID string) (registry.Route, bool) {
	for _, route := range routes {
		if strings.TrimSpace(route.ID) == routeID {
			return route, true
		}
	}
	return registry.Route{}, false
}

func filterNodesByTags(nodes []registry.Node, tags []string, matchAny bool) []registry.Node {
	tags = normalizeTags(tags)
	if len(tags) == 0 {
		return nodes
	}
	out := make([]registry.Node, 0, len(nodes))
	for _, node := range nodes {
		if nodeMatchesTags(node, tags, matchAny) {
			out = append(out, node)
		}
	}
	return out
}

func nodeMatchesTags(node registry.Node, tags []string, matchAny bool) bool {
	nodeTags := normalizeTags(node.Tags)
	if len(nodeTags) == 0 {
		return false
	}
	have := make(map[string]struct{}, len(nodeTags))
	for _, tag := range nodeTags {
		have[tag] = struct{}{}
	}
	if matchAny {
		for _, tag := range tags {
			if _, ok := have[tag]; ok {
				return true
			}
		}
		return false
	}
	for _, tag := range tags {
		if _, ok := have[tag]; !ok {
			return false
		}
	}
	return true
}

func tagBucket(tags []string, matchAny bool) string {
	tags = normalizeTags(tags)
	if len(tags) == 0 {
		return "all"
	}
	mode := "all"
	if matchAny {
		mode = "any"
	}
	encoded := make([]string, 0, len(tags))
	for _, tag := range tags {
		encoded = append(encoded, encodeTagBucketPart(tag))
	}
	return mode + ":" + strings.Join(encoded, ",")
}

func encodeTagBucketPart(tag string) string {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return "_"
	}
	return base64.RawURLEncoding.EncodeToString([]byte(tag))
}

func normalizeTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(tags))
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(strings.ToLower(tag))
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		out = append(out, tag)
	}
	sort.Strings(out)
	return out
}

func selectWeightedByKey(candidates []registry.Node, key string) registry.Node {
	total := 0
	for _, candidate := range candidates {
		total += nodeWeight(candidate)
	}
	if total <= 0 {
		return candidates[hashIndex(key, len(candidates))]
	}
	index := hashIndex(key, total)
	for _, candidate := range candidates {
		weight := nodeWeight(candidate)
		if index < weight {
			return candidate
		}
		index -= weight
	}
	return candidates[len(candidates)-1]
}

func hashIndex(key string, size int) int {
	if size <= 1 {
		return 0
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return 0
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return int(uint64(h.Sum32()) % uint64(size)) // #nosec G115 -- 取模结果必定小于 size，size 已经是 int。
}

func normalizeStrategy(strategy Strategy) Strategy {
	value := Strategy(strings.ToLower(strings.TrimSpace(string(strategy))))
	switch value {
	case StrategyRandom, StrategyRoundRobin, StrategyWeightedRoundRobin:
		return value
	default:
		return StrategyHash
	}
}

func cloneNodes(nodes []registry.Node) []registry.Node {
	out := append([]registry.Node(nil), nodes...)
	for idx := range out {
		out[idx].Meta = cloneMeta(out[idx].Meta)
	}
	return out
}

func cloneMeta(meta map[string]string) map[string]string {
	if len(meta) == 0 {
		return nil
	}
	out := make(map[string]string, len(meta))
	for key, value := range meta {
		out[key] = value
	}
	return out
}
