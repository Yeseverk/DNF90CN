// 本文件验证 DNF 静态数据总装配只读取内存 PVF source。
package staticdata

import (
	"context"
	"errors"
	"testing"

	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestLoadBuildsStaticStore(t *testing.T) {
	source := testSource{
		texts: map[string]string{
			"equipment/equipment.lst": "1001 `equipment/training_sword.equ`\n",
			"equipment/training_sword.equ": `
[name]
` + "`training sword`" + `
[equipment type]
` + "`weapon`" + `
[minimum level]
1
[physical attack]
12
`,
			"skill/skilllist.lst":     "0 `SwordmanSkill.lst`\n",
			"skill/SwordmanSkill.lst": "2001 `Swordman/training_slash.skl`\n",
			"skill/Swordman/training_slash.skl": `
[name]
` + "`training slash`" + `
[skill type]
` + "`active`" + `
[required level]
1
[cool time]
1.5
`,
			"monster/monster.lst": "3001 `monster/training_dummy.mob`\n",
			"monster/training_dummy.mob": `
[name]
` + "`training dummy`" + `
[monster type]
` + "`normal`" + `
[level]
1
[hp]
100
`,
			"dungeon/dungeon.lst": "4001 `dungeon/training_room.dgn`\n",
			"dungeon/training_room.dgn": `
[name]
` + "`training room`" + `
[minimum level]
1
[maps]
` + "`map/training.map`" + `
[monsters]
3001
[reward]
` + "`reward/training.rwd`" + `
`,
			"drop/drop.lst": "5001 `drop/training.drop`\n",
			"drop/training.drop": `
[name]
` + "`training drop`" + `
[items]
1001
[drop rate]
1001 1.0
[gold]
10
`,
			"reward/reward.lst": "6001 `reward/training.rwd`\n",
			"reward/training.rwd": `
[name]
` + "`training reward`" + `
[drops]
5001
[gold]
20
[exp]
30
`,
		},
		reads: make(map[string]int),
	}
	store, err := Load(context.Background(), source, Options{})
	if err != nil {
		t.Fatalf("load staticdata: %v", err)
	}
	snapshot := store.Snapshot()
	if snapshot.Index.Documents != 13 || snapshot.Index.Lists != 7 || snapshot.Index.Refs != 7 {
		t.Fatalf("unexpected index snapshot: %+v", snapshot.Index)
	}
	if snapshot.Equip.Items != 1 || snapshot.Skill.Skills != 1 || snapshot.Monster.Monsters != 1 ||
		snapshot.Dungeon.Dungeons != 1 || snapshot.Drop.Entries != 1 || snapshot.Reward.Rewards != 1 {
		t.Fatalf("unexpected store snapshot: %+v", snapshot)
	}
	equipItem, ok := store.Equip.Find(1001)
	if !ok || equipItem.Name != "training sword" || equipItem.Stats["physical_attack"] != 12 {
		t.Fatalf("unexpected equip item: %+v ok=%v", equipItem, ok)
	}
	reward, ok := store.Reward.FindPath("./REWARD/training.rwd")
	if !ok || reward.Gold != 20 || reward.Exp != 30 {
		t.Fatalf("unexpected reward: %+v ok=%v", reward, ok)
	}
	before := source.reads["reward/training.rwd"]
	if _, ok := store.Index.Document("reward/training.rwd"); !ok {
		t.Fatalf("expected indexed reward document")
	}
	if source.reads["reward/training.rwd"] != before {
		t.Fatalf("document lookup should not read source again")
	}
}

func TestLoadRequiresSource(t *testing.T) {
	_, err := Load(context.Background(), nil, Options{})
	if !errors.Is(err, ErrSourceRequired) {
		t.Fatalf("expected ErrSourceRequired, got %v", err)
	}
}

func TestLoadHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Load(ctx, testSource{texts: map[string]string{}, reads: make(map[string]int)}, Options{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
}

func TestDefaultListsReturnsCopy(t *testing.T) {
	lists := DefaultLists()
	lists[0] = "changed.lst"
	again := DefaultLists()
	if again[0] == "changed.lst" {
		t.Fatalf("default lists returned mutable storage: %+v", again)
	}
}

type testSource struct {
	texts map[string]string
	reads map[string]int
}

func (s testSource) ReadText(relativePath string) (string, error) {
	key := listKey(relativePath)
	for path, text := range s.texts {
		if listKey(path) == key {
			s.reads[cleanList(path)]++
			return text, nil
		}
	}
	return "", platformpvf.ErrFileNotFound
}
