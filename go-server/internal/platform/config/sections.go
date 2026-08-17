package config

type ServiceConfig struct {
	Service       ServiceSection       `toml:"service"`
	Cluster       ClusterSection       `toml:"cluster"`
	Admin         AdminSection         `toml:"admin"`
	Metrics       MetricsSection       `toml:"metrics"`
	Tracing       TracingSection       `toml:"tracing"`
	OTel          OTelSection          `toml:"otel"`
	Debug         DebugSection         `toml:"debug"`
	DataTables    DataTablesSection    `toml:"data_tables"`
	I18N          I18NSection          `toml:"i18n"`
	PVF           PVFSection           `toml:"pvf"`
	Audit         AuditSection         `toml:"audit"`
	LogicLog      LogicLogSection      `toml:"logic_log"`
	BILog         BILogSection         `toml:"bi_log"`
	EventLog      EventLogSection      `toml:"eventlog"`
	AccountCenter AccountCenterSection `toml:"account_center"`
	StorageObject StorageObjectSection `toml:"storage_object"`
	BattleAgent   BattleAgentSection   `toml:"battle_agent"`
	Reload        ReloadSection        `toml:"reload"`
	HotUpdate     HotUpdateSection     `toml:"hot_update"`
	StateSync     StateSyncSection     `toml:"state_sync"`
	ServerGroup   ServerGroupSection   `toml:"server_group"`
	Topology      TopologySection      `toml:"topology"`
	Discovery     DiscoverySection     `toml:"discovery"`
	RPC           RPCSection           `toml:"rpc"`
	RateLimit     RateLimitSection     `toml:"rate_limit"`
	Idempotency   IdempotencySection   `toml:"idempotency"`
	Registry      RegistrySection      `toml:"registry"`
	Bus           BusSection           `toml:"bus"`
	Cache         CacheSection         `toml:"cache"`
	Lock          LockSection          `toml:"lock"`
	Presence      PresenceSection      `toml:"presence"`
	Worker        WorkerSection        `toml:"worker"`
	ProfileStore  ProfileStoreSection  `toml:"profile_store"`
	Gateway       GatewaySection       `toml:"gateway"`
}

type ServiceSection struct {
	Name                    string `toml:"name"`
	NodeID                  string `toml:"node_id"`
	Environment             string `toml:"environment"`
	PublicAddr              string `toml:"public_addr"`
	PrivateAddr             string `toml:"private_addr"`
	StopTimeoutSeconds      int    `toml:"stop_timeout_seconds"`
	StopInitialGraceSeconds int    `toml:"stop_initial_grace_seconds"`
	StopProbeSeconds        int    `toml:"stop_probe_seconds"`
	StopStableSeconds       int    `toml:"stop_stable_seconds"`
}

type ClusterSection struct {
	ID                       string `toml:"id"`
	GID                      int64  `toml:"gid"`
	VirtualGID               int64  `toml:"virtual_gid"`
	ShardID                  int64  `toml:"shard_id"`
	Zone                     string `toml:"zone"`
	Version                  string `toml:"version"`
	DataVersion              string `toml:"data_version"`
	Maintaining              bool   `toml:"maintaining"`
	HeartbeatIntervalSeconds int    `toml:"heartbeat_interval_seconds"`
}

type AdminSection struct {
	Listen                   string              `toml:"listen"`
	Token                    string              `toml:"token"`
	RBACEnabled              bool                `toml:"rbac_enabled"`
	DangerousConfirm         bool                `toml:"dangerous_confirm"`
	ReadTimeoutSeconds       int                 `toml:"read_timeout_seconds"`
	ReadHeaderTimeoutSeconds int                 `toml:"read_header_timeout_seconds"`
	WriteTimeoutSeconds      int                 `toml:"write_timeout_seconds"`
	IdleTimeoutSeconds       int                 `toml:"idle_timeout_seconds"`
	Tokens                   []AdminTokenSection `toml:"tokens"`
}

type AdminTokenSection struct {
	Name   string   `toml:"name"`
	Token  string   `toml:"token"`
	Role   string   `toml:"role"`
	Scopes []string `toml:"scopes"`
}

type MetricsSection struct {
	PrometheusEnabled bool `toml:"prometheus_enabled"`
	RequireAdminToken bool `toml:"require_admin_token"`
}

type TracingSection struct {
	Enabled  bool `toml:"enabled"`
	MaxSpans int  `toml:"max_spans"`
}

