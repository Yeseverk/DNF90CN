package readiness

import (
	"fmt"
	"sort"
	"strings"
)

type Plan struct {
	SchemaVersion int        `json:"schema_version"`
	Posture       string     `json:"posture"`
	Scenarios     []Scenario `json:"scenarios"`
	Modules       []Module   `json:"modules"`
	Framework     []Control  `json:"framework"`
	Operations    []Control  `json:"operations"`
	Security      []Control  `json:"security"`
	Ecosystem     []Control  `json:"ecosystem"`
	Evidence      []Evidence `json:"evidence"`
	ReleaseGates  []Gate     `json:"release_gates"`
	Gaps          []Gap      `json:"gaps,omitempty"`
}

type Scenario struct {
	Name       string   `json:"name"`
	Tier       string   `json:"tier"`
	Status     string   `json:"status"`
	Signals    []string `json:"signals"`
	Commands   []string `json:"commands,omitempty"`
	ExitGate   string   `json:"exit_gate"`
	Owner      string   `json:"owner"`
	NextAction string   `json:"next_action,omitempty"`
}

type Module struct {
	Name       string   `json:"name"`
	Depth      string   `json:"depth"`
	MustProve  []string `json:"must_prove"`
	NextAction string   `json:"next_action"`
}

type Control struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Boundary   string `json:"boundary"`
	NextAction string `json:"next_action"`
}

type Evidence struct {
	Name     string   `json:"name"`
	Status   string   `json:"status"`
	Reports  []string `json:"reports,omitempty"`
	Commands []string `json:"commands,omitempty"`
	Owner    string   `json:"owner"`
}

type Gate struct {
	Name     string   `json:"name"`
	Tier     string   `json:"tier"`
	Requires []string `json:"requires"`
	Decision string   `json:"decision"`
}

