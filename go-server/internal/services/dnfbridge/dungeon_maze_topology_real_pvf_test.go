package dnfbridge

import (
	"context"
	"os"
	"testing"

	"longheng.io/server/internal/modules/dnf/worldmap"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestRealScriptPVFDungeon3Quest3145StartRoomHasResolvedExit(t *testing.T) {
	pvfPath := os.Getenv("DNF_WORLDMAP_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNF_WORLDMAP_REAL_PVF_SMOKE to run real dungeon topology smoke")
	}
	archive, err := platformpvf.Open(pvfPath)
	if err != nil {
		t.Fatal(err)
	}
	table, err := worldmap.LoadSource(context.Background(), archive, worldmap.Options{})
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := worldmap.NewResolver(table)
	if err != nil {
		t.Fatal(err)
	}
	dungeon, ok := table.FindDungeon(3)
	if !ok {
		t.Fatal("real PVF dungeon 3 is missing")
	}
	mazeIndex := -1
	for index, maze := range dungeon.Mazes {
		connection := maze.QuestConnection
		if len(connection) >= 2 && connection[0] == 0 && connection[1] == 3145 {
			mazeIndex = index
			break
		}
	}
	if mazeIndex < 0 {
		t.Fatal("real PVF dungeon 3 has no difficulty-0 quest-3145 maze")
	}
	topology, err := worldmap.BuildDungeonLayout(
		resolver,
		dungeon.ID,
		mazeIndex,
		func(choice worldmap.DungeonMapChoice) (int64, error) {
			return choice.Candidates[0].ID, nil
		},
	)
	if err != nil {
		t.Fatalf("build real dungeon 3 maze %d layout: %v", mazeIndex, err)
	}
	if topology.Start == nil {
		t.Fatalf("real dungeon 3 maze %d has no start coordinate", mazeIndex)
	}
	start, ok := topology.Room(*topology.Start)
	if !ok || start.Map == nil {
		t.Fatalf("real dungeon 3 maze %d start room is unresolved: %+v", mazeIndex, start)
	}
	if len(start.Neighbors) == 0 {
		t.Fatalf(
			"real dungeon 3 maze %d start room %s map %d has no topology neighbor",
			mazeIndex,
			start.Coordinate,
			start.Map.Map.ID,
		)
	}
	for _, neighbor := range start.Neighbors {
		room, found := topology.Room(neighbor.Coordinate)
		if !found || room.Map == nil {
			t.Fatalf("real start neighbor is unresolved: direction=%s coordinate=%s room=%+v", neighbor.Direction, neighbor.Coordinate, room)
		}
		t.Logf(
			"real dungeon 3 quest maze exit: maze=%d start=%s map=%d direction=%s target=%s target_map=%d start_portals=%+v",
			mazeIndex,
			start.Coordinate,
			start.Map.Map.ID,
			neighbor.Direction,
			neighbor.Coordinate,
			room.Map.Map.ID,
			start.Map.Map.Portals,
		)
	}
}
