package dnfbridge

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

const currentRentalSystemPVFPath = "etc/chnrentsystem/rentsysteminfo.etc"

var (
	errRentalCatalogInvalid = errors.New("dnf rental catalog is invalid")
	errRentalItemInvalid    = errors.New("dnf rental item is invalid")
)

type currentRentalTier struct {
	Tier         int
	MinimumLevel int
	Opaque       [5]int64
}

type currentRentalGroupKey struct {
	JobTag string
	Tier   int
}

type currentRentalCatalog struct {
	Limit        uint32
	GoldPerPoint int64
	Tiers        []currentRentalTier
	Groups       map[currentRentalGroupKey]map[uint32]uint32
}

func parseCurrentRentalCatalog(source initialEquipmentTextSource) (*currentRentalCatalog, error) {
	if source == nil {
		return nil, fmt.Errorf("%w: PVF source is nil", errRentalCatalogInvalid)
	}
	text, err := source.ReadText(currentRentalSystemPVFPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", currentRentalSystemPVFPath, err)
	}
	doc, err := dnfpvf.Parse(currentRentalSystemPVFPath, text)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", currentRentalSystemPVFPath, err)
	}
	limit, ok := doc.Int("limit point")
	if !ok || limit <= 0 || limit > math.MaxUint32 {
		return nil, fmt.Errorf("%w: invalid [limit point] %d", errRentalCatalogInvalid, limit)
	}
	pointCharge, ok := doc.Section("point charge")
	if !ok {
		return nil, fmt.Errorf("%w: missing [point charge]", errRentalCatalogInvalid)
	}
	chargeTexts := make([]string, 0, 2)
	chargeInts := make([]int64, 0, 2)
	for _, token := range pointCharge {
		switch token.Kind {
		case dnfpvf.TokenString, dnfpvf.TokenIdent:
			chargeTexts = append(chargeTexts, strings.ToLower(strings.TrimSpace(token.Value)))
		case dnfpvf.TokenInt:
			chargeInts = append(chargeInts, token.Int)
		}
	}
	if len(chargeTexts) != 2 || len(chargeInts) != 2 || chargeTexts[0] != "pdungeon" || chargeTexts[1] != "gold" || chargeInts[0] != 1 || chargeInts[1] <= 0 {
		return nil, fmt.Errorf("%w: unsupported [point charge] texts=%v ints=%v", errRentalCatalogInvalid, chargeTexts, chargeInts)
	}

	tierValues := doc.Ints("section")
	if len(tierValues) == 0 || len(tierValues)%7 != 0 {
		return nil, fmt.Errorf("%w: [section] integer count=%d", errRentalCatalogInvalid, len(tierValues))
	}
	tiers := make([]currentRentalTier, 0, len(tierValues)/7)
	seenTier := make(map[int]struct{}, len(tierValues)/7)
	previousMinimum := 0
	for offset := 0; offset < len(tierValues); offset += 7 {
		tierValue, minimumValue := tierValues[offset], tierValues[offset+1]
		if tierValue < 0 || tierValue > math.MaxUint16 || minimumValue <= 0 || minimumValue > math.MaxUint8 {
			return nil, fmt.Errorf("%w: invalid tier row=%v", errRentalCatalogInvalid, tierValues[offset:offset+7])
		}
		tier := int(tierValue)
		if _, exists := seenTier[tier]; exists || int(minimumValue) <= previousMinimum {
			return nil, fmt.Errorf("%w: duplicate/nonascending tier row=%v", errRentalCatalogInvalid, tierValues[offset:offset+7])
		}
		seenTier[tier] = struct{}{}
		previousMinimum = int(minimumValue)
		row := currentRentalTier{Tier: tier, MinimumLevel: int(minimumValue)}
		copy(row.Opaque[:], tierValues[offset+2:offset+7])
		tiers = append(tiers, row)
	}

	groups := make(map[currentRentalGroupKey]map[uint32]uint32)
	for index, section := range doc.Sections {
		if currentRentalSectionName(section.Name) != "group" {
			continue
		}
		groupTokens, ok := currentRentalSectionTokens(doc, section)
		if !ok || index+1 >= len(doc.Sections) || currentRentalSectionName(doc.Sections[index+1].Name) != "package selection" {
			return nil, fmt.Errorf("%w: group at section %d has no package selection", errRentalCatalogInvalid, index)
		}
		jobTag := ""
		tier := int64(-1)
		for _, token := range groupTokens {
			switch token.Kind {
			case dnfpvf.TokenString, dnfpvf.TokenIdent:
				if jobTag == "" {
					jobTag = currentRentalJobTag(token.Value)
				}
			case dnfpvf.TokenInt:
				if tier < 0 {
					tier = token.Int
				}
			}
		}
		if jobTag == "" || tier < 0 || tier > math.MaxUint16 {
			return nil, fmt.Errorf("%w: invalid group tokens at section %d", errRentalCatalogInvalid, index)
		}
		key := currentRentalGroupKey{JobTag: jobTag, Tier: int(tier)}
		if _, exists := groups[key]; exists {
			return nil, fmt.Errorf("%w: duplicate group job=%s tier=%d", errRentalCatalogInvalid, jobTag, tier)
		}
		packageTokens, ok := currentRentalSectionTokens(doc, doc.Sections[index+1])
		if !ok {
			return nil, fmt.Errorf("%w: invalid package section job=%s tier=%d", errRentalCatalogInvalid, jobTag, tier)
		}
		values := make([]int64, 0, len(packageTokens))
		for _, token := range packageTokens {
			if token.Kind == dnfpvf.TokenInt {
				values = append(values, token.Int)
			}
		}
		if len(values) == 0 || len(values)%2 != 0 {
			return nil, fmt.Errorf("%w: package pair count job=%s tier=%d values=%d", errRentalCatalogInvalid, jobTag, tier, len(values))
		}
		items := make(map[uint32]uint32, len(values)/2)
		for pair := 0; pair < len(values); pair += 2 {
			itemID, cost := values[pair], values[pair+1]
			if itemID <= 0 || itemID > math.MaxUint32 || cost <= 0 || cost > math.MaxUint32 {
				return nil, fmt.Errorf("%w: invalid item/cost job=%s tier=%d pair=%v", errRentalCatalogInvalid, jobTag, tier, values[pair:pair+2])
			}
			id := uint32(itemID)
			if _, exists := items[id]; exists {
				return nil, fmt.Errorf("%w: duplicate item job=%s tier=%d item=%d", errRentalCatalogInvalid, jobTag, tier, id)
			}
			items[id] = uint32(cost)
		}
		groups[key] = items
	}
	if len(groups) == 0 {
		return nil, fmt.Errorf("%w: no rental groups", errRentalCatalogInvalid)
	}
	return &currentRentalCatalog{
		Limit:        uint32(limit),
		GoldPerPoint: chargeInts[1],
		Tiers:        tiers,
		Groups:       groups,
	}, nil
}

