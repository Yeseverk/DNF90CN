package servergroup

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

var (
	// ErrMergeModuleInvalid 表示合服模块缺少名称、阶段或执行函数。
	ErrMergeModuleInvalid = errors.New("server group merge module is invalid")
	// ErrMergeModuleExists 表示合服模块名称已经注册。
	ErrMergeModuleExists = errors.New("server group merge module already exists")
	// ErrMergeModuleFailed 表示合服模块执行失败。
	ErrMergeModuleFailed = errors.New("server group merge module failed")
)

const (
	// MergeModulePhaseGenDB 表示合服模块处于预生成数据库阶段。
	MergeModulePhaseGenDB = "gendb"
	// MergeModulePhaseMerge 表示合服模块处于正式合并阶段。
	MergeModulePhaseMerge = "merge"
	// MergeModulePhaseRollback 表示合服模块处于回滚阶段。
	MergeModulePhaseRollback = "rollback"
)

// MergeModuleFunc 是项目侧合服模块的统一执行函数。
type MergeModuleFunc func(context.Context, MergeModuleInput) (MergeModuleResult, error)

// MergeGenDBFunc 对应合服前的数据生成和冲突预检阶段。
type MergeGenDBFunc = MergeModuleFunc

// MergeApplyFunc 对应业务数据真正迁移或合并阶段。
type MergeApplyFunc = MergeModuleFunc

// MergeRollbackFunc 对应业务数据回滚或补偿阶段。
type MergeRollbackFunc = MergeModuleFunc

// MergeModuleSpec 描述一个项目侧合服模块，平台只保存通用契约，不保存具体业务表结构。
type MergeModuleSpec struct {
	// Name 是模块唯一名，建议使用 profile、mail、leaderboard 这类稳定业务域名。
	Name string `json:"name"`
	// Description 描述模块职责，主要用于报告和运营后台展示。
	Description string `json:"description,omitempty"`
	// Features 是模块影响的功能域，用于和合服请求里的 feature 对齐。
	Features []string `json:"features,omitempty"`
	// Required 表示该模块是项目合服的必跑模块，必须同时提供三段处理器。
	Required bool `json:"required"`
	// GenDB 负责合服前生成预检数据、冲突方案和 dry-run 证据。
	GenDB MergeGenDBFunc `json:"-"`
	// Merge 负责合服窗口内的真实业务数据合并。
	Merge MergeApplyFunc `json:"-"`
	// Rollback 负责合服失败后的业务回滚或补偿。
	Rollback MergeRollbackFunc `json:"-"`
}

// MergeModuleSnapshot 是注册器对外暴露的只读模块摘要。
type MergeModuleSnapshot struct {
	// Name 是模块唯一名。
	Name string `json:"name"`
	// Description 是模块职责说明。
	Description string `json:"description,omitempty"`
	// Features 是模块影响的功能域。
	Features []string `json:"features,omitempty"`
	// Required 表示模块是否为必跑模块。
	Required bool `json:"required"`
	// HasGenDB 表示是否注册了预检处理器。
	HasGenDB bool `json:"has_gendb"`
	// HasMerge 表示是否注册了合并处理器。
	HasMerge bool `json:"has_merge"`
	// HasRollback 表示是否注册了回滚处理器。
	HasRollback bool `json:"has_rollback"`
}

// MergeModuleInput 是传给项目侧合服模块的通用上下文快照。
type MergeModuleInput struct {
	// Request 是当前合服请求。
	Request MergeRequest `json:"request"`
	// Current 是执行前的平台路由计划快照。
	Current Plan `json:"current"`
	// DryRun 是平台 dry-run 结果，模块侧可以复用其中的回滚点和写检查。
	DryRun MergeDryRun `json:"dry_run"`
	// Steps 是平台迁移步骤快照。
	Steps []MigrationStep `json:"steps,omitempty"`
	// Findings 是前置计划已有的风险项，供后续阶段生成证据时参考。
	Findings []ConflictFinding `json:"findings,omitempty"`
	// Meta 是项目编排器传入的通用元数据。
	Meta map[string]string `json:"meta,omitempty"`
}

