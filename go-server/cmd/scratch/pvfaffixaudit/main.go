package main

import (
	"flag"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	"longheng.io/server/internal/modules/dnf/combatpower"
	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

var affixTerms = []string{
	"additional damage",
	"increase damage",
	"critical damage",
	"add increase damage",
	"add increase critical damage",
	"all attack bonus rate",
}

func main() {
	pvfPath := flag.String("pvf", `D:/DNF/runtime/data/dnf/Script.pvf`, "Script.pvf path")
	setIDsText := flag.String("set-ids", "12590,12594,12605", "comma-separated part-set ids to locate")
	flag.Parse()

	archive, err := platformpvf.Open(*pvfPath)
	if err != nil {
		panic(err)
	}

	itemIDs := make(map[int64]struct{})
	for _, arg := range flag.Args() {
		itemID, parseErr := strconv.ParseInt(strings.TrimSpace(arg), 10, 64)
		if parseErr != nil {
			panic(parseErr)
		}
		itemIDs[itemID] = struct{}{}
	}
	if len(itemIDs) > 0 {
		auditItems(archive, itemIDs)
		catalog, loadErr := combatpower.Load(nil, archive)
		if loadErr != nil {
			panic(loadErr)
		}
		ids := make([]int64, 0, len(itemIDs))
		for itemID := range itemIDs {
			ids = append(ids, itemID)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		result, aggregateErr := catalog.Aggregate(nil, ids)
		if aggregateErr != nil {
			panic(aggregateErr)
		}
		fmt.Printf("AGGREGATE %+v\n", result)
	}

	setIDs := splitSetIDs(*setIDsText)
	if len(setIDs) > 0 {
		locateSetFiles(archive, setIDs)
	}
}

func auditItems(archive *platformpvf.Archive, requested map[int64]struct{}) {
	const listPath = "equipment/equipment.lst"
	text, err := archive.ReadText(listPath)
	if err != nil {
		panic(err)
	}
	doc, err := dnfpvf.Parse(listPath, text)
	if err != nil {
		panic(err)
	}

	paths := make(map[int64]string)
	for _, entry := range dnfpvf.ParseList(doc) {
		if _, ok := requested[entry.ID]; !ok {
			continue
		}
		for _, candidate := range []string{entry.Path, path.Join(path.Dir(listPath), entry.Path)} {
			if file, ok := archive.FindFile(candidate); ok {
				paths[entry.ID] = file.ArchivePath
				break
			}
		}
	}

	ids := make([]int64, 0, len(paths))
	for itemID := range paths {
		ids = append(ids, itemID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, itemID := range ids {
		itemPath := paths[itemID]
		itemText, readErr := archive.ReadText(itemPath)
		if readErr != nil {
			fmt.Printf("ITEM %d PATH %s ERROR %v\n", itemID, itemPath, readErr)
			continue
		}
		itemDoc, parseErr := dnfpvf.Parse(itemPath, itemText)
		if parseErr != nil {
			fmt.Printf("ITEM %d PATH %s PARSE_ERROR %v\n", itemID, itemPath, parseErr)
			continue
		}
		name, _ := itemDoc.Text("name")
		setID, _ := itemDoc.Int("part set index")
		fmt.Printf("ITEM %d NAME %q PATH %s SET %d\n", itemID, name, itemPath, setID)
		printMatchingLines(itemText, "  ")
	}
}

func locateSetFiles(archive *platformpvf.Archive, setIDs []string) {
	fmt.Println("SET_PATH_CANDIDATES")
	files := archive.Files()
	for _, file := range files {
		lower := strings.ToLower(file.ArchivePath)
		if strings.Contains(lower, "setitem") || strings.Contains(lower, "set_item") || strings.Contains(lower, "equipment/set") {
			fmt.Printf("  %s type=%d size=%d\n", file.ArchivePath, file.DataType, file.Size)
		}
	}
	fmt.Println("SET_ID_TEXT_MATCHES")
	for _, file := range files {
		lower := strings.ToLower(file.ArchivePath)
		if !strings.Contains(lower, "set") || file.DataType != 1 {
			continue
		}
		text, err := archive.FileText(file.Index)
		if err != nil {
			continue
		}
		matched := false
		for _, setID := range setIDs {
			if containsNumericToken(text, setID) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		fmt.Printf("FILE %s\n", file.ArchivePath)
		printMatchingLinesForTerms(text, "  ", append(setIDs, affixTerms...))
	}
}

func splitSetIDs(value string) []string {
	var out []string
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func containsNumericToken(text, token string) bool {
	for _, field := range strings.FieldsFunc(text, func(r rune) bool {
		return r < '0' || r > '9'
	}) {
		if field == token {
			return true
		}
	}
	return false
}

func printMatchingLines(text, prefix string) {
	printMatchingLinesForTerms(text, prefix, affixTerms)
}

func printMatchingLinesForTerms(text, prefix string, terms []string) {
	for lineNo, line := range strings.Split(text, "\n") {
		lower := strings.ToLower(line)
		for _, term := range terms {
			if strings.Contains(lower, strings.ToLower(term)) {
				fmt.Printf("%s%d: %s\n", prefix, lineNo+1, strings.TrimSpace(line))
				break
			}
		}
	}
}
