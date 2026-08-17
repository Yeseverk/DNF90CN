package registry

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

var ErrClosed = errors.New("registry is closed")

type Node struct {
	Service      string            `json:"service"`
	NodeID       string            `json:"node_id"`
	GID          int64             `json:"gid,omitempty"`
	VirtualGID   int64             `json:"virtual_gid,omitempty"`
	ShardID      int64             `json:"shard_id,omitempty"`
	PublicAddr   string            `json:"public_addr"`
	PrivateAddr  string            `json:"private_addr"`
	State        string            `json:"state,omitempty"`
	Weight       int               `json:"weight,omitempty"`
	Routes       []Route           `json:"routes,omitempty"`
	Events       []Event           `json:"events,omitempty"`
	Services     []string          `json:"services,omitempty"`
	Tags         []string          `json:"tags,omitempty"`
	GMVisible    bool              `json:"gm_visible,omitempty"`
	Meta         map[string]string `json:"meta,omitempty"`
	RegisteredAt time.Time         `json:"registered_at"`
}

type Route struct {
	ID         string `json:"id"`
	Group      string `json:"group,omitempty"`
	Internal   bool   `json:"internal,omitempty"`
	Stateful   bool   `json:"stateful,omitempty"`
	Authorized bool   `json:"authorized,omitempty"`
	Idempotent bool   `json:"idempotent,omitempty"`
}

type Event struct {
	Name string `json:"name"`
}

type Registry interface {
	Register(context.Context, Node) error
	Deregister(context.Context, string, string) error
	List(context.Context, string) ([]Node, error)
	Close() error
}

type WatchEventType string

const (
	EventUpsert WatchEventType = "upsert"
	EventDelete WatchEventType = "delete"
)

type WatchEvent struct {
	Type WatchEventType `json:"type"`
	Node Node           `json:"node"`
}

type Watcher interface {
	Watch(context.Context, string) (<-chan WatchEvent, error)
}

type Memory struct {
	mu       sync.RWMutex
	closed   bool
	closedCh chan struct{}
	nodes    map[string]map[string]Node
	watchers map[string]map[chan WatchEvent]struct{}
}

func NewMemory() *Memory {
	return &Memory{
		closedCh: make(chan struct{}),
		nodes:    make(map[string]map[string]Node),
		watchers: make(map[string]map[chan WatchEvent]struct{}),
	}
}

func contextOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func (r *Memory) Register(ctx context.Context, node Node) error {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	node.Service = strings.TrimSpace(node.Service)
	node.NodeID = strings.TrimSpace(node.NodeID)
	node.Tags = normalizeTags(node.Tags)
	if node.Service == "" || node.NodeID == "" {
		return fmt.Errorf("registry node service and node_id are required")
	}
	if r == nil {
		return ErrClosed
	}
	r.mu.Lock()
	r.ensureReadyLocked()
	if r.closed {
		r.mu.Unlock()
		return ErrClosed
	}
	if _, ok := r.nodes[node.Service]; !ok {
		r.nodes[node.Service] = make(map[string]Node)
	}
	node.RegisteredAt = time.Now().UTC()
	node = cloneNode(node)
	r.nodes[node.Service][node.NodeID] = node
	r.publishLocked(ctx, node.Service, WatchEvent{Type: EventUpsert, Node: node})
	r.mu.Unlock()
	return nil
}

func (r *Memory) Check(ctx context.Context) error {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if r == nil {
		return ErrClosed
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return ErrClosed
	}
	return nil
}

func (r *Memory) Deregister(ctx context.Context, service, nodeID string) error {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	service = strings.TrimSpace(service)
	nodeID = strings.TrimSpace(nodeID)
	if service == "" || nodeID == "" {
		return nil
	}
	if r == nil {
		return ErrClosed
	}
	r.mu.Lock()
	r.ensureReadyLocked()
	if r.closed {
		r.mu.Unlock()
		return ErrClosed
	}
	node := Node{Service: service, NodeID: nodeID}
	if serviceNodes, ok := r.nodes[service]; ok {
		if existing, exists := serviceNodes[nodeID]; exists {
			node = cloneNode(existing)
		}
		delete(serviceNodes, nodeID)
		if len(serviceNodes) == 0 {
			delete(r.nodes, service)
		}
	}
	r.publishLocked(ctx, service, WatchEvent{Type: EventDelete, Node: node})
	r.mu.Unlock()
	return nil
}

