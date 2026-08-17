package cluster

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"longheng.io/server/internal/platform/bus"
	"longheng.io/server/internal/platform/config"
	"longheng.io/server/internal/platform/health"
	"longheng.io/server/internal/platform/httpx"
	"longheng.io/server/internal/platform/registry"
	"longheng.io/server/pkg/contracts"
)

const (
	StateStarting    = "starting"
	StateReady       = "ready"
	StateMaintaining = "maintaining"
	StateStopping    = "stopping"
	StateStopped     = "stopped"
)

type NodeSnapshot struct {
	ClusterID   string            `json:"cluster_id"`
	GID         int64             `json:"gid,omitempty"`
	VirtualGID  int64             `json:"virtual_gid,omitempty"`
	ShardID     int64             `json:"shard_id,omitempty"`
	Service     string            `json:"service"`
	NodeID      string            `json:"node_id"`
	Environment string            `json:"environment"`
	Zone        string            `json:"zone"`
	PublicAddr  string            `json:"public_addr,omitempty"`
	PrivateAddr string            `json:"private_addr,omitempty"`
	AdminAddr   string            `json:"admin_addr,omitempty"`
	Version     string            `json:"version,omitempty"`
	DataVersion string            `json:"data_version,omitempty"`
	State       string            `json:"state"`
	Maintaining bool              `json:"maintaining"`
	Weight      int               `json:"weight,omitempty"`
	Routes      []registry.Route  `json:"routes,omitempty"`
	Events      []registry.Event  `json:"events,omitempty"`
	Services    []string          `json:"services,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	GMVisible   bool              `json:"gm_visible"`
	StartedAt   time.Time         `json:"started_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	Meta        map[string]string `json:"meta,omitempty"`
}

type Coordinator struct {
	cfg        config.ServiceConfig
	registry   registry.Registry
	bus        bus.Bus
	health     *health.Service
	logger     *slog.Logger
	adminToken string
	admin      func(http.HandlerFunc) http.HandlerFunc

	mu          sync.RWMutex
	state       string
	maintaining bool
	weight      int
	routes      []registry.Route
	events      []registry.Event
	services    []string
	tags        []string
	gmVisible   bool
	startedAt   time.Time
	updatedAt   time.Time
	cancel      context.CancelFunc
	done        chan struct{}
}

func New(cfg config.ServiceConfig, reg registry.Registry, eventBus bus.Bus, healthSvc *health.Service, mux *http.ServeMux, adminToken string, logger *slog.Logger, adminWrap ...func(http.HandlerFunc) http.HandlerFunc) *Coordinator {
	wrap := func(next http.HandlerFunc) http.HandlerFunc {
		return httpx.WrapAdmin(adminToken, logger, "cluster", next)
	}
	if len(adminWrap) > 0 && adminWrap[0] != nil {
		wrap = adminWrap[0]
	}
	c := &Coordinator{
		cfg:         cfg,
		registry:    reg,
		bus:         eventBus,
		health:      healthSvc,
		logger:      logger,
		adminToken:  adminToken,
		admin:       wrap,
		state:       StateStarting,
		maintaining: cfg.Cluster.Maintaining,
		gmVisible:   true,
		startedAt:   time.Now().UTC(),
	}
	c.updatedAt = c.startedAt
	c.registerRoutes(mux)
	return c
}

func (c *Coordinator) Name() string {
	return "cluster-coordinator"
}

func (c *Coordinator) Start(context.Context) error {
	return nil
}

func (c *Coordinator) AfterStart(ctx context.Context) error {
	c.health.SetStartupStep("cluster-registry", health.StateStarting, "registering distributed node")
	state := StateReady
	if c.maintaining {
		state = StateMaintaining
	}
	if err := c.setState(ctx, state, c.maintaining, contracts.TopicClusterServiceStarted); err != nil {
		c.health.SetStartupStep("cluster-registry", health.StateDegraded, err.Error())
		return err
	}
	c.startHeartbeat(ctx)
	c.health.SetStartupStep("cluster-registry", health.StateReady, "distributed node registered")
	return nil
}

func (c *Coordinator) BeforeStop(ctx context.Context) error {
	return c.setState(ctx, StateStopping, true, contracts.TopicClusterServiceState)
}

