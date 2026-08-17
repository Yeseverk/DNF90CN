// 本文件把 DNF 玩家仓储装配接入框架组件生命周期。
package mysql

import (
	"context"
	"fmt"
	"longheng.io/server/internal/modules/dnf/repository"
	"sync"
	"time"

	"longheng.io/server/internal/platform/servergroup"
)

type Component struct {
	db      SQLDB
	manager *servergroup.Manager
	options ComponentOptions
	now     func() time.Time

	mu        sync.RWMutex
	group     repository.Group
	plan      repository.DatabasePlan
	started   bool
	startedAt time.Time
	lastError string
}

type ComponentOptions struct {
	ShardID                   string
	TablePrefix               string
	AutoCreateSchema          bool
	IncludeCSharpLegacySchema bool
	CreateDatabases           bool
	DatabasePlan              repository.DatabasePlan
	Now                       func() time.Time
}

type ComponentSnapshot struct {
	Started                   bool      `json:"started"`
	StartedAt                 time.Time `json:"started_at,omitempty"`
	ShardID                   string    `json:"shard_id,omitempty"`
	GroupID                   string    `json:"group_id,omitempty"`
	TablePrefix               string    `json:"table_prefix"`
	AutoCreateSchema          bool      `json:"auto_create_schema"`
	IncludeCSharpLegacySchema bool      `json:"include_csharp_legacy_schema"`
	CreateDatabases           bool      `json:"create_databases"`
	WriteDatabases            []string  `json:"write_databases,omitempty"`
	ReadDatabases             []string  `json:"read_databases,omitempty"`
	LastError                 string    `json:"last_error,omitempty"`
}

// NewComponent 创建 DNF 玩家仓储生命周期组件。
// 调用方可直接传入 DatabasePlan；组件只装配仓储，不接触 PVF 或频道目录。
func NewComponent(db SQLDB, options ComponentOptions) *Component {
	return &Component{
		db:      db,
		options: cloneCompOptions(options),
		now:     firstClock(options.Now),
	}
}

// NewComponentFromServerGroup 创建基于 servergroup 派生库名的仓储组件。
// 启动时按 ShardID 解析 dnf_repository route meta，再装配 MySQL 仓储聚合。
func NewComponentFromServerGroup(db SQLDB, manager *servergroup.Manager, options ComponentOptions) *Component {
	component := NewComponent(db, options)
	component.manager = manager
	return component
}

// Name 返回框架生命周期中展示的组件名。
func (c *Component) Name() string {
	return "dnf-repository"
}

// Preflight 校验 DNF 仓储装配依赖和库表配置。
// 这里只解析 servergroup 和本地 schema，不连接 MySQL，也不创建数据库或表。
func (c *Component) Preflight(ctx context.Context) error {
	if err := repoCtxErr(ctx); err != nil {
		return err
	}
	if c == nil || c.db == nil {
		return ErrMySQLDBRequired
	}
	plan, err := c.resolvePlan(ctx)
	if err != nil {
		return err
	}
	if _, err := NewMySQLGroup(c.db, c.groupOptions(plan)); err != nil {
		return err
	}
	if c.options.AutoCreateSchema {
		_, err = MySQLSchema(c.schemaOptions(plan))
		return err
	}
	return nil
}

// Start 解析区服库计划、按需初始化 schema，并发布 DNF 仓储聚合。
// 只有 AutoCreateSchema=true 时才会写 MySQL schema；玩家业务状态仍由 repository 接口读写。
func (c *Component) Start(ctx context.Context) error {
	if err := c.Preflight(ctx); err != nil {
		c.recordError(err)
		return err
	}
	plan, err := c.resolvePlan(ctx)
	if err != nil {
		c.recordError(err)
		return err
	}
	if err := EnsureMySQLSchema(ctx, c.db, c.schemaOptions(plan)); err != nil {
		c.recordError(err)
		return err
	}
	if err := MigrateLegacyJSONStorage(ctx, c.db, c.schemaOptions(plan)); err != nil {
		c.recordError(err)
		return err
	}
	group, err := NewMySQLGroup(c.db, c.groupOptions(plan))
	if err != nil {
		c.recordError(err)
		return err
	}
	if err := c.db.PingContext(ctxOrBackground(ctx)); err != nil {
		c.recordError(err)
		return err
	}

	c.mu.Lock()
	c.group = group
	c.plan = clonePlan(plan)
	c.started = true
	c.startedAt = c.clock().UTC()
	c.lastError = ""
	c.mu.Unlock()
	return nil
}

