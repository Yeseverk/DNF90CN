package worldmap

import (
	"fmt"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

func ParseArea(id int64, areaPath string, doc *dnfpvf.Document) Area {
	result := Area{ID: id, Path: areaPath}
	sections, diagnostics := rawSections(doc)
	result.SourceSections = sections
	result.Diagnostics = append(result.Diagnostics, diagnostics...)
	for _, section := range sections {
		key := sectionKey(section.Name)
		if isClosingSection(key) {
			continue
		}
		if _, ok := knownAreaSections[key]; !ok {
			result.UnknownSections = append(result.UnknownSections, cloneRawSection(section))
			result.Extensions = append(result.Extensions, structureExtension(section))
			continue
		}
		switch key {
		case "map image":
			if path, ok := firstText(section); ok {
				result.MapImage.Path = path
			} else {
				result.Diagnostics = append(result.Diagnostics, diagnostic(section, "expected map image path"))
			}
			parsed, _ := ints(section)
			result.MapImage.Params = append(result.MapImage.Params, parsed...)
		case "ui path":
			setText(&result.UIPath, section, &result.Diagnostics)
		case "name":
			setText(&result.Name, section, &result.Diagnostics)
		case "dungeon":
			result.Dungeons = append(result.Dungeons, parseDungeonEntries(section, &result.Diagnostics)...)
		case "in progress":
			result.Dungeons = append(result.Dungeons, parseInProgressDungeonEntries(section, &result.Diagnostics)...)
		case "hell dungeon":
			setOptionalInt(&result.HellDungeon, section, &result.Diagnostics)
		case "hell quest":
			result.HellQuestIDs = appendInts(result.HellQuestIDs, section, &result.Diagnostics)
		case "hell freepass item":
			result.HellFreePassItems = append(result.HellFreePassItems, parseTicketItems(section, &result.Diagnostics)...)
		case "item condition":
			result.ItemConditions = appendInts(result.ItemConditions, section, &result.Diagnostics)
		}
	}
	return result
}

func parseInProgressDungeonEntries(section RawSection, diagnostics *[]Diagnostic) []DungeonEntry {
	parsed, valid := ints(section)
	if !valid || len(parsed)%2 != 0 {
		*diagnostics = append(*diagnostics, diagnostic(section, "expected in-progress quest/dungeon integer pairs"))
	}
	out := make([]DungeonEntry, 0, len(parsed)/2)
	for index := 0; index+1 < len(parsed); index += 2 {
		out = append(out, DungeonEntry{QuestID: parsed[index], DungeonID: parsed[index+1], InProgressOnly: true})
	}
	return out
}

func parseDungeonEntries(section RawSection, diagnostics *[]Diagnostic) []DungeonEntry {
	tokens := values(section)
	var out []DungeonEntry
	pendingInProgress := false
	for i := 0; i < len(tokens); {
		if marker, width := inProgressMarker(tokens, i); marker {
			pendingInProgress = true
			i += width
			continue
		}
		if tokens[i].Kind != dnfpvf.TokenInt {
			*diagnostics = append(*diagnostics, diagnostic(section, fmt.Sprintf("unexpected dungeon token %q", tokens[i].Raw)))
			i++
			continue
		}
		first := tokens[i].Int
		i++
		if pendingInProgress {
			if i >= len(tokens) || tokens[i].Kind != dnfpvf.TokenInt {
				*diagnostics = append(*diagnostics, diagnostic(section, "in-progress quest is missing dungeon id"))
				break
			}
			out = append(out, DungeonEntry{DungeonID: tokens[i].Int, QuestID: first, InProgressOnly: true})
			i++
			pendingInProgress = false
			continue
		}
		entry := DungeonEntry{DungeonID: first, QuestID: -1}
		if marker, width := inProgressMarker(tokens, i); marker {
			entry.InProgressOnly = true
			i += width
		}
		if i < len(tokens) && tokens[i].Kind == dnfpvf.TokenInt {
			entry.QuestID = tokens[i].Int
			i++
		}
		out = append(out, entry)
	}
	if pendingInProgress {
		*diagnostics = append(*diagnostics, diagnostic(section, "in-progress marker has no quest/dungeon pair"))
	}
	return out
}

func inProgressMarker(tokens []dnfpvf.Token, index int) (bool, int) {
	if index < 0 || index >= len(tokens) || !isText(tokens[index]) {
		return false, 0
	}
	value := sectionKey(tokens[index].Value)
	if value == "[in progress]" || value == "in progress" {
		return true, 1
	}
	if (value == "[in" || value == "in") && index+1 < len(tokens) && isText(tokens[index+1]) {
		next := sectionKey(tokens[index+1].Value)
		if next == "progress]" || next == "progress" {
			return true, 2
		}
	}
	return false, 0
}

func parseTicketItems(section RawSection, diagnostics *[]Diagnostic) []TicketItem {
	parsed, valid := ints(section)
	if !valid || len(parsed)%2 != 0 {
		*diagnostics = append(*diagnostics, diagnostic(section, "expected item/count integer pairs"))
	}
	out := make([]TicketItem, 0, len(parsed)/2)
	for i := 0; i+1 < len(parsed); i += 2 {
		out = append(out, TicketItem{ItemID: parsed[i], Count: parsed[i+1]})
	}
	return out
}
