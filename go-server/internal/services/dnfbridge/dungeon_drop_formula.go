package dnfbridge

import (
	"errors"
	"fmt"
	"math"
	"sync"

	dnfdrops "longheng.io/server/internal/modules/dnf/drop"
	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

const (
	currentDungeonDropDenominator  = 10000
	currentDungeonExplicitPoolRate = 1000
)

var currentDungeonDropDifficultyBonus = [...]float64{1.0, 1.2, 1.4, 1.6, 1.8}

var (
	errCurrentDungeonDropRulesUnavailable = errors.New("current dungeon drop PVF rules are unavailable")
	errCurrentDungeonGoldReferenceInvalid = errors.New("current dungeon gold PVF reference is invalid")
)

// currentDungeonDropLCG is the room PRNG algorithm used by the verified C#
// domain implementation.  Its state is persisted in the room visit, not
// reconstructed from a monster key, so sequential kills consume the same
// deterministic stream as the room run.
type currentDungeonDropLCG struct {
	seed uint32
}

func newCurrentDungeonDropLCG(seed uint32) *currentDungeonDropLCG {
	return &currentDungeonDropLCG{seed: seed}
}

func (rng *currentDungeonDropLCG) Seed() uint32 {
	if rng == nil {
		return 0
	}
	return rng.seed
}

func (rng *currentDungeonDropLCG) NextUint32() uint32 {
	if rng == nil {
		return 0
	}
	v2 := rng.seed*1103515245 + 12345
	v3 := v2*1103515245 + 12345
	v4 := v3*1103515245 + 12345
	rng.seed = v4
	hi2 := (v2 >> 16) & 0x7ff
	hi3 := (v3 >> 16) & 0x3ff
	hi4 := (v4 >> 16) & 0x3ff
	return (((hi2 << 10) ^ hi3) << 10) | hi4
}

func (rng *currentDungeonDropLCG) Next(max int) int {
	value := rng.NextUint32()
	if max <= 0 {
		return int(value)
	}
	return int(value % uint32(max))
}

type currentDungeonGoldReference struct {
	Level       int64
	Base        int64
	VariancePct int64
}

type currentDungeonGoldReferenceCacheEntry struct {
	byLevel map[int64]currentDungeonGoldReference
	err     error
}

type currentDungeonMonsterDropRulesCacheEntry struct {
	rules *dnfdrops.MonsterRules
	err   error
}

var currentDungeonGoldReferencesByCatalog sync.Map
var currentDungeonMonsterDropRulesByCatalog sync.Map

func currentDungeonMonsterDropRules(catalog *pvfDungeonMonsterCatalog) (*dnfdrops.MonsterRules, error) {
	if catalog == nil || catalog.source == nil {
		return nil, errCurrentDungeonDropRulesUnavailable
	}
	if cached, found := currentDungeonMonsterDropRulesByCatalog.Load(catalog); found {
		entry := cached.(currentDungeonMonsterDropRulesCacheEntry)
		return entry.rules, entry.err
	}
	rules, err := dnfdrops.LoadMonsterRules(catalog.source)
	entry := currentDungeonMonsterDropRulesCacheEntry{rules: rules, err: err}
	actual, _ := currentDungeonMonsterDropRulesByCatalog.LoadOrStore(catalog, entry)
	entry = actual.(currentDungeonMonsterDropRulesCacheEntry)
	if entry.err != nil {
		return nil, fmt.Errorf("%w: %v", errCurrentDungeonDropRulesUnavailable, entry.err)
	}
	return entry.rules, nil
}

func currentDungeonGoldReferences(catalog *pvfDungeonMonsterCatalog) (map[int64]currentDungeonGoldReference, error) {
	if catalog == nil || catalog.source == nil {
		return nil, errCurrentDungeonGoldReferenceInvalid
	}
	if cached, found := currentDungeonGoldReferencesByCatalog.Load(catalog); found {
		entry := cached.(currentDungeonGoldReferenceCacheEntry)
		return entry.byLevel, entry.err
	}
	entry := currentDungeonGoldReferenceCacheEntry{}
	text, err := catalog.source.ReadText(dungeonCardGoldReferencePath)
	if err == nil {
		var document *dnfpvf.Document
		document, err = dnfpvf.Parse(dungeonCardGoldReferencePath, text)
		if err == nil {
			values, parseErr := dungeonCardPVFSectionInts(document, "gold drop ref table")
			if parseErr != nil {
				err = parseErr
			} else if len(values)%3 != 0 {
				err = fmt.Errorf("%w: rows=%d", errCurrentDungeonGoldReferenceInvalid, len(values))
			} else {
				entry.byLevel = make(map[int64]currentDungeonGoldReference, len(values)/3)
				for index := 0; index < len(values); index += 3 {
					row := currentDungeonGoldReference{Level: values[index], Base: values[index+1], VariancePct: values[index+2]}
					if row.Level <= 0 || row.Base <= 0 || row.VariancePct < 0 || row.VariancePct > 100 || entry.byLevel[row.Level].Level != 0 {
						err = fmt.Errorf("%w: row=%+v", errCurrentDungeonGoldReferenceInvalid, row)
						break
					}
					entry.byLevel[row.Level] = row
				}
			}
		}
	}
	if err != nil {
		entry.err = fmt.Errorf("%w: %v", errCurrentDungeonGoldReferenceInvalid, err)
	}
	actual, _ := currentDungeonGoldReferencesByCatalog.LoadOrStore(catalog, entry)
	entry = actual.(currentDungeonGoldReferenceCacheEntry)
	return entry.byLevel, entry.err
}

func currentDungeonDropRate(base int64, typeBonus float64, difficultyBonus float64) int {
	if base <= 0 || math.IsNaN(typeBonus) || math.IsInf(typeBonus, 0) || typeBonus <= 0 ||
		math.IsNaN(difficultyBonus) || math.IsInf(difficultyBonus, 0) || difficultyBonus <= 0 {
		return 0
	}
	value := math.Floor(float64(base) * typeBonus * difficultyBonus)
	if value <= 0 {
		return 0
	}
	if value >= currentDungeonDropDenominator {
		return currentDungeonDropDenominator
	}
	return int(value)
}

func currentDungeonDropDifficultyRate(index int) float64 {
	if index < 0 || index >= len(currentDungeonDropDifficultyBonus) {
		return 1.0
	}
	return currentDungeonDropDifficultyBonus[index]
}

func currentDungeonDropRarity(rng *currentDungeonDropLCG, rules *dnfdrops.MonsterRules) int64 {
	if rng == nil || rules == nil {
		return 0
	}
	thresholds := rules.RawTables().RarityDecision[0]
	roll := int64(rng.Next(1000000)) + 1
	for rarity, threshold := range thresholds {
		if roll <= threshold {
			return int64(rarity)
		}
	}
	return 0
}

func currentDungeonGoldAmount(rng *currentDungeonDropLCG, reference currentDungeonGoldReference, difficulty float64) uint32 {
	if rng == nil || reference.Base <= 0 || reference.VariancePct < 0 || difficulty <= 0 {
		return 0
	}
	variance := int64(0)
	if reference.VariancePct > 0 {
		rangeSize := reference.VariancePct*2 + 1
		variance = (int64(rng.Next(int(rangeSize))) - reference.VariancePct) * reference.Base / 100
	}
	value := int64(math.Floor(float64(reference.Base+variance) * difficulty))
	if value < 1 {
		value = 1
	}
	if value > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(value)
}
