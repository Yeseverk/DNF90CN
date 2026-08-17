package inventory

import (
	"context"
	"encoding/binary"
	"errors"
	"reflect"
	"strconv"
	"testing"
	"time"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestOwnerMoveAutoStacksIntoLowestTargetThatFitsPVFLimit(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 65): pvfMoveStack(3227, 3, 10),
			slotKey(listTypeMain, 70): pvfMoveStack(3227, 8, 10),
			slotKey(listTypeMain, 72): pvfMoveStack(3227, 4, 10),
			slotKey(listTypeMain, 75): pvfMoveStack(3227, 1, 10),
		},
	})
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}

	result, err := owner.Move(ctx, NewMoveCommand(alignedcmd.Request{
		AccountID:           "dnf:1",
		SelectedCharacterID: 77,
	}, MoveItemspaceRequest{
		SourceListType:       listTypeMain,
		SourceSlotIndex:      65,
		MoveCount:            3,
		DestinationListType:  listTypeMain,
		DestinationSlotIndex: 80,
	}))
	if err != nil {
		t.Fatalf("Move error = %v", err)
	}
	if result.Mode != "auto_stack" || result.MoveCount != 3 || !result.Changed {
		t.Fatalf("result = %+v", result)
	}
	if !reflect.DeepEqual(result.RefreshListTypes, []byte{listTypeMain}) {
		t.Fatalf("refresh list types = %v", result.RefreshListTypes)
	}

	loaded := loadTestInventory(t, ctx, repos, "77")
	if _, exists := loaded.Slots[slotKey(listTypeMain, 65)]; exists {
		t.Fatalf("source still exists: %+v", loaded.Slots)
	}
	if _, exists := loaded.Slots[slotKey(listTypeMain, 80)]; exists {
		t.Fatalf("literal empty destination was populated instead of merging: %+v", loaded.Slots)
	}
	if got := loaded.Slots[slotKey(listTypeMain, 70)].Count; got != 8 {
		t.Fatalf("full lower slot changed = %d", got)
	}
	if got := loaded.Slots[slotKey(listTypeMain, 72)].Count; got != 7 {
		t.Fatalf("lowest compatible slot count = %d, want 7", got)
	}
	if got := loaded.Slots[slotKey(listTypeMain, 75)].Count; got != 1 {
		t.Fatalf("later compatible slot changed = %d", got)
	}
}

func TestOwnerMoveAutoStackIntoPersonalCargoSynchronizesDurableCountFields(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	source := pvfMoveStack(3330, 5850, 0)
	source.RawEntry = make([]byte, currentItemListEntrySize)
	binary.LittleEndian.PutUint32(source.RawEntry[0x06:0x0A], 5850)
	source.Extra["amount"] = "5850"
	source.Extra["amount_or_count"] = "5850"
	target := pvfMoveStack(3330, 20470, 0)
	target.RawEntry = make([]byte, currentItemListEntrySize)
	binary.LittleEndian.PutUint32(target.RawEntry[0x06:0x0A], 20470)
	target.Extra["amount"] = "17970"
	target.Extra["amount_or_count"] = "17970"
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 7): source,
		},
		Warehouse: map[string]dnfrepo.ItemStack{
			slotKey(listTypePersonalCargo, 0): target,
		},
	})
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}

	result, err := owner.Move(ctx, NewMoveCommand(alignedcmd.Request{
		AccountID:           "dnf:1",
		SelectedCharacterID: 77,
	}, MoveItemspaceRequest{
		SourceListType:       listTypeMain,
		SourceSlotIndex:      7,
		MoveCount:            5850,
		DestinationListType:  listTypePersonalCargo,
		DestinationSlotIndex: 43,
	}))
	if err != nil {
		t.Fatalf("Move error = %v", err)
	}
	if result.Mode != "auto_stack" || result.MoveCount != 5850 || !result.Changed {
		t.Fatalf("result = %+v", result)
	}

	loaded := loadTestInventory(t, ctx, repos, "77")
	if _, exists := loaded.Slots[slotKey(listTypeMain, 7)]; exists {
		t.Fatalf("source still exists: %+v", loaded.Slots)
	}
	if _, exists := loaded.Warehouse[slotKey(listTypePersonalCargo, 43)]; exists {
		t.Fatalf("literal empty destination was populated: %+v", loaded.Warehouse)
	}
	merged := loaded.Warehouse[slotKey(listTypePersonalCargo, 0)]
	if merged.Count != 26320 {
		t.Fatalf("merged count = %d, want 26320", merged.Count)
	}
	if got := binary.LittleEndian.Uint32(merged.RawEntry[0x06:0x0A]); got != 26320 {
		t.Fatalf("merged raw count = %d, want 26320", got)
	}
	if merged.Extra["amount"] != "26320" || merged.Extra["amount_or_count"] != "26320" {
		t.Fatalf("merged count aliases = %+v", merged.Extra)
	}
}

