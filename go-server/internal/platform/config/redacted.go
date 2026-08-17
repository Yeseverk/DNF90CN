package config

func (c ServiceConfig) Redacted() ServiceConfig {
	redacted := c
	if redacted.Admin.Token != "" {
		redacted.Admin.Token = "[redacted]"
	}
	if len(redacted.Admin.Tokens) > 0 {
		redacted.Admin.Tokens = append([]AdminTokenSection(nil), redacted.Admin.Tokens...)
		for idx := range redacted.Admin.Tokens {
			if len(redacted.Admin.Tokens[idx].Scopes) > 0 {
				redacted.Admin.Tokens[idx].Scopes = append([]string(nil), redacted.Admin.Tokens[idx].Scopes...)
			}
			if redacted.Admin.Tokens[idx].Token != "" {
				redacted.Admin.Tokens[idx].Token = "[redacted]"
			}
		}
	}
	if redacted.Gateway.AuthToken != "" {
		redacted.Gateway.AuthToken = "[redacted]"
	}
	if redacted.Gateway.LoginToken != "" {
		redacted.Gateway.LoginToken = "[redacted]"
	}
	if redacted.Gateway.SessionKeyMasterSecret != "" {
		redacted.Gateway.SessionKeyMasterSecret = "[redacted]"
	}
	if redacted.ProfileStore.StorePassword != "" {
		redacted.ProfileStore.StorePassword = "[redacted]"
	}
	if redacted.ProfileStore.SummaryStorePassword != "" {
		redacted.ProfileStore.SummaryStorePassword = "[redacted]"
	}
	if redacted.ProfileStore.MySQLDSN != "" {
		redacted.ProfileStore.MySQLDSN = "[redacted]"
	}
	if len(redacted.ProfileStore.MySQLShards) > 0 {
		redacted.ProfileStore.MySQLShards = append([]ProfileMySQLShard(nil), redacted.ProfileStore.MySQLShards...)
		for idx := range redacted.ProfileStore.MySQLShards {
			if redacted.ProfileStore.MySQLShards[idx].DSN != "" {
				redacted.ProfileStore.MySQLShards[idx].DSN = "[redacted]"
			}
		}
	}
	if redacted.Idempotency.RedisPassword != "" {
		redacted.Idempotency.RedisPassword = "[redacted]"
	}
	if redacted.Idempotency.MySQLDSN != "" {
		redacted.Idempotency.MySQLDSN = "[redacted]"
	}
	if redacted.Registry.RedisPassword != "" {
		redacted.Registry.RedisPassword = "[redacted]"
	}
	if redacted.Bus.RedisPassword != "" {
		redacted.Bus.RedisPassword = "[redacted]"
	}
	if redacted.Bus.NATSToken != "" {
		redacted.Bus.NATSToken = "[redacted]"
	}
	if redacted.Bus.NATSPassword != "" {
		redacted.Bus.NATSPassword = "[redacted]"
	}
	if redacted.Bus.NATSCredentials != "" {
		redacted.Bus.NATSCredentials = "[redacted]"
	}
	if redacted.EventLog.MySQLDSN != "" {
		redacted.EventLog.MySQLDSN = "[redacted]"
	}
	if redacted.AccountCenter.MySQLDSN != "" {
		redacted.AccountCenter.MySQLDSN = "[redacted]"
	}
	if redacted.EventLog.PublishHTTPURL != "" {
		redacted.EventLog.PublishHTTPURL = "[redacted]"
	}
	if len(redacted.EventLog.PublishHTTPHeaders) > 0 {
		headers := make(map[string]string, len(redacted.EventLog.PublishHTTPHeaders))
		for key := range redacted.EventLog.PublishHTTPHeaders {
			headers[key] = "[redacted]"
		}
		redacted.EventLog.PublishHTTPHeaders = headers
	}
	if redacted.Cache.RedisPassword != "" {
		redacted.Cache.RedisPassword = "[redacted]"
	}
	if redacted.Lock.RedisPassword != "" {
		redacted.Lock.RedisPassword = "[redacted]"
	}
	if redacted.Presence.RedisPassword != "" {
		redacted.Presence.RedisPassword = "[redacted]"
	}
	if redacted.StorageObject.MySQLDSN != "" {
		redacted.StorageObject.MySQLDSN = "[redacted]"
	}
	return redacted
}
