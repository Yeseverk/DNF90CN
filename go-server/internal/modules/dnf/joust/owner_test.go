package joust

import (
	"context"
	"errors"
	"testing"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestOwnerBetAtomicallyConsumesCrystalGrantsParticipationAndPersistsLedger(t *testing.T) {
	ctx := context.Background()
	repos := betRepositories(t, 10)
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	result, err := owner.Bet(ctx, Command{
		AccountID:           "account-1",
		SelectedCharacterID: 19,
		Round:               7,
		Knight:              1,
		SourceSlot:          7,
		Amount:              3,
		KnightAllowed:       allowTestKnight,
		PlaceReward: func(inventory *dnfrepo.InventoryRecord) (int16, dnfrepo.ItemStack, error) {
			stack := dnfrepo.ItemStack{ItemID: ParticipationItemID, Count: 3}
			inventory.Slots["0:8"] = stack
			return 8, stack, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.RoundTotal != 3 || result.RemainingSource.Count != 7 || result.RewardSlot != 8 {
		t.Fatalf("result=%+v", result)
	}
	character, _, _ := repos.Character.Load(ctx, "19")
	if character.Stats[RoundStat] != 7 || character.Stats[KnightStat] != 1 ||
		character.Stats[AmountStat] != 3 || character.Stats[SourceItemIDStat] != PermanentCrystalID ||
		character.Stats[PendingStat] != 1 {
		t.Fatalf("stats=%v", character.Stats)
	}
	inventory, _, _ := repos.Inventory.Load(ctx, "19")
	if inventory.Slots["0:7"].Count != 7 || inventory.Slots["0:8"].ItemID != ParticipationItemID || inventory.Slots["0:8"].Count != 3 {
		t.Fatalf("inventory=%v", inventory.Slots)
	}
}

func TestOwnerBetPlacementFailureRollsBackCrystalAndLedger(t *testing.T) {
	ctx := context.Background()
	repos := betRepositories(t, 10)
	owner, _ := NewOwner(repos)
	_, err := owner.Bet(ctx, Command{
		SelectedCharacterID: 19,
		Round:               7,
		Knight:              1,
		SourceSlot:          7,
		Amount:              3,
		KnightAllowed:       allowTestKnight,
		PlaceReward: func(*dnfrepo.InventoryRecord) (int16, dnfrepo.ItemStack, error) {
			return 0, dnfrepo.ItemStack{}, errors.New("full")
		},
	})
	if !errors.Is(err, ErrRewardPlacement) {
		t.Fatalf("error=%v", err)
	}
	character, _, _ := repos.Character.Load(ctx, "19")
	inventory, _, _ := repos.Inventory.Load(ctx, "19")
	if character.Stats[PendingStat] != 0 || inventory.Slots["0:7"].Count != 10 {
		t.Fatalf("rollback stats=%v inventory=%v", character.Stats, inventory.Slots)
	}
}

func TestOwnerBetAllowsSplitSupportAcrossKnightsAndEnforcesTotalRoundLimit(t *testing.T) {
	ctx := context.Background()
	repos := betRepositories(t, 20_000)
	character, _, _ := repos.Character.Load(ctx, "19")
	character.Stats = map[string]int64{RoundStat: 7, KnightStat: 1, AmountStat: 9_999, PendingStat: 1}
	if err := repos.Character.Save(ctx, character); err != nil {
		t.Fatal(err)
	}
	owner, _ := NewOwner(repos)
	placer := func(inventory *dnfrepo.InventoryRecord) (int16, dnfrepo.ItemStack, error) {
		stack := dnfrepo.ItemStack{ItemID: ParticipationItemID, Count: 1}
		inventory.Slots["0:8"] = stack
		return 8, stack, nil
	}
	result, err := owner.Bet(ctx, Command{SelectedCharacterID: 19, Round: 7, Knight: 2, SourceSlot: 7, Amount: 1, KnightAllowed: allowTestKnight, PlaceReward: placer})
	if err != nil || result.RoundTotal != 10_000 {
		t.Fatalf("split wager result=%+v err=%v", result, err)
	}
	if _, err := owner.Bet(ctx, Command{SelectedCharacterID: 19, Round: 7, Knight: 1, SourceSlot: 7, Amount: 1, KnightAllowed: allowTestKnight, PlaceReward: placer}); !errors.Is(err, ErrRoundLimit) {
		t.Fatalf("limit error=%v", err)
	}
	character, _, _ = repos.Character.Load(ctx, "19")
	if character.Stats[KnightAmountStat(1)] != 9_999 || character.Stats[KnightAmountStat(2)] != 1 || character.Stats[AmountStat] != 10_000 {
		t.Fatalf("split stats=%v", character.Stats)
	}
	inventory, _, _ := repos.Inventory.Load(ctx, "19")
	if inventory.Slots["0:7"].Count != 19_999 {
		t.Fatalf("source count=%d", inventory.Slots["0:7"].Count)
	}
}

func allowTestKnight(knight byte) bool {
	return knight < OpeningKnightCount
}

func betRepositories(t *testing.T, count int64) dnfrepo.Group {
	t.Helper()
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "account-1",
		Level:       90,
		Stats:       map[string]int64{},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			"0:7": {ItemID: PermanentCrystalID, Count: count},
		},
	}); err != nil {
		t.Fatal(err)
	}
	return repos
}