func TestOwnerMoveAutoStackIsSymmetricForReverseOccupiedEndpoint(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 65): pvfMoveStack(3227, 2, 10),
		},
		Warehouse: map[string]dnfrepo.ItemStack{
			slotKey(listTypePersonalCargo, 9): pvfMoveStack(3227, 3, 10),
		},
	})
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}

	result, err := owner.Move(ctx, NewMoveCommand(alignedcmd.Request{
		AccountID:           "dnf:1",
		SelectedCharacterID: 77,
	}, MoveItemspaceRequest{
		SourceListType:       listTypeMain,
		SourceSlotIndex:      80,
		MoveCount:            3,
		DestinationListType:  listTypePersonalCargo,
		DestinationSlotIndex: 9,
	}))
	if err != nil {
		t.Fatalf("Move error = %v", err)
	}
	if result.Mode != "auto_stack_reverse" || result.MoveCount != 3 || !result.Changed {
		t.Fatalf("result = %+v", result)
	}
	if !reflect.DeepEqual(result.RefreshListTypes, []byte{listTypeMain, listTypePersonalCargo}) {
		t.Fatalf("refresh list types = %v", result.RefreshListTypes)
	}

	loaded := loadTestInventory(t, ctx, repos, "77")
	if got := loaded.Slots[slotKey(listTypeMain, 65)].Count; got != 5 {
		t.Fatalf("main target count = %d, want 5", got)
	}
	if _, exists := loaded.Warehouse[slotKey(listTypePersonalCargo, 9)]; exists {
		t.Fatalf("reverse source still exists: %+v", loaded.Warehouse)
	}
	if _, exists := loaded.Slots[slotKey(listTypeMain, 80)]; exists {
		t.Fatalf("literal empty endpoint was populated: %+v", loaded.Slots)
	}
}

func TestOwnerMoveToExplicitEmptyQuickSlotDoesNotAutoStackElsewhere(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 65): pvfMoveStack(3227, 3, 10),
			slotKey(listTypeMain, 66): pvfMoveStack(3227, 1, 10),
		},
	})
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}

	result, err := owner.Move(ctx, NewMoveCommand(alignedcmd.Request{
		AccountID:           "dnf:1",
		SelectedCharacterID: 77,
	}, MoveItemspaceRequest{
		SourceListType:       listTypeMain,
		SourceSlotIndex:      65,
		MoveCount:            3,
		DestinationListType:  listTypeMain,
		DestinationSlotIndex: 3,
	}))
	if err != nil {
		t.Fatalf("Move error = %v", err)
	}
	if result.Mode != "move" || result.MoveCount != 3 || !result.Changed {
		t.Fatalf("result = %+v", result)
	}

	loaded := loadTestInventory(t, ctx, repos, "77")
	if _, exists := loaded.Slots[slotKey(listTypeMain, 65)]; exists {
		t.Fatalf("source still exists: %+v", loaded.Slots)
	}
	if got := loaded.Slots[slotKey(listTypeMain, 3)]; got.ItemID != 3227 || got.Count != 3 {
		t.Fatalf("explicit quick slot stack = %+v", got)
	}
	if got := loaded.Slots[slotKey(listTypeMain, 66)].Count; got != 1 {
		t.Fatalf("ordinary compatible stack changed = %d", got)
	}
}

func TestOwnerReverseMoveToExplicitEmptyQuickSlotDoesNotAutoStackElsewhere(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 65): pvfMoveStack(3227, 3, 10),
			slotKey(listTypeMain, 66): pvfMoveStack(3227, 1, 10),
		},
	})
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}

	result, err := owner.Move(ctx, NewMoveCommand(alignedcmd.Request{
		AccountID:           "dnf:1",
		SelectedCharacterID: 77,
	}, MoveItemspaceRequest{
		SourceListType:       listTypeMain,
		SourceSlotIndex:      3,
		MoveCount:            3,
		DestinationListType:  listTypeMain,
		DestinationSlotIndex: 65,
	}))
	if err != nil {
		t.Fatalf("Move error = %v", err)
	}
	if result.Mode != "reverse_move" || result.MoveCount != 3 || !result.Changed {
		t.Fatalf("result = %+v", result)
	}

	loaded := loadTestInventory(t, ctx, repos, "77")
	if _, exists := loaded.Slots[slotKey(listTypeMain, 65)]; exists {
		t.Fatalf("occupied endpoint still exists: %+v", loaded.Slots)
	}
	if got := loaded.Slots[slotKey(listTypeMain, 3)]; got.ItemID != 3227 || got.Count != 3 {
		t.Fatalf("explicit quick slot stack = %+v", got)
	}
	if got := loaded.Slots[slotKey(listTypeMain, 66)].Count; got != 1 {
		t.Fatalf("ordinary compatible stack changed = %d", got)
	}
}

