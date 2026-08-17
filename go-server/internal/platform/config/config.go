package config

import (
	"fmt"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

const defaultWSPayloadMax = 1024 * 1024
const defaultWSHBSeconds = 10
const defaultRPCPayloadMax = 1024 * 1024
const defaultKCPWindowSize = 128
const defaultQUICStreams int64 = 128
const defRateWindow = 10
const defaultConnRateMax = 60
const defConnMaxEntries = 4096
const defConnIPv4Prefix = 32
const defConnIPv6Prefix = 64
const defaultEventLogTable = "outbox_events"
const defaultAccountTable = "account_center_state"
const defaultObjectTable = "storage_objects"
const defaultOutboxStream = "battle.settlement"
const defaultOutboxType = "battle.settlement.created"

func Load(path, defaultName string) (ServiceConfig, error) {
	var cfg ServiceConfig
	meta, err := toml.DecodeFile(path, &cfg)
	if err != nil {
		return ServiceConfig{}, err
	}
	if keys := undecodedTOMLKeys(meta); len(keys) > 0 {
		return ServiceConfig{}, fmt.Errorf("unknown config keys: %s", strings.Join(keys, ", "))
	}
	expandEnvValues(&cfg)
	cfg.Normalize(defaultName)
	if err := cfg.Validate(); err != nil {
		return ServiceConfig{}, err
	}
	return cfg, nil
}

func undecodedTOMLKeys(meta toml.MetaData) []string {
	undecoded := meta.Undecoded()
	if len(undecoded) == 0 {
		return nil
	}
	keys := make([]string, 0, len(undecoded))
	for _, key := range undecoded {
		keys = append(keys, key.String())
	}
	slices.Sort(keys)
	return keys
}

func (c *ServiceConfig) Normalize(defaultName string) {
	c.trim()
	if c.Service.Name == "" {
		c.Service.Name = defaultName
	}
	if c.Service.NodeID == "" {
		c.Service.NodeID = c.Service.Name + "-local-1"
	}
	if c.Service.Environment == "" {
		c.Service.Environment = "local"
	}
	if c.Service.StopTimeoutSeconds == 0 {
		c.Service.StopTimeoutSeconds = 10
	}
	if c.Service.StopInitialGraceSeconds == 0 {
		c.Service.StopInitialGraceSeconds = 2
	}
	if c.Service.StopProbeSeconds == 0 {
		c.Service.StopProbeSeconds = 1
	}
	if c.Service.StopStableSeconds == 0 {
		c.Service.StopStableSeconds = 5
	}
	if c.Cluster.ID == "" {
		c.Cluster.ID = "local"
	}
	if c.Cluster.VirtualGID == 0 {
		c.Cluster.VirtualGID = c.Cluster.GID
	}
	if c.Cluster.Zone == "" {
		c.Cluster.Zone = c.Service.Environment
	}
	if c.Cluster.Version == "" {
		c.Cluster.Version = "local"
	}
	if c.Cluster.DataVersion == "" {
		c.Cluster.DataVersion = "local"
	}
	if c.Cluster.HeartbeatIntervalSeconds == 0 {
		c.Cluster.HeartbeatIntervalSeconds = 10
	}
	if c.Admin.Listen == "" {
		c.Admin.Listen = ":18080"
	}
	for idx := range c.Admin.Tokens {
		if c.Admin.Tokens[idx].Role == "" {
			c.Admin.Tokens[idx].Role = "operator"
		}
	}
	if c.Admin.ReadTimeoutSeconds == 0 {
		c.Admin.ReadTimeoutSeconds = 10
	}
	if c.Admin.ReadHeaderTimeoutSeconds == 0 {
		c.Admin.ReadHeaderTimeoutSeconds = 5
	}
	if c.Admin.WriteTimeoutSeconds == 0 {
		c.Admin.WriteTimeoutSeconds = 15
	}
	if c.Admin.IdleTimeoutSeconds == 0 {
		c.Admin.IdleTimeoutSeconds = 60
	}
	if c.RateLimit.Algorithm == "" {
		c.RateLimit.Algorithm = "window"
	}
	if c.RateLimit.WindowSeconds == 0 {
		c.RateLimit.WindowSeconds = 1
	}
	if c.RateLimit.MaxRequests == 0 {
		c.RateLimit.MaxRequests = 60
	}
	if c.RateLimit.CleanupIntervalSeconds == 0 {
		c.RateLimit.CleanupIntervalSeconds = 60
	}
	if c.Tracing.MaxSpans == 0 {
		c.Tracing.MaxSpans = 256
	}
	if c.OTel.Endpoint == "" {
		c.OTel.Endpoint = "127.0.0.1:4318"
	}
	if c.OTel.ServiceName == "" {
		c.OTel.ServiceName = c.Service.Name
	}
	if c.OTel.SampleRatio == 0 {
		c.OTel.SampleRatio = 1
	}
	if c.OTel.BatchTimeoutSeconds == 0 {
		c.OTel.BatchTimeoutSeconds = 5
	}
	if c.DataTables.Directory == "" {
		c.DataTables.Directory = "configs/tables"
	}
	if c.DataTables.Version == "" {
		c.DataTables.Version = c.Cluster.DataVersion
	}
	if c.I18N.Directory == "" {
		c.I18N.Directory = "configs/i18n"
	}
	if c.I18N.DefaultLanguage == "" {
		c.I18N.DefaultLanguage = "zh-CN"
	}
	if c.I18N.Version == "" {
		c.I18N.Version = c.Cluster.DataVersion
	}
	c.normalizePVF()
	if c.Audit.Kind == "" {
		c.Audit.Kind = "memory"
	}
	if c.Audit.FilePath == "" {
		c.Audit.FilePath = "data/audit/events.jsonl"
	}
	if c.Audit.MemoryLimit == 0 {
		c.Audit.MemoryLimit = 256
	}
	if c.LogicLog.Kind == "" {
		c.LogicLog.Kind = "memory"
	}
	if c.LogicLog.FilePath == "" {
		c.LogicLog.FilePath = "data/logiclog/events.jsonl"
	}
	if c.LogicLog.ReasonsPath == "" {
		c.LogicLog.ReasonsPath = "configs/logiclog/reasons.csv"
	}
	if c.LogicLog.MemoryLimit == 0 {
		c.LogicLog.MemoryLimit = 256
	}
	if c.BILog.Kind == "" {
		c.BILog.Kind = "memory"
	}
	if c.BILog.FilePath == "" {
		c.BILog.FilePath = "data/bilog/events.jsonl"
	}
	if c.BILog.SchemaPath == "" {
		c.BILog.SchemaPath = "configs/bilog/schema.json"
	}
	if c.BILog.MemoryLimit == 0 {
		c.BILog.MemoryLimit = 256
	}
	if c.Bus.NATSName == "" {
		c.Bus.NATSName = "longheng-bus"
	}
	if c.Bus.NATSTimeoutSeconds == 0 {
		c.Bus.NATSTimeoutSeconds = 5
	}
	if c.Bus.NATSPingIntervalSeconds == 0 {
		c.Bus.NATSPingIntervalSeconds = 1
	}
	if c.Bus.NATSMaxPingsOutstanding == 0 {
		c.Bus.NATSMaxPingsOutstanding = 10
	}
	if c.Bus.NATSMaxReconnects == 0 {
		c.Bus.NATSMaxReconnects = -1
	}
	if c.Bus.NATSReconnectWaitSeconds == 0 {
		c.Bus.NATSReconnectWaitSeconds = 1
	}
	if c.Bus.NATSWireEncoding == "" {
		c.Bus.NATSWireEncoding = "binary_v1"
	}
	if c.EventLog.StoreKind == "" {
		c.EventLog.StoreKind = "memory"
	}
	if c.EventLog.MySQLTable == "" {
		c.EventLog.MySQLTable = defaultEventLogTable
	}
	if c.EventLog.MySQLMaxOpenConns == 0 {
		c.EventLog.MySQLMaxOpenConns = 32
	}
	if c.EventLog.MySQLMaxIdleConns == 0 {
		c.EventLog.MySQLMaxIdleConns = 8
	}
	if c.EventLog.MySQLConnMaxLifetime == 0 {
		c.EventLog.MySQLConnMaxLifetime = 300
	}
	if c.EventLog.AdminPrefix == "" {
		c.EventLog.AdminPrefix = "/eventlog"
	}
	if c.EventLog.PublishIntervalSeconds == 0 {
		c.EventLog.PublishIntervalSeconds = 1
	}
	if c.EventLog.PublishBatchSize == 0 {
		c.EventLog.PublishBatchSize = 100
	}
	if c.EventLog.PublishRetrySeconds == 0 {
		c.EventLog.PublishRetrySeconds = 1
	}
	if c.EventLog.PublishMaxAttempts == 0 {
		c.EventLog.PublishMaxAttempts = 3
	}
	if c.EventLog.PublishKind == "" {
		c.EventLog.PublishKind = "bus"
	}
	if c.EventLog.PublishHTTPMethod == "" {
		c.EventLog.PublishHTTPMethod = "POST"
	}
	if c.EventLog.PublishHTTPTimeout == 0 {
		c.EventLog.PublishHTTPTimeout = 10
	}
	if c.EventLog.PublishHTTPPayload == "" {
		c.EventLog.PublishHTTPPayload = "event"
	}
	if c.AccountCenter.StoreKind == "" {
		c.AccountCenter.StoreKind = "memory"
	}
	if c.AccountCenter.MySQLTable == "" {
		c.AccountCenter.MySQLTable = defaultAccountTable
	}
	if c.AccountCenter.AdminPrefix == "" {
		c.AccountCenter.AdminPrefix = "/accountcenter"
	}
	if c.StorageObject.StoreKind == "" {
		c.StorageObject.StoreKind = "memory"
	}
	if c.StorageObject.MySQLTable == "" {
		c.StorageObject.MySQLTable = defaultObjectTable
	}
	if c.StorageObject.AdminPrefix == "" {
		c.StorageObject.AdminPrefix = "/storage"
	}
	if c.BattleAgent.SettlementOutboxKind == "" {
		c.BattleAgent.SettlementOutboxKind = "memory"
	}
	if c.BattleAgent.SettlementOutboxStream == "" {
		c.BattleAgent.SettlementOutboxStream = defaultOutboxStream
	}
	if c.BattleAgent.SettlementOutboxType == "" {
		c.BattleAgent.SettlementOutboxType = defaultOutboxType
	}
	if c.Reload.PollIntervalSeconds == 0 {
		c.Reload.PollIntervalSeconds = 5
	}
	if c.Reload.ApplyTimeoutSeconds == 0 {
		c.Reload.ApplyTimeoutSeconds = 5
	}
	if c.HotUpdate.Target == "" {
		c.HotUpdate.Target = c.Service.Name
	}
	if c.HotUpdate.ControlKind == "" {
		c.HotUpdate.ControlKind = "memory"
	}
	if c.HotUpdate.SourceKind == "" {
		c.HotUpdate.SourceKind = "local"
	}
	if c.HotUpdate.ApplierKind == "" {
		c.HotUpdate.ApplierKind = "datatable"
	}
	if c.HotUpdate.Workspace == "" {
		c.HotUpdate.Workspace = "data/hotupdate"
	}
	if c.HotUpdate.ApplyTimeoutSeconds == 0 {
		c.HotUpdate.ApplyTimeoutSeconds = 30
	}
	if c.HotUpdate.ApplierKind == "native_patch" && c.HotUpdate.NativePatchMaxLiveSeconds == 0 {
		c.HotUpdate.NativePatchMaxLiveSeconds = 300
	}
	if c.StateSync.Kind == "" {
		c.StateSync.Kind = "memory"
	}
	if c.StateSync.Namespace == "" {
		c.StateSync.Namespace = c.Cluster.ID
	}
	if c.StateSync.TTLSeconds == 0 {
		c.StateSync.TTLSeconds = 60
	}
	if c.StateSync.CleanupIntervalSeconds == 0 {
		c.StateSync.CleanupIntervalSeconds = 30
	}
	if c.StateSync.MaxPayloadBytes == 0 {
		c.StateSync.MaxPayloadBytes = 64 * 1024
	}
	if c.ServerGroup.StoreKind == "" {
		c.ServerGroup.StoreKind = "file"
	}
	if c.ServerGroup.PlanFile == "" {
		c.ServerGroup.PlanFile = "configs/servergroup/plan.json"
	}
	if c.ServerGroup.MergeArchiveDir == "" {
		c.ServerGroup.MergeArchiveDir = "data/servergroup/merge_archives"
	}
	if c.ServerGroup.AdminPrefix == "" {
		c.ServerGroup.AdminPrefix = "/debug/servergroup"
	}
	if c.ServerGroup.InZoneDelaySeconds == 0 {
		c.ServerGroup.InZoneDelaySeconds = 24 * 60 * 60
	}
	if c.ServerGroup.NoticeLeadSeconds == 0 {
		c.ServerGroup.NoticeLeadSeconds = 24 * 60 * 60
	}
	if len(c.Topology.Services) == 0 && c.Service.Name != "" {
		c.Topology.Services = []string{c.Service.Name}
	}
	if c.Topology.GatewayService == "" {
		c.Topology.GatewayService = "gateway"
	}
	if c.Topology.GatewayBackendService == "" {
		if c.Gateway.BackendService != "" {
			c.Topology.GatewayBackendService = c.Gateway.BackendService
		} else {
			c.Topology.GatewayBackendService = "logic"
		}
	}
	if c.Topology.RefreshIntervalSeconds == 0 {
		c.Topology.RefreshIntervalSeconds = 5
	}
	if c.Discovery.CacheTTLSeconds == 0 {
		c.Discovery.CacheTTLSeconds = 1
	}
	if c.Discovery.FailureTTLSeconds == 0 {
		c.Discovery.FailureTTLSeconds = 5
	}
	if c.Discovery.FailureThreshold == 0 {
		c.Discovery.FailureThreshold = 1
	}
	if c.Discovery.Strategy == "" {
		c.Discovery.Strategy = "hash"
	}
	if c.RPC.CallTimeoutSeconds == 0 {
		c.RPC.CallTimeoutSeconds = 3
	}
	if c.RPC.MaxPending == 0 {
		c.RPC.MaxPending = 1024
	}
	if c.RPC.MaxPayloadBytes == 0 {
		c.RPC.MaxPayloadBytes = defaultRPCPayloadMax
	}
	if c.Idempotency.Kind == "" {
		c.Idempotency.Kind = "memory"
	}
	if c.Idempotency.TTLSeconds == 0 {
		c.Idempotency.TTLSeconds = 600
	}
	if c.Idempotency.KeyPrefix == "" {
		c.Idempotency.KeyPrefix = "idempotency"
	}
	if c.Idempotency.RedisAddress == "" {
		c.Idempotency.RedisAddress = "127.0.0.1:6379"
	}
	if c.Idempotency.RedisPoolSize == 0 {
		c.Idempotency.RedisPoolSize = 8
	}
	if c.Idempotency.RedisTimeoutSeconds == 0 {
		c.Idempotency.RedisTimeoutSeconds = 2
	}
	if c.Idempotency.RedisConnectTimeoutSecs == 0 {
		c.Idempotency.RedisConnectTimeoutSecs = c.Idempotency.RedisTimeoutSeconds
	}
	if c.Idempotency.RedisReadTimeoutSecs == 0 {
		c.Idempotency.RedisReadTimeoutSecs = c.Idempotency.RedisTimeoutSeconds
	}
	if c.Idempotency.RedisWriteTimeoutSecs == 0 {
		c.Idempotency.RedisWriteTimeoutSecs = c.Idempotency.RedisTimeoutSeconds
	}
	if c.Idempotency.MySQLDSN == "" {
		c.Idempotency.MySQLDSN = "longheng:longheng@tcp(127.0.0.1:3306)/longheng?parseTime=true&charset=utf8mb4,utf8"
	}
	if c.Idempotency.MySQLMaxOpenConns == 0 {
		c.Idempotency.MySQLMaxOpenConns = 32
	}
	if c.Idempotency.MySQLMaxIdleConns == 0 {
		c.Idempotency.MySQLMaxIdleConns = 8
	}
	if c.Idempotency.MySQLConnMaxLifetimeSec == 0 {
		c.Idempotency.MySQLConnMaxLifetimeSec = 300
	}
	if c.Registry.Kind == "" {
		c.Registry.Kind = "memory"
	}
	if c.Registry.Namespace == "" {
		c.Registry.Namespace = "longheng"
	}
	if c.Registry.LeaseTTL == 0 {
		c.Registry.LeaseTTL = 15
	}
	if c.Bus.Kind == "" {
		c.Bus.Kind = "memory"
	}
	if c.Bus.Namespace == "" {
		c.Bus.Namespace = "longheng"
	}
	if c.Cache.Kind == "" {
		c.Cache.Kind = "memory"
	}
	if c.Cache.KeyPrefix == "" {
		c.Cache.KeyPrefix = "cache"
	}
	if c.Cache.DefaultTTLSeconds == 0 {
		c.Cache.DefaultTTLSeconds = 60
	}
	if c.Cache.MaxEntries == 0 {
		c.Cache.MaxEntries = 10000
	}
	if c.Cache.RedisAddress == "" {
		c.Cache.RedisAddress = "127.0.0.1:6379"
	}
	if c.Cache.RedisPoolSize == 0 {
		c.Cache.RedisPoolSize = 8
	}
	if c.Cache.RedisTimeoutSeconds == 0 {
		c.Cache.RedisTimeoutSeconds = 2
	}
	if c.Cache.RedisConnectTimeout == 0 {
		c.Cache.RedisConnectTimeout = c.Cache.RedisTimeoutSeconds
	}
	if c.Cache.RedisReadTimeout == 0 {
		c.Cache.RedisReadTimeout = c.Cache.RedisTimeoutSeconds
	}
	if c.Cache.RedisWriteTimeout == 0 {
		c.Cache.RedisWriteTimeout = c.Cache.RedisTimeoutSeconds
	}
	if c.Lock.Kind == "" {
		c.Lock.Kind = "memory"
	}
	if c.Lock.KeyPrefix == "" {
		c.Lock.KeyPrefix = "lock"
	}
	if c.Lock.TTLSeconds == 0 {
		c.Lock.TTLSeconds = 10
	}
	if c.Lock.RedisAddress == "" {
		c.Lock.RedisAddress = "127.0.0.1:6379"
	}
	if c.Lock.RedisPoolSize == 0 {
		c.Lock.RedisPoolSize = 8
	}
	if c.Lock.RedisTimeoutSeconds == 0 {
		c.Lock.RedisTimeoutSeconds = 2
	}
	if c.Lock.RedisConnectTimeout == 0 {
		c.Lock.RedisConnectTimeout = c.Lock.RedisTimeoutSeconds
	}
	if c.Lock.RedisReadTimeout == 0 {
		c.Lock.RedisReadTimeout = c.Lock.RedisTimeoutSeconds
	}
	if c.Lock.RedisWriteTimeout == 0 {
		c.Lock.RedisWriteTimeout = c.Lock.RedisTimeoutSeconds
	}
	if c.Presence.Kind == "" {
		c.Presence.Kind = "memory"
	}
	if c.Presence.KeyPrefix == "" {
		c.Presence.KeyPrefix = "presence"
	}
	if c.Presence.TTLSeconds == 0 {
		c.Presence.TTLSeconds = c.Gateway.SessionIdleTimeoutSeconds
		if c.Presence.TTLSeconds == 0 {
			c.Presence.TTLSeconds = 300
		}
	}
	if c.Presence.RedisAddress == "" {
		c.Presence.RedisAddress = "127.0.0.1:6379"
	}
	if c.Presence.RedisPoolSize == 0 {
		c.Presence.RedisPoolSize = 8
	}
	if c.Presence.RedisTimeoutSeconds == 0 {
		c.Presence.RedisTimeoutSeconds = 2
	}
	if c.Presence.RedisConnectTimeout == 0 {
		c.Presence.RedisConnectTimeout = c.Presence.RedisTimeoutSeconds
	}
	if c.Presence.RedisReadTimeout == 0 {
		c.Presence.RedisReadTimeout = c.Presence.RedisTimeoutSeconds
	}
	if c.Presence.RedisWriteTimeout == 0 {
		c.Presence.RedisWriteTimeout = c.Presence.RedisTimeoutSeconds
	}
	if c.Worker.Size == 0 {
		c.Worker.Size = 4
	}
	if c.Worker.Queue == 0 {
		c.Worker.Queue = 128
	}
	if c.ProfileStore.StoreKind == "" {
		c.ProfileStore.StoreKind = "memory"
	}
	if c.ProfileStore.StoreDirectory == "" {
		c.ProfileStore.StoreDirectory = "data/profiles"
	}
	if c.ProfileStore.StoreAddress == "" {
		c.ProfileStore.StoreAddress = "127.0.0.1:6379"
	}
	if c.ProfileStore.StoreKeyPrefix == "" {
		c.ProfileStore.StoreKeyPrefix = "profile"
	}
	if c.ProfileStore.StorePoolSize == 0 {
		c.ProfileStore.StorePoolSize = 8
	}
	if c.ProfileStore.StoreTimeoutSeconds == 0 {
		c.ProfileStore.StoreTimeoutSeconds = 2
	}
	if c.ProfileStore.StoreConnectTimeout == 0 {
		c.ProfileStore.StoreConnectTimeout = c.ProfileStore.StoreTimeoutSeconds
	}
	if c.ProfileStore.StoreReadTimeout == 0 {
		c.ProfileStore.StoreReadTimeout = c.ProfileStore.StoreTimeoutSeconds
	}
	if c.ProfileStore.StoreWriteTimeout == 0 {
		c.ProfileStore.StoreWriteTimeout = c.ProfileStore.StoreTimeoutSeconds
	}
	if c.ProfileStore.MySQLDSN == "" {
		c.ProfileStore.MySQLDSN = "longheng:longheng@tcp(127.0.0.1:3306)/longheng?parseTime=true&charset=utf8mb4,utf8"
	}
	if c.ProfileStore.MySQLMaxOpenConns == 0 {
		c.ProfileStore.MySQLMaxOpenConns = 32
	}
	if c.ProfileStore.MySQLMaxIdleConns == 0 {
		c.ProfileStore.MySQLMaxIdleConns = 8
	}
	if c.ProfileStore.MySQLConnMaxLifetime == 0 {
		c.ProfileStore.MySQLConnMaxLifetime = 300
	}
	if c.ProfileStore.SummaryStoreKind == "" {
		if c.ProfileStore.StoreKind == "mysql_redis" {
			c.ProfileStore.SummaryStoreKind = "redis"
		} else {
			c.ProfileStore.SummaryStoreKind = "memory"
		}
	}
	if c.ProfileStore.SummaryStoreAddress == "" {
		c.ProfileStore.SummaryStoreAddress = c.ProfileStore.StoreAddress
	}
	if c.ProfileStore.SummaryStoreKeyPrefix == "" {
		c.ProfileStore.SummaryStoreKeyPrefix = "profile_summary"
	}
	if c.ProfileStore.SummaryStorePoolSize == 0 {
		c.ProfileStore.SummaryStorePoolSize = c.ProfileStore.StorePoolSize
	}
	if c.ProfileStore.SummaryStoreTimeout == 0 {
		c.ProfileStore.SummaryStoreTimeout = c.ProfileStore.StoreTimeoutSeconds
	}
	if c.ProfileStore.SummaryStoreConnectTimeout == 0 {
		c.ProfileStore.SummaryStoreConnectTimeout = c.ProfileStore.SummaryStoreTimeout
	}
	if c.ProfileStore.SummaryStoreReadTimeout == 0 {
		c.ProfileStore.SummaryStoreReadTimeout = c.ProfileStore.SummaryStoreTimeout
	}
	if c.ProfileStore.SummaryStoreWriteTimeout == 0 {
		c.ProfileStore.SummaryStoreWriteTimeout = c.ProfileStore.SummaryStoreTimeout
	}
	if c.ProfileStore.SaveMode == "" {
		c.ProfileStore.SaveMode = "sync"
	}
	if c.ProfileStore.AsyncFlushIntervalSeconds == 0 {
		c.ProfileStore.AsyncFlushIntervalSeconds = 5
	}
	if c.ProfileStore.AsyncMaxPending == 0 {
		c.ProfileStore.AsyncMaxPending = 128
	}
	if c.ProfileStore.AsyncRetryBackoffSeconds == 0 {
		c.ProfileStore.AsyncRetryBackoffSeconds = 1
	}
	if c.ProfileStore.AsyncDeadLetterLimit == 0 {
		c.ProfileStore.AsyncDeadLetterLimit = 128
	}
	if c.ProfileStore.AsyncDeadLetterDirectory == "" {
		c.ProfileStore.AsyncDeadLetterDirectory = "data/profile_dead_letters"
	}
	if c.Gateway.AuthListen == "" {
		c.Gateway.AuthListen = ":8081"
	}
	if c.Gateway.AuthMode == "" {
		c.Gateway.AuthMode = "mock"
	}
	if c.Gateway.GateRouteFeature == "" {
		c.Gateway.GateRouteFeature = "gateway"
	}
	if c.Gateway.GateService == "" {
		if c.Topology.GatewayService != "" {
			c.Gateway.GateService = c.Topology.GatewayService
		} else {
			c.Gateway.GateService = c.Service.Name
		}
	}
	if c.Gateway.TCPListen == "" {
		c.Gateway.TCPListen = ":3341"
	}
	if c.Gateway.TCPProxyProtocolEnabled && c.Gateway.TCPProxyHeaderSec == 0 {
		c.Gateway.TCPProxyHeaderSec = 3
	}
	if c.Gateway.KCPEnabled {
		if c.Gateway.KCPListen == "" {
			c.Gateway.KCPListen = ":3342"
		}
		if c.Gateway.KCPNoDelay == 0 {
			c.Gateway.KCPNoDelay = 1
		}
		if c.Gateway.KCPInterval == 0 {
			c.Gateway.KCPInterval = 10
		}
		if c.Gateway.KCPResend == 0 {
			c.Gateway.KCPResend = 2
		}
		if c.Gateway.KCPNoCongestion == 0 {
			c.Gateway.KCPNoCongestion = 1
		}
		if c.Gateway.KCPWindowSize == 0 {
			c.Gateway.KCPWindowSize = defaultKCPWindowSize
		}
	}
	if c.Gateway.QUICEnabled {
		if c.Gateway.QUICListen == "" {
			c.Gateway.QUICListen = ":3343"
		}
		if c.Gateway.QUICHandshakeSec == 0 {
			c.Gateway.QUICHandshakeSec = 5
		}
		if c.Gateway.QUICMaxIdleTimeoutSeconds == 0 {
			c.Gateway.QUICMaxIdleTimeoutSeconds = c.Gateway.SessionIdleTimeoutSeconds
			if c.Gateway.QUICMaxIdleTimeoutSeconds == 0 {
				c.Gateway.QUICMaxIdleTimeoutSeconds = 300
			}
		}
		if c.Gateway.QUICKeepAliveSeconds == 0 {
			c.Gateway.QUICKeepAliveSeconds = 10
		}
		if c.Gateway.QUICMaxIncomingStreams == 0 {
			c.Gateway.QUICMaxIncomingStreams = defaultQUICStreams
		}
	}
	if c.Gateway.WebSocketPath == "" {
		c.Gateway.WebSocketPath = "/ws"
	}
	if c.Gateway.WebSocketEnabled && c.Gateway.WebSocketListen == "" {
		c.Gateway.WebSocketListen = ":8082"
	}
	if c.Gateway.WebSocketMaxPayloadBytes == 0 {
		c.Gateway.WebSocketMaxPayloadBytes = defaultWSPayloadMax
	}
	if c.Gateway.WebSocketHeartbeatSeconds == 0 {
		c.Gateway.WebSocketHeartbeatSeconds = defaultWSHBSeconds
	}
	if c.Gateway.ChatListen == "" {
		c.Gateway.ChatListen = "127.0.0.1:10001"
	}
	if c.Gateway.BackendService == "" {
		c.Gateway.BackendService = "logic"
	}
	if c.Gateway.BackendPollIntervalSeconds == 0 {
		c.Gateway.BackendPollIntervalSeconds = 5
	}
	if c.Gateway.MaxConnections == 0 {
		c.Gateway.MaxConnections = 1024
	}
	if c.Gateway.ConnRateWindowSec == 0 {
		c.Gateway.ConnRateWindowSec = defRateWindow
	}
	if c.Gateway.ConnectionRateLimitMax == 0 {
		c.Gateway.ConnectionRateLimitMax = defaultConnRateMax
	}
	if c.Gateway.ConnRateMaxEntries == 0 {
		c.Gateway.ConnRateMaxEntries = defConnMaxEntries
	}
	if c.Gateway.ConnRateIPv4Prefix == 0 {
		c.Gateway.ConnRateIPv4Prefix = defConnIPv4Prefix
	}
	if c.Gateway.ConnRateIPv6Prefix == 0 {
		c.Gateway.ConnRateIPv6Prefix = defConnIPv6Prefix
	}
	if c.Gateway.SessionIdleTimeoutSeconds == 0 {
		c.Gateway.SessionIdleTimeoutSeconds = 300
	}
	if c.Gateway.SessionSweepSec == 0 {
		c.Gateway.SessionSweepSec = 30
	}
	if c.Gateway.SessionHookTimeoutSeconds == 0 {
		c.Gateway.SessionHookTimeoutSeconds = 3
	}
	if c.Gateway.AccountID == "" {
		c.Gateway.AccountID = "local_account_10001"
	}
	if c.Gateway.AccountNumID == 0 {
		c.Gateway.AccountNumID = 10001
	}
	if c.Gateway.AuthMode == "mock" && c.Gateway.AuthToken == "" {
		c.Gateway.AuthToken = "local-auth-token"
	}
	if c.Gateway.AuthMode == "mock" && c.Gateway.LoginToken == "" {
		c.Gateway.LoginToken = "local-login-token"
	}
	if c.Gateway.ServerVersion == "" {
		c.Gateway.ServerVersion = "local-gateway 1.0"
	}
	if c.Gateway.ShardID == 0 {
		c.Gateway.ShardID = 9999
	}
	if c.Gateway.ShardName == "" {
		c.Gateway.ShardName = "9999"
	}
	if c.Gateway.ShardDisplay == "" {
		c.Gateway.ShardDisplay = "9999.Local"
	}
	if c.Gateway.ShardState == 0 {
		c.Gateway.ShardState = 1
	}
	if c.Gateway.Recommend == 0 {
		c.Gateway.Recommend = 1
	}
	if c.Gateway.RecommendLang == "" {
		c.Gateway.RecommendLang = "ZH"
	}
	if c.Gateway.VirtualGID == 0 {
		c.Gateway.VirtualGID = 2
	}
	if len(c.Gateway.ProductIDs) == 0 {
		c.Gateway.ProductIDs = []string{"1", "2", "3", "4", "25", "1001"}
	}
	if !c.Gateway.HasRole {
		c.Gateway.SendCreatePush = true
	}
}

func (c *ServiceConfig) trim() {
	c.Service.Name = strings.TrimSpace(c.Service.Name)
	c.Service.NodeID = strings.TrimSpace(c.Service.NodeID)
	c.Service.Environment = strings.TrimSpace(c.Service.Environment)
	c.Service.PublicAddr = strings.TrimSpace(c.Service.PublicAddr)
	c.Service.PrivateAddr = strings.TrimSpace(c.Service.PrivateAddr)
	c.Cluster.ID = strings.TrimSpace(c.Cluster.ID)
	c.Cluster.Zone = strings.TrimSpace(c.Cluster.Zone)
	c.Cluster.Version = strings.TrimSpace(c.Cluster.Version)
	c.Cluster.DataVersion = strings.TrimSpace(c.Cluster.DataVersion)
	c.Admin.Listen = strings.TrimSpace(c.Admin.Listen)
	c.Admin.Token = strings.TrimSpace(c.Admin.Token)
	for idx := range c.Admin.Tokens {
		c.Admin.Tokens[idx].Name = strings.TrimSpace(c.Admin.Tokens[idx].Name)
		c.Admin.Tokens[idx].Token = strings.TrimSpace(c.Admin.Tokens[idx].Token)
		c.Admin.Tokens[idx].Role = strings.ToLower(strings.TrimSpace(c.Admin.Tokens[idx].Role))
		c.Admin.Tokens[idx].Scopes = trimLowerStringSlice(c.Admin.Tokens[idx].Scopes)
	}
	c.OTel.Endpoint = strings.TrimSpace(c.OTel.Endpoint)
	c.OTel.ServiceName = strings.TrimSpace(c.OTel.ServiceName)
	c.DataTables.Directory = strings.TrimSpace(c.DataTables.Directory)
	c.DataTables.Version = strings.TrimSpace(c.DataTables.Version)
	c.I18N.Directory = strings.TrimSpace(c.I18N.Directory)
	c.I18N.DefaultLanguage = strings.TrimSpace(c.I18N.DefaultLanguage)
	c.I18N.Version = strings.TrimSpace(c.I18N.Version)
	c.trimPVF()
	c.Audit.Kind = strings.ToLower(strings.TrimSpace(c.Audit.Kind))
	c.Audit.FilePath = strings.TrimSpace(c.Audit.FilePath)
	c.LogicLog.Kind = strings.ToLower(strings.TrimSpace(c.LogicLog.Kind))
	c.LogicLog.FilePath = strings.TrimSpace(c.LogicLog.FilePath)
	c.LogicLog.ReasonsPath = strings.TrimSpace(c.LogicLog.ReasonsPath)
	c.BILog.Kind = strings.ToLower(strings.TrimSpace(c.BILog.Kind))
	c.BILog.FilePath = strings.TrimSpace(c.BILog.FilePath)
	c.BILog.SchemaPath = strings.TrimSpace(c.BILog.SchemaPath)
	c.EventLog.StoreKind = strings.ToLower(strings.TrimSpace(c.EventLog.StoreKind))
	c.EventLog.MySQLDSN = strings.TrimSpace(c.EventLog.MySQLDSN)
	c.EventLog.MySQLTable = strings.TrimSpace(c.EventLog.MySQLTable)
	c.EventLog.AdminPrefix = strings.TrimSpace(c.EventLog.AdminPrefix)
	c.EventLog.PublishKind = strings.ToLower(strings.TrimSpace(c.EventLog.PublishKind))
	c.EventLog.PublishHTTPURL = strings.TrimSpace(c.EventLog.PublishHTTPURL)
	c.EventLog.PublishHTTPMethod = strings.ToUpper(strings.TrimSpace(c.EventLog.PublishHTTPMethod))
	c.EventLog.PublishHTTPPayload = strings.ToLower(strings.TrimSpace(c.EventLog.PublishHTTPPayload))
	c.EventLog.PublishHTTPHeaders = trimStringMap(c.EventLog.PublishHTTPHeaders)
	c.AccountCenter.StoreKind = strings.ToLower(strings.TrimSpace(c.AccountCenter.StoreKind))
	c.AccountCenter.MySQLDSN = strings.TrimSpace(c.AccountCenter.MySQLDSN)
	c.AccountCenter.MySQLTable = strings.TrimSpace(c.AccountCenter.MySQLTable)
	c.AccountCenter.AdminPrefix = strings.TrimSpace(c.AccountCenter.AdminPrefix)
	c.StorageObject.StoreKind = strings.ToLower(strings.TrimSpace(c.StorageObject.StoreKind))
	c.StorageObject.MySQLDSN = strings.TrimSpace(c.StorageObject.MySQLDSN)
	c.StorageObject.MySQLTable = strings.TrimSpace(c.StorageObject.MySQLTable)
	c.StorageObject.AdminPrefix = strings.TrimSpace(c.StorageObject.AdminPrefix)
	c.BattleAgent.SettlementOutboxKind = strings.ToLower(strings.TrimSpace(c.BattleAgent.SettlementOutboxKind))
	c.BattleAgent.SettlementOutboxStream = strings.TrimSpace(c.BattleAgent.SettlementOutboxStream)
	c.BattleAgent.SettlementOutboxType = strings.TrimSpace(c.BattleAgent.SettlementOutboxType)
	c.HotUpdate.Target = strings.TrimSpace(c.HotUpdate.Target)
	c.HotUpdate.ControlKind = strings.ToLower(strings.TrimSpace(c.HotUpdate.ControlKind))
	c.HotUpdate.SourceKind = strings.ToLower(strings.TrimSpace(c.HotUpdate.SourceKind))
	c.HotUpdate.ApplierKind = strings.ToLower(strings.TrimSpace(c.HotUpdate.ApplierKind))
	c.HotUpdate.Workspace = strings.TrimSpace(c.HotUpdate.Workspace)
	c.HotUpdate.SigningKeyEnv = strings.TrimSpace(c.HotUpdate.SigningKeyEnv)
	c.HotUpdate.NativePatchAllowedTargets = trimStringSlice(c.HotUpdate.NativePatchAllowedTargets)
	c.HotUpdate.PatchOldSymbols = trimStringSlice(c.HotUpdate.PatchOldSymbols)
	c.StateSync.Kind = strings.ToLower(strings.TrimSpace(c.StateSync.Kind))
	c.StateSync.Namespace = strings.TrimSpace(c.StateSync.Namespace)
	c.ServerGroup.StoreKind = strings.ToLower(strings.TrimSpace(c.ServerGroup.StoreKind))
	c.ServerGroup.PlanFile = strings.TrimSpace(c.ServerGroup.PlanFile)
	c.ServerGroup.MergeArchiveDir = strings.TrimSpace(c.ServerGroup.MergeArchiveDir)
	c.ServerGroup.AdminPrefix = strings.TrimSpace(c.ServerGroup.AdminPrefix)
	c.Topology.Services = trimStringSlice(c.Topology.Services)
	c.Topology.GatewayService = strings.TrimSpace(c.Topology.GatewayService)
	c.Topology.GatewayBackendService = strings.TrimSpace(c.Topology.GatewayBackendService)
	c.Discovery.Strategy = strings.ToLower(strings.TrimSpace(c.Discovery.Strategy))
	c.RateLimit.Algorithm = strings.ToLower(strings.TrimSpace(c.RateLimit.Algorithm))
	if c.RateLimit.Algorithm == "tokenbucket" || c.RateLimit.Algorithm == "token-bucket" {
		c.RateLimit.Algorithm = "token_bucket"
	}
	c.RateLimit.Rules = strings.TrimSpace(c.RateLimit.Rules)
	c.RateLimit.TrustedHeader = strings.TrimSpace(c.RateLimit.TrustedHeader)
	c.Idempotency.Kind = strings.ToLower(strings.TrimSpace(c.Idempotency.Kind))
	c.Idempotency.KeyPrefix = strings.TrimSpace(c.Idempotency.KeyPrefix)
	c.Idempotency.RedisAddress = strings.TrimSpace(c.Idempotency.RedisAddress)
	c.Idempotency.RedisPassword = strings.TrimSpace(c.Idempotency.RedisPassword)
	c.Idempotency.MySQLDSN = strings.TrimSpace(c.Idempotency.MySQLDSN)
	c.Registry.Kind = strings.ToLower(strings.TrimSpace(c.Registry.Kind))
	c.Registry.Namespace = strings.TrimSpace(c.Registry.Namespace)
	c.Registry.Endpoints = trimStringSlice(c.Registry.Endpoints)
	c.Registry.RedisPassword = strings.TrimSpace(c.Registry.RedisPassword)
	c.Bus.Kind = strings.ToLower(strings.TrimSpace(c.Bus.Kind))
	c.Bus.Namespace = strings.TrimSpace(c.Bus.Namespace)
	c.Bus.Endpoints = trimStringSlice(c.Bus.Endpoints)
	c.Bus.RedisUsername = strings.TrimSpace(c.Bus.RedisUsername)
	c.Bus.RedisPassword = strings.TrimSpace(c.Bus.RedisPassword)
	c.Bus.RedisTLSServerName = strings.TrimSpace(c.Bus.RedisTLSServerName)
	c.Bus.NATSToken = strings.TrimSpace(c.Bus.NATSToken)
	c.Bus.NATSUsername = strings.TrimSpace(c.Bus.NATSUsername)
	c.Bus.NATSPassword = strings.TrimSpace(c.Bus.NATSPassword)
	c.Bus.NATSCredentials = strings.TrimSpace(c.Bus.NATSCredentials)
	c.Bus.NATSTLSServerName = strings.TrimSpace(c.Bus.NATSTLSServerName)
	c.Bus.NATSCAFile = strings.TrimSpace(c.Bus.NATSCAFile)
	c.Bus.NATSCertFile = strings.TrimSpace(c.Bus.NATSCertFile)
	c.Bus.NATSKeyFile = strings.TrimSpace(c.Bus.NATSKeyFile)
	c.Cache.Kind = strings.ToLower(strings.TrimSpace(c.Cache.Kind))
	c.Cache.KeyPrefix = strings.TrimSpace(c.Cache.KeyPrefix)
	c.Cache.RedisAddress = strings.TrimSpace(c.Cache.RedisAddress)
	c.Cache.RedisPassword = strings.TrimSpace(c.Cache.RedisPassword)
	c.Lock.Kind = strings.ToLower(strings.TrimSpace(c.Lock.Kind))
	c.Lock.KeyPrefix = strings.TrimSpace(c.Lock.KeyPrefix)
	c.Lock.RedisAddress = strings.TrimSpace(c.Lock.RedisAddress)
	c.Lock.RedisPassword = strings.TrimSpace(c.Lock.RedisPassword)
	c.Presence.Kind = strings.ToLower(strings.TrimSpace(c.Presence.Kind))
	c.Presence.KeyPrefix = strings.TrimSpace(c.Presence.KeyPrefix)
	c.Presence.RedisAddress = strings.TrimSpace(c.Presence.RedisAddress)
	c.Presence.RedisPassword = strings.TrimSpace(c.Presence.RedisPassword)
	c.ProfileStore.StoreKind = strings.ToLower(strings.TrimSpace(c.ProfileStore.StoreKind))
	c.ProfileStore.StoreDirectory = strings.TrimSpace(c.ProfileStore.StoreDirectory)
	c.ProfileStore.StoreAddress = strings.TrimSpace(c.ProfileStore.StoreAddress)
	c.ProfileStore.StorePassword = strings.TrimSpace(c.ProfileStore.StorePassword)
	c.ProfileStore.StoreKeyPrefix = strings.TrimSpace(c.ProfileStore.StoreKeyPrefix)
	c.ProfileStore.MySQLDSN = strings.TrimSpace(c.ProfileStore.MySQLDSN)
	for idx := range c.ProfileStore.MySQLShards {
		c.ProfileStore.MySQLShards[idx].ID = strings.TrimSpace(c.ProfileStore.MySQLShards[idx].ID)
		c.ProfileStore.MySQLShards[idx].DSN = strings.TrimSpace(c.ProfileStore.MySQLShards[idx].DSN)
		c.ProfileStore.MySQLShards[idx].TableName = strings.TrimSpace(c.ProfileStore.MySQLShards[idx].TableName)
		c.ProfileStore.MySQLShards[idx].TablePrefix = strings.TrimSpace(c.ProfileStore.MySQLShards[idx].TablePrefix)
		c.ProfileStore.MySQLShards[idx].HashSlots = strings.TrimSpace(c.ProfileStore.MySQLShards[idx].HashSlots)
	}
	c.ProfileStore.SummaryStoreKind = strings.ToLower(strings.TrimSpace(c.ProfileStore.SummaryStoreKind))
	c.ProfileStore.SummaryStoreAddress = strings.TrimSpace(c.ProfileStore.SummaryStoreAddress)
	c.ProfileStore.SummaryStorePassword = strings.TrimSpace(c.ProfileStore.SummaryStorePassword)
	c.ProfileStore.SummaryStoreKeyPrefix = strings.TrimSpace(c.ProfileStore.SummaryStoreKeyPrefix)
	c.ProfileStore.SaveMode = strings.ToLower(strings.TrimSpace(c.ProfileStore.SaveMode))
	c.ProfileStore.AsyncDeadLetterDirectory = strings.TrimSpace(c.ProfileStore.AsyncDeadLetterDirectory)
	c.Gateway.AuthListen = strings.TrimSpace(c.Gateway.AuthListen)
	c.Gateway.AuthMode = strings.ToLower(strings.TrimSpace(c.Gateway.AuthMode))
	c.Gateway.GateRouteFeature = strings.TrimSpace(c.Gateway.GateRouteFeature)
	c.Gateway.GateService = strings.TrimSpace(c.Gateway.GateService)
	c.Gateway.TCPListen = strings.TrimSpace(c.Gateway.TCPListen)
	c.Gateway.TCPProxyTrustedCIDRs = trimStringSlice(c.Gateway.TCPProxyTrustedCIDRs)
	c.Gateway.KCPListen = strings.TrimSpace(c.Gateway.KCPListen)
	c.Gateway.QUICListen = strings.TrimSpace(c.Gateway.QUICListen)
	c.Gateway.QUICCertFile = strings.TrimSpace(c.Gateway.QUICCertFile)
	c.Gateway.QUICKeyFile = strings.TrimSpace(c.Gateway.QUICKeyFile)
	c.Gateway.WebSocketListen = strings.TrimSpace(c.Gateway.WebSocketListen)
	c.Gateway.WebSocketPath = strings.TrimSpace(c.Gateway.WebSocketPath)
	c.Gateway.ChatListen = strings.TrimSpace(c.Gateway.ChatListen)
	c.Gateway.BackendService = strings.TrimSpace(c.Gateway.BackendService)
	c.Gateway.AccountID = strings.TrimSpace(c.Gateway.AccountID)
	c.Gateway.SessionKeyMasterSecret = strings.TrimSpace(c.Gateway.SessionKeyMasterSecret)
	c.Gateway.AuthToken = strings.TrimSpace(c.Gateway.AuthToken)
	c.Gateway.LoginToken = strings.TrimSpace(c.Gateway.LoginToken)
	c.Gateway.ServerVersion = strings.TrimSpace(c.Gateway.ServerVersion)
	c.Gateway.ShardName = strings.TrimSpace(c.Gateway.ShardName)
	c.Gateway.ShardDisplay = strings.TrimSpace(c.Gateway.ShardDisplay)
	c.Gateway.RecommendLang = strings.TrimSpace(c.Gateway.RecommendLang)
	c.Gateway.ProductIDs = trimStringSlice(c.Gateway.ProductIDs)
}

func trimStringSlice(values []string) []string {
	for idx := range values {
		values[idx] = strings.TrimSpace(values[idx])
	}
	return values
}

func trimLowerStringSlice(values []string) []string {
	for idx := range values {
		values[idx] = strings.ToLower(strings.TrimSpace(values[idx]))
	}
	return values
}

func trimStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = strings.TrimSpace(value)
	}
	return out
}
