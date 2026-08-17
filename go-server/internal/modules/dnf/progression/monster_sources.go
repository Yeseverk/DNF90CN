package progression

import (
	"errors"
	"fmt"
	"math"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

const (
	MonsterExperienceTablePath    = "monster/monsterexp.tbl"
	ServerParameterPath           = "Etc/ServerParameter.etc"
	WorldMapExperiencePenaltyPath = "etc/worldmapexppenaltytable.etc"
)

var (
	ErrMonsterExperienceSourceRequired = errors.New("dnf monster experience PVF source is required")
	ErrMonsterExperienceSectionMissing = errors.New("dnf monster experience PVF section is missing")
	ErrMonsterExperienceSectionShape   = errors.New("dnf monster experience PVF section shape is invalid")
	ErrMonsterExperienceLookup         = errors.New("dnf monster experience PVF lookup is absent")
)

// MonsterExperienceSourceSnapshot reports only typed PVF source coverage. It
// does not claim that the current server's award formula has been reconstructed.
type MonsterExperienceSourceSnapshot struct {
	MonsterLevels          int
	MonsterGlobalRates     int
	PartyRates             int
	StarterPartyRates      int
	DifficultyRates        int
	DungeonIncreasingRates int
	PenaltyRows            int
	ClearGlobalRates       int
	ClearRankRates         int
}

// MonsterExperienceRawModifiers preserves the exact ServerParameter.etc
// sections whose names mention monster/dungeon experience. Section ordering is
// retained; no multiplication order or hidden level-difference rule is implied.
type MonsterExperienceRawModifiers struct {
	MonsterGlobalRates []float64
	PartyRates         [4]float64
	StarterPartyRates  [4]float64
	DifficultyRates    [5]float64
	ClearGlobalRates   []float64
	ClearRankRates     [5]float64
}

type MonsterExperienceBlocker string

const (
	MonsterExperienceCombinationUnproved      MonsterExperienceBlocker = "modifier_combination_unproved"
	MonsterExperiencePenaltyDirectionUnproved MonsterExperienceBlocker = "level_difference_direction_unproved"
	MonsterExperienceActorTypeUnproved        MonsterExperienceBlocker = "actor_type_rate_mapping_unproved"
)

// MonsterExperienceSourcePlan is a pure, non-awarding lookup. Values come from
// the current runtime PVF, but AwardReady deliberately remains false until the
// current server/EXE proves how these independent sources combine.
type MonsterExperienceSourcePlan struct {
	MonsterLevel          int
	MonsterTableValue     uint32
	PartyMembers          int
	PartyRate             float64
	StarterPartyRate      float64
	DifficultyIndex       int
	DifficultyRate        float64
	DungeonID             int64
	DungeonIncreasingRate float64
	DungeonRateFound      bool
	AwardReady            bool
	Blockers              []MonsterExperienceBlocker
}

// MonsterExperienceSources is an immutable typed view of
// monster/monsterexp.tbl plus the experience-named sections in
// Etc/ServerParameter.etc.
type MonsterExperienceSources struct {
	monsterByLevel     []uint32
	monsterGlobalRates []float64
	partyRates         [4]float64
	starterPartyRates  [4]float64
	difficultyRates    [5]float64
	dungeonRates       map[int64]float64
	penaltyRates       map[int]float64
	clearGlobalRates   []float64
	clearRankRates     [5]float64
}

func LoadMonsterExperienceSources(source dnfpvf.Source) (*MonsterExperienceSources, error) {
	if source == nil {
		return nil, ErrMonsterExperienceSourceRequired
	}
	monsterDocument, err := readDocument(source, MonsterExperienceTablePath)
	if err != nil {
		return nil, err
	}
	monsterByLevel, err := parseMonsterExperienceTable(monsterDocument)
	if err != nil {
		return nil, err
	}
	serverDocument, err := readDocument(source, ServerParameterPath)
	if err != nil {
		return nil, err
	}
	globalRates, err := strictMonsterNumbers(serverDocument, "monster exp bonusrate")
	if err != nil {
		return nil, err
	}
	if len(globalRates) != 1 {
		return nil, fmt.Errorf("%w: path=%s section=monster exp bonusrate values=%d want=1",
			ErrMonsterExperienceSectionShape, ServerParameterPath, len(globalRates))
	}
	partyValues, err := strictMonsterFixedNumbers(serverDocument, "party user number exp bonusrate", 4)
	if err != nil {
		return nil, err
	}
	starterPartyValues, err := strictMonsterFixedNumbers(serverDocument, "party user number exp bonusrate starter server", 4)
	if err != nil {
		return nil, err
	}
	difficultyValues, err := strictMonsterFixedNumbers(serverDocument, "dungeon difficulty exp bonusrate", 5)
	if err != nil {
		return nil, err
	}
	dungeonRates, err := parseMonsterDungeonRates(serverDocument)
	if err != nil {
		return nil, err
	}
	clearGlobalRates, err := strictMonsterNumbers(serverDocument, "clear exp bonusrate")
	if err != nil {
		return nil, err
	}
	if len(clearGlobalRates) != 1 {
		return nil, fmt.Errorf("%w: path=%s section=clear exp bonusrate values=%d want=1",
			ErrMonsterExperienceSectionShape, ServerParameterPath, len(clearGlobalRates))
	}
	clearRankValues, err := strictMonsterFixedNumbers(serverDocument, "clear rank exp bonusrate", 5)
	if err != nil {
		return nil, err
	}
	penaltyDocument, err := readDocument(source, WorldMapExperiencePenaltyPath)
	if err != nil {
		return nil, err
	}
	penaltyRates, err := parseMonsterPenaltyRates(penaltyDocument)
	if err != nil {
		return nil, err
	}
	result := &MonsterExperienceSources{
		monsterByLevel:     monsterByLevel,
		monsterGlobalRates: globalRates,
		dungeonRates:       dungeonRates,
		penaltyRates:       penaltyRates,
		clearGlobalRates:   clearGlobalRates,
	}
	copy(result.partyRates[:], partyValues)
	copy(result.starterPartyRates[:], starterPartyValues)
	copy(result.difficultyRates[:], difficultyValues)
	copy(result.clearRankRates[:], clearRankValues)
	return result, nil
}

func parseMonsterExperienceTable(document *dnfpvf.Document) ([]uint32, error) {
	if document == nil || len(document.Tokens) == 0 {
		return nil, fmt.Errorf("%w: path=%s empty", ErrMonsterExperienceSectionShape, MonsterExperienceTablePath)
	}
	values := make([]int64, 0, len(document.Tokens))
	for index, token := range document.Tokens {
		if token.Kind != dnfpvf.TokenInt {
			return nil, fmt.Errorf("%w: path=%s token=%d kind=%s", ErrMonsterExperienceSectionShape, MonsterExperienceTablePath, index, token.Kind)
		}
		values = append(values, token.Int)
	}
	if len(values)%2 != 0 {
		return nil, fmt.Errorf("%w: path=%s values=%d row_width=2", ErrMonsterExperienceSectionShape, MonsterExperienceTablePath, len(values))
	}
	table := make([]uint32, 0, len(values)/2)
	for offset := 0; offset < len(values); offset += 2 {
		level, value := values[offset], values[offset+1]
		wantLevel := int64(len(table) + 1)
		if level != wantLevel || value <= 0 || value > math.MaxUint32 {
			return nil, fmt.Errorf("%w: path=%s row=%d level=%d want_level=%d value=%d",
				ErrMonsterExperienceSectionShape, MonsterExperienceTablePath, offset/2, level, wantLevel, value)
		}
		table = append(table, uint32(value))
	}
	return table, nil
}

func strictMonsterNumbers(document *dnfpvf.Document, section string) ([]float64, error) {
	tokens, found := document.Section(section)
	if !found {
		return nil, fmt.Errorf("%w: path=%s section=%s", ErrMonsterExperienceSectionMissing, ServerParameterPath, section)
	}
	if len(tokens) == 0 {
		return nil, fmt.Errorf("%w: path=%s section=%s empty", ErrMonsterExperienceSectionShape, ServerParameterPath, section)
	}
	values := make([]float64, 0, len(tokens))
	for index, token := range tokens {
		var value float64
		switch token.Kind {
		case dnfpvf.TokenInt:
			value = float64(token.Int)
		case dnfpvf.TokenFloat:
			value = token.Float
		default:
			return nil, fmt.Errorf("%w: path=%s section=%s token=%d kind=%s",
				ErrMonsterExperienceSectionShape, ServerParameterPath, section, index, token.Kind)
		}
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return nil, fmt.Errorf("%w: path=%s section=%s token=%d value=%v",
				ErrMonsterExperienceSectionShape, ServerParameterPath, section, index, value)
		}
		values = append(values, value)
	}
	return values, nil
}

