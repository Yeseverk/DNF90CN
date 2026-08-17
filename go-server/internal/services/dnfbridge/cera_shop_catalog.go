package dnfbridge

import (
	"fmt"
	"math"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

var currentCeraShopAvatarCouponItemIDs = map[int]map[uint32]int64{
	1: {1: 2681588, 2: 2681589, 3: 2681590},
	2: {1: 2681591, 2: 2681592, 3: 2681593},
	3: {3: 2681594},
}

// currentCeraShopProduct is a real runtime etc/cerashop.etc row.  The item
// ID, item count, and Cera price are never supplied by the client request.
type currentCeraShopProduct struct {
	CommodityID uint32
	ItemID      uint32
	Count       uint32
	CeraPrice   uint32
	Section     string
	// DurationDays is the runtime Cera-shop row's relative lifetime. The
	// active `visual` grammar stores it at token four (for example 30 in a
	// `(30天)` name-decoration product); it is independent of item-PVF
	// [usable period] and must be materialized once at checkout.
	DurationDays           uint32
	AvatarDurationIndex    uint32
	AvatarDurationDays     uint32
	AvatarPriceFromItemPVF bool
	// PremiumPackageDays is the [charac premium package] row's day count
	// (token 7). Zero for every non-package section.
	PremiumPackageDays uint32
}

// pvfCeraShopCatalog owns only static runtime PVF state.  Inventory placement
// and Cera deduction are handled within the character settlement transaction.
type pvfCeraShopCatalog struct {
	products         map[uint32]currentCeraShopProduct
	bonusByCommodity map[uint32][]currentCeraShopBonusItem
	items            *pvfDungeonDropCatalog
	source           dnfpvf.Source
}

func newPVFCeraShopCatalog(source dnfpvf.Source) (*pvfCeraShopCatalog, error) {
	if source == nil {
		return nil, errCurrentCeraShopCatalogUnavailable
	}
	document, err := parseDungeonCardPVFDocument(source, "etc/cerashop.etc")
	if err != nil {
		return nil, fmt.Errorf("read cera shop catalog: %w", err)
	}
	items, err := newPVFDungeonDropCatalog(source)
	if err != nil {
		return nil, fmt.Errorf("load cera shop item resolver: %w", err)
	}
	products := make(map[uint32]currentCeraShopProduct)
	// The normal purchase categories share a 9-token row: commodity, item,
	// amount, gold, point, Cera, name, 0, 0.  The catalog deliberately owns
	// every runtime category whose row grammar is known; item placement is
	// still decided later from the real item PVF type, not the client request.
	for _, section := range []string{"item", "premium", "creature", "coin", "material", "recoveryitem"} {
		if err := parseCurrentCeraShopProductSection(document, section, 9, 5, true, products); err != nil {
			return nil, err
		}
	}
	// These category names occur in different runtime PVFs.  They have the
	// ordinary nine-token product grammar and are optional by PVF version.
	for _, section := range []string{"community item", "communityitem", "regular item", "regularitem"} {
		if err := parseCurrentCeraShopProductSection(document, section, 9, 5, false, products); err != nil {
			return nil, err
		}
	}
	// visual rows are eight tokens (the fourth token is duration metadata),
	// while the three package catalogs use the Cera price at token four.
	if err := parseCurrentCeraShopProductSection(document, "visual", 8, 5, false, products); err != nil {
		return nil, err
	}
	for _, section := range []string{"package", "regular package", "regularpackage", "community package", "communitypackage"} {
		if err := parseCurrentCeraShopProductSection(document, section, 11, 4, false, products); err != nil {
			return nil, err
		}
	}
	// Contract products are catalogued too, including their distinct row
	// grammars.  They are never treated as ordinary bag items at settlement:
	// checkout activates their account-level premium contract state instead.
	if err := parseCurrentCeraShopSelectableCharacterPremiumSection(document, products); err != nil {
		return nil, err
	}
	if err := parseCurrentCeraShopCharacterPremiumPackageSection(document, products); err != nil {
		return nil, err
	}
	// Avatar rows are six tokens.  A negative Cera field means the price and
	// duration are selected from the purchased equipment's [avatar type
	// select] row; defer that real-PVF lookup to the actual purchase instead
	// of preloading every avatar definition at login.
	if err := parseCurrentCeraShopAvatarSection(document, products); err != nil {
		return nil, err
	}
	// One-plus-one IPG gifts (etc/chn_cerashop_multibonus.etc) ride along
	// with the catalog so checkout can grant them in the same transaction.
	bonusByCommodity, err := parseCurrentCeraShopMultiBonus(source)
	if err != nil {
		return nil, err
	}
	return &pvfCeraShopCatalog{products: products, bonusByCommodity: bonusByCommodity, items: items, source: source}, nil
}

func parseCurrentCeraShopProductSection(
	document *dnfpvf.Document,
	section string,
	rowWidth int,
	ceraPriceOffset int,
	required bool,
	products map[uint32]currentCeraShopProduct,
) error {
	if document == nil || products == nil || rowWidth < 3 || ceraPriceOffset < 0 || ceraPriceOffset >= rowWidth {
		return errCurrentCeraShopCatalogUnavailable
	}
	tokens, found := document.Section(section)
	if !found {
		if required {
			return fmt.Errorf("%w: missing section=%s", errCurrentCeraShopCatalogUnavailable, section)
		}
		return nil
	}
	values := currentCeraShopPVFValues(tokens)
	if len(values)%rowWidth != 0 {
		return fmt.Errorf("%w: section=%s tokens=%d row_width=%d", errCurrentCeraShopCatalogUnavailable, section, len(values), rowWidth)
	}
	for offset := 0; offset < len(values); offset += rowWidth {
		commodityID, ok := currentCeraShopPVFUint32(values[offset])
		if !ok || commodityID == 0 {
			return fmt.Errorf("%w: section=%s row=%d invalid_commodity", errCurrentCeraShopCatalogUnavailable, section, offset/rowWidth)
		}
		itemID, ok := currentCeraShopPVFUint32(values[offset+1])
		if !ok || itemID == 0 {
			return fmt.Errorf("%w: section=%s commodity=%d invalid_item", errCurrentCeraShopCatalogUnavailable, section, commodityID)
		}
		count, ok := currentCeraShopPVFUint32(values[offset+2])
		// Some visual/package rows use a negative third token as a client
		// display sentinel.  It is never a requested quantity, so normalize it
		// to one real delivery item just as zero-count package rows are.
		if !ok {
			count = 1
		}
		if count == 0 {
			count = 1
		}
		ceraPrice, ok := currentCeraShopPVFUint32(values[offset+ceraPriceOffset])
		if !ok {
			return fmt.Errorf("%w: section=%s commodity=%d invalid_cera_price", errCurrentCeraShopCatalogUnavailable, section, commodityID)
		}
		product := currentCeraShopProduct{
			CommodityID: commodityID,
			ItemID:      itemID,
			Count:       count,
			CeraPrice:   ceraPrice,
			Section:     section,
		}
		if normalizeMagicBoxSectionName(section) == "visual" {
			durationDays, durationOK := currentCeraShopPVFUint32(values[offset+3])
			if !durationOK {
				return fmt.Errorf("%w: section=%s commodity=%d invalid_duration", errCurrentCeraShopCatalogUnavailable, section, commodityID)
			}
			product.DurationDays = durationDays
		}
		products[commodityID] = product
	}
	return nil
}

func parseCurrentCeraShopAvatarSection(document *dnfpvf.Document, products map[uint32]currentCeraShopProduct) error {
	if document == nil || products == nil {
		return errCurrentCeraShopCatalogUnavailable
	}
	tokens, found := document.Section("avatar")
	if !found {
		return nil
	}
	values := currentCeraShopPVFValues(tokens)
	const rowWidth = 6
	if len(values)%rowWidth != 0 {
		return fmt.Errorf("%w: section=avatar tokens=%d row_width=%d", errCurrentCeraShopCatalogUnavailable, len(values), rowWidth)
	}
	for offset := 0; offset < len(values); offset += rowWidth {
		commodityID, ok := currentCeraShopPVFUint32(values[offset])
		if !ok || commodityID == 0 {
			return fmt.Errorf("%w: section=avatar row=%d invalid_commodity", errCurrentCeraShopCatalogUnavailable, offset/rowWidth)
		}
		itemID, ok := currentCeraShopPVFUint32(values[offset+1])
		if !ok || itemID == 0 {
			return fmt.Errorf("%w: section=avatar commodity=%d invalid_item", errCurrentCeraShopCatalogUnavailable, commodityID)
		}
		durationIndex, ok := currentCeraShopPVFUint32(values[offset+2])
		if !ok || durationIndex == 0 {
			durationIndex = 1
		}
		product := currentCeraShopProduct{
			CommodityID:         commodityID,
			ItemID:              itemID,
			Count:               1,
			Section:             "avatar",
			AvatarDurationIndex: durationIndex,
		}
		if ceraPrice, ok := currentCeraShopPVFUint32(values[offset+5]); ok {
			product.CeraPrice = ceraPrice
		} else {
			product.AvatarPriceFromItemPVF = true
		}
		products[commodityID] = product
	}
	return nil
}

// [charac premium package] does not use the normal package row: its third
// value is Cera, followed by its localized duration label and activation
// metadata (token 7 = days). Buying it is consumed at checkout and activates
// every 魔王契约 devil slot for that duration; no container item is granted.
func parseCurrentCeraShopCharacterPremiumPackageSection(document *dnfpvf.Document, products map[uint32]currentCeraShopProduct) error {
	if document == nil || products == nil {
		return errCurrentCeraShopCatalogUnavailable
	}
	tokens, found := document.Section("charac premium package")
	if !found {
		return nil
	}
	values := currentCeraShopPVFValues(tokens)
	const rowWidth = 9
	if len(values)%rowWidth != 0 {
		return fmt.Errorf("%w: section=charac premium package tokens=%d row_width=%d", errCurrentCeraShopCatalogUnavailable, len(values), rowWidth)
	}
	for offset := 0; offset < len(values); offset += rowWidth {
		commodityID, ok := currentCeraShopPVFUint32(values[offset])
		if !ok || commodityID == 0 {
			return fmt.Errorf("%w: section=charac premium package row=%d invalid_commodity", errCurrentCeraShopCatalogUnavailable, offset/rowWidth)
		}
		itemID, ok := currentCeraShopPVFUint32(values[offset+1])
		if !ok || itemID == 0 {
			return fmt.Errorf("%w: section=charac premium package commodity=%d invalid_item", errCurrentCeraShopCatalogUnavailable, commodityID)
		}
		ceraPrice, ok := currentCeraShopPVFUint32(values[offset+2])
		if !ok {
			return fmt.Errorf("%w: section=charac premium package commodity=%d invalid_cera_price", errCurrentCeraShopCatalogUnavailable, commodityID)
		}
		// Token 7 is the contract day count (the label at token 3 repeats it,
		// e.g. `60天` <-> 60). A package without a positive day count is not
		// purchasable.
		packageDays, _ := currentCeraShopPVFUint32(values[offset+7])
		products[commodityID] = currentCeraShopProduct{
			CommodityID:        commodityID,
			ItemID:             itemID,
			Count:              1,
			CeraPrice:          ceraPrice,
			Section:            "charac premium package",
			PremiumPackageDays: packageDays,
		}
	}
	return nil
}

// [selectable character premium] is nominally a nine-token product section,
// but the first row in the runtime PVF is a -1 default selector rather than a
// purchasable commodity.  Preserve the structural check and ignore only such
// explicitly non-purchasable rows.
func parseCurrentCeraShopSelectableCharacterPremiumSection(document *dnfpvf.Document, products map[uint32]currentCeraShopProduct) error {
	if document == nil || products == nil {
		return errCurrentCeraShopCatalogUnavailable
	}
	tokens, found := document.Section("selectable character premium")
	if !found {
		return nil
	}
	values := currentCeraShopPVFValues(tokens)
	const rowWidth = 9
	if len(values)%rowWidth != 0 {
		return fmt.Errorf("%w: section=selectable character premium tokens=%d row_width=%d", errCurrentCeraShopCatalogUnavailable, len(values), rowWidth)
	}
	for offset := 0; offset < len(values); offset += rowWidth {
		commodityID, commodityOK := currentCeraShopPVFUint32(values[offset])
		itemID, itemOK := currentCeraShopPVFUint32(values[offset+1])
		if !commodityOK || commodityID == 0 || !itemOK || itemID == 0 {
			continue
		}
		count, ok := currentCeraShopPVFUint32(values[offset+2])
		if !ok || count == 0 {
			count = 1
		}
		ceraPrice, ok := currentCeraShopPVFUint32(values[offset+5])
		if !ok {
			continue
		}
		products[commodityID] = currentCeraShopProduct{
			CommodityID: commodityID,
			ItemID:      itemID,
			Count:       count,
			CeraPrice:   ceraPrice,
			Section:     "selectable character premium",
		}
	}
	return nil
}

func currentCeraShopPVFValues(tokens []dnfpvf.Token) []dnfpvf.Token {
	values := make([]dnfpvf.Token, 0, len(tokens))
	for _, token := range tokens {
		switch token.Kind {
		case dnfpvf.TokenInt, dnfpvf.TokenString, dnfpvf.TokenIdent:
			values = append(values, token)
		}
	}
	return values
}

func currentCeraShopPVFUint32(token dnfpvf.Token) (uint32, bool) {
	if token.Kind != dnfpvf.TokenInt || token.Int < 0 || token.Int > math.MaxUint32 {
		return 0, false
	}
	return uint32(token.Int), true
}

func (c *pvfCeraShopCatalog) Product(commodityID uint32) (currentCeraShopProduct, bool) {
	if c == nil {
		return currentCeraShopProduct{}, false
	}
	product, found := c.products[commodityID]
	return product, found
}

func (c *pvfCeraShopCatalog) resolvePurchase(product currentCeraShopProduct) (currentCeraShopProduct, error) {
	if c == nil || c.items == nil || c.source == nil || product.ItemID == 0 {
		return currentCeraShopProduct{}, errCurrentCeraShopCatalogUnavailable
	}
	if product.Section != "avatar" {
		// Contract sections ([selectable character premium], [charac premium
		// package]) and premiumlist contract items now activate their real
		// account-level state at settlement; they pass through here and are
		// classified by commitCurrentCeraShopPurchase.
		return product, nil
	}
	definition, err := c.items.ResolveItem(product.ItemID)
	if err != nil {
		return currentCeraShopProduct{}, fmt.Errorf("%w: avatar item=%d: %v", errCurrentCeraShopProductUnavailable, product.ItemID, err)
	}
	if definition.Kind != dungeonDropItemEquipment {
		return currentCeraShopProduct{}, fmt.Errorf("%w: avatar item=%d is not equipment", errCurrentCeraShopProductUnavailable, product.ItemID)
	}
	document, err := parseDungeonCardPVFDocument(c.source, definition.PVFPath)
	if err != nil {
		return currentCeraShopProduct{}, fmt.Errorf("%w: avatar item=%d PVF=%s: %v", errCurrentCeraShopProductUnavailable, product.ItemID, definition.PVFPath, err)
	}
	equipmentType, found := document.Text("equipment type")
	if !found {
		return currentCeraShopProduct{}, fmt.Errorf("%w: avatar item=%d missing equipment type", errCurrentCeraShopProductUnavailable, product.ItemID)
	}
	rule, ok := currentEquipmentPlacementRuleForPVFType(equipmentType)
	if !ok || rule.class != currentEquipmentPlacementClassAvatar {
		return currentCeraShopProduct{}, fmt.Errorf("%w: item=%d type=%q is not avatar equipment", errCurrentCeraShopProductUnavailable, product.ItemID, equipmentType)
	}
	if durationDays, ceraPrice, found := currentCeraShopAvatarTypeOption(document, product.AvatarDurationIndex); found {
		product.AvatarPriceFromItemPVF = false
		product.CeraPrice = ceraPrice
		product.Count = 1
		product.AvatarDurationDays = durationDays
		return product, nil
	}
	if product.AvatarPriceFromItemPVF {
		return currentCeraShopProduct{}, fmt.Errorf("%w: avatar item=%d duration_index=%d has no runtime price", errCurrentCeraShopProductUnavailable, product.ItemID, product.AvatarDurationIndex)
	}
	return product, nil
}

func (c *pvfCeraShopCatalog) resolveAvatarCouponItemID(product currentCeraShopProduct) (int64, error) {
	if c == nil || c.items == nil || c.source == nil || product.ItemID == 0 {
		return 0, errCurrentCeraShopCatalogUnavailable
	}
	if product.Section != "avatar" {
		return 0, fmt.Errorf("%w: commodity=%d payment_mode=avatar_coupon section=%s", errCurrentCeraShopPaymentUnsupported, product.CommodityID, product.Section)
	}
	definition, err := c.items.ResolveItem(product.ItemID)
	if err != nil {
		return 0, fmt.Errorf("%w: avatar coupon item=%d: %v", errCurrentCeraShopProductUnavailable, product.ItemID, err)
	}
	if definition.Kind != dungeonDropItemEquipment {
		return 0, fmt.Errorf("%w: avatar coupon item=%d is not equipment", errCurrentCeraShopProductUnavailable, product.ItemID)
	}
	document, err := parseDungeonCardPVFDocument(c.source, definition.PVFPath)
	if err != nil {
		return 0, fmt.Errorf("%w: avatar coupon item=%d PVF=%s: %v", errCurrentCeraShopProductUnavailable, product.ItemID, definition.PVFPath, err)
	}
	grade, found := document.Int("grade")
	if !found || grade < 1 || grade > math.MaxInt32 {
		return 0, fmt.Errorf("%w: avatar item=%d missing supported grade", errCurrentCeraShopProductUnavailable, product.ItemID)
	}
	durationIndex := product.AvatarDurationIndex
	if durationIndex == 0 {
		durationIndex = product.Count
	}
	couponID, ok := currentCeraShopAvatarCouponItemIDs[int(grade)][durationIndex]
	if !ok {
		return 0, fmt.Errorf("%w: avatar item=%d grade=%d duration_index=%d has no coupon", errCurrentCeraShopPaymentUnsupported, product.ItemID, grade, durationIndex)
	}
	return couponID, nil
}

// currentCeraShopAvatarTypeOption reads the real equipment [avatar type
// select] grammar: duration, ignored, ignored, Cera, ignored, ignored,
// ignored.  The shop's third avatar field is a one-based duration selector.
func currentCeraShopAvatarTypeOption(document *dnfpvf.Document, index uint32) (uint32, uint32, bool) {
	if document == nil || index == 0 {
		return 0, 0, false
	}
	values := document.Ints("avatar type select")
	const rowWidth = 7
	offset := int(index-1) * rowWidth
	if offset < 0 || offset+3 >= len(values) || values[offset] < 0 || values[offset] > math.MaxUint32 || values[offset+3] < 0 || values[offset+3] > math.MaxUint32 {
		return 0, 0, false
	}
	return uint32(values[offset]), uint32(values[offset+3]), true
}

func (s *Service) currentCeraShopCatalog() (*pvfCeraShopCatalog, error) {
	if s == nil {
		return nil, errCurrentCeraShopCatalogUnavailable
	}
	s.ceraShopMu.Lock()
	defer s.ceraShopMu.Unlock()
	if s.ceraShopCatalog != nil {
		return s.ceraShopCatalog, nil
	}
	if s.ceraShopCatalogLoadErr != nil {
		return nil, s.ceraShopCatalogLoadErr
	}
	s.initialEquipmentMu.Lock()
	archive, err := s.initialEquipmentArchiveLocked()
	s.initialEquipmentMu.Unlock()
	if err == nil {
		s.ceraShopCatalog, err = newPVFCeraShopCatalog(archive)
	}
	if err != nil {
		s.ceraShopCatalogLoadErr = err
		return nil, err
	}
	s.logPacketEvent("dnf-cera-shop-pvf-catalog-loaded", "products", len(s.ceraShopCatalog.products))
	return s.ceraShopCatalog, nil
}