func TestOwnerMoveAutoStackDoesNotCrossAccountSharedOwnership(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 65):  pvfMoveStack(3227, 2, 10),
			slotKey(listTypeMain, 354): pvfMoveStack(3227, 1, 10),
		},
	})
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}

	result, err := owner.Move(ctx, NewMoveCommand(alignedcmd.Request{
		AccountID:           "dnf:1",
		SelectedCharacterID: 77,
	}, MoveItemspaceRequest{
		SourceListType:       listTypeMain,
		SourceSlotIndex:      65,
		MoveCount:            2,
		DestinationListType:  listTypeMain,
		DestinationSlotIndex: 80,
	}))
	if err != nil {
		t.Fatalf("Move error = %v", err)
	}
	if result.Mode != "move" || len(result.RefreshListTypes) != 0 {
		t.Fatalf("result = %+v", result)
	}
	loaded := loadTestInventory(t, ctx, repos, "77")
	if got := loaded.Slots[slotKey(listTypeMain, 354)].Count; got != 1 {
		t.Fatalf("account-shared candidate changed = %d", got)
	}
	if got := loaded.Slots[slotKey(listTypeMain, 80)].Count; got != 2 {
		t.Fatalf("literal destination count = %d, want 2", got)
	}
}

func TestCanStackRequiresMatchingPVFIdentityOwnershipExpiryAndCapacity(t *testing.T) {
	baseSource := pvfMoveStack(3227, 2, 10)
	baseSource.Extra["pvf_path"] = "STACKABLE\\Material\\test_3227.stk"
	baseDestination := pvfMoveStack(3227, 8, 10)

	tests := []struct {
		name        string
		mutate      func(*dnfrepo.ItemStack, *dnfrepo.ItemStack)
		moveCount   int64
		wantAllowed bool
	}{
		{name: "exact limit", moveCount: 2, wantAllowed: true},
		{name: "over limit", moveCount: 3, mutate: func(source, _ *dnfrepo.ItemStack) { source.Count = 3 }, wantAllowed: false},
		{name: "missing item kind", moveCount: 2, mutate: func(source, _ *dnfrepo.ItemStack) { delete(source.Extra, "item_kind") }},
		{name: "missing PVF path", moveCount: 2, mutate: func(source, _ *dnfrepo.ItemStack) { delete(source.Extra, "pvf_path") }},
		{name: "different PVF path", moveCount: 2, mutate: func(_, destination *dnfrepo.ItemStack) {
			destination.Extra["pvf_path"] = "stackable/material/other.stk"
		}},
		{name: "different stack limit", moveCount: 2, mutate: func(_, destination *dnfrepo.ItemStack) { destination.Extra["stack_limit"] = "11" }},
		{name: "different bind", moveCount: 2, mutate: func(_, destination *dnfrepo.ItemStack) { destination.Bind = true }},
		{name: "different expiry", moveCount: 2, mutate: func(_, destination *dnfrepo.ItemStack) { destination.ExpireAt = time.Unix(1234, 0) }},
		{name: "source count exceeds PVF limit", moveCount: 2, mutate: func(source, _ *dnfrepo.ItemStack) { source.Count = 11 }},
		{name: "PVF omitted stack limit is uncapped", moveCount: 2, mutate: func(source, destination *dnfrepo.ItemStack) {
			delete(source.Extra, "stack_limit")
			delete(destination.Extra, "stack_limit")
		}, wantAllowed: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := cloneStack(baseSource)
			destination := cloneStack(baseDestination)
			if test.mutate != nil {
				test.mutate(&source, &destination)
			}
			if got := canStack(source, destination, test.moveCount); got != test.wantAllowed {
				t.Fatalf("canStack = %t, want %t; source=%+v destination=%+v", got, test.wantAllowed, source, destination)
			}
		})
	}
}

func TestOwnerManualMoveStacksPVFUnlimitedRows(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	source := pvfMoveStack(2660671, 200, 10)
	destination := pvfMoveStack(2660671, 200, 10)
	delete(source.Extra, "stack_limit")
	delete(destination.Extra, "stack_limit")
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 100): source,
			slotKey(listTypeMain, 109): destination,
		},
	})
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}

	result, err := owner.Move(ctx, NewMoveCommand(alignedcmd.Request{
		AccountID:           "dnf:1",
		SelectedCharacterID: 77,
	}, MoveItemspaceRequest{
		SourceListType:       listTypeMain,
		SourceSlotIndex:      100,
		MoveCount:            0,
		DestinationListType:  listTypeMain,
		DestinationSlotIndex: 109,
	}))
	if err != nil {
		t.Fatalf("Move error = %v", err)
	}
	if result.Mode != "stack" || result.MoveCount != 200 || !result.Changed {
		t.Fatalf("result = %+v", result)
	}
	if !reflect.DeepEqual(result.RefreshListTypes, []byte{listTypeMain}) {
		t.Fatalf("refresh list types = %v", result.RefreshListTypes)
	}
	loaded := loadTestInventory(t, ctx, repos, "77")
	if _, found := loaded.Slots[slotKey(listTypeMain, 100)]; found {
		t.Fatalf("source row still exists: %+v", loaded.Slots)
	}
	if got := loaded.Slots[slotKey(listTypeMain, 109)].Count; got != 400 {
		t.Fatalf("destination count = %d, want 400", got)
	}
}

