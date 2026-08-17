package player

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"longheng.io/server/internal/platform/db"
	profileruntime "longheng.io/server/internal/platform/profile"
	"longheng.io/server/internal/platform/readmodel"
)

// Store 是玩家 Profile 的基础持久化接口。
type Store = db.Store[Profile]

// RoleProfileStore 定义按角色 ID 读取玩家 Profile 的扩展接口。
type RoleProfileStore interface {
	LoadByRoleID(context.Context, string) (Profile, bool, error)
}

// ProfileScanStore 定义按账号游标扫描玩家 Profile 的扩展接口。
type ProfileScanStore interface {
	ListProfiles(context.Context, string, int) ([]Profile, error)
}

// SummaryRebuildOptions 是重建玩家摘要读模型的游标和批量配置。
type SummaryRebuildOptions struct {
	AfterAccountID string `json:"after_account_id,omitempty"`
	BatchSize      int    `json:"batch_size,omitempty"`
	MaxProfiles    int    `json:"max_profiles,omitempty"`
}

// SummaryRebuildResult 是玩家摘要读模型重建结果。
type SummaryRebuildResult struct {
	Scanned       int    `json:"scanned"`
	Rebuilt       int    `json:"rebuilt"`
	ActiveRebuilt int    `json:"active_rebuilt,omitempty"`
	LastAccountID string `json:"last_account_id,omitempty"`
	Complete      bool   `json:"complete"`
}

// ProfileRepair 描述后台修复玩家 Profile 的可选字段。
type ProfileRepair struct {
	AccountID string  `json:"account_id"`
	State     *string `json:"state,omitempty"`
	Name      *string `json:"name,omitempty"`
	Level     *int    `json:"level,omitempty"`
	Reason    string  `json:"reason,omitempty"`
}

// StoreStatus 汇总玩家存储、摘要、异步写入和事件日志状态。
type StoreStatus struct {
	Type               string                 `json:"type"`
	SummaryType        string                 `json:"summary_type,omitempty"`
	AsyncStats         *AsyncStoreStats       `json:"async_stats,omitempty"`
	DeadLetters        []AsyncStoreDeadLetter `json:"dead_letters,omitempty"`
	EventLogConfigured bool                   `json:"eventlog_configured,omitempty"`
	EventLogStrict     bool                   `json:"eventlog_strict,omitempty"`
	EventLogErrors     uint64                 `json:"eventlog_errors,omitempty"`
}

// Module 是玩家 Profile 生命周期、摘要读模型和事件日志的参考实现。
type Module struct {
	logger         *slog.Logger
	store          Store
	summaries      SummaryStore
	profiles       *profileruntime.Lifecycle[Profile, ProfileField]
	events         ProfileEventAppender
	eventAsync     *asyncProfileEvents
	eventsStrict   bool
	eventLogErrors uint64
}

// New 创建使用内存存储的玩家模块。
func New(logger *slog.Logger) *Module {
	return NewWithStore(logger, NewMemoryStore())
}

// NewWithStore 创建使用指定 Profile 存储的玩家模块。
func NewWithStore(logger *slog.Logger, store Store) *Module {
	return NewWithStores(logger, store, NewMemorySummaryStore())
}

// NewWithStoreChecked 创建玩家模块并返回初始化错误。
func NewWithStoreChecked(logger *slog.Logger, store Store) (*Module, error) {
	return NewWithStoresChecked(logger, store, NewMemorySummaryStore())
}

// NewWithStores 创建同时指定 Profile 存储和摘要存储的玩家模块。
func NewWithStores(logger *slog.Logger, store Store, summaries SummaryStore) *Module {
	module, err := NewWithStoresChecked(logger, store, summaries)
	if err != nil {
		panic(err)
	}
	return module
}

