package dnfbridge

import (
	"encoding/hex"
	"errors"
	"testing"
)

func TestCurrentDungeonContextBuildMinimumMatchesCurrentEXEReader(t *testing.T) {
	body, err := (currentDungeonContext{}).Build()
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != 37 {
		t.Fatalf("op27 minimum body len=%d want=37 body=%x", len(body), body)
	}
	if got := hex.EncodeToString(body); got != "00000000000000000000000000000000000000000000000000000000000000000000000000" {
		t.Fatalf("op27 minimum body=%s", got)
	}
}

func TestCurrentDungeonContextBuildMatchesCurrentEXEReader(t *testing.T) {
	body, err := (currentDungeonContext{
		Value0:               1,
		Value1:               2,
		List0:                []uint16{3},
		List1:                []uint16{4},
		Groups:               []currentDungeonContextGroup{{ObjectOrActorKey: 5, Rows: []currentDungeonContextGroupRow{{ValueA: 6, ValueB: 7, ValueC: 8}}}},
		ContextValue0:        9,
		ContextValue1:        10,
		ContextValue2:        11,
		ContextValue3:        12,
		OptionalTablePresent: true,
		OptionalRows:         []currentDungeonContextOptionalRow{{Key: 13, Value0: 14, Value1: 15}},
		BooleanLikeValue:     1,
		LargeContextValue0:   16,
		LargeContextValue1:   17,
		BoundedMode:          7,
		CappedValue:          100,
		Pairs:                []currentDungeonContextPair{{Key: 18, Value: 19}},
	}).Build()
	if err != nil {
		t.Fatal(err)
	}
	const want = "010000000200000001030001040001050001060000000700000008000000090000000a000b000c00000001010d0000000e000f00011000000011000000076400011200000013000000"
	if got := hex.EncodeToString(body); got != want {
		t.Fatalf("op27 body=%s want=%s", got, want)
	}
}

func TestCurrentDungeonContextBuildRejectsUnsafeRanges(t *testing.T) {
	tests := []struct {
		name   string
		packet currentDungeonContext
		want   error
	}{
		{name: "first list", packet: currentDungeonContext{List0: make([]uint16, 9)}, want: errDungeonContextListCount},
		{name: "second list", packet: currentDungeonContext{List1: make([]uint16, 9)}, want: errDungeonContextListCount},
		{name: "optional rows without flag", packet: currentDungeonContext{OptionalRows: []currentDungeonContextOptionalRow{{}}}, want: errDungeonContextOptionalCount},
		{name: "bounded mode", packet: currentDungeonContext{BoundedMode: 8}, want: errDungeonContextBoundedMode},
		{name: "capped value", packet: currentDungeonContext{CappedValue: 101}, want: errDungeonContextCappedValue},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := test.packet.Build()
			if !errors.Is(err, test.want) {
				t.Fatalf("error=%v want=%v", err, test.want)
			}
		})
	}
}
