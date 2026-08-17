package adapters

import (
	"fmt"
	"sort"
	"strings"

	"longheng.io/server/internal/platform/config"
)

type Status string

const (
	StatusStable        Status = "stable"
	StatusExperimental  Status = "experimental"
	StatusPlanned       Status = "planned"
	StatusSkeleton      Status = "skeleton"
	StatusInterfaceOnly Status = "interface_only"
)

type Matrix struct {
	SchemaVersion int          `json:"schema_version"`
	Selected      Selected     `json:"selected"`
	Capabilities  []Capability `json:"capabilities"`
	Gaps          []Gap        `json:"gaps,omitempty"`
}

type Selected struct {
	Registry   string   `json:"registry"`
	Config     string   `json:"config"`
	EventBus   string   `json:"event_bus"`
	Cache      string   `json:"cache"`
	Lock       string   `json:"lock"`
	Transports []string `json:"transports"`
}

type Capability struct {
	Area       string   `json:"area"`
	Backend    string   `json:"backend"`
	Status     Status   `json:"status"`
	ConfigKind string   `json:"config_kind,omitempty"`
	Features   []string `json:"features"`
	Smoke      string   `json:"smoke,omitempty"`
	Notes      []string `json:"notes,omitempty"`
}

type Gap struct {
	Area    string `json:"area"`
	Backend string `json:"backend"`
	Reason  string `json:"reason"`
}

func MatrixFromConfig(cfg config.ServiceConfig) Matrix {
	matrix := Matrix{
		SchemaVersion: 1,
		Selected: Selected{
			Registry:   normalizeKind(cfg.Registry.Kind, "memory"),
			Config:     "file",
			EventBus:   normalizeKind(cfg.Bus.Kind, "memory"),
			Cache:      normalizeKind(cfg.Cache.Kind, "memory"),
			Lock:       normalizeKind(cfg.Lock.Kind, "memory"),
			Transports: selectedTransports(cfg.Gateway),
		},
		Capabilities: defaultCapabilities(),
	}
	matrix.Gaps = selectedGaps(matrix.Selected, matrix.Capabilities)
	return matrix
}

func ValidateSelected(matrix Matrix) error {
	var missing []string
	available := make(map[string]map[string]Status)
	for _, capability := range matrix.Capabilities {
		area := strings.TrimSpace(capability.Area)
		backend := strings.TrimSpace(capability.Backend)
		if area == "" || backend == "" {
			continue
		}
		if _, ok := available[area]; !ok {
			available[area] = make(map[string]Status)
		}
		available[area][backend] = capability.Status
	}
	for _, selected := range []struct {
		area    string
		backend string
	}{
		{area: "registry", backend: matrix.Selected.Registry},
		{area: "config", backend: matrix.Selected.Config},
		{area: "eventbus", backend: matrix.Selected.EventBus},
		{area: "cache", backend: matrix.Selected.Cache},
		{area: "lock", backend: matrix.Selected.Lock},
	} {
		missing = append(missing, validSelectedBackend(available, selected.area, normalizeKind(selected.backend, "memory"))...)
	}
	transports := matrix.Selected.Transports
	if len(transports) == 0 {
		transports = []string{"tcp"}
	}
	for _, transport := range transports {
		missing = append(missing, validSelectedBackend(available, "transport", normalizeKind(transport, "tcp"))...)
	}
	if len(missing) > 0 {
		return fmt.Errorf("adapter matrix invalid: %s", strings.Join(missing, "; "))
	}
	return nil
}

