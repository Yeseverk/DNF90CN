package dungeon

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"longheng.io/server/internal/modules/dnf/adventuregroup"
	dnfhonor "longheng.io/server/internal/modules/dnf/honor"
	"longheng.io/server/internal/modules/dnf/progression"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestOwnerGrantCardRewardCommitsWalletAndInventoryTogether(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	seedDungeonOwnerCharacter(t, ctx, repos, 100)
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			"0:0": {ItemID: 7001, Count: 2},
		},
	}); err != nil {
		t.Fatalf("seed inventory: %v", err)
	}
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner: %v", err)
	}
	expireAt := time.Unix(2000, 0).UTC()
	updatedAt := time.Unix(1000, 0).UTC()
	result, err := owner.GrantCardReward(ctx, CardRewardCommand{
		CharacterID: "19",
		MainSlots:   3,
		Bundle: CardRewardBundle{
			Gold: 50,
			Items: []CardItemReward{{
				Stack:     dnfrepo.ItemStack{ItemID: 7001, Count: 3},
				Stackable: true,
				ExpireAt:  expireAt,
			}},
		},
		UpdatedAt: updatedAt,
		Project: func(stack dnfrepo.ItemStack, expiration time.Time) (dnfrepo.ItemStack, error) {
			stack.ExpireAt = expiration
			return stack, nil
		},
	})
	if err != nil {
		t.Fatalf("GrantCardReward: %v", err)
	}
	if result.GoldBefore != 100 || result.GoldAfter != 150 ||
		len(result.ItemSlots) != 1 || result.ItemSlots[0] != 0 {
		t.Fatalf("result = %+v", result)
	}
	character, _, err := repos.Character.Load(ctx, "19")
	if err != nil {
		t.Fatalf("load character: %v", err)
	}
	inventory, _, err := repos.Inventory.Load(ctx, "19")
	if err != nil {
		t.Fatalf("load inventory: %v", err)
	}
	if character.Stats["gold"] != 150 ||
		inventory.Slots["0:0"].Count != 5 ||
		!inventory.Slots["0:0"].ExpireAt.Equal(expireAt) {
		t.Fatalf("character=%+v inventory=%+v", character, inventory)
	}
}

func TestOwnerGrantCardRewardRoutesItemToMailboxWhenInventoryIsFull(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	seedDungeonOwnerCharacter(t, ctx, repos, 100)
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			"0:0": {ItemID: 1, Count: 1},
		},
	}); err != nil {
		t.Fatalf("seed inventory: %v", err)
	}
	owner, _ := NewOwner(repos)
	result, err := owner.GrantCardReward(ctx, CardRewardCommand{
		CharacterID: "19",
		MainSlots:   1,
		Bundle: CardRewardBundle{
			Gold: 50,
			Items: []CardItemReward{{
				Stack: dnfrepo.ItemStack{ItemID: 2, Count: 1},
			}},
		},
	})
	if err != nil {
		t.Fatalf("GrantCardReward: %v", err)
	}
	if result.OverflowMailID == "" || len(result.ItemSlots) != 1 || result.ItemSlots[0] != -1 {
		t.Fatalf("result = %+v", result)
	}
	character, _, err := repos.Character.Load(ctx, "19")
	if err != nil {
		t.Fatalf("load character: %v", err)
	}
	if character.Stats["gold"] != 150 {
		t.Fatalf("gold = %d, want 150", character.Stats["gold"])
	}
	mailbox, found, err := repos.Mailbox.Load(ctx, "19")
	if err != nil || !found {
		t.Fatalf("load mailbox found=%t err=%v", found, err)
	}
	mail := mailbox.Mails[result.OverflowMailID]
	if len(mail.Attachments) != 1 || mail.Attachments[0].ItemID != 2 || mail.Attachments[0].Count != 1 {
		t.Fatalf("overflow mail = %+v", mail)
	}
	if mail.SenderName != "系统" || mail.Title != "背包已满：通关奖励" ||
		mail.Body != "背包空间不足，通关奖励已通过邮件发送。请清理对应道具分页后领取。" {
		t.Fatalf("overflow mail prompt = %+v", mail)
	}
}

