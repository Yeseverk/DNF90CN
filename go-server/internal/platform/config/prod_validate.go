package config

import (
	"fmt"
	"os"
	"reflect"
	"strings"
)

func validateProdSafety(errs *[]error, cfg ServiceConfig) {
	// 生产类环境不能只看 environment=prod，k8s/helm/staging 也按上线标准 fail-fast。
	validEnvPlaceholders(errs, reflect.ValueOf(cfg), "")
	rejectProdDefault(errs, "cluster.id", cfg.Cluster.ID)
	rejectProdDefault(errs, "cluster.version", cfg.Cluster.Version)
	rejectProdDefault(errs, "cluster.data_version", cfg.Cluster.DataVersion)
	rejectKnownSecret(errs, "admin.token", cfg.Admin.Token)
	if len(cfg.Admin.Tokens) == 0 {
		*errs = append(*errs, fmt.Errorf("admin.tokens requires at least one scoped token for production-like environments"))
	}
	if !cfg.Metrics.RequireAdminToken {
		*errs = append(*errs, fmt.Errorf("metrics.require_admin_token is required for production-like environments"))
	}
	if cfg.HotUpdate.Enabled && !cfg.HotUpdate.RequireSignature {
		*errs = append(*errs, fmt.Errorf("hot_update.require_signature is required for production-like environments"))
	}
	if cfg.Bus.Kind == "" || cfg.Bus.Kind == "memory" {
		*errs = append(*errs, fmt.Errorf("bus.kind memory is not allowed for production-like environments"))
	}
	if cfg.Cache.Kind == "" || cfg.Cache.Kind == "memory" {
		*errs = append(*errs, fmt.Errorf("cache.kind memory is not allowed for production-like environments"))
	}
	if cfg.Lock.Kind == "" || cfg.Lock.Kind == "memory" {
		*errs = append(*errs, fmt.Errorf("lock.kind memory is not allowed for production-like environments"))
	}
	if cfg.Registry.Kind == "" || cfg.Registry.Kind == "memory" {
		*errs = append(*errs, fmt.Errorf("registry.kind memory is not allowed for production-like environments"))
	}
	if !cfg.Tracing.Enabled && !cfg.OTel.Enabled {
		*errs = append(*errs, fmt.Errorf("tracing.enabled or otel.enabled is required for production-like environments"))
	}
	if cfg.Audit.Kind != "file" {
		*errs = append(*errs, fmt.Errorf("audit.kind file is required for production-like environments"))
	}
	if sdkInsecureEnv() {
		*errs = append(*errs, fmt.Errorf("LONGHENG_SDK_INSECURE must not be enabled for production-like environments"))
	}
	for idx, token := range cfg.Admin.Tokens {
		rejectKnownSecret(errs, fmt.Sprintf("admin.tokens[%d].token", idx), token.Token)
	}
	if isGatewayService(cfg.Service.Name) {
		// 多 gateway 必须共享在线状态，否则跨网关推送、踢人和重连恢复都会失真。
		if cfg.Presence.Kind != "redis" {
			*errs = append(*errs, fmt.Errorf("presence.kind redis is required for production gateway services"))
		}
		if !cfg.Gateway.ConnectionRateLimitEnabled {
			*errs = append(*errs, fmt.Errorf("gateway.connection_rate_limit_enabled is required for production gateway services"))
		}
		if cfg.Gateway.AuthToken != "" {
			rejectKnownSecret(errs, "gateway.auth_token", cfg.Gateway.AuthToken)
		}
		if cfg.Gateway.LoginToken != "" {
			rejectKnownSecret(errs, "gateway.login_token", cfg.Gateway.LoginToken)
		}
		requireProdSecret(errs, "gateway.session_key_master_secret", cfg.Gateway.SessionKeyMasterSecret)
	}
	if cfg.Idempotency.Kind == "redis" || cfg.Idempotency.Kind == "mysql_redis" {
		requireProdSecret(errs, "idempotency.redis_password", cfg.Idempotency.RedisPassword)
	}
	if cfg.Registry.Kind == "redis" {
		requireProdSecret(errs, "registry.redis_password", cfg.Registry.RedisPassword)
	}
	if cfg.Bus.Kind == "redis" {
		requireProdSecret(errs, "bus.redis_password", cfg.Bus.RedisPassword)
		if !cfg.Bus.RedisTLSEnabled {
			*errs = append(*errs, fmt.Errorf("bus.redis_tls_enabled is required for production-like redis bus"))
		}
	}
	if cfg.Bus.Kind == "nats" {
		if strings.TrimSpace(cfg.Bus.NATSToken) == "" && strings.TrimSpace(cfg.Bus.NATSCredentials) == "" &&
			(strings.TrimSpace(cfg.Bus.NATSUsername) == "" || strings.TrimSpace(cfg.Bus.NATSPassword) == "") {
			*errs = append(*errs, fmt.Errorf("bus nats requires token, credentials, or username/password for production-like environments"))
		}
		if strings.TrimSpace(cfg.Bus.NATSToken) != "" {
			rejectKnownSecret(errs, "bus.nats_token", cfg.Bus.NATSToken)
		}
		if strings.TrimSpace(cfg.Bus.NATSPassword) != "" {
			rejectKnownSecret(errs, "bus.nats_password", cfg.Bus.NATSPassword)
		}
		if !cfg.Bus.NATSTLSEnabled && strings.TrimSpace(cfg.Bus.NATSCAFile) == "" && strings.TrimSpace(cfg.Bus.NATSCertFile) == "" {
			*errs = append(*errs, fmt.Errorf("bus.nats_tls_enabled or nats CA/client cert is required for production-like nats bus"))
		}
	}
	if cfg.Cache.Kind == "redis" {
		requireProdSecret(errs, "cache.redis_password", cfg.Cache.RedisPassword)
	}
	if cfg.Lock.Kind == "redis" {
		requireProdSecret(errs, "lock.redis_password", cfg.Lock.RedisPassword)
	}
	if cfg.Presence.Kind == "redis" {
		requireProdSecret(errs, "presence.redis_password", cfg.Presence.RedisPassword)
	}
	if cfg.ProfileStore.StoreKind == "redis" || cfg.ProfileStore.StoreKind == "pika" || cfg.ProfileStore.StoreKind == "profiledb" || cfg.ProfileStore.StoreKind == "profile_db" || cfg.ProfileStore.StoreKind == "mysql_redis" {
		requireProdSecret(errs, "profile_store.store_password", cfg.ProfileStore.StorePassword)
	}
	if cfg.ProfileStore.SummaryStoreKind == "redis" || cfg.ProfileStore.SummaryStoreKind == "pika" {
		requireProdSecret(errs, "profile_store.summary_store_password", cfg.ProfileStore.SummaryStorePassword)
	}
	if isProfileCriticalSvc(cfg.Service.Name) {
		// logic/playerd 是 Profile 权威写路径，生产不能退回文件、内存或纯 Redis 存档。
		switch cfg.ProfileStore.StoreKind {
		case "mysql", "mysql_redis":
		default:
			*errs = append(*errs, fmt.Errorf("profile_store.store_kind mysql or mysql_redis is required for production %s services", cfg.Service.Name))
		}
		switch cfg.ProfileStore.SummaryStoreKind {
		case "redis", "pika":
		default:
			*errs = append(*errs, fmt.Errorf("profile_store.summary_store_kind redis or pika is required for production %s services", cfg.Service.Name))
		}
	}
}

func sdkInsecureEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("LONGHENG_SDK_INSECURE"))) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func validEnvPlaceholders(errs *[]error, value reflect.Value, path string) {
	if !value.IsValid() {
		return
	}
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return
		}
		validEnvPlaceholders(errs, value.Elem(), path)
		return
	}
	switch value.Kind() {
	case reflect.Struct:
		valueType := value.Type()
		for idx := 0; idx < value.NumField(); idx++ {
			fieldType := valueType.Field(idx)
			if fieldType.PkgPath != "" {
				continue
			}
			fieldPath := joinConfigPath(path, tomlFieldName(fieldType))
			validEnvPlaceholders(errs, value.Field(idx), fieldPath)
		}
	case reflect.Slice, reflect.Array:
		for idx := 0; idx < value.Len(); idx++ {
			validEnvPlaceholders(errs, value.Index(idx), fmt.Sprintf("%s[%d]", path, idx))
		}
	case reflect.String:
		text := value.String()
		if configEnvPattern.MatchString(text) || isShellEnvOnly(text) {
			// 未解析占位符比空值更危险：看起来非空，实际会把字面量当密钥或地址启动。
			*errs = append(*errs, fmt.Errorf("%s contains unresolved environment variable placeholder", path))
		}
	}
}

func tomlFieldName(field reflect.StructField) string {
	tag := field.Tag.Get("toml")
	if tag == "" || tag == "-" {
		return strings.ToLower(field.Name)
	}
	name := strings.Split(tag, ",")[0]
	if name == "" {
		return strings.ToLower(field.Name)
	}
	return name
}

