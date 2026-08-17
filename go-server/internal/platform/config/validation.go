package config

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

func validateAdminTokens(errs *[]error, admin AdminSection) {
	if strings.ContainsAny(admin.Token, "\r\n") {
		*errs = append(*errs, fmt.Errorf("admin.token cannot contain line breaks"))
	}
	seen := map[string]string{}
	if admin.Token != "" {
		seen[admin.Token] = "admin.token"
	}
	for idx, token := range admin.Tokens {
		prefix := fmt.Sprintf("admin.tokens[%d]", idx)
		requireString(errs, prefix+".name", token.Name)
		requireString(errs, prefix+".token", token.Token)
		requireOneOf(errs, prefix+".role", token.Role, "admin", "operator", "viewer", "auditor")
		if strings.ContainsAny(token.Token, "\r\n") {
			*errs = append(*errs, fmt.Errorf("%s.token cannot contain line breaks", prefix))
		}
		if token.Token != "" {
			if first, ok := seen[token.Token]; ok {
				*errs = append(*errs, fmt.Errorf("%s.token duplicates %s", prefix, first))
			} else {
				seen[token.Token] = prefix + ".token"
			}
		}
		for scopeIdx, scope := range token.Scopes {
			if scope == "" {
				*errs = append(*errs, fmt.Errorf("%s.scopes[%d] is required", prefix, scopeIdx))
				continue
			}
			for _, r := range scope {
				if isAdminScopeRune(r) {
					continue
				}
				*errs = append(*errs, fmt.Errorf("%s.scopes[%d] %q can only contain letters, digits, dot, dash, underscore, colon, or star", prefix, scopeIdx, scope))
				break
			}
		}
	}
}

