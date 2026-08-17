// 本文件把 DNF 仓储 schema 初始化接入框架生命周期。
package mysql

import (
	"context"
	"sync"
	"time"
)

type SchemaComponent struct {
	exec    SQLExecutor
	options SchemaOptions
	now     func() time.Time

	mu        sync.RWMutex
	started   bool
	startedAt time.Time
	lastError string
}

type SchemaSnapshot struct {
	Started                   bool      `json:"started"`
	StartedAt                 time.Time `json:"started_at,omitempty"`
	AutoCreate                bool      `json:"auto_create"`
	IncludeCSharpLegacySchema bool      `json:"include_csharp_legacy_schema"`
	CreateDatabases           bool      `json:"create_databases"`
	TablePrefix               string    `json:"table_prefix"`
	ShardID                   string    `json:"shard_id,omitempty"`
	WriteDatabases            []string  `json:"write_databases,omitempty"`
	ReadDatabases             []string  `json:"read_databases,omitempty"`
	LastError                 string    `json:"last_error,omitempty"`
}

// NewSchemaComponent 创建 DNF 仓储 schema 生命周期组件。
// 组件只在 AutoCreate=true 时写 MySQL schema；默认关闭时 Start 不产生持久化副作用。
func NewSchemaComponent(exec SQLExecutor, options SchemaOptions) *SchemaComponent {
	return &SchemaComponent{
		exec:    exec,
		options: cloneSchemaOptions(options),
		now:     time.Now,
	}
}

// Name 返回框架生命周期中展示的组件名。
func (c *SchemaComponent) Name() string {
	return "dnf-repository-schema"
}

// Preflight 校验 DNF 仓储 schema 配置。
// 它只生成 DDL 做本地校验，不连接 MySQL，也不创建数据库或表。
func (c *SchemaComponent) Preflight(ctx context.Context) error {
	if err := repoCtxErr(ctx); err != nil {
		return err
	}
	if c == nil {
		return ErrSchemaExecutorRequired
	}
	if !c.options.AutoCreate {
		return nil
	}
	if c.exec == nil {
		return ErrSchemaExecutorRequired
	}
	_, err := MySQLSchema(c.options)
	return err
}

// Start 按配置执行 DNF 仓储首次启动建库建表。
// 该函数可能写 MySQL schema；只有显式 AutoCreate=true 时才会执行 DDL。
func (c *SchemaComponent) Start(ctx context.Context) error {
	if err := c.Preflight(ctx); err != nil {
		c.recordSchemaError(err)
		return err
	}
	if err := EnsureMySQLSchema(ctx, c.exec, c.options); err != nil {
		c.recordSchemaError(err)
		return err
	}
	c.mu.Lock()
	c.started = true
	c.startedAt = c.clock().UTC()
	c.lastError = ""
	c.mu.Unlock()
	return nil
}

// Stop 清理 DNF 仓储 schema 组件的启动状态。
// 它不删除数据库或表，只收敛生命周期可见状态。
func (c *SchemaComponent) Stop(context.Context) error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	c.started = false
	c.startedAt = time.Time{}
	c.mu.Unlock()
	return nil
}

// Snapshot 返回 DNF 仓储 schema 组件的配置和启动状态。
// 快照只暴露库名和开关，不包含 DSN、账号或密码。
func (c *SchemaComponent) Snapshot() SchemaSnapshot {
	if c == nil {
		return SchemaSnapshot{}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return SchemaSnapshot{
		Started:                   c.started,
		StartedAt:                 c.startedAt,
		AutoCreate:                c.options.AutoCreate,
		IncludeCSharpLegacySchema: c.options.IncludeCSharpLegacySchema,
		CreateDatabases:           c.options.CreateDatabases,
		TablePrefix:               firstValue(c.options.TablePrefix, defaultTablePrefix),
		ShardID:                   c.options.DatabasePlan.ShardID,
		WriteDatabases:            append([]string(nil), c.options.DatabasePlan.WriteDatabases...),
		ReadDatabases:             append([]string(nil), c.options.DatabasePlan.ReadDatabases...),
		LastError:                 c.lastError,
	}
}

func (c *SchemaComponent) recordSchemaError(err error) {
	if c == nil || err == nil {
		return
	}
	c.mu.Lock()
	c.started = false
	c.startedAt = time.Time{}
	c.lastError = err.Error()
	c.mu.Unlock()
}

func (c *SchemaComponent) clock() time.Time {
	if c == nil || c.now == nil {
		return time.Now()
	}
	return c.now()
}

func cloneSchemaOptions(options SchemaOptions) SchemaOptions {
	options.DatabasePlan.WriteDatabases = append([]string(nil), options.DatabasePlan.WriteDatabases...)
	options.DatabasePlan.ReadDatabases = append([]string(nil), options.DatabasePlan.ReadDatabases...)
	return options
}

func repoCtxErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
