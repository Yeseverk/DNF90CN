package inventory

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestOwnerDeleteSimpleSlotPersists(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 2): {ItemID: 100, Count: 5},
		},
	})
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	result, err := owner.Delete(ctx, NewDeleteCommand(alignedcmd.Request{
		AccountID:           "acc",
		SelectedCharacterID: 77,
	}, DeleteRequest{
		ListType:  listTypeMain,
		SlotIndex: 2,
		Count:     3,
	}))
	if err != nil {
		t.Fatalf("Delete error = %v", err)
	}
	if !result.Changed || len(result.Removed) != 1 || result.Removed[0].RemainingCount != 2 {
		t.Fatalf("result = %+v", result)
	}

	loaded := loadTestInventory(t, ctx, repos, "77")
	got := loaded.Slots[slotKey(listTypeMain, 2)]
	if got.ItemID != 100 || got.Count != 2 {
		t.Fatalf("persisted stack = %+v", got)
	}
}

func TestOwnerDeleteExtendedEntriesPersistsTogether(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 2): {ItemID: 100, Count: 5},
			slotKey(listTypeMain, 3): {ItemID: 200, Count: 7},
		},
	})

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	_, err = owner.Delete(ctx, NewDeleteCommand(alignedcmd.Request{
		AccountID:           "acc",
		SelectedCharacterID: 77,
	}, DeleteRequest{
		Extended: true,
		ListType: listTypeMain,
		Entries: []DeleteEntry{
			{SlotIndex: 2, ItemID: 100, DeleteCount: 2},
			{SlotIndex: 3, ItemID: 200, DeleteCount: 0},
		},
	}))
	if err != nil {
		t.Fatalf("Delete error = %v", err)
	}

	loaded := loadTestInventory(t, ctx, repos, "77")
	if got := loaded.Slots[slotKey(listTypeMain, 2)].Count; got != 3 {
		t.Fatalf("slot 2 count = %d, want 3", got)
	}
	if _, ok := loaded.Slots[slotKey(listTypeMain, 3)]; ok {
		t.Fatalf("slot 3 should be deleted: %+v", loaded.Slots)
	}
}

func TestOwnerDeleteExtendedMismatchDoesNotPersistPartialMutation(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 2): {ItemID: 100, Count: 5},
			slotKey(listTypeMain, 3): {ItemID: 200, Count: 7},
		},
	})

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	_, err = owner.Delete(ctx, NewDeleteCommand(alignedcmd.Request{
		AccountID:           "acc",
		SelectedCharacterID: 77,
	}, DeleteRequest{
		Extended: true,
		ListType: listTypeMain,
		Entries: []DeleteEntry{
			{SlotIndex: 2, ItemID: 100, DeleteCount: 2},
			{SlotIndex: 3, ItemID: 999, DeleteCount: 1},
		},
	}))
	if !errors.Is(err, ErrItemMismatch) {
		t.Fatalf("Delete error = %v, want ErrItemMismatch", err)
	}

	loaded := loadTestInventory(t, ctx, repos, "77")
	if got := loaded.Slots[slotKey(listTypeMain, 2)].Count; got != 5 {
		t.Fatalf("slot 2 count = %d, want unchanged 5", got)
	}
	if got := loaded.Slots[slotKey(listTypeMain, 3)].Count; got != 7 {
		t.Fatalf("slot 3 count = %d, want unchanged 7", got)
	}
}

func TestHandlerDeleteUsesOwnerAndReturnsAck(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 2): {ItemID: 100, Count: 5},
		},
	})

	got, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketDeleteItem),
		Body:                []byte{14, 0, 0, 0, 16, 0, 26, 8, 8, 1, 16, 2, 24, 100, 32, 3, 32, 0, 0},
		AccountID:           "acc",
		SelectedCharacterID: 77,
		Repositories:        repos,
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || !got.ResponseAllowed || got.Operation != "delete_item" {
		t.Fatalf("result = %+v", got)
	}
	for _, want := range []string{"inventory owner applied", "0x0012"} {
		if !strings.Contains(got.Reason, want) {
			t.Fatalf("reason %q missing %q", got.Reason, want)
		}
	}
	if len(got.UpperResponses) != 1 {
		t.Fatalf("response count = %d, want 1", len(got.UpperResponses))
	}
	ack := got.UpperResponses[0]
	if ack.MsgID != uint16(dnfenum.CmdPacketDeleteItem) || ack.Classification != dnfproto.DefaultChannelClassification || !ack.AllowCodec {
		t.Fatalf("ack = %+v", ack)
	}
	if want := []byte{1, 12, 0, 0, 0, 8, listTypeMain, 18, 6, 8, 2, 16, 3, 24, 0, 24, 0}; string(ack.Body) != string(want) {
		t.Fatalf("ack body = % X, want % X", ack.Body, want)
	}

	loaded := loadTestInventory(t, ctx, repos, "77")
	if got := loaded.Slots[slotKey(listTypeMain, 2)].Count; got != 2 {
		t.Fatalf("persisted count = %d, want 2", got)
	}
}

func TestHandlerDeleteSkillMaterialUsesAccountSharedCrystalWarehouse(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 121): {ItemID: 3021, Count: 1},
		},
	})
	if err := repos.AccountInventory.Save(ctx, dnfrepo.AccountInventoryRecord{
		AccountID: "dnf:1",
		Slots: map[string]dnfrepo.ItemStack{
			dnfrepo.AccountSharedInventorySlotKey(358): {ItemID: 3037, Count: 602},
		},
	}); err != nil {
		t.Fatal(err)
	}

	body := []byte{
		0x10, 0x00, 0x00, 0x00,
		0x10, 0x00,
		0x1A, 0x0A,
		0x08, 0x02,
		0x10, 0xE6, 0x02,
		0x18, 0xDD, 0x17,
		0x20, 0x01,
		0x20, 0x00,
		0x01,
	}
	got, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketDeleteItem),
		Body:                body,
		AccountID:           "dnf:1",
		SelectedCharacterID: 77,
		Repositories:        repos,
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || !got.ResponseAllowed || len(got.UpperResponses) != 1 {
		t.Fatalf("result = %+v", got)
	}

	account, found, err := repos.AccountInventory.Load(ctx, "dnf:1")
	if err != nil || !found {
		t.Fatalf("account inventory found=%t err=%v", found, err)
	}
	if stack := account.Slots[dnfrepo.AccountSharedInventorySlotKey(358)]; stack.ItemID != 3037 || stack.Count != 601 {
		t.Fatalf("account crystal slot = %+v, want 3037x601", stack)
	}
	character := loadTestInventory(t, ctx, repos, "77")
	if _, exists := character.Slots[dnfrepo.AccountSharedInventorySlotKey(358)]; exists {
		t.Fatalf("account crystal was written into character inventory: %+v", character.Slots)
	}
	if stack := character.Slots[slotKey(listTypeMain, 121)]; stack.ItemID != 3021 || stack.Count != 1 {
		t.Fatalf("ordinary character stack changed: %+v", stack)
	}
}

func TestHandlerDeleteEquipmentListBlocksBeforeMutation(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeEquipment, 11): {ItemID: 999, Count: 1},
		},
	})
	if err := repos.Equipment.Save(ctx, dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"11": {SlotIndex: 11, ItemID: 700, RawEntry: []byte{1, 2, 3}},
		},
	}); err != nil {
		t.Fatalf("Save equipment error = %v", err)
	}

	got, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketDeleteItem),
		Body:                []byte{listTypeEquipment, 0x0B, 0x00, 0x01, 0x00},
		AccountID:           "acc",
		SelectedCharacterID: 77,
		Repositories:        repos,
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || got.ResponseAllowed || got.Operation != "delete_item" || len(got.UpperResponses) != 0 {
		t.Fatalf("result = %+v", got)
	}
	if !strings.Contains(got.Reason, "equipment delete requires a typed equipment owner") {
		t.Fatalf("reason = %q", got.Reason)
	}

	inventory := loadTestInventory(t, ctx, repos, "77")
	if stack := inventory.Slots[slotKey(listTypeEquipment, 11)]; stack.ItemID != 999 || stack.Count != 1 {
		t.Fatalf("inventory equipment-shaped decoy mutated: %+v", stack)
	}
	equipment, ok, err := repos.Equipment.Load(ctx, "77")
	if err != nil || !ok {
		t.Fatalf("Load equipment ok=%t err=%v", ok, err)
	}
	if entry := equipment.Entries["11"]; entry.ItemID != 700 || entry.SlotIndex != 11 {
		t.Fatalf("equipment mutated: %+v", entry)
	}
}

func TestOwnerDeleteEquipmentListFailsClosedForDirectCaller(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeEquipment, 11): {ItemID: 999, Count: 1},
		},
	})
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}

	_, err = owner.Delete(ctx, NewDeleteCommand(alignedcmd.Request{SelectedCharacterID: 77}, DeleteRequest{
		ListType:  listTypeEquipment,
		SlotIndex: 11,
		Count:     1,
	}))
	if !errors.Is(err, ErrDeleteRequiresEquipmentOwner) {
		t.Fatalf("Delete error = %v, want ErrDeleteRequiresEquipmentOwner", err)
	}
	if stack := loadTestInventory(t, ctx, repos, "77").Slots[slotKey(listTypeEquipment, 11)]; stack.ItemID != 999 || stack.Count != 1 {
		t.Fatalf("inventory mutated: %+v", stack)
	}
}

func TestHandlerDeleteExtendedReturnsOneProtobufAck(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 2): {ItemID: 100, Count: 5},
			slotKey(listTypeMain, 3): {ItemID: 200, Count: 7},
		},
	})

	got, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode: uint16(dnfenum.CmdPacketDeleteItem),
		Body: []byte{
			listTypeMain, 2,
			2, 0, 2, 0, 100, 0, 0, 0, 1, 0, 0, 0,
			2, 0, 3, 0, 200, 0, 0, 0, 2, 0, 0, 0,
		},
		AccountID:           "acc",
		SelectedCharacterID: 77,
		Repositories:        repos,
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || !got.ResponseAllowed || len(got.UpperResponses) != 1 {
		t.Fatalf("result = %+v", got)
	}
	if want := []byte{1, 20, 0, 0, 0, 8, listTypeMain, 18, 6, 8, 2, 16, 1, 24, 0, 18, 6, 8, 3, 16, 2, 24, 0, 24, 0}; string(got.UpperResponses[0].Body) != string(want) {
		t.Fatalf("ack = % X, want % X", got.UpperResponses[0].Body, want)
	}

	loaded := loadTestInventory(t, ctx, repos, "77")
	if got := loaded.Slots[slotKey(listTypeMain, 2)].Count; got != 4 {
		t.Fatalf("slot 2 count = %d, want 4", got)
	}
	if got := loaded.Slots[slotKey(listTypeMain, 3)].Count; got != 5 {
		t.Fatalf("slot 3 count = %d, want 5", got)
	}
}

func TestOwnerSellZeroPricePersistsInventoryOnly(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "77", AccountID: "acc", Stats: map[string]int64{"gold": 12345}}); err != nil {
		t.Fatalf("Character.Save error = %v", err)
	}
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 4): {ItemID: 300, Count: 6, Extra: map[string]string{"sell_gold": "0"}},
		},
	})

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	result, err := owner.Sell(ctx, NewSellCommand(alignedcmd.Request{
		AccountID:           "acc",
		SelectedCharacterID: 77,
	}, DeleteOrSellRequest{
		HasListType: true,
		ListType:    listTypeMain,
		SlotIndex:   4,
		Count:       2,
	}))
	if err != nil {
		t.Fatalf("Sell error = %v", err)
	}
	if !result.Changed || result.Sold.AppliedCount != 2 || result.GoldDelta != 0 || result.UpdatedGold != 12345 {
		t.Fatalf("result = %+v", result)
	}

	loaded := loadTestInventory(t, ctx, repos, "77")
	if got := loaded.Slots[slotKey(listTypeMain, 4)].Count; got != 4 {
		t.Fatalf("persisted count = %d, want 4", got)
	}
}

func TestOwnerSellRequiresPriceEvidence(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 4): {ItemID: 300, Count: 6},
		},
	})

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	_, err = owner.Sell(ctx, NewSellCommand(alignedcmd.Request{
		AccountID:           "acc",
		SelectedCharacterID: 77,
	}, DeleteOrSellRequest{
		HasListType: true,
		ListType:    listTypeMain,
		SlotIndex:   4,
		Count:       2,
	}))
	if !errors.Is(err, ErrSellPriceMissing) {
		t.Fatalf("Sell error = %v, want ErrSellPriceMissing", err)
	}

	loaded := loadTestInventory(t, ctx, repos, "77")
	if got := loaded.Slots[slotKey(listTypeMain, 4)].Count; got != 6 {
		t.Fatalf("persisted count = %d, want unchanged 6", got)
	}
}

func TestOwnerSellPositivePriceRequiresWalletTransaction(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 4): {ItemID: 300, Count: 6, Extra: map[string]string{"sell_gold": "10"}},
		},
	})

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	_, err = owner.Sell(ctx, NewSellCommand(alignedcmd.Request{
		AccountID:           "acc",
		SelectedCharacterID: 77,
	}, DeleteOrSellRequest{
		HasListType: true,
		ListType:    listTypeMain,
		SlotIndex:   4,
		Count:       2,
	}))
	if !errors.Is(err, ErrWalletTxnRequired) {
		t.Fatalf("Sell error = %v, want ErrWalletTxnRequired", err)
	}

	loaded := loadTestInventory(t, ctx, repos, "77")
	if got := loaded.Slots[slotKey(listTypeMain, 4)].Count; got != 6 {
		t.Fatalf("persisted count = %d, want unchanged 6", got)
	}
}

func TestHandlerSellUsesOwnerAndReturnsAck(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{CharacterID: "77", AccountID: "acc", Stats: map[string]int64{"gold": 12345}}); err != nil {
		t.Fatalf("Character.Save error = %v", err)
	}
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 4): {ItemID: 300, Count: 6, Extra: map[string]string{"sell_gold": "0"}},
		},
	})

	got, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketSellItem),
		Body:                []byte{listTypeMain, 0x04, 0x00, 0x02, 0x00},
		AccountID:           "acc",
		SelectedCharacterID: 77,
		Repositories:        repos,
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || !got.ResponseAllowed || got.Operation != "sell_item" {
		t.Fatalf("result = %+v", got)
	}
	for _, want := range []string{"inventory owner applied", "0x0016", "updatedGold=12345"} {
		if !strings.Contains(got.Reason, want) {
			t.Fatalf("reason %q missing %q", got.Reason, want)
		}
	}
	if len(got.UpperResponses) != 1 {
		t.Fatalf("responses = %+v", got.UpperResponses)
	}
	ack := got.UpperResponses[0]
	if ack.MsgID != uint16(dnfenum.CmdPacketSellItem) || ack.Classification != 1 || !ack.AllowCodec {
		t.Fatalf("ack header = %+v", ack)
	}
	wantBody := []byte{1, 0x39, 0x30, 0x00, 0x00, listTypeMain, 0x04, 0x00, 0x02, 0x00}
	if string(ack.Body) != string(wantBody) {
		t.Fatalf("ack body = % X, want % X", ack.Body, wantBody)
	}

	loaded := loadTestInventory(t, ctx, repos, "77")
	if got := loaded.Slots[slotKey(listTypeMain, 4)].Count; got != 4 {
		t.Fatalf("persisted count = %d, want 4", got)
	}
}

