package expertjob

import (
	"errors"
	"testing"
)

func TestPlanCompoundUsesLearnedRecipeRateAndSuccessfulExperienceOnly(t *testing.T) {
	config := &Config{
		JobType:              AlchemistType,
		ExperienceThresholds: []int64{10, 20, 30},
		AutoLearnRecipes:     map[int]int64{1: 1001},
		CompoundRates: []CompoundRate{{
			MaximumLevelDifference: 3,
			SuccessRatePercent:     50,
			MinimumExperienceGain:  2,
			MaximumExperienceGain:  4,
		}},
		Recipes: map[int64]Recipe{
			1002: {
				RecipeItemID:  1002,
				ProductItemID: 2002,
				RequiredLevel: 2,
				Materials:     []RecipeEntry{{ItemID: 3001, Count: 2}},
				Output:        RecipeEntry{ItemID: 2002, Count: 3},
			},
		},
	}
	catalog := &Catalog{configs: map[byte]*Config{AlchemistType: config}}
	rolls := []int{0, 2, 99}
	position := 0
	plan, err := catalog.PlanCompoundWithLearned(AlchemistType, 0, 1002, 2, true, func(int) int {
		value := rolls[position]
		position++
		return value
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.SuccessCount != 1 || plan.FailureCount != 1 || plan.ExperienceGain != 4 || plan.FinalExperience != 4 ||
		len(plan.Materials) != 1 || plan.Materials[0].Count != 4 ||
		len(plan.AttemptedOutputs) != 1 || plan.AttemptedOutputs[0].Count != 6 ||
		len(plan.Rewards) != 1 || plan.Rewards[0].Count != 3 {
		t.Fatalf("compound plan = %+v", plan)
	}
	if _, err := catalog.PlanCompound(AlchemistType, 0, 1002, 1, nil); err != ErrRecipeUnavailable {
		t.Fatalf("unlearned recipe error = %v", err)
	}
}

func TestPlanExtractionUsesEquipmentStateAndWeightedAdditionalResult(t *testing.T) {
	config := &Config{
		JobType:              DollControllerType,
		ExperienceThresholds: []int64{10, 20},
		Extractors: map[int64]Extractor{
			4001: {ItemID: 4001, RequiredLevel: 1, MinimumExperienceGain: 1, MaximumExperienceGain: 1},
		},
		ExtractionRules: map[extractionRuleKey]ExtractionRule{
			{extractorID: 4001, rarity: 3, state: 1}: {ResultItemID: 5001, Multiplier: 0.1, AdditionalTable: 2},
		},
		AdditionalResults: map[int][]SelectionRule{
			2: {{MinimumLevel: 1, MaximumLevel: 99, ItemID: 5002, Weight: 1, QuantityMultiplier: 0.2}},
		},
	}
	catalog := &Catalog{configs: map[byte]*Config{DollControllerType: config}}
	plan, err := catalog.PlanExtraction(DollControllerType, 0, 4001, EquipmentMetadata{Rarity: 3, Level: 50, State: 1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Materials) != 2 || plan.Materials[0] != (RecipeEntry{ItemID: 5001, Count: 5}) ||
		plan.Materials[1] != (RecipeEntry{ItemID: 5002, Count: 10}) || plan.ExperienceGain != 1 {
		t.Fatalf("extraction plan = %+v", plan)
	}
}

func TestPlanEnchanterBeadAndStoreUseCardQualificationAndEndurance(t *testing.T) {
	recipes := &Config{JobType: EnchanterType, ExperienceThresholds: []int64{10, 20}, Recipes: map[int64]Recipe{}}
	enchanter := &EnchanterConfig{
		Recipes: recipes, InitialEndurance: 300, EnduranceReduction: 3, EnduranceMinimumLevel: 1,
		CardRecipes: map[int64]EnchanterCardRecipe{100: {Qualification: 1, RecipeItemID: 100, RequiredLevel: 1, Materials: []RecipeEntry{{ItemID: 300, Count: 2}}}},
		Cards:       map[int64]EnchanterCard{200: {Qualification: 1}}, BeadByCard: map[int64]int64{200: 400},
		CardExperienceByLevel: map[int]EnchanterCardExperienceRule{1: {Level: 1, SuccessRates: [5]int{100, 70, 60, 50, 0}, MinimumExperienceGain: 3, MaximumExperienceGain: 6}},
	}
	catalog := &Catalog{configs: map[byte]*Config{EnchanterType: recipes}, enchanter: enchanter}
	bead, err := catalog.PlanEnchanterBead(0, 100, 200, func(limit int) int {
		if limit == 100 {
			return 69
		}
		return limit - 1
	})
	if err != nil || !bead.Success || bead.BeadItemID != 400 || bead.ExperienceGain != 6 || bead.FinalExperience != 6 {
		t.Fatalf("bead plan=%+v error=%v", bead, err)
	}
	store, err := catalog.PlanEnchanterStore(0, 100, 200, 3, func(int) int { return 99 })
	if err != nil || store.Success || store.EnduranceReduction != 3 || store.ExperienceGain != 0 || store.FinalExperience != 0 {
		t.Fatalf("store plan=%+v error=%v", store, err)
	}
	if _, err := catalog.PlanEnchanterStore(0, 100, 200, 2, nil); err != ErrMachineEndurance {
		t.Fatalf("low-endurance error=%v", err)
	}
}

func TestPlanDisjointerRepairAndUpgradeUseMachineRules(t *testing.T) {
	config := &DisjointerConfig{
		InitialEndurance: 300, BaseConst: 150, EnduranceReduceMin: 1, EnduranceReduceMax: 1,
		ExperienceGainMin: 1, ExperienceGainMax: 1,
		ExperienceThresholds: []int64{10, 20},
		Results:              map[disjointerResultKey]DisjointerResultRule{{MachineGrade: 0, Rarity: 2, State: 0}: {ItemID: 500, Multiplier: 1}},
		RepairRules:          []RepairRule{{FullRepairCost: 300, MaxEndurance: 300}, {FullRepairCost: 600, MaxEndurance: 400}},
		UpgradeCosts:         map[int]int64{2: 1000}, CharacterLevelLimits: map[int]int{2: 20},
	}
	catalog := &Catalog{disjointer: config}
	plan, err := catalog.PlanDisjointer(0, 1, EquipmentMetadata{Rarity: 2, Grade: 20, SellGold: 150}, false, nil)
	if err != nil || len(plan.Materials) != 1 || plan.Materials[0] != (RecipeEntry{ItemID: 500, Count: 1}) || plan.EnduranceReduction != 1 || plan.ExperienceGain != 1 {
		t.Fatalf("disjointer plan=%+v error=%v", plan, err)
	}
	repair, err := PlanMachineRepair(50, 200, config.RepairRules[0])
	if err != nil || repair.Cost != 50 || repair.Endurance != 250 || repair.Gold != 0 {
		t.Fatalf("repair plan=%+v error=%v", repair, err)
	}
	upgrade, err := config.PlanUpgrade(1500, 10, 300, 1, 20)
	if err != nil || upgrade.Grade != 2 || upgrade.Gold != 500 || upgrade.Endurance != 400 {
		t.Fatalf("upgrade plan=%+v error=%v", upgrade, err)
	}
}

func TestPlanGiveUpUsesPVFCostTierAndCapsAtLastTier(t *testing.T) {
	costs := []int64{1000, 10_000, 100_000, 1_000_000}
	first, err := PlanGiveUp(60_000, 0, costs)
	if err != nil || first.FinalGold != 59_000 || first.Cost != 1000 || first.FinalState != 1 {
		t.Fatalf("first give-up plan=%+v error=%v", first, err)
	}
	last, err := PlanGiveUp(2_000_000, 99, costs)
	if err != nil || last.FinalGold != 1_000_000 || last.Cost != 1_000_000 || last.FinalState != 3 {
		t.Fatalf("last give-up plan=%+v error=%v", last, err)
	}
	if _, err := PlanGiveUp(999, 0, costs); !errors.Is(err, ErrInsufficientGold) {
		t.Fatalf("insufficient-gold error=%v", err)
	}
}
