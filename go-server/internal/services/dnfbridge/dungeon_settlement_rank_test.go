package dnfbridge

import (
	"errors"
	"os"
	"testing"
	"time"

	"longheng.io/server/internal/modules/dnf/worldmap"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestCurrentDungeonClearRankCatalogUsesExactPVFThresholds(t *testing.T) {
	catalog, err := loadCurrentDungeonClearRankCatalog(bridgePVFSource{
		currentDungeonRankSystemPVFPath: "[rank grade]\n99 90 80 60 50 30 20 10\n[/rank grade]\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		point byte
		want  byte
	}{
		{point: 99, want: 99},
		{point: 98, want: 90},
		{point: 80, want: 80},
		{point: 73, want: 60},
		{point: 10, want: 10},
		{point: 9, want: 0},
	} {
		if got := catalog.GradeForPoint(test.point); got != test.want {
			t.Fatalf("point=%d grade=%d want=%d", test.point, got, test.want)
		}
	}
}

func TestCurrentDungeonClearRankCatalogRejectsMissingAndInvalidPVFSections(t *testing.T) {
	for _, test := range []struct {
		name string
		text string
	}{
		{name: "missing", text: "[other]\n1\n[/other]\n"},
		{name: "ascending", text: "[rank grade]\n90 99\n[/rank grade]\n"},
		{name: "range", text: "[rank grade]\n256\n[/rank grade]\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := loadCurrentDungeonClearRankCatalog(bridgePVFSource{currentDungeonRankSystemPVFPath: test.text})
			if !errors.Is(err, errCurrentDungeonClearRankCatalogInvalid) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestRealScriptPVFCurrentDungeonClearRankCatalog(t *testing.T) {
	pvfPath := os.Getenv("DNF_WORLDMAP_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNF_WORLDMAP_REAL_PVF_SMOKE to verify real settlement rank thresholds")
	}
	archive, err := platformpvf.Open(pvfPath)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := loadCurrentDungeonClearRankCatalog(archive)
	if err != nil {
		t.Fatal(err)
	}
	if got := catalog.GradeForPoint(73); got != 60 {
		t.Fatalf("real Script.pvf rank point 73 grade=%d want=60", got)
	}
}

func TestCurrentDungeonSettlementPresentationUsesFrozenRuntimeDomains(t *testing.T) {
	catalog, err := loadCurrentDungeonClearRankCatalog(bridgePVFSource{
		currentDungeonRankSystemPVFPath: "[rank grade]\n99 90 80 60 50 30 20 10\n[/rank grade]\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	startedAt := time.Unix(1700000000, 0)
	completedAt := startedAt.Add(12*time.Second + 345*time.Millisecond)
	run := worldmap.DungeonRunSnapshot{
		Status:  worldmap.DungeonRunCompleted,
		Visited: []worldmap.RoomCoordinate{{X: 0, Y: 0}, {X: 1, Y: 0}},
		Cleared: []worldmap.RoomCoordinate{{X: 0, Y: 0}, {X: 1, Y: 0}},
	}
	presentation, err := currentDungeonSettlementPresentationForRun(startedAt, completedAt, run, catalog, 73)
	if err != nil {
		t.Fatal(err)
	}
	if presentation.RankGrade != 60 || presentation.ClearTimeMS != 12345 ||
		presentation.TimeBonusPoint != 0 || !presentation.AllVisitedClear {
		t.Fatalf("presentation=%+v", presentation)
	}
}

func TestCurrentDungeonSettlementPresentationRejectsUnsafeClockOrIncompleteRun(t *testing.T) {
	catalog := currentDungeonClearRankCatalog{gradeThresholds: []byte{99}}
	startedAt := time.Unix(1700000000, 0)
	completedAt := startedAt.Add(-time.Millisecond)
	_, err := currentDungeonSettlementPresentationForRun(
		startedAt,
		completedAt,
		worldmap.DungeonRunSnapshot{Status: worldmap.DungeonRunCompleted},
		catalog,
		99,
	)
	if !errors.Is(err, errCurrentDungeonSettlementPresentationBad) {
		t.Fatalf("clock reversal error=%v", err)
	}
	_, err = currentDungeonSettlementPresentationForRun(
		startedAt,
		startedAt,
		worldmap.DungeonRunSnapshot{Status: worldmap.DungeonRunActive},
		catalog,
		99,
	)
	if !errors.Is(err, errCurrentDungeonSettlementPresentationBad) {
		t.Fatalf("incomplete run error=%v", err)
	}
}

func TestCurrentDungeonAllVisitedRoomsClearedRejectsDuplicateOrMissingRoom(t *testing.T) {
	run := worldmap.DungeonRunSnapshot{
		Status:  worldmap.DungeonRunCompleted,
		Visited: []worldmap.RoomCoordinate{{X: 0, Y: 0}, {X: 1, Y: 0}},
		Cleared: []worldmap.RoomCoordinate{{X: 0, Y: 0}, {X: 0, Y: 0}},
	}
	if currentDungeonAllVisitedRoomsCleared(run) {
		t.Fatal("duplicate/missing clear was accepted")
	}
}
