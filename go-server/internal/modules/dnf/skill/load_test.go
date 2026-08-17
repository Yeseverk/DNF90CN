package skill

import (
	"context"
	"errors"
	"testing"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

func TestLoadBuildsJobScopedSkillTable(t *testing.T) {
	index := testIndex(t)
	table, err := Load(context.Background(), index, Options{})
	if err != nil {
		t.Fatalf("load skill: %v", err)
	}
	if snapshot := table.Snapshot(); snapshot.Jobs != 2 || snapshot.Skills != 2 {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}

	swordman, ok := table.Find(0, 46)
	if !ok {
		t.Fatal("expected swordman skill")
	}
	if swordman.Name != "upper slash" || swordman.Job != 0 || swordman.Kind != "[active]" || !swordman.Active {
		t.Fatalf("unexpected swordman skill: %+v", swordman)
	}
	if swordman.RequiredLevel != 1 || swordman.MaximumLevel != 20 || swordman.SkillClass != 1 {
		t.Fatalf("unexpected level metadata: %+v", swordman)
	}
	if !swordman.FixedLevelSkill || swordman.FixedLevelBase != 1 || swordman.FixedLevelInterval != 5 || swordman.FixedLevelIncrement != 2 || swordman.FixedLevelForCharacter(11) != 5 {
		t.Fatalf("unexpected fixed-level metadata: %+v", swordman)
	}
	if len(swordman.GrowTypes) != 3 || len(swordman.SecondGrowTypes) != 1 || swordman.SecondGrowTypes[0] != 2 || len(swordman.PurchaseCost) != 1 || swordman.PurchaseCost[0] != 20 {
		t.Fatalf("unexpected rule arrays: %+v", swordman)
	}
	if len(swordman.Prerequisites) != 1 || swordman.Prerequisites[0] != (Prerequisite{SkillID: 20, Level: 1}) {
		t.Fatalf("unexpected prerequisites: %+v", swordman.Prerequisites)
	}
	if !swordman.SupportsGrowType(2) || swordman.SupportsGrowType(9) || !swordman.IsTPSkill() || swordman.LevelCost(10) != 2 {
		t.Fatalf("unexpected domain rules: %+v", swordman)
	}
	if swordman.SupportsCharacterGrowth(0x12) || !swordman.SupportsCharacterGrowth(0x22) {
		t.Fatalf("second growtype fitness was not applied to packed grow_type: %+v", swordman)
	}
	if swordman.CoolTime != 2000 || swordman.MPCost != 12 || swordman.Scalars["power"] != 120 {
		t.Fatalf("unexpected scalar fields: %+v", swordman)
	}

	fighter, ok := table.Find(1, 46)
	if !ok || fighter.Name != "muse upper" {
		t.Fatalf("same skill ID must remain job-scoped: %+v ok=%t", fighter, ok)
	}
	if _, ok := table.Find(2, 46); ok {
		t.Fatal("skill leaked into an unrelated job")
	}

	swordman.GrowTypes[0] = 99
	swordman.Prerequisites[0].Level = 99
	swordman.PurchaseCost[0] = 99
	swordman.Scalars["power"] = 1
	again, _ := table.FindPath(0, "./SKILL/Swordman/UpperSlash.skl")
	if again.GrowTypes[0] != 0 || again.Prerequisites[0].Level != 1 || again.PurchaseCost[0] != 20 || again.Scalars["power"] != 120 {
		t.Fatalf("table returned mutable fields: %+v", again)
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
		"skill/skilllist.lst": "0 `SwordmanSkill.lst`\n",
	}, dnfpvf.BuildOptions{Paths: []string{"skill/skilllist.lst"}})
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
		"skill/skilllist.lst":     "0 `SwordmanSkill.lst`\n1 `FighterSkill.lst`\n",
		"skill/SwordmanSkill.lst": "46 `Swordman/UpperSlash.skl`\n",
		"skill/FighterSkill.lst":  "46 `Fighter/MuseUpper.skl`\n",
		"skill/Swordman/UpperSlash.skl": `
[name]
` + "`upper slash`" + `
[type]
` + "`[active]`" + `
[required level]
1
[maximum level]
20
[fixed level skill]
1
[interval level]
5
[add level per interval]
2
[skill class]
1
[skill fitness growtype]
0 1 2
[/skill fitness growtype]
[skill fitness second growtype]
2
[/skill fitness second growtype]
[pre required skill]
20 1
[/pre required skill]
[purchase cost]
20
[/purchase cost]
[special purchase cost]
2
[/special purchase cost]
[feature skill type]
1
[cool time]
2000
[consume MP]
12
[power]
120
`,
		"skill/Fighter/MuseUpper.skl": `
[name]
` + "`muse upper`" + `
[type]
` + "`[passive]`" + `
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
