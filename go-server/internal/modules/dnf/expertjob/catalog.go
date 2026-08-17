package expertjob

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path"
	"sort"
	"strings"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

const (
	EnchanterType      byte = 1
	AlchemistType      byte = 2
	DisjointerType     byte = 3
	DollControllerType byte = 4

	EnchanterPVFPath      = "character/expertjob/enchanter.exj"
	AlchemistPVFPath      = "character/expertjob/alchemist.exj"
	DisjointerPVFPath     = "character/expertjob/disjointer.exj"
	DollControllerPVFPath = "character/expertjob/doll_controller.exj"
)

var (
	ErrCatalogUnavailable = errors.New("expert job catalog is unavailable")
	ErrJobUnsupported     = errors.New("expert job is not supported")
	ErrRecipeUnavailable  = errors.New("expert job recipe is unavailable")
	ErrLevelTooLow        = errors.New("expert job level is too low")
	ErrExtractorInvalid   = errors.New("expert job extractor is invalid")
	ErrExtractionInvalid  = errors.New("expert job extraction target is invalid")
	ErrMachineEndurance   = errors.New("expert job machine has no endurance")
	ErrMachineInvalid     = errors.New("expert job machine state is invalid")
	ErrMachineGradeTooLow = errors.New("expert job machine grade is too low")
	ErrInsufficientGold   = errors.New("expert job gold is insufficient")
)

type randomSource interface {
	Intn(int) int
}

type RandomFunc func(int) int

func (f RandomFunc) Intn(n int) int {
	if n <= 1 || f == nil {
		return 0
	}
	return f(n)
}

type itemKind byte

const (
	itemStackable itemKind = iota + 1
	itemEquipment
)

type itemReference struct {
	kind itemKind
	path string
}

type RecipeEntry struct {
	ItemID int64
	Count  int64
}

type Recipe struct {
	RecipeItemID          int64
	ProductItemID         int64
	RequiredLevel         int
	MinimumExperienceGain int
	MaximumExperienceGain int
	Materials             []RecipeEntry
	Output                RecipeEntry
	GoldCost              int64
}

type CompoundRate struct {
	MaximumLevelDifference int
	SuccessRatePercent     int
	MinimumExperienceGain  int
	MaximumExperienceGain  int
}

type Extractor struct {
	ItemID                int64
	RequiredLevel         int
	MinimumExperienceGain int
	MaximumExperienceGain int
}

type extractionRuleKey struct {
	extractorID int64
	rarity      int
	state       int
}

type ExtractionRule struct {
	ResultItemID        int64
	Multiplier          float64
	AdditionalTable     int
	BigWinTable         int
	BigWinChancePercent int
}

type SelectionRule struct {
	MinimumLevel       int
	MaximumLevel       int
	ItemID             int64
	Weight             int
	QuantityMultiplier float64
}

type Config struct {
	JobType              byte
	ExperienceThresholds []int64
	GiveUpCosts          []int64
	AutoLearnRecipes     map[int]int64
	Skills               map[int]int
	CompoundRates        []CompoundRate
	Recipes              map[int64]Recipe
	Extractors           map[int64]Extractor
	ExtractionRules      map[extractionRuleKey]ExtractionRule
	AdditionalResults    map[int][]SelectionRule
	BigWinResults        map[int][]SelectionRule
	// ExtractionBaseConst selects the enchanter sell-price formula when it is
	// positive. Alchemist and doll-controller extraction use equipment grade.
	ExtractionBaseConst float64
}

func (c *Config) Level(experience int64) int {
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
	if level > len(c.ExperienceThresholds) {
		level = len(c.ExperienceThresholds)
	}
	return level
}