func TestOwnerGrantPickupGoldUsesCharacterAssetTransaction(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	seedDungeonOwnerCharacter(t, ctx, repos, 100)
	owner, _ := NewOwner(repos)
	result, err := owner.GrantPickupGold(ctx, GoldPickupCommand{
		CharacterID: "19",
		Amount:      25,
	})
	if err != nil {
		t.Fatalf("GrantPickupGold: %v", err)
	}
	if result.GoldBefore != 100 || result.GoldAfter != 125 {
		t.Fatalf("result = %+v", result)
	}
	character, _, err := repos.Character.Load(ctx, "19")
	if err != nil {
		t.Fatalf("load character: %v", err)
	}
	if character.Stats["gold"] != 125 {
		t.Fatalf("gold = %d, want 125", character.Stats["gold"])
	}
}

func TestOwnerGrantPickupItemOwnsPlacementAndPersistence(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			"0:3": {ItemID: 7001, Count: 2},
		},
	}); err != nil {
		t.Fatalf("seed inventory: %v", err)
	}
	owner, _ := NewOwner(repos)
	result, err := owner.GrantPickupItem(ctx, PickupItemCommand{
		CharacterID: "19",
		Placement: PickupItemPlacement{
			Definition: PickupItemDefinition{
				ItemID:          7001,
				Kind:            PickupItemStackable,
				StackLimit:      10,
				SlotStart:       65,
				SlotEnd:         120,
				PreferQuickSlot: true,
			},
			Amount: 3,
			BuildNew: func(int16) (dnfrepo.ItemStack, error) {
				return dnfrepo.ItemStack{ItemID: 7001, Count: 3}, nil
			},
		},
		Finalize: func(_ int16, stack dnfrepo.ItemStack) (dnfrepo.ItemStack, error) {
			stack.RawEntry = []byte{5}
			return stack, nil
		},
	})
	if err != nil {
		t.Fatalf("GrantPickupItem: %v", err)
	}
	if result.Slot != 3 || result.Stack.Count != 5 {
		t.Fatalf("result = %+v", result)
	}
	inventory, _, err := repos.Inventory.Load(ctx, "19")
	if err != nil {
		t.Fatalf("load inventory: %v", err)
	}
	if inventory.Slots["0:3"].Count != 5 ||
		len(inventory.Slots["0:3"].RawEntry) != 1 ||
		inventory.Slots["0:3"].RawEntry[0] != 5 {
		t.Fatalf("inventory = %+v", inventory)
	}
}

func TestPlacePickupItemTreatsZeroStackLimitAsUnlimited(t *testing.T) {
	record := dnfrepo.InventoryRecord{Slots: map[string]dnfrepo.ItemStack{
		"0:65": {ItemID: 7001, Count: 2},
	}}
	slot, stack, err := PlacePickupItem(&record, PickupItemPlacement{
		Definition: PickupItemDefinition{
			ItemID:     7001,
			Kind:       PickupItemStackable,
			StackLimit: 0,
			SlotStart:  65,
			SlotEnd:    120,
		},
		Amount: 3,
		BuildNew: func(int16) (dnfrepo.ItemStack, error) {
			return dnfrepo.ItemStack{ItemID: 7001, Count: 3}, nil
		},
	})
	if err != nil {
		t.Fatalf("PlacePickupItem: %v", err)
	}
	if slot != 65 || stack.Count != 5 || record.Slots["0:65"].Count != 5 || len(record.Slots) != 1 {
		t.Fatalf("slot=%d stack=%+v inventory=%+v", slot, stack, record.Slots)
	}
}

