package dnfbridge

import (
	"testing"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

func TestCurrentDungeonIsRecommendedClearUsesPVFRangeAndAcceptedOrdinaryClear(t *testing.T) {
	runtime := &runtimeDungeonState{
		Dungeon: worldmap.Dungeon{
			ID: 11,
			Metadata: worldmap.DungeonMetadata{
				RecommendedLevels: []int64{85, 90},
			},
		},
		Character:                      dnfrepo.CharacterRecord{Level: 90},
		ordinaryFinalRoomClearAccepted: true,
	}
	if !currentDungeonIsRecommendedClear(runtime) {
		t.Fatal("accepted level-90 clear in PVF range was not recommended")
	}
	runtime.Character.Level = 91
	if currentDungeonIsRecommendedClear(runtime) {
		t.Fatal("out-of-range clear was recommended")
	}
	runtime.Character.Level = 90
	runtime.ordinaryFinalRoomClearAccepted = false
	if currentDungeonIsRecommendedClear(runtime) {
		t.Fatal("unaccepted clear was recommended")
	}
}

func TestCurrentDungeonMatchesRecommendedLevelRejectsMalformedPVFRange(t *testing.T) {
	runtime := &runtimeDungeonState{
		Dungeon: worldmap.Dungeon{
			ID: 11,
			Metadata: worldmap.DungeonMetadata{
				RecommendedLevels: []int64{90, 85},
			},
		},
		Character: dnfrepo.CharacterRecord{Level: 90},
	}
	if currentDungeonMatchesRecommendedLevel(runtime) {
		t.Fatal("reversed PVF range matched")
	}
	runtime.Dungeon.Metadata.RecommendedLevels = []int64{90}
	if currentDungeonMatchesRecommendedLevel(runtime) {
		t.Fatal("incomplete PVF range matched")
	}
}
