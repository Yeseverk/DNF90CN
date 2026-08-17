package pet

import (
	"errors"
	"fmt"
	"math"
	"time"
)

const MaxCreatureExperience int64 = math.MaxInt32

const petSatietyScale int64 = 1_000_000

var (
	ErrPetGrowthCatalogRequired = errors.New("pet growth catalog is required")
	ErrPetGrowthStateInvalid    = errors.New("pet growth state is invalid")
	ErrPetGrowthElapsedInvalid  = errors.New("pet growth elapsed duration is invalid")
)

// PetGrowthState contains the typed fields changed by the pure growth domain.
type PetGrowthState struct {
	ItemID     int64
	Experience int64
	Level      int
	Satiety    int
	// SatietyMicros is the authoritative continuous gauge. A zero value with a
	// positive Satiety is treated as a legacy integer-only state.
	SatietyMicros int64
}

// PetEvolutionUpdate distinguishes an automatic item transition from an
// eligible quest-gated transition.
type PetEvolutionUpdate struct {
	Definition    PetEvolutionDefinition
	Changed       bool
	QuestEligible bool
}

// PetGrowthUpdate is a pure before/after result; persistence is owned by the
// caller's aggregate transaction.
type PetGrowthUpdate struct {
	Before           PetGrowthState
	After            PetGrowthState
	ExperienceGained int64
	Evolution        PetEvolutionUpdate
}

// PetSatietyUpdate is the pure result of one elapsed-time calculation.
type PetSatietyUpdate struct {
	Before         PetGrowthState
	After          PetGrowthState
	Elapsed        time.Duration
	SatietyDelta   int
	Changed        bool
	FoodMultiplier float64
}

// PetGrowthEngine applies 86JP's domain rules using only typed PVF catalog
// inputs. It has no repository, clock, timer, or protocol responsibility.
type PetGrowthEngine struct {
	experience CreatureExperienceTable
	evolution  PetEvolutionResolver
	creatures  PetCreatureResolver
}

func NewPetGrowthEngine(catalog *PVFCatalog) (*PetGrowthEngine, error) {
	if catalog == nil || catalog.source == nil {
		return nil, ErrPetGrowthCatalogRequired
	}
	return &PetGrowthEngine{experience: catalog.ExperienceTable(), evolution: catalog, creatures: catalog}, nil
}

func (e *PetGrowthEngine) resolveCreature(itemID int64) (PetCreatureDefinition, error) {
	if e == nil || e.creatures == nil {
		return PetCreatureDefinition{}, ErrPetGrowthCatalogRequired
	}
	definition, err := e.creatures.ResolveCreature(itemID)
	if err != nil {
		return PetCreatureDefinition{}, err
	}
	if definition.ItemID != itemID || definition.PVFPath == "" {
		return PetCreatureDefinition{}, fmt.Errorf("%w: item_id=%d definition=%+v", ErrPetGrowthCatalogRequired, itemID, definition)
	}
	return definition, nil
}

// ApplyDungeonClear grants cumulative creature experience equal to consumed
// fatigue only while satiety is positive. A single clear can trigger at most
// one automatic evolution, matching the 86JP domain owner.
func (e *PetGrowthEngine) ApplyDungeonClear(state PetGrowthState, consumedFatigue int) (PetGrowthUpdate, error) {
	return e.applyDungeonClear(state, consumedFatigue, true)
}

// applyDungeonClear keeps current online progression independent from the
// still opt-in evolution route. Runtime callers pass resolveEvolution=false
// until the current-client evolution wire is closed; that path must not lose
// earned experience merely because a valid creature equipment document has no
// creature.lst identity.
func (e *PetGrowthEngine) applyDungeonClear(state PetGrowthState, consumedFatigue int, resolveEvolution bool) (PetGrowthUpdate, error) {
	if e == nil || (resolveEvolution && e.evolution == nil) {
		return PetGrowthUpdate{}, ErrPetGrowthCatalogRequired
	}
	if err := validatePetGrowthState(state); err != nil {
		return PetGrowthUpdate{}, err
	}
	update := PetGrowthUpdate{Before: state, After: state}
	if consumedFatigue <= 0 || petSatietyMicros(state) <= 0 || state.Level >= MaxCreatureLevel {
		return update, nil
	}
	gained := int64(consumedFatigue)
	afterExperience := state.Experience
	if gained >= MaxCreatureExperience-afterExperience {
		afterExperience = MaxCreatureExperience
	} else {
		afterExperience += gained
	}
	afterLevel := e.experience.LevelForExperience(afterExperience)
	if afterLevel < state.Level {
		afterLevel = state.Level
	}
	if afterLevel > MaxCreatureLevel {
		afterLevel = MaxCreatureLevel
	}
	update.After.Experience = afterExperience
	update.After.Level = afterLevel
	update.ExperienceGained = afterExperience - state.Experience
	if !resolveEvolution || afterLevel <= state.Level {
		return update, nil
	}
	definition, found, err := e.evolution.ResolveEvolution(state.ItemID)
	if err != nil {
		return PetGrowthUpdate{}, fmt.Errorf("resolve pet evolution: %w", err)
	}
	if !found || afterLevel < definition.RequiredLevel {
		return update, nil
	}
	update.Evolution.Definition = definition
	if definition.RequiresQuest {
		update.Evolution.QuestEligible = true
		return update, nil
	}
	update.After.ItemID = definition.TargetItemID
	update.Evolution.Changed = true
	return update, nil
}