func (c *Config) AutoRecipeIDs(experience int64) []int64 {
	if c == nil {
		return nil
	}
	level := c.Level(experience)
	type row struct {
		level int
		id    int64
	}
	rows := make([]row, 0, len(c.AutoLearnRecipes))
	for required, id := range c.AutoLearnRecipes {
		if required <= level {
			rows = append(rows, row{required, id})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].level != rows[j].level {
			return rows[i].level < rows[j].level
		}
		return rows[i].id < rows[j].id
	})
	out := make([]int64, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.id)
	}
	return out
}

func (c *Config) CanLearn(experience int64, recipeID int64) bool {
	if c == nil {
		return false
	}
	recipe, ok := c.Recipes[recipeID]
	return ok && recipe.RequiredLevel <= c.Level(experience)+2
}

func (c *Config) compoundRate(recipeLevel, jobLevel int) CompoundRate {
	if len(c.CompoundRates) == 0 {
		return CompoundRate{SuccessRatePercent: 100}
	}
	difference := recipeLevel - jobLevel
	for _, rate := range c.CompoundRates {
		if rate.MaximumLevelDifference >= difference {
			return rate
		}
	}
	return c.CompoundRates[len(c.CompoundRates)-1]
}

type Catalog struct {
	source                    dnfpvf.Source
	items                     map[int64]itemReference
	configs                   map[byte]*Config
	equipmentSellRatePermille int64
	enchanter                 *EnchanterConfig
	disjointer                *DisjointerConfig
}

