package adapters

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

const SupportDocumentKind = "plugin_adapter_support_matrix"

var ErrInvalidSupportMatrix = errors.New("adapter support matrix is invalid")

type SupportLevel string

const (
	SupportDevOnly          SupportLevel = "dev_only"
	SupportLiveOnly         SupportLevel = "live_only"
	SupportContract         SupportLevel = "contract"
	SupportSupported        SupportLevel = "supported"
	SupportPlanned          SupportLevel = "planned"
	SupportPlannedDurable   SupportLevel = "planned_durable"
	SupportSkeleton         SupportLevel = "skeleton"
	SupportInterfaceOnly    SupportLevel = "interface_only"
	SupportEvidenceRequired SupportLevel = "evidence_required"
	SupportNotApplicable    SupportLevel = "not_cache"
)

type SupportDocument struct {
	SchemaVersion int             `json:"schema_version"`
	Kind          string          `json:"kind"`
	Adapters      []SupportDomain `json:"adapters"`
}

type SupportDomain struct {
	Domain           string                  `json:"domain"`
	Backends         map[string]SupportLevel `json:"-"`
	RequiredEvidence []string                `json:"required_evidence"`
}

type SupportValidateOptions struct {
	RequiredDomains []string
}

func SupportDomains() []string {
	return []string{"registry", "config", "eventbus", "cache", "lock"}
}

func LoadSupportFile(path string) (SupportDocument, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304：路径来自框架配置、仓库扫描或测试临时目录，调用点负责限定输入范围。
	if err != nil {
		return SupportDocument{}, err
	}
	return ParseSupport(data)
}

func ParseSupport(data []byte) (SupportDocument, error) {
	var doc SupportDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return SupportDocument{}, err
	}
	doc = NormalizeSupport(doc)
	if err := ValidateSupport(doc, SupportValidateOptions{RequiredDomains: SupportDomains()}); err != nil {
		return SupportDocument{}, err
	}
	return doc, nil
}

func NormalizeSupport(doc SupportDocument) SupportDocument {
	doc.Kind = strings.TrimSpace(doc.Kind)
	for idx := range doc.Adapters {
		doc.Adapters[idx] = NormalizeSupportDomain(doc.Adapters[idx])
	}
	sort.SliceStable(doc.Adapters, func(i, j int) bool { return doc.Adapters[i].Domain < doc.Adapters[j].Domain })
	return doc
}

func NormalizeSupportDomain(domain SupportDomain) SupportDomain {
	domain.Domain = normalizeAdapterID(domain.Domain)
	domain.RequiredEvidence = cleanSorted(domain.RequiredEvidence)
	backends := make(map[string]SupportLevel, len(domain.Backends))
	for key, level := range domain.Backends {
		key = normalizeAdapterID(key)
		level = SupportLevel(strings.TrimSpace(string(level)))
		if key != "" {
			backends[key] = level
		}
	}
	domain.Backends = backends
	return domain
}

func ValidateSupport(doc SupportDocument, options SupportValidateOptions) error {
	if doc.SchemaVersion <= 0 {
		return fmt.Errorf("%w: schema_version is required", ErrInvalidSupportMatrix)
	}
	if doc.Kind != SupportDocumentKind {
		return fmt.Errorf("%w: kind must be %s", ErrInvalidSupportMatrix, SupportDocumentKind)
	}
	if len(doc.Adapters) == 0 {
		return fmt.Errorf("%w: adapters is empty", ErrInvalidSupportMatrix)
	}
	seen := make(map[string]SupportDomain, len(doc.Adapters))
	for _, domain := range doc.Adapters {
		if err := validSupportDomain(domain); err != nil {
			return err
		}
		if _, ok := seen[domain.Domain]; ok {
			return fmt.Errorf("%w: duplicate domain %s", ErrInvalidSupportMatrix, domain.Domain)
		}
		seen[domain.Domain] = domain
	}
	for _, required := range cleanSorted(options.RequiredDomains) {
		required = normalizeAdapterID(required)
		if _, ok := seen[required]; !ok {
			return fmt.Errorf("%w: required domain %s is missing", ErrInvalidSupportMatrix, required)
		}
	}
	return nil
}

func (d SupportDocument) FindSupportDomain(name string) (SupportDomain, bool) {
	name = normalizeAdapterID(name)
	for _, domain := range d.Adapters {
		if domain.Domain == name {
			return domain, true
		}
	}
	return SupportDomain{}, false
}

func (d *SupportDomain) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	for key, value := range raw {
		switch key {
		case "domain":
			if err := json.Unmarshal(value, &d.Domain); err != nil {
				return err
			}
		case "required_evidence":
			if err := json.Unmarshal(value, &d.RequiredEvidence); err != nil {
				return err
			}
		default:
			var level SupportLevel
			if err := json.Unmarshal(value, &level); err != nil {
				return err
			}
			if d.Backends == nil {
				d.Backends = make(map[string]SupportLevel)
			}
			d.Backends[key] = level
		}
	}
	return nil
}

func validSupportDomain(domain SupportDomain) error {
	if domain.Domain == "" {
		return fmt.Errorf("%w: domain is required", ErrInvalidSupportMatrix)
	}
	if len(domain.Backends) == 0 {
		return fmt.Errorf("%w: %s has no backend", ErrInvalidSupportMatrix, domain.Domain)
	}
	if len(domain.RequiredEvidence) == 0 {
		return fmt.Errorf("%w: %s required_evidence is empty", ErrInvalidSupportMatrix, domain.Domain)
	}
	hasUsable := false
	for backend, level := range domain.Backends {
		if backend == "" {
			return fmt.Errorf("%w: %s backend name is empty", ErrInvalidSupportMatrix, domain.Domain)
		}
		if !allowedSupportLevel(level) {
			return fmt.Errorf("%w: %s/%s has invalid support level %s", ErrInvalidSupportMatrix, domain.Domain, backend, level)
		}
		switch level {
		case SupportDevOnly, SupportLiveOnly, SupportContract, SupportSupported, SupportSkeleton, SupportInterfaceOnly, SupportEvidenceRequired:
			hasUsable = true
		}
	}
	if !hasUsable {
		return fmt.Errorf("%w: %s has no usable backend", ErrInvalidSupportMatrix, domain.Domain)
	}
	return nil
}

func allowedSupportLevel(level SupportLevel) bool {
	switch level {
	case SupportDevOnly, SupportLiveOnly, SupportContract, SupportSupported, SupportPlanned, SupportPlannedDurable, SupportSkeleton, SupportInterfaceOnly, SupportEvidenceRequired, SupportNotApplicable:
		return true
	default:
		return false
	}
}

func normalizeAdapterID(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "-", "_")
	value = strings.ReplaceAll(value, " ", "_")
	return strings.ToLower(value)
}