type OTelSection struct {
	Enabled             bool    `toml:"enabled"`
	Endpoint            string  `toml:"endpoint"`
	Insecure            bool    `toml:"insecure"`
	ServiceName         string  `toml:"service_name"`
	SampleRatio         float64 `toml:"sample_ratio"`
	BatchTimeoutSeconds int     `toml:"batch_timeout_seconds"`
}

type DebugSection struct {
	PprofEnabled bool `toml:"pprof_enabled"`
}

type DataTablesSection struct {
	Enabled   bool   `toml:"enabled"`
	Directory string `toml:"directory"`
	Version   string `toml:"version"`
}

type I18NSection struct {
	Enabled         bool   `toml:"enabled"`
	Directory       string `toml:"directory"`
	DefaultLanguage string `toml:"default_language"`
	Version         string `toml:"version"`
}

type AuditSection struct {
	Enabled     bool   `toml:"enabled"`
	Kind        string `toml:"kind"`
	FilePath    string `toml:"file_path"`
	MemoryLimit int    `toml:"memory_limit"`
}

type LogicLogSection struct {
	Enabled     bool   `toml:"enabled"`
	Kind        string `toml:"kind"`
	FilePath    string `toml:"file_path"`
	ReasonsPath string `toml:"reasons_path"`
	MemoryLimit int    `toml:"memory_limit"`
}

type BILogSection struct {
	Enabled     bool   `toml:"enabled"`
	Kind        string `toml:"kind"`
	FilePath    string `toml:"file_path"`
	SchemaPath  string `toml:"schema_path"`
	MemoryLimit int    `toml:"memory_limit"`
}

type EventLogSection struct {
	Enabled                bool              `toml:"enabled"`
	StoreKind              string            `toml:"store_kind"`
	MySQLDSN               string            `toml:"mysql_dsn"`
	MySQLTable             string            `toml:"mysql_table"`
	MySQLMaxOpenConns      int               `toml:"mysql_max_open_conns"`
	MySQLMaxIdleConns      int               `toml:"mysql_max_idle_conns"`
	MySQLConnMaxLifetime   int               `toml:"mysql_conn_max_lifetime_seconds"`
	MySQLEnsureSchema      bool              `toml:"mysql_ensure_schema"`
	AdminPrefix            string            `toml:"admin_prefix"`
	Strict                 bool              `toml:"strict"`
	PublishEnabled         bool              `toml:"publish_enabled"`
	PublishIntervalSeconds int               `toml:"publish_interval_seconds"`
	PublishBatchSize       int               `toml:"publish_batch_size"`
	PublishRetrySeconds    int               `toml:"publish_retry_seconds"`
	PublishMaxAttempts     int               `toml:"publish_max_attempts"`
	PublishKind            string            `toml:"publish_kind"`
	PublishHTTPURL         string            `toml:"publish_http_url"`
	PublishHTTPMethod      string            `toml:"publish_http_method"`
	PublishHTTPTimeout     int               `toml:"publish_http_timeout_seconds"`
	PublishHTTPPayload     string            `toml:"publish_http_payload"`
	PublishHTTPHeaders     map[string]string `toml:"publish_http_headers"`
}

type AccountCenterSection struct {
	Enabled           bool   `toml:"enabled"`
	StoreKind         string `toml:"store_kind"`
	MySQLDSN          string `toml:"mysql_dsn"`
	MySQLTable        string `toml:"mysql_table"`
	MySQLEnsureSchema bool   `toml:"mysql_ensure_schema"`
	AdminPrefix       string `toml:"admin_prefix"`
}

type StorageObjectSection struct {
	Enabled           bool   `toml:"enabled"`
	StoreKind         string `toml:"store_kind"`
	MySQLDSN          string `toml:"mysql_dsn"`
	MySQLTable        string `toml:"mysql_table"`
	MySQLEnsureSchema bool   `toml:"mysql_ensure_schema"`
	AdminPrefix       string `toml:"admin_prefix"`
}

type BattleAgentSection struct {
	SettlementOutboxKind   string `toml:"settlement_outbox_kind"`
	SettlementOutboxStream string `toml:"settlement_outbox_stream"`
	SettlementOutboxType   string `toml:"settlement_outbox_type"`
}

