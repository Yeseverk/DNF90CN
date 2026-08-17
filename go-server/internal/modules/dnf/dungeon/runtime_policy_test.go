package dungeon

import (
	"errors"
	"testing"

	"longheng.io/server/internal/modules/dnf/dungeoncmd"
)

func TestValidateEntry(t *testing.T) {
	base := EntryPolicy{
		CharacterLevel:  20,
		MinimumLevel:    10,
		MinimumLevelSet: true,
		PartyCount:      1,
		PartyLimit:      4,
		PartyLimitSet:   true,
		Fatigue:         100,
		FatigueKnown:    true,
	}
	tests := []struct {
		name    string
		mutate  func(*EntryPolicy)
		wantErr error
	}{
		{name: "accepted"},
		{
			name: "minimum level",
			mutate: func(policy *EntryPolicy) {
				policy.CharacterLevel = 9
			},
			wantErr: ErrEntryLevelTooLow,
		},
		{
			name: "party limit",
			mutate: func(policy *EntryPolicy) {
				policy.PartyCount = 5
			},
			wantErr: ErrEntryPartyLimit,
		},
		{
			name: "fatigue unavailable",
			mutate: func(policy *EntryPolicy) {
				policy.FatigueKnown = false
			},
			wantErr: ErrEntryFatigueUnknown,
		},
		{
			name: "fatigue exhausted",
			mutate: func(policy *EntryPolicy) {
				policy.Fatigue = 0
			},
			wantErr: ErrEntryFatigueExhausted,
		},
		{
			name: "PVF no fatigue",
			mutate: func(policy *EntryPolicy) {
				policy.FatigueKnown = false
				policy.NoFatigue = true
			},
		},
		{
			name: "PVF enter without fatigue",
			mutate: func(policy *EntryPolicy) {
				policy.FatigueKnown = false
				policy.EnterWithoutFatigue = true
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			policy := base
			if test.mutate != nil {
				test.mutate(&policy)
			}
			err := ValidateEntry(policy)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error=%v want=%v", err, test.wantErr)
			}
		})
	}
}

func TestSameSelectRequest(t *testing.T) {
	base := dungeoncmd.SelectDungeonRequest{
		DungeonID:       700,
		Difficulty:      2,
		EntryOption:     3,
		SelectionMode:   4,
		RuntimeState:    5,
		RuntimeToken:    6,
		Reserved:        7,
		PartyState:      8,
		LeaderObjectKey: 9,
		SpecialMode:     10,
	}
	if !SameSelectRequest(base, base) {
		t.Fatal("equal proved fields were not replay-compatible")
	}
	changed := base
	changed.RuntimeToken++
	if SameSelectRequest(base, changed) {
		t.Fatal("different proved fields were replay-compatible")
	}
	withTail := base
	withTail.OpaqueTail = []byte{1}
	if SameSelectRequest(base, withTail) {
		t.Fatal("opaque tail was accepted without current-client evidence")
	}
}

func TestRuntimeOwnsCharacter(t *testing.T) {
	if !RuntimeOwnsCharacter(" 99 ", 99) {
		t.Fatal("stored character did not own its selected runtime")
	}
	if RuntimeOwnsCharacter("99", 0) || RuntimeOwnsCharacter("98", 99) {
		t.Fatal("invalid selected character owned the runtime")
	}
}