func (c *Coordinator) Stop(ctx context.Context) error {
	c.stopHeartbeat(ctx)
	now := time.Now().UTC()
	c.mu.Lock()
	c.state = StateStopped
	c.maintaining = true
	c.updatedAt = now
	event := c.eventLocked(now)
	c.mu.Unlock()

	if err := c.registry.Deregister(ctx, c.cfg.Service.Name, c.cfg.Service.NodeID); err != nil {
		return err
	}
	return c.publish(ctx, contracts.TopicClusterServiceStopped, event)
}

func (c *Coordinator) SetMaintaining(ctx context.Context, maintaining bool) error {
	state := StateReady
	if maintaining {
		state = StateMaintaining
	}
	return c.setState(ctx, state, maintaining, contracts.TopicClusterServiceState)
}

func (c *Coordinator) SetWeight(weight int) {
	if c == nil {
		return
	}
	if weight <= 0 {
		weight = 1
	}
	c.mu.Lock()
	c.weight = weight
	c.updatedAt = time.Now().UTC()
	c.mu.Unlock()
}

func (c *Coordinator) SetRoutes(routes []registry.Route) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.routes = normalizeRoutes(routes)
	c.updatedAt = time.Now().UTC()
	c.mu.Unlock()
}

func (c *Coordinator) SetEvents(events []registry.Event) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.events = normalizeEvents(events)
	c.updatedAt = time.Now().UTC()
	c.mu.Unlock()
}

func (c *Coordinator) SetMeshServices(services []string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.services = normalizeStrings(services)
	c.updatedAt = time.Now().UTC()
	c.mu.Unlock()
}

func (c *Coordinator) SetTags(tags []string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.tags = normalizeStrings(tags)
	c.updatedAt = time.Now().UTC()
	c.mu.Unlock()
}

func (c *Coordinator) SetGMVisible(visible bool) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.gmVisible = visible
	c.updatedAt = time.Now().UTC()
	c.mu.Unlock()
}

func (c *Coordinator) Snapshot() NodeSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.snapshotLocked(c.updatedAt)
}

func (c *Coordinator) setState(ctx context.Context, state string, maintaining bool, topic string) error {
	now := time.Now().UTC()
	c.mu.Lock()
	c.state = state
	c.maintaining = maintaining
	c.updatedAt = now
	node := c.registryNodeLocked(now)
	event := c.eventLocked(now)
	c.mu.Unlock()

	if err := c.registry.Register(ctx, node); err != nil {
		return err
	}
	return c.publish(ctx, topic, event)
}

func (c *Coordinator) heartbeat(ctx context.Context) error {
	now := time.Now().UTC()
	c.mu.Lock()
	node := c.registryNodeLocked(now)
	event := c.eventLocked(now)
	c.updatedAt = now
	c.mu.Unlock()

	if err := c.registry.Register(ctx, node); err != nil {
		return err
	}
	return c.publish(ctx, contracts.TopicClusterServiceBeat, event)
}

func (c *Coordinator) startHeartbeat(ctx context.Context) {
	if ctx == nil {
		return
	}
	c.mu.Lock()
	if c.cancel != nil {
		c.mu.Unlock()
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	c.cancel = cancel
	c.done = done
	interval := time.Duration(c.cfg.Cluster.HeartbeatIntervalSeconds) * time.Second
	if interval <= 0 {
		interval = 10 * time.Second
	}
	c.mu.Unlock()

	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				if err := c.heartbeat(runCtx); err != nil && c.logger != nil {
					c.logger.Warn("cluster heartbeat failed", "service", c.cfg.Service.Name, "node_id", c.cfg.Service.NodeID, "error", err)
				}
			}
		}
	}()
}

func (c *Coordinator) stopHeartbeat(ctx context.Context) {
	c.mu.Lock()
	cancel := c.cancel
	done := c.done
	c.cancel = nil
	c.done = nil
	c.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done == nil {
		return
	}
	select {
	case <-done:
	case <-ctx.Done():
	}
}

