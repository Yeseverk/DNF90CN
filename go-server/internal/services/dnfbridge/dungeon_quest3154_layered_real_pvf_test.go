package dnfbridge

import (
	"context"
	"os"
	"path"
	"testing"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	dnfquest "longheng.io/server/internal/modules/dnf/quest"
	"longheng.io/server/internal/modules/dnf/worldmap"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestRealScriptPVFDungeon1000Quest3154LayeredStoryEvidence(t *testing.T) {
	pvfPath := os.Getenv("DNF_WORLDMAP_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNF_WORLDMAP_REAL_PVF_SMOKE to verify dungeon 1000 quest 3154 layered story ownership")
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
	questIndex, err := dnfpvf.Build(ctx, archive, dnfpvf.BuildOptions{Lists: []string{dnfquest.DefaultList}})
	if err != nil {
		t.Fatal(err)
	}
	questCatalog, err := dnfquest.Load(ctx, questIndex)
	if err != nil {
		t.Fatal(err)
	}

	dungeon, ok := table.FindDungeon(1000)
	if !ok || len(dungeon.Mazes) <= 2 {
		t.Fatalf("real dungeon 1000 found=%t mazes=%d", ok, len(dungeon.Mazes))
	}
	maze := dungeon.Mazes[2]
	coordinate := worldmap.RoomCoordinate{X: 3, Y: 0}
	base, err := resolver.Resolve(worldmap.ResolveRequest{
		DungeonID: 1000,
		MazeIndex: 2,
		X:         coordinate.X,
		Y:         coordinate.Y,
	})
	if err != nil {
		t.Fatal(err)
	}
	layer, err := resolver.ResolveLayered(1000, 2, coordinate, 0)
	if err != nil {
		t.Fatal(err)
	}
	quest, questFound := questCatalog.Find(3154)
	if maze.Index != 2 || len(maze.QuestConnection) < 3 ||
		maze.QuestConnection[0] != 0 || maze.QuestConnection[1] != 3154 || maze.QuestConnection[2] != -1 {
		t.Fatalf("real dungeon1000 maze2 quest connection=%v", maze.QuestConnection)
	}
	if base.Map.ID != 76196 || layer.Map.ID != 76197 {
		t.Fatalf("real dungeon1000 maze2 final maps base=%d layer=%d", base.Map.ID, layer.Map.ID)
	}
	if len(base.Map.Monsters) != 1 || base.Map.Monsters[0].MonsterID != 107000920 ||
		len(layer.Map.Monsters) != 1 || layer.Map.Monsters[0].MonsterID != 75100 {
		t.Fatalf("real dungeon1000 maze2 actors base=%+v layer=%+v", base.Map.Monsters, layer.Map.Monsters)
	}
	if !questFound || normalizeDungeonPVFSymbol(quest.Type) != "clear map" ||
		len(quest.IntData) != 1 || quest.IntData[0] != 76196 {
		t.Fatalf("real quest3154 definition=%+v found=%t", quest, questFound)
	}
	runtime := &runtimeDungeonState{Dungeon: dungeon, MazeIndex: 2}
	if !currentDungeonConnectedClearMapQuestOwnsBaseMap(runtime, base.Map.ID, layer.Map.ID, questCatalog) {
		t.Fatal("real quest3154 must make base map76196 authoritative over optional layer76197")
	}
	maze1 := dungeon.Mazes[1]
	maze1Base, err := resolver.Resolve(worldmap.ResolveRequest{
		DungeonID: 1000,
		MazeIndex: 1,
		X:         maze1.Boss.X,
		Y:         maze1.Boss.Y,
	})
	if err != nil {
		t.Fatal(err)
	}
	maze1Coordinate := worldmap.RoomCoordinate{X: maze1.Boss.X, Y: maze1.Boss.Y}
	maze1Layer, err := resolver.ResolveLayered(1000, 1, maze1Coordinate, 0)
	if err != nil {
		t.Fatal(err)
	}
	runtime.MazeIndex = 1
	if currentDungeonConnectedClearMapQuestOwnsBaseMap(runtime, maze1Base.Map.ID, maze1Layer.Map.ID, questCatalog) {
		t.Fatal("real quest3151 targets layer76187 and must not settle on base map76186")
	}
	actionPath := path.Join(path.Dir(base.Map.Path), base.Map.BasicAction)
	actionText, err := archive.ReadText(actionPath)
	if err != nil {
		t.Fatalf("read base map action %s: %v", actionPath, err)
	}
	t.Logf("base_action_path=%s text=%q", actionPath, actionText)
}
