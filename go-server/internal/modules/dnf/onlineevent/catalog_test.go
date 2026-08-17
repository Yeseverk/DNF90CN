package onlineevent

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

type attendanceCatalogTestSource map[string]string

func (s attendanceCatalogTestSource) ReadText(relativePath string) (string, error) {
	text, ok := s[relativePath]
	if !ok {
		return "", fmt.Errorf("missing %s", relativePath)
	}
	return text, nil
}

type cancelingAttendanceCatalogSource struct {
	attendanceCatalogTestSource
	cancel     context.CancelFunc
	cancelPath string
}

func (s cancelingAttendanceCatalogSource) ReadText(relativePath string) (string, error) {
	text, err := s.attendanceCatalogTestSource.ReadText(relativePath)
	if err == nil && relativePath == s.cancelPath {
		s.cancel()
	}
	return text, err
}

func TestLoadAttendancePVFCatalogPreservesRawSections(t *testing.T) {
	catalog, err := LoadAttendancePVFCatalog(context.Background(), validAttendanceCatalogSource())
	if err != nil {
		t.Fatal(err)
	}
	snapshot := catalog.Snapshot()
	if !reflect.DeepEqual(snapshot.ProcessDurationsSeconds, []int64{1800, 1800, 3600, 3600}) {
		t.Fatalf("raw process durations=%v", snapshot.ProcessDurationsSeconds)
	}
	if !reflect.DeepEqual(snapshot.SumThresholds, []int64{5, 10, 15, 25}) {
		t.Fatalf("raw sum thresholds=%v", snapshot.SumThresholds)
	}
	if !reflect.DeepEqual(snapshot.RewardActivityForSum, []int64{0}) {
		t.Fatalf("raw reward activity for sum=%v", snapshot.RewardActivityForSum)
	}
	wantRewards := []AttendancePVFItemReward{
		{ItemID: 490006574, Count: 1, DefinitionPath: "stackable/490006001/chn_490006574.stk"},
		{ItemID: 490006575, Count: 1, DefinitionPath: "stackable/490006001/chn_490006575.stk"},
		{ItemID: 490006576, Count: 1, DefinitionPath: "stackable/490006001/chn_490006576.stk"},
		{ItemID: 490006577, Count: 1, DefinitionPath: "stackable/490006001/chn_490006577.stk"},
	}
	if !reflect.DeepEqual(snapshot.RewardItems, wantRewards) {
		t.Fatalf("reward items=%+v", snapshot.RewardItems)
	}
	wantSumRewards := []AttendancePVFItemReward{
		{ItemID: 490006578, Count: 1, DefinitionPath: "stackable/490006001/chn_490006578.stk"},
		{ItemID: 490006579, Count: 1, DefinitionPath: "stackable/490006001/chn_490006579.stk"},
		{ItemID: 490006580, Count: 1, DefinitionPath: "stackable/490006001/chn_490006580.stk"},
		{ItemID: 490006581, Count: 1, DefinitionPath: "stackable/490006001/chn_490006581.stk"},
	}
	if !reflect.DeepEqual(snapshot.RewardItemsForSum, wantSumRewards) {
		t.Fatalf("sum reward items=%+v", snapshot.RewardItemsForSum)
	}

	// Every accessor result must be detached from the immutable catalog.
	snapshot.ProcessDurationsSeconds[0] = 999
	snapshot.RewardItems[0].ItemID = 1
	snapshot.SumThresholds[0] = 999
	snapshot.RewardItemsForSum[0].DefinitionPath = "changed"
	snapshot.RewardActivityForSum[0] = 999
	again := catalog.Snapshot()
	if again.ProcessDurationsSeconds[0] != 1800 || again.RewardItems[0].ItemID != 490006574 ||
		again.SumThresholds[0] != 5 || again.RewardItemsForSum[0].DefinitionPath != "stackable/490006001/chn_490006578.stk" ||
		again.RewardActivityForSum[0] != 0 {
		t.Fatalf("catalog leaked mutable state: %+v", again)
	}
}