func TestHandlerManualMoveStacksCapturedPVFUnlimitedRows(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	source := pvfMoveStack(2660671, 200, 10)
	destination := pvfMoveStack(2660671, 200, 10)
	delete(source.Extra, "stack_limit")
	delete(destination.Extra, "stack_limit")
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "19",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 100): source,
			slotKey(listTypeMain, 109): destination,
		},
	})

	got, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketMoveItemspace),
		Body:                currentMoveItemspaceBody(listTypeMain, 100, 0x0028993F, 0, listTypeMain, 109, 0x0028993F, 0, -1),
		AccountID:           "dnf:1",
		SelectedCharacterID: 19,
		Repositories:        repos,
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || !got.ResponseAllowed || len(got.UpperResponses) != 2 {
		t.Fatalf("result = %+v", got)
	}
	wantACK := []byte{1, listTypeMain, 100, 0, 200, 0, 0, 0, listTypeMain, 109, 0}
	if ack := got.UpperResponses[0]; ack.MsgID != uint16(dnfenum.CmdPacketMoveItemspace) ||
		ack.Classification != 1 || !reflect.DeepEqual(ack.Body, wantACK) {
		t.Fatalf("ACK = %+v body=% X, want % X", ack, ack.Body, wantACK)
	}
	refresh := got.UpperResponses[1]
	if refresh.MsgID != msgItemListRefresh || refresh.Classification != 0 || refresh.Body[0] != listTypeMain {
		t.Fatalf("refresh = %+v", refresh)
	}
	if count := binary.LittleEndian.Uint16(refresh.Body[3:5]); count != 1 {
		t.Fatalf("refresh entry count = %d, want 1", count)
	}
	row := refresh.Body[5 : 5+currentItemListEntrySize]
	if slot := int16(binary.LittleEndian.Uint16(row[0:2])); slot != 109 ||
		binary.LittleEndian.Uint32(row[2:6]) != 2660671 ||
		binary.LittleEndian.Uint32(row[6:10]) != 400 {
		t.Fatalf("refresh row = % X", row[:16])
	}
}

func TestHandlerAutoStackSendsAckThenAuthoritativeMainItemList(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 65): pvfMoveStack(3227, 3, 10),
			slotKey(listTypeMain, 70): pvfMoveStack(3227, 4, 10),
		},
	})
	if err := repos.AccountInventory.Save(ctx, dnfrepo.AccountInventoryRecord{
		AccountID: "dnf:1",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 354): {ItemID: 3033, Count: 9},
		},
	}); err != nil {
		t.Fatalf("Save account inventory error = %v", err)
	}

	got, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketMoveItemspace),
		Body:                currentMoveItemspaceBody(listTypeMain, 65, 0, 0, listTypeMain, 80, 0, 0, -1),
		AccountID:           "dnf:1",
		SelectedCharacterID: 77,
		Repositories:        repos,
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || !got.ResponseAllowed || len(got.UpperResponses) != 2 {
		t.Fatalf("result = %+v", got)
	}
	ack := got.UpperResponses[0]
	if ack.MsgID != uint16(dnfenum.CmdPacketMoveItemspace) || ack.Classification != 1 {
		t.Fatalf("ACK header = %+v", ack)
	}
	wantACK := []byte{1, listTypeMain, 65, 0, 3, 0, 0, 0, listTypeMain, 80, 0}
	if !reflect.DeepEqual(ack.Body, wantACK) {
		t.Fatalf("ACK body = % X, want % X", ack.Body, wantACK)
	}

	refresh := got.UpperResponses[1]
	if refresh.MsgID != msgItemListRefresh || refresh.Classification != 0 || !refresh.AllowCodec {
		t.Fatalf("refresh header = %+v", refresh)
	}
	if len(refresh.Body) != 5+2*currentItemListEntrySize {
		t.Fatalf("refresh length = %d, want %d", len(refresh.Body), 5+2*currentItemListEntrySize)
	}
	if refresh.Body[0] != listTypeMain || binary.LittleEndian.Uint16(refresh.Body[3:5]) != 2 {
		t.Fatalf("refresh header body = % X", refresh.Body[:5])
	}
	first := refresh.Body[5 : 5+currentItemListEntrySize]
	second := refresh.Body[5+currentItemListEntrySize:]
	if slot := int16(binary.LittleEndian.Uint16(first[0:2])); slot != 70 ||
		binary.LittleEndian.Uint32(first[2:6]) != 3227 || binary.LittleEndian.Uint32(first[6:10]) != 7 {
		t.Fatalf("first row = % X", first[:16])
	}
	if slot := int16(binary.LittleEndian.Uint16(second[0:2])); slot != 354 ||
		binary.LittleEndian.Uint32(second[2:6]) != 3033 || binary.LittleEndian.Uint32(second[6:10]) != 9 {
		t.Fatalf("account row = % X", second[:16])
	}
}

