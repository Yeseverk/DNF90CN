package dnfbridge

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestNewDungeonCardRewardPlanIsStableAndDetached(t *testing.T) {
	identity := dungeonCardPlanIdentity{CharacterID: "77", DungeonID: 3, MazeIndex: 1, RunSeed: 0x10203040}
	raw := make([]byte, currentItemListEntryWireSize)
	extra := map[string]string{"source": "runtime_pvf"}
	free := dungeonCardRewardBundle{Gold: 100, Items: []dungeonCardItemReward{{
		ItemID: 3227, Count: 1, Stackable: true, SlotStart: 121, SlotEnd: 176, RawEntry: raw, Extra: extra,
	}}}

	first, err := newDungeonCardRewardPlan(identity, "runtime_pvf_clear_reward", free, dungeonCardRewardBundle{})
	if err != nil {
		t.Fatalf("new plan: %v", err)
	}
	second, err := newDungeonCardRewardPlan(identity, "runtime_pvf_clear_reward", free, dungeonCardRewardBundle{})
	if err != nil {
		t.Fatalf("new second plan: %v", err)
	}
	if first.ID != second.ID || len(first.ID) != 32 {
		t.Fatalf("stable plan ids = %q/%q", first.ID, second.ID)
	}
	different, err := newDungeonCardRewardPlan(identity, "runtime_pvf_clear_reward", dungeonCardRewardBundle{Gold: 101}, dungeonCardRewardBundle{})
	if err != nil || different.ID == first.ID {
		t.Fatalf("reward-sensitive plan id = %q err=%v", different.ID, err)
	}
	raw[0] = 9
	extra["source"] = "mutated"
	if first.Sides[dungeonCardSideFree].Items[0].RawEntry[0] != 0 || first.Sides[dungeonCardSideFree].Items[0].Extra["source"] != "runtime_pvf" {
		t.Fatal("plan retained caller-owned reward state")
	}
}

func TestDungeonCardStateReservesEachSideOnce(t *testing.T) {
	plan := mustTestDungeonCardPlan(t, dungeonCardRewardBundle{Gold: 10}, dungeonCardRewardBundle{Gold: 20})
	state, err := newDungeonCardState(plan)
	if err != nil {
		t.Fatalf("new state: %v", err)
	}

	free, result, err := state.reserveSelection(dungeonCardSideFree, 0)
	if err != nil || result != dungeonCardSelectionAccepted || !free.grant || free.bundle.Gold != 10 {
		t.Fatalf("free reservation = %+v result=%d err=%v", free, result, err)
	}
	replayBusy, result, err := state.reserveSelection(dungeonCardSideFree, 0)
	if err != nil || result != dungeonCardSelectionReplay || replayBusy.grant {
		t.Fatalf("busy replay = %+v result=%d err=%v", replayBusy, result, err)
	}
	state.finishDelivery(free, true)
	replayDone, result, err := state.reserveSelection(dungeonCardSideFree, 0)
	if err != nil || result != dungeonCardSelectionReplay || replayDone.grant {
		t.Fatalf("delivered replay = %+v result=%d err=%v", replayDone, result, err)
	}
	if _, _, err := state.reserveSelection(dungeonCardSideFree, 1); !errors.Is(err, errDungeonCardSelectionConflict) {
		t.Fatalf("conflicting selection error = %v", err)
	}
	paid, result, err := state.reserveSelection(dungeonCardSidePaid, 0)
	if err != nil || result != dungeonCardSelectionAccepted || !paid.grant || paid.bundle.Gold != 20 {
		t.Fatalf("paid reservation = %+v result=%d err=%v", paid, result, err)
	}
}

func TestDungeonCardStateRetriesFailedDelivery(t *testing.T) {
	plan := mustTestDungeonCardPlan(t, dungeonCardRewardBundle{Gold: 10}, dungeonCardRewardBundle{})
	state, _ := newDungeonCardState(plan)
	first, _, _ := state.reserveSelection(dungeonCardSideFree, 0)
	state.finishDelivery(first, false)
	retry, result, err := state.reserveSelection(dungeonCardSideFree, 0)
	if err != nil || result != dungeonCardSelectionReplay || !retry.grant || retry.bundle.Gold != 10 {
		t.Fatalf("failed delivery retry = %+v result=%d err=%v", retry, result, err)
	}
}

