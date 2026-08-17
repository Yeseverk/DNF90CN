package dnfbridge

import (
	"errors"
	"fmt"
	"math"
	"path"
	"strings"
	"sync"
	"time"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

const (
	dungeonDropStackableList           = "stackable/stackable.lst"
	dungeonDropEquipmentList           = "equipment/equipment.lst"
	dungeonDropEquipmentDictionaryPath = "Etc/ItemDictionary/ItemDictionary.etc"
	currentPVFExpirationDateLayout     = "2006-01-02 15:04:05"
)

var currentPVFExpirationDateLocation = time.FixedZone("CST", 8*60*60)

var (
	errDungeonDropSourceRequired = errors.New("dnf dungeon drop PVF source is required")
	errDungeonDropItemUnresolved = errors.New("dnf dungeon drop item is not present in a supported PVF item list")
)

type dungeonDropItemKind string

const (
	dungeonDropItemStackable dungeonDropItemKind = "stackable"
	dungeonDropItemEquipment dungeonDropItemKind = "equipment"
)

// dungeonMonsterDropPoolEntry is one exact (item id, weight) pair from a
// monster document's [item] section. Weight selects an item within the pool;
// it is not treated as a drop probability.
type dungeonMonsterDropPoolEntry struct {
	ItemID uint32
	Weight uint32
}

type dungeonDropItemDefinition struct {
	ItemID                    uint32
	Kind                      dungeonDropItemKind
	PVFPath                   string
	AttachType                string
	EquipmentType             string
	Grade                     int64
	StackableType             string
	EquipmentEffectID         uint16
	StackLimit                int64
	SlotStart                 int16
	SlotEnd                   int16
	Durability                uint16
	ExpirationDate            time.Time
	UsablePeriodDays          int64
	Price                     int64
	PriceFound                bool
	ActionType                string
	DamageFontIndex           uint16
	DamageFontExpirationMode  alignedcmd.DamageFontExpirationMode
	DamageFontPeriodDays      int64
	DamageFontFixedExpiration time.Time
}

type dungeonDropItemReference struct {
	kind dungeonDropItemKind
	path string
}

// dungeonDropGradeRarityKey mirrors the C# domain pool key.  It is not a
// protocol value: grade and rarity are read from the runtime PVF and only
// select a real item definition before the existing op38 item wire is built.
type dungeonDropGradeRarityKey struct {
	Grade  int64
	Rarity int64
}

type dungeonDropWeightedItem struct {
	ItemID uint32
	Weight uint32
}

// pvfDungeonDropCatalog owns only static PVF mapping. It deliberately does
// not decide how often a monster drops, allocate a scene slot, or mutate the
// player's inventory. Those are separate runtime/asset transactions.
type pvfDungeonDropCatalog struct {
	mu sync.Mutex

	source       dnfpvf.Source
	monsterPaths map[int64]string
	itemRefs     map[uint32]dungeonDropItemReference
	stackableIDs []uint32
	poolCache    map[int64][]dungeonMonsterDropPoolEntry
	itemCache    map[uint32]dungeonDropItemDefinition

	// The generic pools deliberately stay separate from itemCache.  Loading
	// every eligible runtime PVF item only happens once, on the first actual
	// generic drop roll, and has no effect on packet layout or persistence.
	genericLoaded     bool
	genericErr        error
	genericStackables map[dungeonDropGradeRarityKey][]uint32
	genericEquipment  map[dungeonDropGradeRarityKey][]dungeonDropWeightedItem
}

func newPVFDungeonDropCatalog(source dnfpvf.Source) (*pvfDungeonDropCatalog, error) {
	if source == nil {
		return nil, errDungeonDropSourceRequired
	}
	monsterPaths, _, err := loadDungeonDropList(source, "monster/monster.lst")
	if err != nil {
		return nil, fmt.Errorf("load dungeon drop monster list: %w", err)
	}
	stackablePaths, stackableIDs, err := loadDungeonDropList(source, dungeonDropStackableList)
	if err != nil {
		return nil, fmt.Errorf("load dungeon drop stackable list: %w", err)
	}
	equipmentPaths, _, err := loadDungeonDropList(source, dungeonDropEquipmentList)
	if err != nil {
		return nil, fmt.Errorf("load dungeon drop equipment list: %w", err)
	}
	itemRefs := make(map[uint32]dungeonDropItemReference, len(stackablePaths)+len(equipmentPaths))
	for itemID, itemPath := range stackablePaths {
		if itemID <= 0 || itemID > math.MaxUint32 {
			continue
		}
		itemRefs[uint32(itemID)] = dungeonDropItemReference{kind: dungeonDropItemStackable, path: itemPath}
	}
	for itemID, itemPath := range equipmentPaths {
		if itemID <= 0 || itemID > math.MaxUint32 {
			continue
		}
		if _, exists := itemRefs[uint32(itemID)]; !exists {
			itemRefs[uint32(itemID)] = dungeonDropItemReference{kind: dungeonDropItemEquipment, path: itemPath}
		}
	}
	return &pvfDungeonDropCatalog{
		source:       source,
		monsterPaths: monsterPaths,
		itemRefs:     itemRefs,
		stackableIDs: append([]uint32(nil), stackableIDs...),
		poolCache:    make(map[int64][]dungeonMonsterDropPoolEntry),
		itemCache:    make(map[uint32]dungeonDropItemDefinition),
	}, nil
}

func loadDungeonDropList(source dnfpvf.Source, listPath string) (map[int64]string, []uint32, error) {
	text, err := source.ReadText(listPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", listPath, err)
	}
	document, err := dnfpvf.Parse(listPath, text)
	if err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", listPath, err)
	}
	entries := dnfpvf.ParseList(document)
	out := make(map[int64]string, len(entries))
	order := make([]uint32, 0, len(entries))
	for _, entry := range entries {
		clean := cleanDungeonDropPath(entry.Path)
		if entry.ID <= 0 || entry.ID > math.MaxUint32 || clean == "" {
			continue
		}
		if _, exists := out[entry.ID]; !exists {
			out[entry.ID] = clean
			order = append(order, uint32(entry.ID))
		}
	}
	return out, order, nil
}

