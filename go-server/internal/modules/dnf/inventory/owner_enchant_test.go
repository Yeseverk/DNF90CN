package inventory

import (
	"context"
	"errors"
	"testing"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func staticEnchantResolver(resolution alignedcmd.EnchantBeadResolution) alignedcmd.EnchantBeadResolver {
	return func(beadItemID int64, targetItemID int64) (alignedcmd.EnchantBeadResolution, error) {
		return resolution, nil
	}
}

func coatEnchantResolver(cardItemID int64, counts []int64) alignedcmd.EnchantBeadResolver {
	return staticEnchantResolver(alignedcmd.EnchantBeadResolution{
		CardItemID:            cardItemID,
		AllowedEquipmentTypes: []string{"[coat]"},
		UpgradeCounts:         counts,
		TargetEquipmentType:   "[coat]",
		TargetKind:            "equipment",
	})
}

func creatureEnchantResolver(cardItemID int64, targetItemID int64) alignedcmd.EnchantBeadResolver {
	return staticEnchantResolver(alignedcmd.EnchantBeadResolution{
		CardItemID:            cardItemID,
		TargetWhitelist:       []int64{targetItemID},
		AllowedEquipmentTypes: []string{"[creature]"},
		TargetEquipmentType:   "[creature]",
		TargetKind:            "equipment",
	})
}

func newEnchantTestCommand(beadSlot int16, targetSlot int16) Command {
	return NewEnchantCommand(alignedcmd.Request{
		AccountID:           "acc",
		SelectedCharacterID: 77,
	}, EnchantByBeadRequest{
		BeadListType:    listTypeMain,
		BeadSlotIndex:   beadSlot,
		TargetListType:  listTypeMain,
		TargetSlotIndex: targetSlot,
	})
}

func saveEnchantFixture(t *testing.T, ctx context.Context, repos dnfrepo.Group, beadCount int64, beadRaw byte, targetCardID int64) {
	t.Helper()
	beadRawEntry := make([]byte, currentItemListEntrySize)
	beadRawEntry[0x12] = beadRaw
	targetRawEntry := make([]byte, currentItemListEntrySize)
	targetRawEntry[0x0E] = byte(targetCardID)
	targetRawEntry[0x0F] = byte(targetCardID >> 8)
	targetRawEntry[0x10] = byte(targetCardID >> 16)
	targetRawEntry[0x11] = byte(targetCardID >> 24)
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 12): {
				ItemID:   8801,
				Count:    beadCount,
				RawEntry: beadRawEntry,
				Extra: map[string]string{
					"item_kind": "stackable",
					"pvf_path":  "stackable/bead/fire_bead.stk",
				},
			},
			slotKey(listTypeMain, 30): {
				ItemID:   700,
				Count:    1,
				RawEntry: targetRawEntry,
				Extra: map[string]string{
					"item_kind": "equipment",
				},
			},
		},
	})
}

func savePetEnchantFixture(t *testing.T, ctx context.Context, repos dnfrepo.Group, beadCount int64) {
	t.Helper()
	const (
		petItemID int64 = 400990168
		petSlot   int16 = 24
	)
	petRaw := make([]byte, currentItemListEntrySize)
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 12): {
				ItemID: 490007163,
				Count:  beadCount,
				Extra: map[string]string{
					"item_kind": "stackable",
				},
			},
			slotKey(listTypePet, petSlot): {
				ItemID:   petItemID,
				Count:    1,
				RawEntry: petRaw,
				Extra: map[string]string{
					"creature_serial_or_handle": "37",
					"creature_key":              "37",
					"item_kind":                 "equipment",
					"equipment_type":            "[creature]",
				},
			},
		},
	})
	if err := repos.Pet.Save(ctx, dnfrepo.PetRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.PetEntry{
			"37": {
				PetKey:          "37",
				CreatureKey:     37,
				ItemID:          petItemID,
				SourceListType:  listTypePet,
				SourceSlotIndex: petSlot,
				Level:           1,
				Satiety:         100,
				RawEntry:        make([]byte, currentItemListEntrySize),
			},
		},
	}); err != nil {
		t.Fatalf("Save pet error = %v", err)
	}
}

