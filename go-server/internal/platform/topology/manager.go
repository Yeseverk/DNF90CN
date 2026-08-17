package topology

import (
	"context"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"longheng.io/server/internal/platform/registry"
)

const (
	RoleGate = "gate"
	RoleNode = "node"
	RoleMesh = "mesh"

	LinkGatewayBackend = "gateway_backend"
)

type Config struct {
	Enabled               bool
	Services              []string
	GatewayService        string
	GatewayBackendService string
	RejectMaintaining     bool
	RefreshInterval       time.Duration
}

type Snapshot struct {
	Enabled                bool              `json:"enabled"`
	RefreshIntervalSeconds int64             `json:"refresh_interval_seconds"`
	Services               []ServiceSnapshot `json:"services"`
	Gates                  []NodeSnapshot    `json:"gates"`
	Nodes                  []NodeSnapshot    `json:"nodes"`
	Mesh                   MeshSnapshot      `json:"mesh"`
	UpdatedAt              time.Time         `json:"updated_at,omitempty"`
	Error                  string            `json:"error,omitempty"`
}

type ServiceSnapshot struct {
	Service          string `json:"service"`
	Role             string `json:"role"`
	TotalNodes       int    `json:"total_nodes"`
	ReadyNodes       int    `json:"ready_nodes"`
	MaintainingNodes int    `json:"maintaining_nodes"`
	Routes           int    `json:"routes"`
	Events           int    `json:"events"`
	MeshServices     int    `json:"mesh_services"`
}

type NodeSnapshot struct {
	Service       string                 `json:"service"`
	NodeID        string                 `json:"node_id"`
	Role          string                 `json:"role"`
	ClusterID     string                 `json:"cluster_id,omitempty"`
	GID           int64                  `json:"gid,omitempty"`
	ShardID       int64                  `json:"shard_id,omitempty"`
	Environment   string                 `json:"environment,omitempty"`
	Zone          string                 `json:"zone,omitempty"`
	PublicAddr    string                 `json:"public_addr,omitempty"`
	PrivateAddr   string                 `json:"private_addr,omitempty"`
	AdminAddr     string                 `json:"admin_addr,omitempty"`
	Version       string                 `json:"version,omitempty"`
	DataVersion   string                 `json:"data_version,omitempty"`
	State         string                 `json:"state,omitempty"`
	Weight        int                    `json:"weight,omitempty"`
	Maintaining   bool                   `json:"maintaining"`
	Accepting     bool                   `json:"accepting"`
	Routes        []RouteSnapshot        `json:"routes,omitempty"`
	Events        []EventSnapshot        `json:"events,omitempty"`
	Services      []string               `json:"services,omitempty"`
	Observability *ObservabilitySnapshot `json:"observability,omitempty"`
	Meta          map[string]string      `json:"meta,omitempty"`
	RegisteredAt  time.Time              `json:"registered_at"`
}

type ObservabilitySnapshot struct {
	MetricsEnabled           bool   `json:"metrics_enabled"`
	MetricsPath              string `json:"metrics_path,omitempty"`
	MetricsURL               string `json:"metrics_url,omitempty"`
	MetricsSnapshotPath      string `json:"metrics_snapshot_path,omitempty"`
	MetricsSnapshotURL       string `json:"metrics_snapshot_url,omitempty"`
	MetricsRequireAdminToken bool   `json:"metrics_require_admin_token,omitempty"`
	PprofEnabled             bool   `json:"pprof_enabled"`
	PprofPath                string `json:"pprof_path,omitempty"`
	PprofURL                 string `json:"pprof_url,omitempty"`
}

type RouteSnapshot struct {
	ID         string `json:"id"`
	Group      string `json:"group,omitempty"`
	Internal   bool   `json:"internal,omitempty"`
	Stateful   bool   `json:"stateful,omitempty"`
	Authorized bool   `json:"authorized,omitempty"`
	Idempotent bool   `json:"idempotent,omitempty"`
}

type EventSnapshot struct {
	Name string `json:"name"`
}

type MeshSnapshot struct {
	Services []string       `json:"services"`
	Links    []LinkSnapshot `json:"links"`
}