func Load(ctx context.Context, source dnfpvf.Source) (*Catalog, error) {
	if source == nil {
		return nil, ErrCatalogUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	stackables, err := loadItemList(source, "stackable/stackable.lst", itemStackable)
	if err != nil {
		return nil, err
	}
	equipment, err := loadItemList(source, "equipment/equipment.lst", itemEquipment)
	if err != nil {
		return nil, err
	}
	for id, ref := range equipment {
		if _, ok := stackables[id]; !ok {
			stackables[id] = ref
		}
	}
	catalog := &Catalog{source: source, items: stackables, configs: make(map[byte]*Config, 3)}
	if err := catalog.loadEquipmentSellRate(); err != nil {
		return nil, err
	}
	for _, input := range []struct {
		typ                    byte
		path, name, extraction string
	}{
		{EnchanterType, EnchanterPVFPath, "enchanter", "enchanter extraction"},
		{AlchemistType, AlchemistPVFPath, "alchemist", "alchemist extraction"},
		{DollControllerType, DollControllerPVFPath, "doll_controller", "doll_controller extraction"},
	} {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		config, err := catalog.loadConfig(input.typ, input.path, input.name, input.extraction)
		if err != nil {
			return nil, fmt.Errorf("load expert job type=%d: %w", input.typ, err)
		}
		catalog.configs[input.typ] = config
	}
	if catalog.enchanter, err = catalog.loadEnchanterConfig(catalog.configs[EnchanterType]); err != nil {
		return nil, fmt.Errorf("load enchanter config: %w", err)
	}
	if catalog.disjointer, err = catalog.loadDisjointerConfig(); err != nil {
		return nil, fmt.Errorf("load disjointer config: %w", err)
	}
	return catalog, nil
}

func (c *Catalog) Config(jobType byte) (*Config, bool) {
	if c == nil {
		return nil, false
	}
	config, ok := c.configs[jobType]
	return config, ok
}

func (c *Catalog) RecipeJob(recipeID int64) (byte, Recipe, bool) {
	if c == nil {
		return 0, Recipe{}, false
	}
	if c.enchanter != nil {
		if recipe, ok := c.enchanter.CardRecipes[recipeID]; ok {
			return EnchanterType, Recipe{RequiredLevel: recipe.RequiredLevel, Materials: append([]RecipeEntry(nil), recipe.Materials...)}, true
		}
	}
	for _, jobType := range []byte{EnchanterType, AlchemistType, DollControllerType} {
		config := c.configs[jobType]
		if config == nil {
			continue
		}
		if recipe, ok := config.Recipes[recipeID]; ok {
			return jobType, recipe, true
		}
	}
	return 0, Recipe{}, false
}

func (c *Catalog) Enchanter() (*EnchanterConfig, bool) {
	if c == nil || c.enchanter == nil {
		return nil, false
	}
	return c.enchanter, true
}

func (c *Catalog) Disjointer() (*DisjointerConfig, bool) {
	if c == nil || c.disjointer == nil {
		return nil, false
	}
	return c.disjointer, true
}

// GiveUpCosts returns the PVF-defined escalating abandonment costs for one
// expert job. The returned slice is detached from the immutable catalog.
func (c *Catalog) GiveUpCosts(jobType byte) ([]int64, bool) {
	if c == nil {
		return nil, false
	}
	if jobType == DisjointerType {
		if c.disjointer == nil || len(c.disjointer.GiveUpCosts) == 0 {
			return nil, false
		}
		return append([]int64(nil), c.disjointer.GiveUpCosts...), true
	}
	config := c.configs[jobType]
	if config == nil || len(config.GiveUpCosts) == 0 {
		return nil, false
	}
	return append([]int64(nil), config.GiveUpCosts...), true
}

type CompoundPlan struct {
	AttemptedOutputs []RecipeEntry
	Rewards          []RecipeEntry
	Materials        []RecipeEntry
	GoldCost         int64
	SuccessCount     int
	FailureCount     int
	ExperienceGain   int64
	FinalExperience  int64
	LevelChanged     bool
}

func (c *Catalog) PlanCompound(jobType byte, experience int64, recipeID int64, count int, rng RandomFunc) (CompoundPlan, error) {
	return c.PlanCompoundWithLearned(jobType, experience, recipeID, count, false, rng)
}

func (c *Catalog) PlanCompoundWithLearned(jobType byte, experience int64, recipeID int64, count int, learned bool, rng RandomFunc) (CompoundPlan, error) {
	config, ok := c.Config(jobType)
	if !ok {
		return CompoundPlan{}, ErrJobUnsupported
	}
	recipe, ok := config.Recipes[recipeID]
	if !ok || count <= 0 || count > math.MaxUint16 {
		return CompoundPlan{}, ErrRecipeUnavailable
	}
	hasRecipe := learned
	for _, id := range config.AutoRecipeIDs(experience) {
		if id == recipeID {
			hasRecipe = true
			break
		}
	}
	if !hasRecipe {
		return CompoundPlan{}, ErrRecipeUnavailable
	}
	level := config.Level(experience)
	if len(config.CompoundRates) == 0 && level < recipe.RequiredLevel {
		return CompoundPlan{}, ErrLevelTooLow
	}
	plan := CompoundPlan{
		AttemptedOutputs: []RecipeEntry{{ItemID: recipe.Output.ItemID, Count: recipe.Output.Count * int64(count)}},
		Materials:        multiplyEntries(recipe.Materials, int64(count)),
		GoldCost:         recipe.GoldCost * int64(count),
	}
	rate := config.compoundRate(recipe.RequiredLevel, level)
	if len(config.CompoundRates) == 0 {
		rate.MinimumExperienceGain = recipe.MinimumExperienceGain
		rate.MaximumExperienceGain = recipe.MaximumExperienceGain
	}
	random := randomSource(RandomFunc(rng))
	for attempt := 0; attempt < count; attempt++ {
		if random.Intn(100) >= rate.SuccessRatePercent {
			continue
		}
		plan.SuccessCount++
		gain := rate.MinimumExperienceGain
		if rate.MaximumExperienceGain > gain {
			gain += random.Intn(rate.MaximumExperienceGain - gain + 1)
		}
		plan.ExperienceGain += int64(gain)
	}
	plan.FailureCount = count - plan.SuccessCount
	if plan.SuccessCount > 0 {
		plan.Rewards = []RecipeEntry{{ItemID: recipe.Output.ItemID, Count: recipe.Output.Count * int64(plan.SuccessCount)}}
	}
	plan.FinalExperience = saturatingAdd(experience, plan.ExperienceGain)
	plan.LevelChanged = config.Level(experience) != config.Level(plan.FinalExperience)
	return plan, nil
}

type EquipmentMetadata struct {
	Rarity, Level, Grade, State int
	SellGold                    int64
	AttachType                  string
	DisjointForbidden           bool
}

func (c *Catalog) Equipment(itemID int64) (EquipmentMetadata, error) {
	ref, ok := c.items[itemID]
	if !ok || ref.kind != itemEquipment {
		return EquipmentMetadata{}, ErrExtractionInvalid
	}
	doc, err := c.itemDocument(ref)
	if err != nil {
		return EquipmentMetadata{}, err
	}
	rarity, rarityOK := doc.Int("rarity")
	level, levelOK := doc.Int("minimum level")
	if !rarityOK || !levelOK || rarity < 0 || rarity > 6 || level < 0 || level > math.MaxInt32 {
		return EquipmentMetadata{}, ErrExtractionInvalid
	}
	metadata := EquipmentMetadata{
		Rarity:     int(rarity),
		Level:      int(level),
		Grade:      int(level),
		AttachType: normalizeTag(firstText(doc, "attach type")),
	}
	if grade, ok := doc.Int("grade"); ok && grade >= 0 && grade <= math.MaxInt32 {
		metadata.Grade = int(grade)
	}
	base, baseFound := doc.Int("value")
	if !baseFound || base < 0 {
		base, baseFound = doc.Int("price")
	}
	if !baseFound || base < 0 || c.equipmentSellRatePermille <= 0 || base > math.MaxInt64/c.equipmentSellRatePermille {
		return EquipmentMetadata{}, ErrExtractionInvalid
	}
	metadata.SellGold = base * c.equipmentSellRatePermille / 1000
	if metadata.SellGold < 1 {
		metadata.SellGold = 1
	}
	for _, content := range doc.Texts("impossible content") {
		if normalizeTag(content) == "disjoint" {
			metadata.DisjointForbidden = true
			break
		}
	}
	return metadata, nil
}

type ExtractionPlan struct {
	Materials       []RecipeEntry
	ExperienceGain  int64
	FinalExperience int64
	LevelChanged    bool
}

func (c *Catalog) PlanExtraction(jobType byte, experience, extractorID int64, equipment EquipmentMetadata, rng RandomFunc) (ExtractionPlan, error) {
	config, ok := c.Config(jobType)
	if !ok {
		return ExtractionPlan{}, ErrJobUnsupported
	}
	extractor, ok := config.Extractors[extractorID]
	if !ok || config.Level(experience) < extractor.RequiredLevel {
		return ExtractionPlan{}, ErrExtractorInvalid
	}
	rule, ok := config.ExtractionRules[extractionRuleKey{extractorID, equipment.Rarity, equipment.State}]
	if !ok {
		return ExtractionPlan{}, ErrExtractionInvalid
	}
	random := randomSource(RandomFunc(rng))
	equipmentGrade := equipment.Grade
	if equipmentGrade <= 0 {
		equipmentGrade = equipment.Level
	}
	baseInput := float64(max(1, equipmentGrade))
	if config.ExtractionBaseConst > 0 {
		baseInput = float64(max(int64(1), equipment.SellGold)) / config.ExtractionBaseConst
	}
	base := int64(math.Floor(baseInput * rule.Multiplier))
	if base < 1 {
		base = 1
	}
	plan := ExtractionPlan{Materials: []RecipeEntry{{ItemID: rule.ResultItemID, Count: base}}}
	bigWin := random.Intn(10000) < max(0, min(100, rule.BigWinChancePercent))*100
	table := rule.AdditionalTable
	selections := config.AdditionalResults
	if bigWin {
		table = rule.BigWinTable
		selections = config.BigWinResults
	}
	if selected, ok := selectExtractionRule(selections[table], equipmentGrade, random); ok {
		quantity := int64(math.Floor(float64(equipmentGrade) * selected.QuantityMultiplier))
		if quantity < 1 {
			quantity = 1
		}
		plan.Materials = mergeEntry(plan.Materials, RecipeEntry{ItemID: selected.ItemID, Count: quantity})
	}
	gain := extractor.MinimumExperienceGain
	if extractor.MaximumExperienceGain > gain {
		gain += random.Intn(extractor.MaximumExperienceGain - gain + 1)
	}
	plan.ExperienceGain = int64(gain)
	plan.FinalExperience = saturatingAdd(experience, plan.ExperienceGain)
	plan.LevelChanged = config.Level(experience) != config.Level(plan.FinalExperience)
	return plan, nil
}

func (c *Catalog) loadConfig(jobType byte, pvfPath, jobName, extractionSection string) (*Config, error) {
	doc, err := readDocument(c.source, pvfPath)
	if err != nil {
		return nil, err
	}
	config := &Config{JobType: jobType, AutoLearnRecipes: map[int]int64{}, Skills: map[int]int{}, Recipes: map[int64]Recipe{}, Extractors: map[int64]Extractor{}, ExtractionRules: map[extractionRuleKey]ExtractionRule{}, AdditionalResults: map[int][]SelectionRule{}, BigWinResults: map[int][]SelectionRule{}}
	thresholds, found := doc.Section("expertness exp")
	if !found || len(thresholds) == 0 || len(thresholds)%3 != 0 {
		return nil, fmt.Errorf("%s [expertness exp] row width tokens=%d", pvfPath, len(thresholds))
	}
	for i := 0; i < len(thresholds); i += 3 {
		if thresholds[i].Kind != dnfpvf.TokenInt {
			return nil, fmt.Errorf("%s [expertness exp] threshold row=%d", pvfPath, i/3)
		}
		config.ExperienceThresholds = append(config.ExperienceThresholds, thresholds[i].Int)
	}
	for _, cost := range doc.Ints("giveup cost") {
		if cost < 0 {
			return nil, fmt.Errorf("%s [giveup cost] contains negative value %d", pvfPath, cost)
		}
		config.GiveUpCosts = append(config.GiveUpCosts, cost)
	}
	if len(config.GiveUpCosts) == 0 {
		return nil, fmt.Errorf("%s [giveup cost] is empty", pvfPath)
	}
	if err := parsePairs(doc.Ints("auto learn recipe"), func(k, v int64) { config.AutoLearnRecipes[int(k)] = v }); err != nil {
		return nil, fmt.Errorf("%s [auto learn recipe]: %w", pvfPath, err)
	}
	if err := parsePairs(doc.Ints("skill"), func(k, v int64) { config.Skills[int(k)] = int(v) }); err != nil {
		return nil, fmt.Errorf("%s [skill]: %w", pvfPath, err)
	}
	rates := doc.Ints("compound rate")
	if len(rates)%4 != 0 {
		return nil, fmt.Errorf("%s [compound rate] row width", pvfPath)
	}
	for i := 0; i < len(rates); i += 4 {
		config.CompoundRates = append(config.CompoundRates, CompoundRate{int(rates[i]), int(rates[i+1]), int(rates[i+2]), int(rates[i+3])})
	}
	extractExp, err := parseRanges(doc.Ints("extract exp"))
	if err != nil {
		return nil, fmt.Errorf("%s [extract exp]: %w", pvfPath, err)
	}
	productExp := map[int64][2]int{}
	if values := doc.Ints("product exp"); len(values) > 0 {
		productExp, err = parseRanges(values)
		if err != nil {
			return nil, fmt.Errorf("%s [product exp]: %w", pvfPath, err)
		}
	}
	items := doc.Ints("items")
	if len(items) == 0 || len(items)%4 != 0 {
		return nil, fmt.Errorf("%s [items] row width", pvfPath)
	}
	for i := 0; i < len(items); i += 4 {
		recipeID, productID, required := items[i+1], items[i+2], int(items[i+3])
		experienceRange := productExp[recipeID]
		if recipe, recipeErr := c.loadRecipe(recipeID, productID, required, experienceRange, config.Skills); recipeErr == nil {
			config.Recipes[recipeID] = recipe
		}
		if experienceRange, ok := extractExp[productID]; ok {
			if extractorErr := c.validateExtractor(productID, required, jobName, extractionSection); extractorErr != nil {
				return nil, extractorErr
			}
			config.Extractors[productID] = Extractor{productID, required, experienceRange[0], experienceRange[1]}
		}
	}
	if len(config.Recipes) == 0 || len(config.Extractors) == 0 {
		return nil, fmt.Errorf("%s has no recipes or extractors", pvfPath)
	}
	values := sectionNumbers(doc, "extraction result")
	if len(values) == 0 || len(values)%8 != 0 {
		return nil, fmt.Errorf("%s [extraction result] row width", pvfPath)
	}
	for i := 0; i < len(values); i += 8 {
		key := extractionRuleKey{int64(values[i]), int(values[i+1]), int(values[i+2])}
		config.ExtractionRules[key] = ExtractionRule{int64(values[i+3]), values[i+4], int(values[i+5]), int(values[i+6]), int(values[i+7])}
	}
	if err := parseSelections(sectionNumbers(doc, "additional result"), config.AdditionalResults); err != nil {
		return nil, err
	}
	if err := parseSelections(sectionNumbers(doc, "big win result"), config.BigWinResults); err != nil {
		return nil, err
	}
	if jobType == EnchanterType {
		base := doc.Ints("enchanter extraction result item")
		if len(base) != 2 || base[0] <= 0 || base[1] <= 0 {
			return nil, fmt.Errorf("%s [enchanter extraction result item] is invalid", pvfPath)
		}
		config.ExtractionBaseConst = float64(base[1])
	}
	return config, nil
}

func (c *Catalog) loadRecipe(recipeID, productID int64, required int, experienceRange [2]int, skills map[int]int) (Recipe, error) {
	ref, ok := c.items[recipeID]
	if !ok || ref.kind != itemStackable {
		return Recipe{}, ErrRecipeUnavailable
	}
	doc, err := c.itemDocument(ref)
	if err != nil {
		return Recipe{}, err
	}
	category := normalizeTag(strings.Join(doc.Texts("item category"), " "))
	if !strings.Contains(normalizeTag(firstText(doc, "stackable type")), "recipe") || category != "expertjob recipe" {
		return Recipe{}, ErrRecipeUnavailable
	}
	needSkill := doc.Ints("need skill")
	if len(needSkill) < 2 || needSkill[1] <= 0 {
		return Recipe{}, ErrRecipeUnavailable
	}
	if _, ok := skills[int(needSkill[0])]; !ok {
		return Recipe{}, ErrRecipeUnavailable
	}
	materials, outputs, gold := parseRecipeEntries(doc)
	if len(materials) == 0 || len(outputs) != 1 || outputs[0].ItemID != productID {
		return Recipe{}, ErrRecipeUnavailable
	}
	return Recipe{
		RecipeItemID: recipeID, ProductItemID: productID, RequiredLevel: required,
		MinimumExperienceGain: experienceRange[0], MaximumExperienceGain: experienceRange[1],
		Materials: materials, Output: outputs[0], GoldCost: gold,
	}, nil
}

func (c *Catalog) validateExtractor(itemID int64, required int, jobName, extractionSection string) error {
	ref, ok := c.items[itemID]
	if !ok || ref.kind != itemStackable {
		return ErrExtractorInvalid
	}
	doc, err := c.itemDocument(ref)
	if err != nil {
		return err
	}
	jobOnly := normalizeTag(firstText(doc, "expertjob only"))
	level, levelOK := doc.Int("expertjob only")
	extraction, extractionOK := doc.Int(extractionSection)
	if jobOnly != normalizeTag(jobName) || !levelOK || int(level) != required || !extractionOK || extraction < 0 {
		return fmt.Errorf("%w: item=%d job=%q level=%d level_found=%t required=%d extraction=%d extraction_found=%t section=%q", ErrExtractorInvalid, itemID, jobOnly, level, levelOK, required, extraction, extractionOK, extractionSection)
	}
	return nil
}

func (c *Catalog) itemDocument(ref itemReference) (*dnfpvf.Document, error) {
	root := "stackable"
	if ref.kind == itemEquipment {
		root = "equipment"
	}
	clean := cleanPath(ref.path)
	candidates := []string{clean}
	if !strings.HasPrefix(strings.ToLower(clean), root+"/") {
		candidates = append(candidates, path.Join(root, clean))
	}
	for _, candidate := range append([]string(nil), candidates...) {
		dir, base := path.Split(candidate)
		if base != "" && !strings.HasPrefix(strings.ToLower(base), "(r)") {
			candidates = append(candidates, dir+"(r)"+base)
		}
	}
	var last error
	for _, candidate := range candidates {
		doc, err := readDocument(c.source, candidate)
		if err == nil {
			return doc, nil
		}
		last = err
	}
	return nil, last
}

func loadItemList(source dnfpvf.Source, pvfPath string, kind itemKind) (map[int64]itemReference, error) {
	doc, err := readDocument(source, pvfPath)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]itemReference)
	for _, entry := range dnfpvf.ParseList(doc) {
		if entry.ID > 0 {
			out[entry.ID] = itemReference{kind, cleanPath(entry.Path)}
		}
	}
	return out, nil
}

