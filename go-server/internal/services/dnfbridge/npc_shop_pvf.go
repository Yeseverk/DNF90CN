package dnfbridge

import (
	"errors"
	"fmt"
	"math"
	"strings"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

const (
	currentNPCShopItemListPath   = "itemshop/itemshop.lst"
	currentNPCShopPriceTablePath = "equipment/pricetable.tbl"
)

var (
	errCurrentNPCShopCatalogUnavailable = errors.New("dnf current NPC shop PVF catalog is unavailable")
	errCurrentNPCShopProductUnavailable = errors.New("dnf current NPC shop product is unavailable")
)

type currentNPCShopCatalog struct {
	source                    dnfpvf.Source
	buyableItems              map[uint32]struct{}
	equipmentSellRatePermille int64
	listedShopCount           int
	loadedShopCount           int
	missingShopCount          int
	firstMissingShopPath      string
}

type currentNPCShopPricing struct {
	Definition        dungeonDropItemDefinition
	BuyGold           int64
	SellGold          int64
	Buyable           bool
	MaterialExchange  bool
	NeedMaterialItem  uint32
	NeedMaterialCount int64
}

func newCurrentNPCShopCatalog(source dnfpvf.Source) (*currentNPCShopCatalog, error) {
	if source == nil {
		return nil, errCurrentNPCShopCatalogUnavailable
	}
	shopPaths, _, err := loadDungeonDropList(source, currentNPCShopItemListPath)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errCurrentNPCShopCatalogUnavailable, err)
	}
	buyable := make(map[uint32]struct{})
	loadedShopCount := 0
	missingShopCount := 0
	firstMissingShopPath := ""
	for shopID, listedPath := range shopPaths {
		documentPath, text, readErr := readDungeonDropText(source, "itemshop", listedPath)
		if readErr != nil {
			if errors.Is(readErr, platformpvf.ErrFileNotFound) || errors.Is(readErr, dnfpvf.ErrDocNotFound) {
				missingShopCount++
				if firstMissingShopPath == "" {
					firstMissingShopPath = listedPath
				}
				continue
			}
			return nil, fmt.Errorf("%w: shop=%d path=%s: %v", errCurrentNPCShopCatalogUnavailable, shopID, listedPath, readErr)
		}
		document, parseErr := dnfpvf.Parse(documentPath, text)
		if parseErr != nil {
			return nil, fmt.Errorf("%w: shop=%d path=%s: %v", errCurrentNPCShopCatalogUnavailable, shopID, documentPath, parseErr)
		}
		loadedShopCount++
		for _, section := range document.Sections {
			if strings.ToLower(strings.TrimSpace(section.Name)) != "item list" || section.Start < 0 || section.End > len(document.Tokens) || section.Start > section.End {
				continue
			}
			for _, token := range document.Tokens[section.Start:section.End] {
				if token.Kind == dnfpvf.TokenInt && token.Int > 0 && token.Int <= math.MaxUint32 {
					buyable[uint32(token.Int)] = struct{}{}
				}
			}
		}
	}
	if len(buyable) == 0 {
		return nil, fmt.Errorf("%w: no [item list] entries", errCurrentNPCShopCatalogUnavailable)
	}
	priceTable, err := parseDungeonCardPVFDocument(source, currentNPCShopPriceTablePath)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", errCurrentNPCShopCatalogUnavailable, currentNPCShopPriceTablePath, err)
	}
	rates := priceTable.Ints("rate")
	if len(rates) < 1 || rates[0] <= 0 || rates[0] > math.MaxInt32 {
		return nil, fmt.Errorf("%w: %s [rate] is invalid", errCurrentNPCShopCatalogUnavailable, currentNPCShopPriceTablePath)
	}
	return &currentNPCShopCatalog{
		source:                    source,
		buyableItems:              buyable,
		equipmentSellRatePermille: rates[0],
		listedShopCount:           len(shopPaths),
		loadedShopCount:           loadedShopCount,
		missingShopCount:          missingShopCount,
		firstMissingShopPath:      firstMissingShopPath,
	}, nil
}

