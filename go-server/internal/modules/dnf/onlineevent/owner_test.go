package onlineevent

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestOwnerObserveMergesReplayOverlapAndGapWithoutDoubleCredit(t *testing.T) {
	ctx := context.Background()
	repositories := seededRepositories(t, ctx)
	owner := mustOwner(t, repositories)
	definition := testDefinition()
	location := definition.calendar()
	base := time.Date(2026, 8, 3, 10, 0, 0, 0, location)

	observe := func(from time.Time, to time.Time) ObserveResult {
		t.Helper()
		result, err := owner.Observe(ctx, ObserveCommand{
			AccountID:    "account-1",
			CharacterID:  "character-1",
			Definition:   definition,
			IntervalFrom: from,
			IntervalTo:   to,
		})
		if err != nil {
			t.Fatalf("Observe(%s, %s): %v", from, to, err)
		}
		return result
	}

	first := observe(base, base.Add(10*time.Second))
	if first.CreditedSeconds != 10 || first.Snapshot.OnlineSeconds != 10 || !first.Changed {
		t.Fatalf("first=%+v", first)
	}
	replay := observe(base, base.Add(10*time.Second))
	if replay.CreditedSeconds != 0 || replay.Snapshot.OnlineSeconds != 10 || replay.Changed {
		t.Fatalf("replay=%+v", replay)
	}
	overlap := observe(base.Add(5*time.Second), base.Add(15*time.Second))
	if overlap.CreditedSeconds != 5 || overlap.Snapshot.OnlineSeconds != 15 {
		t.Fatalf("overlap=%+v", overlap)
	}
	gap := observe(base.Add(20*time.Second), base.Add(25*time.Second))
	if gap.CreditedSeconds != 5 || gap.Snapshot.OnlineSeconds != 20 {
		t.Fatalf("gap=%+v", gap)
	}

	account, found, err := repositories.Account.Load(ctx, "account-1")
	if err != nil || !found {
		t.Fatalf("load account found=%t err=%v", found, err)
	}
	if account.Metadata["unrelated"] != "preserved" {
		t.Fatalf("unrelated metadata=%v", account.Metadata)
	}
	state, err := parseState(account.Metadata[metadataKey(definition.ID)], definition)
	if err != nil {
		t.Fatal(err)
	}
	if state.OnlineSeconds != 20 || !reflect.DeepEqual(state.Intervals, []creditedInterval{
		{Start: 4 * 60 * 60, End: 4*60*60 + 15},
		{Start: 4*60*60 + 20, End: 4*60*60 + 25},
	}) {
		t.Fatalf("state=%+v", state)
	}
}

