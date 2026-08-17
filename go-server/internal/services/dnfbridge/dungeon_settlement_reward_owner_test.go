package dnfbridge

import (
	"context"
	"encoding/binary"
	"os"
	"strconv"
	"testing"
	"time"

	"longheng.io/server/internal/modules/dnf/dungeoncmd"
	"longheng.io/server/internal/modules/dnf/premium"
	"longheng.io/server/internal/modules/dnf/progression"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	"longheng.io/server/internal/modules/dnf/worldmap"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestCurrentDungeonSettlementProducerCommitsProgressionAndFreezesRealPickupReceiptOnce(t *testing.T) {
	service, runtime, session, _, _ := prepareCompletedSettlementRuntime(t)
	repositories, ok := service.repositoryGroup()
	if !ok {
		t.Fatal("repository group unavailable")
	}
	character, found, err := repositories.Character.Load(context.Background(), "99")
	if err != nil || !found {
		t.Fatalf("load character found=%t err=%v", found, err)
	}
	character.Level = 1
	character.Stats["exp"] = 90
	if err := repositories.Character.Save(context.Background(), character); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Skill.Save(context.Background(), dnfrepo.SkillRecord{
		CharacterID: "99",
		Points: dnfrepo.SkillPointState{
			TotalSP: 10, RemainingSP: 10, SyncedLevel: 1,
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(context.Background(), dnfrepo.InventoryRecord{
		CharacterID: "99",
		Slots: map[string]dnfrepo.ItemStack{
			"0:65": {ItemID: 8474, Count: 2},
		},
	}); err != nil {
		t.Fatal(err)
	}
	runtime.Character = character
	runtime.Dungeon.Metadata.BasisLevel.Set = true
	runtime.Dungeon.Metadata.BasisLevel.Value = 1
	runtime.Dungeon.Metadata.ExperienceIncreasingPoint.Set = true
	runtime.Dungeon.Metadata.ExperienceIncreasingPoint.Value = 1
	runtime.Request.Difficulty = 1
	runtime.DropOwner = newRuntimeDungeonDropOwner()
	runtime.DropOwner.byObjectKey[70001] = &runtimeDungeonDrop{
		ObjectKey: 70001, Item: dungeonDropItemDefinition{ItemID: 8474}, Amount: 1,
		Status: runtimeDungeonDropConsumed, DestinationSlot: 65,
	}
	runtime.settlementPlayResultReceived = true
	runtime.settlementPlayResultBody = make([]byte, currentDungeonPlayResultBaseSize)
	runtime.settlementPlayResultBody[currentDungeonPlayResultClientRankPointOffset] = 73
	runtime.startedAt = time.Unix(1700000000, 0)
	runtime.clearMapCompletionAt = runtime.startedAt.Add(12*time.Second + 345*time.Millisecond)

	resources := currentDungeonSettlementTestResources(t)
	if err := service.produceCurrentDungeonSettlementPlanWithResourcesLocked(
		context.Background(), session, runtime, resources,
	); err != nil {
		t.Fatal(err)
	}
	if runtime.settlementResultPlan == nil {
		t.Fatal("settlement plan was not frozen")
	}
	assertCurrentDungeonSettlementProducerPlayResultBody(t, runtime.settlementResultPlan.PlayResultBody)
	assertCurrentDungeonSettlementProducerRewardBody(t, runtime.settlementResultPlan.ClearRewardBody)
	committed, found, err := repositories.Character.Load(context.Background(), "99")
	if err != nil || !found {
		t.Fatalf("load committed character found=%t err=%v", found, err)
	}
	if committed.Level != 3 || committed.Stats["exp"] != 330 {
		t.Fatalf("committed progression level=%d exp=%d", committed.Level, committed.Stats["exp"])
	}
	skill, found, err := repositories.Skill.Load(context.Background(), "99")
	if err != nil || !found {
		t.Fatalf("load committed skill found=%t err=%v", found, err)
	}
	if skill.Points.TotalSP != 70 || skill.Points.RemainingSP != 70 || skill.Points.SyncedLevel != 3 {
		t.Fatalf("committed skill points=%+v", skill.Points)
	}

	// Force a producer retry after the database commit. The durable receipt
	// must reconstruct the same result without granting EXP/SP a second time.
	firstPlan := *runtime.settlementResultPlan
	runtime.settlementResultPlan = nil
	if err := service.produceCurrentDungeonSettlementPlanWithResourcesLocked(
		context.Background(), session, runtime, resources,
	); err != nil {
		t.Fatal(err)
	}
	replayed, _, _ := repositories.Character.Load(context.Background(), "99")
	replayedSkill, _, _ := repositories.Skill.Load(context.Background(), "99")
	if replayed.Level != 3 || replayed.Stats["exp"] != 330 || replayedSkill.Points != skill.Points {
		t.Fatalf("producer retry duplicated progression character=%+v skill=%+v", replayed, replayedSkill.Points)
	}
	if string(runtime.settlementResultPlan.PlayResultBody) != string(firstPlan.PlayResultBody) ||
		string(runtime.settlementResultPlan.CharacterBody) != string(firstPlan.CharacterBody) ||
		string(runtime.settlementResultPlan.ClearRewardBody) != string(firstPlan.ClearRewardBody) {
		t.Fatal("durable producer retry changed frozen packet plan")
	}
}

func assertCurrentDungeonSettlementProducerPlayResultBody(t *testing.T, body []byte) {
	t.Helper()
	if len(body) != 16 {
		t.Fatalf("op34 body len=%d want=16", len(body))
	}
	reader := settlementProtocolReader{body: body}
	if reader.u8() != 60 || reader.u32() != 12345 || reader.u8() != 0 ||
		reader.u8() != 73 || reader.u8() != 1 || reader.u8() != 1 ||
		reader.u16() != 99 || reader.u32() != 12345 || reader.u8() != 0 ||
		reader.offset != len(body) {
		t.Fatalf("op34 presentation/body mismatch body=%x offset=%d", body, reader.offset)
	}
}

func assertCurrentDungeonSettlementProducerRewardBody(t *testing.T, body []byte) {
	t.Helper()
	reader := settlementProtocolReader{body: body}
	if got := reader.u32(); got != 200 {
		t.Fatalf("op35 clear base experience=%d, want base=200", got)
	}
	// Base[1] = score bonus (200 × 0.20 rank rate for rank point 73 exceeding all 5 thresholds).
	if got := reader.u32(); got != 40 {
		t.Fatalf("op35 score bonus=%d, want 40", got)
	}
	for index := 2; index < 4; index++ {
		if got := reader.u32(); got != 0 {
			t.Fatalf("unknown op35 base[%d]=%d, want zero", index, got)
		}
	}
	if got := reader.u8(); got != 0 {
		t.Fatalf("unknown op35 base flag=%d, want zero", got)
	}
	for index := 0; index < currentDungeonClearRewardBonusFieldCount; index++ {
		if got := reader.u32(); got != 0 {
			t.Fatalf("unknown op35 bonus[%d]=%d, want zero", index, got)
		}
	}
	if reader.u8() != 0 || reader.u8() != 0 {
		t.Fatal("unknown op35 bonus groups are nonempty")
	}
	for index := 0; index < currentDungeonClearRewardPostBaseFieldCount; index++ {
		if got := reader.u32(); got != 0 {
			t.Fatalf("unknown op35 post-base[%d]=%d, want zero", index, got)
		}
	}
	for index := 0; index < currentDungeonClearRewardScoreFieldCount; index++ {
		if got := reader.u32(); got != 0 {
			t.Fatalf("unknown op35 score[%d]=%d, want zero", index, got)
		}
	}
	if reader.u32() != 0 || reader.u8() != 1 {
		t.Fatal("op35 quest/drop counts do not contain exactly one drop")
	}
	if reader.u16() != 65 || reader.u32() != 8474 || reader.u32() != 2 ||
		reader.u16() != 0 || reader.u8() != 0 || reader.u16() != 0 {
		t.Fatal("op35 committed pickup receipt does not match DB slot/item/post-count")
	}
	for index := 0; index < currentDungeonClearRewardGroupCount; index++ {
		if reader.u8() != 0 {
			t.Fatalf("op35 card slot %d is active", index)
		}
	}
	if reader.u32() != 0 {
		t.Fatal("unknown op35 total reward is nonzero")
	}
	for index := 0; index < currentDungeonClearRewardGroupCount*2; index++ {
		if reader.u8() != 0 {
			t.Fatalf("unknown op35 aux group %d is nonempty", index)
		}
	}
	if reader.u32() != 0 {
		t.Fatal("unknown op35 pre-tail value is nonzero")
	}
	if reader.u32() != 0 || reader.u8() != 1 || reader.u8() != 0 ||
		reader.u32() != 0 || reader.u32() != 0 || reader.u32() != 0 || reader.u32() != 0 ||
		reader.offset != len(body) {
		t.Fatalf("op35 current reader tail/boundary mismatch offset=%d len=%d", reader.offset, len(body))
	}
}

func TestCurrentDungeonCommittedDropReceiptsRejectsRuntimeDBMismatch(t *testing.T) {
	runtime := &runtimeDungeonState{DropOwner: newRuntimeDungeonDropOwner()}
	runtime.DropOwner.byObjectKey[1] = &runtimeDungeonDrop{
		ObjectKey: 1, Item: dungeonDropItemDefinition{ItemID: 8474},
		Status: runtimeDungeonDropConsumed, DestinationSlot: 65,
	}
	_, err := currentDungeonCommittedDropReceipts(runtime, dnfrepo.InventoryRecord{
		CharacterID: "99",
		Slots: map[string]dnfrepo.ItemStack{
			"0:65": {ItemID: 9000, Count: 1},
		},
	})
	if err == nil {
		t.Fatal("runtime/DB pickup mismatch was accepted")
	}
}

func TestCurrentDungeonClearRewardCardSlotsMirrorFrozen86JPCardPlan(t *testing.T) {
	raw := make([]byte, currentItemListEntryWireSize)
	state, err := newDungeonCardState(mustTestDungeonCardPlan(t,
		dungeonCardRewardBundle{
			Gold: 80,
			Items: []dungeonCardItemReward{{
				ItemID: 3227, Count: 2, Stackable: true, SlotStart: 121, SlotEnd: 176, RawEntry: raw,
			}},
		},
		dungeonCardRewardBundle{Gold: 30},
	))
	if err != nil {
		t.Fatal(err)
	}
	slots := currentDungeonClearRewardCardSlotsFromRuntime(&runtimeDungeonState{
		settlementCardRewardState: state,
	})
	if len(slots[0]) != 2 ||
		slots[0][0] != (currentDungeonClearRewardPair{Key: 0, Value: 80}) ||
		slots[0][1] != (currentDungeonClearRewardPair{Key: 3227, Value: 2}) {
		t.Fatalf("free card op35 pairs=%+v", slots[0])
	}
	if len(slots[dungeonCardSlotsPerSide]) != 1 ||
		slots[dungeonCardSlotsPerSide][0] != (currentDungeonClearRewardPair{Key: 0, Value: 30}) {
		t.Fatalf("paid card op35 pairs=%+v", slots[dungeonCardSlotsPerSide])
	}
	for index, slot := range slots {
		if index == 0 || index == dungeonCardSlotsPerSide {
			continue
		}
		if len(slot) != 0 {
			t.Fatalf("unexpected card slot %d pairs=%+v", index, slot)
		}
	}
}

func TestCurrentDungeonClearRewardCardSlotsAggregateGoldCardEquipmentInstances(t *testing.T) {
	paid := dungeonCardRewardBundle{
		Gold: 19,
		Items: []dungeonCardItemReward{{
			ItemID: 31865, Count: 1, SlotStart: 9, SlotEnd: 64,
		}},
	}
	if err := applyCurrentDungeonGoldCardItemMultiplier(&paid); err != nil {
		t.Fatal(err)
	}
	state, err := newDungeonCardState(mustTestDungeonCardPlan(
		t,
		dungeonCardRewardBundle{},
		paid,
	))
	if err != nil {
		t.Fatal(err)
	}
	slots := currentDungeonClearRewardCardSlotsFromRuntime(&runtimeDungeonState{
		settlementCardRewardState: state,
	})
	if got := slots[dungeonCardSlotsPerSide]; len(got) != 2 ||
		got[0] != (currentDungeonClearRewardPair{Key: 0, Value: 19}) ||
		got[1] != (currentDungeonClearRewardPair{Key: 31865, Value: 2}) {
		t.Fatalf("gold-card op35 equipment pairs=%+v, want item x2", got)
	}
	if got := slots[dungeonCardSlotsPerSide+1]; len(got) != 0 {
		t.Fatalf("gold-card op35 leaked into next member seat: %+v", got)
	}
}

func TestCurrentDungeonSettlementMonsterStatisticsPopulateClassifiedScoreFields(t *testing.T) {
	runtime := &runtimeDungeonState{}
	runtime.accumulateCurrentDungeonSettlementMonsterExperience(0, 7)
	runtime.accumulateCurrentDungeonSettlementMonsterExperience(1, 11)
	runtime.accumulateCurrentDungeonSettlementMonsterExperience(2, 13)
	runtime.accumulateCurrentDungeonSettlementMonsterExperience(3, 17)
	if runtime.settlementMonsterExperienceTotal != 48 {
		t.Fatalf("monster experience total=%d", runtime.settlementMonsterExperienceTotal)
	}
	if score := currentDungeonSettlementScore(runtime); score != ([4]uint32{0, 11, 13, 17}) {
		t.Fatalf("settlement score fields=%v, want classified subtotals", score)
	}
	runtime.settlementBossExperience = ^uint32(0) - 1
	runtime.accumulateCurrentDungeonSettlementMonsterExperience(3, 10)
	if runtime.settlementBossExperience != ^uint32(0) {
		t.Fatalf("boss experience did not saturate: %d", runtime.settlementBossExperience)
	}
}

func TestCurrentDungeonClearExperienceUsesExactOptionalWeightSemantics(t *testing.T) {
	resources := currentDungeonSettlementTestResources(t)
	for _, test := range []struct {
		name      string
		set       bool
		weight    float64
		want      uint32
		wantError bool
	}{
		{name: "absent is neutral", want: 200},
		{name: "negative is neutral", set: true, weight: -1, want: 200},
		{name: "explicit zero is zero reward and rejected", set: true, weight: 0, wantError: true},
		{name: "explicit positive is exact", set: true, weight: 1.25, want: 250},
	} {
		t.Run(test.name, func(t *testing.T) {
			runtime := &runtimeDungeonState{}
			runtime.Dungeon.Metadata.BasisLevel.Set = true
			runtime.Dungeon.Metadata.BasisLevel.Value = 1
			runtime.Dungeon.Metadata.ExperienceIncreasingPoint.Set = test.set
			runtime.Dungeon.Metadata.ExperienceIncreasingPoint.Value = test.weight
			runtime.Request.Difficulty = 1
			got, err := currentDungeonClearExperience(runtime, resources)
			if test.wantError {
				if err == nil {
					t.Fatalf("weight=%v produced gain=%d", test.weight, got)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("weight set=%t value=%v gain=%d want=%d err=%v", test.set, test.weight, got, test.want, err)
			}
		})
	}
}

func TestRealScriptPVFDungeon3SettlementExperienceSourcesAreComplete(t *testing.T) {
	pvfPath := os.Getenv("DNF_WORLDMAP_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNF_WORLDMAP_REAL_PVF_SMOKE to verify real dungeon settlement experience")
	}
	archive, err := platformpvf.Open(pvfPath)
	if err != nil {
		t.Fatal(err)
	}
	table, err := worldmap.LoadSource(context.Background(), archive, worldmap.Options{})
	if err != nil {
		t.Fatal(err)
	}
	dungeon, found := table.FindDungeon(3)
	if !found {
		t.Fatal("real dungeon 3 is absent")
	}
	tables, err := progression.Load(context.Background(), archive)
	if err != nil {
		t.Fatal(err)
	}
	experience, err := progression.LoadMonsterExperienceSources(archive)
	if err != nil {
		t.Fatal(err)
	}
	clearRanks, err := loadCurrentDungeonClearRankCatalog(archive)
	if err != nil {
		t.Fatal(err)
	}
	gain, err := currentDungeonClearExperience(&runtimeDungeonState{
		Dungeon: dungeon,
		Request: dungeoncmd.SelectDungeonRequest{DungeonID: 3, Difficulty: 0},
	}, currentDungeonSettlementRewardResources{
		Progression: tables, ExperienceSources: experience, ClearRankCatalog: clearRanks,
	})
	if err != nil {
		dungeonRate, dungeonRateFound := experience.DungeonIncreasingRate(3)
		t.Fatalf("%v: dungeon metadata=%+v difficulty_rates=%+v dungeon_rate=%v found=%t", err, dungeon.Metadata, experience.RawModifiers().DifficultyRates, dungeonRate, dungeonRateFound)
	}
	if gain == 0 {
		t.Fatal("real dungeon 3 produced zero clear experience")
	}
}

func currentDungeonSettlementTestResources(t *testing.T) currentDungeonSettlementRewardResources {
	t.Helper()
	source := bridgePVFSource{
		currentDungeonRankSystemPVFPath: "[rank grade]\n99 90 80 60 50 30 20 10\n[/rank grade]\n",
		progression.ExperienceTablePath: "100 250 500 900\n",
		progression.SkillPointTablePath: "[sp table]\n1 10\n2 30\n3 30\n4 40\n5 50\n[/sp table]\n[tp table]\n50 1\n[/tp table]\n",
		progression.QuestParameterPath: "[difficulty]\n`N` 100\n[/difficulty]\n" +
			"[exp reward table]\n100 -1\n200 -1\n300 -1\n400 -1\n500 -1\n" +
			"[green level penalty]\n80\n[grey level penalty]\n30\n",
		progression.MonsterExperienceTablePath: "1 30 2 60 3 72\n",
		progression.ServerParameterPath: "[monster exp bonusrate]\n1.00\n" +
			"[party user number exp bonusrate]\n1 2 3 4\n" +
			"[party user number exp bonusrate starter server]\n1 2 3 4\n" +
			"[dungeon difficulty exp bonusrate]\n1.30 2.00 2.50 3.00 4.00\n" +
			"[clear exp bonusrate]\n1.00\n" +
			"[clear rank exp bonusrate]\n0.05 0.10 0.12 0.15 0.20\n" +
			"[experience increasing point]\n700 1.00\n[/experience increasing point]\n",
		progression.WorldMapExperiencePenaltyPath: "[penalty table info]\n" +
			"-16 0.30 -15 0.50 -14 0.50 -13 0.50 -12 0.70 -11 0.70 -10 0.70 -9 0.80 -8 0.80 -7 0.80 -6 0.90 -5 0.90 -4 0.90 -3 1.00 -2 1.00 -1 1.00 0 1.00 1 1.00 2 1.00 3 1.00 4 1.00 5 1.00 6 0.90 7 0.90 8 0.90 9 0.80 10 0.80 11 0.80 12 0.70 13 0.70 14 0.70 15 0.50 16 0.50 17 0.50 18 0.30 19 0.30 20 0.30\n" +
			"[/penalty table info]\n",
	}
	tables, err := progression.Load(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	experience, err := progression.LoadMonsterExperienceSources(source)
	if err != nil {
		t.Fatal(err)
	}
	clearRanks, err := loadCurrentDungeonClearRankCatalog(source)
	if err != nil {
		t.Fatal(err)
	}
	return currentDungeonSettlementRewardResources{
		Progression: tables, ExperienceSources: experience, ClearRankCatalog: clearRanks,
	}
}

func TestCurrentDungeonSettlementGrowthContractAddsTwentyPercentClearExp(t *testing.T) {
	service, runtime, session, _, _ := prepareCompletedSettlementRuntime(t)
	service.premiumCatalog = &currentPremiumCatalog{
		effectsByType: map[int64]currentPremiumEffectInfo{
			premium.TypeBonusExp: {BonusExperiencePercent: 20},
		},
	}
	repositories, ok := service.repositoryGroup()
	if !ok {
		t.Fatal("repository group unavailable")
	}
	character, found, err := repositories.Character.Load(context.Background(), "99")
	if err != nil || !found {
		t.Fatalf("load character found=%t err=%v", found, err)
	}
	character.Level = 1
	character.Stats["exp"] = 90
	if err := repositories.Character.Save(context.Background(), character); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Skill.Save(context.Background(), dnfrepo.SkillRecord{
		CharacterID: "99",
		Points: dnfrepo.SkillPointState{
			TotalSP: 10, RemainingSP: 10, SyncedLevel: 1,
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(context.Background(), dnfrepo.InventoryRecord{
		CharacterID: "99",
		Slots:       map[string]dnfrepo.ItemStack{},
	}); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(24 * time.Hour).Unix()
	if err := repositories.Account.Save(context.Background(), dnfrepo.AccountRecord{
		AccountID: "account-1",
		Metadata:  map[string]string{"premium_expire_84": strconv.FormatInt(future, 10)},
	}); err != nil {
		t.Fatal(err)
	}
	runtime.Character = character
	runtime.Dungeon.Metadata.BasisLevel.Set = true
	runtime.Dungeon.Metadata.BasisLevel.Value = 1
	runtime.Dungeon.Metadata.ExperienceIncreasingPoint.Set = true
	runtime.Dungeon.Metadata.ExperienceIncreasingPoint.Value = 1
	runtime.Request.Difficulty = 1
	runtime.DropOwner = newRuntimeDungeonDropOwner()
	runtime.settlementPlayResultReceived = true
	runtime.settlementPlayResultBody = make([]byte, currentDungeonPlayResultBaseSize)
	runtime.settlementPlayResultBody[currentDungeonPlayResultClientRankPointOffset] = 73
	runtime.settlementMonsterExperienceTotal = 900
	runtime.settlementMonsterGrowthContractBonus = 180
	runtime.settlementChampionExperience = 200
	runtime.settlementSuperChampionExperience = 100
	runtime.settlementBossExperience = 300
	runtime.startedAt = time.Unix(1700000000, 0)
	runtime.clearMapCompletionAt = runtime.startedAt.Add(12*time.Second + 345*time.Millisecond)

	resources := currentDungeonSettlementTestResources(t)
	if err := service.produceCurrentDungeonSettlementPlanWithResourcesLocked(
		context.Background(), session, runtime, resources,
	); err != nil {
		t.Fatal(err)
	}
	if runtime.settlementResultPlan == nil {
		t.Fatal("settlement plan was not frozen")
	}
	// Clear gain 200 + score bonus 40 + growth contract 20% = 280 committed.
	// op35 Base[0] = base clear exp only (200).
	reader := settlementProtocolReader{body: runtime.settlementResultPlan.ClearRewardBody}
	if got := reader.u32(); got != 200 {
		t.Fatalf("op35 clear base experience=%d, want 200 (base only)", got)
	}
	body := runtime.settlementResultPlan.ClearRewardBody
	bonusStart := 4*4 + 1
	if got := binary.LittleEndian.Uint32(body[bonusStart+currentDungeonClearRewardBonusClearGrowthContractIndex*4:]); got != 40 {
		t.Fatalf("clear growth-contract Bonus[%d]=%d, want 40", currentDungeonClearRewardBonusClearGrowthContractIndex, got)
	}
	if got := binary.LittleEndian.Uint32(body[bonusStart+currentDungeonClearRewardBonusClearFatigueBurnIndex*4:]); got != 0 {
		t.Fatalf("clear fatigue-burn Bonus[%d]=%d, want 0", currentDungeonClearRewardBonusClearFatigueBurnIndex, got)
	}
	if got := binary.LittleEndian.Uint32(body[bonusStart+currentDungeonClearRewardBonusMonsterGrowthContractIndex*4:]); got != 180 {
		t.Fatalf("monster growth-contract Bonus[%d]=%d, want 180", currentDungeonClearRewardBonusMonsterGrowthContractIndex, got)
	}
	scoreStart := bonusStart + currentDungeonClearRewardBonusFieldCount*4 + 2 + currentDungeonClearRewardPostBaseFieldCount*4
	wantScore := [currentDungeonClearRewardScoreFieldCount]uint32{0, 200, 100, 300}
	for index, want := range wantScore {
		if got := binary.LittleEndian.Uint32(body[scoreStart+index*4:]); got != want {
			t.Fatalf("score[%d]=%d, want %d", index, got, want)
		}
	}
	if got := binary.LittleEndian.Uint32(body[len(body)-16:]); got != 900 {
		t.Fatalf("monster total experience=%d, want 900", got)
	}
	committed, found, err := repositories.Character.Load(context.Background(), "99")
	if err != nil || !found {
		t.Fatalf("load committed character found=%t err=%v", found, err)
	}
	if committed.Level != 3 || committed.Stats["exp"] != 370 {
		t.Fatalf("committed progression level=%d exp=%d, want level=3 exp=370", committed.Level, committed.Stats["exp"])
	}

	// The durable receipt holds the total: a producer retry must not grant
	// the growth bonus a second time.
	runtime.settlementResultPlan = nil
	if err := service.produceCurrentDungeonSettlementPlanWithResourcesLocked(
		context.Background(), session, runtime, resources,
	); err != nil {
		t.Fatal(err)
	}
	replayed, _, _ := repositories.Character.Load(context.Background(), "99")
	if replayed.Level != 3 || replayed.Stats["exp"] != 370 {
		t.Fatalf("producer retry duplicated progression: %+v", replayed)
	}
}

func TestCurrentBlackDiamondBonusUsesRuntimePVFTypeAndKeepsLegacyAccounts(t *testing.T) {
	service, _, _, _, _ := prepareCompletedSettlementRuntime(t)
	repositories, ok := service.repositoryGroup()
	if !ok {
		t.Fatal("repository group unavailable")
	}
	future := strconv.FormatInt(time.Now().Add(24*time.Hour).Unix(), 10)
	for _, metadataKey := range []string{
		"premium_expire_29", // current runtime-PVF Black Diamond type
		"premium_expire_1",  // prior local compatibility profile
		"premium_expire_17", // prior local compatibility profile
	} {
		if err := repositories.Account.Save(context.Background(), dnfrepo.AccountRecord{
			AccountID: "account-1",
			Metadata:  map[string]string{metadataKey: future},
		}); err != nil {
			t.Fatal(err)
		}
		if got := service.currentBlackDiamondBonusExp(context.Background(), "account-1", 200); got != 20 {
			t.Fatalf("%s black diamond bonus = %d, want 20", metadataKey, got)
		}
	}
}
