package profession

import (
	"context"
	"os"
	"testing"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	dnfskill "longheng.io/server/internal/modules/dnf/skill"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestRealScriptPVFProfessionProfiles(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to verify profession profiles")
	}
	archive, err := platformpvf.LoadArchive(platformpvf.Options{Path: pvfPath})
	if err != nil {
		t.Fatal(err)
	}
	index, err := dnfpvf.Build(context.Background(), archive, dnfpvf.BuildOptions{Lists: []string{dnfskill.DefaultList}})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := dnfskill.Load(context.Background(), index, dnfskill.Options{})
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := LoadProfiles(context.Background(), archive, catalog)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := profiles.Snapshot()
	if snapshot.Jobs != 16 || snapshot.InitialGrants != 116 || snapshot.ClassGrants != 453 || snapshot.AwakeningGrants != 118 || snapshot.MissingSkills != 3 {
		t.Fatalf("profession snapshot = %+v", snapshot)
	}
	if name, ok := profiles.DisplayName(11, 0x21); !ok || name != "剑皇" {
		t.Fatalf("female-slayer second-awakening display=%q found=%t want=剑皇", name, ok)
	}
	if name, ok := profiles.DisplayName(2, 0x01); !ok || name != "漫游枪手" {
		t.Fatalf("gunner class display=%q found=%t want=漫游枪手", name, ok)
	}
	fixedLevelSkills := 0
	fixedLevelAt90 := 0
	for _, definition := range catalog.Skills() {
		if !definition.FixedLevelSkill {
			continue
		}
		fixedLevelSkills++
		if definition.FixedLevelForCharacter(90) > 0 {
			fixedLevelAt90++
		}
	}
	gunbladerFirst, ok := profiles.FreeGrants(15, 1, 1)
	if !ok || len(gunbladerFirst) == 0 {
		t.Fatalf("gunblader class1 awakening1 grants = %v, found=%t", gunbladerFirst, ok)
	}
	gunbladerSecond, ok := profiles.FreeGrants(15, 1, 2)
	if !ok || !sameGrants(gunbladerFirst, gunbladerSecond) {
		t.Fatalf("gunblader second awakening must not fabricate free grants: first=%v second=%v", gunbladerFirst, gunbladerSecond)
	}
	thiefFirst, _ := profiles.FreeGrants(6, 3, 1)
	thiefSecond, _ := profiles.FreeGrants(6, 3, 2)
	if containsGrant(thiefFirst, 73) || !containsGrant(thiefSecond, 73) {
		t.Fatalf("thief branch3 second-awakening grant mismatch: first=%v second=%v", thiefFirst, thiefSecond)
	}
	atFighterFirst, _ := profiles.FreeGrants(7, 1, 1)
	atFighterSecond, _ := profiles.FreeGrants(7, 1, 2)
	if containsGrant(atFighterFirst, 242) || !containsGrant(atFighterSecond, 242) {
		t.Fatalf("at-fighter branch1 second-awakening grant mismatch: first=%v second=%v", atFighterFirst, atFighterSecond)
	}
	professionMasteryBranches := 0
	for job := byte(0); job <= 15; job++ {
		if job == 9 || job == 10 { // Dark Knight and Creator are external professions.
			continue
		}
		base, supported := profiles.FreeGrants(job, 0, 0)
		if !supported || containsGrant(base, 197) {
			t.Fatalf("ordinary job=%d base must not own profession mastery skill 197: grants=%v supported=%t", job, base, supported)
		}
		lastBranch := byte(4)
		if job == 8 { // Female mage owns the fifth ordinary class branch in this PVF.
			lastBranch = 5
		}
		for branch := byte(1); branch <= lastBranch; branch++ {
			grants, branchSupported := profiles.FreeGrants(job, branch, 0)
			if !branchSupported || !containsGrant(grants, 197) {
				t.Fatalf("ordinary job=%d branch=%d must grant profession mastery skill 197: grants=%v supported=%t", job, branch, grants, branchSupported)
			}
			professionMasteryBranches++
		}
	}
	if professionMasteryBranches != 57 {
		t.Fatalf("ordinary profession mastery branch count=%d want=57", professionMasteryBranches)
	}
	branchZeroFirstRequest, _ := ParseReward(2, "[awakening type]", []int64{1})
	branchZeroFirst, err := profiles.PlanTransition(9, 0x00, branchZeroFirstRequest)
	if err != nil || branchZeroFirst.NewGrowType != 0x10 {
		t.Fatalf("real PVF branch-zero first awakening = %+v, %v", branchZeroFirst, err)
	}
	branchZeroSecondRequest, _ := ParseReward(3, "[awakening type]", []int64{2})
	branchZeroSecond, err := profiles.PlanTransition(10, 0x10, branchZeroSecondRequest)
	if err != nil || branchZeroSecond.NewGrowType != 0x20 {
		t.Fatalf("real PVF branch-zero second awakening = %+v, %v", branchZeroSecond, err)
	}
	if _, err := profiles.PlanTransition(8, 0x00, branchZeroFirstRequest); err == nil {
		t.Fatal("ordinary branch-zero job unexpectedly exposes awakening capability")
	}
	t.Logf("profession snapshot=%+v fixed_level_skills=%d active_at_90=%d gunblader_class1_awaken1=%v", snapshot, fixedLevelSkills, fixedLevelAt90, gunbladerFirst)
}

func containsGrant(grants []Grant, skillID uint16) bool {
	for _, grant := range grants {
		if grant.SkillID == skillID {
			return true
		}
	}
	return false
}

func sameGrants(left []Grant, right []Grant) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
