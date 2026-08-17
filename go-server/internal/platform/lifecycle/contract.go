package lifecycle

import (
	"fmt"
	"sort"
	"strings"
)

type Layer string

const (
	PlatformLayer Layer = "platform"
	ServiceLayer  Layer = "service"
	GameplayLayer Layer = "gameplay"
	OpsLayer      Layer = "ops"
)

type Contract struct {
	SchemaVersion  int         `json:"schema_version"`
	LifecycleOrder []string    `json:"lifecycle_order"`
	Components     []Component `json:"components"`
	Invariants     []Invariant `json:"invariants"`
}

type Component struct {
	Name             string   `json:"name"`
	Layer            Layer    `json:"layer"`
	Owns             []string `json:"owns"`
	Consumes         []string `json:"consumes,omitempty"`
	Publishes        []string `json:"publishes,omitempty"`
	Lifecycle        []string `json:"lifecycle"`
	MayDependOn      []string `json:"may_depend_on,omitempty"`
	ForbiddenImports []string `json:"forbidden_imports,omitempty"`
	ForbiddenOwns    []string `json:"forbidden_owns,omitempty"`
}

type Invariant struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func DefaultContract() Contract {
	contract := Contract{
		SchemaVersion:  1,
		LifecycleOrder: []string{"configure", "preflight", "init", "after_init", "start", "after_start", "drain", "before_shutdown", "stop", "shutdown"},
		Components: []Component{
			{
				Name:      "acceptor_transport",
				Layer:     PlatformLayer,
				Owns:      []string{"tcp_listener", "websocket_listener", "kcp_listener", "quic_listener", "frame_codec", "connection_lifecycle"},
				Consumes:  []string{"gateway_config", "session_contract"},
				Publishes: []string{"session_connected", "gateway_client_packet", "session_disconnected"},
				Lifecycle: []string{"preflight", "init", "start", "drain", "before_shutdown", "stop", "shutdown"},
				MayDependOn: []string{
					"internal/platform/config",
					"internal/platform/metrics",
					"internal/platform/session",
					"pkg/protocol",
				},
				ForbiddenImports: []string{"internal/modules", "internal/services/logic"},
				ForbiddenOwns:    []string{"profile_state", "gameplay_state"},
			},
			{
				Name:      "handler_dispatch",
				Layer:     PlatformLayer,
				Owns:      []string{"route_policy", "auth_gate", "request_pipeline", "handler_metadata"},
				Consumes:  []string{"gateway_client_packet"},
				Publishes: []string{"handler_response"},
				Lifecycle: []string{"configure", "init", "after_init", "snapshot", "dispatch", "before_shutdown", "shutdown"},
				MayDependOn: []string{
					"internal/platform/pipeline",
					"pkg/protocol",
				},
				ForbiddenImports: []string{"internal/modules"},
				ForbiddenOwns:    []string{"profile_state", "socket_write"},
			},
			{
				Name:      "remote_rpc",
				Layer:     PlatformLayer,
				Owns:      []string{"call_timeout", "pending_limit", "discovery_resolution", "trace_correlation"},
				Consumes:  []string{"event_bus", "registry_snapshot"},
				Publishes: []string{"rpc_request", "rpc_response"},
				Lifecycle: []string{"configure", "init", "start", "call", "before_shutdown", "stop", "shutdown"},
				MayDependOn: []string{
					"internal/platform/bus",
					"internal/platform/discovery",
					"internal/platform/registry",
				},
				ForbiddenImports: []string{"internal/modules"},
				ForbiddenOwns:    []string{"gameplay_transaction"},
			},
			{
				Name:      "module_manager",
				Layer:     PlatformLayer,
				Owns:      []string{"preflight_order", "start_order", "after_start", "before_stop", "reverse_stop"},
				Consumes:  []string{"component_manifest"},
				Publishes: []string{"health_component_state"},
				Lifecycle: []string{"configure", "preflight", "init", "after_init", "start", "after_start", "drain", "before_shutdown", "stop", "shutdown"},
				MayDependOn: []string{
					"internal/platform/health",
				},
				ForbiddenImports: []string{"internal/modules"},
				ForbiddenOwns:    []string{"gameplay_registration"},
			},
			{
				Name:      "actor_runtime",
				Layer:     PlatformLayer,
				Owns:      []string{"pid", "mailbox", "user_binding", "serial_execution", "actor_snapshot"},
				Consumes:  []string{"actor_message"},
				Publishes: []string{"actor_failure", "actor_snapshot"},
				Lifecycle: []string{"init", "spawn", "tell", "call", "invoke", "drain", "before_shutdown", "stop", "shutdown"},
				MayDependOn: []string{
					"internal/platform/metrics",
				},
				ForbiddenImports: []string{"internal/modules"},
				ForbiddenOwns:    []string{"socket_write", "database_transaction"},
			},
			{
				Name:      "service_composition",
				Layer:     ServiceLayer,
				Owns:      []string{"env_wiring", "component_registration", "admin_route_registration"},
				Consumes:  []string{"config", "platform_runtime"},
				Publishes: []string{"service_health", "admin_snapshot"},
				Lifecycle: []string{"configure", "preflight", "init", "after_init", "start", "drain", "before_shutdown", "stop", "shutdown"},
				MayDependOn: []string{
					"internal/platform",
					"internal/modules",
					"pkg/contracts",
				},
				ForbiddenOwns: []string{"platform_primitive_semantics"},
			},
			{
				Name:      "gameplay_module",
				Layer:     GameplayLayer,
				Owns:      []string{"domain_rules", "profile_mutation", "matchmaking_policy", "leaderboard_score", "party_rule"},
				Consumes:  []string{"dispatch_request", "profile_store", "runtime_actor"},
				Publishes: []string{"domain_event", "gateway_push_request"},
				Lifecycle: []string{"configure", "register_handlers", "preflight", "init", "after_init", "start", "before_shutdown", "stop", "shutdown"},
				MayDependOn: []string{
					"internal/platform/dispatch",
					"internal/platform/profile",
					"internal/platform/actor",
					"pkg/contracts",
				},
				ForbiddenImports: []string{"internal/services/gateway"},
				ForbiddenOwns:    []string{"client_socket", "transport_listener"},
			},
			{
				Name:      "admin_ops",
				Layer:     OpsLayer,
				Owns:      []string{"openapi", "admin_snapshot", "audit_log", "readiness_report"},
				Consumes:  []string{"platform_snapshot", "service_snapshot"},
				Publishes: []string{"operator_action", "audit_record"},
				Lifecycle: []string{"configure", "init", "serve", "authorize", "snapshot", "audit", "before_shutdown", "shutdown"},
				MayDependOn: []string{
					"internal/platform/audit",
					"internal/platform/adminspec",
				},
				ForbiddenOwns: []string{"gameplay_mutation_without_audit"},
			},
		},
		Invariants: []Invariant{
			{Name: "platform is gameplay agnostic", Description: "internal/platform packages must not import internal/modules or concrete service wiring."},
			{Name: "transport is not gameplay", Description: "acceptors and transports own bytes and sessions, never domain state."},
			{Name: "handlers cross route policy", Description: "business handlers are entered only through dispatch metadata and route policy."},
			{Name: "remote calls are bounded", Description: "RPC and actor calls must be timeout or mailbox bounded."},
			{Name: "ops mutations are audited", Description: "operator-facing mutating endpoints must pass admin auth and leave audit evidence."},
		},
	}
	return Normalize(contract)
}

