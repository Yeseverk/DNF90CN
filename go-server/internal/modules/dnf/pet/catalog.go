package pet

import (
	"errors"
	"fmt"
	"math"
	"path"
	"strings"
	"sync"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

const (
	petEquipmentListPath      = "equipment/equipment.lst"
	petCreatureExperiencePath = "creature/exptable.tbl"
	MaxCreatureLevel          = 50
)

var (
	ErrPetPVFSourceRequired       = errors.New("pet PVF source is required")
	ErrPetPVFEquipmentUnresolved  = errors.New("pet equipment is not present in equipment.lst")
	ErrPetPVFNotCreature          = errors.New("pet equipment type is not creature")
	ErrPetPVFHatchOutputInvalid   = errors.New("pet hatch output index is invalid")
	ErrPetPVFExperienceTableShape = errors.New("pet creature experience table is invalid")
)

// PetHatchDefinition is the typed, PVF-proven egg-to-creature mapping used by
// the hatch owner. It contains domain data only and has no packet fields.
type PetHatchDefinition struct {
	EggItemID      int64
	HatchedItemID  int64
	EggPVFPath     string
	HatchedPVFPath string
	// MinimumLevel is retained as typed output-item metadata. The 86JP hatch
	// transaction still creates a new creature instance at level 1.
	MinimumLevel int
}

// PetHatchResolver lets the mutation owner depend on a real PVF mapping rather
// than inventory Extra fields or request-body item-id guesses.
type PetHatchResolver interface {
	ResolveHatch(eggItemID int64) (PetHatchDefinition, error)
}

// PetCreatureDefinition proves that an equipment-list item is a current
// runtime-PVF creature. It is deliberately small: growth persistence needs an
// item-kind authority, not protocol fields or an inferred slot convention.
type PetCreatureDefinition struct {
	ItemID  int64
	PVFPath string
	Name    string
}

// PetCreatureResolver is the fail-closed item-kind boundary used before any
// equipped creature growth or satiety mutation. Slot 24 is also a valid
// ordinary support-weapon slot, so position and raw instance bytes alone are
// not sufficient proof that an item is a creature.
type PetCreatureResolver interface {
	ResolveCreature(itemID int64) (PetCreatureDefinition, error)
}

// CreatureExperienceTable preserves the 50 values from
// creature/exptable.tbl. Current pet progression is capped at level 50, so
// LevelForExperience consumes the first 49 thresholds exactly like 86JP.
type CreatureExperienceTable struct {
	thresholds [MaxCreatureLevel]int64
}

// Thresholds returns a copy of the authoritative PVF values.
func (t CreatureExperienceTable) Thresholds() []int64 {
	out := make([]int64, len(t.thresholds))
	copy(out, t.thresholds[:])
	return out
}

// LevelForExperience maps cumulative creature experience to level 1..50.
func (t CreatureExperienceTable) LevelForExperience(experience int64) int {
	if experience < 0 {
		experience = 0
	}
	level := 1
	for nextLevel := 2; nextLevel <= MaxCreatureLevel; nextLevel++ {
		if t.thresholds[nextLevel-2] > experience {
			break
		}
		level = nextLevel
	}
	return level
}

// PVFCatalog owns immutable pet item mappings and the creature experience
// table loaded from the runtime Script.pvf.
type PVFCatalog struct {
	source         dnfpvf.Source
	equipmentPaths map[int64]string
	experience     CreatureExperienceTable

	evolutionMu     sync.Mutex
	evolutionLoaded bool
	evolutionIndex  petEvolutionIndex
	evolutionErr    error
}

// NewPVFCatalog loads only the authoritative indexes needed for the first pet
// domain closure: equipment item references and creature experience values.
func NewPVFCatalog(source dnfpvf.Source) (*PVFCatalog, error) {
	if source == nil {
		return nil, ErrPetPVFSourceRequired
	}
	equipmentPaths, err := loadPetEquipmentPaths(source)
	if err != nil {
		return nil, err
	}
	experience, err := loadCreatureExperienceTable(source)
	if err != nil {
		return nil, err
	}
	return &PVFCatalog{
		source:         source,
		equipmentPaths: equipmentPaths,
		experience:     experience,
	}, nil
}

// ExperienceTable returns the immutable-by-value creature experience table.
func (c *PVFCatalog) ExperienceTable() CreatureExperienceTable {
	if c == nil {
		return CreatureExperienceTable{}
	}
	return c.experience
}

// ResolveCreature independently verifies the current item against the active
// equipment.lst and its equipment document. The same typed proof is reused by
// hatch/evolution code, while callers cannot inject a request-body item kind.
func (c *PVFCatalog) ResolveCreature(itemID int64) (PetCreatureDefinition, error) {
	if c == nil || c.source == nil {
		return PetCreatureDefinition{}, ErrPetPVFSourceRequired
	}
	documentPath, document, err := c.resolveCreatureEquipment(itemID)
	if err != nil {
		return PetCreatureDefinition{}, err
	}
	name, _ := document.Text("name")
	return PetCreatureDefinition{
		ItemID:  itemID,
		PVFPath: documentPath,
		Name:    strings.TrimSpace(name),
	}, nil
}

// ResolveHatch applies the 86JP domain rule to current runtime PVF data: the
// source must be [creature] equipment and have a positive [output index]
// different from itself. The output is independently resolved and verified as
// creature equipment before the mapping can authorize a mutation.
func (c *PVFCatalog) ResolveHatch(eggItemID int64) (PetHatchDefinition, error) {
	if c == nil || c.source == nil {
		return PetHatchDefinition{}, ErrPetPVFSourceRequired
	}
	eggPath, eggDocument, err := c.resolveCreatureEquipment(eggItemID)
	if err != nil {
		return PetHatchDefinition{}, err
	}
	outputItemID, found := eggDocument.Int("output index")
	if !found || outputItemID <= 0 || outputItemID > math.MaxUint32 || outputItemID == eggItemID {
		return PetHatchDefinition{}, fmt.Errorf("%w: item_id=%d output_index=%d", ErrPetPVFHatchOutputInvalid, eggItemID, outputItemID)
	}
	hatchedPath, hatchedDocument, err := c.resolveCreatureEquipment(outputItemID)
	if err != nil {
		return PetHatchDefinition{}, fmt.Errorf("resolve pet hatch output item_id=%d: %w", outputItemID, err)
	}
	minimumLevel := 1
	if value, found := hatchedDocument.Int("minimum level"); found {
		if value <= 0 || value > MaxCreatureLevel {
			return PetHatchDefinition{}, fmt.Errorf("%w: output item_id=%d minimum_level=%d", ErrPetPVFHatchOutputInvalid, outputItemID, value)
		}
		minimumLevel = int(value)
	}
	return PetHatchDefinition{
		EggItemID:      eggItemID,
		HatchedItemID:  outputItemID,
		EggPVFPath:     eggPath,
		HatchedPVFPath: hatchedPath,
		MinimumLevel:   minimumLevel,
	}, nil
}

func (c *PVFCatalog) resolveCreatureEquipment(itemID int64) (string, *dnfpvf.Document, error) {
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
	equipmentType, found := document.Text("equipment type")
	if !found || !strings.Contains(normalizePetPVFType(equipmentType), "[creature]") {
		return "", nil, fmt.Errorf("%w: item_id=%d equipment_type=%q", ErrPetPVFNotCreature, itemID, equipmentType)
	}
	return documentPath, document, nil
}

func loadPetEquipmentPaths(source dnfpvf.Source) (map[int64]string, error) {
	text, err := source.ReadText(petEquipmentListPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", petEquipmentListPath, err)
	}
	document, err := dnfpvf.Parse(petEquipmentListPath, text)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", petEquipmentListPath, err)
	}
	entries := dnfpvf.ParseList(document)
	paths := make(map[int64]string, len(entries))
	for _, entry := range entries {
		listedPath := cleanPetPVFPath(entry.Path)
		if entry.ID <= 0 || entry.ID > math.MaxUint32 || listedPath == "" {
			continue
		}
		if previous, exists := paths[entry.ID]; exists && !strings.EqualFold(previous, listedPath) {
			return nil, fmt.Errorf("parse %s: duplicate item_id=%d paths=%q,%q", petEquipmentListPath, entry.ID, previous, listedPath)
		}
		paths[entry.ID] = listedPath
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("parse %s: no valid item references", petEquipmentListPath)
	}
	return paths, nil
}