func readDocument(source dnfpvf.Source, pvfPath string) (*dnfpvf.Document, error) {
	text, err := source.ReadText(pvfPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", pvfPath, err)
	}
	doc, err := dnfpvf.Parse(pvfPath, text)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", pvfPath, err)
	}
	return doc, nil
}

func (c *Catalog) loadEquipmentSellRate() error {
	if c == nil || c.source == nil {
		return ErrCatalogUnavailable
	}
	doc, err := readDocument(c.source, "equipment/pricetable.tbl")
	if err != nil {
		return err
	}
	rates := doc.Ints("rate")
	if len(rates) == 0 || rates[0] <= 0 || rates[0] > math.MaxInt32 {
		return errors.New("equipment/pricetable.tbl [rate] is invalid")
	}
	c.equipmentSellRatePermille = rates[0]
	return nil
}

func cleanPath(value string) string {
	return strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(strings.ReplaceAll(value, "\\", "/")), "./"), "/")
}
func normalizeTag(value string) string {
	return strings.ToLower(strings.Trim(strings.TrimSpace(strings.ReplaceAll(value, "`", "")), "[]"))
}
func firstText(doc *dnfpvf.Document, name string) string { value, _ := doc.Text(name); return value }

func parsePairs(values []int64, add func(int64, int64)) error {
	if len(values) == 0 || len(values)%2 != 0 {
		return errors.New("row width is not 2")
	}
	seen := map[int64]struct{}{}
	for i := 0; i < len(values); i += 2 {
		if values[i] <= 0 || values[i+1] <= 0 {
			return errors.New("non-positive pair")
		}
		if _, ok := seen[values[i]]; ok {
			return errors.New("duplicate key")
		}
		seen[values[i]] = struct{}{}
		add(values[i], values[i+1])
	}
	return nil
}

