package dnfbridge

import (
	"context"
	"fmt"
	"time"
)

func (s *Service) currentPVFItemCatalog() (*pvfDungeonDropCatalog, error) {
	if s == nil {
		return nil, errDungeonDropSourceRequired
	}
	s.pvfItemCatalogMu.Lock()
	defer s.pvfItemCatalogMu.Unlock()
	if s.pvfItemCatalog != nil || s.pvfItemCatalogLoadErr != nil {
		return s.pvfItemCatalog, s.pvfItemCatalogLoadErr
	}
	s.initialEquipmentMu.Lock()
	archive, err := s.initialEquipmentArchiveLocked()
	s.initialEquipmentMu.Unlock()
	if err == nil {
		s.pvfItemCatalog, err = newPVFDungeonDropCatalog(archive)
	}
	s.pvfItemCatalogLoadErr = err
	return s.pvfItemCatalog, s.pvfItemCatalogLoadErr
}

func (s *Service) currentPVFItemCatalogIfLoaded() (*pvfDungeonDropCatalog, bool, error) {
	if s == nil {
		return nil, false, errDungeonDropSourceRequired
	}
	s.pvfItemCatalogMu.Lock()
	defer s.pvfItemCatalogMu.Unlock()
	if s.pvfItemCatalog != nil {
		return s.pvfItemCatalog, true, nil
	}
	if s.pvfItemCatalogLoadErr != nil {
		return nil, true, s.pvfItemCatalogLoadErr
	}
	return nil, false, nil
}

func (s *Service) preloadPVFItemCatalog(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	started := time.Now()
	catalog, err := s.currentPVFItemCatalog()
	if err != nil {
		return fmt.Errorf("preload current pvf item catalog: %w", err)
	}
	elapsed := time.Since(started)
	s.logPacketEvent("dnf-pvf-item-catalog-loaded",
		"monster_paths", len(catalog.monsterPaths),
		"item_refs", len(catalog.itemRefs),
		"stackable_ids", len(catalog.stackableIDs),
		"elapsed_ms", elapsed.Milliseconds())
	return nil
}