func (c *pvfDungeonDropCatalog) MonsterPool(monsterID int64) ([]dungeonMonsterDropPoolEntry, error) {
	if c == nil || c.source == nil {
		return nil, errDungeonDropSourceRequired
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if cached, ok := c.poolCache[monsterID]; ok {
		return cloneDungeonMonsterDropPool(cached), nil
	}
	listedPath, ok := c.monsterPaths[monsterID]
	if !ok {
		return nil, nil
	}
	docPath, text, err := readDungeonDropText(c.source, "monster", listedPath)
	if err != nil {
		return nil, fmt.Errorf("read drop pool monster=%d path=%s: %w", monsterID, listedPath, err)
	}
	document, err := dnfpvf.Parse(docPath, text)
	if err != nil {
		return nil, fmt.Errorf("parse drop pool monster=%d path=%s: %w", monsterID, docPath, err)
	}
	pool := parseDungeonMonsterDropPool(document)
	c.poolCache[monsterID] = cloneDungeonMonsterDropPool(pool)
	return cloneDungeonMonsterDropPool(pool), nil
}

func (c *pvfDungeonDropCatalog) ResolveItem(itemID uint32) (dungeonDropItemDefinition, error) {
	if c == nil || c.source == nil {
		return dungeonDropItemDefinition{}, errDungeonDropSourceRequired
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if cached, ok := c.itemCache[itemID]; ok {
		return cached, nil
	}
	reference, ok := c.itemRefs[itemID]
	if !ok {
		return dungeonDropItemDefinition{}, fmt.Errorf("%w: item_id=%d", errDungeonDropItemUnresolved, itemID)
	}
	root := string(reference.kind)
	docPath, text, err := readDungeonDropText(c.source, root, reference.path)
	if err != nil {
		return dungeonDropItemDefinition{}, fmt.Errorf("read drop item=%d path=%s: %w", itemID, reference.path, err)
	}
	document, err := dnfpvf.Parse(docPath, text)
	if err != nil {
		return dungeonDropItemDefinition{}, fmt.Errorf("parse drop item=%d path=%s: %w", itemID, docPath, err)
	}
	definition := dungeonDropItemDefinition{ItemID: itemID, Kind: reference.kind, PVFPath: docPath}
	definition.AttachType, _ = document.Text("attach type")
	if price, found := document.Int("price"); found && price >= 0 {
		definition.Price = price
		definition.PriceFound = true
	}
	expirationDate, err := parseCurrentPVFExpirationDate(document)
	if err != nil {
		return dungeonDropItemDefinition{}, fmt.Errorf("parse drop item=%d path=%s expiration date: %w", itemID, docPath, err)
	}
	definition.ExpirationDate = expirationDate
	if usablePeriod, found := document.Int("usable period"); found && usablePeriod > 0 {
		definition.UsablePeriodDays = usablePeriod
	}
	if reference.kind == dungeonDropItemEquipment {
		definition.SlotStart, definition.SlotEnd = 9, 64
		definition.EquipmentType, _ = document.Text("equipment type")
		definition.Grade, _ = document.Int("grade")
		for _, name := range []string{"durability", "maximum durability", "max durability"} {
			value, found := document.Int(name)
			if !found || value <= 0 {
				continue
			}
			if value > math.MaxUint16 {
				value = math.MaxUint16
			}
			definition.Durability = uint16(value)
			break
		}
	} else {
		definition.StackableType, _ = document.Text("stackable type")
		if effectID, found := document.Int("int data"); found && effectID > 0 && effectID <= math.MaxUint16 {
			definition.EquipmentEffectID = uint16(effectID)
		}
		if limit, found := document.Int("stack limit"); found && limit > 0 {
			definition.StackLimit = limit
		}
		definition.SlotStart, definition.SlotEnd = dungeonDropStackableSlotRange(definition.StackableType)
		if err := parseCurrentDamageFontDefinition(document, &definition); err != nil {
			return dungeonDropItemDefinition{}, fmt.Errorf("parse drop item=%d path=%s damage font: %w", itemID, docPath, err)
		}
	}
	c.itemCache[itemID] = definition
	return definition, nil
}

func parseCurrentDamageFontDefinition(document *dnfpvf.Document, definition *dungeonDropItemDefinition) error {
	if document == nil || definition == nil {
		return nil
	}
	actionType, found := document.Text("action type")
	if !found {
		return nil
	}
	definition.ActionType = strings.TrimSpace(actionType)
	if !strings.EqualFold(definition.ActionType, "[add damage font skin]") {
		return nil
	}
	fontIndex, found := document.Int("index")
	if !found || fontIndex <= 0 || fontIndex > math.MaxUint16 {
		return fmt.Errorf("font index %d invalid", fontIndex)
	}
	definition.DamageFontIndex = uint16(fontIndex)

	texts := document.Texts("expiration info")
	ints := document.Ints("expiration info")
	for index, token := range texts {
		switch {
		case strings.EqualFold(strings.TrimSpace(token), "[period]"):
			if len(ints) == 0 || ints[0] <= 0 {
				return fmt.Errorf("period expiration missing positive days")
			}
			definition.DamageFontExpirationMode = alignedcmd.DamageFontExpirationPeriod
			definition.DamageFontPeriodDays = ints[0]
			return nil
		case strings.EqualFold(strings.TrimSpace(token), "[date]"):
			if index+1 >= len(texts) {
				return fmt.Errorf("fixed expiration missing date")
			}
			expiresAt, err := time.ParseInLocation(currentPVFExpirationDateLayout, strings.TrimSpace(texts[index+1]), currentPVFExpirationDateLocation)
			if err != nil {
				return fmt.Errorf("fixed expiration %q: %w", texts[index+1], err)
			}
			if expiresAt.Unix() <= 0 || expiresAt.Unix() > math.MaxUint32 {
				return fmt.Errorf("fixed expiration unix %d exceeds current u32 wire", expiresAt.Unix())
			}
			definition.DamageFontExpirationMode = alignedcmd.DamageFontExpirationFixed
			definition.DamageFontFixedExpiration = expiresAt.UTC()
			return nil
		case strings.EqualFold(strings.TrimSpace(token), "[unlimit]"):
			definition.DamageFontExpirationMode = alignedcmd.DamageFontExpirationUnlimited
			return nil
		}
	}
	return fmt.Errorf("expiration info is missing a supported period/date/unlimit mode")
}

func parseCurrentPVFExpirationDate(document *dnfpvf.Document) (time.Time, error) {
	if document == nil {
		return time.Time{}, nil
	}
	raw, found := document.Text("expiration date")
	raw = strings.TrimSpace(raw)
	if !found || raw == "" {
		return time.Time{}, nil
	}
	expirationDate, err := time.ParseInLocation(currentPVFExpirationDateLayout, raw, currentPVFExpirationDateLocation)
	if err != nil {
		return time.Time{}, fmt.Errorf("value=%q layout=%q: %w", raw, currentPVFExpirationDateLayout, err)
	}
	unix := expirationDate.Unix()
	if unix <= 0 || unix > math.MaxUint32 {
		return time.Time{}, fmt.Errorf("value=%q unix=%d exceeds current u32 item wire", raw, unix)
	}
	return expirationDate.UTC(), nil
}

// SelectGenericStackable applies the C# MonsterDropConfig candidate rule to
// the active PVF: candidates are uniform within (grade, rarity), then fall
// back to rarity zero exactly as the reference implementation does.  It does
// not create a scene object or write an inventory row.
func (c *pvfDungeonDropCatalog) SelectGenericStackable(
	rng *currentDungeonDropLCG,
	monsterLevel int,
	gradeDown int64,
	gradeUp int64,
	rarity int64,
) (uint32, bool, error) {
	if rng == nil {
		return 0, false, errDungeonDropSourceRequired
	}
	if err := c.ensureGenericPools(); err != nil {
		return 0, false, err
	}
	candidates := c.genericStackableCandidates(monsterLevel, gradeDown, gradeUp, rarity)
	if len(candidates) == 0 && rarity > 0 {
		candidates = c.genericStackableCandidates(monsterLevel, gradeDown, gradeUp, 0)
	}
	if len(candidates) == 0 {
		return 0, false, nil
	}
	return candidates[rng.Next(len(candidates))], true, nil
}

// SelectGenericEquipment is the weighted counterpart of
// SelectGenericStackable.  ItemDictionary's generation rate is a domain
// weight only; it is never serialized into the current EXE item wire.
func (c *pvfDungeonDropCatalog) SelectGenericEquipment(
	rng *currentDungeonDropLCG,
	monsterLevel int,
	gradeDown int64,
	gradeUp int64,
	rarity int64,
) (uint32, bool, error) {
	if rng == nil {
		return 0, false, errDungeonDropSourceRequired
	}
	if err := c.ensureGenericPools(); err != nil {
		return 0, false, err
	}
	candidates := c.genericEquipmentCandidates(monsterLevel, gradeDown, gradeUp, rarity)
	if len(candidates) == 0 && rarity > 0 {
		candidates = c.genericEquipmentCandidates(monsterLevel, gradeDown, gradeUp, 0)
	}
	if len(candidates) == 0 {
		return 0, false, nil
	}
	var total uint64
	for _, candidate := range candidates {
		if candidate.ItemID == 0 || candidate.Weight == 0 || math.MaxUint64-total < uint64(candidate.Weight) {
			continue
		}
		total += uint64(candidate.Weight)
	}
	if total == 0 {
		return 0, false, nil
	}
	want := uint64(rng.NextUint32()) % total
	var cumulative uint64
	for _, candidate := range candidates {
		if candidate.ItemID == 0 || candidate.Weight == 0 || math.MaxUint64-cumulative < uint64(candidate.Weight) {
			continue
		}
		cumulative += uint64(candidate.Weight)
		if want < cumulative {
			return candidate.ItemID, true, nil
		}
	}
	return 0, false, nil
}

func (c *pvfDungeonDropCatalog) genericStackableCandidates(
	monsterLevel int,
	gradeDown int64,
	gradeUp int64,
	rarity int64,
) []uint32 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.collectGenericStackablesLocked(monsterLevel, gradeDown, gradeUp, rarity)
}

func (c *pvfDungeonDropCatalog) genericEquipmentCandidates(
	monsterLevel int,
	gradeDown int64,
	gradeUp int64,
	rarity int64,
) []dungeonDropWeightedItem {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.collectGenericEquipmentLocked(monsterLevel, gradeDown, gradeUp, rarity)
}

func (c *pvfDungeonDropCatalog) collectGenericStackablesLocked(
	monsterLevel int,
	gradeDown int64,
	gradeUp int64,
	rarity int64,
) []uint32 {
	if c == nil || gradeDown < 0 || gradeUp <= 0 {
		return nil
	}
	result := make([]uint32, 0)
	for offset := -gradeDown; offset < gradeUp; offset++ {
		grade := int64(monsterLevel) + offset
		if grade < 1 || grade > 200 {
			continue
		}
		result = append(result, c.genericStackables[dungeonDropGradeRarityKey{Grade: grade, Rarity: rarity}]...)
	}
	return result
}

func (c *pvfDungeonDropCatalog) collectGenericEquipmentLocked(
	monsterLevel int,
	gradeDown int64,
	gradeUp int64,
	rarity int64,
) []dungeonDropWeightedItem {
	if c == nil || gradeDown < 0 || gradeUp <= 0 {
		return nil
	}
	result := make([]dungeonDropWeightedItem, 0)
	for offset := -gradeDown; offset < gradeUp; offset++ {
		grade := int64(monsterLevel) + offset
		if grade < 1 || grade > 200 {
			continue
		}
		result = append(result, c.genericEquipment[dungeonDropGradeRarityKey{Grade: grade, Rarity: rarity}]...)
	}
	return result
}

func (c *pvfDungeonDropCatalog) ensureGenericPools() error {
	if c == nil || c.source == nil {
		return errDungeonDropSourceRequired
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.genericLoaded {
		return c.genericErr
	}
	c.genericLoaded = true
	c.genericStackables = make(map[dungeonDropGradeRarityKey][]uint32)
	c.genericEquipment = make(map[dungeonDropGradeRarityKey][]dungeonDropWeightedItem)
	if err := c.loadGenericStackablesLocked(); err != nil {
		c.genericErr = err
		return err
	}
	if err := c.loadGenericEquipmentLocked(); err != nil {
		c.genericErr = err
		return err
	}
	return nil
}

func (c *pvfDungeonDropCatalog) loadGenericStackablesLocked() error {
	for _, itemID := range c.stackableIDs {
		reference, found := c.itemRefs[itemID]
		if !found || reference.kind != dungeonDropItemStackable || dungeonDropExcludedStackablePath(reference.path) {
			continue
		}
		docPath, text, err := readDungeonDropText(c.source, string(dungeonDropItemStackable), reference.path)
		if err != nil {
			// The C# loader skips one unreadable stackable rather than replacing the
			// whole table with a synthetic fallback.
			continue
		}
		document, err := dnfpvf.Parse(docPath, text)
		if err != nil {
			continue
		}
		creationRate, found := document.Int("creation rate")
		if !found || creationRate <= 0 {
			continue
		}
		grade, found := document.Int("grade")
		if !found || grade <= 0 || grade > 200 {
			continue
		}
		rarity, found := document.Int("rarity")
		if !found {
			rarity = 0
		}
		if rarity < 0 || rarity > 6 {
			continue
		}
		key := dungeonDropGradeRarityKey{Grade: grade, Rarity: rarity}
		c.genericStackables[key] = append(c.genericStackables[key], itemID)
	}
	return nil
}

func (c *pvfDungeonDropCatalog) loadGenericEquipmentLocked() error {
	text, err := c.source.ReadText(dungeonDropEquipmentDictionaryPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", dungeonDropEquipmentDictionaryPath, err)
	}
	document, err := dnfpvf.Parse(dungeonDropEquipmentDictionaryPath, text)
	if err != nil {
		return fmt.Errorf("parse %s: %w", dungeonDropEquipmentDictionaryPath, err)
	}
	numbers := make([]int64, 0, 16)
	flush := func() {
		if len(numbers) < 6 {
			numbers = numbers[:0]
			return
		}
		itemID, rarity, grade := numbers[0], numbers[1], numbers[2]
		equipmentCategory, generationRate := numbers[4], numbers[5]
		numbers = numbers[:0]
		if itemID <= 0 || itemID > math.MaxUint32 || rarity < 0 || rarity > 5 || grade <= 0 || grade > 200 || generationRate <= 0 || generationRate > math.MaxUint32 {
			return
		}
		if equipmentCategory/1000 < 10 || equipmentCategory/1000 > 12 {
			return
		}
		reference, found := c.itemRefs[uint32(itemID)]
		if !found || reference.kind != dungeonDropItemEquipment {
			return
		}
		key := dungeonDropGradeRarityKey{Grade: grade, Rarity: rarity}
		c.genericEquipment[key] = append(c.genericEquipment[key], dungeonDropWeightedItem{
			ItemID: uint32(itemID), Weight: uint32(generationRate),
		})
	}
	for _, token := range document.Tokens {
		switch token.Kind {
		case dnfpvf.TokenInt:
			numbers = append(numbers, token.Int)
		case dnfpvf.TokenString:
			flush()
		}
	}
	// ItemDictionary rows are name-terminated.  A missing final name is not a
	// valid row and intentionally remains ignored, matching the C# parser.
	return nil
}

func dungeonDropExcludedStackablePath(value string) bool {
	value = strings.ToLower(cleanDungeonDropPath(value))
	for _, prefix := range []string{
		"cash/", "quest/", "recipe/", "temp/", "event/", "emblem/", "monstercard/",
	} {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

// dungeonDropStackableSlotRange follows the repository's established main
// inventory category ranges and uses the real PVF [stackable type] value.
// It is a domain placement rule, not a claim about the current pickup wire.
func dungeonDropStackableSlotRange(stackableType string) (int16, int16) {
	normalized := normalizeDungeonDropStackableType(stackableType)
	switch {
	case strings.HasPrefix(normalized, "[material]") && strings.HasSuffix(normalized, "4"):
		return 345, 359
	case strings.HasPrefix(normalized, "[material]"):
		return 121, 176
	case strings.HasPrefix(normalized, "[quest]"):
		return 177, 232
	case strings.HasPrefix(normalized, "[material expert job]"):
		return 233, 288
	case strings.HasPrefix(normalized, "[avatar emblem]"):
		return 289, 344
	default:
		return 65, 120
	}
}

func dungeonDropStackablePrefersItemQuickSlots(stackableType string) bool {
	normalized := normalizeDungeonDropStackableType(stackableType)
	return strings.HasPrefix(normalized, "[waste]") ||
		strings.HasPrefix(normalized, "[material]")
}

func normalizeDungeonDropStackableType(stackableType string) string {
	return strings.ToLower(strings.TrimSpace(strings.ReplaceAll(stackableType, "`", "")))
}

// DropCatalog lazily derives the item/drop catalog from the same authoritative
// PVF source as the monster catalog. Synthetic tests that do not provide item
// lists therefore keep the existing zero-drop death path until a drop is
// actually requested.
func (c *pvfDungeonMonsterCatalog) DropCatalog() (*pvfDungeonDropCatalog, error) {
	if c == nil || c.source == nil {
		return nil, errDungeonDropSourceRequired
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.dropCatalogLoaded {
		c.dropCatalog, c.dropCatalogErr = newPVFDungeonDropCatalog(c.source)
		c.dropCatalogLoaded = true
	}
	return c.dropCatalog, c.dropCatalogErr
}

func parseDungeonMonsterDropPool(document *dnfpvf.Document) []dungeonMonsterDropPoolEntry {
	tokens, ok := document.Section("item")
	if !ok || len(tokens) == 0 {
		return nil
	}
	values := make([]int64, 0, len(tokens))
	for _, token := range tokens {
		if token.Kind == dnfpvf.TokenInt {
			values = append(values, token.Int)
		}
	}
	out := make([]dungeonMonsterDropPoolEntry, 0, len(values)/2)
	for index := 0; index+1 < len(values); index += 2 {
		itemID, weight := values[index], values[index+1]
		if itemID <= 0 || itemID > math.MaxUint32 || weight <= 0 || weight > math.MaxUint32 {
			continue
		}
		out = append(out, dungeonMonsterDropPoolEntry{ItemID: uint32(itemID), Weight: uint32(weight)})
	}
	return out
}

func selectDungeonMonsterDrop(pool []dungeonMonsterDropPoolEntry, roll uint64) (dungeonMonsterDropPoolEntry, bool) {
	var total uint64
	for _, entry := range pool {
		if entry.ItemID == 0 || entry.Weight == 0 || math.MaxUint64-total < uint64(entry.Weight) {
			continue
		}
		total += uint64(entry.Weight)
	}
	if total == 0 {
		return dungeonMonsterDropPoolEntry{}, false
	}
	want := roll % total
	var cumulative uint64
	for _, entry := range pool {
		if entry.ItemID == 0 || entry.Weight == 0 || math.MaxUint64-cumulative < uint64(entry.Weight) {
			continue
		}
		cumulative += uint64(entry.Weight)
		if want < cumulative {
			return entry, true
		}
	}
	return dungeonMonsterDropPoolEntry{}, false
}

func readDungeonDropText(source dnfpvf.Source, root, listedPath string) (string, string, error) {
	clean := cleanDungeonDropPath(listedPath)
	candidates := []string{clean}
	root = cleanDungeonDropPath(root)
	if root != "" && !strings.HasPrefix(strings.ToLower(clean), strings.ToLower(root)+"/") {
		candidates = append(candidates, path.Join(root, clean))
	}
	// The 90 PVF marks revised files with a "(r)" basename prefix while list
	// indexes keep naming the original file; fall back to the revised name
	// only when the original is absent.
	for _, candidate := range append([]string(nil), candidates...) {
		dir, base := path.Split(candidate)
		if base == "" || strings.HasPrefix(strings.ToLower(base), "(r)") {
			continue
		}
		candidates = append(candidates, dir+"(r)"+base)
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

func cleanDungeonDropPath(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	value = strings.TrimPrefix(value, "./")
	return strings.TrimPrefix(value, "/")
}

func cloneDungeonMonsterDropPool(values []dungeonMonsterDropPoolEntry) []dungeonMonsterDropPoolEntry {
	return append([]dungeonMonsterDropPoolEntry(nil), values...)
}
