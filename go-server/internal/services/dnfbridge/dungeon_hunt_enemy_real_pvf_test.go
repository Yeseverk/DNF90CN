package dnfbridge

import (
	"context"
	"os"
	"testing"

	"longheng.io/server/internal/modules/dnf/worldmap"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestRealScriptPVFMap76136Enemy13099Ownership(t *testing.T) {
	pvfPath := os.Getenv("DNF_WORLDMAP_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNF_WORLDMAP_REAL_PVF_SMOKE to audit real map 76136 enemy 13099 ownership")
	}
	archive, err := platformpvf.Open(pvfPath)
	if err != nil {
		t.Fatal(err)
	}
	table, err := worldmap.LoadSource(context.Background(), archive, worldmap.Options{})
	if err != nil {
		t.Fatal(err)
	}
	mapValue, ok := table.FindMap(76136)
	if !ok {
		t.Fatal("real PVF map 76136 is missing")
	}
	for index, monster := range mapValue.Monsters {
		t.Logf("ordinary index=%d id=%d rank=%q level=%d auto=%d position=%+v", index, monster.MonsterID, monster.Rank, monster.Level, monster.AutoLevel, monster.Position)
	}
	for objectIndex, object := range mapValue.SpecialPassiveObjects {
		t.Logf("special object index=%d id=%d position=(%d,%d) spawns=%+v", objectIndex, object.ObjectID, object.X, object.Y, object.Spawns)
	}
	for _, candidate := range table.Maps() {
		for index, monster := range candidate.Monsters {
			if monster.MonsterID == 13099 {
				t.Logf("enemy 13099 ordinary owner map=%d index=%d rank=%q position=%+v", candidate.ID, index, monster.Rank, monster.Position)
			}
		}
		for objectIndex, object := range candidate.SpecialPassiveObjects {
			for spawnIndex, spawn := range object.Spawns {
				if spawn.Code == 13099 {
					t.Logf("enemy 13099 special owner map=%d object_index=%d object_id=%d spawn_index=%d kind=%q level=%d", candidate.ID, objectIndex, object.ObjectID, spawnIndex, spawn.Kind, spawn.Level)
				}
			}
		}
		for index, actor := range candidate.AICharacters {
			if actor.Code == 13099 {
				t.Logf("enemy 13099 AI owner map=%d index=%d faction=%q type=%q", candidate.ID, index, actor.Faction, actor.AIType)
			}
		}
	}
}
