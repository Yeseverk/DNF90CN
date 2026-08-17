package admincmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

var (
	ErrTemplateNotFound = errors.New("admin command template not found")
	ErrInvalidTemplate  = errors.New("admin command template is invalid")
	ErrInvalidParam     = errors.New("admin command template param is invalid")
)

const (
	ParamString = "string"
	ParamInt    = "int"
	ParamBool   = "bool"
	ParamNumber = "number"
)

type ParamSpec struct {
	Name        string   `json:"name"`
	Type        string   `json:"type,omitempty"`
	Required    bool     `json:"required,omitempty"`
	Allowed     []string `json:"allowed,omitempty"`
	Description string   `json:"description,omitempty"`
}

type Template struct {
	Name        string      `json:"name"`
	Operation   string      `json:"operation"`
	Scope       string      `json:"scope,omitempty"`
	Target      string      `json:"target,omitempty"`
	ShardID     string      `json:"shard_id,omitempty"`
	OfflineSafe bool        `json:"offline_safe,omitempty"`
	Dangerous   bool        `json:"dangerous,omitempty"`
	Description string      `json:"description,omitempty"`
	Params      []ParamSpec `json:"params,omitempty"`
}

type TemplateRegistry struct {
	templates map[string]Template
}

func NewTemplateRegistry(templates ...Template) (*TemplateRegistry, error) {
	registry := &TemplateRegistry{templates: make(map[string]Template, len(templates))}
	for _, template := range templates {
		if err := registry.Register(template); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func LoadTemplateRegistry(data []byte) (*TemplateRegistry, error) {
	var doc struct {
		Templates []Template `json:"templates"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	return NewTemplateRegistry(doc.Templates...)
}

func (r *TemplateRegistry) Register(template Template) error {
	if r == nil {
		return ErrTemplateNotFound
	}
	template = normalizeTemplate(template)
	if err := validateTemplate(template); err != nil {
		return err
	}
	if r.templates == nil {
		r.templates = make(map[string]Template)
	}
	if _, exists := r.templates[template.Name]; exists {
		return fmt.Errorf("%w: duplicate template %s", ErrInvalidTemplate, template.Name)
	}
	r.templates[template.Name] = cloneTemplate(template)
	return nil
}

func (r *TemplateRegistry) Get(name string) (Template, bool) {
	if r == nil {
		return Template{}, false
	}
	name = normalizeTemplateID(name)
	template, ok := r.templates[name]
	return cloneTemplate(template), ok
}

func (r *TemplateRegistry) List() []Template {
	if r == nil {
		return nil
	}
	out := make([]Template, 0, len(r.templates))
	for _, template := range r.templates {
		out = append(out, cloneTemplate(template))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (r *TemplateRegistry) Build(name string, command Command, params map[string]any) (Command, error) {
	template, ok := r.Get(name)
	if !ok {
		return Command{}, fmt.Errorf("%w: %s", ErrTemplateNotFound, strings.TrimSpace(name))
	}
	return template.Build(command, params)
}

func (t Template) Build(command Command, params map[string]any) (Command, error) {
	t = normalizeTemplate(t)
	if err := validateTemplate(t); err != nil {
		return Command{}, err
	}
	normalizedParams, err := normTemplateParams(t, params)
	if err != nil {
		return Command{}, err
	}
	command = Normalize(command)
	command.Operation = t.Operation
	command.Scope = firstTemplateString(t.Scope, command.Scope)
	command.Target = firstTemplateString(t.Target, command.Target)
	command.ShardID = firstTemplateString(t.ShardID, command.ShardID)
	command.OfflineSafe = command.OfflineSafe || t.OfflineSafe
	command.Params = normalizedParams
	if t.Dangerous && command.Confirmation == "" {
		command.Confirmation = t.Operation
	}
	return command, nil
}

func DefaultTemplates() []Template {
	return []Template{
		{
			Name:        "profile_repair",
			Operation:   "profile.repair",
			Scope:       "logic",
			Target:      "player",
			Dangerous:   true,
			Description: "Profile check or targeted field repair",
			Params: []ParamSpec{
				{Name: "account_id", Type: ParamString, Required: true},
				{Name: "field", Type: ParamString, Required: true},
				{Name: "mode", Type: ParamString, Required: true, Allowed: []string{"check", "repair"}},
			},
		},
		{
			Name:        "mail_publish",
			Operation:   "notice.publish",
			Scope:       "logic",
			Target:      "notice",
			Dangerous:   true,
			Description: "Notice, mail, or operation announcement publish",
			Params: []ParamSpec{
				{Name: "notice_id", Type: ParamString, Required: true},
				{Name: "shard_id", Type: ParamString},
				{Name: "audience", Type: ParamString, Required: true, Allowed: []string{"account", "shard", "all"}},
			},
		},
		{
			Name:        "redeem_code_put",
			Operation:   "redeem.code.put",
			Scope:       "redeem",
			Target:      "code",
			Dangerous:   true,
			Description: "Create or update a redeem code definition",
			Params: []ParamSpec{
				{Name: "code", Type: ParamString, Required: true},
				{Name: "reward_ref", Type: ParamString, Required: true},
				{Name: "campaign", Type: ParamString},
				{Name: "max_uses", Type: ParamInt},
				{Name: "per_account_limit", Type: ParamInt},
			},
		},
		{
			Name:        "redeem_claim_repair",
			Operation:   "redeem.claim.repair",
			Scope:       "redeem",
			Target:      "account",
			Dangerous:   true,
			Description: "Manual redeem claim repair or compensation",
			Params: []ParamSpec{
				{Name: "code", Type: ParamString, Required: true},
				{Name: "account_id", Type: ParamString, Required: true},
				{Name: "idempotency_key", Type: ParamString, Required: true},
			},
		},
		{
			Name:        "payment_replay",
			Operation:   "commerce.fulfillment.replay",
			Scope:       "commerce",
			Target:      "order",
			Dangerous:   true,
			Description: "Payment fulfillment compensation or receipt replay",
			Params: []ParamSpec{
				{Name: "order_id", Type: ParamString, Required: true},
				{Name: "channel", Type: ParamString, Required: true},
			},
		},
		{
			Name:        "shard_drain",
			Operation:   "servergroup.shard.operation",
			Scope:       "servergroup",
			Target:      "shard",
			Dangerous:   true,
			Description: "Open, drain, or close a shard",
			Params: []ParamSpec{
				{Name: "shard_id", Type: ParamString, Required: true},
				{Name: "state", Type: ParamString, Required: true, Allowed: []string{"open", "draining", "closed"}},
				{Name: "next_version", Type: ParamString, Required: true},
			},
		},
		{
			Name:        "servergroup_merge",
			Operation:   "servergroup.merge.workflow",
			Scope:       "servergroup",
			Target:      "merge",
			Dangerous:   true,
			Description: "区服合服 dry-run、执行或回滚工作流",
			Params: []ParamSpec{
				{Name: "merge_id", Type: ParamString, Required: true},
				{Name: "main_shard_id", Type: ParamString, Required: true},
				{Name: "shards", Type: ParamString, Required: true, Description: "逗号分隔的区服 ID 列表"},
				{Name: "mode", Type: ParamString, Required: true, Allowed: []string{"dry_run", "apply", "rollback_dry_run", "rollback"}},
				{Name: "check_features", Type: ParamString, Description: "逗号分隔的检查 feature"},
				{Name: "block_features", Type: ParamString, Description: "逗号分隔的冻结 feature"},
				{Name: "next_version", Type: ParamString},
				{Name: "archive_id", Type: ParamString},
				{Name: "rollback_archive_id", Type: ParamString, Description: "回滚时引用的 apply 归档 ID"},
				{Name: "restore_version", Type: ParamString},
				{Name: "allow_version_mismatch", Type: ParamBool},
			},
		},
	}
}

func normalizeTemplate(template Template) Template {
	template.Name = normalizeTemplateID(template.Name)
	template.Operation = strings.TrimSpace(template.Operation)
	template.Scope = strings.Trim(strings.ToLower(strings.TrimSpace(template.Scope)), ":")
	template.Target = strings.TrimSpace(template.Target)
	template.ShardID = strings.TrimSpace(template.ShardID)
	template.Description = strings.TrimSpace(template.Description)
	out := make([]ParamSpec, 0, len(template.Params))
	for _, param := range template.Params {
		param.Name = normalizeTemplateID(param.Name)
		param.Type = normalizeParamType(param.Type)
		param.Description = strings.TrimSpace(param.Description)
		param.Allowed = normAllowedValues(param.Allowed)
		out = append(out, param)
	}
	template.Params = out
	return template
}

func validateTemplate(template Template) error {
	if template.Name == "" || template.Operation == "" {
		return fmt.Errorf("%w: name and operation are required", ErrInvalidTemplate)
	}
	seen := make(map[string]struct{}, len(template.Params))
	for _, param := range template.Params {
		if param.Name == "" {
			return fmt.Errorf("%w: param name is required", ErrInvalidTemplate)
		}
		if _, exists := seen[param.Name]; exists {
			return fmt.Errorf("%w: duplicate param %s", ErrInvalidTemplate, param.Name)
		}
		seen[param.Name] = struct{}{}
		switch param.Type {
		case "", ParamString, ParamInt, ParamBool, ParamNumber:
		default:
			return fmt.Errorf("%w: unsupported param type %s", ErrInvalidTemplate, param.Type)
		}
	}
	return nil
}

func normTemplateParams(template Template, params map[string]any) (map[string]any, error) {
	params = normalizeParamMap(params)
	out := make(map[string]any, len(params))
	specs := make(map[string]ParamSpec, len(template.Params))
	for _, spec := range template.Params {
		specs[spec.Name] = spec
		value, ok := params[spec.Name]
		if !ok {
			if spec.Required {
				return nil, fmt.Errorf("%w: %s is required", ErrInvalidParam, spec.Name)
			}
			continue
		}
		normalized, err := normalizeParamValue(spec, value)
		if err != nil {
			return nil, err
		}
		out[spec.Name] = normalized
	}
	for name := range params {
		if _, ok := specs[name]; !ok {
			return nil, fmt.Errorf("%w: unexpected param %s", ErrInvalidParam, name)
		}
	}
	return out, nil
}

func normalizeParamValue(spec ParamSpec, value any) (any, error) {
	switch spec.Type {
	case "", ParamString:
		raw, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("%w: %s must be string", ErrInvalidParam, spec.Name)
		}
		text := strings.TrimSpace(raw)
		if err := validateAllowedValue(spec, text); err != nil {
			return nil, err
		}
		return text, nil
	case ParamInt:
		parsed, err := parseTemplateInt(value)
		if err != nil {
			return nil, fmt.Errorf("%w: %s must be int", ErrInvalidParam, spec.Name)
		}
		return parsed, nil
	case ParamBool:
		parsed, err := parseTemplateBool(value)
		if err != nil {
			return nil, fmt.Errorf("%w: %s must be bool", ErrInvalidParam, spec.Name)
		}
		return parsed, nil
	case ParamNumber:
		parsed, err := parseTemplateNumber(value)
		if err != nil {
			return nil, fmt.Errorf("%w: %s must be number", ErrInvalidParam, spec.Name)
		}
		return parsed, nil
	default:
		return nil, fmt.Errorf("%w: unsupported param type %s", ErrInvalidTemplate, spec.Type)
	}
}

func validateAllowedValue(spec ParamSpec, value string) error {
	if len(spec.Allowed) == 0 {
		return nil
	}
	for _, allowed := range spec.Allowed {
		if value == allowed {
			return nil
		}
	}
	return fmt.Errorf("%w: %s must be one of %s", ErrInvalidParam, spec.Name, strings.Join(spec.Allowed, ","))
}

func parseTemplateInt(value any) (int64, error) {
	switch v := value.(type) {
	case int:
		return int64(v), nil
	case int64:
		return v, nil
	case float64:
		if v != float64(int64(v)) {
			return 0, ErrInvalidParam
		}
		return int64(v), nil
	case json.Number:
		return v.Int64()
	case string:
		return strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	default:
		return 0, ErrInvalidParam
	}
}

func parseTemplateBool(value any) (bool, error) {
	switch v := value.(type) {
	case bool:
		return v, nil
	case string:
		return strconv.ParseBool(strings.TrimSpace(v))
	default:
		return false, ErrInvalidParam
	}
}

func parseTemplateNumber(value any) (float64, error) {
	switch v := value.(type) {
	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case float64:
		return v, nil
	case json.Number:
		return v.Float64()
	case string:
		return strconv.ParseFloat(strings.TrimSpace(v), 64)
	default:
		return 0, ErrInvalidParam
	}
}

func normalizeParamMap(params map[string]any) map[string]any {
	out := make(map[string]any, len(params))
	for key, value := range params {
		key = normalizeTemplateID(key)
		if key != "" {
			out[key] = value
		}
	}
	return out
}

func normalizeTemplateID(value string) string {
	return strings.Trim(strings.ToLower(strings.TrimSpace(value)), ":")
}

func normalizeParamType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ParamString
	}
	return value
}

func normAllowedValues(values []string) []string {
	if len(values) == 0 {
		return nil
	}
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

func cloneTemplate(template Template) Template {
	template.Params = append([]ParamSpec(nil), template.Params...)
	for idx := range template.Params {
		template.Params[idx].Allowed = append([]string(nil), template.Params[idx].Allowed...)
	}
	return template
}

func firstTemplateString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