// Stop 清理 DNF 仓储组件的可见状态。
// 组件不关闭 SQL 连接池，连接池生命周期由项目启动装配层统一管理。
func (c *Component) Stop(context.Context) error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	c.group = repository.Group{}
	c.plan = repository.DatabasePlan{}
	c.started = false
	c.startedAt = time.Time{}
	c.mu.Unlock()
	return nil
}

// Group 返回启动完成后的 DNF 玩家仓储聚合。
// 返回值只用于账号、角色、背包、技能、设置和 packet 模板等可变数据。
func (c *Component) Group() (repository.Group, bool) {
	if c == nil {
		return repository.Group{}, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.started {
		return repository.Group{}, false
	}
	return c.group, true
}

// Snapshot 返回 DNF 仓储装配状态和库计划。
// 快照不包含 DSN、账号、密码或任何玩家数据。
func (c *Component) Snapshot() ComponentSnapshot {
	if c == nil {
		return ComponentSnapshot{}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return ComponentSnapshot{
		Started:                   c.started,
		StartedAt:                 c.startedAt,
		ShardID:                   c.plan.ShardID,
		GroupID:                   c.plan.GroupID,
		TablePrefix:               firstValue(c.options.TablePrefix, defaultTablePrefix),
		AutoCreateSchema:          c.options.AutoCreateSchema,
		IncludeCSharpLegacySchema: c.options.IncludeCSharpLegacySchema,
		CreateDatabases:           c.options.CreateDatabases,
		WriteDatabases:            append([]string(nil), c.plan.WriteDatabases...),
		ReadDatabases:             append([]string(nil), c.plan.ReadDatabases...),
		LastError:                 c.lastError,
	}
}

func (c *Component) resolvePlan(ctx context.Context) (repository.DatabasePlan, error) {
	if c == nil {
		return repository.DatabasePlan{}, fmt.Errorf("%w: component is required", repository.ErrDatabasePlanInvalid)
	}
	if len(c.options.DatabasePlan.WriteDatabases) > 0 {
		plan := clonePlan(c.options.DatabasePlan)
		if plan.ShardID == "" {
			plan.ShardID = c.options.ShardID
		}
		return plan, nil
	}
	if c.manager == nil {
		return repository.DatabasePlan{}, fmt.Errorf("%w: database plan or servergroup is required", repository.ErrDatabasePlanInvalid)
	}
	shardID := firstValue(c.options.ShardID, c.options.DatabasePlan.ShardID)
	return repository.ResolveDatabasePlan(ctx, c.manager, shardID)
}

func (c *Component) groupOptions(plan repository.DatabasePlan) MySQLGroupOptions {
	return MySQLGroupOptions{
		DatabasePlan: clonePlan(plan),
		TablePrefix:  c.options.TablePrefix,
		Now:          c.options.Now,
	}
}

func (c *Component) schemaOptions(plan repository.DatabasePlan) SchemaOptions {
	return SchemaOptions{
		AutoCreate:                c.options.AutoCreateSchema,
		CreateDatabases:           c.options.CreateDatabases,
		IncludeCSharpLegacySchema: c.options.IncludeCSharpLegacySchema,
		TablePrefix:               c.options.TablePrefix,
		DatabasePlan:              clonePlan(plan),
	}
}

func (c *Component) recordError(err error) {
	if c == nil || err == nil {
		return
	}
	c.mu.Lock()
	c.group = repository.Group{}
	c.plan = repository.DatabasePlan{}
	c.started = false
	c.startedAt = time.Time{}
	c.lastError = err.Error()
	c.mu.Unlock()
}

func (c *Component) clock() time.Time {
	if c == nil || c.now == nil {
		return time.Now()
	}
	return c.now()
}

func cloneCompOptions(options ComponentOptions) ComponentOptions {
	options.DatabasePlan = clonePlan(options.DatabasePlan)
	return options
}

func clonePlan(plan repository.DatabasePlan) repository.DatabasePlan {
	plan.WriteDatabases = append([]string(nil), plan.WriteDatabases...)
	plan.ReadDatabases = append([]string(nil), plan.ReadDatabases...)
	return plan
}

func firstClock(now func() time.Time) func() time.Time {
	if now != nil {
		return now
	}
	return time.Now
}

func ctxOrBackground(ctx context.Context) context.Context {
	if ctx != nil {
		return ctx
	}
	return context.Background()
}
