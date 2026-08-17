// This file loads the job-scoped skill catalog from an in-memory PVF index.
package skill

import (
	"context"
	"fmt"
	"math"
	"path"
	"strings"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

func Load(ctx context.Context, index *dnfpvf.Index, options Options) (*Table, error) {
	if index == nil {
		return nil, ErrIndexRequired
	}
	if ctx == nil {
		ctx = context.Background()
	}
	listPath := options.ListPath
	if listPath == "" {
		listPath = DefaultList
	}
	entries := index.List(listPath)
	if len(entries) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrListEmpty, listPath)
	}

	table := &Table{
		skills: make([]Skill, 0),
		byKey:  make(map[Key]int),
		byPath: make(map[jobPathKey]int),
		byJob:  make(map[byte][]int),
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if strings.EqualFold(path.Ext(entry.Path), ".lst") {
			job, err := skillJob(entry.ID)
			if err != nil {
				return nil, err
			}
			jobEntries := index.List(entry.Path)
			if len(jobEntries) == 0 {
				return nil, fmt.Errorf("%w: job=%d list=%s", ErrListEmpty, job, entry.Path)
			}
			if err := loadJob(ctx, table, index, job, jobEntries); err != nil {
				return nil, err
			}
			continue
		}
		// Explicit flat lists remain supported for focused tools and fixtures. They
		// live in job zero and never collapse IDs from multiple job lists.
		if err := loadJob(ctx, table, index, 0, []dnfpvf.ListEntry{entry}); err != nil {
			return nil, err
		}
	}
	if len(table.skills) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrListEmpty, listPath)
	}
	return table, nil
}

func loadJob(ctx context.Context, table *Table, index *dnfpvf.Index, job byte, entries []dnfpvf.ListEntry) error {
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		id, err := skillID(entry.ID)
		if err != nil {
			return fmt.Errorf("job=%d path=%s: %w", job, entry.Path, err)
		}
		key := Key{Job: job, ID: id}
		if _, exists := table.byKey[key]; exists {
			continue
		}
		doc, ok := index.Document(entry.Path)
		if !ok {
			return fmt.Errorf("%w: job=%d skill=%d path=%s", ErrDocMissing, job, id, entry.Path)
		}
		definition, err := parseSkill(job, id, entry.Path, doc)
		if err != nil {
			return fmt.Errorf("job=%d skill=%d path=%s: %w", job, id, entry.Path, err)
		}
		idx := len(table.skills)
		table.skills = append(table.skills, definition)
		table.byKey[key] = idx
		table.byJob[job] = append(table.byJob[job], idx)
		pathKey := jobPathKey{Job: job, Path: pathKey(definition.Path)}
		if pathKey.Path != "" {
			if _, exists := table.byPath[pathKey]; !exists {
				table.byPath[pathKey] = idx
			}
		}
	}
	return nil
}

func skillJob(value int64) (byte, error) {
	if value < 0 || value > math.MaxUint8 {
		return 0, fmt.Errorf("%w: %d", ErrJobOutOfRange, value)
	}
	return byte(value), nil
}

func skillID(value int64) (uint16, error) {
	if value < 0 || value > math.MaxUint16 {
		return 0, fmt.Errorf("%w: %d", ErrIDOutOfRange, value)
	}
	return uint16(value), nil
}
