package dnfbridge

import (
	"context"
	"encoding/binary"
	"os"
	"testing"

	"longheng.io/server/internal/modules/dnf/dungeoncmd"
	"longheng.io/server/internal/modules/dnf/worldmap"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

// TestRealScriptPVFDungeon1000Maze2RetainedSpecialSpawnsAreMetadata locks the
// real quest-connected start-room ownership boundary. Map 76191 owns one
// type-9 special passive object. Its non-monster child rows are object-script
// metadata, not op29 actors and not materialized op29 extra-item entries.
func TestRealScriptPVFDungeon1000Maze2RetainedSpecialSpawnsAreMetadata(t *testing.T) {
	pvfPath := os.Getenv("DNF_WORLDMAP_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNF_WORLDMAP_REAL_PVF_SMOKE to verify dungeon 1000 maze 2 retained special-spawn ownership")
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
	monsterCatalog, err := newPVFDungeonMonsterCatalog(archive)
	if err != nil {
		t.Fatal(err)
	}

	dungeon, ok := table.FindDungeon(1000)
	if !ok {
		t.Fatal("real PVF dungeon 1000 is missing")
	}
	const mazeIndex = 2
	if len(dungeon.Mazes) <= mazeIndex {
		t.Fatalf("real PVF dungeon 1000 maze count=%d, want index %d", len(dungeon.Mazes), mazeIndex)
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
		t.Fatalf("build real dungeon 1000 maze 2 topology: %v", err)
	}
	if topology.Start == nil || len(topology.Bosses) == 0 {
		t.Fatalf("real dungeon 1000 maze 2 start=%v bosses=%v", topology.Start, topology.Bosses)
	}
	run, err := worldmap.NewDungeonRun(topology)
	if err != nil {
		t.Fatal(err)
	}
	session, err := worldmap.NewDungeonSession(run)
	if err != nil {
		t.Fatal(err)
	}
	scene, ok := session.Scene()
	if !ok {
		t.Fatal("real dungeon 1000 maze 2 start scene is missing")
	}
	if scene.Map.Map.ID != 76191 {
		t.Fatalf("real dungeon 1000 maze 2 start map=%d, want 76191", scene.Map.Map.ID)
	}
	if len(scene.Monsters) != 6 {
		t.Fatalf("real map 76191 ordinary monsters=%d, want 6", len(scene.Monsters))
	}
	if len(scene.SpecialPassiveObjects) != 1 {
		t.Fatalf("real map 76191 special passive objects=%d, want 1", len(scene.SpecialPassiveObjects))
	}
	specialObject := scene.SpecialPassiveObjects[0]
	if specialObject.ObjectID != 109006908 {
		t.Fatalf("real map 76191 special object id=%d, want 109006908", specialObject.ObjectID)
	}
	if len(specialObject.Spawns) != 2 {
		t.Fatalf("real map 76191 special object spawns=%d, want 2", len(specialObject.Spawns))
	}
	expectedSpawns := []worldmap.SpecialObjectSpawn{
		{Kind: "[item]", Code: 3, Level: -1, Params: [3]int64{1, -1, -1}},
		{Kind: "[trap]", Code: 48022, Level: 15, Params: [3]int64{1, 5, -1}},
	}
	for index, spawn := range specialObject.Spawns {
		t.Logf("real map 76191 special object index=0 id=%d spawn_index=%d kind=%q code=%d level=%d params=%v", specialObject.ObjectID, index, spawn.Kind, spawn.Code, spawn.Level, spawn.Params)
		if spawn != expectedSpawns[index] {
			t.Fatalf("real map 76191 special object spawn[%d]=%+v, want %+v", index, spawn, expectedSpawns[index])
		}
	}

	room, nextObjectKey, err := newRuntimeDungeonRoom(scene, monsterCatalog, firstDungeonMonsterObjectKey)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planRuntimeDungeonExtendedActors(scene, monsterCatalog, nil, dungeon.Metadata.BasisLevel, nextObjectKey)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actors) != 1 || plan.Actors[0].Kind != runtimeDungeonActorSpecialObject || plan.Actors[0].Packet.Code != uint32(specialObject.ObjectID) || plan.Actors[0].Packet.Type != 9 {
		t.Fatalf("real map 76191 extended actors=%+v, want only the type-9 object", plan.Actors)
	}
	if len(plan.RetainedSpecialSpawns) != 2 {
		t.Fatalf("real map 76191 retained special spawns=%+v, want 2", plan.RetainedSpecialSpawns)
	}
	if err := room.AttachExtendedActors(plan); err != nil {
		t.Fatal(err)
	}
	runtime := &runtimeDungeonState{
		Request:        dungeoncmd.SelectDungeonRequest{DungeonID: 1000},
		Dungeon:        dungeon,
		MazeIndex:      mazeIndex,
		Session:        session,
		Room:           room,
		NextObjectKey:  plan.NextObjectKey,
		BossCoordinate: topology.Bosses[0],
		BossSet:        true,
		Seed:           1,
	}
	packets, err := buildCurrentDungeonEntryPackets(runtime, scene)
	if err != nil {
		t.Fatalf("real dungeon 1000 maze 2 op28/op29 build blocked: %v", err)
	}
	if len(packets.DungeonInfo) != 36 {
		t.Fatalf("real dungeon 1000 maze 2 op28 len=%d", len(packets.DungeonInfo))
	}
	if len(packets.StartMap) < 20 {
		t.Fatalf("real dungeon 1000 maze 2 op29 len=%d", len(packets.StartMap))
	}
	actorCount := int(packets.StartMap[18])
	if actorCount != 7 {
		t.Fatalf("real map 76191 op29 actor count=%d, want 6 ordinary + 1 type-9 object", actorCount)
	}
	type9Count := 0
	for index := 0; index < actorCount; index++ {
		offset := 19 + index*21
		code := binary.LittleEndian.Uint32(packets.StartMap[offset+8 : offset+12])
		actorType := packets.StartMap[offset+13]
		if actorType == 9 {
			type9Count++
			if code != uint32(specialObject.ObjectID) {
				t.Fatalf("real map 76191 type-9 actor code=%d, want %d", code, specialObject.ObjectID)
			}
		}
		for _, spawn := range specialObject.Spawns {
			if spawn.Code > 0 && code == uint32(spawn.Code) {
				t.Fatalf("real map 76191 metadata child kind=%q code=%d was fabricated as op29 actor", spawn.Kind, spawn.Code)
			}
		}
	}
	if type9Count != 1 {
		t.Fatalf("real map 76191 type-9 actor count=%d, want 1", type9Count)
	}
	extraCountOffset := 19 + actorCount*21
	if packets.StartMap[extraCountOffset] != 0 {
		t.Fatalf("real map 76191 metadata children were fabricated as op29 extra entries: count=%d", packets.StartMap[extraCountOffset])
	}
}