func strictMonsterFixedNumbers(document *dnfpvf.Document, section string, want int) ([]float64, error) {
	values, err := strictMonsterNumbers(document, section)
	if err != nil {
		return nil, err
	}
	if len(values) != want {
		return nil, fmt.Errorf("%w: path=%s section=%s values=%d want=%d",
			ErrMonsterExperienceSectionShape, ServerParameterPath, section, len(values), want)
	}
	return values, nil
}

func parseMonsterDungeonRates(document *dnfpvf.Document) (map[int64]float64, error) {
	tokens, found := document.Section("experience increasing point")
	if !found {
		return nil, fmt.Errorf("%w: path=%s section=experience increasing point", ErrMonsterExperienceSectionMissing, ServerParameterPath)
	}
	if len(tokens) == 0 || len(tokens)%2 != 0 {
		return nil, fmt.Errorf("%w: path=%s section=experience increasing point values=%d row_width=2",
			ErrMonsterExperienceSectionShape, ServerParameterPath, len(tokens))
	}
	rates := make(map[int64]float64, len(tokens)/2)
	for offset := 0; offset < len(tokens); offset += 2 {
		idToken, rateToken := tokens[offset], tokens[offset+1]
		if idToken.Kind != dnfpvf.TokenInt || idToken.Int <= 0 {
			return nil, fmt.Errorf("%w: path=%s section=experience increasing point row=%d id_kind=%s id=%d",
				ErrMonsterExperienceSectionShape, ServerParameterPath, offset/2, idToken.Kind, idToken.Int)
		}
		var rate float64
		switch rateToken.Kind {
		case dnfpvf.TokenInt:
			rate = float64(rateToken.Int)
		case dnfpvf.TokenFloat:
			rate = rateToken.Float
		default:
			return nil, fmt.Errorf("%w: path=%s section=experience increasing point row=%d rate_kind=%s",
				ErrMonsterExperienceSectionShape, ServerParameterPath, offset/2, rateToken.Kind)
		}
		if math.IsNaN(rate) || math.IsInf(rate, 0) || rate < 0 {
			return nil, fmt.Errorf("%w: path=%s section=experience increasing point row=%d rate=%v",
				ErrMonsterExperienceSectionShape, ServerParameterPath, offset/2, rate)
		}
		if _, duplicate := rates[idToken.Int]; duplicate {
			return nil, fmt.Errorf("%w: path=%s section=experience increasing point duplicate_dungeon=%d",
				ErrMonsterExperienceSectionShape, ServerParameterPath, idToken.Int)
		}
		rates[idToken.Int] = rate
	}
	return rates, nil
}

