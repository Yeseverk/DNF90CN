package worldmap

import (
	"context"
	"os"
	"strings"
	"testing"

	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestProductionTrainingDungeon5000ResolvesOwnedStartMap(t *testing.T) {
	pvfPath := os.Getenv("DNF_WORLDMAP_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("DNF_WORLDMAP_REAL_PVF_SMOKE is not set")
	}
	archive, err := platformpvf.LoadArchive(platformpvf.Options{Path: pvfPath})
	if err != nil {
		t.Fatal(err)
	}
	table, err := LoadSource(context.Background(), archive, Options{})
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := NewResolver(table)
	if err != nil {
		t.Fatal(err)
	}

	topology, err := BuildDungeonLayout(resolver, 5000, 0, nil)
	if err != nil {
		t.Fatalf("build production training-room layout: %v", err)
	}
	room, ok := topology.Room(RoomCoordinate{X: 0, Y: 0})
	if !ok || !room.Start || !room.Boss || room.Map == nil {
		t.Fatalf("production training-room start/boss room = %+v ok=%v", room, ok)
	}
	if room.Map.Map.ID != 36250 ||
		!strings.EqualFold(room.Map.Map.Path, "map/PoongjinTrainingRoom/11100(0,0)start.map") ||
		room.Map.Source != ResolutionDungeonOwnership {
		t.Fatalf("production training-room map = %+v", room.Map)
	}
	if len(topology.UnresolvedRooms()) != 0 || len(topology.resolutionErrors) != 0 {
		t.Fatalf("layout unresolved = %+v errors=%v", topology.UnresolvedRooms(), topology.resolutionErrors)
	}
}