// MergeModuleResult 是项目侧模块执行后的通用回执。
type MergeModuleResult struct {
	// Module 是执行结果所属模块，空值会回填注册器里的模块名。
	Module string `json:"module"`
	// Phase 是执行阶段，取值为 gendb、merge 或 rollback。
	Phase string `json:"phase"`
	// OK 表示该模块阶段没有 blocker finding。
	OK bool `json:"ok"`
	// Skipped 表示模块与当前请求无关，不进入阶段报告。
	Skipped bool `json:"skipped,omitempty"`
	// Findings 是模块返回的冲突、风险或阻断项。
	Findings []ConflictFinding `json:"findings,omitempty"`
	// Evidence 是模块返回的证据路径、批次号或校验摘要。
	Evidence []string `json:"evidence,omitempty"`
	// Rollback 是模块建议的回滚动作或补偿步骤。
	Rollback []string `json:"rollback,omitempty"`
	// Meta 是模块返回的通用元数据。
	Meta map[string]string `json:"meta,omitempty"`
}

// MergeModuleRunReport 汇总一次模块级合服阶段执行结果，用于运营审计和上线证据归档。
type MergeModuleRunReport struct {
	// Phase 是本次报告对应的阶段，取值为 gendb、merge 或 rollback。
	Phase string `json:"phase"`
	// Request 是合服请求快照，便于证据脱离执行进程后仍能复核。
	Request MergeRequest `json:"request"`
	// GeneratedAt 是报告生成时间，统一保存为 UTC。
	GeneratedAt time.Time `json:"generated_at"`
	// OK 表示本阶段没有 blocker finding，业务错误应直接返回 error。
	OK bool `json:"ok"`
	// Results 保存每个模块的原始阶段回执。
	Results []MergeModuleResult `json:"results,omitempty"`
	// Findings 汇总所有模块返回的冲突或风险项。
	Findings []ConflictFinding `json:"findings,omitempty"`
	// Evidence 汇总所有模块返回的证据路径、批次号或校验摘要。
	Evidence []string `json:"evidence,omitempty"`
	// Rollback 汇总所有模块声明的回滚动作或补偿步骤。
	Rollback []string `json:"rollback,omitempty"`
	// Meta 保存项目编排器附加的通用元数据。
	Meta map[string]string `json:"meta,omitempty"`
}

// MergeModuleCoverageReport 描述合服请求 feature 与模块处理器之间的覆盖关系。
type MergeModuleCoverageReport struct {
	// Phase 是本次覆盖检查对应的阶段。
	Phase string `json:"phase"`
	// Request 是被检查的合服请求快照。
	Request MergeRequest `json:"request"`
	// OK 表示所有声明的 feature 都有对应阶段处理器覆盖。
	OK bool `json:"ok"`
	// Features 是本次请求声明需要检查或冻结的功能域。
	Features []string `json:"features,omitempty"`
	// Missing 是缺少模块处理器覆盖的功能域。
	Missing []string `json:"missing,omitempty"`
	// Modules 是注册器当前模块快照。
	Modules []MergeModuleSnapshot `json:"modules,omitempty"`
	// Findings 是缺失覆盖转换出的 blocker 风险项。
	Findings []ConflictFinding `json:"findings,omitempty"`
}

// MergeOpRegistry 把成熟项目里按模块注册 GenDB/Merge/Rollback 的经验抽成龙恒通用扩展点。
type MergeOpRegistry struct {
	mu      sync.RWMutex
	modules map[string]MergeModuleSpec
}