func TestDungeonCardStateConcurrentReplayHasOneGrantOwner(t *testing.T) {
	plan := mustTestDungeonCardPlan(t, dungeonCardRewardBundle{Gold: 10}, dungeonCardRewardBundle{})
	state, _ := newDungeonCardState(plan)
	var wg sync.WaitGroup
	grants := make(chan bool, 32)
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reservation, _, err := state.reserveSelection(dungeonCardSideFree, 0)
			grants <- err == nil && reservation.grant
		}()
	}
	wg.Wait()
	close(grants)
	count := 0
	for grant := range grants {
		if grant {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("grant owners = %d, want 1", count)
	}
}

func TestDeliverDungeonCardReservationCommitsGoldAndItemOnce(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	seedDungeonCardCharacter(t, ctx, repos, 100, map[string]dnfrepo.ItemStack{})
	free := dungeonCardRewardBundle{
		Gold: 25,
		Items: []dungeonCardItemReward{{
			ItemID: 3227, Count: 2, Stackable: true, SlotStart: 121, SlotEnd: 176,
			Extra: map[string]string{"source": "runtime_pvf"},
		}},
	}
	plan := mustTestDungeonCardPlan(t, free, dungeonCardRewardBundle{})
	state, _ := newDungeonCardState(plan)
	reservation, _, _ := state.reserveSelection(dungeonCardSideFree, 0)

	result, err := deliverDungeonCardReservation(ctx, state, reservation, repos, 0, time.Unix(100, 0).UTC())
	if err != nil {
		t.Fatalf("deliver reward: %v", err)
	}
	if result.GoldBefore != 100 || result.GoldAfter != 125 || len(result.ItemSlots) != 1 || result.ItemSlots[0] != 121 {
		t.Fatalf("grant result = %+v", result)
	}

	replay, replayResult, err := state.reserveSelection(dungeonCardSideFree, 0)
	if err != nil || replayResult != dungeonCardSelectionReplay || replay.grant {
		t.Fatalf("post-commit replay = %+v result=%d err=%v", replay, replayResult, err)
	}
	if _, err := deliverDungeonCardReservation(ctx, state, replay, repos, 0, time.Now()); err != nil {
		t.Fatalf("replay delivery: %v", err)
	}
	character, _, _ := repos.Character.Load(ctx, "77")
	bag, _, _ := repos.Inventory.Load(ctx, "77")
	if character.Stats["gold"] != 125 || bag.Slots["0:121"].ItemID != 3227 || bag.Slots["0:121"].Count != 2 {
		t.Fatalf("persisted assets = gold:%d slots:%+v", character.Stats["gold"], bag.Slots)
	}
}

func TestGoldCardDoublesEquipmentAsIndependentInstancesAndOneDisplayTuple(t *testing.T) {
	paid := dungeonCardRewardBundle{
		Gold: 19,
		Items: []dungeonCardItemReward{{
			ItemID: 31865,
			Count:  1,
			Extra:  map[string]string{"item_kind": "equipment"},
		}},
	}
	if err := applyCurrentDungeonGoldCardItemMultiplier(&paid); err != nil {
		t.Fatal(err)
	}
	if len(paid.Items) != 2 ||
		paid.Items[0].Count != 1 ||
		paid.Items[1].Count != 1 {
		t.Fatalf("gold-card physical equipment rewards=%+v, want two count-one instances", paid.Items)
	}
	paid.Items[0].Extra["mutated"] = "first"
	if paid.Items[1].Extra["mutated"] != "" {
		t.Fatal("gold-card equipment instances share mutable metadata")
	}
	tuples := currentDungeonOp71RewardTuplesFromCardBundle(paid)
	if len(tuples) != 2 ||
		tuples[0] != (currentDungeonOp71RewardTuple{ValueA: 0, ValueB: 19}) ||
		tuples[1] != (currentDungeonOp71RewardTuple{ValueA: 31865, ValueB: 2}) {
		t.Fatalf("gold-card display tuples=%+v, want gold plus equipment x2", tuples)
	}
}

