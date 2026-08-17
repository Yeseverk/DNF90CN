package dnfbridge

import (
	"context"
	"os"
	"sort"
	"strings"
	"testing"

	"longheng.io/server/internal/modules/dnf/worldmap"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestRealScriptPVFTutorialCompletionShapes(t *testing.T) {
	pvfPath := os.Getenv("DNF_WORLDMAP_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNF_WORLDMAP_REAL_PVF_SMOKE to inspect tutorial completion shapes")
	}
	archive, err := platformpvf.Open(pvfPath)
	if err != nil {
		t.Fatal(err)
	}
	table, err := worldmap.LoadSource(context.Background(), archive, worldmap.Options{})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := newPVFDungeonTutorialScriptCatalog(archive)
	if err != nil {
		t.Fatal(err)
	}

	type completionShape struct {
		mapID       int64
		wantBoss    bool
		wantDummy   bool
		wantTargets bool
	}
	for _, shape := range []completionShape{
		{mapID: 53130},
		{mapID: 53138, wantDummy: true, wantTargets: true},
		{mapID: 76031, wantDummy: true, wantTargets: true},
		{mapID: 57805, wantTargets: true},
		{mapID: 91210, wantBoss: true, wantTargets: true},
	} {
		mapID := shape.mapID
		mapValue, ok := table.FindMap(mapID)
		if !ok {
			t.Fatalf("tutorial map %d was not loaded", mapID)
		}
		targets := make([]int, 0)
		for monsterIndex := range catalog.byMapID[mapID] {
			targets = append(targets, monsterIndex)
		}
		sort.Ints(targets)
		hasBoss := false
		hasDummy := false
		for _, monster := range mapValue.Monsters {
			rank := strings.ToLower(strings.Trim(monster.Rank, "[] \t\r\n"))
			hasBoss = hasBoss || rank == "boss"
			hasDummy = hasDummy || rank == "dummy"
		}
		t.Logf("map=%d path=%s monsters=%+v monster_teams=%v AI=%+v CMT_destroy_targets=%v", mapID, mapValue.Path, mapValue.Monsters, mapValue.MonsterTeam, mapValue.AICharacters, targets)
		if hasBoss != shape.wantBoss || hasDummy != shape.wantDummy || (len(targets) != 0) != shape.wantTargets {
			t.Fatalf("map=%d completion shape boss=%t dummy=%t targets=%v want boss=%t dummy=%t targets=%t",
				mapID, hasBoss, hasDummy, targets, shape.wantBoss, shape.wantDummy, shape.wantTargets)
		}
	}
}

func TestRealScriptPVFGunbladerBasicActionDestroyTarget(t *testing.T) {
	pvfPath := os.Getenv("DNF_WORLDMAP_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNF_WORLDMAP_REAL_PVF_SMOKE to inspect gunblader tutorial action")
	}
	archive, err := platformpvf.Open(pvfPath)
	if err != nil {
		t.Fatal(err)
	}
	table, err := worldmap.LoadSource(context.Background(), archive, worldmap.Options{})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := newPVFDungeonTutorialScriptCatalog(archive)
	if err != nil {
		t.Fatal(err)
	}
	catalog.indexBasicActionDestroyTargets(archive, table)
	targets := catalog.BasicActionMonsterDestroyTargets(70577)
	if len(targets) != 1 || targets[0].MonsterIndex != 3 || targets[0].MonsterID != 70216 ||
		normalizeDungeonRuntimePath(targets[0].ActionPath) != "map/cataclysm/newtutorial/gunblader_m/action/70577.act" {
		t.Fatalf("gunblader 70577 action targets=%+v", targets)
	}
}
