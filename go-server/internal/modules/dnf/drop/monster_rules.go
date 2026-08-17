package drop

import (
	"errors"
	"fmt"
	"math"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

const MonsterRulePath = "Etc/ItemDropInfo_Monseter.etc"

var (
	ErrMonsterRuleSourceRequired = errors.New("dnf monster drop rule PVF source is required")
	ErrMonsterRuleSectionMissing = errors.New("dnf monster drop rule PVF section is missing")
	ErrMonsterRuleSectionShape   = errors.New("dnf monster drop rule PVF section shape is invalid")
)

// MonsterProbabilityRow preserves one exact seven-column [drop prob] row.
// The first two values form the lookup range. The five remaining columns stay
// opaque until their category mapping is proved from the current server/EXE;
// the PVF section alone does not safely name them as gold/equipment/material.
type MonsterProbabilityRow struct {
	KeyMin int64
	KeyMax int64
	Rates  [5]int64
}

// MonsterItemReference preserves one exact three-column [item drop ref table]
// row. ValueA and ValueB are intentionally not named as grade offsets because
// that business meaning is not established by the PVF document itself.
type MonsterItemReference struct {
	LookupKey int64
	ValueA    int64
	ValueB    int64
}

type MonsterRuleSnapshot struct {
	ProbabilityRows     int
	RarityRows          int
	PartyBonusRows      int
	DifficultyBonusRows int
	MonsterTypeRows     int
	ItemReferences      int
	ConditionRows       int
}

// MonsterRawTables is a value-copy of the remaining fixed-size PVF sections.
// The names describe source sections and shape only; row/column category
// semantics are not promoted to business rules without separate evidence.
type MonsterRawTables struct {
	RarityDecision   [4][7]int64
	PartyBonus       [5][4]float64
	DifficultyBonus  [5][5]float64
	MonsterTypeBonus [5][4]float64
	FirstBossNamed   [2]int64
	ConditionRates   [4][2]int64
	GoldQuantity     [4]int64
	GoldVolume       [2]int64
	RarityControl    [2][5]int64
}

// MonsterSourcePlan is a pure lookup result. It does not roll a probability,
// choose an item, allocate a scene object, or mutate an inventory.
type MonsterSourcePlan struct {
	LookupKey          int64
	Probability        MonsterProbabilityRow
	ItemReference      MonsterItemReference
	ProbabilityFound   bool
	ItemReferenceFound bool
}

// MonsterRules is a read-only typed view of every numeric section currently
// present in ItemDropInfo_Monseter.etc. Matrices preserve PVF row/column order;
// their axes remain deliberately opaque where the current runtime has not
// proved a category mapping.
type MonsterRules struct {
	probabilityRows  []MonsterProbabilityRow
	rarityDecision   [4][7]int64
	partyBonus       [5][4]float64
	difficultyBonus  [5][5]float64
	monsterTypeBonus [5][4]float64
	itemReferences   map[int64]MonsterItemReference
	firstBossNamed   [2]int64
	conditionRates   [4][2]int64
	goldQuantity     [4]int64
	goldVolume       [2]int64
	rarityControl    [2][5]int64
}

func LoadMonsterRules(source dnfpvf.Source) (*MonsterRules, error) {
	if source == nil {
		return nil, ErrMonsterRuleSourceRequired
	}
	text, err := source.ReadText(MonsterRulePath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", MonsterRulePath, err)
	}
	document, err := dnfpvf.Parse(MonsterRulePath, text)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", MonsterRulePath, err)
	}

	probabilityRows, err := parseMonsterProbabilityRows(document)
	if err != nil {
		return nil, err
	}
	itemReferences, err := parseMonsterItemReferences(document)
	if err != nil {
		return nil, err
	}
	rules := &MonsterRules{
		probabilityRows: probabilityRows,
		itemReferences:  itemReferences,
	}
	if err := fillMonsterRuleIntMatrix(document, "basis of rarity dicision", 4, 7, func(row, column int, value int64) {
		rules.rarityDecision[row][column] = value
	}); err != nil {
		return nil, err
	}
	for row := range rules.rarityDecision {
		for column := 1; column < len(rules.rarityDecision[row]); column++ {
			if rules.rarityDecision[row][column] < rules.rarityDecision[row][column-1] {
				return nil, fmt.Errorf("%w: section=basis of rarity dicision row=%d column=%d value=%d previous=%d",
					ErrMonsterRuleSectionShape, row, column, rules.rarityDecision[row][column], rules.rarityDecision[row][column-1])
			}
		}
	}
	if err := fillMonsterRuleNumberMatrix(document, "party member drop bonusrate", 5, 4, func(row, column int, value float64) {
		rules.partyBonus[row][column] = value
	}); err != nil {
		return nil, err
	}
	if err := fillMonsterRuleNumberMatrix(document, "dungeon difficulty drop bonusrate", 5, 5, func(row, column int, value float64) {
		rules.difficultyBonus[row][column] = value
	}); err != nil {
		return nil, err
	}
	if err := fillMonsterRuleNumberMatrix(document, "monster type drop bonusrate", 5, 4, func(row, column int, value float64) {
		rules.monsterTypeBonus[row][column] = value
	}); err != nil {
		return nil, err
	}
	if err := fillMonsterRuleIntArray(document, "first boss/named mob hunting", rules.firstBossNamed[:]); err != nil {
		return nil, err
	}
	if err := fillMonsterRuleIntMatrix(document, "condition rate", 4, 2, func(row, column int, value int64) {
		rules.conditionRates[row][column] = value
	}); err != nil {
		return nil, err
	}
	if err := fillMonsterRuleIntArray(document, "gold quantity", rules.goldQuantity[:]); err != nil {
		return nil, err
	}
	if err := fillMonsterRuleIntArray(document, "gold volume", rules.goldVolume[:]); err != nil {
		return nil, err
	}
	if err := fillMonsterRuleIntMatrix(document, "item drop rarity control", 2, 5, func(row, column int, value int64) {
		rules.rarityControl[row][column] = value
	}); err != nil {
		return nil, err
	}
	return rules, nil
}

