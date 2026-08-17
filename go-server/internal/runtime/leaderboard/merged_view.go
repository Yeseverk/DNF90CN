package leaderboard

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

func (m *Manager) MergedView(ctx context.Context, options MergedViewOptions) (MergedView, error) {
	if err := ctxErr(ctx); err != nil {
		return MergedView{}, err
	}
	if m == nil {
		return MergedView{}, ErrManagerRequired
	}
	normalized, err := normMergedViewOpts(options)
	if err != nil {
		return MergedView{}, err
	}

	m.mu.Lock()
	m.ensureReadyLocked()
	sources := make([]boardRankData, 0, len(normalized.IDs))
	for _, id := range normalized.IDs {
		data, ok := m.rankDataLocked(id)
		if !ok {
			m.mu.Unlock()
			return MergedView{}, ErrLeaderboardNotFound
		}
		sources = append(sources, data)
	}
	generatedAt := m.now().UTC()
	m.mu.Unlock()

	records := make([]Record, 0)
	for _, data := range sources {
		records = append(records, rankRecords(data.definition, data.records)...)
	}
	return buildMergedView(normalized, records, generatedAt), nil
}

func (m *RedisManager) MergedView(ctx context.Context, options MergedViewOptions) (MergedView, error) {
	if err := ctxErr(ctx); err != nil {
		return MergedView{}, err
	}
	normalized, err := normMergedViewOpts(options)
	if err != nil {
		return MergedView{}, err
	}

	records := make([]Record, 0)
	for _, id := range normalized.IDs {
		if _, ok, err := m.definition(ctx, id); err != nil || !ok {
			if err != nil {
				return MergedView{}, err
			}
			return MergedView{}, ErrLeaderboardNotFound
		}
		ranked, err := m.rankedRecords(ctx, id)
		if err != nil {
			return MergedView{}, err
		}
		records = append(records, ranked...)
	}
	return buildMergedView(normalized, records, m.nowUTC()), nil
}

func normMergedViewOpts(options MergedViewOptions) (MergedViewOptions, error) {
	ids := make([]string, 0, len(options.IDs))
	seen := make(map[string]struct{}, len(options.IDs))
	for _, id := range options.IDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return MergedViewOptions{}, fmt.Errorf("%w: ids are required", ErrInvalidDefinition)
	}
	sortOrder, err := normalizeSortOrder(options.SortOrder)
	if err != nil {
		return MergedViewOptions{}, err
	}
	options.IDs = ids
	options.SortOrder = sortOrder
	return options, nil
}

func buildMergedView(options MergedViewOptions, records []Record, generatedAt time.Time) MergedView {
	records = cloneRecords(records)
	sort.SliceStable(records, func(i, j int) bool {
		return compareRecords(records[i], records[j], options.SortOrder) < 0
	})
	if options.DedupeOwner {
		records = dedupeMergedRecords(records)
	}
	for i := range records {
		records[i].Rank = i + 1
	}

	total := len(records)
	offset, limit := normalizeListOptions(ListOptions{Offset: options.Offset, Limit: options.Limit})
	if offset >= len(records) {
		records = []Record{}
	} else {
		end := offset + limit
		if end > len(records) {
			end = len(records)
		}
		records = cloneRecords(records[offset:end])
	}
	return MergedView{
		IDs:         append([]string(nil), options.IDs...),
		SortOrder:   options.SortOrder,
		Records:     records,
		RecordCount: total,
		GeneratedAt: generatedAt.UTC(),
	}
}

func dedupeMergedRecords(records []Record) []Record {
	seen := make(map[string]struct{}, len(records))
	out := make([]Record, 0, len(records))
	for _, record := range records {
		if _, ok := seen[record.OwnerID]; ok {
			continue
		}
		seen[record.OwnerID] = struct{}{}
		out = append(out, record)
	}
	return out
}