func TestOwnerObserveResetsProgressAndClaimsAtDefaultShanghaiSixAM(t *testing.T) {
	ctx := context.Background()
	repositories := seededRepositories(t, ctx)
	owner := mustOwner(t, repositories)
	definition := testDefinition()
	location := definition.calendar()
	dayOne := time.Date(2026, 8, 4, 5, 59, 40, 0, location)

	if _, err := owner.Observe(ctx, ObserveCommand{
		AccountID:    "account-1",
		CharacterID:  "character-1",
		Definition:   definition,
		IntervalFrom: dayOne,
		IntervalTo:   dayOne.Add(10 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Claim(ctx, ClaimCommand{
		AccountID:   "account-1",
		CharacterID: "character-1",
		Definition:  definition,
		StageID:     "ten_seconds",
		ClaimedAt:   dayOne.Add(11 * time.Second),
		Allocate:    testAllocator(nil),
	}); err != nil {
		t.Fatal(err)
	}

	crossMidnight, err := owner.Observe(ctx, ObserveCommand{
		AccountID:    "account-1",
		CharacterID:  "character-1",
		Definition:   definition,
		IntervalFrom: dayOne.Add(15 * time.Second),
		IntervalTo:   dayOne.Add(30 * time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if crossMidnight.Snapshot.CalendarDate != "2026-08-04" ||
		crossMidnight.CreditedSeconds != 10 || crossMidnight.Snapshot.OnlineSeconds != 10 ||
		len(crossMidnight.Snapshot.ClaimedStageIDs) != 0 {
		t.Fatalf("crossBoundary=%+v", crossMidnight)
	}
	secondClaim, err := owner.Claim(ctx, ClaimCommand{
		AccountID:   "account-1",
		CharacterID: "character-1",
		Definition:  definition,
		StageID:     "ten_seconds",
		ClaimedAt:   dayOne.Add(31 * time.Second),
		Allocate:    testAllocator(nil),
	})
	if err != nil || secondClaim.Replayed {
		t.Fatalf("secondClaim=%+v err=%v", secondClaim, err)
	}
}

func TestOwnerObserveEndingExactlyAtSixAMCreditsThePriorServiceDay(t *testing.T) {
	ctx := context.Background()
	repositories := seededRepositories(t, ctx)
	owner := mustOwner(t, repositories)
	definition := testDefinition()
	location := definition.calendar()
	boundary := time.Date(2026, 8, 4, 6, 0, 0, 0, location)

	result, err := owner.Observe(ctx, ObserveCommand{
		AccountID:    "account-1",
		CharacterID:  "character-1",
		Definition:   definition,
		IntervalFrom: boundary.Add(-10 * time.Second),
		IntervalTo:   boundary,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Snapshot.CalendarDate != "2026-08-03" ||
		result.CreditedSeconds != 10 || result.Snapshot.OnlineSeconds != 10 {
		t.Fatalf("prior-day result=%+v", result)
	}

	status, err := owner.Status(ctx, StatusCommand{
		AccountID:   "account-1",
		CharacterID: "character-1",
		Definition:  definition,
		ObservedAt:  boundary,
	})
	if err != nil {
		t.Fatal(err)
	}
	if status.CalendarDate != "2026-08-04" || status.OnlineSeconds != 0 || len(status.ClaimedStageIDs) != 0 {
		t.Fatalf("new-day status=%+v", status)
	}
}

func TestOwnerObserveDoesNotResetAtMidnightWithDefaultBoundary(t *testing.T) {
	ctx := context.Background()
	repositories := seededRepositories(t, ctx)
	owner := mustOwner(t, repositories)
	definition := testDefinition()
	location := definition.calendar()
	from := time.Date(2026, 8, 3, 23, 59, 55, 0, location)
	result, err := owner.Observe(ctx, ObserveCommand{
		AccountID:    "account-1",
		CharacterID:  "character-1",
		Definition:   definition,
		IntervalFrom: from,
		IntervalTo:   from.Add(15 * time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Snapshot.CalendarDate != "2026-08-03" || result.CreditedSeconds != 15 ||
		result.Snapshot.OnlineSeconds != 15 {
		t.Fatalf("result=%+v", result)
	}
}

func TestOwnerObserveSupportsExplicitMidnightBoundary(t *testing.T) {
	ctx := context.Background()
	repositories := seededRepositories(t, ctx)
	owner := mustOwner(t, repositories)
	definition := testDefinition()
	boundary := DailyBoundary{}
	definition.Boundary = &boundary
	location := definition.calendar()
	from := time.Date(2026, 8, 3, 23, 59, 55, 0, location)
	result, err := owner.Observe(ctx, ObserveCommand{
		AccountID:    "account-1",
		CharacterID:  "character-1",
		Definition:   definition,
		IntervalFrom: from,
		IntervalTo:   from.Add(15 * time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Snapshot.CalendarDate != "2026-08-04" || result.CreditedSeconds != 10 ||
		result.Snapshot.OnlineSeconds != 10 {
		t.Fatalf("result=%+v", result)
	}
}

func TestOwnerStatusPersistsDailyResetWithoutCreditingTime(t *testing.T) {
	ctx := context.Background()
	repositories := seededRepositories(t, ctx)
	owner := mustOwner(t, repositories)
	definition := testDefinition()
	location := definition.calendar()
	dayOne := time.Date(2026, 8, 3, 12, 0, 0, 0, location)
	if _, err := owner.Observe(ctx, ObserveCommand{
		AccountID:    "account-1",
		CharacterID:  "character-1",
		Definition:   definition,
		IntervalFrom: dayOne,
		IntervalTo:   dayOne.Add(10 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Claim(ctx, ClaimCommand{
		AccountID:   "account-1",
		CharacterID: "character-1",
		Definition:  definition,
		StageID:     "ten_seconds",
		ClaimedAt:   dayOne.Add(11 * time.Second),
		Allocate:    testAllocator(nil),
	}); err != nil {
		t.Fatal(err)
	}

	status, err := owner.Status(ctx, StatusCommand{
		AccountID:   "account-1",
		CharacterID: "character-1",
		Definition:  definition,
		ObservedAt:  dayOne.AddDate(0, 0, 1),
	})
	if err != nil {
		t.Fatal(err)
	}
	if status.CalendarDate != "2026-08-04" || status.OnlineSeconds != 0 || len(status.ClaimedStageIDs) != 0 {
		t.Fatalf("status=%+v", status)
	}
	account, _, _ := repositories.Account.Load(ctx, "account-1")
	state, err := parseState(account.Metadata[metadataKey(definition.ID)], definition)
	if err != nil || state.CalendarDate != "2026-08-04" || state.OnlineSeconds != 0 || len(state.Claims) != 0 {
		t.Fatalf("state=%+v err=%v", state, err)
	}
}

func TestOwnerClaimAtomicallyGrantsAndReplaysStoredReceipt(t *testing.T) {
	ctx := context.Background()
	repositories := seededRepositories(t, ctx)
	owner := mustOwner(t, repositories)
	definition := testDefinition()
	location := definition.calendar()
	base := time.Date(2026, 8, 3, 12, 0, 0, 0, location)
	if _, err := owner.Observe(ctx, ObserveCommand{
		AccountID:    "account-1",
		CharacterID:  "character-1",
		Definition:   definition,
		IntervalFrom: base,
		IntervalTo:   base.Add(12 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}

	allocatorCalls := 0
	command := ClaimCommand{
		AccountID:   "account-1",
		CharacterID: "character-1",
		Definition:  definition,
		StageID:     "ten_seconds",
		ClaimedAt:   base.Add(13 * time.Second),
		Allocate:    testAllocator(&allocatorCalls),
	}
	first, err := owner.Claim(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	if first.Replayed || allocatorCalls != 1 || first.GoldBefore != 7 || first.GoldAfter != 12 ||
		len(first.Items) != 1 || first.Items[0].ItemID != 2001 || first.Items[0].PostCount != 2 {
		t.Fatalf("first=%+v calls=%d", first, allocatorCalls)
	}
	first.Items[0].RawEntry[0] = 0xff
	replay, err := owner.Claim(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	if !replay.Replayed || allocatorCalls != 1 || replay.Items[0].RawEntry[0] != 0x20 {
		t.Fatalf("replay=%+v calls=%d", replay, allocatorCalls)
	}

	character, found, err := repositories.Character.Load(ctx, "character-1")
	if err != nil || !found || character.Stats["gold"] != 12 {
		t.Fatalf("character=%+v found=%t err=%v", character, found, err)
	}
	inventory, found, err := repositories.Inventory.Load(ctx, "character-1")
	if err != nil || !found || inventory.Slots["0:9"].ItemID != 2001 || inventory.Slots["0:9"].Count != 2 {
		t.Fatalf("inventory=%+v found=%t err=%v", inventory, found, err)
	}
	account, found, err := repositories.Account.Load(ctx, "account-1")
	if err != nil || !found {
		t.Fatalf("account found=%t err=%v", found, err)
	}
	state, err := parseState(account.Metadata[metadataKey(definition.ID)], definition)
	if err != nil || len(state.Claims) != 1 || state.Claims["ten_seconds"].GoldAfter != 12 {
		t.Fatalf("state=%+v err=%v", state, err)
	}
}

func TestOwnerClaimRollbackKeepsRewardAndLedgerTogether(t *testing.T) {
	ctx := context.Background()
	repositories := seededRepositories(t, ctx)
	owner := mustOwner(t, repositories)
	definition := testDefinition()
	location := definition.calendar()
	base := time.Date(2026, 8, 3, 12, 0, 0, 0, location)
	if _, err := owner.Observe(ctx, ObserveCommand{
		AccountID:    "account-1",
		CharacterID:  "character-1",
		Definition:   definition,
		IntervalFrom: base,
		IntervalTo:   base.Add(12 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}

	wantErr := errors.New("reject event settlement")
	rejecting := mustOwner(t, dnfrepo.Group{
		CharacterSettlement: rejectAfterApplySettlement{
			inner: repositories.CharacterSettlement,
			err:   wantErr,
		},
	})
	_, err := rejecting.Claim(ctx, ClaimCommand{
		AccountID:   "account-1",
		CharacterID: "character-1",
		Definition:  definition,
		StageID:     "ten_seconds",
		ClaimedAt:   base.Add(13 * time.Second),
		Allocate:    testAllocator(nil),
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Claim error=%v, want=%v", err, wantErr)
	}
	character, _, _ := repositories.Character.Load(ctx, "character-1")
	if character.Stats["gold"] != 7 {
		t.Fatalf("gold=%d", character.Stats["gold"])
	}
	inventory, _, _ := repositories.Inventory.Load(ctx, "character-1")
	if len(inventory.Slots) != 0 {
		t.Fatalf("inventory=%v", inventory.Slots)
	}
	account, _, _ := repositories.Account.Load(ctx, "account-1")
	state, parseErr := parseState(account.Metadata[metadataKey(definition.ID)], definition)
	if parseErr != nil || len(state.Claims) != 0 || state.OnlineSeconds != 12 {
		t.Fatalf("state=%+v err=%v", state, parseErr)
	}
}

func TestOwnerRejectsLockedCorruptAndStaleStateWithoutAllocating(t *testing.T) {
	ctx := context.Background()
	repositories := seededRepositories(t, ctx)
	owner := mustOwner(t, repositories)
	definition := testDefinition()
	location := definition.calendar()
	base := time.Date(2026, 8, 3, 12, 0, 0, 0, location)
	if _, err := owner.Observe(ctx, ObserveCommand{
		AccountID:    "account-1",
		CharacterID:  "character-1",
		Definition:   definition,
		IntervalFrom: base,
		IntervalTo:   base.Add(5 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	allocatorCalls := 0
	if _, err := owner.Claim(ctx, ClaimCommand{
		AccountID:   "account-1",
		CharacterID: "character-1",
		Definition:  definition,
		StageID:     "ten_seconds",
		ClaimedAt:   base.Add(6 * time.Second),
		Allocate:    testAllocator(&allocatorCalls),
	}); !errors.Is(err, ErrStageLocked) || allocatorCalls != 0 {
		t.Fatalf("locked err=%v calls=%d", err, allocatorCalls)
	}

	account, _, _ := repositories.Account.Load(ctx, "account-1")
	account.Metadata[metadataKey(definition.ID)] = `{"version":1,"event_id":"daily-online","calendar_date":"2026-08-03","online_seconds":99,"intervals":[]}`
	if err := repositories.Account.Save(ctx, account); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Claim(ctx, ClaimCommand{
		AccountID:   "account-1",
		CharacterID: "character-1",
		Definition:  definition,
		StageID:     "ten_seconds",
		ClaimedAt:   base.Add(7 * time.Second),
		Allocate:    testAllocator(&allocatorCalls),
	}); !errors.Is(err, ErrStateInvalid) || allocatorCalls != 0 {
		t.Fatalf("corrupt err=%v calls=%d", err, allocatorCalls)
	}

	valid := newState(definition.ID)
	valid.resetTo("2026-08-04")
	encoded, err := encodeState(valid)
	if err != nil {
		t.Fatal(err)
	}
	account, _, _ = repositories.Account.Load(ctx, "account-1")
	account.Metadata[metadataKey(definition.ID)] = encoded
	if err := repositories.Account.Save(ctx, account); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Observe(ctx, ObserveCommand{
		AccountID:    "account-1",
		CharacterID:  "character-1",
		Definition:   definition,
		IntervalFrom: base,
		IntervalTo:   base.Add(time.Second),
	}); !errors.Is(err, ErrObservationStale) {
		t.Fatalf("stale err=%v", err)
	}
}

func TestDefinitionRejectsAmbiguousOrImpossibleDailyCatalog(t *testing.T) {
	badBoundary := DailyBoundary{Hour: 24}
	tests := []Definition{
		{ID: "bad event", Stages: []Stage{{ID: "a", RequiredSeconds: 1, Gold: 1}}},
		{ID: "event", Stages: []Stage{{ID: "a", RequiredSeconds: 0, Gold: 1}}},
		{ID: "event", Stages: []Stage{{ID: "a", RequiredSeconds: secondsPerDay + 1, Gold: 1}}},
		{ID: "event", Stages: []Stage{{ID: "a", RequiredSeconds: 2, Gold: 1}, {ID: "b", RequiredSeconds: 1, Gold: 1}}},
		{ID: "event", Stages: []Stage{{ID: "a", RequiredSeconds: 1}}},
		{ID: "event", Stages: []Stage{{ID: "a", RequiredSeconds: 1, Items: []ItemReward{{ItemID: 0, Count: 1}}}}},
		{ID: "event", Boundary: &badBoundary, Stages: []Stage{{ID: "a", RequiredSeconds: 1, Gold: 1}}},
	}
	for index, definition := range tests {
		if err := definition.Validate(); !errors.Is(err, ErrDefinitionInvalid) {
			t.Fatalf("case %d error=%v", index, err)
		}
	}
}

func TestParseStateValidatesClaimAgainstConfiguredServiceDate(t *testing.T) {
	definition := testDefinition()
	location := definition.calendar()
	state := newState(definition.ID)
	state.resetTo("2026-08-03")
	state.Intervals = []creditedInterval{{Start: 0, End: 10}}
	state.OnlineSeconds = 10
	state.Claims["ten_seconds"] = claimReceipt{
		StageID:     "ten_seconds",
		CharacterID: "character-1",
		Items: []CommittedItem{{
			SlotKey:   "0:9",
			SlotIndex: 9,
			ItemID:    2001,
			Delta:     2,
			PostCount: 2,
		}},
		GoldBefore: 7,
		GoldAfter:  12,
		ClaimedAt:  time.Date(2026, 8, 4, 5, 59, 0, 0, location),
	}
	encoded, err := encodeState(state)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseState(encoded, definition); err != nil {
		t.Fatalf("05:59 should share service date: %v", err)
	}
	receipt := state.Claims["ten_seconds"]
	receipt.ClaimedAt = time.Date(2026, 8, 4, 6, 0, 0, 0, location)
	state.Claims["ten_seconds"] = receipt
	encoded, err = encodeState(state)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseState(encoded, definition); !errors.Is(err, ErrStateInvalid) {
		t.Fatalf("06:00 claim date error=%v", err)
	}
}

func seededRepositories(t *testing.T, ctx context.Context) dnfrepo.Group {
	t.Helper()
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Account.Save(ctx, dnfrepo.AccountRecord{
		AccountID: "account-1",
		Metadata:  map[string]string{"unrelated": "preserved"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "character-1",
		AccountID:   "account-1",
		Stats:       map[string]int64{"gold": 7},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "character-1",
		Slots:       make(map[string]dnfrepo.ItemStack),
	}); err != nil {
		t.Fatal(err)
	}
	return repositories
}

func testDefinition() Definition {
	return Definition{
		ID:       "daily-online",
		Calendar: time.FixedZone("Asia/Shanghai", 8*60*60),
		Stages: []Stage{{
			ID:              "ten_seconds",
			RequiredSeconds: 10,
			Gold:            5,
			Items:           []ItemReward{{ItemID: 2001, Count: 2}},
		}},
	}
}

func mustOwner(t *testing.T, repositories dnfrepo.Group) *Owner {
	t.Helper()
	owner, err := NewOwner(repositories)
	if err != nil {
		t.Fatal(err)
	}
	return owner
}

func testAllocator(calls *int) ItemAllocator {
	return func(record *dnfrepo.InventoryRecord, reward ItemReward) (CommittedItem, error) {
		if calls != nil {
			*calls++
		}
		if record == nil || record.Slots == nil {
			return CommittedItem{}, ErrInventoryNotFound
		}
		const slotKey = "0:9"
		stack := record.Slots[slotKey]
		if stack.ItemID != 0 && stack.ItemID != reward.ItemID {
			return CommittedItem{}, fmt.Errorf("occupied test slot")
		}
		stack.ItemID = reward.ItemID
		stack.Count += reward.Count
		stack.RawEntry = []byte{0x20, byte(stack.Count)}
		record.Slots[slotKey] = stack
		return CommittedItem{
			SlotKey:   slotKey,
			SlotIndex: 9,
			ItemID:    reward.ItemID,
			Delta:     reward.Count,
			PostCount: stack.Count,
			RawEntry:  append([]byte(nil), stack.RawEntry...),
		}, nil
	}
}

type rejectAfterApplySettlement struct {
	inner dnfrepo.CharacterSettlementUnitOfWork
	err   error
}

func (r rejectAfterApplySettlement) WithinCharacterSettlement(
	ctx context.Context,
	characterID string,
	apply func(dnfrepo.Group) error,
) error {
	return r.inner.WithinCharacterSettlement(ctx, characterID, func(tx dnfrepo.Group) error {
		if err := apply(tx); err != nil {
			return err
		}
		return r.err
	})
}