func isAdminScopeRune(r rune) bool {
	return r == '.' || r == '-' || r == '_' || r == ':' || r == '*' ||
		(r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9')
}

func (c ServiceConfig) Validate() error {
	var errs []error
	productionLike := isProdLikeEnv(c.Service.Environment)
	requireSafeName(&errs, "service.name", c.Service.Name)
	requireSafeName(&errs, "service.node_id", c.Service.NodeID)
	requireString(&errs, "service.environment", c.Service.Environment)
	requirePositiveInt(&errs, "service.stop_timeout_seconds", c.Service.StopTimeoutSeconds)
	requireNonNegInt(&errs, "service.stop_initial_grace_seconds", c.Service.StopInitialGraceSeconds)
	requirePositiveInt(&errs, "service.stop_probe_seconds", c.Service.StopProbeSeconds)
	requirePositiveInt(&errs, "service.stop_stable_seconds", c.Service.StopStableSeconds)
	if productionLike {
		validateProdSafety(&errs, c)
	}

	requireSafeName(&errs, "cluster.id", c.Cluster.ID)
	requireNonNegInt64(&errs, "cluster.gid", c.Cluster.GID)
	requireNonNegInt64(&errs, "cluster.virtual_gid", c.Cluster.VirtualGID)
	requireNonNegInt64(&errs, "cluster.shard_id", c.Cluster.ShardID)
	requireString(&errs, "cluster.zone", c.Cluster.Zone)
	requireString(&errs, "cluster.version", c.Cluster.Version)
	requireString(&errs, "cluster.data_version", c.Cluster.DataVersion)
	requirePositiveInt(&errs, "cluster.heartbeat_interval_seconds", c.Cluster.HeartbeatIntervalSeconds)

	requireListenAddress(&errs, "admin.listen", c.Admin.Listen)
	requireString(&errs, "admin.token", c.Admin.Token)
	requirePositiveInt(&errs, "admin.read_timeout_seconds", c.Admin.ReadTimeoutSeconds)
	requirePositiveInt(&errs, "admin.read_header_timeout_seconds", c.Admin.ReadHeaderTimeoutSeconds)
	requirePositiveInt(&errs, "admin.write_timeout_seconds", c.Admin.WriteTimeoutSeconds)
	requirePositiveInt(&errs, "admin.idle_timeout_seconds", c.Admin.IdleTimeoutSeconds)
	if productionLike {
		if !c.Admin.RBACEnabled {
			errs = append(errs, fmt.Errorf("admin.rbac_enabled is required for production-like environments"))
		}
		if !c.Admin.DangerousConfirm {
			errs = append(errs, fmt.Errorf("admin.dangerous_confirm is required for production-like environments"))
		}
		if !c.Audit.Enabled {
			errs = append(errs, fmt.Errorf("audit.enabled is required for production-like environments"))
		}
	}
	validateAdminTokens(&errs, c.Admin)

	if c.RateLimit.Enabled {
		if c.RateLimit.Algorithm != "window" && c.RateLimit.Algorithm != "token_bucket" {
			errs = append(errs, fmt.Errorf("rate_limit.algorithm must be window or token_bucket"))
		}
		requirePositiveInt(&errs, "rate_limit.window_seconds", c.RateLimit.WindowSeconds)
		requirePositiveInt(&errs, "rate_limit.max_requests", c.RateLimit.MaxRequests)
		requirePositiveInt(&errs, "rate_limit.cleanup_interval_seconds", c.RateLimit.CleanupIntervalSeconds)
		if c.RateLimit.TrustedHeader != "" && strings.ContainsAny(c.RateLimit.TrustedHeader, "\r\n") {
			errs = append(errs, fmt.Errorf("rate_limit.trusted_header cannot contain line breaks"))
		}
		if err := validRateRules(c.RateLimit.Rules); err != nil {
			errs = append(errs, err)
		}
	}
	requireNonNegInt(&errs, "tracing.max_spans", c.Tracing.MaxSpans)
	if c.OTel.Enabled {
		requireString(&errs, "otel.endpoint", c.OTel.Endpoint)
		requireString(&errs, "otel.service_name", c.OTel.ServiceName)
		requirePositiveInt(&errs, "otel.batch_timeout_seconds", c.OTel.BatchTimeoutSeconds)
		if c.OTel.SampleRatio <= 0 || c.OTel.SampleRatio > 1 {
			errs = append(errs, fmt.Errorf("otel.sample_ratio must be greater than 0 and at most 1"))
		}
	}
	if c.DataTables.Enabled {
		requireString(&errs, "data_tables.directory", c.DataTables.Directory)
		requireString(&errs, "data_tables.version", c.DataTables.Version)
	}
	if c.I18N.Enabled {
		requireString(&errs, "i18n.directory", c.I18N.Directory)
		requireString(&errs, "i18n.default_language", c.I18N.DefaultLanguage)
		requireString(&errs, "i18n.version", c.I18N.Version)
	}
	c.validatePVF(&errs)
	if c.Audit.Enabled {
		requireOneOf(&errs, "audit.kind", c.Audit.Kind, "memory", "file")
		requirePositiveInt(&errs, "audit.memory_limit", c.Audit.MemoryLimit)
		if c.Audit.Kind == "file" {
			requireString(&errs, "audit.file_path", c.Audit.FilePath)
		}
	}
	if c.LogicLog.Enabled {
		requireOneOf(&errs, "logic_log.kind", c.LogicLog.Kind, "memory", "file")
		requirePositiveInt(&errs, "logic_log.memory_limit", c.LogicLog.MemoryLimit)
		requireString(&errs, "logic_log.reasons_path", c.LogicLog.ReasonsPath)
		if c.LogicLog.Kind == "file" {
			requireString(&errs, "logic_log.file_path", c.LogicLog.FilePath)
		}
		if productionLike && c.LogicLog.Kind == "memory" {
			errs = append(errs, fmt.Errorf("logic_log.kind memory is only allowed for local/dev/test environments"))
		}
	}
	if c.BILog.Enabled {
		requireOneOf(&errs, "bi_log.kind", c.BILog.Kind, "memory", "file")
		requirePositiveInt(&errs, "bi_log.memory_limit", c.BILog.MemoryLimit)
		requireString(&errs, "bi_log.schema_path", c.BILog.SchemaPath)
		if c.BILog.Kind == "file" {
			requireString(&errs, "bi_log.file_path", c.BILog.FilePath)
		}
		if productionLike && c.BILog.Kind == "memory" {
			errs = append(errs, fmt.Errorf("bi_log.kind memory is only allowed for local/dev/test environments"))
		}
	}
	if productionLike && isEventLogCritical(c.Service.Name) && !c.EventLog.Enabled {
		errs = append(errs, fmt.Errorf("eventlog.enabled is required for production %s services", c.Service.Name))
	}
	if c.EventLog.Enabled {
		requireOneOf(&errs, "eventlog.store_kind", c.EventLog.StoreKind, "memory", "mysql")
		requireString(&errs, "eventlog.admin_prefix", c.EventLog.AdminPrefix)
		if c.EventLog.StoreKind == "memory" && (c.EventLog.Strict || productionLike) {
			errs = append(errs, fmt.Errorf("eventlog.store_kind memory is only allowed for local/dev/test eventlog paths"))
		}
		if c.EventLog.StoreKind == "mysql" {
			requireString(&errs, "eventlog.mysql_dsn", c.EventLog.MySQLDSN)
			requireSQLIdentifier(&errs, "eventlog.mysql_table", c.EventLog.MySQLTable)
			requirePositiveInt(&errs, "eventlog.mysql_max_open_conns", c.EventLog.MySQLMaxOpenConns)
			requirePositiveInt(&errs, "eventlog.mysql_max_idle_conns", c.EventLog.MySQLMaxIdleConns)
			requirePositiveInt(&errs, "eventlog.mysql_conn_max_lifetime_seconds", c.EventLog.MySQLConnMaxLifetime)
		}
		if c.EventLog.PublishEnabled {
			requireOneOf(&errs, "eventlog.publish_kind", c.EventLog.PublishKind, "bus", "http", "bus_http")
			requirePositiveInt(&errs, "eventlog.publish_interval_seconds", c.EventLog.PublishIntervalSeconds)
			requirePositiveInt(&errs, "eventlog.publish_batch_size", c.EventLog.PublishBatchSize)
			requirePositiveInt(&errs, "eventlog.publish_retry_seconds", c.EventLog.PublishRetrySeconds)
			requirePositiveInt(&errs, "eventlog.publish_max_attempts", c.EventLog.PublishMaxAttempts)
			if c.EventLog.PublishKind == "http" || c.EventLog.PublishKind == "bus_http" {
				requireString(&errs, "eventlog.publish_http_url", c.EventLog.PublishHTTPURL)
				requireString(&errs, "eventlog.publish_http_method", c.EventLog.PublishHTTPMethod)
				requirePositiveInt(&errs, "eventlog.publish_http_timeout_seconds", c.EventLog.PublishHTTPTimeout)
				requireOneOf(&errs, "eventlog.publish_http_payload", c.EventLog.PublishHTTPPayload, "event", "payload")
			}
		}
	}
	if c.AccountCenter.Enabled {
		requireOneOf(&errs, "account_center.store_kind", c.AccountCenter.StoreKind, "memory", "mysql")
		if !strings.HasPrefix(c.AccountCenter.AdminPrefix, "/") {
			errs = append(errs, fmt.Errorf("account_center.admin_prefix %q must start with /", c.AccountCenter.AdminPrefix))
		}
		if c.AccountCenter.StoreKind == "memory" && productionLike {
			errs = append(errs, fmt.Errorf("account_center.store_kind memory is only allowed for local/dev/test account center paths"))
		}
		if c.AccountCenter.StoreKind == "mysql" {
			requireString(&errs, "account_center.mysql_dsn", c.AccountCenter.MySQLDSN)
			requireSQLIdentifier(&errs, "account_center.mysql_table", c.AccountCenter.MySQLTable)
		}
	}
	if c.StorageObject.Enabled {
		requireOneOf(&errs, "storage_object.store_kind", c.StorageObject.StoreKind, "memory", "mysql")
		if !strings.HasPrefix(c.StorageObject.AdminPrefix, "/") {
			errs = append(errs, fmt.Errorf("storage_object.admin_prefix %q must start with /", c.StorageObject.AdminPrefix))
		}
		if c.StorageObject.StoreKind == "memory" && productionLike {
			errs = append(errs, fmt.Errorf("storage_object.store_kind memory is only allowed for local/dev/test storage object paths"))
		}
		if c.StorageObject.StoreKind == "mysql" {
			requireString(&errs, "storage_object.mysql_dsn", c.StorageObject.MySQLDSN)
			requireSQLIdentifier(&errs, "storage_object.mysql_table", c.StorageObject.MySQLTable)
		}
	}
	if isBattleAgentService(c.Service.Name) {
		requireOneOf(&errs, "battle_agent.settlement_outbox_kind", c.BattleAgent.SettlementOutboxKind, "memory", "eventlog")
		requireSafeName(&errs, "battle_agent.settlement_outbox_stream", c.BattleAgent.SettlementOutboxStream)
		requireSafeName(&errs, "battle_agent.settlement_outbox_type", c.BattleAgent.SettlementOutboxType)
		if c.BattleAgent.SettlementOutboxKind == "eventlog" && !c.EventLog.Enabled {
			errs = append(errs, fmt.Errorf("eventlog.enabled is required when battle_agent.settlement_outbox_kind is eventlog"))
		}
		if productionLike && c.BattleAgent.SettlementOutboxKind != "eventlog" {
			errs = append(errs, fmt.Errorf("battle_agent.settlement_outbox_kind eventlog is required for production battleagent services"))
		}
	}
	requirePositiveInt(&errs, "reload.poll_interval_seconds", c.Reload.PollIntervalSeconds)
	requirePositiveInt(&errs, "reload.apply_timeout_seconds", c.Reload.ApplyTimeoutSeconds)
	if c.HotUpdate.Enabled {
		requireSafeName(&errs, "hot_update.target", c.HotUpdate.Target)
		requireOneOf(&errs, "hot_update.control_kind", c.HotUpdate.ControlKind, "memory", "eventlog")
		requireOneOf(&errs, "hot_update.source_kind", c.HotUpdate.SourceKind, "local")
		requireOneOf(&errs, "hot_update.applier_kind", c.HotUpdate.ApplierKind, "datatable", "native_patch")
		requireString(&errs, "hot_update.workspace", c.HotUpdate.Workspace)
		requirePositiveInt(&errs, "hot_update.apply_timeout_seconds", c.HotUpdate.ApplyTimeoutSeconds)
		requireNonNegInt(&errs, "hot_update.native_patch_max_symbols", c.HotUpdate.NativePatchMaxSymbols)
		requireNonNegInt(&errs, "hot_update.native_patch_max_live_seconds", c.HotUpdate.NativePatchMaxLiveSeconds)
		requireNonNegInt(&errs, "hot_update.native_patch_min_reason_length", c.HotUpdate.NativePatchMinReasonLength)
		if c.HotUpdate.RequireSignature {
			requireString(&errs, "hot_update.signing_key_env", c.HotUpdate.SigningKeyEnv)
		}
		validatePatchPolicy(&errs, c.HotUpdate, productionLike)
		if c.HotUpdate.ControlKind == "eventlog" && !c.EventLog.Enabled {
			errs = append(errs, fmt.Errorf("eventlog.enabled is required when hot_update.control_kind is eventlog"))
		}
		if productionLike && c.HotUpdate.ControlKind != "eventlog" {
			errs = append(errs, fmt.Errorf("hot_update.control_kind eventlog is required for production-like hot update"))
		}
	}
	if c.StateSync.Enabled {
		requireOneOf(&errs, "state_sync.kind", c.StateSync.Kind, "memory")
		requireSafeName(&errs, "state_sync.namespace", c.StateSync.Namespace)
		requirePositiveInt(&errs, "state_sync.ttl_seconds", c.StateSync.TTLSeconds)
		requirePositiveInt(&errs, "state_sync.cleanup_interval_seconds", c.StateSync.CleanupIntervalSeconds)
		requirePositiveInt(&errs, "state_sync.max_payload_bytes", c.StateSync.MaxPayloadBytes)
	}
	if c.ServerGroup.Enabled {
		requireOneOf(&errs, "server_group.store_kind", c.ServerGroup.StoreKind, "file")
		requireString(&errs, "server_group.plan_file", c.ServerGroup.PlanFile)
		requireString(&errs, "server_group.merge_archive_dir", c.ServerGroup.MergeArchiveDir)
		if !strings.HasPrefix(c.ServerGroup.AdminPrefix, "/") {
			errs = append(errs, fmt.Errorf("server_group.admin_prefix %q must start with /", c.ServerGroup.AdminPrefix))
		}
		requirePositiveInt(&errs, "server_group.in_zone_delay_seconds", c.ServerGroup.InZoneDelaySeconds)
		requirePositiveInt(&errs, "server_group.notice_lead_seconds", c.ServerGroup.NoticeLeadSeconds)
	}
	if c.Topology.Enabled {
		requireNonEmptySlice(&errs, "topology.services", c.Topology.Services)
		requireSafeName(&errs, "topology.gateway_service", c.Topology.GatewayService)
		requireSafeName(&errs, "topology.gateway_backend_service", c.Topology.GatewayBackendService)
		requirePositiveInt(&errs, "topology.refresh_interval_seconds", c.Topology.RefreshIntervalSeconds)
		for idx, service := range c.Topology.Services {
			requireSafeName(&errs, fmt.Sprintf("topology.services[%d]", idx), service)
		}
	}
	requirePositiveInt(&errs, "discovery.cache_ttl_seconds", c.Discovery.CacheTTLSeconds)
	requirePositiveInt(&errs, "discovery.failure_ttl_seconds", c.Discovery.FailureTTLSeconds)
	requirePositiveInt(&errs, "discovery.failure_threshold", c.Discovery.FailureThreshold)
	requireOneOf(&errs, "discovery.strategy", c.Discovery.Strategy, "hash", "random", "round_robin", "weighted_round_robin")
	requirePositiveInt(&errs, "rpc.call_timeout_seconds", c.RPC.CallTimeoutSeconds)
	requirePositiveInt(&errs, "rpc.max_pending", c.RPC.MaxPending)
	requirePositiveInt(&errs, "rpc.max_payload_bytes", c.RPC.MaxPayloadBytes)

	requireOneOf(&errs, "idempotency.kind", c.Idempotency.Kind, "memory", "redis", "mysql", "mysql_redis")
	if productionLike && isIdemCriticalSvc(c.Service.Name) && c.Idempotency.Kind != "mysql_redis" {
		errs = append(errs, fmt.Errorf("idempotency.kind mysql_redis is required for production %s services", c.Service.Name))
	}
	requirePositiveInt(&errs, "idempotency.ttl_seconds", c.Idempotency.TTLSeconds)
	requireSafeName(&errs, "idempotency.key_prefix", c.Idempotency.KeyPrefix)
	if c.Idempotency.Kind == "redis" || c.Idempotency.Kind == "mysql_redis" {
		requireString(&errs, "idempotency.redis_address", c.Idempotency.RedisAddress)
		requireNonNegInt(&errs, "idempotency.redis_db", c.Idempotency.RedisDB)
		requirePositiveInt(&errs, "idempotency.redis_pool_size", c.Idempotency.RedisPoolSize)
		requirePositiveInt(&errs, "idempotency.redis_timeout_seconds", c.Idempotency.RedisTimeoutSeconds)
		requirePositiveInt(&errs, "idempotency.redis_connect_timeout_seconds", c.Idempotency.RedisConnectTimeoutSecs)
		requirePositiveInt(&errs, "idempotency.redis_read_timeout_seconds", c.Idempotency.RedisReadTimeoutSecs)
		requirePositiveInt(&errs, "idempotency.redis_write_timeout_seconds", c.Idempotency.RedisWriteTimeoutSecs)
	}
	if c.Idempotency.Kind == "mysql" || c.Idempotency.Kind == "mysql_redis" {
		requireString(&errs, "idempotency.mysql_dsn", c.Idempotency.MySQLDSN)
		requirePositiveInt(&errs, "idempotency.mysql_max_open_conns", c.Idempotency.MySQLMaxOpenConns)
		requirePositiveInt(&errs, "idempotency.mysql_max_idle_conns", c.Idempotency.MySQLMaxIdleConns)
		requirePositiveInt(&errs, "idempotency.mysql_conn_max_lifetime_seconds", c.Idempotency.MySQLConnMaxLifetimeSec)
	}

	requireOneOf(&errs, "registry.kind", c.Registry.Kind, "memory", "redis", "etcd")
	requireSafeName(&errs, "registry.namespace", c.Registry.Namespace)
	requirePositiveInt64(&errs, "registry.lease_ttl", c.Registry.LeaseTTL)
	if c.Registry.Kind == "redis" || c.Registry.Kind == "etcd" {
		requireNonEmptySlice(&errs, "registry.endpoints", c.Registry.Endpoints)
	}
	if c.Registry.Kind == "redis" {
		requireNonNegInt(&errs, "registry.redis_db", c.Registry.RedisDB)
	}

	requireOneOf(&errs, "bus.kind", c.Bus.Kind, "memory", "redis", "nats")
	requireSafeName(&errs, "bus.namespace", c.Bus.Namespace)
	if c.Bus.Kind == "redis" || c.Bus.Kind == "nats" {
		requireNonEmptySlice(&errs, "bus.endpoints", c.Bus.Endpoints)
	}
	if c.Bus.Kind == "redis" {
		requireNonNegInt(&errs, "bus.redis_db", c.Bus.RedisDB)
		if c.Bus.RedisUsername != "" && c.Bus.RedisPassword == "" {
			errs = append(errs, fmt.Errorf("bus.redis_password is required when bus.redis_username is set"))
		}
	}
	if c.Bus.Kind == "nats" {
		requireOneOf(&errs, "bus.nats_wire_encoding", c.Bus.NATSWireEncoding, "binary_v1", "json")
		if c.Bus.NATSPassword != "" && c.Bus.NATSUsername == "" {
			errs = append(errs, fmt.Errorf("bus.nats_username is required when bus.nats_password is set"))
		}
		if (c.Bus.NATSCertFile == "") != (c.Bus.NATSKeyFile == "") {
			errs = append(errs, fmt.Errorf("bus.nats_cert_file and bus.nats_key_file must be configured together"))
		}
	}

	requireOneOf(&errs, "cache.kind", c.Cache.Kind, "memory", "redis")
	requireSafeName(&errs, "cache.key_prefix", c.Cache.KeyPrefix)
	requirePositiveInt(&errs, "cache.default_ttl_seconds", c.Cache.DefaultTTLSeconds)
	requirePositiveInt(&errs, "cache.max_entries", c.Cache.MaxEntries)
	if c.Cache.Kind == "redis" {
		requireString(&errs, "cache.redis_address", c.Cache.RedisAddress)
		requireNonNegInt(&errs, "cache.redis_db", c.Cache.RedisDB)
		requirePositiveInt(&errs, "cache.redis_pool_size", c.Cache.RedisPoolSize)
		requirePositiveInt(&errs, "cache.redis_timeout_seconds", c.Cache.RedisTimeoutSeconds)
		requirePositiveInt(&errs, "cache.redis_connect_timeout_seconds", c.Cache.RedisConnectTimeout)
		requirePositiveInt(&errs, "cache.redis_read_timeout_seconds", c.Cache.RedisReadTimeout)
		requirePositiveInt(&errs, "cache.redis_write_timeout_seconds", c.Cache.RedisWriteTimeout)
	}

	requireOneOf(&errs, "lock.kind", c.Lock.Kind, "memory", "redis")
	requireSafeName(&errs, "lock.key_prefix", c.Lock.KeyPrefix)
	requirePositiveInt(&errs, "lock.ttl_seconds", c.Lock.TTLSeconds)
	if c.Lock.Kind == "redis" {
		requireString(&errs, "lock.redis_address", c.Lock.RedisAddress)
		requireNonNegInt(&errs, "lock.redis_db", c.Lock.RedisDB)
		requirePositiveInt(&errs, "lock.redis_pool_size", c.Lock.RedisPoolSize)
		requirePositiveInt(&errs, "lock.redis_timeout_seconds", c.Lock.RedisTimeoutSeconds)
		requirePositiveInt(&errs, "lock.redis_connect_timeout_seconds", c.Lock.RedisConnectTimeout)
		requirePositiveInt(&errs, "lock.redis_read_timeout_seconds", c.Lock.RedisReadTimeout)
		requirePositiveInt(&errs, "lock.redis_write_timeout_seconds", c.Lock.RedisWriteTimeout)
	}

	requireOneOf(&errs, "presence.kind", c.Presence.Kind, "memory", "redis")
	requireSafeName(&errs, "presence.key_prefix", c.Presence.KeyPrefix)
	requirePositiveInt(&errs, "presence.ttl_seconds", c.Presence.TTLSeconds)
	if c.Presence.Kind == "redis" {
		requireString(&errs, "presence.redis_address", c.Presence.RedisAddress)
		requireNonNegInt(&errs, "presence.redis_db", c.Presence.RedisDB)
		requirePositiveInt(&errs, "presence.redis_pool_size", c.Presence.RedisPoolSize)
		requirePositiveInt(&errs, "presence.redis_timeout_seconds", c.Presence.RedisTimeoutSeconds)
		requirePositiveInt(&errs, "presence.redis_connect_timeout_seconds", c.Presence.RedisConnectTimeout)
		requirePositiveInt(&errs, "presence.redis_read_timeout_seconds", c.Presence.RedisReadTimeout)
		requirePositiveInt(&errs, "presence.redis_write_timeout_seconds", c.Presence.RedisWriteTimeout)
	}

	requirePositiveInt(&errs, "worker.size", c.Worker.Size)
	requirePositiveInt(&errs, "worker.queue", c.Worker.Queue)

	requireOneOf(&errs, "profile_store.store_kind", c.ProfileStore.StoreKind, "memory", "file", "json", "redis", "pika", "profiledb", "profile_db", "mysql", "mysql_redis")
	if c.ProfileStore.StoreKind == "file" || c.ProfileStore.StoreKind == "json" {
		requireString(&errs, "profile_store.store_directory", c.ProfileStore.StoreDirectory)
	}
	if c.ProfileStore.StoreKind == "redis" || c.ProfileStore.StoreKind == "pika" || c.ProfileStore.StoreKind == "profiledb" || c.ProfileStore.StoreKind == "profile_db" {
		requireString(&errs, "profile_store.store_address", c.ProfileStore.StoreAddress)
		requireString(&errs, "profile_store.store_key_prefix", c.ProfileStore.StoreKeyPrefix)
	}
	if c.ProfileStore.StoreKind == "mysql" || c.ProfileStore.StoreKind == "mysql_redis" {
		if !c.ProfileStore.MySQLShardingEnabled && len(c.ProfileStore.MySQLShards) == 0 {
			requireString(&errs, "profile_store.mysql_dsn", c.ProfileStore.MySQLDSN)
		}
		requirePositiveInt(&errs, "profile_store.mysql_max_open_conns", c.ProfileStore.MySQLMaxOpenConns)
		requirePositiveInt(&errs, "profile_store.mysql_max_idle_conns", c.ProfileStore.MySQLMaxIdleConns)
		requirePositiveInt(&errs, "profile_store.mysql_conn_max_lifetime_seconds", c.ProfileStore.MySQLConnMaxLifetime)
		validateMySQLShards(&errs, c.ProfileStore)
	} else if c.ProfileStore.MySQLShardingEnabled || len(c.ProfileStore.MySQLShards) > 0 {
		errs = append(errs, fmt.Errorf("profile_store.mysql_shards require profile_store.store_kind mysql or mysql_redis"))
	}
	requireOneOf(&errs, "profile_store.summary_store_kind", c.ProfileStore.SummaryStoreKind, "memory", "redis", "pika")
	if c.ProfileStore.SummaryStoreKind == "redis" || c.ProfileStore.SummaryStoreKind == "pika" {
		requireString(&errs, "profile_store.summary_store_address", c.ProfileStore.SummaryStoreAddress)
		requireString(&errs, "profile_store.summary_store_key_prefix", c.ProfileStore.SummaryStoreKeyPrefix)
		requirePositiveInt(&errs, "profile_store.summary_store_pool_size", c.ProfileStore.SummaryStorePoolSize)
		requirePositiveInt(&errs, "profile_store.summary_store_timeout_seconds", c.ProfileStore.SummaryStoreTimeout)
		requirePositiveInt(&errs, "profile_store.summary_store_connect_timeout_seconds", c.ProfileStore.SummaryStoreConnectTimeout)
		requirePositiveInt(&errs, "profile_store.summary_store_read_timeout_seconds", c.ProfileStore.SummaryStoreReadTimeout)
		requirePositiveInt(&errs, "profile_store.summary_store_write_timeout_seconds", c.ProfileStore.SummaryStoreWriteTimeout)
		requireNonNegInt(&errs, "profile_store.summary_store_db", c.ProfileStore.SummaryStoreDB)
		requireNonNegInt(&errs, "profile_store.summary_store_ttl_seconds", c.ProfileStore.SummaryStoreTTL)
	}
	requireNonNegInt(&errs, "profile_store.store_db", c.ProfileStore.StoreDB)
	requirePositiveInt(&errs, "profile_store.store_pool_size", c.ProfileStore.StorePoolSize)
	requirePositiveInt(&errs, "profile_store.store_timeout_seconds", c.ProfileStore.StoreTimeoutSeconds)
	requirePositiveInt(&errs, "profile_store.store_connect_timeout_seconds", c.ProfileStore.StoreConnectTimeout)
	requirePositiveInt(&errs, "profile_store.store_read_timeout_seconds", c.ProfileStore.StoreReadTimeout)
	requirePositiveInt(&errs, "profile_store.store_write_timeout_seconds", c.ProfileStore.StoreWriteTimeout)
	requireNonNegInt(&errs, "profile_store.store_ttl_seconds", c.ProfileStore.StoreTTLSeconds)
	requireOneOf(&errs, "profile_store.save_mode", c.ProfileStore.SaveMode, "sync", "async", "writebehind", "write_behind")
	requirePositiveInt(&errs, "profile_store.async_flush_interval_seconds", c.ProfileStore.AsyncFlushIntervalSeconds)
	requirePositiveInt(&errs, "profile_store.async_max_pending", c.ProfileStore.AsyncMaxPending)
	requirePositiveInt(&errs, "profile_store.async_retry_backoff_seconds", c.ProfileStore.AsyncRetryBackoffSeconds)
	requireNonNegInt(&errs, "profile_store.async_max_retries", c.ProfileStore.AsyncMaxRetries)
	requireNonNegInt(&errs, "profile_store.async_auto_expire_seconds", c.ProfileStore.AsyncAutoExpireSeconds)
	requirePositiveInt(&errs, "profile_store.async_dead_letter_limit", c.ProfileStore.AsyncDeadLetterLimit)
	if isAsyncSaveMode(c.ProfileStore.SaveMode) {
		requireString(&errs, "profile_store.async_dead_letter_directory", c.ProfileStore.AsyncDeadLetterDirectory)
		if productionLike && c.ProfileStore.AsyncMaxRetries <= 0 {
			errs = append(errs, fmt.Errorf("profile_store.async_max_retries must be positive for production-like async profile stores; 0 means retry forever"))
		}
	}

	requireListenAddress(&errs, "gateway.auth_listen", c.Gateway.AuthListen)
	requireOneOf(&errs, "gateway.auth_mode", c.Gateway.AuthMode, "mock", "external", "disabled")
	if isGatewayService(c.Service.Name) && productionLike && c.Gateway.AuthMode != "external" {
		errs = append(errs, fmt.Errorf("gateway.auth_mode external is required for production gateway services"))
	}
	if c.Gateway.RegistryGateSelection {
		requireString(&errs, "gateway.gate_service", c.Gateway.GateService)
		requireString(&errs, "gateway.gate_route_feature", c.Gateway.GateRouteFeature)
	}
	if c.Gateway.RemoteGateSelection && !c.Gateway.RegistryGateSelection {
		errs = append(errs, fmt.Errorf("gateway.remote_gate_selection requires gateway.registry_gate_selection"))
	}
	if c.Gateway.RemoteGateSelection && !c.Gateway.SharedLoginTokens {
		errs = append(errs, fmt.Errorf("gateway.remote_gate_selection requires gateway.shared_login_tokens"))
	}
	if isGatewayService(c.Service.Name) && productionLike && c.Gateway.RemoteGateSelection && c.Cache.Kind != "redis" {
		errs = append(errs, fmt.Errorf("gateway.remote_gate_selection requires cache.kind=redis for production gateway services"))
	}
	requireListenAddress(&errs, "gateway.tcp_listen", c.Gateway.TCPListen)
	if c.Gateway.TCPProxyProtocolEnabled {
		requirePositiveInt(&errs, "gateway.tcp_proxy_header_timeout_seconds", c.Gateway.TCPProxyHeaderSec)
		requireNonEmptySlice(&errs, "gateway.tcp_proxy_trusted_cidrs", c.Gateway.TCPProxyTrustedCIDRs)
		for idx, value := range c.Gateway.TCPProxyTrustedCIDRs {
			if err := validateIPOrCIDR(value); err != nil {
				errs = append(errs, fmt.Errorf("gateway.tcp_proxy_trusted_cidrs[%d]: %w", idx, err))
			}
		}
	}
	if c.Gateway.KCPEnabled {
		requireListenAddress(&errs, "gateway.kcp_listen", c.Gateway.KCPListen)
		requireNonNegInt(&errs, "gateway.kcp_nodelay", c.Gateway.KCPNoDelay)
		requirePositiveInt(&errs, "gateway.kcp_interval", c.Gateway.KCPInterval)
		requireNonNegInt(&errs, "gateway.kcp_resend", c.Gateway.KCPResend)
		requireNonNegInt(&errs, "gateway.kcp_no_congestion", c.Gateway.KCPNoCongestion)
		requirePositiveInt(&errs, "gateway.kcp_window_size", c.Gateway.KCPWindowSize)
		requireNonNegInt(&errs, "gateway.kcp_data_shards", c.Gateway.KCPDataShards)
		requireNonNegInt(&errs, "gateway.kcp_parity_shards", c.Gateway.KCPParityShards)
	}
	if c.Gateway.QUICEnabled {
		requireListenAddress(&errs, "gateway.quic_listen", c.Gateway.QUICListen)
		requirePositiveInt(&errs, "gateway.quic_handshake_timeout_seconds", c.Gateway.QUICHandshakeSec)
		requirePositiveInt(&errs, "gateway.quic_max_idle_timeout_seconds", c.Gateway.QUICMaxIdleTimeoutSeconds)
		requireNonNegInt(&errs, "gateway.quic_keep_alive_seconds", c.Gateway.QUICKeepAliveSeconds)
		requirePositiveInt64(&errs, "gateway.quic_max_incoming_streams", c.Gateway.QUICMaxIncomingStreams)
		if (c.Gateway.QUICCertFile == "") != (c.Gateway.QUICKeyFile == "") {
			errs = append(errs, fmt.Errorf("gateway.quic_cert_file and gateway.quic_key_file must be set together"))
		}
		if productionLike && c.Gateway.QUICCertFile == "" && c.Gateway.QUICKeyFile == "" {
			errs = append(errs, fmt.Errorf("gateway.quic_cert_file and gateway.quic_key_file are required for production-like QUIC"))
		}
	}
	if c.Gateway.WebSocketEnabled {
		requireListenAddress(&errs, "gateway.websocket_listen", c.Gateway.WebSocketListen)
		if !strings.HasPrefix(c.Gateway.WebSocketPath, "/") {
			errs = append(errs, fmt.Errorf("gateway.websocket_path %q must start with /", c.Gateway.WebSocketPath))
		}
		requirePositiveInt(&errs, "gateway.websocket_max_payload_bytes", c.Gateway.WebSocketMaxPayloadBytes)
		requirePositiveInt(&errs, "gateway.websocket_heartbeat_seconds", c.Gateway.WebSocketHeartbeatSeconds)
	}
	requireListenAddress(&errs, "gateway.chat_listen", c.Gateway.ChatListen)
	requireSafeName(&errs, "gateway.backend_service", c.Gateway.BackendService)
	requirePositiveInt(&errs, "gateway.backend_poll_interval_seconds", c.Gateway.BackendPollIntervalSeconds)
	requirePositiveInt(&errs, "gateway.max_connections", c.Gateway.MaxConnections)
	requirePositiveInt(&errs, "gateway.connection_rate_limit_window_seconds", c.Gateway.ConnRateWindowSec)
	requirePositiveInt(&errs, "gateway.connection_rate_limit_max", c.Gateway.ConnectionRateLimitMax)
	requirePositiveInt(&errs, "gateway.connection_rate_limit_max_entries", c.Gateway.ConnRateMaxEntries)
	if c.Gateway.ConnRateIPv4Prefix < 1 || c.Gateway.ConnRateIPv4Prefix > 32 {
		errs = append(errs, fmt.Errorf("gateway.connection_rate_limit_ipv4_prefix must be between 1 and 32"))
	}
	if c.Gateway.ConnRateIPv6Prefix < 1 || c.Gateway.ConnRateIPv6Prefix > 128 {
		errs = append(errs, fmt.Errorf("gateway.connection_rate_limit_ipv6_prefix must be between 1 and 128"))
	}
	requirePositiveInt(&errs, "gateway.session_idle_timeout_seconds", c.Gateway.SessionIdleTimeoutSeconds)
	requirePositiveInt(&errs, "gateway.session_sweep_interval_seconds", c.Gateway.SessionSweepSec)
	requirePositiveInt(&errs, "gateway.session_hook_timeout_seconds", c.Gateway.SessionHookTimeoutSeconds)
	requireString(&errs, "gateway.account_id", c.Gateway.AccountID)
	requirePositiveInt32(&errs, "gateway.account_num_id", c.Gateway.AccountNumID)
	if c.Gateway.AuthMode == "mock" {
		requireString(&errs, "gateway.auth_token", c.Gateway.AuthToken)
		requireString(&errs, "gateway.login_token", c.Gateway.LoginToken)
	}
	requireString(&errs, "gateway.server_version", c.Gateway.ServerVersion)
	requirePositiveInt32(&errs, "gateway.shard_id", c.Gateway.ShardID)
	requireString(&errs, "gateway.shard_name", c.Gateway.ShardName)
	requireString(&errs, "gateway.shard_display", c.Gateway.ShardDisplay)
	requirePositiveInt32(&errs, "gateway.shard_state", c.Gateway.ShardState)
	requirePositiveInt32(&errs, "gateway.recommend", c.Gateway.Recommend)
	requireString(&errs, "gateway.recommend_lang", c.Gateway.RecommendLang)
	requirePositiveInt32(&errs, "gateway.virtual_gid", c.Gateway.VirtualGID)
	requireNonEmptySlice(&errs, "gateway.product_ids", c.Gateway.ProductIDs)

	return errors.Join(errs...)
}

func validateIPOrCIDR(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("empty value")
	}
	if net.ParseIP(value) != nil {
		return nil
	}
	if _, _, err := net.ParseCIDR(value); err != nil {
		return fmt.Errorf("invalid IP or CIDR %q", value)
	}
	return nil
}