func TestOwnerMoveToEmptySlotPersists(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 2): {ItemID: 100, Count: 1},
		},
	})

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	result, err := owner.Move(ctx, NewMoveCommand(alignedcmd.Request{
		AccountID:           "acc",
		SelectedCharacterID: 77,
	}, MoveItemspaceRequest{
		SourceListType:       listTypeMain,
		SourceSlotIndex:      2,
		MoveCount:            1,
		DestinationListType:  listTypeMain,
		DestinationSlotIndex: 8,
	}))
	if err != nil {
		t.Fatalf("Move error = %v", err)
	}
	if !result.Changed || result.Mode != "move" || result.MoveCount != 1 {
		t.Fatalf("result = %+v", result)
	}

	loaded := loadTestInventory(t, ctx, repos, "77")
	if _, ok := loaded.Slots[slotKey(listTypeMain, 2)]; ok {
		t.Fatalf("source slot should be empty: %+v", loaded.Slots)
	}
	got := loaded.Slots[slotKey(listTypeMain, 8)]
	if got.ItemID != 100 || got.Count != 1 {
		t.Fatalf("destination stack = %+v", got)
	}
}

func TestOwnerMoveSplitStackPersists(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	extra := map[string]string{
		"item_kind":   "stackable",
		"pvf_path":    "stackable/material/test_0100.stk",
		"stack_limit": "20",
	}
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 2): {ItemID: 100, Count: 10, Extra: extra},
		},
	})

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	result, err := owner.Move(ctx, NewMoveCommand(alignedcmd.Request{
		AccountID:           "acc",
		SelectedCharacterID: 77,
	}, MoveItemspaceRequest{
		SourceListType:       listTypeMain,
		SourceSlotIndex:      2,
		MoveCount:            3,
		DestinationListType:  listTypeMain,
		DestinationSlotIndex: 8,
	}))
	if err != nil {
		t.Fatalf("Move error = %v", err)
	}
	if result.Mode != "split" || result.MoveCount != 3 {
		t.Fatalf("result = %+v", result)
	}

	loaded := loadTestInventory(t, ctx, repos, "77")
	if got := loaded.Slots[slotKey(listTypeMain, 2)].Count; got != 7 {
		t.Fatalf("source count = %d, want 7", got)
	}
	if got := loaded.Slots[slotKey(listTypeMain, 8)].Count; got != 3 {
		t.Fatalf("destination count = %d, want 3", got)
	}
}

func TestOwnerMoveStacksSameItemPersists(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	extra := map[string]string{
		"item_kind":   "stackable",
		"pvf_path":    "stackable/material/test_0100.stk",
		"stack_limit": "10",
	}
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 2): {ItemID: 100, Count: 3, Extra: extra},
			slotKey(listTypeMain, 8): {ItemID: 100, Count: 4, Extra: extra},
		},
	})

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	result, err := owner.Move(ctx, NewMoveCommand(alignedcmd.Request{
		AccountID:           "acc",
		SelectedCharacterID: 77,
	}, MoveItemspaceRequest{
		SourceListType:       listTypeMain,
		SourceSlotIndex:      2,
		MoveCount:            3,
		DestinationListType:  listTypeMain,
		DestinationSlotIndex: 8,
	}))
	if err != nil {
		t.Fatalf("Move error = %v", err)
	}
	if result.Mode != "stack" || result.MoveCount != 3 {
		t.Fatalf("result = %+v", result)
	}

	loaded := loadTestInventory(t, ctx, repos, "77")
	if _, ok := loaded.Slots[slotKey(listTypeMain, 2)]; ok {
		t.Fatalf("source slot should be empty: %+v", loaded.Slots)
	}
	if got := loaded.Slots[slotKey(listTypeMain, 8)].Count; got != 7 {
		t.Fatalf("destination count = %d, want 7", got)
	}
}

func TestOwnerMoveSwapsDistinctItems(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 2): {ItemID: 100, Count: 1},
			slotKey(listTypeMain, 8): {ItemID: 200, Count: 1},
		},
	})

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	result, err := owner.Move(ctx, NewMoveCommand(alignedcmd.Request{
		AccountID:           "acc",
		SelectedCharacterID: 77,
	}, MoveItemspaceRequest{
		SourceListType:       listTypeMain,
		SourceSlotIndex:      2,
		MoveCount:            1,
		DestinationListType:  listTypeMain,
		DestinationSlotIndex: 8,
	}))
	if err != nil {
		t.Fatalf("Move error = %v", err)
	}
	if result.Mode != "swap" {
		t.Fatalf("result = %+v", result)
	}

	loaded := loadTestInventory(t, ctx, repos, "77")
	if got := loaded.Slots[slotKey(listTypeMain, 2)].ItemID; got != 200 {
		t.Fatalf("source slot item = %d, want 200", got)
	}
	if got := loaded.Slots[slotKey(listTypeMain, 8)].ItemID; got != 100 {
		t.Fatalf("destination slot item = %d, want 100", got)
	}
}

func TestOwnerMoveRejectsEquipmentOwner(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeAvatar, 2): {ItemID: 100, Count: 1},
		},
	})

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	_, err = owner.Move(ctx, NewMoveCommand(alignedcmd.Request{
		AccountID:           "acc",
		SelectedCharacterID: 77,
	}, MoveItemspaceRequest{
		SourceListType:       listTypeEquipment,
		SourceSlotIndex:      2,
		MoveCount:            1,
		DestinationListType:  listTypeMain,
		DestinationSlotIndex: 8,
	}))
	if !errors.Is(err, ErrMoveRequiresEquipmentOwner) {
		t.Fatalf("Move error = %v, want ErrMoveRequiresEquipmentOwner", err)
	}

	loaded := loadTestInventory(t, ctx, repos, "77")
	if got := loaded.Slots[slotKey(listTypeAvatar, 2)].ItemID; got != 100 {
		t.Fatalf("persisted item = %d, want unchanged 100", got)
	}
}

func TestSlotKeyKeepsEquipmentAndAvatarContainersDistinct(t *testing.T) {
	if equipment, avatar := slotKey(listTypeEquipment, 11), slotKey(listTypeAvatar, 11); equipment == avatar {
		t.Fatalf("equipment key %q aliases avatar key %q", equipment, avatar)
	}
}

func TestHandlerMoveUsesOwnerAndReturnsAck(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 2): {ItemID: 100, Count: 1},
		},
	})

	got, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketMoveItemspace),
		Body:                currentMoveItemspaceBody(listTypeMain, 2, 0, 1, listTypeMain, 8, 0, 0, -1),
		AccountID:           "acc",
		SelectedCharacterID: 77,
		Repositories:        repos,
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || !got.ResponseAllowed || got.Operation != "move_itemspace" {
		t.Fatalf("result = %+v", got)
	}
	for _, want := range []string{"inventory owner applied", "0x0013"} {
		if !strings.Contains(got.Reason, want) {
			t.Fatalf("reason %q missing %q", got.Reason, want)
		}
	}
	if len(got.UpperResponses) != 1 {
		t.Fatalf("responses = %+v", got.UpperResponses)
	}
	ack := got.UpperResponses[0]
	if ack.MsgID != uint16(dnfenum.CmdPacketMoveItemspace) || ack.Classification != 1 || !ack.AllowCodec {
		t.Fatalf("ack header = %+v", ack)
	}
	wantBody := []byte{1, listTypeMain, 0x02, 0x00, 1, 0, 0, 0, listTypeMain, 0x08, 0x00}
	if string(ack.Body) != string(wantBody) {
		t.Fatalf("ack body = % X, want % X", ack.Body, wantBody)
	}

	loaded := loadTestInventory(t, ctx, repos, "77")
	if _, ok := loaded.Slots[slotKey(listTypeMain, 2)]; ok {
		t.Fatalf("source slot should be empty: %+v", loaded.Slots)
	}
	if got := loaded.Slots[slotKey(listTypeMain, 8)].ItemID; got != 100 {
		t.Fatalf("destination item = %d, want 100", got)
	}
}

func TestHandlerMoveDirectGrantedPetCreatesTypedState(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypePet, 0): {
				ItemID: 400990199,
				Count:  1,
				Extra: map[string]string{
					"source":         "booster_item",
					"item_kind":      "equipment",
					"equipment_type": "[creature]",
					"pvf_path":       "equipment/creature/2018Summer/chn_2018_summer_400990199.equ",
				},
			},
		},
	})

	got, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketMoveItemspace),
		Body:                currentMoveItemspaceBody(listTypePet, 0, 0, 1, listTypePet, 24, 0, 0, -1),
		AccountID:           "acc",
		SelectedCharacterID: 77,
		Repositories:        repos,
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || !got.ResponseAllowed || got.Operation != "move_itemspace" {
		t.Fatalf("result = %+v", got)
	}
	if len(got.PostActions) != 1 || got.PostActions[0] != alignedcmd.PostActionRefreshSelectedItemContainers {
		t.Fatalf("post actions = %v", got.PostActions)
	}

	loaded := loadTestInventory(t, ctx, repos, "77")
	if _, ok := loaded.Slots[slotKey(listTypePet, 0)]; ok {
		t.Fatalf("pet source slot should be empty: %+v", loaded.Slots)
	}
	moved := loaded.Slots[slotKey(listTypePet, 24)]
	serial := moved.Extra["creature_serial_or_handle"]
	if moved.ItemID != 400990199 || serial == "" || moved.Extra["creature_key"] != serial {
		t.Fatalf("moved pet = %+v", moved)
	}
	petRecord, ok, err := repos.Pet.Load(ctx, "77")
	if err != nil || !ok {
		t.Fatalf("load pet ok=%t err=%v", ok, err)
	}
	entry := petRecord.Entries[serial]
	if entry.ItemID != moved.ItemID || entry.SourceListType != listTypePet || entry.SourceSlotIndex != 24 || entry.Level != 1 || entry.Satiety != 100 {
		t.Fatalf("pet entry = %+v", entry)
	}
}

func TestHandlerMovePetToActorEndpoint17ReturnsAckThenAuthoritativeRefreshes(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypePet, 48): {ItemID: 9001, Count: 1, Extra: map[string]string{"creature_serial_or_handle": "37"}},
		},
	})
	if err := repos.Equipment.Save(ctx, dnfrepo.EquipmentRecord{CharacterID: "77", Entries: map[string]dnfrepo.EquipmentEntry{}}); err != nil {
		t.Fatalf("Save equipment error = %v", err)
	}
	if err := repos.Pet.Save(ctx, dnfrepo.PetRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.PetEntry{
			"37": {PetKey: "37", CreatureKey: 37, ItemID: 9001, SourceListType: listTypePet, SourceSlotIndex: 48, Level: 1, Satiety: 100},
		},
	}); err != nil {
		t.Fatalf("Save pet error = %v", err)
	}

	got, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketMoveItemspace),
		Body:                currentMoveItemspaceBody(listTypePet, 48, 37, 1, listTypeActorWornAlt, 26, 0, 0, -1),
		AccountID:           "acc",
		SelectedCharacterID: 77,
		Repositories:        repos,
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || !got.ResponseAllowed || got.Operation != "move_itemspace" {
		t.Fatalf("result = %+v", got)
	}
	if len(got.UpperResponses) != 1 {
		t.Fatalf("responses = %+v", got.UpperResponses)
	}
	if len(got.ItemSlotRefreshes) != 0 {
		t.Fatalf("pet slot26 equip emitted duplicate item slot refreshes=%v", got.ItemSlotRefreshes)
	}
	wantPetActions := []alignedcmd.PostAction{
		alignedcmd.PostActionRefreshSelectedItemContainers,
		alignedcmd.PostActionRefreshSelectedCreatureState,
		alignedcmd.PostActionRefreshSelectedActorAppearance,
	}
	if !reflect.DeepEqual(got.PostActions, wantPetActions) {
		t.Fatalf("post actions = %v, want %v", got.PostActions, wantPetActions)
	}
	if ack := got.UpperResponses[0]; ack.MsgID != uint16(dnfenum.CmdPacketMoveItemspace) || string(ack.Body) != string([]byte{1, listTypePet, 0x30, 0x00, 1, 0, 0, 0, listTypeActorWornAlt, 0x1A, 0x00}) {
		t.Fatalf("ack = %+v body=% X", ack, ack.Body)
	}

	inventory := loadTestInventory(t, ctx, repos, "77")
	if _, ok := inventory.Slots[slotKey(listTypePet, 48)]; ok {
		t.Fatalf("pet source slot should be empty: %+v", inventory.Slots)
	}
	equipment, ok, err := repos.Equipment.Load(ctx, "77")
	if err != nil || !ok {
		t.Fatalf("load equipment ok=%v err=%v", ok, err)
	}
	if entry := equipment.Entries["26"]; entry.ItemID != 9001 || len(entry.RawEntry) < 28 {
		t.Fatalf("equipment pet entry = %+v", entry)
	}
	pet, ok, err := repos.Pet.Load(ctx, "77")
	if err != nil || !ok {
		t.Fatalf("load pet ok=%v err=%v", ok, err)
	}
	if pet.EquippedKey != "37" || pet.Entries["37"].SourceListType != listTypeEquipment || pet.Entries["37"].SourceSlotIndex != 26 {
		t.Fatalf("equipped pet state = %+v", pet)
	}
}

