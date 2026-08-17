package inventory

import (
	"bytes"
	"context"
	"testing"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func testRandomOptionStack(itemID int64, raw []byte) dnfrepo.ItemStack {
	return dnfrepo.ItemStack{ItemID: itemID, Count: 1, RawEntry: append([]byte(nil), raw...)}
}

func testRandomOptionResolution() alignedcmd.RandomOptionResolution {
	return alignedcmd.RandomOptionResolution{
		TargetKind:         "equipment",
		TargetPVFPath:      "equipment/character/common/amulet/test.equ",
		TargetEquipmentKey: "amulet",
		TargetMinimumLevel: 80,
		TargetRarity:       2,
		Eligible:           true,
		QuantityWeights:    []alignedcmd.RandomOptionWeightedQuantity{{Quantity: 3, Weight: 1}},
		InitialGroups: [][]alignedcmd.RandomOptionCandidate{
			{{Type: 11, Value1: 21, Value2: 31, Weight: 1}},
			{{Type: 12, Value1: 22, Value2: 32, Weight: 1}},
			{{Type: 13, Value1: 23, Value2: 33, Weight: 1}},
		},
		ModifiedGroups: [][]alignedcmd.RandomOptionCandidate{
			{{Type: 21, Value1: 41, Value2: 51, Weight: 1}},
			{{Type: 22, Value1: 42, Value2: 52, Weight: 1}},
			{{Type: 23, Value1: 43, Value2: 53, Weight: 1}},
		},
		BreakSealGoldCost:    100,
		ModificationGoldCost: 25,
	}
}

func staticRandomOptionResolver(resolution alignedcmd.RandomOptionResolution) alignedcmd.RandomOptionResolver {
	return func(targetItemID int64) (alignedcmd.RandomOptionResolution, error) {
		return resolution, nil
	}
}

func saveRandomOptionFixture(t *testing.T, ctx context.Context, repos dnfrepo.Group, gold int64, raw []byte, extra map[string]string) {
	t.Helper()
	if raw == nil {
		raw = make([]byte, currentItemListEntrySize)
	}
	stack := testRandomOptionStack(700, raw)
	stack.Extra = extra
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 9): stack,
		},
	})
	saveTestCharacter(t, ctx, repos, dnfrepo.CharacterRecord{CharacterID: "77", Stats: map[string]int64{"gold": gold}})
}

func unsealRandomOptionCommand() Command {
	return NewUnsealRandomOptionCommand(alignedcmd.Request{SelectedCharacterID: 77}, UnsealRandomOptionRequest{TargetSlotIndex: 9, InventoryManagerState: 0xFFFF})
}

func changeRandomOptionCommand(index byte) Command {
	return NewChangeRandomOptionCommand(alignedcmd.Request{SelectedCharacterID: 77}, ChangeRandomOptionRequest{TargetSlotIndex: 9, OptionIndex: index})
}

func TestOwnerUnsealRandomOptionMutatesOnlySealFieldsAndGold(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	raw := currentRawEntryForStack(9, testRandomOptionStack(700, bytes.Repeat([]byte{0xA5}, currentItemListEntrySize)))
	for index := randomOptionCountOffset; index <= randomOptionCandidateOffset+3; index++ {
		raw[index] = 0
	}
	saveRandomOptionFixture(t, ctx, repos, 1000, raw, nil)
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	result, err := owner.UnsealRandomOption(ctx, unsealRandomOptionCommand(), staticRandomOptionResolver(testRandomOptionResolution()))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success || !result.Changed || result.GoldCost != 100 || result.UpdatedGold != 900 || len(result.Options) != 3 {
		t.Fatalf("result = %+v", result)
	}
	loaded := loadTestInventory(t, ctx, repos, "77")
	got := loaded.Slots[slotKey(listTypeMain, 9)]
	wantWindow := []byte{3, 11, 12, 13, 21, 22, 23, 31, 32, 33, 0, 0xFF, 0, 0, 0, 0}
	if !bytes.Equal(got.RawEntry[randomOptionCountOffset:randomOptionCandidateOffset+4], wantWindow) {
		t.Fatalf("random-option raw = % X, want % X", got.RawEntry[randomOptionCountOffset:randomOptionCandidateOffset+4], wantWindow)
	}
	for index := range raw {
		if index >= randomOptionCountOffset && index <= randomOptionCandidateOffset+3 {
			continue
		}
		if got.RawEntry[index] != raw[index] {
			t.Fatalf("unrelated raw[%02X] = %02X, want %02X", index, got.RawEntry[index], raw[index])
		}
	}
	character, found, err := repos.Character.Load(ctx, "77")
	if err != nil || !found || character.Stats["gold"] != 900 {
		t.Fatalf("character = %+v found=%t err=%v", character, found, err)
	}

	beforeRepeat := append([]byte(nil), got.RawEntry...)
	repeat, err := owner.UnsealRandomOption(ctx, unsealRandomOptionCommand(), staticRandomOptionResolver(testRandomOptionResolution()))
	if err != nil || repeat.Success || repeat.Changed {
		t.Fatalf("repeat result = %+v err=%v", repeat, err)
	}
	loaded = loadTestInventory(t, ctx, repos, "77")
	if !bytes.Equal(loaded.Slots[slotKey(listTypeMain, 9)].RawEntry, beforeRepeat) {
		t.Fatal("repeat unseal mutated target")
	}
	character, _, _ = repos.Character.Load(ctx, "77")
	if character.Stats["gold"] != 900 {
		t.Fatalf("repeat gold = %d, want 900", character.Stats["gold"])
	}
}

