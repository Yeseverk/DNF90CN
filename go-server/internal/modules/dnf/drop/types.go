// 本文件定义 DNF 掉落表的内存视图和查询接口。
package drop

import "errors"

const DefaultList = "drop/drop.lst"

var (
	ErrIndexRequired = errors.New("dnf drop index is required")
	ErrListEmpty     = errors.New("dnf drop list is empty")
	ErrDocMissing    = errors.New("dnf drop document is missing")
)

type Options struct {
	ListPath string
}

type WeightedItem struct {
	ID     int64   `json:"id,omitempty"`
	Path   string  `json:"path,omitempty"`
	Weight float64 `json:"weight,omitempty"`
}

type Entry struct {
	ID        int64              `json:"id"`
	Path      string             `json:"path"`
	Name      string             `json:"name"`
	Kind      string             `json:"kind,omitempty"`
	ItemIDs   []int64            `json:"item_ids,omitempty"`
	ItemPaths []string           `json:"item_paths,omitempty"`
	Items     []WeightedItem     `json:"items,omitempty"`
	Gold      int64              `json:"gold,omitempty"`
	MinCount  int64              `json:"min_count,omitempty"`
	MaxCount  int64              `json:"max_count,omitempty"`
	Scalars   map[string]float64 `json:"scalars,omitempty"`
}

type Table struct {
	entries []Entry
	byID    map[int64]int
	byPath  map[string]int
}

type Snapshot struct {
	Entries int `json:"entries"`
}

// Entries 返回掉落表副本，避免调用方改坏内存索引。
func (t *Table) Entries() []Entry {
	if t == nil || len(t.entries) == 0 {
		return nil
	}
	out := make([]Entry, len(t.entries))
	for idx, entry := range t.entries {
		out[idx] = cloneEntry(entry)
	}
	return out
}

// Find 按 `.lst` 中的掉落 id 查询内存表。
func (t *Table) Find(id int64) (Entry, bool) {
	if t == nil {
		return Entry{}, false
	}
	idx, ok := t.byID[id]
	if !ok {
		return Entry{}, false
	}
	return cloneEntry(t.entries[idx]), true
}

// FindPath 按 PVF 相对路径查询内存表。
func (t *Table) FindPath(dropPath string) (Entry, bool) {
	if t == nil {
		return Entry{}, false
	}
	idx, ok := t.byPath[pathKey(dropPath)]
	if !ok {
		return Entry{}, false
	}
	return cloneEntry(t.entries[idx]), true
}

// Snapshot 返回掉落表当前规模，用于启动日志和 debug 面板。
func (t *Table) Snapshot() Snapshot {
	if t == nil {
		return Snapshot{}
	}
	return Snapshot{Entries: len(t.entries)}
}

// cloneEntry 复制切片和 map，保证加载后的表只读。
func cloneEntry(entry Entry) Entry {
	entry.ItemIDs = append([]int64(nil), entry.ItemIDs...)
	entry.ItemPaths = append([]string(nil), entry.ItemPaths...)
	entry.Items = append([]WeightedItem(nil), entry.Items...)
	if len(entry.Scalars) > 0 {
		scalars := make(map[string]float64, len(entry.Scalars))
		for key, value := range entry.Scalars {
			scalars[key] = value
		}
		entry.Scalars = scalars
	}
	return entry
}
