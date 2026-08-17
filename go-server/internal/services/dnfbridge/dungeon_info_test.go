package dnfbridge

import (
	"encoding/hex"
	"errors"
	"testing"
)

func TestCurrentDungeonInfoBuildMatchesCurrentEXEReader(t *testing.T) {
	body, err := (currentDungeonInfo{
		DungeonID: 0x05040302, Difficulty: 6, EntryOption: 0x0807,
		MazeIndex: 9, BossX: 10, BossY: 11,
		HellPartyRoomX: 12, HellPartyRoomY: 13, DungeonMode: 14,
		PairGroups:     [][]currentDungeonInfoPair{{{First: 17, Second: 18}}},
		HellPartyValue: 0x1413, DungeonValue: 0x1615, Value2: 23, FlagA: 24,
		PacketSeed: 0x1c1b1a19, ParamA: 29, ParamB: 30, ParamC: 31,
		TailFlag0: 32, TailFlag1: 33, TailFlag2: 34,
		OpaqueEntries: []currentDungeonInfoOpaqueEntry{{Value: 0x27262524, ParamA: 40, ParamB: 41}},
		TailMode:      42, TailValue0: 0x2c2b, TailValue1: 0x2e2d,
	}).Build()
	if err != nil {
		t.Fatal(err)
	}
	const wantHex = "02030405060708090a0b0c0d0e01011112131415161718191a1b1c1d1e1f202122012425262728292a2b2c2d2e"
	if got := hex.EncodeToString(body); got != wantHex {
		t.Fatalf("body=%s want=%s", got, wantHex)
	}
}

func TestCurrentDungeonInfoBuildMinimumIsCurrentEXEWidth(t *testing.T) {
	body, err := (currentDungeonInfo{}).Build()
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != 36 {
		t.Fatalf("body len=%d want=36 body=%x", len(body), body)
	}
}

func TestCurrentDungeonInfoBuildRejectsCountOverflow(t *testing.T) {
	tests := []struct {
		name   string
		packet currentDungeonInfo
		want   error
	}{
		{name: "groups", packet: currentDungeonInfo{PairGroups: make([][]currentDungeonInfoPair, 256)}, want: errDungeonInfoPairGroupCount},
		{name: "pairs", packet: currentDungeonInfo{PairGroups: [][]currentDungeonInfoPair{make([]currentDungeonInfoPair, 256)}}, want: errDungeonInfoPairGroupCount},
		{name: "opaque", packet: currentDungeonInfo{OpaqueEntries: make([]currentDungeonInfoOpaqueEntry, 256)}, want: errDungeonInfoOpaqueCount},
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
