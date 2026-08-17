// 本文件定义 DNF 装备表的内存视图和查询接口。
package equip

import "errors"

const DefaultList = "equipment/equipment.lst"

var (
	ErrIndexRequired = errors.New("dnf equip index is required")
	ErrListEmpty     = errors.New("dnf equip list is empty")
	ErrDocMissing    = errors.New("dnf equip document is missing")
)

type Options struct {
	ListPath string
}

type Item struct {
	ID     int64              `json:"id"`
	Path   string             `json:"path"`
	Name   string             `json:"name"`
	Kind   string             `json:"kind,omitempty"`
	Slot   string             `json:"slot,omitempty"`
	Rarity string             `json:"rarity,omitempty"`
	Level  int64              `json:"level,omitempty"`
	Icon   string             `json:"icon,omitempty"`
	Stats  map[string]float64 `json:"stats,omitempty"`
}

type Table struct {
	items  []Item
	byID   map[int64]int
	byPath map[string]int
}

type Snapshot struct {
	Items int `json:"items"`
}

// Items 返回装备表副本，避免调用方改坏内存索引。
func (t *Table) Items() []Item {
	if t == nil || len(t.items) == 0 {
		return nil
	}
	out := make([]Item, len(t.items))
	for idx, item := range t.items {
		out[idx] = cloneItem(item)
	}
	return out
}

// Find 按 `.lst` 中的装备 id 查询内存表。
func (t *Table) Find(id int64) (Item, bool) {
	if t == nil {
		return Item{}, false
	}
	idx, ok := t.byID[id]
	if !ok {
		return Item{}, false
	}
	return cloneItem(t.items[idx]), true
}

// FindPath 按 PVF 相对路径查询内存表。
func (t *Table) FindPath(itemPath string) (Item, bool) {
	if t == nil {
		return Item{}, false
	}
	idx, ok := t.byPath[pathKey(itemPath)]
	if !ok {
		return Item{}, false
	}
	return cloneItem(t.items[idx]), true
}

// Snapshot 返回装备表当前规模，用于启动日志和 debug 面板。
func (t *Table) Snapshot() Snapshot {
	if t == nil {
		return Snapshot{}
	}
	return Snapshot{Items: len(t.items)}
}

// cloneItem 复制 map，保证加载后的表只读。
func cloneItem(item Item) Item {
	if len(item.Stats) == 0 {
		return item
	}
	stats := make(map[string]float64, len(item.Stats))
	for key, value := range item.Stats {
		stats[key] = value
	}
	item.Stats = stats
	return item
}
