package quest

import (
	"context"
	"errors"
	"reflect"
	"testing"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

type catalogTestSource map[string]string

func (s catalogTestSource) ReadText(relativePath string) (string, error) {
	value, ok := s[relativePath]
	if !ok {
		return "", errors.New("missing test PVF path: " + relativePath)
	}
	return value, nil
}

func TestCatalogAcceptableUsesPVFCharacterAndQuestState(t *testing.T) {
	source := catalogTestSource{
		DefaultList:             "1 `main.qst`\n2 `next.qst`\n3 `active.qst`\n4 `event.qst`\n5 `other_job.qst`\n6 `answer.qst`\n7 `collision.qst`\n",
		"n_quest/main.qst":      questCatalogTestDefinition("[epic]", 1, 10, "[gunner]", ""),
		"n_quest/next.qst":      questCatalogTestDefinition("[epic]", 2, 10, "[gunner]", "[pre required quest]\n1\n"),
		"n_quest/active.qst":    questCatalogTestDefinition("[normal]", 1, 10, "[gunner]", ""),
		"n_quest/event.qst":     questCatalogTestDefinition("[epic]", 1, 10, "[gunner]", "[event]\n1\n"),
		"n_quest/other_job.qst": questCatalogTestDefinition("[epic]", 1, 10, "[fighter]", ""),
		"n_quest/answer.qst":    questCatalogTestDefinition("[epic]", 1, 10, "[gunner]", "[pre required quest answer]\n1\n"),
		"n_quest/collision.qst": questCatalogTestDefinition("[epic]", 1, 10, "[gunner]", "[collision quest]\n1\n"),
	}
	index, err := dnfpvf.Build(context.Background(), source, dnfpvf.BuildOptions{Lists: []string{DefaultList}})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := Load(context.Background(), index)
	if err != nil {
		t.Fatal(err)
	}

	record := dnfrepo.QuestRecord{States: map[int64]dnfrepo.QuestState{
		3: {Status: "active"},
	}}
	fresh := catalog.Acceptable(CharacterEligibility{Level: 1, Job: 2, GrowType: 0}, record)
	if !reflect.DeepEqual(fresh.IDs, []int32{1, 7}) || fresh.EpicCount != 2 {
		t.Fatalf("fresh acceptable=%v epic=%d, want [1 7] epic=2", fresh.IDs, fresh.EpicCount)
	}
	listed := catalog.QuestList(CharacterEligibility{Level: 1, Job: 2, GrowType: 0}, record)
	if !reflect.DeepEqual(listed.IDs, []int32{1, 3, 7}) || listed.EpicCount != 2 || listed.ActiveCount != 1 {
		t.Fatalf("fresh quest list=%v epic=%d active=%d, want [1 3 7] epic=2 active=1", listed.IDs, listed.EpicCount, listed.ActiveCount)
	}

	record.States[1] = dnfrepo.QuestState{Status: "completed"}
	advanced := catalog.Acceptable(CharacterEligibility{Level: 2, Job: 2, GrowType: 0}, record)
	if !reflect.DeepEqual(advanced.IDs, []int32{2}) || advanced.EpicCount != 1 {
		t.Fatalf("advanced acceptable=%v epic=%d, want [2] epic=1", advanced.IDs, advanced.EpicCount)
	}
	advancedList := catalog.QuestList(CharacterEligibility{Level: 2, Job: 2, GrowType: 0}, record)
	if !reflect.DeepEqual(advancedList.IDs, []int32{2, 3}) || advancedList.EpicCount != 1 || advancedList.ActiveCount != 1 {
		t.Fatalf("advanced quest list=%v epic=%d active=%d, want [2 3] epic=1 active=1", advancedList.IDs, advancedList.EpicCount, advancedList.ActiveCount)
	}
}

func TestExpertJobTransitionQuestDirectoryPreservesChainAndTerminals(t *testing.T) {
	catalog := &Catalog{ordered: []Definition{
		{ID: 2702, JobChangeQuest: 20},
		{ID: 10020, JobChangeQuest: 20, PreRequiredGroups: [][]int64{{2702}}},
		{ID: 10021, JobChangeQuest: 20, PreRequiredGroups: [][]int64{{10020}}},
		{ID: 9000, JobChangeQuest: 2},
	}}
	if got := catalog.ExpertJobTransitionQuestIDs(); !reflect.DeepEqual(got, []int64{2702, 10020, 10021}) {
		t.Fatalf("transition quests=%v", got)
	}
	if got := catalog.ExpertJobTransitionTerminalQuestIDs(); !reflect.DeepEqual(got, []int64{10021}) {
		t.Fatalf("terminal transition quests=%v", got)
	}
}

func TestCatalogAcceptableAppliesGrowType(t *testing.T) {
	source := catalogTestSource{
		DefaultList:                 "10 `grow.qst`\n11 `job_change.qst`\n12 `second_awaken.qst`\n",
		"n_quest/grow.qst":          questCatalogTestDefinition("[epic]", 1, 99, "[gunner]", "[grow type]\n2\n"),
		"n_quest/job_change.qst":    questCatalogTestDefinition("[epic]", 1, 99, "[gunner]", "[grow type]\n2\n[job change quest]\n2\n"),
		"n_quest/second_awaken.qst": questCatalogTestDefinition("[epic]", 1, 99, "[gunner]", "[grow type]\n2\n[job change quest]\n3\n"),
	}
	index, err := dnfpvf.Build(context.Background(), source, dnfpvf.BuildOptions{Lists: []string{DefaultList}})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := Load(context.Background(), index)
	if err != nil {
		t.Fatal(err)
	}

	got := catalog.Acceptable(CharacterEligibility{Level: 20, Job: 2, GrowType: 0x12}, dnfrepo.QuestRecord{})
	if !reflect.DeepEqual(got.IDs, []int32{11, 12}) {
		t.Fatalf("grow acceptable=%v, want [11 12]", got.IDs)
	}
}

func TestCatalogPlanAcceptUsesCSharpInitialTriggerRulesAndBlocksEventItems(t *testing.T) {
	source := catalogTestSource{
		DefaultList: "3145 `clear_map.qst`\n3146 `event_item.qst`\n3147 `seek_meet.qst`\n3148 `quest_clear.qst`\n3149 `fallback.qst`\n",
		"n_quest/clear_map.qst": questCatalogTestDefinition("[epic]", 1, 99, "[gunner]",
			"[type]\n`[clear map]`\n[int data]\n76126\n"),
		"n_quest/event_item.qst": questCatalogTestDefinition("[epic]", 1, 99, "[gunner]",
			"[type]\n`[clear map]`\n[int data]\n76126\n[depend give item]\n1001 1\n"),
		"n_quest/seek_meet.qst": questCatalogTestDefinition("[epic]", 1, 99, "[gunner]",
			"[type]\n`[seek n meet npc]`\n[int data]\n1001 1 1\n"),
		"n_quest/quest_clear.qst": questCatalogTestDefinition("[epic]", 1, 99, "[gunner]",
			"[type]\n`[quest clear]`\n[int data]\n1 2 3\n"),
		"n_quest/fallback.qst": questCatalogTestDefinition("[epic]", 1, 99, "[gunner]",
			"[type]\n`[custom client only type]`\n"),
	}
	index, err := dnfpvf.Build(context.Background(), source, dnfpvf.BuildOptions{Lists: []string{DefaultList}})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := Load(context.Background(), index)
	if err != nil {
		t.Fatal(err)
	}
	character := CharacterEligibility{Level: 1, Job: 2, GrowType: 0}
	plan, err := catalog.PlanAccept(character, dnfrepo.QuestRecord{}, 3145)
	if err != nil {
		t.Fatalf("PlanAccept clear-map error = %v", err)
	}
	if plan.QuestID != 3145 || normalizeQuestTag(plan.Type) != "clear map" || plan.InitTrigger != 1 {
		t.Fatalf("clear-map plan = %+v", plan)
	}
	if _, err := catalog.PlanAccept(character, dnfrepo.QuestRecord{}, 3146); !errors.Is(err, ErrQuestAcceptEventItemsRequired) {
		t.Fatalf("event-item PlanAccept error = %v", err)
	}
	seekPlan, err := catalog.PlanAccept(character, dnfrepo.QuestRecord{}, 3147)
	if err != nil || seekPlan.InitTrigger != 1|(1<<9) {
		t.Fatalf("seek-and-meet PlanAccept = %+v, %v", seekPlan, err)
	}
	questClearPlan, err := catalog.PlanAccept(character, dnfrepo.QuestRecord{States: map[int64]dnfrepo.QuestState{
		1: {Status: "completed"},
	}}, 3148)
	if err != nil || questClearPlan.InitTrigger != 2 {
		t.Fatalf("quest-clear PlanAccept = %+v, %v", questClearPlan, err)
	}
	fallbackPlan, err := catalog.PlanAccept(character, dnfrepo.QuestRecord{}, 3149)
	if err != nil || fallbackPlan.InitTrigger != 1 {
		t.Fatalf("fallback PlanAccept = %+v, %v", fallbackPlan, err)
	}
	active := dnfrepo.QuestRecord{States: map[int64]dnfrepo.QuestState{3145: {Status: "active"}}}
	if _, err := catalog.PlanAccept(character, active, 3145); !errors.Is(err, ErrQuestNotAcceptable) {
		t.Fatalf("active PlanAccept error = %v", err)
	}
}

func TestCatalogPlanAcceptLinksNoRewardMainSubQuests(t *testing.T) {
	source := catalogTestSource{
		DefaultList: "3145 `pre.qst`\n3146 `parent.qst`\n3157 `hunt.qst`\n3054 `clear.qst`\n",
		"n_quest/pre.qst": questCatalogTestDefinition("[epic]", 1, 99, "[gunner]",
			"[type]\n`[clear map]`\n[int data]\n76126\n"),
		"n_quest/parent.qst": questCatalogTestDefinition("[epic]", 3, 99, "[gunner]",
			"[type]\n`[quest clear]`\n[pre required quest]\n3145\n[int data]\n3157 3054\n"),
		"n_quest/hunt.qst": questCatalogTestDefinition("[sub]", 3, 99, "[gunner]",
			"[type]\n`[hunt enemy]`\n[main quest]\n3146\n[int data]\n3 -1 13099 3 1\n"),
		"n_quest/clear.qst": questCatalogTestDefinition("[sub]", 1, 99, "[gunner]",
			"[type]\n`[clear map]`\n[main quest]\n3146\n[int data]\n76136\n"),
	}
	index, err := dnfpvf.Build(context.Background(), source, dnfpvf.BuildOptions{Lists: []string{DefaultList}})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := Load(context.Background(), index)
	if err != nil {
		t.Fatal(err)
	}
	if definition, ok := catalog.Find(3157); !ok || definition.MainQuestID != 3146 {
		t.Fatalf("main quest parse = %+v ok=%t, want 3146", definition, ok)
	}
	plan, err := catalog.PlanAccept(CharacterEligibility{Level: 3, Job: 2, GrowType: 0}, dnfrepo.QuestRecord{States: map[int64]dnfrepo.QuestState{
		3145: {Status: "completed"},
	}}, 3146)
	if err != nil {
		t.Fatalf("PlanAccept parent error = %v", err)
	}
	if plan.InitTrigger != 2 || len(plan.LinkedSubQuests) != 2 {
		t.Fatalf("parent plan = %+v, want trigger 2 with two linked subtasks", plan)
	}
	if plan.LinkedSubQuests[0].QuestID != 3157 || plan.LinkedSubQuests[0].InitTrigger != 1 ||
		plan.LinkedSubQuests[1].QuestID != 3054 || plan.LinkedSubQuests[1].InitTrigger != 1 {
		t.Fatalf("linked subtasks = %+v", plan.LinkedSubQuests)
	}
}

func TestTriggerFromIntDataPacksThreeNineBitChannels(t *testing.T) {
	values := []int64{10, 0, 0, 5, 20, 0, 0, 3, 30, 0, 0, 2}
	if got, want := triggerFromIntData(values, 4), uint32(5|(3<<9)|(2<<18)); got != want {
		t.Fatalf("trigger = %d, want %d", got, want)
	}
}

func TestCatalogPlanFinishRewardFiltersPVFJobAndSelection(t *testing.T) {
	source := catalogTestSource{
		DefaultList: "3145 `finish.qst`\n",
		"n_quest/finish.qst": questCatalogTestDefinition("[epic]", 1, 99, "[all]", `
[difficulty]
`+"`1`"+`
[reward type]
`+"`[item]`"+`
[reward int data]
10165055 1 104010230 `+"`[job]`"+` 2 -1 1 999 `+"`[job]`"+` 1 -1 1
[reward selection int data]
2001 `+"`[job]`"+` 2 -1 1 2002 `+"`[job]`"+` 2 -1 1
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
	plan, err := catalog.PlanFinishReward(CharacterEligibility{Level: 1, Job: 2, GrowType: 0}, 3145, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Difficulty != '1' || !plan.RewardSelectionUsed || len(plan.Items) != 3 ||
		plan.Items[0].ItemID != 10165055 || plan.Items[1].ItemID != 104010230 || plan.Items[2].ItemID != 2002 {
		t.Fatalf("finish reward plan = %+v", plan)
	}
	if _, err := catalog.PlanFinishReward(CharacterEligibility{Level: 1, Job: 2, GrowType: 0}, 3145, 2, true); !errors.Is(err, ErrQuestRewardSelectionInvalid) {
		t.Fatalf("out-of-range selection error = %v", err)
	}
}

func TestCatalogPlanFinishRewardPreservesGoldMarkerOutOfItemSlots(t *testing.T) {
	source := catalogTestSource{
		DefaultList: "3145 `finish.qst`\n",
		"n_quest/finish.qst": questCatalogTestDefinition("[epic]", 1, 99, "[gunner]", `
[difficulty]
`+"`N`"+`
[reward type]
`+"`[item]`"+`
[gold multiple]
150
[reward int data]
0 1 10403 2
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
	plan, err := catalog.PlanFinishReward(CharacterEligibility{Level: 1, Job: 2, GrowType: 0}, 3145, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.HasGoldReward || plan.GoldMultiple != 150 {
		t.Fatalf("gold reward marker not preserved: %+v", plan)
	}
	if len(plan.Items) != 1 || plan.Items[0].ItemID != 10403 || plan.Items[0].Count != 2 {
		t.Fatalf("gold marker leaked into item rewards: %+v", plan.Items)
	}
}

func TestCatalogPlanFinishRewardRejectsMalformedPVF(t *testing.T) {
	source := catalogTestSource{
		DefaultList: "7 `bad.qst`\n",
		"n_quest/bad.qst": questCatalogTestDefinition("[epic]", 1, 99, "[all]", `
[reward type]
`+"`[item]`"+`
[reward int data]
101 `+"`[job]`"+` 2
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
	if _, err := catalog.PlanFinishReward(CharacterEligibility{Level: 1, Job: 2}, 7, 0, false); !errors.Is(err, ErrQuestRewardMalformed) {
		t.Fatalf("malformed reward error = %v", err)
	}
}

func TestCatalogPlanFinishRewardPlansPVFProfessionTransitions(t *testing.T) {
	source := catalogTestSource{
		DefaultList: "100 `change.qst`\n101 `awaken.qst`\n102 `second.qst`\n",
		"n_quest/change.qst": questCatalogTestDefinition("[epic]", 15, 99, "[gunner]", `
[job change quest]
1
[reward type]
`+"`[grow type]`"+`
[reward int data]
2
`),
		"n_quest/awaken.qst": questCatalogTestDefinition("[epic]", 50, 99, "[gunner]", `
[job change quest]
2
[grow type]
2
[reward type]
`+"`[awakening type]`"+`
[reward int data]
1
`),
		"n_quest/second.qst": questCatalogTestDefinition("[epic]", 75, 99, "[gunner]", `
[job change quest]
3
[grow type]
2
[reward type]
`+"`[awakening type]`"+`
[reward int data]
2
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
	changed, err := catalog.PlanFinishReward(CharacterEligibility{Level: 15, Job: 2, GrowType: 0}, 100, 0, false)
	if err != nil || !changed.HasProfession || changed.ProfessionRequest.GrowNumber != 2 || changed.ProfessionRequest.ChainType != 1 || len(changed.Items) != 0 {
		t.Fatalf("change plan = %+v, %v", changed, err)
	}
	awakened, err := catalog.PlanFinishReward(CharacterEligibility{Level: 50, Job: 2, GrowType: 0x02}, 101, 0, false)
	if err != nil || awakened.ProfessionRequest.GrowNumber != 1 || awakened.ProfessionRequest.ChainType != 2 {
		t.Fatalf("awakening plan = %+v, %v", awakened, err)
	}
	second, err := catalog.PlanFinishReward(CharacterEligibility{Level: 75, Job: 2, GrowType: 0x12}, 102, 0, false)
	if err != nil || second.ProfessionRequest.GrowNumber != 2 || second.ProfessionRequest.JobChangeQuest != 3 {
		t.Fatalf("second awakening plan = %+v, %v", second, err)
	}
}

func TestCatalogPlanFinishRewardPlansExpertJobChain20(t *testing.T) {
	source := catalogTestSource{
		DefaultList: "2710 `expert.qst`\n",
		"n_quest/expert.qst": questCatalogTestDefinition("[common unique]", 20, 99, "[all]", `
[job change quest]
20
[reward type]
`+"`[expert job]`"+`
[reward int data]
3
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
	plan, err := catalog.PlanFinishReward(CharacterEligibility{Level: 20, Job: 2}, 2710, 0, false)
	if err != nil || !plan.HasExpertJob || plan.ExpertJobType != 3 || plan.HasProfession || len(plan.Items) != 0 {
		t.Fatalf("expert-job plan = %+v, %v", plan, err)
	}
	if _, err := catalog.PlanFinishReward(CharacterEligibility{Level: 20, Job: 2}, 2710, 0, true); !errors.Is(err, ErrQuestRewardSelectionInvalid) {
		t.Fatalf("expert-job selectable plan error = %v", err)
	}
}

func TestCatalogPlanFinishRewardDerivesSlotExpansionFromRuntimePVFIndex(t *testing.T) {
	source := catalogTestSource{
		DefaultList: "649 `support.qst`\n650 `magic.qst`\n2636 `earring.qst`\n",
		"n_quest/support.qst": questCatalogTestDefinition("[common unique]", 1, 99, "[all]", `
[reward type]
`+"`[slot expansion]`"+`
[reward int data]
0
`),
		"n_quest/magic.qst": questCatalogTestDefinition("[common unique]", 1, 99, "[all]", `
[reward type]
`+"`[slot expansion]`"+`
[reward int data]
1
`),
		"n_quest/earring.qst": questCatalogTestDefinition("[common unique]", 1, 99, "[all]", `
[reward type]
`+"`[slot expansion]`"+`
[reward int data]
2
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
	for _, test := range []struct {
		questID int64
		index   uint32
		bit     byte
	}{
		{questID: 649, index: 0, bit: 0x01},
		{questID: 650, index: 1, bit: 0x02},
		{questID: 2636, index: 2, bit: 0x10},
	} {
		plan, planErr := catalog.PlanFinishReward(CharacterEligibility{Level: 90, Job: 2}, test.questID, 0, false)
		if planErr != nil {
			t.Fatalf("quest %d plan: %v", test.questID, planErr)
		}
		if !plan.HasSlotExpansion || plan.SlotExpansionIndex != test.index || plan.SlotExpansionBit != test.bit {
			t.Fatalf("quest %d slot expansion plan=%+v", test.questID, plan)
		}
	}
	if ExEquipSlotAll != 0x13 {
		t.Fatalf("current NoPack extra-equipment-slot mask=%#x want=0x13", ExEquipSlotAll)
	}

	for _, values := range [][]int64{nil, {}, {0, 1}, {-1}, {3}} {
		definition := Definition{
			ID: 9000, Path: "n_quest/bad_slot.qst", LevelMin: 1, LevelMax: 99,
			Difficulty: "N", RewardType: "[slot expansion]", RewardIntData: values,
		}
		badCatalog := &Catalog{ordered: []Definition{definition}, byID: map[int64]Definition{definition.ID: definition}}
		if _, planErr := badCatalog.PlanFinishReward(CharacterEligibility{Level: 90, Job: 2}, definition.ID, 0, false); !errors.Is(planErr, ErrQuestRewardMalformed) {
			t.Fatalf("slot expansion values=%v error=%v", values, planErr)
		}
	}
}

func TestCatalogPlanFinishRewardAcceptsPVFLegitimateEmptyReward(t *testing.T) {
	definition := Definition{
		ID: 943, Path: "n_quest/job2_mid.qst", LevelMin: 1, LevelMax: 99,
		Difficulty: "N", JobChangeQuest: 2, RewardIntData: []int64{0, 0},
	}
	catalog := &Catalog{ordered: []Definition{definition}, byID: map[int64]Definition{definition.ID: definition}}
	plan, err := catalog.PlanFinishReward(CharacterEligibility{Level: 50, Job: 2, GrowType: 0x01}, definition.ID, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if plan.HasProfession || len(plan.Items) != 0 || plan.QuestID != definition.ID {
		t.Fatalf("empty PVF reward plan = %+v", plan)
	}
	if _, err := catalog.PlanFinishReward(CharacterEligibility{Level: 50, Job: 2, GrowType: 0x01}, definition.ID, 0, true); !errors.Is(err, ErrQuestRewardSelectionInvalid) {
		t.Fatalf("empty reward selection error = %v", err)
	}
	malformed := definition
	malformed.ID = 944
	malformed.RewardIntData = []int64{0, 1}
	catalog.byID[malformed.ID] = malformed
	if _, err := catalog.PlanFinishReward(CharacterEligibility{Level: 50, Job: 2, GrowType: 0x01}, malformed.ID, 0, false); !errors.Is(err, ErrQuestRewardMalformed) {
		t.Fatalf("nonzero empty-type reward error = %v", err)
	}
}

func TestCatalogAcceptablePreservesTargetCharacterGrowAndAwakeningTuple(t *testing.T) {
	source := catalogTestSource{
		DefaultList: "2680 `branch_zero_first.qst`\n5135 `branch_zero_second.qst`\n",
		"n_quest/branch_zero_first.qst": questCatalogTestDefinition("[realization]", 50, 99, "[all]", `
[target character]
`+"`[demonic swordman]`"+` 0 0 `+"`[creator mage]`"+` 0 0
[job change quest]
2
`),
		"n_quest/branch_zero_second.qst": questCatalogTestDefinition("[realization]", 75, 99, "[all]", `
[target character]
`+"`[demonic swordman]`"+` 0 1
[job change quest]
3
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
	if got := catalog.Acceptable(CharacterEligibility{Level: 50, Job: 9, GrowType: 0x00}, dnfrepo.QuestRecord{}).IDs; !reflect.DeepEqual(got, []int32{2680}) {
		t.Fatalf("branch-zero first-awakening acceptable=%v", got)
	}
	if got := catalog.Acceptable(CharacterEligibility{Level: 75, Job: 9, GrowType: 0x10}, dnfrepo.QuestRecord{}).IDs; !reflect.DeepEqual(got, []int32{5135}) {
		t.Fatalf("branch-zero second-awakening acceptable=%v", got)
	}
	if got := catalog.Acceptable(CharacterEligibility{Level: 75, Job: 2, GrowType: 0x10}, dnfrepo.QuestRecord{}).IDs; len(got) != 0 {
		t.Fatalf("target tuple leaked to ordinary job: %v", got)
	}
}

func questCatalogTestDefinition(grade string, levelMin, levelMax int, job, extra string) string {
	return "[grade]\n`" + grade + "`\n[level]\n" + integerText(levelMin) + " " + integerText(levelMax) + "\n[job]\n`" + job + "`\n[exposed by npc]\n1\n" + extra
}

func integerText(value int) string {
	if value == 0 {
		return "0"
	}
	digits := make([]byte, 0, 8)
	for value > 0 {
		digits = append(digits, byte('0'+value%10))
		value /= 10
	}
	for left, right := 0, len(digits)-1; left < right; left, right = left+1, right-1 {
		digits[left], digits[right] = digits[right], digits[left]
	}
	return string(digits)
}

func TestCatalogSuccessorsFollowsPrerequisiteGroupsInOrder(t *testing.T) {
	source := catalogTestSource{
		DefaultList:         "1 `first.qst`\n2 `second.qst`\n3 `branch.qst`\n4 `unrelated.qst`\n",
		"n_quest/first.qst": questCatalogTestDefinition("[epic]", 1, 99, "[all]", ""),
		"n_quest/second.qst": questCatalogTestDefinition("[epic]", 1, 99, "[all]",
			"[pre required quest]\n1\n"),
		"n_quest/branch.qst": questCatalogTestDefinition("[normal]", 1, 99, "[all]",
			"[pre required quest]\n1 9\n"),
		"n_quest/unrelated.qst": questCatalogTestDefinition("[epic]", 1, 99, "[all]",
			"[pre required quest]\n2\n"),
	}
	index, err := dnfpvf.Build(context.Background(), source, dnfpvf.BuildOptions{Lists: []string{DefaultList}})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := Load(context.Background(), index)
	if err != nil {
		t.Fatal(err)
	}

	successors := catalog.Successors(1)
	if len(successors) != 2 || successors[0].ID != 2 || successors[1].ID != 3 {
		t.Fatalf("successors(1) = %+v, want [2 3] in catalog order", successors)
	}
	if none := catalog.Successors(99); len(none) != 0 {
		t.Fatalf("successors(99) = %+v, want none", none)
	}
	if successors[0].PreRequiredGroups[0][0] != 1 {
		t.Fatalf("successor clone shares prerequisite state: %+v", successors[0].PreRequiredGroups)
	}
}
