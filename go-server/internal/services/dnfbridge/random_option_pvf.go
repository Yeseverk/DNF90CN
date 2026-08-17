package dnfbridge

import (
	"errors"
	"fmt"
	"math"
	"path"
	"sort"
	"strings"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

const (
	currentRandomOptionOverallPath  = "etc/randomoption/randomizedoptionoverall2.etc"
	currentRandomOptionQuantityPath = "etc/randomoption/optionquantity.etc"
	currentRandomOptionSelectPath   = "etc/randomoption/optiongroupselection.etc"
	currentRandomOptionGroupingPath = "etc/randomoption/optiongrouping.etc"
	currentRandomOptionListPath     = "etc/randomoption/randomoption.lst"
)

type currentRandomOptionConfig struct {
	quantity       map[int64][]alignedcmd.RandomOptionWeightedQuantity
	initialSelect  map[int64]map[string][3]int64
	modifiedSelect map[int64]map[string][3]int64
	groups         map[int64][]currentRandomOptionWeightedID
	optionPaths    map[int64]string
	breakSealCosts []currentRandomOptionBreakCost
	modifyCosts    []currentRandomOptionModifyCost
}

type currentRandomOptionWeightedID struct {
	optionID int64
	weight   int64
}

type currentRandomOptionBreakCost struct {
	rarity     int64
	level      int64
	optionSlot int64
	cost       int64
}

type currentRandomOptionModifyCost struct {
	level      int64
	commonCost int64
	uniqueCost int64
}

func (s *Service) alignedRandomOptionResolverForCommand(opcode dnfenum.CmdPacket) (alignedcmd.RandomOptionResolver, error) {
	if opcode != dnfenum.CmdPacketUnsealRandomOption && opcode != dnfenum.CmdPacketChangeRandomOption {
		return nil, nil
	}
	catalog, err := s.currentPVFItemCatalog()
	if err != nil {
		return nil, err
	}
	if catalog == nil {
		return nil, errDungeonDropSourceRequired
	}
	s.initialEquipmentMu.Lock()
	source, err := s.initialEquipmentArchiveLocked()
	s.initialEquipmentMu.Unlock()
	if err != nil {
		return nil, err
	}
	config, err := loadCurrentRandomOptionConfig(source)
	if err != nil {
		return nil, err
	}
	return func(targetItemID int64) (alignedcmd.RandomOptionResolution, error) {
		return resolveCurrentRandomOptionMetadata(catalog, source, config, targetItemID)
	}, nil
}

func loadCurrentRandomOptionConfig(source dnfpvf.Source) (*currentRandomOptionConfig, error) {
	if source == nil {
		return nil, dnfpvf.ErrSourceRequired
	}
	overall, err := parseCurrentRandomOptionDocument(source, currentRandomOptionOverallPath)
	if err != nil {
		return nil, err
	}
	quantity, err := parseCurrentRandomOptionDocument(source, currentRandomOptionQuantityPath)
	if err != nil {
		return nil, err
	}
	selection, err := parseCurrentRandomOptionDocument(source, currentRandomOptionSelectPath)
	if err != nil {
		return nil, err
	}
	grouping, err := parseCurrentRandomOptionDocument(source, currentRandomOptionGroupingPath)
	if err != nil {
		return nil, err
	}
	list, err := parseCurrentRandomOptionDocument(source, currentRandomOptionListPath)
	if err != nil {
		return nil, err
	}

	config := &currentRandomOptionConfig{
		quantity:       parseCurrentRandomOptionQuantities(quantity),
		initialSelect:  make(map[int64]map[string][3]int64),
		modifiedSelect: make(map[int64]map[string][3]int64),
		groups:         parseCurrentRandomOptionGroups(grouping),
		optionPaths:    make(map[int64]string),
	}
	parseCurrentRandomOptionSelections(selection, config.initialSelect, config.modifiedSelect)
	config.breakSealCosts, config.modifyCosts = parseCurrentRandomOptionCosts(overall)
	for _, entry := range dnfpvf.ParseList(list) {
		if entry.ID <= 0 || strings.TrimSpace(entry.Path) == "" {
			continue
		}
		config.optionPaths[entry.ID] = path.Clean(path.Join(path.Dir(currentRandomOptionListPath), entry.Path))
	}
	if len(config.quantity[2]) == 0 || len(config.quantity[3]) == 0 {
		return nil, fmt.Errorf("random-option quantity config missing rare or unique weights")
	}
	if len(config.initialSelect) == 0 || len(config.modifiedSelect) == 0 || len(config.groups) == 0 || len(config.optionPaths) == 0 {
		return nil, fmt.Errorf("random-option config incomplete: initial=%d modified=%d groups=%d options=%d", len(config.initialSelect), len(config.modifiedSelect), len(config.groups), len(config.optionPaths))
	}
	if len(config.breakSealCosts) == 0 || len(config.modifyCosts) == 0 {
		return nil, fmt.Errorf("random-option cost config incomplete: break=%d modify=%d", len(config.breakSealCosts), len(config.modifyCosts))
	}
	return config, nil
}

func parseCurrentRandomOptionDocument(source dnfpvf.Source, documentPath string) (*dnfpvf.Document, error) {
	text, err := source.ReadText(documentPath)
	if err != nil {
		return nil, fmt.Errorf("read random-option PVF %s: %w", documentPath, err)
	}
	document, err := dnfpvf.Parse(documentPath, text)
	if err != nil {
		return nil, fmt.Errorf("parse random-option PVF %s: %w", documentPath, err)
	}
	return document, nil
}

func parseCurrentRandomOptionQuantities(document *dnfpvf.Document) map[int64][]alignedcmd.RandomOptionWeightedQuantity {
	out := make(map[int64][]alignedcmd.RandomOptionWeightedQuantity, 2)
	for _, section := range document.Sections {
		var rarity int64
		switch currentRandomOptionSectionKey(section.Name) {
		case "common":
			rarity = 2
		case "unique":
			rarity = 3
		default:
			continue
		}
		values := currentRandomOptionSectionInts(document, section)
		for index := 0; index+1 < len(values); index += 2 {
			quantity, weight := values[index], values[index+1]
			if quantity >= 1 && quantity <= 3 && weight > 0 {
				out[rarity] = append(out[rarity], alignedcmd.RandomOptionWeightedQuantity{Quantity: byte(quantity), Weight: weight})
			}
		}
	}
	return out
}

func parseCurrentRandomOptionSelections(document *dnfpvf.Document, initial map[int64]map[string][3]int64, modified map[int64]map[string][3]int64) {
	for _, section := range document.Sections {
		key := currentRandomOptionSectionKey(section.Name)
		var target map[int64]map[string][3]int64
		switch {
		case strings.Contains(key, "modified option selection"):
			target = modified
		case strings.Contains(key, "choose option group"):
			target = initial
		default:
			continue
		}
		values := currentRandomOptionSectionValues(document, section)
		for index, value := range values {
			if value.text == "" || index < 2 || index+1 >= len(values) {
				continue
			}
			rarity, rarityOK := values[index-2].integer()
			marker, markerOK := values[index-1].integer()
			if !rarityOK || !markerOK || marker != -1 || (rarity != 2 && rarity != 3) {
				continue
			}
			// The runtime row starts with two control integers (1,3) after the
			// equipment key. The first three 1000-series integers are the real
			// group IDs; later integers are flags/defaults and must be ignored.
			var groups [3]int64
			groupCount := 0
			for cursor := index + 1; cursor < len(values) && groupCount < len(groups); cursor++ {
				group, ok := values[cursor].integer()
				if ok && group >= 1000 && group < 2000 {
					groups[groupCount] = group
					groupCount++
				}
			}
			if groupCount != len(groups) {
				continue
			}
			equipmentKey := normalizeCurrentRandomOptionKey(value.text)
			if equipmentKey == "" {
				continue
			}
			if target[rarity] == nil {
				target[rarity] = make(map[string][3]int64)
			}
			target[rarity][equipmentKey] = groups
		}
	}
}

func parseCurrentRandomOptionGroups(document *dnfpvf.Document) map[int64][]currentRandomOptionWeightedID {
	out := make(map[int64][]currentRandomOptionWeightedID)
	for _, section := range document.Sections {
		if currentRandomOptionSectionKey(section.Name) != "option group" {
			continue
		}
		values := currentRandomOptionSectionInts(document, section)
		if len(values) < 3 {
			continue
		}
		groupID := values[0]
		for index := 1; index+1 < len(values); index += 2 {
			optionID, weight := values[index], values[index+1]
			if groupID > 0 && optionID > 0 && weight > 0 {
				out[groupID] = append(out[groupID], currentRandomOptionWeightedID{optionID: optionID, weight: weight})
			}
		}
	}
	return out
}

func parseCurrentRandomOptionCosts(document *dnfpvf.Document) ([]currentRandomOptionBreakCost, []currentRandomOptionModifyCost) {
	var breaks []currentRandomOptionBreakCost
	var modifies []currentRandomOptionModifyCost
	for _, section := range document.Sections {
		values := currentRandomOptionSectionInts(document, section)
		switch currentRandomOptionSectionKey(section.Name) {
		case "break seal cost":
			for index := 0; index+3 < len(values); index += 4 {
				row := currentRandomOptionBreakCost{rarity: values[index], level: values[index+1], optionSlot: values[index+2], cost: values[index+3]}
				if (row.rarity == 2 || row.rarity == 3) && row.level >= 0 && row.optionSlot >= 0 && row.cost >= 0 {
					breaks = append(breaks, row)
				}
			}
		case "option modification":
			for index := 0; index+2 < len(values); index += 3 {
				row := currentRandomOptionModifyCost{level: values[index], commonCost: values[index+1], uniqueCost: values[index+2]}
				if row.level >= 0 && row.commonCost >= 0 && row.uniqueCost >= 0 {
					modifies = append(modifies, row)
				}
			}
		}
	}
	return breaks, modifies
}

func resolveCurrentRandomOptionMetadata(catalog *pvfDungeonDropCatalog, source dnfpvf.Source, config *currentRandomOptionConfig, targetItemID int64) (alignedcmd.RandomOptionResolution, error) {
	resolution := alignedcmd.RandomOptionResolution{}
	if catalog == nil || source == nil || config == nil {
		return resolution, errDungeonDropSourceRequired
	}
	if targetItemID <= 0 || targetItemID > math.MaxUint32 {
		return resolution, nil
	}
	definition, err := catalog.ResolveItem(uint32(targetItemID))
	if err != nil {
		if errors.Is(err, errDungeonDropItemUnresolved) {
			return resolution, nil
		}
		return resolution, fmt.Errorf("resolve random-option target item=%d: %w", targetItemID, err)
	}
	resolution.TargetKind = string(definition.Kind)
	resolution.TargetPVFPath = definition.PVFPath
	if definition.Kind != dungeonDropItemEquipment {
		return resolution, nil
	}
	document, err := parseCurrentRandomOptionDocument(source, definition.PVFPath)
	if err != nil {
		return alignedcmd.RandomOptionResolution{}, err
	}
	minimumLevel, minimumFound := document.Int("minimum level")
	rarity, rarityFound := document.Int("rarity")
	randomOption, randomFound := document.Int("random option")
	equipmentType, typeFound := document.Text("equipment type")
	if !minimumFound || !rarityFound || !randomFound || !typeFound || randomOption <= 0 || (rarity != 2 && rarity != 3) {
		return resolution, nil
	}
	resolution.TargetMinimumLevel = minimumLevel
	resolution.TargetRarity = rarity

	keys := currentRandomOptionEquipmentKeys(definition.PVFPath, equipmentType)
	initialGroups, initialKey, initialFound := currentRandomOptionSelectedGroups(config.initialSelect[rarity], keys)
	modifiedGroups, _, modifiedFound := currentRandomOptionSelectedGroups(config.modifiedSelect[rarity], keys)
	if !initialFound || !modifiedFound {
		return resolution, nil
	}
	resolution.TargetEquipmentKey = initialKey
	resolution.QuantityWeights = append([]alignedcmd.RandomOptionWeightedQuantity(nil), config.quantity[rarity]...)
	resolution.InitialGroups, err = resolveCurrentRandomOptionGroups(source, config, initialGroups, minimumLevel)
	if err != nil {
		return alignedcmd.RandomOptionResolution{}, fmt.Errorf("resolve initial random-option groups item=%d key=%s: %w", targetItemID, initialKey, err)
	}
	resolution.ModifiedGroups, err = resolveCurrentRandomOptionGroups(source, config, modifiedGroups, minimumLevel)
	if err != nil {
		return alignedcmd.RandomOptionResolution{}, fmt.Errorf("resolve modified random-option groups item=%d key=%s: %w", targetItemID, initialKey, err)
	}
	resolution.BreakSealGoldCost, err = currentRandomOptionBreakSealCost(config.breakSealCosts, rarity, minimumLevel)
	if err != nil {
		return alignedcmd.RandomOptionResolution{}, err
	}
	resolution.ModificationGoldCost, err = currentRandomOptionModificationCost(config.modifyCosts, rarity, minimumLevel)
	if err != nil {
		return alignedcmd.RandomOptionResolution{}, err
	}
	resolution.Eligible = len(resolution.QuantityWeights) > 0 && len(resolution.InitialGroups) == 3 && len(resolution.ModifiedGroups) == 3
	return resolution, nil
}

func resolveCurrentRandomOptionGroups(source dnfpvf.Source, config *currentRandomOptionConfig, groups [3]int64, level int64) ([][]alignedcmd.RandomOptionCandidate, error) {
	out := make([][]alignedcmd.RandomOptionCandidate, 0, len(groups))
	for _, groupID := range groups {
		weighted := config.groups[groupID]
		if len(weighted) == 0 {
			return nil, fmt.Errorf("group %d missing", groupID)
		}
		candidates := make([]alignedcmd.RandomOptionCandidate, 0, len(weighted))
		for _, entry := range weighted {
			optionPath := config.optionPaths[entry.optionID]
			if optionPath == "" {
				return nil, fmt.Errorf("group %d option %d path missing", groupID, entry.optionID)
			}
			optionType, value1, value2, err := resolveCurrentRandomOptionValues(source, optionPath, level)
			if err != nil {
				return nil, fmt.Errorf("group %d option %d: %w", groupID, entry.optionID, err)
			}
			candidates = append(candidates, alignedcmd.RandomOptionCandidate{Type: optionType, Value1: value1, Value2: value2, Weight: entry.weight})
		}
		if len(candidates) == 0 {
			return nil, fmt.Errorf("group %d has no candidates", groupID)
		}
		out = append(out, candidates)
	}
	return out, nil
}

func resolveCurrentRandomOptionValues(source dnfpvf.Source, optionPath string, targetLevel int64) (byte, byte, byte, error) {
	document, err := parseCurrentRandomOptionDocument(source, optionPath)
	if err != nil {
		return 0, 0, 0, err
	}
	optionID, found := document.Int("option")
	if !found || optionID <= 0 || optionID > math.MaxUint8 {
		return 0, 0, 0, fmt.Errorf("option type invalid: %d found=%t", optionID, found)
	}
	dungeonStart, dungeonEnd := -1, len(document.Sections)
	for index, section := range document.Sections {
		name := currentRandomOptionSectionKey(section.Name)
		if dungeonStart < 0 {
			if name == "dungeon" {
				dungeonStart = index + 1
			}
			continue
		}
		if name == "/dungeon" {
			dungeonEnd = index
			break
		}
	}
	if dungeonStart < 0 || dungeonStart > dungeonEnd {
		return 0, 0, 0, fmt.Errorf("option %d dungeon value block missing", optionID)
	}
	bestLevel := int64(-1)
	var bestValues []currentRandomOptionNumber
	elementProperty, categorical := currentRandomOptionElementProperty(optionID)
	for index := dungeonStart; index < dungeonEnd; index++ {
		section := document.Sections[index]
		if currentRandomOptionSectionKey(section.Name) != "level" {
			continue
		}
		levelValues := currentRandomOptionSectionInts(document, section)
		if len(levelValues) == 0 || levelValues[0] < 0 || levelValues[0] > targetLevel || levelValues[0] < bestLevel {
			continue
		}
		var values []currentRandomOptionNumber
		var elementValues []string
		for cursor := index + 1; cursor < dungeonEnd; cursor++ {
			name := currentRandomOptionSectionKey(document.Sections[cursor].Name)
			if name == "level" || name == "/level" {
				break
			}
			values = append(values, currentRandomOptionSectionNumbers(document, document.Sections[cursor])...)
			if name == "elemental property" || name == "/elemental property" {
				for _, value := range currentRandomOptionSectionValues(document, document.Sections[cursor]) {
					if !value.isInteger && strings.TrimSpace(value.text) != "" {
						elementValues = append(elementValues, normalizeCurrentRandomOptionKey(value.text))
					}
				}
			}
		}
		if categorical {
			if len(elementValues) == 2 && elementValues[0] == elementProperty && elementValues[1] == elementProperty {
				bestLevel = levelValues[0]
				bestValues = []currentRandomOptionNumber{{value: 1}, {value: 1}}
			}
			continue
		}
		if len(values) >= 2 {
			bestLevel = levelValues[0]
			bestValues = values[len(values)-2:]
		}
	}
	if bestLevel < 0 || len(bestValues) != 2 {
		return 0, 0, 0, fmt.Errorf("option %d has no values for target level %d (best_level=%d values=%v)", optionID, targetLevel, bestLevel, bestValues)
	}
	value1, err := encodeCurrentRandomOptionNumber(bestValues[0])
	if err != nil {
		return 0, 0, 0, fmt.Errorf("option %d value1: %w", optionID, err)
	}
	value2, err := encodeCurrentRandomOptionNumber(bestValues[1])
	if err != nil {
		return 0, 0, 0, fmt.Errorf("option %d value2: %w", optionID, err)
	}
	return byte(optionID), value1, value2, nil
}

// The runtime PVF represents the four elemental-attack options with two
// repeated textual endpoints instead of numeric scalars. The option ID itself
// identifies the element, while the current item row still has one byte for
// each endpoint. Encode the two validated endpoint-presence markers as 1/1.
// Keep this closed list strict so unrelated malformed text options still fail.
func currentRandomOptionElementProperty(optionID int64) (string, bool) {
	switch optionID {
	case 244:
		return "fireelement", true
	case 245:
		return "waterelement", true
	case 246:
		return "darkelement", true
	case 247:
		return "lightelement", true
	default:
		return "", false
	}
}

// Current item rows have one byte per random-option scalar and the current EXE
// forwards that byte unchanged into its random-option model. Preserve signed
// negative values as two's-complement bytes, truncate fractional PVF scalars
// toward zero, and saturate values outside the one-byte wire domain.
func encodeCurrentRandomOptionNumber(value currentRandomOptionNumber) (byte, error) {
	encoded := math.Trunc(value.value)
	if math.IsNaN(encoded) || math.IsInf(encoded, 0) {
		return 0, fmt.Errorf("PVF scalar %v is not finite", value.value)
	}
	if encoded < math.MinInt8 {
		encoded = math.MinInt8
	}
	if encoded > math.MaxUint8 {
		encoded = math.MaxUint8
	}
	if encoded < 0 {
		return byte(int8(int64(encoded))), nil
	}
	return byte(encoded), nil
}

func currentRandomOptionBreakSealCost(rows []currentRandomOptionBreakCost, rarity int64, level int64) (int64, error) {
	bestLevel := int64(-1)
	cost := int64(0)
	found := false
	for _, row := range rows {
		if row.rarity == rarity && row.optionSlot == 1 && row.level <= level && row.level >= bestLevel {
			bestLevel, cost, found = row.level, row.cost, true
		}
	}
	if !found {
		return 0, fmt.Errorf("random-option break-seal cost missing for rarity=%d level=%d", rarity, level)
	}
	return cost, nil
}

func currentRandomOptionModificationCost(rows []currentRandomOptionModifyCost, rarity int64, level int64) (int64, error) {
	bestLevel := int64(-1)
	cost := int64(0)
	found := false
	for _, row := range rows {
		if row.level <= level && row.level >= bestLevel {
			bestLevel, found = row.level, true
			if rarity >= 3 {
				cost = row.uniqueCost
			} else {
				cost = row.commonCost
			}
		}
	}
	if !found {
		return 0, fmt.Errorf("random-option modification cost missing for rarity=%d level=%d", rarity, level)
	}
	return cost, nil
}

func currentRandomOptionEquipmentKeys(pvfPath string, equipmentType string) []string {
	keys := make(map[string]struct{})
	add := func(value string) {
		if normalized := normalizeCurrentRandomOptionKey(value); normalized != "" {
			keys[normalized] = struct{}{}
		}
	}
	normalizedEquipmentType := normalizeCurrentRandomOptionKey(equipmentType)
	add(normalizedEquipmentType)
	segments := strings.Split(strings.ToLower(strings.ReplaceAll(pvfPath, "\\", "/")), "/")
	for index, segment := range segments {
		switch segment {
		case "weapon":
			if index+1 < len(segments) {
				weapon := normalizeCurrentRandomOptionKey(segments[index+1])
				add(weapon)
				switch weapon {
				case "boxglove":
					add("bglove")
				case "hsword":
					add("lswd")
				case "beamsword":
					add("beamswd")
				case "twinsword":
					add("twinswd")
				case "chakram":
					add("chakraweapon")
				}
			}
		case "cloth", "leather", "larmor", "harmor", "plate":
			prefix := map[string]string{"cloth": "cl", "leather": "lt", "larmor": "la", "harmor": "ha", "plate": "mt"}[segment]
			// Current PVF armor paths use both material/slot and slot/material
			// layouts. The live failed rows are the latter, for example
			// common/shoes/leather/100260138.equ. Derive the canonical
			// random-option key from [equipment type] first, then retain both
			// adjacent path orders for older resources.
			add(prefix + normalizedEquipmentType)
			if index > 0 && !strings.HasSuffix(segments[index-1], ".equ") {
				add(prefix + normalizeCurrentRandomOptionKey(segments[index-1]))
			}
			if index+1 < len(segments) {
				if !strings.HasSuffix(segments[index+1], ".equ") {
					add(prefix + normalizeCurrentRandomOptionKey(segments[index+1]))
				}
			}
		}
	}
	out := make([]string, 0, len(keys))
	for key := range keys {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func currentRandomOptionSelectedGroups(entries map[string][3]int64, keys []string) ([3]int64, string, bool) {
	for _, key := range keys {
		if groups, ok := entries[key]; ok {
			return groups, key, true
		}
	}
	return [3]int64{}, "", false
}

type currentRandomOptionValue struct {
	text      string
	intValue  int64
	isInteger bool
}

type currentRandomOptionNumber struct {
	value float64
}

func (v currentRandomOptionValue) integer() (int64, bool) { return v.intValue, v.isInteger }

func currentRandomOptionSectionValues(document *dnfpvf.Document, section dnfpvf.Section) []currentRandomOptionValue {
	if document == nil || section.Start < 0 || section.Start > section.End || section.End > len(document.Tokens) {
		return nil
	}
	values := make([]currentRandomOptionValue, 0, section.End-section.Start)
	for _, token := range document.Tokens[section.Start:section.End] {
		switch token.Kind {
		case dnfpvf.TokenInt:
			values = append(values, currentRandomOptionValue{text: token.Value, intValue: token.Int, isInteger: true})
		case dnfpvf.TokenString, dnfpvf.TokenIdent:
			values = append(values, currentRandomOptionValue{text: token.Value})
		}
	}
	return values
}

func currentRandomOptionSectionInts(document *dnfpvf.Document, section dnfpvf.Section) []int64 {
	values := currentRandomOptionSectionValues(document, section)
	ints := make([]int64, 0, len(values))
	for _, value := range values {
		if integer, ok := value.integer(); ok {
			ints = append(ints, integer)
		}
	}
	return ints
}

func currentRandomOptionSectionNumbers(document *dnfpvf.Document, section dnfpvf.Section) []currentRandomOptionNumber {
	if document == nil || section.Start < 0 || section.Start > section.End || section.End > len(document.Tokens) {
		return nil
	}
	values := make([]currentRandomOptionNumber, 0, section.End-section.Start)
	for _, token := range document.Tokens[section.Start:section.End] {
		switch token.Kind {
		case dnfpvf.TokenInt:
			values = append(values, currentRandomOptionNumber{value: float64(token.Int)})
		case dnfpvf.TokenFloat:
			values = append(values, currentRandomOptionNumber{value: token.Float})
		}
	}
	return values
}

func currentRandomOptionSectionKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeCurrentRandomOptionKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.Trim(value, "`[](){}<> ")
	value = strings.ReplaceAll(value, "_", "")
	value = strings.ReplaceAll(value, "-", "")
	value = strings.ReplaceAll(value, " ", "")
	return value
}
