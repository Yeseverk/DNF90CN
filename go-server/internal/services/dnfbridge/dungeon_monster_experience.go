package dnfbridge

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"longheng.io/server/internal/modules/dnf/premium"
	"longheng.io/server/internal/modules/dnf/progression"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

var (
	errCurrentDungeonMonsterExperienceUnavailable  = errors.New("current dungeon monster experience is unavailable")
	errCurrentDungeonMonsterExperiencePVFInvalid   = errors.New("current dungeon monster experience PVF source is invalid")
	errCurrentDungeonMonsterExperienceStateInvalid = errors.New("current dungeon monster experience persisted state is invalid")
)

// currentDungeonMonsterExperienceResources intentionally keeps the current
// runtime PVF tables together.  The formula below takes its sequencing and
// rounding from the user-authorized 86JP domain implementation, but never
// copies its stale hard-coded base EXP table or old packet body.
type currentDungeonMonsterExperienceResources struct {
	Progression *progression.Tables
	Sources     *progression.MonsterExperienceSources
}

type currentDungeonMonsterExperienceResourcesCacheEntry struct {
	resources currentDungeonMonsterExperienceResources
}

var currentDungeonMonsterExperienceResourcesByCatalog sync.Map

func currentDungeonMonsterExperienceResourcesForCatalog(
	catalog *pvfDungeonMonsterCatalog,
) (currentDungeonMonsterExperienceResources, error) {
	if catalog == nil || catalog.source == nil {
		return currentDungeonMonsterExperienceResources{}, errCurrentDungeonMonsterExperienceUnavailable
	}
	if cached, found := currentDungeonMonsterExperienceResourcesByCatalog.Load(catalog); found {
		return cached.(currentDungeonMonsterExperienceResourcesCacheEntry).resources, nil
	}
	tables, err := progression.Load(context.Background(), catalog.source)
	if err != nil {
		return currentDungeonMonsterExperienceResources{}, err
	}
	sources, err := currentDungeonMonsterExperienceSources(catalog)
	if err != nil {
		return currentDungeonMonsterExperienceResources{}, err
	}
	resources := currentDungeonMonsterExperienceResources{Progression: tables, Sources: sources}
	actual, _ := currentDungeonMonsterExperienceResourcesByCatalog.LoadOrStore(
		catalog,
		currentDungeonMonsterExperienceResourcesCacheEntry{resources: resources},
	)
	return actual.(currentDungeonMonsterExperienceResourcesCacheEntry).resources, nil
}

// currentDungeonMonsterExperienceAward records the exact inputs used for one
// accepted hostile death.  It intentionally omits contract, account honor,
// party, and result-window bonuses: none of those has a current Go owner in
// this single-character in-dungeon path.
type currentDungeonMonsterExperienceAward struct {
	MonsterID       int64
	MonsterLevel    int
	MonsterType     byte
	NamedMonster    bool
	MonsterTableEXP uint32
	DungeonWeight   float32
	DifficultyRate  float32
	MonsterTypeRate float32
	LevelPenalty    float32
	PrePenaltyGain  uint32
	Gain            uint32
}