func parseRanges(values []int64) (map[int64][2]int, error) {
	if len(values) == 0 || len(values)%3 != 0 {
		return nil, errors.New("row width is not 3")
	}
	out := map[int64][2]int{}
	for i := 0; i < len(values); i += 3 {
		if values[i] <= 0 || values[i+1] < 0 || values[i+2] < values[i+1] {
			return nil, errors.New("invalid range")
		}
		out[values[i]] = [2]int{int(values[i+1]), int(values[i+2])}
	}
	return out, nil
}

func sectionNumbers(doc *dnfpvf.Document, name string) []float64 { return doc.Numbers(name) }

func parseSelections(values []float64, target map[int][]SelectionRule) error {
	if len(values)%6 != 0 {
		return errors.New("expert job selection row width is not 6")
	}
	for i := 0; i < len(values); i += 6 {
		table := int(values[i])
		rule := SelectionRule{int(values[i+1]), int(values[i+2]), int64(values[i+3]), int(values[i+4]), values[i+5]}
		if table <= 0 || rule.MinimumLevel < 0 || rule.MaximumLevel < rule.MinimumLevel || rule.ItemID <= 0 || rule.Weight <= 0 || rule.QuantityMultiplier <= 0 {
			return errors.New("invalid expert job selection")
		}
		target[table] = append(target[table], rule)
	}
	return nil
}

