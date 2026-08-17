package pet

import (
	"errors"
	"math"
	"os"
	"testing"
	"time"

	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestPetGrowthDungeonClearLevelsAndAutoEvolvesFromPVF(t *testing.T) {
	catalog := newPetGrowthTestCatalog(t, false)
	engine, err := NewPetGrowthEngine(catalog)
	if err != nil {
		t.Fatal(err)
	}
	update, err := engine.ApplyDungeonClear(PetGrowthState{
		ItemID: 10, Experience: 1, Level: 1, Satiety: 100,
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if update.After.Experience != 2 || update.After.Level != 2 || update.After.ItemID != 11 || update.ExperienceGained != 1 {
		t.Fatalf("update=%+v", update)
	}
	if !update.Evolution.Changed || update.Evolution.QuestEligible ||
		update.Evolution.Definition.CurrentCreatureID != 1 || update.Evolution.Definition.TargetCreatureID != 2 {
		t.Fatalf("evolution=%+v", update.Evolution)
	}
}

func TestPetGrowthDungeonClearExposesQuestEvolutionWithoutAutoMutation(t *testing.T) {
	catalog := newPetGrowthTestCatalog(t, true)
	engine, err := NewPetGrowthEngine(catalog)
	if err != nil {
		t.Fatal(err)
	}
	update, err := engine.ApplyDungeonClear(PetGrowthState{
		ItemID: 10, Experience: 1, Level: 1, Satiety: 100,
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if update.After.ItemID != 10 || update.Evolution.Changed || !update.Evolution.QuestEligible ||
		update.Evolution.Definition.QuestReference != "quest/faras.qst" {
		t.Fatalf("update=%+v", update)
	}
}

func TestPetGrowthDungeonClearHonorsSatietyAndExperienceBounds(t *testing.T) {
	catalog := newPetGrowthTestCatalog(t, false)
	engine, err := NewPetGrowthEngine(catalog)
	if err != nil {
		t.Fatal(err)
	}
	dead := PetGrowthState{ItemID: 10, Experience: 1, Level: 1, Satiety: 0}
	update, err := engine.ApplyDungeonClear(dead, 50)
	if err != nil || update.After != dead || update.ExperienceGained != 0 {
		t.Fatalf("dead update=%+v err=%v", update, err)
	}
	maxed := PetGrowthState{ItemID: 11, Experience: MaxCreatureExperience, Level: MaxCreatureLevel, Satiety: 100}
	update, err = engine.ApplyDungeonClear(maxed, math.MaxInt32)
	if err != nil || update.After != maxed || update.ExperienceGained != 0 {
		t.Fatalf("max update=%+v err=%v", update, err)
	}
}

func TestPetGrowthEvolutionMissingPVFFailsBeforeReturningMutation(t *testing.T) {
	source := petGrowthTestSource(false)
	delete(source, petCreatureListPath)
	catalog, err := NewPVFCatalog(source)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := NewPetGrowthEngine(catalog)
	if err != nil {
		t.Fatal(err)
	}
	state := PetGrowthState{ItemID: 10, Experience: 1, Level: 1, Satiety: 100}
	if _, err := engine.ApplyDungeonClear(state, 1); err == nil {
		t.Fatal("missing creature evolution PVF did not fail closed")
	}
}

func TestPetGrowthSatietyUsesTypedArtifactRatesAndTownRecovery(t *testing.T) {
	catalog := newPetGrowthTestCatalog(t, false)
	engine, err := NewPetGrowthEngine(catalog)
	if err != nil {
		t.Fatal(err)
	}
	modifiers, err := catalog.ResolveSatietyModifiers([]int64{20, 20})
	if err != nil {
		t.Fatal(err)
	}
	if modifiers.FoodConsumeRatePercent() != 50 || modifiers.FoodConsumeMultiplier() != 1.5 {
		t.Fatalf("modifiers=%+v multiplier=%v", modifiers, modifiers.FoodConsumeMultiplier())
	}
	state := PetGrowthState{ItemID: 10, Experience: 0, Level: 1, Satiety: 10}
	dungeon, err := engine.ApplyDungeonElapsed(state, time.Minute, modifiers)
	if err != nil {
		t.Fatal(err)
	}
	if dungeon.After.Satiety != 8 || dungeon.SatietyDelta != -2 || !dungeon.Changed {
		t.Fatalf("dungeon=%+v", dungeon)
	}
	town, err := engine.ApplyTownElapsed(dungeon.After, 360*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if town.After.Satiety != 9 || town.SatietyDelta != 1 || !town.Changed {
		t.Fatalf("town=%+v", town)
	}
	reduction, err := catalog.ResolveSatietyModifiers([]int64{21})
	if err != nil {
		t.Fatal(err)
	}
	if reduction.FoodConsumeMultiplier() != 0.01 {
		t.Fatalf("clamped multiplier=%v", reduction.FoodConsumeMultiplier())
	}
}

func TestPetGrowthSatietyPreservesFractionalGaugeAcrossSettlements(t *testing.T) {
	engine, err := NewPetGrowthEngine(newPetGrowthTestCatalog(t, false))
	if err != nil {
		t.Fatal(err)
	}
	state := PetGrowthState{ItemID: 10, Level: 1, Satiety: 100}
	first, err := engine.ApplyDungeonElapsed(state, 30*time.Second, PetSatietyModifiers{})
	if err != nil {
		t.Fatal(err)
	}
	if first.After.Satiety != 99 || first.After.SatietyMicros != 99_500_000 {
		t.Fatalf("first=%+v", first)
	}
	second, err := engine.ApplyDungeonElapsed(first.After, 30*time.Second, PetSatietyModifiers{})
	if err != nil {
		t.Fatal(err)
	}
	if second.After.Satiety != 99 || second.After.SatietyMicros != 99_000_000 {
		t.Fatalf("second=%+v", second)
	}

	empty := PetGrowthState{ItemID: 10, Level: 1, Satiety: 0}
	recovery1, err := engine.ApplyTownElapsed(empty, 180*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if recovery1.After.Satiety != 0 || recovery1.After.SatietyMicros != 500_000 {
		t.Fatalf("recovery1=%+v", recovery1)
	}
	recovery2, err := engine.ApplyTownElapsed(recovery1.After, 180*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if recovery2.After.Satiety != 1 || recovery2.After.SatietyMicros != 1_000_000 {
		t.Fatalf("recovery2=%+v", recovery2)
	}
}

func TestPetGrowthRejectsInvalidStateElapsedAndArtifact(t *testing.T) {
	catalog := newPetGrowthTestCatalog(t, false)
	engine, err := NewPetGrowthEngine(catalog)
	if err != nil {
		t.Fatal(err)
	}
	invalid := PetGrowthState{ItemID: 10, Level: 1, Satiety: 101}
	if _, err := engine.ApplyTownElapsed(invalid, time.Second); !errors.Is(err, ErrPetGrowthStateInvalid) {
		t.Fatalf("state error=%v", err)
	}
	valid := PetGrowthState{ItemID: 10, Level: 1, Satiety: 10}
	if _, err := engine.ApplyTownElapsed(valid, -time.Second); !errors.Is(err, ErrPetGrowthElapsedInvalid) {
		t.Fatalf("elapsed error=%v", err)
	}
	if _, err := catalog.ResolveSatietyModifiers([]int64{10}); !errors.Is(err, ErrPetPVFArtifactInvalid) {
		t.Fatalf("artifact error=%v", err)
	}
}

func TestRealScriptPVFPetGrowthEvolutionAndArtifactRate(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to verify runtime pet growth PVF")
	}
	archive, err := platformpvf.LoadArchive(platformpvf.Options{Path: pvfPath})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := NewPVFCatalog(archive)
	if err != nil {
		t.Fatal(err)
	}
	evolution, found, err := catalog.ResolveEvolution(63000)
	if err != nil {
		t.Fatal(err)
	}
	if !found || evolution.CurrentCreatureID != 1 || evolution.TargetCreatureID != 2 ||
		evolution.TargetItemID != 63001 || evolution.RequiredLevel != 20 || evolution.RequiresQuest {
		t.Fatalf("real evolution=%+v found=%t", evolution, found)
	}
	modifiers, err := catalog.ResolveSatietyModifiers([]int64{2747170})
	if err != nil {
		t.Fatal(err)
	}
	if modifiers.FoodConsumeRatePercent() != 50 || modifiers.FoodConsumeMultiplier() != 1.5 {
		t.Fatalf("real artifact modifiers=%+v", modifiers)
	}
	engine, err := NewPetGrowthEngine(catalog)
	if err != nil {
		t.Fatal(err)
	}
	update, err := engine.ApplyDungeonClear(PetGrowthState{
		ItemID: 63000, Experience: 314, Level: 19, Satiety: 100,
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if update.After.Level != 20 || update.After.ItemID != 63001 || !update.Evolution.Changed {
		t.Fatalf("real growth update=%+v", update)
	}
}

func newPetGrowthTestCatalog(t *testing.T, quest bool) *PVFCatalog {
	t.Helper()
	catalog, err := NewPVFCatalog(petGrowthTestSource(quest))
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func petGrowthTestSource(quest bool) petCatalogTestSource {
	questValue := "0"
	if quest {
		questValue = "`quest/faras.qst`"
	}
	return petCatalogTestSource{
		petEquipmentListPath: "10 `creature/faras.equ`\n11 `creature/pentabas.equ`\n" +
			"20 `creature/artifact_red/test.equ`\n21 `creature/artifact_blue/reduction.equ`\n" +
			"30 `weapon/support/test.equ`\n",
		"equipment/creature/faras.equ":                   "[equipment type] `[creature]`\n[output index] 11\n",
		"equipment/creature/pentabas.equ":                "[equipment type] `[creature]`\n",
		"equipment/creature/artifact_red/test.equ":       "[equipment type] `[artifact red]`\n[creature food consume rate] 50\n",
		"equipment/creature/artifact_blue/reduction.equ": "[equipment type] `[artifact blue]`\n[creature food consume rate] -200\n",
		"equipment/weapon/support/test.equ":              "[equipment type] `[support weapon]`\n",
		petCreatureExperiencePath:                        petCatalogTestExperienceText(),
		petCreatureListPath:                              "1 `faras/faras.cre`\n2 `pentabas/pentabas.cre`\n",
		"creature/faras/faras.cre": "[evolution quest] " + questValue + "\n" +
			"[evolution creature id] 2\n[evolution level] 2\n",
		"creature/pentabas/pentabas.cre": "[evolution quest] 0\n[evolution creature id] 0\n[evolution level] 0\n",
	}
}