func TestOwnerChangeRandomOptionChangesOnlySelectedEntry(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	raw := make([]byte, currentItemListEntrySize)
	raw[randomOptionCountOffset] = 3
	copy(raw[randomOptionTypeOffset:], []byte{11, 12, 13})
	copy(raw[randomOptionValue1Offset:], []byte{21, 22, 23})
	copy(raw[randomOptionValue2Offset:], []byte{31, 32, 33})
	raw[randomOptionStateOffset] = 7
	raw[randomOptionChangedIndexOffset] = 1
	copy(raw[randomOptionCandidateOffset:], []byte{9, 8, 7, 6})
	saveRandomOptionFixture(t, ctx, repos, 1000, raw, nil)
	owner, _ := NewOwner(repos)
	result, err := owner.ChangeRandomOption(ctx, changeRandomOptionCommand(1), staticRandomOptionResolver(testRandomOptionResolution()))
	if err != nil || !result.Success || result.UpdatedGold != 975 {
		t.Fatalf("result = %+v err=%v", result, err)
	}
	loaded := loadTestInventory(t, ctx, repos, "77")
	got := loaded.Slots[slotKey(listTypeMain, 9)].RawEntry
	if got[randomOptionCountOffset] != 3 || !bytes.Equal(got[randomOptionTypeOffset:randomOptionTypeOffset+3], []byte{11, 22, 13}) ||
		!bytes.Equal(got[randomOptionValue1Offset:randomOptionValue1Offset+3], []byte{21, 42, 23}) ||
		!bytes.Equal(got[randomOptionValue2Offset:randomOptionValue2Offset+3], []byte{31, 52, 33}) {
		t.Fatalf("changed options = % X", got[randomOptionCountOffset:randomOptionCandidateOffset+4])
	}
	if got[randomOptionStateOffset] != 0 || got[randomOptionChangedIndexOffset] != 0xFF || !bytes.Equal(got[randomOptionCandidateOffset:randomOptionCandidateOffset+4], make([]byte, 4)) {
		t.Fatalf("transient fields = % X", got[randomOptionStateOffset:randomOptionCandidateOffset+4])
	}
}

func TestOwnerRandomOptionValidationIsAtomic(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name  string
		gold  int64
		extra map[string]string
	}{
		{name: "insufficient gold", gold: 99},
		{name: "locked target", gold: 1000, extra: map[string]string{"equipment_lock_state": "locked"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repos := dnfrepomemory.NewMemoryGroup()
			raw := make([]byte, currentItemListEntrySize)
			raw[0x60] = 0x7B
			saveRandomOptionFixture(t, ctx, repos, test.gold, raw, test.extra)
			owner, _ := NewOwner(repos)
			result, err := owner.UnsealRandomOption(ctx, unsealRandomOptionCommand(), staticRandomOptionResolver(testRandomOptionResolution()))
			if err != nil || result.Success || result.Changed {
				t.Fatalf("result = %+v err=%v", result, err)
			}
			loaded := loadTestInventory(t, ctx, repos, "77")
			if !bytes.Equal(loaded.Slots[slotKey(listTypeMain, 9)].RawEntry, raw) {
				t.Fatal("rejected mutation changed target")
			}
			character, _, _ := repos.Character.Load(ctx, "77")
			if character.Stats["gold"] != test.gold {
				t.Fatalf("gold = %d, want %d", character.Stats["gold"], test.gold)
			}
		})
	}
}