func parseRecipeEntries(doc *dnfpvf.Document) ([]RecipeEntry, []RecipeEntry, int64) {
	values := doc.Ints("int data")
	if len(values) > 0 {
		position, materialCount := 1, int(values[0])
		if materialCount >= 0 && len(values) >= position+materialCount*2 {
			materials := make([]RecipeEntry, 0, materialCount)
			for i := 0; i < materialCount; i++ {
				materials = append(materials, RecipeEntry{values[position], values[position+1]})
				position += 2
			}
			if position < len(values) {
				outputCount := int(values[position])
				position++
				if outputCount >= 0 && len(values) >= position+outputCount*2 {
					outputs := make([]RecipeEntry, 0, outputCount)
					for i := 0; i < outputCount; i++ {
						outputs = append(outputs, RecipeEntry{values[position], values[position+1]})
						position += 2
					}
					return materials, outputs, 0
				}
			}
		}
	}
	input := doc.Ints("input item")
	output := doc.Ints("output item")
	materials := make([]RecipeEntry, 0, len(input)/2)
	var gold int64
	for i := 0; i+1 < len(input); i += 2 {
		if input[i] == 0 {
			gold = max(gold, input[i+1])
		} else if input[i] > 0 && input[i+1] > 0 {
			materials = append(materials, RecipeEntry{input[i], input[i+1]})
		}
	}
	outputs := make([]RecipeEntry, 0, len(output)/2)
	for i := 0; i+1 < len(output); i += 2 {
		if output[i] > 0 && output[i+1] > 0 {
			outputs = append(outputs, RecipeEntry{output[i], output[i+1]})
		}
	}
	return materials, outputs, gold
}

