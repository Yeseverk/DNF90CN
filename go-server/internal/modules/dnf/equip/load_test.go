package equip

import (
	"context"
	"errors"
	"testing"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

func TestLoadBuildsEquipTable(t *testing.T) {
	index := testIndex(t)
	table, err := Load(context.Background(), index, Options{})
	if err != nil {
		t.Fatalf("load equip: %v", err)
	}
	if snapshot := table.Snapshot(); snapshot.Items != 1 {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
	item, ok := table.Find(1001)
	if !ok {
		t.Fatalf("expected item")
	}
	if item.Name != "short sword" || item.Kind != "weapon" || item.Slot != "weapon" || item.Level != 12 {
		t.Fatalf("unexpected item: %+v", item)
	}
	if item.Stats["physical_attack"] != 34 || item.Stats["attack_speed"] != 1.5 {
		t.Fatalf("unexpected stats: %+v", item.Stats)
	}
	item.Stats["physical_attack"] = 1
	again, _ := table.FindPath("./EQUIPMENT/weapon/sword.equ")
	if again.Stats["physical_attack"] != 34 {
		t.Fatalf("table returned mutable stats: %+v", again.Stats)
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
		"equipment/equipment.lst": "1001 `equipment/weapon/missing.equ`\n",
	}, dnfpvf.BuildOptions{Paths: []string{"equipment/equipment.lst"}})
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
		"equipment/equipment.lst": "1001 `equipment/weapon/sword.equ`\n",
		"equipment/weapon/sword.equ": `
[name]
` + "`short sword`" + `
[equipment type]
` + "`weapon`" + `
[attach type]
` + "`weapon`" + `
[minimum level]
12
[rarity]
` + "`rare`" + `
[physical attack]
34
[attack speed]
1.5
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