func TestOwnerMoveAutoStackRollsBackWhenAuthoritativeListCannotBeBuilt(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 65): pvfMoveStack(3227, 3, 10),
			slotKey(listTypeMain, 70): pvfMoveStack(3227, 4, 10),
		},
	})
	wantErr := errors.New("account snapshot unavailable")
	repos.AccountInventory = failingMoveAccountInventoryRepository{err: wantErr}
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}

	_, err = owner.Move(ctx, NewMoveCommand(alignedcmd.Request{
		AccountID:           "dnf:1",
		SelectedCharacterID: 77,
	}, MoveItemspaceRequest{
		SourceListType:       listTypeMain,
		SourceSlotIndex:      65,
		MoveCount:            3,
		DestinationListType:  listTypeMain,
		DestinationSlotIndex: 80,
	}))
	if !errors.Is(err, wantErr) {
		t.Fatalf("Move error = %v, want %v", err, wantErr)
	}
	loaded := loadTestInventory(t, ctx, repos, "77")
	if got := loaded.Slots[slotKey(listTypeMain, 65)].Count; got != 3 {
		t.Fatalf("source changed despite snapshot failure = %d", got)
	}
	if got := loaded.Slots[slotKey(listTypeMain, 70)].Count; got != 4 {
		t.Fatalf("target changed despite snapshot failure = %d", got)
	}
	if _, exists := loaded.Slots[slotKey(listTypeMain, 80)]; exists {
		t.Fatalf("literal destination appeared despite rollback: %+v", loaded.Slots)
	}
}

func TestHandlerAccountSharedExplicitStackSendsAckThenSingleMergedList0Refresh(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 9): {ItemID: 700, Count: 1},
		},
	})
	if err := repos.AccountInventory.Save(ctx, dnfrepo.AccountInventoryRecord{
		AccountID: "dnf:1",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 354): pvfMoveStack(3227, 3, 10),
			slotKey(listTypeMain, 355): pvfMoveStack(3227, 4, 10),
		},
	}); err != nil {
		t.Fatalf("Save account inventory error = %v", err)
	}

	got, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketMoveItemspace),
		Body:                currentMoveItemspaceBody(listTypeMain, 354, 0, 3, listTypeMain, 355, 0, 0, -1),
		AccountID:           "dnf:1",
		SelectedCharacterID: 77,
		Repositories:        repos,
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || !got.ResponseAllowed || len(got.UpperResponses) != 2 {
		t.Fatalf("result = %+v", got)
	}
	wantACK := []byte{1, listTypeMain, 0x62, 0x01, 3, 0, 0, 0, listTypeMain, 0x63, 0x01}
	if ack := got.UpperResponses[0]; ack.MsgID != uint16(dnfenum.CmdPacketMoveItemspace) || ack.Classification != 1 || !reflect.DeepEqual(ack.Body, wantACK) {
		t.Fatalf("ACK = %+v body=% X, want % X", ack, ack.Body, wantACK)
	}

	refresh := got.UpperResponses[1]
	if refresh.MsgID != msgItemListRefresh || refresh.Classification != 0 || refresh.Body[0] != listTypeMain {
		t.Fatalf("refresh = %+v", refresh)
	}
	if count := binary.LittleEndian.Uint16(refresh.Body[3:5]); count != 2 {
		t.Fatalf("refresh count = %d body=% X", count, refresh.Body[:5])
	}
	if len(refresh.Body) != 5+2*currentItemListEntrySize {
		t.Fatalf("refresh length = %d", len(refresh.Body))
	}
	ordinary := refresh.Body[5 : 5+currentItemListEntrySize]
	shared := refresh.Body[5+currentItemListEntrySize:]
	if slot := int16(binary.LittleEndian.Uint16(ordinary[0:2])); slot != 9 ||
		binary.LittleEndian.Uint32(ordinary[2:6]) != 700 || binary.LittleEndian.Uint32(ordinary[6:10]) != 1 {
		t.Fatalf("ordinary row = % X", ordinary[:16])
	}
	if slot := int16(binary.LittleEndian.Uint16(shared[0:2])); slot != 355 ||
		binary.LittleEndian.Uint32(shared[2:6]) != 3227 || binary.LittleEndian.Uint32(shared[6:10]) != 7 {
		t.Fatalf("shared row = % X", shared[:16])
	}
}

