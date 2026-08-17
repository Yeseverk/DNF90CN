// 本文件验证 DNF 静态数据组件只做生命周期装配。
package staticdata

import (
	"context"
	"errors"
	"testing"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

func TestComponentStartLoadsStore(t *testing.T) {
	component := NewComponent(componentSource(), Options{})
	if component.Name() != "dnf-staticdata" {
		t.Fatalf("unexpected component name: %s", component.Name())
	}
	if err := component.Preflight(context.Background()); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if _, ok := component.Store(); ok {
		t.Fatalf("store should not be visible before start")
	}

	if err := component.Start(context.Background()); err != nil {
		t.Fatalf("start component: %v", err)
	}
	store, ok := component.Store()
	if !ok {
		t.Fatalf("expected loaded store")
	}
	item, ok := store.Equip.Find(1001)
	if !ok || item.Name != "component sword" {
		t.Fatalf("unexpected equip item: %+v ok=%v", item, ok)
	}
	snapshot := component.Snapshot()
	if !snapshot.Started || snapshot.Store.Index.Documents != 13 || snapshot.Store.Equip.Items != 1 {
		t.Fatalf("unexpected component snapshot: %+v", snapshot)
	}
}

func TestComponentRejectsMissingSource(t *testing.T) {
	component := NewComponent(nil, Options{})
	if err := component.Preflight(context.Background()); !errors.Is(err, ErrSourceRequired) {
		t.Fatalf("expected ErrSourceRequired, got %v", err)
	}
	if err := component.Start(context.Background()); !errors.Is(err, ErrSourceRequired) {
		t.Fatalf("expected start source error, got %v", err)
	}
	if snapshot := component.Snapshot(); snapshot.Started || snapshot.LastError == "" {
		t.Fatalf("expected failed snapshot, got %+v", snapshot)
	}
}

func TestComponentHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	component := NewComponent(componentSource(), Options{})
	if err := component.Start(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
	if _, ok := component.Store(); ok {
		t.Fatalf("store should not be visible after canceled start")
	}
}

func TestComponentStopClearsStore(t *testing.T) {
	component := NewComponent(componentSource(), Options{})
	if err := component.Start(context.Background()); err != nil {
		t.Fatalf("start component: %v", err)
	}
	if err := component.Stop(context.Background()); err != nil {
		t.Fatalf("stop component: %v", err)
	}
	if _, ok := component.Store(); ok {
		t.Fatalf("store should be cleared after stop")
	}
	if snapshot := component.Snapshot(); snapshot.Started || snapshot.Store.Index.Documents != 0 {
		t.Fatalf("unexpected stopped snapshot: %+v", snapshot)
	}
}

func TestComponentClonesOptions(t *testing.T) {
	lists := []string{"equipment/equipment.lst"}
	component := NewComponent(componentSource(), Options{
		Build: dnfpvf.BuildOptions{Lists: lists},
	})
	lists[0] = "missing.lst"
	if err := component.Start(context.Background()); err != nil {
		t.Fatalf("start with cloned options: %v", err)
	}
}

func componentSource() testSource {
	return testSource{
		texts: map[string]string{
			"equipment/equipment.lst": "1001 `equipment/component_sword.equ`\n",
			"equipment/component_sword.equ": `
[name]
` + "`component sword`" + `
[equipment type]
` + "`weapon`" + `
[minimum level]
1
`,
			"skill/skilllist.lst":     "0 `SwordmanSkill.lst`\n",
			"skill/SwordmanSkill.lst": "2001 `Swordman/component_slash.skl`\n",
			"skill/Swordman/component_slash.skl": `
[name]
` + "`component slash`" + `
[required level]
1
`,
			"monster/monster.lst": "3001 `monster/component_dummy.mob`\n",
			"monster/component_dummy.mob": `
[name]
` + "`component dummy`" + `
[level]
1
`,
			"dungeon/dungeon.lst": "4001 `dungeon/component_room.dgn`\n",
			"dungeon/component_room.dgn": `
[name]
` + "`component room`" + `
[minimum level]
1
`,
			"drop/drop.lst": "5001 `drop/component.drop`\n",
			"drop/component.drop": `
[name]
` + "`component drop`" + `
[gold]
10
`,
			"reward/reward.lst": "6001 `reward/component.rwd`\n",
			"reward/component.rwd": `
[name]
` + "`component reward`" + `
[gold]
20
`,
		},
		reads: make(map[string]int),
	}
}
