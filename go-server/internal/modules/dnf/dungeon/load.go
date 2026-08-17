// 本文件负责从 DNF PVF 内存索引加载地下城强类型表。
package dungeon

import (
	"context"
	"fmt"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

// Load 从已构建的 DNF PVF 内存索引加载地下城表。
// 这里只解析副本静态数据，不创建房间、不扣疲劳，也不发放结算奖励。
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
		dungeons: make([]Dungeon, 0, len(entries)),
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
		dungeon := parse(entry.ID, entry.Path, doc)
		idx := len(table.dungeons)
		table.dungeons = append(table.dungeons, dungeon)
		if _, exists := table.byID[dungeon.ID]; !exists {
			table.byID[dungeon.ID] = idx
		}
		key := pathKey(dungeon.Path)
		if key != "" {
			table.byPath[key] = idx
		}
	}
	return table, nil
}