func TestOwnerAccountSharedExplicitCrossListStackFreezesSourceThenDestinationSnapshots(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 9):   {ItemID: 700, Count: 1},
			slotKey(listTypeMain, 354): {ItemID: 9999, Count: 99},
		},
		Warehouse: map[string]dnfrepo.ItemStack{
			slotKey(listTypePersonalCargo, 8): pvfMoveStack(3227, 2, 10),
		},
	})
	if err := repos.AccountInventory.Save(ctx, dnfrepo.AccountInventoryRecord{
		AccountID: "dnf:1",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 354): pvfMoveStack(3227, 3, 10),
			slotKey(listTypeMain, 355): {ItemID: 3033, Count: 6},
		},
	}); err != nil {
		t.Fatalf("Save account inventory error = %v", err)
	}
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}

	result, err := owner.Move(ctx, NewMoveCommand(alignedcmd.Request{
		AccountID:           "dnf:1",
		SelectedCharacterID: 77,
	}, MoveItemspaceRequest{
		SourceListType:       listTypePersonalCargo,
		SourceSlotIndex:      8,
		MoveCount:            2,
		DestinationListType:  listTypeMain,
		DestinationSlotIndex: 354,
	}))
	if err != nil {
		t.Fatalf("Move error = %v", err)
	}
	if result.Mode != "stack" || !result.Changed || result.MoveCount != 2 {
		t.Fatalf("result = %+v", result)
	}
	if !reflect.DeepEqual(result.RefreshListTypes, []byte{listTypePersonalCargo, listTypeMain}) {
		t.Fatalf("refresh order = %v", result.RefreshListTypes)
	}
	if len(result.Refresh[listTypePersonalCargo]) != 0 {
		t.Fatalf("source snapshot = %+v, want empty post-merge list2", result.Refresh[listTypePersonalCargo])
	}
	main := result.Refresh[listTypeMain]
	if len(main) != 3 {
		t.Fatalf("main snapshot = %+v", main)
	}
	if got := main[slotKey(listTypeMain, 9)]; got.ItemID != 700 || got.Count != 1 {
		t.Fatalf("ordinary slot = %+v", got)
	}
	if got := main[slotKey(listTypeMain, 354)]; got.ItemID != 3227 || got.Count != 5 {
		t.Fatalf("overlaid shared destination = %+v", got)
	}
	if got := main[slotKey(listTypeMain, 355)]; got.ItemID != 3033 || got.Count != 6 {
		t.Fatalf("other shared slot = %+v", got)
	}
	for key := range main {
		if listType, _, ok := parseSlotKey(key); !ok || listType != listTypeMain {
			t.Fatalf("non-list0 key in main snapshot: %q", key)
		}
	}
}

func TestHandlerSameListPartialSplitSendsAckThenFinalMainRefresh(t *testing.T) {
	tests := []struct {
		name            string
		sourceSlot      int16
		destinationSlot int16
		shared          bool
		wantCounts      map[int16]uint32
	}{
		{
			name:            "ordinary split",
			sourceSlot:      65,
			destinationSlot: 80,
			wantCounts:      map[int16]uint32{10: 1, 65: 3, 80: 2},
		},
		{
			name:            "ordinary reverse split",
			sourceSlot:      80,
			destinationSlot: 65,
			wantCounts:      map[int16]uint32{10: 1, 65: 3, 80: 2},
		},
		{
			name:            "account shared split",
			sourceSlot:      354,
			destinationSlot: 9,
			shared:          true,
			wantCounts:      map[int16]uint32{9: 2, 10: 1, 354: 3},
		},
		{
			name:            "account shared reverse split",
			sourceSlot:      9,
			destinationSlot: 354,
			shared:          true,
			wantCounts:      map[int16]uint32{9: 2, 10: 1, 354: 3},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			repos := dnfrepomemory.NewMemoryGroup()
			characterSlots := map[string]dnfrepo.ItemStack{
				slotKey(listTypeMain, 10): {ItemID: 700, Count: 1},
			}
			accountSlots := make(map[string]dnfrepo.ItemStack)
			occupiedSlot := test.sourceSlot
			if test.name == "ordinary reverse split" || test.name == "account shared reverse split" {
				occupiedSlot = test.destinationSlot
			}
			if test.shared {
				accountSlots[slotKey(listTypeMain, occupiedSlot)] = pvfMoveStack(3227, 5, 10)
			} else {
				characterSlots[slotKey(listTypeMain, occupiedSlot)] = pvfMoveStack(3227, 5, 10)
			}
			saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{CharacterID: "77", Slots: characterSlots})
			if err := repos.AccountInventory.Save(ctx, dnfrepo.AccountInventoryRecord{AccountID: "dnf:1", Slots: accountSlots}); err != nil {
				t.Fatalf("Save account inventory error = %v", err)
			}

			got, err := NewHandler().Handle(ctx, alignedcmd.Request{
				Opcode:              uint16(dnfenum.CmdPacketMoveItemspace),
				Body:                currentMoveItemspaceBody(listTypeMain, test.sourceSlot, 0, 2, listTypeMain, test.destinationSlot, 0, 0, -1),
				AccountID:           "dnf:1",
				SelectedCharacterID: 77,
				Repositories:        repos,
			})
			if err != nil {
				t.Fatalf("Handle error = %v", err)
			}
			if !got.Handled || !got.ResponseAllowed || len(got.UpperResponses) != 2 {
				t.Fatalf("result = %+v", got)
			}
			ack := got.UpperResponses[0]
			if ack.MsgID != uint16(dnfenum.CmdPacketMoveItemspace) || ack.Classification != 1 ||
				int16(binary.LittleEndian.Uint16(ack.Body[2:4])) != test.sourceSlot ||
				binary.LittleEndian.Uint32(ack.Body[4:8]) != 2 ||
				int16(binary.LittleEndian.Uint16(ack.Body[9:11])) != test.destinationSlot {
				t.Fatalf("ACK = %+v body=% X", ack, ack.Body)
			}
			refresh := got.UpperResponses[1]
			if refresh.MsgID != msgItemListRefresh || refresh.Classification != 0 || refresh.Body[0] != listTypeMain {
				t.Fatalf("refresh = %+v", refresh)
			}
			count := int(binary.LittleEndian.Uint16(refresh.Body[3:5]))
			if count != len(test.wantCounts) || len(refresh.Body) != 5+count*currentItemListEntrySize {
				t.Fatalf("refresh count=%d len=%d body=% X", count, len(refresh.Body), refresh.Body[:5])
			}
			gotCounts := make(map[int16]uint32, count)
			for index := range count {
				offset := 5 + index*currentItemListEntrySize
				row := refresh.Body[offset : offset+currentItemListEntrySize]
				gotCounts[int16(binary.LittleEndian.Uint16(row[0:2]))] = binary.LittleEndian.Uint32(row[6:10])
			}
			if !reflect.DeepEqual(gotCounts, test.wantCounts) {
				t.Fatalf("refresh counts = %v, want %v", gotCounts, test.wantCounts)
			}
		})
	}
}

