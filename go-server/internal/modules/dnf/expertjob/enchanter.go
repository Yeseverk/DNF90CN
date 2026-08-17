package expertjob

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

type RepairRule struct {
	FullRepairCost int64
	MaxEndurance   int64
}

type EnchanterCardRecipe struct {
	Qualification int
	RecipeItemID  int64
	RequiredLevel int
	Materials     []RecipeEntry
}

type EnchanterCard struct {
	Qualification int
	BindChance    int
}

type EnchanterCardExperienceRule struct {
	Level                 int
	SuccessRates          [5]int
	ExtraRate             int
	MinimumExperienceGain int
	MaximumExperienceGain int
}

type EnchanterConfig struct {
	Recipes                    *Config
	MaximumStoreCharge         int64
	InitialEndurance           int64
	EnduranceReduction         int64
	EnduranceMinimumLevel      int
	RepairRules                []RepairRule
	QualificationRequiredLevel map[int]int
	CardRecipes                map[int64]EnchanterCardRecipe
	Cards                      map[int64]EnchanterCard
	BeadByCard                 map[int64]int64
	CardExperienceByLevel      map[int]EnchanterCardExperienceRule
}

func (c *EnchanterConfig) Qualifications(experience int64) []byte {
	if c == nil || c.Recipes == nil {
		return nil
	}
	level := c.Recipes.Level(experience)
	values := make([]int, 0, len(c.QualificationRequiredLevel))
	for qualification, required := range c.QualificationRequiredLevel {
		if qualification >= 0 && qualification <= math.MaxUint8 && required <= level {
			values = append(values, qualification)
		}
	}
	sort.Ints(values)
	out := make([]byte, len(values))
	for i, value := range values {
		out[i] = byte(value)
	}
	return out
}

func (c *EnchanterConfig) StoreSkills(experience int64) []byte {
	if c == nil || c.Recipes == nil {
		return nil
	}
	level := c.Recipes.Level(experience)
	values := make([]int, 0, len(c.Recipes.Skills))
	for skill, required := range c.Recipes.Skills {
		if skill > 0 && skill <= math.MaxUint8 && required <= level {
			values = append(values, skill)
		}
	}
	sort.Ints(values)
	out := make([]byte, len(values))
	for i, value := range values {
		out[i] = byte(value)
	}
	return out
}

func (c *EnchanterConfig) RepairRule(experience int64) (RepairRule, bool) {
	if c == nil || c.Recipes == nil {
		return RepairRule{}, false
	}
	index := c.Recipes.Level(experience) - 1
	if index < 0 || index >= len(c.RepairRules) {
		return RepairRule{}, false
	}
	return c.RepairRules[index], true
}

func (c *EnchanterConfig) KnowsRecipe(recipeID int64) bool {
	if c == nil {
		return false
	}
	if _, ok := c.CardRecipes[recipeID]; ok {
		return true
	}
	_, ok := c.Recipes.Recipes[recipeID]
	return ok
}

type EnchanterBeadPlan struct {
	Recipe          EnchanterCardRecipe
	Card            EnchanterCard
	BeadItemID      int64
	Success         bool
	ExperienceGain  int64
	FinalExperience int64
	LevelChanged    bool
}

func (c *Catalog) PlanEnchanterBead(experience, recipeID, cardID int64, rng RandomFunc) (EnchanterBeadPlan, error) {
	config, ok := c.Enchanter()
	if !ok {
		return EnchanterBeadPlan{}, ErrJobUnsupported
	}
	recipe, recipeOK := config.CardRecipes[recipeID]
	card, cardOK := config.Cards[cardID]
	bead, beadOK := config.BeadByCard[cardID]
	level := config.Recipes.Level(experience)
	rule, ruleOK := config.CardExperienceByLevel[level]
	if !recipeOK || !cardOK || !beadOK || !ruleOK || recipe.Qualification != card.Qualification ||
		level < recipe.RequiredLevel || recipe.Qualification < 0 || recipe.Qualification >= len(rule.SuccessRates) {
		return EnchanterBeadPlan{}, ErrRecipeUnavailable
	}
	random := randomSource(RandomFunc(rng))
	plan := EnchanterBeadPlan{Recipe: recipe, Card: card, BeadItemID: bead}
	plan.Success = random.Intn(100) < rule.SuccessRates[recipe.Qualification]
	if plan.Success {
		gain := rule.MinimumExperienceGain
		if rule.MaximumExperienceGain > gain {
			gain += random.Intn(rule.MaximumExperienceGain - gain + 1)
		}
		plan.ExperienceGain = int64(gain)
	}
	plan.FinalExperience = saturatingAdd(experience, plan.ExperienceGain)
	plan.LevelChanged = config.Recipes.Level(experience) != config.Recipes.Level(plan.FinalExperience)
	return plan, nil
}

type EnchanterStorePlan struct {
	Recipe             EnchanterCardRecipe
	Success            bool
	EnduranceReduction int64
	ExperienceGain     int64
	FinalExperience    int64
	LevelChanged       bool
}

