package dnfbridge

import (
	"errors"
	"math"
	"sync"

	"longheng.io/server/internal/modules/dnf/progression"
)

var errCurrentDungeonMonsterRewardSourceUnavailable = errors.New("current dungeon monster reward source is unavailable")

var currentDungeonMonsterExperienceSourcesByCatalog sync.Map

type currentDungeonMonsterExperienceSourceCacheEntry struct {
	sources *progression.MonsterExperienceSources
}

type currentDungeonMonsterPenaltySourceCandidate struct {
	Key   int
	Rate  float64
	Found bool
}

type currentDungeonMonsterExperienceSourceEvidence struct {
	CharacterLevel int
	MonsterLevel   int
	MonsterTable   uint32
	DefinitionEXP  int64

	MonsterGlobalRates []float64
	PartyRate          float64
	StarterPartyRate   float64
	DifficultyIndex    int
	DifficultyRate     float64

	DungeonID                 int64
	DungeonRate               float64
	DungeonRateFound          bool
	DungeonLocalRate          float64
	DungeonLocalRateSpecified bool

	MonsterMinusCharacter currentDungeonMonsterPenaltySourceCandidate
	CharacterMinusMonster currentDungeonMonsterPenaltySourceCandidate

	AwardReady bool
	Blockers   []string
}

type currentDungeonMonsterDropSourceEvidence struct {
	RandomDropCount int64
	FixedDropCount  int64
	ExplicitPool    []dungeonMonsterDropPoolEntry
	ExplicitWeight  uint64

	ExplicitPoolCompatibilityReady bool
	GenericDropAwardReady          bool
	Blockers                       []string
}

type currentDungeonMonsterRewardSourceEvidence struct {
	MonsterID      int64
	SpawnRank      string
	DefinitionRank string
	DefinitionKind string
	WireActorType  byte
	Experience     currentDungeonMonsterExperienceSourceEvidence
	Drop           currentDungeonMonsterDropSourceEvidence
	FullAwardReady bool
}