func TestOwnerCrossListPartialSplitDoesNotRequestRefresh(t *testing.T) {
	tests := []struct {
		name       string
		shared     bool
		sourceSlot int16
	}{
		{name: "ordinary", sourceSlot: 65},
		{name: "account shared", shared: true, sourceSlot: 354},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			repos := dnfrepomemory.NewMemoryGroup()
			characterSlots := make(map[string]dnfrepo.ItemStack)
			accountSlots := make(map[string]dnfrepo.ItemStack)
			if test.shared {
				accountSlots[slotKey(listTypeMain, test.sourceSlot)] = pvfMoveStack(3227, 5, 10)
			} else {
				characterSlots[slotKey(listTypeMain, test.sourceSlot)] = pvfMoveStack(3227, 5, 10)
			}
			saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{CharacterID: "77", Slots: characterSlots})
			if err := repos.AccountInventory.Save(ctx, dnfrepo.AccountInventoryRecord{AccountID: "dnf:1", Slots: accountSlots}); err != nil {
				t.Fatalf("Save account inventory error = %v", err)
			}
			owner, err := NewOwner(repos)
			if err != nil {
				t.Fatalf("NewOwner error = %v", err)
			}
			result, err := owner.Move(ctx, NewMoveCommand(alignedcmd.Request{
				AccountID:           "dnf:1",
				SelectedCharacterID: 77,
			}, MoveItemspaceRequest{
				SourceListType:       listTypeMain,
				SourceSlotIndex:      test.sourceSlot,
				MoveCount:            2,
				DestinationListType:  listTypePersonalCargo,
				DestinationSlotIndex: 8,
			}))
			if err != nil {
				t.Fatalf("Move error = %v", err)
			}
			if result.Mode != "split" || len(result.RefreshListTypes) != 0 || len(result.Refresh) != 0 {
				t.Fatalf("result = %+v", result)
			}
		})
	}
}

func TestHandlerAccountSharedExplicitStackTransactionFailureSendsNoSuccessAndRollsBack(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "77",
		Warehouse: map[string]dnfrepo.ItemStack{
			slotKey(listTypePersonalCargo, 8): pvfMoveStack(3227, 2, 10),
		},
	})
	if err := repos.AccountInventory.Save(ctx, dnfrepo.AccountInventoryRecord{
		AccountID: "dnf:1",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 354): pvfMoveStack(3227, 3, 10),
		},
	}); err != nil {
		t.Fatalf("Save account inventory error = %v", err)
	}
	wantErr := errors.New("forced account stack rollback")
	repos.AccountItems = rollbackAccountCharacterItemsAfterApply{base: repos.AccountItems, err: wantErr}

	got, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketMoveItemspace),
		Body:                currentMoveItemspaceBody(listTypePersonalCargo, 8, 0, 2, listTypeMain, 354, 0, 0, -1),
		AccountID:           "dnf:1",
		SelectedCharacterID: 77,
		Repositories:        repos,
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || got.ResponseAllowed || len(got.UpperResponses) != 0 {
		t.Fatalf("failure result = %+v", got)
	}
	character := loadTestInventory(t, ctx, repos, "77")
	if got := character.Warehouse[slotKey(listTypePersonalCargo, 8)].Count; got != 2 {
		t.Fatalf("source count after rollback = %d, want 2", got)
	}
	account, found, loadErr := repos.AccountInventory.Load(ctx, "dnf:1")
	if loadErr != nil || !found {
		t.Fatalf("Load account inventory found=%t err=%v", found, loadErr)
	}
	if got := account.Slots[slotKey(listTypeMain, 354)].Count; got != 3 {
		t.Fatalf("destination count after rollback = %d, want 3", got)
	}
}

