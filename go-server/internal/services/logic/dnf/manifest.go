package dnf

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	"longheng.io/server/internal/modules/dnf/repository/mysql"
	appkit "longheng.io/server/internal/platform/app"
	platformdb "longheng.io/server/internal/platform/db"
	"longheng.io/server/internal/platform/servergroup"
)

var openSQL = sql.Open

type Runtime struct {
	Repository *RepoComponent
}

type RepoComponent struct {
	component *mysql.Component
	db        *sql.DB
	redis     *platformdb.RedigoExecutor
	cacheOpts dnfrepo.RedisCacheOptions
}

// NewRuntime 创建 DNF logic 运行时装配对象。
// 它只负责项目依赖装配，不注册协议 handler，也不直接修改玩家状态。
func NewRuntime(env *appkit.Env, cfg Config) (*Runtime, error) {
	cfg.Normalize(env)
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	runtime := &Runtime{}
	if cfg.Repository.Enabled {
		component, err := newRepoComponent(env, cfg.Repository)
		if err != nil {
			return nil, err
		}
		runtime.Repository = component
	}
	return runtime, nil
}

// Group 返回启动后的 DNF 玩家仓储聚合。
// owner/service 只能通过该聚合访问账号、角色、背包、技能、设置和 packet 模板。
func (r *Runtime) Group() (dnfrepo.Group, bool) {
	if r == nil || r.Repository == nil {
		return dnfrepo.Group{}, false
	}
	return r.Repository.Group()
}

// NewRepoComponent 按 DNF 配置打开 MySQL 连接池并创建仓储组件。
// 连接池由返回的组件在 Stop 阶段关闭。
func NewRepoComponent(env *appkit.Env, cfg RepositoryConfig) (*RepoComponent, error) {
	envCfg := Config{Repository: cfg}
	envCfg.Normalize(env)
	if err := envCfg.Validate(); err != nil {
		return nil, err
	}
	cfg = envCfg.Repository
	return newRepoComponent(env, cfg)
}

func newRepoComponent(env *appkit.Env, cfg RepositoryConfig) (*RepoComponent, error) {
	if openSQL == nil {
		return nil, fmt.Errorf("%w: sql opener is required", ErrConfigInvalid)
	}
	db, err := openSQL("mysql", cfg.MySQLDSN)
	if err != nil {
		return nil, err
	}
	if db == nil {
		return nil, fmt.Errorf("%w: sql db is nil", ErrConfigInvalid)
	}
	db.SetMaxOpenConns(cfg.MySQLMaxOpenConns)
	db.SetMaxIdleConns(cfg.MySQLMaxIdleConns)
	db.SetConnMaxLifetime(cfg.connMaxLife())

	var manager *servergroup.Manager
	if env == nil {
		manager = nil
	} else {
		manager = env.ServerGroup
	}
	if manager == nil && cfg.ServerGroupPlanFile != "" {
		var err error
		manager, err = loadServerGroupPlan(cfg.ServerGroupPlanFile)
		if err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	var redisExec *platformdb.RedigoExecutor
	if cfg.RedisEnabled {
		redisExec = platformdb.NewRedigoExecutor(platformdb.RedigoOptions{
			Address:  cfg.RedisAddress,
			Password: cfg.RedisPassword,
			DB:       cfg.RedisDB,
			PoolSize: cfg.RedisPoolSize,
			Timeout:  cfg.redisTimeout(),
		})
	}
	component := mysql.NewComponentFromServerGroup(mysql.SQLDBAdapter{DB: db}, manager, mysql.ComponentOptions{
		ShardID:                   cfg.ShardID,
		TablePrefix:               cfg.TablePrefix,
		AutoCreateSchema:          cfg.AutoCreateSchema,
		IncludeCSharpLegacySchema: cfg.CSharpLegacySchema,
		CreateDatabases:           cfg.CreateDatabases,
		Now:                       time.Now,
	})
	return &RepoComponent{
		component: component,
		db:        db,
		redis:     redisExec,
		cacheOpts: dnfrepo.RedisCacheOptions{
			KeyPrefix: cfg.RedisKeyPrefix,
			TTL:       cfg.redisTTL(),
		},
	}, nil
}

// loadServerGroupPlan 从外部区服计划文件构造只读 manager。
// DNF manifest 可能早于框架 servergroup 注入运行，仓储仍必须按同一份 route meta 派生库名。
func loadServerGroupPlan(path string) (*servergroup.Manager, error) {
	store, err := servergroup.NewFileStore(path)
	if err != nil {
		return nil, err
	}
	plan, err := store.Load(context.Background())
	if err != nil {
		return nil, err
	}
	return servergroup.New(plan)
}

// Name 返回框架生命周期组件名。
func (c *RepoComponent) Name() string {
	if c == nil || c.component == nil {
		return "dnf-repository"
	}
	return c.component.Name()
}

// Preflight 检查 DNF 仓储启动依赖，不创建 schema。
func (c *RepoComponent) Preflight(ctx context.Context) error {
	if c == nil || c.component == nil {
		return mysql.ErrMySQLDBRequired
	}
	if err := c.component.Preflight(ctx); err != nil {
		return err
	}
	return c.pingRedis(ctx)
}

// Start 启动 DNF 仓储组件。
func (c *RepoComponent) Start(ctx context.Context) error {
	if c == nil || c.component == nil {
		return mysql.ErrMySQLDBRequired
	}
	if err := c.component.Start(ctx); err != nil {
		return err
	}
	return c.pingRedis(ctx)
}

// Stop 停止 DNF 仓储组件并关闭项目侧 SQL 连接池。
func (c *RepoComponent) Stop(ctx context.Context) error {
	if c == nil {
		return nil
	}
	var errs []error
	if c.component != nil {
		errs = append(errs, c.component.Stop(ctx))
	}
	if c.db != nil {
		errs = append(errs, c.db.Close())
	}
	if c.redis != nil {
		errs = append(errs, c.redis.Close())
	}
	return errors.Join(errs...)
}

// Group 返回启动后的 DNF 玩家仓储聚合。
func (c *RepoComponent) Group() (dnfrepo.Group, bool) {
	if c == nil || c.component == nil {
		return dnfrepo.Group{}, false
	}
	group, ok := c.component.Group()
	if !ok {
		return dnfrepo.Group{}, false
	}
	if c.redis != nil {
		group = dnfrepo.NewCachedGroup(group, c.redis, c.cacheOpts)
	}
	return group, true
}

// Snapshot 返回 DNF 仓储装配状态。
func (c *RepoComponent) Snapshot() mysql.ComponentSnapshot {
	if c == nil || c.component == nil {
		return mysql.ComponentSnapshot{}
	}
	return c.component.Snapshot()
}

func (c *RepoComponent) pingRedis(ctx context.Context) error {
	if c == nil || c.redis == nil {
		return nil
	}
	_, err := c.redis.Do(ctx, "PING")
	return err
}

func (c RepositoryConfig) redisTimeout() time.Duration {
	if c.RedisTimeoutSeconds <= 0 {
		return 2 * time.Second
	}
	return time.Duration(c.RedisTimeoutSeconds) * time.Second
}

func (c RepositoryConfig) redisTTL() time.Duration {
	if c.RedisTTLSeconds <= 0 {
		return 0
	}
	return time.Duration(c.RedisTTLSeconds) * time.Second
}