// currentDungeonMonsterRewardSources is a read-only evidence plan. It keeps
// values from the active runtime, current PVF monster table, and current PVF
// reward tables together without inventing the server-side combination rule.
// In particular, it does not calculate an award, persist progression, roll the
// generic drop matrices, or imply an op38/op37 packet order.
func currentDungeonMonsterRewardSources(
	runtime *runtimeDungeonState,
	monster runtimeDungeonMonster,
	sources *progression.MonsterExperienceSources,
	explicitPool []dungeonMonsterDropPoolEntry,
) (currentDungeonMonsterRewardSourceEvidence, error) {
	if runtime == nil || sources == nil || runtime.Dungeon.ID <= 0 || runtime.Character.Level <= 0 ||
		monster.Spawn.MonsterID <= 0 {
		return currentDungeonMonsterRewardSourceEvidence{}, errCurrentDungeonMonsterRewardSourceUnavailable
	}
	monsterLevel, err := currentDungeonMonsterLevel(monster.Spawn, runtime.Dungeon.Metadata.BasisLevel)
	if err != nil {
		return currentDungeonMonsterRewardSourceEvidence{}, err
	}
	wireActorType, err := currentDungeonMonsterType(monster.Spawn.Rank)
	if err != nil {
		return currentDungeonMonsterRewardSourceEvidence{}, err
	}
	sourcePlan, err := sources.PlanSource(
		int(monsterLevel),
		1,
		int(runtime.Request.Difficulty),
		runtime.Dungeon.ID,
	)
	if err != nil {
		return currentDungeonMonsterRewardSourceEvidence{}, err
	}
	raw := sources.RawModifiers()
	monsterMinusCharacterKey := int(monsterLevel) - runtime.Character.Level
	characterMinusMonsterKey := runtime.Character.Level - int(monsterLevel)
	monsterMinusCharacterRate, monsterMinusCharacterFound := sources.PenaltyRateBySourceKey(monsterMinusCharacterKey)
	characterMinusMonsterRate, characterMinusMonsterFound := sources.PenaltyRateBySourceKey(characterMinusMonsterKey)

	experienceBlockers := make([]string, 0, len(sourcePlan.Blockers)+3)
	for _, blocker := range sourcePlan.Blockers {
		experienceBlockers = append(experienceBlockers, string(blocker))
	}
	experienceBlockers = append(experienceBlockers,
		"monster_definition_exp_semantics_unproved",
		"dungeon_local_experience_rate_composition_unproved",
		"op38_op37_natural_order_unproved",
	)

	dropPool := cloneDungeonMonsterDropPool(explicitPool)
	var explicitWeight uint64
	explicitPoolValid := len(dropPool) > 0
	coinBranchPresent := false
	for _, entry := range dropPool {
		if entry.ItemID == 0 || entry.Weight == 0 || math.MaxUint64-explicitWeight < uint64(entry.Weight) {
			explicitPoolValid = false
			continue
		}
		if entry.ItemID == 1199 {
			coinBranchPresent = true
		}
		explicitWeight += uint64(entry.Weight)
	}
	explicitPoolReady := monster.Spawn.FixedDropCount > 0 && monster.Spawn.FixedDropCount <= math.MaxUint8 &&
		explicitPoolValid && explicitWeight > 0 && !coinBranchPresent
	dropBlockers := []string{
		"random_drop_count_semantics_unproved",
		"generic_drop_matrix_axes_unproved",
		"generic_drop_probability_and_rarity_selection_unproved",
	}
	if coinBranchPresent {
		dropBlockers = append(dropBlockers, "coin_gold_drop_branch_unproved")
	}

	return currentDungeonMonsterRewardSourceEvidence{
		MonsterID:      monster.Spawn.MonsterID,
		SpawnRank:      monster.Spawn.Rank,
		DefinitionRank: monster.Definition.Rank,
		DefinitionKind: monster.Definition.Kind,
		WireActorType:  wireActorType,
		Experience: currentDungeonMonsterExperienceSourceEvidence{
			CharacterLevel: runtime.Character.Level,
			MonsterLevel:   int(monsterLevel),
			MonsterTable:   sourcePlan.MonsterTableValue,
			DefinitionEXP:  monster.Definition.Exp,

			MonsterGlobalRates: append([]float64(nil), raw.MonsterGlobalRates...),
			PartyRate:          sourcePlan.PartyRate,
			StarterPartyRate:   sourcePlan.StarterPartyRate,
			DifficultyIndex:    sourcePlan.DifficultyIndex,
			DifficultyRate:     sourcePlan.DifficultyRate,

			DungeonID:                 sourcePlan.DungeonID,
			DungeonRate:               sourcePlan.DungeonIncreasingRate,
			DungeonRateFound:          sourcePlan.DungeonRateFound,
			DungeonLocalRate:          runtime.Dungeon.Metadata.ExperienceIncreasingPoint.Value,
			DungeonLocalRateSpecified: runtime.Dungeon.Metadata.ExperienceIncreasingPoint.Set,

			MonsterMinusCharacter: currentDungeonMonsterPenaltySourceCandidate{
				Key: monsterMinusCharacterKey, Rate: monsterMinusCharacterRate, Found: monsterMinusCharacterFound,
			},
			CharacterMinusMonster: currentDungeonMonsterPenaltySourceCandidate{
				Key: characterMinusMonsterKey, Rate: characterMinusMonsterRate, Found: characterMinusMonsterFound,
			},

			AwardReady: false,
			Blockers:   experienceBlockers,
		},
		Drop: currentDungeonMonsterDropSourceEvidence{
			RandomDropCount:                monster.Spawn.RandomDropCount,
			FixedDropCount:                 monster.Spawn.FixedDropCount,
			ExplicitPool:                   dropPool,
			ExplicitWeight:                 explicitWeight,
			ExplicitPoolCompatibilityReady: explicitPoolReady,
			GenericDropAwardReady:          false,
			Blockers:                       dropBlockers,
		},
		FullAwardReady: false,
	}, nil
}

func currentDungeonMonsterExperienceSources(
	catalog *pvfDungeonMonsterCatalog,
) (*progression.MonsterExperienceSources, error) {
	if catalog == nil || catalog.source == nil {
		return nil, errCurrentDungeonMonsterRewardSourceUnavailable
	}
	if cached, found := currentDungeonMonsterExperienceSourcesByCatalog.Load(catalog); found {
		return cached.(currentDungeonMonsterExperienceSourceCacheEntry).sources, nil
	}
	sources, err := progression.LoadMonsterExperienceSources(catalog.source)
	if err != nil {
		return nil, err
	}
	actual, _ := currentDungeonMonsterExperienceSourcesByCatalog.LoadOrStore(
		catalog,
		currentDungeonMonsterExperienceSourceCacheEntry{sources: sources},
	)
	return actual.(currentDungeonMonsterExperienceSourceCacheEntry).sources, nil
}

