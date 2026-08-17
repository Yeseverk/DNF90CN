package datatable

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrViewRequired = errors.New("data table view binding is required")
)

type ViewRefreshResult struct {
	Manifest Manifest  `json:"manifest"`
	Tables   []string  `json:"tables,omitempty"`
	Views    int       `json:"views"`
	Skipped  []string  `json:"skipped,omitempty"`
	At       time.Time `json:"at"`
	Error    string    `json:"error,omitempty"`
}

type ViewManagerSnapshot struct {
	Tables     []string          `json:"tables,omitempty"`
	Views      int               `json:"views"`
	LastResult ViewRefreshResult `json:"last_result"`
}

type ViewManager struct {
	mu    sync.RWMutex
	views map[string][]ViewBinding
	last  ViewRefreshResult
}

func NewViewManager(bindings ...ViewBinding) (*ViewManager, error) {
	manager := &ViewManager{
		views: make(map[string][]ViewBinding),
	}
	if err := manager.Register(bindings...); err != nil {
		return nil, err
	}
	return manager, nil
}

func (m *ViewManager) Register(bindings ...ViewBinding) error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.views == nil {
		m.views = make(map[string][]ViewBinding)
	}
	for _, binding := range bindings {
		if binding == nil {
			return ErrViewRequired
		}
		name := strings.TrimSpace(binding.TableName())
		if name == "" {
			return ErrTableRequired
		}
		m.views[name] = append(m.views[name], binding)
	}
	return nil
}

func (m *ViewManager) Refresh(ctx context.Context, source RowSource, load LoadResult) (ViewRefreshResult, error) {
	return m.refresh(ctx, source, load.Manifest, affectedTableNames(load.Diff), false)
}

func (m *ViewManager) RefreshAll(ctx context.Context, source RowSource) (ViewRefreshResult, error) {
	manifest := Manifest{}
	if source != nil {
		manifest = source.Manifest()
	}
	return m.refresh(ctx, source, manifest, nil, true)
}

func (m *ViewManager) ValidateDataTables(ctx context.Context, dataset Dataset) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if m == nil {
		return nil
	}
	bindings, _, _ := m.selectBindings(nil, true)
	for _, binding := range bindings {
		view, err := binding.Prepare(ctx, dataset)
		if err != nil {
			return fmt.Errorf("prepare data table view %q: %w", binding.TableName(), err)
		}
		if err := view.Validate(); err != nil {
			return fmt.Errorf("validate data table view %q: %w", view.Table, err)
		}
	}
	return nil
}

func (m *ViewManager) Clear() ViewRefreshResult {
	if m == nil {
		return ViewRefreshResult{}
	}
	bindings, skipped, tables := m.selectBindings(nil, true)
	result := ViewRefreshResult{
		Tables:  tables,
		Skipped: skipped,
		At:      time.Now().UTC(),
	}
	for _, binding := range bindings {
		clearer, ok := binding.(interface{ Clear() })
		if !ok {
			result.Skipped = append(result.Skipped, binding.TableName())
			continue
		}
		clearer.Clear()
		result.Views++
	}
	result.Skipped = normalizeTableNames(result.Skipped)
	m.record(result)
	return cloneViewRefresh(result)
}

func (m *ViewManager) Run(ctx context.Context, source RowSource, changes <-chan LoadResult, onError func(ViewRefreshResult, error)) <-chan struct{} {
	if ctx == nil {
		ctx = context.Background()
	}
	done := make(chan struct{})
	if changes == nil {
		close(done)
		return done
	}
	go func() {
		defer close(done)
		for {
			select {
			case <-ctx.Done():
				return
			case change, ok := <-changes:
				if !ok {
					return
				}
				result, err := m.Refresh(ctx, source, change)
				if err != nil && onError != nil {
					onError(result, err)
				}
			}
		}
	}()
	return done
}

