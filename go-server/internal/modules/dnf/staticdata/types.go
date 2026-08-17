// 本文件定义 DNF 静态数据总装配后的只读查询入口。
package staticdata

import (
	"errors"

	"longheng.io/server/internal/modules/dnf/drop"
	"longheng.io/server/internal/modules/dnf/dungeon"
	"longheng.io/server/internal/modules/dnf/equip"
	"longheng.io/server/internal/modules/dnf/monster"
	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	"longheng.io/server/internal/modules/dnf/reward"
	"longheng.io/server/internal/modules/dnf/skill"
)

var ErrSourceRequired = errors.New("dnf staticdata source is required")

type Options struct {
	Build   dnfpvf.BuildOptions
	Equip   equip.Options
	Skill   skill.Options
	Monster monster.Options
	Dungeon dungeon.Options
	Drop    drop.Options
	Reward  reward.Options
}

type Store struct {
	Index   *dnfpvf.Index
	Equip   *equip.Table
	Skill   *skill.Table
	Monster *monster.Table
	Dungeon *dungeon.Table
	Drop    *drop.Table
	Reward  *reward.Table
}

type Snapshot struct {
	Index   dnfpvf.Snapshot  `json:"index"`
	Equip   equip.Snapshot   `json:"equip"`
	Skill   skill.Snapshot   `json:"skill"`
	Monster monster.Snapshot `json:"monster"`
	Dungeon dungeon.Snapshot `json:"dungeon"`
	Drop    drop.Snapshot    `json:"drop"`
	Reward  reward.Snapshot  `json:"reward"`
}

// Snapshot 返回 DNF 静态数据总表规模，用于启动日志、debug 面板和接入验收。
func (s *Store) Snapshot() Snapshot {
	if s == nil {
		return Snapshot{}
	}
	return Snapshot{
		Index:   s.Index.Snapshot(),
		Equip:   s.Equip.Snapshot(),
		Skill:   s.Skill.Snapshot(),
		Monster: s.Monster.Snapshot(),
		Dungeon: s.Dungeon.Snapshot(),
		Drop:    s.Drop.Snapshot(),
		Reward:  s.Reward.Snapshot(),
	}
}
