package moderation

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalidSanction = errors.New("moderation sanction is invalid")
	ErrSanctionMissing = errors.New("moderation sanction not found")
)

type SanctionKind string

const (
	SanctionMute SanctionKind = "mute"
	SanctionBan  SanctionKind = "ban"
)

type Sanction struct {
	ID        string            `json:"id"`
	Subject   string            `json:"subject"`
	Scope     string            `json:"scope,omitempty"`
	Kind      SanctionKind      `json:"kind"`
	Reason    string            `json:"reason,omitempty"`
	Source    string            `json:"source,omitempty"`
	Until     time.Time         `json:"until,omitempty"`
	CreatedAt time.Time         `json:"created_at,omitempty"`
	Meta      map[string]string `json:"meta,omitempty"`
}

type SanctionQuery struct {
	Subject string         `json:"subject"`
	Scope   string         `json:"scope,omitempty"`
	Kinds   []SanctionKind `json:"kinds,omitempty"`
	Now     time.Time      `json:"now,omitempty"`
}

type SanctionSnapshot struct {
	Items []Sanction `json:"items,omitempty"`
}

type SanctionStore interface {
	Upsert(context.Context, Sanction) (Sanction, error)
	Remove(context.Context, string) error
	Active(context.Context, SanctionQuery) ([]Sanction, error)
	Snapshot(context.Context) (SanctionSnapshot, error)
}

type MemorySanctionStore struct {
	mu        sync.RWMutex
	items     map[string]Sanction
	bySubject map[string]map[string]struct{}
	now       func() time.Time
}

func NewMemorySanctionStore() *MemorySanctionStore {
	return &MemorySanctionStore{
		items:     make(map[string]Sanction),
		bySubject: make(map[string]map[string]struct{}),
		now:       time.Now,
	}
}

func (s *MemorySanctionStore) SetNow(now func() time.Time) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.now = now
	s.mu.Unlock()
}

func (s *MemorySanctionStore) Upsert(ctx context.Context, sanction Sanction) (Sanction, error) {
	if err := ctxErr(ctx); err != nil {
		return Sanction{}, err
	}
	if s == nil {
		return Sanction{}, ErrSanctionMissing
	}
	item, err := normalizeSanction(sanction, s.nowUTC())
	if err != nil {
		return Sanction{}, err
	}
	s.mu.Lock()
	if s.items == nil {
		s.items = make(map[string]Sanction)
	}
	if s.bySubject == nil {
		s.bySubject = make(map[string]map[string]struct{})
	}
	if existing, ok := s.items[item.ID]; ok && existing.Subject != item.Subject {
		s.removeSubjectIdx(existing.Subject, item.ID)
	}
	s.items[item.ID] = cloneSanction(item)
	s.addSubjectIndex(item.Subject, item.ID)
	s.mu.Unlock()
	return item, nil
}

func (s *MemorySanctionStore) Remove(ctx context.Context, id string) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if s == nil {
		return ErrSanctionMissing
	}
	id = normalizeToken(id)
	if id == "" {
		return fmt.Errorf("%w: id is required", ErrInvalidSanction)
	}
	s.mu.Lock()
	if _, ok := s.items[id]; !ok {
		s.mu.Unlock()
		return ErrSanctionMissing
	}
	s.removeSubjectIdx(s.items[id].Subject, id)
	delete(s.items, id)
	s.mu.Unlock()
	return nil
}

func (s *MemorySanctionStore) Active(ctx context.Context, query SanctionQuery) ([]Sanction, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if s == nil {
		return nil, nil
	}
	query.Subject = normalizeToken(query.Subject)
	query.Scope = normalizeToken(query.Scope)
	if query.Now.IsZero() {
		query.Now = s.nowUTC()
	} else {
		query.Now = query.Now.UTC()
	}
	kindSet := sanctionKindSet(query.Kinds)

	s.mu.RLock()
	out := make([]Sanction, 0)
	ids := make([]string, 0, len(s.bySubject[query.Subject]))
	for id := range s.bySubject[query.Subject] {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		item, ok := s.items[id]
		if !ok {
			continue
		}
		if !sanctionScopeApplies(item.Scope, query.Scope) {
			continue
		}
		if len(kindSet) > 0 {
			if _, ok := kindSet[item.Kind]; !ok {
				continue
			}
		}
		if !item.ActiveAt(query.Now) {
			continue
		}
		out = append(out, cloneSanction(item))
	}
	s.mu.RUnlock()
	sortSanctions(out)
	return out, nil
}