func TestOwnerEnchantAppliesCardAndConsumesBead(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveEnchantFixture(t, ctx, repos, 2, 0, 0)
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	result, err := owner.Enchant(ctx, newEnchantTestCommand(12, 30), coatEnchantResolver(9001, nil))
	if err != nil {
		t.Fatalf("Enchant error = %v", err)
	}
	if !result.Success || !result.Changed || result.CardItemID != 9001 || result.EnchantUpgradeCount != 0 || result.BeadRemainingCount != 1 {
		t.Fatalf("result = %+v", result)
	}
	loaded := loadTestInventory(t, ctx, repos, "77")
	target := loaded.Slots[slotKey(listTypeMain, 30)]
	if got := target.Extra["value_a"]; got != "9001" {
		t.Fatalf("target value_a = %q, want 9001", got)
	}
	if got := target.Extra["byte_12"]; got != "0" {
		t.Fatalf("target byte_12 = %q, want 0", got)
	}
	if got := target.RawEntry[0x0E:0x12]; got[0] != 0x29 || got[1] != 0x23 || got[2] != 0 || got[3] != 0 {
		t.Fatalf("target raw card id = %x, want 9001 LE", got)
	}
	if got := target.RawEntry[0x12]; got != 0 {
		t.Fatalf("target raw upgrade byte = %d, want 0", got)
	}
	bead := loaded.Slots[slotKey(listTypeMain, 12)]
	if bead.Count != 1 {
		t.Fatalf("bead count = %d, want 1", bead.Count)
	}
	if got := bead.RawEntry[0x06]; got != 1 {
		t.Fatalf("bead raw amount = %d, want 1", got)
	}
}

func TestOwnerEnchantDeletesExhaustedBeadAndOverwritesCard(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveEnchantFixture(t, ctx, repos, 1, 0, 1234)
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	result, err := owner.Enchant(ctx, newEnchantTestCommand(12, 30), coatEnchantResolver(9001, nil))
	if err != nil {
		t.Fatalf("Enchant error = %v", err)
	}
	if !result.Success || result.BeadRemainingCount != 0 {
		t.Fatalf("result = %+v", result)
	}
	loaded := loadTestInventory(t, ctx, repos, "77")
	if _, ok := loaded.Slots[slotKey(listTypeMain, 12)]; ok {
		t.Fatalf("exhausted bead slot should be deleted")
	}
	target := loaded.Slots[slotKey(listTypeMain, 30)]
	if got := target.Extra["value_a"]; got != "9001" {
		t.Fatalf("overwritten target value_a = %q, want 9001", got)
	}
}

func TestOwnerEnchantPetCreatureUpdatesInventoryAndTypedPetAtomically(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	savePetEnchantFixture(t, ctx, repos, 1)
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	cmd := newEnchantTestCommand(12, 24)
	cmd.TargetListType = listTypePet
	result, err := owner.Enchant(ctx, cmd, creatureEnchantResolver(10008663, 400990168))
	if err != nil {
		t.Fatalf("Enchant error = %v", err)
	}
	if !result.Success || !result.Changed || result.TargetListType != listTypePet ||
		result.CardItemID != 10008663 || result.BeadRemainingCount != 0 {
		t.Fatalf("result = %+v", result)
	}
	inventory := loadTestInventory(t, ctx, repos, "77")
	if _, found := inventory.Slots[slotKey(listTypeMain, 12)]; found {
		t.Fatal("exhausted pet bead was not removed")
	}
	target := inventory.Slots[slotKey(listTypePet, 24)]
	if target.Extra["value_a"] != "10008663" || target.Extra["byte_12"] != "0" {
		t.Fatalf("pet target extra = %+v", target.Extra)
	}
	if got := int64(uint32(target.RawEntry[0x0E]) | uint32(target.RawEntry[0x0F])<<8 | uint32(target.RawEntry[0x10])<<16 | uint32(target.RawEntry[0x11])<<24); got != 10008663 {
		t.Fatalf("pet target raw card = %d, want 10008663", got)
	}
	petRecord, found, err := repos.Pet.Load(ctx, "77")
	if err != nil || !found {
		t.Fatalf("Load pet found=%t err=%v", found, err)
	}
	pet := petRecord.Entries["37"]
	if pet.Extra["pet_enchant_card_item_id"] != "10008663" || pet.Extra["pet_enchant_upgrade_count"] != "0" {
		t.Fatalf("typed pet extra = %+v", pet.Extra)
	}
	if got := int64(uint32(pet.RawEntry[0x0E]) | uint32(pet.RawEntry[0x0F])<<8 | uint32(pet.RawEntry[0x10])<<16 | uint32(pet.RawEntry[0x11])<<24); got != 10008663 {
		t.Fatalf("typed pet raw card = %d, want 10008663", got)
	}
}

