package dnfbridge

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"longheng.io/server/internal/modules/dnf/adventuregroup"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

var (
	errAdventureGroupTableUnavailable  = errors.New("dnf adventure-group table is unavailable")
	errAdventureGroupRosterUnavailable = errors.New("dnf adventure-group account roster is unavailable")
	errAdventureGroupOwnerMismatch     = errors.New("dnf adventure-group selected character owner mismatch")
)

type accountAdventureGroupState struct {
	Characters []dnfrepo.CharacterRecord
	Summary    adventuregroup.Summary
	Projection adventuregroup.Projection
}

// preloadAdventureGroupTable makes the current runtime PVF authoritative for
// adventure-group calculation. Production cannot start with a guessed table.
func (s *Service) preloadAdventureGroupTable(ctx context.Context) error {
	if _, err := s.loadAdventureGroupTable(ctx); err != nil {
		return fmt.Errorf("preload dnf adventure-group table: %w", err)
	}
	return nil
}

func (s *Service) loadAdventureGroupTable(ctx context.Context) (*adventuregroup.Tables, error) {
	if s == nil {
		return nil, errAdventureGroupTableUnavailable
	}
	s.adventureGroupMu.Lock()
	defer s.adventureGroupMu.Unlock()
	if s.adventureGroupTable != nil {
		return s.adventureGroupTable, nil
	}
	if s.adventureGroupLoadErr != nil {
		return nil, s.adventureGroupLoadErr
	}

	s.initialEquipmentMu.Lock()
	archive, err := s.initialEquipmentArchiveLocked()
	s.initialEquipmentMu.Unlock()
	if err != nil {
		s.adventureGroupLoadErr = err
		return nil, err
	}
	table, err := adventuregroup.LoadComplete(ctx, archive)
	if err != nil {
		s.adventureGroupLoadErr = err
		return nil, err
	}
	s.adventureGroupTable = table
	snapshot := table.Snapshot()
	s.logPacketEvent("dnf-adventure-group-table-loaded",
		"point_ranges", snapshot.PointRanges,
		"manage_thresholds", snapshot.ManageThresholds,
		"manage_level_max", snapshot.ManageLevelMax,
		"exp_bonus_levels", snapshot.ExpBonusLevels,
		"gold_bonus_levels", snapshot.GoldBonusLevels,
		"manage_option_levels", snapshot.ManageOptionLevels)
	return table, nil
}

func (s *Service) currentAccountAdventureGroupSummary(ctx context.Context, selected dnfrepo.CharacterRecord, hasSelected bool, sessions ...*gameSession) (adventuregroup.Summary, error) {
	state, err := s.currentAccountAdventureGroupState(ctx, selected, hasSelected, sessions...)
	return state.Summary, err
}

func (s *Service) currentAccountAdventureGroupState(ctx context.Context, selected dnfrepo.CharacterRecord, hasSelected bool, sessions ...*gameSession) (accountAdventureGroupState, error) {
	if s == nil {
		return accountAdventureGroupState{}, errAdventureGroupTableUnavailable
	}
	s.adventureGroupMu.Lock()
	table := s.adventureGroupTable
	loadErr := s.adventureGroupLoadErr
	s.adventureGroupMu.Unlock()
	if table == nil {
		if loadErr != nil {
			return accountAdventureGroupState{}, loadErr
		}
		return accountAdventureGroupState{}, errAdventureGroupTableUnavailable
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Character == nil {
		return accountAdventureGroupState{}, errAdventureGroupRosterUnavailable
	}
	accountID := s.accountIDForSession(sessions...)
	if hasSelected {
		selectedAccountID := strings.TrimSpace(selected.AccountID)
		if selectedAccountID != "" && selectedAccountID != accountID {
			return accountAdventureGroupState{}, fmt.Errorf("%w: selected=%q current=%q", errAdventureGroupOwnerMismatch, selectedAccountID, accountID)
		}
	}
	characters, err := repositories.Character.ListByAccount(ctx, accountID, defaultCharacterSlots)
	if err != nil {
		return accountAdventureGroupState{}, fmt.Errorf("list dnf adventure-group account characters: %w", err)
	}
	rows := make([]adventuregroup.Character, 0, len(characters))
	for _, character := range characters {
		rows = append(rows, adventuregroup.Character{Level: character.Level})
	}
	summary, err := table.Calculate(rows)
	if err != nil {
		return accountAdventureGroupState{}, err
	}
	return accountAdventureGroupState{
		Characters: characters,
		Summary:    summary,
	}, nil
}

// currentAccountAdventureGroupInfoState adds durable account activity needed
// only by the full 7,420-byte adventure-group payload. The process lock
// serializes the metadata read/modify/write for this local account profile.
func (s *Service) currentAccountAdventureGroupInfoState(
	ctx context.Context,
	selected dnfrepo.CharacterRecord,
	hasSelected bool,
	sessions ...*gameSession,
) (accountAdventureGroupState, error) {
	state, err := s.currentAccountAdventureGroupState(ctx, selected, hasSelected, sessions...)
	if err != nil {
		return accountAdventureGroupState{}, err
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Account == nil {
		state.Projection = adventuregroup.ProjectActivity(0, state.Characters)
		return state, nil
	}
	owner, err := adventuregroup.NewOwner(repositories)
	if err != nil {
		state.Projection = adventuregroup.ProjectActivity(0, state.Characters)
		return state, nil
	}
	accountID := s.accountIDForSession(sessions...)
	s.adventureGroupLoginMu.Lock()
	login, err := owner.ObserveDailyLogin(ctx, adventuregroup.ObserveDailyLoginCommand{
		AccountID:  accountID,
		ObservedAt: s.gameplayNow(),
	})
	s.adventureGroupLoginMu.Unlock()
	if err != nil {
		if errors.Is(err, adventuregroup.ErrAccountNotFound) ||
			errors.Is(err, adventuregroup.ErrAccountRequired) {
			state.Projection = adventuregroup.ProjectActivity(0, state.Characters)
			return state, nil
		}
		return accountAdventureGroupState{}, err
	}
	state.Projection = adventuregroup.ProjectActivity(login.ConsecutiveDays, state.Characters)
	account, found, loadErr := repositories.Account.Load(ctx, accountID)
	if loadErr != nil {
		return accountAdventureGroupState{}, fmt.Errorf("load adventure-group runtime account: %w", loadErr)
	}
	if found {
		runtime, parseErr := adventuregroup.ParseRuntimeState(account, s.adventureGroupTable.Runtime(), s.gameplayNow())
		if parseErr != nil {
			return accountAdventureGroupState{}, parseErr
		}
		state.Projection.Runtime = runtime
	}
	return state, nil
}

func (s *Service) currentAccountAdventureGroupSummaryForPacket(ctx context.Context, session *gameSession, selected dnfrepo.CharacterRecord, hasSelected bool) adventuregroup.Summary {
	summary, err := s.currentAccountAdventureGroupSummary(ctx, selected, hasSelected, session)
	if err == nil {
		return summary
	}
	if session != nil {
		s.logGameEvent(session, "game-adventure-group-summary-unavailable",
			"character_id", selected.CharacterID,
			"error", err)
	}
	// Production Start preloads the strict PVF table. This zero result is only
	// a fail-closed wire value for a transient repository error or direct unit
	// construction that bypasses Start; it is always accompanied by an error.
	return adventuregroup.Summary{}
}