func TestDeliverGoldCardReservationCommitsBothEquipmentInstances(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	seedDungeonCardCharacter(t, ctx, repos, 100, map[string]dnfrepo.ItemStack{})
	paid := dungeonCardRewardBundle{
		Gold: 19,
		Items: []dungeonCardItemReward{{
			ItemID: 31865, Count: 1, SlotStart: 9, SlotEnd: 64,
			Extra: map[string]string{"item_kind": "equipment"},
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
	reservation, _, err := state.reserveSelection(dungeonCardSidePaid, 0)
	if err != nil {
		t.Fatal(err)
	}
	result, err := deliverDungeonCardReservation(
		ctx,
		state,
		reservation,
		repos,
		0,
		time.Unix(100, 0).UTC(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.GoldBefore != 100 || result.GoldAfter != 119 ||
		len(result.ItemSlots) != 2 ||
		result.ItemSlots[0] != 9 ||
		result.ItemSlots[1] != 10 {
		t.Fatalf("gold-card grant result=%+v", result)
	}
	inventory, found, err := repos.Inventory.Load(ctx, "77")
	if err != nil || !found ||
		inventory.Slots["0:9"].ItemID != 31865 ||
		inventory.Slots["0:9"].Count != 1 ||
		inventory.Slots["0:10"].ItemID != 31865 ||
		inventory.Slots["0:10"].Count != 1 {
		t.Fatalf("gold-card inventory found=%t err=%v slots=%+v", found, err, inventory.Slots)
	}
}

func TestDeliverDungeonCardReservationRoutesItemToMailboxWhenBagFull(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	seedDungeonCardCharacter(t, ctx, repos, 100, map[string]dnfrepo.ItemStack{"0:9": {ItemID: 700, Count: 1}})
	free := dungeonCardRewardBundle{Gold: 25, Items: []dungeonCardItemReward{{
		ItemID: 3227, Count: 1, Stackable: false, SlotStart: 9, SlotEnd: 9,
	}}}
	state, _ := newDungeonCardState(mustTestDungeonCardPlan(t, free, dungeonCardRewardBundle{}))
	reservation, _, _ := state.reserveSelection(dungeonCardSideFree, 0)

	result, err := deliverDungeonCardReservation(ctx, state, reservation, repos, 0, time.Now())
	if err != nil {
		t.Fatalf("full bag delivery: %v", err)
	}
	if result.OverflowMailID == "" || len(result.ItemSlots) != 1 || result.ItemSlots[0] != -1 {
		t.Fatalf("full bag result = %+v", result)
	}
	character, _, _ := repos.Character.Load(ctx, "77")
	if character.Stats["gold"] != 125 {
		t.Fatalf("gold after overflow delivery = %d", character.Stats["gold"])
	}
	mailbox, found, loadErr := repos.Mailbox.Load(ctx, "77")
	if loadErr != nil || !found {
		t.Fatalf("load overflow mailbox found=%t err=%v", found, loadErr)
	}
	mail := mailbox.Mails[result.OverflowMailID]
	if len(mail.Attachments) != 1 || mail.Attachments[0].ItemID != 3227 || mail.Attachments[0].Count != 1 {
		t.Fatalf("overflow mail = %+v", mail)
	}
	replay, replayResult, replayErr := state.reserveSelection(dungeonCardSideFree, 0)
	if replayErr != nil || replayResult != dungeonCardSelectionReplay || replay.grant {
		t.Fatalf("post-mail replay = %+v result=%d err=%v", replay, replayResult, replayErr)
	}
}

func mustTestDungeonCardPlan(t *testing.T, free dungeonCardRewardBundle, paid dungeonCardRewardBundle) dungeonCardRewardPlan {
	t.Helper()
	plan, err := newDungeonCardRewardPlan(
		dungeonCardPlanIdentity{CharacterID: "77", DungeonID: 3, MazeIndex: 1, RunSeed: 9},
		"test_resolved_rewards",
		free,
		paid,
	)
	if err != nil {
		t.Fatalf("new test plan: %v", err)
	}
	return plan
}

func seedDungeonCardCharacter(t *testing.T, ctx context.Context, repos dnfrepo.Group, gold int64, slots map[string]dnfrepo.ItemStack) {
	t.Helper()
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "77", AccountID: "dnf:1", Stats: map[string]int64{"gold": gold}}); err != nil {
		t.Fatalf("seed character: %v", err)
	}
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{CharacterID: "77", Slots: slots}); err != nil {
		t.Fatalf("seed inventory: %v", err)
	}
	if err := repos.Equipment.Save(ctx, dnfrepo.EquipmentRecord{CharacterID: "77", Entries: map[string]dnfrepo.EquipmentEntry{}}); err != nil {
		t.Fatalf("seed equipment: %v", err)
	}
}