// currentDungeonMonsterExperienceAwardFor reconstructs only the ordinary
// monster EXP domain rule used by 86JP:
//
//	runtime monsterexp.tbl base -> dungeon weight -> difficulty -> type/named
//	-> killer level penalty.
//
// Each cast is deliberately performed at the same boundaries as the C# float
// to int/uint casts.  The base table is the current runtime PVF table because
// the 86JP static table is known to differ from this server's Script.pvf.
func currentDungeonMonsterExperienceAwardFor(
	runtime *runtimeDungeonState,
	monster runtimeDungeonMonster,
	sources *progression.MonsterExperienceSources,
) (currentDungeonMonsterExperienceAward, error) {
	if runtime == nil || sources == nil || runtime.Character.Level <= 0 || monster.Spawn.MonsterID <= 0 {
		return currentDungeonMonsterExperienceAward{}, errCurrentDungeonMonsterExperienceUnavailable
	}
	monsterLevel, err := currentDungeonMonsterLevel(monster.Spawn, runtime.Dungeon.Metadata.BasisLevel)
	if err != nil {
		return currentDungeonMonsterExperienceAward{}, err
	}
	monsterType, err := currentDungeonMonsterType(monster.Spawn.Rank)
	if err != nil {
		return currentDungeonMonsterExperienceAward{}, err
	}
	base, found := sources.MonsterTableValue(int(monsterLevel))
	if !found {
		return currentDungeonMonsterExperienceAward{}, fmt.Errorf(
			"%w: monster level=%d is absent from monsterexp.tbl",
			errCurrentDungeonMonsterExperiencePVFInvalid,
			monsterLevel,
		)
	}

	weight := float32(1)
	if runtime.Dungeon.Metadata.ExperienceIncreasingPoint.Set &&
		runtime.Dungeon.Metadata.ExperienceIncreasingPoint.Value >= 0 {
		weight = float32(runtime.Dungeon.Metadata.ExperienceIncreasingPoint.Value)
	}
	if !currentDungeonMonsterExperienceFiniteNonNegative(weight) {
		return currentDungeonMonsterExperienceAward{}, fmt.Errorf(
			"%w: dungeon weight=%v",
			errCurrentDungeonMonsterExperiencePVFInvalid,
			weight,
		)
	}

	modifiers := sources.RawModifiers()
	difficultyRate := float32(1) // exact 86JP fallback for an out-of-range index.
	difficulty := int(runtime.Request.Difficulty)
	if difficulty >= 0 && difficulty < len(modifiers.DifficultyRates) {
		difficultyRate = float32(modifiers.DifficultyRates[difficulty])
	}
	if !currentDungeonMonsterExperienceFiniteNonNegative(difficultyRate) {
		return currentDungeonMonsterExperienceAward{}, fmt.Errorf(
			"%w: difficulty=%d rate=%v",
			errCurrentDungeonMonsterExperiencePVFInvalid,
			difficulty,
			difficultyRate,
		)
	}

	monsterTypeRate := currentDungeonMonsterCSharpTypeRate(modifiers.MonsterGlobalRates, int(monsterType))
	if !currentDungeonMonsterExperienceFiniteNonNegative(monsterTypeRate) {
		return currentDungeonMonsterExperienceAward{}, fmt.Errorf(
			"%w: monster type=%d rate=%v",
			errCurrentDungeonMonsterExperiencePVFInvalid,
			monsterType,
			monsterTypeRate,
		)
	}
	named := monsterType != 3 && currentDungeonMonsterIsNamed(runtime, monster.Spawn.MonsterID)
	if named {
		monsterTypeRate *= 3
		if !currentDungeonMonsterExperienceFiniteNonNegative(monsterTypeRate) {
			return currentDungeonMonsterExperienceAward{}, fmt.Errorf(
				"%w: named monster type rate=%v",
				errCurrentDungeonMonsterExperiencePVFInvalid,
				monsterTypeRate,
			)
		}
	}

	weightedBase, err := currentDungeonMonsterExperienceCSharpInt(float32(base) * weight)
	if err != nil {
		return currentDungeonMonsterExperienceAward{}, err
	}
	prePenalty, err := currentDungeonMonsterExperienceCSharpInt(
		float32(weightedBase) * difficultyRate * monsterTypeRate,
	)
	if err != nil {
		return currentDungeonMonsterExperienceAward{}, err
	}
	levelPenalty := currentDungeonMonsterCSharpBaseExpPenalty(runtime.Character.Level, int(monsterLevel))
	gain, err := currentDungeonMonsterExperienceCSharpUint(float32(prePenalty) * levelPenalty)
	if err != nil {
		return currentDungeonMonsterExperienceAward{}, err
	}
	return currentDungeonMonsterExperienceAward{
		MonsterID:       monster.Spawn.MonsterID,
		MonsterLevel:    int(monsterLevel),
		MonsterType:     monsterType,
		NamedMonster:    named,
		MonsterTableEXP: base,
		DungeonWeight:   weight,
		DifficultyRate:  difficultyRate,
		MonsterTypeRate: monsterTypeRate,
		LevelPenalty:    levelPenalty,
		PrePenaltyGain:  prePenalty,
		Gain:            gain,
	}, nil
}

func currentDungeonMonsterIsNamed(runtime *runtimeDungeonState, monsterID int64) bool {
	if runtime == nil || monsterID <= 0 {
		return false
	}
	for _, namedID := range runtime.Dungeon.Metadata.NamedMonsters {
		if namedID == monsterID {
			return true
		}
	}
	return false
}

// currentDungeonMonsterCSharpTypeRate preserves the 86JP range fallback:
// missing or out-of-range monster-type entries use index zero, and an empty
// rate list is neutral.  Current runtime PVF has one entry, but do not invent
// distinct rank rates where its typed source does not provide them.
func currentDungeonMonsterCSharpTypeRate(values []float64, monsterType int) float32 {
	if len(values) == 0 {
		return 1
	}
	if monsterType < 0 || monsterType >= len(values) {
		return float32(values[0])
	}
	return float32(values[monsterType])
}

