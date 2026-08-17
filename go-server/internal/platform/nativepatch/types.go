package nativepatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	PlanFileName       = "native_patch.json"
	SymbolManifestFunc = "LongHengPatchSymbols"
	MaxSymbols         = 100
)

var (
	ErrUnsupported        = errors.New("native patch is unsupported on this platform")
	ErrPlanRequired       = errors.New("native patch plan is required")
	ErrVersionRequired    = errors.New("native patch version is required")
	ErrPluginRequired     = errors.New("native patch plugin file is required")
	ErrSymbolRequired     = errors.New("native patch symbol mapping is required")
	ErrTooManySymbols     = errors.New("native patch symbol mapping exceeds limit")
	ErrUnsafePackageEntry = errors.New("native patch package contains unsafe entry")
	ErrPolicyRejected     = errors.New("native patch policy rejected plan")
)

type Plan struct {
	Version     string       `json:"version"`
	BuildID     string       `json:"build_id,omitempty"`
	Target      string       `json:"target,omitempty"`
	RequestedBy string       `json:"requested_by,omitempty"`
	Reason      string       `json:"reason,omitempty"`
	Plugins     []PluginPlan `json:"plugins"`
}

type PluginPlan struct {
	File    string            `json:"file"`
	Symbols map[string]string `json:"symbols"`
}

type Package struct {
	Directory string `json:"directory"`
	Plan      Plan   `json:"plan"`
}

type Policy struct {
	AllowedTargets     []string
	AllowedOldSymbols  []string
	RequireBuildID     bool
	RequireRequestedBy bool
	RequireReason      bool
	MinReasonLength    int
	MaxSymbols         int
	MaxLiveDuration    time.Duration
}

type Result struct {
	Version   string    `json:"version"`
	Target    string    `json:"target,omitempty"`
	Applied   int       `json:"applied"`
	Restored  bool      `json:"restored,omitempty"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at"`
}

type Snapshot struct {
	Supported      bool      `json:"supported"`
	Applied        bool      `json:"applied"`
	Version        string    `json:"version,omitempty"`
	Target         string    `json:"target,omitempty"`
	AppliedSymbols int       `json:"applied_symbols,omitempty"`
	LastAppliedAt  time.Time `json:"last_applied_at,omitempty"`
	LastRestoredAt time.Time `json:"last_restored_at,omitempty"`
}

type Engine interface {
	Supported() bool
	Apply(context.Context, Package) (Result, error)
	Restore(context.Context) (Result, error)
	Snapshot() Snapshot
}

func LoadPackage(ctx context.Context, directory string) (Package, error) {
	if err := contextErr(ctx); err != nil {
		return Package{}, err
	}
	directory = strings.TrimSpace(directory)
	if directory == "" {
		return Package{}, ErrPlanRequired
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return Package{}, fmt.Errorf("stat native patch package %q: %w", directory, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return Package{}, fmt.Errorf("%w: package root symlink %s", ErrUnsafePackageEntry, directory)
	}
	if !info.IsDir() {
		return Package{}, fmt.Errorf("native patch package %q is not a directory", directory)
	}
	// 热补丁包会被进程直接加载执行，必须先拒绝软链和越界路径。
	// 这里不信任打包端，所有文件都按外部输入处理。
	if err := rejectUnsafeEntries(ctx, directory); err != nil {
		return Package{}, err
	}
	data, err := os.ReadFile(filepath.Join(directory, PlanFileName)) //nolint:gosec // G304：路径来自框架配置、仓库扫描或测试临时目录，调用点负责限定输入范围。
	if err != nil {
		return Package{}, fmt.Errorf("read native patch plan: %w", err)
	}
	var plan Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		return Package{}, fmt.Errorf("parse native patch plan: %w", err)
	}
	if err := ValidatePlan(plan); err != nil {
		return Package{}, err
	}
	if err := validatePackageFiles(directory, plan); err != nil {
		return Package{}, err
	}
	return Package{Directory: directory, Plan: plan}, nil
}

func ValidatePlan(plan Plan) error {
	plan.Version = strings.TrimSpace(plan.Version)
	if plan.Version == "" {
		return ErrVersionRequired
	}
	if len(plan.Plugins) == 0 {
		return ErrPluginRequired
	}
	totalSymbols := 0
	seenFiles := make(map[string]struct{}, len(plan.Plugins))
	seenOldSymbols := make(map[string]struct{}, MaxSymbols)
	for idx, plugin := range plan.Plugins {
		file := strings.TrimSpace(plugin.File)
		if file != plugin.File {
			return fmt.Errorf("plugin %d: %w: surrounding whitespace in plugin file", idx, ErrUnsafePackageEntry)
		}
		if err := validatePluginFile(file); err != nil {
			return fmt.Errorf("plugin %d: %w", idx, err)
		}
		if _, ok := seenFiles[file]; ok {
			return fmt.Errorf("plugin %d: duplicate plugin file %q", idx, file)
		}
		seenFiles[file] = struct{}{}
		if len(plugin.Symbols) == 0 {
			return fmt.Errorf("plugin %d: %w", idx, ErrSymbolRequired)
		}
		keys := make([]string, 0, len(plugin.Symbols))
		for newSymbol := range plugin.Symbols {
			keys = append(keys, newSymbol)
		}
		sort.Strings(keys)
		// 同一个旧函数只能被替换一次，否则恢复顺序和审计含义都会变得不可控。
		for _, newSymbol := range keys {
			oldSymbol := plugin.Symbols[newSymbol]
			if newSymbol != strings.TrimSpace(newSymbol) || oldSymbol != strings.TrimSpace(oldSymbol) {
				return fmt.Errorf("plugin %d: %w: symbol contains surrounding whitespace", idx, ErrSymbolRequired)
			}
			if strings.TrimSpace(newSymbol) == "" || strings.TrimSpace(oldSymbol) == "" {
				return fmt.Errorf("plugin %d: %w", idx, ErrSymbolRequired)
			}
			if hasUnsafeTokenRune(newSymbol) || hasUnsafeTokenRune(oldSymbol) {
				return fmt.Errorf("plugin %d: %w: symbol contains control character", idx, ErrUnsafePackageEntry)
			}
			if _, ok := seenOldSymbols[oldSymbol]; ok {
				return fmt.Errorf("plugin %d: duplicate old symbol %q", idx, oldSymbol)
			}
			seenOldSymbols[oldSymbol] = struct{}{}
			totalSymbols++
			if totalSymbols > MaxSymbols {
				return ErrTooManySymbols
			}
		}
	}
	return nil
}