type LinkSnapshot struct {
	Kind          string `json:"kind"`
	SourceService string `json:"source_service"`
	TargetService string `json:"target_service"`
	Available     bool   `json:"available"`
}

type Manager struct {
	registry registry.Registry
	logger   *slog.Logger

	mu         sync.RWMutex
	cfg        Config
	snapshot   Snapshot
	cancel     context.CancelFunc
	done       chan struct{}
	generation uint64
}

var detachedRootContext = context.Background()

func New(cfg Config, reg registry.Registry, logger *slog.Logger) *Manager {
	cfg = normalizeConfig(cfg)
	return &Manager{
		registry: reg,
		logger:   logger,
		cfg:      cfg,
		snapshot: Snapshot{
			Enabled:                cfg.Enabled,
			RefreshIntervalSeconds: int64(cfg.RefreshInterval / time.Second),
			Mesh:                   MeshSnapshot{Services: append([]string(nil), cfg.Services...)},
		},
	}
}

func (m *Manager) Name() string {
	return "topology-manager"
}

func (m *Manager) Start(ctx context.Context) error {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	generation := m.generation
	m.mu.RUnlock()
	return m.start(ctx, generation)
}

func (m *Manager) start(ctx context.Context, generation uint64) error {
	m.mu.RLock()
	enabled := m.cfg.Enabled
	started := m.cancel != nil
	interval := m.cfg.RefreshInterval
	current := m.generation
	m.mu.RUnlock()
	if generation != current || started || !enabled {
		return nil
	}
	if err := m.Refresh(ctx); err != nil {
		return err
	}

	m.mu.Lock()
	if m.generation != generation || m.cancel != nil || !m.cfg.Enabled {
		m.mu.Unlock()
		return nil
	}
	runCtx, cancel := context.WithCancel(detachedContext(ctx))
	done := make(chan struct{})
	m.cancel = cancel
	m.done = done
	m.mu.Unlock()

	go m.loop(runCtx, done, interval)
	return nil
}

func (m *Manager) AfterStart(ctx context.Context) error {
	if m == nil || !m.Enabled() {
		return nil
	}
	return m.Refresh(ctx)
}

func (m *Manager) Stop(ctx context.Context) error {
	if m == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	cancel := m.cancel
	done := m.done
	m.generation++
	m.cancel = nil
	m.done = nil
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Manager) Configure(cfg Config) {
	if m == nil {
		return
	}
	cfg = normalizeConfig(cfg)
	m.mu.Lock()
	wasEnabled := m.cfg.Enabled
	intervalChanged := m.cfg.RefreshInterval != cfg.RefreshInterval
	cancel := m.cancel
	done := m.done
	m.generation++
	generation := m.generation
	m.cfg = cfg
	m.snapshot.Enabled = cfg.Enabled
	m.snapshot.RefreshIntervalSeconds = int64(cfg.RefreshInterval / time.Second)
	if wasEnabled && (!cfg.Enabled || intervalChanged) {
		m.cancel = nil
		m.done = nil
	}
	m.mu.Unlock()

	if wasEnabled && (!cfg.Enabled || intervalChanged) && cancel != nil {
		cancel()
	}
	if cfg.Enabled && (!wasEnabled || intervalChanged) {
		if done != nil {
			go m.startAfterDone(done, generation)
			return
		}
		_ = m.start(detachedRootContext, generation)
	}
}

func (m *Manager) startAfterDone(done <-chan struct{}, generation uint64) {
	<-done
	_ = m.start(detachedRootContext, generation)
}

func detachedContext(ctx context.Context) context.Context {
	if ctx == nil {
		return detachedRootContext
	}
	return context.WithoutCancel(ctx)
}

func (m *Manager) Enabled() bool {
	if m == nil {
		return false
	}
	m.mu.RLock()
	enabled := m.cfg.Enabled
	m.mu.RUnlock()
	return enabled
}