func (c *currentRentalCatalog) tierForLevel(level int) (int, bool) {
	if c == nil || level <= 0 {
		return 0, false
	}
	tier, found := 0, false
	for _, row := range c.Tiers {
		if row.MinimumLevel > level {
			break
		}
		tier, found = row.Tier, true
	}
	return tier, found
}

func (c *currentRentalCatalog) itemCost(jobTag string, tier int, itemID uint32) (uint32, bool) {
	if c == nil {
		return 0, false
	}
	items := c.Groups[currentRentalGroupKey{JobTag: currentRentalJobTag(jobTag), Tier: tier}]
	cost, ok := items[itemID]
	return cost, ok
}

func currentRentalCharacterJobTag(source initialEquipmentTextSource, job byte) (string, error) {
	characterList, err := source.ReadText(initialEquipmentCharacterList)
	if err != nil {
		return "", err
	}
	refPath, found, err := initialCharacterPVFPath(characterList, job)
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("%w: character job=%d is absent from %s", errRentalCatalogInvalid, job, initialEquipmentCharacterList)
	}
	characterText, _, err := readInitialPVFText(source, initialPVFPath("character", refPath), refPath)
	if err != nil {
		return "", err
	}
	jobTag := currentRentalJobTag(initialEquipmentJobTag(characterText))
	if jobTag == "" {
		return "", fmt.Errorf("%w: character job=%d has no [job] tag", errRentalCatalogInvalid, job)
	}
	return jobTag, nil
}

