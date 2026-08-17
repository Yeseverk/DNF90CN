package worldmap

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

var (
	ErrIndexRequired = errors.New("dnf worldmap index is required")
	ErrListEmpty     = errors.New("dnf worldmap list is empty")
	ErrDocMissing    = errors.New("dnf worldmap document is missing")
)

type Table struct {
	maps          []Map
	areas         []Area
	dungeons      []Dungeon
	mapByID       map[int64]int
	mapByPath     map[string]int
	areaByID      map[int64]int
	areaByPath    map[string]int
	areaByDungeon map[int64]int
	dungeonByID   map[int64]int
	dungeonByPath map[string]int
}

func Load(ctx context.Context, index *dnfpvf.Index, options Options) (*Table, error) {
	if index == nil {
		return nil, ErrIndexRequired
	}
	if ctx == nil {
		ctx = context.Background()
	}
	table := &Table{
		mapByID: make(map[int64]int), mapByPath: make(map[string]int),
		areaByID: make(map[int64]int), areaByPath: make(map[string]int), areaByDungeon: make(map[int64]int),
		dungeonByID: make(map[int64]int), dungeonByPath: make(map[string]int),
	}
	if !options.SkipMaps {
		listPath := options.MapListPath
		if listPath == "" {
			listPath = DefaultMapList
		}
		entries := index.List(listPath)
		if len(entries) == 0 {
			return nil, fmt.Errorf("%w: %s", ErrListEmpty, listPath)
		}
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			doc, ok := index.Document(entry.Path)
			if !ok {
				return nil, fmt.Errorf("%w: %s", ErrDocMissing, entry.Path)
			}
			parsed, err := parseMapWithImports(entry.ID, entry.Path, doc, func(candidate string) (string, *dnfpvf.Document, error) {
				importDoc, ok := index.Document(candidate)
				if !ok {
					return "", nil, fmt.Errorf("%w: %s", ErrDocMissing, candidate)
				}
				return candidate, importDoc, nil
			})
			if err != nil {
				return nil, fmt.Errorf("load dnf worldmap map %q: %w", entry.Path, err)
			}
			pos := len(table.maps)
			table.maps = append(table.maps, parsed)
			if _, exists := table.mapByID[parsed.ID]; !exists {
				table.mapByID[parsed.ID] = pos
			}
			table.mapByPath[pathKey(parsed.Path)] = pos
		}
	}
	if !options.SkipDungeons {
		listPath := options.DungeonListPath
		if listPath == "" {
			listPath = DefaultDungeonList
		}
		entries := index.List(listPath)
		if len(entries) == 0 {
			return nil, fmt.Errorf("%w: %s", ErrListEmpty, listPath)
		}
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			doc, ok := index.Document(entry.Path)
			if !ok {
				return nil, fmt.Errorf("%w: %s", ErrDocMissing, entry.Path)
			}
			parsed := ParseDungeon(entry.ID, entry.Path, doc)
			pos := len(table.dungeons)
			table.dungeons = append(table.dungeons, parsed)
			if _, exists := table.dungeonByID[parsed.ID]; !exists {
				table.dungeonByID[parsed.ID] = pos
			}
			table.dungeonByPath[pathKey(parsed.Path)] = pos
		}
	}
	if !options.SkipAreas {
		listPath := options.WorldMapListPath
		if listPath == "" {
			listPath = DefaultWorldMapList
		}
		entries := index.List(listPath)
		if len(entries) == 0 {
			return nil, fmt.Errorf("%w: %s", ErrListEmpty, listPath)
		}
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			doc, ok := index.Document(entry.Path)
			if !ok {
				return nil, fmt.Errorf("%w: %s", ErrDocMissing, entry.Path)
			}
			parsed := ParseArea(entry.ID, entry.Path, doc)
			pos := len(table.areas)
			table.areas = append(table.areas, parsed)
			if _, exists := table.areaByID[parsed.ID]; !exists {
				table.areaByID[parsed.ID] = pos
			}
			table.areaByPath[pathKey(parsed.Path)] = pos
			for _, dungeon := range parsed.Dungeons {
				if dungeon.DungeonID <= 0 {
					continue
				}
				if _, exists := table.areaByDungeon[dungeon.DungeonID]; !exists {
					table.areaByDungeon[dungeon.DungeonID] = pos
				}
			}
		}
	}
	return table, nil
}

