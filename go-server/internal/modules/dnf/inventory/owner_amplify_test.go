package inventory

import (
	"context"
	"testing"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func staticAmplifyResolver(resolution alignedcmd.AmplifyItemResolution) alignedcmd.AmplifyItemResolver {
	return func(materialItemID int64, targetItemID int64) (alignedcmd.AmplifyItemResolution, error) {
		return resolution, nil
	}
}

func validAmplifyResolution() alignedcmd.AmplifyItemResolution {
	return alignedcmd.AmplifyItemResolution{
		TargetKind:         "equipment",
		TargetPVFPath:      "equipment/test.equ",
		TargetMinimumLevel: 90,
		TargetRarity:       4,
		EquipLevelConst:    55,
		InitialValues: map[byte]uint16{
			1: 7,
			2: 7,
			3: 7,
			4: 7,
		},
		MaterialPVFPath: "stackable/material.stk",
	}
}

func saveAmplifyFixture(t *testing.T, ctx context.Context, repos dnfrepo.Group, amplifyType byte, amplifyValue uint16, upgradeLevel byte, materialItemID int64, materialCount int64) {
	t.Helper()
	targetRaw := make([]byte, currentItemListEntrySize)
	targetRaw[0x0A] = upgradeLevel
	targetRaw[0x13] = amplifyType
	targetRaw[0x14] = byte(amplifyValue)
	targetRaw[0x15] = byte(amplifyValue >> 8)
	materialRaw := make([]byte, currentItemListEntrySize)
	materialRaw[0x06] = byte(materialCount)
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 9): {
				ItemID:   700,
				Count:    1,
				RawEntry: targetRaw,
			},
			slotKey(listTypeMain, 12): {
				ItemID:   materialItemID,
				Count:    materialCount,
				RawEntry: materialRaw,
			},
		},
	})
}

func purifyAmplifyCommand(materialItemID int32) Command {
	return NewPurifyItemCommand(alignedcmd.Request{SelectedCharacterID: 77}, PurifyItemRequest{
		TargetSlotIndex:        9,
		TargetItemTemplateID:   700,
		MaterialSlotIndex:      12,
		MaterialItemTemplateID: materialItemID,
	})
}

func investAmplifyCommand(action byte, selected byte, materialItemID int32) Command {
	return NewInvestItemAmplifyOptionCommand(alignedcmd.Request{SelectedCharacterID: 77}, InvestItemAmplifyOptionRequest{
		Action:                 action,
		TargetSlotIndex:        9,
		TargetItemTemplateID:   700,
		MaterialSlotIndex:      12,
		MaterialItemTemplateID: materialItemID,
		SelectedOption:         selected,
		TargetItemName:         "target",
	})
}

func TestOwnerPurifyUsesRawUnidentifiedFlagAndConsumesConfiguredCount(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveAmplifyFixture(t, ctx, repos, unidentifiedAmplifyFlag, 0, 0, 1183, 3)
	resolution := validAmplifyResolution()
	resolution.PurifyMaterialCount = 2
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	result, err := owner.PurifyAmplifyItem(ctx, purifyAmplifyCommand(1183), staticAmplifyResolver(resolution))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success || !result.Changed || result.Mode != "purify" || result.AmplifyType < 1 || result.AmplifyType > 4 || result.AmplifyValue != 7 || result.MaterialRemainingCount != 1 {
		t.Fatalf("result = %+v", result)
	}
	loaded := loadTestInventory(t, ctx, repos, "77")
	target := loaded.Slots[slotKey(listTypeMain, 9)]
	if target.RawEntry[0x13] != result.AmplifyType || target.RawEntry[0x14] != 7 || target.RawEntry[0x15] != 0 {
		t.Fatalf("target raw amplification = %x", target.RawEntry[0x13:0x16])
	}
	if target.Extra["amplify_type"] != target.Extra["byte_13"] || target.Extra["amplify_value"] != "7" || target.Extra["marker_16"] != "7" {
		t.Fatalf("target extra = %+v", target.Extra)
	}
	material := loaded.Slots[slotKey(listTypeMain, 12)]
	if material.Count != 1 || material.RawEntry[0x06] != 1 {
		t.Fatalf("material = %+v rawAmount=%d", material, material.RawEntry[0x06])
	}
}