func (c *Coordinator) registryNodeLocked(now time.Time) registry.Node {
	return registry.Node{
		Service:     c.cfg.Service.Name,
		NodeID:      c.cfg.Service.NodeID,
		GID:         c.cfg.Cluster.GID,
		VirtualGID:  c.cfg.Cluster.VirtualGID,
		ShardID:     c.cfg.Cluster.ShardID,
		PublicAddr:  c.cfg.Service.PublicAddr,
		PrivateAddr: c.cfg.Service.PrivateAddr,
		State:       c.state,
		Weight:      c.weightLocked(),
		Routes:      cloneRoutes(c.routes),
		Events:      cloneEvents(c.events),
		Services:    append([]string(nil), c.services...),
		Tags:        append([]string(nil), c.tags...),
		GMVisible:   c.gmVisible,
		Meta:        c.metaLocked(now),
	}
}

func (c *Coordinator) metaLocked(now time.Time) map[string]string {
	meta := map[string]string{
		"cluster_id":                  c.cfg.Cluster.ID,
		"environment":                 c.cfg.Service.Environment,
		"env":                         c.cfg.Service.Environment,
		"gid":                         strconv.FormatInt(c.cfg.Cluster.GID, 10),
		"virtual_gid":                 strconv.FormatInt(c.cfg.Cluster.VirtualGID, 10),
		"shard_id":                    strconv.FormatInt(c.cfg.Cluster.ShardID, 10),
		"zone":                        c.cfg.Cluster.Zone,
		"version":                     c.cfg.Cluster.Version,
		"data_version":                c.cfg.Cluster.DataVersion,
		"state":                       c.state,
		"maintaining":                 strconv.FormatBool(c.maintaining),
		"gm_visible":                  strconv.FormatBool(c.gmVisible),
		"admin_addr":                  c.cfg.Admin.Listen,
		"started_at":                  c.startedAt.Format(time.RFC3339Nano),
		"updated_at":                  now.Format(time.RFC3339Nano),
		"heartbeat_at":                now.Format(time.RFC3339Nano),
		"metrics_enabled":             strconv.FormatBool(c.cfg.Metrics.PrometheusEnabled),
		"metrics_snapshot_path":       "/debug/metrics",
		"metrics_require_admin_token": strconv.FormatBool(c.cfg.Metrics.RequireAdminToken),
		"pprof_enabled":               strconv.FormatBool(c.cfg.Debug.PprofEnabled),
	}
	if c.cfg.Metrics.PrometheusEnabled {
		meta["metrics_path"] = "/metrics"
	}
	if c.cfg.Debug.PprofEnabled {
		meta["pprof_path"] = "/debug/pprof/"
	}
	if c.cfg.Service.PublicAddr != "" {
		meta["public_addr"] = c.cfg.Service.PublicAddr
	}
	if c.cfg.Service.PrivateAddr != "" {
		meta["private_addr"] = c.cfg.Service.PrivateAddr
	}
	return meta
}

func (c *Coordinator) weightLocked() int {
	if c.weight > 0 {
		return c.weight
	}
	return 1
}

func (c *Coordinator) snapshotLocked(now time.Time) NodeSnapshot {
	return NodeSnapshot{
		ClusterID:   c.cfg.Cluster.ID,
		GID:         c.cfg.Cluster.GID,
		VirtualGID:  c.cfg.Cluster.VirtualGID,
		ShardID:     c.cfg.Cluster.ShardID,
		Service:     c.cfg.Service.Name,
		NodeID:      c.cfg.Service.NodeID,
		Environment: c.cfg.Service.Environment,
		Zone:        c.cfg.Cluster.Zone,
		PublicAddr:  c.cfg.Service.PublicAddr,
		PrivateAddr: c.cfg.Service.PrivateAddr,
		AdminAddr:   c.cfg.Admin.Listen,
		Version:     c.cfg.Cluster.Version,
		DataVersion: c.cfg.Cluster.DataVersion,
		State:       c.state,
		Maintaining: c.maintaining,
		Weight:      c.weightLocked(),
		Routes:      cloneRoutes(c.routes),
		Events:      cloneEvents(c.events),
		Services:    append([]string(nil), c.services...),
		Tags:        append([]string(nil), c.tags...),
		GMVisible:   c.gmVisible,
		StartedAt:   c.startedAt,
		UpdatedAt:   now,
		Meta:        c.metaLocked(now),
	}
}

