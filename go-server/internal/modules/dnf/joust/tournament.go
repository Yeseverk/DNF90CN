package joust

import (
	"fmt"
	"math"
)

type Match struct {
	Winner       byte
	WinnerAction byte
	Loser        byte
	LoserAction  byte
}

type Tournament struct {
	Round   uint16
	Matches [7]Match
}

func (t Tournament) Champion() byte {
	return t.Matches[6].Winner
}

// TournamentFor deterministically completes the four quarter-finals, two
// semi-finals and final. The PVF supplies four client battle timeline profiles
// per rider but no rider strength or win-probability field, so winner selection
// is an unbiased deterministic coin. This makes restarts reproduce the same
// bracket without inventing a hidden ranking.
func (c *Catalog) TournamentFor(round uint16) (Tournament, error) {
	if c == nil || len(c.riders) < OpeningKnightCount {
		return Tournament{}, ErrRoundUnavailable
	}
	roster, err := selectOpeningRiders(c.riders, round)
	if err != nil {
		return Tournament{}, err
	}
	tournament := Tournament{Round: round}
	participants := make([]byte, OpeningKnightCount)
	for index := range roster {
		participants[index] = roster[index].ID
	}
	matchIndex := 0
	next := make([]byte, 0, 4)
	for index := 0; index < len(participants); index += 2 {
		match, err := c.simulateMatch(round, matchIndex, participants[index], participants[index+1])
		if err != nil {
			return Tournament{}, err
		}
		tournament.Matches[matchIndex] = match
		next = append(next, match.Winner)
		matchIndex++
	}
	semi := make([]byte, 0, 2)
	for index := 0; index < len(next); index += 2 {
		match, err := c.simulateMatch(round, matchIndex, next[index], next[index+1])
		if err != nil {
			return Tournament{}, err
		}
		tournament.Matches[matchIndex] = match
		semi = append(semi, match.Winner)
		matchIndex++
	}
	final, err := c.simulateMatch(round, matchIndex, semi[0], semi[1])
	if err != nil {
		return Tournament{}, err
	}
	tournament.Matches[matchIndex] = final
	return tournament, nil
}

func (c *Catalog) simulateMatch(round uint16, index int, leftID, rightID byte) (Match, error) {
	left, leftOK := c.rider(leftID)
	right, rightOK := c.rider(rightID)
	if !leftOK || !rightOK || left.ID == right.ID {
		return Match{}, fmt.Errorf("%w: match=%d riders=%d/%d", ErrRoundUnavailable, index, leftID, rightID)
	}
	state := tournamentRandomState(round, index, leftID, rightID)
	winner, loser := left, right
	if nextTournamentRandom(&state)&1 != 0 {
		winner, loser = right, left
	}
	winnerAction, err := battleAction(winner.Win, loser.AttackType)
	if err != nil {
		return Match{}, err
	}
	loserAction, err := battleAction(loser.Loss, winner.AttackType)
	if err != nil {
		return Match{}, err
	}
	return Match{Winner: winner.ID, WinnerAction: winnerAction, Loser: loser.ID, LoserAction: loserAction}, nil
}

// battleAction returns the profile key consumed by the current EXE's
// sub_365C180 nested map lookup. Each rider's [win]/[loss] section is four
// seven-value timelines selected by the opponent's attack type. The values are
// timeline percentages, not seven independently selectable action IDs.
// Returning a value from a CDF roll here used to emit keys 5 and 6; the client
// inserted an empty vector for those absent keys and then deliberately aborted
// in sub_F2EE20 when it compared the two match timelines.
func battleAction(values []uint16, opponentAttackType byte) (byte, error) {
	offset, ok := attackTypeTableOffset(opponentAttackType)
	if !ok || len(values) != 28 {
		return 0, ErrPVFMalformed
	}
	if values[offset+6] == 0 {
		return 0, ErrPVFMalformed
	}
	return byte(offset / 7), nil
}

func tournamentRandomState(round uint16, index int, left, right byte) uint32 {
	state := uint32(round)*0x9E3779B9 ^ uint32(index+1)*0x85EBCA6B ^ uint32(left)<<8 ^ uint32(right)
	if state == 0 {
		state = math.MaxUint32
	}
	return state
}

func nextTournamentRandom(state *uint32) uint32 {
	*state ^= *state << 13
	*state ^= *state >> 17
	*state ^= *state << 5
	return *state
}