func (r *Memory) List(ctx context.Context, service string) ([]Node, error) {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	service = strings.TrimSpace(service)
	if service == "" {
		return nil, nil
	}
	if r == nil {
		return nil, ErrClosed
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return nil, ErrClosed
	}

	serviceNodes := r.nodes[service]
	nodes := make([]Node, 0, len(serviceNodes))
	for _, node := range serviceNodes {
		nodes = append(nodes, cloneNode(node))
	}
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].NodeID < nodes[j].NodeID
	})
	return nodes, nil
}

func ListGMVisible(ctx context.Context, reg Registry, service string) ([]Node, error) {
	if reg == nil {
		return nil, nil
	}
	nodes, err := reg.List(ctx, service)
	if err != nil {
		return nil, err
	}
	return FilterGMVisible(nodes), nil
}

func FilterGMVisible(nodes []Node) []Node {
	if len(nodes) == 0 {
		return nil
	}
	out := make([]Node, 0, len(nodes))
	for _, node := range nodes {
		if !node.GMVisible {
			continue
		}
		out = append(out, cloneNode(node))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Service != out[j].Service {
			return out[i].Service < out[j].Service
		}
		return out[i].NodeID < out[j].NodeID
	})
	return out
}

func (r *Memory) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	r.ensureReadyLocked()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	r.nodes = make(map[string]map[string]Node)
	r.watchers = make(map[string]map[chan WatchEvent]struct{})
	closedCh := r.closedCh
	r.mu.Unlock()

	close(closedCh)
	return nil
}

func (r *Memory) Watch(ctx context.Context, service string) (<-chan WatchEvent, error) {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	service = strings.TrimSpace(service)
	if service == "" {
		return nil, fmt.Errorf("registry watch service is required")
	}
	if r == nil {
		return nil, ErrClosed
	}
	r.mu.Lock()
	r.ensureReadyLocked()
	if r.closed {
		r.mu.Unlock()
		return nil, ErrClosed
	}
	initial := make([]Node, 0, len(r.nodes[service]))
	for _, node := range r.nodes[service] {
		initial = append(initial, cloneNode(node))
	}
	sort.Slice(initial, func(i, j int) bool {
		return initial[i].NodeID < initial[j].NodeID
	})

	ch := make(chan WatchEvent, len(initial)+16)
	for _, node := range initial {
		ch <- WatchEvent{Type: EventUpsert, Node: node}
	}
	if _, ok := r.watchers[service]; !ok {
		r.watchers[service] = make(map[chan WatchEvent]struct{})
	}
	r.watchers[service][ch] = struct{}{}
	closedCh := r.closedCh
	r.mu.Unlock()

	go func() {
		select {
		case <-ctx.Done():
		case <-closedCh:
		}

		r.mu.Lock()
		if serviceWatchers, ok := r.watchers[service]; ok {
			delete(serviceWatchers, ch)
			if len(serviceWatchers) == 0 {
				delete(r.watchers, service)
			}
		}
		// publishLocked 与这里共用 r.mu，保证删除后关闭时没有发送者仍持有 ch。
		close(ch)
		r.mu.Unlock()
	}()

	return ch, nil
}

// ensureReadyLocked 补齐手工构造 Memory 时缺失的内部索引和关闭信号。
// 调用方必须已经持有 r.mu 写锁。
func (r *Memory) ensureReadyLocked() {
	if r.closedCh == nil {
		r.closedCh = make(chan struct{})
	}
	if r.nodes == nil {
		r.nodes = make(map[string]map[string]Node)
	}
	if r.watchers == nil {
		r.watchers = make(map[string]map[chan WatchEvent]struct{})
	}
}

// publishLocked 只做非阻塞发送，调用方必须持有 r.mu 写锁。
// channel 的发送与关闭由同一把锁保护，不能再用 recover 掩盖生命周期竞争。
func (r *Memory) publishLocked(ctx context.Context, service string, event WatchEvent) {
	if ctx != nil && ctx.Err() != nil {
		return
	}
	for ch := range r.watchers[service] {
		next := event
		next.Node = cloneNode(event.Node)
		select {
		case ch <- next:
		default:
		}
	}
}

func cloneNode(node Node) Node {
	node.Meta = cloneMeta(node.Meta)
	node.Routes = append([]Route(nil), node.Routes...)
	node.Events = append([]Event(nil), node.Events...)
	node.Services = append([]string(nil), node.Services...)
	node.Tags = append([]string(nil), node.Tags...)
	return node
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