func parseMonsterPenaltyRates(document *dnfpvf.Document) (map[int]float64, error) {
	tokens, found := document.Section("penalty table info")
	if !found {
		return nil, fmt.Errorf("%w: path=%s section=penalty table info", ErrMonsterExperienceSectionMissing, WorldMapExperiencePenaltyPath)
	}
	const firstKey, lastKey = -16, 20
	if len(tokens) != (lastKey-firstKey+1)*2 {
		return nil, fmt.Errorf("%w: path=%s section=penalty table info values=%d want=%d",
			ErrMonsterExperienceSectionShape, WorldMapExperiencePenaltyPath, len(tokens), (lastKey-firstKey+1)*2)
	}
	rates := make(map[int]float64, lastKey-firstKey+1)
	for offset := 0; offset < len(tokens); offset += 2 {
		keyToken, rateToken := tokens[offset], tokens[offset+1]
		wantKey := int64(firstKey + offset/2)
		if keyToken.Kind != dnfpvf.TokenInt || keyToken.Int != wantKey {
			return nil, fmt.Errorf("%w: path=%s section=penalty table info row=%d key=%d want=%d kind=%s",
				ErrMonsterExperienceSectionShape, WorldMapExperiencePenaltyPath, offset/2, keyToken.Int, wantKey, keyToken.Kind)
		}
		var rate float64
		switch rateToken.Kind {
		case dnfpvf.TokenInt:
			rate = float64(rateToken.Int)
		case dnfpvf.TokenFloat:
			rate = rateToken.Float
		default:
			return nil, fmt.Errorf("%w: path=%s section=penalty table info row=%d rate_kind=%s",
				ErrMonsterExperienceSectionShape, WorldMapExperiencePenaltyPath, offset/2, rateToken.Kind)
		}
		if math.IsNaN(rate) || math.IsInf(rate, 0) || rate < 0 {
			return nil, fmt.Errorf("%w: path=%s section=penalty table info row=%d rate=%v",
				ErrMonsterExperienceSectionShape, WorldMapExperiencePenaltyPath, offset/2, rate)
		}
		rates[int(wantKey)] = rate
	}
	return rates, nil
}