// NewWithStoresChecked 创建玩家模块并校验 Profile 运行时配置。
func NewWithStoresChecked(logger *slog.Logger, store Store, summaries SummaryStore) (*Module, error) {
	if store == nil {
		store = NewMemoryStore()
	}
	if summaries == nil {
		summaries = NewMemorySummaryStore()
	}
	summaries = wrapSummaryStore(summaries)
	runtime, err := profileruntime.NewRuntime[Profile, ProfileField](profileruntime.Options[Profile, ProfileField]{
		Store:         store,
		Fields:        profileFieldRegistry,
		Clone:         cloneProfile,
		NewRecord:     newProfile,
		PrepareLoaded: prepareLoadedProfile,
		PrepareSaved:  prepareSavedProfile,
		Touch:         touchProfileRecord,
		SaveFields:    saveProfileFields,
		SameActive:    sameProfileVersion,
		NormalizeKey:  strings.TrimSpace,
	})
	if err != nil {
		return nil, err
	}
	profiles, err := profileruntime.NewLifecycle[Profile, ProfileField](profileruntime.LifecycleOptions[Profile, ProfileField]{
		Store:   store,
		Runtime: runtime,
	})
	if err != nil {
		return nil, err
	}
	return &Module{
		logger:    logger,
		store:     store,
		summaries: summaries,
		profiles:  profiles,
	}, nil
}

// Name 返回玩家模块名。
func (m *Module) Name() string {
	return "player-module"
}

// Start 启动玩家模块，当前参考实现无需额外后台任务。
func (m *Module) Start(context.Context) error {
	return nil
}

// Preflight 检查玩家 Profile 存储和摘要存储是否可用。
func (m *Module) Preflight(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := m.profiles.Preflight(ctx); err != nil {
		return err
	}
	return db.Check(ctx, m.summaries)
}

// Stop 保存所有在线玩家 Profile 并关闭或刷新底层存储。
func (m *Module) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	firstErr := m.profiles.SaveActiveAndUnload(ctx, func(ctx context.Context, accountID string, saveProfile, summaryProfile Profile) error {
		m.observeSummary(ctx, summaryProfile)
		if m.logger != nil {
			m.logger.Info("player profile saved", "account_id", accountID, "role_id", saveProfile.RoleID, "reason", "service_stop")
		}
		return m.emitProfileSaved(ctx, saveProfile, "service_stop")
	})
	storeErr := m.profiles.CloseOrFlush(ctx)
	summaryErr := db.CloseOrFlush(ctx, m.summaries)
	eventErr := m.closeProfileEvents(ctx)
	if firstErr != nil {
		return firstErr
	}
	if storeErr != nil {
		return storeErr
	}
	if summaryErr != nil {
		return summaryErr
	}
	if eventErr != nil {
		return eventErr
	}
	return storeErr
}

// LoadOrCreate 加载玩家 Profile，不存在时创建默认档案并置为在线。
func (m *Module) LoadOrCreate(ctx context.Context, accountID string) (Profile, error) {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return Profile{}, fmt.Errorf("account id is required")
	}
	profile, activated, err := m.profiles.LoadOrCreate(ctx, accountID)
	if err != nil {
		return Profile{}, err
	}
	if activated {
		m.observeSummary(ctx, profile)
	}

	if activated && m.logger != nil {
		m.logger.Info("player profile loaded", "account_id", accountID, "role_id", profile.RoleID)
	}
	return cloneProfile(profile), nil
}

// SaveAndUnload 保存并卸载在线玩家 Profile。
func (m *Module) SaveAndUnload(ctx context.Context, accountID, reason string) (Profile, bool, error) {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return Profile{}, false, fmt.Errorf("account id is required")
	}
	saveProfile, summaryProfile, ok, err := m.profiles.SaveAndUnload(ctx, accountID)
	if err != nil {
		return Profile{}, false, err
	}
	if !ok {
		return Profile{}, false, nil
	}
	m.observeSummary(ctx, summaryProfile)

	if m.logger != nil {
		m.logger.Info("player profile saved", "account_id", accountID, "role_id", saveProfile.RoleID, "reason", reason)
	}
	if err := m.emitProfileSaved(ctx, saveProfile, reason); err != nil {
		return cloneProfile(saveProfile), true, err
	}
	return cloneProfile(saveProfile), true, nil
}

