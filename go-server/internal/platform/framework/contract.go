package framework

import (
	"fmt"
	"sort"
	"strings"
)

// Role 表示框架边界中的服务角色。
type Role string

const (
	FrontendRole Role = "frontend"
	BackendRole  Role = "backend"
	MeshRole     Role = "mesh"
)

// Contract 是可由工具校验的实时服务边界规范。
type Contract struct {
	SchemaVersion  int             `json:"schema_version"`
	Frontend       Boundary        `json:"frontend"`
	Backends       []Boundary      `json:"backends"`
	RemoteServices []RemoteService `json:"remote_services"`
	GroupServices  []GroupService  `json:"group_services"`
	Session        SessionContract `json:"session"`
	Invariants     []Invariant     `json:"invariants"`
}

// Boundary 描述单个服务边界、职责，以及它提供或消费的规范。
type Boundary struct {
	Service          string   `json:"service"`
	Role             Role     `json:"role"`
	Responsibilities []string `json:"responsibilities"`
	Provides         []string `json:"provides"`
	Consumes         []string `json:"consumes"`
}

// RemoteService 描述跨服务规范及其背后的传输语义。
type RemoteService struct {
	Name       string   `json:"name"`
	Transport  string   `json:"transport"`
	Provider   string   `json:"provider"`
	Consumers  []string `json:"consumers"`
	Guarantees []string `json:"guarantees"`
}

// GroupService 描述多个后端服务共同使用的运行时分组规范。
type GroupService struct {
	Name      string   `json:"name"`
	Runtime   string   `json:"runtime"`
	Backends  []string `json:"backends"`
	Semantics []string `json:"semantics"`
}

// SessionContract 定义前端会话状态的归属、定位和重放规则。
type SessionContract struct {
	Owner        string   `json:"owner"`
	Binding      string   `json:"binding"`
	Locator      string   `json:"locator"`
	Events       []string `json:"events"`
	ClosePolicy  string   `json:"close_policy"`
	ReplayPolicy string   `json:"replay_policy"`
}

// Invariant 记录服务和运行时演进中必须保持成立的规则。
type Invariant struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// DefaultRealtimeContract 返回 gateway/logic/playerd/scene/social 的默认实时边界规范。
func DefaultRealtimeContract() Contract {
	contract := Contract{
		SchemaVersion: 1,
		Frontend: Boundary{
			Service: "gateway",
			Role:    FrontendRole,
			Responsibilities: []string{
				"transport_accept",
				"login_handshake",
				"session_binding",
				"backend_route_forward",
				"server_push",
			},
			Provides: []string{
				"frontend_session",
				"gateway_push",
				"transport_snapshot",
				"presence_publish",
			},
			Consumes: []string{
				"logic_route_manifest",
				"logic_player_response",
				"registry_backend_nodes",
			},
		},
		Backends: []Boundary{
			{
				Service: "logic",
				Role:    BackendRole,
				Responsibilities: []string{
					"stateful_player_handlers",
					"idempotent_request_guard",
					"profile_runtime",
					"session_event_projection",
				},
				Provides: []string{
					"route_manifest",
					"logic_player_response",
					"remote_rpc_handlers",
				},
				Consumes: []string{
					"gateway_client_packet",
					"session_connected",
					"session_disconnected",
				},
			},
			{
				Service: "playerd",
				Role:    BackendRole,
				Responsibilities: []string{
					"profile_flush",
					"player_loop_worker",
					"storage_deadletter_requeue",
				},
				Provides: []string{"player_storage_admin"},
				Consumes: []string{"profile_dirty_event"},
			},
			{
				Service:          "scene",
				Role:             MeshRole,
				Responsibilities: []string{"room_allocation", "scene_event_projection"},
				Provides:         []string{"room_allocated"},
				Consumes:         []string{"room_allocate_request"},
			},
			{
				Service:          "social",
				Role:             MeshRole,
				Responsibilities: []string{"chat_broadcast", "social_event_projection"},
				Provides:         []string{"chat_broadcast"},
				Consumes:         []string{"chat_send_request"},
			},
		},
		RemoteServices: []RemoteService{
			{
				Name:      "reliable_rpc",
				Transport: "event_bus",
				Provider:  "internal/platform/rpc.Endpoint",
				Consumers: []string{
					"gateway",
					"logic",
					"scene",
					"social",
					"playerd",
				},
				Guarantees: []string{
					"request_id_correlation",
					"timeout_bounded_pending",
					"discovery_resolved_target",
					"trace_span_capture",
				},
			},
		},
		GroupServices: []GroupService{
			{
				Name:     "runtime_group",
				Runtime:  "internal/platform/group.Runtime",
				Backends: []string{"memory", "redis"},
				Semantics: []string{
					"create",
					"ttl_renew",
					"membership",
					"broadcast",
					"multicast",
				},
			},
		},
		Session: SessionContract{
			Owner:        "gateway",
			Binding:      "one_active_session_per_account",
			Locator:      "presence_tracker + registry node metadata",
			Events:       []string{"OnBind", "AfterBind", "OnClose", "OnKick", "OnReconnect"},
			ClosePolicy:  "new same-account bind closes stale frontend session",
			ReplayPolicy: "idempotent backend response cache replays duplicate stateful packets",
		},
		Invariants: []Invariant{
			{Name: "frontend owns connection state", Description: "Only gateway owns sockets and session connection objects."},
			{Name: "backend owns gameplay state", Description: "Logic/playerd/scene/social own state mutations and never write client sockets directly."},
			{Name: "push crosses explicit contract", Description: "Backend push must use GatewayPush or RPC/event bus contracts."},
			{Name: "stateful routes are idempotent", Description: "Stateful authorized player routes must declare route policy and use idempotency guard."},
		},
	}
	return Normalize(contract)
}

