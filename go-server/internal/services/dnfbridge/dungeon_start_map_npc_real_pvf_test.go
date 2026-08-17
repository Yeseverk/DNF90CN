package dnfbridge

import (
	"context"
	"errors"
	"os"
	"testing"

	"longheng.io/server/internal/modules/dnf/worldmap"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestRealScriptPVFDungeon162BossRoomNPCStartMap(t *testing.T) {
	pvfPath := os.Getenv("DNF_WORLDMAP_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNF_WORLDMAP_REAL_PVF_SMOKE to verify dungeon 162 NPC start-map construction")
	}
	ctx := context.Background()
	archive, err := platformpvf.Open(pvfPath)
	if err != nil {
		t.Fatal(err)
	}
	table, err := worldmap.LoadSource(ctx, archive, worldmap.Options{})
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := worldmap.NewResolver(table)
	if err != nil {
		t.Fatal(err)
	}
	topology, err := worldmap.BuildDungeonLayout(
		resolver,
		162,
		3,
		func(choice worldmap.DungeonMapChoice) (int64, error) { return choice.Candidates[0].ID, nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	room, ok := topology.Room(worldmap.RoomCoordinate{X: 4, Y: 2})
	if !ok || room.Map == nil || len(room.Map.Map.Monsters) == 0 {
		t.Fatalf("dungeon 162 maze 3 room 4:2 missing: room=%+v found=%t", room, ok)
	}
	spawn := room.Map.Map.Monsters[0]
	if spawn.MonsterID != 62531 || normalizeDungeonPVFSymbol(spawn.Rank) != "npc" {
		t.Fatalf("dungeon 162 room 4:2 first monster=%+v map=%d", spawn, room.Map.Map.ID)
	}
	dungeon, ok := table.FindDungeon(162)
	if !ok {
		t.Fatal("dungeon 162 missing from real PVF")
	}
	monsters, err := newPVFDungeonMonsterCatalog(archive)
	if err != nil {
		t.Fatal(err)
	}
	scene := worldmap.DungeonRoomScene{
		Coordinate: room.Coordinate,
		Boss:       room.Boss,
		Map:        *room.Map,
		Monsters:   append([]worldmap.MonsterSpawn(nil), room.Map.Map.Monsters...),
	}
	runtimeRoom, _, err := newRuntimeDungeonRoom(scene, monsters, firstDungeonMonsterObjectKey)
	if err != nil {
		t.Fatal(err)
	}
	runtime := &runtimeDungeonState{Dungeon: dungeon, Room: runtimeRoom}
	packet, err := currentDungeonStartMapFromRuntime(runtime, scene, currentDungeonStartMapState{})
	if err != nil {
		t.Fatalf("build dungeon 162 room 4:2 start-map: %v", err)
	}
	if len(packet.Actors) == 0 || packet.Actors[0].Code != 62531 ||
		packet.Actors[0].Type != currentDungeonNPCActorType || packet.Actors[0].Blocking != 1 {
		t.Fatalf("dungeon 162 room 4:2 actors=%+v", packet.Actors)
	}
	if _, err := packet.Build(); err != nil {
		t.Fatalf("serialize dungeon 162 room 4:2 start-map: %v", err)
	}
	if _, err := currentDungeonMonsterType(spawn.Rank); !errors.Is(err, errDungeonStartMapMonsterRank) {
		t.Fatalf("NPC unexpectedly entered ordinary reward type mapping: %v", err)
	}
}
