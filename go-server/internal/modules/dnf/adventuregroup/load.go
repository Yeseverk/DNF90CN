package adventuregroup

import (
	"context"
	"fmt"
	"math"
	"strings"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

const (
	pointBonusSection     = "point bonus"
	managePointSection    = "manage level point"
	manageLevelMaxSection = "manage level max"
	expBonusSection       = "exp bonus"
	goldBonusSection      = "gold bonus"
	manageOptionSection   = "manage option"
)

// Load parses the typed character-management document. It remains the narrow
// calculator loader used by domain tests and tooling.
func Load(ctx context.Context, source dnfpvf.Source) (*Tables, error) {
	if source == nil {
		return nil, dnfpvf.ErrSourceRequired
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	text, err := source.ReadText(CharacterManagementPath)
	if err != nil {
		return nil, fmt.Errorf("read dnf adventure-group PVF %q: %w", CharacterManagementPath, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	doc, err := dnfpvf.Parse(CharacterManagementPath, text)
	if err != nil {
		return nil, fmt.Errorf("parse dnf adventure-group PVF %q: %w", CharacterManagementPath, err)
	}
	tables, err := parseTables(doc)
	if err != nil {
		return nil, err
	}
	return tables, nil
}

// LoadComplete loads the management table together with the current 2018
// expedition, shop and growth-capsule documents. Production composition uses
// this strict compatibility-unit loader.
func LoadComplete(ctx context.Context, source dnfpvf.Source) (*Tables, error) {
	tables, err := Load(ctx, source)
	if err != nil {
		return nil, err
	}
	runtime, err := loadRuntimeConfig(ctx, source)
	if err != nil {
		return nil, err
	}
	tables.runtime = runtime
	return tables, nil
}

func parseTables(doc *dnfpvf.Document) (*Tables, error) {
	pointValues, err := numericSection(doc, pointBonusSection, false)
	if err != nil {
		return nil, err
	}
	pointRanges, err := parsePointRanges(pointValues)
	if err != nil {
		return nil, err
	}
	thresholdValues, err := numericSection(doc, managePointSection, false)
	if err != nil {
		return nil, err
	}
	thresholds, err := parseThresholds(thresholdValues)
	if err != nil {
		return nil, err
	}
	maxValues, err := numericSection(doc, manageLevelMaxSection, false)
	if err != nil {
		return nil, err
	}
	if len(maxValues) != 1 || maxValues[0] <= 0 || maxValues[0] > int64(math.MaxInt) {
		return nil, fmt.Errorf("%w: [%s] values=%v", ErrTableMalformed, manageLevelMaxSection, maxValues)
	}
	manageLevelMax := int(maxValues[0])

	expValues, err := numericSection(doc, expBonusSection, true)
	if err != nil {
		return nil, err
	}
	expBonus, err := parseLevelValueTable(expBonusSection, expValues, manageLevelMax, true)
	if err != nil {
		return nil, err
	}
	goldValues, err := numericSection(doc, goldBonusSection, true)
	if err != nil {
		return nil, err
	}
	goldBonus, err := parseLevelValueTable(goldBonusSection, goldValues, manageLevelMax, true)
	if err != nil {
		return nil, err
	}
	optionValues, err := numericSection(doc, manageOptionSection, false)
	if err != nil {
		return nil, err
	}
	options, err := parseLevelValueTable(manageOptionSection, optionValues, manageLevelMax, false)
	if err != nil {
		return nil, err
	}

	return &Tables{
		pointRanges:      pointRanges,
		manageThresholds: thresholds,
		manageLevelMax:   manageLevelMax,
		expBonusByLevel:  expBonus,
		goldBonusByLevel: goldBonus,
		optionByLevel:    options,
	}, nil
}

func numericSection(doc *dnfpvf.Document, name string, allowEmpty bool) ([]int64, error) {
	if doc == nil {
		return nil, fmt.Errorf("%w: %s [%s]", ErrTableEmpty, CharacterManagementPath, name)
	}
	count := 0
	for _, section := range doc.Sections {
		if strings.EqualFold(strings.TrimSpace(section.Name), name) {
			count++
		}
	}
	if count == 0 {
		return nil, fmt.Errorf("%w: %s missing [%s]", ErrTableEmpty, CharacterManagementPath, name)
	}
	if count != 1 {
		return nil, fmt.Errorf("%w: %s duplicate [%s] count=%d", ErrTableMalformed, CharacterManagementPath, name, count)
	}
	tokens, ok := doc.Section(name)
	if !ok {
		return nil, fmt.Errorf("%w: %s unreadable [%s]", ErrTableMalformed, CharacterManagementPath, name)
	}
	values := make([]int64, 0, len(tokens))
	for _, token := range tokens {
		if token.Kind != dnfpvf.TokenInt {
			return nil, fmt.Errorf("%w: %s [%s] non-integer token %q at %d:%d", ErrTableMalformed, CharacterManagementPath, name, token.Raw, token.Line, token.Column)
		}
		values = append(values, token.Int)
	}
	if len(values) == 0 && !allowEmpty {
		return nil, fmt.Errorf("%w: %s [%s]", ErrTableEmpty, CharacterManagementPath, name)
	}
	return values, nil
}

func parsePointRanges(values []int64) ([]pointRange, error) {
	if len(values)%3 != 0 {
		return nil, fmt.Errorf("%w: [%s] integers=%d", ErrTableMalformed, pointBonusSection, len(values))
	}
	rows := make([]pointRange, 0, len(values)/3)
	previousMax := 0
	for index := 0; index < len(values); index += 3 {
		minValue, maxValue, pointValue := values[index], values[index+1], values[index+2]
		if minValue <= 0 || minValue > int64(math.MaxInt) || maxValue < minValue || maxValue > int64(math.MaxInt) || pointValue < 0 {
			return nil, fmt.Errorf("%w: [%s] row=%d min=%d max=%d point=%d", ErrTableMalformed, pointBonusSection, index/3, minValue, maxValue, pointValue)
		}
		minLevel, maxLevel := int(minValue), int(maxValue)
		if minLevel <= previousMax {
			return nil, fmt.Errorf("%w: [%s] row=%d range=%d..%d overlaps or is unsorted after max=%d", ErrTableMalformed, pointBonusSection, index/3, minLevel, maxLevel, previousMax)
		}
		rows = append(rows, pointRange{minLevel: minLevel, maxLevel: maxLevel, point: uint64(pointValue)})
		previousMax = maxLevel
	}
	return rows, nil
}

func parseThresholds(values []int64) ([]uint64, error) {
	thresholds := make([]uint64, len(values))
	var previous uint64
	for index, value := range values {
		if value <= 0 {
			return nil, fmt.Errorf("%w: [%s] index=%d value=%d", ErrTableMalformed, managePointSection, index, value)
		}
		threshold := uint64(value)
		if index > 0 && threshold <= previous {
			return nil, fmt.Errorf("%w: [%s] index=%d value=%d previous=%d", ErrTableMalformed, managePointSection, index, threshold, previous)
		}
		thresholds[index] = threshold
		previous = threshold
	}
	return thresholds, nil
}

func parseLevelValueTable(name string, values []int64, manageLevelMax int, allowEmpty bool) (map[int]uint64, error) {
	if len(values) == 0 {
		if allowEmpty {
			return map[int]uint64{}, nil
		}
		return nil, fmt.Errorf("%w: [%s]", ErrTableEmpty, name)
	}
	if len(values)%2 != 0 {
		return nil, fmt.Errorf("%w: [%s] integers=%d", ErrTableMalformed, name, len(values))
	}
	table := make(map[int]uint64, len(values)/2)
	previousLevel := 0
	for index := 0; index < len(values); index += 2 {
		levelValue, dataValue := values[index], values[index+1]
		if levelValue <= 0 || levelValue > int64(manageLevelMax) || levelValue > int64(math.MaxInt) || dataValue < 0 {
			return nil, fmt.Errorf("%w: [%s] row=%d level=%d value=%d", ErrTableMalformed, name, index/2, levelValue, dataValue)
		}
		level := int(levelValue)
		if level <= previousLevel {
			return nil, fmt.Errorf("%w: [%s] row=%d level=%d previous=%d", ErrTableMalformed, name, index/2, level, previousLevel)
		}
		table[level] = uint64(dataValue)
		previousLevel = level
	}
	return table, nil
}