func TestOwnerGrantTutorialRewardIsAtomicAndIdempotent(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	seedDungeonOwnerCharacter(t, ctx, repos, 0)
	expireAt := time.Unix(3000, 0).UTC()
	owner, _ := NewOwner(repos)
	command := TutorialRewardCommand{
		CharacterID: "19",
		Progress:    38,
		Rewards: []TutorialItemReward{{
			Progress:      38,
			ItemID:        8001,
			Count:         2,
			Consumable:    true,
			StackLimit:    10,
			SlotStart:     65,
			SlotEnd:       120,
			ExpireAt:      expireAt,
			PVFPath:       "stackable/test.stk",
			StackableType: "[waste]",
		}},
		Project: func(stack dnfrepo.ItemStack, expiration time.Time) (dnfrepo.ItemStack, error) {
			stack.ExpireAt = expiration
			return stack, nil
		},
	}
	result, err := owner.GrantTutorialReward(ctx, command)
	if err != nil {
		t.Fatalf("GrantTutorialReward: %v", err)
	}
	if !result.Granted || len(result.Rows) != 1 || result.Rows[0].Slot != 3 {
		t.Fatalf("result = %+v", result)
	}
	replay, err := owner.GrantTutorialReward(ctx, command)
	if err != nil {
		t.Fatalf("replay GrantTutorialReward: %v", err)
	}
	if replay.Granted || len(replay.Rows) != 0 {
		t.Fatalf("replay = %+v", replay)
	}
	character, _, err := repos.Character.Load(ctx, "19")
	if err != nil {
		t.Fatalf("load character: %v", err)
	}
	inventory, _, err := repos.Inventory.Load(ctx, "19")
	if err != nil {
		t.Fatalf("load inventory: %v", err)
	}
	if character.Stats[TutorialRewardMarker(38)] != 1 ||
		inventory.Slots["0:3"].Count != 2 ||
		!inventory.Slots["0:3"].ExpireAt.Equal(expireAt) {
		t.Fatalf("character=%+v inventory=%+v", character, inventory)
	}
}

func TestOwnerConsumeReviveCoinRetainsWalletSlotAtZero(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			"0:1": {ItemID: 1, Count: 1},
			"0:5": {ItemID: 1, Count: 2},
		},
	}); err != nil {
		t.Fatalf("seed inventory: %v", err)
	}
	owner, _ := NewOwner(repos)
	result, err := owner.ConsumeReviveCoin(ctx, ReviveCoinCommand{
		CharacterID: "19",
		ItemID:      1,
		WalletSlot:  1,
		AllowFree:   true,
		Project: func(_ int16, stack dnfrepo.ItemStack) (dnfrepo.ItemStack, error) {
			stack.RawEntry = []byte{byte(stack.Count)}
			return stack, nil
		},
	})
	if err != nil {
		t.Fatalf("ConsumeReviveCoin: %v", err)
	}
	if !result.Consumed || result.FreeRevive || result.Slot != 1 ||
		result.CountAfter != 0 || result.Removed {
		t.Fatalf("result = %+v", result)
	}
	inventory, _, err := repos.Inventory.Load(ctx, "19")
	if err != nil {
		t.Fatalf("load inventory: %v", err)
	}
	if inventory.Slots["0:1"].Count != 0 ||
		inventory.Slots["0:5"].Count != 2 ||
		inventory.Slots["0:1"].Extra["last_consume_source"] != "dungeon_use_coin_revive" {
		t.Fatalf("inventory = %+v", inventory)
	}
}