func (p Policy) Validate(plan Plan) error {
	if err := ValidatePlan(plan); err != nil {
		return err
	}
	// Policy 是热补丁的第二道门：包结构合法不代表允许在当前生产目标上执行。
	// 生产环境应至少要求 build_id、requested_by、reason 和允许替换的符号白名单。
	if p.RequireBuildID && strings.TrimSpace(plan.BuildID) == "" {
		return fmt.Errorf("%w: build_id is required", ErrPolicyRejected)
	}
	if p.RequireRequestedBy && strings.TrimSpace(plan.RequestedBy) == "" {
		return fmt.Errorf("%w: requested_by is required", ErrPolicyRejected)
	}
	reason := strings.TrimSpace(plan.Reason)
	if p.MinReasonLength < 0 {
		return fmt.Errorf("%w: min reason length must be non-negative", ErrPolicyRejected)
	}
	if p.RequireReason && reason == "" {
		return fmt.Errorf("%w: reason is required", ErrPolicyRejected)
	}
	if reason != "" {
		minReasonLength := p.MinReasonLength
		if p.RequireReason && minReasonLength == 0 {
			minReasonLength = 8
		}
		if minReasonLength > 0 && len([]rune(reason)) < minReasonLength {
			return fmt.Errorf("%w: reason must be at least %d characters", ErrPolicyRejected, minReasonLength)
		}
	}
	if p.MaxLiveDuration < 0 {
		return fmt.Errorf("%w: max live duration must be non-negative", ErrPolicyRejected)
	}
	if len(p.AllowedTargets) > 0 && !stringAllowed(strings.TrimSpace(plan.Target), p.AllowedTargets) {
		return fmt.Errorf("%w: target %q is not allowed", ErrPolicyRejected, plan.Target)
	}
	limit := p.MaxSymbols
	if limit <= 0 {
		limit = MaxSymbols
	}
	count := 0
	for _, plugin := range plan.Plugins {
		for _, oldSymbol := range plugin.Symbols {
			oldSymbol = strings.TrimSpace(oldSymbol)
			count++
			if count > limit {
				return ErrTooManySymbols
			}
			if len(p.AllowedOldSymbols) > 0 && !stringAllowed(oldSymbol, p.AllowedOldSymbols) {
				return fmt.Errorf("%w: old symbol %q is not allowed", ErrPolicyRejected, oldSymbol)
			}
		}
	}
	return nil
}

func stringAllowed(value string, allowed []string) bool {
	value = strings.TrimSpace(value)
	for _, item := range allowed {
		if value == strings.TrimSpace(item) {
			return true
		}
	}
	return false
}

func validatePluginFile(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return ErrPluginRequired
	}
	if hasUnsafeTokenRune(value) {
		return fmt.Errorf("%w: %s", ErrUnsafePackageEntry, value)
	}
	if filepath.IsAbs(value) || strings.Contains(value, "..") {
		return fmt.Errorf("%w: %s", ErrUnsafePackageEntry, value)
	}
	clean := filepath.Clean(value)
	if clean == "." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%w: %s", ErrUnsafePackageEntry, value)
	}
	base := filepath.Base(clean)
	if clean != base {
		return fmt.Errorf("%w: plugin file must be in package root: %s", ErrUnsafePackageEntry, value)
	}
	if !strings.HasPrefix(base, "patch") || !strings.HasSuffix(base, ".so") {
		return fmt.Errorf("native patch plugin %q must match patch*.so", value)
	}
	return nil
}

func validatePackageFiles(directory string, plan Plan) error {
	for idx, plugin := range plan.Plugins {
		path := filepath.Join(directory, plugin.File)
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("plugin %d: stat native patch plugin %q: %w", idx, plugin.File, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: plugin symlink %s", ErrUnsafePackageEntry, plugin.File)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%w: plugin %s is not a regular file", ErrUnsafePackageEntry, plugin.File)
		}
	}
	return nil
}

func rejectUnsafeEntries(ctx context.Context, root string) error {
	// WalkDir 覆盖整个包目录，防止压缩包里藏软链、绝对路径或目录逃逸。
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := contextErr(ctx); err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: symlink %s", ErrUnsafePackageEntry, path)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		clean := filepath.Clean(rel)
		if clean == "." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.IsAbs(clean) {
			return fmt.Errorf("%w: %s", ErrUnsafePackageEntry, rel)
		}
		return nil
	})
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func hasUnsafeTokenRune(value string) bool {
	for _, r := range value {
		if r == 0 || r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}
