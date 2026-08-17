package profession

import (
	"context"
	"errors"
	"testing"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfskill "longheng.io/server/internal/modules/dnf/skill"
)

type profileSource map[string]string

func (s profileSource) ReadText(relativePath string) (string, error) {
	value, ok := s[relativePath]
	if !ok {
		return "", errors.New("missing " + relativePath)
	}
	return value, nil
}

func TestProfilesApplyClassChangeAndCumulativeAwakening(t *testing.T) {
	source := profileSource{
		DefaultCharacterList: "2 `Gunner/Gunner.chr`\n",
		"character/Gunner/Gunner.chr": `[initial value]
[skill]
1 1
[/skill]
[growtype 2]
[skill]
2 1
[/skill]
[awakening 1]
[awakening skill]
3 1
[/awakening skill]
[awakening 2]
[awakening skill]
4 1
[/awakening skill]
`,
		dnfskill.DefaultList:    "2 `GunnerSkill.lst`\n",
		"skill/GunnerSkill.lst": "1 `Gunner/a.skl` 2 `Gunner/b.skl` 3 `Gunner/c.skl` 4 `Gunner/d.skl`\n",
		"skill/Gunner/a.skl":    "[name]\n`a`\n[skill type]\n`[active]`\n",
		"skill/Gunner/b.skl":    "[name]\n`b`\n[skill type]\n`[passive]`\n",
		"skill/Gunner/c.skl":    "[name]\n`c`\n[skill type]\n`[passive]`\n",
		"skill/Gunner/d.skl":    "[name]\n`d`\n[skill type]\n`[passive]`\n",
	}
	index, err := dnfpvf.Build(context.Background(), source, dnfpvf.BuildOptions{Lists: []string{dnfskill.DefaultList}})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := dnfskill.Load(context.Background(), index, dnfskill.Options{})
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := LoadProfiles(context.Background(), source, catalog)
	if err != nil {
		t.Fatal(err)
	}
	points := dnfrepo.SkillPointState{TotalSP: 100, RemainingSP: 70, TotalTP: 10, RemainingTP: 5, SyncedLevel: 50}
	record := dnfrepo.SkillRecord{CharacterID: "9", Skills: map[int64]dnfrepo.SkillState{99: {Level: 2, Enabled: true}}}
	changed, err := profiles.ApplySkillTransition(catalog, 2, 50, Transition{Kind: KindClassChange, FirstGrowType: 1}, record, points)
	if err != nil {
		t.Fatal(err)
	}
	if len(changed.Skills) != 2 || changed.Skills[1].Level != 1 || changed.Skills[2].Level != 1 || changed.Points.RemainingSP != 100 || changed.Points.RemainingTP != 10 {
		t.Fatalf("changed = %+v", changed)
	}
	awakened, err := profiles.ApplySkillTransition(catalog, 2, 50, Transition{Kind: KindAwakening, FirstGrowType: 1, AwakeningStage: 2}, changed, changed.Points)
	if err != nil {
		t.Fatal(err)
	}
	if len(awakened.Skills) != 4 || awakened.Skills[3].Level != 1 || awakened.Skills[4].Level != 1 {
		t.Fatalf("awakened skills = %+v", awakened.Skills)
	}
	purchased := record
	purchased.Skills = map[int64]dnfrepo.SkillState{99: {Level: 2, Enabled: true}}
	purchased.Points = points
	preserved, err := profiles.ApplySkillTransition(catalog, 2, 50, Transition{Kind: KindAwakening, FirstGrowType: 1, AwakeningStage: 1}, purchased, points)
	if err != nil {
		t.Fatal(err)
	}
	if preserved.Points != points || preserved.Skills[99].Level != 2 || preserved.Skills[3].Level != 1 {
		t.Fatalf("awakening changed learned skills or point ledger = %+v", preserved)
	}
}

func TestProfilesDisplayNameUsesPVFGrowAndAwakeningNames(t *testing.T) {
	profile, err := parseJobProfile(`[growtype name]
` + "`鬼剑士` `驭剑士`" + `
[initial value]
[skill]
1 1
[/skill]
[growtype 1]
[awakening name]
` + "`黑暗武士` `时空主宰`" + `
[growtype 2]
[skill]
2 1
[/skill]
[awakening name]
` + "`剑宗` `剑皇`" + `
`)
	if err != nil {
		t.Fatal(err)
	}
	profiles := &Profiles{jobs: map[byte]jobProfile{11: profile}}
	for _, test := range []struct {
		grow byte
		want string
	}{
		{grow: 0x00, want: "鬼剑士"},
		{grow: 0x01, want: "驭剑士"},
		{grow: 0x11, want: "剑宗"},
		{grow: 0x21, want: "剑皇"},
	} {
		got, ok := profiles.DisplayName(11, test.grow)
		if !ok || got != test.want {
			t.Fatalf("grow=0x%02x display=%q found=%t want=%q", test.grow, got, ok, test.want)
		}
	}
}

func TestProfilesApplyPVFFixedLevelSkillAtCharacterLevel(t *testing.T) {
	source := profileSource{
		DefaultCharacterList:          "2 `Gunner/Gunner.chr`\n",
		"character/Gunner/Gunner.chr": "[initial value]\n[skill]\n1 1\n[/skill]\n[growtype 2]\n[skill]\n2 1\n[/skill]\n",
		dnfskill.DefaultList:          "2 `GunnerSkill.lst`\n",
		"skill/GunnerSkill.lst":       "1 `Gunner/a.skl` 2 `Gunner/b.skl`\n",
		"skill/Gunner/a.skl":          "[name]\n`a`\n[required level]\n1\n[maximum level]\n10\n[fixed level skill]\n1\n[interval level]\n10\n[add level per interval]\n1\n",
		"skill/Gunner/b.skl":          "[name]\n`b`\n[type]\n`[passive]`\n",
	}
	index, err := dnfpvf.Build(context.Background(), source, dnfpvf.BuildOptions{Lists: []string{dnfskill.DefaultList}})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := dnfskill.Load(context.Background(), index, dnfskill.Options{})
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := LoadProfiles(context.Background(), source, catalog)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := profiles.ApplySkillTransition(
		catalog, 2, 21,
		Transition{Kind: KindClassChange, FirstGrowType: 1},
		dnfrepo.SkillRecord{CharacterID: "9"},
		dnfrepo.SkillPointState{TotalSP: 100, RemainingSP: 70},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := changed.Skills[1].Level; got != 3 {
		t.Fatalf("fixed-level initial skill=%d want=3", got)
	}
}

func TestParseJobProfileRecordsEmptyBranchZeroAwakeningCapabilities(t *testing.T) {
	profile, err := parseJobProfile(`[initial value]
[skill]
1 1
[/skill]
[growtype 1]
[awakening 1]
[awakening skill]
[/awakening skill]
[awakening 2]
[awakening skill]
[/awakening skill]
`)
	if err != nil {
		t.Fatal(err)
	}
	profiles := &Profiles{jobs: map[byte]jobProfile{9: profile}}
	firstRequest, _ := ParseReward(2, "[awakening type]", []int64{1})
	first, err := profiles.PlanTransition(9, 0x00, firstRequest)
	if err != nil || first.NewGrowType != 0x10 {
		t.Fatalf("empty branch-zero awakening1 capability = %+v, %v", first, err)
	}
	secondRequest, _ := ParseReward(3, "[awakening type]", []int64{2})
	second, err := profiles.PlanTransition(9, first.NewGrowType, secondRequest)
	if err != nil || second.NewGrowType != 0x20 {
		t.Fatalf("empty branch-zero awakening2 capability = %+v, %v", second, err)
	}
}
