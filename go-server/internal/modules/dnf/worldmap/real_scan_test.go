package worldmap

import (
	"context"
	"os"
	"path"
	"sort"
	"strings"
	"testing"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestRealScriptPVFUnknownSectionInventory(t *testing.T) {
	pvfPath := os.Getenv("DNF_WORLDMAP_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("DNF_WORLDMAP_REAL_PVF_SMOKE is not set")
	}
	archive, err := platformpvf.LoadArchive(platformpvf.Options{Path: pvfPath})
	if err != nil {
		t.Fatal(err)
	}
	table, err := LoadSource(context.Background(), archive, Options{})
	if err != nil {
		t.Fatal(err)
	}
	mapUnknown := make(map[string]int)
	areaUnknown := make(map[string]int)
	dungeonUnknown := make(map[string]int)
	mazeUnknown := make(map[string]int)
	for _, value := range table.Maps() {
		for _, section := range value.UnknownSections {
			key := sectionKey(section.Name)
			mapUnknown[key]++
		}
	}
	for _, value := range table.Areas() {
		for _, section := range value.UnknownSections {
			areaUnknown[sectionKey(section.Name)]++
		}
	}
	for _, value := range table.Dungeons() {
		for _, section := range value.UnknownSections {
			dungeonUnknown[sectionKey(section.Name)]++
		}
		for _, maze := range value.Mazes {
			for _, section := range maze.UnknownSections {
				mazeUnknown[sectionKey(section.Name)]++
			}
		}
	}
	t.Logf("map unknown top20: %v", topSectionCounts(mapUnknown, 20))
	t.Logf("area unknown top20: %v", topSectionCounts(areaUnknown, 20))
	t.Logf("dungeon unknown top20: %v", topSectionCounts(dungeonUnknown, 20))
	t.Logf("maze unknown top20: %v", topSectionCounts(mazeUnknown, 20))
}

func TestRealScriptPVFElvengardImportsStorageNPC(t *testing.T) {
	pvfPath := os.Getenv("DNF_WORLDMAP_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("DNF_WORLDMAP_REAL_PVF_SMOKE is not set")
	}
	archive, err := platformpvf.LoadArchive(platformpvf.Options{Path: pvfPath})
	if err != nil {
		t.Fatal(err)
	}
	mapPath := ""
	for _, file := range archive.Files() {
		if strings.EqualFold(path.Base(file.ArchivePath), "(f)new_seria_room.map") {
			mapPath = file.ArchivePath
			break
		}
	}
	if mapPath == "" {
		var candidates []string
		for _, file := range archive.Files() {
			value := strings.ToLower(file.ArchivePath + " " + file.Path + " " + file.Name)
			if path.Ext(file.ArchivePath) == ".map" && (strings.Contains(value, "seria") || strings.Contains(value, "elvengard")) {
				candidates = append(candidates, file.ArchivePath+" | "+file.Path+" | "+file.Name)
				if len(candidates) == 12 {
					break
				}
			}
		}
		t.Fatalf("(F)new_seria_room.map is absent from the runtime PVF; matching entries=%q", candidates)
	}
	text, err := archive.ReadText(mapPath)
	if err != nil {
		t.Fatalf("read %s: %v", mapPath, err)
	}
	doc, err := ParseDocument(mapPath, text)
	if err != nil {
		t.Fatalf("parse %s: %v", mapPath, err)
	}
	parsed, err := parseMapWithImports(0, mapPath, doc, func(candidate string) (string, *dnfpvf.Document, error) {
		importText, err := archive.ReadText(candidate)
		if err != nil {
			return "", nil, err
		}
		importDoc, err := ParseDocument(candidate, importText)
		if err != nil {
			return "", nil, err
		}
		return candidate, importDoc, nil
	})
	if err != nil {
		t.Fatalf("resolve imports for %s: %v", mapPath, err)
	}
	for _, spawn := range parsed.NPCs {
		if spawn.NPCID == 22 && spawn.Direction == "[right]" && spawn.Position == (Point3{X: 187, Y: 200, Z: 0}) {
			return
		}
	}
	t.Fatalf("Storage.npc (id=22 at 187,200) not found in %s imported NPCs: %+v", mapPath, parsed.NPCs)
}

type sectionCount struct {
	Name  string
	Count int
}

func sortedSectionCounts(values map[string]int) []sectionCount {
	out := make([]sectionCount, 0, len(values))
	for name, count := range values {
		out = append(out, sectionCount{Name: name, Count: count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func topSectionCounts(values map[string]int, limit int) []sectionCount {
	out := sortedSectionCounts(values)
	if limit >= 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}
