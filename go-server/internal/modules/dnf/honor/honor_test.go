package honor

import (
	"errors"
	"fmt"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"

	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestTablesParseRepeatedOrdinaryGradesAndResolveExpertProgress(t *testing.T) {
	tables, err := LoadTables(honorMemorySource{TablePath: honorTestDocument()})
	if err != nil {
		t.Fatal(err)
	}
	wantSnapshot := Snapshot{
		OrdinaryGrades:         2,
		OrdinaryLevels:         3,
		MaxOrdinaryLevel:       3,
		MaxLevelExperience:     29,
		MaxTotalExperience:     59,
		ExpertGrades:           2,
		ExpertExperienceRows:   3,
		ExpertCalculationReady: true,
	}
	if snapshot := tables.Snapshot(); snapshot != wantSnapshot {
		t.Fatalf("snapshot=%+v want=%+v", snapshot, wantSnapshot)
	}
	if tables.MaxOrdinaryLevel() != 3 || tables.MaxLevelExperience() != 29 || tables.MaxTotalExperience() != 59 {
		t.Fatalf("ordinary maxima level=%d level_exp=%d total_exp=%d", tables.MaxOrdinaryLevel(), tables.MaxLevelExperience(), tables.MaxTotalExperience())
	}

	grades := tables.OrdinaryGrades()
	if len(grades) != 2 || grades[0].Grade != 1 || grades[1].Grade != 2 ||
		grades[0].EffectPath != "effect/one.ani" ||
		grades[0].Medal != (ImageReference{Path: "medal.img", Index: 0}) ||
		grades[1].Icon != (ImageReference{Path: "icon.img", Index: 1}) ||
		!reflect.DeepEqual(grades[0].Levels, []LevelRequirement{
			{Level: 1, RequiredExperience: 0},
			{Level: 2, RequiredExperience: 10},
		}) ||
		!reflect.DeepEqual(grades[1].Levels, []LevelRequirement{{Level: 3, RequiredExperience: 20}}) {
		t.Fatalf("ordinary grades=%+v", grades)
	}
	grades[0].Levels[0].RequiredExperience = 999
	if fresh := tables.OrdinaryGrades(); fresh[0].Levels[0].RequiredExperience != 0 {
		t.Fatal("ordinary grade accessor leaked mutable level slice")
	}

	expertGrades := tables.ExpertGrades()
	if !reflect.DeepEqual(expertGrades, []ExpertGradeInfo{
		{Grade: 0, Name: "challenger", MinLevel: 0, MaxLevel: 0},
		{
			Grade: 1, Name: "veteran",
			Medal: ImageReference{Path: "expert-medal.img", Index: 2}, HasMedal: true,
			Icon: ImageReference{Path: "expert-icon.img", Index: 3}, HasIcon: true,
			MinLevel: 1, MaxLevel: -1,
		},
	}) {
		t.Fatalf("expert grades=%+v", expertGrades)
	}
	wantExpertRows := []ExpertExperienceRow{
		{Level: 1, Experience: 100},
		{Level: 2, Experience: 204},
		{Level: 3, Experience: 9999999999},
	}
	if rows := tables.ExpertExperienceRows(); !reflect.DeepEqual(rows, wantExpertRows) {
		t.Fatalf("expert rows=%+v", rows)
	}
	if !tables.Snapshot().ExpertCalculationReady {
		t.Fatal("expert progression calculation is unavailable")
	}

	for level, wantRequired := range map[int]uint64{1: 0, 2: 10, 3: 20} {
		if got, found := tables.RequiredExperienceToEnter(level); !found || got != wantRequired {
			t.Fatalf("required level=%d got=%d,%t want=%d", level, got, found, wantRequired)
		}
	}
	if _, found := tables.RequiredExperienceToEnter(4); found {
		t.Fatal("out-of-table ordinary requirement fabricated")
	}
	for level, wantGrade := range map[int]int{1: 1, 2: 1, 3: 2} {
		if got, found := tables.GradeForLevel(level); !found || got != wantGrade {
			t.Fatalf("grade level=%d got=%d,%t want=%d", level, got, found, wantGrade)
		}
	}
}

func TestTablesAdvanceExpertUsesPerLevelThresholds(t *testing.T) {
	tables, err := LoadTables(honorMemorySource{TablePath: honorTestDocument()})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		state ExpertProgress
		gain  uint64
		want  ExpertProgress
	}{
		{name: "zero gain", state: ExpertProgress{}, gain: 0, want: ExpertProgress{}},
		{name: "partial level zero", state: ExpertProgress{}, gain: 99, want: ExpertProgress{CurrentLevelExperience: 99}},
		{name: "first level", state: ExpertProgress{CurrentLevelExperience: 99}, gain: 1, want: ExpertProgress{Level: 1}},
		{name: "cross multiple levels", state: ExpertProgress{}, gain: 304, want: ExpertProgress{Level: 2}},
		{name: "final level caps excess", state: ExpertProgress{Level: 2}, gain: 9999999999 + 17, want: ExpertProgress{Level: 3}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := tables.AdvanceExpert(test.state, test.gain)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("AdvanceExpert(%+v, %d) = %+v, want %+v", test.state, test.gain, got, test.want)
			}
		})
	}

	for _, state := range []ExpertProgress{
		{Level: 4},
		{CurrentLevelExperience: 100},
		{Level: 3, CurrentLevelExperience: 1},
	} {
		if _, err := tables.ResolveExpert(state); !errors.Is(err, ErrExpertStateInvalid) {
			t.Fatalf("ResolveExpert(%+v) error = %v", state, err)
		}
	}
}