func TestHandlerMovePetUnequipKeepsCurrentEXEEndpointOrder(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{CharacterID: "77", Slots: map[string]dnfrepo.ItemStack{}})
	if err := repos.Equipment.Save(ctx, dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"26": {SlotIndex: 26, ItemID: 9001, RawEntry: testPetEquipRaw(26, 9001, 37), Extra: map[string]string{"source": "equipped"}},
		},
	}); err != nil {
		t.Fatalf("Save equipment error = %v", err)
	}
	if err := repos.Pet.Save(ctx, dnfrepo.PetRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.PetEntry{
			"37": {PetKey: "37", CreatureKey: 37, ItemID: 9001, SourceListType: listTypeEquipment, SourceSlotIndex: 26, Level: 1, Satiety: 100},
		},
		EquippedKey: "37",
	}); err != nil {
		t.Fatalf("Save pet error = %v", err)
	}

	got, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketMoveItemspace),
		Body:                currentMoveItemspaceBody(listTypePet, 48, 0, 1, listTypeActorWornAlt, 26, 37, 0, -1),
		AccountID:           "acc",
		SelectedCharacterID: 77,
		Repositories:        repos,
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || !got.ResponseAllowed {
		t.Fatalf("result = %+v", got)
	}
	if len(got.UpperResponses) != 1 {
		t.Fatalf("responses = %+v", got.UpperResponses)
	}
	if len(got.ItemSlotRefreshes) != 0 {
		t.Fatalf("pet slot26 unequip emitted duplicate item slot refreshes=%v", got.ItemSlotRefreshes)
	}
	wantPetActions := []alignedcmd.PostAction{
		alignedcmd.PostActionRefreshSelectedItemContainers,
		alignedcmd.PostActionRefreshSelectedCreatureState,
		alignedcmd.PostActionRefreshSelectedActorAppearance,
	}
	if !reflect.DeepEqual(got.PostActions, wantPetActions) {
		t.Fatalf("post actions = %v, want %v", got.PostActions, wantPetActions)
	}
	if ack := got.UpperResponses[0]; ack.MsgID != uint16(dnfenum.CmdPacketMoveItemspace) || string(ack.Body) != string([]byte{1, listTypePet, 0x30, 0x00, 1, 0, 0, 0, listTypeActorWornAlt, 0x1A, 0x00}) {
		t.Fatalf("ack = %+v body=% X", ack, ack.Body)
	}

	equipment, ok, err := repos.Equipment.Load(ctx, "77")
	if err != nil || !ok {
		t.Fatalf("load equipment ok=%v err=%v", ok, err)
	}
	if _, ok := equipment.Entries["26"]; ok {
		t.Fatalf("equipment slot should be empty: %+v", equipment.Entries)
	}
	inventory := loadTestInventory(t, ctx, repos, "77")
	if stack := inventory.Slots[slotKey(listTypePet, 48)]; stack.ItemID != 9001 || stack.Extra["creature_serial_or_handle"] != "37" {
		t.Fatalf("pet stack = %+v", stack)
	}
	pet, ok, err := repos.Pet.Load(ctx, "77")
	if err != nil || !ok {
		t.Fatalf("load pet ok=%v err=%v", ok, err)
	}
	if pet.EquippedKey != "" || pet.Entries["37"].SourceListType != listTypePet || pet.Entries["37"].SourceSlotIndex != 48 {
		t.Fatalf("unequipped pet state = %+v", pet)
	}
}

func TestHandlerMoveEquipmentBlocksBeforeOwnerAndMutation(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 5): {ItemID: 700, Count: 1, Extra: map[string]string{"raw_entry_hex": "000102030405060708090c000d0e"}},
		},
	})
	if err := repos.Equipment.Save(ctx, dnfrepo.EquipmentRecord{CharacterID: "77", Entries: map[string]dnfrepo.EquipmentEntry{}}); err != nil {
		t.Fatalf("Save equipment error = %v", err)
	}

	got, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketMoveItemspace),
		Body:                currentMoveItemspaceBody(listTypeMain, 5, 0, 1, listTypeEquipment, 11, 0, 0, -1),
		AccountID:           "acc",
		SelectedCharacterID: 77,
		Repositories:        repos,
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || got.ResponseAllowed || got.Operation != "move_itemspace" || len(got.UpperResponses) != 0 {
		t.Fatalf("result = %+v", got)
	}
	if !strings.Contains(got.Reason, "normal equipment move requires the current PVF placement validator") {
		t.Fatalf("reason = %q", got.Reason)
	}

	loaded := loadTestInventory(t, ctx, repos, "77")
	if stack := loaded.Slots[slotKey(listTypeMain, 5)]; stack.ItemID != 700 || stack.Count != 1 {
		t.Fatalf("source slot mutated: %+v", stack)
	}
	equipment, ok, err := repos.Equipment.Load(ctx, "77")
	if err != nil || !ok {
		t.Fatalf("Load equipment ok=%t err=%v", ok, err)
	}
	if len(equipment.Entries) != 0 {
		t.Fatalf("equipment mutated: %+v", equipment.Entries)
	}
}

func TestHandlerMoveEquipmentUsesPVFValidatorAndReturnsAckThenActorRefresh(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	raw := make([]byte, currentItemListEntrySize)
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 5): {ItemID: 700, Count: 1, RawEntry: raw},
		},
	})
	if err := repos.Equipment.Save(ctx, dnfrepo.EquipmentRecord{CharacterID: "77", Entries: map[string]dnfrepo.EquipmentEntry{}}); err != nil {
		t.Fatal(err)
	}
	placements := make([]alignedcmd.EquipmentPlacementRequest, 0, 1)
	request := alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketMoveItemspace),
		Body:                currentMoveItemspaceBody(listTypeMain, 5, 0, 1, listTypeEquipment, 11, 0, 0, -1),
		AccountID:           "acc",
		SelectedCharacterID: 77,
		Repositories:        repos,
		EquipmentPlacement: func(_ context.Context, placement alignedcmd.EquipmentPlacementRequest) error {
			placements = append(placements, placement)
			return nil
		},
	}

	equipped, err := NewHandler().Handle(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	wantWearActions := []alignedcmd.PostAction{
		alignedcmd.PostActionRefreshSelectedItemContainers,
		alignedcmd.PostActionRefreshSelectedActorAppearance,
	}
	if !equipped.Handled || !equipped.ResponseAllowed || len(equipped.UpperResponses) != 1 ||
		!reflect.DeepEqual(equipped.PostActions, wantWearActions) {
		t.Fatalf("equip result = %+v", equipped)
	}
	if len(equipped.ItemSlotRefreshes) != 0 {
		t.Fatalf("equipment equip emitted duplicate item slot refreshes=%v", equipped.ItemSlotRefreshes)
	}
	wantAck := []byte{1, listTypeMain, 5, 0, 1, 0, 0, 0, listTypeEquipment, 11, 0}
	if got := equipped.UpperResponses[0].Body; string(got) != string(wantAck) {
		t.Fatalf("equip ACK = %x, want %x", got, wantAck)
	}
	if len(placements) != 1 || placements[0].CharacterID != "77" || placements[0].ItemID != 700 || placements[0].SourceListType != listTypeMain || placements[0].TargetSlotIndex != 11 {
		t.Fatalf("placements = %+v", placements)
	}
	if _, ok := loadTestInventory(t, ctx, repos, "77").Slots[slotKey(listTypeMain, 5)]; ok {
		t.Fatal("equipped source slot still occupied")
	}
	storedEquipment, ok, err := repos.Equipment.Load(ctx, "77")
	if err != nil || !ok || storedEquipment.Entries["11"].ItemID != 700 {
		t.Fatalf("stored equipment ok=%t err=%v record=%+v", ok, err, storedEquipment)
	}

	unequipped, err := NewHandler().Handle(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if !unequipped.ResponseAllowed || len(unequipped.UpperResponses) != 1 ||
		!reflect.DeepEqual(unequipped.PostActions, wantWearActions) {
		t.Fatalf("unequip result = %+v", unequipped)
	}
	if len(unequipped.ItemSlotRefreshes) != 0 {
		t.Fatalf("equipment unequip emitted duplicate item slot refreshes=%v", unequipped.ItemSlotRefreshes)
	}
	if got := unequipped.UpperResponses[0].Body; string(got) != string(wantAck) {
		t.Fatalf("unequip ACK changed endpoint order: %x", got)
	}
	if stack := loadTestInventory(t, ctx, repos, "77").Slots[slotKey(listTypeMain, 5)]; stack.ItemID != 700 {
		t.Fatalf("unequipped stack = %+v", stack)
	}
}

func TestHandlerMoveEquipmentRedirectsStaleUnequipDestinationAndReportsCommittedSourceInAck(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 10): {ItemID: 900, Count: 1, RawEntry: []byte{0x90}},
		},
	})
	if err := repos.Equipment.Save(ctx, dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"17": {SlotIndex: 17, ItemID: 700, RawEntry: []byte{0x70}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	placements := make([]alignedcmd.EquipmentPlacementRequest, 0, 1)
	result, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketMoveItemspace),
		Body:                currentMoveItemspaceBody(listTypeMain, 10, 0, 1, listTypeEquipment, 17, 0x47E0, 0, -1),
		AccountID:           "acc",
		SelectedCharacterID: 77,
		Repositories:        repos,
		EquipmentPlacement: func(_ context.Context, placement alignedcmd.EquipmentPlacementRequest) error {
			placements = append(placements, placement)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantWearActions := []alignedcmd.PostAction{
		alignedcmd.PostActionRefreshSelectedItemContainers,
		alignedcmd.PostActionRefreshSelectedActorAppearance,
	}
	if !result.ResponseAllowed || len(result.UpperResponses) != 1 ||
		!reflect.DeepEqual(result.PostActions, wantWearActions) {
		t.Fatalf("result = %+v", result)
	}
	if len(result.ItemSlotRefreshes) != 0 {
		t.Fatalf("redirected equipment unequip emitted duplicate item slot refreshes=%v", result.ItemSlotRefreshes)
	}
	wantAck := []byte{1, listTypeMain, 11, 0, 1, 0, 0, 0, listTypeEquipment, 17, 0}
	if got := result.UpperResponses[0].Body; !bytes.Equal(got, wantAck) {
		t.Fatalf("redirected ACK = %x, want %x", got, wantAck)
	}
	loaded := loadTestInventory(t, ctx, repos, "77")
	if loaded.Slots[slotKey(listTypeMain, 10)].ItemID != 900 || loaded.Slots[slotKey(listTypeMain, 11)].ItemID != 700 {
		t.Fatalf("redirected inventory = %+v", loaded.Slots)
	}
	storedEquipment, ok, err := repos.Equipment.Load(ctx, "77")
	if err != nil || !ok || len(storedEquipment.Entries) != 0 {
		t.Fatalf("equipment after unequip ok=%t err=%v entries=%+v", ok, err, storedEquipment.Entries)
	}
	if len(placements) != 0 {
		t.Fatalf("redirected unequip unexpectedly invoked PVF swap validator: %+v", placements)
	}
}

func TestOwnerSortMainSegmentPersists(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 9):  {ItemID: 300, Count: 1, Extra: map[string]string{"item_kind": "stackable"}},
			slotKey(listTypeMain, 12): {ItemID: 200, Count: 1, Extra: map[string]string{"item_kind": "equipment"}},
			slotKey(listTypeMain, 20): {ItemID: 100, Count: 1, Extra: map[string]string{"item_kind": "avatar"}},
			slotKey(listTypeMain, 65): {ItemID: 999, Count: 1, Extra: map[string]string{"item_kind": "equipment"}},
		},
	})

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	result, err := owner.Sort(ctx, NewSortCommand(alignedcmd.Request{
		AccountID:           "acc",
		SelectedCharacterID: 77,
	}, SortItemRequest{
		ListType: listTypeMain,
		Category: 1,
	}))
	if err != nil {
		t.Fatalf("Sort error = %v", err)
	}
	if !result.Changed || result.Mode != "sort" || result.MovedCount != 3 || result.StartSlot != 9 || result.EndSlot != 64 {
		t.Fatalf("result = %+v", result)
	}

	loaded := loadTestInventory(t, ctx, repos, "77")
	if got := loaded.Slots[slotKey(listTypeMain, 9)].ItemID; got != 100 {
		t.Fatalf("slot 9 item = %d, want 100", got)
	}
	if got := loaded.Slots[slotKey(listTypeMain, 10)].ItemID; got != 200 {
		t.Fatalf("slot 10 item = %d, want 200", got)
	}
	if got := loaded.Slots[slotKey(listTypeMain, 11)].ItemID; got != 300 {
		t.Fatalf("slot 11 item = %d, want 300", got)
	}
	if _, ok := loaded.Slots[slotKey(listTypeMain, 12)]; ok {
		t.Fatalf("slot 12 should be empty after compaction: %+v", loaded.Slots)
	}
	if got := loaded.Slots[slotKey(listTypeMain, 65)].ItemID; got != 999 {
		t.Fatalf("outside segment item = %d, want 999", got)
	}
}

func TestOwnerSortUnknownCategoryIsNoop(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 20): {ItemID: 100, Count: 1},
		},
	})

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	result, err := owner.Sort(ctx, NewSortCommand(alignedcmd.Request{
		AccountID:           "acc",
		SelectedCharacterID: 77,
	}, SortItemRequest{
		ListType: listTypeMain,
		Category: 99,
	}))
	if err != nil {
		t.Fatalf("Sort error = %v", err)
	}
	if result.Changed || result.Mode != "noop" || result.MovedCount != 0 {
		t.Fatalf("result = %+v", result)
	}

	loaded := loadTestInventory(t, ctx, repos, "77")
	if got := loaded.Slots[slotKey(listTypeMain, 20)].ItemID; got != 100 {
		t.Fatalf("slot 20 item = %d, want unchanged 100", got)
	}
}

func TestOwnerSortRejectsAccountCargo(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Warehouse: map[string]dnfrepo.ItemStack{
			slotKey(listTypePersonalCargo, 1): {ItemID: 100, Count: 1},
		},
	})

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	_, err = owner.Sort(ctx, NewSortCommand(alignedcmd.Request{
		AccountID:           "acc",
		SelectedCharacterID: 77,
	}, SortItemRequest{
		ListType: listTypeAccountCargo,
		Category: 11,
	}))
	if !errors.Is(err, ErrSortAccountCargoUnsupported) {
		t.Fatalf("Sort error = %v, want ErrSortAccountCargoUnsupported", err)
	}

	loaded := loadTestInventory(t, ctx, repos, "77")
	if got := loaded.Warehouse[slotKey(listTypePersonalCargo, 1)].ItemID; got != 100 {
		t.Fatalf("warehouse item = %d, want unchanged 100", got)
	}
}