func joinConfigPath(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}

func isShellEnvOnly(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) < 2 || value[0] != '$' {
		return false
	}
	for idx, r := range value[1:] {
		if idx == 0 {
			if r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				continue
			}
			return false
		}
		if r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

func requireProdSecret(errs *[]error, field, value string) {
	requireString(errs, field, value)
	if strings.TrimSpace(value) == "" {
		return
	}
	// 非空还不够，常见本地默认值也要直接拒绝。
	rejectKnownSecret(errs, field, value)
}

func rejectKnownSecret(errs *[]error, field, value string) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "local-admin-token",
		"local-login-token",
		"local-auth-token",
		"local-session-key-master-secret",
		"local-token",
		"admin",
		"token",
		"password",
		"redis",
		"mysql",
		"changeme",
		"change-me",
		"change_me":
		*errs = append(*errs, fmt.Errorf("%s must not use a local/default secret in production-like environments", field))
	}
}

func rejectProdDefault(errs *[]error, field, value string) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "local", "default", "changeme", "change-me", "change_me":
		*errs = append(*errs, fmt.Errorf("%s must not use a local/default value in production-like environments", field))
	}
}

func IsProdLikeEnv(environment string) bool {
	return isProdLikeEnv(environment)
}

// UsesDistributedRuntimeAuthority 判断配置是否已经启用分布式或持久化权威路径，
// 用于承载有状态运行时决策。
//
// 它不会单独把整份配置判定为生产环境：本地开发也可能使用 MySQL/Redis。
// 该信号只服务于运行时装配和预检，避免调用方只依赖 service.environment。
func UsesDistributedRuntimeAuthority(cfg ServiceConfig) bool {
	if cfg.Topology.Enabled {
		return true
	}
	if isProfileCriticalSvc(cfg.Service.Name) && ProfileStoreUsesDurableAuthority(cfg.ProfileStore) {
		return true
	}
	return false
}

// ProfileStoreUsesDurableAuthority 判断 profile_store.store_kind 是否由持久化 DB
// 作为权威存储，而不是进程本地或仅文件的开发存储。
func ProfileStoreUsesDurableAuthority(section ProfileStoreSection) bool {
	switch strings.ToLower(strings.TrimSpace(section.StoreKind)) {
	case "mysql", "mysql_redis":
		return true
	default:
		return false
	}
}

func isProdLikeEnv(environment string) bool {
	environment = strings.ToLower(strings.TrimSpace(environment))
	if environment == "" {
		return false
	}
	for _, token := range []string{"prod", "production", "staging", "stage", "preprod", "pre-production", "k8s", "kubernetes", "helm", "cluster", "live"} {
		if strings.Contains(environment, token) {
			return true
		}
	}
	return false
}

func isGatewayService(serviceName string) bool {
	return strings.EqualFold(strings.TrimSpace(serviceName), "gateway")
}

func isEventLogCritical(serviceName string) bool {
	switch strings.ToLower(strings.TrimSpace(serviceName)) {
	case "gateway", "logic", "playerd", "payx":
		return true
	default:
		return false
	}
}

func isIdemCriticalSvc(serviceName string) bool {
	switch strings.ToLower(strings.TrimSpace(serviceName)) {
	case "logic", "playerd":
		return true
	default:
		return false
	}
}

func isProfileCriticalSvc(serviceName string) bool {
	switch strings.ToLower(strings.TrimSpace(serviceName)) {
	case "logic", "playerd":
		return true
	default:
		return false
	}
}

func isBattleAgentService(serviceName string) bool {
	return strings.EqualFold(strings.TrimSpace(serviceName), "battleagent")
}