// ApplyDungeonElapsed consumes one satiety point per elapsed minute multiplied
// by the typed artifact modifier. Positive fractional remainder is truncated;
// a still-positive creature remains visible with at least one satiety.
func (e *PetGrowthEngine) ApplyDungeonElapsed(state PetGrowthState, elapsed time.Duration, modifiers PetSatietyModifiers) (PetSatietyUpdate, error) {
	if e == nil {
		return PetSatietyUpdate{}, ErrPetGrowthCatalogRequired
	}
	if err := validatePetGrowthState(state); err != nil {
		return PetSatietyUpdate{}, err
	}
	if elapsed < 0 {
		return PetSatietyUpdate{}, ErrPetGrowthElapsedInvalid
	}
	multiplier := modifiers.FoodConsumeMultiplier()
	update := PetSatietyUpdate{Before: state, After: state, Elapsed: elapsed, FoodMultiplier: multiplier}
	beforeMicros := petSatietyMicros(state)
	if elapsed == 0 || beforeMicros <= 0 {
		return update, nil
	}
	consumedMicros := int64(math.Round(elapsed.Seconds() / 60 * multiplier * float64(petSatietyScale)))
	afterMicros := beforeMicros - consumedMicros
	if afterMicros < 0 {
		afterMicros = 0
	}
	after := visiblePetSatietyFromMicros(afterMicros, true)
	update.After.Satiety = after
	update.After.SatietyMicros = afterMicros
	update.SatietyDelta = after - state.Satiety
	update.Changed = afterMicros != beforeMicros
	return update, nil
}

// ApplyTownElapsed restores one satiety point per 360 elapsed seconds, capped
// at 100. Artifact modifiers do not affect recovery.
func (e *PetGrowthEngine) ApplyTownElapsed(state PetGrowthState, elapsed time.Duration) (PetSatietyUpdate, error) {
	if e == nil {
		return PetSatietyUpdate{}, ErrPetGrowthCatalogRequired
	}
	if err := validatePetGrowthState(state); err != nil {
		return PetSatietyUpdate{}, err
	}
	if elapsed < 0 {
		return PetSatietyUpdate{}, ErrPetGrowthElapsedInvalid
	}
	update := PetSatietyUpdate{Before: state, After: state, Elapsed: elapsed, FoodMultiplier: 1}
	beforeMicros := petSatietyMicros(state)
	if elapsed == 0 || beforeMicros >= 100*petSatietyScale {
		return update, nil
	}
	recoveredMicros := int64(math.Round(elapsed.Seconds() / 360 * float64(petSatietyScale)))
	afterMicros := beforeMicros + recoveredMicros
	if afterMicros > 100*petSatietyScale {
		afterMicros = 100 * petSatietyScale
	}
	after := visiblePetSatietyFromMicros(afterMicros, false)
	update.After.Satiety = after
	update.After.SatietyMicros = afterMicros
	update.SatietyDelta = after - state.Satiety
	update.Changed = afterMicros != beforeMicros
	return update, nil
}

func petSatietyMicros(state PetGrowthState) int64 {
	if state.SatietyMicros > 0 || state.Satiety == 0 {
		return state.SatietyMicros
	}
	return int64(state.Satiety) * petSatietyScale
}

func visiblePetSatietyFromMicros(value int64, clampAliveMinimum bool) int {
	if value <= 0 {
		return 0
	}
	if value >= 100*petSatietyScale {
		return 100
	}
	visible := int(value / petSatietyScale)
	if visible == 0 && clampAliveMinimum {
		return 1
	}
	return visible
}

func validatePetGrowthState(state PetGrowthState) error {
	if state.ItemID <= 0 || state.ItemID > math.MaxUint32 ||
		state.Experience < 0 || state.Experience > MaxCreatureExperience ||
		state.Level < 1 || state.Level > MaxCreatureLevel ||
		state.Satiety < 0 || state.Satiety > 100 ||
		state.SatietyMicros < 0 || state.SatietyMicros > 100*petSatietyScale {
		return fmt.Errorf("%w: %+v", ErrPetGrowthStateInvalid, state)
	}
	return nil
}
