package dnfbridge

import (
	"context"
	"fmt"
	"time"

	dnfexpertjob "longheng.io/server/internal/modules/dnf/expertjob"
)

func (s *Service) currentExpertJobCatalog() (*dnfexpertjob.Catalog, error) {
	if s == nil {
		return nil, dnfexpertjob.ErrCatalogUnavailable
	}
	s.expertJobMu.Lock()
	defer s.expertJobMu.Unlock()
	if s.expertJobCatalog != nil || s.expertJobCatalogLoadErr != nil {
		return s.expertJobCatalog, s.expertJobCatalogLoadErr
	}
	items, err := s.currentPVFItemCatalog()
	if err == nil && items != nil {
		s.expertJobCatalog, err = dnfexpertjob.Load(context.Background(), items.source)
	}
	s.expertJobCatalogLoadErr = err
	return s.expertJobCatalog, s.expertJobCatalogLoadErr
}

func (s *Service) preloadExpertJobCatalog(ctx context.Context) error {
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
	catalog, err := s.currentExpertJobCatalog()
	if err != nil {
		return fmt.Errorf("preload current expert job catalog: %w", err)
	}
	alchemist, _ := catalog.Config(dnfexpertjob.AlchemistType)
	doll, _ := catalog.Config(dnfexpertjob.DollControllerType)
	s.logPacketEvent("dnf-pvf-expert-job-catalog-loaded",
		"alchemist_recipes", len(alchemist.Recipes),
		"alchemist_extractors", len(alchemist.Extractors),
		"doll_recipes", len(doll.Recipes),
		"doll_extractors", len(doll.Extractors),
		"elapsed_ms", time.Since(started).Milliseconds())
	return nil
}
