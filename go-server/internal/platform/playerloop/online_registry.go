package playerloop

import (
	"strings"
	"sync"
	"time"
)

// OnlineRecord 记录账号在线状态和最近变更时间。
type OnlineRecord struct {
	AccountID string    `json:"account_id"`
	Online    bool      `json:"online"`
	UpdatedAt time.Time `json:"updated_at"`
}

// OnlineRegistry 在内存中维护账号在线状态。
type OnlineRegistry struct {
	mu      sync.RWMutex
	records map[string]OnlineRecord
	now     func() time.Time
}

// NewOnlineRegistry 使用系统时钟创建在线状态表。
func NewOnlineRegistry() *OnlineRegistry {
	return NewOnlineRegistryWithClock(time.Now)
}

// NewOnlineRegistryWithClock 使用指定时钟创建在线状态表，便于测试和回放。
func NewOnlineRegistryWithClock(now func() time.Time) *OnlineRegistry {
	if now == nil {
		now = time.Now
	}
	return &OnlineRegistry{
		records: make(map[string]OnlineRecord),
		now:     now,
	}
}

// MarkOnline 标记账号在线并返回新状态。
func (r *OnlineRegistry) MarkOnline(accountID string) OnlineRecord {
	return r.set(accountID, true)
}

// MarkOffline 标记账号离线并返回新状态。
func (r *OnlineRegistry) MarkOffline(accountID string) OnlineRecord {
	return r.set(accountID, false)
}

// IsOnline 判断账号当前是否在线。
func (r *OnlineRegistry) IsOnline(accountID string) bool {
	accountID = strings.TrimSpace(accountID)
	if r == nil || accountID == "" {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.records[accountID].Online
}

// Snapshot 返回在线状态表的当前副本。
func (r *OnlineRegistry) Snapshot() []OnlineRecord {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]OnlineRecord, 0, len(r.records))
	for _, record := range r.records {
		out = append(out, record)
	}
	return out
}

func (r *OnlineRegistry) set(accountID string, online bool) OnlineRecord {
	accountID = strings.TrimSpace(accountID)
	if r == nil || accountID == "" {
		return OnlineRecord{}
	}
	record := OnlineRecord{AccountID: accountID, Online: online, UpdatedAt: r.now().UTC()}
	r.mu.Lock()
	r.records[accountID] = record
	r.mu.Unlock()
	return record
}