type Gap struct {
	Area   string `json:"area"`
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

func DefaultPlan() Plan {
	plan := Plan{
		SchemaVersion: 1,
		Posture:       "engineering-ready; production maturity requires evidence-backed soak, chaos, and operator drills",
		Scenarios: []Scenario{
			{
				Name:    "single-node functional acceptance",
				Tier:    "P0",
				Status:  "automated",
				Signals: []string{"go test ./...", "go vet ./...", "check -roadmap", "admin_snapshot"},
				Commands: []string{
					"python scripts/release_gate.py --profile local",
					"go test -count=1 ./...",
					"go vet ./...",
					"go run ./cmd/check -roadmap",
					"python scripts/admin_snapshot.py --strict --json-out reports/admin-snapshot.json --markdown-out reports/admin-snapshot.md",
				},
				ExitGate: "all commands pass on every release candidate",
				Owner:    "platform",
			},
			{
				Name:    "multi-node gateway logic replay",
				Tier:    "P0",
				Status:  "scripted",
				Signals: []string{"same-account session replacement", "idempotent response replay", "gateway backend failover"},
				Commands: []string{
					"python scripts/sdk_gateway_smoke.py",
					"python scripts/redis_mysql_failover_smoke.py",
				},
				ExitGate:   "no duplicated handler execution and no dropped idempotent response",
				Owner:      "platform",
				NextAction: "run nightly against Redis/MySQL and at least two gateway nodes",
			},
			{
				Name:    "weak network TCP/WebSocket/KCP/QUIC",
				Tier:    "P0",
				Status:  "evidence-backed",
				Signals: []string{"TCP latency", "WebSocket latency", "KCP latency", "QUIC latency", "packet loss", "reconnect behavior"},
				Commands: []string{
					"python scripts/run_gateway_weaknet.py",
				},
				ExitGate: "summary.md includes TCP, WebSocket, KCP, and QUIC data under loss/jitter profiles",
				Owner:    "network",
			},
			{
				Name:    "long soak and leak detection",
				Tier:    "P1",
				Status:  "scripted",
				Signals: []string{"goroutine count", "heap", "session count", "pending rpc", "player loop lag"},
				Commands: []string{
					"python scripts/run_soak.py --duration 6h --interval 30s --load-command \"go run ./cmd/gwbot -duration 6h -clients 5000\"",
					"python scripts/check_rollout_report.py reports/soak-*/summary.json --kind soak --min-samples 10",
				},
				ExitGate:   "no unbounded goroutine, heap, pending rpc, or session growth",
				Owner:      "performance",
				NextAction: "run nightly against staging and attach reports/soak-*/summary.json to release candidates",
			},
			{
				Name:       "chaos and rolling upgrade",
				Tier:       "P1",
				Status:     "scripted",
				Signals:    []string{"gateway kill", "logic kill", "Redis restart", "MySQL restart", "registry lease expiration", "config reload"},
				Commands:   []string{"python scripts/run_chaos_drill.py --action restart-logic=\"systemctl restart game-logic\"", "python scripts/check_rollout_report.py reports/chaos-*/summary.json --kind chaos"},
				ExitGate:   "clients reconnect, stateful requests replay, and admin readiness returns to ok",
				Owner:      "sre",
				NextAction: "add Redis/MySQL and rolling gateway actions to the staging drill matrix",
			},
			{
				Name:    "operator audit drill",
				Tier:    "P1",
				Status:  "scripted",
				Signals: []string{"admin token", "audit log", "OpenAPI", "snapshot diff"},
				Commands: []string{
					"python scripts/admin_snapshot.py --strict",
				},
				ExitGate:   "every mutating admin endpoint requires auth and emits audit evidence",
				Owner:      "ops",
				NextAction: "add UI workflow for drain, kick, deadletter requeue, and rollback",
			},
		},
		Modules: []Module{
			{Name: "matchmaker", Depth: "foundation", MustProve: []string{"queue fairness", "timeout", "cancel", "multi-node ownership"}, NextAction: "add multi-node fairness and timeout report"},
			{Name: "party", Depth: "foundation", MustProve: []string{"leader transfer", "invite expiry", "disconnect recovery"}, NextAction: "add reconnect and leader migration smoke"},
			{Name: "leaderboard", Depth: "usable sample", MustProve: []string{"season rollover", "tie break", "rank query scale"}, NextAction: "add Redis sorted-set scale smoke"},
			{Name: "tournament", Depth: "foundation", MustProve: []string{"bracket lifecycle", "reward idempotency", "late join policy"}, NextAction: "add lifecycle and reward replay tests"},
			{Name: "presence", Depth: "distributed path", MustProve: []string{"Lua atomic update", "stale index cleanup", "cross-gateway push"}, NextAction: "add stale-index pressure report"},
		},
		Framework: []Control{
			{Name: "Gate / Node / Mesh", Status: "implemented", Boundary: "topology roles + route-aware discovery", NextAction: "add multi-node chaos evidence for mesh route changes"},
			{Name: "Node route options", Status: "implemented", Boundary: "route_policy internal/stateful/authorized/idempotent", NextAction: "generate route matrix docs from handler schema"},
			{Name: "Actor model", Status: "foundation", Boundary: "internal/platform/actor mailbox and pid lifecycle", NextAction: "add supervision and remote actor placement smoke"},
			{Name: "Config center", Status: "foundation", Boundary: "config reload + typed ServiceConfig", NextAction: "add remote config backend adapter acceptance"},
			{Name: "Registry", Status: "implemented", Boundary: "memory/Redis/etcd registry adapters", NextAction: "run etcd lease expiry smoke in server acceptance"},
			{Name: "Event bus", Status: "implemented", Boundary: "memory/Redis/NATS bus adapters", NextAction: "run NATS reconnect smoke in server acceptance"},
			{Name: "Distributed lock", Status: "implemented", Boundary: "memory/Redis lock manager", NextAction: "run Redis failover and lock expiry smoke"},
			{Name: "Cache", Status: "implemented", Boundary: "memory/Redis cache store", NextAction: "run cache TTL and reconnect smoke"},
			{Name: "Network transport plugins", Status: "implemented", Boundary: "TCP/WebSocket/KCP/QUIC gateway transports", NextAction: "keep weaknet report in every release candidate"},
		},
		Operations: []Control{
			{Name: "admin console", Status: "minimal-ops", Boundary: "HTML admin + OpenAPI + player/profile/event/leaderboard/ban workflows", NextAction: "add visual topology, session, audit and readiness dashboards"},
			{Name: "admin snapshot", Status: "scripted", Boundary: "scripts/admin_snapshot.py JSON/Markdown reports", NextAction: "run in deployment acceptance and attach reports"},
			{Name: "audit trail", Status: "rbac-enabled", Boundary: "RBAC admin wrapper + audit logger + trace id + dangerous confirmation", NextAction: "require audit evidence for every mutating admin action in release gate reports"},
			{Name: "alerts", Status: "planned", Boundary: "metrics/OpenAPI snapshot data", NextAction: "add Prometheus alert rules for session growth, rpc pending, deadletters, idempotency errors"},
			{Name: "operator rollback", Status: "planned", Boundary: "deployment runbook", NextAction: "script drain, canary rollback, and config rollback drills"},
		},
		Security: []Control{
			{Name: "account identity", Status: "extension-boundary", Boundary: "gateway auth_mode external/disabled + project account adapter", NextAction: "integrate real platform auth/OIDC/device binding in project layer"},
			{Name: "payments", Status: "extension-boundary", Boundary: "external platform receipt adapter + idempotent DB/EventLog/Outbox grant", NextAction: "add project payment receipt smoke and audit report"},
			{Name: "admin auth", Status: "rbac-enabled", Boundary: "X-Admin-Token + role scopes + X-Admin-Confirm + audit trace", NextAction: "add mTLS/reverse proxy pattern and secret rotation runbook"},
			{Name: "abuse/risk", Status: "foundation", Boundary: "moderation + rate limit", NextAction: "add risk scoring extension points and sanction audit reports"},
		},
		Ecosystem: []Control{
			{Name: "CLI", Status: "basic", Boundary: "ctl + scripts", NextAction: "turn common admin actions into stable JSON CLI commands"},
			{Name: "SDK", Status: "multi-language foundation", Boundary: "Go/TypeScript/Python/Lua/C#/Unity", NextAction: "generate typed DTO clients from sdk contract for C# and Unity"},
			{Name: "public docs", Status: "engineering docs", Boundary: "docs/", NextAction: "add tutorial, runbook, and production case-study tracks"},
			{Name: "operator console", Status: "minimal-ops", Boundary: "admin HTML + OpenAPI + ops endpoints", NextAction: "build visual workflows for topology, sessions, audit, readiness and rollback"},
		},
		Evidence: []Evidence{
			{Name: "local full verification", Status: "automated", Reports: []string{"reports/release-gate-*/summary.json", "go test", "go vet", "check"}, Commands: []string{"python scripts/release_gate.py --profile local", "go test -count=1 ./...", "go vet ./...", "go run ./cmd/check -roadmap"}, Owner: "platform"},
			{Name: "server acceptance", Status: "captured", Reports: []string{"reports/server-acceptance-*/summary.json", "reports/server-acceptance-*/deploy_smoke/admin_snapshot.md"}, Owner: "sre"},
			{Name: "TCP/WebSocket/KCP/QUIC weaknet", Status: "captured", Reports: []string{"reports/kcp-quic-weaknet-*/summary.md", "reports/server-acceptance-*/kcp_quic_weaknet*/summary.md", "reports/server-staging-*/kcp_quic_weaknet*/summary.md", "reports/release-gate-server-staging-*/kcp_quic_weaknet*/summary.md"}, Commands: []string{"python scripts/run_gateway_weaknet.py"}, Owner: "network"},
			{Name: "Redis/MySQL failover", Status: "scripted", Reports: []string{"reports/server-acceptance-*/mysql_redis_idempotency*.log"}, Commands: []string{"python scripts/redis_mysql_failover_smoke.py"}, Owner: "platform"},
			{Name: "multi-node stress", Status: "scripted", Reports: []string{"reports/multi-node-stress-*/summary.json", "reports/multi-node-stress-*/summary.md"}, Commands: []string{"python scripts/run_multi_node_stress.py", "python scripts/check_stress_report.py reports/multi-node-stress-*/summary.json"}, Owner: "performance"},
			{Name: "Redis/MySQL/NATS fault injection", Status: "scripted", Reports: []string{"reports/dependency-faults-*/summary.json", "reports/dependency-faults-*/summary.md"}, Commands: []string{"python scripts/run_dependency_faults.py", "python scripts/check_dep_fault_report.py reports/dependency-faults-*/summary.json"}, Owner: "sre"},
			{Name: "long soak", Status: "scripted", Reports: []string{"reports/soak-*/summary.json", "reports/soak-*/summary.md"}, Commands: []string{"python scripts/run_soak.py --duration 6h --interval 30s", "python scripts/check_rollout_report.py reports/soak-*/summary.json --kind soak"}, Owner: "performance"},
			{Name: "chaos drill", Status: "scripted", Reports: []string{"reports/chaos-*/summary.json", "reports/chaos-*/summary.md"}, Commands: []string{"python scripts/run_chaos_drill.py --action restart-logic=\"systemctl restart game-logic\"", "python scripts/check_rollout_report.py reports/chaos-*/summary.json --kind chaos"}, Owner: "sre"},
		},
		ReleaseGates: []Gate{
			{Name: "engineering merge", Tier: "P0", Requires: []string{"local full verification", "boundary checks", "roadmap checks"}, Decision: "must pass before merge"},
			{Name: "staging deployment", Tier: "P0", Requires: []string{"admin snapshot", "sdk smoke", "Redis/MySQL failover", "TCP/WebSocket/KCP/QUIC weaknet", "multi-node stress", "Redis/MySQL/NATS fault injection"}, Decision: "must attach reports before release candidate"},
			{Name: "production rollout", Tier: "P1", Requires: []string{"long soak", "chaos drill", "operator audit drill", "rollback drill"}, Decision: "blocked until evidence exists for LongHeng production launch"},
		},
	}
	return Normalize(plan)
}

func Normalize(plan Plan) Plan {
	for i := range plan.Scenarios {
		plan.Scenarios[i].Name = strings.TrimSpace(plan.Scenarios[i].Name)
		plan.Scenarios[i].Signals = cleanSorted(plan.Scenarios[i].Signals)
		plan.Scenarios[i].Commands = cleanSorted(plan.Scenarios[i].Commands)
	}
	sort.Slice(plan.Scenarios, func(i, j int) bool {
		if plan.Scenarios[i].Tier != plan.Scenarios[j].Tier {
			return plan.Scenarios[i].Tier < plan.Scenarios[j].Tier
		}
		return plan.Scenarios[i].Name < plan.Scenarios[j].Name
	})
	for i := range plan.Modules {
		plan.Modules[i].MustProve = cleanSorted(plan.Modules[i].MustProve)
	}
	sort.Slice(plan.Modules, func(i, j int) bool { return plan.Modules[i].Name < plan.Modules[j].Name })
	sort.Slice(plan.Framework, func(i, j int) bool { return plan.Framework[i].Name < plan.Framework[j].Name })
	sort.Slice(plan.Operations, func(i, j int) bool { return plan.Operations[i].Name < plan.Operations[j].Name })
	sort.Slice(plan.Security, func(i, j int) bool { return plan.Security[i].Name < plan.Security[j].Name })
	sort.Slice(plan.Ecosystem, func(i, j int) bool { return plan.Ecosystem[i].Name < plan.Ecosystem[j].Name })
	for i := range plan.Evidence {
		plan.Evidence[i].Reports = cleanSorted(plan.Evidence[i].Reports)
		plan.Evidence[i].Commands = cleanSorted(plan.Evidence[i].Commands)
	}
	sort.Slice(plan.Evidence, func(i, j int) bool { return plan.Evidence[i].Name < plan.Evidence[j].Name })
	for i := range plan.ReleaseGates {
		plan.ReleaseGates[i].Requires = cleanSorted(plan.ReleaseGates[i].Requires)
	}
	sort.Slice(plan.ReleaseGates, func(i, j int) bool {
		if plan.ReleaseGates[i].Tier != plan.ReleaseGates[j].Tier {
			return plan.ReleaseGates[i].Tier < plan.ReleaseGates[j].Tier
		}
		return plan.ReleaseGates[i].Name < plan.ReleaseGates[j].Name
	})
	plan.Gaps = gaps(plan)
	return plan
}

func Validate(plan Plan) error {
	plan = Normalize(plan)
	var missing []string
	if plan.SchemaVersion <= 0 {
		missing = append(missing, "schema_version")
	}
	requiredScenarios := []string{"single-node functional acceptance", "multi-node gateway logic replay", "weak network TCP/WebSocket/KCP/QUIC", "long soak and leak detection", "chaos and rolling upgrade"}
	for _, name := range requiredScenarios {
		if !hasScenario(plan, name) {
			missing = append(missing, "scenario "+name)
		}
	}
	for _, name := range []string{"matchmaker", "party", "leaderboard", "tournament", "presence"} {
		if !hasModule(plan, name) {
			missing = append(missing, "module "+name)
		}
	}
	for _, name := range []string{"Gate / Node / Mesh", "Actor model", "Config center", "Registry", "Event bus", "Distributed lock", "Cache", "Network transport plugins"} {
		if !hasControl(plan.Framework, name) {
			missing = append(missing, "framework "+name)
		}
	}
	for _, name := range []string{"admin console", "admin snapshot", "audit trail", "alerts"} {
		if !hasControl(plan.Operations, name) {
			missing = append(missing, "operations "+name)
		}
	}
	for _, name := range []string{"account identity", "payments", "admin auth"} {
		if !hasControl(plan.Security, name) {
			missing = append(missing, "security "+name)
		}
	}
	for _, name := range []string{"local full verification", "server acceptance", "TCP/WebSocket/KCP/QUIC weaknet", "Redis/MySQL failover", "multi-node stress", "Redis/MySQL/NATS fault injection", "long soak", "chaos drill"} {
		if !hasEvidence(plan, name) {
			missing = append(missing, "evidence "+name)
		}
	}
	for _, name := range []string{"engineering merge", "staging deployment", "production rollout"} {
		if !hasGate(plan, name) {
			missing = append(missing, "release gate "+name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("readiness plan incomplete: %s", strings.Join(missing, "; "))
	}
	return nil
}

func gaps(plan Plan) []Gap {
	var out []Gap
	for _, scenario := range plan.Scenarios {
		if scenario.Status == "planned" {
			out = append(out, Gap{Area: "scenario", Name: scenario.Name, Reason: "planned validation evidence is not yet automated"})
		}
	}
	controls := append([]Control{}, plan.Framework...)
	controls = append(controls, plan.Operations...)
	controls = append(controls, plan.Security...)
	controls = append(controls, plan.Ecosystem...)
	for _, control := range controls {
		if control.Status == "placeholder" ||
			control.Status == "not-core" ||
			control.Status == "minimal" ||
			control.Status == "minimal-ops" ||
			control.Status == "extension-boundary" {
			out = append(out, Gap{Area: "control", Name: control.Name, Reason: control.Status})
		}
	}
	for _, evidence := range plan.Evidence {
		if evidence.Status == "planned" {
			out = append(out, Gap{Area: "evidence", Name: evidence.Name, Reason: "missing report evidence"})
		}
	}
	return out
}

func hasScenario(plan Plan, name string) bool {
	for _, scenario := range plan.Scenarios {
		if scenario.Name == name {
			return true
		}
	}
	return false
}

func hasModule(plan Plan, name string) bool {
	for _, module := range plan.Modules {
		if module.Name == name {
			return true
		}
	}
	return false
}

func hasControl(controls []Control, name string) bool {
	for _, control := range controls {
		if control.Name == name {
			return true
		}
	}
	return false
}

func hasEvidence(plan Plan, name string) bool {
	for _, evidence := range plan.Evidence {
		if evidence.Name == name {
			return true
		}
	}
	return false
}

func hasGate(plan Plan, name string) bool {
	for _, gate := range plan.ReleaseGates {
		if gate.Name == name {
			return true
		}
	}
	return false
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