func (s *Service) currentNPCShopCatalog() (*currentNPCShopCatalog, error) {
	if s == nil {
		return nil, errCurrentNPCShopCatalogUnavailable
	}
	s.npcShopCatalogMu.Lock()
	defer s.npcShopCatalogMu.Unlock()
	if s.npcShopCatalog != nil || s.npcShopCatalogLoadErr != nil {
		return s.npcShopCatalog, s.npcShopCatalogLoadErr
	}
	s.initialEquipmentMu.Lock()
	archive, err := s.initialEquipmentArchiveLocked()
	s.initialEquipmentMu.Unlock()
	if err == nil {
		s.npcShopCatalog, err = newCurrentNPCShopCatalog(archive)
	}
	s.npcShopCatalogLoadErr = err
	if err == nil && s.npcShopCatalog != nil {
		s.logInfo("dnf NPC shop PVF catalog loaded",
			"listed_shops", s.npcShopCatalog.listedShopCount,
			"loaded_shops", s.npcShopCatalog.loadedShopCount,
			"missing_shops", s.npcShopCatalog.missingShopCount,
			"buyable_items", len(s.npcShopCatalog.buyableItems))
		if s.npcShopCatalog.missingShopCount > 0 {
			s.logWarn("dnf NPC shop PVF list contains missing shop files",
				"missing_shops", s.npcShopCatalog.missingShopCount,
				"first_missing_shop", s.npcShopCatalog.firstMissingShopPath)
		}
	}
	return s.npcShopCatalog, s.npcShopCatalogLoadErr
}

func resolveCurrentNPCShopPricing(shop *currentNPCShopCatalog, items *pvfDungeonDropCatalog, itemID uint32) (currentNPCShopPricing, error) {
	if shop == nil || shop.source == nil || items == nil || itemID == 0 {
		return currentNPCShopPricing{}, errCurrentNPCShopCatalogUnavailable
	}
	definition, err := items.ResolveItem(itemID)
	if err != nil {
		return currentNPCShopPricing{}, fmt.Errorf("%w: item=%d: %v", errCurrentNPCShopProductUnavailable, itemID, err)
	}
	document, err := parseDungeonCardPVFDocument(shop.source, definition.PVFPath)
	if err != nil {
		return currentNPCShopPricing{}, fmt.Errorf("%w: item=%d path=%s: %v", errCurrentNPCShopProductUnavailable, itemID, definition.PVFPath, err)
	}
	price, priceFound := document.Int("price")
	value, valueFound := document.Int("value")
	buyGold := int64(0)
	if priceFound && price >= 0 {
		buyGold = price
	} else if valueFound && value >= 0 {
		buyGold = value
	}
	if buyGold > math.MaxInt32 {
		return currentNPCShopPricing{}, fmt.Errorf("%w: item=%d buy_gold=%d", errCurrentNPCShopProductUnavailable, itemID, buyGold)
	}

	sellGold := int64(0)
	switch definition.Kind {
	case dungeonDropItemEquipment:
		base := buyGold
		if valueFound && value >= 0 {
			base = value
		}
		if base > math.MaxInt64/shop.equipmentSellRatePermille {
			return currentNPCShopPricing{}, fmt.Errorf("%w: item=%d sell price overflow", errCurrentNPCShopProductUnavailable, itemID)
		}
		sellGold = base * shop.equipmentSellRatePermille / 1000
		if sellGold < 1 {
			sellGold = 1
		}
	case dungeonDropItemStackable:
		if valueFound && value >= 0 {
			sellGold = value / 5
		} else if priceFound && price > 0 {
			sellGold = price / 5
		}
	default:
		return currentNPCShopPricing{}, fmt.Errorf("%w: item=%d kind=%s", errCurrentNPCShopProductUnavailable, itemID, definition.Kind)
	}
	if sellGold < 0 || sellGold > math.MaxInt32 {
		return currentNPCShopPricing{}, fmt.Errorf("%w: item=%d sell_gold=%d", errCurrentNPCShopProductUnavailable, itemID, sellGold)
	}
	needMaterial := document.Ints("need material")
	materialExchange := len(needMaterial) >= 2 && needMaterial[0] > 0 && needMaterial[0] <= math.MaxUint32 && needMaterial[1] > 0
	_, listed := shop.buyableItems[itemID]
	pricing := currentNPCShopPricing{
		Definition:       definition,
		BuyGold:          buyGold,
		SellGold:         sellGold,
		MaterialExchange: materialExchange,
	}
	if materialExchange {
		pricing.NeedMaterialItem = uint32(needMaterial[0])
		pricing.NeedMaterialCount = needMaterial[1]
	}
	// Material-exchange goods pay with their [need material] rows instead of
	// gold; the client shop UI shows only the material cost for them.
	pricing.Buyable = listed && (materialExchange || buyGold > 0)
	return pricing, nil
}

func currentNPCShopContainsItem(catalog *currentNPCShopCatalog, itemID uint32) bool {
	if catalog == nil {
		return false
	}
	_, found := catalog.buyableItems[itemID]
	return found
}
