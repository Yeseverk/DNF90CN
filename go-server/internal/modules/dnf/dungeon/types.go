// 本文件定义 DNF 地下城表的内存视图和查询接口。
package dungeon

import "errors"

const DefaultList = "dungeon/dungeon.lst"

var (
	ErrIndexRequired = errors.New("dnf dungeon index is required")
	ErrListEmpty     = errors.New("dnf dungeon list is empty")
	ErrDocMissing    = errors.New("dnf dungeon document is missing")
)

type Options struct {
	ListPath string
}

type Dungeon struct {
	ID           int64              `json:"id"`
	Path         string             `json:"path"`
	Name         string             `json:"name"`
	Area         string             `json:"area,omitempty"`
	Kind         string             `json:"kind,omitempty"`
	MinLevel     int64              `json:"min_level,omitempty"`
	MaxLevel     int64              `json:"max_level,omitempty"`
	Fatigue      int64              `json:"fatigue,omitempty"`
	PartyMin     int64              `json:"party_min,omitempty"`
	PartyMax     int64              `json:"party_max,omitempty"`
	MapPaths     []string           `json:"map_paths,omitempty"`
	MonsterIDs   []int64            `json:"monster_ids,omitempty"`
	MonsterPaths []string           `json:"monster_paths,omitempty"`
	BossIDs      []int64            `json:"boss_ids,omitempty"`
	BossPaths    []string           `json:"boss_paths,omitempty"`
	RewardPath   string             `json:"reward_path,omitempty"`
	Scalars      map[string]float64 `json:"scalars,omitempty"`
	// Abyss (深渊) seal door positioning from [seal door map index] / [seal door pos].
	SealDoorMapIndex int64 `json:"seal_door_map_index,omitempty"`
	SealDoorPosX     int64 `json:"seal_door_pos_x,omitempty"`
	SealDoorPosY     int64 `json:"seal_door_pos_y,omitempty"`
}

type Table struct {
	dungeons []Dungeon
	byID     map[int64]int
	byPath   map[string]int
}

type Snapshot struct {
	Dungeons int `json:"dungeons"`
}

// Dungeons 返回地下城表副本，避免调用方改坏内存索引。
func (t *Table) Dungeons() []Dungeon {
	if t == nil || len(t.dungeons) == 0 {
		return nil
	}
	out := make([]Dungeon, len(t.dungeons))
	for idx, dungeon := range t.dungeons {
		out[idx] = cloneDungeon(dungeon)
	}
	return out
}

// Find 按 `.lst` 中的地下城 id 查询内存表。
func (t *Table) Find(id int64) (Dungeon, bool) {
	if t == nil {
		return Dungeon{}, false
	}
	idx, ok := t.byID[id]
	if !ok {
		return Dungeon{}, false
	}
	return cloneDungeon(t.dungeons[idx]), true
}

// FindPath 按 PVF 相对路径查询内存表。
func (t *Table) FindPath(dungeonPath string) (Dungeon, bool) {
	if t == nil {
		return Dungeon{}, false
	}
	idx, ok := t.byPath[pathKey(dungeonPath)]
	if !ok {
		return Dungeon{}, false
	}
	return cloneDungeon(t.dungeons[idx]), true
}

// Snapshot 返回地下城表当前规模，用于启动日志和 debug 面板。
func (t *Table) Snapshot() Snapshot {
	if t == nil {
		return Snapshot{}
	}
	return Snapshot{Dungeons: len(t.dungeons)}
}

// cloneDungeon 复制切片和 map，保证加载后的表只读。
func cloneDungeon(dungeon Dungeon) Dungeon {
	dungeon.MapPaths = append([]string(nil), dungeon.MapPaths...)
	dungeon.MonsterIDs = append([]int64(nil), dungeon.MonsterIDs...)
	dungeon.MonsterPaths = append([]string(nil), dungeon.MonsterPaths...)
	dungeon.BossIDs = append([]int64(nil), dungeon.BossIDs...)
	dungeon.BossPaths = append([]string(nil), dungeon.BossPaths...)
	if len(dungeon.Scalars) > 0 {
		scalars := make(map[string]float64, len(dungeon.Scalars))
		for key, value := range dungeon.Scalars {
			scalars[key] = value
		}
		dungeon.Scalars = scalars
	}
	return dungeon
}
