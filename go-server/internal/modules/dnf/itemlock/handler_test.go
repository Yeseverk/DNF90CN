package itemlock

import (
	"context"
	"strings"
	"testing"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestDecodeRequest(t *testing.T) {
	got, err := DecodeRequest([]byte{3, 0x08, 0x00})
	if err != nil {
		t.Fatalf("DecodeRequest error = %v", err)
	}
	if got.ListType != 3 || got.SlotIndex != 8 {
		t.Fatalf("got = %+v", got)
	}
}

func TestHandlerBlocksItemLockAck(t *testing.T) {
	got, err := NewHandler().Handle(context.Background(), alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketRequestItemUnlockCancel),
		Body:                []byte{3, 0x08, 0x00},
		AccountID:           " acc-lock ",
		SelectedCharacterID: 27,
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || got.ResponseAllowed {
		t.Fatalf("result = %+v, want handled without response", got)
	}
	if got.Operation != "request_item_unlock_cancel" {
		t.Fatalf("operation = %q", got.Operation)
	}
	if !strings.Contains(got.Reason, `account="acc-lock"`) || !strings.Contains(got.Reason, "char=27") || !strings.Contains(got.Reason, "list=3") || !strings.Contains(got.Reason, "slot=8") {
		t.Fatalf("reason should include command plan, got %q", got.Reason)
	}
}

func TestCommandRecordsOwnerGap(t *testing.T) {
	cmd := NewCommand(alignedcmd.Request{
		AccountID:           " acc-lock-2 ",
		SelectedCharacterID: 88,
	}, "request_item_lock", Request{ListType: 3, SlotIndex: 9})
	if cmd.AccountID != "acc-lock-2" || cmd.SelectedCharacterID != 88 || cmd.ListType != 3 || cmd.SlotIndex != 9 {
		t.Fatalf("cmd = %+v", cmd)
	}
	if !strings.Contains(cmd.String(), "NOTI order") {
		t.Fatalf("command plan must name notify/order gap: %s", cmd.String())
	}
}

func TestOwnerLockPersists(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "27",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 8): {ItemID: 1001, Count: 1},
		},
	})

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	result, err := owner.Apply(ctx, NewCommand(alignedcmd.Request{
		AccountID:           "acc-lock",
		SelectedCharacterID: 27,
	}, "request_item_lock", Request{ListType: listTypeMain, SlotIndex: 8}))
	if err != nil {
		t.Fatalf("Apply error = %v", err)
	}
	if !result.Changed || result.State != lockStateActive {
		t.Fatalf("result = %+v", result)
	}

	loaded := loadTestInventory(t, ctx, repos, "27")
	got := loaded.Slots[slotKey(listTypeMain, 8)].Extra["equipment_lock_state"]
	if got != lockStateActive {
		t.Fatalf("lock state = %q, want %q", got, lockStateActive)
	}
}

func TestOwnerUnlockClearsLock(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "27",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeAvatar, 4): {ItemID: 2002, Count: 1, Extra: map[string]string{"equipment_lock_state": lockStateActive}},
		},
	})

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	result, err := owner.Apply(ctx, NewCommand(alignedcmd.Request{
		AccountID:           "acc-lock",
		SelectedCharacterID: 27,
	}, "request_item_unlock", Request{ListType: listTypeAvatar, SlotIndex: 4}))
	if err != nil {
		t.Fatalf("Apply error = %v", err)
	}
	if !result.Changed || result.State != "" {
		t.Fatalf("result = %+v", result)
	}

	loaded := loadTestInventory(t, ctx, repos, "27")
	if _, ok := loaded.Slots[slotKey(listTypeAvatar, 4)].Extra["equipment_lock_state"]; ok {
		t.Fatalf("lock state should be cleared: %+v", loaded.Slots[slotKey(listTypeAvatar, 4)].Extra)
	}
}

func TestOwnerRejectsEquipmentListUntilEquipmentOwnerExists(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{CharacterID: "27"})

	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatalf("NewOwner error = %v", err)
	}
	_, err = owner.Apply(ctx, NewCommand(alignedcmd.Request{
		AccountID:           "acc-lock",
		SelectedCharacterID: 27,
	}, "request_item_lock", Request{ListType: listTypeEquipment, SlotIndex: 1}))
	if err != ErrEquipmentOwnerRequired {
		t.Fatalf("Apply error = %v, want ErrEquipmentOwnerRequired", err)
	}
}

