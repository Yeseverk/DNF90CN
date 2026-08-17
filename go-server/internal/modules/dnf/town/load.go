package town

import (
	"context"
	"fmt"
	"path"
	"strings"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

func Load(ctx context.Context, source Source, options Options) (*Table, error) {
	if source == nil {
		return nil, ErrSourceRequired
	}
	if ctx == nil {
		ctx = context.Background()
	}
	listPath := strings.TrimSpace(options.ListPath)
	if listPath == "" {
		listPath = DefaultList
	}
	listText, err := source.ReadText(listPath)
	if err != nil {
		return nil, err
	}
	listDoc, err := dnfpvf.Parse(listPath, listText)
	if err != nil {
		return nil, err
	}
	entries := dnfpvf.ParseList(listDoc)
	if len(entries) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrListEmpty, listPath)
	}

	table := &Table{towns: make(map[int64]Town, len(entries))}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		documentPath := townDocumentPath(listPath, entry.Path)
		text, err := source.ReadText(documentPath)
		if err != nil {
			return nil, fmt.Errorf("read town %d %s: %w", entry.ID, documentPath, err)
		}
		doc, err := dnfpvf.Parse(documentPath, text)
		if err != nil {
			return nil, fmt.Errorf("parse town %d %s: %w", entry.ID, documentPath, err)
		}
		value, err := parseTown(entry.ID, documentPath, doc)
		if err != nil {
			return nil, err
		}
		if _, exists := table.towns[value.ID]; !exists {
			table.towns[value.ID] = value
		}
	}
	return table, nil
}

func townDocumentPath(listPath, referenced string) string {
	referenced = strings.TrimSpace(strings.ReplaceAll(referenced, "\\", "/"))
	if strings.Contains(referenced, "/") {
		return path.Clean(referenced)
	}
	return path.Join(path.Dir(listPath), referenced)
}