func (m *Manager) Refresh(ctx context.Context) error {
	if m == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.RLock()
	cfg := m.cfg
	reg := m.registry
	m.mu.RUnlock()
	if !cfg.Enabled {
		m.storeSnapshot(buildSnapshot(cfg, nil, nil))
		return nil
	}
	if reg == nil {
		snapshot := buildSnapshot(cfg, nil, nil)
		snapshot.Error = "registry is required"
		m.storeSnapshot(snapshot)
		return registry.ErrClosed
	}

	nodesByService := make(map[string][]registry.Node, len(cfg.Services))
	var firstErr error
	for _, service := range cfg.Services {
		nodes, err := reg.List(ctx, service)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		nodesByService[service] = nodes
	}
	snapshot := buildSnapshot(cfg, nodesByService, firstErr)
	m.storeSnapshot(snapshot)
	return firstErr
}

func (m *Manager) Snapshot() Snapshot {
	if m == nil {
		return Snapshot{}
	}
	m.mu.RLock()
	snapshot := cloneSnapshot(m.snapshot)
	m.mu.RUnlock()
	return snapshot
}

func (m *Manager) loop(ctx context.Context, done chan struct{}, interval time.Duration) {
	defer close(done)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := m.Refresh(ctx); err != nil && m.logger != nil {
				m.logger.Warn("topology refresh failed", "error", err)
			}
		}
	}
}

func (m *Manager) storeSnapshot(snapshot Snapshot) {
	m.mu.Lock()
	m.snapshot = snapshot
	m.mu.Unlock()
}

func buildSnapshot(cfg Config, nodesByService map[string][]registry.Node, err error) Snapshot {
	snapshot := Snapshot{
		Enabled:                cfg.Enabled,
		RefreshIntervalSeconds: int64(cfg.RefreshInterval / time.Second),
		UpdatedAt:              time.Now().UTC(),
		Mesh: MeshSnapshot{
			Services: append([]string(nil), cfg.Services...),
		},
	}
	if err != nil {
		snapshot.Error = err.Error()
	}
	serviceReady := make(map[string]bool, len(cfg.Services))
	for _, service := range cfg.Services {
		role := roleForService(cfg, service)
		nodes := nodesByService[service]
		serviceSnapshot := ServiceSnapshot{Service: service, Role: role}
		for _, node := range nodes {
			nodeSnapshot := nodeFromRegistry(cfg, role, node)
			serviceSnapshot.TotalNodes++
			serviceSnapshot.Routes += len(nodeSnapshot.Routes)
			serviceSnapshot.Events += len(nodeSnapshot.Events)
			serviceSnapshot.MeshServices += len(nodeSnapshot.Services)
			if nodeSnapshot.Maintaining {
				serviceSnapshot.MaintainingNodes++
			}
			if nodeSnapshot.Accepting {
				serviceSnapshot.ReadyNodes++
				serviceReady[service] = true
			}
			if role == RoleGate {
				snapshot.Gates = append(snapshot.Gates, nodeSnapshot)
			} else {
				snapshot.Nodes = append(snapshot.Nodes, nodeSnapshot)
			}
		}
		snapshot.Services = append(snapshot.Services, serviceSnapshot)
	}
	if cfg.GatewayService != "" && cfg.GatewayBackendService != "" {
		snapshot.Mesh.Links = append(snapshot.Mesh.Links, LinkSnapshot{
			Kind:          LinkGatewayBackend,
			SourceService: cfg.GatewayService,
			TargetService: cfg.GatewayBackendService,
			Available:     serviceReady[cfg.GatewayService] && serviceReady[cfg.GatewayBackendService],
		})
	}
	sortSnapshot(&snapshot)
	return snapshot
}

