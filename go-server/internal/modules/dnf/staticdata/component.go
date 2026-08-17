// 本文件把 DNF 静态数据装配接入框架组件生命周期。
package staticdata

import (
	"context"
	"sync"
	"time"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

type Component struct {
	source  dnfpvf.Source
	options Options
	now     func() time.Time

	mu        sync.RWMutex
	store     *Store
	started   bool
	loadedAt  time.Time
	lastError string
}

type ComponentSnapshot struct {
	Started   bool      `json:"started"`
	LoadedAt  time.Time `json:"loaded_at,omitempty"`
	Store     Snapshot  `json:"store"`
	LastError string    `json:"last_error,omitempty"`
}

// NewComponent 创建 DNF 静态数据生命周期组件。
// 组件只在 Start 阶段从已加载内存的 PVF source 构建只读表，不访问磁盘、不写玩家状态。
func NewComponent(source dnfpvf.Source, options Options) *Component {
	return &Component{
		source:  source,
		options: cloneOptions(options),
		now:     time.Now,
	}
}

// Name 返回框架生命周期里展示的组件名。
func (c *Component) Name() string {
	return "dnf-staticdata"
}

// Preflight 检查 DNF 静态数据启动依赖是否齐全。
// 这里只校验内存 PVF source 和 ctx 状态，不解析 PVF，也不产生经济或持久化副作用。
func (c *Component) Preflight(ctx context.Context) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if c == nil || c.source == nil {
		return ErrSourceRequired
	}
	return nil
}

// Start 构建 DNF 静态数据内存表并发布给后续业务 owner 查询。
// 这里复用 Load 的只读装配流程，不发奖、不扣资产、不写 Profile/MySQL/Redis/EventLog/Outbox。
func (c *Component) Start(ctx context.Context) error {
	if err := c.Preflight(ctx); err != nil {
		c.recordError(err)
		return err
	}
	store, err := Load(ctx, c.source, c.options)
	if err != nil {
		c.recordError(err)
		return err
	}

	c.mu.Lock()
	c.store = store
	c.started = true
	c.loadedAt = c.clock().UTC()
	c.lastError = ""
	c.mu.Unlock()
	return nil
}

// Stop 清理 DNF 静态数据组件持有的 Store 引用。
// Store 本身没有后台 goroutine 或外部连接，因此停止只收敛内存可见状态。
func (c *Component) Stop(context.Context) error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	c.store = nil
	c.started = false
	c.loadedAt = time.Time{}
	c.mu.Unlock()
	return nil
}

// Store 返回启动后构建完成的 DNF 静态数据只读表。
// 返回的 Store 只能用于查询，调用方不得把它当作玩家状态或结算 owner。
func (c *Component) Store() (*Store, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.started || c.store == nil {
		return nil, false
	}
	return c.store, true
}

// Snapshot 返回 DNF 静态数据组件的启动状态和表规模。
// 该快照用于 debug 面板、启动日志和接入验收，不暴露 PVF 原文内容。
func (c *Component) Snapshot() ComponentSnapshot {
	if c == nil {
		return ComponentSnapshot{}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()

	snapshot := ComponentSnapshot{
		Started:   c.started,
		LoadedAt:  c.loadedAt,
		LastError: c.lastError,
	}
	if c.store != nil {
		snapshot.Store = c.store.Snapshot()
	}
	return snapshot
}

func (c *Component) recordError(err error) {
	if c == nil || err == nil {
		return
	}
	c.mu.Lock()
	c.store = nil
	c.started = false
	c.loadedAt = time.Time{}
	c.lastError = err.Error()
	c.mu.Unlock()
}

func (c *Component) clock() time.Time {
	if c == nil || c.now == nil {
		return time.Now()
	}
	return c.now()
}

func cloneOptions(options Options) Options {
	options.Build.Paths = append([]string(nil), options.Build.Paths...)
	options.Build.Lists = append([]string(nil), options.Build.Lists...)
	options.Build.Prefixes = append([]string(nil), options.Build.Prefixes...)
	return options
}

func ctxErr(ctx context.Context) error {
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
