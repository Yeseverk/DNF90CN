package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

type ETCD struct {
	logger   *slog.Logger
	client   *clientv3.Client
	prefix   string
	leaseTTL int64

	mu     sync.Mutex
	closed bool
	leases map[string]etcdLease
}

type etcdLease struct {
	key    string
	lease  clientv3.LeaseID
	cancel context.CancelFunc
}

const etcdCleanupTimeout = 5 * time.Second

func NewETCD(endpoints []string, namespace string, leaseTTL int64, logger *slog.Logger) (*ETCD, error) {
	endpoints = normalizeEndpoints(endpoints)
	if len(endpoints) == 0 {
		return nil, fmt.Errorf("etcd registry requires at least one endpoint")
	}
	if leaseTTL <= 0 {
		leaseTTL = 15
	}

	client, err := clientv3.New(clientv3.Config{
		Endpoints:   endpoints,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		return nil, err
	}

	return &ETCD{
		logger:   logger,
		client:   client,
		prefix:   normalizeNamespace(namespace),
		leaseTTL: leaseTTL,
		leases:   make(map[string]etcdLease),
	}, nil
}

func (r *ETCD) Register(ctx context.Context, node Node) error {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if r == nil {
		return ErrClosed
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return ErrClosed
	}
	client := r.client
	r.mu.Unlock()

	if client == nil {
		return ErrClosed
	}
	node.Service = strings.TrimSpace(node.Service)
	node.NodeID = strings.TrimSpace(node.NodeID)
	node.Tags = normalizeTags(node.Tags)
	if node.Service == "" || node.NodeID == "" {
		return fmt.Errorf("registry node service and node_id are required")
	}
	node.RegisteredAt = time.Now().UTC()
	data, err := json.Marshal(node)
	if err != nil {
		return err
	}

	leaseResp, err := client.Grant(ctx, r.leaseTTL)
	if err != nil {
		return err
	}

	key := r.nodeKey(node.Service, node.NodeID)
	if _, err := client.Put(ctx, key, string(data), clientv3.WithLease(leaseResp.ID)); err != nil {
		cleanupCtx, cleanupCancel := etcdCleanupContext(ctx)
		_, _ = client.Revoke(cleanupCtx, leaseResp.ID)
		cleanupCancel()
		return err
	}

	keepAliveCtx, cancel := context.WithCancel(context.Background())
	ch, err := client.KeepAlive(keepAliveCtx, leaseResp.ID)
	if err != nil {
		cancel()
		cleanupCtx, cleanupCancel := etcdCleanupContext(ctx)
		_, _ = client.Delete(cleanupCtx, key)
		_, _ = client.Revoke(cleanupCtx, leaseResp.ID)
		cleanupCancel()
		return err
	}

	id := node.Service + "\x00" + node.NodeID

	var oldLease etcdLease
	var replaced bool
	r.mu.Lock()
	if existing, ok := r.leases[id]; ok {
		existing.cancel()
		oldLease = existing
		replaced = true
	}
	if r.leases == nil {
		r.leases = make(map[string]etcdLease)
	}
	r.leases[id] = etcdLease{
		key:    key,
		lease:  leaseResp.ID,
		cancel: cancel,
	}
	r.mu.Unlock()
	if replaced {
		cleanupCtx, cleanupCancel := etcdCleanupContext(ctx)
		_, _ = client.Revoke(cleanupCtx, oldLease.lease)
		cleanupCancel()
	}

	go r.consumeKeepAlive(id, key, leaseResp.ID, ch)
	return nil
}

func (r *ETCD) Check(ctx context.Context) error {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if r == nil {
		return ErrClosed
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return ErrClosed
	}
	client := r.client
	prefix := r.prefix
	r.mu.Unlock()

	if client == nil {
		return ErrClosed
	}
	_, err := client.Get(ctx, path.Join("/", prefix), clientv3.WithPrefix(), clientv3.WithLimit(1))
	return err
}

func (r *ETCD) Deregister(ctx context.Context, service, nodeID string) error {
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
	if r.closed {
		r.mu.Unlock()
		return ErrClosed
	}
	id := service + "\x00" + nodeID
	lease, ok := r.leases[id]
	client := r.client
	r.mu.Unlock()

	if client == nil {
		return ErrClosed
	}
	key := r.nodeKey(service, nodeID)
	if _, err := client.Delete(ctx, key); err != nil {
		return err
	}

	if ok && r.removeLeaseIfCurrent(id, lease) {
		lease.cancel()
		cleanupCtx, cleanupCancel := etcdCleanupContext(ctx)
		_, _ = client.Revoke(cleanupCtx, lease.lease)
		cleanupCancel()
	}

	return nil
}

func (r *ETCD) List(ctx context.Context, service string) ([]Node, error) {
	nodes, _, err := r.listWithRevision(ctx, service)
	return nodes, err
}

func (r *ETCD) listWithRevision(ctx context.Context, service string) ([]Node, int64, error) {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	service = strings.TrimSpace(service)
	if service == "" {
		return nil, 0, nil
	}
	if r == nil {
		return nil, 0, ErrClosed
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, 0, ErrClosed
	}
	client := r.client
	prefix := r.servicePrefix(service)
	r.mu.Unlock()

	if client == nil {
		return nil, 0, ErrClosed
	}
	resp, err := client.Get(ctx, prefix, clientv3.WithPrefix())
	if err != nil {
		return nil, 0, err
	}

	nodes := make([]Node, 0, len(resp.Kvs))
	for _, kv := range resp.Kvs {
		var node Node
		if err := json.Unmarshal(kv.Value, &node); err != nil {
			if r.logger != nil {
				r.logger.Warn("registry decode failed", "service", service, "key", string(kv.Key), "error", err)
			}
			continue
		}
		nodes = append(nodes, node)
	}

	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].NodeID < nodes[j].NodeID
	})
	revision := int64(0)
	if resp.Header != nil {
		revision = resp.Header.Revision
	}
	return nodes, revision, nil
}

