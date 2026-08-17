package joust

import (
	"context"
	"math"
	"reflect"
	"testing"
	"time"
)

func TestRoundNumberAtUsesStableTwoHourWindows(t *testing.T) {
	start := time.Unix(7200*1234, 0)
	if got := RoundNumberAt(start); got != 1234 {
		t.Fatalf("round=%d want=1234", got)
	}
	if got := RoundNumberAt(start.Add(RoundDuration - time.Second)); got != 1234 {
		t.Fatalf("same-window round=%d want=1234", got)
	}
	if got := RoundNumberAt(start.Add(RoundDuration)); got != 1235 {
		t.Fatalf("next-window round=%d want=1235", got)
	}
}

func TestOpeningHasEightDistinctNonZeroMultipliersAndChangesNextRound(t *testing.T) {
	catalog, err := LoadCatalog(context.Background(), joustCatalogTestSource{EventPVFPath: testJoustPVF(12)})
	if err != nil {
		t.Fatal(err)
	}
	first, err := catalog.Opening(77, BettingLedger{})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Riders) != OpeningKnightCount || first.TotalSupport == 0 {
		t.Fatalf("opening=%+v", first)
	}
	ids := make(map[byte]struct{}, OpeningKnightCount)
	multipliers := make(map[uint32]struct{}, OpeningKnightCount)
	for _, rider := range first.Riders {
		if rider.Multiplier <= 0 || rider.Support == 0 {
			t.Fatalf("rider=%+v", rider)
		}
		ids[rider.ID] = struct{}{}
		multipliers[math.Float32bits(rider.Multiplier)] = struct{}{}
	}
	if len(ids) != OpeningKnightCount || len(multipliers) != OpeningKnightCount {
		t.Fatalf("distinct ids=%d multipliers=%d opening=%+v", len(ids), len(multipliers), first)
	}
	second, err := catalog.Opening(78, BettingLedger{})
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(first, second) {
		t.Fatalf("next round did not change: %+v", second)
	}
}

func TestOpeningOverlaysDurableLocalBetAndRecalculatesAllOdds(t *testing.T) {
	catalog, err := LoadCatalog(context.Background(), joustCatalogTestSource{EventPVFPath: testJoustPVF(12)})
	if err != nil {
		t.Fatal(err)
	}
	base, err := catalog.Opening(91, BettingLedger{})
	if err != nil {
		t.Fatal(err)
	}
	target := base.Riders[3]
	updated, err := catalog.Opening(91, BettingLedger{Round: 91, Knight: target.ID, Amount: 500, Valid: true})
	if err != nil {
		t.Fatal(err)
	}
	if updated.TotalSupport != base.TotalSupport+500 {
		t.Fatalf("total=%d want=%d", updated.TotalSupport, base.TotalSupport+500)
	}
	for index := range base.Riders {
		wantSupport := base.Riders[index].Support
		if base.Riders[index].ID == target.ID {
			wantSupport += 500
		}
		if updated.Riders[index].Support != wantSupport || updated.Riders[index].Multiplier == base.Riders[index].Multiplier {
			t.Fatalf("rider[%d] base=%+v updated=%+v", index, base.Riders[index], updated.Riders[index])
		}
	}
}

func TestOpeningUsesSevenRegularKnightsAndOnlyAnEligibleMysteryKnight(t *testing.T) {
	catalog, err := LoadCatalog(context.Background(), joustCatalogTestSource{EventPVFPath: testJoustPVF(12)})
	if err != nil {
		t.Fatal(err)
	}
	regular := []byte{1, 0, 2, 3, 4, 5, 7}
	mysteries := map[byte]struct{}{6: {}, 9: {}, 10: {}, 11: {}}
	seenMysteries := make(map[byte]struct{}, len(mysteries))
	for round := uint16(0); round < 16; round++ {
		opening, openingErr := catalog.Opening(round, BettingLedger{})
		if openingErr != nil {
			t.Fatalf("round=%d: %v", round, openingErr)
		}
		for index, id := range regular {
			if opening.Riders[index].ID != id || opening.Riders[index].Status != 0 {
				t.Fatalf("round=%d regular[%d]=%+v want_id=%d visible", round, index, opening.Riders[index], id)
			}
		}
		mysteryRider := opening.Riders[OpeningKnightCount-1]
		mystery := mysteryRider.ID
		if _, allowed := mysteries[mystery]; !allowed {
			t.Fatalf("round=%d mystery=%d is not an original mystery rider", round, mystery)
		}
		if mysteryRider.Status != MysteryRiderStatus {
			t.Fatalf("round=%d mystery=%+v missing hidden status=%d", round, mysteryRider, MysteryRiderStatus)
		}
		seenMysteries[mystery] = struct{}{}
	}
	if len(seenMysteries) != len(mysteries) {
		t.Fatalf("seen mystery riders=%v want=%v", seenMysteries, mysteries)
	}
}