func (c *Catalog) PlanEnchanterStore(experience, recipeID, cardID, endurance int64, rng RandomFunc) (EnchanterStorePlan, error) {
	config, ok := c.Enchanter()
	if !ok {
		return EnchanterStorePlan{}, ErrJobUnsupported
	}
	recipe, recipeOK := config.CardRecipes[recipeID]
	card, cardOK := config.Cards[cardID]
	level := config.Recipes.Level(experience)
	rule, ruleOK := config.CardExperienceByLevel[level]
	if !recipeOK || !cardOK || !ruleOK || recipe.Qualification != card.Qualification || level < recipe.RequiredLevel ||
		recipe.Qualification < 0 || recipe.Qualification >= len(rule.SuccessRates) {
		return EnchanterStorePlan{}, ErrRecipeUnavailable
	}
	reduction := int64(0)
	if level >= config.EnduranceMinimumLevel {
		reduction = config.EnduranceReduction
	}
	if endurance < reduction {
		return EnchanterStorePlan{}, ErrMachineEndurance
	}
	random := randomSource(RandomFunc(rng))
	plan := EnchanterStorePlan{Recipe: recipe, EnduranceReduction: reduction}
	plan.Success = random.Intn(100) < rule.SuccessRates[recipe.Qualification]
	if plan.Success {
		gain := rule.MinimumExperienceGain
		if rule.MaximumExperienceGain > gain {
			gain += random.Intn(rule.MaximumExperienceGain - gain + 1)
		}
		plan.ExperienceGain = int64(gain)
	}
	plan.FinalExperience = saturatingAdd(experience, plan.ExperienceGain)
	plan.LevelChanged = config.Recipes.Level(experience) != config.Recipes.Level(plan.FinalExperience)
	return plan, nil
}

func (c *Catalog) loadEnchanterConfig(recipes *Config) (*EnchanterConfig, error) {
	if recipes == nil {
		return nil, ErrCatalogUnavailable
	}
	doc, err := readDocument(c.source, EnchanterPVFPath)
	if err != nil {
		return nil, err
	}
	config := &EnchanterConfig{
		Recipes: recipes, QualificationRequiredLevel: map[int]int{}, CardRecipes: map[int64]EnchanterCardRecipe{},
		Cards: map[int64]EnchanterCard{}, BeadByCard: map[int64]int64{}, CardExperienceByLevel: map[int]EnchanterCardExperienceRule{},
	}
	if value, ok := doc.Int("limit store charge"); ok {
		config.MaximumStoreCharge = value
	}
	if value, ok := doc.Int("endurance initial value"); ok {
		config.InitialEndurance = value
	}
	reduction := doc.Ints("endurance reduce")
	if len(reduction) != 2 {
		return nil, errors.New("enchanter [endurance reduce] is invalid")
	}
	config.EnduranceMinimumLevel, config.EnduranceReduction = int(reduction[0]), reduction[1]
	repairs := doc.Ints("endurance repair cost")
	if len(repairs) == 0 || len(repairs)%2 != 0 {
		return nil, errors.New("enchanter [endurance repair cost] is invalid")
	}
	for i := 0; i < len(repairs); i += 2 {
		if repairs[i] <= 0 || repairs[i+1] <= 0 {
			return nil, errors.New("enchanter repair rule is invalid")
		}
		config.RepairRules = append(config.RepairRules, RepairRule{repairs[i], repairs[i+1]})
	}
	if err := c.parseEnchanterRarityRecipes(doc, config); err != nil {
		return nil, err
	}
	bind := doc.Ints("monstercard bind info")
	if len(bind) == 0 || len(bind)%2 != 0 {
		return nil, fmt.Errorf("enchanter [monstercard bind info] is invalid: values=%d", len(bind))
	}
	for i := 0; i < len(bind); i += 2 {
		qualification, chance := int(bind[i]), int(bind[i+1])
		if qualification < 0 || chance < 0 || chance > 1000 {
			return nil, fmt.Errorf("enchanter bind rule is invalid: qualification=%d chance=%d", qualification, chance)
		}
	}
	rules := doc.Ints("monstercard exp")
	if len(rules) == 0 || len(rules)%9 != 0 {
		return nil, errors.New("enchanter [monstercard exp] is invalid")
	}
	for i := 0; i < len(rules); i += 9 {
		rule := EnchanterCardExperienceRule{Level: int(rules[i]), ExtraRate: int(rules[i+6]), MinimumExperienceGain: int(rules[i+7]), MaximumExperienceGain: int(rules[i+8])}
		for j := range rule.SuccessRates {
			rule.SuccessRates[j] = int(rules[i+1+j])
			if rule.SuccessRates[j] < 0 || rule.SuccessRates[j] > 100 {
				return nil, errors.New("enchanter card success rate is invalid")
			}
		}
		if rule.Level <= 0 || rule.ExtraRate < 0 || rule.ExtraRate > 100 || rule.MinimumExperienceGain < 0 || rule.MaximumExperienceGain < rule.MinimumExperienceGain {
			return nil, errors.New("enchanter card experience rule is invalid")
		}
		config.CardExperienceByLevel[rule.Level] = rule
	}
	if config.MaximumStoreCharge < 0 || config.InitialEndurance <= 0 || config.EnduranceReduction <= 0 || config.EnduranceMinimumLevel <= 0 ||
		len(config.RepairRules) != len(recipes.ExperienceThresholds) || len(config.CardRecipes) == 0 || len(config.Cards) == 0 ||
		len(config.BeadByCard) == 0 || len(config.CardExperienceByLevel) != len(recipes.ExperienceThresholds) {
		return nil, errors.New("enchanter configuration is incomplete")
	}
	return config, nil
}

