package dnfbridge

import (
	"fmt"
	"strings"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

func initialEquipmentPathMap(source initialEquipmentTextSource) (map[int64]string, error) {
	text, err := source.ReadText(initialEquipmentItemList)
	if err != nil {
		return nil, err
	}
	doc, err := dnfpvf.Parse(initialEquipmentItemList, text)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]string)
	for _, entry := range dnfpvf.ParseList(doc) {
		if entry.ID <= 0 || entry.Path == "" {
			continue
		}
		if _, exists := out[entry.ID]; !exists {
			out[entry.ID] = entry.Path
		}
	}
	return out, nil
}

func initialEquipmentDurability(source initialEquipmentTextSource, equipmentPaths map[int64]string, itemID int64) (uint16, string, error) {
	durability, _, pvfPath, _, err := initialEquipmentMetadata(source, equipmentPaths, itemID, "")
	return durability, pvfPath, err
}

func initialEquipmentMetadata(source initialEquipmentTextSource, equipmentPaths map[int64]string, itemID int64, jobTag string) (uint16, int64, string, []initialEquipmentModelLayer, error) {
	refPath, ok := equipmentPaths[itemID]
	if !ok {
		return 0, 0, "", nil, fmt.Errorf("initial equipment item %d not found in %s", itemID, initialEquipmentItemList)
	}
	text, actualPath, err := readInitialPVFText(source, initialPVFPath("equipment", refPath), refPath)
	if err != nil {
		return 0, 0, "", nil, err
	}
	doc, err := dnfpvf.Parse(actualPath, text)
	if err != nil {
		return 0, 0, "", nil, err
	}
	durability := int64(initialEquipmentDefaultDur)
	if value, ok := firstInitialEquipmentInt(doc, "durability", "maximum durability", "max durability"); ok && value > 0 {
		durability = value
	}
	if durability > 0xffff {
		durability = 0xffff
	}
	equipType, _ := firstInitialEquipmentInt(doc, "equipment type")
	return uint16(durability), equipType, actualPath, parseInitialEquipmentModelLayers(doc, jobTag), nil
}

func parseInitialEquipmentModelLayers(doc *dnfpvf.Document, jobTag string) []initialEquipmentModelLayer {
	if doc == nil || strings.TrimSpace(jobTag) == "" {
		return nil
	}
	wantJob := normalizeInitialPVFTag(jobTag)
	var layers []initialEquipmentModelLayer
	activeJob := false
	for idx, section := range doc.Sections {
		name := normalizeInitialEquipmentSlotName(section.Name)
		switch name {
		case "animation job":
			activeJob = normalizeInitialPVFTag(sectionFirstText(doc, section)) == wantJob
		case "layer variation":
			if !activeJob {
				continue
			}
			layer, ok := parseInitialEquipmentLayerVariation(doc, section)
			if !ok {
				continue
			}
			if idx+1 < len(doc.Sections) && normalizeInitialEquipmentSlotName(doc.Sections[idx+1].Name) == "equipment ani script" {
				layer.Script = strings.TrimSpace(sectionFirstText(doc, doc.Sections[idx+1]))
			}
			layers = append(layers, layer)
		}
	}
	return layers
}

func parseInitialEquipmentLayerVariation(doc *dnfpvf.Document, section dnfpvf.Section) (initialEquipmentModelLayer, bool) {
	var layer initialEquipmentModelLayer
	if section.Start < 0 || section.End > len(doc.Tokens) || section.Start > section.End {
		return layer, false
	}
	for _, token := range doc.Tokens[section.Start:section.End] {
		switch token.Kind {
		case dnfpvf.TokenInt:
			if layer.Key == 0 && token.Int > 0 && token.Int <= 0xffff {
				layer.Key = uint16(token.Int)
			}
		case dnfpvf.TokenString, dnfpvf.TokenIdent:
			if strings.TrimSpace(layer.Name) == "" {
				layer.Name = strings.TrimSpace(token.Value)
			}
		}
	}
	if layer.Key == 0 || strings.TrimSpace(layer.Name) == "" {
		return layer, false
	}
	return layer, true
}

func sectionFirstText(doc *dnfpvf.Document, section dnfpvf.Section) string {
	if doc == nil || section.Start < 0 || section.End > len(doc.Tokens) || section.Start > section.End {
		return ""
	}
	for _, token := range doc.Tokens[section.Start:section.End] {
		if token.Kind == dnfpvf.TokenString || token.Kind == dnfpvf.TokenIdent {
			return strings.TrimSpace(token.Value)
		}
	}
	return ""
}

func firstInitialPVFSectionText(text, name string) (string, bool) {
	want := normalizeInitialEquipmentSlotName(name)
	lines := strings.Split(text, "\n")
	for idx, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "[") {
			continue
		}
		end := strings.IndexByte(trimmed, ']')
		if end <= 0 || normalizeInitialEquipmentSlotName(trimmed[1:end]) != want {
			continue
		}
		if value, ok := firstInitialPVFLineText(trimmed[end+1:]); ok {
			return value, true
		}
		for next := idx + 1; next < len(lines); next++ {
			nextLine := strings.TrimSpace(lines[next])
			if nextLine == "" || strings.HasPrefix(nextLine, "#") || strings.HasPrefix(nextLine, "//") {
				continue
			}
			if strings.HasPrefix(nextLine, "[") {
				if nextEnd := strings.IndexByte(nextLine, ']'); nextEnd > 0 {
					return "", false
				}
			}
			return firstInitialPVFLineText(nextLine)
		}
		return "", false
	}
	return "", false
}

func firstInitialPVFLineText(line string) (string, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
		return "", false
	}
	if idx := strings.Index(line, "//"); idx >= 0 {
		line = strings.TrimSpace(line[:idx])
	}
	if idx := strings.Index(line, "#"); idx >= 0 {
		line = strings.TrimSpace(line[:idx])
	}
	if line == "" {
		return "", false
	}
	switch line[0] {
	case '`', '\'', '"':
		quote := line[0]
		if end := strings.IndexByte(line[1:], quote); end >= 0 {
			return strings.TrimSpace(line[1 : end+1]), true
		}
	}
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return "", false
	}
	return strings.Trim(fields[0], "`'\""), strings.Trim(fields[0], "`'\"") != ""
}

func normalizeInitialPVFTag(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}

func firstInitialEquipmentInt(doc *dnfpvf.Document, names ...string) (int64, bool) {
	for _, name := range names {
		if value, ok := doc.Int(name); ok {
			return value, true
		}
	}
	return 0, false
}
