package dnfbridge

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

const currentAmplifyItemPVFPath = "etc/amplifyitem.etc"

type currentAmplifyMaterialOption struct {
	Option byte
	Count  int64
}

type currentAmplifyItemConfig struct {
	EquipLevelConst int64
	OptionMapping   map[string]byte
	OptionBaseValue map[byte]float64
	RarityWeight    map[int64]float64
	Purify          map[int64]int64
	Clear           map[int64]int64
	Invest          map[int64]currentAmplifyMaterialOption
	Reinvest        map[int64]currentAmplifyMaterialOption
	PureGold        map[int64]currentAmplifyMaterialOption
}

func (s *Service) alignedAmplifyItemResolverForCommand(opcode dnfenum.CmdPacket) (alignedcmd.AmplifyItemResolver, error) {
	if opcode != dnfenum.CmdPacketPurifyItem && opcode != dnfenum.CmdPacketInvestItemAmplifyOption {
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
	config, err := currentAmplifyItemConfigFromSource(source)
	if err != nil {
		return nil, err
	}
	return func(materialItemID int64, targetItemID int64) (alignedcmd.AmplifyItemResolution, error) {
		return resolveCurrentAmplifyItemMetadata(catalog, source, config, materialItemID, targetItemID)
	}, nil
}

func currentAmplifyItemConfigFromSource(source dnfpvf.Source) (*currentAmplifyItemConfig, error) {
	if source == nil {
		return nil, dnfpvf.ErrSourceRequired
	}
	text, err := source.ReadText(currentAmplifyItemPVFPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", currentAmplifyItemPVFPath, err)
	}
	document, err := dnfpvf.Parse(currentAmplifyItemPVFPath, text)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", currentAmplifyItemPVFPath, err)
	}
	config, err := parseCurrentAmplifyItemConfig(document)
	if err != nil {
		return nil, fmt.Errorf("parse %s config: %w", currentAmplifyItemPVFPath, err)
	}
	return config, nil
}

func parseCurrentAmplifyItemConfig(document *dnfpvf.Document) (*currentAmplifyItemConfig, error) {
	if document == nil {
		return nil, fmt.Errorf("document required")
	}
	config := &currentAmplifyItemConfig{
		OptionMapping:   make(map[string]byte),
		OptionBaseValue: make(map[byte]float64),
		RarityWeight:    make(map[int64]float64),
		Purify:          make(map[int64]int64),
		Clear:           make(map[int64]int64),
		Invest:          make(map[int64]currentAmplifyMaterialOption),
		Reinvest:        make(map[int64]currentAmplifyMaterialOption),
		PureGold:        make(map[int64]currentAmplifyMaterialOption),
	}
	for _, section := range document.Sections {
		if sectionKey648(section.Name) != "option mapping table" {
			continue
		}
		tokens := currentAmplifySectionValues(document, section)
		for index := 0; index+1 < len(tokens); index += 2 {
			value, ok := tokens[index+1].integer()
			if !ok || value < 0 || value > 5 {
				continue
			}
			config.OptionMapping[normalizeAmplifyOptionName(tokens[index].text)] = byte(value)
		}
	}
	for _, section := range document.Sections {
		tokens := currentAmplifySectionValues(document, section)
		switch sectionKey648(section.Name) {
		case "option data":
			for index := 0; index+2 < len(tokens); index += 3 {
				option := config.OptionMapping[normalizeAmplifyOptionName(tokens[index].text)]
				base, ok := tokens[index+2].number()
				if option >= 1 && option <= 4 && ok && base > 0 {
					config.OptionBaseValue[option] = base
				}
			}
		case "rarity weight":
			for index := 0; index+1 < len(tokens); index += 2 {
				weight, ok := tokens[index+1].number()
				rarity, known := currentAmplifyRarityIndex(tokens[index].text)
				if known && ok && weight > 0 {
					config.RarityWeight[rarity] = weight
				}
			}
		case "equip level const":
			if len(tokens) > 0 {
				if value, ok := tokens[0].integer(); ok && value > 0 {
					config.EquipLevelConst = value
				}
			}
		case "purify material":
			parseCurrentAmplifyItemCounts(tokens, config.Purify)
		case "purify only material", "purify only cera material":
			parseCurrentAmplifyItemCounts(tokens, config.Clear)
		case "invest option":
			parseCurrentAmplifyMaterialOptions(tokens, config.OptionMapping, config.Invest)
		case "reinvest option":
			parseCurrentAmplifyMaterialOptions(tokens, config.OptionMapping, config.Reinvest)
		case "random invest upgrade option":
			parseCurrentAmplifyMaterialOptions(tokens, config.OptionMapping, config.PureGold)
		}
	}
	if config.EquipLevelConst <= 0 {
		return nil, fmt.Errorf("missing positive [equip level const]")
	}
	for option := byte(1); option <= 4; option++ {
		if config.OptionBaseValue[option] <= 0 {
			return nil, fmt.Errorf("missing positive [option data] base for option %d", option)
		}
	}
	for rarity := int64(0); rarity <= 6; rarity++ {
		if config.RarityWeight[rarity] <= 0 {
			return nil, fmt.Errorf("missing positive [rarity weight] for rarity %d", rarity)
		}
	}
	if len(config.Purify)+len(config.Clear)+len(config.Invest)+len(config.Reinvest)+len(config.PureGold) == 0 {
		return nil, fmt.Errorf("no amplification materials")
	}
	return config, nil
}