func (s *Service) currentRentalSources(ctx context.Context) (*currentRentalCatalog, initialEquipmentTextSource, *pvfDungeonDropCatalog, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, nil, err
	}
	s.initialEquipmentMu.Lock()
	archive, err := s.initialEquipmentArchiveLocked()
	s.initialEquipmentMu.Unlock()
	if err != nil {
		return nil, nil, nil, err
	}
	catalog, err := parseCurrentRentalCatalog(archive)
	if err != nil {
		return nil, nil, nil, err
	}
	if s.dungeonMonsterTable == nil {
		return nil, nil, nil, fmt.Errorf("%w: dungeon item catalog is unavailable", errRentalCatalogInvalid)
	}
	dropCatalog, err := s.dungeonMonsterTable.DropCatalog()
	if err != nil {
		return nil, nil, nil, err
	}
	return catalog, archive, dropCatalog, nil
}

func validateCurrentRentalItem(source initialEquipmentTextSource, items *pvfDungeonDropCatalog, itemID uint32, jobTag string) (dungeonDropItemDefinition, error) {
	definition, err := items.ResolveItem(itemID)
	if err != nil {
		return dungeonDropItemDefinition{}, err
	}
	if definition.Kind != dungeonDropItemEquipment || definition.Durability == 0 || definition.SlotStart != 9 || definition.SlotEnd != 64 {
		return dungeonDropItemDefinition{}, fmt.Errorf("%w: item=%d is not a durable equipment item", errRentalItemInvalid, itemID)
	}
	text, err := source.ReadText(definition.PVFPath)
	if err != nil {
		return dungeonDropItemDefinition{}, err
	}
	doc, err := dnfpvf.Parse(definition.PVFPath, text)
	if err != nil {
		return dungeonDropItemDefinition{}, err
	}
	if !strings.Contains(strings.ToLower(text), "trade delete") {
		return dungeonDropItemDefinition{}, fmt.Errorf("%w: item=%d lacks [trade delete]", errRentalItemInvalid, itemID)
	}
	if !strings.Contains(text, "24") {
		return dungeonDropItemDefinition{}, fmt.Errorf("%w: item=%d lacks the PVF 24-hour rental marker", errRentalItemInvalid, itemID)
	}
	usableJobs := doc.Texts("usable job")
	if len(usableJobs) > 0 {
		want := currentRentalJobTag(jobTag)
		usable := false
		for _, candidate := range usableJobs {
			normalized := currentRentalJobTag(candidate)
			if normalized == want || normalized == "[all]" || normalized == "all" {
				usable = true
				break
			}
		}
		if !usable {
			return dungeonDropItemDefinition{}, fmt.Errorf("%w: item=%d is not usable by %s", errRentalItemInvalid, itemID, want)
		}
	}
	return definition, nil
}

func currentRentalSectionTokens(doc *dnfpvf.Document, section dnfpvf.Section) ([]dnfpvf.Token, bool) {
	if doc == nil || section.Start < 0 || section.Start > section.End || section.End > len(doc.Tokens) {
		return nil, false
	}
	return doc.Tokens[section.Start:section.End], true
}

func currentRentalSectionName(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}

func currentRentalJobTag(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}
