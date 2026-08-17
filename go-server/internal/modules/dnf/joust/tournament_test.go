package joust

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestTournamentForBuildsStableCompleteBracket(t *testing.T) {
	catalog, err := LoadCatalog(context.Background(), joustCatalogTestSource{EventPVFPath: testJoustPVF(12)})
	if err != nil {
		t.Fatal(err)
	}
	first, err := catalog.TournamentFor(91)
	if err != nil {
		t.Fatal(err)
	}
	second, err := catalog.TournamentFor(91)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) || first.Champion() != first.Matches[6].Winner {
		t.Fatalf("unstable tournament first=%+v second=%+v", first, second)
	}
	for index, match := range first.Matches {
		if match.Winner == match.Loser || match.WinnerAction > 3 || match.LoserAction > 3 {
			t.Fatalf("match[%d]=%+v", index, match)
		}
	}
	if first.Matches[4].Winner != first.Matches[6].Winner && first.Matches[5].Winner != first.Matches[6].Winner {
		t.Fatalf("final champion not a semifinal winner: %+v", first.Matches)
	}
}

func TestBattleActionUsesOpponentAttackTypeProfileKey(t *testing.T) {
	values := []uint16{
		0, 20, 35, 65, 75, 95, 100,
		0, 15, 20, 50, 80, 100, 100,
		0, 35, 60, 80, 80, 90, 100,
		0, 20, 35, 55, 75, 90, 100,
	}
	for _, test := range []struct {
		attackType byte
		want       byte
	}{{0, 0}, {1, 1}, {27, 2}, {28, 3}} {
		got, err := battleAction(values, test.attackType)
		if err != nil || got != test.want {
			t.Fatalf("attack_type=%d action=%d err=%v want=%d", test.attackType, got, err, test.want)
		}
	}
	if _, err := battleAction(values, 2); err == nil {
		t.Fatal("unsupported attack type did not fail")
	}
}

func TestTournamentActionsMatchEachOpponentAttackType(t *testing.T) {
	catalog, err := LoadCatalog(context.Background(), joustCatalogTestSource{EventPVFPath: testJoustPVF(12)})
	if err != nil {
		t.Fatal(err)
	}
	for round := uint16(0); ; round++ {
		tournament, tournamentErr := catalog.TournamentFor(round)
		if tournamentErr != nil {
			t.Fatalf("round=%d: %v", round, tournamentErr)
		}
		for index, match := range tournament.Matches {
			winner, winnerOK := catalog.rider(match.Winner)
			loser, loserOK := catalog.rider(match.Loser)
			winnerOffset, winnerTypeOK := attackTypeTableOffset(loser.AttackType)
			loserOffset, loserTypeOK := attackTypeTableOffset(winner.AttackType)
			if !winnerOK || !loserOK || !winnerTypeOK || !loserTypeOK ||
				match.WinnerAction != byte(winnerOffset/7) || match.LoserAction != byte(loserOffset/7) {
				t.Fatalf("round=%d match[%d]=%+v winner=%+v loser=%+v", round, index, match, winner, loser)
			}
		}
		if round == ^uint16(0) {
			break
		}
	}
}

func TestTimelineAtUsesBettingBracketAndSettlementBoundaries(t *testing.T) {
	start := time.Unix(7200*900, 0).UTC()
	tests := []struct {
		offset time.Duration
		phase  Phase
		state  byte
		stage  byte
	}{
		{0, PhaseBetting, 1, 0},
		{90 * time.Minute, PhaseQuarterFinal, 2, 0},
		{95 * time.Minute, PhaseSemiFinal, 2, 1},
		{100 * time.Minute, PhaseFinal, 3, 2},
		{105 * time.Minute, PhaseFinal, 3, 3},
		{110 * time.Minute, PhaseSettled, 4, 3},
	}
	for _, test := range tests {
		got := TimelineAt(start.Add(test.offset))
		if got.Round != 900 || got.Phase != test.phase || got.State != test.state || got.Stage != test.stage {
			t.Fatalf("offset=%s timeline=%+v", test.offset, got)
		}
	}
}