func TestOwnerMutateOwnedInventoryValidatesCharacterAccount(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "account-1",
	}); err != nil {
		t.Fatalf("seed character: %v", err)
	}
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			"0:5": {ItemID: 9001, Count: 2},
		},
	}); err != nil {
		t.Fatalf("seed inventory: %v", err)
	}
	owner, _ := NewOwner(repos)
	err := owner.MutateOwnedInventory(ctx, OwnedInventoryMutationCommand{
		AccountID:   "account-1",
		CharacterID: "19",
		Apply: func(slots map[string]dnfrepo.ItemStack) (bool, error) {
			stack := slots["0:5"]
			stack.Count--
			slots["0:5"] = stack
			return true, nil
		},
	})
	if err != nil {
		t.Fatalf("MutateOwnedInventory: %v", err)
	}
	inventory, _, err := repos.Inventory.Load(ctx, "19")
	if err != nil {
		t.Fatalf("load inventory: %v", err)
	}
	if inventory.Slots["0:5"].Count != 1 {
		t.Fatalf("inventory = %+v", inventory)
	}
	err = owner.MutateOwnedInventory(ctx, OwnedInventoryMutationCommand{
		AccountID:   "account-2",
		CharacterID: "19",
		Apply: func(map[string]dnfrepo.ItemStack) (bool, error) {
			return true, nil
		},
	})
	if !errors.Is(err, ErrCharacterNotFound) {
		t.Fatalf("wrong account error = %v, want %v", err, ErrCharacterNotFound)
	}
}

func TestOwnerCompleteTutorialPersistsNextLoginState(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "19",
		Stats: map[string]int64{
			"tutorial_completed": 0,
			"town_id":            99,
		},
	}); err != nil {
		t.Fatalf("seed character: %v", err)
	}
	owner, _ := NewOwner(repos)
	command := TutorialCompletionCommand{
		CharacterID:  "19",
		CompletedKey: "tutorial_completed",
		Completed:    1,
		NextLogin: map[string]int64{
			"town_id": 1,
			"area_id": 2,
		},
	}
	result, err := owner.CompleteTutorial(ctx, command)
	if err != nil {
		t.Fatalf("CompleteTutorial: %v", err)
	}
	if result.Previous != 0 || !result.Changed {
		t.Fatalf("result = %+v", result)
	}
	replay, err := owner.CompleteTutorial(ctx, command)
	if err != nil {
		t.Fatalf("replay CompleteTutorial: %v", err)
	}
	if replay.Previous != 1 || replay.Changed {
		t.Fatalf("replay = %+v", replay)
	}
	character, _, err := repos.Character.Load(ctx, "19")
	if err != nil {
		t.Fatalf("load character: %v", err)
	}
	if character.Stats["tutorial_completed"] != 1 ||
		character.Stats["town_id"] != 1 ||
		character.Stats["area_id"] != 2 {
		t.Fatalf("character = %+v", character)
	}
}

func TestOwnerAwardLuckyStarOwnsSuitabilityAndCap(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Account.Save(ctx, dnfrepo.AccountRecord{
		AccountID: "account-1",
		Metadata:  map[string]string{LuckyStarMetadataKey: "998"},
	}); err != nil {
		t.Fatalf("seed account: %v", err)
	}
	owner, _ := NewOwner(repos)
	result, err := owner.AwardLuckyStar(ctx, LuckyStarCommand{
		AccountID:      "account-1",
		CharacterLevel: 50,
		RecommendedMin: 45,
		RecommendedMax: 55,
	})
	if err != nil {
		t.Fatalf("AwardLuckyStar: %v", err)
	}
	if !result.Awarded || result.Before != 998 || result.After != 999 {
		t.Fatalf("result = %+v", result)
	}
	capped, err := owner.AwardLuckyStar(ctx, LuckyStarCommand{
		AccountID:      "account-1",
		CharacterLevel: 50,
		RecommendedMin: 45,
		RecommendedMax: 55,
	})
	if err != nil {
		t.Fatalf("capped AwardLuckyStar: %v", err)
	}
	if capped.Awarded || capped.Before != 999 || capped.After != 999 {
		t.Fatalf("capped = %+v", capped)
	}
	outside, err := owner.AwardLuckyStar(ctx, LuckyStarCommand{
		AccountID:      "account-1",
		CharacterLevel: 60,
		RecommendedMin: 45,
		RecommendedMax: 55,
	})
	if err != nil || outside.Awarded {
		t.Fatalf("outside = %+v, err=%v", outside, err)
	}
}