func TestOwnerClearUnidentifiedAmplification(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveAmplifyFixture(t, ctx, repos, unidentifiedAmplifyFlag|3, 9, 0, 10000408, 1)
	resolution := validAmplifyResolution()
	resolution.ClearMaterialCount = 1
	owner, _ := NewOwner(repos)
	result, err := owner.PurifyAmplifyItem(ctx, purifyAmplifyCommand(10000408), staticAmplifyResolver(resolution))
	if err != nil || !result.Success || result.Mode != "clear" || result.AmplifyType != 0 || result.AmplifyValue != 0 {
		t.Fatalf("result = %+v err=%v", result, err)
	}
	loaded := loadTestInventory(t, ctx, repos, "77")
	if _, found := loaded.Slots[slotKey(listTypeMain, 12)]; found {
		t.Fatal("exhausted clear material was not deleted")
	}
	if got := loaded.Slots[slotKey(listTypeMain, 9)].RawEntry[0x13:0x16]; got[0] != 0 || got[1] != 0 || got[2] != 0 {
		t.Fatalf("cleared raw amplification = %x", got)
	}
}

func TestOwnerInvestAndPureGoldUsePVFResolvedOptions(t *testing.T) {
	ctx := context.Background()
	t.Run("invest all uses selected option", func(t *testing.T) {
		repos := dnfrepomemory.NewMemoryGroup()
		saveAmplifyFixture(t, ctx, repos, 0, 0, 0, 1286, 1)
		resolution := validAmplifyResolution()
		resolution.InvestOption = amplifyOptionAll
		resolution.InvestMaterialCount = 1
		owner, _ := NewOwner(repos)
		result, err := owner.InvestAmplifyOption(ctx, investAmplifyCommand(investAmplifyActionInvest, 3, 1286), staticAmplifyResolver(resolution))
		if err != nil || !result.Success || result.AmplifyType != 3 || result.AmplifyValue != 7 || result.AmplifyLevel != 0 {
			t.Fatalf("result = %+v err=%v", result, err)
		}
	})

	t.Run("Pure Gold uses material random table", func(t *testing.T) {
		repos := dnfrepomemory.NewMemoryGroup()
		saveAmplifyFixture(t, ctx, repos, 1, 7, 5, 8238, 1)
		resolution := validAmplifyResolution()
		resolution.PureGoldOption = amplifyOptionAll
		resolution.PureGoldMaterialCount = 1
		resolution.PureGoldLevels = []alignedcmd.AmplifyWeightedLevel{{Level: 9, Weight: 1}}
		owner, _ := NewOwner(repos)
		result, err := owner.InvestAmplifyOption(ctx, investAmplifyCommand(investAmplifyActionPureGold, 4, 8238), staticAmplifyResolver(resolution))
		if err != nil || !result.Success || result.AmplifyType != 4 || result.AmplifyValue != 7 || result.AmplifyLevel != 9 {
			t.Fatalf("result = %+v err=%v", result, err)
		}
		loaded := loadTestInventory(t, ctx, repos, "77")
		target := loaded.Slots[slotKey(listTypeMain, 9)]
		if target.RawEntry[0x0A]&0x1F != 9 {
			t.Fatalf("target upgrade raw = %d, want 9", target.RawEntry[0x0A]&0x1F)
		}
	})
}

func TestOwnerAmplifyValidationDoesNotConsumeMaterial(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveAmplifyFixture(t, ctx, repos, 3, 7, 0, 1286, 2)
	resolution := validAmplifyResolution()
	resolution.InvestOption = amplifyOptionAll
	resolution.InvestMaterialCount = 1
	owner, _ := NewOwner(repos)
	result, err := owner.InvestAmplifyOption(ctx, investAmplifyCommand(investAmplifyActionInvest, 3, 1286), staticAmplifyResolver(resolution))
	if err != nil {
		t.Fatal(err)
	}
	if result.Success || result.ErrorCode != investAmplifyErrorAlreadyHasOption {
		t.Fatalf("result = %+v", result)
	}
	loaded := loadTestInventory(t, ctx, repos, "77")
	if loaded.Slots[slotKey(listTypeMain, 12)].Count != 2 || loaded.Slots[slotKey(listTypeMain, 9)].RawEntry[0x13] != 3 {
		t.Fatalf("validation mutated inventory: %+v", loaded.Slots)
	}
}