func Normalize(contract Contract) Contract {
	contract.LifecycleOrder = cleanSortedLifecycle(contract.LifecycleOrder)
	for i := range contract.Components {
		component := &contract.Components[i]
		component.Name = strings.TrimSpace(component.Name)
		component.Owns = cleanSorted(component.Owns)
		component.Consumes = cleanSorted(component.Consumes)
		component.Publishes = cleanSorted(component.Publishes)
		component.Lifecycle = cleanSorted(component.Lifecycle)
		component.MayDependOn = cleanSorted(component.MayDependOn)
		component.ForbiddenImports = cleanSorted(component.ForbiddenImports)
		component.ForbiddenOwns = cleanSorted(component.ForbiddenOwns)
	}
	sort.Slice(contract.Components, func(i, j int) bool {
		return contract.Components[i].Name < contract.Components[j].Name
	})
	return contract
}

func Validate(contract Contract) error {
	contract = Normalize(contract)
	var missing []string
	if contract.SchemaVersion <= 0 {
		missing = append(missing, "schema_version")
	}
	if len(contract.Components) == 0 {
		missing = append(missing, "components")
	}
	if !hasLifecycleStageSet(contract.LifecycleOrder, []string{"init", "after_init", "before_shutdown", "shutdown"}) {
		missing = append(missing, "lifecycle_order.init/after_init/before_shutdown/shutdown")
	}
	byName := make(map[string]Component, len(contract.Components))
	for _, component := range contract.Components {
		if component.Name == "" {
			missing = append(missing, "component.name")
		}
		if component.Layer == "" {
			missing = append(missing, component.Name+".layer")
		}
		if len(component.Owns) == 0 {
			missing = append(missing, component.Name+".owns")
		}
		if len(component.Lifecycle) == 0 {
			missing = append(missing, component.Name+".lifecycle")
		}
		if isRuntimeComponent(component.Name) && !hasLifecycleStageSet(component.Lifecycle, []string{"init", "before_shutdown", "shutdown"}) {
			missing = append(missing, component.Name+".lifecycle.init/before_shutdown/shutdown")
		}
		byName[component.Name] = component
	}
	for _, name := range []string{"acceptor_transport", "handler_dispatch", "remote_rpc", "module_manager", "actor_runtime", "gameplay_module"} {
		if _, ok := byName[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(contract.Invariants) == 0 {
		missing = append(missing, "invariants")
	}
	if len(missing) > 0 {
		return fmt.Errorf("lifecycle contract incomplete: %s", strings.Join(missing, "; "))
	}
	return nil
}

func isRuntimeComponent(name string) bool {
	switch name {
	case "acceptor_transport", "handler_dispatch", "remote_rpc", "module_manager", "actor_runtime", "service_composition", "gameplay_module", "admin_ops":
		return true
	default:
		return false
	}
}

func hasLifecycleStageSet(stages []string, required []string) bool {
	seen := make(map[string]struct{}, len(stages))
	for _, stage := range stages {
		seen[stage] = struct{}{}
	}
	for _, stage := range required {
		if _, ok := seen[stage]; !ok {
			return false
		}
	}
	return true
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

func cleanSortedLifecycle(values []string) []string {
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
	return out
}