// Upsert 使用后台上下文更新玩家在线状态，兼容旧调用方。
func (m *Module) Upsert(accountID, state string) Record {
	profile, err := m.UpsertWithContext(context.Background(), accountID, state)
	if err != nil && profile.AccountID == "" {
		return Record{}
	}
	return profile
}

// UpsertWithContext 更新玩家在线状态并写入摘要和事件。
func (m *Module) UpsertWithContext(ctx context.Context, accountID, state string) (Record, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return Record{}, fmt.Errorf("account id is required")
	}
	if state == "" {
		state = "active"
	}

	profile, err := m.profiles.Upsert(accountID, func(profile Profile, existed bool, now time.Time) (Profile, []ProfileField, error) {
		fields := dirtyFieldsByModule(ProfileModuleRuntime)
		if !existed {
			profile.LoadedAt = now
		}
		profile.State = state
		profile.UpdatedAt = now
		profile.Version++
		return profile, fields, nil
	})
	if err != nil {
		return Record{}, err
	}
	m.observeSummary(ctx, profile)

	if m.logger != nil {
		m.logger.Info("player state updated", "account_id", accountID, "state", state)
	}
	if err := m.emitProfileChanged(ctx, profile, state); err != nil {
		return cloneProfile(profile), err
	}
	return cloneProfile(profile), nil
}

// Get 从在线生命周期缓存读取玩家 Profile。
func (m *Module) Get(accountID string) (Profile, bool) {
	accountID = strings.TrimSpace(accountID)
	profile, ok := m.profiles.Get(accountID)
	return cloneProfile(profile), ok
}

// LoadReadonly 读取玩家 Profile，优先返回在线缓存但不改变生命周期。
func (m *Module) LoadReadonly(ctx context.Context, accountID string) (Profile, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return Profile{}, false, fmt.Errorf("account id is required")
	}
	if profile, ok := m.Get(accountID); ok {
		return profile, true, nil
	}
	profile, ok, err := m.store.Load(ctx, accountID)
	if err != nil || !ok {
		return Profile{}, ok, err
	}
	return cloneProfile(profile), true, nil
}

// RepairProfile 按后台修复请求修改玩家 Profile 并刷新摘要。
func (m *Module) RepairProfile(ctx context.Context, repair ProfileRepair) (Profile, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	repair.AccountID = strings.TrimSpace(repair.AccountID)
	repair.Reason = strings.TrimSpace(repair.Reason)
	if repair.AccountID == "" {
		return Profile{}, false, fmt.Errorf("account id is required")
	}
	if active, ok := m.Get(repair.AccountID); ok {
		before, profile, err := m.profiles.MutateLoaded(ctx, repair.AccountID, func(profile *Profile, now time.Time) ([]ProfileField, error) {
			return applyProfileRepair(profile, repair, now), nil
		})
		if err != nil {
			return Profile{}, false, err
		}
		m.observeSummaryChange(ctx, before, profile)
		if sameProfileVersion(active, profile) {
			return cloneProfile(profile), true, nil
		}
		if err := m.emitProfileSaved(ctx, profile, repairReason(repair)); err != nil {
			return cloneProfile(profile), true, err
		}
		return cloneProfile(profile), true, nil
	}
	profile, ok, err := m.store.Load(ctx, repair.AccountID)
	if err != nil || !ok {
		return Profile{}, ok, err
	}
	before := cloneProfile(profile)
	fields := applyProfileRepair(&profile, repair, time.Now().UTC())
	if len(fields) == 0 {
		return cloneProfile(profile), true, nil
	}
	if err := m.store.Save(ctx, profile); err != nil {
		return Profile{}, true, err
	}
	m.observeSummaryChange(ctx, before, profile)
	if err := m.emitProfileSaved(ctx, profile, repairReason(repair)); err != nil {
		return cloneProfile(profile), true, err
	}
	return cloneProfile(profile), true, nil
}

