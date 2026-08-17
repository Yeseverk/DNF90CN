package dnfbridge

import (
	"context"
	"testing"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestCurrentJoustOpeningCountersUseOnlyPersistedSettlements(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	catalog := mustTestJoustEventCatalog(t)
	const settledRound = uint16(4320)
	tournament, err := catalog.TournamentFor(settledRound)
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{
		options:            options{accountID: "account-1"},
		joustCatalog:       catalog,
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	if err := service.persistCurrentJoustHistoryRecord(ctx, "account-1", currentJoustHistoryRecord{
		Round: settledRound, Winner: tournament.Champion(), Multiplier: 3.5,
	}, time.Unix(7200*4321, 0).UTC()); err != nil {
		t.Fatal(err)
	}
	opening, err := service.currentJoustOpeningForRound(ctx, catalog, "account-1", 4321)
	if err != nil {
		t.Fatal(err)
	}
	want := make(map[byte][2]uint16)
	for _, match := range tournament.Matches {
		wins := want[match.Winner]
		wins[0]++
		want[match.Winner] = wins
		losses := want[match.Loser]
		losses[1]++
		want[match.Loser] = losses
	}
	for _, rider := range opening.Riders {
		if got := [2]uint16{rider.Wins, rider.Losses}; got != want[rider.ID] {
			t.Fatalf("rider=%d record=%v want=%v", rider.ID, got, want[rider.ID])
		}
	}
}

func TestCurrentJoustLegacyIncompatibleSettlementIsIgnoredWithoutBlockingLogin(t *testing.T) {
	ctx := context.Background()
	repositories := dnfrepomemory.NewMemoryGroup()
	catalog := mustTestJoustEventCatalog(t)
	const legacyRound = uint16(4320)
	tournament, err := catalog.TournamentFor(legacyRound)
	if err != nil {
		t.Fatal(err)
	}
	legacyWinner := byte(0)
	if legacyWinner == tournament.Champion() {
		legacyWinner = 1
	}
	service := &Service{
		options:            options{accountID: "account-1"},
		joustCatalog:       catalog,
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	if err := service.persistCurrentJoustHistoryRecord(ctx, "account-1", currentJoustHistoryRecord{
		Round: legacyRound, Winner: legacyWinner, Multiplier: 3.5,
	}, time.Unix(7200*4321, 0).UTC()); err != nil {
		t.Fatal(err)
	}
	opening, err := service.currentJoustOpeningForRound(ctx, catalog, "account-1", legacyRound+1)
	if err != nil {
		t.Fatalf("stale history must not block opening: %v", err)
	}
	for _, rider := range opening.Riders {
		if rider.Wins != 0 || rider.Losses != 0 {
			t.Fatalf("legacy mismatched row counted for rider=%d: wins=%d losses=%d", rider.ID, rider.Wins, rider.Losses)
		}
	}
	records, err := service.currentJoustHistory(ctx, nil, time.Unix(7200*4322, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Fatalf("legacy mismatched history was displayed: %+v", records)
	}
}