func TestHandlerSortMainReturnsAckAndFullRefresh(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 12): {ItemID: 200, Count: 1, Extra: map[string]string{"item_kind": "equipment"}},
			slotKey(listTypeMain, 20): {ItemID: 100, Count: 1, Extra: map[string]string{"item_kind": "avatar"}},
		},
	})
	if err := repos.Settings.Save(ctx, dnfrepo.SettingsRecord{
		Scope: dnfrepo.CharacterContainerStateScope("77"),
		Values: map[string]string{
			"main_list_param16":           "24",
			"avatar_list_param16":         "0",
			"personal_cargo_list_param16": "8",
		},
	}); err != nil {
		t.Fatalf("save container state: %v", err)
	}

	got, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketSortItem),
		Body:                []byte{6, 0, 0, 0, 16, listTypeMain, 24, 1, 32, 0},
		AccountID:           "acc",
		SelectedCharacterID: 77,
		Repositories:        repos,
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || !got.ResponseAllowed || got.Operation != "sort_item" {
		t.Fatalf("result = %+v", got)
	}
	if len(got.UpperResponses) != 2 {
		t.Fatalf("responses = %+v", got.UpperResponses)
	}
	ack := got.UpperResponses[0]
	if ack.MsgID != uint16(dnfenum.CmdPacketSortItem) || ack.Classification != dnfproto.DefaultChannelClassification || !ack.AllowCodec {
		t.Fatalf("ack = %+v", ack)
	}
	if string(ack.Body) != string([]byte{1, 4, 0, 0, 0, 8, listTypeMain, 16, 0}) {
		t.Fatalf("ack body = % X", ack.Body)
	}
	refresh := got.UpperResponses[1]
	if refresh.MsgID != msgItemListRefresh || refresh.Classification != 0 || !refresh.AllowCodec {
		t.Fatalf("refresh = %+v", refresh)
	}
	if len(refresh.Body) != 5+2*currentItemListEntrySize {
		t.Fatalf("refresh body length = %d", len(refresh.Body))
	}
	if refresh.Body[0] != listTypeMain || refresh.Body[1] != 24 || refresh.Body[2] != 0 || refresh.Body[3] != 2 || refresh.Body[4] != 0 {
		t.Fatalf("refresh header = % X", refresh.Body[:5])
	}
	if slot := int16(refresh.Body[5]) | int16(refresh.Body[6])<<8; slot != 9 {
		t.Fatalf("first slot = %d, want 9", slot)
	}
	if item := int32(refresh.Body[7]) | int32(refresh.Body[8])<<8 | int32(refresh.Body[9])<<16 | int32(refresh.Body[10])<<24; item != 100 {
		t.Fatalf("first item = %d, want 100", item)
	}
	second := 5 + currentItemListEntrySize
	if slot := int16(refresh.Body[second]) | int16(refresh.Body[second+1])<<8; slot != 10 {
		t.Fatalf("second slot = %d, want 10", slot)
	}
	if item := int32(refresh.Body[second+2]) | int32(refresh.Body[second+3])<<8 | int32(refresh.Body[second+4])<<16 | int32(refresh.Body[second+5])<<24; item != 200 {
		t.Fatalf("second item = %d, want 200", item)
	}

	loaded := loadTestInventory(t, ctx, repos, "77")
	if got := loaded.Slots[slotKey(listTypeMain, 9)].ItemID; got != 100 {
		t.Fatalf("slot 9 item = %d, want 100", got)
	}
	if got := loaded.Slots[slotKey(listTypeMain, 10)].ItemID; got != 200 {
		t.Fatalf("slot 10 item = %d, want 200", got)
	}
}

func TestHandlerSortPetFailsClosedWithoutRefresh(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypePet, 5): {ItemID: 900, Count: 1, Extra: map[string]string{"creature_serial_or_handle": "1234"}},
		},
	})

	got, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketSortItem),
		Body:                []byte{listTypePet, 0x05, 0x00},
		AccountID:           "acc",
		SelectedCharacterID: 77,
		Repositories:        repos,
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || got.ResponseAllowed || got.Operation != "sort_item" || len(got.UpperResponses) != 0 {
		t.Fatalf("result = %+v", got)
	}
	if !strings.Contains(got.Reason, ErrPetInventoryOwnerRequired.Error()) {
		t.Fatalf("reason = %q", got.Reason)
	}
	stack := loadTestInventory(t, ctx, repos, "77").Slots[slotKey(listTypePet, 5)]
	if stack.ItemID != 900 || stack.Count != 1 || stack.Extra["creature_serial_or_handle"] != "1234" {
		t.Fatalf("pet inventory mutated: %+v", stack)
	}
}

func TestHandlerSortAvatarReturnsAckAnd127ByteRowRefresh(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeAvatar, 8):  {ItemID: 300, Count: 1, Extra: map[string]string{"item_kind": "avatar"}},
			slotKey(listTypeAvatar, 10): {ItemID: 200, Count: 1, Extra: map[string]string{"item_kind": "avatar"}},
		},
	})

	got, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketSortItem),
		Body:                []byte{listTypeAvatar, 0x08, 0x00},
		AccountID:           "acc",
		SelectedCharacterID: 77,
		Repositories:        repos,
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || !got.ResponseAllowed || got.Operation != "sort_item" || len(got.UpperResponses) != 2 {
		t.Fatalf("result = %+v", got)
	}
	ack := got.UpperResponses[0]
	if ack.MsgID != uint16(dnfenum.CmdPacketSortItem) || string(ack.Body) != string([]byte{1, 4, 0, 0, 0, 8, listTypeAvatar, 16, 0}) {
		t.Fatalf("ack = %+v", ack)
	}
	refresh := got.UpperResponses[1]
	const avatarRowSize = currentItemListEntrySize + 8
	if refresh.MsgID != msgItemListRefresh || refresh.Classification != 0 || len(refresh.Body) != 5+2*avatarRowSize {
		t.Fatalf("refresh = %+v", refresh)
	}
	if string(refresh.Body[:5]) != string([]byte{listTypeAvatar, 0, 0, 2, 0}) {
		t.Fatalf("refresh header = % X", refresh.Body[:5])
	}
	for index := range 2 {
		row := refresh.Body[5+index*avatarRowSize : 5+(index+1)*avatarRowSize]
		if !bytes.Equal(row[currentItemListEntrySize:], make([]byte, 8)) {
			t.Fatalf("row %d optional lengths = % X", index, row[currentItemListEntrySize:])
		}
	}
	loaded := loadTestInventory(t, ctx, repos, "77")
	if loaded.Slots[slotKey(listTypeAvatar, 0)].ItemID != 200 || loaded.Slots[slotKey(listTypeAvatar, 1)].ItemID != 300 {
		t.Fatalf("sorted avatar slots = %+v", loaded.Slots)
	}
}

func TestOwnerRepairZeroCostPersistsDurability(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestCharacter(t, ctx, repos, dnfrepo.CharacterRecord{CharacterID: "77", Stats: map[string]int64{"gold": 43210}})
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 5): {
				ItemID: 700,
				Count:  1,
				Extra: map[string]string{
					"item_kind":      "equipment",
					"durability":     "12",
					"max_durability": "20",
					"repair_gold":    "0",
				},
			},
		},
	})

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	result, err := owner.Repair(ctx, NewRepairCommand(alignedcmd.Request{
		AccountID:           "acc",
		SelectedCharacterID: 77,
	}, RepairEquipmentRequest{
		InvenType: listTypeMain,
		SlotIndex: 5,
	}), inventoryRepairTestResolver(20, 0, 20))
	if err != nil {
		t.Fatalf("Repair error = %v", err)
	}
	if !result.Changed || result.OldDurability != 12 || result.NewDurability != 20 || result.Cost != 0 || result.UpdatedGold != 43210 {
		t.Fatalf("result = %+v", result)
	}

	loaded := loadTestInventory(t, ctx, repos, "77")
	if got := loaded.Slots[slotKey(listTypeMain, 5)].Extra["durability"]; got != "20" {
		t.Fatalf("durability = %q, want 20", got)
	}
}

func inventoryRepairTestResolver(maxDurability int64, repairPrice int64, grade int64) alignedcmd.RepairCostResolver {
	return func(itemID int64) (alignedcmd.RepairCostEvidence, error) {
		return alignedcmd.RepairCostEvidence{
			EquipmentType:   "[weapon]",
			MaxDurability:   maxDurability,
			RepairPrice:     repairPrice,
			Grade:           grade,
			RepairCostRate:  0.08,
			QuickRepairRate: 1.5,
			UpgradeRates:    []float64{1, 1, 1},
		}, nil
	}
}

func TestOwnerRepairDeductsFormulaCostAndGoldAtomically(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestCharacter(t, ctx, repos, dnfrepo.CharacterRecord{CharacterID: "77", Stats: map[string]int64{"gold": 1000}})
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 5): {
				ItemID: 700,
				Count:  1,
				Extra: map[string]string{
					"item_kind":      "equipment",
					"durability":     "12",
					"max_durability": "20",
				},
			},
		},
	})

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	// 86JP formula: 6400*(20+5)/10=16000; 16000*0.08/20*8 = 512.
	result, err := owner.Repair(ctx, NewRepairCommand(alignedcmd.Request{
		AccountID:           "acc",
		SelectedCharacterID: 77,
	}, RepairEquipmentRequest{
		InvenType: listTypeMain,
		SlotIndex: 5,
	}), inventoryRepairTestResolver(20, 6400, 20))
	if err != nil {
		t.Fatalf("Repair error = %v", err)
	}
	if !result.Changed || result.Cost != 512 || result.UpdatedGold != 488 || result.FreeRepair {
		t.Fatalf("result = %+v, want cost=512 gold=488", result)
	}
	character, _, _ := repos.Character.Load(ctx, "77")
	if character.Stats["gold"] != 488 {
		t.Fatalf("persisted gold = %d, want 488", character.Stats["gold"])
	}
	loaded := loadTestInventory(t, ctx, repos, "77")
	if got := loaded.Slots[slotKey(listTypeMain, 5)].Extra["durability"]; got != "20" {
		t.Fatalf("durability = %q, want 20", got)
	}
}

func TestOwnerRepairInsufficientGoldRollsBack(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestCharacter(t, ctx, repos, dnfrepo.CharacterRecord{CharacterID: "77", Stats: map[string]int64{"gold": 100}})
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 5): {
				ItemID: 700,
				Count:  1,
				Extra: map[string]string{
					"item_kind":      "equipment",
					"durability":     "12",
					"max_durability": "20",
				},
			},
		},
	})

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	_, err = owner.Repair(ctx, NewRepairCommand(alignedcmd.Request{
		AccountID:           "acc",
		SelectedCharacterID: 77,
	}, RepairEquipmentRequest{
		InvenType: listTypeMain,
		SlotIndex: 5,
	}), inventoryRepairTestResolver(20, 6400, 20))
	if !errors.Is(err, ErrRepairGoldInsufficient) {
		t.Fatalf("Repair error = %v, want ErrRepairGoldInsufficient", err)
	}
	loaded := loadTestInventory(t, ctx, repos, "77")
	if got := loaded.Slots[slotKey(listTypeMain, 5)].Extra["durability"]; got != "12" {
		t.Fatalf("durability = %q, want unchanged 12", got)
	}
	character, _, _ := repos.Character.Load(ctx, "77")
	if character.Stats["gold"] != 100 {
		t.Fatalf("gold mutated = %d", character.Stats["gold"])
	}
}

func TestOwnerRepairPersonalCargoPersistsWarehouseDurability(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestCharacter(t, ctx, repos, dnfrepo.CharacterRecord{CharacterID: "77", Stats: map[string]int64{"gold": 7654}})
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Warehouse: map[string]dnfrepo.ItemStack{
			slotKey(listTypePersonalCargo, 9): {
				ItemID: 701,
				Count:  1,
				Extra: map[string]string{
					"item_kind":      "equipment",
					"durability":     "3",
					"max_durability": "15",
					"repair_gold":    "0",
				},
			},
		},
	})

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	result, err := owner.Repair(ctx, NewRepairCommand(alignedcmd.Request{
		AccountID:           "acc",
		SelectedCharacterID: 77,
	}, RepairEquipmentRequest{
		InvenType: listTypePersonalCargo,
		SlotIndex: 9,
	}), inventoryRepairTestResolver(15, 0, 0))
	if err != nil {
		t.Fatalf("Repair error = %v", err)
	}
	if !result.Changed || result.ListType != listTypePersonalCargo || result.OldDurability != 3 || result.NewDurability != 15 || result.UpdatedGold != 7654 {
		t.Fatalf("result = %+v", result)
	}

	loaded := loadTestInventory(t, ctx, repos, "77")
	if got := loaded.Warehouse[slotKey(listTypePersonalCargo, 9)].Extra["durability"]; got != "15" {
		t.Fatalf("warehouse durability = %q, want 15", got)
	}
}

func TestOwnerRepairRejectsEquipmentListUntilEquipmentOwnerExists(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeAvatar, 11): {
				ItemID: 700,
				Count:  1,
				Extra: map[string]string{
					"durability":     "12",
					"max_durability": "20",
					"repair_gold":    "0",
				},
			},
		},
	})

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	_, err = owner.Repair(ctx, NewRepairCommand(alignedcmd.Request{
		AccountID:           "acc",
		SelectedCharacterID: 77,
	}, RepairEquipmentRequest{
		InvenType: listTypeEquipment,
		SlotIndex: 11,
	}), nil)
	if !errors.Is(err, ErrRepairRequiresEquipmentOwner) {
		t.Fatalf("Repair error = %v, want ErrRepairRequiresEquipmentOwner", err)
	}

	loaded := loadTestInventory(t, ctx, repos, "77")
	if got := loaded.Slots[slotKey(listTypeAvatar, 11)].Extra["durability"]; got != "12" {
		t.Fatalf("durability = %q, want unchanged 12", got)
	}
}

func TestHandlerRepairUsesOwnerAndReturnsAck(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestCharacter(t, ctx, repos, dnfrepo.CharacterRecord{CharacterID: "77", Stats: map[string]int64{"gold": 12345}})
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 5): {
				ItemID: 700,
				Count:  1,
				Extra: map[string]string{
					"item_kind":      "equipment",
					"durability":     "12",
					"max_durability": "20",
					"repair_gold":    "0",
				},
			},
		},
	})

	got, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode: uint16(dnfenum.CmdPacketRepairEquipment),
		Body: []byte{
			listTypeMain,
			0x05, 0x00,
			0xFF, 0xFF,
			0x00, 0x00, 0x01,
		},
		AccountID:           "acc",
		SelectedCharacterID: 77,
		Repositories:        repos,
		RepairCostResolver:  inventoryRepairTestResolver(20, 0, 20),
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || !got.ResponseAllowed || got.Operation != "repair_equipment" {
		t.Fatalf("result = %+v", got)
	}
	if len(got.UpperResponses) != 1 {
		t.Fatalf("responses = %+v", got.UpperResponses)
	}
	ack := got.UpperResponses[0]
	if ack.MsgID != uint16(dnfenum.CmdPacketRepairEquipment) || ack.Classification != dnfproto.DefaultChannelClassification || !ack.AllowCodec {
		t.Fatalf("ack = %+v", ack)
	}
	wantBody := []byte{1, 0x39, 0x30, 0x00, 0x00, listTypeMain, 0x05, 0x00, 0x00, 0x00}
	if string(ack.Body) != string(wantBody) {
		t.Fatalf("ack body = % X, want % X", ack.Body, wantBody)
	}

	loaded := loadTestInventory(t, ctx, repos, "77")
	if got := loaded.Slots[slotKey(listTypeMain, 5)].Extra["durability"]; got != "20" {
		t.Fatalf("durability = %q, want 20", got)
	}
}