// currentDungeonMonsterCSharpBaseExpPenalty is the authorized 86JP domain
// rule (MonsterRewardTable.BaseExpPenalty), including its truncation inputs.
// It is not a guessed interpretation of worldmapexppenaltytable.etc.
func currentDungeonMonsterCSharpBaseExpPenalty(characterLevel, monsterLevel int) float32 {
	difference := monsterLevel - characterLevel
	if difference <= -7 {
		return 0.05
	}
	switch difference {
	case -6:
		return 0.20
	case -5:
		return 0.50
	case -4:
		return 0.75
	case -3, -2, -1, 0:
		return 1
	case 1, 2, 3:
		return 1.12
	case 4, 5:
		return 1
	case 6:
		return 0.75
	case 7:
		return 0.70
	case 8:
		return 0.60
	case 9:
		return 0.50
	default:
		return 0.05
	}
}

func currentDungeonMonsterExperienceFiniteNonNegative(value float32) bool {
	return !math.IsNaN(float64(value)) && !math.IsInf(float64(value), 0) && value >= 0
}

// C# CalcBaseExp and CalcExp both cast their non-negative float result to
// Int32.  Rejecting an out-of-range current PVF product is safer than allowing
// Go's conversion semantics to create a fabricated reward.
func currentDungeonMonsterExperienceCSharpInt(value float32) (uint32, error) {
	if !currentDungeonMonsterExperienceFiniteNonNegative(value) || value > math.MaxInt32 {
		return 0, fmt.Errorf("%w: C# int product=%v", errCurrentDungeonMonsterExperiencePVFInvalid, value)
	}
	return uint32(value), nil
}

// C# applies BaseExpPenalty after CalcExp and then casts the product to UInt32.
func currentDungeonMonsterExperienceCSharpUint(value float32) (uint32, error) {
	if !currentDungeonMonsterExperienceFiniteNonNegative(value) || value > math.MaxUint32 {
		return 0, fmt.Errorf("%w: C# uint product=%v", errCurrentDungeonMonsterExperiencePVFInvalid, value)
	}
	return uint32(value), nil
}

type currentDungeonMonsterExperienceCommitResult struct {
	Character  dnfrepo.CharacterRecord
	Skill      dnfrepo.SkillRecord
	Award      currentDungeonMonsterExperienceAward
	SPGain     int
	TPGain     int
	Applied    bool
	Idempotent bool
	// GrowthContractBonus is the 成长契约 (premium type 84) +20% bonus
	// experience added on top of Award.Gain at commit time.
	GrowthContractBonus uint32
	// HonorExpertGain is the accepted part of this award earned after the
	// DNF90 character-level cap.
	HonorExpertGain uint32
}

// recordCurrentDungeonMonsterExperience owns result-window statistics only.
// Monster experience is committed per accepted death and must not be granted
// again while building the settlement packet.
func recordCurrentDungeonMonsterExperience(
	runtime *runtimeDungeonState,
	monster runtimeDungeonMonster,
	result currentDungeonMonsterExperienceCommitResult,
) {
	if runtime == nil || result.Award.MonsterID <= 0 {
		return
	}
	base := result.Award.PrePenaltyGain
	runtime.settlementMonsterExperienceTotal = saturatingCurrentDungeonUint32Add(
		runtime.settlementMonsterExperienceTotal,
		base,
	)
	runtime.settlementMonsterGrowthContractBonus = saturatingCurrentDungeonUint32Add(
		runtime.settlementMonsterGrowthContractBonus,
		result.GrowthContractBonus,
	)
	actorType, err := currentDungeonMonsterActorType(monster.Spawn)
	if err != nil {
		actorType = result.Award.MonsterType
	}
	switch actorType {
	case 1:
		runtime.settlementChampionExperience = saturatingCurrentDungeonUint32Add(
			runtime.settlementChampionExperience,
			base,
		)
	case 2:
		if !result.Award.NamedMonster {
			runtime.settlementSuperChampionExperience = saturatingCurrentDungeonUint32Add(
				runtime.settlementSuperChampionExperience,
				base,
			)
		}
	case 3:
		runtime.settlementBossExperience = saturatingCurrentDungeonUint32Add(
			runtime.settlementBossExperience,
			base,
		)
	}
}

