package dnfbridge

import (
	"errors"
	"fmt"
	"strings"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

// alignedUpgradeTicketResolverForCommand keeps PVF loading request-driven.
// Only the op50 upgrade command needs this dependency; other aligned commands
// do not open or scan the item catalog. The resolver mirrors the 86JP
// ItemUpgradeConsumableResolver: the material stackable's
// [equipment reinforcement ticket] / [equipment amplify reinforcement ticket]
// sections carry TargetLevel and SuccessRatePercent (weight = percent*1000),
// [enchant random] marks the unsupported multi-candidate family, and the
// target equipment's [impossible contents] may forbid upgrade flows.
func (s *Service) alignedUpgradeTicketResolverForCommand(opcode dnfenum.CmdPacket) (alignedcmd.UpgradeTicketResolver, error) {
	if opcode != dnfenum.CmdPacketUpgradeItem {
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
	return func(materialItemID int64, targetItemID int64) (alignedcmd.UpgradeTicketResolution, error) {
		return resolveCurrentUpgradeTicketMetadata(catalog, source, materialItemID, targetItemID)
	}, nil
}

// resolveCurrentUpgradeTicketMetadata resolves one op50 material/target pair
// through the active runtime PVF. Materials that are not stackables or carry
// no ticket section resolve with an empty TicketMode (the owner keeps the
// request on the pending normal-reinforcement path); only catalog/document
// read failures surface as errors so the mutation fails closed.
func resolveCurrentUpgradeTicketMetadata(catalog *pvfDungeonDropCatalog, source dnfpvf.Source, materialItemID int64, targetItemID int64) (alignedcmd.UpgradeTicketResolution, error) {
	if catalog == nil || source == nil {
		return alignedcmd.UpgradeTicketResolution{}, errDungeonDropSourceRequired
	}
	resolution := alignedcmd.UpgradeTicketResolution{}
	if materialItemID <= 0 || materialItemID > int64(^uint32(0)) || targetItemID <= 0 || targetItemID > int64(^uint32(0)) {
		return resolution, nil
	}

	materialDefinition, err := catalog.ResolveItem(uint32(materialItemID))
	if err != nil {
		if errors.Is(err, errDungeonDropItemUnresolved) {
			return resolution, nil
		}
		return alignedcmd.UpgradeTicketResolution{}, fmt.Errorf("resolve upgrade ticket material item=%d: %w", materialItemID, err)
	}
	if materialDefinition.Kind != dungeonDropItemStackable {
		return resolution, nil
	}
	materialDocument, err := parseDungeonCardPVFDocument(source, materialDefinition.PVFPath)
	if err != nil {
		return alignedcmd.UpgradeTicketResolution{}, fmt.Errorf("parse upgrade ticket material item=%d path=%s: %w", materialItemID, materialDefinition.PVFPath, err)
	}
	if values := materialDocument.Ints("equipment reinforcement ticket"); len(values) > 0 {
		resolution.TicketMode = "reinforce"
		resolution.TargetLevel = values[0]
		resolution.SuccessWeight = upgradeTicketSuccessWeight(values)
		resolution.TicketPVFPath = materialDefinition.PVFPath
	} else if values := materialDocument.Ints("equipment amplify reinforcement ticket"); len(values) > 0 {
		resolution.TicketMode = "amplify"
		resolution.TargetLevel = values[0]
		resolution.SuccessWeight = upgradeTicketSuccessWeight(values)
		resolution.TicketPVFPath = materialDefinition.PVFPath
	}
	if _, found := materialDocument.Section("enchant random"); found {
		resolution.TicketRandom = true
		if resolution.TicketMode == "" {
			// Carry a placeholder mode so the owner enters the resolved path
			// and rejects the random family with its client-visible code
			// before any mode matching happens.
			resolution.TicketMode = "reinforce"
			resolution.TicketPVFPath = materialDefinition.PVFPath
		}
	}
	if resolution.TicketMode == "" {
		return resolution, nil
	}

	targetDefinition, err := catalog.ResolveItem(uint32(targetItemID))
	if err != nil {
		if errors.Is(err, errDungeonDropItemUnresolved) {
			return resolution, nil
		}
		return alignedcmd.UpgradeTicketResolution{}, fmt.Errorf("resolve upgrade ticket target item=%d: %w", targetItemID, err)
	}
	resolution.TargetKind = string(targetDefinition.Kind)
	if targetDefinition.Kind != dungeonDropItemEquipment {
		return resolution, nil
	}
	targetDocument, err := parseDungeonCardPVFDocument(source, targetDefinition.PVFPath)
	if err != nil {
		return alignedcmd.UpgradeTicketResolution{}, fmt.Errorf("parse upgrade ticket target item=%d path=%s: %w", targetItemID, targetDefinition.PVFPath, err)
	}
	for _, value := range targetDocument.Texts("impossible contents") {
		normalized := strings.ToLower(strings.Trim(value, "` []"))
		if normalized == "upgrade" || normalized == "amplify upgrade" {
			resolution.TargetUpgradeForbidden = true
			break
		}
	}
	if equipmentType, found := targetDocument.Text("equipment type"); found {
		resolution.TargetEquipmentType = equipmentType
	}
	return resolution, nil
}

// upgradeTicketSuccessWeight maps the ticket section's SuccessRatePercent to
// the 86JP roll weight (percent*1000). A missing percent means a certain
// success, matching the reference default.
func upgradeTicketSuccessWeight(values []int64) int64 {
	if len(values) < 2 {
		return 100000
	}
	weight := values[1] * 1000
	if weight < 0 {
		return 0
	}
	if weight > 100000 {
		return 100000
	}
	return weight
}