func loadCreatureExperienceTable(source dnfpvf.Source) (CreatureExperienceTable, error) {
	if source == nil {
		return CreatureExperienceTable{}, ErrPetPVFSourceRequired
	}
	text, err := source.ReadText(petCreatureExperiencePath)
	if err != nil {
		return CreatureExperienceTable{}, fmt.Errorf("read %s: %w", petCreatureExperiencePath, err)
	}
	document, err := dnfpvf.Parse(petCreatureExperiencePath, text)
	if err != nil {
		return CreatureExperienceTable{}, fmt.Errorf("parse %s: %w", petCreatureExperiencePath, err)
	}
	values := make([]int64, 0, MaxCreatureLevel)
	for _, token := range document.Tokens {
		if token.Kind == dnfpvf.TokenInt {
			values = append(values, token.Int)
		}
	}
	if len(values) < MaxCreatureLevel {
		return CreatureExperienceTable{}, fmt.Errorf("%w: path=%s values=%d want_at_least=%d", ErrPetPVFExperienceTableShape, petCreatureExperiencePath, len(values), MaxCreatureLevel)
	}
	var table CreatureExperienceTable
	for index, value := range values[:MaxCreatureLevel] {
		if value <= 0 || (index > 0 && value <= values[index-1]) {
			return CreatureExperienceTable{}, fmt.Errorf("%w: path=%s index=%d value=%d", ErrPetPVFExperienceTableShape, petCreatureExperiencePath, index, value)
		}
		table.thresholds[index] = value
	}
	return table, nil
}

func readPetPVFReference(source dnfpvf.Source, root, listedPath string) (string, string, error) {
	clean := cleanPetPVFPath(listedPath)
	candidates := []string{clean}
	root = cleanPetPVFPath(root)
	if root != "" && !strings.HasPrefix(strings.ToLower(clean), strings.ToLower(root)+"/") {
		candidates = append(candidates, path.Join(root, clean))
	}
	var lastErr error
	for _, candidate := range candidates {
		text, err := source.ReadText(candidate)
		if err == nil {
			return candidate, text, nil
		}
		lastErr = err
	}
	return "", "", lastErr
}

func cleanPetPVFPath(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	value = strings.TrimPrefix(value, "./")
	return strings.TrimPrefix(value, "/")
}

func normalizePetPVFType(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(strings.ReplaceAll(value, "`", ""))), " "))
}