// NewMergeOpRegistry 创建模块注册器，并按注册顺序校验重复和必填处理器。
func NewMergeOpRegistry(specs ...MergeModuleSpec) (*MergeOpRegistry, error) {
	registry := &MergeOpRegistry{modules: make(map[string]MergeModuleSpec, len(specs))}
	for _, spec := range specs {
		if err := registry.Register(spec); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

// Register 注册一个项目侧合服模块，重复模块名会被拒绝。
func (r *MergeOpRegistry) Register(spec MergeModuleSpec) error {
	if r == nil {
		return fmt.Errorf("%w: registry is nil", ErrMergeModuleInvalid)
	}
	normalized, err := normMergeSpec(spec)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.modules[normalized.Name]; exists {
		return fmt.Errorf("%w: %s", ErrMergeModuleExists, normalized.Name)
	}
	r.modules[normalized.Name] = normalized
	return nil
}

// Snapshot 返回按模块名排序的注册器快照，调用方可以安全修改返回值。
func (r *MergeOpRegistry) Snapshot() []MergeModuleSnapshot {
	specs := r.snapshotSpecs()
	if len(specs) == 0 {
		return nil
	}
	out := make([]MergeModuleSnapshot, 0, len(specs))
	for _, spec := range specs {
		out = append(out, MergeModuleSnapshot{
			Name:        spec.Name,
			Description: spec.Description,
			Features:    append([]string(nil), spec.Features...),
			Required:    spec.Required,
			HasGenDB:    spec.GenDB != nil,
			HasMerge:    spec.Merge != nil,
			HasRollback: spec.Rollback != nil,
		})
	}
	return out
}

// BuildChecker 把注册器转换为合服迁移计划可用的冲突检查器。
func (r *MergeOpRegistry) BuildChecker() ConflictChecker {
	if r == nil {
		return nil
	}
	return ConflictCheckFunc(func(ctx context.Context, request ConflictCheckRequest) ([]ConflictFinding, error) {
		coverage, err := r.CheckCoverage(request.DryRun.Request, MergeModulePhaseGenDB)
		if err != nil {
			return nil, err
		}
		results, err := r.RunGenDB(ctx, MergeModuleInput{
			Request: request.DryRun.Request,
			Current: request.Current,
			DryRun:  request.DryRun,
			Steps:   request.Steps,
		})
		if err != nil {
			return nil, err
		}
		findings := cloneClashFindings(coverage.Findings)
		for _, result := range results {
			findings = append(findings, result.Findings...)
		}
		return findings, nil
	})
}

// CheckCoverage 检查合服请求声明的 feature 是否有对应阶段模块处理器覆盖。
func (r *MergeOpRegistry) CheckCoverage(request MergeRequest, phase string) (MergeModuleCoverageReport, error) {
	phase = normalizeID(phase)
	if !validMergePhase(phase) {
		return MergeModuleCoverageReport{}, fmt.Errorf("%w: invalid coverage phase %s", ErrMergeModuleInvalid, phase)
	}
	request = normMergeReq(request)
	specs := r.snapshotSpecs()
	features := requestedFeatures(request)
	report := MergeModuleCoverageReport{
		Phase:    phase,
		Request:  request,
		OK:       true,
		Features: features,
		Modules:  snapshotsFromSpecs(specs),
	}
	if len(features) == 0 || !hasFeatureDecls(specs) {
		return report, nil
	}
	for _, feature := range features {
		if mergeFeatureCovered(specs, phase, feature) {
			continue
		}
		report.Missing = append(report.Missing, feature)
		report.Findings = append(report.Findings, ConflictFinding{
			Code:     "merge_module_feature_missing",
			Severity: MigrationSeverityBlocker,
			Subject:  feature,
			Detail:   fmt.Sprintf("合服 feature %s 缺少 %s 阶段模块处理器", feature, phase),
		})
	}
	report.Missing = normalizeIDs(report.Missing)
	report.Findings = cloneClashFindings(report.Findings)
	report.OK = len(report.Missing) == 0
	return report, nil
}

// CoverageFindings 返回覆盖缺口对应的 blocker findings，便于接入自定义计划检查器。
func (r *MergeOpRegistry) CoverageFindings(request MergeRequest, phase string) ([]ConflictFinding, error) {
	report, err := r.CheckCoverage(request, phase)
	if err != nil {
		return nil, err
	}
	return cloneClashFindings(report.Findings), nil
}

// RunGenDB 执行所有已注册的合服前数据生成和冲突预检模块。
func (r *MergeOpRegistry) RunGenDB(ctx context.Context, input MergeModuleInput) ([]MergeModuleResult, error) {
	return r.runPhase(ctx, MergeModulePhaseGenDB, input)
}

// RunGenDBReport 执行合服前数据生成和冲突预检，并生成可归档报告。
func (r *MergeOpRegistry) RunGenDBReport(ctx context.Context, input MergeModuleInput) (MergeModuleRunReport, error) {
	return r.runPhaseReport(ctx, MergeModulePhaseGenDB, input)
}

// RunMerge 执行所有已注册的业务合并模块。
func (r *MergeOpRegistry) RunMerge(ctx context.Context, input MergeModuleInput) ([]MergeModuleResult, error) {
	return r.runPhase(ctx, MergeModulePhaseMerge, input)
}

// RunMergeReport 执行业务合并模块，并生成可归档报告。
func (r *MergeOpRegistry) RunMergeReport(ctx context.Context, input MergeModuleInput) (MergeModuleRunReport, error) {
	return r.runPhaseReport(ctx, MergeModulePhaseMerge, input)
}

// RunRollback 执行所有已注册的业务回滚模块。
func (r *MergeOpRegistry) RunRollback(ctx context.Context, input MergeModuleInput) ([]MergeModuleResult, error) {
	return r.runPhase(ctx, MergeModulePhaseRollback, input)
}

// RunRollbackReport 执行业务回滚模块，并生成可归档报告。
func (r *MergeOpRegistry) RunRollbackReport(ctx context.Context, input MergeModuleInput) (MergeModuleRunReport, error) {
	return r.runPhaseReport(ctx, MergeModulePhaseRollback, input)
}

// MergeModuleInputFromPlan 根据平台迁移计划生成项目侧模块输入，便于业务编排器复用同一份 dry-run 证据。
func MergeModuleInputFromPlan(current Plan, plan MigrationPlan) MergeModuleInput {
	return MergeModuleInput{
		Request:  plan.Request,
		Current:  current,
		DryRun:   plan.DryRun,
		Steps:    plan.Steps,
		Findings: plan.Findings,
	}
}

func (r *MergeOpRegistry) runPhase(ctx context.Context, phase string, input MergeModuleInput) ([]MergeModuleResult, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	phase = normalizeID(phase)
	if !validMergePhase(phase) {
		return nil, fmt.Errorf("%w: invalid phase %s", ErrMergeModuleInvalid, phase)
	}
	specs := r.snapshotSpecs()
	if len(specs) == 0 {
		return nil, nil
	}
	input = cloneMergeInput(input)
	results := make([]MergeModuleResult, 0, len(specs))
	for _, spec := range specs {
		if err := ctxErr(ctx); err != nil {
			return results, err
		}
		fn := mergeFuncForPhase(spec, phase)
		if fn == nil {
			continue
		}
		result, err := fn(ctx, cloneMergeInput(input))
		if err != nil {
			return results, fmt.Errorf("%w: %s %s: %w", ErrMergeModuleFailed, spec.Name, phase, err)
		}
		normalized, err := normMergeResult(spec.Name, phase, result)
		if err != nil {
			return results, err
		}
		if normalized.Skipped {
			continue
		}
		results = append(results, normalized)
	}
	return results, nil
}

func (r *MergeOpRegistry) runPhaseReport(ctx context.Context, phase string, input MergeModuleInput) (MergeModuleRunReport, error) {
	coverage, err := r.CheckCoverage(mergeInputReq(input), phase)
	if err != nil {
		return MergeModuleRunReport{}, err
	}
	results, err := r.runPhase(ctx, phase, input)
	if err != nil {
		return MergeModuleRunReport{}, err
	}
	report, err := newMergeRunReport(phase, input, results, time.Now().UTC())
	if err != nil {
		return MergeModuleRunReport{}, err
	}
	report.Findings = append(cloneClashFindings(coverage.Findings), report.Findings...)
	if !coverage.OK {
		report.OK = false
	}
	return report, nil
}

func (r *MergeOpRegistry) snapshotSpecs() []MergeModuleSpec {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.modules) == 0 {
		return nil
	}
	names := make([]string, 0, len(r.modules))
	for name := range r.modules {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]MergeModuleSpec, 0, len(names))
	for _, name := range names {
		out = append(out, cloneMergeModuleSpec(r.modules[name]))
	}
	return out
}