func (m *ViewManager) Snapshot() ViewManagerSnapshot {
	if m == nil {
		return ViewManagerSnapshot{}
	}
	m.mu.RLock()
	tables := make([]string, 0, len(m.views))
	views := 0
	for table, bindings := range m.views {
		tables = append(tables, table)
		views += len(bindings)
	}
	last := cloneViewRefresh(m.last)
	m.mu.RUnlock()
	sort.Strings(tables)
	return ViewManagerSnapshot{
		Tables:     tables,
		Views:      views,
		LastResult: last,
	}
}

func (m *ViewManager) refresh(ctx context.Context, source RowSource, manifest Manifest, names []string, all bool) (ViewRefreshResult, error) {
	if err := contextErr(ctx); err != nil {
		return ViewRefreshResult{}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if m == nil {
		return ViewRefreshResult{}, nil
	}
	if source == nil {
		return ViewRefreshResult{}, ErrDataSourceRequired
	}
	names = normalizeTableNames(names)
	bindings, missing, selected := m.selectBindings(names, all)
	result := ViewRefreshResult{
		Manifest: manifest,
		Tables:   selected,
		Skipped:  missing,
		At:       time.Now().UTC(),
	}
	if len(bindings) == 0 {
		m.record(result)
		return result, nil
	}

	prepared := make([]PreparedView, 0, len(bindings))
	for _, binding := range bindings {
		view, err := binding.Prepare(ctx, source)
		if err != nil {
			return m.fail(result, fmt.Errorf("prepare data table view %q: %w", binding.TableName(), err))
		}
		prepared = append(prepared, view)
	}
	for _, view := range prepared {
		if err := view.Validate(); err != nil {
			return m.fail(result, fmt.Errorf("validate data table view %q: %w", view.Table, err))
		}
	}
	committed := make([]PreparedView, 0, len(prepared))
	for _, view := range prepared {
		if err := view.Commit(); err != nil {
			for i := len(committed) - 1; i >= 0; i-- {
				committed[i].Rollback()
			}
			return m.fail(result, fmt.Errorf("commit data table view %q: %w", view.Table, err))
		}
		committed = append(committed, view)
	}
	result.Views = len(prepared)
	m.record(result)
	return cloneViewRefresh(result), nil
}

func (m *ViewManager) selectBindings(names []string, all bool) ([]ViewBinding, []string, []string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if all {
		tables := make([]string, 0, len(m.views))
		for table := range m.views {
			tables = append(tables, table)
		}
		sort.Strings(tables)
		out := make([]ViewBinding, 0)
		for _, table := range tables {
			out = append(out, m.views[table]...)
		}
		return out, nil, tables
	}
	if len(names) == 0 {
		return nil, nil, nil
	}
	out := make([]ViewBinding, 0)
	missing := make([]string, 0)
	for _, name := range names {
		bindings := m.views[name]
		if len(bindings) == 0 {
			missing = append(missing, name)
			continue
		}
		out = append(out, bindings...)
	}
	return out, missing, append([]string(nil), names...)
}

func (m *ViewManager) record(result ViewRefreshResult) {
	m.mu.Lock()
	m.last = cloneViewRefresh(result)
	m.mu.Unlock()
}

func (m *ViewManager) fail(result ViewRefreshResult, err error) (ViewRefreshResult, error) {
	if err != nil {
		result.Error = err.Error()
	}
	m.record(result)
	return cloneViewRefresh(result), err
}

func affectedTableNames(diff Diff) []string {
	names := make([]string, 0, len(diff.Added)+len(diff.Changed)+len(diff.Removed))
	for _, change := range diff.Added {
		names = append(names, change.Name)
	}
	for _, change := range diff.Changed {
		names = append(names, change.Name)
	}
	for _, change := range diff.Removed {
		names = append(names, change.Name)
	}
	return normalizeTableNames(names)
}

func normalizeTableNames(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(names))
	out := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func cloneViewRefresh(result ViewRefreshResult) ViewRefreshResult {
	result.Tables = append([]string(nil), result.Tables...)
	result.Skipped = append([]string(nil), result.Skipped...)
	return result
}