func (sources *MonsterExperienceSources) Snapshot() MonsterExperienceSourceSnapshot {
	if sources == nil {
		return MonsterExperienceSourceSnapshot{}
	}
	return MonsterExperienceSourceSnapshot{
		MonsterLevels:          len(sources.monsterByLevel),
		MonsterGlobalRates:     len(sources.monsterGlobalRates),
		PartyRates:             len(sources.partyRates),
		StarterPartyRates:      len(sources.starterPartyRates),
		DifficultyRates:        len(sources.difficultyRates),
		DungeonIncreasingRates: len(sources.dungeonRates),
		PenaltyRows:            len(sources.penaltyRates),
		ClearGlobalRates:       len(sources.clearGlobalRates),
		ClearRankRates:         len(sources.clearRankRates),
	}
}

func (sources *MonsterExperienceSources) MonsterTableValue(level int) (uint32, bool) {
	if sources == nil || level <= 0 || level > len(sources.monsterByLevel) {
		return 0, false
	}
	return sources.monsterByLevel[level-1], true
}

func (sources *MonsterExperienceSources) DungeonIncreasingRate(dungeonID int64) (float64, bool) {
	if sources == nil {
		return 0, false
	}
	value, found := sources.dungeonRates[dungeonID]
	return value, found
}

// PenaltyRateBySourceKey returns one exact key/value pair from
// worldmapexppenaltytable.etc. The method intentionally does not accept player
// and monster levels because the current EXE has not yet proved which
// subtraction direction produces the source key.
func (sources *MonsterExperienceSources) PenaltyRateBySourceKey(key int) (float64, bool) {
	if sources == nil {
		return 0, false
	}
	value, found := sources.penaltyRates[key]
	return value, found
}

func (sources *MonsterExperienceSources) RawModifiers() MonsterExperienceRawModifiers {
	if sources == nil {
		return MonsterExperienceRawModifiers{}
	}
	return MonsterExperienceRawModifiers{
		MonsterGlobalRates: append([]float64(nil), sources.monsterGlobalRates...),
		PartyRates:         sources.partyRates,
		StarterPartyRates:  sources.starterPartyRates,
		DifficultyRates:    sources.difficultyRates,
		ClearGlobalRates:   append([]float64(nil), sources.clearGlobalRates...),
		ClearRankRates:     sources.clearRankRates,
	}
}

func (sources *MonsterExperienceSources) PlanSource(monsterLevel int, partyMembers int, difficultyIndex int, dungeonID int64) (MonsterExperienceSourcePlan, error) {
	if sources == nil {
		return MonsterExperienceSourcePlan{}, ErrMonsterExperienceSourceRequired
	}
	monsterValue, found := sources.MonsterTableValue(monsterLevel)
	if !found {
		return MonsterExperienceSourcePlan{}, fmt.Errorf("%w: monster_level=%d", ErrMonsterExperienceLookup, monsterLevel)
	}
	if partyMembers <= 0 || partyMembers > len(sources.partyRates) {
		return MonsterExperienceSourcePlan{}, fmt.Errorf("%w: party_members=%d", ErrMonsterExperienceLookup, partyMembers)
	}
	if difficultyIndex < 0 || difficultyIndex >= len(sources.difficultyRates) {
		return MonsterExperienceSourcePlan{}, fmt.Errorf("%w: difficulty_index=%d", ErrMonsterExperienceLookup, difficultyIndex)
	}
	dungeonRate, dungeonFound := sources.DungeonIncreasingRate(dungeonID)
	return MonsterExperienceSourcePlan{
		MonsterLevel:          monsterLevel,
		MonsterTableValue:     monsterValue,
		PartyMembers:          partyMembers,
		PartyRate:             sources.partyRates[partyMembers-1],
		StarterPartyRate:      sources.starterPartyRates[partyMembers-1],
		DifficultyIndex:       difficultyIndex,
		DifficultyRate:        sources.difficultyRates[difficultyIndex],
		DungeonID:             dungeonID,
		DungeonIncreasingRate: dungeonRate,
		DungeonRateFound:      dungeonFound,
		AwardReady:            false,
		Blockers: []MonsterExperienceBlocker{
			MonsterExperienceCombinationUnproved,
			MonsterExperiencePenaltyDirectionUnproved,
			MonsterExperienceActorTypeUnproved,
		},
	}, nil
}
