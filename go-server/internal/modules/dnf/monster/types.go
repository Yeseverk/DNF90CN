// 本文件定义 DNF 怪物表的内存视图和查询接口。
package monster

import "errors"

const DefaultList = "monster/monster.lst"

var (
	ErrIndexRequired = errors.New("dnf monster index is required")
	ErrListEmpty     = errors.New("dnf monster list is empty")
	ErrDocMissing    = errors.New("dnf monster document is missing")
)

type Options struct {
	ListPath string
}

type Monster struct {
	ID       int64              `json:"id"`
	Path     string             `json:"path"`
	Name     string             `json:"name"`
	Kind     string             `json:"kind,omitempty"`
	Rank     string             `json:"rank,omitempty"`
	Level    int64              `json:"level,omitempty"`
	HP       int64              `json:"hp,omitempty"`
	Attack   int64              `json:"attack,omitempty"`
	Defense  int64              `json:"defense,omitempty"`
	Move     float64            `json:"move,omitempty"`
	Speed    float64            `json:"speed,omitempty"`
	Exp      int64              `json:"exp,omitempty"`
	AI       string             `json:"ai,omitempty"`
	Icon     string             `json:"icon,omitempty"`
	Scalars  map[string]float64 `json:"scalars,omitempty"`
	Sections []string           `json:"sections,omitempty"`
}

type Table struct {
	monsters []Monster
	byID     map[int64]int
	byPath   map[string]int
}

type Snapshot struct {
	Monsters int `json:"monsters"`
}

// Monsters 返回怪物表副本，避免调用方改坏内存索引。
func (t *Table) Monsters() []Monster {
	if t == nil || len(t.monsters) == 0 {
		return nil
	}
	out := make([]Monster, len(t.monsters))
	for idx, monster := range t.monsters {
		out[idx] = cloneMonster(monster)
	}
	return out
}

// Find 按 `.lst` 中的怪物 id 查询内存表。
func (t *Table) Find(id int64) (Monster, bool) {
	if t == nil {
		return Monster{}, false
	}
	idx, ok := t.byID[id]
	if !ok {
		return Monster{}, false
	}
	return cloneMonster(t.monsters[idx]), true
}

// FindPath 按 PVF 相对路径查询内存表。
func (t *Table) FindPath(monsterPath string) (Monster, bool) {
	if t == nil {
		return Monster{}, false
	}
	idx, ok := t.byPath[pathKey(monsterPath)]
	if !ok {
		return Monster{}, false
	}
	return cloneMonster(t.monsters[idx]), true
}

// Snapshot 返回怪物表当前规模，用于启动日志和 debug 面板。
func (t *Table) Snapshot() Snapshot {
	if t == nil {
		return Snapshot{}
	}
	return Snapshot{Monsters: len(t.monsters)}
}

// cloneMonster 复制切片和 map，保证加载后的表只读。
func cloneMonster(monster Monster) Monster {
	if len(monster.Scalars) > 0 {
		scalars := make(map[string]float64, len(monster.Scalars))
		for key, value := range monster.Scalars {
			scalars[key] = value
		}
		monster.Scalars = scalars
	}
	monster.Sections = append([]string(nil), monster.Sections...)
	return monster
}