func Normalize(contract Contract) Contract {
	normalizeBoundary(&contract.Frontend)
	for i := range contract.Backends {
		normalizeBoundary(&contract.Backends[i])
	}
	sort.Slice(contract.Backends, func(i, j int) bool {
		if contract.Backends[i].Role == contract.Backends[j].Role {
			return contract.Backends[i].Service < contract.Backends[j].Service
		}
		return contract.Backends[i].Role < contract.Backends[j].Role
	})
	for i := range contract.RemoteServices {
		contract.RemoteServices[i].Name = strings.TrimSpace(contract.RemoteServices[i].Name)
		contract.RemoteServices[i].Transport = strings.TrimSpace(contract.RemoteServices[i].Transport)
		contract.RemoteServices[i].Provider = strings.TrimSpace(contract.RemoteServices[i].Provider)
		contract.RemoteServices[i].Consumers = cleanSorted(contract.RemoteServices[i].Consumers)
		contract.RemoteServices[i].Guarantees = cleanSorted(contract.RemoteServices[i].Guarantees)
	}
	sort.Slice(contract.RemoteServices, func(i, j int) bool {
		return contract.RemoteServices[i].Name < contract.RemoteServices[j].Name
	})
	for i := range contract.GroupServices {
		contract.GroupServices[i].Name = strings.TrimSpace(contract.GroupServices[i].Name)
		contract.GroupServices[i].Runtime = strings.TrimSpace(contract.GroupServices[i].Runtime)
		contract.GroupServices[i].Backends = cleanSorted(contract.GroupServices[i].Backends)
		contract.GroupServices[i].Semantics = cleanSorted(contract.GroupServices[i].Semantics)
	}
	sort.Slice(contract.GroupServices, func(i, j int) bool {
		return contract.GroupServices[i].Name < contract.GroupServices[j].Name
	})
	contract.Session.Events = cleanSorted(contract.Session.Events)
	return contract
}

func Validate(contract Contract) error {
	contract = Normalize(contract)
	var missing []string
	if contract.SchemaVersion <= 0 {
		missing = append(missing, "schema_version")
	}
	if err := validateBoundary("frontend", contract.Frontend, FrontendRole); err != nil {
		missing = append(missing, err.Error())
	}
	services := map[string]struct{}{}
	if contract.Frontend.Service != "" {
		services[contract.Frontend.Service] = struct{}{}
	}
	if len(contract.Backends) == 0 {
		missing = append(missing, "backends")
	}
	for _, backend := range contract.Backends {
		if _, ok := services[backend.Service]; ok {
			missing = append(missing, fmt.Sprintf("duplicate service %s", backend.Service))
		}
		if backend.Service != "" {
			services[backend.Service] = struct{}{}
		}
		if backend.Role != BackendRole && backend.Role != MeshRole {
			missing = append(missing, fmt.Sprintf("backend %s role must be backend or mesh", backend.Service))
		}
		if err := validateBoundary("backend", backend, ""); err != nil {
			missing = append(missing, err.Error())
		}
	}
	if len(contract.RemoteServices) == 0 {
		missing = append(missing, "remote_services")
	}
	for _, remote := range contract.RemoteServices {
		if remote.Name == "" || remote.Transport == "" || remote.Provider == "" {
			missing = append(missing, "remote_service name/transport/provider")
		}
		for _, consumer := range remote.Consumers {
			if _, ok := services[consumer]; !ok {
				missing = append(missing, fmt.Sprintf("remote_service %s unknown consumer %s", remote.Name, consumer))
			}
		}
	}
	if len(contract.GroupServices) == 0 {
		missing = append(missing, "group_services")
	}
	for _, group := range contract.GroupServices {
		if group.Name == "" || group.Runtime == "" || len(group.Backends) == 0 {
			missing = append(missing, "group_service name/runtime/backends")
		}
	}
	if contract.Session.Owner == "" || contract.Session.Binding == "" || contract.Session.Locator == "" {
		missing = append(missing, "session owner/binding/locator")
	}
	if len(missing) > 0 {
		return fmt.Errorf("framework contract incomplete: %s", strings.Join(missing, "; "))
	}
	return nil
}

func validateBoundary(label string, boundary Boundary, role Role) error {
	if boundary.Service == "" {
		return fmt.Errorf("%s service", label)
	}
	if role != "" && boundary.Role != role {
		return fmt.Errorf("%s %s role must be %s", label, boundary.Service, role)
	}
	if boundary.Role == "" {
		return fmt.Errorf("%s %s role", label, boundary.Service)
	}
	if len(boundary.Responsibilities) == 0 {
		return fmt.Errorf("%s %s responsibilities", label, boundary.Service)
	}
	if len(boundary.Provides) == 0 {
		return fmt.Errorf("%s %s provides", label, boundary.Service)
	}
	return nil
}

func normalizeBoundary(boundary *Boundary) {
	boundary.Service = strings.TrimSpace(boundary.Service)
	boundary.Role = Role(strings.TrimSpace(string(boundary.Role)))
	boundary.Responsibilities = cleanSorted(boundary.Responsibilities)
	boundary.Provides = cleanSorted(boundary.Provides)
	boundary.Consumes = cleanSorted(boundary.Consumes)
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
