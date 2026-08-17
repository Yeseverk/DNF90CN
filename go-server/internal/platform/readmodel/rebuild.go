package readmodel

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type RebuildOptions struct {
	AfterID    string `json:"after_id,omitempty"`
	BatchSize  int    `json:"batch_size,omitempty"`
	MaxRecords int    `json:"max_records,omitempty"`
}

type RebuildResult struct {
	Scanned       int    `json:"scanned"`
	Rebuilt       int    `json:"rebuilt"`
	ActiveRebuilt int    `json:"active_rebuilt,omitempty"`
	LastID        string `json:"last_id,omitempty"`
	Complete      bool   `json:"complete"`
}

type StreamRebuildResult struct {
	Scanned       int    `json:"scanned"`
	Rebuilt       int    `json:"rebuilt"`
	ActiveRebuilt int    `json:"active_rebuilt,omitempty"`
	LastID        string `json:"last_id,omitempty"`
	Complete      bool   `json:"complete"`
	Pages         int    `json:"pages"`
}

type RebuildRunner[T any, V any] struct {
	Scan           func(context.Context, string, int) ([]T, error)
	Flush          func(context.Context) error
	ActiveSnapshot func() []T
	RecordID       func(T) string
	Build          func(T) V
	Save           func(context.Context, V) error
}

type RebuildProgressFunc func(RebuildResult) error

func Rebuild[T any, V any](ctx context.Context, options RebuildOptions, runner RebuildRunner[T, V]) (RebuildResult, error) {
	if err := runner.validate(); err != nil {
		return RebuildResult{}, err
	}
	ctx = contextOrBackground(ctx)
	options = normRebuildOpts(options)

	after := strings.TrimSpace(options.AfterID)
	result := RebuildResult{}
	activeAfterID := after
	rebuiltActive := make(map[string]struct{})
	if runner.Flush != nil {
		if err := runner.Flush(ctx); err != nil {
			return result, fmt.Errorf("flush source before read model rebuild: %w", err)
		}
	}
	activeRecords, activeByRecordID := snapshotActive(runner)

	for result.Scanned < options.MaxRecords {
		limit := options.BatchSize
		if remaining := options.MaxRecords - result.Scanned; remaining < limit {
			limit = remaining
		}
		records, err := runner.Scan(ctx, after, limit)
		if err != nil {
			return result, err
		}
		if len(records) == 0 {
			result.Complete = true
			return result, rebuildActive(ctx, runner, &result, activeRecords, activeAfterID, "", rebuiltActive)
		}
		for _, record := range records {
			if err := ctx.Err(); err != nil {
				return result, err
			}
			recordID := strings.TrimSpace(runner.RecordID(record))
			if recordID == "" {
				return result, errors.New("read model rebuild source returned blank id")
			}
			result.Scanned++
			result.LastID = recordID
			after = recordID
			summaryRecord := record
			if active, ok := activeByRecordID[recordID]; ok {
				summaryRecord = active
				result.ActiveRebuilt++
				rebuiltActive[recordID] = struct{}{}
			}
			if err := runner.Save(ctx, runner.Build(summaryRecord)); err != nil {
				return result, err
			}
			result.Rebuilt++
		}
		if len(records) < limit {
			result.Complete = true
			return result, rebuildActive(ctx, runner, &result, activeRecords, activeAfterID, "", rebuiltActive)
		}
	}
	return result, rebuildActive(ctx, runner, &result, activeRecords, activeAfterID, result.LastID, rebuiltActive)
}