// Snapshot 返回当前在线玩家 Profile 快照。
func (m *Module) Snapshot() []Record {
	return m.profiles.Snapshot()
}

// GetSummary 按账号 ID 读取玩家摘要，必要时从 Profile 回填。
func (m *Module) GetSummary(ctx context.Context, accountID string) (PlayerSummary, bool, error) {
	if m.summaries == nil {
		return PlayerSummary{}, false, nil
	}
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return PlayerSummary{}, false, nil
	}
	summary, summaryOK, err := m.summaries.GetPlayerSummary(ctx, accountID)
	if err != nil {
		m.logSummaryReadFail("account", accountID, err)
	}
	if profile, activeOK := m.activeProfileByAcct(accountID); activeOK {
		activeSummary := NewPlayerSummary(profile)
		if err == nil && (!summaryOK || !samePublicSummary(summary, activeSummary)) {
			m.saveSummaryBest(ctx, activeSummary)
		}
		return activeSummary, true, nil
	}
	if err == nil && summaryOK {
		return summary, true, nil
	}
	profile, ok, err := m.store.Load(ctx, accountID)
	if err != nil || !ok {
		return PlayerSummary{}, ok, err
	}
	summary = NewPlayerSummary(profile)
	m.saveSummaryBest(ctx, summary)
	return summary, true, nil
}

// GetSummaryByRoleID 按角色 ID 读取玩家摘要，必要时从 Profile 回填。
func (m *Module) GetSummaryByRoleID(ctx context.Context, roleID string) (PlayerSummary, bool, error) {
	if m.summaries == nil {
		return PlayerSummary{}, false, nil
	}
	roleID = strings.TrimSpace(roleID)
	if roleID == "" {
		return PlayerSummary{}, false, nil
	}
	summary, summaryOK, err := m.summaries.GetPlayerSummaryByRoleID(ctx, roleID)
	if err != nil {
		m.logSummaryReadFail("role", roleID, err)
	}
	if profile, activeOK := m.activeRoleProfile(roleID); activeOK {
		activeSummary := NewPlayerSummary(profile)
		if err == nil && (!summaryOK || !samePublicSummary(summary, activeSummary)) {
			m.saveSummaryBest(ctx, activeSummary)
		}
		return activeSummary, true, nil
	}
	if err == nil && summaryOK {
		return summary, true, nil
	}
	roleStore, ok := m.store.(RoleProfileStore)
	if !ok {
		return PlayerSummary{}, false, nil
	}
	profile, ok, err := roleStore.LoadByRoleID(ctx, roleID)
	if err != nil || !ok {
		return PlayerSummary{}, ok, err
	}
	summary = NewPlayerSummary(profile)
	m.saveSummaryBest(ctx, summary)
	return summary, true, nil
}

// ListSummariesByAccountIDs 按账号 ID 批量读取玩家摘要并保持稳定排序。
func (m *Module) ListSummariesByAccountIDs(ctx context.Context, accountIDs []string) ([]PlayerSummary, error) {
	if m.summaries == nil {
		return nil, nil
	}
	summaries := make([]PlayerSummary, 0, len(accountIDs))
	seen := make(map[string]struct{}, len(accountIDs))
	for _, accountID := range accountIDs {
		accountID = strings.TrimSpace(accountID)
		if accountID == "" {
			continue
		}
		if _, ok := seen[accountID]; ok {
			continue
		}
		seen[accountID] = struct{}{}
		summary, ok, err := m.GetSummary(ctx, accountID)
		if err != nil {
			return nil, err
		}
		if ok {
			summaries = append(summaries, summary)
		}
	}
	sortPlayerSummaries(summaries)
	return summaries, nil
}