func TestOwnerCommitSettlementAdvancesAndReplaysReceipt(t *testing.T) {
	ctx := context.Background()
	tables, err := progression.Load(ctx, settlementTestSource())
	if err != nil {
		t.Fatalf("load tables: %v", err)
	}
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "account-1",
		Level:       1,
		Stats:       map[string]int64{"exp": 90},
	}); err != nil {
		t.Fatalf("seed character: %v", err)
	}
	if err := repos.Skill.Save(ctx, dnfrepo.SkillRecord{
		CharacterID: "19",
		Points: dnfrepo.SkillPointState{
			TotalSP: 10, RemainingSP: 10, SyncedLevel: 1,
		},
	}); err != nil {
		t.Fatalf("seed skill: %v", err)
	}
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots:       map[string]dnfrepo.ItemStack{},
	}); err != nil {
		t.Fatalf("seed inventory: %v", err)
	}
	owner, _ := NewOwner(repos)
	result, err := owner.CommitSettlement(ctx, SettlementCommand{
		AccountID:               "account-1",
		CharacterID:             "19",
		CompletionKey:           "run-1",
		Tables:                  tables,
		Experience:              20,
		RecommendedDungeonClear: true,
	})
	if err != nil {
		t.Fatalf("CommitSettlement: %v", err)
	}
	if result.Replayed || result.Character.Level != 2 ||
		result.Character.Stats["exp"] != 110 ||
		result.Character.Stats[adventuregroup.RecommendedDungeonClearStatKey] != 1 ||
		result.ExperienceGain != 20 || result.SPGain != 30 ||
		result.Skill.Points.SyncedLevel != 2 {
		t.Fatalf("result = %+v", result)
	}
	replay, err := owner.CommitSettlement(ctx, SettlementCommand{
		AccountID:               "account-1",
		CharacterID:             "19",
		CompletionKey:           "run-1",
		Tables:                  tables,
		Experience:              999,
		RecommendedDungeonClear: true,
	})
	if err != nil {
		t.Fatalf("replay CommitSettlement: %v", err)
	}
	if !replay.Replayed || replay.ExperienceGain != 20 || replay.SPGain != 30 ||
		replay.Character.Level != 2 || replay.Character.Stats["exp"] != 110 ||
		replay.Character.Stats[adventuregroup.RecommendedDungeonClearStatKey] != 1 {
		t.Fatalf("replay = %+v", replay)
	}
}