func (c *Catalog) parseEnchanterRarityRecipes(doc *dnfpvf.Document, config *EnchanterConfig) error {
	active := false
	for index := 0; index < len(doc.Sections); index++ {
		name := strings.ToLower(strings.TrimSpace(doc.Sections[index].Name))
		switch name {
		case "rarity recipe":
			active = true
			continue
		case "/rarity recipe":
			return nil
		}
		if !active || name != "rarity" {
			continue
		}
		qualificationValues := sectionInts(doc, doc.Sections[index])
		if len(qualificationValues) != 1 {
			return errors.New("enchanter [rarity] is invalid")
		}
		qualification := int(qualificationValues[0])
		var recipeID, required int64
		var baseResults []int64
		for index++; index < len(doc.Sections); index++ {
			childName := strings.ToLower(strings.TrimSpace(doc.Sections[index].Name))
			if childName == "/rarity" {
				break
			}
			values := sectionInts(doc, doc.Sections[index])
			switch childName {
			case "recipe":
				if len(values) == 1 {
					recipeID = values[0]
				}
			case "expert job level":
				if len(values) == 1 {
					required = values[0]
				}
			case "base result":
				baseResults = values
			}
		}
		if qualification < 0 || qualification > math.MaxUint8 || recipeID <= 0 || required <= 0 || len(baseResults) == 0 || len(baseResults)%2 != 0 {
			return errors.New("enchanter rarity recipe is invalid")
		}
		ref, found := c.items[recipeID]
		if !found || ref.kind != itemStackable {
			return fmt.Errorf("enchanter card recipe item=%d is unavailable", recipeID)
		}
		recipeDoc, err := c.itemDocument(ref)
		if err != nil {
			return err
		}
		jobOnly := normalizeTag(firstText(recipeDoc, "expertjob only"))
		jobLevel, levelFound := recipeDoc.Int("expertjob only")
		needSkill := recipeDoc.Ints("need skill")
		materials := positivePairs(recipeDoc.Ints("input item"))
		if !strings.Contains(normalizeTag(firstText(recipeDoc, "stackable type")), "recipe") || jobOnly != "enchanter" || !levelFound || jobLevel != required ||
			len(needSkill) < 2 || needSkill[1] <= 0 || config.Recipes.Skills[int(needSkill[0])] == 0 || len(materials) == 0 {
			return errors.New("enchanter card recipe metadata is invalid")
		}
		config.QualificationRequiredLevel[qualification] = int(required)
		config.CardRecipes[recipeID] = EnchanterCardRecipe{qualification, recipeID, int(required), materials}
		for i := 0; i < len(baseResults); i += 2 {
			cardID, beadID := baseResults[i], baseResults[i+1]
			if cardID <= 0 || beadID <= 0 {
				return errors.New("enchanter bead result is invalid")
			}
			if _, duplicate := config.BeadByCard[cardID]; duplicate {
				return errors.New("enchanter bead result is duplicated")
			}
			ref, found := c.items[cardID]
			config.BeadByCard[cardID] = beadID
			if found && ref.kind == itemStackable {
				cardDoc, loadErr := c.itemDocument(ref)
				if loadErr == nil && normalizeTag(strings.Join(cardDoc.Texts("item category"), " ")) == "monster card" {
					config.Cards[cardID] = EnchanterCard{Qualification: qualification}
				}
			}
		}
	}
	return errors.New("enchanter [rarity recipe] is not closed")
}

func sectionInts(doc *dnfpvf.Document, section dnfpvf.Section) []int64 {
	if doc == nil || section.Start < 0 || section.End > len(doc.Tokens) || section.Start > section.End {
		return nil
	}
	values := make([]int64, 0, section.End-section.Start)
	for _, token := range doc.Tokens[section.Start:section.End] {
		if token.Kind == dnfpvf.TokenInt {
			values = append(values, token.Int)
		}
	}
	return values
}

func positivePairs(values []int64) []RecipeEntry {
	if len(values) == 0 || len(values)%2 != 0 {
		return nil
	}
	merged := map[int64]int64{}
	for i := 0; i < len(values); i += 2 {
		if values[i] <= 0 || values[i+1] <= 0 || merged[values[i]] > math.MaxInt64-values[i+1] {
			return nil
		}
		merged[values[i]] += values[i+1]
	}
	keys := make([]int64, 0, len(merged))
	for itemID := range merged {
		keys = append(keys, itemID)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	out := make([]RecipeEntry, 0, len(keys))
	for _, itemID := range keys {
		out = append(out, RecipeEntry{itemID, merged[itemID]})
	}
	return out
}