func TestHandlerUsesOwnerAndReturnsLockAckThenListDelta(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "27",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeMain, 8): {ItemID: 1001, Count: 1},
		},
	})

	got, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketRequestItemLock),
		Body:                []byte{listTypeMain, 0x08, 0x00},
		AccountID:           " acc-lock ",
		SelectedCharacterID: 27,
		Repositories:        repos,
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || !got.ResponseAllowed || got.Operation != "request_item_lock" {
		t.Fatalf("result = %+v", got)
	}
	for _, want := range []string{"itemlock owner applied", "0x010B"} {
		if !strings.Contains(got.Reason, want) {
			t.Fatalf("reason %q missing %q", got.Reason, want)
		}
	}
	if len(got.UpperResponses) != 2 {
		t.Fatalf("response count = %d, want 2", len(got.UpperResponses))
	}
	ack := got.UpperResponses[0]
	if ack.MsgID != uint16(dnfenum.CmdPacketRequestItemLock) || ack.Classification != dnfproto.DefaultChannelClassification || !ack.AllowCodec {
		t.Fatalf("ack = %+v", ack)
	}
	if want := []byte{listTypeMain, 0x08, 0x00}; string(ack.Body) != string(want) {
		t.Fatalf("ack body = % X, want % X", ack.Body, want)
	}
	refresh := got.UpperResponses[1]
	if refresh.MsgID != msgItemLockList || refresh.Classification != 0 || !refresh.AllowCodec {
		t.Fatalf("refresh = %+v", refresh)
	}
	if want := []byte{1, 0, listTypeMain, 0x08, 0x00, itemLockStateActive}; string(refresh.Body) != string(want) {
		t.Fatalf("refresh body = % X, want % X", refresh.Body, want)
	}

	loaded := loadTestInventory(t, ctx, repos, "27")
	if got := loaded.Slots[slotKey(listTypeMain, 8)].Extra["equipment_lock_state"]; got != lockStateActive {
		t.Fatalf("lock state = %q, want %q", got, lockStateActive)
	}
}

func TestHandlerUnlockReturnsAckThenUnlockNotice(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "27",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypeAvatar, 4): {ItemID: 2002, Count: 1, Extra: map[string]string{"equipment_lock_state": lockStateActive}},
		},
	})

	got, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketRequestItemUnlock),
		Body:                []byte{listTypeAvatar, 0x04, 0x00},
		AccountID:           "acc-lock",
		SelectedCharacterID: 27,
		Repositories:        repos,
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || !got.ResponseAllowed || got.Operation != "request_item_unlock" {
		t.Fatalf("result = %+v", got)
	}
	if len(got.UpperResponses) != 2 {
		t.Fatalf("response count = %d, want 2", len(got.UpperResponses))
	}
	if ack := got.UpperResponses[0]; ack.MsgID != uint16(dnfenum.CmdPacketRequestItemUnlock) || string(ack.Body) != string([]byte{listTypeAvatar, 0x04, 0x00, 0, 0, 0, 0}) {
		t.Fatalf("ack = %+v", ack)
	}
	if notice := got.UpperResponses[1]; notice.MsgID != msgItemUnlockNotice || notice.Classification != 0 || string(notice.Body) != string([]byte{listTypeAvatar, 0x04, 0x00}) {
		t.Fatalf("notice = %+v", notice)
	}

	loaded := loadTestInventory(t, ctx, repos, "27")
	if _, ok := loaded.Slots[slotKey(listTypeAvatar, 4)].Extra["equipment_lock_state"]; ok {
		t.Fatalf("lock state should be cleared: %+v", loaded.Slots[slotKey(listTypeAvatar, 4)].Extra)
	}
}

func TestHandlerUnlockCancelReturnsAckThenListDelta(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	saveTestInventory(t, ctx, repos, dnfrepo.InventoryRecord{
		CharacterID: "27",
		Slots: map[string]dnfrepo.ItemStack{
			slotKey(listTypePet, 2): {ItemID: 3003, Count: 1, Extra: map[string]string{"equipment_lock_state": lockStateActive}},
		},
	})

	got, err := NewHandler().Handle(ctx, alignedcmd.Request{
		Opcode:              uint16(dnfenum.CmdPacketRequestItemUnlockCancel),
		Body:                []byte{listTypePet, 0x02, 0x00},
		AccountID:           "acc-lock",
		SelectedCharacterID: 27,
		Repositories:        repos,
	})
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if !got.Handled || !got.ResponseAllowed || got.Operation != "request_item_unlock_cancel" {
		t.Fatalf("result = %+v", got)
	}
	if len(got.UpperResponses) != 2 {
		t.Fatalf("response count = %d, want 2", len(got.UpperResponses))
	}
	if ack := got.UpperResponses[0]; ack.MsgID != uint16(dnfenum.CmdPacketRequestItemUnlockCancel) || string(ack.Body) != string([]byte{listTypePet, 0x02, 0x00}) {
		t.Fatalf("ack = %+v", ack)
	}
	if refresh := got.UpperResponses[1]; refresh.MsgID != msgItemLockList || string(refresh.Body) != string([]byte{1, 0, listTypePet, 0x02, 0x00, itemLockStateActive}) {
		t.Fatalf("refresh = %+v", refresh)
	}
}

func saveTestInventory(t *testing.T, ctx context.Context, repos dnfrepo.Group, record dnfrepo.InventoryRecord) {
	t.Helper()
	if err := repos.Inventory.Save(ctx, record); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
}

func loadTestInventory(t *testing.T, ctx context.Context, repos dnfrepo.Group, characterID string) dnfrepo.InventoryRecord {
	t.Helper()
	record, ok, err := repos.Inventory.Load(ctx, characterID)
	if err != nil {
		t.Fatalf("load inventory: %v", err)
	}
	if !ok {
		t.Fatalf("inventory %s not found", characterID)
	}
	return record
}