func normMergeSpec(spec MergeModuleSpec) (MergeModuleSpec, error) {
	spec.Name = normalizeID(spec.Name)
	spec.Description = firstNonEmpty(spec.Description)
	spec.Features = normalizeIDs(spec.Features)
	if spec.Name == "" {
		return MergeModuleSpec{}, fmt.Errorf("%w: module name is required", ErrMergeModuleInvalid)
	}
	if spec.GenDB == nil && spec.Merge == nil && spec.Rollback == nil {
		return MergeModuleSpec{}, fmt.Errorf("%w: module %s has no handlers", ErrMergeModuleInvalid, spec.Name)
	}
	if spec.Required && (spec.GenDB == nil || spec.Merge == nil || spec.Rollback == nil) {
		return MergeModuleSpec{}, fmt.Errorf("%w: required module %s must provide gendb, merge and rollback handlers", ErrMergeModuleInvalid, spec.Name)
	}
	return spec, nil
}

func snapshotsFromSpecs(specs []MergeModuleSpec) []MergeModuleSnapshot {
	if len(specs) == 0 {
		return nil
	}
	out := make([]MergeModuleSnapshot, 0, len(specs))
	for _, spec := range specs {
		out = append(out, MergeModuleSnapshot{
			Name:        spec.Name,
			Description: spec.Description,
			Features:    append([]string(nil), spec.Features...),
			Required:    spec.Required,
			HasGenDB:    spec.GenDB != nil,
			HasMerge:    spec.Merge != nil,
			HasRollback: spec.Rollback != nil,
		})
	}
	return out
}