func TestHandlerMainAndPersonalCargoExplicitStackUsesTwoSlotIncrementalRefresh(t *testing.T) {
	tests := []struct {
		name            string
		sourceList      byte
		sourceSlot      int16
		destinationList byte
		destinationSlot int16
	}{
		{
			name:            "backpack into personal cargo",
			sourceList:      listTypeMain,
			sourceSlot:      65,
			destinationList: listTypePersonalCargo,
			destinationSlot: 8,
		},
		{
			name:            "personal cargo into backpack",
			sourceList:      listTypePersonalCargo,
			sourceSlot:      8,
			destinationList: listTypeMain,
			destinationSlot: 65,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			repos := dnfrepomemory.NewMemoryGroup()
			record := dnfrepo.InventoryRecord{
				CharacterID: "77",
				Slots:       make(map[string]dnfrepo.ItemStack),
				Warehouse:   make(map[string]dnfrepo.ItemStack),
			}
			source := pvfMoveStack(3227, 2, 10)
			destination := pvfMoveStack(3227, 3, 10)
			if test.sourceList == listTypePersonalCargo {
				record.Warehouse[slotKey(test.sourceList, test.sourceSlot)] = source
			} else {
				record.Slots[slotKey(test.sourceList, test.sourceSlot)] = source
			}
			if test.destinationList == listTypePersonalCargo {
				record.Warehouse[slotKey(test.destinationList, test.destinationSlot)] = destination
			} else {
				record.Slots[slotKey(test.destinationList, test.destinationSlot)] = destination
			}
			saveTestInventory(t, ctx, repos, record)

			got, err := NewHandler().Handle(ctx, alignedcmd.Request{
				Opcode: uint16(dnfenum.CmdPacketMoveItemspace),
				Body: currentMoveItemspaceBody(
					test.sourceList,
					test.sourceSlot,
					0,
					2,
					test.destinationList,
					test.destinationSlot,
					0,
					3,
					-1,
				),
				AccountID:           "dnf:1",
				SelectedCharacterID: 77,
				Repositories:        repos,
			})
			if err != nil {
				t.Fatalf("Handle error = %v", err)
			}
			wantRefreshes := []alignedcmd.ItemSlotRefresh{
				{ListType: test.sourceList, SlotIndex: test.sourceSlot},
				{ListType: test.destinationList, SlotIndex: test.destinationSlot},
			}
			if !got.Handled || !got.ResponseAllowed ||
				len(got.UpperResponses) != 1 ||
				!reflect.DeepEqual(got.ItemSlotRefreshes, wantRefreshes) ||
				len(got.PostActions) != 0 {
				t.Fatalf("result = %+v, want refreshes=%+v", got, wantRefreshes)
			}
			ack := got.UpperResponses[0]
			if ack.MsgID != uint16(dnfenum.CmdPacketMoveItemspace) ||
				ack.Classification != 1 ||
				ack.Body[1] != test.sourceList ||
				int16(binary.LittleEndian.Uint16(ack.Body[2:4])) != test.sourceSlot ||
				binary.LittleEndian.Uint32(ack.Body[4:8]) != 2 ||
				ack.Body[8] != test.destinationList ||
				int16(binary.LittleEndian.Uint16(ack.Body[9:11])) != test.destinationSlot {
				t.Fatalf("ACK = %+v body=% X", ack, ack.Body)
			}

			loaded := loadTestInventory(t, ctx, repos, "77")
			sourceKey := slotKey(test.sourceList, test.sourceSlot)
			if _, exists := loaded.Slots[sourceKey]; exists {
				t.Fatalf("source remained in slots: %+v", loaded.Slots)
			}
			if _, exists := loaded.Warehouse[sourceKey]; exists {
				t.Fatalf("source remained in warehouse: %+v", loaded.Warehouse)
			}
			destinationKey := slotKey(test.destinationList, test.destinationSlot)
			merged, exists := loaded.Slots[destinationKey]
			if test.destinationList == listTypePersonalCargo {
				merged, exists = loaded.Warehouse[destinationKey]
			}
			if !exists || merged.ItemID != 3227 || merged.Count != 5 {
				t.Fatalf("merged destination = %+v exists=%t", merged, exists)
			}
		})
	}
}

func pvfMoveStack(itemID, count, limit int64) dnfrepo.ItemStack {
	return dnfrepo.ItemStack{
		ItemID: itemID,
		Count:  count,
		Extra: map[string]string{
			"item_kind":      "stackable",
			"pvf_path":       "stackable/material/test_3227.stk",
			"stackable_type": "[material]",
			"stack_limit":    strconv.FormatInt(limit, 10),
		},
	}
}

type failingMoveAccountInventoryRepository struct {
	err error
}

func (r failingMoveAccountInventoryRepository) Load(context.Context, string) (dnfrepo.AccountInventoryRecord, bool, error) {
	return dnfrepo.AccountInventoryRecord{}, false, r.err
}

func (r failingMoveAccountInventoryRepository) Save(context.Context, dnfrepo.AccountInventoryRecord) error {
	return r.err
}

type rollbackAccountCharacterItemsAfterApply struct {
	base dnfrepo.AccountCharacterItemUnitOfWork
	err  error
}

func (u rollbackAccountCharacterItemsAfterApply) WithinAccountCharacterItems(
	ctx context.Context,
	accountID string,
	characterID string,
	apply func(dnfrepo.AccountInventoryRepository, dnfrepo.InventoryRepository) error,
) error {
	return u.base.WithinAccountCharacterItems(ctx, accountID, characterID, func(accounts dnfrepo.AccountInventoryRepository, characters dnfrepo.InventoryRepository) error {
		if err := apply(accounts, characters); err != nil {
			return err
		}
		return u.err
	})
}
