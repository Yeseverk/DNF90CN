package dnfbridge

import (
	"errors"
	"fmt"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

// alignedEnchantBeadResolverForCommand keeps PVF loading request-driven. Only
// the real current enchant-by-bead command needs this dependency; other
// aligned commands do not open or scan the item catalog. The resolver mirrors
// the 86JP InventoryEnchantStore metadata chain: bead [monster card id] /
// [enchant index] / [bead limited usable item] (legacy [target item id]), card [string data] equipment-type
// whitelist and [enchant table]/[enchant index] upgrade-count table, and the
// target equipment's [equipment type].
func (s *Service) alignedEnchantBeadResolverForCommand(opcode dnfenum.CmdPacket) (alignedcmd.EnchantBeadResolver, error) {
	if opcode != dnfenum.CmdPacketEnchantByBead {
		return nil, nil
	}
	catalog, err := s.currentPVFItemCatalog()
	if err != nil {
		return nil, err
	}
	if catalog == nil {
		return nil, errDungeonDropSourceRequired
	}
	s.initialEquipmentMu.Lock()
	source, err := s.initialEquipmentArchiveLocked()
	s.initialEquipmentMu.Unlock()
	if err != nil {
		return nil, err
	}
	return func(beadItemID int64, targetItemID int64) (alignedcmd.EnchantBeadResolution, error) {
		return resolveCurrentEnchantBeadMetadata(catalog, source, beadItemID, targetItemID)
	}, nil
}

// resolveCurrentEnchantBeadMetadata resolves one bead/target template pair
// through the active runtime PVF. Unresolvable or non-stackable beads and
// beads without an enchant card are reported as a zero CardItemID (the owner
// maps that to the client-visible invalid-bead code); only catalog/document
// read failures surface as errors so the mutation fails closed.
func resolveCurrentEnchantBeadMetadata(catalog *pvfDungeonDropCatalog, source dnfpvf.Source, beadItemID int64, targetItemID int64) (alignedcmd.EnchantBeadResolution, error) {
	if catalog == nil || source == nil {
		return alignedcmd.EnchantBeadResolution{}, errDungeonDropSourceRequired
	}
	if beadItemID <= 0 || beadItemID > int64(^uint32(0)) || targetItemID <= 0 || targetItemID > int64(^uint32(0)) {
		return alignedcmd.EnchantBeadResolution{}, nil
	}

	resolution := alignedcmd.EnchantBeadResolution{}
	beadDefinition, err := catalog.ResolveItem(uint32(beadItemID))
	if err != nil {
		if errors.Is(err, errDungeonDropItemUnresolved) {
			return resolution, nil
		}
		return alignedcmd.EnchantBeadResolution{}, fmt.Errorf("resolve enchant bead item=%d: %w", beadItemID, err)
	}
	if beadDefinition.Kind != dungeonDropItemStackable {
		return resolution, nil
	}
	beadDocument, err := parseDungeonCardPVFDocument(source, beadDefinition.PVFPath)
	if err != nil {
		return alignedcmd.EnchantBeadResolution{}, fmt.Errorf("parse enchant bead item=%d path=%s: %w", beadItemID, beadDefinition.PVFPath, err)
	}
	resolution.TargetWhitelist = append([]int64(nil), beadDocument.Ints("bead limited usable item")...)
	if len(resolution.TargetWhitelist) == 0 {
		resolution.TargetWhitelist = append([]int64(nil), beadDocument.Ints("target item id")...)
	}
	cardItemID, found := beadDocument.Int("monster card id")
	if !found {
		cardItemID, found = beadDocument.Int("enchant index")
	}
	if !found || cardItemID <= 0 || cardItemID > int64(^uint32(0)) {
		return resolution, nil
	}
	resolution.CardItemID = cardItemID

	cardDefinition, err := catalog.ResolveItem(uint32(cardItemID))
	if err != nil {
		if errors.Is(err, errDungeonDropItemUnresolved) {
			resolution.CardItemID = 0
			return resolution, nil
		}
		return alignedcmd.EnchantBeadResolution{}, fmt.Errorf("resolve enchant card item=%d: %w", cardItemID, err)
	}
	if cardDefinition.Kind != dungeonDropItemStackable {
		resolution.CardItemID = 0
		return resolution, nil
	}
	resolution.CardPVFPath = cardDefinition.PVFPath
	cardDocument, err := parseDungeonCardPVFDocument(source, cardDefinition.PVFPath)
	if err != nil {
		return alignedcmd.EnchantBeadResolution{}, fmt.Errorf("parse enchant card item=%d path=%s: %w", cardItemID, cardDefinition.PVFPath, err)
	}
	// The first [string data] entry is the card icon; the remaining entries
	// are the equipment types the card may enchant (86JP metadata rule).
	stringData := cardDocument.Texts("string data")
	if len(stringData) > 1 {
		resolution.AllowedEquipmentTypes = append([]string(nil), stringData[1:]...)
	}
	resolution.UpgradeCounts = append([]int64(nil), cardDocument.Ints("enchant index")...)
	if tableTokens, ok := cardDocument.Section("enchant table"); ok {
		for _, token := range tableTokens {
			if token.Kind == dnfpvf.TokenInt {
				resolution.UpgradeCounts = append(resolution.UpgradeCounts, token.Int)
			}
		}
	}

	targetDefinition, err := catalog.ResolveItem(uint32(targetItemID))
	if err != nil {
		if errors.Is(err, errDungeonDropItemUnresolved) {
			return resolution, nil
		}
		return alignedcmd.EnchantBeadResolution{}, fmt.Errorf("resolve enchant target item=%d: %w", targetItemID, err)
	}
	resolution.TargetKind = string(targetDefinition.Kind)
	if targetDefinition.Kind != dungeonDropItemEquipment {
		return resolution, nil
	}
	targetDocument, err := parseDungeonCardPVFDocument(source, targetDefinition.PVFPath)
	if err != nil {
		return alignedcmd.EnchantBeadResolution{}, fmt.Errorf("parse enchant target item=%d path=%s: %w", targetItemID, targetDefinition.PVFPath, err)
	}
	equipmentType, found := targetDocument.Text("equipment type")
	if !found {
		return alignedcmd.EnchantBeadResolution{}, fmt.Errorf("enchant target item=%d path=%s missing [equipment type]", targetItemID, targetDefinition.PVFPath)
	}
	resolution.TargetEquipmentType = equipmentType
	return resolution, nil
}

// resolveCurrentEnchantCardMetadata validates a raw monster card used through
// an enchanter player store. Unlike the bead command, the supplied stack is
// already the card, so no bead-to-card indirection or target-item whitelist is
// involved; the card's PVF equipment-type and upgrade-count tables remain
// authoritative.
func resolveCurrentEnchantCardMetadata(catalog *pvfDungeonDropCatalog, source dnfpvf.Source, cardItemID, targetItemID int64) (alignedcmd.EnchantBeadResolution, error) {
	resolution := alignedcmd.EnchantBeadResolution{CardItemID: cardItemID}
	if catalog == nil || source == nil {
		return alignedcmd.EnchantBeadResolution{}, errDungeonDropSourceRequired
	}
	if cardItemID <= 0 || cardItemID > int64(^uint32(0)) || targetItemID <= 0 || targetItemID > int64(^uint32(0)) {
		return alignedcmd.EnchantBeadResolution{}, nil
	}
	cardDefinition, err := catalog.ResolveItem(uint32(cardItemID))
	if err != nil {
		if errors.Is(err, errDungeonDropItemUnresolved) {
			return alignedcmd.EnchantBeadResolution{}, nil
		}
		return alignedcmd.EnchantBeadResolution{}, fmt.Errorf("resolve enchant card item=%d: %w", cardItemID, err)
	}
	if cardDefinition.Kind != dungeonDropItemStackable {
		return alignedcmd.EnchantBeadResolution{}, nil
	}
	resolution.CardPVFPath = cardDefinition.PVFPath
	cardDocument, err := parseDungeonCardPVFDocument(source, cardDefinition.PVFPath)
	if err != nil {
		return alignedcmd.EnchantBeadResolution{}, fmt.Errorf("parse enchant card item=%d path=%s: %w", cardItemID, cardDefinition.PVFPath, err)
	}
	stringData := cardDocument.Texts("string data")
	if len(stringData) > 1 {
		resolution.AllowedEquipmentTypes = append([]string(nil), stringData[1:]...)
	}
	resolution.UpgradeCounts = append([]int64(nil), cardDocument.Ints("enchant index")...)
	if tableTokens, ok := cardDocument.Section("enchant table"); ok {
		for _, token := range tableTokens {
			if token.Kind == dnfpvf.TokenInt {
				resolution.UpgradeCounts = append(resolution.UpgradeCounts, token.Int)
			}
		}
	}
	targetDefinition, err := catalog.ResolveItem(uint32(targetItemID))
	if err != nil {
		if errors.Is(err, errDungeonDropItemUnresolved) {
			return resolution, nil
		}
		return alignedcmd.EnchantBeadResolution{}, fmt.Errorf("resolve enchant target item=%d: %w", targetItemID, err)
	}
	resolution.TargetKind = string(targetDefinition.Kind)
	if targetDefinition.Kind != dungeonDropItemEquipment {
		return resolution, nil
	}
	targetDocument, err := parseDungeonCardPVFDocument(source, targetDefinition.PVFPath)
	if err != nil {
		return alignedcmd.EnchantBeadResolution{}, fmt.Errorf("parse enchant target item=%d path=%s: %w", targetItemID, targetDefinition.PVFPath, err)
	}
	equipmentType, found := targetDocument.Text("equipment type")
	if !found {
		return alignedcmd.EnchantBeadResolution{}, fmt.Errorf("enchant target item=%d path=%s missing [equipment type]", targetItemID, targetDefinition.PVFPath)
	}
	resolution.TargetEquipmentType = equipmentType
	return resolution, nil
}