type ReloadSection struct {
	Enabled             bool `toml:"enabled"`
	PollIntervalSeconds int  `toml:"poll_interval_seconds"`
	ApplyTimeoutSeconds int  `toml:"apply_timeout_seconds"`
}

type HotUpdateSection struct {
	Enabled                    bool     `toml:"enabled"`
	Target                     string   `toml:"target"`
	ControlKind                string   `toml:"control_kind"`
	SourceKind                 string   `toml:"source_kind"`
	ApplierKind                string   `toml:"applier_kind"`
	Workspace                  string   `toml:"workspace"`
	ApplyTimeoutSeconds        int      `toml:"apply_timeout_seconds"`
	SigningKeyEnv              string   `toml:"signing_key_env"`
	RequireSignature           bool     `toml:"require_signature"`
	NativePatchAllowedTargets  []string `toml:"native_patch_allowed_targets"`
	PatchOldSymbols            []string `toml:"native_patch_allowed_old_symbols"`
	NativePatchRequireBuildID  bool     `toml:"native_patch_require_build_id"`
	PatchRequireActor          bool     `toml:"native_patch_require_requested_by"`
	NativePatchRequireReason   bool     `toml:"native_patch_require_reason"`
	NativePatchMinReasonLength int      `toml:"native_patch_min_reason_length"`
	NativePatchMaxSymbols      int      `toml:"native_patch_max_symbols"`
	NativePatchMaxLiveSeconds  int      `toml:"native_patch_max_live_seconds"`
}

type StateSyncSection struct {
	Enabled                bool   `toml:"enabled"`
	Kind                   string `toml:"kind"`
	Namespace              string `toml:"namespace"`
	TTLSeconds             int    `toml:"ttl_seconds"`
	CleanupIntervalSeconds int    `toml:"cleanup_interval_seconds"`
	MaxPayloadBytes        int    `toml:"max_payload_bytes"`
}

type ServerGroupSection struct {
	Enabled            bool   `toml:"enabled"`
	StoreKind          string `toml:"store_kind"`
	PlanFile           string `toml:"plan_file"`
	MergeArchiveDir    string `toml:"merge_archive_dir"`
	AdminPrefix        string `toml:"admin_prefix"`
	InZoneDelaySeconds int    `toml:"in_zone_delay_seconds"`
	NoticeLeadSeconds  int    `toml:"notice_lead_seconds"`
}

type TopologySection struct {
	Enabled                bool     `toml:"enabled"`
	Services               []string `toml:"services"`
	GatewayService         string   `toml:"gateway_service"`
	GatewayBackendService  string   `toml:"gateway_backend_service"`
	RejectMaintaining      bool     `toml:"reject_maintaining"`
	RefreshIntervalSeconds int      `toml:"refresh_interval_seconds"`
}

type DiscoverySection struct {
	CacheTTLSeconds   int    `toml:"cache_ttl_seconds"`
	FailureTTLSeconds int    `toml:"failure_ttl_seconds"`
	FailureThreshold  int    `toml:"failure_threshold"`
	AllowMaintaining  bool   `toml:"allow_maintaining"`
	Strategy          string `toml:"strategy"`
}

type RPCSection struct {
	CallTimeoutSeconds int `toml:"call_timeout_seconds"`
	MaxPending         int `toml:"max_pending"`
	MaxPayloadBytes    int `toml:"max_payload_bytes"`
}

type RateLimitSection struct {
	Enabled                bool   `toml:"enabled"`
	Algorithm              string `toml:"algorithm"`
	WindowSeconds          int    `toml:"window_seconds"`
	MaxRequests            int    `toml:"max_requests"`
	Rules                  string `toml:"rules"`
	CleanupIntervalSeconds int    `toml:"cleanup_interval_seconds"`
	TrustedHeader          string `toml:"trusted_header"`
}

type IdempotencySection struct {
	Kind                    string `toml:"kind"`
	TTLSeconds              int    `toml:"ttl_seconds"`
	KeyPrefix               string `toml:"key_prefix"`
	RedisAddress            string `toml:"redis_address"`
	RedisPassword           string `toml:"redis_password"`
	RedisDB                 int    `toml:"redis_db"`
	RedisPoolSize           int    `toml:"redis_pool_size"`
	RedisTimeoutSeconds     int    `toml:"redis_timeout_seconds"`
	RedisConnectTimeoutSecs int    `toml:"redis_connect_timeout_seconds"`
	RedisReadTimeoutSecs    int    `toml:"redis_read_timeout_seconds"`
	RedisWriteTimeoutSecs   int    `toml:"redis_write_timeout_seconds"`
	MySQLDSN                string `toml:"mysql_dsn"`
	MySQLMaxOpenConns       int    `toml:"mysql_max_open_conns"`
	MySQLMaxIdleConns       int    `toml:"mysql_max_idle_conns"`
	MySQLConnMaxLifetimeSec int    `toml:"mysql_conn_max_lifetime_seconds"`
}