// logCurrentDungeonMonsterRewardSources records the real inputs needed for a
// later natural-reference comparison. It is deliberately side-effect free:
// no EXP/SP, inventory, runtime-drop, packet, or settlement state is changed.
func (s *Service) logCurrentDungeonMonsterRewardSources(
	session *gameSession,
	runtime *runtimeDungeonState,
	monster runtimeDungeonMonster,
) {
	catalog, err := s.dungeonMonsterCatalog()
	if err != nil {
		s.logGameEvent(session, "game-dungeon-monster-reward-source-deferred",
			"monster_id", monster.Spawn.MonsterID,
			"reason", "runtime_pvf_monster_catalog_unavailable",
			"award_action", "none_fail_closed",
			"error", err)
		return
	}
	sources, err := currentDungeonMonsterExperienceSources(catalog)
	if err != nil {
		s.logGameEvent(session, "game-dungeon-monster-reward-source-deferred",
			"monster_id", monster.Spawn.MonsterID,
			"reason", "runtime_pvf_monster_experience_sources_unavailable",
			"award_action", "none_fail_closed",
			"error", err)
		return
	}
	var explicitPool []dungeonMonsterDropPoolEntry
	var explicitPoolErr error
	if monster.Spawn.FixedDropCount > 0 {
		var dropCatalog *pvfDungeonDropCatalog
		dropCatalog, explicitPoolErr = catalog.DropCatalog()
		if explicitPoolErr == nil {
			explicitPool, explicitPoolErr = dropCatalog.MonsterPool(monster.Spawn.MonsterID)
		}
	}
	evidence, err := currentDungeonMonsterRewardSources(runtime, monster, sources, explicitPool)
	if err != nil {
		s.logGameEvent(session, "game-dungeon-monster-reward-source-deferred",
			"dungeon_id", runtime.Dungeon.ID,
			"monster_id", monster.Spawn.MonsterID,
			"monster_object_key", monster.ObjectKey,
			"reason", "runtime_pvf_monster_reward_source_plan_rejected",
			"award_action", "none_fail_closed",
			"error", err)
		return
	}
	experience := evidence.Experience
	drop := evidence.Drop
	s.logGameEvent(session, "game-dungeon-monster-reward-source-evidence",
		"dungeon_id", evidence.Experience.DungeonID,
		"maze_index", runtime.MazeIndex,
		"monster_id", evidence.MonsterID,
		"monster_object_key", monster.ObjectKey,
		"spawn_rank", evidence.SpawnRank,
		"definition_rank", evidence.DefinitionRank,
		"definition_kind", evidence.DefinitionKind,
		"wire_actor_type", evidence.WireActorType,
		"character_level", experience.CharacterLevel,
		"character_level_source", "dungeon_runtime_entry_snapshot",
		"monster_level", experience.MonsterLevel,
		"monster_table_exp", experience.MonsterTable,
		"monster_definition_exp", experience.DefinitionEXP,
		"monster_global_rates", experience.MonsterGlobalRates,
		"single_party_rate", experience.PartyRate,
		"single_party_starter_rate", experience.StarterPartyRate,
		"difficulty_index", experience.DifficultyIndex,
		"difficulty_rate", experience.DifficultyRate,
		"dungeon_global_rate", experience.DungeonRate,
		"dungeon_global_rate_found", experience.DungeonRateFound,
		"dungeon_local_rate", experience.DungeonLocalRate,
		"dungeon_local_rate_specified", experience.DungeonLocalRateSpecified,
		"monster_minus_character_key", experience.MonsterMinusCharacter.Key,
		"monster_minus_character_rate", experience.MonsterMinusCharacter.Rate,
		"monster_minus_character_found", experience.MonsterMinusCharacter.Found,
		"character_minus_monster_key", experience.CharacterMinusMonster.Key,
		"character_minus_monster_rate", experience.CharacterMinusMonster.Rate,
		"character_minus_monster_found", experience.CharacterMinusMonster.Found,
		"experience_award_ready", experience.AwardReady,
		"experience_blockers", experience.Blockers,
		"random_drop_count", drop.RandomDropCount,
		"fixed_drop_count", drop.FixedDropCount,
		"explicit_pool_entries", len(drop.ExplicitPool),
		"explicit_pool_weight", drop.ExplicitWeight,
		"explicit_pool_compatibility_ready", drop.ExplicitPoolCompatibilityReady,
		"explicit_pool_source_error", explicitPoolErr,
		"generic_drop_award_ready", drop.GenericDropAwardReady,
		"generic_drop_blockers", drop.Blockers,
		"full_award_ready", evidence.FullAwardReady,
		"award_action", "diagnostic_only_no_asset_or_packet_mutation")
}
