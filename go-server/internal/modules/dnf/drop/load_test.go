// 本文件验证掉落表加载只依赖内存 PVF 索引。
package drop

import (
	"context"
	"errors"
	"testing"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

func TestLoadBuildsDropTable(t *testing.T) {
	table, err := Load(context.Background(), testIndex(t), Options{})
	if err != nil {
		t.Fatalf("load drop: %v", err)
	}
	if snapshot := table.Snapshot(); snapshot.Entries != 1 {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
	entry, ok := table.Find(5001)
	if !ok {
		t.Fatalf("expected drop entry")
	}
	if entry.Name != "training drop" || entry.Kind != "normal" || entry.Gold != 120 || entry.MinCount != 1 {
		t.Fatalf("unexpected entry: %+v", entry)
	}
	if len(entry.ItemIDs) != 2 || entry.ItemIDs[0] != 1001 {
		t.Fatalf("unexpected item ids: %+v", entry.ItemIDs)
	}
	if len(entry.Items) != 2 || entry.Items[0].ID != 1001 || entry.Items[0].Weight != 0.75 {
		t.Fatalf("unexpected weighted items: %+v", entry.Items)
	}
	entry.ItemIDs[0] = 1
	entry.Items[0].Weight = 1
	entry.Scalars["gold"] = 1
	again, _ := table.FindPath("./DROP/training.drop")
	if again.ItemIDs[0] != 1001 || again.Items[0].Weight != 0.75 || again.Scalars["gold"] != 120 {
		t.Fatalf("table returned mutable entry: %+v", again)
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
		"drop/drop.lst": "5001 `drop/missing.drop`\n",
	}, dnfpvf.BuildOptions{Paths: []string{"drop/drop.lst"}})
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
		"drop/drop.lst": "5001 `drop/training.drop`\n",
		"drop/training.drop": `
[name]
` + "`training drop`" + `
[drop type]
` + "`normal`" + `
[items]
1001 1002
[item paths]
` + "`equipment/training_sword.equ`" + `
[drop rate]
1001 0.75 ` + "`equipment/training_sword.equ`" + ` 0.25
[gold]
120
[min count]
1
[max count]
2
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