type RegistrySection struct {
	Kind          string   `toml:"kind"`
	Namespace     string   `toml:"namespace"`
	Endpoints     []string `toml:"endpoints"`
	LeaseTTL      int64    `toml:"lease_ttl"`
	RedisPassword string   `toml:"redis_password"`
	RedisDB       int      `toml:"redis_db"`
}

type BusSection struct {
	Kind                     string   `toml:"kind"`
	Namespace                string   `toml:"namespace"`
	Endpoints                []string `toml:"endpoints"`
	RedisUsername            string   `toml:"redis_username"`
	RedisPassword            string   `toml:"redis_password"`
	RedisDB                  int      `toml:"redis_db"`
	RedisTLSEnabled          bool     `toml:"redis_tls_enabled"`
	RedisTLSServerName       string   `toml:"redis_tls_server_name"`
	NATSToken                string   `toml:"nats_token"`
	NATSUsername             string   `toml:"nats_username"`
	NATSPassword             string   `toml:"nats_password"`
	NATSCredentials          string   `toml:"nats_credentials"`
	NATSTLSEnabled           bool     `toml:"nats_tls_enabled"`
	NATSTLSServerName        string   `toml:"nats_tls_server_name"`
	NATSCAFile               string   `toml:"nats_ca_file"`
	NATSCertFile             string   `toml:"nats_cert_file"`
	NATSKeyFile              string   `toml:"nats_key_file"`
	NATSName                 string   `toml:"nats_name"`
	NATSTimeoutSeconds       int      `toml:"nats_timeout_seconds"`
	NATSPingIntervalSeconds  int      `toml:"nats_ping_interval_seconds"`
	NATSMaxPingsOutstanding  int      `toml:"nats_max_pings_outstanding"`
	NATSMaxReconnects        int      `toml:"nats_max_reconnects"`
	NATSReconnectWaitSeconds int      `toml:"nats_reconnect_wait_seconds"`
	NATSWireEncoding         string   `toml:"nats_wire_encoding"`
}

type CacheSection struct {
	Kind                string `toml:"kind"`
	KeyPrefix           string `toml:"key_prefix"`
	DefaultTTLSeconds   int    `toml:"default_ttl_seconds"`
	MaxEntries          int    `toml:"max_entries"`
	RedisAddress        string `toml:"redis_address"`
	RedisPassword       string `toml:"redis_password"`
	RedisDB             int    `toml:"redis_db"`
	RedisPoolSize       int    `toml:"redis_pool_size"`
	RedisTimeoutSeconds int    `toml:"redis_timeout_seconds"`
	RedisConnectTimeout int    `toml:"redis_connect_timeout_seconds"`
	RedisReadTimeout    int    `toml:"redis_read_timeout_seconds"`
	RedisWriteTimeout   int    `toml:"redis_write_timeout_seconds"`
}

type LockSection struct {
	Kind                string `toml:"kind"`
	KeyPrefix           string `toml:"key_prefix"`
	TTLSeconds          int    `toml:"ttl_seconds"`
	RedisAddress        string `toml:"redis_address"`
	RedisPassword       string `toml:"redis_password"`
	RedisDB             int    `toml:"redis_db"`
	RedisPoolSize       int    `toml:"redis_pool_size"`
	RedisTimeoutSeconds int    `toml:"redis_timeout_seconds"`
	RedisConnectTimeout int    `toml:"redis_connect_timeout_seconds"`
	RedisReadTimeout    int    `toml:"redis_read_timeout_seconds"`
	RedisWriteTimeout   int    `toml:"redis_write_timeout_seconds"`
}

