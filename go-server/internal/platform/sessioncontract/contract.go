package sessioncontract

import (
	"fmt"
	"sort"
	"strings"

	"longheng.io/server/internal/platform/dispatch"
)

type Contract struct {
	SchemaVersion int                 `json:"schema_version"`
	Frontend      FrontendSession     `json:"frontend"`
	Backend       BackendSession      `json:"backend"`
	Transitions   []Transition        `json:"transitions"`
	RoutePolicies []RoutePolicy       `json:"route_policies"`
	Invariants    []Invariant         `json:"invariants"`
	Forbidden     []ForbiddenCoupling `json:"forbidden"`
}

type FrontendSession struct {
	Owner       string   `json:"owner"`
	Identity    []string `json:"identity"`
	State       []string `json:"state"`
	Operations  []string `json:"operations"`
	Publishes   []string `json:"publishes"`
	MayRead     []string `json:"may_read"`
	MustNotOwn  []string `json:"must_not_own"`
	Storage     string   `json:"storage"`
	ClosePolicy string   `json:"close_policy"`
}

type BackendSession struct {
	Owner          string   `json:"owner"`
	Projection     []string `json:"projection"`
	State          []string `json:"state"`
	Operations     []string `json:"operations"`
	Consumes       []string `json:"consumes"`
	Publishes      []string `json:"publishes"`
	MayRead        []string `json:"may_read"`
	MustNotOwn     []string `json:"must_not_own"`
	Storage        string   `json:"storage"`
	Idempotency    string   `json:"idempotency"`
	ReplayContract string   `json:"replay_contract"`
}

type Transition struct {
	Name         string   `json:"name"`
	From         string   `json:"from"`
	To           string   `json:"to"`
	Actor        string   `json:"actor"`
	RequiredData []string `json:"required_data,omitempty"`
	Events       []string `json:"events,omitempty"`
	Guarantees   []string `json:"guarantees,omitempty"`
}

type RoutePolicy struct {
	Name             string `json:"name"`
	Internal         bool   `json:"internal,omitempty"`
	Stateful         bool   `json:"stateful,omitempty"`
	Authorized       bool   `json:"authorized,omitempty"`
	Idempotent       bool   `json:"idempotent,omitempty"`
	FrontendCallable bool   `json:"frontend_callable,omitempty"`
	BackendOnly      bool   `json:"backend_only,omitempty"`
}

type Invariant struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type ForbiddenCoupling struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Reason string `json:"reason"`
}

