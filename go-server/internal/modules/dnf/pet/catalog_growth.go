package pet

import (
	"errors"
	"fmt"
	"math"
	"path"
	"strconv"
	"strings"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

const petCreatureListPath = "creature/creature.lst"

var (
	ErrPetPVFArtifactInvalid     = errors.New("pet artifact PVF definition is invalid")
	ErrPetPVFEvolutionUnresolved = errors.New("pet evolution mapping is unresolved")
	ErrPetPVFEvolutionInvalid    = errors.New("pet evolution PVF definition is invalid")
)

// PetSatietyModifiers is catalog-produced input for satiety calculations.
// Its rate is deliberately private so runtime callers obtain it by resolving
// the actual equipped artifact item ids through PVF.
type PetSatietyModifiers struct {
	foodConsumeRatePercent int64
}

// FoodConsumeRatePercent reports the summed PVF artifact modifier.
func (m PetSatietyModifiers) FoodConsumeRatePercent() int64 {
	return m.foodConsumeRatePercent
}

// FoodConsumeMultiplier follows 86JP's domain rule and preserves its lower
// bound for artifact sets whose summed reduction is -100 percent or lower.
func (m PetSatietyModifiers) FoodConsumeMultiplier() float64 {
	multiplier := 1 + float64(m.foodConsumeRatePercent)/100
	if multiplier <= 0 {
		return 0.01
	}
	return multiplier
}

// ResolveSatietyModifiers verifies each equipped artifact against the runtime
// equipment list/document and sums only typed artifact food-consumption rates.
func (c *PVFCatalog) ResolveSatietyModifiers(artifactItemIDs []int64) (PetSatietyModifiers, error) {
	if c == nil || c.source == nil {
		return PetSatietyModifiers{}, ErrPetPVFSourceRequired
	}
	seen := make(map[int64]struct{}, len(artifactItemIDs))
	var total int64
	for _, itemID := range artifactItemIDs {
		if itemID <= 0 {
			continue
		}
		if _, exists := seen[itemID]; exists {
			continue
		}
		seen[itemID] = struct{}{}
		documentPath, document, err := c.resolveEquipmentDocument(itemID)
		if err != nil {
			return PetSatietyModifiers{}, err
		}
		equipmentType, found := document.Text("equipment type")
		normalizedType := normalizePetPVFType(equipmentType)
		if !found || (normalizedType != "[artifact red]" && normalizedType != "[artifact blue]" && normalizedType != "[artifact green]") {
			return PetSatietyModifiers{}, fmt.Errorf("%w: item_id=%d path=%s equipment_type=%q", ErrPetPVFArtifactInvalid, itemID, documentPath, equipmentType)
		}
		rate, _ := document.Int("creature food consume rate")
		if (rate > 0 && total > math.MaxInt64-rate) || (rate < 0 && total < math.MinInt64-rate) {
			return PetSatietyModifiers{}, fmt.Errorf("%w: food consume rate overflow item_id=%d", ErrPetPVFArtifactInvalid, itemID)
		}
		total += rate
	}
	return PetSatietyModifiers{foodConsumeRatePercent: total}, nil
}

// PetEvolutionDefinition is a fully resolved current-creature -> next-creature
// mapping. It is domain state only and contains no current-EXE packet fields.
type PetEvolutionDefinition struct {
	CurrentItemID     int64
	CurrentCreatureID int64
	TargetItemID      int64
	TargetCreatureID  int64
	RequiredLevel     int
	RequiresQuest     bool
	QuestReference    string
}

// PetEvolutionResolver is the fail-closed boundary used by the growth engine.
// found=false means the creature PVF explicitly has no evolution; an absent or
// malformed required mapping returns an error instead.
type PetEvolutionResolver interface {
	ResolveEvolution(itemID int64) (definition PetEvolutionDefinition, found bool, err error)
}

type petEvolutionIndex struct {
	creaturePaths       map[int64]string
	creatureIDByBase    map[string]int64
	itemByCreatureID    map[int64]int64
	ambiguousCreatureID map[int64]struct{}
}

// ResolveEvolution joins equipment.lst, creature.lst, the current equipment
// document, and the current creature document. Output-index is preferred only
// when it resolves to the PVF-declared target creature; otherwise the unique
// creature-id item mapping is used.
func (c *PVFCatalog) ResolveEvolution(itemID int64) (PetEvolutionDefinition, bool, error) {
	if c == nil || c.source == nil {
		return PetEvolutionDefinition{}, false, ErrPetPVFSourceRequired
	}
	currentPath, currentEquipment, err := c.resolveCreatureEquipment(itemID)
	if err != nil {
		return PetEvolutionDefinition{}, false, err
	}
	index, err := c.petEvolutionIndex()
	if err != nil {
		return PetEvolutionDefinition{}, false, err
	}
	currentCreatureID, found := index.creatureIDByBase[petPVFFileBase(currentPath)]
	if !found || currentCreatureID <= 0 {
		return PetEvolutionDefinition{}, false, fmt.Errorf("%w: item_id=%d path=%s has no creature.lst identity", ErrPetPVFEvolutionUnresolved, itemID, currentPath)
	}
	creaturePath, found := index.creaturePaths[currentCreatureID]
	if !found {
		return PetEvolutionDefinition{}, false, fmt.Errorf("%w: creature_id=%d", ErrPetPVFEvolutionUnresolved, currentCreatureID)
	}
	documentPath, text, err := readPetPVFReference(c.source, "creature", creaturePath)
	if err != nil {
		return PetEvolutionDefinition{}, false, fmt.Errorf("read pet evolution creature_id=%d path=%s: %w", currentCreatureID, creaturePath, err)
	}
	creatureDocument, err := dnfpvf.Parse(documentPath, text)
	if err != nil {
		return PetEvolutionDefinition{}, false, fmt.Errorf("parse pet evolution creature_id=%d path=%s: %w", currentCreatureID, documentPath, err)
	}
	targetCreatureID, targetFound, err := petPVFInteger(creatureDocument, "evolution creature id")
	if err != nil {
		return PetEvolutionDefinition{}, false, fmt.Errorf("%w: creature_id=%d field=evolution creature id: %v", ErrPetPVFEvolutionInvalid, currentCreatureID, err)
	}
	requiredLevel, levelFound, err := petPVFInteger(creatureDocument, "evolution level")
	if err != nil {
		return PetEvolutionDefinition{}, false, fmt.Errorf("%w: creature_id=%d field=evolution level: %v", ErrPetPVFEvolutionInvalid, currentCreatureID, err)
	}
	questReference, requiresQuest := petPVFEvolutionQuest(creatureDocument)
	if (!targetFound || targetCreatureID == 0) && (!levelFound || requiredLevel == 0) && !requiresQuest {
		return PetEvolutionDefinition{}, false, nil
	}
	if !targetFound || targetCreatureID <= 0 || targetCreatureID == currentCreatureID ||
		!levelFound || requiredLevel <= 0 || requiredLevel > MaxCreatureLevel {
		return PetEvolutionDefinition{}, false, fmt.Errorf("%w: creature_id=%d target=%d level=%d quest=%q", ErrPetPVFEvolutionInvalid, currentCreatureID, targetCreatureID, requiredLevel, questReference)
	}

	targetItemID := int64(0)
	if outputItemID, outputFound := currentEquipment.Int("output index"); outputFound && outputItemID > 0 && outputItemID != itemID {
		if outputPath, _, outputErr := c.resolveCreatureEquipment(outputItemID); outputErr == nil {
			if resolvedID, resolved := index.creatureIDByBase[petPVFFileBase(outputPath)]; resolved && resolvedID == targetCreatureID {
				targetItemID = outputItemID
			}
		}
	}
	if targetItemID == 0 {
		if _, ambiguous := index.ambiguousCreatureID[targetCreatureID]; ambiguous {
			return PetEvolutionDefinition{}, false, fmt.Errorf("%w: target creature_id=%d has ambiguous equipment items", ErrPetPVFEvolutionUnresolved, targetCreatureID)
		}
		targetItemID = index.itemByCreatureID[targetCreatureID]
	}
	if targetItemID <= 0 || targetItemID == itemID {
		return PetEvolutionDefinition{}, false, fmt.Errorf("%w: target creature_id=%d item_id=%d", ErrPetPVFEvolutionUnresolved, targetCreatureID, targetItemID)
	}
	targetPath, _, err := c.resolveCreatureEquipment(targetItemID)
	if err != nil {
		return PetEvolutionDefinition{}, false, fmt.Errorf("resolve evolution target item_id=%d: %w", targetItemID, err)
	}
	resolvedTargetID, found := index.creatureIDByBase[petPVFFileBase(targetPath)]
	if !found || resolvedTargetID != targetCreatureID {
		return PetEvolutionDefinition{}, false, fmt.Errorf("%w: target item_id=%d creature_id=%d want=%d", ErrPetPVFEvolutionUnresolved, targetItemID, resolvedTargetID, targetCreatureID)
	}
	return PetEvolutionDefinition{
		CurrentItemID:     itemID,
		CurrentCreatureID: currentCreatureID,
		TargetItemID:      targetItemID,
		TargetCreatureID:  targetCreatureID,
		RequiredLevel:     int(requiredLevel),
		RequiresQuest:     requiresQuest,
		QuestReference:    questReference,
	}, true, nil
}

func (c *PVFCatalog) resolveEquipmentDocument(itemID int64) (string, *dnfpvf.Document, error) {
	if itemID <= 0 || itemID > math.MaxUint32 {
		return "", nil, fmt.Errorf("%w: item_id=%d", ErrPetPVFEquipmentUnresolved, itemID)
	}
	listedPath, found := c.equipmentPaths[itemID]
	if !found {
		return "", nil, fmt.Errorf("%w: item_id=%d", ErrPetPVFEquipmentUnresolved, itemID)
	}
	documentPath, text, err := readPetPVFReference(c.source, "equipment", listedPath)
	if err != nil {
		return "", nil, fmt.Errorf("read pet equipment item_id=%d path=%s: %w", itemID, listedPath, err)
	}
	document, err := dnfpvf.Parse(documentPath, text)
	if err != nil {
		return "", nil, fmt.Errorf("parse pet equipment item_id=%d path=%s: %w", itemID, documentPath, err)
	}
	return documentPath, document, nil
}

func (c *PVFCatalog) petEvolutionIndex() (petEvolutionIndex, error) {
	c.evolutionMu.Lock()
	defer c.evolutionMu.Unlock()
	if !c.evolutionLoaded {
		c.evolutionIndex, c.evolutionErr = loadPetEvolutionIndex(c.source, c.equipmentPaths)
		c.evolutionLoaded = true
	}
	return c.evolutionIndex, c.evolutionErr
}

func loadPetEvolutionIndex(source dnfpvf.Source, equipmentPaths map[int64]string) (petEvolutionIndex, error) {
	text, err := source.ReadText(petCreatureListPath)
	if err != nil {
		return petEvolutionIndex{}, fmt.Errorf("read %s: %w", petCreatureListPath, err)
	}
	document, err := dnfpvf.Parse(petCreatureListPath, text)
	if err != nil {
		return petEvolutionIndex{}, fmt.Errorf("parse %s: %w", petCreatureListPath, err)
	}
	index := petEvolutionIndex{
		creaturePaths:       make(map[int64]string),
		creatureIDByBase:    make(map[string]int64),
		itemByCreatureID:    make(map[int64]int64),
		ambiguousCreatureID: make(map[int64]struct{}),
	}
	for _, entry := range dnfpvf.ParseList(document) {
		creaturePath := cleanPetPVFPath(entry.Path)
		if entry.ID <= 0 || entry.ID > math.MaxUint32 || creaturePath == "" {
			continue
		}
		if previous, exists := index.creaturePaths[entry.ID]; exists && !strings.EqualFold(previous, creaturePath) {
			return petEvolutionIndex{}, fmt.Errorf("%w: duplicate creature_id=%d paths=%q,%q", ErrPetPVFEvolutionInvalid, entry.ID, previous, creaturePath)
		}
		index.creaturePaths[entry.ID] = creaturePath
		base := petPVFFileBase(creaturePath)
		if base == "" {
			continue
		}
		if previous, exists := index.creatureIDByBase[base]; exists && previous != entry.ID {
			index.creatureIDByBase[base] = 0
			continue
		}
		index.creatureIDByBase[base] = entry.ID
	}
	if len(index.creaturePaths) == 0 {
		return petEvolutionIndex{}, fmt.Errorf("%w: %s contains no creature references", ErrPetPVFEvolutionInvalid, petCreatureListPath)
	}
	for itemID, equipmentPath := range equipmentPaths {
		clean := cleanPetPVFPath(equipmentPath)
		if !strings.HasPrefix(strings.ToLower(clean), "creature/") {
			continue
		}
		creatureID := index.creatureIDByBase[petPVFFileBase(clean)]
		if creatureID <= 0 {
			continue
		}
		if previous, exists := index.itemByCreatureID[creatureID]; exists && previous != itemID {
			delete(index.itemByCreatureID, creatureID)
			index.ambiguousCreatureID[creatureID] = struct{}{}
			continue
		}
		if _, ambiguous := index.ambiguousCreatureID[creatureID]; !ambiguous {
			index.itemByCreatureID[creatureID] = itemID
		}
	}
	return index, nil
}

func petPVFFileBase(value string) string {
	clean := cleanPetPVFPath(value)
	base := path.Base(clean)
	return strings.ToLower(strings.TrimSuffix(base, path.Ext(base)))
}

func petPVFInteger(document *dnfpvf.Document, section string) (int64, bool, error) {
	if document == nil {
		return 0, false, nil
	}
	if value, found := document.Int(section); found {
		return value, true, nil
	}
	value, found := document.Text(section)
	if !found || strings.TrimSpace(value) == "" {
		return 0, false, nil
	}
	parsed, err := strconv.ParseInt(strings.TrimSpace(strings.Trim(value, "`")), 10, 64)
	if err != nil {
		return 0, true, err
	}
	return parsed, true, nil
}

func petPVFEvolutionQuest(document *dnfpvf.Document) (string, bool) {
	if document == nil {
		return "", false
	}
	tokens, found := document.Section("evolution quest")
	if !found {
		return "", false
	}
	for _, token := range tokens {
		switch token.Kind {
		case dnfpvf.TokenInt:
			value := strconv.FormatInt(token.Int, 10)
			return value, token.Int > 0
		case dnfpvf.TokenString, dnfpvf.TokenIdent:
			value := strings.TrimSpace(strings.Trim(token.Value, "`"))
			if value == "" || value == "0" || value == "-1" {
				return value, false
			}
			if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
				return value, parsed > 0
			}
			return value, true
		}
	}
	return "", false
}
