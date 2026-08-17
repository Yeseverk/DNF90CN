package discovery

import (
	"context"
	"strings"
	"time"

	"longheng.io/server/internal/platform/registry"
)

type NodeProbe func(context.Context, registry.Node) error

type ProbeOptions struct {
	Service  string
	Timeout  time.Duration
	Probe    NodeProbe
	OnChange func(ProbeResult)
}

type ProbeResult struct {
	Service   string    `json:"service"`
	NodeID    string    `json:"node_id"`
	Healthy   bool      `json:"healthy"`
	Error     string    `json:"error,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}

func (r *Resolver) MustAlive(ctx context.Context, service, nodeID string) (bool, error) {
	node, ok, err := r.lookupNodeDirect(ctx, service, nodeID)
	if err != nil || !ok {
		return false, err
	}
	if r.rejectsMaintaining(node) {
		return false, nil
	}
	return nodeAccepting(node), nil
}

func (r *Resolver) ProbeNodes(ctx context.Context, options ProbeOptions) ([]ProbeResult, error) {
	if r == nil || r.reg == nil {
		return nil, ErrNoRegistry
	}
	if ctx == nil {
		ctx = context.Background()
	}
	service := strings.TrimSpace(options.Service)
	if service == "" {
		return nil, ErrNoNodes
	}
	if options.Probe == nil {
		return nil, ErrNoProbe
	}
	nodes, err := r.reg.List(ctx, service)
	if err != nil {
		return nil, err
	}
	now := r.now().UTC()
	results := make([]ProbeResult, 0, len(nodes))
	for _, node := range nodes {
		if strings.TrimSpace(node.NodeID) == "" {
			continue
		}
		result := ProbeResult{
			Service:   service,
			NodeID:    node.NodeID,
			CheckedAt: now,
		}
		probeCtx := ctx
		cancel := func() {}
		if options.Timeout > 0 {
			probeCtx, cancel = context.WithTimeout(ctx, options.Timeout)
		}
		err := options.Probe(probeCtx, node)
		cancel()
		if err != nil {
			result.Error = err.Error()
			r.MarkFailure(node.NodeID)
		} else {
			result.Healthy = true
			r.MarkSuccess(node.NodeID)
		}
		r.Invalidate(service)
		if options.OnChange != nil {
			options.OnChange(result)
		}
		results = append(results, result)
	}
	return results, nil
}

func (r *Resolver) lookupNodeDirect(ctx context.Context, service, nodeID string) (registry.Node, bool, error) {
	if r == nil || r.reg == nil {
		return registry.Node{}, false, ErrNoRegistry
	}
	if ctx == nil {
		ctx = context.Background()
	}
	service = strings.TrimSpace(service)
	nodeID = strings.TrimSpace(nodeID)
	if service == "" {
		return registry.Node{}, false, ErrNoNodes
	}
	if nodeID == "" {
		return registry.Node{}, false, nil
	}
	nodes, err := r.reg.List(ctx, service)
	if err != nil {
		return registry.Node{}, false, err
	}
	for _, node := range nodes {
		if strings.TrimSpace(node.NodeID) == nodeID {
			return node, true, nil
		}
	}
	return registry.Node{}, false, nil
}

func (r *Resolver) rejectsMaintaining(node registry.Node) bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	reject := r.rejectMaintaining
	r.mu.Unlock()
	return reject && nodeMaintaining(node.Meta)
}
