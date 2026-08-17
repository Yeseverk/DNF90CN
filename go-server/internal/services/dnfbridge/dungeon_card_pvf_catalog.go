package dnfbridge

import (
	"errors"
	"fmt"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

const (
	dungeonCardGoldReferencePath = "Etc/ItemDropInfo_Common.etc"
	dungeonCardDropRulePath      = "Etc/ItemDropInfo_Monseter.etc"
)

var (
	errDungeonCardPVFSourceRequired = errors.New("dungeon card PVF source is required")
	errDungeonCardPVFSectionMissing = errors.New("dungeon card PVF section is missing")
	errDungeonCardPVFSectionShape   = errors.New("dungeon card PVF section shape is invalid")
)

// dungeonCardPVFGoldReference preserves one exact three-column row from the
// real [gold drop ref table]. All three values remain opaque until the current
// reward owner is traced; the first is only used as the table lookup key.
type dungeonCardPVFGoldReference struct {
	LookupKey int64
	ValueA    int64
	ValueB    int64
}

// dungeonCardPVFDropProbabilityRow preserves all five rate columns. They stay
// opaque because the active clear-card generator uses the current
// ServerS4A12 rarity thresholds and the PVF item-drop grade window instead.
type dungeonCardPVFDropProbabilityRow struct {
	KeyMin     int64
	KeyMax     int64
	RateValues [5]int64
}

// dungeonCardPVFItemDropReference preserves one exact three-column row from
// [item drop ref table]. The first value is only used as its lookup key.
type dungeonCardPVFItemDropReference struct {
	LookupKey int64
	ValueA    int64
	ValueB    int64
}

type dungeonCardPVFCatalogSnapshot struct {
	GoldReferences      int
	DropProbabilityRows int
	ItemDropReferences  int
}

// pvfDungeonCardRewardCatalog is the read-only typed view of the clear-card
// source rows. Runtime selection and durable asset mutation remain separate
// owners.
type pvfDungeonCardRewardCatalog struct {
	goldByKey       map[int64]dungeonCardPVFGoldReference
	dropProbability []dungeonCardPVFDropProbabilityRow
	itemDropByKey   map[int64]dungeonCardPVFItemDropReference
	items           *pvfDungeonDropCatalog
}

func newPVFDungeonCardRewardCatalog(source dnfpvf.Source) (*pvfDungeonCardRewardCatalog, error) {
	if source == nil {
		return nil, errDungeonCardPVFSourceRequired
	}
	goldDocument, err := parseDungeonCardPVFDocument(source, dungeonCardGoldReferencePath)
	if err != nil {
		return nil, err
	}
	dropDocument, err := parseDungeonCardPVFDocument(source, dungeonCardDropRulePath)
	if err != nil {
		return nil, err
	}
	goldByKey, err := parseDungeonCardPVFGoldReferences(goldDocument)
	if err != nil {
		return nil, err
	}
	dropProbability, err := parseDungeonCardPVFDropProbability(dropDocument)
	if err != nil {
		return nil, err
	}
	itemDropByKey, err := parseDungeonCardPVFItemDropReferences(dropDocument)
	if err != nil {
		return nil, err
	}
	items, err := newPVFDungeonDropCatalog(source)
	if err != nil {
		return nil, fmt.Errorf("load dungeon card item resolver: %w", err)
	}
	return &pvfDungeonCardRewardCatalog{
		goldByKey:       goldByKey,
		dropProbability: dropProbability,
		itemDropByKey:   itemDropByKey,
		items:           items,
	}, nil
}

func parseDungeonCardPVFDocument(source dnfpvf.Source, documentPath string) (*dnfpvf.Document, error) {
	text, err := source.ReadText(documentPath)
	if err != nil {
		return nil, fmt.Errorf("read dungeon card PVF %s: %w", documentPath, err)
	}
	document, err := dnfpvf.Parse(documentPath, text)
	if err != nil {
		return nil, fmt.Errorf("parse dungeon card PVF %s: %w", documentPath, err)
	}
	return document, nil
}

func parseDungeonCardPVFGoldReferences(document *dnfpvf.Document) (map[int64]dungeonCardPVFGoldReference, error) {
	values, err := dungeonCardPVFSectionInts(document, "gold drop ref table")
	if err != nil {
		return nil, err
	}
	if len(values)%3 != 0 {
		return nil, fmt.Errorf("%w: section=gold drop ref table values=%d row_width=3", errDungeonCardPVFSectionShape, len(values))
	}
	out := make(map[int64]dungeonCardPVFGoldReference, len(values)/3)
	for offset := 0; offset < len(values); offset += 3 {
		row := dungeonCardPVFGoldReference{LookupKey: values[offset], ValueA: values[offset+1], ValueB: values[offset+2]}
		if _, exists := out[row.LookupKey]; exists {
			return nil, fmt.Errorf("%w: section=gold drop ref table duplicate_key=%d", errDungeonCardPVFSectionShape, row.LookupKey)
		}
		out[row.LookupKey] = row
	}
	return out, nil
}

func parseDungeonCardPVFDropProbability(document *dnfpvf.Document) ([]dungeonCardPVFDropProbabilityRow, error) {
	values, err := dungeonCardPVFSectionInts(document, "drop prob")
	if err != nil {
		return nil, err
	}
	if len(values)%7 != 0 {
		return nil, fmt.Errorf("%w: section=drop prob values=%d row_width=7", errDungeonCardPVFSectionShape, len(values))
	}
	count, found := document.Int("drop prob count")
	if !found {
		return nil, fmt.Errorf("%w: section=drop prob count", errDungeonCardPVFSectionMissing)
	}
	if count < 0 || int64(len(values)/7) != count {
		return nil, fmt.Errorf("%w: section=drop prob declared=%d actual=%d", errDungeonCardPVFSectionShape, count, len(values)/7)
	}
	out := make([]dungeonCardPVFDropProbabilityRow, 0, len(values)/7)
	for offset := 0; offset < len(values); offset += 7 {
		row := dungeonCardPVFDropProbabilityRow{
			KeyMin: values[offset],
			KeyMax: values[offset+1],
			RateValues: [5]int64{
				values[offset+2], values[offset+3], values[offset+4], values[offset+5], values[offset+6],
			},
		}
		if row.KeyMax < row.KeyMin {
			return nil, fmt.Errorf("%w: section=drop prob row=%+v", errDungeonCardPVFSectionShape, row)
		}
		out = append(out, row)
	}
	return out, nil
}

func parseDungeonCardPVFItemDropReferences(document *dnfpvf.Document) (map[int64]dungeonCardPVFItemDropReference, error) {
	values, err := dungeonCardPVFSectionInts(document, "item drop ref table")
	if err != nil {
		return nil, err
	}
	if len(values)%3 != 0 {
		return nil, fmt.Errorf("%w: section=item drop ref table values=%d row_width=3", errDungeonCardPVFSectionShape, len(values))
	}
	out := make(map[int64]dungeonCardPVFItemDropReference, len(values)/3)
	for offset := 0; offset < len(values); offset += 3 {
		row := dungeonCardPVFItemDropReference{LookupKey: values[offset], ValueA: values[offset+1], ValueB: values[offset+2]}
		if _, exists := out[row.LookupKey]; exists {
			return nil, fmt.Errorf("%w: section=item drop ref table duplicate_key=%d", errDungeonCardPVFSectionShape, row.LookupKey)
		}
		out[row.LookupKey] = row
	}
	return out, nil
}

func dungeonCardPVFSectionInts(document *dnfpvf.Document, section string) ([]int64, error) {
	if document == nil {
		return nil, fmt.Errorf("%w: section=%s", errDungeonCardPVFSectionMissing, section)
	}
	tokens, found := document.Section(section)
	if !found {
		return nil, fmt.Errorf("%w: section=%s", errDungeonCardPVFSectionMissing, section)
	}
	values := make([]int64, 0, len(tokens))
	for _, token := range tokens {
		if token.Kind == dnfpvf.TokenInt {
			values = append(values, token.Int)
		}
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("%w: section=%s empty", errDungeonCardPVFSectionShape, section)
	}
	return values, nil
}

func (c *pvfDungeonCardRewardCatalog) GoldReference(key int64) (dungeonCardPVFGoldReference, bool) {
	if c == nil {
		return dungeonCardPVFGoldReference{}, false
	}
	row, found := c.goldByKey[key]
	return row, found
}

func (c *pvfDungeonCardRewardCatalog) DropProbability(key int64) (dungeonCardPVFDropProbabilityRow, bool) {
	if c == nil {
		return dungeonCardPVFDropProbabilityRow{}, false
	}
	for _, row := range c.dropProbability {
		if key >= row.KeyMin && key <= row.KeyMax {
			return row, true
		}
	}
	return dungeonCardPVFDropProbabilityRow{}, false
}

func (c *pvfDungeonCardRewardCatalog) ItemDropReference(key int64) (dungeonCardPVFItemDropReference, bool) {
	if c == nil {
		return dungeonCardPVFItemDropReference{}, false
	}
	row, found := c.itemDropByKey[key]
	return row, found
}

func (c *pvfDungeonCardRewardCatalog) ResolveItem(itemID uint32) (dungeonDropItemDefinition, error) {
	if c == nil || c.items == nil {
		return dungeonDropItemDefinition{}, errDungeonCardPVFSourceRequired
	}
	return c.items.ResolveItem(itemID)
}

func (c *pvfDungeonCardRewardCatalog) Snapshot() dungeonCardPVFCatalogSnapshot {
	if c == nil {
		return dungeonCardPVFCatalogSnapshot{}
	}
	return dungeonCardPVFCatalogSnapshot{
		GoldReferences:      len(c.goldByKey),
		DropProbabilityRows: len(c.dropProbability),
		ItemDropReferences:  len(c.itemDropByKey),
	}
}