func Default() Contract {
	contract := Contract{
		SchemaVersion: 1,
		Frontend: FrontendSession{
			Owner: "gateway",
			Identity: []string{
				"session_id",
				"account_id",
				"gateway_node_id",
				"transport",
				"remote_addr",
			},
			State: []string{
				"accepting",
				"handshaking",
				"bound",
				"draining",
				"closed",
			},
			Operations: []string{
				"accept_transport",
				"login_handshake",
				"bind_account_session",
				"touch_last_seen",
				"write_client_packet",
				"close_socket",
			},
			Publishes: []string{
				"session_connected",
				"session_bound",
				"session_disconnected",
				"gateway_push_result",
			},
			MayRead: []string{
				"route_manifest",
				"presence_locator",
				"backend_registry",
			},
			MustNotOwn: []string{
				"profile_state",
				"player_actor_state",
				"business_idempotency_commit",
			},
			Storage:     "gateway memory session store + optional Redis presence locator",
			ClosePolicy: "same account new bind closes stale frontend session and publishes disconnect",
		},
		Backend: BackendSession{
			Owner: "logic/playerd/scene/social",
			Projection: []string{
				"account_id",
				"frontend_session_id",
				"gateway_node_id",
				"player_actor_pid",
				"profile_revision",
			},
			State: []string{
				"projected",
				string(PlayerStateLoading),
				string(PlayerStateOnline),
				string(PlayerStateValid),
				string(PlayerStateReconnecting),
				string(PlayerStateClosing),
				string(PlayerStateKicked),
				string(PlayerStateInvalid),
				"stateful_route_running",
				"dirty",
				"flushing",
				"detached",
			},
			Operations: []string{
				"authorize_route",
				"load_profile",
				"mutate_game_state",
				"commit_idempotency",
				"cache_response",
				"emit_gateway_push",
			},
			Consumes: []string{
				"gateway_client_packet",
				"session_connected",
				"session_disconnected",
				"presence_update",
			},
			Publishes: []string{
				"logic_player_response",
				"profile_dirty_event",
				"gateway_push_request",
				"auth_state_notify",
			},
			MayRead: []string{
				"frontend_presence_projection",
				"profile_store",
				"idempotency_store",
				"route_manifest",
			},
			MustNotOwn: []string{
				"client_socket",
				"transport_acceptor",
				"frontend_session_close_without_gateway",
			},
			Storage:        "profile store + idempotency store + actor mailbox; never frontend socket storage",
			Idempotency:    "stateful authorized routes must reserve, execute, commit, and replay cached response",
			ReplayContract: "duplicate stateful request replays cached response without rerunning handler",
		},
		Transitions: []Transition{
			{
				Name:         "login_bind",
				From:         "frontend.handshaking",
				To:           "frontend.bound",
				Actor:        "gateway",
				RequiredData: []string{"session_id", "account_id", "gateway_node_id"},
				Events:       []string{"session_bound"},
				Guarantees:   []string{"one_active_frontend_session_per_account"},
			},
			{
				Name:         "packet_forward",
				From:         "frontend.bound",
				To:           "backend.stateful_route_running",
				Actor:        "gateway -> logic",
				RequiredData: []string{"session_id", "account_id", "msg_id", "request_id"},
				Events:       []string{"gateway_client_packet"},
				Guarantees:   []string{"route_policy_checked_before_handler"},
			},
			{
				Name:         "backend_response",
				From:         "backend.stateful_route_running",
				To:           "frontend.bound",
				Actor:        "logic -> gateway",
				RequiredData: []string{"session_id", "msg_id", "response_body"},
				Events:       []string{"logic_player_response"},
				Guarantees:   []string{"duplicate_request_replays_cached_response"},
			},
			{
				Name:         "disconnect",
				From:         "frontend.bound",
				To:           "backend.reconnecting",
				Actor:        "gateway -> logic",
				RequiredData: []string{"session_id", "account_id"},
				Events:       []string{"session_disconnected"},
				Guarantees:   []string{"backend_projection_can_detach_without_socket_access", "profile_kept_until_idle_reclaim"},
			},
			{
				Name:         "backend_load_profile",
				From:         "backend.projected",
				To:           "backend.loading",
				Actor:        "logic/playerd",
				RequiredData: []string{"account_id", "session_id"},
				Events:       []string{"session_connected"},
				Guarantees:   []string{"handler_not_executed_before_profile_loaded"},
			},
			{
				Name:         "backend_route_valid",
				From:         "backend.online",
				To:           "backend.valid",
				Actor:        "logic/playerd",
				RequiredData: []string{"account_id", "session_id", "msg_id"},
				Events:       []string{"stateful_handler_success"},
				Guarantees:   []string{"stateful_route_has_current_session_projection"},
			},
			{
				Name:         "backend_reclaim_flush",
				From:         "backend.reconnecting",
				To:           "backend.closing",
				Actor:        "logic/playerd",
				RequiredData: []string{"account_id"},
				Events:       []string{"idle_reclaim"},
				Guarantees:   []string{"new_packets_rejected_before_profile_flush", "actor_mailbox_drained_before_unload"},
			},
			{
				Name:         "backend_flush_done",
				From:         "backend.closing",
				To:           "backend.invalid",
				Actor:        "logic/playerd",
				RequiredData: []string{"account_id"},
				Events:       []string{"profile_flushed"},
				Guarantees:   []string{"profile_unloaded_after_successful_flush"},
			},
		},
		RoutePolicies: []RoutePolicy{
			{Name: "internal", Internal: true, BackendOnly: true},
			{Name: "authorized", Authorized: true, FrontendCallable: true},
			{Name: "stateful", Stateful: true, Authorized: true, Idempotent: true, FrontendCallable: true},
			{Name: "public_read", FrontendCallable: true},
		},
		Invariants: []Invariant{
			{Name: "frontend owns sockets", Description: "Only gateway may accept, write, close, or retain client sockets."},
			{Name: "backend owns state mutation", Description: "Logic and gameplay modules own profile and business state mutations through route policy."},
			{Name: "session projection is explicit", Description: "Backend session state is a projection of gateway events and must be rebuildable."},
			{Name: "stateful routes replay", Description: "Stateful authorized requests must not be dropped on retry; they must execute once or replay the cached response."},
			{Name: "disconnect keeps reclaim window", Description: "Backend disconnect moves player runtime to reconnecting; idle reclaim must flush and unload through closing before invalid."},
		},
		Forbidden: []ForbiddenCoupling{
			{Source: "internal/platform", Target: "internal/modules", Reason: "platform layer must remain gameplay agnostic"},
			{Source: "internal/platform", Target: "internal/services/gateway", Reason: "platform abstractions must not depend on service wiring"},
			{Source: "internal/modules", Target: "transport_acceptor", Reason: "gameplay modules must not own sockets or transport loops"},
			{Source: "backend", Target: "client_socket", Reason: "backend push must cross GatewayPush/RPC/event bus contracts"},
		},
	}
	return Normalize(contract)
}