type PresenceSection struct {
	Kind                string `toml:"kind"`
	KeyPrefix           string `toml:"key_prefix"`
	TTLSeconds          int    `toml:"ttl_seconds"`
	RedisAddress        string `toml:"redis_address"`
	RedisPassword       string `toml:"redis_password"`
	RedisDB             int    `toml:"redis_db"`
	RedisPoolSize       int    `toml:"redis_pool_size"`
	RedisTimeoutSeconds int    `toml:"redis_timeout_seconds"`
	RedisConnectTimeout int    `toml:"redis_connect_timeout_seconds"`
	RedisReadTimeout    int    `toml:"redis_read_timeout_seconds"`
	RedisWriteTimeout   int    `toml:"redis_write_timeout_seconds"`
}

type WorkerSection struct {
	Size  int `toml:"size"`
	Queue int `toml:"queue"`
}

type ProfileStoreSection struct {
	StoreKind                  string              `toml:"store_kind"`
	StoreDirectory             string              `toml:"store_directory"`
	StoreAddress               string              `toml:"store_address"`
	StorePassword              string              `toml:"store_password"`
	StoreDB                    int                 `toml:"store_db"`
	StoreKeyPrefix             string              `toml:"store_key_prefix"`
	StorePoolSize              int                 `toml:"store_pool_size"`
	StoreTimeoutSeconds        int                 `toml:"store_timeout_seconds"`
	StoreConnectTimeout        int                 `toml:"store_connect_timeout_seconds"`
	StoreReadTimeout           int                 `toml:"store_read_timeout_seconds"`
	StoreWriteTimeout          int                 `toml:"store_write_timeout_seconds"`
	StoreTTLSeconds            int                 `toml:"store_ttl_seconds"`
	MySQLDSN                   string              `toml:"mysql_dsn"`
	MySQLMaxOpenConns          int                 `toml:"mysql_max_open_conns"`
	MySQLMaxIdleConns          int                 `toml:"mysql_max_idle_conns"`
	MySQLConnMaxLifetime       int                 `toml:"mysql_conn_max_lifetime_seconds"`
	MySQLShardingEnabled       bool                `toml:"mysql_sharding_enabled"`
	MySQLShards                []ProfileMySQLShard `toml:"mysql_shards"`
	SummaryStoreKind           string              `toml:"summary_store_kind"`
	SummaryStoreAddress        string              `toml:"summary_store_address"`
	SummaryStorePassword       string              `toml:"summary_store_password"`
	SummaryStoreDB             int                 `toml:"summary_store_db"`
	SummaryStoreKeyPrefix      string              `toml:"summary_store_key_prefix"`
	SummaryStorePoolSize       int                 `toml:"summary_store_pool_size"`
	SummaryStoreTimeout        int                 `toml:"summary_store_timeout_seconds"`
	SummaryStoreConnectTimeout int                 `toml:"summary_store_connect_timeout_seconds"`
	SummaryStoreReadTimeout    int                 `toml:"summary_store_read_timeout_seconds"`
	SummaryStoreWriteTimeout   int                 `toml:"summary_store_write_timeout_seconds"`
	SummaryStoreTTL            int                 `toml:"summary_store_ttl_seconds"`
	SaveMode                   string              `toml:"save_mode"`
	AsyncFlushIntervalSeconds  int                 `toml:"async_flush_interval_seconds"`
	AsyncMaxPending            int                 `toml:"async_max_pending"`
	AsyncRetryBackoffSeconds   int                 `toml:"async_retry_backoff_seconds"`
	AsyncMaxRetries            int                 `toml:"async_max_retries"`
	AsyncAutoExpireSeconds     int                 `toml:"async_auto_expire_seconds"`
	AsyncDeadLetterLimit       int                 `toml:"async_dead_letter_limit"`
	AsyncDeadLetterDirectory   string              `toml:"async_dead_letter_directory"`
}

type ProfileMySQLShard struct {
	ID              string `toml:"id"`
	DSN             string `toml:"dsn"`
	TableName       string `toml:"table_name"`
	TablePrefix     string `toml:"table_prefix"`
	HashSlots       string `toml:"hash_slots"`
	MaxOpenConns    int    `toml:"max_open_conns"`
	MaxIdleConns    int    `toml:"max_idle_conns"`
	ConnMaxLifetime int    `toml:"conn_max_lifetime_seconds"`
}

