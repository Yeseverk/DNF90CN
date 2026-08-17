package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"
	"sync"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

type result struct {
	Kind          string `json:"kind"`
	ItemID        int64  `json:"item_id"`
	Name          string `json:"name,omitempty"`
	Name2         string `json:"name2,omitempty"`
	Path          string `json:"path"`
	Rarity        int64  `json:"rarity,omitempty"`
	Grade         int64  `json:"grade,omitempty"`
	MinimumLevel  int64  `json:"minimum_level,omitempty"`
	Durability    int64  `json:"durability,omitempty"`
	EquipmentType string `json:"equipment_type,omitempty"`
	StackableType string `json:"stackable_type,omitempty"`
}

type sourceEntry struct {
	kind   string
	itemID int64
	path   string
}

func main() {
	pvfPath := flag.String("pvf", `D:/DNF/runtime/data/dnf/Script.pvf`, "Script.pvf path")
	readPath := flag.String("read", "", "print one exact archive text path and exit")
	allMedals := flag.Bool("all-medals", false, "include every [flag] equipment")
	allGuardianGems := flag.Bool("all-guardian-gems", false, "include every [flag gem] stackable")
	equipmentOnly := flag.Bool("equipment-only", false, "search names only in equipment")
	npcOnly := flag.Bool("npc-only", false, "search names only in npc/npc.lst definitions")
	pathOnly := flag.Bool("path-only", false, "search every archive path and print matches without reading documents")
	scanPrefix := flag.String("scan-prefix", "", "search decoded text under one archive path prefix and print matching paths")
	flag.Parse()

	archive, err := platformpvf.Open(*pvfPath)
	if err != nil {
		panic(err)
	}
	if strings.TrimSpace(*readPath) != "" {
		value, readErr := archive.ReadText(*readPath)
		if readErr != nil {
			panic(readErr)
		}
		fmt.Print(value)
		return
	}
	queries := make([]string, 0, flag.NArg())
	for _, query := range flag.Args() {
		if query = strings.ToLower(strings.TrimSpace(query)); query != "" {
			queries = append(queries, query)
		}
	}
	if *pathOnly {
		for _, file := range archive.Files() {
			lowerPath := strings.ToLower(file.ArchivePath)
			for _, query := range queries {
				if strings.Contains(lowerPath, query) {
					fmt.Println(file.ArchivePath)
					break
				}
			}
		}
		return
	}
	if prefix := strings.ToLower(strings.TrimSpace(*scanPrefix)); prefix != "" {
		for _, file := range archive.Files() {
			lowerPath := strings.ToLower(file.ArchivePath)
			if !strings.HasPrefix(lowerPath, prefix) {
				continue
			}
			text, readErr := archive.ReadText(file.ArchivePath)
			if readErr != nil {
				continue
			}
			haystack := strings.ToLower(text)
			for _, query := range queries {
				if strings.Contains(haystack, query) {
					fmt.Println(file.ArchivePath)
					break
				}
			}
		}
		return
	}

	sources := []struct {
		kind     string
		listPath string
	}{
		{kind: "equipment", listPath: "equipment/equipment.lst"},
		{kind: "stackable", listPath: "stackable/stackable.lst"},
		{kind: "npc", listPath: "npc/npc.lst"},
	}
	entries := make([]sourceEntry, 0, 65536)
	for _, source := range sources {
		if *npcOnly && source.kind != "npc" {
			continue
		}
		if !*npcOnly && source.kind == "npc" {
			continue
		}
		if source.kind == "stackable" && *equipmentOnly && !*allGuardianGems {
			continue
		}
		text, readErr := archive.ReadText(source.listPath)
		if readErr != nil {
			panic(readErr)
		}
		doc, parseErr := dnfpvf.Parse(source.listPath, text)
		if parseErr != nil {
			panic(parseErr)
		}
		for _, entry := range dnfpvf.ParseList(doc) {
			docPath := resolvePath(archive, source.listPath, entry.Path)
			if docPath == "" {
				continue
			}
			lowerPath := strings.ToLower(docPath)
			if source.kind == "stackable" && *allGuardianGems && len(queries) == 0 && !strings.Contains(lowerPath, "flaggem/") {
				continue
			}
			if source.kind == "equipment" && *allMedals && len(queries) == 0 && !strings.Contains(lowerPath, "flag/") {
				continue
			}
			entries = append(entries, sourceEntry{kind: source.kind, itemID: entry.ID, path: docPath})
		}
	}

	jobs := make(chan sourceEntry)
	results := make(chan result, 256)
	var workers sync.WaitGroup
	for worker := 0; worker < 16; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for entry := range jobs {
				if row, ok := inspect(archive, entry, queries, *allMedals, *allGuardianGems); ok {
					results <- row
				}
			}
		}()
	}
	go func() {
		for _, entry := range entries {
			jobs <- entry
		}
		close(jobs)
		workers.Wait()
		close(results)
	}()

	rows := make([]result, 0, 64)
	for row := range results {
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Kind != rows[j].Kind {
			return rows[i].Kind < rows[j].Kind
		}
		return rows[i].ItemID < rows[j].ItemID
	})
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(rows); err != nil {
		panic(fmt.Errorf("encode results: %w", err))
	}
}

func resolvePath(archive *platformpvf.Archive, listPath, reference string) string {
	reference = strings.TrimSpace(strings.Trim(reference, "`\"'"))
	for _, candidate := range []string{reference, path.Join(path.Dir(listPath), reference)} {
		if file, ok := archive.FindFile(candidate); ok {
			return file.ArchivePath
		}
	}
	return ""
}

func inspect(archive *platformpvf.Archive, entry sourceEntry, queries []string, allMedals, allGuardianGems bool) (result, bool) {
	text, err := archive.ReadText(entry.path)
	if err != nil {
		return result{}, false
	}
	doc, err := dnfpvf.Parse(entry.path, text)
	if err != nil {
		return result{}, false
	}
	name, _ := doc.Text("name")
	name2, _ := doc.Text("name2")
	equipmentType, _ := doc.Text("equipment type")
	stackableType, _ := doc.Text("stackable type")
	medal := entry.kind == "equipment" && strings.EqualFold(strings.TrimSpace(equipmentType), "[flag]")
	guardianGem := entry.kind == "stackable" && strings.EqualFold(strings.TrimSpace(stackableType), "[flag gem]")
	matched := (allMedals && medal) || (allGuardianGems && guardianGem)
	haystack := strings.ToLower(strings.Join([]string{name, name2, entry.path}, "\n"))
	for _, query := range queries {
		if strings.Contains(haystack, query) {
			matched = true
			break
		}
	}
	if !matched {
		return result{}, false
	}
	rarity, _ := doc.Int("rarity")
	grade, _ := doc.Int("grade")
	minimumLevel, _ := doc.Int("minimum level")
	durability, _ := doc.Int("durability")
	return result{
		Kind:          entry.kind,
		ItemID:        entry.itemID,
		Name:          name,
		Name2:         name2,
		Path:          entry.path,
		Rarity:        rarity,
		Grade:         grade,
		MinimumLevel:  minimumLevel,
		Durability:    durability,
		EquipmentType: equipmentType,
		StackableType: stackableType,
	}, true
}
