package quest

import (
	"context"
	"reflect"
	"strconv"
	"testing"
	"time"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func TestPlanLegacySaturatedActiveTriggerRepairUsesPVFMultiTargetCounts(t *testing.T) {
	source := catalogTestSource{
		DefaultList: "2635 `earring.qst`\n2637 `single.qst`\n2638 `other.qst`\n",
		"n_quest/earring.qst": questCatalogTestDefinition("[side]", 90, 90, "[all]", `
[type]
`+"`[hunt monster]`"+`
[int data]
311 2 63767 1 314 2 63796 1
`),
		"n_quest/single.qst": questCatalogTestDefinition("[side]", 90, 90, "[all]", `
[type]
`+"`[hunt monster]`"+`
[int data]
311 2 63767 511
`),
		"n_quest/other.qst": questCatalogTestDefinition("[side]", 90, 90, "[all]", `
[type]
`+"`[meet npc]`"+`
`),
	}
	index, err := dnfpvf.Build(context.Background(), source, dnfpvf.BuildOptions{Lists: []string{DefaultList}})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := Load(context.Background(), index)
	if err != nil {
		t.Fatal(err)
	}
	repairedAt := time.Date(2026, 7, 23, 2, 30, 0, 0, time.UTC)
	record := dnfrepo.QuestRecord{
		CharacterID: "19",
		States: map[int64]dnfrepo.QuestState{
			2635: {Status: "active", ProgressValue: 511},
			2637: {Status: "active", ProgressValue: 511},
			2638: {Status: "active", ProgressValue: 511},
		},
		Progress: map[int64]dnfrepo.QuestState{
			2635: {Status: "active", ProgressValue: 512},
		},
	}
	plan, err := catalog.PlanLegacySaturatedActiveTriggerRepair(record, repairedAt)
	if err != nil {
		t.Fatal(err)
	}
	if got := plan.Record.States[2635].ProgressValue; got != 0 {
		t.Fatalf("quest 2635 repaired trigger=%d, want completed trigger 0", got)
	}
	if got := plan.Record.Progress[2635].ProgressValue; got != 512 {
		t.Fatalf("legitimate partial progress changed to %d", got)
	}
	if got := plan.Record.States[2637].ProgressValue; got != 511 {
		t.Fatalf("legitimate single-channel 511 changed to %d", got)
	}
	if got := plan.Record.States[2638].ProgressValue; got != 511 {
		t.Fatalf("unrelated quest changed to %d", got)
	}
	if !reflect.DeepEqual(plan.ChangedFields, []dnfrepo.QuestField{dnfrepo.QuestFieldStates}) || len(plan.Repairs) != 1 {
		t.Fatalf("repair plan=%+v", plan)
	}
	state := plan.Record.States[2635]
	if !state.UpdatedAt.Equal(repairedAt) || state.Extra["legacy_trigger_repair_previous"] != "511" || state.Extra["legacy_trigger_repair_expected"] != "0" || state.Extra["legacy_trigger_pvf_initial"] != "513" {
		t.Fatalf("repair metadata=%+v", state)
	}
}

func TestPlanLegacySaturatedActiveTriggerRepairCorrectsPriorReset(t *testing.T) {
	source := catalogTestSource{
		DefaultList: "2635 `earring.qst`\n",
		"n_quest/earring.qst": questCatalogTestDefinition("[side]", 90, 90, "[all]", `
[type]
`+"`[hunt monster]`"+`
[int data]
311 2 63767 1 314 2 63796 1
`),
	}
	index, err := dnfpvf.Build(context.Background(), source, dnfpvf.BuildOptions{Lists: []string{DefaultList}})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := Load(context.Background(), index)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := catalog.PlanLegacySaturatedActiveTriggerRepair(dnfrepo.QuestRecord{
		CharacterID: "19",
		States: map[int64]dnfrepo.QuestState{
			2635: {
				Status:        "active",
				ProgressValue: 513,
				Extra: map[string]string{
					"legacy_trigger_repair_kind":     "pvf_multitarget_saturated_0x1ff",
					"legacy_trigger_repair_previous": "511",
				},
			},
		},
	}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Repairs) != 1 || plan.Record.States[2635].ProgressValue != 0 {
		t.Fatalf("prior reset correction plan=%+v", plan)
	}
}

func TestPlanLegacySaturatedActiveTriggerRepairClampsOnlyOversaturatedChannel(t *testing.T) {
	source := catalogTestSource{
		DefaultList: "2635 `earring.qst`\n",
		"n_quest/earring.qst": questCatalogTestDefinition("[side]", 90, 90, "[all]", `
[type]
`+"`[hunt monster]`"+`
[int data]
311 2 9600 1 311 2 9601 1 311 2 9602 1
`),
	}
	index, err := dnfpvf.Build(context.Background(), source, dnfpvf.BuildOptions{Lists: []string{DefaultList}})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := Load(context.Background(), index)
	if err != nil {
		t.Fatal(err)
	}
	repairedAt := time.Date(2026, 7, 23, 22, 15, 0, 0, time.UTC)
	polluted := int64(packTriggerChannels(505, 0, 1))
	record := dnfrepo.QuestRecord{
		CharacterID: "19",
		States: map[int64]dnfrepo.QuestState{
			2635: {Status: "active", ProgressValue: polluted},
		},
	}

	plan, err := catalog.PlanLegacySaturatedActiveTriggerRepair(record, repairedAt)
	if err != nil {
		t.Fatal(err)
	}
	want := int64(packTriggerChannels(0, 0, 1))
	if got := plan.Record.States[2635].ProgressValue; got != want {
		t.Fatalf("repaired trigger=%d, want %d", got, want)
	}
	if len(plan.Repairs) != 1 || plan.Repairs[0].PreviousTrigger != polluted || plan.Repairs[0].CurrentTrigger != want {
		t.Fatalf("repairs=%+v", plan.Repairs)
	}
	state := plan.Record.States[2635]
	if !state.UpdatedAt.Equal(repairedAt) ||
		state.Extra["legacy_trigger_repair_kind"] != "pvf_multitarget_oversaturated_channel_to_zero" ||
		state.Extra["legacy_trigger_repair_previous"] != strconv.FormatInt(polluted, 10) ||
		state.Extra["legacy_trigger_repair_expected"] != strconv.FormatInt(want, 10) ||
		state.Extra["legacy_trigger_pvf_initial"] != strconv.FormatUint(uint64(packTriggerChannels(1, 1, 1)), 10) {
		t.Fatalf("repair metadata=%+v", state)
	}
}
