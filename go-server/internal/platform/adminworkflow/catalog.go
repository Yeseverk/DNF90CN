package adminworkflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

const CatalogKind = "admin_operator_workflows"

var ErrCatalogInvalid = errors.New("admin workflow catalog is invalid")

type Catalog struct {
	SchemaVersion int                `json:"schema_version"`
	Kind          string             `json:"kind"`
	Workflows     []WorkflowTemplate `json:"workflows"`
}

type WorkflowTemplate struct {
	Name             string   `json:"name"`
	RequiredControls []string `json:"required_controls"`
	Evidence         []string `json:"evidence"`
}

type CatalogValidateOptions struct {
	RequiredNames []string
}

func WorkflowNames() []string {
	return []string{
		"player_query",
		"session_drain_and_kick",
		"profile_repair",
		"deadletter_requeue",
		"event_replay",
		"leaderboard_correction",
		"ban_or_unban",
		"rollback_or_gray_recovery",
	}
}

func LoadCatalogFile(path string) (Catalog, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304：路径来自框架配置、仓库扫描或测试临时目录，调用点负责限定输入范围。
	if err != nil {
		return Catalog{}, err
	}
	return ParseCatalog(data)
}

func ParseCatalog(data []byte) (Catalog, error) {
	var catalog Catalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		return Catalog{}, err
	}
	catalog = NormalizeCatalog(catalog)
	if err := ValidateCatalog(catalog, CatalogValidateOptions{RequiredNames: WorkflowNames()}); err != nil {
		return Catalog{}, err
	}
	return catalog, nil
}

func NormalizeCatalog(catalog Catalog) Catalog {
	catalog.Kind = strings.TrimSpace(catalog.Kind)
	for idx := range catalog.Workflows {
		catalog.Workflows[idx] = NormalizeWorkflowTemplate(catalog.Workflows[idx])
	}
	sort.SliceStable(catalog.Workflows, func(i, j int) bool { return catalog.Workflows[i].Name < catalog.Workflows[j].Name })
	return catalog
}

func NormalizeWorkflowTemplate(template WorkflowTemplate) WorkflowTemplate {
	template.Name = normalizeWorkflowID(template.Name)
	template.RequiredControls = normWorkflowList(template.RequiredControls)
	template.Evidence = normWorkflowList(template.Evidence)
	return template
}

func ValidateCatalog(catalog Catalog, options CatalogValidateOptions) error {
	if catalog.SchemaVersion <= 0 {
		return fmt.Errorf("%w: schema_version is required", ErrCatalogInvalid)
	}
	if catalog.Kind != CatalogKind {
		return fmt.Errorf("%w: kind must be %s", ErrCatalogInvalid, CatalogKind)
	}
	if len(catalog.Workflows) == 0 {
		return fmt.Errorf("%w: workflows is empty", ErrCatalogInvalid)
	}
	seen := make(map[string]WorkflowTemplate, len(catalog.Workflows))
	for _, template := range catalog.Workflows {
		if err := ValidateWorkflowTemplate(template); err != nil {
			return err
		}
		if _, ok := seen[template.Name]; ok {
			return fmt.Errorf("%w: duplicate workflow %s", ErrCatalogInvalid, template.Name)
		}
		seen[template.Name] = template
	}
	for _, name := range normWorkflowList(options.RequiredNames) {
		name = normalizeWorkflowID(name)
		if _, ok := seen[name]; !ok {
			return fmt.Errorf("%w: required workflow %s is missing", ErrCatalogInvalid, name)
		}
	}
	return nil
}

func ValidateWorkflowTemplate(template WorkflowTemplate) error {
	template = NormalizeWorkflowTemplate(template)
	if template.Name == "" {
		return fmt.Errorf("%w: workflow name is required", ErrCatalogInvalid)
	}
	if len(template.RequiredControls) == 0 {
		return fmt.Errorf("%w: %s controls are empty", ErrCatalogInvalid, template.Name)
	}
	if len(template.Evidence) == 0 {
		return fmt.Errorf("%w: %s evidence is empty", ErrCatalogInvalid, template.Name)
	}
	controls := workflowSet(template.RequiredControls)
	if !hasWorkflowItem(controls, "RBAC") {
		return fmt.Errorf("%w: %s requires RBAC control", ErrCatalogInvalid, template.Name)
	}
	if !hasWorkflowItem(controls, "audit") {
		return fmt.Errorf("%w: %s requires audit control", ErrCatalogInvalid, template.Name)
	}
	if hasWorkflowItem(controls, "dangerous_confirmation") {
		for _, required := range []string{"idempotency_key", "command_receipt"} {
			if !hasWorkflowItem(controls, required) {
				return fmt.Errorf("%w: %s dangerous workflow requires %s", ErrCatalogInvalid, template.Name, required)
			}
		}
	}
	return nil
}

func (c Catalog) FindWorkflow(name string) (WorkflowTemplate, bool) {
	name = normalizeWorkflowID(name)
	for _, template := range c.Workflows {
		if template.Name == name {
			return template, true
		}
	}
	return WorkflowTemplate{}, false
}

func workflowSet(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			out[value] = struct{}{}
		}
	}
	return out
}

func hasWorkflowItem(values map[string]struct{}, item string) bool {
	_, ok := values[strings.ToLower(strings.TrimSpace(item))]
	return ok
}

func normalizeWorkflowID(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "-", "_")
	value = strings.ReplaceAll(value, " ", "_")
	return strings.ToLower(value)
}

func normWorkflowList(values []string) []string {
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
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}