func TestHandlerRepairEquippedUsesEquipOwnerAndReturnsAck(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	raw := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 12, 0, 13, 14}
	saveTestCharacter(t, ctx, repos, dnfrepo.CharacterRecord{CharacterID: "77", Stats: map[string]int64{"gold": 888}})
	if err := repos.Equipment.Save(ctx, dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"11": {
				SlotIndex: 11,
				ItemID:    700,
				RawEntry:  raw,
				Extra: map[string]string{
					"max_durability": "20",
					"repair_gold":    "0",
				},
			},
		},
	}); err != nil {
		t.Fatalf("Save equipment error = %v", err)
	}

	got, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode: uint16(dnfenum.CmdPacketRepairEquipment),
		Body: []byte{
			listTypeEquipment,
			0x0B, 0x00,
			0xFF, 0xFF,
			0x00, 0x00, 0x00,
		},
		AccountID:           "acc",
		SelectedCharacterID: 77,
		Repositories:        repos,
		RepairCostResolver:  inventoryRepairTestResolver(20, 0, 20),
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || !got.ResponseAllowed || got.Operation != "repair_equipment" {
		t.Fatalf("result = %+v", got)
	}
	if len(got.UpperResponses) != 1 {
		t.Fatalf("responses = %+v", got.UpperResponses)
	}
	ack := got.UpperResponses[0]
	wantBody := []byte{1, 0x78, 0x03, 0x00, 0x00, listTypeEquipment, 0x0B, 0x00, 0x00, 0x00}
	if ack.MsgID != uint16(dnfenum.CmdPacketRepairEquipment) || string(ack.Body) != string(wantBody) {
		t.Fatalf("ack = %+v body=% X want=% X", ack, ack.Body, wantBody)
	}

	loaded, ok, err := repos.Equipment.Load(ctx, "77")
	if err != nil || !ok {
		t.Fatalf("Load equipment ok=%t err=%v", ok, err)
	}
	entry := loaded.Entries["11"]
	if got := uint16(entry.RawEntry[10]) | uint16(entry.RawEntry[11])<<8; got != 20 {
		t.Fatalf("raw durability = %d, want 20", got)
	}
	if got := entry.Extra["durability"]; got != "20" {
		t.Fatalf("extra durability = %q, want 20", got)
	}
}

func TestOwnerUpgradeReinforcePersistsPackedLevelAndConsumesMaterial(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestCharacter(t, ctx, repos, dnfrepo.CharacterRecord{CharacterID: "77", AccountID: "acc", Stats: map[string]int64{"gold": 12345}})
	raw := make([]byte, currentItemListEntrySize)
	raw[0x0A] = 2
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 9): {
				ItemID:   700,
				Count:    1,
				RawEntry: raw,
				Extra: map[string]string{
					"item_kind":      "equipment",
					"durability":     "45",
					"max_durability": "45",
				},
			},
			slotKey(listTypeMain, 121): {
				ItemID: 5000,
				Count:  3,
				Extra: map[string]string{
					"item_kind":   "stackable",
					"pvf_path":    "stackable/material/upgrade.mat",
					"stack_limit": "999",
				},
			},
		},
	})
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	result, err := owner.Upgrade(ctx, NewUpgradeCommand(alignedcmd.Request{
		AccountID:           "acc",
		SelectedCharacterID: 77,
	}, UpgradeItemRequest{
		Mode:                    "reinforce",
		TargetSlotIndex:         9,
		TargetItemTemplateID:    700,
		MaterialSlotIndex:       121,
		OptionalTicketSlotIndex: -1,
	}))
	if err != nil {
		t.Fatalf("Upgrade error = %v", err)
	}
	if !result.Success || !result.Changed || result.OldLevel != 2 || result.NewLevel != 3 || result.MaterialRemainingStackCount != 2 {
		t.Fatalf("result = %+v", result)
	}
	loaded := loadTestInventory(t, ctx, repos, "77")
	target := loaded.Slots[slotKey(listTypeMain, 9)]
	if got := target.Extra["reinforce"]; got != "3" {
		t.Fatalf("reinforce extra = %q, want 3", got)
	}
	if got := target.RawEntry[0x0A]; got != 3 {
		t.Fatalf("raw upgrade byte = %d, want 3", got)
	}
	if got := loaded.Slots[slotKey(listTypeMain, 121)].Count; got != 2 {
		t.Fatalf("material count = %d, want 2", got)
	}
}

func TestOwnerUpgradeReinforceConsumesAccountSharedColorlessCubes(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestCharacter(t, ctx, repos, dnfrepo.CharacterRecord{CharacterID: "77", AccountID: "acc", Stats: map[string]int64{"gold": 12345}})
	if err := repos.AccountInventory.Save(ctx, dnfrepo.AccountInventoryRecord{
		AccountID: "acc",
		Slots: map[string]dnfrepo.ItemStack{
			dnfrepo.AccountSharedInventorySlotKey(358): {ItemID: 3037, Count: 12},
		},
	}); err != nil {
		t.Fatal(err)
	}
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 9): {
				ItemID: 700,
				Count:  1,
				Extra: map[string]string{
					"item_kind":      "equipment",
					"durability":     "45",
					"max_durability": "45",
				},
			},
		},
	})
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	cmd := NewUpgradeCommand(alignedcmd.Request{
		AccountID:           "acc",
		SelectedCharacterID: 77,
	}, UpgradeItemRequest{
		Mode:                    "reinforce",
		TargetSlotIndex:         9,
		TargetItemTemplateID:    700,
		MaterialSlotIndex:       -1,
		OptionalTicketSlotIndex: -1,
	})
	cmd.UpgradeMaterialItemID = 3037
	cmd.UpgradeMaterialCount = 10
	result, err := owner.Upgrade(ctx, cmd)
	if err != nil {
		t.Fatalf("Upgrade error = %v", err)
	}
	if !result.Success || !result.Changed || result.NewLevel != 1 || result.MaterialRemainingStackCount != 2 {
		t.Fatalf("result = %+v", result)
	}
	account, ok, err := repos.AccountInventory.Load(ctx, "acc")
	if err != nil || !ok {
		t.Fatalf("account inventory ok=%t err=%v", ok, err)
	}
	if got := account.Slots[dnfrepo.AccountSharedInventorySlotKey(358)].Count; got != 2 {
		t.Fatalf("colorless cube count = %d, want 2", got)
	}
	loaded := loadTestInventory(t, ctx, repos, "77")
	if got := loaded.Slots[slotKey(listTypeMain, 9)].Extra["reinforce"]; got != "1" {
		t.Fatalf("reinforce extra = %q, want 1", got)
	}
}

func TestOwnerUpgradeReinforceRejectsInsufficientAccountSharedCubes(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestCharacter(t, ctx, repos, dnfrepo.CharacterRecord{CharacterID: "77", AccountID: "acc", Stats: map[string]int64{"gold": 12345}})
	if err := repos.AccountInventory.Save(ctx, dnfrepo.AccountInventoryRecord{
		AccountID: "acc",
		Slots: map[string]dnfrepo.ItemStack{
			dnfrepo.AccountSharedInventorySlotKey(358): {ItemID: 3037, Count: 9},
		},
	}); err != nil {
		t.Fatal(err)
	}
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 9): {
				ItemID: 700,
				Count:  1,
				Extra: map[string]string{
					"item_kind":      "equipment",
					"durability":     "45",
					"max_durability": "45",
				},
			},
		},
	})
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	cmd := NewUpgradeCommand(alignedcmd.Request{
		AccountID:           "acc",
		SelectedCharacterID: 77,
	}, UpgradeItemRequest{
		Mode:                    "reinforce",
		TargetSlotIndex:         9,
		TargetItemTemplateID:    700,
		MaterialSlotIndex:       -1,
		OptionalTicketSlotIndex: -1,
	})
	cmd.UpgradeMaterialItemID = 3037
	cmd.UpgradeMaterialCount = 10
	result, err := owner.Upgrade(ctx, cmd)
	if err != nil {
		t.Fatalf("Upgrade error = %v", err)
	}
	if result.Success || result.Changed || result.ErrorCode != upgradeErrorInvalidMaterial {
		t.Fatalf("result = %+v, want material failure", result)
	}
	account, ok, err := repos.AccountInventory.Load(ctx, "acc")
	if err != nil || !ok {
		t.Fatalf("account inventory ok=%t err=%v", ok, err)
	}
	if got := account.Slots[dnfrepo.AccountSharedInventorySlotKey(358)].Count; got != 9 {
		t.Fatalf("colorless cube mutated to %d, want 9", got)
	}
	loaded := loadTestInventory(t, ctx, repos, "77")
	if got := loaded.Slots[slotKey(listTypeMain, 9)].Extra["reinforce"]; got != "" {
		t.Fatalf("reinforce extra mutated to %q, want empty", got)
	}
}

func TestOwnerUpgradeAmplifyRequiresIdentifiedAmplifyState(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestCharacter(t, ctx, repos, dnfrepo.CharacterRecord{CharacterID: "77", AccountID: "acc", Stats: map[string]int64{"gold": 1}})
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 9): {
				ItemID: 700,
				Count:  1,
				Extra: map[string]string{
					"item_kind":      "equipment",
					"durability":     "45",
					"max_durability": "45",
				},
			},
		},
	})
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	result, err := owner.Upgrade(ctx, NewUpgradeCommand(alignedcmd.Request{
		AccountID:           "acc",
		SelectedCharacterID: 77,
	}, UpgradeItemRequest{
		Mode:                    "amplify",
		TargetSlotIndex:         9,
		TargetItemTemplateID:    700,
		MaterialSlotIndex:       -1,
		OptionalTicketSlotIndex: -1,
	}))
	if err != nil {
		t.Fatalf("Upgrade error = %v", err)
	}
	if result.Success || result.ErrorCode != upgradeErrorWrongMode {
		t.Fatalf("result = %+v, want wrong-mode failure", result)
	}

	loaded := loadTestInventory(t, ctx, repos, "77")
	stack := loaded.Slots[slotKey(listTypeMain, 9)]
	stack.Extra["amplify_type"] = "3"
	stack.Extra["amplify_value"] = "12"
	loaded.Slots[slotKey(listTypeMain, 9)] = stack
	saveTestInventory(t, ctx, repos, loaded)

	result, err = owner.Upgrade(ctx, NewUpgradeCommand(alignedcmd.Request{
		AccountID:           "acc",
		SelectedCharacterID: 77,
	}, UpgradeItemRequest{
		Mode:                    "amplify",
		TargetSlotIndex:         9,
		TargetItemTemplateID:    700,
		MaterialSlotIndex:       -1,
		OptionalTicketSlotIndex: -1,
	}))
	if err != nil {
		t.Fatalf("Upgrade amplify error = %v", err)
	}
	if !result.Success || result.NewLevel != 1 {
		t.Fatalf("amplify result = %+v", result)
	}
	loaded = loadTestInventory(t, ctx, repos, "77")
	if got := loaded.Slots[slotKey(listTypeMain, 9)].Extra["reinforce"]; got != "1" {
		t.Fatalf("amplify upgrade level = %q, want 1", got)
	}
}

func TestHandlerUpgradeReturnsOp50AckAndSlotUpdate(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestCharacter(t, ctx, repos, dnfrepo.CharacterRecord{CharacterID: "77", AccountID: "acc", Stats: map[string]int64{"gold": 999}})
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 9): {
				ItemID: 700,
				Count:  1,
				Extra: map[string]string{
					"item_kind":      "equipment",
					"durability":     "45",
					"max_durability": "45",
				},
			},
			slotKey(listTypeMain, 121): {ItemID: 5000, Count: 2, Extra: map[string]string{"item_kind": "stackable", "pvf_path": "stackable/material/upgrade.mat", "stack_limit": "999"}},
		},
	})

	body := make([]byte, 16)
	binary.LittleEndian.PutUint16(body[0:2], 0)
	binary.LittleEndian.PutUint16(body[2:4], 9)
	binary.LittleEndian.PutUint32(body[4:8], 700)
	binary.LittleEndian.PutUint16(body[8:10], 121)
	binary.LittleEndian.PutUint16(body[10:12], 0xFFFF)
	got, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketUpgradeItem),
		Body:                body,
		AccountID:           "acc",
		SelectedCharacterID: 77,
		Repositories:        repos,
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || !got.ResponseAllowed || got.Operation != "upgrade_item" || len(got.UpperResponses) < 2 {
		t.Fatalf("result = %+v", got)
	}
	ack := got.UpperResponses[0]
	wantAck := []byte{1, 0, 121, 0, 1, 0, 0, 0, 0xFF, 0xFF, 0, 0, 0, 1, 0, 9, 0, 0xFF, 0xFF}
	if ack.MsgID != uint16(dnfenum.CmdPacketUpgradeItem) || ack.Classification != dnfproto.DefaultChannelClassification || !ack.AllowCodec || !bytes.Equal(ack.Body, wantAck) {
		t.Fatalf("ack = %+v body=% X want=% X", ack, ack.Body, wantAck)
	}
	refresh := got.UpperResponses[1]
	if refresh.MsgID != msgItemListUpdate || refresh.Classification != 0 || len(refresh.Body) != 3+2*currentItemListEntrySize {
		t.Fatalf("refresh = %+v bodyLen=%d", refresh, len(refresh.Body))
	}
	if refresh.Body[0] != listTypeMain || binary.LittleEndian.Uint16(refresh.Body[1:3]) != 2 || refresh.Body[3+0x0A] != 1 {
		t.Fatalf("refresh body = % X", refresh.Body[:3+currentItemListEntrySize])
	}
}

