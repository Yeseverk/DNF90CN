package runtimeguard

import (
	"errors"
	"sort"
	"strings"
	"sync"
)

// 历史背景：本包之前只暴露 Check / CheckDescriber 这一对无状态函数；wiring 层
// 要为每一个 runtime 模块（matchmaker、party、crossserver、未来还会更多）手动
// 调用一次，谁漏调用谁就静默逃过门禁。
//
// 全局清单把"声明 — 校验"分成两步：
//   1. 各 runtime 模块构造完成后立即调用 Register / RegisterDescriber，
//      把模块名 + 后端类型挂进进程内全局清单。
//   2. wiring 收尾前调用 Verify(profile) 一次性核对所有声明，违规项被合并
//      成一个 multi-error 抛出，main 看到 error != nil 直接停机。
//
// 这样的好处：
//   - 加新的 runtime 模块不再需要 wiring 代码同步加一行 Check；
//   - 多个违规会一次性报出来，而不是修一条复活一条；
//   - 单元测试可以用 Reset 清空全局状态，避免污染。

// Registration 记录一次注册。Source 是可选的调用方标识（比如 main 的 file:line），
// 主要用来在 Verify 失败时给 ops 一个"到底是谁声明的"的线索。
type Registration struct {
	Module  string
	Backend Backend
	Source  string
}

type Catalog struct {
	mu      sync.RWMutex
	entries []Registration
}

func NewCatalog() *Catalog {
	return &Catalog{}
}

var defaultCatalog = NewCatalog()

// Register 把 module + backend 加进全局清单。生产代码调用，参数静态可见。
// 重复注册（同 module）允许，最后一个生效；这样在 hot-reload 或重新装配场景
// 下不会泄漏过时声明。
func Register(module string, backend Backend) {
	defaultCatalog.Register(module, backend)
}

// RegisterDescriber 是 Register 的便捷形式。describer 为 nil 视为 BackendUnknown。
func RegisterDescriber(module string, describer BackendDescriber) {
	defaultCatalog.RegisterDescriber(module, describer)
}

func (inv *Catalog) Register(module string, backend Backend) {
	if inv == nil {
		return
	}
	inv.registerInternal(Registration{Module: normalizeModule(module), Backend: backend})
}

func (inv *Catalog) RegisterDescriber(module string, describer BackendDescriber) {
	var b Backend
	if describer != nil {
		b = describer.BackendKind()
	}
	inv.Register(module, b)
}

// RegisterFromSource 与 Register 相同，但额外记录 caller 信息。
// 推荐 wiring 层使用 "package.Function" 或文件路径作为 source。
func RegisterFromSource(module string, backend Backend, source string) {
	defaultCatalog.RegisterFromSource(module, backend, source)
}

func (inv *Catalog) RegisterFromSource(module string, backend Backend, source string) {
	if inv == nil {
		return
	}
	inv.registerInternal(Registration{
		Module:  normalizeModule(module),
		Backend: backend,
		Source:  strings.TrimSpace(source),
	})
}

func (inv *Catalog) registerInternal(reg Registration) {
	if reg.Module == "" {
		return
	}
	inv.mu.Lock()
	defer inv.mu.Unlock()
	for i := range inv.entries {
		if inv.entries[i].Module == reg.Module {
			inv.entries[i] = reg
			return
		}
	}
	inv.entries = append(inv.entries, reg)
}

// Snapshot 返回当前清单的副本，按 module 名排序，便于测试和 admin 报告。
func Snapshot() []Registration {
	return defaultCatalog.Snapshot()
}

func (inv *Catalog) Snapshot() []Registration {
	if inv == nil {
		return nil
	}
	inv.mu.RLock()
	out := make([]Registration, len(inv.entries))
	copy(out, inv.entries)
	inv.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Module < out[j].Module })
	return out
}

// Verify 对清单里每条注册都跑一次 Check。所有违规聚合成 errors.Join，
// 调用方可以用 errors.Is(err, ErrMemoryDistProfile) 判断是否有
// 至少一个内存后端被拒。
//
// 调用方应在装配完成、http server / bus subscribe 启动之前调用一次。
func Verify(profile Profile) error {
	return defaultCatalog.Verify(profile)
}

func (inv *Catalog) Verify(profile Profile) error {
	snapshot := inv.Snapshot()
	var errs []error
	for _, reg := range snapshot {
		if err := Check(reg.Module, reg.Backend, profile); err != nil {
			if reg.Source != "" {
				err = wrapWithSource(err, reg.Source)
			}
			errs = append(errs, err)
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}

// Reset 清空清单。仅用于测试 / hot-reload。生产代码不应该调用。
func Reset() {
	defaultCatalog.Reset()
}

func (inv *Catalog) Reset() {
	if inv == nil {
		return
	}
	inv.mu.Lock()
	inv.entries = nil
	inv.mu.Unlock()
}

// Has 返回 module 是否已注册。便于测试断言。
func Has(module string) bool {
	return defaultCatalog.Has(module)
}

func (inv *Catalog) Has(module string) bool {
	if inv == nil {
		return false
	}
	module = normalizeModule(module)
	inv.mu.RLock()
	defer inv.mu.RUnlock()
	for _, r := range inv.entries {
		if r.Module == module {
			return true
		}
	}
	return false
}

func normalizeModule(name string) string {
	return strings.TrimSpace(name)
}

// sourceWrappedError 让 errors.Is 仍能命中底层 sentinel，同时携带 Source。
type sourceWrappedError struct {
	inner  error
	source string
}

func (e *sourceWrappedError) Error() string {
	if e.source == "" {
		return e.inner.Error()
	}
	return e.inner.Error() + " (registered at " + e.source + ")"
}

func (e *sourceWrappedError) Unwrap() error { return e.inner }

func wrapWithSource(inner error, source string) error {
	source = strings.TrimSpace(source)
	if inner == nil || source == "" {
		return inner
	}
	return &sourceWrappedError{inner: inner, source: source}
}
