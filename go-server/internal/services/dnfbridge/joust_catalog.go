package dnfbridge

import (
	"context"

	dnfjoust "longheng.io/server/internal/modules/dnf/joust"
)

func (s *Service) currentJoustCatalog(ctx context.Context) (*dnfjoust.Catalog, error) {
	if s == nil {
		return nil, dnfjoust.ErrRoundUnavailable
	}
	s.joustCatalogMu.Lock()
	if s.joustCatalog != nil || s.joustCatalogLoadErr != nil {
		catalog, err := s.joustCatalog, s.joustCatalogLoadErr
		s.joustCatalogMu.Unlock()
		return catalog, err
	}
	s.joustCatalogMu.Unlock()
	itemCatalog, err := s.currentPVFItemCatalog()
	if err != nil {
		return nil, err
	}
	s.joustCatalogMu.Lock()
	defer s.joustCatalogMu.Unlock()
	if s.joustCatalog != nil || s.joustCatalogLoadErr != nil {
		return s.joustCatalog, s.joustCatalogLoadErr
	}
	s.joustCatalog, s.joustCatalogLoadErr = dnfjoust.LoadCatalog(ctx, itemCatalog.source)
	return s.joustCatalog, s.joustCatalogLoadErr
}
