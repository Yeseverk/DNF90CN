package joust

import (
	"errors"
	"fmt"
	"math"
	"time"
)

const RoundDuration = 2 * time.Hour

const (
	BettingDuration = 90 * time.Minute
	QuarterFinalEnd = 95 * time.Minute
	SemiFinalEnd    = 100 * time.Minute
	FinalEnd        = 105 * time.Minute
	SettlementStart = 110 * time.Minute

	// MysteryRiderStatus is the current client's explicit hidden-rider flag in
	// class-0/op1241.  The eighth entrant keeps its real PVF ID for wagering
	// and settlement, but this marker makes the initial bracket render the
	// native black "mystery knight" instead of revealing that ID up front.
	MysteryRiderStatus byte = 1
)

var ErrRoundUnavailable = errors.New("joust round is unavailable")

type BettingLedger struct {
	Round  uint16
	Knight byte
	Amount uint32
	Valid  bool
}

type OpeningRider struct {
	ID         byte
	AttackType byte
	Multiplier float32
	Wins       uint16
	Losses     uint16
	Status     byte
	Support    uint32
}

type OpeningRound struct {
	Number       uint16
	TotalSupport uint32
	Riders       []OpeningRider
}

// RoundNumberAt gives the all-day local profile a stable two-hour round key.
// Unix time is used so restart and daylight-saving changes cannot create a
// second identity for the same active round.
func RoundNumberAt(now time.Time) uint16 {
	if now.IsZero() {
		return 0
	}
	seconds := now.Unix()
	if seconds < 0 {
		seconds = 0
	}
	return uint16(uint64(seconds / int64(RoundDuration/time.Second)))
}

// Opening builds the eight-rider class-0/op1241 view. The original event odds
// are total support divided by support for that rider. A local single-account
// server has no cross-channel population to seed that pool, so each round uses
// a deterministic background baseline. It is stable across restarts, changes
// in the next round, and the durable local bet is overlaid on its rider.
func (c *Catalog) Opening(round uint16, ledger BettingLedger) (OpeningRound, error) {
	ledgers := []BettingLedger(nil)
	if ledger.Valid {
		ledgers = []BettingLedger{ledger}
	}
	return c.OpeningWithLedgers(round, ledgers)
}

// OpeningWithLedgers overlays every durable character ledger for the local
// account. Pending is intentionally not part of BettingLedger: settlement
// clears only the claim flag, while the round's public support pool and final
// multiplier must remain stable until the next two-hour opening.
func (c *Catalog) OpeningWithLedgers(round uint16, ledgers []BettingLedger) (OpeningRound, error) {
	if c == nil || len(c.riders) < OpeningKnightCount {
		return OpeningRound{}, ErrRoundUnavailable
	}
	selected, err := selectOpeningRiders(c.riders, round)
	if err != nil {
		return OpeningRound{}, err
	}
	opening := OpeningRound{
		Number: round,
		Riders: make([]OpeningRider, len(selected)),
	}
	for position, rider := range selected {
		support := openingBaselineSupport(round, position)
		for _, ledger := range ledgers {
			if ledger.Valid && ledger.Round == round && ledger.Knight == rider.ID {
				support += ledger.Amount
			}
		}
		opening.Riders[position] = OpeningRider{
			ID:         rider.ID,
			AttackType: rider.AttackType,
			Support:    support,
		}
		if position == OpeningKnightCount-1 {
			opening.Riders[position].Status = MysteryRiderStatus
		}
		opening.TotalSupport += support
	}
	if opening.TotalSupport == 0 {
		return OpeningRound{}, fmt.Errorf("%w: round=%d total support is zero", ErrRoundUnavailable, round)
	}
	for index := range opening.Riders {
		support := opening.Riders[index].Support
		if support == 0 {
			return OpeningRound{}, fmt.Errorf("%w: round=%d rider=%d support is zero", ErrRoundUnavailable, round, opening.Riders[index].ID)
		}
		multiplier := float32(opening.TotalSupport) / float32(support)
		if math.IsNaN(float64(multiplier)) || math.IsInf(float64(multiplier), 0) || multiplier <= 0 || multiplier > 999 {
			return OpeningRound{}, fmt.Errorf("%w: round=%d rider=%d multiplier=%v", ErrRoundUnavailable, round, opening.Riders[index].ID, multiplier)
		}
		opening.Riders[index].Multiplier = multiplier
	}
	return opening, nil
}

type Phase byte

const (
	PhaseBetting Phase = iota
	PhaseQuarterFinal
	PhaseSemiFinal
	PhaseFinal
	PhaseSettled
)