func cloneMergeModuleSpec(spec MergeModuleSpec) MergeModuleSpec {
	spec.Features = append([]string(nil), spec.Features...)
	return spec
}

func mergeFuncForPhase(spec MergeModuleSpec, phase string) MergeModuleFunc {
	switch phase {
	case MergeModulePhaseGenDB:
		return spec.GenDB
	case MergeModulePhaseMerge:
		return spec.Merge
	case MergeModulePhaseRollback:
		return spec.Rollback
	default:
		return nil
	}
}

func validMergePhase(phase string) bool {
	switch phase {
	case MergeModulePhaseGenDB, MergeModulePhaseMerge, MergeModulePhaseRollback:
		return true
	default:
		return false
	}
}

func requestedFeatures(request MergeRequest) []string {
	request = normMergeReq(request)
	features := append([]string(nil), request.CheckFeatures...)
	features = append(features, request.BlockFeatures...)
	return normalizeIDs(features)
}

func hasFeatureDecls(specs []MergeModuleSpec) bool {
	for _, spec := range specs {
		if len(spec.Features) > 0 {
			return true
		}
	}
	return false
}

func mergeFeatureCovered(specs []MergeModuleSpec, phase, feature string) bool {
	feature = normalizeID(feature)
	for _, spec := range specs {
		if len(spec.Features) == 0 || !containsID(spec.Features, feature) {
			continue
		}
		if mergeFuncForPhase(spec, phase) != nil {
			return true
		}
	}
	return false
}

func mergeInputReq(input MergeModuleInput) MergeRequest {
	request := normMergeReq(input.Request)
	if request.ID != "" {
		return request
	}
	return normMergeReq(input.DryRun.Request)
}

