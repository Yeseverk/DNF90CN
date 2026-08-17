package servergroup

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

var (
	// ErrInvalidPlan 表示服务器分组计划缺少分片、分组或路由关系。
	ErrInvalidPlan = errors.New("server group plan is invalid")
	// ErrNotFound 表示未找到可用的服务器分组路由。
	ErrNotFound = errors.New("server group route not found")
)

const (
	// StateOpen 表示分片或分组处于可服务状态。
	StateOpen = "open"
	// StateClosed 表示分片或分组已关闭。
	StateClosed = "closed"
	// StateDraining 表示分片或分组正在排空。
	StateDraining = "draining"
)

const (
	// WarZoneStatusNotStart 表示战区尚未进入开启流程。
	WarZoneStatusNotStart = "not_start"
	// WarZoneStatusNotOpen 表示战区当前不可开放。
	WarZoneStatusNotOpen = "not_open"
	// WarZoneStatusNotNotice 表示战区尚未进入公告期。
	WarZoneStatusNotNotice = "not_notice"
	// WarZoneStatusNoticed 表示战区已进入公告期。
	WarZoneStatusNoticed = "noticed"
	// WarZoneStatusOpened 表示战区已开放。
	WarZoneStatusOpened = "opened"
	// WarZoneStatusNoConfig 表示战区缺少有效配置。
	WarZoneStatusNoConfig = "no_config"
	// WarZoneStatusReverted 表示战区状态已回退。
	WarZoneStatusReverted = "reverted"
)

// Plan 是服务器分组、分片、战区、合服组和路由的完整配置快照。
type Plan struct {
	Version     string       `json:"version,omitempty"`
	Shards      []Shard      `json:"shards,omitempty"`
	Groups      []Group      `json:"groups,omitempty"`
	WarZones    []WarZone    `json:"war_zones,omitempty"`
	MergeGroups []MergeGroup `json:"merge_groups,omitempty"`
	Routes      []Route      `json:"routes,omitempty"`
	Conflicts   []Conflict   `json:"conflicts,omitempty"`
	UpdatedAt   time.Time    `json:"updated_at,omitempty"`
}

// Shard 是单个逻辑分片的状态和路由归属。
type Shard struct {
	ID           string            `json:"id"`
	GroupID      string            `json:"group_id"`
	State        string            `json:"state,omitempty"`
	OpenAt       time.Time         `json:"open_at,omitempty"`
	PublicOpenAt time.Time         `json:"public_open_at,omitempty"`
	WarZoneID    string            `json:"warzone_id,omitempty"`
	MergeGroupID string            `json:"merge_group_id,omitempty"`
	UpdatedAt    time.Time         `json:"updated_at,omitempty"`
	Meta         map[string]string `json:"meta,omitempty"`
}

// Group 是承载一组分片的服务分组。
type Group struct {
	ID        string            `json:"id"`
	Service   string            `json:"service,omitempty"`
	MemberID  string            `json:"member_id,omitempty"`
	Shards    []string          `json:"shards,omitempty"`
	State     string            `json:"state,omitempty"`
	Weight    int               `json:"weight,omitempty"`
	UpdatedAt time.Time         `json:"updated_at,omitempty"`
	Meta      map[string]string `json:"meta,omitempty"`
}

// WarZone 是跨分片战区的开启状态和成员分片集合。
type WarZone struct {
	ID            string            `json:"id"`
	LeaderGroupID string            `json:"leader_group_id,omitempty"`
	Shards        []string          `json:"shards,omitempty"`
	State         string            `json:"state,omitempty"`
	Status        string            `json:"status,omitempty"`
	SeasonID      string            `json:"season_id,omitempty"`
	NoticeAt      time.Time         `json:"notice_at,omitempty"`
	OpenAt        time.Time         `json:"open_at,omitempty"`
	UpdatedAt     time.Time         `json:"updated_at,omitempty"`
	Meta          map[string]string `json:"meta,omitempty"`
}

// MergeGroup 描述合服后的主分片和被合并分片集合。
type MergeGroup struct {
	ID          string            `json:"id"`
	MainShardID string            `json:"main_shard_id"`
	Shards      []string          `json:"shards,omitempty"`
	State       string            `json:"state,omitempty"`
	UpdatedAt   time.Time         `json:"updated_at,omitempty"`
	Meta        map[string]string `json:"meta,omitempty"`
}

// Route 是功能和分片到服务分组的读写路由。
type Route struct {
	Feature   string            `json:"feature"`
	ShardID   string            `json:"shard_id"`
	GroupID   string            `json:"group_id"`
	Service   string            `json:"service,omitempty"`
	MemberID  string            `json:"member_id,omitempty"`
	State     string            `json:"state,omitempty"`
	UpdatedAt time.Time         `json:"updated_at,omitempty"`
	Meta      map[string]string `json:"meta,omitempty"`
}