// awardCurrentDungeonMonsterExperience commits only after the room's
// authoritative actor-death transition has succeeded.  The room owner makes
// that transition one-time for the active dungeon runtime; the progression
// UoW then keeps EXP/level and the SP ledger indivisible.
func (s *Service) awardCurrentDungeonMonsterExperience(
	ctx context.Context,
	session *gameSession,
	runtime *runtimeDungeonState,
	monster runtimeDungeonMonster,
) (currentDungeonMonsterExperienceCommitResult, error) {
	if s == nil || session == nil || runtime == nil || session.selectedCharacterID == 0 {
		return currentDungeonMonsterExperienceCommitResult{}, errCurrentDungeonMonsterExperienceUnavailable
	}
	catalog, err := s.dungeonMonsterCatalog()
	if err != nil {
		return currentDungeonMonsterExperienceCommitResult{}, err
	}
	resources, err := currentDungeonMonsterExperienceResourcesForCatalog(catalog)
	if err != nil {
		return currentDungeonMonsterExperienceCommitResult{}, err
	}
	award, err := currentDungeonMonsterExperienceAwardFor(runtime, monster, resources.Sources)
	if err != nil {
		return currentDungeonMonsterExperienceCommitResult{}, err
	}
	result := currentDungeonMonsterExperienceCommitResult{Award: award}
	if award.Gain == 0 {
		return result, nil
	}
	return s.commitCurrentDungeonMonsterExperience(ctx, session, runtime, resources.Progression, result)
}