func resolveCurrentAmplifyItemMetadata(catalog *pvfDungeonDropCatalog, source dnfpvf.Source, config *currentAmplifyItemConfig, materialItemID int64, targetItemID int64) (alignedcmd.AmplifyItemResolution, error) {
	if catalog == nil || source == nil || config == nil {
		return alignedcmd.AmplifyItemResolution{}, errDungeonDropSourceRequired
	}
	resolution := alignedcmd.AmplifyItemResolution{EquipLevelConst: config.EquipLevelConst}
	if materialItemID <= 0 || materialItemID > math.MaxUint32 || targetItemID <= 0 || targetItemID > math.MaxUint32 {
		return resolution, nil
	}
	targetDefinition, err := catalog.ResolveItem(uint32(targetItemID))
	if err != nil {
		if errors.Is(err, errDungeonDropItemUnresolved) {
			return resolution, nil
		}
		return alignedcmd.AmplifyItemResolution{}, fmt.Errorf("resolve amplification target item=%d: %w", targetItemID, err)
	}
	resolution.TargetKind = string(targetDefinition.Kind)
	resolution.TargetPVFPath = targetDefinition.PVFPath
	if targetDefinition.Kind == dungeonDropItemEquipment {
		targetDocument, err := parseDungeonCardPVFDocument(source, targetDefinition.PVFPath)
		if err != nil {
			return alignedcmd.AmplifyItemResolution{}, fmt.Errorf("parse amplification target item=%d path=%s: %w", targetItemID, targetDefinition.PVFPath, err)
		}
		minimumLevel, minimumFound := targetDocument.Int("minimum level")
		rarity, rarityFound := targetDocument.Int("rarity")
		if !minimumFound || !rarityFound {
			return alignedcmd.AmplifyItemResolution{}, fmt.Errorf("amplification target item=%d path=%s missing minimum level or rarity", targetItemID, targetDefinition.PVFPath)
		}
		resolution.TargetMinimumLevel = minimumLevel
		resolution.TargetRarity = rarity
		initialValues, err := currentAmplifyInitialValues(config, rarity)
		if err != nil {
			return alignedcmd.AmplifyItemResolution{}, fmt.Errorf("amplification target item=%d path=%s: %w", targetItemID, targetDefinition.PVFPath, err)
		}
		resolution.InitialValues = initialValues
	}

	materialDefinition, err := catalog.ResolveItem(uint32(materialItemID))
	if err != nil {
		if errors.Is(err, errDungeonDropItemUnresolved) {
			return resolution, nil
		}
		return alignedcmd.AmplifyItemResolution{}, fmt.Errorf("resolve amplification material item=%d: %w", materialItemID, err)
	}
	if materialDefinition.Kind != dungeonDropItemStackable {
		return resolution, nil
	}
	resolution.MaterialPVFPath = materialDefinition.PVFPath
	resolution.PurifyMaterialCount = config.Purify[materialItemID]
	resolution.ClearMaterialCount = config.Clear[materialItemID]
	if option, ok := config.Invest[materialItemID]; ok {
		resolution.InvestOption = option.Option
		resolution.InvestMaterialCount = option.Count
	}
	if option, ok := config.Reinvest[materialItemID]; ok {
		resolution.ReinvestOption = option.Option
		resolution.ReinvestMaterialCount = option.Count
	}
	if option, ok := config.PureGold[materialItemID]; ok {
		resolution.PureGoldOption = option.Option
		resolution.PureGoldMaterialCount = option.Count
		materialDocument, err := parseDungeonCardPVFDocument(source, materialDefinition.PVFPath)
		if err != nil {
			return alignedcmd.AmplifyItemResolution{}, fmt.Errorf("parse Pure Gold material item=%d path=%s: %w", materialItemID, materialDefinition.PVFPath, err)
		}
		values := materialDocument.Ints("amplification random value")
		for index := 0; index+1 < len(values); index += 2 {
			level, weight := values[index], values[index+1]
			if level < 0 || level > 31 || weight <= 0 {
				return alignedcmd.AmplifyItemResolution{}, fmt.Errorf("Pure Gold material item=%d path=%s invalid random pair level=%d weight=%d", materialItemID, materialDefinition.PVFPath, level, weight)
			}
			resolution.PureGoldLevels = append(resolution.PureGoldLevels, alignedcmd.AmplifyWeightedLevel{Level: byte(level), Weight: weight})
		}
		if len(values)%2 != 0 || len(resolution.PureGoldLevels) == 0 {
			return alignedcmd.AmplifyItemResolution{}, fmt.Errorf("Pure Gold material item=%d path=%s missing complete [amplification random value] pairs", materialItemID, materialDefinition.PVFPath)
		}
	}
	return resolution, nil
}