// Conflict 描述合服或迁移期间需要阻断的功能路由。
type Conflict struct {
	Feature string `json:"feature"`
	ShardID string `json:"shard_id,omitempty"`
	GroupID string `json:"group_id,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

// Target 是解析路由后的目标分组和可用性结果。
type Target struct {
	Feature         string            `json:"feature,omitempty"`
	ShardID         string            `json:"shard_id,omitempty"`
	GroupID         string            `json:"group_id"`
	Service         string            `json:"service,omitempty"`
	MemberID        string            `json:"member_id,omitempty"`
	Shards          []string          `json:"shards,omitempty"`
	WarZoneID       string            `json:"warzone_id,omitempty"`
	MergeGroupID    string            `json:"merge_group_id,omitempty"`
	MainShardID     string            `json:"main_shard_id,omitempty"`
	RedirectShardID string            `json:"redirect_shard_id,omitempty"`
	Available       bool              `json:"available"`
	Reason          string            `json:"reason,omitempty"`
	Meta            map[string]string `json:"meta,omitempty"`
}

// Snapshot 是服务器分组计划的只读快照。
type Snapshot = Plan

// Change 描述服务器分组计划版本变化。
type Change struct {
	PreviousVersion string    `json:"previous_version,omitempty"`
	CurrentVersion  string    `json:"current_version,omitempty"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// Manager 维护服务器分组计划、索引和变更订阅。
type Manager struct {
	mu       sync.RWMutex
	plan     Plan
	index    planIndex
	watchers map[chan Change]struct{}
}

// New 创建服务器分组管理器并构建路由索引。
func New(plan Plan) (*Manager, error) {
	normalized, index, err := normalizePlan(plan)
	if err != nil {
		return nil, err
	}
	return &Manager{
		plan:     normalized,
		index:    index,
		watchers: make(map[chan Change]struct{}),
	}, nil
}

// Validate 校验服务器分组计划并构建临时索引。
func Validate(plan Plan) error {
	_, _, err := normalizePlan(plan)
	return err
}

// Replace 原子替换服务器分组计划并通知订阅者。
func (m *Manager) Replace(ctx context.Context, plan Plan) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if m == nil {
		return nil
	}
	normalized, index, err := normalizePlan(plan)
	if err != nil {
		return err
	}
	m.mu.Lock()
	previous := m.plan.Version
	m.plan = normalized
	m.index = index
	change := Change{PreviousVersion: previous, CurrentVersion: normalized.Version, UpdatedAt: normalized.UpdatedAt}
	for ch := range m.watchers {
		select {
		case ch <- change:
		default:
		}
	}
	m.mu.Unlock()
	return nil
}

// Snapshot 返回当前服务器分组计划快照。
func (m *Manager) Snapshot() Snapshot {
	if m == nil {
		return Snapshot{}
	}
	m.mu.RLock()
	snapshot := clonePlan(m.plan)
	m.mu.RUnlock()
	return snapshot
}

// Resolve 按功能和分片解析读路由目标。
func (m *Manager) Resolve(ctx context.Context, feature, shardID string) (Target, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return Target{}, false, err
	}
	if m == nil {
		return Target{}, false, ErrNotFound
	}
	feature = normalizeID(feature)
	shardID = normalizeID(shardID)
	if feature == "" || shardID == "" {
		return Target{}, false, ErrNotFound
	}
	m.mu.RLock()
	target, ok := m.index.resolve(feature, shardID)
	m.mu.RUnlock()
	if !ok {
		return Target{}, false, ErrNotFound
	}
	return target, true, nil
}

// ResolveWrite 按功能和分片解析写路由目标，并处理合服重定向。
func (m *Manager) ResolveWrite(ctx context.Context, feature, shardID string) (Target, bool, error) {
	target, ok, err := m.Resolve(ctx, feature, shardID)
	if err != nil || !ok {
		return target, ok, err
	}
	if target.MainShardID != "" && target.ShardID != target.MainShardID {
		target.Available = false
		target.RedirectShardID = target.MainShardID
		target.Reason = "merged_to:" + target.MainShardID
	}
	return target, ok, nil
}

// ResolveGroup 按分组 ID 解析服务分组目标。
func (m *Manager) ResolveGroup(ctx context.Context, groupID string) (Target, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return Target{}, false, err
	}
	if m == nil {
		return Target{}, false, ErrNotFound
	}
	groupID = normalizeID(groupID)
	if groupID == "" {
		return Target{}, false, ErrNotFound
	}
	m.mu.RLock()
	group, ok := m.index.groups[groupID]
	if !ok {
		m.mu.RUnlock()
		return Target{}, false, ErrNotFound
	}
	target := targetFromGroup(group)
	m.mu.RUnlock()
	return target, true, nil
}

