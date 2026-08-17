package dungeon

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"longheng.io/server/internal/modules/dnf/dungeoncmd"
)

var (
	ErrEntryLevelTooLow      = errors.New("dnf dungeon character level is too low")
	ErrEntryFatigueUnknown   = errors.New("dnf dungeon character fatigue is unavailable")
	ErrEntryFatigueExhausted = errors.New("dnf dungeon character fatigue is exhausted")
	ErrEntryPartyLimit       = errors.New("dnf dungeon party exceeds PVF limit")
)

// EntryPolicy contains only the proved PVF and persisted-character facts used
// to decide whether a character may enter a dungeon. Transport/session code is
// responsible for taking a stable party snapshot before calling ValidateEntry.
type EntryPolicy struct {
	CharacterLevel      int
	MinimumLevel        int64
	MinimumLevelSet     bool
	PartyCount          int
	PartyLimit          int64
	PartyLimitSet       bool
	NoFatigue           bool
	EnterWithoutFatigue bool
	Fatigue             int64
	FatigueKnown        bool
}

func ValidateEntry(policy EntryPolicy) error {
	if policy.MinimumLevelSet && int64(policy.CharacterLevel) < policy.MinimumLevel {
		return fmt.Errorf(
			"%w: level=%d required=%d",
			ErrEntryLevelTooLow,
			policy.CharacterLevel,
			policy.MinimumLevel,
		)
	}
	partyCount := policy.PartyCount
	if partyCount < 1 {
		partyCount = 1
	}
	if policy.PartyLimitSet && policy.PartyLimit > 0 && int64(partyCount) > policy.PartyLimit {
		return fmt.Errorf("%w: members=%d limit=%d", ErrEntryPartyLimit, partyCount, policy.PartyLimit)
	}
	if policy.NoFatigue || policy.EnterWithoutFatigue {
		return nil
	}
	if !policy.FatigueKnown {
		return ErrEntryFatigueUnknown
	}
	if policy.Fatigue <= 0 {
		return fmt.Errorf("%w: fatigue=%d", ErrEntryFatigueExhausted, policy.Fatigue)
	}
	return nil
}

func CharacterStat(stats map[string]int64, names ...string) (int64, bool) {
	for _, name := range names {
		if value, ok := stats[name]; ok {
			return value, true
		}
	}
	return 0, false
}

// SameSelectRequest defines idempotent current-client op16 replay identity.
// Opaque tails are deliberately excluded until current-EXE evidence proves
// their semantics.
func SameSelectRequest(left, right dungeoncmd.SelectDungeonRequest) bool {
	return left.DungeonID == right.DungeonID &&
		left.Difficulty == right.Difficulty &&
		left.EntryOption == right.EntryOption &&
		left.SelectionMode == right.SelectionMode &&
		left.RuntimeState == right.RuntimeState &&
		left.RuntimeToken == right.RuntimeToken &&
		left.Reserved == right.Reserved &&
		left.PartyState == right.PartyState &&
		left.LeaderObjectKey == right.LeaderObjectKey &&
		left.SpecialMode == right.SpecialMode &&
		len(left.OpaqueTail) == 0 && len(right.OpaqueTail) == 0
}

func RuntimeOwnsCharacter(storedCharacterID string, selectedCharacterID uint16) bool {
	return selectedCharacterID != 0 &&
		strings.TrimSpace(storedCharacterID) == strconv.Itoa(int(selectedCharacterID))
}