func TestOwnerCommitSettlementAccumulatesMaximumLevelAdventureExperienceOnce(t *testing.T) {
	ctx := context.Background()
	tables, err := progression.Load(ctx, settlementTestSource())
	if err != nil {
		t.Fatal(err)
	}
	honorTables, err := dnfhonor.LoadTables(settlementMemorySource{dnfhonor.TablePath: settlementHonorExpertTestDocument()})
	if err != nil {
		t.Fatal(err)
	}
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Account.Save(ctx, dnfrepo.AccountRecord{
		AccountID: "account-1",
		Metadata:  map[string]string{},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "19",
		AccountID:   "account-1",
		Level:       5,
		Stats:       map[string]int64{"exp": 900},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repos.Skill.Save(ctx, dnfrepo.SkillRecord{
		CharacterID: "19",
		Points: dnfrepo.SkillPointState{
			TotalSP: 160, RemainingSP: 160, TotalTP: 4, RemainingTP: 4, SyncedLevel: 5,
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repos.Inventory.Save(ctx, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots:       map[string]dnfrepo.ItemStack{},
	}); err != nil {
		t.Fatal(err)
	}
	config := adventuregroup.RuntimeConfig{
		ShopCategories: []adventuregroup.ShopCategory{
			{
				Index:              adventuregroup.ShopPointBrave,
				MaxPoint:           250,
				ExperiencePerPoint: 10,
				PointPerExperience: 1,
			},
			{Index: adventuregroup.ShopPointGlory, MaxPoint: 9999},
			{Index: adventuregroup.ShopPointPure},
		},
		Capsule: adventuregroup.CapsuleConfig{
			MinimumExperience: 10,
			MaximumExperience: 100,
			MaximumCount:      10,
			MinimumLevel:      1,
			MaximumLevel:      4,
			GrantedExperience: 1,
		},
	}
	owner, _ := NewOwner(repos)
	command := SettlementCommand{
		AccountID:             "account-1",
		CharacterID:           "19",
		CompletionKey:         "max-level-run",
		Tables:                tables,
		Experience:            25,
		AdventureRuntime:      &config,
		HonorExpertTables:     honorTables,
		MaximumCharacterLevel: 5,
	}
	result, err := owner.CommitSettlement(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	if result.Character.Level != 5 || result.Character.Stats["exp"] != 925 ||
		result.HonorExpertGain != 25 ||
		result.Character.Stats[dnfhonor.ExpertLevelStatKey] != 1 ||
		result.Character.Stats[dnfhonor.ExpertProgressExperienceStatKey] != 15 {
		t.Fatalf("result=%+v", result)
	}
	account, found, err := repos.Account.Load(ctx, "account-1")
	if err != nil || !found {
		t.Fatalf("load account found=%v err=%v", found, err)
	}
	runtime, err := adventuregroup.ParseRuntimeState(account, config, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if account.HonorExp != 25 || runtime.ShopPoints[adventuregroup.ShopPointBrave] != 2 ||
		runtime.BraveExperience != 5 || runtime.GrowthExperience != 25 {
		t.Fatalf("account=%+v runtime=%+v", account, runtime)
	}
	command.Experience = 99
	replay, err := owner.CommitSettlement(ctx, command)
	if err != nil || !replay.Replayed {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	account, _, _ = repos.Account.Load(ctx, "account-1")
	runtime, _ = adventuregroup.ParseRuntimeState(account, config, time.Now())
	if account.HonorExp != 25 || runtime.GrowthExperience != 25 ||
		replay.HonorExpertGain != 0 || replay.Character.Stats[dnfhonor.ExpertLevelStatKey] != 1 ||
		replay.Character.Stats[dnfhonor.ExpertProgressExperienceStatKey] != 15 {
		t.Fatalf("replay duplicated account=%+v runtime=%+v", account, runtime)
	}
}

func settlementHonorExpertTestDocument() string {
	return `[grade]
1 ` + "`effect.ani` `medal.img` 0 `icon.img` 0" + ` 1 0
[/grade]
[maxexp on maxlevel]
1
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
` + "`expert-medal.img`" + ` 0
[icon img]
` + "`expert-icon.img`" + ` 0
[min lv]
1
[max lv]
-1
[/grade info]
[/expert info]
[honor expert exp table]
1 10 2 20
[/honor expert exp table]
`
}

func seedDungeonOwnerCharacter(t *testing.T, ctx context.Context, repos dnfrepo.Group, gold int64) {
	t.Helper()
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "19",
		Stats:       map[string]int64{"gold": gold},
	}); err != nil {
		t.Fatalf("seed character: %v", err)
	}
}

type settlementMemorySource map[string]string

func (s settlementMemorySource) ReadText(path string) (string, error) {
	value, ok := s[path]
	if !ok {
		return "", fmt.Errorf("missing test PVF path %s", path)
	}
	return value, nil
}

func settlementTestSource() settlementMemorySource {
	return settlementMemorySource{
		progression.ExperienceTablePath: "100 250 500 900\n",
		progression.SkillPointTablePath: "[sp table]\n1 10\n2 30\n3 30\n4 40\n5 50\n[/sp table]\n[tp table]\n3 2\n4 1\n5 1\n[/tp table]\n",
		progression.QuestParameterPath: `[difficulty]
` + "`N` 100\n`E` 200\n" + `[/difficulty]
[exp reward table]
100 -1
200 -1
300 -1
400 -1
500 -1
[gold reward table]
10 -1
[green level penalty]
80
[grey level penalty]
30
`,
	}
}
