package expertjob

import (
	"context"
	"os"
	"reflect"
	"testing"

	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestRealScriptPVFAllExpertJobsCatalog(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to verify expert-job catalog")
	}
	archive, err := platformpvf.LoadArchive(platformpvf.Options{Path: pvfPath})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := Load(context.Background(), archive)
	if err != nil {
		t.Fatal(err)
	}
	for _, jobType := range []byte{EnchanterType, AlchemistType, DollControllerType} {
		config, ok := catalog.Config(jobType)
		if !ok || config.Level(0) != 1 || len(config.AutoRecipeIDs(0)) == 0 ||
			len(config.GiveUpCosts) == 0 || len(config.Recipes) == 0 || len(config.Extractors) == 0 || len(config.ExtractionRules) == 0 {
			t.Fatalf("expert job type=%d config=%+v found=%t", jobType, config, ok)
		}
		if !reflect.DeepEqual(config.GiveUpCosts, []int64{1000, 10_000, 100_000, 1_000_000}) {
			t.Fatalf("expert job type=%d give-up costs=%v", jobType, config.GiveUpCosts)
		}
		t.Logf("type=%d levels=%d recipes=%d extractors=%d extraction_rules=%d auto_level1=%v",
			jobType, len(config.ExperienceThresholds), len(config.Recipes), len(config.Extractors),
			len(config.ExtractionRules), config.AutoRecipeIDs(0))
	}
	enchanter, ok := catalog.Enchanter()
	if !ok || enchanter.MaximumStoreCharge != 10_000 || enchanter.InitialEndurance <= 0 ||
		enchanter.Recipes.ExtractionBaseConst != 500 || len(enchanter.CardRecipes) < 5 || len(enchanter.Cards) == 0 ||
		enchanter.CardRecipes[10015129].RequiredLevel != 1 || enchanter.Cards[3619].Qualification != 0 || enchanter.BeadByCard[3619] != 2600313 {
		t.Fatalf("enchanter config=%+v found=%t", enchanter, ok)
	}
	disjointer, ok := catalog.Disjointer()
	if !ok || disjointer.InitialEndurance != 300 || disjointer.MaximumStoreCharge != 10_000 || len(disjointer.GiveUpCosts) == 0 || disjointer.BaseConst != 150 ||
		len(disjointer.RepairRules) != 11 || disjointer.UpgradeCosts[2] != 30_000 || disjointer.CharacterLevelLimits[2] != 20 {
		t.Fatalf("disjointer config=%+v found=%t", disjointer, ok)
	}
	if !reflect.DeepEqual(disjointer.GiveUpCosts, []int64{1000, 10_000, 100_000, 1_000_000}) {
		t.Fatalf("disjointer give-up costs=%v", disjointer.GiveUpCosts)
	}
	t.Logf("enchanter card_recipes=%d cards=%d beads=%d repair_rules=%d; disjointer results=%d repair_rules=%d",
		len(enchanter.CardRecipes), len(enchanter.Cards), len(enchanter.BeadByCard), len(enchanter.RepairRules),
		len(disjointer.Results), len(disjointer.RepairRules))
}