func TestOwnerEnchantPetCreatureRejectsPVFWhitelistMismatchWithoutMutation(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	savePetEnchantFixture(t, ctx, repos, 1)
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	cmd := newEnchantTestCommand(12, 24)
	cmd.TargetListType = listTypePet
	result, err := owner.Enchant(ctx, cmd, creatureEnchantResolver(10008663, 400990006))
	if err != nil {
		t.Fatalf("Enchant error = %v", err)
	}
	if result.Success || result.Changed || result.ErrorCode != enchantErrorInvalidTarget {
		t.Fatalf("result = %+v, want PVF whitelist rejection", result)
	}
	inventory := loadTestInventory(t, ctx, repos, "77")
	if got := inventory.Slots[slotKey(listTypeMain, 12)].Count; got != 1 {
		t.Fatalf("bead count mutated on whitelist rejection: %d", got)
	}
	if got := inventory.Slots[slotKey(listTypePet, 24)].Extra["value_a"]; got != "" {
		t.Fatalf("pet target mutated on whitelist rejection: value_a=%q", got)
	}
}

func TestOwnerEnchantUpgradeCountTable(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveEnchantFixture(t, ctx, repos, 2, 2, 0)
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	result, err := owner.Enchant(ctx, newEnchantTestCommand(12, 30), coatEnchantResolver(9001, []int64{0, 1, 2}))
	if err != nil {
		t.Fatalf("Enchant error = %v", err)
	}
	if !result.Success || result.EnchantUpgradeCount != 2 {
		t.Fatalf("result = %+v", result)
	}
	loaded := loadTestInventory(t, ctx, repos, "77")
	target := loaded.Slots[slotKey(listTypeMain, 30)]
	if got := target.RawEntry[0x12]; got != 2 {
		t.Fatalf("target raw upgrade byte = %d, want 2", got)
	}

	rejections := []struct {
		name     string
		resolver alignedcmd.EnchantBeadResolver
	}{
		{name: "count outside table", resolver: coatEnchantResolver(9001, []int64{0, 1})},
		{name: "nonzero count without table", resolver: coatEnchantResolver(9001, nil)},
	}
	for _, tc := range rejections {
		t.Run(tc.name, func(t *testing.T) {
			fresh := dnfrepomemory.NewMemoryGroup()
			saveEnchantFixture(t, ctx, fresh, 2, 2, 0)
			freshOwner, err := NewOwner(fresh)
			if err != nil {
				t.Fatal(err)
			}
			result, err := freshOwner.Enchant(ctx, newEnchantTestCommand(12, 30), tc.resolver)
			if err != nil {
				t.Fatalf("Enchant error = %v", err)
			}
			if result.Success || result.ErrorCode != enchantErrorUnsupported {
				t.Fatalf("result = %+v, want unsupported 0x17", result)
			}
			loaded := loadTestInventory(t, ctx, fresh, "77")
			if got := loaded.Slots[slotKey(listTypeMain, 12)].Count; got != 2 {
				t.Fatalf("bead count mutated on rejection: %d", got)
			}
		})
	}
}

