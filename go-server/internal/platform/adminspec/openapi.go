package adminspec

import (
	"encoding/json"
	"sort"
)

type Spec struct {
	OpenAPI    string              `json:"openapi"`
	Info       Info                `json:"info"`
	Servers    []Server            `json:"servers,omitempty"`
	Tags       []Tag               `json:"tags,omitempty"`
	Paths      map[string]PathItem `json:"paths"`
	Components Components          `json:"components,omitempty"`
}

type Info struct {
	Title       string `json:"title"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
}

type Server struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

type Tag struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type Components struct {
	SecuritySchemes map[string]SecurityScheme `json:"securitySchemes,omitempty"`
	Schemas         map[string]Schema         `json:"schemas,omitempty"`
}

type SecurityScheme struct {
	Type string `json:"type"`
	In   string `json:"in,omitempty"`
	Name string `json:"name,omitempty"`
}

type PathItem struct {
	Get    *Operation `json:"get,omitempty"`
	Post   *Operation `json:"post,omitempty"`
	Delete *Operation `json:"delete,omitempty"`
}

type Operation struct {
	OperationID string              `json:"operationId,omitempty"`
	Summary     string              `json:"summary"`
	Description string              `json:"description,omitempty"`
	Tags        []string            `json:"tags,omitempty"`
	Security    []map[string][]any  `json:"security,omitempty"`
	RequestBody *RequestBody        `json:"requestBody,omitempty"`
	Parameters  []Parameter         `json:"parameters,omitempty"`
	Responses   map[string]Response `json:"responses"`
}

type RequestBody struct {
	Required bool                 `json:"required,omitempty"`
	Content  map[string]MediaType `json:"content"`
}

type MediaType struct {
	Schema SchemaRef `json:"schema"`
}

type Parameter struct {
	Name        string    `json:"name"`
	In          string    `json:"in"`
	Description string    `json:"description,omitempty"`
	Required    bool      `json:"required,omitempty"`
	Schema      SchemaRef `json:"schema"`
}

type Response struct {
	Description string               `json:"description"`
	Content     map[string]MediaType `json:"content,omitempty"`
}

type SchemaRef struct {
	Ref        string         `json:"$ref,omitempty"`
	Type       string         `json:"type,omitempty"`
	Format     string         `json:"format,omitempty"`
	Items      *SchemaRef     `json:"items,omitempty"`
	Properties map[string]any `json:"properties,omitempty"`
}

type Schema struct {
	Type                 string               `json:"type,omitempty"`
	Format               string               `json:"format,omitempty"`
	AdditionalProperties bool                 `json:"additionalProperties,omitempty"`
	Properties           map[string]SchemaRef `json:"properties,omitempty"`
	Items                *SchemaRef           `json:"items,omitempty"`
}

func Default() Spec {
	spec := Spec{
		OpenAPI: "3.0.3",
		Info: Info{
			Title:       "LongHeng 龙恒 Admin API",
			Version:     "v1",
			Description: "LongHeng 龙恒的 Admin 和 debug API 规范。项目模块可以扩展自己的运营接口。",
		},
		Servers: []Server{{URL: "http://127.0.0.1:18101", Description: "local gateway admin"}},
		Tags: []Tag{
			{Name: "admin", Description: "Admin console and OpenAPI document"},
			{Name: "health", Description: "Health checks"},
			{Name: "platform", Description: "Platform runtime snapshots"},
			{Name: "gateway", Description: "Gateway operations"},
			{Name: "logic", Description: "Logic service operations"},
			{Name: "ops", Description: "Minimal operator backend workflows"},
			{Name: "adminworkflow", Description: "Operator workflow records and rollback notes"},
			{Name: "servergroup", Description: "Server group routing and merge evidence"},
			{Name: "eventlog", Description: "Outbox and event log"},
			{Name: "onlinepush", Description: "Online push, offline fallback, and push receipts"},
			{Name: "accountcenter", Description: "Account center and GetGate control plane"},
			{Name: "storageobject", Description: "Generic lightweight object storage"},
			{Name: "moderation", Description: "Moderation runtime"},
			{Name: "notice", Description: "Notice and announcement runtime"},
			{Name: "redeem", Description: "Redeem code runtime"},
		},
		Paths: make(map[string]PathItem),
		Components: Components{
			SecuritySchemes: map[string]SecurityScheme{
				"AdminToken": {Type: "apiKey", In: "header", Name: "X-Admin-Token"},
			},
			Schemas: defaultSchemas(),
		},
	}
	addGET(&spec, "/healthz/live", "health", "Live check", false)
	addGET(&spec, "/healthz/ready", "health", "Ready check", false)
	addGET(&spec, "/healthz", "health", "Health snapshot", false)
	addGET(&spec, "/admin", "admin", "Admin console", false)
	addGET(&spec, "/admin/openapi.json", "admin", "OpenAPI document", false)
	for _, path := range []string{
		"/debug/config",
		"/debug/metrics",
		"/debug/rate_limit",
		"/debug/discovery",
		"/debug/rpc",
		"/debug/topology",
		"/debug/framework",
		"/debug/adapters",
		"/debug/session-contract",
		"/debug/lifecycle",
		"/debug/readiness",
		"/debug/scheduler",
		"/debug/traces",
		"/debug/datatables",
		"/debug/datatable_views",
		"/debug/i18n",
		"/debug/audit",
		"/debug/logiclog",
		"/debug/bilog",
		"/debug/log_level",
	} {
		addGET(&spec, path, "platform", summaryFromPath(path), true)
	}
	addGET(&spec, "/debug/reload", "platform", "Reload snapshot", true)
	addPOST(&spec, "/debug/reload", "platform", "Trigger runtime reload", true, "")
	addPOST(&spec, "/debug/log_level", "platform", "Change runtime log level", true, "")

	for _, path := range []string{"/gateway/sessions", "/gateway/presence", "/gateway/backends", "/gateway/transports", "/gateway/protocol"} {
		addGET(&spec, path, "gateway", summaryFromPath(path), true)
	}
	addPOST(&spec, "/gateway/push", "gateway", "Push message to gateway session or account", true, "GatewayPushRequest")
	addPOST(&spec, "/gateway/session/kick", "gateway", "Kick gateway session", true, "GatewayKickRequest")
	addPOST(&spec, "/gateway/drain", "gateway", "Enable or disable gateway drain", true, "GatewayDrainRequest")

	addGET(&spec, "/eventlog", "eventlog", "EventLog snapshot", true)
	addGET(&spec, "/eventlog/snapshot", "eventlog", "EventLog snapshot", true)
	addGET(&spec, "/eventlog/deadletters", "eventlog", "List event dead letters", true)
	addPOST(&spec, "/eventlog/deadletters/requeue", "eventlog", "Requeue dead letter", true, "")

	addGET(&spec, "/onlinepush", "onlinepush", "Online push snapshot", true)
	addPOST(&spec, "/onlinepush/send", "onlinepush", "Send online push with idempotent receipt", true, "OnlinePushRequest")
	addGET(&spec, "/onlinepush/offline", "onlinepush", "List offline push messages", true)
	addDELETE(&spec, "/onlinepush/offline", "onlinepush", "Delete offline push message", true)

	addGET(&spec, "/accountcenter", "accountcenter", "Account center snapshot", true)
	addGET(&spec, "/accountcenter/account", "accountcenter", "Query account by id or identity", true)
	addPOST(&spec, "/accountcenter/login", "accountcenter", "Create login and GetGate result", true, "")
	addPOST(&spec, "/accountcenter/bind", "accountcenter", "Bind identity to account", true, "")
	addPOST(&spec, "/accountcenter/ban", "accountcenter", "Create or update account ban", true, "")
	addPOST(&spec, "/accountcenter/allow", "accountcenter", "Create or update account allowlist", true, "")
	addPOST(&spec, "/accountcenter/shards", "accountcenter", "Replace account center shard list", true, "")
	addPOST(&spec, "/accountcenter/gates", "accountcenter", "Replace account center gate list", true, "")

	addGET(&spec, "/storage/objects", "storageobject", "List storage objects", true)
	addPOST(&spec, "/storage/objects/read", "storageobject", "Batch read storage objects", true, "StorageObjectBatchReadRequest")
	addPOST(&spec, "/storage/objects/write", "storageobject", "Batch write storage objects", true, "StorageObjectBatchWriteRequest")
	addPOST(&spec, "/storage/objects/delete", "storageobject", "Batch delete storage objects", true, "StorageObjectBatchDeleteRequest")
	addGET(&spec, "/storage/object", "storageobject", "Read storage object", true)
	addPOST(&spec, "/storage/object", "storageobject", "Write storage object", true, "")
	addDELETE(&spec, "/storage/object", "storageobject", "Delete storage object", true)

	addGET(&spec, "/debug/moderation", "moderation", "Moderation snapshot", true)
	addGET(&spec, "/debug/moderation/snapshot", "moderation", "Moderation snapshot", true)
	addPOST(&spec, "/debug/moderation/evaluate", "moderation", "Evaluate moderation request", true, "ModerationRequest")
	addGET(&spec, "/debug/moderation/sanctions", "moderation", "List moderation sanctions", true)
	addPOST(&spec, "/debug/moderation/sanctions", "moderation", "Upsert moderation sanction", true, "ModerationSanction")
	addDELETE(&spec, "/debug/moderation/sanctions", "moderation", "Delete moderation sanction", true)

	for _, path := range []string{
		"/logic/players",
		"/logic/accounts",
		"/logic/playerstore",
		"/logic/playerstore/deadletters",
		"/logic/world",
		"/logic/playerloops",
		"/logic/idempotency",
		"/logic/handlers",
		"/logic/profiledb",
	} {
		addGET(&spec, path, "logic", summaryFromPath(path), true)
	}
	addPOST(&spec, "/logic/playerstore/deadletters/requeue", "logic", "Requeue player store dead letter", true, "")
	addGET(&spec, "/logic/ops", "ops", "Operator workflow index", true)
	addGET(&spec, "/logic/ops/players", "ops", "Query players and account summaries", true)
	addGET(&spec, "/logic/ops/profile", "ops", "Read Profile without creating it", true)
	addPOST(&spec, "/logic/ops/profile/repair", "ops", "Repair selected Profile fields", true, "ProfileRepairRequest")
	addPOST(&spec, "/logic/ops/events/replay", "ops", "Replay an EventLog dead letter", true, "EventReplayRequest")
	addGET(&spec, "/logic/ops/leaderboard", "ops", "Query leaderboard state", true)
	addPOST(&spec, "/logic/ops/leaderboard/repair", "ops", "Repair leaderboard definition or records", true, "LeaderboardRepairRequest")
	addGET(&spec, "/logic/ops/bans", "ops", "List bans and sanctions", true)
	addPOST(&spec, "/logic/ops/bans", "ops", "Create or update ban", true, "OpsSanctionRequest")
	addDELETE(&spec, "/logic/ops/bans", "ops", "Remove ban", true)
	addGET(&spec, "/debug/admin/workflows", "adminworkflow", "List operator workflow records", true)
	addGET(&spec, "/debug/admin/workflows/{id}", "adminworkflow", "Read operator workflow record", true)
	addGET(&spec, "/debug/admin/workflows/{id}/rollback-note", "adminworkflow", "Read operator workflow rollback note", true)
	addGET(&spec, "/debug/servergroup/merge/workflow/archives", "servergroup", "List merge workflow archives", true)
	addGET(&spec, "/debug/servergroup/merge/workflow/archives/{archive_id}", "servergroup", "Read merge workflow archive", true)
	addGET(&spec, "/debug/servergroup/merge/workflow/archives/{archive_id}/export", "servergroup", "Export merge workflow archive JSON", true)

	addGET(&spec, "/logic/notice", "notice", "Notice runtime snapshot", true)
	addGET(&spec, "/logic/notice/snapshot", "notice", "Notice runtime snapshot", true)
	addPOST(&spec, "/logic/notice/publish", "notice", "Publish direct notice or announcement", true, "NoticePublishRequest")
	addPOST(&spec, "/logic/notice/acknowledge", "notice", "Acknowledge notice delivery", true, "NoticeAcknowledgeRequest")
	addGET(&spec, "/logic/notice/account", "notice", "List account visible notice deliveries", true)
	addGET(&spec, "/logic/notice/announcements", "notice", "List active announcements", true)
	addGET(&spec, "/logic/redeem", "redeem", "Redeem runtime snapshot", true)
	addGET(&spec, "/logic/redeem/snapshot", "redeem", "Redeem runtime snapshot", true)
	addPOST(&spec, "/logic/redeem/codes", "redeem", "Create or update redeem code", true, "RedeemCode")
	addPOST(&spec, "/logic/redeem/claim", "redeem", "Repair or issue redeem claim", true, "RedeemClaimRequest")
	return spec
}

func MarshalJSON(spec Spec, pretty bool) ([]byte, error) {
	if pretty {
		return json.MarshalIndent(spec, "", "  ")
	}
	return json.Marshal(spec)
}

func SortedPaths(spec Spec) []string {
	paths := make([]string, 0, len(spec.Paths))
	for path := range spec.Paths {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func addGET(spec *Spec, path, tag, summary string, protected bool) {
	item := spec.Paths[path]
	item.Get = operation("get", path, tag, summary, protected, "")
	spec.Paths[path] = item
}

func addPOST(spec *Spec, path, tag, summary string, protected bool, requestSchema string) {
	item := spec.Paths[path]
	item.Post = operation("post", path, tag, summary, protected, requestSchema)
	spec.Paths[path] = item
}

func addDELETE(spec *Spec, path, tag, summary string, protected bool) {
	item := spec.Paths[path]
	item.Delete = operation("delete", path, tag, summary, protected, "")
	item.Delete.Parameters = append(item.Delete.Parameters, Parameter{
		Name:        "id",
		In:          "query",
		Description: "resource id",
		Schema:      SchemaRef{Type: "string"},
	})
	spec.Paths[path] = item
}

func operation(method, path, tag, summary string, protected bool, requestSchema string) *Operation {
	op := &Operation{
		OperationID: operationID(method, path),
		Summary:     summary,
		Tags:        []string{tag},
		Responses: map[string]Response{
			"200": {Description: "OK", Content: jsonObjectContent()},
			"400": {Description: "Bad request", Content: jsonObjectContent()},
			"401": {Description: "Unauthorized", Content: jsonObjectContent()},
			"403": {Description: "Forbidden", Content: jsonObjectContent()},
			"428": {Description: "Dangerous operation confirmation required", Content: jsonObjectContent()},
			"503": {Description: "Service unavailable", Content: jsonObjectContent()},
		},
	}
	if protected {
		op.Security = []map[string][]any{{"AdminToken": []any{}}}
	}
	if protected && (method == "post" || method == "delete") {
		op.Parameters = append(op.Parameters,
			Parameter{
				Name:        "X-Admin-Actor",
				In:          "header",
				Description: "operator or automation identity for audit logs",
				Schema:      SchemaRef{Type: "string"},
			},
			Parameter{
				Name:        "X-Admin-Trace-ID",
				In:          "header",
				Description: "optional operation trace id",
				Schema:      SchemaRef{Type: "string"},
			},
			Parameter{
				Name:        "X-Admin-Confirm",
				In:          "header",
				Description: "required for dangerous operations when admin.dangerous_confirm is enabled; value is the operation id such as POST /gateway/drain",
				Schema:      SchemaRef{Type: "string"},
			},
		)
	}
	if requestSchema != "" {
		op.RequestBody = &RequestBody{
			Required: true,
			Content: map[string]MediaType{
				"application/json": {Schema: SchemaRef{Ref: "#/components/schemas/" + requestSchema}},
			},
		}
	}
	return op
}

func jsonObjectContent() map[string]MediaType {
	return map[string]MediaType{
		"application/json": {Schema: SchemaRef{Type: "object"}},
	}
}

func operationID(method, path string) string {
	out := method
	for _, r := range path {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			out += string(r)
			continue
		}
		out += "_"
	}
	return out
}

func summaryFromPath(path string) string {
	switch path {
	case "/debug/config":
		return "Redacted config snapshot"
	case "/debug/metrics":
		return "Metrics snapshot"
	case "/debug/reload":
		return "Config reload"
	case "/debug/log_level":
		return "Log level"
	default:
		return path
	}
}

func defaultSchemas() map[string]Schema {
	return map[string]Schema{
		"GatewayPushRequest": {
			Type: "object",
			Properties: map[string]SchemaRef{
				"account_id": {Type: "string"},
				"session_id": {Type: "string"},
				"msg_id":     {Type: "integer", Format: "int64"},
				"body":       {Type: "string"},
			},
		},
		"GatewayKickRequest": {
			Type: "object",
			Properties: map[string]SchemaRef{
				"session_id": {Type: "string"},
				"account_id": {Type: "string"},
				"reason":     {Type: "string"},
			},
		},
		"GatewayDrainRequest": {
			Type: "object",
			Properties: map[string]SchemaRef{
				"enabled":        {Type: "boolean"},
				"reason":         {Type: "string"},
				"close_existing": {Type: "boolean"},
			},
		},
		"OnlinePushRequest": {
			Type: "object",
			Properties: map[string]SchemaRef{
				"idempotency_key":        {Type: "string"},
				"account_id":             {Type: "string"},
				"account_ids":            {Type: "array", Items: &SchemaRef{Type: "string"}},
				"session_id":             {Type: "string"},
				"target_gateway_node_id": {Type: "string"},
				"broadcast":              {Type: "boolean"},
				"packet_id":              {Type: "integer", Format: "int32"},
				"msg_id":                 {Type: "integer", Format: "int64"},
				"wire_format":            {Type: "string"},
				"body":                   {Type: "string"},
				"offline_policy":         {Type: "string"},
				"note":                   {Type: "string"},
			},
		},
		"StorageObjectBatchReadRequest": {
			Type: "object",
			Properties: map[string]SchemaRef{
				"keys": {Type: "array", Items: &SchemaRef{Type: "object"}},
			},
		},
		"StorageObjectBatchWriteRequest": {
			Type: "object",
			Properties: map[string]SchemaRef{
				"writes": {Type: "array", Items: &SchemaRef{Type: "object"}},
			},
		},
		"StorageObjectBatchDeleteRequest": {
			Type: "object",
			Properties: map[string]SchemaRef{
				"deletes": {Type: "array", Items: &SchemaRef{Type: "object"}},
			},
		},
		"ModerationRequest": {
			Type: "object",
			Properties: map[string]SchemaRef{
				"subject": {Type: "string"},
				"scope":   {Type: "string"},
				"text":    {Type: "string"},
			},
		},
		"ModerationSanction": {
			Type: "object",
			Properties: map[string]SchemaRef{
				"id":      {Type: "string"},
				"subject": {Type: "string"},
				"scope":   {Type: "string"},
				"kind":    {Type: "string"},
				"reason":  {Type: "string"},
				"source":  {Type: "string"},
				"until":   {Type: "string", Format: "date-time"},
			},
		},
		"ProfileRepairRequest": {
			Type: "object",
			Properties: map[string]SchemaRef{
				"account_id": {Type: "string"},
				"state":      {Type: "string"},
				"name":       {Type: "string"},
				"level":      {Type: "integer", Format: "int32"},
				"reason":     {Type: "string"},
			},
		},
		"EventReplayRequest": {
			Type: "object",
			Properties: map[string]SchemaRef{
				"id": {Type: "string"},
			},
		},
		"LeaderboardRepairRequest": {
			Type: "object",
			Properties: map[string]SchemaRef{
				"action":         {Type: "string"},
				"leaderboard_id": {Type: "string"},
				"owner_id":       {Type: "string"},
				"title":          {Type: "string"},
				"sort_order":     {Type: "string"},
				"operator":       {Type: "string"},
				"max_size":       {Type: "integer", Format: "int32"},
				"score":          {Type: "integer", Format: "int64"},
				"subscore":       {Type: "integer", Format: "int64"},
			},
		},
		"OpsSanctionRequest": {
			Type: "object",
			Properties: map[string]SchemaRef{
				"id":          {Type: "string"},
				"subject":     {Type: "string"},
				"scope":       {Type: "string"},
				"kind":        {Type: "string"},
				"reason":      {Type: "string"},
				"source":      {Type: "string"},
				"until":       {Type: "string", Format: "date-time"},
				"ttl_seconds": {Type: "integer", Format: "int64"},
			},
		},
		"NoticePublishRequest": {
			Type: "object",
			Properties: map[string]SchemaRef{
				"notice":          {Type: "object"},
				"recipients":      {Type: "array", Items: &SchemaRef{Type: "string"}},
				"idempotency_key": {Type: "string"},
				"meta":            {Type: "object"},
				"admin_command":   {Type: "object"},
			},
		},
		"NoticeAcknowledgeRequest": {
			Type: "object",
			Properties: map[string]SchemaRef{
				"delivery_id":     {Type: "string"},
				"notice_id":       {Type: "string"},
				"account_id":      {Type: "string"},
				"idempotency_key": {Type: "string"},
			},
		},
		"RedeemCode": {
			Type: "object",
			Properties: map[string]SchemaRef{
				"code":              {Type: "string"},
				"campaign":          {Type: "string"},
				"reward_ref":        {Type: "string"},
				"max_uses":          {Type: "integer", Format: "int32"},
				"per_account_limit": {Type: "integer", Format: "int32"},
				"starts_at":         {Type: "string", Format: "date-time"},
				"ends_at":           {Type: "string", Format: "date-time"},
				"disabled":          {Type: "boolean"},
				"meta":              {Type: "object"},
			},
		},
		"RedeemClaimRequest": {
			Type: "object",
			Properties: map[string]SchemaRef{
				"code":            {Type: "string"},
				"account_id":      {Type: "string"},
				"shard_id":        {Type: "string"},
				"source":          {Type: "string"},
				"idempotency_key": {Type: "string"},
				"meta":            {Type: "object"},
				"admin_command":   {Type: "object"},
			},
		},
	}
}
