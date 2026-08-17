// 本文件负责从内存 PVF source 构建 DNF 静态数据总表。
package staticdata

import (
	"context"
	"fmt"

	"longheng.io/server/internal/modules/dnf/drop"
	"longheng.io/server/internal/modules/dnf/dungeon"
	"longheng.io/server/internal/modules/dnf/equip"
	"longheng.io/server/internal/modules/dnf/monster"
	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	"longheng.io/server/internal/modules/dnf/reward"
	"longheng.io/server/internal/modules/dnf/skill"
)

// Load 从已加载进内存的 PVF source 构建 DNF 静态数据 Store。
// 这里只装配只读索引和强类型表，不发奖、不扣资产、不写 Profile/MySQL/Redis/EventLog/Outbox。
func Load(ctx context.Context, source dnfpvf.Source, options Options) (*Store, error) {
	if source == nil {
		return nil, ErrSourceRequired
	}
	if ctx == nil {
		ctx = context.Background()
	}
	options = normalizeOptions(options)
	index, err := dnfpvf.Build(ctx, source, options.Build)
	if err != nil {
		return nil, fmt.Errorf("build dnf staticdata index: %w", err)
	}
	equipTable, err := equip.Load(ctx, index, options.Equip)
	if err != nil {
		return nil, fmt.Errorf("load dnf staticdata equip: %w", err)
	}
	skillTable, err := skill.Load(ctx, index, options.Skill)
	if err != nil {
		return nil, fmt.Errorf("load dnf staticdata skill: %w", err)
	}
	monsterTable, err := monster.Load(ctx, index, options.Monster)
	if err != nil {
		return nil, fmt.Errorf("load dnf staticdata monster: %w", err)
	}
	dungeonTable, err := dungeon.Load(ctx, index, options.Dungeon)
	if err != nil {
		return nil, fmt.Errorf("load dnf staticdata dungeon: %w", err)
	}
	dropTable, err := drop.Load(ctx, index, options.Drop)
	if err != nil {
		return nil, fmt.Errorf("load dnf staticdata drop: %w", err)
	}
	rewardTable, err := reward.Load(ctx, index, options.Reward)
	if err != nil {
		return nil, fmt.Errorf("load dnf staticdata reward: %w", err)
	}
	return &Store{
		Index:   index,
		Equip:   equipTable,
		Skill:   skillTable,
		Monster: monsterTable,
		Dungeon: dungeonTable,
		Drop:    dropTable,
		Reward:  rewardTable,
	}, nil
}