type Timeline struct {
	Round   uint16
	Elapsed time.Duration
	Phase   Phase
	State   byte
	Stage   byte
}

// TimelineAt maps the all-day two-hour activity into the original four state
// notices and four progressive bracket snapshots. The 90/95/100/105/110
// minute boundaries preserve the published 2017 betting and settlement
// cadence while keeping the event available around the clock.
func TimelineAt(now time.Time) Timeline {
	round := RoundNumberAt(now)
	elapsed := time.Duration(now.Unix()%int64(RoundDuration/time.Second)) * time.Second
	if elapsed < 0 {
		elapsed = 0
	}
	timeline := Timeline{Round: round, Elapsed: elapsed, State: 1}
	switch {
	case elapsed < BettingDuration:
		timeline.Phase = PhaseBetting
	case elapsed < QuarterFinalEnd:
		timeline.Phase, timeline.State, timeline.Stage = PhaseQuarterFinal, 2, 0
	case elapsed < SemiFinalEnd:
		timeline.Phase, timeline.State, timeline.Stage = PhaseSemiFinal, 2, 1
	case elapsed < FinalEnd:
		timeline.Phase, timeline.State, timeline.Stage = PhaseFinal, 3, 2
	case elapsed < SettlementStart:
		timeline.Phase, timeline.State, timeline.Stage = PhaseFinal, 3, 3
	default:
		timeline.Phase, timeline.State, timeline.Stage = PhaseSettled, 4, 3
	}
	return timeline
}

func NextBoundaryAfter(now time.Time) time.Time {
	timeline := TimelineAt(now)
	start := now.Add(-timeline.Elapsed)
	for _, offset := range []time.Duration{BettingDuration, QuarterFinalEnd, SemiFinalEnd, FinalEnd, SettlementStart, RoundDuration} {
		candidate := start.Add(offset)
		if candidate.After(now) {
			return candidate
		}
	}
	return start.Add(RoundDuration)
}

func (r OpeningRound) Contains(knight byte) bool {
	for _, rider := range r.Riders {
		if rider.ID == knight {
			return true
		}
	}
	return false
}

// openingRegularKnightIDs are the seven visible 2017 CN joust entrants:
// 理查德、爱德华、罗兰、贝奥武夫、莱奥、伊萨尔、席恩. The eighth slot is intentionally
// filled from mysteryKnightIDs for that round. The active PVF names ID 6 as
// 吉利特 and ID 9 as 无双飞将吕布; both are valid mystery entries. Lancelot (ID 8)
// is the activity-dungeon lord, never a joust entrant.
var openingRegularKnightIDs = [...]byte{1, 0, 2, 3, 4, 5, 7}

var mysteryKnightIDs = [...]byte{6, 9, 10, 11}

func selectOpeningRiders(riders []Rider, round uint16) ([]Rider, error) {
	byID := make(map[byte]Rider, len(riders))
	for _, rider := range riders {
		if _, duplicate := byID[rider.ID]; duplicate {
			return nil, fmt.Errorf("%w: duplicate roster rider=%d", ErrRoundUnavailable, rider.ID)
		}
		byID[rider.ID] = rider
	}
	selected := make([]Rider, 0, OpeningKnightCount)
	for _, id := range openingRegularKnightIDs {
		rider, found := byID[id]
		if !found {
			return nil, fmt.Errorf("%w: required original rider=%d missing", ErrRoundUnavailable, id)
		}
		selected = append(selected, cloneRider(rider))
	}
	// Keep the draw restart-stable, but choose only from the original mystery
	// pool rather than shuffling in unrelated normal/event NPCs.
	mysteryID := mysteryKnightIDs[(uint32(round)*1103515245+12345)%uint32(len(mysteryKnightIDs))]
	mystery, found := byID[mysteryID]
	if !found {
		return nil, fmt.Errorf("%w: required mystery rider=%d missing", ErrRoundUnavailable, mysteryID)
	}
	selected = append(selected, cloneRider(mystery))
	return selected, nil
}

func openingBaselineSupport(round uint16, position int) uint32 {
	// Five is coprime to eight, so all eight ranks are distinct in every round.
	rank := (position*5 + int(round)%OpeningKnightCount) % OpeningKnightCount
	// The small round jitter changes the numeric odds while preserving distinct
	// support buckets and realistic single-digit/low-double-digit multipliers.
	jitter := uint32((uint32(round)*37 + uint32(position)*17) % 101)
	return 900 + uint32(rank)*325 + jitter
}