func (t *Table) FindDungeon(id int64) (Dungeon, bool) {
	if t == nil {
		return Dungeon{}, false
	}
	pos, ok := t.dungeonByID[id]
	if !ok {
		return Dungeon{}, false
	}
	return cloneDungeon(t.dungeons[pos]), true
}

func (t *Table) FindDungeonPath(dungeonPath string) (Dungeon, bool) {
	if t == nil {
		return Dungeon{}, false
	}
	pos, ok := t.dungeonByPath[pathKey(dungeonPath)]
	if !ok {
		return Dungeon{}, false
	}
	return cloneDungeon(t.dungeons[pos]), true
}

func (t *Table) FindMap(id int64) (Map, bool) {
	if t == nil {
		return Map{}, false
	}
	pos, ok := t.mapByID[id]
	if !ok {
		return Map{}, false
	}
	return cloneMap(t.maps[pos]), true
}

func (t *Table) FindMapPath(mapPath string) (Map, bool) {
	if t == nil {
		return Map{}, false
	}
	pos, ok := t.mapByPath[pathKey(mapPath)]
	if !ok {
		return Map{}, false
	}
	return cloneMap(t.maps[pos]), true
}

func (t *Table) FindArea(id int64) (Area, bool) {
	if t == nil {
		return Area{}, false
	}
	pos, ok := t.areaByID[id]
	if !ok {
		return Area{}, false
	}
	return cloneArea(t.areas[pos]), true
}

func (t *Table) FindAreaPath(areaPath string) (Area, bool) {
	if t == nil {
		return Area{}, false
	}
	pos, ok := t.areaByPath[pathKey(areaPath)]
	if !ok {
		return Area{}, false
	}
	return cloneArea(t.areas[pos]), true
}

func (t *Table) FindAreaByDungeon(dungeonID int64) (Area, bool) {
	if t == nil {
		return Area{}, false
	}
	pos, ok := t.areaByDungeon[dungeonID]
	if !ok {
		return Area{}, false
	}
	return cloneArea(t.areas[pos]), true
}

func (t *Table) Maps() []Map {
	if t == nil {
		return nil
	}
	out := make([]Map, len(t.maps))
	for i := range t.maps {
		out[i] = cloneMap(t.maps[i])
	}
	return out
}

func (t *Table) Areas() []Area {
	if t == nil {
		return nil
	}
	out := make([]Area, len(t.areas))
	for i := range t.areas {
		out[i] = cloneArea(t.areas[i])
	}
	return out
}

func (t *Table) Dungeons() []Dungeon {
	if t == nil {
		return nil
	}
	out := make([]Dungeon, len(t.dungeons))
	for i := range t.dungeons {
		out[i] = cloneDungeon(t.dungeons[i])
	}
	return out
}

func (t *Table) Snapshot() Snapshot {
	if t == nil {
		return Snapshot{}
	}
	mazes := 0
	for _, dungeon := range t.dungeons {
		mazes += len(dungeon.Mazes)
	}
	return Snapshot{
		Maps: len(t.maps), Areas: len(t.areas), Dungeons: len(t.dungeons),
		Mazes: mazes, DungeonRefs: len(t.areaByDungeon),
	}
}

func pathKey(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	for strings.HasPrefix(value, "./") || strings.HasPrefix(value, "/") {
		value = strings.TrimPrefix(value, "./")
		value = strings.TrimPrefix(value, "/")
	}
	if value == "" {
		return ""
	}
	return strings.ToLower(path.Clean(value))
}