func defaultCapabilities() []Capability {
	capabilities := []Capability{
		{
			Area:       "registry",
			Backend:    "memory",
			Status:     StatusStable,
			ConfigKind: "memory",
			Features:   []string{"register", "deregister", "list", "watch"},
			Smoke:      "go test ./internal/platform/registry",
		},
		{
			Area:       "registry",
			Backend:    "redis",
			Status:     StatusStable,
			ConfigKind: "redis",
			Features:   []string{"register", "deregister", "list", "lease_ttl"},
			Smoke:      "LONGHENG_REDIS_INTEGRATION=1 go test ./internal/platform/registry",
		},
		{
			Area:       "registry",
			Backend:    "etcd",
			Status:     StatusExperimental,
			ConfigKind: "etcd",
			Features:   []string{"register", "deregister", "list", "watch", "lease_keepalive"},
			Smoke:      "external etcd integration smoke",
		},
		{
			Area:       "config",
			Backend:    "file",
			Status:     StatusStable,
			ConfigKind: "file",
			Features:   []string{"toml", "validate", "reload", "redaction"},
			Smoke:      "go test ./internal/platform/config ./internal/platform/reload",
		},
		{
			Area:       "config",
			Backend:    "etcd",
			Status:     StatusSkeleton,
			ConfigKind: "etcd",
			Features:   []string{"watch", "versioned_snapshot"},
			Notes:      []string{"interface reserved; provider not wired"},
		},
		{
			Area:       "config",
			Backend:    "consul",
			Status:     StatusSkeleton,
			ConfigKind: "consul",
			Features:   []string{"watch", "kv_prefix"},
			Notes:      []string{"interface reserved; provider not wired"},
		},
		{
			Area:       "config",
			Backend:    "nacos",
			Status:     StatusSkeleton,
			ConfigKind: "nacos",
			Features:   []string{"watch", "namespace_group_dataid"},
			Notes:      []string{"interface reserved; provider not wired"},
		},
		{
			Area:       "eventbus",
			Backend:    "memory",
			Status:     StatusStable,
			ConfigKind: "memory",
			Features:   []string{"publish", "subscribe", "panic_recovery"},
			Smoke:      "go test ./internal/platform/bus",
		},
		{
			Area:       "eventbus",
			Backend:    "redis",
			Status:     StatusStable,
			ConfigKind: "redis",
			Features:   []string{"pubsub", "typed_payload", "reconnect"},
			Smoke:      "LONGHENG_REDIS_INTEGRATION=1 go test ./internal/platform/bus",
		},
		{
			Area:       "eventbus",
			Backend:    "nats",
			Status:     StatusStable,
			ConfigKind: "nats",
			Features:   []string{"pubsub", "typed_payload", "drain"},
			Smoke:      "external nats integration smoke",
		},
		{
			Area:       "external_api",
			Backend:    "http_webhook",
			Status:     StatusExperimental,
			ConfigKind: "external.webhook",
			Features:   []string{"http", "timeout", "custom_headers"},
			Smoke:      "go test ./internal/platform/adapters/external",
			Notes:      []string{"vendor-specific schemas live in project adapters"},
		},
		{
			Area:       "external_api",
			Backend:    "hmac_webhook",
			Status:     StatusExperimental,
			ConfigKind: "external.webhook.hmac",
			Features:   []string{"http", "timeout", "custom_headers", "hmac_sha256"},
			Smoke:      "go test ./internal/platform/adapters/external",
			Notes:      []string{"critical events must use EventLog or Outbox before calling external APIs"},
		},
		{
			Area:       "cache",
			Backend:    "memory",
			Status:     StatusStable,
			ConfigKind: "memory",
			Features:   []string{"get", "set", "delete", "ttl"},
			Smoke:      "go test ./internal/platform/cache",
		},
		{
			Area:       "cache",
			Backend:    "redis",
			Status:     StatusStable,
			ConfigKind: "redis",
			Features:   []string{"get", "set", "delete", "ttl", "pool"},
			Smoke:      "LONGHENG_REDIS_INTEGRATION=1 go test ./internal/platform/cache",
		},
		{
			Area:       "lock",
			Backend:    "memory",
			Status:     StatusStable,
			ConfigKind: "memory",
			Features:   []string{"token", "ttl", "safe_release"},
			Smoke:      "go test ./internal/platform/lock",
		},
		{
			Area:       "lock",
			Backend:    "redis",
			Status:     StatusStable,
			ConfigKind: "redis",
			Features:   []string{"set_nx_px", "lua_safe_release", "owner_token"},
			Smoke:      "LONGHENG_REDIS_INTEGRATION=1 go test ./internal/platform/lock",
		},
		{
			Area:       "orchestrator",
			Backend:    "kubernetes",
			Status:     StatusExperimental,
			ConfigKind: "k8s",
			Features:   []string{"list_pods", "watch_pods", "deployment_scale"},
			Smoke:      "go test ./internal/platform/adapters/k8s",
			Notes:      []string{"standard-library HTTP adapter; no client-go dependency in core"},
		},
		{
			Area:       "transport",
			Backend:    "tcp",
			Status:     StatusStable,
			ConfigKind: "gateway.tcp_listen",
			Features:   []string{"packet", "frame_v1", "session"},
			Smoke:      "go run ./cmd/gwbot -transport tcp",
		},
		{
			Area:       "transport",
			Backend:    "websocket",
			Status:     StatusStable,
			ConfigKind: "gateway.websocket_enabled",
			Features:   []string{"frame_v1", "browser_client", "session"},
			Smoke:      "go run ./cmd/gwbot -transport websocket",
		},
		{
			Area:       "transport",
			Backend:    "kcp",
			Status:     StatusStable,
			ConfigKind: "gateway.kcp_enabled",
			Features:   []string{"frame_v1", "weaknet", "fec_optional"},
			Smoke:      "python3 scripts/run_gateway_weaknet.py",
		},
		{
			Area:       "transport",
			Backend:    "quic",
			Status:     StatusStable,
			ConfigKind: "gateway.quic_enabled",
			Features:   []string{"frame_v1", "tls", "weaknet"},
			Smoke:      "python3 scripts/run_gateway_weaknet.py",
		},
	}
	for i := range capabilities {
		capabilities[i].Features = cleanSorted(capabilities[i].Features)
		capabilities[i].Notes = cleanSorted(capabilities[i].Notes)
	}
	sort.Slice(capabilities, func(i, j int) bool {
		if capabilities[i].Area == capabilities[j].Area {
			return capabilities[i].Backend < capabilities[j].Backend
		}
		return capabilities[i].Area < capabilities[j].Area
	})
	return capabilities
}

