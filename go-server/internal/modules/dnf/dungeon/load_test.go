package dungeon

import (
	"context"
	"errors"
	"testing"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

func TestLoadBuildsDungeonTable(t *testing.T) {
	table, err := Load(context.Background(), testIndex(t), Options{})
	if err != nil {
		t.Fatalf("load dungeon: %v", err)
	}
	if snapshot := table.Snapshot(); snapshot.Dungeons != 1 {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
	dungeon, ok := table.Find(4001)
	if !ok {
		t.Fatalf("expected dungeon")
	}
	if dungeon.Name != "training room" || dungeon.Area != "west coast" || dungeon.MinLevel != 10 || dungeon.Fatigue != 8 {
		t.Fatalf("unexpected dungeon: %+v", dungeon)
	}
	if len(dungeon.MapPaths) != 2 || dungeon.MapPaths[1] != "map/training_b.map" {
		t.Fatalf("unexpected map paths: %+v", dungeon.MapPaths)
	}
	if len(dungeon.MonsterIDs) != 2 || dungeon.MonsterIDs[0] != 3001 || dungeon.BossIDs[0] != 3999 {
		t.Fatalf("unexpected monster refs: monsters=%+v boss=%+v", dungeon.MonsterIDs, dungeon.BossIDs)
	}
	dungeon.MapPaths[0] = "changed"
	again, _ := table.FindPath("./DUNGEON/training_room.dgn")
	if again.MapPaths[0] != "map/training_a.map" {
		t.Fatalf("table returned mutable map paths: %+v", again.MapPaths)
	}
}

func TestLoadRequiresIndex(t *testing.T) {
	_, err := Load(context.Background(), nil, Options{})
	if !errors.Is(err, ErrIndexRequired) {
		t.Fatalf("expected ErrIndexRequired, got %v", err)
	}
}

func TestLoadReportsEmptyList(t *testing.T) {
	index, err := dnfpvf.Build(context.Background(), textSource{
		"dungeon/dungeon.lst": "4001 `dungeon/missing.dgn`\n",
	}, dnfpvf.BuildOptions{Paths: []string{"dungeon/dungeon.lst"}})
	if err != nil {
		t.Fatalf("build index: %v", err)
	}
	_, err = Load(context.Background(), index, Options{})
	if !errors.Is(err, ErrListEmpty) {
		t.Fatalf("expected ErrListEmpty, got %v", err)
	}
}

func testIndex(t *testing.T) *dnfpvf.Index {
	t.Helper()
	index, err := dnfpvf.Build(context.Background(), textSource{
		"dungeon/dungeon.lst": "4001 `dungeon/training_room.dgn`\n",
		"dungeon/training_room.dgn": `
[name]
` + "`training room`" + `
[area]
` + "`west coast`" + `
[minimum level]
10
[maximum level]
20
[fatigue]
8
[maps]
` + "`map/training_a.map` `map/training_b.map`" + `
[monsters]
3001 3002
[boss]
3999
[reward]
` + "`drop/training.drop`" + `
`,
	}, dnfpvf.BuildOptions{Lists: []string{DefaultList}})
	if err != nil {
		t.Fatalf("build index: %v", err)
	}
	return index
}

type textSource map[string]string

func (s textSource) ReadText(relativePath string) (string, error) {
	for key, value := range s {
		if pathKey(key) == pathKey(relativePath) {
			return value, nil
		}
	}
	return "", dnfpvf.ErrDocNotFound
}
