package monster

import (
	"context"
	"errors"
	"testing"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

func TestLoadBuildsMonsterTable(t *testing.T) {
	table, err := Load(context.Background(), testIndex(t), Options{})
	if err != nil {
		t.Fatalf("load monster: %v", err)
	}
	if snapshot := table.Snapshot(); snapshot.Monsters != 1 {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
	monster, ok := table.Find(3001)
	if !ok {
		t.Fatalf("expected monster")
	}
	if monster.Name != "training dummy" || monster.Kind != "normal" || monster.Level != 7 || monster.HP != 120 {
		t.Fatalf("unexpected monster: %+v", monster)
	}
	if monster.Scalars["attack"] != 21 || monster.Scalars["move_speed"] != 2.5 {
		t.Fatalf("unexpected scalars: %+v", monster.Scalars)
	}
	monster.Scalars["attack"] = 1
	again, _ := table.FindPath("./MONSTER/training_dummy.mob")
	if again.Scalars["attack"] != 21 {
		t.Fatalf("table returned mutable scalars: %+v", again.Scalars)
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
		"monster/monster.lst": "3001 `monster/missing.mob`\n",
	}, dnfpvf.BuildOptions{Paths: []string{"monster/monster.lst"}})
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
		"monster/monster.lst": "3001 `monster/training_dummy.mob`\n",
		"monster/training_dummy.mob": `
[name]
` + "`training dummy`" + `
[monster type]
` + "`normal`" + `
[level]
7
[hp]
120
[attack]
21
[move speed]
2.5
[exp]
8
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