// ListSummariesByRoleIDs 按角色 ID 批量读取玩家摘要并保持稳定排序。
func (m *Module) ListSummariesByRoleIDs(ctx context.Context, roleIDs []string) ([]PlayerSummary, error) {
	if m.summaries == nil {
		return nil, nil
	}
	summaries := make([]PlayerSummary, 0, len(roleIDs))
	seen := make(map[string]struct{}, len(roleIDs))
	for _, roleID := range roleIDs {
		roleID = strings.TrimSpace(roleID)
		if roleID == "" {
			continue
		}
		if _, ok := seen[roleID]; ok {
			continue
		}
		seen[roleID] = struct{}{}
		summary, ok, err := m.GetSummaryByRoleID(ctx, roleID)
		if err != nil {
			return nil, err
		}
		if ok {
			summaries = append(summaries, summary)
		}
	}
	sortPlayerSummaries(summaries)
	return summaries, nil
}

// SearchSummaries 按读模型查询条件搜索玩家摘要。
func (m *Module) SearchSummaries(ctx context.Context, query PlayerSummaryQuery) ([]PlayerSummary, error) {
	if m.summaries == nil {
		return nil, nil
	}
	if len(query.AccountIDs) > 0 {
		summaries, err := m.ListSummariesByAccountIDs(ctx, query.AccountIDs)
		return filterSummaries(summaries, query), err
	}
	if len(query.RoleIDs) > 0 {
		summaries, err := m.ListSummariesByRoleIDs(ctx, query.RoleIDs)
		return filterSummaries(summaries, query), err
	}
	return m.summaries.SearchPlayerSummaries(ctx, query)
}

// RebuildSummaries 从 Profile 存储重建玩家摘要读模型。
func (m *Module) RebuildSummaries(ctx context.Context, options SummaryRebuildOptions) (SummaryRebuildResult, error) {
	scanner, ok := m.store.(ProfileScanStore)
	if !ok {
		return SummaryRebuildResult{}, fmt.Errorf("profile store %T does not support profile scans", m.store)
	}
	if m.summaries == nil {
		return SummaryRebuildResult{}, fmt.Errorf("player summary store is nil")
	}
	var flush func(context.Context) error
	if flusher, ok := m.store.(interface {
		Flush(context.Context) error
	}); ok {
		flush = flusher.Flush
	}
	result, err := readmodel.Rebuild(ctx, readmodel.RebuildOptions{
		AfterID:    options.AfterAccountID,
		BatchSize:  options.BatchSize,
		MaxRecords: options.MaxProfiles,
	}, readmodel.RebuildRunner[Profile, PlayerSummary]{
		Scan:           scanner.ListProfiles,
		Flush:          flush,
		ActiveSnapshot: m.Snapshot,
		RecordID: func(profile Profile) string {
			return strings.TrimSpace(profile.AccountID)
		},
		Build: NewPlayerSummary,
		Save:  m.summaries.SavePlayerSummary,
	})
	if flusher, ok := m.summaries.(interface {
		Flush(context.Context) error
	}); ok {
		if flushErr := flusher.Flush(ctx); flushErr != nil {
			if err != nil {
				err = errors.Join(err, flushErr)
			} else {
				err = flushErr
			}
		}
	}
	return SummaryRebuildResult{
		Scanned:       result.Scanned,
		Rebuilt:       result.Rebuilt,
		ActiveRebuilt: result.ActiveRebuilt,
		LastAccountID: result.LastID,
		Complete:      result.Complete,
	}, err
}

// StoreStatus 返回玩家模块底层存储和事件日志状态。
func (m *Module) StoreStatus() StoreStatus {
	status := StoreStatus{
		Type:        fmt.Sprintf("%T", m.store),
		SummaryType: fmt.Sprintf("%T", m.summaries),
	}
	if asyncStore, ok := m.store.(interface {
		Stats() AsyncStoreStats
		DeadLetters() []AsyncStoreDeadLetter
	}); ok {
		stats := asyncStore.Stats()
		status.AsyncStats = &stats
		status.DeadLetters = asyncStore.DeadLetters()
	}
	if m.events != nil {
		status.EventLogConfigured = true
		status.EventLogStrict = m.eventsStrict
		status.EventLogErrors = m.EventLogErrors()
	}
	return status
}