func TestHandlerUpgradeUsesPolicyAccountSharedMaterial(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestCharacter(t, ctx, repos, dnfrepo.CharacterRecord{CharacterID: "77", AccountID: "acc", Stats: map[string]int64{"gold": 999}})
	if err := repos.AccountInventory.Save(ctx, dnfrepo.AccountInventoryRecord{
		AccountID: "acc",
		Slots: map[string]dnfrepo.ItemStack{
			dnfrepo.AccountSharedInventorySlotKey(358): {ItemID: 3037, Count: 12},
		},
	}); err != nil {
		t.Fatal(err)
	}
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 9): {
				ItemID: 700,
				Count:  1,
				Extra: map[string]string{
					"item_kind":      "equipment",
					"durability":     "45",
					"max_durability": "45",
				},
			},
		},
	})

	body := make([]byte, 16)
	binary.LittleEndian.PutUint16(body[0:2], 0)
	binary.LittleEndian.PutUint16(body[2:4], 9)
	binary.LittleEndian.PutUint32(body[4:8], 700)
	binary.LittleEndian.PutUint16(body[8:10], 0xFFFF)
	binary.LittleEndian.PutUint16(body[10:12], 0xFFFF)
	got, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketUpgradeItem),
		Body:                body,
		AccountID:           "acc",
		SelectedCharacterID: 77,
		Repositories:        repos,
		UpgradePolicyResolver: func(mode string, currentLevel int) (alignedcmd.UpgradePolicyResolution, error) {
			if mode != "reinforce" || currentLevel != 0 {
				t.Fatalf("policy args mode=%s level=%d", mode, currentLevel)
			}
			return alignedcmd.UpgradePolicyResolution{
				SuccessWeight:  currentUpgradeSuccessBase,
				MaterialItemID: 3037,
				MaterialCount:  10,
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || !got.ResponseAllowed || got.Operation != "upgrade_item" || len(got.UpperResponses) < 2 {
		t.Fatalf("result = %+v", got)
	}
	account, ok, err := repos.AccountInventory.Load(ctx, "acc")
	if err != nil || !ok {
		t.Fatalf("account inventory ok=%t err=%v", ok, err)
	}
	if got := account.Slots[dnfrepo.AccountSharedInventorySlotKey(358)].Count; got != 2 {
		t.Fatalf("colorless cube count = %d, want 2", got)
	}
}

func TestHandlerUpgradeUsesPolicyAccountSharedMaterialWhenClientSendsSharedSlot(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestCharacter(t, ctx, repos, dnfrepo.CharacterRecord{CharacterID: "77", AccountID: "acc", Stats: map[string]int64{"gold": 999}})
	if err := repos.AccountInventory.Save(ctx, dnfrepo.AccountInventoryRecord{
		AccountID: "acc",
		Slots: map[string]dnfrepo.ItemStack{
			dnfrepo.AccountSharedInventorySlotKey(358): {ItemID: 3037, Count: 12},
		},
	}); err != nil {
		t.Fatal(err)
	}
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 9): {
				ItemID: 700,
				Count:  1,
				Extra: map[string]string{
					"item_kind":      "equipment",
					"durability":     "45",
					"max_durability": "45",
				},
			},
		},
	})

	body := make([]byte, 16)
	binary.LittleEndian.PutUint16(body[0:2], 0)
	binary.LittleEndian.PutUint16(body[2:4], 9)
	binary.LittleEndian.PutUint32(body[4:8], 700)
	binary.LittleEndian.PutUint16(body[8:10], 358)
	binary.LittleEndian.PutUint16(body[10:12], 0xFFFF)
	got, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketUpgradeItem),
		Body:                body,
		AccountID:           "acc",
		SelectedCharacterID: 77,
		Repositories:        repos,
		UpgradePolicyResolver: func(mode string, currentLevel int) (alignedcmd.UpgradePolicyResolution, error) {
			if mode != "reinforce" || currentLevel != 0 {
				t.Fatalf("policy args mode=%s level=%d", mode, currentLevel)
			}
			return alignedcmd.UpgradePolicyResolution{
				SuccessWeight:  currentUpgradeSuccessBase,
				MaterialItemID: 3037,
				MaterialCount:  10,
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || !got.ResponseAllowed || got.Operation != "upgrade_item" || len(got.UpperResponses) < 2 {
		t.Fatalf("result = %+v", got)
	}
	account, ok, err := repos.AccountInventory.Load(ctx, "acc")
	if err != nil || !ok {
		t.Fatalf("account inventory ok=%t err=%v", ok, err)
	}
	if got := account.Slots[dnfrepo.AccountSharedInventorySlotKey(358)].Count; got != 2 {
		t.Fatalf("colorless cube count = %d, want 2", got)
	}
}

func TestHandlerUpgradeSkipsTicketResolverForAccountSharedMaterialSlot(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestCharacter(t, ctx, repos, dnfrepo.CharacterRecord{CharacterID: "77", AccountID: "acc", Stats: map[string]int64{"gold": 999}})
	if err := repos.AccountInventory.Save(ctx, dnfrepo.AccountInventoryRecord{
		AccountID: "acc",
		Slots: map[string]dnfrepo.ItemStack{
			dnfrepo.AccountSharedInventorySlotKey(358): {ItemID: 3037, Count: 12},
		},
	}); err != nil {
		t.Fatal(err)
	}
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 9): {
				ItemID: 700,
				Count:  1,
				Extra: map[string]string{
					"item_kind":      "equipment",
					"durability":     "45",
					"max_durability": "45",
				},
			},
		},
	})

	body := make([]byte, 16)
	binary.LittleEndian.PutUint16(body[0:2], 0)
	binary.LittleEndian.PutUint16(body[2:4], 9)
	binary.LittleEndian.PutUint32(body[4:8], 700)
	binary.LittleEndian.PutUint16(body[8:10], 358)
	binary.LittleEndian.PutUint16(body[10:12], 0xFFFF)
	ticketCalled := false
	policyCalled := false
	got, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketUpgradeItem),
		Body:                body,
		AccountID:           "acc",
		SelectedCharacterID: 77,
		Repositories:        repos,
		UpgradeTicketResolver: func(materialItemID int64, targetItemID int64) (alignedcmd.UpgradeTicketResolution, error) {
			ticketCalled = true
			return alignedcmd.UpgradeTicketResolution{}, nil
		},
		UpgradePolicyResolver: func(mode string, currentLevel int) (alignedcmd.UpgradePolicyResolution, error) {
			policyCalled = true
			if mode != "reinforce" || currentLevel != 0 {
				t.Fatalf("policy args mode=%s level=%d", mode, currentLevel)
			}
			return alignedcmd.UpgradePolicyResolution{
				SuccessWeight:  currentUpgradeSuccessBase,
				MaterialItemID: 3037,
				MaterialCount:  10,
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if ticketCalled {
		t.Fatalf("ticket resolver was called for account-shared material slot")
	}
	if !policyCalled {
		t.Fatalf("policy resolver was not called")
	}
	if !got.Handled || !got.ResponseAllowed || got.Operation != "upgrade_item" || len(got.UpperResponses) < 2 {
		t.Fatalf("result = %+v", got)
	}
	account, ok, err := repos.AccountInventory.Load(ctx, "acc")
	if err != nil || !ok {
		t.Fatalf("account inventory ok=%t err=%v", ok, err)
	}
	if got := account.Slots[dnfrepo.AccountSharedInventorySlotKey(358)].Count; got != 2 {
		t.Fatalf("colorless cube count = %d, want 2", got)
	}
}

func TestHandlerUpgradeLogsPolicyResolverError(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestCharacter(t, ctx, repos, dnfrepo.CharacterRecord{CharacterID: "77", AccountID: "acc", Stats: map[string]int64{"gold": 999}})
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 9): {
				ItemID: 700,
				Count:  1,
				Extra: map[string]string{
					"item_kind":      "equipment",
					"durability":     "45",
					"max_durability": "45",
				},
			},
		},
	})

	body := make([]byte, 16)
	binary.LittleEndian.PutUint16(body[0:2], 0)
	binary.LittleEndian.PutUint16(body[2:4], 9)
	binary.LittleEndian.PutUint32(body[4:8], 700)
	binary.LittleEndian.PutUint16(body[8:10], 358)
	binary.LittleEndian.PutUint16(body[10:12], 0xFFFF)
	got, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketUpgradeItem),
		Body:                body,
		AccountID:           "acc",
		SelectedCharacterID: 77,
		Repositories:        repos,
		UpgradePolicyResolver: func(mode string, currentLevel int) (alignedcmd.UpgradePolicyResolution, error) {
			return alignedcmd.UpgradePolicyResolution{}, errors.New("upgrade table smoke failure")
		},
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || !got.ResponseAllowed || got.Operation != "upgrade_item" {
		t.Fatalf("result = %+v", got)
	}
	if !strings.Contains(got.Reason, `policyErr="upgrade table smoke failure"`) {
		t.Fatalf("reason = %q, want policyErr", got.Reason)
	}
	if len(got.UpperResponses) != 1 || got.UpperResponses[0].MsgID != uint16(dnfenum.CmdPacketUpgradeItem) {
		t.Fatalf("responses = %+v", got.UpperResponses)
	}
}

func TestHandlerUpgradeMissingPolicyResolverFailsClosedBeforeSharedSlotFallback(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestCharacter(t, ctx, repos, dnfrepo.CharacterRecord{CharacterID: "77", AccountID: "acc", Stats: map[string]int64{"gold": 999}})
	if err := repos.AccountInventory.Save(ctx, dnfrepo.AccountInventoryRecord{
		AccountID: "acc",
		Slots: map[string]dnfrepo.ItemStack{
			dnfrepo.AccountSharedInventorySlotKey(358): {ItemID: 3037, Count: 12},
		},
	}); err != nil {
		t.Fatal(err)
	}
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 9): {
				ItemID: 700,
				Count:  1,
				Extra: map[string]string{
					"item_kind":      "equipment",
					"durability":     "45",
					"max_durability": "45",
				},
			},
		},
	})

	body := make([]byte, 16)
	binary.LittleEndian.PutUint16(body[0:2], 0)
	binary.LittleEndian.PutUint16(body[2:4], 9)
	binary.LittleEndian.PutUint32(body[4:8], 700)
	binary.LittleEndian.PutUint16(body[8:10], 358)
	binary.LittleEndian.PutUint16(body[10:12], 0xFFFF)
	got, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketUpgradeItem),
		Body:                body,
		AccountID:           "acc",
		SelectedCharacterID: 77,
		Repositories:        repos,
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || !got.ResponseAllowed || got.Operation != "upgrade_item" {
		t.Fatalf("result = %+v", got)
	}
	if !strings.Contains(got.Reason, `policyErr="upgrade policy resolver missing"`) {
		t.Fatalf("reason = %q, want missing policy resolver", got.Reason)
	}
	account, ok, err := repos.AccountInventory.Load(ctx, "acc")
	if err != nil || !ok {
		t.Fatalf("account inventory ok=%t err=%v", ok, err)
	}
	if got := account.Slots[dnfrepo.AccountSharedInventorySlotKey(358)].Count; got != 12 {
		t.Fatalf("colorless cube count mutated to %d, want 12", got)
	}
}

func TestOwnerUseStackableRequiresPVFProvenWasteBeforeMutation(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 4): {ItemID: 500, Count: 2, Extra: map[string]string{"stackable": "true"}},
		},
	})

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	_, err = owner.UseStackable(ctx, NewUseStackableCommand(alignedcmd.Request{
		AccountID:           "acc",
		SelectedCharacterID: 77,
	}, UseStackableRequest{
		SlotIndex:     4,
		ListType:      listTypeMain,
		InstanceValue: 0x11223344,
		ItemCode:      500,
	}), nil)
	if !errors.Is(err, ErrUseStackableContractRequired) {
		t.Fatalf("UseStackable error = %v, want ErrUseStackableContractRequired", err)
	}
	loaded := loadTestInventory(t, ctx, repos, "77")
	if stack := loaded.Slots[slotKey(listTypeMain, 4)]; stack.ItemID != 500 || stack.Count != 2 {
		t.Fatalf("stack mutated: %+v", stack)
	}
}

func TestHandlerUseStackablePremiumMetadataBlocksBeforeMutation(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 4): {
				ItemID: 600,
				Count:  1,
				Extra:  map[string]string{"premium_type": "2", "premium_days": "7"},
			},
		},
	})

	got, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode: uint16(dnfenum.CmdPacketUseStackable),
		Body: []byte{
			0x04, 0x00,
			listTypeMain,
			0x11, 0x22, 0x33, 0x44,
			0x58, 0x02, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00,
		},
		AccountID:           "acc",
		SelectedCharacterID: 77,
		Repositories:        repos,
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || got.ResponseAllowed || got.Operation != "use_stackable" || len(got.UpperResponses) != 0 {
		t.Fatalf("result = %+v", got)
	}
	if !strings.Contains(got.Reason, "PVF-proven [waste] provenance and stable item identity") {
		t.Fatalf("reason = %q", got.Reason)
	}
	loaded := loadTestInventory(t, ctx, repos, "77")
	if stack := loaded.Slots[slotKey(listTypeMain, 4)]; stack.ItemID != 600 || stack.Count != 1 {
		t.Fatalf("premium stack mutated: %+v", stack)
	}
	if _, ok, err := repos.Account.Load(ctx, "acc"); err != nil || ok {
		t.Fatalf("premium account mutation ok=%t err=%v", ok, err)
	}
}

func TestHandlerUseStackablePVFProvenWasteDecrementsAndReturnsExactAck(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "78",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 5): {
				ItemID: 500,
				Count:  2,
				Extra: map[string]string{
					"item_kind":      "stackable",
					"pvf_path":       "stackable/tutorial/tutorial_0500.stk",
					"stackable_type": "[waste] 4",
				},
			},
		},
	})

	got, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode: uint16(dnfenum.CmdPacketUseStackable),
		Body: []byte{
			0x05, 0x00,
			listTypeMain,
			0x11, 0x22, 0x33, 0x44,
			0xF4, 0x01, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00,
		},
		AccountID:           "acc",
		SelectedCharacterID: 78,
		Repositories:        repos,
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || !got.ResponseAllowed || got.Operation != "use_stackable" || len(got.UpperResponses) != 1 ||
		len(got.ItemSlotRefreshes) != 1 ||
		got.ItemSlotRefreshes[0] != (alignedcmd.ItemSlotRefresh{ListType: listTypeMain, SlotIndex: 5}) {
		t.Fatalf("result = %+v", got)
	}
	response := got.UpperResponses[0]
	wantBody := []byte{1, 5, 0, listTypeMain, 0x11, 0x22, 0x33, 0x44, 0xF4, 0x01, 0, 0}
	if response.MsgID != uint16(dnfenum.CmdPacketUseStackable) ||
		response.Classification != dnfproto.DefaultChannelClassification ||
		!response.AllowCodec || string(response.Body) != string(wantBody) {
		t.Fatalf("response = %+v body=%x want=%x", response, response.Body, wantBody)
	}

	loaded := loadTestInventory(t, ctx, repos, "78")
	if stack := loaded.Slots[slotKey(listTypeMain, 5)]; stack.ItemID != 500 || stack.Count != 1 {
		t.Fatalf("stack mutated: %+v", stack)
	}
}