func (s *MemorySanctionStore) Snapshot(ctx context.Context) (SanctionSnapshot, error) {
	if err := ctxErr(ctx); err != nil {
		return SanctionSnapshot{}, err
	}
	if s == nil {
		return SanctionSnapshot{}, nil
	}
	s.mu.RLock()
	items := make([]Sanction, 0, len(s.items))
	for _, item := range s.items {
		items = append(items, cloneSanction(item))
	}
	s.mu.RUnlock()
	sortSanctions(items)
	return SanctionSnapshot{Items: items}, nil
}

func (s *MemorySanctionStore) addSubjectIndex(subject, id string) {
	if s.bySubject[subject] == nil {
		s.bySubject[subject] = make(map[string]struct{})
	}
	s.bySubject[subject][id] = struct{}{}
}

func (s *MemorySanctionStore) removeSubjectIdx(subject, id string) {
	if ids := s.bySubject[subject]; ids != nil {
		delete(ids, id)
		if len(ids) == 0 {
			delete(s.bySubject, subject)
		}
	}
}

func (s Sanction) ActiveAt(now time.Time) bool {
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	return s.Until.IsZero() || s.Until.After(now)
}

func normalizeSanction(sanction Sanction, now time.Time) (Sanction, error) {
	sanction.ID = normalizeToken(sanction.ID)
	sanction.Subject = normalizeToken(sanction.Subject)
	sanction.Scope = normalizeToken(sanction.Scope)
	sanction.Reason = strings.TrimSpace(sanction.Reason)
	sanction.Source = strings.TrimSpace(sanction.Source)
	sanction.Meta = cloneStringMap(sanction.Meta)
	if sanction.Subject == "" {
		return Sanction{}, fmt.Errorf("%w: subject is required", ErrInvalidSanction)
	}
	if sanction.Kind != SanctionMute && sanction.Kind != SanctionBan {
		return Sanction{}, fmt.Errorf("%w: invalid kind %q", ErrInvalidSanction, sanction.Kind)
	}
	if sanction.ID == "" {
		if hasDefaultIDDelim(sanction.Subject) || hasDefaultIDDelim(sanction.Scope) {
			return Sanction{}, fmt.Errorf("%w: subject or scope contains reserved delimiter", ErrInvalidSanction)
		}
		sanction.ID = sanction.Subject + ":" + string(sanction.Kind) + ":" + sanction.Scope
	}
	if sanction.CreatedAt.IsZero() {
		sanction.CreatedAt = now.UTC()
	} else {
		sanction.CreatedAt = sanction.CreatedAt.UTC()
	}
	if !sanction.Until.IsZero() {
		sanction.Until = sanction.Until.UTC()
	}
	return sanction, nil
}

func sanctionKindSet(kinds []SanctionKind) map[SanctionKind]struct{} {
	if len(kinds) == 0 {
		return nil
	}
	out := make(map[SanctionKind]struct{}, len(kinds))
	for _, kind := range kinds {
		if kind == "" {
			continue
		}
		out[kind] = struct{}{}
	}
	return out
}

func sanctionScopeApplies(sanctionScope, requestScope string) bool {
	sanctionScope = normalizeToken(sanctionScope)
	requestScope = normalizeToken(requestScope)
	return sanctionScope == "" || sanctionScope == "global" || sanctionScope == requestScope
}

// hasDefaultIDDelim 拦截会让默认处罚 ID 产生歧义的保留分隔符。
func hasDefaultIDDelim(value string) bool {
	return strings.Contains(value, ":") || strings.ContainsRune(value, '\x00')
}

// encodeModerationKey 用长度前缀拼接多段键，避免字段内容自带分隔符时发生碰撞。
func encodeModerationKey(parts ...string) string {
	var builder strings.Builder
	for _, part := range parts {
		builder.WriteString(strconv.Itoa(len(part)))
		builder.WriteByte(':')
		builder.WriteString(part)
	}
	return builder.String()
}

func sortSanctions(items []Sanction) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Subject != items[j].Subject {
			return items[i].Subject < items[j].Subject
		}
		if items[i].Scope != items[j].Scope {
			return items[i].Scope < items[j].Scope
		}
		if items[i].Kind != items[j].Kind {
			return items[i].Kind < items[j].Kind
		}
		return items[i].ID < items[j].ID
	})
}

func cloneSanction(sanction Sanction) Sanction {
	sanction.Meta = cloneStringMap(sanction.Meta)
	return sanction
}

func (s *MemorySanctionStore) nowUTC() time.Time {
	s.mu.RLock()
	now := s.now
	s.mu.RUnlock()
	if now == nil {
		return time.Now().UTC()
	}
	return now().UTC()
}

func ctxErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}
