package expertjob

import (
	"errors"
	"math"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

type disjointerResultKey struct {
	MachineGrade int
	Rarity       int
	State        int
}

type DisjointerResultRule struct {
	ItemID              int64
	Multiplier          float64
	AdditionalTable     int
	BigWinTable         int
	BigWinChancePercent int
}

type DisjointerSelectionRule struct {
	MinimumGrade int
	MaximumGrade int
	ItemID       int64
	Weight       int
	CountDivisor float64
}

type DisjointerConfig struct {
	InitialEndurance     int64
	MaximumStoreCharge   int64
	GiveUpCosts          []int64
	BaseConst            float64
	EnduranceReduceMin   int
	EnduranceReduceMax   int
	ExperienceGainMin    int
	ExperienceGainMax    int
	SelfServiceChance    int
	SelfServiceItemID    int64
	SelfServiceItemCount int64
	Results              map[disjointerResultKey]DisjointerResultRule
	AdditionalResults    map[int][]DisjointerSelectionRule
	BigWinResults        map[int][]DisjointerSelectionRule
	RepairRules          []RepairRule
	ExperienceThresholds []int64
	UpgradeCosts         map[int]int64
	CharacterLevelLimits map[int]int
}

func (c *DisjointerConfig) Level(experience int64) int {
	if c == nil || len(c.ExperienceThresholds) == 0 {
		return 0
	}
	level := 1
	for _, threshold := range c.ExperienceThresholds {
		if experience < threshold {
			break
		}
		level++
	}
	return min(level, len(c.ExperienceThresholds))
}

func (c *DisjointerConfig) RepairRule(grade int) (RepairRule, bool) {
	if c == nil || grade <= 0 || grade > len(c.RepairRules) {
		return RepairRule{}, false
	}
	return c.RepairRules[grade-1], true
}

type DisjointerPlan struct {
	Materials          []RecipeEntry
	EnduranceReduction int64
	ExperienceGain     int64
	FinalExperience    int64
	LevelChanged       bool
}

func (c *Catalog) PlanDisjointer(experience int64, machineGrade int, equipment EquipmentMetadata, selfService bool, rng RandomFunc) (DisjointerPlan, error) {
	config, ok := c.Disjointer()
	if !ok || machineGrade <= 0 {
		return DisjointerPlan{}, ErrMachineInvalid
	}
	rule, found := config.Results[disjointerResultKey{machineGrade - 1, equipment.Rarity, equipment.State}]
	if !found && equipment.State == 1 {
		return DisjointerPlan{}, ErrMachineGradeTooLow
	}
	if !found || rule.ItemID <= 0 || rule.Multiplier <= 0 || config.BaseConst <= 0 {
		return DisjointerPlan{}, ErrExtractionInvalid
	}
	random := randomSource(RandomFunc(rng))
	equipmentGrade := equipment.Grade
	if equipmentGrade <= 0 {
		equipmentGrade = equipment.Level
	}
	adjustedSellGold := math.Max(1, math.Floor(float64(max(int64(1), equipment.SellGold))*1.1))
	base := int64(math.Max(1, math.Floor(adjustedSellGold*rule.Multiplier/config.BaseConst)))
	plan := DisjointerPlan{Materials: []RecipeEntry{{rule.ItemID, base}}}
	bigWin := random.Intn(10000) < max(0, min(100, rule.BigWinChancePercent))*100
	table := rule.AdditionalTable
	selections := config.AdditionalResults
	if bigWin {
		table = rule.BigWinTable
		selections = config.BigWinResults
	}
	if selected, selectedOK := selectDisjointerRule(selections[table], equipmentGrade, random); selectedOK {
		quantity := int64(1)
		if selected.CountDivisor > 0 {
			quantity = int64(math.Floor(float64(equipmentGrade) / selected.CountDivisor))
			if quantity < 1 {
				quantity = 1
			}
		}
		plan.Materials = mergeEntry(plan.Materials, RecipeEntry{selected.ItemID, quantity})
	}
	if selfService && equipment.Rarity > 1 && config.SelfServiceItemID > 0 && config.SelfServiceItemCount > 0 && random.Intn(100) < config.SelfServiceChance {
		plan.Materials = mergeEntry(plan.Materials, RecipeEntry{config.SelfServiceItemID, config.SelfServiceItemCount})
	}
	plan.EnduranceReduction = int64(config.EnduranceReduceMin)
	if config.EnduranceReduceMax > config.EnduranceReduceMin {
		plan.EnduranceReduction += int64(random.Intn(config.EnduranceReduceMax - config.EnduranceReduceMin + 1))
	}
	plan.ExperienceGain = int64(config.ExperienceGainMin)
	if config.ExperienceGainMax > config.ExperienceGainMin {
		plan.ExperienceGain += int64(random.Intn(config.ExperienceGainMax - config.ExperienceGainMin + 1))
	}
	plan.FinalExperience = saturatingAdd(experience, plan.ExperienceGain)
	plan.LevelChanged = config.Level(experience) != config.Level(plan.FinalExperience)
	return plan, nil
}

type MachineRepairPlan struct {
	Gold      int64
	Endurance int64
	Cost      int64
}

func PlanMachineRepair(gold, currentEndurance int64, rule RepairRule) (MachineRepairPlan, error) {
	if gold < 0 || currentEndurance < 0 || rule.MaxEndurance <= 0 || rule.FullRepairCost <= 0 {
		return MachineRepairPlan{}, ErrMachineInvalid
	}
	current := min(currentEndurance, rule.MaxEndurance)
	cost := rule.FullRepairCost - current*rule.FullRepairCost/rule.MaxEndurance
	if cost <= 0 {
		return MachineRepairPlan{}, ErrMachineInvalid
	}
	if gold < cost {
		unit := rule.FullRepairCost / rule.MaxEndurance
		if unit <= 0 {
			return MachineRepairPlan{}, ErrMachineInvalid
		}
		cost = gold - gold%unit
		repaired := cost / unit
		if cost <= 0 || repaired <= 0 {
			return MachineRepairPlan{}, ErrMachineInvalid
		}
		current = min(rule.MaxEndurance, current+repaired)
	} else {
		current = rule.MaxEndurance
	}
	return MachineRepairPlan{Gold: gold - cost, Endurance: current, Cost: cost}, nil
}

type DisjointerUpgradePlan struct {
	Gold      int64
	Grade     int
	Endurance int64
	Cost      int64
}

func (c *DisjointerConfig) PlanUpgrade(gold, experience, endurance int64, machineGrade, characterLevel int) (DisjointerUpgradePlan, error) {
	if c == nil || gold < 0 || machineGrade <= 0 || characterLevel <= 0 {
		return DisjointerUpgradePlan{}, ErrMachineInvalid
	}
	current, currentOK := c.RepairRule(machineGrade)
	targetGrade := machineGrade + 1
	target, targetOK := c.RepairRule(targetGrade)
	cost, costOK := c.UpgradeCosts[targetGrade]
	minimumCharacterLevel, levelOK := c.CharacterLevelLimits[targetGrade]
	if !currentOK || !targetOK || !costOK || cost <= 0 || !levelOK || endurance != current.MaxEndurance || c.Level(experience) < targetGrade {
		return DisjointerUpgradePlan{}, ErrMachineInvalid
	}
	if characterLevel < minimumCharacterLevel {
		return DisjointerUpgradePlan{}, ErrLevelTooLow
	}
	if gold < cost {
		return DisjointerUpgradePlan{}, ErrInsufficientGold
	}
	return DisjointerUpgradePlan{Gold: gold - cost, Grade: targetGrade, Endurance: target.MaxEndurance, Cost: cost}, nil
}

func (c *Catalog) loadDisjointerConfig() (*DisjointerConfig, error) {
	doc, err := readDocument(c.source, DisjointerPVFPath)
	if err != nil {
		return nil, err
	}
	config := &DisjointerConfig{
		Results: map[disjointerResultKey]DisjointerResultRule{}, AdditionalResults: map[int][]DisjointerSelectionRule{},
		BigWinResults: map[int][]DisjointerSelectionRule{}, UpgradeCosts: map[int]int64{}, CharacterLevelLimits: map[int]int{},
	}
	config.InitialEndurance, _ = doc.Int("endurance initial value")
	config.MaximumStoreCharge, _ = doc.Int("limit store charge")
	for _, cost := range doc.Ints("giveup cost") {
		if cost < 0 {
			return nil, errors.New("disjointer [giveup cost] contains a negative value")
		}
		config.GiveUpCosts = append(config.GiveUpCosts, cost)
	}
	if value, ok := doc.Int("base const"); ok {
		config.BaseConst = float64(value)
	}
	reduce := doc.Ints("endurance reduce")
	gain := doc.Ints("gain exp")
	self := doc.Ints("disjoint self service")
	if len(reduce) != 2 || len(gain) != 2 || len(self) < 3 {
		return nil, errors.New("disjointer scalar rules are invalid")
	}
	config.EnduranceReduceMin, config.EnduranceReduceMax = int(reduce[0]), int(reduce[1])
	config.ExperienceGainMin, config.ExperienceGainMax = int(gain[0]), int(gain[1])
	config.SelfServiceChance, config.SelfServiceItemID, config.SelfServiceItemCount = int(self[0]), self[1], self[2]
	results := doc.Numbers("disjoint result")
	if len(results) == 0 || len(results)%8 != 0 {
		return nil, errors.New("disjointer [disjoint result] is invalid")
	}
	for i := 0; i < len(results); i += 8 {
		key := disjointerResultKey{int(results[i]), int(results[i+1]), int(results[i+2])}
		rule := DisjointerResultRule{int64(results[i+3]), results[i+4], int(results[i+5]), int(results[i+6]), int(results[i+7])}
		if key.MachineGrade < 0 || key.Rarity < 0 || key.State < 0 || rule.ItemID <= 0 || rule.Multiplier <= 0 {
			return nil, errors.New("disjointer result rule is invalid")
		}
		if _, duplicate := config.Results[key]; duplicate {
			return nil, errors.New("disjointer result rule is duplicated")
		}
		config.Results[key] = rule
	}
	if err := parseDisjointerSelections(doc.Numbers("additional result"), config.AdditionalResults); err != nil {
		return nil, err
	}
	if err := parseDisjointerSelections(doc.Numbers("big win result"), config.BigWinResults); err != nil {
		return nil, err
	}
	repairs := doc.Ints("endurance repair cost")
	if len(repairs) == 0 || len(repairs)%2 != 0 {
		return nil, errors.New("disjointer repair rules are invalid")
	}
	for i := 0; i < len(repairs); i += 2 {
		if repairs[i] <= 0 || repairs[i+1] <= 0 {
			return nil, errors.New("disjointer repair rule is invalid")
		}
		config.RepairRules = append(config.RepairRules, RepairRule{repairs[i], repairs[i+1]})
	}
	thresholdTokens, found := doc.Section("expertness exp")
	if !found || len(thresholdTokens) == 0 || len(thresholdTokens)%3 != 0 {
		return nil, errors.New("disjointer experience thresholds are invalid")
	}
	for i := 0; i < len(thresholdTokens); i += 3 {
		if thresholdTokens[i].Kind != dnfpvf.TokenInt {
			return nil, errors.New("disjointer experience threshold is invalid")
		}
		config.ExperienceThresholds = append(config.ExperienceThresholds, thresholdTokens[i].Int)
	}
	if err := parseInt64Pairs(doc.Ints("upgrade cost"), config.UpgradeCosts); err != nil {
		return nil, err
	}
	etc, err := readDocument(c.source, "character/expertjob.etc")
	if err != nil {
		return nil, err
	}
	limits := map[int]int64{}
	if err := parseInt64Pairs(etc.Ints("expertjob level limit"), limits); err != nil {
		return nil, err
	}
	for grade, level := range limits {
		config.CharacterLevelLimits[grade] = int(level)
	}
	if config.InitialEndurance <= 0 || config.MaximumStoreCharge < 0 || len(config.GiveUpCosts) == 0 || config.BaseConst <= 0 || config.EnduranceReduceMin < 0 ||
		config.EnduranceReduceMax < config.EnduranceReduceMin || config.ExperienceGainMin < 0 || config.ExperienceGainMax < config.ExperienceGainMin ||
		len(config.Results) == 0 || len(config.RepairRules) == 0 || len(config.RepairRules) != len(config.ExperienceThresholds) ||
		len(config.UpgradeCosts) != len(config.RepairRules) || len(config.CharacterLevelLimits) < len(config.RepairRules) {
		return nil, errors.New("disjointer configuration is incomplete")
	}
	return config, nil
}

func parseDisjointerSelections(values []float64, target map[int][]DisjointerSelectionRule) error {
	if len(values)%6 != 0 {
		return errors.New("disjointer selection row width is invalid")
	}
	for i := 0; i < len(values); i += 6 {
		table := int(values[i])
		rule := DisjointerSelectionRule{int(values[i+1]), int(values[i+2]), int64(values[i+3]), int(values[i+4]), values[i+5]}
		if table <= 0 || rule.MinimumGrade < 0 || rule.MaximumGrade < rule.MinimumGrade || rule.ItemID <= 0 || rule.Weight <= 0 || rule.CountDivisor <= 0 {
			return errors.New("disjointer selection is invalid")
		}
		target[table] = append(target[table], rule)
	}
	return nil
}

func parseInt64Pairs(values []int64, target map[int]int64) error {
	if len(values) == 0 || len(values)%2 != 0 {
		return errors.New("expert job pair row width is invalid")
	}
	for i := 0; i < len(values); i += 2 {
		key, value := int(values[i]), values[i+1]
		if key <= 0 || value < 0 {
			return errors.New("expert job pair is invalid")
		}
		if _, duplicate := target[key]; duplicate {
			return errors.New("expert job pair is duplicated")
		}
		target[key] = value
	}
	return nil
}

func selectDisjointerRule(values []DisjointerSelectionRule, grade int, rng randomSource) (DisjointerSelectionRule, bool) {
	total := 0
	eligible := make([]DisjointerSelectionRule, 0, len(values))
	for _, value := range values {
		if grade >= value.MinimumGrade && grade <= value.MaximumGrade {
			eligible = append(eligible, value)
			total += value.Weight
		}
	}
	if total <= 0 {
		return DisjointerSelectionRule{}, false
	}
	roll := rng.Intn(total)
	for _, value := range eligible {
		if roll < value.Weight {
			return value, true
		}
		roll -= value.Weight
	}
	return DisjointerSelectionRule{}, false
}
