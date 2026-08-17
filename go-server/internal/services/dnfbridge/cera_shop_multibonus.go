package dnfbridge

import (
	"fmt"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

// currentCeraShopBonusItem is one row inside a runtime
// etc/chn_cerashop_multibonus.etc [bonus item] block: buying the block's
// commodity once grants this item count on top of the purchased product
// (the "one plus one" IPG gifts, e.g. 充满爱慕的信 -> 电动小马达泰迪礼盒 +
// 幸运魔锤).
type currentCeraShopBonusItem struct {
	ItemID uint32
	Count  uint32
}

// parseCurrentCeraShopMultiBonus reads the optional runtime multi-bonus
// document.  A missing document means the PVF version has no one-plus-one
// gifts at all and yields an empty map; a present but malformed document is a
// catalog error, matching the optional Cera-shop section policy.
func parseCurrentCeraShopMultiBonus(source dnfpvf.Source) (map[uint32][]currentCeraShopBonusItem, error) {
	bonusByCommodity := make(map[uint32][]currentCeraShopBonusItem)
	if source == nil {
		return bonusByCommodity, nil
	}
	text, err := source.ReadText("etc/chn_cerashop_multibonus.etc")
	if err != nil {
		return bonusByCommodity, nil
	}
	document, err := dnfpvf.Parse("etc/chn_cerashop_multibonus.etc", text)
	if err != nil {
		return nil, fmt.Errorf("%w: parse etc/chn_cerashop_multibonus.etc: %v", errCurrentCeraShopCatalogUnavailable, err)
	}
	// The runtime grammar nests repeated [bonus item] blocks inside
	// [one plus one bonus item info ipg].  The flat document model keeps one
	// section entry per block, so walk every occurrence instead of Section(),
	// which only returns the first.
	for _, section := range document.Sections {
		if section.Name != "bonus item" {
			continue
		}
		if section.Start < 0 || section.End > len(document.Tokens) || section.Start > section.End {
			return nil, fmt.Errorf("%w: multibonus section bounds start=%d end=%d tokens=%d", errCurrentCeraShopCatalogUnavailable, section.Start, section.End, len(document.Tokens))
		}
		values := currentCeraShopPVFValues(document.Tokens[section.Start:section.End])
		if len(values) < 3 || len(values)%2 == 0 {
			return nil, fmt.Errorf("%w: multibonus bonus item values=%d want 1+2n", errCurrentCeraShopCatalogUnavailable, len(values))
		}
		commodityID, ok := currentCeraShopPVFUint32(values[0])
		if !ok || commodityID == 0 {
			return nil, fmt.Errorf("%w: multibonus bonus item invalid_commodity", errCurrentCeraShopCatalogUnavailable)
		}
		bonuses := make([]currentCeraShopBonusItem, 0, (len(values)-1)/2)
		for offset := 1; offset+1 < len(values); offset += 2 {
			itemID, itemOK := currentCeraShopPVFUint32(values[offset])
			count, countOK := currentCeraShopPVFUint32(values[offset+1])
			if !itemOK || itemID == 0 || !countOK || count == 0 {
				return nil, fmt.Errorf("%w: multibonus commodity=%d invalid_bonus_pair", errCurrentCeraShopCatalogUnavailable, commodityID)
			}
			bonuses = append(bonuses, currentCeraShopBonusItem{ItemID: itemID, Count: count})
		}
		if _, exists := bonusByCommodity[commodityID]; exists {
			return nil, fmt.Errorf("%w: multibonus duplicate commodity=%d", errCurrentCeraShopCatalogUnavailable, commodityID)
		}
		bonusByCommodity[commodityID] = bonuses
	}
	return bonusByCommodity, nil
}
