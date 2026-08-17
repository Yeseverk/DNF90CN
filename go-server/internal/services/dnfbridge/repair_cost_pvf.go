package dnfbridge

import (
	"context"
	"fmt"
	"strings"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

// currentRepairCostCatalog owns the runtime-PVF repair pricing inputs: the
// global rates from equipment/pricetable.tbl and etc/upgrade.etc, plus the
// per-item [durability]/[repair price]/[grade]/[equipment type] evidence.
// It mirrors the 86JP EquipmentRepairPriceProvider inputs but reads only the
// active runtime PVF. Per-item evidence is cached; the zero MaxDurability
// marks a known non-repairable (or unknown) item without erroring.
type currentRepairCostCatalog struct {
	repairCostRate  float64
	quickRepairRate float64
	upgradeRates    []float64
	source          initialEquipmentTextSource
	paths           map[int64]string
	evidence        map[int64]alignedcmd.RepairCostEvidence
}

func (s *Service) currentRepairCostCatalog() (*currentRepairCostCatalog, error) {
	if s == nil {
		return nil, fmt.Errorf("repair cost catalog: service required")
	}
	s.repairCostMu.Lock()
	defer s.repairCostMu.Unlock()
	if s.repairCostCatalog != nil || s.repairCostLoadErr != nil {
		return s.repairCostCatalog, s.repairCostLoadErr
	}
	if err := s.preloadEquipmentStatIndex(context.Background()); err != nil {
		s.repairCostLoadErr = fmt.Errorf("repair cost catalog: equipment index: %w", err)
		return nil, s.repairCostLoadErr
	}
	s.initialEquipmentMu.Lock()
	archive, err := s.initialEquipmentArchiveLocked()
	s.initialEquipmentMu.Unlock()
	if err != nil {
		s.repairCostLoadErr = fmt.Errorf("repair cost catalog: PVF archive: %w", err)
		return nil, s.repairCostLoadErr
	}
	s.equipmentStatsMu.Lock()
	paths := s.equipmentStatPaths
	s.equipmentStatsMu.Unlock()
	if len(paths) == 0 {
		s.repairCostLoadErr = fmt.Errorf("repair cost catalog: equipment paths unavailable")
		return nil, s.repairCostLoadErr
	}
	catalog, err := newCurrentRepairCostCatalog(archive, paths)
	if err != nil {
		s.repairCostLoadErr = err
		return nil, err
	}
	s.repairCostCatalog = catalog
	return catalog, nil
}

func newCurrentRepairCostCatalog(source initialEquipmentTextSource, paths map[int64]string) (*currentRepairCostCatalog, error) {
	if source == nil {
		return nil, fmt.Errorf("repair cost catalog: PVF source required")
	}
	priceText, err := source.ReadText("equipment/pricetable.tbl")
	if err != nil {
		return nil, fmt.Errorf("repair cost catalog: read equipment/pricetable.tbl: %w", err)
	}
	priceDoc, err := dnfpvf.Parse("equipment/pricetable.tbl", priceText)
	if err != nil {
		return nil, fmt.Errorf("repair cost catalog: parse equipment/pricetable.tbl: %w", err)
	}
	repairRates := priceDoc.Numbers("repair cost")
	if len(repairRates) == 0 || repairRates[0] <= 0 {
		return nil, fmt.Errorf("repair cost catalog: pricetable.tbl [repair cost] missing")
	}
	quickRates := priceDoc.Ints("quick repair cost rate")
	if len(quickRates) == 0 || quickRates[0] <= 0 {
		return nil, fmt.Errorf("repair cost catalog: pricetable.tbl [quick repair cost rate] missing")
	}
	upgradeText, err := source.ReadText("etc/upgrade.etc")
	if err != nil {
		return nil, fmt.Errorf("repair cost catalog: read etc/upgrade.etc: %w", err)
	}
	upgradeDoc, err := dnfpvf.Parse("etc/upgrade.etc", upgradeText)
	if err != nil {
		return nil, fmt.Errorf("repair cost catalog: parse etc/upgrade.etc: %w", err)
	}
	upgradeRateInts := upgradeDoc.Ints("repair cost rate by upgrade level")
	upgradeRates := make([]float64, 0, len(upgradeRateInts))
	for _, value := range upgradeRateInts {
		upgradeRates = append(upgradeRates, float64(value))
	}
	return &currentRepairCostCatalog{
		repairCostRate:  repairRates[0],
		quickRepairRate: float64(quickRates[0]) / 100,
		upgradeRates:    upgradeRates,
		source:          source,
		paths:           paths,
		evidence:        make(map[int64]alignedcmd.RepairCostEvidence),
	}, nil
}

func (c *currentRepairCostCatalog) resolve(itemID int64) (alignedcmd.RepairCostEvidence, error) {
	if evidence, ok := c.evidence[itemID]; ok {
		return evidence, nil
	}
	evidence := alignedcmd.RepairCostEvidence{
		MaxDurability:   -1,
		RepairCostRate:  c.repairCostRate,
		QuickRepairRate: c.quickRepairRate,
		UpgradeRates:    c.upgradeRates,
	}
	refPath := strings.TrimSpace(c.paths[itemID])
	if refPath == "" {
		// Unknown item: marked non-repairable, never an error (86JP skip rule).
		c.evidence[itemID] = evidence
		return evidence, nil
	}
	text, actualPath, err := readInitialPVFText(c.source, initialPVFPath("equipment", refPath), refPath)
	if err != nil {
		return alignedcmd.RepairCostEvidence{}, fmt.Errorf("repair cost catalog: item=%d path=%q: %w", itemID, refPath, err)
	}
	document, err := dnfpvf.Parse(actualPath, text)
	if err != nil {
		return alignedcmd.RepairCostEvidence{}, fmt.Errorf("repair cost catalog: item=%d path=%q: %w", itemID, actualPath, err)
	}
	if durability, found := document.Int("durability"); found {
		evidence.MaxDurability = durability
	}
	if repairPrice, found := document.Int("repair price"); found {
		evidence.RepairPrice = repairPrice
	}
	if grade, found := document.Int("grade"); found {
		evidence.Grade = grade
	}
	if equipmentType, found := document.Text("equipment type"); found {
		evidence.EquipmentType = normalizeEquipmentPlacementPVFType(equipmentType)
	}
	c.evidence[itemID] = evidence
	return evidence, nil
}

// alignedRepairCostResolverForCommand keeps PVF loading request-driven: only
// the repair command opens the catalog. A nil resolver makes every paid
// repair fail closed in the owners.
func (s *Service) alignedRepairCostResolverForCommand(opcode dnfenum.CmdPacket) (alignedcmd.RepairCostResolver, error) {
	if opcode != dnfenum.CmdPacketRepairEquipment {
		return nil, nil
	}
	catalog, err := s.currentRepairCostCatalog()
	if err != nil {
		return nil, err
	}
	return catalog.resolve, nil
}