func TestTablesResolveOrdinaryCumulativeExperience(t *testing.T) {
	tables, err := LoadTables(honorMemorySource{TablePath: honorTestDocument()})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		total uint64
		want  Progress
	}{
		{total: 0, want: Progress{TotalExperience: 0, Level: 1, CurrentLevelExperience: 0, Grade: 1}},
		{total: 9, want: Progress{TotalExperience: 9, Level: 1, CurrentLevelExperience: 9, Grade: 1}},
		{total: 10, want: Progress{TotalExperience: 10, Level: 2, CurrentLevelExperience: 0, Grade: 1}},
		{total: 29, want: Progress{TotalExperience: 29, Level: 2, CurrentLevelExperience: 19, Grade: 1}},
		{total: 30, want: Progress{TotalExperience: 30, Level: 3, CurrentLevelExperience: 0, Grade: 2}},
		{total: 59, want: Progress{TotalExperience: 59, Level: 3, CurrentLevelExperience: 29, Grade: 2}},
		{total: math.MaxUint64, want: Progress{TotalExperience: 59, Level: 3, CurrentLevelExperience: 29, Grade: 2}},
	}
	for _, test := range tests {
		got, err := tables.Resolve(test.total)
		if err != nil {
			t.Fatalf("total=%d: %v", test.total, err)
		}
		if got != test.want {
			t.Fatalf("total=%d got=%+v want=%+v", test.total, got, test.want)
		}
	}
	var nilTables *Tables
	if _, err := nilTables.Resolve(0); !errors.Is(err, ErrTablesRequired) {
		t.Fatalf("nil resolve error=%v", err)
	}
}

func TestCalculateHonorExperienceGainUsesCallerCharacterCap(t *testing.T) {
	tests := []struct {
		name     string
		level    int
		previous uint32
		gained   uint32
		cap      CharacterExperienceCap
		want     uint32
	}{
		{
			name:  "already caller max level",
			level: 70, previous: 123, gained: 1000,
			cap: CharacterExperienceCap{MaxLevel: 70, MaxLevelEntryExperience: 9000}, want: 1000,
		},
		{
			name:  "below caller threshold",
			level: 69, previous: 800, gained: 100,
			cap: CharacterExperienceCap{MaxLevel: 70, MaxLevelEntryExperience: 1000}, want: 0,
		},
		{
			name:  "exactly reaches caller threshold",
			level: 69, previous: 900, gained: 100,
			cap: CharacterExperienceCap{MaxLevel: 70, MaxLevelEntryExperience: 1000}, want: 0,
		},
		{
			name:  "only crossing overflow becomes honor",
			level: 69, previous: 900, gained: 250,
			cap: CharacterExperienceCap{MaxLevel: 70, MaxLevelEntryExperience: 1000}, want: 150,
		},
		{
			name:  "inconsistent pre-cap level above threshold keeps only new delta",
			level: 69, previous: 1100, gained: 25,
			cap: CharacterExperienceCap{MaxLevel: 70, MaxLevelEntryExperience: 1000}, want: 25,
		},
		{
			name:  "uint32 cumulative experience saturates",
			level: 69, previous: math.MaxUint32 - 10, gained: 100,
			cap: CharacterExperienceCap{MaxLevel: 70, MaxLevelEntryExperience: math.MaxUint32 - 20}, want: 10,
		},
		{
			name:  "zero gain",
			level: 89, previous: 900, gained: 0,
			cap: CharacterExperienceCap{MaxLevel: 90, MaxLevelEntryExperience: 1000}, want: 0,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := CalculateHonorExperienceGain(test.level, test.previous, test.gained, test.cap)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("got=%d want=%d", got, test.want)
			}
		})
	}
	for _, request := range []struct {
		level int
		cap   CharacterExperienceCap
	}{
		{level: 0, cap: CharacterExperienceCap{MaxLevel: 90, MaxLevelEntryExperience: 1000}},
		{level: 89, cap: CharacterExperienceCap{MaxLevel: 0, MaxLevelEntryExperience: 1000}},
		{level: 89, cap: CharacterExperienceCap{MaxLevel: 90, MaxLevelEntryExperience: 0}},
	} {
		if _, err := CalculateHonorExperienceGain(request.level, 0, 1, request.cap); !errors.Is(err, ErrCharacterCapInvalid) {
			t.Fatalf("request=%+v error=%v", request, err)
		}
	}
}