func (r *ETCD) Watch(ctx context.Context, service string) (<-chan WatchEvent, error) {
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
	if r.closed {
		r.mu.Unlock()
		return nil, ErrClosed
	}
	client := r.client
	prefix := r.servicePrefix(service)
	r.mu.Unlock()

	if client == nil {
		return nil, ErrClosed
	}

	initial, revision, err := r.listWithRevision(ctx, service)
	if err != nil {
		return nil, err
	}

	out := make(chan WatchEvent, 32)
	go func() {
		defer close(out)

		for _, node := range initial {
			select {
			case out <- WatchEvent{Type: EventUpsert, Node: node}:
			case <-ctx.Done():
				return
			}
		}

		watchOptions := []clientv3.OpOption{clientv3.WithPrefix()}
		if revision > 0 {
			watchOptions = append(watchOptions, clientv3.WithRev(revision+1))
		}
		watchCh := client.Watch(ctx, prefix, watchOptions...)
		for resp := range watchCh {
			if err := resp.Err(); err != nil {
				if r.logger != nil {
					r.logger.Warn("registry watch failed", "service", service, "error", err)
				}
				continue
			}
			for _, event := range resp.Events {
				switch event.Type {
				case clientv3.EventTypePut:
					var node Node
					if err := json.Unmarshal(event.Kv.Value, &node); err != nil {
						if r.logger != nil {
							r.logger.Warn("registry watch decode failed", "service", service, "key", string(event.Kv.Key), "error", err)
						}
						continue
					}
					select {
					case out <- WatchEvent{Type: EventUpsert, Node: node}:
					case <-ctx.Done():
						return
					}
				case clientv3.EventTypeDelete:
					select {
					case out <- WatchEvent{Type: EventDelete, Node: r.nodeFromKey(service, string(event.Kv.Key))}:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()
	return out, nil
}

func (r *ETCD) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	leases := make([]etcdLease, 0, len(r.leases))
	for _, lease := range r.leases {
		leases = append(leases, lease)
	}
	r.leases = make(map[string]etcdLease)
	client := r.client
	r.client = nil
	r.mu.Unlock()

	if client == nil {
		return nil
	}
	for _, lease := range leases {
		lease.cancel()
		cleanupCtx, cleanupCancel := etcdCleanupContext(nil)
		_, _ = client.Revoke(cleanupCtx, lease.lease)
		cleanupCancel()
	}
	return client.Close()
}

func (r *ETCD) nodeKey(service, nodeID string) string {
	return path.Join("/", r.prefix, strings.TrimSpace(service), strings.TrimSpace(nodeID))
}

func (r *ETCD) servicePrefix(service string) string {
	return path.Join("/", r.prefix, strings.TrimSpace(service)) + "/"
}

func (r *ETCD) nodeFromKey(service, key string) Node {
	return Node{
		Service: service,
		NodeID:  path.Base(key),
	}
}

func (r *ETCD) consumeKeepAlive(id, key string, leaseID clientv3.LeaseID, ch <-chan *clientv3.LeaseKeepAliveResponse) {
	for range ch {
	}

	r.mu.Lock()
	lease, ok := r.leases[id]
	if ok && lease.key == key && lease.lease == leaseID && !r.closed {
		delete(r.leases, id)
	}
	closed := r.closed
	r.mu.Unlock()

	if !closed && r.logger != nil {
		r.logger.Warn("registry keepalive stopped", "key", key)
	}
}

func (r *ETCD) removeLeaseIfCurrent(id string, lease etcdLease) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.leases[id]
	if !ok || current.key != lease.key || current.lease != lease.lease {
		return false
	}
	delete(r.leases, id)
	return true
}

func normalizeNamespace(namespace string) string {
	namespace = strings.TrimSpace(namespace)
	namespace = strings.Trim(namespace, "/")
	if namespace == "" {
		return "longheng"
	}
	return namespace
}

func etcdCleanupContext(parent context.Context) (context.Context, context.CancelFunc) {
	base := context.Background()
	if parent != nil {
		base = context.WithoutCancel(parent)
	}
	return context.WithTimeout(base, etcdCleanupTimeout)
}

func normalizeEndpoints(endpoints []string) []string {
	normalized := make([]string, 0, len(endpoints))
	for _, endpoint := range endpoints {
		endpoint = strings.TrimSpace(endpoint)
		if endpoint != "" {
			normalized = append(normalized, endpoint)
		}
	}
	return normalized
}
