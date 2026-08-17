// Package dnf 提供 DNF 项目接入 logic 服务的装配入口。
package dnf

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	appkit "longheng.io/server/internal/platform/app"
)

const (
	// EnvConfigPath 是 DNF logic manifest 使用的外部配置路径环境变量。
	EnvConfigPath = "LONGHENG_DNF_CONFIG"
	// DefaultConfigPath 是本地 DNF logic manifest 配置路径。
	DefaultConfigPath = "configs/dnf/logic.toml"
)

var ErrConfigInvalid = errors.New("dnf logic config is invalid")

type Config struct {
	Repository RepositoryConfig `toml:"repository"`
}

type RepositoryConfig struct {
	Enabled              bool   `toml:"enabled"`
	MySQLDSN             string `toml:"mysql_dsn"`
	MySQLMaxOpenConns    int    `toml:"mysql_max_open_conns"`
	MySQLMaxIdleConns    int    `toml:"mysql_max_idle_conns"`
	MySQLConnMaxLifetime int    `toml:"mysql_conn_max_lifetime_seconds"`
	RedisEnabled         bool   `toml:"redis_enabled"`
	RedisAddress         string `toml:"redis_address"`
	RedisPassword        string `toml:"redis_password"`
	RedisDB              int    `toml:"redis_db"`
	RedisPoolSize        int    `toml:"redis_pool_size"`
	RedisTimeoutSeconds  int    `toml:"redis_timeout_seconds"`
	RedisKeyPrefix       string `toml:"redis_key_prefix"`
	RedisTTLSeconds      int    `toml:"redis_ttl_seconds"`
	ShardID              string `toml:"shard_id"`
	TablePrefix          string `toml:"table_prefix"`
	ServerGroupPlanFile  string `toml:"server_group_plan_file"`
	AutoCreateSchema     bool   `toml:"auto_create_schema"`
	CSharpLegacySchema   bool   `toml:"csharp_legacy_schema"`
	CreateDatabases      bool   `toml:"create_databases"`
}

// LoadConfig 读取 DNF logic manifest 外部配置。
// 该配置只属于 DNF 项目装配层，不进入平台通用 ServiceConfig；环境相关默认值由 LoadConfigForEnv 补齐。
func LoadConfig(path string) (Config, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = DefaultConfigPath
	}
	var cfg Config
	meta, err := toml.DecodeFile(path, &cfg)
	if err != nil {
		return Config{}, err
	}
	if keys := meta.Undecoded(); len(keys) > 0 {
		names := make([]string, 0, len(keys))
		for _, key := range keys {
			names = append(names, key.String())
		}
		return Config{}, fmt.Errorf("%w: unknown keys: %s", ErrConfigInvalid, strings.Join(names, ", "))
	}
	return cfg, nil
}

// LoadConfigForEnv 读取并校验 DNF logic manifest 配置。
// 它会从框架 Env 补齐当前区服 shard_id，再执行配置校验。
func LoadConfigForEnv(path string, env *appkit.Env) (Config, error) {
	cfg, err := LoadConfig(path)
	if err != nil {
		return Config{}, err
	}
	cfg.Normalize(env)
	return cfg, cfg.Validate()
}

// Normalize 补齐 DNF 仓储装配默认值。
// ShardID 默认读取框架区服 ID，读写库名仍由 servergroup route meta 派生。
func (c *Config) Normalize(env *appkit.Env) {
	if c == nil {
		return
	}
	repo := &c.Repository
	repo.MySQLDSN = strings.TrimSpace(repo.MySQLDSN)
	repo.RedisAddress = strings.TrimSpace(repo.RedisAddress)
	repo.RedisPassword = strings.TrimSpace(repo.RedisPassword)
	repo.RedisKeyPrefix = strings.TrimSpace(repo.RedisKeyPrefix)
	repo.ShardID = strings.TrimSpace(repo.ShardID)
	repo.TablePrefix = strings.TrimSpace(repo.TablePrefix)
	repo.ServerGroupPlanFile = strings.TrimSpace(repo.ServerGroupPlanFile)
	if repo.ShardID == "" && env != nil && env.Config.Cluster.ShardID != 0 {
		repo.ShardID = fmt.Sprintf("%d", env.Config.Cluster.ShardID)
	}
	if repo.ServerGroupPlanFile == "" && env != nil {
		repo.ServerGroupPlanFile = strings.TrimSpace(env.Config.ServerGroup.PlanFile)
	}
	if repo.TablePrefix == "" {
		repo.TablePrefix = "dnf"
	}
	if repo.MySQLMaxOpenConns <= 0 {
		repo.MySQLMaxOpenConns = 32
	}
	if repo.MySQLMaxIdleConns <= 0 {
		repo.MySQLMaxIdleConns = 8
	}
	if repo.MySQLConnMaxLifetime <= 0 {
		repo.MySQLConnMaxLifetime = 300
	}
	if repo.RedisEnabled {
		if repo.RedisAddress == "" {
			repo.RedisAddress = "127.0.0.1:6379"
		}
		if repo.RedisPoolSize <= 0 {
			repo.RedisPoolSize = 8
		}
		if repo.RedisTimeoutSeconds <= 0 {
			repo.RedisTimeoutSeconds = 2
		}
		if repo.RedisKeyPrefix == "" {
			repo.RedisKeyPrefix = "dnf:repository"
			if repo.ShardID != "" {
				repo.RedisKeyPrefix += ":" + repo.ShardID
			}
		}
		if repo.RedisTTLSeconds < 0 {
			repo.RedisTTLSeconds = 0
		}
	}
}

// Validate 校验 DNF 仓储装配配置。
// 开启仓储时必须显式配置 MySQL DSN，并通过 servergroup 提供 shard 路由。
func (c Config) Validate() error {
	repo := c.Repository
	if !repo.Enabled {
		return nil
	}
	if strings.TrimSpace(repo.MySQLDSN) == "" {
		return fmt.Errorf("%w: repository mysql_dsn is required", ErrConfigInvalid)
	}
	if strings.TrimSpace(repo.ShardID) == "" {
		return fmt.Errorf("%w: repository shard_id is required", ErrConfigInvalid)
	}
	if repo.MySQLMaxOpenConns <= 0 || repo.MySQLMaxIdleConns <= 0 || repo.MySQLConnMaxLifetime <= 0 {
		return fmt.Errorf("%w: repository mysql pool values must be positive", ErrConfigInvalid)
	}
	if repo.RedisEnabled {
		if strings.TrimSpace(repo.RedisAddress) == "" {
			return fmt.Errorf("%w: repository redis_address is required", ErrConfigInvalid)
		}
		if repo.RedisDB < 0 {
			return fmt.Errorf("%w: repository redis_db must be non-negative", ErrConfigInvalid)
		}
		if repo.RedisPoolSize <= 0 || repo.RedisTimeoutSeconds <= 0 {
			return fmt.Errorf("%w: repository redis pool values must be positive", ErrConfigInvalid)
		}
		if strings.TrimSpace(repo.RedisKeyPrefix) == "" {
			return fmt.Errorf("%w: repository redis_key_prefix is required", ErrConfigInvalid)
		}
	}
	return nil
}

func (c RepositoryConfig) connMaxLife() time.Duration {
	if c.MySQLConnMaxLifetime <= 0 {
		return 5 * time.Minute
	}
	return time.Duration(c.MySQLConnMaxLifetime) * time.Second
}