func Normalize(contract Contract) Contract {
	contract.Frontend.Identity = cleanSorted(contract.Frontend.Identity)
	contract.Frontend.State = cleanSorted(contract.Frontend.State)
	contract.Frontend.Operations = cleanSorted(contract.Frontend.Operations)
	contract.Frontend.Publishes = cleanSorted(contract.Frontend.Publishes)
	contract.Frontend.MayRead = cleanSorted(contract.Frontend.MayRead)
	contract.Frontend.MustNotOwn = cleanSorted(contract.Frontend.MustNotOwn)
	contract.Backend.Projection = cleanSorted(contract.Backend.Projection)
	contract.Backend.State = cleanSorted(contract.Backend.State)
	contract.Backend.Operations = cleanSorted(contract.Backend.Operations)
	contract.Backend.Consumes = cleanSorted(contract.Backend.Consumes)
	contract.Backend.Publishes = cleanSorted(contract.Backend.Publishes)
	contract.Backend.MayRead = cleanSorted(contract.Backend.MayRead)
	contract.Backend.MustNotOwn = cleanSorted(contract.Backend.MustNotOwn)
	for i := range contract.Transitions {
		contract.Transitions[i].Name = strings.TrimSpace(contract.Transitions[i].Name)
		contract.Transitions[i].From = strings.TrimSpace(contract.Transitions[i].From)
		contract.Transitions[i].To = strings.TrimSpace(contract.Transitions[i].To)
		contract.Transitions[i].Actor = strings.TrimSpace(contract.Transitions[i].Actor)
		contract.Transitions[i].RequiredData = cleanSorted(contract.Transitions[i].RequiredData)
		contract.Transitions[i].Events = cleanSorted(contract.Transitions[i].Events)
		contract.Transitions[i].Guarantees = cleanSorted(contract.Transitions[i].Guarantees)
	}
	sort.Slice(contract.Transitions, func(i, j int) bool {
		return contract.Transitions[i].Name < contract.Transitions[j].Name
	})
	for i := range contract.RoutePolicies {
		contract.RoutePolicies[i].Name = strings.TrimSpace(contract.RoutePolicies[i].Name)
	}
	sort.Slice(contract.RoutePolicies, func(i, j int) bool {
		return contract.RoutePolicies[i].Name < contract.RoutePolicies[j].Name
	})
	return contract
}

func Validate(contract Contract) error {
	contract = Normalize(contract)
	var missing []string
	if contract.SchemaVersion <= 0 {
		missing = append(missing, "schema_version")
	}
	if contract.Frontend.Owner == "" {
		missing = append(missing, "frontend.owner")
	}
	if !contains(contract.Frontend.Operations, "write_client_packet") || !contains(contract.Frontend.MustNotOwn, "profile_state") {
		missing = append(missing, "frontend socket/state ownership split")
	}
	if contract.Backend.Owner == "" {
		missing = append(missing, "backend.owner")
	}
	if !contains(contract.Backend.Operations, "commit_idempotency") || !contains(contract.Backend.MustNotOwn, "client_socket") {
		missing = append(missing, "backend idempotency/socket ownership split")
	}
	if len(contract.Transitions) == 0 {
		missing = append(missing, "transitions")
	}
	if len(contract.RoutePolicies) == 0 {
		missing = append(missing, "route_policies")
	}
	if len(contract.Invariants) == 0 {
		missing = append(missing, "invariants")
	}
	if len(missing) > 0 {
		return fmt.Errorf("session contract incomplete: %s", strings.Join(missing, "; "))
	}
	return nil
}

func RoutePolicyFromHandler(meta dispatch.HandlerMeta) RoutePolicy {
	return RoutePolicy{
		Name:             strings.TrimSpace(meta.Name),
		Internal:         meta.Internal,
		Stateful:         meta.Stateful || meta.PlayerScoped,
		Authorized:       meta.Authorized || meta.AuthRequired,
		Idempotent:       meta.Idempotent,
		FrontendCallable: !meta.Internal,
		BackendOnly:      meta.Internal,
	}
}

func ValidateHandlerMeta(meta dispatch.HandlerMeta) error {
	policy := RoutePolicyFromHandler(meta)
	if policy.Stateful && !policy.Authorized {
		return fmt.Errorf("handler %s stateful routes must be authorized", policy.Name)
	}
	if policy.Stateful && !policy.Idempotent {
		return fmt.Errorf("handler %s stateful routes must be idempotent", policy.Name)
	}
	return nil
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

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
