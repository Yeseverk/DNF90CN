package worldmap

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

var ErrSourceRequired = errors.New("dnf worldmap source is required")

func LoadSource(ctx context.Context, source dnfpvf.Source, options Options) (*Table, error) {
	if source == nil {
		return nil, ErrSourceRequired
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
		entries, err := sourceList(source, listPath)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			docPath, doc, err := sourceDocument(source, listPath, entry.Path)
			if err != nil {
				return nil, err
			}
			parsed, err := parseMapWithImports(entry.ID, docPath, doc, func(candidate string) (string, *dnfpvf.Document, error) {
				text, err := source.ReadText(candidate)
				if err != nil {
					return "", nil, err
				}
				importDoc, err := ParseDocument(candidate, text)
				if err != nil {
					return "", nil, err
				}
				return candidate, importDoc, nil
			})
			if err != nil {
				return nil, fmt.Errorf("load dnf worldmap map %q: %w", docPath, err)
			}
			addMapToTable(table, parsed)
		}
	}
	if !options.SkipDungeons {
		listPath := options.DungeonListPath
		if listPath == "" {
			listPath = DefaultDungeonList
		}
		entries, err := sourceList(source, listPath)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			docPath, doc, err := sourceDocument(source, listPath, entry.Path)
			if err != nil {
				return nil, err
			}
			addDungeonToTable(table, ParseDungeon(entry.ID, docPath, doc))
		}
	}
	if !options.SkipAreas {
		listPath := options.WorldMapListPath
		if listPath == "" {
			listPath = DefaultWorldMapList
		}
		entries, err := sourceList(source, listPath)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			docPath, doc, err := sourceDocument(source, listPath, entry.Path)
			if err != nil {
				return nil, err
			}
			addAreaToTable(table, ParseArea(entry.ID, docPath, doc))
		}
	}
	return table, nil
}

func sourceList(source dnfpvf.Source, listPath string) ([]dnfpvf.ListEntry, error) {
	text, err := source.ReadText(listPath)
	if err != nil {
		return nil, fmt.Errorf("read dnf worldmap list %q: %w", listPath, err)
	}
	doc, err := ParseDocument(listPath, text)
	if err != nil {
		return nil, err
	}
	entries := dnfpvf.ParseList(doc)
	if len(entries) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrListEmpty, listPath)
	}
	return entries, nil
}

func sourceDocument(source dnfpvf.Source, listPath, reference string) (string, *dnfpvf.Document, error) {
	docPath := resolveSourceReference(listPath, reference)
	text, err := source.ReadText(docPath)
	if err != nil {
		return "", nil, fmt.Errorf("read dnf worldmap document %q: %w", docPath, err)
	}
	doc, err := ParseDocument(docPath, text)
	if err != nil {
		return "", nil, err
	}
	return docPath, doc, nil
}

func resolveSourceReference(listPath, reference string) string {
	reference = strings.TrimSpace(strings.ReplaceAll(reference, "\\", "/"))
	reference = strings.TrimPrefix(reference, "./")
	reference = strings.TrimPrefix(reference, "/")
	listDirectory := strings.TrimSuffix(path.Dir(strings.ReplaceAll(listPath, "\\", "/")), "/")
	if listDirectory == "." || listDirectory == "" {
		return reference
	}
	if pathKey(reference) == pathKey(listDirectory) || strings.HasPrefix(pathKey(reference), pathKey(listDirectory)+"/") {
		return reference
	}
	return path.Join(listDirectory, reference)
}

func addMapToTable(table *Table, parsed Map) {
	pos := len(table.maps)
	table.maps = append(table.maps, parsed)
	if _, exists := table.mapByID[parsed.ID]; !exists {
		table.mapByID[parsed.ID] = pos
	}
	table.mapByPath[pathKey(parsed.Path)] = pos
}

func addDungeonToTable(table *Table, parsed Dungeon) {
	pos := len(table.dungeons)
	table.dungeons = append(table.dungeons, parsed)
	if _, exists := table.dungeonByID[parsed.ID]; !exists {
		table.dungeonByID[parsed.ID] = pos
	}
	table.dungeonByPath[pathKey(parsed.Path)] = pos
}

func addAreaToTable(table *Table, parsed Area) {
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