func TestOwnerEnchantRejections(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name      string
		mutateCmd func(*Command)
		resolver  alignedcmd.EnchantBeadResolver
		setup     func(t *testing.T, repos dnfrepo.Group)
		wantCode  byte
	}{
		{
			name: "bead outside main list",
			mutateCmd: func(cmd *Command) {
				cmd.BeadListType = 1
			},
			resolver: coatEnchantResolver(9001, nil),
			wantCode: enchantErrorUnsupported,
		},
		{
			name: "target outside main list",
			mutateCmd: func(cmd *Command) {
				cmd.TargetListType = listTypeEquipment
			},
			resolver: coatEnchantResolver(9001, nil),
			wantCode: enchantErrorUnsupported,
		},
		{
			name:     "bead slot missing",
			resolver: coatEnchantResolver(9001, nil),
			setup: func(t *testing.T, repos dnfrepo.Group) {
				saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{CharacterID: "77", Slots: map[string]dnfrepo.ItemStack{}})
			},
			wantCode: enchantErrorInvalidBead,
		},
		{
			name:     "target slot missing",
			resolver: coatEnchantResolver(9001, nil),
			setup: func(t *testing.T, repos dnfrepo.Group) {
				saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
					CharacterID: "77",
					Slots: map[string]dnfrepo.ItemStack{
						slotKey(listTypeMain, 12): {ItemID: 8801, Count: 1},
					},
				})
			},
			wantCode: enchantErrorInvalidTarget,
		},
		{
			name:     "bead carries no card",
			resolver: coatEnchantResolver(0, nil),
			wantCode: enchantErrorInvalidBead,
		},
		{
			name: "target not equipment kind",
			resolver: staticEnchantResolver(alignedcmd.EnchantBeadResolution{
				CardItemID:            9001,
				AllowedEquipmentTypes: []string{"[coat]"},
				TargetEquipmentType:   "[coat]",
				TargetKind:            "stackable",
			}),
			wantCode: enchantErrorInvalidTarget,
		},
		{
			name: "target outside bead whitelist",
			resolver: staticEnchantResolver(alignedcmd.EnchantBeadResolution{
				CardItemID:            9001,
				TargetWhitelist:       []int64{111, 222},
				AllowedEquipmentTypes: []string{"[coat]"},
				TargetEquipmentType:   "[coat]",
				TargetKind:            "equipment",
			}),
			wantCode: enchantErrorInvalidTarget,
		},
		{
			name: "equipment type not on card",
			resolver: staticEnchantResolver(alignedcmd.EnchantBeadResolution{
				CardItemID:            9001,
				AllowedEquipmentTypes: []string{"[weapon]"},
				TargetEquipmentType:   "[coat]",
				TargetKind:            "equipment",
			}),
			wantCode: enchantErrorInvalidTarget,
		},
		{
			name: "card has no allowed types",
			resolver: staticEnchantResolver(alignedcmd.EnchantBeadResolution{
				CardItemID:          9001,
				TargetEquipmentType: "[coat]",
				TargetKind:          "equipment",
			}),
			wantCode: enchantErrorInvalidTarget,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repos := dnfrepomemory.NewMemoryGroup()
			if tc.setup != nil {
				tc.setup(t, repos)
			} else {
				saveEnchantFixture(t, ctx, repos, 2, 0, 0)
			}
			owner, err := NewOwner(repos)
			if err != nil {
				t.Fatal(err)
			}
			cmd := newEnchantTestCommand(12, 30)
			if tc.mutateCmd != nil {
				tc.mutateCmd(&cmd)
			}
			result, err := owner.Enchant(ctx, cmd, tc.resolver)
			if err != nil {
				t.Fatalf("Enchant error = %v", err)
			}
			if result.Success || result.ErrorCode != tc.wantCode {
				t.Fatalf("result = %+v, want error code 0x%02X", result, tc.wantCode)
			}
			if result.Changed {
				t.Fatalf("rejected enchant must not mutate state")
			}
		})
	}
}

func TestOwnerEnchantSameSlotRejected(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveEnchantFixture(t, ctx, repos, 2, 0, 0)
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	result, err := owner.Enchant(ctx, newEnchantTestCommand(12, 12), coatEnchantResolver(9001, nil))
	if err != nil {
		t.Fatalf("Enchant error = %v", err)
	}
	if result.Success || result.ErrorCode != enchantErrorInvalidTarget {
		t.Fatalf("result = %+v, want 0x13", result)
	}
}

func TestOwnerEnchantNilResolverFailsClosed(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Enchant(ctx, newEnchantTestCommand(12, 30), nil); !errors.Is(err, ErrEnchantResolverRequired) {
		t.Fatalf("Enchant error = %v, want ErrEnchantResolverRequired", err)
	}
}