func cloneMergeInput(input MergeModuleInput) MergeModuleInput {
	return MergeModuleInput{
		Request:  mergeInputReq(input),
		Current:  clonePlan(input.Current),
		DryRun:   cloneMergeDryRun(input.DryRun),
		Steps:    cloneMigrationSteps(input.Steps),
		Findings: cloneClashFindings(input.Findings),
		Meta:     normalizeStringMap(input.Meta),
	}
}

func normMergeResult(module, phase string, result MergeModuleResult) (MergeModuleResult, error) {
	result.Module = firstNonEmpty(result.Module, module)
	result.Phase = firstNonEmpty(result.Phase, phase)
	result.Evidence = cloneMergeStrings(result.Evidence)
	result.Rollback = cloneMergeStrings(result.Rollback)
	result.Meta = normalizeStringMap(result.Meta)
	if result.Module == "" || result.Phase == "" {
		return MergeModuleResult{}, fmt.Errorf("%w: result module and phase are required", ErrMergeModuleInvalid)
	}
	if !validMergePhase(result.Phase) {
		return MergeModuleResult{}, fmt.Errorf("%w: result %s has invalid phase %s", ErrMergeModuleInvalid, result.Module, result.Phase)
	}
	findings, err := normalizeFindings(result.Module, result.Findings)
	if err != nil {
		return MergeModuleResult{}, err
	}
	result.Findings = findings
	result.OK = !hasBlockerFinding(result.Findings)
	return result, nil
}

func newMergeRunReport(phase string, input MergeModuleInput, results []MergeModuleResult, generatedAt time.Time) (MergeModuleRunReport, error) {
	phase = normalizeID(phase)
	if !validMergePhase(phase) {
		return MergeModuleRunReport{}, fmt.Errorf("%w: invalid report phase %s", ErrMergeModuleInvalid, phase)
	}
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	} else {
		generatedAt = generatedAt.UTC()
	}
	input = cloneMergeInput(input)
	results = cloneMergeResults(results)
	report := MergeModuleRunReport{
		Phase:       phase,
		Request:     input.Request,
		GeneratedAt: generatedAt,
		OK:          true,
		Results:     results,
		Meta:        cloneStringMap(input.Meta),
	}
	for _, result := range results {
		report.Findings = append(report.Findings, result.Findings...)
		report.Evidence = append(report.Evidence, result.Evidence...)
		report.Rollback = append(report.Rollback, result.Rollback...)
		if !result.OK {
			report.OK = false
		}
	}
	report.Findings = cloneClashFindings(report.Findings)
	report.Evidence = cloneMergeStrings(report.Evidence)
	report.Rollback = cloneMergeStrings(report.Rollback)
	if hasBlockerFinding(report.Findings) {
		report.OK = false
	}
	return report, nil
}

func normalizeFindings(module string, findings []ConflictFinding) ([]ConflictFinding, error) {
	if len(findings) == 0 {
		return nil, nil
	}
	out := make([]ConflictFinding, 0, len(findings))
	for _, finding := range findings {
		if finding.Subject == "" {
			finding.Subject = module
		}
		normalized, err := normConflict(finding)
		if err != nil {
			return nil, err
		}
		out = append(out, normalized)
	}
	return out, nil
}

func cloneMergeResults(results []MergeModuleResult) []MergeModuleResult {
	if len(results) == 0 {
		return nil
	}
	out := make([]MergeModuleResult, len(results))
	for i, result := range results {
		out[i] = MergeModuleResult{
			Module:   result.Module,
			Phase:    result.Phase,
			OK:       result.OK,
			Skipped:  result.Skipped,
			Findings: cloneClashFindings(result.Findings),
			Evidence: cloneMergeStrings(result.Evidence),
			Rollback: cloneMergeStrings(result.Rollback),
			Meta:     cloneStringMap(result.Meta),
		}
	}
	return out
}

func cloneMergeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = firstNonEmpty(value)
		if value != "" {
			out = append(out, value)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
