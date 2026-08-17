package statesync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrScopeRequired   = errors.New("state sync scope is required")
	ErrKeyRequired     = errors.New("state sync key is required")
	ErrRecordNotFound  = errors.New("state sync record not found")
	ErrStaleVersion    = errors.New("state sync version is stale")
	ErrVersionConflict = errors.New("state sync version conflicts with existing record")
	ErrPayloadTooLarge = errors.New("state sync payload is too large")
	ErrPayloadInvalid  = errors.New("state sync payload is invalid")
	ErrUnsafeName      = errors.New("state sync scope or key contains unsafe character")
	ErrStoreClosed     = errors.New("state sync store is closed")
)

type EventType string

const (
	EventUpsert EventType = "upsert"
	EventDelete EventType = "delete"
)

type Record struct {
	Scope     string          `json:"scope"`
	Key       string          `json:"key"`
	Version   int64           `json:"version"`
	OwnerNode string          `json:"owner_node,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	UpdatedAt time.Time       `json:"updated_at"`
	ExpiresAt time.Time       `json:"expires_at,omitempty"`
	Tombstone bool            `json:"tombstone,omitempty"`
}

type Event struct {
	Type   EventType `json:"type"`
	Record Record    `json:"record"`
}

type Options struct {
	MaxPayloadBytes int
	Now             func() time.Time
}

type Store interface {
	Upsert(context.Context, Record) (Record, error)
	Delete(context.Context, string, string, int64) (Record, error)
	Get(context.Context, string, string) (Record, bool, error)
	List(context.Context, string) ([]Record, error)
	Watch(context.Context, string, int) (<-chan Event, error)
	SweepExpired(context.Context) (int, error)
	Snapshot() Snapshot
	Close() error
}

type Snapshot struct {
	Records  int            `json:"records"`
	Watchers map[string]int `json:"watchers,omitempty"`
	Closed   bool           `json:"closed"`
}

type Memory struct {
	mu              sync.RWMutex
	records         map[string]Record
	watchers        map[string]map[chan Event]struct{}
	maxPayloadBytes int
	now             func() time.Time
	done            chan struct{}
	closed          bool
}

func NewMemory(options Options) *Memory {
	now := options.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Memory{
		records:         make(map[string]Record),
		watchers:        make(map[string]map[chan Event]struct{}),
		maxPayloadBytes: options.MaxPayloadBytes,
		now:             now,
		done:            make(chan struct{}),
	}
}

func (m *Memory) Upsert(ctx context.Context, record Record) (Record, error) {
	if err := contextErr(ctx); err != nil {
		return Record{}, err
	}
	if m == nil {
		return Record{}, errors.New("state sync memory store is nil")
	}
	record, err := normalizeRecord(record, m.now().UTC())
	if err != nil {
		return Record{}, err
	}
	if m.maxPayloadBytes > 0 && len(record.Payload) > m.maxPayloadBytes {
		return Record{}, fmt.Errorf("%w: %d > %d", ErrPayloadTooLarge, len(record.Payload), m.maxPayloadBytes)
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return Record{}, ErrStoreClosed
	}
	key := recordKey(record.Scope, record.Key)
	if current, ok := m.records[key]; ok {
		switch {
		case current.Version > record.Version:
			m.mu.Unlock()
			return Record{}, fmt.Errorf("%w: current=%d next=%d", ErrStaleVersion, current.Version, record.Version)
		case current.Version == record.Version:
			if sameRecordValue(current, record) {
				m.mu.Unlock()
				return cloneRecord(current), nil
			}
			m.mu.Unlock()
			return Record{}, fmt.Errorf("%w: scope=%s key=%s version=%d", ErrVersionConflict, record.Scope, record.Key, record.Version)
		}
	}
	m.records[key] = cloneRecord(record)
	m.publishLocked(ctx, record.Scope, Event{Type: EventUpsert, Record: record})
	m.mu.Unlock()
	return cloneRecord(record), nil
}

func (m *Memory) Delete(ctx context.Context, scope, key string, version int64) (Record, error) {
	if err := contextErr(ctx); err != nil {
		return Record{}, err
	}
	if m == nil {
		return Record{}, errors.New("state sync memory store is nil")
	}
	scope, key, err := normalizeScopeKey(scope, key)
	if err != nil {
		return Record{}, err
	}
	if version <= 0 {
		version = 1
	}
	now := m.now().UTC()
	deleted := Record{Scope: scope, Key: key, Version: version, UpdatedAt: now, Tombstone: true}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return Record{}, ErrStoreClosed
	}
	recordKey := recordKey(scope, key)
	if current, ok := m.records[recordKey]; ok {
		switch {
		case current.Version > version:
			m.mu.Unlock()
			return Record{}, fmt.Errorf("%w: current=%d next=%d", ErrStaleVersion, current.Version, version)
		case current.Version == version:
			if current.Tombstone {
				m.mu.Unlock()
				return cloneRecord(current), nil
			}
			m.mu.Unlock()
			return Record{}, fmt.Errorf("%w: scope=%s key=%s version=%d", ErrVersionConflict, scope, key, version)
		}
	}
	m.records[recordKey] = deleted
	m.publishLocked(ctx, scope, Event{Type: EventDelete, Record: deleted})
	m.mu.Unlock()
	return cloneRecord(deleted), nil
}

func (m *Memory) Get(ctx context.Context, scope, key string) (Record, bool, error) {
	if err := contextErr(ctx); err != nil {
		return Record{}, false, err
	}
	if m == nil {
		return Record{}, false, nil
	}
	scope, key, err := normalizeScopeKey(scope, key)
	if err != nil {
		return Record{}, false, err
	}
	now := m.now().UTC()
	m.mu.RLock()
	record, ok := m.records[recordKey(scope, key)]
	m.mu.RUnlock()
	if !ok || isExpired(record, now) || record.Tombstone {
		return Record{}, false, nil
	}
	return cloneRecord(record), true, nil
}

func (m *Memory) List(ctx context.Context, scope string) ([]Record, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	if m == nil {
		return nil, nil
	}
	scope, err := normalizeScope(scope)
	if err != nil {
		return nil, err
	}
	now := m.now().UTC()
	prefix := scope + "/"
	var out []Record
	m.mu.RLock()
	for key, record := range m.records {
		if !strings.HasPrefix(key, prefix) || record.Tombstone || isExpired(record, now) {
			continue
		}
		out = append(out, cloneRecord(record))
	}
	m.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

func (m *Memory) Watch(ctx context.Context, scope string, buffer int) (<-chan Event, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if m == nil {
		return nil, errors.New("state sync memory store is nil")
	}
	scope, err := normalizeScope(scope)
	if err != nil {
		return nil, err
	}
	if buffer < 0 {
		buffer = 0
	}
	ch := make(chan Event, buffer)
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		close(ch)
		return ch, nil
	}
	if m.watchers[scope] == nil {
		m.watchers[scope] = make(map[chan Event]struct{})
	}
	m.watchers[scope][ch] = struct{}{}
	m.mu.Unlock()

	go func() {
		select {
		case <-ctx.Done():
		case <-m.done:
		}
		m.mu.Lock()
		if watchers := m.watchers[scope]; watchers != nil {
			if _, ok := watchers[ch]; ok {
				delete(watchers, ch)
				close(ch)
			}
			if len(watchers) == 0 {
				delete(m.watchers, scope)
			}
		}
		m.mu.Unlock()
	}()
	return ch, nil
}

func (m *Memory) SweepExpired(ctx context.Context) (int, error) {
	if err := contextErr(ctx); err != nil {
		return 0, err
	}
	if m == nil {
		return 0, nil
	}
	now := m.now().UTC()
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return 0, ErrStoreClosed
	}
	removed := 0
	for key, record := range m.records {
		if record.Tombstone || isExpired(record, now) {
			delete(m.records, key)
			removed++
		}
	}
	m.mu.Unlock()
	return removed, nil
}

func (m *Memory) Snapshot() Snapshot {
	if m == nil {
		return Snapshot{}
	}
	m.mu.RLock()
	watchers := make(map[string]int, len(m.watchers))
	for scope, list := range m.watchers {
		watchers[scope] = len(list)
	}
	snapshot := Snapshot{Records: len(m.records), Watchers: watchers, Closed: m.closed}
	m.mu.RUnlock()
	if len(snapshot.Watchers) == 0 {
		snapshot.Watchers = nil
	}
	return snapshot
}

func (m *Memory) Close() error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	close(m.done)
	for scope, watchers := range m.watchers {
		for ch := range watchers {
			close(ch)
		}
		delete(m.watchers, scope)
	}
	m.mu.Unlock()
	return nil
}

// publishLocked 只做非阻塞发送，调用方必须持有 m.mu 写锁。
// Watch/Close 也在这把锁内关闭 channel，从根源上消除 send-vs-close 竞争。
func (m *Memory) publishLocked(ctx context.Context, scope string, event Event) {
	if done := contextDone(ctx); done != nil {
		select {
		case <-done:
			return
		default:
		}
	}
	for ch := range m.watchers[scope] {
		select {
		case ch <- cloneEvent(event):
		default:
		}
	}
}

func normalizeRecord(record Record, now time.Time) (Record, error) {
	scope, key, err := normalizeScopeKey(record.Scope, record.Key)
	if err != nil {
		return Record{}, err
	}
	record.Scope = scope
	record.Key = key
	record.OwnerNode = strings.TrimSpace(record.OwnerNode)
	if record.Version <= 0 {
		record.Version = 1
	}
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = now.UTC()
	} else {
		record.UpdatedAt = record.UpdatedAt.UTC()
	}
	if !record.ExpiresAt.IsZero() {
		record.ExpiresAt = record.ExpiresAt.UTC()
	}
	if len(record.Payload) > 0 && !json.Valid(record.Payload) {
		return Record{}, ErrPayloadInvalid
	}
	record.Payload = cloneJSON(record.Payload)
	return record, nil
}

func normalizeScopeKey(scope, key string) (string, string, error) {
	scope, err := normalizeScope(scope)
	if err != nil {
		return "", "", err
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return "", "", ErrKeyRequired
	}
	if hasUnsafeNameRune(key) {
		return "", "", ErrUnsafeName
	}
	return scope, key, nil
}

func normalizeScope(scope string) (string, error) {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return "", ErrScopeRequired
	}
	if hasUnsafeNameRune(scope) {
		return "", ErrUnsafeName
	}
	return scope, nil
}

func recordKey(scope, key string) string {
	return scope + "/" + key
}

func isExpired(record Record, now time.Time) bool {
	return !record.ExpiresAt.IsZero() && !record.ExpiresAt.After(now)
}

func sameRecordValue(left, right Record) bool {
	return left.OwnerNode == right.OwnerNode &&
		left.ExpiresAt.Equal(right.ExpiresAt) &&
		left.Tombstone == right.Tombstone &&
		string(left.Payload) == string(right.Payload)
}

func cloneEvent(event Event) Event {
	event.Record = cloneRecord(event.Record)
	return event
}

func cloneRecord(record Record) Record {
	record.Payload = cloneJSON(record.Payload)
	return record
}

func cloneJSON(data json.RawMessage) json.RawMessage {
	if data == nil {
		return nil
	}
	return append(json.RawMessage(nil), data...)
}

func hasUnsafeNameRune(value string) bool {
	for _, r := range value {
		if r == '/' || r == '\\' || r == 0 || r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func contextDone(ctx context.Context) <-chan struct{} {
	if ctx == nil {
		return nil
	}
	return ctx.Done()
}
