package dnfbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	dnfjoust "longheng.io/server/internal/modules/dnf/joust"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const currentJoustHistorySettingsKey = "completed_rounds_v1"

type currentJoustHistoryRecord struct {
	Round      uint16  `json:"round"`
	Winner     byte    `json:"winner"`
	Multiplier float32 `json:"multiplier"`
}

func currentJoustHistoryScope(accountID string) string {
	return "account:" + strings.TrimSpace(accountID) + ":joust"
}

func (service *Service) currentJoustHistory(
	ctx context.Context,
	session *gameSession,
	now time.Time,
) ([]currentJoustHistoryRecord, error) {
	if service == nil {
		return nil, dnfjoust.ErrOwnerUnavailable
	}
	overrides, err := service.loadCurrentJoustHistoryOverrides(ctx, service.accountIDForSession(session))
	if err != nil {
		return nil, err
	}
	catalog, err := service.currentJoustCatalog(ctx)
	if err != nil {
		return nil, err
	}
	timeline := dnfjoust.TimelineAt(now)
	latest := timeline.Round - 1
	if timeline.Phase == dnfjoust.PhaseSettled {
		latest = timeline.Round
	}
	records := make([]currentJoustHistoryRecord, 0, len(overrides))
	for round, record := range overrides {
		if round == 0 || round > latest {
			continue
		}
		// History written by older, non-original brackets is retained in the
		// database for recovery, but cannot be shown as an actual result after
		// the original roster is restored.  Never let such a legacy row enter
		// the current client's visible history.
		tournament, tournamentErr := catalog.TournamentFor(round)
		if tournamentErr != nil || tournament.Champion() != record.Winner {
			continue
		}
		records = append(records, record)
	}
	sort.Slice(records, func(left, right int) bool {
		return uint16(latest-records[left].Round) < uint16(latest-records[right].Round)
	})
	if len(records) > currentJoustHistoryRecordCount {
		records = records[:currentJoustHistoryRecordCount]
	}
	return records, nil
}

// currentJoustRecordedWinLoss projects only durable, actually settled rounds.
// Never derive counters from future or simulated rounds: a fresh activity must
// show 0 wins / 0 losses until a real tournament reaches settlement.
func (service *Service) currentJoustRecordedWinLoss(
	ctx context.Context,
	accountID string,
	catalog *dnfjoust.Catalog,
) (map[byte][2]uint16, error) {
	if catalog == nil {
		return nil, dnfjoust.ErrRoundUnavailable
	}
	records, err := service.loadCurrentJoustHistoryOverrides(ctx, accountID)
	if err != nil {
		return nil, err
	}
	counters := make(map[byte][2]uint16, len(catalog.Riders()))
	for round, record := range records {
		tournament, tournamentErr := catalog.TournamentFor(round)
		if tournamentErr != nil {
			continue
		}
		if tournament.Champion() != record.Winner {
			// Do not turn a stale pre-restoration row into a login failure.  It is
			// deliberately ignored until a settlement matches the active, original
			// bracket in full.
			continue
		}
		for _, match := range tournament.Matches {
			winner := counters[match.Winner]
			winner[0]++
			counters[match.Winner] = winner
			loser := counters[match.Loser]
			loser[1]++
			counters[match.Loser] = loser
		}
	}
	return counters, nil
}

func currentJoustWinnerMultiplier(opening dnfjoust.OpeningRound, winner byte) (float32, bool) {
	for _, rider := range opening.Riders {
		if rider.ID == winner && rider.Multiplier > 0 && !math.IsNaN(float64(rider.Multiplier)) && !math.IsInf(float64(rider.Multiplier), 0) {
			return rider.Multiplier, true
		}
	}
	return 0, false
}

func (service *Service) loadCurrentJoustHistoryOverrides(
	ctx context.Context,
	accountID string,
) (map[uint16]currentJoustHistoryRecord, error) {
	result := make(map[uint16]currentJoustHistoryRecord)
	if service == nil {
		return result, nil
	}
	repositories, ok := service.repositoryGroup()
	if !ok || repositories.Settings == nil {
		return result, nil
	}
	record, found, err := repositories.Settings.Load(ctx, currentJoustHistoryScope(accountID))
	if err != nil || !found || record.Values == nil || strings.TrimSpace(record.Values[currentJoustHistorySettingsKey]) == "" {
		return result, err
	}
	var stored []currentJoustHistoryRecord
	if err := json.Unmarshal([]byte(record.Values[currentJoustHistorySettingsKey]), &stored); err != nil {
		return nil, fmt.Errorf("decode joust history: %w", err)
	}
	for _, item := range stored {
		if item.Round != 0 && item.Multiplier > 0 && !math.IsNaN(float64(item.Multiplier)) && !math.IsInf(float64(item.Multiplier), 0) {
			result[item.Round] = item
		}
	}
	return result, nil
}

func (service *Service) persistCurrentJoustHistoryRecord(
	ctx context.Context,
	accountID string,
	completed currentJoustHistoryRecord,
	now time.Time,
) error {
	if service == nil || completed.Round == 0 || completed.Multiplier <= 0 {
		return dnfjoust.ErrSettlementInvalid
	}
	repositories, ok := service.repositoryGroup()
	if !ok || repositories.Settings == nil {
		return errors.New("joust history settings repository unavailable")
	}
	service.joustHistoryMu.Lock()
	defer service.joustHistoryMu.Unlock()
	scope := currentJoustHistoryScope(accountID)
	record, found, err := repositories.Settings.Load(ctx, scope)
	if err != nil {
		return err
	}
	if !found {
		record = dnfrepo.SettingsRecord{Scope: scope, Values: make(map[string]string)}
	} else {
		record = dnfrepo.CloneSettings(record)
		if record.Values == nil {
			record.Values = make(map[string]string)
		}
	}
	stored := make([]currentJoustHistoryRecord, 0, currentJoustHistoryRecordCount)
	if raw := strings.TrimSpace(record.Values[currentJoustHistorySettingsKey]); raw != "" {
		if err := json.Unmarshal([]byte(raw), &stored); err != nil {
			return fmt.Errorf("decode joust history: %w", err)
		}
	}
	updated := make([]currentJoustHistoryRecord, 0, currentJoustHistoryRecordCount)
	updated = append(updated, completed)
	for _, item := range stored {
		if item.Round != completed.Round && len(updated) < currentJoustHistoryRecordCount {
			updated = append(updated, item)
		}
	}
	encoded, err := json.Marshal(updated)
	if err != nil {
		return err
	}
	record.Values[currentJoustHistorySettingsKey] = string(encoded)
	if now.IsZero() {
		now = time.Now().UTC()
	}
	record.UpdatedAt = now.UTC()
	return dnfrepo.SaveSettingsFields(ctx, repositories.Settings, record, dnfrepo.SettingsFieldValues)
}