func StreamRebuild[T any, V any](ctx context.Context, options RebuildOptions, runner RebuildRunner[T, V], progress RebuildProgressFunc) (StreamRebuildResult, error) {
	if err := runner.validate(); err != nil {
		return StreamRebuildResult{}, err
	}
	ctx = contextOrBackground(ctx)
	options = normStreamOpts(options)

	after := strings.TrimSpace(options.AfterID)
	remaining := options.MaxRecords
	streamRunner := runner
	if runner.ActiveSnapshot != nil {
		activeSnapshot := runner.ActiveSnapshot()
		streamRunner.ActiveSnapshot = func() []T {
			return append([]T(nil), activeSnapshot...)
		}
	}
	var result StreamRebuildResult
	for {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		pageLimit := options.BatchSize
		if remaining > 0 && remaining < pageLimit {
			pageLimit = remaining
		}
		page, err := Rebuild(ctx, RebuildOptions{
			AfterID:    after,
			BatchSize:  options.BatchSize,
			MaxRecords: pageLimit,
		}, streamRunner)
		if err != nil {
			return result, err
		}
		if page.Scanned == 0 && page.Rebuilt == 0 && page.Complete {
			result.Complete = true
			return result, nil
		}
		result.Pages++
		result.Scanned += page.Scanned
		result.Rebuilt += page.Rebuilt
		result.ActiveRebuilt += page.ActiveRebuilt
		if page.LastID != "" {
			result.LastID = page.LastID
			after = page.LastID
		}
		result.Complete = page.Complete
		if progress != nil {
			if err := progress(page); err != nil {
				return result, err
			}
		}
		if page.Complete {
			return result, nil
		}
		if page.Scanned == 0 || page.LastID == "" {
			return result, errors.New("read model stream rebuild made no cursor progress")
		}
		if remaining > 0 {
			remaining -= page.Scanned
			if remaining <= 0 {
				result.Complete = false
				return result, nil
			}
		}
		streamRunner.Flush = nil
	}
}

func (r RebuildRunner[T, V]) validate() error {
	if r.Scan == nil {
		return errors.New("read model rebuild scan function is required")
	}
	if r.RecordID == nil {
		return errors.New("read model rebuild record id function is required")
	}
	if r.Build == nil {
		return errors.New("read model rebuild build function is required")
	}
	if r.Save == nil {
		return errors.New("read model rebuild save function is required")
	}
	return nil
}

func normRebuildOpts(options RebuildOptions) RebuildOptions {
	options.AfterID = strings.TrimSpace(options.AfterID)
	if options.BatchSize <= 0 {
		options.BatchSize = 100
	}
	if options.BatchSize > 1000 {
		options.BatchSize = 1000
	}
	if options.MaxRecords <= 0 {
		options.MaxRecords = options.BatchSize
	}
	return options
}

func normStreamOpts(options RebuildOptions) RebuildOptions {
	options.AfterID = strings.TrimSpace(options.AfterID)
	if options.BatchSize <= 0 {
		options.BatchSize = 100
	}
	if options.BatchSize > 1000 {
		options.BatchSize = 1000
	}
	if options.MaxRecords < 0 {
		options.MaxRecords = 0
	}
	return options
}

func rebuildActive[T any, V any](ctx context.Context, runner RebuildRunner[T, V], result *RebuildResult, records []T, afterID, throughID string, skip map[string]struct{}) error {
	if len(records) == 0 {
		return nil
	}
	for _, record := range records {
		if err := ctx.Err(); err != nil {
			return err
		}
		recordID := strings.TrimSpace(runner.RecordID(record))
		if recordID == "" {
			continue
		}
		if _, ok := skip[recordID]; ok {
			continue
		}
		if afterID != "" && recordID <= afterID {
			continue
		}
		if throughID != "" && recordID > throughID {
			continue
		}
		if err := runner.Save(ctx, runner.Build(record)); err != nil {
			return err
		}
		result.Rebuilt++
		result.ActiveRebuilt++
	}
	return nil
}

func snapshotActive[T any, V any](runner RebuildRunner[T, V]) ([]T, map[string]T) {
	byID := make(map[string]T)
	if runner.ActiveSnapshot == nil {
		return nil, byID
	}
	records := runner.ActiveSnapshot()
	for _, record := range records {
		recordID := strings.TrimSpace(runner.RecordID(record))
		if recordID == "" {
			continue
		}
		byID[recordID] = record
	}
	return records, byID
}