type GatewaySection struct {
	AuthListen                 string   `toml:"auth_listen"`
	AuthMode                   string   `toml:"auth_mode"`
	SharedLoginTokens          bool     `toml:"shared_login_tokens"`
	RegistryGateSelection      bool     `toml:"registry_gate_selection"`
	RemoteGateSelection        bool     `toml:"remote_gate_selection"`
	GateRouteFeature           string   `toml:"gate_route_feature"`
	GateService                string   `toml:"gate_service"`
	TCPListen                  string   `toml:"tcp_listen"`
	TCPProxyProtocolEnabled    bool     `toml:"tcp_proxy_protocol_enabled"`
	TCPProxyTrustedCIDRs       []string `toml:"tcp_proxy_trusted_cidrs"`
	TCPProxyHeaderSec          int      `toml:"tcp_proxy_header_timeout_seconds"`
	KCPEnabled                 bool     `toml:"kcp_enabled"`
	KCPListen                  string   `toml:"kcp_listen"`
	KCPNoDelay                 int      `toml:"kcp_nodelay"`
	KCPInterval                int      `toml:"kcp_interval"`
	KCPResend                  int      `toml:"kcp_resend"`
	KCPNoCongestion            int      `toml:"kcp_no_congestion"`
	KCPWindowSize              int      `toml:"kcp_window_size"`
	KCPDataShards              int      `toml:"kcp_data_shards"`
	KCPParityShards            int      `toml:"kcp_parity_shards"`
	QUICEnabled                bool     `toml:"quic_enabled"`
	QUICListen                 string   `toml:"quic_listen"`
	QUICCertFile               string   `toml:"quic_cert_file"`
	QUICKeyFile                string   `toml:"quic_key_file"`
	QUICHandshakeSec           int      `toml:"quic_handshake_timeout_seconds"`
	QUICMaxIdleTimeoutSeconds  int      `toml:"quic_max_idle_timeout_seconds"`
	QUICKeepAliveSeconds       int      `toml:"quic_keep_alive_seconds"`
	QUICMaxIncomingStreams     int64    `toml:"quic_max_incoming_streams"`
	WebSocketEnabled           bool     `toml:"websocket_enabled"`
	WebSocketListen            string   `toml:"websocket_listen"`
	WebSocketPath              string   `toml:"websocket_path"`
	WebSocketMaxPayloadBytes   int      `toml:"websocket_max_payload_bytes"`
	WebSocketHeartbeatSeconds  int      `toml:"websocket_heartbeat_seconds"`
	ChatListen                 string   `toml:"chat_listen"`
	BackendService             string   `toml:"backend_service"`
	BackendPollIntervalSeconds int      `toml:"backend_poll_interval_seconds"`
	RejectMaintainingBackends  bool     `toml:"reject_maintaining_backends"`
	MaxConnections             int      `toml:"max_connections"`
	ConnectionRateLimitEnabled bool     `toml:"connection_rate_limit_enabled"`
	ConnRateWindowSec          int      `toml:"connection_rate_limit_window_seconds"`
	ConnectionRateLimitMax     int      `toml:"connection_rate_limit_max"`
	ConnRateMaxEntries         int      `toml:"connection_rate_limit_max_entries"`
	ConnRateIPv4Prefix         int      `toml:"connection_rate_limit_ipv4_prefix"`
	ConnRateIPv6Prefix         int      `toml:"connection_rate_limit_ipv6_prefix"`
	SessionIdleTimeoutSeconds  int      `toml:"session_idle_timeout_seconds"`
	SessionSweepSec            int      `toml:"session_sweep_interval_seconds"`
	SessionHookTimeoutSeconds  int      `toml:"session_hook_timeout_seconds"`
	SessionKeyMasterSecret     string   `toml:"session_key_master_secret"`
	AccountID                  string   `toml:"account_id"`
	AccountNumID               int32    `toml:"account_num_id"`
	AuthToken                  string   `toml:"auth_token"`
	LoginToken                 string   `toml:"login_token"`
	ServerVersion              string   `toml:"server_version"`
	ShardID                    int32    `toml:"shard_id"`
	ShardName                  string   `toml:"shard_name"`
	ShardDisplay               string   `toml:"shard_display"`
	ShardState                 int32    `toml:"shard_state"`
	Recommend                  int32    `toml:"recommend"`
	RecommendLang              string   `toml:"recommend_lang"`
	VirtualGID                 int32    `toml:"virtual_gid"`
	ProductIDs                 []string `toml:"product_ids"`
	HasRole                    bool     `toml:"has_role"`
	SendCreatePush             bool     `toml:"send_create_profile_push"`
}
