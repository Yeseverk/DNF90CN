package dnfbridge

import (
	"context"
	"errors"
	"fmt"

	dnfhonor "longheng.io/server/internal/modules/dnf/honor"
)

var (
	errHonorTableUnavailable   = errors.New("dnf honor table is unavailable")
	errHonorAccountUnavailable = errors.New("dnf honor account repository is unavailable")
	errHonorAccountNotFound    = errors.New("dnf honor account record was not found")
)

// preloadHonorTable validates both ordinary account-honor and character-scoped
// HonorExpert source sections from the runtime PVF.
func (s *Service) preloadHonorTable(ctx context.Context) error {
	if _, err := s.loadHonorTable(ctx); err != nil {
		return fmt.Errorf("preload dnf honor table: %w", err)
	}
	return nil
}

func (s *Service) loadHonorTable(ctx context.Context) (*dnfhonor.Tables, error) {
	if s == nil {
		return nil, errHonorTableUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.honorMu.Lock()
	defer s.honorMu.Unlock()
	if s.honorTable != nil {
		return s.honorTable, nil
	}
	if s.honorLoadErr != nil {
		return nil, s.honorLoadErr
	}

	s.initialEquipmentMu.Lock()
	archive, err := s.initialEquipmentArchiveLocked()
	s.initialEquipmentMu.Unlock()
	if err != nil {
		s.honorLoadErr = err
		return nil, err
	}
	table, err := dnfhonor.LoadTables(archive)
	if err != nil {
		s.honorLoadErr = err
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.honorTable = table
	snapshot := table.Snapshot()
	s.logPacketEvent("dnf-honor-table-loaded",
		"ordinary_grades", snapshot.OrdinaryGrades,
		"ordinary_levels", snapshot.OrdinaryLevels,
		"max_ordinary_level", snapshot.MaxOrdinaryLevel,
		"max_level_experience", snapshot.MaxLevelExperience,
		"max_total_experience", snapshot.MaxTotalExperience,
		"expert_grades", snapshot.ExpertGrades,
		"expert_experience_rows", snapshot.ExpertExperienceRows,
		"expert_calculation_ready", snapshot.ExpertCalculationReady)
	return table, nil
}

func (s *Service) currentAccountHonorProgress(ctx context.Context, sessions ...*gameSession) (dnfhonor.Progress, error) {
	if s == nil {
		return dnfhonor.Progress{}, errHonorTableUnavailable
	}
	s.honorMu.Lock()
	table := s.honorTable
	loadErr := s.honorLoadErr
	s.honorMu.Unlock()
	if table == nil {
		if loadErr != nil {
			return dnfhonor.Progress{}, loadErr
		}
		return dnfhonor.Progress{}, errHonorTableUnavailable
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Account == nil {
		return dnfhonor.Progress{}, errHonorAccountUnavailable
	}
	accountID := s.accountIDForSession(sessions...)
	account, found, err := repositories.Account.Load(ctx, accountID)
	if err != nil {
		return dnfhonor.Progress{}, fmt.Errorf("load dnf honor account %q: %w", accountID, err)
	}
	if !found {
		return dnfhonor.Progress{}, fmt.Errorf("%w: %s", errHonorAccountNotFound, accountID)
	}
	return table.Resolve(account.HonorExp)
}
