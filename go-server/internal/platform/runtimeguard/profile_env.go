package runtimeguard

import "strings"

// 生产类环境集合：与 SECURITY.md / config validation 中的"生产基线"对齐。
// 任何一项命中即视为多节点 Profile。命中条件做成显式列表（而不是"非 dev 即生产"），
// 避免出现"prod-canary" 这种新词被悄悄当成 dev 的情况——这种情形应该明确加进来。
var prodEnvTokens = []string{
	"prod",
	"production",
	"staging",
	"stage",
	"preprod",
	"pre-production",
	"k8s",
	"kubernetes",
	"helm",
	"cluster",
	"live",
}

// IsProductionEnvironment 判断给定的环境名是否落在"生产类"集合内。匹配规则：
// 字符串包含 prodEnvTokens 中任意一个 token（不区分大小写）。
// 例如 "prod-canary"、"k8s-staging"、"helm-cluster-2" 都视为生产类。
func IsProductionEnvironment(env string) bool {
	env = strings.ToLower(strings.TrimSpace(env))
	if env == "" {
		return false
	}
	for _, token := range prodEnvTokens {
		if strings.Contains(env, token) {
			return true
		}
	}
	return false
}

// ProfileFromEnvironment 从环境名 + topology 开关推导出一个默认 Profile。
// 规则：
//   - 命中生产类环境名 -> Distributed=true（即使 topology 关着，因为多 logic 实例
//     是生产部署的常态，要求项目显式声明"这不是多节点"才允许 memory 后端）
//   - topology 显式开启 -> Distributed=true
//   - 否则 Distributed=false（local / dev / smoke 默认放行内存后端）
//
// 项目侧可以拿到这个 Profile 之后再调 WithAllowMemory 把已知"暂时不能上分布式后端"
// 的模块加入白名单。
func ProfileFromEnvironment(env string, topologyEnabled bool) Profile {
	distributed := topologyEnabled || IsProductionEnvironment(env)
	return Profile{Distributed: distributed}
}

// ProfileFromEnvironmentAndBackends 从环境名、topology 开关和实际存储后端共同推导 Profile。
//
// 这个入口用于项目侧 runtime wiring。不要只因为 service.environment 写成 local
// 就把共享 runtime 当成单节点安全：如果 Profile / guild / chat 等 owner 已经接入
// MySQL、Redis 或 hybrid 后端，说明这条链路正在使用跨进程权威/缓存，启动期也应该
// 按分布式 Profile 检查内存 runtime。
func ProfileFromEnvironmentAndBackends(env string, topologyEnabled bool, backends ...Backend) Profile {
	distributed := topologyEnabled || IsProductionEnvironment(env)
	for _, backend := range backends {
		if backend.IsDistributed() {
			distributed = true
			break
		}
	}
	return Profile{Distributed: distributed}
}

// BackendFromStoreKind 把通用配置里的 store_kind / kind 字段映射为 runtimeguard 后端。
// 项目侧在接入自定义 owner 时可以复用它，避免散落的字符串判断只看 environment。
func BackendFromStoreKind(kind string) Backend {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "redis", "pika", "profiledb", "profile_db":
		return BackendRedis
	case "mysql":
		return BackendMySQL
	case "mysql_redis", "redis_mysql", "hybrid":
		return BackendHybrid
	case "memory", "file", "json", "":
		return BackendMemory
	default:
		return BackendUnknown
	}
}

// WithAllowMemory 是 Profile 链式构造的便捷方法：返回一份拷贝，附加 module 到
// AllowMemoryModules 白名单。
func (p Profile) WithAllowMemory(modules ...string) Profile {
	if len(modules) == 0 {
		return p
	}
	out := Profile{
		Distributed:        p.Distributed,
		AllowMemoryModules: make([]string, 0, len(p.AllowMemoryModules)+len(modules)),
	}
	out.AllowMemoryModules = append(out.AllowMemoryModules, p.AllowMemoryModules...)
	for _, m := range modules {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		if containsString(out.AllowMemoryModules, m) {
			continue
		}
		out.AllowMemoryModules = append(out.AllowMemoryModules, m)
	}
	return out
}

func containsString(list []string, target string) bool {
	for _, item := range list {
		if item == target {
			return true
		}
	}
	return false
}
