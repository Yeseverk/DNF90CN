// 本文件负责从 DNF PVF 内存索引加载怪物强类型表。
package monster

import (
	"context"
	"fmt"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

// Load 从已构建的 DNF PVF 内存索引加载怪物表。
// 这里只解析怪物静态数据，不创建战斗实体，也不推进 AI tick。
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
		monsters: make([]Monster, 0, len(entries)),
		byID:     make(map[int64]int, len(entries)),
		byPath:   make(map[string]int, len(entries)),
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		doc, ok := index.Document(entry.Path)
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrDocMissing, entry.Path)
		}
		monster := parse(entry.ID, entry.Path, doc)
		idx := len(table.monsters)
		table.monsters = append(table.monsters, monster)
		if _, exists := table.byID[monster.ID]; !exists {
			table.byID[monster.ID] = idx
		}
		key := pathKey(monster.Path)
		if key != "" {
			table.byPath[key] = idx
		}
	}
	return table, nil
}