func TestTablesRejectMalformedOrPartialPVF(t *testing.T) {
	if _, err := LoadTables(nil); !errors.Is(err, ErrSourceRequired) {
		t.Fatalf("nil source error=%v", err)
	}
	tests := []struct {
		name   string
		mutate func(string) string
	}{
		{
			name: "missing ordinary grade",
			mutate: func(text string) string {
				start := strings.Index(text, "[grade]")
				end := strings.LastIndex(text, "[/grade]") + len("[/grade]")
				return text[:start] + text[end:]
			},
		},
		{
			name: "duplicate ordinary grade id",
			mutate: func(text string) string {
				return strings.Replace(text, "[grade]\n2 `effect/two.ani`", "[grade]\n1 `effect/two.ani`", 1)
			},
		},
		{
			name: "ordinary level gap",
			mutate: func(text string) string {
				return strings.Replace(text, "3 20", "4 20", 1)
			},
		},
		{
			name: "ordinary level one nonzero",
			mutate: func(text string) string {
				return strings.Replace(text, "1 0 2 10", "1 1 2 10", 1)
			},
		},
		{
			name: "ordinary positive level zero requirement",
			mutate: func(text string) string {
				return strings.Replace(text, "2 10", "2 0", 1)
			},
		},
		{
			name: "ordinary grade missing close",
			mutate: func(text string) string {
				return strings.Replace(text, "[/grade]\n[grade]", "[grade]", 1)
			},
		},
		{
			name: "duplicate maxexp section",
			mutate: func(text string) string {
				return strings.Replace(text, "[expert info]", "[maxexp on maxlevel]\n29\n[expert info]", 1)
			},
		},
		{
			name: "zero maxexp",
			mutate: func(text string) string {
				return strings.Replace(text, "[maxexp on maxlevel]\n29", "[maxexp on maxlevel]\n0", 1)
			},
		},
		{
			name: "missing expert info close",
			mutate: func(text string) string {
				return strings.Replace(text, "[/expert info]", "", 1)
			},
		},
		{
			name: "duplicate expert grade id",
			mutate: func(text string) string {
				return strings.Replace(text, "[grade info]\n1 `veteran`", "[grade info]\n0 `veteran`", 1)
			},
		},
		{
			name: "expert grade missing icon",
			mutate: func(text string) string {
				return strings.Replace(text, "[icon img]\n`expert-icon.img` 3\n", "", 1)
			},
		},
		{
			name: "expert grade invalid range",
			mutate: func(text string) string {
				return strings.Replace(text, "[max lv]\n-1", "[max lv]\n0", 1)
			},
		},
		{
			name: "expert exp level gap",
			mutate: func(text string) string {
				return strings.Replace(text, "2 `204`", "3 `204`", 1)
			},
		},
		{
			name: "expert exp nonnumeric",
			mutate: func(text string) string {
				return strings.Replace(text, "2 `204`", "2 `unknown`", 1)
			},
		},
		{
			name: "expert exp missing close",
			mutate: func(text string) string {
				return strings.Replace(text, "[/honor expert exp table]", "", 1)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := LoadTables(honorMemorySource{TablePath: test.mutate(honorTestDocument())})
			if !errors.Is(err, ErrSectionShape) && !errors.Is(err, ErrSectionMissing) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestTablesRejectOrdinaryTotalOverflow(t *testing.T) {
	text := honorTestDocument()
	text = strings.Replace(text, "1 0 2 10", fmt.Sprintf("1 0 2 %d", int64(math.MaxInt64)), 1)
	text = strings.Replace(text, "3 20", fmt.Sprintf("3 %d", int64(math.MaxInt64)), 1)
	text = strings.Replace(text, "[maxexp on maxlevel]\n29", fmt.Sprintf("[maxexp on maxlevel]\n%d", int64(math.MaxInt64)), 1)
	if _, err := LoadTables(honorMemorySource{TablePath: text}); !errors.Is(err, ErrSectionShape) {
		t.Fatalf("overflow error=%v", err)
	}
}

func TestRealScriptPVFHonorTablesPreserveOrdinaryAndExpertSources(t *testing.T) {
	pvfPath := os.Getenv("DNF_HONOR_REAL_PVF_SMOKE")
	if pvfPath == "" {
		pvfPath = os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	}
	if pvfPath == "" {
		t.Skip("set DNF_HONOR_REAL_PVF_SMOKE to verify real honor PVF tables")
	}
	archive, err := platformpvf.Open(pvfPath)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := LoadTables(archive)
	if err != nil {
		t.Fatal(err)
	}
	wantSnapshot := Snapshot{
		OrdinaryGrades:         6,
		OrdinaryLevels:         59,
		MaxOrdinaryLevel:       59,
		MaxLevelExperience:     589999999,
		MaxTotalExperience:     17699999999,
		ExpertGrades:           7,
		ExpertExperienceRows:   289,
		ExpertCalculationReady: true,
	}
	if snapshot := tables.Snapshot(); snapshot != wantSnapshot {
		t.Fatalf("real snapshot=%+v want=%+v", snapshot, wantSnapshot)
	}
	progress, err := tables.Resolve(35000000)
	if err != nil {
		t.Fatal(err)
	}
	if progress != (Progress{TotalExperience: 35000000, Level: 3, CurrentLevelExperience: 5000000, Grade: 1}) {
		t.Fatalf("real progress=%+v", progress)
	}
	capped, err := tables.Resolve(math.MaxUint64)
	if err != nil {
		t.Fatal(err)
	}
	if capped != (Progress{TotalExperience: 17699999999, Level: 59, CurrentLevelExperience: 589999999, Grade: 6}) {
		t.Fatalf("real capped progress=%+v", capped)
	}
	rows := tables.ExpertExperienceRows()
	for level, want := range map[int]uint64{
		1:   1000000,
		2:   2040000,
		100: 48562450,
		200: 2452644029,
		289: 80464018485,
	} {
		if got := rows[level-1]; got != (ExpertExperienceRow{Level: level, Experience: want}) {
			t.Fatalf("real expert row level=%d got=%+v want=%d", level, got, want)
		}
	}
	expertGrades := tables.ExpertGrades()
	if expertGrades[0].HasMedal || expertGrades[0].HasIcon || expertGrades[0].MinLevel != 0 || expertGrades[0].MaxLevel != 0 {
		t.Fatalf("real expert grade zero=%+v", expertGrades[0])
	}
	if !expertGrades[6].HasMedal || !expertGrades[6].HasIcon || expertGrades[6].MinLevel != 200 || expertGrades[6].MaxLevel != -1 {
		t.Fatalf("real expert grade six=%+v", expertGrades[6])
	}
}

type honorMemorySource map[string]string

func (source honorMemorySource) ReadText(path string) (string, error) {
	text, found := source[path]
	if !found {
		return "", os.ErrNotExist
	}
	return text, nil
}

func honorTestDocument() string {
	return `[grade]
1 ` + "`effect/one.ani` `medal.img` 0 `icon.img` 0" + ` 1 0 2 10
[/grade]
[grade]
2 ` + "`effect/two.ani` `medal.img` 2 `icon.img` 1" + ` 3 20
[/grade]
[maxexp on maxlevel]
29
[expert info]
[grade info]
0 ` + "`challenger`" + `
[min lv]
0
[max lv]
0
[/grade info]
[grade info]
1 ` + "`veteran`" + `
[medal img]
` + "`expert-medal.img`" + ` 2
[icon img]
` + "`expert-icon.img`" + ` 3
[min lv]
1
[max lv]
-1
[/grade info]
[/expert info]
[honor expert exp table]
1 ` + "`100`" + ` 2 ` + "`204`" + ` 3 ` + "`9999999999`" + `
[/honor expert exp table]
`
}
