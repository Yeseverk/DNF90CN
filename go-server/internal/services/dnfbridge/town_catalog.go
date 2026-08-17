package dnfbridge

import (
	"context"
	"fmt"

	dnftown "longheng.io/server/internal/modules/dnf/town"
)

func (s *Service) preloadTownCatalog(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.initialEquipmentMu.Lock()
	archive, err := s.initialEquipmentArchiveLocked()
	s.initialEquipmentMu.Unlock()
	if err != nil {
		return fmt.Errorf("preload town pvf: %w", err)
	}
	table, err := dnftown.Load(ctx, archive, dnftown.Options{})
	if err != nil {
		return fmt.Errorf("preload town catalog: %w", err)
	}
	s.townCatalogMu.Lock()
	s.townCatalog = table
	s.townCatalogMu.Unlock()
	snapshot := table.Snapshot()
	s.logPacketEvent("dnf-town-catalog-loaded", "towns", snapshot.Towns, "areas", snapshot.Areas)
	return nil
}

func (s *Service) townArea(townID, areaID int64) (dnftown.Area, bool) {
	if s == nil {
		return dnftown.Area{}, false
	}
	s.townCatalogMu.RLock()
	table := s.townCatalog
	s.townCatalogMu.RUnlock()
	if table == nil {
		return dnftown.Area{}, false
	}
	return table.FindArea(townID, areaID)
}

func (s *Service) townGateArea(townID int64) (dnftown.Area, bool) {
	if s == nil {
		return dnftown.Area{}, false
	}
	s.townCatalogMu.RLock()
	table := s.townCatalog
	s.townCatalogMu.RUnlock()
	if table == nil {
		return dnftown.Area{}, false
	}
	return table.FindGateArea(townID)
}
