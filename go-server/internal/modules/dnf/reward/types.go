// 本文件定义 DNF 奖励表的内存视图和查询接口。
package reward

import "errors"

const DefaultList = "reward/reward.lst"

var (
	ErrIndexRequired = errors.New("dnf reward index is required")
	ErrListEmpty     = errors.New("dnf reward list is empty")
	ErrDocMissing    = errors.New("dnf reward document is missing")
)

type Options struct {
	ListPath string
}

type Reward struct {
	ID        int64              `json:"id"`
	Path      string             `json:"path"`
	Name      string             `json:"name"`
	Kind      string             `json:"kind,omitempty"`
	DropIDs   []int64            `json:"drop_ids,omitempty"`
	DropPaths []string           `json:"drop_paths,omitempty"`
	ItemIDs   []int64            `json:"item_ids,omitempty"`
	ItemPaths []string           `json:"item_paths,omitempty"`
	Gold      int64              `json:"gold,omitempty"`
	Exp       int64              `json:"exp,omitempty"`
	Scalars   map[string]float64 `json:"scalars,omitempty"`
}

type Table struct {
	rewards []Reward
	byID    map[int64]int
	byPath  map[string]int
}

type Snapshot struct {
	Rewards int `json:"rewards"`
}

// Rewards 返回奖励表副本，避免调用方改坏内存索引。
func (t *Table) Rewards() []Reward {
	if t == nil || len(t.rewards) == 0 {
		return nil
	}
	out := make([]Reward, len(t.rewards))
	for idx, reward := range t.rewards {
		out[idx] = cloneReward(reward)
	}
	return out
}

// Find 按 `.lst` 中的奖励 id 查询内存表。
func (t *Table) Find(id int64) (Reward, bool) {
	if t == nil {
		return Reward{}, false
	}
	idx, ok := t.byID[id]
	if !ok {
		return Reward{}, false
	}
	return cloneReward(t.rewards[idx]), true
}

// FindPath 按 PVF 相对路径查询内存表。
func (t *Table) FindPath(rewardPath string) (Reward, bool) {
	if t == nil {
		return Reward{}, false
	}
	idx, ok := t.byPath[pathKey(rewardPath)]
	if !ok {
		return Reward{}, false
	}
	return cloneReward(t.rewards[idx]), true
}

// Snapshot 返回奖励表当前规模，用于启动日志和 debug 面板。
func (t *Table) Snapshot() Snapshot {
	if t == nil {
		return Snapshot{}
	}
	return Snapshot{Rewards: len(t.rewards)}
}

// cloneReward 复制切片和 map，保证加载后的表只读。
func cloneReward(reward Reward) Reward {
	reward.DropIDs = append([]int64(nil), reward.DropIDs...)
	reward.DropPaths = append([]string(nil), reward.DropPaths...)
	reward.ItemIDs = append([]int64(nil), reward.ItemIDs...)
	reward.ItemPaths = append([]string(nil), reward.ItemPaths...)
	if len(reward.Scalars) > 0 {
		scalars := make(map[string]float64, len(reward.Scalars))
		for key, value := range reward.Scalars {
			scalars[key] = value
		}
		reward.Scalars = scalars
	}
	return reward
}