func TestHandlerUseReviveCoinConsumableAtomicallyIncrementsWallet(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "78",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 5): {
				ItemID: 42,
				Count:  2,
				Extra: map[string]string{
					"item_kind":      "stackable",
					"pvf_path":       "stackable/cash/coin_general.stk",
					"stackable_type": "[waste]",
				},
			},
			slotKey(listTypeMain, 1): {
				ItemID: 1,
				Count:  3,
				Extra: map[string]string{
					"amount_or_count": "3",
					"count":           "3",
					"value_a":         "3",
				},
			},
		},
	})

	got, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode: uint16(dnfenum.CmdPacketUseStackable),
		Body: []byte{
			0x05, 0x00,
			listTypeMain,
			0x11, 0x22, 0x33, 0x44,
			0x2A, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00,
		},
		AccountID:           "acc",
		SelectedCharacterID: 78,
		Repositories:        repos,
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || !got.ResponseAllowed || len(got.UpperResponses) != 1 ||
		len(got.PostActions) != 0 ||
		len(got.ItemSlotRefreshes) != 2 ||
		got.ItemSlotRefreshes[0] != (alignedcmd.ItemSlotRefresh{ListType: listTypeMain, SlotIndex: 5}) ||
		got.ItemSlotRefreshes[1] != (alignedcmd.ItemSlotRefresh{ListType: listTypeMain, SlotIndex: 1}) {
		t.Fatalf("result = %+v", got)
	}
	wantBody := []byte{1, 5, 0, listTypeMain, 0x11, 0x22, 0x33, 0x44, 0x2A, 0, 0, 0}
	if response := got.UpperResponses[0]; response.MsgID != uint16(dnfenum.CmdPacketUseStackable) ||
		string(response.Body) != string(wantBody) {
		t.Fatalf("response=%+v body=%x want=%x", response, response.Body, wantBody)
	}

	loaded := loadTestInventory(t, ctx, repos, "78")
	if stack := loaded.Slots[slotKey(listTypeMain, 5)]; stack.ItemID != 42 || stack.Count != 1 {
		t.Fatalf("consumable after=%+v", stack)
	}
	if wallet := loaded.Slots[slotKey(listTypeMain, 1)]; wallet.ItemID != 1 ||
		wallet.Count != 4 ||
		wallet.Extra["amount_or_count"] != "4" ||
		wallet.Extra["value_a"] != "4" {
		t.Fatalf("wallet after=%+v", wallet)
	}
}

func TestOwnerUseStackableDeletesLastPVFProvenWasteAtomically(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "79",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 3): {
				ItemID: 8474,
				Count:  1,
				Extra: map[string]string{
					"item_kind":      "stackable",
					"pvf_path":       "stackable/tutorial/tutorial_8474.stk",
					"stackable_type": "`[waste]` 4",
				},
			},
		},
	})
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	result, err := owner.UseStackable(ctx, NewUseStackableCommand(alignedcmd.Request{
		AccountID:           "acc",
		SelectedCharacterID: 79,
	}, UseStackableRequest{
		SlotIndex:     3,
		ListType:      listTypeMain,
		InstanceValue: 0x11223344,
		ItemCode:      8474,
	}), nil)
	if err != nil {
		t.Fatalf("UseStackable error = %v", err)
	}
	if !result.Changed || result.ItemID != 8474 || result.RemainingCount != 0 || result.PVFPath == "" {
		t.Fatalf("result = %+v", result)
	}
	loaded := loadTestInventory(t, ctx, repos, "79")
	if _, exists := loaded.Slots[slotKey(listTypeMain, 3)]; exists {
		t.Fatalf("consumed last stack still exists: %+v", loaded.Slots)
	}
}

func TestOwnerUseStackableRandomRewardItemConsumesAndGrantsAtomically(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "79",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 65): {ItemID: 490701730, Count: 2},
		},
	})
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	result, err := owner.UseStackable(ctx, NewUseStackableCommand(alignedcmd.Request{
		AccountID:           "acc",
		SelectedCharacterID: 79,
	}, UseStackableRequest{
		SlotIndex:     65,
		ListType:      listTypeMain,
		InstanceValue: 0x11223344,
		ItemCode:      490701730,
	}), nil, randomRewardItemTestResolver(490701730, 490701733))
	if err != nil {
		t.Fatalf("UseStackable error = %v", err)
	}
	if !result.Changed || result.RemainingCount != 1 || result.RandomRewardItemID != 490701733 || len(result.RandomRewardSlots) != 1 || result.RandomRewardSlots[0] != 66 {
		t.Fatalf("result = %+v", result)
	}
	loaded := loadTestInventory(t, ctx, repos, "79")
	if source := loaded.Slots[slotKey(listTypeMain, 65)]; source.ItemID != 490701730 || source.Count != 1 {
		t.Fatalf("source = %+v", source)
	}
	if reward := loaded.Slots[slotKey(listTypeMain, 66)]; reward.ItemID != 490701733 || reward.Count != 1 || reward.Extra["pvf_path"] != "stackable/cash/chn_490701733.stk" {
		t.Fatalf("reward = %+v", reward)
	}
}

func TestOwnerUseStackableRandomRewardItemRollsBackWhenNoResultSlot(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "79",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 65): {ItemID: 490701730, Count: 2},
			slotKey(listTypeMain, 66): {ItemID: 1, Count: 1},
		},
	})
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	_, err = owner.UseStackable(ctx, NewUseStackableCommand(alignedcmd.Request{
		AccountID:           "acc",
		SelectedCharacterID: 79,
	}, UseStackableRequest{SlotIndex: 65, ListType: listTypeMain, InstanceValue: 0x11223344, ItemCode: 490701730}), nil, randomRewardItemTestResolverInSlots(490701730, 490701733, 65, 66))
	if !errors.Is(err, ErrRandomRewardInventoryFull) {
		t.Fatalf("UseStackable error = %v", err)
	}
	loaded := loadTestInventory(t, ctx, repos, "79")
	if source := loaded.Slots[slotKey(listTypeMain, 65)]; source.ItemID != 490701730 || source.Count != 2 {
		t.Fatalf("source mutated after rollback = %+v", source)
	}
}

func randomRewardItemTestResolver(sourceID int64, rewardID int64) alignedcmd.RandomRewardItemResolver {
	return randomRewardItemTestResolverInSlots(sourceID, rewardID, 65, 120)
}

func randomRewardItemTestResolverInSlots(sourceID int64, rewardID int64, slotStart, slotEnd int16) alignedcmd.RandomRewardItemResolver {
	return func(candidate int64) (alignedcmd.RandomRewardItemResolution, error) {
		if candidate != sourceID {
			return alignedcmd.RandomRewardItemResolution{}, nil
		}
		return alignedcmd.RandomRewardItemResolution{
			SourceItemID:  sourceID,
			SourcePVFPath: "stackable/cash/chn_490701730.stk",
			StackableType: "random reward item",
			Outcomes: []alignedcmd.RandomRewardItemOutcome{{
				Weight: 1,
				Reward: alignedcmd.MagicBoxRewardItem{
					ItemID: rewardID, Kind: "stackable", TargetListType: listTypeMain, StackLimit: 1000,
					SlotStart: slotStart, SlotEnd: slotEnd, PVFPath: "stackable/cash/chn_490701733.stk",
				},
			}},
		}, nil
	}
}

func TestOwnerDeleteRejectsEquipmentLockedItem(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 2): {ItemID: 100, Count: 5, Extra: map[string]string{"equipment_lock_state": "1"}},
		},
	})

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	_, err = owner.Delete(ctx, NewDeleteCommand(alignedcmd.Request{
		AccountID:           "acc",
		SelectedCharacterID: 77,
	}, DeleteRequest{
		ListType:  listTypeMain,
		SlotIndex: 2,
		Count:     1,
	}))
	if !errors.Is(err, ErrItemLocked) {
		t.Fatalf("Delete error = %v, want ErrItemLocked", err)
	}

	loaded := loadTestInventory(t, ctx, repos, "77")
	if got := loaded.Slots[slotKey(listTypeMain, 2)].Count; got != 5 {
		t.Fatalf("count = %d, want unchanged 5", got)
	}
}

func TestOwnerSellRejectsEquipmentLockedItem(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 2): {ItemID: 100, Count: 5, Extra: map[string]string{"equipment_lock_state": "1", "sell_gold": "0"}},
		},
	})

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	_, err = owner.Sell(ctx, NewSellCommand(alignedcmd.Request{
		AccountID:           "acc",
		SelectedCharacterID: 77,
	}, DeleteOrSellRequest{
		HasListType: true,
		ListType:    listTypeMain,
		SlotIndex:   2,
		Count:       1,
	}))
	if !errors.Is(err, ErrItemLocked) {
		t.Fatalf("Sell error = %v, want ErrItemLocked", err)
	}

	loaded := loadTestInventory(t, ctx, repos, "77")
	if got := loaded.Slots[slotKey(listTypeMain, 2)].Count; got != 5 {
		t.Fatalf("count = %d, want unchanged 5", got)
	}
}

func TestOwnerPetInventoryDeleteSellSortFailClosedWithoutStateMutation(t *testing.T) {
	tests := []struct {
		name string
		run  func(context.Context, *Owner) error
	}{
		{
			name: "delete",
			run: func(ctx context.Context, owner *Owner) error {
				_, err := owner.Delete(ctx, NewDeleteCommand(alignedcmd.Request{SelectedCharacterID: 91}, DeleteRequest{
					ListType:  listTypePet,
					SlotIndex: 5,
					Count:     1,
				}))
				return err
			},
		},
		{
			name: "sell",
			run: func(ctx context.Context, owner *Owner) error {
				_, err := owner.Sell(ctx, NewSellCommand(alignedcmd.Request{SelectedCharacterID: 91}, DeleteOrSellRequest{
					HasListType: true,
					ListType:    listTypePet,
					SlotIndex:   5,
					Count:       1,
				}))
				return err
			},
		},
		{
			name: "sort",
			run: func(ctx context.Context, owner *Owner) error {
				_, err := owner.Sort(ctx, NewSortCommand(alignedcmd.Request{SelectedCharacterID: 91}, SortItemRequest{
					ListType: listTypePet,
					Category: 5,
				}))
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			repos := dnfrepomemory.NewMemoryGroup()
			savePetInventoryGuardFixture(t, ctx, repos)
			owner, err := NewOwner(repos)
			if err != nil {
				t.Fatalf("NewOwner error = %v", err)
			}

			if err := test.run(ctx, owner); !errors.Is(err, ErrPetInventoryOwnerRequired) {
				t.Fatalf("%s error = %v, want ErrPetInventoryOwnerRequired", test.name, err)
			}
			assertPetInventoryGuardFixtureUnchanged(t, ctx, repos)
		})
	}
}

func TestHandlerPetInventoryDeleteSellSortFailClosedWithoutStateMutation(t *testing.T) {
	tests := []struct {
		name      string
		opcode    dnfenum.CmdPacket
		body      []byte
		operation string
	}{
		{name: "delete", opcode: dnfenum.CmdPacketDeleteItem, body: []byte{listTypePet, 5, 0, 1, 0}, operation: "delete_item"},
		{name: "sell", opcode: dnfenum.CmdPacketSellItem, body: []byte{listTypePet, 5, 0, 1, 0}, operation: "sell_item"},
		{name: "sort", opcode: dnfenum.CmdPacketSortItem, body: []byte{listTypePet, 5, 0}, operation: "sort_item"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			repos := dnfrepomemory.NewMemoryGroup()
			savePetInventoryGuardFixture(t, ctx, repos)

			got, err := NewHandler().Handle(ctx, alignedcmd.Request{
				Opcode:              uint16(test.opcode),
				Body:                test.body,
				AccountID:           "acc-pet-guard",
				SelectedCharacterID: 91,
				Repositories:        repos,
			})
			if err != nil {
				t.Fatalf("Handle error = %v", err)
			}
			if !got.Handled || got.ResponseAllowed || got.Operation != test.operation || len(got.UpperResponses) != 0 {
				t.Fatalf("result = %+v", got)
			}
			if !strings.Contains(got.Reason, ErrPetInventoryOwnerRequired.Error()) {
				t.Fatalf("reason = %q, want %q", got.Reason, ErrPetInventoryOwnerRequired.Error())
			}
			assertPetInventoryGuardFixtureUnchanged(t, ctx, repos)
		})
	}
}

func savePetInventoryGuardFixture(t *testing.T, ctx context.Context, repos dnfrepo.Group) {
	t.Helper()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "91",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypePet, 5): {
				ItemID: 9001,
				Count:  1,
				Extra: map[string]string{
					"creature_serial_or_handle": "37",
					"sell_gold":                 "0",
				},
			},
		},
	})
	if err := repos.Pet.Save(ctx, dnfrepo.PetRecord{
		CharacterID: "91",
		Entries: map[string]dnfrepo.PetEntry{
			"37": {PetKey: "37", ItemID: 9001, SourceListType: listTypePet, SourceSlotIndex: 5, Name: "guard-pet", Level: 4, Exp: 12},
		},
		EquippedKey: "",
		TownDisplay: false,
	}); err != nil {
		t.Fatalf("Save pet error = %v", err)
	}
}

func assertPetInventoryGuardFixtureUnchanged(t *testing.T, ctx context.Context, repos dnfrepo.Group) {
	t.Helper()
	inventory := loadTestInventory(t, ctx, repos, "91")
	stack, found := inventory.Slots[slotKey(listTypePet, 5)]
	if !found || stack.ItemID != 9001 || stack.Count != 1 || stack.Extra["creature_serial_or_handle"] != "37" {
		t.Fatalf("pet inventory mutated: found=%t stack=%+v", found, stack)
	}
	petRecord, found, err := repos.Pet.Load(ctx, "91")
	if err != nil || !found {
		t.Fatalf("Load pet found=%t err=%v", found, err)
	}
	entry, found := petRecord.Entries["37"]
	if !found || entry.PetKey != "37" || entry.ItemID != 9001 || entry.SourceListType != listTypePet || entry.SourceSlotIndex != 5 || entry.Name != "guard-pet" || entry.Level != 4 || entry.Exp != 12 || petRecord.EquippedKey != "" || petRecord.TownDisplay {
		t.Fatalf("pet record mutated: record=%+v entry=%+v found=%t", petRecord, entry, found)
	}
}