func nodeFromRegistry(cfg Config, role string, node registry.Node) NodeSnapshot {
	meta := cloneMeta(node.Meta)
	maintaining := parseBool(meta["maintaining"]) || parseBool(meta["is_maintaining"]) || parseBool(meta["maintenance"])
	state := firstNonEmpty(node.State, meta["state"])
	accepting := state != "stopping" && state != "stopped" && (!cfg.RejectMaintaining || !maintaining)
	return NodeSnapshot{
		Service:       strings.TrimSpace(node.Service),
		NodeID:        strings.TrimSpace(node.NodeID),
		Role:          role,
		ClusterID:     strings.TrimSpace(meta["cluster_id"]),
		GID:           parseInt64(meta["gid"]),
		ShardID:       parseInt64(meta["shard_id"]),
		Environment:   firstNonEmpty(meta["environment"], meta["env"]),
		Zone:          strings.TrimSpace(meta["zone"]),
		PublicAddr:    firstNonEmpty(node.PublicAddr, meta["public_addr"]),
		PrivateAddr:   firstNonEmpty(node.PrivateAddr, meta["private_addr"]),
		AdminAddr:     strings.TrimSpace(meta["admin_addr"]),
		Version:       strings.TrimSpace(meta["version"]),
		DataVersion:   strings.TrimSpace(meta["data_version"]),
		State:         state,
		Weight:        nodeWeight(node),
		Maintaining:   maintaining,
		Accepting:     accepting,
		Routes:        routesFromRegistry(node.Routes),
		Events:        eventsFromRegistry(node.Events),
		Services:      cloneStrings(node.Services),
		Observability: obsFromMeta(meta),
		Meta:          meta,
		RegisteredAt:  node.RegisteredAt,
	}
}

func normalizeConfig(cfg Config) Config {
	cfg.Services = normalizeServices(cfg.Services)
	cfg.GatewayService = strings.TrimSpace(cfg.GatewayService)
	cfg.GatewayBackendService = strings.TrimSpace(cfg.GatewayBackendService)
	if cfg.RefreshInterval <= 0 {
		cfg.RefreshInterval = 5 * time.Second
	}
	return cfg
}

func normalizeServices(services []string) []string {
	seen := make(map[string]struct{}, len(services))
	out := make([]string, 0, len(services))
	for _, service := range services {
		service = strings.TrimSpace(service)
		if service == "" {
			continue
		}
		if _, ok := seen[service]; ok {
			continue
		}
		seen[service] = struct{}{}
		out = append(out, service)
	}
	sort.Strings(out)
	return out
}

func roleForService(cfg Config, service string) string {
	if strings.TrimSpace(service) == cfg.GatewayService {
		return RoleGate
	}
	if cfg.GatewayBackendService != "" && strings.TrimSpace(service) != cfg.GatewayBackendService {
		return RoleMesh
	}
	return RoleNode
}

func sortSnapshot(snapshot *Snapshot) {
	sort.Slice(snapshot.Services, func(i, j int) bool {
		return snapshot.Services[i].Service < snapshot.Services[j].Service
	})
	sort.Slice(snapshot.Gates, func(i, j int) bool {
		return snapshot.Gates[i].NodeID < snapshot.Gates[j].NodeID
	})
	sort.Slice(snapshot.Nodes, func(i, j int) bool {
		if snapshot.Nodes[i].Service != snapshot.Nodes[j].Service {
			return snapshot.Nodes[i].Service < snapshot.Nodes[j].Service
		}
		return snapshot.Nodes[i].NodeID < snapshot.Nodes[j].NodeID
	})
	sort.Slice(snapshot.Mesh.Links, func(i, j int) bool {
		if snapshot.Mesh.Links[i].SourceService != snapshot.Mesh.Links[j].SourceService {
			return snapshot.Mesh.Links[i].SourceService < snapshot.Mesh.Links[j].SourceService
		}
		return snapshot.Mesh.Links[i].TargetService < snapshot.Mesh.Links[j].TargetService
	})
}

