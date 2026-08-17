// 本文件验证奖励表加载只依赖内存 PVF 索引。
package reward

import (
	"context"
	"errors"
	"testing"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

func TestLoadBuildsRewardTable(t *testing.T) {
	table, err := Load(context.Background(), testIndex(t), Options{})
	if err != nil {
		t.Fatalf("load reward: %v", err)
	}
	if snapshot := table.Snapshot(); snapshot.Rewards != 1 {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
	reward, ok := table.Find(6001)
	if !ok {
		t.Fatalf("expected reward")
	}
	if reward.Name != "training reward" || reward.Kind != "clear" || reward.Gold != 300 || reward.Exp != 800 {
		t.Fatalf("unexpected reward: %+v", reward)
	}
	if len(reward.DropIDs) != 2 || reward.DropIDs[0] != 5001 {
		t.Fatalf("unexpected drop ids: %+v", reward.DropIDs)
	}
	if len(reward.ItemPaths) != 1 || reward.ItemPaths[0] != "equipment/training_sword.equ" {
		t.Fatalf("unexpected item paths: %+v", reward.ItemPaths)
	}
	reward.DropIDs[0] = 1
	reward.ItemPaths[0] = "changed"
	reward.Scalars["exp"] = 1
	again, _ := table.FindPath("./REWARD/training.rwd")
	if again.DropIDs[0] != 5001 || again.ItemPaths[0] != "equipment/training_sword.equ" || again.Scalars["exp"] != 800 {
		t.Fatalf("table returned mutable reward: %+v", again)
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
		"reward/reward.lst": "6001 `reward/missing.rwd`\n",
	}, dnfpvf.BuildOptions{Paths: []string{"reward/reward.lst"}})
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
		"reward/reward.lst": "6001 `reward/training.rwd`\n",
		"reward/training.rwd": `
[name]
` + "`training reward`" + `
[reward type]
` + "`clear`" + `
[drops]
5001 5002
[drop paths]
` + "`drop/training.drop`" + `
[items]
1001
[item paths]
` + "`equipment/training_sword.equ`" + `
[gold]
300
[exp]
800
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
