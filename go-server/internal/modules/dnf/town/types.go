package town

import (
	"errors"
	"sort"
)

const DefaultList = "town/town.lst"

var (
	ErrSourceRequired = errors.New("dnf town pvf source is required")
	ErrListEmpty      = errors.New("dnf town list is empty")
	ErrDocumentEmpty  = errors.New("dnf town document is empty")
)

type Source interface {
	ReadText(string) (string, error)
}

type Options struct {
	ListPath string
}

type Gate struct {
	X int64 `json:"x"`
	Y int64 `json:"y"`
}

type Area struct {
	ID          int64   `json:"id"`
	MapPath     string  `json:"map_path"`
	Kind        string  `json:"kind,omitempty"`
	Gate        *Gate   `json:"gate,omitempty"`
	DungeonGate *int64  `json:"dungeon_gate,omitempty"`
	MinLevel    int64   `json:"min_level,omitempty"`
	NeedQuests  []int64 `json:"need_quests,omitempty"`
}

type Town struct {
	ID    int64  `json:"id"`
	Path  string `json:"path"`
	Name  string `json:"name"`
	Areas []Area `json:"areas"`
}

type Table struct {
	towns map[int64]Town
}

type Snapshot struct {
	Towns int `json:"towns"`
	Areas int `json:"areas"`
}

func (t *Table) Find(townID int64) (Town, bool) {
	if t == nil {
		return Town{}, false
	}
	value, ok := t.towns[townID]
	return cloneTown(value), ok
}

// Towns returns a deterministic cloned snapshot of every loaded PVF town.
func (t *Table) Towns() []Town {
	if t == nil || len(t.towns) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(t.towns))
	for id := range t.towns {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	out := make([]Town, 0, len(ids))
	for _, id := range ids {
		out = append(out, cloneTown(t.towns[id]))
	}
	return out
}

func (t *Table) FindArea(townID, areaID int64) (Area, bool) {
	town, ok := t.Find(townID)
	if !ok {
		return Area{}, false
	}
	for _, area := range town.Areas {
		if area.ID == areaID {
			return cloneArea(area), true
		}
	}
	return Area{}, false
}

// FindGateArea returns the first gate area declared by the requested town.
// Gate coordinates and area ownership come from the runtime PVF catalog.
func (t *Table) FindGateArea(townID int64) (Area, bool) {
	town, ok := t.Find(townID)
	if !ok {
		return Area{}, false
	}
	for _, area := range town.Areas {
		if area.Kind == "gate" && area.Gate != nil {
			return cloneArea(area), true
		}
	}
	return Area{}, false
}

func (t *Table) Snapshot() Snapshot {
	if t == nil {
		return Snapshot{}
	}
	snapshot := Snapshot{Towns: len(t.towns)}
	for _, town := range t.towns {
		snapshot.Areas += len(town.Areas)
	}
	return snapshot
}

func cloneTown(value Town) Town {
	value.Areas = append([]Area(nil), value.Areas...)
	for idx := range value.Areas {
		value.Areas[idx] = cloneArea(value.Areas[idx])
	}
	return value
}

func cloneArea(value Area) Area {
	value.NeedQuests = append([]int64(nil), value.NeedQuests...)
	if value.Gate != nil {
		gate := *value.Gate
		value.Gate = &gate
	}
	if value.DungeonGate != nil {
		dungeonGate := *value.DungeonGate
		value.DungeonGate = &dungeonGate
	}
	return value
}
