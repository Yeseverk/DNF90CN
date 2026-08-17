package adventuregroup

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"

	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestCalculateUsesCumulativePerLevelPointsAndExactBenefits(t *testing.T) {
	tables, err := Load(context.Background(), adventureMemorySource{
		CharacterManagementPath: managementDocument(
			"2 3 10 5 5 20",
			"10 30 50",
			"2",
			"1 5 2 10",
			"1 2 2 4",
			"1 7 2 9",
		),
	})
	if err != nil {
		t.Fatal(err)
	}

	point, err := tables.CharacterPoint(5)
	if err != nil {
		t.Fatal(err)
	}
	if point != 40 {
		t.Fatalf("level-5 point = %d, want 40", point)
	}

	summary, err := tables.Calculate([]Character{
		{Level: 1},
		{Level: 2},
		{Level: 3},
		{Level: 5},
		{Level: 5, Deleted: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if summary.TotalPoint != 70 || summary.ManageLevel != 2 {
		t.Fatalf("summary = %+v, want total=70 level=2", summary)
	}
	if summary.ExpBonusPercent != 10 || summary.GoldBonusPercent != 4 || summary.ManageOption != 9 {
		t.Fatalf("benefits = %+v", summary.Benefits)
	}
	if got := tables.BenefitsForLevel(3); got != (Benefits{}) {
		t.Fatalf("sparse level inherited benefits: %+v", got)
	}
	if got := tables.ManageLevelForPoint(9); got != 0 {
		t.Fatalf("point 9 level = %d, want 0", got)
	}
	if got := tables.ManageLevelForPoint(10); got != 1 {
		t.Fatalf("point 10 level = %d, want 1", got)
	}
	if got := tables.ManageLevelForPoint(50); got != 2 {
		t.Fatalf("max-capped level = %d, want 2", got)
	}

	levelsSummary, err := tables.CalculateLevels([]int{2, 3, 5})
	if err != nil {
		t.Fatal(err)
	}
	if levelsSummary.TotalPoint != 70 || levelsSummary.ManageLevel != 2 {
		t.Fatalf("level-only summary = %+v", levelsSummary)
	}
	if got := tables.Snapshot(); got != (Snapshot{PointRanges: 2, ManageThresholds: 3, ManageLevelMax: 2, ExpBonusLevels: 2, GoldBonusLevels: 2, ManageOptionLevels: 2}) {
		t.Fatalf("snapshot = %+v", got)
	}
}

func TestCurrentShapeAllowsEmptyExpAndGoldTables(t *testing.T) {
	tables, err := Load(context.Background(), adventureMemorySource{
		CharacterManagementPath: managementDocument("40 40 100", "100", "1", "", "", "1 10"),
	})
	if err != nil {
		t.Fatal(err)
	}
	summary, err := tables.CalculateLevels([]int{40})
	if err != nil {
		t.Fatal(err)
	}
	if summary != (Summary{TotalPoint: 100, ManageLevel: 1, Benefits: Benefits{ManageOption: 10}}) {
		t.Fatalf("summary = %+v", summary)
	}
}

func TestLoadRejectsMalformedTypedSections(t *testing.T) {
	valid := managementDocument("2 3 10", "10 30", "2", "1 5", "1 2", "1 7")
	tests := []struct {
		name string
		doc  string
		want error
	}{
		{name: "missing section", doc: strings.Replace(valid, "[exp bonus]\n1 5\n[/exp bonus]\n", "", 1), want: ErrTableEmpty},
		{name: "duplicate section", doc: valid + "\n[gold bonus]\n[/gold bonus]\n", want: ErrTableMalformed},
		{name: "point tuple", doc: managementDocument("2 3", "10 30", "2", "1 5", "1 2", "1 7"), want: ErrTableMalformed},
		{name: "point overlap", doc: managementDocument("2 3 10 3 4 20", "10 30", "2", "1 5", "1 2", "1 7"), want: ErrTableMalformed},
		{name: "negative point", doc: managementDocument("2 3 -1", "10 30", "2", "1 5", "1 2", "1 7"), want: ErrTableMalformed},
		{name: "threshold order", doc: managementDocument("2 3 10", "30 10", "2", "1 5", "1 2", "1 7"), want: ErrTableMalformed},
		{name: "max arity", doc: managementDocument("2 3 10", "10 30", "2 3", "1 5", "1 2", "1 7"), want: ErrTableMalformed},
		{name: "odd pair", doc: managementDocument("2 3 10", "10 30", "2", "1", "1 2", "1 7"), want: ErrTableMalformed},
		{name: "duplicate pair level", doc: managementDocument("2 3 10", "10 30", "2", "1 5 1 6", "1 2", "1 7"), want: ErrTableMalformed},
		{name: "pair level above max", doc: managementDocument("2 3 10", "10 30", "2", "3 5", "1 2", "1 7"), want: ErrTableMalformed},
		{name: "non integer", doc: managementDocument("2 3 10", "10 30", "2", "1 5.5", "1 2", "1 7"), want: ErrTableMalformed},
		{name: "empty option", doc: managementDocument("2 3 10", "10 30", "2", "1 5", "1 2", ""), want: ErrTableEmpty},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Load(context.Background(), adventureMemorySource{CharacterManagementPath: test.doc})
			if !errors.Is(err, test.want) {
				t.Fatalf("Load error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestCalculateRejectsInvalidLevelsAndPointOverflow(t *testing.T) {
	tables, err := Load(context.Background(), adventureMemorySource{
		CharacterManagementPath: managementDocument("1 1 10", "10", "1", "", "", "1 10"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tables.Calculate([]Character{{Level: 0}}); !errors.Is(err, ErrCharacterLevel) {
		t.Fatalf("zero-level error = %v", err)
	}
	if summary, err := tables.Calculate([]Character{{Level: 0, Deleted: true}}); err != nil || summary != (Summary{}) {
		t.Fatalf("deleted invalid row should not contribute: summary=%+v err=%v", summary, err)
	}

	largePoint := fmt.Sprintf("1 1 %d", int64(math.MaxInt64))
	overflowTables, err := Load(context.Background(), adventureMemorySource{
		CharacterManagementPath: managementDocument(largePoint, "1", "1", "", "", "1 1"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := overflowTables.CalculateLevels([]int{1, 1, 1}); !errors.Is(err, ErrPointOverflow) {
		t.Fatalf("roster overflow error = %v", err)
	}

	rangePoint := fmt.Sprintf("1 3 %d", int64(math.MaxInt64))
	rangeTables, err := Load(context.Background(), adventureMemorySource{
		CharacterManagementPath: managementDocument(rangePoint, "1", "1", "", "", "1 1"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rangeTables.CharacterPoint(3); !errors.Is(err, ErrPointOverflow) {
		t.Fatalf("range overflow error = %v", err)
	}
}

func TestLoadHonorsContextAndSourceErrors(t *testing.T) {
	if _, err := Load(context.Background(), nil); err == nil {
		t.Fatal("nil source accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Load(ctx, adventureMemorySource{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context error = %v", err)
	}
	if _, err := Load(context.Background(), adventureMemorySource{}); !errors.Is(err, platformpvf.ErrFileNotFound) {
		t.Fatalf("missing source error = %v", err)
	}
}

func TestRealScriptPVFAdventureGroupTables(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to verify real adventure-group tables")
	}
	archive, err := platformpvf.LoadArchive(platformpvf.Options{Path: pvfPath})
	if err != nil {
		t.Fatal(err)
	}
	tables, err := LoadComplete(context.Background(), archive)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := tables.Snapshot()
	wantSnapshot := Snapshot{
		PointRanges:        61,
		ManageThresholds:   13,
		ManageLevelMax:     17,
		ExpBonusLevels:     0,
		GoldBonusLevels:    0,
		ManageOptionLevels: 11,
	}
	if snapshot != wantSnapshot {
		t.Fatalf("real snapshot = %+v, want %+v", snapshot, wantSnapshot)
	}
	runtime := tables.Runtime()
	if len(runtime.ShopCategories) != 3 || len(runtime.ExpeditionAreas) != 4 ||
		runtime.Capsule.MinimumExperience != 11691495 ||
		runtime.Capsule.GrantedExperience != 1000000 {
		t.Fatalf("real runtime config = %+v", runtime)
	}
	point, err := tables.CharacterPoint(86)
	if err != nil {
		t.Fatal(err)
	}
	if point != 10785 {
		t.Fatalf("real Lv86 point = %d, want 10785", point)
	}
	one, err := tables.CalculateLevels([]int{86})
	if err != nil {
		t.Fatal(err)
	}
	if one != (Summary{TotalPoint: 10785, ManageLevel: 2, Benefits: Benefits{ManageOption: 30}}) {
		t.Fatalf("real one-Lv86 summary = %+v", one)
	}
	eight, err := tables.CalculateLevels([]int{86, 86, 86, 86, 86, 86, 86, 86})
	if err != nil {
		t.Fatal(err)
	}
	if eight != (Summary{TotalPoint: 86280, ManageLevel: 7, Benefits: Benefits{ManageOption: 125}}) {
		t.Fatalf("real eight-Lv86 summary = %+v", eight)
	}
	currentAccount, err := tables.CalculateLevels([]int{86, 90, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1})
	if err != nil {
		t.Fatal(err)
	}
	if currentAccount != (Summary{TotalPoint: 23310, ManageLevel: 4, Benefits: Benefits{ManageOption: 70}}) {
		t.Fatalf("real current-account 16-character summary = %+v", currentAccount)
	}
	zero, err := tables.Calculate([]Character{{Level: 100, Deleted: true}})
	if err != nil || zero != (Summary{}) {
		t.Fatalf("real deleted-only summary = %+v err=%v", zero, err)
	}
	t.Logf("real adventure-group snapshot=%+v oneLv86=%+v eightLv86=%+v currentAccount=%+v", snapshot, one, eight, currentAccount)
}

type adventureMemorySource map[string]string

func (s adventureMemorySource) ReadText(path string) (string, error) {
	value, ok := s[path]
	if !ok {
		return "", fmt.Errorf("%w: %s", platformpvf.ErrFileNotFound, path)
	}
	return value, nil
}

func managementDocument(point, thresholds, maxLevel, exp, gold, option string) string {
	return fmt.Sprintf(`[point bonus]
%s
[/point bonus]
[manage level point]
%s
[/manage level point]
[manage level max]
%s
[exp bonus]
%s
[/exp bonus]
[gold bonus]
%s
[/gold bonus]
[manage option]
%s
[/manage option]
`, point, thresholds, maxLevel, exp, gold, option)
}
