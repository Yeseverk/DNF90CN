// Package runtimeguard 提供"多节点部署是否使用了进程内内存后端"这一类启动期断言。
//
// 历史背景：matchmaker、party、crossserver、aoi、framesync 等 runtime 模块的默认实现是内存版，
// 仅在单节点演示和测试场景下正确。生产部署一旦超过一个 logic 节点，内存后端会
// 静默产生分裂脑（同一 user 被两个节点分别匹配成功、队伍状态读到不同副本等）。
// 框架历史上只在源码注释里提示"多节点请改用 Redis 版本"，没有任何启动期校验。
//
// 本包暴露 Backend 枚举、BackendDescriber 接口和 Check / MustCheck 入口，让各
// runtime 模块声明自己当前的后端类型；wiring 层可在启动时统一调用 Check，对
// 多节点 Profile 下仍然挂着内存后端的模块直接 fail-fast。
package runtimeguard

import (
	"errors"
	"fmt"
	"strings"
)

// Backend 标识一个 runtime 模块当前实际使用的存储后端。
type Backend string

const (
	// BackendMemory 表示纯进程内 map / sync.Mutex 实现，仅单节点安全。
	BackendMemory Backend = "memory"
	// BackendRedis 表示通过 Redis Lua / Hash / Sorted Set 维护跨节点一致性的实现。
	BackendRedis Backend = "redis"
	// BackendMySQL 表示通过 MySQL 事务 + 唯一索引承载权威状态的实现。
	BackendMySQL Backend = "mysql"
	// BackendHybrid 表示同时使用多种后端（如 MySQL 权威 + Redis 缓存）的复合实现。
	BackendHybrid Backend = "hybrid"
	// BackendUnknown 表示模块未声明后端类型，等价于内存级的不安全实现。
	BackendUnknown Backend = ""
)

// IsDistributed 返回 true 表示该后端在多节点部署下是安全的（跨节点一致）。
func (b Backend) IsDistributed() bool {
	switch b {
	case BackendRedis, BackendMySQL, BackendHybrid:
		return true
	default:
		return false
	}
}

// BackendDescriber 是一个 runtime 模块需要选择性实现的小接口；
// 任何 Manager / RedisManager 都可以通过它告诉 wiring 层自己的后端类型。
// 故意做成可选实现而非塞进 Runtime 接口，避免影响下游已有实现。
type BackendDescriber interface {
	BackendKind() Backend
}

// Profile 描述部署形态。Distributed=true 表示存在多个 logic / scene / social
// 节点同时承担同一 runtime 模块的写入。
type Profile struct {
	// Distributed 为 true 时本包会拒绝任何非分布式安全后端。
	Distributed bool
	// AllowMemoryModules 列出仍允许使用内存后端的模块名。常用于灰度上线、
	// 单元测试或显式接受"该模块在多节点下功能降级"的项目决策。
	// 列表中的名字与 Check 的 module 参数完全匹配（区分大小写）。
	AllowMemoryModules []string
}

func (p Profile) allowsMemoryFor(module string) bool {
	module = strings.ToLower(strings.TrimSpace(module))
	for _, name := range p.AllowMemoryModules {
		if strings.ToLower(strings.TrimSpace(name)) == module {
			return true
		}
	}
	return false
}

// ErrMemoryDistProfile 表示在多节点 Profile 下检测到了内存后端。
// 通过 errors.Is 可以稳定识别。
var ErrMemoryDistProfile = errors.New("runtimeguard: memory backend rejected by distributed profile")

// Check 在多节点 Profile 下拒绝内存 / 未知后端；其余情况返回 nil。
// module 用于错误消息和 AllowMemoryModules 白名单匹配。
func Check(module string, backend Backend, profile Profile) error {
	module = strings.TrimSpace(module)
	if module == "" {
		return fmt.Errorf("runtimeguard: module name is required")
	}
	if !profile.Distributed {
		return nil
	}
	if backend.IsDistributed() {
		return nil
	}
	if profile.allowsMemoryFor(module) {
		return nil
	}
	kind := backend
	if kind == BackendUnknown {
		kind = BackendMemory
	}
	return fmt.Errorf("%w: module %q uses backend %q which is single-node only",
		ErrMemoryDistProfile, module, kind)
}

// CheckDescriber 是 Check 的便捷重载：如果 describer 为 nil 视为未知后端。
func CheckDescriber(module string, describer BackendDescriber, profile Profile) error {
	var backend Backend
	if describer != nil {
		backend = describer.BackendKind()
	}
	return Check(module, backend, profile)
}

// MustCheck 行为同 Check，但在检测失败时 panic。仅供启动早期、且失败即应停机的 wiring 路径使用。
func MustCheck(module string, backend Backend, profile Profile) {
	if err := Check(module, backend, profile); err != nil {
		panic(err)
	}
}