func parseMonsterProbabilityRows(document *dnfpvf.Document) ([]MonsterProbabilityRow, error) {
	declared, found := document.Int("drop prob count")
	if !found {
		return nil, fmt.Errorf("%w: section=drop prob count", ErrMonsterRuleSectionMissing)
	}
	values, err := monsterRuleIntegers(document, "drop prob")
	if err != nil {
		return nil, err
	}
	if declared <= 0 || len(values)%7 != 0 || int64(len(values)/7) != declared {
		return nil, fmt.Errorf("%w: section=drop prob declared=%d values=%d row_width=7",
			ErrMonsterRuleSectionShape, declared, len(values))
	}
	rows := make([]MonsterProbabilityRow, 0, len(values)/7)
	for offset := 0; offset < len(values); offset += 7 {
		row := MonsterProbabilityRow{
			KeyMin: values[offset],
			KeyMax: values[offset+1],
			Rates: [5]int64{
				values[offset+2], values[offset+3], values[offset+4], values[offset+5], values[offset+6],
			},
		}
		if row.KeyMin < 0 || row.KeyMax < row.KeyMin {
			return nil, fmt.Errorf("%w: section=drop prob row=%+v", ErrMonsterRuleSectionShape, row)
		}
		for _, value := range row.Rates {
			if value < 0 {
				return nil, fmt.Errorf("%w: section=drop prob row=%+v negative_rate=%d", ErrMonsterRuleSectionShape, row, value)
			}
		}
		if len(rows) != 0 && row.KeyMin <= rows[len(rows)-1].KeyMax {
			return nil, fmt.Errorf("%w: section=drop prob overlapping_or_unsorted previous=%+v current=%+v",
				ErrMonsterRuleSectionShape, rows[len(rows)-1], row)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func parseMonsterItemReferences(document *dnfpvf.Document) (map[int64]MonsterItemReference, error) {
	values, err := monsterRuleIntegers(document, "item drop ref table")
	if err != nil {
		return nil, err
	}
	if len(values)%3 != 0 {
		return nil, fmt.Errorf("%w: section=item drop ref table values=%d row_width=3", ErrMonsterRuleSectionShape, len(values))
	}
	rows := make(map[int64]MonsterItemReference, len(values)/3)
	var previous int64
	for offset := 0; offset < len(values); offset += 3 {
		row := MonsterItemReference{LookupKey: values[offset], ValueA: values[offset+1], ValueB: values[offset+2]}
		if row.LookupKey <= 0 || (offset != 0 && row.LookupKey <= previous) {
			return nil, fmt.Errorf("%w: section=item drop ref table previous_key=%d row=%+v", ErrMonsterRuleSectionShape, previous, row)
		}
		rows[row.LookupKey] = row
		previous = row.LookupKey
	}
	return rows, nil
}

func monsterRuleIntegers(document *dnfpvf.Document, section string) ([]int64, error) {
	tokens, found := document.Section(section)
	if !found {
		return nil, fmt.Errorf("%w: section=%s", ErrMonsterRuleSectionMissing, section)
	}
	if len(tokens) == 0 {
		return nil, fmt.Errorf("%w: section=%s empty", ErrMonsterRuleSectionShape, section)
	}
	values := make([]int64, 0, len(tokens))
	for index, token := range tokens {
		if token.Kind != dnfpvf.TokenInt {
			return nil, fmt.Errorf("%w: section=%s token=%d kind=%s", ErrMonsterRuleSectionShape, section, index, token.Kind)
		}
		values = append(values, token.Int)
	}
	return values, nil
}

func monsterRuleNumbers(document *dnfpvf.Document, section string) ([]float64, error) {
	tokens, found := document.Section(section)
	if !found {
		return nil, fmt.Errorf("%w: section=%s", ErrMonsterRuleSectionMissing, section)
	}
	if len(tokens) == 0 {
		return nil, fmt.Errorf("%w: section=%s empty", ErrMonsterRuleSectionShape, section)
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
			return nil, fmt.Errorf("%w: section=%s token=%d kind=%s", ErrMonsterRuleSectionShape, section, index, token.Kind)
		}
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return nil, fmt.Errorf("%w: section=%s token=%d value=%v", ErrMonsterRuleSectionShape, section, index, value)
		}
		values = append(values, value)
	}
	return values, nil
}

func fillMonsterRuleIntArray(document *dnfpvf.Document, section string, destination []int64) error {
	values, err := monsterRuleIntegers(document, section)
	if err != nil {
		return err
	}
	if len(values) != len(destination) {
		return fmt.Errorf("%w: section=%s values=%d want=%d", ErrMonsterRuleSectionShape, section, len(values), len(destination))
	}
	copy(destination, values)
	return nil
}

func fillMonsterRuleIntMatrix(
	document *dnfpvf.Document,
	section string,
	rows int,
	columns int,
	set func(int, int, int64),
) error {
	values, err := monsterRuleIntegers(document, section)
	if err != nil {
		return err
	}
	if len(values) != rows*columns {
		return fmt.Errorf("%w: section=%s values=%d matrix=%dx%d", ErrMonsterRuleSectionShape, section, len(values), rows, columns)
	}
	for index, value := range values {
		set(index/columns, index%columns, value)
	}
	return nil
}

func fillMonsterRuleNumberMatrix(
	document *dnfpvf.Document,
	section string,
	rows int,
	columns int,
	set func(int, int, float64),
) error {
	values, err := monsterRuleNumbers(document, section)
	if err != nil {
		return err
	}
	if len(values) != rows*columns {
		return fmt.Errorf("%w: section=%s values=%d matrix=%dx%d", ErrMonsterRuleSectionShape, section, len(values), rows, columns)
	}
	for index, value := range values {
		set(index/columns, index%columns, value)
	}
	return nil
}

func (rules *MonsterRules) Probability(key int64) (MonsterProbabilityRow, bool) {
	if rules == nil {
		return MonsterProbabilityRow{}, false
	}
	for _, row := range rules.probabilityRows {
		if key >= row.KeyMin && key <= row.KeyMax {
			return row, true
		}
	}
	return MonsterProbabilityRow{}, false
}

func (rules *MonsterRules) ItemReference(key int64) (MonsterItemReference, bool) {
	if rules == nil {
		return MonsterItemReference{}, false
	}
	row, found := rules.itemReferences[key]
	return row, found
}

func (rules *MonsterRules) PlanSource(key int64) MonsterSourcePlan {
	plan := MonsterSourcePlan{LookupKey: key}
	plan.Probability, plan.ProbabilityFound = rules.Probability(key)
	plan.ItemReference, plan.ItemReferenceFound = rules.ItemReference(key)
	return plan
}

func (rules *MonsterRules) Snapshot() MonsterRuleSnapshot {
	if rules == nil {
		return MonsterRuleSnapshot{}
	}
	return MonsterRuleSnapshot{
		ProbabilityRows:     len(rules.probabilityRows),
		RarityRows:          len(rules.rarityDecision),
		PartyBonusRows:      len(rules.partyBonus),
		DifficultyBonusRows: len(rules.difficultyBonus),
		MonsterTypeRows:     len(rules.monsterTypeBonus),
		ItemReferences:      len(rules.itemReferences),
		ConditionRows:       len(rules.conditionRates),
	}
}

func (rules *MonsterRules) RawTables() MonsterRawTables {
	if rules == nil {
		return MonsterRawTables{}
	}
	return MonsterRawTables{
		RarityDecision:   rules.rarityDecision,
		PartyBonus:       rules.partyBonus,
		DifficultyBonus:  rules.difficultyBonus,
		MonsterTypeBonus: rules.monsterTypeBonus,
		FirstBossNamed:   rules.firstBossNamed,
		ConditionRates:   rules.conditionRates,
		GoldQuantity:     rules.goldQuantity,
		GoldVolume:       rules.goldVolume,
		RarityControl:    rules.rarityControl,
	}
}
