// 本文件负责从 DNF PVF 内存索引加载奖励强类型表。
package reward

import (
	"context"
	"fmt"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

// Load 从已构建的 DNF PVF 内存索引加载奖励表。
// 这里不扣疲劳、不发金币、不写背包，只准备结算 owner 后续要查的静态数据。
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
		rewards: make([]Reward, 0, len(entries)),
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
		reward := parse(entry.ID, entry.Path, doc)
		idx := len(table.rewards)
		table.rewards = append(table.rewards, reward)
		if _, exists := table.byID[reward.ID]; !exists {
			table.byID[reward.ID] = idx
		}
		key := pathKey(reward.Path)
		if key != "" {
			table.byPath[key] = idx
		}
	}
	return table, nil
}