func (c *Coordinator) eventLocked(now time.Time) contracts.ClusterServiceEvent {
	snapshot := c.snapshotLocked(now)
	return contracts.ClusterServiceEvent{
		ClusterID:   snapshot.ClusterID,
		GID:         snapshot.GID,
		VirtualGID:  snapshot.VirtualGID,
		ShardID:     snapshot.ShardID,
		Service:     snapshot.Service,
		NodeID:      snapshot.NodeID,
		Environment: snapshot.Environment,
		Zone:        snapshot.Zone,
		PublicAddr:  snapshot.PublicAddr,
		PrivateAddr: snapshot.PrivateAddr,
		AdminAddr:   snapshot.AdminAddr,
		Version:     snapshot.Version,
		DataVersion: snapshot.DataVersion,
		State:       snapshot.State,
		Maintaining: snapshot.Maintaining,
		Meta:        snapshot.Meta,
		At:          now,
	}
}

func (c *Coordinator) publish(ctx context.Context, topic string, event contracts.ClusterServiceEvent) error {
	if c.bus == nil {
		return nil
	}
	if err := c.bus.Publish(ctx, topic, event); err != nil {
		return fmt.Errorf("publish cluster event %s: %w", topic, err)
	}
	return nil
}

func (c *Coordinator) registerRoutes(mux *http.ServeMux) {
	wrap := c.admin
	if wrap == nil {
		wrap = func(next http.HandlerFunc) http.HandlerFunc {
			return httpx.WrapAdmin(c.adminToken, c.logger, "cluster", next)
		}
	}
	mux.HandleFunc("/cluster/node", wrap(c.handleNode))
	mux.HandleFunc("/cluster/nodes", wrap(c.handleNodes))
	mux.HandleFunc("/cluster/maintaining", wrap(c.handleMaintaining))
}

func (c *Coordinator) handleNode(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, c.Snapshot())
}

func (c *Coordinator) handleNodes(w http.ResponseWriter, r *http.Request) {
	service := strings.TrimSpace(r.URL.Query().Get("service"))
	if service == "" {
		service = c.cfg.Service.Name
	}
	nodes, err := c.registry.List(r.Context(), service)
	if err != nil {
		httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"service": service,
		"nodes":   nodes,
	})
}

func (c *Coordinator) handleMaintaining(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	enabled, err := parseBoolParam(r.URL.Query().Get("enabled"))
	if err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := c.SetMaintaining(r.Context(), enabled); err != nil {
		httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, c.Snapshot())
}

func parseBoolParam(value string) (bool, error) {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "1", "true", "yes", "y", "on":
		return true, nil
	case "0", "false", "no", "n", "off":
		return false, nil
	default:
		return false, fmt.Errorf("enabled must be true or false")
	}
}

func normalizeRoutes(routes []registry.Route) []registry.Route {
	if len(routes) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(routes))
	out := make([]registry.Route, 0, len(routes))
	for _, route := range routes {
		route.ID = strings.TrimSpace(route.ID)
		route.Group = strings.TrimSpace(route.Group)
		if route.ID == "" {
			continue
		}
		if _, ok := seen[route.ID]; ok {
			continue
		}
		seen[route.ID] = struct{}{}
		out = append(out, route)
	}
	return out
}

func normalizeEvents(events []registry.Event) []registry.Event {
	if len(events) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(events))
	out := make([]registry.Event, 0, len(events))
	for _, event := range events {
		event.Name = strings.TrimSpace(event.Name)
		if event.Name == "" {
			continue
		}
		if _, ok := seen[event.Name]; ok {
			continue
		}
		seen[event.Name] = struct{}{}
		out = append(out, event)
	}
	return out
}

func normalizeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func cloneRoutes(routes []registry.Route) []registry.Route {
	if len(routes) == 0 {
		return nil
	}
	return append([]registry.Route(nil), routes...)
}

func cloneEvents(events []registry.Event) []registry.Event {
	if len(events) == 0 {
		return nil
	}
	return append([]registry.Event(nil), events...)
}
