package combatpower

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

const (
	equipmentListPath = "equipment/equipment.lst"
	partSetListPath   = "etc/equipmentpartset.etc"
)

type Catalog struct {
	source dnfpvf.Source

	mu        sync.Mutex
	itemPaths map[int64]string
	setPaths  map[int64]string
	items     map[int64]ItemDefinition
	sets      map[int64]SetDefinition
}

func Load(ctx context.Context, source dnfpvf.Source) (*Catalog, error) {
	if source == nil {
		return nil, ErrSourceRequired
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	equipmentText, err := source.ReadText(equipmentListPath)
	if err != nil {
		return nil, fmt.Errorf("read combat power equipment list: %w", err)
	}
	equipmentDoc, err := dnfpvf.Parse(equipmentListPath, equipmentText)
	if err != nil {
		return nil, fmt.Errorf("parse combat power equipment list: %w", err)
	}
	itemPaths := make(map[int64]string)
	for _, entry := range dnfpvf.ParseList(equipmentDoc) {
		if entry.ID <= 0 {
			continue
		}
		itemPaths[entry.ID] = resolvePVFReference(equipmentListPath, entry.Path)
	}

	setText, err := source.ReadText(partSetListPath)
	if err != nil {
		return nil, fmt.Errorf("read combat power part-set list: %w", err)
	}
	setDoc, err := dnfpvf.Parse(partSetListPath, setText)
	if err != nil {
		return nil, fmt.Errorf("parse combat power part-set list: %w", err)
	}
	setPaths := parsePartSetPaths(setDoc)

	return &Catalog{
		source:    source,
		itemPaths: itemPaths,
		setPaths:  setPaths,
		items:     make(map[int64]ItemDefinition),
		sets:      make(map[int64]SetDefinition),
	}, nil
}

func (c *Catalog) Snapshot() (items, sets int) {
	if c == nil {
		return 0, 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.itemPaths), len(c.setPaths)
}

func (c *Catalog) Aggregate(ctx context.Context, itemIDs []int64) (Result, error) {
	if c == nil || c.source == nil {
		return Result{}, ErrSourceRequired
	}
	if ctx == nil {
		ctx = context.Background()
	}
	result := Result{}
	setPieces := make(map[int64]int)
	for _, itemID := range itemIDs {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		if itemID <= 0 {
			continue
		}
		item, err := c.item(ctx, itemID)
		if err != nil {
			return Result{}, err
		}
		result.Affixes.Add(item.Affixes)
		result.EquippedItems++
		if item.EquipmentScore > 0 {
			result.ScoredItems++
			result.PVFEquipmentScore += item.EquipmentScore
		}
		if item.Rarity == 4 && item.MinimumLevel >= 90 {
			result.Level90EpicItems++
		}
		if item.PartSetID > 0 {
			setPieces[item.PartSetID]++
		}
	}

	setIDs := make([]int64, 0, len(setPieces))
	for setID := range setPieces {
		setIDs = append(setIDs, setID)
	}
	sort.Slice(setIDs, func(i, j int) bool { return setIDs[i] < setIDs[j] })
	for _, setID := range setIDs {
		pieces := setPieces[setID]
		definition, err := c.set(ctx, setID)
		if err != nil {
			return Result{}, err
		}
		active := false
		for _, ability := range definition.Abilities {
			if ability.RequiredPieces <= 0 || pieces < ability.RequiredPieces {
				continue
			}
			result.Affixes.Add(ability.Affixes)
			active = true
		}
		if active {
			result.ActiveSets = append(result.ActiveSets, ActiveSet{ID: setID, Pieces: pieces})
		}
	}
	return result, nil
}

func (c *Catalog) item(ctx context.Context, itemID int64) (ItemDefinition, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if item, ok := c.items[itemID]; ok {
		return item, nil
	}
	itemPath := c.itemPaths[itemID]
	if itemPath == "" {
		return ItemDefinition{}, fmt.Errorf("%w: item %d", ErrItemMissing, itemID)
	}
	if err := ctx.Err(); err != nil {
		return ItemDefinition{}, err
	}
	text, err := c.source.ReadText(itemPath)
	if err != nil {
		return ItemDefinition{}, fmt.Errorf("read combat power item %d at %s: %w", itemID, itemPath, err)
	}
	doc, err := dnfpvf.Parse(itemPath, text)
	if err != nil {
		return ItemDefinition{}, fmt.Errorf("parse combat power item %d at %s: %w", itemID, itemPath, err)
	}
	partSetID, _ := doc.Int("part set index")
	rarity, _ := doc.Int("rarity")
	minimumLevel, _ := doc.Int("minimum level")
	grade, _ := doc.Int("grade")
	equipmentType, _ := doc.Text("equipment type")
	item := ItemDefinition{
		ID:            itemID,
		Path:          itemPath,
		PartSetID:     partSetID,
		Rarity:        boundedPVFScoreMetadata(rarity, 10),
		MinimumLevel:  boundedPVFScoreMetadata(minimumLevel, 200),
		Grade:         boundedPVFScoreMetadata(grade, 200),
		EquipmentType: strings.TrimSpace(equipmentType),
		Affixes:       parseDocumentAffixes(doc),
	}
	item.EquipmentScore = pvfEquipmentBaseScore(item)
	c.items[itemID] = item
	return item, nil
}

// pvfEquipmentBaseScore is the local V6 calibration for the equipment-grade
// part of the TGP-style display. The original TGP coefficients were never
// published, so this must remain an explicit, auditable projection from the
// runtime PVF instead of an item-ID table: required level carries the largest
// weight, PVF grade refines items at the same level, and rarity is the final
// quality tier. Every current actor slot (including avatars, creature and
// creature artifacts) is eligible when its authoritative PVF document carries
// these fields.
func pvfEquipmentBaseScore(item ItemDefinition) int {
	return item.MinimumLevel*20 + item.Grade*5 + item.Rarity*100
}

func boundedPVFScoreMetadata(value int64, maximum int) int {
	if value <= 0 {
		return 0
	}
	if value >= int64(maximum) {
		return maximum
	}
	return int(value)
}

func (c *Catalog) set(ctx context.Context, setID int64) (SetDefinition, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if definition, ok := c.sets[setID]; ok {
		return definition, nil
	}
	setPath := c.setPaths[setID]
	if setPath == "" {
		return SetDefinition{}, fmt.Errorf("%w: part set %d", ErrItemMissing, setID)
	}
	if err := ctx.Err(); err != nil {
		return SetDefinition{}, err
	}
	text, err := c.source.ReadText(setPath)
	if err != nil {
		return SetDefinition{}, fmt.Errorf("read combat power part set %d at %s: %w", setID, setPath, err)
	}
	doc, err := dnfpvf.Parse(setPath, text)
	if err != nil {
		return SetDefinition{}, fmt.Errorf("parse combat power part set %d at %s: %w", setID, setPath, err)
	}
	definition := SetDefinition{ID: setID, Path: setPath, Abilities: parseSetAbilities(doc)}
	c.sets[setID] = definition
	return definition, nil
}

func parsePartSetPaths(doc *dnfpvf.Document) map[int64]string {
	out := make(map[int64]string)
	if doc == nil {
		return out
	}
	for index := 0; index+1 < len(doc.Tokens); index++ {
		id := doc.Tokens[index]
		ref := doc.Tokens[index+1]
		if id.Kind != dnfpvf.TokenInt || id.Int <= 0 ||
			(ref.Kind != dnfpvf.TokenString && ref.Kind != dnfpvf.TokenIdent) ||
			!strings.HasSuffix(strings.ToLower(strings.TrimSpace(ref.Value)), ".equ") {
			continue
		}
		out[id.Int] = resolvePVFReference("equipment/equipmentpartset.etc", ref.Value)
	}
	return out
}

func resolvePVFReference(listPath, reference string) string {
	reference = strings.TrimSpace(strings.Trim(reference, "`\"'"))
	reference = strings.ReplaceAll(reference, "\\", "/")
	if strings.HasPrefix(strings.ToLower(reference), "equipment/") {
		return path.Clean(reference)
	}
	return path.Clean(path.Join(path.Dir(listPath), reference))
}