// DeadLetters 返回玩家异步存储中尚未处理的死信记录。
func (m *Module) DeadLetters() []AsyncStoreDeadLetter {
	if asyncStore, ok := m.store.(interface {
		DeadLetters() []AsyncStoreDeadLetter
	}); ok {
		return asyncStore.DeadLetters()
	}
	return nil
}

// RequeueDeadLetter 将指定账号的异步死信重新放回待写队列。
func (m *Module) RequeueDeadLetter(accountID string) bool {
	accountID = strings.TrimSpace(accountID)
	if asyncStore, ok := m.store.(interface {
		RequeueDeadLetter(string) bool
	}); ok {
		return asyncStore.RequeueDeadLetter(accountID)
	}
	return false
}

func applyProfileRepair(profile *Profile, repair ProfileRepair, now time.Time) []ProfileField {
	if profile == nil {
		return nil
	}
	fields := newProfileFieldSet()
	if repair.State != nil {
		state := strings.TrimSpace(*repair.State)
		if profile.State != state {
			profile.State = state
			fields.Add(ProfileFieldRuntime)
		}
	}
	if repair.Name != nil {
		name := strings.TrimSpace(*repair.Name)
		if profile.Name != name {
			profile.Name = name
			fields.Add(ProfileFieldBase)
		}
	}
	if repair.Level != nil && profile.Level != *repair.Level {
		profile.Level = *repair.Level
		fields.Add(ProfileFieldBase)
	}
	if len(fields.List()) == 0 {
		return nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	profile.UpdatedAt = now
	profile.Version++
	fields.Add(ProfileFieldRuntime, ProfileFieldMeta)
	return fields.List()
}

func repairReason(repair ProfileRepair) string {
	reason := strings.TrimSpace(repair.Reason)
	if reason == "" {
		return "admin_repair"
	}
	return "admin_repair:" + reason
}

func (m *Module) observeSummary(ctx context.Context, profile Profile) {
	if m.summaries == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := m.summaries.SavePlayerSummary(ctx, NewPlayerSummary(profile)); err != nil && m.logger != nil {
		m.logger.Error("player summary update failed", "account_id", profile.AccountID, "error", err)
	}
}

func (m *Module) activeProfileByAcct(accountID string) (Profile, bool) {
	accountID = strings.TrimSpace(accountID)
	profile, ok := m.profiles.Get(accountID)
	return cloneProfile(profile), ok
}

func (m *Module) activeRoleProfile(roleID string) (Profile, bool) {
	roleID = strings.TrimSpace(roleID)
	profile, ok := m.profiles.ActiveByPredicate(func(profile Profile) bool {
		return profile.RoleID == roleID
	})
	return cloneProfile(profile), ok
}

func (m *Module) logSummaryReadFail(scope, key string, err error) {
	if m.logger == nil || err == nil {
		return
	}
	attrs := []any{"scope", scope, "error", err}
	if key != "" {
		attrs = append(attrs, "key", key)
	}
	m.logger.Warn("player summary read failed, falling back", attrs...)
}

func (m *Module) saveSummaryBest(ctx context.Context, summary PlayerSummary) {
	if m.summaries == nil {
		return
	}
	if err := m.summaries.SavePlayerSummary(ctx, summary); err != nil && m.logger != nil {
		m.logger.Error("player summary backfill failed", "account_id", summary.AccountID, "error", err)
	}
}

func (m *Module) observeSummaryChange(ctx context.Context, before, after Profile) {
	beforeSummary := NewPlayerSummary(before)
	afterSummary := NewPlayerSummary(after)
	if samePublicSummary(beforeSummary, afterSummary) {
		return
	}
	m.observeSummary(ctx, after)
}

func samePublicSummary(left, right PlayerSummary) bool {
	return left.AccountID == right.AccountID &&
		left.RoleID == right.RoleID &&
		left.Name == right.Name &&
		left.Level == right.Level &&
		left.State == right.State &&
		left.Online == right.Online
}