func TestLoadAttendancePVFCatalogRejectsMalformedSections(t *testing.T) {
	valid := validAttendanceCatalogSource()[AttendanceEventPVFPath]
	tests := []struct {
		name string
		text string
	}{
		{
			name: "missing required section",
			text: strings.Replace(valid, "[reward activity for sum]\n0\n[/reward activity for sum]\n", "", 1),
		},
		{
			name: "duplicate section",
			text: valid + "\n[process time seconds]\n1 2 3 4\n[/process time seconds]\n",
		},
		{
			name: "missing closing section",
			text: strings.Replace(valid, "[/reward activity for sum]\n", "", 1),
		},
		{
			name: "unexpected section",
			text: valid + "\n[guessed event id]\n1\n[/guessed event id]\n",
		},
		{
			name: "mismatched closing section order",
			text: swapAttendanceClosingMarkers(valid, "process time seconds", "reward item"),
		},
		{
			name: "closing section contains a value",
			text: strings.Replace(valid, "[/process time seconds]\n", "[/process time seconds]\n999\n", 1),
		},
		{
			name: "root value before first section",
			text: "999\n" + valid,
		},
		{
			name: "non integer duration",
			text: strings.Replace(valid, "1800 1800 3600 3600", "1800 invalid 3600 3600", 1),
		},
		{
			name: "wrong duration count",
			text: strings.Replace(valid, "1800 1800 3600 3600", "1800 1800 3600", 1),
		},
		{
			name: "non positive duration",
			text: strings.Replace(valid, "1800 1800 3600 3600", "1800 0 3600 3600", 1),
		},
		{
			name: "non positive reward item",
			text: strings.Replace(valid, "490006574 1", "0 1", 1),
		},
		{
			name: "non positive reward count",
			text: strings.Replace(valid, "490006574 1", "490006574 0", 1),
		},
		{
			name: "wrong sum reward count",
			text: strings.Replace(valid, "490006578 1 490006579 1 490006580 1 490006581 1", "490006578 1 490006579 1 490006580 1", 1),
		},
		{
			name: "non positive sum threshold",
			text: strings.Replace(valid, "5 10 15 25", "5 10 0 25", 1),
		},
		{
			name: "wrong raw activity shape",
			text: strings.Replace(valid, "[reward activity for sum]\n0", "[reward activity for sum]\n0 1", 1),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := validAttendanceCatalogSource()
			source[AttendanceEventPVFPath] = test.text
			_, err := LoadAttendancePVFCatalog(context.Background(), source)
			if !errors.Is(err, ErrAttendancePVFMalformed) {
				t.Fatalf("error=%v, want %v", err, ErrAttendancePVFMalformed)
			}
		})
	}
}