func (s *Service) commitCurrentDungeonMonsterExperience(
	ctx context.Context,
	session *gameSession,
	runtime *runtimeDungeonState,
	tables *progression.Tables,
	result currentDungeonMonsterExperienceCommitResult,
) (currentDungeonMonsterExperienceCommitResult, error) {
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Character == nil || repositories.Skill == nil ||
		repositories.CharacterProgression == nil || tables == nil || result.Award.Gain == 0 {
		return currentDungeonMonsterExperienceCommitResult{}, errCurrentDungeonMonsterExperienceUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	characterID := strconv.FormatUint(uint64(session.selectedCharacterID), 10)
	accountID := strings.TrimSpace(s.accountIDForSession(session))
	if accountID == "" || runtime.Character.CharacterID != characterID {
		return currentDungeonMonsterExperienceCommitResult{}, errCurrentDungeonMonsterExperienceStateInvalid
	}
	character, found, err := repositories.Character.Load(ctx, characterID)
	if err != nil {
		return currentDungeonMonsterExperienceCommitResult{}, err
	}
	if !found || character.CharacterID != characterID || strings.TrimSpace(character.AccountID) != accountID {
		return currentDungeonMonsterExperienceCommitResult{}, errCurrentDungeonMonsterExperienceStateInvalid
	}
	skill, found, err := repositories.Skill.Load(ctx, characterID)
	if err != nil {
		return currentDungeonMonsterExperienceCommitResult{}, err
	}
	if !found || skill.CharacterID != characterID {
		return currentDungeonMonsterExperienceCommitResult{}, errCurrentDungeonMonsterExperienceStateInvalid
	}
	experience, present := character.Stats["exp"]
	runtimeExperience, runtimePresent := runtime.Character.Stats["exp"]
	if !present || !runtimePresent || experience < 0 || uint64(experience) > math.MaxUint32 ||
		character.Level <= 0 || character.Level != runtime.Character.Level || experience != runtimeExperience ||
		skill.Points.SyncedLevel != character.Level {
		return currentDungeonMonsterExperienceCommitResult{}, errCurrentDungeonMonsterExperienceStateInvalid
	}
	// 成长契约 (premium type 84): +20% monster kill experience while active.
	// The total is what gets planned and committed; the base Gain stays the
	// pure PVF/86JP monster award for logging and tests.
	growthBonus := s.currentGrowthContractBonusExp(ctx, accountID, result.Award.Gain)
	totalGain := result.Award.Gain + growthBonus
	result.GrowthContractBonus = growthBonus
	honorExpertGain, err := currentHonorExpertExperienceGain(
		tables,
		character.Level,
		uint32(experience),
		totalGain,
	)
	if err != nil {
		return currentDungeonMonsterExperienceCommitResult{}, err
	}
	planned, err := planCurrentDungeonSettlementProgression(
		tables,
		character.Level,
		uint32(experience),
		totalGain,
		skill.Points,
	)
	if err != nil {
		return currentDungeonMonsterExperienceCommitResult{}, err
	}
	commit := progression.CommitPlanFromExperienceSkillPoints(characterID, accountID, planned)
	if planned.Experience.LevelsGained > 0 {
		levelStats, statErr := s.characterPVFStatValuesForLevel(character, planned.Experience.NewLevel)
		if statErr != nil {
			return currentDungeonMonsterExperienceCommitResult{}, statErr
		}
		commit.NextCharacterStats = levelStats
	}
	if honorExpertGain > 0 {
		honorTables, honorErr := s.loadHonorTable(ctx)
		if honorErr != nil {
			return currentDungeonMonsterExperienceCommitResult{}, honorErr
		}
		currentHonor, honorErr := currentHonorExpertProgress(character)
		if honorErr != nil {
			return currentDungeonMonsterExperienceCommitResult{}, honorErr
		}
		nextHonor, honorErr := planCurrentHonorExpertProgress(honorTables, character, honorExpertGain)
		if honorErr != nil {
			return currentDungeonMonsterExperienceCommitResult{}, honorErr
		}
		commit.ExpectedCharacterStats = currentHonorExpertStats(currentHonor)
		if commit.NextCharacterStats == nil {
			commit.NextCharacterStats = make(map[string]int64, 2)
		}
		for key, value := range currentHonorExpertStats(nextHonor) {
			commit.NextCharacterStats[key] = value
		}
		result.HonorExpertGain = honorExpertGain
	}
	committed, err := progression.Commit(ctx, repositories.CharacterProgression, commit)
	if err != nil {
		return currentDungeonMonsterExperienceCommitResult{}, err
	}
	character = dnfrepo.CloneCharacter(character)
	if character.Stats == nil {
		return currentDungeonMonsterExperienceCommitResult{}, errCurrentDungeonMonsterExperienceStateInvalid
	}
	character.Level = committed.Current.Level
	character.Stats["exp"] = int64(committed.Current.Experience)
	for key, value := range commit.NextCharacterStats {
		character.Stats[key] = value
	}
	skill = dnfrepo.CloneSkill(skill)
	skill.Points = committed.Current.SkillPoints
	result.Character = character
	result.Skill = skill
	result.SPGain = planned.SkillPoints.SPGain
	result.TPGain = planned.SkillPoints.TPGain
	result.Applied = committed.Applied
	result.Idempotent = committed.Idempotent
	return result, nil
}

// currentGrowthContractEffect resolves the live account entitlement and the
// effect values from the active runtime PVF. A missing account, expired
// contract, or unavailable PVF catalog is inactive rather than inventing a
// compatibility multiplier.
func (s *Service) currentGrowthContractEffect(ctx context.Context, accountID string) (currentPremiumEffectInfo, bool) {
	if s == nil || strings.TrimSpace(accountID) == "" {
		return currentPremiumEffectInfo{}, false
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Account == nil {
		return currentPremiumEffectInfo{}, false
	}
	account, found, err := repositories.Account.Load(ctx, strings.TrimSpace(accountID))
	if err != nil || !found {
		return currentPremiumEffectInfo{}, false
	}
	if !premium.Active(account, premium.TypeBonusExp, time.Now().UTC()) {
		return currentPremiumEffectInfo{}, false
	}
	catalog, err := s.currentPremiumCatalog()
	if err != nil || catalog == nil {
		return currentPremiumEffectInfo{}, false
	}
	effect, found := catalog.effectsByType[premium.TypeBonusExp]
	if !found {
		return currentPremiumEffectInfo{}, false
	}
	return effect, true
}

// currentGrowthContractBonusExp returns the runtime-PVF [bonus exp]
// percentage while Growth Contract is active. The bonus saturates so the
// committed base+bonus never overflows the current-client u32 experience
// field.
func (s *Service) currentGrowthContractBonusExp(ctx context.Context, accountID string, base uint32) uint32 {
	if base == 0 {
		return 0
	}
	effect, active := s.currentGrowthContractEffect(ctx, accountID)
	if !active {
		return 0
	}
	return currentPercentageBonusUint32(base, effect.BonusExperiencePercent)
}

func currentPercentageBonusUint32(base uint32, percent int64) uint32 {
	if base == 0 || percent <= 0 {
		return 0
	}
	limit := uint64(math.MaxUint32 - base)
	if uint64(percent) > math.MaxUint64/uint64(base) {
		return uint32(limit)
	}
	bonus := uint64(base) * uint64(percent) / 100
	if bonus > limit {
		bonus = limit
	}
	return uint32(bonus)
}