// Watch 订阅服务器分组计划变更。
func (m *Manager) Watch(ctx context.Context, buffer int) (<-chan Change, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if m == nil {
		return nil, ErrNotFound
	}
	if buffer < 0 {
		buffer = 0
	}
	ch := make(chan Change, buffer)
	m.mu.Lock()
	if m.watchers == nil {
		m.watchers = make(map[chan Change]struct{})
	}
	m.watchers[ch] = struct{}{}
	m.mu.Unlock()
	go func() {
		<-ctx.Done()
		m.mu.Lock()
		if _, ok := m.watchers[ch]; ok {
			delete(m.watchers, ch)
			close(ch)
		}
		m.mu.Unlock()
	}()
	return ch, nil
}

type planIndex struct {
	shards      map[string]Shard
	groups      map[string]Group
	warZones    map[string]WarZone
	mergeGroups map[string]MergeGroup
	routes      map[string]Route
	conflicts   map[string]Conflict
}

func (idx planIndex) resolve(feature, shardID string) (Target, bool) {
	shard, ok := idx.shards[shardID]
	if !ok {
		return Target{}, false
	}
	groupID := shard.GroupID
	route, hasRoute := idx.routes[routeKey(feature, shardID)]
	if hasRoute {
		groupID = route.GroupID
	}
	group, ok := idx.groups[groupID]
	if !ok {
		return Target{}, false
	}
	target := targetFromGroup(group)
	target.Feature = feature
	target.ShardID = shardID
	target.WarZoneID = shard.WarZoneID
	target.MergeGroupID = shard.MergeGroupID
	if hasRoute {
		if route.Service != "" {
			target.Service = route.Service
		}
		if route.MemberID != "" {
			target.MemberID = route.MemberID
		}
		target.Meta = mergeMeta(target.Meta, route.Meta)
	}
	if target.Meta == nil {
		target.Meta = cloneStringMap(shard.Meta)
	} else {
		target.Meta = mergeMeta(target.Meta, shard.Meta)
	}
	if shard.WarZoneID != "" {
		if warZone, ok := idx.warZones[shard.WarZoneID]; ok {
			target.Meta = mergeMeta(target.Meta, prefixedMeta("warzone.", warZone.Meta))
			if warZone.SeasonID != "" {
				target.Meta = mergeMeta(target.Meta, map[string]string{"warzone.season_id": warZone.SeasonID})
			}
			if warZone.Status != "" {
				target.Meta = mergeMeta(target.Meta, map[string]string{"warzone.status": warZone.Status})
			}
			if !warZone.NoticeAt.IsZero() {
				target.Meta = mergeMeta(target.Meta, map[string]string{"warzone.notice_at": warZone.NoticeAt.Format(time.RFC3339)})
			}
			if !warZone.OpenAt.IsZero() {
				target.Meta = mergeMeta(target.Meta, map[string]string{"warzone.open_at": warZone.OpenAt.Format(time.RFC3339)})
			}
			if warZone.Status != "" && warZone.Status != WarZoneStatusOpened {
				target.Available = false
				target.Reason = "warzone_status:" + warZone.Status
			}
			if warZone.State != StateOpen && (warZone.Status == "" || warZone.Status == WarZoneStatusOpened) {
				target.Available = false
				target.Reason = "warzone_state:" + warZone.State
			}
		}
	}
	if shard.MergeGroupID != "" {
		if mergeGroup, ok := idx.mergeGroups[shard.MergeGroupID]; ok {
			target.MainShardID = mergeGroup.MainShardID
			target.Meta = mergeMeta(target.Meta, prefixedMeta("merge.", mergeGroup.Meta))
			if mergeGroup.State != StateOpen {
				target.Available = false
				target.Reason = "merge_state:" + mergeGroup.State
			}
		}
	}
	if shard.State != StateOpen {
		target.Available = false
		target.Reason = "shard_state:" + shard.State
	}
	if hasRoute && route.State != StateOpen {
		target.Available = false
		target.Reason = "route_state:" + route.State
	}
	if conflict, blocked := idx.conflicts[conflictKey(feature, shardID, "")]; blocked {
		target.Available = false
		target.Reason = firstNonEmpty(conflict.Reason, "shard_conflict")
	}
	if conflict, blocked := idx.conflicts[conflictKey(feature, "", group.ID)]; blocked {
		target.Available = false
		target.Reason = firstNonEmpty(conflict.Reason, "group_conflict")
	}
	return target, true
}

func mergeMeta(base map[string]string, overlays ...map[string]string) map[string]string {
	out := cloneStringMap(base)
	for _, overlay := range overlays {
		for key, value := range overlay {
			if out == nil {
				out = make(map[string]string)
			}
			out[key] = value
		}
	}
	return out
}

func prefixedMeta(prefix string, values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[prefix+key] = value
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func ctxErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}