func TestLoadAttendancePVFCatalogRequiresListedReadableDefinitions(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(attendanceCatalogTestSource)
		want   error
	}{
		{
			name: "reward absent from stackable list",
			mutate: func(source attendanceCatalogTestSource) {
				source[attendanceStackableListPath] = strings.Replace(
					source[attendanceStackableListPath],
					"490006574 `490006001/chn_490006574.stk`\n",
					"",
					1,
				)
			},
			want: ErrAttendanceRewardUnresolved,
		},
		{
			name: "duplicate reward list entry",
			mutate: func(source attendanceCatalogTestSource) {
				source[attendanceStackableListPath] += "490006574 `other/duplicate.stk`\n"
			},
			want: ErrAttendancePVFMalformed,
		},
		{
			name: "definition is unreadable",
			mutate: func(source attendanceCatalogTestSource) {
				delete(source, "stackable/490006001/chn_490006574.stk")
			},
			want: ErrAttendanceRewardUnresolved,
		},
		{
			name: "definition escapes stackable root",
			mutate: func(source attendanceCatalogTestSource) {
				source[attendanceStackableListPath] = strings.Replace(
					source[attendanceStackableListPath],
					"490006574 `490006001/chn_490006574.stk`",
					"490006574 `../equipment/invalid.equ`",
					1,
				)
			},
			want: ErrAttendanceRewardUnresolved,
		},
		{
			name: "definition normalizes outside stackable root",
			mutate: func(source attendanceCatalogTestSource) {
				source[attendanceStackableListPath] = strings.Replace(
					source[attendanceStackableListPath],
					"490006574 `490006001/chn_490006574.stk`",
					"490006574 `stackable/../equipment/invalid.stk`",
					1,
				)
			},
			want: ErrAttendanceRewardUnresolved,
		},
		{
			name: "definition uses an absolute drive path",
			mutate: func(source attendanceCatalogTestSource) {
				source[attendanceStackableListPath] = strings.Replace(
					source[attendanceStackableListPath],
					"490006574 `490006001/chn_490006574.stk`",
					"490006574 `C:/outside/invalid.stk`",
					1,
				)
			},
			want: ErrAttendanceRewardUnresolved,
		},
		{
			name: "definition is readable but not stackable extension",
			mutate: func(source attendanceCatalogTestSource) {
				source[attendanceStackableListPath] = strings.Replace(
					source[attendanceStackableListPath],
					"490006574 `490006001/chn_490006574.stk`",
					"490006574 `490006001/chn_490006574.equ`",
					1,
				)
				source["stackable/490006001/chn_490006574.equ"] = "[name] `wrong kind`\n"
			},
			want: ErrAttendanceRewardUnresolved,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := validAttendanceCatalogSource()
			test.mutate(source)
			_, err := LoadAttendancePVFCatalog(context.Background(), source)
			if !errors.Is(err, test.want) {
				t.Fatalf("error=%v, want %v", err, test.want)
			}
		})
	}
}

func TestLoadAttendancePVFCatalogHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := LoadAttendancePVFCatalog(ctx, validAttendanceCatalogSource())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v, want context canceled", err)
	}
}

func TestLoadAttendancePVFCatalogHonorsCancellationDuringLastDefinitionRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	source := cancelingAttendanceCatalogSource{
		attendanceCatalogTestSource: validAttendanceCatalogSource(),
		cancel:                      cancel,
		cancelPath:                  "stackable/490006001/chn_490006581.stk",
	}
	_, err := LoadAttendancePVFCatalog(ctx, source)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v, want context canceled", err)
	}
}

func swapAttendanceClosingMarkers(text string, left string, right string) string {
	placeholder := "[/__attendance_swap_marker__]"
	text = strings.Replace(text, "[/"+left+"]", placeholder, 1)
	text = strings.Replace(text, "[/"+right+"]", "[/"+left+"]", 1)
	return strings.Replace(text, placeholder, "[/"+right+"]", 1)
}

func validAttendanceCatalogSource() attendanceCatalogTestSource {
	source := attendanceCatalogTestSource{
		AttendanceEventPVFPath: `[process time seconds]
1800 1800 3600 3600
[/process time seconds]

[reward item]
490006574 1 490006575 1 490006576 1 490006577 1
[/reward item]

[process seconds for max count]
5 10 15 25
[/process seconds for max count]

[reward item for sum]
490006578 1 490006579 1 490006580 1 490006581 1
[/reward item for sum]

[reward activity for sum]
0
[/reward activity for sum]
`,
		attendanceStackableListPath: `490006574 ` + "`490006001/chn_490006574.stk`" + `
490006575 ` + "`490006001/chn_490006575.stk`" + `
490006576 ` + "`490006001/chn_490006576.stk`" + `
490006577 ` + "`490006001/chn_490006577.stk`" + `
490006578 ` + "`490006001/chn_490006578.stk`" + `
490006579 ` + "`490006001/chn_490006579.stk`" + `
490006580 ` + "`490006001/chn_490006580.stk`" + `
490006581 ` + "`490006001/chn_490006581.stk`" + `
`,
	}
	for itemID := int64(490006574); itemID <= 490006581; itemID++ {
		source[fmt.Sprintf("stackable/490006001/chn_%d.stk", itemID)] = fmt.Sprintf("[name] `reward-%d`\n", itemID)
	}
	return source
}