func validSelectedBackend(available map[string]map[string]Status, area, backend string) []string {
	status, ok := available[area][backend]
	if !ok {
		return []string{fmt.Sprintf("%s/%s unsupported", area, backend)}
	}
	switch status {
	case StatusPlanned:
		return []string{fmt.Sprintf("%s/%s planned only", area, backend)}
	case StatusSkeleton, StatusInterfaceOnly:
		return []string{fmt.Sprintf("%s/%s %s only", area, backend, status)}
	}
	return nil
}

func selectedGaps(selected Selected, capabilities []Capability) []Gap {
	var gaps []Gap
	statusByAreaBackend := make(map[string]Status, len(capabilities))
	for _, capability := range capabilities {
		statusByAreaBackend[capability.Area+"/"+capability.Backend] = capability.Status
	}
	for _, item := range []struct {
		area    string
		backend string
	}{
		{area: "registry", backend: selected.Registry},
		{area: "config", backend: selected.Config},
		{area: "eventbus", backend: selected.EventBus},
		{area: "cache", backend: selected.Cache},
		{area: "lock", backend: selected.Lock},
	} {
		gaps = appendSelectedGap(gaps, statusByAreaBackend, item.area, normalizeKind(item.backend, "memory"))
	}
	transports := selected.Transports
	if len(transports) == 0 {
		transports = []string{"tcp"}
	}
	for _, transport := range transports {
		gaps = appendSelectedGap(gaps, statusByAreaBackend, "transport", normalizeKind(transport, "tcp"))
	}
	return gaps
}

func appendSelectedGap(gaps []Gap, statusByAreaBackend map[string]Status, area, backend string) []Gap {
	key := area + "/" + backend
	status, ok := statusByAreaBackend[key]
	if !ok {
		return append(gaps, Gap{Area: area, Backend: backend, Reason: "unsupported backend"})
	}
	switch status {
	case StatusPlanned:
		return append(gaps, Gap{Area: area, Backend: backend, Reason: "planned backend not wired"})
	case StatusSkeleton, StatusInterfaceOnly:
		return append(gaps, Gap{Area: area, Backend: backend, Reason: string(status) + " backend not wired"})
	}
	return gaps
}

func selectedTransports(gateway config.GatewaySection) []string {
	transports := []string{"tcp"}
	if gateway.WebSocketEnabled {
		transports = append(transports, "websocket")
	}
	if gateway.KCPEnabled {
		transports = append(transports, "kcp")
	}
	if gateway.QUICEnabled {
		transports = append(transports, "quic")
	}
	return cleanSorted(transports)
}

func normalizeKind(kind, fallback string) string {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "" {
		return fallback
	}
	return kind
}

func cleanSorted(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
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
	sort.Strings(out)
	return out
}
