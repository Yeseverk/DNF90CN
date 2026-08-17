package dnfbridge

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

var (
	// 对齐 C# InitialCharacterEquipment.SlotMap。
	initialEquipmentSlotMap = map[string]int16{
		"weapon":      11,
		"coat":        13,
		"pants":       15,
		"shoulder":    14,
		"shoes":       16,
		"waist":       17,
		"amulet":      18,
		"wrist":       19,
		"ring":        20,
		"support":     21,
		"magic stone": 22,
		"earring":     23,
	}
	// DOVE new-character behavior equips these three starter categories.
	// Item ids and metadata still come from the current PVF list.
	initialEquipmentStarterSlots = map[string]struct{}{
		"weapon": {},
		"coat":   {},
		"pants":  {},
	}
	initialEquipmentLineRE = regexp.MustCompile("(?i)\\[([A-Za-z_][A-Za-z0-9_\\s]*)\\]`?\\s*([0-9]+)")
)

func isInitialEquipmentStarterSlot(slot int16) bool {
	switch slot {
	case 11, 13, 15:
		return true
	default:
		return false
	}
}

func parseInitialCharacterEquipmentFromSource(source initialEquipmentTextSource, job byte) ([]initialEquipmentEntry, error) {
	if source == nil {
		return nil, fmt.Errorf("initial equipment pvf source is nil")
	}
	characterList, err := source.ReadText(initialEquipmentCharacterList)
	if err != nil {
		return nil, err
	}
	characterPath, ok, err := initialCharacterPVFPath(characterList, job)
	if err != nil || !ok {
		return nil, err
	}
	characterText, _, err := readInitialPVFText(source, initialPVFPath("character", characterPath), characterPath)
	if err != nil {
		return nil, err
	}
	entries := parseInitialEquipmentSection(characterText)
	if len(entries) == 0 {
		return nil, nil
	}
	jobTag := initialEquipmentJobTag(characterText)
	equipmentPaths, err := initialEquipmentPathMap(source)
	if err != nil {
		return nil, err
	}
	for idx := range entries {
		durability, equipType, pvfPath, modelLayers, err := initialEquipmentMetadata(source, equipmentPaths, entries[idx].ItemID, jobTag)
		if err != nil {
			return nil, err
		}
		entries[idx].Durability = durability
		entries[idx].EquipType = equipType
		entries[idx].PVFPath = pvfPath
		entries[idx].RawEntry = buildInitialEquipmentRawEntry(entries[idx].Slot, entries[idx].ItemID, durability)
		entries[idx].ModelLayers = modelLayers
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Slot == entries[j].Slot {
			return entries[i].ItemID < entries[j].ItemID
		}
		return entries[i].Slot < entries[j].Slot
	})
	return entries, nil
}

func parseInitialCharacterEquipmentAllFromSource(source initialEquipmentTextSource) (map[byte][]initialEquipmentEntry, error) {
	if source == nil {
		return nil, fmt.Errorf("initial equipment pvf source is nil")
	}
	characterList, err := source.ReadText(initialEquipmentCharacterList)
	if err != nil {
		return nil, err
	}
	doc, err := dnfpvf.Parse(initialEquipmentCharacterList, characterList)
	if err != nil {
		return nil, err
	}
	equipmentPaths, err := initialEquipmentPathMap(source)
	if err != nil {
		return nil, err
	}
	out := make(map[byte][]initialEquipmentEntry)
	for _, listEntry := range dnfpvf.ParseList(doc) {
		if listEntry.ID < 0 || listEntry.ID > 0xff || strings.TrimSpace(listEntry.Path) == "" {
			continue
		}
		characterText, _, err := readInitialPVFText(source, initialPVFPath("character", listEntry.Path), listEntry.Path)
		if err != nil {
			return nil, err
		}
		entries := parseInitialEquipmentSection(characterText)
		if len(entries) == 0 {
			continue
		}
		jobTag := initialEquipmentJobTag(characterText)
		for idx := range entries {
			durability, equipType, pvfPath, modelLayers, err := initialEquipmentMetadata(source, equipmentPaths, entries[idx].ItemID, jobTag)
			if err != nil {
				return nil, err
			}
			entries[idx].Durability = durability
			entries[idx].EquipType = equipType
			entries[idx].PVFPath = pvfPath
			entries[idx].RawEntry = buildInitialEquipmentRawEntry(entries[idx].Slot, entries[idx].ItemID, durability)
			entries[idx].ModelLayers = modelLayers
		}
		sort.SliceStable(entries, func(i, j int) bool {
			if entries[i].Slot == entries[j].Slot {
				return entries[i].ItemID < entries[j].ItemID
			}
			return entries[i].Slot < entries[j].Slot
		})
		out[byte(listEntry.ID)] = cloneInitialEquipmentEntries(entries)
	}
	return out, nil
}

func initialEquipmentJobTag(characterText string) string {
	doc, err := dnfpvf.Parse("character/current.chr", characterText)
	if err == nil {
		if job, ok := doc.Text("job"); ok && strings.TrimSpace(job) != "" {
			return strings.TrimSpace(job)
		}
	}
	job, _ := firstInitialPVFSectionText(characterText, "job")
	return job
}

func initialCharacterPVFPath(characterList string, job byte) (string, bool, error) {
	doc, err := dnfpvf.Parse(initialEquipmentCharacterList, characterList)
	if err != nil {
		return "", false, err
	}
	want := int64(job)
	for _, entry := range dnfpvf.ParseList(doc) {
		if entry.ID == want {
			return entry.Path, true, nil
		}
	}
	return "", false, nil
}

func parseInitialEquipmentSection(characterText string) []initialEquipmentEntry {
	lower := strings.ToLower(characterText)
	start := strings.Index(lower, "[create equipment list]")
	if start < 0 {
		return nil
	}
	end := strings.Index(lower[start:], "[/create equipment list]")
	section := characterText[start:]
	if end >= 0 {
		section = characterText[start : start+end]
	}
	matches := initialEquipmentLineRE.FindAllStringSubmatch(section, -1)
	if len(matches) == 0 {
		return nil
	}
	entries := make([]initialEquipmentEntry, 0, len(matches))
	for _, match := range matches {
		if len(match) < 3 {
			continue
		}
		slotName := normalizeInitialEquipmentSlotName(match[1])
		if _, starter := initialEquipmentStarterSlots[slotName]; !starter {
			continue
		}
		slot, ok := initialEquipmentSlotMap[slotName]
		if !ok {
			continue
		}
		itemID, err := strconv.ParseInt(match[2], 10, 64)
		if err != nil || itemID <= 0 {
			continue
		}
		entries = append(entries, initialEquipmentEntry{Slot: slot, ItemID: itemID})
	}
	if len(entries) == 0 {
		return nil
	}
	return entries
}
