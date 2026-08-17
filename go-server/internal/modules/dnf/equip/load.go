// 本文件负责从 DNF PVF 内存索引加载装备强类型表。
package equip

import (
	"context"
	"fmt"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

// Load 从已构建的 DNF PVF 内存索引加载装备表。
// 这里只解析静态装备字段，不校验穿戴条件，也不修改角色背包或装备状态。
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
		items:  make([]Item, 0, len(entries)),
		byID:   make(map[int64]int, len(entries)),
		byPath: make(map[string]int, len(entries)),
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		doc, ok := index.Document(entry.Path)
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrDocMissing, entry.Path)
		}
		item := parseItem(entry.ID, entry.Path, doc)
		idx := len(table.items)
		table.items = append(table.items, item)
		if _, exists := table.byID[item.ID]; !exists {
			table.byID[item.ID] = idx
		}
		key := pathKey(item.Path)
		if key != "" {
			table.byPath[key] = idx
		}
	}
	return table, nil
}
