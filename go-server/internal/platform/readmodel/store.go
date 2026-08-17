package readmodel

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
)

var ErrSecondaryConflict = errors.New("read model secondary id conflict")

type Store[T any, Q any] interface {
	Save(context.Context, T) error
	Get(context.Context, string) (T, bool, error)
	GetBySecondaryID(context.Context, string) (T, bool, error)
	ListByPrimaryIDs(context.Context, []string) ([]T, error)
	ListBySecondaryIDs(context.Context, []string) ([]T, error)
	Search(context.Context, Q) ([]T, error)
}

type ModelOptions[T any, Q any] struct {
	PrimaryID         func(T) string
	SecondaryID       func(T) string
	Normalize         func(T) T
	Clone             func(T) T
	Match             func(T, Q) bool
	Less              func(T, T) bool
	QueryPrimaryIDs   func(Q) []string
	QuerySecondaryIDs func(Q) []string
	QueryLimit        func(Q) int
}

func (o ModelOptions[T, Q]) validate() error {
	if o.PrimaryID == nil {
		return errors.New("read model primary id function is required")
	}
	if o.SecondaryID == nil {
		return errors.New("read model secondary id function is required")
	}
	return nil
}

func (o ModelOptions[T, Q]) normalize(record T) T {
	if o.Normalize != nil {
		return o.Normalize(record)
	}
	return record
}

func (o ModelOptions[T, Q]) clone(record T) T {
	if o.Clone != nil {
		return o.Clone(record)
	}
	return record
}

func (o ModelOptions[T, Q]) primaryID(record T) string {
	return strings.TrimSpace(o.PrimaryID(record))
}

func (o ModelOptions[T, Q]) secondaryID(record T) string {
	return strings.TrimSpace(o.SecondaryID(record))
}

func (o ModelOptions[T, Q]) queryPrimaryIDs(query Q) []string {
	if o.QueryPrimaryIDs == nil {
		return nil
	}
	return normalizeIDs(o.QueryPrimaryIDs(query))
}

func (o ModelOptions[T, Q]) querySecondaryIDs(query Q) []string {
	if o.QuerySecondaryIDs == nil {
		return nil
	}
	return normalizeIDs(o.QuerySecondaryIDs(query))
}

func (o ModelOptions[T, Q]) queryLimit(query Q) int {
	if o.QueryLimit == nil {
		return 0
	}
	limit := o.QueryLimit(query)
	if limit < 0 {
		return 0
	}
	return limit
}

func (o ModelOptions[T, Q]) match(record T, query Q) bool {
	if o.Match == nil {
		return true
	}
	return o.Match(record, query)
}

func (o ModelOptions[T, Q]) sort(records []T) {
	if o.Less == nil {
		return
	}
	sort.Slice(records, func(i, j int) bool {
		return o.Less(records[i], records[j])
	})
}

func (o ModelOptions[T, Q]) filter(records []T, query Q) []T {
	out := make([]T, 0, len(records))
	for _, record := range records {
		candidate := o.clone(record)
		if o.match(candidate, query) {
			out = append(out, candidate)
		}
	}
	o.sort(out)
	if limit := o.queryLimit(query); limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

type MemoryStore[T any, Q any] struct {
	options   ModelOptions[T, Q]
	mu        sync.RWMutex
	byPrimary map[string]T
	secondary map[string]string
}

func NewMemoryStore[T any, Q any](options ModelOptions[T, Q]) (*MemoryStore[T, Q], error) {
	if err := options.validate(); err != nil {
		return nil, err
	}
	return &MemoryStore[T, Q]{
		options:   options,
		byPrimary: make(map[string]T),
		secondary: make(map[string]string),
	}, nil
}

func (s *MemoryStore[T, Q]) Save(ctx context.Context, record T) error {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	record = s.options.normalize(record)
	primaryID := s.options.primaryID(record)
	if primaryID == "" {
		return nil
	}
	secondaryID := s.options.secondaryID(record)

	s.mu.Lock()
	defer s.mu.Unlock()
	if secondaryID != "" {
		if owner, ok := s.secondary[secondaryID]; ok && owner != primaryID {
			return ErrSecondaryConflict
		}
	}
	if old, ok := s.byPrimary[primaryID]; ok {
		oldSecondaryID := s.options.secondaryID(old)
		if oldSecondaryID != "" && oldSecondaryID != secondaryID {
			delete(s.secondary, oldSecondaryID)
		}
	}
	s.byPrimary[primaryID] = s.options.clone(record)
	if secondaryID != "" {
		s.secondary[secondaryID] = primaryID
	}
	return nil
}

func (s *MemoryStore[T, Q]) Get(ctx context.Context, primaryID string) (T, bool, error) {
	ctx = contextOrBackground(ctx)
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, false, err
	}
	primaryID = strings.TrimSpace(primaryID)
	if primaryID == "" {
		return zero, false, nil
	}
	s.mu.RLock()
	record, ok := s.byPrimary[primaryID]
	s.mu.RUnlock()
	return s.options.clone(record), ok, nil
}

func (s *MemoryStore[T, Q]) GetBySecondaryID(ctx context.Context, secondaryID string) (T, bool, error) {
	ctx = contextOrBackground(ctx)
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, false, err
	}
	secondaryID = strings.TrimSpace(secondaryID)
	if secondaryID == "" {
		return zero, false, nil
	}
	s.mu.RLock()
	primaryID := s.secondary[secondaryID]
	record, ok := s.byPrimary[primaryID]
	s.mu.RUnlock()
	if !ok || s.options.secondaryID(record) != secondaryID {
		return zero, false, nil
	}
	return s.options.clone(record), true, nil
}

func (s *MemoryStore[T, Q]) ListByPrimaryIDs(ctx context.Context, primaryIDs []string) ([]T, error) {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	primaryIDs = normalizeIDs(primaryIDs)
	s.mu.RLock()
	out := make([]T, 0, len(primaryIDs))
	for _, primaryID := range primaryIDs {
		if record, ok := s.byPrimary[primaryID]; ok {
			out = append(out, s.options.clone(record))
		}
	}
	s.mu.RUnlock()
	s.options.sort(out)
	return out, nil
}

func (s *MemoryStore[T, Q]) ListBySecondaryIDs(ctx context.Context, secondaryIDs []string) ([]T, error) {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	secondaryIDs = normalizeIDs(secondaryIDs)
	s.mu.RLock()
	out := make([]T, 0, len(secondaryIDs))
	for _, secondaryID := range secondaryIDs {
		if record, ok := s.byPrimary[s.secondary[secondaryID]]; ok && s.options.secondaryID(record) == secondaryID {
			out = append(out, s.options.clone(record))
		}
	}
	s.mu.RUnlock()
	s.options.sort(out)
	return out, nil
}

func (s *MemoryStore[T, Q]) Search(ctx context.Context, query Q) ([]T, error) {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if ids := s.options.queryPrimaryIDs(query); len(ids) > 0 {
		records, err := s.ListByPrimaryIDs(ctx, ids)
		return s.options.filter(records, query), err
	}
	if ids := s.options.querySecondaryIDs(query); len(ids) > 0 {
		records, err := s.ListBySecondaryIDs(ctx, ids)
		return s.options.filter(records, query), err
	}

	s.mu.RLock()
	candidates := make([]T, 0, len(s.byPrimary))
	for _, record := range s.byPrimary {
		candidates = append(candidates, record)
	}
	s.mu.RUnlock()
	return s.options.filter(candidates, query), nil
}

func normalizeIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	out := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func contextOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