func currentAmplifyInitialValues(config *currentAmplifyItemConfig, rarity int64) (map[byte]uint16, error) {
	weight := config.RarityWeight[rarity]
	if weight <= 0 {
		return nil, fmt.Errorf("rarity %d has no positive weight", rarity)
	}
	values := make(map[byte]uint16, 4)
	for option := byte(1); option <= 4; option++ {
		value := int64(weight * config.OptionBaseValue[option])
		if value <= 0 || value > math.MaxUint16 {
			return nil, fmt.Errorf("option %d initial value %d invalid", option, value)
		}
		values[option] = uint16(value)
	}
	return values, nil
}

type currentAmplifyValue struct {
	text       string
	numeric    float64
	isNumeric  bool
	isInteger  bool
	integerVal int64
}

func (v currentAmplifyValue) number() (float64, bool) { return v.numeric, v.isNumeric }
func (v currentAmplifyValue) integer() (int64, bool)  { return v.integerVal, v.isInteger }

func currentAmplifySectionValues(document *dnfpvf.Document, section dnfpvf.Section) []currentAmplifyValue {
	if section.Start < 0 || section.End > len(document.Tokens) || section.Start > section.End {
		return nil
	}
	values := make([]currentAmplifyValue, 0, section.End-section.Start)
	for _, token := range document.Tokens[section.Start:section.End] {
		switch token.Kind {
		case dnfpvf.TokenString, dnfpvf.TokenIdent:
			values = append(values, currentAmplifyValue{text: token.Value})
		case dnfpvf.TokenInt:
			values = append(values, currentAmplifyValue{text: token.Value, numeric: float64(token.Int), isNumeric: true, isInteger: true, integerVal: token.Int})
		case dnfpvf.TokenFloat:
			values = append(values, currentAmplifyValue{text: token.Value, numeric: token.Float, isNumeric: true})
		}
	}
	return values
}

func parseCurrentAmplifyItemCounts(tokens []currentAmplifyValue, target map[int64]int64) {
	for index := 0; index+1 < len(tokens); index += 2 {
		itemID, itemOK := tokens[index].integer()
		count, countOK := tokens[index+1].integer()
		if itemOK && countOK && itemID > 0 && itemID <= math.MaxUint32 && count > 0 {
			target[itemID] = count
		}
	}
}

func parseCurrentAmplifyMaterialOptions(tokens []currentAmplifyValue, mapping map[string]byte, target map[int64]currentAmplifyMaterialOption) {
	for index := 0; index+2 < len(tokens); index += 3 {
		option := mapping[normalizeAmplifyOptionName(tokens[index].text)]
		itemID, itemOK := tokens[index+1].integer()
		count, countOK := tokens[index+2].integer()
		if option >= 1 && option <= 5 && itemOK && countOK && itemID > 0 && itemID <= math.MaxUint32 && count > 0 {
			target[itemID] = currentAmplifyMaterialOption{Option: option, Count: count}
		}
	}
}

func normalizeAmplifyOptionName(value string) string {
	return strings.ToLower(strings.TrimSpace(strings.Trim(value, "`")))
}

func sectionKey648(value string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(value, "/")))
}

func currentAmplifyRarityIndex(value string) (int64, bool) {
	switch strings.ToLower(strings.TrimSpace(strings.Trim(value, "`"))) {
	case "common":
		return 0, true
	case "uncommon":
		return 1, true
	case "rare":
		return 2, true
	case "unique":
		return 3, true
	case "epic":
		return 4, true
	case "chronicle":
		return 5, true
	case "legendary":
		return 6, true
	default:
		return 0, false
	}
}