func saveTestInventory(t *testing.T, ctx context.Context, repos dnfrepo.Group, record dnfrepo.InventoryRecord) {
	t.Helper()
	if err := repos.Inventory.Save(ctx, record); err != nil {
		t.Fatalf("Save inventory error = %v", err)
	}
}

func saveTestCharacter(t *testing.T, ctx context.Context, repos dnfrepo.Group, record dnfrepo.CharacterRecord) {
	t.Helper()
	if err := repos.Character.Save(ctx, record); err != nil {
		t.Fatalf("Save character error = %v", err)
	}
}

func loadTestInventory(t *testing.T, ctx context.Context, repos dnfrepo.Group, characterID string) dnfrepo.InventoryRecord {
	t.Helper()
	record, ok, err := repos.Inventory.Load(ctx, characterID)
	if err != nil {
		t.Fatalf("Load inventory error = %v", err)
	}
	if !ok {
		t.Fatalf("inventory %s not found", characterID)
	}
	return record
}

func currentMoveItemspaceBody(sourceList byte, sourceSlot int16, sourceInstance int32, moveCount int32, destinationList byte, destinationSlot int16, destinationInstance int32, destinationStack int32, actorIndex int32) []byte {
	body := make([]byte, 28)
	body[0] = sourceList
	binary.LittleEndian.PutUint16(body[1:3], uint16(sourceSlot))
	binary.LittleEndian.PutUint32(body[3:7], uint32(sourceInstance))
	binary.LittleEndian.PutUint32(body[7:11], uint32(moveCount))
	body[11] = destinationList
	binary.LittleEndian.PutUint16(body[12:14], uint16(destinationSlot))
	binary.LittleEndian.PutUint32(body[14:18], uint32(destinationInstance))
	binary.LittleEndian.PutUint32(body[18:22], uint32(destinationStack))
	binary.LittleEndian.PutUint32(body[22:26], uint32(actorIndex))
	return body
}

func testPetEquipRaw(slot int16, itemID int32, serial int32) []byte {
	raw := make([]byte, 47)
	raw[0] = byte(slot)
	binary.LittleEndian.PutUint32(raw[1:], uint32(itemID))
	binary.LittleEndian.PutUint32(raw[5:], uint32(serial))
	binary.LittleEndian.PutUint32(raw[24:], uint32(serial))
	return raw
}

func premiumContractTestResolver(itemID int64, premiumType int64, durationSeconds int64) alignedcmd.PremiumContractResolver {
	return func(candidate int64) (alignedcmd.PremiumContractResolution, error) {
		if candidate != itemID {
			return alignedcmd.PremiumContractResolution{}, nil
		}
		return alignedcmd.PremiumContractResolution{
			ItemID:          itemID,
			PremiumType:     premiumType,
			DurationSeconds: durationSeconds,
		}, nil
	}
}

func TestOwnerUseStackablePremiumContractActivatesAccountPremium(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Account.Save(ctx, dnfrepo.AccountRecord{AccountID: "acc"}); err != nil {
		t.Fatal(err)
	}
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 4): {ItemID: 30, Count: 2, Extra: map[string]string{"item_kind": "etc"}},
		},
	})

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	result, err := owner.UseStackable(ctx, NewUseStackableCommand(alignedcmd.Request{
		AccountID:           "acc",
		SelectedCharacterID: 77,
	}, UseStackableRequest{
		SlotIndex:     4,
		ListType:      listTypeMain,
		InstanceValue: 0x11223344,
		ItemCode:      30,
	}), premiumContractTestResolver(30, 22, 86400))
	if err != nil {
		t.Fatalf("UseStackable error = %v", err)
	}
	if !result.Changed || !result.PremiumActivated || result.PremiumType != 22 || result.RemainingCount != 1 {
		t.Fatalf("result = %+v", result)
	}
	if result.PremiumRemainingSeconds < 86390 || result.PremiumRemainingSeconds > 86400 {
		t.Fatalf("remaining seconds = %d, want ~86400", result.PremiumRemainingSeconds)
	}
	loaded := loadTestInventory(t, ctx, repos, "77")
	if stack := loaded.Slots[slotKey(listTypeMain, 4)]; stack.ItemID != 30 || stack.Count != 1 {
		t.Fatalf("stack = %+v, want item 30 count 1", stack)
	}
	account, ok, err := repos.Account.Load(ctx, "acc")
	if err != nil || !ok {
		t.Fatalf("account load ok=%t err=%v", ok, err)
	}
	if raw := account.Metadata["premium_expire_22"]; raw == "" {
		t.Fatalf("premium_expire_22 missing: %+v", account.Metadata)
	}
}

func TestOwnerUseStackablePremiumContractRenewalStacksOnExpiry(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	future := time.Now().Add(48 * time.Hour).Unix()
	if err := repos.Account.Save(ctx, dnfrepo.AccountRecord{
		AccountID: "acc",
		Metadata:  map[string]string{"premium_expire_27": strconv.FormatInt(future, 10)},
	}); err != nil {
		t.Fatal(err)
	}
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 4): {ItemID: 43, Count: 1},
		},
	})

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	result, err := owner.UseStackable(ctx, NewUseStackableCommand(alignedcmd.Request{
		AccountID:           "acc",
		SelectedCharacterID: 77,
	}, UseStackableRequest{
		SlotIndex:     4,
		ListType:      listTypeMain,
		InstanceValue: 0x11223344,
		ItemCode:      43,
	}), premiumContractTestResolver(43, 27, 86400))
	if err != nil {
		t.Fatalf("UseStackable error = %v", err)
	}
	account, ok, err := repos.Account.Load(ctx, "acc")
	if err != nil || !ok {
		t.Fatalf("account load ok=%t err=%v", ok, err)
	}
	got, err := strconv.ParseInt(account.Metadata["premium_expire_27"], 10, 64)
	if err != nil {
		t.Fatalf("premium_expire_27 = %q: %v", account.Metadata["premium_expire_27"], err)
	}
	if want := future + 86400; got != want {
		t.Fatalf("premium_expire_27 = %d, want %d (stacked on existing expiry)", got, want)
	}
	if result.PremiumRemainingSeconds <= 0 {
		t.Fatalf("remaining = %d", result.PremiumRemainingSeconds)
	}
	if _, exists := loadTestInventory(t, ctx, repos, "77").Slots[slotKey(listTypeMain, 4)]; exists {
		t.Fatal("consumed last contract item still exists")
	}
}

func TestOwnerUseStackablePremiumContractMismatchKeepsItemAndAccount(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Account.Save(ctx, dnfrepo.AccountRecord{AccountID: "acc"}); err != nil {
		t.Fatal(err)
	}
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 4): {ItemID: 31, Count: 1},
		},
	})

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	_, err = owner.UseStackable(ctx, NewUseStackableCommand(alignedcmd.Request{
		AccountID:           "acc",
		SelectedCharacterID: 77,
	}, UseStackableRequest{
		SlotIndex:     4,
		ListType:      listTypeMain,
		InstanceValue: 0x11223344,
		ItemCode:      30,
	}), premiumContractTestResolver(30, 22, 86400))
	if !errors.Is(err, ErrItemMismatch) {
		t.Fatalf("UseStackable error = %v, want ErrItemMismatch", err)
	}
	loaded := loadTestInventory(t, ctx, repos, "77")
	if stack := loaded.Slots[slotKey(listTypeMain, 4)]; stack.ItemID != 31 || stack.Count != 1 {
		t.Fatalf("stack mutated: %+v", stack)
	}
	account, ok, err := repos.Account.Load(ctx, "acc")
	if err != nil || !ok {
		t.Fatalf("account load ok=%t err=%v", ok, err)
	}
	if _, exists := account.Metadata["premium_expire_22"]; exists {
		t.Fatalf("premium metadata written on mismatch: %+v", account.Metadata)
	}
}

func TestOwnerUseStackablePremiumContractResolverErrorFailsClosed(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 4): {ItemID: 30, Count: 1},
		},
	})

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	_, err = owner.UseStackable(ctx, NewUseStackableCommand(alignedcmd.Request{
		AccountID:           "acc",
		SelectedCharacterID: 77,
	}, UseStackableRequest{
		SlotIndex:     4,
		ListType:      listTypeMain,
		InstanceValue: 0x11223344,
		ItemCode:      30,
	}), func(int64) (alignedcmd.PremiumContractResolution, error) {
		return alignedcmd.PremiumContractResolution{}, errors.New("PVF unavailable")
	})
	if !errors.Is(err, ErrPremiumContractResolveFailed) {
		t.Fatalf("UseStackable error = %v, want ErrPremiumContractResolveFailed", err)
	}
	loaded := loadTestInventory(t, ctx, repos, "77")
	if stack := loaded.Slots[slotKey(listTypeMain, 4)]; stack.ItemID != 30 || stack.Count != 1 {
		t.Fatalf("stack mutated: %+v", stack)
	}
}

func TestHandlerUseStackablePremiumContractReturnsAckAndPremiumNoti(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Account.Save(ctx, dnfrepo.AccountRecord{AccountID: "acc"}); err != nil {
		t.Fatal(err)
	}
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "78",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 5): {ItemID: 30, Count: 2},
		},
	})

	got, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode: uint16(dnfenum.CmdPacketUseStackable),
		Body: []byte{
			0x05, 0x00,
			listTypeMain,
			0x11, 0x22, 0x33, 0x44,
			0x1E, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00,
		},
		AccountID:               "acc",
		SelectedCharacterID:     78,
		Repositories:            repos,
		PremiumContractResolver: premiumContractTestResolver(30, 22, 86400),
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || !got.ResponseAllowed || got.Operation != "use_stackable" || len(got.UpperResponses) != 2 {
		t.Fatalf("result = %+v", got)
	}
	ack := got.UpperResponses[0]
	wantAck := []byte{1, 5, 0, listTypeMain, 0x11, 0x22, 0x33, 0x44, 0x1E, 0, 0, 0}
	if ack.MsgID != uint16(dnfenum.CmdPacketUseStackable) || !bytes.Equal(ack.Body, wantAck) {
		t.Fatalf("ack = msgID=%d body=% X want=% X", ack.MsgID, ack.Body, wantAck)
	}
	noti := got.UpperResponses[1]
	wantNoti := []byte{2, 0, 22, 0x80, 0x51, 0x01, 0, 0, 0, 0, 0}
	if noti.MsgID != 0x0042 || noti.Classification != 0 || !bytes.Equal(noti.Body, wantNoti) {
		t.Fatalf("noti = msgID=%d class=%d body=% X want class0 % X", noti.MsgID, noti.Classification, noti.Body, wantNoti)
	}
	account, ok, err := repos.Account.Load(ctx, "acc")
	if err != nil || !ok {
		t.Fatalf("account load ok=%t err=%v", ok, err)
	}
	if raw := account.Metadata["premium_expire_22"]; raw == "" {
		t.Fatalf("premium_expire_22 missing: %+v", account.Metadata)
	}
}

func TestHandlerRepairAllReturnsAckWithSlotFFFF(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestCharacter(t, ctx, repos, dnfrepo.CharacterRecord{CharacterID: "77", Stats: map[string]int64{"gold": 5000}})
	if err := repos.Equipment.Save(ctx, dnfrepo.EquipmentRecord{
		CharacterID: "77",
		Entries: map[string]dnfrepo.EquipmentEntry{
			"12": {SlotIndex: 12, ItemID: 700, RawEntry: []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 12, 0}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			"0:4": {ItemID: 701, Count: 1, Extra: map[string]string{"durability": "10"}},
		},
	})
	resolver := func(itemID int64) (alignedcmd.RepairCostEvidence, error) {
		equipmentType := "[amulet]"
		if itemID == 700 {
			equipmentType = "[weapon]"
		}
		return alignedcmd.RepairCostEvidence{
			EquipmentType:   equipmentType,
			MaxDurability:   20,
			RepairPrice:     6400,
			Grade:           20,
			RepairCostRate:  0.08,
			QuickRepairRate: 1.5,
			UpgradeRates:    []float64{1, 1, 1},
		}, nil
	}

	got, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode: uint16(dnfenum.CmdPacketRepairEquipment),
		Body: []byte{
			listTypeEquipment,
			0xFF, 0xFF,
			0xFF, 0xFF,
			0x00, 0x00, 0x00,
		},
		AccountID:           "acc",
		SelectedCharacterID: 77,
		Repositories:        repos,
		RepairCostResolver:  resolver,
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || !got.ResponseAllowed || got.Operation != "repair_equipment" || len(got.UpperResponses) != 1 {
		t.Fatalf("result = %+v", got)
	}
	// weapon 12 lost 8 -> 512; amulet slot4 lost 10 -> 640; total 1152,
	// gold 5000-1152 = 3848 = 0x0F08 little-endian.
	ack := got.UpperResponses[0]
	wantBody := []byte{1, 0x08, 0x0F, 0x00, 0x00, listTypeEquipment, 0xFF, 0xFF, 0x00, 0x00}
	if ack.MsgID != uint16(dnfenum.CmdPacketRepairEquipment) || !bytes.Equal(ack.Body, wantBody) {
		t.Fatalf("ack = %+v body=% X want=% X", ack, ack.Body, wantBody)
	}
	equipment, _, _ := repos.Equipment.Load(ctx, "77")
	if got := uint16(equipment.Entries["12"].RawEntry[10]) | uint16(equipment.Entries["12"].RawEntry[11])<<8; got != 20 {
		t.Fatalf("equipped durability = %d, want 20", got)
	}
	loaded := loadTestInventory(t, ctx, repos, "77")
	if loaded.Slots["0:4"].Extra["durability"] != "20" {
		t.Fatalf("quickbar durability = %q, want 20", loaded.Slots["0:4"].Extra["durability"])
	}
	character, _, _ := repos.Character.Load(ctx, "77")
	if character.Stats["gold"] != 3848 {
		t.Fatalf("gold = %d, want 3848", character.Stats["gold"])
	}
}

func TestSortSegmentUsesCurrentEXEPetConsumableRange(t *testing.T) {
	start, end, ok := sortSegment(listTypePet, 7)
	if !ok || start != 189 || end != 238 {
		t.Fatalf("pet-consumable segment=(%d,%d,%t), want (189,238,true)", start, end, ok)
	}
}
