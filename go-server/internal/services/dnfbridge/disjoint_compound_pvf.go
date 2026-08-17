package dnfbridge

import (
	cryptorand "crypto/rand"
	"math"
	"math/big"
	"strconv"
	"strings"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

func loadCurrentDisjointPVFConfig(source dnfpvf.Source) (currentDisjointPVFConfig, error) {
	if source == nil {
		return currentDisjointPVFConfig{}, errCurrentDisjointUnavailable
	}
	disjoint, err := parseDungeonCardPVFDocument(source, "etc/disjoint.etc")
	if err != nil {
		return currentDisjointPVFConfig{}, err
	}
	config := currentDisjointPVFConfig{CubeBase: 150, AvatarByJob: make(map[string][]uint32), EmblemBoosters: make(map[[2]int]uint32)}
	for _, section := range disjoint.Sections {
		if !strings.EqualFold(strings.TrimSpace(section.Name), "cube index") {
			continue
		}
		tokens := currentDisjointSectionTokens(disjoint, section)
		if len(tokens) == 0 || !strings.EqualFold(strings.TrimSpace(strings.Trim(tokens[0].Value, "`")), "[no element]") {
			continue
		}
		for _, token := range tokens[1:] {
			if token.Kind == dnfpvf.TokenInt && token.Int > 0 && token.Int <= math.MaxUint32 {
				config.CubeItemID = uint32(token.Int)
				break
			}
		}
	}
	values := disjoint.Numbers("cube creation const")
	if len(values) > 0 && values[0] > 0 {
		config.CubeBase = values[0]
		config.CubeMultipliers = append(config.CubeMultipliers, values[1:]...)
	}
	config.Additional = currentDisjointParseAdditional(disjoint.Ints("additional result"))
	config.AdditionalConsts = currentDisjointParseAdditionalConsts(disjoint.Numbers("additional result const"), len(config.Additional))
	config.Expands = currentDisjointParseExpands(disjoint.Ints("additional result expand"), disjoint.Numbers("additional result expand const"))
	config.AvatarByJob = currentDisjointParseAvatarByJob(disjoint)
	config.AvatarEmblemTables = currentDisjointLoadAvatarEmblemTables(source)

	emblem, err := parseDungeonCardPVFDocument(source, "etc/emblemcompound.etc")
	if err != nil {
		return currentDisjointPVFConfig{}, err
	}
	config.EmblemBoosters = currentDisjointParseEmblemBoosters(emblem.Ints("emblem compound info"))
	if config.CubeItemID == 0 || len(config.CubeMultipliers) == 0 || len(config.AvatarByJob) == 0 || len(config.EmblemBoosters) == 0 {
		return currentDisjointPVFConfig{}, errCurrentDisjointRewardInvalid
	}
	return config, nil
}

func currentDisjointSectionTokens(document *dnfpvf.Document, section dnfpvf.Section) []dnfpvf.Token {
	if document == nil || section.Start < 0 || section.End > len(document.Tokens) || section.Start > section.End {
		return nil
	}
	return document.Tokens[section.Start:section.End]
}

func currentDisjointParseAdditional(values []int64) [][]uint32 {
	out := make([][]uint32, 0)
	for offset := 0; offset < len(values); {
		count := int(values[offset])
		offset++
		if count < 0 || offset+count > len(values) {
			return nil
		}
		row := make([]uint32, 0, count)
		for _, value := range values[offset : offset+count] {
			if value > 0 && value <= math.MaxUint32 {
				row = append(row, uint32(value))
			}
		}
		offset += count
		out = append(out, row)
	}
	return out
}

func currentDisjointParseAdditionalConsts(values []float64, rows int) []currentDisjointAdditionalConst {
	out := make([]currentDisjointAdditionalConst, 0, rows)
	offset := 0
	for row := 0; row < rows && offset < len(values); row++ {
		remaining := rows - row - 1
		if len(values)-offset == 1+remaining*4 {
			out = append(out, currentDisjointAdditionalConst{GreatDivisor: values[offset]})
			offset++
			continue
		}
		if offset+3 >= len(values) {
			return nil
		}
		out = append(out, currentDisjointAdditionalConst{GreatDivisor: values[offset], NormalDivisor: values[offset+1], GreatChance: values[offset+2]})
		offset += 4
	}
	return out
}

func currentDisjointParseExpands(ids []int64, values []float64) []currentDisjointExpand {
	out := make([]currentDisjointExpand, 0, len(ids)/2)
	for index := 0; index+1 < len(ids); index += 2 {
		itemID := uint32(0)
		if ids[index+1] > 0 && ids[index+1] <= math.MaxUint32 {
			itemID = uint32(ids[index+1])
		}
		out = append(out, currentDisjointExpand{Enabled: ids[index] > 0 && itemID > 0, ItemID: itemID})
	}
	offset := 0
	for index := range out {
		remaining := len(out) - index - 1
		if !out[index].Enabled && out[index].ItemID == 0 && offset+1 < len(values) && len(values)-(offset+2) == remaining*3 {
			out[index].LevelDivisor, out[index].GreatChance = values[offset], values[offset+1]
			offset += 2
			continue
		}
		if offset+2 >= len(values) {
			break
		}
		out[index].LevelDivisor, out[index].GreatChance, out[index].NormalChance = values[offset], values[offset+1], values[offset+2]
		offset += 3
	}
	return out
}

// currentDisjointLoadAvatarEmblemTables reads the per-grade avatar disjoint
// reward tables etc/avatardisjoint/emblemlistinfo_<grade>.etc until the first
// missing or unusable file. Missing tables are not an error here; only the
// avatar disjoint reward path requires them.
func currentDisjointLoadAvatarEmblemTables(source dnfpvf.Source) []currentAvatarEmblemGradeTable {
	tables := make([]currentAvatarEmblemGradeTable, 0, 6)
	for grade := 0; grade < 32; grade++ {
		document, err := parseDungeonCardPVFDocument(source, "etc/avatardisjoint/emblemlistinfo_"+strconv.Itoa(grade)+".etc")
		if err != nil {
			break
		}
		table := currentDisjointParseAvatarEmblemTable(document)
		if len(table.Pools) == 0 {
			break
		}
		tables = append(tables, table)
	}
	return tables
}

func currentDisjointParseAvatarEmblemTable(document *dnfpvf.Document) currentAvatarEmblemGradeTable {
	table := currentAvatarEmblemGradeTable{}
	if document == nil {
		return table
	}
	for _, section := range document.Sections {
		if !strings.EqualFold(strings.TrimSpace(section.Name), "result info") {
			continue
		}
		values := currentDisjointTokensInts(currentDisjointSectionTokens(document, section))
		if len(values) < 5 || values[0] <= 0 {
			continue
		}
		pool := currentAvatarEmblemPool{PickCount: int(values[0])}
		for offset := 1; offset+3 < len(values); offset += 4 {
			item, weight, count := values[offset], values[offset+1], values[offset+2]
			if item <= 0 || item > math.MaxUint32 || weight <= 0 || weight > math.MaxUint32 || count <= 0 || count > math.MaxUint32 {
				continue
			}
			pool.Rewards = append(pool.Rewards, currentAvatarEmblemReward{
				ItemID:  uint32(item),
				Weight:  uint32(weight),
				Count:   uint32(count),
				Special: values[offset+3] != 0,
			})
		}
		if len(pool.Rewards) > 0 {
			table.Pools = append(table.Pools, pool)
		}
	}
	return table
}

func currentDisjointParseAvatarByJob(document *dnfpvf.Document) map[string][]uint32 {
	out := make(map[string][]uint32)
	active := false
	for _, section := range document.Sections {
		name := strings.ToLower(strings.TrimSpace(section.Name))
		switch name {
		case "avatar disjoint info":
			active = true
			continue
		case "/avatar disjoint info":
			active = false
			continue
		}
		if !active || strings.HasPrefix(name, "/") {
			continue
		}
		values := make([]uint32, 0, 15)
		for _, token := range currentDisjointSectionTokens(document, section) {
			if token.Kind == dnfpvf.TokenInt && token.Int > 0 && token.Int <= math.MaxUint32 {
				values = append(values, uint32(token.Int))
			}
		}
		if len(values) >= 15 {
			out[currentDisjointNormalizeJob(name)] = append([]uint32(nil), values[:15]...)
		}
	}
	return out
}

func currentDisjointParseEmblemBoosters(values []int64) map[[2]int]uint32 {
	out := make(map[[2]int]uint32)
	for offset := 0; offset+1 < len(values); {
		grade, maxCount := int(values[offset]), int(values[offset+1])
		offset += 2
		if grade <= 0 || maxCount < 2 || offset+maxCount-1 > len(values) {
			return map[[2]int]uint32{}
		}
		for inputCount := 2; inputCount <= maxCount; inputCount++ {
			value := values[offset]
			offset++
			if value > 0 && value <= math.MaxUint32 {
				out[[2]int{grade, inputCount}] = uint32(value)
			}
		}
	}
	return out
}

func currentEquipmentDisjointRewards(config currentDisjointPVFConfig, document *dnfpvf.Document) ([]currentDisjointReward, error) {
	if document == nil || config.CubeItemID == 0 || config.CubeBase <= 0 {
		return nil, errCurrentDisjointRewardInvalid
	}
	rarity, found := document.Int("rarity")
	if !found || rarity < 0 || rarity >= int64(len(config.CubeMultipliers)) {
		return nil, errCurrentDisjointRewardInvalid
	}
	minimumLevel, levelFound := document.Int("minimum level")
	if !levelFound || minimumLevel < 1 {
		minimumLevel = 1
	}
	value, valueFound := document.Int("value")
	if !valueFound || value <= 0 {
		value, valueFound = document.Int("price")
	}
	if !valueFound || value <= 0 {
		return nil, errCurrentDisjointRewardInvalid
	}
	cubeCount := uint32(math.Max(1, math.Floor(float64(value)*config.CubeMultipliers[rarity]/config.CubeBase)))
	rewards := []currentDisjointReward{{ItemID: config.CubeItemID, Count: cubeCount}}
	if int(rarity) < len(config.Additional) && len(config.Additional[rarity]) > 0 {
		index, err := currentDisjointRandomIndex(len(config.Additional[rarity]))
		if err != nil {
			return nil, err
		}
		count, err := currentDisjointAdditionalCount(config, int(rarity), int(minimumLevel))
		if err != nil {
			return nil, err
		}
		currentDisjointMergeReward(&rewards, currentDisjointReward{ItemID: config.Additional[rarity][index], Count: count})
	}
	expandIndex := int(rarity)
	if rarity == 6 {
		expandIndex = 5
	}
	if expandIndex >= 0 && expandIndex < len(config.Expands) {
		expand := config.Expands[expandIndex]
		if expand.Enabled && expand.ItemID > 0 {
			roll, err := currentDisjointRandomPercent()
			if err != nil {
				return nil, err
			}
			if roll < currentDisjointClampPercent(expand.NormalChance) {
				count, countErr := currentDisjointLevelDivisorCount(int(minimumLevel), expand.LevelDivisor)
				if countErr != nil {
					return nil, countErr
				}
				greatRoll, rollErr := currentDisjointRandomPercent()
				if rollErr != nil {
					return nil, rollErr
				}
				if greatRoll < currentDisjointClampPercent(expand.GreatChance) {
					count++
				}
				currentDisjointMergeReward(&rewards, currentDisjointReward{ItemID: expand.ItemID, Count: count})
			}
		}
	}
	return rewards, nil
}

// currentAvatarDisjointRewards resolves the avatar's PVF [grade] into the
// matching etc/avatardisjoint grade table and rolls every pool's weighted
// picks (86JP AvatarDisjointConfigProvider.Calculate), merging duplicate
// items. The rewards are real emblem items, not the old fixed booster boxes.
func currentAvatarDisjointRewards(config currentDisjointPVFConfig, document *dnfpvf.Document) ([]currentDisjointReward, error) {
	if document == nil {
		return nil, errCurrentDisjointRewardInvalid
	}
	grade, gradeFound := document.Int("grade")
	equipmentType, typeFound := document.Text("equipment type")
	rule, supported := currentEquipmentPlacementRuleForPVFType(equipmentType)
	if !gradeFound || !typeFound || !supported || rule.class != currentEquipmentPlacementClassAvatar {
		return nil, errCurrentDisjointRewardInvalid
	}
	if grade < 0 || grade >= int64(len(config.AvatarEmblemTables)) {
		return nil, errCurrentDisjointRewardInvalid
	}
	rewards := make([]currentDisjointReward, 0, 4)
	for _, pool := range config.AvatarEmblemTables[int(grade)].Pools {
		for pick := 0; pick < pool.PickCount; pick++ {
			reward, err := currentDisjointRollAvatarEmblem(pool.Rewards)
			if err != nil {
				return nil, err
			}
			merged := false
			for index := range rewards {
				if rewards[index].ItemID == reward.ItemID {
					rewards[index].Count += reward.Count
					merged = true
					break
				}
			}
			if !merged {
				rewards = append(rewards, reward)
			}
		}
	}
	if len(rewards) == 0 {
		return nil, errCurrentDisjointRewardInvalid
	}
	return rewards, nil
}

func currentDisjointRollAvatarEmblem(candidates []currentAvatarEmblemReward) (currentDisjointReward, error) {
	var total uint64
	for _, candidate := range candidates {
		total += uint64(candidate.Weight)
	}
	if total == 0 {
		return currentDisjointReward{}, errCurrentDisjointRewardInvalid
	}
	roll, err := currentDisjointRandomUint64(total)
	if err != nil {
		return currentDisjointReward{}, err
	}
	for _, candidate := range candidates {
		if roll < uint64(candidate.Weight) {
			return currentDisjointReward{ItemID: candidate.ItemID, Count: candidate.Count}, nil
		}
		roll -= uint64(candidate.Weight)
	}
	return currentDisjointReward{}, errCurrentDisjointRewardInvalid
}

func currentDisjointDocumentIsAvatar(document *dnfpvf.Document) bool {
	if document == nil {
		return false
	}
	equipmentType, found := document.Text("equipment type")
	if !found {
		return false
	}
	rule, supported := currentEquipmentPlacementRuleForPVFType(equipmentType)
	return supported && rule.class == currentEquipmentPlacementClassAvatar
}

func currentRollEmblemBoosterReward(catalog *pvfDungeonDropCatalog, boosterID uint32) (currentDisjointReward, error) {
	definition, err := catalog.ResolveItem(boosterID)
	if err != nil || definition.Kind != dungeonDropItemStackable {
		return currentDisjointReward{}, errCurrentDisjointRewardInvalid
	}
	document, err := parseDungeonCardPVFDocument(catalog.source, definition.PVFPath)
	if err != nil || !strings.HasPrefix(strings.ToLower(strings.TrimSpace(documentText(document, "stackable type"))), "[booster]") {
		return currentDisjointReward{}, errCurrentDisjointRewardInvalid
	}
	active := false
	for _, section := range document.Sections {
		name := strings.ToLower(strings.TrimSpace(section.Name))
		switch name {
		case "booster info":
			active = true
			continue
		case "/booster info":
			return currentDisjointReward{}, errCurrentDisjointRewardInvalid
		}
		if !active || name != "stackable" {
			continue
		}
		return currentRollEmblemBoosterPool(currentDisjointTokensInts(currentDisjointSectionTokens(document, section)))
	}
	return currentDisjointReward{}, errCurrentDisjointRewardInvalid
}

func currentRollEmblemBoosterPool(values []int64) (currentDisjointReward, error) {
	start := 0
	if len(values) >= 4 && (len(values)-1)%3 == 0 {
		start = 1 // draw-count; op256 supports one popup result only.
	}
	if (len(values)-start)%3 != 0 || len(values) == start {
		return currentDisjointReward{}, errCurrentDisjointRewardInvalid
	}
	type candidate struct{ itemID, weight, count uint32 }
	candidates := make([]candidate, 0, (len(values)-start)/3)
	var total uint64
	for offset := start; offset+2 < len(values); offset += 3 {
		if values[offset] <= 0 || values[offset] > math.MaxUint32 || values[offset+1] <= 0 || values[offset+1] > math.MaxUint32 || values[offset+2] <= 0 || values[offset+2] > math.MaxUint32 {
			continue
		}
		entry := candidate{uint32(values[offset]), uint32(values[offset+1]), uint32(values[offset+2])}
		total += uint64(entry.weight)
		candidates = append(candidates, entry)
	}
	if total == 0 || len(candidates) == 0 {
		return currentDisjointReward{}, errCurrentDisjointRewardInvalid
	}
	roll, err := currentDisjointRandomUint64(total)
	if err != nil {
		return currentDisjointReward{}, err
	}
	for _, candidate := range candidates {
		if roll < uint64(candidate.weight) {
			return currentDisjointReward{ItemID: candidate.itemID, Count: candidate.count}, nil
		}
		roll -= uint64(candidate.weight)
	}
	return currentDisjointReward{}, errCurrentDisjointRewardInvalid
}

func documentText(document *dnfpvf.Document, name string) string {
	if document == nil {
		return ""
	}
	value, _ := document.Text(name)
	return value
}

func currentDisjointTokensInts(tokens []dnfpvf.Token) []int64 {
	values := make([]int64, 0, len(tokens))
	for _, token := range tokens {
		if token.Kind == dnfpvf.TokenInt {
			values = append(values, token.Int)
		}
	}
	return values
}

func currentDisjointAdditionalCount(config currentDisjointPVFConfig, rarity, level int) (uint32, error) {
	if rarity < 0 || rarity >= len(config.AdditionalConsts) {
		return 1, nil
	}
	consts := config.AdditionalConsts[rarity]
	divisor := consts.NormalDivisor
	roll, err := currentDisjointRandomPercent()
	if err != nil {
		return 0, err
	}
	if roll < currentDisjointClampPercent(consts.GreatChance) && consts.GreatDivisor > 0 {
		divisor = consts.GreatDivisor
	}
	return currentDisjointLevelDivisorCount(level, divisor)
}

func currentDisjointLevelDivisorCount(level int, divisor float64) (uint32, error) {
	if divisor <= 0 {
		return 1, nil
	}
	value := float64(max(1, level)) / divisor
	count := math.Floor(value)
	roll, err := currentDisjointRandomPercent()
	if err != nil {
		return 0, err
	}
	if roll/100 < value-count {
		count++
	}
	if count > math.MaxUint32 {
		return 0, errCurrentDisjointRewardInvalid
	}
	// 86JP DisjointResultCalculator.CalculateLevelDivisorCount clamps the
	// rolled count to at least 1; a low-level source still yields one
	// additional/expand item instead of failing the whole disjoint.
	if count < 1 {
		count = 1
	}
	return uint32(count), nil
}

func currentDisjointMergeReward(rewards *[]currentDisjointReward, reward currentDisjointReward) {
	if rewards == nil || reward.ItemID == 0 || reward.Count == 0 {
		return
	}
	for index := range *rewards {
		if (*rewards)[index].ItemID == reward.ItemID {
			(*rewards)[index].Count += reward.Count
			return
		}
	}
	*rewards = append(*rewards, reward)
}

func currentDisjointNormalizeJob(value string) string {
	value = strings.ToLower(strings.TrimSpace(strings.Trim(value, "`")))
	return strings.TrimSpace(strings.Trim(value, "[]"))
}

func currentDisjointClampPercent(value float64) float64 {
	return math.Max(0, math.Min(100, value))
}

func currentDisjointRandomIndex(limit int) (int, error) {
	if limit <= 0 {
		return 0, errCurrentDisjointRewardInvalid
	}
	value, err := cryptorand.Int(cryptorand.Reader, big.NewInt(int64(limit)))
	if err != nil {
		return 0, err
	}
	return int(value.Int64()), nil
}

func currentDisjointRandomUint64(limit uint64) (uint64, error) {
	if limit == 0 || limit > math.MaxInt64 {
		return 0, errCurrentDisjointRewardInvalid
	}
	value, err := cryptorand.Int(cryptorand.Reader, big.NewInt(int64(limit)))
	if err != nil {
		return 0, err
	}
	return uint64(value.Int64()), nil
}

func currentDisjointRandomPercent() (float64, error) {
	value, err := currentDisjointRandomUint64(1_000_000)
	if err != nil {
		return 0, err
	}
	return float64(value) / 10_000, nil
}