func multiplyEntries(values []RecipeEntry, multiplier int64) []RecipeEntry {
	out := make([]RecipeEntry, 0, len(values))
	for _, value := range values {
		out = append(out, RecipeEntry{value.ItemID, value.Count * multiplier})
	}
	return out
}
func mergeEntry(values []RecipeEntry, entry RecipeEntry) []RecipeEntry {
	for i := range values {
		if values[i].ItemID == entry.ItemID {
			values[i].Count += entry.Count
			return values
		}
	}
	return append(values, entry)
}
func saturatingAdd(left, right int64) int64 {
	if right > 0 && left > math.MaxInt64-right {
		return math.MaxInt64
	}
	return left + right
}

func selectExtractionRule(values []SelectionRule, level int, rng randomSource) (SelectionRule, bool) {
	total := 0
	eligible := make([]SelectionRule, 0, len(values))
	for _, value := range values {
		if level >= value.MinimumLevel && level <= value.MaximumLevel {
			eligible = append(eligible, value)
			total += value.Weight
		}
	}
	if total <= 0 {
		return SelectionRule{}, false
	}
	roll := rng.Intn(total)
	for _, value := range eligible {
		if roll < value.Weight {
			return value, true
		}
		roll -= value.Weight
	}
	return SelectionRule{}, false
}
