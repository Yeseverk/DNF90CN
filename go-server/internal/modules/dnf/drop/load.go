// 本文件负责从 DNF PVF 内存索引加载掉落强类型表。
package drop

import (
	"context"
	"fmt"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

// Load 从已构建的 DNF PVF 内存索引加载掉落表。
// 这里不做随机、不结算、不发奖，只把 PVF 静态文本转成可查询的只读表。
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
		entries: make([]Entry, 0, len(entries)),
		byID:    make(map[int64]int, len(entries)),
		byPath:  make(map[string]int, len(entries)),
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		doc, ok := index.Document(entry.Path)
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrDocMissing, entry.Path)
		}
		dropEntry := parse(entry.ID, entry.Path, doc)
		idx := len(table.entries)
		table.entries = append(table.entries, dropEntry)
		if _, exists := table.byID[dropEntry.ID]; !exists {
			table.byID[dropEntry.ID] = idx
		}
		key := pathKey(dropEntry.Path)
		if key != "" {
			table.byPath[key] = idx
		}
	}
	return table, nil
}