func cloneSnapshot(snapshot Snapshot) Snapshot {
	snapshot.Services = append([]ServiceSnapshot(nil), snapshot.Services...)
	snapshot.Gates = append([]NodeSnapshot(nil), snapshot.Gates...)
	for idx := range snapshot.Gates {
		snapshot.Gates[idx].Meta = cloneMeta(snapshot.Gates[idx].Meta)
		snapshot.Gates[idx].Routes = append([]RouteSnapshot(nil), snapshot.Gates[idx].Routes...)
		snapshot.Gates[idx].Events = append([]EventSnapshot(nil), snapshot.Gates[idx].Events...)
		snapshot.Gates[idx].Services = cloneStrings(snapshot.Gates[idx].Services)
		snapshot.Gates[idx].Observability = cloneObservability(snapshot.Gates[idx].Observability)
	}
	snapshot.Nodes = append([]NodeSnapshot(nil), snapshot.Nodes...)
	for idx := range snapshot.Nodes {
		snapshot.Nodes[idx].Meta = cloneMeta(snapshot.Nodes[idx].Meta)
		snapshot.Nodes[idx].Routes = append([]RouteSnapshot(nil), snapshot.Nodes[idx].Routes...)
		snapshot.Nodes[idx].Events = append([]EventSnapshot(nil), snapshot.Nodes[idx].Events...)
		snapshot.Nodes[idx].Services = cloneStrings(snapshot.Nodes[idx].Services)
		snapshot.Nodes[idx].Observability = cloneObservability(snapshot.Nodes[idx].Observability)
	}
	snapshot.Mesh.Services = append([]string(nil), snapshot.Mesh.Services...)
	snapshot.Mesh.Links = append([]LinkSnapshot(nil), snapshot.Mesh.Links...)
	return snapshot
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

func cloneObservability(observability *ObservabilitySnapshot) *ObservabilitySnapshot {
	if observability == nil {
		return nil
	}
	out := *observability
	return &out
}

func obsFromMeta(meta map[string]string) *ObservabilitySnapshot {
	metricsEnabled := parseBool(meta["metrics_enabled"])
	pprofEnabled := parseBool(meta["pprof_enabled"])
	metricsPath := ""
	if metricsEnabled {
		metricsPath = firstNonEmpty(meta["metrics_path"], "/metrics")
	}
	metricsSnapPath := strings.TrimSpace(meta["metrics_snapshot_path"])
	if metricsSnapPath == "" && meta["metrics_enabled"] != "" {
		metricsSnapPath = "/debug/metrics"
	}
	pprofPath := ""
	if pprofEnabled {
		pprofPath = firstNonEmpty(meta["pprof_path"], "/debug/pprof/")
	}
	if !metricsEnabled && !pprofEnabled && metricsSnapPath == "" {
		return nil
	}
	adminAddr := strings.TrimSpace(meta["admin_addr"])
	return &ObservabilitySnapshot{
		MetricsEnabled:           metricsEnabled,
		MetricsPath:              metricsPath,
		MetricsURL:               endpointURL(adminAddr, metricsPath),
		MetricsSnapshotPath:      metricsSnapPath,
		MetricsSnapshotURL:       endpointURL(adminAddr, metricsSnapPath),
		MetricsRequireAdminToken: parseBool(meta["metrics_require_admin_token"]),
		PprofEnabled:             pprofEnabled,
		PprofPath:                pprofPath,
		PprofURL:                 endpointURL(adminAddr, pprofPath),
	}
}

func endpointURL(addr, path string) string {
	addr = strings.TrimSpace(addr)
	path = strings.TrimSpace(path)
	if addr == "" || path == "" {
		return ""
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return strings.TrimRight(addr, "/") + path
	}
	if strings.HasPrefix(addr, ":") {
		return "http://127.0.0.1" + addr + path
	}
	return "http://" + addr + path
}

func parseBool(value string) bool {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func parseInt64(value string) int64 {
	parsed, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return parsed
}

func parseInt(value string) int {
	parsed, _ := strconv.Atoi(strings.TrimSpace(value))
	return parsed
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func nodeWeight(node registry.Node) int {
	if node.Weight > 0 {
		return node.Weight
	}
	weight := parseInt(node.Meta["weight"])
	if weight > 0 {
		return weight
	}
	return 1
}

func routesFromRegistry(routes []registry.Route) []RouteSnapshot {
	if len(routes) == 0 {
		return nil
	}
	out := make([]RouteSnapshot, 0, len(routes))
	for _, route := range routes {
		id := strings.TrimSpace(route.ID)
		if id == "" {
			continue
		}
		out = append(out, RouteSnapshot{
			ID:         id,
			Group:      strings.TrimSpace(route.Group),
			Internal:   route.Internal,
			Stateful:   route.Stateful,
			Authorized: route.Authorized,
			Idempotent: route.Idempotent,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

func eventsFromRegistry(events []registry.Event) []EventSnapshot {
	if len(events) == 0 {
		return nil
	}
	out := make([]EventSnapshot, 0, len(events))
	for _, event := range events {
		name := strings.TrimSpace(event.Name)
		if name == "" {
			continue
		}
		out = append(out, EventSnapshot{Name: name})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

func cloneStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}
