package dnfbridge

import (
	"os"
	"slices"
	"testing"

	"longheng.io/server/internal/modules/dnf/worldmap"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestParseDungeonCinematicDestroyTargetsBindsSubjectMonsterOnly(t *testing.T) {
	document, err := worldmap.ParseDocument("cinematic/test.cmt", `
[MAP]
57796
[SCENE]
[BEHAVIOR]
[ACTOR]
[TYPE]
[MONSTER]
[INDEX]
0
[/ACTOR]
[DESTROY]
[IS SHOW DIE]
0
[/DESTROY]
[/BEHAVIOR]
[BEHAVIOR]
[ACTOR]
[TYPE]
[PASSIVE OBJECT]
[INDEX]
1
[/ACTOR]
[DESTROY]
[/DESTROY]
[/BEHAVIOR]
[BEHAVIOR]
[ACTOR]
[TYPE]
[MONSTER]
[INDEX]
2
[/ACTOR]
[MOVE]
[ACTOR]
[TYPE]
[WAY POINT]
[INDEX]
9
[/ACTOR]
[/MOVE]
[/BEHAVIOR]
[BEHAVIOR]
[ACTOR]
[TYPE]
[MONSTER]
[INDEX]
3
[/ACTOR]
[DESTROY]
[/DESTROY]
[/BEHAVIOR]
[/SCENE]
`)
	if err != nil {
		t.Fatal(err)
	}
	usage, ok := parseDungeonCinematicMonsterUsage(document)
	if !ok || usage.MapID != 57796 {
		t.Fatalf("cinematic parse usage=%+v ok=%t", usage, ok)
	}
	if !slices.Equal(usage.MonsterActorIndexes, []int{0, 2, 3}) {
		t.Fatalf("monster actor indexes=%v", usage.MonsterActorIndexes)
	}
	if !slices.Equal(usage.DestroyMonsterIndexes, []int{0, 3}) {
		t.Fatalf("destroy monster indexes=%v", usage.DestroyMonsterIndexes)
	}
	mapID, indexes, ok := parseDungeonCinematicDestroyTargets(document)
	if !ok || mapID != usage.MapID || !slices.Equal(indexes, usage.DestroyMonsterIndexes) {
		t.Fatalf("compat destroy parse map=%d indexes=%v ok=%t usage=%+v", mapID, indexes, ok, usage)
	}
}

func TestParseDungeonBasicActionDestroyUsageFindsGunbladerTutorialTarget(t *testing.T) {
	document, err := worldmap.ParseDocument("map/cataclysm/newtutorial/gunblader_m/action/70577.act", `
[TRIGGER]
[ON SUCCESS TUTORIAL KEY]
6
[WHICH]
[MONSTER]
[CHECKUP]
[IS INDEX]
70216
[/IS INDEX]
[/CHECKUP]
[DO BEHAVIOR]
[CHECKUP OBJECT]
3
[DO BEHAVIOR]
[ME]
4
[/TRIGGER]
[BEHAVIOR]
[CINEMATIC]
6803
[/BEHAVIOR]
[BEHAVIOR]
[CINEMATIC]
6802
[/BEHAVIOR]
[BEHAVIOR]
[TUTORIAL KEY]
[/TUTORIAL KEY]
[/BEHAVIOR]
[BEHAVIOR]
[DESTROY]
[/BEHAVIOR]
[BEHAVIOR]
[CREATE PASSIVEOBJECT]
[/CREATE PASSIVEOBJECT]
[/BEHAVIOR]
`)
	if err != nil {
		t.Fatal(err)
	}
	usage, ok := parseDungeonBasicActionDestroyUsage(document)
	if !ok || !slices.Equal(usage.MonsterIDs, []int64{70216}) || !slices.Equal(usage.BehaviorIndexes, []int{3}) {
		t.Fatalf("usage=%+v ok=%t", usage, ok)
	}
	if got := resolveDungeonBasicActionPath(
		"Cataclysm/NewTutorial/GunBlader_M/70577.map",
		"Action/70577.act",
	); got != "Cataclysm/NewTutorial/GunBlader_M/Action/70577.act" {
		t.Fatalf("resolved action path=%q", got)
	}
}

func TestPVFDungeonTutorialScriptCatalogIndexesExplicitMonsterDestroyTargets(t *testing.T) {
	source := bridgePVFSource{
		defaultDungeonCinematicList: "1001 `Dungeon/Test/first.cmt`\n" +
			"1002 `Dungeon/Test/second.cmt`\n" +
			"1003 `Dungeon/Test/missing.cmt`\n",
		"cinematic/Dungeon/Test/first.cmt": "[MAP]\n100\n[SCENE]\n[BEHAVIOR]\n" +
			"[ACTOR]\n[TYPE]\n[MONSTER]\n[INDEX]\n0\n[/ACTOR]\n[DESTROY]\n[/DESTROY]\n[/BEHAVIOR]\n[/SCENE]\n",
		"cinematic/Dungeon/Test/second.cmt": "[MAP]\n100\n[SCENE]\n" +
			"[BEHAVIOR]\n[ACTOR]\n[TYPE]\n[PASSIVE OBJECT]\n[INDEX]\n1\n[/ACTOR]\n[DESTROY]\n[/DESTROY]\n[/BEHAVIOR]\n" +
			"[BEHAVIOR]\n[ACTOR]\n[TYPE]\n[MONSTER]\n[INDEX]\n2\n[/ACTOR]\n[DESTROY]\n[/DESTROY]\n[/BEHAVIOR]\n[/SCENE]\n",
	}
	catalog, err := newPVFDungeonTutorialScriptCatalog(source)
	if err != nil {
		t.Fatal(err)
	}
	first, ok := catalog.FindMonsterDestroy(100, 0)
	if !ok || first.CinematicID != 1001 || first.MonsterIndex != 0 {
		t.Fatalf("first destroy evidence=(%+v,%t)", first, ok)
	}
	if !slices.Equal(first.MonsterActorIndexes, []int{0}) ||
		!slices.Equal(first.DestroyMonsterIndexes, []int{0}) {
		t.Fatalf("first cinematic actor evidence=%+v", first)
	}
	second, ok := catalog.FindMonsterDestroy(100, 2)
	if !ok || second.CinematicID != 1002 || second.MonsterIndex != 2 {
		t.Fatalf("second destroy evidence=(%+v,%t)", second, ok)
	}
	if !slices.Equal(second.MonsterActorIndexes, []int{2}) ||
		!slices.Equal(second.DestroyMonsterIndexes, []int{2}) {
		t.Fatalf("second cinematic actor evidence=%+v", second)
	}
	if evidence, ok := catalog.FindMonsterDestroy(100, 1); ok {
		t.Fatalf("passive-object destroy indexed as monster: %+v", evidence)
	}
	if _, ok := catalog.FindMonsterDestroy(101, 0); ok {
		t.Fatal("destroy target crossed map ownership")
	}
	if !catalog.HasMonsterDestroyTargets(100) || catalog.HasMonsterDestroyTargets(101) ||
		catalog.HasMonsterDestroyTargets(0) {
		t.Fatalf("map target ownership map100=%t map101=%t map0=%t",
			catalog.HasMonsterDestroyTargets(100),
			catalog.HasMonsterDestroyTargets(101),
			catalog.HasMonsterDestroyTargets(0))
	}
	snapshot := catalog.Snapshot()
	if snapshot.CinematicEntries != 3 || snapshot.CinematicsWithTargets != 2 ||
		snapshot.MapsWithTargets != 1 || snapshot.MonsterTargets != 2 || snapshot.ReadFailures != 1 {
		t.Fatalf("catalog snapshot=%+v", snapshot)
	}
}

func TestPVFDungeonTutorialScriptCatalogDoesNotMergeMonsterActorsAcrossCinematics(t *testing.T) {
	source := bridgePVFSource{
		defaultDungeonCinematicList: "2001 `Dungeon/Test/left.cmt`\n" +
			"2002 `Dungeon/Test/right.cmt`\n",
		"cinematic/Dungeon/Test/left.cmt": "[MAP]\n200\n[SCENE]\n" +
			"[BEHAVIOR]\n[ACTOR]\n[TYPE]\n[MONSTER]\n[INDEX]\n0\n[/ACTOR]\n[DESTROY]\n[/DESTROY]\n[/BEHAVIOR]\n" +
			"[BEHAVIOR]\n[ACTOR]\n[TYPE]\n[MONSTER]\n[INDEX]\n1\n[/ACTOR]\n[/BEHAVIOR]\n[/SCENE]\n",
		"cinematic/Dungeon/Test/right.cmt": "[MAP]\n200\n[SCENE]\n" +
			"[BEHAVIOR]\n[ACTOR]\n[TYPE]\n[MONSTER]\n[INDEX]\n0\n[/ACTOR]\n[DESTROY]\n[/DESTROY]\n[/BEHAVIOR]\n" +
			"[BEHAVIOR]\n[ACTOR]\n[TYPE]\n[MONSTER]\n[INDEX]\n2\n[/ACTOR]\n[/BEHAVIOR]\n[/SCENE]\n",
	}
	catalog, err := newPVFDungeonTutorialScriptCatalog(source)
	if err != nil {
		t.Fatal(err)
	}
	if candidates := catalog.byMapID[200][0]; len(candidates) != 2 {
		t.Fatalf("same destroy key candidates=%+v", candidates)
	}
	left, ok := catalog.FindMonsterDestroyCovering(200, 0, []int{1})
	if !ok || left.CinematicID != 2001 {
		t.Fatalf("left coverage=(%+v,%t)", left, ok)
	}
	right, ok := catalog.FindMonsterDestroyCovering(200, 0, []int{2})
	if !ok || right.CinematicID != 2002 {
		t.Fatalf("right coverage=(%+v,%t)", right, ok)
	}
	if evidence, ok := catalog.FindMonsterDestroyCovering(200, 0, []int{1, 2}); ok {
		t.Fatalf("separate cinematic actor sets were merged: %+v", evidence)
	}
	compat, ok := catalog.FindMonsterDestroy(200, 0)
	if !ok || compat.CinematicID != 2001 || !slices.Equal(compat.DestroyMonsterIndexes, []int{0}) {
		t.Fatalf("explicit destroy compatibility=(%+v,%t)", compat, ok)
	}
}

func TestPVFDungeonTutorialScriptCatalogFindsLaterCandidateWithCompleteCoverage(t *testing.T) {
	source := bridgePVFSource{
		defaultDungeonCinematicList: "2101 `Dungeon/Test/narrow.cmt`\n" +
			"2102 `Dungeon/Test/complete.cmt`\n",
		"cinematic/Dungeon/Test/narrow.cmt": "[MAP]\n201\n[SCENE]\n" +
			"[BEHAVIOR]\n[ACTOR]\n[TYPE]\n[MONSTER]\n[INDEX]\n0\n[/ACTOR]\n[DESTROY]\n[/DESTROY]\n[/BEHAVIOR]\n" +
			"[BEHAVIOR]\n[ACTOR]\n[TYPE]\n[MONSTER]\n[INDEX]\n1\n[/ACTOR]\n[/BEHAVIOR]\n[/SCENE]\n",
		"cinematic/Dungeon/Test/complete.cmt": "[MAP]\n201\n[SCENE]\n" +
			"[BEHAVIOR]\n[ACTOR]\n[TYPE]\n[MONSTER]\n[INDEX]\n0\n[/ACTOR]\n[DESTROY]\n[/DESTROY]\n[/BEHAVIOR]\n" +
			"[BEHAVIOR]\n[ACTOR]\n[TYPE]\n[MONSTER]\n[INDEX]\n1\n[/ACTOR]\n[/BEHAVIOR]\n" +
			"[BEHAVIOR]\n[ACTOR]\n[TYPE]\n[MONSTER]\n[INDEX]\n2\n[/ACTOR]\n[/BEHAVIOR]\n[/SCENE]\n",
	}
	catalog, err := newPVFDungeonTutorialScriptCatalog(source)
	if err != nil {
		t.Fatal(err)
	}
	evidence, ok := catalog.FindMonsterDestroyCovering(201, 0, []int{2, 1, 2})
	if !ok || evidence.CinematicID != 2102 ||
		!slices.Equal(evidence.MonsterActorIndexes, []int{0, 1, 2}) ||
		!slices.Equal(evidence.DestroyMonsterIndexes, []int{0}) {
		t.Fatalf("complete coverage candidate=(%+v,%t)", evidence, ok)
	}
	if evidence, ok := catalog.FindMonsterDestroyCovering(201, 0, []int{-1}); ok {
		t.Fatalf("negative remaining index accepted: %+v", evidence)
	}
}

func TestRealScriptPVFTutorialCinematicDestroyTargets(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to verify real tutorial cinematic destroy targets")
	}
	archive, err := platformpvf.LoadArchive(platformpvf.Options{Path: pvfPath})
	if err != nil {
		t.Fatalf("load real Script.pvf: %v", err)
	}
	catalog, err := newPVFDungeonTutorialScriptCatalog(archive)
	if err != nil {
		t.Fatalf("build real tutorial cinematic catalog: %v", err)
	}
	wants := []struct {
		mapID        int64
		monsterIndex int
		cinematicID  int64
	}{
		{57796, 0, 6401},
		{57800, 0, 6412},
		{57800, 1, 6412},
		{57800, 9, 6412},
		{91210, 4, 5210},
	}
	for _, want := range wants {
		evidence, ok := catalog.FindMonsterDestroy(want.mapID, want.monsterIndex)
		if !ok || evidence.CinematicID != want.cinematicID {
			t.Errorf("real destroy target map=%d monster_index=%d evidence=(%+v,%t) want_cinematic=%d",
				want.mapID, want.monsterIndex, evidence, ok, want.cinematicID)
		}
	}
	if evidence, ok := catalog.FindMonsterDestroy(57796, 1); ok {
		t.Errorf("nonexistent Knight_F start monster index was indexed: %+v", evidence)
	}
	escape, ok := catalog.FindMonsterDestroyCovering(91210, 4, []int{0, 1, 2, 3, 5})
	if !ok || escape.CinematicID != 5210 ||
		!slices.Equal(escape.MonsterActorIndexes, []int{0, 1, 2, 3, 4, 5}) ||
		!slices.Equal(escape.DestroyMonsterIndexes, []int{4}) {
		t.Errorf("real CMT 5210 coverage evidence=(%+v,%t)", escape, ok)
	}
	if evidence, ok := catalog.FindMonsterDestroyCovering(91210, 4, []int{0, 1, 2, 3, 5, 6}); ok {
		t.Errorf("real CMT 5210 covered a non-cinematic monster index: %+v", evidence)
	}
	t.Logf("real tutorial cinematic catalog: %+v", catalog.Snapshot())
}
